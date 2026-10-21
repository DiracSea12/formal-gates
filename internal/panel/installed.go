package panel

// The execution-platform "installed" verdict is the platform's own native
// path resolution: the same ResolveNativePaths resolver its selection and
// dispatch paths run, over the configured values. The verdict stays
// PATH-anchored - each resolver does the platform's own executable
// resolution, and a platform whose dispatch preconditions reach beyond one
// binary is installed only when they all resolve.

import (
	"maps"

	"formal-gates/internal/host"
)

func installedExecutables(native map[string]string, platforms []string) map[string]bool {
	installed := map[string]bool{}
	for _, name := range platforms {
		adapter, err := host.LookupAdapter(name)
		if err != nil {
			installed[name] = false
			continue
		}
		resolver, ok := adapter.(host.NativePathResolver)
		if !ok {
			installed[name] = false
			continue
		}
		resolved := maps.Clone(native)
		if resolved == nil {
			resolved = map[string]string{}
		}
		installed[name] = resolver.ResolveNativePaths(resolved, map[string]string{}) == nil
	}
	return installed
}
