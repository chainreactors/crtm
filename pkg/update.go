package pkg

import (
	"fmt"
	"github.com/chainreactors/crtm/pkg/update"
	"github.com/chainreactors/crtm/pkg/utils"
	"os"
	"path/filepath"
	"strings"

	ospath "github.com/chainreactors/crtm/pkg/path"
	"github.com/chainreactors/crtm/pkg/types"
	"github.com/chainreactors/crtm/pkg/version"
	"github.com/charmbracelet/glamour"
	"github.com/projectdiscovery/gologger"
)

// Update updates a given tool
func Update(path string, tool types.Tool, disableChangeLog bool) error {
	if executablePath, exists := ospath.GetExecutablePath(path, tool.Name); exists {
		if isUpToDate(tool, path) {
			return types.ErrIsUpToDate
		}
		gologger.Info().Msgf("updating %s...", tool.Name)

		if len(tool.Assets) == 0 {
			return fmt.Errorf(types.ErrNoAssetFound, tool.Name, executablePath)
		}

		if err := os.Remove(executablePath); err != nil {
			return err
		}

		ver, err := install(tool, path)
		if err != nil {
			return err
		}
		if !disableChangeLog {
			showReleaseNotes(tool.Repo)
		}
		gologger.Info().Msgf("updated %s to %s (%s)", tool.Name, ver, au.BrightGreen("latest").String())
		return nil
	} else {
		return fmt.Errorf(types.ErrToolNotFound, tool.Name, executablePath)
	}
}

func isUpToDate(tool types.Tool, path string) bool {
	v, err := version.ExtractInstalledVersion(tool, path)
	return err == nil && strings.EqualFold(tool.Version, v)
}

func showReleaseNotes(toolname string) {
	gh, err := update.NewghReleaseDownloader(toolname)
	if err != nil {
		gologger.Fatal().Label("updater").Msgf("failed to download latest release got %v", err)
	}
	gh.SetToolName(toolname)
	output := gh.Latest.GetBody()
	// adjust colors for both dark / light terminal themes
	r, err := glamour.NewTermRenderer(glamour.WithAutoStyle())
	if err != nil {
		gologger.Error().Msgf("markdown rendering not supported: %v", err)
	}
	if rendered, err := r.Render(output); err == nil {
		output = rendered
	} else {
		gologger.Error().Msg(err.Error())
	}
	gologger.Print().Msgf("%v\n", output)
}

// GetVersionCheckCallback returns a callback function and when it is executed returns a version string of that tool
func GetVersionCheckCallback(toolName, basePath string) func() string {
	return func() string {
		tool, err := utils.FetchTool(toolName)
		if err != nil {
			return err.Error()
		}
		return fmt.Sprintf("%s %s", toolName, utils.InstalledVersion(tool, basePath, au))
	}
}

// GetUpdaterCallback returns a callback function when executed  updates that tool
func GetUpdaterCallback(toolName string) func() {
	return func() {
		home, _ := os.UserHomeDir()
		dp := filepath.Join(home, ".crtm/go/bin")
		tool, err := utils.FetchTool(toolName)
		if err != nil {
			gologger.Error().Msgf("failed to fetch details of %v skipping update: %v", toolName, err)
			return
		}
		err = Update(dp, tool, false)
		if err == types.ErrIsUpToDate {
			gologger.Info().Msgf("%s: %s", toolName, err)
		} else {
			gologger.Error().Msgf("error while updating %s: %s", toolName, err)
		}
	}
}
