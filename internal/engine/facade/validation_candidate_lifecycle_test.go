package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"testing"
)

// Completed external facts remain auditable when authorized QA changes supersede their inputs.
func TestFinalCandidateCompletionAfterQAChange(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"THAW", "QA_ARTIFACT_REPAIR"} {
		for _, held := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/held=%t", kind, held), func(t *testing.T) {
				d := newValidationDriver(t, "blackbox", "quality")
				id, in := d.action("engine.qa.design.v6")
				m := simpleManifest(in)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
				id, in = d.action("engine.qa.review.rationale.v2")
				review := approvedReview(in.Manifest)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &review})
				id, in = d.action("engine.development.batch.v6")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implement input contract"}})
				d.materialize()
				var old protocol.HostActionIntent
				for _, v := range d.run.AdapterIntents {
					old = v
				}
				if old.Adapter == nil || old.Adapter.Operation != "op.candidate" || old.Adapter.Params["stage"] != "REVIEW_READY" {
					t.Fatal("missing final candidate intent")
				}
				d.chooseValidation(protocol.ValidationRequest{Kind: kind, Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
				id, in = d.action("engine.qa.design.v6")
				m = in.Manifest
				m.Cases[0].Preconditions += "; initialized input file"
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
				id, in = d.action("engine.qa.review.rationale.v2")
				review = approvedReview(in.Manifest)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &review})
				currentVersion := d.run.Validation.Sets[coverage.ReviewBlackbox].Version
				var params protocol.CandidateParameters
				data, _ := json.Marshal(old.Adapter.Params)
				if err := json.Unmarshal(data, &params); err != nil {
					t.Fatal(err)
				}
				delivered := d.candidateReceipt(old.ActionID, old, params)
				if held {
					request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Revise"}, protocol.AskOption{ID: "abort", Label: "Keep"})
					if err != nil {
						t.Fatal(err)
					}
					d.submit(request)
				}
				d.submit(delivered)
				d.submit(delivered)
				d.reopen()
				d.drive()
				if held {
					if _, ok := d.run.AdapterIntents[old.ActionID]; !ok || d.run.Validation.Candidate != nil {
						t.Fatal("requirement barrier settled candidate prematurely")
					}
					ask := d.run.PendingRequirementAsk
					d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "abort"}})
				}
				d.reopen()
				d.drive()
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				recorded, ok := snapshot.State.HostActionReceipts[old.ActionID]
				if !ok || recorded.AdapterEvidence == nil || recorded.AdapterEvidence.Values["identity"] != delivered.HostAction.AdapterEvidence.Values["identity"] || recorded.PayloadDigest != old.PayloadDigest {
					t.Fatal("old external completion facts lost")
				}
				if _, ok := d.run.AdapterIntents[old.ActionID]; ok {
					t.Fatal("stale intent blocks rebuilding")
				}
				if d.run.Validation.Candidate != nil || len(d.run.Validation.Waves) != 0 {
					t.Fatal("stale output promoted to current candidate")
				}
				if len(d.run.AdapterIntents) != 1 {
					t.Fatalf("expected one replacement intent, got %d", len(d.run.AdapterIntents))
				}
				d.submit(delivered)
				d.reopen()
				d.materialize()
				d.drive()
				if d.run.Validation.Candidate == nil || d.run.Validation.Candidate.Identity == delivered.HostAction.AdapterEvidence.Values["identity"] {
					t.Fatal("new candidate not materialized independently")
				}
				wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				if wave.Versions[coverage.ReviewBlackbox] != currentVersion {
					t.Fatal("new wave does not bind newly approved cases")
				}
				id, in = d.action("engine.qa.execution.v4")
				execution := executionFor(in)
				d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &execution}))
				id, in = d.gateAction("quality")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
				d.reopen()
				d.drive()
				if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
					t.Fatal("current candidate cannot complete validation")
				}
			})
		}
	}
}
