package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
)

func TestQADraftIncrementRetainsAndRejectsReplay(t *testing.T) {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	s, _ := e.Load()
	r := s.State.Regular
	draft := testDesign(r, "BLACKBOX")
	draft.Manifest.Cases[0].CaseID = "new-output"
	draft.Manifest.Edges[0].CaseID = "new-output"
	first, ids, err := mergeQADesign(nil, draft, "blackbox", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ids["new-output"] != "BLACKBOX:C1" {
		t.Fatal(ids)
	}
	if err = validateQADraft(r, "blackbox", first); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(first)
	delta := QADesign{Manifest: coverage.AcceptanceManifest{Binding: first.Manifest.Binding}}
	c := first.Manifest.Cases[0]
	c.CaseID = "new-error"
	c.Oracle = "documented error output"
	c.Steps = "invoke invalid argument"
	c.Dependencies = []string{first.Manifest.Cases[0].CaseID}
	delta.Manifest.Cases = []coverage.AcceptanceCase{c}
	delta.Manifest.Edges = []coverage.CoverageEdge{{SourceID: r.Requirement.Path, PointID: "BLACKBOX:AC-001", CaseID: "new-error"}}
	second, _, err := mergeQADesign(&first, delta, "blackbox", nil)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(first)
	if string(before) != string(after) {
		t.Fatal("merge mutated prior receipt")
	}
	if err := validateQADraft(r, "blackbox", second); err != nil {
		t.Fatal("increment could not reference retained dependency", err)
	}
	if len(second.Manifest.Cases) != 2 || !reflect.DeepEqual(second.Manifest.Cases[0], first.Manifest.Cases[0]) {
		t.Fatal("lost retained case")
	}
	var restored QADesign
	b, _ := json.Marshal(second)
	_ = json.Unmarshal(b, &restored)
	if _, _, err = mergeQADesign(&restored, restored, "blackbox", nil); err == nil || !strings.Contains(err.Error(), "no substantive") {
		t.Fatal("replayed whole set accepted", err)
	}
	prose := QADesign{Manifest: coverage.AcceptanceManifest{Binding: restored.Manifest.Binding}, CoverageJudgment: "AC missing, no work"}
	if _, _, err = mergeQADesign(&restored, prose, "blackbox", nil); err == nil {
		t.Fatal("prose-only repair accepted")
	}
	removal := QADesign{Manifest: coverage.AcceptanceManifest{Binding: restored.Manifest.Binding}, RemoveCases: []string{restored.Manifest.Cases[1].CaseID}}
	reduced, _, err := mergeQADesign(&restored, removal, "blackbox", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reduced.Manifest, first.Manifest) {
		t.Fatal("explicit removal changed unrelated entries")
	}
	replacement := delta
	replacement.Replace = true
	replacement.Manifest.Cases[0].Dependencies = nil
	replacement.Manifest.Sources = first.Manifest.Sources
	replacement.Manifest.Points = first.Manifest.Points
	replaced, _, err := mergeQADesign(&restored, replacement, "blackbox", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateQADraft(r, "blackbox", replaced); err != nil {
		t.Fatal("replacement not valid", err)
	}
	if len(replaced.Manifest.Cases) != 1 {
		t.Fatal("explicit replacement retained omitted case")
	}
}

func TestQADraftCoverageGapBelongsToReview(t *testing.T) {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	s, _ := e.Load()
	r := s.State.Regular
	d := testDesign(r, "BLACKBOX")
	// A known requirement source without a case is a reviewable gap, not malformed author output.
	d.Manifest.Edges = nil
	if err := validateQADraft(r, "blackbox", d); err != nil {
		t.Fatal(err)
	}
	if err := validateQADesign(r, "blackbox", d); err == nil {
		t.Fatal("incomplete draft became complete coverage")
	}
	review := coverage.ReviewResult{Binding: d.Manifest.Binding, Scope: coverage.ScopeFull,
		SourceDecisions: []coverage.ReviewDecision{{ID: r.Requirement.Path, Status: coverage.StatusFail}},
		PointDecisions:  []coverage.ReviewDecision{{ID: d.Manifest.Points[0].PointID, Status: coverage.StatusFail}},
		CaseDecisions:   []coverage.ReviewDecision{{ID: d.Manifest.Cases[0].CaseID, Status: coverage.StatusPass}}}
	review.SetStatus = coverage.StatusFail
	review.SourceDecisions[0].Status = coverage.StatusPass
	review.PointDecisions[0].Status = coverage.StatusPass
	if err := validateFailedQAReview(r, "blackbox", d, review); err != nil {
		t.Fatal("independent coverage gap rejected", err)
	}
	d.Manifest.Edges = []coverage.CoverageEdge{{SourceID: r.Requirement.Path, PointID: "unknown", CaseID: d.Manifest.Cases[0].CaseID}}
	if err := validateQADraft(r, "blackbox", d); err == nil {
		t.Fatal("draft accepted broken reference")
	}
}

func TestQADraftDeliveryReviewRepairAndReplay(t *testing.T) {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "git-base")
	completeQA(t, e, "blackbox", "design")
	s, _ := e.Load()
	q := s.State.Regular.Preparation.QA["blackbox"]
	if q.Frozen || q.Review != nil {
		t.Fatal("draft granted approval")
	}
	original := *q.Design
	failQAReview(t, e, "blackbox")
	s, _ = e.Load()
	if !strings.Contains(s.State.Regular.Preparation.QA["blackbox"].DesignGap, "compare the actual output") {
		t.Fatal("review repair reason was not carried to designer")
	}
	x := currentExecution(t, e, "blackbox-design")
	claimAndAttach(t, e, x)
	result := preparationResult(x)
	result.Design = &original
	got := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	if got.Status != "REJECTED_RESULT" {
		t.Fatal("unchanged repair accepted", got)
	}
	sourceUnavailable(t, e, x)
	x = currentExecution(t, e, "blackbox-design")
	claimAndAttach(t, e, x)
	result = preparationResult(x)
	delta := QADesign{Manifest: coverage.AcceptanceManifest{Binding: original.Manifest.Binding, Cases: []coverage.AcceptanceCase{original.Manifest.Cases[0]}}}
	delta.Manifest.Cases[0].Steps = "invoke, capture stdout and compare exactly with hello"
	result.Design = &delta
	if got = submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result}); got.Status != "ACCEPTED" {
		t.Fatal(got)
	}
	sourceUnavailable(t, e, x)
	s, _ = e.Load()
	q = s.State.Regular.Preparation.QA["blackbox"]
	if len(q.Design.Manifest.Edges) != 1 || q.Design.Manifest.Cases[0].CaseID != original.Manifest.Cases[0].CaseID {
		t.Fatal("increment lost stable mappings")
	}
	completeQA(t, e, "blackbox", "review")
	s, _ = e.Load()
	if !s.State.Regular.Preparation.QA["blackbox"].Frozen {
		t.Fatal("repaired design did not freeze through independent review")
	}
}

func TestQAIncrementSharedCommandInvalidatesOnlyItsUsers(t *testing.T) {
	old := coverage.AcceptanceManifest{SharedSteps: []coverage.SharedStep{{ID: "setup", Procedure: "prepare data", Command: &coverage.Command{Executable: "fixture", Args: []string{"old"}}}}, Cases: []coverage.AcceptanceCase{{CaseID: "one", SharedSteps: []string{"setup"}}, {CaseID: "two"}}}
	data, _ := json.Marshal(old)
	var next coverage.AcceptanceManifest
	_ = json.Unmarshal(data, &next)
	next.SharedSteps[0].Command.Args = []string{"new"}
	review := coverage.ReviewResult{CaseDecisions: []coverage.ReviewDecision{{ID: "one", Status: coverage.StatusPass}, {ID: "two", Status: coverage.StatusPass}}, SharedStepDecisions: []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass}}}
	retained := ChangedQA(old, next, review)
	if len(retained.CaseDecisions) != 1 || retained.CaseDecisions[0].ID != "two" || len(retained.SharedStepDecisions) != 0 {
		t.Fatal(retained)
	}
}

func TestWhiteboxIncrementRequiresNewEvidenceOnlyForChangedCases(t *testing.T) {
	prior := QADesign{Manifest: coverage.AcceptanceManifest{Cases: []coverage.AcceptanceCase{{CaseID: "one", Oracle: "old"}, {CaseID: "two", Oracle: "stable"}}}}
	data, _ := json.Marshal(prior)
	var next QADesign
	_ = json.Unmarshal(data, &next)
	next.Manifest.Cases[0].Oracle = "new"
	if err := validateQAReuseIncrement(&prior, next, nil, nil); err == nil {
		t.Fatal("stale test evidence accepted")
	}
	if err := validateQAReuseIncrement(&prior, next, []TestReuse{{CaseID: "one"}}, nil); err != nil {
		t.Fatal("unchanged case needlessly rerun", err)
	}
}

func TestQAIncrementAddsPrerequisiteWithoutReplacingOldCases(t *testing.T) {
	previous := QADesign{Manifest: coverage.AcceptanceManifest{Cases: []coverage.AcceptanceCase{{CaseID: "BLACKBOX:C1", Oracle: "result"}}}}
	delta := QADesign{Manifest: coverage.AcceptanceManifest{Cases: []coverage.AcceptanceCase{{CaseID: "new-setup", Oracle: "ready"}, {CaseID: "BLACKBOX:C1", Oracle: "result", Dependencies: []string{"new-setup"}}}}}
	merged, ids, err := mergeQADesign(&previous, delta, "blackbox", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Manifest.Cases) != 2 || merged.Manifest.Cases[0].CaseID != ids["new-setup"] || merged.Manifest.Cases[1].CaseID != "BLACKBOX:C1" || merged.Manifest.Cases[1].Dependencies[0] != ids["new-setup"] {
		t.Fatal("increment lost stable identity or prerequisite order")
	}
}
