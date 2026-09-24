package runner

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chainreactors/crtm/pkg"
	"github.com/chainreactors/crtm/pkg/path"
	"github.com/chainreactors/crtm/pkg/registry"
	"github.com/projectdiscovery/gologger"
)

// Runner is the CLI adapter for the shared Manager.
type Runner struct{ options *Options }

func NewRunner(options *Options) (*Runner, error) { return &Runner{options: options}, nil }

func (r *Runner) Run() error {
	if r.options.SetPath || r.options.Path == defaultPath {
		if err := path.SetENV(r.options.Path); err != nil {
			return err
		}
	}
	if r.options.UnSetPath {
		if err := path.UnsetENV(r.options.Path); err != nil {
			return err
		}
	}
	mgr, err := pkg.NewManager(pkg.ManagerOption{BinPath: r.options.Path, ConfigPath: r.options.ConfigFile})
	if err != nil {
		return err
	}

	if len(r.options.Search) > 0 {
		for _, query := range r.options.Search {
			results := mgr.Search(query)
			if len(results) == 0 {
				gologger.Info().Msgf("no tools found for %q", query)
			}
			for i, tool := range results {
				description := tool.Description
				if description == "" {
					description = strings.Join(tool.Tags, ", ")
				}
				fmt.Printf("%d. [%s] %s %s %s\n", i+1, tool.Org(), tool.Name, installedVersion(mgr, tool.Name), description)
			}
		}
		return nil
	}
	if r.options.AddTool != "" {
		owner, name, ok := strings.Cut(r.options.AddTool, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return fmt.Errorf("custom tool must use owner/repo format")
		}
		entry := registry.ToolEntry{Name: name, Repo: r.options.AddTool, AssetPattern: r.options.AssetPattern}
		added, err := mgr.AddCustomTool(entry)
		if err != nil {
			return err
		}
		if added {
			gologger.Info().Msgf("added %s to custom tools", entry.Name)
		} else {
			gologger.Info().Msgf("%s already registered", entry.Repo)
		}
		return nil
	}

	for _, tool := range mgr.ListTools() {
		switch {
		case r.options.InstallAll:
			r.options.Install = append(r.options.Install, tool.Name)
		case r.options.UpdateAll:
			r.options.Update = append(r.options.Update, tool.Name)
		case r.options.RemoveAll:
			r.options.Remove = append(r.options.Remove, tool.Name)
		}
	}
	if len(r.options.Install) == 0 && len(r.options.Update) == 0 && len(r.options.Remove) == 0 {
		return r.ListToolsAndEnv(mgr)
	}
	if !path.IsSubPath(homeDir, r.options.Path) {
		return fmt.Errorf("binary path must be within the home directory: %s", r.options.Path)
	}
	var failures []error
	for _, action := range []struct {
		names []string
		run   func(string) error
	}{
		{r.options.Install, mgr.InstallTool},
		{r.options.Update, mgr.UpdateTool},
		{r.options.Remove, mgr.RemoveTool},
	} {
		for _, name := range action.names {
			if err := action.run(name); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", name, err))
			}
		}
	}
	for _, name := range r.options.Install {
		if tool, ok := mgr.Catalog().Find(name); ok && mgr.IsInstalled(name) && tool.Hint != "" {
			gologger.Info().Msgf("%s: %s", tool.Name, tool.Hint)
		}
	}
	return errors.Join(failures...)
}

func installedVersion(mgr *pkg.Manager, name string) string {
	if version := mgr.InstalledVersion(name); version != "" {
		return "[installed: " + version + "]"
	}
	return "[not installed]"
}

func (r *Runner) ListToolsAndEnv(mgr *pkg.Manager) error {
	gologger.Info().Msg(path.GetOsData())
	gologger.Info().Msgf("Path to download project binary: %s", r.options.Path)
	if path.IsSet(r.options.Path) {
		gologger.Info().Msgf("Path %s configured in environment variable $PATH", r.options.Path)
	} else {
		gologger.Info().Msgf("Path %s not configured in environment variable $PATH", r.options.Path)
	}
	for i, tool := range mgr.ListTools() {
		fmt.Printf("%d. [%s] %s %s\n", i+1, tool.Org(), tool.Name, installedVersion(mgr, tool.Name))
	}
	return nil
}

func (r *Runner) Close() {}
