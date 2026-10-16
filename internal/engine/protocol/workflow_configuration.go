package protocol

import (
	"slices"

	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

// ConfigurationSelection is the complete suite shown on a SELECT_ROUTE Ask.
// Configuration.Execution contains resolved tuples, including native omissions.
// Decisions owns accepted changes; the run's Settings retains its creation defaults.
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
	selected := ConfigurationSelection{Name: settings.DefaultConfigurationName, Configuration: settings.DefaultConfiguration()}
	if s.Settings != nil {
		selected = ConfigurationSelection{Name: s.Settings.ConfigurationName, Source: s.Settings.ConfigurationSource, Configuration: s.Settings.Configuration}
	}
	var revision uint64
	for _, d := range s.Decisions {
		if d.Control == ControlSelectRoute && d.Choice == "approve" && d.Review != nil && d.Review.Route != nil && d.Review.Route.Configuration != nil && d.Revision > revision {
			selected, revision = *d.Review.Route.Configuration, d.Revision
		}
	}
	selected.Configuration.FlowSelection = s.FlowSelection
	if s.RouteConfirmed || revision > 0 {
		selected.SetSelectedGates(s.SelectedGates)
	}
	selected.Configuration.Execution = make(map[string]ExecutionChoice, len(ExecutionNodeIDs()))
	for _, node := range ExecutionNodeIDs() {
		selected.Configuration.Execution[node] = s.EffectiveExecutionChoice(node)
	}
	return selected
}

func (e *Engine) configurationPreview(s *State, selected ConfigurationSelection, gates []string, revision uint64, parents ...Snapshot) (NodeContinuationPreview, error) {
	projected := *s
	projected.FlowSelection = selected.Configuration.FlowSelection
	projected.SelectedGates = selected.SelectedGates(gates)
	if s.mergeMaster() {
		projected.SelectedGates = mergeRouteGates()
	}
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

func (s *State) RouteConfigurationRequired() bool {
	if s.RouteConfirmed {
		return true
	}
	for _, d := range s.Decisions {
		if d.Control == ControlSelectRoute {
			return true
		}
	}
	return false
}
