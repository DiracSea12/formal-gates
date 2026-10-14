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
