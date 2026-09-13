package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

func (f *Facade) taskOperationDir(id string) string {
	return filepath.Join(f.stateDir, "artifacts", "task-operations", strings.TrimPrefix(id, "hact:task:"))
}

// ExecuteTaskOperation serves the CLI and native dynamic tool using one path.
// Claim/CAS precedes process creation; a claimed operation is reconciled, never
// blindly retried. Its files are native observations, not another task ledger.
func (f *Facade) ExecuteTaskOperation(q protocol.TaskOperationRequest, resume bool) (protocol.TaskOperationReceipt, error) {
	id := protocol.TaskOperationID(q)
	job := f.taskOperationDir(id)
	unlock, err := coordination.AcquirePath(job+".lock", "task operation")
	if err != nil {
		return protocol.TaskOperationReceipt{}, err
	}
	defer unlock()
	run, err := f.Show()
	if err != nil {
		return protocol.TaskOperationReceipt{}, err
	}
	if run.Regular == nil {
		return protocol.TaskOperationReceipt{}, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: task operations require regular")
	}
	x := run.Executions[q.Binding.ActionID]
	if q.Kind == "model.run" {
		probe := &exec.Cmd{Args: []string{"native-model"}}
		if err := runtimeCommandContext(probe, q.Binding.Context); err != nil {
			return protocol.TaskOperationReceipt{}, err
		}
	}
	var op protocol.TaskOperation
	if resume {
		var ok bool
		op, ok = x.Operations[id]
		if !ok || protocol.TaskOperationDigest(op.Request) != protocol.TaskOperationDigest(q) {
			return protocol.TaskOperationReceipt{}, fmt.Errorf("INTENT_MISMATCH: recovery requires the original request")
		}
		if op.Receipt != nil {
			if err := f.releaseCollectedResources(run); err != nil {
				return *op.Receipt, err
			}
			return *op.Receipt, nil
		}
	} else {
		event, err := protocol.NewRegularEvent(protocol.EventID("task-claim:"+id), protocol.RegularEvent{Operation: "TASK_OPERATION_CLAIM", ExpectedRevision: q.ExpectedRevision, TaskOperation: &q})
		if err != nil {
			return protocol.TaskOperationReceipt{}, err
		}
		_, run, err = f.Submit(event)
		if err != nil {
			return protocol.TaskOperationReceipt{}, err
		}
		// Event replay is not admission to execute again, even when its original
		// acceptance was idempotent. Only this call's unclaimed transition may run.
		if _, already := x.Operations[id]; already {
			return protocol.TaskOperationReceipt{}, fmt.Errorf("DUPLICATE_ACTION: operation was already claimed")
		}
		x = run.Executions[q.Binding.ActionID]
		op = x.Operations[id]
	}
	receiptPath := filepath.Join(job, "observed.json")
	var receipt protocol.TaskOperationReceipt
	if data, readErr := os.ReadFile(receiptPath); readErr == nil {
		if err = json.Unmarshal(data, &receipt); err != nil {
			return receipt, err
		}
	} else if !os.IsNotExist(readErr) {
		return receipt, readErr
	} else if resume {
		receipt, err = f.observeTaskOperation(op, job)
		if err != nil {
			return receipt, err
		}
		if err = writeJSONAtomic(receiptPath, receipt); err != nil {
			return receipt, err
		}
	} else {
		if err = os.MkdirAll(job, 0700); err != nil {
			return receipt, err
		}
		// Persist the call boundary before invoking. If the driver dies between this
		// marker and native attach, absence of a result is UNKNOWN, not permission to
		// execute again. This is the same intent discipline as outer dispatch.
		op.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err = writeJSONAtomic(filepath.Join(job, "request.json"), op); err != nil {
			return receipt, err
		}
		receipt = f.performTaskOperation(run, op, job)
		if receipt.Status == "COMPLETED" && f.fault != nil {
			if err = f.fault(persistence.FaultExecuteAfter); err != nil {
				return receipt, err
			}
		}
		if err = writeJSONAtomic(receiptPath, receipt); err != nil {
			return receipt, err
		}
	}
	if receipt.ID != id || receipt.RequestDigest != protocol.TaskOperationDigest(q) {
		return receipt, fmt.Errorf("INTENT_MISMATCH: observed result differs")
	}
	_, run, err = f.submitAdapterRegular("task-receipt:"+id, protocol.RegularEvent{Operation: "TASK_OPERATION_RECEIPT", TaskOperationReceipt: &receipt})
	if err == nil {
		err = f.releaseCollectedResources(run)
	}
	return receipt, err
}

func (f *Facade) performTaskOperation(run Run, op protocol.TaskOperation, job string) (result protocol.TaskOperationReceipt) {
	q := op.Request
	result = protocol.TaskOperationReceipt{ID: op.ID, ActionID: q.Binding.ActionID, RequestDigest: protocol.TaskOperationDigest(q), Status: "FAILED", StartedAt: op.StartedAt}
	defer func() { result.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano) }()
	fail := func(err error) protocol.TaskOperationReceipt {
		result.Status = "FAILED"
		result.Message = err.Error()
		return result
	}
	ctx := context.Background()
	switch q.Kind {
	case "qa.prepare":
		result.Workspace = f.operationResources(op).Work
		if err := f.createOperationResources(op); err != nil {
			return fail(err)
		}
		if err := f.prepareShared(ctx, run, op, &result); err != nil {
			return fail(err)
		}

		if op.WorkspaceMode == "isolated" {
			if _, err := os.Stat(result.Workspace); err == nil {
				return fail(fmt.Errorf("UNKNOWN: case workspace already exists"))
			} else if !os.IsNotExist(err) {
				return fail(err)
			}
			if err := os.MkdirAll(result.Workspace, 0700); err != nil {
				return fail(err)
			}
			result.Output = "Prepared isolated case work for candidate " + q.Binding.Candidate
		} else if q.Binding.Task.Step == "repair-worker" {
			source := run.Regular.Preparation.Resources[q.Binding.PrerequisiteDigest]
			if source == "" {
				return fail(fmt.Errorf("PREREQUISITE_FAILED: missing registered repair workspace"))
			}
			paths, err := gitprep.WorkingCopy(f.root, source, result.Workspace, q.Binding.Candidate)
			if err != nil {
				return fail(err)
			}
			result.Output = fmt.Sprintf("Prepared repair workspace %s against candidate %s; copied changes %v", source, q.Binding.Candidate, paths)
		} else {
			if err := gitprep.Workspace(f.root, result.Workspace, q.Binding.Candidate); err != nil {
				return fail(err)
			}
			result.Output = "Prepared immutable candidate " + q.Binding.Candidate
		}
		result.Status = "COMPLETED"
		// HEAD alone proves only the baseline. This observation proves that the
		// repair overlay finished too, so recovery never accepts a partial copy.
		if err := writeJSONAtomic(filepath.Join(job, "prepared.json"), result); err != nil {
			return fail(err)
		}
	case "qa.target":
		if prior := latestTaskOperation(run.Executions[q.Binding.ActionID], "qa.prepare", q.CaseID); prior != nil && prior.Receipt != nil {
			result.Workspace = prior.Receipt.Workspace
			result.InputDigest = prior.Receipt.InputDigest
			result.SharedWorkspace = prior.Receipt.SharedWorkspace
			result.PreparationReused = prior.Receipt.PreparationReused
		}
		if result.Workspace == "" {
			return fail(fmt.Errorf("PREREQUISITE_FAILED: missing prepared workspace"))
		}
		if op.WorkspaceMode != "isolated" {
			head, err := gitprep.Head(result.Workspace)
			if err != nil {
				return fail(err)
			}
			if head != q.Binding.Candidate {
				return fail(fmt.Errorf("EXTERNAL_CHANGED: prepared QA candidate differs"))
			}
		}
		cmd := exec.CommandContext(ctx, op.Command.Executable, op.Command.Args...)
		cmd.Dir = result.Workspace
		prepare := latestTaskOperation(run.Executions[q.Binding.ActionID], "qa.prepare", q.CaseID)
		cmd.Env = f.operationEnvironment(*prepare, result.SharedWorkspace)
		code, err := runOperationProcess(cmd, job, &result)
		result.ExitCode = code
		if err != nil {
			return fail(err)
		}
		// Nonzero target exits are observable acceptance data. Invocation failures
		// are FAILED prerequisites; an observed program exit can still be collected.
		result.Status = "COMPLETED"
	case "qa.collect":
		target := latestTaskOperation(run.Executions[q.Binding.ActionID], "qa.target", q.CaseID)
		if target == nil || target.Receipt == nil {
			return fail(fmt.Errorf("PREREQUISITE_FAILED: no completed target"))
		}
		data, err := os.ReadFile(filepath.Join(f.taskOperationDir(target.ID), "stdout.log"))
		if err != nil {
			return fail(err)
		}
		result.Output = string(data)
		stderr, err := os.ReadFile(filepath.Join(f.taskOperationDir(target.ID), "stderr.log"))
		if err != nil {
			return fail(err)
		}
		result.Stderr = string(stderr)
		result.ExitCode = target.Receipt.ExitCode
		result.Workspace = target.Receipt.Workspace
		result.Status = "COMPLETED"
	case "whitebox.coverage":
		result.Output = q.Prompt
		result.Status = "COMPLETED"
	case "model.run":
		adapter, err := runtimeAdapter(q.Binding.Provider)
		if err != nil {
			return fail(err)
		}
		return adapter.runModel(ctx, run.Regular.Settings.Values, op, job, result)
	default:
		return fail(fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: unknown task operation"))
	}
	return result
}

func runOperationProcess(cmd *exec.Cmd, job string, result *protocol.TaskOperationReceipt) (int, error) {
	out, err := os.Create(filepath.Join(job, "stdout.log"))
	if err != nil {
		return -1, err
	}
	defer out.Close()
	stderr, err := os.Create(filepath.Join(job, "stderr.log"))
	if err != nil {
		return -1, err
	}
	defer stderr.Close()
	cmd.Stdout = out
	cmd.Stderr = stderr
	isolateCodexProcess(cmd)
	if err = cmd.Start(); err != nil {
		return -1, err
	}
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err = writeJSONAtomic(filepath.Join(job, "process.json"), map[string]any{"pid": cmd.Process.Pid, "argv": cmd.Args, "cwd": cmd.Dir, "stopped": false, "startedAt": startedAt}); err != nil {
		_ = stopCodexProcess(cmd.Process.Pid)
		_ = cmd.Wait()
		return -1, err
	}
	waitErr := cmd.Wait()
	_ = out.Sync()
	_ = stderr.Sync()
	code := cmd.ProcessState.ExitCode()
	if err = writeJSONAtomic(filepath.Join(job, "process.json"), map[string]any{"pid": cmd.Process.Pid, "argv": cmd.Args, "cwd": cmd.Dir, "stopped": true, "exitCode": code, "startedAt": startedAt, "completedAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		return code, err
	}
	if waitErr != nil {
		if _, ok := waitErr.(*exec.ExitError); !ok {
			return code, waitErr
		}
	}
	return code, nil
}

// observeTaskOperation recovers only facts already completed by the provider or
// registered workspace. Missing/unfinished observations remain UNKNOWN. It has
// no branch that calls a model, clones a workspace or executes a target again.
func (f *Facade) observeTaskOperation(op protocol.TaskOperation, job string) (protocol.TaskOperationReceipt, error) {
	q := op.Request
	r := protocol.TaskOperationReceipt{ID: op.ID, ActionID: q.Binding.ActionID, RequestDigest: protocol.TaskOperationDigest(q), Status: "COMPLETED"}
	unknown := func(err error) (protocol.TaskOperationReceipt, error) {
		return r, fmt.Errorf("UNKNOWN: %s; no underlying call repeated: %w", op.ID, err)
	}
	if q.Kind == "qa.prepare" {
		r.Workspace = f.operationResources(op).Work
		if op.WorkspaceMode != "isolated" {
			head, err := gitprep.Head(r.Workspace)
			if err != nil {
				return unknown(err)
			}
			if head != q.Binding.Candidate {
				return unknown(fmt.Errorf("candidate workspace differs"))
			}
		}
		data, err := os.ReadFile(filepath.Join(job, "prepared.json"))
		if err != nil {
			return unknown(err)
		}
		if err = json.Unmarshal(data, &r); err != nil {
			return unknown(err)
		}
		return r, nil
	}
	if q.Kind == "qa.collect" {
		run, err := f.Show()
		if err != nil {
			return unknown(err)
		}
		return f.performTaskOperation(run, op, job), nil
	}
	data, err := os.ReadFile(filepath.Join(job, "process.json"))
	if err != nil {
		return unknown(err)
	}
	var process struct {
		Stopped  bool   `json:"stopped"`
		ExitCode int    `json:"exitCode"`
		Cwd      string `json:"cwd"`
	}
	if err = json.Unmarshal(data, &process); err != nil {
		return unknown(err)
	}
	if !process.Stopped {
		return unknown(fmt.Errorf("native process completion is not observed"))
	}
	r.ExitCode = process.ExitCode
	r.Workspace = process.Cwd
	if q.Kind == "qa.target" {
		return r, nil
	}
	if q.Kind != "model.run" {
		return unknown(fmt.Errorf("unsupported operation"))
	}
	adapter, err := runtimeAdapter(q.Binding.Provider)
	if err != nil {
		return unknown(err)
	}
	return adapter.observeModel(op, job, r)
}

func (f *Facade) verifyTaskOperationReceipt(event protocol.Event) error {
	if event.Regular == nil || event.Regular.TaskOperationReceipt == nil {
		return nil
	}
	v := event.Regular.TaskOperationReceipt
	data, err := os.ReadFile(filepath.Join(f.taskOperationDir(v.ID), "observed.json"))
	if err != nil {
		return fmt.Errorf("task receipt requires the adapter's native observation: %w", err)
	}
	var actual protocol.TaskOperationReceipt
	if json.Unmarshal(data, &actual) != nil || gitprep.Digest(actual) != gitprep.Digest(v) {
		return fmt.Errorf("INTENT_MISMATCH: task receipt differs from observed operation")
	}
	return nil
}

func latestTaskOperation(x protocol.RegularExecution, kind, caseID string) *protocol.TaskOperation {
	var latest *protocol.TaskOperation
	for _, op := range x.Operations {
		if op.Request.Kind == kind && op.Request.CaseID == caseID && (latest == nil || op.ClaimedRevision > latest.ClaimedRevision) {
			copy := op
			latest = &copy
		}
	}
	return latest
}

// Abort reconciles the same owned process before releasing an internal intent.
// A missing native observation remains UNKNOWN; it never authorizes another call.
func (f *Facade) abortTaskOperations(run Run) error {
	for _, x := range run.Executions {
		for _, op := range x.Operations {
			if op.Receipt != nil {
				continue
			}
			job := f.taskOperationDir(op.ID)
			if op.Request.Kind == "qa.prepare" {
				if err := f.stopPreparationProcesses(job); err != nil {
					return err
				}
			}
			if op.Request.Kind == "qa.target" || op.Request.Kind == "model.run" {
				data, err := os.ReadFile(filepath.Join(job, "process.json"))
				if err != nil {
					return fmt.Errorf("UNKNOWN: abort needs task process observation: %w", err)
				}
				var p struct {
					PID     int  `json:"pid"`
					Stopped bool `json:"stopped"`
				}
				if err = json.Unmarshal(data, &p); err != nil || p.PID <= 0 {
					return fmt.Errorf("UNKNOWN: invalid task process observation")
				}
				if !p.Stopped && codexProcessAlive(p.PID) {
					if err = stopCodexProcess(p.PID); err != nil {
						return err
					}
					deadline := time.Now().Add(3 * time.Second)
					for codexProcessAlive(p.PID) && time.Now().Before(deadline) {
						time.Sleep(20 * time.Millisecond)
					}
					if codexProcessAlive(p.PID) {
						return fmt.Errorf("HOST_ACTION_RUNNING: task process is stopping")
					}
				}
			}
			// The stopped process's original driver may still be persisting its
			// final observation. Let that owner finish before reconciling here.
			var unlock func()
			var err error
			deadline := time.Now().Add(3 * time.Second)
			for {
				unlock, err = coordination.AcquirePath(job+".lock", "task operation")
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("HOST_ACTION_RUNNING: original task driver is still settling: %w", err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			current, err := f.Show()
			if err == nil && current.Executions[x.Claim.ActionID].Operations[op.ID].Receipt == nil {
				receipt, observeErr := f.observeTaskOperation(op, job)
				if observeErr != nil && op.Request.Kind == "qa.collect" {
					unlock()
					return observeErr
				}
				if observeErr != nil {
					receipt = protocol.TaskOperationReceipt{ID: op.ID, ActionID: x.Claim.ActionID, RequestDigest: protocol.TaskOperationDigest(op.Request), Status: "FAILED", ExitCode: -1, Message: "user authorized abort; owned process stopped without complete success"}
				}
				err = writeJSONAtomic(filepath.Join(job, "observed.json"), receipt)
				if err == nil {
					_, _, err = f.submitAdapterRegular("task-receipt:"+op.ID, protocol.RegularEvent{Operation: "TASK_OPERATION_RECEIPT", TaskOperationReceipt: &receipt})
				}
			}
			unlock()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
