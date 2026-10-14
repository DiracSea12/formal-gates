package protocol

import (
	"fmt"
	"slices"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
)

// FlowSelection owns fixed main-flow choices. SelectedGates owns dynamic gates
// and QA modes; continuation decisions do not alter these persistent choices.
type FlowSelection struct {
	Disabled []string `json:"disabled"`
}

func FlowNodeIDs() []string {
	nodes := definition.MainFlowNodes()
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

func ValidateFlowSelection(selection *FlowSelection) error {
	if selection == nil {
		return nil
	}
	ids := FlowNodeIDs()
	for i, id := range selection.Disabled {
		if !slices.Contains(ids, id) {
			return fmt.Errorf("flow selection: unknown main-flow node %q (available: %v)", id, ids)
		}
		if slices.Contains(selection.Disabled[:i], id) {
			return fmt.Errorf("flow selection: node %q is disabled more than once", id)
		}
	}
	return nil
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
