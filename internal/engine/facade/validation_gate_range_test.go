package facade

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestRegularStartRequiresNativeBaseBeforeWriting(t *testing.T) {
	root := t.TempDir()
	_, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "missing-base", Route: "regular", IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "target", "package", DefaultProvider)})
	if err == nil {
		t.Fatal("regular run without native base accepted")
	}
	if _, err := os.Stat(filepath.Join(root, EngineNamespace)); !os.IsNotExist(err) {
		t.Fatalf("start wrote before validating native base: %v", err)
	}
}

func TestGateRangeRequiresBoundOwnershipEvidence(t *testing.T) {
	for _, variant := range []string{"missing", "duplicate", "empty-path", "wrong-parameters"} {
		t.Run(variant, func(t *testing.T) {
			d, intent := candidateAtStage(t, "REVIEW_READY")
			ev := candidateCompletion(t, d, intent)
			values := ev.HostAction.AdapterEvidence.Values
			switch variant {
			case "missing":
				delete(values, "gateView")
			case "duplicate":
				values["gateView"] = protocol.GateReviewView{TestOwnedPaths: []string{"app_test.go", "app_test.go"}}
			case "empty-path":
				values["gateView"] = protocol.GateReviewView{TestOwnedPaths: []string{""}}
			case "wrong-parameters":
				values["parametersDigest"] = "another-range"
			}
			before, _ := d.f.engine.Load()
			if _, _, err := d.f.Submit(ev); err == nil {
				t.Fatal("candidate without bound complete gate view accepted")
			}
			after, _ := d.f.engine.Load()
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected view wrote state")
			}
			d.reopen()
			ev = candidateCompletion(t, d, intent)
			d.submit(ev)
			d.drive()
			_, in := d.gateAction("quality")
			if in.Gate == nil || in.Gate.BaseSnapshot != frozenSnapshot || in.Gate.Gate != "quality" || in.Gate.Candidate.Identity != in.Binding.Candidate || !reflect.DeepEqual(in.Gate.TestOwnedPaths, []string{"app_test.go"}) {
				t.Fatalf("missing exact gate input: %+v", in.Gate)
			}
			saved := *in.Gate
			d.reopen()
			_, in = d.gateAction("quality")
			if !reflect.DeepEqual(saved, *in.Gate) {
				t.Fatal("reopen changed gate material")
			}
		})
	}
}

func TestGateRangeKeepsRunBaseAcrossRepairRouteAndRequirement(t *testing.T) {
	d := gateImpactBoundary(t)
	w := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if w.Candidate == d.run.Validation.Waves[0].Candidate {
		t.Fatal("fixture did not materialize a repaired candidate")
	}
	unlockRouteByRequirementRedo(d)
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security", "performance"}))
	d.reopen()
	d.drive()
	id, in := d.gateAction("performance")
	before := *in.Gate
	binding := in.Binding
	if in.Gate == nil || in.Gate.BaseSnapshot != frozenSnapshot || in.Gate.Candidate != w.Candidate || in.Gate.BaseSnapshot == d.run.Validation.DevelopmentSnapshot || in.Gate.Gate != "performance" || len(in.Gate.TestOwnedPaths) != 0 {
		t.Fatalf("wrong immutable review range: %+v", in.Gate)
	}
	confirmValidationWork(d, "")
	d.reopen()
	d.drive()
	after, ok := d.run.ValidationInputs[id]
	if !ok || after.Binding != binding || after.Gate == nil || !reflect.DeepEqual(before, *after.Gate) {
		t.Fatal("CARRY changed the issued gate duty or its native range")
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
}

func TestGateRangeOwnershipIsNotLimitedToCurrentMaterializationPaths(t *testing.T) {
	d, intent := candidateAtStage(t, "REVIEW_READY")
	ev := candidateCompletion(t, d, intent)
	// This native range also contains a test artifact carried in the base;
	// it is not a write declared by the current materialization operation.
	owned := []string{"app_test.go", "carried-fixture.go"}
	ev.HostAction.AdapterEvidence.Values["gateView"] = protocol.GateReviewView{TestOwnedPaths: owned}
	d.submit(ev)
	d.reopen()
	d.drive()
	_, in := d.gateAction("quality")
	if in.Gate == nil || !reflect.DeepEqual(in.Gate.TestOwnedPaths, owned) {
		t.Fatal("native range ownership was reduced to this operation's writes")
	}
}

func TestGateOwnershipEvidenceRequiresReviewReadyStage(t *testing.T) {
	for _, stage := range []string{"DEVELOPMENT", "AUTHORING", "QA_ARTIFACT"} {
		t.Run(stage, func(t *testing.T) {
			d, intent := candidateAtStage(t, stage)
			ev := candidateCompletion(t, d, intent)
			ev.HostAction.AdapterEvidence.Values["gateView"] = protocol.GateReviewView{TestOwnedPaths: []string{}}
			before, _ := d.f.engine.Load()
			if _, _, err := d.f.Submit(ev); err == nil {
				t.Fatal("non-review operation supplied a consumable gate view")
			}
			after, _ := d.f.engine.Load()
			if !reflect.DeepEqual(before, after) {
				t.Fatal("stage-inapplicable gate proof changed state")
			}
			d.submit(candidateCompletion(t, d, intent))
		})
	}
}
