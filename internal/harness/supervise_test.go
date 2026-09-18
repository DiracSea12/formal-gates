package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// supervisorBinary returns the formal-gates driver, building it on the fly if the
// prebuilt binary is missing. Tests spawn it as an independent process because the
// supervisor's contract is process ownership, which an in-process call cannot
// demonstrate (and SIGTERM cancellation must not disturb the test process).
func supervisorBinary(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(file))) // internal/harness -> repo root
	bin := filepath.Join(root, ".harness", "bin", "formal-gates")
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	tmp, err := os.MkdirTemp("", "formal-gates-test-bin")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	out := filepath.Join(tmp, "formal-gates")
	// The package path is relative to the repository root, not this package's
	// directory; without Dir the build looks for internal/harness/cmd/... and
	// fails on any tree that has no prebuilt .harness binary (clean CI checkouts).
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

func TestSuperviseExitCodes(t *testing.T) {
	bin := supervisorBinary(t)
	cases := []struct {
		name string
		argv []string
		code int
	}{
		{"exit0", []string{"/usr/bin/true"}, 0},
		{"exit7", []string{"/bin/sh", "-c", "exit 7"}, 7},
		{"exit130explicit", []string{"/bin/sh", "-c", "exit 130"}, 130},
		{"sigkill", []string{"/bin/sh", "-c", "kill -9 $$"}, -9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			jobDir, _, env := newJob(t, bin, map[string]any{"kind": "check", "argv": c.argv, "stdin": ""})
			rc := runSupervisor(t, bin, jobDir, env)
			if rc != 0 {
				t.Fatalf("supervisor exit %d, want 0 (receipt written)", rc)
			}
			rec := readReceipt(t, jobDir)
			if got := int(rec["exit_code"].(float64)); got != c.code {
				t.Fatalf("exit_code = %d, want %d", got, c.code)
			}
			if c.name != "exit130explicit" {
				if rec["error"].(string) != "" {
					t.Fatalf("error = %q, want empty", rec["error"].(string))
				}
			}
		})
	}
}

func TestSuperviseExplicit130IsNotCancellation(t *testing.T) {
	bin := supervisorBinary(t)
	jobDir, _, env := newJob(t, bin, map[string]any{
		"kind": "check", "argv": []string{"/bin/sh", "-c", "exit 130"}, "stdin": "",
	})
	_ = runSupervisor(t, bin, jobDir, env)
	rec := readReceipt(t, jobDir)
	if rec["exit_code"].(float64) != 130 {
		t.Fatalf("exit_code = %v, want 130", rec["exit_code"])
	}
	if rec["error"].(string) != "" {
		t.Fatalf("error = %q, want empty (explicit exit 130 is not cancellation)", rec["error"].(string))
	}
}

func TestSuperviseStreamBytes(t *testing.T) {
	bin := supervisorBinary(t)
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"plain", []string{"/bin/sh", "-c", "printf OUT; printf ERR 1>&2"}, ""},
		{"no-trailing-newline", []string{"/bin/sh", "-c", "printf X"}, ""},
		{"multibyte", []string{"/bin/sh", "-c", "printf '\xc3\xa5\xc3\xa4\xc3\xb6'"}, ""},
		{"large", []string{"/bin/sh", "-c", "/usr/bin/head -c 100000 /dev/zero | /usr/bin/tr '\\0' 'A'"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			jobDir, _, env := newJob(t, bin, map[string]any{"kind": "check", "argv": c.argv, "stdin": ""})
			_ = runSupervisor(t, bin, jobDir, env)
			rec := readReceipt(t, jobDir)
			if rec["exit_code"].(float64) != 0 {
				t.Fatalf("exit_code = %v", rec["exit_code"])
			}
			stdout := readBytes(t, filepath.Join(jobDir, "stdout.log"))
			stderr := readBytes(t, filepath.Join(jobDir, "stderr.log"))
			// The exact split (OUT vs ERR, multibyte, 100KB) is asserted by the
			// differential test against the reference; here we only require the
			// streams were captured and the large case is genuinely large.
			if len(stdout)+len(stderr) == 0 && c.name != "no-trailing-newline" {
				t.Fatalf("no stream bytes captured for %s", c.name)
			}
			if c.name == "large" && len(stdout) != 100000 {
				t.Fatalf("large stdout = %d bytes, want 100000", len(stdout))
			}
		})
	}
}

func TestSuperviseStdin(t *testing.T) {
	bin := supervisorBinary(t)
	payloads := []string{"", "hello\nworld", "日本語\nmulti-byte", strings.Repeat("Z", 70000)}
	for _, p := range payloads {
		jobDir, _, env := newJob(t, bin, map[string]any{
			"kind": "check", "argv": []string{"/bin/cat"}, "stdin": p,
		})
		_ = runSupervisor(t, bin, jobDir, env)
		rec := readReceipt(t, jobDir)
		if rec["exit_code"].(float64) != 0 {
			t.Fatalf("exit_code = %v for payload len %d", rec["exit_code"], len(p))
		}
		stdout := readBytes(t, filepath.Join(jobDir, "stdout.log"))
		if string(stdout) != p {
			t.Fatalf("stdout = %q, want %q", stdout, p)
		}
	}
}

func TestSuperviseEnvironmentMerge(t *testing.T) {
	bin := supervisorBinary(t)
	script := []string{"/bin/sh", "-c", "printf '%s' \"$MYVAR\""}
	// override existing var
	t.Run("override", func(t *testing.T) {
		jobDir, _, env := newJob(t, bin, map[string]any{
			"kind": "check", "argv": script, "stdin": "",
			"environment": map[string]any{"MYVAR": "overridden"},
		})
		env["MYVAR"] = "base"
		_ = runSupervisor(t, bin, jobDir, env)
		rec := readReceipt(t, jobDir)
		if rec["exit_code"].(float64) != 0 {
			t.Fatalf("exit_code = %v", rec["exit_code"])
		}
		if got := string(readBytes(t, filepath.Join(jobDir, "stdout.log"))); got != "overridden" {
			t.Fatalf("stdout = %q, want overridden", got)
		}
	})
	// add new var
	t.Run("add", func(t *testing.T) {
		jobDir, _, env := newJob(t, bin, map[string]any{
			"kind": "check", "argv": script, "stdin": "",
			"environment": map[string]any{"MYVAR": "fresh"},
		})
		_ = runSupervisor(t, bin, jobDir, env)
		if got := string(readBytes(t, filepath.Join(jobDir, "stdout.log"))); got != "fresh" {
			t.Fatalf("stdout = %q, want fresh", got)
		}
	})
	// default empty environment
	t.Run("default", func(t *testing.T) {
		jobDir, _, env := newJob(t, bin, map[string]any{
			"kind": "check", "argv": script, "stdin": "",
			"environment": map[string]any{},
		})
		env["MYVAR"] = "fromparent"
		_ = runSupervisor(t, bin, jobDir, env)
		if got := string(readBytes(t, filepath.Join(jobDir, "stdout.log"))); got != "fromparent" {
			t.Fatalf("stdout = %q, want fromparent", got)
		}
	})
}

func TestSuperviseCwd(t *testing.T) {
	bin := supervisorBinary(t)
	jobDir, workDir, env := newJob(t, bin, map[string]any{
		"kind": "check", "argv": []string{"/bin/sh", "-c", "pwd"}, "stdin": "",
	})
	_ = runSupervisor(t, bin, jobDir, env)
	stdout := string(readBytes(t, filepath.Join(jobDir, "stdout.log")))
	// macOS resolves /var -> /private/var, so compare against the symlink-resolved
	// cwd, exactly as the reference implementation's child would observe it.
	wantCwd, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		wantCwd = workDir
	}
	if stdout != wantCwd+"\n" {
		t.Fatalf("cwd stdout = %q, want %q", stdout, wantCwd+"\n")
	}
}

func TestSuperviseSpawnErrors(t *testing.T) {
	bin := supervisorBinary(t)
	work := t.TempDir()

	cases := []struct {
		name    string
		setup   func(t *testing.T) (argv []string, cwd string)
		wantErr string
	}{
		{"argv0-missing", func(t *testing.T) (a []string, c string) {
			return []string{"/no/such/binary/xyz"}, work
		}, "FileNotFoundError: [Errno 2] No such file or directory: '/no/such/binary/xyz'"},
		{"argv0-notexec", func(t *testing.T) (a []string, c string) {
			p := filepath.Join(work, "noexec")
			os.WriteFile(p, []byte("x"), 0o644)
			return []string{p}, work
		}, fmt.Sprintf("PermissionError: [Errno 13] Permission denied: '%s'", filepath.Join(work, "noexec"))},
		{"argv0-dir", func(t *testing.T) (a []string, c string) {
			return []string{work}, work
		}, fmt.Sprintf("PermissionError: [Errno 13] Permission denied: '%s'", work)},
		{"argv0-textfile", func(t *testing.T) (a []string, c string) {
			p := filepath.Join(work, "txtscript")
			os.WriteFile(p, []byte("not a binary\n"), 0o755)
			return []string{p}, work
		}, fmt.Sprintf("OSError: [Errno 8] Exec format error: '%s'", filepath.Join(work, "txtscript"))},
		{"cwd-missing", func(t *testing.T) (a []string, c string) {
			return []string{"/usr/bin/true"}, filepath.Join(work, "nope")
		}, fmt.Sprintf("FileNotFoundError: [Errno 2] No such file or directory: '%s'", filepath.Join(work, "nope"))},
		{"cwd-file", func(t *testing.T) (a []string, c string) {
			f := filepath.Join(work, "afile")
			os.WriteFile(f, []byte("x"), 0o644)
			return []string{"/usr/bin/true"}, f
		}, fmt.Sprintf("NotADirectoryError: [Errno 20] Not a directory: '%s'", filepath.Join(work, "afile"))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			argv, cwd := c.setup(t)
			jobDir := t.TempDir()
			writeRequest(t, jobDir, map[string]any{"kind": "check", "argv": argv, "cwd": cwd, "stdin": ""})
			rc := runSupervisor(t, bin, jobDir, baseEnv(bin))
			if rc != 0 {
				t.Fatalf("supervisor exit %d, want 0", rc)
			}
			rec := readReceipt(t, jobDir)
			if rec["exit_code"].(float64) != 127 {
				t.Fatalf("exit_code = %v, want 127", rec["exit_code"])
			}
			if rec["error"].(string) != c.wantErr {
				t.Fatalf("error = %q, want %q", rec["error"].(string), c.wantErr)
			}
		})
	}
}

// TestSuperviseBareArgv0ResolvesMergedPath pins the highest-risk divergence:
// a bare argv[0] present only in the request's PATH must resolve against the
// MERGED environment, not the parent process's PATH.
func TestSuperviseBareArgv0ResolvesMergedPath(t *testing.T) {
	bin := supervisorBinary(t)
	tools := t.TempDir()
	script := filepath.Join(tools, "mytool")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Parent PATH must NOT contain tools; the request supplies it.
	env := baseEnv(bin)
	delete(env, "PATH")
	jobDir, _, _ := newJob(t, bin, map[string]any{
		"kind": "check", "argv": []string{"mytool"}, "stdin": "",
		"environment": map[string]any{"PATH": tools},
	})
	_ = runSupervisor(t, bin, jobDir, env)
	rec := readReceipt(t, jobDir)
	if rec["exit_code"].(float64) != 0 {
		t.Fatalf("exit_code = %v, want 0", rec["exit_code"])
	}
	if got := string(readBytes(t, filepath.Join(jobDir, "stdout.log"))); got != "hi\n" {
		t.Fatalf("stdout = %q, want hi\\n", got)
	}
}

// TestSuperviseBareArgv0FallsBackToDefpath pins the missing-PATH fallback to
// /bin:/usr/bin (os.defpath), so a bare name resolvable only there still runs.
func TestSuperviseBareArgv0FallsBackToDefpath(t *testing.T) {
	bin := supervisorBinary(t)
	env := baseEnv(bin)
	delete(env, "PATH") // merged environment has no PATH at all
	jobDir, _, _ := newJob(t, bin, map[string]any{
		"kind": "check", "argv": []string{"ls"}, "stdin": "",
		"environment": map[string]any{},
	})
	_ = runSupervisor(t, bin, jobDir, env)
	rec := readReceipt(t, jobDir)
	if rec["exit_code"].(float64) != 0 {
		t.Fatalf("exit_code = %v, want 0", rec["exit_code"])
	}
}

// TestSuperviseOwnerExists proves a second supervisor for the same job backs off
// with exit 2, writes no receipt, no process.json, and never launches the child.
func TestSuperviseOwnerExists(t *testing.T) {
	bin := supervisorBinary(t)
	workDir := t.TempDir()
	jobDir := t.TempDir()
	writeRequest(t, jobDir, map[string]any{
		"kind": "check", "argv": []string{"/bin/sh", "-c", "touch " + workDir + "/MARKER"},
		"cwd": workDir, "stdin": "",
	})
	env := baseEnv(bin)
	// Pre-claim the owner.
	if _, err := os.OpenFile(filepath.Join(jobDir, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err != nil {
		t.Fatal(err)
	}
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
	if _, err := os.Stat(workDir + "/MARKER"); !os.IsNotExist(err) {
		t.Fatalf("child must not have run (MARKER exists)")
	}
}

// TestSuperviseReceiptShape asserts the receipt has exactly the six fields and the
// streams map exactly the two keys, and that request_digest equals the canonical
// digest of the request (reusing CanonicalDigest, no second hasher).
func TestSuperviseReceiptShape(t *testing.T) {
	bin := supervisorBinary(t)
	req := map[string]any{
		"kind": "check", "argv": []string{"/usr/bin/true"}, "stdin": "x",
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
			"kind": kind, "argv": []string{"/usr/bin/true"}, "cwd": workDir, "stdin": "",
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
