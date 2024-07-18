package utils

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chainreactors/crtm/pkg/path"
	"github.com/chainreactors/crtm/pkg/types"
	"github.com/chainreactors/crtm/pkg/version"
	"github.com/logrusorgru/aurora/v4"
)

var (
	CRTMRepo       = "crtm"
	GOGORepo       = "gogo"
	SprayRepo      = "spray"
	ZombieRepo     = "zombie"
	UrlFounderRepo = "urlfounder"
	CDNCheckRepo   = "cdncheck"

	Tools = map[string]string{
		//"crtm":       CRTMRepo,
		"gogo":       GOGORepo,
		"spray":      SprayRepo,
		"zombie":     ZombieRepo,
		"urlfounder": UrlFounderRepo,
		//"cdncheck_cn": CDNCheckRepo,
	}
)

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// configure aurora for logging
var au = aurora.New(aurora.WithColors(true))

func FetchToolList() ([]types.Tool, error) {
	tools := make([]types.Tool, 0)
	for name, repo := range Tools {
		tool, err := fetchToolFromGitHub(name, repo)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func fetchToolFromGitHub(toolName, repo string) (types.Tool, error) {
	ctx := context.Background()
	client := GithubClient()
	release, _, err := client.Repositories.GetLatestRelease(ctx, types.Organization, repo)
	if err != nil {
		return types.Tool{}, err
	}

	assets := make(map[string]int64)
	for _, asset := range release.Assets {
		assets[asset.GetName()] = asset.GetID()
	}

	tool := types.Tool{
		Name:        toolName,
		Repo:        repo,
		Version:     strings.TrimPrefix(release.GetTagName(), "v"),
		Assets:      assets,
		InstallType: "unknown", // You may want to set this appropriately
	}
	return tool, nil
}

func FetchTool(toolName string) (types.Tool, error) {
	repo, exists := Tools[toolName]
	if !exists {
		return types.Tool{}, fmt.Errorf("tool %s not found in Tools map", toolName)
	}
	return fetchToolFromGitHub(toolName, repo)
}

func Contains(s []types.Tool, toolName string) (int, bool) {
	for i, a := range s {
		if strings.EqualFold(a.Name, toolName) {
			return i, true
		}
	}
	return -1, false
}

func InstalledVersion(tool types.Tool, basePath string, au *aurora.Aurora) string {
	var msg string

	installedVersion, err := version.ExtractInstalledVersion(tool, basePath)
	if err != nil {
		osAvailable := isOsAvailable(tool)
		if !osAvailable {
			msg = fmt.Sprintf("(%s)", au.Gray(10, "not supported").String())
		} else {
			msg = fmt.Sprintf("(%s)", au.BrightYellow("not installed").String())
		}
	}

	if installedVersion != "" {
		if strings.Contains(tool.Version, installedVersion) {
			msg = fmt.Sprintf("(%s) (%s)", au.BrightGreen("latest").String(), au.BrightGreen(tool.Version).String())
		} else {
			msg = fmt.Sprintf("(%s) (%s) ➡ (%s)",
				au.Red("outdated").String(),
				au.Red(installedVersion).String(),
				au.BrightGreen(tool.Version).String())
		}
	}

	return msg
}

func isOsAvailable(tool types.Tool) bool {
	osData := path.CheckOS()
	for asset := range tool.Assets {
		expectedAssetPrefix := tool.Name + "_" + tool.Version + "_" + osData
		if strings.Contains(asset, expectedAssetPrefix) {
			return true
		}
	}
	return false
}
