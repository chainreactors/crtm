package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	crtm "github.com/chainreactors/crtm/pkg"
	"github.com/chainreactors/crtm/pkg/registry"
	"github.com/stretchr/testify/require"
)

type executableSource []byte

func (s executableSource) Resolve(_ context.Context, r crtm.Request) (crtm.Artifact, error) {
	return crtm.Artifact{Tool: r.Tool, Version: r.Version, Target: r.Target, Source: "fixture",
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(s)), nil },
	}, nil
}

func TestGeneratedEmbedBuildAndRun(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and executes standalone binaries")
	}
	dir := t.TempDir()
	moduleRoot, err := filepath.Abs("../..")
	require.NoError(t, err)
	goMod := fmt.Sprintf("module bundle-fixture\n\ngo 1.20\nrequire github.com/chainreactors/crtm v0.0.0\nreplace github.com/chainreactors/crtm => %s\n", filepath.ToSlash(moduleRoot))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644))
	runGo := func(target crtm.Target, args ...string) []byte {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOOS="+target.GOOS, "GOARCH="+target.GOARCH, "CGO_ENABLED=0")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return out
	}
	target := crtm.CurrentTarget()
	fixture := filepath.Join(dir, "fixture.go")
	require.NoError(t, os.WriteFile(fixture, []byte("package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"offline embedded executable\")}"), 0644))
	binary := filepath.Join(dir, crtm.BinaryName("fixture"))
	runGo(target, "build", "-mod=mod", "-o", binary, fixture)
	body, err := os.ReadFile(binary)
	require.NoError(t, err)
	require.NoError(t, os.Remove(fixture))
	// Keep two platform bundles beside one another; only the selected one embeds.
	other := crtm.Target{GOOS: "linux", GOARCH: "arm64"}
	if other == target {
		other = crtm.Target{GOOS: "windows", GOARCH: "amd64"}
	}
	spec := crtm.BundleSpec{ID: "fixture-app", Tools: map[string]string{"fixture-tool": "1.0.0"},
		Platforms: map[string]crtm.ToolSelection{other.String(): {"cross-tool": "2.0.0"}}, Definitions: []registry.ToolEntry{
			{Name: "cross-tool", Repo: "example/cross-tool"},
			{Name: "fixture-tool", Repo: "example/fixture", AssetPattern: "{name}_{os}_{arch}", Platforms: map[string]registry.PlatformAsset{
				"linux/arm64": {Asset: "fixture-linux-arm64"},
			}},
		}}
	require.NoError(t, writeSpec(dir, "main", spec))
	for _, platform := range []crtm.Target{target, other} {
		payload := body
		if platform != target {
			header := map[string]string{"linux": "\x7fELF", "windows": "MZxx"}[platform.GOOS]
			payload = []byte(header + "cross build fixture")
		}
		bundleDir, err := crtm.BuildBundle(context.Background(), spec, platform, filepath.Join(dir, "assets", platform.GOOS+"_"+platform.GOARCH), executableSource(payload))
		require.NoError(t, err)
		require.NoError(t, writeEmbed(dir, bundleDir, "main", "arsenal_embed", platform))
		// Regeneration must replace the generated file on Windows as well.
		require.NoError(t, writeEmbed(dir, bundleDir, "main", "arsenal_embed", platform))
	}
	main := `package main
import (
 "context"
 "os"
 "os/exec"
 "path/filepath"
 crtm "github.com/chainreactors/crtm/pkg"
)
func main() {
 if len(ToolSpec.ToolsFor(crtm.CurrentTarget())) != 1 { panic("wrong runtime tool selection") }
 b, err := EmbeddedBundle(); if err != nil { panic(err) }; if b == nil { return }
 options := ToolSpec.ManagerOption(b)
 options.BinPath, options.ConfigPath = filepath.Join(os.Args[1],"bin"), filepath.Join(os.Args[1],"config.yaml")
 m, err := crtm.NewManager(options)
 if err != nil { panic(err) }; if err = m.Prepare(context.Background()); err != nil { panic(err) }
 c:=exec.Command(filepath.Join(m.BinPath(),crtm.BinaryName("fixture-tool"))); c.Stdout=os.Stdout; c.Stderr=os.Stderr
 if err=c.Run(); err!=nil { panic(err) }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0644))
	stub := "//go:build !arsenal_embed\n\npackage main\nimport crtm \"github.com/chainreactors/crtm/pkg\"\nfunc EmbeddedBundle()(*crtm.Bundle,error){return nil,nil}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "external.go"), []byte(stub), 0644))
	for _, platform := range []crtm.Target{target, other} {
		var listed struct{ EmbedFiles []string }
		out := runGo(platform, "list", "-mod=mod", "-json", "-tags=arsenal_embed", ".")
		require.NoError(t, json.Unmarshal(out, &listed))
		require.Len(t, listed.EmbedFiles, 1+len(spec.ToolsFor(platform)))
		for _, file := range listed.EmbedFiles {
			require.Contains(t, file, "assets/"+platform.GOOS+"_"+platform.GOARCH+"/")
		}
		runGo(platform, "build", "-mod=mod", "-tags=arsenal_embed", "-o", filepath.Join(dir, "application-"+platform.GOOS+".exe"), ".")
	}
	out := runGo(target, "list", "-mod=mod", "-json", ".")
	var plain struct{ EmbedFiles []string }
	require.NoError(t, json.Unmarshal(out, &plain))
	require.Empty(t, plain.EmbedFiles)
	runGo(target, "build", "-mod=mod", "-o", filepath.Join(dir, "plain.exe"), ".")
	// Run the embedded application after the source payloads have disappeared.
	require.NoError(t, os.Rename(filepath.Join(dir, "assets"), filepath.Join(dir, "unavailable-assets")))
	cmd := exec.Command(filepath.Join(dir, "application-"+target.GOOS+".exe"), t.TempDir())
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, "offline embedded executable\n", strings.ReplaceAll(string(out), "\r\n", "\n"))
}
