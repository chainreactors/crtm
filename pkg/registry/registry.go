package registry

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed arsenal.yaml
var embedded []byte

// ToolEntry is the declarative definition of a tool in the YAML registry.
// Version is the default for bundles; omitted versions resolve at build time.
type ToolEntry struct {
	Name         string   `yaml:"name" json:"name"`
	Version      string   `yaml:"version,omitempty" json:"version,omitempty"`
	Repo         string   `yaml:"repo" json:"repo"`
	AssetPattern string   `yaml:"asset_pattern" json:"asset_pattern"`
	Description  string   `yaml:"description,omitempty" json:"description,omitempty"`
	Tags         []string `yaml:"tags,omitempty,flow" json:"tags,omitempty"`
	Category     string   `yaml:"category,omitempty" json:"category,omitempty"`
	DocsURL      string   `yaml:"docs_url,omitempty" json:"docs_url,omitempty"`
	Hint         string   `yaml:"hint,omitempty" json:"hint,omitempty"`
	// TagPattern defaults to v{version} for the existing registries.
	TagPattern string                   `yaml:"tag_pattern,omitempty" json:"tag_pattern,omitempty"`
	Executable string                   `yaml:"executable,omitempty" json:"executable,omitempty"`
	Platforms  map[string]PlatformAsset `yaml:"platforms,omitempty" json:"platforms,omitempty"`
}

// PlatformAsset selects a release artifact by GOOS/GOARCH.
type PlatformAsset struct {
	Asset      string `yaml:"asset,omitempty" json:"asset,omitempty"`
	Executable string `yaml:"executable,omitempty" json:"executable,omitempty"`
}

func (e ToolEntry) AssetFor(version, goos, goarch string) (string, string, error) {
	pattern, executable := e.AssetPattern, e.Executable
	if len(e.Platforms) > 0 {
		platform, ok := e.Platforms[goos+"/"+goarch]
		if !ok {
			return "", "", fmt.Errorf("%s: unsupported platform %s/%s", e.Name, goos, goarch)
		}
		if platform.Asset != "" {
			pattern = platform.Asset
		}
		if platform.Executable != "" {
			executable = platform.Executable
		}
	}
	if executable == "" {
		executable = e.Name
	}
	if goos == "windows" && !strings.HasSuffix(executable, ".exe") {
		executable += ".exe"
	}
	osName := goos
	if goos == "darwin" {
		osName = "macOS"
	}
	asset := strings.NewReplacer("{name}", e.Name, "{version}", version, "{os}", osName, "{arch}", goarch).Replace(pattern)
	return strings.ReplaceAll(asset, "__", "_"), executable, nil
}

// ReleaseURL keeps the exact GitHub tag distinct from the version in filenames.
func (e ToolEntry) ReleaseURL(tag, version, goos, goarch string) (string, error) {
	asset, _, err := e.AssetFor(version, goos, goarch)
	if err != nil {
		return "", err
	}
	if tag == "" {
		return fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", e.Repo, asset), nil
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", e.Repo, url.PathEscape(tag), asset), nil
}

func (e ToolEntry) ReleaseTag(version string) string {
	pattern := e.TagPattern
	if pattern == "" {
		pattern = "v{version}"
	}
	return strings.ReplaceAll(pattern, "{version}", strings.TrimPrefix(version, "v"))
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
	asset, _, _ := e.AssetFor(version, runtime.GOOS, runtime.GOARCH)
	return asset
}

// DownloadURL returns the direct GitHub release download URL.
// If version is empty, uses /releases/latest/download/ which auto-resolves.
func (e ToolEntry) DownloadURL(version string) string {
	tag := ""
	if version != "" {
		tag = e.ReleaseTag(version)
	}
	u, _ := e.ReleaseURL(tag, strings.TrimPrefix(version, "v"), runtime.GOOS, runtime.GOARCH)
	return u
}

// LoadEmbedded loads CRTM's default catalog for standalone use.
func LoadEmbedded() ([]ToolEntry, error) {
	return ParseYAML(embedded)
}

// ParseYAML parses a YAML file containing a list of ToolEntry.
func ParseYAML(data []byte) ([]ToolEntry, error) {
	var entries []ToolEntry
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&entries); err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name := strings.ToLower(entry.Name)
		if name == "" || seen[name] {
			return nil, fmt.Errorf("empty or duplicate tool name %q", entry.Name)
		}
		seen[name] = true
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
