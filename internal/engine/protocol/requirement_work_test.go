package protocol

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

func workChange(t *testing.T, engine *Engine, candidate string, dispositions map[string]string) *RequirementDecisionPayload {
	t.Helper()
	work, err := engine.RequirementWork()
	if err != nil {
		t.Fatal(err)
	}
	payload := &RequirementDecisionPayload{BaseRevision: engine.LoadMustSucceed(t).State.RequirementRevision(), CandidateRevision: candidate, DeltaDigest: "delta:" + candidate, ImpactDigest: "impact:" + candidate, Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged, Reason: "confirm the changed requirement and each affected obligation"}
	for _, item := range work {
		disposition, ok := dispositions[string(item.Task.Step)]
		if !ok {
			t.Fatalf("no explicit disposition for %s", item.Task)
		}
		payload.WorkDecisions = append(payload.WorkDecisions, RequirementWorkDecision{Work: item, Disposition: disposition, Reason: "the selected disposition follows the revised acceptance criteria"})
	}
	return payload
}

func workEngine(t *testing.T, names ...string) (*Engine, string) {
	t.Helper()
	cd, reg := agentStepsDefinition(t, func(string) authoring.AgentSpec {
		return authoring.AgentSpec{Handler: "engine.test.reviewer", Reason: authoring.ReasonSemanticJudgment, Timeout: time.Minute}
	}, names...)
	return reviewEngine(t, cd, reg)
}

func TestRequirementWorkRequiresEveryDispositionBeforeOpeningBarrier(t *testing.T) {
	engine, dir := workEngine(t, "develop", "verify")
	action := issueReady(t, engine)[0]
	submitPlainResult(t, engine, "develop-original", action)
	payload := workChange(t, engine, "rev-2", map[string]string{"develop": RequirementWorkCarry, "verify": RequirementWorkCarry})
	for _, decisions := range [][]RequirementWorkDecision{nil, payload.WorkDecisions[:1]} {
		payload.WorkDecisions = decisions
		request, err := NewRequirementRequestEvent("incomplete-change", payload.packageView(), requirementOptions()...)
		if err != nil {
			t.Fatal(err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(request, fingerprintOf(t, engine)); err == nil || rejectionCode(t, err) != CodeRequirementDecisionRejected {
			t.Fatalf("incomplete work accepted: %v", err)
		}
		if !slices.Equal(before, stateBytesOf(t, dir)) {
			t.Fatal("rejected inventory changed the durable state")
		}
	}
}

func TestRequirementWorkRedoFailureKeepsDependentBlockedAcrossRestart(t *testing.T) {
	engine, dir := workEngine(t, "develop", "verify", "publish")
	original := issueReady(t, engine)[0]
	submitPlainResult(t, engine, "develop-initial", original)
	carried := pendingActionOfStep(t, engine, "verify")
	payload := workChange(t, engine, "rev-2", map[string]string{"develop": RequirementWorkRedo, "verify": RequirementWorkCarry})
	askAndDecideRequirement(t, engine, "redo-develop", "proceed", payload)
	replacement := pendingActionOfStep(t, engine, "develop")
	if replacement == "" || replacement == original {
		t.Fatal("redo did not issue a new obligation")
	}
	submitPlainResult(t, engine, "verify-carried", carried)
	if pendingActionOfStep(t, engine, "publish") != "" {
		t.Fatal("carried descendant concealed unfinished upstream redo")
	}
	if _, err := engine.Submit(spawnReceipt(t, "redo-failed-spawn", replacement, "redo-worker"), fingerprintOf(t, engine)); err != nil {
		t.Fatal(err)
	}
	failed, err := NewWorkerResultEvent("redo-failed", replacement, testProvider, OutcomeFail, "failed-payload", authoring.FailureBusinessReject)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Submit(failed, fingerprintOf(t, engine)); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(mustStore(t, dir), engine.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := restarted.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Next.Kind != decision.KindWait || pendingActionOfStep(t, restarted, "publish") != "" {
		t.Fatalf("terminal FAIL discharged redo: %+v", plan.Next)
	}
	state := restarted.LoadMustSucceed(t).State
	if !state.stepCompleted("develop") || len(state.requiredWorkTasks()) != 1 {
		t.Fatal("historical completion or current obligation was lost")
	}
	next := workChange(t, restarted, "rev-3", map[string]string{"develop": RequirementWorkRedo, "verify": RequirementWorkCarry})
	askAndDecideRequirement(t, restarted, "redo-failed-work", "proceed", next)
	final := pendingActionOfStep(t, restarted, "develop")
	submitPlainResult(t, restarted, "develop-revised", final)
	if pendingActionOfStep(t, restarted, "publish") == "" {
		t.Fatal("accepted fresh PASS did not release the dependent")
	}
	state = restarted.LoadMustSucceed(t).State
	if len(state.ObsoleteResults[original]) != 1 || len(state.ObsoleteResults[replacement]) != 1 || state.ObsoleteResults[original][0].Digest == "" || state.ObsoleteResults[replacement][0].Digest == "" {
		t.Fatal("redo lost previous result evidence")
	}
	if len(state.RequirementRevisions[1].WorkDecisions) != 2 || len(state.RequirementRevisions[2].WorkDecisions) != 2 {
		t.Fatal("exact user decisions not retained")
	}
}

func TestRequirementWorkRedoPreservesEvidenceAtEveryArrivalBoundary(t *testing.T) {
	for _, boundary := range []string{"pending", "staged", "quarantined", "terminal", "completed"} {
		t.Run(boundary, func(t *testing.T) {
			engine, _ := workEngine(t, "develop", "verify")
			old := issueReady(t, engine)[0]
			result, err := NewWorkerResultEvent("old-result", old, testProvider, OutcomePass, "old-payload", "")
			if err != nil {
				t.Fatal(err)
			}
			if boundary == "staged" {
				if _, err := engine.Submit(result, fingerprintOf(t, engine)); err != nil {
					t.Fatal(err)
				}
			}
			if boundary == "terminal" || boundary == "completed" {
				if _, err := engine.Submit(spawnReceipt(t, "old-spawn", old, "old-worker"), fingerprintOf(t, engine)); err != nil {
					t.Fatal(err)
				}
			}
			if boundary == "terminal" {
				pending := engine.LoadMustSucceed(t).State.PendingActions[old]
				task := pending.Task
				for i, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
					event, err := NewTaskEvent(EventID("progress-"+string(rune('a'+i))), task, pending.AttemptID, status)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := engine.Submit(event, fingerprintOf(t, engine)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if boundary == "completed" {
				if _, err := engine.Submit(result, fingerprintOf(t, engine)); err != nil {
					t.Fatal(err)
				}
			}
			payload := workChange(t, engine, "rev-2", map[string]string{"develop": RequirementWorkRedo, "verify": RequirementWorkCarry})
			request, err := NewRequirementRequestEvent("change-work", payload.packageView(), requirementOptions()...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
				t.Fatal(err)
			}
			if boundary == "quarantined" {
				if _, err := engine.Submit(result, fingerprintOf(t, engine)); err != nil {
					t.Fatal(err)
				}
			}
			token, err := engine.Freshness("change-work")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Submit(requirementDecideEvent(t, "change-work", token, "proceed", payload), fingerprintOf(t, engine)); err != nil {
				t.Fatal(err)
			}
			state := engine.LoadMustSucceed(t).State
			if _, ok := state.ObsoleteActions[old]; !ok {
				t.Fatal("old execution still current")
			}
			if boundary == "staged" || boundary == "quarantined" || boundary == "completed" {
				if len(state.ObsoleteResults[old]) != 1 || state.ObsoleteResults[old][0].PayloadDigest != "old-payload" {
					t.Fatal("received raw evidence was discarded")
				}
			}
			if pendingActionOfStep(t, engine, "develop") == "" {
				t.Fatal("replacement obligation was not issued")
			}
			if boundary == "terminal" {
				acceptance, err := engine.Submit(result, fingerprintOf(t, engine))
				if err != nil || acceptance.Status != "OBSOLETE_RESULT" {
					t.Fatalf("late terminal result: %+v %v", acceptance, err)
				}
			}
		})
	}
}

func TestRequirementWorkQueuedRedoCanBeSupersededWhileBarrierIsHeld(t *testing.T) {
	engine, dir := workEngine(t, "develop")
	old := issueReady(t, engine)[0]
	first := workChange(t, engine, "rev-2", map[string]string{"develop": RequirementWorkRedo})
	for _, requestID := range []string{"change-first", "change-held"} {
		request, err := NewRequirementRequestEvent(EventID(requestID), first.packageView(), requirementOptions()...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
			t.Fatal(err)
		}
	}
	token, err := engine.Freshness("change-first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Submit(requirementDecideEvent(t, "change-first", token, "proceed", first), fingerprintOf(t, engine)); err != nil {
		t.Fatal(err)
	}
	if pendingActionOfStep(t, engine, "develop") != "" {
		t.Fatal("replacement issued through a remaining change barrier")
	}
	restarted, err := New(mustStore(t, dir), engine.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := restarted.RequirementWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory) != 1 || inventory[0].IssuedRevision != 0 || inventory[0].RequirementRevision != "rev-2" || inventory[0].ActionID == old {
		t.Fatalf("queued redo inventory: %+v", inventory)
	}
	queued := inventory[0]
	next := workChange(t, restarted, "rev-3", map[string]string{"develop": RequirementWorkRedo})
	askAndDecideRequirement(t, restarted, "change-queued", "proceed", next)
	if pendingActionOfStep(t, restarted, "develop") != "" {
		t.Fatal("second redo issued while held Ask remained")
	}
	abortExistingAsk(t, restarted, "change-held")
	replacement := pendingActionOfStep(t, restarted, "develop")
	if replacement == "" || replacement == old || replacement == queued.ActionID {
		t.Fatalf("wrong final obligation issued: %s", replacement)
	}
	state := restarted.LoadMustSucceed(t).State
	if state.Tasks[queued.Task] != runtime.TaskTerminal {
		t.Fatal("superseded queued task remains schedulable")
	}
	if len(state.requiredWorkTasks()) != 1 {
		t.Fatal("superseding redo duplicated work obligations")
	}
	submitPlainResult(t, restarted, "final-queued-work", replacement)
	plan, _, err := restarted.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Next.Kind != decision.KindComplete {
		t.Fatalf("fresh PASS failed to discharge work: %+v", plan.Next)
	}
}

func TestRequirementWorkIncludesIndependentGateReview(t *testing.T) {
	cd, reg := agentStepsDefinition(t, func(string) authoring.AgentSpec {
		return authoring.AgentSpec{Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview, ReviewSeries: authoring.ReviewSeriesUnaffiliated, Timeout: time.Minute}
	}, "gate.check")
	engine, _ := reviewEngine(t, cd, reg)
	old := issueReady(t, engine)[0]
	payload := workChange(t, engine, "rev-2", map[string]string{"gate.check": RequirementWorkRedo})
	if len(payload.WorkDecisions) != 1 {
		t.Fatal("independent gate missing from impact decisions")
	}
	askAndDecideRequirement(t, engine, "redo-gate", "proceed", payload)
	replacement := pendingActionOfStep(t, engine, "gate.check")
	if replacement == "" || replacement == old {
		t.Fatal("independent review gate was not replaced")
	}
	submitPlainResult(t, engine, "gate-result", replacement)
	if len(engine.LoadMustSucceed(t).State.requiredWorkTasks()) != 0 {
		t.Fatal("gate result did not fulfill revised obligation")
	}
}

func TestRequirementWorkArchivesAllQuarantinedResultsDeterministically(t *testing.T) {
	var archived []string
	for _, reverse := range []bool{false, true} {
		engine, dir := workEngine(t, "develop")
		action := issueReady(t, engine)[0]
		payload := workChange(t, engine, "rev-2", map[string]string{"develop": RequirementWorkRedo})
		request, err := NewRequirementRequestEvent("change-work", payload.packageView(), requirementOptions()...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
			t.Fatal(err)
		}
		transient, err := NewWorkerResultEvent("transient-result", action, testProvider, OutcomeRuntimeError, "transient-payload", authoring.FailureTransientEngine)
		if err != nil {
			t.Fatal(err)
		}
		final, err := NewWorkerResultEvent("final-result", action, testProvider, OutcomePass, "final-payload", "")
		if err != nil {
			t.Fatal(err)
		}
		results := []Event{transient, final}
		if reverse {
			slices.Reverse(results)
		}
		for _, event := range results {
			if _, err := engine.Submit(event, fingerprintOf(t, engine)); err != nil {
				t.Fatal(err)
			}
		}
		token, err := engine.Freshness("change-work")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Submit(requirementDecideEvent(t, "change-work", token, "proceed", payload), fingerprintOf(t, engine)); err != nil {
			t.Fatal(err)
		}
		history := engine.LoadMustSucceed(t).State.ObsoleteResults[action]
		if len(history) != 2 {
			t.Fatalf("archive lost observed results: %+v", history)
		}
		digests := []string{history[0].Digest, history[1].Digest}
		want := []string{resultPayloadDigest(transient.Result), resultPayloadDigest(final.Result)}
		slices.Sort(want)
		if !slices.Equal(digests, want) {
			t.Fatalf("archived content differs: %v want %v", digests, want)
		}
		if reverse && !slices.Equal(archived, digests) {
			t.Fatal("archive order depends on arrival order")
		}
		archived = digests
		replay := transient
		replay.ID = "late-same-result"
		if acceptance, err := engine.Submit(replay, fingerprintOf(t, engine)); err != nil || acceptance.Status != "OBSOLETE_RESULT" {
			t.Fatalf("known obsolete evidence replay: %+v %v", acceptance, err)
		}
		if len(engine.LoadMustSucceed(t).State.ObsoleteResults[action]) != 2 {
			t.Fatal("duplicate payload was archived again")
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(replay, fingerprintOf(t, engine)); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(before, stateBytesOf(t, dir)) {
			t.Fatal("same-event obsolete replay changed state")
		}

		conflict, err := NewWorkerResultEvent("late-conflict", action, testProvider, OutcomePass, "different-final-payload", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Submit(conflict, fingerprintOf(t, engine)); err == nil || rejectionCode(t, err) != CodeReceiptConflict {
			t.Fatalf("conflicting late evidence accepted: %v", err)
		}
	}
}

func TestObsoleteResultHistoryReadsBothSupportedFormats(t *testing.T) {
	record := WorkerResult{ActionID: "dispatch", Provider: testProvider, Outcome: OutcomePass, PayloadDigest: "payload", Digest: "result-digest"}
	for _, value := range []any{record, []WorkerResult{record}} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var history WorkerResultHistory
		if err := json.Unmarshal(encoded, &history); err != nil {
			t.Fatal(err)
		}
		if len(history) != 1 || history[0].Digest != record.Digest || history[0].PayloadDigest != record.PayloadDigest {
			t.Fatalf("result evidence changed on load: %+v", history)
		}
		written, err := json.Marshal(history)
		if err != nil {
			t.Fatal(err)
		}
		if len(written) == 0 || written[0] != '[' {
			t.Fatalf("new result history is not an array: %s", written)
		}
	}
}
