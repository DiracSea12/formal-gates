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
	Owner         string                 `json:"owner,omitempty"`
	Route         *RouteDecision         `json:"route,omitempty"`
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

const (
	CheckEdit      = "EDIT"
	CheckFullBuild = "FULL_BUILD"
	CheckFullTests = "FULL_TESTS"
)

func DevelopmentCheckPurposes() []string {
	return []string{"", CheckEdit, CheckFullBuild, CheckFullTests}
}

type DevelopmentCheck struct {
	Purpose string `json:"purpose,omitempty"`
	ID      string `json:"id"`
	Command string `json:"command,omitempty"`
	Goal    string `json:"goal,omitempty"`
}
type DevelopmentCompletion struct {
	DoD     map[string]string `json:"dod"`
	Handoff string            `json:"handoff"`
}

// DevelopmentStarted projects current or explicitly carried issued work.
// Completed work retains its binding; obsolete and replaced duties do not.
func (s *State) DevelopmentStarted() bool {
	for action, binding := range s.DispatchBindings {
		if binding.PlanRequest == "" || binding.BatchID == "" {
			continue
		}
		if _, obsolete := s.ObsoleteActions[action]; obsolete {
			continue
		}
		current, _ := s.requirementWorkReplacement(binding.Task)
		if current != binding.Task {
			continue
		}
		if binding.RequirementRevision == s.RequirementRevision() || s.validationWorkCarryAuthorized(action, binding.Task) {
			return true
		}
	}
	return false
}

type DevelopmentPlanConfirmation struct {
	ReviewActionID string `json:"reviewActionId"`
	Digest         string `json:"digest"`
	// Proposal carries the child's inline execution contract, projected from
	// the confirmed master's explicit owners or supplied for route confirmation.
	// Children inherit readiness and have no local review candidate to reference.
	// ReviewActionID is empty and Digest binds the exact proposal contents.
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
	Result       *WorkerResult           `json:"result,omitempty"`
	// Checks retains the executed prefix of Batch.Checks for its frozen source.
	Checks []CheckEvidence `json:"checks,omitempty"`
}

// GranularityVerificationTiers is the finite proposal tier domain; empty uses FINAL.
func GranularityVerificationTiers() []string { return []string{"", "FINAL", "CHECKPOINT"} }

func ValidateGranularity(p *GranularityReview) error {
	if p == nil || strings.TrimSpace(p.Reason) == "" || len(p.Batches) == 0 {
		return validationError("readiness requires a granularity review with batches and rationale")
	}
	if !slices.Contains(GranularityVerificationTiers(), p.VerificationTier) {
		return validationError("verification tier must be FINAL or CHECKPOINT")
	}
	ids := map[string]int{}
	subtasks := map[string]bool{}
	for i, b := range p.Batches {
		if b.ID == "" {
			return validationError("batch ID is required")
		}
		if b.Owner != "" && !validSliceID(b.Owner) {
			return validationError("batch owner must name a slice or the master merge scope")
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
			if c.ID == "" || checks[c.ID] || !slices.Contains(DevelopmentCheckPurposes(), c.Purpose) {
				return validationError("verification IDs and purposes must be unique and complete")
			}
			if (strings.TrimSpace(c.Command) == "" && strings.TrimSpace(c.Goal) == "") || (c.Command != "" && c.Goal != "") || (c.Purpose == CheckEdit && c.Command == "") {
				return validationError("verification requires a command or goal; EDIT requires a command")
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

// ReadinessProposal projects the effective readiness worker's original contract.
func (s *State) ReadinessProposal() *GranularityReview {
	_, proposal := s.readinessProposal()
	return proposal
}
func planTask(cd *compiler.CompiledDefinition, p *GranularityReview, id string) runtime.TaskKey {
	return pipelineTask(cd, "engine.development.batch.v4", planTaskScope(p, id))
}
func planTaskScope(plan *GranularityReview, id string) string {
	return "plan:" + strings.TrimPrefix(planContractDigest(plan), "sha256:") + ":" + id
}
func (s *State) developmentPlanPreview(cd *compiler.CompiledDefinition, route RouteDecision) (*DevelopmentPlanPreview, error) {
	id, p := s.readinessProposal()
	if s.SliceChild != nil {
		// Child route confirmation carries its exact inline proposal.
		if route.DevelopmentPlan == nil {
			return nil, validationError("a slice child route must carry its slice development plan")
		}
		id, p = "", route.DevelopmentPlan.Proposal
	}
	if err := ValidateGranularity(p); err != nil {
		return nil, err
	}
	checks := p
	if s.mergeMaster() {
		checks = masterDevelopmentPlan(p)
	}
	if err := ValidateProjectChecks(checks); err != nil {
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
	if receipt := s.CurrentIntakeConfirmation(); receipt != nil && receipt.SolutionRevision != "" {
		solutionRevision = receipt.SolutionRevision
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

// splitPlanPreview projects the proposed binding without changing accepted state.
func (s *State) splitPlanPreview(cd *compiler.CompiledDefinition, topology TopologyDecision) (*DevelopmentPlanPreview, error) {
	if topology.Decision != topologySplit {
		return nil, validationError("prospective plan requires split topology")
	}
	binding, err := s.topologyBinding(&topology)
	if err != nil {
		return nil, err
	}
	projected := *s
	projected.Topology = binding
	projected.RouteMode = routeModeMerge
	_, proposal := projected.readinessProposal()
	if err := projected.validateSplitPlan(proposal); err != nil {
		return nil, err
	}
	return projected.developmentPlanPreview(cd, RouteDecision{Mode: routeModeMerge, Gates: mergeRouteGates()})
}

// SliceDevelopmentPlan projects only the original confirmed master's ownership.
// Route confirmation remains the child's execution-contract authority.
func (s *State) SliceDevelopmentPlan(sliceID string) (*GranularityReview, error) {
	_, proposal, _ := s.confirmedDevelopmentPlan()
	return s.sliceDevelopmentPlan(proposal, sliceID)
}

func (s *State) sliceDevelopmentPlan(proposal *GranularityReview, owner string) (*GranularityReview, error) {
	if s.Topology == nil || !slices.Contains(s.Topology.Slices, owner) || proposal == nil {
		return nil, validationError("slice development plan needs its confirmed topology and master proposal")
	}
	projected := *proposal
	projected.Batches = nil
	selected := map[string]bool{}
	for _, batch := range proposal.Batches {
		if batch.Owner == owner {
			projected.Batches = append(projected.Batches, batch)
			selected[batch.ID] = true
		}
	}
	projected.CheckpointGroups = nil
	for _, group := range proposal.CheckpointGroups {
		var members []string
		for _, id := range group {
			if selected[id] {
				members = append(members, id)
			}
		}
		if len(members) != 0 {
			projected.CheckpointGroups = append(projected.CheckpointGroups, members)
		}
	}
	if projected.VerificationTier == "CHECKPOINT" && len(projected.CheckpointGroups) < 2 {
		projected.VerificationTier = "FINAL"
		projected.CheckpointGroups = nil
		projected.CheckpointReason = ""
	}
	if err := ValidateGranularity(&projected); err != nil {
		return nil, err
	}
	if err := ValidateProjectChecks(&projected); err != nil {
		return nil, err
	}
	return &projected, nil
}

func (s *State) validateSplitPlan(proposal *GranularityReview) error {
	if proposal == nil {
		return validationError("split topology requires the readiness development plan")
	}
	if err := ValidateGranularity(proposal); err != nil {
		return err
	}
	for _, batch := range proposal.Batches {
		if batch.Owner != masterMergeOwner && !slices.Contains(s.Topology.Slices, batch.Owner) {
			return validationError("split batch requires an explicit confirmed owner: " + batch.ID)
		}
	}
	for _, owner := range s.Topology.Slices {
		if _, err := s.sliceDevelopmentPlan(proposal, owner); err != nil {
			return validationError(fmt.Sprintf("slice %s: %v", owner, err))
		}
	}
	return ValidateProjectChecks(masterDevelopmentPlan(proposal))
}

func masterDevelopmentPlan(proposal *GranularityReview) *GranularityReview {
	if proposal == nil || !slices.ContainsFunc(proposal.Batches, func(batch DevelopmentBatch) bool { return batch.Owner != "" }) {
		return proposal
	}
	projected := *proposal
	projected.Batches = nil
	for _, batch := range proposal.Batches {
		if batch.Owner == masterMergeOwner {
			projected.Batches = append(projected.Batches, batch)
		}
	}
	return &projected
}

func (e *Engine) DevelopmentPlan(route RouteDecision, topology ...TopologyDecision) (*DevelopmentPlanPreview, error) {
	_, s, err := e.load()
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, &RejectedError{Code: CodeNotInitialized}
	}
	if len(topology) > 1 {
		return nil, validationError("plan preview has more than one prospective topology")
	}
	if len(topology) == 1 {
		return s.splitPlanPreview(e.cfg.Definition, topology[0])
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
		if decisionPlanConfirmation(d) == nil || d.Revision < latest {
			continue
		}
		source := s.Results[decisionPlanConfirmation(d).ReviewActionID]
		if source.Review == nil {
			continue
		}
		request, p, revision, latest = id, source.Review.GranularityReview, source.Review.RequirementRevision, d.Revision
	}
	// Identical confirmations preserve the original execution contract reference.
	for id, d := range s.Decisions {
		if d.Revision >= latest || decisionPlanConfirmation(d) == nil {
			continue
		}
		source := s.Results[decisionPlanConfirmation(d).ReviewActionID]
		if source.Review != nil && planContractDigest(source.Review.GranularityReview) == planContractDigest(p) {
			request, revision, latest = id, source.Review.RequirementRevision, d.Revision
		}
	}
	return request, p, revision
}

// confirmedSlicePlan 取 child 已确认的片内计划：最新路线决定的内联提案
// 为准；同摘要的更早决定保留原引用（与普通 run 的执行契约引用保留形态
// 一致）。返回的计划绑定 revision＝原确认决定时点的权威需求 revision（账
// 本中代数不晚于该决定的最近本地确认；起点为继承绑定）。
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
	return request, p, s.requirementRevisionAt(latest)
}

func decisionPlanConfirmation(d RecordedDecision) *DevelopmentPlanConfirmation {
	if d.Review == nil {
		return nil
	}
	if d.Control == ControlSelectRoute && d.Review.Route != nil {
		return d.Review.Route.DevelopmentPlan
	}
	if d.Control == ControlConfirmTopology && d.Review.Topology != nil && d.Review.Topology.Decision == topologySplit {
		return d.Review.Topology.DevelopmentPlan
	}
	return nil
}

// decisionPlanProposal reads the plan source from its explicit topology or route confirmation.
func (s *State) decisionPlanProposal(d RecordedDecision) *GranularityReview {
	confirmation := decisionPlanConfirmation(d)
	if confirmation == nil {
		return nil
	}
	if s.SliceChild != nil {
		return confirmation.Proposal
	}
	source := s.Results[confirmation.ReviewActionID]
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

func (s *State) developmentBatchRequired(batch DevelopmentBatch) bool {
	return !s.mergeMaster() || batch.Owner == masterMergeOwner && slices.ContainsFunc(batch.Checks, func(check DevelopmentCheck) bool { return check.Command == "" })
}

func (s *State) plannedBatchSource(task runtime.TaskKey) (DevelopmentBatch, bool) {
	original := s.sourceValidationTask(task)
	for _, d := range s.Decisions {
		p := s.decisionPlanProposal(d)
		if p == nil {
			continue
		}
		for _, b := range p.Batches {
			if original.Scope == planTaskScope(p, b.ID) {
				return b, true
			}
		}
	}
	return DevelopmentBatch{}, false
}
func (s *State) currentPlanTask(cd *compiler.CompiledDefinition, p *GranularityReview, id string) runtime.TaskKey {
	return s.currentPlanTaskFrom(planTask(cd, p, id), p, id)
}
func (s *State) currentPlanTaskFrom(task runtime.TaskKey, p *GranularityReview, id string) runtime.TaskKey {
	task.Scope = planTaskScope(p, id)
	var batch DevelopmentBatch
	for _, b := range p.Batches {
		if b.ID == id {
			batch = b
		}
	}
	for _, entry := range s.RequirementRevisions {
		for i, c := range entry.WorkDecisions {
			prior, known := s.plannedBatchSource(c.Work.Task)
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
func (s *State) developmentEditResult(task runtime.TaskKey) *WorkerResult {
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
func (s *State) developmentResultRecord(task runtime.TaskKey) *WorkerResult {
	record := s.developmentEditResult(task)
	if record == nil || !s.developmentChecksPassed(*record) {
		return nil
	}
	return record
}
func (s *State) developmentResult(task runtime.TaskKey) *DevelopmentEvidence {
	record := s.developmentResultRecord(task)
	if record == nil {
		return nil
	}
	return record.Validation.Development
}

// bindDevelopmentChecks fills only the confirmed plan's missing calls in order.
// The accepted development result owns these values; the plan owns their duties.
func bindDevelopmentChecks(batch DevelopmentBatch, commands []string) ([]DevelopmentCheck, error) {
	checks := slices.Clone(batch.Checks)
	count := 0
	for i := range checks {
		if checks[i].Command != "" {
			continue
		}
		if count >= len(commands) || strings.TrimSpace(commands[count]) == "" {
			return nil, validationError("development must supply every missing verification call")
		}
		checks[i].Command, checks[i].Goal = commands[count], ""
		count++
	}
	if count != len(commands) {
		return nil, validationError("development cannot replace an existing verification call")
	}
	return checks, nil
}

func validateDevelopmentCompletion(batch DevelopmentBatch, e *DevelopmentEvidence) error {
	if _, err := bindDevelopmentChecks(batch, e.Commands); err != nil {
		return err
	}
	if e.RemainingWork != "" {
		return validationError("completion cannot retain unfinished work")
	}
	if e.Completion == nil || strings.TrimSpace(e.Completion.Handoff) == "" {
		return validationError("batch completion requires DoD and handoff evidence")
	}
	if len(e.Completion.DoD) != len(batch.DoD) {
		return validationError("batch completion must cover exactly its confirmed DoD")
	}
	for _, d := range batch.DoD {
		if strings.TrimSpace(e.Completion.DoD[d.ID]) == "" {
			return validationError("missing batch DoD evidence: " + d.ID)
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
		if s.developmentBatchRequired(b) && s.currentPlanTask(cd, p, b.ID) == task {
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
			if !s.developmentBatchRequired(b) {
				continue
			}
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
	if result := s.developmentEditResult(task); result != nil && (result.Validation.Status == "CONTINUE" || s.developmentChecksFailed(*result)) {
		input.Result = result
		for _, check := range s.editChecks(*result) {
			if evidence, ok := s.checkReceipt(result.ActionID, check.ID); ok {
				input.Checks = append(input.Checks, *evidence)
			}
		}
	}
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
	if s.mergeMaster() || p.VerificationTier != "CHECKPOINT" {
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
				if w.Status == "COMPLETE" && sameTaskSet(w.CheckpointTasks, tasks) && s.validationEvidenceComplete(&w) {
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
	if s.mergeMaster() {
		_, _, _, complete := s.integratedSources()
		if !complete {
			return
		}
	}
	_, p, _ := s.confirmedDevelopmentPlan()
	if p == nil || !s.mergeMaster() && !s.developmentPlanCurrent(p) {
		return
	}
	allowed, _ := s.planCheckpoint(cd, p)
	for _, b := range p.Batches {
		if !s.developmentBatchRequired(b) {
			continue
		}
		key := s.currentPlanTask(cd, p, b.ID)
		if s.developmentEditResult(key) != nil {
			continue
		}
		blocked := !slices.Contains(allowed, key) || !s.workspaceReady(key, cd)
		group := runtime.Batch{ID: runtime.BatchID(b.ID)}
		for _, dep := range b.DependsOn {
			if s.mergeMaster() && !slices.ContainsFunc(p.Batches, func(batch DevelopmentBatch) bool { return batch.ID == dep && s.developmentBatchRequired(batch) }) {
				continue
			}
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
	if s.mergeMaster() {
		sources, paths, base, complete := s.integratedSources()
		if !complete {
			return nil, nil, "", false
		}
		for _, task := range s.planTasks(cd) {
			result := s.developmentResultRecord(task)
			if result == nil {
				return nil, nil, "", false
			}
			source, ok := s.editSourceProof(result.ActionID)
			if !ok {
				return nil, nil, "", false
			}
			sources = append(sources, task)
			paths = append(paths, result.Validation.Development.Paths...)
			base = source.Identity
		}
		slices.Sort(paths)
		return sources, slices.Compact(paths), base, true
	}
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
	for _, work := range s.repairWork() {
		if work.result == nil && len(s.repairMembers(work.wave)) == 1 && s.requirementRevisionForTask(work.task) != "" {
			continue
		}
		if work.result == nil || !s.repairGatesComplete(work) {
			return nil, nil, "", false
		}
		if s.NativeRoot != "" {
			_, proof, ok := s.repairFinalSource(work)
			if !ok {
				return nil, nil, "", false
			}
			base = proof.Identity
		} else {
			base = work.result.Validation.Binding.Candidate
		}
		sources = append(sources, work.task)
		paths = append(paths, work.result.Validation.Development.Paths...)
	}
	for i := range s.Validation.Waves {
		if work := s.repairVerificationWork(&s.Validation.Waves[i], cd); work != nil && work.result == nil {
			return nil, nil, "", false
		}
	}

	slices.Sort(paths)
	paths = slices.Compact(paths)
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

func ValidateProjectChecks(p *GranularityReview) error {
	if p == nil || len(p.Batches) == 0 {
		return validationError("project verification contract is missing " + CheckFullBuild)
	}
	for _, batch := range p.Batches {
		purposes := map[string]bool{}
		for _, check := range batch.Checks {
			purposes[check.Purpose] = true
		}
		for _, purpose := range []string{CheckFullBuild, CheckFullTests} {
			if !purposes[purpose] {
				return validationError("batch " + batch.ID + " verification contract is missing " + purpose)
			}
		}
	}
	return nil
}

func (s *State) plannedDevelopmentChecks(plan *GranularityReview, batch DevelopmentBatch) []DevelopmentCheck {
	for _, binding := range s.DispatchBindings {
		if binding.BatchID != batch.ID || s.currentPlanTaskFrom(binding.Task, plan, batch.ID) != binding.Task {
			continue
		}
		result := s.developmentEditResult(binding.Task)
		if result == nil || !s.editResultCurrent(*result) {
			continue
		}
		if result.Validation.Status == "CONTINUE" && len(result.Validation.Development.Commands) == 0 {
			return batch.Checks
		}
		checks, err := bindDevelopmentChecks(batch, result.Validation.Development.Commands)
		if err != nil {
			return nil
		}
		return checks
	}
	return batch.Checks
}

func (s *State) fullProjectChecks() []DevelopmentCheck {
	_, confirmed, _ := s.confirmedDevelopmentPlan()
	p := confirmed
	if s.mergeMaster() {
		p = masterDevelopmentPlan(p)
	}
	if p == nil {
		return nil
	}
	var checks []DevelopmentCheck
	for _, batch := range p.Batches {
		batch.Checks = s.plannedDevelopmentChecks(confirmed, batch)
		for _, check := range batch.Checks {
			if check.Purpose != CheckFullBuild && check.Purpose != CheckFullTests {
				continue
			}
			if slices.ContainsFunc(checks, func(existing DevelopmentCheck) bool {
				return existing.Purpose == check.Purpose && existing.Command == check.Command
			}) {
				continue
			}
			check.ID = "project:" + strings.TrimPrefix(digestOfCanonical(struct{ Batch, Check string }{batch.ID, check.ID}), "sha256:")
			checks = append(checks, check)
		}
	}
	return checks
}

func validateDevelopmentContinuation(e *DevelopmentEvidence) error {
	if e == nil || e.Paths == nil || strings.TrimSpace(e.Summary) == "" || strings.TrimSpace(e.RemainingWork) == "" || e.Completion != nil || len(e.CaseRuns) != 0 {
		return validationError("development continuation requires actual progress and remaining work without completion claims")
	}
	return nil
}
