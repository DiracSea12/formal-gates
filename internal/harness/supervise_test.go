package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// supervisorBinary builds the current source as an independent driver. Process
// ownership and SIGTERM cancellation require a separate process.
func supervisorBinary(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(file))) // internal/harness -> repo root
	name := "formal-gates"
	if runtime.GOOS == "windows" {
		name = "formal-gates.exe"
	}
	out := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-o", out, "./cmd/formal-gates")
	build.Dir = root
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build supervisor: %v\n%s", err, b)
	}
	return out
}

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func writeRequest(t *testing.T, dir string, req map[string]any) {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "request.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readReceipt(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "exit.json"))
	if err != nil {
		t.Fatalf("no exit.json in %s: %v", dir, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// baseEnv is the parent environment handed to the supervisor subprocess. Children
// are always invoked by absolute path, so PATH here is only used to confirm the
// merged-PATH resolution behaviour, not to locate the child.
func baseEnv(bin string) map[string]string {
	return map[string]string{
		"PATH":                        "/usr/bin:/bin",
		"HOME":                        "/tmp",
		"TMPDIR":                      "/private/tmp",
		"FORMAL_GATES_HARNESS_BINARY": bin,
	}
}

// runSupervisor spawns the supervisor over dir and returns its exit code.
func runSupervisor(t *testing.T, bin, dir string, env map[string]string) int {
	t.Helper()
	cmd := exec.Command(bin, "harness", "supervise", dir)
	if env != nil {
		cmd.Env = envSlice(env)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		t.Fatalf("supervisor run failed: %v\n%s", err, out)
	}
	return 0
}

// newJob returns a fresh job dir (with request.json written) and a work dir the
// child runs in. The two are deliberately distinct so a single request never
// shares a directory between two supervisors.
func newJob(t *testing.T, bin string, req map[string]any) (jobDir, workDir string, env map[string]string) {
	t.Helper()
	jobDir = t.TempDir()
	workDir = t.TempDir()
	req["cwd"] = workDir
	writeRequest(t, jobDir, req)
	return jobDir, workDir, baseEnv(bin)
}

// waitForFile blocks until path exists or 15s elapse, then fails the test. It is
// used to obtain POSITIVE proof that a child process actually started (the child
// touches path before entering its long sleep), which is strictly stronger than
// observing process.json: process.json is written before the supervisor arms its
// SIGTERM handler, so a signal sent on the latter's heels can kill the supervisor
// with no receipt. A running child can only exist after the handler is installed.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after 15s waiting for %s to appear (child never started?)", path)
}

// TestSuperviseOwnerExists proves a second supervisor for the same job backs off
// with exit 2, writes no receipt, no process.json, and never launches the child.
func TestSuperviseOwnerExists(t *testing.T) {
	bin := supervisorBinary(t)
	workDir := t.TempDir()
	jobDir := t.TempDir()
	// The child is never launched, so any platform's zero-side-effect command
	// works as the would-be argv; the driver's own --version needs no fixture.
	writeRequest(t, jobDir, map[string]any{
		"kind": "check", "argv": []string{bin, "--version"},
		"cwd": workDir, "stdin": "",
	})
	env := baseEnv(bin)
	// Pre-claim the owner. The handle is closed immediately: an open file would
	// keep Windows from removing the temp directory during cleanup.
	owner, err := os.OpenFile(filepath.Join(jobDir, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	owner.Close()
	rc := runSupervisor(t, bin, jobDir, env)
	if rc != 2 {
		t.Fatalf("supervisor exit %d, want 2", rc)
	}
	if _, err := os.Stat(filepath.Join(jobDir, "exit.json")); !os.IsNotExist(err) {
		t.Fatalf("exit.json should not exist")
	}
	if _, err := os.Stat(filepath.Join(jobDir, "process.json")); !os.IsNotExist(err) {
		t.Fatalf("process.json should not exist")
	}
	if _, err := os.Stat(filepath.Join(jobDir, "stdout.log")); !os.IsNotExist(err) {
		t.Fatalf("child must not have run (stdout.log exists)")
	}
}

// TestSuperviseReceiptShape asserts the receipt has exactly the six fields and the
// streams map exactly the two keys, and that request_digest equals the canonical
// digest of the request (reusing CanonicalDigest, no second hasher).
func TestSuperviseReceiptShape(t *testing.T) {
	bin := supervisorBinary(t)
	req := map[string]any{
		"kind": "check", "argv": []string{bin, "--version"}, "stdin": "x",
		"environment": map[string]any{"A": "b"},
	}
	jobDir, _, env := newJob(t, bin, req)
	_ = runSupervisor(t, bin, jobDir, env)
	rec := readReceipt(t, jobDir)

	wantFields := []string{"exit_code", "error", "started_at", "finished_at", "request_digest", "streams"}
	for _, f := range wantFields {
		if _, ok := rec[f]; !ok {
			t.Fatalf("receipt missing field %q; got %v", f, keysOf(rec))
		}
	}
	if len(rec) != len(wantFields) {
		t.Fatalf("receipt has %d fields, want %d: %v", len(rec), len(wantFields), keysOf(rec))
	}
	streams, ok := rec["streams"].(map[string]any)
	if !ok {
		t.Fatalf("streams not a map")
	}
	if len(streams) != 2 || streams["stdout.log"] == nil || streams["stderr.log"] == nil {
		t.Fatalf("streams keys = %v, want {stdout.log, stderr.log}", keysOf(streams))
	}
	for _, name := range []string{"stdout.log", "stderr.log"} {
		proof, ok := streams[name].(map[string]any)
		if !ok || proof["sha256"] == nil || proof["size"] == nil {
			t.Fatalf("proof %s = %v", name, proof)
		}
	}
	// request_digest must equal CanonicalDigest(request). The request is decoded
	// exactly as the supervisor decodes it (JSON with number fidelity), so we
	// round-trip through JSON+UseNumber before digesting.
	want, err := digestOf(t, req)
	if err != nil {
		t.Fatal(err)
	}
	if rec["request_digest"].(string) != want {
		t.Fatalf("request_digest = %q, want %q", rec["request_digest"].(string), want)
	}
}

// TestSuperviseIgnoresKind proves the supervisor does not read the request's kind
// field: two requests identical except kind produce identical receipts (the only
// field that legitimately differs is request_digest, which digests the whole
// request including kind).
func TestSuperviseIgnoresKind(t *testing.T) {
	bin := supervisorBinary(t)
	build := func(kind string) (string, map[string]string) {
		jobDir := t.TempDir()
		workDir := t.TempDir()
		req := map[string]any{
			"kind": kind, "argv": []string{bin, "--version"}, "cwd": workDir, "stdin": "",
			"environment": map[string]any{},
		}
		writeRequest(t, jobDir, req)
		return jobDir, baseEnv(bin)
	}
	d1, e1 := build("check")
	d2, e2 := build("native")
	_ = runSupervisor(t, bin, d1, e1)
	_ = runSupervisor(t, bin, d2, e2)
	r1 := readReceipt(t, d1)
	r2 := readReceipt(t, d2)
	d1digest := r1["request_digest"].(string)
	d2digest := r2["request_digest"].(string)
	if d1digest == d2digest {
		t.Fatalf("expected request_digest to differ by kind, both %q", d1digest)
	}
	// started_at and finished_at are non-deterministic (per the contract), so they
	// are normalised away; only request_digest legitimately differs by kind.
	for _, field := range []string{"request_digest", "started_at", "finished_at"} {
		delete(r1, field)
		delete(r2, field)
	}
	if fmt.Sprintf("%v", r1) != fmt.Sprintf("%v", r2) {
		t.Fatalf("receipt differs by more than request_digest:\n %v\n vs\n %v", r1, r2)
	}
}

// digestOf mirrors the supervisor's request_digest computation: decode the request
// JSON with number fidelity (as the supervisor does) and canonicalise it.
func digestOf(t *testing.T, req map[string]any) (string, error) {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return "", err
	}
	return CanonicalDigest(m)
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
