package validate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"formal-gates/internal/cost"
	"formal-gates/internal/lifecycle"
)

type RunState struct {
	RunID                string                       `json:"runId"`
	Flow                 string                       `json:"flow"`
	Status               string                       `json:"status"`
	RequirementSource    string                       `json:"requirementSource"`
	RequirementRevision  string                       `json:"requirementRevision"`
	RequirementConfirmed bool                         `json:"requirementConfirmed"`
	RequirementArtifacts []RequirementArtifact        `json:"requirementArtifacts"`
	BasePromptRevision   string                       `json:"basePromptRevision"`
	CatalogRevision      string                       `json:"catalogRevision"`
	PromptHashes         map[string]string            `json:"promptHashes,omitempty"`
	VCS                  string                       `json:"vcs"`
	BaseSnapshot         string                       `json:"baseSnapshot"`
	CurrentSnapshot      string                       `json:"currentSnapshot"`
	// StateIntegrity 是 run 状态的完整性校验：SaveRunState 写盘前先置空本字段、
	// 以 json.MarshalIndent 规范化序列化、对规范化内容计算 sha256 回填后写盘；LoadRunState
	// 校验非空的本字段：置空后按同样方式重算比对，不匹配即硬拒绝 "state was modified outside
	// the CLI"。run 状态只能由 CLI 写入，任何人（含 host/主代理）不得手工改写。旧状态文件
	// 无本字段，跳过校验。随 Seal 保留（写入后随状态文件持久化）。
	StateIntegrity       string                       `json:"stateIntegrity,omitempty"`
	RetainedOverall      bool                         `json:"retainedOverall,omitempty"`
	// SplitDeclaration 记录 workflow start 时强制声明的拆分意向（需求 4）：yes 表示本 run 是
	// 保留总任务实例或切片实例、no 表示不拆分。旧 run（本功能上线前启动）缺失本字段，按旧
	// 语义处理：保留总任务实例仍可记录 split，其余 run 不受额外限制。
	SplitDeclaration     string                       `json:"splitDeclaration,omitempty"`
	// SplitMasterRunID 记录切片实例在启动声明中钉死的保留总任务 master run id（--split yes
	// --master <id>）；workflow slicing 记录 split 时引用的 master 必须与之一致。
	SplitMasterRunID     string                       `json:"splitMasterRunID,omitempty"`
	PreRepairSnapshot    string                       `json:"preRepairSnapshot,omitempty"`
	Slicing              *Slicing                     `json:"slicing,omitempty"`
	SettledFindings      map[string][]SettledFinding  `json:"settledFindings,omitempty"`
	RouteMode            string                       `json:"routeMode,omitempty"`
	SelectedGates        []string                     `json:"selectedGates"`
	SkipAuthorizations   map[string]SkipAuthorization `json:"skipAuthorizations"`
	CompletedReviewWaves int                          `json:"completedReviewWaves"`
	ExtraReviewWaves     int                          `json:"extraReviewWaves"`
	Actions              map[string]ActionResult      `json:"actions"`
	// QACasesByMode 按 QA 派发 mode 分开存储用例：黑盒/白盒各一条，mode=="" 为
	// 合并/单派发流程。qa-design 记录轮只对该 mode 的列表做增量替换，另一 mode 的既有用例
	// （含其 review PASS 状态与已记录执行结果）保持不动，修复"设计白盒时整表替换清掉黑盒
	// 用例"的数据丢失缺陷。旧状态文件（qaCases 数组）在 LoadRunState 时迁移进 "" 键。
	QACasesByMode map[string][]QACase `json:"qaCasesByMode,omitempty"`
	// QAExecutionByMode 按 QA 派发 mode 分开存储执行结果（黑白盒完完全全解耦）：
	// blackbox / whitebox 各自独立、互不影响，空 mode 键对应合并/单派发流程。一个 mode
	// 的设计/执行/记录 SHALL NOT 重置或清除另一 mode 的执行结果。旧状态文件的单一
	// qaExecution 字段在 LoadRunState 时迁移进 "" 键。
	QAExecutionByMode map[string]QAExecutionResult `json:"qaExecutionByMode,omitempty"`
	// ExecutionScopes 记录每个 QA 派发 mode（blackbox / whitebox / 合并空 mode）最近一次
	// 的重跑 scope 决策：FULL 全量重跑，或 AFFECTED 只重跑 host 综合判定
	// 的受影响子集。按 mode 一条、最新决策覆盖；每条 scope 的 BaseSnapshot 记继承来源
	// （上一轮权威结果快照）。
	ExecutionScopes map[string]QAExecutionScope `json:"executionScopes"`
	// PriorQAExecutionByMode 按 mode 分开保留"上一轮权威执行结果"（PASS/FAIL，含快照与
	// FAIL 用例集），供重跑识别与 AFFECTED 子集判定使用：修复快照
	// 推进时从被重置的权威结果保留而来，被该 mode 新一轮权威结果记录时只取代本 mode。
	// 一个 mode 记录新权威结果 SHALL NOT 清空另一 mode 的上一轮权威结果。RUNTIME_ERROR
	// 不构成权威结果、不保留。旧状态文件的单一 priorQAExecution 在 LoadRunState 时迁移
	// 进 "" 键。
	PriorQAExecutionByMode map[string]*QAExecutionResult `json:"priorQAExecutionByMode,omitempty"`
	// QAReviewByMode 按 QA 派发 mode 分开存储 qa-review 权威结果（完完全全解耦）：
	// blackbox / whitebox 各自独立，一个 mode 记录 review 结果 SHALL NOT 影响另一 mode 的
	// review 判定；空 mode 键对应合并/单派发流程。旧状态文件的单一 Actions["qa-review"] 不再
	// 适配：本字段不标 omitempty、恒在场（新 run 序列化为 {}），LoadRunState 缺它即报格式
	// 不符。对本新字段不做 nil 容忍迁移；既有字段的迁移逻辑保持不动。
	QAReviewByMode map[string]ActionResult `json:"qaReviewByMode"`
	// QADesignByMode 按 QA 派发 mode 分开存储 qa-design 权威结果：语义与
	// QAReviewByMode 一致，一个 mode 的 review FAIL 重置设计 SHALL NOT 把另一 mode 的
	// 设计重置为 PENDING。
	QADesignByMode map[string]ActionResult `json:"qaDesignByMode"`
	Gates          map[string]GateResult   `json:"gates"`
	Carry                  map[string]CarryResult        `json:"carry"`
	Dispatches             map[string]PreparedDispatch   `json:"dispatches"`
	Cost                   *cost.RunCost                 `json:"cost,omitempty"`
	// QAWorktree 是黑盒 QA 的隔离工作区路径（Git 为从基线分支的 linked worktree，
	// SVN/P4 为签出到基线版本的工作副本/客户端工作区）。它从基线快照创建、恒等于
	// 基线，始终不含本次开发代码；黑盒 qa-design/qa-review 的原生标识校验与派发源
	// 绑定都改对这里解析。黑盒 qa-review 记录 PASS 时清空，需求作废重置时一并清空。
	QAWorktree string `json:"qaWorktree,omitempty"`
	// BlackboxReviewFails 是黑盒 qa-review 的连续 FAIL 计数：出现 PASS 即清零重算，
	// RUNTIME_ERROR 不计入也不打断连续。用于展示"长期不过"并交由用户决策处置。
	BlackboxReviewFails int `json:"blackboxReviewFails,omitempty"`
	// SnapshotOverride 记录快照黑盒门的手动放行授权：黑盒 qa-review 未 PASS 时经
	// workflow snapshot --user-requested 显式授权带风险继续，记录授权来源。
	SnapshotOverride *SnapshotOverride `json:"snapshotOverride,omitempty"`
	// NeedsReReview 记录"需重审"标记：确认的 P0/P1 发现项（product-review /
	// start-readiness）未重审时置位；record-action PASS 在该标记置位且未重审时被拒。
	// 值是被确认的发现项消息。需求语义变更（invalidateRequirementResults）时一并清除。
	NeedsReReview map[string]string `json:"needsReReview,omitempty"`
	// ReReviewDispatch 记录每个待重审动作当前已派发的重审轮派发 id：只有该重审轮
	// 返回 PASS 才算"重审完成"并清除 NeedsReReview；其余派发在标记置位时记 PASS 被拒。
	ReReviewDispatch map[string]string `json:"reReviewDispatch,omitempty"`
	// ReviewOverrides 记录用户对 product-review / start-readiness 复审规则的显式破例
	// 来源（动作 → 用户理由），与 --user-requested 对应；需求语义变更时一并清除。
	ReviewOverrides map[string]string `json:"reviewOverrides,omitempty"`
	// ReviewItemsByAction 按动作逐项存储需求项的审查结论（增量审查，格式无关）：
	// action（product-review / start-readiness）→ 需求项键 → ReviewItem{Status}。动作级
	// Actions[actionID] 保留为聚合结果（下游判断不变）。增量判定沿 meaning-preserved 修订
	// 的记录：主代理在 prepare-action 显式传 --scope <item>... 声明本次审查范围，声明项置
	// PENDING 待判、未声明的已 PASS 项保持 PASS、任何轮不可改（除非主代理下次显式声明变更）；
	// record-action 对 PENDING 项逐项下发 --item-status PASS|FAIL（全判）、对 PASS 项下发判定
	// 被拒、FAIL 项必须带 finding。meaning-preserved 重绑不清表；meaning-changed 清空（全量重审）。
	ReviewItemsByAction map[string]map[string]ReviewItem `json:"reviewItemsByAction,omitempty"`
}

// ReviewItem 记录某个需求项（需求修订中新增/变更的需求项或验收点）在增量审查中的
// 逐项结论。Status 为 PASS | FAIL | PENDING；DispatchID 是产出该结论的审查
// 派发；Message 记录 FAIL 的 finding。格式无关：增量判定不依赖需求文档结构（openspec/
// PRD 或其它格式统一按"需求修订中新增/变更的需求项或验收点"识别，主代理在 --scope 声明）。
type ReviewItem struct {
	Status     string `json:"status"`
	DispatchID string `json:"dispatchId,omitempty"`
	Message    string `json:"message,omitempty"`
}

type RequirementArtifact struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

// Slicing 记录一次正式 run 的拆分决定。决定是二元（split 或 no-split），一旦记录
// 即为绑定点：确认后不重切，确需调整走既有需求变更流程。拆分建议的呈现与留痕对
// 所有正式 run 必填；仅高置信要拆时需用户确认拆分方案，其余情形记录"建议不拆（
// 原因）"即可。子任务实例继承主任务 run 的拆分决定，本字段只作用于发起的主任务
// run。
type Slicing struct {
	Decision         string   `json:"decision,omitempty"`         // split 或 no-split
	SplitCount       int      `json:"splitCount,omitempty"`       // 子任务数，split 时 >= 2
	Slices           []string `json:"slices,omitempty"`           // 拆分定位：如何拆、子任务边界
	Parallel         string   `json:"parallel,omitempty"`         // 并行建议：哪些子任务可并行
	Note             string   `json:"note,omitempty"`             // 原因留痕，no-split 时必填（建议不拆原因）
	MasterRunID      string   `json:"masterRunID,omitempty"`      // 切片实例引用的保留总任务 run id
	InheritedReviews []string `json:"inheritedReviews,omitempty"` // 切片实例继承的整体级审查来源（product-review/start-readiness）
}

type PreparedDispatch struct {
	ID                  string `json:"id"`
	Target              string `json:"target"`
	TargetKind          string `json:"targetKind"`
	Attempt             int    `json:"attempt"`
	ReviewWave          int    `json:"reviewWave"`
	PromptHash          string `json:"promptHash"`
	RequirementRevision string `json:"requirementRevision"`
	CatalogRevision     string `json:"catalogRevision"`
	SourceSnapshot      string `json:"sourceSnapshot"`
	ReviewerRequired    bool   `json:"reviewerRequired"`
	ReviewerIdentity    string `json:"reviewerIdentity,omitempty"`
	Status              string `json:"status"`
	// Mode 记录 qa-design/qa-review 派发的模式：blackbox 或 whitebox。黑盒派发对
	// QA 隔离工作区解析原生标识与派发源绑定（绑基线）；白盒派发对主工作区解析
	// （绑当前快照）。其他派发为空。
	Mode string `json:"mode,omitempty"`
	// PromptFile 是 prepare 写出的本 run 规范提示词文件路径
	// （.gates/tmp/<run-id>/prompts/<dispatch-id>.md）。派发只消费该规范文件——
	// 主代理只发指向文件的薄启动消息、不得手写/凭记忆拼写提示词内容。写入时立即
	// 校验内容 hash == PromptHash；认领（派发）时兜底再次校验文件内容与 prepare
	// 记录一致，不一致即硬阻断。旧派发（本功能上线前准备）无本字段，不强制。
	PromptFile string `json:"promptFile,omitempty"`
}

type ActionResult struct {
	Status     string    `json:"status"`
	Message    string    `json:"message,omitempty"`
	Findings   []Finding `json:"findings,omitempty"`
	DispatchID string    `json:"dispatchId,omitempty"`
}

type QAExecutionResult struct {
	Status   string           `json:"status,omitempty"`
	Message  string           `json:"message,omitempty"`
	Snapshot string           `json:"snapshot,omitempty"`
	Cases    []QAResultRecord `json:"cases,omitempty"`
	Findings []Finding        `json:"findings,omitempty"`
}

type QAResultRecord struct {
	CaseID       string `json:"caseId"`
	Mode         string `json:"mode"`
	Outcome      string `json:"outcome"`
	Procedure    string `json:"procedure"`
	Observation  string `json:"observation"`
	OracleResult string `json:"oracleResult"`
	// Origin 标记结果来源：经执行用例记 "executed"；AFFECTED 下未覆盖的已批准
	// 用例记 "inherited"（继承上一轮 PASS）。旧状态缺省（空串）按 executed 处理。
	Origin string `json:"origin,omitempty"`
}

// QAExecutionScope 记录一次 QA 执行重跑的 scope 决策：重跑时用户选择
// FULL（全量重跑该 mode 全部已批准用例）或 AFFECTED（只重跑 host 综合判定的受影响
// 子集，其余已批准用例继承上一轮 PASS）。按 mode 一条、最新决策覆盖；BaseSnapshot 是
// 本次重跑继承来源的权威结果快照；CaseIDs 是 AFFECTED 子集（FULL 为空）；Origin 固定
// 为 USER；Source 区分记录来源（PREPARE / AUTHORIZE_REPAIR / CARRY_FORWARD）。
type QAExecutionScope struct {
	Decision     string   `json:"decision"`     // "FULL" | "AFFECTED"
	Mode         string   `json:"mode"`         // blackbox | whitebox | ""
	BaseSnapshot string   `json:"baseSnapshot"` // 继承来源=上一轮权威结果快照
	CaseIDs      []string `json:"caseIds"`      // AFFECTED 子集
	Reason       string   `json:"reason,omitempty"`
	Origin       string   `json:"origin"` // 固定 "USER"
	Source       string   `json:"source"` // PREPARE | AUTHORIZE_REPAIR | CARRY_FORWARD
}

type Finding struct {
	Severity  string   `json:"severity,omitempty"`
	Message   string   `json:"message"`
	Locations []string `json:"locations,omitempty"`
}

type SkipAuthorization struct {
	Origin   string `json:"origin"`
	Status   string `json:"status"`
	Snapshot string `json:"snapshot,omitempty"`
}

// SnapshotOverride 记录快照黑盒门的手动放行授权。黑盒 qa-review 未 PASS 时用户经
// workflow snapshot --user-requested 显式授权带风险继续；授权延续到后续修复快照（放行
// 后未批准黑盒用例验证状态视为 PASS），直到黑盒 review 真正 PASS 或需求作废重置才清除。
// Snapshot 字段记录授权发放时点的快照（溯源），不用于绑定失效。Origin 固定为 USER。
type SnapshotOverride struct {
	Origin   string `json:"origin"`
	Snapshot string `json:"snapshot"`
	Message  string `json:"message,omitempty"`
}

// SettledFinding 记录用户对 pre-development 审查发现项的一次已拍板处置。确认问题
// （认为是真问题、需修订）或驳回问题（认为不是问题、作废）由 Disposition 区分；
// 确认的 P0/P1 驱动"需重审"标记，驳回的发现项不阻塞。
type SettledFinding struct {
	Message     string `json:"message"`
	Disposition string `json:"disposition"`
}

type GateResult struct {
	Status         string    `json:"status"`
	Snapshot       string    `json:"snapshot,omitempty"`
	SourceSnapshot string    `json:"sourceSnapshot,omitempty"`
	Compared       string    `json:"compared,omitempty"`
	Findings       []Finding `json:"findings,omitempty"`
	Message        string    `json:"message,omitempty"`
	DispatchID     string    `json:"dispatchId,omitempty"`
}

type CarryResult struct {
	Decision       string `json:"decision"`
	Origin         string `json:"origin"`
	SourceSnapshot string `json:"sourceSnapshot"`
	TargetSnapshot string `json:"targetSnapshot"`
	Message        string `json:"message,omitempty"`
}

type QACase struct {
	ID           string `json:"id"`
	Mode         string `json:"mode"`
	Description  string `json:"description"`
	Procedure    string `json:"procedure"`
	Oracle       string `json:"oracle"`
	// Test 是白盒用例对应的测试引用 = "<文件路径>::<函数名>"：文件路径定位到
	// 交付测试代码所在文件、函数名定位到该文件里的测试，两者都是不透明字符串、CLI 不解析
	// 代码内容。用例文档自包含——读文档即知该测试在哪个文件、叫什么。CLI 记录时只校验引用
	// 非空、且同一引用不被两条白盒用例共用（一个测试实现一个用例）；存在性与对应性由
	// qa-review（读代码核对）与 qa-execution（实际运行）验证，使"测 A 的测试给 B 用例标
	// PASS"可被发现。黑盒用例不需要（黑盒执行实际使用产品、无结构测试绑定）。
	Test         string `json:"test,omitempty"`
	ReviewStatus string `json:"reviewStatus"`
}

type RunSummary struct {
	RunID                string                       `json:"runId"`
	Flow                 string                       `json:"flow"`
	Status               string                       `json:"status"`
	RequirementRevision  string                       `json:"requirementRevision"`
	BasePromptRevision   string                       `json:"basePromptRevision"`
	CatalogRevision      string                       `json:"catalogRevision"`
	VCS                  string                       `json:"vcs"`
	BaseSnapshot         string                       `json:"baseSnapshot"`
	CurrentSnapshot      string                       `json:"currentSnapshot"`
	RequirementArtifacts []RequirementArtifact        `json:"requirementArtifacts"`
	Slicing              *Slicing                     `json:"slicing,omitempty"`
	RouteMode            string                       `json:"routeMode"`
	SelectedGates        []string                     `json:"selectedGates"`
	SkipAuthorizations   map[string]SkipAuthorization `json:"skipAuthorizations"`
	CompletedReviewWaves int                          `json:"completedReviewWaves"`
	ExtraReviewWaves     int                          `json:"extraReviewWaves"`
	Gates                map[string]GateResult        `json:"gates"`
	QA                   QAExecutionResult            `json:"qaExecution"`
	Cost                 *cost.RunCost                `json:"cost,omitempty"`
}

func NewRunState(runID, flow, requirementSource, requirementRevision, vcs, baseSnapshot, currentSnapshot, basePromptRevision, catalogRevision string, confirmed bool, gateIDs []string, artifacts []RequirementArtifact) RunState {
	gates := map[string]GateResult{}
	for _, id := range gateIDs {
		gates[id] = GateResult{Status: "PENDING"}
	}
	return RunState{RunID: runID, Flow: flow, Status: "ACTIVE", RequirementSource: requirementSource, RequirementRevision: requirementRevision, RequirementConfirmed: confirmed, RequirementArtifacts: artifacts, BasePromptRevision: basePromptRevision, CatalogRevision: catalogRevision, VCS: vcs, BaseSnapshot: baseSnapshot, CurrentSnapshot: currentSnapshot, SelectedGates: []string{}, SkipAuthorizations: map[string]SkipAuthorization{}, Actions: pendingRequirementActions(), QACasesByMode: map[string][]QACase{}, QAExecutionByMode: map[string]QAExecutionResult{}, ExecutionScopes: map[string]QAExecutionScope{}, PriorQAExecutionByMode: map[string]*QAExecutionResult{}, QAReviewByMode: map[string]ActionResult{}, QADesignByMode: map[string]ActionResult{}, Gates: gates, Carry: map[string]CarryResult{}, Dispatches: map[string]PreparedDispatch{}, NeedsReReview: map[string]string{}, ReReviewDispatch: map[string]string{}, ReviewOverrides: map[string]string{}, ReviewItemsByAction: map[string]map[string]ReviewItem{}, SettledFindings: map[string][]SettledFinding{}}
}

func pendingRequirementActions() map[string]ActionResult {
	// qa-design / qa-review 的权威结果改由按 mode 的 QAReviewByMode / QADesignByMode
	// 承载，不再初始化共享的 Actions["qa-design"] / Actions["qa-review"] 占位。
	return map[string]ActionResult{"requirements-clarification": {Status: "PENDING"}, "product-review": {Status: "PENDING"}, "start-readiness": {Status: "PENDING"}, "development-worker": {Status: "PENDING"}}
}

func RunDir(root, runID string) string {
	return filepath.Join(lifecycle.CleanRoot(root), ".gates", "tmp", runID)
}

func RunStatePath(root, runID string) string {
	return filepath.Join(RunDir(root, runID), "state.json")
}

func SaveRunState(root string, state RunState) error {
	if strings.TrimSpace(state.RunID) == "" {
		return fmt.Errorf("run id is required")
	}
	if state.Status != "ACTIVE" && state.Status != "SEALED" && state.Status != "ABORTED" {
		return fmt.Errorf("invalid run status %q", state.Status)
	}
	if state.Actions == nil {
		state.Actions = map[string]ActionResult{}
	}
	if state.Gates == nil {
		state.Gates = map[string]GateResult{}
	}
	if state.Carry == nil {
		state.Carry = map[string]CarryResult{}
	}
	if state.SkipAuthorizations == nil {
		state.SkipAuthorizations = map[string]SkipAuthorization{}
	}
	if state.Dispatches == nil {
		state.Dispatches = map[string]PreparedDispatch{}
	}
	if state.RequirementArtifacts == nil {
		state.RequirementArtifacts = []RequirementArtifact{}
	}
	if state.SelectedGates == nil {
		state.SelectedGates = []string{}
	}
	if state.QACasesByMode == nil {
		state.QACasesByMode = map[string][]QACase{}
	}
	if state.NeedsReReview == nil {
		state.NeedsReReview = map[string]string{}
	}
	if state.ReReviewDispatch == nil {
		state.ReReviewDispatch = map[string]string{}
	}
	if state.ReviewOverrides == nil {
		state.ReviewOverrides = map[string]string{}
	}
	if state.ReviewItemsByAction == nil {
		state.ReviewItemsByAction = map[string]map[string]ReviewItem{}
	}
	if state.SettledFindings == nil {
		state.SettledFindings = map[string][]SettledFinding{}
	}
	if state.ExecutionScopes == nil {
		state.ExecutionScopes = map[string]QAExecutionScope{}
	}
	if state.QAExecutionByMode == nil {
		state.QAExecutionByMode = map[string]QAExecutionResult{}
	}
	if state.PriorQAExecutionByMode == nil {
		state.PriorQAExecutionByMode = map[string]*QAExecutionResult{}
	}
	if state.QAReviewByMode == nil {
		state.QAReviewByMode = map[string]ActionResult{}
	}
	if state.QADesignByMode == nil {
		state.QADesignByMode = map[string]ActionResult{}
	}
	// 写盘前先置空自身、以 json.MarshalIndent 规范化序列化、对规范化内容计算 sha256，
	// 回填 StateIntegrity 后再写盘。LoadRunState 校验时按同样方式置空重算比对，任何非 CLI
	// 的手工改写都会破坏一致性而硬拒绝。
	state.StateIntegrity = ""
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	state.StateIntegrity = hex.EncodeToString(sum[:])
	finalData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := RunStatePath(root, state.RunID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, append(finalData, '\n'), 0o600)
}

func LoadRunState(root, runID string) (RunState, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return RunState{}, fmt.Errorf("run id is required (--run-id)")
	}
	data, err := os.ReadFile(RunStatePath(root, runID))
	if err != nil {
		if os.IsNotExist(err) {
			return RunState{}, fmt.Errorf("run %q was not found or is already terminated (aborted/sealed); no state file at %s", runID, RunStatePath(root, runID))
		}
		return RunState{}, err
	}
	// 格式校验：读取任何 run 状态文件时，若其结构与当前 CLI 期望的 schema 不符
	// （含旧格式的 Actions["qa-review"]/["qa-design"] 字段缺失、未知的必需字段缺失或字段
	// 类型不匹配），CLI SHALL 返回清晰错误（指出格式不匹配），不得静默降级为空/默认状态
	// 继续执行。两个按 mode 存储的 QA review/design 字段恒在场（新 run 序列化为 {}），文件
	// 缺任一即视为旧格式/格式不符。该校验不窄化为"旧格式"专属：任何 schema 不符均报错。
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return RunState{}, fmt.Errorf("state JSON is invalid: %w", err)
	}
	for _, required := range []string{"qaReviewByMode", "qaDesignByMode"} {
		if _, ok := fields[required]; !ok {
			return RunState{}, fmt.Errorf("run state format does not match the current schema: missing required field %q (a run state without the per-mode QA review/design fields is not supported)", required)
		}
	}
	// 迁移：旧状态文件把用例存成单个 qaCases 数组、执行结果存成单一 qaExecution /
	// priorQAExecution，新模型按 mode 分开存储。加载时把旧数组迁移进合并键 ""，保证续跑/
	// 接手旧 run 不丢用例与执行结果。本 change 对上述两个新字段不做 nil 容忍；既有字段的
	// 迁移逻辑保持不动。
	var holder struct {
		RunState
		LegacyQACases          []QACase           `json:"qaCases"`
		LegacyQAExecution      QAExecutionResult  `json:"qaExecution"`
		LegacyPriorQAExecution *QAExecutionResult `json:"priorQAExecution"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&holder); err != nil {
		return RunState{}, fmt.Errorf("state JSON is invalid or does not match the current schema: %w", err)
	}
	state := holder.RunState
	if state.QACasesByMode == nil {
		state.QACasesByMode = map[string][]QACase{}
		if len(holder.LegacyQACases) != 0 {
			state.QACasesByMode[""] = holder.LegacyQACases
		}
	}
	if state.QAExecutionByMode == nil {
		state.QAExecutionByMode = map[string]QAExecutionResult{}
		if holder.LegacyQAExecution.Status != "" || holder.LegacyQAExecution.Snapshot != "" || len(holder.LegacyQAExecution.Cases) != 0 {
			state.QAExecutionByMode[""] = holder.LegacyQAExecution
		}
	}
	if state.PriorQAExecutionByMode == nil {
		state.PriorQAExecutionByMode = map[string]*QAExecutionResult{}
		if holder.LegacyPriorQAExecution != nil {
			state.PriorQAExecutionByMode[""] = holder.LegacyPriorQAExecution
		}
	}
	if state.RunID != runID {
		return RunState{}, fmt.Errorf("state run id does not match %q", runID)
	}
	// 完整性校验。非空 StateIntegrity（新格式状态文件）时置空后按与 SaveRunState
	// 相同的 json.MarshalIndent 规范化重算 sha256 比对；不匹配即硬拒绝，任何人（含 host/
	// 主代理）手工改写 run 状态都会被检测。旧状态文件无本字段，跳过校验。校验通过后把
	// 字段值复原，使内存中的状态与文件一致（随 Seal 保留）。
	if state.StateIntegrity != "" {
		stored := state.StateIntegrity
		state.StateIntegrity = ""
		recomputed, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return RunState{}, err
		}
		sum := sha256.Sum256(recomputed)
		if hex.EncodeToString(sum[:]) != stored {
			return RunState{}, fmt.Errorf("run state integrity check failed: state was modified outside the CLI")
		}
		state.StateIntegrity = stored
	}
	return state, nil
}

// qaCases returns the QA case list stored for the dispatch mode's own key
// the merged (empty) mode holds the merged/single-dispatch set, and
// concrete modes hold their own per-mode sets. Returns nil when that mode has no
// stored list. Reads here return the map's backing slice; use setQACases to write.
func (state RunState) qaCases(mode string) []QACase {
	if state.QACasesByMode == nil {
		return nil
	}
	return state.QACasesByMode[mode]
}

// setQACases replaces the QA case list for the dispatch mode only, leaving every
// other mode's cases (and their review status and recorded execution results)
// untouched.
func (state *RunState) setQACases(mode string, cases []QACase) {
	if state.QACasesByMode == nil {
		state.QACasesByMode = map[string][]QACase{}
	}
	state.QACasesByMode[mode] = cases
}

// qaExecution returns the QA execution result stored for the dispatch mode
// (full decoupling): blackbox/whitebox each hold their own result, and the
// merged empty mode holds the merged/single-dispatch result. A missing result
// reads as PENDING (zero value).
func (state RunState) qaExecution(mode string) QAExecutionResult {
	if state.QAExecutionByMode == nil {
		return QAExecutionResult{}
	}
	return state.QAExecutionByMode[mode]
}

// setQAExecution replaces the QA execution result for the dispatch mode only,
// leaving every other mode's result untouched (full decoupling).
func (state *RunState) setQAExecution(mode string, result QAExecutionResult) {
	if state.QAExecutionByMode == nil {
		state.QAExecutionByMode = map[string]QAExecutionResult{}
	}
	state.QAExecutionByMode[mode] = result
}

// deleteQAExecution clears the dispatch mode's execution result (used on
// requirement invalidation / snapshot rebinding).
func (state *RunState) deleteQAExecution(mode string) {
	if state.QAExecutionByMode != nil {
		delete(state.QAExecutionByMode, mode)
	}
}

// priorQAExecution returns the dispatch mode's preserved prior authoritative
// execution result, or nil when the mode has none (full decoupling).
func (state RunState) priorQAExecution(mode string) *QAExecutionResult {
	if state.PriorQAExecutionByMode == nil {
		return nil
	}
	return state.PriorQAExecutionByMode[mode]
}

// setPriorQAExecution preserves the dispatch mode's prior authoritative execution
// result, leaving every other mode's prior untouched (full decoupling).
func (state *RunState) setPriorQAExecution(mode string, result QAExecutionResult) {
	if state.PriorQAExecutionByMode == nil {
		state.PriorQAExecutionByMode = map[string]*QAExecutionResult{}
	}
	prior := result
	state.PriorQAExecutionByMode[mode] = &prior
}

// deletePriorQAExecution clears the dispatch mode's preserved prior authoritative
// execution result only (an authoritative result record replaces its own mode's
// prior; other modes' priors stay).
func (state *RunState) deletePriorQAExecution(mode string) {
	if state.PriorQAExecutionByMode != nil {
		delete(state.PriorQAExecutionByMode, mode)
	}
}

// qaExecutionModes returns the dispatch modes that hold a stored QA execution
// result, including the merged "" key when present, in stable order.
func (state RunState) qaExecutionModes() []string {
	if state.QAExecutionByMode == nil {
		return nil
	}
	modes := make([]string, 0, len(state.QAExecutionByMode))
	for mode := range state.QAExecutionByMode {
		modes = append(modes, mode)
	}
	sort.Strings(modes)
	return modes
}

// allQACases returns the run's complete QA case set across every storage layout
// used for merged-set operations and overall checks. It applies
// per-mode precedence per mode: a mode's own per-mode key, when non-empty,
// replaces that mode's cases in the merged "" key (which then only holds
// legacy-migrated or fast-path merged cases that a per-mode redesign superseded),
// so stale merged cases are never double-counted. A merged-only run (no per-mode
// key) keeps the "" key's cases in their original order.
func (state RunState) allQACases() []QACase {
	var all []QACase
	for _, testCase := range state.qaCases("") {
		if testCase.Mode == "blackbox" || testCase.Mode == "whitebox" {
			if len(state.qaCases(testCase.Mode)) != 0 {
				continue // 该 mode 已被 per-mode 存储取代
			}
		}
		all = append(all, testCase)
	}
	for _, mode := range []string{"blackbox", "whitebox"} {
		all = append(all, state.qaCases(mode)...)
	}
	return all
}

// qaModeCases returns the dispatch mode's QA cases across every storage layout
// Per-mode precedence per mode (fix): a concrete mode's own per-mode
// key is authoritative — when it is non-empty, exactly those cases are returned
// and any cases migrated into the merged "" key are ignored (a run that designed
// per-mode after a legacy migration would otherwise double-count and see stale
// PENDING cases, e.g. blocking blackboxReviewPassed). Only when the per-mode key
// is empty do reads fall back to the merged "" key (legacy / fast-path merged
// storage). The empty mode is an alias for allQACases — the merged/single-dispatch
// view is exactly the full current set across every storage layout (per-mode
// precedence), so the "" branch delegates to it instead of duplicating the merge.
// Returns a copy for concrete modes; use setQACases to mutate a mode's stored list.
func (state RunState) qaModeCases(mode string) []QACase {
	if mode != "" {
		if perMode := state.qaCases(mode); len(perMode) != 0 {
			return append([]QACase{}, perMode...)
		}
		return filterQACasesByMode(state.qaCases(""), mode)
	}
	return state.allQACases()
}

// qaModeCasesWithKey returns the dispatch mode's QA cases using the same read
// view the review prompt assembly uses (qaModeCases: a concrete mode's own
// per-mode key when non-empty, else the merged "" fallback; empty mode = the full
// current set), together with the storage key those cases were read from. A review
// records its decisions with setQACasesForReview against that key, so the recorder
// and the prompt always see the same pending set (alignment).
func (state RunState) qaModeCasesWithKey(mode string) (string, []QACase) {
	if mode != "" {
		if perMode := state.qaCases(mode); len(perMode) != 0 {
			return mode, append([]QACase{}, perMode...)
		}
		return "", filterQACasesByMode(state.qaCases(""), mode)
	}
	return "", state.allQACases()
}

// setQACasesForReview persists a review's decided case set back into the storage
// key it was read from (see qaModeCasesWithKey). When a concrete mode's cases were
// read from the merged "" fallback, only that mode's cases are replaced there,
// preserving the other modes' cases in the "" key. The empty mode replaces
// the whole merged list (the single-dispatch flow).
func (state *RunState) setQACasesForReview(mode, key string, updated []QACase) {
	if key == "" && mode != "" {
		replaced := map[string]bool{}
		for _, testCase := range updated {
			replaced[testCase.ID] = true
		}
		merged := make([]QACase, 0, len(state.qaCases(""))+len(updated))
		for _, existing := range state.qaCases("") {
			if replaced[existing.ID] {
				continue
			}
			merged = append(merged, existing)
		}
		merged = append(merged, updated...)
		state.setQACases("", merged)
		return
	}
	state.setQACases(key, updated)
}

// qaReview returns the authoritative qa-review result for the dispatch mode
// (完完全全解耦): the mode's own per-mode key when it holds a recorded
// result, else the merged "" key (single-dispatch / legacy merged storage). An
// empty mode reads the merged key directly. Reads here mirror the recorder's
// write key (setQAReview), so the two always see the same stored result.
func (state RunState) qaReview(mode string) ActionResult {
	if state.QAReviewByMode == nil {
		return ActionResult{}
	}
	if mode != "" {
		if result, ok := state.QAReviewByMode[mode]; ok && result.Status != "" {
			return result
		}
	}
	return state.QAReviewByMode[""]
}

// setQAReview records the authoritative qa-review result for the dispatch mode
// only, leaving every other mode's review result untouched. The empty
// mode is the merged/single-dispatch flow.
func (state *RunState) setQAReview(mode string, result ActionResult) {
	if state.QAReviewByMode == nil {
		state.QAReviewByMode = map[string]ActionResult{}
	}
	state.QAReviewByMode[mode] = result
}

// qaDesign returns the authoritative qa-design result for the dispatch mode
// (完完全全解耦); read semantics mirror qaReview (per-mode key first,
// merged "" fallback).
func (state RunState) qaDesign(mode string) ActionResult {
	if state.QADesignByMode == nil {
		return ActionResult{}
	}
	if mode != "" {
		if result, ok := state.QADesignByMode[mode]; ok && result.Status != "" {
			return result
		}
	}
	return state.QADesignByMode[""]
}

// setQADesign records the authoritative qa-design result for the dispatch mode
// only, leaving every other mode's design result untouched.
func (state *RunState) setQADesign(mode string, result ActionResult) {
	if state.QADesignByMode == nil {
		state.QADesignByMode = map[string]ActionResult{}
	}
	state.QADesignByMode[mode] = result
}

func DeleteRun(root, runID string) error {
	return os.RemoveAll(RunDir(root, runID))
}

func SaveRunSummary(root string, state RunState) error {
	summary := runSummary(state)
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(lifecycle.CleanRoot(root), ".gates", "results")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeAtomic(RunSummaryPath(root, state.RunID), append(data, '\n'), 0o600)
}

func RunSummaryPath(root, runID string) string {
	return filepath.Join(lifecycle.CleanRoot(root), ".gates", "results", runID+".json")
}

// SliceCostRecord persists a slice instance's cost projection for its
// retained-overall master to fold into the master's final seal ledger. A
// slice seals without writing its own ledger file (封板文件); this cost
// sidecar lives under the master's temp run dir (gitignored, so it never
// enters a delivery diff) and the master's seal consumes and removes it
// after folding the numbers in.
type SliceCostRecord struct {
	RunID string        `json:"runId"`
	Cost  *cost.RunCost `json:"cost,omitempty"`
}

// SliceCostDir returns the directory a slice instance writes its cost sidecar
// into, scoped under the retained-overall master's temp run dir so the master
// scans only its own slices and the sidecars are auto-cleaned with the master
// run. .gates/tmp is gitignored, so slice cost sidecars are never staged into
// a delivery commit.
func SliceCostDir(root, masterRunID string) string {
	return filepath.Join(RunDir(root, masterRunID), "slice-costs")
}

// SliceCostPath returns the sidecar path for one sealed slice instance under
// its retained-overall master's temp run dir.
func SliceCostPath(root, masterRunID, sliceRunID string) string {
	return filepath.Join(SliceCostDir(root, masterRunID), sliceRunID+".cost.json")
}

// SaveSliceCost atomically writes a slice instance's cost sidecar under its
// retained-overall master's temp run dir.
func SaveSliceCost(root, masterRunID string, record SliceCostRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(SliceCostDir(root, masterRunID), 0o700); err != nil {
		return err
	}
	return writeAtomic(SliceCostPath(root, masterRunID, record.RunID), append(data, '\n'), 0o600)
}

func runSummary(state RunState) RunSummary {
	return RunSummary{RunID: state.RunID, Flow: state.Flow, Status: state.Status, RequirementRevision: state.RequirementRevision, RequirementArtifacts: state.RequirementArtifacts, Slicing: state.Slicing, BasePromptRevision: state.BasePromptRevision, CatalogRevision: state.CatalogRevision, VCS: state.VCS, BaseSnapshot: state.BaseSnapshot, CurrentSnapshot: state.CurrentSnapshot, RouteMode: state.RouteMode, SelectedGates: state.SelectedGates, SkipAuthorizations: state.SkipAuthorizations, CompletedReviewWaves: state.CompletedReviewWaves, ExtraReviewWaves: state.ExtraReviewWaves, Gates: state.Gates, QA: qaOverallResult(state), Cost: state.Cost}
}

// RequirementRevision 计算需求工件的内容哈希。先规范化行尾（CRLF→LF）再哈希：
// Windows 上 git 检出会按 core.autocrlf 把工作树行尾转成 CRLF，隔离工作区与主工作
// 区若行尾不同，注入哈希校验会误报不匹配。规范化后哈希跨平台稳定。
func RequirementRevision(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:]), nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceCompletedFile(tmpName, path)
}
