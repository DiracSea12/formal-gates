package protocol

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/decision"
)

// lifecycleEvent 便捷构造 lifecycle 事件。
func lifecycleEvent(t *testing.T, id EventID, identity, eventName string) Event {
	t.Helper()
	ev, err := NewLifecycleEventEvent(id, testProvider, identity, eventName)
	if err != nil {
		t.Fatalf("lifecycle event: %v", err)
	}
	return ev
}

// issuedRunWithReceipt 完成「初始化 + 签发 + SPAWNED 回执（correlation
// 认领 identity）」的标准前置，返回引擎与 fp。
func issuedRunWithReceipt(t *testing.T, correlation string) (*Engine, Observation) {
	t.Helper()
	engine, _, plan, _ := preparedEngine(t)
	observedInput := Observation{}
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, observedInput); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "evt-receipt-lc", reviewAction, correlation), observedInput); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	return engine, observedInput
}

// TestLifecyclePairRetainsOriginalEvents：已认领 identity 的 start/stop
// 保留原 provider/correlation 与接纳事实；workflow 投影不动。
func TestLifecyclePairRetainsOriginalEvents(t *testing.T) {
	engine, observedInput := issuedRunWithReceipt(t, "agent-review-1")
	before := engine.LoadMustSucceed(t)

	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-1", "agent-review-1", LifecycleStart), observedInput); err != nil {
		t.Fatalf("start: %v", err)
	}
	mid := engine.LoadMustSucceed(t)
	if len(mid.State.lifecycleMatches(testProvider, "agent-review-1")) != 0 {
		t.Fatal("unpaired start matched")
	}
	acceptance, err := engine.Submit(lifecycleEvent(t, "evt-lc-2", "agent-review-1", LifecycleStop), observedInput)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if acceptance.Status != "ACCEPTED" {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	snap := engine.LoadMustSucceed(t)
	if !reflect.DeepEqual(snap.State.lifecycleMatches(testProvider, "agent-review-1"), []string{"agent-review-1"}) {
		t.Fatalf("paired identities = %+v", snap.State.lifecycleMatches(testProvider, "agent-review-1"))
	}
	if recorded := snap.State.Events["evt-lc-2"].Acceptance; !reflect.DeepEqual(recorded, acceptance) || recorded.Revision == 0 {
		t.Fatalf("recorded acceptance = %+v, returned %+v", recorded, acceptance)
	}
	// lifecycle 只写 observation buffer：任务/步骤/expected 投影与回执前
	// 完全一致。
	if snap.State.TaskStatusOf(reviewTaskKey()) != before.State.TaskStatusOf(reviewTaskKey()) ||
		!reflect.DeepEqual(snap.State.Expected, before.State.Expected) ||
		!reflect.DeepEqual(snap.State.Completed, before.State.Completed) {
		t.Fatal("lifecycle mutated workflow projection")
	}
	if len(snap.State.LifecycleEvents) != 2 {
		t.Fatalf("buffer = %d records", len(snap.State.LifecycleEvents))
	}
	for i, record := range snap.State.LifecycleEvents {
		if record.Provider != testProvider || record.Correlation != "agent-review-1" || record.Identity != "agent-review-1" || record.Event != []string{LifecycleStart, LifecycleStop}[i] || record.Digest == "" {
			t.Fatalf("original lifecycle event = %+v", record)
		}
	}
}

// TestLifecyclePairBeforeReceiptPreservesOriginalClaim：start/stop 先于
// SpawnReceipt 到达时，receipt 认领原配对，不改写缓冲事件与接纳事实。
func TestLifecyclePairBeforeReceiptPreservesOriginalClaim(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	observedInput := Observation{}
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, observedInput); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-before-start", "agent-review-1", LifecycleStart), observedInput); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-before-stop", "agent-review-1", LifecycleStop), observedInput); err != nil {
		t.Fatalf("stop: %v", err)
	}
	beforeReceipt := engine.LoadMustSucceed(t)
	if beforeReceipt.State.lifecycleClaimed(testProvider, "agent-review-1") {
		t.Fatal("pair claimed before receipt")
	}
	for _, id := range []string{"evt-lc-before-start", "evt-lc-before-stop"} {
		if recorded := beforeReceipt.State.Events[id].Acceptance; recorded.Status != "BUFFERED" || recorded.Revision == 0 {
			t.Fatalf("unclaimed event acceptance = %+v", recorded)
		}
	}
	if _, err := engine.Submit(spawnReceipt(t, "evt-lc-after-receipt", reviewAction, "agent-review-1"), observedInput); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	receipt := snap.State.SpawnReceipts[reviewAction]
	if !snap.State.lifecycleClaimed(testProvider, "agent-review-1") || receipt.Provider != testProvider || receipt.Correlation != "agent-review-1" || !reflect.DeepEqual(snap.State.lifecycleMatches(testProvider, receipt.Correlation), []string{"agent-review-1"}) {
		t.Fatalf("receipt did not claim the original pair: %+v", receipt)
	}
	if !reflect.DeepEqual(snap.State.LifecycleEvents, beforeReceipt.State.LifecycleEvents) {
		t.Fatal("receipt changed original lifecycle events")
	}
	for _, id := range []string{"evt-lc-before-start", "evt-lc-before-stop"} {
		if !reflect.DeepEqual(snap.State.Events[id], beforeReceipt.State.Events[id]) {
			t.Fatalf("receipt changed original event record %q", id)
		}
	}
}

// TestLifecycleDuplicateReplayIdempotent：逐字节重发不重复 lifecycle
// buffer；换事件 ID 重发时只占用新的事件台账键。
func TestLifecycleDuplicateReplayIdempotent(t *testing.T) {
	engine, observedInput := issuedRunWithReceipt(t, "agent-review-1")
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-1", "agent-review-1", LifecycleStart), observedInput); err != nil {
		t.Fatalf("start: %v", err)
	}
	before := engine.LoadMustSucceed(t)

	same, err := engine.Submit(lifecycleEvent(t, "evt-lc-1", "agent-review-1", LifecycleStart), observedInput)
	if err != nil || same.Status != "ACCEPTED" {
		t.Fatalf("same-id replay = %+v err = %v", same, err)
	}
	cross, err := engine.Submit(lifecycleEvent(t, "evt-lc-1bis", "agent-review-1", LifecycleStart), observedInput)
	if err != nil {
		t.Fatalf("cross-id replay: %v", err)
	}
	if cross.Status != "DUPLICATE" || cross.Revision != before.Revision+1 {
		t.Fatalf("cross-id replay = %+v", cross)
	}
	after := engine.LoadMustSucceed(t)
	if after.Revision != before.Revision+1 || len(after.State.LifecycleEvents) != len(before.State.LifecycleEvents) || len(after.State.Events) != len(before.State.Events)+1 {
		t.Fatal("duplicate lifecycle replay did not produce exactly one event-ledger commit")
	}
}

// TestLifecycleUnclaimedPairIsBufferedAndLoneStopRejected：未认领的原配对
// 只缓冲；已认领 identity 的孤立 stop 拒绝且零写入。
func TestLifecycleUnclaimedPairIsBufferedAndLoneStopRejected(t *testing.T) {
	engine, observedInput := issuedRunWithReceipt(t, "agent-review-1")
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-a", "agent-stranger", LifecycleStart), observedInput); err != nil {
		t.Fatalf("stranger start: %v", err)
	}
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-b", "agent-stranger", LifecycleStop), observedInput); err != nil {
		t.Fatalf("stranger stop: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if snap.State.lifecycleClaimed(testProvider, "agent-stranger") {
		t.Fatal("stranger identity claimed")
	}
	for _, id := range []string{"evt-lc-a", "evt-lc-b"} {
		if snap.State.Events[id].Acceptance.Status != "BUFFERED" {
			t.Fatalf("unclaimed event acceptance = %+v", snap.State.Events[id].Acceptance)
		}
	}
	if len(snap.State.LifecycleEvents) != 2 {
		t.Fatalf("buffer = %d", len(snap.State.LifecycleEvents))
	}

	// 未配对（只有 stop）的已认领 identity 在 admission 层拒绝，不能进入
	// observation buffer。
	beforeLoneStop := engine.LoadMustSucceed(t)
	if _, err := engine.Submit(lifecycleEvent(t, "evt-lc-c", "agent-review-1", LifecycleStop), observedInput); err == nil {
		t.Fatal("lone stop accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("lone stop rejection code = %q", code)
	}
	snap2 := engine.LoadMustSucceed(t)
	if snap2.Revision != beforeLoneStop.Revision || !reflect.DeepEqual(snap2.State, beforeLoneStop.State) {
		t.Fatal("rejected lone stop changed state")
	}
	if len(snap2.State.lifecycleMatches(testProvider, "agent-review-1")) != 0 {
		t.Fatal("unpaired identity matched")
	}
}

// TestLifecycleNegatives：provider mismatch、非法事件名。
func TestLifecycleNegatives(t *testing.T) {
	engine, observedInput := issuedRunWithReceipt(t, "agent-review-1")
	before := engine.LoadMustSucceed(t)
	mismatch, err := NewLifecycleEventEvent("evt-lc-m", "another-host", "agent-review-1", LifecycleStart)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(mismatch, observedInput); err == nil {
		t.Fatal("provider mismatch accepted")
	} else if code := rejectionCode(t, err); code != CodeProviderMismatch {
		t.Fatalf("provider code = %q", code)
	}
	if _, err := NewLifecycleEventEvent("evt-lc-bad", testProvider, "agent-x", "subagent_crashed"); err == nil {
		t.Fatal("bogus lifecycle event name accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("bogus event code = %q", code)
	}
	if after := engine.LoadMustSucceed(t); after.Revision != before.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("rejected lifecycle events changed state")
	}
}

// TestUnknownReceiptUsesDurableLifecycleCardinality proves reconciliation does
// not accept caller-supplied identities and can represent the multiple-match
// branch when two paired identities share one transport correlation.
func TestUnknownReceiptUsesDurableLifecycleCardinality(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	observedInput := Observation{}
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, observedInput); err != nil {
		t.Fatalf("issue: %v", err)
	}
	for _, identity := range []string{"agent-a", "agent-b"} {
		for _, eventName := range []string{LifecycleStart, LifecycleStop} {
			ev, err := NewCorrelatedLifecycleEvent(
				EventID("evt-"+identity+"-"+eventName), testProvider, "spawn-correlation", identity, eventName,
			)
			if err != nil {
				t.Fatalf("lifecycle event: %v", err)
			}
			if _, err := engine.Submit(ev, observedInput); err != nil {
				t.Fatalf("submit lifecycle event: %v", err)
			}
		}
	}
	unknown := unknownSpawnReceipt(t, "evt-multi-unknown", reviewAction, "spawn-correlation")
	acceptance, err := engine.Submit(unknown, observedInput)
	if err != nil {
		t.Fatalf("UNKNOWN receipt: %v", err)
	}
	if acceptance.RecoveryAction != string(RecoveryOperator) {
		t.Fatalf("initial multiple-match route = %+v", acceptance)
	}
	planResult, _, err := engine.ReconcileUnknownReceipt(reviewAction, observedInput)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if planResult.Action != RecoveryOperator || planResult.LifecycleMatches != 2 {
		t.Fatalf("multiple-match plan = %+v", planResult)
	}
	snap := engine.LoadMustSucceed(t)
	if snap.State.SpawnReceipts[reviewAction].Status != SpawnStatusUnknown {
		t.Fatalf("multiple evidence attached receipt: %+v", snap.State.SpawnReceipts[reviewAction])
	}
	boundary, _, err := engine.Plan(Observation{})
	if err != nil || boundary.Next.Kind != decision.KindAsk {
		t.Fatalf("original Ask priority changed: %+v err=%v", boundary, err)
	}
	view := snap.State.decisionView(engine.cfg.Definition)
	if len(view.OperatorFacts) != 1 || view.OperatorFacts[0].Key != reviewAction {
		t.Fatalf("multiple-match operator fact = %+v", view.OperatorFacts)
	}

	if !reflect.DeepEqual(snap.State.lifecycleMatches(testProvider, "spawn-correlation"), []string{"agent-a", "agent-b"}) || len(snap.State.LifecycleEvents) != 4 {
		t.Fatalf("original lifecycle pairs = %+v, events=%+v", snap.State.lifecycleMatches(testProvider, "spawn-correlation"), snap.State.LifecycleEvents)
	}
}

func TestUnknownReceiptAutomaticAttachmentConvergesAcrossArrivalOrders(t *testing.T) {
	for _, lifecycleFirst := range []bool{false, true} {
		for _, resultFirst := range []bool{false, true} {
			for _, taskEnded := range []bool{false, true} {
				t.Run(fmt.Sprintf("lifecycle-first=%t/result-first=%t/task-ended=%t", lifecycleFirst, resultFirst, taskEnded), func(t *testing.T) {
					engine, dir, action := refillEngine(t)
					observedInput := Observation{}
					pending := engine.LoadMustSucceed(t).State.PendingActions[action]
					claims := 0
					engine.cfg.ClaimDispatch = func(run, id, identity, provider string) error {
						if id != action || identity != "actual-worker" || provider != testProvider {
							t.Fatalf("claim differs from observed native identity: %q %q %q", id, identity, provider)
						}
						claims++
						return nil
					}
					submit := func(event Event) Acceptance {
						t.Helper()
						accepted, err := engine.Submit(event, observedInput)
						if err != nil {
							t.Fatal(err)
						}
						return accepted
					}
					observe := func() {
						for _, edge := range []string{LifecycleStart, LifecycleStop} {
							submit(mustLifecycleEvent(t, "native-"+edge, "transport-key", "actual-worker", edge))
						}
					}
					result := workerResult(t, "native-result", action, "sha256:actual-output")
					if resultFirst {
						if got := submit(result); got.Status != "STAGED" {
							t.Fatalf("result-before-receipt = %+v", got)
						}
					}
					if taskEnded {
						terminalizeDispatchTask(t, engine, action)
					}
					if lifecycleFirst {
						observe()
					}
					submit(unknownSpawnReceipt(t, "native-receipt", action, "transport-key"))
					var err error
					engine, err = New(mustStore(t, dir), engine.cfg)
					if err != nil {
						t.Fatal(err)
					}
					if !lifecycleFirst {
						observe()
					}
					if !resultFirst {
						if got := submit(result); got.Status != "ACCEPTED" {
							t.Fatalf("attached result = %+v", got)
						}
					}
					settled := engine.LoadMustSucceed(t)
					receipt := settled.State.SpawnReceipts[action]
					if receipt.Status != SpawnStatusAttached || receipt.LifecycleIdentity != "actual-worker" || receipt.Correlation != "transport-key" || receipt.Task != pending.Task {
						t.Fatalf("attachment lost original binding: %+v", receipt)
					}
					if got := settled.State.Results[action]; got.Outcome != OutcomePass || got.PayloadDigest != "sha256:actual-output" {
						t.Fatalf("result = %+v", got)
					}
					if len(settled.State.StagedResults) != 0 || len(settled.State.EndedAttempts) != 0 || !containsStep(settled.State.Completed, string(pending.Task.Step)) || claims != 1 {
						t.Fatalf("unsettled execution: %+v, claims=%d", settled.State, claims)
					}
					submit(unknownSpawnReceipt(t, "native-receipt-replay", action, "transport-key"))
					submit(mustLifecycleEvent(t, "native-stop-replay", "transport-key", "actual-worker", LifecycleStop))
					if claims != 1 || !reflect.DeepEqual(engine.LoadMustSucceed(t).State.Results[action], settled.State.Results[action]) {
						t.Fatal("replay repeated attachment or result")
					}
				})
			}
		}
	}
}

func TestUnknownReceiptWithoutPairedIdentityPresentsOriginalOperatorBoundary(t *testing.T) {
	for _, matches := range []int{0, 2} {
		t.Run(fmt.Sprintf("paired-identities=%d", matches), func(t *testing.T) {
			cd, reg := reviewStepsFixture(t, "product.review")
			engine, _ := reviewEngine(t, cd, reg)
			action := issueReady(t, engine)[0]
			observedInput := Observation{}
			for i := 0; i < matches; i++ {
				for _, edge := range []string{LifecycleStart, LifecycleStop} {
					if _, err := engine.Submit(mustLifecycleEvent(t, fmt.Sprintf("pair-%d-%s", i, edge), "transport-key", fmt.Sprintf("worker-%d", i), edge), observedInput); err != nil {
						t.Fatal(err)
					}
				}
			}
			submitUnknownReceipt(t, engine, "unknown-unpaired", action, "transport-key")
			if matches == 0 {
				if _, err := engine.Submit(mustLifecycleEvent(t, "unpaired-start", "transport-key", "actual-worker", LifecycleStart), observedInput); err != nil {
					t.Fatal(err)
				}
			}
			before := engine.LoadMustSucceed(t)
			plan, revision, err := engine.Plan(Observation{})
			if err != nil || plan.Next.Kind != decision.KindOperator || revision != before.Revision {
				t.Fatalf("unresolved boundary = %+v revision=%d err=%v", plan, revision, err)
			}
			if len(plan.Next.Operator.Facts) != 1 || plan.Next.Operator.Facts[0].Source != decision.SourceReceipt || plan.Next.Operator.Facts[0].Key != action {
				t.Fatalf("unbound operator facts: %+v", plan.Next.Operator)
			}
			if receipt := before.State.SpawnReceipts[action]; receipt.Status != SpawnStatusUnknown || receipt.LifecycleIdentity != "" {
				t.Fatalf("ambiguous lifecycle attached: %+v", receipt)
			}
			if after := engine.LoadMustSucceed(t); !reflect.DeepEqual(before, after) {
				t.Fatal("read-only operator projection mutated authority")
			}
		})
	}
}

func TestUnknownReceiptOperatorProjectionPreservesOriginalFacts(t *testing.T) {
	engine, _, action := refillEngine(t)
	submitUnknownReceipt(t, engine, "unknown-original-facts", action, "transport-key")
	state := engine.LoadMustSucceed(t).State
	state.OperatorFacts = make([]decision.Fact, 1, 4)
	state.OperatorFacts[0] = decision.Fact{Source: decision.SourceReceipt, Key: "original", Value: "original evidence"}
	original := slices.Clone(state.OperatorFacts[:cap(state.OperatorFacts)])
	view := state.decisionView(engine.cfg.Definition)
	if len(view.OperatorFacts) != 2 || !reflect.DeepEqual(original, state.OperatorFacts[:cap(state.OperatorFacts)]) {
		t.Fatal("operator projection mutated source facts or lost the original evidence")
	}
}
