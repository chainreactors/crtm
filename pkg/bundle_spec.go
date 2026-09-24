package pkg

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chainreactors/crtm/pkg/registry"
	"gopkg.in/yaml.v3"
)

// ToolSelection accepts a list of names, or a name/version map for overrides.
type ToolSelection map[string]string

func (s *ToolSelection) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		type versions ToolSelection
		return node.Decode((*versions)(s))
	}
	var names []string
	if err := node.Decode(&names); err != nil {
		return err
	}
	*s = make(ToolSelection, len(names))
	for _, name := range names {
		if _, duplicate := (*s)[name]; duplicate {
			return fmt.Errorf("duplicate tool %q", name)
		}
		(*s)[name] = ""
	}
	return nil
}

// LoadBundleSpec selects tools and versions from Arsenal's embedded catalog.
// An optional catalog file uses the same ToolEntry YAML format and is resolved
// relative to the selection file. Only selected custom definitions are retained.
func LoadBundleSpec(path string) (BundleSpec, error) {
	spec, err := readBundleSpec(path)
	if err != nil {
		return BundleSpec{}, err
	}
	if spec.Catalog != "" {
		catalogPath := spec.Catalog
		if !filepath.IsAbs(catalogPath) {
			catalogPath = filepath.Join(filepath.Dir(path), catalogPath)
		}
		data, err := os.ReadFile(catalogPath)
		if err != nil {
			return BundleSpec{}, err
		}
		entries, err := registry.ParseYAML(data)
		if err != nil {
			return BundleSpec{}, fmt.Errorf("%s: %w", catalogPath, err)
		}
		spec.CustomTools = registry.Merge(entries, spec.CustomTools)
		spec.Catalog = ""
	}
	return spec.resolved()
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
	_, err := s.resolved()
	return err
}

func (s BundleSpec) resolved() (BundleSpec, error) {
	if s.Catalog != "" {
		return BundleSpec{}, fmt.Errorf("unresolved catalog: use LoadBundleSpec")
	}
	if !validToolName(s.ID) || len(s.Tools) == 0 {
		return BundleSpec{}, fmt.Errorf("bundle requires an ID and at least one tool")
	}
	entries, err := registry.LoadEmbedded()
	if err != nil {
		return BundleSpec{}, err
	}
	catalog := NewCatalog(registry.Merge(entries, s.CustomTools))
	versions := make(ToolSelection, len(s.Tools))
	for name, version := range s.Tools {
		entry, ok := catalog.Find(name)
		if !ok || entry.Name != name || !validToolName(name) {
			return BundleSpec{}, fmt.Errorf("invalid or unknown tool %q", name)
		}
		if version == "" {
			version = entry.Version
		}
		versions[name] = version
	}
	s.Tools = versions
	definitions := s.CustomTools
	s.CustomTools = nil
	for _, entry := range definitions {
		if _, selected := s.Tools[entry.Name]; selected {
			s.CustomTools = append(s.CustomTools, entry)
		}
	}
	return s, nil
}
