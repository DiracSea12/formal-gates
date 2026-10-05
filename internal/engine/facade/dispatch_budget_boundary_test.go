package facade

// 派发预算边界（token cap 到达、有限 retry 余量耗尽）的公开链行为：边界＝
// 零签发/零宿主效果＋DISPATCH_BUDGET_DISPOSITION Ask；承载观察事实的提交事务
// 照常成立；一次 continue 授权只解锁一个被阻效果；UNKNOWN 回执 attach 后以解
// 析出的真实身份完成精确计量；终态收口移除 lifecycle 活动标记。

import (
	"os"
	"path/filepath"
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
	if _, err := lifecycle.Capture(root, d.host(), "SubagentStart", []byte(`{"agent_id":"`+agent+`"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Capture(root, d.host(), "SubagentStop", []byte(`{"agent_id":"`+agent+`","agent_transcript_path":"`+transcript+`"}`)); err != nil {
		t.Fatal(err)
	}
	return d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: result})
}

// TestAutomaticResumeAtReachedTokenCapLandsBudgetAsk：cap 被真实用量达到后，
// 在途治理派发的瞬态自动恢复不再派发 resume，落 DISPATCH_BUDGET_DISPOSITION
// Ask（继续执行只能由用户授权一次有限增量）。
func TestAutomaticResumeAtReachedTokenCapLandsBudgetAsk(t *testing.T) {
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
	for pendingID := range snap.State.PendingActions {
		qaID = pendingID
		break
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
	fp, err := d.f.engine.ObserveFingerprint()
	if err != nil {
		t.Fatal(err)
	}
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
	// PASS 完成的执行按 removeTaskBookkeeping 清理；观察事实的持久载体是
	// Results 账本（不在暂存）。
	record, recorded := snap.State.Results[first]
	if !recorded || record.Outcome != protocol.OutcomePass {
		t.Fatalf("boundary dropped the accepted result: %+v", record)
	}
	if _, staged := snap.State.StagedResults[first]; staged {
		t.Fatal("accepted result stayed staged at the boundary")
	}
	if _, issued := snap.State.PendingActions[first]; issued {
		t.Fatal("boundary re-issued the completed action")
	}
	for pendingID, pending := range snap.State.PendingActions {
		if pending.Task.Scope == "plan:command:command" {
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
	if _, _, err := d.f.engine.RecoverAttempt(first, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, emptyFingerprint()); err != nil {
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
	observed, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), first, protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	d.submit(observed)
	receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), resumeIntent.ActionID, resumeIntent.Operation, d.host(), resumeIntent.Correlation, resumeIntent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: first, Event: protocol.LifecycleStart})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(receipt)
	reviewTranscript := filepath.Join(root, "review-worker.jsonl")
	writeTranscript(t, reviewTranscript, []string{`{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}}`})
	if _, err := lifecycle.Capture(root, d.host(), "SubagentStop", []byte(`{"agent_id":"`+first+`","agent_transcript_path":"`+reviewTranscript+`"}`)); err != nil {
		t.Fatal(err)
	}
	reviewPass, err := protocol.NewReviewWorkerResultEvent(d.eventID(), first, d.host(), string(d.eventID()), protocol.ReviewResultEvidence{
		Action: review.ProductReview, DispatchID: first, RequirementRevision: d.revision, Status: "PASS",
		Findings:     []review.Finding{},
		Verification: review.OperatorVerification{Checks: []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, Evidence: "checked the exact submitted requirement and all listed acceptance conditions"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(reviewPass)
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
	if _, _, err := d.f.engine.RecoverAttempt(next, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, emptyFingerprint()); err != nil {
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
	root := t.TempDir()
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
	unknown, err := protocol.NewSpawnReceiptEvent("spawn-unknown", action, meteredProvider, "corr-track-1", protocol.SpawnStatusUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.Submit(unknown); err != nil {
		t.Fatal(err)
	}
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
	// 宿主 sidecar 先按真实身份写 stop＋transcript，绑定创建晚于事件到达。
	transcript := filepath.Join(root, "agent-nine.jsonl")
	writeTranscript(t, transcript, []string{`{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":1}}}`})
	if _, err := lifecycle.Capture(root, meteredProvider, "SubagentStart", []byte(`{"agent_id":"agent-9"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Capture(root, meteredProvider, "SubagentStop", []byte(`{"agent_id":"agent-9","agent_transcript_path":"`+transcript+`"}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.engine.ReconcileUnknownReceipt(action, emptyFingerprint()); err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.State.SpawnReceipts[action].LifecycleIdentity; got != "agent-9" {
		t.Fatalf("attached identity = %q", got)
	}
	result, err := protocol.NewReviewWorkerResultEvent("result-attached", action, meteredProvider, "sha256:result", protocol.ReviewResultEvidence{
		Action: review.ProductReview, DispatchID: action, RequirementRevision: "req-rev", Status: "PASS",
		Findings:     []review.Finding{},
		Verification: review.OperatorVerification{Checks: []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, Evidence: "checked the current requirement and exact result"},
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
}

// TestOwnerOverlapDetectsLegacyActiveRun：同一 owner transcript 上存在 ACTIVE
// legacy run（.gates/tmp 平坦形状 state.json）时，owner 区间记 UNAVAILABLE 零
// 数字（迁移期两代并存可见）。
func TestOwnerOverlapDetectsLegacyActiveRun(t *testing.T) {
	root := t.TempDir()
	transcript := filepath.Join(root, "shared-owner.jsonl")
	writeTranscript(t, transcript, []string{`{"type":"assistant","uuid":"base-1","message":{"usage":{"input_tokens":9,"output_tokens":4,"cache_read_input_tokens":1}}}`})
	legacyDir := filepath.Join(root, ".gates", "tmp", "legacy-run")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyState := `{"runId":"legacy-run","flow":"lightweight","status":"ACTIVE","ownerTranscript":"` + transcript + `","ownerProvider":"` + meteredProvider + `"}`
	if err := os.WriteFile(filepath.Join(legacyDir, "state.json"), []byte(legacyState), 0o600); err != nil {
		t.Fatal(err)
	}
	f, _, err := Start(StartOptions{Root: root, Owner: OwnerBinding{Transcript: transcript, Provider: meteredProvider}, Request: StartRequest{RunID: "engine-owner", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.Cost.Owner == nil || run.Cost.Owner.Source != "unavailable" || run.Cost.Owner.TotalInputTokens != 0 || run.Cost.Owner.OutputTokens != 0 {
		t.Fatalf("owner interval = %+v, want UNAVAILABLE with zero numbers", run.Cost.Owner)
	}
}

// TestTerminalRunEndsLifecycleAttribution：run 终态收口移除 lifecycle 活动标
// 记（幂等）——终态后晚到的宿主事件归属无 run，不再落入该 run 的事件树。
func TestTerminalRunEndsLifecycleAttribution(t *testing.T) {
	root := t.TempDir()
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
	captured, err := lifecycle.Capture(root, meteredProvider, "SubagentStart", []byte(`{"agent_id":"late-agent"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(captured.Roots) != 0 {
		t.Fatalf("late host event was attributed to the terminal run: %+v", captured)
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

// resumeAndComplete 执行一次公开 ResumeAgent 并提交其 EXECUTED 回执（恢复执
// 行经 accountResumeExecution 计入承载账本）。
func resumeAndComplete(d *validationDriver, action string) error {
	d.t.Helper()
	intent, _, err := d.f.engine.ResumeAgent(action, action, emptyFingerprint())
	if err != nil {
		return err
	}
	ev, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), intent.ActionID, intent.Operation, d.host(), "resume-"+intent.ActionID, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: action, Event: protocol.LifecycleStart})
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(ev)
	return nil
}

// TestEndedAwaitingResumeCountsAndExhaustsBudget：TASK_PROGRESS TERMINAL 后仍
// 等待结果的执行（EndedAttempts，PendingActions 已清理）走恢复——EXECUTED 回
// 执在承载条目的账本计数（Attempts 0→1），有限余量耗尽后第二次恢复被拒并落预
// 算处置 Ask（恢复准入与恢复记账共用同一任务身份解析）。
func TestEndedAwaitingResumeCountsAndExhaustsBudget(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	id, input := d.action("engine.qa.design")
	d.receipt(id)
	start, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), id, protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	d.submit(start)
	progress, err := protocol.NewTaskEvent(d.eventID(), input.Binding.Task, d.run.PendingActions[id].AttemptID, runtime.TaskTerminal)
	if err != nil {
		t.Fatal(err)
	}
	d.submit(progress)
	ended := d.run.EndedAttempts[input.Binding.Task]
	if !ended.AwaitingResult || ended.Attempts != 0 {
		t.Fatalf("precondition: ended attempt = %+v", ended)
	}
	if err := resumeAndComplete(d, id); err != nil {
		t.Fatalf("first resume of the ended-awaiting execution blocked: %v", err)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	counted := snap.State.EndedAttempts[input.Binding.Task]
	if counted.Attempts != 1 || !counted.AwaitingResult {
		t.Fatalf("resume execution not counted in the holding ledger: %+v", counted)
	}
	if _, _, err := d.f.engine.ResumeAgent(id, id, emptyFingerprint()); err == nil {
		t.Fatal("second resume accepted after the finite budget was consumed")
	}
	shown, err := d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	d.run = shown
	if ask := budgetAsk(d); ask.RequestID == "" {
		t.Fatal("exhaustion boundary did not land a budget disposition ask")
	}
	snap, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	final := snap.State.EndedAttempts[input.Binding.Task]
	if final.Attempts != 1 || final.MaxAttempts != 2 {
		t.Fatalf("rejection changed the attempt ledger: %+v", final)
	}
}

// TestSingleRetryExtensionAcrossBudgetAndRetryAsks：同一逻辑任务的一次追加
// retry slot 是一个授权事实——预算处置 retry 与 RETRY approve 任一通道行使后，
// 另一通道对同任务的追加即被拒（两个方向都要；max 不得被推到 4）。
func TestSingleRetryExtensionAcrossBudgetAndRetryAsks(t *testing.T) {
	t.Run("budget retry first blocks the RETRY extension", func(t *testing.T) {
		d := newValidationDriver(t, "blackbox")
		id, _ := d.action("engine.qa.design")
		d.receipt(id)
		start, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), id, id, protocol.LifecycleStart)
		if err != nil {
			t.Fatal(err)
		}
		d.submit(start)
		if err := resumeAndComplete(d, id); err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.f.engine.ResumeAgent(id, id, emptyFingerprint()); err == nil {
			t.Fatal("missing exhaustion boundary")
		}
		d.run, err = d.f.Show()
		if err != nil {
			t.Fatal(err)
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
		id, input := d.action("engine.qa.design")
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
		reissued, reinput := d.action("engine.qa.design")
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
		if _, _, err := d.f.engine.ResumeAgent(reissued, reissued, emptyFingerprint()); err == nil {
			t.Fatal("resume of the carried ledger was expected to hit the boundary")
		}
		d.run, err = d.f.Show()
		if err != nil {
			t.Fatal(err)
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
// 启动 run 经协议幂等入口把重叠补写进先启动 run 的 owner 投影（其启动布尔不随
// 后续重叠更新），双方终态都记 UNAVAILABLE 零数字（legacy beginOwnerCost 的双
// 边标记语义）。
func TestOwnerOverlapMarksBothRunsUnavailable(t *testing.T) {
	root := t.TempDir()
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
	if _, err := lifecycle.Capture(root, d.host(), "SubagentStart", []byte(`{"agent_id":"agent-parser"}`)); err != nil {
		t.Fatal(err)
	}
	garbage := filepath.Join(root, "agent-parser.jsonl")
	writeTranscript(t, garbage, []string{"not a transcript line"})
	if _, err := lifecycle.Capture(root, d.host(), "SubagentStop", []byte(`{"agent_id":"agent-parser","agent_transcript_path":"`+garbage+`"}`)); err != nil {
		t.Fatal(err)
	}
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
	qa := ""
	for pendingID := range d.run.PendingActions {
		qa = pendingID
		break
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
	if _, err := lifecycle.Capture(root, d.host(), "SubagentStart", []byte(`{"agent_id":"agent-qa"}`)); err != nil {
		t.Fatal(err)
	}
	garbage := filepath.Join(root, "agent-qa.jsonl")
	writeTranscript(t, garbage, []string{"not a transcript line"})
	if _, err := lifecycle.Capture(root, d.host(), "SubagentStop", []byte(`{"agent_id":"agent-qa","agent_transcript_path":"`+garbage+`"}`)); err != nil {
		t.Fatal(err)
	}
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
	for _, pending := range snap.State.PendingActions {
		if pending.Task == snap.State.DispatchBindings[qa].Task {
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
