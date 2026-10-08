//go:build !windows

package harness

// Process-group cancellation semantics are POSIX-only (Setpgid/Getpgid/Kill
// have no Windows equivalents and the fixtures run /bin/sh), so these tests
// live behind a platform boundary; every other supported platform keeps them.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// The approved purposes retain the same successful executable and merged environment facts.
	for _, kind := range []string{"check", "approved-development-check", "approved-qa-check"} {
		t.Run("override/"+kind, func(t *testing.T) {
			jobDir, _, env := newJob(t, bin, map[string]any{
				"kind": kind, "argv": script, "stdin": "",
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
			if kind == "check" {
				return
			}
			tool, refusal := FileProof("/bin/sh")
			if refusal != "" {
				t.Fatal(refusal)
			}
			environment := map[string]any{}
			for key, value := range env {
				environment[key] = value
			}
			environment["MYVAR"] = "overridden"
			digest, err := CanonicalDigest(environment)
			if err != nil {
				t.Fatal(err)
			}
			if rec["tool_identity"] != "/bin/sh sha256:"+tool.SHA256 || rec["environment_identity"] != digest {
				t.Fatalf("receipt does not bind actual tool and merged environment: tool=%v environment=%v", rec["tool_identity"], rec["environment_identity"])
			}
		})
	}
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
