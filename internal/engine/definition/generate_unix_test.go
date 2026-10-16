//go:build !windows

package definition

import "os"

func openGeneratedReader(path string) (*os.File, error) { return os.Open(path) }
