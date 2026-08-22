package compiler

import (
	"sort"

	"formal-gates/internal/engine/authoring"
)

// 本文件承载全局图不变量（ADR-001 决策 3）：依赖存在、无循环、可达性、
// 并行组 join/failure 覆盖。这些都是单步 constructor 看不见的全局性质。

// checkRefs 校验引用存在性：依赖、并行 children 与 join 目标都必须是表内
// 步骤。它必须先于定序执行，保证"引用缺失"报的是 not found 而不是伪装成
// cycle（spike 结论）。
func checkRefs(steps []CompiledStep, index map[authoring.StepID]int) error {
	for i := range steps {
		cs := &steps[i]
		for _, d := range cs.Header.Dependencies {
			if _, ok := index[d]; !ok {
				return invariantError(`step %q: dependency %q not found`, cs.Header.ID, d)
			}
		}
		if p, ok := cs.Payload.(CompiledParallelStep); ok {
			for _, c := range p.Children {
				if _, ok := index[c]; !ok {
					return invariantError(`parallel step %q: child %q not found`, cs.Header.ID, c)
				}
			}
			if _, ok := index[p.Join.JoinStep]; !ok {
				return invariantError(`parallel step %q: join step %q not found`, cs.Header.ID, p.Join.JoinStep)
			}
		}
	}
	return nil
}

// computeOrdinals 以确定性 Kahn 拓扑序派生 ordinal：每轮从 ready 集合
// （全部依赖已定序的步骤）中取 (nodeID, stepID) 字典序最小者。ordinal 是
// 图性质的函数，与输入 assembly 顺序无关——这是 assembly 顺序不泄漏进
// 制品的关键（spike 实证）。无法收敛即存在依赖循环，报出卡住的步骤集合。
func computeOrdinals(steps []CompiledStep) (map[authoring.StepID]int, error) {
	assigned := make(map[authoring.StepID]int, len(steps))
	done := make([]bool, len(steps))
	for len(assigned) < len(steps) {
		pick := -1
		for i := range steps {
			if done[i] {
				continue
			}
			ready := true
			for _, d := range steps[i].Header.Dependencies {
				if _, ok := assigned[d]; !ok {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			if pick < 0 || stepKeyLess(steps[i].Header, steps[pick].Header) {
				pick = i
			}
		}
		if pick < 0 {
			var stuck []string
			for i := range steps {
				if !done[i] {
					stuck = append(stuck, string(steps[i].Header.ID))
				}
			}
			sort.Strings(stuck)
			return nil, invariantError("dependency cycle among steps %v", stuck)
		}
		assigned[steps[pick].Header.ID] = len(assigned)
		done[pick] = true
	}
	return assigned, nil
}

// stepKeyLess 是 ready 集合与输出排序共用的字典序键：(nodeID, stepID)。
func stepKeyLess(a, b CompiledHeader) bool {
	if a.NodeID != b.NodeID {
		return a.NodeID < b.NodeID
	}
	return a.ID < b.ID
}

// checkReachable 校验可达性：根 = 入口节点内无依赖的步骤；沿依赖方向前向
// 传播，表内每一步都必须从某个根可达。入口节点之外的无依赖步骤、或挂在
// 断开分支上的步骤都是不可达步骤（八类拒绝之一）。
func checkReachable(entry authoring.NodeID, steps []CompiledStep) error {
	dependents := make(map[authoring.StepID][]authoring.StepID)
	var frontier []authoring.StepID
	entrySeen := false
	for i := range steps {
		h := steps[i].Header
		if h.NodeID == entry {
			entrySeen = true
			if len(h.Dependencies) == 0 {
				frontier = append(frontier, h.ID)
			}
		}
		for _, d := range h.Dependencies {
			dependents[d] = append(dependents[d], h.ID)
		}
	}
	if !entrySeen {
		return invariantError("entry node %q has no steps", entry)
	}
	if len(frontier) == 0 {
		return invariantError("entry node %q has no dependency-free step (graph roots required)", entry)
	}
	visited := make(map[authoring.StepID]bool, len(steps))
	for _, r := range frontier {
		visited[r] = true
	}
	for len(frontier) > 0 {
		cur := frontier[len(frontier)-1]
		frontier = frontier[:len(frontier)-1]
		for _, nxt := range dependents[cur] {
			if !visited[nxt] {
				visited[nxt] = true
				frontier = append(frontier, nxt)
			}
		}
	}
	var unreachable []string
	for i := range steps {
		if !visited[steps[i].Header.ID] {
			unreachable = append(unreachable, string(steps[i].Header.ID))
		}
	}
	if len(unreachable) > 0 {
		sort.Strings(unreachable)
		return invariantError("unreachable steps %v", unreachable)
	}
	return nil
}

// checkParallelGroups 校验并行组图不变量：
//   - join 必须是组外分立步骤：join 步 == 并行步自身时 join 依赖集合与
//     children 自指重合，fan-out 覆盖与分支封闭检查全部被绕过（封板后审计
//     H1；constructor 主拦，这里对绕过构造的原始结构体二次防线复核）；
//   - 归属排他：同一 child 或同一 join step 不得被多个并行组声明，否则
//     调度归属歧义（封板后审计 H2）；
//   - fan-out 锚点依赖存在（并行组必须挂在已完成步骤后）；
//   - join 目标不得同时是 children 成员；
//   - join 依赖集合与 children 集合精确相等——join 覆盖完整 fan-out，
//     既不缺成员也不多拉外部步骤（八类拒绝之一的主拦截层）；
//   - 分支封闭：成员的 dependent 只能是 join 步，防止组内成员被组外部分
//     消费而绕过 join 语义。
//
// join/children 的存在性已由 checkRefs 保证。
func checkParallelGroups(steps []CompiledStep, index map[authoring.StepID]int) error {
	dependents := make(map[authoring.StepID][]authoring.StepID)
	for i := range steps {
		for _, d := range steps[i].Header.Dependencies {
			dependents[d] = append(dependents[d], steps[i].Header.ID)
		}
	}
	// 归属排他预检：先登记全部并行组的 children/join 归属，冲突立即拒绝，
	// 不与单组覆盖检查竞争报错优先级。
	childOwner := make(map[authoring.StepID]authoring.StepID)
	joinOwner := make(map[authoring.StepID]authoring.StepID)
	for i := range steps {
		p, ok := steps[i].Payload.(CompiledParallelStep)
		if !ok {
			continue
		}
		h := steps[i].Header
		if p.Join.JoinStep == h.ID {
			return invariantError(`parallel step %q: join step %q must be outside the parallel group (join step is the parallel step itself)`, h.ID, p.Join.JoinStep)
		}
		for _, c := range p.Children {
			if owner, clash := childOwner[c]; clash {
				return invariantError(`parallel step %q: child %q already claimed by parallel step %q (parallel group ownership is exclusive)`, h.ID, c, owner)
			}
			childOwner[c] = h.ID
		}
		if owner, clash := joinOwner[p.Join.JoinStep]; clash {
			return invariantError(`parallel step %q: join step %q already claimed by parallel step %q (parallel group ownership is exclusive)`, h.ID, p.Join.JoinStep, owner)
		}
		joinOwner[p.Join.JoinStep] = h.ID
	}
	for i := range steps {
		p, ok := steps[i].Payload.(CompiledParallelStep)
		if !ok {
			continue
		}
		h := steps[i].Header
		if len(h.Dependencies) == 0 {
			return invariantError("parallel step %q: fan-out anchor dependency required", h.ID)
		}
		children := make(map[authoring.StepID]bool, len(p.Children))
		for _, c := range p.Children {
			if c == p.Join.JoinStep {
				return invariantError(`parallel step %q: join step %q must not be a child`, h.ID, c)
			}
			children[c] = true
		}
		joinDeps := make(map[authoring.StepID]bool, len(steps[index[p.Join.JoinStep]].Header.Dependencies))
		for _, d := range steps[index[p.Join.JoinStep]].Header.Dependencies {
			joinDeps[d] = true
		}
		for _, c := range p.Children {
			if !joinDeps[c] {
				return invariantError(`parallel step %q: join step %q does not depend on child %q (fan-out coverage)`, h.ID, p.Join.JoinStep, c)
			}
		}
		for d := range joinDeps {
			if !children[d] {
				return invariantError(`parallel step %q: join step %q depends on %q outside children (fan-out coverage)`, h.ID, p.Join.JoinStep, d)
			}
		}
		for _, c := range p.Children {
			for _, dep := range dependents[c] {
				if dep != p.Join.JoinStep {
					return invariantError(`parallel step %q: child %q has dependent %q other than join %q`, h.ID, c, dep, p.Join.JoinStep)
				}
			}
		}
	}
	return nil
}
