package protocol

import (
	"encoding/json"
	"fmt"
	"slices"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

func candidateParameters(intent HostActionIntent) (CandidateParameters, error) {
	var p CandidateParameters
	if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
		return p, validationError("intent is not a candidate operation")
	}
	raw, err := json.Marshal(intent.Adapter.Params)
	if err == nil {
		err = json.Unmarshal(raw, &p)
	}
	return p, err
}

// Candidate correlation identifies the semantic effect. Context that cannot
// change this stage's output cannot reset its finite execution allowance.
func candidateCorrelation(p CandidateParameters) string {
	key := CandidateParameters{Stage: p.Stage}
	switch p.Stage {
	case "DEVELOPMENT":
		key.SourceTasks, key.Paths, key.Base = p.SourceTasks, p.Paths, p.Base
	case "AUTHORING":
		key.Base = p.Base
	case "QA_ARTIFACT":
		key.Base, key.Paths = p.Base, p.Paths
		key.DesignTasks = map[coverage.ReviewKind]runtime.TaskKey{coverage.ReviewWhitebox: p.DesignTasks[coverage.ReviewWhitebox]}
	case "REVIEW_READY":
		key.ReviewBase = p.ReviewBase
		key.Base, key.Paths, key.DesignTasks, key.QAArtifacts, key.Finalize = p.Base, p.Paths, p.DesignTasks, p.QAArtifacts, p.Finalize
	default:
		key = p
	}
	return "candidate:" + digestOfCanonical(key)
}

func (s *State) candidateFailures(correlation string) []HostActionReceipt {
	var failures []HostActionReceipt
	for _, records := range s.HostActionFailures {
		for _, receipt := range records {
			if receipt.AdapterOperation == "op.candidate" && receipt.Correlation == correlation {
				failures = append(failures, receipt)
			}
		}
	}
	slices.SortFunc(failures, func(a, b HostActionReceipt) int {
		if a.ActionID < b.ActionID {
			return -1
		}
		if a.ActionID > b.ActionID {
			return 1
		}
		return 0
	})
	return failures
}

func (s *State) candidateRetryGranted(correlation string) bool {
	for request, d := range s.Decisions {
		ask := s.PendingAsks[request].Validation
		if ask == nil || ask.Kind != "RETRY" || d.Choice != "approve" {
			continue
		}
		for _, receipt := range s.HostActionFailures[ask.Target] {
			if receipt.AdapterOperation == "op.candidate" && receipt.Correlation == correlation {
				return true
			}
		}
	}
	return false
}

func (s *State) candidateFailureRoute(intent HostActionIntent) (FailureRoute, int, int) {
	failures := s.candidateFailures(intent.Correlation)
	max := 2
	if s.candidateRetryGranted(intent.Correlation) {
		max++
	}
	latest := s.HostActionFailures[intent.ActionID]
	if len(latest) == 0 {
		return FailureRoute{}, len(failures), max
	}
	return RouteFailure(latest[len(latest)-1].FailureClass), len(failures), max
}

func (s *State) candidateInputsCurrent(intent HostActionIntent, cd *compiler.CompiledDefinition) bool {
	p, err := candidateParameters(intent)
	if err != nil || s.Validation == nil {
		return false
	}
	current, ready, err := s.currentCandidateParameters(cd)
	return err == nil && ready && candidateCorrelation(p) == candidateCorrelation(current)
}

func (e *Engine) advanceCandidateFailures(s *State, nextRevision uint64) bool {
	changed := false
	for action, intent := range s.PendingHostActions {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
			continue
		}
		route, used, max := s.candidateFailureRoute(intent)
		if route.Class == "" {
			continue
		}
		if receipt, ok := s.HostActionReceipts[action]; ok && receipt.Status == HostActionStatusUnknown {
			continue
		}
		terminalBug := route.Class == authoring.FailureBlockedBug || route.Class == authoring.FailureInvariantViolation
		if !terminalBug && (!s.candidateInputsCurrent(intent, e.cfg.Definition) || s.abortRequested()) {
			delete(s.PendingHostActions, action)
			changed = true
			continue
		}
		if route.Action == RecoveryResumeAttempt {
			if used < max {
				delete(s.PendingHostActions, action)
				changed = true
			} else {
				changed = s.ensureValidationAsk(ValidationRequest{Kind: "RETRY", Target: action}, nextRevision) || changed
			}
		}
	}
	return changed
}

// ExecutableHostActions exposes only operations whose original effect has not
// failed or become unknown. Pending reconciliation remains in State.
func (s *State) ExecutableHostActions() map[string]HostActionIntent {
	result := map[string]HostActionIntent{}
	for id, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" {
			if receipt, ok := s.HostActionReceipts[id]; ok && receipt.Status == HostActionStatusUnknown {
				continue
			}
			if len(s.HostActionFailures[id]) > 0 {
				continue
			}
		}
		result[id] = intent
	}
	return result
}

func (s *State) candidateFailureFact() *decision.Fact {
	ids := make([]string, 0, len(s.PendingHostActions))
	for id := range s.PendingHostActions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		intent := s.PendingHostActions[id]
		if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
			continue
		}
		if receipt, ok := s.HostActionReceipts[id]; ok && receipt.Status == HostActionStatusUnknown {
			return &decision.Fact{Source: decision.SourceReceipt, Key: id, Value: "candidate effect is UNKNOWN; observe and submit its typed completion evidence"}
		}
		route, used, max := s.candidateFailureRoute(intent)
		if route.Class != "" {
			return &decision.Fact{Source: decision.SourceReceipt, Key: id, Value: fmt.Sprintf("%s; candidate attempts %d/%d", route.ActionDetail(), used, max)}
		}
	}
	return nil
}

// currentCandidateParameters is the single source of candidate effect inputs
// for controller planning and explicit adapter execution.
func (s *State) currentCandidateParameters(cd *compiler.CompiledDefinition) (CandidateParameters, bool, error) {
	v := s.Validation
	if v == nil || s.abortRequested() {
		return CandidateParameters{}, false, nil
	}
	sources, paths, base, complete := s.developmentSources(cd)
	if !complete {
		return CandidateParameters{}, false, nil
	}
	params := CandidateParameters{RouteScope: s.validationRouteDigest(), RequirementRevision: v.RequirementRevision, Generation: v.Generation, Paths: paths, SourceTasks: sources}
	switch {
	case v.DevelopmentSnapshot == "":
		params.Stage = "DEVELOPMENT"
		params.Base = base
	case s.nextRepairWork() != nil:
		return CandidateParameters{}, false, nil
	case slices.Contains(s.validationKinds(), coverage.ReviewWhitebox) && v.Sets[coverage.ReviewWhitebox] == nil && v.AuthoringWorkspace == "":
		params.Stage = "AUTHORING"
		params.Base = v.DevelopmentSnapshot
	default:
		set := v.Sets[coverage.ReviewWhitebox]

		if set != nil && set.Status == "REVIEW" && set.Candidate == "" {
			params.Stage = "QA_ARTIFACT"
			params.Base = v.DevelopmentSnapshot
			params.Paths = slices.Clone(set.Paths)
		} else {
			if v.Candidate != nil {
				return CandidateParameters{}, false, nil
			}
			for _, mode := range s.validationKinds() {
				if !s.caseSetReady(mode) {
					return CandidateParameters{}, false, nil
				}
			}
			if err := s.validateFinalCoverage(); err != nil {
				return CandidateParameters{}, false, nil
			}
			params.Stage = "REVIEW_READY"
			params.ReviewBase = s.BaseSnapshot
			params.QAArtifacts = map[coverage.ReviewKind]CandidateArtifact{}
			for mode, artifact := range v.Sets {
				if len(artifact.Paths) > 0 {
					if artifact.Candidate == "" {
						return CandidateParameters{}, false, validationError("approved QA artifact has no immutable candidate")
					}
					params.QAArtifacts[mode] = CandidateArtifact{Identity: artifact.Candidate, Paths: slices.Clone(artifact.Paths)}
					for _, path := range artifact.Paths {
						if !slices.Contains(params.Paths, path) {
							params.Paths = append(params.Paths, path)
						}
					}
				}
			}
			slices.Sort(params.Paths)
			params.Base = v.DevelopmentSnapshot
			params.Finalize = true
		}
	}
	if params.Stage == "QA_ARTIFACT" || params.Stage == "REVIEW_READY" {
		if set := v.Sets[coverage.ReviewWhitebox]; set != nil {
			params.ReviewCases = map[string]string{}
			for _, c := range set.Manifest.Cases {
				params.ReviewCases[c.CaseID] = c.TestRef
			}
		}
	}
	params.CaseVersions = map[coverage.ReviewKind]string{}
	params.DesignTasks = map[coverage.ReviewKind]runtime.TaskKey{}
	for mode, set := range v.Sets {
		params.CaseVersions[mode] = set.Version
		params.DesignTasks[mode] = set.DesignTask
	}
	return params, true, nil
}
