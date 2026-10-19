package validate

import (
	"os"
	"path/filepath"
	"runtime"
)

func devinInstallTargetPaths(home, project, scope string) (base, hookConfig, managedRulePath string, err error) {
	config := filepath.Join(project, ".devin")
	managedRulePath = filepath.Join(project, "AGENTS.md")
	if scope == "global" {
		root := os.Getenv("XDG_CONFIG_HOME")
		if root == "" {
			root = filepath.Join(home, ".config")
		}
		if runtime.GOOS == "windows" {
			root = os.Getenv("APPDATA")
			if root == "" {
				root = filepath.Join(home, "AppData", "Roaming")
			}
		}
		config = filepath.Join(root, "devin")
		managedRulePath = filepath.Join(config, "AGENTS.md")
	}
	return filepath.Join(config, "skills"), filepath.Join(config, "config.json"), managedRulePath, nil
}
