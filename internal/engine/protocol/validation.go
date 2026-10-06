package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/requirement"
)

// Validation owns the run's QA artifacts and post-development obligations.
// Scheduling, attempts and user authorization remain in their protocol owners.
type Validation struct {
	Document            requirement.FrozenRequirementProjection `json:"document"`
	RequirementRevision string                                  `json:"requirementRevision"`
	Generation          uint64                                  `json:"generation"`
	Candidate           *coverage.ValidationCandidate           `json:"candidate,omitempty"`
	AuthoringWorkspace  string                                  `json:"authoringWorkspace,omitempty"`
	DevelopmentSnapshot string                                  `json:"developmentSnapshot,omitempty"`
	Sets                map[coverage.ReviewKind]*CaseSet        `json:"sets"`
	Waves               []ValidationWave                        `json:"waves"`
	Findings            map[string]*ValidationFinding           `json:"findings"`
	Problems            map[string]*ValidationProblem           `json:"problems"`
}

// Validation work uses the inventory observed for the latest local confirmation.
// Equal document bytes can belong to a different confirmation occurrence, so a
// reset's temporary projection cannot authorize work before its document sync.
func (s *State) validationRequirementCurrent() bool {
	return s.Validation != nil && s.Validation.RequirementRevision == s.RequirementRevision() && s.Validation.Generation == s.requirementGeneration()
}

// DesignTask refers to the accepted design result supplying the artifact inputs.
type CaseSet struct {
	DesignTask    runtime.TaskKey             `json:"designTask"`
	AffectedCases []string                    `json:"affectedCases,omitempty"`
	Paths         []string                    `json:"paths,omitempty"`
	Manifest      coverage.AcceptanceManifest `json:"manifest"`
	Review        *coverage.ReviewResult      `json:"review,omitempty"`
	Version       string                      `json:"version"`
	Candidate     string                      `json:"candidate,omitempty"`
	Status        string                      `json:"status"`
	Suggestions   []QASuggestion              `json:"suggestions,omitempty"`
	ReviewHistory []CaseReviewEvidence        `json:"reviewHistory,omitempty"`
}
type CaseReviewEvidence struct {
	Candidate string                      `json:"candidate,omitempty"`
	Manifest  coverage.AcceptanceManifest `json:"manifest"`
	Review    coverage.ReviewResult       `json:"review"`
}
type QASuggestion struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type RepairCaseEvidence struct {
	Mode        coverage.ReviewKind      `json:"mode"`
	CaseID      string                   `json:"caseId"`
	Procedure   string                   `json:"procedure"`
	Observation string                   `json:"observation"`
	Comparison  string                   `json:"comparison"`
	Result      coverage.ExecutionStatus `json:"result"`
}
type DevelopmentEvidence struct {
	Completion    *DevelopmentCompletion `json:"completion,omitempty"`
	CaseRuns      []RepairCaseEvidence   `json:"caseRuns,omitempty"`
	Paths         []string               `json:"paths"`
	Summary       string                 `json:"summary"`
	Build         []string               `json:"build"`
	Tests         []string               `json:"tests"`
	AffectedCases []string               `json:"affectedCases,omitempty"`
}

// ValidationResult is attached to WORKER_RESULT. Outcome describes the worker
// transport; Status and per-item results describe the business verdict.
type ValidationResult struct {
	GranularityReview *GranularityReview           `json:"granularityReview,omitempty"`
	Resolution        *ConflictResolution          `json:"resolution,omitempty"`
	Binding           WorkBinding                  `json:"binding"`
	Status            string                       `json:"status"`
	Development       *DevelopmentEvidence         `json:"development,omitempty"`
	Manifest          *coverage.AcceptanceManifest `json:"manifest,omitempty"`
	Review            *coverage.ReviewResult       `json:"review,omitempty"`
	Suggestions       []QASuggestion               `json:"suggestions,omitempty"`
	Execution         *coverage.ExecutionReport    `json:"execution,omitempty"`
	Tool              string                       `json:"tool,omitempty"`
	Environment       string                       `json:"environment,omitempty"`
	Checkpoint        bool                         `json:"checkpoint,omitempty"`
	Findings          []review.Finding             `json:"findings,omitempty"`
}

// WorkBinding accompanies every issued business task and its result. Revision
// is the confirmed content identity; Generation distinguishes its occurrence.
type WorkBinding struct {
	WorkspaceID         string          `json:"workspaceId,omitempty"`
	PlanRequest         string          `json:"planRequest,omitempty"`
	BatchID             string          `json:"batchId,omitempty"`
	RouteScope          string          `json:"routeScope,omitempty"`
	RequirementRevision string          `json:"requirementRevision"`
	Generation          uint64          `json:"generation"`
	Candidate           string          `json:"candidate,omitempty"`
	CaseVersion         string          `json:"caseVersion,omitempty"`
	WaveID              string          `json:"waveId,omitempty"`
	Task                runtime.TaskKey `json:"task"`
}

type ValidationWave struct {
	CheckpointTasks      []runtime.TaskKey              `json:"checkpointTasks,omitempty"`
	AuthorizationRequest string                         `json:"authorizationRequest,omitempty"`
	Coverage             coverage.CoverageContract      `json:"coverage"`
	Reviews              []coverage.ReviewResult        `json:"reviews"`
	ID                   string                         `json:"id"`
	Candidate            coverage.ValidationCandidate   `json:"candidate"`
	RequirementRevision  string                         `json:"requirementRevision"`
	Generation           uint64                         `json:"generation"`
	RouteDigest          string                         `json:"routeDigest"`
	Versions             map[coverage.ReviewKind]string `json:"versions"`
	ExpectedDigest       string                         `json:"expectedDigest"`
	Targets              map[string]*ValidationTarget   `json:"targets"`
	Status               string                         `json:"status"`
	SourceTasks          []runtime.TaskKey              `json:"sourceTasks"`
	Counted              bool                           `json:"counted"`
	Scope                coverage.ScopeMode             `json:"scope"`
	Repair               *ValidationRepair              `json:"repair,omitempty"`
	ExtraAuthorized      bool                           `json:"extraAuthorized,omitempty"`
}

// GateImpact binds the host's impact judgment to the original frozen passing
// gate boundary. The source wave owns its candidate, result and responsibility.
type GateImpact struct {
	SourceWave string `json:"sourceWave"`
	Decision   string `json:"decision,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type ValidationTarget struct {
	SliceID    string           `json:"sliceId,omitempty"`
	Reuse      *ValidationReuse `json:"reuse,omitempty"`
	GateImpact *GateImpact      `json:"gateImpact,omitempty"`
	// RequiredCases and ScopeReason are the host's immutable AFFECTED execution decision.
	RequiredCases     []string                  `json:"requiredCases,omitempty"`
	ScopeReason       string                    `json:"scopeReason,omitempty"`
	Task              runtime.TaskKey           `json:"task"`
	Kind              string                    `json:"kind"`
	Mode              coverage.ReviewKind       `json:"mode,omitempty"`
	Gate              string                    `json:"gate,omitempty"`
	Status            string                    `json:"status"`
	ResultAction      string                    `json:"resultAction,omitempty"`
	Execution         *coverage.ExecutionReport `json:"execution,omitempty"`
	Tool              string                    `json:"tool,omitempty"`
	Environment       string                    `json:"environment,omitempty"`
	DispositionReason string                    `json:"dispositionReason,omitempty"`
}
type ValidationRepair struct {
	Targets  map[string]*ValidationTarget `json:"targets,omitempty"`
	Task     runtime.TaskKey              `json:"task"`
	Problems []string                     `json:"problems"`
	PureP2   bool                         `json:"pureP2"`
}

// Target lookup includes repair gates without adding them to the frozen wave
// expected set. The maps retain the original and replacement responsibilities.
func (w *ValidationWave) target(id string) *ValidationTarget {
	if w == nil {
		return nil
	}
	if target := w.Targets[id]; target != nil {
		return target
	}
	if w.Repair != nil {
		return w.Repair.Targets[id]
	}
	return nil
}

func (w *ValidationWave) controlTargets() []*ValidationTarget {
	if w == nil {
		return nil
	}
	var targets []*ValidationTarget
	for _, target := range w.Targets {
		targets = append(targets, target)
	}
	if w.Repair != nil {
		for _, target := range w.Repair.Targets {
			targets = append(targets, target)
		}
	}
	return targets
}

// A finding owns exactly one original evidence domain and one adjudication history.
// ExecutionFailure references the completed target's cumulative report, including
// records accepted through checkpoints; it never assigns a severity to case FAIL.
type ValidationFinding struct {
	ID               string              `json:"id"`
	WaveID           string              `json:"waveId"`
	Target           string              `json:"target"`
	Raw              *review.Finding     `json:"raw,omitempty"`
	ExecutionFailure *ExecutionFailure   `json:"executionFailure,omitempty"`
	History          []FindingResolution `json:"history"`
}
type ExecutionFailure struct {
	ResultAction string `json:"resultAction"`
	CaseID       string `json:"caseId"`
}
type FindingResolution struct {
	// AcceptedRevision is assigned by the engine when this historical fact is accepted.
	AcceptedRevision  uint64                     `json:"acceptedRevision"`
	FindingID         string                     `json:"findingId"`
	Decision          string                     `json:"decision"`
	ProblemID         string                     `json:"problemId,omitempty"`
	Requirement       string                     `json:"requirement"`
	NormalPath        string                     `json:"normalPath"`
	Module            string                     `json:"module"`
	RootCause         string                     `json:"rootCause"`
	Consequence       string                     `json:"consequence"`
	Severity          string                     `json:"severity"`
	SeverityRationale string                     `json:"severityRationale"`
	Verification      review.FindingVerification `json:"verification"`
	Reason            string                     `json:"reason"`
	Supersedes        int                        `json:"supersedes"`
}
type ValidationProblem struct {
	ResolvedWave string   `json:"resolvedWave,omitempty"`
	ID           string   `json:"id"`
	FindingIDs   []string `json:"findingIds"`
}

// ValidationObservation carries semantic facts. It cannot authorize a retry,
// extra review or repair, a THAW, or an error disposition on the user's behalf.
type ValidationObservation struct {
	Gate                string              `json:"gate,omitempty"`
	Kind                string              `json:"kind"`
	RequirementRevision string              `json:"requirementRevision,omitempty"`
	Document            string              `json:"document,omitempty"`
	Mode                coverage.ReviewKind `json:"mode,omitempty"`
	SuggestionID        string              `json:"suggestionId,omitempty"`
	Decision            string              `json:"decision,omitempty"`
	Reason              string              `json:"reason,omitempty"`
	AffectedCases       []string            `json:"affectedCases,omitempty"`
	Findings            []FindingResolution `json:"findings,omitempty"`
}

// CandidateArtifact applies only its declared test paths from the immutable
// source tree. A declared path absent from that tree is a deletion. Production
// paths come from CandidateParameters.Base, never from the artifact source.
type CandidateArtifact struct {
	Identity string   `json:"identity"`
	Paths    []string `json:"paths"`
}
type CandidateParameters struct {
	RepairTask          *runtime.TaskKey                          `json:"repairTask,omitempty"`
	Integration         *IntegrationParameters                    `json:"integration,omitempty"`
	MergeManifest       *coverage.AcceptanceManifest              `json:"mergeManifest,omitempty"`
	Promotion           *PromotionParameters                      `json:"promotion,omitempty"`
	WorkspaceID         string                                    `json:"workspaceId,omitempty"`
	BlackboxManifest    *coverage.AcceptanceManifest              `json:"blackboxManifest,omitempty"`
	ReviewBase          string                                    `json:"reviewBase,omitempty"`
	ReviewCases         map[string]string                         `json:"reviewCases,omitempty"`
	SourceTasks         []runtime.TaskKey                         `json:"sourceTasks,omitempty"`
	DesignTasks         map[coverage.ReviewKind]runtime.TaskKey   `json:"designTasks,omitempty"`
	RouteScope          string                                    `json:"routeScope,omitempty"`
	QAArtifacts         map[coverage.ReviewKind]CandidateArtifact `json:"qaArtifacts,omitempty"`
	Stage               string                                    `json:"stage"`
	RequirementRevision string                                    `json:"requirementRevision"`
	Generation          uint64                                    `json:"generation"`
	Base                string                                    `json:"base,omitempty"`
	Paths               []string                                  `json:"paths"`
	CaseVersions        map[coverage.ReviewKind]string            `json:"caseVersions,omitempty"`
	Finalize            bool                                      `json:"finalize"`
}

// CaseReviewView is the trusted adapter's complete native input closure for
// one test: its implementation, production sources, shared fixtures/helpers,
// configuration, build inputs and other sources affecting reviewability.
// Inputs maps each path to its content digest. An unavailable or UNKNOWN
// closure is omitted from ReviewViews; a complete single-input view is valid.
type CaseReviewView struct {
	TestRef string            `json:"testRef"`
	Inputs  map[string]string `json:"inputs"`
}

// GateReviewView is the trusted adapter's ownership evidence for the entire
// issued ReviewBase-to-candidate range. Only proved test-owned paths appear;
// UNKNOWN ownership remains visible. A nil view means evidence is unavailable,
// while an empty list confirms that the complete range has no exclusions.
type GateReviewView struct {
	TestOwnedPaths []string `json:"testOwnedPaths"`
}

type CandidateEvidence struct {
	Integration         *IntegrationEvidence           `json:"integration,omitempty"`
	ChildViews          map[string]ChildValidationView `json:"childViews,omitempty"`
	Promotion           *CandidatePromotionReceipt     `json:"promotion,omitempty"`
	Changes             []vcs.Change                   `json:"changes,omitempty"`
	WorkspaceID         string                         `json:"workspaceId,omitempty"`
	GateView            *GateReviewView                `json:"gateView,omitempty"`
	ReviewViews         map[string]CaseReviewView      `json:"reviewViews,omitempty"`
	ParametersDigest    string                         `json:"parametersDigest"`
	Identity            string                         `json:"identity"`
	Digest              string                         `json:"digest"`
	Paths               []string                       `json:"paths"`
	TestOnly            bool                           `json:"testOnly"`
	ProductionViewEqual bool                           `json:"productionViewEqual"`
}

type ValidationRequest struct {
	Binding       WorkBinding         `json:"binding"`
	Obligations   []string            `json:"obligations,omitempty"`
	Remedies      []string            `json:"remedies,omitempty"`
	Kind          string              `json:"kind"`
	Target        string              `json:"target"`
	Mode          coverage.ReviewKind `json:"mode,omitempty"`
	Category      string              `json:"category,omitempty"`
	AffectedCases []string            `json:"affectedCases,omitempty"`
	Digest        string              `json:"digest"`
	StateRevision uint64              `json:"stateRevision"`
	Summary       json.RawMessage     `json:"summary,omitempty"`
}
type ValidationDecision struct {
	Obligations   []string `json:"obligations,omitempty"`
	Remedy        string   `json:"remedy,omitempty"`
	Reason        string   `json:"reason"`
	Digest        string   `json:"digest"`
	StateRevision uint64   `json:"stateRevision"`
}

func validationHandler(step compiler.CompiledStep) string {
	p, ok := step.Payload.(compiler.CompiledAgentStep)
	if !ok {
		return ""
	}
	switch p.Handler {
	case "engine.development.batch", "engine.merge.resolve", "engine.qa.design", "engine.qa.review.rationale", "engine.qa.execution", "engine.gate", "engine.repair":
		return string(p.Handler)
	}
	return ""
}
func pipelineStep(cd *compiler.CompiledDefinition, handler string) (compiler.CompiledStep, bool) {
	for _, step := range cd.Steps {
		if validationHandler(step) == handler {
			return step, true
		}
	}
	return compiler.CompiledStep{}, false
}
func pipelineEnabled(s *State, cd *compiler.CompiledDefinition) bool {
	_, ok := pipelineStep(cd, "engine.development.batch")
	return ok && s.Route != "lightweight"
}
func pipelineTask(cd *compiler.CompiledDefinition, handler, scope string) runtime.TaskKey {
	step, _ := pipelineStep(cd, handler)
	return runtime.TaskKey{Node: step.Header.NodeID, Step: step.Header.ID, Scope: scope}
}
func (s *State) currentValidationWave() *ValidationWave {
	if s.Validation == nil || len(s.Validation.Waves) == 0 {
		return nil
	}
	return &s.Validation.Waves[len(s.Validation.Waves)-1]
}
func (s *State) validationKinds() []coverage.ReviewKind {
	var kinds []coverage.ReviewKind
	for _, g := range s.SelectedGates {
		switch g {
		case "blackbox":
			kinds = append(kinds, coverage.ReviewBlackbox)
		case "whitebox":
			kinds = append(kinds, coverage.ReviewWhitebox)
		case mergeQAGateID:
			kinds = append(kinds, coverage.ReviewMerge)
		}
	}
	slices.Sort(kinds)
	return kinds
}

// routeHasNoQAModes 报告所选路线是否不含任何 QA 审查模式——覆盖义务豁免
// 的唯一语义判据（default、QA-free custom 与 QA-free full child 同判；普通 full 必
// 含双内建 QA，由路线接纳层强制，不会落入本形态）。
func (s *State) routeHasNoQAModes() bool {
	return len(s.validationKinds()) == 0
}
func (s *State) validationRole(key runtime.TaskKey, cd *compiler.CompiledDefinition) string {
	for _, step := range cd.Steps {
		if step.Header.ID == key.Step {
			return validationHandler(step)
		}
	}
	return ""
}
func (s *State) validationMode(key runtime.TaskKey) coverage.ReviewKind {
	key = s.sourceValidationTask(key)
	parts := strings.Split(key.Scope, ":")
	if len(parts) < 2 {
		return ""
	}
	mode := coverage.ReviewKind(parts[1])
	if slices.Contains(s.validationKinds(), mode) {
		return mode
	}
	return ""
}
func (s *State) qaReviewTarget(mode coverage.ReviewKind) string {
	return fmt.Sprintf("%d:%s:%d", s.Validation.Generation, mode, len(s.Validation.Sets[mode].ReviewHistory))
}

// Review history owns the failure streak; an issued review owns its authority
// through receipt pairing, retries and an explicitly carried requirement change.
func (s *State) prewaveBlocked(mode coverage.ReviewKind, role string, cd *compiler.CompiledDefinition) bool {
	set := s.Validation.Sets[mode]
	failures := 0
	for i := len(set.ReviewHistory) - 1; i >= 0 && set.ReviewHistory[i].Review.Verdict() == coverage.StatusFail; i-- {
		failures++
	}
	if failures < 3 {
		return false
	}
	for action, binding := range s.DispatchBindings {
		if s.liveDispatch(action) && s.prewaveWorkCurrent(binding, cd) && s.validationRole(binding.Task, cd) == "engine.qa.review.rationale" && s.validationMode(binding.Task) == mode {
			return false
		}
	}
	for task, attempt := range s.EndedAttempts {
		if s.endedWorkCurrent(attempt, cd) && s.validationRole(task, cd) == "engine.qa.review.rationale" && s.validationMode(task) == mode {
			return false
		}
	}
	for id, d := range s.Decisions {
		r := s.PendingAsks[id].Validation
		if r == nil || r.Mode != mode {
			continue
		}
		original := requestCaseSet(r)
		if original == nil || len(original.ReviewHistory) != len(set.ReviewHistory) {
			continue
		}
		switch r.Kind {
		case "QA_REVIEW":
			if r.Target == s.qaReviewTarget(mode) && (d.Choice == "redesign" || d.Choice == "approve" && role == "engine.qa.review.rationale") {
				return false
			}
		case "RETRY":
			if d.Choice == "redesign" {
				return false
			}
		case "THAW", "QA_ARTIFACT_REPAIR":
			if d.Choice == "approve" {
				return false
			}
		case "VALIDATION_DETAIL_DISPOSITION":
			if d.Choice == "approve" && d.Validation != nil && d.Validation.Remedy == "QA_ARTIFACT_REPAIR" {
				return false
			}
		}
	}
	return true
}

func (s *State) validationBinding(key runtime.TaskKey, cd *compiler.CompiledDefinition) WorkBinding {
	v := s.Validation
	if v == nil {
		return WorkBinding{Task: key}
	}
	b := WorkBinding{RequirementRevision: v.RequirementRevision, Generation: v.Generation, RouteScope: s.validationRouteDigest(), Task: key}
	if gate, ok := s.repairGateBinding(key); ok {
		if r := s.taskResource(key, cd); r.Status == "READY" {
			gate.WorkspaceID = r.ID
		}
		return gate
	}
	role := s.validationRole(key, cd)
	if role == "engine.merge.resolve" {
		if params, ready := s.nextIntegrationParameters(cd); ready && params.Integration != nil {
			b.Candidate, b.CaseVersion = params.Base, params.Integration.StartAction
		}
	}
	if role == "engine.development.batch" {
		if request, _, batch, ok := s.developmentBatch(key, cd); ok {
			b.PlanRequest, b.BatchID = request, batch.ID
			_, _, revision := s.confirmedDevelopmentPlan()
			b.RequirementRevision = revision
			if revised := s.requirementRevisionForTask(key); revised != "" {
				b.RequirementRevision = revised
			}
		}
	}
	mode := s.validationMode(key)
	if set := v.Sets[mode]; set != nil {
		b.CaseVersion = set.Version
	}
	if role != "engine.development.batch" && role != "engine.merge.resolve" && !(role == "engine.qa.design" && (mode == coverage.ReviewBlackbox || mode == coverage.ReviewMerge)) {
		b.Candidate = v.DevelopmentSnapshot
		if v.Candidate != nil {
			b.Candidate = v.Candidate.Identity
		}
	}
	if role == "engine.qa.review.rationale" {
		if set := v.Sets[mode]; set != nil && set.Candidate != "" {
			b.Candidate = set.Candidate
		}
	}
	if role == "engine.repair" {
		for _, work := range s.repairWork() {
			if work.task == key {
				b.WaveID, b.Candidate = work.wave.ID, work.wave.Candidate.Identity
				if work.previous != nil {
					_, prior, ok := s.repairSource(work.previous.Validation.Binding.Task)
					if ok {
						b.Candidate = prior.Identity
					}
				}
				if work.verification {
					_, proof, _ := s.repairFinalSource(work)
					b.Candidate = proof.Identity
				}
				if len(s.repairMembers(work.wave)) == 1 && s.requirementRevisionForTask(work.task) != "" {
					b.Candidate = v.DevelopmentSnapshot
				}
				b.PlanRequest = work.planAction
				if work.batch != nil {
					b.BatchID = work.batch.ID
				}
			}
		}
	}
	if wave := s.currentValidationWave(); wave != nil && (role == "engine.qa.execution" || role == "engine.gate") {
		b.WaveID = wave.ID
		b.Candidate = wave.Candidate.Identity
	}
	if r := s.taskResource(key, cd); r.Status == "READY" {
		b.WorkspaceID = r.ID
	}
	return b
}

// A design obligation advances only after an accepted design result. Manifest
// content, review rounds and failed execution attempts do not identify it.
func (s *State) nextDesignScope(mode coverage.ReviewKind, cd *compiler.CompiledDefinition) string {
	completed := 0
	for _, result := range s.Results {
		if result.Outcome != OutcomePass || result.Validation == nil || result.Validation.Status != "PASS" || result.Validation.Manifest == nil {
			continue
		}
		key := result.Validation.Binding.Task
		if s.validationRole(key, cd) == "engine.qa.design" && s.validationMode(key) == mode {
			completed++
		}
	}
	return fmt.Sprintf("%d:%s:%d:%s", s.Validation.Generation, mode, completed+1, s.prewaveScope(mode))
}

func (s *State) projectValidationTasks(view *decision.State, cd *compiler.CompiledDefinition) {
	if !pipelineEnabled(s, cd) {
		return
	}
	for _, step := range cd.Steps {
		if validationHandler(step) != "" {
			view.RequiredTasks[step.Header.ID] = nil
		}
	}
	if !s.validationRequirementCurrent() || s.Topology == nil || s.RouteMode == "" {
		return
	}
	v := s.Validation
	add := func(handler, scope string, blocked bool) {
		key := s.validationRequiredTask(pipelineTask(cd, handler, scope), s.currentValidationWave(), cd)
		blocked = blocked || !s.workspaceReady(key, cd)
		reason := decision.WaitReason("")
		if blocked {
			reason = decision.WaitDependencies
		}
		if retry, ok := s.EndedAttempts[key]; ok && retry.RetryExhausted {
			reason = decision.WaitCapacity
		}
		view.RequiredTasks[key.Step] = append(view.RequiredTasks[key.Step], decision.RequiredTask{Task: key, BlockedBy: reason})
	}
	s.projectDevelopmentTasks(view, cd)
	s.projectRepairGates(view, cd)
	if p, ready := s.nextIntegrationParameters(cd); ready && p.Integration.StartAction != "" && len(s.conflictInput(p.Integration.StartAction).Evidence.Conflicts) > 0 && s.integrationResolution(p.Integration.StartAction) == nil {
		add("engine.merge.resolve", p.Integration.StartAction, false)
	}
	for _, mode := range s.validationKinds() {
		set := v.Sets[mode]
		if s.caseSetWaiver(mode) != "" {
			continue
		}
		if set == nil {
			if mode == coverage.ReviewBlackbox || mode == coverage.ReviewMerge || v.AuthoringWorkspace != "" {
				add("engine.qa.design", s.nextDesignScope(mode, cd), false)
			}
			continue
		}
		switch set.Status {
		case "DRAFT", "THAWED":
			add("engine.qa.design", s.nextDesignScope(mode, cd), s.prewaveBlocked(mode, "engine.qa.design", cd))
		case "REVIEW":
			add("engine.qa.review.rationale", s.reviewTask(mode, v.Generation, set.Version, cd).Scope, s.prewaveBlocked(mode, "engine.qa.review.rationale", cd) || (mode == coverage.ReviewWhitebox && set.Candidate == ""))
		}
	}
	wave := s.currentValidationWave()
	if wave == nil {
		return
	}
	if wave.Status == "COLLECTING" && s.validateCoverageRelations() == nil {
		for _, target := range wave.Targets {
			if target.Status == "PENDING" || target.Status == "RUNTIME_ERROR" || target.Status == "RETRY_EXHAUSTED" {
				handler := "engine.gate"
				if target.Kind == "QA" {
					handler = "engine.qa.execution"
				}
				add(handler, target.Task.Scope, (target.Kind == "QA" && wave.Scope == coverage.ScopeAffected && len(target.RequiredCases) == 0) || target.GateImpact != nil && target.GateImpact.Decision == "")
			}
		}
	}
	for _, member := range s.repairWork() {
		if member.result != nil {
			continue
		}
		work := &member
		blocked := !s.workspaceReady(work.task, cd) || !s.repairReady(work) || s.validationDispositionBlocked(work.wave) || s.completedValidationWaves() >= automaticValidationWaveLimit && !s.repairExtraAuthorized(work) && !work.wave.Repair.PureP2 && !work.pureP2 && !work.verification
		reason := decision.WaitReason("")
		if blocked {
			reason = decision.WaitDependencies
		}
		view.RequiredTasks[work.task.Step] = append(view.RequiredTasks[work.task.Step], decision.RequiredTask{Task: work.task, BlockedBy: reason})
	}
}

func (s *State) coverageContract(candidate *coverage.ValidationCandidate) coverage.CoverageContract {
	v := s.Validation
	b := coverage.SourceBinding{RunID: s.RunID, RequirementRevision: v.RequirementRevision, SolutionRevision: v.RequirementRevision}
	if s.IntakeConfirmationReceipt != nil {
		b.SolutionRevision = s.IntakeConfirmationReceipt.SolutionRevision
	}
	c := coverage.CoverageContract{RequiredSources: coverage.RequiredSources{Binding: b}, SelectedKinds: s.validationKinds(), Candidate: candidate}
	for _, req := range v.Document.Requirements {
		c.RequiredSources.Sources = append(c.RequiredSources.Sources, coverage.RequiredSource{SourceID: req.ID, Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability})
	}
	if inventory := s.confirmedCoverage(); inventory != nil {
		c.RequiredSources.Sources = append(c.RequiredSources.Sources, inventory.Sources...)
		c.AlternativeVerifications = inventory.Alternatives
	}
	for _, mode := range c.SelectedKinds {
		if set := v.Sets[mode]; set != nil {
			c.Manifests = append(c.Manifests, set.Manifest)
		}
	}
	return c
}
func (s *State) validationRouteDigest() string {
	gates := slices.Clone(s.SelectedGates)
	slices.Sort(gates)
	return digestOfCanonical(struct {
		Route    string
		Gates    []string
		Topology *TopologyBinding
	}{s.RouteMode, gates, s.Topology})
}

// automaticValidationWaveLimit 是不经逐次授权的返修轮数上限：计满后每轮
// 返修都需 REPAIR Ask 显式授权（授权闸与 REPAIR Ask 摘要的剩余轮数共用
// 本常量）。
const automaticValidationWaveLimit = 3

func (s *State) completedValidationWaves() int {
	n := 0
	if s.Validation != nil {
		for _, wave := range s.Validation.Waves {
			if wave.Counted {
				n++
			}
			n += s.repairBusinessRounds(&wave, "")
		}
	}
	return n
}
func sortedTargetKeys(w *ValidationWave) []string {
	keys := make([]string, 0, len(w.Targets))
	for key := range w.Targets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *State) validationWave(id string) *ValidationWave {
	if s.Validation == nil {
		return nil
	}
	for i := range s.Validation.Waves {
		if s.Validation.Waves[i].ID == id {
			return &s.Validation.Waves[i]
		}
	}
	return nil
}
func (s *State) validationWaveForTask(key runtime.TaskKey) *ValidationWave {
	if s.Validation == nil {
		return nil
	}
	for i := range s.Validation.Waves {
		if s.Validation.Waves[i].target(key.String()) != nil {
			return &s.Validation.Waves[i]
		}
	}
	return nil
}
