package pkg

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chainreactors/crtm/pkg/registry"
)

// Manager is the public API for crtm, importable as a library by aiscan.
//
// List/Search are fully offline — they read the embedded YAML registry
// plus user config. Only Install/Update hit the network (direct GitHub
// release download, no API pre-check).
type Manager struct {
	catalog    *Catalog
	manifest   *Manifest
	binPath    string
	configPath string
}

type ManagerOption struct {
	BinPath    string // default ~/.crtm/bin
	ConfigPath string // default ~/.crtm/config.yaml
}

// NewManager loads the tool registry (embedded + user config) and builds
// the catalog. No network calls.
func NewManager(opt ManagerOption) (*Manager, error) {
	if opt.ConfigPath == "" {
		opt.ConfigPath = DefaultConfigPath()
	}
	if opt.BinPath == "" {
		opt.BinPath = filepath.Join(DefaultConfigDir(), "bin")
	}

	embedded, err := registry.LoadEmbedded()
	if err != nil {
		return nil, fmt.Errorf("load embedded registry: %w", err)
	}

	cfg, err := LoadCRTMConfig(opt.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	all := registry.Merge(embedded, cfg.CustomTools)

	return &Manager{
		catalog:    NewCatalog(all),
		manifest:   newManifest(filepath.Dir(opt.ConfigPath)),
		binPath:    opt.BinPath,
		configPath: opt.ConfigPath,
	}, nil
}

// Catalog returns the tool registry catalog (offline, no network).
func (m *Manager) Catalog() *Catalog { return m.catalog }

// ListTools returns all tools from the registry.
func (m *Manager) ListTools() []registry.ToolEntry {
	return m.catalog.All()
}

// Search searches tools by query across name, tags, description.
func (m *Manager) Search(query string) []registry.ToolEntry {
	return m.catalog.Search(query)
}

// InstallTool downloads and installs a tool (latest version).
func (m *Manager) InstallTool(name string) error {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return fmt.Errorf("tool %q not found in registry", name)
	}
	if m.isInstalled(name) {
		return fmt.Errorf("%s: already installed", name)
	}
	version, err := m.downloadAndInstall(entry, "")
	if err != nil {
		return err
	}
	m.manifest.Set(name, version)
	return nil
}

// UpdateTool re-downloads the latest (or specified) version.
func (m *Manager) UpdateTool(name string) error {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return fmt.Errorf("tool %q not found in registry", name)
	}
	version, err := m.downloadAndInstall(entry, "")
	if err != nil {
		return err
	}
	m.manifest.Set(name, version)
	return nil
}

// RemoveTool deletes the binary and removes from manifest.
func (m *Manager) RemoveTool(name string) error {
	bin := m.binaryPath(name)
	if _, err := os.Stat(bin); os.IsNotExist(err) {
		return fmt.Errorf("%s: not installed", name)
	}
	if err := os.Remove(bin); err != nil {
		return err
	}
	m.manifest.Delete(name)
	return nil
}

// AddCustomTool registers a custom tool in the user config.
func (m *Manager) AddCustomTool(entry registry.ToolEntry) (bool, error) {
	added, err := AddCustomTool(m.configPath, entry)
	if err != nil || !added {
		return added, err
	}
	m.catalog.add(entry)
	return true, nil
}

// IsInstalled checks if the binary exists on disk.
func (m *Manager) IsInstalled(name string) bool {
	return m.isInstalled(name)
}

// InstalledVersion returns the version from manifest (instant, no exec).
// Returns "" if not installed.
func (m *Manager) InstalledVersion(name string) string {
	if !m.isInstalled(name) {
		return ""
	}
	if e, ok := m.manifest.Get(name); ok && e.Version != "" {
		return e.Version
	}
	return "installed"
}

// BinPath returns the binary installation directory.
func (m *Manager) BinPath() string { return m.binPath }

// downloadAndInstall wraps DownloadAndInstall and captures the resolved version.
func (m *Manager) downloadAndInstall(entry registry.ToolEntry, version string) (string, error) {
	resolved, err := ResolveVersionIfNeeded(entry, version)
	if err != nil {
		return "", err
	}

	if err := DownloadAndInstall(entry, resolved, m.binPath); err != nil {
		return "", err
	}

	if resolved != "" {
		return resolved, nil
	}
	return "latest", nil
}

func (m *Manager) isInstalled(name string) bool {
	_, err := os.Stat(m.binaryPath(name))
	return err == nil
}

func (m *Manager) binaryPath(name string) string {
	return filepath.Join(m.binPath, name)
}
