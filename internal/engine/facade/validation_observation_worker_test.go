package facade

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
)

func TestSemanticObservationFixesAffectedScopeAndGateImpactTogether(t *testing.T) {
	for _, scenario := range []string{"complete", "unaffected-only", "illegal-impact"} {
		t.Run(scenario, func(t *testing.T) {
			d := gateImpactBoundary(t)
			id, input := d.action("engine.validation.observation")
			issued := input.Observation
			if issued == nil || len(issued.Scopes) != 1 || len(issued.GateImpacts) != 1 || issued.Scopes[0].Mode != coverage.ReviewBlackbox || !slices.Equal(issued.Scopes[0].MustExecute, []string{"invalid"}) || !slices.Contains(issued.Scopes[0].ApprovedCases, "normal") {
				t.Fatalf("affected scope lost actual legal complement: %#v", issued)
			}
			impact := issued.GateImpacts[0]
			if impact.Gate != "security" || impact.Range == nil || impact.Evidence.Validation == nil || impact.Evidence.Validation.Status != "PASS" || impact.Evidence.Validation.Execution != nil || impact.Evidence.Validation.Manifest != nil {
				t.Fatalf("impact does not preserve its original gate range/evidence: %#v", impact)
			}
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			original := snapshot.State.WorkerInputs(d.f.definition)[id]
			judgment := worker.ObservationJudgments{Findings: []worker.FindingJudgment{}, Scopes: []worker.ScopeJudgment{{Cases: []string{"invalid"}, Reason: "Only rejection diagnostic changed; original normal behavior remains unaffected"}}, GateImpacts: []worker.ImpactJudgment{{Decision: "INHERIT", Reason: "Original authorization security responsibility and reviewed range remain unaffected"}}}
			if scenario == "unaffected-only" {
				judgment.Scopes[0].Cases = []string{"normal"}
			}
			if scenario == "illegal-impact" {
				judgment.GateImpacts[0].Decision = "PASS"
			}
			raw, err := json.Marshal(judgment)
			if err != nil {
				t.Fatal(err)
			}
			event, err := worker.DecodeResult(original, d.eventID(), raw, "actual-semantic-native-output")
			if err != nil {
				t.Fatal(err)
			}
			d.receipt(id)
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			accepted := d.submit(event)
			if scenario != "complete" {
				if accepted.Status != "REJECTED_INVALID_RESULT" {
					t.Fatalf("original validator did not reject invalid %s: %#v", scenario, accepted)
				}
				after, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before.State.Validation, after.State.Validation) {
					t.Fatal("failed package partly fixed scope/impact")
				}
				return
			}
			if accepted.Status != "ACCEPTED" {
				t.Fatalf("legal original scope/impact rejected: %#v", accepted)
			}
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			wave := after.State.Validation.Waves[len(after.State.Validation.Waves)-1]
			for _, target := range wave.Targets {
				if target.Kind == "QA" && (!slices.Equal(target.RequiredCases, []string{"invalid"}) || target.ScopeReason == "") {
					t.Fatal("program did not retain exact affected subset")
				}
				if target.Gate == "security" && (target.GateImpact.Decision != "INHERIT" || target.ResultAction != impact.Evidence.ActionID) {
					t.Fatal("impact rewrote original PASS evidence")
				}
			}
			if after.State.Results[id].PayloadDigest != "actual-semantic-native-output" {
				t.Fatal("semantic worker lost original output evidence")
			}
			repeated, _, err := d.f.Submit(event)
			if err != nil || !reflect.DeepEqual(accepted, repeated) {
				t.Fatalf("repeat changed original acceptance: %#v %v", repeated, err)
			}
			// The same result completion first releases the approved native recipe.
			released := false
			for _, intent := range after.State.ExecutableHostActions() {
				if intent.Adapter == nil || intent.Adapter.Operation != "op.check" {
					continue
				}
				raw, err := json.Marshal(intent.Adapter.Params)
				if err != nil {
					t.Fatal(err)
				}
				var params protocol.CheckParameters
				if err := json.Unmarshal(raw, &params); err != nil {
					t.Fatal(err)
				}
				if params.QA != nil && params.QA.CaseID == "invalid" && params.QA.Execution.Candidate == issued.Candidate {
					released = true
				}
			}
			if !released {
				t.Fatal("semantic completion did not refill the original approved native recipe")
			}
			d.drive()
			_, qa := d.action("engine.qa.execution")
			if !slices.Equal(qa.RequiredCases, []string{"invalid"}) || len(qa.Inherited) != 1 || qa.Inherited[0].CaseID != "normal" || qa.Inherited[0].Result != coverage.ExecutedPass {
				t.Fatal("scope bypassed original complementary inheritance")
			}

		})
	}
}
