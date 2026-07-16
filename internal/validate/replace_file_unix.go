//go:build !windows

package validate

import "os"

func replaceCompletedFile(source, destination string) error {
	return os.Rename(source, destination)
}
