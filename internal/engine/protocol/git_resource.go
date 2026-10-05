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
)

// GitResource is the finite registry for this run's native worktrees and refs.
// Projections may retire a role while its resource remains owed to cleanup.
type GitResource struct {
	ID           string `json:"id"`
	Owner        string `json:"owner"`
	Role         string `json:"role"`
	Path         string `json:"path"`
	Ref          string `json:"ref"`
	Source       string `json:"source"`
	Identity     string `json:"identity,omitempty"`
	Status       string `json:"status"`
	MirrorDigest string `json:"mirrorDigest,omitempty"`
}
type WorkspaceParameters struct {
	RequirementRevision string                       `json:"requirementRevision"`
	Generation          uint64                       `json:"generation"`
	RouteScope          string                       `json:"routeScope"`
	Resource            GitResource                  `json:"resource"`
	Manifest            *coverage.AcceptanceManifest `json:"manifest,omitempty"`
}
type WorkspaceEvidence struct {
	ParametersDigest string      `json:"parametersDigest"`
	Resource         GitResource `json:"resource"`
	Digest           string      `json:"digest"`
}
type CleanupParameters struct {
	Resources []GitResource `json:"resources"`
}
type WorkspaceCleanupReceipt struct {
	ParametersDigest string        `json:"parametersDigest"`
	Resources        []GitResource `json:"resources"`
}

// ResourceObservation binds an abort withdrawal to the original native root,
// ref and actual prefix; it records no completion or cancellation receipt.
type ResourceObservation struct {
	ParametersDigest string                `json:"parametersDigest"`
	Resource         GitResource           `json:"resource"`
	WorktreeExists   bool                  `json:"worktreeExists"`
	RefIdentity      string                `json:"refIdentity"`
	Prefix           *PromotionObservation `json:"prefix,omitempty"`
}

func (s *State) validateResourceObservation(intent HostActionIntent, o ResourceObservation) error {
	if intent.Adapter == nil || o.ParametersDigest != intent.PayloadDigest || o.Resource.Owner != s.RunID || s.GitResources[o.Resource.ID] != o.Resource {
		return validationError("withdrawal requires its original registered resource")
	}
	raw, _ := json.Marshal(intent.Adapter.Params)
	switch intent.Adapter.Operation {
	case "op.workspace":
		var p WorkspaceParameters
		if json.Unmarshal(raw, &p) != nil || p.Resource != o.Resource {
			return validationError("workspace withdrawal differs from original inputs")
		}
	case "op.candidate":
		p, err := candidateParameters(intent)
		if err != nil || p.Promotion != nil || p.WorkspaceID != o.Resource.ID {
			return validationError("candidate withdrawal differs from original inputs")
		}
	default:
		return validationError("resource withdrawal is not a workspace or candidate")
	}
	if o.WorktreeExists != (o.Prefix != nil) || o.Prefix != nil && (o.Prefix.Head == "" || o.Prefix.Tree == "" || o.Prefix.IndexTree == "") {
		return validationError("resource withdrawal has no exact observed prefix")
	}
	if o.RefIdentity != "" && o.RefIdentity != o.Resource.Source && (o.Prefix == nil || o.RefIdentity != o.Prefix.Head) {
		return validationError("resource ref conflicts with its original source or prefix")
	}
	return nil
}

func (s *State) resource(role, source, scope string) GitResource {
	id := strings.TrimPrefix(digestOfCanonical(struct{ Role, Source, Scope string }{role, source, scope}), "sha256:")
	return GitResource{ID: id, Owner: s.RunID, Role: role, Path: filepath.Join(s.NativeRoot, ".gates", "qa-isolation", s.RunID, id), Ref: "refs/formal-gates/" + s.RunID + "/" + id, Source: source, Status: "PLANNED"}
}
func (s *State) taskResource(key runtime.TaskKey, cd *compiler.CompiledDefinition) GitResource {
	if s.NativeRoot == "" || s.Validation == nil {
		return GitResource{}
	}
	for _, b := range s.DispatchBindings {
		if b.Task == key && (b.Generation == s.Validation.Generation && b.RequirementRevision == s.Validation.RequirementRevision || s.validationWorkCarryAuthorized("", b.Task)) && b.WorkspaceID != "" && s.resourceUsable(b.WorkspaceID) {
			return s.GitResources[b.WorkspaceID]
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
	case "engine.repair":
		for _, work := range s.repairWork() {
			if work.task == key {
				source = work.wave.Candidate.Identity
				scope = work.wave.ID
			}
		}
		role = "development"
	case "engine.qa.design":
		if s.validationMode(key) == coverage.ReviewWhitebox {
			role = "whitebox-authoring"
			source = s.Validation.DevelopmentSnapshot
			scope = key.Scope + ":" + strconv.FormatUint(s.Validation.Generation, 10)
		} else {
			scope += ":" + strconv.FormatUint(s.Validation.Generation, 10)
			role = "blackbox-design"
		}
	case "engine.qa.review.rationale":
		set := s.Validation.Sets[s.validationMode(key)]
		if set == nil {
			return GitResource{}
		}
		return s.taskResource(set.DesignTask, cd)
	case "engine.qa.execution", "engine.gate":
		wave := s.currentValidationWave()
		if wave == nil {
			return GitResource{}
		}
		source = wave.Candidate.Identity
		scope = wave.ID
		if role == "engine.qa.execution" {
			role = strings.ToLower(string(s.validationMode(key))) + "-execution"
		} else {
			role = "gate"
		}
	default:
		return GitResource{}
	}
	if source == "" {
		return GitResource{}
	}
	resource := s.resource(role, source, scope)
	if existing, ok := s.GitResources[resource.ID]; ok && existing.Status != "CLEANED" && !s.ResourceCleanupPending(resource.ID) {
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
func (s *State) candidateResource(p CandidateParameters, cd *compiler.CompiledDefinition) GitResource {
	switch p.Stage {
	case "DEVELOPMENT":
		var chosen GitResource
		var latest uint64
		for _, task := range p.SourceTasks {
			if result := s.developmentResultRecord(task); result != nil {
				issued := s.issuedRevisionOf(result.ActionID)
				if issued >= latest {
					latest = issued
					chosen = s.GitResources[result.Validation.Binding.WorkspaceID]
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
				return s.GitResources[original.WorkspaceID]
			}
		}
		return s.resource("candidate", p.Base, candidateCorrelation(p)+":"+strconv.FormatUint(s.Validation.Generation, 10))
	}
	return GitResource{}
}
func (e *Engine) ensureWorkspace(s *State, r GitResource, nextRevision uint64) (bool, error) {
	if r.ID == "" || s.GitResources[r.ID].Status == "CLEANED" || s.ResourceCleanupPending(r.ID) {
		return false, nil
	}
	params := WorkspaceParameters{Resource: r, RequirementRevision: s.Validation.RequirementRevision, Generation: s.Validation.Generation, RouteScope: s.validationRouteDigest()}
	if r.Role == "blackbox-design" && s.Validation != nil {
		if set := s.Validation.Sets[coverage.ReviewBlackbox]; set != nil {
			manifest := set.Manifest
			params.Manifest = &manifest
		}
	}
	if existing, ok := s.GitResources[r.ID]; ok && existing.Status == "READY" && (params.Manifest == nil || existing.MirrorDigest == BlackboxCasesDigest(s.RunID, *params.Manifest)) {
		return false, nil
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.workspace" && s.candidateInputsCurrent(intent, e.cfg.Definition) {
			return false, nil
		}
	}
	if s.GitResources == nil {
		s.GitResources = map[string]GitResource{}
	}
	s.GitResources[r.ID] = r
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
			return e.ensureWorkspace(s, r, nextRevision)
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
		applicable := s.nativeEffectApplicable(intent, e.cfg.Definition)
		project := applicable && !s.abortRequested() && v.Resource.Role == "blackbox-design" && s.candidateInputsCurrent(intent, e.cfg.Definition)
		if applicable {
			s.GitResources[v.Resource.ID] = v.Resource
		}
		if project {
			s.QAWorktree = v.Resource.Path
		}
	case "op.cleanup":
		var v WorkspaceCleanupReceipt
		_ = decodeAdapterValues(receipt, &v)
		for _, r := range v.Resources {
			if old, ok := s.GitResources[r.ID]; ok && old.Owner == r.Owner && old.Path == r.Path && old.Ref == r.Ref {
				s.GitResources[r.ID] = r
			}
		}
	}
	return nil
}
func (s *State) ResourcesCleaned() bool {
	for _, r := range s.GitResources {
		if r.Status != "CLEANED" {
			return false
		}
	}
	return true
}

// cleanupStarted proves the terminal resource boundary from its issued intent
// or accepted receipt and the original abort or delivered candidate authority.
func (s *State) cleanupStarted(cd *compiler.CompiledDefinition) bool {
	authority := s.abortRequested()
	if !authority && s.SliceChild == nil && s.CandidatePromotion != nil {
		p := s.CandidatePromotion
		original, exists := s.hostIntent(p.OperationID)
		params, err := candidateParameters(original)
		authority = exists && err == nil && s.promotionAuthorized(params) && p.Provider == "git" && p.TargetRoot == s.NativeRoot && p.TargetBase == s.BaseSnapshot && p.Final == s.PromotedSnapshot && p.FinalDigest == s.PromotedDigest && p.FinalDigest == p.SourceDigest
	}
	if !authority {
		return false
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.cleanup" {
			continue
		}
		var p CleanupParameters
		raw, err := json.Marshal(intent.Adapter.Params)
		if err != nil || json.Unmarshal(raw, &p) != nil || len(p.Resources) == 0 {
			continue
		}
		valid := true
		for _, r := range p.Resources {
			valid = valid && r.Owner == s.RunID && s.GitResources[r.ID] == r
		}
		if valid {
			return true
		}
	}
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.cleanup" || receipt.Status != HostActionStatusExecuted {
			continue
		}
		var proof WorkspaceCleanupReceipt
		if decodeAdapterValues(receipt, &proof) != nil || len(proof.Resources) == 0 {
			continue
		}
		valid := true
		for _, r := range proof.Resources {
			registered, ok := s.GitResources[r.ID]
			valid = valid && ok && r.Owner == s.RunID && r.Status == "CLEANED" && registered == r
		}
		if valid {
			return true
		}
	}
	return false
}

func (s *State) resourceUsable(id string) bool {
	r, exists := s.GitResources[id]
	return exists && r.Status != "CLEANED" && !s.ResourceCleanupPending(id)
}

func (s *State) workspaceParametersCurrent(p WorkspaceParameters, cd *compiler.CompiledDefinition) bool {
	if s.Validation == nil || s.abortRequested() || p.RequirementRevision != s.Validation.RequirementRevision || p.Generation != s.Validation.Generation || p.RouteScope != s.validationRouteDigest() || !s.resourceUsable(p.Resource.ID) || s.GitResources[p.Resource.ID] != p.Resource {
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
	view := decision.State{RequiredTasks: map[authoring.StepID][]decision.RequiredTask{}}
	s.projectValidationTasks(&view, cd)
	for _, tasks := range view.RequiredTasks {
		for _, task := range tasks {
			if s.taskResource(task.Task, cd).ID == p.Resource.ID {
				return true
			}
		}
	}
	current, ready, _ := s.currentCandidateParameters(cd)
	if ready {
		if s.candidateResource(current, cd).ID == p.Resource.ID {
			return true
		}
		if current.Stage == "QA_ARTIFACT" {
			return s.resource("candidate", current.Base, "ownership:"+candidateCorrelation(current)+":"+strconv.FormatUint(s.Validation.Generation, 10)).ID == p.Resource.ID
		}
	}
	return false
}

func (s *State) cleanupParametersCurrent(p CleanupParameters) bool {
	var expected []GitResource
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
	for _, r := range s.GitResources {
		if r.Status != "CLEANED" {
			expected = append(expected, r)
		}
	}
	actual := slices.Clone(p.Resources)
	order := func(a, b GitResource) int { return strings.Compare(a.ID, b.ID) }
	slices.SortFunc(expected, order)
	slices.SortFunc(actual, order)
	return len(expected) != 0 && slices.Equal(actual, expected)
}

func (s *State) CleanupEligible(cd *compiler.CompiledDefinition) bool {
	if s.pendingControl() || s.nonCleanupNativeDebt() || s.SliceChild != nil && !s.abortRequested() || len(s.PendingActions) != 0 {
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
	return s.abortRequested() || s.validationDeliveryComplete(cd)
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

func (e *Engine) BeginCleanup(expectedFingerprint string) error {
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s == nil {
		return validationError("cleanup requires a run")
	}
	if !s.CleanupEligible(e.cfg.Definition) {
		return validationError("cleanup requires concluded validation or an abort decision")
	}
	if s.SliceChild != nil && !s.abortRequested() {
		return validationError("a child retains its checkpoint resources")
	}
	if s.ResourcesCleaned() {
		return nil
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.cleanup" {
			return nil
		}
	}
	if s.Phase != runtime.PhaseFinalizingCleanup {
		if err := s.TransitionPhase(runtime.PhaseFinalizingCleanup); err != nil {
			return err
		}
	}
	var resources []GitResource
	for _, r := range s.GitResources {
		if r.Status != "CLEANED" {
			resources = append(resources, r)
		}
	}
	slices.SortFunc(resources, func(a, b GitResource) int { return strings.Compare(a.ID, b.ID) })
	intent, err := e.prepareAdapterIntent(s, revision+1, "op.cleanup", CleanupParameters{Resources: resources}, "")
	if err != nil {
		return err
	}
	s.PendingHostActions[intent.ActionID] = intent
	_, err = e.commit(s, revision, expectedFingerprint)
	return err
}
func (s *State) projectResourceBoundary(view *decision.State, cd *compiler.CompiledDefinition) {
	for _, step := range cd.Steps {
		p, ok := step.Payload.(compiler.CompiledHostActionStep)
		if !ok || (p.Operation != "op.workspace" && p.Operation != "op.cleanup") {
			continue
		}
		view.RequiredTasks[step.Header.ID] = nil
		for _, intent := range s.ExecutableHostActions() {
			if intent.Adapter != nil && intent.Adapter.Operation == p.Operation && s.NativeActionCurrent(intent, cd) {
				view.RequiredTasks[step.Header.ID] = append(view.RequiredTasks[step.Header.ID], decision.RequiredTask{Task: runtime.TaskKey{Node: step.Header.NodeID, Step: step.Header.ID, Scope: intent.ActionID}})
			}
		}
	}
}

// BlackboxCasesMarkdown renders the same complete manifest for the isolated
// review mirror and the final tracked artifact.
func BlackboxCasesMarkdown(runID string, m coverage.AcceptanceManifest) string {
	data, _ := json.MarshalIndent(m, "", "  ")
	return "# Blackbox QA cases (run: " + runID + ")\n\n```json\n" + string(data) + "\n```\n"
}

func (e *Engine) applyNativeReceipt(s *State, intent HostActionIntent, receipt HostActionReceipt) error {
	if intent.Adapter == nil {
		return nil
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
