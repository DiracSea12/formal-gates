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
	f, r, err := Start(StartOptions{Capacity: 1, Root: root, Request: StartRequest{RunID: "unknown-attach", Route: "regular", Provider: meteredProvider, BaseSnapshot: "native-base", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "candidate"}})
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
	f, _, err := Start(StartOptions{Root: root, Owner: OwnerBinding{Transcript: transcript, Provider: meteredProvider}, Request: StartRequest{RunID: "engine-owner", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "candidate"}})
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
	f, run, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "terminal-lifecycle", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "candidate"}})
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
