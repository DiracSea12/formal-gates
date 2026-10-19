package protocol

import (
	"slices"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

func configurationNodeEnabled(configuration settings.Configuration, id string) bool {
	if id == "qa-blackbox" || id == "qa-whitebox" {
		return configuration.QAModeEnabled(id[3:])
	}
	return configuration.FlowSelection == nil || !slices.Contains(configuration.FlowSelection.Disabled, id)
}

// ExecutionNodeState describes current obligations, independently of admission
// capacity or a pending user authorization. Native ownership owns Running.
type ExecutionNodeState struct {
	Node         string `json:"node"`
	Enabled      bool   `json:"enabled"`
	Owed         bool   `json:"owed"`
	Dispatchable bool   `json:"dispatchable"`
	Completed    bool   `json:"completed"`
	Running      bool   `json:"running"`
	Editable     bool   `json:"editable"`
	Closing      bool   `json:"closing"`
	Bypassed     bool   `json:"bypassed"`
}

func (s *State) executionObligations(cd *compiler.CompiledDefinition, configuration settings.Configuration) map[string]bool {
	owed := map[string]bool{}
	if s.Phase == runtime.PhaseTerminal {
		return owed
	}
	projected := *s
	projected.FlowSelection = nil
	mergeNode := "qa-blackbox"
	if slices.Contains(configuration.QAModes, "whitebox") {
		mergeNode = "qa-whitebox"
	}
	oweTask := func(task runtime.TaskKey) {
		node := s.ExecutionNode(task, cd)
		if s.validationMode(task) == coverage.ReviewMerge {
			node = mergeNode
		}
		owed[node] = true
	}
	view := projected.decisionView(cd)
	for _, tasks := range view.RequiredTasks {
		for _, task := range tasks {
			if projected.TaskStatusOf(task.Task) != runtime.TaskTerminal {
				oweTask(task.Task)
			}
		}
	}
	for _, mark := range []review.Action{review.ProductReview, review.StartReadiness} {
		series, exists := s.ReviewSeries.Series(mark)
		if s.SliceChild == nil && exists && !s.effectivePass(series) {
			owed[string(mark)] = true
		}
	}
	if pipelineEnabled(s, cd) {
		_, plan, _ := s.confirmedDevelopmentPlan()
		if !s.mergeMaster() && (plan == nil || !s.developmentPlanCurrent(plan)) {
			owed["development"] = true
		}
		for _, task := range projected.planTasks(cd) {
			if s.developmentResult(task) == nil {
				owed["development"] = true
			}
		}
		wave := s.currentValidationWave()
		futureSources := owed["development"] || s.nextRepairWork() != nil
		frozenModes := map[coverage.ReviewKind]bool{}
		frozenGates := map[string]bool{}
		if wave != nil && !futureSources {
			for _, target := range wave.Targets {
				frozenModes[target.Mode] = true
				frozenGates[target.Gate] = true
			}
		}
		// Selected duties absent from the frozen wave still require preparation
		// and execution. Existing targets retain their actual completion status.
		for _, mode := range s.validationKinds() {
			if frozenModes[mode] {
				continue
			}
			switch mode {
			case coverage.ReviewBlackbox:
				owed["qa-blackbox"] = true
			case coverage.ReviewWhitebox:
				owed["qa-whitebox"] = true
			case coverage.ReviewMerge:
				owed[mergeNode] = true
			}
		}
		for _, gate := range s.SelectedGates {
			if gate != "blackbox" && gate != "whitebox" && gate != mergeQAGateID && !frozenGates[gate] {
				owed["gate"] = true
			}
		}
		if wave != nil {
			for _, target := range wave.Targets {
				if target.Kind == "QA" && !slices.Contains(projected.validationKinds(), target.Mode) || target.Kind == "GATE" && !slices.Contains(s.SelectedGates, target.Gate) {
					continue
				}
				if target.Status == "PENDING" || target.Status == "RUNTIME_ERROR" || target.Status == "RETRY_EXHAUSTED" {
					oweTask(target.Task)
				}
			}
		}
		if s.nextRepairWork() != nil {
			owed["repair"] = true
		}
	}
	return owed
}

func (e *Engine) executionNodeStates(s *State) ([]ExecutionNodeState, error) {
	configuration := s.AppliedConfiguration()
	if e.cfg.Configuration != nil && s.Phase != runtime.PhaseTerminal {
		if current, err := e.cfg.Configuration(s.ConfigurationName()); err == nil {
			configuration = current
		}
	}
	return e.executionNodeStatesFor(s, configuration)
}

// ConfigurationNodeStates previews a candidate value's node obligations
// against the current run for read-only display; it reconciles nothing.
func (e *Engine) ConfigurationNodeStates(configuration settings.Configuration) ([]ExecutionNodeState, error) {
	_, state, err := e.load()
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	return e.executionNodeStatesFor(state, configuration)
}

func (e *Engine) executionNodeStatesFor(s *State, configuration settings.Configuration) ([]ExecutionNodeState, error) {
	gates, parents, err := e.configurationContext(s)
	if err != nil {
		return nil, err
	}
	projected := *s
	projected.selectConfiguration(configuration, gates)
	// Closing a route cannot erase its outstanding work. The saved route can
	// also introduce real duties not represented by that route's frozen wave.
	owed := s.executionObligations(e.cfg.Definition, s.AppliedConfiguration())
	for node := range projected.executionObligations(e.cfg.Definition, configuration) {
		owed[node] = true
	}
	running := map[string]bool{}
	for id, pending := range s.liveWorkerActions() {
		if _, returned := s.Results[id]; returned {
			continue
		}
		claimed, err := e.dispatchClaimed(s, id)
		if err != nil {
			return nil, err
		}
		if claimed {
			node := s.ExecutionNode(pending.Task, e.cfg.Definition)
			if node == "qa" {
				node = "qa-blackbox"
				if slices.Contains(s.AppliedConfiguration().QAModes, "whitebox") {
					node = "qa-whitebox"
				}
			}
			running[node] = true
		}
	}
	var closing []string
	bypassed := false
	if len(s.AppliedConfigurations) != 0 {
		entry := s.AppliedConfigurations[len(s.AppliedConfigurations)-1]
		closing, bypassed = entry.PendingNodes, entry.Bypassed
	}
	if len(parents) != 0 {
		bypassed = parents[0].State.SliceEnteredMainline(s.RunID)
	}
	var nodes []ExecutionNodeState
	for _, id := range ExecutionNodeIDs() {
		enabled := configurationNodeEnabled(configuration, id)
		remaining := owed[id] && !bypassed
		completed := !remaining && !running[id]
		nodes = append(nodes, ExecutionNodeState{Node: id, Enabled: enabled, Owed: remaining, Dispatchable: remaining && projected.nodeDispatchEnabled(configuration, id), Completed: completed, Running: running[id], Editable: !completed && !running[id] && !bypassed, Closing: !enabled && running[id] || slices.Contains(closing, id), Bypassed: bypassed})
	}
	return nodes, nil
}
