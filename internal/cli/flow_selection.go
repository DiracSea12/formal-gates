package cli

import (
	"fmt"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
)

func workflowFlowNodeChoices() []map[string]any {
	var choices []map[string]any
	for index, node := range definition.MainFlowNodes() {
		choices = append(choices, map[string]any{"node": index + 1, "description": node.Description})
	}
	return choices
}

func selectFlowNodes(positions []int) (*protocol.FlowSelection, error) {
	if len(positions) == 0 {
		return nil, nil
	}
	nodes := protocol.FlowNodeIDs()
	selection := &protocol.FlowSelection{}
	for _, position := range positions {
		if position < 1 || position > len(nodes) {
			return nil, fmt.Errorf("flow node position %d is outside the current %d choices", position, len(nodes))
		}
		selection.Disabled = append(selection.Disabled, nodes[position-1])
	}
	if err := protocol.ValidateFlowSelection(selection); err != nil {
		return nil, err
	}
	return selection, nil
}
