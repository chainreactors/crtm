package pkg

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chainreactors/crtm/pkg/registry"
	"github.com/stretchr/testify/require"
)

func TestSharedCatalogSelection(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "release"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "arsenal.yaml"), []byte(`
- name: selected
  version: "2.0.0"
  repo: example/selected
  asset_pattern: "{name}_{os}_{arch}"
- name: unselected
  version: "3.0.0"
  repo: example/unselected
  asset_pattern: "{name}_{os}_{arch}"
`), 0644))
	selection := filepath.Join(dir, "release", "arsenal.yaml")
	require.NoError(t, os.WriteFile(selection, []byte(`
id: release
catalog: ../arsenal.yaml
tools:
  gogo: "1.1.0"
  selected:
`), 0644))
	spec, err := LoadBundleSpec(selection)
	require.NoError(t, err)
	require.NoError(t, spec.Validate())
	require.Equal(t, "release", spec.ID)
	require.Empty(t, spec.Catalog)
	require.Equal(t, ToolSelection{"gogo": "1.1.0", "selected": "2.0.0"}, spec.Tools)
	require.Equal(t, []registry.ToolEntry{{Name: "selected", Version: "2.0.0", Repo: "example/selected", AssetPattern: "{name}_{os}_{arch}"}}, spec.CustomTools)

	// Remote-only installations also know the selected custom definition, without
	// writing it into user configuration or leaking unselected tools.
	opt := spec.ManagerOption(nil)
	opt.BinPath, opt.ConfigPath = filepath.Join(dir, "bin"), filepath.Join(dir, "config.yaml")
	mgr, err := NewManager(opt)
	require.NoError(t, err)
	entry, ok := mgr.Catalog().Find("selected")
	require.True(t, ok)
	require.Equal(t, "example/selected", entry.Repo)
	_, ok = mgr.Catalog().Find("unselected")
	require.False(t, ok)
	_, err = os.Stat(opt.ConfigPath)
	require.True(t, os.IsNotExist(err))

	require.NoError(t, os.WriteFile(selection, []byte("id: release\ncatalog: ../arsenal.yaml\ntools:\n  unknown:\n"), 0644))
	_, err = LoadBundleSpec(selection)
	require.ErrorContains(t, err, `unknown tool "unknown"`)
	require.NoError(t, os.WriteFile(selection, []byte("id: release\ncatalogue: ../arsenal.yaml\ntools:\n  gogo:\n"), 0644))
	_, err = LoadBundleSpec(selection)
	require.ErrorContains(t, err, "catalogue")
}

func TestBundleSelectsArsenalNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.yaml")
	require.NoError(t, os.WriteFile(path, []byte("id: audit\ntools: [rg, ast-grep, osv-scanner]\n"), 0644))
	spec, err := LoadBundleSpec(path)
	require.NoError(t, err)
	require.Len(t, spec.Tools, 3)
	entries, err := registry.LoadEmbedded()
	require.NoError(t, err)
	for _, entry := range entries {
		if version, selected := spec.Tools[entry.Name]; selected {
			require.NotEmpty(t, version)
			require.Equal(t, entry.Version, version)
		}
	}
	// A bundle contains exactly the selected subset of the larger catalog.
	dir, err := BuildBundle(context.Background(), spec, CurrentTarget(), t.TempDir(), fixtureSource(nil))
	require.NoError(t, err)
	bundle, err := OpenBundle(os.DirFS(dir))
	require.NoError(t, err)
	require.Len(t, bundle.manifest.Tools, 3)
	for _, artifact := range bundle.manifest.Tools {
		require.Equal(t, spec.Tools[artifact.Tool.Name], artifact.Version)
	}
	require.NoError(t, os.WriteFile(path, []byte("id: audit\ntools: [rg, rg]\n"), 0644))
	_, err = LoadBundleSpec(path)
	require.ErrorContains(t, err, "duplicate tool")
}
