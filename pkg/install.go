package pkg

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"github.com/chainreactors/crtm/pkg/utils"
	osutils "github.com/projectdiscovery/utils/os"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	ospath "github.com/chainreactors/crtm/pkg/path"
	"github.com/chainreactors/crtm/pkg/types"
	"github.com/google/go-github/github"
	"github.com/logrusorgru/aurora/v4"
	"github.com/projectdiscovery/gologger"
)

var (
	WindowExt = ".exe"
	au        = aurora.New(aurora.WithColors(true))
)

// Install installs given tool at path
func Install(path string, tool types.Tool) error {
	if _, exists := ospath.GetExecutablePath(path, tool.Name); exists {
		return types.ErrIsInstalled
	}
	gologger.Info().Msgf("installing %s...", tool.Name)
	version, err := install(tool, path)
	if err != nil {
		return err
	}
	gologger.Info().Msgf("installed %s %s (%s)", tool.Name, version, au.BrightGreen("latest").String())
	return nil
}

// GoInstall installs given tool at path
func GoInstall(path string, tool types.Tool) error {
	if _, exists := ospath.GetExecutablePath(path, tool.Name); exists {
		return types.ErrIsInstalled
	}
	gologger.Info().Msgf("installing %s with go install...", tool.Name)
	cmd := exec.Command("go", "install", "-v", fmt.Sprintf("github.com/projectdiscovery/%s/%s", tool.Name, tool.GoInstallPath))
	cmd.Env = append(os.Environ(), "GOBIN="+path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go install failed %s", string(output))
	}
	gologger.Info().Msgf("installed %s %s (%s)", tool.Name, tool.Version, au.BrightGreen("latest").String())
	return nil
}

func install(tool types.Tool, path string) (string, error) {
	var id int64
	var isZip, isTar bool
loop:
	for asset, assetID := range tool.Assets {
		switch {
		case strings.Contains(asset, ".zip"):
			if isAsset(asset, tool.Name, runtime.GOOS, runtime.GOARCH) {
				id = assetID
				isZip = true
				break loop
			}
		case strings.Contains(asset, ".tar.gz"):
			if isAsset(asset, tool.Name, runtime.GOOS, runtime.GOARCH) {
				id = assetID
				isTar = true
				break loop
			}
		default:
			if isAsset(asset, tool.Name, runtime.GOOS, runtime.GOARCH) {
				id = assetID
				break loop
			}
		}
	}

	// handle if id is zero (no asset found)
	if id == 0 {
		return "", fmt.Errorf(types.ErrNoAssetFound, runtime.GOOS, runtime.GOARCH)
	}

	_, rdurl, err := utils.GithubClient().Repositories.DownloadReleaseAsset(context.Background(), types.Organization, tool.Repo, int64(id))
	if err != nil {
		if arlErr, ok := err.(*github.AbuseRateLimitError); ok {
			// Provide user with more info regarding the rate limit
			gologger.Error().Msgf("error for remaining request per hour: %s, RetryAfter: %s", err.Error(), arlErr.RetryAfter)
		}
		return "", err
	}

	resp, err := http.Get(rdurl)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", err
	}

	switch {
	case isZip:
		err := downloadZip(resp.Body, tool.Name, path)
		if err != nil {
			return "", err
		}
	case isTar:
		err := downloadTar(resp.Body, tool.Name, path)
		if err != nil {
			return "", err
		}
	default:
		err := downloadBin(resp.Body, tool.Name, path)
		if err != nil {
			return "", err
		}
	}
	return tool.Version, nil
}

func isAsset(asset, name, os, arch string) bool {
	if strings.Contains(asset, name) && strings.Contains(asset, os) && strings.Contains(asset, arch) {
		return true
	}
	return false
}

func downloadTar(reader io.Reader, toolName, path string) error {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	tarReader := tar.NewReader(gzipReader)
	// iterate through the files in the archive
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSuffix(header.FileInfo().Name(), WindowExt), toolName) {
			continue
		}
		// if the file is not a directory, extract it
		if !header.FileInfo().IsDir() {
			filePath := filepath.Join(path, header.FileInfo().Name())
			if !strings.HasPrefix(filePath, filepath.Clean(path)+string(os.PathSeparator)) {
				return err
			}

			if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
				return err
			}

			dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return err
			}
			defer dstFile.Close()
			// copy the file data from the archive
			_, err = io.Copy(dstFile, tarReader)
			if err != nil {
				return err
			}
			// set the file permissions
			err = os.Chmod(dstFile.Name(), 0755)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func downloadZip(reader io.Reader, toolName, path string) error {
	buff := bytes.NewBuffer([]byte{})
	size, err := io.Copy(buff, reader)
	if err != nil {
		return err
	}
	zipReader, err := zip.NewReader(bytes.NewReader(buff.Bytes()), size)
	if err != nil {
		return err
	}
	for _, f := range zipReader.File {
		if !strings.EqualFold(strings.TrimSuffix(f.Name, WindowExt), toolName) {
			continue
		}
		filePath := filepath.Join(path, f.Name)
		if !strings.HasPrefix(filePath, filepath.Clean(path)+string(os.PathSeparator)) {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			return err
		}

		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		fileInArchive, err := f.Open()
		if err != nil {
			return err
		}

		if _, err := io.Copy(dstFile, fileInArchive); err != nil {
			return err
		}
		err = os.Chmod(dstFile.Name(), 0755)
		if err != nil {
			return err
		}

		dstFile.Close()
		fileInArchive.Close()
	}
	return nil
}

func downloadBin(reader io.Reader, toolName, path string) error {
	filePath := filepath.Join(path, toolName)
	if osutils.IsWindows() {
		filePath += WindowExt
	}
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(file, reader); err != nil {
		return err
	}
	return os.Chmod(filePath, 0755)
}
