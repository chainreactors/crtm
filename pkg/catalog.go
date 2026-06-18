package pkg

import (
	"strings"

	"github.com/chainreactors/crtm/pkg/registry"
)

// Catalog indexes tool entries from the YAML registry and provides search.
type Catalog struct {
	entries []registry.ToolEntry
	byName  map[string]int // lowercase name → index
}

func NewCatalog(entries []registry.ToolEntry) *Catalog {
	byName := make(map[string]int, len(entries))
	for i, e := range entries {
		key := strings.ToLower(e.Name)
		if _, exists := byName[key]; !exists {
			byName[key] = i
		}
	}
	return &Catalog{entries: entries, byName: byName}
}

func (c *Catalog) All() []registry.ToolEntry {
	return c.entries
}

func (c *Catalog) Find(name string) (registry.ToolEntry, bool) {
	if i, ok := c.byName[strings.ToLower(name)]; ok {
		return c.entries[i], true
	}
	return registry.ToolEntry{}, false
}

func (c *Catalog) Search(query string) []registry.ToolEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return c.entries
	}
	keywords := strings.Fields(q)
	var results []registry.ToolEntry
	for _, e := range c.entries {
		if matchEntry(e, keywords) {
			results = append(results, e)
		}
	}
	return results
}

func (c *Catalog) add(entry registry.ToolEntry) {
	key := strings.ToLower(entry.Name)
	if _, exists := c.byName[key]; exists {
		return
	}
	c.byName[key] = len(c.entries)
	c.entries = append(c.entries, entry)
}

func matchEntry(e registry.ToolEntry, keywords []string) bool {
	for _, kw := range keywords {
		if !matchOneEntry(e, kw) {
			return false
		}
	}
	return true
}

func matchOneEntry(e registry.ToolEntry, kw string) bool {
	if strings.Contains(strings.ToLower(e.Name), kw) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Description), kw) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Category), kw) {
		return true
	}
	for _, tag := range e.Tags {
		if strings.Contains(strings.ToLower(tag), kw) {
			return true
		}
	}
	return false
}
