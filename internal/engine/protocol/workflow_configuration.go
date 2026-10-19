package protocol

import (
	"errors"
	"fmt"
	"reflect"
	"slices"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

// ConfigurationUnavailable reports a bound configuration the library cannot
// resolve. New dispatch stops until a later evaluation resolves it again.
type ConfigurationUnavailable struct {
	Reason string
}

func (e *ConfigurationUnavailable) Error() string { return e.Reason }

// AppliedConfiguration is one entry of the run's applied-value history: the
// complete library value in effect from Revision and the fields it changed.
// The first entry is the initial snapshot and lists no changed fields.
type AppliedConfiguration struct {
	Revision      uint64                    `json:"revision"`
	Configuration settings.Configuration    `json:"configuration"`
	Changed       []string                  `json:"changed,omitempty"`
	SelectedGates []string                  `json:"selectedGates,omitempty"`
	Preview       *NodeContinuationPreview  `json:"preview,omitempty"`
	Effect        *NodeContinuationDecision `json:"effect,omitempty"`
	PendingNodes  []string                  `json:"pendingNodes,omitempty"`
	Bypassed      bool                      `json:"bypassed,omitempty"`
	RouteChanged  bool                      `json:"routeChanged,omitempty"`
}

// ConfigurationName is the run's binding into the configuration library.
func (s *State) ConfigurationName() string {
	if s.Settings == nil {
		return settings.DefaultConfigurationName
	}
	return s.Settings.ConfigurationName
}

// AppliedConfiguration is the latest value of the applied-value history.
func (s *State) AppliedConfiguration() settings.Configuration {
	if len(s.AppliedConfigurations) == 0 {
		return settings.Configuration{}
	}
	for i := len(s.AppliedConfigurations) - 1; i >= 0; i-- {
		if !s.AppliedConfigurations[i].Bypassed {
			return s.AppliedConfigurations[i].Configuration
		}
	}
	return settings.Configuration{}
}

// reconcileConfiguration reads the bound configuration and records a changed
// value, rebinding unclaimed dispatches whose execution choice changed. A
// library that cannot be read or resolved keeps the latest applied value. A
// terminal run has no further dispatch evaluation.
func (e *Engine) reconcileConfiguration(s *State, nextRevision uint64) (bool, error) {
	if e.cfg.Configuration == nil || s.Phase == runtime.PhaseTerminal || s.Phase == runtime.PhasePreparing || s.abortRequested() || s.resetPending() || s.requirementChangePending() {
		return false, nil
	}
	current, err := e.cfg.Configuration(s.ConfigurationName())
	if err != nil {
		return false, nil
	}
	previous := s.AppliedConfigurations[len(s.AppliedConfigurations)-1].Configuration
	changed := configurationChanges(previous, current)
	gates, parents, err := e.configurationContext(s)
	if err != nil {
		return false, err
	}
	projected := *s
	projected.selectConfiguration(current, gates)
	if len(changed) == 0 && sameGateSet(s.AppliedConfigurations[len(s.AppliedConfigurations)-1].SelectedGates, projected.SelectedGates) {
		return e.advanceConfiguration(s, nextRevision)
	}
	if len(changed) == 0 {
		changed = []string{"gates"}
	}
	preview, err := e.configurationPreview(s, ConfigurationSelection{Configuration: current}, gates, nextRevision, parents...)
	if err != nil {
		return false, err
	}
	routeChanged := projected.validationRouteDigest() != s.validationRouteDigest()
	if routeChanged && len(preview.ParentClaims) != 0 {
		return false, nil
	}
	nodes := ExecutionNodeIDs()
	entry := AppliedConfiguration{Revision: nextRevision, Configuration: settings.CloneConfiguration(current), Changed: changed, SelectedGates: slices.Clone(projected.SelectedGates), Preview: &preview, Bypassed: preview.EnteredMainline, RouteChanged: routeChanged}
	entry.Effect = &NodeContinuationDecision{PreviewDigest: preview.Digest, Reason: "Apply the bound configuration", Work: slices.Clone(preview.Work)}
	s.AppliedConfigurations = append(s.AppliedConfigurations, entry)
	for id, ask := range s.PendingAsks {
		if !ask.Resolved && ask.Control == ControlSelectRoute && !sameGateSet(ask.RouteGates, gates) {
			ask.Resolved = true
			s.PendingAsks[id] = ask
		}
	}
	if entry.Bypassed {
		return true, nil
	}
	s.applyRoundLimits(current, nextRevision)
	for _, node := range nodes {
		choice := s.EffectiveExecutionChoice(node)
		if err := e.rebindUnclaimedExecution(s, node, choice); err != nil {
			return false, err
		}
	}
	_, err = e.advanceConfiguration(s, nextRevision)
	return true, err
}

func (s *State) configurationAuthority() *AppliedConfiguration {
	for i := len(s.AppliedConfigurations) - 1; i >= 0; i-- {
		entry := s.AppliedConfigurations[i]
		if !entry.Bypassed && entry.RouteChanged && entry.Effect != nil && entry.Effect.AppliedRevision != 0 {
			return &entry
		}
	}
	return nil
}

func (e *Engine) configurationContext(s *State) ([]string, []Snapshot, error) {
	gates := slices.Clone(s.SelectedGates)
	var parents []Snapshot
	if e.cfg.RouteGates != nil {
		var err error
		gates, err = e.cfg.RouteGates()
		if err != nil {
			return nil, nil, err
		}
	}
	if s.SliceChild != nil {
		gates = slices.DeleteFunc(gates, func(g string) bool { return g == "blackbox" || g == "whitebox" })
		if e.cfg.ParentSnapshot != nil {
			parent, err := e.cfg.ParentSnapshot(s.SliceChild.MasterRunID)
			if err != nil {
				return nil, nil, err
			}
			parents = append(parents, parent)
		}
	}
	return gates, parents, nil
}

func (s *State) selectConfiguration(configuration settings.Configuration, gates []string) {
	s.FlowSelection = configuration.FlowSelection
	s.SelectedGates = (ConfigurationSelection{Configuration: configuration}).SelectedGates(gates)
	if s.mergeMaster() {
		s.SelectedGates = mergeRouteGates()
		if !configurationNodeEnabled(configuration, "qa-blackbox") && !configurationNodeEnabled(configuration, "qa-whitebox") {
			s.SelectedGates = slices.DeleteFunc(s.SelectedGates, func(gate string) bool { return gate == mergeQAGateID })
		}
	}
	if !slices.Contains(s.SelectedGates, "blackbox") {
		s.QAWorktree = ""
	}
}

func configurationRequest(revision uint64) string { return fmt.Sprintf("configuration:%d", revision) }

// advanceConfiguration keeps claimed work on its original business path. Only
// unclaimed work may be retired by a configuration application.
func (e *Engine) advanceConfiguration(s *State, revision uint64) (bool, error) {
	if len(s.AppliedConfigurations) == 0 {
		return false, nil
	}
	index := len(s.AppliedConfigurations) - 1
	entry := s.AppliedConfigurations[index]
	if entry.Bypassed || entry.Effect == nil || entry.Effect.AppliedRevision != 0 {
		return false, nil
	}
	gates, parents, err := e.configurationContext(s)
	if err != nil {
		return false, err
	}
	preview, err := e.configurationPreview(s, ConfigurationSelection{Configuration: entry.Configuration}, gates, revision, parents...)
	if err != nil {
		return false, err
	}
	if preview.EnteredMainline {
		entry.Bypassed = true
		s.AppliedConfigurations[index] = entry
		return true, nil
	}
	projected := *s
	projected.selectConfiguration(entry.Configuration, gates)
	routeChanged := projected.validationRouteDigest() != s.validationRouteDigest()
	if routeChanged && len(preview.ParentClaims) != 0 {
		return false, nil
	}
	nodes, err := e.executionNodeStatesFor(s, entry.Configuration)
	if err != nil {
		return false, err
	}
	unfinished, enabledOwed := false, false
	for _, node := range nodes {
		unfinished = unfinished || node.Owed
		enabledOwed = enabledOwed || node.Dispatchable
	}
	if unfinished && !enabledOwed {
		return false, nil
	}
	var pendingNodes []string
	var unclaimed []string
	for id, pending := range s.liveWorkerActions() {
		affected := !projected.taskFlowEnabled(pending.Task, e.cfg.Definition) || routeChanged && s.validationWaveForTask(pending.Task) != nil
		if !affected {
			continue
		}
		claimed, err := e.dispatchClaimed(s, id)
		if err != nil {
			return false, err
		}
		if claimed {
			pendingNodes = append(pendingNodes, s.ExecutionNode(pending.Task, e.cfg.Definition))
		} else {
			unclaimed = append(unclaimed, id)
		}
	}
	slices.Sort(pendingNodes)
	pendingNodes = slices.Compact(pendingNodes)
	changed := !slices.Equal(entry.PendingNodes, pendingNodes)
	entry.PendingNodes = pendingNodes
	// Unclaimed dispatches may be retired even while another call finishes.
	for _, id := range unclaimed {
		pending, retired := s.retireAction(id, "", "Bound configuration changed the responsibility")
		if retired {
			if err := s.terminalizeTask(pending.Task); err != nil {
				return false, err
			}
			changed = true
		}
	}
	if len(pendingNodes) != 0 {
		s.AppliedConfigurations[index] = entry
		return changed, nil
	}
	// Refresh the effect from current work: results returned while closing are
	// carried, not retrospectively invalidated.
	preview.StateRevision = entry.Revision
	preview.Digest = preview.CanonicalDigest()
	entry.Preview = &preview
	entry.Effect.PreviewDigest = preview.Digest
	entry.Effect.Work = preview.Work
	entry.Effect.AppliedRevision = revision
	s.AppliedConfigurations[index] = entry
	s.selectConfiguration(entry.Configuration, gates)
	if err := e.applyContinuationWork(s, *entry.Preview, *entry.Effect, configurationRequest(entry.Revision), revision, revision); err != nil {
		return false, err
	}
	return true, nil
}

func (e *Engine) dispatchClaimed(s *State, id string) (bool, error) {
	if _, ok := s.SpawnReceipts[id]; ok {
		return true, nil
	}
	if e.cfg.WorkerExecution == nil {
		return false, nil
	}
	pending, attempt, _ := s.workerActionTarget(id)
	receipt, err := e.cfg.WorkerExecution(pending, attempt)
	return receipt != nil, err
}

func (s *State) liveWorkerActions() map[string]PendingAction {
	actions := make(map[string]PendingAction, len(s.PendingActions))
	for id, pending := range s.PendingActions {
		actions[id] = pending
	}
	for task, attempt := range s.EndedAttempts {
		if attempt.AwaitingResult {
			actions[attempt.ActionID] = PendingAction{ActionID: attempt.ActionID, Task: task, AttemptID: attempt.ID}
		}
	}
	return actions
}

func (s *State) nodeDispatchEnabled(configuration settings.Configuration, node string) bool {
	if !configurationNodeEnabled(configuration, node) {
		return false
	}
	if node == "gate" {
		for _, gate := range s.SelectedGates {
			if gate != "blackbox" && gate != "whitebox" && gate != mergeQAGateID {
				return true
			}
		}
		return false
	}
	return true
}

// configurationChanges preserves nil Gates as distinct from an explicit list.
func configurationChanges(previous, current settings.Configuration) []string {
	var changed []string
	if !reflect.DeepEqual(previous.FlowSelection, current.FlowSelection) {
		changed = append(changed, "flowSelection")
	}
	if !reflect.DeepEqual(previous.QAModes, current.QAModes) {
		changed = append(changed, "qaModes")
	}
	if !reflect.DeepEqual(previous.Gates, current.Gates) {
		changed = append(changed, "gates")
	}
	keys := map[string]bool{}
	for key := range previous.Execution {
		keys[key] = true
	}
	for key := range current.Execution {
		keys[key] = true
	}
	for key := range keys {
		before, beforePresent := previous.Execution[key]
		after, afterPresent := current.Execution[key]
		if beforePresent != afterPresent || before != after {
			changed = append(changed, "execution."+key)
		}
	}
	keys = map[string]bool{}
	for key := range previous.RoundLimits {
		keys[key] = true
	}
	for key := range current.RoundLimits {
		keys[key] = true
	}
	for key := range keys {
		before, beforePresent := previous.RoundLimits[key]
		after, afterPresent := current.RoundLimits[key]
		if beforePresent != afterPresent || before != after {
			changed = append(changed, "roundLimits."+key)
		}
	}
	slices.Sort(changed)
	return changed
}

// holdUnavailableConfiguration replaces new dispatch with a reported wait while
// the bound configuration is unavailable. Issued work still advances.
func (e *Engine) holdUnavailableConfiguration(s *State, plan *decision.Plan) error {
	if e.cfg.Configuration == nil || s.Phase == runtime.PhaseTerminal || plan.Next.Kind == decision.KindComplete || plan.Next.Kind == decision.KindHostAction {
		return nil
	}
	var unavailable *ConfigurationUnavailable
	configuration, readErr := e.cfg.Configuration(s.ConfigurationName())
	if !errors.As(readErr, &unavailable) {
		if readErr != nil {
			configuration = s.AppliedConfiguration()
		}
		nodes, err := e.executionNodeStatesFor(s, configuration)
		if err != nil {
			return err
		}
		unfinished, dispatchable := false, false
		for _, node := range nodes {
			unfinished = unfinished || node.Owed
			dispatchable = dispatchable || node.Dispatchable
		}
		if !unfinished || dispatchable {
			return nil
		}
		unavailable = &ConfigurationUnavailable{Reason: "No enabled node has an outstanding dispatch obligation"}
	}
	plan.Next = decision.NextResult{Kind: decision.KindWait, Wait: &decision.WaitPayload{Reason: decision.WaitConfigurationUnavailable, Detail: unavailable.Reason}}
	if len(s.ExecutableHostActions(e.cfg.Definition)) > 0 {
		plan.Next = decision.NextResult{Kind: decision.KindHostAction, HostAction: &decision.HostActionPayload{}}
	}
	return nil
}

// ConfigurationSelection projects the bound name and its application value.
type ConfigurationSelection struct {
	Name          string                 `json:"name,omitempty"`
	Source        string                 `json:"source,omitempty"`
	Configuration settings.Configuration `json:"configuration"`
}

func (c ConfigurationSelection) SelectedGates(candidates []string) []string {
	var selected []string
	for _, gate := range candidates {
		if gate == "blackbox" || gate == "whitebox" {
			if slices.Contains(c.Configuration.QAModes, gate) {
				selected = append(selected, gate)
			}
		} else if c.Configuration.Gates == nil || slices.Contains(*c.Configuration.Gates, gate) {
			selected = append(selected, gate)
		}
	}
	return selected
}

func (c *ConfigurationSelection) SetSelectedGates(selected []string) {
	gates := []string{}
	c.Configuration.QAModes = []string{}
	for _, gate := range selected {
		if gate == "blackbox" || gate == "whitebox" {
			c.Configuration.QAModes = append(c.Configuration.QAModes, gate)
		} else if !slices.Contains(reservedRouteGates, gate) {
			gates = append(gates, gate)
		}
	}
	c.Configuration.Gates = &gates
}

func (s *State) EffectiveConfiguration() ConfigurationSelection {
	selected := ConfigurationSelection{Name: s.ConfigurationName(), Configuration: s.AppliedConfiguration()}
	return selected
}

func (e *Engine) configurationPreview(s *State, selected ConfigurationSelection, gates []string, revision uint64, parents ...Snapshot) (NodeContinuationPreview, error) {
	projected := *s
	projected.selectConfiguration(selected.Configuration, gates)
	p := NodeContinuationPreview{From: s.businessFrom(), FromPhase: s.Phase, To: BusinessNode{ID: "configuration", Kind: "configuration"}, RequirementRevision: s.RequirementRevision(), StateRevision: revision}
	if err := s.bindContinuationParent(&p, parents); err != nil {
		return p, err
	}
	routeChanged := projected.validationRouteDigest() != s.validationRouteDigest()
	for _, work := range s.RequirementWork(e.cfg.Definition) {
		disposition := RequirementWorkCarry
		reason := "Apply the confirmed workflow configuration"
		unfinished := s.liveDispatch(work.ActionID) || s.TaskStatusOf(work.Task) != runtime.TaskTerminal
		if unfinished && !projected.taskFlowEnabled(work.Task, e.cfg.Definition) {
			disposition = RequirementWorkRedo
		} else if unfinished && routeChanged && s.validationWaveForTask(work.Task) != nil {
			reason = "Carry the unchanged duty; retire its prior wave execution and rebind the confirmed route"
		}
		p.Work = append(p.Work, RequirementWorkDecision{Work: work, Disposition: disposition, Reason: reason})
	}
	s.eachSeriesLiveDispatch(e.reviewSeriesOf, func(action string, mark review.Action) {
		if !projected.FlowNodeEnabled(string(mark)) {
			p.ReviewActions = append(p.ReviewActions, action)
		}
	})
	for _, attempt := range s.EndedAttempts {
		mark := e.reviewSeriesOf(attempt.Task.Step)
		if attempt.AwaitingResult && validReviewAction(mark) && !projected.FlowNodeEnabled(string(mark)) && !slices.Contains(p.ReviewActions, attempt.ActionID) {
			p.ReviewActions = append(p.ReviewActions, attempt.ActionID)
		}
	}
	slices.Sort(p.ReviewActions)
	for id, intent := range s.PendingHostActions {
		if s.nodeAffectsIntent(p, intent) {
			p.Effects = append(p.Effects, id)
		}
	}
	slices.Sort(p.Effects)
	p.Digest = p.CanonicalDigest()
	return p, nil
}
