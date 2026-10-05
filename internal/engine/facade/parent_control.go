package facade

import (
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"os"
	"path/filepath"
)

// acquireWriteWindow linearizes a child's writes with its master's controls.
// Native adapter effects cannot continue on authority read before this window.
func (f *Facade) acquireWriteWindow() (unlock func(), err error) {
	snap, err := f.engine.Load()
	if err != nil {
		if isMissingEngineState(err) {
			return f.acquireNativeWindow()
		}
		return nil, err
	}
	if snap.State.SliceChild == nil {
		u, err := f.acquireNativeWindow()
		if err != nil {
			return nil, err
		}
		defer func() {
			if unlock == nil {
				u()
			}
		}()
		if _, err := f.engine.Load(); isMissingEngineState(err) {
			return u, nil
		} else if err != nil {
			return nil, err
		}
		if err = f.syncReleasedChildren(); err != nil {
			return nil, err
		}
		return u, nil
	}
	parent, err := Open(f.root, snap.State.SliceChild.MasterRunID)
	if err != nil {
		return nil, err
	}
	up, err := parent.acquireNativeWindow()
	if err != nil {
		return nil, err
	}
	defer func() {
		if unlock == nil {
			up()
		}
	}()
	uc, err := f.acquireNativeWindow()
	if err != nil {
		return nil, err
	}
	defer func() {
		if unlock == nil {
			uc()
		}
	}()
	release := func() { uc(); up() }
	actual, err := f.engine.Load()
	if isMissingEngineState(err) {
		return release, nil
	}
	if err != nil {
		return nil, err
	}
	current, err := parent.engine.Load()
	if err == nil {
		err = f.recordIntakeReceipt(actual.State)
	}
	if err == nil {
		err = f.engine.SyncParentReset(current.State, emptyFingerprint())
	}
	if err == nil {
		err = f.engine.SyncParentRequirement(current.State, emptyFingerprint())
	}
	if err == nil {
		actual, loadErr := f.engine.Load()
		err = loadErr
		if err == nil && actual.State.Phase == runtime.PhasePreparing && current.State.ChildCreationCurrent() && current.State.SliceChildren[actual.State.SliceChild.SliceID] == f.runID {
			err = f.engine.ActivateChild(current.State, emptyFingerprint())
		}
	}
	if err != nil {
		return nil, err
	}
	return release, nil
}

func (f *Facade) requirementChildren() ([]protocol.ChildRequirementDecision, error) {
	snap, err := f.engine.Load()
	if err != nil {
		return nil, err
	}
	var out []protocol.ChildRequirementDecision
	if snap.State.SliceChild != nil || snap.State.ChildCreation == nil {
		return out, nil
	}
	for _, plan := range snap.State.ChildCreation.Children {
		child, err := Open(f.root, plan.RunID)
		if err != nil {
			return nil, err
		}
		actual, err := child.engine.Load()
		if err != nil {
			if isMissingEngineState(err) {
				summary, e := child.readSummary()
				if e != nil {
					return nil, e
				}
				actual.State = summary.Termination
			} else {
				return nil, err
			}
		}
		work, err := child.engine.RequirementWork()
		if err != nil && !isMissingEngineState(err) {
			return nil, err
		}
		decisions := make([]protocol.RequirementWorkDecision, len(work))
		for i, w := range work {
			decisions[i].Work = w
		}
		material := actual.State.ChildRequirementMaterial(decisions)
		material.EnteredMainline = snap.State.SliceEnteredMainline(plan.RunID)
		out = append(out, material)
	}
	return out, nil
}
func (f *Facade) validateChildRequirementPackage(pkg *protocol.RequirementDecisionPackage, deciding ...bool) error {
	if pkg == nil {
		return nil
	}
	actual, err := f.requirementChildren()
	if err != nil {
		return err
	}
	if len(actual) != len(pkg.Children) {
		return fmt.Errorf("requirement confirmation must decide every current child")
	}
	for i, want := range actual {
		got := pkg.Children[i]
		if got.EnteredMainline != want.EnteredMainline || got.ChildRunID != want.ChildRunID || got.SliceID != want.SliceID || got.RequirementRevision != want.RequirementRevision || got.ReceiptDigest != want.ReceiptDigest || got.CandidateDigest != want.CandidateDigest || got.RouteDigest != want.RouteDigest || got.Reason == "" || !disposition(got.RouteDisposition) || want.ReceiptDigest != "" && !disposition(got.ReceiptDisposition) || want.CandidateDigest != "" && !disposition(got.CandidateDisposition) || len(got.WorkDecisions) != len(want.WorkDecisions) {
			return fmt.Errorf("child requirement decision does not match exact work/material inventory: %s", want.ChildRunID)
		}
		if got.EnteredMainline && !pkg.RedoTopology && (got.ReceiptDisposition == protocol.RequirementWorkRedo || got.CandidateDisposition == protocol.RequirementWorkRedo || got.RouteDisposition == protocol.RequirementWorkRedo) {
			return fmt.Errorf("entered slice %s is retained as source evidence; decide affected master obligations", got.ChildRunID)
		}
		for j, work := range got.WorkDecisions {
			if got.EnteredMainline && !pkg.RedoTopology && work.Disposition == protocol.RequirementWorkRedo {
				return fmt.Errorf("entered child work is owned by the master candidate/repair obligations: %s", got.ChildRunID)
			}
			expected := want.WorkDecisions[j].Work
			if len(deciding) > 0 && deciding[0] && work.Work.ResultDigest == "" {
				expected.ResultDigest = ""
			}
			if work.Work != expected || !disposition(work.Disposition) || work.Reason == "" {
				return fmt.Errorf("child requirement work decision does not match %s", want.ChildRunID)
			}
		}
	}
	if len(actual) > 0 && pkg.Impact == nil {
		return fmt.Errorf("split requirement confirmation requires the complete changed/transitive impact inventory")
	}
	return nil
}
func disposition(v string) bool {
	return v == protocol.RequirementWorkCarry || v == protocol.RequirementWorkRedo
}
func (f *Facade) syncReleasedChildren() error {
	snap, err := f.engine.Load()
	if err != nil {
		return err
	}
	if snap.State.SliceChild != nil || snap.State.ChildCreation == nil {
		return nil
	}
	for _, plan := range snap.State.ChildCreation.Children {
		established, err := runDirectoryEstablished(filepath.Join(f.root, EngineNamespace, plan.RunID))
		if err != nil {
			return err
		}
		if !established {
			continue
		}
		child, err := Open(f.root, plan.RunID)
		if err != nil {
			return err
		}
		unlock, err := child.acquireNativeWindow()
		if err != nil {
			return err
		}
		err = func() error {
			defer unlock()
			actual, loadErr := child.engine.Load()
			if isMissingEngineState(loadErr) {
				return nil
			}
			if loadErr == nil && actual.State.Phase != runtime.PhasePreparing {
				err = child.recordIntakeReceipt(actual.State)
				if err == nil {
					err = child.engine.SyncParentReset(snap.State, emptyFingerprint())
				}
				if err == nil {
					err = child.engine.SyncParentRequirement(snap.State, emptyFingerprint())
				}
				if err == nil {
					actual, err = child.engine.Load()
					if err == nil && actual.State.ResetPending() {
						_, err = child.driveInWindow()
					}
				}
			} else {
				err = loadErr
			}
			return err
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *Facade) recordChildReset() error {
	snap, err := f.engine.Load()
	if err != nil {
		return err
	}
	if snap.State.SliceChild == nil {
		return nil
	}
	parent, err := Open(f.root, snap.State.SliceChild.MasterRunID)
	if err != nil {
		return err
	}
	return parent.engine.RecordChildReset(snap.State, emptyFingerprint())
}

func (f *Facade) resetChildrenPreview(preview *protocol.ResetPreview) error {
	for _, id := range preview.Children {
		child, err := Open(f.root, id)
		if err != nil {
			return err
		}
		snap, err := child.engine.Load()
		if isMissingEngineState(err) {
			summary, e := child.readSummary()
			err = e
			snap.State = summary.Termination
		}
		if err != nil {
			return err
		}
		p := snap.State.ResetPreview()
		p.Work, err = child.engine.RequirementWork()
		if err != nil && !isMissingEngineState(err) {
			return err
		}
		p.RequirementRevision = preview.RequirementRevision
		p.Digest = p.CanonicalDigest()
		preview.ChildPreviews = append(preview.ChildPreviews, p)
	}
	return nil
}
func (f *Facade) validateResetChildren(request string) error {
	snap, err := f.engine.Load()
	if err != nil {
		return err
	}
	approved := snap.State.PendingAsks[request].Reset
	if approved == nil || len(approved.Children) == 0 {
		return nil
	}
	current := *approved
	current.ChildPreviews = nil
	if err := f.resetChildrenPreview(&current); err != nil {
		return err
	}
	if current.CanonicalDigest() != approved.Digest {
		return fmt.Errorf("child reset scope changed; request a fresh complete preview")
	}
	return nil
}

// Reset re-registration observes the complete confirmed document through the
// same requirement handler. Drive resumes this observation after response loss.
func (f *Facade) syncResetRequirement() error {
	snap, err := f.engine.Load()
	if isMissingEngineState(err) {
		return nil
	}
	if err != nil {
		return err
	}
	s := snap.State
	if s.ResetPending() || len(s.RequirementRevisions) < 2 {
		return nil
	}
	entry := s.RequirementRevisions[len(s.RequirementRevisions)-1]
	if entry.Source != protocol.RequirementSourceReset || s.Validation == nil || s.Validation.Generation == entry.StateRevision {
		return nil
	}
	req, err := f.request()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(f.root, req.IntakeConfirmationReceipt.RequirementSource))
	if err != nil {
		return err
	}
	event := protocol.Event{ID: protocol.EventID("reset-requirement:" + entry.RequestID), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: entry.Revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: entry.Revision, Document: string(data)}}}
	_, err = f.engine.Submit(event, emptyFingerprint())
	return err
}
