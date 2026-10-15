package facade

import (
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
)

// FlowNodeFace projects configured main-flow choices separately from the
// business nodes that can be selected for continuation.
type FlowNodeFace struct {
	definition.FlowNode
	Enabled bool `json:"enabled"`
}

func flowNodeFaces(state *protocol.State) []FlowNodeFace {
	var nodes []FlowNodeFace
	for _, node := range definition.MainFlowNodes() {
		nodes = append(nodes, FlowNodeFace{FlowNode: node, Enabled: state.FlowNodeEnabled(node.ID)})
	}
	return nodes
}

// ExecutionNodeFace projects each selectable group's effective execution choice.
type ExecutionNodeFace struct {
	definition.FlowNode
	Execution protocol.ExecutionChoice `json:"execution"`
}

func executionNodeFaces(state *protocol.State) []ExecutionNodeFace {
	var nodes []ExecutionNodeFace
	for _, node := range definition.ExecutionNodes() {
		nodes = append(nodes, ExecutionNodeFace{FlowNode: node, Execution: state.EffectiveExecutionChoice(node.ID)})
	}
	return nodes
}
