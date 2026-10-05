// slice.go 是拆分 run 身份层在提交协议层的归属点：split 拓扑的验收条件
// 责任映射校验与合并路线词汇、master 的已解锁成员（sliceID → childRunID
// 的唯一事实源）、child run 启动携带的继承事实，以及整体审查结论经继承
// 等价满足的判据。相位机与账本校验（review.go）直接引用本文件的判据，
// 不另建第二套判定路径。
package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
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
	MasterRunID         string                      `json:"masterRunId"`
	SliceID             string                      `json:"sliceId"`
	RequirementRevision string                      `json:"requirementRevision"`
	Coverage            *CoverageInventory          `json:"coverage,omitempty"`
	ParentChange        *ParentRequirementAuthority `json:"parentChange,omitempty"`
	ParentReset         *ResetAuthority             `json:"parentReset,omitempty"`
	ParentReviewPending bool                        `json:"parentReviewPending,omitempty"`
	ParentAbort         *AbortAuthority             `json:"parentAbort,omitempty"`
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
	OriginRequirementRevision string                                              `json:"originRequirementRevision,omitempty"`
	ParentRequirement         *ParentRequirementAuthority                         `json:"parentRequirement,omitempty"`
	ParentReset               *ResetAuthority                                     `json:"parentReset,omitempty"`
	Status                    runtime.RunPhase                                    `json:"status"`
	ChildRunID                string                                              `json:"childRunId"`
	SliceID                   string                                              `json:"sliceId"`
	MasterRunID               string                                              `json:"masterRunId"`
	Envelope                  persistence.Envelope                                `json:"envelope"`
	Snapshot                  string                                              `json:"snapshot"`
	Tree                      string                                              `json:"tree"`
	Digest                    string                                              `json:"digest"`
	RequirementRevision       string                                              `json:"requirementRevision"`
	Topology                  TopologyBinding                                     `json:"topology"`
	RouteMode                 string                                              `json:"routeMode"`
	SelectedGates             []string                                            `json:"selectedGates"`
	Validation                SliceValidationReceipt                              `json:"validation"`
	SelectedQA                []coverage.ReviewKind                               `json:"selectedQA"`
	ApprovedCases             map[coverage.ReviewKind]coverage.AcceptanceManifest `json:"approvedCases"`
	Cost                      *cost.RunCost                                       `json:"cost"`
	OwnerTranscript           string                                              `json:"ownerTranscript,omitempty"`
	OwnerStartedAt            string                                              `json:"ownerStartedAt,omitempty"`
	OwnerEndedAt              string                                              `json:"ownerEndedAt,omitempty"`
	Abort                     *AbortAuthority                                     `json:"abort,omitempty"`
	ResetRequestID            string                                              `json:"resetRequestId,omitempty"`
	LastEvent                 *SliceEventIdentity                                 `json:"lastEvent,omitempty"`
	LastRequest               *SliceRequestIdentity                               `json:"lastRequest,omitempty"`
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
	if state.Phase == runtime.PhaseTerminal || state.abortRequested() || state.pendingControl() || state.nativeDebt() || !state.validationConcluded(e.cfg.Definition) {
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
		OriginRequirementRevision: state.SliceChild.RequirementRevision,
		ParentRequirement:         state.SliceChild.ParentChange, ParentReset: state.SliceChild.ParentReset,
		Status:              runtime.PhaseSliceReady,
		ChildRunID:          state.RunID,
		SliceID:             state.SliceChild.SliceID,
		MasterRunID:         state.SliceChild.MasterRunID,
		Envelope:            e.store.Envelope(),
		Snapshot:            snapshot,
		Tree:                tree,
		Digest:              tree,
		RequirementRevision: state.RequirementRevision(),
		ResetRequestID:      state.resetRequestID(),
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
		if _, local := state.Events[decision.EventID]; !local {
			continue
		}
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
	for childID, receipt := range s.SliceReceipts {
		if receipt.Receipt.ChildRunID != childID || receipt.Receipt.MasterRunID != s.RunID || receipt.verify() != nil {
			return &RejectedError{Code: CodeStateIntegrityInvalid, Detail: "accepted slice receipt differs from its frozen identity or digest"}
		}
	}
	if s.SliceReceipt == nil {
		if s.Phase == runtime.PhaseSliceReady {
			return &RejectedError{Code: CodeStateIntegrityInvalid, Detail: "SLICE_READY requires its frozen receipt"}
		}
		return nil
	}
	r := s.SliceReceipt.Receipt
	if s.SliceChild == nil || (r.Status != runtime.PhaseSliceReady && r.Status != "ABORTED") ||
		r.ChildRunID != s.RunID || r.MasterRunID != s.SliceChild.MasterRunID || r.SliceID != s.SliceChild.SliceID ||
		r.Status == runtime.PhaseSliceReady && (r.Snapshot != s.PromotedSnapshot || r.Tree != s.PromotedDigest || r.Digest != r.Tree) ||
		s.SliceReceipt.Digest != r.CanonicalDigest() {
		return &RejectedError{Code: CodeStateIntegrityInvalid, Detail: "slice receipt does not match its frozen child identity and digest"}
	}
	return nil
}

// verify checks the frozen receipt's own bindings. Current master eligibility
// is separate: a legal requirement or topology decision may supersede evidence.
// Gate results retain their original identities, including repair/carry evidence;
// the child loader and checkpoint own whether the final obligations concluded.
func (r SliceReceipt) verify() error {
	v := r.Receipt
	route := State{RouteMode: v.RouteMode, SelectedGates: v.SelectedGates, Topology: &v.Topology}
	if r.Digest != v.CanonicalDigest() || (v.Status != runtime.PhaseSliceReady && v.Status != "ABORTED") || v.ChildRunID == "" || v.MasterRunID == "" || v.SliceID == "" ||
		v.Status == runtime.PhaseSliceReady && (v.Snapshot == "" || v.Tree == "" || v.Digest != v.Tree || v.Validation.Status != "COMPLETE" || v.Validation.Candidate.Digest != v.Tree) ||
		v.RequirementRevision == "" || v.Status == runtime.PhaseSliceReady && (v.Validation.RequirementRevision != v.RequirementRevision || v.Validation.RouteDigest != route.validationRouteDigest()) || v.Status == "ABORTED" && (v.Abort == nil || v.Abort.Event.Digest == "" || v.Abort.Decision.RequestID == "") {
		return &RejectedError{Code: CodeReceiptConflict, Detail: "slice receipt has inconsistent frozen bindings"}
	}
	return nil
}

// AcceptSliceReceipt is the sole master acceptance transaction. Its receipt map
// owns deduplication for cases and both cost contributions; no caller supplies
// increments. Discovery supplies the frozen evidence of a loader-validated child.
func (e *Engine) AcceptSliceReceipt(receipt SliceReceipt, expectedFingerprint string) error {
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if err := receipt.verify(); err != nil {
		return err
	}
	r := receipt.Receipt
	accepted, exists := state.SliceReceipts[r.ChildRunID]
	if exists && accepted.Digest == receipt.Digest {
		return nil
	}
	if exists && accepted.Receipt.Status == "ABORTED" && r.Status == runtime.PhaseSliceReady {
		return nil
	}
	if err := state.errWriteBarrier("slice receipt acceptance"); err != nil && r.Status != "ABORTED" {
		return err
	}
	if (!state.SliceReceiptCurrent(receipt) && !state.abortedSliceMember(receipt)) || r.Envelope != e.store.Envelope() {
		return &RejectedError{Code: CodeReceiptConflict, Detail: "slice receipt does not match the active master's exact claim, requirement, topology or version"}
	}
	if state.SliceReceipts == nil {
		state.SliceReceipts = map[string]SliceReceipt{}
	}
	if exists {
		state.SliceReceiptHistory = append(state.SliceReceiptHistory, accepted)
	}
	state.SliceReceipts[r.ChildRunID] = receipt
	ledger := state.costLedger()
	cost.Merge(ledger, r.ChildRunID, r.Cost)
	if exists && accepted.Receipt.Cost != nil && accepted.Receipt.Cost.Owner != nil {
		o := accepted.Receipt.Cost.Owner
		ledger.TotalInputTokens -= o.TotalInputTokens
		ledger.InputCacheHitTokens -= o.InputCacheHitTokens
		ledger.InputCacheMissTokens -= o.InputCacheMissTokens
		ledger.OutputTokens -= o.OutputTokens
	}
	if r.Cost != nil {
		cost.AddChildOwner(ledger, r.Cost.Owner)
	}
	if r.Status == "ABORTED" && !state.abortRequested() && r.Abort.Requirement == nil {
		id := "slice-aborted:" + r.ChildRunID + ":" + receipt.Digest
		state.PendingAsks[id] = PendingAsk{RequestID: id, Control: ControlAbort, Options: []AskOption{{ID: "abort", Label: "Abort master because slice " + r.SliceID + " (" + r.ChildRunID + ") aborted by " + r.Abort.Decision.RequestID + "; request reset/rebuild or requirement change to continue"}}}
	}
	_, err = e.commit(state, revision, expectedFingerprint)
	return err
}

func (s *State) abortedSliceMember(receipt SliceReceipt) bool {
	r := receipt.Receipt
	if r.Status != "ABORTED" || r.MasterRunID != s.RunID || s.Phase == runtime.PhaseTerminal {
		return false
	}
	for creation := s.ChildCreation; creation != nil; creation = creation.Previous {
		for _, child := range creation.Children {
			if child.RunID == r.ChildRunID && child.SliceID == r.SliceID && digestOfCanonical(creation.Topology) == digestOfCanonical(r.Topology) {
				return true
			}
		}
	}
	return false
}

// SliceReceiptCurrent derives eligibility from the current master context;
// historical accepted evidence remains readable when that context changes.
func (s *State) SliceReceiptCurrent(receipt SliceReceipt) bool {
	r := receipt.Receipt
	return s.SliceChild == nil && s.Phase != runtime.PhaseTerminal && !s.abortRequested() && s.errWriteBarrier("slice receipt acceptance") == nil &&
		s.Topology != nil && s.Topology.Decision == topologySplit && s.RouteMode == routeModeMerge &&
		r.MasterRunID == s.RunID && slices.Contains(s.Topology.Slices, r.SliceID) && s.SliceChildren[r.SliceID] == r.ChildRunID &&
		(r.RequirementRevision == s.RequirementRevision() || s.childReceiptCarried(receipt) || r.Status == "ABORTED") && (digestOfCanonical(r.Topology) == digestOfCanonical(*s.Topology) || s.childReceiptCarried(receipt)) && (r.Status == "ABORTED" || s.SliceResetAuthorities[r.ChildRunID].Decision.RequestID == r.ResetRequestID)
}

func (s *State) sliceReceiptProgress() (ready, expected int) {
	if s.Topology == nil || s.Topology.Decision != topologySplit || s.RouteMode != routeModeMerge {
		return 0, 0
	}
	for _, sliceID := range s.Topology.Slices {
		if receipt, accepted := s.SliceReceipts[s.SliceChildren[sliceID]]; accepted && receipt.Receipt.Status == runtime.PhaseSliceReady && receipt.Receipt.SliceID == sliceID && s.SliceReceiptCurrent(receipt) {
			ready++
		}
	}
	return ready, len(s.Topology.Slices)
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
	return !s.SliceChild.ParentReviewPending && !series.FreshOwed && s.confirmedRequirementRevision(s.SliceChild.RequirementRevision)
}

// FreezeAbortedSlice replaces a success checkpoint with the abort authority only
// after registered cleanup is proven. Its frozen value survives state deletion
// in the ordinary terminal summary and is discovered through the same claim.
func (e *Engine) FreezeAbortedSlice(endedAt, expectedFingerprint string) error {
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s.SliceChild == nil {
		return nil
	}
	d, ok := s.AbortDecision()
	record, recorded := s.AbortEvent()
	if !ok || !recorded || s.Phase != runtime.PhaseTerminal || !s.ResourcesCleaned() {
		return validationError("aborted slice receipt requires terminal cleanup and exact authority")
	}
	if s.SliceReceipt != nil && s.SliceReceipt.Receipt.Status == "ABORTED" {
		return nil
	}
	r := SliceTerminalReceipt{OriginRequirementRevision: s.SliceChild.RequirementRevision, ParentRequirement: s.SliceChild.ParentChange, ParentReset: s.SliceChild.ParentReset, Status: "ABORTED", ChildRunID: s.RunID, SliceID: s.SliceChild.SliceID, MasterRunID: s.SliceChild.MasterRunID, Envelope: e.store.Envelope(), Snapshot: s.AcceptedNativeSnapshot(), Tree: s.PromotedDigest, Digest: s.PromotedDigest, RequirementRevision: s.RequirementRevision(), Topology: copyTopology(*s.Topology), RouteMode: s.RouteMode, SelectedGates: slices.Clone(s.SelectedGates), Cost: s.Cost, Abort: &AbortAuthority{Decision: d, Event: record}, ResetRequestID: s.resetRequestID(), OwnerTranscript: s.OwnerTranscript, OwnerStartedAt: s.OwnerStartedAt, OwnerEndedAt: endedAt, LastEvent: &SliceEventIdentity{EventID: d.EventID, RequestID: d.RequestID, Digest: record.Digest, Revision: record.Acceptance.Revision}, LastRequest: &SliceRequestIdentity{RequestID: d.RequestID, EventID: d.EventID, RequestDigest: s.requestDigest(d.RequestID), DecisionDigest: record.Digest, Revision: d.Revision}}
	if s.SliceChild.ParentAbort != nil {
		r.Abort = s.SliceChild.ParentAbort
		r.LastRequest.RequestDigest = r.Abort.RequestDigest
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	var frozen SliceTerminalReceipt
	if err = json.Unmarshal(data, &frozen); err != nil {
		return err
	}
	s.SliceReceipt = &SliceReceipt{Receipt: frozen, Digest: frozen.CanonicalDigest()}
	_, err = e.commit(s, revision, expectedFingerprint)
	return err
}
