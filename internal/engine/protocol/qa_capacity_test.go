package protocol

import (
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

func TestQAAdmissionQueuesAtItsLimitAndFillsIndependentTotalCapacity(t *testing.T) {
	t.Parallel()
	engine := &Engine{cfg: Config{Definition: compiledCanonicalWorkflow(t), Capacity: 3}}
	state := &State{Settings: &settings.Effective{Values: settings.Values{Capacity: 3, QACapacity: 1}}, PendingActions: map[string]PendingAction{"active-qa": {Task: runtime.TaskKey{Step: "qa.design"}}}}
	ready := []decision.ReadyTask{{Task: runtime.TaskKey{Step: "qa.execution", Scope: "blackbox"}, Step: "qa.execution"}, {Task: runtime.TaskKey{Step: "development.worker", Scope: "independent"}, Step: "development.worker"}, {Task: runtime.TaskKey{Step: "development.repair", Scope: "other"}, Step: "development.repair"}}
	plan := &decision.Plan{Next: decision.NextResult{Kind: decision.KindReady, Ready: &decision.ReadyPayload{Tasks: ready}}}
	issued, err := engine.selectIssued(state, plan, decision.Admission{Capacity: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(issued) != 2 || issued[0].Task != ready[1].Task || issued[1].Task != ready[2].Task {
		t.Fatalf("QA limit blocked independent work or over-issued: %+v", issued)
	}
	delete(state.PendingActions, "active-qa")
	issued, err = engine.selectIssued(state, plan, decision.Admission{Capacity: 3})
	if err != nil || len(issued) != 3 || issued[0].Task != ready[0].Task {
		t.Fatalf("released QA capacity did not resume original order: %+v %v", issued, err)
	}
	if len(plan.Next.Ready.Tasks) != 3 {
		t.Fatal("admission changed canonical plan")
	}
}
