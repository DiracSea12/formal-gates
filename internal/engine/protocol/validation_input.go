package protocol

import (
	"slices"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/requirement"
)

// ValidationInput is a read-only projection of the exact issued obligation.
// It supplies contract values rather than asking the host to reconstruct them.
type ValidationInput struct {
	ReviewScope coverage.ScopeMode `json:"reviewScope,omitempty"`
	// RequirementDocument supplies the current confirmed context alongside
	// the original issued work binding, including explicitly carried work.
	RequirementDocument      requirement.FrozenRequirementProjection `json:"requirementDocument"`
	Workspace                *NativeResource                         `json:"workspace,omitempty"`
	Interruption             *RecoveryRecord                         `json:"interruption,omitempty"`
	Gate                     *GateInput                              `json:"gate,omitempty"`
	InheritableSharedReviews []coverage.ReviewDecision               `json:"inheritableSharedReviews,omitempty"`
	Repair                   *RepairInput                            `json:"repair,omitempty"`
	Development              *DevelopmentInput                       `json:"development,omitempty"`
	RequiredCases            []string                                `json:"requiredCases,omitempty"`
	Inherited                []coverage.ExecutionRecord              `json:"inherited,omitempty"`
	SourcePoints             []SourcePoint                           `json:"sourcePoints,omitempty"`
	Binding                  WorkBinding                             `json:"binding"`
	Handler                  string                                  `json:"handler"`
	Manifest                 coverage.AcceptanceManifest             `json:"manifest"`
	Contract                 *coverage.CoverageContract              `json:"contract,omitempty"`
	Whitelist                *coverage.ApprovedWhitelist             `json:"whitelist,omitempty"`
	Execution                *coverage.ExecutionBinding              `json:"execution,omitempty"`
	Completed                []coverage.ExecutionRecord              `json:"completed,omitempty"`
	InheritableReviews       []coverage.ReviewDecision               `json:"inheritableReviews,omitempty"`
	PendingCases             []string                                `json:"pendingCases,omitempty"`
	Conflict                 *ConflictInput                          `json:"conflict,omitempty"`
}

func (s *State) ValidationInputs(cd *compiler.CompiledDefinition) map[string]ValidationInput {
	values := map[string]ValidationInput{}
	if s.Validation == nil || s.abortRequested() {
		return values
	}
	for id, pending := range s.PendingActions {
		if s.validationDispatchBlocked(id) || !s.prewaveDispatchCurrent(id, cd) {
			continue
		}
		if _, ok := s.DispatchBindings[id]; !ok {
			continue
		}
		values[id] = s.validationInput(pending, cd)
	}
	return values
}

func (s *State) validationInput(pending PendingAction, cd *compiler.CompiledDefinition) ValidationInput {
	binding := s.DispatchBindings[pending.ActionID]
	mode := s.validationMode(pending.Task)
	role := s.validationRole(pending.Task, cd)
	input := ValidationInput{Binding: binding, Handler: role, RequirementDocument: s.Validation.Document}
	if r, ok := s.NativeResources[binding.WorkspaceID]; ok {
		input.Workspace = &r
	}
	for i := len(s.RecoveryRecords) - 1; i >= 0; i-- {
		record := s.RecoveryRecords[i]
		if record.Task == pending.Task && (record.RequestID != "" || s.RejectedResults[record.ActionID].Payload.Outcome == OutcomeRuntimeError) {
			input.Interruption = &record
			break
		}
	}
	if role == "engine.merge.resolve" {
		input.Conflict = s.conflictInput(pending.Task.Scope)
	}
	if role == "engine.gate" {
		input.Gate = s.gateInput(binding, pending.Task)
	}
	if role == "engine.development.batch" {
		input.Development = s.developmentInput(pending.Task, cd)
	}
	c := s.coverageContract(s.Validation.Candidate)
	switch role {
	case "engine.qa.design", "engine.qa.review.rationale":
		contract := modeCoverageContract(c, mode)
		contract.Candidate = nil
		input.Contract = &contract
	case "engine.repair":
		input.Contract = &c
		input.Repair = s.repairInput(pending.Task)
		if work := s.repairMember(pending.Task); work != nil {
			input.Development = s.repairDevelopmentInput(*work)
		}
	}
	_, input.SourcePoints = s.coveragePoints()
	input.Manifest.Binding = coverage.ManifestBinding{SourceBinding: c.RequiredSources.Binding, ReviewKind: mode, RouteScope: s.validationRouteDigest(), TopologyScope: digestOfCanonical(s.Topology)}
	for _, src := range c.RequiredSources.Sources {
		if src.Applicability == coverage.QAApplicability {
			input.Manifest.Sources = append(input.Manifest.Sources, src.SourceID)
		}
	}
	input.Manifest.Points, _ = s.coveragePoints()
	if set := s.Validation.Sets[mode]; set != nil {
		input.Manifest = set.Manifest
		if role == "engine.qa.review.rationale" {
			input.ReviewScope = coverage.ScopeFull
			for _, prior := range set.ReviewHistory {
				if prior.Review.Scope == coverage.ScopeFull && prior.Manifest.Binding.ReviewKind == mode {
					input.ReviewScope = coverage.ScopeAffected
					break
				}
			}
		}
		input.InheritableReviews = s.inheritableReviews(set)
		input.InheritableSharedReviews = inheritableSharedReviews(set)
	}
	if role == "engine.qa.design" || role == "engine.qa.review.rationale" {
		input.Manifest.Binding.RequirementRevision = binding.RequirementRevision
		input.Contract.RequiredSources.Binding.RequirementRevision = binding.RequirementRevision
		if binding.RouteScope != "" {
			input.Manifest = manifestAtRoute(input.Manifest, binding.RouteScope)
		}
		for i := range input.Contract.Manifests {
			input.Contract.Manifests[i].Binding.RequirementRevision = binding.RequirementRevision
			if binding.RouteScope != "" {
				input.Contract.Manifests[i] = manifestAtRoute(input.Contract.Manifests[i], binding.RouteScope)
			}
		}
	}
	if role == "engine.qa.execution" {
		wave := s.validationWave(binding.WaveID)
		if wave == nil {
			return input
		}
		contract, whitelist, err := s.executionContract(mode, wave)
		if err == nil {
			input.Contract = &contract
			input.Whitelist = &whitelist
			input.Execution = &coverage.ExecutionBinding{RequiredSourcesDigest: whitelist.RequiredSourcesDigest, ManifestDigest: whitelist.ManifestDigest, MapDigest: whitelist.MapDigest, WhitelistDigest: whitelist.Digest, Candidate: wave.Candidate, Scope: wave.Scope}
			input.Execution.ExpectedCaseIDs = executionCaseIDs(whitelist)
			if target := wave.Targets[pending.Task.String()]; target != nil {
				input.RequiredCases = slices.Clone(requiredExecutionCases(wave, target, whitelist))
				input.Inherited, _ = s.executionInheritance(wave, mode, whitelist, input.RequiredCases, target.ScopeReason)
				if target.Execution != nil {
					input.Completed = target.Execution.Records
				}
			}
			for _, edge := range whitelist.Entries {
				complete := slices.ContainsFunc(input.Completed, func(record coverage.ExecutionRecord) bool {
					return record.ReviewKind == edge.ReviewKind && record.SourceID == edge.SourceID && record.PointID == edge.PointID && record.CaseID == edge.CaseID
				})
				if !complete && !slices.Contains(input.PendingCases, edge.CaseID) {
					input.PendingCases = append(input.PendingCases, edge.CaseID)
				}
			}
			slices.Sort(input.PendingCases)
		}
	}
	return input
}
