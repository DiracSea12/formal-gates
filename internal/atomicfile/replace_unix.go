//go:build !windows

package atomicfile

import "os"

// Replace publishes a completed file by replacing the destination directory entry.
func Replace(source, destination string) error {
	return os.Rename(source, destination)
}
