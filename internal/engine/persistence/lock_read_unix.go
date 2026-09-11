//go:build !windows

package persistence

import "os"

func openLockFile(path string) (*os.File, error) { return os.Open(path) }
