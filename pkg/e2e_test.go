//go:build e2e

package pkg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/chainreactors/crtm/pkg/registry"
	"github.com/stretchr/testify/require"
)

// E2E tests hit the real GitHub release CDN (direct HTTP, no API).
// Run with: go test -tags e2e -v ./pkg/...

func TestE2E_InstallChainreactorsTool(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install test only runs on linux/amd64")
	}

	entry := registry.ToolEntry{
		Name:         "gogo",
		Repo:         "chainreactors/gogo",
		AssetPattern: "{name}_{os}_{arch}",
	}

	binDir := t.TempDir()
	err := DownloadAndInstall(entry, "", binDir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(binDir, "gogo"))
	require.NoError(t, err)
	require.True(t, info.Mode().Perm()&0111 != 0, "should be executable")
	require.Greater(t, info.Size(), int64(100_000), "should be a real binary")
}

func TestE2E_InstallPDTool(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install test only runs on linux/amd64")
	}

	entry := registry.ToolEntry{
		Name:         "dnsx",
		Repo:         "projectdiscovery/dnsx",
		AssetPattern: "{name}_{version}_{os}_{arch}.zip",
	}

	// Use pinned version so the asset name is exact.
	binDir := t.TempDir()
	err := DownloadAndInstall(entry, "1.2.3", binDir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(binDir, "dnsx"))
	require.NoError(t, err)
	require.True(t, info.Mode().Perm()&0111 != 0, "should be executable")
	require.Greater(t, info.Size(), int64(100_000), "should be a real binary")
}

func TestE2E_InstallPDToolLatest(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install test only runs on linux/amd64")
	}

	// Latest install for PD tool — version auto-resolved from /releases/latest redirect.
	entry := registry.ToolEntry{
		Name:         "mapcidr",
		Repo:         "projectdiscovery/mapcidr",
		AssetPattern: "{name}_{version}_{os}_{arch}.zip",
	}

	binDir := t.TempDir()
	err := DownloadAndInstall(entry, "", binDir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(binDir, "mapcidr"))
	require.NoError(t, err, "mapcidr binary should exist")
	require.True(t, info.Mode().Perm()&0111 != 0, "should be executable")
	require.Greater(t, info.Size(), int64(100_000), "should be a real binary")
}

func TestE2E_InstallFFuf(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install test only runs on linux/amd64")
	}

	// ffuf uses {name}_{version}_{os}_{arch}.tar.gz — version auto-resolved.
	entry := registry.ToolEntry{
		Name:         "ffuf",
		Repo:         "ffuf/ffuf",
		AssetPattern: "{name}_{version}_{os}_{arch}.tar.gz",
	}

	binDir := t.TempDir()
	err := DownloadAndInstall(entry, "", binDir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(binDir, "ffuf"))
	require.NoError(t, err, "ffuf binary should exist")
	require.True(t, info.Mode().Perm()&0111 != 0, "should be executable")
	require.Greater(t, info.Size(), int64(1_000_000), "ffuf should be >1MB")
}

func TestE2E_ManagerInstallAndRemove(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install test only runs on linux/amd64")
	}

	dir := t.TempDir()
	mgr, err := NewManager(ManagerOption{
		BinPath:    filepath.Join(dir, "bin"),
		ConfigPath: filepath.Join(dir, "config.yaml"),
	})
	require.NoError(t, err)

	// Install a CR tool (uses /releases/latest/download/).
	err = mgr.InstallTool("gogo")
	require.NoError(t, err)
	require.True(t, mgr.IsInstalled("gogo"))

	// Install again should fail.
	err = mgr.InstallTool("gogo")
	require.Error(t, err)

	// Remove.
	err = mgr.RemoveTool("gogo")
	require.NoError(t, err)
	require.False(t, mgr.IsInstalled("gogo"))
}
