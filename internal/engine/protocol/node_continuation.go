package protocol

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// BusinessNode names an existing workflow responsibility. Missing inputs are
// obtained through that responsibility's ordinary preparation and Ask paths.
type BusinessNode struct {
	ID          string              `json:"id"`
	Kind        string              `json:"kind"`
	Mode        coverage.ReviewKind `json:"mode,omitempty"`
	Gate        string              `json:"gate,omitempty"`
	Batch       string              `json:"batch,omitempty"`
	Label       string              `json:"label,omitempty"`
	Task        *runtime.TaskKey    `json:"task,omitempty"`
	Missing     []string            `json:"missing,omitempty"`
	Unavailable string              `json:"unavailable,omitempty"`
}

type NodeContinuationRequest struct {
	Target        string `json:"target"`
	StateRevision uint64 `json:"stateRevision"`
	PreviewDigest string `json:"previewDigest"`
}

// An Ask or configuration application owns its from/to and execution preview.
// Its decision or application effect records the resulting work dispositions.
type NodeContinuationPreview struct {
	ParentRunID         string                    `json:"parentRunId,omitempty"`
	ParentRevision      uint64                    `json:"parentRevision,omitempty"`
	EnteredMainline     bool                      `json:"enteredMainline,omitempty"`
	ParentClaims        []string                  `json:"parentClaims,omitempty"`
	From                []string                  `json:"from"`
	FromPhase           runtime.RunPhase          `json:"fromPhase"`
	To                  BusinessNode              `json:"to"`
	RequirementRevision string                    `json:"requirementRevision"`
	StateRevision       uint64                    `json:"stateRevision"`
	Work                []RequirementWorkDecision `json:"work"`
	ReviewActions       []string                  `json:"reviewActions,omitempty"`
	Effects             []string                  `json:"effects"`
	Digest              string                    `json:"digest"`
}

func (p NodeContinuationPreview) CanonicalDigest() string {
	p.Digest = ""
	return digestOfCanonical(p)
}

type NodeContinuationDecision struct {
	PreviewDigest   string                    `json:"previewDigest"`
	Reason          string                    `json:"reason"`
	FreshnessToken  string                    `json:"freshnessToken"`
	Work            []RequirementWorkDecision `json:"work"`
	AppliedRevision uint64                    `json:"appliedRevision,omitempty"`
}

// BusinessNodes enumerates domain targets, independently of the phase enum or
// the four definition NodeIDs. Selection never supplies missing gate facts.
func (s *State) BusinessNodes(cd *compiler.CompiledDefinition, parents ...Snapshot) []BusinessNode {
	if s.Phase == runtime.PhaseTerminal || s.abortRequested() {
		return nil
	}
	var nodes []BusinessNode
	add := func(kind string, missing ...string) {
		nodes = append(nodes, BusinessNode{ID: kind, Kind: kind, Missing: missing})
	}
	if s.Route == "regular" {
		if s.SliceChild == nil {
			add("intake")
			add("product-review")
			add("start-readiness")
			add("topology")
		}
		if !s.mergeMaster() {
			add("route")
		}
		_, plan, _ := s.confirmedDevelopmentPlan()
		if plan == nil && !s.mergeMaster() {
			add("development", "先取得并确认开发计划及路线")
		} else if plan != nil {
			for _, batch := range plan.Batches {
				if s.developmentBatchRequired(batch) {
					nodes = append(nodes, BusinessNode{ID: "development:" + batch.ID, Kind: "development", Batch: batch.ID, Label: batch.Deliverable})
				}
			}
		}
		if s.SliceChild == nil {
			if s.mergeMaster() {
				add("mainline-integration")
			} else if s.Topology == nil {
				add("mainline-integration", "先确认拆分拓扑并取得全部子运行收据")
			}
		}
		modes := s.validationKinds()
		if s.SliceChild == nil && !s.RouteConfirmed {
			modes = []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox}
		}
		for _, mode := range modes {
			for _, kind := range []string{"qa-design", "qa-review", "qa-execution"} {
				n := BusinessNode{ID: kind + ":" + string(mode), Kind: kind, Mode: mode}
				if s.Validation == nil || kind != "qa-design" && s.Validation.Sets[mode] == nil {
					n.Missing = []string{"先完成该模式的用例设计"}
				} else if kind == "qa-execution" && s.Validation.Candidate == nil {
					n.Missing = []string{"先冻结批准用例和最终验证候选"}
				}
				nodes = append(nodes, n)
			}
		}
		for _, gate := range s.SelectedGates {
			if gate == "blackbox" || gate == "whitebox" || gate == mergeQAGateID {
				continue
			}
			n := BusinessNode{ID: "gate:" + gate, Kind: "gate", Gate: gate}
			if s.Validation == nil || s.Validation.Candidate == nil {
				n.Missing = []string{"先冻结最终验证候选"}
			}
			nodes = append(nodes, n)
		}
		add("candidate")
		add("post-review")
		if s.nextRepairWork() == nil {
			add("repair", "先核实并处置真实返修义务")
		} else {
			for _, work := range s.repairWork() {
				task := work.task
				nodes = append(nodes, BusinessNode{ID: "repair:" + task.String(), Kind: "repair", Task: &task})
			}
		}
		if s.SliceChild != nil {
			add("slice-ready")
		}
	}
	add("cleanup")
	add("abort")
	var parent *State
	var work []RequirementWork
	if s.SliceChild != nil && len(parents) == 1 && parents[0].State != nil {
		parent = parents[0].State
		work = s.RequirementWork(cd)
	}
	for i := range nodes {
		if !s.nodeFlowEnabled(nodes[i].Kind) {
			nodes[i].Unavailable = "该职责已由用户关闭；节点继续不改变主流程选择"
			nodes[i].Missing = append(nodes[i].Missing, nodes[i].Unavailable)
			continue
		}
		if parent != nil && s.nodeCheckpointChanges(nodes[i], work, cd) {
			if parent.SliceEnteredMainline(s.RunID) {
				nodes[i].Unavailable = "该片已进入主线；普通修改由主线职责承接，重建需主运行授权"
			} else if len(parent.SliceIntegrationClaims(s.RunID)) != 0 {
				nodes[i].Unavailable = "主线正在对账此片输入，需要先由主线继续"
			}
			if nodes[i].Unavailable != "" {
				nodes[i].Missing = append(nodes[i].Missing, nodes[i].Unavailable)
			}
		}
		if len(nodes[i].Missing) == 0 && !s.nodeInputsReady(nodes[i], cd) {
			nodes[i].Missing = []string{"先通过原流程取得该职责的确认输入或完成依赖"}
		}
	}
	return nodes
}

func (s *State) businessFrom() []string {
	var from []string
	for action := range s.PendingActions {
		from = append(from, action)
	}
	for _, attempt := range s.EndedAttempts {
		if attempt.AwaitingResult && !slices.Contains(from, attempt.ActionID) {
			from = append(from, attempt.ActionID)
		}
	}
	for action := range s.PendingHostActions {
		from = append(from, action)
	}
	if len(from) == 0 {
		from = append(from, string(s.Phase))
	}
	slices.Sort(from)
	return from
}

func (e *Engine) nodeContinuationPreview(s *State, target string, revision uint64, parents ...Snapshot) (NodeContinuationPreview, error) {
	var to BusinessNode
	for _, n := range s.BusinessNodes(e.cfg.Definition, parents...) {
		if n.ID == target {
			to = n
		}
	}
	if to.ID == "" {
		return NodeContinuationPreview{}, validationError("继续目标不属于当前运行的业务节点")
	}
	p := NodeContinuationPreview{From: s.businessFrom(), FromPhase: s.Phase, To: to, RequirementRevision: s.RequirementRevision(), StateRevision: revision}
	if err := s.bindContinuationParent(&p, parents); err != nil {
		return p, err
	}
	for _, w := range s.RequirementWork(e.cfg.Definition) {
		disposition := RequirementWorkCarry
		if s.nodeOwnsTask(to, w.Task, e.cfg.Definition) {
			disposition = RequirementWorkRedo
		}
		p.Work = append(p.Work, RequirementWorkDecision{Work: w, Disposition: disposition, Reason: "按所选业务节点的现有输入和依赖继续"})
	}
	if to.Kind == "product-review" || to.Kind == "start-readiness" {
		s.eachSeriesLiveDispatch(e.reviewSeriesOf, func(action string, mark review.Action) {
			if string(mark) == to.Kind {
				p.ReviewActions = append(p.ReviewActions, action)
			}
		})
		for _, attempt := range s.EndedAttempts {
			if attempt.AwaitingResult && string(e.reviewSeriesOf(attempt.Task.Step)) == to.Kind && !slices.Contains(p.ReviewActions, attempt.ActionID) {
				p.ReviewActions = append(p.ReviewActions, attempt.ActionID)
			}
		}
		slices.Sort(p.ReviewActions)
	}
	for id, intent := range s.PendingHostActions {
		if s.nodeAffectsIntent(p, intent) {
			p.Effects = append(p.Effects, id)
		}
	}
	slices.Sort(p.Effects)
	p.Digest = p.CanonicalDigest()
	return p, nil
}

func (s *State) bindContinuationParent(p *NodeContinuationPreview, parents []Snapshot) error {
	if s.SliceChild != nil && len(parents) == 1 && parents[0].State != nil {
		parent := parents[0]
		if parent.State.RunID != s.SliceChild.MasterRunID || parent.State.SliceChildren[s.SliceChild.SliceID] != s.RunID {
			return validationError("片内节点需要原主运行成员事实")
		}
		p.ParentRunID, p.ParentRevision = parent.State.RunID, parent.Revision
		p.EnteredMainline = parent.State.SliceEnteredMainline(s.RunID)
		p.ParentClaims = parent.State.SliceIntegrationClaims(s.RunID)
	}
	return nil
}

func (s *State) validateContinuationParent(preview NodeContinuationPreview, changes bool, parent *Snapshot) error {
	entered := preview.EnteredMainline
	claims := preview.ParentClaims
	if parent != nil && parent.State != nil && s.SliceChild != nil {
		entered = parent.State.SliceEnteredMainline(s.RunID)
		claims = parent.State.SliceIntegrationClaims(s.RunID)
	}
	if changes && len(claims) != 0 {
		return validationError("主线正在对账此片输入，需要先由主线继续")
	}
	if changes && entered {
		return validationError("该片已进入主线；普通修改由主线职责承接，重建需主运行授权")
	}
	return nil
}

func (e *Engine) NodeContinuationPreview(target string, parents ...Snapshot) (NodeContinuationPreview, error) {
	revision, s, err := e.load()
	if err != nil {
		return NodeContinuationPreview{}, err
	}
	return e.nodeContinuationPreview(s, target, revision, parents...)
}

func (s *State) nodeOwnsTask(node BusinessNode, task runtime.TaskKey, cd *compiler.CompiledDefinition) bool {
	role := s.validationRole(task, cd)
	switch node.Kind {
	case "development":
		batch, known := s.plannedBatchSource(task)
		_, plan, _ := s.confirmedDevelopmentPlan()
		if role == "engine.development.batch.v6" {
			return node.Batch == "" || known && batchDependsOn(plan, batch.ID, node.Batch)
		}
		return role == "engine.qa.execution.v4" || role == "engine.gate.v3" || (role == "engine.qa.design.v6" || role == "engine.qa.review.rationale.v2") && s.validationMode(task) == coverage.ReviewWhitebox
	case "qa-design":
		return (role == "engine.qa.design.v6" || role == "engine.qa.review.rationale.v2" || role == "engine.qa.execution.v4") && s.validationMode(task) == node.Mode
	case "qa-review":
		return (role == "engine.qa.review.rationale.v2" || role == "engine.qa.execution.v4") && s.validationMode(task) == node.Mode
	case "qa-execution":
		return role == "engine.qa.execution.v4" && s.validationMode(task) == node.Mode
	case "gate":
		key := s.sourceValidationTask(task)
		return role == "engine.gate.v3" && strings.HasSuffix(key.Scope, ":"+node.Gate)
	case "repair":
		return role == "engine.repair.v7" && (node.Task == nil || *node.Task == task)
	}
	return false
}

func batchDependsOn(plan *GranularityReview, id, dependency string) bool {
	if id == dependency {
		return true
	}
	if plan != nil {
		for _, batch := range plan.Batches {
			if batch.ID == id {
				for _, parent := range batch.DependsOn {
					if batchDependsOn(plan, parent, dependency) {
						return true
					}
				}
			}
		}
	}
	return false
}

type NodeContinuationView struct {
	Preview NodeContinuationPreview `json:"preview"`
	Receipt RecordedDecision        `json:"receipt"`
	Status  string                  `json:"status"`
}

func (s *State) NodeContinuation(cd *compiler.CompiledDefinition) *NodeContinuationView {
	d, ok := s.userNodeContinuationDecision()
	if !ok || d.Decision == nil {
		return nil
	}
	preview := *d.Preview
	status := "RECONCILING"
	if d.Continuation.AppliedRevision != 0 {
		status = "APPLIED"
	} else if !s.nodeInputsReady(preview.To, cd) {
		status = "PREPARING"
	}
	return &NodeContinuationView{Preview: preview, Receipt: *d.Decision, Status: status}
}

func (s *State) nodeAffectsIntent(p NodeContinuationPreview, intent HostActionIntent) bool {
	if intent.Adapter == nil || intent.Adapter.Operation == "op.cleanup" {
		return false
	}
	if p.To.Kind == "product-review" || p.To.Kind == "start-readiness" || p.To.Kind == "abort" {
		return true
	}
	for _, choice := range p.Work {
		if choice.Disposition != RequirementWorkRedo {
			continue
		}
		if intent.Adapter.Operation == "op.check" {
			check, err := checkParameters(intent)
			if err == nil && check.SourceActionID == choice.Work.ActionID {
				return true
			}
		}
		if intent.Adapter.Operation == "op.workspace" {
			var workspace WorkspaceParameters
			data, _ := json.Marshal(intent.Adapter.Params)
			if json.Unmarshal(data, &workspace) == nil && workspace.Task != nil && *workspace.Task == choice.Work.Task {
				return true
			}
		}
		if candidate, err := candidateParameters(intent); err == nil {
			if slices.Contains(candidate.SourceTasks, choice.Work.Task) || candidate.RepairTask != nil && *candidate.RepairTask == choice.Work.Task {
				return true
			}
			for _, task := range candidate.DesignTasks {
				if task == choice.Work.Task {
					return true
				}
			}
		}
	}
	return false
}

func (e *Engine) acceptNodeContinuation(s *State, ask PendingAsk, d *DecidePayload, parent *Snapshot) error {
	if ask.Control != ControlContinueNode || ask.Continuation == nil {
		return validationError("节点继续必须绑定原 typed Ask")
	}
	if d.Choice == "decline" && d.Continuation == nil {
		return nil
	}
	n := d.Continuation
	if d.Choice != "approve" || n == nil || n.AppliedRevision != 0 || strings.TrimSpace(n.Reason) == "" || n.PreviewDigest != ask.Continuation.Digest || n.FreshnessToken != d.Token || ask.Continuation.RequirementRevision != s.RequirementRevision() {
		return validationError("节点继续需要当前原预览、授权 token 和用户理由")
	}
	if err := s.validateContinuationParent(*ask.Continuation, n.changesExecutionEpoch(ask.Continuation.To) || ask.Continuation.To.Kind == "route", parent); err != nil {
		return err
	}
	if err := e.validateRequirementWork(s, n.Work, false); err != nil {
		return err
	}
	if len(n.Work) != len(ask.Continuation.Work) {
		return validationError("节点继续需要原完整工作处置")
	}
	for i, choice := range n.Work {
		if choice.Work != ask.Continuation.Work[i].Work {
			return validationError("节点继续改变了原工作绑定")
		}
	}
	s.retireContinuationWork(*ask.Continuation, n.Work, string(d.Request))
	return nil
}

func (s *State) retireContinuationWork(preview NodeContinuationPreview, work []RequirementWorkDecision, request string) {
	for _, choice := range work {
		if choice.Disposition == RequirementWorkRedo {
			s.archiveDispatchResults(choice.Work.ActionID)
			if pending, attempt, live := s.workerActionTarget(choice.Work.ActionID); live {
				s.recordObsoleteAction(pending, attempt, "", "Workflow continuation authorized by "+request)
			}
		}
	}
	for _, action := range preview.ReviewActions {
		s.archiveDispatchResults(action)
		if pending, attempt, live := s.workerActionTarget(action); live {
			s.recordObsoleteAction(pending, attempt, "", "Workflow continuation authorized by "+request)
		}
	}
}

// continuationFact reads the two owners without adding configuration values to
// the user decision ledger.
type continuationFact struct {
	RequestID    string
	Revision     uint64
	Preview      *NodeContinuationPreview
	Continuation *NodeContinuationDecision
	Decision     *RecordedDecision
}

func (s *State) nodeContinuationDecisions() []continuationFact {
	var decisions []continuationFact
	for _, d := range s.Decisions {
		if d.Control != ControlContinueNode || d.Choice != "approve" || d.Continuation == nil {
			continue
		}
		if len(s.RequirementRevisions) > 1 && s.RequirementRevisions[len(s.RequirementRevisions)-1].StateRevision > d.Revision {
			continue
		}
		decision := d
		decisions = append(decisions, continuationFact{RequestID: d.RequestID, Revision: d.Revision, Preview: s.PendingAsks[d.RequestID].Continuation, Continuation: d.Continuation, Decision: &decision})
	}
	for index, applied := range s.AppliedConfigurations {
		if applied.Bypassed || applied.Preview == nil || applied.Effect == nil {
			continue
		}
		if applied.Effect.AppliedRevision == 0 && index != len(s.AppliedConfigurations)-1 {
			continue
		}
		revision := applied.Effect.AppliedRevision
		if revision == 0 {
			revision = applied.Revision
		}
		if len(s.RequirementRevisions) > 1 && s.RequirementRevisions[len(s.RequirementRevisions)-1].StateRevision > revision && applied.Effect.AppliedRevision != 0 {
			continue
		}
		decisions = append(decisions, continuationFact{RequestID: configurationRequest(applied.Revision), Revision: revision, Preview: applied.Preview, Continuation: applied.Effect})
	}
	slices.SortFunc(decisions, func(a, b continuationFact) int { return cmp.Compare(a.Revision, b.Revision) })
	return decisions
}

func (s *State) userNodeContinuationDecision() (continuationFact, bool) {
	entries := s.nodeContinuationDecisions()
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Decision != nil && entries[i].Decision.Control == ControlContinueNode {
			return entries[i], true
		}
	}
	return continuationFact{}, false
}

// Withdrawal is authorized only by recorded user decisions. A proposal or
// pending Ask never withdraws a signed native responsibility.
func (s *State) NodeContinuationWithdraws(intent HostActionIntent) bool {
	for _, d := range s.nodeContinuationDecisions() {
		if d.Decision == nil || d.Continuation.AppliedRevision != 0 {
			continue
		}
		p := *d.Preview
		p.Work = d.Continuation.Work
		if s.nodeAffectsIntent(p, intent) {
			return true
		}
	}
	return false
}

// workAuthorizations is a transient view of the two existing authority owners.
// Node continuation does not append a fictitious requirement confirmation.
func (s *State) workAuthorizations() []ConfirmedRevision {
	entries := slices.Clone(s.RequirementRevisions)
	for _, d := range s.nodeContinuationDecisions() {
		if d.Continuation.AppliedRevision == 0 {
			continue
		}
		p := d.Preview
		scope := []string{}
		if p.To.Kind == "product-review" || p.To.Kind == "start-readiness" {
			scope = append(scope, p.To.Kind)
		}
		entries = append(entries, ConfirmedRevision{Revision: p.RequirementRevision, DecidedScope: scope, WorkDecisions: d.Continuation.Work, RequestID: d.RequestID, Reason: d.Continuation.Reason, StateRevision: d.Revision})
	}
	slices.SortFunc(entries, func(a, b ConfirmedRevision) int { return cmp.Compare(a.StateRevision, b.StateRevision) })
	return entries
}

func (e *Engine) advanceNodeContinuation(s *State, nextRevision uint64) (bool, error) {
	decisions := s.nodeContinuationDecisions()
	if len(decisions) == 0 || s.requirementChangePending() || s.abortRequested() {
		return false, nil
	}
	changed := false
	latestUser, _ := s.userNodeContinuationDecision()
	for _, d := range decisions {
		if d.Decision == nil || d.Continuation.AppliedRevision != 0 {
			continue
		}
		current := d.RequestID == latestUser.RequestID
		p := d.Preview
		if current && !s.nodeInputsReady(p.To, e.cfg.Definition) {
			return changed, nil
		}
		stopped, err := e.stopControlledWorkers(s, d.RequestID, "节点继续授权 ", nextRevision, false)
		changed = stopped || changed
		if err != nil {
			return changed, err
		}
		for _, choice := range d.Continuation.Work {
			if choice.Disposition == RequirementWorkRedo {
				if _, _, live := s.workerActionTarget(choice.Work.ActionID); live {
					return changed, nil
				}
			}
		}
		for _, action := range p.ReviewActions {
			if _, _, live := s.workerActionTarget(action); live {
				return changed, nil
			}
		}
		for _, intent := range s.PendingHostActions {
			if s.NodeContinuationWithdraws(intent) {
				return changed, nil
			}
		}
		authorization := *d.Continuation
		d.Continuation = &authorization
		d.Continuation.AppliedRevision = nextRevision
		recorded := *d.Decision
		recorded.Continuation = d.Continuation
		s.Decisions[d.RequestID] = recorded
		if err := e.applyContinuationWork(s, *p, *d.Continuation, d.RequestID, d.Revision, nextRevision); err != nil {
			return changed, err
		}
		switch p.To.Kind {
		case "product-review", "start-readiness":
			mark := review.Action(p.To.Kind)
			s.ReviewSeries = review.StaleSeries(s.ReviewSeries, mark, requirementChangeSourcePrefix+d.RequestID, d.Revision)
		case "qa-review":
			if set := s.Validation.Sets[p.To.Mode]; set != nil {
				set.Status = "REVIEW"
			}
		}
		if current {
			switch p.To.Kind {
			case "route":
				s.RouteConfirmed = false
				var original RecordedDecision
				for _, decision := range s.Decisions {
					if decision.Control == ControlSelectRoute && decision.Review != nil && decision.Review.Route != nil && decision.Revision > original.Revision {
						original = decision
					}
				}
				if original.Review != nil {
					request := "continuation-route:" + d.RequestID
					s.PendingAsks[request] = PendingAsk{RequestID: request, Control: ControlSelectRoute, RouteGates: slices.Clone(original.Review.Route.Gates), Options: []AskOption{{ID: "approve", Label: "确认开发计划及路线"}}}
				}
			case "intake":
				request := "continuation-intake:" + d.RequestID
				s.PendingAsks[request] = PendingAsk{RequestID: request, Control: ControlRequestRequirementChange, BaseRevision: s.RequirementRevision(), BaseGeneration: s.requirementGeneration(), Options: []AskOption{{ID: "proceed", Label: "确认完整当前需求和方案"}, {ID: "abort", Label: "保留原确认，继续原流程"}}}

			}
		}
		s.routingReleasedCapacity = true
		changed = true
	}
	return changed, nil
}

func (e *Engine) applyContinuationWork(s *State, preview NodeContinuationPreview, effect NodeContinuationDecision, request string, revision, nextRevision uint64) error {
	// Library changes replace work within the current confirmed requirement generation.
	if s.Validation != nil && preview.To.Kind != "configuration" && effect.changesExecutionEpoch(preview.To) {
		s.Validation.Generation = nextRevision
	}
	entry := ConfirmedRevision{Revision: preview.RequirementRevision, WorkDecisions: effect.Work, RequestID: request, Reason: effect.Reason, StateRevision: revision}
	if err := e.applyWorkAuthorization(s, entry); err != nil {
		return err
	}
	if preview.To.Kind == "configuration" {
		e.rebindValidationRoute(s)
	}
	if effect.changesExecutionEpoch(preview.To) || preview.To.Kind == "route" || preview.To.Kind == "configuration" && s.SliceReceipt != nil && s.SliceReceipt.Receipt.Validation.RouteDigest != s.validationRouteDigest() {
		return s.reopenSliceCheckpoint()
	}
	return nil
}

func (d NodeContinuationDecision) changesExecutionEpoch(target BusinessNode) bool {
	return target.Kind == "product-review" || target.Kind == "start-readiness" || slices.ContainsFunc(d.Work, func(w RequirementWorkDecision) bool { return w.Disposition == RequirementWorkRedo })
}

func (s *State) nodeContinuationPending() bool {
	for _, d := range s.nodeContinuationDecisions() {
		if d.Continuation.AppliedRevision == 0 && d.Preview.To.Kind != "abort" && d.Preview.To.Kind != "cleanup" {
			return true
		}
	}
	return false
}

func (s *State) nodeFlowEnabled(kind string) bool {
	switch kind {
	case "qa-design", "qa-review", "qa-execution":
		kind = "qa"
	}
	return s.FlowNodeEnabled(kind)
}

func (s *State) nodeInputsReady(n BusinessNode, cd *compiler.CompiledDefinition) bool {
	if !s.nodeFlowEnabled(n.Kind) {
		return false
	}
	switch n.Kind {
	case "configuration":
		return true
	case "intake":
		return s.IntakeReceipt != nil
	case "product-review":
		return s.IntakeReceipt != nil
	case "start-readiness":
		return s.preDevelopmentReviewSatisfied(review.ProductReview)
	case "topology":
		return s.PreDevelopmentReviewsSatisfied() && s.readinessPreparationReady()
	case "route":
		return s.Topology != nil && s.PreDevelopmentReviewsSatisfied() && s.readinessPreparationReady()
	case "development":
		_, plan, _ := s.confirmedDevelopmentPlan()
		return plan != nil && s.developmentPlanCurrent(plan) && s.RouteConfirmed && s.PreDevelopmentReviewsSatisfied() && s.readinessPreparationReady()
	case "qa-design":
		return s.validationRequirementCurrent() && slices.Contains(s.validationKinds(), n.Mode) && (n.Mode != coverage.ReviewWhitebox || s.Validation.AuthoringWorkspace != "")
	case "qa-review":
		return s.Validation != nil && s.Validation.Sets[n.Mode] != nil && (n.Mode != coverage.ReviewWhitebox || s.Validation.Sets[n.Mode].Candidate != "")
	case "qa-execution", "gate":
		return s.Validation != nil && s.Validation.Candidate != nil && s.validateFinalCoverage() == nil
	case "candidate":
		_, _, _, complete := s.developmentSources(cd)
		return complete
	case "post-review":
		w := s.currentValidationWave()
		return w != nil && w.Status != "COLLECTING" && w.Status != "INVALIDATED"
	case "repair":
		return s.nextRepairWork() != nil
	case "mainline-integration":
		ready, expected := s.sliceReceiptProgress()
		return s.mergeMaster() && expected != 0 && ready == expected
	case "slice-ready":
		return s.SliceChild != nil && s.DeliveryComplete(cd)
	case "cleanup":
		return s.DeliveryComplete(cd) || s.Route == "lightweight"
	case "abort":
		return true
	}
	return false
}

func (s *State) nodeCheckpointChanges(target BusinessNode, work []RequirementWork, cd *compiler.CompiledDefinition) bool {
	if target.Kind == "route" {
		return true
	}
	for _, item := range work {
		if s.nodeOwnsTask(target, item.Task, cd) {
			return true
		}
	}
	return false
}
