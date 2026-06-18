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
}

func newManifest(dir string) *Manifest {
	m := &Manifest{
		path:    filepath.Join(dir, "manifest.json"),
		entries: make(map[string]ManifestEntry),
	}
	m.load()
	return m
}

func (m *Manifest) Get(name string) (ManifestEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[name]
	return e, ok
}

func (m *Manifest) Set(name, version string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[name] = ManifestEntry{
		Version:     version,
		InstalledAt: time.Now(),
	}
	m.save()
}

func (m *Manifest) Delete(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, name)
	m.save()
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

func (m *Manifest) load() {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &m.entries)
}

func (m *Manifest) save() {
	_ = os.MkdirAll(filepath.Dir(m.path), 0o755)
	data, err := json.MarshalIndent(m.entries, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(m.path, data, 0o644)
}
