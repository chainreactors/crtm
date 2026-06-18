package pkg

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/chainreactors/crtm/pkg/registry"
)

// ---------------------------------------------------------------------------
// Release info types (for agent-facing APIs)
// ---------------------------------------------------------------------------

// ReleaseInfo describes a GitHub release tag.
type ReleaseInfo struct {
	Version string `json:"version"`
	Tag     string `json:"tag"`
	URL     string `json:"url"` // release page URL
}

// AssetInfo describes a downloadable asset in a release.
type AssetInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size,omitempty"`
}

// ToolInfo combines registry metadata with runtime state.
type ToolInfo struct {
	registry.ToolEntry
	Installed        bool   `json:"installed"`
	InstalledPath    string `json:"installed_path,omitempty"`
	LatestVersion    string `json:"latest_version,omitempty"`
	LatestVersionErr string `json:"latest_version_err,omitempty"`
}

// ---------------------------------------------------------------------------
// Manager API — atomic operations for agent mode
// ---------------------------------------------------------------------------

// GetToolInfo returns metadata + install status + latest version (lazy resolve).
func (m *Manager) GetToolInfo(name string) (ToolInfo, error) {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return ToolInfo{}, fmt.Errorf("tool %q not found in registry", name)
	}

	info := ToolInfo{
		ToolEntry: entry,
		Installed: m.isInstalled(name),
	}
	if info.Installed {
		info.InstalledPath = m.binaryPath(name)
	}

	ver, err := ResolveLatestVersion(entry.Repo)
	if err != nil {
		info.LatestVersionErr = err.Error()
	} else {
		info.LatestVersion = ver
	}
	return info, nil
}

// ListReleases returns the latest release tag for a tool. Uses a single
// HEAD request per call, no GitHub API.
// Returns a single-element slice (latest only — expanding to full history
// would require scraping or API).
func (m *Manager) ListReleases(name string) ([]ReleaseInfo, error) {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return nil, fmt.Errorf("tool %q not found in registry", name)
	}

	version, err := ResolveLatestVersion(entry.Repo)
	if err != nil {
		return nil, err
	}
	tag := "v" + version
	return []ReleaseInfo{{
		Version: version,
		Tag:     tag,
		URL:     fmt.Sprintf("https://github.com/%s/releases/tag/%s", entry.Repo, tag),
	}}, nil
}

// ListAssets returns the downloadable assets for a tool's release.
// Probes common asset name patterns via HEAD requests. No API call.
func (m *Manager) ListAssets(name, version string) ([]AssetInfo, error) {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return nil, fmt.Errorf("tool %q not found in registry", name)
	}

	version, err := ResolveVersionIfNeeded(entry, version)
	if err != nil {
		return nil, err
	}

	var assets []AssetInfo
	candidates := buildAssetCandidates(entry, version)
	for _, c := range candidates {
		if URLOK(c.URL) {
			assets = append(assets, c)
		}
	}
	return assets, nil
}

// InstallVersion downloads and installs a specific version of a tool.
func (m *Manager) InstallVersion(name, version string) error {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return fmt.Errorf("tool %q not found in registry", name)
	}
	return DownloadAndInstall(entry, version, m.binPath)
}

// DownloadTo downloads a tool to a custom directory (not the default bin).
// Returns the path to the installed binary.
func (m *Manager) DownloadTo(name, version, destDir string) (string, error) {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return "", fmt.Errorf("tool %q not found in registry", name)
	}
	if err := DownloadAndInstall(entry, version, destDir); err != nil {
		return "", err
	}
	binName := name
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	return filepath.Join(destDir, binName), nil
}

// InstallFromRepo downloads and installs a tool from an arbitrary GitHub
// repo without pre-registering it. The caller provides the full specification.
// This is the most flexible API for agent use.
func (m *Manager) InstallFromRepo(repo, assetPattern, version string) (string, error) {
	name := repoBaseName(repo)
	entry := registry.ToolEntry{
		Name:         name,
		Repo:         repo,
		AssetPattern: assetPattern,
	}
	if err := DownloadAndInstall(entry, version, m.binPath); err != nil {
		return "", err
	}
	return m.binaryPath(name), nil
}

// ResolveVersion exposes version resolution for a registered tool.
func (m *Manager) ResolveVersion(name string) (string, error) {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return "", fmt.Errorf("tool %q not found in registry", name)
	}
	return ResolveLatestVersion(entry.Repo)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func buildAssetCandidates(entry registry.ToolEntry, version string) []AssetInfo {
	base := entry.DownloadURL(version)
	var candidates []AssetInfo

	if hasArchiveExt(entry.AssetPattern) {
		candidates = append(candidates, AssetInfo{Name: entry.AssetName(version), URL: base})
		return candidates
	}

	for _, suffix := range []string{"", ".tar.gz", ".zip"} {
		name := entry.AssetName(version) + suffix
		candidates = append(candidates, AssetInfo{Name: name, URL: base + suffix})
	}
	return candidates
}

func repoBaseName(repo string) string {
	if i := strings.LastIndexByte(repo, '/'); i >= 0 {
		return repo[i+1:]
	}
	return repo
}
