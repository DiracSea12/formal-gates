//go:build !windows

package atomicfile

import "os"

// ReadFile reads a published file while permitting concurrent replacement.
func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
