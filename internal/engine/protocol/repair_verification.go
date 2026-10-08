package protocol

import (
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

// A comparison is a projection of its source receipts and original edit
// results. Native command receipts own execution; Results own judgments.
type repairComparison struct {
	binding         WorkBinding
	resource        NativeResource
	source          CandidateEvidence
	originals       []WorkerResult
	wave            *ValidationWave
	paths, affected []string
	final           bool
}

func (s *State) repairComparisonResult(task runtime.TaskKey) *WorkerResult {
	for id, r := range s.Results {
		if _, obsolete := s.ObsoleteActions[id]; !obsolete && r.Outcome == OutcomePass && r.Validation != nil && r.Validation.Binding.Task == task && r.Validation.Status == OutcomePass && r.Validation.Development != nil {
			copy := r
			return &copy
		}
	}
	return nil
}

func (s *State) repairComparisonContexts(cd *compiler.CompiledDefinition) []repairComparison {
	var contexts []repairComparison
	if s.Validation == nil {
		return contexts
	}
	for _, work := range s.repairWork() {

		r := s.developmentEditResult(work.task)
		if r == nil || !s.editResultCurrent(*r) || !r.Validation.Binding.PureP2 || !s.developmentChecksPassed(*r) {
			continue
		}
		proof, ready := s.editSourceProof(r.ActionID)
		if ready {
			contexts = append(contexts, s.newRepairComparison(work.wave, []WorkerResult{*r}, proof, false, cd))
		}
	}
	for i := range s.Validation.Waves {
		work := s.repairVerificationWork(&s.Validation.Waves[i], cd)
		if work == nil {
			continue
		}
		_, proof, ready := s.repairFinalSource(*work)
		if !ready {
			continue
		}
		var originals []WorkerResult
		for _, member := range s.repairMembers(work.wave) {
			if r := s.developmentResultRecord(member.task); r != nil {
				originals = append(originals, *r)
			}
		}
		contexts = append(contexts, s.newRepairComparison(work.wave, originals, proof, true, cd))
	}

	for action, binding := range s.DispatchBindings {
		if binding.VerificationSource == "" || s.validationRole(binding.Task, cd) != "engine.repair.verify" || !s.validationWorkCarryAuthorized(action, binding.Task) || slices.ContainsFunc(contexts, func(c repairComparison) bool { return c.binding.Task == binding.Task }) {
			continue
		}
		wave := s.validationWave(binding.WaveID)
		if wave == nil || wave.Repair == nil {
			continue
		}
		for receiptAction, receipt := range s.HostActionReceipts {
			if receipt.AdapterOperation != "op.check" || receipt.Status != HostActionStatusExecuted {
				continue
			}
			if effect := s.ReconciledEffects[receiptAction]; effect.Status == "SUPERSEDED" || effect.Status == "WITHDRAWN" {
				continue
			}
			intent, exists := s.hostIntent(receiptAction)
			if !exists || intent.Adapter == nil || intent.Adapter.Operation != "op.check" {
				continue
			}
			p, err := checkParameters(intent)
			if err != nil || p.Repair == nil || p.Repair.Task != binding.Task || p.Binding != binding || p.SourceIdentity != binding.Candidate {
				continue
			}
			resource := s.NativeResources[binding.WorkspaceID]
			if resource.ID != p.WorkspaceID || resource.Source != p.SourceIdentity {
				continue
			}
			c := repairComparison{binding: binding, resource: resource, source: CandidateEvidence{Identity: p.SourceIdentity, Digest: p.SourceDigest}, wave: wave}
			valid := len(p.Repair.SourceActions) > 0
			for _, id := range p.Repair.SourceActions {
				r, ok := s.frozenRepairEdit(id, wave.ID)
				if !ok {
					valid = false
					break
				}
				c.originals = append(c.originals, r)
				c.paths = append(c.paths, r.Validation.Development.Paths...)
				c.affected = append(c.affected, r.Validation.Development.AffectedCases...)
			}
			if !valid {
				continue
			}
			c.final = len(c.originals) > 1
			slices.Sort(c.paths)
			c.paths = slices.Compact(c.paths)
			slices.Sort(c.affected)
			c.affected = slices.Compact(c.affected)
			contexts = append(contexts, c)
			break
		}
	}

	return contexts
}

func (s *State) newRepairComparison(w *ValidationWave, originals []WorkerResult, source CandidateEvidence, final bool, cd *compiler.CompiledDefinition) repairComparison {
	context := repairComparison{originals: originals, source: source, wave: w, final: final}
	var actions []string
	for _, r := range originals {
		actions = append(actions, r.ActionID)
		context.paths = append(context.paths, r.Validation.Development.Paths...)
		context.affected = append(context.affected, r.Validation.Development.AffectedCases...)
	}
	slices.Sort(actions)
	slices.Sort(context.paths)
	context.paths = slices.Compact(context.paths)
	slices.Sort(context.affected)
	context.affected = slices.Compact(context.affected)
	scope := strings.TrimPrefix(digestOfCanonical(struct {
		Actions       []string
		Source, Cases string
		Final         bool
	}{actions, source.Digest, digestOfCanonical(context.affected), final}), "sha256:")
	task := s.replacementFold(pipelineTask(cd, "engine.repair.verify", scope))
	if final {
		if work := s.repairVerificationWork(w, cd); work != nil {
			task = work.task
		}
	}
	context.resource = s.resource("repair-verification", source.Identity, task.String())
	if registered, ok := s.NativeResources[context.resource.ID]; ok {
		context.resource = registered
	}
	context.binding = WorkBinding{Task: task, WaveID: w.ID, RequirementRevision: s.Validation.RequirementRevision, Generation: s.Validation.Generation, RouteScope: s.validationRouteDigest(), WorkspaceID: context.resource.ID, Candidate: source.Identity, PureP2: true}
	if !final {
		context.binding.VerificationSource = originals[0].ActionID
	} else if work := s.repairVerificationWork(w, cd); work != nil {
		intent, _, _ := s.repairFinalSource(*work)
		context.binding.VerificationSource = intent.ActionID
	}
	for action, binding := range s.DispatchBindings {
		if binding.Task == task && s.validationWorkCarryAuthorized(action, task) && binding.Candidate == source.Identity {
			context.binding = binding
			context.resource = s.NativeResources[binding.WorkspaceID]
			break
		}
	}
	return context
}

func (s *State) repairComparisonContext(task runtime.TaskKey, cd *compiler.CompiledDefinition) (repairComparison, bool) {
	for _, context := range s.repairComparisonContexts(cd) {
		if context.binding.Task == task {
			return context, true
		}
	}
	return repairComparison{}, false
}

func (s *State) repairComparisonInput(context repairComparison) *RepairInput {
	return &RepairInput{OriginTask: context.wave.Repair.Task, FinalCandidate: &coverage.ValidationCandidate{Identity: context.source.Identity, Digest: context.source.Digest}, MemberResults: slices.Clone(context.originals), VerificationPaths: slices.Clone(context.paths), VerificationCases: slices.Clone(context.affected), Obligation: ValidationRepair{Task: context.binding.Task, Problems: slices.Clone(context.wave.Repair.Problems), PureP2: true}}
}

func (s *State) repairComparisonPlans(context repairComparison, cd *compiler.CompiledDefinition) ([]CheckParameters, error) {
	if context.resource.Status != "READY" {
		return nil, nil
	}
	var plans []CheckParameters
	if context.final {
		for _, check := range s.fullProjectChecks() {
			plans = append(plans, CheckParameters{Repair: repairCheckSources(context), Binding: context.binding, Check: check, SourceIdentity: context.source.Identity, SourceDigest: context.source.Digest, WorkspaceID: context.resource.ID})
		}
	}
	for _, id := range context.affected {
		var manifest coverage.AcceptanceManifest
		var qaCase coverage.AcceptanceCase
		found := false
		for _, m := range context.wave.Coverage.Manifests {
			for _, item := range m.Cases {
				if item.CaseID == id {
					if found {
						return nil, validationError("repair case ID is ambiguous: " + id)
					}
					manifest, qaCase, found = m, item, true
				}
			}
		}
		if !found {
			return nil, validationError("repair case is outside the frozen approved set: " + id)
		}
		operations, err := caseOperationPlans(manifest, qaCase)
		if err != nil {
			return nil, err
		}
		for ordinal, operation := range operations {
			operation.CaseID, operation.Ordinal, operation.WorkspaceRoot = id, ordinal, context.resource.Path
			plans = append(plans, CheckParameters{Repair: repairCheckSources(context), QA: &operation, Binding: context.binding, SourceIdentity: context.source.Identity, SourceDigest: context.source.Digest, WorkspaceID: context.resource.ID})
		}
	}
	return plans, nil
}

func (s *State) repairPlanReceipt(plan CheckParameters, cd *compiler.CompiledDefinition) (ExecutionOperationFact, bool) {
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" || receipt.Status != HostActionStatusExecuted {
			continue
		}
		intent, ok := s.hostIntent(receipt.ActionID)
		if !ok {
			continue
		}
		if _, obsolete := s.ObsoleteActions[receipt.ActionID]; obsolete {
			continue
		}
		if effect := s.ReconciledEffects[receipt.ActionID]; effect.Status == "SUPERSEDED" || effect.Status == "WITHDRAWN" {
			continue
		}
		p, err := checkParameters(intent)
		if err != nil || digestOfCanonical(p) != digestOfCanonical(plan) || !s.NativeActionCurrent(intent, cd) || validateCheckReceipt(intent, receipt) != nil {
			continue
		}
		var evidence CheckEvidence
		if decodeAdapterValues(receipt, &evidence) == nil {
			return ExecutionOperationFact{ActionID: intent.ActionID, Parameters: p, Evidence: evidence}, true
		}
	}
	return ExecutionOperationFact{}, false
}

func (s *State) repairComparisonFacts(context repairComparison, cd *compiler.CompiledDefinition) []ExecutionFact {
	plans, err := s.repairComparisonPlans(context, cd)
	if err != nil {
		return nil
	}
	facts := map[string]*ExecutionFact{}
	for _, plan := range plans {
		actual, done := s.repairPlanReceipt(plan, cd)
		if !done || actual.Evidence.Error != "" {
			return nil
		}
		if plan.QA == nil {
			if actual.Evidence.ExitCode != 0 {
				return nil
			}
			continue
		}
		if !qaOperationExecuted(actual) {
			return nil
		}
		if facts[plan.QA.CaseID] == nil {
			facts[plan.QA.CaseID] = &ExecutionFact{CaseID: plan.QA.CaseID}
		}
		facts[plan.QA.CaseID].Operations = append(facts[plan.QA.CaseID].Operations, actual)
	}
	var result []ExecutionFact
	for _, id := range context.affected {
		if fact := facts[id]; fact != nil {
			result = append(result, *fact)
		}
	}
	return result
}

func (s *State) repairComparisonReady(context repairComparison, cd *compiler.CompiledDefinition) bool {
	plans, err := s.repairComparisonPlans(context, cd)
	if err != nil || context.resource.Status != "READY" {
		return false
	}
	for _, plan := range plans {
		fact, done := s.repairPlanReceipt(plan, cd)
		if !done || fact.Evidence.Error != "" || plan.QA == nil && fact.Evidence.ExitCode != 0 || plan.QA != nil && !qaOperationExecuted(fact) {
			return false
		}
	}
	return len(s.repairComparisonFacts(context, cd)) == len(context.affected)
}

func (e *Engine) advanceRepairComparisons(s *State, revision uint64) (bool, error) {
	for _, context := range s.repairComparisonContexts(e.cfg.Definition) {
		if s.repairComparisonResult(context.binding.Task) != nil {
			continue
		}
		if context.resource.Status != "READY" {
			return e.ensureWorkspace(s, context.resource, revision, context.binding.Task)
		}
		plans, err := s.repairComparisonPlans(context, e.cfg.Definition)
		if err != nil {
			return false, err
		}
		for _, plan := range plans {
			if fact, done := s.repairPlanReceipt(plan, e.cfg.Definition); done {
				if fact.Evidence.Error != "" || plan.QA == nil && fact.Evidence.ExitCode != 0 {
					break
				}
				continue
			}
			correlation := "check:" + digestOfCanonical(plan)
			if s.latestNativeIntent(correlation).ActionID != "" {
				break
			}
			intent, err := e.prepareAdapterIntent(s, revision, "op.check", plan, "")
			if err != nil {
				return false, err
			}
			s.PendingHostActions[intent.ActionID] = intent
			return true, nil
		}
	}
	return false, nil
}

func (s *State) projectRepairComparisons(view *decision.State, cd *compiler.CompiledDefinition) {
	for _, context := range s.repairComparisonContexts(cd) {
		if s.repairComparisonResult(context.binding.Task) != nil {
			continue
		}
		reason := decision.WaitDependencies
		if s.repairComparisonReady(context, cd) {
			reason = ""
		}
		view.RequiredTasks[context.binding.Task.Step] = append(view.RequiredTasks[context.binding.Task.Step], decision.RequiredTask{Task: context.binding.Task, BlockedBy: reason})
	}
}

func (s *State) acceptRepairComparison(record WorkerResult, cd *compiler.CompiledDefinition) error {
	context, ok := s.repairComparisonContext(record.Validation.Binding.Task, cd)
	if !ok || !s.repairComparisonReady(context, cd) || record.Validation.Binding != context.binding {
		return validationError("repair comparison has no complete native verification responsibility")
	}
	d := record.Validation.Development
	if d == nil || !slices.Equal(d.Paths, context.paths) || !slices.Equal(d.AffectedCases, context.affected) || len(d.CaseRuns) != len(context.affected) {
		return validationError("repair comparison must contain the program-derived paths and affected-case union")
	}
	facts := s.repairComparisonFacts(context, cd)
	for _, row := range d.CaseRuns {
		index := slices.IndexFunc(facts, func(f ExecutionFact) bool { return f.CaseID == row.CaseID })
		if index < 0 {
			return validationError("comparison case has no native execution")
		}
		procedure, observation, err := facts[index].Render()
		if err != nil {
			return err
		}
		if row.Procedure != procedure || row.Observation != observation {
			return validationError("repair case facts differ from native receipts")
		}
		for _, operation := range facts[index].Operations {
			if operation.Parameters.QA.GoTestRef != "" && operation.Evidence.GoResult != "pass" && row.Result == coverage.ExecutedPass {
				return validationError("repair Go test did not pass")
			}
		}
	}
	if record.Validation.Status == OutcomePass {
		if err := ValidateRepairEvidence(context.wave.Coverage.Manifests, *d, true); err != nil {
			return err
		}
	} else if record.Validation.Status != OutcomeFail {
		return validationError("repair comparison requires PASS or FAIL")
	}
	return nil
}

func (s *State) repairCheckInputsCurrent(plan CheckParameters, cd *compiler.CompiledDefinition) bool {
	if plan.Repair == nil {
		return false
	}
	context, ok := s.repairComparisonContext(plan.Repair.Task, cd)
	if !ok {
		return false
	}
	plans, err := s.repairComparisonPlans(context, cd)
	return err == nil && slices.ContainsFunc(plans, func(expected CheckParameters) bool { return digestOfCanonical(plan) == digestOfCanonical(expected) })
}

func (s *State) repairVerificationFailure(cd *compiler.CompiledDefinition) *decision.Fact {
	for _, context := range s.repairComparisonContexts(cd) {
		for action, result := range s.Results {
			if _, obsolete := s.ObsoleteActions[action]; !obsolete && result.Outcome == OutcomePass && result.Validation != nil && result.Validation.Binding == context.binding && result.Validation.Status == OutcomeFail {
				return &decision.Fact{Source: decision.SourceReceipt, Key: action, Value: "repair native execution does not match the approved affected-case oracle"}
			}
		}
		plans, err := s.repairComparisonPlans(context, cd)
		if err != nil {
			return &decision.Fact{Source: decision.SourceReceipt, Key: context.binding.Task.String(), Value: err.Error()}
		}
		for _, plan := range plans {
			if fact, done := s.repairPlanReceipt(plan, cd); done && (fact.Evidence.Error != "" || plan.QA == nil && fact.Evidence.ExitCode != 0 || plan.QA != nil && !qaOperationExecuted(fact)) {
				return &decision.Fact{Source: decision.SourceReceipt, Key: fact.ActionID, Value: fmt.Sprintf("repair full verification failed (exit %d): %s", fact.Evidence.ExitCode, fact.Evidence.Error)}
			}
		}
	}
	return nil
}

func (s *State) nextRepairCheckParameters(task runtime.TaskKey, cd *compiler.CompiledDefinition) (CheckParameters, bool) {
	context, ok := s.repairComparisonContext(task, cd)
	if !ok {
		return CheckParameters{}, false
	}
	plans, err := s.repairComparisonPlans(context, cd)
	if err != nil {
		return CheckParameters{}, false
	}
	for _, plan := range plans {
		if fact, done := s.repairPlanReceipt(plan, cd); done {
			if fact.Evidence.Error != "" || plan.QA == nil && fact.Evidence.ExitCode != 0 || plan.QA != nil && !qaOperationExecuted(fact) {
				return CheckParameters{}, false
			}
			continue
		}
		return plan, true
	}
	return CheckParameters{}, false
}

func (s *State) repairSourceComparisonPassed(work repairWork) bool {
	original := s.developmentEditResult(work.task)
	if original == nil || !original.Validation.Binding.PureP2 {
		return original != nil
	}
	for id, result := range s.Results {
		if _, obsolete := s.ObsoleteActions[id]; obsolete {
			continue
		}
		if result.Validation != nil && result.Validation.Binding.VerificationSource == original.ActionID && result.Validation.Status == OutcomePass && result.Outcome == OutcomePass {
			return true
		}
	}
	return false
}

func repairCheckSources(context repairComparison) *RepairCheckParameters {
	p := &RepairCheckParameters{Task: context.binding.Task}
	for _, r := range context.originals {
		p.SourceActions = append(p.SourceActions, r.ActionID)
	}
	return p
}

func (s *State) frozenRepairEdit(action, wave string) (WorkerResult, bool) {
	candidates := []WorkerResult{}
	if r, ok := s.Results[action]; ok {
		candidates = append(candidates, r)
	}
	candidates = append(candidates, s.ObsoleteResults[action]...)
	binding, issued := s.DispatchBindings[action]
	if !issued || binding.WaveID != wave {
		return WorkerResult{}, false
	}
	for _, r := range candidates {
		if r.ActionID != action || r.Outcome != OutcomePass || r.Validation == nil || r.Validation.Status != OutcomePass || r.Validation.Development == nil || r.Validation.Binding != binding {
			continue
		}
		for receiptAction, receipt := range s.HostActionReceipts {
			if receipt.AdapterOperation != "op.candidate" || receipt.Status != HostActionStatusExecuted {
				continue
			}
			intent, ok := s.hostIntent(receiptAction)
			if !ok {
				continue
			}
			p, err := candidateParameters(intent)
			if err != nil || p.Stage != "CHECK_SOURCE" || p.WorkspaceID != binding.WorkspaceID || !slices.Contains(p.SourceTasks, binding.Task) {
				continue
			}
			if _, _, err := validateCandidateReceipt(s.NativeVCS(), intent, receipt); err != nil {
				continue
			}
			if !slices.ContainsFunc(r.Validation.Development.Paths, func(path string) bool { return !slices.Contains(p.Paths, path) }) {
				return r, true
			}
		}
	}
	return WorkerResult{}, false
}
