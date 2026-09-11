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
)

// runWorkBuddyProcess consumes an adapter-built command only after its caller
// has claimed the action. The job directory is the engine-assigned invocation
// directory. Logs and process completion are kept even when result parsing fails.
func runWorkBuddyProcess(ctx context.Context, command *exec.Cmd, job, actionID, expectedIdentity, prompt string, attached func(string) error) (workBuddyResult, error) {
	result, err := runNativeStreamProcess(ctx, command, job, actionID, func(reader io.Reader, stdin io.WriteCloser) (nativeProcessResult, error) {
		_, writeErr := io.WriteString(stdin, prompt)
		closeErr := stdin.Close()
		result, err := observeWorkBuddyStream(reader, expectedIdentity, attached)
		return nativeProcessResult{Identity: result.Identity, Answer: result.Answer}, errors.Join(writeErr, closeErr, err)
	})
	return workBuddyResult{Identity: result.Identity, Answer: result.Answer}, err
}

// recoverWorkBuddyProcess only observes the recorded invocation. A running or
// incompletely recorded process is UNKNOWN, never permission to start again.
func recoverWorkBuddyProcess(job, actionID, identity string) (workBuddyResult, error) {
	var result workBuddyResult
	data, err := os.ReadFile(filepath.Join(job, "process.json"))
	if err != nil {
		return result, fmt.Errorf("UNKNOWN: WorkBuddy process record: %w", err)
	}
	var process struct {
		ActionID string `json:"actionId"`
		Stopped  bool   `json:"stopped"`
		ExitCode *int   `json:"exitCode"`
	}
	if err := json.Unmarshal(data, &process); err != nil {
		return result, fmt.Errorf("UNKNOWN: WorkBuddy process record: %w", err)
	}
	if actionID == "" || process.ActionID != actionID {
		return result, fmt.Errorf("INTENT_MISMATCH: WorkBuddy invocation action differs")
	}
	if !process.Stopped || process.ExitCode == nil {
		return result, fmt.Errorf("UNKNOWN: WorkBuddy process completion is not recorded")
	}
	if *process.ExitCode != 0 {
		return result, fmt.Errorf("WORKBUDDY_PROCESS: recorded exit code %d", *process.ExitCode)
	}
	stream, err := os.Open(filepath.Join(job, "stdout.jsonl"))
	if err != nil {
		return result, err
	}
	defer stream.Close()
	return observeWorkBuddyResult(stream, identity)
}
