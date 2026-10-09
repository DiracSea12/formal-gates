package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
)

// NativeResource is the finite registry for this run's native worktrees and refs.
// Projections may retire a role while its resource remains owed to cleanup.
type NativeResource struct {
	ID           string              `json:"id"`
	Handle       vcs.WorkspaceHandle `json:"handle,omitempty"`
	Owner        string              `json:"owner"`
	Role         string              `json:"role"`
	Path         string              `json:"path"`
	Ref          string              `json:"ref"`
	Source       string              `json:"source"`
	Identity     string              `json:"identity,omitempty"`
	Status       string              `json:"status"`
	MirrorDigest string              `json:"mirrorDigest,omitempty"`
}
type WorkspaceParameters struct {
	Task                *runtime.TaskKey             `json:"task,omitempty"`
	RequirementRevision string                       `json:"requirementRevision"`
	Generation          uint64                       `json:"generation"`
	RouteScope          string                       `json:"routeScope"`
	Resource            NativeResource               `json:"resource"`
	Manifest            *coverage.AcceptanceManifest `json:"manifest,omitempty"`
}
type WorkspaceEvidence struct {
	ParametersDigest string         `json:"parametersDigest"`
	Resource         NativeResource `json:"resource"`
	Digest           string         `json:"digest"`
	SourceDigest     string         `json:"sourceDigest,omitempty"`
}
type CleanupParameters struct {
	Resources  []NativeResource   `json:"resources"`
	Checkpoint *CheckpointCleanup `json:"checkpoint,omitempty"`
}

// CheckpointCleanup carries the delivered master authority in the child's
// original cleanup intent; the frozen slice receipt remains unchanged.
type CheckpointCleanup struct {
	MasterRunID   string                    `json:"masterRunId"`
	Promotion     CandidatePromotionReceipt `json:"promotion"`
	ReceiptDigest string                    `json:"receiptDigest"`
}
type WorkspaceCleanupReceipt struct {
	ParametersDigest string           `json:"parametersDigest"`
	Resources        []NativeResource `json:"resources"`
}

// ResourceObservation binds an abort withdrawal to the original native root,
// ref and actual prefix; it records no completion or cancellation receipt.
type ResourceObservation struct {
	Merge            *IntegrationEvidence  `json:"merge,omitempty"`
	ParametersDigest string                `json:"parametersDigest"`
	Resource         NativeResource        `json:"resource"`
	WorktreeExists   bool                  `json:"worktreeExists"`
	RefIdentity      string                `json:"refIdentity"`
	Prefix           *PromotionObservation `json:"prefix,omitempty"`
}

func (s *State) validateResourceObservation(intent HostActionIntent, o ResourceObservation) error {
	if intent.Adapter == nil || o.ParametersDigest != intent.PayloadDigest || o.Resource.Owner != s.RunID || s.NativeResources[o.Resource.ID] != o.Resource {
		return validationError("withdrawal requires its original registered resource")
	}
	raw, _ := json.Marshal(intent.Adapter.Params)
	refPrefix := o.Resource.Source
	switch intent.Adapter.Operation {
	case "op.workspace":
		var p WorkspaceParameters
		if json.Unmarshal(raw, &p) != nil || p.Resource != o.Resource {
			return validationError("workspace withdrawal differs from original inputs")
		}
	case "op.check":
		p, err := checkParameters(intent)
		if err != nil || p.WorkspaceID != o.Resource.ID {
			return validationError("check withdrawal differs from its original resource")
		}
	case "op.candidate":
		p, err := candidateParameters(intent)
		if err != nil || p.Promotion != nil || p.WorkspaceID != o.Resource.ID {
			return validationError("candidate withdrawal differs from original inputs")
		}
		if p.Integration != nil && o.WorktreeExists && (o.Merge == nil || o.Merge.Incoming != p.Integration.Incoming || o.Merge.Prefix != p.Base || o.Merge.MergeHead != "" && o.Merge.MergeHead != p.Integration.Incoming) {
			return validationError("integration withdrawal differs from the original native prefix")
		}

		if p.Integration != nil {
			refPrefix = p.Base
		}
	default:
		return validationError("resource withdrawal is not a workspace or candidate")
	}
	if o.WorktreeExists != (o.Prefix != nil) || o.Prefix != nil && (o.Prefix.Head == "" || o.Prefix.Tree == "" || o.Prefix.IndexTree == "" && o.Prefix.PendingFingerprint == "" && o.Merge == nil) {
		return validationError("resource withdrawal has no exact observed prefix")
	}
	if o.RefIdentity != "" && o.RefIdentity != o.Resource.Source && o.RefIdentity != refPrefix && (o.Prefix == nil || o.RefIdentity != o.Prefix.Head) {
		return validationError("resource ref conflicts with its original source or prefix")
	}
	return nil
}

func (s *State) resource(role, source, scope string) NativeResource {
	id := strings.TrimPrefix(digestOfCanonical(struct{ Role, Source, Scope string }{role, source, scope}), "sha256:")
	resource := NativeResource{ID: id, Owner: s.RunID, Role: role, Path: filepath.Join(s.NativeRoot, ".gates", "qa-isolation", s.RunID, id), Ref: "refs/formal-gates/" + s.RunID + "/" + id, Source: source, Status: "PLANNED"}
	if s.NativeVCS() != "git" {
		resource.Ref = ""
		resource.Handle = vcs.PlanWorkspace(s.NativeVCS(), source, s.RunID, id)
	}
	return resource
}
func (s *State) taskResource(key runtime.TaskKey, cd *compiler.CompiledDefinition) NativeResource {
	if s.NativeRoot == "" || s.Validation == nil {
		return NativeResource{}
	}
	for _, b := range s.DispatchBindings {
		if b.Task == key && (b.Generation == s.Validation.Generation && b.RequirementRevision == s.Validation.RequirementRevision || s.validationWorkCarryAuthorized("", b.Task)) && b.WorkspaceID != "" && s.resourceUsable(b.WorkspaceID) {
			return s.NativeResources[b.WorkspaceID]
		}
	}
	role := s.validationRole(key, cd)
	source := s.BaseSnapshot
	scope := s.Validation.RequirementRevision
	switch role {
	case "engine.development.batch":
		source = s.AcceptedNativeSnapshot()
		request, _, _ := s.confirmedDevelopmentPlan()
		scope = request + ":" + s.Validation.RequirementRevision + ":" + strconv.FormatUint(s.Validation.Generation, 10)
		// Spike conclusions are dependencies; their edits remain outside delivery.
		if _, _, batch, ok := s.developmentBatch(key, cd); ok && batch.Spike {
			scope += ":" + key.String()
		}
		if s.Validation.Candidate != nil {
			source = s.Validation.Candidate.Identity
		} else if s.Validation.DevelopmentSnapshot != "" {
			source = s.Validation.DevelopmentSnapshot
		} else if w := s.currentValidationWave(); w != nil {
			source = w.Candidate.Identity
		}
		role = "development"
	case "engine.merge.resolve":
		if intent, ok := s.hostIntent(key.Scope); ok {
			if p, err := candidateParameters(intent); err == nil {
				return s.NativeResources[p.WorkspaceID]
			}
		}
		return NativeResource{}
	case "engine.repair.verify":
		if context, ok := s.repairComparisonContext(key, cd); ok {
			return context.resource
		}
		return NativeResource{}
	case "engine.repair":
		for _, work := range s.repairWork() {
			if work.task == key {
				source = work.wave.Candidate.Identity
				if work.previous != nil {
					_, prior, ok := s.repairSource(work.previous.Validation.Binding.Task)
					if ok {
						source = prior.Identity
					}
				}
				if len(s.repairMembers(work.wave)) == 1 && s.requirementRevisionForTask(work.task) != "" && s.Validation.DevelopmentSnapshot != "" {
					source = s.Validation.DevelopmentSnapshot
				}
				scope = work.wave.ID + ":" + key.String()
			}
		}
		role = "development"
	case "engine.qa.design":
		if s.validationMode(key) == coverage.ReviewMerge {
			source = s.AcceptedNativeSnapshot()
		}
		if s.validationMode(key) == coverage.ReviewWhitebox {
			role = "whitebox-authoring"
			source = s.Validation.DevelopmentSnapshot
			scope = key.Scope + ":" + strconv.FormatUint(s.Validation.Generation, 10)
		} else {
			scope += ":" + strconv.FormatUint(s.Validation.Generation, 10)
			role = strings.ToLower(string(s.validationMode(key))) + "-design"
		}
	case "engine.qa.review.rationale":
		set := s.Validation.Sets[s.validationMode(key)]
		if set == nil {
			return NativeResource{}
		}
		return s.taskResource(set.DesignTask, cd)
	case "engine.validation.observation":
		if input := s.validationObservationInput(); input != nil {
			ids := []string{}
			for id, r := range s.NativeResources {
				if r.Role == "candidate" && r.Identity == input.Candidate.Identity && r.Status == "READY" && s.resourceUsable(id) {
					ids = append(ids, id)
				}
			}
			slices.Sort(ids)
			if len(ids) > 0 {
				return s.NativeResources[ids[0]]
			}
			source = input.Candidate.Identity
			scope = input.WaveID
			role = "candidate"
		} else {
			return NativeResource{}
		}
	case "engine.qa.execution", "engine.gate":
		if _, target := s.repairGateTarget(key); target != nil {
			if binding, ok := s.repairGateBinding(key, s.repairWork()); ok {
				source, scope, role = binding.Candidate, key.String(), "gate"
				break
			}
		}
		wave := s.currentValidationWave()
		if wave == nil {
			return NativeResource{}
		}
		source = wave.Candidate.Identity
		scope = wave.ID
		if role == "engine.qa.execution" {
			role = strings.ToLower(string(s.validationMode(key))) + "-execution"
		} else {
			role = "gate"
		}
	default:
		return NativeResource{}
	}
	if source == "" {
		return NativeResource{}
	}
	resource := s.resource(role, source, scope)
	if existing, ok := s.NativeResources[resource.ID]; ok && existing.Status != "CLEANED" && !s.ResourceCleanupPending(resource.ID) {
		return existing
	}
	return resource
}
func (s *State) workspaceReady(key runtime.TaskKey, cd *compiler.CompiledDefinition) bool {
	if s.NativeRoot == "" {
		return true
	}
	r := s.taskResource(key, cd)
	return s.resourceUsable(r.ID) && r.Status == "READY" && (s.validationRole(key, cd) != "engine.qa.review.rationale" || s.validationMode(key) != coverage.ReviewBlackbox || r.MirrorDigest == BlackboxCasesDigest(s.RunID, s.Validation.Sets[coverage.ReviewBlackbox].Manifest))
}
func (s *State) candidateResource(p CandidateParameters, cd *compiler.CompiledDefinition) NativeResource {
	if p.Stage == "CHECK_SOURCE" {
		return s.NativeResources[p.WorkspaceID]
	}
	switch p.Stage {
	case "MERGE_START", "MERGE_FINISH":
		if p.Integration.RepairTask != nil {
			if p.Integration.TargetTask != nil {
				return s.taskResource(*p.Integration.TargetTask, cd)
			}
			if member := s.repairMember(*p.Integration.RepairTask); member != nil {
				return s.repairAssemblyResource(member.wave)
			}
		}
		return s.integrationResource()
	case "DEVELOPMENT":
		if p.RepairTask != nil {
			return s.taskResource(*p.RepairTask, cd)
		}
		for _, work := range s.repairWork() {
			if slices.Contains(p.SourceTasks, work.task) {
				if len(s.repairMembers(work.wave)) == 1 {
					return s.taskResource(work.task, cd)
				}
				return s.repairAssemblyResource(work.wave)
			}
		}
		if s.mergeMaster() && len(p.SourceTasks) == 0 {
			return s.integrationResource()
		}
		var chosen NativeResource
		var latest uint64
		for _, task := range p.SourceTasks {
			if result := s.developmentResultRecord(task); result != nil {
				issued := s.issuedRevisionOf(result.ActionID)
				if issued >= latest {
					latest = issued
					chosen = s.NativeResources[result.Validation.Binding.WorkspaceID]
				}
			}
		}
		return chosen
	case "AUTHORING":
		return s.taskResource(pipelineTask(cd, "engine.qa.design", s.nextDesignScope(coverage.ReviewWhitebox, cd)), cd)
	case "QA_ARTIFACT":
		return s.taskResource(p.DesignTasks[coverage.ReviewWhitebox], cd)
	case "REVIEW_READY":
		for _, intent := range s.PendingHostActions {
			original, err := candidateParameters(intent)
			if err == nil && candidateCorrelation(original) == candidateCorrelation(p) && s.candidateContextCurrent(original) && s.resourceUsable(original.WorkspaceID) {
				return s.NativeResources[original.WorkspaceID]
			}
		}
		return s.resource("candidate", p.Base, candidateCorrelation(p)+":"+strconv.FormatUint(s.Validation.Generation, 10))
	}
	return NativeResource{}
}
func (e *Engine) ensureWorkspace(s *State, r NativeResource, nextRevision uint64, tasks ...runtime.TaskKey) (bool, error) {
	if r.ID == "" || s.NativeResources[r.ID].Status == "CLEANED" || s.ResourceCleanupPending(r.ID) {
		return false, nil
	}
	params := WorkspaceParameters{Resource: r, RequirementRevision: s.Validation.RequirementRevision, Generation: s.Validation.Generation, RouteScope: s.validationRouteDigest()}
	if len(tasks) != 0 {
		task := tasks[0]
		params.Task = &task
	}
	if r.Role == "blackbox-design" && s.Validation != nil {
		if set := s.Validation.Sets[coverage.ReviewBlackbox]; set != nil {
			manifest := set.Manifest
			params.Manifest = &manifest
		}
	}
	if existing, ok := s.NativeResources[r.ID]; ok && existing.Status == "READY" && (params.Manifest == nil || existing.MirrorDigest == BlackboxCasesDigest(s.RunID, *params.Manifest)) {
		return false, nil
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.workspace" && s.candidateInputsCurrent(intent, e.cfg.Definition) {
			var original WorkspaceParameters
			raw, _ := json.Marshal(intent.Adapter.Params)
			if err := json.Unmarshal(raw, &original); err != nil {
				return false, err
			}
			if original.Resource.ID == r.ID {
				return false, nil
			}
		}
	}
	if s.NativeResources == nil {
		s.NativeResources = map[string]NativeResource{}
	}
	s.NativeResources[r.ID] = r
	intent, err := e.prepareAdapterIntent(s, nextRevision, "op.workspace", params, "")
	if err != nil {
		return false, err
	}
	s.PendingHostActions[intent.ActionID] = intent
	return true, nil
}
func (e *Engine) advanceWorkspaces(s *State, nextRevision uint64) (bool, error) {
	if s.NativeRoot == "" || s.abortRequested() || s.requirementChangePending() {
		return false, nil
	}
	view := s.decisionView(e.cfg.Definition)
	var keys []runtime.TaskKey
	for _, tasks := range view.RequiredTasks {
		for _, task := range tasks {
			keys = append(keys, task.Task)
		}
	}
	slices.SortFunc(keys, func(a, b runtime.TaskKey) int { return strings.Compare(a.String(), b.String()) })
	for _, key := range keys {
		if s.TaskStatusOf(key) == runtime.TaskTerminal {
			continue
		}
		r := s.taskResource(key, e.cfg.Definition)
		if r.ID != "" && !s.workspaceReady(key, e.cfg.Definition) {
			return e.ensureWorkspace(s, r, nextRevision, key)
		}
	}
	return false, nil
}
func decodeAdapterValues(receipt HostActionReceipt, out any) error {
	if receipt.AdapterEvidence == nil {
		return validationError("native operation requires typed evidence")
	}
	data, err := json.Marshal(receipt.AdapterEvidence.Values)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
func validateResourceReceipt(intent HostActionIntent, receipt HostActionReceipt) error {
	if intent.Adapter == nil {
		return nil
	}
	raw, _ := json.Marshal(intent.Adapter.Params)
	switch intent.Adapter.Operation {
	case "op.workspace":
		var p WorkspaceParameters
		var v WorkspaceEvidence
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if err := decodeAdapterValues(receipt, &v); err != nil {
			return err
		}
		expected := p.Resource
		expected.Identity = expected.Source
		if p.Resource.Handle.Source != "" {
			expected.Identity = v.Resource.Identity
			if expected.Identity == "" || v.SourceDigest == "" || v.SourceDigest != v.Digest {
				return validationError("native workspace must preserve its source values")
			}
		}
		expected.Status = "READY"
		if p.Manifest != nil {
			expected.MirrorDigest = BlackboxCasesDigest(expected.Owner, *p.Manifest)
		}
		if v.ParametersDigest != intent.PayloadDigest || v.Resource != expected || v.Digest == "" {
			return validationError("workspace receipt does not prove its exact registered root and source")
		}
	case "op.cleanup":
		var p CleanupParameters
		var v WorkspaceCleanupReceipt
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if err := decodeAdapterValues(receipt, &v); err != nil {
			return err
		}
		if v.ParametersDigest != intent.PayloadDigest || len(v.Resources) != len(p.Resources) {
			return validationError("cleanup receipt does not account for every resource")
		}
		for i, r := range p.Resources {
			r.Status = "CLEANED"
			if r != v.Resources[i] {
				return validationError("cleanup receipt resource differs from its intent")
			}
		}
	}
	return nil
}
func (e *Engine) applyResourceReceipt(s *State, intent HostActionIntent, receipt HostActionReceipt) error {
	if err := validateResourceReceipt(intent, receipt); err != nil {
		return err
	}
	switch intent.Adapter.Operation {
	case "op.workspace":
		var v WorkspaceEvidence
		_ = decodeAdapterValues(receipt, &v)
		if v.Resource.Role == "child-target" && (s.ChildCreation == nil || v.Digest != s.ChildCreation.Digest) {
			return validationError("child workspace receipt content differs from the creation source")
		}
		applicable := s.nativeEffectApplicable(intent, e.cfg.Definition)
		project := applicable && !s.abortRequested() && v.Resource.Role == "blackbox-design" && s.candidateInputsCurrent(intent, e.cfg.Definition)
		if applicable {
			s.NativeResources[v.Resource.ID] = v.Resource
		}
		if project {
			s.QAWorktree = v.Resource.Path
		}
	case "op.cleanup":
		var v WorkspaceCleanupReceipt
		_ = decodeAdapterValues(receipt, &v)
		for _, r := range v.Resources {
			if old, ok := s.NativeResources[r.ID]; ok && old.Owner == r.Owner && old.Path == r.Path && old.Ref == r.Ref {
				s.NativeResources[r.ID] = r
			}
		}
	}
	return nil
}
func (s *State) ResourcesCleaned() bool {
	for _, r := range s.NativeResources {
		if r.Status != "CLEANED" {
			return false
		}
	}
	return true
}

// cleanupStarted retains the authority checked when the original cleanup intent
// was issued. Current controls own permission to execute, not that durable fact.
// checkpointOnly binds child eligibility to its current checkpoint receipt.
func (s *State) cleanupStarted(checkpointOnly bool) bool {
	valid := func(intent HostActionIntent, cleaned bool) bool {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.cleanup" {
			return false
		}
		var p CleanupParameters
		raw, _ := json.Marshal(intent.Adapter.Params)
		if json.Unmarshal(raw, &p) != nil || len(p.Resources) == 0 || checkpointOnly && !s.checkpointCleanupCurrent(p.Checkpoint) {
			return false
		}
		for _, r := range p.Resources {
			if cleaned {
				r.Status = "CLEANED"
			}
			if r.Owner != s.RunID || s.NativeResources[r.ID] != r {
				return false
			}
		}
		return true
	}
	for _, intent := range s.PendingHostActions {
		if valid(intent, false) {
			return true
		}
	}
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.cleanup" || receipt.Status != HostActionStatusExecuted {
			continue
		}
		intent, exists := s.hostIntent(receipt.ActionID)
		if exists && validateResourceReceipt(intent, receipt) == nil && valid(intent, true) {
			return true
		}
	}
	return false
}

func (s *State) checkpointCleanupCurrent(p *CheckpointCleanup) bool {
	if p == nil || s.SliceChild == nil || s.Phase != runtime.PhaseSliceReady || s.SliceReceipt == nil || s.abortRequested() {
		return false
	}
	promotion := p.Promotion
	return p.MasterRunID == s.SliceChild.MasterRunID && p.ReceiptDigest == s.SliceReceipt.Digest && p.ReceiptDigest == s.SliceReceipt.Receipt.CanonicalDigest() && promotion.OperationID != "" && promotion.Provider == s.NativeVCS() && promotion.Final != "" && promotion.FinalDigest == promotion.SourceDigest
}

func (s *State) cleanupAuthorized(p CleanupParameters, cd *compiler.CompiledDefinition, parents ...*State) bool {
	if p.Checkpoint == nil {
		return s.CleanupEligible(cd)
	}
	if !s.cleanupQuiescent() || !s.checkpointCleanupCurrent(p.Checkpoint) {
		return false
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.cleanup" {
			var original CleanupParameters
			raw, _ := json.Marshal(intent.Adapter.Params)
			return json.Unmarshal(raw, &original) == nil && digestOfCanonical(original) == digestOfCanonical(p)
		}
	}
	if len(parents) != 1 || parents[0] == nil {
		return false
	}
	parent := parents[0]
	if parent.RunID != p.Checkpoint.MasterRunID || !parent.validationDeliveryComplete(cd) || parent.CandidatePromotion == nil || digestOfCanonical(parent.CandidatePromotion) != digestOfCanonical(p.Checkpoint.Promotion) {
		return false
	}
	original, exists := parent.hostIntent(parent.CandidatePromotion.OperationID)
	params, err := candidateParameters(original)
	receipt, accepted := parent.SliceReceipts[s.RunID]
	return exists && err == nil && parent.promotionAuthorized(params) && accepted && receipt.Digest == p.Checkpoint.ReceiptDigest && parent.SliceChildren[s.SliceChild.SliceID] == s.RunID
}

func (s *State) resourceUsable(id string) bool {
	r, exists := s.NativeResources[id]
	return exists && r.Status != "CLEANED" && !s.ResourceCleanupPending(id)
}

func (s *State) workspaceParametersCurrent(p WorkspaceParameters, cd *compiler.CompiledDefinition) bool {
	if p.Resource.Role == "merge" {
		r := s.integrationResource()
		return s.Validation != nil && p.RequirementRevision == s.Validation.RequirementRevision && p.Generation == s.Validation.Generation && p.RouteScope == s.validationRouteDigest() && p.Resource == r && s.resourceUsable(r.ID)
	}
	if p.Resource.Role == "child-target" {
		return s.creationWorkspaceCurrent(p)
	}
	registered := s.NativeResources[p.Resource.ID]
	resourceMatches := registered == p.Resource
	if p.Task != nil {
		resourceMatches = (registered.Status == "PLANNED" || registered.Status == "READY") && sameWorkspaceInput(registered, p.Resource)
	}
	if s.Validation == nil || s.abortRequested() || p.RouteScope != s.validationRouteDigest() || !s.resourceUsable(p.Resource.ID) || !resourceMatches {
		return false
	}
	current := p.RequirementRevision == s.Validation.RequirementRevision && p.Generation == s.Validation.Generation
	carried := p.Task != nil && s.validationWorkCarryAuthorized("", *p.Task)
	if !current && !carried {
		return false
	}
	if p.Task != nil && (s.requirementChangePending() || s.resetPending()) {
		return false
	}
	if p.Resource.Role == "blackbox-design" {
		set := s.Validation.Sets[coverage.ReviewBlackbox]
		if set == nil && p.Manifest != nil || set != nil && (p.Manifest == nil || digestOfCanonical(p.Manifest) != digestOfCanonical(&set.Manifest)) {
			return false
		}
	} else if p.Manifest != nil {
		return false
	}
	if p.Resource.Role == "repair-verification" {
		for _, context := range s.repairComparisonContexts(cd) {
			if context.resource.ID == p.Resource.ID && p.Task != nil && *p.Task == context.binding.Task {
				return true
			}
		}
		return false
	}
	if p.Resource.Role == "check" {
		for _, result := range s.Results {
			if proof, ok := s.editSourceProof(result.ActionID); ok && s.editResultCurrent(result) && s.checkResource(result.ActionID, proof).ID == p.Resource.ID {
				return true
			}
		}
		return false
	}
	view := decision.State{RequiredTasks: map[authoring.StepID][]decision.RequiredTask{}}
	s.projectValidationTasks(&view, cd)
	for _, tasks := range view.RequiredTasks {
		for _, task := range tasks {
			if sameWorkspaceInput(s.taskResource(task.Task, cd), p.Resource) && (p.Task == nil && current || p.Task != nil && task.Task == *p.Task) {
				return true
			}
		}
	}
	if p.Task != nil || !current {
		return false
	}
	candidate, ready, _ := s.currentCandidateParameters(cd)
	if ready {
		if s.candidateResource(candidate, cd).ID == p.Resource.ID {
			return true
		}
		if candidate.Stage == "QA_ARTIFACT" {
			return s.resource("candidate", candidate.Base, "ownership:"+candidateCorrelation(candidate)+":"+strconv.FormatUint(s.Validation.Generation, 10)).ID == p.Resource.ID
		}
	}
	return false
}

// Workspace input identity excludes creation facts owned by resource receipts.
// Manifest equality is checked by workspaceParametersCurrent, not MirrorDigest.
func sameWorkspaceInput(actual, issued NativeResource) bool {
	actual.Status, actual.Identity, actual.MirrorDigest = issued.Status, issued.Identity, issued.MirrorDigest
	return actual == issued
}

func (s *State) cleanupParametersCurrent(p CleanupParameters) bool {
	var expected []NativeResource
	for _, intent := range s.PendingHostActions {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.cleanup" {
			continue
		}
		var original CleanupParameters
		raw, _ := json.Marshal(intent.Adapter.Params)
		if json.Unmarshal(raw, &original) != nil {
			return false
		}
		return digestOfCanonical(p) == digestOfCanonical(original)
	}
	if s.resetPending() {
		d, _ := s.resetDecision()
		preview := s.PendingAsks[d.RequestID].Reset
		if preview == nil {
			return false
		}
		for _, r := range preview.Resources {
			if current := s.NativeResources[r.ID]; current.Status != "CLEANED" {
				expected = append(expected, current)
			}
		}
	} else if s.RebuildingChildren() {
		expected = s.replacedTargetResources()
	} else {
		for _, r := range s.NativeResources {
			if r.Status != "CLEANED" {
				expected = append(expected, r)
			}
		}
	}
	actual := slices.Clone(p.Resources)
	order := func(a, b NativeResource) int { return strings.Compare(a.ID, b.ID) }
	slices.SortFunc(expected, order)
	slices.SortFunc(actual, order)
	return len(expected) != 0 && slices.Equal(actual, expected)
}

// cleanupResourcesQuiescent checks the existing execution bindings for the
// resources being retired. Work on carried resources can continue independently.
func (s *State) cleanupResourcesQuiescent(resources []NativeResource) bool {
	uses := func(id, path string) bool {
		return slices.ContainsFunc(resources, func(r NativeResource) bool { return r.ID == id || path != "" && r.Path == path })
	}
	for id := range s.PendingActions {
		if uses(s.DispatchBindings[id].WorkspaceID, "") {
			return false
		}
	}
	for _, attempt := range s.EndedAttempts {
		if attempt.AwaitingResult && attempt.Work != nil && uses(attempt.Work.WorkspaceID, "") {
			return false
		}
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter == nil {
			continue
		}
		switch intent.Adapter.Operation {
		case "op.workspace":
			var p WorkspaceParameters
			raw, _ := json.Marshal(intent.Adapter.Params)
			if json.Unmarshal(raw, &p) != nil || uses(p.Resource.ID, p.Resource.Path) {
				return false
			}
		case "op.check":
			p, err := checkParameters(intent)
			if err != nil || uses(p.WorkspaceID, "") {
				return false
			}
		case "op.candidate":
			p, err := candidateParameters(intent)
			if err != nil || uses(p.WorkspaceID, "") || p.Promotion != nil && uses(p.Promotion.SourceWorkspace, p.Promotion.TargetRoot) {
				return false
			}
		}
	}
	return true
}

func (s *State) cleanupQuiescent() bool {
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.check" {
			return false
		}
	}
	if s.ChildCreation != nil && len(s.SliceChildren) == 0 && !slices.Contains(s.Completed, authoring.StepID("children.publish")) {
		return false
	}
	if s.RebuildingChildren() {
		return s.cleanupResourcesQuiescent(s.replacedTargetResources())
	}
	if s.resetPending() {
		return len(s.PendingActions) == 0
	}
	if s.pendingControl() || s.nonCleanupNativeDebt() || len(s.PendingActions) != 0 {
		return false
	}
	for _, attempt := range s.EndedAttempts {
		if attempt.AwaitingResult {
			return false
		}
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.cleanup" {
			return false
		}
	}
	return true
}

func (s *State) CleanupEligible(cd *compiler.CompiledDefinition) bool {
	return s.cleanupQuiescent() && (s.resetPending() || s.RebuildingChildren() || s.abortRequested() || s.SliceChild == nil && s.validationDeliveryComplete(cd) || s.SliceChild != nil && s.cleanupStarted(true))
}

func (s *State) ResourceCleanupPending(id string) bool {
	for _, intent := range s.PendingHostActions {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.cleanup" {
			continue
		}
		var p CleanupParameters
		raw, err := json.Marshal(intent.Adapter.Params)
		if err != nil || json.Unmarshal(raw, &p) != nil {
			continue
		}
		for _, r := range p.Resources {
			if r.ID == id {
				return true
			}
		}
	}
	return false
}

// BeginCleanup carries related run states inside the parent's native window:
// a child receives its delivered master; a master receives its cleaned children.
func (e *Engine) BeginCleanup(expectedFingerprint string, related ...*State) error {
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s == nil {
		return validationError("cleanup requires a run")
	}
	if s.ResourcesCleaned() {
		return nil
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.cleanup" {
			return nil
		}
	}
	if s.resetPending() {
		return validationError("reset cleanup is issued from its accepted preview")
	}
	p := CleanupParameters{}
	if s.SliceChild != nil && !s.abortRequested() {
		if len(related) != 1 || related[0] == nil || related[0].CandidatePromotion == nil || s.SliceReceipt == nil {
			return validationError("checkpoint cleanup requires its delivered master")
		}
		p.Checkpoint = &CheckpointCleanup{MasterRunID: related[0].RunID, Promotion: *related[0].CandidatePromotion, ReceiptDigest: s.SliceReceipt.Digest}
	} else if !s.childTargetsCleanable(related) {
		return validationError("cleanup requires released children with cleaned resources")
	}
	for _, r := range s.NativeResources {
		if r.Status != "CLEANED" {
			p.Resources = append(p.Resources, r)
		}
	}
	slices.SortFunc(p.Resources, func(a, b NativeResource) int { return strings.Compare(a.ID, b.ID) })
	intent, err := e.prepareAdapterIntent(s, revision+1, "op.cleanup", p, "", related...)
	if err != nil {
		return err
	}
	if p.Checkpoint == nil && s.Phase != runtime.PhaseFinalizingCleanup {
		if err := s.TransitionPhase(runtime.PhaseFinalizingCleanup); err != nil {
			return err
		}
	}
	s.PendingHostActions[intent.ActionID] = intent
	_, err = e.commit(s, revision, expectedFingerprint)
	return err
}

func (s *State) projectResourceBoundary(view *decision.State, cd *compiler.CompiledDefinition) {
	for _, step := range cd.Steps {
		p, ok := step.Payload.(compiler.CompiledHostActionStep)
		if !ok || (p.Operation != "op.workspace" && p.Operation != "op.cleanup" && p.Operation != "op.check") {
			continue
		}
		view.RequiredTasks[step.Header.ID] = nil
		for _, intent := range s.ExecutableHostActions() {
			if intent.Adapter != nil && intent.Adapter.Operation == p.Operation && intent.Step == step.Header.ID && s.NativeActionCurrent(intent, cd) {
				view.RequiredTasks[step.Header.ID] = append(view.RequiredTasks[step.Header.ID], decision.RequiredTask{Task: runtime.TaskKey{Node: step.Header.NodeID, Step: step.Header.ID, Scope: intent.ActionID}})
			}
		}
	}
}

// BlackboxCasesMarkdown renders the same complete manifest for the isolated
// review mirror and the final tracked artifact.
func BlackboxCasesMarkdown(runID string, m coverage.AcceptanceManifest) string {
	return qaCasesMarkdown(runID, "Blackbox", m)
}

func MergeCasesMarkdown(runID string, m coverage.AcceptanceManifest) string {
	return qaCasesMarkdown(runID, "Merge", m)
}

func qaCasesMarkdown(runID, kind string, m coverage.AcceptanceManifest) string {
	data, _ := json.MarshalIndent(m, "", "  ")
	return "# " + kind + " QA cases (run: " + runID + ")\n\n```json\n" + string(data) + "\n```\n"
}

func (e *Engine) applyNativeReceipt(s *State, intent HostActionIntent, receipt HostActionReceipt) error {
	if intent.Adapter == nil {
		return nil
	}
	if intent.Adapter.Operation == "op.check" {
		return validateCheckReceipt(intent, receipt)
	}
	if intent.Adapter.Operation == "op.workspace" || intent.Adapter.Operation == "op.cleanup" {
		return e.applyResourceReceipt(s, intent, receipt)
	}
	return e.applyCandidateReceipt(s, intent, receipt)
}

func (s *State) RequirementBarrierActive() bool { return s.requirementChangePending() }

func BlackboxCasesDigest(runID string, m coverage.AcceptanceManifest) string {
	sum := sha256.Sum256([]byte(BlackboxCasesMarkdown(runID, m)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// The original workspace receipt owns the created native baseline. The
// resource's current Identity advances with accepted candidate receipts.
func (s *State) ResourceStartIdentity(resource NativeResource) string {
	if s.NativeVCS() == "git" {
		return resource.Source
	}
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.workspace" || receipt.Status != HostActionStatusExecuted {
			continue
		}
		var proof WorkspaceEvidence
		if decodeAdapterValues(receipt, &proof) == nil && proof.Resource.ID == resource.ID {
			return proof.Resource.Identity
		}
	}
	return resource.Source
}
