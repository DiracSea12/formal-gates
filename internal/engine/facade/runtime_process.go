package facade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type nativeProcessResult struct {
	Identity string
	Answer   json.RawMessage
}

// runNativeStreamProcess preserves the engine-owned invocation and process
// evidence. Platform code supplies only the native stream exchange.
func runNativeStreamProcess(ctx context.Context, command *exec.Cmd, job, actionID string, exchange func(io.Reader, io.WriteCloser) (nativeProcessResult, error)) (nativeProcessResult, error) {
	var observed nativeProcessResult
	if command == nil || actionID == "" || !filepath.IsAbs(job) {
		return observed, fmt.Errorf("NATIVE_CONFIG: command, action and absolute invocation directory required")
	}
	cmd := exec.CommandContext(ctx, command.Path, command.Args[1:]...)
	cmd.Dir, cmd.Env = command.Dir, command.Env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return observed, err
	}
	defer stdin.Close()
	if err := os.MkdirAll(job, 0700); err != nil {
		return observed, err
	}
	journal, err := os.OpenFile(filepath.Join(job, "stdout.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return observed, err
	}
	defer journal.Close()
	stderr, err := os.OpenFile(filepath.Join(job, "stderr.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return observed, err
	}
	defer stderr.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return observed, err
	}
	cmd.Stderr = stderr
	// Reuse the existing OS process-group lifecycle helper, also used by task operations.
	isolateCodexProcess(cmd)
	if err := cmd.Start(); err != nil {
		return observed, err
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	process := map[string]any{"pid": cmd.Process.Pid, "actionId": actionID, "argv": cmd.Args, "cwd": cmd.Dir, "startedAt": started, "stopped": false}
	if err := writeJSONAtomic(filepath.Join(job, "process.json"), process); err != nil {
		_ = stopCodexProcess(cmd.Process.Pid)
		_ = cmd.Wait()
		return observed, err
	}
	// Durably journal bytes before exposing their native identity to the engine.
	reader := io.TeeReader(stdout, nativeStreamJournal{journal})
	observed, parseErr := exchange(reader, stdin)
	_ = stdin.Close()
	if parseErr != nil {
		_ = stopCodexProcess(cmd.Process.Pid)
	}
	// Preserve unread output on parse/attach failure; never launch a replacement.
	_, drainErr := io.Copy(io.Discard, reader)
	waitErr := cmd.Wait()
	syncErr := errors.Join(journal.Sync(), stderr.Sync())
	process["stopped"] = true
	process["identity"] = observed.Identity
	process["exitCode"] = cmd.ProcessState.ExitCode()
	process["completedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	persistErr := writeJSONAtomic(filepath.Join(job, "process.json"), process)
	if err := errors.Join(parseErr, drainErr, waitErr, syncErr, persistErr); err != nil {
		return observed, err
	}
	if err := os.WriteFile(filepath.Join(job, "answer.json"), observed.Answer, 0600); err != nil {
		return observed, err
	}
	return observed, nil
}

type nativeStreamJournal struct{ file *os.File }

func (j nativeStreamJournal) Write(data []byte) (int, error) {
	n, err := j.file.Write(data)
	if err != nil {
		return n, err
	}
	return n, j.file.Sync()
}
