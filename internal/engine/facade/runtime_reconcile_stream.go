package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/engine/protocol"
)

func (f *Facade) reconcileStreamNative(actionID string, x protocol.RegularExecution, observeIdentity func(string) (string, error), finish func(string, string, error) (Run, error)) (Run, bool, error) {
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
	identity := x.Identity
	if identity == "" {
		identity = process.Identity
	}
	if identity == "" && observeIdentity != nil {
		identity, err = observeIdentity(job)
		if err != nil {
			return Run{}, false, err
		}
	}
	if identity == "" {
		return Run{}, false, fmt.Errorf("UNKNOWN: native session identity not observed; no new session created")
	}
	if x.Identity == "" {
		if err := f.attachStreamIdentity(x.Claim, identity); err != nil {
			return Run{}, false, err
		}
	}
	if process.Stopped && process.ExitCode != nil {
		var executionErr error
		if *process.ExitCode != 0 {
			executionErr = fmt.Errorf("native process exited %d", *process.ExitCode)
		}
		if _, err := os.Stat(filepath.Join(job, "answer.json")); err != nil && executionErr == nil {
			if _, coverageErr := os.Stat(filepath.Join(job, "coverage-answer.json")); coverageErr == nil && x.Claim.Task.Step == "whitebox-design" {
				current, readErr := f.Show()
				if readErr != nil {
					return Run{}, false, readErr
				}
				if !protocol.WhiteboxCoverageReady(current.Executions[actionID]) {
					answer, readErr := os.ReadFile(filepath.Join(job, "coverage-answer.json"))
					if readErr != nil {
						return Run{}, false, readErr
					}
					bound := current.Executions[actionID].Claim
					bound.Identity = identity
					if readErr = f.commitNativeCoverage(current, bound, answer); readErr != nil {
						return Run{}, false, readErr
					}
				}
				run, showErr := f.Show()
				return run, false, showErr
			}
			return Run{}, false, fmt.Errorf("UNKNOWN: native answer unavailable after exit: %w", err)
		}
		run, err := finish(identity, job, executionErr)
		return run, true, err
	}
	run, err := f.Show()
	return run, false, err
}
