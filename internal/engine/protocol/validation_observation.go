package protocol

import (
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

// ObservationInput projects the current finite semantic duties from their
// finding, target and original-result owners. It is not another workflow ledger.
type ObservationInput struct {
	WaveID      string                       `json:"waveId"`
	Candidate   coverage.ValidationCandidate `json:"candidate"`
	Findings    []ObservationFinding         `json:"findings,omitempty"`
	Problems    []ObservationProblem         `json:"problems,omitempty"`
	Scopes      []ObservationScope           `json:"scopes,omitempty"`
	GateImpacts []ObservationGateImpact      `json:"gateImpacts,omitempty"`
	Suggestions []ObservationSuggestion      `json:"suggestions,omitempty"`
}
type ObservationProblem struct {
	ValidationProblem
	Findings []ObservationFinding `json:"findings"`
}
type ObservationSuggestion struct {
	Mode       coverage.ReviewKind         `json:"mode"`
	Suggestion QASuggestion                `json:"suggestion"`
	Manifest   coverage.AcceptanceManifest `json:"manifest"`
	Review     coverage.ReviewResult       `json:"review"`
	Workspace  *NativeResource             `json:"workspace,omitempty"`
}
type ObservationFinding struct {
	Finding     ValidationFinding          `json:"finding"`
	Target      ValidationTarget           `json:"target"`
	Evidence    WorkerResult               `json:"evidence"`
	FailedCases []coverage.ExecutionRecord `json:"failedCases,omitempty"`
}
type ObservationScope struct {
	Mode              coverage.ReviewKind           `json:"mode"`
	Manifest          coverage.AcceptanceManifest   `json:"manifest"`
	ApprovedCases     []string                      `json:"approvedCases"`
	MustExecute       []string                      `json:"mustExecute"`
	Baseline          *ValidationTarget             `json:"baseline,omitempty"`
	BaselineCandidate *coverage.ValidationCandidate `json:"baselineCandidate,omitempty"`
}
type ObservationGateImpact struct {
	Gate     string                       `json:"gate"`
	Range    *GateInput                   `json:"range"`
	Previous coverage.ValidationCandidate `json:"previous"`
	Evidence WorkerResult                 `json:"evidence"`
}

func (s *State) validationObservationInput() *ObservationInput {
	if s.Validation == nil || s.requirementChangePending() || s.abortRequested() {
		return nil
	}
	suggestions := &ObservationInput{}
	approved := true
	for _, mode := range s.validationKinds() {
		if s.caseSetWaiver(mode) != "" {
			continue
		}
		set := s.Validation.Sets[mode]
		if set == nil || set.Review == nil || set.Review.Verdict() != coverage.StatusPass {
			approved = false
			break
		}
		for _, suggestion := range set.Suggestions {
			if suggestion.Decision == "" {
				suggestions.Suggestions = append(suggestions.Suggestions, ObservationSuggestion{Mode: mode, Suggestion: suggestion, Manifest: set.Manifest, Review: *set.Review})
			}
		}
	}
	if approved && len(suggestions.Suggestions) > 0 {
		return suggestions
	}
	wave := s.currentValidationWave()
	if wave == nil {
		return nil
	}
	if wave.Generation != s.Validation.Generation || wave.RouteDigest != s.validationRouteDigest() {
		return nil
	}
	input := &ObservationInput{WaveID: wave.ID, Candidate: wave.Candidate}
	if wave.Status == "ADJUDICATING" || wave.Status == "REPAIR" || wave.Status == "REPAIRED" || wave.Status == "COMPLETE" {
		ids := []string{}
		for id, f := range s.Validation.Findings {
			if s.validationFindingTarget(wave, f) != nil && len(f.History) == 0 {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			f := s.Validation.Findings[id]
			input.Findings = append(input.Findings, s.observationFinding(f))
		}
		if len(input.Findings) > 0 {
			ids = ids[:0]
			for id := range s.Validation.Problems {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			for _, id := range ids {
				problem := ObservationProblem{ValidationProblem: *s.Validation.Problems[id]}
				for _, findingID := range problem.FindingIDs {
					problem.Findings = append(problem.Findings, s.observationFinding(s.Validation.Findings[findingID]))
				}
				input.Problems = append(input.Problems, problem)
			}
		}
	}
	if wave.Status == "COLLECTING" {
		for _, key := range sortedTargetKeys(wave) {
			target := wave.Targets[key]
			if target.Kind == "QA" && wave.Scope == coverage.ScopeAffected && target.Status == "PENDING" && len(target.RequiredCases) == 0 {
				contract, whitelist, err := s.executionContract(target.Mode, wave)
				if err != nil {
					continue
				}
				value := ObservationScope{Mode: target.Mode, ApprovedCases: executionCaseIDs(whitelist), MustExecute: []string{}}
				for _, manifest := range contract.Manifests {
					if manifest.Binding.ReviewKind == target.Mode {
						value.Manifest = manifest
						break
					}
				}
				if prior, baseline := s.executionBaseline(wave, target.Mode); prior != nil {
					value.Baseline = baseline
					candidate := prior.Candidate
					value.BaselineCandidate = &candidate
				}
				for _, edge := range whitelist.Entries {
					if _, err := s.inheritedExecutionRecord(wave, target.Mode, edge, "", ""); err != nil && !slices.Contains(value.MustExecute, edge.CaseID) {
						value.MustExecute = append(value.MustExecute, edge.CaseID)
					}
				}
				slices.Sort(value.MustExecute)
				input.Scopes = append(input.Scopes, value)
			}
			if target.Kind == "GATE" && target.GateImpact != nil && target.GateImpact.Decision == "" {
				prior := s.validationWave(target.GateImpact.SourceWave)
				if prior == nil {
					continue
				}
				for _, source := range prior.Targets {
					if source.Gate == target.Gate && source.Status == "PASS" {
						binding := WorkBinding{WaveID: wave.ID, Candidate: wave.Candidate.Identity}
						input.GateImpacts = append(input.GateImpacts, ObservationGateImpact{Gate: target.Gate, Range: s.gateInput(binding, target.Task), Previous: prior.Candidate, Evidence: s.Results[source.ResultAction]})
						break
					}
				}
			}
		}
	}
	if len(input.Findings)+len(input.Scopes)+len(input.GateImpacts) == 0 {
		return nil
	}
	return input
}

func (s *State) observationFinding(f *ValidationFinding) ObservationFinding {
	target := s.validationFindingTarget(s.validationWave(f.WaveID), f)
	result, current := s.Results[target.ResultAction]
	if !current {
		for _, original := range s.ObsoleteResults[target.ResultAction] {
			if original.ActionID == target.ResultAction && original.Outcome == OutcomePass && original.Validation != nil && !original.Validation.Checkpoint && original.Validation.Binding.WaveID == f.WaveID && original.Validation.Binding.Task.String() == f.Target {
				result = original
				break
			}
		}
	}
	return ObservationFinding{Finding: *f, Target: *target, Evidence: result, FailedCases: s.executionFailureRecords(f)}
}

func observationTask(cd *compiler.CompiledDefinition, input *ObservationInput) runtime.TaskKey {
	return pipelineTask(cd, "engine.validation.observation.v2", input.WaveID+":"+strings.TrimPrefix(digestOfCanonical(input), "sha256:"))
}

func (e *Engine) acceptWorkerObservations(s *State, record WorkerResult, nextRevision uint64) error {
	input := s.validationObservationInput()
	if input == nil || s.sourceValidationTask(record.Validation.Binding.Task) != observationTask(e.cfg.Definition, input) {
		return validationError("semantic observation differs from the current frozen duties")
	}
	expected := len(input.Scopes) + len(input.GateImpacts) + len(input.Suggestions)
	if len(input.Findings) > 0 {
		expected++
	}
	observations := record.Validation.Observations
	if len(observations) != expected {
		return validationError("semantic result must cover exactly the issued observation duties")
	}
	seen := map[string]bool{}
	for _, payload := range observations {
		o := payload.Validation
		if payload.Subject != input.WaveID || o == nil {
			return validationError("semantic observation differs from its issued wave")
		}
		key := o.Kind
		switch o.Kind {
		case "ADJUDICATE":
			if len(o.Findings) != len(input.Findings) || len(input.Findings) == 0 {
				return validationError("adjudication must cover exactly the issued findings")
			}
			ids := map[string]bool{}
			for _, item := range o.Findings {
				if ids[item.FindingID] || !slices.ContainsFunc(input.Findings, func(f ObservationFinding) bool { return f.Finding.ID == item.FindingID }) {
					return validationError("adjudication contains an unissued or duplicate finding")
				}
				ids[item.FindingID] = true
			}
		case "EXECUTION_SCOPE":
			key += ":" + string(o.Mode)
			if !slices.ContainsFunc(input.Scopes, func(v ObservationScope) bool { return v.Mode == o.Mode }) {
				return validationError("execution scope was not issued")
			}
		case "GATE_IMPACT":
			key += ":" + o.Gate
			if !slices.ContainsFunc(input.GateImpacts, func(v ObservationGateImpact) bool { return v.Gate == o.Gate }) {
				return validationError("gate impact was not issued")
			}
		case "SUGGESTION":
			key += ":" + string(o.Mode) + ":" + o.SuggestionID
			if !slices.ContainsFunc(input.Suggestions, func(v ObservationSuggestion) bool { return v.Mode == o.Mode && v.Suggestion.ID == o.SuggestionID }) {
				return validationError("QA suggestion was not issued")
			}
		default:
			return validationError("semantic worker cannot authorize this observation kind")
		}
		if seen[key] {
			return validationError("semantic duty is duplicated")
		}
		seen[key] = true
	}
	// Freshness is checked once for the whole package. Each original validator
	// mutates its own domain; all mutations are accepted in the same transaction.
	for i := range observations {
		if err := e.acceptValidationObservation(s, &observations[i], nextRevision-1, nextRevision); err != nil {
			return err
		}
	}
	return nil
}

// NextWorkflowChoice names only a current user decision. Semantic evidence is
// handled by issued workers, while topology and route stay explicit typed Asks.
func (s *State) NextWorkflowChoice(cd *compiler.CompiledDefinition) ControlKind {
	if !pipelineEnabled(s, cd) || !s.PreDevelopmentReviewsSatisfied() || !s.readinessPreparationReady() || s.hasPendingAsks() || s.abortRequested() || s.resetPending() {
		return ""
	}
	if s.SliceChild == nil && s.Topology == nil {
		return ControlConfirmTopology
	}
	if s.RouteMode == "" {
		return ControlSelectRoute
	}
	if s.SliceChild == nil && !s.mergeMaster() && s.FlowNodeEnabled("development") {
		if _, p, _ := s.confirmedDevelopmentPlan(); p == nil || !s.developmentPlanCurrent(p) {
			return ControlSelectRoute
		}
	}
	return ""
}

// FindingProblemID is the original adjudication owner's deterministic NEW identity.
func FindingProblemID(f ValidationFinding) string {
	return digestOfCanonical(struct {
		Finding  string
		Revision int
	}{f.ID, len(f.History) + 1})
}
