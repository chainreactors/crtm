package pkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Manifest tracks installed tools with version and timestamp,
// avoiding slow exec-based version detection on every list call.
type Manifest struct {
	mu      sync.RWMutex
	path    string
	entries map[string]ManifestEntry
}

type ManifestEntry struct {
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installed_at"`
	Source      string    `json:"source,omitempty"`
	ManagedBy   string    `json:"managed_by,omitempty"`
	SHA256      string    `json:"sha256,omitempty"`
}

func newManifest(dir string) (*Manifest, error) {
	m := &Manifest{
		path:    filepath.Join(dir, "manifest.json"),
		entries: make(map[string]ManifestEntry),
	}
	return m, m.load()
}

func (m *Manifest) Get(name string) (ManifestEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[name]
	return e, ok
}

func (m *Manifest) Set(name, version string) error {
	return m.put(name, ManifestEntry{Version: version, InstalledAt: time.Now()})
}

func (m *Manifest) put(name string, entry ManifestEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, existed := m.entries[name]
	m.entries[name] = entry
	if err := m.save(); err != nil {
		if existed {
			m.entries[name] = old
		} else {
			delete(m.entries, name)
		}
		return err
	}
	return nil
}

func (m *Manifest) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, existed := m.entries[name]
	delete(m.entries, name)
	if err := m.save(); err != nil {
		if existed {
			m.entries[name] = old
		}
		return err
	}
	return nil
}

func (m *Manifest) All() map[string]ManifestEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]ManifestEntry, len(m.entries))
	for k, v := range m.entries {
		out[k] = v
	}
	return out
}

func (m *Manifest) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		m.entries = make(map[string]ManifestEntry)
		return nil
	}
	if err != nil {
		return err
	}
	entries := make(map[string]ManifestEntry)
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	if entries == nil {
		entries = make(map[string]ManifestEntry)
	}
	m.entries = entries
	return nil
}

func (m *Manifest) save() error {
	data, err := json.MarshalIndent(m.entries, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(m.path, data, 0o644)
}
