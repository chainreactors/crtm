package pkg

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chainreactors/crtm/pkg/registry"
)

var httpClient = &http.Client{Timeout: 5 * time.Minute}
var noFollowClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// Release retains the original tag, including prefixes and nested paths.
type Release struct{ Tag, Version string }

func ResolveLatestRelease(ctx context.Context, repo string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://github.com/"+repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	resp, err := noFollowClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	_, tag, ok := strings.Cut(loc, "/releases/tag/")
	if !ok || tag == "" {
		return Release{}, fmt.Errorf("invalid latest release redirect (HTTP %d): %s", resp.StatusCode, loc)
	}
	tag, err = url.PathUnescape(tag)
	if err != nil {
		return Release{}, err
	}
	return Release{Tag: tag, Version: strings.TrimPrefix(path.Base(tag), "v")}, nil
}

func ResolveLatestVersion(repo string) (string, error) {
	release, err := ResolveLatestRelease(context.Background(), repo)
	return release.Version, err
}

func ResolveVersionIfNeeded(entry registry.ToolEntry, version string) (string, error) {
	if version != "" {
		return version, nil
	}
	asset := entry.AssetName("{version}")
	if !strings.Contains(asset, "{version}") {
		return "", nil
	}
	return ResolveLatestVersion(entry.Repo)
}

func DownloadAndInstall(entry registry.ToolEntry, version, binPath string) error {
	release := Release{Version: strings.TrimPrefix(version, "v")}
	if version != "" {
		release.Tag = entry.ReleaseTag(version)
	} else {
		var err error
		release, err = ResolveLatestRelease(context.Background(), entry.Repo)
		if err != nil {
			return err
		}
	}
	return InstallRelease(context.Background(), entry, release, binPath, nil)
}

// InstallRelease stages, validates and atomically replaces one executable.
// A failed installation never removes an existing version.
func InstallRelease(ctx context.Context, entry registry.ToolEntry, release Release, binPath string, validate func(context.Context, string) error) error {
	staged, _, err := stageArtifact(ctx, releaseArtifact(entry, release, CurrentTarget()), binPath, validate)
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	return replaceBinary(staged, filepath.Join(binPath, BinaryName(entry.Name)))
}

// DownloadRelease downloads and extracts an executable for an explicit target.
// It does not execute the result, so build tools can use it when cross-compiling.
func DownloadRelease(ctx context.Context, entry registry.ToolEntry, release Release, goos, goarch string) ([]byte, error) {
	if entry.Name == "" || filepath.Base(entry.Name) != entry.Name || strings.ContainsAny(entry.Name, `/\\`) || entry.Name == "." || entry.Name == ".." {
		return nil, fmt.Errorf("invalid tool name %q", entry.Name)
	}
	_, executable, err := entry.AssetFor(release.Version, goos, goarch)
	if err != nil {
		return nil, err
	}
	downloadURL, err := findReleaseURLFor(ctx, entry, release, goos, goarch)
	if err != nil {
		return nil, err
	}
	data, err := httpGet(ctx, downloadURL)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", entry.Name, err)
	}
	body, err := binaryFromArchive(data, executable)
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", entry.Name, err)
	}
	if !isExecutableFor(body, goos) {
		return nil, fmt.Errorf("%s: release asset is not a %s executable", entry.Name, goos)
	}
	return body, nil
}

func BinaryName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

func FindDownloadURL(entry registry.ToolEntry, version string) (string, error) {
	release := Release{Version: strings.TrimPrefix(version, "v")}
	if version != "" {
		release.Tag = entry.ReleaseTag(version)
	}
	return findReleaseURL(context.Background(), entry, release)
}

func findReleaseURL(ctx context.Context, entry registry.ToolEntry, release Release) (string, error) {
	return findReleaseURLFor(ctx, entry, release, runtime.GOOS, runtime.GOARCH)
}

func findReleaseURLFor(ctx context.Context, entry registry.ToolEntry, release Release, goos, goarch string) (string, error) {
	primary, err := entry.ReleaseURL(release.Tag, release.Version, goos, goarch)
	if err != nil {
		return "", err
	}
	// Explicit platform mappings are exact; old third-party patterns retain suffix probing.
	if len(entry.Platforms) > 0 || hasArchiveExt(primary) {
		return primary, nil
	}
	candidates := []string{primary, primary + ".tar.gz", primary + ".zip"}
	if goos == "darwin" {
		for _, u := range append([]string(nil), candidates...) {
			candidates = append(candidates, strings.ReplaceAll(u, "macOS", "darwin"))
		}
	}
	for _, u := range candidates {
		if urlOK(ctx, u) {
			return u, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no release asset for %s at %s", entry.Name, primary)
}

func URLOK(u string) bool { return urlOK(context.Background(), u) }
func urlOK(ctx context.Context, u string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return false
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
func HTTPGet(u string) ([]byte, error) { return httpGet(context.Background(), u) }
func httpGet(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
	}
	return readBounded(resp.Body)
}
func readBounded(r io.Reader) ([]byte, error) {
	const limit = MaxBinarySize
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if len(data) > limit {
		return nil, fmt.Errorf("release executable exceeds 512 MiB")
	}
	return data, err
}
func hasArchiveExt(p string) bool {
	p = strings.ToLower(p)
	return strings.HasSuffix(p, ".tar.gz") || strings.HasSuffix(p, ".tgz") || strings.HasSuffix(p, ".zip")
}

// ExtractBinary selects only the requested executable. It never guesses using file size.
func ExtractBinary(data []byte, toolName, binPath string) error {
	body, err := binaryFromArchive(data, BinaryName(toolName))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(binPath, 0755); err != nil {
		return err
	}
	if filepath.Base(toolName) != toolName || strings.ContainsAny(toolName, `/\\`) || toolName == "." || toolName == ".." || toolName == "" {
		return fmt.Errorf("invalid tool name")
	}
	return os.WriteFile(filepath.Join(binPath, BinaryName(toolName)), body, 0755)
}
func binaryFromArchive(data []byte, executable string) ([]byte, error) {
	var found []byte
	accept := func(name string, r io.Reader) error {
		if path.Base(strings.ReplaceAll(name, "\\", "/")) != executable {
			return nil
		}
		if found != nil {
			return fmt.Errorf("ambiguous executable %q in archive", executable)
		}
		var err error
		found, err = readBounded(r)
		return err
	}
	switch {
	case len(data) > 4 && bytes.HasPrefix(data, []byte("PK")):
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, file := range archive.File {
			if !file.Mode().IsRegular() {
				continue
			}
			r, err := file.Open()
			if err != nil {
				return nil, err
			}
			err = accept(file.Name, r)
			r.Close()
			if err != nil {
				return nil, err
			}
		}
	case len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b:
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if header.Typeflag == tar.TypeReg {
				if err := accept(header.Name, tr); err != nil {
					return nil, err
				}
			}
		}
	default:
		return data, nil
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("executable %q not found in archive", executable)
	}
	return found, nil
}
func isExecutableFor(data []byte, goos string) bool {
	if len(data) < 4 {
		return false
	}
	switch goos {
	case "windows":
		return bytes.HasPrefix(data, []byte("MZ"))
	case "linux":
		return bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'})
	case "darwin":
		magic := string(data[:4])
		return magic == "\xcf\xfa\xed\xfe" || magic == "\xfe\xed\xfa\xcf" || magic == "\xca\xfe\xba\xbe" || magic == "\xbe\xba\xfe\xca"
	}
	return false
}
