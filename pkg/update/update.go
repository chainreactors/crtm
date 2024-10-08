package update

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/chainreactors/crtm/pkg/utils"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/charmbracelet/glamour"
	"github.com/denisbrodbeck/machineid"
	"github.com/minio/selfupdate"
	"github.com/projectdiscovery/gologger"
)

const (
	Organization = "chainreactors"
)

var (
	// By default when tool is updated release notes of latest version are printed
	HideReleaseNotes      = false
	HideProgressBar       = false
	VersionCheckTimeout   = time.Duration(5) * time.Second
	DownloadUpdateTimeout = time.Duration(30) * time.Second
	// Note: DefaultHttpClient is only used in GetToolVersionCallback
	DefaultHttpClient *http.Client
)

// GetUpdateToolCallback returns a callback function
// that updates given tool if given version is older than latest gh release and exits
func GetUpdateToolCallback(toolName, version string) func() {
	return GetUpdateToolFromRepoCallback(toolName, version, "")
}

// GetUpdateToolWithRepoCallback returns a callback function that is similar to GetUpdateToolCallback
// but it takes repoName as an argument (repoName can be either just repoName ex: `nuclei` or full repo Addr ex: `projectdiscovery/nuclei`)
func GetUpdateToolFromRepoCallback(toolName, version, repoName string) func() {
	return func() {
		if repoName == "" {
			repoName = toolName
		}
		gh, err := NewghReleaseDownloader(repoName)
		if err != nil {
			gologger.Fatal().Label("updater").Msgf("failed to download latest release got %v", err)
		}
		gh.SetToolName(toolName)
		latestVersion, err := semver.NewVersion(gh.Latest.GetTagName())
		if err != nil {
			gologger.Fatal().Label("updater").Msgf("failed to parse semversion from tagname `%v` got %v", gh.Latest.GetTagName(), err)
		}
		currentVersion, err := semver.NewVersion(version)
		if err != nil {
			gologger.Fatal().Label("updater").Msgf("failed to parse semversion from current version %v got %v", version, err)
		}
		// check if current version is outdated
		if !IsOutdated(currentVersion.String(), latestVersion.String()) {
			gologger.Info().Msgf("%v is already updated to latest version", toolName)
			os.Exit(0)
		}
		// check permissions before downloading release
		updateOpts := selfupdate.Options{}
		// TODO: selfupdate(https://github.com/minio/selfupdate) has support for checksum validation , code signing verification etc. implement them after discussion
		if err := updateOpts.CheckPermissions(); err != nil {
			gologger.Fatal().Label("updater").Msgf("update of %v %v -> %v failed , insufficient permission detected got: %v", toolName, currentVersion.String(), latestVersion.String(), err)
		}
		bin, err := gh.GetExecutableFromAsset()
		if err != nil {
			gologger.Fatal().Label("updater").Msgf("executable %v not found in release asset `%v` got: %v", toolName, gh.AssetID, err)
		}

		if err = selfupdate.Apply(bytes.NewBuffer(bin), updateOpts); err != nil {
			gologger.Error().Msgf("update of %v %v -> %v failed, rolling back update", toolName, currentVersion.String(), latestVersion.String())
			if err := selfupdate.RollbackError(err); err != nil {
				gologger.Fatal().Label("updater").Msgf("rollback of update of %v failed got %v,pls reinstall %v", toolName, err, toolName)
			}
			os.Exit(1)
		}

		gologger.Print().Msg("")
		gologger.Info().Msgf("%v sucessfully updated %v -> %v (latest)", toolName, currentVersion.String(), latestVersion.String())

		if !HideReleaseNotes {
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
			gologger.Print().Msgf("%v\n\n", output)
		}
		os.Exit(0)
	}
}

// GetToolVersionCallback returns a callback function that checks for updates of tool
// by sending a request to update check endpoint and returns latest version
// if repoName is empty then tool name is considered as repoName
func GetToolVersionCallback(toolName, version string) func() (string, error) {
	return func() (string, error) {
		client := utils.GithubClient()

		release, _, err := client.Repositories.GetLatestRelease(context.Background(), Organization, toolName)
		if err != nil {
			return "", fmt.Errorf("failed to get latest release from GitHub: %w", err)
		}

		if release.GetTagName() == "" {
			return "", fmt.Errorf("something went wrong, expected version string but got empty string")
		}

		return release.GetTagName(), nil
	}
}

// GetcrtmParams returns encoded query parameters sent to update check endpoint
func GetcrtmParams(version string) string {
	params := &url.Values{}
	params.Add("os", runtime.GOOS)
	params.Add("arch", runtime.GOARCH)
	params.Add("go_version", runtime.Version())
	params.Add("v", version)
	params.Add("machine_id", buildMachineId())
	return params.Encode()
}

func buildMachineId() string {
	machineId, err := machineid.ProtectedID("crtm")
	if err != nil {
		return "unknown"
	}
	return machineId
}

// Deprecated: use GetToolVersionCheckCallback instead
//func GetVersionCheckCallback(toolName string) func() (string, error) {
//	return GetToolVersionCallback(toolName, "")
//}

func init() {
	DefaultHttpClient = &http.Client{
		Timeout: VersionCheckTimeout,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}
