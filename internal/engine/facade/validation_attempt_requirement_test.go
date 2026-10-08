package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"reflect"
	"testing"
)

// The requirement decision covers the unfinished business duty even when its
// last execution is over. CARRY keeps its allowance; REDO replaces the duty.
func TestEndedWorkRequiresRequirementDisposition(t *testing.T) {
	for _, disposition := range []string{"CARRY", "REDO"} {
		for _, reopen := range []bool{false, true} {
			name := disposition + map[bool]string{false: "/normal", true: "/reopen"}[reopen]
			t.Run(name, func(t *testing.T) {
				d := newValidationDriver(t, "quality")
				oldID, original := d.action("engine.development.batch")
				task := original.Binding.Task
				for i := 0; i < 2; i++ {
					id, in := d.action("engine.development.batch")
					oldID = id
					if in.Binding.Task != task {
						t.Fatal("mechanical retry changed duty")
					}
					d.receipt(id)
					d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: string(d.eventID())}})
				}
				if reopen {
					d.reopen()
					d.drive()
				}
				before, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				prior := before.State.EndedAttempts[task]
				evidence, _ := json.Marshal(before.State.RejectedResults)
				matches := 0
				for _, work := range d.run.RequirementWork {
					if work.Task == task {
						matches++
						if work.ActionID != oldID || work.RequirementRevision != original.Binding.RequirementRevision {
							t.Fatalf("work lost original current attempt: %+v", work)
						}
					}
				}
				if matches != 1 {
					t.Fatalf("unfinished exhausted duty must occur once in user's work decision; got %d, work=%+v", matches, d.run.RequirementWork)
				}
				redo := ""
				if disposition == "REDO" {
					redo = oldID
				}
				confirmValidationWork(d, redo)
				if reopen {
					d.reopen()
				}
				d.drive()
				snap, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if disposition == "CARRY" {
					for _, in := range d.run.ValidationInputs {
						if in.Handler == "engine.development.batch" {
							t.Fatal("CARRY reset exhausted duty by issuing new work")
						}
					}
					kept := snap.State.EndedAttempts[task]
					if kept.Attempts != prior.Attempts || kept.MaxAttempts != prior.MaxAttempts || !kept.RetryExhausted {
						t.Fatalf("CARRY changed retry allowance: before=%+v after=%+v", prior, kept)
					}
					ask := d.validationAsk("RETRY")
					if ask.Validation.Target != task.String() {
						t.Fatal("CARRY lost original retry target")
					}
					decideValidation(d, ask, "approve")
					d.drive()
				} else {
					for _, ask := range d.run.PendingAsks {
						if ask.Validation != nil && ask.Validation.Kind == "RETRY" && ask.Validation.Target == task.String() {
							t.Fatal("REDO left prior retry actionable")
						}
					}
					if _, active := snap.State.EndedAttempts[task]; active {
						t.Fatal("REDO kept a parallel retry authority")
					}
				}
				id, in := d.action("engine.development.batch")
				if disposition == "CARRY" && (in.Binding.Task != task || in.Binding.RequirementRevision != original.Binding.RequirementRevision) {
					t.Fatal("CARRY rewrote signed original responsibility")
				}
				if disposition == "REDO" && in.Binding.Task == task {
					t.Fatal("REDO did not replace responsibility")
				}
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "input contract"}})
				d.materialize()
				d.materialize()
				gate, gi := d.action("engine.gate")
				d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
				d.reopen()
				d.drive()
				if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
					t.Fatal("decided duty cannot complete")
				}
				for _, ask := range d.run.PendingAsks {
					if ask.Validation != nil && ask.Validation.Kind == "RETRY" {
						t.Fatalf("completed duty retained actionable retry: %+v", ask)
					}
				}
				after, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(after.State.RejectedResults)
				if !bytes.Equal(evidence, raw) {
					t.Fatal("requirement disposition rewrote original rejected execution evidence")
				}
			})
		}
	}
}

// Failed executions remain decisions about the same duty across both pre-wave
// authoring and frozen validation targets, including a carried invalidated wave.
func TestEndedValidationWorkRetainsItsOwnerAcrossRequirementChange(t *testing.T) {
	for _, role := range []string{"engine.qa.design", "engine.qa.review.rationale", "engine.qa.execution", "engine.gate"} {
		for _, redo := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/redo=%t", role, redo), func(t *testing.T) {
				d := newValidationDriver(t, "blackbox", "quality")
				if role == "engine.qa.execution" || role == "engine.gate" {
					d.reviewReady()
				} else if role == "engine.qa.review.rationale" {
					id, in := d.action("engine.qa.design")
					m := simpleManifest(in)
					d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
				}
				id, original := d.action(role)
				for i := 0; i < 2; i++ {
					id, in := d.action(role)
					if in.Binding != original.Binding {
						t.Fatal("retry changed signed responsibility")
					}
					d.receipt(id)
					d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: string(d.eventID())}})
				}
				snap, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				ended := snap.State.EndedAttempts[original.Binding.Task]
				id = ended.ActionID
				raw, _ := json.Marshal(snap.State.RejectedResults)
				matched := 0
				for _, w := range d.run.RequirementWork {
					if w.Task == original.Binding.Task {
						matched++
						if w.ActionID != id || w.ResultDigest != snap.State.RejectedResults[id].Payload.Digest {
							t.Fatal("requirement package does not bind last execution evidence")
						}
					}
				}
				if matched != 1 {
					t.Fatalf("duty occurs %d times: %+v", matched, d.run.RequirementWork)
				}
				selected := ""
				if redo {
					selected = id
				}
				var candidate coverage.ValidationCandidate
				if role == "engine.qa.execution" || role == "engine.gate" {
					candidate = *d.run.Validation.Candidate
				}
				if role == "engine.qa.execution" && !redo {
					// A different target's REDO invalidates the wave, while this
					// exhausted QA duty is explicitly carried with its old binding.
					selected, _ = d.gateAction("quality")
				}
				confirmValidationWork(d, selected)
				d.reopen()
				d.drive()
				if candidate.Identity != "" && (redo || selected != "") {
					materializeUnchangedCandidate(d, candidate)
				}
				d.drive()
				if !redo {
					ask := d.validationAsk("RETRY")
					if ask.Validation.Target != original.Binding.Task.String() {
						t.Fatal("carry lost exhausted target")
					}
					decideValidation(d, ask, "approve")
				} else {
					if _, present := d.run.EndedAttempts[original.Binding.Task]; present {
						t.Fatal("redo retained ended control")
					}
					for _, ask := range d.run.PendingAsks {
						if ask.Validation != nil && ask.Validation.Target == original.Binding.Task.String() {
							t.Fatal("redo retained actionable retry")
						}
					}
				}
				if redo && role == "engine.qa.review.rationale" {
					design, di := d.action("engine.qa.design")
					m := di.Manifest
					d.business(design, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Manifest: &m})
				}
				id, in := d.action(role)
				if !redo && in.Binding != original.Binding || redo && in.Binding.Task == original.Binding.Task {
					t.Fatal("requirement decision changed the wrong responsibility")
				}
				if !redo {
					a := d.run.Attempts[in.Binding.Task]
					if a.Attempts != 2 || a.MaxAttempts != 3 {
						t.Fatalf("carry reset allowance: %+v", a)
					}
				}
				switch role {
				case "engine.qa.design":
					m := simpleManifest(in)
					d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
				case "engine.qa.review.rationale":
					r := approvedReview(in.Manifest)
					d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r})
				case "engine.qa.execution":
					r := executionFor(in)
					d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &r, Tool: "app-v1", Environment: "isolated"})
				case "engine.gate":
					d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
				}
				if role == "engine.qa.design" {
					rid, ri := d.action("engine.qa.review.rationale")
					r := approvedReview(ri.Manifest)
					d.business(rid, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &r})
				}
				if candidate.Identity == "" {
					dev, di := d.action("engine.development.batch")
					d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "input behavior"}})
					d.materialize()
					d.materialize()
				}
				if role != "engine.qa.execution" {
					qa, qi := d.action("engine.qa.execution")
					r := executionFor(qi)
					d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &r, Tool: "app-v1", Environment: "isolated"})
				}
				if role != "engine.gate" {
					gate, gi := d.gateAction("quality")
					d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
				}
				d.reopen()
				d.drive()
				if w := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]; w.Status != "COMPLETE" {
					t.Fatalf("current wave not complete: %+v", w)
				}
				after, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				got, _ := json.Marshal(after.State.RejectedResults)
				if !bytes.Equal(raw, got) {
					t.Fatal("original rejected payload changed")
				}
				if len(d.run.EndedAttempts) != 0 {
					t.Fatal("completed work retained retry control")
				}
			})
		}
	}
}

func TestRepeatedRequirementCarryKeepsOriginalRetryBinding(t *testing.T) {
	for _, redo := range []bool{false, true} {
		t.Run(fmt.Sprintf("replace=%t", redo), func(t *testing.T) {
			d := newValidationDriver(t, "quality")
			_, original := d.action("engine.development.batch")
			fail := func() string {
				id, in := d.action("engine.development.batch")
				if in.Binding != original.Binding {
					t.Fatal("same duty changed binding")
				}
				d.receipt(id)
				d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: string(d.eventID())}})
				return id
			}
			fail()
			fail()
			confirmValidationWork(d, "")
			d.drive()
			decideValidation(d, d.validationAsk("RETRY"), "approve")
			last := fail()
			for _, w := range d.run.RequirementWork {
				if w.Task == original.Binding.Task && (w.ActionID != last || w.RequirementRevision != original.Binding.RequirementRevision) {
					t.Fatalf("carried retry work changed its signed revision: %+v, binding=%+v", w, original.Binding)
				}
			}
			selected := ""
			if redo {
				selected = last
			}
			confirmValidationDocument(d, selected, nil, validationDocument+"\n\n")
			d.reopen()
			d.drive()
			if redo {
				id, in := d.action("engine.development.batch")
				if in.Binding.Task == original.Binding.Task {
					t.Fatal("redo retained old duty")
				}
				confirmValidationDocument(d, "", nil, validationDocument+"\n\n\n")
				d.reopen()
				d.drive()
				if _, old := d.run.EndedAttempts[original.Binding.Task]; old {
					t.Fatal("later carry revived replaced duty")
				}
				if got := d.run.ValidationInputs[id].Binding; got != in.Binding {
					t.Fatal("replacement carry rewrote signed work")
				}
			} else {
				a := d.run.EndedAttempts[original.Binding.Task]
				if a.Attempts != 3 || a.MaxAttempts != 3 || !a.RetryExhausted {
					t.Fatalf("second carry reset finite allowance: %+v", a)
				}
				for _, option := range d.validationAsk("RETRY").Options {
					if option.ID == "approve" {
						t.Fatal("second carry regranted one-time allowance")
					}
				}
				for _, in := range d.run.ValidationInputs {
					if in.Handler == "engine.development.batch" {
						t.Fatal("exhausted carried work was auto-issued")
					}
				}
			}
		})
	}
}

func TestCarriedRepairRemainsOwedWhileAnotherTargetIsRedone(t *testing.T) {
	for _, thaw := range []bool{false, true} {
		t.Run(fmt.Sprintf("thaw=%t", thaw), func(t *testing.T) {
			d, finding, wave := validationWithGateFinding(t)
			resolveGateFinding(d, finding, wave)
			_, original := d.action("engine.repair")
			for i := 0; i < 2; i++ {
				id, _ := d.action("engine.repair")
				d.receipt(id)
				d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: string(d.eventID())}})
			}
			var redo string
			for _, work := range d.run.RequirementWork {
				if work.Task.Step == "validation.gate" {
					redo = work.ActionID
				}
			}
			if redo == "" {
				t.Fatalf("missing completed gate: %+v", d.run.RequirementWork)
			}
			originalState, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			oldGate := originalState.State.Results[redo]
			confirmValidationWork(d, redo)
			d.reopen()
			d.drive()
			found := false
			for _, work := range d.run.RequirementWork {
				if work.Task == original.Binding.Task {
					found = true
				}
			}
			if !found {
				t.Fatalf("carried repair disappeared while requirement target awaits redo: %+v", d.run.RequirementWork)
			}
			ask := d.validationAsk("RETRY")
			if ask.Validation.Target != original.Binding.Task.String() {
				t.Fatal("carried repair lost retry control")
			}
			if thaw {
				d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
				d.reopen()
				d.drive()
				if _, active := d.run.EndedAttempts[original.Binding.Task]; active {
					t.Fatal("THAW kept carried repair control")
				}
				for _, ask := range d.run.PendingAsks {
					if ask.Validation != nil && ask.Validation.Kind == "RETRY" && ask.Validation.Target == original.Binding.Task.String() {
						t.Fatal("THAW retained carried repair offer")
					}
				}
				for _, in := range d.run.ValidationInputs {
					if in.Handler == "engine.repair" {
						t.Fatal("THAW revived previously carried repair")
					}
				}
				d.action("engine.qa.design")
				return
			}
			if len(d.run.AdapterIntents) != 0 {
				t.Fatal("candidate issued before carried repair finished")
			}
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(before.State.RejectedResults)
			decideValidation(d, ask, "approve")
			id, in := d.action("engine.repair")
			if in.Binding != original.Binding || d.run.Attempts[in.Binding.Task].Attempts != 2 || d.run.Attempts[in.Binding.Task].MaxAttempts != 3 {
				t.Fatal("carried repair changed signed duty or allowance")
			}
			d.business(id, gateRepairResult(in))
			if d.run.Validation.Waves[0].Status != "REPAIRED" {
				t.Fatal("accepted repair did not settle original duty")
			}
			d.materialize()
			d.materialize()
			d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
			gate, gi := d.gateAction("quality")
			if gi.Binding.Task == oldGate.Validation.Binding.Task {
				t.Fatal("carried repair discharged explicitly redone gate")
			}
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
			for i := 0; i < 2; i++ {
				qa, qi := d.action("engine.qa.execution")
				want := coverage.ScopeAffected
				if i == 1 {
					want = coverage.ScopeFull
				}
				if qi.Execution.Scope != want {
					t.Fatal("repair lost affected then full contract")
				}
				r := executionFor(qi)
				d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &r, Tool: "app-v1", Environment: "isolated"})
			}
			d.reopen()
			d.drive()
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatal("carried repair and redone gate did not complete")
			}
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(after.State.RejectedResults)
			if !bytes.Equal(raw, got) {
				t.Fatal("repair carry changed original failure evidence")
			}
			archived := false
			for _, result := range after.State.ObsoleteResults[redo] {
				if reflect.DeepEqual(result, oldGate) {
					archived = true
				}
			}
			if !archived {
				t.Fatal("gate REDO lost original result")
			}
		})
	}
}

func TestRequirementBarrierSettlesFailedAttemptByItsDecision(t *testing.T) {
	for _, redo := range []bool{false, true} {
		t.Run(fmt.Sprintf("redo=%t", redo), func(t *testing.T) {
			d := newValidationDriver(t, "quality")
			first, original := d.action("engine.development.batch")
			d.receipt(first)
			d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: first, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: "first execution interrupted"}})
			id, in := d.action("engine.development.batch")
			d.receipt(id)
			var choices []protocol.RequirementWorkDecision
			for _, w := range d.run.RequirementWork {
				disposition := protocol.RequirementWorkCarry
				if redo && w.Task == original.Binding.Task {
					disposition = protocol.RequirementWorkRedo
				}
				choices = append(choices, protocol.RequirementWorkDecision{Work: w, Disposition: disposition, Reason: "explicitly decide the in-flight duty"})
			}
			document := validationDocument + "\n"
			revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
			pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:formatting", ImpactDigest: "sha256:work-selection", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: choices}
			request, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(request)
			failure := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: "second execution interrupted"}}
			d.submit(failure)
			d.reopen()
			d.drive()
			if d.run.Attempts[in.Binding.Task].Attempts != 1 {
				t.Fatal("held failure was consumed before decision")
			}
			ask := d.run.PendingRequirementAsk
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: choices, Reason: "confirm exact work dispositions"}}})
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
			d.reopen()
			d.drive()
			snap, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if redo {
				if _, old := snap.State.EndedAttempts[original.Binding.Task]; old {
					t.Fatal("held REDO reconstructed old ended duty")
				}
				if len(snap.State.ObsoleteResults[id]) != 1 || snap.State.ObsoleteResults[id][0].PayloadDigest != failure.Result.PayloadDigest {
					t.Fatal("held REDO lost exact original result")
				}
			} else {
				a := snap.State.EndedAttempts[original.Binding.Task]
				if a.Attempts != 2 || a.MaxAttempts != 2 || !a.RetryExhausted {
					t.Fatalf("held CARRY did not consume failure exactly once: %+v", a)
				}
				decideValidation(d, d.validationAsk("RETRY"), "approve")
			}
			dev, di := d.action("engine.development.batch")
			if !redo && di.Binding != original.Binding || redo && di.Binding.Task == original.Binding.Task {
				t.Fatal("barrier decision lost duty identity")
			}
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			d.submit(failure)
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("duplicate held result consumed another attempt")
			}
			d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "input behavior"}})
			d.materialize()
			d.materialize()
			gate, gi := d.gateAction("quality")
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatal("held decision did not complete")
			}
		})
	}
}
