package facade

import (
	"formal-gates/internal/coordination"
	"os/exec"
	"strconv"
)

func isolateCodexProcess(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return stopCodexProcess(cmd.Process.Pid) }
}
func codexProcessAlive(pid int) bool { return coordination.ProcessAlive(pid) }
func stopCodexProcess(pid int) error {
	if !codexProcessAlive(pid) {
		return nil
	}
	// The native Windows taskkill tree option also closes launcher descendants.
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		if !codexProcessAlive(pid) {
			return nil
		}
		return err
	}
	return nil
}
