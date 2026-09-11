//go:build !windows

package persistence

import "os"

func openSharedFile(path string) (*os.File, error) { return os.Open(path) }

func createLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}

func replaceStateFile(source, target string) error { return os.Rename(source, target) }
