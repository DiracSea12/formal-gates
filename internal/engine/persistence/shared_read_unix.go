//go:build !windows

package persistence

import "os"

func openSharedFile(path string) (*os.File, error) { return os.Open(path) }
