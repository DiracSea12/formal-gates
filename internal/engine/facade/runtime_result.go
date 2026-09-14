package facade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/cost"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
)

// finishNativeResult admits a transport's final answer through the existing
// result and source writers. The transport owns execution and usage evidence.
func (f *Facade) finishNativeResult(actionID string, claim protocol.ExecutionClaim, identity, answer string, waitErr error, source protocol.ExecutionSource) (Run, error) {
	// Return the raw result first. Source settlement follows through submit and
	// blocks any new issue in between, including after a process interruption.
	data, readErr := os.ReadFile(answer)
	if verificationAction(string(claim.Task.Step)) {
		return f.finishVerification(claim, identity, data, readErr, waitErr, source)
	}
	if preparationAction(string(claim.Task.Step)) {
		var result protocol.PreparationResult
		decodeErr := json.Unmarshal(bytes.TrimSpace(data), &result)
		if waitErr != nil || readErr != nil || decodeErr != nil {
			result = protocol.PreparationResult{ActionID: actionID, AttemptID: claim.AttemptID, Provider: claim.Provider, RequirementRevision: claim.RequirementRevision, Candidate: claim.Candidate, Status: "RUNTIME_ERROR", Message: fmt.Sprintf("native execution=%v answer=%v decode=%v", waitErr, readErr, decodeErr)}
		}
		result.Identity = identity
		current, err := f.Show()
		if err != nil {
			return Run{}, err
		}
		if claim.Task.Step == "development-worker" && result.Status == "PASS" {
			p := current.Regular.Preparation
			if err := gitprep.CheckManifest(protocol.DevelopmentWorkspace(p, claim.Task), claim.Candidate, result.Manifest); err != nil {
				result.Status = "CONTINUE"
				result.Message = "Development delivery validation failed; correct the manifest in this same batch identity: " + err.Error()
			}
		}
		ev, _ := protocol.NewRegularEvent(protocol.EventID("result:"+actionID), protocol.RegularEvent{Operation: "PREPARATION_RESULT", ExpectedRevision: current.Revision, PreparationResult: &result})
		if !current.Executions[actionID].Finished {
			if _, _, err = f.submitAdapterRegular(string(ev.ID), *ev.Regular); err != nil {
				return Run{}, err
			}
		}
		if err = f.submitSource(source); err != nil {
			return Run{}, err
		}
		return f.Show()
	}
	var result protocol.RawReviewCandidate
	decodeErr := json.Unmarshal(bytes.TrimSpace(data), &result)
	if waitErr != nil || readErr != nil || decodeErr != nil {
		result = protocol.RawReviewCandidate{ActionID: actionID, AttemptID: claim.AttemptID, Provider: claim.Provider, Action: string(claim.Task.Step), RequirementRevision: claim.RequirementRevision, Candidate: claim.Candidate, Status: "RUNTIME_ERROR", Message: fmt.Sprintf("native execution=%v answer=%v decode=%v", waitErr, readErr, decodeErr)}
	}
	result.Identity = identity
	current, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	resultEvent, _ := protocol.NewRegularEvent(protocol.EventID("result:"+actionID), protocol.RegularEvent{Operation: "REVIEW_RESULT", ExpectedRevision: current.Revision, Review: &result})
	if !current.Executions[actionID].Finished {
		if _, _, err = f.submitAdapterRegular(string(resultEvent.ID), *resultEvent.Regular); err != nil {
			return Run{}, err
		}
	}
	if err = f.submitSource(source); err != nil {
		return Run{}, err
	}
	return f.Show()
}

func (f *Facade) collectNativeResult(actionID string, claim protocol.ExecutionClaim, identity, job string, waitErr error, adapter nativeExecutionTransport) (Run, error) {
	path, err := adapter.source(identity, job, claim.Context)
	source := protocol.ExecutionSource{ActionID: actionID, AttemptID: claim.AttemptID, Provider: claim.Provider, Identity: identity, Status: "STOPPED", TranscriptPath: path}
	if err == nil {
		_, err = cost.ParseTranscript(claim.Provider, path)
	}
	if err != nil {
		source.Status = "UNAVAILABLE"
		source.Reason = err.Error()
	}
	return f.finishNativeResult(actionID, claim, identity, filepath.Join(job, "answer.json"), waitErr, source)
}
