//go:build !windows

package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSuperviseCheckRunsUntilCompletion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	command := "printf 'before completion'; sleep 60 & child=$!; printf '%s' \"$child\" > child.pid; sleep 0.25; printf ' after completion'"
	writeRequest(t, dir, map[string]any{"kind": "approved-development-check", "argv": []string{"/bin/sh", "-c", command}, "cwd": dir, "timeout_nanos": int64(120 * time.Millisecond)})
	cmd := exec.Command(supervisorBinary(t), "harness", "supervise", dir)
	cmd.SysProcAttr = supervisorSysProcAttr()
	started := time.Now()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("supervisor: %v %s", err, out)
	}
	receipt := readReceipt(t, dir)
	if receipt["exit_code"] != float64(0) || receipt["error"] != "" || time.Since(started) < 120*time.Millisecond {
		t.Fatalf("unbounded receipt: %#v elapsed %s", receipt, time.Since(started))
	}
	var process map[string]any
	value, err := readJSONFile(filepath.Join(dir, "process.json"))
	if err != nil {
		t.Fatal(err)
	}
	process = value.(map[string]any)
	if process["pgid"] == nil || process["request_digest"] == nil || process["deadline_at"] != nil {
		t.Fatalf("unbounded process identity: %#v", process)
	}
	pidBytes, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil || pid <= 0 {
		t.Fatalf("actual descendant pid %q: %v", pidBytes, err)
	}
	status, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if state := strings.TrimSpace(string(status)); state != "" && !strings.HasPrefix(state, "Z") {
		t.Fatalf("completed check left descendant %d executing: %s", pid, state)
	}
	t.Logf("actual_descendant_pid=%d after_completion_state=%q", pid, strings.TrimSpace(string(status)))
	out, err := os.ReadFile(filepath.Join(dir, "stdout.log"))
	if err != nil || string(out) != "before completion after completion" {
		t.Fatalf("original completion output %q: %v", out, err)
	}
	streams := receipt["streams"].(map[string]any)
	kind, refusal := VerifyProcess(dir, receipt, receipt, map[string]any{"kind": "approved-development-check", "argv": []any{"/bin/sh", "-c", command}, "cwd": dir, "timeout_nanos": int64(120 * time.Millisecond)}, []Proof{{Name: "stdout.log", Expected: streams["stdout.log"].(map[string]any)}, {Name: "stderr.log", Expected: streams["stderr.log"].(map[string]any)}})
	if refusal != "" {
		t.Fatalf("completion proof %s: %s", kind, refusal)
	}
}
