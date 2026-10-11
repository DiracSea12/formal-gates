package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"slices"
	"testing"
)

func TestCompletedRepairRedoPreservesOriginalEvidenceAndReplacesCandidateSource(t *testing.T) {
	d := plannedValidationDriver(t, nil, commandPlan(), "quality")
	dev, di := planBatchAction(d, "command")
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: batchEvidence(di)})
	d.materialize()
	d.materialize()
	gate, gi := d.gateAction("quality")
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P1", SeverityRationale: "Supported success path is unusable", Requirement: "AC-001", NormalPath: "app run valid", ActualError: "valid input exits nonzero", DeferralImpact: "command cannot be used", Locations: []string{"app.go:1"}, Message: "Command rejects supported input"}}})
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	var finding string
	for id, f := range d.run.Validation.Findings {
		if f.WaveID == wave.ID {
			finding = id
		}
	}
	resolution := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run valid", Module: "command", RootCause: "valid input rejected", Consequence: "public success path unusable", Severity: "P1", SeverityRationale: "Supported command cannot complete", Verification: review.FindingVerification{Scope: "confirmed command contract", Severity: "main path unusable", Causality: "input dispatch branch", Reproduction: "valid input rejected", Classification: "implementation"}, Reason: "Reproduced public input rejection"}
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
	d.drive()
	repair, ri := d.action("engine.repair.v2")
	d.business(repair, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "Correct dispatch"}})
	d.materialize()
	d.materialize()
	gate, gi = d.gateAction("quality")
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	d.drive()
	d.reopen()
	passedGate := gate
	passedInput := gi
	passedResult, _ := json.Marshal(driverState(t, d).Results[passedGate])
	originalWave, _ := json.Marshal(d.run.Validation.Waves[0])
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	originalResult, _ := json.Marshal(snapshot.State.Results[repair])
	found := false
	for _, w := range d.run.RequirementWork {
		if w.ActionID == repair {
			found = true
		}
	}
	if !found {
		t.Fatal("completed repair missing from explicit requirement decisions")
	}
	confirmValidationWork(d, repair)
	d.reopen()
	d.drive()
	for _, intent := range d.run.AdapterIntents {
		data, _ := json.Marshal(intent.Adapter.Params)
		var params protocol.CandidateParameters
		if err := json.Unmarshal(data, &params); err != nil {
			t.Fatal(err)
		}
		for _, source := range params.SourceTasks {
			if source == ri.Binding.Task {
				t.Fatalf("explicitly REDO completed repair still sources candidate: stage=%s source=%s base=%s", params.Stage, source, params.Base)
			}
		}
	}
	if len(d.run.AdapterIntents) > 0 {
		for _, intent := range d.run.AdapterIntents {
			data, _ := json.Marshal(intent.Adapter.Params)
			var params protocol.CandidateParameters
			json.Unmarshal(data, &params)
			if params.Stage != "DEVELOPMENT" {
				t.Fatal("final candidate prepared before required repair replacement")
			}
		}
		d.materialize()
		d.drive()
	}
	next, nextInput := d.action("engine.repair.v2")
	if nextInput.Binding.Task == ri.Binding.Task {
		t.Fatal("replacement reused retired repair task")
	}
	d.business(next, protocol.ValidationResult{Binding: nextInput.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "Reimplement confirmed repair responsibility"}})
	seen := false
	for _, intent := range d.run.AdapterIntents {
		data, _ := json.Marshal(intent.Adapter.Params)
		var params protocol.CandidateParameters
		json.Unmarshal(data, &params)
		for _, task := range params.SourceTasks {
			if task == ri.Binding.Task {
				t.Fatal("retired repair reentered final source set")
			}
			if task == nextInput.Binding.Task {
				seen = true
			}
		}
	}
	if !seen {
		t.Fatal("completed replacement missing from actual candidate source set")
	}
	d.materialize()
	d.materialize()
	d.reopen()
	d.drive()
	state := driverState(t, d)
	carried := false
	for _, decision := range state.RequirementRevisions[len(state.RequirementRevisions)-1].WorkDecisions {
		if decision.Work.ActionID == passedGate && decision.Disposition == protocol.RequirementWorkCarry {
			carried = true
		}
	}
	currentResult, _ := json.Marshal(state.Results[passedGate])
	if !carried || !bytes.Equal(passedResult, currentResult) {
		t.Fatal("confirmed gate carry changed its original result or lost explicit authority")
	}
	currentWave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if nextInput.Binding.PureP2 || currentWave.Candidate.Identity == passedInput.Binding.Candidate || !slices.Contains(currentWave.SourceTasks, nextInput.Binding.Task) {
		t.Fatal("P1 replacement did not establish its new candidate and source responsibility")
	}
	gate, gi = d.gateAction("quality")
	if gate == passedGate || gi.Binding.Candidate != currentWave.Candidate.Identity || gi.Binding.Task == passedInput.Binding.Task {
		t.Fatal("new P1 candidate reused the original gate input")
	}
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	d.drive()
	for _, target := range d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Targets {
		if target.Gate == "quality" && (target.Status != "PASS" || target.ResultAction != gate) {
			t.Fatal("final candidate lost its accepted gate responsibility")
		}
	}
	if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
		t.Fatal("replacement repair cannot finish final validation")
	}
	retained, _ := json.Marshal(d.run.Validation.Waves[0])
	if !bytes.Equal(originalWave, retained) {
		t.Fatal("redo rewrote the original completed repair wave")
	}
	snapshot, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	historyMatched := false
	for _, record := range snapshot.State.ObsoleteResults[repair] {
		retained, _ = json.Marshal(record)
		if bytes.Equal(originalResult, retained) {
			historyMatched = true
		}
	}
	if !historyMatched {
		t.Fatal("redo failed to retain exact original repair result")
	}
}

func TestMultipleRepairRedosKeepCausalOrderAndActualBases(t *testing.T) {
	for _, scenario := range []string{"normal", "reopen", "inflight", "mixed-severity"} {
		t.Run(scenario, func(t *testing.T) {
			reopen := scenario == "reopen"
			inflight := scenario == "inflight"
			d := plannedValidationDriver(t, nil, commandPlan(), "quality")
			id, in := planBatchAction(d, "command")
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: batchEvidence(in)})
			d.materialize()
			d.materialize()
			var actions []string
			var oldInputs []protocol.ValidationInput
			for n := 0; n < 2; n++ {
				gate, gi := d.gateAction("quality")
				findingEvidence := review.Finding{Severity: "P2", Message: "documented output order differs"}
				if scenario == "mixed-severity" && n == 0 {
					findingEvidence = review.Finding{Severity: "P1", SeverityRationale: "supported success path cannot complete", Requirement: "AC-001", NormalPath: "app run valid", ActualError: "valid input exits nonzero", DeferralImpact: "command unusable", Locations: []string{"app.go:1"}, Message: "valid input rejected"}
				}
				d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "FAIL", Findings: []review.Finding{findingEvidence}})
				wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				var finding string
				for key, f := range d.run.Validation.Findings {
					if f.WaveID == wave.ID {
						finding = key
					}
				}
				if scenario == "mixed-severity" && n == 0 {
					resolution := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run valid", Module: "input", RootCause: "input dispatch branch", Consequence: "success input rejected", Severity: "P1", SeverityRationale: "public command unusable", Verification: review.FindingVerification{Scope: "command contract", Severity: "supported path blocked", Causality: "dispatch branch", Reproduction: "valid input exits nonzero", Classification: "implementation"}, Reason: "reproduced command failure"}
					d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
				} else {
					resolveGateFinding(d, finding, wave.ID)
				}
				d.drive()
				repair, ri := d.action("engine.repair.v2")
				actions = append(actions, repair)
				oldInputs = append(oldInputs, ri)
				if n == 1 && inflight {
					break
				}
				edit := protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "apply exact output correction"}}
				d.business(repair, edit)
				if ri.Binding.PureP2 {
					d.verifyRepair(edit)
				}
				d.materialize()
				d.materialize()
				// Pure P2's successful carry may complete without a new gate. Requesting a
				// requirement gate REDO supplies the next independently observed obligation.
				if n == 0 {
					var gateAction string
					for _, w := range d.run.RequirementWork {
						if w.Task.Step == gi.Binding.Task.Step {
							gateAction = w.ActionID
						}
					}
					if scenario == "mixed-severity" {
						gateAction, _ = d.gateAction("quality")
					}
					confirmValidationWork(d, gateAction)
					d.drive()
					for len(d.run.AdapterIntents) > 0 {
						d.materialize()
					}
				}
			}
			document := validationDocument + "\n\n"
			revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
			var choices []protocol.RequirementWorkDecision
			for _, w := range d.run.RequirementWork {
				disposition := protocol.RequirementWorkCarry
				if slices.Contains(actions, w.ActionID) && !(inflight && w.ActionID == actions[1]) {
					disposition = protocol.RequirementWorkRedo
				}
				choices = append(choices, protocol.RequirementWorkDecision{Work: w, Disposition: disposition, Reason: "explicitly repeat both original corrections"})
			}
			pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "formatting", ImpactDigest: "two original corrections", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: choices}
			request, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(request)
			ask := d.run.PendingRequirementAsk
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: choices, Reason: "confirm both obligations"}}})
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
			if reopen {
				d.reopen()
			}
			if inflight {
				id, in := d.action("engine.repair.v2")
				if in.Binding != oldInputs[1].Binding {
					t.Fatal("pending carried repair lost signed input")
				}
				edit := protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "finish carried correction"}}
				d.business(id, edit)
				if in.Binding.PureP2 {
					d.verifyRepair(edit)
				}
			}
			d.materialize()
			var replacements []protocol.WorkBinding
			countRedos := 2
			if inflight {
				countRedos = 1
			}
			for n := 0; n < countRedos; n++ {
				repair, ri := d.action("engine.repair.v2")
				if ri.Binding.Task == oldInputs[n].Binding.Task || ri.Binding.WaveID != oldInputs[n].Binding.WaveID || ri.Binding.Candidate != d.run.Validation.DevelopmentSnapshot {
					t.Fatal("repair lost original order or actual materialized base")
				}
				if ri.Repair == nil || len(ri.Repair.Findings) == 0 || len(ri.Repair.Problems) == 0 || ri.Repair.Obligation.Task != ri.Binding.Task {
					t.Fatal("repair input requires hidden problem lookup")
				}
				count := 0
				for _, input := range d.run.ValidationInputs {
					if input.Handler == "engine.repair.v2" {
						count++
					}
				}
				if count != 1 {
					t.Fatal("multiple same-workspace repairs dispatched")
				}
				replacements = append(replacements, ri.Binding)
				edit := protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "redo original correction on actual current production base"}}
				d.business(repair, edit)
				if ri.Binding.PureP2 {
					d.verifyRepair(edit)
				}
				if reopen {
					d.reopen()
				}
				d.materialize()
			}
			if len(replacements) == 2 && (replacements[0].Task == replacements[1].Task || replacements[0].Candidate == replacements[1].Candidate) {
				t.Fatal("redo duties or their actual bases were folded")
			}
			var issued protocol.CandidateParameters
			var finalIntent protocol.HostActionIntent
			for _, intent := range d.run.AdapterIntents {
				if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
					continue
				}
				var params protocol.CandidateParameters
				if err := decodeNativeParams(intent, &params); err != nil {
					t.Fatal(err)
				}
				if params.Stage == "REVIEW_READY" {
					if finalIntent.ActionID != "" {
						t.Fatal("multiple final candidate writers")
					}
					finalIntent, issued = intent, params
				}
			}
			if finalIntent.ActionID == "" {
				t.Fatal("missing final candidate intent")
			}
			d.fixtureCandidate(finalIntent.ActionID, finalIntent, issued)
			d.drive()
			frozen := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			state := driverState(t, d)
			receipt := state.HostActionReceipts[finalIntent.ActionID]
			var evidence protocol.CandidateEvidence
			if receipt.AdapterEvidence == nil {
				t.Fatal("final candidate has no original native evidence")
			}
			data, _ := json.Marshal(receipt.AdapterEvidence.Values)
			if err := json.Unmarshal(data, &evidence); err != nil {
				t.Fatal(err)
			}
			if receipt.Status != protocol.HostActionStatusExecuted || receipt.PayloadDigest != finalIntent.PayloadDigest || state.ReconciledEffects[finalIntent.ActionID].ObservationDigest != receipt.Digest || evidence.ParametersDigest != finalIntent.PayloadDigest || evidence.Identity != frozen.Candidate.Identity || evidence.Digest != frozen.Candidate.Digest {
				t.Fatal("final candidate lost its exact signed effect and accepted receipt")
			}
			if !slices.Equal(frozen.SourceTasks, issued.SourceTasks) {
				t.Fatal("wave lost exact accepted candidate sources")
			}
			for _, binding := range replacements {
				if !slices.Contains(frozen.SourceTasks, binding.Task) {
					t.Fatal("candidate omitted actual replacement source")
				}
			}
			if scenario == "mixed-severity" {
				gate, gi := d.gateAction("quality")
				d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
				if !d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Counted {
					t.Fatal("mixed P1/P2 redo was classified solely by the last P2 repair")
				}
				return
			}
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Counted {
				t.Fatal("pure P2 replacement consumed semantic repair round")
			}
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatalf("precise repaired gate carry did not complete: %+v targets=%+v", d.run.Validation.Waves[len(d.run.Validation.Waves)-1], d.run.ValidationInputs)
			}
		})
	}
}
