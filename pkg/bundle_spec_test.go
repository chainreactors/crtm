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

func TestPlatformBundleSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
id: audit
tools: [rg]
platforms:
  windows/amd64: [windows-tool, shared-tool, second-tool]
  linux/amd64:
    shared-tool:
    second-tool:
    rg: "15.3.0"
custom_tools:
  - name: windows-tool
    version: 6.2.2
    repo: example/windows-tool
  - name: shared-tool
    version: 9.4.0
    repo: example/shared-tool
  - name: second-tool
    version: 3.1.1
    repo: example/second-tool
  - name: unused
    repo: example/unused
`), 0644))
	spec, err := LoadBundleSpec(path)
	require.NoError(t, err)
	require.NoError(t, spec.Validate())
	require.Len(t, spec.CustomTools, 3)
	for _, test := range []struct {
		target Target
		tools  ToolSelection
	}{
		{Target{"windows", "amd64"}, ToolSelection{"rg": "15.2.0", "windows-tool": "6.2.2", "shared-tool": "9.4.0", "second-tool": "3.1.1"}},
		{Target{"linux", "amd64"}, ToolSelection{"rg": "15.3.0", "shared-tool": "9.4.0", "second-tool": "3.1.1"}},
		{Target{"windows", "arm64"}, ToolSelection{"rg": "15.2.0"}},
		{Target{"darwin", "arm64"}, ToolSelection{"rg": "15.2.0"}},
	} {
		t.Run(test.target.String(), func(t *testing.T) {
			require.Equal(t, test.tools, spec.ToolsFor(test.target))
			dir, err := BuildBundle(context.Background(), spec, test.target, t.TempDir(), fixtureSource(nil))
			require.NoError(t, err)
			bundle, err := OpenBundle(os.DirFS(dir))
			require.NoError(t, err)
			actual := ToolSelection{}
			for _, tool := range bundle.manifest.Tools {
				actual[tool.Tool.Name] = tool.Version
			}
			require.Equal(t, test.tools, actual)
		})
	}
	// Selection and overrides must not leak into another target or common tools.
	spec.ToolsFor(Target{"linux", "amd64"})["rg"] = "changed"
	require.Equal(t, ToolSelection{"rg": "15.2.0"}, spec.Tools)
	require.Equal(t, "15.3.0", spec.Platforms["linux/amd64"]["rg"])

	for _, invalid := range []string{
		"platforms:\n  linux-amd64: [shared-tool]\n",
		"platforms:\n  windows/amd64: [unknown]\n",
		"platforms:\n  windows/amd64: [shared-tool, shared-tool]\n",
	} {
		require.NoError(t, os.WriteFile(path, []byte("id: audit\ntools: [rg]\n"+invalid), 0644))
		_, err := LoadBundleSpec(path)
		require.Error(t, err)
	}
}

func TestPlatformOnlyCustomTool(t *testing.T) {
	spec := BundleSpec{ID: "custom", Platforms: map[string]ToolSelection{"linux/amd64": {"local": ""}},
		CustomTools: []registry.ToolEntry{{Name: "local", Version: "1.0.0", Repo: "example/local"}}}
	resolved, err := spec.resolved()
	require.NoError(t, err)
	require.Len(t, resolved.CustomTools, 1)
	require.Equal(t, "1.0.0", resolved.ToolsFor(Target{"linux", "amd64"})["local"])
	_, err = BuildBundle(context.Background(), spec, Target{"windows", "amd64"}, t.TempDir(), fixtureSource(nil))
	require.ErrorContains(t, err, "no tools")
}
