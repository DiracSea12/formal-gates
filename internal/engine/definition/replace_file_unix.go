//go:build !windows

package definition

import "os"

func replaceCompletedFile(source, destination string) error {
	return os.Rename(source, destination)
}
