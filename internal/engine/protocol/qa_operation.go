package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

// QACheckParameters selects one operation from the frozen reviewed case. The
// execution binding and full recipe travel with the original host intent.
type QACheckParameters struct {
	OutputArtifacts    []string                  `json:"outputArtifacts,omitempty"`
	Preparation        *coverage.SharedStep      `json:"preparation,omitempty"`
	PreparationOrdinal int                       `json:"preparationOrdinal,omitempty"`
	Recipe             []coverage.Operation      `json:"recipe,omitempty"`
	Execution          coverage.ExecutionBinding `json:"execution"`
	CaseID             string                    `json:"caseId"`
	Ordinal            int                       `json:"ordinal"`
	Source             string                    `json:"source"`
	Operation          coverage.Operation        `json:"operation"`
	Directory          string                    `json:"directory"`
	WorkspaceRoot      string                    `json:"workspaceRoot"`
	GoTestRef          string                    `json:"goTestRef,omitempty"`
}

// ExecutionFact is a read-only projection of complete native receipts. It is
// frozen in worker input; HostActionReceipts remain the execution authority.
type ExecutionFact struct {
	CaseID     string                   `json:"caseId"`
	Operations []ExecutionOperationFact `json:"operations"`
}
type ExecutionOperationFact struct {
	ActionID   string          `json:"actionId"`
	Parameters CheckParameters `json:"parameters"`
	Evidence   CheckEvidence   `json:"evidence"`
}

func (f ExecutionFact) Render() (procedure, observation string, err error) {
	procedures, observations := []string{}, []string{}
	for _, operation := range f.Operations {
		p := operation.Parameters.QA
		raw, err := json.Marshal(struct {
			Source     string             `json:"source"`
			Ordinal    int                `json:"ordinal"`
			Operation  coverage.Operation `json:"operation"`
			GoTestRef  string             `json:"goTestRef,omitempty"`
			Invocation string             `json:"invocation"`
			Directory  string             `json:"directory"`
		}{p.Source, p.Ordinal, p.Operation, p.GoTestRef, operation.Evidence.Invocation, operation.Evidence.WorkingDirectory})
		if err != nil {
			return "", "", err
		}
		procedures = append(procedures, string(raw))
		e := operation.Evidence
		raw, err = json.Marshal(struct {
			ActionID string `json:"actionId"`
			CheckEvidence
		}{operation.ActionID, e})
		if err != nil {
			return "", "", err
		}
		observations = append(observations, string(raw))
	}
	return strings.Join(procedures, "\n"), strings.Join(observations, "\n"), nil
}

func ExecutionIdentities(facts []ExecutionFact) (tool, environment string) {
	tools, environments := []string{}, []string{}
	for _, fact := range facts {
		for _, operation := range fact.Operations {
			tools = append(tools, operation.Evidence.Tool)
			environments = append(environments, operation.Evidence.Environment)
		}
	}
	slices.Sort(tools)
	tools = slices.Compact(tools)
	slices.Sort(environments)
	environments = slices.Compact(environments)
	return strings.Join(tools, "\n"), strings.Join(environments, "\n")
}

func (s *State) qaCheckPlans(target *ValidationTarget, cd *compiler.CompiledDefinition) ([]CheckParameters, error) {
	binding := s.validationBinding(target.Task, cd)
	if attempt := s.Attempts[target.Task]; attempt.Work != nil {
		binding = *attempt.Work
	} else if attempt := s.EndedAttempts[target.Task]; attempt.Work != nil {
		binding = *attempt.Work
	}
	return s.qaOperationPlans(target, binding)
}

func (s *State) qaOperationPlans(target *ValidationTarget, binding WorkBinding) ([]CheckParameters, error) {
	wave := s.validationWave(binding.WaveID)
	if wave == nil || s.currentValidationWave() == nil || s.currentValidationWave().Status != "COLLECTING" || s.validationTargetCurrent(binding) == nil || target.Kind != "QA" || !s.validationRequirementCurrent() || s.currentValidationWave().RouteDigest != s.validationRouteDigest() {
		return nil, validationError("QA execution needs the current frozen collecting wave")
	}
	target = wave.Targets[binding.Task.String()]
	if target == nil {
		return nil, validationError("QA execution has no original frozen target")
	}
	c, whitelist, err := s.executionContract(target.Mode, wave)
	if err != nil {
		return nil, err
	}
	required := requiredExecutionCases(wave, target, whitelist)
	if len(required) == 0 {
		return nil, validationError("QA execution has no approved required cases")
	}
	workspace := s.NativeResources[binding.WorkspaceID]
	if workspace.Status != "READY" || !s.resourceUsable(workspace.ID) || workspace.Source != wave.Candidate.Identity {
		return nil, validationError("QA execution has no registered candidate workspace")
	}
	execution := coverage.ExecutionBinding{RequiredSourcesDigest: whitelist.RequiredSourcesDigest, ManifestDigest: whitelist.ManifestDigest, MapDigest: whitelist.MapDigest, WhitelistDigest: whitelist.Digest, Candidate: wave.Candidate, Scope: wave.Scope, ExpectedCaseIDs: executionCaseIDs(whitelist)}
	var plans []CheckParameters
	for _, id := range executionCaseIDs(whitelist) {
		if !slices.Contains(required, id) {
			continue
		}
		manifest := c.Manifests[0]
		index := slices.IndexFunc(manifest.Cases, func(item coverage.AcceptanceCase) bool { return item.CaseID == id })
		if index < 0 {
			return nil, validationError("approved QA case is missing: " + id)
		}
		item := manifest.Cases[index]
		bound := s.qaBoundRecipe(binding, id)
		if len(item.Operations) == 0 && len(bound) > 0 {
			item.Operations = bound
		}
		operations, err := caseOperationPlans(manifest, item)
		if err != nil {
			return nil, err
		}
		for ordinal, op := range operations {
			op.Execution, op.CaseID, op.Ordinal, op.WorkspaceRoot = execution, id, ordinal, workspace.Path
			if strings.HasPrefix(op.Source, "case:") && len(bound) > 0 {
				op.Recipe = bound
			}
			plans = append(plans, CheckParameters{QA: &op, Binding: binding, SourceIdentity: wave.Candidate.Identity, SourceDigest: wave.Candidate.Digest, WorkspaceID: workspace.ID})
		}
	}
	return plans, nil
}

func (s *State) qaCheckReceipt(p CheckParameters) (ExecutionOperationFact, bool) {
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" || receipt.Status != HostActionStatusExecuted {
			continue
		}
		intent, ok := s.hostIntent(receipt.ActionID)
		if !ok {
			continue
		}
		issued, err := checkParameters(intent)
		if err != nil || issued.QA == nil || digestOfCanonical(issued) != digestOfCanonical(p) || s.validateNativeCheckReceipt(intent, receipt) != nil {
			continue
		}
		if _, obsolete := s.ObsoleteActions[intent.ActionID]; obsolete {
			continue
		}
		var evidence CheckEvidence
		if decodeAdapterValues(receipt, &evidence) != nil {
			continue
		}
		if evidence.PreparationSourceAction != "" {
			original, ok := s.hostIntent(evidence.PreparationSourceAction)
			if !ok {
				continue
			}
			issued, err = checkParameters(original)
			if err != nil {
				continue
			}
			return ExecutionOperationFact{ActionID: original.ActionID, Parameters: issued, Evidence: evidence}, true
		}
		return ExecutionOperationFact{ActionID: intent.ActionID, Parameters: issued, Evidence: evidence}, true
	}
	return ExecutionOperationFact{}, false
}

func (s *State) qaExecutionFacts(target *ValidationTarget, cd *compiler.CompiledDefinition) []ExecutionFact {
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil {
		return nil
	}
	var facts []ExecutionFact
	for _, p := range plans {
		if len(facts) == 0 || facts[len(facts)-1].CaseID != p.QA.CaseID {
			facts = append(facts, ExecutionFact{CaseID: p.QA.CaseID})
		}
		fact := &facts[len(facts)-1]
		operation, settled := s.qaCheckReceipt(p)
		if !settled || !qaOperationExecuted(operation) {
			fact.Operations = nil
			continue
		}
		if p.QA.Ordinal == len(fact.Operations) {
			fact.Operations = append(fact.Operations, operation)
		}
	}
	return slices.DeleteFunc(facts, func(fact ExecutionFact) bool {
		count := 0
		for _, p := range plans {
			if p.QA.CaseID == fact.CaseID {
				count++
			}
		}
		return len(fact.Operations) != count
	})
}

func (s *State) nextQACheckParameters(task runtime.TaskKey, cd *compiler.CompiledDefinition) (CheckParameters, bool) {
	if s.requirementChangePending() {
		return CheckParameters{}, false
	}
	wave := s.currentValidationWave()
	if wave == nil {
		return CheckParameters{}, false
	}
	target := wave.Targets[task.String()]
	if target == nil || (target.Status != "PENDING" && target.Status != "RUNTIME_ERROR" && target.Status != "RETRY_EXHAUSTED") {
		return CheckParameters{}, false
	}
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil {
		return CheckParameters{}, false
	}
	failedCase, waitingCase := "", ""
	for _, p := range plans {
		if p.QA.CaseID == failedCase || p.QA.CaseID == waitingCase {
			continue
		}
		if target.Execution != nil && slices.ContainsFunc(target.Execution.Records, func(row coverage.ExecutionRecord) bool { return row.CaseID == p.QA.CaseID }) {
			continue
		}
		if fact, done := s.qaCheckReceipt(p); done {
			if !qaOperationExecuted(fact) {
				failedCase = p.QA.CaseID
			}
			continue
		}
		if p.QA.Operation.Kind == "" && p.QA.GoTestRef == "" {
			continue
		}
		if s.latestNativeIntent("check:"+digestOfCanonical(p)).ActionID != "" {
			waitingCase = p.QA.CaseID
			continue
		}
		return p, true
	}
	return CheckParameters{}, false
}

func (s *State) qaCheckInputsCurrent(p CheckParameters) bool {
	wave := s.validationWave(p.Binding.WaveID)
	if wave == nil || p.QA == nil || p.SourceActionID != "" || p.Check.ID != "" || p.Check.Command != "" {
		return false
	}
	target := wave.Targets[p.Binding.Task.String()]
	if target == nil {
		return false
	}
	if p.Binding.WaveID != wave.ID || p.Binding.Candidate != wave.Candidate.Identity || p.Binding.Generation != wave.Generation || p.Binding.RequirementRevision != wave.RequirementRevision {
		return false
	}
	plans, err := s.qaOperationPlans(target, p.Binding)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(plans, func(expected CheckParameters) bool { return digestOfCanonical(expected) == digestOfCanonical(p) })
}

func (e *Engine) advanceQAOperations(s *State, revision uint64) (bool, error) {
	wave := s.currentValidationWave()
	if wave == nil {
		return false, nil
	}
	for _, key := range sortedTargetKeys(wave) {
		p, ready := s.nextQACheckParameters(wave.Targets[key].Task, e.cfg.Definition)
		if !ready {
			continue
		}
		if s.latestNativeIntent("check:"+digestOfCanonical(p)).ActionID != "" {
			continue
		}
		intent, err := e.prepareAdapterIntent(s, revision, "op.check", p, "")
		if err != nil {
			return false, err
		}
		s.PendingHostActions[intent.ActionID] = intent
		return true, nil
	}
	return false, nil
}

func (s *State) qaComparisonReady(target *ValidationTarget, cd *compiler.CompiledDefinition) bool {
	if len(s.qaCommandCases(target, cd)) > 0 {
		return true
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.check" {
			continue
		}
		p, err := checkParameters(intent)
		if err == nil && p.QA != nil && p.Binding.Task == target.Task {
			if _, settled := s.HostActionReceipts[intent.ActionID]; !settled {
				return false
			}
		}
	}
	facts := s.qaExecutionFacts(target, cd)
	wave := s.validationWaveForTask(target.Task)
	if wave == nil {
		return false
	}
	_, whitelist, err := s.executionContract(target.Mode, wave)
	if err != nil {
		return false
	}
	for _, fact := range facts {
		for _, edge := range whitelist.Entries {
			if edge.CaseID != fact.CaseID {
				continue
			}
			if target.Execution == nil || !slices.ContainsFunc(target.Execution.Records, func(row coverage.ExecutionRecord) bool {
				return row.ReviewKind == edge.ReviewKind && row.SourceID == edge.SourceID && row.PointID == edge.PointID && row.CaseID == edge.CaseID
			}) {
				return true
			}
		}
	}
	return false
}

func qaOperationExecuted(fact ExecutionOperationFact) bool {
	return fact.Evidence.Error == "" && (fact.Parameters.QA.GoTestRef == "" || fact.Evidence.GoResult == "pass" || fact.Evidence.GoResult == "fail")
}

func (s *State) qaOperationPending(cd *compiler.CompiledDefinition) string {
	wave := s.currentValidationWave()
	if wave == nil || wave.Status != "COLLECTING" {
		return ""
	}
	for _, key := range sortedTargetKeys(wave) {
		target := wave.Targets[key]
		if target.Kind != "QA" || (target.Status != "PENDING" && target.Status != "RUNTIME_ERROR" && target.Status != "RETRY_EXHAUSTED") {
			continue
		}
		plans, err := s.qaCheckPlans(target, cd)
		if err != nil {
			return err.Error()
		}
		for _, p := range plans {
			if fact, settled := s.qaCheckReceipt(p); settled && !qaOperationExecuted(fact) {
				return fmt.Sprintf("QA case %s operation %d did not execute completely: error=%q Go result=%q; original receipt %s", p.QA.CaseID, p.QA.Ordinal, fact.Evidence.Error, fact.Evidence.GoResult, fact.ActionID)
			}
		}
	}
	return ""
}

func caseOperationPlans(manifest coverage.AcceptanceManifest, item coverage.AcceptanceCase) ([]QACheckParameters, error) {
	var operations []QACheckParameters
	for _, sharedID := range item.SharedSteps {
		index := slices.IndexFunc(manifest.SharedSteps, func(step coverage.SharedStep) bool { return step.ID == sharedID })
		if index < 0 {
			return nil, validationError("approved shared step is missing: " + sharedID)
		}
		step := manifest.SharedSteps[index]
		outputs := []string{}
		remaining := item.SharedSteps[slices.Index(item.SharedSteps, sharedID):]
		for _, possible := range manifest.SharedSteps {
			if slices.Contains(remaining, possible.ID) {
				outputs = append(outputs, possible.Artifacts...)
			}
		}
		slices.Sort(outputs)
		outputs = slices.Compact(outputs)
		for ordinal, op := range step.Operations {
			operations = append(operations, QACheckParameters{OutputArtifacts: outputs, Preparation: &step, PreparationOrdinal: ordinal, Source: "shared:" + sharedID, Operation: op, Directory: "."})
		}
	}
	if len(item.Operations) > 0 {
		for _, operation := range item.Operations {
			operations = append(operations, QACheckParameters{Source: "case:" + item.CaseID, Operation: operation, Directory: "."})
		}
	} else if goOperation, err := coverage.GoTestOperation(item.TestRef); err == nil {
		operations = append(operations, QACheckParameters{Source: "case:" + item.CaseID, GoTestRef: item.TestRef, Directory: goOperation.Directory})
	} else {
		operations = append(operations, QACheckParameters{Source: "case:" + item.CaseID, Directory: "."})
	}
	return operations, nil
}

// QACaseOperations binds only missing invocation values to an approved case.
type QACaseOperations struct {
	CaseID     string               `json:"caseId"`
	Operations []coverage.Operation `json:"operations"`
}

func (s *State) qaBoundRecipe(binding WorkBinding, caseID string) []coverage.Operation {
	for _, intent := range s.qaCheckIntents() {
		p, err := checkParameters(intent)
		if err == nil && p.QA != nil && p.Binding == binding && p.QA.CaseID == caseID && len(p.QA.Recipe) > 0 {
			return slices.Clone(p.QA.Recipe)
		}
	}
	return nil
}

func (s *State) qaCheckIntents() []HostActionIntent {
	intents := []HostActionIntent{}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.check" {
			intents = append(intents, intent)
		}
	}
	for _, effect := range s.ReconciledEffects {
		if effect.Intent != nil && effect.Intent.Adapter != nil && effect.Intent.Adapter.Operation == "op.check" {
			intents = append(intents, *effect.Intent)
		}
	}
	slices.SortFunc(intents, func(a, b HostActionIntent) int { return strings.Compare(a.ActionID, b.ActionID) })
	return intents
}

func (s *State) qaCommandCases(target *ValidationTarget, cd *compiler.CompiledDefinition) []string {
	plans, err := s.qaCheckPlans(target, cd)
	if err != nil {
		return nil
	}
	cases := []string{}
	for _, p := range plans {
		if p.QA.Operation.Kind == "" && p.QA.GoTestRef == "" && !slices.Contains(cases, p.QA.CaseID) {
			cases = append(cases, p.QA.CaseID)
		}
	}
	return cases
}

func (e *Engine) qaCommandIntents(s *State, result ValidationResult, revision uint64) ([]HostActionIntent, error) {
	wave := s.validationWave(result.Binding.WaveID)
	if wave == nil {
		return nil, validationError("QA command has no frozen wave")
	}
	target := wave.Targets[result.Binding.Task.String()]
	if target == nil {
		return nil, validationError("QA command has no original target")
	}
	cases := s.qaCommandCases(target, e.cfg.Definition)
	if len(cases) == 0 || len(result.QACommands) != len(cases) {
		return nil, validationError("QA commands must cover exactly the issued cases missing invocation")
	}
	plans, err := s.qaCheckPlans(target, e.cfg.Definition)
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 || plans[0].Binding != result.Binding {
		return nil, validationError("QA command binding differs from its original issued work")
	}
	intents := []HostActionIntent{}
	for index, command := range result.QACommands {
		if command.CaseID != cases[index] || len(command.Operations) == 0 {
			return nil, validationError("QA command differs from its issued case order")
		}
		if err := coverage.ValidateOperations(command.Operations); err != nil {
			return nil, err
		}
		for _, p := range plans {
			if p.QA.CaseID != command.CaseID || p.QA.Operation.Kind != "" || p.QA.GoTestRef != "" {
				continue
			}
			qa := *p.QA
			qa.Recipe, qa.Operation = slices.Clone(command.Operations), command.Operations[0]
			p.QA = &qa
			intent, err := e.prepareAdapterIntent(s, revision, "op.check", p, "")
			if err != nil {
				return nil, err
			}
			// A compound binding signs distinct effects at one state revision.
			// Each existing payload identity qualifies its own native effect.
			if len(result.QACommands) > 1 {
				intent.ActionID += ":" + strings.TrimPrefix(intent.PayloadDigest, "sha256:")
			}
			intents = append(intents, intent)
		}
	}
	return intents, nil
}

func (s *State) qaCheckExecutionReady(intent HostActionIntent) bool {
	p, err := checkParameters(intent)
	if err != nil || p.QA == nil {
		return true
	}
	wave := s.validationWave(p.Binding.WaveID)
	if wave == nil {
		return false
	}
	target := wave.Targets[p.Binding.Task.String()]
	if target == nil {
		return false
	}
	plans, err := s.qaOperationPlans(target, p.Binding)
	if err != nil {
		return false
	}
	for _, prior := range plans {
		if prior.QA.CaseID == p.QA.CaseID && prior.QA.Ordinal < p.QA.Ordinal {
			fact, done := s.qaCheckReceipt(prior)
			if !done || !qaOperationExecuted(fact) {
				return false
			}
		}
	}
	return true
}

// CommonPreparationIntents selects original native jobs by their approved
// content and registered source. Actual byte/environment equality is observed
// by the native adapter before reuse.
func (s *State) CommonPreparationIntents(p CheckParameters) []HostActionIntent {
	if p.QA == nil || p.QA.Preparation == nil || p.QA.Preparation.Independent {
		return nil
	}
	step := *p.QA.Preparation
	step.ID = ""
	intents := []HostActionIntent{}
	for _, intent := range s.qaCheckIntents() {
		if _, obsolete := s.ObsoleteActions[intent.ActionID]; obsolete {
			continue
		}
		prior, err := checkParameters(intent)
		if err != nil || prior.QA == nil || prior.QA.Preparation == nil || prior.QA.Preparation.Independent {
			continue
		}
		old := *prior.QA.Preparation
		old.ID = ""
		if prior.SourceIdentity == p.SourceIdentity && prior.SourceDigest == p.SourceDigest && prior.WorkspaceID == p.WorkspaceID && prior.QA.Directory == p.QA.Directory && prior.QA.PreparationOrdinal == p.QA.PreparationOrdinal && digestOfCanonical(old) == digestOfCanonical(step) {
			intents = append(intents, intent)
		}
	}
	return intents
}

func (s *State) qaInvocationBindingCurrent(p CheckParameters, cd *compiler.CompiledDefinition) bool {
	if p.QA == nil || len(p.QA.Recipe) == 0 || p.QA.GoTestRef != "" || p.QA.Preparation != nil || coverage.ValidateOperations(p.QA.Recipe) != nil {
		return false
	}
	wave := s.validationWave(p.Binding.WaveID)
	if wave == nil {
		return false
	}
	target := wave.Targets[p.Binding.Task.String()]
	if target == nil {
		return false
	}
	plans, err := s.qaOperationPlans(target, p.Binding)
	if err != nil {
		return false
	}
	for _, missing := range plans {
		if missing.QA.CaseID != p.QA.CaseID || missing.QA.Operation.Kind != "" || missing.QA.GoTestRef != "" {
			continue
		}
		qa := *missing.QA
		qa.Recipe, qa.Operation = p.QA.Recipe, p.QA.Recipe[0]
		missing.QA = &qa
		return digestOfCanonical(missing) == digestOfCanonical(p)
	}
	return false
}
