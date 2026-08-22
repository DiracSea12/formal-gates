package decision

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/runtime"
)

// Kind 是 NextResult 的六值封闭 Kind（master-requirements §5.11）：同一
// canonical Plan 的 Kind 唯一，对应 payload 之外的 payload 必须为空。
type Kind string

const (
	// KindReady：向宿主整批搬运的 agent 派发集（含 spawn transport；
	// spawn 属于 Ready，不进 HostAction）。主代理不得挑子集。
	KindReady Kind = "READY"
	// KindHostAction：宿主 adapter 程序化动作（RESUME_AGENT |
	// TERMINATE_AGENT | EXECUTE_ADAPTER_OPERATION 的封闭 typed union）。
	KindHostAction Kind = "HOST_ACTION"
	// KindAsk：用户拥有的决定（AskRequest 只承载用户决定）。
	KindAsk Kind = "ASK"
	// KindWait：无外部动作可推进（容量为零属 admission 层；本枚举只覆盖
	// Decide 可见的原因）。Ask 不得伪装成 Wait。
	KindWait Kind = "WAIT"
	// KindOperator：要求主代理核实证据/分类影响的语义判断（不替用户授权、
	// 不执行 adapter 动作）。本批 Decide 不产出 Operator：其触发源是
	// receipt UNKNOWN 多重匹配等对账事实，属后续可靠写入批次。
	KindOperator Kind = "OPERATOR"
	// KindComplete：run 结束，无后续外部动作。
	KindComplete Kind = "COMPLETE"
)

// Valid 报告 k 是否属于六值封闭集合。
func (k Kind) Valid() bool {
	switch k {
	case KindReady, KindHostAction, KindAsk, KindWait, KindOperator, KindComplete:
		return true
	}
	return false
}

// WaitReason 是 Wait 的封闭原因枚举（§6.4.2 的 Decide 可见子集）。
type WaitReason string

const (
	// WaitEngineInternal：eligible frontier 只剩 engine-internal 步骤
	// （LOCAL/DURABLE/PARALLEL）——由 Controller 机械执行，不产生外部
	// 边界；纯决策内核把它投影为带原因的 Wait，而非空结果。
	WaitEngineInternal WaitReason = "ENGINE_INTERNAL"
	// WaitTasksInFlight：frontier 含外部边界步骤，但全部已签发在途（或
	// 已终态待处置），无可签发项；receipt/result 到达后 submit 立即补位。
	WaitTasksInFlight WaitReason = "TASKS_IN_FLIGHT"
)

// ReadyTask 是 Ready 集的单个待签发任务：薄指针（TaskKey + step），不携
// 输入数据与 actionID——actionID 由 SelectIssued 分配（§5.12）。
type ReadyTask struct {
	Task runtime.TaskKey
	Step authoring.StepID
}

// ReadyPayload 是 Ready 的 payload：完整可签发任务集（固定顺序）。
type ReadyPayload struct {
	Tasks []ReadyTask
}

// HostActionPayload 是 HostAction 的 payload：待宿主执行的步骤引用。
type HostActionPayload struct {
	Steps []authoring.StepID
}

// AskPayload 是 Ask 的 payload：待用户决定的步骤引用（schema 在定义中）。
type AskPayload struct {
	Steps []authoring.StepID
}

// WaitPayload 是 Wait 的 payload：封闭原因。
type WaitPayload struct {
	Reason WaitReason
}

// OperatorPayload 是 Operator 的 payload：待核实的事实集合。
type OperatorPayload struct {
	Facts []Fact
}

// CompletePayload 是 Complete 的 payload：无附加数据（终态摘要属阶段 2
// 的 state envelope）。
type CompletePayload struct{}

// NextResult 是六类外部边界的 tagged union。除本 Kind 对应的一个 payload
// 外，其余必须为 nil（Validate 强制）。
type NextResult struct {
	Kind       Kind
	Ready      *ReadyPayload
	HostAction *HostActionPayload
	Ask        *AskPayload
	Wait       *WaitPayload
	Operator   *OperatorPayload
	Complete   *CompletePayload
}

// Validate 校验 tagged union 不变量：Kind 属封闭集合，且恰好其对应
// payload 非 nil、其余全 nil。
func (n NextResult) Validate() error {
	if !n.Kind.Valid() {
		return fmt.Errorf("decision: next result kind %q invalid", n.Kind)
	}
	slots := []struct {
		kind     Kind
		set      bool
		mismatch string
	}{
		{KindReady, n.Ready != nil, "ready"},
		{KindHostAction, n.HostAction != nil, "hostAction"},
		{KindAsk, n.Ask != nil, "ask"},
		{KindWait, n.Wait != nil, "wait"},
		{KindOperator, n.Operator != nil, "operator"},
		{KindComplete, n.Complete != nil, "complete"},
	}
	for _, s := range slots {
		if s.set != (s.kind == n.Kind) {
			return fmt.Errorf("decision: next result kind %s with payload %s presence = %v (payloads outside the kind must be empty)", n.Kind, s.mismatch, s.set)
		}
	}
	return nil
}

// FrontierEntry 是 eligible frontier 的单个条目：依赖已满足且未完成的
// 步骤，按 compiled definition 的稳定 ordinal 排序（ordinal 由 compiler
// 以确定性 Kahn 拓扑序派生，全局唯一，与 assembly 顺序无关）。
type FrontierEntry struct {
	Step    authoring.StepID
	Node    authoring.NodeID
	Ordinal int
	Kind    compiler.StepKind
}

// Plan 是 Decide 的产物：三类输入 digest 绑定 + 完整 eligible frontier +
// 唯一 Kind 的 NextResult。Plan 不含随机 actionID、当前时间或 map 遍历
// 序——相同 state+observation+definition 恒得相同 canonical 字节，
// PlanDigest 即该字节的 SHA-256（§5.9：PlanDigest 绑定给定
// state+observation 的决策结果）。
type Plan struct {
	DefinitionDigest  string
	StateDigest       string
	ObservationDigest string
	Frontier          []FrontierEntry
	Next              NextResult
}

// Decide 是纯函数决策（master-requirements §5.1/§5.11）：
//   - 版本绑定最后防线：state 的 definition 版本必须与定义精确一致；
//   - 输入一致性：completed 集合必须是定义内步骤、无重复、依赖闭包
//     完整（乱序/遗漏/重复的完成在 State.CompleteStep 拦截，此处对
//     手工构造的 state 复核）；
//   - frontier = 依赖已满足且未完成的全部步骤（完整性），按 ordinal
//     固定排序；
//   - Kind 判定优先级固定：Complete > Ready > HostAction > Ask > Wait。
//     agent 工作最慢，先派发以最大化并行；宿主机械动作次之；人工决定
//     随后（submit 接纳后立即重入 Decide，后续边界很快到达）。
//
// 决定性错误（版本不符、状态不一致、定义携带 MISSING_ENGINE_ADAPTER
// marker 等）返回 error，绝不产出静默空结果。
func Decide(state *State, obs Observation, cd *compiler.CompiledDefinition) (*Plan, error) {
	if state == nil {
		return nil, fmt.Errorf("decision: decide: nil state")
	}
	if cd == nil {
		return nil, fmt.Errorf("decision: decide: nil definition")
	}
	if state.DefinitionVersion != cd.Version {
		return nil, fmt.Errorf("decision: decide: state definition version %q != definition %q", state.DefinitionVersion, cd.Version)
	}
	// definition digest 经 canonical 编码得出：携带 marker 的
	// diagnostic-only 定义在此被拒（不得进入 executable plan）。
	defBytes, err := encoder.Encode(cd)
	if err != nil {
		return nil, fmt.Errorf("decision: decide: %w", err)
	}
	defDigest := encoder.Digest(defBytes)

	completedSet := make(map[authoring.StepID]bool, len(state.Completed))
	for _, id := range state.Completed {
		if completedSet[id] {
			return nil, fmt.Errorf("decision: decide: state completed step %q duplicated", id)
		}
		completedSet[id] = true
	}
	index := make(map[authoring.StepID]*compiler.CompiledStep, len(cd.Steps))
	for i := range cd.Steps {
		index[cd.Steps[i].Header.ID] = &cd.Steps[i]
	}
	for _, id := range state.Completed {
		cs, ok := index[id]
		if !ok {
			return nil, fmt.Errorf("decision: decide: state completed step %q not in definition", id)
		}
		for _, dep := range cs.Header.Dependencies {
			if !completedSet[dep] {
				return nil, fmt.Errorf("decision: decide: state completed step %q before dependency %q (out-of-order or skipped)", id, dep)
			}
		}
	}

	var frontier []*compiler.CompiledStep
	for i := range cd.Steps {
		cs := &cd.Steps[i]
		if completedSet[cs.Header.ID] {
			continue
		}
		ready := true
		for _, dep := range cs.Header.Dependencies {
			if !completedSet[dep] {
				ready = false
				break
			}
		}
		if ready {
			frontier = append(frontier, cs)
		}
	}
	// 稳定全序：compiled ordinal（compiler 确定性 Kahn 序，全局唯一）。
	sortByOrdinal(frontier)

	// 按 NextResult 边界分类：AGENT→Ready、HOST_ACTION→HostAction、
	// HUMAN_ASK→Ask 为外部边界；LOCAL/DURABLE/PARALLEL 由 Controller
	// 内部执行。已签发在途（或已终态待处置）的任务不再进入可签发集。
	externalPresent := false
	var agentTasks []ReadyTask
	var hostSteps, askSteps []authoring.StepID
	for _, cs := range frontier {
		// 稳定键构造走 NewTaskKey 校验：绕过构造层的非法段字符（"/" 或
		// "\"）在此决定性失败，而非静默坍缩进 canonical 键形态。
		key, err := runtime.NewTaskKey(cs.Header.NodeID, cs.Header.ID, "")
		if err != nil {
			return nil, fmt.Errorf("decision: decide: %w", err)
		}
		queued := state.TaskStatusOf(key) == runtime.TaskQueued
		switch cs.Header.Kind {
		case compiler.KindAgent:
			externalPresent = true
			if queued {
				agentTasks = append(agentTasks, ReadyTask{Task: key, Step: cs.Header.ID})
			}
		case compiler.KindHostAction:
			externalPresent = true
			if queued {
				hostSteps = append(hostSteps, cs.Header.ID)
			}
		case compiler.KindHumanAsk:
			externalPresent = true
			if queued {
				askSteps = append(askSteps, cs.Header.ID)
			}
		}
	}

	var next NextResult
	switch {
	case state.Phase == runtime.PhaseTerminal || len(state.Completed) == len(cd.Steps):
		next = NextResult{Kind: KindComplete, Complete: &CompletePayload{}}
	case len(agentTasks) > 0:
		next = NextResult{Kind: KindReady, Ready: &ReadyPayload{Tasks: agentTasks}}
	case len(hostSteps) > 0:
		next = NextResult{Kind: KindHostAction, HostAction: &HostActionPayload{Steps: hostSteps}}
	case len(askSteps) > 0:
		next = NextResult{Kind: KindAsk, Ask: &AskPayload{Steps: askSteps}}
	case externalPresent:
		next = NextResult{Kind: KindWait, Wait: &WaitPayload{Reason: WaitTasksInFlight}}
	default:
		next = NextResult{Kind: KindWait, Wait: &WaitPayload{Reason: WaitEngineInternal}}
	}
	if err := next.Validate(); err != nil {
		return nil, err
	}

	stateDigest, err := state.Digest()
	if err != nil {
		return nil, fmt.Errorf("decision: decide: %w", err)
	}
	obsDigest, err := obs.Digest()
	if err != nil {
		return nil, fmt.Errorf("decision: decide: %w", err)
	}
	entries := make([]FrontierEntry, 0, len(frontier))
	for _, cs := range frontier {
		entries = append(entries, FrontierEntry{
			Step: cs.Header.ID, Node: cs.Header.NodeID, Ordinal: cs.Header.Ordinal, Kind: cs.Header.Kind,
		})
	}
	return &Plan{
		DefinitionDigest:  defDigest,
		StateDigest:       stateDigest,
		ObservationDigest: obsDigest,
		Frontier:          entries,
		Next:              next,
	}, nil
}

// sortByOrdinal 按 compiled ordinal 升序排序 frontier 指针切片
// （ordinal 全局唯一，排序结果为稳定全序）。
func sortByOrdinal(steps []*compiler.CompiledStep) {
	sort.Slice(steps, func(i, j int) bool { return steps[i].Header.Ordinal < steps[j].Header.Ordinal })
}

// ---- canonical 编码（形态与 encoder 制品一致） ----

// canonicalJSON 是本包统一的 canonical 编码：JSON、2 空格缩进、不转义
// HTML、恰一个尾随换行。wire 结构只含 string/int 与有序切片，字节输出
// 只是数据的函数。
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("decision: canonical encode: %w", err)
	}
	return buf.Bytes(), nil
}

type planWire struct {
	DefinitionDigest  string         `json:"definitionDigest"`
	StateDigest       string         `json:"stateDigest"`
	ObservationDigest string         `json:"observationDigest"`
	Frontier          []frontierWire `json:"frontier"`
	Next              nextWire       `json:"next"`
}

type frontierWire struct {
	Step    string `json:"step"`
	Node    string `json:"node"`
	Ordinal int    `json:"ordinal"`
	Kind    string `json:"kind"`
}

type nextWire struct {
	Kind       string          `json:"kind"`
	Ready      *readyWire      `json:"ready,omitempty"`
	HostAction *hostActionWire `json:"hostAction,omitempty"`
	Ask        *askWire        `json:"ask,omitempty"`
	Wait       *waitWire       `json:"wait,omitempty"`
	Operator   *operatorWire   `json:"operator,omitempty"`
	Complete   *completeWire   `json:"complete,omitempty"`
}

type readyWire struct {
	Tasks []readyTaskWire `json:"tasks"`
}

type readyTaskWire struct {
	Task string `json:"task"`
	Step string `json:"step"`
}

type hostActionWire struct {
	Steps []string `json:"steps"`
}

type askWire struct {
	Steps []string `json:"steps"`
}

type waitWire struct {
	Reason string `json:"reason"`
}

type operatorWire struct {
	Facts []factWire `json:"facts"`
}

type completeWire struct{}

// CanonicalBytes 返回 Plan 的 canonical 字节（空集编码为 []，不用 null）。
func (p *Plan) CanonicalBytes() ([]byte, error) {
	if err := p.Next.Validate(); err != nil {
		return nil, err
	}
	w := planWire{
		DefinitionDigest:  p.DefinitionDigest,
		StateDigest:       p.StateDigest,
		ObservationDigest: p.ObservationDigest,
		Frontier:          make([]frontierWire, 0, len(p.Frontier)),
	}
	for _, e := range p.Frontier {
		w.Frontier = append(w.Frontier, frontierWire{
			Step: string(e.Step), Node: string(e.Node), Ordinal: e.Ordinal, Kind: string(e.Kind),
		})
	}
	nw := nextWire{Kind: string(p.Next.Kind)}
	if p.Next.Ready != nil {
		rw := &readyWire{Tasks: make([]readyTaskWire, 0, len(p.Next.Ready.Tasks))}
		for _, t := range p.Next.Ready.Tasks {
			rw.Tasks = append(rw.Tasks, readyTaskWire{Task: t.Task.String(), Step: string(t.Step)})
		}
		nw.Ready = rw
	}
	if p.Next.HostAction != nil {
		nw.HostAction = &hostActionWire{Steps: idStrings(p.Next.HostAction.Steps)}
	}
	if p.Next.Ask != nil {
		nw.Ask = &askWire{Steps: idStrings(p.Next.Ask.Steps)}
	}
	if p.Next.Wait != nil {
		nw.Wait = &waitWire{Reason: string(p.Next.Wait.Reason)}
	}
	if p.Next.Operator != nil {
		ow := &operatorWire{Facts: make([]factWire, 0, len(p.Next.Operator.Facts))}
		for _, f := range p.Next.Operator.Facts {
			ow.Facts = append(ow.Facts, factWire{Source: string(f.Source), Key: f.Key, Value: f.Value})
		}
		nw.Operator = ow
	}
	if p.Next.Complete != nil {
		nw.Complete = &completeWire{}
	}
	w.Next = nw
	return canonicalJSON(w)
}

// Digest 返回 Plan canonical 字节的 SHA-256 摘要（sha256: 前缀），
// 即 §5.9 的 PlanDigest。
func (p *Plan) Digest() (string, error) {
	data, err := p.CanonicalBytes()
	if err != nil {
		return "", err
	}
	return encoder.Digest(data), nil
}

func idStrings(ids []authoring.StepID) []string {
	if len(ids) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}
