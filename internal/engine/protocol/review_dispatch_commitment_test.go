// review_dispatch_commitment_test.go verifies that issued review work stays
// recordable across terminal task reports, receipt reconciliation, staging and
// requirement barriers. Each series has at most one unsettled dispatch.
package protocol

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

func unsettledDispatchIDs(st *State) []string {
	ids := make([]string, 0, len(st.PendingActions)+len(st.SpawnReceipts))
	for id := range st.PendingActions {
		ids = append(ids, id)
	}
	for id, receipt := range st.SpawnReceipts {
		if _, pending := st.PendingActions[id]; pending {
			continue
		}
		if receipt.Status != SpawnStatusSpawned && receipt.Status != SpawnStatusAttached && receipt.Status != SpawnStatusUnknown {
			continue
		}
		if _, recorded := st.Results[id]; recorded {
			continue
		}
		if _, retired := st.ObsoleteActions[id]; retired {
			continue
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func seriesOfDispatch(st *State, engine *Engine, dispatchID string) review.Action {
	if pending, ok := st.PendingActions[dispatchID]; ok && pending.ReviewSeries != "" {
		return pending.ReviewSeries
	}
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		series, _ := st.ReviewSeries.Series(action)
		if review.AuthorizationOfDispatch(series, dispatchID) != "" {
			return action
		}
	}
	return engine.reviewSeriesOf(st.SpawnReceipts[dispatchID].Task.Step)
}

func seriesCommitmentViolations(t *testing.T, engine *Engine) []string {
	t.Helper()
	state := engine.LoadMustSucceed(t).State
	live := unsettledDispatchIDs(state)
	var violations []string
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		series, _ := state.ReviewSeries.Series(action)
		authorizationOf := map[string]string{}
		for _, authorization := range series.Authorizations {
			for _, dispatchID := range authorization.DispatchIDs {
				if other, duplicate := authorizationOf[dispatchID]; duplicate {
					violations = append(violations, fmt.Sprintf("dispatch %s is bound more than once: %s and %s", dispatchID, other, authorization.ID))
				}
				authorizationOf[dispatchID] = authorization.ID
			}
		}
		occupied := review.OccupiedAuthorizationIDs(series)
		liveInSeries := 0
		for _, dispatchID := range live {
			if seriesOfDispatch(state, engine, dispatchID) != action {
				continue
			}
			liveInSeries++
			authorizationID := authorizationOf[dispatchID]
			if series.Completed >= series.AutomaticLimit && authorizationID == "" {
				violations = append(violations, fmt.Sprintf("over-limit dispatch %s has no authorization", dispatchID))
			}
			if authorizationID != "" && occupied[authorizationID] {
				violations = append(violations, fmt.Sprintf("live dispatch %s holds consumed authorization %s", dispatchID, authorizationID))
			}
		}
		if liveInSeries > 1 {
			violations = append(violations, fmt.Sprintf("series %s has %d live dispatches for one review obligation", action, liveInSeries))
		}
	}
	return violations
}

func assertSeriesCommitmentHeld(t *testing.T, engine *Engine) {
	t.Helper()
	if violations := seriesCommitmentViolations(t, engine); len(violations) != 0 {
		t.Fatalf("the series commitment invariant is torn:\n%s", strings.Join(violations, "\n"))
	}
}

func terminalizeDispatchTask(t *testing.T, engine *Engine, actionID string) {
	t.Helper()
	pending, known := engine.LoadMustSucceed(t).State.PendingActions[actionID]
	if !known {
		t.Fatalf("cannot terminalize %s: not in flight", actionID)
	}
	for i, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
		ev, err := NewTaskEvent(EventID(fmt.Sprintf("progress-%s-%d", actionID, i)), pending.Task, pending.AttemptID, status)
		if err != nil {
			t.Fatalf("task event: %v", err)
		}
		if _, err := engine.Submit(ev, fingerprintOf(t, engine)); err != nil {
			t.Fatalf("task progress %s of %s: %v", status, actionID, err)
		}
	}
	if _, still := engine.LoadMustSucceed(t).State.PendingActions[actionID]; still {
		t.Fatalf("terminal task still has dispatch bookkeeping: %s", actionID)
	}
}

func submitSpawnReceipt(t *testing.T, engine *Engine, event, actionID string) {
	t.Helper()
	if _, err := engine.Submit(spawnReceipt(t, EventID(event), actionID, "agent-"+event), fingerprintOf(t, engine)); err != nil {
		t.Fatalf("spawn receipt %s: %v", event, err)
	}
}

func blockingReviewEvidence(action review.Action, dispatchID, message string) ReviewResultEvidence {
	return reviewEvidence(action, dispatchID, "", "FAIL", message, []review.Finding{
		{Severity: "P1", Message: "unconfirmed blocker keeps the series open"},
	})
}

func submitLateDispatchResult(t *testing.T, engine *Engine, event, actionID string, evidence ReviewResultEvidence) error {
	t.Helper()
	evidence.DispatchID = actionID
	ev, err := NewReviewWorkerResultEvent(EventID(event), actionID, testProvider, "sha256:payload-"+event, evidence)
	if err != nil {
		t.Fatalf("late result event: %v", err)
	}
	_, err = engine.Submit(ev, fingerprintOf(t, engine))
	return err
}

func submitUnknownReceipt(t *testing.T, engine *Engine, event, actionID, correlation string) {
	t.Helper()
	if _, err := engine.Submit(unknownSpawnReceipt(t, EventID(event), actionID, correlation), fingerprintOf(t, engine)); err != nil {
		t.Fatalf("unknown receipt %s: %v", event, err)
	}
}

func attachUnknownReceipt(t *testing.T, engine *Engine, actionID, correlation, identity string) {
	t.Helper()
	for i, name := range []string{LifecycleStart, LifecycleStop} {
		ev, err := NewCorrelatedLifecycleEvent(EventID(fmt.Sprintf("cycle-%s-%d", actionID, i)), testProvider, correlation, identity, name)
		if err != nil {
			t.Fatalf("lifecycle event: %v", err)
		}
		if _, err := engine.Submit(ev, fingerprintOf(t, engine)); err != nil {
			t.Fatalf("lifecycle %s: %v", name, err)
		}
	}
	plan, _, err := engine.ReconcileUnknownReceipt(actionID, fingerprintOf(t, engine))
	if err != nil || plan.Action != RecoveryAttachReceipt {
		t.Fatalf("reconcile %s = %+v err=%v", actionID, plan, err)
	}
	if receipt := engine.LoadMustSucceed(t).State.SpawnReceipts[actionID]; receipt.Status != SpawnStatusAttached {
		t.Fatalf("receipt %s = %q, want ATTACHED", actionID, receipt.Status)
	}
}

func submitFailureResult(t *testing.T, engine *Engine, event, actionID string) (Acceptance, error) {
	t.Helper()
	ev, err := NewWorkerResultEvent(EventID(event), actionID, testProvider, OutcomeFail, "sha256:payload-"+event, authoring.FailureBusinessReject)
	if err != nil {
		t.Fatalf("failure event: %v", err)
	}
	acceptance, err := engine.Submit(ev, fingerprintOf(t, engine))
	return acceptance, err
}

func TestDispatchPromiseMatchesResultRecordabilityAcrossReceiptFaces(t *testing.T) {
	submitTransientFailureReceipt := func(t *testing.T, engine *Engine, lane string) {
		t.Helper()
		ev, err := NewSpawnReceiptWithFailureClass(EventID("spawn-face-failed"), lane, testProvider, "agent-face", SpawnStatusFailed, authoring.FailureTransientEngine)
		if err != nil {
			t.Fatalf("failed receipt event: %v", err)
		}
		if _, err := engine.Submit(ev, fingerprintOf(t, engine)); err != nil {
			t.Fatalf("failed receipt submit: %v", err)
		}
	}
	// pending 列的构造在 terminal 列复用：先落回执面，再按需 terminal 化。
	rows := []struct {
		name     string
		terminal bool
		counted  bool
		outcome  string // 失败结果探针的期态：recorded / staged / rejected
		prepare  func(t *testing.T, engine *Engine, lane string)
	}{
		{"pending without a receipt", false, true, "staged", func(t *testing.T, engine *Engine, lane string) {}},
		{"pending with a SPAWNED receipt", false, true, "recorded", func(t *testing.T, engine *Engine, lane string) {
			submitSpawnReceipt(t, engine, "spawn-face", lane)
		}},
		{"pending with an ATTACHED receipt", false, true, "recorded", func(t *testing.T, engine *Engine, lane string) {
			submitUnknownReceipt(t, engine, "spawn-face", lane, "corr-face")
			attachUnknownReceipt(t, engine, lane, "corr-face", "worker-face")
		}},
		{"pending with an UNKNOWN receipt", false, true, "staged", func(t *testing.T, engine *Engine, lane string) {
			submitUnknownReceipt(t, engine, "spawn-face", lane, "corr-face")
		}},
		{"pending with a FAILED receipt", false, true, "staged", func(t *testing.T, engine *Engine, lane string) {
			submitTransientFailureReceipt(t, engine, lane)
		}},
		{"terminal without a receipt", true, false, "rejected", func(t *testing.T, engine *Engine, lane string) {}},
		{"terminal with a SPAWNED receipt", true, true, "recorded", func(t *testing.T, engine *Engine, lane string) {
			submitSpawnReceipt(t, engine, "spawn-face", lane)
		}},
		{"terminal with an ATTACHED receipt", true, true, "recorded", func(t *testing.T, engine *Engine, lane string) {
			submitUnknownReceipt(t, engine, "spawn-face", lane, "corr-face")
			attachUnknownReceipt(t, engine, lane, "corr-face", "worker-face")
		}},
		{"terminal with an UNKNOWN receipt", true, true, "recorded", func(t *testing.T, engine *Engine, lane string) {
			submitUnknownReceipt(t, engine, "spawn-face", lane, "corr-face")
		}},
		{"terminal with a FAILED receipt", true, false, "rejected", func(t *testing.T, engine *Engine, lane string) {
			submitTransientFailureReceipt(t, engine, lane)
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			cd, reg := productReviewDAGFixture(t, dagStep{name: "lane.a", deps: []string{"prep.parse"}})
			engine, _ := reviewEngine(t, cd, reg)
			lanes := issueReady(t, engine)
			if len(lanes) != 1 {
				t.Fatalf("single-lane fixture issued %d dispatches: %v", len(lanes), lanes)
			}
			lane := lanes[0]
			row.prepare(t, engine, lane)
			if row.terminal {
				terminalizeDispatchTask(t, engine, lane)
			}
			assertCounted := func(want bool) {
				t.Helper()
				st := engine.LoadMustSucceed(t).State
				if got := slices.Contains(unsettledDispatchIDs(st), lane); got != want {
					t.Fatalf("unsettled-promise membership = %v, want %v", got, want)
				}
			}
			assertCounted(row.counted)

			acceptance, err := submitFailureResult(t, engine, "fail-face", lane)
			switch row.outcome {
			case "recorded":
				if err != nil {
					t.Fatalf("the failure-result exit must settle the counted dispatch: %v", err)
				}
				if _, recorded := engine.LoadMustSucceed(t).State.Results[lane]; !recorded {
					t.Fatal("the failure result must be recorded in Results")
				}
				assertCounted(false)
			case "staged":
				if err != nil || acceptance.Status != "STAGED" {
					t.Fatalf("the pending dispatch must hold its result in staging: %+v err=%v", acceptance, err)
				}
				assertCounted(true)
			case "rejected":
				if err == nil || rejectionCode(t, err) != CodeUnknownAction {
					t.Fatalf("an uncounted dispatch must reject the late result: %v", err)
				}
				assertCounted(false)
			}
			assertSeriesCommitmentHeld(t, engine)
		})
	}
}

func TestUndefinedReceiptStatusStaysOutOfPromiseAndExits(t *testing.T) {
	if issuedWitnessReceipt("SOME_UNDEFINED_STATUS") {
		t.Fatal("an undefined receipt status must not prove the dispatch was issued")
	}
	if inflightWitnessReceipt("SOME_UNDEFINED_STATUS") {
		t.Fatal("an undefined receipt status must not count as an in-flight witness")
	}
}

func TestTerminalReviewDispatchRemainsRecordableAcrossReceipts(t *testing.T) {
	for _, receipt := range []string{"SPAWNED", "ATTACHED", "UNKNOWN"} {
		for _, failure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failure=%t", receipt, failure), func(t *testing.T) {
				cd, reg := productReviewDAGFixture(t,
					dagStep{name: "review.primary", deps: []string{"prep.parse"}},
					dagStep{name: "review.spare", deps: []string{"prep.parse"}},
				)
				engine, _ := reviewEngine(t, cd, reg)
				issued := issueReady(t, engine)
				if len(issued) != 1 {
					t.Fatalf("one owed review must issue exactly once: %v", issued)
				}
				action := issued[0]
				if slot := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, action); slot != "" {
					t.Fatalf("initial automatic review must not consume extra authorization: %s", slot)
				}
				if receipt == "SPAWNED" {
					submitSpawnReceipt(t, engine, "spawn-review", action)
				} else {
					submitUnknownReceipt(t, engine, "spawn-review", action, "correlation-review")
				}
				terminalizeDispatchTask(t, engine, action)
				if receipt == "ATTACHED" {
					attachUnknownReceipt(t, engine, action, "correlation-review", "worker-review")
				}
				assertSeriesCommitmentHeld(t, engine)
				assertReviewIssuanceHeld(t, engine)
				if !slices.Contains(unsettledDispatchIDs(engine.LoadMustSucceed(t).State), action) {
					t.Fatal("the terminal dispatch must retain its result obligation")
				}
				if failure {
					if _, err := submitFailureResult(t, engine, "failed-review", action); err != nil {
						t.Fatalf("terminal failure result: %v", err)
					}
					snap := engine.LoadMustSucceed(t)
					result, ok := snap.State.Results[action]
					if !ok || result.Outcome != OutcomeFail || result.Review != nil || result.PayloadDigest != "sha256:payload-failed-review" {
						t.Fatalf("failure evidence was not preserved: %+v", result)
					}
					if series := snap.State.ReviewSeries.ProductReview; series.Completed != 0 || len(series.RawCandidates) != 0 || containsStep(snap.State.Completed, "review.primary") {
						t.Fatalf("execution failure must not complete a semantic review: %+v", series)
					}
					replacement := pendingActionID(t, engine)
					if replacement == action || len(snap.State.PendingActions) != 1 {
						t.Fatalf("only the unsettled review obligation may be retried: %+v", snap.State.PendingActions)
					}
					if slot := review.AuthorizationOfDispatch(snap.State.ReviewSeries.ProductReview, replacement); slot != "" {
						t.Fatalf("retry under automatic capacity must not consume a slot: %s", slot)
					}
					submitReviewResult(t, engine, "replacement-review", replacement, reviewEvidence(review.ProductReview, replacement, "", "PASS", "the owed review completed", nil))
					if _, err := submitFailureResult(t, engine, "different-failure", action); err == nil || rejectionCode(t, err) != CodeReceiptConflict {
						t.Fatalf("a distinct second result must conflict with the settled evidence: %v", err)
					}
				} else {
					if receipt == "UNKNOWN" {
						if err := submitLateDispatchResult(t, engine, "unobserved-review", action, reviewEvidence(review.ProductReview, action, "", "PASS", "unattached result", nil)); err == nil || rejectionCode(t, err) != CodeUnknownAction {
							t.Fatalf("semantic evidence needs a witnessed receipt: %v", err)
						}
						attachUnknownReceipt(t, engine, action, "correlation-review", "worker-review")
						assertSeriesCommitmentHeld(t, engine)
						assertReviewIssuanceHeld(t, engine)
					}
					if err := submitLateDispatchResult(t, engine, "late-review", action, blockingReviewEvidence(review.ProductReview, action, "late review findings")); err != nil {
						t.Fatalf("late review: %v", err)
					}
					if result := engine.LoadMustSucceed(t).State.Results[action]; result.Review == nil || result.PayloadDigest != "sha256:payload-late-review" {
						t.Fatalf("late evidence was not retained: %+v", result)
					}
					assertReviewIssuanceHeld(t, engine)
				}
				snap := engine.LoadMustSucceed(t)
				if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || len(series.RawCandidates) != 1 {
					t.Fatalf("exactly one semantic result must count: %+v", series)
				}
				if slices.Contains(unsettledDispatchIDs(snap.State), action) {
					t.Fatal("recorded result did not settle its dispatch")
				}
				assertSeriesCommitmentHeld(t, engine)
			})
		}
	}
}

func TestTerminalReviewAuthorizationRemainsExclusiveUntilSettlement(t *testing.T) {
	for _, attached := range []bool{false, true} {
		t.Run(fmt.Sprintf("attached=%t", attached), func(t *testing.T) {
			cd, reg := singleCarrierFixture(t)
			engine, dir := reviewEngine(t, cd, reg)
			exhaustProductReviewCapacity(t, engine)
			redoProductReview(t, engine, "authorized-review")
			askAndDecide(t, engine, ControlReviewRoundGrant, "grant-review", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
				Action: review.ProductReview, Reason: "review the explicitly changed requirement",
			}})
			action := pendingActionID(t, engine)
			series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
			authorization := review.AuthorizationOfDispatch(series, action)
			if authorization == "" {
				t.Fatal("over-limit review has no authorization")
			}
			if attached {
				submitUnknownReceipt(t, engine, "spawn-authorized", action, "correlation-authorized")
			} else {
				submitSpawnReceipt(t, engine, "spawn-authorized", action)
			}
			terminalizeDispatchTask(t, engine, action)
			if attached {
				attachUnknownReceipt(t, engine, action, "correlation-authorized", "worker-authorized")
			}
			assertSeriesCommitmentHeld(t, engine)
			assertReviewIssuanceHeld(t, engine)
			if err := askAndDecideRejecting(t, engine, dir, ControlReviewRoundGrant, "grant-occupied", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
				Action: review.ProductReview, Reason: "another grant while the first is unsettled",
			}}); err == nil {
				t.Fatal("unconsumed authorization must prevent another grant")
			}
			revision := engine.LoadMustSucceed(t).State.RequirementRevision()
			evidence := reviewEvidenceAt(review.ProductReview, action, revision, "PASS", "late authorized review", nil)
			evidence.AuthorizationID = authorization
			if err := submitLateDispatchResult(t, engine, "late-authorized", action, evidence); err != nil {
				t.Fatalf("late authorized result: %v", err)
			}
			series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
			if series.Completed != 4 || series.ExtraConsumed != 1 || !review.OccupiedAuthorizationIDs(series)[authorization] {
				t.Fatalf("recorded result must consume its original authorization: %+v", series)
			}
			redoProductReview(t, engine, "next-authorized-review")
			assertReviewIssuanceHeld(t, engine)
			askAndDecide(t, engine, ControlReviewRoundGrant, "grant-next-review", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
				Action: review.ProductReview, Reason: "review the next explicitly changed requirement",
			}})
			next := pendingActionID(t, engine)
			series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
			nextAuthorization := review.AuthorizationOfDispatch(series, next)
			if nextAuthorization == "" || nextAuthorization == authorization {
				t.Fatalf("the next obligation must have its own authorization: %q", nextAuthorization)
			}
			revision = engine.LoadMustSucceed(t).State.RequirementRevision()
			evidence = reviewEvidenceAt(review.ProductReview, next, revision, "PASS", "next authorized result", nil)
			evidence.AuthorizationID = nextAuthorization
			submitReviewResult(t, engine, "next-authorized-result", next, evidence)
			if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 5 || series.ExtraConsumed != 2 {
				t.Fatalf("both distinct authorizations must be consumed: %+v", series)
			}
			assertSeriesCommitmentHeld(t, engine)
		})
	}
}

func TestQuarantinedTerminalReviewKeepsItsObligationThroughReplay(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprintf("rejected=%t", rejected), func(t *testing.T) {
			cd, reg := productReviewDAGFixture(t,
				dagStep{name: "review.primary", deps: []string{"prep.parse"}},
				dagStep{name: "review.spare", deps: []string{"prep.parse"}},
			)
			engine, _ := reviewEngine(t, cd, reg)
			action := issueReady(t, engine)[0]
			submitSpawnReceipt(t, engine, "spawn-quarantined", action)
			terminalizeDispatchTask(t, engine, action)
			request, err := NewRequirementRequestEvent("request-quarantine", requirementPackageOf(nil), requirementOptions()...)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
				t.Fatalf("request submit: %v", err)
			}
			evidence := blockingReviewEvidence(review.ProductReview, action, "quarantined review")
			if rejected {
				evidence.Verification = review.OperatorVerification{}
			}
			event, err := NewReviewWorkerResultEvent("quarantined-review", action, testProvider, "sha256:quarantined-review", evidence)
			if err != nil {
				t.Fatalf("event: %v", err)
			}
			if acceptance, err := engine.Submit(event, fingerprintOf(t, engine)); err != nil || acceptance.Status != "QUARANTINED" {
				t.Fatalf("quarantine: %+v %v", acceptance, err)
			}
			assertSeriesCommitmentHeld(t, engine)
			abortExistingAsk(t, engine, "request-quarantine")
			snap := engine.LoadMustSucceed(t)
			if len(snap.State.QuarantinedResults) != 0 {
				t.Fatal("replay did not consume quarantine")
			}
			if rejected {
				if record := snap.State.RejectedResults[action]; record.Status != "REJECTED_INVALID_RESULT" || record.Payload.PayloadDigest != "sha256:quarantined-review" || record.Payload.Review == nil {
					t.Fatalf("rejected replay did not retain evidence: %+v", record)
				}
				if _, recorded := snap.State.Results[action]; recorded {
					t.Fatal("rejected evidence must not settle the dispatch")
				}
				trail := false
				for _, record := range snap.State.RecoveryRecords {
					trail = trail || record.ActionID == action && strings.Contains(record.Detail, "REJECTED_INVALID_RESULT")
				}
				if !trail {
					t.Fatal("rejected replay has no recovery trail")
				}
				assertSeriesCommitmentHeld(t, engine)
				assertReviewIssuanceHeld(t, engine)
				action = pendingActionID(t, engine)
				submitSpawnReceipt(t, engine, "retry-review-spawn", action)
				if err := submitLateDispatchResult(t, engine, "admissible-review", action, blockingReviewEvidence(review.ProductReview, action, "verified replacement review")); err != nil {
					t.Fatalf("admissible late evidence: %v", err)
				}
			}
			snap = engine.LoadMustSucceed(t)
			if result := snap.State.Results[action]; result.Review == nil || result.ActionID != action {
				t.Fatalf("accepted evidence did not settle the bound dispatch: %+v", result)
			}
			if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || len(series.RawCandidates) != 1 {
				t.Fatalf("replay must count exactly one valid review: %+v", series)
			}
			assertSeriesCommitmentHeld(t, engine)
			assertReviewIssuanceHeld(t, engine)
		})
	}
}

func TestStagedResultOfAttachedTerminalDispatchPairsIntoResults(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "review.primary", deps: []string{"prep.parse"}},
		dagStep{name: "review.spare", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)
	action := issueReady(t, engine)[0]
	evidence := blockingReviewEvidence(review.ProductReview, action, "staged before the receipt")
	event, err := NewReviewWorkerResultEvent("staged-review", action, testProvider, "sha256:staged-review", evidence)
	if err != nil {
		t.Fatalf("staged event: %v", err)
	}
	if acceptance, err := engine.Submit(event, fingerprintOf(t, engine)); err != nil || acceptance.Status != "STAGED" {
		t.Fatalf("staging: %+v %v", acceptance, err)
	}
	submitUnknownReceipt(t, engine, "spawn-staged", action, "correlation-staged")
	terminalizeDispatchTask(t, engine, action)
	assertSeriesCommitmentHeld(t, engine)
	assertReviewIssuanceHeld(t, engine)
	attachUnknownReceipt(t, engine, action, "correlation-staged", "worker-staged")
	snap := engine.LoadMustSucceed(t)
	if _, staged := snap.State.StagedResults[action]; staged {
		t.Fatal("paired evidence must leave staging")
	}
	result := snap.State.Results[action]
	if result.PayloadDigest != "sha256:staged-review" || result.Review == nil || result.Review.DispatchID != action {
		t.Fatalf("attaching transaction did not preserve the paired evidence: %+v", result)
	}
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || len(series.RawCandidates) != 1 {
		t.Fatalf("pairing must count one review: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
	assertReviewIssuanceHeld(t, engine)
}

func TestFailRouteAfterAttachKeepsTheDispatchRecordable(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "review.primary", deps: []string{"prep.parse"}},
		dagStep{name: "review.spare", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)
	action := issueReady(t, engine)[0]
	submitUnknownReceipt(t, engine, "spawn-recovered", action, "correlation-recovered")
	attachUnknownReceipt(t, engine, action, "correlation-recovered", "worker-recovered")
	plan, _, err := engine.RecoverAttempt(action, Interruption{Class: authoring.FailureBusinessReject, CauseKnown: true}, fingerprintOf(t, engine))
	if err != nil || plan.Action != RecoveryFail {
		t.Fatalf("recover: %+v %v", plan, err)
	}
	if _, pending := engine.LoadMustSucceed(t).State.PendingActions[action]; pending {
		t.Fatal("the failure route must finish the attempt")
	}
	assertSeriesCommitmentHeld(t, engine)
	assertReviewIssuanceHeld(t, engine)
	if err := submitLateDispatchResult(t, engine, "recovered-review", action, blockingReviewEvidence(review.ProductReview, action, "late review after recovery")); err != nil {
		t.Fatalf("late recovered result: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if result := snap.State.Results[action]; result.Review == nil || result.PayloadDigest != "sha256:payload-recovered-review" {
		t.Fatalf("recovery lost the original dispatch evidence: %+v", result)
	}
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 {
		t.Fatalf("recovered result must count once: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

func TestSuspendedReadinessPairingRetainsEvidenceAndResumesAfterCorrection(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "product-advisory", product, reviewEvidence(review.ProductReview, product, "", "FAIL", "advisory finding", []review.Finding{{Severity: "P2", Message: "a documented path needs correction"}}))
	readiness := pendingActionID(t, engine)
	event, err := NewReviewWorkerResultEvent("staged-readiness", readiness, testProvider, "sha256:staged-readiness", reviewEvidence(review.StartReadiness, readiness, "", "PASS", "readiness evidence", nil))
	if err != nil {
		t.Fatalf("staged event: %v", err)
	}
	if acceptance, err := engine.Submit(event, fingerprintOf(t, engine)); err != nil || acceptance.Status != "STAGED" {
		t.Fatalf("staging: %+v %v", acceptance, err)
	}
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	candidate := series.RawCandidates[0]
	finding := candidate.Findings[0]
	upgrade := AskOption{ID: AskOptionID("RESEVERITIZE:" + finding.ID), Label: "reseverity"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "upgrade-product", []AskOption{upgrade}, upgrade.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: candidate.DispatchID, RequirementRevision: candidate.RequirementRevision,
		ResultDigest: candidate.ResultDigest, FindingID: finding.ID, Decision: "RESEVERITIZE", Severity: "P1",
		SeverityRationale: "the defect blocks the documented acceptance path", Reason: "user verified the wider impact", ExpectedEffectiveRevision: series.Effective.Revision,
	}})
	submitUnknownReceipt(t, engine, "spawn-suspended", readiness, "correlation-suspended")
	terminalizeDispatchTask(t, engine, readiness)
	attachUnknownReceipt(t, engine, readiness, "correlation-suspended", "worker-suspended")
	snap := engine.LoadMustSucceed(t)
	if _, recorded := snap.State.Results[readiness]; recorded {
		t.Fatal("readiness must wait for the product conclusion")
	}
	if staged := snap.State.StagedResults[readiness]; staged.PayloadDigest != "sha256:staged-readiness" || staged.Review == nil {
		t.Fatalf("suspension lost the original evidence: %+v", staged)
	}
	trail := false
	for _, record := range snap.State.RecoveryRecords {
		trail = trail || record.ActionID == readiness && strings.Contains(record.Detail, "stays pending until the product-review conclusion re-passes")
	}
	if !trail {
		t.Fatal("suspended pairing has no recovery trail")
	}
	assertSeriesCommitmentHeld(t, engine)
	series = snap.State.ReviewSeries.ProductReview
	dismiss := AskOption{ID: AskOptionID("dismiss:" + finding.ID), Label: "dismiss"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "dismiss-product", []AskOption{dismiss}, dismiss.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: candidate.DispatchID, RequirementRevision: candidate.RequirementRevision,
		ResultDigest: candidate.ResultDigest, FindingID: finding.ID, Decision: "DISMISS",
		Supersedes: review.LatestDecisionID(series, candidate.ResultDigest, finding.ID),
		Reason:     "the reported blocking impact was disproved", ExpectedEffectiveRevision: series.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	if _, staged := snap.State.StagedResults[readiness]; staged {
		t.Fatal("restored product conclusion must resume pairing")
	}
	if result := snap.State.Results[readiness]; result.PayloadDigest != "sha256:staged-readiness" || result.Review == nil {
		t.Fatalf("resumed pairing lost the original evidence: %+v", result)
	}
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 1 || series.Effective.Status != "PASS" {
		t.Fatalf("readiness must finish once after product recovery: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}
