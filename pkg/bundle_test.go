package pkg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/crtm/pkg/registry"
	"github.com/stretchr/testify/require"
)

type sourceFunc func(context.Context, Request) (Artifact, error)

func (f sourceFunc) Resolve(ctx context.Context, r Request) (Artifact, error) { return f(ctx, r) }

func fixtureSource(body []byte) Source {
	return sourceFunc(func(_ context.Context, r Request) (Artifact, error) {
		version := strings.TrimPrefix(r.Version, "v")
		if version == "" || version == "latest" {
			version = "9.0.0"
		}
		data := body
		if data == nil {
			header := map[string]string{"windows": "MZxx", "linux": "\x7fELF", "darwin": "\xcf\xfa\xed\xfe"}[r.Target.GOOS]
			data = []byte(header + r.Tool.Name + version)
		}
		return Artifact{Tool: r.Tool, Version: version, Target: r.Target, Source: "fixture", Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}, nil
	})
}

func testBundle(t *testing.T, version string) (*Bundle, string) {
	t.Helper()
	dir, err := BuildBundle(context.Background(), BundleSpec{ID: "test-app", Tools: map[string]string{"gogo": version}}, CurrentTarget(), t.TempDir(), fixtureSource(nil))
	require.NoError(t, err)
	b, err := OpenBundle(os.DirFS(dir))
	require.NoError(t, err)
	return b, dir
}

func bundleManager(t *testing.T, dir string, sources ...Source) *Manager {
	t.Helper()
	m, err := NewManager(ManagerOption{BinPath: filepath.Join(dir, "bin"), ConfigPath: filepath.Join(dir, "config.yaml"), Sources: sources})
	require.NoError(t, err)
	return m
}

func TestBundleLifecycle(t *testing.T) {
	ctx := context.Background()
	b1, _ := testBundle(t, "1.0.0")
	b2, _ := testBundle(t, "2.0.0")
	dir := t.TempDir()
	// Construction is read-only, even with an embedded source.
	m := bundleManager(t, dir, b1, fixtureSource(nil))
	_, err := os.Stat(m.binPath)
	require.True(t, os.IsNotExist(err))
	require.NoError(t, m.Prepare(ctx, b1))
	require.Equal(t, "1.0.0", m.InstalledVersion("gogo"))
	initial, _ := os.Stat(m.binaryPath("gogo"))
	entry, _ := m.manifest.Get("gogo")
	require.Equal(t, "test-app", entry.ManagedBy)
	require.NoError(t, m.Prepare(ctx, b1))
	again, _ := os.Stat(m.binaryPath("gogo"))
	require.Equal(t, initial.ModTime(), again.ModTime())
	require.True(t, os.SameFile(initial, again))

	m = bundleManager(t, dir, b2, fixtureSource(nil))
	require.NoError(t, m.Prepare(ctx, b2))
	require.Equal(t, "2.0.0", m.InstalledVersion("gogo"))
	// Explicit update bypasses the immutable bundle and relinquishes ownership.
	require.NoError(t, m.UpdateTool("gogo"))
	require.Equal(t, "9.0.0", m.InstalledVersion("gogo"))
	require.NoError(t, m.Prepare(ctx, b1))
	require.Equal(t, "9.0.0", m.InstalledVersion("gogo"))
	entry, _ = m.manifest.Get("gogo")
	require.Empty(t, entry.ManagedBy)

	require.NoError(t, m.RemoveTool("gogo"))
	require.NoError(t, m.Prepare(ctx, b2))
	require.Equal(t, "2.0.0", m.InstalledVersion("gogo"))
	// Same process can reinstall from the bundle without a network source.
	require.NoError(t, m.RemoveTool("gogo"))
	offline := bundleManager(t, dir, b2)
	require.NoError(t, offline.InstallVersion("gogo", "v2.0.0"))
	require.ErrorIs(t, offline.InstallVersion("gogo", "3.0.0"), ErrArtifactNotFound)
}

func TestBundlePreservesUserFiles(t *testing.T) {
	b1, _ := testBundle(t, "1.0.0")
	b2, _ := testBundle(t, "2.0.0")
	for _, mode := range []string{"modified", "legacy", "different-owner"} {
		t.Run(mode, func(t *testing.T) {
			m := bundleManager(t, t.TempDir(), b1)
			require.NoError(t, m.Prepare(context.Background(), b1))
			entry, _ := m.manifest.Get("gogo")
			switch mode {
			case "modified":
				require.NoError(t, os.WriteFile(m.binaryPath("gogo"), []byte("user binary"), 0755))
			case "legacy":
				require.NoError(t, m.manifest.Set("gogo", "1.0.0"))
			case "different-owner":
				entry.ManagedBy = "other-app"
				require.NoError(t, m.manifest.put("gogo", entry))
			}
			before, err := os.ReadFile(m.binaryPath("gogo"))
			require.NoError(t, err)
			require.NoError(t, m.Prepare(context.Background(), b2))
			after, err := os.ReadFile(m.binaryPath("gogo"))
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestBundleCorruptionDoesNotFallbackOrReplace(t *testing.T) {
	b1, _ := testBundle(t, "1.0.0")
	b2, dir := testBundle(t, "2.0.0")
	fallback := sourceFunc(func(context.Context, Request) (Artifact, error) {
		t.Fatal("corruption must not cause fallback")
		return Artifact{}, nil
	})
	m := bundleManager(t, t.TempDir(), b1)
	require.NoError(t, m.Prepare(context.Background(), b1))
	before, err := os.ReadFile(m.binaryPath("gogo"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.gz"), []byte("broken"), 0644))
	require.Error(t, m.Prepare(context.Background(), b2))
	after, err := os.ReadFile(m.binaryPath("gogo"))
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, "1.0.0", m.InstalledVersion("gogo"))
	m = bundleManager(t, t.TempDir(), b2, fallback)
	require.Error(t, m.InstallTool("gogo"))
	require.False(t, m.IsInstalled("gogo"))
}

func TestBundleIntegrityAndValidationPreserveInstallation(t *testing.T) {
	for _, failure := range []string{"checksum", "size", "validation"} {
		t.Run(failure, func(t *testing.T) {
			b1, _ := testBundle(t, "1.0.0")
			b2, _ := testBundle(t, "2.0.0")
			m := bundleManager(t, t.TempDir(), b1)
			require.NoError(t, m.Prepare(context.Background(), b1))
			before, err := os.ReadFile(m.binaryPath("gogo"))
			require.NoError(t, err)
			switch failure {
			case "checksum":
				b2.manifest.Tools[0].SHA256 = strings.Repeat("0", 64)
			case "size":
				b2.manifest.Tools[0].Size--
			}
			if failure == "validation" {
				m.sources = []Source{b2}
				err = m.InstallVersionContext(context.Background(), "gogo", "2.0.0", func(context.Context, string) error { return errors.New("incompatible tool") })
			} else {
				err = m.Prepare(context.Background(), b2)
			}
			require.Error(t, err)
			after, err := os.ReadFile(m.binaryPath("gogo"))
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Equal(t, "1.0.0", m.InstalledVersion("gogo"))
			files, err := os.ReadDir(m.binPath)
			require.NoError(t, err)
			for _, f := range files {
				require.False(t, strings.HasSuffix(f.Name(), ".tmp"), "staged file leaked")
			}
		})
	}
}

func TestBundleSourcesAndDefinitions(t *testing.T) {
	b, _ := testBundle(t, "1.0.0")
	m := bundleManager(t, t.TempDir(), b, fixtureSource(nil))
	require.NoError(t, m.InstallVersion("gogo", "2.0.0"))
	require.Equal(t, "2.0.0", m.InstalledVersion("gogo"))
	errSource := sourceFunc(func(context.Context, Request) (Artifact, error) { return Artifact{}, errors.New("source failed") })
	m = bundleManager(t, t.TempDir(), errSource, fixtureSource(nil))
	require.ErrorContains(t, m.InstallTool("gogo"), "source failed")

	custom := registry.ToolEntry{Name: "bundle-fixture", Repo: "example/fixture", AssetPattern: "fixture_{os}_{arch}"}
	output, err := BuildBundle(context.Background(), BundleSpec{ID: "custom", Tools: map[string]string{custom.Name: "1.0.0"}, CustomTools: []registry.ToolEntry{custom}}, CurrentTarget(), t.TempDir(), fixtureSource(nil))
	require.NoError(t, err)
	b, err = OpenBundle(os.DirFS(output))
	require.NoError(t, err)
	dir := t.TempDir()
	m = bundleManager(t, dir, b)
	require.NoError(t, m.Prepare(context.Background(), b))
	_, ok := m.catalog.Find(custom.Name)
	require.True(t, ok)
	_, err = os.Stat(m.configPath)
	require.True(t, os.IsNotExist(err), "bundle must not rewrite user config")
	custom.Repo = "different/repo"
	require.NoError(t, SaveCRTMConfig(m.configPath, CRTMConfig{CustomTools: []registry.ToolEntry{custom}}))
	_, err = NewManager(ManagerOption{BinPath: m.binPath, ConfigPath: m.configPath, Sources: []Source{b}})
	require.ErrorContains(t, err, "conflicts")
}

func TestBundleBuildReproducibleAndAtomic(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	spec := BundleSpec{ID: "example", Tools: map[string]string{"gogo": "1.0.0"}}
	first, err := BuildBundle(ctx, spec, CurrentTarget(), root, fixtureSource(nil))
	require.NoError(t, err)
	again, err := BuildBundle(ctx, spec, CurrentTarget(), root, fixtureSource(nil))
	require.NoError(t, err)
	require.Equal(t, first, again)
	spec.Tools["nuclei"] = "1.0.0"
	fail := sourceFunc(func(ctx context.Context, req Request) (Artifact, error) {
		if req.Tool.Name == "nuclei" {
			return Artifact{}, errors.New("download failed")
		}
		return fixtureSource(nil).Resolve(ctx, req)
	})
	_, err = BuildBundle(ctx, spec, CurrentTarget(), root, fail)
	require.Error(t, err)
	files, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, files, 1, "failed build must leave only the previous complete bundle")
	_, err = OpenBundle(os.DirFS(first))
	require.NoError(t, err)
}

func TestBundleMetadataValidation(t *testing.T) {
	for _, change := range []string{"platform", "name", "digest", "size", "format", "duplicate", "duplicate-exe"} {
		t.Run(change, func(t *testing.T) {
			b, dir := testBundle(t, "1.0.0")
			switch change {
			case "platform":
				b.manifest.Tools[0].Target.GOOS = "other"
			case "name":
				b.manifest.Tools[0].Tool.Name = "../escape"
			case "digest":
				b.manifest.Tools[0].SHA256 = "bad"
			case "size":
				b.manifest.Tools[0].Size = MaxBinarySize + 1
			case "format":
				b.manifest.Format = 99
			case "duplicate":
				b.manifest.Tools = append(b.manifest.Tools, b.manifest.Tools[0])
			case "duplicate-exe":
				duplicate := b.manifest.Tools[0]
				duplicate.Tool.Name = "GOGO.EXE"
				b.manifest.Tools = append(b.manifest.Tools, duplicate)
			}
			data, err := json.Marshal(b.manifest)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644))
			_, err = OpenBundle(os.DirFS(dir))
			require.Error(t, err)
		})
	}
}

func TestBundleCrossTarget(t *testing.T) {
	target := Target{GOOS: "linux", GOARCH: "arm64"}
	if target == CurrentTarget() {
		target.GOOS = "windows"
	}
	dir, err := BuildBundle(context.Background(), BundleSpec{ID: "cross", Tools: map[string]string{"gogo": "1.0.0"}}, target, t.TempDir(), fixtureSource(nil))
	require.NoError(t, err)
	b, err := OpenBundle(os.DirFS(dir))
	require.NoError(t, err)
	m := bundleManager(t, t.TempDir(), b)
	require.ErrorContains(t, m.Prepare(context.Background(), b), "targets")
	require.False(t, m.IsInstalled("gogo"))
}

func TestBundleConcurrentPrepare(t *testing.T) {
	b, _ := testBundle(t, "1.0.0")
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	// All managers are constructed before any writes, exercising manifest reload.
	managers := make([]*Manager, 8)
	for i := range managers {
		managers[i] = bundleManager(t, dir, b)
	}
	for _, m := range managers {
		wg.Add(1)
		go func(m *Manager) { defer wg.Done(); errs <- m.Prepare(context.Background(), b) }(m)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for _, m := range managers {
		require.Equal(t, "1.0.0", m.InstalledVersion("gogo"))
	}
}

func TestBundleProcessHelper(t *testing.T) {
	if os.Getenv("CRTM_BUNDLE_HELPER") != "1" {
		return
	}
	b, err := OpenBundle(os.DirFS(os.Getenv("CRTM_BUNDLE_DIR")))
	require.NoError(t, err)
	m := bundleManager(t, os.Getenv("CRTM_INSTALL_DIR"), b)
	require.NoError(t, m.Prepare(context.Background(), b))
}

func TestBundleConcurrentProcesses(t *testing.T) {
	_, bundleDir := testBundle(t, "1.0.0")
	dir := t.TempDir()
	processes := make([]*exec.Cmd, 4)
	for i := range processes {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBundleProcessHelper$")
		cmd.Env = append(os.Environ(), "CRTM_BUNDLE_HELPER=1", "CRTM_BUNDLE_DIR="+bundleDir, "CRTM_INSTALL_DIR="+dir)
		cmd.Stdout, cmd.Stderr = &bytes.Buffer{}, &bytes.Buffer{}
		require.NoError(t, cmd.Start())
		processes[i] = cmd
	}
	for _, cmd := range processes {
		require.NoError(t, cmd.Wait(), fmt.Sprint(cmd.Stderr))
	}
	m := bundleManager(t, dir)
	require.Equal(t, "1.0.0", m.InstalledVersion("gogo"))
}

func TestInstallLockCancellation(t *testing.T) {
	dir := t.TempDir()
	release, err := lockDirectory(context.Background(), dir)
	require.NoError(t, err)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = lockDirectory(ctx, dir)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestBundleManifestWriteFailureIsReported(t *testing.T) {
	b, _ := testBundle(t, "1.0.0")
	m := bundleManager(t, t.TempDir(), b)
	// Make the metadata parent unwritable portably, including as root on Unix.
	bad := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(bad, nil, 0644))
	m.manifest.path = filepath.Join(bad, "manifest.json")
	require.Error(t, m.Prepare(context.Background(), b))
	require.False(t, m.IsInstalled("gogo"))
}

func TestBundleRealExecutable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a real Go executable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(source, []byte("package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"bundle works\")}"), 0644))
	executable := filepath.Join(dir, BinaryName("fixture"))
	cmd := exec.Command("go", "build", "-o", executable, source)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	body, err := os.ReadFile(executable)
	require.NoError(t, err)
	bundleDir, err := BuildBundle(context.Background(), BundleSpec{ID: "executable", Tools: map[string]string{"gogo": "1.0.0"}}, CurrentTarget(), t.TempDir(), fixtureSource(body))
	require.NoError(t, err)
	b, err := OpenBundle(os.DirFS(bundleDir))
	require.NoError(t, err)
	m := bundleManager(t, t.TempDir(), b)
	require.NoError(t, m.Prepare(context.Background(), b))
	out, err = exec.Command(m.binaryPath("gogo")).CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, "bundle works\n", strings.ReplaceAll(string(out), "\r\n", "\n"))
	// Consumers such as audit execute the staged path before installation.
	require.NoError(t, m.InstallVersionContext(context.Background(), "gogo", "1.0.0", func(ctx context.Context, path string) error {
		out, err := exec.CommandContext(ctx, path).CombinedOutput()
		if err != nil {
			return fmt.Errorf("staged executable: %w: %s", err, out)
		}
		return nil
	}))
	if runtime.GOOS != "windows" {
		info, err := os.Stat(m.binaryPath("gogo"))
		require.NoError(t, err)
		require.NotZero(t, info.Mode().Perm()&0111)
	}
}
