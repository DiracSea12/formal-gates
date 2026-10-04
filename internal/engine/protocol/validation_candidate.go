package protocol

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

func nativeOperation(operation authoring.OperationID) bool {
	return operation == "op.candidate" || operation == "op.workspace" || operation == "op.cleanup"
}

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
	case "PROMOTION":
		if p.Promotion != nil {
			key.Promotion = &PromotionParameters{TargetRoot: p.Promotion.TargetRoot, TargetBase: p.Promotion.TargetBase, SourceDigest: p.Promotion.SourceDigest}
		}
	case "DEVELOPMENT":
		key.SourceTasks, key.Paths, key.Base = p.SourceTasks, p.Paths, p.Base
	case "AUTHORING":
		key.Base = p.Base
	case "QA_ARTIFACT":
		key.Base, key.Paths = p.Base, p.Paths
		key.DesignTasks = map[coverage.ReviewKind]runtime.TaskKey{coverage.ReviewWhitebox: p.DesignTasks[coverage.ReviewWhitebox]}
	case "REVIEW_READY":
		key.ReviewBase = p.ReviewBase
		key.BlackboxManifest = p.BlackboxManifest
		key.Base, key.Paths, key.DesignTasks, key.QAArtifacts, key.Finalize = p.Base, p.Paths, p.DesignTasks, p.QAArtifacts, p.Finalize
	default:
		key = p
	}
	return "candidate:" + digestOfCanonical(key)
}

func (s *State) latestNativeIntent(correlation string) HostActionIntent {
	var latest HostActionIntent
	for _, intent := range s.PendingHostActions {
		if intent.Correlation == correlation && intent.Revision > latest.Revision {
			latest = intent
		}
	}
	return latest
}

// nativeExecutionAllowed combines qualification with the original finite
// allowance. Observing completion does not consume another execution slot.
func (s *State) nativeExecutionAllowed(intent HostActionIntent, cd *compiler.CompiledDefinition) bool {
	if !s.NativeActionCurrent(intent, cd) || s.latestNativeIntent(intent.Correlation).ActionID != intent.ActionID {
		return false
	}
	if receipt, ok := s.HostActionReceipts[intent.ActionID]; ok && receipt.Status == HostActionStatusUnknown {
		return false
	}
	route, used, max := s.candidateFailureRoute(intent)
	return used < max && (route.Class == "" || route.Action == RecoveryResumeAttempt)
}

func (s *State) NativeActionWritable(intent HostActionIntent, cd *compiler.CompiledDefinition) bool {
	return len(s.HostActionFailures[intent.ActionID]) == 0 && s.nativeExecutionAllowed(intent, cd)
}

func (s *State) admitNativeSigning(intent HostActionIntent) error {
	route, used, max := s.candidateFailureRoute(intent)
	if used >= max || route.Class != "" && route.Action != RecoveryResumeAttempt {
		return validationError("native responsibility has no remaining execution allowance")
	}
	for _, old := range s.PendingHostActions {
		if old.Correlation != intent.Correlation {
			continue
		}
		if receipt, ok := s.HostActionReceipts[old.ActionID]; ok && receipt.Status == HostActionStatusUnknown {
			return validationError("native responsibility is UNKNOWN and requires observation")
		}
		if len(s.HostActionFailures[old.ActionID]) == 0 {
			return validationError("native responsibility already has an unresolved writer")
		}
	}
	return nil
}

func (s *State) candidateFailures(correlation string) []HostActionReceipt {
	var failures []HostActionReceipt
	for _, records := range s.HostActionFailures {
		for _, receipt := range records {
			if nativeOperation(receipt.AdapterOperation) && receipt.Correlation == correlation {
				failures = append(failures, receipt)
			}
		}
	}
	slices.SortStableFunc(failures, func(a, b HostActionReceipt) int {
		left, _ := s.hostIntent(a.ActionID)
		right, _ := s.hostIntent(b.ActionID)
		return cmp.Compare(left.Revision, right.Revision)
	})
	return failures
}

func (s *State) candidateRetryGrantTarget(correlation string) string {
	for request, d := range s.Decisions {
		ask := s.PendingAsks[request].Validation
		if ask == nil || ask.Kind != "RETRY" || d.Choice != "approve" {
			continue
		}
		for _, receipt := range s.HostActionFailures[ask.Target] {
			if nativeOperation(receipt.AdapterOperation) && receipt.Correlation == correlation {
				return ask.Target
			}
		}
	}
	return ""
}

func (s *State) candidateFailureRoute(intent HostActionIntent) (FailureRoute, int, int) {
	failures := s.candidateFailures(intent.Correlation)
	max := 2
	grant := s.candidateRetryGrantTarget(intent.Correlation)
	for i, failure := range failures {
		if failure.ActionID == grant {
			max = i + 2
			break
		}
	}
	if len(failures) == 0 {
		return FailureRoute{}, 0, max
	}
	route := RouteFailure(failures[len(failures)-1].FailureClass)
	if route.Class == authoring.FailureUserActionRequired && grant != "" && len(failures) < max {
		route.Action = RecoveryResumeAttempt
	}
	return route, len(failures), max
}

func (s *State) candidateInputsCurrent(intent HostActionIntent, cd *compiler.CompiledDefinition) bool {
	if intent.Adapter != nil && intent.Adapter.Operation == "op.workspace" {
		var p WorkspaceParameters
		raw, _ := json.Marshal(intent.Adapter.Params)
		if json.Unmarshal(raw, &p) != nil {
			return false
		}
		return s.workspaceParametersCurrent(p, cd)
	}
	if intent.Adapter != nil && intent.Adapter.Operation == "op.cleanup" {
		return !s.ResourcesCleaned() && s.CleanupEligible(cd)
	}
	p, err := candidateParameters(intent)
	if err != nil || s.Validation == nil {
		return false
	}
	if p.Stage == "PROMOTION" {
		if p.Promotion == nil || !s.FinalValidationComplete(cd) {
			return false
		}
		latest, ok := s.PromotionIntent()
		return (!ok || latest.ActionID == intent.ActionID) && p.Promotion.Source == s.currentValidationWave().Candidate.Identity && p.Promotion.SourceDigest == s.currentValidationWave().Candidate.Digest
	}
	current, ready, err := s.currentCandidateParameters(cd)
	return err == nil && ready && s.candidateContextCurrent(p) && candidateCorrelation(p) == candidateCorrelation(current) && p.WorkspaceID == current.WorkspaceID
}

// nativeEffectApplicable projects an observed effect through its original
// inputs. The next planner stage and its execution allowance own new writes.
func (s *State) nativeEffectApplicable(intent HostActionIntent, cd *compiler.CompiledDefinition) bool {
	if intent.Adapter == nil {
		return false
	}
	if effect := s.ReconciledEffects[intent.ActionID]; effect.Status == "SUPERSEDED" || effect.Status == "WITHDRAWN" {
		return false
	}
	switch intent.Adapter.Operation {
	case "op.workspace":
		var p WorkspaceParameters
		raw, err := json.Marshal(intent.Adapter.Params)
		if err != nil || json.Unmarshal(raw, &p) != nil || !s.resourceUsable(p.Resource.ID) {
			return false
		}
		r := s.GitResources[p.Resource.ID]
		return r.Owner == p.Resource.Owner && r.Path == p.Resource.Path && r.Ref == p.Resource.Ref && r.Source == p.Resource.Source
	case "op.cleanup":
		return s.cleanupStarted(cd)
	case "op.candidate":
		p, err := candidateParameters(intent)
		if err != nil {
			return false
		}
		if p.Stage == "PROMOTION" {
			return s.promotionAuthorized(p)
		}
		return s.candidateEffectApplicable(p, cd)
	}
	return false
}

func (s *State) candidateEffectApplicable(p CandidateParameters, cd *compiler.CompiledDefinition) bool {
	v := s.Validation
	if s.abortRequested() || !s.candidateContextCurrent(p) || p.WorkspaceID != "" && !s.resourceUsable(p.WorkspaceID) {
		return false
	}
	if p.Stage == "DEVELOPMENT" {
		sources, paths, base, complete := s.developmentSources(cd)
		if s.NativeRoot != "" {
			base = s.GitResources[p.WorkspaceID].Source
		}
		return complete && sameTaskSet(p.SourceTasks, sources) && slices.Equal(p.Paths, paths) && p.Base == base
	}
	if p.Base != v.DevelopmentSnapshot {
		return false
	}
	switch p.Stage {
	case "AUTHORING":
		return true
	case "QA_ARTIFACT":
		set := v.Sets[coverage.ReviewWhitebox]
		return set != nil && set.Status == "REVIEW" && s.candidateManifestCurrent(p, coverage.ReviewWhitebox)
	case "REVIEW_READY":
		if !p.Finalize || p.ReviewBase != s.BaseSnapshot {
			return false
		}
		sources, paths, _, complete := s.developmentSources(cd)
		if !complete || !sameTaskSet(p.SourceTasks, sources) {
			return false
		}
		paths = slices.Clone(paths)
		for _, mode := range s.validationKinds() {
			if !s.caseSetReady(mode) || !s.candidateManifestCurrent(p, mode) {
				return false
			}
			set := v.Sets[mode]
			if len(set.Paths) > 0 {
				artifact := p.QAArtifacts[mode]
				if artifact.Identity != set.Candidate || !slices.Equal(artifact.Paths, set.Paths) {
					return false
				}
				for _, path := range set.Paths {
					if !slices.Contains(paths, path) {
						paths = append(paths, path)
					}
				}
			}
		}
		if p.BlackboxManifest != nil {
			paths = append(paths, ".gates/results/"+s.RunID+".blackbox-cases.md")
		}
		slices.Sort(paths)
		return slices.Equal(p.Paths, paths) && s.validateFinalCoverage() == nil
	}
	return false
}

// candidateManifestCurrent verifies all content against the signed version.
// Carried context comes from retained manifests; a missing witness stays audit-only.
func (s *State) candidateManifestCurrent(p CandidateParameters, mode coverage.ReviewKind) bool {
	set := s.Validation.Sets[mode]
	if set == nil || set.DesignTask != p.DesignTasks[mode] {
		return false
	}
	if p.Generation == s.Validation.Generation {
		return digestOfCanonical(manifestAtRoute(set.Manifest, p.RouteScope)) == p.CaseVersions[mode]
	}
	matches := func(original coverage.AcceptanceManifest) bool {
		if original.Binding.ReviewKind != mode {
			return false
		}
		original = manifestAtRoute(original, p.RouteScope)
		original.Binding.RequirementRevision = p.RequirementRevision
		if digestOfCanonical(original) != p.CaseVersions[mode] {
			return false
		}
		current := set.Manifest
		current.Binding = original.Binding
		return digestOfCanonical(current) == p.CaseVersions[mode]
	}
	if mode == coverage.ReviewBlackbox && p.BlackboxManifest != nil && matches(*p.BlackboxManifest) {
		return true
	}
	for action, record := range s.Results {
		if record.Validation != nil && record.Validation.Binding == s.DispatchBindings[action] && record.Validation.Binding.Task == p.DesignTasks[mode] && record.Validation.Manifest != nil && matches(*record.Validation.Manifest) {
			return true
		}
	}
	for _, prior := range set.ReviewHistory {
		if matches(prior.Manifest) {
			return true
		}
	}
	for _, wave := range s.Validation.Waves {
		if wave.Generation != p.Generation || wave.RequirementRevision != p.RequirementRevision {
			continue
		}
		for _, manifest := range wave.Coverage.Manifests {
			if matches(manifest) {
				return true
			}
		}
	}
	return false
}

// candidateContextCurrent retains only explicitly carried source duties.
// Output equality alone cannot authorize work from another requirement occurrence.
func (s *State) candidateContextCurrent(p CandidateParameters) bool {
	if s.Validation == nil {
		return false
	}
	if p.Generation == s.Validation.Generation {
		return p.RequirementRevision == s.Validation.RequirementRevision
	}
	if len(p.SourceTasks) == 0 {
		return false
	}
	for _, task := range p.SourceTasks {
		if !s.validationWorkCarryAuthorized("", task) {
			return false
		}
	}
	for _, task := range p.DesignTasks {
		if !s.validationWorkCarryAuthorized("", task) {
			return false
		}
	}
	return true
}

func (e *Engine) advanceCandidateFailures(s *State, nextRevision uint64) (bool, error) {
	changed := false
	ids := make([]string, 0, len(s.PendingHostActions))
	for action := range s.PendingHostActions {
		ids = append(ids, action)
	}
	slices.Sort(ids)
	for _, action := range ids {
		intent := s.PendingHostActions[action]
		if intent.Adapter == nil || !nativeOperation(intent.Adapter.Operation) {
			continue
		}
		route, used, max := s.candidateFailureRoute(intent)
		if route.Class == "" {
			continue
		}
		if receipt, ok := s.HostActionReceipts[action]; ok && (receipt.Status == HostActionStatusUnknown || receipt.Status == HostActionStatusExecuted) {
			continue
		}
		if s.latestNativeIntent(intent.Correlation).ActionID != action || !s.NativeActionCurrent(intent, e.cfg.Definition) {
			continue
		}
		if route.Action == RecoveryAsk || route.Action == RecoveryResumeAttempt && used >= max {
			changed = s.ensureValidationAsk(ValidationRequest{Kind: "RETRY", Target: action}, nextRevision) || changed
		} else if route.Action == RecoveryResumeAttempt && len(s.HostActionFailures[action]) != 0 {
			params := any(intent.Adapter.Params)
			if intent.Adapter.Operation == "op.candidate" {
				p, err := candidateParameters(intent)
				if err != nil {
					return changed, err
				}
				if p.Promotion == nil {
					current, ready, err := s.currentCandidateParameters(e.cfg.Definition)
					if err != nil || !ready {
						return changed, err
					}
					params = current
				} else {
					p.Promotion.SupersedesActionID = intent.ActionID
					params = p
				}
			}
			next, err := e.prepareAdapterIntent(s, nextRevision, intent.Adapter.Operation, params, "")
			if err != nil {
				return changed, err
			}
			s.PendingHostActions[next.ActionID] = next
			return true, nil
		}
	}
	return changed, nil
}

// ExecutableHostActions exposes only operations whose original effect has not
// failed or become unknown. Pending reconciliation remains in State.
func (s *State) ExecutableHostActions() map[string]HostActionIntent {
	result := map[string]HostActionIntent{}
	for id, intent := range s.PendingHostActions {
		if intent.Adapter != nil && nativeOperation(intent.Adapter.Operation) {
			if params, err := candidateParameters(intent); err == nil && params.Promotion != nil {
				latest, _ := s.PromotionIntent()
				if latest.ActionID != id {
					continue
				}
			}
			if s.latestNativeIntent(intent.Correlation).ActionID != id {
				continue
			}
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

func (s *State) candidateFailureFact(cd *compiler.CompiledDefinition) *decision.Fact {
	ids := make([]string, 0, len(s.PendingHostActions))
	for id := range s.PendingHostActions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		intent := s.PendingHostActions[id]
		if intent.Adapter == nil || !nativeOperation(intent.Adapter.Operation) || !s.NativeActionCurrent(intent, cd) || s.latestNativeIntent(intent.Correlation).ActionID != id {
			continue
		}
		if receipt, ok := s.HostActionReceipts[id]; ok && receipt.Status == HostActionStatusUnknown {
			return &decision.Fact{Source: decision.SourceReceipt, Key: id, Value: "native effect is UNKNOWN; observe and submit its typed completion evidence"}
		}
		route, used, max := s.candidateFailureRoute(intent)
		if route.Class != "" {
			return &decision.Fact{Source: decision.SourceReceipt, Key: id, Value: fmt.Sprintf("%s; native attempts %d/%d", route.ActionDetail(), used, max)}
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
		params.CaseVersions = map[coverage.ReviewKind]string{}
		params.DesignTasks = map[coverage.ReviewKind]runtime.TaskKey{}
		for mode, set := range v.Sets {
			if params.Stage == "QA_ARTIFACT" && mode != coverage.ReviewWhitebox {
				continue
			}
			params.CaseVersions[mode] = set.Version
			params.DesignTasks[mode] = set.DesignTask
		}
	}
	if s.NativeRoot != "" && params.Stage == "DEVELOPMENT" {
		params.Base = s.candidateResource(params, cd).Source
	}
	if s.NativeRoot != "" && params.Stage == "REVIEW_READY" {
		if set := v.Sets[coverage.ReviewBlackbox]; set != nil && set.Status == "FROZEN" {
			manifest := set.Manifest
			params.BlackboxManifest = &manifest
			params.Paths = append(params.Paths, ".gates/results/"+s.RunID+".blackbox-cases.md")
			slices.Sort(params.Paths)
		}
	}
	if s.NativeRoot != "" {
		r := s.candidateResource(params, cd)
		params.WorkspaceID = r.ID
	}
	return params, true, nil
}

func (s *State) NativeActionCurrent(intent HostActionIntent, cd *compiler.CompiledDefinition) bool {
	if s.requirementChangePending() {
		return false
	}
	if s.abortRequested() && (intent.Adapter == nil || intent.Adapter.Operation != "op.cleanup") {
		return false
	}
	return s.candidateInputsCurrent(intent, cd)
}
