package pkg

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// LoadBundleSpec resolves a selection against its catalog, relative to the YAML
// file. Without a catalog it uses CRTM defaults. Only selected definitions remain.
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
		spec.Definitions = registry.Merge(entries, spec.Definitions)
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
	opt := ManagerOption{Catalog: s.Definitions, Sources: []Source{GitHubSource{}}}
	if bundle != nil {
		opt.Sources = append([]Source{bundle}, opt.Sources...)
	}
	return opt
}

// ToolsFor returns the common tools plus this platform's additions. Platform
// versions override common versions; the original selection is never mutated.
func (s BundleSpec) ToolsFor(target Target) ToolSelection {
	tools := make(ToolSelection, len(s.Tools)+len(s.Platforms[target.String()]))
	for name, version := range s.Tools {
		tools[name] = version
	}
	for name, version := range s.Platforms[target.String()] {
		tools[name] = version
	}
	return tools
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
	if !validToolName(s.ID) || len(s.Tools) == 0 && len(s.Platforms) == 0 {
		return BundleSpec{}, fmt.Errorf("bundle requires an ID and at least one tool")
	}
	entries := s.Definitions
	var err error
	if entries == nil {
		entries, err = registry.LoadEmbedded()
		if err != nil {
			return BundleSpec{}, err
		}
	}
	catalog := NewCatalog(entries)
	selected := map[string]bool{}
	resolveSelection := func(selection ToolSelection) (ToolSelection, error) {
		versions := make(ToolSelection, len(selection))
		for name, version := range selection {
			entry, ok := catalog.Find(name)
			if !ok || entry.Name != name || !validToolName(name) {
				return nil, fmt.Errorf("invalid or unknown tool %q", name)
			}
			if version == "" {
				version = entry.Version
			}
			versions[name] = version
			selected[name] = true
		}
		return versions, nil
	}
	if s.Tools, err = resolveSelection(s.Tools); err != nil {
		return BundleSpec{}, err
	}
	if s.Platforms != nil {
		platforms := make(map[string]ToolSelection, len(s.Platforms))
		for platform, tools := range s.Platforms {
			goos, goarch, _ := strings.Cut(platform, "/")
			if err := (Target{goos, goarch}).validate(); err != nil {
				return BundleSpec{}, err
			}
			platforms[platform], err = resolveSelection(tools)
			if err != nil {
				return BundleSpec{}, fmt.Errorf("%s: %w", platform, err)
			}
		}
		s.Platforms = platforms
	}
	s.Definitions = nil
	for _, entry := range entries {
		if selected[entry.Name] {
			s.Definitions = append(s.Definitions, entry)
		}
	}
	return s, nil
}
