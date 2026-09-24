package runner

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/chainreactors/crtm/pkg"
	"github.com/stretchr/testify/require"
)

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected network access")
}

func TestRunnerOfflineCatalog(t *testing.T) {
	previous := http.DefaultTransport
	http.DefaultTransport = offlineTransport{}
	t.Cleanup(func() { http.DefaultTransport = previous })
	dir := t.TempDir()
	options := &Options{Path: filepath.Join(dir, "bin"), ConfigFile: filepath.Join(dir, "config.yaml")}
	runner, err := NewRunner(options)
	require.NoError(t, err)
	require.NoError(t, runner.Run())
	_, err = os.Stat(options.Path)
	require.True(t, os.IsNotExist(err), "listing should not create installation storage")
	options.AddTool, options.AssetPattern = "example/custom", "custom-{os}-{arch}"
	require.NoError(t, runner.Run())
	config, err := pkg.LoadCRTMConfig(options.ConfigFile)
	require.NoError(t, err)
	require.Len(t, config.CustomTools, 1)
	require.Equal(t, "custom", config.CustomTools[0].Name)
	require.Equal(t, options.AssetPattern, config.CustomTools[0].AssetPattern)
	options.AddTool = "bad/owner/repo"
	require.ErrorContains(t, runner.Run(), "owner/repo")
	options.AddTool, options.Search = "", []string{"custom"}
	require.NoError(t, runner.Run())
}
