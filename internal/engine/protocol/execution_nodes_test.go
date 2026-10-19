package protocol

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

func executionNodeState(t *testing.T, nodes []ExecutionNodeState, id string) ExecutionNodeState {
	t.Helper()
	for _, node := range nodes {
		if node.Node == id {
			return node
		}
	}
	t.Fatal("missing execution node", id)
	return ExecutionNodeState{}
}

func TestMergeValidationNodeProjectionUsesSavedQAModeAndFrozenCompletion(t *testing.T) {
	cd := compiledCanonicalWorkflow(t)
	view, err := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"PENDING", "PASS"} {
		t.Run(status, func(t *testing.T) {
			state := NewState(*view)
			state.RunID, state.RouteConfirmed = "master", true
			state.Topology = &TopologyBinding{Decision: topologySplit}
			state.SelectedGates = mergeRouteGates()
			task := pipelineTask(cd, "engine.qa.execution.v4", "wave:"+string(coverage.ReviewMerge))
			state.Validation = &Validation{Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewMerge: {Status: "FROZEN"}}, Waves: []ValidationWave{{ID: "wave", Status: "COLLECTING", Targets: map[string]*ValidationTarget{
				task.String(): {Task: task, Kind: "QA", Mode: coverage.ReviewMerge, Status: status},
				"merge-gate":  {Kind: "GATE", Gate: mergeGateID, Status: "PASS"},
			}}}}
			state.AppliedConfigurations = []AppliedConfiguration{{Configuration: settings.Configuration{QAModes: []string{"blackbox"}}}}
			saved := settings.Configuration{QAModes: []string{"whitebox"}}
			e := &Engine{cfg: Config{Definition: cd}}
			nodes, err := e.executionNodeStatesFor(state, saved)
			if err != nil {
				t.Fatal(err)
			}
			white := executionNodeState(t, nodes, "qa-whitebox")
			if status == "PENDING" {
				if !white.Owed || !white.Dispatchable || !white.Editable || white.Completed {
					t.Fatal("saved merge QA lost its pending duty", white)
				}
			} else if !white.Completed || white.Owed || white.Editable || white.Dispatchable {
				t.Fatal("completed merge QA invented another obligation", white)
			}
			if gate := executionNodeState(t, nodes, "gate"); gate.Owed || !gate.Completed || gate.Editable {
				t.Fatal("passing merge gate was reopened", gate)
			}
			if development := executionNodeState(t, nodes, "development"); development.Owed || !development.Completed {
				t.Fatal("mainline validation invented development work", development)
			}
			if !reflect.DeepEqual(state.AppliedConfiguration().QAModes, []string{"blackbox"}) || state.Validation.Waves[0].Targets[task.String()].Status != status {
				t.Fatal("projection mutated the applied configuration or frozen result")
			}
		})
	}
}

func TestRepairNodeProjectionRetainsWorkAndFutureValidationDuties(t *testing.T) {
	e, state, pending := unfinishedDevelopmentState(t, "engine.repair.v7")
	state.SelectedGates = []string{"quality"}
	state.AppliedConfigurations = []AppliedConfiguration{{Configuration: settings.Configuration{QAModes: []string{}}}}
	e.cfg.RouteGates = func() ([]string, error) { return []string{"blackbox", "whitebox", "quality"}, nil }
	gates := []string{}
	saved := settings.Configuration{Gates: &gates, QAModes: []string{"whitebox"}}
	before := state.Attempts[pending.Task]
	nodes, err := e.executionNodeStatesFor(state, saved)
	if err != nil {
		t.Fatal(err)
	}
	repair := executionNodeState(t, nodes, "repair")
	white := executionNodeState(t, nodes, "qa-whitebox")
	if !repair.Owed || !repair.Running || repair.Editable || !white.Owed || !white.Dispatchable || white.Completed {
		t.Fatal("repair or its future validation was lost", repair, white)
	}
	saved.FlowSelection = &FlowSelection{Disabled: []string{"development", "qa", "gate", "repair", "product-review", "start-readiness"}}
	nodes, err = e.executionNodeStatesFor(state, saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.Dispatchable {
			t.Fatal("closed duties bypassed the unavailable guard", node)
		}
	}
	if !executionNodeState(t, nodes, "repair").Owed || !reflect.DeepEqual(before, state.Attempts[pending.Task]) {
		t.Fatal("closure erased or rewrote the actual repair duty")
	}
}
