//go:build !windows

package validate

import (
	"os"

	"formal-gates/internal/atomicfile"
)

func replaceInstallBinary(completed, target, retired string) error {
	return atomicfile.Replace(completed, target)
}

func removeInstallBinary(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func retireInstallBinary(path, retired string) error { return removeInstallBinary(path) }

func installBinaryInUse(err error) bool { return false }

func deferOuterCleanup(path string, journal outerInstallJournal) error { return nil }
