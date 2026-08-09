package validate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type StartOptions struct {
	Root, PackageRoot, RunID, Flow, RequirementSource, VCS, BaseSnapshot string
	// CurrentSnapshot 显式指定当前快照停在某祖先（RQ-010）：默认不传时取原生 HEAD；
	// 传入值必须是原生 HEAD 的祖先或相等，用于接手"开发已提交"的 run 时让 current 停在
	// 开发前、已有开发提交作为待登记快照。
	CurrentSnapshot      string
	RequirementArtifacts []string
	RequirementConfirmed, RetainedOverall bool
}

type FindingInput struct {
	Severity  string
	Message   string
	Locations []string
}

// QACaseInput 是一次 qa-design 记录轮输入的用例。Test（RQ-013）是白盒用例对应的测试
// 引用 = "<文件路径>::<函数名>"：文件路径定位到交付测试代码所在文件、函数名定位到该文件
// 里的测试，两者都是不透明字符串、CLI 不解析代码内容。白盒设计者独立编写结构测试代码，
// 并在 CLI 记录用例时用 --test 指定；CLI 记录时只校验引用非空、且同一引用不被两条白盒
// 用例共用（一个测试实现一个用例），存在性与对应性由 qa-review（读代码核对）与
// qa-execution（实际运行）验证，不满足即拒绝记录。黑盒用例不需要 Test。
type QACaseInput struct{ Mode, Description, Procedure, Oracle, Test string }

// ReviewItemInput 是 record-action 对 product-review / start-readiness 逐项下发的
// 需求项判定（RQ-012 增量审查）。Key 是 prepare-action --scope 声明的需求项键；Status 为
// PASS | FAIL；Reason 是 FAIL 项必须携带的 finding。
type ReviewItemInput struct{ Key, Status, Reason string }

type QAReviewInput struct{ CaseID, Outcome, Reason string }

type QAResultInput struct{ CaseID, Outcome, Procedure, Observation, OracleResult string }

// QAScopeInput carries one QA execution rerun scope decision bundled into an
// authorize-repair call (RQ-007): Mode is the dispatch mode (blackbox / whitebox /
// empty for the merged set), Decision is FULL or AFFECTED, CaseIDs is the host
// judged AFFECTED subset, and Reason traces the decision.
type QAScopeInput struct {
	Mode     string
	Decision string
	CaseIDs  []string
	Reason   string
}

type CarryInput struct{ GateID, Decision, Message string }

const (
	carryOriginIndependent  = "INDEPENDENT"
	carryOriginMainShortcut = "MAIN_SHORTCUT"
)

// carryAdoptKey is the reserved Carry map key recording an adopted external VCS
// change: Decision ADOPT with Origin ADOPT, SourceSnapshot the pre-adoption
// snapshot, TargetSnapshot the adopted native head, and Message the main-agent
// reason. Gate ids cannot collide with it because they match promptIDPattern.
const carryAdoptKey = "__adopt__"

const formalFlow = "formal"
const automaticReviewWaveLimit = 3

// QA execution scope source markers (RQ-003/007): PREPARE for the standalone
// workflow qa-execution-scope command, AUTHORIZE_REPAIR for a scope recorded inline
// by an authorize-repair call at the review-wave limit, and CARRY_FORWARD for a
// scope auto-carried at the limit from a prior user AFFECTED choice without asking
// again.
const (
	scopeSourcePrepare         = "PREPARE"
	scopeSourceAuthorizeRepair = "AUTHORIZE_REPAIR"
	scopeSourceCarryForward    = "CARRY_FORWARD"
)

const (
	developmentPending        = "PENDING"
	developmentPrepared       = "PREPARED"
	developmentRepairPrepared = "REPAIR_PREPARED"
	developmentComplete       = "PASS"
	developmentVerified       = "VERIFIED"
)

var routeModes = map[string]bool{"full": true, "custom": true}

// isQAMode reports whether the id is a built-in QA mode entry. Blackbox, whitebox,
// and merge QA share the run's QA case set and QA Execution result; discovered
// gate entries live in the per-gate result map instead. The legacy "qa" id is
// recognized so runs bound to an old catalog that listed QA as a gate still count
// as QA-selected after migration and share the QA Execution result.
func isQAMode(id string) bool {
	return id == blackboxQAID || id == whiteboxQAID || id == mergeQAID || id == legacyQAID
}

// isSelectedQA reports whether any QA mode is selected for this run.
func isSelectedQA(state RunState) bool {
	for id := range selectedSet(state) {
		if isQAMode(id) {
			return true
		}
	}
	return false
}

// isMergeVerification reports whether this run is a retained overall run whose
// split decision forces merge gate and merge QA as its only post-merge
// verification. The route is auto-determined as the merge route when the split
// decision is recorded, so this run does not go through normal route selection.
func isMergeVerification(state RunState) bool {
	return state.RetainedOverall && slicingRecorded(state) && state.Slicing.Decision == "split"
}

// slicingRecorded reports whether the run's formal split decision has been
// recorded. The decision is the binding point: once recorded it is not re-cut.
func slicingRecorded(state RunState) bool {
	return state.Slicing != nil && strings.TrimSpace(state.Slicing.Decision) != ""
}

// selectedQAModes lists the normal QA modes selected for the run, used to tell
// the QA Design agent which modes' cases it must design.
func selectedQAModes(state RunState) []string {
	var modes []string
	if isSelected(state, blackboxQAID) {
		modes = append(modes, "blackbox: real QA designed from the current confirmed requirement in the QA isolation worktree, executed against the built product on the main worktree after the snapshot")
	}
	if isSelected(state, whiteboxQAID) {
		modes = append(modes, "whitebox: structure test cases designed by reading the implementation after development")
	}
	return modes
}

// blackboxReviewPassed reports whether the blackbox review gate is satisfied for
// a snapshot: when blackbox QA is selected, the blackbox review must have
// approved the set, and the blackbox review result itself must not have failed.
// With blackbox cases present, every blackbox case must be approved (ReviewStatus
// PASS) by a blackbox review round AND the blackbox qa-review result must not be
// FAIL: a review round can judge every pending case PASS yet still fail the set
// with a coverage-omission finding (qa-review FAIL, qa-design re-opened), and
// that FAIL must block the snapshot just like a case-level FAIL. With zero
// blackbox cases the gate is NOT vacuous: the snapshot is allowed only when the
// blackbox qa-review result recorded PASS (the review judged the empty set's
// coverage sufficient, requirement 4) — a still-pending or FAIL review keeps
// blocking the snapshot (requirements 1 and 2: snapshot waits until development
// complete 且 blackbox review PASS). Runs without blackbox QA are vacuously
// passed. The only bypass is the user-authorized snapshot release
// (snapshotBlackboxReleased), handled by AdvanceSnapshot. RQ-001：黑盒门的
// review 结果按 blackbox mode 独立读取，不读取另一 mode 的 review 判定。
func blackboxReviewPassed(state RunState) bool {
	if !isSelected(state, blackboxQAID) && !isSelected(state, legacyQAID) {
		return true
	}
	hasBlackboxCases := false
	// RQ-012：黑盒用例按 mode 分开存储；从全量跨 mode 视图按 mode 过滤读取，兼容合并
	// 存储（用例在 "" 键）与按 mode 存储两种布局。
	for _, testCase := range state.qaModeCases("blackbox") {
		hasBlackboxCases = true
		if testCase.ReviewStatus != "PASS" {
			return false
		}
	}
	if !hasBlackboxCases {
		// RQ-001：黑盒门读黑盒 mode 的 review 权威结果；legacy "qa" 合并态经回退取 "" 键。
		return state.qaReview("blackbox").Status == "PASS"
	}
	// 有黑盒用例时整个审查判定仍须通过：集合层面 P1 覆盖遗漏使审查动作 FAIL 时，即使
	// 各用例单独均 PASS 也阻挡快照；唯一绕过是用户授权放行（AdvanceSnapshot 处理）。
	if state.qaReview("blackbox").Status == "FAIL" {
		return false
	}
	return true
}

// snapshotBlackboxReleased reports whether the user has explicitly authorized a
// snapshot while the blackbox review gate is unmet. The authorization persists
// for the rest of the run (until a requirement invalidation clears it): once the
// user releases the gate, the unapproved blackbox cases are treated as PASS, so
// subsequent repair snapshots are not blocked again by the same unapproved cases.
func snapshotBlackboxReleased(state RunState) bool {
	return state.SnapshotOverride != nil
}

func Start(options StartOptions) (RunState, error) {
	root := cleanRoot(options.Root)
	for name, value := range map[string]string{"flow": options.Flow, "requirement": options.RequirementSource, "VCS": options.VCS} {
		if strings.TrimSpace(value) == "" {
			return RunState{}, fmt.Errorf("%s is required", name)
		}
	}
	if strings.TrimSpace(options.Flow) != formalFlow {
		return RunState{}, fmt.Errorf("flow must be formal")
	}
	if options.RequirementConfirmed {
		return RunState{}, fmt.Errorf("a run cannot start with a pre-confirmed requirement; record Requirements Clarification first")
	}
	vcs := strings.ToLower(strings.TrimSpace(options.VCS))
	resolver, err := resolverForVCS(vcs, nil)
	if err != nil {
		return RunState{}, err
	}
	nativeHead, err := resolver.Resolve(root)
	if err != nil {
		return RunState{}, err
	}
	currentSnapshot := nativeHead
	// RQ-010：显式指定当前快照停在某祖先（默认不传时仍取 HEAD）。传入值必须是原生 HEAD
	// 的祖先或相等；用于接手"开发已提交"的 run 时让 current 停在开发前、已有开发提交作为
	// 待登记快照。
	if supplied := strings.TrimSpace(options.CurrentSnapshot); supplied != "" {
		if err := resolver.Verify(root, supplied); err != nil {
			return RunState{}, err
		}
		if err := resolver.IsAncestorOrEqual(root, supplied, nativeHead); err != nil {
			return RunState{}, fmt.Errorf("current snapshot %s is not an ancestor or equal of the native head: %w", supplied, err)
		}
		currentSnapshot = strings.ToLower(supplied)
	}
	baseSnapshot := currentSnapshot
	if supplied := strings.TrimSpace(options.BaseSnapshot); supplied != "" {
		if err := resolver.Verify(root, supplied); err != nil {
			return RunState{}, err
		}
		if err := resolver.IsAncestorOrEqual(root, supplied, currentSnapshot); err != nil {
			return RunState{}, err
		}
		baseSnapshot = strings.ToLower(supplied)
	}
	catalog, err := LoadPromptCatalog(options.PackageRoot)
	if err != nil {
		return RunState{}, err
	}
	artifacts, err := requirementArtifactSet(root, options.RequirementSource, options.RequirementArtifacts)
	if err != nil {
		return RunState{}, err
	}
	revision := artifactRevision(artifacts, normalizeArtifactPath(root, options.RequirementSource))
	runID := strings.TrimSpace(options.RunID)
	if runID == "" {
		runID, err = newRunID()
		if err != nil {
			return RunState{}, err
		}
	}
	if !promptIDPattern.MatchString(runID) {
		return RunState{}, fmt.Errorf("run id must match [a-z0-9]+(?:-[a-z0-9]+)*")
	}
	if _, err := os.Stat(RunDir(root, runID)); err == nil {
		return RunState{}, fmt.Errorf("run %q already exists", runID)
	} else if !os.IsNotExist(err) {
		return RunState{}, err
	}
	if _, err := os.Stat(RunSummaryPath(root, runID)); err == nil {
		return RunState{}, fmt.Errorf("run %q already has a retained result", runID)
	} else if !os.IsNotExist(err) {
		return RunState{}, err
	}
	if err := os.MkdirAll(filepath.Dir(RunDir(root, runID)), 0o700); err != nil {
		return RunState{}, err
	}
	if err := os.Mkdir(RunDir(root, runID), 0o700); err != nil {
		return RunState{}, fmt.Errorf("cannot create run %q: %w", runID, err)
	}
	if err := workflowLifecycle.Begin(root, runID); err != nil {
		_ = os.RemoveAll(RunDir(root, runID))
		return RunState{}, err
	}
	state := NewRunState(runID, strings.TrimSpace(options.Flow), normalizeArtifactPath(root, options.RequirementSource), revision, vcs, baseSnapshot, currentSnapshot, catalog.BaseRevision, catalog.CatalogRevision, options.RequirementConfirmed, catalog.GateIDs(), artifacts)
	state.PromptHashes = catalogPromptHashes(catalog)
	state.RetainedOverall = options.RetainedOverall
	if err := SaveRunState(root, state); err != nil {
		_ = os.RemoveAll(RunDir(root, runID))
		return RunState{}, err
	}
	return state, nil
}

// ResumeStatus is the recoverable classification reported when resuming an
// interrupted run: requirement edits need classification, catalog changes are
// reported per gate/action, a drifted native snapshot must be adopted, and a
// registered QA isolation worktree that no longer sits at the base snapshot must
// be confirmed or rebuilt by the user.
type ResumeStatus struct {
	ClassificationRequired bool     `json:"classificationRequired"`
	CatalogDelta           []string `json:"catalogDelta,omitempty"`
	NativeDrifted          bool     `json:"nativeDrifted"`
	IsolationDrifted       bool     `json:"isolationDrifted"`
}

// ResumeReport classifies everything the main agent must judge before the run
// can continue without hard failure.
func ResumeReport(root, packageRoot, runID string) (ResumeStatus, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return ResumeStatus{}, err
	}
	if err := requireActive(state); err != nil {
		return ResumeStatus{}, err
	}
	catalog, err := LoadPromptCatalog(packageRoot)
	if err != nil {
		return ResumeStatus{}, err
	}
	changed, err := requirementArtifactsChanged(root, state.RequirementArtifacts)
	if err != nil {
		return ResumeStatus{}, err
	}
	native, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return ResumeStatus{}, err
	}
	isolationDrifted := false
	if strings.TrimSpace(state.QAWorktree) != "" {
		// 中断续跑时重校验隔离工作区原生标识 == 基线（工作区应停在基线，未漂移即正常；
		// 仅真实漂移才需用户确认/重建）。
		resolver, err := resolverForVCS(state.VCS, nil)
		if err != nil {
			return ResumeStatus{}, err
		}
		resolved, err := resolver.Resolve(cleanWorktree(state.QAWorktree))
		if err != nil {
			return ResumeStatus{}, fmt.Errorf("cannot re-verify QA isolation worktree: %w", err)
		}
		isolationDrifted = !strings.EqualFold(resolved, state.BaseSnapshot)
	}
	return ResumeStatus{ClassificationRequired: changed, CatalogDelta: catalogDelta(state, catalog), NativeDrifted: native != state.CurrentSnapshot, IsolationDrifted: isolationDrifted}, nil
}

// AdoptExternalChange explicitly rebinds the current snapshot to the native
// head after the workspace drifted outside the run. When a development snapshot
// already exists, the previous snapshot becomes the pre-repair boundary so
// unaffected prior PASS results stay eligible for a Carry inheritance decision,
// and the review surface is reset. When the run has not yet produced a
// development snapshot (dev status is PENDING/PREPARED), there is nothing to
// inherit, so the adoption only rebinds the current snapshot and records the
// provenance under the reserved Carry key: no PreRepairSnapshot is set and the
// review surface is not reset. The main agent's reason is recorded as the
// adoption provenance under the reserved Carry key.
func AdoptExternalChange(root, packageRoot, runID, reason string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		current, err := resolveNativeSnapshot(root, state.VCS)
		if err != nil {
			return err
		}
		if current == state.CurrentSnapshot {
			return fmt.Errorf("native current snapshot already matches the run current snapshot")
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return fmt.Errorf("adopting an external change requires a reason")
		}
		oldSnapshot := state.CurrentSnapshot
		state.CurrentSnapshot = current
		// 采纳使派发源快照失效：既有 OPEN/CLAIMED 派发一律标 STALE。
		staleAllDispatches(state)
		if preDevelopment(*state) {
			// 尚无开发快照：不设置 PreRepairSnapshot、不重置审查面，后续开发不会
			// 被 "the current repair still requires verification" 挡住。
		} else {
			state.PreRepairSnapshot = oldSnapshot
			resetSnapshotReviewSurface(state, oldSnapshot, true, true)
		}
		state.Carry[carryAdoptKey] = CarryResult{Decision: "ADOPT", Origin: "ADOPT", SourceSnapshot: oldSnapshot, TargetSnapshot: current, Message: reason}
		return nil
	})
}

// preDevelopment reports whether the run has not yet produced a development
// snapshot: the development action status is PENDING or PREPARED. REPAIR_PREPARED
// sits behind an existing development snapshot and must not be treated as
// pre-development, so the predicate is not !hasDevelopmentSnapshot.
func preDevelopment(state RunState) bool {
	status := state.Actions["development-worker"].Status
	return status == developmentPending || status == developmentPrepared
}

func staleAllDispatches(state *RunState) {
	for id, dispatch := range state.Dispatches {
		if dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED" {
			dispatch.Status = "STALE"
			state.Dispatches[id] = dispatch
		}
	}
}

func UpdateRequirement(root, packageRoot, runID, source string, confirmed bool, semanticEffect string, artifactPaths []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentCatalog(*state, packageRoot)
		if err != nil {
			return err
		}
		oldSource := state.RequirementSource
		if strings.TrimSpace(source) == "" {
			source = state.RequirementSource
		}
		source = normalizeArtifactPath(root, source)
		additional := artifactPaths
		if additional == nil {
			for _, artifact := range state.RequirementArtifacts {
				if artifact.Path != oldSource && artifact.Path != source {
					additional = append(additional, artifact.Path)
				}
			}
		}
		artifacts, err := requirementArtifactSet(cleanRoot(root), source, additional)
		if err != nil {
			return err
		}
		revision := artifactRevision(artifacts, source)
		changed := revision != state.RequirementRevision || source != state.RequirementSource || !sameArtifactSet(artifacts, state.RequirementArtifacts)
		semanticEffect = strings.ToLower(strings.TrimSpace(semanticEffect))
		if changed {
			if semanticEffect != "preserved" && semanticEffect != "changed" {
				return fmt.Errorf("changed requirement requires semantic effect preserved or changed")
			}
			liveSnapshot, err := resolveNativeSnapshot(root, state.VCS)
			if err != nil {
				return err
			}
			if developmentStarted(*state) && semanticEffect == "preserved" && !confirmed {
				return fmt.Errorf("meaning-preserved requirement rebinding after development starts requires user confirmation")
			}
			state.RequirementSource, state.RequirementRevision, state.RequirementArtifacts = source, revision, artifacts
			if semanticEffect == "preserved" {
				if !state.RequirementConfirmed {
					return fmt.Errorf("meaning can be preserved only for a previously confirmed requirement")
				}
				rebindCurrentSnapshot(state, liveSnapshot)
				return nil
			}
			state.CurrentSnapshot = liveSnapshot
			invalidateRequirementResults(state, catalog.GateIDs())
			state.RequirementConfirmed = false
			if confirmed {
				return fmt.Errorf("a meaning-changing requirement must return to Requirements Clarification")
			}
			return nil
		}
		if semanticEffect != "" {
			return fmt.Errorf("semantic effect is accepted only when the requirement revision changed")
		}
		if confirmed && state.Actions["requirements-clarification"].Status != "PASS" {
			return fmt.Errorf("Requirements Clarification must pass before requirement confirmation")
		}
		state.RequirementConfirmed = confirmed
		return nil
	})
}

func RouteCandidates(root, packageRoot, runID string) ([]string, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return nil, err
	}
	catalog, err := requireCurrentDefinitions(root, state, packageRoot)
	if err != nil {
		return nil, err
	}
	if !state.RequirementConfirmed {
		return nil, fmt.Errorf("the current requirement is not confirmed")
	}
	return catalog.RouteCandidates(), nil
}

func SetRoute(root, packageRoot, runID, mode string, selected []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireTransition(*state, "route", ""); err != nil {
			return err
		}
		mode = strings.ToLower(strings.TrimSpace(mode))
		if !routeModes[mode] {
			return fmt.Errorf("route mode must be full or custom")
		}
		candidates := catalog.RouteCandidates()
		if mode == "full" {
			if len(selected) != 0 {
				return fmt.Errorf("full route selects the complete discovered list without --gate")
			}
			selected = candidates
		} else {
			var err error
			selected, err = normalizeSelected(selected, candidates)
			if err != nil {
				return err
			}
			if len(selected) == 0 || len(selected) == len(candidates) {
				return fmt.Errorf("custom route must select a non-empty proper subset; use full for the complete list")
			}
		}
		state.RouteMode = mode
		state.SelectedGates = append([]string{}, selected...)
		state.SkipAuthorizations = map[string]SkipAuthorization{}
		chosen := selectedSet(*state)
		for _, id := range candidates {
			if !chosen[id] {
				state.SkipAuthorizations[id] = SkipAuthorization{Origin: "ROUTE", Status: "UNSELECTED"}
			}
		}
		discardUnmatchedQADesign(state)
		return nil
	})
}

// discardUnmatchedQADesign reconciles a fast-path speculative QA design recorded
// before the route was confirmed against the now-confirmed route. The fast-path
// design is always blackbox behavior design, so it matches the confirmed route
// exactly when blackbox QA (or the legacy "qa" mode) is selected; whitebox
// structure cases are designed after development, so they do not apply at route
// confirmation. When the route omits blackbox QA, the parallel design is
// discarded (the documented fast-path tradeoff: a design for a route that does
// not include blackbox QA is abandoned) so the design is re-done against the
// confirmed route. An approved set (QA Review passed) is never discarded here.
func discardUnmatchedQADesign(state *RunState) {
	// RQ-001：快速路径设计存于合并 "" 键，按 mode 读取与重置。
	if state.qaDesign("").Status != "PASS" || state.qaReview("").Status == "PASS" {
		return
	}
	if isSelected(*state, blackboxQAID) || isSelected(*state, legacyQAID) {
		return
	}
	// RQ-012：废弃快速路径的投机黑盒设计时清空全部按 mode 存储的用例与执行结果。
	state.QACasesByMode = map[string][]QACase{}
	state.QAExecutionByMode = map[string]QAExecutionResult{}
	state.setQADesign("", ActionResult{Status: "PENDING"})
	state.setQAReview("", ActionResult{Status: "PENDING"})
	// 最终路线不含黑盒时设计废弃，黑盒隔离工作区随之移除（清空登记，host 重建时才需要）。
	state.QAWorktree = ""
}

func AddRouteGates(root, packageRoot, runID string, additions []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireTransition(*state, "route-add", ""); err != nil {
			return err
		}
		if len(additions) == 0 {
			return fmt.Errorf("at least one gate addition is required")
		}
		candidates := catalog.RouteCandidates()
		normalized, err := normalizeSelected(additions, candidates)
		if err != nil {
			return err
		}
		chosen := selectedSet(*state)
		for _, id := range normalized {
			if chosen[id] {
				return fmt.Errorf("gate %q is already selected", id)
			}
			if isQAMode(id) && developmentStarted(*state) {
				return fmt.Errorf("QA cannot be added after development begins")
			}
			chosen[id] = true
			delete(state.SkipAuthorizations, id)
		}
		state.SelectedGates = orderedSelection(chosen, candidates)
		return nil
	})
}

// RecordSlicing records the run's formal split decision. The decision is binary
// (split or no-split), can only be recorded after Start Readiness passes, and
// once recorded is the binding point: it is not re-cut. A split requires at
// least two slices; when the run is retained overall, recording a split
// auto-attaches merge gate and merge QA as the run's only post-merge
// verification (the merge route), so it never goes through normal route
// selection. A no-split decision requires the mandatory "建议不拆（原因）" reason
// trace. The fast-path (non-high-confidence) decision note may note uncertainty.
func RecordSlicing(root, packageRoot, runID, decision string, splitCount int, slices []string, parallel, note, masterRunID string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		decision = strings.ToLower(strings.TrimSpace(decision))
		if decision != "split" && decision != "no-split" {
			return fmt.Errorf("slicing decision must be split or no-split")
		}
		if slicingRecorded(*state) {
			return fmt.Errorf("the slicing decision is already recorded and is the binding point; it is not re-cut")
		}
		if !state.RequirementConfirmed {
			return fmt.Errorf("the current requirement is not confirmed")
		}
		if state.RetainedOverall && decision == "no-split" {
			// 保留总任务实例专为拆分而启动，必须记录 split；记录 no-split 会让它
			// 无法进入开发（prepareDevelopmentAction 拒绝保留总任务实例），只能
			// abort+restart 恢复，是死端。
			return fmt.Errorf("a retained-overall run must record a split decision")
		}
		if decision == "split" && !state.RetainedOverall {
			// 切片实例：必须引用其保留总任务 master run，且该 master 的整体级产品
			// 审/技术审已 PASS，才继承整体级审查结果（记录继承来源与 master 引用），
			// 不再要求切片内重跑；development-worker 门对切片实例经继承满足。不自动
			// 附加合并验证，之后仍走逐切片路线确认与常规开发流程。
			if splitCount < 2 {
				return fmt.Errorf("a split requires at least two slices")
			}
			if strings.TrimSpace(masterRunID) == "" {
				return fmt.Errorf("a slice instance must reference its retained-overall master with --master")
			}
			master, err := LoadRunState(root, strings.TrimSpace(masterRunID))
			if err != nil {
				return fmt.Errorf("slice master run %q is not found: %v", strings.TrimSpace(masterRunID), err)
			}
			if master.Actions["product-review"].Status != "PASS" {
				return fmt.Errorf("slice master %q has not passed Product Review", strings.TrimSpace(masterRunID))
			}
			if master.Actions["start-readiness"].Status != "PASS" {
				return fmt.Errorf("slice master %q has not passed Start Readiness", strings.TrimSpace(masterRunID))
			}
			state.Slicing = &Slicing{Decision: decision, SplitCount: splitCount, Slices: slices, Parallel: strings.TrimSpace(parallel), MasterRunID: strings.TrimSpace(masterRunID), InheritedReviews: []string{"product-review", "start-readiness"}}
			return nil
		}
		if !actionPassedOrAbsent(*state, "product-review") {
			return fmt.Errorf("Product Review must pass before the slicing decision")
		}
		if !actionPassedOrAbsent(*state, "start-readiness") {
			return fmt.Errorf("Start Readiness must pass before the slicing decision")
		}
		if decision == "split" {
			if splitCount < 2 {
				return fmt.Errorf("a split requires at least two slices")
			}
			// 走到这里时 decision == "split" 且 state.RetainedOverall 必为 true（顶层
			// 已处理所有拆分的非保留总任务 run）。分片 >= 2 的保留总任务实例自动
			// 附加合并门与合并 QA：路线确定为合并路线，不涉常规路线选择，custom
			// 的省略不延伸到合并验证。先确认合并门在当前目录中已发现，否则后续
			// prepare-gate 会死端。
			mergeGateDiscovered := false
			for _, gate := range catalog.Gates {
				if gate.ID == mergeGateID {
					mergeGateDiscovered = true
					break
				}
			}
			if !mergeGateDiscovered {
				return fmt.Errorf("merge gate %q is not discovered in the package catalog", mergeGateID)
			}
			state.Slicing = &Slicing{Decision: decision, SplitCount: splitCount, Slices: slices, Parallel: strings.TrimSpace(parallel)}
			state.RouteMode = "merge"
			state.SelectedGates = []string{mergeQAID, mergeGateID}
			state.SkipAuthorizations = map[string]SkipAuthorization{}
			state.QAExecutionByMode = map[string]QAExecutionResult{}
			return nil
		}
		if strings.TrimSpace(note) == "" {
			return fmt.Errorf("a no-split decision requires the mandatory reason note (建议不拆原因)")
		}
		state.Slicing = &Slicing{Decision: decision, Note: strings.TrimSpace(note)}
		return nil
	})
}

// RecordSettledFindings records the user's per-item disposition of findings from
// a product-review / start-readiness review. Confirm (确认问题：真问题、需修订) and
// dismiss (驳回问题：不是问题、作废) are both recorded and injected into the next
// dispatch so the reviewer does not re-raise them (reviewer-side enforcement of
// the double guarantee). Confirming a P0/P1 finding sets the "需重审" marker
// (NeedsReReview): the CLI then refuses to record PASS until a re-review round
// returns PASS. A dismissed P0/P1 is void and does not block. A meaning-changing
// requirement revision clears the settled list because the revised premise may
// legitimately re-raise an item.
func RecordSettledFindings(root, packageRoot, runID, actionID string, confirm, dismiss []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if actionID != "product-review" && actionID != "start-readiness" {
			return fmt.Errorf("settled findings are recorded for product-review or start-readiness only")
		}
		if len(confirm) == 0 && len(dismiss) == 0 {
			return fmt.Errorf("at least one settled finding is required")
		}
		if state.NeedsReReview == nil {
			state.NeedsReReview = map[string]string{}
		}
		severityByMessage := map[string]string{}
		for _, finding := range state.Actions[actionID].Findings {
			if strings.TrimSpace(finding.Message) != "" {
				severityByMessage[strings.TrimSpace(finding.Message)] = finding.Severity
			}
		}
		settle := func(message, disposition string) error {
			message = strings.TrimSpace(message)
			if message == "" {
				return fmt.Errorf("settled finding message is required")
			}
			if severity, ok := severityByMessage[message]; !ok || severity == "" {
				return fmt.Errorf("finding %q is not in the recorded %s result", message, actionID)
			}
			if state.SettledFindings == nil {
				state.SettledFindings = map[string][]SettledFinding{}
			}
			state.SettledFindings[actionID] = append(state.SettledFindings[actionID], SettledFinding{Message: message, Disposition: disposition})
			// 确认的 P0/P1 置位"需重审"标记；驳回或确认的 P2/P3 不置位。
			if disposition == "confirm" && (severityByMessage[message] == "P0" || severityByMessage[message] == "P1") {
				state.NeedsReReview[actionID] = message
			}
			return nil
		}
		for _, message := range confirm {
			if err := settle(message, "confirm"); err != nil {
				return err
			}
		}
		for _, message := range dismiss {
			if err := settle(message, "dismiss"); err != nil {
				return err
			}
		}
		return nil
	})
}

// RegisterQAWorktree registers the QA isolation worktree for the run. The
// worktree is created from the base snapshot by the host (Git linked worktree
// branched from base; SVN workspace checked out at base; P4 client synced to the
// base changelist) and must resolve its native identity to the base snapshot.
// Registration also verifies the current requirement document / acceptance
// artifacts are injected into the worktree with the run's registered revisions
// (a worktree-state injection, not a drift: git commits / p4 changelists / svn
// BASE versions ignore it). This is the single home of that hash check
// (requirement 1's "登记**或**黑盒 prepare 时校验" guard is contracted here):
// later blackbox prepare/claim/record only re-resolve the native identity (==
// base) without re-reading or re-hashing the worktree files. Each blackbox
// design round recreates or reuses this worktree; it never contains development
// code.
func RegisterQAWorktree(root, packageRoot, runID, worktree string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		worktree = strings.TrimSpace(worktree)
		if worktree == "" {
			return fmt.Errorf("QA isolation worktree path is required")
		}
		resolver, err := resolverForVCS(state.VCS, nil)
		if err != nil {
			return err
		}
		resolved, err := resolver.Resolve(cleanWorktree(worktree))
		if err != nil {
			return fmt.Errorf("cannot resolve QA isolation worktree: %w", err)
		}
		if !strings.EqualFold(resolved, state.BaseSnapshot) {
			return fmt.Errorf("QA isolation worktree native identity %s does not match the base snapshot %s", resolved, state.BaseSnapshot)
		}
		// 校验隔离工作区内需求文档/验收产物哈希与 run 登记 revision 一致（防 host 遗忘
		// 刷新注入；需求 1 的"登记**或**黑盒 prepare 校验"单点落在登记处，prepare/claim/
		// record 不再重复哈希复查）。注入是工作树状态，Git=提交、P4=changelist、SVN=BASE
		// 版本级身份校验不受影响。
		if err := verifyIsolatedRequirementRevisions(*state, worktree); err != nil {
			return err
		}
		state.QAWorktree = worktree
		return nil
	})
}

// PrepareGate prepares a gate review dispatch. userRequested is the explicit
// user override signal for the RQ-008 resume rule: when a claimed, unchanged
// dispatch for the same gate is interrupted, the CLI forces a resume unless the
// user authorizes a fresh dispatch (the source is recorded in ReviewOverrides).
func PrepareGate(root, packageRoot, runID, gateID string, userRequested bool, userReason string) (string, error) {
	return prepareBoundPrompt(root, packageRoot, runID, gateID, "gate", true, "", userRequested, userReason, func(state *RunState, catalog PromptCatalog, route PromptRoute) (string, error) {
		if err := requireTransition(*state, "gate", gateID); err != nil {
			return "", err
		}
		result, ok := state.Gates[gateID]
		if !ok {
			return "", fmt.Errorf("gate %q is not in this run's discovered catalog", gateID)
		}
		if semanticResultRecorded(result.Status, result.Snapshot, state.CurrentSnapshot) && !gatePromptChanged(*state, catalog, gateID) {
			return "", fmt.Errorf("gate %q already has an authoritative %s result for the current snapshot; re-review requires a changed gate prompt or a repair snapshot", gateID, result.Status)
		}
		return ComposeGatePrompt(catalog, gateID, route)
	})
}

// PrepareAction prepares an action dispatch. mode is required for qa-design and
// qa-review (blackbox or whitebox): a blackbox dispatch binds its identity and
// source to the QA isolation worktree (== base), a whitebox dispatch to the main
// worktree (== current). userRequested is the explicit user override signal:
// for product-review / start-readiness it allows re-preparing a review of an
// already-PASS round (the only-user-can-break-the-rule side-B exception) and the
// source is recorded in ReviewOverrides. scope（RQ-012 增量审查）对 product-review /
// start-readiness 声明本次审查的需求项范围：声明项置 PENDING 待判，未声明的已 PASS 项
// 保持 PASS、任何轮不可改（除非主代理下次显式声明变更）；格式无关，不按文件名/后缀解析。
func PrepareAction(root, packageRoot, runID, actionID, mode string, userRequested bool, userReason string, scope ...string) (string, error) {
	if actionID == "development-worker" {
		return prepareDevelopmentAction(root, packageRoot, runID, userRequested, userReason)
	}
	reviewerRequired := actionID != "requirements-clarification"
	return prepareBoundPrompt(root, packageRoot, runID, actionID, "action", reviewerRequired, mode, userRequested, userReason, func(state *RunState, catalog PromptCatalog, route PromptRoute) (string, error) {
		// RQ-012：prepare 时声明本次审查范围——声明项置 PENDING 待判，未声明项保持既有
		// 结论不动（已 PASS 项保持 PASS、任何轮不可改）。
		if (actionID == "product-review" || actionID == "start-readiness") && len(scope) != 0 {
			if state.ReviewItemsByAction == nil {
				state.ReviewItemsByAction = map[string]map[string]ReviewItem{}
			}
			items := state.ReviewItemsByAction[actionID]
			if items == nil {
				items = map[string]ReviewItem{}
			}
			for _, key := range scope {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				items[key] = ReviewItem{Status: "PENDING"}
			}
			state.ReviewItemsByAction[actionID] = items
		}
		overridePassRound := userRequested && (actionID == "product-review" || actionID == "start-readiness") && state.Actions[actionID].Status == "PASS"
		if !overridePassRound {
			// RQ-001：qa-design / qa-review / qa-execution 的 transition 判定都按派发 mode
			// 传递 target，使 requireTransition 能读对应 mode 的 review/design 结果。
			target := ""
			switch actionID {
			case "qa-design", "qa-review", "qa-execution":
				target = mode
			}
			if err := requireTransition(*state, actionID, target); err != nil {
				return "", err
			}
		} else if state.ReviewOverrides == nil {
			state.ReviewOverrides = map[string]string{}
		}
		if overridePassRound {
			reason := strings.TrimSpace(userReason)
			if reason == "" {
				reason = "user requested a re-review of an already-passed review round"
			}
			state.ReviewOverrides[actionID] = reason
		}
		// R 修复清单 item 3：黑盒/白盒各自独立派发、并行执行，同一 snapshot 下每个 mode
		// 的 qa-execution 各记录一次；同 mode 已出权威结果时才挡后续同 mode 派发。
		if actionID == "qa-execution" && qaExecutionModeResulted(*state, mode) {
			return "", fmt.Errorf("QA Execution already has an authoritative %s result for this mode", state.qaExecution(mode).Status)
		}
		// RQ-002：重跑强制 scope 决策（CLI 强制，防主代理遗漏）。该 mode 存在更早快照的
		// 权威执行结果（即本次是重跑）时，prepare 前必须已记录覆盖本次重跑（BaseSnapshot
		// 匹配上一轮权威结果快照）的 scope 决策，否则拒绝 prepare。
		if actionID == "qa-execution" {
			if base, ok := qaExecutionPriorResultedBase(*state, mode); ok {
				sc, ok2 := state.ExecutionScopes[mode]
				if !ok2 || sc.BaseSnapshot != base {
					return "", fmt.Errorf("QA Execution rerun requires a scope decision: run `workflow qa-execution-scope --mode %s --decision FULL|AFFECTED ...` first", mode)
				}
			}
		}
		detail, err := actionPromptDetail(*state, catalog, actionID, mode)
		if err != nil {
			return "", err
		}
		return ComposeActionPrompt(catalog, actionID, route, detail)
	})
}

// prepareBoundPrompt composes and registers a prepared dispatch. mode selects the
// QA isolation binding for qa-design/qa-review blackbox dispatches; userRequested
// carries the explicit user override signal for the review-rule enforcement.
func prepareBoundPrompt(root, packageRoot, runID, target, targetKind string, reviewerRequired bool, mode string, userRequested bool, userReason string, compose func(*RunState, PromptCatalog, PromptRoute) (string, error)) (string, error) {
	prompt := ""
	_, err := mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		// RQ-005 硬闸：任何未处理完毕的继承判定未处理完时拒绝继续 / 重跑入口。
		// carry 是处置命令，准备它的审查派发在待决时放行（否则无法处置、死端）。
		if !(targetKind == "action" && target == "carry") {
			if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
				return err
			}
		}
		// RQ-007 续用强制：同一职责、源快照不变、任务内容不变且未带 --user-requested
		// 时，拒绝 prepare 同目标新派发，指示恢复原代理继续同一派发；用户显式授权
		// （RQ-008）时记录来源后放行新派发。
		if err := requireResumeInterrupted(root, *state, catalog, targetKind, target, mode); err != nil {
			if !userRequested {
				return err
			}
			recordReviewOverride(state, target, userReason)
		}
		// 开发开始后 qa-design/qa-review 派发 mode 必填：省略 --mode 会静默绑主工作区、
		// 黑盒代理可读开发代码、隔离被绕过。快速路径（开发尚未开始，路线未确认时的预演
		// 设计）空 mode 合法，仅开发开始后必填。
		if (target == "qa-design" || target == "qa-review") && developmentStarted(*state) && mode == "" {
			return fmt.Errorf("%s dispatch requires --mode blackbox or whitebox after development starts", target)
		}
		blackbox := mode == "blackbox" && (target == "qa-design" || target == "qa-review")
		if blackbox {
			if err := requireIsolatedCurrent(root, *state); err != nil {
				return err
			}
		} else if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		wave := 0
		if targetKind == "gate" {
			wave = currentGateReviewWave(*state)
		}
		attempt := nextDispatchAttempt(*state, targetKind, target, wave)
		dispatchID, err := newDispatchID()
		if err != nil {
			return err
		}
		route := routeForState(root, *state)
		if blackbox {
			route.Worktree = absPath(state.QAWorktree)
			route.CurrentSnapshot = state.BaseSnapshot
		}
		route.DispatchID, route.DispatchAttempt, route.ReviewWave = dispatchID, attempt, wave
		prompt, err = compose(state, catalog, route)
		if err != nil {
			return err
		}
		// RQ-013：prepare（生成任务票）SHALL NOT 作废任何派发。作废只发生在认领新派发时
		// （同功能旧 OPEN 空票清理 / CLAIMED 去重与手动终止例外）与全局失效事件（采纳外部
		// 改动、需求作废、快照重绑），不再在 prepare 时把既有在途派发标 STALE。
		source := state.CurrentSnapshot
		if blackbox {
			source = state.BaseSnapshot
		}
		// 确认的 P0/P1 待重审时，新派发的 product-review/start-readiness 轮即重审轮，
		// 记录其派发 id，只有该轮返回 PASS 才算重审完成。
		if (target == "product-review" || target == "start-readiness") && state.NeedsReReview[target] != "" {
			if state.ReReviewDispatch == nil {
				state.ReReviewDispatch = map[string]string{}
			}
			state.ReReviewDispatch[target] = dispatchID
		}
		sum := sha256.Sum256([]byte(prompt))
		state.Dispatches[dispatchID] = PreparedDispatch{ID: dispatchID, Target: target, TargetKind: targetKind, Attempt: attempt, ReviewWave: wave, PromptHash: hex.EncodeToString(sum[:]), RequirementRevision: state.RequirementRevision, CatalogRevision: state.CatalogRevision, SourceSnapshot: source, ReviewerRequired: reviewerRequired, Status: "OPEN", Mode: mode}
		return nil
	})
	return prompt, err
}

// requireIsolatedCurrent verifies the registered QA isolation worktree sits at
// the base snapshot (its native identity == base). Blackbox qa-design/qa-review
// prepare/claim/record all resolve identity against this worktree instead of the
// main worktree, so the parallel QA never observes development code under normal
// navigation. The injected requirement-document / acceptance-artifact hash check
// lives at workflow qa-worktree registration only (requirement 1's "登记或黑盒
// prepare 时校验" guard is contracted to registration); prepare/claim/record do
// not re-read or re-hash the worktree files.
func requireIsolatedCurrent(root string, state RunState) error {
	if strings.TrimSpace(state.QAWorktree) == "" {
		return fmt.Errorf("QA isolation worktree is not registered; run workflow qa-worktree first")
	}
	resolver, err := resolverForVCS(state.VCS, nil)
	if err != nil {
		return err
	}
	resolved, err := resolver.Resolve(cleanWorktree(state.QAWorktree))
	if err != nil {
		return err
	}
	if !strings.EqualFold(resolved, state.BaseSnapshot) {
		return fmt.Errorf("QA isolation worktree native identity %s does not match the base snapshot %s", resolved, state.BaseSnapshot)
	}
	return nil
}

// verifyIsolatedRequirementRevisions verifies the requirement document / acceptance
// artifact hashes inside the QA isolation worktree match the run's registered
// revisions（防 host 遗忘刷新注入）。注入是工作树状态，原生标识校验（Git=提交、P4=
// changelist、SVN=BASE 版本级）不受影响。
func verifyIsolatedRequirementRevisions(state RunState, worktree string) error {
	for _, artifact := range state.RequirementArtifacts {
		path := resolveFromRoot(cleanWorktree(worktree), artifact.Path)
		revision, err := RequirementRevision(path)
		if err != nil {
			return fmt.Errorf("QA isolation worktree requirement artifact %s: %w", artifact.Path, err)
		}
		if revision != artifact.Revision {
			return fmt.Errorf("QA isolation worktree requirement artifact %s does not match the run revision %s", artifact.Path, artifact.Revision)
		}
	}
	return nil
}

func actionPromptDetail(state RunState, catalog PromptCatalog, actionID, mode string) (string, error) {
	if actionID == "qa-design" {
		var lines []string
		if isMergeVerification(state) {
			lines = append(lines, "Merge QA: design cross-slice interaction cases for the merged snapshot. The case set may be empty when the slices are essentially independent (leave a trace noting that instead of forcing cases).")
		} else if state.RouteMode == "" {
			// 快速路径：路线未确认时只开始黑盒设计，与 start-readiness 并行，先于拆分
			// 决定与路线确认；最终路线不含黑盒 QA 时本设计废弃。
			lines = append(lines, "Fast-path blackbox QA design: from the confirmed requirement, design blackbox behavior cases. The route is not yet confirmed; if it omits blackbox QA this design is discarded.")
		} else if mode == "blackbox" {
			lines = append(lines, "This is a blackbox QA design round: design real QA behavior cases from the current confirmed requirement document. Work in the QA isolation worktree; the current requirement document (revision registered by the run) is injected there as working-tree state. Base every case on the current requirement document, not on any baseline product documentation. '实际使用产品' is the execution-phase description: after the snapshot you execute cases against the built product on the main worktree. In this design phase, the runnable product is not yet built, so base the cases on the injected current requirement.")
		} else if mode == "whitebox" {
			lines = append(lines, "This is a whitebox QA design round: read the implementation on the main worktree, independently design structure test cases (unit, system, integration) by responsibility, and directly write the structural test code you deliver. Every whitebox case must bind the test implementing it with a --test \"<file>::<function>\" reference (file path locating the delivered test code file, function name locating the test inside it; both opaque strings, the CLI does not parse code): the CLI records the reference and requires it non-empty and unique per case (one test implements one case). Test existence and correspondence are verified by QA Review (reading your delivered code) and QA Execution (running the tests), so the case ID and the executed test are truly bound.")
		} else if modes := selectedQAModes(state); len(modes) != 0 {
			lines = append(lines, "Selected QA modes: "+strings.Join(modes, "; "))
		}
		// RQ-012：设计轮只动本派发 mode 的用例，提示词只列该 mode 的既有用例。
		modeCases := state.qaModeCases(mode)
		if len(modeCases) != 0 {
			lines = append(lines, "Review the complete current requirement and every prior case below. Return the complete resulting case set. Retain exact unaffected passing cases and add, modify, or remove only affected cases when impact is reliably bounded; replace the complete set when it is not or the overall workflow changed.")
			// RQ-001：设计轮只列出本派发 mode 的 review FAIL 发现项，不混入另一 mode。
			if review := state.qaReview(mode); review.Status == "FAIL" {
				lines = append(lines, "Address these QA Review findings while redesigning the complete case set:")
				for _, finding := range review.Findings {
					line := "- " + finding.Message
					if len(finding.Locations) != 0 {
						line += " (" + strings.Join(finding.Locations, ", ") + ")"
					}
					lines = append(lines, line)
				}
			}
			for _, testCase := range modeCases {
				lines = append(lines, formatQACase(testCase, true))
			}
		}
		if len(lines) == 0 {
			return "", nil
		}
		return strings.Join(lines, "\n\n"), nil
	}
	if actionID == "qa-review" {
		// RQ-012：按派发 mode 读取该 mode 的用例组装审查输入。
		modeCases := state.qaModeCases(mode)
		pendingCases := pendingQACases(modeCases, mode)
		accepted := []string{"Accepted coverage context; do not return new decisions for these cases:"}
		pending := []string{"Return one decision for every pending case below:"}
		for _, testCase := range modeCases {
			if testCase.ReviewStatus == "PASS" {
				accepted = append(accepted, fmt.Sprintf("%s: %s", testCase.ID, testCase.Description))
			}
		}
		for _, testCase := range pendingCases {
			pending = append(pending, formatQACase(testCase, false))
		}
		if len(pending) == 1 {
			accepted = append(accepted, "There are no pending case decisions. Review the set for missing or duplicated coverage and return no case decisions. Set-level coverage omission for a selected mode is P1 and blocks; P2 findings are suggestions only.")
			return strings.Join(accepted, "\n\n"), nil
		}
		if len(accepted) == 1 {
			return strings.Join(pending, "\n\n"), nil
		}
		return strings.Join(append(accepted, pending...), "\n\n"), nil
	}
	if actionID == "qa-execution" {
		// R 修复清单 item 3：按派发 mode 过滤需执行集，黑盒/白盒各自独立派发、并行执行。
		required := qaExecutionRequiredCases(state, mode)
		var lines []string
		for _, testCase := range required {
			lines = append(lines, formatQACase(testCase, false))
		}
		// RQ-010：AFFECTED 重跑的子集在派发前由 host 综合判定定死，向执行者显式说明
		// 本次需执行范围与继承范围，禁止执行中自行补跑/改判/改选名单外（继承）用例。
		if sc, ok := qaExecutionAffectedScope(state, mode); ok {
			lines = append(lines, fmt.Sprintf("This is an AFFECTED rerun: execute ONLY the affected subset listed above (fixed before dispatch by the host). The other approved cases inherit their PASS from snapshot %s and are NOT part of this execution. Run only this subset; do not self-add, re-judge, or re-select cases outside it, and do not report or change the subset mid-execution.", sc.BaseSnapshot))
		}
		return strings.Join(lines, "\n\n"), nil
	}
	if actionID == "carry" {
		eligible := eligibleCarryGates(state)
		if len(eligible) == 0 {
			return "", fmt.Errorf("no prior passing gates require a Carry decision")
		}
		lines := []string{"Decide INHERIT or RERUN for each gate below:"}
		for _, id := range eligible {
			gate, _ := catalog.Gate(id)
			lines = append(lines, fmt.Sprintf("\n[Gate: %s]\n%s", id, gate.Content))
		}
		return strings.Join(lines, "\n"), nil
	}
	if actionID == "product-review" || actionID == "start-readiness" {
		// RQ-012 增量审查：prepare-action --scope 声明的需求项组成审查输入——PENDING 项
		// "必须判定"、已 PASS 项 "accepted context 不得重判"。与 QA 增量的语义一致：只审新增/
		// 变更的需求项，已审查通过的存量部分由 CLI 保留、不重审。格式无关，不解析文档结构。
		if items := state.ReviewItemsByAction[actionID]; len(items) != 0 {
			var lines []string
			if settled := state.SettledFindings[actionID]; len(settled) != 0 {
				lines = append(lines, "Findings and decisions the user has already settled. Do not re-raise them; re-raise one only if a requirement revision changed the premise it relied on. Dispositions: 确认问题 (confirm, treated as a real problem being fixed) or 驳回问题 (dismiss, voided).")
				for _, settledItem := range settled {
					lines = append(lines, "- ["+settledItem.Disposition+"] "+settledItem.Message)
				}
			}
			pending := []string{"Return one PASS or FAIL decision for every pending item below (each FAIL requires a reason):"}
			accepted := []string{"Accepted coverage context; do not re-judge these already-passed items:"}
			keys := make([]string, 0, len(items))
			for key := range items {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				item := items[key]
				if item.Status == "PASS" {
					accepted = append(accepted, "- "+key)
				} else {
					pending = append(pending, "- "+key)
				}
			}
			if len(pending) == 1 {
				lines = append(lines, "There are no pending items to decide in this incremental review. Review the declared scope and return no item decisions; set-level coverage omission for a declared item is P1 and blocks; P2 findings are suggestions only.")
				return strings.Join(lines, "\n"), nil
			}
			lines = append(lines, strings.Join(pending, "\n"), strings.Join(accepted, "\n"))
			return strings.Join(lines, "\n\n"), nil
		}
		if settled := state.SettledFindings[actionID]; len(settled) != 0 {
			lines := []string{"Findings and decisions the user has already settled. Do not re-raise them; re-raise one only if a requirement revision changed the premise it relied on. Dispositions: 确认问题 (confirm, treated as a real problem being fixed) or 驳回问题 (dismiss, voided)."}
			for _, item := range settled {
				lines = append(lines, "- ["+item.Disposition+"] "+item.Message)
			}
			return strings.Join(lines, "\n"), nil
		}
	}
	return "", nil
}

// pendingQACases lists the pending cases (ReviewStatus != PASS) for a review
// dispatch, filtered to the dispatch's mode when one is set so blackbox and
// whitebox review rounds stay single-mode and do not decide each other's cases.
func pendingQACases(cases []QACase, mode string) []QACase {
	var pending []QACase
	for _, testCase := range cases {
		if testCase.ReviewStatus == "PASS" {
			continue
		}
		if mode != "" && testCase.Mode != mode {
			continue
		}
		pending = append(pending, testCase)
	}
	return pending
}

// qaExecutionRequiredCases is the case set QA Execution must cover. Normally the
// complete approved case set; after a user-authorized snapshot release of the
// blackbox gate, only the approved cases are executed and the unapproved blackbox
// cases are treated as PASS (the origin is recorded by the snapshot override).
// When the dispatch carries a mode, the required set is filtered to that mode so
// blackbox and whitebox execution each dispatch independently and run in parallel
// (R 修复清单 item 3: 执行按 mode 分流). An empty mode keeps the merged set for
// existing single-dispatch flows and merge QA.
func qaExecutionRequiredCases(state RunState, mode string) []QACase {
	// RQ-002/004：重跑按 scope 决策分流需执行集。AFFECTED（且 BaseSnapshot 匹配本次重跑
	// 的继承来源，见 B2）时，需执行集 = 记录的受影响子集（派发前由 host 综合判定定死，
	// 见 RQ-004/010）；否则（FULL / 首次执行）为全部已批准用例（按 mode 过滤，现状不变）。
	// RQ-012：按 mode 读取该 mode 的需执行用例（从跨 mode 视图按 mode 过滤，兼容合并与
	// 按 mode 存储两种布局）。
	modeCases := state.qaModeCases(mode)
	if sc, ok := qaExecutionAffectedScope(state, mode); ok {
		inSubset := map[string]bool{}
		for _, id := range sc.CaseIDs {
			inSubset[id] = true
		}
		subset := []QACase{}
		for _, testCase := range modeCases {
			if inSubset[testCase.ID] {
				subset = append(subset, testCase)
			}
		}
		return subset
	}
	if !snapshotBlackboxReleased(state) {
		return modeCases
	}
	approved := []QACase{}
	for _, testCase := range modeCases {
		if testCase.ReviewStatus == "PASS" {
			approved = append(approved, testCase)
		}
	}
	return approved
}

// filterQACasesByMode returns only the cases whose mode matches the dispatch mode;
// an empty mode returns every case unchanged.
func filterQACasesByMode(cases []QACase, mode string) []QACase {
	if mode == "" {
		return cases
	}
	filtered := []QACase{}
	for _, testCase := range cases {
		if testCase.Mode == mode {
			filtered = append(filtered, testCase)
		}
	}
	return filtered
}

// qaExecutionModeResulted reports whether the run already has an authoritative QA
// Execution result for the dispatch mode at the current snapshot (RQ-012 full
// decoupling): blackbox and whitebox each record independently into their own
// per-mode result, and the merged empty mode holds the single merged-set result.
// An authoritative result is PASS / FAIL / RUNTIME_ERROR recorded at the current
// snapshot.
func qaExecutionModeResulted(state RunState, mode string) bool {
	result := state.qaExecution(mode)
	if result.Snapshot != state.CurrentSnapshot {
		return false
	}
	return result.Status == "PASS" || result.Status == "FAIL" || result.Status == "RUNTIME_ERROR"
}

// qaResultHasMode reports whether the given QA execution result holds at least one
// case record for the dispatch mode. Every record carries its concrete mode, so the
// merged (empty) mode never matches a record; callers check the empty mode by the
// result's authoritative status directly.
func qaResultHasMode(result QAExecutionResult, mode string) bool {
	for _, record := range result.Cases {
		if record.Mode == mode {
			return true
		}
	}
	return false
}

// priorAuthoritativeQA returns the dispatch mode's prior authoritative QA execution
// result (PASS or FAIL) whose snapshot differs from the current one — the result a
// rerun of that mode inherits from (RQ-002/009, RQ-012 full decoupling). It prefers
// the mode's preserved current result (e.g. a PASS kept across a repair snapshot
// advance), then the mode's PriorQAExecutionByMode entry. RUNTIME_ERROR is never
// authoritative.
func priorAuthoritativeQA(state RunState, mode string) (QAExecutionResult, bool) {
	current := state.qaExecution(mode)
	if (current.Status == "PASS" || current.Status == "FAIL") && current.Snapshot != "" && current.Snapshot != state.CurrentSnapshot {
		return current, true
	}
	if prior := state.priorQAExecution(mode); prior != nil && (prior.Status == "PASS" || prior.Status == "FAIL") {
		return *prior, true
	}
	return QAExecutionResult{}, false
}

// qaExecutionPriorResultedBase returns the snapshot of the prior authoritative QA
// execution result a rerun of the mode inherits from — the BaseSnapshot the rerun
// scope decision must match (RQ-002, B1). ok is false when there is no prior
// authoritative result: the mode's first execution, which requires no scope.
func qaExecutionPriorResultedBase(state RunState, mode string) (string, bool) {
	result, ok := priorAuthoritativeQA(state, mode)
	if !ok {
		return "", false
	}
	return result.Snapshot, true
}

// qaExecutionRerunSource returns the authoritative QA execution result a rerun of
// the mode inherits from, together with its snapshot. For the prepare path this is
// the prior authoritative result at an earlier snapshot. At the repair-limit path
// (authorize-repair before the repair snapshot advance) the current authoritative
// FAIL result at the current snapshot is itself the inheritance source — after the
// advance it is preserved to the mode's PriorQAExecutionByMode and becomes the
// "上一轮" (RQ-007).
func qaExecutionRerunSource(state RunState, mode string) (QAExecutionResult, string, bool) {
	if prior, ok := priorAuthoritativeQA(state, mode); ok {
		return prior, prior.Snapshot, true
	}
	current := state.qaExecution(mode)
	if current.Status == "FAIL" && current.Snapshot == state.CurrentSnapshot {
		return current, state.CurrentSnapshot, true
	}
	return QAExecutionResult{}, "", false
}

// qaOverallResult aggregates the per-mode QA execution results into a single view
// for display / summary purposes (RQ-012 full decoupling). The merged (empty-mode)
// result is returned as-is when the run uses merged storage; otherwise the concrete
// modes' results are combined (union of cases and findings) with FAIL winning over
// RUNTIME_ERROR over PASS, and the snapshot taken from the recorded modes.
func qaOverallResult(state RunState) QAExecutionResult {
	if merged := state.qaExecution(""); merged.Status != "" || merged.Snapshot != "" || len(merged.Cases) != 0 {
		return merged
	}
	var cases []QAResultRecord
	var findings []Finding
	status := "PENDING"
	snapshot := state.CurrentSnapshot
	haveRecorded := false
	for _, mode := range []string{"blackbox", "whitebox"} {
		result := state.qaExecution(mode)
		if result.Status == "" {
			continue
		}
		haveRecorded = true
		cases = append(cases, result.Cases...)
		findings = append(findings, result.Findings...)
		switch {
		case result.Status == "FAIL":
			status = "FAIL"
		case status != "FAIL" && result.Status == "RUNTIME_ERROR":
			status = "RUNTIME_ERROR"
		case status != "FAIL" && status != "RUNTIME_ERROR" && result.Status == "PASS":
			status = "PASS"
		}
		if result.Snapshot != "" {
			snapshot = result.Snapshot
		}
	}
	if !haveRecorded {
		return QAExecutionResult{Status: "PENDING"}
	}
	return QAExecutionResult{Status: status, Snapshot: snapshot, Cases: cases, Findings: findings}
}

// qaExecutionAffectedScope returns the recorded AFFECTED scope decision that applies
// to the current rerun of the mode: Decision AFFECTED and BaseSnapshot matching the
// prior authoritative result this rerun inherits from (B2). ok is false when the run
// is not under an AFFECTED rerun scope (FULL decision or first execution).
func qaExecutionAffectedScope(state RunState, mode string) (QAExecutionScope, bool) {
	sc, ok := state.ExecutionScopes[mode]
	if !ok || sc.Decision != "AFFECTED" {
		return QAExecutionScope{}, false
	}
	base, has := qaExecutionPriorResultedBase(state, mode)
	if !has || sc.BaseSnapshot != base {
		return QAExecutionScope{}, false
	}
	return sc, true
}

// qaModeLabel renders a dispatch mode for user-facing messages: the merged (empty)
// mode is shown as "merged".
func qaModeLabel(mode string) string {
	if mode == "" {
		return "merged"
	}
	return mode
}

// qaReviewDispatchPrepared reports whether a qa-review dispatch for the given mode
// is prepared (OPEN or CLAIMED) and not yet completed — the design lock point
// (RQ-011, RQ-012 full decoupling): once a review dispatch for a mode is prepared,
// that mode's QA case set is frozen until the review returns, so further qa-design
// re-records for that mode are rejected. The lock is per mode: a blackbox review in
// flight does not lock the whitebox design (and vice versa); an empty mode matches
// merged / fast-path reviews. After the review records PASS/FAIL (or is still
// PENDING with no dispatch), the design is unlocked again.
func qaReviewDispatchPrepared(state RunState, mode string) bool {
	for _, dispatch := range state.Dispatches {
		if dispatch.TargetKind != "action" || dispatch.Target != "qa-review" || (dispatch.Status != "OPEN" && dispatch.Status != "CLAIMED") {
			continue
		}
		if dispatch.Mode == mode {
			return true
		}
	}
	return false
}

func formatQACase(testCase QACase, includeReview bool) string {
	value := fmt.Sprintf("%s\nmode: %s\ndescription: %s\nprocedure: %s\noracle: %s", testCase.ID, testCase.Mode, testCase.Description, testCase.Procedure, testCase.Oracle)
	// RQ-013：白盒用例带测试引用（<文件>::<函数>）绑定，展示给审查/执行代理，使执行按用例
	// 对应的测试跑；文档自包含——读文档即知该测试在哪个文件、叫什么。
	if strings.TrimSpace(testCase.Test) != "" {
		value += "\ntest: " + testCase.Test
	}
	if includeReview {
		value += "\nreview status: " + testCase.ReviewStatus
	}
	return value
}

func prepareDevelopmentAction(root, packageRoot, runID string, userRequested bool, userReason string) (string, error) {
	return prepareBoundPrompt(root, packageRoot, runID, "development-worker", "action", true, "", userRequested, userReason, func(state *RunState, catalog PromptCatalog, route PromptRoute) (string, error) {
		if state.RetainedOverall {
			return "", fmt.Errorf("a retained overall run keeps implementation and repair ownership in slice runs; record merged slice snapshots with workflow snapshot")
		}
		if err := requireTransition(*state, "development-worker", ""); err != nil {
			return "", err
		}
		detail := ""
		status := state.Actions["development-worker"].Status
		if status == developmentComplete || status == developmentVerified || status == developmentRepairPrepared {
			detail = repairInput(*state)
		}
		prompt, err := ComposeActionPrompt(catalog, "development-worker", route, detail)
		if err != nil {
			return "", err
		}
		if status == developmentComplete || status == developmentVerified {
			state.Actions["development-worker"] = ActionResult{Status: developmentRepairPrepared}
		} else if status == developmentPending {
			state.Actions["development-worker"] = ActionResult{Status: developmentPrepared}
		}
		return prompt, nil
	})
}

func ClaimDispatch(root, packageRoot, runID, dispatchID, reviewerIdentity string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		dispatchID, reviewerIdentity = strings.TrimSpace(dispatchID), strings.TrimSpace(reviewerIdentity)
		if dispatchID == "" {
			return fmt.Errorf("dispatch id is required")
		}
		dispatch, ok := state.Dispatches[dispatchID]
		if !ok {
			return fmt.Errorf("unknown dispatch %q", dispatchID)
		}
		// RQ-005 硬闸：carry 处置派发的认领豁免，其余派发在存在待决继承判定时拒绝认领。
		if !(dispatch.TargetKind == "action" && dispatch.Target == "carry") {
			if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
				return err
			}
		}
		if dispatch.TargetKind == "action" && dispatch.Target == "development-worker" {
			// 开发/修复派发是 reviewer-required；worker 一旦提交，原生 HEAD 就前进到
			// 派发源快照之后。认领放宽：当前原生 HEAD 是派发源快照的后代（或相等）
			// 即允许认领（覆盖 worker 已提交的情形），否则 worker 提交后认领必失败，
			// "会提交的开发 worker"没有可行路径。该检查不验证 HEAD 是否由 worker 产生，
			// 开发期间无关外部提交落地会被静默吸收进开发快照（文档已注明）。其余派发
			// （审查、QA 等）仍要求原生 HEAD 精确等于当前快照。
			if err := requireDevelopmentClaimableHead(root, *state, dispatch); err != nil {
				return err
			}
		} else if dispatch.Mode == "blackbox" && (dispatch.Target == "qa-design" || dispatch.Target == "qa-review") {
			// 黑盒 qa-design/qa-review 派发对 QA 隔离工作区解析原生标识（恒等于基线）。
			if err := requireIsolatedCurrent(root, *state); err != nil {
				return err
			}
		} else if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if !dispatch.ReviewerRequired {
			return fmt.Errorf("dispatch %q does not require a reviewer claim", dispatchID)
		}
		if dispatch.Status != "OPEN" {
			return fmt.Errorf("dispatch %q is %s and cannot be claimed", dispatchID, dispatch.Status)
		}
		// Resolve the effective claim identity: the preferred reviewer
		// identity wins when it matches a pending subagent start observation;
		// otherwise a unique pending start observation supplies its own
		// identity (common operator mistake compatibility), and an ambiguous
		// or empty resolution is rejected rather than binding the wrong
		// subagent or silently dropping lifecycle evidence.
		effective, err := workflowLifecycle.ResolveClaimIdentity(root, state.RunID, reviewerIdentity)
		if err != nil {
			return err
		}
		for priorID, prior := range state.Dispatches {
			if prior.ReviewerIdentity == effective {
				return fmt.Errorf("reviewer identity is already reserved by dispatch %s", priorID)
			}
		}
		// RQ-013 同功能去重：认领新派发时清掉同功能旧 OPEN 空票（无子代理、不挡认领）；已有
		// CLAIMED 同功能派发默认拒绝并行，除非前子代理已被主代理手动终结（生命周期已捕获其
		// stop 事件/中断原因 → 把前派发标 STALE、允许认领新派发）。
		if err := enforceSameFunctionDedup(root, state, dispatch); err != nil {
			return err
		}
		if err := workflowLifecycle.Bind(root, state.RunID, dispatchID, effective); err != nil {
			return err
		}
		dispatch.ReviewerIdentity, dispatch.Status = effective, "CLAIMED"
		state.Dispatches[dispatchID] = dispatch
		return nil
	})
}

// RecordAction records a product-review / start-readiness / requirements-
// clarification result. userRequested is the explicit user override signal for
// the review-rule enforcement (only the user can break the rule); its source is
// recorded in ReviewOverrides.
func RecordAction(root, packageRoot, runID, actionID, dispatchID, status, message string, findings []FindingInput, userRequested bool, userReason string, items ...ReviewItemInput) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if _, ok := catalog.Action(actionID); !ok {
			return fmt.Errorf("unknown action prompt %q", actionID)
		}
		if actionID != "requirements-clarification" && actionID != "start-readiness" && actionID != "product-review" {
			return fmt.Errorf("action %q has a dedicated workflow command and cannot use record-action", actionID)
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", actionID)
		if err != nil {
			return err
		}
		if err := requireTransition(*state, actionID, ""); err != nil {
			return err
		}
		if actionID == "start-readiness" || actionID == "product-review" {
			if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
				return err
			}
			if err := enforceReviewRule(state, actionID, dispatch.ID, status, findings, userRequested, userReason); err != nil {
				return err
			}
			// RQ-012 增量审查：record-action 下发逐项判定。所有 PENDING 项必须全判；对已 PASS
			// 项下发判定被拒；FAIL 项必须带 finding（reason）。
			if err := recordReviewItems(state, actionID, dispatch.ID, items); err != nil {
				return err
			}
		}
		backfillDispatchCost(root, state, dispatch)
		result, err := semanticActionResult(actionID, status, message, findings, state)
		if err != nil {
			return err
		}
		result.DispatchID = dispatch.ID
		state.Actions[actionID] = result
		completeDispatch(state, dispatch.ID)
		return nil
	})
}

// recordReviewItems 逐项登记 product-review / start-readiness 的增量审查判定（RQ-012）。
// 语义与 QA 增量一致：prepare-action --scope 声明的 PENDING 项在此必须全部判定；对已 PASS
// 项下发判定被拒（除非主代理下次 prepare 显式重新声明变更）；FAIL 项必须携带 finding。
// 未声明 scope（该动作无逐项表）时不做逐项约束，回到全量审查路径。
func recordReviewItems(state *RunState, actionID, dispatchID string, items []ReviewItemInput) error {
	table := state.ReviewItemsByAction[actionID]
	if len(table) == 0 {
		return nil
	}
	seen := map[string]bool{}
	pending := map[string]bool{}
	for key, item := range table {
		if item.Status != "PASS" {
			pending[key] = true
		}
	}
	// 逐项表非空但已全部 PASS（无待定项）时，空判定集是合法形态（P2-1 死路修复）：
	// prepare 无 --scope 时逐项表全 PASS 会生成"无待定项可判"提示，审查者按提示不下发
	// 任何 --item，record 必须接受空集而非拒绝——否则无法记录该轮审查。仅当确实存在
	// 待定项时才强制"所有 PENDING 必须全判"。
	if len(pending) == 0 && len(items) == 0 {
		return nil
	}
	if len(items) == 0 {
		return fmt.Errorf("the %s incremental review requires one --item decision for every pending item", actionID)
	}
	for _, input := range items {
		key := strings.TrimSpace(input.Key)
		itemStatus := strings.ToUpper(strings.TrimSpace(input.Status))
		if key == "" {
			return fmt.Errorf("review item key is required")
		}
		if itemStatus != "PASS" && itemStatus != "FAIL" {
			return fmt.Errorf("review item %q status must be PASS or FAIL", key)
		}
		existing, ok := table[key]
		if !ok {
			return fmt.Errorf("review item %q is not in the declared review scope", key)
		}
		if existing.Status == "PASS" {
			return fmt.Errorf("review item %q already has an authoritative PASS result and cannot be re-judged; redeclare it in prepare-action --scope to re-review", key)
		}
		if seen[key] {
			return fmt.Errorf("duplicate review item decision for %q", key)
		}
		seen[key] = true
		if itemStatus == "FAIL" && strings.TrimSpace(input.Reason) == "" {
			return fmt.Errorf("review item %q FAIL requires a finding (reason)", key)
		}
		table[key] = ReviewItem{Status: itemStatus, DispatchID: dispatchID, Message: strings.TrimSpace(input.Reason)}
	}
	for key := range pending {
		if !seen[key] {
			return fmt.Errorf("review item %q is pending and must be judged (all pending items require a decision)", key)
		}
	}
	return nil
}

// enforceReviewRule implements the CLI-forced review rules for product-review
// and start-readiness (requirement 5):
//   - 仅 P2/P3 → 该轮记录 PASS，P2/P3 建议随 PASS 可见、不阻塞、不产生 FAIL；
//   - P0/P1 → 记录 FAIL；用户逐项确认或驳回。驳回的 P0/P1 作废不阻塞（PASS 可携带已
//     驳回的 P0/P1）；确认的 P0/P1 置位"需重审"标记，CLI 在重审前拒绝记录 PASS；
//   - 只有用户可破例：任一侧的破例都须 userRequested 显式授权，来源记录到
//     ReviewOverrides；主代理无破例权。
func enforceReviewRule(state *RunState, actionID, dispatchID, status string, findings []FindingInput, userRequested bool, userReason string) error {
	if state.NeedsReReview == nil {
		state.NeedsReReview = map[string]string{}
	}
	if state.ReReviewDispatch == nil {
		state.ReReviewDispatch = map[string]string{}
	}
	status = strings.ToUpper(strings.TrimSpace(status))
	switch status {
	case "PASS":
		dismissed := settledMessagesByDisposition(*state, actionID, "dismiss")
		for _, finding := range findings {
			severity := strings.ToUpper(strings.TrimSpace(finding.Severity))
			if (severity == "P0" || severity == "P1") && !dismissed[strings.TrimSpace(finding.Message)] {
				if userRequested {
					recordReviewOverride(state, actionID, userReason)
					return nil
				}
				return fmt.Errorf("P0/P1 finding %q is confirmed or undisposed; record FAIL and re-review after a requirement revision, or dismiss it explicitly", finding.Message)
			}
		}
		if state.NeedsReReview[actionID] != "" && state.ReReviewDispatch[actionID] != dispatchID {
			if userRequested {
				recordReviewOverride(state, actionID, userReason)
				return nil
			}
			return fmt.Errorf("confirmed P0/P1 finding %q awaits a re-review; record-action PASS is rejected before the re-review", state.NeedsReReview[actionID])
		}
		if state.NeedsReReview[actionID] != "" {
			delete(state.NeedsReReview, actionID)
			delete(state.ReReviewDispatch, actionID)
		}
	case "FAIL":
		confirmed := settledMessagesByDisposition(*state, actionID, "confirm")
		for _, finding := range findings {
			severity := strings.ToUpper(strings.TrimSpace(finding.Severity))
			if (severity == "P0" || severity == "P1") && confirmed[strings.TrimSpace(finding.Message)] {
				state.NeedsReReview[actionID] = strings.TrimSpace(finding.Message)
				delete(state.ReReviewDispatch, actionID)
			}
		}
	}
	return nil
}

func recordReviewOverride(state *RunState, actionID, userReason string) {
	if state.ReviewOverrides == nil {
		state.ReviewOverrides = map[string]string{}
	}
	reason := strings.TrimSpace(userReason)
	if reason == "" {
		reason = "user explicitly requested an override"
	}
	state.ReviewOverrides[actionID] = reason
}

// settledMessagesByDisposition lists the settled finding messages of the action
// that carry the named disposition (confirm or dismiss).
func settledMessagesByDisposition(state RunState, actionID, disposition string) map[string]bool {
	result := map[string]bool{}
	for _, item := range state.SettledFindings[actionID] {
		if item.Disposition == disposition {
			result[item.Message] = true
		}
	}
	return result
}

func RecordGate(root, packageRoot, runID, gateID, dispatchID, status, message, compared string, findings []FindingInput) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if _, ok := catalog.Gate(gateID); !ok {
			return fmt.Errorf("gate %q is not discovered", gateID)
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "gate", gateID)
		if err != nil {
			return err
		}
		if err := requireTransition(*state, "gate", gateID); err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
		existing := state.Gates[gateID]
		if semanticResultRecorded(existing.Status, existing.Snapshot, state.CurrentSnapshot) && !gatePromptChanged(*state, catalog, gateID) {
			return fmt.Errorf("gate %q already has an authoritative %s result for the current snapshot; re-review requires a changed gate prompt or a repair snapshot", gateID, existing.Status)
		}
		if normalized := strings.ToUpper(strings.TrimSpace(status)); normalized != "RUNTIME_ERROR" {
			want := comparedRange(*state)
			if !strings.EqualFold(strings.TrimSpace(compared), want) {
				return fmt.Errorf("gate review reported compared %q but the requested base-to-current range is %q", compared, want)
			}
		}
		result, err := semanticGateResult(status, message, findings, state)
		if err != nil {
			return err
		}
		if err := rejectFrozenArtifactFindings(*state, result.Findings); err != nil {
			return err
		}
		result.Compared = strings.TrimSpace(compared)
		result.DispatchID = dispatch.ID
		state.Gates[gateID] = result
		// Settle the gate's recorded prompt hash only when the run already keeps a
		// full hash record. A run state loaded without hashes keeps its absent
		// semantics (nil) so an unmoved catalog still reports no delta instead of
		// mis-reporting every entry after a partial backfill.
		if gate, ok := catalog.Gate(gateID); ok && state.PromptHashes != nil {
			state.PromptHashes["gate:"+gateID] = composedGatePromptHash(catalog, gate.Content)
		}
		completeDispatch(state, dispatch.ID)
		completeReviewWaveIfReady(state)
		return nil
	})
}

func RecordQADesign(root, packageRoot, runID, dispatchID string, cases []QACaseInput, runtimeError string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", "qa-design")
		if err != nil {
			return err
		}
		// RQ-012：qa-design 的 review 锁按 dispatch mode 检查（黑盒 review 在飞不锁白盒设计）。
		if err := requireTransition(*state, "qa-design", dispatch.Mode); err != nil {
			return err
		}
		// 黑盒设计派发对 QA 隔离工作区解析原生标识（== 基线）；白盒/其他对主工作区（== 当前）。
		if err := requireDispatchNativeCurrent(root, *state, dispatch); err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
		if strings.TrimSpace(runtimeError) != "" {
			if len(cases) != 0 {
				return fmt.Errorf("QA Design runtime error cannot include cases")
			}
			// RQ-012：只重置本派发 mode 的执行结果，另一 mode 的执行结果不受影响。
			state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: "PENDING"})
			// RQ-001：qa-design 权威结果按 mode 独立记录。
			state.setQADesign(dispatch.Mode, ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), DispatchID: dispatch.ID})
			completeDispatch(state, dispatch.ID)
			return nil
		}
		// 空用例集不再被拒绝：被选中模式零用例是覆盖缺失，由 qa-review 的 set-level 覆盖
		// 判定承担（P1、阻塞），不设机械化质量下限。合并 QA 的零用例既有例外保留。空设计
		// 记录 PASS 后进入 QA Review（无待定用例，只做集合覆盖判定）。
		if len(cases) == 0 {
			message := ""
			if isMergeVerification(*state) {
				// 合并 QA 的用例集可为零/极少：留痕注明"切片基本独立、无跨切片交互用例"。
				message = "切片基本独立、无跨切片交互用例"
			}
			// RQ-012：只清空本派发 mode 的用例列表与执行结果，不触碰另一 mode 的既有用例/结果。
			state.setQACases(dispatch.Mode, []QACase{})
			state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: "PENDING"})
			// RQ-001：设计 PASS 记录到本 mode，并把本 mode 的 review 重置为 PENDING（不触碰
			// 另一 mode 的 review 判定）。
			state.setQADesign(dispatch.Mode, ActionResult{Status: "PASS", Message: message, DispatchID: dispatch.ID})
			state.setQAReview(dispatch.Mode, ActionResult{Status: "PENDING"})
			completeDispatch(state, dispatch.ID)
			return nil
		}
		// RQ-012：设计轮只对本派发 mode 的用例列表做增量替换——从该 mode 自己的存储列表取
		// 既有用例（保留已批准用例、增量补全），另一 mode 的列表保持不动。
		priorCases := state.qaCases(dispatch.Mode)
		seen := map[string]bool{}
		priorByKey := map[string]QACase{}
		// 用例 ID 跨所有 mode 全局唯一（执行记录 / AFFECTED 子集按 ID 索引）：新 ID 生成
		// 时保留全部 mode 已占用的 ID，避免不同 mode 的用例撞号；priorByKey 只从本 mode
		// 的既有用例取，保证只复用/保留本 mode 的已批准用例。
		usedIDs := map[string]bool{}
		for _, prior := range state.allQACases() {
			usedIDs[prior.ID] = true
		}
		for _, prior := range priorCases {
			priorByKey[qaCaseSemanticKeyWithTest(prior.Mode, prior.Description, prior.Procedure, prior.Oracle, prior.Test)] = prior
		}
		nextID := 1
		updated := make([]QACase, 0, len(cases))
		for index, item := range cases {
			normalized := QACase{
				Mode:        strings.ToLower(strings.TrimSpace(item.Mode)),
				Description: strings.TrimSpace(item.Description),
				Procedure:   strings.TrimSpace(item.Procedure),
				Oracle:      strings.TrimSpace(item.Oracle),
				Test:        strings.TrimSpace(item.Test),
			}
			if normalized.Mode != "blackbox" && normalized.Mode != "whitebox" {
				return fmt.Errorf("QA case %d mode must be blackbox or whitebox", index+1)
			}
			// RQ-012：按 mode 派发的设计轮只记录本 mode 的用例，防止把别的 mode 的用例写进
			// 本 mode 的列表。
			if dispatch.Mode != "" && normalized.Mode != dispatch.Mode {
				return fmt.Errorf("QA case %d mode %q does not match the %s design dispatch", index+1, normalized.Mode, dispatch.Mode)
			}
			for name, value := range map[string]string{"description": normalized.Description, "procedure": normalized.Procedure, "oracle": normalized.Oracle} {
				if value == "" {
					return fmt.Errorf("QA case %d %s is required", index+1, name)
				}
			}
			// RQ-013 caseId↔测试绑定：白盒用例必须写明实现该用例的测试引用（Test 字段 =
			// "<文件路径>::<函数名>"，两个不透明字符串，CLI 不解析代码内容）。CLI 记录时只做
			// 最小校验：引用非空、且同一引用不被两条白盒用例共用（一个测试实现一个用例）；
			// 存在性与对应性由 qa-review（读代码核对）与 qa-execution（实际运行）验证。
			// 黑盒用例无结构测试绑定、不要求 Test。不满足即拒绝记录。
			if normalized.Mode == "whitebox" {
				if normalized.Test == "" {
					return fmt.Errorf("QA case %d (whitebox) requires a --test <file>::<function> reference locating the whitebox designer's delivered test", index+1)
				}
			}
			key := qaCaseSemanticKeyWithTest(normalized.Mode, normalized.Description, normalized.Procedure, normalized.Oracle, normalized.Test)
			if seen[key] {
				return fmt.Errorf("duplicate QA case %d", index+1)
			}
			seen[key] = true
			if prior, ok := priorByKey[key]; ok {
				normalized.ID = prior.ID
				if prior.ReviewStatus == "PASS" {
					normalized.ReviewStatus = "PASS"
				} else {
					normalized.ReviewStatus = "PENDING"
				}
			} else {
				for usedIDs[fmt.Sprintf("CASE-%03d", nextID)] {
					nextID++
				}
				normalized.ID, normalized.ReviewStatus = fmt.Sprintf("CASE-%03d", nextID), "PENDING"
				usedIDs[normalized.ID] = true
				nextID++
			}
			updated = append(updated, normalized)
		}
		// RQ-013 1:1：同一测试引用（<文件>::<函数>）不能被两条白盒用例共用——一个测试实现
		// 一个用例。对本次记录后的完整用例集检查（含保留的既有用例），撞引用即拒绝记录。
		// 黑盒用例无测试引用，不参与。
		refOwner := map[string]string{}
		for _, testCase := range updated {
			if testCase.Mode != "whitebox" {
				continue
			}
			ref := strings.TrimSpace(testCase.Test)
			if ref == "" {
				continue
			}
			if owner, dup := refOwner[ref]; dup {
				return fmt.Errorf("QA whitebox test reference %q is shared by case %s and case %s; one test implements one case", ref, owner, testCase.ID)
			}
			refOwner[ref] = testCase.ID
		}
		// RQ-001：返工约束只按本派发 mode 的 review FAIL 检查，不读另一 mode 的 review 判定。
		if state.qaReview(dispatch.Mode).Status == "FAIL" {
			pending := false
			for _, testCase := range updated {
				if testCase.ReviewStatus != "PASS" {
					pending = true
					break
				}
			}
			if !pending {
				if len(updated) == len(priorCases) {
					return fmt.Errorf("QA Design rework must add or revise a case, or remove an obsolete or duplicated case")
				}
			}
		}
		state.setQACases(dispatch.Mode, updated)
		// RQ-012：设计轮只重置本派发 mode 的执行结果——白盒设计不清黑盒已记录的执行结果。
		state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: "PENDING"})
		// RQ-001：设计 PASS 记录到本 mode、本 mode 的 review 重置为 PENDING，另一 mode 不动。
		state.setQADesign(dispatch.Mode, ActionResult{Status: "PASS", DispatchID: dispatch.ID})
		state.setQAReview(dispatch.Mode, ActionResult{Status: "PENDING"})
		completeDispatch(state, dispatch.ID)
		return nil
	})
}

func RecordQAReview(root, packageRoot, runID, dispatchID string, decisions []QAReviewInput, runtimeError string, setFindings []FindingInput) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", "qa-review")
		if err != nil {
			return err
		}
		// RQ-001：review 的 transition 判定按派发 mode 进行（target=mode）。
		if err := requireTransition(*state, "qa-review", dispatch.Mode); err != nil {
			return err
		}
		// 黑盒 review 派发对 QA 隔离工作区解析原生标识（== 基线）；白盒对主工作区（== 当前）。
		if err := requireDispatchNativeCurrent(root, *state, dispatch); err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
		if strings.TrimSpace(runtimeError) != "" {
			if len(decisions) != 0 || len(setFindings) != 0 {
				return fmt.Errorf("QA Review runtime error cannot include case decisions or findings")
			}
			// RQ-001：review RUNTIME_ERROR 按 mode 独立记录。
			state.setQAReview(dispatch.Mode, ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), DispatchID: dispatch.ID})
			completeDispatch(state, dispatch.ID)
			return nil
		}
		// 待定用例集按派发 mode 限定：黑盒 review 只决定黑盒待定用例、白盒 review 只决定
		// 白盒待定用例，各派发为单 mode、不混合。mode 为空（快速路径/合并 QA）时覆盖全部
		// 待定用例。被选中模式零用例时待定集为空，只做集合覆盖判定。RQ-012：与提示词组装
		// 用同一读取视图（per-mode 键非空即用、否则回退合并 "" 键），保证 recorder 的
		// pending 集与提示词列出的一致；决定后写回同一存储键。
		storageKey, modeCases := state.qaModeCasesWithKey(dispatch.Mode)
		pending := map[string]int{}
		for index, testCase := range modeCases {
			if testCase.ReviewStatus == "PASS" {
				continue
			}
			pending[testCase.ID] = index
		}
		if len(decisions) != len(pending) {
			return fmt.Errorf("QA Review must decide all %d pending cases", len(pending))
		}
		seen := map[string]bool{}
		findings := make([]Finding, 0, len(decisions)+len(setFindings))
		status := "PASS"
		for _, input := range decisions {
			caseID := strings.TrimSpace(input.CaseID)
			index, ok := pending[caseID]
			if !ok {
				return fmt.Errorf("QA Review case %q is not pending in this dispatch", input.CaseID)
			}
			if seen[caseID] {
				return fmt.Errorf("duplicate QA Review decision for %s", caseID)
			}
			seen[caseID] = true
			outcome := strings.ToUpper(strings.TrimSpace(input.Outcome))
			if outcome != "PASS" && outcome != "FAIL" {
				return fmt.Errorf("QA Review outcome for %s must be PASS or FAIL", caseID)
			}
			reason := strings.TrimSpace(input.Reason)
			if outcome == "FAIL" && reason == "" {
				return fmt.Errorf("QA Review FAIL for %s requires a reason", caseID)
			}
			modeCases[index].ReviewStatus = outcome
			if outcome == "FAIL" {
				status = "FAIL"
				findings = append(findings, Finding{Message: caseID + ": " + reason})
			}
		}
		// 集合层面发现项按严重度分类：覆盖遗漏（用例集未覆盖需求验收点/被选中模式）判 P1、
		// 阻塞、必须补用例；P2 仅为建议、不阻塞、不需处置。P0 不接受（集合层面不判终态致命）。
		for _, input := range setFindings {
			severity := strings.ToUpper(strings.TrimSpace(input.Severity))
			if severity != "P1" && severity != "P2" {
				return fmt.Errorf("QA Review set finding severity must be P1 or P2")
			}
			if strings.TrimSpace(input.Message) == "" {
				return fmt.Errorf("QA Review finding message is required")
			}
			locations := make([]string, 0, len(input.Locations))
			for _, location := range input.Locations {
				if err := validateFindingLocation(location); err != nil {
					return err
				}
				locations = append(locations, strings.TrimSpace(location))
			}
			findings = append(findings, Finding{Severity: severity, Message: strings.TrimSpace(input.Message), Locations: locations})
			if severity == "P1" {
				status = "FAIL"
			}
		}
		// RQ-001：review 权威结果按 mode 独立记录；FAIL 只把本 mode 的设计重置为 PENDING，
		// 另一 mode 的设计/执行/审查判定不受影响。
		state.setQAReview(dispatch.Mode, ActionResult{Status: status, Findings: findings, DispatchID: dispatch.ID})
		if status == "FAIL" {
			state.setQADesign(dispatch.Mode, ActionResult{Status: "PENDING"})
			// RQ-012：review FAIL 只重置本派发 mode 的执行结果，另一 mode 的结果不受影响。
			state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: "PENDING"})
			// 黑盒 review 连续 FAIL 计数：PASS 清零、RUNTIME_ERROR 不计入也不打断连续。
			if dispatch.Mode == "blackbox" {
				state.BlackboxReviewFails++
			}
		} else if dispatch.Mode == "blackbox" {
			// 黑盒 qa-review PASS 自动清空隔离工作区、清零连续 FAIL 计数。
			state.QAWorktree = ""
			state.BlackboxReviewFails = 0
		}
		// RQ-012：把决定的审查状态写回读取时同一存储键（per-mode 键或合并 "" 回退）。
		state.setQACasesForReview(dispatch.Mode, storageKey, modeCases)
		completeDispatch(state, dispatch.ID)
		return nil
	})
}

func RecordQAExecution(root, packageRoot, runID, dispatchID string, results []QAResultInput, runtimeError string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", "qa-execution")
		if err != nil {
			return err
		}
		// RQ-001：qa-execution 的 transition 判定按派发 mode 进行（target=mode）。
		if err := requireTransition(*state, "qa-execution", dispatch.Mode); err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
		// R 修复清单 item 3：按派发 mode 分流，黑盒/白盒各自独立派发、并行执行——同一
		// snapshot 下每个 mode 各记录一次，同 mode 已出权威结果时才挡后续同 mode 派发
		// （RQ-012：按 mode 独立）。
		if qaExecutionModeResulted(*state, dispatch.Mode) {
			return fmt.Errorf("QA Execution already has an authoritative %s result for this mode", state.qaExecution(dispatch.Mode).Status)
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(results) != 0 {
				return fmt.Errorf("QA runtime error cannot include case results")
			}
			// RQ-012：按 mode 分开记录，只影响本派发 mode。
			state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), Snapshot: state.CurrentSnapshot})
			// RUNTIME_ERROR 不是权威结果，不得驱逐存续的上一轮结果（P2-乙确认）：下一轮
			// 重跑的 base 仍取本 mode 的 PriorQAExecution，所以这里不清空本 mode 的 prior。
			completeDispatch(state, dispatch.ID)
			completeReviewWaveIfReady(state)
			return nil
		}
		// 需执行集：正常流程为完整用例集；快照黑盒门经用户授权放行后只覆盖已批准用例，
		// 未批准的（黑盒）用例不计入需执行集、验证状态视为 PASS（授权来源由快照放行记录）。
		// 按派发 mode 过滤（R 修复清单 item 3）：黑盒/白盒各自独立派发、并行执行时，每次
		// 记录只覆盖该派发对应 mode 的需执行集。
		required := qaExecutionRequiredCases(*state, dispatch.Mode)
		// 空需执行集放行：除合并 QA 零用例既有例外外，本派发 mode 的 qa-review 已记录 PASS
		// （空集 review 判定覆盖充分，需求 4，与快照门 blackboxReviewPassed 的空集语义一致）
		// 时同样放行——零用例场景下 review 判定覆盖充分后 QA 执行对空集直接 PASS，避免 run
		// 卡死在 QA 执行、无法 seal。review 仍 PENDING 或 FAIL 时空集不被放行。RQ-001：按
		// 本派发 mode 的 review 结果判定，不读另一 mode。
		if len(required) == 0 && !isMergeVerification(*state) && !snapshotBlackboxReleased(*state) && state.qaReview(dispatch.Mode).Status != "PASS" {
			return fmt.Errorf("approved QA cases are missing")
		}
		if len(results) != len(required) {
			return fmt.Errorf("QA execution must cover all %d approved cases", len(required))
		}
		byID := map[string]QAResultInput{}
		for _, item := range results {
			if _, exists := byID[item.CaseID]; exists {
				return fmt.Errorf("duplicate QA result for %s", item.CaseID)
			}
			if item.Outcome != "PASS" && item.Outcome != "FAIL" {
				return fmt.Errorf("QA outcome for %s must be PASS or FAIL", item.CaseID)
			}
			for name, value := range map[string]string{"procedure": item.Procedure, "observation": item.Observation, "oracle result": item.OracleResult} {
				if strings.TrimSpace(value) == "" {
					return fmt.Errorf("QA result %s %s is required", item.CaseID, name)
				}
			}
			byID[item.CaseID] = item
		}
		// RQ-012：执行结果按 mode 分开存储——每个 mode 的结果只含本 mode 的用例记录，黑盒/
		// 白盒各自独立、互不累计（另一 mode 的结果在 QAExecutionByModel[otherMode] 原样保持）。
		// 同一 mode 的重复记录由 qaExecutionModeResulted 挡在入口。
		recorded := make([]QAResultRecord, 0, len(required))
		for _, testCase := range required {
			item, ok := byID[testCase.ID]
			if !ok {
				return fmt.Errorf("QA result is missing for %s", testCase.ID)
			}
			recorded = append(recorded, QAResultRecord{CaseID: item.CaseID, Mode: testCase.Mode, Outcome: item.Outcome, Procedure: strings.TrimSpace(item.Procedure), Observation: strings.TrimSpace(item.Observation), OracleResult: strings.TrimSpace(item.OracleResult), Origin: "executed"})
		}
		// 快照放行后未获批准的黑盒用例：经用户授权跳过、验证状态视为 PASS（记录授权来源）。
		// 仅黑盒或合并（空 mode）派发补记这些跳过；白盒派发不补记黑盒用例。
		if snapshotBlackboxReleased(*state) && (dispatch.Mode == "" || dispatch.Mode == "blackbox") {
			for _, testCase := range state.qaModeCases("blackbox") {
				if testCase.ReviewStatus == "PASS" {
					continue
				}
				recorded = append(recorded, QAResultRecord{CaseID: testCase.ID, Mode: testCase.Mode, Outcome: "PASS", Procedure: "skipped", Observation: "authorized skip (user snapshot release)", OracleResult: "authorized PASS", Origin: "executed"})
			}
		}
		// RQ-005：AFFECTED 重跑下未覆盖的已批准用例继承上一轮 PASS——追加 inherited 记录
		// （恒 PASS、不参与 FAIL 聚合），观察记录继承来源快照，供审计与聚合区分。RQ-012：
		// 继承范围是该派发 mode 自己的用例。
		if sc, ok := qaExecutionAffectedScope(*state, dispatch.Mode); ok {
			covered := map[string]bool{}
			for _, record := range recorded {
				covered[record.CaseID] = true
			}
			for _, testCase := range state.qaModeCases(dispatch.Mode) {
				if testCase.ReviewStatus != "PASS" || covered[testCase.ID] {
					continue
				}
				recorded = append(recorded, QAResultRecord{CaseID: testCase.ID, Mode: testCase.Mode, Outcome: "PASS", Procedure: "inherited", Observation: "inherited PASS from " + sc.BaseSnapshot, OracleResult: "inherited", Origin: "inherited"})
			}
		}
		// 本派发 mode 的状态与发现项：该 mode 任一【经执行】用例 FAIL 即该 mode FAIL（RQ-012
		// 按 mode 独立）；发现项从本 mode 的用例集重新生成，保证返修输入保留各 mode 的失败
		// 原因。继承用例恒 PASS、不参与 FAIL 判定（RQ-005）。
		status := "PASS"
		findings := []Finding{}
		for _, existing := range recorded {
			if existing.Origin == "inherited" || existing.Outcome != "FAIL" {
				continue
			}
			status = "FAIL"
			findings = append(findings, Finding{Message: existing.CaseID + ": " + existing.Observation})
		}
		message := ""
		if isMergeVerification(*state) && len(state.allQACases()) == 0 {
			message = "切片基本独立、无跨切片交互用例"
		}
		state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: status, Snapshot: state.CurrentSnapshot, Cases: recorded, Findings: findings, Message: message})
		// 本轮权威结果（PASS/FAIL）记录时只取代本 mode 存续的上一轮结果（RQ-009 / RQ-012：
		// 一个 mode 记录 SHALL NOT 清空另一 mode 的 PriorQAExecution）；RUNTIME_ERROR 分支
		// 在上面提前返回、不清空本 mode 的 prior。
		state.deletePriorQAExecution(dispatch.Mode)
		completeDispatch(state, dispatch.ID)
		completeReviewWaveIfReady(state)
		return nil
	})
}

// AdvanceSnapshot records the development or repair snapshot. The snapshot gate
// requires development complete 且 黑盒 qa-review PASS（两边都完成）；黑盒 review 未
// PASS 时快照被挡。userRequested 是用户显式的手动放行授权（类比 --user-requested），
// 记录授权来源到 SnapshotOverride，使黑盒门未通过时带风险继续；未获批准的黑盒用例
// 验证状态视为 PASS、qa-execution 只覆盖已批准用例。
func AdvanceSnapshot(root, packageRoot, runID, dispatchID string, userRequested bool, reason string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		currentSnapshot, err := resolveNativeSnapshot(root, state.VCS)
		if err != nil {
			return err
		}
		if err := verifySnapshotReady(root, state.VCS); err != nil {
			return err
		}
		developmentStatus := state.Actions["development-worker"].Status
		// 快照要求开发侧真正完成：产生开发提交（原生标识前进到派发源快照之后），而非仅
		// PREPARED 状态。dev worker 已派发但未提交时，快照不得直接把基线记为开发快照
		// （需求 2 验收"任一未完成时 snapshot 被挡"）。
		if currentSnapshot == state.CurrentSnapshot {
			return fmt.Errorf("a new current snapshot is required")
		}
		if err := verifyNativeSnapshot(root, state.VCS, state.CurrentSnapshot); err != nil {
			return err
		}
		if err := requireTransition(*state, "snapshot", ""); err != nil {
			return err
		}
		var developmentDispatch PreparedDispatch
		if state.RetainedOverall {
			if strings.TrimSpace(dispatchID) != "" {
				return fmt.Errorf("a retained overall snapshot does not accept a development dispatch")
			}
		} else {
			developmentDispatch, err = requirePreparedDispatch(*state, dispatchID, "action", "development-worker")
			if err != nil {
				return err
			}
			if err := requireLifecycleVerification(root, *state, developmentDispatch); err != nil {
				return err
			}
			backfillDispatchCost(root, state, developmentDispatch)
		}
		// 快照黑盒门（等两边都完成）：黑盒 qa-review PASS 且 开发完成才可快照。黑盒
		// qa-review 未 PASS 且此前没有用户放行时，只有用户显式授权可手动放行并记录授权
		// 来源；已放行（SnapshotOverride 非空）后未批准的黑盒用例验证状态视为 PASS，
		// 后续修复快照不再重复被挡。黑盒 review 真正 PASS 时清除放行授权。
		blackboxSelected := isSelected(*state, blackboxQAID) || isSelected(*state, legacyQAID)
		if blackboxSelected && !blackboxReviewPassed(*state) && state.SnapshotOverride == nil {
			if !userRequested {
				return fmt.Errorf("blackbox QA Review must pass before a development snapshot; development and blackbox QA review both need to complete")
			}
			state.SnapshotOverride = &SnapshotOverride{Origin: "USER", Snapshot: currentSnapshot, Message: strings.TrimSpace(reason)}
		} else if blackboxSelected && blackboxReviewPassed(*state) {
			state.SnapshotOverride = nil
		}
		oldSnapshot := state.CurrentSnapshot
		isRepair := developmentStatus == developmentRepairPrepared ||
			(state.RetainedOverall && (developmentStatus == developmentComplete || developmentStatus == developmentVerified))
		state.CurrentSnapshot = currentSnapshot
		state.Actions["development-worker"] = ActionResult{Status: developmentComplete, DispatchID: developmentDispatch.ID}
		if developmentDispatch.ID != "" {
			completeDispatch(state, developmentDispatch.ID)
		}
		if isRepair {
			state.PreRepairSnapshot = oldSnapshot
		} else {
			state.PreRepairSnapshot = ""
		}
		resetSnapshotReviewSurface(state, oldSnapshot, isRepair, isRepair)
		return nil
	})
}

// resetSnapshotReviewSurface re-opens the post-snapshot review surface shared
// by a development snapshot and an adopted external change: QA Execution is
// kept only when it already passed at the previous snapshot and preserveQA is
// set, every recorded Carry judgment and seal-scoped skip authorization is
// dropped, non-PASS selected gates return to PENDING, and the Carry action is
// re-opened when reopenCarry is set and prior passing gates are eligible.
func resetSnapshotReviewSurface(state *RunState, oldSnapshot string, preserveQA, reopenCarry bool) {
	// RQ-009：修复快照推进不得抹掉上一快照的权威结果（PASS/FAIL，含快照与 FAIL 用例集）。
	// 在把每个 mode 的 QAExecution 重置为 PENDING 前，若其为权威结果且已落在旧快照，先保留
	// 到该 mode 的 PriorQAExecutionByMode，供重跑识别（RQ-002）与 AFFECTED 子集判定
	// （RQ-004）使用；RUNTIME_ERROR 不构成权威结果、直接重置不保留。RQ-012：按 mode 独立
	// 保留与重置，一个 mode 不触碰另一 mode。被新一轮权威结果取代时由 RecordQAExecution
	// 只清空该 mode 的 prior。
	for _, mode := range state.qaExecutionModes() {
		result := state.qaExecution(mode)
		if (result.Status == "PASS" || result.Status == "FAIL") && result.Snapshot != "" && result.Snapshot != state.CurrentSnapshot {
			state.setPriorQAExecution(mode, result)
		}
		if !preserveQA || result.Status != "PASS" || result.Snapshot != oldSnapshot {
			state.setQAExecution(mode, QAExecutionResult{Status: "PENDING"})
		}
	}
	state.Carry = map[string]CarryResult{}
	for id, authorization := range state.SkipAuthorizations {
		if isSealScopedAuthorization(authorization) {
			delete(state.SkipAuthorizations, id)
		}
	}
	for id, result := range state.Gates {
		if !isSelected(*state, id) {
			continue
		}
		if result.Status != "PASS" {
			state.Gates[id] = GateResult{Status: "PENDING"}
		}
	}
	if reopenCarry && len(eligibleCarryGates(*state)) != 0 {
		state.Actions["carry"] = ActionResult{Status: "PENDING"}
	} else {
		delete(state.Actions, "carry")
	}
}

func RecordCarry(root, packageRoot, runID, dispatchID string, decisions []CarryInput, runtimeError string, mainAgent bool, mainReason string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if mainAgent {
			if !hasDevelopmentSnapshot(*state) {
				return fmt.Errorf("a development snapshot is required before a main-agent Carry")
			}
		} else if err := requireTransition(*state, "carry", ""); err != nil {
			return err
		}
		if mainAgent {
			if len(decisions) != 0 || strings.TrimSpace(runtimeError) != "" {
				return fmt.Errorf("main-agent Carry cannot include independent decisions or a runtime error")
			}
			if strings.TrimSpace(dispatchID) != "" {
				return fmt.Errorf("main-agent Carry does not accept an independent dispatch")
			}
			reason := strings.TrimSpace(mainReason)
			if reason == "" {
				return fmt.Errorf("main-agent Carry requires a reason")
			}
			delta := catalogDelta(*state, catalog)
			// 存在目录变更（RQ-005 第三类：检测到的中途修改 / 目录接受）时，主代理 Carry
			// 是既定的处置入口，即使先前已记录过独立 Carry 判定也可用于新变更的处置
			// （B3：该入口在存在受影响记录结果时可用，含未发生修复的中途修改场景）。
			if len(delta) == 0 && (hasRecordedCarryDecisions(*state) || (state.PreRepairSnapshot != "" && repairRerunRecorded(*state))) {
				return fmt.Errorf("main-agent Carry must be recorded before independent Carry or repair reruns")
			}
			if len(delta) != 0 {
				state.CatalogRevision = catalog.CatalogRevision
				state.BasePromptRevision = catalog.BaseRevision
				acceptCatalogHashes(state, catalog)
			}
			eligible := eligibleMainCarryResults(*state, len(delta) != 0)
			if len(eligible) == 0 && len(delta) == 0 {
				return fmt.Errorf("no prior passing selected results are eligible for main-agent Carry")
			}
			source := state.PreRepairSnapshot
			if source == "" {
				source = state.CurrentSnapshot
			}
			for _, id := range eligible {
				inheritCarryResult(state, id, carryOriginMainShortcut, reason, source)
			}
			state.Actions["carry"] = ActionResult{Status: "PASS"}
			if state.PreRepairSnapshot != "" {
				resolveCarryBoundary(state)
			}
			return nil
		}
		if strings.TrimSpace(mainReason) != "" {
			return fmt.Errorf("--main-reason requires main-agent Carry")
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", "carry")
		if err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
		eligible := eligibleCarryGates(*state)
		if len(eligible) == 0 {
			return fmt.Errorf("no prior passing gates require a Carry decision")
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(decisions) != 0 {
				return fmt.Errorf("Carry runtime error cannot include decisions")
			}
			state.Actions["carry"] = ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), DispatchID: dispatch.ID}
			completeDispatch(state, dispatch.ID)
			completeReviewWaveIfReady(state)
			return nil
		}
		if len(decisions) != len(eligible) {
			return fmt.Errorf("Carry must decide all %d prior passing gates", len(eligible))
		}
		wanted := map[string]bool{}
		for _, id := range eligible {
			wanted[id] = true
		}
		seen := map[string]bool{}
		for _, decision := range decisions {
			if !wanted[decision.GateID] {
				return fmt.Errorf("gate %q is not eligible for Carry", decision.GateID)
			}
			if seen[decision.GateID] {
				return fmt.Errorf("duplicate Carry decision for %s", decision.GateID)
			}
			if decision.Decision != "INHERIT" && decision.Decision != "RERUN" {
				return fmt.Errorf("Carry decision for %s must be INHERIT or RERUN", decision.GateID)
			}
			if strings.TrimSpace(decision.Message) == "" {
				return fmt.Errorf("Carry decision for %s requires a reason", decision.GateID)
			}
			seen[decision.GateID] = true
			if decision.Decision == "INHERIT" {
				inheritCarryResult(state, decision.GateID, carryOriginIndependent, strings.TrimSpace(decision.Message), state.PreRepairSnapshot)
			} else {
				state.Carry[decision.GateID] = CarryResult{Decision: decision.Decision, Origin: carryOriginIndependent, SourceSnapshot: state.PreRepairSnapshot, TargetSnapshot: state.CurrentSnapshot, Message: strings.TrimSpace(decision.Message)}
				state.Gates[decision.GateID] = GateResult{Status: "PENDING"}
			}
		}
		state.Actions["carry"] = ActionResult{Status: "PASS", DispatchID: dispatch.ID}
		completeDispatch(state, dispatch.ID)
		resolveCarryBoundary(state)
		return nil
	})
}

// eligibleMainCarryResults lists the selected prior PASS results a main-agent
// Carry may inherit. In a repair or adoption the previous snapshot is the
// pre-repair boundary; at a catalog-delta rebinding the PASS results at the
// current snapshot are the ones whose judgment is being recorded.
func eligibleMainCarryResults(state RunState, catalogChanged bool) []string {
	if state.PreRepairSnapshot == "" && !catalogChanged {
		return nil
	}
	ids := []string{}
	// RQ-012：按 mode 各自独立判定 QA 结果的 carry 资格；每个 PASS 的选中 QA mode 都是
	// 独立可继承结果（各自发出一个 QA 模式 id，使 inheritCarryResult 按 isQAMode 把该
	// mode 的结果快照重绑，而不写进虚假的 Gates["qa"]）。
	for id := range selectedSet(state) {
		if !isQAMode(id) {
			continue
		}
		// RQ-002：直取该 mode 的执行结果（不再经过只返回 current-snapshot 结果的
		// qaModeResult / qaModeResultKey），使修复快照（PreRepairSnapshot）之前已 PASS 的
		// QA mode 可被 main-agent Carry 继承；单派发/legacy 合并流程经 "" 键回退取合并结果。
		result := qaModeCarryResult(state, qaDispatchMode(id))
		if result.Status != "PASS" {
			continue
		}
		eligible := state.PreRepairSnapshot != "" && result.Snapshot == state.PreRepairSnapshot
		eligible = eligible || (catalogChanged && result.Snapshot == state.CurrentSnapshot)
		if eligible {
			ids = append(ids, id)
		}
	}
	for id, result := range state.Gates {
		if !isSelected(state, id) || result.Status != "PASS" {
			continue
		}
		if state.PreRepairSnapshot != "" && result.Snapshot == state.PreRepairSnapshot {
			ids = append(ids, id)
		} else if catalogChanged && result.Snapshot == state.CurrentSnapshot {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func repairRerunRecorded(state RunState) bool {
	if isSelectedQA(state) {
		for id := range selectedSet(state) {
			if !isQAMode(id) {
				continue
			}
			result := qaModeResult(state, qaDispatchMode(id))
			if result.Snapshot == state.CurrentSnapshot && result.Status != "" && result.Status != "PENDING" {
				return true
			}
		}
	}
	for id := range selectedSet(state) {
		if isQAMode(id) {
			continue
		}
		result := state.Gates[id]
		if result.Snapshot == state.CurrentSnapshot && result.Status != "" && result.Status != "PENDING" {
			return true
		}
	}
	return false
}

func inheritCarryResult(state *RunState, id, origin, reason, source string) {
	state.Carry[id] = CarryResult{Decision: "INHERIT", Origin: origin, SourceSnapshot: source, TargetSnapshot: state.CurrentSnapshot, Message: reason}
	if isQAMode(id) {
		// RQ-002：只重绑该 QA 模式的有效执行结果快照（per-mode 或合并单派发结果），另一
		// mode 不受影响。用与 eligibleMainCarryResults 相同的任意快照读取（qaModeCarryResultKey），
		// 保证写回键就是读取键——修复快照前已 PASS 的 per-mode 结果也能被正确重绑到当前快照。
		key, result := qaModeCarryResultKey(*state, qaDispatchMode(id))
		result.Snapshot = state.CurrentSnapshot
		state.setQAExecution(key, result)
		return
	}
	prior := state.Gates[id]
	prior.SourceSnapshot = source
	prior.Snapshot = state.CurrentSnapshot
	state.Gates[id] = prior
}

// hasRecordedCarryDecisions reports whether any non-adoption Carry judgment has
// been recorded; an adoption's provenance under the reserved adoption key does
// not count as a decision, so a main-agent Carry still records the first one.
func hasRecordedCarryDecisions(state RunState) bool {
	for id := range state.Carry {
		if id != carryAdoptKey {
			return true
		}
	}
	return false
}

// isAdoptionBoundary reports whether the current pre-repair boundary was created
// by an adopted external change rather than by a repair snapshot: the reserved
// adoption key still points its target at the current snapshot. A later repair
// snapshot drops the adoption provenance through resetSnapshotReviewSurface, so
// this stays true only while the adoption itself is the boundary being resolved.
func isAdoptionBoundary(state RunState) bool {
	adopted, ok := state.Carry[carryAdoptKey]
	return ok && adopted.TargetSnapshot == state.CurrentSnapshot && state.PreRepairSnapshot != ""
}

// acceptCatalogHashes records the current per-entry prompt hashes while
// preserving the recorded hash of every gate whose composed prompt changed, so
// a selected gate whose prompt moved stays re-dispatchable until a fresh review
// at the accepted catalog settles it.
func acceptCatalogHashes(state *RunState, catalog PromptCatalog) {
	current := catalogPromptHashes(catalog)
	merged := make(map[string]string, len(current))
	for id, hash := range current {
		if strings.HasPrefix(id, "gate:") {
			if recorded, ok := state.PromptHashes[id]; ok && recorded != hash {
				merged[id] = recorded
				continue
			}
		}
		merged[id] = hash
	}
	state.PromptHashes = merged
}

func AuthorizeExtraRepair(root, packageRoot, runID string, cycles int, scopes []QAScopeInput) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		if cycles != 1 {
			return fmt.Errorf("each extra repair authorization must add exactly one review wave")
		}
		if state.CompletedReviewWaves < effectiveReviewWaveLimit(*state) {
			return fmt.Errorf("automatic review waves are not exhausted")
		}
		if !hasRepairableBlocker(*state) && !hasSuggestionRecommendation(*state) {
			return fmt.Errorf("no recorded result requires another repair")
		}
		// RQ-006/007：轮次上限用尽后每一轮额外修复都须显式授权（carry-forward 不授予轮次，
		// 见 bundleRerunScopes）；QA 被选中且当前快照存在某 mode 的权威 FAIL 结果（该 mode
		// 将重跑）时，scope 决策在同一个交互中打包询问/记录（多 mode 各自一份、可不同）。
		if err := bundleRerunScopes(state, scopes); err != nil {
			return err
		}
		state.ExtraReviewWaves += cycles
		return nil
	})
}

// qaRerunModes returns the QA dispatch modes that have an authoritative FAIL result
// at the current snapshot and will therefore be rerun after the next repair snapshot
// (RQ-007). Blackbox and whitebox each need their own scope decision; merge QA and
// the legacy "qa" id use the merged empty mode. A mode with no recorded cases has
// nothing to rerun and needs no scope decision.
func qaRerunModes(state RunState) []string {
	var modes []string
	if !isSelectedQA(state) {
		return modes
	}
	// RQ-012：合并单派发流程（合并 "" 结果权威）下整个合并集重跑，只需一个合并 scope。
	if qaUsesMergedExecution(state) {
		if state.qaExecution("").Status == "FAIL" {
			return []string{""}
		}
		return modes
	}
	// RQ-012：任一选中 mode 当前快照有权威 FAIL 结果即触发打包（该 mode 将重跑）；此时
	// 所有在当前快照有权威结果的选中 mode（FAIL 或 PASS）都会在修复快照推进后重跑（PASS
	// 结果成为上一轮），因此都需要 scope 决策。
	anyFail := false
	for id := range selectedSet(state) {
		if !isQAMode(id) {
			continue
		}
		if result := state.qaExecution(qaDispatchMode(id)); result.Status == "FAIL" && result.Snapshot == state.CurrentSnapshot {
			anyFail = true
			break
		}
	}
	if !anyFail {
		return modes
	}
	seen := map[string]bool{}
	for id := range selectedSet(state) {
		if !isQAMode(id) {
			continue
		}
		mode := qaDispatchMode(id)
		result := state.qaExecution(mode)
		if result.Snapshot != state.CurrentSnapshot || result.Status == "" || result.Status == "PENDING" {
			continue
		}
		if !seen[mode] {
			modes = append(modes, mode)
			seen[mode] = true
		}
	}
	sort.Strings(modes)
	return modes
}

// bundleRerunScopes enforces the limit-point scope bundling (RQ-007): when QA is
// selected and the current snapshot holds an authoritative FAIL result for a
// dispatch mode (that mode will be rerun), the mode must carry a scope decision
// covering the current snapshot. A pre-recorded scope wins; otherwise the inline
// authorize-repair scope input records it with Source AUTHORIZE_REPAIR, or, when the
// last recorded decision was a user-chosen AFFECTED (Source != CARRY_FORWARD),
// auto-carries it as CARRY_FORWARD with the host-judged subset without asking the
// user again (RQ-007/008). Each such authorization still grants exactly one extra
// review wave.
func bundleRerunScopes(state *RunState, scopes []QAScopeInput) error {
	if !isSelectedQA(*state) {
		return nil
	}
	modes := qaRerunModes(*state)
	if len(modes) == 0 {
		return nil
	}
	byMode := map[string]QAScopeInput{}
	for _, input := range scopes {
		mode := strings.TrimSpace(input.Mode)
		if _, dup := byMode[mode]; dup {
			return fmt.Errorf("duplicate QA scope for mode %q", qaModeLabel(mode))
		}
		byMode[mode] = input
	}
	for _, mode := range modes {
		existing, ok := state.ExecutionScopes[mode]
		if ok && existing.BaseSnapshot == state.CurrentSnapshot {
			// 已有一份覆盖当前 FAIL 快照的 scope 决策，无需再记录。
			continue
		}
		input, has := byMode[mode]
		if !has {
			return fmt.Errorf("QA Execution rerun requires a scope decision for mode %q: run `workflow qa-execution-scope --mode %s --decision FULL|AFFECTED ...` or pass --qa-scope to this authorize-repair call", qaModeLabel(mode), mode)
		}
		source := scopeSourceAuthorizeRepair
		if ok && existing.Decision == "AFFECTED" && existing.Source != scopeSourceCarryForward && strings.TrimSpace(input.Decision) == "" {
			// RQ-007：最近一次是用户主动选择 AFFECTED 且 host 未显式改选时自动沿用子集，
			// 不再询问"全量 vs 受影响"；子集扩展由 host 自行决定、不要求用户确认。产品审
			// P2：host 若显式给出新的 decision（如 --qa-scope <mode>=FULL 升级为全量），
			// 则以新 decision 为准，而不是被强制沿用 AFFECTED。
			source = scopeSourceCarryForward
			input.Decision = "AFFECTED"
		}
		// RQ-012：prior 按 mode 取该 mode 自己的权威结果。
		if err := recordExecutionScope(state, mode, input.Decision, input.CaseIDs, input.Reason, source, state.CurrentSnapshot, state.qaExecution(mode)); err != nil {
			return err
		}
	}
	return nil
}

// recordExecutionScope validates and records one QA execution scope decision for the
// mode (RQ-003/004). prior is the authoritative QA execution result this rerun
// inherits from — its FAIL cases are mandatory members of an AFFECTED subset;
// baseSnapshot is that result's snapshot. source records how the decision was
// captured: PREPARE for the standalone workflow qa-execution-scope command,
// AUTHORIZE_REPAIR or CARRY_FORWARD for the bundled authorize-repair path.
func recordExecutionScope(state *RunState, mode, decision string, caseIDs []string, reason, source, baseSnapshot string, prior QAExecutionResult) error {
	mode = strings.TrimSpace(mode)
	decision = strings.ToUpper(strings.TrimSpace(decision))
	if mode != "blackbox" && mode != "whitebox" && mode != "" {
		return fmt.Errorf("invalid QA execution scope mode %q (want blackbox, whitebox, or empty for the merged set)", mode)
	}
	if decision != "FULL" && decision != "AFFECTED" {
		return fmt.Errorf("QA execution scope decision must be FULL or AFFECTED")
	}
	normalized := []string{}
	seen := map[string]bool{}
	for _, raw := range caseIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if seen[id] {
			return fmt.Errorf("duplicate case %s in QA execution scope", id)
		}
		seen[id] = true
		normalized = append(normalized, id)
	}
	if decision == "AFFECTED" {
		if len(normalized) == 0 {
			return fmt.Errorf("AFFECTED QA execution scope requires a non-empty case subset")
		}
		// 子集必须是该 mode 已批准用例的子集，且包含继承来源（prior）中该 mode 的全部
		// FAIL 用例。受连带影响的既往通过用例由 host 综合判定、自行扩展子集，CLI 只做
		// 机械校验、不要求用户逐项确认（RQ-004/008）。
		// RQ-012：子集校验按该 mode 自己的用例读取（跨 mode 视图按 mode 过滤）。
		approved := map[string]bool{}
		for _, testCase := range state.qaModeCases(mode) {
			if testCase.ReviewStatus == "PASS" {
				approved[testCase.ID] = true
			}
		}
		for _, id := range normalized {
			if !approved[id] {
				return fmt.Errorf("case %s is not an approved %s QA case", id, qaModeLabel(mode))
			}
		}
		for _, record := range prior.Cases {
			if mode != "" && record.Mode != mode {
				continue
			}
			if record.Outcome != "FAIL" {
				continue
			}
			if !seen[record.CaseID] {
				return fmt.Errorf("AFFECTED scope must include the prior FAIL case %s", record.CaseID)
			}
		}
	}
	if state.ExecutionScopes == nil {
		state.ExecutionScopes = map[string]QAExecutionScope{}
	}
	state.ExecutionScopes[mode] = QAExecutionScope{Decision: decision, Mode: mode, BaseSnapshot: baseSnapshot, CaseIDs: normalized, Reason: strings.TrimSpace(reason), Origin: "USER", Source: source}
	return nil
}

// RecordExecutionScope records a QA execution rerun scope decision for the mode via
// the standalone workflow qa-execution-scope command (RQ-002/003). The mode must
// have an authoritative QA execution result to rerun (first execution needs no
// scope); the recorded BaseSnapshot is that result's snapshot. Source is PREPARE.
func RecordExecutionScope(root, packageRoot, runID, mode, decision string, caseIDs []string, reason string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		mode = strings.TrimSpace(mode)
		// RQ-001：scope 决策按 mode 校验该 mode 的 design/review 前置（target=mode）。
		if err := requireTransition(*state, "qa-execution", mode); err != nil {
			return err
		}
		prior, base, ok := qaExecutionRerunSource(*state, mode)
		if !ok {
			return fmt.Errorf("this mode has no authoritative QA execution result to rerun, so a scope decision is not required (a first execution defaults to the full approved set)")
		}
		return recordExecutionScope(state, mode, decision, caseIDs, reason, scopeSourcePrepare, base, prior)
	})
}

func Abort(root, runID string) (RunSummary, error) { return finishRun(root, runID, "ABORTED") }

// Seal aggregates the run. For git runs whose base→current range holds more than
// one commit, the range is squashed into a single commit (git reset --soft base +
// a fresh commit with --squash-message) as the last VCS operation, preserving the
// final tree. The summary's current snapshot records the squashed commit, the base
// stays unchanged, gate-review compared records keep history, and the squashed
// message is authored by the main agent (host-provided). Single-commit or empty
// ranges are not rewritten; SVN/P4 are never squashed.
func Seal(root, packageRoot, runID string, skips []string, userRequested bool, squashMessage string) (RunSummary, error) {
	path := RunStatePath(root, runID)
	release, err := acquireStateLock(path)
	if err != nil {
		return RunSummary{}, err
	}
	defer release()
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunSummary{}, err
	}
	if err := requireActive(state); err != nil {
		return RunSummary{}, err
	}
	catalog, err := requireCurrentDefinitions(root, state, packageRoot)
	if err != nil {
		return RunSummary{}, err
	}
	if err := requireNoPendingInheritance(root, state, catalog); err != nil {
		return RunSummary{}, err
	}
	before, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return RunSummary{}, err
	}
	if before != state.CurrentSnapshot {
		return RunSummary{}, fmt.Errorf("native VCS identity does not match the current snapshot before aggregation")
	}
	if err := authorizeSealSkips(&state, skips, userRequested); err != nil {
		return RunSummary{}, err
	}
	if err := requireTransition(state, "seal", ""); err != nil {
		if saveErr := SaveRunState(root, state); saveErr != nil {
			return RunSummary{}, saveErr
		}
		return RunSummary{}, err
	}
	after, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return RunSummary{}, err
	}
	if after != state.CurrentSnapshot {
		return RunSummary{}, fmt.Errorf("native VCS identity does not match the current snapshot after aggregation")
	}
	// 压缩提交（仅 Git、自动）：校验通过后、落 summary 前执行，作为 seal 的最后一步
	// VCS 操作。压缩前确认工作树干净；单条提交或空范围不操作。
	if state.VCS == "git" {
		count, err := gitCommitCountInRange(root, state.BaseSnapshot, state.CurrentSnapshot)
		if err != nil {
			return RunSummary{}, err
		}
		if count > 1 {
			if err := verifySnapshotReady(root, state.VCS); err != nil {
				return RunSummary{}, fmt.Errorf("seal git squash requires a clean working tree: %w", err)
			}
			message := strings.TrimSpace(squashMessage)
			if message == "" {
				return RunSummary{}, fmt.Errorf("seal git squash requires --squash-message for the combined commit")
			}
			if err := squashGitRangeToBase(root, state.BaseSnapshot, message); err != nil {
				return RunSummary{}, err
			}
			newSnapshot, err := resolveNativeSnapshot(root, state.VCS)
			if err != nil {
				return RunSummary{}, err
			}
			// 最终树不变，所有审查结果对最终树仍成立：快照引用重绑到压缩后的提交，
			// 门审查 compared 记录保持历史（不重写）。
			rebindCurrentSnapshot(&state, newSnapshot)
		}
	}
	state.Status = "SEALED"
	if err := SaveRunState(root, state); err != nil {
		return RunSummary{}, err
	}
	if err := SaveRunSummary(root, state); err != nil {
		return RunSummary{}, err
	}
	summary := runSummary(state)
	if err := DeleteRun(root, runID); err != nil {
		return RunSummary{}, err
	}
	_, _ = CleanupTempRuns(root) // best-effort sweep of residual terminated runs
	return summary, nil
}

func finishRun(root, runID, status string) (RunSummary, error) {
	path := RunStatePath(root, runID)
	release, err := acquireStateLock(path)
	if err != nil {
		return RunSummary{}, err
	}
	defer release()
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunSummary{}, err
	}
	if err := requireActive(state); err != nil {
		return RunSummary{}, err
	}
	state.Status = status
	if err := SaveRunState(root, state); err != nil {
		return RunSummary{}, err
	}
	if err := SaveRunSummary(root, state); err != nil {
		return RunSummary{}, err
	}
	summary := runSummary(state)
	if err := DeleteRun(root, runID); err != nil {
		return RunSummary{}, err
	}
	_, _ = CleanupTempRuns(root) // best-effort sweep of residual terminated runs
	return summary, nil
}

func mutateRun(root, runID string, change func(*RunState) error) (RunState, error) {
	path := RunStatePath(root, runID)
	release, err := acquireStateLock(path)
	if err != nil {
		return RunState{}, err
	}
	defer release()
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunState{}, err
	}
	if err := requireActive(state); err != nil {
		return RunState{}, err
	}
	if err := change(&state); err != nil {
		return RunState{}, err
	}
	if err := SaveRunState(root, state); err != nil {
		return RunState{}, err
	}
	return state, nil
}

func acquireStateLock(statePath string) (func(), error) {
	lockPath := statePath + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			file.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for another run-state update")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func requireActive(state RunState) error {
	if state.Status != "ACTIVE" {
		return fmt.Errorf("run %s is %s", state.RunID, state.Status)
	}
	return nil
}

func requireCurrentCatalog(state RunState, packageRoot string) (PromptCatalog, error) {
	catalog, err := LoadPromptCatalog(packageRoot)
	if err != nil {
		return PromptCatalog{}, err
	}
	// Catalog content changes are a recoverable classification, not a run
	// killer: the run continues with the live catalog, per-gate/action deltas
	// are reported, and unaffected results are inherited by a Carry judgment.
	return catalog, nil
}

func requireCurrentDefinitions(root string, state RunState, packageRoot string) (PromptCatalog, error) {
	catalog, err := requireCurrentCatalog(state, packageRoot)
	if err != nil {
		return PromptCatalog{}, err
	}
	changed, err := requirementArtifactsChanged(root, state.RequirementArtifacts)
	if err != nil {
		return PromptCatalog{}, err
	}
	if changed {
		if developmentStarted(state) {
			return PromptCatalog{}, fmt.Errorf("frozen requirement artifact changed; return to requirement clarification before continuing")
		}
		return PromptCatalog{}, fmt.Errorf("requirement artifacts changed; resume the run before continuing")
	}
	return catalog, nil
}

func requireNativeCurrent(root string, state RunState) (string, error) {
	current, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return "", err
	}
	if current != state.CurrentSnapshot {
		return "", fmt.Errorf("native VCS identity does not match the current snapshot")
	}
	return current, nil
}

// requireDevelopmentClaimableHead is the relaxed native identity check used when
// claiming a development-worker dispatch. The worker's commit advances native
// HEAD past the dispatch's source snapshot, so any current HEAD that is a
// descendant (or equal) of the source snapshot is accepted as the claim basis.
func requireDevelopmentClaimableHead(root string, state RunState, dispatch PreparedDispatch) error {
	current, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return err
	}
	resolver, err := resolverForVCS(state.VCS, nil)
	if err != nil {
		return err
	}
	if err := resolver.IsAncestorOrEqual(root, dispatch.SourceSnapshot, current); err != nil {
		return fmt.Errorf("native VCS identity does not match the current snapshot")
	}
	return nil
}

func routeForState(root string, state RunState) PromptRoute {
	return PromptRoute{RequirementSource: state.RequirementSource, RequirementRevision: state.RequirementRevision, CatalogRevision: state.CatalogRevision, Worktree: absPath(cleanRoot(root)), VCS: state.VCS, BaseSnapshot: state.BaseSnapshot, CurrentSnapshot: state.CurrentSnapshot, PreRepairSnapshot: state.PreRepairSnapshot, RequirementArtifacts: append([]RequirementArtifact{}, state.RequirementArtifacts...)}
}

func requirePreparedDispatch(state RunState, dispatchID, targetKind, target string) (PreparedDispatch, error) {
	dispatchID = strings.TrimSpace(dispatchID)
	if dispatchID == "" {
		return PreparedDispatch{}, fmt.Errorf("dispatch id is required")
	}
	dispatch, ok := state.Dispatches[dispatchID]
	if !ok {
		return PreparedDispatch{}, fmt.Errorf("unknown dispatch %q", dispatchID)
	}
	if dispatch.TargetKind != targetKind || dispatch.Target != target {
		return PreparedDispatch{}, fmt.Errorf("dispatch %q does not belong to %s %q", dispatchID, targetKind, target)
	}
	recovery := false
	switch dispatch.Status {
	case "CLAIMED":
		if !dispatch.ReviewerRequired || strings.TrimSpace(dispatch.ReviewerIdentity) == "" {
			return PreparedDispatch{}, fmt.Errorf("dispatch %q has no claimed reviewer identity", dispatchID)
		}
	case "OPEN":
		if dispatch.ReviewerRequired {
			return PreparedDispatch{}, fmt.Errorf("dispatch %q is %s and cannot record a result", dispatchID, dispatch.Status)
		}
	case "STALE":
		// RQ-013 恢复路径：STALE 但审查者已认领（ReviewerIdentity 已绑定）且已产出结果的
		// 派发仍可记录（校验身份与结果内容后接受，不重审）。审查阶段快照未变时 source
		// 绑定匹配、恢复记录落当前快照；非常规快照已变情形由下方 source 绑定校验保守拒绝。
		if !dispatch.ReviewerRequired || strings.TrimSpace(dispatch.ReviewerIdentity) == "" {
			return PreparedDispatch{}, fmt.Errorf("dispatch %q is %s and cannot record a result", dispatchID, dispatch.Status)
		}
		recovery = true
	default:
		return PreparedDispatch{}, fmt.Errorf("dispatch %q is %s and cannot record a result", dispatchID, dispatch.Status)
	}
	// 派发源绑定的陈旧校验按 mode 分叉：黑盒 qa-design/qa-review 绑基线（隔离工作区），
	// 其余派发绑当前快照（主工作区）。
	wantedSource := state.CurrentSnapshot
	if isBlackboxQADispatch(dispatch) {
		wantedSource = state.BaseSnapshot
	}
	if dispatch.RequirementRevision != state.RequirementRevision || dispatch.CatalogRevision != state.CatalogRevision || dispatch.SourceSnapshot != wantedSource {
		return PreparedDispatch{}, fmt.Errorf("dispatch %q has stale source bindings", dispatchID)
	}
	if recovery {
		// 防 STALE 记录与替换派发并行记录双记：同功能替换派发在途（OPEN 空票或 CLAIMED
		// 子代理）时拒绝 STALE 记录——host 应以替换派发为准，避免同一功能两个记录落盘。
		for id, candidate := range state.Dispatches {
			if id == dispatchID {
				continue
			}
			if candidate.TargetKind != targetKind || candidate.Target != target || candidate.Mode != dispatch.Mode {
				continue
			}
			if candidate.Status == "OPEN" || candidate.Status == "CLAIMED" {
				return PreparedDispatch{}, fmt.Errorf("dispatch %q is STALE and has a same-function replacement dispatch %s in flight; record %s instead", dispatchID, id, id)
			}
		}
	}
	return dispatch, nil
}

// isBlackboxQADispatch reports whether the dispatch is a blackbox qa-design or
// qa-review round whose identity and source bind to the QA isolation worktree
// (== base) instead of the main worktree (== current).
func isBlackboxQADispatch(dispatch PreparedDispatch) bool {
	return dispatch.Mode == "blackbox" && (dispatch.Target == "qa-design" || dispatch.Target == "qa-review")
}

// requireDispatchNativeCurrent resolves the native identity for a dispatch: a
// blackbox qa-design/qa-review dispatch against the QA isolation worktree
// (== base), every other dispatch against the main worktree (== current).
func requireDispatchNativeCurrent(root string, state RunState, dispatch PreparedDispatch) error {
	if isBlackboxQADispatch(dispatch) {
		return requireIsolatedCurrent(root, state)
	}
	_, err := requireNativeCurrent(root, state)
	return err
}

func completeDispatch(state *RunState, dispatchID string) {
	dispatch := state.Dispatches[dispatchID]
	dispatch.Status = "COMPLETED"
	state.Dispatches[dispatchID] = dispatch
}

// staleOpenDispatches supersedes the prior OPEN empty tickets of the same target
// when a fresh dispatch is claimed (RQ-013). An OPEN dispatch was prepared but
// never claimed, so no subagent was ever dispatched for it (no start event), and
// it must not block or shadow the live claim. Staling is mode-scoped across every
// target (RQ-013): a dispatch whose mode differs from the fresh dispatch's mode
// is a different function (blackbox vs whitebox qa-review / qa-execution, etc.)
// and stays untouched — fixing "whitebox review prepare staled the blackbox
// review". CLAIMED same-function dedup and the manual-termination exception are
// enforced separately at claim time (see ClaimDispatch); prepare no longer calls
// this at all.
func staleOpenDispatches(state *RunState, targetKind, target, mode string) {
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind != targetKind || dispatch.Target != target || dispatch.Status != "OPEN" {
			continue
		}
		if dispatch.Mode != "" && dispatch.Mode != mode {
			continue
		}
		dispatch.Status = "STALE"
		state.Dispatches[id] = dispatch
	}
}

// enforceSameFunctionDedup implements RQ-013's claim-time parallel-dispatch
// guard, the default and only guard against two subagents of the same function
// (same target kind, target, and mode) running at once. Claiming a dispatch is
// rejected when a same-function dispatch is already CLAIMED and its subagent has
// not been terminated (no recorded stop event / interruption reason). A claimed
// dispatch whose subagent was manually terminated (stop event captured) is marked
// STALE so the fresh claim proceeds. OPEN same-function empty tickets are staled
// by staleOpenDispatches and never block a claim (deadlock elimination). The
// staling is transactional: it only persists when the whole claim succeeds.
func enforceSameFunctionDedup(root string, state *RunState, dispatch PreparedDispatch) error {
	staleOpenDispatches(state, dispatch.TargetKind, dispatch.Target, dispatch.Mode)
	for id, prior := range state.Dispatches {
		if id == dispatch.ID {
			continue
		}
		if prior.TargetKind != dispatch.TargetKind || prior.Target != dispatch.Target || prior.Mode != dispatch.Mode {
			continue
		}
		if prior.Status != "CLAIMED" {
			continue
		}
		// 手动终止例外（RQ-013，不新增 CLI 命令）：主代理直接终结前一个同功能子代理后，
		// 生命周期已捕获其 stop 事件并记录中断原因；读得中断原因即把前派发标 STALE、允许
		// 认领该新派发。读不到原因（子代理仍在途）时默认拒绝两个同功能子代理并行。
		reason, err := workflowLifecycle.InterruptionReason(root, state.RunID, id)
		if err != nil {
			return err
		}
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("a claimed %s %q dispatch %s is already in flight for the same function; resume the original agent or terminate its subagent (recording the interruption reason) before claiming a fresh dispatch", dispatch.TargetKind, dispatch.Target, id)
		}
		prior.Status = "STALE"
		state.Dispatches[id] = prior
	}
	return nil
}

func nextDispatchAttempt(state RunState, targetKind, target string, wave int) int {
	attempt := 1
	for _, dispatch := range state.Dispatches {
		if dispatch.TargetKind == targetKind && dispatch.Target == target && dispatch.ReviewWave == wave && dispatch.Attempt >= attempt {
			attempt = dispatch.Attempt + 1
		}
	}
	return attempt
}

func currentGateReviewWave(state RunState) int {
	if state.CompletedReviewWaves > 0 && state.Actions["development-worker"].Status == developmentVerified {
		return state.CompletedReviewWaves
	}
	return state.CompletedReviewWaves + 1
}

func newDispatchID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "dispatch-" + hex.EncodeToString(value[:]), nil
}

func semanticActionResult(actionID, status, message string, findings []FindingInput, state *RunState) (ActionResult, error) {
	normalized, converted, err := validateSemanticResult(actionID, status, message, findings, false)
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Status: normalized, Message: strings.TrimSpace(message), Findings: converted}, nil
}

func semanticGateResult(status, message string, findings []FindingInput, state *RunState) (GateResult, error) {
	normalized, converted, err := validateSemanticResult("", status, message, findings, true)
	if err != nil {
		return GateResult{}, err
	}
	return GateResult{Status: normalized, Message: strings.TrimSpace(message), Snapshot: state.CurrentSnapshot, Findings: converted}, nil
}

func validateSemanticResult(actionID, status, message string, findings []FindingInput, gateResult bool) (string, []Finding, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "PASS" && status != "FAIL" && status != "RUNTIME_ERROR" {
		return "", nil, fmt.Errorf("status must be PASS, FAIL, or RUNTIME_ERROR")
	}
	reviewAction := actionID == "product-review" || actionID == "start-readiness"
	if status == "PASS" && len(findings) != 0 && !gateResult && !reviewAction {
		return "", nil, fmt.Errorf("PASS cannot include findings")
	}
	if status == "FAIL" && len(findings) == 0 {
		return "", nil, fmt.Errorf("FAIL requires at least one finding")
	}
	if status == "RUNTIME_ERROR" {
		if len(findings) != 0 {
			return "", nil, fmt.Errorf("RUNTIME_ERROR cannot include reviewer findings")
		}
		if strings.TrimSpace(message) == "" {
			return "", nil, fmt.Errorf("RUNTIME_ERROR requires a message")
		}
	}
	converted := make([]Finding, 0, len(findings))
	hasBlocking := false
	for _, input := range findings {
		if strings.TrimSpace(input.Message) == "" {
			return "", nil, fmt.Errorf("finding message is required")
		}
		locations := make([]string, 0, len(input.Locations))
		for _, location := range input.Locations {
			if err := validateFindingLocation(location); err != nil {
				return "", nil, err
			}
			locations = append(locations, strings.TrimSpace(location))
		}
		severity := strings.ToUpper(strings.TrimSpace(input.Severity))
		if gateResult || reviewAction {
			// 门与 product-review / start-readiness 的发现项必须分级 P0/P1/P2/P3（非空）；
			// 仅 P2/P3 的审查记录 PASS 且 P2/P3 建议可见，存在 P0/P1 时记录 FAIL（驳回的
			// P0/P1 由 enforceReviewRule 放行，确认的 P0/P1 置位需重审标记）。
			if severity != "P0" && severity != "P1" && severity != "P2" && severity != "P3" {
				if gateResult {
					return "", nil, fmt.Errorf("gate finding severity must be P0, P1, P2, or P3")
				}
				return "", nil, fmt.Errorf("review finding severity must be P0, P1, P2, or P3")
			}
			if severity == "P0" || severity == "P1" {
				hasBlocking = true
			}
		} else if severity != "" {
			return "", nil, fmt.Errorf("severity is accepted only for discovered-gate findings or product/start-readiness review findings")
		}
		converted = append(converted, Finding{Severity: severity, Message: strings.TrimSpace(input.Message), Locations: locations})
	}
	// 门的 PASS 只允许 P2/P3；product-review/start-readiness 的 PASS 允许携带发现项（仅
	// P2/P3 或已驳回的 P0/P1，由 enforceReviewRule 逐项判定），FAIL 两者都必须含 P0/P1。
	if gateResult && status == "PASS" && hasBlocking {
		return "", nil, fmt.Errorf("PASS can include only P2/P3 findings")
	}
	if (gateResult || reviewAction) && status == "FAIL" && !hasBlocking {
		return "", nil, fmt.Errorf("FAIL requires at least one P0 or P1 finding")
	}
	return status, converted, nil
}

func validateFindingLocation(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("finding location is empty")
	}
	if strings.Contains(value, `\`) || strings.Contains(value, "://") || strings.HasPrefix(value, "/") || (len(value) > 1 && value[1] == ':') {
		return fmt.Errorf("finding location must be repository-relative: %s", value)
	}
	path := value
	for count := 0; count < 2; count++ {
		index := strings.LastIndex(path, ":")
		if index <= 0 {
			break
		}
		if !suffixIsDigits(path[index+1:]) {
			break
		}
		path = path[:index]
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("finding location must be repository-relative: %s", value)
	}
	return nil
}

func rejectFrozenArtifactFindings(state RunState, findings []Finding) error {
	excluded := map[string]bool{}
	for _, artifact := range state.RequirementArtifacts {
		excluded[artifact.Path] = true
	}
	for _, finding := range findings {
		for _, location := range finding.Locations {
			path := location
			for count := 0; count < 2; count++ {
				index := strings.LastIndex(path, ":")
				if index <= 0 || !suffixIsDigits(path[index+1:]) {
					break
				}
				path = path[:index]
			}
			if excluded[filepath.ToSlash(filepath.Clean(path))] {
				return fmt.Errorf("finding location %s is a frozen acceptance artifact and not a review target", location)
			}
		}
	}
	return nil
}

func suffixIsDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func invalidateRequirementResults(state *RunState, gateIDs []string) {
	routeMode := state.RouteMode
	selected := append([]string{}, state.SelectedGates...)
	routeSkips := map[string]SkipAuthorization{}
	for id, authorization := range state.SkipAuthorizations {
		if authorization.Origin == "ROUTE" {
			routeSkips[id] = authorization
		}
	}
	state.Actions = pendingRequirementActions()
	// RQ-008：语义变更作废全部结果时，per-mode review/design 权威结果一并作废——qa-review /
	// qa-design 已移出 Actions（RQ-001 按 mode 存储），失效路径只重置 Actions 会让 per-mode
	// 旧 PASS 残留，快照黑盒门读到旧 PASS 仍放行。这里清空两个按 mode 的权威结果 map，并把
	// 各 mode 用例的 ReviewStatus 置回 PENDING，使门读到旧 PASS 不放行、须对新需求重新设计/重审。
	state.QAReviewByMode = map[string]ActionResult{}
	state.QADesignByMode = map[string]ActionResult{}
	if len(state.QACasesByMode) != 0 {
		reset := map[string][]QACase{}
		for mode, cases := range state.QACasesByMode {
			updated := make([]QACase, 0, len(cases))
			for _, testCase := range cases {
				testCase.ReviewStatus = "PENDING"
				updated = append(updated, testCase)
			}
			reset[mode] = updated
		}
		state.QACasesByMode = reset
	}
	state.QAExecutionByMode = map[string]QAExecutionResult{}
	state.Carry = map[string]CarryResult{}
	// 需求作废重置：一并清空 scope 决策与上一轮执行结果（P2-丙确认：两者同属"上一轮/
	// 历史执行上下文"，随重置对称清除，防止残留决策污染新一轮重跑）。
	state.ExecutionScopes = map[string]QAExecutionScope{}
	state.PriorQAExecutionByMode = map[string]*QAExecutionResult{}
	state.PreRepairSnapshot = ""
	// 语义已变的需求修订改变了原决定的前提，已拍板发现项清单随之清空，允许重新提出。
	state.SettledFindings = map[string][]SettledFinding{}
	// 需求作废重置：一并清空黑盒隔离工作区、快照放行授权、连续 FAIL 计数与"需重审"标记。
	state.QAWorktree = ""
	state.SnapshotOverride = nil
	state.BlackboxReviewFails = 0
	state.NeedsReReview = map[string]string{}
	state.ReReviewDispatch = map[string]string{}
	state.ReviewOverrides = map[string]string{}
	// RQ-012：meaning-changed 语义变更清空逐项审查表，全量重审；meaning-preserved 重绑
	// 不清表（rebindCurrentSnapshot 不触碰本表）。
	state.ReviewItemsByAction = map[string]map[string]ReviewItem{}
	state.Gates = map[string]GateResult{}
	for _, id := range gateIDs {
		state.Gates[id] = GateResult{Status: "PENDING"}
	}
	state.RouteMode = routeMode
	state.SelectedGates = selected
	state.SkipAuthorizations = routeSkips
	state.CompletedReviewWaves = 0
	state.ExtraReviewWaves = 0
	staleAllDispatches(state)
}

func rebindCurrentSnapshot(state *RunState, snapshot string) {
	previous := state.CurrentSnapshot
	state.CurrentSnapshot = snapshot
	if previous == snapshot {
		return
	}
	staleAllDispatches(state)
	// RQ-012：每个 mode 的执行结果快照各自重绑（合并空 mode 一并处理）。
	for _, mode := range state.qaExecutionModes() {
		result := state.qaExecution(mode)
		if result.Snapshot == previous {
			result.Snapshot = snapshot
			state.setQAExecution(mode, result)
		}
	}
	for id, result := range state.Gates {
		if result.Snapshot == previous {
			result.Snapshot = snapshot
			state.Gates[id] = result
		}
	}
	for id, authorization := range state.SkipAuthorizations {
		if isSealScopedAuthorization(authorization) && authorization.Snapshot == previous {
			authorization.Snapshot = snapshot
			state.SkipAuthorizations[id] = authorization
		}
	}
	for id, result := range state.Carry {
		if result.TargetSnapshot == previous {
			result.TargetSnapshot = snapshot
			state.Carry[id] = result
		}
	}
}

func eligibleCarryGates(state RunState) []string {
	var ids []string
	if state.PreRepairSnapshot == "" {
		return ids
	}
	for id, result := range state.Gates {
		if isSelected(state, id) && result.Status == "PASS" && result.Snapshot == state.PreRepairSnapshot {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// runHasAction reports whether the run's state carries the named action. A run
// seeds its actions from the catalog at start; a run started before an action
// was added to the catalog carries no entry for it.
func runHasAction(state RunState, actionID string) bool {
	_, ok := state.Actions[actionID]
	return ok
}

// actionPassedOrAbsent reports whether the named pre-development action does
// not gate this run: it is absent from the run (predating run), has recorded
// PASS, or the run is a slice instance that inherited the overall-level review.
// Unselected action changes must not block an existing run.
func actionPassedOrAbsent(state RunState, actionID string) bool {
	if !runHasAction(state, actionID) || state.Actions[actionID].Status == "PASS" {
		return true
	}
	return inheritedReview(state, actionID)
}

// inheritedReview reports whether the run's split record names the action as an
// overall-level review inherited by a slice instance (product-review /
// start-readiness from the retained overall run).
func inheritedReview(state RunState, actionID string) bool {
	if state.Slicing == nil {
		return false
	}
	for _, id := range state.Slicing.InheritedReviews {
		if id == actionID {
			return true
		}
	}
	return false
}

func requireTransition(state RunState, operation, target string) error {
	if operation == "requirements-clarification" {
		if state.RequirementConfirmed {
			return fmt.Errorf("the current requirement is already confirmed")
		}
		return nil
	}
	if !state.RequirementConfirmed {
		return fmt.Errorf("the current requirement is not confirmed")
	}
	if operation == "route" {
		if !slicingRecorded(state) {
			return fmt.Errorf("the slicing decision must be recorded before the route")
		}
		if state.RouteMode != "" {
			return fmt.Errorf("the run already has its one route decision")
		}
		return nil
	}
	// 产品审（Part 1）、start-readiness（Part 2）与快速路径的黑盒 QA 设计在拆分决定
	// 与路线确认之前进行，不受路线已确认约束；其余下游操作都需要已确认路线。qa-design
	// 的快速路径分支自行在 case 内约束（见下）。
	if operation != "product-review" && operation != "start-readiness" && operation != "qa-design" && state.RouteMode == "" {
		return fmt.Errorf("the gate route is not confirmed")
	}
	switch operation {
	case "route-add":
		developmentStatus := state.Actions["development-worker"].Status
		if developmentStatus == developmentPrepared || developmentStatus == developmentRepairPrepared {
			return fmt.Errorf("the gate route cannot change while a development worker is prepared")
		}
		if state.PreRepairSnapshot != "" {
			return fmt.Errorf("the gate route cannot change while a repair snapshot requires verification")
		}
		return nil
	case "product-review":
		if state.Actions["product-review"].Status == "PASS" {
			return fmt.Errorf("Product Review already has an authoritative PASS result")
		}
		if developmentStarted(state) {
			return fmt.Errorf("Product Review must be recorded before development")
		}
	case "start-readiness":
		if developmentStarted(state) {
			return fmt.Errorf("Start Readiness must be recorded before development")
		}
		if !actionPassedOrAbsent(state, "product-review") {
			return fmt.Errorf("Product Review must pass before Start Readiness")
		}
	case "qa-design":
		if state.RouteMode == "" {
			// 快速路径：黑盒 QA 设计在拆分决定与路线确认前与 start-readiness 并行开始，
			// 此时路线尚未确认、QA 也未被选中；设计是黑盒且是固有取舍，最终路线不含黑盒
			// QA 时并行设计即废弃。
			if !actionPassedOrAbsent(state, "product-review") {
				return fmt.Errorf("Product Review must pass before QA Design")
			}
			if developmentStarted(state) {
				return fmt.Errorf("QA Design must be recorded before development")
			}
			return nil
		}
		if !isSelectedQA(state) {
			return fmt.Errorf("QA is not selected")
		}
		// 黑盒设计不再被"开发已开始"阻止：黑盒 qa-design/qa-review 在 QA 隔离工作区与
		// 开发并发推进。
		if !actionPassedOrAbsent(state, "product-review") {
			return fmt.Errorf("Product Review must pass before QA Design")
		}
		// RQ-011：设计记录后、该 mode 的 review 派发尚未准备（无 OPEN/CLAIMED qa-review
		// 派发）时，允许再次调用 qa-design 追加/更新用例集（保留既有已批准用例、增量补全）；
		// 只有该 mode 的 review 派发准备后设计才锁定（RQ-012：锁按 mode，黑盒 review 在飞
		// 不锁白盒 qa-design）。qa-review 为 PENDING（无派发）或 PASS/FAIL 后均允许。
		if qaReviewDispatchPrepared(state, target) {
			return fmt.Errorf("the QA case set is locked for an already-prepared QA Review dispatch")
		}
	case "qa-review":
		if !isSelectedQA(state) {
			return fmt.Errorf("QA is not selected")
		}
		// 黑盒 review 不再被"开发已开始"阻止（与开发并发）；零用例可流到 review 的集合
		// 覆盖判定（被选中模式零用例是覆盖缺失，判 P1 阻塞）。RQ-001：判定只受本 mode
		// 的 design/review 结果约束（target=mode），另一 mode 的 review FAIL 不阻止本 mode。
		if state.qaDesign(target).Status != "PASS" {
			return fmt.Errorf("a complete QA case set is required before QA Review")
		}
		if status := state.qaReview(target).Status; status == "PASS" || status == "FAIL" {
			return fmt.Errorf("QA Review already has an authoritative %s result for the current case set", status)
		}
	case "development-worker":
		developmentStatus := state.Actions["development-worker"].Status
		if developmentStatus != developmentPending && developmentStatus != developmentPrepared && developmentStatus != developmentRepairPrepared && developmentStatus != developmentComplete && developmentStatus != developmentVerified {
			return fmt.Errorf("development worker is already prepared")
		}
		if !actionPassedOrAbsent(state, "product-review") {
			return fmt.Errorf("Product Review must pass before development")
		}
		if !actionPassedOrAbsent(state, "start-readiness") {
			return fmt.Errorf("Start Readiness must pass before development")
		}
		if !slicingRecorded(state) {
			return fmt.Errorf("the slicing decision must be recorded before development")
		}
		// 开发开始不再要求黑盒 qa-review PASS：黑盒 QA 设计/review/返修在 QA 隔离工作区
		// 与开发并发推进，快照门（需求 2）在两边都完成时才放行。
		if developmentStatus == developmentPrepared || developmentStatus == developmentRepairPrepared {
			return nil
		}
		if developmentStatus == developmentComplete || developmentStatus == developmentVerified {
			if !reviewWaveRecorded(state) {
				if state.PreRepairSnapshot != "" {
					return fmt.Errorf("the current repair still requires verification")
				}
				return fmt.Errorf("all selected review results must be recorded before repair")
			}
			if !hasRepairableBlocker(state) && !hasSuggestionRecommendation(state) {
				if state.PreRepairSnapshot != "" {
					return fmt.Errorf("the current repair still requires verification")
				}
				return fmt.Errorf("no recorded result requires repair")
			}
			if (developmentStatus != developmentVerified || hasSelectedRuntimeError(state)) && !runtimeErrorsAuthorizedForRepair(state) {
				if state.PreRepairSnapshot != "" {
					return fmt.Errorf("the current repair still requires verification")
				}
				return fmt.Errorf("the current review wave is not complete")
			}
			if state.CompletedReviewWaves >= effectiveReviewWaveLimit(state) {
				return fmt.Errorf("review-wave limit is exhausted; explicit additional repair authorization is required")
			}
		}
	case "snapshot":
		developmentStatus := state.Actions["development-worker"].Status
		adoptingMergedSlices := state.RetainedOverall && developmentStatus == developmentPending
		adoptingSliceRepair := state.RetainedOverall && (developmentStatus == developmentComplete || developmentStatus == developmentVerified)
		if !adoptingMergedSlices && !adoptingSliceRepair && developmentStatus != developmentPrepared && developmentStatus != developmentRepairPrepared {
			return fmt.Errorf("development worker must be prepared before a snapshot")
		}
		if !actionPassedOrAbsent(state, "product-review") {
			return fmt.Errorf("Product Review must pass before a development snapshot")
		}
		if !actionPassedOrAbsent(state, "start-readiness") {
			return fmt.Errorf("Start Readiness must pass before a development snapshot")
		}
		// 快照黑盒门（开发完成 且 黑盒 qa-review PASS）在 AdvanceSnapshot 内强制，
		// 用户显式授权可手动放行；此处不再重复校验。
		if state.PreRepairSnapshot != "" && !runtimeErrorsAuthorizedForRepair(state) {
			return fmt.Errorf("the current repair still requires verification")
		}
		if developmentStatus == developmentRepairPrepared || adoptingSliceRepair {
			if !reviewWaveRecorded(state) {
				return fmt.Errorf("all selected review results must be recorded before repair")
			}
			if !hasRepairableBlocker(state) && !hasSuggestionRecommendation(state) {
				return fmt.Errorf("no recorded result requires repair")
			}
			if adoptingSliceRepair && (developmentStatus != developmentVerified || hasSelectedRuntimeError(state)) && !runtimeErrorsAuthorizedForRepair(state) {
				return fmt.Errorf("the current review wave is not complete")
			}
			if state.CompletedReviewWaves >= effectiveReviewWaveLimit(state) {
				return fmt.Errorf("review-wave limit is exhausted; explicit additional repair authorization is required")
			}
		}
	case "qa-execution":
		if !isSelectedQA(state) {
			return fmt.Errorf("QA is not selected")
		}
		if !hasDevelopmentSnapshot(state) {
			return fmt.Errorf("an immutable development snapshot is required before QA Execution")
		}
		// RQ-001：按目标 mode 读 design/review 结果（target=mode；合并/单派发为 ""）。
		if state.qaDesign(target).Status != "PASS" {
			return fmt.Errorf("QA Design must pass before QA Execution")
		}
		// 黑盒 review 经用户授权放行后，qa-execution 只覆盖已批准用例；未放行时仍要求
		// review PASS。
		if state.qaReview(target).Status != "PASS" && !snapshotBlackboxReleased(state) {
			return fmt.Errorf("QA Review must pass before QA Execution")
		}
	case "gate":
		if !isSelected(state, target) {
			return fmt.Errorf("gate %q is not selected", target)
		}
		if !hasDevelopmentSnapshot(state) {
			return fmt.Errorf("an immutable development snapshot is required before post-development review")
		}
		if !actionPassedOrAbsent(state, "product-review") {
			return fmt.Errorf("Product Review must pass before post-development review")
		}
		if !actionPassedOrAbsent(state, "start-readiness") {
			return fmt.Errorf("Start Readiness must pass before post-development review")
		}
	case "carry":
		if !hasDevelopmentSnapshot(state) || state.PreRepairSnapshot == "" {
			return fmt.Errorf("a repaired immutable snapshot is required before Carry")
		}
	case "seal":
		if !hasDevelopmentSnapshot(state) {
			return fmt.Errorf("an immutable development snapshot is required before Seal")
		}
		if !actionPassedOrAbsent(state, "product-review") {
			return fmt.Errorf("Product Review must pass before Seal")
		}
		if !actionPassedOrAbsent(state, "start-readiness") {
			return fmt.Errorf("Start Readiness must pass before Seal")
		}
		if err := requireSelectedResultsResolved(state); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown workflow transition %q", operation)
	}
	return nil
}

func developmentStarted(state RunState) bool {
	return state.Actions["development-worker"].Status != developmentPending
}

func hasDevelopmentSnapshot(state RunState) bool {
	status := state.Actions["development-worker"].Status
	return status == developmentComplete || status == developmentVerified
}

func semanticResultRecorded(status, snapshot, currentSnapshot string) bool {
	return snapshot == currentSnapshot && (status == "PASS" || status == "FAIL")
}

// comparedRange is the snapshot pair a gate review must report: every gate
// review covers the complete base-to-current delivery, including reruns.
func comparedRange(state RunState) string {
	return state.BaseSnapshot + ".." + state.CurrentSnapshot
}

// gatePromptChanged reports whether a gate's composed prompt (the shared
// reviewer base plus the gate's own content) hash moved since the run started,
// making a recorded authoritative result a candidate for the main agent's
// per-gate judgment rather than a fixed conclusion. A base-only change therefore
// enables per-gate re-dispatch too.
func gatePromptChanged(state RunState, catalog PromptCatalog, gateID string) bool {
	recorded, ok := state.PromptHashes["gate:"+gateID]
	if !ok {
		return false
	}
	gate, ok := catalog.Gate(gateID)
	if !ok {
		return false
	}
	return recorded != composedGatePromptHash(catalog, gate.Content)
}

// reviewerActionIDs are the reviewer actions whose prompt changes with a
// recorded result create an inheritance judgment (RQ-005's "动作提示词" scope,
// mirroring RQ-004's contamination-check coverage). The non-reviewer actions
// (development worker, qa executor) and the carry disposition command are
// excluded: their changes are not zero-context review results that a Carry
// judgment re-decides, and carrying the carry action itself would deadlock (a
// recorded carry cannot be re-judged by carry --main-agent).
var reviewerActionIDs = map[string]bool{
	"requirements-clarification": true,
	"product-review":             true,
	"start-readiness":            true,
	"qa-design":                  true,
	"qa-review":                  true,
}

// actionPromptChanged reports whether an action prompt content hash moved since
// the run recorded it, making a recorded authoritative result a candidate for
// the main agent's judgment. For injected reviewer actions (RQ-003) the composed
// prompt carries the shared reviewer base, so the comparison includes the base
// (a base-only change moves every injected reviewer action's hash and triggers
// the inheritance judgment, symmetric with the gate path); non-reviewer actions
// hash only their own content.
func actionPromptChanged(state RunState, catalog PromptCatalog, actionID string) bool {
	recorded, ok := state.PromptHashes["action:"+actionID]
	if !ok {
		return false
	}
	action, ok := catalog.Action(actionID)
	if !ok {
		return false
	}
	return recorded != composedActionPromptHash(catalog, action)
}

// requireNoPendingInheritance is the hard gate behind RQ-005: every continue /
// rerun entry (prepare / claim / record / snapshot / seal / qa-* /
// authorize-repair) is rejected until every inheritance judgment is handled. A
// judgment is pending when an old-snapshot PASS result awaits a Carry decision,
// or a selected gate / action prompt moved while that target already has a
// recorded PASS/FAIL result and no main-agent Carry judgment was recorded for it
// yet (the mid-flight modification category; it only fires when a recorded
// result exists, so a pre-development run is never blocked by it). The
// disposition commands (carry / requirement / settle-findings) are exempt and
// route around this gate; requirement-artifact reclassification is enforced
// separately by requireCurrentDefinitions, which every entry already calls.
func requireNoPendingInheritance(root string, state RunState, catalog PromptCatalog) error {
	// 首个开发快照前没有继承边界：无开发快照时不存在"旧快照结果待判定"，且处置入口
	// （carry --main-agent 继承判定）依赖开发快照才能记录。首个快照只是记录交付成果、
	// 不是重跑/继续，跳过硬闸；快照之后（开发快照已存在）硬闸恢复，继承判定可正常记录。
	if !hasDevelopmentSnapshot(state) {
		return nil
	}
	if len(eligibleCarryGates(state)) != 0 {
		return fmt.Errorf("prior passing results at the pre-repair snapshot await a Carry decision; dispose them with `workflow carry --main-agent --main-reason '<reason>'` (or an independent carry) before continuing")
	}
	for id := range selectedSet(state) {
		if isQAMode(id) {
			continue
		}
		result := state.Gates[id]
		if !semanticResultRecorded(result.Status, result.Snapshot, state.CurrentSnapshot) {
			continue
		}
		if !gatePromptChanged(state, catalog, id) {
			continue
		}
		// FAIL 结果提示词变化时重派即处置（R 修复清单 P1-1）：已记录 FAIL 的门可直接重派、
		// 重派覆盖旧 FAIL 即完成处置，不进入待决继承，也不需要主代理 Carry 判定；只有已
		// 记录 PASS 的结果才需要判定保留旧结论（INHERIT）还是重跑（RERUN）。
		if result.Status == "FAIL" {
			continue
		}
		// 主代理已对这条提示词变更记录过 Carry 判定（INHERIT / RERUN）时不再待决。
		if state.Carry[id].Decision != "" {
			continue
		}
		return fmt.Errorf("gate %q has a recorded PASS result whose prompt changed; record a main-agent Carry judgment (carry --main-agent) before continuing", id)
	}
	for _, action := range catalog.Actions {
		if !reviewerActionIDs[action.ID] {
			continue
		}
		result := state.Actions[action.ID]
		if result.Status != "PASS" && result.Status != "FAIL" {
			continue
		}
		if !actionPromptChanged(state, catalog, action.ID) {
			continue
		}
		// 与门一致（P1-1）：已记录 FAIL 的动作提示词变化时重派即处置，不进入待决继承。
		if result.Status == "FAIL" {
			continue
		}
		return fmt.Errorf("action %q has a recorded PASS result whose prompt changed; record a main-agent Carry judgment (carry --main-agent) before continuing", action.ID)
	}
	return nil
}

// requireResumeInterrupted enforces the RQ-007 three-branch resume rule for a
// claimed, un-resulted dispatch. When the recorded interruption reason is an
// objective transient API cause and every judgment condition is unchanged, the
// CLI forces a resume: it rejects a fresh prepare of the same target and directs
// the host to restore the original agent. When the conditions are unchanged but
// no objective reason is recorded (including "未知"), the CLI forces the user to
// decide (RQ-008: the main agent has no override power). Any change to snapshot,
// responsibility, task content, requirement, method, or intent — or a recorded
// non-objective interruption reason — lets the guard pass so a new dispatch is
// allowed; an explicit --user-requested authorization is handled by the caller.
func requireResumeInterrupted(root string, state RunState, catalog PromptCatalog, targetKind, target string, mode string) error {
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind != targetKind || dispatch.Target != target || dispatch.Status != "CLAIMED" {
			continue
		}
		// qa-execution 按 mode 分流（R 修复清单 item 3）：不同 mode 的派发互不拦截，
		// 黑盒与白盒各自独立派发、并行执行。
		if target == "qa-execution" && dispatch.Mode != "" && mode != "" && dispatch.Mode != mode {
			continue
		}
		wantedSource := state.CurrentSnapshot
		if isBlackboxQADispatch(dispatch) {
			wantedSource = state.BaseSnapshot
		}
		if dispatch.SourceSnapshot != wantedSource {
			continue
		}
		unchanged, err := dispatchTaskUnchanged(root, state, catalog, dispatch)
		if err != nil {
			// 无法重算任务内容时不强制续用：放行新派发，避免死端。
			continue
		}
		if !unchanged {
			continue
		}
		// RQ-013：读取 CLI 自动记录的中断原因；无 stop 事件或未记录时为空。
		reason, err := workflowLifecycle.InterruptionReason(root, state.RunID, dispatch.ID)
		if err != nil {
			// 无法读取原因时不强制续用：放行新派发，避免死端。
			continue
		}
		// 已记录的非客观原因（如用户主动中断、max_turns 正常结束）按 RQ-008 不受限：
		// 可开新派发。仅"未知"（宿主未提供原因）视同无原因、落入第三分支。
		if reason != "" && reason != "未知" && !isObjectiveInterruptionReason(reason) {
			continue
		}
		if isObjectiveInterruptionReason(reason) {
			// RQ-007 第一分支：客观瞬时原因 + 一切未变 → 必续用。
			return fmt.Errorf("dispatch %q is claimed and interrupted by an objective transient API cause (recorded reason %q); every resume condition is unchanged, so resume the original agent (identity %q) to continue the same dispatch instead of preparing a new one", id, reason, dispatch.ReviewerIdentity)
		}
		// RQ-007 第三分支：一切未变但无客观中断原因（含未记录与"未知"）→ 强制询问用户。
		return fmt.Errorf("dispatch %q is claimed and interrupted with an unchanged task and snapshot but no recorded objective interruption reason; the user must decide: resume the original agent (identity %q) to continue the same dispatch, or authorize a fresh dispatch with --user-requested", id, dispatch.ReviewerIdentity)
	}
	return nil
}

// isObjectiveInterruptionReason reports whether a recorded interruption reason is
// an objective transient API cause (RQ-007/013): HTTP error codes such as
// 429/402/500/502/503/504/529 and transient-overload phrasing. Non-objective
// reasons (user abort, max turns, normal end) are not objective, so RQ-008 lets
// a new dispatch open when the reason is recorded and non-objective.
func isObjectiveInterruptionReason(reason string) bool {
	lower := strings.ToLower(reason)
	for _, code := range []string{"429", "402", "500", "502", "503", "504", "529"} {
		if strings.Contains(lower, code) {
			return true
		}
	}
	for _, phrase := range []string{"rate limit", "rate_limit", "overloaded", "temporarily unavailable", "timeout", "server error", "too many requests"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// dispatchTaskUnchanged reports whether the current catalog and state would
// recompose the dispatch's exact task prompt. The recomposed prompt uses the
// dispatch's own id / attempt / review wave, so the [Dispatch] block is
// identical and only the task content (catalog, requirement, snapshot, detail)
// can move the hash.
func dispatchTaskUnchanged(root string, state RunState, catalog PromptCatalog, dispatch PreparedDispatch) (bool, error) {
	route := routeForState(root, state)
	if isBlackboxQADispatch(dispatch) {
		route.Worktree = absPath(state.QAWorktree)
		route.CurrentSnapshot = state.BaseSnapshot
	}
	route.DispatchID, route.DispatchAttempt, route.ReviewWave = dispatch.ID, dispatch.Attempt, dispatch.ReviewWave
	prompt, err := composeDispatchPrompt(state, catalog, route, dispatch)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:]) == dispatch.PromptHash, nil
}

// composeDispatchPrompt recomposes the exact dispatch prompt the prepare flow
// would produce, mirroring the detail computation of prepareDevelopmentAction
// and the actionPromptDetail injection of PrepareAction without re-running the
// transition guards (they are not part of the task text).
func composeDispatchPrompt(state RunState, catalog PromptCatalog, route PromptRoute, dispatch PreparedDispatch) (string, error) {
	if dispatch.TargetKind == "gate" {
		return ComposeGatePrompt(catalog, dispatch.Target, route)
	}
	if dispatch.Target == "development-worker" {
		detail := ""
		status := state.Actions["development-worker"].Status
		if status == developmentComplete || status == developmentVerified || status == developmentRepairPrepared {
			detail = repairInput(state)
		}
		return ComposeActionPrompt(catalog, "development-worker", route, detail)
	}
	detail, err := actionPromptDetail(state, catalog, dispatch.Target, dispatch.Mode)
	if err != nil {
		return "", err
	}
	return ComposeActionPrompt(catalog, dispatch.Target, route, detail)
}

func normalizeSelected(values, candidates []string) ([]string, error) {
	allowed := map[string]bool{}
	for _, id := range candidates {
		allowed[id] = true
	}
	chosen := map[string]bool{}
	for _, value := range values {
		id := strings.TrimSpace(value)
		if !allowed[id] {
			return nil, fmt.Errorf("gate %q is not in the current route candidates", id)
		}
		if chosen[id] {
			return nil, fmt.Errorf("duplicate selected gate %q", id)
		}
		chosen[id] = true
	}
	return orderedSelection(chosen, candidates), nil
}

func orderedSelection(chosen map[string]bool, candidates []string) []string {
	selected := []string{}
	for _, id := range candidates {
		if chosen[id] {
			selected = append(selected, id)
		}
	}
	return selected
}

func selectedSet(state RunState) map[string]bool {
	selected := map[string]bool{}
	for _, id := range state.SelectedGates {
		selected[id] = true
	}
	return selected
}

func isSelected(state RunState, id string) bool { return selectedSet(state)[id] }

func reviewWaveRecorded(state RunState) bool {
	// R 修复清单 item 8 / RQ-012：波次完成前按选中 mode 校验各 mode 均已记录执行——黑盒/
	// 白盒各自独立派发、各自记录到自己的执行结果；若某选中 mode 从未派发记录，其用例与
	// 发现项被静默跳过，波次不得视为已记录。
	if !selectedQAModesRecorded(state) {
		return false
	}
	for id := range selectedSet(state) {
		if isQAMode(id) {
			continue
		}
		result := state.Gates[id]
		if result.Snapshot != state.CurrentSnapshot || result.Status == "PENDING" || result.Status == "" {
			return false
		}
	}
	return true
}

// selectedQAModesRecorded reports whether every selected QA mode has recorded
// execution at the current snapshot (RQ-012 per-mode storage): blackbox and
// whitebox each record their own result, and a wave may only complete (and Seal)
// once every selected mode has recorded execution, so one mode cannot be silently
// skipped while the other passes. A mode with zero cases has nothing to execute
// (the qa-review set-level decision judged the empty set), so it is trivially
// recorded. Merge QA and the legacy "qa" id use the merged (empty-mode) result.
// A RUNTIME_ERROR result is a recorded outcome and blocks through the existing
// skip-authorization path, so it is not treated as a missing mode here.
func selectedQAModesRecorded(state RunState) bool {
	if !isSelectedQA(state) {
		return true
	}
	for id := range selectedSet(state) {
		if !isQAMode(id) {
			continue
		}
		if !qaModeRecordedAtCurrent(state, qaDispatchMode(id)) {
			return false
		}
	}
	return true
}

// qaModeResultKey returns the storage key that currently holds the effective QA
// execution result for a dispatch mode, together with that result (RQ-012): the
// mode's own per-mode key when it holds an authoritative record at the current
// snapshot, otherwise the merged "" key (the single-dispatch flow that executes
// all modes together, or a legacy merged execution migrated into the "" key).
// Aggregate per-mode checks use this so a merged execution in a per-mode-selected
// run is recognized, without coupling the per-mode results.
func qaModeResultKey(state RunState, mode string) (string, QAExecutionResult) {
	if result := state.qaExecution(mode); result.Snapshot == state.CurrentSnapshot && result.Status != "" && result.Status != "PENDING" {
		return mode, result
	}
	return "", state.qaExecution("")
}

// qaModeResult returns the effective QA execution result for a dispatch mode (see
// qaModeResultKey).
func qaModeResult(state RunState, mode string) QAExecutionResult {
	_, result := qaModeResultKey(state, mode)
	return result
}

// qaModeCarryResult returns the QA execution result a main-agent Carry considers
// for a dispatch mode (RQ-002), at ANY snapshot: the mode's own per-mode key when
// it holds a non-PENDING record, else the mode's preserved prior authoritative
// result (a result reset to PENDING after a re-design or an earlier repair round),
// else the merged "" key (single-dispatch / legacy merged storage). Unlike
// qaModeResultKey it does not require the result to sit at the current snapshot,
// so a pre-repair PASS preserved across a repair snapshot is still visible to
// main-agent carry inheritance.
func qaModeCarryResult(state RunState, mode string) QAExecutionResult {
	_, result := qaModeCarryResultKey(state, mode)
	return result
}

// qaModeCarryResultKey returns the storage key and result a main-agent Carry
// should rebind for a dispatch mode (RQ-002), mirroring qaModeCarryResult's read
// precedence so the carry writes back to the same key it read from.
func qaModeCarryResultKey(state RunState, mode string) (string, QAExecutionResult) {
	if result := state.qaExecution(mode); result.Status != "" && result.Status != "PENDING" {
		return mode, result
	}
	if prior := state.priorQAExecution(mode); prior != nil && (prior.Status == "PASS" || prior.Status == "FAIL") {
		return mode, *prior
	}
	return "", state.qaExecution("")
}

// qaUsesMergedExecution reports whether the run's QA execution is the merged
// single-dispatch flow: the merged "" key holds the authoritative result at the
// current snapshot (a legacy merged execution or the single-dispatch flow), as
// opposed to per-mode dispatch where each concrete mode records its own result.
func qaUsesMergedExecution(state RunState) bool {
	merged := state.qaExecution("")
	return merged.Snapshot == state.CurrentSnapshot && merged.Status != "" && merged.Status != "PENDING"
}

// qaModeRecordedAtCurrent reports whether the given QA dispatch mode has recorded
// execution at the current snapshot (RQ-012 per-mode storage). A mode with no
// cases in the run's QA case set is trivially recorded: there is nothing to
// execute, and the qa-review set-level coverage decision already judged the empty
// set (requirement 4). Otherwise the mode's effective execution result must sit
// at the current snapshot with a non-PENDING status (PASS / FAIL / RUNTIME_ERROR)
// — a merged "" result counts only when it truly carries records for the mode.
func qaModeRecordedAtCurrent(state RunState, mode string) bool {
	if len(state.qaModeCases(mode)) == 0 {
		return true
	}
	key, result := qaModeResultKey(state, mode)
	if result.Snapshot != state.CurrentSnapshot || result.Status == "" || result.Status == "PENDING" {
		return false
	}
	if mode != "" && key == "" {
		return qaResultHasMode(result, mode)
	}
	return true
}

// qaDispatchMode maps a selected QA id to its per-mode dispatch mode name.
// blackbox and whitebox dispatch by their own mode; merge QA and the legacy "qa"
// id dispatch as a single merged (empty-mode) set.
func qaDispatchMode(id string) string {
	switch id {
	case blackboxQAID:
		return "blackbox"
	case whiteboxQAID:
		return "whitebox"
	default:
		return ""
	}
}

// qaModeHasRecorded reports whether the given QA dispatch mode has recorded
// execution at the current snapshot (RQ-012 per-mode storage): a mode with no
// cases is trivially recorded, otherwise the mode's effective execution result
// must sit at the current snapshot with records for the mode (merged "" result
// counts only when it truly carries the mode's records).
func qaModeHasRecorded(state RunState, mode string) bool {
	return qaModeRecordedAtCurrent(state, mode)
}

func hasRepairableBlocker(state RunState) bool {
	// RQ-012：任一选中 QA mode 当前快照有权威 FAIL 结果即阻塞（黑盒/白盒各自独立，合并
	// 单派发结果按 mode 生效）。
	if isSelectedQA(state) {
		for id := range selectedSet(state) {
			if !isQAMode(id) {
				continue
			}
			result := qaModeResult(state, qaDispatchMode(id))
			if result.Status == "FAIL" && result.Snapshot == state.CurrentSnapshot {
				return true
			}
		}
	}
	for id := range selectedSet(state) {
		if isQAMode(id) {
			continue
		}
		if state.Gates[id].Status == "FAIL" && state.Gates[id].Snapshot == state.CurrentSnapshot {
			return true
		}
	}
	return false
}

func hasSuggestionRecommendation(state RunState) bool {
	for id := range selectedSet(state) {
		if isQAMode(id) {
			continue
		}
		result := state.Gates[id]
		if result.Snapshot != state.CurrentSnapshot {
			continue
		}
		for _, finding := range result.Findings {
			if finding.Severity == "P2" || finding.Severity == "P3" {
				return true
			}
		}
	}
	return false
}

func hasSelectedRuntimeError(state RunState) bool {
	for id := range selectedSet(state) {
		if selectedResultStatus(state, id) == "RUNTIME_ERROR" {
			return true
		}
	}
	return false
}

func runtimeErrorsAuthorizedForRepair(state RunState) bool {
	foundRuntime := false
	for id := range selectedSet(state) {
		if selectedResultStatus(state, id) != "RUNTIME_ERROR" {
			continue
		}
		foundRuntime = true
		authorization, ok := state.SkipAuthorizations[id]
		if !ok || authorization.Origin != "SEAL" || authorization.Status != "RUNTIME_ERROR" || authorization.Snapshot != state.CurrentSnapshot {
			return false
		}
	}
	return foundRuntime
}

func repairInput(state RunState) string {
	lines := []string{"Repair the complete recorded wave below. P2/P3 recommendations are included whenever this wave has a blocker or the user explicitly requested their repair."}
	// RQ-012：收集每个 FAIL 的选中 QA mode 的发现项（黑盒/白盒各自独立，合并单派发按
	// mode 生效）。
	if isSelectedQA(state) {
		for id := range selectedSet(state) {
			if !isQAMode(id) {
				continue
			}
			result := qaModeResult(state, qaDispatchMode(id))
			if result.Status != "FAIL" {
				continue
			}
			for _, finding := range result.Findings {
				lines = append(lines, "QA FAIL: "+finding.Message)
			}
		}
	}
	for _, id := range state.SelectedGates {
		if isQAMode(id) {
			continue
		}
		for _, finding := range state.Gates[id].Findings {
			lines = append(lines, fmt.Sprintf("%s %s: %s", id, finding.Severity, finding.Message))
		}
	}
	return strings.Join(lines, "\n")
}

func effectiveReviewWaveLimit(state RunState) int {
	return automaticReviewWaveLimit + state.ExtraReviewWaves
}

func completeReviewWaveIfReady(state *RunState) {
	if len(state.SelectedGates) == 0 || state.Actions["development-worker"].Status != developmentComplete || !reviewWaveRecorded(*state) {
		clearResolvedCarryBoundary(state)
		return
	}
	// RQ-012：任一选中 QA mode 为 RUNTIME_ERROR 即不自动完成波次（走既有 skip 授权路径）。
	if isSelectedQA(*state) {
		for id := range selectedSet(*state) {
			if !isQAMode(id) {
				continue
			}
			if qaModeResult(*state, qaDispatchMode(id)).Status == "RUNTIME_ERROR" {
				return
			}
		}
	}
	for id := range selectedSet(*state) {
		if isQAMode(id) {
			continue
		}
		if state.Gates[id].Status == "RUNTIME_ERROR" {
			return
		}
	}
	if len(eligibleCarryGates(*state)) != 0 || state.Actions["carry"].Status == "RUNTIME_ERROR" {
		return
	}
	state.CompletedReviewWaves++
	state.Actions["development-worker"] = ActionResult{Status: developmentVerified}
	state.PreRepairSnapshot = ""
}

// resolveCarryBoundary closes a carry boundary after a Carry judgment: a repair
// boundary completes the review wave when every selected result is recorded at
// the current snapshot, while a boundary created by an adopted external change
// that needed no real rerun resolves through clearResolvedCarryBoundary without
// counting a new review wave. Real reruns after an adoption are counted by the
// review that records them.
func resolveCarryBoundary(state *RunState) {
	if isAdoptionBoundary(*state) {
		clearResolvedCarryBoundary(state)
		return
	}
	completeReviewWaveIfReady(state)
}

// clearResolvedCarryBoundary drops a carry boundary once every selected result
// is recorded at the current snapshot and no prior passing gate still awaits a
// Carry decision. An adopted external change that needs no real rerun resolves
// this way without counting a new review wave.
func clearResolvedCarryBoundary(state *RunState) {
	if state.PreRepairSnapshot == "" || !reviewWaveRecorded(*state) {
		return
	}
	if len(eligibleCarryGates(*state)) != 0 {
		return
	}
	state.PreRepairSnapshot = ""
}

// authorizeSealSkips records seal-time skip authorizations for the named
// non-passing selected gates. FAIL and RUNTIME_ERROR results may be skipped;
// a FAIL skip is only allowed once the shared review-wave limit is
// exhausted, unless the user explicitly requested the skip (userRequested),
// which records a distinguishable SEAL-USER origin. A RUNTIME_ERROR is
// always manually skippable and keeps the SEAL origin. The authorizations
// are bound to the current snapshot and cleared by the next repair snapshot.
func authorizeSealSkips(state *RunState, skips []string, userRequested bool) error {
	wanted := map[string]bool{}
	for _, raw := range skips {
		id := strings.TrimSpace(raw)
		if wanted[id] {
			return fmt.Errorf("duplicate Seal skip %q", id)
		}
		if !isSelected(*state, id) {
			return fmt.Errorf("Seal skip %q is not a selected gate", id)
		}
		status := selectedResultStatus(*state, id)
		if status == "PENDING" || status == "" {
			return fmt.Errorf("selected gate %q is PENDING and cannot be skipped", id)
		}
		if status == "PASS" {
			return fmt.Errorf("selected gate %q already passed", id)
		}
		if status == "FAIL" && !userRequested && state.CompletedReviewWaves < effectiveReviewWaveLimit(*state) {
			return fmt.Errorf("selected gate %q cannot be skipped before the review-wave limit is exhausted", id)
		}
		wanted[id] = true
	}
	for id := range wanted {
		origin := "SEAL"
		if userRequested && selectedResultStatus(*state, id) == "FAIL" {
			origin = "SEAL-USER"
		}
		state.SkipAuthorizations[id] = SkipAuthorization{Origin: origin, Status: selectedResultStatus(*state, id), Snapshot: state.CurrentSnapshot}
	}
	return nil
}

// isSealScopedAuthorization reports whether the authorization is a seal-time
// skip authorization bound to the current snapshot: SEAL for limit-exhausted
// and RUNTIME_ERROR skips, SEAL-USER for FAIL skips the user explicitly
// requested before the limit is exhausted.
func isSealScopedAuthorization(authorization SkipAuthorization) bool {
	return authorization.Origin == "SEAL" || authorization.Origin == "SEAL-USER"
}

func requireSelectedResultsResolved(state RunState) error {
	// R 修复清单 item 8：seal 前按选中 mode 校验各 mode 均已记录执行，避免单个 mode
	// 记录 PASS 而另一 mode 被静默跳过。
	if !selectedQAModesRecorded(state) {
		return fmt.Errorf("QA Execution has not recorded every selected QA mode at the current snapshot")
	}
	for id := range selectedSet(state) {
		status := selectedResultStatus(state, id)
		if status == "PENDING" || status == "" {
			return fmt.Errorf("selected gate %q is PENDING", id)
		}
		snapshot := state.Gates[id].Snapshot
		if isQAMode(id) {
			// RQ-012：QA 结果的快照按该 mode 的有效执行结果读取（per-mode 或合并单派发）。
			snapshot = qaModeResult(state, qaDispatchMode(id)).Snapshot
		}
		if snapshot != state.CurrentSnapshot {
			return fmt.Errorf("selected gate %q has not been verified at the current snapshot", id)
		}
		if status == "PASS" {
			continue
		}
		authorization, ok := state.SkipAuthorizations[id]
		if !ok || !isSealScopedAuthorization(authorization) || authorization.Status != status || authorization.Snapshot != state.CurrentSnapshot {
			return fmt.Errorf("selected gate %q with status %s requires explicit Seal skip authorization", id, status)
		}
	}
	return nil
}

func selectedResultStatus(state RunState, id string) string {
	if isQAMode(id) {
		// RQ-012：QA 模式的状态按该 mode 的有效执行结果读取（per-mode 或合并单派发）。
		return qaModeResult(state, qaDispatchMode(id)).Status
	}
	return state.Gates[id].Status
}

func sortedGateIDs(gates map[string]GateResult) []string {
	ids := make([]string, 0, len(gates))
	for id := range gates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func resolveFromRoot(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.FromSlash(path))
}

func absPath(path string) string {
	full, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(full)
}

func newRunID() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return strings.ToLower(time.Now().UTC().Format("20060102t150405000z")) + "-" + hex.EncodeToString(suffix[:]), nil
}

func requirementArtifactSet(root, primary string, additional []string) ([]RequirementArtifact, error) {
	root = cleanWorktree(root)
	paths := append([]string{primary}, additional...)
	seen := map[string]bool{}
	artifacts := make([]RequirementArtifact, 0, len(paths))
	for _, raw := range paths {
		path, err := validatedArtifactPath(root, raw)
		if err != nil {
			return nil, err
		}
		if seen[path] {
			return nil, fmt.Errorf("duplicate requirement artifact %q", path)
		}
		seen[path] = true
		revision, err := RequirementRevision(resolveFromRoot(root, path))
		if err != nil {
			return nil, fmt.Errorf("requirement artifact %s: %w", path, err)
		}
		artifacts = append(artifacts, RequirementArtifact{Path: path, Revision: revision})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func validatedArtifactPath(root, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("requirement artifact path is required")
	}
	full := resolveFromRoot(root, strings.TrimSpace(raw))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("requirement artifact must be a file under the repository root: %s", raw)
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("requirement artifact %s: %w", filepath.ToSlash(rel), err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("requirement artifact %s is not a regular file", filepath.ToSlash(rel))
	}
	return filepath.ToSlash(filepath.Clean(rel)), nil
}

func normalizeArtifactPath(root, raw string) string {
	full := resolveFromRoot(cleanWorktree(root), strings.TrimSpace(raw))
	rel, err := filepath.Rel(cleanWorktree(root), full)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(raw))
	}
	return filepath.ToSlash(filepath.Clean(rel))
}

func artifactRevision(artifacts []RequirementArtifact, path string) string {
	for _, artifact := range artifacts {
		if artifact.Path == path {
			return artifact.Revision
		}
	}
	return ""
}

func requirementArtifactsChanged(root string, artifacts []RequirementArtifact) (bool, error) {
	if len(artifacts) == 0 {
		return false, fmt.Errorf("requirement artifact set is empty")
	}
	for _, artifact := range artifacts {
		revision, err := RequirementRevision(resolveFromRoot(cleanWorktree(root), artifact.Path))
		if err != nil {
			return false, fmt.Errorf("requirement artifact %s: %w", artifact.Path, err)
		}
		if revision != artifact.Revision {
			return true, nil
		}
	}
	return false, nil
}

func sameArtifactSet(left, right []RequirementArtifact) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func qaCaseSemanticKey(kind, description, procedure, oracle string) string {
	return strings.Join([]string{strings.ToUpper(strings.TrimSpace(kind)), strings.TrimSpace(description), strings.TrimSpace(procedure), strings.TrimSpace(oracle)}, "\x00")
}

// qaCaseSemanticKeyWithTest is the incremental-design identity of a whitebox case:
// it includes the Test binding (RQ-013, the "<file>::<function>" test reference) so
// that changing a case's test reference produces a new case identity (re-review)
// instead of silently retaining the prior case's PASS for a case whose executed test
// changed. Blackbox cases carry an empty test, so the key reduces to the four-field
// identity.
func qaCaseSemanticKeyWithTest(kind, description, procedure, oracle, test string) string {
	return qaCaseSemanticKey(kind, description, procedure, oracle) + "\x00" + strings.TrimSpace(test)
}
