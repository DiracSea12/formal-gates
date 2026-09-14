package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"reflect"
	"strings"
	"testing"
)

func TestDevelopmentWorkspaceKeepDoesNotCarryUncommittedBatchAcceptance(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "D1")
	completeBatch(t, e, "B2")
	loaded, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := loaded.State.Regular
	old := r.Preparation
	oldTask := batchTask(old, "B2")
	r.Requirement.Revision = "new-confirmed-requirement"
	r.Plan.RequirementRevision = r.Requirement.Revision
	r.Plan.ConfirmedRevision = r.Requirement.Revision
	r.Changes = append(r.Changes, RequirementChangeReceipt{
		ToRevision:      r.Requirement.Revision,
		UserDisposition: &RequirementImpactDecision{Choices: map[string]string{"development": "keep", "batch:B1": "keep"}},
		Impact:          map[string]string{"development": "unaffected", "batch:B1": "unaffected"},
	})
	old.BindingDigest = "previous-requirement-binding"
	prepareProjection(r)
	p := r.Preparation
	if _, kept := p.Batches["B2"]; kept {
		t.Fatal("keeping workspace silently retained unfinished Batch acceptance")
	}
	if _, exists := p.Commits["B2"]; exists {
		t.Fatal("uncommitted Batch acquired a phantom receipt")
	}
	if p.Resources["development"] != old.Resources["development"] || p.DevelopmentSnapshot != "D1" || !reflect.DeepEqual(p.Commits["B1"], old.Commits["B1"]) {
		t.Fatal("workspace, checkpoint or explicitly retained Batch changed")
	}
	if batchTask(p, "B2") == oldTask {
		t.Fatal("new requirement reused an already completed execution task")
	}
	if _, exists := old.Batches["B2"]; !exists {
		t.Fatal("original acceptance evidence was erased")
	}
}

func TestExplicitDevelopmentKeepSurvivesTopologyRebinding(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "base")
	loaded, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := loaded.State.Regular
	old := r.Preparation
	old.Batches["B1"] = PreparationResult{Status: "PASS"}
	r.RequirementImpact = &RequirementImpactDecision{RequirementRevision: r.Requirement.Revision, Candidate: r.Candidate, Choices: map[string]string{"development": "keep", "batch:B1": "keep", "blackbox-review": "delegate"}}
	old.BindingDigest = "previous binding"
	prepareProjection(r)
	if r.Preparation.Resources["development"] != old.Resources["development"] || r.Preparation.DevelopmentSnapshot != old.DevelopmentSnapshot {
		t.Fatal("explicit keep lost workspace or snapshot")
	}
	if !reflect.DeepEqual(r.Preparation.Batches["B1"], old.Batches["B1"]) {
		t.Fatal("explicit keep lost batch result")
	}
	if len(r.PreparationHistory) != 1 {
		t.Fatal("original preparation history not retained")
	}
	// The same decision repairs a previously projected empty preparation using
	// its preserved history, without inventing evidence or touching the files.
	r.Preparation.Resources = map[string]string{}
	r.Preparation.Batches = map[string]PreparationResult{}
	prepareProjection(r)
	if _, ok := r.Preparation.Batches["B1"]; !ok {
		t.Fatal("saved explicit keep did not recover retained result")
	}
}
func TestExplicitDevelopmentRetentionDoesNotOverrideChangedInput(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "base")
	loaded, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := loaded.State.Regular
	old := r.Preparation
	r.PreparationHistory = []Preparation{*old}
	r.RequirementImpact = &RequirementImpactDecision{RequirementRevision: "obsolete", Candidate: r.Candidate, Choices: map[string]string{"development": "keep"}}
	p := &Preparation{PlanDigest: old.PlanDigest, Resources: map[string]string{}, Batches: map[string]PreparationResult{}}
	retainExplicitDevelopment(r, p)
	if len(p.Resources) != 0 {
		t.Fatal("obsolete decision reused workspace")
	}
	r.RequirementImpact.RequirementRevision = r.Requirement.Revision
	r.RequirementImpact.Choices["development"] = "rerun"
	retainExplicitDevelopment(r, p)
	if len(p.Resources) != 0 {
		t.Fatal("rerun decision reused workspace")
	}
}

func TestUnaffectedAdoptionPreservesUnfrozenQADesignAndApprovals(t *testing.T) {
	for _, mode := range []string{"blackbox", "whitebox"} {
		for _, revising := range []bool{false, true} {
			for _, impact := range []string{"unaffected", "affected"} {
				t.Run(fmt.Sprintf("%s/revising=%t/%s", mode, revising, impact), func(t *testing.T) {
					e := preparationEngine(t, mode)
					gitReceipt(t, e, "base")
					loaded, err := e.Load()
					if err != nil {
						t.Fatal(err)
					}
					r := loaded.State.Regular
					old := r.Preparation
					design := testDesign(r, strings.ToUpper(mode))
					review := coverage.ReviewResult{Binding: design.Manifest.Binding, Scope: coverage.ScopeAffected, CaseDecisions: []coverage.ReviewDecision{{ID: design.Manifest.Cases[0].CaseID, Status: coverage.StatusPass}}}
					q := &PreparedQA{Design: &design, RetainedReview: &review, ReviewScope: coverage.ScopeAffected, Revision: 2, ConsecutiveFailures: 1, DesignGap: "Remaining specific issue", Snapshot: "base"}
					if revising {
						q.PreviousDesign = q.Design
						q.Design = nil
						q.NeedsRevision = true
					}
					old.QA[mode] = q
					before, _ := json.Marshal(q)
					old.Resources[mode] = "/registered/qa-workspace"
					r.Candidate = "adopted"
					r.Changes = append(r.Changes, RequirementChangeReceipt{FromRevision: r.Requirement.Revision, ToRevision: r.Requirement.Revision, Origin: "EXTERNAL_ADOPTION", Impact: map[string]string{"development": "unaffected", mode + "-review": impact}})
					prepareProjection(r)
					retained := r.Preparation.QA[mode]
					if impact == "affected" {
						if retained.Design != nil || retained.PreviousDesign != nil || retained.RetainedReview != nil {
							t.Fatal("affected QA was inherited")
						}
					} else {
						if mode == "whitebox" {
							if retained.Snapshot != "" || retained.AuthoringBase != "" || retained.TestArtifactsFrom != q.Snapshot || retained.TestArtifactsBase != old.DevelopmentSnapshot || r.Preparation.Resources[mode] != "" {
								t.Fatal("whitebox retained old implementation workspace")
							}
							retained.Snapshot = q.Snapshot // Compare all remaining evidence without changing history.
							retained.TestArtifactsFrom = q.TestArtifactsFrom
							retained.TestArtifactsBase = q.TestArtifactsBase
						}
						after, _ := json.Marshal(retained)
						if !bytes.Equal(before, after) {
							t.Fatalf("unaffected partial QA changed: before %s after %s", before, after)
						}
						if mode == "blackbox" && r.Preparation.Resources[mode] != old.Resources[mode] {
							t.Fatal("registered QA workspace lost")
						}
						retained.RetainedReview.CaseDecisions[0].Status = coverage.StatusFail
						if q.RetainedReview.CaseDecisions[0].Status != coverage.StatusPass {
							t.Fatal("new review mutated historical approval")
						}
					}
					history, _ := json.Marshal(r.PreparationHistory[len(r.PreparationHistory)-1].QA[mode])
					if !bytes.Equal(before, history) {
						t.Fatal("original QA history changed")
					}
				})
			}
		}
	}
}

func TestExternalAdoptionRequiresApplicableImpactsBeforeMutation(t *testing.T) {
	e := preparationEngine(t, "blackbox", "whitebox")
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "D1")
	loaded, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	impacts := map[string]string{"product-review": "unaffected", "start-readiness": "unaffected", "development": "unaffected", "blackbox-review": "unaffected", "whitebox-review": "unaffected"}
	for missing := range impacts {
		for _, value := range []string{"", "keep"} {
			t.Run(missing+"/"+value, func(t *testing.T) {
				// Check the request itself, before an event transaction could hide
				// accidental mutation by discarding its rejected state copy.
				s, err := e.Load()
				if err != nil {
					t.Fatal(err)
				}
				before, _ := json.Marshal(s.State)
				impact := map[string]string{}
				for key, v := range impacts {
					if key != missing {
						impact[key] = v
					}
				}
				if value != "" {
					impact[missing] = value
				}
				request := RegularRequest{Action: "adopt-external", Reason: "fixture external delivery", External: &ExternalObservation{Identity: "external", Lineage: s.State.Regular.BaseSnapshot, DiffDigest: "sha256:diff"}, Impact: impact}
				err = e.regularRequest(s.State, "adopt", request, s.Revision)
				if err == nil || !strings.Contains(err.Error(), "explicit impact for "+missing) {
					t.Fatalf("missing/invalid impact accepted: %v", err)
				}
				after, _ := json.Marshal(s.State)
				if !bytes.Equal(before, after) {
					t.Fatal("rejected adoption mutated state")
				}
			})
		}
	}
	old := loaded.State.Regular.Preparation.Batches["B1"]
	regularChoice(t, e, RegularRequest{Action: "adopt-external", Reason: "explicitly preserve delivery", External: &ExternalObservation{Identity: "external", Lineage: loaded.State.Regular.BaseSnapshot, DiffDigest: "sha256:diff"}, Impact: impacts}, "confirm")
	after, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old, after.State.Regular.Preparation.Batches["B1"]) {
		t.Fatal("explicit unaffected adoption lost accepted Batch")
	}
}
