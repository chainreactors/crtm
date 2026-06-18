package pkg

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chainreactors/crtm/pkg/registry"
	"github.com/projectdiscovery/gologger"
)

var httpClient = &http.Client{
	Timeout: 5 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

var noFollowClient = &http.Client{
	Timeout:       15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// DownloadAndInstall fetches the tool binary via direct GitHub release URL
// and extracts it to binPath.
func DownloadAndInstall(entry registry.ToolEntry, version, binPath string) error {
	version, err := ResolveVersionIfNeeded(entry, version)
	if err != nil {
		return err
	}

	url, err := FindDownloadURL(entry, version)
	if err != nil {
		return err
	}

	gologger.Info().Msgf("downloading %s from %s", entry.Name, url)
	data, err := HTTPGet(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", entry.Name, err)
	}

	if err := os.MkdirAll(binPath, 0o755); err != nil {
		return err
	}

	if err := ExtractBinary(data, entry.Name, binPath); err != nil {
		return fmt.Errorf("extract %s: %w", entry.Name, err)
	}

	gologger.Info().Msgf("installed %s to %s", entry.Name, binPath)
	return nil
}

// ---------------------------------------------------------------------------
// Version resolution (public helpers)
// ---------------------------------------------------------------------------

// ResolveLatestVersion does a HEAD on /releases/latest and extracts the
// version tag from the 302 redirect. No GitHub API call.
func ResolveLatestVersion(repo string) (string, error) {
	url := fmt.Sprintf("https://github.com/%s/releases/latest", repo)
	resp, err := noFollowClient.Head(url)
	if err != nil {
		return "", fmt.Errorf("HEAD %s: %w", url, err)
	}
	resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("no redirect from %s (HTTP %d)", url, resp.StatusCode)
	}
	// Location: https://github.com/ffuf/ffuf/releases/tag/v2.1.0
	if i := strings.LastIndex(loc, "/"); i >= 0 {
		tag := loc[i+1:]
		return strings.TrimPrefix(tag, "v"), nil
	}
	return "", fmt.Errorf("cannot parse version from redirect: %s", loc)
}

// ResolveVersionIfNeeded auto-resolves the latest version when the asset
// pattern contains {version} and no version was provided.
func ResolveVersionIfNeeded(entry registry.ToolEntry, version string) (string, error) {
	if version != "" || !strings.Contains(entry.AssetPattern, "{version}") {
		return version, nil
	}
	resolved, err := ResolveLatestVersion(entry.Repo)
	if err != nil {
		return "", fmt.Errorf("resolve latest version for %s: %w", entry.Name, err)
	}
	gologger.Info().Msgf("resolved %s latest version: %s", entry.Name, resolved)
	return resolved, nil
}

// ---------------------------------------------------------------------------
// URL probing (public helpers)
// ---------------------------------------------------------------------------

// FindDownloadURL tries the primary asset name, then probes common suffixes.
func FindDownloadURL(entry registry.ToolEntry, version string) (string, error) {
	primary := entry.DownloadURL(version)

	if hasArchiveExt(entry.AssetPattern) {
		if URLOK(primary) {
			return primary, nil
		}
		// Try with os alias (macOS ↔ darwin).
		if alt := osAliasURL(entry, version); alt != "" && URLOK(alt) {
			return alt, nil
		}
		return "", fmt.Errorf("asset not found: %s", primary)
	}

	// No extension in pattern — probe raw, .tar.gz, .zip.
	for _, suffix := range []string{"", ".tar.gz", ".zip"} {
		u := primary + suffix
		if URLOK(u) {
			return u, nil
		}
	}
	// Try os alias.
	if alt := osAliasURL(entry, version); alt != "" {
		for _, suffix := range []string{"", ".tar.gz", ".zip"} {
			u := alt + suffix
			if URLOK(u) {
				return u, nil
			}
		}
	}
	return "", fmt.Errorf("no downloadable asset for %s (tried %s +.tar.gz +.zip)", entry.Name, primary)
}

// URLOK does a HEAD to check reachability.
func URLOK(url string) bool {
	resp, err := httpClient.Head(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// HTTPGet fetches a URL and returns the body bytes.
func HTTPGet(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func hasArchiveExt(pattern string) bool {
	p := strings.ToLower(pattern)
	return strings.HasSuffix(p, ".tar.gz") || strings.HasSuffix(p, ".tgz") ||
		strings.HasSuffix(p, ".zip") || strings.HasSuffix(p, ".gz") ||
		strings.HasSuffix(p, ".xz") || strings.HasSuffix(p, ".bz2")
}

func osAliasURL(entry registry.ToolEntry, version string) string {
	osName := runtime.GOOS
	var alias string
	switch osName {
	case "darwin":
		alias = "macOS"
	case "linux":
		return "" // no common alias
	default:
		return ""
	}
	alt := registry.ToolEntry{
		Name:         entry.Name,
		Repo:         entry.Repo,
		AssetPattern: strings.ReplaceAll(entry.AssetPattern, "{os}", alias),
	}
	return alt.DownloadURL(version)
}

// ---------------------------------------------------------------------------
// Archive extraction (public, strong tolerance)
// ---------------------------------------------------------------------------

// ExtractBinary auto-detects archive format by magic bytes and extracts
// the tool binary. It tolerates:
//   - binary in subdirectories
//   - binary name with .exe suffix
//   - archive containing a single executable (fallback)
func ExtractBinary(data []byte, toolName, binPath string) error {
	// Zip (PK magic).
	if len(data) > 4 && data[0] == 'P' && data[1] == 'K' {
		return extractZip(data, toolName, binPath)
	}
	// Gzip (1f 8b magic) — likely tar.gz.
	if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
		return extractTarGz(data, toolName, binPath)
	}
	// Raw binary.
	return writeBinaryFile(bytes.NewReader(data), toolName, binPath)
}

func extractZip(data []byte, toolName, binPath string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}

	// Pass 1: exact name match (case-insensitive, any directory depth).
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if isBinaryMatch(filepath.Base(f.Name), toolName) {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = writeBinaryFile(rc, toolName, binPath)
			rc.Close()
			return err
		}
	}

	// Pass 2: largest executable-looking file as fallback.
	return extractLargestFromZip(r, toolName, binPath)
}

func extractTarGz(data []byte, toolName, binPath string) error {
	// We need two passes, so buffer the tar entries.
	type tarEntry struct {
		name string
		size int64
		data []byte
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()

	var entries []tarEntry
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		entries = append(entries, tarEntry{
			name: hdr.Name,
			size: hdr.Size,
			data: body,
		})
	}

	// Pass 1: exact match.
	for _, e := range entries {
		if isBinaryMatch(filepath.Base(e.name), toolName) {
			return writeBinaryFile(bytes.NewReader(e.data), toolName, binPath)
		}
	}

	// Pass 2: largest file.
	if len(entries) == 0 {
		return fmt.Errorf("binary %q not found in tar.gz (archive is empty)", toolName)
	}
	largest := entries[0]
	for _, e := range entries[1:] {
		if e.size > largest.size {
			largest = e
		}
	}
	if largest.size < 1024 {
		return fmt.Errorf("binary %q not found in tar.gz (no suitable file)", toolName)
	}
	return writeBinaryFile(bytes.NewReader(largest.data), toolName, binPath)
}

func extractLargestFromZip(r *zip.Reader, toolName, binPath string) error {
	var best *zip.File
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if best == nil || f.UncompressedSize64 > best.UncompressedSize64 {
			best = f
		}
	}
	if best == nil || best.UncompressedSize64 < 1024 {
		return fmt.Errorf("binary %q not found in zip (no suitable file)", toolName)
	}
	rc, err := best.Open()
	if err != nil {
		return err
	}
	err = writeBinaryFile(rc, toolName, binPath)
	rc.Close()
	return err
}

// isBinaryMatch checks if a filename in an archive matches the expected
// tool binary name. Tolerates: case differences, .exe suffix, version
// suffixes like "ffuf_2.1.0" matching "ffuf".
func isBinaryMatch(filename, toolName string) bool {
	clean := strings.TrimSuffix(filename, ".exe")
	if strings.EqualFold(clean, toolName) {
		return true
	}
	// Handle: "toolName_version" or "toolName-version" or "toolName.version"
	for _, sep := range []string{"_", "-", "."} {
		if strings.HasPrefix(strings.ToLower(clean), strings.ToLower(toolName)+sep) {
			return true
		}
	}
	return false
}

func writeBinaryFile(r io.Reader, toolName, binPath string) error {
	name := toolName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(binPath, name)
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}
