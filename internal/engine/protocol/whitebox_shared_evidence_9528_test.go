package protocol

import (
	"encoding/json"
	"testing"

	"formal-gates/internal/engine/coverage"
)

// AC-004: changing one shared preparation must not demand fresh execution
// evidence from a case using a different, unchanged preparation.
func TestWhiteboxIndependentSharedEvidence9528(t *testing.T) {
	prior := QADesign{Manifest: coverage.AcceptanceManifest{
		SharedSteps: []coverage.SharedStep{
			{ID: "setup-a", Procedure: "prepare A", Command: &coverage.Command{Executable: "sh", Args: []string{"-c", "printf old-a"}}},
			{ID: "setup-b", Procedure: "prepare B", Command: &coverage.Command{Executable: "sh", Args: []string{"-c", "printf stable-b"}}},
		},
		Cases: []coverage.AcceptanceCase{
			{CaseID: "WHITEBOX:C1", SharedSteps: []string{"setup-a"}, Oracle: "A output"},
			{CaseID: "WHITEBOX:C2", SharedSteps: []string{"setup-b"}, Oracle: "B output"},
		},
	}}
	raw, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	var changed QADesign
	if err := json.Unmarshal(raw, &changed); err != nil {
		t.Fatal(err)
	}
	changed.Manifest.SharedSteps[0].Command.Args[1] = "printf new-a"
	t.Run("affected-case-still-needs-evidence", func(t *testing.T) {
		if err := validateQAReuseIncrement(&prior, changed, nil, nil); err == nil {
			t.Fatal("changed setup-a accepted without fresh C1 evidence")
		}
	})
	t.Run("unrelated-shared-case-retains-evidence", func(t *testing.T) {
		if err := validateQAReuseIncrement(&prior, changed, []TestReuse{{CaseID: "WHITEBOX:C1"}}, nil); err != nil {
			t.Fatalf("AC-004: unchanged C2/setup-b must retain evidence when only setup-a changes: %v", err)
		}
	})
	if string(raw) != func() string { b, _ := json.Marshal(prior); return string(b) }() {
		t.Fatal("validation mutated prior design")
	}
}
