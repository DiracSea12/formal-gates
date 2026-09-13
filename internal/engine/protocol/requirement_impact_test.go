package protocol

import (
	"strings"
	"testing"
)

func TestRequirementImpactAskRequiresExplicitUserDisposition(t *testing.T) {
	e := regularEngine(t, nil)
	regularStart(t, e)
	submitRegular(t, e, RegularEvent{Operation: "REQUEST", Request: &RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "fixture user changes acceptance"}})
	snap, _ := e.Load()
	a := snap.State.Regular.Ask
	if len(a.ImpactItems) != 2 {
		t.Fatalf("engine impact scope=%+v", a.ImpactItems)
	}
	missing := RegularDecision{RequestID: a.ID, Token: RegularFreshness(snap.Revision, a.ID), Digest: a.Digest, Choice: "confirm"}
	event, _ := NewRegularEvent("missing-user-disposition", RegularEvent{Operation: "DECIDE", ExpectedRevision: snap.Revision, Decision: &missing})
	if _, err := e.Submit(event, fingerprintOf(t, e)); err == nil || !strings.Contains(err.Error(), "USER_IMPACT_REQUIRED") {
		t.Fatalf("missing decision=%v", err)
	}
	unchanged, _ := e.Load()
	if unchanged.Revision != snap.Revision || unchanged.State.Regular.RequirementImpact != nil {
		t.Fatal("missing response mutated authorization")
	}
	if _, _, err := e.Drive(fingerprintOf(t, e)); err != nil {
		t.Fatal(err)
	}
	paused, _ := e.Load()
	if paused.State.Regular.Ask == nil || len(paused.State.RegularExecutions) != len(snap.State.RegularExecutions) {
		t.Fatal("unanswered Ask advanced work")
	}
	missing.Token = RegularFreshness(paused.Revision, a.ID)
	missing.Dispositions = map[string]string{"product-review": "rerun", "start-readiness": "delegate"}
	submitRegular(t, e, RegularEvent{Operation: "DECIDE", Decision: &missing})
	doc := Document("requirements.md", []byte(regularDocument+"\nClarified fixture acceptance.\n"))
	request := RegularRequest{Action: "apply-requirement", Document: &doc, Origin: "USER_INITIATED_CHANGE", Meaning: "preserved", Reason: "apply exact candidate", Impact: map[string]string{"product-review": "unaffected", "start-readiness": "unknown"}}
	before, _ := e.Load()
	event, _ = NewRegularEvent("override-fixed-user-choice", RegularEvent{Operation: "REQUEST", ExpectedRevision: before.Revision, Request: &request})
	if _, err := e.Submit(event, fingerprintOf(t, e)); err == nil || !strings.Contains(err.Error(), "USER_IMPACT_CONFLICT") {
		t.Fatalf("override=%v", err)
	}
	delete(request.Impact, "product-review")
	submitRegular(t, e, RegularEvent{Operation: "REQUEST", Request: &request})
	normalized, _ := e.Load()
	if normalized.State.Regular.Ask.Request.Impact["product-review"] != "affected" {
		t.Fatal("user choice not normalized into frozen apply Ask")
	}
}

func TestRequirementImpactDelegationCannotAuthorizeUnlistedWork(t *testing.T) {
	r := &RegularState{Candidate: "c", RequirementImpact: &RequirementImpactDecision{RequirementRevision: "r", Candidate: "c", Choices: map[string]string{"product-review": "keep", "start-readiness": "delegate"}}}
	r.Requirement.Revision = "r"
	for _, q := range []RegularRequest{
		{Impact: map[string]string{"start-readiness": "unknown", "unlisted": "unaffected"}},
		{Impact: map[string]string{}},
		{Impact: map[string]string{"start-readiness": "unknown", "product-review": "affected"}},
	} {
		if err := resolveRequirementImpact(r, &q); err == nil {
			t.Fatalf("accepted missing/delegation-overreach: %+v", q)
		}
	}
}
