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
	Execution     coverage.ExecutionBinding `json:"execution"`
	CaseID        string                    `json:"caseId"`
	Ordinal       int                       `json:"ordinal"`
	Source        string                    `json:"source"`
	Operation     coverage.Operation        `json:"operation"`
	Directory     string                    `json:"directory"`
	WorkspaceRoot string                    `json:"workspaceRoot"`
	GoTestRef     string                    `json:"goTestRef,omitempty"`
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
	return s.qaOperationPlans(target, binding)
}

func (s *State) qaOperationPlans(target *ValidationTarget, binding WorkBinding) ([]CheckParameters, error) {
	wave := s.validationWaveForTask(target.Task)
	if wave == nil || wave != s.currentValidationWave() || wave.Status != "COLLECTING" || target.Kind != "QA" || !s.validationRequirementCurrent() || s.requirementChangePending() || wave.RouteDigest != s.validationRouteDigest() {
		return nil, validationError("QA execution needs the current frozen collecting wave")
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
		operations, err := caseOperationPlans(manifest, item)
		if err != nil {
			return nil, err
		}
		for ordinal, op := range operations {
			op.Execution, op.CaseID, op.Ordinal, op.WorkspaceRoot = execution, id, ordinal, workspace.Path
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
		if err != nil || issued.QA == nil || digestOfCanonical(issued) != digestOfCanonical(p) || validateCheckReceipt(intent, receipt) != nil {
			continue
		}
		if _, obsolete := s.ObsoleteActions[intent.ActionID]; obsolete {
			continue
		}
		var evidence CheckEvidence
		if decodeAdapterValues(receipt, &evidence) != nil {
			continue
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
	wave := s.validationWaveForTask(task)
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
	failedCase := ""
	for _, p := range plans {
		if p.QA.CaseID == failedCase {
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
		return p, true
	}
	return CheckParameters{}, false
}

func (s *State) qaCheckInputsCurrent(p CheckParameters) bool {
	wave := s.validationWaveForTask(p.Binding.Task)
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
		for _, op := range manifest.SharedSteps[index].Operations {
			operations = append(operations, QACheckParameters{Source: "shared:" + sharedID, Operation: op, Directory: "."})
		}
	}
	if len(item.Operations) > 0 {
		for _, operation := range item.Operations {
			operations = append(operations, QACheckParameters{Source: "case:" + item.CaseID, Operation: operation, Directory: "."})
		}
	} else if goOperation, err := coverage.GoTestOperation(item.TestRef); err == nil {
		operations = append(operations, QACheckParameters{Source: "case:" + item.CaseID, GoTestRef: item.TestRef, Directory: goOperation.Directory})
	} else {
		return nil, fmt.Errorf("case %s lacks approved operations or an exact Go test reference", item.CaseID)
	}
	return operations, nil
}
