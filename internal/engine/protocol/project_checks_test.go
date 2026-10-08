package protocol

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"strings"
	"testing"
)

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
