package validate

import (
	"reflect"
	"testing"
)

// TestReproQACarryRegression reproduces the complexity-gate P1 finding: with QA
// selected, main-agent carry must rebind QAExecution.Snapshot to the current
// snapshot, not write a spurious Gates["qa"] entry.
func TestReproQACarryRegression(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "repro-qa-carry", "custom", []string{blackboxQAID, "architecture"})
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.QACases), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.QAExecution.Snapshot != state.CurrentSnapshot {
		t.Fatalf("QA execution snapshot=%s want=%s", state.QAExecution.Snapshot, state.CurrentSnapshot)
	}
	state = recordGateResult(t, root, pkg, state, "architecture", "repro-arch", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	state = advanceRepair(t, root, pkg, state, "repro-repair")
	if got := eligibleMainCarryResults(state, false); !reflect.DeepEqual(got, []string{blackboxQAID}) {
		t.Fatalf("eligible carry results=%v want=[blackbox]", got)
	}
	state, err = RecordCarry(root, pkg, state.RunID, "", nil, "", true, "repair does not touch QA behavior")
	if err != nil {
		t.Fatal(err)
	}
	if state.QAExecution.Snapshot != state.CurrentSnapshot {
		t.Fatalf("QAExecution.Snapshot=%s want=%s (carry failed to rebind QA snapshot)", state.QAExecution.Snapshot, state.CurrentSnapshot)
	}
	if _, ok := state.Gates["qa"]; ok {
		t.Fatalf("spurious Gates[qa] entry written: %#v", state.Gates["qa"])
	}
}
