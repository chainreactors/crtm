package pkg

import (
	"os"
	"path/filepath"

	"github.com/chainreactors/crtm/pkg/registry"
	"gopkg.in/yaml.v3"
)

// CRTMConfig is the on-disk YAML config at ~/.crtm/config.yaml.
type CRTMConfig struct {
	CustomTools []registry.ToolEntry `yaml:"custom_tools,omitempty"`
}

func DefaultConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".crtm")
}

func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "config.yaml")
}

func LoadCRTMConfig(path string) (CRTMConfig, error) {
	var cfg CRTMConfig
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	err = yaml.Unmarshal(data, &cfg)
	return cfg, err
}

func SaveCRTMConfig(path string, cfg CRTMConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// AddCustomTool appends a tool entry to the config, avoiding duplicates by name.
func AddCustomTool(configPath string, entry registry.ToolEntry) (bool, error) {
	cfg, err := LoadCRTMConfig(configPath)
	if err != nil {
		return false, err
	}
	for _, e := range cfg.CustomTools {
		if e.Name == entry.Name || e.Repo == entry.Repo {
			return false, nil
		}
	}
	cfg.CustomTools = append(cfg.CustomTools, entry)
	return true, SaveCRTMConfig(configPath, cfg)
}
