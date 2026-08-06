package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/cost"
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
	RetainedOverall      bool                         `json:"retainedOverall,omitempty"`
	PreRepairSnapshot    string                       `json:"preRepairSnapshot,omitempty"`
	Slicing              *Slicing                     `json:"slicing,omitempty"`
	SettledFindings      map[string][]SettledFinding  `json:"settledFindings,omitempty"`
	RouteMode            string                       `json:"routeMode,omitempty"`
	SelectedGates        []string                     `json:"selectedGates"`
	SkipAuthorizations   map[string]SkipAuthorization `json:"skipAuthorizations"`
	CompletedReviewWaves int                          `json:"completedReviewWaves"`
	ExtraReviewWaves     int                          `json:"extraReviewWaves"`
	Actions              map[string]ActionResult      `json:"actions"`
	QACases              []QACase                     `json:"qaCases"`
	QAExecution          QAExecutionResult            `json:"qaExecution"`
	Gates                map[string]GateResult        `json:"gates"`
	Carry                map[string]CarryResult       `json:"carry"`
	Dispatches           map[string]PreparedDispatch  `json:"dispatches"`
	Cost                 *cost.RunCost                `json:"cost,omitempty"`
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
	return RunState{RunID: runID, Flow: flow, Status: "ACTIVE", RequirementSource: requirementSource, RequirementRevision: requirementRevision, RequirementConfirmed: confirmed, RequirementArtifacts: artifacts, BasePromptRevision: basePromptRevision, CatalogRevision: catalogRevision, VCS: vcs, BaseSnapshot: baseSnapshot, CurrentSnapshot: currentSnapshot, SelectedGates: []string{}, SkipAuthorizations: map[string]SkipAuthorization{}, Actions: pendingRequirementActions(), QACases: []QACase{}, QAExecution: QAExecutionResult{Status: "PENDING"}, Gates: gates, Carry: map[string]CarryResult{}, Dispatches: map[string]PreparedDispatch{}, NeedsReReview: map[string]string{}, ReReviewDispatch: map[string]string{}, ReviewOverrides: map[string]string{}, SettledFindings: map[string][]SettledFinding{}}
}

func pendingRequirementActions() map[string]ActionResult {
	return map[string]ActionResult{"requirements-clarification": {Status: "PENDING"}, "product-review": {Status: "PENDING"}, "start-readiness": {Status: "PENDING"}, "qa-design": {Status: "PENDING"}, "qa-review": {Status: "PENDING"}, "development-worker": {Status: "PENDING"}}
}

func RunDir(root, runID string) string {
	return filepath.Join(cleanRoot(root), ".gates", "tmp", runID)
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
	if state.QACases == nil {
		state.QACases = []QACase{}
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
	if state.SettledFindings == nil {
		state.SettledFindings = map[string][]SettledFinding{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := RunStatePath(root, state.RunID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}

func LoadRunState(root, runID string) (RunState, error) {
	data, err := os.ReadFile(RunStatePath(root, runID))
	if err != nil {
		return RunState{}, err
	}
	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return RunState{}, fmt.Errorf("state JSON is invalid: %w", err)
	}
	if state.RunID != runID {
		return RunState{}, fmt.Errorf("state run id does not match %q", runID)
	}
	return state, nil
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
	dir := filepath.Join(cleanRoot(root), ".gates", "results")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeAtomic(RunSummaryPath(root, state.RunID), append(data, '\n'), 0o600)
}

func RunSummaryPath(root, runID string) string {
	return filepath.Join(cleanRoot(root), ".gates", "results", runID+".json")
}

func runSummary(state RunState) RunSummary {
	return RunSummary{RunID: state.RunID, Flow: state.Flow, Status: state.Status, RequirementRevision: state.RequirementRevision, RequirementArtifacts: state.RequirementArtifacts, Slicing: state.Slicing, BasePromptRevision: state.BasePromptRevision, CatalogRevision: state.CatalogRevision, VCS: state.VCS, BaseSnapshot: state.BaseSnapshot, CurrentSnapshot: state.CurrentSnapshot, RouteMode: state.RouteMode, SelectedGates: state.SelectedGates, SkipAuthorizations: state.SkipAuthorizations, CompletedReviewWaves: state.CompletedReviewWaves, ExtraReviewWaves: state.ExtraReviewWaves, Gates: state.Gates, QA: state.QAExecution, Cost: state.Cost}
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
