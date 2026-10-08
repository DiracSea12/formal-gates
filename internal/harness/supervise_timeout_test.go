//go:build !windows

package harness

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSuperviseDeadlineOwnsExecution(t *testing.T) {
	dir := t.TempDir()
	writeRequest(t, dir, map[string]any{"argv": []string{"/bin/sh", "-c", "printf 'before timeout'; sleep 10"}, "cwd": dir, "timeout_nanos": int64(120 * time.Millisecond)})
	cmd := exec.Command(supervisorBinary(t), "harness", "supervise", dir)
	cmd.SysProcAttr = supervisorSysProcAttr()
	started := time.Now()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("supervisor: %v %s", err, out)
	}
	receipt := readReceipt(t, dir)
	if receipt["exit_code"] != float64(124) || receipt["error"] != "owned job exceeded its timeout" || time.Since(started) > 2*time.Second {
		t.Fatalf("timeout receipt: %#v elapsed %s", receipt, time.Since(started))
	}
	var process map[string]any
	value, err := readJSONFile(filepath.Join(dir, "process.json"))
	if err != nil {
		t.Fatal(err)
	}
	process = value.(map[string]any)
	if process["pgid"] == nil || process["deadline_at"] == nil {
		t.Fatalf("timeout process identity: %#v", process)
	}
	streams := receipt["streams"].(map[string]any)
	kind, refusal := VerifyProcess(dir, receipt, receipt, map[string]any{"argv": []any{"/bin/sh", "-c", "printf 'before timeout'; sleep 10"}, "cwd": dir, "timeout_nanos": int64(120 * time.Millisecond)}, []Proof{{Name: "stdout.log", Expected: streams["stdout.log"].(map[string]any)}, {Name: "stderr.log", Expected: streams["stderr.log"].(map[string]any)}})
	if refusal != "" {
		t.Fatalf("timeout proof %s: %s", kind, refusal)
	}
}
