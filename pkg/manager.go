package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/chainreactors/crtm/pkg/registry"
)

// Manager is the public API for crtm, importable as a library by aiscan.
//
// List/Search are offline. Install resolves through the configured sources;
// Prepare only reads its bundle. Update explicitly requests the latest release.
type Manager struct {
	catalog    *Catalog
	manifest   *Manifest
	binPath    string
	configPath string
	sources    []Source
}

type ManagerOption struct {
	BinPath    string               // default ~/.crtm/bin
	ConfigPath string               // default ~/.crtm/config.yaml
	Tools      []registry.ToolEntry // distribution definitions; user config overrides these
	// nil uses GitHub. A non-nil list is the complete ordered source chain.
	Sources []Source
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

	all := registry.Merge(registry.Merge(embedded, opt.Tools), cfg.CustomTools)
	sources := opt.Sources
	if sources == nil {
		sources = []Source{GitHubSource{}}
	}
	for _, source := range sources {
		if bundle, ok := source.(*Bundle); ok {
			if err := mergeBundle(&all, bundle); err != nil {
				return nil, err
			}
		}
	}
	manifest, err := newManifest(filepath.Dir(opt.ConfigPath))
	if err != nil {
		return nil, fmt.Errorf("load installation manifest: %w", err)
	}

	return &Manager{
		catalog:    NewCatalog(all),
		manifest:   manifest,
		binPath:    opt.BinPath,
		configPath: opt.ConfigPath,
		sources:    append([]Source(nil), sources...),
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

// InstallTool installs from the first matching source (bundled or remote latest).
func (m *Manager) InstallTool(name string) error {
	return m.install(context.Background(), name, "", true, nil)
}

// UpdateTool re-downloads the latest (or specified) version.
func (m *Manager) UpdateTool(name string) error {
	return m.install(context.Background(), name, "latest", false, nil)
}

// RemoveTool deletes the binary and removes from manifest.
func (m *Manager) RemoveTool(name string) error {
	if !validToolName(name) {
		return fmt.Errorf("invalid tool name %q", name)
	}
	if entry, ok := m.catalog.Find(name); ok {
		name = entry.Name
	}
	return m.withInstallLock(context.Background(), func() error {
		bin := m.binaryPath(name)
		if _, err := os.Stat(bin); os.IsNotExist(err) {
			return fmt.Errorf("%s: not installed", name)
		}
		if err := os.Remove(bin); err != nil {
			return err
		}
		return m.manifest.Delete(name)
	})
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
	if entry, ok := m.catalog.Find(name); ok {
		name = entry.Name
	}
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

// InstallVersionContext installs a pinned release after validating the staged executable.
// The existing binary is untouched if download, extraction or validation fails.
func (m *Manager) InstallVersionContext(ctx context.Context, name, version string, validate func(context.Context, string) error) error {
	if version == "" {
		return fmt.Errorf("a pinned version is required")
	}
	return m.install(ctx, name, version, false, validate)
}

func (m *Manager) install(ctx context.Context, name, version string, onlyMissing bool, validate func(context.Context, string) error) error {
	entry, ok := m.catalog.Find(name)
	if !ok {
		return fmt.Errorf("tool %q not found in registry", name)
	}
	return m.withInstallLock(ctx, func() error {
		if onlyMissing && m.isInstalled(entry.Name) {
			return fmt.Errorf("%s: already installed", entry.Name)
		}
		a, err := resolve(ctx, m.sources, Request{entry, version, CurrentTarget()})
		if err != nil {
			return err
		}
		return m.installArtifact(ctx, a, "", validate)
	})
}

func (m *Manager) withInstallLock(ctx context.Context, work func() error) error {
	if err := os.MkdirAll(filepath.Dir(m.manifest.path), 0755); err != nil {
		return err
	}
	release, err := lockDirectory(ctx, m.binPath)
	if err != nil {
		return err
	}
	defer release()
	// Managers and processes may have been created before the last installation.
	if err := m.manifest.load(); err != nil {
		return err
	}
	return work()
}

func (m *Manager) installArtifact(ctx context.Context, a Artifact, managedBy string, validate func(context.Context, string) error) error {
	staged, digest, err := stageArtifact(ctx, a, m.binPath, validate)
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	if err := replaceBinary(staged, m.binaryPath(a.Tool.Name)); err != nil {
		return err
	}
	return m.manifest.put(a.Tool.Name, ManifestEntry{Version: a.Version, InstalledAt: time.Now(), Source: a.Source, ManagedBy: managedBy, SHA256: digest})
}

// Prepare restores missing tools and follows bundle revisions only while the
// previous installation is still owned by this bundle and has not been edited.
// It never consults other sources or the network.
func (m *Manager) Prepare(ctx context.Context, bundle *Bundle) error {
	if bundle == nil {
		return nil
	}
	if bundle.manifest.Target != CurrentTarget() {
		return fmt.Errorf("bundle targets %s, running %s", bundle.manifest.Target, CurrentTarget())
	}
	return m.withInstallLock(ctx, func() error {
		entries := append([]registry.ToolEntry(nil), m.catalog.All()...)
		if err := mergeBundle(&entries, bundle); err != nil {
			return err
		}
		m.catalog = NewCatalog(entries)
		for _, tool := range bundle.manifest.Tools {
			if err := ctx.Err(); err != nil {
				return err
			}
			path := m.binaryPath(tool.Tool.Name)
			stat, err := os.Lstat(path)
			if err == nil {
				previous, ok := m.manifest.Get(tool.Tool.Name)
				if !ok || previous.ManagedBy != bundle.manifest.ID || !stat.Mode().IsRegular() {
					continue
				}
				digest, err := fileDigest(ctx, path)
				if err != nil {
					return err
				}
				if digest != previous.SHA256 {
					previous.ManagedBy = ""
					if err := m.manifest.put(tool.Tool.Name, previous); err != nil {
						return err
					}
					continue
				}
				if strings.EqualFold(digest, tool.SHA256) && previous.Version == tool.Version {
					continue
				}
			} else if !os.IsNotExist(err) {
				return err
			}
			a, err := bundle.Resolve(ctx, Request{tool.Tool, tool.Version, CurrentTarget()})
			if err != nil {
				return err
			}
			if err := m.installArtifact(ctx, a, bundle.manifest.ID, nil); err != nil {
				return fmt.Errorf("prepare %s: %w", tool.Tool.Name, err)
			}
		}
		return nil
	})
}

func mergeBundle(entries *[]registry.ToolEntry, bundle *Bundle) error {
	if bundle == nil {
		return fmt.Errorf("nil bundle source")
	}
	for _, tool := range bundle.manifest.Tools {
		found := false
		for _, entry := range *entries {
			if !strings.EqualFold(entry.Name, tool.Tool.Name) {
				continue
			}
			// Documentation may evolve independently; executable identity may not.
			if entry.Name != tool.Tool.Name || entry.Repo != tool.Tool.Repo || entry.AssetPattern != tool.Tool.AssetPattern || entry.Executable != tool.Tool.Executable || entry.TagPattern != tool.Tool.TagPattern || !reflect.DeepEqual(entry.Platforms, tool.Tool.Platforms) {
				return fmt.Errorf("bundle tool %q conflicts with registry definition", entry.Name)
			}
			found = true
			break
		}
		if !found {
			*entries = append(*entries, tool.Tool)
		}
	}
	return nil
}

func (m *Manager) isInstalled(name string) bool {
	if entry, ok := m.catalog.Find(name); ok {
		name = entry.Name
	}
	if !validToolName(name) {
		return false
	}
	info, err := os.Stat(m.binaryPath(name))
	return err == nil && info.Mode().IsRegular()
}

func (m *Manager) binaryPath(name string) string {
	return filepath.Join(m.binPath, BinaryName(name))
}
