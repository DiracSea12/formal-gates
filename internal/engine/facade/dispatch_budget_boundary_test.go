package facade

// 派发预算边界（token cap 到达、有限 retry 余量耗尽）的公开链行为：边界＝
// 零签发/零宿主效果＋DISPATCH_BUDGET_DISPOSITION Ask；承载观察事实的提交事务
// 照常成立；一次 continue 授权只解锁一个被阻效果；UNKNOWN 回执 attach 后以解
// 析出的真实身份完成精确计量；终态收口移除 lifecycle 活动标记。

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/testkit"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/lifecycle"
)

// meteredProvider 是暴露 transcript 计量链的宿主 provider。
const meteredProvider = "claude-code"

func writeTokenCap(t *testing.T, root string, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".gates"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gates", "token-cap.json"), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

// completeMeteredDispatch 完成一次派发的宿主侧观察与结果提交（真实 transcript
// 用量经 bridge 记入 cost 投影）。
func completeMeteredDispatch(t *testing.T, d *validationDriver, root, action, agent string, transcriptLines []string, result *protocol.WorkerResultPayload) protocol.Acceptance {
	t.Helper()
	transcript := filepath.Join(root, agent+".jsonl")
	writeTranscript(t, transcript, transcriptLines)
	spawned, err := protocol.NewSpawnReceiptEvent(protocol.EventID("spawn-"+agent), action, d.host(), agent, protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = d.f.Submit(spawned); err != nil {
		t.Fatal(err)
	}
	completeMeteredSource(t, root, d.host(), agent, transcript)
	return d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: result})
}

// TestAutomaticResumeAtReachedTokenCapLandsBudgetAsk：cap 被真实用量达到后，
// 在途治理派发的瞬态自动恢复不再派发 resume，落 DISPATCH_BUDGET_DISPOSITION
// Ask（继续执行只能由用户授权一次有限增量）。
func TestAutomaticResumeAtReachedTokenCapLandsBudgetAsk(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// 21 = 首个派发将记录的精确用量（11+3 输入、7 输出）。
	writeTokenCap(t, root, `{"engineDispatchMaxTotalTokens":21}`)
	d := plannedValidationDriverAt(t, root, meteredProvider, nil, commandPlan(), "blackbox")
	id, input := planBatchAction(d, "command")
	completeMeteredDispatch(t, d, root, id, "agent-batch", []string{
		`{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":3}}}`,
	}, &protocol.WorkerResultPayload{ActionID: id, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "batch-digest", Validation: &protocol.ValidationResult{Binding: input.Binding, Development: batchEvidence(input), Status: "PASS"}})

	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := snap.State.Cost.Dispatches[id]
	if entry.Source != "transcript" || entry.TotalInputTokens+entry.OutputTokens != 21 {
		t.Fatalf("first dispatch cost = %+v", entry)
	}

	// cap 已达后在途的 QA 派发：瞬态恢复入口必须零效果＋落预算 Ask。
	qaID := ""
	for pendingID, pending := range snap.State.PendingActions {
		if input, ok := snap.State.ValidationInputs(d.f.definition)[pendingID]; ok && input.Handler == "engine.qa.design.v6" && input.Manifest.Binding.ReviewKind == "BLACKBOX" && pending.AttemptID == snap.State.Attempts[pending.Task].ID {
			qaID = pendingID
			break
		}
	}
	if qaID == "" {
		t.Fatal("no governed dispatch remained in flight after the cap was reached")
	}
	spawned, err := protocol.NewSpawnReceiptEvent("spawn-qa", qaID, d.host(), "agent-qa", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = d.f.Submit(spawned); err != nil {
		t.Fatal(err)
	}
	fp := protocol.Observation{}
	if _, _, err := d.f.engine.RecoverAttempt(qaID, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, fp); err != nil {
		t.Fatalf("recover attempt failed: %v", err)
	}
	d.drive()
	snap, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	resumeIntents := 0
	for _, intent := range snap.State.PendingHostActions {
		if intent.Operation == protocol.HostActionResumeAgent {
			resumeIntents++
		}
	}
	if resumeIntents != 0 {
		t.Fatalf("automatic resume dispatched after the token cap was reached: %d intents", resumeIntents)
	}
	asked := false
	for _, ask := range snap.State.PendingAsks {
		if ask.Validation != nil && ask.Validation.Kind == protocol.KindDispatchBudgetDisposition && !ask.Resolved {
			asked = true
		}
	}
	if !asked {
		t.Fatal("the recovery at the reached cap did not land a budget disposition ask")
	}
}

// TestResultSubmissionSurvivesTokenCapRefillBoundary：结果提交的同事务 refill
// 在 cap 边界零签发——观察事实（结果＋精确用量）照常落账，Ask 落地，事务不被
// 边界拒绝（重投必再拒的 wedge 不存在）。
func TestResultSubmissionSurvivesTokenCapRefillBoundary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTokenCap(t, root, `{"engineDispatchMaxTotalTokens":21}`)
	p := commandPlan()
	base := p.Batches[0]
	p.Batches = nil
	for _, id := range []string{"parser", "command"} {
		b := base
		b.ID = id
		b.Subtasks = []protocol.DevelopmentSubtask{{ID: id + "-body", Description: "implement " + id}}
		b.Artifacts = []string{id + ".go"}
		if id == "command" {
			b.DependsOn = []string{"parser"}
		}
		p.Batches = append(p.Batches, b)
	}
	d := plannedValidationDriverAt(t, root, meteredProvider, nil, p, "blackbox")
	first, input := planBatchAction(d, "parser")
	originalAttempt := d.run.Attempts[input.Binding.Task]
	accepted := completeMeteredDispatch(t, d, root, first, "agent-parser", []string{
		`{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":3}}}`,
	}, &protocol.WorkerResultPayload{ActionID: first, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "parser-digest", Validation: &protocol.ValidationResult{Binding: input.Binding, Development: batchEvidence(input), Status: "PASS"}})
	if accepted.Status != "ACCEPTED" {
		t.Fatalf("result at the cap boundary status = %s: %v", accepted.Status, accepted)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := snap.State.Cost.Dispatches[first]
	if entry.Source != "transcript" || entry.TotalInputTokens+entry.OutputTokens != 21 {
		t.Fatalf("boundary dropped the observation: cost = %+v", entry)
	}
	// Accepted edits keep their original execution until programmed checks settle.
	record, recorded := snap.State.Results[first]
	if !recorded || record.Outcome != protocol.OutcomePass {
		t.Fatalf("boundary dropped the accepted result: %+v", record)
	}
	if _, staged := snap.State.StagedResults[first]; staged {
		t.Fatal("accepted result stayed staged at the boundary")
	}
	if attempt := snap.State.Attempts[input.Binding.Task]; attempt.ID != originalAttempt.ID || attempt.ActionID != first {
		t.Fatal("boundary re-issued the original edit execution")
	}
	d.drive()
	snap, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, issued := snap.State.PendingActions[first]; issued || snap.State.TaskStatusOf(input.Binding.Task) != runtime.TaskTerminal {
		t.Fatal("programmed source checks did not complete and clear the original action")
	}
	checks := map[string]bool{}
	for action, receipt := range snap.State.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" {
			continue
		}
		var evidence protocol.CheckEvidence
		if decodeReceiptEvidence(receipt, &evidence) != nil || evidence.SourceActionID != first {
			continue
		}
		intent := snap.State.ReconciledEffects[action].Intent
		if intent == nil || intent.Adapter == nil {
			t.Fatal("check receipt lost its original approved invocation")
		}
		var params protocol.CheckParameters
		body, _ := json.Marshal(intent.Adapter.Params)
		if json.Unmarshal(body, &params) != nil || receipt.Status != protocol.HostActionStatusExecuted || evidence.ExitCode != 0 || evidence.Error != "" || evidence.SourceIdentity != params.SourceIdentity || evidence.SourceDigest != params.SourceDigest {
			t.Fatal("original check receipt did not prove successful execution on its frozen source")
		}
		for _, approved := range input.Development.Batch.Checks {
			if params.Check == approved {
				checks[approved.ID] = true
			}
		}
	}
	if len(checks) != len(input.Development.Batch.Checks) {
		t.Fatal("original action completed without every approved check receipt")
	}
	for pendingID, pending := range snap.State.PendingActions {
		if input := snap.State.ValidationInputs(d.f.definition)[pendingID]; input.Development != nil && input.Development.Batch.ID == "command" {
			t.Fatalf("dependent batch issued at the reached cap: %s %+v", pendingID, pending)
		}
	}
	asked := false
	for _, ask := range snap.State.PendingAsks {
		if ask.Validation != nil && ask.Validation.Kind == protocol.KindDispatchBudgetDisposition && !ask.Resolved {
			asked = true
		}
	}
	if !asked {
		t.Fatal("the cap boundary did not land a budget disposition ask")
	}
}

// TestBudgetContinueUnlocksOneBlockedRecoveryOnly：一次 continue 授权只解锁一
// 个被阻的恢复效果——授权效果落账（resume intent／其 EXECUTED 回执接纳）即消
// 耗凭据，其后到达的被阻恢复不放行（不预先累积）。
func TestBudgetContinueUnlocksOneBlockedRecoveryOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTokenCap(t, root, `{"engineDispatchMaxTotalTokens":0}`)
	d := startValidationDriverAt(t, root, meteredProvider, nil)
	// 受理后的首条审查派发在 guard 包络就位前签出；cap=0 已达。
	first := ""
	for id := range d.run.PendingActions {
		first = id
	}
	if first == "" {
		t.Fatal("no review dispatch in flight")
	}
	d.receipt(first)
	if _, _, err := d.f.engine.RecoverAttempt(first, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range snap.State.PendingHostActions {
		if intent.Operation == protocol.HostActionResumeAgent {
			t.Fatal("resume dispatched while the cap boundary held")
		}
	}
	shown, err := d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	d.run = shown
	var ask PendingAskFace
	for _, face := range d.run.PendingAsks {
		if face.Validation != nil && face.Validation.Kind == protocol.KindDispatchBudgetDisposition {
			ask = face
		}
	}
	if ask.RequestID == "" {
		t.Fatal("the blocked recovery did not land a budget disposition ask")
	}
	// 一次 continue 授权：同事务内解锁该恢复（resume intent 落账即消耗凭据）。
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{
		Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "continue",
		Validation: &protocol.ValidationDecision{Reason: "one bounded continuation", Digest: ask.Validation.Digest, StateRevision: d.run.Revision},
	}})
	snap, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	resumeIntent := protocol.HostActionIntent{}
	for _, intent := range snap.State.PendingHostActions {
		if intent.Operation == protocol.HostActionResumeAgent {
			resumeIntent = intent
		}
	}
	if resumeIntent.Resume == nil || resumeIntent.Resume.WorkerActionID != first {
		t.Fatalf("continue authorization unlocked the wrong or no effect: %+v", resumeIntent)
	}
	// 授权的恢复执行完成，其审查 PASS 推进到下一条派发。
	receipt := d.resumeReceipt(resumeIntent)
	d.submit(receipt)
	reviewTranscript := filepath.Join(root, "review-worker.jsonl")
	writeTranscript(t, reviewTranscript, []string{`{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}}`})
	completeMeteredSource(t, root, d.host(), first, reviewTranscript)
	reviewPass, err := protocol.NewReviewWorkerResultEvent(d.eventID(), first, d.host(), string(d.eventID()), protocol.ReviewResultEvidence{
		Action: review.ProductReview, DispatchID: first, RequirementRevision: d.revision, Status: "PASS",
		Findings: []review.Finding{},
	})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(reviewPass)
	verifyReviewFixture(t, d, reviewPass)
	d.drive()
	next := ""
	for id := range d.run.PendingActions {
		next = id
	}
	if next == "" || next == first {
		t.Fatalf("no follow-up dispatch after the authorized recovery completed: %+v", d.run.PendingActions)
	}
	// 同一授权不得解锁第二个被阻恢复：后续瞬态恢复仍零效果＋落自己的 Ask。
	d.receipt(next)
	if _, _, err := d.f.engine.RecoverAttempt(next, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	d.drive()
	snap, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range snap.State.PendingHostActions {
		if intent.Operation == protocol.HostActionResumeAgent {
			t.Fatalf("one continue authorization unlocked a second blocked recovery: %+v", intent)
		}
	}
	pendingNext := false
	for _, ask := range snap.State.PendingAsks {
		if ask.Validation != nil && ask.Validation.Kind == protocol.KindDispatchBudgetDisposition && !ask.Resolved {
			pendingNext = true
		}
	}
	if !pendingNext {
		t.Fatal("the second blocked recovery did not land its own budget disposition ask")
	}
}

// TestUnknownReceiptAttachesMeteredIdentityAndRecordsUsage：UNKNOWN 回执的绑定
// 延后至 attach，以 lifecycle 解析出的真实身份建立——sidecar 事件先于绑定到达
// 时计量仍成立，结果走公开接纳记精确用量。
func TestUnknownReceiptAttachesMeteredIdentityAndRecordsUsage(t *testing.T) {
	t.Parallel()
	for _, lifecycleFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("lifecycle-first=%t", lifecycleFirst), func(t *testing.T) {

			root := t.TempDir()
			testkit.ConfigureNativePaths(t, root, meteredProvider)
			if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0o600); err != nil {
				t.Fatal(err)
			}
			f, r, err := Start(StartOptions{Capacity: 1, Root: root, VCSRunner: newFrozenGitRepo(), Request: StartRequest{RunID: "unknown-attach", Route: "regular", Provider: meteredProvider, BaseSnapshot: frozenSnapshot, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", meteredProvider)})
			if err != nil {
				t.Fatal(err)
			}
			if r, err = f.Drive(); err != nil {
				t.Fatal(err)
			}
			action := ""
			for id := range r.PendingActions {
				action = id
				break
			}
			if action == "" {
				t.Fatal("no dispatch")
			}
			observe := func() {
				start, err := protocol.NewCorrelatedLifecycleEvent("lc-start", meteredProvider, "corr-track-1", "agent-9", protocol.LifecycleStart)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = f.Submit(start); err != nil {
					t.Fatal(err)
				}
				stop, err := protocol.NewCorrelatedLifecycleEvent("lc-stop", meteredProvider, "corr-track-1", "agent-9", protocol.LifecycleStop)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = f.Submit(stop); err != nil {
					t.Fatal(err)
				}
			}
			if lifecycleFirst {
				observe()
			}

			unknown, err := protocol.NewSpawnReceiptEvent("spawn-unknown", action, meteredProvider, "corr-track-1", protocol.SpawnStatusUnknown)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = f.Submit(unknown); err != nil {
				t.Fatal(err)
			}
			if !lifecycleFirst {
				observe()
			}

			transcript := filepath.Join(root, "agent-nine.jsonl")
			writeTranscript(t, transcript, []string{`{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":1}}}`})

			completeMeteredSource(t, root, meteredProvider, "agent-9", transcript)
			snap, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if got := snap.State.SpawnReceipts[action].LifecycleIdentity; got != "agent-9" {
				t.Fatalf("attached identity = %q", got)
			}
			result, err := protocol.NewReviewWorkerResultEvent("result-attached", action, meteredProvider, "sha256:result", protocol.ReviewResultEvidence{
				Action: review.ProductReview, DispatchID: action, RequirementRevision: testReceipt().RequirementRevision, Status: "PASS",
				Findings: []review.Finding{},
			})
			if err != nil {
				t.Fatal(err)
			}
			accepted, _, err := f.Submit(result)
			if err != nil {
				t.Fatal(err)
			}
			if accepted.Status != "ACCEPTED" {
				t.Fatalf("result after attach status = %s, want ACCEPTED", accepted.Status)
			}
			snap, err = f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			entry := snap.State.Cost.Dispatches[action]
			if entry.Source != "transcript" || entry.InputCacheMissTokens != 5 || entry.InputCacheHitTokens != 1 || entry.OutputTokens != 2 {
				t.Fatalf("attached dispatch not metered by its resolved identity: %+v", entry)
			}
			if replay, _, err := f.Submit(result); err != nil || replay.Status != "ACCEPTED" {
				t.Fatalf("same result replay = %+v err=%v", replay, err)
			}
			after, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after.State.Cost, snap.State.Cost) || after.State.SpawnReceipts[action].LifecycleIdentity != "agent-9" {
				t.Fatal("replay changed metering or native identity")
			}

		})
	}
}

// TestTerminalRunEndsLifecycleAttribution：run 终态收口移除 lifecycle 活动标
// 记（幂等）——终态后晚到的宿主事件归属无 run，不再落入该 run 的事件树。
func TestTerminalRunEndsLifecycleAttribution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	configureLightweight(t, root)
	f, run, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "terminal-lifecycle", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "ACTIVE" {
		t.Fatalf("run status = %s", run.Status)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates", "tmp", "terminal-lifecycle", "lifecycle", "active")); err != nil {
		t.Fatalf("active marker missing while the run is active: %v", err)
	}
	if run, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	if run.Status != "COMPLETE" {
		t.Fatalf("run status = %s", run.Status)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates", "tmp", "terminal-lifecycle", "lifecycle", "active")); !os.IsNotExist(err) {
		t.Fatalf("terminal run kept its lifecycle active marker: %v", err)
	}
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	active, err := lifecycle.ActiveRunIDs(root)
	if err != nil || len(active) != 0 {
		t.Fatalf("terminal run remains active: %v %v", active, err)
	}

}

// submitDecide 提交一条 DECIDE 决定并返回提交错误（判拒断言用：零状态变化由
// 调用方核对账本）。
func submitDecide(d *validationDriver, ask PendingAskFace, choice, reason string) error {
	d.t.Helper()
	_, _, err := d.f.Submit(protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{
		Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: protocol.AskOptionID(choice),
		Validation: &protocol.ValidationDecision{Reason: reason, Digest: ask.Validation.Digest, StateRevision: d.run.Revision},
	}})
	return err
}

// budgetAsk 返回当前投影里未决的预算处置 Ask 面。
func budgetAsk(d *validationDriver) PendingAskFace {
	d.t.Helper()
	for _, face := range d.run.PendingAsks {
		if face.Validation != nil && face.Validation.Kind == protocol.KindDispatchBudgetDisposition {
			return face
		}
	}
	d.t.Fatalf("no pending budget disposition ask: %+v", d.run.PendingAsks)
	return PendingAskFace{}
}

// resumeAndComplete 接纳原恢复 owner 签发的 RESUME_AGENT；没有在途恢复时，
// 先记录客观瞬态中断，再按原 RequestID 读取 intent 并提交 EXECUTED 回执。
func resumeAndComplete(d *validationDriver, action string) error {
	d.t.Helper()
	snap, err := d.f.engine.Load()
	if err != nil {
		return err
	}
	var intent protocol.HostActionIntent
	for _, pending := range snap.State.PendingHostActions {
		if pending.Resume != nil && pending.Resume.WorkerActionID == action {
			intent = pending
		}
	}
	if intent.ActionID == "" {
		plan, _, err := d.f.engine.RecoverAttempt(action, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{})
		if err != nil {
			return err
		}
		snap, err = d.f.engine.Load()
		if err != nil {
			return err
		}
		for _, pending := range snap.State.PendingHostActions {
			if pending.Correlation == plan.RequestID {
				intent = pending
			}
		}
	}
	if intent.Resume == nil || intent.Resume.WorkerActionID != action || intent.Resume.Identity != action || intent.Correlation == "" {
		d.t.Fatalf("recovery lost original worker binding: %+v", intent)
	}
	ev := d.resumeReceipt(intent)
	d.submit(ev)
	return nil
}

// TestEndedAwaitingResumeCountsAndExhaustsBudget：原恢复 Ask 在任务终态后仍
// 绑定已终待果条目；EXECUTED 在承载账计数，有限边界只落预算 Ask。
func TestEndedAwaitingResumeCountsAndExhaustsBudget(t *testing.T) {
	t.Parallel()
	for _, used := range []int{0, 1} {
		t.Run(map[int]string{0: "available", 1: "exhausted"}[used], func(t *testing.T) {
			d := newValidationDriver(t, "blackbox")
			id, input := d.action("engine.qa.design.v6")
			original := d.run.Attempts[input.Binding.Task]
			d.receipt(id)
			start, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), id, protocol.LifecycleStart)
			if err != nil {
				t.Fatal(err)
			}
			d.submit(start)
			if used == 1 {
				if err := resumeAndComplete(d, id); err != nil {
					t.Fatal(err)
				}
			}
			plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "USER_ACTION_REQUIRED"}, protocol.Observation{})
			if err != nil || plan.Action != protocol.RecoveryAsk {
				t.Fatalf("original recovery Ask = %+v %v", plan, err)
			}
			progress, err := protocol.NewTaskEvent(d.eventID(), input.Binding.Task, original.ID, runtime.TaskTerminal)
			if err != nil {
				t.Fatal(err)
			}
			d.submit(progress)
			ended := d.run.EndedAttempts[input.Binding.Task]
			if !ended.AwaitingResult || ended.Attempts != used || ended.ID != original.ID || ended.ActionID != id {
				t.Fatalf("precondition: ended attempt = %+v", ended)
			}
			if _, pending := d.run.PendingActions[id]; pending {
				t.Fatal("terminal task retained its pending action")
			}
			token, err := d.f.Freshness(plan.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			decision, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(plan.RequestID), token, "resume")
			if err != nil {
				t.Fatal(err)
			}
			d.submit(decision)
			snap, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			var intent protocol.HostActionIntent
			for _, pending := range snap.State.PendingHostActions {
				if pending.Correlation == plan.RequestID {
					intent = pending
				}
			}
			if used == 0 {
				expectedInput := input
				for _, record := range snap.State.RecoveryRecords {
					if record.RequestID == plan.RequestID {
						if record.ActionID != id || record.Task != input.Binding.Task || record.AttemptID != original.ID || record.Action != protocol.RecoveryAsk {
							t.Fatalf("recovery lost original task and attempt: %+v", record)
						}
						expectedInput.Interruption = &record
					}
				}
				if expectedInput.Interruption == nil || intent.Resume == nil || intent.Resume.WorkerActionID != id || intent.Resume.AttemptID != original.ID || intent.Resume.Identity != id || intent.Resume.Input == nil || !reflect.DeepEqual(*intent.Resume.Input, expectedInput) {
					t.Fatalf("ended recovery lost original unfinished input: %+v", intent)
				}
				if err := resumeAndComplete(d, id); err != nil {
					t.Fatalf("first resume of the ended-awaiting execution blocked: %v", err)
				}
			} else {
				if intent.ActionID != "" {
					t.Fatalf("second resume accepted after the finite budget was consumed: %+v", intent)
				}
				if ask := budgetAsk(d); ask.RequestID == "" {
					t.Fatal("exhaustion boundary did not land a budget disposition ask")
				}
			}
			snap, err = d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			final := snap.State.EndedAttempts[input.Binding.Task]
			if final.Attempts != 1 || final.MaxAttempts != 2 || !final.AwaitingResult || final.ID != original.ID || final.ActionID != id {
				t.Fatalf("ended recovery changed the holding ledger: %+v", final)
			}
		})
	}
}

// TestSingleRetryExtensionAcrossBudgetAndRetryAsks：同一逻辑任务的一次追加
// retry slot 是一个授权事实——预算处置 retry 与 RETRY approve 任一通道行使后，
// 另一通道对同任务的追加即被拒（两个方向都要；max 不得被推到 4）。
func TestSingleRetryExtensionAcrossBudgetAndRetryAsks(t *testing.T) {
	t.Parallel()
	t.Run("budget retry first blocks the RETRY extension", func(t *testing.T) {
		d := newValidationDriver(t, "blackbox")
		id, _ := d.action("engine.qa.design.v6")
		d.receipt(id)
		start, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), id, id, protocol.LifecycleStart)
		if err != nil {
			t.Fatal(err)
		}
		d.submit(start)
		if err := resumeAndComplete(d, id); err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{}); err != nil {
			t.Fatal(err)
		}
		d.run, err = d.f.Show()
		if err != nil {
			t.Fatal(err)
		}
		blocked, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		for _, intent := range blocked.State.PendingHostActions {
			if intent.Operation == protocol.HostActionResumeAgent {
				t.Fatalf("budget boundary signed a resume: %+v", intent)
			}
		}
		if err := submitDecide(d, budgetAsk(d), "retry", "one bounded retry slot for this task"); err != nil {
			t.Fatal(err)
		}
		if err := resumeAndComplete(d, id); err != nil {
			t.Fatal(err)
		}
		// 授权的恢复执行入账（2/3）后提交瞬态运行错误：余量耗尽，引擎生成
		// RETRY Ask——offer 侧不再提供 approve（一次追加已被预算通道授予）。
		d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: d.host(), Outcome: protocol.OutcomeRuntimeError, PayloadDigest: "runtime-failure", FailureClass: "TRANSIENT_ENGINE_ERROR"}})
		retryAsk := d.validationAsk("RETRY")
		for _, option := range retryAsk.Options {
			if option.ID == "approve" {
				t.Fatalf("RETRY offer still advertises an extension after the budget channel granted it: %+v", retryAsk.Options)
			}
		}
		// accept 侧同一谓词兜底：直接提交 approve 仍被拒（零状态变化）。
		if err := submitDecide(d, retryAsk, "approve", "a second extension of the same task"); err == nil {
			t.Fatal("RETRY approve accepted after the budget channel already extended the same task")
		}
		snap, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		ended := snap.State.EndedAttempts[snap.State.DispatchBindings[id].Task]
		if ended.MaxAttempts != 3 || ended.Attempts != 3 {
			t.Fatalf("rejected extension changed the ledger: %+v", ended)
		}
	})

	t.Run("RETRY extension first blocks the budget retry", func(t *testing.T) {
		d := newValidationDriver(t, "blackbox")
		id, input := d.action("engine.qa.design.v6")
		d.receipt(id)
		start, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), id, id, protocol.LifecycleStart)
		if err != nil {
			t.Fatal(err)
		}
		d.submit(start)
		if err := resumeAndComplete(d, id); err != nil {
			t.Fatal(err)
		}
		// 一次执行入账（1/2）后提交瞬态运行错误：余量耗尽 → RETRY Ask。
		d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: d.host(), Outcome: protocol.OutcomeRuntimeError, PayloadDigest: "runtime-failure", FailureClass: "TRANSIENT_ENGINE_ERROR"}})
		decideValidation(d, d.validationAsk("RETRY"), "approve")
		d.drive()
		reissued, reinput := d.action("engine.qa.design.v6")
		if reissued == id || reinput.Binding.Task != input.Binding.Task {
			t.Fatalf("retry extension re-signed a different task: %s %+v", reissued, reinput.Binding)
		}
		d.receipt(reissued)
		start2, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), reissued, reissued, protocol.LifecycleStart)
		if err != nil {
			t.Fatal(err)
		}
		d.submit(start2)
		// 换新携带已计执行（2/3）：恢复即触达预算边界，落预算 Ask。
		if _, _, err := d.f.engine.RecoverAttempt(reissued, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{}); err != nil {
			t.Fatal(err)
		}
		d.run, err = d.f.Show()
		if err != nil {
			t.Fatal(err)
		}
		blocked, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		for _, intent := range blocked.State.PendingHostActions {
			if intent.Operation == protocol.HostActionResumeAgent {
				t.Fatalf("budget boundary signed a resume: %+v", intent)
			}
		}
		if err := submitDecide(d, budgetAsk(d), "retry", "a second extension via the budget channel"); err == nil {
			t.Fatal("budget retry accepted after the RETRY channel already extended the same task")
		}
		snap, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if kept := snap.State.Attempts[input.Binding.Task]; kept.MaxAttempts != 3 || kept.Attempts != 2 {
			t.Fatalf("rejected extension changed the ledger: %+v", kept)
		}
	})
}

// TestOwnerOverlapMarksBothRunsUnavailable：owner 区间重叠是双边持久事实——后
// 启动 run 经协议幂等入口把重叠补写进先启动 run 的 owner 投影，
// 双方终态都记 UNAVAILABLE 零数字。
func TestOwnerOverlapMarksBothRunsUnavailable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	configureLightweight(t, root)
	transcript := filepath.Join(root, "shared.jsonl")
	writeTranscript(t, transcript, []string{`{"type":"assistant","uuid":"base","message":{"usage":{"input_tokens":9,"output_tokens":4}}}`})
	start := func(id string) *Facade {
		f, _, err := Start(StartOptions{Root: root, Owner: OwnerBinding{Transcript: transcript, Provider: meteredProvider}, Request: StartRequest{RunID: id, Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	first := start("first")
	second := start("second")
	writeTranscript(t, transcript, []string{
		`{"type":"assistant","uuid":"base","message":{"usage":{"input_tokens":9,"output_tokens":4}}}`,
		`{"type":"assistant","uuid":"during-both","message":{"usage":{"input_tokens":15,"output_tokens":8}}}`,
	})
	// second 先终态：自身检测＝UNAVAILABLE。
	secondRun, err := second.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if secondRun.Cost.Owner == nil || secondRun.Cost.Owner.Source != "unavailable" || secondRun.Cost.Owner.TotalInputTokens != 0 {
		t.Fatalf("second owner interval = %+v, want UNAVAILABLE with zero numbers", secondRun.Cost.Owner)
	}
	// first 后终态：重叠事实由 second 启动时经协议入口补写，不得报精确区间。
	firstRun, err := first.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if firstRun.Cost.Owner == nil || firstRun.Cost.Owner.Source != "unavailable" || firstRun.Cost.Owner.TotalInputTokens != 0 || firstRun.Cost.Owner.OutputTokens != 0 {
		t.Fatalf("first owner interval = %+v, want UNAVAILABLE with zero numbers", firstRun.Cost.Owner)
	}
	// first 终态的 UNAVAILABLE 即 peer 侧持久事实的证明：其启动布尔已定，
	// 只有 second 启动时经协议入口补写的 Overlapped 能改变终态报告。
}

// TestUnreliableUsageMarksCapUnavailableAndKeepsIssuing：存在不可计量派发（未
// 知用量）且已知总量未达 cap——cap 状态诚实 UNAVAILABLE，闸不拦截（已知总量
// 未达下界），后续治理派发照常签出。
func TestUnreliableUsageMarksCapUnavailableAndKeepsIssuing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTokenCap(t, root, `{"engineDispatchMaxTotalTokens":100}`)
	p := commandPlan()
	base := p.Batches[0]
	p.Batches = nil
	for _, id := range []string{"parser", "command"} {
		b := base
		b.ID = id
		b.Subtasks = []protocol.DevelopmentSubtask{{ID: id + "-body", Description: "implement " + id}}
		b.Artifacts = []string{id + ".go"}
		if id == "command" {
			b.DependsOn = []string{"parser"}
		}
		p.Batches = append(p.Batches, b)
	}
	d := plannedValidationDriverAt(t, root, meteredProvider, nil, p, "blackbox")
	first, input := planBatchAction(d, "parser")
	// 可计量 provider 下构造不可计量派发：stop 携带无法解析的 transcript。
	spawned, err := protocol.NewSpawnReceiptEvent(protocol.EventID("spawn-parser"), first, d.host(), "agent-parser", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = d.f.Submit(spawned); err != nil {
		t.Fatal(err)
	}
	observeMeteredStart(t, root, d.host(), "agent-parser")
	garbage := filepath.Join(root, "agent-parser.jsonl")
	writeTranscript(t, garbage, []string{"not a transcript line"})
	completeMeteredSource(t, root, d.host(), "agent-parser", garbage)
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: first, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "parser-digest", Validation: &protocol.ValidationResult{Binding: input.Binding, Development: batchEvidence(input), Status: "PASS"}}})
	if d.run.GuardBudget == nil || d.run.GuardBudget.TokenCapStatus != protocol.TokenCapUnavailable {
		t.Fatalf("unreliable usage advertised cap = %+v", d.run.GuardBudget)
	}
	if got := d.run.Cost.Dispatches[first].Source; got != "unavailable" {
		t.Fatalf("precondition: parser dispatch source = %s", got)
	}
	// 已知总量（0）未达 cap：闸不拦截，依赖批次照常签出。
	d.drive()
	commandID, _ := planBatchAction(d, "command")
	if commandID == "" || commandID == first {
		t.Fatalf("dependent batch not issued although the known total was below the cap: %q", commandID)
	}
}

// TestKnownTotalAtCapStillGatesWhenUsageUnreliable：存在不可计量派发且已知总
// 量已达 cap——状态 UNAVAILABLE（余量不可信），但闸保持已知总量下界语义：已
// 知量达上限仍零签发＋落预算处置 Ask（未知用量只会更多）。
func TestKnownTotalAtCapStillGatesWhenUsageUnreliable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTokenCap(t, root, `{"engineDispatchMaxTotalTokens":21}`)
	p := commandPlan()
	base := p.Batches[0]
	p.Batches = nil
	for _, id := range []string{"parser", "command"} {
		b := base
		b.ID = id
		b.Subtasks = []protocol.DevelopmentSubtask{{ID: id + "-body", Description: "implement " + id}}
		b.Artifacts = []string{id + ".go"}
		if id == "command" {
			b.DependsOn = []string{"parser"}
		}
		p.Batches = append(p.Batches, b)
	}
	d := plannedValidationDriverAt(t, root, meteredProvider, nil, p, "blackbox")
	first, input := planBatchAction(d, "parser")
	completeMeteredDispatch(t, d, root, first, "agent-parser", []string{
		`{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":3}}}`,
	}, &protocol.WorkerResultPayload{ActionID: first, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "parser-digest", Validation: &protocol.ValidationResult{Binding: input.Binding, Development: batchEvidence(input), Status: "PASS"}})
	// 第二条派发以不可解析 transcript 完成：来源 unavailable（未知用量）、
	// 已知总量停在被真实计量的 21。
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := before.State.Cost.Dispatches[first]
	if entry.Source != "transcript" || entry.TotalInputTokens+entry.OutputTokens != 21 || before.State.Results[first].Outcome != protocol.OutcomePass {
		t.Fatal("parser completion did not retain the original metered 21 tokens")
	}
	qa := ""
	for pendingID, pending := range before.State.PendingActions {
		if input, ok := before.State.ValidationInputs(d.f.definition)[pendingID]; ok && input.Handler == "engine.qa.design.v6" && input.Manifest.Binding.ReviewKind == "BLACKBOX" && pending.AttemptID == before.State.Attempts[pending.Task].ID {
			qa = pendingID
			break
		}
	}
	if qa == "" {
		t.Fatal("no second dispatch in flight")
	}
	spawned, err := protocol.NewSpawnReceiptEvent(protocol.EventID("spawn-qa"), qa, d.host(), "agent-qa", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = d.f.Submit(spawned); err != nil {
		t.Fatal(err)
	}
	observeMeteredStart(t, root, d.host(), "agent-qa")
	garbage := filepath.Join(root, "agent-qa.jsonl")
	writeTranscript(t, garbage, []string{"not a transcript line"})
	completeMeteredSource(t, root, d.host(), "agent-qa", garbage)
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: qa, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "qa-digest"}})
	if d.run.GuardBudget == nil || d.run.GuardBudget.TokenCapStatus != protocol.TokenCapUnavailable {
		t.Fatalf("unreliable usage advertised cap = %+v", d.run.GuardBudget)
	}
	if got := d.run.Cost.Dispatches[qa].Source; got != "unavailable" {
		t.Fatalf("precondition: qa dispatch source = %s", got)
	}
	// 已知总量 21 已达 cap：下界闸仍零签发（依赖批次不得签出）＋Ask 在地。
	d.drive()
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	for pendingID, pending := range snap.State.PendingActions {
		if input := snap.State.ValidationInputs(d.f.definition)[pendingID]; input.Development != nil && input.Development.Batch.ID == "command" {
			t.Fatalf("dependent batch issued while the known total was at the cap: %+v", pending)
		}
	}
	asked := false
	for _, ask := range snap.State.PendingAsks {
		if ask.Validation != nil && ask.Validation.Kind == protocol.KindDispatchBudgetDisposition && !ask.Resolved {
			asked = true
		}
	}
	if !asked {
		t.Fatal("the known-total gate at the cap did not land a budget disposition ask")
	}
}

func TestStagedNativeCostObservationSettlesThroughDrive(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"PENDING", "READY", "UNAVAILABLE", "READY_BARRIER"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			testkit.ConfigureNativePaths(t, root, meteredProvider)
			if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
				t.Fatal(err)
			}
			f, r, err := Start(StartOptions{Capacity: 1, Root: root, VCSRunner: newFrozenGitRepo(), Request: StartRequest{RunID: "staged-native-ready", Route: "regular", Provider: meteredProvider, BaseSnapshot: frozenSnapshot, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", meteredProvider)})
			if err != nil {
				t.Fatal(err)
			}
			r, err = f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			action := ""
			for id := range r.PendingActions {
				action = id
				break
			}
			if action == "" {
				t.Fatal("no dispatch")
			}
			submit := func(ev protocol.Event) {
				if _, _, err := f.Submit(ev); err != nil {
					t.Fatal(err)
				}
			}
			unknown, err := protocol.NewSpawnReceiptEvent("unknown", action, meteredProvider, "transport-correlation", protocol.SpawnStatusUnknown)
			if err != nil {
				t.Fatal(err)
			}
			submit(unknown)
			for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
				ev, err := protocol.NewCorrelatedLifecycleEvent(protocol.EventID("lc-"+edge), meteredProvider, "transport-correlation", "actual-agent", edge)
				if err != nil {
					t.Fatal(err)
				}
				submit(ev)
			}
			result, err := protocol.NewReviewWorkerResultEvent("result", action, meteredProvider, "sha256:result", protocol.ReviewResultEvidence{Action: review.ProductReview, DispatchID: action, RequirementRevision: testReceipt().RequirementRevision, Status: "PASS", Findings: []review.Finding{}})
			if err != nil {
				t.Fatal(err)
			}
			accepted, _, err := f.Submit(result)
			if err != nil {
				t.Fatal(err)
			}
			if accepted.Status != protocol.StatusCostPending {
				t.Fatalf("missing source not held: %+v", accepted)
			}

			before, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if source == "PENDING" {
				if _, err = f.Drive(); err != nil {
					t.Fatal(err)
				}
				if _, err = f.Drive(); err != nil {
					t.Fatal(err)
				}
				after, err := f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if before.Revision != after.Revision || len(after.State.Cost.Dispatches) != 0 || len(after.State.Results) != 0 {
					t.Fatalf("pending source changed accepted facts: before=%+v after=%+v", before, after)
				}
				return
			}
			if source == "READY_BARRIER" {
				request, err := protocol.NewRequirementRequestEvent("source-barrier", protocol.RequirementDecisionPackage{CandidateRevision: "sha256:new-document", DeltaDigest: "sha256:clarification", ImpactDigest: "sha256:preserved", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
				if err != nil {
					t.Fatal(err)
				}
				submit(request)
			}
			if source == "UNAVAILABLE" {
				if err := lifecycle.ObserveNativeCompletion(root, "staged-native-ready", action, meteredProvider, "actual-agent"); err != nil {
					t.Fatal(err)
				}
			} else {
				transcript := filepath.Join(root, "native.jsonl")
				writeTranscript(t, transcript, []string{`{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":1}}}`})
				completeMeteredSource(t, root, meteredProvider, "actual-agent", transcript)
			}
			resolved := CostSourceBridge(root, "staged-native-ready")(protocol.CostSourceRef{RunID: "staged-native-ready", ActionID: action, Provider: meteredProvider, Correlation: "transport-correlation"})
			want := protocol.CostSourceReady
			if source == "UNAVAILABLE" {
				want = protocol.CostSourceUnavailable
			}
			if resolved.State != want {
				t.Fatalf("native source state=%+v want=%s", resolved, want)
			}

			r, err = f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			snap, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if source == "READY_BARRIER" {
				if snap.State.Results[action].Outcome != "" || snap.State.StagedResults[action].PayloadDigest != "sha256:result" || snap.State.ReviewSeries.ProductReview.Completed != 0 {
					t.Fatal("source observation crossed the requirement barrier")
				}
				token, err := f.engine.Freshness("source-barrier")
				if err != nil {
					t.Fatal(err)
				}
				decision, err := protocol.NewDecideEvent("source-barrier-decision", "source-barrier", token, "abort")
				if err != nil {
					t.Fatal(err)
				}
				submit(decision)
				snap, err = f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
			}
			if got := snap.State.Results[action]; got.Outcome != protocol.OutcomePass || got.PayloadDigest != "sha256:result" {
				t.Fatalf("ready original result not accepted: %+v", got)
			}
			if receipt := snap.State.SpawnReceipts[action]; receipt.Status != protocol.SpawnStatusAttached || receipt.LifecycleIdentity != "actual-agent" || receipt.Correlation != "transport-correlation" {
				t.Fatalf("native binding changed: %+v", receipt)
			}
			entry := snap.State.Cost.Dispatches[action]
			if source == "UNAVAILABLE" {
				if entry.Source != "unavailable" || entry.Reason == "" {
					t.Fatalf("missing native source fabricated consumption: %+v", entry)
				}
			} else if entry.Source != "transcript" || entry.InputCacheMissTokens != 5 || entry.InputCacheHitTokens != 1 || entry.OutputTokens != 2 {
				t.Fatalf("original consumption changed: %+v", entry)
			}
			if _, err = f.Drive(); err != nil {
				t.Fatal(err)
			}
			if replay, _, err := f.Submit(result); err != nil || !reflect.DeepEqual(replay, accepted) {
				t.Fatalf("original event replay=%+v err=%v", replay, err)
			}
			after, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after.State.Cost, snap.State.Cost) || !reflect.DeepEqual(after.State.Results[action], snap.State.Results[action]) {
				t.Fatal("drive/replay counted the original result or usage twice")
			}
		})
	}
}

func TestAttachedReceiptPreservesAuthorizedResumeIdentity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	testkit.ConfigureNativePaths(t, root, meteredProvider)
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
		t.Fatal(err)
	}
	f, r, err := Start(StartOptions{Capacity: 1, Root: root, VCSRunner: newFrozenGitRepo(), Request: StartRequest{RunID: "staged-native-ready", Route: "regular", Provider: meteredProvider, BaseSnapshot: frozenSnapshot, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", meteredProvider)})
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	action := ""
	for id := range r.PendingActions {
		action = id
		break
	}
	if action == "" {
		t.Fatal("no dispatch")
	}
	submit := func(ev protocol.Event) {
		if _, _, err := f.Submit(ev); err != nil {
			t.Fatal(err)
		}
	}
	unknown, err := protocol.NewSpawnReceiptEvent("unknown", action, meteredProvider, "transport-correlation", protocol.SpawnStatusUnknown)
	if err != nil {
		t.Fatal(err)
	}
	submit(unknown)
	for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
		ev, err := protocol.NewCorrelatedLifecycleEvent(protocol.EventID("lc-"+edge), meteredProvider, "transport-correlation", "actual-agent", edge)
		if err != nil {
			t.Fatal(err)
		}
		submit(ev)
	}

	before, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	pending := before.State.PendingActions[action]
	if input, current := before.State.WorkerInput(action, f.definition); !current || input.Pending.AttemptID != pending.AttemptID {
		t.Fatalf("fixture has no original current canonical worker: %+v current=%t", input, current)
	}
	receipt := before.State.SpawnReceipts[action]
	if receipt.Status != protocol.SpawnStatusAttached || receipt.LifecycleIdentity != "actual-agent" {
		t.Fatalf("not uniquely attached: %+v", receipt)
	}
	recovery, _, err := f.engine.RecoverAttempt(action, protocol.Interruption{InterruptionReason: "native outcome interrupted before completion"}, protocol.Observation{})
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Action != protocol.RecoveryAsk {
		t.Fatalf("unknown-cause recovery did not retain its typed Ask: %+v", recovery)
	}
	token, err := f.engine.Freshness(protocol.RequestID(recovery.RequestID))
	if err != nil {
		t.Fatal(err)
	}
	ev, err := protocol.NewDecideEvent("resume-authorized", protocol.RequestID(recovery.RequestID), token, "resume")
	if err != nil {
		t.Fatal(err)
	}
	submit(ev)
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	intent := protocol.HostActionIntent{}
	for _, value := range snap.State.PendingHostActions {
		if value.Resume != nil && value.Resume.WorkerActionID == action {
			intent = value
			break
		}
	}
	if intent.ActionID == "" || intent.Resume.Identity != "actual-agent" || intent.Resume.AttemptID != pending.AttemptID {
		t.Fatalf("resume lost original native binding: %+v", intent)
	}
	if input, current := snap.State.WorkerInput(action, f.definition); !current || input.Pending.AttemptID != pending.AttemptID {
		t.Fatalf("original canonical worker no longer current: %+v current=%t", input, current)
	}
	input, identity, err := snap.State.WorkerExecutionIdentity(action, pending.AttemptID, intent.ActionID, f.definition)
	t.Logf("receipt=%+v recovery=%+v pending=%+v intent=%+v input=%+v admittedIdentity=%q error=%v", receipt, recovery, pending, intent, input, identity, err)
	if err != nil || identity != "actual-agent" || input.Pending.ActionID != action {
		t.Error("authorized original resume rejected its uniquely attached native identity")
	}
}
