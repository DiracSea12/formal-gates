package facade

import (
	"bytes"
	"encoding/json"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

// G AC-017 keeps a signed repair's diagnosis stable while a later correction
// records a different problem for the repaired snapshot's next wave.
func TestSignedRepairDiagnosisSurvivesCorrectionAndReopen(t *testing.T) {
	d, finding, wave := validationWithGateFinding(t)
	resolveGateFinding(d, finding, wave)
	id, input := d.action("engine.repair.v4")
	if input.Repair == nil || len(input.Repair.Problems) != 1 {
		t.Fatal("setup must issue the repair with its complete original diagnosis")
	}
	originalInput, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	originalResolution := d.run.Validation.Findings[finding].History[0]
	originalEvidence, err := json.Marshal(d.run.Validation.Findings[finding].Raw)
	if err != nil {
		t.Fatal(err)
	}
	correction := originalResolution
	correction.Decision = "NEW"
	correction.ProblemID = ""
	correction.Supersedes = 1
	correction.RootCause = "independent verification identifies a separate missing output value"
	correction.Consequence = "documented value is absent from otherwise correctly ordered output"
	correction.Severity = "P1"
	correction.SeverityRationale = "required output value is unavailable on the normal command"
	correction.Reason = "corrected root cause and consequence identify a distinct problem"
	event := protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{correction}}}}
	d.submit(event)
	for _, reopen := range []bool{false, true} {
		if reopen {
			d.reopen()
		}
		d.drive()
		actualID, actual := d.action("engine.repair.v4")
		actualInput, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		if actualID != id || !bytes.Equal(originalInput, actualInput) {
			t.Fatalf("signed repair input changed after correction/reopen=%v\nbefore=%s\nafter=%s", reopen, originalInput, actualInput)
		}
		if len(d.run.Validation.Findings[finding].History) != 2 || len(d.run.Validation.Problems) != 2 {
			t.Fatal("correction did not preserve both original and new problem facts")
		}
		raw, err := json.Marshal(d.run.Validation.Findings[finding].Raw)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, originalEvidence) {
			t.Fatal("correction rewrote original evidence")
		}
	}
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	replay, _, err := d.f.Submit(event)
	if err != nil {
		t.Fatal(err)
	}
	after, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if replay.Status != "ACCEPTED" || !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatal("same correction event replay changed persisted facts")
	}
}

// Finding provenance may precede a route change. Deferred correction follows
// the actual signed repair and its materialized candidate, not the report wave.
func TestDeferredCorrectionFollowsActualRepairResponsibility(t *testing.T) {
	d, finding, origin := validationWithGateFinding(t)
	unlockRouteByRequirementRedo(d)
	unlockRouteByRequirementRedo(d)
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.drive()
	id, input := d.gateAction("security")
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	d.drive()
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if wave.ID == origin {
		t.Fatal("route change did not create a distinct expected set")
	}
	resolveGateFinding(d, finding, wave.ID)
	id, input = d.action("engine.repair.v4")
	originalTask := input.Binding.Task
	correction := d.run.Validation.Findings[finding].History[0]
	correction.Decision, correction.ProblemID, correction.Supersedes = "NEW", "", 1
	correction.RootCause = "required output value absent after independent verification"
	correction.Consequence = "normal command lacks required value"
	correction.Severity = "P1"
	correction.SeverityRationale = "documented output unavailable; reordering cannot restore absent content"
	correction.Reason = "correct distinct root cause while issued ordering repair remains in flight"
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{correction}}}})
	d.reopen()
	d.drive()
	corrected := d.run.Validation.Findings[finding].History[1].ProblemID
	repairResult := gateRepairResult(input)
	d.business(id, repairResult)
	d.verifyRepair(repairResult)
	d.materialize()
	d.materialize()
	wave = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	for _, target := range wave.Targets {
		if target.Gate == "security" && target.Status != "PASS" {
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "GATE_IMPACT", Gate: "security", Decision: "INHERIT", Reason: "verified output-only ordering change preserves the prior security inputs"}}})
		}
	}
	d.recordExecutionScope(coverage.ReviewBlackbox, "normal", "invalid")
	for step := 0; step < 6; step++ {
		d.drive()
		for _, current := range d.run.ValidationInputs {
			if current.Handler != "engine.repair.v4" {
				continue
			}
			if current.Binding.Task == originalTask {
				t.Fatal("deferred correction issued a second repair for the original snapshot")
			}
			if current.Repair == nil {
				t.Fatal("new repair lost its diagnostic contract")
			}
			for _, problem := range current.Repair.Problems {
				if problem.ID == corrected {
					return
				}
			}
			t.Fatal("next-candidate repair omitted the deferred corrected problem")
		}
		advanced := false
		for action, current := range d.run.ValidationInputs {
			switch current.Handler {
			case "engine.gate.v2":
				d.business(action, protocol.ValidationResult{Binding: current.Binding, Status: "PASS"})
				advanced = true
			case "engine.qa.execution.v2":
				report := executionFor(current)
				d.business(action, nativeExecutionResult(current, protocol.ValidationResult{Binding: current.Binding, Status: "PASS", Execution: &report}))
				advanced = true
			}
			if advanced {
				break
			}
		}
		if !advanced {
			break
		}
	}
	t.Fatalf("deferred canonical problem was lost across route/repair/candidate: next=%+v wave=%+v", d.run.Next, d.run.Validation.Waves[len(d.run.Validation.Waves)-1])
}

// A retired snapshot repair cannot absorb a later diagnosis or be recreated by
// a different validation projection. Each new problem needs its own decision.
func TestCorrectionAfterRepairWaiverKeepsSnapshotDutyRetired(t *testing.T) {
	for _, projection := range []string{"current", "route", "qa-retry", "requirement-carry"} {
		t.Run(projection, func(t *testing.T) {
			d, finding, origin := validationWithGateFinding(t)
			resolveGateFinding(d, finding, origin)
			repair, input := d.action("engine.repair.v4")
			originalPlan := *d.run.Validation.Waves[0].Repair
			counted := d.run.Validation.Waves[0].Counted
			old := d.run.Validation.Findings[finding].History[0]
			d.chooseValidation(protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: origin, Obligations: []string{old.ProblemID}, Remedies: []string{"REPAIR", "DISMISS"}}, "DISMISS")
			d.reopen()
			d.drive()
			if d.run.Validation.Waves[0].Status != "COMPLETE" {
				t.Fatal("original waiver did not close original obligations")
			}
			correction := old
			correction.Decision, correction.ProblemID, correction.Supersedes = "NEW", "", 1
			correction.RootCause = "serializer omits the requested ordering before the sorting routine receives values"
			correction.Verification.Causality = "serializer output is unordered while sorting the original values succeeds"
			correction.Reason = "independent inspection corrects the sorting attribution to the serializer"
			event := protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: origin, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{correction}}}}
			d.submit(event)
			d.reopen()
			d.drive()
			problem := d.run.Validation.Findings[finding].History[1].ProblemID
			switch projection {
			case "route":
				unlockRouteByRequirementRedo(d)
				unlockRouteByRequirementRedo(d)
				d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
				d.reopen()
				d.drive()
				id, in := d.gateAction("security")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
			case "qa-retry":
				var target string
				for key, item := range d.run.Validation.Waves[0].Targets {
					if item.Kind == "QA" {
						target = key
					}
				}
				d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Repeat selected QA"}}, Validation: &protocol.ValidationRequest{Kind: "QA_RETRY", Target: origin}}})
				decideValidation(d, d.validationAsk("QA_RETRY"), "approve", target)
				d.reopen()
				d.drive()
				id, in := d.action("engine.qa.execution.v2")
				report := executionFor(in)
				d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report}))
			case "requirement-carry":
				for _, work := range d.run.RequirementWork {
					if work.Task == input.Binding.Task {
						t.Fatal("retired repair reappeared in requirement decisions")
					}
				}
				confirmValidationWork(d, "")
			}
			d.reopen()
			d.drive()
			w := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if w.Status != "ADJUDICATING" {
				t.Fatalf("new unwaived problem lost: %s", w.Status)
			}
			for _, pending := range d.run.PendingActions {
				if pending.Task == input.Binding.Task {
					t.Fatal("same snapshot repair was resurrected")
				}
			}
			if len(d.run.AdapterIntents) != 0 {
				t.Fatal("unmodified retired work generated candidate")
			}
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if before.State.DispatchBindings[repair] != input.Binding {
				t.Fatal("original signed binding changed")
			}
			if d.run.Validation.Waves[0].Counted != counted {
				t.Fatal("original count changed")
			}
			a, _ := json.Marshal(originalPlan)
			b, _ := json.Marshal(d.run.Validation.Waves[0].Repair)
			if !bytes.Equal(a, b) {
				t.Fatal("original repair plan changed")
			}
			invalid := protocol.Event{ID: d.eventID(), Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Choose remedy"}}, Validation: &protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: w.ID, Obligations: []string{problem}, Remedies: []string{"REPAIR", "DISMISS"}}}}
			if _, _, err = d.f.Submit(invalid); err == nil {
				t.Fatal("retired repair offered as current work")
			}
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			a, _ = json.Marshal(before)
			b, _ = json.Marshal(after)
			if !bytes.Equal(a, b) {
				t.Fatal("invalid remedy changed state")
			}
			d.chooseValidation(protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: w.ID, Obligations: []string{problem}, Remedies: []string{"DISMISS", "REQUEST_REQUIREMENT_CHANGE"}}, "DISMISS")
			d.reopen()
			d.drive()
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" || !d.run.NotGuaranteed {
				t.Fatal("new exact waiver did not close current obligations")
			}
			if len(d.run.Validation.Findings[finding].History) != 2 {
				t.Fatal("disposition rewrote diagnosis")
			}
			before, _ = d.f.engine.Load()
			d.drive()
			after, _ = d.f.engine.Load()
			a, _ = json.Marshal(before)
			b, _ = json.Marshal(after)
			if !bytes.Equal(a, b) {
				t.Fatal("settled repeated drive changed facts")
			}
		})
	}
}
