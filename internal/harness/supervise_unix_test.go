//go:build !windows

package harness

// Process-group cancellation semantics are POSIX-only (Setpgid/Getpgid/Kill
// have no Windows equivalents and the fixtures run /bin/sh), so these tests
// live behind a platform boundary; every other supported platform keeps them.

import (
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestSuperviseCancel pins SIGTERM cancellation: the receipt carries exit_code
// 130 and the exact cancellation error, and the child is stopped.
func TestSuperviseCancel(t *testing.T) {
	bin := supervisorBinary(t)
	jobDir := t.TempDir()
	workDir := t.TempDir()
	marker := filepath.Join(jobDir, "child-started")
	writeRequest(t, jobDir, map[string]any{
		"kind":  "check",
		"argv":  []string{"/bin/sh", "-c", "touch " + marker + " && exec /bin/sleep 30"},
		"cwd":   workDir,
		"stdin": "",
	})
	cmd := exec.Command(bin, "harness", "supervise", jobDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = envSlice(baseEnv(bin))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Wait for POSITIVE proof the supervisor is past SIGTERM-handler installation:
	// the child touches child-started before exec'ing the long sleep, and handler
	// arming strictly precedes the first spawn. Waiting for process.json alone is
	// racy because it is written before the handler is armed; a signal landing in
	// that window kills the supervisor outright with no receipt (signal: terminated).
	waitForFile(t, marker)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 0 {
				t.Fatalf("supervisor exit error: %v", err)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("supervisor did not exit after cancel")
	}
	rec := readReceipt(t, jobDir)
	if rec["exit_code"].(float64) != 130 {
		t.Fatalf("exit_code = %v, want 130", rec["exit_code"])
	}
	if rec["error"].(string) != "owned job was cancelled" {
		t.Fatalf("error = %q, want 'owned job was cancelled'", rec["error"].(string))
	}
}

// TestSuperviseCancelGroup pins D2 determinism: production cancellation signals
// the supervisor's whole process group (Store.cancel_jobs uses os.killpg), which
// also reaches the child. Across many rounds the receipt must be the deterministic
// 130 / "owned job was cancelled" every single time, never the child's own
// signal death (-15) even though the group signal killed the child too.
func TestSuperviseCancelGroup(t *testing.T) {
	bin := supervisorBinary(t)
	const rounds = 20
	for round := 0; round < rounds; round++ {
		jobDir := t.TempDir()
		workDir := t.TempDir()
		marker := filepath.Join(jobDir, "child-started")
		writeRequest(t, jobDir, map[string]any{
			"kind":  "check",
			"argv":  []string{"/bin/sh", "-c", "touch " + marker + " && exec /bin/sleep 30"},
			"cwd":   workDir,
			"stdin": "",
		})
		cmd := exec.Command(bin, "harness", "supervise", jobDir)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Env = envSlice(baseEnv(bin))
		if err := cmd.Start(); err != nil {
			t.Fatalf("round %d: start: %v", round, err)
		}
		// Wait for POSITIVE proof the supervisor is past SIGTERM-handler
		// installation: the child touches child-started before exec'ing the long
		// sleep, and handler arming strictly precedes the first spawn. Waiting for
		// process.json alone is racy (it is written before the handler is armed), so
		// a group signal landing in that window killed the supervisor outright (no
		// exit.json, "signal: terminated") and made this test fail under load.
		waitForFile(t, marker)
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		if err != nil {
			t.Fatalf("round %d: getpgid: %v", round, err)
		}
		if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
			t.Fatalf("round %d: killpg: %v", round, err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 0 {
					t.Fatalf("round %d: supervisor exit error: %v", round, err)
				}
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("round %d: supervisor did not exit after cancel", round)
		}
		rec := readReceipt(t, jobDir)
		if rec["exit_code"].(float64) != 130 {
			t.Fatalf("round %d: exit_code = %v, want 130", round, rec["exit_code"])
		}
		if rec["error"].(string) != "owned job was cancelled" {
			t.Fatalf("round %d: error = %q, want 'owned job was cancelled'", round, rec["error"].(string))
		}
	}
}
