package types

import "errors"

const Organization = "chainreactors"

var (
	ErrIsInstalled = errors.New("already installed")
	ErrIsUpToDate  = errors.New("already up to date")

	ErrNoAssetFound = "could not find release asset for your platform (%s/%s)"
	ErrToolNotFound = "%s: tool not found in path %s: skipping, please install first"
)

type Tool struct {
	Name          string            `json:"name"`
	Repo          string            `json:"repo"`
	Version       string            `json:"version"`
	GoInstallPath string            `json:"go_install_path" yaml:"go_install_path"`
	Requirements  []ToolRequirement `json:"requirements"`
	Assets        map[string]int64  `json:"assets"`
	InstallType   InstallType       `json:"install_type" yaml:"install_type"`

	Org         string   `json:"org,omitempty" yaml:"org,omitempty"`
	Source      string   `json:"source,omitempty" yaml:"source,omitempty"`
	Tags        []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Category    string   `json:"category,omitempty" yaml:"category,omitempty"`
}

// GetOrg returns the tool's GitHub organization, defaulting to the
// chainreactors constant when not explicitly set.
func (t Tool) GetOrg() string {
	if t.Org != "" {
		return t.Org
	}
	return Organization
}

type InstallType string

const (
	Binary InstallType = "binary"
	Go     InstallType = "go"
)

type ToolRequirement struct {
	OS            string                         `json:"os"`
	Specification []ToolRequirementSpecification `json:"specification"`
}

type ToolRequirementSpecification struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Command     string `json:"command"`
	Instruction string `json:"instruction"`
}

type NucleiData struct {
	IgnoreHash string `json:"ignore-hash"`
	Tools      []Tool `json:"tools"`
}
