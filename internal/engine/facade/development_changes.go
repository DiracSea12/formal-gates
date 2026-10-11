package facade

import (
	"fmt"
	"strings"

	"formal-gates/internal/engine/protocol"
)

// PrepareReplan uses the existing complete requirement/plan confirmation and
// exact work dispositions. It accepts no unconfirmed batch definitions.
func (f *Facade) PrepareReplan(input RequirementChangeInput) (ControlCandidate, error) {
	run, err := f.Show()
	if err != nil {
		return ControlCandidate{}, err
	}
	if run.Route != "regular" || run.Status != "ACTIVE" || run.PendingRequirementAsk != nil {
		return ControlCandidate{}, fmt.Errorf("replan requires an active regular run without a requirement barrier")
	}
	if input.Classification != "" && input.Classification != protocol.RequirementClassificationPreserved || len(input.Redo) != 0 {
		return ControlCandidate{}, fmt.Errorf("replan keeps the requirement and topology; use ordinary requirement change for a changed requirement")
	}
	input.Classification = protocol.RequirementClassificationPreserved
	input.Redo = []string{"start-readiness", "route"}
	candidate, err := f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Requirement: &input})
	if err != nil {
		return ControlCandidate{}, err
	}
	if strings.TrimPrefix(candidate.Event.Request.Package.CandidateRevision, "sha256:") != strings.TrimPrefix(run.Review.RequirementRevision, "sha256:") {
		return ControlCandidate{}, fmt.Errorf("replan requires the original unchanged confirmed requirement bytes")
	}
	return candidate, nil
}
func (f *Facade) RequestWorkerHandoff(worker int, reason string) (Run, error) {
	release, err := f.acquireWriteWindow()
	if err != nil {
		return Run{}, err
	}
	snap, err := f.engine.Load()
	if err != nil {
		release()
		return Run{}, err
	}
	ids := workerActionOrder(snap.State.WorkerInputs(f.definition))
	if worker < 1 || worker > len(ids) {
		release()
		return Run{}, fmt.Errorf("handoff worker is not one of the current workers")
	}
	_, err = f.engine.RequestDevelopmentHandoff(ids[worker-1], reason, snap.Revision, emptyFingerprint())
	release()
	if err != nil {
		return Run{}, err
	}
	return f.Show()
}
