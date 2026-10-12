package facade

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"reflect"
	"strings"
	"testing"
)

func TestConfirmedCoveragePreservesEveryRelation(t *testing.T) {
	for _, complete := range []bool{false, true} {
		for _, reopen := range []bool{false, true} {
			name := map[bool]string{false: "missing", true: "complete"}[complete] + "/" + map[bool]string{false: "normal", true: "reopened"}[reopen]
			t.Run(name, func(t *testing.T) {
				inventory := &protocol.CoverageInventory{Sources: []coverage.RequiredSource{{SourceID: "SOL-001", Category: coverage.SolutionConstraint, Applicability: coverage.QAApplicability}, {SourceID: "SOL-002", Category: coverage.SolutionConstraint, Applicability: coverage.QAApplicability}}, Points: []coverage.AcceptancePoint{{PointID: "SP-001", ObservableBehavior: "records outcome", Oracle: "record exists"}}, Links: []protocol.SourcePoint{{SourceID: "SOL-001", PointID: "SP-001"}, {SourceID: "SOL-002", PointID: "AC-001"}, {SourceID: "SOL-002", PointID: "SP-001"}}}
				d := configuredValidationDriver(t, inventory, "blackbox")
				dev, di := d.action("engine.development.batch.v4")
				design, qi := d.action("engine.qa.design.v5")
				m := simpleManifest(qi)
				for _, id := range []string{"normal", "invalid"} {
					m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "SOL-001", PointID: "SP-001", CaseID: id}, coverage.CoverageEdge{SourceID: "SOL-002", PointID: "AC-001", CaseID: id})
					if complete {
						m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "SOL-002", PointID: "SP-001", CaseID: id})
					}
				}
				d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &m})
				d.drive()
				reviewID, ri := d.action("engine.qa.review.rationale.v2")
				r := approvedReview(m)
				d.business(reviewID, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &r})
				d.drive()
				d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implement public behavior"}})
				d.drive()
				d.materialize()
				if reopen {
					d.reopen()
					d.drive()
				}
				if !complete {
					for _, intent := range d.run.AdapterIntents {
						if intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" {
							t.Error("candidate for execution issued with a missing confirmed source-point relation")
						}
					}
					for _, in := range d.run.ValidationInputs {
						if in.Handler == "engine.qa.execution.v3" {
							t.Error("execution issued with missing relation")
						}
					}
					return
				}
				d.materialize()
				qa, in := d.action("engine.qa.execution.v3")
				found := false
				for _, e := range in.Whitelist.Entries {
					if e.SourceID == "SOL-002" && e.PointID == "SP-001" {
						found = true
					}
				}
				if !found {
					t.Fatal("complete relation lost from approved whitelist")
				}
				report := executionFor(in)
				d.business(qa, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report}))
				d.drive()
				waves := d.run.Validation.Waves
				if waves[len(waves)-1].Status != "COMPLETE" {
					t.Fatal("complete confirmed graph cannot finish")
				}
			})
		}
	}
}

func relationInventory() *protocol.CoverageInventory {
	return &protocol.CoverageInventory{Sources: []coverage.RequiredSource{{SourceID: "SOL-001", Category: coverage.SolutionConstraint, Applicability: coverage.QAApplicability}, {SourceID: "SOL-002", Category: coverage.SolutionConstraint, Applicability: coverage.QAApplicability}}, Points: []coverage.AcceptancePoint{{PointID: "SP-001", ObservableBehavior: "records outcome", Oracle: "record exists"}}, Links: []protocol.SourcePoint{{SourceID: "SOL-001", PointID: "SP-001"}, {SourceID: "SOL-002", PointID: "AC-001"}}}
}
func relationManifest(in protocol.ValidationInput) coverage.AcceptanceManifest {
	m := simpleManifest(in)
	m.Edges = nil
	for _, link := range in.SourcePoints {
		for _, c := range m.Cases {
			m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: link.SourceID, PointID: link.PointID, CaseID: c.CaseID})
		}
	}
	return m
}

func TestConfirmedRelationChangeBlocksCarryUntilDesignIsComplete(t *testing.T) {
	for _, phase := range []string{"in-flight", "completed"} {
		t.Run(phase, func(t *testing.T) {
			inventory := relationInventory()
			d := configuredValidationDriver(t, inventory, "blackbox")
			_, in := d.action("engine.qa.design.v5")
			d.reviewReadyWithManifest(relationManifest(in))
			qa, qi := d.action("engine.qa.execution.v3")
			report := executionFor(qi)
			if phase == "completed" {
				d.business(qa, nativeExecutionResult(qi, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report}))
			}
			inventory.Links = append(inventory.Links, protocol.SourcePoint{SourceID: "SOL-002", PointID: "SP-001"})
			confirmValidationCoverage(d, "", inventory)
			d.reopen()
			d.drive()
			if d.run.Next.Operator == nil || !strings.Contains(d.run.Next.Operator.Facts[0].Value, "SOL-002/SP-001") {
				t.Fatalf("new confirmed relation was not exposed: %+v", d.run.Next)
			}
			if phase == "in-flight" {
				d.business(qa, nativeExecutionResult(qi, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report}))
				d.reopen()
				d.drive()
			}
			if d.run.Next.Operator == nil || !strings.Contains(d.run.Next.Operator.Facts[0].Value, "SOL-002/SP-001") {
				t.Fatal("old result erased the new relation obligation")
			}
			snap, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(snap.State.Results[qa].Validation.Execution, &report) {
				t.Fatal("carried result was rewritten")
			}
			d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal", "invalid"}}, "")
			id, in := d.action("engine.qa.design.v5")
			manifest := relationManifest(in)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
			d.drive()
			id, in = d.action("engine.qa.review.rationale.v2")
			approved := approvedReview(manifest)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
			d.materialize()
			id, in = d.action("engine.qa.execution.v3")
			report = executionFor(in)
			d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report}))
			d.drive()
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatal("repaired coverage did not complete")
			}
		})
	}
}

func TestSelectedModesShareConfirmedRelations(t *testing.T) {
	inventory := relationInventory()
	inventory.Links = append(inventory.Links, protocol.SourcePoint{SourceID: "SOL-002", PointID: "SP-001"})
	d := configuredValidationDriver(t, inventory, "blackbox", "whitebox", "quality")
	dev, di := d.action("engine.development.batch.v4")
	id, in := qaModeAction(d, "engine.qa.design.v5", coverage.ReviewBlackbox)
	black := simpleManifest(in)
	black.Sources = []string{"REQ-001", "SOL-001"}
	for _, c := range black.Cases {
		black.Edges = append(black.Edges, coverage.CoverageEdge{SourceID: "SOL-001", PointID: "SP-001", CaseID: c.CaseID})
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &black})
	d.drive()
	id, in = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewBlackbox)
	approved := approvedReview(black)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implement inputs and records"}})
	d.materialize()
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.design.v5", coverage.ReviewWhitebox)
	white := in.Manifest
	white.Sources = []string{"SOL-002"}
	white.Cases = []coverage.AcceptanceCase{{CaseID: "white-normal", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestValid", Oracle: "valid input succeeds"}, {CaseID: "white-invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestInvalid", Oracle: "invalid input rejected"}}
	for _, c := range white.Cases {
		for _, point := range []string{"AC-001", "SP-001"} {
			white.Edges = append(white.Edges, coverage.CoverageEdge{SourceID: "SOL-002", PointID: point, CaseID: c.CaseID})
		}
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &white, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewWhitebox)
	approved = approvedReview(white)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
	d.materialize()
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox} {
		id, in = qaModeAction(d, "engine.qa.execution.v3", mode)
		report := executionFor(in)
		for _, edge := range in.Whitelist.Entries {
			if mode == coverage.ReviewBlackbox && edge.SourceID == "SOL-002" || mode == coverage.ReviewWhitebox && edge.SourceID != "SOL-002" {
				t.Fatal("mode partition changed")
			}
		}
		d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report}))
	}
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "normal output order differs"}}})
	wave := d.run.Validation.Waves[0]
	for finding := range d.run.Validation.Findings {
		resolveGateFinding(d, finding, wave.ID)
	}
	id, in = d.action("engine.repair.v5")
	repair := gateRepairResult(in)
	d.business(id, repair)
	d.verifyRepair(repair)
	d.materialize()
	d.materialize()
	unlockRouteByRequirementRedo(d)
	d.confirmRoute(protocol.RouteDecision{Mode: "full", Gates: []string{"blackbox", "whitebox", "quality", "security"}})
	d.drive()
	id, in = d.gateAction("security")
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.qa.execution.v3" {
			t.Fatal("QA dispatched without its own host scope")
		}
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.qa.execution.v3" && input.Execution != nil && input.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
			t.Fatal("blackbox scope authorized whitebox")
		}
	}
	id, in = qaModeAction(d, "engine.qa.execution.v3", coverage.ReviewBlackbox)
	report := executionFor(in)
	d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report}))
	d.recordExecutionScope(coverage.ReviewWhitebox, "white-normal")
	id, in = qaModeAction(d, "engine.qa.execution.v3", coverage.ReviewWhitebox)
	report = executionFor(in)
	d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report}))
	d.drive()
	d.assertRequiredValidationComplete(coverage.ScopeAffected)
	if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
		t.Fatal("legal mode union did not complete")
	}
}
