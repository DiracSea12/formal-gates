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

func TestConfigurationNodeStatesPreviewsCandidateWithoutWriting(t *testing.T) {
	e, dir := readyReviewEngine(t)
	library := bindLibrary(t, e, settings.DefaultConfiguration())

	missing, err := New(mustStore(t, t.TempDir()), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.ConfigurationNodeStates(settings.DefaultConfiguration()); err == nil || rejectionCode(t, err) != CodeNotInitialized {
		t.Fatalf("a store without state did not report the not-initialized rejection: %v", err)
	}

	var before []byte
	var revision uint64
	var decisions, applied int
	snapshot := func() {
		before = stateBytesOf(t, dir)
		snap := e.LoadMustSucceed(t)
		revision, decisions, applied = snap.Revision, len(snap.State.Decisions), len(snap.State.AppliedConfigurations)
	}
	unchanged := func() {
		snap := e.LoadMustSucceed(t)
		if !reflect.DeepEqual(before, stateBytesOf(t, dir)) || snap.Revision != revision || len(snap.State.Decisions) != decisions || len(snap.State.AppliedConfigurations) != applied {
			t.Fatal("candidate projection changed persisted state, revision, decisions or applied history")
		}
	}

	snapshot()
	nodes, err := e.ConfigurationNodeStates(settings.Configuration{
		QAModes:       []string{},
		FlowSelection: &FlowSelection{Disabled: []string{"product-review"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if review := executionNodeState(t, nodes, "product-review"); !review.Owed || review.Enabled || review.Completed || review.Dispatchable || review.Running || !review.Editable {
		t.Fatal("a candidate-disabled node lost or closed its owed obligation", review)
	}
	if blackbox := executionNodeState(t, nodes, "qa-blackbox"); blackbox.Enabled || blackbox.Completed || blackbox.Owed || !blackbox.Editable {
		t.Fatal("a disabled node was projected as a completed verdict instead of staying editable", blackbox)
	}
	unchanged()

	action := issueReady(t, e)[0]
	e.cfg.WorkerExecution = func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
		if p.ActionID != action {
			t.Fatal("claim lookup did not bind the original action")
		}
		return &SpawnReceiptPayload{ActionID: action, Provider: testProvider}, nil
	}
	library.value.FlowSelection = &FlowSelection{Disabled: []string{"product-review"}}
	reconcile(t, e)

	snapshot()
	candidate := settings.Configuration{
		QAModes:       []string{},
		FlowSelection: &FlowSelection{Disabled: []string{"start-readiness"}},
	}
	preview := func(engine *Engine) {
		nodes, err := engine.ConfigurationNodeStates(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if review := executionNodeState(t, nodes, "product-review"); !review.Running || !review.Closing || !review.Owed || review.Editable || review.Completed {
			t.Fatal("a claimed node lost its running close or outstanding duty under the candidate", review)
		}
		if readiness := executionNodeState(t, nodes, "start-readiness"); readiness.Enabled {
			t.Fatal("a candidate-disabled node stayed enabled", readiness)
		}
	}
	preview(e)
	reopened, err := New(mustStore(t, dir), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	preview(reopened)
	unchanged()
}
