// Package decision 是确定性三段式决策核心：
//
//	Observe(state, collectors)      只读收集带 source binding 的外部事实
//	Decide(state, obs, definition)  纯函数，产出字节级稳定的 canonical Plan
//	SelectIssued(plan, admission)   按容量机械裁剪，分配 actionID 并持久化
//
// 本批只有决策与接口：真实外部事实收集器（VCS/文件/宿主/lifecycle/
// receipt/容量）与可靠写协议（revision CAS、submit 幂等）属后续批次。
// 依赖单向向下：authoring → compiler → encoder 之下的 runtime 模型与
// 本包；本包不执行 handler、不写权威 state。
package decision

import (
	"fmt"
	"slices"
	"sort"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/runtime"
)

// State 是决策内核的 state 投影视图：run-level phase、已完成步骤与动态
// 任务状态。权威持久化投影（state.json 及其版本信封、revision/CAS）由
// 持久化层承载；本结构只承载 Decide/Observe 所需的最小决策输入。Tasks 中缺省
// 的键按 QUEUED 解释（依赖满足、尚未签发）。
type State struct {
	OperatorFacts     []Fact                                 `json:"-"`
	DefinitionVersion authoring.DefinitionVersion            `json:"definitionVersion"`
	Phase             runtime.RunPhase                       `json:"phase"`
	Completed         []authoring.StepID                     `json:"completed"` // 有序去重，由 CompleteStep 维护
	Tasks             map[runtime.TaskKey]runtime.TaskStatus `json:"tasks"`
	// RequiredTasks is a read-only projection of authorized work still owed.
	// Its owner supplies it with the view; it is not another persisted ledger.
	RequiredTasks map[authoring.StepID][]RequiredTask `json:"-"`
	// ControlRequests are unresolved protocol Ask identities. Suspension holds
	// automatic progression until the controlling requirement Ask is resolved.
	ControlRequests []string `json:"-"`
	Suspended       bool     `json:"-"`
}

// RequiredTask carries an outstanding obligation and its policy eligibility.
// An empty RequiredTasks entry suppresses the step's implicit default task.
// Blocked work still holds dependencies and completion until its owner releases it.
type RequiredTask struct {
	Task      runtime.TaskKey `json:"task"`
	BlockedBy WaitReason      `json:"blockedBy,omitempty"`
	// Covers names the alternate carrier steps whose dependents share this
	// obligation. It changes dependency satisfaction, never execution identity.
	Covers []authoring.StepID `json:"covers,omitempty"`
}

// NewState 构造并校验初始 state：definition 版本与 phase 枚举必填。
func NewState(version authoring.DefinitionVersion, phase runtime.RunPhase) (*State, error) {
	if version == "" {
		return nil, fmt.Errorf("decision: state definition version required")
	}
	if !phase.Valid() {
		return nil, fmt.Errorf("decision: state phase %q invalid", phase)
	}
	return &State{DefinitionVersion: version, Phase: phase}, nil
}

// TaskStatusOf 返回任务当前状态；未登记的键按 QUEUED 解释。
func (s *State) TaskStatusOf(key runtime.TaskKey) runtime.TaskStatus {
	if st, ok := s.Tasks[key]; ok {
		return st
	}
	return runtime.TaskQueued
}

// CompleteStep 登记一个步骤完成，是运行时边界的接口校验
// （运行时只允许当前 eligible frontier，拒绝
// 乱序、遗漏和重复 step）。四类拒绝：
//   - 版本绑定不符（state 与 definition 的版本信封不一致）；
//   - 遗漏/未知：完成一个不在 definition 中的步骤；
//   - 重复：步骤已完成再次完成；
//   - 乱序/遗漏：步骤的依赖尚未全部完成。
func (s *State) CompleteStep(id authoring.StepID, cd *compiler.CompiledDefinition) error {
	if cd == nil {
		return fmt.Errorf("decision: complete step %q: nil definition", id)
	}
	if s.DefinitionVersion != cd.Version {
		return fmt.Errorf("decision: complete step %q: state definition version %q != definition %q", id, s.DefinitionVersion, cd.Version)
	}
	var step *compiler.CompiledStep
	for i := range cd.Steps {
		if cd.Steps[i].Header.ID == id {
			step = &cd.Steps[i]
			break
		}
	}
	if step == nil {
		return fmt.Errorf("decision: complete step %q: not in definition", id)
	}
	for _, done := range s.Completed {
		if done == id {
			return fmt.Errorf("decision: complete step %q: already completed (duplicate)", id)
		}
	}
	for _, dep := range step.Header.Dependencies {
		if !s.completedContains(dep) {
			return fmt.Errorf("decision: complete step %q: dependency %q not completed (out-of-order or skipped)", id, dep)
		}
	}
	// 有序插入，保证 Completed 与 CanonicalBytes 不依赖登记顺序。
	pos := sort.Search(len(s.Completed), func(i int) bool { return s.Completed[i] >= id })
	s.Completed = append(s.Completed, "")
	copy(s.Completed[pos+1:], s.Completed[pos:])
	s.Completed[pos] = id
	return nil
}

func (s *State) completedContains(id authoring.StepID) bool {
	for _, done := range s.Completed {
		if done == id {
			return true
		}
	}
	return false
}

// TransitionPhase 按 runtime 静态迁移表校验并推进 phase。
func (s *State) TransitionPhase(to runtime.RunPhase) error {
	if err := runtime.PhaseTransition(s.Phase, to); err != nil {
		return err
	}
	s.Phase = to
	return nil
}

// TransitionTask 按 TaskTransitionTable 校验并推进任务状态；未登记的键
// 从 QUEUED 出发。非法转移（回退、跳步、自环）一律拒绝。
func (s *State) TransitionTask(key runtime.TaskKey, to runtime.TaskStatus) error {
	if !key.Valid() {
		return fmt.Errorf("decision: task key %q invalid", key.String())
	}
	from := s.TaskStatusOf(key)
	if err := runtime.TaskTransition(from, to); err != nil {
		return err
	}
	if s.Tasks == nil {
		s.Tasks = make(map[runtime.TaskKey]runtime.TaskStatus)
	}
	s.Tasks[key] = to
	return nil
}

// stateWire 是 State 的 canonical wire 形态：任务按 TaskKey 字符串排序，
// 不携带 map 遍历序；Completed 由 CompleteStep 维持有序。
type stateWire struct {
	OperatorFacts     []Fact                              `json:"operatorFacts,omitempty"`
	DefinitionVersion string                              `json:"definitionVersion"`
	Phase             string                              `json:"phase"`
	Completed         []string                            `json:"completed"`
	Tasks             []stateTaskWire                     `json:"tasks"`
	RequiredTasks     map[authoring.StepID][]RequiredTask `json:"requiredTasks,omitempty"`
	ControlRequests   []string                            `json:"controlRequests,omitempty"`
	Suspended         bool                                `json:"suspended,omitempty"`
}

type stateTaskWire struct {
	Key    string `json:"key"`
	Status string `json:"status"`
}

// CanonicalBytes 返回 State 的 canonical 字节（形态与 encoder 制品一致；
// 空集编码为 []，不用 null）。这是 StateDigest 的输入，不是权威
// state.json 投影——后者由持久化层承载。
func (s *State) CanonicalBytes() ([]byte, error) {
	w := stateWire{
		DefinitionVersion: string(s.DefinitionVersion),
		Phase:             string(s.Phase),
		Completed:         make([]string, 0, len(s.Completed)),
		Tasks:             make([]stateTaskWire, 0, len(s.Tasks)),
	}
	w.OperatorFacts = s.OperatorFacts
	w.ControlRequests = append([]string(nil), s.ControlRequests...)
	sort.Strings(w.ControlRequests)
	w.Suspended = s.Suspended
	for _, id := range s.Completed {
		w.Completed = append(w.Completed, string(id))
	}
	if len(s.RequiredTasks) > 0 {
		w.RequiredTasks = make(map[authoring.StepID][]RequiredTask, len(s.RequiredTasks))
		for step, tasks := range s.RequiredTasks {
			ordered := append([]RequiredTask{}, tasks...)
			sort.Slice(ordered, func(i, j int) bool { return ordered[i].Task.String() < ordered[j].Task.String() })
			w.RequiredTasks[step] = ordered
		}
	}
	keys := make([]string, 0, len(s.Tasks))
	byKey := make(map[string]runtime.TaskStatus, len(s.Tasks))
	for k, st := range s.Tasks {
		ks := k.String()
		keys = append(keys, ks)
		byKey[ks] = st
	}
	sort.Strings(keys)
	for _, ks := range keys {
		w.Tasks = append(w.Tasks, stateTaskWire{Key: ks, Status: string(byKey[ks])})
	}
	return canonicalJSON(w)
}

// Digest 返回 State canonical 字节的 SHA-256 摘要（sha256: 前缀）。
func (s *State) Digest() (string, error) {
	data, err := s.CanonicalBytes()
	if err != nil {
		return "", err
	}
	return encoder.Digest(data), nil
}

// StepSatisfied requires a completed step and no outstanding work in its
// dependency ancestry. A carried descendant cannot conceal an upstream redo.
func (s *State) StepSatisfied(id authoring.StepID, cd *compiler.CompiledDefinition) bool {
	if !s.completedContains(id) {
		return false
	}
	for owner, tasks := range s.RequiredTasks {
		for _, task := range tasks {
			if owner == id || slices.Contains(task.Covers, id) {
				return false
			}
		}
	}
	for _, step := range cd.Steps {
		if step.Header.ID != id {
			continue
		}
		for _, dep := range step.Header.Dependencies {
			if !s.StepSatisfied(dep, cd) {
				return false
			}
		}
		return true
	}
	return false
}
