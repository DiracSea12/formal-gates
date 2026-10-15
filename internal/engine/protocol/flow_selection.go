package protocol

import (
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

type FlowSelection = settings.FlowSelection

func FlowNodeIDs() []string {
	nodes := definition.MainFlowNodes()
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

func ValidateFlowSelection(selection *FlowSelection) error {
	return settings.ValidateFlowSelection(selection)
}

func (s *State) FlowNodeEnabled(id string) bool {
	return s.FlowSelection == nil || !slices.Contains(s.FlowSelection.Disabled, id)
}

// PreDevelopmentReviewsSatisfied checks real PASS only for selected reviews.
// A disabled review retains its original result, effective status and quota.
func (s *State) PreDevelopmentReviewsSatisfied() bool {
	return s.preDevelopmentReviewSatisfied(review.ProductReview) && s.preDevelopmentReviewSatisfied(review.StartReadiness)
}

func (s *State) preDevelopmentReviewSatisfied(action review.Action) bool {
	if !s.FlowNodeEnabled(string(action)) {
		return true
	}
	series, ok := s.ReviewSeries.Series(action)
	return ok && s.effectivePass(series)
}

func flowStepNode(step compiler.CompiledStep) string {
	payload, ok := step.Payload.(compiler.CompiledAgentStep)
	if !ok {
		return ""
	}
	if validReviewAction(review.Action(payload.ReviewSeries)) {
		return string(payload.ReviewSeries)
	}
	for _, node := range definition.MainFlowNodes() {
		if slices.Contains(node.Handlers, payload.Handler) {
			return node.ID
		}
	}
	return ""
}

func (s *State) projectFlowSelection(view *decision.State, cd *compiler.CompiledDefinition) {
	for _, step := range cd.Steps {
		if node := flowStepNode(step); node != "" && !s.FlowNodeEnabled(node) {
			if node == "repair" {
				for i := range view.RequiredTasks[step.Header.ID] {
					view.RequiredTasks[step.Header.ID][i].BlockedBy = decision.WaitDependencies
				}
			} else {
				view.RequiredTasks[step.Header.ID] = nil
			}
		}
	}
}

func (s *State) taskFlowEnabled(task runtime.TaskKey, cd *compiler.CompiledDefinition) bool {
	node := s.ExecutionNode(task, cd)
	switch node {
	case "qa-blackbox":
		return s.FlowNodeEnabled("qa") && slices.Contains(s.SelectedGates, "blackbox")
	case "qa-whitebox":
		return s.FlowNodeEnabled("qa") && slices.Contains(s.SelectedGates, "whitebox")
	case "qa":
		return s.FlowNodeEnabled("qa") && slices.Contains(s.validationKinds(), coverage.ReviewMerge)
	case "gate":
		if !s.FlowNodeEnabled("gate") {
			return false
		}
		for _, gate := range s.SelectedGates {
			if gate != "blackbox" && gate != "whitebox" && strings.HasSuffix(s.sourceValidationTask(task).Scope, ":"+gate) {
				return true
			}
		}
		return false
	}
	return s.FlowNodeEnabled(node)
}

func (s *State) flowNodeWasDisabled(id string) bool {
	if !s.FlowNodeEnabled(id) {
		return true
	}
	if s.Settings != nil && s.Settings.Configuration.FlowSelection != nil && slices.Contains(s.Settings.Configuration.FlowSelection.Disabled, id) {
		return true
	}
	for _, decision := range s.Decisions {
		if decision.Control == ControlSelectRoute && decision.Choice == "approve" && decision.Review != nil && decision.Review.Route != nil && decision.Review.Route.Configuration != nil {
			selection := decision.Review.Route.Configuration.Configuration.FlowSelection
			if selection != nil && slices.Contains(selection.Disabled, id) {
				return true
			}
		}
	}
	return false
}
