package pkg

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chainreactors/crtm/pkg/registry"
	"gopkg.in/yaml.v3"
)

// LoadBundleSpec resolves a release's selection against an optional shared
// catalog. Catalog paths are relative to the selection file, never the cwd.
// Both files use BundleSpec; the catalog supplies versions and tool definitions,
// while the release owns its ID and the set of tools to include.
func LoadBundleSpec(path string) (BundleSpec, error) {
	spec, err := readBundleSpec(path)
	if err != nil || spec.Catalog == "" {
		return spec, err
	}
	catalogPath := spec.Catalog
	if !filepath.IsAbs(catalogPath) {
		catalogPath = filepath.Join(filepath.Dir(path), catalogPath)
	}
	catalog, err := readBundleSpec(catalogPath)
	if err != nil {
		return BundleSpec{}, err
	}
	if catalog.Catalog != "" {
		return BundleSpec{}, fmt.Errorf("%s: catalogs cannot include another catalog", catalogPath)
	}
	for name, version := range spec.Tools {
		defaultVersion, ok := catalog.Tools[name]
		if !ok {
			return BundleSpec{}, fmt.Errorf("%s: tool %q not found in catalog %s", path, name, catalogPath)
		}
		if version == "" {
			spec.Tools[name] = defaultVersion
		}
	}
	definitions := registry.Merge(catalog.CustomTools, spec.CustomTools)
	spec.CustomTools = nil
	for _, entry := range definitions {
		if _, selected := spec.Tools[entry.Name]; selected {
			spec.CustomTools = append(spec.CustomTools, entry)
		}
	}
	spec.Catalog = ""
	return spec, nil
}

func readBundleSpec(path string) (BundleSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BundleSpec{}, err
	}
	var spec BundleSpec
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&spec); err != nil {
		return BundleSpec{}, fmt.Errorf("%s: %w", path, err)
	}
	return spec, nil
}

// ManagerOption uses the same selected definitions for remote and embedded
// installations. The caller supplies only installation paths.
func (s BundleSpec) ManagerOption(bundle *Bundle) ManagerOption {
	opt := ManagerOption{Tools: s.CustomTools, Sources: []Source{GitHubSource{}}}
	if bundle != nil {
		opt.Sources = append([]Source{bundle}, opt.Sources...)
	}
	return opt
}

// Validate checks the selection before a generator writes metadata or downloads.
func (s BundleSpec) Validate() error {
	if s.Catalog != "" {
		return fmt.Errorf("unresolved catalog: use LoadBundleSpec")
	}
	if !validToolName(s.ID) || len(s.Tools) == 0 {
		return fmt.Errorf("bundle requires an ID and at least one tool")
	}
	entries, err := registry.LoadEmbedded()
	if err != nil {
		return err
	}
	catalog := NewCatalog(registry.Merge(entries, s.CustomTools))
	for name := range s.Tools {
		entry, ok := catalog.Find(name)
		if !ok || entry.Name != name || !validToolName(name) {
			return fmt.Errorf("invalid or unknown tool %q", name)
		}
	}
	return nil
}
