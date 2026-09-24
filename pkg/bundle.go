package pkg

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chainreactors/crtm/pkg/registry"
)

// BundleSpec is the build input. Empty versions resolve latest at build time.
type BundleSpec struct {
	ID          string               `yaml:"id"`
	Catalog     string               `yaml:"catalog,omitempty"`
	Tools       map[string]string    `yaml:"tools"`
	CustomTools []registry.ToolEntry `yaml:"custom_tools,omitempty"`
}

type bundleManifest struct {
	Format int        `json:"format"`
	ID     string     `json:"id"`
	Target Target     `json:"target"`
	Tools  []Artifact `json:"tools"`
}

// Bundle is an immutable Source backed by any fs.FS, including embed.FS.
type Bundle struct {
	files    fs.FS
	manifest bundleManifest
}

func OpenBundle(files fs.FS) (*Bundle, error) {
	data, err := fs.ReadFile(files, "manifest.json")
	if err != nil {
		return nil, err
	}
	b := &Bundle{files: files}
	if err := json.Unmarshal(data, &b.manifest); err != nil {
		return nil, err
	}
	if b.manifest.Format != 1 || !validToolName(b.manifest.ID) {
		return nil, fmt.Errorf("invalid bundle format or ID")
	}
	if err := b.manifest.Target.validate(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, a := range b.manifest.Tools {
		key := strings.TrimSuffix(strings.ToLower(a.Tool.Name), ".exe")
		digest, err := hex.DecodeString(a.SHA256)
		if !validToolName(a.Tool.Name) || seen[key] || a.Target != b.manifest.Target || a.Version == "" || a.Version == "latest" || a.Size <= 0 || a.Size > MaxBinarySize || err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("invalid bundle artifact %q", a.Tool.Name)
		}
		seen[key] = true
	}
	return b, nil
}

func (b *Bundle) Resolve(ctx context.Context, req Request) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	if req.Version == "latest" || req.Target != b.manifest.Target {
		return Artifact{}, ErrArtifactNotFound
	}
	for _, artifact := range b.manifest.Tools {
		a := artifact
		if a.Tool.Name != req.Tool.Name || req.Version != "" && strings.TrimPrefix(req.Version, "v") != strings.TrimPrefix(a.Version, "v") {
			continue
		}
		a.Source = "bundle"
		a.Open = func(ctx context.Context) (io.ReadCloser, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			f, err := b.files.Open(a.Tool.Name + ".gz")
			if err != nil {
				return nil, err
			}
			z, err := gzip.NewReader(f)
			if err != nil {
				f.Close()
				return nil, err
			}
			return &bundleReader{z, f}, nil
		}
		return a, nil
	}
	return Artifact{}, ErrArtifactNotFound
}

type bundleReader struct {
	*gzip.Reader
	file fs.File
}

func (r *bundleReader) Close() error { return errors.Join(r.Reader.Close(), r.file.Close()) }

// BuildBundle publishes a complete, content-addressed directory under output.
// It returns that directory for embedding or use through os.DirFS. A failed
// build leaves earlier bundles intact. Sources default to GitHub.
func BuildBundle(ctx context.Context, spec BundleSpec, target Target, output string, sources ...Source) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	if err := target.validate(); err != nil {
		return "", err
	}
	entries, err := registry.LoadEmbedded()
	if err != nil {
		return "", err
	}
	catalog := NewCatalog(registry.Merge(entries, spec.CustomTools))
	if len(sources) == 0 {
		sources = []Source{GitHubSource{}}
	}
	names := make([]string, 0, len(spec.Tools))
	for name := range spec.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	if err := os.MkdirAll(output, 0755); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(output, ".bundle-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	manifest := bundleManifest{Format: 1, ID: spec.ID, Target: target}
	for _, name := range names {
		entry, ok := catalog.Find(name)
		if !ok {
			return "", fmt.Errorf("tool %q not found in registry", name)
		}
		a, err := resolve(ctx, sources, Request{entry, spec.Tools[name], target})
		if err != nil {
			return "", err
		}
		if err := compressArtifact(ctx, stage, &a); err != nil {
			return "", err
		}
		manifest.Tools = append(manifest.Tools, a)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), data, 0644); err != nil {
		return "", err
	}
	// Validate the finished format, including collisions after canonicalization.
	if _, err := OpenBundle(os.DirFS(stage)); err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	dest := filepath.Join(output, hex.EncodeToString(digest[:]))
	if _, err := os.Stat(dest); err == nil {
		// Do not reuse damaged build artifacts just because their directory exists.
		stored, err := os.ReadFile(filepath.Join(dest, "manifest.json"))
		if err != nil {
			return "", err
		}
		if !bytes.Equal(stored, data) {
			return "", fmt.Errorf("bundle manifest changed at %s", dest)
		}
		b, err := OpenBundle(os.DirFS(dest))
		if err != nil {
			return "", err
		}
		for _, a := range manifest.Tools {
			stored, err := b.Resolve(ctx, Request{a.Tool, a.Version, target})
			if err != nil {
				return "", err
			}
			if _, _, err := copyArtifact(ctx, io.Discard, stored); err != nil {
				return "", err
			}
		}
		return dest, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(stage, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func compressArtifact(ctx context.Context, dir string, a *Artifact) error {
	if !validToolName(a.Tool.Name) {
		return fmt.Errorf("invalid tool name %q", a.Tool.Name)
	}
	f, err := os.OpenFile(filepath.Join(dir, a.Tool.Name+".gz"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	z := gzip.NewWriter(f)
	n, digest, err := copyArtifact(ctx, z, *a)
	closeErr := z.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := f.Close(); err != nil {
		return err
	}
	a.Size, a.SHA256 = n, digest
	return nil
}
