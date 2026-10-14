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
	RequestID           string                     `json:"requestId,omitempty"`
	Previous            *ChildCreation             `json:"previous,omitempty"`
	RequirementRevision string                     `json:"requirementRevision"`
	Intake              *IntakeConfirmationReceipt `json:"intake,omitempty"`
	Coverage            *CoverageInventory         `json:"coverage,omitempty"`
	Topology            TopologyBinding            `json:"topology"`
	Source              string                     `json:"source"`
	Digest              string                     `json:"digest"`
	Children            []ChildPreparation         `json:"children"`
}

type ChildPreparation struct {
	SliceID    string `json:"sliceId"`
	RunID      string `json:"runId"`
	ResourceID string `json:"resourceId"`
	Carried    bool   `json:"carried,omitempty"`
}

func (s *State) ChildCreationCurrent() bool {
	c := s.ChildCreation
	return c != nil && s.SliceChild == nil && s.Phase != runtime.PhaseTerminal && !s.abortRequested() && !s.requirementChangePending() && digestOfCanonical(c.Topology) == digestOfCanonical(s.Topology)
}

func (s *State) ChildStart(child ChildPreparation) *SliceRunStart {
	if child.Carried && s.ChildCreation.Previous != nil {
		parent := *s
		parent.ChildCreation = s.ChildCreation.Previous
		for _, previous := range parent.ChildCreation.Children {
			if previous.RunID == child.RunID {
				return parent.ChildStart(previous)
			}
		}
	}
	start := &SliceRunStart{Binding: SliceChildBinding{MasterRunID: s.RunID, SliceID: child.SliceID, RequirementRevision: s.ChildCreation.RequirementRevision, InheritedIntake: s.ChildCreation.Intake, Coverage: s.ChildCreation.Coverage}, Topology: copyTopology(s.ChildCreation.Topology)}
	if s.FlowSelection != nil {
		start.FlowSelection = &FlowSelection{Disabled: slices.Clone(s.FlowSelection.Disabled)}
	}
	return start
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

func (e *Engine) PrepareChildren(sourceDigest string, input Observation) error {
	e = e.observing(input)
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if err := s.errWriteBarrier("child preparation"); err != nil {
		return err
	}
	if s.ChildCreation != nil && digestOfCanonical(s.ChildCreation.Topology) == digestOfCanonical(s.Topology) {
		return nil
	}
	if s.SliceChild != nil || s.Topology == nil || s.Topology.Decision != topologySplit && s.ChildCreation == nil || s.abortRequested() || s.Phase == runtime.PhaseTerminal || s.NativeRoot == "" || sourceDigest == "" {
		return validationError("child preparation requires an active native split master")
	}
	var previous *ChildCreation
	request := ""
	topologyRequest := ""
	if s.ChildCreation != nil {
		previous = s.ChildCreation
		for _, entry := range s.RequirementRevisions {
			if slices.Contains(entry.DecidedScope, "topology") {
				request = entry.RequestID
			}
		}
		if request == "" {
			return validationError("topology rebuild requires a confirmed requirement authorization")
		}
		var topologyRevision uint64
		for _, decision := range s.Decisions {
			if decision.Control == ControlConfirmTopology && decision.Revision > topologyRevision {
				topologyRequest, topologyRevision = decision.RequestID, decision.Revision
			}
		}
		if topologyRequest == "" {
			return validationError("topology rebuild requires its confirmed topology request")
		}
	}
	c := &ChildCreation{Previous: previous, RequestID: request, RequirementRevision: s.RequirementRevision(), Intake: s.CurrentIntakeConfirmation(), Coverage: s.confirmedCoverage(), Topology: copyTopology(*s.Topology), Source: s.AcceptedNativeSnapshot(), Digest: sourceDigest}
	if s.NativeResources == nil {
		s.NativeResources = map[string]NativeResource{}
	}
sliceLoop:
	for _, sliceID := range c.Topology.Slices {
		if previous != nil {
			_, choice := s.childRequirementDecision(s.SliceChildren[sliceID])
			if choice != nil && choice.ReceiptDisposition == RequirementWorkCarry && choice.RouteDisposition == RequirementWorkCarry && !slices.ContainsFunc(choice.WorkDecisions, func(w RequirementWorkDecision) bool { return w.Disposition != RequirementWorkCarry }) {
				for _, child := range previous.Children {
					if child.SliceID == sliceID {
						child.Carried = true
						c.Children = append(c.Children, child)
						continue sliceLoop
					}
				}
			}
		}
		digest := digestOfCanonical(struct{ Master, Slice string }{s.RunID, sliceID})
		if topologyRequest != "" {
			digest = digestOfCanonical(struct{ Master, Slice, Request string }{s.RunID, sliceID, topologyRequest})
		}
		id := "child-" + strings.TrimPrefix(digest, "sha256:")
		r := s.resource("child-target", c.Source, id)
		s.NativeResources[r.ID] = r
		c.Children = append(c.Children, ChildPreparation{SliceID: sliceID, RunID: id, ResourceID: r.ID})
	}
	if previous != nil {
		s.Completed = slices.DeleteFunc(s.Completed, func(step authoring.StepID) bool {
			return step == "children.prepare" || step == "children.workspace" || step == "children.publish"
		})
	}
	s.ChildCreation = c
	_, err = e.commit(s, revision, input)
	return err
}

func (s *State) preparedChild(child ChildPreparation, actual *State) bool {
	if actual == nil || actual.SliceChild == nil {
		return false
	}
	expected := s.ChildStart(child)
	return actual.NativeVCS() == s.NativeVCS() && actual.NativeHandle == s.NativeResources[child.ResourceID].Handle && actual.RunID == child.RunID && actual.Route == "regular" && actual.BaseSnapshot == s.BaseSnapshot && actual.NativeRoot == s.NativeResources[child.ResourceID].Path && actual.SliceChild.MasterRunID == expected.Binding.MasterRunID && actual.SliceChild.SliceID == expected.Binding.SliceID && actual.SliceChild.RequirementRevision == expected.Binding.RequirementRevision && digestOfCanonical(actual.SliceChild.InheritedIntake) == digestOfCanonical(expected.Binding.InheritedIntake) && digestOfCanonical(actual.SliceChild.Coverage) == digestOfCanonical(expected.Binding.Coverage) && digestOfCanonical(actual.Topology) == digestOfCanonical(expected.Topology) && digestOfCanonical(actual.FlowSelection) == digestOfCanonical(expected.FlowSelection) && actual.IntakeConfirmationReceipt != nil && s.IntakeConfirmationReceipt != nil && sameIntakeConfirmation(*actual.IntakeConfirmationReceipt, *s.IntakeConfirmationReceipt)
}

func (e *Engine) RecordPreparedChildren(children []*State, input Observation) error {
	e = e.observing(input)
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
		if !s.preparedChild(child, children[i]) || !child.Carried && (children[i].Phase != runtime.PhasePreparing || children[i].abortRequested()) {
			return validationError("child preparation does not prove the planned child")
		}
	}
	if err := s.CompleteStep("children.prepare", e.cfg.Definition); err != nil {
		return err
	}
	_, err = e.commit(s, revision, input)
	return err
}

func (e *Engine) SignChildWorkspace(input Observation) (bool, error) {
	e = e.observing(input)
	revision, s, err := e.load()
	if err != nil {
		return false, err
	}
	if !s.ChildCreationCurrent() || !slices.Contains(s.Completed, authoring.StepID("children.prepare")) || len(s.SliceChildren) != 0 && s.ChildCreation.Previous == nil {
		return false, nil
	}
	for _, child := range s.ChildCreation.Children {
		r := s.NativeResources[child.ResourceID]
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
		_, err = e.commit(s, revision, input)
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
		if !found || intent.Step != "children.workspace" || validateResourceReceipt(intent, receipt) != nil || evidence.Digest != s.ChildCreation.Digest || s.NativeVCS() == "git" && evidence.Resource.Identity != s.ChildCreation.Source {
			continue
		}
		return evidence, true
	}
	return WorkspaceEvidence{}, false
}

// AcceptPreparedSnapshot binds the first native identity only from the
// master's original typed workspace proof while both run windows are held.
func (e *Engine) AcceptPreparedSnapshot(parent *State, input Observation) error {
	e = e.observing(input)
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
	_, err = e.commit(s, revision, input)
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

func (e *Engine) ReleaseChildren(children []*State, input Observation) error {
	e = e.observing(input)
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if !s.ChildCreationCurrent() || len(children) != len(s.ChildCreation.Children) {
		return validationError("child release no longer matches the master")
	}
	if len(s.SliceChildren) != 0 && s.ChildCreation.Previous == nil {
		return nil
	}
	for i, child := range s.ChildCreation.Children {
		if !s.preparedChild(child, children[i]) || !child.Carried && (children[i].Phase != runtime.PhasePreparing || children[i].abortRequested()) || !child.Carried && children[i].InitialSnapshot != s.childInitialSnapshot(child) {
			return validationError("child release lacks a live prepared child")
		}
		if _, proven := s.ChildWorkspaceProof(child); !child.Carried && !proven {
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
	_, err = e.commit(s, revision, input)
	return err
}

func (e *Engine) ActivateChild(parent *State, input Observation) error {
	e = e.observing(input)
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s.Phase != runtime.PhasePreparing {
		return nil
	}
	child, found := parent.CreationChildByRun(s.RunID)
	if !found || !parent.ChildCreationCurrent() || !parent.preparedChild(child, s) || parent.SliceChildren[child.SliceID] != s.RunID || s.InitialSnapshot != parent.childInitialSnapshot(child) || s.abortRequested() {
		return validationError("child activation lacks current parent release authority")
	}
	if err := s.TransitionPhase(runtime.PhaseTopologyAndRoute); err != nil {
		return err
	}
	_, err = e.commit(s, revision, input)
	return err
}

func (e *Engine) CancelChild(parent *State, input Observation) error {
	e = e.observing(input)
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
	s.SliceChild.ParentAbort = &AbortAuthority{Decision: d, Event: record, RequestDigest: parent.requestDigest(d.RequestID)}
	_, err = e.commit(s, revision, input)
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
			if len(s.SliceChildren) != 0 && s.ChildCreation.Previous == nil {
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
	return found && s.ChildCreationCurrent() && (len(s.SliceChildren) == 0 || s.ChildCreation.Previous != nil) && p.Generation == 0 && p.RouteScope == "" && p.Manifest == nil && p.RequirementRevision == s.ChildCreation.RequirementRevision && s.NativeResources[child.ResourceID] == p.Resource && p.Resource.Source == s.ChildCreation.Source && s.resourceUsable(p.Resource.ID)
}

func (s *State) RebuildingChildren() bool {
	return !s.abortRequested() && s.ChildCreation != nil && s.ChildCreation.Previous != nil && !slices.Contains(s.Completed, authoring.StepID("children.publish"))
}
func (e *Engine) CancelReplacedChild(parent *State, input Observation) error {
	e = e.observing(input)
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s.Phase == runtime.PhaseTerminal || s.abortRequested() {
		return nil
	}
	c := parent.ChildCreation
	if c == nil || c.Previous == nil {
		return validationError("replaced child lacks authorized topology")
	}
	original := *parent
	original.ChildCreation = c.Previous
	plan, ok := original.CreationChildByRun(s.RunID)
	if !ok || !original.preparedChild(plan, s) {
		return validationError("replaced child origin is not exact")
	}
	if slices.ContainsFunc(c.Children, func(child ChildPreparation) bool { return child.RunID == s.RunID }) {
		return nil
	}
	d := parent.Decisions[c.RequestID]
	event, ok := parent.Events[d.EventID]
	if !ok {
		return validationError("topology replacement lacks original user decision")
	}
	var authorization *ConfirmedRevision
	for i := range parent.RequirementRevisions {
		if parent.RequirementRevisions[i].RequestID == c.RequestID {
			authorization = &parent.RequirementRevisions[i]
		}
	}
	if authorization == nil || !slices.Contains(authorization.DecidedScope, "topology") {
		return validationError("child replacement lacks confirmed topology scope")
	}
	s.SliceChild.ParentAbort = &AbortAuthority{Decision: d, Event: event, RequestDigest: parent.requestDigest(d.RequestID), Requirement: authorization}
	_, err = e.commit(s, revision, input)
	return err
}

// replacedTargetResources derives the outstanding cleanup debt from the frozen
// predecessor and current child membership. Carried and new targets remain live.
func (s *State) replacedTargetResources() []NativeResource {
	var resources []NativeResource
	for _, old := range s.ChildCreation.Previous.Children {
		if !slices.ContainsFunc(s.ChildCreation.Children, func(c ChildPreparation) bool { return c.RunID == old.RunID }) {
			r := s.NativeResources[old.ResourceID]
			if r.Status != "CLEANED" {
				resources = append(resources, r)
			}
		}
	}
	slices.SortFunc(resources, func(a, b NativeResource) int { return strings.Compare(a.ID, b.ID) })
	return resources
}

func (e *Engine) CleanupReplacedTargets(input Observation) error {
	e = e.observing(input)
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if !s.RebuildingChildren() {
		return nil
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.cleanup" {
			return nil
		}
	}
	resources := s.replacedTargetResources()
	if len(resources) == 0 {
		return nil
	}
	intent, err := e.prepareAdapterIntent(s, revision+1, "op.cleanup", CleanupParameters{Resources: resources}, "")
	if err != nil {
		return err
	}
	s.PendingHostActions[intent.ActionID] = intent
	_, err = e.commit(s, revision, input)
	return err
}

func (s *State) childInitialSnapshot(child ChildPreparation) string {
	proof, ok := s.ChildWorkspaceProof(child)
	if !ok {
		return ""
	}
	return proof.Resource.Identity
}
