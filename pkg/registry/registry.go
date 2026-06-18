package registry

import (
	"embed"
	"fmt"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed *.yaml
var embedded embed.FS

// ToolEntry is the declarative definition of a tool in the YAML registry.
// No version — version is resolved lazily at install time.
type ToolEntry struct {
	Name         string   `yaml:"name" json:"name"`
	Repo         string   `yaml:"repo" json:"repo"`               // "org/repo" format
	AssetPattern string   `yaml:"asset_pattern" json:"asset_pattern"` // e.g. "{name}_{version}_{os}_{arch}.zip"
	Description  string   `yaml:"description,omitempty" json:"description,omitempty"`
	Tags         []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Category     string   `yaml:"category,omitempty" json:"category,omitempty"`
}

// Org returns the GitHub organization from the Repo field.
func (e ToolEntry) Org() string {
	if i := strings.IndexByte(e.Repo, '/'); i >= 0 {
		return e.Repo[:i]
	}
	return ""
}

// RepoName returns the GitHub repository name from the Repo field.
func (e ToolEntry) RepoName() string {
	if i := strings.IndexByte(e.Repo, '/'); i >= 0 {
		return e.Repo[i+1:]
	}
	return e.Repo
}

// AssetName builds the expected release asset filename for the current
// platform. The pattern supports placeholders: {name}, {version}, {os}, {arch}.
// If version is empty, the "_{version}" segment is omitted (for latest-only patterns).
func (e ToolEntry) AssetName(version string) string {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macOS"
	}
	arch := runtime.GOARCH

	r := strings.NewReplacer(
		"{name}", e.Name,
		"{version}", version,
		"{os}", osName,
		"{arch}", arch,
	)
	result := r.Replace(e.AssetPattern)

	// Clean up double separators if version was empty.
	result = strings.ReplaceAll(result, "__", "_")
	return result
}

// DownloadURL returns the direct GitHub release download URL.
// If version is empty, uses /releases/latest/download/ which auto-resolves.
func (e ToolEntry) DownloadURL(version string) string {
	asset := e.AssetName(version)
	if version == "" {
		return fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", e.Repo, asset)
	}
	tag := version
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", e.Repo, tag, asset)
}

// LoadEmbedded loads the built-in CR + PD tool registries.
func LoadEmbedded() ([]ToolEntry, error) {
	var all []ToolEntry
	for _, name := range []string{"chainreactors.yaml", "projectdiscovery.yaml"} {
		data, err := embedded.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", name, err)
		}
		var entries []ToolEntry
		if err := yaml.Unmarshal(data, &entries); err != nil {
			return nil, fmt.Errorf("parse embedded %s: %w", name, err)
		}
		all = append(all, entries...)
	}
	return all, nil
}

// ParseYAML parses a YAML file containing a list of ToolEntry.
func ParseYAML(data []byte) ([]ToolEntry, error) {
	var entries []ToolEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// RenderToolList produces a formatted text summary of all entries,
// grouped by org, suitable for embedding in agent skill prompts.
func RenderToolList(entries []ToolEntry) string {
	groups := make(map[string][]ToolEntry)
	var order []string
	for _, e := range entries {
		org := e.Org()
		if _, seen := groups[org]; !seen {
			order = append(order, org)
		}
		groups[org] = append(groups[org], e)
	}

	var sb strings.Builder
	for _, org := range order {
		sb.WriteString("### ")
		sb.WriteString(org)
		sb.WriteString("\n\n")
		for _, e := range groups[org] {
			sb.WriteString("- **")
			sb.WriteString(e.Name)
			sb.WriteString("**")
			if e.Description != "" {
				sb.WriteString(" — ")
				sb.WriteString(e.Description)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// Merge combines built-in and user-defined entries. User entries override
// built-in ones with the same name.
func Merge(builtIn, user []ToolEntry) []ToolEntry {
	byName := make(map[string]int, len(builtIn))
	result := make([]ToolEntry, len(builtIn))
	copy(result, builtIn)
	for i, e := range result {
		byName[strings.ToLower(e.Name)] = i
	}
	for _, e := range user {
		key := strings.ToLower(e.Name)
		if idx, exists := byName[key]; exists {
			result[idx] = e // user overrides built-in
		} else {
			byName[key] = len(result)
			result = append(result, e)
		}
	}
	return result
}
