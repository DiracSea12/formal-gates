package facade

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
)

func TestQARevisionMaterialCarriesOriginalConfirmedInstruction(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	thaw := func(kind, reason string) ControlCandidate {
		t.Helper()
		request, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlValidation, Validation: &ValidationControlInput{Kind: kind, Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", Cases: []int{1}}})
		if err != nil {
			t.Fatal(err)
		}
		d.submit(request.Event)
		candidate, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlValidation), Choice: "approve", Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		acceptance, run, err := d.f.SubmitControl(candidate)
		if err != nil || acceptance.Status != "ACCEPTED" {
			t.Fatal(acceptance, err)
		}
		d.run = run
		before := run.Revision
		if _, again, err := d.f.SubmitControl(candidate); err != nil || again.Revision != before {
			t.Fatal("replay changed authoritative state", err)
		}
		d.drive()
		return candidate
	}
	reason := "Replace the setup with the documented command; retain exact stdin and original negative oracle."
	candidate := thaw("THAW", reason)
	id, input := d.action("engine.qa.design.v3")
	instruction := input.RevisionInstruction
	if instruction == nil || instruction.Decision.RequestID != string(candidate.Event.Decide.Request) || instruction.Decision.Validation.Reason != reason || instruction.Request.Category != "CASE_EXECUTABILITY" || !reflect.DeepEqual(instruction.Request.AffectedCases, []string{"normal"}) || instruction.Request.Binding.RequirementRevision != input.Binding.RequirementRevision {
		t.Fatal("revision instruction lost its original confirmed facts")
	}
	checkMaterial := func(action string, expected string, requestID string) {
		t.Helper()
		d.drive()
		ref := d.run.WorkerMaterials[action]
		body, err := os.ReadFile(ref.Path)
		if err != nil {
			t.Fatal(err)
		}
		var material worker.Material
		if err := json.Unmarshal(body, &material); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(material.Prompt, expected) || !strings.Contains(material.Prompt, "CASE_EXECUTABILITY") || !strings.Contains(material.Prompt, requestID) {
			t.Fatal("actual issued material omitted original instruction, category or identity")
		}
	}
	checkMaterial(id, reason, instruction.Decision.RequestID)
	before := input
	d.reopen()
	d.drive()
	againID, again := d.action("engine.qa.design.v3")
	if againID != id || !reflect.DeepEqual(before, again) {
		t.Fatal("reopen changed original revision instruction")
	}
	manifest := input.Manifest
	manifest.Cases[0].Preconditions += "; use the documented setup"
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
	reviewAction, reviewInput := d.action("engine.qa.review.rationale.v2")
	if !reflect.DeepEqual(reviewInput.RevisionInstruction, instruction) {
		t.Fatal("independent review did not receive the same confirmed instruction")
	}
	checkMaterial(reviewAction, reason, instruction.Decision.RequestID)
	approved := approvedReview(manifest)
	d.business(reviewAction, protocol.ValidationResult{Binding: reviewInput.Binding, Status: "PASS", Review: &approved})
	d.drive()
	later := "Use the new current setup and preserve its exact return code."
	next := thaw("QA_ARTIFACT_REPAIR", later)
	nextID, nextInput := d.action("engine.qa.design.v3")
	if nextID == id || nextInput.RevisionInstruction == nil || nextInput.RevisionInstruction.Decision.RequestID != string(next.Event.Decide.Request) || nextInput.RevisionInstruction.Decision.Validation.Reason != later {
		t.Fatal("later authorized revision reused the old instruction")
	}
	checkMaterial(nextID, later, nextInput.RevisionInstruction.Decision.RequestID)
	body, err := os.ReadFile(d.run.WorkerMaterials[nextID].Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), reason) {
		t.Fatal("old revision reason leaked into new issued material")
	}
	resetRequest, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlReset})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(resetRequest.Event)
	reset, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlReset), Choice: "proceed", Reason: "reset the current QA scope"})
	if err != nil {
		t.Fatal(err)
	}
	if _, run, err := d.f.SubmitControl(reset); err != nil {
		t.Fatal(err)
	} else {
		d.run = run
	}
	d.drive()
	for _, input := range d.run.ValidationInputs {
		if input.RevisionInstruction != nil {
			t.Fatal("reset scope retained old QA revision feedback")
		}
	}
}
