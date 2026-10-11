package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
)

// ackStore defers persistence until the selected actions, attempts and state
// can be committed in one protocol transaction.
type ackStore struct{}

func (ackStore) PersistIssued(decision.IssuedSet) error { return nil }

// DispatchClaim 在派发签出落账时建立宿主侧 lifecycle 绑定（runID/actionID/
// identity/provider）：identity 是回执声明的宿主 agent 实际派发身份，
// 宿主 lifecycle 事件按它配对。nil＝本引擎无宿主计量集成。
type DispatchClaim func(runID, actionID, identity, provider string) error

// Config 是 Engine 的定义绑定与容量输入：CompiledDefinition 供结果接纳的
// step 完成（CompleteStep）与补位签发（Decide/SelectIssued）使用；
// Registry 保留为编译期绑定的注册表输入；HostAction 的 operation
// 与 schema 以 Definition 中的 compiled payload 为唯一运行时来源。Capacity
// 是并行在途任务上限，驱动与补位从 PendingActions 扣除已占槽位。CostSource 是派发计量
// 来源的 sidecar 解析器（nil＝无宿主计量集成，cost 维度不参与判定）；
// InternalSteps binds transaction-local implementations by compiled HandlerID.
// ClaimDispatch 是派发签出时的 lifecycle 绑定建立回调（与 CostSource 同一注
// 入模式，protocol 不持 lifecycle 依赖）。
type Config struct {
	RequirementGuarantee *GuaranteeActivation
	VCSName              string
	NativeHandle         vcs.WorkspaceHandle
	NativeRoot           string
	Definition           *compiler.CompiledDefinition
	Registry             *compiler.Registry
	Capacity             int
	InternalSteps        map[authoring.HandlerID]InternalStep
	CostSource           CostSourceResolver
	ClaimDispatch        DispatchClaim
	// WorkerExecution reads the native owner's execution facts under its
	// existing admission window. A nil payload means no execution was claimed.
	WorkerExecution func(PendingAction, Attempt) (*SpawnReceiptPayload, error)
}

// Engine 是提交协议内核：每个方法一次完整的「读 state → 变更 →
// persistence 四段协议提交」事务（CAS + 指纹重验由 persistence 承担）。
// 引擎不缓存内存态，一切以磁盘权威投影为准。
type Engine struct {
	store   *persistence.Store
	cfg     Config
	collect func() (string, error)
}

// New 构造 Engine。collect 是外部事实指纹收集函数，nil 表示本引擎不依赖外部事实——空观察本身有确定指纹
// （decision 空 Observation 的 canonical digest），显式使用它而不是跳过
// 校验，保持每次写入都走完整指纹纪律。
func New(store *persistence.Store, cfg Config, collect func() (string, error)) (*Engine, error) {
	if store == nil {
		return nil, fmt.Errorf("protocol: nil store")
	}
	if cfg.Definition == nil {
		return nil, fmt.Errorf("protocol: config Definition is required")
	}
	if cfg.Registry == nil {
		return nil, fmt.Errorf("protocol: config Registry is required")
	}
	if cfg.Capacity < 0 {
		return nil, fmt.Errorf("protocol: config Capacity %d invalid", cfg.Capacity)
	}
	if collect == nil {
		collect = func() (string, error) {
			return emptyObservationDigest()
		}
	}
	bindings, err := bindInternalSteps(cfg.Definition, cfg.Registry, cfg.InternalSteps)
	if err != nil {
		return nil, err
	}
	cfg.InternalSteps = bindings
	return &Engine{store: store, cfg: cfg, collect: collect}, nil
}

// emptyObservationDigest 返回空观察的确定指纹（无外部事实也是一种
// 可复核的观察结果）。
func emptyObservationDigest() (string, error) {
	return decision.Observation{}.Digest()
}

// ObserveFingerprint 调用引擎的指纹收集，供调用方在提交前获取期望值
// （写事务将重验它）。
func (e *Engine) ObserveFingerprint() (string, error) {
	return e.collect()
}

// load 读取当前 (revision, state)。状态文件不存在归一化为 (0, nil, nil)
// ——上层以 NOT_INITIALIZED 拒绝；其余读取/校验错误原样上抛。
func (e *Engine) load() (uint64, *State, error) {
	snap, err := e.store.Load()
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	var state State
	if err := json.Unmarshal(snap.Content, &state); err != nil {
		return 0, nil, fmt.Errorf("protocol: decode engine state: %w", err)
	}
	// 加载边界与提交边界共用同一完整性校验：篡改的审查账本或越门的
	// phase 在读取侧即可区分拒绝。
	if err := verifyReviewIntegrity(&state, e.cfg.Definition); err != nil {
		return 0, nil, err
	}
	if err := state.verifySliceReceipt(); err != nil {
		return 0, nil, err
	}
	// 额度投影同一加载边界重算比对：持久投影与事实重算不一致（篡改投影或
	// 改写 basis 事实）＝BLOCKED_BUG，拒绝读取。
	if err := e.verifyGuardBudget(&state); err != nil {
		return 0, nil, err
	}
	return snap.Revision, &state, nil
}

// Init 落盘初始权威状态（revision 1）。view 经 decision.NewState 构造
// （版本与 phase 已校验）；provider 是本 run 绑定的宿主 provider 身份，
// 非空必填——后续一切携带 provider 的事件/回执与之精确比对，不同即
// PROVIDER_MISMATCH 硬拒绝、不降级 default。已存在状态时
// 拒绝且零变化。
func (e *Engine) Init(view *decision.State, provider string, expectedFingerprint string) error {
	return e.InitWithMetadata(view, provider, expectedFingerprint, "", "", nil, "", "", nil)
}

// InitWithMetadata is the façade bootstrap variant.  It preserves Init's
// original signature for protocol callers while persisting run identity and
// the typed pre-start intake confirmation in the same authoritative state
// write. sliceStart 非 nil 时是 child run 启动：继承事实与继承拓扑在首次
// 状态写入一并落账（与受理 phase 同点），不存在无继承事实的 child 中间态。
func (e *Engine) InitWithMetadata(view *decision.State, provider, expectedFingerprint, runID, route string, confirmation *IntakeConfirmationReceipt, baseSnapshot, initialSnapshot string, sliceStart *SliceRunStart) error {
	if view == nil {
		return fmt.Errorf("protocol: init: nil view")
	}
	if provider == "" {
		return fmt.Errorf("protocol: init: provider binding is required")
	}
	if _, state, err := e.load(); err != nil {
		return err
	} else if state != nil {
		return &RejectedError{Code: CodeAlreadyInitialized, Detail: "engine state already exists"}
	}
	state := NewState(*view)
	state.RunProvider = provider
	state.RunID = runID
	state.Route = route
	state.BaseSnapshot = baseSnapshot
	state.InitialSnapshot = initialSnapshot
	state.NativeRoot = e.cfg.NativeRoot
	state.VCSName = e.cfg.VCSName
	state.NativeHandle = e.cfg.NativeHandle
	state.IntakeConfirmationReceipt = confirmation
	state.RequirementGuarantee = e.cfg.RequirementGuarantee
	if sliceStart != nil {
		binding := sliceStart.Binding
		state.SliceChild = &binding
		topology := copyTopology(sliceStart.Topology)
		state.Topology = &topology
	}
	_, err := e.commit(state, 0, expectedFingerprint)
	return err
}

// RecordIntakeReceipt persists the first-drive receipt exactly once.  A
// matching existing receipt is idempotent; a different receipt is rejected so
// a run can never acquire a second or altered intake confirmation.
func (e *Engine) RecordIntakeReceipt(receipt IntakeReceipt, expectedFingerprint string) (uint64, error) {
	if strings.TrimSpace(expectedFingerprint) == "" {
		return 0, fmt.Errorf("protocol: intake receipt: expected fingerprint is required")
	}
	revision, state, err := e.load()
	if err != nil {
		return 0, err
	}
	if state == nil {
		return 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if state.IntakeReceipt != nil {
		if !sameIntakeReceipt(*state.IntakeReceipt, receipt) {
			return 0, &RejectedError{Code: CodeDuplicateEventMismatch, Detail: "intake receipt already recorded with different digest"}
		}
		return revision, nil
	}
	if state.IntakeConfirmationReceipt == nil || !sameIntakeConfirmation(*state.IntakeConfirmationReceipt, receipt.Confirmation) {
		return 0, &RejectedError{Code: CodeEventSchemaInvalid, Detail: "intake receipt does not match start confirmation"}
	}
	receipt.Revision = revision + 1
	state.IntakeReceipt = &receipt
	return e.commit(state, revision, expectedFingerprint)
}

func sameIntakeConfirmation(a, b IntakeConfirmationReceipt) bool {
	return a.Source == b.Source && a.Authority == b.Authority && a.Transport == b.Transport &&
		a.RequirementSource == b.RequirementSource &&
		a.RequirementRevision == b.RequirementRevision && a.SolutionRevision == b.SolutionRevision &&
		a.SolutionDigest == b.SolutionDigest && equalIntakeArtifacts(a.Artifacts, b.Artifacts)
}

func sameIntakeReceipt(a, b IntakeReceipt) bool {
	return a.IntakeDigest == b.IntakeDigest && sameIntakeConfirmation(a.Confirmation, b.Confirmation)
}

func equalIntakeArtifacts(a, b []IntakeArtifact) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// commit 把变更后的状态经 persistence 四段协议原子提交（期望 revision
// 即 CAS 值；指纹写入前后各重验一次）。提交前在同一事务内评估 run
// phase 门控（条件可从持久账本重算）并做账本完整性校验——条件不满足
// 时静默留置，账本/phase 与投影不一致则拒绝写入。
func (e *Engine) commit(state *State, expectedRevision uint64, expectedFingerprint string) (uint64, error) {
	revision, _, err := e.commitEffects(state, expectedRevision, expectedFingerprint, nil)
	return revision, err
}

// commitEffects 是一切携业务后果事务的唯一提交点：落账前先过事务收尾
// 单点（transactionEpilogue），并把收尾补位签发的 action ID 回传给调用方。
// 提交事件的完整 acceptance 随同一事务保存；非事件调用传 nil。
func (e *Engine) commitEffects(state *State, expectedRevision uint64, expectedFingerprint string, event *EventRecord) (uint64, []string, error) {
	refill, err := e.transactionEpilogue(state, expectedRevision+1, expectedFingerprint)
	if err != nil {
		return 0, nil, err
	}
	if err := e.refreshGuardBudget(state); err != nil {
		return 0, nil, err
	}
	rebindValidationAsks(state, expectedRevision+1)
	if err := state.advanceReviewPhases(); err != nil {
		return 0, nil, err
	}
	if err := verifyReviewIntegrity(state, e.cfg.Definition); err != nil {
		return 0, nil, err
	}
	if err := state.verifySliceReceipt(); err != nil {
		return 0, nil, err
	}
	if event != nil {
		event.Acceptance.Refill = append(event.Acceptance.Refill, refill...)
		state.Events[event.Acceptance.EventID] = *event
	}
	res, err := e.store.Save(persistence.Transaction{
		ExpectedRevision:    expectedRevision,
		ExpectedFingerprint: expectedFingerprint,
		CollectFingerprint:  e.collect,
		Content:             state,
	})
	if err != nil {
		return 0, nil, err
	}
	return res.Revision, refill, nil
}

// releaseUpstreamVoidIfReleasable 是上游失效解除的唯一触发点：上游重新
// PASS（effectivePass＝绑确认账本内 revision、无重走义务）且再无已确认
// 阻塞的任何事务都在此解除下游 UpstreamVoid——纠正路径与新鲜轮完成同
// 覆盖。下游自身的新鲜轮义务保持独立：上游恢复只解除依赖失效，不能
// 清偿下游重走。屏障在位时不做业务解除。无需重走的下游按其当前账本
// 重投影，frontier 步随同一事务经 settle 补齐
// （§7.3.3 可逆——载体步的完成闩单调，重投影只影响账本结论）。
func (e *Engine) releaseUpstreamVoidIfReleasable(state *State) error {
	downstream := state.ReviewSeries.StartReadiness
	if !downstream.UpstreamVoid || state.requirementChangePending() {
		return nil
	}
	upstream := state.ReviewSeries.ProductReview
	if !state.effectivePass(upstream) || review.HasConfirmedBlocking(upstream) {
		return nil
	}
	review.ReleaseUpstreamVoid(&state.ReviewSeries)
	return state.settleFrontierSteps(e.cfg.Definition, e.cfg.InternalSteps)
}

// transactionEpilogue 是事务收尾单点：一切携业务后果的提交在落账前经此
// 一次判定＋一次扫描——屏障在位时不收尾（挂起事实保持持有）；屏障不在
// 位时统一执行挂起的失效路由、挂起的 intent 清算与 frontier settle、挂
// 起配对扫描与容量补位。返回补位签发的 action ID。
func (e *Engine) transactionEpilogue(state *State, nextRevision uint64, expectedFingerprint string) ([]string, error) {
	if len(state.RequirementRevisions) > 1 {
		entry := state.RequirementRevisions[len(state.RequirementRevisions)-1]
		if entry.StateRevision == nextRevision && len(entry.Children) > 0 {
			return nil, nil
		}
	}
	if state.abortRequested() {
		_, err := e.advanceAbort(state, nextRevision)
		return nil, err
	}
	if state.resetPending() {
		_, err := e.advanceReset(state, nextRevision)
		return nil, err
	}
	if state.requirementChangePending() {
		state.routingReleasedCapacity = false
		return nil, nil
	}
	if _, err := e.advanceValidation(state, nextRevision); err != nil {
		return nil, err
	}
	if _, err := e.stopControlledWorkers(state, "superseded-work", "Worker responsibility superseded: ", nextRevision, false); err != nil {
		return nil, err
	}
	e.reconcileReviewGrantAsks(state)
	heldRouted := e.executeHeldFailureRoutes(state, nextRevision)
	heldSettled, err := e.executeHeldSettles(state, nextRevision)
	if err != nil {
		return nil, err
	}
	paired, pairedRefills := e.pairSuspendedResults(state, nextRevision, expectedFingerprint)
	if err := e.advanceRecoveries(state, nextRevision); err != nil {
		return nil, err
	}
	// 容量补位只在收尾真正做了改变派发面的工作时执行：观察类事务（回执
	// 落账等）不得自动签发——否则调用方持有的 Plan（Plan→Submit→
	// IssueFromPlan 驱动形态）会被收尾失效。业务工作＝挂起路由/清算/配
	// 对，或本事务释放容量、固定 host 执行范围使已有容量可用于待执行任务。
	// 结果完成与决定事务的补位在各自完成路径（completeResult／DECIDE
	// settle）照常执行。
	releasedCapacity := state.routingReleasedCapacity
	state.routingReleasedCapacity = false
	if !heldRouted && !heldSettled && paired == 0 && !releasedCapacity {
		return nil, nil
	}
	issued, err := e.refill(state, nextRevision, expectedFingerprint)
	if err != nil {
		// 补位失败不否决已合法的落账：留痕后由下一次提交重试。
		state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
			Detail: fmt.Sprintf("capacity refill deferred after an internal error: %v", err), Revision: nextRevision,
		})
		return pairedRefills, nil
	}
	return append(pairedRefills, issued...), nil
}

// executeHeldFailureRoutes 执行屏障期挂起的 FAILED 回执失败路由（按既定
// 规则：暂存删除、瞬态重试记账、terminalize、intent 清算、路由留痕）。
func (e *Engine) executeHeldFailureRoutes(state *State, nextRevision uint64) bool {
	if len(state.HeldFailureRoutes) == 0 {
		return false
	}
	held := append([]string(nil), state.HeldFailureRoutes...)
	slices.Sort(held)
	state.HeldFailureRoutes = nil
	for _, actionID := range held {
		if receipts, ok := state.SpawnFailures[actionID]; ok && len(receipts) > 0 {
			e.applyFailedSpawnRouting(state, actionID, receipts[len(receipts)-1], nextRevision)
			continue
		}
		if receipts, ok := state.HostActionFailures[actionID]; ok && len(receipts) > 0 {
			state.applyFailedHostActionRouting(actionID, receipts[len(receipts)-1], nextRevision)
		}
	}
	return true
}

// executeHeldSettles applies completed effects and settles their frontier at
// the transaction boundary, after any requirement barrier has been resolved.
func (e *Engine) executeHeldSettles(state *State, nextRevision uint64) (bool, error) {
	if len(state.HeldHostSettles) == 0 && !state.FrontierSettleHeld {
		return false, nil
	}
	for _, actionID := range state.HeldHostSettles {
		intent, ok := state.hostIntent(actionID)
		if !ok {
			return false, validationError("held effect has no original intent")
		}
		applicable := state.nativeEffectApplicable(intent, e.cfg.Definition)
		if err := e.applyNativeReceipt(state, intent, state.HostActionReceipts[actionID]); err != nil {
			return false, err
		}
		if intent.Adapter != nil && nativeOperation(intent.Adapter.Operation) {
			state.retireNative(intent, "FULFILLED", state.HostActionReceipts[actionID].Digest, nil, nextRevision)
		}
		if applicable && intent.Adapter != nil && nativeOperation(intent.Adapter.Operation) {
			for _, old := range state.PendingHostActions {
				if old.ActionID == intent.ActionID {
					continue
				}
				if receipt, ok := state.HostActionReceipts[old.ActionID]; ok && receipt.Status == HostActionStatusUnknown {
					continue
				}
				if state.nativeReplacementAuthorized(old, intent, e.cfg.Definition) {
					state.retireNative(old, "SUPERSEDED", state.HostActionReceipts[actionID].Digest, nil, nextRevision)
				}
			}
		}
		delete(state.PendingHostActions, actionID)
	}
	state.HeldHostSettles = nil
	state.FrontierSettleHeld = false
	if err := state.settleFrontierSteps(e.cfg.Definition, e.cfg.InternalSteps); err != nil {
		return false, err
	}
	state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
		Detail:   "settled host action intents were cleared and the frontier settled at the commit boundary",
		Revision: nextRevision,
	})
	return true, nil
}

// Snapshot 是一次 Load 的只读结果。
type Snapshot struct {
	Revision uint64
	State    *State
}

// MarshalJSON keeps the report-facing snapshot contract stable: Snapshot's
// exported fields are addressed as Revision/State, and the State fields are
// addressed with their exported names for consumers that inspect a report
// without knowing the persistence wire tags.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	var state map[string]json.RawMessage
	if s.State != nil {
		encoded, err := json.Marshal(s.State)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(encoded, &state); err != nil {
			return nil, err
		}
		upper := make(map[string]json.RawMessage, len(state))
		for key, value := range state {
			if key == "" {
				continue
			}
			upper[strings.ToUpper(key[:1])+key[1:]] = value
		}
		state = upper
	}
	return json.Marshal(struct {
		Revision uint64                     `json:"Revision"`
		State    map[string]json.RawMessage `json:"State"`
	}{Revision: s.Revision, State: state})
}

// Load 读取当前权威投影（未初始化时 NOT_INITIALIZED 拒绝）。
func (e *Engine) Load() (Snapshot, error) {
	revision, state, err := e.load()
	if err != nil {
		return Snapshot{}, err
	}
	if state == nil {
		return Snapshot{}, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	return Snapshot{Revision: revision, State: state}, nil
}

// decisionView derives all outstanding work before dependency selection and
// capacity admission. Review identities come from the series issuance ledger;
// live dispatches retain their actual task identity, including replacement tasks.
func (s *State) decisionView(compiled *compiler.CompiledDefinition) decision.State {
	view := s.State
	view.Suspended = pipelineEnabled(s, compiled) && s.dualReviewPass() && !s.validationRequirementCurrent() || !s.parentReviewReady() || s.requirementChangePending() || s.abortRequested() || s.Phase == runtime.PhaseFinalizingCleanup && s.pendingControl() || s.Phase == runtime.PhaseSliceReady || s.Phase == runtime.PhasePreparing || s.SliceChild == nil && s.Topology != nil && s.Topology.Decision == topologySplit && len(s.SliceChildren) == 0
	view.ControlRequests = nil
	for id, ask := range s.PendingAsks {
		if !ask.Resolved {
			view.ControlRequests = append(view.ControlRequests, id)
		}
	}
	slices.Sort(view.ControlRequests)
	view.RequiredTasks = map[authoring.StepID][]decision.RequiredTask{}
	for _, key := range s.requiredWorkTasks() {
		view.RequiredTasks[key.Step] = append(view.RequiredTasks[key.Step], decision.RequiredTask{Task: key})
	}
	carriers := map[review.Action][]compiler.CompiledStep{}
	seriesOf := map[authoring.StepID]review.Action{}
	for _, step := range compiled.Steps {
		payload, ok := step.Payload.(compiler.CompiledAgentStep)
		if !ok || !s.reviewSeriesBooked(review.Action(payload.ReviewSeries)) {
			continue
		}
		mark := review.Action(payload.ReviewSeries)
		seriesOf[step.Header.ID] = mark
		view.RequiredTasks[step.Header.ID] = nil
		carriers[mark] = append(carriers[mark], step)
	}
	coveredSteps := func(mark review.Action) []authoring.StepID {
		steps := make([]authoring.StepID, 0, len(carriers[mark]))
		for _, step := range carriers[mark] {
			steps = append(steps, step.Header.ID)
		}
		return steps
	}
	live := map[review.Action]bool{}
	s.eachSeriesLiveDispatch(func(step authoring.StepID) review.Action { return seriesOf[step] }, func(id string, mark review.Action) {
		if len(carriers[mark]) == 0 {
			return
		}
		key := s.SpawnReceipts[id].Task
		if pending, ok := s.PendingActions[id]; ok {
			key = pending.Task
		}
		view.RequiredTasks[key.Step] = append(view.RequiredTasks[key.Step], decision.RequiredTask{Task: key, Covers: coveredSteps(mark)})
		live[mark] = true
	})
	for mark, steps := range carriers {
		key := runtime.TaskKey{Node: steps[0].Header.NodeID, Step: steps[0].Header.ID}
		series, _ := s.ReviewSeries.Series(mark)
		if live[mark] || s.effectivePass(series) {
			continue
		}
		if round := series.Issued + 1; round > 1 {
			key.Scope = strconv.Itoa(round)
		}
		if boundary := s.resetRequestID(); boundary != "" {
			key.Scope = "reset:" + boundary + ":" + strconv.Itoa(series.Issued+1)
		}
		var blockedBy decision.WaitReason
		if !s.reviewRoundOwed(mark) {
			blockedBy = decision.WaitDependencies
		} else if series.RemainingCapacity == 0 {
			blockedBy = decision.WaitCapacity
		}
		view.RequiredTasks[key.Step] = append(view.RequiredTasks[key.Step], decision.RequiredTask{
			Task: key, BlockedBy: blockedBy, Covers: coveredSteps(mark),
		})
	}
	// All obligations must be present before testing dependencies. Multiple
	// carriers share one series obligation; use its first eligible carrier in
	// compiled order, or keep it waiting on the first carrier's dependencies.
	for _, mark := range []review.Action{review.ProductReview, review.StartReadiness} {
		steps := carriers[mark]
		if len(steps) == 0 || live[mark] || len(view.RequiredTasks[steps[0].Header.ID]) == 0 {
			continue
		}
		for _, step := range steps {
			eligible := true
			for _, dep := range step.Header.Dependencies {
				if !view.StepSatisfied(dep, compiled) {
					eligible = false
					break
				}
			}
			if !eligible {
				continue
			}
			task := view.RequiredTasks[steps[0].Header.ID][0]
			view.RequiredTasks[steps[0].Header.ID] = nil
			task.Task.Node, task.Task.Step = step.Header.NodeID, step.Header.ID
			view.RequiredTasks[step.Header.ID] = []decision.RequiredTask{task}
			break
		}
	}
	s.projectValidationTasks(&view, compiled)
	validationTasks := map[authoring.StepID][]decision.RequiredTask{}
	for _, step := range compiled.Steps {
		if validationHandler(step) != "" {
			validationTasks[step.Header.ID] = slices.Clone(view.RequiredTasks[step.Header.ID])
		}
	}
	s.projectRetryTasks(&view, compiled)
	s.projectValidationBoundary(&view, compiled)
	s.projectResourceBoundary(&view, compiled, validationTasks)
	return view
}

func (e *Engine) decide(state *State) (*decision.Plan, error) {
	view := state.decisionView(e.cfg.Definition)
	plan, err := decision.Decide(&view, decision.Observation{}, e.cfg.Definition)
	if err == nil {
		state.abortPlan(plan)
	}
	return plan, err
}

// Plan returns the deterministic next boundary for the current state without
// writing.  Façades use it for read-only `next`/`status` projections and for
// validating a plan before issuing work.
func (e *Engine) Plan() (*decision.Plan, uint64, error) {
	revision, state, err := e.load()
	if err != nil {
		return nil, 0, err
	}
	if state == nil {
		return nil, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	plan, err := e.decide(state)
	if err != nil {
		return nil, revision, err
	}
	return plan, revision, nil
}

// Drive advances deterministic engine-internal steps, then performs the
// standard Decide → SelectIssued cycle until an external boundary (or
// Complete) is reached.  No external event is accepted here; callers must use
// Submit for receipts, decisions and observations.
func (e *Engine) Drive(expectedFingerprint string) (*decision.Plan, uint64, error) {
	if strings.TrimSpace(expectedFingerprint) == "" {
		return nil, 0, fmt.Errorf("protocol: drive: expected fingerprint is required")
	}
	for {
		revision, state, err := e.load()
		if err != nil {
			return nil, 0, err
		}
		if state == nil {
			return nil, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
		}
		advanced, err := e.advanceValidation(state, revision+1)
		if err != nil {
			return nil, revision, err
		}
		// 跨事务事实收敛点：wave COMPLETE 与晋升绑定两个收口事实无论先
		// 后、无论落在哪个事务，下一次 Drive 都在此完成收口。settle 执行事务内 handler 并完
		// 成步、不签发工作——观察类事务不自动签发的约束不受影响。
		completed := len(state.Completed)
		if err := state.settleFrontierSteps(e.cfg.Definition, e.cfg.InternalSteps); err != nil {
			return nil, revision, err
		}
		if advanced || len(state.Completed) != completed || e.reconcileReviewGrantAsks(state) {
			if _, err := e.commit(state, revision, expectedFingerprint); err != nil {
				return nil, revision, err
			}
			continue
		}
		plan, err := e.decide(state)
		if err != nil {
			return nil, revision, err
		}
		switch plan.Next.Kind {
		case decision.KindComplete:
			// 终态相变按路线分流：lightweight 的收口事实全在引擎账本内，
			// Drive 直接相变（已 TERMINAL 的重入只读——不 bump revision、
			// 不改写状态）。regular 的终态绑定原生交付身份——外部事实经
			// SealTerminal 公开入口进入，Drive 只呈报 COMPLETE 边界。
			if state.Phase != runtime.PhaseTerminal {
				if state.Route != "lightweight" {
					return plan, revision, nil
				}
				if err := state.TransitionPhase(runtime.PhaseTerminal); err != nil {
					return nil, revision, err
				}
				if _, err := e.commit(state, revision, expectedFingerprint); err != nil {
					return nil, revision, err
				}
				continue
			}
			return plan, revision, nil
		case decision.KindWait:
			return plan, revision, nil
		case decision.KindReady:
			issued, _, err := e.IssueFromPlan(plan, decision.Admission{Capacity: max(0, e.cfg.Capacity-len(state.PendingActions))}, expectedFingerprint)
			if err != nil {
				var rejected *RejectedError
				if errors.As(err, &rejected) && rejected.Code == CodeGuardAllowanceBlocked && state.hasPendingBudgetAsk() {
					// 预算边界以未决 typed Ask 为外部边界（本次零签发），
					// Drive 返回当前计划而非错误；投影面经未决 Ask 呈现。
					return plan, revision, nil
				}
				return nil, revision, err
			}
			if len(issued) == 0 {
				return plan, revision, nil
			}
			continue
		default:
			return plan, revision, nil
		}
	}
}

// CompleteAll deterministically settles every compiled step and moves the run
// to TERMINAL.  It is used only by the lightweight façade route, whose
// acceptance contract has no external worker/host actions.
func (e *Engine) CompleteAll(expectedFingerprint string) (Snapshot, error) {
	if strings.TrimSpace(expectedFingerprint) == "" {
		return Snapshot{}, fmt.Errorf("protocol: complete all: expected fingerprint is required")
	}
	revision, state, err := e.load()
	if err != nil {
		return Snapshot{}, err
	}
	if state == nil {
		return Snapshot{}, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	// Terminal completion is a read-only replay. Repeated drive calls after
	// Complete must not advance revision or rewrite the state document.
	if state.Phase == runtime.PhaseTerminal {
		return e.Load()
	}
	// Compiled steps are ordered by deterministic ordinal, not necessarily by
	// the author's slice order. Repeatedly settle the currently eligible set so
	// every dependency is completed before its successor.
	for len(state.Completed) < len(e.cfg.Definition.Steps) {
		progressed := false
		for _, step := range e.cfg.Definition.Steps {
			done := false
			for _, completed := range state.Completed {
				if completed == step.Header.ID {
					done = true
					break
				}
			}
			if done {
				continue
			}
			if err := state.CompleteStep(step.Header.ID, e.cfg.Definition); err != nil {
				// An unmet dependency may become eligible later in this pass;
				// any other error is a malformed definition/state and must stop.
				if strings.Contains(err.Error(), "dependency") {
					continue
				}
				return Snapshot{}, err
			}
			progressed = true
		}
		if !progressed {
			return Snapshot{}, fmt.Errorf("protocol: complete all: definition dependencies cannot be settled")
		}
	}
	if state.Phase != runtime.PhaseTerminal {
		if err := state.TransitionPhase(runtime.PhaseTerminal); err != nil {
			return Snapshot{}, err
		}
	}
	if _, err := e.commit(state, revision, expectedFingerprint); err != nil {
		return Snapshot{}, err
	}
	return e.Load()
}

// IssueFromPlan 落账一次 Ready 签发：actionID 派生复用 decision.SelectIssued，
// 随后在同一事务里登记 expected 任务、当前 Attempt 与 pendingActions，
// 任务视图经 decision.State.TransitionTask 推进 QUEUED→ISSUED（复用
// runtime 任务状态机校验）。撞上已存在的 actionID 或任务已有当前
// Attempt 时 DUPLICATE_ACTION 拒绝且零变化。签出集为空且 Expected 无
// 新增条目时零状态变化、不经提交（原样返回当前 revision）；retainExpected
// 有增量属状态变化，照常提交。
func (e *Engine) IssueFromPlan(plan *decision.Plan, adm decision.Admission, expectedFingerprint string) (decision.IssuedSet, uint64, error) {
	revision, state, err := e.load()
	if err != nil {
		return nil, 0, err
	}
	if state == nil {
		return nil, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	// 修订屏障（Phase A）：未决的需求变化 Ask 期间冻结新的业务任务签发，
	// 以可区分错误拒绝直接签发调用；Drive 经此同步受阻。
	if err := state.errWriteBarrier("new task issuance"); err != nil {
		return nil, 0, err
	}
	identity, err := e.validatePlan(state, plan)
	if err != nil {
		return nil, 0, err
	}
	issued, err := decision.SelectIssued(plan, adm, ackStore{})
	if err != nil {
		return nil, 0, fmt.Errorf("protocol: issue: %w", err)
	}
	expectedCount := len(state.Expected)
	state.retainExpected(plan.Next.Ready)
	issued, err = state.issueInto(issued, revision+1, expectedFingerprint, state.RunProvider, identity, e.retryMaxAttempts, e.reviewSeriesOf, e.guardIssuanceGate(state))
	for _, action := range issued {
		e.bindBusinessAttempt(state, action.Task)
	}
	if err != nil {
		return nil, 0, err
	}
	// 零状态变化不经提交：容量裁剪后签出集为空且 Expected 未新增时，
	// 提交只会空转 revision；retainExpected 有增量属状态变化，照常提交。
	if len(issued) == 0 && len(state.Expected) == expectedCount {
		return issued, revision, nil
	}
	commitRevision, err := e.commit(state, revision, expectedFingerprint)
	if err != nil {
		return nil, 0, err
	}
	return issued, commitRevision, nil
}

// validatePlan rebuilds the canonical decision for the current authoritative
// state and compares the full plan identity before any task ledger mutation.
// The observation input is the same explicit empty Observation used by callers; the
// persistence fingerprint is bound separately to each Attempt as Snapshot.
func (e *Engine) validatePlan(state *State, plan *decision.Plan) (PlanIdentity, error) {
	if plan == nil {
		return PlanIdentity{}, fmt.Errorf("protocol: issue: nil plan")
	}
	want, err := e.decide(state)
	if err != nil {
		return PlanIdentity{}, fmt.Errorf("protocol: issue: decide current state: %w", err)
	}
	gotIdentity, err := identityOfPlan(plan)
	if err != nil {
		return PlanIdentity{}, fmt.Errorf("protocol: issue: candidate plan identity: %w", err)
	}
	wantIdentity, err := identityOfPlan(want)
	if err != nil {
		return PlanIdentity{}, fmt.Errorf("protocol: issue: current plan identity: %w", err)
	}
	if gotIdentity != wantIdentity {
		return PlanIdentity{}, &RejectedError{
			Code: CodePlanBindingMismatch,
			Detail: fmt.Sprintf("candidate plan identity %+v does not match current identity %+v",
				gotIdentity, wantIdentity),
		}
	}
	return gotIdentity, nil
}

func identityOfPlan(plan *decision.Plan) (PlanIdentity, error) {
	digest, err := plan.Digest()
	if err != nil {
		return PlanIdentity{}, err
	}
	return PlanIdentity{
		PlanDigest: digest, DefinitionDigest: plan.DefinitionDigest,
		StateDigest: plan.StateDigest, ObservationDigest: plan.ObservationDigest,
	}, nil
}

// reviewSeriesOf 返回编译定义中步骤声明的审查系列身份（未标记为零值）：
// 签发点据此把系列身份落进 PendingAction（签发时事实，不从定义即时推导）。
func (e *Engine) reviewSeriesOf(stepID authoring.StepID) review.Action {
	for _, step := range e.cfg.Definition.Steps {
		if step.Header.ID != stepID {
			continue
		}
		if payload, ok := step.Payload.(compiler.CompiledAgentStep); ok {
			return review.Action(payload.ReviewSeries)
		}
		return ""
	}
	return ""
}

// reviewSeriesBooked 报告声明身份是否落在账本系列上（封闭两值）：声明了
// 未入账标记（unaffiliated）的派发不进系列轮计数。
func (s *State) reviewSeriesBooked(mark review.Action) bool {
	_, ok := s.ReviewSeries.Series(mark)
	return ok
}

// retryMaxAttempts uses the compiled step's declared policy when present.
// Agent steps default to two attempts; other steps without a policy return zero.
func (e *Engine) retryMaxAttempts(stepID authoring.StepID) int {
	for _, step := range e.cfg.Definition.Steps {
		if step.Header.ID != stepID {
			continue
		}
		switch payload := step.Payload.(type) {
		case compiler.CompiledLocalStep:
			if payload.Retry != nil {
				return payload.Retry.MaxAttempts
			}
		case compiler.CompiledDurableStep:
			return payload.Retry.MaxAttempts
		case compiler.CompiledAgentStep:
			if payload.Retry != nil {
				return payload.Retry.MaxAttempts
			}
			return 2
		}
		return 0
	}
	return 0
}

// issuedWitnessReceipt 报告回执状态是否证明"派发真实签出去过"：SPAWNED
// 是宿主对派发成功的直接声明，UNKNOWN 是宿主对派发中断的观察（经 lifecycle
// 对账挂接为 ATTACHED 后是同一事实）。FAILED 只进失败史（从未签出去过）、
// 未定义状态不构成任何证明。承诺谓词（liveDispatch）与 terminal 对账的失
// 败结果准入以此为准。
func issuedWitnessReceipt(status string) bool {
	return status == SpawnStatusSpawned || status == SpawnStatusAttached || status == SpawnStatusUnknown
}

// inflightWitnessReceipt 报告回执状态是否构成"派发真实在途过"的观察事
// 实：SPAWNED 是宿主对派发成功的直接声明，ATTACHED 是 UNKNOWN 回执经
// lifecycle 对账挂接后的同一事实。它只承担结果准入的分层——terminal 对账
// 的真实结果（PASS 或携审查证据）与暂存结果的配对候选判定以此为准；承诺
// 的计入与解除（liveDispatch）读签出证明（issuedWitnessReceipt），不经由
// 本判定。FAILED 只进失败史、UNKNOWN 尚待对账，都不构成该事实。
func inflightWitnessReceipt(status string) bool {
	return status == SpawnStatusSpawned || status == SpawnStatusAttached
}

// liveDispatch 判定一条派发是否仍未落定——签发闸的在途计数与授权槽的
// 干净判定共用的承诺谓词。承诺始于签发、终于落定：回执状态只决定"哪类
// 结果可入账"（terminal 对账的准入分层），不决定承诺是否存在，因此任何
// 回执状态迁移（UNKNOWN 挂接为 ATTACHED）都不改变承诺集合——追认性承诺
// 增长结构性不可能。在途集合＝未落定派发：
//   - PendingActions 成员（任务未到终态）；或
//   - 回执证明派发真实签出去过（issuedWitnessReceipt），且结果未入账
//     （Results 无该派发）、派发未退位（ObsoleteActions 无该派发）。
//
// eachSeriesLiveDispatch 是系列在途派发枚举的唯一遍历（签发闸的在途计
// 数）：PendingActions 成员（系列身份＝签发标记），或回执在账且结果
// 未入账、派发未退位的 terminal 派发（簿记已收回，系列身份从回执固化的
// 任务步骤推导——签发标记与步骤标记同源，issueInto 以同一函数落标记）。
// visit 收到每条在途派发与其系列标记（含不在账本的标记，过滤归调用方）。
func (s *State) eachSeriesLiveDispatch(seriesOf func(authoring.StepID) review.Action, visit func(actionID string, mark review.Action)) {
	for actionID, pending := range s.PendingActions {
		if pending.ReviewSeries != "" {
			visit(actionID, pending.ReviewSeries)
		}
	}
	for actionID, receipt := range s.SpawnReceipts {
		if _, pending := s.PendingActions[actionID]; pending {
			continue
		}
		if !s.liveDispatch(actionID) {
			continue
		}
		if mark := seriesOf(receipt.Task.Step); mark != "" {
			visit(actionID, mark)
		}
	}
}

// 任务 terminal 化只收回簿记，不解除承诺：terminal 派发的迟到结果仍可经
// 对账路径合法入账，承诺必须覆盖"结果仍可合法入账的每一条派发"。承诺解
// 除仅随派发命运落定：结果入账（Results，含 terminal 对账与隔离重放的接
// 纳）、换新退位（ObsoleteActions）、spawn 从未成功（回执非签出证明
// ——FAILED 只进失败史）。无回执的非 pending 派发同样不计：其结果本就不
// 可入账，账实一致。被隔离重放拒绝的记录消化入迟到证据账（ObsoleteResults
// ＋留痕），但派发本身不因此落定——新的合法结果仍可对账入账，直到某个结
// 果真正落账或派发退位。
func (s *State) liveDispatch(dispatchID string) bool {
	if _, pending := s.PendingActions[dispatchID]; pending {
		return true
	}
	receipt, receipted := s.SpawnReceipts[dispatchID]
	if !receipted || !issuedWitnessReceipt(receipt.Status) {
		return false
	}
	if _, recorded := s.Results[dispatchID]; recorded {
		return false
	}
	_, retired := s.ObsoleteActions[dispatchID]
	return !retired
}

// issueInto persists the capacity-selected Ready set without changing its
// membership or task identities. Extra review authorization binds here to the
// selected action, and issuance advances the series' round ledger atomically.
// guardGate is the dispatch-budget judgment at this funnel: it runs before the
// duplicate/transition checks and every ledger write (it only reads the
// ledgers), so a refusal leaves zero state change.
func (s *State) issueInto(issued decision.IssuedSet, nextRevision uint64, snapshot, responsibility string, plan PlanIdentity, retryMax func(authoring.StepID) int, reviewSeriesOf func(authoring.StepID) review.Action, guardGate func(decision.IssuedSet) error) (decision.IssuedSet, error) {
	if guardGate != nil {
		if err := guardGate(issued); err != nil {
			return nil, err
		}
	}
	for i := range issued {
		if prior, ok := s.EndedAttempts[issued[i].Task]; ok {
			if prior.RetryExhausted {
				return nil, fmt.Errorf("retry exhausted for %s", issued[i].Task)
			}
			issued[i].ActionID = fmt.Sprintf("%s:attempt:%d", issued[i].ActionID, prior.Attempts+1)
		}
	}

	for _, action := range issued {
		if _, exists := s.PendingActions[action.ActionID]; exists {
			return nil, &RejectedError{Code: CodeDuplicateAction, Detail: fmt.Sprintf("action %q already issued", action.ActionID)}
		}
		if _, exists := s.Attempts[action.Task]; exists {
			return nil, &RejectedError{Code: CodeDuplicateAction, Detail: fmt.Sprintf("task %s already has a current attempt", action.Task.String())}
		}
		if err := s.TransitionTask(action.Task, runtime.TaskIssued); err != nil {
			return nil, &RejectedError{Code: CodeIllegalTransition, Detail: fmt.Sprintf("issue %s: %v", action.Task.String(), err)}
		}
	}
	for _, action := range issued {
		attempt := Attempt{
			IssuedAction: action,
			ID:           "att:" + action.Task.String() + ":" + strconv.FormatUint(nextRevision, 10),
			Bindings: AttemptBindings{
				Task: action.Task, Snapshot: snapshot, Responsibility: responsibility,
			},
			Plan: plan,
			// Attempts counts transient executions observed for this logical
			// task. It starts at zero; the first transient result advances it
			// to one, making a declared MaxAttempts value an exact exhaustion
			// count for the registered step.
			Attempts: 0,
		}
		if retryMax != nil {
			attempt.MaxAttempts = retryMax(action.Step)
		}
		s.Attempts[action.Task] = attempt
		s.DispatchIssueRevisions[action.ActionID] = nextRevision
		s.PendingActions[action.ActionID] = PendingAction{
			ActionID: action.ActionID, Task: action.Task, Step: string(action.Step), AttemptID: attempt.ID,
			ReviewSeries: reviewSeriesOf(action.Step),
		}
		if !s.expectedContains(action.Task) {
			s.Expected = append(s.Expected, action.Task)
		}
		if mark := reviewSeriesOf(action.Step); s.reviewSeriesBooked(mark) && s.EndedAttempts[action.Task].ID == "" {
			series, _ := s.ReviewSeries.Series(mark)
			if series.Completed >= series.AutomaticLimit {
				updated, err := review.BindDispatch(s.ReviewSeries, s.RunID, mark, action.ActionID, s.liveDispatch)
				if err != nil {
					return nil, err
				}
				s.ReviewSeries = updated
			}
			s.ReviewSeries = review.IssueRound(s.ReviewSeries, mark)
		}
		s.restoreRetryBinding(action, nextRevision)
	}
	return issued, nil
}

// reviewRoundOwed derives fresh-review work from the existing conclusion and
// redo obligation. A failed review awaits finding disposition; remaining
// capacity or an extra authorization does not itself request another review.
func (s *State) reviewRoundOwed(mark review.Action) bool {
	series, ok := s.ReviewSeries.Series(mark)
	if !ok || s.effectivePass(series) {
		return false
	}
	if mark == review.StartReadiness && (!s.effectivePass(s.ReviewSeries.ProductReview) || series.UpstreamVoid) {
		return false
	}
	if series.Effective.RequirementRevision == s.RequirementRevision() && review.HasConfirmedBlocking(series) {
		return false
	}
	return series.FreshOwed || series.Effective.Status == "PENDING"
}

// retainExpected keeps the complete Ready frontier in the durable task ledger,
// even when admission only signs the first capacity-sized prefix. Unissued
// tasks have no Attempt or pending action until a later refill.
func (s *State) retainExpected(ready *decision.ReadyPayload) {
	if ready == nil {
		return
	}
	for _, task := range ready.Tasks {
		if !s.expectedContains(task.Task) {
			s.Expected = append(s.Expected, task.Task)
		}
	}
}

// RecordedEvent reads the accepted event ledger without opening a write window.
// A missing event still needs Submit's current admission and commit checks.
func (e *Engine) RecordedEvent(ev Event) (Acceptance, bool, error) {
	if err := ev.Validate(); err != nil {
		return Acceptance{}, false, err
	}
	_, state, err := e.load()
	if err != nil {
		return Acceptance{}, false, err
	}
	if state == nil {
		return Acceptance{}, false, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	record, seen := state.Events[string(ev.ID)]
	if !seen {
		return Acceptance{}, false, nil
	}
	acceptance, err := record.Replay(ev)
	return acceptance, true, err
}

// Replay verifies one immutable accepted record, including terminal fallback.
func (record EventRecord) Replay(ev Event) (Acceptance, error) {
	if err := ev.Validate(); err != nil {
		return Acceptance{}, err
	}
	digest, err := ev.Digest()
	if err != nil {
		return Acceptance{}, err
	}
	if record.Acceptance.EventID != string(ev.ID) || record.Digest != digest {
		return Acceptance{}, &RejectedError{Code: CodeDuplicateEventMismatch, Detail: fmt.Sprintf("event %q replayed with different payload digest (recorded %s, submitted %s)", ev.ID, record.Digest, digest)}
	}
	return record.Acceptance, nil
}

// Submit 是唯一的外部事件接纳入口。顺序固定：
//
//	事件 schema 校验 → 幂等台账检查（同 ID 同 digest 重放返回稳定
//	acceptance、零状态变化；同 ID 异 digest 硬拒绝）→ 逐 kind 接纳
//	校验与变更 → persistence 四段协议提交
//
// 一切拒绝都发生在提交之前：零状态变化（被拒事件不进台账）。
func (e *Engine) Submit(ev Event, expectedFingerprint string) (Acceptance, error) {
	if err := ev.Validate(); err != nil {
		return Acceptance{}, err
	}
	digest, err := ev.Digest()
	if err != nil {
		return Acceptance{}, err
	}
	revision, state, err := e.load()
	if err != nil {
		return Acceptance{}, err
	}
	if state == nil {
		return Acceptance{}, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	// 幂等台账优先于一切接纳校验：重放不得因 token 已轮换或任务已推进
	// 而被误拒（同 ID 同 digest 返回稳定 acceptance/status）。
	if record, seen := state.Events[string(ev.ID)]; seen {
		return record.Replay(ev)
	}
	if state.Phase == runtime.PhaseTerminal {
		return Acceptance{}, &RejectedError{Code: CodeEventNotCurrent, Detail: "terminal run accepts only recorded event replay"}
	}
	if state.Phase == runtime.PhasePreparing {
		if ev.Kind == KindRequestControl && ev.Request.Control != ControlAbort || ev.Kind == KindOperatorObservation && ev.Observation.Validation != nil {
			return Acceptance{}, &RejectedError{Code: CodeEventNotCurrent, Detail: "preparing child has no business control authority"}
		}
	}
	nextRevision := revision + 1
	acceptance, mutated, wrote, reject := e.admit(ev, digest, state, revision, nextRevision, expectedFingerprint)
	if reject != nil {
		return Acceptance{}, reject
	}
	if !wrote {
		// A payload duplicate under a new event ID also binds that ID and digest.
		acceptance.Revision = nextRevision
		mutated = state
	}
	record := EventRecord{Digest: digest, Acceptance: acceptance}
	if _, _, err := e.commitEffects(mutated, revision, expectedFingerprint, &record); err != nil {
		return Acceptance{}, err
	}
	return record.Acceptance, nil
}

// admit 按事件 kind 做接纳校验并落账变更，返回接纳回执。nextRevision
// 是本事务将要提交到的 revision（CAS 保证其精确性），freshness token
// 与 Attempt ID 都以它派生。
func (e *Engine) admit(ev Event, digest string, state *State, revision, nextRevision uint64, expectedFingerprint string) (Acceptance, *State, bool, error) {
	// 修订屏障（Phase A）的处置表是唯一屏障检查点：观察类照常、
	// WORKER_RESULT 持久隔离、其余决定拒绝。表外不存在第二套屏障逻辑。
	if state.requirementChangePending() {
		switch err := state.admitInCurrentState(ev, false); {
		case err == nil:
			// 照常接纳，落入常规分支。
		case errors.Is(err, errPhaseAQuarantine):
			if perr := state.checkProvider(ev.Result.Provider); perr != nil {
				return Acceptance{}, nil, false, perr
			}
			return e.quarantineWorkerResult(state, ev, nextRevision)
		default:
			return Acceptance{}, nil, false, err
		}
	}
	// 事务收尾（挂起配对扫描、延迟补发、held 失效路由与 settle、容量补
	// 位）在唯一提交点 commitEffects——各分支不做。
	return e.admitEvent(ev, digest, state, revision, nextRevision, expectedFingerprint)
}

// admitEvent 按事件 kind 做接纳校验与落账（屏障处置表之后）。补配对属
// 事务收尾（见 admit），各分支不做。
func (e *Engine) admitEvent(ev Event, digest string, state *State, revision, nextRevision uint64, expectedFingerprint string) (Acceptance, *State, bool, error) {
	if state.abortRequested() && (ev.Kind == KindRequestControl || ev.Kind == KindDecide || ev.Kind == KindOperatorObservation && ev.Observation.Validation != nil) {
		cleanupRecovery := false
		if ev.Kind == KindDecide {
			ask := state.PendingAsks[string(ev.Decide.Request)]
			cleanupRecovery = ask.Control == ControlValidation && state.cleanupRetryRequest(ask.Validation) && (ev.Decide.Choice == "approve" || ev.Decide.Choice == "decline")
		}
		if !cleanupRecovery {
			return Acceptance{}, nil, false, validationError("run abort has stopped business controls")
		}
	}
	switch ev.Kind {
	case KindRequestControl:
		if ev.Request.Control == ControlRecovery {
			return Acceptance{}, nil, false, &RejectedError{Code: CodeEventSchemaInvalid, Detail: "control is not supported as a public request: " + string(ev.Request.Control)}
		}
		if ev.Request.Request != "" {
			return e.bindRequirementPackage(state, ev, revision, nextRevision)
		}
		// 两阶段主动控制第一段：受限 REQUEST_* 只创建
		// pending Ask——request ID（即事件 ID）、控制类型与选项集落账，
		// 不执行任何控制。同 requestID 的 Ask 已存在时拒绝（正常路径
		// 不可能：创建即入台账，重放已在台账层短路）。
		requestID := string(ev.ID)
		if _, exists := state.PendingAsks[requestID]; exists {
			return Acceptance{}, nil, false, &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("pending ask %q already exists", requestID)}
		}
		if state.SliceChild != nil && ev.Request.Control == ControlRequestRequirementChange {
			return Acceptance{}, nil, false, validationError("slice requirements are owned by master " + state.SliceChild.MasterRunID)
		}
		step := state.nextHumanAskStep(e.cfg.Definition)
		ask := PendingAsk{
			RequestID: requestID, Control: ev.Request.Control, Step: step, Options: ev.Request.Options, Package: ev.Request.Package,
		}
		if ev.Request.Control == ControlRequestRequirementChange {
			if ev.Request.Package != nil {
				if err := e.validateRequirementWork(state, ev.Request.Package.WorkDecisions, true); err != nil {
					return Acceptance{}, nil, false, err
				}
			}
			// Ask 落账创建时的权威基线：任何后续确认都使本 Ask 过期
			//（DECIDE 三方相等校验）。
			ask.BaseRevision = state.RequirementRevision()
			ask.BaseGeneration = state.requirementGeneration()
		}
		if ev.Request.Control == ControlReset {
			preview := state.ResetPreview()
			preview.Work = e.requirementWork(state)
			if ev.resetPreview != nil {
				preview.RequirementRevision = ev.resetPreview.RequirementRevision
				preview.Children = ev.resetPreview.Children
				preview.ChildPreviews = ev.resetPreview.ChildPreviews
				preview.Digest = preview.CanonicalDigest()
			}
			ask.Reset = &preview
			ask.Options = []AskOption{{ID: "proceed", Label: "Reset with this exact preservation and cleanup preview"}, {ID: "decline", Label: "Keep the current run"}}
		}
		if ev.Request.Control == ControlValidation {
			value, err := e.acceptValidationRequest(state, ev.Request.Validation, nextRevision)
			if err != nil {
				return Acceptance{}, nil, false, err
			}
			ask.Validation = value
			_, ask.Options = state.validationOffer(*value)
		}
		// SELECT_ROUTE 的候选门全集必填且形状合法（与决定侧候选表同一定义
		// 点），通过后钉进 Ask：用户确认边界以此为唯一权威，DECIDE 的候选
		// 表必须与之集合相等（admitReviewDecision 核对）。schema 层不校验
		// 该字段——事件 schema 判定先于幂等台账，已接纳事件的重放不得因
		// 此字段被拒。按 run 形态分流的约束在此接纳处判定：已绑定路线的
		// run 照常抬路线 Ask（§6.3.1 开发前重选/开发后追加，变更约束在决定
		// 侧由 validateRouteChange 把关）；split 绑定锁定的合并路线拒绝普通
		// route-add/reselect；child 的候选不含内建 QA 模式。
		if ev.Request.Control == ControlSelectRoute {
			if err := validateRouteGateCandidates(ev.Request.RouteGates); err != nil {
				return Acceptance{}, nil, false, err
			}
			if state.RouteMode == routeModeMerge {
				return Acceptance{}, nil, false, &RejectedError{Code: CodeReviewDecisionRejected, Detail: "route is locked to merge verification by the split topology binding; it is not selected (change goes through requirement change, reset, or abort)"}
			}
			if state.SliceChild != nil {
				for _, gate := range ev.Request.RouteGates {
					if gate == "blackbox" || gate == "whitebox" {
						return Acceptance{}, nil, false, &RejectedError{
							Code:   CodeReviewDecisionRejected,
							Detail: fmt.Sprintf("slice route candidates cannot carry the built-in QA mode %q; ordinary QA is not selected or executed per slice", gate),
						}
					}
				}
			}
			ask.RouteGates = slices.Clone(ev.Request.RouteGates)
		}
		state.PendingAsks[requestID] = ask
		token := freshnessToken(nextRevision, RequestID(requestID))
		return Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision, RequestID: requestID, FreshnessToken: token,
		}, state, true, nil

	case KindDecide:
		// 两阶段第二段：request 存在、未决、token 为当前值、选项在
		// 落账选项集内。token 绑定 (当前 revision, requestID)：任何后续
		// 提交都会使旧 token 失效——STALE_FRESHNESS 拒绝且零状态变化。
		requestID := string(ev.Decide.Request)
		ask, exists := state.PendingAsks[requestID]
		if !exists {
			if _, decided := state.Decisions[requestID]; decided {
				return Acceptance{}, nil, false, &RejectedError{Code: CodeRequestResolved, Detail: fmt.Sprintf("request %q already decided", requestID)}
			}
			return Acceptance{}, nil, false, &RejectedError{Code: CodeUnknownRequest, Detail: fmt.Sprintf("request %q does not exist", requestID)}
		}

		if ask.Resolved {
			return Acceptance{}, nil, false, &RejectedError{Code: CodeRequestResolved, Detail: fmt.Sprintf("request %q already decided", requestID)}
		}
		if want := freshnessToken(revision, RequestID(requestID)); ev.Decide.Token != want {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeStaleFreshness,
				Detail: fmt.Sprintf("request %q freshness token superseded by a later commit (current revision %d); re-fetch and resubmit", requestID, revision),
			}
		}
		if !ask.hasOption(ev.Decide.Choice) {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeInvalidChoice,
				Detail: fmt.Sprintf("choice %q not in options of request %q", ev.Decide.Choice, requestID),
			}
		}
		// 审查类业务控制：选项合法后先校验 typed 载荷并执行账目变换
		//（授权轮、纠正、拓扑、路线），再落决定。任一拒绝发生在 state
		// 变更之前——零状态变化。
		if ask.Control == ControlReset || ev.Decide.Reset != nil {
			if err := e.acceptResetDecision(state, ask, ev.Decide); err != nil {
				return Acceptance{}, nil, false, err
			}
		}
		if reviewControl(ask.Control) || ev.Decide.Review != nil {
			if err := e.admitReviewDecision(state, ask, requestID, ev.Decide); err != nil {
				return Acceptance{}, nil, false, err
			}
		}
		// 需求变化控制：同一两段形态，typed Requirement 载荷（或 decline）
		// 在落决定前执行确认登记与重走范围变换；非该控制携此载荷一律
		// schema 拒绝。
		if ask.Control == ControlRequestRequirementChange || ev.Decide.Requirement != nil {
			if err := e.admitRequirementDecision(state, ask, requestID, ev.Decide, nextRevision, expectedFingerprint, ev.requirementDocument); err != nil {
				return Acceptance{}, nil, false, err
			}
		}
		if ask.Control == ControlValidation || ev.Decide.Validation != nil {
			if err := e.acceptValidationDecision(state, ask, ev.Decide, revision, nextRevision); err != nil {
				return Acceptance{}, nil, false, err
			}
		}
		if ask.Control == ControlRecovery {
			record, known := state.recoveryRequest(requestID)
			if _, _, current := e.recoveryAttempt(state, record); !known || !current {
				return Acceptance{}, nil, false, &RejectedError{Code: CodeEventNotCurrent, Detail: "recovery request no longer owns the original attempt"}
			}
		}
		ask.Resolved = true
		state.PendingAsks[requestID] = ask
		state.Decisions[requestID] = RecordedDecision{
			RequestID: requestID, Control: ask.Control, Step: ask.Step, Choice: ev.Decide.Choice,
			EventID: string(ev.ID), Revision: nextRevision, Validation: ev.Decide.Validation, Review: ev.Decide.Review, Reset: ev.Decide.Reset,
		}
		// Keep the request record after settlement so its request ID, step binding,
		// options, and resolved state remain independently inspectable. Decisions
		// remains the immutable decision history and also rejects later submissions.
		// 决定落账即完成对应 HUMAN_ASK frontier 步骤（机械后果）。屏障在位
		// 时 frontier settle 挂起至解除事务统一执行，补位签发属事务收尾单
		// 点（commitEffects）；屏障不在位时先 settle 再补位。
		var refill []string
		if state.requirementChangePending() || ev.Decide.Requirement != nil && len(ev.Decide.Requirement.Children) > 0 {
			state.FrontierSettleHeld = true
		} else {
			if err := state.settleFrontierSteps(e.cfg.Definition, e.cfg.InternalSteps); err != nil {
				return Acceptance{}, nil, false, err
			}
			issued, err := e.refill(state, nextRevision, expectedFingerprint)
			if err != nil {
				return Acceptance{}, nil, false, err
			}
			refill = issued
		}
		return Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision, RequestID: requestID, Refill: refill,
		}, state, true, nil

	case KindTaskProgress:
		// 任务进度接纳：任务必须在 expected 集（非当前节点可区分拒绝）、
		// attempt 必须是当前 Attempt，转移合法（复用 runtime 任务状态机）。
		// TERMINAL 时收回 expected、pendingAction 与当前 Attempt。
		key := ev.Task.Task
		if !state.expectedContains(key) {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeEventNotCurrent,
				Detail: fmt.Sprintf("task %s is not in the expected set (event node is not current)", key.String()),
			}
		}
		attempt, exists := state.Attempts[key]
		if !exists || attempt.ID != ev.Task.Attempt {
			observed := "<none>"
			if exists {
				observed = attempt.ID
			}
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeStaleAttempt,
				Detail: fmt.Sprintf("task %s current attempt is %s, event carries %s", key.String(), observed, ev.Task.Attempt),
			}
		}
		if err := state.TransitionTask(key, ev.Task.Status); err != nil {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeIllegalTransition,
				Detail: fmt.Sprintf("task %s progress: %v", key.String(), err),
			}
		}
		if ev.Task.Status == runtime.TaskTerminal {
			if attempt.Business {
				attempt.AwaitingResult = true
				state.EndedAttempts[key] = attempt
			}
			state.removeTaskBookkeeping(key)
		}
		return Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision,
		}, state, true, nil

	case KindSpawnReceipt:
		// SpawnReceipt 接纳：provider 与 run 绑定精确比对；
		// 已有同 actionID 回执时按 payload digest 判定——逐字节重发幂等
		// （不重复回执效果，新 event ID 仍占台账），字节不同硬拒绝。
		// 首到回执在 pending action 存在时
		// 落账（声明签发态）；SPAWNED 同时配对暂存的 result-before-
		// receipt（同一事务内完成接纳与补位签发）。
		if err := state.checkProvider(ev.Spawn.Provider); err != nil {
			return Acceptance{}, nil, false, err
		}
		actionID := ev.Spawn.ActionID
		payloadDigest := digestOfCanonical(spawnReceiptEventWire{
			ActionID: ev.Spawn.ActionID, Provider: ev.Spawn.Provider,
			Correlation: ev.Spawn.Correlation, Status: ev.Spawn.Status,
			FailureClass: string(ev.Spawn.FailureClass),
		})
		for _, failed := range state.SpawnFailures[actionID] {
			if failed.Digest == payloadDigest {
				return Acceptance{
					EventID: string(ev.ID), Kind: string(ev.Kind), Status: "DUPLICATE",
					Revision: revision, ActionID: actionID,
				}, nil, false, nil
			}
		}
		if existing, seen := state.SpawnReceipts[actionID]; seen {
			if existing.Digest != payloadDigest {
				return Acceptance{}, nil, false, &RejectedError{
					Code:   CodeReceiptConflict,
					Detail: fmt.Sprintf("action %q spawn receipt replayed with different bytes (recorded %s, submitted %s)", actionID, existing.Digest, payloadDigest),
				}
			}
			return Acceptance{
				EventID: string(ev.ID), Kind: string(ev.Kind), Status: "DUPLICATE",
				Revision: revision, ActionID: actionID,
			}, nil, false, nil
		}
		pending, known := state.PendingActions[actionID]
		if !known {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeUnknownAction,
				Detail: fmt.Sprintf("spawn receipt references unknown or completed action %q", actionID),
			}
		}
		// 回执落账即固化派发的任务身份（观察事实）：任务簿记随后可被
		// 收回，terminal 对账以回执里的任务身份完成同一机械后果。
		receipt := SpawnReceipt{
			ActionID: actionID, Provider: ev.Spawn.Provider,
			Correlation: ev.Spawn.Correlation, Status: ev.Spawn.Status,
			FailureClass: ev.Spawn.FailureClass, Task: pending.Task, Digest: payloadDigest,
		}
		acceptance := Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision, ActionID: actionID,
		}
		if ev.Spawn.Status == SpawnStatusFailed {
			state.SpawnFailures[actionID] = append(state.SpawnFailures[actionID], receipt)
			// 回执是观察事实照常落账；失败路由（暂存删除、重试记账、
			// terminalize）是业务后果——屏障期挂起至解除事务统一执行。
			if state.requirementChangePending() {
				if !slices.Contains(state.HeldFailureRoutes, actionID) {
					state.HeldFailureRoutes = append(state.HeldFailureRoutes, actionID)
				}
				state.appendRecovery(RecoveryRecord{
					ActionID: actionID,
					Detail:   "failed spawn receipt recorded; its failure routing is held while the requirement change barrier holds",
					Revision: nextRevision,
				})
				return acceptance, state, true, nil
			}
			route := e.applyFailedSpawnRouting(state, actionID, receipt, nextRevision)
			acceptance.FailureClass = string(route.Class)
			acceptance.RecoveryAction = string(route.Action)
			return acceptance, state, true, nil
		}
		state.SpawnReceipts[actionID] = receipt
		// 派发签出即建立 lifecycle dispatch 绑定（注入 claim 回调）：SPAWNED 回
		// 执的 correlation 即宿主报告的 agent 身份（host 事件按它配对——绑定
		// identity 不可变，须以真实身份绑定）。UNKNOWN 回执的 correlation 只是
		// 追踪键，claim 延后至 ReconcileUnknownReceipt 唯一匹配 attach 时以解析
		// 出的真实身份建立；零/多匹配不 claim（计量来源诚实 UNAVAILABLE）。
		// sidecar 写失败只降级该派发的计量来源，不否决回执落账。
		if e.cfg.ClaimDispatch != nil && ev.Spawn.Status == SpawnStatusSpawned {
			if claimErr := e.cfg.ClaimDispatch(state.RunID, actionID, ev.Spawn.Correlation, state.RunProvider); claimErr != nil {
				state.appendRecovery(RecoveryRecord{
					ActionID: actionID,
					Detail:   fmt.Sprintf("lifecycle dispatch claim failed; the dispatch cost source degrades to unavailable: %v", claimErr),
					Revision: nextRevision,
				})
			}
		}
		state.verifyLifecycleCorrelation(ev.Spawn.Correlation, nextRevision)
		if ev.Spawn.Status == SpawnStatusUnknown {
			plan := DecideRecovery(Interruption{ReceiptUnknown: true, LifecycleMatches: len(state.lifecycleMatches(ev.Spawn.Correlation))})
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: actionID, Class: plan.Class, Action: plan.Action,
				Detail: plan.Detail, LifecycleMatches: plan.LifecycleMatches, Revision: nextRevision,
			})
			acceptance.RecoveryAction = string(plan.Action)
		}
		if _, waiting := state.StagedResults[actionID]; waiting && ev.Spawn.Status == SpawnStatusSpawned {
			// 回执是观察事实照常落账；暂存结果的配对接纳属事务收尾单点
			//（commitEffects）——屏障在位时收尾不配对，解除事务扫清；顺
			// 序门未开时由收尾扫描挂起并留痕。
			if state.requirementChangePending() {
				state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
					ActionID: actionID,
					Detail:   "spawn receipt recorded while the requirement change barrier holds; the staged result stays suspended until the barrier lifts",
					Revision: nextRevision,
				})
			}
		}
		return acceptance, state, true, nil

	case KindWorkerResult:
		// typed result 接纳：provider 精确比对后进入唯一接纳核心（与隔离
		// 重放共用）——结果级摘要幂等/冲突、obsolete 认领、重走态的
		// floor 比对与按声明终止重签发、result-before-receipt 暂存与回
		// 执配对后的完成（frontier 推进＋容量补位）。
		if err := state.checkProvider(ev.Result.Provider); err != nil {
			return Acceptance{}, nil, false, err
		}
		acceptance, wrote, err := e.admitWorkerResultRecord(state, ev.Result.ActionID, ev.Result, string(ev.ID), "", revision, nextRevision, expectedFingerprint)
		if err != nil {
			return Acceptance{}, nil, false, err
		}
		if !wrote {
			return acceptance, nil, false, nil
		}
		// 结果接纳可能把产品审结论重新 PASS——挂起配对的补齐属事务收尾
		// 单点（admit），两种完成顺序（结果先到/回执先到）走同一处。
		return acceptance, state, true, nil

	case KindOperatorObservation:
		if ev.Observation.Validation != nil {
			if err := e.acceptValidationObservation(state, ev.Observation, revision, nextRevision); err != nil {
				return Acceptance{}, nil, false, err
			}
			return Acceptance{EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED", Revision: nextRevision}, state, true, nil
		}
		// Operator typed observation 入账（主代理核实事实、
		// 不替用户授权）：绑定来源对账项并保留提交时 revision。Subject 必
		// 须指向引擎当前真实等待处置的对账对象——UNKNOWN spawn receipt、
		// UNKNOWN HostAction receipt 或未清账 intent——否则 UNKNOWN_ACTION
		// 拒绝且零写入；伪造 subject 不能凭空入账。
		subject := ev.Observation.Subject
		bound := false
		if receipt, ok := state.SpawnReceipts[subject]; ok && receipt.Status == SpawnStatusUnknown {
			bound = true
		}
		if !bound {
			if hostReceipt, ok := state.HostActionReceipts[subject]; ok && hostReceipt.Status == HostActionStatusUnknown {
				bound = true
			}
		}
		if !bound {
			if _, pending := state.PendingHostActions[subject]; pending {
				bound = true
			}
		}
		if !bound {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeUnknownAction,
				Detail: fmt.Sprintf("operator observation subject %q is not a pending reconciliation subject (unknown spawn/HostAction receipt or unsettled intent)", subject),
			}
		}
		state.OperatorObservations = append(state.OperatorObservations, OperatorObservation{
			Subject: ev.Observation.Subject, Facts: ev.Observation.Facts,
			EventID: string(ev.ID), Revision: nextRevision,
		})
		return Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision, ActionID: subject,
		}, state, true, nil

	case KindHostActionReceipt:
		// HostAction 回执接纳：provider 精确比对；已清账的
		// 回执按 payload digest 幂等/冲突；回执必须与 pending intent 的
		// operation 与参数 digest 精确一致；接纳即清账 intent。
		if err := state.checkProvider(ev.HostAction.Provider); err != nil {
			return Acceptance{}, nil, false, err
		}
		actionID := ev.HostAction.ActionID
		payloadDigest := digestOfCanonical(hostActionReceiptWire{
			ActionID: ev.HostAction.ActionID, Operation: string(ev.HostAction.Operation),
			AdapterOperation: string(ev.HostAction.AdapterOperation),
			Provider:         ev.HostAction.Provider, Correlation: ev.HostAction.Correlation,
			PayloadDigest: ev.HostAction.PayloadDigest, Status: ev.HostAction.Status,
			FailureClass: string(ev.HostAction.FailureClass), LifecycleEvidence: ev.HostAction.LifecycleEvidence,
			AdapterEvidence: ev.HostAction.AdapterEvidence,
		})
		for _, failed := range state.HostActionFailures[actionID] {
			if failed.Digest == payloadDigest {
				return Acceptance{
					EventID: string(ev.ID), Kind: string(ev.Kind), Status: "DUPLICATE",
					Revision: revision, ActionID: actionID,
				}, nil, false, nil
			}
		}

		if original, known := state.hostIntent(actionID); known && original.Adapter != nil && nativeOperation(original.Adapter.Operation) && len(state.HostActionFailures[actionID]) != 0 && (ev.HostAction.Status == HostActionStatusFailed || ev.HostAction.Status == HostActionStatusUnknown) {
			return Acceptance{}, nil, false, &RejectedError{Code: CodeReceiptConflict, Detail: fmt.Sprintf("native call %q already has its failure fact", actionID)}
		}
		if reconciled, ok := state.ReconciledEffects[actionID]; ok && reconciled.UnknownReceipt != nil && reconciled.UnknownReceipt.Digest == payloadDigest {
			return Acceptance{EventID: string(ev.ID), Kind: string(ev.Kind), Status: "DUPLICATE", Revision: revision, ActionID: actionID}, nil, false, nil
		}
		if existing, seen := state.HostActionReceipts[actionID]; seen {
			if existing.Digest == payloadDigest {
				return Acceptance{EventID: string(ev.ID), Kind: string(ev.Kind), Status: "DUPLICATE", Revision: revision, ActionID: actionID}, nil, false, nil
			}
			if existing.Status != HostActionStatusUnknown || ev.HostAction.Status != HostActionStatusExecuted {
				return Acceptance{}, nil, false, &RejectedError{Code: CodeReceiptConflict, Detail: fmt.Sprintf("action %q host action receipt conflicts with recorded completion", actionID)}
			}
		}
		intent, exists := state.hostIntent(actionID)
		_, pending := state.PendingHostActions[actionID]
		if !exists {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeUnknownAction,
				Detail: fmt.Sprintf("host action receipt references unknown or settled intent %q", actionID),
			}
		}
		if intent.Operation != ev.HostAction.Operation || intent.PayloadDigest != ev.HostAction.PayloadDigest {
			return Acceptance{}, nil, false, &RejectedError{
				Code: CodeIntentMismatch,
				Detail: fmt.Sprintf("receipt for %q does not match pending intent (operation %q vs %q, payload %s vs %s)",
					actionID, ev.HostAction.Operation, intent.Operation, ev.HostAction.PayloadDigest, intent.PayloadDigest),
			}
		}
		if intent.Correlation != "" && intent.Correlation != ev.HostAction.Correlation {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeIntentMismatch,
				Detail: fmt.Sprintf("receipt for %q does not match pending intent correlation %q vs %q", actionID, ev.HostAction.Correlation, intent.Correlation),
			}
		}
		if err := e.validateHostActionEvidence(state, intent, ev.HostAction); err != nil {
			return Acceptance{}, nil, false, err
		}
		receipt := HostActionReceipt{
			ActionID: actionID, Operation: ev.HostAction.Operation, Step: intent.Step, AdapterOperation: ev.HostAction.AdapterOperation,
			Provider: ev.HostAction.Provider, Correlation: ev.HostAction.Correlation,
			PayloadDigest: ev.HostAction.PayloadDigest, Status: ev.HostAction.Status,
			FailureClass: ev.HostAction.FailureClass, LifecycleEvidence: ev.HostAction.LifecycleEvidence,
			AdapterEvidence: ev.HostAction.AdapterEvidence, Digest: payloadDigest,
		}
		if ev.HostAction.Status == HostActionStatusFailed {
			state.HostActionFailures[actionID] = append(state.HostActionFailures[actionID], receipt)
			if !pending {
				return Acceptance{EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED", Revision: nextRevision, ActionID: actionID}, state, true, nil
			}
			// 回执是观察事实照常落账；失败路由与 intent 清算是业务后果
			//——屏障期挂起至解除事务统一执行。
			if state.requirementChangePending() {
				if !slices.Contains(state.HeldFailureRoutes, actionID) {
					state.HeldFailureRoutes = append(state.HeldFailureRoutes, actionID)
				}
				state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
					ActionID: actionID,
					Detail:   "failed host action receipt recorded; its failure routing is held while the requirement change barrier holds",
					Revision: nextRevision,
				})
				return Acceptance{
					EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED", Revision: nextRevision,
					ActionID: actionID,
				}, state, true, nil
			}
			route := state.applyFailedHostActionRouting(actionID, receipt, nextRevision)
			return Acceptance{
				EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED", Revision: nextRevision,
				ActionID: actionID, FailureClass: string(route.Class), RecoveryAction: string(route.Action),
			}, state, true, nil
		}
		if ev.HostAction.Status == HostActionStatusExecuted && intent.Adapter != nil {
			if err := validateResourceReceipt(intent, receipt); err != nil {
				return Acceptance{}, nil, false, err
			}
		}
		if ev.HostAction.Status == HostActionStatusExecuted && intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" {
			if _, _, err := validateCandidateReceipt(state.NativeVCS(), intent, receipt); err != nil {
				return Acceptance{}, nil, false, err
			}
		}
		if previous, ok := state.HostActionReceipts[actionID]; ok && previous.Status == HostActionStatusUnknown && receipt.Status == HostActionStatusExecuted {
			effect := state.ReconciledEffects[actionID]
			if effect.Status != "SUPERSEDED" && effect.Status != "WITHDRAWN" {
				effect = ReconciledEffect{ActionID: actionID, Operation: intent.Operation, AdapterOperation: receipt.AdapterOperation, ObservationDigest: receipt.Digest, Status: "FULFILLED", Revision: nextRevision, UnknownReceipt: &previous}
			}
			if effect.UnknownReceipt == nil {
				effect.UnknownReceipt = &previous
			}
			state.ReconciledEffects[actionID] = effect
		}
		state.HostActionReceipts[actionID] = receipt
		// 恢复执行的 EXECUTED 回执是执行证据：在目标任务的 Attempt 账本同点
		// 位计入一次执行（公开 ResumeAgent 与恢复决定共用本计数点）。
		if receipt.Status == HostActionStatusExecuted && intent.Operation == HostActionResumeAgent && intent.Resume != nil {
			state.accountResumeExecution(intent.Resume.WorkerActionID)
		}
		if ev.HostAction.Status == HostActionStatusUnknown {
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: actionID, Class: authoring.FailureSideEffectUnknown,
				Action: RecoveryReconcile, Detail: "host action receipt is UNKNOWN; reconcile external facts before retrying",
				Revision: nextRevision,
			})
			return Acceptance{EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED", Revision: nextRevision, ActionID: actionID, RecoveryAction: string(RecoveryReconcile)}, state, true, nil
		}
		// Completed effects settle at the transaction boundary, including while
		// held behind a requirement barrier.
		if !slices.Contains(state.HeldHostSettles, actionID) {
			state.HeldHostSettles = append(state.HeldHostSettles, actionID)
		}
		if state.requirementChangePending() {
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: actionID,
				Detail:   "executed host action receipt recorded; its intent settlement and frontier settle are held while the requirement change barrier holds",
				Revision: nextRevision,
			})
		}

		return Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision, ActionID: actionID,
		}, state, true, nil

	case KindLifecycleEvent:
		// lifecycle 事件只写 observation buffer（不改 workflow 投影）：
		// provider 精确比对；逐字节重发（payload digest 命中）不重复
		// buffer 效果，新 event ID 仍占台账；
		// 本事件补齐 start/stop 配对且 identity 被 SpawnReceipt correlation
		// 认领时落账已验证配对；stop 没有先行 start 则不是合法 observation。
		if err := state.checkProvider(ev.Lifecycle.Provider); err != nil {
			return Acceptance{}, nil, false, err
		}
		if ev.Lifecycle.Event == LifecycleStop && !state.hasCorrelatedLifecycleEvent(ev.Lifecycle.Correlation, ev.Lifecycle.Identity, LifecycleStart) {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeEventSchemaInvalid,
				Detail: fmt.Sprintf("lifecycle stop for %q has no preceding start", ev.Lifecycle.Identity),
			}
		}
		payloadDigest := digestOfCanonical(lifecycleEventWire{
			Provider: ev.Lifecycle.Provider, Correlation: ev.Lifecycle.Correlation,
			Identity: ev.Lifecycle.Identity, Event: ev.Lifecycle.Event,
		})
		for _, record := range state.LifecycleEvents {
			if record.Digest == payloadDigest {
				return Acceptance{
					EventID: string(ev.ID), Kind: string(ev.Kind), Status: "DUPLICATE",
					Revision: revision,
				}, nil, false, nil
			}
		}
		state.LifecycleEvents = append(state.LifecycleEvents, LifecycleEventRecord{
			Provider: ev.Lifecycle.Provider, Correlation: ev.Lifecycle.Correlation, Identity: ev.Lifecycle.Identity,
			Event: ev.Lifecycle.Event, Digest: payloadDigest,
		})
		state.verifyLifecycleCorrelation(ev.Lifecycle.Correlation, nextRevision)
		status := "ACCEPTED"
		if !state.lifecycleClaimed(ev.Lifecycle.Correlation) {
			status = "BUFFERED"
		}
		return Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: status,
			Revision: nextRevision,
		}, state, true, nil
	}
	return Acceptance{}, nil, false, &RejectedError{Code: CodeUnknownEventKind, Detail: fmt.Sprintf("event kind %q unhandled", ev.Kind)}
}

// completeResult 在 result（或配对的暂存 result）接纳后处理对应 expected
// 项。PASS 完成 step 并补位；失败先按 failure-class 路由。引擎瞬态和
// UNKNOWN/用户动作路径保留当前 Attempt，不把 engine 故障伪装成 agent。
// resultPayloadDigest 是 worker result 的结果级摘要定义（唯一计算点）：
// canonical 编码涵盖 action 身份、provider、outcome、载荷摘要、失败类与
// 审查证据，不含事件 ID——同一载荷换事件 ID 仍按它判幂等/冲突。
func resultPayloadDigest(payload *WorkerResultPayload) string {
	return digestOfCanonical(workerResultEventWire{
		ActionID: payload.ActionID, Provider: payload.Provider,
		Outcome: payload.Outcome, PayloadDigest: payload.PayloadDigest,
		SourceResult: payload.SourceResult, InterruptionReason: payload.InterruptionReason, InvalidReason: payload.InvalidReason, FailureClass: string(payload.FailureClass), Review: payload.Review, Validation: payload.Validation,
	})
}

// workerResultRecord 把一条 worker result 载荷落成台账记录（摘要为结果
// 级）：Submit 接纳、result-before-receipt 暂存与持久隔离共用同一形态。
func workerResultRecord(payload *WorkerResultPayload) WorkerResult {
	return WorkerResult{
		ActionID: payload.ActionID, Provider: payload.Provider,
		Outcome: payload.Outcome, PayloadDigest: payload.PayloadDigest,
		SourceResult: payload.SourceResult, InterruptionReason: payload.InterruptionReason, InvalidReason: payload.InvalidReason, FailureClass: payload.FailureClass, Review: payload.Review, Validation: payload.Validation,
		Digest: resultPayloadDigest(payload),
	}
}

// workerResultPayloadOf 从台账记录还原接纳核心的载荷形态（隔离重放路径）。
func workerResultPayloadOf(record WorkerResult) *WorkerResultPayload {
	return &WorkerResultPayload{
		ActionID: record.ActionID, Provider: record.Provider, Outcome: record.Outcome,
		SourceResult: record.SourceResult, InterruptionReason: record.InterruptionReason, InvalidReason: record.InvalidReason, PayloadDigest: record.PayloadDigest, FailureClass: record.FailureClass, Review: record.Review, Validation: record.Validation,
	}
}

// issuedRevisionOf 返回 actionID 派发的签发代数（签发路径唯一写
// DispatchIssueRevisions；终态清理不删，terminal 任务的迟到结果仍以
// 真实代数过重走 floor 校验）。
func (s *State) issuedRevisionOf(actionID string) uint64 {
	return s.DispatchIssueRevisions[actionID]
}

// admitWorkerResultRecord 是 typed worker result 的唯一接纳核心（Submit 与
// 隔离重放共用）：结果级摘要幂等/冲突判定、obsolete 认领、重走态的
// floor 比对（pre-floor 派发按声明终止并重签发）、result-before-receipt
// 暂存（先校验审查身份与授权）与回执配对后的完整内容接纳。返回接纳回执与是
// 否落账变更。
func (e *Engine) admitWorkerResultRecord(state *State, actionID string, result *WorkerResultPayload, eventID, requestID string, revision, nextRevision uint64, expectedFingerprint string) (Acceptance, bool, error) {
	payloadDigest := resultPayloadDigest(result)
	if existing, seen := state.Results[actionID]; seen {
		if existing.Digest != payloadDigest {
			if existing.Validation != nil && (state.validationRole(existing.Validation.Binding.Task, e.cfg.Definition) == "engine.development.batch" || state.validationRole(existing.Validation.Binding.Task, e.cfg.Definition) == "engine.repair") {
				for _, prior := range state.RecoverableResults[actionID] {
					if prior.Digest == payloadDigest {
						return Acceptance{EventID: eventID, Kind: string(KindWorkerResult), Status: "DUPLICATE", Revision: revision, ActionID: actionID}, false, nil
					}
				}
			}
			if !state.developmentContinuation(actionID, e.cfg.Definition) || result.SourceResult != existing.Digest {
				return Acceptance{}, false, &RejectedError{
					Code:   CodeReceiptConflict,
					Detail: fmt.Sprintf("action %q worker result replayed with different bytes (recorded %s, submitted %s)", actionID, existing.Digest, payloadDigest),
				}
			}
		} else {
			return Acceptance{EventID: eventID, Kind: string(KindWorkerResult), Status: "DUPLICATE", Revision: revision, ActionID: actionID}, false, nil
		}
	}
	if obsolete, known := state.ObsoleteActions[actionID]; known {
		if history := state.ObsoleteResults[actionID]; len(history) != 0 {
			for _, existing := range history {
				if existing.Digest == payloadDigest {
					return Acceptance{EventID: eventID, Kind: string(KindWorkerResult), Status: "OBSOLETE_RESULT", Revision: revision, ActionID: actionID}, false, nil
				}
			}
			return Acceptance{}, false, &RejectedError{Code: CodeReceiptConflict, Detail: fmt.Sprintf("obsolete action %q worker result replayed with different bytes", actionID)}
		}
		// 过期账保留原始证据供追溯：记录形态与接纳路径同源（含审查证据）。
		state.archiveObsoleteResult(workerResultRecord(result))
		state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{ActionID: actionID, Task: obsolete.Task, AttemptID: obsolete.AttemptID, Class: result.FailureClass, Action: RecoveryOperator, Detail: "late result belongs to an obsolete Attempt", Revision: nextRevision})
		return Acceptance{EventID: eventID, Kind: string(KindWorkerResult), Status: "OBSOLETE_RESULT", Revision: nextRevision, ActionID: actionID}, true, nil
	}
	for _, recoverable := range state.RecoverableResults[actionID] {
		if recoverable.Digest == payloadDigest {
			return Acceptance{EventID: eventID, Kind: string(KindWorkerResult), Status: "DUPLICATE", Revision: revision, ActionID: actionID}, false, nil
		}
	}
	// 系列身份以签发标记为唯一权威：在途派发读 PendingAction；簿记已收
	// 回的 terminal 派发从回执固化的任务步骤推导（签发标记与步骤标记同
	// 源，issueInto 以同一函数落标记）。审查证据只接纳自查发时携带同系
	// 列标记的派发：未标记的派发没有可绑定的系列身份，标记不一致的证据
	// 与签发事实冲突——都可区分拒绝、零状态变化。
	mark := review.Action("")
	if pending, ok := state.PendingActions[actionID]; ok {
		mark = pending.ReviewSeries
	} else if receipt, receipted := state.SpawnReceipts[actionID]; receipted {
		mark = e.reviewSeriesOf(receipt.Task.Step)
	}
	if result.Review != nil && result.Review.Action != mark {
		return Acceptance{}, false, &RejectedError{
			Code: CodeReviewEvidenceRejected,
			Detail: fmt.Sprintf("dispatch %q was issued for the %q review series; its evidence declares %s (review evidence is only admissible from a dispatch issued for the same series)",
				actionID, mark, result.Review.Action),
		}
	}
	if result.Review != nil {
		if _, err := state.reviewAdmission(result.Review, actionID, state.issuedRevisionOf(actionID)); err != nil {
			return Acceptance{}, false, err
		}
	}
	if state.validationDispatchBlocked(actionID) || state.abortRequested() {
		return state.stageWorkerResult(workerResultRecord(result), eventID, revision, nextRevision, false, "STAGED")
	}
	if _, pending := state.PendingActions[actionID]; !pending {
		// terminal 对账路径：派发簿记已随任务终态收回，准入按结果类别分层
		// （§7.2.2:4 对确认续用项的恢复/接纳不因任务经 TASK_PROGRESS 到
		// terminal 而失效）：
		//   - 真实结果（PASS 或携审查证据）仍须在途观察事实回执（SPAWNED，
		//     或经 lifecycle 对账挂接的 ATTACHED）——其入账要完成步骤与候
		//     选记账，观察事实是前提；UNKNOWN 尚待对账，不接纳真实结果。
		//   - 失败结果（非 PASS 且无审查证据）接受任何签出证明回执
		//     （issuedWitnessReceipt，含 UNKNOWN）——僵尸出口对一切计入者
		//     可用：不建候选、不占轮、不完成步骤，承诺落定释放容量。
		// 无回执或 FAILED 的迟到结果仍是未知 action（FAILED 从未签出去过）。
		receipt, receipted := state.SpawnReceipts[actionID]
		failureResult := result.Outcome != OutcomePass && result.Review == nil
		admissible := receipted && issuedWitnessReceipt(receipt.Status) &&
			(inflightWitnessReceipt(receipt.Status) || failureResult)
		if !admissible {
			return Acceptance{}, false, &RejectedError{
				Code:   CodeUnknownAction,
				Detail: fmt.Sprintf("worker result references unknown or completed action %q", actionID),
			}
		}
		// 计量来源未决（lifecycle stop 未到）时迟到结果同样留在回执边界：
		// COST_PENDING，配对扫描在来源落定后经同一接纳路径补齐。
		if e.dispatchCostHeld(state, actionID) {
			return state.stageWorkerResult(workerResultRecord(result), eventID, revision, nextRevision, false, StatusCostPending)
		}
		if retained, ok := state.EndedAttempts[receipt.Task]; ok && retained.ActionID == actionID && retained.AwaitingResult {
			retained.AwaitingResult = false
			state.Attempts[receipt.Task] = retained
			state.PendingActions[actionID] = PendingAction{ActionID: actionID, Task: receipt.Task, Step: string(receipt.Task.Step), AttemptID: retained.ID, ReviewSeries: e.reviewSeriesOf(receipt.Task.Step)}
			delete(state.EndedAttempts, receipt.Task)
			detail := "late result reconciled against the terminal task"
			if requestID != "" {
				detail += " by requirement decision " + requestID
			}
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{ActionID: actionID, Task: receipt.Task, Detail: detail, Revision: nextRevision})
			refill, err := e.completeResult(state, workerResultRecord(result), nextRevision, expectedFingerprint)
			if err != nil {
				return Acceptance{}, false, err
			}
			status := "ACCEPTED"
			if rejected, ok := state.RejectedResults[actionID]; ok {
				status = rejected.Status
			}
			return Acceptance{EventID: eventID, Kind: string(KindWorkerResult), Status: status, Revision: nextRevision, ActionID: actionID, Refill: refill}, true, nil
		}
		record := workerResultRecord(result)
		if record.Review != nil {
			updated, err := e.prepareReviewResult(state, record.Review, actionID)
			if err != nil {
				return Acceptance{}, false, err
			}
			state.ReviewSeries = updated
		}
		state.Results[actionID] = record
		detail := "late result reconciled against the terminal task"
		if requestID != "" {
			detail = fmt.Sprintf("late result reconciled against the terminal task by requirement decision %s", requestID)
		}
		state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
			ActionID: actionID, Task: receipt.Task,
			Detail:   detail,
			Revision: nextRevision,
		})
		// PASS 的机械后果与结果先到的完成路径（completeResult）同源：任
		// 务迁移已由 TASK_PROGRESS 完成，此处不做；步骤完成（载体步＝系
		// 列首过闩，验证期步＝管线收口，其余 agent 步凭 PASS 结果）、
		// frontier settle 与补位签发照常，两种完成顺序到达同一终态
		// （§8.2.3）。
		if record.Outcome == OutcomePass && state.validationRole(receipt.Task, e.cfg.Definition) == "" && state.passResultCompletesStep(receipt.Task.Step, e.reviewSeriesOf) {
			if err := state.CompleteStep(receipt.Task.Step, e.cfg.Definition); err != nil {
				return Acceptance{}, false, fmt.Errorf("protocol: complete step %s: %w", receipt.Task.Step, err)
			}
		}
		if err := state.settleFrontierSteps(e.cfg.Definition, e.cfg.InternalSteps); err != nil {
			return Acceptance{}, false, err
		}
		refill, refillErr := e.refill(state, nextRevision, expectedFingerprint)
		if refillErr != nil {
			return Acceptance{}, false, refillErr
		}
		return Acceptance{
			EventID: eventID, Kind: string(KindWorkerResult), Status: "ACCEPTED",
			Revision: nextRevision, ActionID: actionID, Refill: refill,
		}, true, nil
	}
	// 重走态的 floor 判定与按声明终止只在标记的系列上执行——系列身份
	// 的唯一权威是签发标记（见上方的一致性判定）。
	if mark != "" {
		if series, ok := state.ReviewSeries.Series(mark); ok && seriesInRedoStale(series) && state.issuedRevisionOf(actionID) < series.FreshFloor {
			return e.terminateSupersededDispatch(state, actionID, mark, workerResultRecord(result), eventID, requestID, nextRevision)
		}
	}
	record := workerResultRecord(result)
	if receipt, receipted := state.SpawnReceipts[actionID]; !receipted || receipt.Status == SpawnStatusUnknown {
		// Identity and authorization are checked before staging. Worker content
		// is completed through the same finite-attempt path once paired.

		return state.stageWorkerResult(record, eventID, revision, nextRevision, receipted, "STAGED")
	}
	// 计量来源未决（lifecycle stop 未到）时结果留在回执边界：COST_PENDING，
	// 事务收尾的配对扫描在来源落定后同一接纳路径补齐。
	if e.dispatchCostHeld(state, actionID) {
		return state.stageWorkerResult(record, eventID, revision, nextRevision, false, StatusCostPending)
	}
	refill, err := e.completeResult(state, record, nextRevision, expectedFingerprint)
	if err != nil {
		return Acceptance{}, false, err
	}
	acceptance := Acceptance{
		EventID: eventID, Kind: string(KindWorkerResult), Status: "ACCEPTED",
		Revision: nextRevision, ActionID: actionID, Refill: refill,
	}
	if rejected, ok := state.RejectedResults[actionID]; ok {
		acceptance.Status = rejected.Status
	}
	if _, obsolete := state.ObsoleteActions[actionID]; obsolete && len(state.ObsoleteResults[actionID]) > 0 {
		acceptance.Status = "OBSOLETE_RESULT"
	}
	if class := record.FailureClass; class != "" {
		recoveryAction := RouteFailure(class).Action
		if len(state.RecoveryRecords) > 0 {
			last := state.RecoveryRecords[len(state.RecoveryRecords)-1]
			if last.ActionID == actionID && last.Revision == nextRevision {
				recoveryAction = last.Action
			}
		}
		acceptance.FailureClass = string(class)
		acceptance.RecoveryAction = string(recoveryAction)
	}
	return acceptance, true, nil
}

// Staging preserves delivered evidence until its receipt and execution
// authorization both allow the common result acceptance path to run.
// status is the acceptance boundary the result is held at (STAGED for a
// missing/unknown receipt; COST_PENDING while the metering source is pending).
func (s *State) stageWorkerResult(record WorkerResult, eventID string, revision, nextRevision uint64, reconcile bool, status string) (Acceptance, bool, error) {
	if staged, exists := s.StagedResults[record.ActionID]; exists {
		if staged.Digest == record.Digest {
			return Acceptance{EventID: eventID, Kind: string(KindWorkerResult), Status: "DUPLICATE", Revision: revision, ActionID: record.ActionID}, false, nil
		}
		return Acceptance{}, false, &RejectedError{Code: CodeReceiptConflict, Detail: fmt.Sprintf("action %q already has a different staged worker result", record.ActionID)}
	}
	s.StagedResults[record.ActionID] = record
	acceptance := Acceptance{EventID: eventID, Kind: string(KindWorkerResult), Status: status, Revision: nextRevision, ActionID: record.ActionID}
	if reconcile {
		acceptance.RecoveryAction = string(RecoveryReconcile)
	}
	return acceptance, true, nil
}

// terminateSupersededDispatch retains stale evidence and closes its task.
// The transaction's normal refill issues any outstanding fresh-review work.
func (e *Engine) terminateSupersededDispatch(state *State, actionID string, mark review.Action, record WorkerResult, eventID, requestID string, nextRevision uint64) (Acceptance, bool, error) {
	if err := state.retireReviewDispatch(actionID, mark, requestID, nextRevision); err != nil {
		return Acceptance{}, false, err
	}
	state.archiveObsoleteResult(record)
	return Acceptance{
		EventID: eventID, Kind: string(KindWorkerResult), Status: "OBSOLETE_DISPATCH",
		Revision: nextRevision, ActionID: actionID,
	}, true, nil
}

// pairSuspendedResults 补齐挂起的配对：回执已在账（SPAWNED，或经恢复
// 挂接到 lifecycle 身份的 ATTACHED——同为派发观察事实）、暂存在账、
// 尚无入账结果的条目逐个经唯一接纳核心补配对。仍被顺序门拦住的条目
// 保持挂起（下一次开门迁移再试）；任何失败经快照恢复零半套账。本函
// 数只由事务收尾单点调用（屏障在位时收尾不调）。
func (e *Engine) pairSuspendedResults(state *State, nextRevision uint64, expectedFingerprint string) (int, []string) {
	if len(state.StagedResults) == 0 {
		return 0, nil
	}
	paired := 0
	var refills []string
	// 一次配对可能开门（产品审结论重新 PASS）使同批另一条挂起条目变为
	// 可配——迭代到不动点，两种完成顺序终态等价。
	for {
		ids := make([]string, 0, len(state.StagedResults))
		for actionID := range state.StagedResults {
			if state.validationDispatchBlocked(actionID) {
				continue
			}
			receipt, receipted := state.SpawnReceipts[actionID]
			if !receipted || !inflightWitnessReceipt(receipt.Status) {
				continue
			}
			// 计量来源仍 PENDING 的暂存结果保持挂起：lifecycle stop 证据落定
			// 的下一事务在同一配对路径补齐。
			if e.dispatchCostHeld(state, actionID) {
				continue
			}
			if _, admitted := state.Results[actionID]; admitted {
				continue
			}
			ids = append(ids, actionID)
		}
		slices.Sort(ids)
		progressed := false
		for _, actionID := range ids {
			snapshot, cloneErr := cloneState(state)
			if cloneErr != nil {
				continue
			}
			payload := workerResultPayloadOf(state.StagedResults[actionID])
			pairAcceptance, _, err := e.admitWorkerResultRecord(state, actionID, payload, "pair:"+actionID, "", nextRevision, nextRevision, expectedFingerprint)
			if err != nil {
				*state = *snapshot
				if isSequenceGate(err) {
					// 顺序门未开：回执已在账，暂存保留、配对挂起——开门迁
					// 移（含本批更早条目开门后的下一轮迭代）自动补配对。
					state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
						ActionID: actionID,
						Detail:   "the staged result stays pending until the product-review conclusion re-passes",
						Revision: nextRevision,
					})
				} else {
					// 其余接纳失败同样不作废：暂存保留、配对挂起，每次尝试
					// 都留痕（重试不静默）——账本变化后的下一次收尾再试，
					// 派发的新结果照常经对账路径入账。
					state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
						ActionID: actionID,
						Detail:   fmt.Sprintf("the staged result stays suspended; the pairing attempt failed: %v", err),
						Revision: nextRevision,
					})
				}
				continue
			}
			delete(state.StagedResults, actionID)
			paired++
			progressed = true
			refills = append(refills, pairAcceptance.Refill...)
		}
		if !progressed {
			return paired, refills
		}
	}
}

func (e *Engine) completeResult(state *State, record WorkerResult, nextRevision uint64, expectedFingerprint string) ([]string, error) {
	if !state.prewaveDispatchCurrent(record.ActionID, e.cfg.Definition) {
		return nil, e.retirePrewaveResult(state, record)
	}
	pending := state.PendingActions[record.ActionID]
	key := pending.Task
	if record.InvalidReason != "" {
		return nil, state.endInvalidAttempt(record.ActionID, "REJECTED_INVALID_RESULT", record.InvalidReason, record, nextRevision)
	}
	class := record.FailureClass
	if e.businessTask(key) && record.Outcome == OutcomeRuntimeError && class == authoring.FailureTransientEngine {
		if record.Validation != nil {
			if state.validationRole(key, e.cfg.Definition) != "engine.qa.execution" || !record.Validation.Checkpoint || record.Validation.Execution == nil {
				return nil, state.endInvalidAttempt(record.ActionID, "REJECTED_INVALID_RESULT", "runtime progress requires a QA execution checkpoint", record, nextRevision)
			}
			next, err := e.prepareValidationResult(state, record, nextRevision)
			if err != nil {
				return nil, state.endInvalidAttempt(record.ActionID, "REJECTED_INVALID_RESULT", err.Error(), record, nextRevision)
			}
			state.Validation = next
		}
		return nil, state.endInvalidAttempt(record.ActionID, OutcomeRuntimeError, "worker reported runtime error", record, nextRevision)
	}
	if record.Outcome == OutcomePass && state.reviewSeriesBooked(pending.ReviewSeries) && record.Review == nil {
		return nil, state.endInvalidAttempt(record.ActionID, "REJECTED_INVALID_RESULT", "signed review duty requires review evidence", record, nextRevision)
	}
	var preparedReview review.Set
	if record.Review != nil {
		var err error
		preparedReview, err = e.prepareReviewResult(state, record.Review, record.ActionID)
		if err != nil {
			if rejected, ok := err.(*RejectedError); ok && rejected.Code == CodeReviewEvidenceRejected {
				return nil, state.endInvalidAttempt(record.ActionID, "REJECTED_INVALID_RESULT", rejected.Detail, record, nextRevision)
			}
			return nil, err
		}
	}
	if class == "" && state.validationRole(key, e.cfg.Definition) != "" {
		next, err := e.prepareValidationResult(state, record, nextRevision)
		if err != nil {
			return nil, state.endInvalidAttempt(record.ActionID, "REJECTED_INVALID_RESULT", err.Error(), record, nextRevision)
		}
		state.Validation = next
		if record.Validation != nil && record.Validation.Checkpoint {
			return nil, nil
		}
		if state.validationRole(key, e.cfg.Definition) == "engine.development.batch" || record.Validation != nil && record.Validation.Status == "CONTINUE" {
			if previous, ok := state.Results[record.ActionID]; ok {
				state.RecoverableResults[record.ActionID] = append(state.RecoverableResults[record.ActionID], previous)
			}
			state.Results[record.ActionID] = record
			return nil, nil
		}
	}
	if class != "" {
		plan := DecideRecovery(Interruption{Class: class})
		attempt := state.Attempts[key]
		if class == authoring.FailureTransientEngine && attempt.MaxAttempts > 0 {
			if attempt.RetryExhausted {
				plan.Action = RecoveryWait
				plan.Detail = fmt.Sprintf("transient retry attempts exhausted (%d/%d); wait for terminal handling", attempt.Attempts, attempt.MaxAttempts)
			} else {
				// A transient result consumes one declared retry attempt while
				// preserving the same logical Attempt identity for the resumable
				// path.
				attempt.Attempts++
				if attempt.Attempts >= attempt.MaxAttempts {
					attempt.RetryExhausted = true
					plan.Action = RecoveryWait
					plan.Detail = fmt.Sprintf("transient retry attempts exhausted (%d/%d); wait for terminal handling", attempt.Attempts, attempt.MaxAttempts)
				}
				state.Attempts[key] = attempt
			}
		}
		state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
			ActionID: record.ActionID, Task: key, AttemptID: pending.AttemptID,
			Class: class, Action: plan.Action, Detail: plan.Detail, InterruptionReason: record.InterruptionReason, Revision: nextRevision,
		})
		if plan.Action != RecoveryFail {
			state.RecoverableResults[record.ActionID] = append(state.RecoverableResults[record.ActionID], record)
			return nil, nil
		}
	}
	if record.Review != nil {
		state.ReviewSeries = preparedReview
	}
	if err := state.terminalizeTask(key); err != nil {
		return nil, err
	}
	if previous, ok := state.Results[record.ActionID]; ok && previous.Validation != nil && previous.Validation.Status == "CONTINUE" {
		state.RecoverableResults[record.ActionID] = append(state.RecoverableResults[record.ActionID], previous)
	}
	state.Results[record.ActionID] = record
	if record.Outcome != OutcomePass {
		// FAIL 路由经终态化释放了容量：收尾补位据此执行（PASS 的补位在本
		// 函数尾部完成路径）。
		state.routingReleasedCapacity = true
		return nil, nil
	}
	// PASS 结果完成其步骤：验证期步除外（其任务跨 wave 反复签发，完成事
	// 实＝管线收口，由 settle 的验证期规则收口）；其余非载体 agent 步直接
	// 完成；载体步完成 ⟺ 所属系列首过闩（语义 FAIL 的证据不置闩，载体保
	// 持开放等待下一轮）。完成后补一次 frontier settle——外部边界事实与其
	// 余载体按依赖序补齐。
	if state.validationRole(key, e.cfg.Definition) == "" && state.passResultCompletesStep(authoring.StepID(pending.Step), e.reviewSeriesOf) {
		if err := state.CompleteStep(authoring.StepID(pending.Step), e.cfg.Definition); err != nil {
			return nil, fmt.Errorf("protocol: complete step %s: %w", pending.Step, err)
		}
	}
	if err := state.settleFrontierSteps(e.cfg.Definition, e.cfg.InternalSteps); err != nil {
		return nil, err
	}
	return e.refill(state, nextRevision, expectedFingerprint)
}

// applyFailedSpawnRouting 执行一条 FAILED spawn 回执的失败路由：瞬态重试
// 记账（业务耗尽交 EndedAttempts 产生 typed Ask）、暂存结果删除（失败派发边界的结果不能由它结算）、
// FAIL 路由 terminalize、RECOVER 路由的 UNKNOWN 账面桥接。立即接纳与屏
// 障解除的统一执行共用本函数。
func (e *Engine) applyFailedSpawnRouting(s *State, actionID string, receipt SpawnReceipt, nextRevision uint64) FailureRoute {
	route := RouteFailure(receipt.FailureClass)
	if (s.abortRequested() || !s.prewaveDispatchCurrent(actionID, e.cfg.Definition)) && route.Action != RecoveryReconcile {
		pending, retired := s.retireAction(actionID, "", "failed spawn no longer has a current business responsibility")
		if retired {
			_ = s.terminalizeTask(pending.Task)
			delete(s.EndedAttempts, pending.Task)
			s.routingReleasedCapacity = true
		}
		return route
	}
	pending, known := s.PendingActions[actionID]
	if !known {
		// 派发簿记已被收回（如任务先到 terminal）：只落路由留痕。
		s.appendRecovery(RecoveryRecord{ActionID: actionID, Class: route.Class, Action: route.Action,
			Detail: route.ActionDetail() + " (the dispatch bookkeeping is already reclaimed)", Revision: nextRevision})
		return route
	}
	if route.Class == authoring.FailureTransientEngine {
		attempt := s.Attempts[pending.Task]
		if attempt.MaxAttempts > 0 {
			if !attempt.RetryExhausted {
				attempt.Attempts++
				if attempt.Attempts >= attempt.MaxAttempts {
					attempt.RetryExhausted = true
				}
			}
			s.Attempts[pending.Task] = attempt
			if attempt.RetryExhausted {
				route.Action = RecoveryWait
				if attempt.Business {
					s.endBusinessAttempt(pending, attempt, "spawn transport attempts exhausted", nextRevision)
					return route
				}
				s.appendRecovery(RecoveryRecord{ActionID: actionID, Task: pending.Task, AttemptID: pending.AttemptID,
					Class: route.Class, Action: route.Action,
					Detail:   fmt.Sprintf("transient retry attempts exhausted (%d/%d); wait for terminal handling", attempt.Attempts, attempt.MaxAttempts),
					Revision: nextRevision})
				delete(s.StagedResults, actionID)
				return route
			}
		}
	}
	// A FAILED spawn cannot settle a result that arrived before the receipt.
	// The result belongs to the failed dispatch boundary and must be submitted
	// again after a later retry.
	delete(s.StagedResults, actionID)
	s.appendRecovery(RecoveryRecord{
		ActionID: actionID, Task: pending.Task, AttemptID: pending.AttemptID,
		Class: route.Class, Action: route.Action, Detail: route.ActionDetail(), Revision: nextRevision,
	})
	if route.Action == RecoveryFail {
		if err := s.terminalizeTask(pending.Task); err != nil {
			s.appendRecovery(RecoveryRecord{ActionID: actionID, Detail: fmt.Sprintf("terminalize after a failed spawn: %v", err), Revision: nextRevision})
		} else {
			// 终态化收回了派发簿记：容量释放是本事务的业务工作，收尾补位
			// 据此执行（transient：收尾读取即复位，不入账本）。
			s.routingReleasedCapacity = true
		}
	}
	intent := s.PendingHostActions[actionID]
	if route.Action == RecoveryReconcile && (intent.Adapter == nil || !nativeOperation(intent.Adapter.Operation)) {
		// Keep the wire-level FAILED receipt in SpawnFailures, while exposing
		// an UNKNOWN ledger entry for external reconciliation.
		unknown := receipt
		unknown.Status = SpawnStatusUnknown
		s.SpawnReceipts[actionID] = unknown
	}
	return route
}

// applyFailedHostActionRouting 执行一条 FAILED host action 回执的失败路由：
// 原生 FAILED 保留原状态；其它 adapter 按既有恢复契约对账。
// 立即接纳与屏障解除的统一执行共用本函数。
func (s *State) applyFailedHostActionRouting(actionID string, receipt HostActionReceipt, nextRevision uint64) FailureRoute {
	route := RouteFailure(receipt.FailureClass)
	s.appendRecovery(RecoveryRecord{
		ActionID: actionID, Class: route.Class, Action: route.Action,
		Detail: route.ActionDetail(), Revision: nextRevision,
	})
	intent := s.PendingHostActions[actionID]
	if route.Action == RecoveryReconcile && (intent.Adapter == nil || !nativeOperation(intent.Adapter.Operation)) {
		// Keep the wire-level FAILED receipt in HostActionFailures while
		// exposing an UNKNOWN ledger entry, mirroring the spawn-side bridge:
		// without it the declared RECOVER route has no reconciliation entry
		// (ReconcileHostAction requires a durable UNKNOWN receipt) and the
		// recovery instruction dead-ends.
		unknown := receipt
		unknown.Status = HostActionStatusUnknown
		s.HostActionReceipts[actionID] = unknown
	}
	if route.Action == RecoveryFail {
		intent := s.PendingHostActions[actionID]
		if intent.Adapter == nil || !nativeOperation(intent.Adapter.Operation) {
			delete(s.PendingHostActions, actionID)
		}
	}
	return route
}

// terminalizeTask uses the legal terminal edge and releases task bookkeeping.
// Result admission, replacement and termination share this completion boundary.
func (s *State) terminalizeTask(key runtime.TaskKey) error {
	if s.TaskStatusOf(key) != runtime.TaskTerminal {
		if err := s.TransitionTask(key, runtime.TaskTerminal); err != nil {
			return &RejectedError{Code: CodeIllegalTransition, Detail: fmt.Sprintf("complete %s: %v", key.String(), err)}
		}
	}
	s.removeTaskBookkeeping(key)
	return nil
}

// refill 按 「submit 接纳事件后立即继续 Decide/SelectIssued」：对推进后
// 的视图重新决策，Ready 计划经 SelectIssued 按容量裁剪后落账（复用与
// IssueFromPlan 完全相同的签发语义）。修订屏障（Phase A）期间静默扣发：
// 触发本函数的回执/结果/决定事件本身合法且必须接纳，被冻结的只是"从中
// 继续签发新任务"这一步——屏障解除后的下一次提交会补进。
func (e *Engine) refill(state *State, nextRevision uint64, expectedFingerprint string) ([]string, error) {
	if state.abortRequested() {
		return nil, nil
	}
	if state.requirementChangePending() {
		return nil, nil
	}
	if _, err := e.advanceValidation(state, nextRevision); err != nil {
		return nil, err
	}
	if err := e.releaseUpstreamVoidIfReleasable(state); err != nil {
		return nil, err
	}
	e.reconcileReviewGrantAsks(state)
	plan, err := e.decide(state)
	if err != nil {
		return nil, fmt.Errorf("protocol: refill decide: %w", err)
	}
	if plan.Next.Kind != decision.KindReady {
		return nil, nil
	}
	issued, err := decision.SelectIssued(plan, decision.Admission{Capacity: max(0, e.cfg.Capacity-len(state.PendingActions))}, ackStore{})
	if err != nil {
		return nil, fmt.Errorf("protocol: refill select: %w", err)
	}
	state.retainExpected(plan.Next.Ready)
	identity, err := identityOfPlan(plan)
	if err != nil {
		return nil, fmt.Errorf("protocol: refill plan identity: %w", err)
	}
	issued, err = state.issueInto(issued, nextRevision, expectedFingerprint, state.RunProvider, identity, e.retryMaxAttempts, e.reviewSeriesOf, e.guardIssuanceGate(state))
	for _, action := range issued {
		e.bindBusinessAttempt(state, action.Task)
	}
	if err != nil {
		var rejected *RejectedError
		if errors.As(err, &rejected) && rejected.Code == CodeGuardAllowanceBlocked {
			// 派发预算边界＝零签发，不是错误：触发本函数的回执/结果/决定事件
			// 本身合法且必须接纳（Ask 已由 advanceValidation 落账，随本事务提
			// 交）——与修订屏障的静默扣发同一形状。账目错位（BLOCKED_BUG）
			// 仍否决提交。
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, action := range issued {
		ids = append(ids, action.ActionID)
	}
	return ids, nil
}

// ExecuteHostAction is the adapter-operation convenience entry for the closed
// HostAction union. The operation must be registered and its parameters must
// satisfy the configured schema before any intent is persisted.
func (e *Engine) ExecuteHostAction(operation authoring.OperationID, params any, expectedFingerprint string) (HostActionIntent, uint64, error) {
	return e.executeHostAction(operation, params, "", expectedFingerprint)
}

// ExecuteHostActionWithCorrelation is the harness/adapter form that binds a
// known transport correlation into the durable intent. ExecuteHostAction keeps
// the field empty when the host chooses the correlation only after execution.
func (e *Engine) ExecuteHostActionWithCorrelation(operation authoring.OperationID, params any, correlation, expectedFingerprint string) (HostActionIntent, uint64, error) {
	return e.executeHostAction(operation, params, correlation, expectedFingerprint)
}

func (e *Engine) executeHostAction(operation authoring.OperationID, params any, correlation, expectedFingerprint string) (HostActionIntent, uint64, error) {
	if operation == "" {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeFreeCommandForm, Detail: "host action requires an operation reference; free command forms do not exist"}
	}
	revision, state, err := e.load()
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	if state == nil {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	intent, err := e.prepareAdapterIntent(state, revision+1, operation, params, correlation)
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	return e.persistHostAction(state, revision, intent, expectedFingerprint)
}

// ResumeAgent persists a RESUME_AGENT intent bound to a current worker Attempt.
func (e *Engine) ResumeAgent(workerActionID, identity, expectedFingerprint string) (HostActionIntent, uint64, error) {
	return e.issueAgentHostAction(HostActionResumeAgent, workerActionID, identity, "", "", expectedFingerprint)
}

// TerminateAgent persists a TERMINATE_AGENT intent bound to a current worker
// Attempt. A non-empty reason is part of the durable typed payload.
func (e *Engine) TerminateAgent(workerActionID, identity, reason, expectedFingerprint string) (HostActionIntent, uint64, error) {
	return e.TerminateAgentWithCorrelation(workerActionID, identity, reason, "", expectedFingerprint)
}

// TerminateAgentWithCorrelation binds a caller-established lifecycle transport
// correlation, for hosts that fix the correlation before execution instead of
// generating it when the intent is issued.
func (e *Engine) TerminateAgentWithCorrelation(workerActionID, identity, reason, correlation, expectedFingerprint string) (HostActionIntent, uint64, error) {
	if strings.TrimSpace(reason) == "" {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeEventSchemaInvalid, Detail: "TERMINATE_AGENT reason is empty"}
	}
	return e.issueAgentHostAction(HostActionTerminateAgent, workerActionID, identity, reason, correlation, expectedFingerprint)
}

func (e *Engine) issueAgentHostAction(operation HostActionOperation, workerActionID, identity, reason, correlation, expectedFingerprint string) (HostActionIntent, uint64, error) {
	if operation != HostActionResumeAgent && operation != HostActionTerminateAgent {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeFreeCommandForm, Detail: fmt.Sprintf("agent host action operation %q is invalid", operation)}
	}
	if strings.TrimSpace(identity) == "" {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeEventSchemaInvalid, Detail: "agent host action identity is empty"}
	}
	revision, state, err := e.load()
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	if state == nil {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if operation == HostActionResumeAgent {
		if blocked := e.guardEffectBudget(state, workerActionID, revision+1); blocked != nil {
			// 落 Ask 后提交（Ask 即进展），再以可区分错误拒绝本次效果创建。
			if _, err := e.commit(state, revision, expectedFingerprint); err != nil {
				return HostActionIntent{}, 0, err
			}
			return HostActionIntent{}, 0, blocked
		}
	}
	intent, err := e.agentHostIntent(state, operation, workerActionID, identity, reason, correlation, revision+1)
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	return e.persistHostAction(state, revision, intent, expectedFingerprint)
}

func (e *Engine) persistHostAction(state *State, revision uint64, intent HostActionIntent, expectedFingerprint string) (HostActionIntent, uint64, error) {
	if state.abortRequested() && intent.Operation != HostActionTerminateAgent && (intent.Adapter == nil || intent.Adapter.Operation != "op.cleanup") {
		return HostActionIntent{}, 0, validationError("run abort permits only outstanding effect reconciliation")
	}
	// Submit 之外的落账路径同样只经处置表：屏障在位时新的 HostAction
	// intent 属"停止新的权威写入"而被拒绝。
	if state.requirementChangePending() {
		if err := state.admitInCurrentState(Event{}, true); err != nil {
			return HostActionIntent{}, 0, err
		}
	}
	if _, exists := state.PendingHostActions[intent.ActionID]; exists {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeDuplicateAction, Detail: fmt.Sprintf("host action %q already exists", intent.ActionID)}
	}

	state.PendingHostActions[intent.ActionID] = intent
	commitRevision, err := e.commit(state, revision, expectedFingerprint)
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	return intent, commitRevision, nil
}

// schemaForOperation resolves the operation and its schema only from the
// compiled canonical definition. Runtime configuration cannot add a second
// operation/schema authority for the same definition digest.
func (e *Engine) schemaForOperation(operation authoring.OperationID) (authoring.SchemaID, error) {
	var schema authoring.SchemaID
	for _, step := range e.cfg.Definition.Steps {
		payload, ok := step.Payload.(compiler.CompiledHostActionStep)
		if !ok || payload.Operation != operation {
			continue
		}
		if schema != "" && schema != payload.Schema {
			return "", &RejectedError{Code: CodeOperationSchemaInvalid, Detail: fmt.Sprintf("operation %q is bound to multiple schema IDs in the canonical definition", operation)}
		}
		schema = payload.Schema
	}
	if schema == "" {
		return "", &RejectedError{
			Code:   CodeOperationNotRegistered,
			Detail: fmt.Sprintf("operation %q is not registered by the canonical definition; refusing to persist intent (host executes nothing)", operation),
		}
	}
	return schema, nil
}

// normalizeHostActionParams keeps the wire object shape used by the typed
// adapter contract. Strings, arrays, and other free command forms are rejected
// before any state is loaded or changed.
func normalizeHostActionParams(params any) (map[string]any, error) {
	if params == nil {
		return map[string]any{}, nil
	}
	data, err := json.Marshal(params)
	var object map[string]any
	if err == nil {
		err = json.Unmarshal(data, &object)
	}
	if err != nil || object == nil {
		return nil, &RejectedError{Code: CodeFreeCommandForm, Detail: fmt.Sprintf("host action params must be a structured object, got %T", params)}
	}
	return object, nil
}

func (e *Engine) validateHostActionEvidence(state *State, intent HostActionIntent, receipt *HostActionReceiptPayload) error {
	switch intent.Operation {
	case HostActionResumeAgent, HostActionTerminateAgent:
		target := intent.Resume
		if intent.Operation == HostActionTerminateAgent {
			target = intent.Terminate
		}
		if target == nil || receipt.LifecycleEvidence == nil || receipt.LifecycleEvidence.Identity != target.Identity {
			return &RejectedError{Code: CodeIntentMismatch, Detail: "host action lifecycle evidence does not match the typed agent target"}
		}
		if !state.hasLifecycleEvent(receipt.LifecycleEvidence.Identity, receipt.LifecycleEvidence.Event) {
			return &RejectedError{Code: CodeIntentMismatch, Detail: fmt.Sprintf("lifecycle evidence %s/%s is not present in the durable buffer", receipt.LifecycleEvidence.Identity, receipt.LifecycleEvidence.Event)}
		}
	case HostActionExecuteAdapterOperation:
		if intent.Adapter == nil || receipt.AdapterEvidence == nil {
			return &RejectedError{Code: CodeIntentMismatch, Detail: "adapter receipt does not match the typed adapter intent"}
		}
		if receipt.AdapterOperation != intent.Adapter.Operation {
			return &RejectedError{Code: CodeIntentMismatch, Detail: fmt.Sprintf("adapter receipt operation %q does not match intent %q", receipt.AdapterOperation, intent.Adapter.Operation)}
		}
		schema, err := e.schemaForOperation(intent.Adapter.Operation)
		if err != nil {
			return err
		}
		if intent.Adapter.Schema != schema {
			return &RejectedError{Code: CodeIntentMismatch, Detail: fmt.Sprintf("adapter intent schema %q does not match canonical schema %q", intent.Adapter.Schema, schema)}
		}
		if err := validateAdapterPayload(schema, "adapter evidence", receipt.AdapterEvidence.Values); err != nil {
			return err
		}
	default:
		return &RejectedError{Code: CodeIntentMismatch, Detail: fmt.Sprintf("pending host action operation %q is invalid", intent.Operation)}
	}
	return nil
}

func validateAdapterPayload(schema authoring.SchemaID, label string, value map[string]any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return &RejectedError{Code: CodeOperationSchemaInvalid, Detail: fmt.Sprintf("%s cannot be encoded for schema %q: %v", label, schema, err)}
	}
	decode := func(target any) error {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(target); err != nil {
			return err
		}
		if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
			if err == nil {
				return errors.New("trailing JSON data")
			}
			return err
		}
		return nil
	}
	var decodeErr error
	switch schema {
	case authoring.SchemaID("schema.workspace"):
		if label == "adapter parameters" {
			decodeErr = decode(&WorkspaceParameters{})
		} else {
			decodeErr = decode(&WorkspaceEvidence{})
		}
	case authoring.SchemaID("schema.check"):
		if label == "adapter parameters" {
			decodeErr = decode(&CheckParameters{})
		} else {
			decodeErr = decode(&CheckEvidence{})
		}
	case authoring.SchemaID("schema.cleanup"):
		if label == "adapter parameters" {
			decodeErr = decode(&CleanupParameters{})
		} else {
			decodeErr = decode(&WorkspaceCleanupReceipt{})
		}
	case authoring.SchemaID("schema.candidate.review-range"):
		if label == "adapter parameters" {
			decodeErr = decode(&CandidateParameters{})
		} else {
			decodeErr = decode(&CandidateEvidence{})
		}

	default:
		return &RejectedError{Code: CodeOperationSchemaInvalid, Detail: fmt.Sprintf("%s has no typed implementation for canonical schema %q", label, schema)}
	}
	if decodeErr != nil {
		return &RejectedError{Code: CodeOperationSchemaInvalid, Detail: fmt.Sprintf("%s does not match canonical schema %q: %v", label, schema, decodeErr)}
	}
	return nil
}

// digestOfCanonical 返回任意 wire 值 canonical 字节的 SHA-256 摘要
// （payload 级幂等判定输入；与事件级 digest 分开——重发允许换事件 ID，
// 不允许换回执字节）。
func digestOfCanonical(v any) string {
	data, err := canonicalJSON(v)
	if err != nil {
		return ""
	}
	return encoder.Digest(data)
}

// checkProvider 硬校验 provider 身份：与 run 绑定不同（含空）即拒绝，
// 不降级 default。
func (s *State) checkProvider(observed string) error {
	if observed == "" || observed != s.RunProvider {
		return &RejectedError{
			Code:   CodeProviderMismatch,
			Detail: fmt.Sprintf("provider %q != run binding %q (no default downgrade)", observed, s.RunProvider),
		}
	}
	return nil
}

// lifecyclePaired reports whether one correlation/identity pair has both
// lifecycle edges in the durable observation buffer.
func (s *State) lifecyclePaired(correlation, identity string) bool {
	var start, stop bool
	for _, record := range s.LifecycleEvents {
		if record.Correlation != correlation || record.Identity != identity {
			continue
		}
		switch record.Event {
		case LifecycleStart:
			start = true
		case LifecycleStop:
			stop = true
		}
	}
	return start && stop
}

func (s *State) lifecycleClaimed(correlation string) bool {
	for _, receipt := range s.SpawnReceipts {
		if receipt.Correlation == correlation {
			return true
		}
	}
	return false
}

// verifyLifecycleCorrelation records every paired identity for a claimed
// correlation. Keeping identities distinct makes zero/one/multiple matching
// evidence representable without trusting caller-supplied candidate lists.
func (s *State) verifyLifecycleCorrelation(correlation string, revision uint64) {
	if correlation == "" || !s.lifecycleClaimed(correlation) {
		return
	}
	for _, identity := range s.lifecycleMatches(correlation) {
		if _, verified := s.LifecycleVerified[identity]; verified {
			continue
		}
		s.LifecycleVerified[identity] = LifecycleVerification{
			Correlation: correlation, Identity: identity, Provider: s.RunProvider, Revision: revision,
		}
	}
}

func (s *State) hasLifecycleEvent(identity, event string) bool {
	for _, record := range s.LifecycleEvents {
		if record.Provider == s.RunProvider && record.Identity == identity && record.Event == event {
			return true
		}
	}
	return false
}

func (s *State) hasCorrelatedLifecycleEvent(correlation, identity, event string) bool {
	for _, record := range s.LifecycleEvents {
		if record.Provider == s.RunProvider && record.Correlation == correlation && record.Identity == identity && record.Event == event {
			return true
		}
	}
	return false
}

// Freshness 返回 request 当前 revision 下的 freshness token（协议层种子，
// 供 status 面暴露带 freshness token 的 availableActions）。request 未知
// 或已决时拒绝。
func (e *Engine) Freshness(request RequestID) (string, error) {
	revision, state, err := e.load()
	if err != nil {
		return "", err
	}
	return (Snapshot{Revision: revision, State: state}).Freshness(request)
}

// Freshness returns a pending request's token from this snapshot alone.
func (s Snapshot) Freshness(request RequestID) (string, error) {
	if s.State == nil {
		return "", &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	ask, exists := s.State.PendingAsks[string(request)]
	if !exists {
		if _, decided := s.State.Decisions[string(request)]; decided {
			return "", &RejectedError{Code: CodeRequestResolved, Detail: fmt.Sprintf("request %q already decided", request)}
		}
		return "", &RejectedError{Code: CodeUnknownRequest, Detail: fmt.Sprintf("request %q does not exist", request)}
	}
	if ask.Resolved {
		return "", &RejectedError{Code: CodeRequestResolved, Detail: fmt.Sprintf("request %q already decided", request)}
	}
	return freshnessToken(s.Revision, request), nil
}

// hasOption 报告选项是否在落账选项集内。
func (a PendingAsk) hasOption(id AskOptionID) bool {
	for _, option := range a.Options {
		if option.ID == id {
			return true
		}
	}
	return false
}

// expectedContains 报告任务是否在 expected 清单内。
func (s *State) expectedContains(key runtime.TaskKey) bool {
	for _, expected := range s.Expected {
		if expected == key {
			return true
		}
	}
	return false
}

// removeTaskBookkeeping 在任务到达 TERMINAL 时收回 expected 条目、
// pendingAction 与当前 Attempt；决策视图的 TERMINAL 状态保留。
func (s *State) removeTaskBookkeeping(key runtime.TaskKey) {
	kept := s.Expected[:0]
	for _, expected := range s.Expected {
		if expected != key {
			kept = append(kept, expected)
		}
	}
	s.Expected = kept
	if attempt, ok := s.Attempts[key]; ok {
		delete(s.Attempts, key)
		delete(s.PendingActions, attempt.ActionID)
	}
}

// prepareAdapterIntent owns the typed operation/schema checks and durable
// identity for both explicit HostActions and controller-required candidates.
func (e *Engine) prepareAdapterIntent(state *State, nextRevision uint64, operation authoring.OperationID, params any, correlation string, parents ...*State) (HostActionIntent, error) {
	schema, err := e.schemaForOperation(operation)
	if err != nil {
		return HostActionIntent{}, err
	}
	normalized, err := normalizeHostActionParams(params)
	if err != nil {
		return HostActionIntent{}, err
	}
	if err := validateAdapterPayload(schema, "adapter parameters", normalized); err != nil {
		return HostActionIntent{}, err
	}
	if nativeOperation(operation) {
		if state.requirementChangePending() && !(state.resetPending() && operation == "op.cleanup") {
			return HostActionIntent{}, validationError("native signing is frozen by requirement control")
		}
		switch operation {
		case "op.check":
			var p CheckParameters
			raw, _ := json.Marshal(normalized)
			if err := json.Unmarshal(raw, &p); err != nil {
				return HostActionIntent{}, err
			}
			expected, ready := state.nextCheckParameters(p.SourceActionID, e.cfg.Definition)
			if p.Repair != nil {
				expected, ready = state.nextRepairCheckParameters(p.Repair.Task, e.cfg.Definition)
			} else if p.QA != nil {
				expected, ready = state.nextQACheckParameters(p.Binding.Task, e.cfg.Definition)
			}
			if !ready || digestOfCanonical(expected) != digestOfCanonical(p) {
				return HostActionIntent{}, validationError("check parameters differ from approved execution inputs")
			}
			correlation = "check:" + digestOfCanonical(p)
		case "op.candidate":
			var p CandidateParameters
			raw, _ := json.Marshal(normalized)
			if err := json.Unmarshal(raw, &p); err != nil {
				return HostActionIntent{}, err
			}
			if p.Stage == "PROMOTION" {
				if err := state.validateCurrentPromotion(p, e.cfg.Definition); err != nil {
					return HostActionIntent{}, err
				}
			} else {
				expected, ready, err := state.currentCandidateParameters(e.cfg.Definition)
				if err != nil || !ready || digestOfCanonical(p) != digestOfCanonical(expected) {
					return HostActionIntent{}, validationError("candidate parameters differ from current effect inputs")
				}
			}
			correlation = candidateCorrelation(p)
		case "op.workspace":
			var p WorkspaceParameters
			raw, _ := json.Marshal(normalized)
			if err := json.Unmarshal(raw, &p); err != nil {
				return HostActionIntent{}, err
			}
			if !state.workspaceParametersCurrent(p, e.cfg.Definition) {
				return HostActionIntent{}, validationError("workspace parameters differ from current registered inputs")
			}
			r := p.Resource
			r.Identity, r.Status, r.MirrorDigest = "", "", ""
			mirror := ""
			if p.Manifest != nil {
				mirror = BlackboxCasesDigest(state.RunID, *p.Manifest)
			}
			correlation = "workspace:" + digestOfCanonical(struct {
				Resource NativeResource
				Mirror   string
			}{r, mirror})
		case "op.cleanup":
			var p CleanupParameters
			raw, _ := json.Marshal(normalized)
			if err := json.Unmarshal(raw, &p); err != nil {
				return HostActionIntent{}, err
			}
			if !state.cleanupAuthorized(p, e.cfg.Definition, parents...) || !state.cleanupParametersCurrent(p) {
				return HostActionIntent{}, validationError("cleanup parameters differ from its exact eligible resources")
			}
			targets := slices.Clone(p.Resources)
			for i := range targets {
				targets[i].Role, targets[i].Source, targets[i].Identity, targets[i].Status, targets[i].MirrorDigest = "", "", "", "", ""
			}
			slices.SortFunc(targets, func(a, b NativeResource) int { return strings.Compare(a.ID, b.ID) })
			correlation = "cleanup:" + digestOfCanonical(targets)
		}
		if err := state.admitNativeSigning(HostActionIntent{Correlation: correlation}); err != nil {
			return HostActionIntent{}, err
		}
	}
	step := state.nextHostActionStep(e.cfg.Definition, operation)
	if operation == "op.workspace" {
		step = childWorkspaceStep(params)
	}
	if step == "" && nativeOperation(operation) {
		for _, compiled := range e.cfg.Definition.Steps {
			if owner, ok := compiled.Payload.(compiler.CompiledHostActionStep); ok && owner.Operation == operation {
				step = compiled.Header.ID
				break
			}
		}
	}
	adapter := &AdapterHostAction{Operation: operation, Schema: schema, Params: normalized}
	action := fmt.Sprintf("hact:%s:%d", HostActionExecuteAdapterOperation, nextRevision)
	return HostActionIntent{ActionID: action, Operation: HostActionExecuteAdapterOperation, Step: step, Adapter: adapter, PayloadDigest: digestOfCanonical(adapter), Correlation: strings.TrimSpace(correlation), Revision: nextRevision}, nil
}
