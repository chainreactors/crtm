package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chainreactors/crtm/pkg/registry"
)

// Manager is the public API for crtm, importable as a library by aiscan.
//
// List/Search are fully offline — they read the embedded YAML registry
// plus user config. Only Install/Update hit the network (direct GitHub
// release download, no API pre-check).
type Manager struct {
	catalog    *Catalog
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

// InstallTool downloads and installs a tool. This is the only method
// that hits the network — a single HTTP GET to GitHub releases.
func (m *Manager) InstallTool(name string) error {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return fmt.Errorf("tool %q not found in registry", name)
	}
	if m.isInstalled(name) {
		return fmt.Errorf("%s: already installed", name)
	}
	return DownloadAndInstall(entry, "", m.binPath)
}

// UpdateTool re-downloads the latest version of a tool.
func (m *Manager) UpdateTool(name string) error {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return fmt.Errorf("tool %q not found in registry", name)
	}
	return DownloadAndInstall(entry, "", m.binPath)
}

// RemoveTool deletes an installed tool binary.
func (m *Manager) RemoveTool(name string) error {
	bin := m.binaryPath(name)
	if _, err := os.Stat(bin); os.IsNotExist(err) {
		return fmt.Errorf("%s: not installed", name)
	}
	return os.Remove(bin)
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

// IsInstalled checks if a tool binary exists in the bin directory.
func (m *Manager) IsInstalled(name string) bool {
	return m.isInstalled(name)
}

// BinPath returns the binary installation directory.
func (m *Manager) BinPath() string { return m.binPath }

// InstalledVersion runs the binary with common version flags and extracts
// a semver-like string. Returns "" if not installed or version undetectable.
// Purely local — no network.
func (m *Manager) InstalledVersion(name string) string {
	bin := m.binaryPath(name)
	if _, err := os.Stat(bin); err != nil {
		return ""
	}
	for _, flag := range []string{"-version", "-v", "--version", "version"} {
		out, err := exec.Command(bin, flag).CombinedOutput()
		if err != nil && len(out) == 0 {
			continue
		}
		if v := extractVersion(string(out)); v != "" {
			return v
		}
	}
	return "installed"
}

// versionRe matches semver-like patterns but skips dates (YYYY-MM-DD, HH:MM.SS).
var versionRe = regexp.MustCompile(`(?:^|[\s/v])(\d{1,3}\.\d{1,3}(?:\.\d{1,3})?)(?:\s|$|[),\]])`)

func extractVersion(s string) string {
	for _, line := range strings.Split(s, "\n") {
		// Skip lines that look like timestamps or dates.
		if strings.Contains(line, ":") && strings.Count(line, "-") >= 2 {
			continue
		}
		if m := versionRe.FindStringSubmatch(line); len(m) > 1 {
			v := m[1]
			parts := strings.Split(v, ".")
			// Reject if first segment > 100 (likely not a version).
			if len(parts) > 0 && len(parts[0]) > 2 {
				continue
			}
			return v
		}
	}
	return ""
}

func (m *Manager) isInstalled(name string) bool {
	_, err := os.Stat(m.binaryPath(name))
	return err == nil
}

func (m *Manager) binaryPath(name string) string {
	return filepath.Join(m.binPath, name)
}
