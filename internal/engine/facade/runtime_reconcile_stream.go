package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
)

func (f *Facade) reconcileNative(actionID string, x protocol.RegularExecution, adapter nativeExecutionTransport) (Run, bool, error) {
	if !x.Claimed {
		return Run{}, false, fmt.Errorf("INTENT_MISMATCH: unclaimed action cannot be resumed")
	}
	if x.Finished && x.Source != nil && x.Source.Status != "RUNNING" {
		return Run{}, false, fmt.Errorf("DUPLICATE_ACTION: completed execution cannot be resumed")
	}
	job := f.codexJob(actionID)
	if x.ResumeClaimed || x.ResumeCount > 0 {
		job = codexResumeJob(job, x.ResumeCount)
	}
	invocationRoot := job
	job, err := nativeProcessDirectory(job, string(x.Claim.Task.Step))
	if err != nil {
		return Run{}, false, fmt.Errorf("UNKNOWN: original native process evidence unavailable: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(job, "process.json"))
	if err != nil {
		return Run{}, false, fmt.Errorf("UNKNOWN: original native process evidence unavailable: %w", err)
	}
	var process struct {
		ActionID string `json:"actionId"`
		Identity string `json:"identity"`
		PID      int    `json:"pid"`
		Stopped  bool   `json:"stopped"`
		ExitCode *int   `json:"exitCode"`
	}
	if json.Unmarshal(data, &process) != nil || process.ActionID != actionID || process.PID <= 0 {
		return Run{}, false, fmt.Errorf("UNKNOWN: original native process binding differs")
	}
	if !process.Stopped && codexProcessAlive(process.PID) {
		return Run{}, false, fmt.Errorf("HOST_ACTION_RUNNING: original native process is still running")
	}
	if run, err := f.Show(); err == nil && process.Stopped && checkpointCanResume(run, x) {
		return run, false, nil
	}
	identity := x.Identity
	if identity == "" {
		identity = process.Identity
	}
	if identity == "" {
		identity, err = adapter.readIdentity(job, actionID)
		if err != nil {
			return Run{}, false, err
		}
	}
	if identity == "" {
		return Run{}, false, fmt.Errorf("UNKNOWN: native identity not observed; no new session created")
	}
	if x.Identity == "" {
		if err := f.attachStreamIdentity(x.Claim, identity); err != nil {
			return Run{}, false, err
		}
	}
	// Directory identity distinguishes the no-tools preflight from authoring,
	// even if interruption occurred before the coverage operation was accepted.
	if job == filepath.Join(invocationRoot, "coverage") && process.Stopped && (process.ExitCode == nil || *process.ExitCode == 0) {
		if err := f.recordNativeCoverage(x.Claim, identity, job, adapter); err != nil {
			return Run{}, false, err
		}
		run, err := f.Show()
		return run, false, err
	}
	var executionErr error
	if process.Stopped && process.ExitCode != nil && *process.ExitCode != 0 {
		executionErr = fmt.Errorf("native process exited %d", *process.ExitCode)
	}
	_, answerErr := os.Stat(filepath.Join(job, "answer.json"))
	if answerErr == nil || executionErr != nil || x.ResumeClaimed {
		if answerErr != nil && executionErr == nil {
			executionErr = fmt.Errorf("interrupted native resume has no result")
		}
		run, err := f.collectNativeResult(actionID, x.Claim, identity, job, executionErr, adapter)
		return run, true, err
	}
	if process.Stopped && process.ExitCode != nil {
		return Run{}, false, fmt.Errorf("UNKNOWN: native answer unavailable after exit: %w", answerErr)
	}
	run, err := f.Show()
	return run, false, err
}

// Main and preflight have separate directories. Only the common process receipt
// is observed here; all other invocation files are opaque platform-owned data.
func nativeProcessDirectory(job, step string) (string, error) {
	if _, err := os.Stat(filepath.Join(job, "process.json")); err == nil {
		return job, nil
	} else if !os.IsNotExist(err) || step != "whitebox-design" {
		return "", err
	}
	coverage := filepath.Join(job, "coverage")
	if _, err := os.Stat(filepath.Join(coverage, "process.json")); err != nil {
		return "", err
	}
	return coverage, nil
}
