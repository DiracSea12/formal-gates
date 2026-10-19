package facade

import (
	"os"
	"path/filepath"
	"slices"
)

// BoundConfigurations maps each configuration name to the unfinished runs
// bound to it. A slice child counts under its master run; runs the engine
// cannot open are not counted.
func BoundConfigurations(root string) (map[string][]string, error) {
	bound := map[string][]string{}
	entries, err := os.ReadDir(filepath.Join(root, EngineNamespace))
	if os.IsNotExist(err) {
		return bound, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		f, err := Open(root, entry.Name())
		if err != nil {
			continue
		}
		run, err := f.Show()
		if err != nil || run.Status == "COMPLETE" || run.Status == "ABORTED" {
			continue
		}
		owner := run.RunID
		if run.MasterRunID != "" {
			owner = run.MasterRunID
		}
		name := run.Configuration.Name
		if !slices.Contains(bound[name], owner) {
			bound[name] = append(bound[name], owner)
		}
	}
	return bound, nil
}
