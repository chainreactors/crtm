package registry

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogRejectsAmbiguousDefinitions(t *testing.T) {
	for _, data := range []string{
		"- name: tool\n  asset_patern: example.zip\n",
		"- name: tool\n- name: TOOL\n",
		"- repo: example/unnamed\n",
	} {
		_, err := ParseYAML([]byte(data))
		require.Error(t, err)
	}
}

func TestLoadEmbedded(t *testing.T) {
	entries, err := LoadEmbedded()
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	names := map[string]bool{}
	for _, e := range entries {
		require.False(t, names[e.Name], "duplicate tool %s", e.Name)
		names[e.Name] = true
		require.NotEmpty(t, e.Version, "tool %s needs a default bundle version", e.Name)
		require.NotEmpty(t, e.Repo, "tool %s must have repo", e.Name)
		require.NotEmpty(t, e.AssetPattern, "tool %s must have asset_pattern", e.Name)
		require.Contains(t, e.Repo, "/", "repo %s must be org/name format", e.Repo)
	}

	// Chainreactors core tools present.
	require.True(t, names["gogo"])
	require.True(t, names["spray"])
	require.True(t, names["zombie"])

	// PD core tools present.
	require.True(t, names["nuclei"])
	require.True(t, names["httpx"])
	require.True(t, names["subfinder"])
}

func TestArsenalReleaseAssetNames(t *testing.T) {
	entries, err := LoadEmbedded()
	require.NoError(t, err)
	byName := map[string]ToolEntry{}
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	for _, test := range []struct{ name, goos, arch, asset string }{
		{"gogo", "windows", "amd64", "gogo_windows_amd64.exe"},
		{"spray", "darwin", "arm64", "spray_darwin_arm64"},
		{"iom", "linux", "amd64", "iom_linux_amd64"},
		{"urlfounder", "windows", "amd64", "urlfounder_1.2.3_windows_amd64.exe"},
		{"urlfounder", "linux", "arm", "urlfounder_1.2.3_linux_armv6"},
		{"nuclei", "linux", "amd64", "nuclei_1.2.3_linux_amd64.zip"},
		{"subfinder", "windows", "arm64", "subfinder_1.2.3_windows_arm64.zip"},
	} {
		asset, _, err := byName[test.name].AssetFor("1.2.3", test.goos, test.arch)
		require.NoError(t, err)
		require.Equal(t, test.asset, asset)
	}
	for _, name := range []string{"gogo", "spray", "nuclei", "httpx"} {
		_, _, err := byName[name].AssetFor("1.2.3", "windows", "arm64")
		require.ErrorContains(t, err, "unsupported platform")
	}
}

func TestToolEntryOrgRepo(t *testing.T) {
	e := ToolEntry{Name: "nuclei", Repo: "projectdiscovery/nuclei"}
	require.Equal(t, "projectdiscovery", e.Org())
	require.Equal(t, "nuclei", e.RepoName())

	e2 := ToolEntry{Name: "gogo", Repo: "chainreactors/gogo"}
	require.Equal(t, "chainreactors", e2.Org())
	require.Equal(t, "gogo", e2.RepoName())
}

func TestAssetNameCR(t *testing.T) {
	e := ToolEntry{Name: "gogo", Repo: "chainreactors/gogo", AssetPattern: "{name}_{os}_{arch}"}
	name := e.AssetName("")
	require.NotContains(t, name, "{")
	require.Contains(t, name, "gogo")
	require.Contains(t, name, runtime.GOARCH)
}

func TestAssetNamePD(t *testing.T) {
	e := ToolEntry{Name: "nuclei", Repo: "projectdiscovery/nuclei", AssetPattern: "{name}_{version}_{os}_{arch}.zip"}
	name := e.AssetName("3.9.0")
	require.Equal(t, true, len(name) > 10)
	require.Contains(t, name, "nuclei")
	require.Contains(t, name, "3.9.0")
	require.Contains(t, name, ".zip")
}

func TestAssetNameNoVersion(t *testing.T) {
	e := ToolEntry{Name: "nuclei", Repo: "projectdiscovery/nuclei", AssetPattern: "{name}_{version}_{os}_{arch}.zip"}
	name := e.AssetName("")
	// Empty version should not leave double underscores.
	require.NotContains(t, name, "__")
}

func TestDownloadURLLatest(t *testing.T) {
	e := ToolEntry{Name: "gogo", Repo: "chainreactors/gogo", AssetPattern: "{name}_{os}_{arch}"}
	url := e.DownloadURL("")
	require.Contains(t, url, "/releases/latest/download/")
	require.Contains(t, url, "chainreactors/gogo")
}

func TestDownloadURLPinned(t *testing.T) {
	e := ToolEntry{Name: "nuclei", Repo: "projectdiscovery/nuclei", AssetPattern: "{name}_{version}_{os}_{arch}.zip"}
	url := e.DownloadURL("3.9.0")
	require.Contains(t, url, "/releases/download/v3.9.0/")
	require.Contains(t, url, "nuclei")
}

func TestMerge(t *testing.T) {
	builtIn := []ToolEntry{
		{Name: "gogo", Repo: "chainreactors/gogo", Description: "original"},
		{Name: "spray", Repo: "chainreactors/spray"},
	}
	user := []ToolEntry{
		{Name: "gogo", Repo: "chainreactors/gogo", Description: "user override"},
		{Name: "ffuf", Repo: "ffuf/ffuf"},
	}
	merged := Merge(builtIn, user)
	require.Len(t, merged, 3) // gogo (overridden) + spray + ffuf

	for _, e := range merged {
		if e.Name == "gogo" {
			require.Equal(t, "user override", e.Description, "user should override built-in")
		}
	}
}

func TestParseYAML(t *testing.T) {
	data := []byte(`
- name: mytool
  repo: myorg/mytool
  asset_pattern: "{name}_{os}_{arch}"
  tags: [scanner]
`)
	entries, err := ParseYAML(data)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "mytool", entries[0].Name)
	require.Equal(t, "myorg/mytool", entries[0].Repo)
}

func TestAuditPlatformMatrix(t *testing.T) {
	entries, err := LoadEmbedded()
	require.NoError(t, err)
	for _, e := range entries {
		if e.Category != "audit" {
			continue
		}
		for _, goos := range []string{"windows", "linux", "darwin"} {
			for _, arch := range []string{"amd64", "arm64"} {
				asset, executable, err := e.AssetFor("1.2.3", goos, arch)
				require.NoError(t, err)
				require.NotContains(t, asset, "{")
				require.NotEmpty(t, executable)
				if goos == "windows" {
					require.Contains(t, executable, ".exe")
				}
				tag := e.ReleaseTag("1.2.3")
				if e.Name == "osv-scanner" {
					require.Equal(t, "v1.2.3", tag)
				} else {
					require.Equal(t, "1.2.3", tag)
				}
				u, err := e.ReleaseURL(tag, "1.2.3", goos, arch)
				require.NoError(t, err)
				require.Contains(t, u, "/"+tag+"/"+asset)
			}
		}
		_, _, err := e.AssetFor("1.2.3", "freebsd", "amd64")
		require.Error(t, err)
	}
}
