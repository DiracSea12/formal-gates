package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	definition "formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// guardPipelineFixture 构造一个验证管线就位的权威状态（拓扑、路线、确认计
// 划、case-set 与已签发 QA 派发事实），供额度派生与签发闸的行为测试使用。
func guardPipelineFixture(t *testing.T) (*Engine, *State, runtime.TaskKey) {
	t.Helper()
	dir := t.TempDir()
	store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:test-package"})
	if err != nil {
		t.Fatal(err)
	}
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(store, Config{InternalSteps: fixtureInternalSteps(), Definition: cd, Registry: definition.Registry(), Capacity: 4}, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatal(err)
	}
	state := NewState(*view)
	state.RunID = "guard-run"
	state.RunProvider = testProvider
	state.IntakeReceipt = &IntakeReceipt{
		Confirmation: IntakeConfirmationReceipt{RequirementSource: "requirements.md", RequirementRevision: "rev-1"},
		IntakeDigest: "sha256:intake", Revision: 1,
	}
	state.RequirementRevisions = []ConfirmedRevision{{
		Revision: "rev-1", Source: RequirementSourceIntake,
		Classification: RequirementClassificationIntake, StateRevision: 1,
	}}
	state.Topology = &TopologyBinding{Decision: "no-split", Note: "single lane"}
	state.RouteMode = "full"
	state.SelectedGates = []string{"blackbox", "gate.quality"}
	plan := &GranularityReview{Reason: "one batch", VerificationTier: "FINAL", Batches: []DevelopmentBatch{{
		ID: "batch-1", Deliverable: "command", PublicEntry: "app run",
	}}}
	state.Decisions = map[string]RecordedDecision{
		"route-1": {
			RequestID: "route-1", Control: ControlSelectRoute, Choice: "confirm", Revision: 5,
			Review: &ReviewDecisionPayload{Route: &RouteDecision{
				Mode: "full", Gates: []string{"blackbox", "gate.quality"},
				DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "act:readiness-plan", Digest: "sha256:plan"},
			}},
		},
	}
	state.Results = map[string]WorkerResult{
		"act:readiness-plan": {ActionID: "act:readiness-plan", Outcome: OutcomePass, Review: &ReviewResultEvidence{
			Action: review.StartReadiness, DispatchID: "act:readiness-plan",
			RequirementRevision: "rev-1", Status: "PASS",
			GranularityReview: plan, Verification: review.OperatorVerification{Evidence: "verified"},
		}},
	}
	qaTask := pipelineTask(cd, "engine.qa.execution", "wave-1:BLACKBOX")
	gateTask := pipelineTask(cd, "engine.gate", "wave-1:quality")
	state.Validation = &Validation{
		RequirementRevision: "rev-1", Generation: 1,
		Sets:     map[coverage.ReviewKind]*CaseSet{coverage.ReviewBlackbox: {Version: "case-v1", Status: "FROZEN"}},
		Findings: map[string]*ValidationFinding{}, Problems: map[string]*ValidationProblem{},
		Waves: []ValidationWave{{
			ID: "wave-1", Status: "COLLECTING", Scope: coverage.ScopeFull,
			Candidate:           coverage.ValidationCandidate{Identity: "native-1", Digest: "sha256:native-1"},
			RequirementRevision: "rev-1", Generation: 1, RouteDigest: state.validationRouteDigest(),
			Versions:       map[coverage.ReviewKind]string{coverage.ReviewBlackbox: "case-v1"},
			ExpectedDigest: "sha256:expected-1",
			Targets: map[string]*ValidationTarget{
				qaTask.String():   {Task: qaTask, Kind: "QA", Mode: coverage.ReviewBlackbox, Status: "PENDING"},
				gateTask.String(): {Task: gateTask, Kind: "GATE", Gate: "gate.quality", Status: "PENDING"},
			},
		}},
	}
	// 一条已签发 QA execution 事实：DispatchBindings 承载 actionID→task。
	state.DispatchBindings["act:qa-1"] = WorkBinding{Task: qaTask, WaveID: "wave-1", Candidate: "native-1", Generation: 1}
	return engine, state, qaTask
}

// TestGuardBudgetDerivesFromAuthoritativeState：包络就位后额度投影从权威
// 事实派生——授权全集（确认批次任务＋已建 wave 的两个 target）立即占 3 个基础
// 槽位、已签发 QA 派发占用其中 1 个；digest 随 case-set 版本变化（AC-058
// engine 侧）。
func TestGuardBudgetDerivesFromAuthoritativeState(t *testing.T) {
	engine, state, _ := guardPipelineFixture(t)
	budget, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if budget == nil || budget.RequestAllowance != 3 || budget.UsedRequests != 1 || budget.UsedRetries != 0 {
		t.Fatalf("budget = %+v", budget)
	}
	if !strings.HasPrefix(budget.BudgetBasisDigest, "sha256:") {
		t.Fatalf("digest = %q", budget.BudgetBasisDigest)
	}
	state.Validation.Sets[coverage.ReviewBlackbox].Version = "case-v2"
	changed, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatalf("derive after thaw: %v", err)
	}
	if changed.BudgetBasisDigest == budget.BudgetBasisDigest {
		t.Fatal("case-set version change did not change the basis digest")
	}
}

// TestGuardBudgetNotEngagedBeforePipeline：受理前/重走再确认窗口（拓扑或路
// 线未绑定）包络不生效——投影为 nil，审查系列派发不受影响。
func TestGuardBudgetNotEngagedBeforePipeline(t *testing.T) {
	engine, state, _ := guardPipelineFixture(t)
	state.Topology = nil
	budget, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if budget != nil {
		t.Fatalf("budget = %+v, want nil in the re-confirmation window", budget)
	}
}

// TestGuardIssuanceAllowsSourceBackedTask：wave target 派发（QA/GATE 证明来
// 源）在预算内照常放行。
func TestGuardIssuanceAllowsSourceBackedTask(t *testing.T) {
	engine, state, qaTask := guardPipelineFixture(t)
	gate := engine.guardIssuanceGate(state)
	if err := gate(decision.IssuedSet{{ActionID: "act:qa-2", Task: qaTask, Step: qaTask.Step}}); err != nil {
		t.Fatalf("backed candidate blocked: %v", err)
	}
}

// TestGuardIssuanceBlocksUnbackedTask：无授权来源的新 TaskKey（raw finding
// 式扩容）零签发、可区分拒绝；guard 自身无授权路径。
func TestGuardIssuanceBlocksUnbackedTask(t *testing.T) {
	engine, state, _ := guardPipelineFixture(t)
	cd := engine.cfg.Definition
	// 伪造一个不在任何 wave/plan/set 中的 repair 任务：唯一 repair 来源是
	// wave.Repair（adjudication 完成、收齐后建立），本状态没有。
	rogue := pipelineTask(cd, "engine.repair", "sha256:rogue-candidate")
	gate := engine.guardIssuanceGate(state)
	err := gate(decision.IssuedSet{{ActionID: "act:rogue", Task: rogue, Step: rogue.Step}})
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Code != CodeGuardAllowanceBlocked {
		t.Fatalf("rogue repair issuance = %v, want GUARD_ALLOWANCE_BLOCKED", err)
	}
	// raw finding 不产生 source/TaskKey：账本里无该任务时，basis 不因 finding
	// 存在而扩张（fixture 无 findings；此处验证 gate 对未建账任务一律拒绝）。
	if state.Validation.Findings != nil && len(state.Validation.Findings) > 0 {
		t.Fatal("fixture unexpectedly carries findings")
	}
}

// TestGuardIssuanceBlocksExhaustedRetry：有限 retry 耗尽的 TaskKey 重派＝零
// 签发；一次性追加（MaxAttempts+1）后同 TaskKey 恢复放行且 digest 变化
// （AC-059）。
func TestGuardIssuanceBlocksExhaustedRetry(t *testing.T) {
	engine, state, qaTask := guardPipelineFixture(t)
	state.EndedAttempts[qaTask] = Attempt{ID: "att:1", Attempts: 2, MaxAttempts: 2, RetryExhausted: true, IssuedAction: decision.IssuedAction{ActionID: "act:qa-1", Task: qaTask}}
	gate := engine.guardIssuanceGate(state)
	err := gate(decision.IssuedSet{{ActionID: "act:qa-2", Task: qaTask, Step: qaTask.Step}})
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Code != CodeGuardAllowanceBlocked {
		t.Fatalf("exhausted retry = %v, want GUARD_ALLOWANCE_BLOCKED", err)
	}
	before, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatal(err)
	}
	// 用户一次性追加一个 attempt：只更新该 TaskKey 的有限 retry policy。
	extended := state.EndedAttempts[qaTask]
	extended.MaxAttempts = 3
	extended.RetryExhausted = false
	state.EndedAttempts[qaTask] = extended
	if err := gate(decision.IssuedSet{{ActionID: "act:qa-2", Task: qaTask, Step: qaTask.Step}}); err != nil {
		t.Fatalf("extended retry blocked: %v", err)
	}
	after, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatal(err)
	}
	if after.BudgetBasisDigest == before.BudgetBasisDigest {
		t.Fatal("retry policy extension did not change the basis digest")
	}
	if after.RequestAllowance != before.RequestAllowance {
		t.Fatalf("retry extension changed the base slots: %d -> %d", before.RequestAllowance, after.RequestAllowance)
	}
}

// TestGuardUsedRetriesCountsExecutionsBeyondTheFirst：UsedRetries 与 guard
// 自身算术同口径（AC-055：retryAllowance=Σ max(0, MaxAttempts−1)）——每个
// TaskKey 的首个执行占 request 槽，其余执行才计 retry。两形：耗尽账本
// （2/2）与在途账本（首个执行已占用、retry 仍可用且闸放行）。
func TestGuardUsedRetriesCountsExecutionsBeyondTheFirst(t *testing.T) {
	engine, state, qaTask := guardPipelineFixture(t)
	state.EndedAttempts[qaTask] = Attempt{
		ID: "att:qa-1", Attempts: 2, MaxAttempts: 2, RetryExhausted: true,
		IssuedAction: decision.IssuedAction{ActionID: "act:qa-1", Task: qaTask},
	}
	exhausted, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatal(err)
	}
	if exhausted.UsedRetries != 1 || exhausted.RetryAllowance != 3 || exhausted.TotalAllowance != 6 {
		t.Fatalf("exhausted budget = %+v, want retries 1/3 total 2/6 (full authorized set)", exhausted)
	}
	if exhausted.UsedRequests != 1 || exhausted.RequestAllowance != 3 {
		t.Fatalf("exhausted request slots = %d/%d, want 1/3", exhausted.UsedRequests, exhausted.RequestAllowance)
	}

	engine, state, qaTask = guardPipelineFixture(t)
	state.Attempts[qaTask] = Attempt{
		ID: "att:qa-1", Attempts: 1, MaxAttempts: 2,
		IssuedAction: decision.IssuedAction{ActionID: "act:qa-1", Task: qaTask},
	}
	midFlight, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatal(err)
	}
	if midFlight.UsedRetries != 0 || midFlight.RetryAllowance != 3 {
		t.Fatalf("mid-flight budget = %+v, want retries 0/3", midFlight)
	}
	if err := engine.guardIssuanceGate(state)(decision.IssuedSet{{ActionID: "act:qa-2", Task: qaTask, Step: qaTask.Step}}); err != nil {
		t.Fatalf("mid-flight retry issuance blocked: %v", err)
	}
}

// TestGuardBlocksOutOfPlanBatchScope：开发批次签发的封闭性——候选 scope 必须
// 属于确认计划的批次任务集；计划外 batch scope 无授权来源（GUARD_ALLOWANCE_
// BLOCKED），确认批次任务照常放行。
func TestGuardBlocksOutOfPlanBatchScope(t *testing.T) {
	engine, state, _ := guardPipelineFixture(t)
	cd := engine.cfg.Definition
	rogue := pipelineTask(cd, "engine.development.batch", "plan:unconfirmed:batch-rogue")
	gate := engine.guardIssuanceGate(state)
	err := gate(decision.IssuedSet{{ActionID: "act:dev-rogue", Task: rogue, Step: rogue.Step}})
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Code != CodeGuardAllowanceBlocked {
		t.Fatalf("out-of-plan batch scope = %v, want GUARD_ALLOWANCE_BLOCKED", err)
	}
	_, plan, _ := state.confirmedDevelopmentPlan()
	if plan == nil {
		t.Fatal("fixture lacks a confirmed development plan")
	}
	planned := state.currentPlanTask(cd, plan, plan.Batches[0].ID)
	if err := gate(decision.IssuedSet{{ActionID: "act:dev-1", Task: planned, Step: planned.Step}}); err != nil {
		t.Fatalf("confirmed plan batch task blocked: %v", err)
	}
}

// TestGuardBlockedBugOnMisalignedLedger：已签发事实对不上账（无授权来源的
// 已签发派发）＝BLOCKED_BUG 派生失败。
func TestGuardBlockedBugOnMisalignedLedger(t *testing.T) {
	engine, state, _ := guardPipelineFixture(t)
	cd := engine.cfg.Definition
	rogue := pipelineTask(cd, "engine.repair", "sha256:rogue-candidate")
	state.DispatchBindings["act:rogue"] = WorkBinding{Task: rogue, WaveID: "wave-1"}
	_, err := engine.deriveGuardBudget(state)
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Code != CodeGuardBlockedBug {
		t.Fatalf("misaligned ledger = %v, want BLOCKED_BUG", err)
	}
}

// TestGuardBlockedBugOnOverissueWithoutLedgerFlag：attempt 账本未记耗尽而
// 重算已超发＝BLOCKED_BUG（将要超发）。
func TestGuardBlockedBugOnOverissueWithoutLedgerFlag(t *testing.T) {
	engine, state, qaTask := guardPipelineFixture(t)
	state.EndedAttempts[qaTask] = Attempt{ID: "att:1", Attempts: 2, MaxAttempts: 2, IssuedAction: decision.IssuedAction{ActionID: "act:qa-1", Task: qaTask}}
	gate := engine.guardIssuanceGate(state)
	err := gate(decision.IssuedSet{{ActionID: "act:qa-2", Task: qaTask, Step: qaTask.Step}})
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Code != CodeGuardBlockedBug {
		t.Fatalf("unflagged over-issue = %v, want BLOCKED_BUG", err)
	}
}

// TestGuardBudgetLoadBoundaryRejectsTamperedProjection：持久投影与事实重算
// 不一致（篡改）＝BLOCKED_BUG 拒绝读取；一致投影正常加载（恢复后投影与账本
// 一致）。
func TestGuardBudgetLoadBoundaryRejectsTamperedProjection(t *testing.T) {
	engine, state, _ := guardPipelineFixture(t)
	budget, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatal(err)
	}
	state.Guard = budget
	if err := engine.verifyGuardBudget(state); err != nil {
		t.Fatalf("consistent projection rejected: %v", err)
	}
	tampered := *budget
	tampered.UsedRetries = budget.UsedRetries + 1
	state.Guard = &tampered
	err = engine.verifyGuardBudget(state)
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Code != CodeGuardBlockedBug {
		t.Fatalf("tampered projection = %v, want BLOCKED_BUG", err)
	}
	state.Guard = budget
	state.Validation.Sets[coverage.ReviewBlackbox].Version = "case-v2"
	err = engine.verifyGuardBudget(state)
	if !errors.As(err, &rejected) || rejected.Code != CodeGuardBlockedBug {
		t.Fatalf("tampered basis fact = %v, want BLOCKED_BUG", err)
	}
}

// TestRecoveryNewAttemptPassesGuardAndContinuesAttemptLedger：恢复换新
// （NEW_ATTEMPT，绑定/责任变化的新计划槽位）仍经派发 guard——治理任务换新
// 必须在授权 basis 内，且其执行计数续在同一 TaskKey 的 Attempt 账本
// （IssueFromPlan/refill/ResumeAgent/RecoveryNewAttempt/HostAction retry 五入
// 口共用同一 guard/Attempt 计数）。
func TestRecoveryNewAttemptPassesGuardAndContinuesAttemptLedger(t *testing.T) {
	engine, state, qaTask := guardedFunnelFixture(t)
	state.PendingActions["act:qa-1"] = PendingAction{ActionID: "act:qa-1", Task: qaTask, Step: string(qaTask.Step), AttemptID: "att:qa-1"}
	state.Attempts[qaTask] = Attempt{
		ID: "att:qa-1", Attempts: 0, MaxAttempts: 2, Business: true,
		IssuedAction: decision.IssuedAction{ActionID: "act:qa-1", Task: qaTask, Step: qaTask.Step},
		Bindings:     AttemptBindings{Task: qaTask, Snapshot: "fp", Responsibility: state.RunProvider},
	}
	state.SpawnReceipts["act:qa-1"] = SpawnReceipt{ActionID: "act:qa-1", Provider: state.RunProvider, Correlation: "qa-agent", Status: SpawnStatusSpawned, Task: qaTask}
	state.LifecycleEvents = append(state.LifecycleEvents,
		LifecycleEventRecord{Provider: state.RunProvider, Correlation: "qa-agent", Identity: "qa-agent", Event: LifecycleStart},
		LifecycleEventRecord{Provider: state.RunProvider, Correlation: "qa-agent", Identity: "qa-agent", Event: LifecycleStop},
	)
	if err := engine.applyAttemptRecovery(state, state.PendingActions["act:qa-1"], state.Attempts[qaTask], RecoveryNewAttempt, "recover:1", 9); err != nil {
		t.Fatalf("guarded replacement blocked: %v", err)
	}
	// 业务派发的换新经 endBusinessAttempt 续在同一 TaskKey 的 EndedAttempts
	// 账本（attempts 0→1），重签发经同一签发闸放行并按 restoreRetryBinding
	// 续接该计数。
	ended, ok := state.EndedAttempts[qaTask]
	if !ok {
		t.Fatalf("fresh recovery did not account the business attempt: attempts=%+v ended=%+v", state.Attempts[qaTask], state.EndedAttempts)
	}
	if ended.Attempts != 1 || ended.MaxAttempts != 2 {
		t.Fatalf("ended attempt ledger = %+v, want attempts 1/2", ended)
	}
	if state.Tasks[qaTask] != runtime.TaskQueued {
		t.Fatalf("task status = %s, want QUEUED for re-issue", state.Tasks[qaTask])
	}
	if err := engine.guardIssuanceGate(state)(decision.IssuedSet{{ActionID: "act:qa-2", Task: qaTask, Step: qaTask.Step}}); err != nil {
		t.Fatalf("re-issue of the accounted task blocked: %v", err)
	}
	// 计划外且已入账（SpawnReceipt 在账）的治理任务＝账目无法对齐：换新以
	// BLOCKED_BUG 零派发终止（issued 事实经 provenance 归类失败）。
	rogue := pipelineTask(engine.cfg.Definition, "engine.development.batch", "plan:rogue:rogue")
	state.PendingActions["act:rogue"] = PendingAction{ActionID: "act:rogue", Task: rogue, Step: string(rogue.Step), AttemptID: "att:rogue"}
	state.Attempts[rogue] = Attempt{ID: "att:rogue", Attempts: 0, MaxAttempts: 2, Business: true,
		IssuedAction: decision.IssuedAction{ActionID: "act:rogue", Task: rogue, Step: rogue.Step},
		Bindings:     AttemptBindings{Task: rogue, Snapshot: "fp", Responsibility: state.RunProvider}}
	state.SpawnReceipts["act:rogue"] = SpawnReceipt{ActionID: "act:rogue", Provider: state.RunProvider, Correlation: "rogue-agent", Status: SpawnStatusSpawned, Task: rogue}
	state.LifecycleEvents = append(state.LifecycleEvents,
		LifecycleEventRecord{Provider: state.RunProvider, Correlation: "rogue-agent", Identity: "rogue-agent", Event: LifecycleStart},
		LifecycleEventRecord{Provider: state.RunProvider, Correlation: "rogue-agent", Identity: "rogue-agent", Event: LifecycleStop},
	)
	err := engine.applyAttemptRecovery(state, state.PendingActions["act:rogue"], state.Attempts[rogue], RecoveryNewAttempt, "recover:2", 10)
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Code != CodeGuardBlockedBug {
		t.Fatalf("misaligned replacement = %v, want BLOCKED_BUG", err)
	}
	if _, installed := state.Attempts[rogue]; !installed {
		t.Fatal("blocked replacement must leave zero state")
	}
	if state.Attempts[rogue].Attempts != 0 {
		t.Fatalf("blocked replacement changed the attempt ledger: %+v", state.Attempts[rogue])
	}
}

func gateTaskKeyOf(w *ValidationWave) string {
	for _, target := range w.Targets {
		if target.Kind == "GATE" {
			return target.Task.String()
		}
	}
	return ""
}

// TestGuardBasisKeepsEstablishedRepairAcrossWaveRegression：已签返修的 wave
// 回归（路线重绑置 INVALIDATED＋新到未裁决 finding）不撤销其 RepairSource 声
// 明——授权全集与 basis 声明集共用 establishedRepairWaves 单一归属点，账目
// 不因回归错位（假阳性 BLOCKED_BUG 硬停）。
func TestGuardBasisKeepsEstablishedRepairAcrossWaveRegression(t *testing.T) {
	engine, state, _ := guardPipelineFixture(t)
	cd := engine.cfg.Definition
	w := &state.Validation.Waves[0]
	gateTask := pipelineTask(cd, "engine.gate", "wave-1:quality")
	for _, target := range w.Targets {
		if target.Kind == "GATE" {
			target.Status = "FAIL"
			target.ResultAction = "act:gate-1"
		} else {
			target.Status = "PASS"
			target.ResultAction = "act:qa-1"
		}
	}
	state.Validation.Findings["finding-1"] = &ValidationFinding{
		ID: "finding-1", WaveID: w.ID, Target: gateTask.String(),
		History: []FindingResolution{{Decision: "NEW", ProblemID: "problem-1", Severity: "P2", AcceptedRevision: 2}},
	}
	w.Status = "REPAIR"
	w.Repair = &ValidationRepair{Task: pipelineTask(cd, "engine.repair", "native-1"), Problems: []string{"problem-1"}}
	// 返修已签发（签发代数在账）。
	state.DispatchBindings["act:repair-1"] = WorkBinding{Task: w.Repair.Task, WaveID: w.ID, Candidate: "native-1", Generation: 1}
	state.DispatchIssueRevisions["act:repair-1"] = 8
	if _, err := engine.deriveGuardBudget(state); err != nil {
		t.Fatalf("established repair basis failed before the regression: %v", err)
	}
	// 回归：wave 失效但返修仍在账，未裁决 finding 到达。
	w.Status = "INVALIDATED"
	state.Validation.Findings["finding-raw"] = &ValidationFinding{
		ID: "finding-raw", WaveID: w.ID, Target: gateTask.String(),
		Raw: &review.Finding{Message: "carried re-execution failed", Severity: "P2"},
	}
	budget, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatalf("regressed wave broke the repair basis: %v", err)
	}
	if budget == nil {
		t.Fatal("guard envelope disappeared after the regression")
	}
}

// TestGuardEnvelopeLeavesSeriesCountersSole：guard 是横切包络，不改写 series
// 轮次/Attempt 代次/wave 计数——同一签发事实下系列与 attempt 账本读数保持
// 其自身语义。
func TestGuardEnvelopeLeavesSeriesCountersSole(t *testing.T) {
	engine, state, qaTask := guardPipelineFixture(t)
	state.ReviewSeries = review.NewSet()
	if err := engine.refreshGuardBudget(state); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if state.ReviewSeries.ProductReview.Completed != 0 || len(state.Validation.Waves) != 1 {
		t.Fatalf("guard refresh mutated series/wave ledgers: %+v", state.ReviewSeries)
	}
	if state.Guard == nil || state.Guard.UsedRequests != 1 {
		t.Fatalf("projection = %+v", state.Guard)
	}
	if _, stillIssued := state.DispatchBindings["act:qa-1"]; !stillIssued {
		t.Fatal("guard refresh mutated the dispatch ledger")
	}
	if state.Attempts[qaTask].ID != "" || state.EndedAttempts[qaTask].ID != "" {
		t.Fatal("guard refresh mutated the attempt ledger")
	}
}

// TestGuardBasisDeclaresEstablishedSources：adjudication 完成、wave 收齐后
// 的有效义务与唯一返修 TaskKey 进 basis（预授权 repair 机械加入）；raw
// finding（未裁决）不产生 source。
func TestGuardBasisDeclaresEstablishedSources(t *testing.T) {
	engine, state, _ := guardPipelineFixture(t)
	cd := engine.cfg.Definition
	w := &state.Validation.Waves[0]
	for _, target := range w.Targets {
		if target.Kind == "GATE" {
			target.Status = "FAIL"
			target.ResultAction = "act:gate-1"
		} else {
			target.Status = "PASS"
			target.ResultAction = "act:qa-1"
		}
	}
	// 未裁决的 raw finding：wave 仍在收集中，不得进入 established sources。
	basis, err := engine.guardBasis(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(basis.EstablishedObligations) != 0 || len(basis.EstablishedRepairs) != 0 {
		t.Fatalf("collecting wave produced established sources: %+v", basis)
	}
	// 收齐且裁决完成后：finding 指向 problem、有效义务与唯一 repair TaskKey
	// 进入 basis（预授权 repair 机械加入，未签发也占槽）。
	w.Status = "REPAIR"
	w.Repair = &ValidationRepair{Task: pipelineTask(cd, "engine.repair", "native-1"), Problems: []string{"problem-1"}}
	state.Validation.Findings["finding-1"] = &ValidationFinding{
		ID: "finding-1", WaveID: w.ID, Target: gateTaskKeyOf(w),
		History: []FindingResolution{{Decision: "NEW", ProblemID: "problem-1", Severity: "P1", AcceptedRevision: 2}},
	}
	state.Validation.Problems["problem-1"] = &ValidationProblem{ID: "problem-1", FindingIDs: []string{"finding-1"}}
	basis, err = engine.guardBasis(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(basis.EstablishedObligations) == 0 || len(basis.EstablishedRepairs) != 1 {
		t.Fatalf("established sources = %+v %+v", basis.EstablishedObligations, basis.EstablishedRepairs)
	}
	if basis.EstablishedRepairs[0].ID != w.Repair.Task.String() {
		t.Fatalf("repair source = %+v", basis.EstablishedRepairs)
	}
	// repair 任务此刻可签发（预授权机械加入，不需额外确认）。
	if err := engine.guardIssuanceGate(state)(decision.IssuedSet{{ActionID: "act:repair-1", Task: w.Repair.Task, Step: w.Repair.Task.Step}}); err != nil {
		t.Fatalf("pre-authorized repair blocked: %v", err)
	}
	// 声明未签发的返修槽位属于授权全集：RequestAllowance 反映 4 个授权槽
	//（批次＋QA target＋门 target＋返修）而占用仍为 1（headroom 可见，
	// AC-059 预授权机械加入）。
	headroom, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatal(err)
	}
	if headroom.RequestAllowance != 4 || headroom.UsedRequests != 1 {
		t.Fatalf("declared-unissued repair headroom = %+v, want allowance 4 used 1", headroom)
	}
	// 预授权 repair 签发后其基础槽位进入额度（占用即计数）。
	state.DispatchBindings["act:repair-1"] = WorkBinding{Task: w.Repair.Task, WaveID: w.ID}
	after, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatal(err)
	}
	if after.RequestAllowance != 4 || after.UsedRequests != 2 {
		t.Fatalf("budget after repair issuance = %+v", after)
	}
}

// TestRepairAskSummaryCarriesAuthorizationFacts：REPAIR Ask 摘要按
// draft:372-375 逐项携带：完整 findings/problems（含历史裁决）、QA scope
// （wave 逐 target 状态与必跑用例）、已用/剩余轮数、dispatch signature/
// TaskKey、engine allowance（权威 Guard 投影）与 owner cost/token cap 可用性。
func TestRepairAskSummaryCarriesAuthorizationFacts(t *testing.T) {
	engine, state := guardRepairFixture(t)
	w := &state.Validation.Waves[0]
	budget, err := engine.deriveGuardBudget(state)
	if err != nil {
		t.Fatal(err)
	}
	state.Guard = budget

	summary := state.validationRequestBasis(ValidationRequest{Kind: "REPAIR", Target: w.ID, StateRevision: 9})
	var face struct {
		Detail struct {
			RequirementRevision string `json:"RequirementRevision"`
			Wave                string `json:"Wave"`
			WavesUsed           int    `json:"WavesUsed"`
			RemainingAutomatic  int    `json:"RemainingAutomatic"`
			TaskKey             string `json:"TaskKey"`
			DispatchActionID    string `json:"DispatchActionID"`
			Repair              struct {
				Findings []map[string]any `json:"Findings"`
				Problems []map[string]any `json:"Problems"`
			} `json:"Repair"`
			Targets []struct {
				Task          string   `json:"Task"`
				Status        string   `json:"Status"`
				RequiredCases []string `json:"RequiredCases"`
			} `json:"Targets"`
		} `json:"Detail"`
		EngineAllowance string `json:"EngineAllowance"`
		OwnerCost       string `json:"OwnerCost"`
		TokenCap        string `json:"TokenCap"`
		StateRevision   uint64 `json:"StateRevision"`
	}
	if err := json.Unmarshal(summary, &face); err != nil {
		t.Fatalf("summary: %v", err)
	}
	if face.Detail.Wave != w.ID || face.Detail.TaskKey != w.Repair.Task.String() {
		t.Fatalf("wave/task = %+v", face.Detail)
	}
	if face.Detail.WavesUsed != 0 || face.Detail.RemainingAutomatic != 3 {
		t.Fatalf("rounds = %+v", face.Detail)
	}
	if len(face.Detail.Repair.Findings) == 0 || len(face.Detail.Repair.Problems) == 0 {
		t.Fatalf("repair detail = %+v", face.Detail.Repair)
	}
	if len(face.Detail.Targets) != 2 {
		t.Fatalf("QA scope targets = %+v", face.Detail.Targets)
	}
	if !strings.Contains(face.EngineAllowance, budget.BudgetBasisDigest) || !strings.HasPrefix(face.EngineAllowance, "AVAILABLE") {
		t.Fatalf("engine allowance = %q", face.EngineAllowance)
	}
	if face.OwnerCost != "UNAVAILABLE" || face.TokenCap != "UNAVAILABLE" {
		t.Fatalf("owner/token = %q %q", face.OwnerCost, face.TokenCap)
	}
}

// guardRepairFixture supplies the collected targets and adjudicated problem
// owned by a current unsigned repair, without consuming a repair authorization.
func guardRepairFixture(t *testing.T) (*Engine, *State) {
	t.Helper()
	engine, state, _ := guardPipelineFixture(t)
	cd := engine.cfg.Definition
	w := &state.Validation.Waves[0]
	for _, target := range w.Targets {
		if target.Kind == "GATE" {
			target.Status = "FAIL"
			target.ResultAction = "act:gate-1"
		} else {
			target.Status = "PASS"
			target.ResultAction = "act:qa-1"
			target.RequiredCases = []string{"normal"}
		}
	}
	w.Status = "REPAIR"
	w.Repair = &ValidationRepair{Task: pipelineTask(cd, "engine.repair", "native-1"), Problems: []string{"problem-1"}}
	state.Validation.Findings["finding-1"] = &ValidationFinding{
		ID: "finding-1", WaveID: w.ID, Target: gateTaskKeyOf(w),
		History: []FindingResolution{{Decision: "NEW", ProblemID: "problem-1", Severity: "P1", Reason: "broken normal path", AcceptedRevision: 2}},
	}
	state.Validation.Problems["problem-1"] = &ValidationProblem{ID: "problem-1", FindingIDs: []string{"finding-1"}}
	return engine, state
}

// TestValidationAskDigestBindsCommittedState：Ask 摘要 digest 在提交点重绑为
// 已提交状态——同事务签发改变额度投影后，未决 Ask 的 digest 与重算一致
// （DECIDE 的摘要重核可用）。
func TestValidationAskDigestBindsCommittedState(t *testing.T) {
	engine, state := guardRepairFixture(t)
	state.PendingAsks = map[string]PendingAsk{}
	if !state.ensureValidationAsk(ValidationRequest{Kind: "REPAIR", Target: state.Validation.Waves[0].ID}, 7) {
		t.Fatal("ask not created")
	}
	var ask PendingAsk
	for id, a := range state.PendingAsks {
		if !a.Resolved {
			ask = a
			_ = id
		}
	}
	if ask.Validation == nil || ask.Validation.Digest == "" {
		t.Fatalf("ask = %+v", ask)
	}
	// 提交点：额度投影刷新＋摘要重绑。
	if err := engine.refreshGuardBudget(state); err != nil {
		t.Fatal(err)
	}
	rebindValidationAsks(state, 8)
	if state.PendingAsks[ask.RequestID].Resolved || ask.Validation.Binding.Task != state.Validation.Waves[0].Repair.Task {
		t.Fatal("current repair offer lost its executable obligation")
	}
	if ask.Validation.StateRevision != 8 {
		t.Fatalf("Ask revision = %d, want committed revision 8", ask.Validation.StateRevision)
	}
	rebound := state.validationRequestBasis(*ask.Validation)
	if got := digestOfCanonical(rebound); got != ask.Validation.Digest {
		t.Fatalf("digest mismatch after rebind: %s != %s", got, ask.Validation.Digest)
	}
}

// TestIssueFromPlanRefusalLeavesPersistedStateUnchanged：公开签发漏斗的
// guard 拒绝零状态变化——重开 store 后持久状态字节不变（闸在一切账本写入
// 之前执行）。构造：attempt 账本计数已达策略上限而未记耗尽（账本不一致）
// ——投影不置 Wait，治理任务进 Ready，闸以 BLOCKED_BUG 拒绝。
func TestIssueFromPlanRefusalLeavesPersistedStateUnchanged(t *testing.T) {
	engine, state, qaTask := guardedFunnelFixture(t)
	state.EndedAttempts[qaTask] = Attempt{
		ID: "att:qa-1", Attempts: 2, MaxAttempts: 2,
		IssuedAction: decision.IssuedAction{ActionID: "act:qa-1", Task: qaTask},
	}
	dir := t.TempDir()
	store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:test-package"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.Save(persistence.Transaction{
		ExpectedFingerprint: fingerprintOf(t, engine), CollectFingerprint: engine.ObserveFingerprint, Content: state,
	})
	if err != nil {
		t.Fatalf("persist fixture: %v", err)
	}
	statePath := filepath.Join(dir, "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := New(mustStoreAt(t, dir), Config{InternalSteps: fixtureInternalSteps(),
		Definition: engine.cfg.Definition, Registry: engine.cfg.Registry, Capacity: 4,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fp := fingerprintOf(t, reopened)
	plan, _, err := reopened.Plan()
	if err != nil {
		t.Fatal(err)
	}
	ready := false
	if plan.Next.Ready != nil {
		for _, task := range plan.Next.Ready.Tasks {
			if task.Task == qaTask {
				ready = true
			}
		}
	}
	if !ready {
		t.Fatalf("fixture did not reach a governed Ready candidate: %+v", plan.Next)
	}
	_, _, err = reopened.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp)
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Code != CodeGuardBlockedBug {
		t.Fatalf("issue = %v, want BLOCKED_BUG", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("guard refusal through the public funnel changed persisted state")
	}
	if snap, err := reopened.Load(); err != nil || snap.Revision != revision.Revision {
		t.Fatalf("reload after refusal = %v revision=%d want %d", err, snap.Revision, revision.Revision)
	}
}

// guardedFunnelFixture 是治理任务可直接 Ready 的最小定义（entry 本地步＋
// development.batch 与 qa.execution agent 步——管线启用需要 batch 步在
// 定义内），配同一套管线事实；qa.execution 任务不依赖审查系列载体步。
func guardedFunnelFixture(t *testing.T) (*Engine, *State, runtime.TaskKey) {
	t.Helper()
	reg := compiler.NewRegistry()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	must(reg.RegisterHandler("engine.funnel.parse", authoring.RunnerEngineLocal))
	must(reg.RegisterHandler("engine.development.batch", authoring.RunnerAgentWorker))
	must(reg.RegisterHandler("engine.qa.execution", authoring.RunnerAgentWorker))
	must(reg.RegisterCodec("codec.any.in"))
	must(reg.RegisterCodec("codec.any.out"))
	must(reg.RegisterPredicate("pred.funnel.post"))
	header := func(id string, deps ...string) authoring.Header {
		ds := make([]authoring.StepID, 0, len(deps))
		for _, d := range deps {
			ds = append(ds, authoring.StepID(d))
		}
		return authoring.Header{ID: authoring.StepID(id), NodeID: "funnel", Dependencies: ds, DefinitionVersion: "1"}
	}
	io := authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out",
		Postconditions: []authoring.PredicateRef{{ID: "pred.funnel.post"}}}
	parse, err := authoring.NewLocalStep(header("prep.parse"), authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out"}, authoring.LocalSpec{Handler: "engine.funnel.parse"})
	must(err)
	agent := func(id, handler string) authoring.Step {
		spec := authoring.AgentSpec{Handler: authoring.HandlerID(handler), Reason: authoring.ReasonSemanticJudgment, Timeout: time.Minute, Retry: &authoring.RetryPolicy{MaxAttempts: 2}}
		if handler == "engine.development.batch" {
			spec.Reason = authoring.ReasonCreativeImplementation
		}
		withDeps := io
		withDeps.Inputs = []authoring.InputBinding{{From: "prep.parse", OutputField: "out", ToField: "in"}}
		step, err := authoring.NewAgentStep(header(id, "prep.parse"), withDeps, spec)
		must(err)
		return step
	}
	cd, err := compiler.Compile(&compiler.Definition{
		Version: "1", EntryNode: "funnel",
		Steps: []authoring.Step{parse, agent("development.worker", "engine.development.batch"), agent("qa.execution", "engine.qa.execution")},
	}, reg)
	must(err)
	engine, err := New(mustStoreAt(t, t.TempDir()), Config{InternalSteps: fixtureInternalSteps(), Definition: cd, Registry: reg, Capacity: 4}, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState(cd.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatal(err)
	}
	if err := view.CompleteStep("prep.parse", cd); err != nil {
		t.Fatal(err)
	}
	state := NewState(*view)
	state.RunID = "guard-run"
	state.RunProvider = testProvider
	state.IntakeReceipt = &IntakeReceipt{
		Confirmation: IntakeConfirmationReceipt{RequirementSource: "requirements.md", RequirementRevision: "rev-1"},
		IntakeDigest: "sha256:intake", Revision: 1,
	}
	state.RequirementRevisions = []ConfirmedRevision{{
		Revision: "rev-1", Source: RequirementSourceIntake,
		Classification: RequirementClassificationIntake, StateRevision: 1,
	}}
	state.Topology = &TopologyBinding{Decision: "no-split", Note: "single lane"}
	state.RouteMode = "full"
	state.SelectedGates = []string{"blackbox"}
	plan := &GranularityReview{Reason: "one batch", VerificationTier: "FINAL", Batches: []DevelopmentBatch{{
		ID: "batch-1", Deliverable: "command", PublicEntry: "app run",
	}}}
	state.Decisions = map[string]RecordedDecision{
		"route-1": {
			RequestID: "route-1", Control: ControlSelectRoute, Choice: "confirm", Revision: 5,
			Review: &ReviewDecisionPayload{Route: &RouteDecision{
				Mode: "full", Gates: []string{"blackbox"},
				DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "act:readiness-plan", Digest: "sha256:plan"},
			}},
		},
	}
	state.Results = map[string]WorkerResult{
		"act:readiness-plan": {ActionID: "act:readiness-plan", Outcome: OutcomePass, Review: &ReviewResultEvidence{
			Action: review.StartReadiness, DispatchID: "act:readiness-plan",
			RequirementRevision: "rev-1", Status: "PASS",
			GranularityReview: plan, Verification: review.OperatorVerification{Evidence: "verified"},
		}},
	}
	qaTask := runtime.TaskKey{Node: "funnel", Step: "qa.execution", Scope: "wave-1:BLACKBOX"}
	state.Validation = &Validation{
		RequirementRevision: "rev-1", Generation: 1,
		Sets:     map[coverage.ReviewKind]*CaseSet{coverage.ReviewBlackbox: {Version: "case-v1", Status: "FROZEN"}},
		Findings: map[string]*ValidationFinding{}, Problems: map[string]*ValidationProblem{},
		Waves: []ValidationWave{{
			ID: "wave-1", Status: "COLLECTING", Scope: coverage.ScopeFull,
			Candidate:           coverage.ValidationCandidate{Identity: "native-1", Digest: "sha256:native-1"},
			RequirementRevision: "rev-1",
			Generation:          1,
			RouteDigest:         state.validationRouteDigest(),
			Versions:            map[coverage.ReviewKind]string{coverage.ReviewBlackbox: "case-v1"},
			ExpectedDigest:      "sha256:expected-1",
			Targets: map[string]*ValidationTarget{
				qaTask.String(): {Task: qaTask, Kind: "QA", Mode: coverage.ReviewBlackbox, Status: "PENDING"},
			},
		}},
	}
	state.DispatchBindings["act:qa-1"] = WorkBinding{Task: qaTask, WaveID: "wave-1", Candidate: "native-1", Generation: 1}
	return engine, state, qaTask
}

func mustStoreAt(t *testing.T, dir string) *persistence.Store {
	t.Helper()
	store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:test-package"})
	if err != nil {
		t.Fatal(err)
	}
	return store
}
