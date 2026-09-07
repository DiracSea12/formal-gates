package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/lifecycle"
)

func generalPlanFixture(count int) DevelopmentPlanInput {
	input := DevelopmentPlanInput{Reason: "coherent delivery units with explicit handoff"}
	for i := 0; i < count; i++ {
		id, subtask := fmt.Sprintf("unit-%d", i+1), fmt.Sprintf("step-%d", i+1)
		input.Subtasks = append(input.Subtasks, DevelopmentSubtask{ID: subtask, Task: "implement " + subtask})
		batch := DevelopmentBatchInput{ID: id, Task: "deliver " + id, SubtaskIDs: []string{subtask}, Interfaces: "public parser contract", StateBoundary: "owns parsed input", Risks: "invalid input", DoD: "valid input works; malformed input rejected", Validation: []string{"go test ./..."}, Recovery: "revert this unit commit", Handoff: "decisions and next task", ContextCost: "one parser per worker"}
		if i > 0 {
			batch.DependsOn = []string{input.Batches[i-1].ID}
		}
		input.Batches = append(input.Batches, batch)
	}
	return input
}

func bindGeneralPlan(t *testing.T, root, pkg string, state RunState, input DevelopmentPlanInput) RunState {
	t.Helper()
	state, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", input.Reason, "", SlicingAmendOptions{UserConfirm: true, DevelopmentPlan: &input})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestGeneralDevelopmentPlanPublicLifecycle(t *testing.T) {
	for _, count := range []int{1, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			root, pkg := workflowFixture(t)
			state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "general-batches"))
			state = recordProductReview(t, root, pkg, state)
			state = recordReadiness(t, root, pkg, state)
			input := generalPlanFixture(count)
			state = bindGeneralPlan(t, root, pkg, state, input)
			state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
			stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "")
			keys := map[string]bool{}
			for i, batch := range input.Batches {
				prompt, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, "")
				if err != nil {
					t.Fatal(err)
				}
				duplicate, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, "")
				if err != nil || duplicate != prompt {
					t.Fatalf("duplicate preparation: %v", err)
				}
				for _, text := range []string{batch.ID, batch.SubtaskIDs[0], batch.Interfaces, batch.StateBoundary, batch.Recovery, batch.ContextCost, input.Subtasks[i].Task} {
					if !strings.Contains(prompt, text) {
						t.Fatalf("prompt missing %q", text)
					}
				}
				if i > 0 && !strings.Contains(prompt, "delivered decisions") {
					t.Fatal("missing predecessor handoff")
				}
				state, _ = LoadRunState(root, state.RunID)
				id := openDispatchID(state, "action", "development-worker")
				dispatch := state.Dispatches[id]
				if dispatch.BatchID != batch.ID || dispatch.TaskKey == "" || keys[dispatch.TaskKey] {
					t.Fatalf("bad batch TaskKey binding: %+v", dispatch)
				}
				keys[dispatch.TaskKey] = true
				if _, err := ClaimDispatch(root, pkg, state.RunID, id, "worker-"+batch.ID); err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, filepath.Join(root, batch.ID+".txt"), batch.ID)
				commitAll(t, root, "deliver "+batch.ID)
				if _, err := AdvanceDevelopmentBatch(root, pkg, state.RunID, id, batch.ID, "", "handoff"); err == nil {
					t.Fatal("accepted missing verification")
				}
				if _, err := AdvanceDevelopmentBatch(root, pkg, state.RunID, id, "wrong-batch", "checks", "handoff"); err == nil {
					t.Fatal("accepted wrong batch")
				}
				if _, err := AdvanceSnapshot(root, pkg, state.RunID, id, false, ""); err == nil {
					t.Fatal("accepted final snapshot without batch evidence")
				}
				state, err = AdvanceDevelopmentBatch(root, pkg, state.RunID, id, batch.ID, "declared checks passed", "delivered decisions")
				if err != nil {
					t.Fatal(err)
				}
				if i < count-1 {
					if state.Actions["development-worker"].Status != developmentPending || len(state.ReviewWaves) != 0 {
						t.Fatal("intermediate batch opened review")
					}
					if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err == nil {
						t.Fatal("gate opened before all batches completed")
					}
				}
				if count > 1 && i == 0 {
					result, err := ResetRun(root, pkg, state.RunID)
					if err != nil {
						t.Fatal(err)
					}
					if result.State.Dispatches[id].Status != "COMPLETED" {
						t.Fatal("reset lost completed work")
					}
					state = confirmRequirement(t, root, pkg, result.State)
					state = recordProductReview(t, root, pkg, state)
					state = recordReadiness(t, root, pkg, state)
				}
			}
			var err error
			state, err = PrepareCandidate(root, pkg, state.RunID, "general batch candidate")
			if err != nil {
				t.Fatal(err)
			}
			state = recordGateResult(t, root, pkg, state, "quality", "general-quality-reviewer", "PASS", "", nil)
			if _, err := Seal(root, pkg, state.RunID, nil, false, "general batch delivery"); err != nil {
				t.Fatal(err)
			}
			summary, err := LoadRunSummary(root, state.RunID)
			if err != nil {
				t.Fatal(err)
			}
			for i, batch := range summary.DevelopmentBatches {
				if batch.Status != "COMPLETE" || batch.Verification != "declared checks passed" || batch.Handoff != "delivered decisions" || batch.TaskKey == "" || !reflect.DeepEqual(batch.Subtasks, []DevelopmentSubtask{input.Subtasks[i]}) || batch.Recovery == "" {
					t.Fatalf("incomplete summary evidence: %+v", batch)
				}
			}
			if summary.DevelopmentPlan == nil || summary.DevelopmentPlan.Digest != state.DevelopmentBatchPlan.Digest || summary.DevelopmentPlan.TechnicalReviewDispatch == "" {
				t.Fatal("Seal lost confirmed plan provenance")
			}
		})
	}
}

func TestGeneralDevelopmentPlanRejectsInvalidContracts(t *testing.T) {
	tests := []struct {
		name   string
		change func(*DevelopmentPlanInput)
	}{
		{"reason", func(p *DevelopmentPlanInput) { p.Reason = "" }},
		{"empty", func(p *DevelopmentPlanInput) { p.Batches = nil }},
		{"duplicate batch", func(p *DevelopmentPlanInput) { p.Batches[1].ID = p.Batches[0].ID }},
		{"duplicate subtask", func(p *DevelopmentPlanInput) { p.Subtasks = append(p.Subtasks, p.Subtasks[0]) }},
		{"unmapped", func(p *DevelopmentPlanInput) {
			p.Subtasks = append(p.Subtasks, DevelopmentSubtask{ID: "extra", Task: "forgotten"})
		}},
		{"duplicate ownership", func(p *DevelopmentPlanInput) { p.Batches[1].SubtaskIDs = p.Batches[0].SubtaskIDs }},
		{"unknown subtask", func(p *DevelopmentPlanInput) { p.Batches[0].SubtaskIDs = []string{"absent"} }},
		{"cycle", func(p *DevelopmentPlanInput) { p.Batches[0].DependsOn = []string{p.Batches[1].ID} }},
		{"unknown dependency", func(p *DevelopmentPlanInput) { p.Batches[1].DependsOn = []string{"absent"} }},
		{"dod", func(p *DevelopmentPlanInput) { p.Batches[0].DoD = "" }},
		{"recovery", func(p *DevelopmentPlanInput) { p.Batches[0].Recovery = "" }},
		{"validation", func(p *DevelopmentPlanInput) { p.Batches[0].Validation = nil }},
		{"handoff", func(p *DevelopmentPlanInput) { p.Batches[0].Handoff = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := generalPlanFixture(2)
			test.change(&input)
			if _, err := validateDevelopmentPlan(input); err == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
	input := generalPlanFixture(1)
	data, _ := json.Marshal(input)
	for _, suffix := range []string{`,"status":"COMPLETE"}`, `} {}`} {
		path := filepath.Join(t.TempDir(), "plan.json")
		os.WriteFile(path, append(data[:len(data)-1], []byte(suffix)...), 0600)
		if _, err := ReadDevelopmentPlan(path); err == nil {
			t.Fatal("unknown progress field or trailing object accepted")
		}
	}
}

func TestGeneralDevelopmentPlanRegistrationAndReplacement(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "plan-replacement"))
	input := generalPlanFixture(2)
	call := func(confirmed bool) error {
		_, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", input.Reason, "", SlicingAmendOptions{UserConfirm: confirmed, DevelopmentPlan: &input})
		return err
	}
	if err := call(true); err == nil {
		t.Fatal("registered without reviews")
	}
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	if err := call(false); err == nil {
		t.Fatal("registered without user confirmation")
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.Slicing != nil || state.DevelopmentBatchPlan != nil {
		t.Fatal("failed registration partially changed state")
	}
	state = bindGeneralPlan(t, root, pkg, state, input)
	if err := call(true); err == nil {
		t.Fatal("silently replaced bound plan")
	}
	state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "")
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "first-unit.txt"), "first unit")
	commitAll(t, root, "first unit before changed plan")
	var err error
	state, err = AdvanceDevelopmentBatch(root, pkg, state.RunID, dispatch, input.Batches[0].ID, "checks passed", "decisions")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "requirements.md"), testRequirementDocument("Changed batch contract."))
	commitAll(t, root, "changed plan requirement")
	state, err = UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil, repairImpactOptions(t, root, pkg, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if err := call(true); err == nil {
		t.Fatal("replacement skipped reconfirmation/review")
	}
	state = confirmRequirement(t, root, pkg, state)
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	input.Batches[0].DoD = "updated definition of done"
	state = bindGeneralPlan(t, root, pkg, state, input)
	if state.RouteMode != "custom" || state.DevelopmentBatchPlan.Batches[0].DoD != input.Batches[0].DoD {
		t.Fatal("replacement lost route or new contract")
	}
	if _, next, ok := activeDevelopmentBatch(state); !ok || next.ID != input.Batches[0].ID {
		t.Fatal("old completion counted toward replacement contract")
	}
}

func TestGeneralDevelopmentPlanFinalBatchRetainsBlackboxGate(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "general-qa"), "custom", []string{blackboxQAID, "quality"})
	input := generalPlanFixture(2)
	state = bindGeneralPlan(t, root, pkg, state, input)
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "")
	for i, batch := range input.Batches {
		dispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
		writeTestFile(t, filepath.Join(root, batch.ID+".txt"), batch.ID)
		commitAll(t, root, batch.ID)
		updated, err := AdvanceDevelopmentBatch(root, pkg, state.RunID, dispatch, batch.ID, "verified", "handoff")
		if i == 0 {
			if err != nil {
				t.Fatal(err)
			}
			state = updated
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "blackbox QA Review must pass") {
			t.Fatalf("final batch bypassed QA: %v", err)
		}
		state, err = AdvanceDevelopmentBatch(root, pkg, state.RunID, dispatch, batch.ID, "verified", "handoff", DevelopmentBatchSnapshotOptions{UserRequested: true, Reason: "explicit fixture authorization"})
		if err != nil {
			t.Fatal(err)
		}
		if state.SnapshotOverride == nil || blackboxReviewPassed(state) || state.Dispatches[dispatch].Verification != "verified" {
			t.Fatal("final override lost evidence or fabricated QA PASS")
		}
	}
}

func TestGeneralDevelopmentPlanSliceInheritsReviewBinding(t *testing.T) {
	root, pkg := workflowFixture(t)
	master := sliceMaster(t, root, pkg, "batch-master")
	slice := confirmRequirement(t, root, pkg, mustStartSlice(t, root, pkg, "batch-slice", master))
	input := generalPlanFixture(1)
	state, err := RecordSlicing(root, pkg, slice.RunID, "split", 2, []string{"slice-a", "slice-b"}, "slice-a and slice-b can run in parallel", "", master, SlicingAmendOptions{UserConfirm: true, DevelopmentPlan: &input})
	if err != nil {
		t.Fatal(err)
	}
	if state.DevelopmentBatchPlan.ReviewMasterRunID != master || state.DevelopmentBatchPlan.TechnicalReviewDispatch == "" {
		t.Fatal("slice lost inherited technical review source")
	}
	state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatal(err)
	}
}
