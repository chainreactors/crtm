package pkg

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chainreactors/crtm/pkg/registry"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func archiveWith(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, data := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(f, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestExactArchiveSelection(t *testing.T) {
	data := archiveWith(t, map[string]string{"bin/ast-grep.exe": "correct", "sg.exe": strings.Repeat("wrong", 1000), "README": strings.Repeat("docs", 3000)})
	out, err := binaryFromArchive(data, "ast-grep.exe")
	if err != nil || string(out) != "correct" {
		t.Fatalf("selection: %q %v", out, err)
	}
	if _, err := binaryFromArchive(data, "absent.exe"); err == nil {
		t.Fatal("must not select largest file")
	}
	duplicate := archiveWith(t, map[string]string{"a/rg": "first", "b/rg": "second"})
	if _, err := binaryFromArchive(duplicate, "rg"); err == nil {
		t.Fatal("ambiguous archive accepted")
	}
}
func TestReleaseTagPreserved(t *testing.T) {
	old := noFollowClient
	t.Cleanup(func() { noFollowClient = old })
	noFollowClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://github.com/org/tool/releases/tag/cli%2Fv1.2.3"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	release, err := ResolveLatestRelease(context.Background(), "org/tool")
	if err != nil || release.Tag != "cli/v1.2.3" || release.Version != "1.2.3" {
		t.Fatalf("%+v %v", release, err)
	}
}
func TestStagedInstallFailurePreservesBinary(t *testing.T) {
	header := map[string]string{"windows": "MZxx", "linux": "\x7fELF", "darwin": "\xcf\xfa\xed\xfe"}[runtime.GOOS]
	if header == "" {
		t.Skip("unsupported native platform")
	}
	old := httpClient
	t.Cleanup(func() { httpClient = old })
	httpClient = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(header + "new executable"))}, nil
	})}
	dir := t.TempDir()
	target := filepath.Join(dir, BinaryName("fixture"))
	if err := os.WriteFile(target, []byte("old executable"), 0755); err != nil {
		t.Fatal(err)
	}
	entry := registry.ToolEntry{Name: "fixture", Repo: "org/fixture", AssetPattern: "fixture", Platforms: map[string]registry.PlatformAsset{runtime.GOOS + "/" + runtime.GOARCH: {Asset: "fixture"}}}
	release := Release{Tag: "1.0.0", Version: "1.0.0"}
	fail := func(context.Context, string) error { return errors.New("incompatible") }
	if err := InstallRelease(context.Background(), entry, release, dir, fail); err == nil {
		t.Fatal("expected validation failure")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old executable" {
		t.Fatal("old binary lost")
	}
	if err := InstallRelease(context.Background(), entry, release, dir, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(target)
	if string(got) != header+"new executable" {
		t.Fatal("binary not replaced")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("staging files leaked")
	}
	mgr, err := NewManager(ManagerOption{BinPath: dir, ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if !mgr.IsInstalled("fixture") {
		t.Fatal("installed executable not found")
	}
	if err := mgr.RemoveTool("fixture"); err != nil {
		t.Fatal(err)
	}
}
func TestInstallCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	entries, _ := registry.LoadEmbedded()
	for _, entry := range entries {
		if entry.Name == "rg" {
			if err := InstallRelease(ctx, entry, Release{Tag: "15.2.0", Version: "15.2.0"}, t.TempDir(), nil); !errors.Is(err, context.Canceled) {
				t.Fatalf("%v", err)
			}
		}
	}
}

func TestTarExecutableSelection(t *testing.T) {
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	for _, entry := range []struct{ name, body string }{{"release/README", strings.Repeat("docs", 4096)}, {"release/rg", "selected executable"}} {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0755, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := binaryFromArchive(data.Bytes(), "rg")
	if err != nil || string(body) != "selected executable" {
		t.Fatalf("tar selection: %q %v", body, err)
	}
	if _, err := binaryFromArchive(data.Bytes(), "absent"); err == nil {
		t.Fatal("tar selected unrelated content")
	}
}
