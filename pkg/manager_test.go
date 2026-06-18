package pkg

import (
	"path/filepath"
	"testing"

	"github.com/chainreactors/crtm/pkg/registry"
	"github.com/stretchr/testify/require"
)

func TestNewManagerOffline(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(ManagerOption{
		BinPath:    filepath.Join(dir, "bin"),
		ConfigPath: filepath.Join(dir, "nonexistent.yaml"),
	})
	require.NoError(t, err, "should work even without config file")
	require.NotNil(t, mgr.Catalog())

	tools := mgr.ListTools()
	require.NotEmpty(t, tools, "should have embedded tools")

	names := map[string]bool{}
	for _, t := range tools {
		names[t.Name] = true
	}
	require.True(t, names["gogo"])
	require.True(t, names["nuclei"])
}

func TestManagerSearch(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(ManagerOption{
		BinPath:    filepath.Join(dir, "bin"),
		ConfigPath: filepath.Join(dir, "nonexistent.yaml"),
	})
	require.NoError(t, err)

	results := mgr.Search("scanner")
	require.NotEmpty(t, results)
	for _, r := range results {
		matched := false
		if contains(r.Tags, "scanner") || r.Category == "scanner" {
			matched = true
		}
		require.True(t, matched, "search result %s should match 'scanner'", r.Name)
	}
}

func TestManagerSearchByName(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(ManagerOption{
		BinPath:    filepath.Join(dir, "bin"),
		ConfigPath: filepath.Join(dir, "nonexistent.yaml"),
	})
	require.NoError(t, err)

	results := mgr.Search("nuclei")
	require.NotEmpty(t, results)
	require.Equal(t, "nuclei", results[0].Name)
}

func TestManagerAddCustomTool(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	mgr, err := NewManager(ManagerOption{
		BinPath:    filepath.Join(dir, "bin"),
		ConfigPath: configPath,
	})
	require.NoError(t, err)

	_, found := mgr.Catalog().Find("ffuf")
	require.False(t, found, "ffuf should not be in catalog initially")

	added, err := mgr.AddCustomTool(registry.ToolEntry{
		Name:         "ffuf",
		Repo:         "ffuf/ffuf",
		AssetPattern: "{name}_{version}_{os}_{arch}.tar.gz",
		Tags:         []string{"fuzzer", "web"},
	})
	require.NoError(t, err)
	require.True(t, added)

	entry, found := mgr.Catalog().Find("ffuf")
	require.True(t, found, "ffuf should be in catalog after add")
	require.Equal(t, "ffuf/ffuf", entry.Repo)

	// Reload and verify persistence.
	mgr2, err := NewManager(ManagerOption{
		BinPath:    filepath.Join(dir, "bin"),
		ConfigPath: configPath,
	})
	require.NoError(t, err)
	_, found = mgr2.Catalog().Find("ffuf")
	require.True(t, found, "ffuf should persist across reload")
}

func TestManagerAddDuplicate(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	mgr, err := NewManager(ManagerOption{
		BinPath:    filepath.Join(dir, "bin"),
		ConfigPath: configPath,
	})
	require.NoError(t, err)

	added, _ := mgr.AddCustomTool(registry.ToolEntry{Name: "ffuf", Repo: "ffuf/ffuf", AssetPattern: "{name}_{os}_{arch}"})
	require.True(t, added)

	added, _ = mgr.AddCustomTool(registry.ToolEntry{Name: "ffuf", Repo: "ffuf/ffuf", AssetPattern: "{name}_{os}_{arch}"})
	require.False(t, added, "should not add duplicate")
}

func TestManagerRemoveNotInstalled(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(ManagerOption{
		BinPath:    filepath.Join(dir, "bin"),
		ConfigPath: filepath.Join(dir, "none.yaml"),
	})
	require.NoError(t, err)

	err = mgr.RemoveTool("nonexistent")
	require.Error(t, err)
}

func contains(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}
