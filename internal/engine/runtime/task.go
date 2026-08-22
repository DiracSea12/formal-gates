package runtime

import (
	"fmt"
	"sort"
	"strings"

	"formal-gates/internal/engine/authoring"
)

// TaskKey 是动态任务的稳定键（master-requirements §5.10）：由 node/step/
// scope 三段派生。node/step 绑定 definition 中的所属节点与步骤；scope
// 区分同一步骤上的并行实例（child、case 等动态扇出），静态步骤为空串。
// 键值即 String() 的规范形态，跨进程/持久化稳定。
type TaskKey struct {
	Node  authoring.NodeID
	Step  authoring.StepID
	Scope string
}

// NewTaskKey 构造并校验稳定键：node 与 step 必填（scope 可空）；三段均不得
// 包含 "/" 或 "\"——String() 以 "/" 连接三段，段内分隔符会使不同键坍缩为
// 同一字符串形态（{n,a/b} 与 {n/a,b} 同为 n/a/b，封板后审计 H4）。
func NewTaskKey(node authoring.NodeID, step authoring.StepID, scope string) (TaskKey, error) {
	k := TaskKey{Node: node, Step: step, Scope: scope}
	if !k.Valid() {
		return TaskKey{}, fmt.Errorf("runtime: task key requires node and step, got %q", k.String())
	}
	for _, seg := range []struct{ name, value string }{
		{"node", string(k.Node)}, {"step", string(k.Step)}, {"scope", k.Scope},
	} {
		if strings.ContainsAny(seg.value, `/\`) {
			return TaskKey{}, fmt.Errorf("runtime: task key %s %q must not contain '/' or '\\' (canonical string form joins node/step/scope with '/')", seg.name, seg.value)
		}
	}
	return k, nil
}

// Valid 报告键是否可解析：node 与 step 非空。
func (k TaskKey) Valid() bool {
	return k.Node != "" && k.Step != ""
}

// String 返回稳定键形态 "node/step" 或 "node/step/scope"（scope 为空时
// 省略尾段与分隔符）。同一 TaskKey 恒得同一字符串。
func (k TaskKey) String() string {
	if k.Scope == "" {
		return string(k.Node) + "/" + string(k.Step)
	}
	return string(k.Node) + "/" + string(k.Step) + "/" + k.Scope
}

// TaskStatus 是单个动态任务的状态机（final-implementation-draft §3.1）。
// 任务级结果是 TaskStatus 之外的数据（由后续批次的 typed result 承载），
// 状态机本身只描述调度位置。
type TaskStatus string

const (
	// TaskQueued：依赖已满足、等待签发（含"尚未登记"——状态投影中缺省
	// 即 QUEUED，见 decision.State.TaskStatus）。
	TaskQueued TaskStatus = "QUEUED"
	// TaskIssued：SelectIssued 已分配 actionID 并持久化，尚未观察到运行。
	TaskIssued TaskStatus = "ISSUED"
	// TaskRunning：worker/lifecycle 已观察到运行中。
	TaskRunning TaskStatus = "RUNNING"
	// TaskValidating：结果已返回，正在做结果校验与 join 前判定。
	TaskValidating TaskStatus = "VALIDATING"
	// TaskTerminal：任务终态（PASS/FAIL/RUNTIME_ERROR 等结果分类不进入
	// 状态机）。无出边。
	TaskTerminal TaskStatus = "TERMINAL"
)

// taskStatuses 是全部合法任务状态值。
var taskStatuses = []TaskStatus{
	TaskQueued, TaskIssued, TaskRunning, TaskValidating, TaskTerminal,
}

// Valid 报告 s 是否属于封闭状态集合。
func (s TaskStatus) Valid() bool {
	for _, v := range taskStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// TaskEdge 是一条合法的任务状态转移边。
type TaskEdge struct {
	From TaskStatus
	To   TaskStatus
}

// taskTransitionTable 是任务状态机合法转移全表。状态机只前进：
//
//	QUEUED    → ISSUED      SelectIssued 签发
//	QUEUED    → TERMINAL    未签发即被作废/替代（需求变化 Phase B、
//	                       用户授权跳转的失效清理）
//	ISSUED    → RUNNING     lifecycle/worker 观察到运行
//	ISSUED    → TERMINAL    签发后被终止（abort/reset 级联）
//	RUNNING   → VALIDATING  worker 结果返回，进入校验
//	RUNNING   → TERMINAL    运行期失败（RUNTIME_ERROR 类）不进结果校验
//	VALIDATING → TERMINAL   校验完成
//
// 不存在回退边：重跑不把旧状态拨回 QUEUED，而是按 §6.4.3 开新 Attempt/
// 新任务实例，旧实例经 TERMINAL stale（result-before-receipt 的对账与
// OBSOLETE_RESULT 拒绝属后续可靠写入批次）。
var taskTransitionTable = []TaskEdge{
	{TaskQueued, TaskIssued},
	{TaskQueued, TaskTerminal},
	{TaskIssued, TaskRunning},
	{TaskIssued, TaskTerminal},
	{TaskRunning, TaskValidating},
	{TaskRunning, TaskTerminal},
	{TaskValidating, TaskTerminal},
}

// TaskTransitionTable 返回合法转移全表（副本；静态权威不可变）。
func TaskTransitionTable() []TaskEdge {
	out := make([]TaskEdge, len(taskTransitionTable))
	copy(out, taskTransitionTable)
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

// taskAllowed 是转移表的索引视图。
var taskAllowed = func() map[TaskEdge]bool {
	m := make(map[TaskEdge]bool, len(taskTransitionTable))
	for _, e := range taskTransitionTable {
		m[e] = true
	}
	return m
}()

// TaskCanTransition 报告 from → to 是否合法。非法枚举值与未列出点对
// （回退、跳过 RUNNING 直达等）一律 false。
func TaskCanTransition(from, to TaskStatus) bool {
	return taskAllowed[TaskEdge{From: from, To: to}]
}

// TaskTransition 校验转移合法性；非法转移返回错误。调用方在成功后才可
// 写入状态投影。
func TaskTransition(from, to TaskStatus) error {
	if TaskCanTransition(from, to) {
		return nil
	}
	return fmt.Errorf("runtime: illegal task transition %s -> %s", from, to)
}
