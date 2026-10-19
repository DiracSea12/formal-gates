package facade

import (
	"formal-gates/internal/engine/testkit"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

// plannedThreeBatchDriver 构造三批确认计划（parser 独立、command 依赖
// parser、independent 独立）的验证管线 run。
func plannedThreeBatchDriver(t *testing.T, gates ...string) *validationDriver {
	t.Helper()
	p := commandPlan()
	base := p.Batches[0]
	p.Batches = nil
	for _, id := range []string{"parser", "command", "independent"} {
		b := base
		b.ID = id
		b.Subtasks = []protocol.DevelopmentSubtask{{ID: id + "-body", Description: "implement " + id}}
		b.Artifacts = []string{id + ".go"}
		if id == "command" {
			b.DependsOn = []string{"parser"}
		}
		p.Batches = append(p.Batches, b)
	}
	return plannedValidationDriver(t, nil, p, gates...)
}

// TestGuardAuthorizedSetCountsConfirmedDependentBatchesStably：已确认授权全集
// 立即覆盖全部批次（依赖未 Ready 同样计数）与 QA 设计槽，完成前驱后 basis
// digest 不随签发进度漂移（额度事实源＝确认计划，非已签发事实）。
func TestGuardAuthorizedSetCountsConfirmedDependentBatchesStably(t *testing.T) {
	t.Parallel()
	d := plannedThreeBatchDriver(t, "blackbox")
	if d.run.GuardBudget == nil {
		t.Fatal("guard envelope is not engaged for the confirmed plan")
	}
	before := *d.run.GuardBudget
	if before.RequestAllowance != 4 {
		t.Fatalf("authorized set = %d slots, want 3 batches + QA design slot = 4", before.RequestAllowance)
	}
	independent, independentInput := planBatchAction(d, "independent")
	d.business(independent, protocol.ValidationResult{Binding: independentInput.Binding, Development: batchEvidence(independentInput), Status: "PASS"})
	id, input := planBatchAction(d, "parser")
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Development: batchEvidence(input), Status: "PASS"})
	d.drive()
	after := *d.run.GuardBudget
	if after.RequestAllowance != before.RequestAllowance || after.BudgetBasisDigest != before.BudgetBasisDigest {
		t.Fatalf("confirmed-plan basis drifted after completing a prerequisite: %+v -> %+v", before, after)
	}
	if after.UsedRequests != before.UsedRequests+1 {
		t.Fatalf("issuing the dependent batch must consume a request slot: %d -> %d", before.UsedRequests, after.UsedRequests)
	}
}

// TestResumeBudgetBoundaryLandsAskAndOneRetryUnlocksOnce：同一逻辑动作的
// resume 消耗有限 retry 余量——首次放行并在 EXECUTED 回执计入一次执行；到达
// 上限后拒绝并落 DISPATCH_BUDGET_DISPOSITION Ask；「retry」授权一次后续用一
// 次，再次到达上限重新拒绝（不累积）。
func TestResumeBudgetBoundaryLandsAskAndOneRetryUnlocksOnce(t *testing.T) {
	t.Parallel()
	d := newValidationDriver(t, "blackbox")
	id, _ := d.action("engine.qa.design.v6")
	d.receipt(id)
	// RESUME 回执要求 durable lifecycle buffer 里有该身份的 start 事件。
	start, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), DefaultProvider, id, id, protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	d.submit(start)
	// blockedResume 触发瞬态恢复并断言边界零签发（扫描全部宿主 intent，
	// 错误 correlation 的越权 resume 也会暴露）。
	blockedResume := func() error {
		if _, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{}); err != nil {
			return err
		}
		snap, err := d.f.engine.Load()
		if err != nil {
			return err
		}
		for _, intent := range snap.State.PendingHostActions {
			if intent.Operation == protocol.HostActionResumeAgent {
				t.Fatalf("finite budget signed an extra resume: %+v", intent)
			}
		}
		return nil
	}
	if err := resumeAndComplete(d, id); err != nil {
		t.Fatalf("first resume within budget blocked: %v", err)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, attempt := range snap.State.Attempts {
		if attempt.ActionID == id && attempt.Attempts != 1 {
			t.Fatalf("resume execution counted %d times, want 1", attempt.Attempts)
		}
	}
	if err := blockedResume(); err != nil {
		t.Fatal(err)
	}
	if r, err := d.f.Show(); err != nil {
		t.Fatal(err)
	} else {
		d.run = r
	}
	var ask PendingAskFace
	for _, face := range d.run.PendingAsks {
		if face.Validation != nil && face.Validation.Kind == protocol.KindDispatchBudgetDisposition {
			ask = face
		}
	}
	if ask.RequestID == "" {
		t.Fatal("budget boundary did not land a DISPATCH_BUDGET_DISPOSITION ask")
	}
	var hasRetry bool
	for _, option := range ask.Options {
		hasRetry = hasRetry || option.ID == "retry"
	}
	if !hasRetry {
		t.Fatalf("budget ask options = %+v", ask.Options)
	}
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{
		Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "retry",
		Validation: &protocol.ValidationDecision{Reason: "one more resume for the interrupted agent", Digest: ask.Validation.Digest, StateRevision: d.run.Revision},
	}})
	if err := resumeAndComplete(d, id); err != nil {
		t.Fatalf("authorized retry resume blocked: %v", err)
	}
	if err := blockedResume(); err != nil {
		t.Fatal(err)
	}
	d.run, err = d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	budgetAsk(d)
}

func writeTranscript(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestCostPublicChainHoldsPendingThenRecordsExactUsage：公开链（不手写
// sidecar）——Start→Drive→SPAWNED 建立绑定，宿主 start 事件被接收，stop 未到
// 时结果滞留 COST_PENDING；stop 携 transcript 后下一事务完成并记精确用量。
func TestCostPublicChainHoldsPendingThenRecordsExactUsage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	testkit.ConfigureNativePaths(t, root, meteredProvider)
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(root, "agent-one.jsonl")
	writeTranscript(t, transcript, []string{
		`{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":3}}}`,
	})
	f, r, err := Start(StartOptions{Capacity: 1, Root: root, VCSRunner: testkit.NewFrozenGitRepo(), Request: StartRequest{RunID: "cost-chain", Route: "regular", Provider: "claude-code", BaseSnapshot: testkit.FrozenSnapshot, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", "claude-code")})
	if err != nil {
		t.Fatal(err)
	}
	if r, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	var action string
	for id := range r.PendingActions {
		action = id
		break
	}
	if action == "" {
		t.Fatal("no dispatch")
	}
	spawned, err := protocol.NewSpawnReceiptEvent("spawn", action, "claude-code", "agent-one", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.Submit(spawned); err != nil {
		t.Fatal(err)
	}
	observeMeteredStart(t, root, "claude-code", "agent-one")
	bridge := CostSourceBridge(root, "cost-chain")
	if source := bridge(protocol.CostSourceRef{RunID: "cost-chain", ActionID: action, Provider: "claude-code", Correlation: "agent-one"}); source.State != protocol.CostSourcePending {
		t.Fatalf("dispatch without stop resolved %s, want PENDING", source.State)
	}
	result, err := protocol.NewReviewWorkerResultEvent("result", action, "claude-code", "sha256:result", protocol.ReviewResultEvidence{
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
	if accepted.Status != protocol.StatusCostPending {
		t.Fatalf("result without stop status = %s, want COST_PENDING", accepted.Status)
	}
	completeMeteredSource(t, root, "claude-code", "agent-one", transcript)

	// stop 证据落定后的下一事务完成滞留结果（同载荷换事件 ID 重走公开接纳）。
	accepted, _, err = f.Submit(protocol.Event{ID: "result-after-stop", Kind: protocol.KindWorkerResult, Result: result.Result})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != "ACCEPTED" {
		t.Fatalf("result after stop status = %s, want ACCEPTED", accepted.Status)
	}
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := snap.State.Cost.Dispatches[action]
	if entry.Source != "transcript" || entry.InputCacheMissTokens != 11 || entry.InputCacheHitTokens != 3 || entry.OutputTokens != 7 {
		t.Fatalf("exact usage not recorded from the public chain: %+v", entry)
	}
}

// TestOwnerBaselineAndTerminalDelta：来源稳定的 owner 在 start 取基线、终态量
// 差值（不含启动前历史）；重复终态处理幂等不重复累计。
func TestOwnerBaselineAndTerminalDelta(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	configureLightweight(t, root)
	transcript := filepath.Join(root, "owner.jsonl")
	writeTranscript(t, transcript, []string{
		`{"type":"assistant","uuid":"base-1","message":{"usage":{"input_tokens":100,"output_tokens":40,"cache_read_input_tokens":10}}}`,
	})
	f, run, err := Start(StartOptions{Root: root, Owner: OwnerBinding{Transcript: transcript, Provider: "claude-code"}, Request: StartRequest{RunID: "owner-run", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "ACTIVE" {
		t.Fatalf("run status = %s", run.Status)
	}
	writeTranscript(t, transcript, []string{
		`{"type":"assistant","uuid":"base-1","message":{"usage":{"input_tokens":100,"output_tokens":40,"cache_read_input_tokens":10}}}`,
		`{"type":"assistant","uuid":"delta-1","message":{"usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":1}}}`,
	})
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "COMPLETE" {
		t.Fatalf("run status = %s", run.Status)
	}
	owner := run.Cost.Owner
	if owner == nil || owner.Source != "transcript" {
		t.Fatalf("owner delta = %+v, want precise transcript source", owner)
	}
	if owner.InputCacheMissTokens != 5 || owner.InputCacheHitTokens != 1 || owner.OutputTokens != 2 || owner.TotalInputTokens != 6 {
		t.Fatalf("owner delta counted pre-start history: %+v", owner)
	}
	first, err := f.readSummary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	second, err := f.readSummary()
	if err != nil {
		t.Fatal(err)
	}
	if first.Cost.Owner == nil || second.Cost.Owner == nil || *first.Cost.Owner != *second.Cost.Owner {
		t.Fatalf("terminal owner recording is not idempotent: %+v vs %+v", first.Cost.Owner, second.Cost.Owner)
	}
}

// TestOwnerMissingBaselineMarksUnavailable：基线来源缺失（owner transcript
// 不存在）＝UNAVAILABLE 零数字，不写估算；同 transcript 重叠的双边事实由
// dispatch_budget_boundary_test 的 TestOwnerOverlapMarksBothRunsUnavailable 覆盖。
func TestOwnerMissingBaselineMarksUnavailable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	configureLightweight(t, root)
	f, _, err := Start(StartOptions{Root: root, Owner: OwnerBinding{Transcript: filepath.Join(root, "missing.jsonl"), Provider: "claude-code"}, Request: StartRequest{RunID: "owner-missing", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.Cost.Owner == nil || run.Cost.Owner.Source != "unavailable" || run.Cost.Owner.TotalInputTokens != 0 {
		t.Fatalf("missing-baseline owner interval = %+v, want UNAVAILABLE with zero numbers", run.Cost.Owner)
	}
}

// TestTokenCapConfigurationStates：无配置＝UNAVAILABLE；非法配置＝明确报错；
// 有配置且达到＝治理派发零签发＋DISPATCH_BUDGET_DISPOSITION Ask，
// 「continue」一次授权解锁一个 Ready batch 后重新拦截（不累积）。
func TestTokenCapConfigurationStates(t *testing.T) {
	t.Parallel()
	t.Run("unavailable without config", func(t *testing.T) {
		d := plannedValidationDriverAt(t, t.TempDir(), DefaultProvider, nil, commandPlan(), "blackbox")
		if d.run.GuardBudget == nil || d.run.GuardBudget.TokenCapStatus != protocol.TokenCapUnavailable {
			t.Fatalf("token cap = %+v without configuration", d.run.GuardBudget)
		}
	})

	t.Run("invalid config is a clear start error", func(t *testing.T) {
		root := t.TempDir()
		writeTokenCap(t, root, `{"engineDispatchMaxTotalTokens":-3}`)
		_, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "bad-cap", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
		if err == nil || !strings.Contains(err.Error(), "token-cap.json") {
			t.Fatalf("invalid cap config error = %v", err)
		}
	})

	t.Run("reached cap blocks governed dispatch and one continue unlocks once", func(t *testing.T) {
		root := t.TempDir()
		// 可计量 provider：派发用量真实入账（来源 transcript、已知总量 0），
		// cap=0 被已知总量真实达到。不可计量来源（存在未知用量）下 cap 状态是
		// UNAVAILABLE、闸不拦截，由 dispatch_budget_boundary_test 的未知用量
		// 对照组覆盖。
		writeTokenCap(t, root, `{"engineDispatchMaxTotalTokens":0}`)
		d := plannedValidationDriverAt(t, root, meteredProvider, nil, commandPlan(), "blackbox")
		if d.run.GuardBudget == nil || d.run.GuardBudget.TokenCapStatus != protocol.TokenCapExceeded {
			t.Fatalf("reached cap status = %+v", d.run.GuardBudget)
		}
		if len(d.run.ValidationInputs) != 0 {
			t.Fatalf("reached cap issued governed dispatches: %+v", d.run.ValidationInputs)
		}
		var ask PendingAskFace
		for _, face := range d.run.PendingAsks {
			if face.Validation != nil && face.Validation.Kind == protocol.KindDispatchBudgetDisposition {
				ask = face
			}
		}
		if ask.RequestID == "" {
			t.Fatal("reached cap did not land a budget disposition ask")
		}
		d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{
			Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "continue",
			Validation: &protocol.ValidationDecision{Reason: "one bounded batch continues", Digest: ask.Validation.Digest, StateRevision: d.run.Revision},
		}})
		if len(d.run.ValidationInputs) == 0 {
			t.Fatal("continue authorization unlocked no governed dispatch")
		}
		snap, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		issued := len(snap.State.PendingActions)
		d.drive()
		snap, err = d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.State.PendingActions) > issued {
			t.Fatal("one-time continue authorization unlocked more than one batch")
		}
	})
}
