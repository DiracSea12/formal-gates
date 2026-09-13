package facade

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"formal-gates/internal/engine/protocol"
)

// The tool only accepts a delta. Identity, candidate, scope and operation remain
// supplied by the attached action; the write itself is the existing event.
func (f *Facade) checkpointQA(q protocol.TaskOperationRequest) (QABatchReceipt, error) {
	empty := QABatchReceipt{}
	switch q.Binding.Task.Step {
	case "blackbox-execution", "whitebox-execution", "repair-worker":
	default:
		return empty, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: checkpoint requires QA or repair")
	}
	if q.RequestID == "" || q.CaseID != "" {
		return empty, fmt.Errorf("qa.checkpoint requires requestId and empty caseId")
	}
	var delta struct {
		Cases       []protocol.CaseExecution          `json:"cases"`
		Remaining   []string                          `json:"remaining"`
		Invalidated []protocol.CheckpointInvalidation `json:"invalidated"`
		Findings    []protocol.ReviewFinding          `json:"findings"`
		Message     string                            `json:"message"`
	}
	decoder := json.NewDecoder(strings.NewReader(q.Prompt))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&delta); err != nil {
		return empty, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return empty, fmt.Errorf("qa.checkpoint requires one delta object")
	}
	snapshot, err := f.engine.Load()
	if err != nil {
		return empty, err
	}
	if err := protocol.CurrentTaskOperationBinding(snapshot.State, q); err != nil {
		return empty, err
	}
	result := protocol.VerificationResult{ActionID: q.Binding.ActionID, AttemptID: q.Binding.AttemptID, Provider: q.Binding.Provider, Identity: q.Binding.Identity, RequirementRevision: q.Binding.RequirementRevision, Candidate: q.Binding.Candidate, BindingDigest: q.Binding.PrerequisiteDigest, Status: "CHECKPOINT", Message: delta.Message, Cases: delta.Cases, Remaining: delta.Remaining, Invalidated: delta.Invalidated, Findings: delta.Findings}
	event, err := protocol.NewRegularEvent(protocol.EventID("qa-checkpoint:"+protocol.TaskOperationID(q)), protocol.RegularEvent{Operation: "VERIFICATION_RESULT", ExpectedRevision: q.ExpectedRevision, VerificationResult: &result})
	if err != nil {
		return empty, err
	}

	if err := f.bindVerificationEvidence(event); err != nil {
		return empty, err
	}
	if err := protocol.ValidateQACheckpoint(snapshot.State, result); err != nil {
		return empty, err
	}
	if _, _, err := f.Submit(event); err != nil {
		return empty, err
	}
	snapshot, err = f.engine.Load()
	if err != nil {
		return empty, err
	}
	empty.Pending, err = protocol.PendingQA(snapshot.State, q.Binding, "")
	return empty, err
}
