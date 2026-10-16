//go:build !windows

package validate

import "testing"

func observeInstallCleanup(t *testing.T, root string) func(...string) {
	return func(...string) {}
}
