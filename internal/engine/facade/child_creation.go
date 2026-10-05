package facade

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/lifecycle"
)

func (f *Facade) request() (StartRequest, error) {
	var req StartRequest
	data, err := os.ReadFile(f.metadata)
	if err != nil {
		return req, err
	}
	err = json.Unmarshal(data, &req)
	return req, err
}

// prepareChild is called inside the parent's native window. Public Start
// cannot create children; its bootstrap writer is reused with exact bindings.
func (f *Facade) prepareChild(parent *protocol.State, plan protocol.ChildPreparation, owner OwnerBinding) (*Facade, error) {
	dir := filepath.Join(f.root, EngineNamespace, plan.RunID)
	established, err := runDirectoryEstablished(dir)
	if err != nil {
		return nil, err
	}
	var child *Facade
	if !established {
		req, err := f.request()
		if err != nil {
			return nil, err
		}
		req.RunID, req.MasterRunID, req.SliceID = plan.RunID, parent.RunID, plan.SliceID
		child, _, err = startRun(StartOptions{Root: f.root, Request: req, Admission: &f.admission, Capacity: f.capacity, VCSRunner: f.vcsRunner, Owner: owner}, parent.ChildStart(plan), parent.GitResources[plan.ResourceID].Path)
		if err != nil {
			return nil, err
		}
	} else {
		child, err = Open(f.root, plan.RunID)
		if err != nil {
			return nil, err
		}
	}
	if child.envelope != f.envelope || child.admission != f.admission {
		return nil, fmt.Errorf("child installation and version identity differ from the parent")
	}
	child.vcsRunner = f.vcsRunner
	child.vcs = f.vcs.At(parent.GitResources[plan.ResourceID].Path)
	return child, nil
}

func (f *Facade) finishChildBootstrap(child *Facade) error {
	snap, err := child.engine.Load()
	if err != nil {
		return err
	}
	if snap.Revision == 1 {
		tokenCap, err := readTokenCap(f.root)
		if err != nil {
			return err
		}
		if err := child.engine.BindCostSources(protocol.OwnerCostBinding{}, tokenCap, emptyFingerprint()); err != nil {
			return err
		}
	}
	return child.admission.write(func() error { return lifecycle.BeginRun(f.root, child.runID) })
}

// driveChildren owns preparation, native receipt join, release and cancellation.
// The caller holds only the parent window before acquiring any child window.
func (f *Facade) driveChildren() error {
	snap, err := f.engine.Load()
	if err != nil {
		return err
	}
	s := snap.State
	if s.SliceChild != nil {
		return nil
	}
	_, aborted := s.AbortDecision()
	if s.ChildCreation != nil && aborted {
		return f.cancelChildren(s)
	}
	if s.ChildCreation != nil && s.Topology != nil && !reflect.DeepEqual(s.ChildCreation.Topology, *s.Topology) && !s.RequirementBarrierActive() {
		digest, err := f.vcs.TreeDigest(s.AcceptedNativeSnapshot())
		if err != nil {
			return err
		}
		if err = f.engine.PrepareChildren(digest, emptyFingerprint()); err != nil {
			return err
		}
		snap, err = f.engine.Load()
		if err != nil {
			return err
		}
		s = snap.State
	}
	if s.RebuildingChildren() {
		ready, err := f.retireReplacedChildren(s)
		if err != nil {
			return err
		}
		if !ready {
			return nil
		}
	}
	if s.ChildCreation != nil && s.DeliveryComplete(f.definition) && !s.ResourcesCleaned() {
		if err := f.freezeCleanupSummary(s); err != nil {
			return err
		}
		return f.cleanupChildren(s)
	}
	if s.ChildCreation == nil {
		if aborted || s.Topology == nil || s.Topology.Decision != "split" || s.RequirementBarrierActive() || s.Phase == runtime.PhaseTerminal {
			return nil
		}
		digest, err := f.vcs.TreeDigest(s.AcceptedNativeSnapshot())
		if err != nil {
			return err
		}
		if err := f.engine.PrepareChildren(digest, emptyFingerprint()); err != nil {
			return err
		}
		snap, err = f.engine.Load()
		if err != nil {
			return err
		}
		s = snap.State
	}
	if len(s.SliceChildren) != 0 && !s.RebuildingChildren() {
		return f.activateReleasedChildren(s)
	}
	if !s.ChildCreationCurrent() {
		return nil
	}
	children := make([]*Facade, len(s.ChildCreation.Children))
	states := make([]*protocol.State, len(children))
	for i, plan := range s.ChildCreation.Children {
		child, err := f.prepareChild(s, plan, OwnerBinding{})
		if err != nil {
			return err
		}
		unlock, err := child.acquireNativeWindow()
		if err != nil {
			return err
		}
		err = f.finishChildBootstrap(child)
		actual, loadErr := child.engine.Load()
		unlock()
		if err != nil {
			return err
		}
		if loadErr != nil {
			return loadErr
		}
		if _, cancelled := actual.State.AbortDecision(); cancelled || actual.State.Phase == runtime.PhaseTerminal && !plan.Carried {
			return nil
		}
		children[i], states[i] = child, actual.State
	}
	if err := f.engine.RecordPreparedChildren(states, emptyFingerprint()); err != nil {
		return err
	}
	for {
		made, err := f.engine.SignChildWorkspace(emptyFingerprint())
		if err != nil {
			return err
		}
		if !made {
			break
		}
	}
	snap, err = f.engine.Load()
	if err != nil {
		return err
	}
	for _, plan := range snap.State.ChildCreation.Children {
		if _, proven := snap.State.ChildWorkspaceProof(plan); !plan.Carried && !proven {
			return nil
		}
	}
	// A deterministic order is shared by normal join and interrupted release.
	order := make([]int, len(children))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return strings.Compare(children[a].runID, children[b].runID) })
	var releases []func()
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	for _, i := range order {
		unlock, err := children[i].acquireNativeWindow()
		if err != nil {
			return err
		}
		releases = append(releases, unlock)
	}
	snap, err = f.engine.Load()
	if err != nil {
		return err
	}
	if !snap.State.ChildCreationCurrent() {
		return nil
	}
	for i, child := range children {
		actual, err := child.engine.Load()
		if err != nil {
			return err
		}
		plan := snap.State.ChildCreation.Children[i]
		if plan.Carried {
			states[i] = actual.State
			continue
		}
		if _, cancelled := actual.State.AbortDecision(); cancelled || actual.State.Phase != runtime.PhasePreparing {
			return nil
		}
		r := snap.State.GitResources[plan.ResourceID]
		if child.envelope != f.envelope || !reflect.DeepEqual(actual.State.Topology, &snap.State.ChildCreation.Topology) {
			return fmt.Errorf("child join identity differs from its parent")
		}
		exists, err := f.observeWorktree(r.Path, r.Source)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("child join has no registered native worktree")
		}
		ref, err := f.vcs.Ref(r.Ref)
		if err != nil {
			return err
		}
		digest, err := child.vcs.TreeDigest(r.Source)
		if err != nil {
			return err
		}
		if ref != r.Source || digest != snap.State.ChildCreation.Digest {
			return fmt.Errorf("child join native identity/content differs from its receipt")
		}
		if err := child.engine.AcceptPreparedSnapshot(snap.State, emptyFingerprint()); err != nil {
			return err
		}
		actual, err = child.engine.Load()
		if err != nil {
			return err
		}
		states[i] = actual.State
	}
	if err := f.engine.ReleaseChildren(states, emptyFingerprint()); err != nil {
		return err
	}
	released, err := f.engine.Load()
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := child.engine.ActivateChild(released.State, emptyFingerprint()); err != nil {
			return err
		}
	}
	return nil
}

func (f *Facade) activateReleasedChildren(parent *protocol.State) error {
	if !parent.ChildCreationCurrent() {
		return nil
	}
	for _, plan := range parent.ChildCreation.Children {
		child, err := f.prepareChild(parent, plan, OwnerBinding{})
		if err != nil {
			return err
		}
		actual, err := child.engine.Load()
		if isMissingEngineState(err) {
			continue
		}
		if err != nil {
			return err
		}
		if actual.State.Phase != runtime.PhasePreparing {
			continue
		}
		unlock, err := child.acquireNativeWindow()
		if err != nil {
			return err
		}
		current, err := f.engine.Load()
		if err == nil {
			err = child.engine.ActivateChild(current.State, emptyFingerprint())
		}
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *Facade) cancelChildren(parent *protocol.State) error {
	states := make([]*protocol.State, len(parent.ChildCreation.Children))
	settled := true
	for i, plan := range parent.ChildCreation.Children {
		if _, err := os.Stat(filepath.Join(f.root, EngineNamespace, plan.RunID)); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		child, err := f.prepareChild(parent, plan, OwnerBinding{})
		if err != nil {
			return err
		}
		unlock, err := child.acquireNativeWindow()
		if err != nil {
			return err
		}
		_, loadErr := child.engine.Load()
		if isMissingEngineState(loadErr) {
			summary, err := child.readSummary()
			unlock()
			if err != nil {
				return err
			}
			states[i] = summary.Termination
			continue
		}
		if loadErr != nil {
			unlock()
			return loadErr
		}
		current, err := f.engine.Load()
		if err == nil {
			err = child.engine.CancelChild(current.State, emptyFingerprint())
		}
		var run Run
		if err == nil {
			run, err = child.driveInWindow()
		}
		unlock()
		if err != nil {
			return err
		}
		if run.Status != "ABORTED" && run.Status != "COMPLETE" {
			settled = false
			continue
		}
		states[i] = run.Termination
		if states[i].SliceReceipt != nil {
			if err := f.acceptSliceReceipt(*states[i].SliceReceipt); err != nil {
				return err
			}
		}
	}
	if !settled {
		return nil
	}
	if err := f.engine.FinishChildCancellation(states, emptyFingerprint()); err != nil {
		return err
	}
	current, err := f.engine.Load()
	if err != nil {
		return err
	}
	if current.State.CleanupEligible(f.definition) && !current.State.ResourcesCleaned() {
		return f.engine.BeginCleanup(emptyFingerprint(), states...)
	}
	return nil
}

// cleanupChildren keeps checkpoints and their states until the master publisher
// has durable terminal authority. Native effects use each child's own registry.
func (f *Facade) cleanupChildren(parent *protocol.State) error {
	states := make([]*protocol.State, len(parent.ChildCreation.Children))
	for i, plan := range parent.ChildCreation.Children {
		child, err := f.prepareChild(parent, plan, OwnerBinding{})
		if err != nil {
			return err
		}
		unlock, err := child.acquireNativeWindow()
		if err != nil {
			return err
		}
		err = func() error {
			if err := child.engine.BeginCleanup(emptyFingerprint(), parent); err != nil {
				return err
			}
			for {
				progress, err := child.driveNative()
				if err != nil {
					return err
				}
				if !progress {
					break
				}
			}
			actual, err := child.engine.Load()
			if err == nil {
				states[i] = actual.State
			}
			return err
		}()
		unlock()
		if err != nil {
			return err
		}
		if !states[i].ResourcesCleaned() {
			return nil
		}
	}
	return f.engine.BeginCleanup(emptyFingerprint(), states...)
}

func (f *Facade) driveChildWorkspace(intent protocol.HostActionIntent, execute bool) (bool, error) {
	parent, err := f.engine.Load()
	if err != nil {
		return false, err
	}
	var p protocol.WorkspaceParameters
	if err := decodeNativeParams(intent, &p); err != nil {
		return false, err
	}
	plan, found := parent.State.CreationChild(p.Resource.ID)
	if !found {
		return false, fmt.Errorf("child workspace intent has no planned child")
	}
	child, err := f.prepareChild(parent.State, plan, OwnerBinding{})
	if err != nil {
		return false, err
	}
	unlock, err := child.acquireNativeWindow()
	if err != nil {
		return false, err
	}
	defer unlock()
	parent, err = f.engine.Load()
	if err != nil {
		return false, err
	}
	actual, err := child.engine.Load()
	if isMissingEngineState(err) {
		return f.completeNativeIntent(parent.State, intent, false)
	}
	if err != nil {
		return false, err
	}
	_, cancelled := actual.State.AbortDecision()
	mayWrite := execute && parent.State.ChildCreationCurrent() && actual.State.Phase == runtime.PhasePreparing && !cancelled
	if execute && !mayWrite {
		return false, nil
	}
	return f.completeNativeIntent(parent.State, intent, mayWrite)
}

// retireReplacedChildren consumes the same cancellation/cleanup handlers, using
// the frozen predecessor and the original topology authorization as evidence.
func (f *Facade) retireReplacedChildren(parent *protocol.State) (bool, error) {
	settled := true
	for _, old := range parent.ChildCreation.Previous.Children {
		if slices.ContainsFunc(parent.ChildCreation.Children, func(c protocol.ChildPreparation) bool { return c.RunID == old.RunID }) {
			continue
		}
		child, err := Open(f.root, old.RunID)
		if err != nil {
			return false, err
		}
		unlock, err := child.acquireNativeWindow()
		if err != nil {
			return false, err
		}
		actual, err := child.engine.Load()
		if isMissingEngineState(err) {
			summary, e := child.readSummary()
			err = e
			actual.State = summary.Termination
		}
		if err == nil && actual.State.Phase != runtime.PhaseTerminal {
			err = child.engine.CancelReplacedChild(parent, emptyFingerprint())
			if err == nil {
				run, e := child.driveInWindow()
				err = e
				settled = settled && (run.Status == "ABORTED" || run.Status == "COMPLETE")
				if err == nil && run.Termination != nil && run.Termination.SliceReceipt != nil {
					err = f.acceptSliceReceipt(*run.Termination.SliceReceipt)
				}
			}
		}
		unlock()
		if err != nil {
			return false, err
		}
	}
	if !settled {
		return false, nil
	}
	if err := f.engine.CleanupReplacedTargets(emptyFingerprint()); err != nil {
		return false, err
	}
	current, err := f.engine.Load()
	if err != nil {
		return false, err
	}
	for _, old := range parent.ChildCreation.Previous.Children {
		if !slices.ContainsFunc(parent.ChildCreation.Children, func(c protocol.ChildPreparation) bool { return c.RunID == old.RunID }) && current.State.GitResources[old.ResourceID].Status != "CLEANED" {
			return false, nil
		}
	}
	return true, nil
}
