package protocol

import (
	"encoding/json"
	"slices"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/runtime"
)

// ChildCreation is the master's immutable preparation plan. SliceChildren
// alone authorizes released children; this intent owns the preceding work.
type ChildCreation struct {
	RequirementRevision string             `json:"requirementRevision"`
	Coverage            *CoverageInventory `json:"coverage,omitempty"`
	Topology            TopologyBinding    `json:"topology"`
	Source              string             `json:"source"`
	Digest              string             `json:"digest"`
	Children            []ChildPreparation `json:"children"`
}

type ChildPreparation struct {
	SliceID    string `json:"sliceId"`
	RunID      string `json:"runId"`
	ResourceID string `json:"resourceId"`
}

func (s *State) ChildCreationCurrent() bool {
	c := s.ChildCreation
	return c != nil && s.SliceChild == nil && s.Phase != runtime.PhaseTerminal && !s.abortRequested() && !s.requirementChangePending() && c.RequirementRevision == s.RequirementRevision() && digestOfCanonical(c.Topology) == digestOfCanonical(s.Topology)
}

func (s *State) ChildStart(child ChildPreparation) *SliceRunStart {
	return &SliceRunStart{Binding: SliceChildBinding{MasterRunID: s.RunID, SliceID: child.SliceID, RequirementRevision: s.ChildCreation.RequirementRevision, Coverage: s.ChildCreation.Coverage}, Topology: copyTopology(s.ChildCreation.Topology)}
}

func (s *State) CreationChild(resourceID string) (ChildPreparation, bool) {
	if s.ChildCreation != nil {
		for _, child := range s.ChildCreation.Children {
			if child.ResourceID == resourceID {
				return child, true
			}
		}
	}
	return ChildPreparation{}, false
}

func (e *Engine) PrepareChildren(sourceDigest, expectedFingerprint string) error {
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if err := s.errWriteBarrier("child preparation"); err != nil {
		return err
	}
	if s.ChildCreation != nil {
		return nil
	}
	if s.SliceChild != nil || s.Topology == nil || s.Topology.Decision != topologySplit || s.abortRequested() || s.Phase == runtime.PhaseTerminal || s.NativeRoot == "" || sourceDigest == "" {
		return validationError("child preparation requires an active native split master")
	}
	c := &ChildCreation{RequirementRevision: s.RequirementRevision(), Coverage: s.confirmedCoverage(), Topology: copyTopology(*s.Topology), Source: s.AcceptedNativeSnapshot(), Digest: sourceDigest}
	if s.GitResources == nil {
		s.GitResources = map[string]GitResource{}
	}
	for _, sliceID := range c.Topology.Slices {
		id := "child-" + strings.TrimPrefix(digestOfCanonical(struct{ Master, Slice string }{s.RunID, sliceID}), "sha256:")
		r := s.resource("child-target", c.Source, id)
		s.GitResources[r.ID] = r
		c.Children = append(c.Children, ChildPreparation{SliceID: sliceID, RunID: id, ResourceID: r.ID})
	}
	s.ChildCreation = c
	_, err = e.commit(s, revision, expectedFingerprint)
	return err
}

func (s *State) preparedChild(child ChildPreparation, actual *State) bool {
	if actual == nil || actual.SliceChild == nil {
		return false
	}
	expected := s.ChildStart(child)
	return actual.RunID == child.RunID && actual.Route == "regular" && actual.BaseSnapshot == s.BaseSnapshot && actual.NativeRoot == s.GitResources[child.ResourceID].Path && actual.SliceChild.MasterRunID == expected.Binding.MasterRunID && actual.SliceChild.SliceID == expected.Binding.SliceID && actual.SliceChild.RequirementRevision == expected.Binding.RequirementRevision && digestOfCanonical(actual.SliceChild.Coverage) == digestOfCanonical(expected.Binding.Coverage) && digestOfCanonical(actual.Topology) == digestOfCanonical(expected.Topology) && actual.IntakeConfirmationReceipt != nil && s.IntakeConfirmationReceipt != nil && sameIntakeConfirmation(*actual.IntakeConfirmationReceipt, *s.IntakeConfirmationReceipt)
}

func (e *Engine) RecordPreparedChildren(children []*State, expectedFingerprint string) error {
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if !s.ChildCreationCurrent() || len(children) != len(s.ChildCreation.Children) {
		return validationError("child preparation no longer matches the master")
	}
	if slices.Contains(s.Completed, authoring.StepID("children.prepare")) {
		return nil
	}
	for i, child := range s.ChildCreation.Children {
		if !s.preparedChild(child, children[i]) || children[i].Phase != runtime.PhasePreparing || children[i].abortRequested() {
			return validationError("child preparation does not prove the planned child")
		}
	}
	if err := s.CompleteStep("children.prepare", e.cfg.Definition); err != nil {
		return err
	}
	_, err = e.commit(s, revision, expectedFingerprint)
	return err
}

func (e *Engine) SignChildWorkspace(expectedFingerprint string) (bool, error) {
	revision, s, err := e.load()
	if err != nil {
		return false, err
	}
	if !s.ChildCreationCurrent() || !slices.Contains(s.Completed, authoring.StepID("children.prepare")) || len(s.SliceChildren) != 0 {
		return false, nil
	}
	for _, child := range s.ChildCreation.Children {
		r := s.GitResources[child.ResourceID]
		if r.Status == "READY" {
			continue
		}
		pending := false
		for _, intent := range s.PendingHostActions {
			if intent.Adapter == nil || intent.Adapter.Operation != "op.workspace" {
				continue
			}
			var p WorkspaceParameters
			raw, _ := json.Marshal(intent.Adapter.Params)
			if err := json.Unmarshal(raw, &p); err != nil {
				return false, err
			}
			pending = pending || p.Resource.ID == r.ID
		}
		if pending {
			continue
		}
		intent, err := e.prepareAdapterIntent(s, revision+1, "op.workspace", WorkspaceParameters{Resource: r, RequirementRevision: s.ChildCreation.RequirementRevision}, "")
		if err != nil {
			return false, err
		}
		s.PendingHostActions[intent.ActionID] = intent
		_, err = e.commit(s, revision, expectedFingerprint)
		return err == nil, err
	}
	return false, nil
}

func (s *State) ChildWorkspaceProof(child ChildPreparation) (WorkspaceEvidence, bool) {
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.workspace" || receipt.Status != HostActionStatusExecuted {
			continue
		}
		var evidence WorkspaceEvidence
		if decodeAdapterValues(receipt, &evidence) != nil || evidence.Resource.ID != child.ResourceID {
			continue
		}
		intent, found := s.hostIntent(receipt.ActionID)
		if !found || intent.Step != "children.workspace" || validateResourceReceipt(intent, receipt) != nil || evidence.Digest != s.ChildCreation.Digest || evidence.Resource.Identity != s.ChildCreation.Source {
			continue
		}
		return evidence, true
	}
	return WorkspaceEvidence{}, false
}

// AcceptPreparedSnapshot binds the first native identity only from the
// master's original typed workspace proof while both run windows are held.
func (e *Engine) AcceptPreparedSnapshot(parent *State, expectedFingerprint string) error {
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if !parent.ChildCreationCurrent() || s.Phase != runtime.PhasePreparing || s.abortRequested() {
		return validationError("prepared child cannot accept a native snapshot")
	}
	child, found := parent.CreationChildByRun(s.RunID)
	if !found || !parent.preparedChild(child, s) {
		return validationError("prepared child differs from its creation binding")
	}
	proof, found := parent.ChildWorkspaceProof(child)
	if !found {
		return validationError("prepared child has no exact workspace proof")
	}
	if s.InitialSnapshot != "" {
		if s.InitialSnapshot != proof.Resource.Identity {
			return validationError("child initial snapshot is immutable")
		}
		return nil
	}
	s.InitialSnapshot = proof.Resource.Identity
	_, err = e.commit(s, revision, expectedFingerprint)
	return err
}

func (s *State) CreationChildByRun(runID string) (ChildPreparation, bool) {
	if s.ChildCreation != nil {
		for _, child := range s.ChildCreation.Children {
			if child.RunID == runID {
				return child, true
			}
		}
	}
	return ChildPreparation{}, false
}

func (e *Engine) ReleaseChildren(children []*State, expectedFingerprint string) error {
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if !s.ChildCreationCurrent() || len(children) != len(s.ChildCreation.Children) {
		return validationError("child release no longer matches the master")
	}
	if len(s.SliceChildren) != 0 {
		return nil
	}
	for i, child := range s.ChildCreation.Children {
		if !s.preparedChild(child, children[i]) || children[i].Phase != runtime.PhasePreparing || children[i].abortRequested() || children[i].InitialSnapshot != s.ChildCreation.Source {
			return validationError("child release lacks a live prepared child")
		}
		if _, proven := s.ChildWorkspaceProof(child); !proven {
			return validationError("child release lacks an exact workspace receipt")
		}
	}
	for _, step := range []authoring.StepID{"children.workspace", "children.publish"} {
		if err := s.CompleteStep(step, e.cfg.Definition); err != nil {
			return err
		}
	}
	s.SliceChildren = map[string]string{}
	for _, child := range s.ChildCreation.Children {
		s.SliceChildren[child.SliceID] = child.RunID
	}
	_, err = e.commit(s, revision, expectedFingerprint)
	return err
}

func (e *Engine) ActivateChild(parent *State, expectedFingerprint string) error {
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s.Phase != runtime.PhasePreparing {
		return nil
	}
	child, found := parent.CreationChildByRun(s.RunID)
	if !found || !parent.ChildCreationCurrent() || !parent.preparedChild(child, s) || parent.SliceChildren[child.SliceID] != s.RunID || s.InitialSnapshot != parent.ChildCreation.Source || s.abortRequested() {
		return validationError("child activation lacks current parent release authority")
	}
	if err := s.TransitionPhase(runtime.PhaseTopologyAndRoute); err != nil {
		return err
	}
	_, err = e.commit(s, revision, expectedFingerprint)
	return err
}

func (e *Engine) CancelChild(parent *State, expectedFingerprint string) error {
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s.Phase == runtime.PhaseTerminal || s.abortRequested() {
		return nil
	}
	child, found := parent.CreationChildByRun(s.RunID)
	d, cancelled := parent.AbortDecision()
	record, recorded := parent.AbortEvent()
	if !found || !cancelled || !recorded || !parent.preparedChild(child, s) {
		return validationError("child cancellation lacks parent authority")
	}
	s.SliceChild.ParentAbort = &AbortAuthority{Decision: d, Event: record}
	_, err = e.commit(s, revision, expectedFingerprint)
	return err
}

func (e *Engine) FinishChildCancellation(children []*State, expectedFingerprint string) error {
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s.ChildCreation == nil || !s.abortRequested() || len(children) != len(s.ChildCreation.Children) {
		return validationError("child cancellation does not match the creation")
	}
	for i, child := range s.ChildCreation.Children {
		if actual := children[i]; actual != nil && (!s.preparedChild(child, actual) || actual.Phase != runtime.PhaseTerminal || !actual.ResourcesCleaned()) {
			return validationError("child cancellation has unfinished children")
		}
	}
	for _, intent := range s.PendingHostActions {
		if intent.Step == "children.workspace" {
			return nil
		}
	}
	if slices.Contains(s.Completed, authoring.StepID("children.publish")) {
		return nil
	}
	for _, step := range []authoring.StepID{"children.prepare", "children.workspace", "children.publish"} {
		if slices.Contains(s.Completed, step) {
			continue
		}
		if err := s.CompleteStep(step, e.cfg.Definition); err != nil {
			return err
		}
	}
	_, err = e.commit(s, revision, expectedFingerprint)
	return err
}

// Creation targets outlive their children until abort or delivered-master
// release has cleaned every child's own native resources.
func (s *State) childTargetsCleanable(children []*State) bool {
	if s.ChildCreation == nil {
		return true
	}
	if len(children) != len(s.ChildCreation.Children) {
		return false
	}
	for i, child := range s.ChildCreation.Children {
		actual := children[i]
		if actual == nil {
			if len(s.SliceChildren) != 0 {
				return false
			}
			continue
		}
		released := actual.Phase == runtime.PhaseTerminal
		if !s.abortRequested() {
			receipt, accepted := s.SliceReceipts[child.RunID]
			released = actual.Phase == runtime.PhaseSliceReady && actual.SliceReceipt != nil && accepted && receipt.Digest == actual.SliceReceipt.Digest
		}
		if !s.preparedChild(child, actual) || !released || !actual.ResourcesCleaned() {
			return false
		}
	}
	return true
}

func (s *State) creationStepSettled(step authoring.StepID) bool {
	return s.SliceChild != nil || s.Topology != nil && s.Topology.Decision != topologySplit || slices.Contains(s.Completed, step)
}

func childWorkspaceStep(params any) authoring.StepID {
	raw, _ := json.Marshal(params)
	var p WorkspaceParameters
	if json.Unmarshal(raw, &p) == nil && p.Resource.Role == "child-target" {
		return "children.workspace"
	}
	return "workspace.materialize"
}

func (s *State) creationWorkspaceCurrent(p WorkspaceParameters) bool {
	child, found := s.CreationChild(p.Resource.ID)
	return found && s.ChildCreationCurrent() && len(s.SliceChildren) == 0 && p.Generation == 0 && p.RouteScope == "" && p.Manifest == nil && p.RequirementRevision == s.ChildCreation.RequirementRevision && s.GitResources[child.ResourceID] == p.Resource && p.Resource.Source == s.ChildCreation.Source && s.resourceUsable(p.Resource.ID)
}
