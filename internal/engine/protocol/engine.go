package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/runtime"
)

// ackStore 是 IssueFromPlan 内部透传给 decision.SelectIssued 的落账回调：
// actionID 派生（"act:" + 规范键形态，阶段 1 语义）由 SelectIssued 持有，
// 真实持久化随本包的事务紧随其后原子完成。
type ackStore struct{}

func (ackStore) PersistIssued(decision.IssuedSet) error { return nil }

// Config 是 Engine 的定义绑定与容量输入：CompiledDefinition 供结果接纳的
// step 完成（CompleteStep）与补位签发（Decide/SelectIssued）使用；
// Registry 保留为编译期绑定的 runtime fixture 输入；HostAction 的 operation
// 与 schema 以 Definition 中的 compiled payload 为唯一运行时来源。Capacity
// 是补位签发的固定容量（真实容量收集器属后续批次）。
type Config struct {
	Definition *compiler.CompiledDefinition
	Registry   *compiler.Registry
	Capacity   int
}

// Engine 是提交协议内核：每个方法一次完整的「读 state → 变更 →
// persistence 四段协议提交」事务（CAS + 指纹重验由 persistence 承担）。
// 引擎不缓存内存态，一切以磁盘权威投影为准。
type Engine struct {
	store   *persistence.Store
	cfg     Config
	collect func() (string, error)
}

// New 构造 Engine。collect 是外部事实指纹收集函数（draft §3.3 写事务的
// 重验输入），nil 表示本引擎不依赖外部事实——空观察本身有确定指纹
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
	return snap.Revision, &state, nil
}

// Init 落盘初始权威状态（revision 1）。view 经 decision.NewState 构造
// （版本与 phase 已校验）；provider 是本 run 绑定的宿主 provider 身份，
// 非空必填——后续一切携带 provider 的事件/回执与之精确比对，不同即
// PROVIDER_MISMATCH 硬拒绝、不降级 default（draft §9.1）。已存在状态时
// 拒绝且零变化。
func (e *Engine) Init(view *decision.State, provider string, expectedFingerprint string) error {
	return e.InitWithMetadata(view, provider, expectedFingerprint, "", "", nil)
}

// InitWithMetadata is the façade bootstrap variant.  It preserves Init's
// original signature for protocol callers while persisting run identity and
// the typed pre-start intake confirmation in the same authoritative state
// write.
func (e *Engine) InitWithMetadata(view *decision.State, provider, expectedFingerprint, runID, route string, confirmation *IntakeConfirmationReceipt) error {
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
	state.IntakeConfirmationReceipt = confirmation
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
// 即 CAS 值；指纹写入前后各重验一次）。
func (e *Engine) commit(state *State, expectedRevision uint64, expectedFingerprint string) (uint64, error) {
	res, err := e.store.Save(persistence.Transaction{
		ExpectedRevision:    expectedRevision,
		ExpectedFingerprint: expectedFingerprint,
		CollectFingerprint:  e.collect,
		Content:             state,
	})
	if err != nil {
		return 0, err
	}
	return res.Revision, nil
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
	plan, err := decision.Decide(&state.State, decision.Observation{}, e.cfg.Definition)
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
		plan, err := decision.Decide(&state.State, decision.Observation{}, e.cfg.Definition)
		if err != nil {
			return nil, revision, err
		}
		switch plan.Next.Kind {
		case decision.KindWait:
			if plan.Next.Wait != nil && plan.Next.Wait.Reason == decision.WaitEngineInternal {
				if len(plan.Frontier) == 0 {
					return plan, revision, nil
				}
				for _, entry := range plan.Frontier {
					if err := state.CompleteStep(entry.Step, e.cfg.Definition); err != nil {
						return nil, revision, fmt.Errorf("protocol: drive complete %s: %w", entry.Step, err)
					}
				}
				if len(state.Completed) == len(e.cfg.Definition.Steps) && state.Phase != runtime.PhaseTerminal {
					if err := state.TransitionPhase(runtime.PhaseTerminal); err != nil {
						return nil, revision, err
					}
				}
				committed, err := e.commit(state, revision, expectedFingerprint)
				if err != nil {
					return nil, revision, err
				}
				_ = committed
				continue
			}
			return plan, revision, nil
		case decision.KindReady:
			issued, _, err := e.IssueFromPlan(plan, decision.Admission{Capacity: e.cfg.Capacity}, expectedFingerprint)
			if err != nil {
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

// IssueFromPlan 落账一次 Ready 签发（draft §3.3「不再次签发已经 ISSUED
// 的 SpawnRequest」的签发侧）：actionID 派生复用 decision.SelectIssued，
// 随后在同一事务里登记 expected 任务、当前 Attempt 与 pendingActions，
// 任务视图经 decision.State.TransitionTask 推进 QUEUED→ISSUED（复用
// runtime 任务状态机校验）。撞上已存在的 actionID 或任务已有当前
// Attempt 时 DUPLICATE_ACTION 拒绝且零变化。
func (e *Engine) IssueFromPlan(plan *decision.Plan, adm decision.Admission, expectedFingerprint string) (decision.IssuedSet, uint64, error) {
	revision, state, err := e.load()
	if err != nil {
		return nil, 0, err
	}
	if state == nil {
		return nil, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	identity, err := e.validatePlan(state, plan)
	if err != nil {
		return nil, 0, err
	}
	issued, err := decision.SelectIssued(plan, adm, ackStore{})
	if err != nil {
		return nil, 0, fmt.Errorf("protocol: issue: %w", err)
	}
	state.retainExpected(plan.Next.Ready)
	if err := state.issueInto(issued, revision+1, expectedFingerprint, state.RunProvider, identity, e.retryMaxAttempts); err != nil {
		return nil, 0, err
	}
	commitRevision, err := e.commit(state, revision, expectedFingerprint)
	if err != nil {
		return nil, 0, err
	}
	return issued, commitRevision, nil
}

// validatePlan rebuilds the canonical decision for the current authoritative
// state and compares the full plan identity before any task ledger mutation.
// Phase 2 has no external decision collectors yet, so the observation input is
// the same explicit empty Observation used by every current caller; the
// persistence fingerprint is bound separately to each Attempt as Snapshot.
func (e *Engine) validatePlan(state *State, plan *decision.Plan) (PlanIdentity, error) {
	if plan == nil {
		return PlanIdentity{}, fmt.Errorf("protocol: issue: nil plan")
	}
	want, err := decision.Decide(&state.State, decision.Observation{}, e.cfg.Definition)
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

// retryMaxAttempts returns the declaration attached to a compiled step. The
// protocol does not invent a retry bound when the step has no retry policy;
// this is important for the canonical review.worker fixture, whose transient
// result must remain resumable by the existing recovery contract.
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
		}
		return 0
	}
	return 0
}

// issueInto 把一次签发集落账进 state（IssueFromPlan 与结果接纳后的容量
// 补位共用）：重复 action/已有当前 Attempt/非法 QUEUED→ISSUED 转移一律
// 拒绝；Attempt ID 以落盘 revision 确定性派生。Expected 由调用方先登记
// 完整 eligible Ready frontier，因此这里只补齐缺失的已签发任务。
func (s *State) issueInto(issued decision.IssuedSet, nextRevision uint64, snapshot, responsibility string, plan PlanIdentity, retryMax func(authoring.StepID) int) error {
	for _, action := range issued {
		if _, exists := s.PendingActions[action.ActionID]; exists {
			return &RejectedError{Code: CodeDuplicateAction, Detail: fmt.Sprintf("action %q already issued", action.ActionID)}
		}
		if _, exists := s.Attempts[action.Task]; exists {
			return &RejectedError{Code: CodeDuplicateAction, Detail: fmt.Sprintf("task %s already has a current attempt", action.Task.String())}
		}
		if err := s.TransitionTask(action.Task, runtime.TaskIssued); err != nil {
			return &RejectedError{Code: CodeIllegalTransition, Detail: fmt.Sprintf("issue %s: %v", action.Task.String(), err)}
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
		s.PendingActions[action.ActionID] = PendingAction{
			ActionID: action.ActionID, Task: action.Task, Step: string(action.Step), AttemptID: attempt.ID,
		}
		if !s.expectedContains(action.Task) {
			s.Expected = append(s.Expected, action.Task)
		}
	}
	return nil
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

// Submit 是唯一的外部事件接纳入口（draft §3.4）。顺序固定：
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
	// 而被误拒（draft §3.4：同 ID 同 digest 返回稳定 acceptance/status）。
	if record, seen := state.Events[string(ev.ID)]; seen {
		if record.Digest != digest {
			return Acceptance{}, &RejectedError{
				Code:   CodeDuplicateEventMismatch,
				Detail: fmt.Sprintf("event %q replayed with different payload digest (recorded %s, submitted %s)", ev.ID, record.Digest, digest),
			}
		}
		return record.Acceptance, nil
	}
	nextRevision := revision + 1
	acceptance, mutated, wrote, reject := e.admit(ev, digest, state, revision, nextRevision, expectedFingerprint)
	if reject != nil {
		return Acceptance{}, reject
	}
	if !wrote {
		// Payload-level duplicate under a new event ID still consumes that ID.
		// Otherwise the same ID could later carry different bytes and bypass the
		// hard event identity invariant.
		acceptance.Revision = nextRevision
		state.Events[string(ev.ID)] = EventRecord{Digest: digest, Acceptance: acceptance}
		if _, err := e.commit(state, revision, expectedFingerprint); err != nil {
			return Acceptance{}, err
		}
		return acceptance, nil
	}
	state.Events[string(ev.ID)] = EventRecord{Digest: digest, Acceptance: acceptance}
	if _, err := e.commit(mutated, revision, expectedFingerprint); err != nil {
		return Acceptance{}, err
	}
	// CAS 语义保证提交恰好落在 nextRevision；acceptance.Revision 即它。
	return acceptance, nil
}

// admit 按事件 kind 做接纳校验并落账变更，返回接纳回执。nextRevision
// 是本事务将要提交到的 revision（CAS 保证其精确性），freshness token
// 与 Attempt ID 都以它派生。
func (e *Engine) admit(ev Event, digest string, state *State, revision, nextRevision uint64, expectedFingerprint string) (Acceptance, *State, bool, error) {
	switch ev.Kind {
	case KindRequestControl:
		// 两阶段主动控制第一段（draft §2.3）：受限 REQUEST_* 只创建
		// pending Ask——request ID（即事件 ID）、控制类型与选项集落账，
		// 不执行任何控制。同 requestID 的 Ask 已存在时拒绝（正常路径
		// 不可能：创建即入台账，重放已在台账层短路）。
		requestID := string(ev.ID)
		if _, exists := state.PendingAsks[requestID]; exists {
			return Acceptance{}, nil, false, &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("pending ask %q already exists", requestID)}
		}
		step := authoring.StepID("")
		if ev.Request.Control != ControlRecovery {
			step = state.nextHumanAskStep(e.cfg.Definition)
		}
		state.PendingAsks[requestID] = PendingAsk{
			RequestID: requestID, Control: ev.Request.Control, Step: step, Options: ev.Request.Options,
		}
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
		ask.Resolved = true
		state.PendingAsks[requestID] = ask
		state.Decisions[requestID] = RecordedDecision{
			RequestID: requestID, Control: ask.Control, Step: ask.Step, Choice: ev.Decide.Choice,
			EventID: string(ev.ID), Revision: nextRevision,
		}
		// Keep the request record after settlement so its request ID, step binding,
		// options, and resolved state remain independently inspectable. Decisions
		// remains the immutable decision history and also rejects later submissions.
		// 决定落账即完成对应 HUMAN_ASK frontier 步骤并补位签发（draft §2.2：
		// submit 接纳后立即继续 Decide/SelectIssued）。
		if err := state.settleFrontierSteps(e.cfg.Definition); err != nil {
			return Acceptance{}, nil, false, err
		}
		refill, refillErr := e.refill(state, nextRevision, expectedFingerprint)
		if refillErr != nil {
			return Acceptance{}, nil, false, refillErr
		}
		return Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision, RequestID: requestID, Refill: refill,
		}, state, true, nil

	case KindTaskProgress:
		// 任务进度接纳：任务必须在 expected 集（非当前节点可区分拒绝）、
		// attempt 必须是当前 Attempt（旧实例拒绝；对账路由属后续批次）、
		// 转移合法（复用 runtime 任务状态机）。TERMINAL 时收回 expected、
		// pendingAction 与当前 Attempt（重试开新 Attempt 是后续批次语义）。
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
			state.removeTaskBookkeeping(key)
		}
		return Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision,
		}, state, true, nil

	case KindSpawnReceipt:
		// SpawnReceipt 接纳（draft §9.1）：provider 与 run 绑定精确比对；
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
		if _, pending := state.PendingActions[actionID]; !pending {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeUnknownAction,
				Detail: fmt.Sprintf("spawn receipt references unknown or completed action %q", actionID),
			}
		}
		receipt := SpawnReceipt{
			ActionID: actionID, Provider: ev.Spawn.Provider,
			Correlation: ev.Spawn.Correlation, Status: ev.Spawn.Status,
			FailureClass: ev.Spawn.FailureClass, Digest: payloadDigest,
		}
		acceptance := Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision, ActionID: actionID,
		}
		if ev.Spawn.Status == SpawnStatusFailed {
			state.SpawnFailures[actionID] = append(state.SpawnFailures[actionID], receipt)
			route := RouteFailure(ev.Spawn.FailureClass)
			pending := state.PendingActions[actionID]
			if route.Class == authoring.FailureTransientEngine {
				attempt := state.Attempts[pending.Task]
				if attempt.MaxAttempts > 0 {
					if !attempt.RetryExhausted {
						attempt.Attempts++
						if attempt.Attempts >= attempt.MaxAttempts {
							attempt.RetryExhausted = true
						}
					}
					state.Attempts[pending.Task] = attempt
					if attempt.RetryExhausted {
						route.Action = RecoveryWait
						routeDetail := fmt.Sprintf("transient retry attempts exhausted (%d/%d); wait for terminal handling", attempt.Attempts, attempt.MaxAttempts)
						state.appendRecovery(RecoveryRecord{ActionID: actionID, Task: pending.Task, AttemptID: pending.AttemptID, Class: route.Class, Action: route.Action, Detail: routeDetail, Revision: nextRevision})
						delete(state.StagedResults, actionID)
						acceptance.FailureClass = string(route.Class)
						acceptance.RecoveryAction = string(route.Action)
						return acceptance, state, true, nil
					}
				}
			}
			// A FAILED spawn cannot settle a result that arrived before the
			// receipt. The result belongs to the failed dispatch boundary and
			// must be submitted again after a later retry.
			delete(state.StagedResults, actionID)
			state.appendRecovery(RecoveryRecord{
				ActionID: actionID, Task: pending.Task, AttemptID: pending.AttemptID,
				Class: route.Class, Action: route.Action, Detail: route.ActionDetail(), Revision: nextRevision,
			})
			if route.Action == RecoveryFail {
				if err := state.terminalizeTask(pending.Task); err != nil {
					return Acceptance{}, nil, false, err
				}
			}
			if route.Action == RecoveryReconcile {
				// Keep the wire-level FAILED receipt in SpawnFailures, while
				// exposing an UNKNOWN ledger entry for external reconciliation.
				unknown := receipt
				unknown.Status = SpawnStatusUnknown
				state.SpawnReceipts[actionID] = unknown
			}
			acceptance.FailureClass = string(route.Class)
			acceptance.RecoveryAction = string(route.Action)
			return acceptance, state, true, nil
		}
		state.SpawnReceipts[actionID] = receipt
		state.verifyLifecycleCorrelation(ev.Spawn.Correlation, nextRevision)
		if ev.Spawn.Status == SpawnStatusUnknown {
			plan := DecideRecovery(Interruption{ReceiptUnknown: true, LifecycleMatches: len(state.lifecycleMatches(ev.Spawn.Correlation))})
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: actionID, Class: plan.Class, Action: plan.Action,
				Detail: plan.Detail, LifecycleMatches: plan.LifecycleMatches, Revision: nextRevision,
			})
			acceptance.RecoveryAction = string(plan.Action)
		}
		if staged, waiting := state.StagedResults[actionID]; waiting && ev.Spawn.Status == SpawnStatusSpawned {
			refill, err := e.completeResult(state, staged, nextRevision, expectedFingerprint)
			if err != nil {
				return Acceptance{}, nil, false, err
			}
			delete(state.StagedResults, actionID)
			acceptance.Refill = refill
			class := staged.FailureClass
			if class != "" {
				acceptance.FailureClass = string(class)
				acceptance.RecoveryAction = string(RouteFailure(class).Action)
			}
		}
		return acceptance, state, true, nil

	case KindWorkerResult:
		// typed result 接纳：provider 精确比对；已有同 actionID 结果按
		// payload digest 判幂等/冲突。receipt 未到时暂存（不丢弃、不推进，
		// draft §3.4 result-before-receipt）；receipt 已在则完成该 expected
		// 项：任务沿合法边推进 TERMINAL、PASS 完成 step（frontier 推进）
		// 并按容量补位签发。
		if err := state.checkProvider(ev.Result.Provider); err != nil {
			return Acceptance{}, nil, false, err
		}
		actionID := ev.Result.ActionID
		payloadDigest := digestOfCanonical(workerResultEventWire{
			ActionID: ev.Result.ActionID, Provider: ev.Result.Provider,
			Outcome: ev.Result.Outcome, PayloadDigest: ev.Result.PayloadDigest,
			FailureClass: string(ev.Result.FailureClass),
		})
		if existing, seen := state.Results[actionID]; seen {
			if existing.Digest != payloadDigest {
				return Acceptance{}, nil, false, &RejectedError{
					Code:   CodeReceiptConflict,
					Detail: fmt.Sprintf("action %q worker result replayed with different bytes (recorded %s, submitted %s)", actionID, existing.Digest, payloadDigest),
				}
			}
			return Acceptance{
				EventID: string(ev.ID), Kind: string(ev.Kind), Status: "DUPLICATE",
				Revision: revision, ActionID: actionID,
			}, nil, false, nil
		}
		if obsolete, known := state.ObsoleteActions[actionID]; known {
			if existing, seen := state.ObsoleteResults[actionID]; seen {
				if existing.Digest != payloadDigest {
					return Acceptance{}, nil, false, &RejectedError{Code: CodeReceiptConflict, Detail: fmt.Sprintf("obsolete action %q worker result replayed with different bytes", actionID)}
				}
				return Acceptance{EventID: string(ev.ID), Kind: string(ev.Kind), Status: "OBSOLETE_RESULT", Revision: revision, ActionID: actionID}, nil, false, nil
			}
			state.ObsoleteResults[actionID] = WorkerResult{ActionID: actionID, Provider: ev.Result.Provider, Outcome: ev.Result.Outcome, PayloadDigest: ev.Result.PayloadDigest, FailureClass: ev.Result.FailureClass, Digest: payloadDigest}
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{ActionID: actionID, Task: obsolete.Task, AttemptID: obsolete.AttemptID, Class: ev.Result.FailureClass, Action: RecoveryOperator, Detail: "late result belongs to an obsolete Attempt", Revision: nextRevision})
			return Acceptance{EventID: string(ev.ID), Kind: string(ev.Kind), Status: "OBSOLETE_RESULT", Revision: nextRevision, ActionID: actionID}, state, true, nil
		}
		for _, recoverable := range state.RecoverableResults[actionID] {
			if recoverable.Digest == payloadDigest {
				return Acceptance{
					EventID: string(ev.ID), Kind: string(ev.Kind), Status: "DUPLICATE",
					Revision: revision, ActionID: actionID,
				}, nil, false, nil
			}
		}
		if _, pending := state.PendingActions[actionID]; !pending {
			return Acceptance{}, nil, false, &RejectedError{
				Code:   CodeUnknownAction,
				Detail: fmt.Sprintf("worker result references unknown or completed action %q", actionID),
			}
		}
		record := WorkerResult{
			ActionID: actionID, Provider: ev.Result.Provider,
			Outcome: ev.Result.Outcome, PayloadDigest: ev.Result.PayloadDigest,
			FailureClass: ev.Result.FailureClass, Digest: payloadDigest,
		}
		if receipt, receipted := state.SpawnReceipts[actionID]; !receipted || receipt.Status == SpawnStatusUnknown {
			if staged, exists := state.StagedResults[actionID]; exists {
				if staged.Digest == payloadDigest {
					return Acceptance{
						EventID: string(ev.ID), Kind: string(ev.Kind), Status: "DUPLICATE",
						Revision: revision, ActionID: actionID,
					}, nil, false, nil
				}
				return Acceptance{}, nil, false, &RejectedError{
					Code: CodeReceiptConflict, Detail: fmt.Sprintf("action %q already has a different staged worker result", actionID),
				}
			}
			state.StagedResults[actionID] = record
			return Acceptance{
				EventID: string(ev.ID), Kind: string(ev.Kind), Status: "STAGED",
				Revision: nextRevision, ActionID: actionID, RecoveryAction: func() string {
					if receipted {
						return string(RecoveryReconcile)
					}
					return ""
				}(),
			}, state, true, nil
		}
		refill, err := e.completeResult(state, record, nextRevision, expectedFingerprint)
		if err != nil {
			return Acceptance{}, nil, false, err
		}
		class := record.FailureClass
		if class != "" {
			recoveryAction := RouteFailure(class).Action
			if len(state.RecoveryRecords) > 0 {
				last := state.RecoveryRecords[len(state.RecoveryRecords)-1]
				if last.ActionID == actionID && last.Revision == nextRevision {
					recoveryAction = last.Action
				}
			}
			return Acceptance{
				EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
				Revision: nextRevision, ActionID: actionID, Refill: refill,
				FailureClass: string(class), RecoveryAction: string(recoveryAction),
			}, state, true, nil
		}
		return Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision, ActionID: actionID, Refill: refill,
		}, state, true, nil

	case KindOperatorObservation:
		// Operator typed observation 入账（draft §2.2：主代理核实事实、
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
		// HostAction 回执接纳（draft §9.1）：provider 精确比对；已清账的
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
		if existing, seen := state.HostActionReceipts[actionID]; seen {
			if existing.Digest != payloadDigest {
				return Acceptance{}, nil, false, &RejectedError{
					Code:   CodeReceiptConflict,
					Detail: fmt.Sprintf("action %q host action receipt replayed with different bytes (recorded %s, submitted %s)", actionID, existing.Digest, payloadDigest),
				}
			}
			return Acceptance{
				EventID: string(ev.ID), Kind: string(ev.Kind), Status: "DUPLICATE",
				Revision: revision, ActionID: actionID,
			}, nil, false, nil
		}
		intent, pending := state.PendingHostActions[actionID]
		if !pending {
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
			route := RouteFailure(ev.HostAction.FailureClass)
			state.appendRecovery(RecoveryRecord{
				ActionID: actionID, Class: route.Class, Action: route.Action,
				Detail: route.ActionDetail(), Revision: nextRevision,
			})
			if route.Action == RecoveryReconcile {
				// Keep the wire-level FAILED receipt in HostActionFailures while
				// exposing an UNKNOWN ledger entry, mirroring the spawn-side
				// bridge: without it the declared RECOVER route has no
				// reconciliation entry (ReconcileHostAction requires a durable
				// UNKNOWN receipt) and the recovery instruction dead-ends.
				unknown := receipt
				unknown.Status = HostActionStatusUnknown
				state.HostActionReceipts[actionID] = unknown
			}
			if route.Action == RecoveryFail {
				delete(state.PendingHostActions, actionID)
			}
			return Acceptance{
				EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED", Revision: nextRevision,
				ActionID: actionID, FailureClass: string(route.Class), RecoveryAction: string(route.Action),
			}, state, true, nil
		}
		state.HostActionReceipts[actionID] = receipt
		if ev.HostAction.Status == HostActionStatusUnknown {
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: actionID, Class: authoring.FailureSideEffectUnknown,
				Action: RecoveryReconcile, Detail: "host action receipt is UNKNOWN; reconcile external facts before retrying",
				Revision: nextRevision,
			})
			return Acceptance{EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED", Revision: nextRevision, ActionID: actionID, RecoveryAction: string(RecoveryReconcile)}, state, true, nil
		}
		delete(state.PendingHostActions, actionID)
		// EXECUTED 回执清账即完成对应 HOST_ACTION frontier 步骤并补位签发
		//（draft §2.2：submit 接纳后立即继续 Decide/SelectIssued）。
		if err := state.settleFrontierSteps(e.cfg.Definition); err != nil {
			return Acceptance{}, nil, false, err
		}
		refill, refillErr := e.refill(state, nextRevision, expectedFingerprint)
		if refillErr != nil {
			return Acceptance{}, nil, false, refillErr
		}
		return Acceptance{
			EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED",
			Revision: nextRevision, ActionID: actionID, Refill: refill,
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
func (e *Engine) completeResult(state *State, record WorkerResult, nextRevision uint64, expectedFingerprint string) ([]string, error) {
	pending := state.PendingActions[record.ActionID]
	key := pending.Task
	class := record.FailureClass
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
			Class: class, Action: plan.Action, Detail: plan.Detail, Revision: nextRevision,
		})
		if plan.Action != RecoveryFail {
			state.RecoverableResults[record.ActionID] = append(state.RecoverableResults[record.ActionID], record)
			return nil, nil
		}
	}
	if err := state.terminalizeTask(key); err != nil {
		return nil, err
	}
	state.Results[record.ActionID] = record
	if record.Outcome != OutcomePass {
		return nil, nil
	}
	if err := state.CompleteStep(authoring.StepID(pending.Step), e.cfg.Definition); err != nil {
		return nil, fmt.Errorf("protocol: complete step %s: %w", pending.Step, err)
	}
	// PASS 结果完成步骤后，其外部边界事实（已落账的 Ask 决定 / EXECUTED
	// 回执）可能恰好满足其他 frontier 步骤——补一次 settle。
	if err := state.settleFrontierSteps(e.cfg.Definition); err != nil {
		return nil, err
	}
	return e.refill(state, nextRevision, expectedFingerprint)
}

// terminalizeTask advances a current task along every missing legal progress
// edge before removing its Attempt bookkeeping. Result and receipt admission
// share it so terminal failure cannot leave a permanently pending action.
func (s *State) terminalizeTask(key runtime.TaskKey) error {
	progress := []runtime.TaskStatus{runtime.TaskIssued, runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal}
	start := 0
	for i, status := range progress {
		if s.TaskStatusOf(key) == status {
			start = i
			break
		}
	}
	for i := start + 1; i < len(progress); i++ {
		if err := s.TransitionTask(key, progress[i]); err != nil {
			return &RejectedError{Code: CodeIllegalTransition, Detail: fmt.Sprintf("complete %s: %v", key.String(), err)}
		}
	}
	s.removeTaskBookkeeping(key)
	return nil
}

// refill 按 draft §2.2「submit 接纳事件后立即继续 Decide/SelectIssued」：
// 对推进后的视图重新决策，Ready 计划经 SelectIssued 按容量裁剪后落账
// （复用与 IssueFromPlan 完全相同的签发语义）。
func (e *Engine) refill(state *State, nextRevision uint64, expectedFingerprint string) ([]string, error) {
	plan, err := decision.Decide(&state.State, decision.Observation{}, e.cfg.Definition)
	if err != nil {
		return nil, fmt.Errorf("protocol: refill decide: %w", err)
	}
	if plan.Next.Kind != decision.KindReady || plan.Next.Ready == nil {
		return nil, nil
	}
	issued, err := decision.SelectIssued(plan, decision.Admission{Capacity: e.cfg.Capacity}, ackStore{})
	if err != nil {
		return nil, fmt.Errorf("protocol: refill select: %w", err)
	}
	if len(issued) == 0 {
		return nil, nil
	}
	state.retainExpected(plan.Next.Ready)
	identity, err := identityOfPlan(plan)
	if err != nil {
		return nil, fmt.Errorf("protocol: refill plan identity: %w", err)
	}
	if err := state.issueInto(issued, nextRevision, expectedFingerprint, state.RunProvider, identity, e.retryMaxAttempts); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(issued))
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
// known transport correlation into the durable intent. The legacy convenience
// method keeps the field empty when the host chooses the correlation only after
// execution.
func (e *Engine) ExecuteHostActionWithCorrelation(operation authoring.OperationID, params any, correlation, expectedFingerprint string) (HostActionIntent, uint64, error) {
	return e.executeHostAction(operation, params, correlation, expectedFingerprint)
}

func (e *Engine) executeHostAction(operation authoring.OperationID, params any, correlation, expectedFingerprint string) (HostActionIntent, uint64, error) {
	if operation == "" {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeFreeCommandForm, Detail: "host action requires an operation reference; free command forms do not exist"}
	}
	schema, err := e.schemaForOperation(operation)
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	normalized, err := normalizeHostActionParams(params)
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	if err := validateAdapterPayload(schema, "adapter parameters", normalized); err != nil {
		return HostActionIntent{}, 0, err
	}
	revision, state, err := e.load()
	if err != nil {
		return HostActionIntent{}, 0, err
	}
	if state == nil {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	nextRevision := revision + 1
	adapter := &AdapterHostAction{Operation: operation, Schema: schema, Params: normalized}
	intent := HostActionIntent{
		ActionID:      "hact:" + string(HostActionExecuteAdapterOperation) + ":" + strconv.FormatUint(nextRevision, 10),
		Operation:     HostActionExecuteAdapterOperation,
		Step:          state.nextHostActionStep(e.cfg.Definition, operation),
		Adapter:       adapter,
		PayloadDigest: digestOfCanonical(adapter),
		Correlation:   strings.TrimSpace(correlation),
		Revision:      nextRevision,
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

// TerminateAgentWithCorrelation binds a known lifecycle transport correlation
// for receipt-file fixtures and adapters that establish it before execution.
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
	pending, ok := state.PendingActions[workerActionID]
	if !ok {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeUnknownAction, Detail: fmt.Sprintf("agent host action references unknown worker action %q", workerActionID)}
	}
	attempt, ok := state.Attempts[pending.Task]
	if !ok || attempt.ID != pending.AttemptID {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeStaleAttempt, Detail: fmt.Sprintf("worker action %q has no matching current Attempt", workerActionID)}
	}
	receipt, ok := state.SpawnReceipts[workerActionID]
	if !ok || receipt.Correlation != identity {
		return HostActionIntent{}, 0, &RejectedError{Code: CodeIntentMismatch, Detail: fmt.Sprintf("agent identity %q is not bound to worker action %q", identity, workerActionID)}
	}
	payload := &AgentHostAction{WorkerActionID: workerActionID, AttemptID: attempt.ID, Identity: identity, Reason: reason}
	nextRevision := revision + 1
	intent := HostActionIntent{
		ActionID:      "hact:" + string(operation) + ":" + strconv.FormatUint(nextRevision, 10),
		Operation:     operation,
		PayloadDigest: digestOfCanonical(payload),
		Correlation:   strings.TrimSpace(correlation),
		Revision:      nextRevision,
	}
	if operation == HostActionResumeAgent {
		intent.Resume = payload
	} else {
		intent.Terminate = payload
	}
	return e.persistHostAction(state, revision, intent, expectedFingerprint)
}

func (e *Engine) persistHostAction(state *State, revision uint64, intent HostActionIntent, expectedFingerprint string) (HostActionIntent, uint64, error) {
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
	switch typed := params.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return typed, nil
	default:
		return nil, &RejectedError{
			Code:   CodeFreeCommandForm,
			Detail: fmt.Sprintf("host action params must be a structured object, got %T (free command forms do not exist)", params),
		}
	}
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

// The canonical definition names the concrete adapter contract. These Go
// structs are the contract implementation; there is deliberately no runtime
// map/reflection schema interpreter.
type fanTransportParams struct {
	Target  string   `json:"target,omitempty"`
	Retries *float64 `json:"retries,omitempty"`
}

type fanTransportEvidence struct {
	Identity          string `json:"identity,omitempty"`
	ObservationDigest string `json:"observationDigest,omitempty"`
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
	case authoring.SchemaID("schema.host.fan.transport"):
		if label == "adapter parameters" {
			decodeErr = decode(&fanTransportParams{})
		} else {
			decodeErr = decode(&fanTransportEvidence{})
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
// 不降级 default（draft §9.1）。
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

// Freshness 返回 request 当前 revision 下的 freshness token（draft §2.3
// 「next/status 暴露带 freshness token 的 availableActions」的协议层种子；
// 公开 status 面属阶段 3）。request 未知或已决时拒绝。
func (e *Engine) Freshness(request RequestID) (string, error) {
	revision, state, err := e.load()
	if err != nil {
		return "", err
	}
	if state == nil {
		return "", &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	ask, exists := state.PendingAsks[string(request)]
	if !exists {
		if _, decided := state.Decisions[string(request)]; decided {
			return "", &RejectedError{Code: CodeRequestResolved, Detail: fmt.Sprintf("request %q already decided", request)}
		}
		return "", &RejectedError{Code: CodeUnknownRequest, Detail: fmt.Sprintf("request %q does not exist", request)}
	}
	if ask.Resolved {
		return "", &RejectedError{Code: CodeRequestResolved, Detail: fmt.Sprintf("request %q already decided", request)}
	}
	return freshnessToken(revision, request), nil
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
