// slice.go 是拆分 run 身份层在提交协议层的归属点：split 拓扑的验收条件
// 责任映射校验与合并路线词汇、master 账上的分片认领（sliceID → childRunID
// 的唯一事实源）、child run 启动携带的继承事实，以及整体审查结论经继承
// 等价满足的判据。相位机与账本校验（review.go）直接引用本文件的判据，
// 不另建第二套判定路径。
package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// 拆分 run 的持久词汇。命名权威：merge/merge-gate/merge-qa 与
// reservedRouteGates（review.go）及 internal/validate/catalog.go:28-31 同值；
// master-merge 与 legacy AC 责任校验（internal/validate/
// requirement_guarantee.go）同值。
const (
	// routeModeMerge 是 split 拓扑绑定同事务锁定的合并路线：保留总任务实例
	// 只跑强制的合并门＋合并 QA，不涉常规路线选择。
	routeModeMerge = "merge"
	mergeQAGateID  = "merge-qa"
	mergeGateID    = "merge-gate"
	// masterMergeOwner 是 AC 责任映射中保留总任务实例（合并侧）的 owner 值。
	masterMergeOwner = "master-merge"
)

// SliceChildBinding 是 child run 状态上的继承事实：分片身份（master run 与
// 分片名）加上继承结论绑定的需求 revision。产品审/技术审的整体结论来源就
// 是 MasterRunID——child 不自行增加审查轮次（incremental-seal-plan：split
// child 继承 master 的整体产品/技术审，不自行增加 series）。
type SliceChildBinding struct {
	MasterRunID         string `json:"masterRunId"`
	SliceID             string `json:"sliceId"`
	RequirementRevision string `json:"requirementRevision"`
}

// SliceRunStart 是 child run 启动（Init）一次落账的完整继承事实：身份绑定
// 加上从 master 继承的拓扑确认（不可重切）。继承拓扑落账为 child 自身的
// State.Topology——child 状态自含，引擎不跨 run 读账。
type SliceRunStart struct {
	Binding  SliceChildBinding
	Topology TopologyBinding
}

// SliceReceipt binds the frozen checkpoint to its canonical content digest.
type SliceReceipt struct {
	Receipt SliceTerminalReceipt `json:"receipt"`
	Digest  string               `json:"digest"`
}

// SliceTerminalReceipt carries the successful child evidence needed by its
// master. It contains final results rather than a second copy of run state.
// Empty SelectedQA and ApprovedCases explicitly mean ordinary QA was not selected.
type SliceTerminalReceipt struct {
	Status              runtime.RunPhase                                    `json:"status"`
	ChildRunID          string                                              `json:"childRunId"`
	SliceID             string                                              `json:"sliceId"`
	MasterRunID         string                                              `json:"masterRunId"`
	Envelope            persistence.Envelope                                `json:"envelope"`
	Snapshot            string                                              `json:"snapshot"`
	Tree                string                                              `json:"tree"`
	Digest              string                                              `json:"digest"`
	RequirementRevision string                                              `json:"requirementRevision"`
	Topology            TopologyBinding                                     `json:"topology"`
	RouteMode           string                                              `json:"routeMode"`
	SelectedGates       []string                                            `json:"selectedGates"`
	Validation          SliceValidationReceipt                              `json:"validation"`
	SelectedQA          []coverage.ReviewKind                               `json:"selectedQA"`
	ApprovedCases       map[coverage.ReviewKind]coverage.AcceptanceManifest `json:"approvedCases"`
	Cost                *cost.RunCost                                       `json:"cost"`
	OwnerTranscript     string                                              `json:"ownerTranscript,omitempty"`
	OwnerStartedAt      string                                              `json:"ownerStartedAt,omitempty"`
	OwnerEndedAt        string                                              `json:"ownerEndedAt,omitempty"`
	LastEvent           *SliceEventIdentity                                 `json:"lastEvent,omitempty"`
	LastRequest         *SliceRequestIdentity                               `json:"lastRequest,omitempty"`
}

type SliceValidationReceipt struct {
	WaveID              string                       `json:"waveId"`
	Status              string                       `json:"status"`
	RequirementRevision string                       `json:"requirementRevision"`
	Generation          uint64                       `json:"generation"`
	RouteDigest         string                       `json:"routeDigest"`
	ExpectedDigest      string                       `json:"expectedDigest"`
	Candidate           coverage.ValidationCandidate `json:"candidate"`
	Scope               coverage.ScopeMode           `json:"scope"`
	Results             []SliceValidationResult      `json:"results"`
}

type SliceValidationResult struct {
	Target ValidationTarget `json:"target"`
	Result WorkerResult     `json:"result"`
}

type SliceEventIdentity struct {
	EventID   string `json:"eventId"`
	RequestID string `json:"requestId,omitempty"`
	Digest    string `json:"digest"`
	Revision  uint64 `json:"revision"`
}

type SliceRequestIdentity struct {
	RequestID      string `json:"requestId"`
	EventID        string `json:"eventId"`
	RequestDigest  string `json:"requestDigest"`
	DecisionDigest string `json:"decisionDigest"`
	Revision       uint64 `json:"revision"`
}

// CanonicalDigest uses the protocol's canonical wire digest, independently of
// sidecar whitespace. The Git tree identity remains the native content digest.
func (r SliceTerminalReceipt) CanonicalDigest() string { return digestOfCanonical(r) }

// requestDigest preserves the canonical event identity of external requests.
// Engine-generated requests are owned by PendingAsks; their offer excludes the
// settlement flag, so deciding or retiring an Ask does not change its evidence.
func (s *State) requestDigest(requestID string) string {
	if event, external := s.Events[requestID]; external {
		return event.Digest
	}
	ask := s.PendingAsks[requestID]
	ask.Resolved = false
	return digestOfCanonical(ask)
}

// CheckpointSlice commits successful child evidence, phase, owner cost and its
// closed interval together. Native observation and measurement belong to facade;
// this transaction rechecks the existing validation/promotion admission owner.
func (e *Engine) CheckpointSlice(snapshot, tree string, owner *cost.OwnerCost, endedAt, expectedFingerprint string) error {
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if state.SliceChild == nil || state.Route != "regular" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "slice checkpoint requires a regular child run"}
	}
	if state.SliceReceipt != nil {
		if state.SliceReceipt.Receipt.Snapshot == snapshot && state.SliceReceipt.Receipt.Tree == tree {
			return nil
		}
		return &RejectedError{Code: CodeReceiptConflict, Detail: "slice checkpoint is already frozen at a different native identity"}
	}
	if err := state.errWriteBarrier("slice checkpoint"); err != nil {
		return err
	}
	if state.Phase == runtime.PhaseTerminal || state.abortRequested() || !state.validationConcluded(e.cfg.Definition) {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "slice checkpoint requires the concluded validation pipeline without an abort decision"}
	}
	if snapshot == "" || snapshot != state.PromotedSnapshot || tree == "" || tree != state.PromotedDigest {
		return &RejectedError{Code: CodeReceiptConflict, Detail: "slice checkpoint native snapshot/tree differs from the verified promotion"}
	}
	if state.OwnerTranscript != "" {
		if _, err := time.Parse(time.RFC3339Nano, endedAt); err != nil || owner == nil {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "slice checkpoint requires its measured owner cost and closed interval"}
		}
		cost.RecordOwner(state.costLedger(), *owner)
	} else if owner != nil || endedAt != "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "slice checkpoint has no owner binding to measure"}
	}
	wave := state.currentValidationWave()
	r := SliceTerminalReceipt{
		Status:              runtime.PhaseSliceReady,
		ChildRunID:          state.RunID,
		SliceID:             state.SliceChild.SliceID,
		MasterRunID:         state.SliceChild.MasterRunID,
		Envelope:            e.store.Envelope(),
		Snapshot:            snapshot,
		Tree:                tree,
		Digest:              tree,
		RequirementRevision: state.RequirementRevision(),
		Topology:            copyTopology(*state.Topology),
		RouteMode:           state.RouteMode,
		SelectedGates:       slices.Clone(state.SelectedGates),
		Validation: SliceValidationReceipt{
			WaveID:              wave.ID,
			Status:              wave.Status,
			RequirementRevision: wave.RequirementRevision,
			Generation:          wave.Generation,
			RouteDigest:         wave.RouteDigest,
			ExpectedDigest:      wave.ExpectedDigest,
			Candidate:           wave.Candidate,
			Scope:               wave.Scope,
			Results:             []SliceValidationResult{},
		},
		SelectedQA:      []coverage.ReviewKind{},
		ApprovedCases:   map[coverage.ReviewKind]coverage.AcceptanceManifest{},
		Cost:            state.Cost,
		OwnerTranscript: state.OwnerTranscript,
		OwnerStartedAt:  state.OwnerStartedAt,
		OwnerEndedAt:    endedAt,
	}
	keys := make([]string, 0, len(wave.Targets))
	for key := range wave.Targets {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		target := wave.Targets[key]
		r.Validation.Results = append(r.Validation.Results, SliceValidationResult{Target: *target, Result: state.Results[target.ResultAction]})
	}
	for id, event := range state.Events {
		if r.LastEvent == nil || event.Acceptance.Revision > r.LastEvent.Revision {
			r.LastEvent = &SliceEventIdentity{EventID: id, RequestID: event.Acceptance.RequestID, Digest: event.Digest, Revision: event.Acceptance.Revision}
		}
	}
	for _, decision := range state.Decisions {
		if r.LastRequest == nil || decision.Revision > r.LastRequest.Revision {
			r.LastRequest = &SliceRequestIdentity{RequestID: decision.RequestID, EventID: decision.EventID, RequestDigest: state.requestDigest(decision.RequestID), DecisionDigest: state.Events[decision.EventID].Digest, Revision: decision.Revision}
		}
	}
	// Detach the frozen evidence from the run's mutable maps and cost projection.
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	var frozen SliceTerminalReceipt
	if err := json.Unmarshal(data, &frozen); err != nil {
		return err
	}
	state.SliceReceipt = &SliceReceipt{Receipt: frozen, Digest: frozen.CanonicalDigest()}
	if state.Phase == runtime.PhaseDevelopmentParallel {
		if err := state.TransitionPhase(runtime.PhaseSnapshotReady); err != nil {
			return err
		}
	}
	if state.Phase == runtime.PhaseSnapshotReady {
		if err := state.TransitionPhase(runtime.PhasePostReview); err != nil {
			return err
		}
	}
	if err := state.TransitionPhase(runtime.PhaseSliceReady); err != nil {
		return err
	}
	_, err = e.commit(state, revision, expectedFingerprint)
	return err
}

func (s *State) verifySliceReceipt() error {
	if s.SliceReceipt == nil {
		if s.Phase == runtime.PhaseSliceReady {
			return &RejectedError{Code: CodeStateIntegrityInvalid, Detail: "SLICE_READY requires its frozen receipt"}
		}
		return nil
	}
	r := s.SliceReceipt.Receipt
	if s.SliceChild == nil || r.Status != runtime.PhaseSliceReady ||
		r.ChildRunID != s.RunID || r.MasterRunID != s.SliceChild.MasterRunID || r.SliceID != s.SliceChild.SliceID ||
		r.Snapshot != s.PromotedSnapshot || r.Tree != s.PromotedDigest || r.Digest != r.Tree ||
		s.SliceReceipt.Digest != r.CanonicalDigest() {
		return &RejectedError{Code: CodeStateIntegrityInvalid, Detail: "slice receipt does not match its frozen child identity and digest"}
	}
	return nil
}

// validSliceID 报告分片名是否为可作 AC 责任 owner 引用与 child 认领键的
// 标识形状：小写字母数字连字符，同 run id 规则（facade validRunID）。
func validSliceID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !(r == '-' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// splitACOwners 校验 split 拓扑决定的验收条件责任映射并归一为
// map[AC-ID]owner：AC 宇宙＝当前确认需求冻结投影的验收条件（引擎内 REQ/AC
// 事实的唯一权威是 REQUIREMENT 观察冻结的需求文档）；每条 AC 恰一个主责
// owner；owner ∈ Slices ∪ {master-merge}。Slices 逐项唯一（认领键）与
// ACOwners 重复声明都在此可区分拒绝。
func (s *State) splitACOwners(decision *TopologyDecision) (map[string]string, error) {
	if s.Validation == nil || s.Validation.RequirementRevision != s.RequirementRevision() {
		return nil, fmt.Errorf("split binding requires the acceptance-condition universe of the confirmed requirement; submit the REQUIREMENT operator observation at revision %q first", s.RequirementRevision())
	}
	conditions := map[string]bool{}
	for _, requirement := range s.Validation.Document.Requirements {
		for _, condition := range requirement.AcceptanceConditions {
			conditions[condition.ID] = true
		}
	}
	allowedOwners := map[string]bool{masterMergeOwner: true}
	seenSlices := make(map[string]bool, len(decision.Slices))
	for _, slice := range decision.Slices {
		// 分片名是 child 认领键（SliceChildren 以它为唯一键）：逐项唯一，
		// 重复声明不可代表拆分数量。
		if seenSlices[slice] {
			return nil, fmt.Errorf("split slice name %q listed twice; each slice is a distinct claim identity", slice)
		}
		seenSlices[slice] = true
		allowedOwners[slice] = true
	}
	owners := make(map[string]string, len(decision.ACOwners))
	for _, entry := range decision.ACOwners {
		if !conditions[entry.AC] {
			return nil, fmt.Errorf("AC responsibility references unknown acceptance condition %s", entry.AC)
		}
		if !allowedOwners[entry.Owner] {
			return nil, fmt.Errorf("AC responsibility for %s references unknown owner %s", entry.AC, entry.Owner)
		}
		if _, duplicate := owners[entry.AC]; duplicate {
			return nil, fmt.Errorf("acceptance condition %s has duplicate primary responsibility", entry.AC)
		}
		owners[entry.AC] = entry.Owner
	}
	for condition := range conditions {
		if owners[condition] == "" {
			return nil, fmt.Errorf("%s is missing its one primary responsibility scope", condition)
		}
	}
	return owners, nil
}

// mergeRouteGates 是 split 绑定同事务锁定的合并验证顺序（沿用 legacy：
// merge-qa 在前、merge-gate 在后）。
func mergeRouteGates() []string {
	return []string{mergeQAGateID, mergeGateID}
}

// ClaimSlice 由 child run 的启动路径在 master 账上认领一个分片，并返回
// child 启动所需的继承事实。唯一性与身份匹配都在认领事务内判定：master
// 须 ACTIVE 且已绑定 split 拓扑、分片在拓扑内、child 受理需求 revision 等
// 于 master 当前确认 revision、双方 BaseSnapshot 相同；同一分片只能被一个
// childRunID 认领（认领即事实——异 id 重入拒绝，同 id 重入幂等补齐，两写窗
// 中断后经同一创建路径恢复）。
func (e *Engine) ClaimSlice(sliceID, childRunID, childRevision, childBaseSnapshot, expectedFingerprint string) (*SliceRunStart, error) {
	claimRejected := func(format string, args ...any) error {
		return &RejectedError{Code: CodeSliceClaimRejected, Detail: fmt.Sprintf(format, args...)}
	}
	sliceID, childRunID = strings.TrimSpace(sliceID), strings.TrimSpace(childRunID)
	if sliceID == "" || childRunID == "" {
		return nil, claimRejected("a slice claim needs both the slice id and the child run id")
	}
	revision, state, err := e.load()
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	// 认领是 master 账上的权威写入：需求变化写屏障（Phase A）在位时可区
	// 分拒绝、零写（复用 FA 单一屏障闸判定，不另建第二套屏障逻辑）。
	if err := state.errWriteBarrier("slice claiming"); err != nil {
		return nil, err
	}
	if state.Phase == runtime.PhaseTerminal || state.abortRequested() {
		return nil, claimRejected("master run %q is not ACTIVE", state.RunID)
	}
	if state.Topology == nil || state.Topology.Decision != topologySplit {
		return nil, claimRejected("master run %q has no confirmed split topology", state.RunID)
	}
	if !slices.Contains(state.Topology.Slices, sliceID) {
		return nil, claimRejected("slice %q is not part of the split topology of master run %q", sliceID, state.RunID)
	}
	if current := state.RequirementRevision(); childRevision != current {
		return nil, claimRejected("child intake revision %q does not match the master's current confirmed revision %q", childRevision, current)
	}
	if childBaseSnapshot != state.BaseSnapshot {
		return nil, claimRejected("child base snapshot %q does not match the master base snapshot %q", childBaseSnapshot, state.BaseSnapshot)
	}
	if claimed := state.SliceChildren[sliceID]; claimed != "" {
		if claimed != childRunID {
			return nil, claimRejected("slice %q is already claimed by child run %q", sliceID, claimed)
		}
		// 同 id 重入：认领事实已在账，直接返回继承事实补齐缺口侧。
		return sliceRunStartOf(state, sliceID), nil
	}
	if state.SliceChildren == nil {
		state.SliceChildren = map[string]string{}
	}
	state.SliceChildren[sliceID] = childRunID
	if _, err := e.commit(state, revision, expectedFingerprint); err != nil {
		return nil, err
	}
	return sliceRunStartOf(state, sliceID), nil
}

// sliceRunStartOf 从 master 状态派生 child 启动继承事实（深拷贝拓扑，避免
// 与 master 状态共享可变容器）。
func sliceRunStartOf(state *State, sliceID string) *SliceRunStart {
	return &SliceRunStart{
		Binding: SliceChildBinding{
			MasterRunID:         state.RunID,
			SliceID:             sliceID,
			RequirementRevision: state.RequirementRevision(),
		},
		Topology: copyTopology(*state.Topology),
	}
}

// copyTopology 深拷贝拓扑绑定（继承落账用）。
func copyTopology(binding TopologyBinding) TopologyBinding {
	copied := binding
	copied.Slices = slices.Clone(binding.Slices)
	if binding.ACOwners != nil {
		copied.ACOwners = make(map[string]string, len(binding.ACOwners))
		for ac, owner := range binding.ACOwners {
			copied.ACOwners[ac] = owner
		}
	}
	return copied
}

// inheritedReviewPass 报告 child 的整体审查结论是否经继承事实等价满足：
// 继承结论绑定的需求 revision 仍在确认账本内（含义不变的再确认按 carry
// 语义续用），且该系列无重走义务。与 master 侧 effectivePass 的判据同构
// （改动接纳判据本身，不建第二套判定路径）。
func (s *State) inheritedReviewPass(series review.Series) bool {
	return !series.FreshOwed && s.confirmedRequirementRevision(s.SliceChild.RequirementRevision)
}
