// Package runtime 承载决策内核的运行期纯数据模型（阶段 1 批 2a）：
// run-level phase 迁移表、动态任务的稳定键与状态机、开发批次（batch）的
// 分组/依赖信息与完成状态派生。
//
// 本包只含数据与静态表，不做 I/O、不持可变全局状态：state 投影与
// Observe/Decide/SelectIssued 属 internal/engine/decision（批 2b），真实
// 外部事实收集器与可靠写协议属后续批次。Go 静态迁移表是唯一流程权威
// （master-requirements §5.1）；迁移表只管理 run-level phase，动态任务由
// TaskKey/TaskTransitionTable 管理（§5.10）。
package runtime

import (
	"fmt"
	"sort"
)

// RunPhase 是 run-level 阶段枚举，名称原样采用 final-implementation-draft
// §3.1 的建议图（编码期不微调）。零值与任何其他字符串一律非法。
type RunPhase string

const (
	// PhaseIntakeRegistered：run 已创建，受理确认已登记（intake receipt）。
	PhaseIntakeRegistered RunPhase = "INTAKE_REGISTERED"
	// PhaseProductReview：产品审进行中。
	PhaseProductReview RunPhase = "PRODUCT_REVIEW"
	// PhaseTechnicalReview：技术审进行中。
	PhaseTechnicalReview RunPhase = "TECHNICAL_REVIEW"
	// PhaseStartReadiness：开发前就绪检查。
	PhaseStartReadiness RunPhase = "START_READINESS"
	// PhaseTopologyAndRoute：start-readiness PASS 后的拆分拓扑与路线确认
	// （唯一绑定点，总需求 §8.1.1）。
	PhaseTopologyAndRoute RunPhase = "TOPOLOGY_AND_ROUTE"
	// PhaseDevelopmentParallel：开发期并行组（开发 worker + 黑盒 QA 设计/
	// 审查；分片时各 child 开发 + 合并 QA 用例设计/审查）。
	PhaseDevelopmentParallel RunPhase = "DEVELOPMENT_PARALLEL"
	// PhaseSnapshotReady：验证候选（Dn/Qn 或主线快照）已冻结，验证组执行中。
	PhaseSnapshotReady RunPhase = "SNAPSHOT_READY"
	// PhasePostReview：验证 wave join 后的发现项处置与审查决定点。
	PhasePostReview RunPhase = "POST_REVIEW"
	// PhaseRepair：返修轮（开发返修或 QA artifact 修订的执行段）。
	PhaseRepair RunPhase = "REPAIR"
	// PhaseSliceReady：child 可恢复 checkpoint（child 不独立 SEALED，
	// 总需求 §8.1.3）。
	PhaseSliceReady RunPhase = "SLICE_READY"
	// PhaseMainlineIntegration：master 收齐全部 child receipts 后的 VCS
	// adapter 主线集成与主线快照冻结。
	PhaseMainlineIntegration RunPhase = "MAINLINE_INTEGRATION"
	// PhaseMergeValidation：合并 QA 与合并门并行执行段。
	PhaseMergeValidation RunPhase = "MERGE_VALIDATION"
	// PhaseTerminal：终态（SEALED/ABORTED 后经统一 cleanup 进入；无出边）。
	PhaseTerminal RunPhase = "TERMINAL"
)

// phases 是全部合法 phase 值，顺序即建议图的主线顺序。枚举校验与终止
// 闭包都从本表派生，新增 phase 必须同步维护 pipelineEdges。
var phases = []RunPhase{
	PhaseIntakeRegistered, PhaseProductReview, PhaseTechnicalReview,
	PhaseStartReadiness, PhaseTopologyAndRoute, PhaseDevelopmentParallel,
	PhaseSnapshotReady, PhasePostReview, PhaseRepair, PhaseSliceReady,
	PhaseMainlineIntegration, PhaseMergeValidation, PhaseTerminal,
}

// Valid 报告 p 是否属于封闭 phase 集合。
func (p RunPhase) Valid() bool {
	for _, v := range phases {
		if p == v {
			return true
		}
	}
	return false
}

// PhaseEdge 是一条合法的 phase 迁移边。
type PhaseEdge struct {
	From RunPhase
	To   RunPhase
}

// pipelineEdges 是流水线进度边（含修复回环与分片/合并支路），语义来源：
// 总需求 §6（受理与双审顺序）、§8.1（拆分绑定点与 child 生命周期）、
// §8.3（候选冻结→验证→join 的回环）、§8.2（合并路径）。
//
//	INTAKE_REGISTERED → PRODUCT_REVIEW → TECHNICAL_REVIEW
//	  → START_READINESS → TOPOLOGY_AND_ROUTE → DEVELOPMENT_PARALLEL
//
// 非分片/child 开发回环：
//
//	DEVELOPMENT_PARALLEL → SNAPSHOT_READY → POST_REVIEW
//	POST_REVIEW → REPAIR → SNAPSHOT_READY        开发返修：新实现快照
//	POST_REVIEW → SNAPSHOT_READY                 QA artifact 修订：候选替换
//	                                             （不形成 RepairObligation，
//	                                             同一候选替换不变量 §8.3.7）
//	POST_REVIEW → TERMINAL                       非分片无义务自动 Seal
//	POST_REVIEW → SLICE_READY                    child 达标进 checkpoint
//
// master 分片支路：
//
//	DEVELOPMENT_PARALLEL → MAINLINE_INTEGRATION  全部 child receipts 收齐
//	MAINLINE_INTEGRATION → MERGE_VALIDATION      主线快照冻结后双验证并行
//	MERGE_VALIDATION → REPAIR → MAINLINE_INTEGRATION  主线返修回环
//	MERGE_VALIDATION → TERMINAL                  master Seal
//	SLICE_READY → TERMINAL                       child 由 master Seal/级联终结
var pipelineEdges = []PhaseEdge{
	{PhaseIntakeRegistered, PhaseProductReview},
	{PhaseProductReview, PhaseTechnicalReview},
	{PhaseTechnicalReview, PhaseStartReadiness},
	{PhaseStartReadiness, PhaseTopologyAndRoute},
	{PhaseTopologyAndRoute, PhaseDevelopmentParallel},
	{PhaseDevelopmentParallel, PhaseSnapshotReady},
	{PhaseSnapshotReady, PhasePostReview},
	{PhasePostReview, PhaseRepair},
	{PhaseRepair, PhaseSnapshotReady},
	{PhasePostReview, PhaseSnapshotReady},
	{PhasePostReview, PhaseSliceReady},
	{PhasePostReview, PhaseTerminal},
	{PhaseSliceReady, PhaseTerminal},
	{PhaseDevelopmentParallel, PhaseMainlineIntegration},
	{PhaseMainlineIntegration, PhaseMergeValidation},
	{PhaseMergeValidation, PhaseRepair},
	{PhaseRepair, PhaseMainlineIntegration},
	{PhaseMergeValidation, PhaseTerminal},
}

// phaseTransitionTable 是合法迁移边全表：流水线边 ∪ 终止闭包。终止闭包
// 来自总需求 §6.3.10–11——任意 ACTIVE 非终态阶段都可经用户授权的
// reset/abort/破坏性级联进入统一 cleanup 终结路径；因此每个非 TERMINAL
// phase 都有指向 TERMINAL 的边。用户授权的业务节点跳转（§6.3.11）经
// suspend/quarantine/reconcile 机制由后续批次的 Controller 承载，不在
// 本表展开为任意点对点边。
var phaseTransitionTable = buildPhaseTransitionTable()

// buildPhaseTransitionTable 组装全表并按 (From, To) 排序，使边表本身即
// 是稳定的 golden 形态。
func buildPhaseTransitionTable() []PhaseEdge {
	type edgeKey struct{ from, to RunPhase }
	seen := make(map[edgeKey]bool)
	var out []PhaseEdge
	add := func(e PhaseEdge) {
		k := edgeKey{e.From, e.To}
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, e)
	}
	for _, e := range pipelineEdges {
		add(e)
	}
	for _, p := range phases {
		if p != PhaseTerminal {
			add(PhaseEdge{From: p, To: PhaseTerminal})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

// PhaseTransitionTable 返回合法迁移边全表（每次返回副本；静态权威本身
// 不可变，调用方不得依赖两次调用共享底层数组）。
func PhaseTransitionTable() []PhaseEdge {
	out := make([]PhaseEdge, len(phaseTransitionTable))
	copy(out, phaseTransitionTable)
	return out
}

// phaseAllowed 是全表的索引视图，供 O(1) 查询共用同一静态权威。
var phaseAllowed = func() map[PhaseEdge]bool {
	m := make(map[PhaseEdge]bool, len(phaseTransitionTable))
	for _, e := range phaseTransitionTable {
		m[e] = true
	}
	return m
}()

// PhaseCanTransition 报告 from → to 是否为合法边。非法枚举值与未列出的
// 点对（回退、跳步、自环、TERMINAL 出边）一律 false。
func PhaseCanTransition(from, to RunPhase) bool {
	return phaseAllowed[PhaseEdge{From: from, To: to}]
}

// PhaseTransition 校验并返回目标 phase；非法边返回错误。调用方在成功后
// 才可把 to 写入 state 投影。
func PhaseTransition(from, to RunPhase) error {
	if PhaseCanTransition(from, to) {
		return nil
	}
	return fmt.Errorf("runtime: illegal phase transition %s -> %s", from, to)
}
