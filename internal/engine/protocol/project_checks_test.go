package protocol

import (
	"bytes"
	"encoding/json"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"reflect"
	"strings"
	"testing"
)

func ownedDevelopmentPlan() *GranularityReview {
	plan := plannedReviewEvidence(review.StartReadiness, "ready", "", "PASS", "complete", nil).GranularityReview
	first := plan.Batches[0]
	plan.Batches = nil
	for _, owner := range []string{"core", "tooling", masterMergeOwner} {
		batch := first
		batch.ID, batch.Owner = owner, owner
		batch.Subtasks = []DevelopmentSubtask{{ID: owner, Description: "Deliver the confirmed responsibility of " + owner}}
		batch.Checks = []DevelopmentCheck{{ID: "build", Purpose: CheckFullBuild, Command: "build " + owner}, {ID: "test", Purpose: CheckFullTests, Command: "test " + owner}}
		plan.Batches = append(plan.Batches, batch)
	}
	return plan
}

func TestSplitPlansPreserveExplicitOwnersAndCompleteChecks(t *testing.T) {
	plan := ownedDevelopmentPlan()
	state := State{RouteMode: routeModeMerge, Topology: &TopologyBinding{Decision: topologySplit, Slices: []string{"core", "tooling"}}, Results: map[string]WorkerResult{"accepted": {Review: &ReviewResultEvidence{GranularityReview: plan}}, "unconfirmed": {Review: &ReviewResultEvidence{GranularityReview: ownedDevelopmentPlan()}}}, Decisions: map[string]RecordedDecision{"split": {Control: ControlConfirmTopology, Review: &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: topologySplit, DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "accepted"}}}}}}
	state.Results["unconfirmed"].Review.GranularityReview.Batches[0].Checks[1].Command = "unconfirmed command"
	if err := state.validateSplitPlan(plan); err != nil {
		t.Fatal(err)
	}
	projection, err := state.SliceDevelopmentPlan("core")
	if err != nil || len(projection.Batches) != 1 || !reflect.DeepEqual(projection.Batches[0], plan.Batches[0]) {
		t.Fatal("slice projection did not preserve the confirmed creator's exact contract", projection, err)
	}
	checks := state.fullProjectChecks()
	if len(checks) != 2 || checks[0].Command != plan.Batches[2].Checks[0].Command {
		t.Fatal("master complete verification included child-purpose commands")
	}
	state.RouteMode, state.Topology = "full", &TopologyBinding{Decision: "no-split"}
	if len(state.fullProjectChecks()) != 6 {
		t.Fatal("unsplit project lost approved commands because a proposal carried owners")
	}
}

func TestSplitPlansRejectMissingOrCrossOwnerContracts(t *testing.T) {
	for _, input := range []struct {
		name   string
		change func(*GranularityReview)
	}{
		{"missing-owner", func(p *GranularityReview) { p.Batches[0].Owner = "" }},
		{"unknown-owner", func(p *GranularityReview) { p.Batches[0].Owner = "other" }},
		{"missing-slice-tests", func(p *GranularityReview) { p.Batches[0].Checks[1].Purpose = "" }},
		{"missing-master-tests", func(p *GranularityReview) { p.Batches[2].Checks[1].Purpose = "" }},
		{"cross-owner-dependency", func(p *GranularityReview) { p.Batches[1].DependsOn = []string{p.Batches[0].ID} }},
		{"insufficient-checkpoint-groups", func(p *GranularityReview) {
			p.VerificationTier, p.CheckpointReason = "CHECKPOINT", "independent deliveries"
			p.CheckpointGroups = [][]string{{"core", "tooling", masterMergeOwner}}
		}},
		{"omitted-checkpoint-batch", func(p *GranularityReview) {
			p.VerificationTier, p.CheckpointReason = "CHECKPOINT", "independent deliveries"
			p.CheckpointGroups = [][]string{{"core"}, {"tooling"}}
		}},
		{"repeated-checkpoint-batch", func(p *GranularityReview) {
			p.VerificationTier, p.CheckpointReason = "CHECKPOINT", "independent deliveries"
			p.CheckpointGroups = [][]string{{"core", "tooling"}, {"core", masterMergeOwner}}
		}},
	} {
		t.Run(input.name, func(t *testing.T) {
			plan := ownedDevelopmentPlan()
			input.change(plan)
			state := State{Topology: &TopologyBinding{Decision: topologySplit, Slices: []string{"core", "tooling"}}}
			if err := state.validateSplitPlan(plan); err == nil {
				t.Fatal("incomplete owner contract accepted for split confirmation")
			}
		})
	}
}

func TestProjectVerificationPurposesRequireApprovedMeaning(t *testing.T) {
	base := plannedReviewEvidence(review.StartReadiness, "readiness", "", "PASS", "complete verification", nil).GranularityReview
	for _, item := range []struct {
		name     string
		purposes []string
		missing  string
	}{
		{"explicit", []string{CheckFullBuild, CheckFullTests}, ""},
		{"unmarked-command-text", []string{"", ""}, CheckFullBuild},
		{"build-only", []string{CheckFullBuild, ""}, CheckFullTests},
		{"tests-only", []string{"", CheckFullTests}, CheckFullBuild},
	} {
		t.Run(item.name, func(t *testing.T) {
			p := *base
			b := base.Batches[0]
			b.Checks = append([]DevelopmentCheck(nil), b.Checks...)
			p.Batches = []DevelopmentBatch{b}
			for i, purpose := range item.purposes {
				p.Batches[0].Checks[i].Purpose = purpose
			}
			err := ValidateProjectChecks(&p)
			if item.missing == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), item.missing) {
				t.Fatalf("missing exact purpose %s: %v", item.missing, err)
			}
			if p.Batches[0].Checks[0].Command != base.Batches[0].Checks[0].Command || p.Batches[0].Checks[1].Command != base.Batches[0].Checks[1].Command {
				t.Fatal("purpose validation changed approved command bytes")
			}
		})
	}
	p := *base
	b := base.Batches[0]
	b.Checks = append([]DevelopmentCheck(nil), b.Checks...)
	b.Checks[0].Purpose = "LANGUAGE_GUESS"
	p.Batches = []DevelopmentBatch{b}
	if ValidateGranularity(&p) == nil {
		t.Fatal("unknown command purpose admitted")
	}
}

func TestProjectChecksReuseExactApprovedPurposes(t *testing.T) {
	p := plannedReviewEvidence(review.StartReadiness, "ready", "", "PASS", "complete", nil).GranularityReview
	first := p.Batches[0]
	second := first
	second.ID = "second"
	second.Checks = append([]DevelopmentCheck(nil), first.Checks...)
	second.Checks = append(second.Checks, DevelopmentCheck{ID: "distinct", Purpose: CheckFullTests, Command: "printf 'distinct tests'"}, DevelopmentCheck{ID: "distinct-purpose", Purpose: CheckFullTests, Command: first.Checks[0].Command})
	p.Batches = []DevelopmentBatch{first, second}
	s := State{Results: map[string]WorkerResult{"ready": {Review: &ReviewResultEvidence{GranularityReview: p}}}, Decisions: map[string]RecordedDecision{"route": {Control: ControlSelectRoute, Review: &ReviewDecisionPayload{Route: &RouteDecision{DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "ready"}}}}}}
	checks := s.fullProjectChecks()
	if len(checks) != 4 || checks[0].Command != first.Checks[0].Command || checks[1].Command != first.Checks[1].Command || checks[2].Command != "printf 'distinct tests'" || checks[3].Purpose != CheckFullTests || checks[3].Command != first.Checks[0].Command {
		t.Fatalf("approved purpose/order changed: %+v", checks)
	}
	r := WorkerResult{Validation: &ValidationResult{Binding: WorkBinding{Task: runtime.TaskKey{Step: "development.repair"}}, GranularityReview: &GranularityReview{Batches: []DevelopmentBatch{first}}}}
	combined := s.editChecks(r)
	if len(combined) != 4 || combined[0] != first.Checks[0] || combined[1] != first.Checks[1] {
		t.Fatalf("batch responsibility was changed or repeated: %+v", combined)
	}
}

func TestSplitMasterHasNoChildCheckpointTasks(t *testing.T) {
	p := plannedReviewEvidence(review.StartReadiness, "ready", "", "PASS", "complete", nil).GranularityReview
	first := p.Batches[0]
	second := first
	second.ID, second.Artifacts = "second", []string{"second.go"}
	second.Subtasks = []DevelopmentSubtask{{ID: "second-body", Description: "Implement the second function contract"}}
	second.DoD = []DevelopmentCriterion{{ID: "second-outcome", Description: "Second function matches its contract"}}
	p.Batches = []DevelopmentBatch{first, second}
	p.VerificationTier = "CHECKPOINT"
	p.CheckpointGroups = [][]string{{first.ID}, {second.ID}}
	p.CheckpointReason = "independent functions have separate acceptance boundaries"
	if err := ValidateGranularity(p); err != nil {
		t.Fatal(err)
	}
	s := State{RouteMode: routeModeMerge, Topology: &TopologyBinding{Decision: topologySplit, SplitCount: 2, Slices: []string{"first", "second"}}, Results: map[string]WorkerResult{"ready": {Review: &ReviewResultEvidence{GranularityReview: p}}}, Decisions: map[string]RecordedDecision{"split": {Control: ControlConfirmTopology, Review: &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: topologySplit, DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "ready"}}}}}, Validation: &Validation{Waves: []ValidationWave{{ID: "merge", Status: "COMPLETE", Scope: coverage.ScopeFull, Candidate: coverage.ValidationCandidate{Identity: "integrated", Digest: "tree"}}}}}
	if len(s.fullProjectChecks()) != 2 || len(s.planTasks(compiledCanonicalWorkflow(t))) != 0 {
		t.Fatal("master lost full project commands or took child development tasks")
	}
	tasks, checkpoint := s.planCheckpoint(compiledCanonicalWorkflow(t), p)
	if checkpoint || len(tasks) != 0 {
		t.Fatalf("master received child checkpoint tasks: %v %v", tasks, checkpoint)
	}
	w := &s.Validation.Waves[0]
	w.CheckpointTasks = tasks
	w.Counted = s.validationWaveCountable(w)
	if !w.Counted || s.completedValidationWaves() != 1 {
		t.Fatal("first integrated validation wave was omitted from repair authorization accounting")
	}
}

func TestSliceCheckpointTierFollowsItsFilteredGroups(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		t.Run(map[bool]string{false: "single-group", true: "multiple-groups"}[multiple], func(t *testing.T) {
			plan := ownedDevelopmentPlan()
			plan.VerificationTier = "CHECKPOINT"
			plan.CheckpointReason = "Confirm independent child boundaries before master integration"
			plan.CheckpointGroups = [][]string{{"core", "tooling"}, {masterMergeOwner}}
			if multiple {
				next := plan.Batches[0]
				next.ID = "core-consumer"
				next.DependsOn = []string{"core"}
				next.Subtasks = []DevelopmentSubtask{{ID: "consumer", Description: "consume the confirmed core interface"}}
				plan.Batches = append(plan.Batches, next)
				plan.CheckpointGroups = [][]string{{"core", "tooling"}, {"core-consumer", masterMergeOwner}}
			}
			if err := ValidateGranularity(plan); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(plan)
			state := &State{Topology: &TopologyBinding{Decision: topologySplit, Slices: []string{"core", "tooling"}}}
			child, err := state.sliceDevelopmentPlan(plan, "core")
			if err != nil {
				t.Fatal(err)
			}
			if multiple {
				if child.VerificationTier != "CHECKPOINT" || child.CheckpointReason != plan.CheckpointReason || !reflect.DeepEqual(child.CheckpointGroups, [][]string{{"core"}, {"core-consumer"}}) {
					t.Fatal("multi-group child lost original boundaries")
				}
			} else if child.VerificationTier != "FINAL" || child.CheckpointGroups != nil || child.CheckpointReason != "" {
				t.Fatal("single child group retained inapplicable checkpoints")
			}
			for i, batch := range child.Batches {
				original := plan.Batches[0]
				if i == 1 {
					original = plan.Batches[3]
				}
				if !reflect.DeepEqual(batch, original) {
					t.Fatal("projection changed owner, dependency, interface, DoD or FULL checks")
				}
			}
			other, err := state.sliceDevelopmentPlan(plan, "tooling")
			if err != nil || other.VerificationTier != "FINAL" {
				t.Fatal("mixed master groups did not derive child final", err)
			}
			after, _ := json.Marshal(plan)
			if !bytes.Equal(before, after) {
				t.Fatal("projection modified the original master proposal")
			}
			empty := *plan
			empty.Batches = []DevelopmentBatch{plan.Batches[2]}
			if _, err := state.sliceDevelopmentPlan(&empty, "core"); err == nil {
				t.Fatal("empty filtered slice acquired a valid plan")
			}
		})
	}
}

func TestSplitMasterMissingCallsUseConfirmedTaskIdentity(t *testing.T) {
	cd := compiledCanonicalWorkflow(t)
	plan := ownedDevelopmentPlan()
	existing := plan.Batches[2]
	missing := existing
	missing.ID = "missing-integration-calls"
	missing.DependsOn = []string{existing.ID}
	missing.Checks = []DevelopmentCheck{{ID: "build", Purpose: CheckFullBuild, Goal: "Build the integrated project"}, {ID: "tests", Purpose: CheckFullTests, Goal: "Test all integrated behavior"}}
	plan.Batches = append(plan.Batches, missing)
	s := State{RouteMode: routeModeMerge, Topology: &TopologyBinding{Decision: topologySplit, Slices: []string{"core", "tooling"}}, Results: map[string]WorkerResult{"ready": {Review: &ReviewResultEvidence{GranularityReview: plan}}}, Decisions: map[string]RecordedDecision{"split": {Control: ControlConfirmTopology, Review: &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: topologySplit, DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "ready"}}}}}}
	tasks := s.planTasks(cd)
	if len(tasks) != 1 || tasks[0] != planTask(cd, plan, missing.ID) {
		t.Fatalf("master invented a plan identity or dispatched an existing call: %+v", tasks)
	}
	_, projected, batch, ok := s.developmentBatch(tasks[0], cd)
	if !ok || projected != plan || !reflect.DeepEqual(batch, missing) {
		t.Fatal("master lost the confirmed plan's ownership")
	}
	if s.developmentBatchRequired(existing) || s.developmentBatchRequired(plan.Batches[0]) {
		t.Fatal("existing or child calls caused extra master development")
	}
}
