package protocol

import (
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// GranularityReview is the readiness worker's proposed development contract.
// Confirmation owns authorization; Results owns this proposal and raw evidence.
type GranularityReview struct {
	Reason           string             `json:"reason"`
	Batches          []DevelopmentBatch `json:"batches"`
	VerificationTier string             `json:"verificationTier"`
	CheckpointGroups [][]string         `json:"checkpointGroups,omitempty"`
	CheckpointReason string             `json:"checkpointReason,omitempty"`
}
type DevelopmentBatch struct {
	ID            string                 `json:"id"`
	Subtasks      []DevelopmentSubtask   `json:"subtasks"`
	DependsOn     []string               `json:"dependsOn,omitempty"`
	Deliverable   string                 `json:"deliverable"`
	PublicEntry   string                 `json:"publicEntry"`
	Artifacts     []string               `json:"artifacts"`
	Interfaces    string                 `json:"interfaces"`
	StateBoundary string                 `json:"stateBoundary"`
	Risk          string                 `json:"risk"`
	DoD           []DevelopmentCriterion `json:"dod"`
	Checks        []DevelopmentCheck     `json:"checks"`
	Rollback      string                 `json:"rollback"`
	Recovery      string                 `json:"recovery"`
	Handoff       string                 `json:"handoff"`
	ContextCost   string                 `json:"contextCost"`
	Spike         bool                   `json:"spike,omitempty"`
}
type DevelopmentSubtask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}
type DevelopmentCriterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}
type DevelopmentCheck struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}
type DevelopmentCompletion struct {
	DoD     map[string]string `json:"dod"`
	Checks  map[string]string `json:"checks"`
	Handoff string            `json:"handoff"`
}
type DevelopmentPlanConfirmation struct {
	ReviewActionID string `json:"reviewActionId"`
	Digest         string `json:"digest"`
	// Proposal 是 child run 的片内开发计划内联提案：child 的整体粒度审随
	// master 继承、无自身 readiness 候选可引用，片内计划随路线决定一并产
	// 生并经同一次用户决定确认（ADR-002 决策 1「显式批次计划在拆分决定
	// 与路线确认时一并产生、经同一次用户决定确认并留痕」）。携带
	// Proposal 时 ReviewActionID 必空、Digest 必等于提案内容摘要；普通
	// run 不携带（计划经 ReviewActionID 引用 readiness 提案）。
	Proposal *GranularityReview `json:"proposal,omitempty"`
}

// DevelopmentPlanPreview is computed from the proposal and the proposed route.
// Tasks are a projection of that contract, not another mutable plan ledger.
type DevelopmentPlanPreview struct {
	Confirmation DevelopmentPlanConfirmation `json:"confirmation"`
	Proposal     GranularityReview           `json:"proposal"`
	Tasks        []runtime.Batch             `json:"tasks"`
}
type DevelopmentInput struct {
	Batch        DevelopmentBatch        `json:"batch"`
	Dependencies map[string]WorkerResult `json:"dependencies"`
}

func validateGranularity(p *GranularityReview) error {
	if p == nil || strings.TrimSpace(p.Reason) == "" || len(p.Batches) == 0 {
		return validationError("readiness requires a granularity review with batches and rationale")
	}
	if p.VerificationTier != "" && p.VerificationTier != "FINAL" && p.VerificationTier != "CHECKPOINT" {
		return validationError("verification tier must be FINAL or CHECKPOINT")
	}
	ids := map[string]int{}
	subtasks := map[string]bool{}
	for i, b := range p.Batches {
		if b.ID == "" {
			return validationError("batch ID is required")
		}
		if _, ok := ids[b.ID]; ok {
			return validationError("duplicate batch ID")
		}
		ids[b.ID] = i
		for _, value := range []string{b.Deliverable, b.PublicEntry, b.Interfaces, b.StateBoundary, b.Risk, b.Rollback, b.Recovery, b.Handoff, b.ContextCost} {
			if strings.TrimSpace(value) == "" {
				return validationError("batch boundary, risk, recovery and handoff fields are required")
			}
		}
		if len(b.Subtasks) == 0 || len(b.DoD) == 0 || len(b.Checks) == 0 || len(b.Artifacts) == 0 {
			return validationError("batch requires subtasks, artifacts, DoD and verification")
		}
		for _, sub := range b.Subtasks {
			if sub.ID == "" || sub.Description == "" || subtasks[sub.ID] {
				return validationError("subtask mapping must be complete and unique")
			}
			subtasks[sub.ID] = true
		}
		criteria := map[string]bool{}
		for _, d := range b.DoD {
			if d.ID == "" || d.Description == "" || criteria[d.ID] {
				return validationError("DoD IDs and descriptions must be unique and complete")
			}
			criteria[d.ID] = true
		}
		checks := map[string]bool{}
		for _, c := range b.Checks {
			if c.ID == "" || c.Command == "" || checks[c.ID] {
				return validationError("verification IDs and commands must be unique and complete")
			}
			checks[c.ID] = true
		}
		if b.Spike && i != 0 {
			return validationError("a spike must be the first batch")
		}
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return validationError("batch dependencies contain a cycle")
		}
		if done[id] {
			return nil
		}
		i, ok := ids[id]
		if !ok {
			return validationError("unknown batch dependency")
		}
		visiting[id] = true
		seen := map[string]bool{}
		for _, dep := range p.Batches[i].DependsOn {
			if seen[dep] {
				return validationError("duplicate batch dependency")
			}
			seen[dep] = true
			if err := visit(dep); err != nil {
				return err
			}
		}
		delete(visiting, id)
		done[id] = true
		return nil
	}
	for id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	if p.Batches[0].Spike {
		var dependsOnSpike func(string) bool
		dependsOnSpike = func(id string) bool {
			if id == p.Batches[0].ID {
				return true
			}
			for _, dep := range p.Batches[ids[id]].DependsOn {
				if dependsOnSpike(dep) {
					return true
				}
			}
			return false
		}
		for _, b := range p.Batches[1:] {
			if !dependsOnSpike(b.ID) {
				return validationError("development must consume the initial spike conclusion")
			}
		}
	}
	if p.VerificationTier != "CHECKPOINT" {
		if len(p.CheckpointGroups) > 0 || p.CheckpointReason != "" {
			return validationError("FINAL has no checkpoint grouping")
		}
		return nil
	}
	if p.CheckpointReason == "" || len(p.CheckpointGroups) < 2 {
		return validationError("CHECKPOINT requires structural rationale and multiple groups")
	}
	groups := map[string]int{}
	for i, g := range p.CheckpointGroups {
		if len(g) == 0 {
			return validationError("empty checkpoint group")
		}
		for _, id := range g {
			if _, ok := ids[id]; !ok {
				return validationError("unknown checkpoint batch")
			}
			if _, ok := groups[id]; ok {
				return validationError("duplicate checkpoint batch")
			}
			groups[id] = i
		}
	}
	if len(groups) != len(ids) {
		return validationError("checkpoint groups must cover all batches")
	}
	for _, b := range p.Batches {
		for _, dep := range b.DependsOn {
			if groups[dep] > groups[b.ID] {
				return validationError("checkpoint dependencies must not point to a later group")
			}
		}
	}
	return nil
}
func (s *State) readinessProposal() (string, *GranularityReview) {
	series := s.ReviewSeries.StartReadiness
	if !s.effectivePass(series) {
		return "", nil
	}
	for _, candidate := range series.RawCandidates {
		if candidate.ResultDigest != series.Effective.ResultDigest {
			continue
		}
		result := s.Results[candidate.DispatchID]
		if result.Review != nil && result.Review.Action == review.StartReadiness {
			return candidate.DispatchID, result.Review.GranularityReview
		}
	}
	return "", nil
}
func planTask(cd *compiler.CompiledDefinition, p *GranularityReview, id string) runtime.TaskKey {
	return pipelineTask(cd, "engine.development.batch", "plan:"+strings.TrimPrefix(planContractDigest(p), "sha256:")+":"+id)
}
func (s *State) developmentPlanPreview(cd *compiler.CompiledDefinition, route RouteDecision) (*DevelopmentPlanPreview, error) {
	id, p := s.readinessProposal()
	if s.SliceChild != nil {
		// child 的片内计划取自决定载荷的内联提案（宿主从 master 继承的整
		// 体粒度审派生），无自身 readiness 候选。
		if route.DevelopmentPlan == nil {
			return nil, validationError("a slice child route must carry its slice development plan")
		}
		id, p = "", route.DevelopmentPlan.Proposal
	}
	if err := validateGranularity(p); err != nil {
		return nil, err
	}
	preview := &DevelopmentPlanPreview{Proposal: *p}
	for _, b := range p.Batches {
		group := runtime.Batch{ID: runtime.BatchID(b.ID), Tasks: []runtime.TaskKey{s.currentPlanTask(cd, p, b.ID)}}
		for _, dep := range b.DependsOn {
			group.DependsOn = append(group.DependsOn, runtime.BatchID(dep))
		}
		preview.Tasks = append(preview.Tasks, group)
	}
	if s.SliceChild != nil {
		preview.Confirmation = DevelopmentPlanConfirmation{Digest: planContractDigest(p), Proposal: p}
		return preview, nil
	}
	route.DevelopmentPlan = nil
	solutionRevision := s.RequirementRevision()
	if s.IntakeConfirmationReceipt != nil && s.IntakeConfirmationReceipt.SolutionRevision != "" {
		solutionRevision = s.IntakeConfirmationReceipt.SolutionRevision
	}
	preview.Confirmation = DevelopmentPlanConfirmation{ReviewActionID: id, Digest: digestOfCanonical(struct {
		Review      string
		Requirement string
		Solution    string
		Topology    *TopologyBinding
		Route       RouteDecision
		Tasks       []runtime.Batch
	}{id, s.RequirementRevision(), solutionRevision, s.Topology, route, preview.Tasks})}
	return preview, nil
}
func (e *Engine) DevelopmentPlan(route RouteDecision) (*DevelopmentPlanPreview, error) {
	_, s, err := e.load()
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, &RejectedError{Code: CodeNotInitialized}
	}
	return s.developmentPlanPreview(e.cfg.Definition, route)
}
func (s *State) confirmedDevelopmentPlan() (string, *GranularityReview, string) {
	if s.SliceChild != nil {
		return s.confirmedSlicePlan()
	}
	var request, revision string
	var latest uint64
	var p *GranularityReview
	for id, d := range s.Decisions {
		if d.Control != ControlSelectRoute || d.Review == nil || d.Review.Route == nil || d.Review.Route.DevelopmentPlan == nil || d.Revision < latest {
			continue
		}
		source := s.Results[d.Review.Route.DevelopmentPlan.ReviewActionID]
		if source.Review == nil {
			continue
		}
		request, p, revision, latest = id, source.Review.GranularityReview, source.Review.RequirementRevision, d.Revision
	}
	// Identical confirmations preserve the original execution contract reference.
	for id, d := range s.Decisions {
		if d.Revision >= latest || d.Review == nil || d.Review.Route == nil || d.Review.Route.DevelopmentPlan == nil {
			continue
		}
		source := s.Results[d.Review.Route.DevelopmentPlan.ReviewActionID]
		if source.Review != nil && planContractDigest(source.Review.GranularityReview) == planContractDigest(p) {
			request, revision, latest = id, source.Review.RequirementRevision, d.Revision
		}
	}
	return request, p, revision
}

// confirmedSlicePlan 取 child 已确认的片内计划：最新路线决定的内联提案
// 为准；同摘要的更早决定保留原引用（与普通 run 的执行契约引用保留形态
// 一致）。返回的计划绑定 revision＝原确认决定时点的权威需求 revision（账
// 本中代数不晚于该决定的最近确认；空账本回退受理确认）。
func (s *State) confirmedSlicePlan() (string, *GranularityReview, string) {
	var request string
	var latest uint64
	var p *GranularityReview
	for id, d := range s.Decisions {
		if d.Control != ControlSelectRoute || d.Revision < latest {
			continue
		}
		if proposal := s.decisionPlanProposal(d); proposal != nil {
			request, p, latest = id, proposal, d.Revision
		}
	}
	for id, d := range s.Decisions {
		if d.Revision >= latest {
			continue
		}
		if proposal := s.decisionPlanProposal(d); proposal != nil && planContractDigest(proposal) == planContractDigest(p) {
			request, latest = id, d.Revision
		}
	}
	revision := ""
	for _, entry := range s.RequirementRevisions {
		if entry.StateRevision <= latest {
			revision = entry.Revision
		}
	}
	if revision == "" && s.IntakeConfirmationReceipt != nil {
		revision = s.IntakeConfirmationReceipt.RequirementRevision
	}
	return request, p, revision
}

// decisionPlanProposal 返回一条 SELECT_ROUTE 决定携带的计划提案来源：
// child 读内联提案，普通 run 经 ReviewActionID 引用 readiness 结果。
func (s *State) decisionPlanProposal(d RecordedDecision) *GranularityReview {
	if d.Review == nil || d.Review.Route == nil || d.Review.Route.DevelopmentPlan == nil {
		return nil
	}
	if s.SliceChild != nil {
		return d.Review.Route.DevelopmentPlan.Proposal
	}
	source := s.Results[d.Review.Route.DevelopmentPlan.ReviewActionID]
	if source.Review == nil {
		return nil
	}
	return source.Review.GranularityReview
}

// latestSlicePlanProposal 返回 child 最近一次路线决定携带的片内计划提案
// （child 的当前提案权威：重选携带新提案时确认计划随之切换）。
func (s *State) latestSlicePlanProposal() *GranularityReview {
	var latest uint64
	var p *GranularityReview
	for _, d := range s.Decisions {
		if d.Control != ControlSelectRoute || d.Revision < latest {
			continue
		}
		if proposal := s.decisionPlanProposal(d); proposal != nil {
			p, latest = proposal, d.Revision
		}
	}
	return p
}
func (s *State) plannedBatchSource(task runtime.TaskKey, cd *compiler.CompiledDefinition) (DevelopmentBatch, bool) {
	original := s.sourceValidationTask(task)
	for _, d := range s.Decisions {
		p := s.decisionPlanProposal(d)
		if p == nil {
			continue
		}
		for _, b := range p.Batches {
			if planTask(cd, p, b.ID) == original {
				return b, true
			}
		}
	}
	return DevelopmentBatch{}, false
}
func (s *State) currentPlanTask(cd *compiler.CompiledDefinition, p *GranularityReview, id string) runtime.TaskKey {
	task := planTask(cd, p, id)
	var batch DevelopmentBatch
	for _, b := range p.Batches {
		if b.ID == id {
			batch = b
		}
	}
	for _, entry := range s.RequirementRevisions {
		for i, c := range entry.WorkDecisions {
			prior, known := s.plannedBatchSource(c.Work.Task, cd)
			if c.Work.Task == task || known && prior.ID == id && (c.Disposition == RequirementWorkRedo || digestOfCanonical(prior) == digestOfCanonical(batch)) {
				if c.Disposition == RequirementWorkRedo {
					task = replacementWorkTask(entry, i)
				} else {
					task = c.Work.Task
				}
			}
		}
	}
	return task
}
func (s *State) developmentResultRecord(task runtime.TaskKey) *WorkerResult {
	for action, r := range s.Results {
		if _, obsolete := s.ObsoleteActions[action]; obsolete {
			continue
		}
		if r.Validation != nil && r.Validation.Binding.Task == task && r.Outcome == OutcomePass && r.Validation.Development != nil {
			return &r
		}
	}
	return nil
}
func (s *State) developmentResult(task runtime.TaskKey) *DevelopmentEvidence {
	record := s.developmentResultRecord(task)
	if record == nil {
		return nil
	}
	return record.Validation.Development
}
func validateDevelopmentCompletion(batch DevelopmentBatch, e *DevelopmentEvidence) error {
	if e.Completion == nil || strings.TrimSpace(e.Completion.Handoff) == "" {
		return validationError("batch completion requires DoD, verification and handoff evidence")
	}
	if len(e.Completion.DoD) != len(batch.DoD) || len(e.Completion.Checks) != len(batch.Checks) {
		return validationError("batch completion must cover exactly its confirmed DoD and checks")
	}
	for _, d := range batch.DoD {
		if strings.TrimSpace(e.Completion.DoD[d.ID]) == "" {
			return validationError("missing batch DoD evidence: " + d.ID)
		}
	}
	for _, c := range batch.Checks {
		if strings.TrimSpace(e.Completion.Checks[c.ID]) == "" {
			return validationError("missing batch verification evidence: " + c.ID)
		}
	}
	return nil
}
func (s *State) developmentBatch(task runtime.TaskKey, cd *compiler.CompiledDefinition) (string, *GranularityReview, DevelopmentBatch, bool) {
	request, p, _ := s.confirmedDevelopmentPlan()
	if p == nil {
		return "", nil, DevelopmentBatch{}, false
	}
	for _, b := range p.Batches {
		if s.currentPlanTask(cd, p, b.ID) == task {
			return request, p, b, true
		}
	}
	return "", nil, DevelopmentBatch{}, false
}
func (s *State) planTasks(cd *compiler.CompiledDefinition) []runtime.TaskKey {
	_, p, _ := s.confirmedDevelopmentPlan()
	var tasks []runtime.TaskKey
	if p != nil {
		for _, b := range p.Batches {
			tasks = append(tasks, s.currentPlanTask(cd, p, b.ID))
		}
	}
	return tasks
}
func (s *State) developmentInput(task runtime.TaskKey, cd *compiler.CompiledDefinition) *DevelopmentInput {
	_, p, b, ok := s.developmentBatch(task, cd)
	if !ok {
		return nil
	}
	input := &DevelopmentInput{Batch: b, Dependencies: map[string]WorkerResult{}}
	for _, dep := range b.DependsOn {
		if r := s.developmentResultRecord(s.currentPlanTask(cd, p, dep)); r != nil {
			input.Dependencies[dep] = *r
		}
	}
	return input
}
func (e *Engine) validatePlanConfirmation(s *State, route *RouteDecision) error {
	if !pipelineEnabled(s, e.cfg.Definition) {
		return nil
	}
	preview, err := s.developmentPlanPreview(e.cfg.Definition, *route)
	if err != nil {
		return err
	}
	// 确认的形态按 run 形态分流：child 的片内计划随路线决定内联确认
	// （ADR-002 决策 1）——提案必填已由 preview 校验、不引用 readiness 派
	// 发，确认摘要＝提案内容摘要；普通 run 经 ReviewActionID 引用 readiness
	// 提案。
	if s.SliceChild != nil {
		if route.DevelopmentPlan.ReviewActionID != "" || *route.DevelopmentPlan != preview.Confirmation {
			return validationError("a slice child route must confirm its exact inline slice development plan")
		}
	} else if route.DevelopmentPlan == nil || *route.DevelopmentPlan != preview.Confirmation {
		return validationError("route must confirm the exact readiness development plan")
	}
	// 计划换约纪律不分流：已确认计划与本次提案内容不一致时，逐批变更须
	// 有用户工作决定授权——child 的已确认片内计划与普通 run 的 readiness
	// 计划经同一 confirmedDevelopmentPlan 读取、同一判定。
	_, prior, _ := s.confirmedDevelopmentPlan()
	if prior != nil && planContractDigest(prior) != planContractDigest(&preview.Proposal) {
		for _, batch := range prior.Batches {
			key := s.currentPlanTask(e.cfg.Definition, prior, batch.ID)
			same := false
			for _, next := range preview.Proposal.Batches {
				if digestOfCanonical(next) == digestOfCanonical(batch) {
					same = true
				}
			}
			var choice *RequirementWorkDecision
			for i := len(s.RequirementRevisions) - 1; i >= 0 && choice == nil; i-- {
				entry := s.RequirementRevisions[i]
				for j, c := range entry.WorkDecisions {
					if c.Work.Task == key || c.Disposition == RequirementWorkRedo && replacementWorkTask(entry, j) == key {
						copy := c
						choice = &copy
						break
					}
				}
			}
			if choice == nil || choice.Disposition == RequirementWorkCarry && !same {
				return validationError(fmt.Sprintf("changed plan requires a matching explicit work disposition for batch %s", batch.ID))
			}
		}
	}
	return nil
}

// planCheckpoint selects the first uncleared group using the original waves.
func (s *State) planCheckpoint(cd *compiler.CompiledDefinition, p *GranularityReview) ([]runtime.TaskKey, bool) {
	if p.VerificationTier != "CHECKPOINT" {
		return s.planTasks(cd), false
	}
	var tasks []runtime.TaskKey
	for i, group := range p.CheckpointGroups {
		for _, id := range group {
			tasks = append(tasks, s.currentPlanTask(cd, p, id))
		}
		if i == len(p.CheckpointGroups)-1 {
			return tasks, false
		}
		delivery, complete := false, true
		for _, b := range p.Batches {
			task := s.currentPlanTask(cd, p, b.ID)
			if !slices.Contains(tasks, task) {
				continue
			}
			delivery = delivery || !b.Spike
			complete = complete && s.developmentResult(task) != nil
		}
		if !delivery && complete {
			continue
		}
		cleared := false
		if s.Validation != nil {
			for _, w := range s.Validation.Waves {
				if w.Status == "COMPLETE" && w.Scope == "FULL" && sameTaskSet(w.CheckpointTasks, tasks) {
					cleared = true
					break
				}
			}
		}
		if !cleared {
			return tasks, true
		}
	}
	return tasks, false
}
func sameTaskSet(a, b []runtime.TaskKey) bool {
	if len(a) != len(b) {
		return false
	}
	for _, task := range a {
		if !slices.Contains(b, task) {
			return false
		}
	}
	return true
}
func (s *State) projectDevelopmentTasks(view *decision.State, cd *compiler.CompiledDefinition) {
	_, p, _ := s.confirmedDevelopmentPlan()
	if p == nil || !s.developmentPlanCurrent(p) {
		return
	}
	allowed, _ := s.planCheckpoint(cd, p)
	for _, b := range p.Batches {
		key := s.currentPlanTask(cd, p, b.ID)
		if s.developmentResult(key) != nil {
			continue
		}
		blocked := !slices.Contains(allowed, key) || !s.workspaceReady(key, cd)
		group := runtime.Batch{ID: runtime.BatchID(b.ID)}
		for _, dep := range b.DependsOn {
			task := s.currentPlanTask(cd, p, dep)
			group.Tasks = append(group.Tasks, task)
			if s.developmentResult(task) == nil {
				blocked = true
			}
		}
		if !group.Complete(s.Tasks) {
			blocked = true
		}
		reason := decision.WaitReason("")
		if blocked {
			reason = decision.WaitDependencies
		}
		if retry, ok := s.EndedAttempts[key]; ok && retry.RetryExhausted {
			reason = decision.WaitCapacity
		}
		view.RequiredTasks[key.Step] = append(view.RequiredTasks[key.Step], decision.RequiredTask{Task: key, BlockedBy: reason})
	}
}
func (s *State) developmentSources(cd *compiler.CompiledDefinition) ([]runtime.TaskKey, []string, string, bool) {
	_, p, _ := s.confirmedDevelopmentPlan()
	if p == nil || !s.developmentPlanCurrent(p) {
		return nil, nil, "", false
	}
	tasks, _ := s.planCheckpoint(cd, p)
	var sources []runtime.TaskKey
	var paths []string
	for _, b := range p.Batches {
		task := s.currentPlanTask(cd, p, b.ID)
		if !slices.Contains(tasks, task) {
			continue
		}
		e := s.developmentResult(task)
		if e == nil {
			return nil, nil, "", false
		}
		if b.Spike {
			continue
		}
		sources = append(sources, task)
		for _, path := range e.Paths {
			if !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
	}
	base := ""
	var latest uint64
	for _, work := range s.repairWork() {
		if work.result == nil {
			continue
		}
		sources = append(sources, work.task)
		for _, path := range work.result.Validation.Development.Paths {
			if !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
		if issued := s.issuedRevisionOf(work.result.ActionID); issued > latest {
			latest, base = issued, work.result.Validation.Binding.Candidate
		}
	}
	slices.SortFunc(sources, func(a, b runtime.TaskKey) int { return strings.Compare(a.String(), b.String()) })
	slices.Sort(paths)
	return sources, paths, base, len(sources) > 0
}

func planContractDigest(p *GranularityReview) string {
	if p == nil {
		return ""
	}
	value := *p
	if value.VerificationTier == "" {
		value.VerificationTier = "FINAL"
	}
	return digestOfCanonical(value)
}
func (s *State) developmentPlanCurrent(p *GranularityReview) bool {
	_, proposed := s.readinessProposal()
	if s.SliceChild != nil {
		// child 的计划确认权威是路线决定本身：当前提案＝最近一次决定携带
		// 的内联提案（重选携带新提案时旧计划停止投影）。
		proposed = s.latestSlicePlanProposal()
	}
	return proposed != nil && planContractDigest(proposed) == planContractDigest(p)
}
