package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"os"
	"os/exec"
	"path/filepath"
)

// Transport owns native state; the existing engine still owns claims and results.
type nativeExecutionTransport interface {
	turn(nativeTurn) (string, error)
	source(string, string, *protocol.HostRuntimeContext) (string, error)
	readIdentity(string, string) (string, error)
	probe(nativeProbe) (string, json.RawMessage, error)
}

func (f *Facade) executeNativeTransport(actionID string, resume bool, adapter nativeExecutionTransport) (Run, error) {
	run, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	if run.Regular == nil {
		return Run{}, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: native transport requires regular")
	}
	execution, ok := run.Executions[actionID]
	if !ok {
		return Run{}, fmt.Errorf("UNKNOWN_ACTION: %s", actionID)
	}
	// Resolve and validate the binding before CLAIM, recovery, or any native call.
	if execution.Claim.Context == nil {
		if execution.Claimed || execution.Identity != "" {
			return Run{}, fmt.Errorf("RUNTIME_CONTEXT_MISSING: original task context is unavailable")
		}
		execution.Claim.Context, err = captureRuntimeContext(execution.Claim.Provider, run.Regular.Settings.Values, f.stateDir, f.root)
		if err != nil {
			return Run{}, err
		}
	}
	probe := &exec.Cmd{Args: []string{execution.Claim.Context.Executable}}
	if err = runtimeCommandContext(probe, execution.Claim.Context); err != nil {
		return Run{}, err
	}
	capturedContext := execution.Claim.Context
	// The owning driver and abort collector share one action lock. Abort may
	// stop its process while this call runs, then waits for the owner to settle
	// its result before deciding whether any collection remains necessary.
	unlock, err := coordination.AcquirePath(f.codexJob(actionID)+".lock", "native action")
	if err != nil {
		return Run{}, err
	}
	defer unlock()
	run, err = f.Show()
	if err != nil {
		return Run{}, err
	}
	execution, ok = run.Executions[actionID]
	if !ok {
		return Run{}, fmt.Errorf("UNKNOWN_ACTION: %s", actionID)
	}
	if execution.Claim.Context == nil && !execution.Claimed {
		execution.Claim.Context = capturedContext
	}
	if unstartedActionClaim(execution, f.codexJob(actionID)) {
		return f.failedNativeStart(actionID, execution.Claim, fmt.Errorf("initial claim has no identity, source, recovery or operations and its job is absent or empty; startup failed before dispatch was recorded"))
	}
	if !resume && execution.Claim.Task.Step == "development-worker" && execution.Claim.Operation == "resume" {
		return Run{}, fmt.Errorf("DEVELOPMENT_CONTINUITY: unfinished batch requires --resume with its bound identity")
	}
	continuation := resume && !execution.Claimed && execution.Claim.Task.Step == "development-worker" && execution.Claim.Operation == "resume"
	replacementRecovery := resume && execution.Recovery != nil && execution.Recovery.Outcome() == "UNRECOVERABLE" && !execution.ReplacementClaimed
	interrupted := resume && !continuation && !replacementRecovery
	if interrupted {
		recovered, complete, recoveryErr := f.reconcileNative(actionID, execution, adapter)
		if recoveryErr != nil {
			return Run{}, recoveryErr
		}
		if complete {
			return recovered, nil
		}
		run, err = f.Show()
		if err != nil {
			return Run{}, err
		}
		execution = run.Executions[actionID]
	}
	if (!resume && execution.Claimed) || (resume && execution.ResumeClaimed && !replacementRecovery) || execution.Finished {
		return Run{}, fmt.Errorf("DUPLICATE_ACTION: execution was already claimed or consumed")
	}
	claim := execution.Claim
	if resume {
		claim.Operation = "resume"
		claim.Identity = execution.Identity
	}
	if claim.Task.Step != "product-review" && claim.Task.Step != "start-readiness" && !preparationAction(string(claim.Task.Step)) && !verificationAction(string(claim.Task.Step)) {
		return Run{}, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: no B1 transport for %s", claim.Task.Step)
	}
	job := f.codexJob(actionID)
	if interrupted {
		job = codexResumeJob(job, execution.ResumeCount+1)
	}
	// Fixed material stays in the original job. Checkpoint resumes bind a new
	// execution slice there without replacing any earlier material or receipt.
	input, err := f.prepareNativeActionInput(run, claim, execution, f.codexJob(actionID))
	if err != nil {
		return Run{}, err
	}
	if err = input.preflight(claim, adapter); err != nil {
		return Run{}, err
	}
	ev, _ := protocol.NewRegularEvent(protocol.EventID("claim:"+actionID+":"+claim.Operation), protocol.RegularEvent{Operation: "CLAIM", ExpectedRevision: run.Revision, Claim: &claim})
	// A rejected or duplicate claim returns here; neither command construction nor
	// subprocess creation occurs on these paths.
	if !replacementRecovery {
		if _, _, err = f.submitAdapterRegular(string(ev.ID), *ev.Regular); err != nil {
			return Run{}, err
		}
	}
	if resume && claim.Task.Step == "development-worker" {
		var recovery protocol.DeveloperRecovery
		var recoveryErr error
		if replacementRecovery {
			recovery = *execution.Recovery
		} else {
			recovery, recoveryErr = f.observeNativeDeveloperRecovery(run, execution, interrupted, adapter)
		}
		if recoveryErr != nil {
			return Run{}, recoveryErr
		}
		switch recovery.Outcome() {
		case "RESUMABLE":
		case "UNRECOVERABLE":
			replacement := claim
			replacement.Operation = "replace"
			if _, _, err = f.submitAdapterRegular("replace:"+actionID, protocol.RegularEvent{Operation: "CLAIM", Claim: &replacement}); err != nil {
				return Run{}, err
			}
			resume = false
			claim.Identity = ""
			claim.Operation = "replace"
		default:
			return f.failedNativeStart(actionID, claim, fmt.Errorf("native recovery is UNKNOWN: %s %s", recovery.NativeResponse, recovery.TransportError))
		}
	}
	if err = os.MkdirAll(job, 0700); err != nil {
		return Run{}, err
	}
	if !execution.Claimed || (resume && checkpointCanResume(run, execution)) {
		if err = input.material.writeFiles(f.codexJob(actionID)); err != nil {
			return f.failedNativeStart(actionID, claim, err)
		}
	}
	if !execution.Claimed {
		if err = writeJSONAtomic(filepath.Join(job, "sources.json"), input.sources); err != nil {
			return f.failedNativeStart(actionID, claim, err)
		}
	}
	prompt, err := input.render(claim)
	if err != nil {
		return Run{}, err
	}
	identity, nativeErr := f.runNativeBridgeTurns(run, claim, resume, job, prompt, input, adapter)
	if identity == "" {
		return f.failedNativeStart(actionID, claim, nativeErr)
	}
	if nativeErr == nil && f.fault != nil {
		if err = f.fault(persistence.FaultExecuteAfter); err != nil {
			return Run{}, err
		}
	}
	if nativeErr != nil {
		// A preflight failure belongs to that invocation, not to a main turn
		// that has never been started. Preserve its actual source and error.
		if observed, err := nativeProcessDirectory(job, string(claim.Task.Step)); err == nil {
			job = observed
		}
	}
	return f.collectNativeResult(actionID, claim, identity, job, nativeErr, adapter)
}
