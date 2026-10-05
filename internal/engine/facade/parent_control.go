package facade

import (
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
)

// acquireWriteWindow linearizes a child's writes with its master's controls.
// Native adapter effects cannot continue on authority read before this window.
func (f *Facade) acquireWriteWindow() (func(), error) {
	snap, err := f.engine.Load()
	if err != nil {
		if isMissingEngineState(err) {
			return f.acquireNativeWindow()
		}
		return nil, err
	}
	if snap.State.SliceChild == nil {
		return f.acquireNativeWindow()
	}
	parent, err := Open(f.root, snap.State.SliceChild.MasterRunID)
	if err != nil {
		return nil, err
	}
	up, err := parent.acquireNativeWindow()
	if err != nil {
		return nil, err
	}
	uc, err := f.acquireNativeWindow()
	if err != nil {
		up()
		return nil, err
	}
	release := func() { uc(); up() }
	current, err := parent.engine.Load()
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
		release()
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
		out = append(out, actual.State.ChildRequirementMaterial(decisions))
	}
	return out, nil
}
func (f *Facade) validateChildRequirementPackage(pkg *protocol.RequirementDecisionPackage) error {
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
		if got.ChildRunID != want.ChildRunID || got.SliceID != want.SliceID || got.RequirementRevision != want.RequirementRevision || got.ReceiptDigest != want.ReceiptDigest || got.CandidateDigest != want.CandidateDigest || got.RouteDigest != want.RouteDigest || got.Reason == "" || !disposition(got.RouteDisposition) || want.ReceiptDigest != "" && !disposition(got.ReceiptDisposition) || want.CandidateDigest != "" && !disposition(got.CandidateDisposition) || len(got.WorkDecisions) != len(want.WorkDecisions) {
			return fmt.Errorf("child requirement decision does not match exact work/material inventory: %s", want.ChildRunID)
		}
		for j, work := range got.WorkDecisions {
			if work.Work != want.WorkDecisions[j].Work || !disposition(work.Disposition) || work.Reason == "" {
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
		child, err := Open(f.root, plan.RunID)
		if err != nil {
			return err
		}
		unlock, err := child.acquireNativeWindow()
		if err != nil {
			return err
		}
		actual, loadErr := child.engine.Load()
		if isMissingEngineState(loadErr) {
			unlock()
			continue
		}
		if loadErr == nil && actual.State.Phase != runtime.PhasePreparing {
			err = child.engine.SyncParentReset(snap.State, emptyFingerprint())
			if err == nil {
				err = child.engine.SyncParentRequirement(snap.State, emptyFingerprint())
			}
		} else {
			err = loadErr
		}
		unlock()
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
