package protocol

import (
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

func TestSliceReceiptVerifiesFrozenFlowSelection(t *testing.T) {
	t.Parallel()
	for _, selection := range []struct {
		name string
		flow *FlowSelection
	}{
		{name: "default"},
		{name: "disabled responsibilities", flow: &FlowSelection{Disabled: []string{"gate", "qa"}}},
	} {
		t.Run(selection.name, func(t *testing.T) {
			state := State{RouteConfirmed: true, SelectedGates: []string{"quality"}, Topology: &TopologyBinding{Decision: topologySplit}, FlowSelection: selection.flow}
			r := SliceTerminalReceipt{
				Status: runtime.PhaseSliceReady, ChildRunID: "child", SliceID: "core", MasterRunID: "master",
				Snapshot: "snapshot", Tree: "tree", Digest: "tree", RequirementRevision: "requirement",
				Topology: *state.Topology, RouteConfirmed: true, SelectedGates: state.SelectedGates, FlowSelection: selection.flow,
				Validation: SliceValidationReceipt{Status: "COMPLETE", RequirementRevision: "requirement", RouteDigest: state.validationRouteDigest(), Candidate: coverage.ValidationCandidate{Digest: "tree"}},
			}
			receipt := SliceReceipt{Receipt: r, Digest: r.CanonicalDigest()}
			if err := receipt.verify(); err != nil {
				t.Fatal("receipt rejected its frozen workflow selection", err)
			}
			if selection.flow == nil {
				receipt.Receipt.FlowSelection = &FlowSelection{Disabled: []string{"gate"}}
			} else {
				receipt.Receipt.FlowSelection = nil
			}
			receipt.Digest = receipt.Receipt.CanonicalDigest()
			if err := receipt.verify(); err == nil || rejectionCode(t, err) != CodeReceiptConflict {
				t.Fatal("receipt accepted a workflow selection outside its frozen route binding", err)
			}
		})
	}
}
