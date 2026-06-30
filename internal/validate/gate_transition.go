package validate

import (
	"fmt"
	"strings"
	"time"
)

type GateRecordTransitionOptions struct {
	Worktree         string
	StatePath        string
	RunDir           string
	WorkflowID       string
	FromSnapshot     string
	ToSnapshot       string
	RerunFromGate    string
	FlowMode         string
	WorkflowMode     string
	DecisionArtifact string
	Reason           string
}

type GateTransition struct {
	WorkflowID           string `json:"workflowId"`
	FromSnapshot         string `json:"fromSnapshot"`
	ToSnapshot           string `json:"toSnapshot"`
	RerunFromGate        string `json:"rerunFromGate"`
	FlowMode             string `json:"flowMode"`
	WorkflowMode         string `json:"workflowMode"`
	DecisionArtifact     string `json:"decisionArtifact"`
	DecisionArtifactHash string `json:"decisionArtifactHash"`
	Reason               string `json:"reason"`
	Worktree             string `json:"worktree"`
	StatePath            string `json:"statePath"`
	UpdatedAtUTC         string `json:"updatedAtUtc"`
}

var postDevelopmentGateOrder = []string{
	"qa-test-gate",
	"complexity-gate",
	"architecture-health-gate",
	"code-quality-gate",
}

var postDevelopmentGateIndex = map[string]int{
	"qa-test-gate":             0,
	"complexity-gate":          1,
	"architecture-health-gate": 2,
	"code-quality-gate":        3,
}

func GateRecordTransition(options GateRecordTransitionOptions) Result {
	worktree := cleanRoot(options.Worktree)
	statePath := resolveStatePath(worktree, options.StatePath)
	var result Result
	transition := normalizeTransitionOptions(worktree, statePath, options)
	validateTransitionFields(worktree, options.RunDir, transition, &result)
	if !result.OK() {
		return result
	}

	state, err := loadGateState(statePath)
	if err != nil {
		result.add(slash(statePath), err.Error())
		return result
	}
	if err := verifyTransitionSourcePrerequisites(worktree, statePath, options.RunDir, state, transition); err != nil {
		result.add("source-pass", err.Error())
		return result
	}
	for _, existing := range state.Transitions {
		if existing.WorkflowID != transition.WorkflowID || existing.ToSnapshot != transition.ToSnapshot {
			continue
		}
		if equivalentTransition(existing, transition) {
			return result
		}
		result.add("transition-conflict", fmt.Sprintf("workflowId=%s toSnapshot=%s has conflicting transition fromSnapshot=%s rerunFromGate=%s workflowMode=%s decisionArtifact=%s",
			transition.WorkflowID, transition.ToSnapshot, existing.FromSnapshot, existing.RerunFromGate, existing.WorkflowMode, existing.DecisionArtifact))
		return result
	}
	state.Transitions = append(state.Transitions, transition)
	if err := writeGateState(statePath, state); err != nil {
		result.add(slash(statePath), err.Error())
	}
	return result
}

func normalizeTransitionOptions(worktree, statePath string, options GateRecordTransitionOptions) GateTransition {
	flowMode := strings.TrimSpace(options.FlowMode)
	if flowMode == "" {
		flowMode = "post-development"
	}
	transition := GateTransition{
		WorkflowID:       strings.TrimSpace(options.WorkflowID),
		FromSnapshot:     strings.TrimSpace(options.FromSnapshot),
		ToSnapshot:       strings.TrimSpace(options.ToSnapshot),
		RerunFromGate:    strings.TrimSpace(options.RerunFromGate),
		FlowMode:         flowMode,
		WorkflowMode:     strings.TrimSpace(options.WorkflowMode),
		DecisionArtifact: strings.TrimSpace(options.DecisionArtifact),
		Reason:           strings.TrimSpace(options.Reason),
		Worktree:         slash(absPath(worktree)),
		StatePath:        slash(absPath(statePath)),
		UpdatedAtUTC:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if transition.DecisionArtifact != "" && isFile(resolvePath(worktree, transition.DecisionArtifact)) {
		transition.DecisionArtifactHash = sha256File(resolvePath(worktree, transition.DecisionArtifact))
	}
	return transition
}

func validateTransitionFields(worktree, runDir string, transition GateTransition, result *Result) {
	if transition.WorkflowID == "" {
		result.add("workflow-id", "--workflow-id is required")
	}
	if transition.FromSnapshot == "" {
		result.add("from-snapshot", "--from-snapshot is required")
	}
	if transition.ToSnapshot == "" {
		result.add("to-snapshot", "--to-snapshot is required")
	}
	if transition.FromSnapshot != "" && transition.ToSnapshot != "" && transition.FromSnapshot == transition.ToSnapshot {
		result.add("snapshot-transition", "--from-snapshot and --to-snapshot must differ")
	}
	if _, ok := postDevelopmentGateIndex[transition.RerunFromGate]; !ok {
		result.add("rerun-from-gate", "must be one of qa-test-gate, complexity-gate, architecture-health-gate, code-quality-gate")
	}
	if transition.FlowMode != "post-development" {
		result.add("flow-mode", "must be post-development")
	}
	switch transition.WorkflowMode {
	case "four-gate", "release", "seal":
	case "start-readiness-only":
		result.add("workflow-mode", "start-readiness-only cannot record rerun transitions")
	case "":
		result.add("workflow-mode", "--workflow-mode is required")
	default:
		result.add("workflow-mode", "must be four-gate, release, or seal")
	}
	if transition.Reason == "" {
		result.add("reason", "--reason is required")
	}
	if transition.DecisionArtifact == "" {
		result.add("decision-artifact", "--decision-artifact is required")
		return
	}
	if strings.TrimSpace(transition.DecisionArtifactHash) == "" || !isSHA256(transition.DecisionArtifactHash) {
		result.add("decision-artifact", "decision artifact hash is missing or invalid")
	}
	if err := verifyTransitionDecisionArtifact(worktree, runDir, transition); err != nil {
		result.add("transition-artifact", err.Error())
	}
}

func verifyTransitionDecisionArtifact(worktree, runDir string, transition GateTransition) error {
	path := resolvePath(worktree, transition.DecisionArtifact)
	if cleanupScratchPath(worktree, path) {
		return fmt.Errorf("transition-artifact-out-of-bounds: decision artifact cannot be under cleanup scratch: %s", slash(path))
	}
	if strings.TrimSpace(runDir) != "" {
		if err := requireAbsPathUnderRunDir(runDir, "decision-artifact", path); err != nil {
			return fmt.Errorf("transition-artifact-out-of-bounds: %s", err.Error())
		}
	}
	if !isFile(path) {
		return fmt.Errorf("transition-artifact-missing: %s", transition.DecisionArtifact)
	}
	if strings.TrimSpace(transition.DecisionArtifactHash) == "" || !isSHA256(transition.DecisionArtifactHash) {
		return fmt.Errorf("transition-artifact-hash-missing: %s", transition.DecisionArtifact)
	}
	if actual := sha256File(path); actual != strings.ToLower(transition.DecisionArtifactHash) {
		return fmt.Errorf("transition-artifact-hash-mismatch: %s", transition.DecisionArtifact)
	}
	text, err := readText(path)
	if err != nil {
		return fmt.Errorf("transition-artifact-missing: cannot read %s: %v", transition.DecisionArtifact, err)
	}
	if !strings.Contains(text, "Rerun Scope Decision") {
		return fmt.Errorf("transition-artifact-conflict: missing Rerun Scope Decision")
	}
	checks := []struct {
		label string
		want  string
		keys  []string
	}{
		{label: "workflowId", want: transition.WorkflowID, keys: []string{"Workflow ID", "workflowId"}},
		{label: "fromSnapshot", want: transition.FromSnapshot, keys: []string{"From snapshot", "fromSnapshot"}},
		{label: "toSnapshot", want: transition.ToSnapshot, keys: []string{"To snapshot", "New change snapshot", "toSnapshot"}},
		{label: "rerunFromGate", want: transition.RerunFromGate, keys: []string{"Rerun from gate", "Earliest gate to rerun", "rerunFromGate"}},
		{label: "flowMode", want: transition.FlowMode, keys: []string{"Flow mode", "flowMode"}},
		{label: "workflowMode", want: transition.WorkflowMode, keys: []string{"Workflow mode", "workflowMode"}},
		{label: "reason", want: transition.Reason, keys: []string{"Transition reason", "Reason"}},
	}
	for _, check := range checks {
		if got := firstArtifactFieldValue(text, check.keys...); got != check.want {
			if got == "" {
				return fmt.Errorf("transition-artifact-conflict: missing %s", check.label)
			}
			return fmt.Errorf("transition-artifact-conflict: %s=%q want %q", check.label, got, check.want)
		}
	}
	if !strings.EqualFold(firstArtifactFieldValue(text, "Full-scope review confirmed"), "YES") {
		return fmt.Errorf("transition-artifact-conflict: Full-scope review confirmed must be YES")
	}
	if !meaningful(firstArtifactFieldValue(text, "Reason skipped gates still apply")) {
		return fmt.Errorf("transition-artifact-conflict: Reason skipped gates still apply is required")
	}
	return nil
}

func firstArtifactFieldValue(text string, fields ...string) string {
	for _, field := range fields {
		if value := fieldValue(text, field); value != "" {
			return value
		}
	}
	return ""
}

func equivalentTransition(a, b GateTransition) bool {
	return a.WorkflowID == b.WorkflowID &&
		a.FromSnapshot == b.FromSnapshot &&
		a.ToSnapshot == b.ToSnapshot &&
		a.RerunFromGate == b.RerunFromGate &&
		a.FlowMode == b.FlowMode &&
		a.WorkflowMode == b.WorkflowMode &&
		a.DecisionArtifact == b.DecisionArtifact &&
		strings.ToLower(a.DecisionArtifactHash) == strings.ToLower(b.DecisionArtifactHash) &&
		a.Reason == b.Reason
}

func verifyTransitionSourcePrerequisites(worktree, statePath, runDir string, state GateState, transition GateTransition) error {
	rerunIndex, ok := postDevelopmentGateIndex[transition.RerunFromGate]
	if !ok {
		return fmt.Errorf("transition-invalid: rerunFromGate=%s is not a post-development gate", transition.RerunFromGate)
	}
	for _, requirement := range requirementsBeforeGate(rerunIndex) {
		if err := verifySourceRequirement(worktree, statePath, runDir, state, requirement, transition.RerunFromGate, transition.WorkflowID, transition.FromSnapshot); err != nil {
			return err
		}
	}
	return nil
}

func requirementsBeforeGate(rerunIndex int) []admissionRequirement {
	requirements := make([]admissionRequirement, 0, rerunIndex)
	for _, gate := range postDevelopmentGateOrder[:rerunIndex] {
		requirement := admissionRequirement{gate: gate, artifact: true}
		if gate == "qa-test-gate" {
			requirement.mode = "formal"
			requirement.stage = "Execution"
		}
		requirements = append(requirements, requirement)
	}
	return requirements
}

func verifyRequirement(worktree, statePath, runDir string, state GateState, requirement admissionRequirement, requiredFor, workflowID, changeSnapshot, mode string) error {
	currentErr, hardFailure := verifyCurrentRequirement(worktree, statePath, runDir, state, requirement, requiredFor, workflowID, changeSnapshot)
	if currentErr == nil {
		return nil
	}
	if hardFailure {
		return currentErr
	}
	if mode == "start-readiness" {
		return fmt.Errorf("%s; transition-invalid: start-readiness-only admission cannot use rerun transitions", currentErr.Error())
	}
	if err := verifyRequirementViaTransition(worktree, statePath, runDir, state, requirement, requiredFor, workflowID, changeSnapshot); err != nil {
		return fmt.Errorf("%s; %s", currentErr.Error(), err.Error())
	}
	return nil
}

func verifyCurrentRequirement(worktree, statePath, runDir string, state GateState, requirement admissionRequirement, requiredFor, workflowID, changeSnapshot string) (error, bool) {
	entries := entriesForGateNewestFirst(state, requirement.gate)
	if len(entries) == 0 {
		return fmt.Errorf("current-pass-missing: missing prerequisite gate=%s requiredFor=%s state=%s", requirement.gate, requiredFor, slash(statePath)), false
	}
	var latestRoute *GateStateEntry
	for i := range entries {
		entry := entries[i]
		if entry.WorkflowID == workflowID && entry.ChangeSnapshot == changeSnapshot {
			if latestRoute == nil {
				copy := entry
				latestRoute = &copy
			}
			if entry.Verdict != "PASS" {
				return fmt.Errorf("current-pass-not-real: gate=%s verdict=%s required=PASS requiredFor=%s state=%s", requirement.gate, entry.Verdict, requiredFor, slash(statePath)), true
			}
			if requirement.mode != "" && entry.Mode != requirement.mode {
				continue
			}
			if requirement.stage != "" && entry.Stage != requirement.stage {
				continue
			}
			if requirement.artifact {
				if err := verifyEntryArtifact(worktree, statePath, runDir, entry, requiredFor); err != nil {
					return fmt.Errorf("current-pass-artifact-invalid: %s", err.Error()), true
				}
			}
			return nil, false
		}
	}
	if latestRoute == nil {
		return fmt.Errorf("current-pass-missing: missing route gate=%s requiredFor=%s workflowId=%s changeSnapshot=%s state=%s", requirement.gate, requiredFor, workflowID, changeSnapshot, slash(statePath)), false
	}
	if requirement.mode != "" && latestRoute.Mode != requirement.mode {
		return fmt.Errorf("current-pass-missing: gate=%s mode=%s requiredMode=%s requiredFor=%s state=%s", requirement.gate, latestRoute.Mode, requirement.mode, requiredFor, slash(statePath)), false
	}
	if requirement.stage != "" && latestRoute.Stage != requirement.stage {
		return fmt.Errorf("current-pass-missing: gate=%s stage=%s requiredStage=%s requiredFor=%s state=%s", requirement.gate, latestRoute.Stage, requirement.stage, requiredFor, slash(statePath)), false
	}
	return fmt.Errorf("current-pass-missing: gate=%s prerequisite did not satisfy admission for %s", requirement.gate, requiredFor), false
}

func verifyRequirementViaTransition(worktree, statePath, runDir string, state GateState, requirement admissionRequirement, requiredFor, workflowID, changeSnapshot string) error {
	transition, err := transitionForAdmission(worktree, statePath, runDir, state, workflowID, changeSnapshot)
	if err != nil {
		return err
	}
	requiredIndex, ok := postDevelopmentGateIndex[requirement.gate]
	if !ok {
		return fmt.Errorf("transition-invalid: prerequisite gate=%s is not a post-development gate", requirement.gate)
	}
	rerunIndex := postDevelopmentGateIndex[transition.RerunFromGate]
	if requiredIndex >= rerunIndex {
		return fmt.Errorf("transition-not-applicable: gate=%s is not strictly earlier than rerunFromGate=%s requiredFor=%s", requirement.gate, transition.RerunFromGate, requiredFor)
	}
	if err := verifySourceRequirement(worktree, statePath, runDir, state, requirement, requiredFor, workflowID, transition.FromSnapshot); err != nil {
		return err
	}
	return nil
}

func transitionForAdmission(worktree, statePath, runDir string, state GateState, workflowID, changeSnapshot string) (GateTransition, error) {
	var selected *GateTransition
	for i := range state.Transitions {
		transition := state.Transitions[i]
		if transition.WorkflowID != workflowID || transition.ToSnapshot != changeSnapshot {
			continue
		}
		if selected == nil {
			copy := transition
			selected = &copy
			continue
		}
		if !equivalentTransition(*selected, transition) {
			return GateTransition{}, fmt.Errorf("transition-conflict: workflowId=%s toSnapshot=%s has conflicting transition records", workflowID, changeSnapshot)
		}
	}
	if selected == nil {
		return GateTransition{}, fmt.Errorf("transition-missing: no transition for workflowId=%s toSnapshot=%s", workflowID, changeSnapshot)
	}
	var result Result
	validateTransitionFields(worktree, runDir, *selected, &result)
	if !result.OK() {
		messages := make([]string, 0, len(result.Failures))
		for _, failure := range result.Failures {
			messages = append(messages, failure.Path+": "+failure.Message)
		}
		return GateTransition{}, fmt.Errorf("transition-invalid: %s", strings.Join(messages, "; "))
	}
	return *selected, nil
}

func verifySourceRequirement(worktree, statePath, runDir string, state GateState, requirement admissionRequirement, requiredFor, workflowID, sourceSnapshot string) error {
	entries := entriesForGateNewestFirst(state, requirement.gate)
	if len(entries) == 0 {
		return fmt.Errorf("source-pass-missing: missing prerequisite gate=%s sourceSnapshot=%s requiredFor=%s state=%s", requirement.gate, sourceSnapshot, requiredFor, slash(statePath))
	}
	var latestRoute *GateStateEntry
	for i := range entries {
		entry := entries[i]
		if entry.WorkflowID == workflowID && entry.ChangeSnapshot == sourceSnapshot {
			if latestRoute == nil {
				copy := entry
				latestRoute = &copy
			}
			if entry.Verdict != "PASS" {
				return fmt.Errorf("source-pass-not-real: gate=%s verdict=%s required=PASS sourceSnapshot=%s requiredFor=%s state=%s", requirement.gate, entry.Verdict, sourceSnapshot, requiredFor, slash(statePath))
			}
			if requirement.mode != "" && entry.Mode != requirement.mode {
				continue
			}
			if requirement.stage != "" && entry.Stage != requirement.stage {
				continue
			}
			if requirement.artifact {
				if err := verifyEntryArtifact(worktree, statePath, runDir, entry, requiredFor); err != nil {
					return fmt.Errorf("source-pass-artifact-invalid: %s", err.Error())
				}
			}
			return nil
		}
	}
	if latestRoute == nil {
		return fmt.Errorf("source-pass-missing: missing route gate=%s sourceSnapshot=%s requiredFor=%s workflowId=%s state=%s", requirement.gate, sourceSnapshot, requiredFor, workflowID, slash(statePath))
	}
	if requirement.mode != "" && latestRoute.Mode != requirement.mode {
		return fmt.Errorf("source-pass-not-real: gate=%s mode=%s requiredMode=%s sourceSnapshot=%s requiredFor=%s state=%s", requirement.gate, latestRoute.Mode, requirement.mode, sourceSnapshot, requiredFor, slash(statePath))
	}
	if requirement.stage != "" && latestRoute.Stage != requirement.stage {
		return fmt.Errorf("source-pass-not-real: gate=%s stage=%s requiredStage=%s sourceSnapshot=%s requiredFor=%s state=%s", requirement.gate, latestRoute.Stage, requirement.stage, sourceSnapshot, requiredFor, slash(statePath))
	}
	return fmt.Errorf("source-pass-not-real: gate=%s prerequisite did not satisfy transition admission for %s", requirement.gate, requiredFor)
}
