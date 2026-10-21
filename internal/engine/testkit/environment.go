package testkit

import (
	"strings"

	"formal-gates/internal/lifecycle"
)

// HostFilteredEnvironment removes every host identity variable owned by the
// platform layer from base, so a subprocess under test observes the default
// provider; extra entries are appended after filtering.
func HostFilteredEnvironment(base []string, extra ...string) []string {
	hostKeys := map[string]bool{}
	for _, key := range lifecycle.ProviderEnvironmentKeys() {
		hostKeys[key] = true
	}
	filtered := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		if key, _, _ := strings.Cut(entry, "="); !hostKeys[key] {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, extra...)
}
