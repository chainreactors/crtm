package pkg

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// copyArtifact validates while streaming; no partially verified file is published.
func copyArtifact(ctx context.Context, dst io.Writer, a Artifact) (int64, string, error) {
	if a.Open == nil || !validToolName(a.Tool.Name) || a.Version == "" {
		return 0, "", fmt.Errorf("invalid artifact")
	}
	if err := a.Target.validate(); err != nil {
		return 0, "", err
	}
	if a.Size < 0 || a.Size > MaxBinarySize {
		return 0, "", fmt.Errorf("invalid artifact size")
	}
	r, err := a.Open(ctx)
	if err != nil {
		return 0, "", err
	}
	defer r.Close()
	reader := bufio.NewReader(contextReader{ctx, r})
	header, err := reader.Peek(4)
	if err != nil {
		return 0, "", err
	}
	if !isExecutableFor(header, a.Target.GOOS) {
		return 0, "", fmt.Errorf("%s: not a %s executable", a.Tool.Name, a.Target.GOOS)
	}
	limit := int64(MaxBinarySize)
	if a.Size > 0 {
		limit = a.Size
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(reader, limit+1))
	if err != nil {
		return 0, "", err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if n > limit || a.Size > 0 && n != a.Size {
		return 0, "", fmt.Errorf("%s: size mismatch", a.Tool.Name)
	}
	if a.SHA256 != "" && !strings.EqualFold(a.SHA256, digest) {
		return 0, "", fmt.Errorf("%s: SHA-256 mismatch", a.Tool.Name)
	}
	return n, digest, ctx.Err()
}

func stageArtifact(ctx context.Context, a Artifact, dir string, validate func(context.Context, string) error) (string, string, error) {
	if a.Target != CurrentTarget() {
		return "", "", fmt.Errorf("artifact targets %s, running %s", a.Target, CurrentTarget())
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	f, err := os.CreateTemp(dir, ".crtm-*.tmp")
	if err != nil {
		return "", "", err
	}
	path := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	_, digest, err := copyArtifact(ctx, f, a)
	if err != nil {
		return "", "", err
	}
	if err := f.Chmod(0755); err != nil {
		return "", "", err
	}
	if err := f.Sync(); err != nil {
		return "", "", err
	}
	if err := f.Close(); err != nil {
		return "", "", err
	}
	if validate != nil {
		if err := validate(ctx, path); err != nil {
			return "", "", fmt.Errorf("validate %s: %w", a.Tool.Name, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	ok = true
	return path, digest, nil
}

func fileDigest(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, contextReader{ctx, f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeAtomic(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".crtm-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(body); err != nil {
		return err
	}
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return replaceBinary(f.Name(), path)
}
