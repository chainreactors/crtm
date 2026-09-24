package pkg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/chainreactors/crtm/pkg/registry"
)

// ErrArtifactNotFound is the only source error that permits fallback.
var ErrArtifactNotFound = errors.New("artifact not found")

const MaxBinarySize = 512 << 20

type Target struct {
	GOOS   string `json:"goos" yaml:"goos"`
	GOARCH string `json:"goarch" yaml:"goarch"`
}

func CurrentTarget() Target     { return Target{runtime.GOOS, runtime.GOARCH} }
func (t Target) String() string { return t.GOOS + "/" + t.GOARCH }
func (t Target) validate() error {
	if t.GOOS != "windows" && t.GOOS != "linux" && t.GOOS != "darwin" {
		return fmt.Errorf("unsupported target %s", t)
	}
	switch t.GOARCH {
	case "386", "amd64", "arm", "arm64", "riscv64", "ppc64", "ppc64le", "s390x", "mips", "mipsle", "mips64", "mips64le", "loong64":
		return nil
	}
	return fmt.Errorf("unsupported target %s", t)
}

// Request.Version is empty for source-preferred, a pinned version, or "latest"
// for an explicit refresh. Immutable bundle sources do not satisfy "latest".
type Request struct {
	Tool    registry.ToolEntry
	Version string
	Target  Target
}

// Artifact is an executable, independent of the release archive it came from.
// Open returns a fresh stream owned by the caller. Size/SHA256 may be unknown
// for a remote source; bundles must always provide both.
type Artifact struct {
	Tool    registry.ToolEntry                           `json:"tool"`
	Version string                                       `json:"version"`
	Tag     string                                       `json:"tag,omitempty"`
	Target  Target                                       `json:"target"`
	Size    int64                                        `json:"size"`
	SHA256  string                                       `json:"sha256"`
	Source  string                                       `json:"source"`
	Open    func(context.Context) (io.ReadCloser, error) `json:"-"`
}

type Source interface {
	Resolve(context.Context, Request) (Artifact, error)
}

type GitHubSource struct{}

func (GitHubSource) Resolve(ctx context.Context, req Request) (Artifact, error) {
	if err := req.Target.validate(); err != nil {
		return Artifact{}, err
	}
	if !validToolName(req.Tool.Name) {
		return Artifact{}, fmt.Errorf("invalid tool name %q", req.Tool.Name)
	}
	if _, _, err := req.Tool.AssetFor(req.Version, req.Target.GOOS, req.Target.GOARCH); err != nil {
		return Artifact{}, err
	}
	release := Release{Version: strings.TrimPrefix(req.Version, "v")}
	if req.Version == "" || req.Version == "latest" {
		var err error
		release, err = ResolveLatestRelease(ctx, req.Tool.Repo)
		if err != nil {
			return Artifact{}, err
		}
	} else {
		release.Tag = req.Tool.ReleaseTag(req.Version)
	}
	return releaseArtifact(req.Tool, release, req.Target), nil
}

func releaseArtifact(entry registry.ToolEntry, release Release, target Target) Artifact {
	return Artifact{Tool: entry, Version: release.Version, Tag: release.Tag, Target: target, Source: "github",
		Open: func(ctx context.Context) (io.ReadCloser, error) {
			body, err := DownloadRelease(ctx, entry, release, target.GOOS, target.GOARCH)
			if err != nil {
				return nil, err
			}
			return io.NopCloser(bytes.NewReader(body)), nil
		},
	}
}

func resolve(ctx context.Context, sources []Source, req Request) (Artifact, error) {
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return Artifact{}, err
		}
		a, err := source.Resolve(ctx, req)
		if errors.Is(err, ErrArtifactNotFound) {
			continue
		}
		if err != nil {
			return Artifact{}, err
		}
		if a.Tool.Name != req.Tool.Name || a.Target != req.Target || a.Version == "" || a.Open == nil {
			return Artifact{}, fmt.Errorf("source returned an invalid artifact for %s (%s)", req.Tool.Name, req.Target)
		}
		if req.Version != "" && req.Version != "latest" && strings.TrimPrefix(req.Version, "v") != strings.TrimPrefix(a.Version, "v") {
			return Artifact{}, fmt.Errorf("source returned version %s, requested %s", a.Version, req.Version)
		}
		return a, nil
	}
	return Artifact{}, fmt.Errorf("%s %s (%s): %w", req.Tool.Name, req.Version, req.Target, ErrArtifactNotFound)
}

func validToolName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return true
}
