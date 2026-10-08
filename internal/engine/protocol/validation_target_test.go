package protocol

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
	"testing"
)

// Projection preserves the original task; equality of candidate identity alone
// cannot retain a replaced obligation or authorize a changed requirement.
func TestProjectedTargetRequiresExactFrozenDuty(t *testing.T) {
	for _, name := range []string{"unchanged", "selected-task-replaced", "candidate-digest", "scope", "case-version", "revision-without-carry", "current-revision-without-carry", "generation-without-carry", "confirmed-carry", "confirmed-redo"} {
		t.Run(name, func(t *testing.T) {
			task := runtime.TaskKey{Node: "validation", Step: "qa.execution", Scope: "original:whitebox"}
			originalTarget := &ValidationTarget{Task: task, Kind: "QA", Mode: coverage.ReviewWhitebox, Status: "PENDING"}
			projectedTarget := *originalTarget
			candidate := coverage.ValidationCandidate{Identity: "immutable-candidate", Digest: "immutable-tree"}
			original := ValidationWave{ID: "original", Candidate: candidate, Scope: coverage.ScopeFull, Versions: map[coverage.ReviewKind]string{coverage.ReviewWhitebox: "cases"}, Targets: map[string]*ValidationTarget{task.String(): originalTarget}, Status: "COLLECTING"}
			projected := ValidationWave{ID: "projected", Candidate: candidate, Scope: coverage.ScopeFull, Versions: map[coverage.ReviewKind]string{coverage.ReviewWhitebox: "cases"}, Targets: map[string]*ValidationTarget{task.String(): &projectedTarget}, Status: "COLLECTING"}
			state := &State{Validation: &Validation{RequirementRevision: "requirement", Generation: 1, Waves: []ValidationWave{original, projected}}}
			binding := WorkBinding{Task: task, WaveID: original.ID, Candidate: candidate.Identity, CaseVersion: "cases", RequirementRevision: "requirement", Generation: 1}
			current := &state.Validation.Waves[1]
			switch name {
			case "selected-task-replaced":
				delete(current.Targets, task.String())
				fresh := projectedTarget
				fresh.Task.Scope = "fresh:whitebox"
				current.Targets[fresh.Task.String()] = &fresh
			case "candidate-digest":
				current.Candidate.Digest = "different-tree"
			case "scope":
				current.Scope = coverage.ScopeAffected
			case "case-version":
				current.Versions[coverage.ReviewWhitebox] = "different-cases"
			case "revision-without-carry":
				state.Validation.RequirementRevision = "new-requirement"
			case "current-revision-without-carry":
				state.Validation.Waves = state.Validation.Waves[:1]
				state.Validation.RequirementRevision = "new-requirement"
			case "generation-without-carry":
				state.Validation.Generation++
			case "confirmed-carry", "confirmed-redo":
				state.Validation.RequirementRevision = "new-requirement"
				state.Validation.Generation++
				disposition := RequirementWorkCarry
				if name == "confirmed-redo" {
					disposition = RequirementWorkRedo
				}
				state.RequirementRevisions = []ConfirmedRevision{{WorkDecisions: []RequirementWorkDecision{{Work: RequirementWork{Task: task}, Disposition: disposition}}}}
			}
			actual := state.validationTargetCurrent(binding)
			allowed := name == "unchanged" || name == "confirmed-carry"
			if allowed && actual != &projectedTarget || !allowed && actual != nil {
				t.Fatalf("projected duty applicability=%v, want allowed=%t", actual, allowed)
			}
		})
	}
}
