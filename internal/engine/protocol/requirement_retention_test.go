package protocol

import (
	"reflect"
	"testing"
)

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
