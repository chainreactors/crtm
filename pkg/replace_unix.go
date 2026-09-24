//go:build !windows

package pkg

import "os"

func replaceBinary(source, target string) error { return os.Rename(source, target) }
