package facade

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

func dshCommand(executable, workspace, patch string) (*exec.Cmd, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(workspace) || !filepath.IsAbs(patch) {
		return nil, fmt.Errorf("DSH_CONFIG: executable, workspace and profile patch must be absolute")
	}
	cmd := exec.Command(executable, "--profile", "acp", "--patch", patch)
	cmd.Dir = workspace
	return cmd, nil
}
