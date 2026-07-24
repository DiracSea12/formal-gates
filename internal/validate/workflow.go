package validate

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type StartOptions struct {
	Root, PackageRoot, RunID, Flow, RequirementSource, VCS, BaseSnapshot, CurrentSnapshot string
	RequirementConfirmed                                                                  bool
}

type FindingInput struct {
	Message   string
	Locations []string
}

type QACaseInput struct{ Description, Procedure, Oracle string }

type QAResultInput struct{ CaseID, Outcome, Procedure, Observation, OracleResult string }

type CarryInput struct{ GateID, Decision, Message string }

const formalFlow = "formal"

func Start(options StartOptions) (RunState, error) {
	root := cleanRoot(options.Root)
	for name, value := range map[string]string{"flow": options.Flow, "requirement": options.RequirementSource, "VCS": options.VCS, "base snapshot": options.BaseSnapshot} {
		if strings.TrimSpace(value) == "" {
			return RunState{}, fmt.Errorf("%s is required", name)
		}
	}
	if strings.EqualFold(strings.TrimSpace(options.VCS), "none") {
		return RunState{}, fmt.Errorf("a supported external VCS is required")
	}
	if strings.TrimSpace(options.Flow) != formalFlow {
		return RunState{}, fmt.Errorf("flow must be formal")
	}
	currentSnapshot := strings.TrimSpace(options.CurrentSnapshot)
	if currentSnapshot == "" {
		currentSnapshot = strings.TrimSpace(options.BaseSnapshot)
	}
	catalog, err := LoadPromptCatalog(options.PackageRoot)
	if err != nil {
		return RunState{}, err
	}
	requirementPath := resolveFromRoot(root, options.RequirementSource)
	revision, err := RequirementRevision(requirementPath)
	if err != nil {
		return RunState{}, fmt.Errorf("requirement: %w", err)
	}
	runID := strings.TrimSpace(options.RunID)
	if runID == "" {
		runID, err = newRunID()
		if err != nil {
			return RunState{}, err
		}
	}
	if !promptIDPattern.MatchString(runID) {
		return RunState{}, fmt.Errorf("run id must match [a-z0-9]+(?:-[a-z0-9]+)*")
	}
	if _, err := os.Stat(RunDir(root, runID)); err == nil {
		return RunState{}, fmt.Errorf("run %q already exists", runID)
	} else if !os.IsNotExist(err) {
		return RunState{}, err
	}
	if _, err := os.Stat(RunSummaryPath(root, runID)); err == nil {
		return RunState{}, fmt.Errorf("run %q already has a retained result", runID)
	} else if !os.IsNotExist(err) {
		return RunState{}, err
	}
	if err := os.MkdirAll(filepath.Dir(RunDir(root, runID)), 0o700); err != nil {
		return RunState{}, err
	}
	if err := os.Mkdir(RunDir(root, runID), 0o700); err != nil {
		return RunState{}, fmt.Errorf("cannot create run %q: %w", runID, err)
	}
	state := NewRunState(runID, strings.TrimSpace(options.Flow), options.RequirementSource, revision, strings.TrimSpace(options.VCS), strings.TrimSpace(options.BaseSnapshot), currentSnapshot, catalog.BaseRevision, catalog.CatalogRevision, options.RequirementConfirmed, catalog.GateIDs())
	if err := SaveRunState(root, state); err != nil {
		_ = os.RemoveAll(RunDir(root, runID))
		return RunState{}, err
	}
	return state, nil
}

func Resume(root, packageRoot, runID string) (RunState, bool, error) {
	var invalidated bool
	state, err := mutateRun(root, runID, func(state *RunState) error {
		catalog, err := LoadPromptCatalog(packageRoot)
		if err != nil {
			return err
		}
		if state.BasePromptRevision != catalog.BaseRevision || state.CatalogRevision != catalog.CatalogRevision {
			return fmt.Errorf("installed prompt catalog changed; start a new run")
		}
		revision, err := RequirementRevision(resolveFromRoot(cleanRoot(root), state.RequirementSource))
		if err != nil {
			return fmt.Errorf("requirement: %w", err)
		}
		if revision != state.RequirementRevision {
			state.RequirementRevision = revision
			state.RequirementConfirmed = false
			invalidateRequirementResults(state, catalog.GateIDs())
			invalidated = true
		}
		return nil
	})
	return state, invalidated, err
}

func UpdateRequirement(root, packageRoot, runID, source string, confirmed bool) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentCatalog(*state, packageRoot)
		if err != nil {
			return err
		}
		if strings.TrimSpace(source) == "" {
			source = state.RequirementSource
		}
		revision, err := RequirementRevision(resolveFromRoot(cleanRoot(root), source))
		if err != nil {
			return fmt.Errorf("requirement: %w", err)
		}
		if revision != state.RequirementRevision || source != state.RequirementSource {
			state.RequirementSource, state.RequirementRevision = source, revision
			invalidateRequirementResults(state, catalog.GateIDs())
		}
		state.RequirementConfirmed = confirmed
		return nil
	})
}

func PrepareGate(root, packageRoot, runID, gateID, liveSnapshot string) (string, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return "", err
	}
	if err := requireActive(state); err != nil {
		return "", err
	}
	if !state.RequirementConfirmed {
		return "", fmt.Errorf("the current requirement is not confirmed")
	}
	catalog, err := requireCurrentDefinitions(root, state, packageRoot)
	if err != nil {
		return "", err
	}
	if err := requireLiveSnapshot(state, liveSnapshot); err != nil {
		return "", err
	}
	result, ok := state.Gates[gateID]
	if !ok {
		return "", fmt.Errorf("gate %q is not in this run's discovered catalog", gateID)
	}
	if result.Status == "PASS" && result.Snapshot != state.CurrentSnapshot {
		return "", fmt.Errorf("gate %q is awaiting a Carry decision", gateID)
	}
	return ComposeGatePrompt(catalog, gateID, routeForState(root, state))
}

func PrepareAction(root, packageRoot, runID, actionID, liveSnapshot string) (string, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return "", err
	}
	if err := requireActive(state); err != nil {
		return "", err
	}
	catalog, err := requireCurrentDefinitions(root, state, packageRoot)
	if err != nil {
		return "", err
	}
	if actionID != "requirements-clarification" && !state.RequirementConfirmed {
		return "", fmt.Errorf("the current requirement is not confirmed")
	}
	if actionID == "qa-execution" || actionID == "carry" || actionID == "development-worker" {
		if err := requireLiveSnapshot(state, liveSnapshot); err != nil {
			return "", err
		}
	}
	if actionID == "development-worker" {
		if len(state.QACases) == 0 {
			return "", fmt.Errorf("approved QA cases are required before development starts")
		}
	}
	detail := ""
	if actionID == "qa-execution" {
		if len(state.QACases) == 0 {
			return "", fmt.Errorf("approved QA cases are missing")
		}
		var lines []string
		for _, testCase := range state.QACases {
			lines = append(lines, fmt.Sprintf("%s\ndescription: %s\nprocedure: %s\noracle: %s", testCase.ID, testCase.Description, testCase.Procedure, testCase.Oracle))
		}
		detail = strings.Join(lines, "\n\n")
	} else if actionID == "carry" {
		eligible := eligibleCarryGates(state)
		if len(eligible) == 0 {
			return "", fmt.Errorf("no prior passing gates require a Carry decision")
		}
		var lines []string
		lines = append(lines, "Decide INHERIT or RERUN for each gate below:")
		for _, id := range eligible {
			gate, _ := catalog.Gate(id)
			lines = append(lines, fmt.Sprintf("\n[Gate: %s]\n%s", id, gate.Content))
		}
		detail = strings.Join(lines, "\n")
	}
	return ComposeActionPrompt(catalog, actionID, routeForState(root, state), detail)
}

func RecordAction(root, packageRoot, runID, actionID, status, message string, findings []FindingInput, sourceRevision, sourceCatalogRevision string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireSourceBinding(*state, sourceRevision, sourceCatalogRevision, "", false); err != nil {
			return err
		}
		if _, ok := catalog.Action(actionID); !ok {
			return fmt.Errorf("unknown action prompt %q", actionID)
		}
		if actionID != "requirements-clarification" && actionID != "start-readiness" {
			return fmt.Errorf("action %q has a dedicated workflow command and cannot use record-action", actionID)
		}
		if actionID == "start-readiness" && !state.RequirementConfirmed {
			return fmt.Errorf("the current requirement is not confirmed")
		}
		result, err := semanticActionResult(status, message, findings, state)
		if err != nil {
			return err
		}
		state.Actions[actionID] = result
		return nil
	})
}

func RecordGate(root, packageRoot, runID, gateID, status, message string, findings []FindingInput, sourceRevision, sourceCatalogRevision, sourceSnapshot, liveSnapshot string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireLiveSnapshot(*state, liveSnapshot); err != nil {
			return err
		}
		if err := requireSourceBinding(*state, sourceRevision, sourceCatalogRevision, sourceSnapshot, true); err != nil {
			return err
		}
		if !state.RequirementConfirmed {
			return fmt.Errorf("the current requirement is not confirmed")
		}
		if _, ok := catalog.Gate(gateID); !ok {
			return fmt.Errorf("gate %q is not discovered", gateID)
		}
		existing := state.Gates[gateID]
		if existing.Status == "PASS" && existing.Snapshot != state.CurrentSnapshot {
			return fmt.Errorf("gate %q requires a Carry decision before rerun", gateID)
		}
		result, err := semanticGateResult(status, message, findings, state)
		if err != nil {
			return err
		}
		state.Gates[gateID] = result
		return nil
	})
}

func RecordQADesign(root, packageRoot, runID string, cases []QACaseInput, runtimeError, sourceRevision, sourceCatalogRevision string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if err := requireSourceBinding(*state, sourceRevision, sourceCatalogRevision, "", false); err != nil {
			return err
		}
		if !state.RequirementConfirmed {
			return fmt.Errorf("the current requirement is not confirmed")
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(cases) != 0 {
				return fmt.Errorf("QA Design runtime error cannot include cases")
			}
			state.QACases = []QACase{}
			state.QAExecution = QAExecutionResult{Status: "PENDING"}
			state.Actions["qa-design"] = ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError)}
			return nil
		}
		if len(cases) == 0 {
			return fmt.Errorf("at least one QA case is required")
		}
		seen := map[string]bool{}
		state.QACases = make([]QACase, 0, len(cases))
		for index, item := range cases {
			normalized := QACase{
				Description: strings.TrimSpace(item.Description),
				Procedure:   strings.TrimSpace(item.Procedure),
				Oracle:      strings.TrimSpace(item.Oracle),
			}
			for name, value := range map[string]string{"description": normalized.Description, "procedure": normalized.Procedure, "oracle": normalized.Oracle} {
				if value == "" {
					return fmt.Errorf("QA case %d %s is required", index+1, name)
				}
			}
			key := normalized.Description + "\x00" + normalized.Procedure + "\x00" + normalized.Oracle
			if seen[key] {
				return fmt.Errorf("duplicate QA case %d", index+1)
			}
			seen[key] = true
			normalized.ID = fmt.Sprintf("CASE-%03d", index+1)
			state.QACases = append(state.QACases, normalized)
		}
		state.QAExecution = QAExecutionResult{Status: "PENDING"}
		state.Actions["qa-design"] = ActionResult{Status: "PASS"}
		return nil
	})
}

func RecordQAExecution(root, packageRoot, runID string, results []QAResultInput, runtimeError, sourceRevision, sourceCatalogRevision, sourceSnapshot, liveSnapshot string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if err := requireLiveSnapshot(*state, liveSnapshot); err != nil {
			return err
		}
		if err := requireSourceBinding(*state, sourceRevision, sourceCatalogRevision, sourceSnapshot, true); err != nil {
			return err
		}
		if !state.RequirementConfirmed {
			return fmt.Errorf("the current requirement is not confirmed")
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(results) != 0 {
				return fmt.Errorf("QA runtime error cannot include case results")
			}
			state.QAExecution = QAExecutionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), Snapshot: state.CurrentSnapshot}
			return nil
		}
		if len(state.QACases) == 0 {
			return fmt.Errorf("approved QA cases are missing")
		}
		if len(results) != len(state.QACases) {
			return fmt.Errorf("QA execution must cover all %d approved cases", len(state.QACases))
		}
		byID := map[string]QAResultInput{}
		for _, item := range results {
			if _, exists := byID[item.CaseID]; exists {
				return fmt.Errorf("duplicate QA result for %s", item.CaseID)
			}
			if item.Outcome != "PASS" && item.Outcome != "FAIL" {
				return fmt.Errorf("QA outcome for %s must be PASS or FAIL", item.CaseID)
			}
			for name, value := range map[string]string{"procedure": item.Procedure, "observation": item.Observation, "oracle result": item.OracleResult} {
				if strings.TrimSpace(value) == "" {
					return fmt.Errorf("QA result %s %s is required", item.CaseID, name)
				}
			}
			byID[item.CaseID] = item
		}
		status := "PASS"
		findings := []Finding{}
		recorded := make([]QAResultRecord, 0, len(state.QACases))
		for _, testCase := range state.QACases {
			item, ok := byID[testCase.ID]
			if !ok {
				return fmt.Errorf("QA result is missing for %s", testCase.ID)
			}
			if item.Outcome == "FAIL" {
				status = "FAIL"
				findings = append(findings, Finding{Message: testCase.ID + ": " + strings.TrimSpace(item.Observation)})
			}
			recorded = append(recorded, QAResultRecord{CaseID: item.CaseID, Outcome: item.Outcome, Procedure: strings.TrimSpace(item.Procedure), Observation: strings.TrimSpace(item.Observation), OracleResult: strings.TrimSpace(item.OracleResult)})
		}
		state.QAExecution = QAExecutionResult{Status: status, Snapshot: state.CurrentSnapshot, Cases: recorded, Findings: findings}
		return nil
	})
}

func AdvanceSnapshot(root, packageRoot, runID, currentSnapshot, liveSnapshot string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if strings.TrimSpace(currentSnapshot) == "" || currentSnapshot == state.CurrentSnapshot {
			return fmt.Errorf("a new current snapshot is required")
		}
		if strings.TrimSpace(liveSnapshot) == "" || liveSnapshot != currentSnapshot {
			return fmt.Errorf("live VCS identity must match the new current snapshot")
		}
		if len(eligibleCarryGates(*state)) != 0 {
			return fmt.Errorf("prior passing gates still await a Carry decision")
		}
		oldSnapshot := state.CurrentSnapshot
		hasPriorPass := false
		for _, result := range state.Gates {
			if result.Status == "PASS" && result.Snapshot == oldSnapshot {
				hasPriorPass = true
				break
			}
		}
		state.CurrentSnapshot = currentSnapshot
		if hasPriorPass {
			state.PreRepairSnapshot = oldSnapshot
		} else {
			state.PreRepairSnapshot = ""
		}
		state.QAExecution = QAExecutionResult{Status: "PENDING"}
		state.Carry = map[string]CarryResult{}
		for id, result := range state.Gates {
			if result.Status != "PASS" {
				state.Gates[id] = GateResult{Status: "PENDING"}
			}
		}
		if hasPriorPass {
			state.Actions["carry"] = ActionResult{Status: "PENDING"}
		} else {
			delete(state.Actions, "carry")
		}
		return nil
	})
}

func RecordCarry(root, packageRoot, runID string, decisions []CarryInput, runtimeError, sourceRevision, sourceCatalogRevision, sourceSnapshot, liveSnapshot string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if err := requireLiveSnapshot(*state, liveSnapshot); err != nil {
			return err
		}
		if err := requireSourceBinding(*state, sourceRevision, sourceCatalogRevision, sourceSnapshot, true); err != nil {
			return err
		}
		if !state.RequirementConfirmed {
			return fmt.Errorf("the current requirement is not confirmed")
		}
		eligible := eligibleCarryGates(*state)
		if len(eligible) == 0 {
			return fmt.Errorf("no prior passing gates require a Carry decision")
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(decisions) != 0 {
				return fmt.Errorf("Carry runtime error cannot include decisions")
			}
			state.Actions["carry"] = ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError)}
			return nil
		}
		if len(decisions) != len(eligible) {
			return fmt.Errorf("Carry must decide all %d prior passing gates", len(eligible))
		}
		wanted := map[string]bool{}
		for _, id := range eligible {
			wanted[id] = true
		}
		seen := map[string]bool{}
		for _, decision := range decisions {
			if !wanted[decision.GateID] {
				return fmt.Errorf("gate %q is not eligible for Carry", decision.GateID)
			}
			if seen[decision.GateID] {
				return fmt.Errorf("duplicate Carry decision for %s", decision.GateID)
			}
			if decision.Decision != "INHERIT" && decision.Decision != "RERUN" {
				return fmt.Errorf("Carry decision for %s must be INHERIT or RERUN", decision.GateID)
			}
			if strings.TrimSpace(decision.Message) == "" {
				return fmt.Errorf("Carry decision for %s requires a reason", decision.GateID)
			}
			seen[decision.GateID] = true
			prior := state.Gates[decision.GateID]
			state.Carry[decision.GateID] = CarryResult{Decision: decision.Decision, SourceSnapshot: state.PreRepairSnapshot, TargetSnapshot: state.CurrentSnapshot, Message: strings.TrimSpace(decision.Message)}
			if decision.Decision == "INHERIT" {
				prior.SourceSnapshot = state.PreRepairSnapshot
				prior.Snapshot = state.CurrentSnapshot
				state.Gates[decision.GateID] = prior
			} else {
				state.Gates[decision.GateID] = GateResult{Status: "PENDING"}
			}
		}
		state.Actions["carry"] = ActionResult{Status: "PASS"}
		return nil
	})
}

func Abort(root, runID string) (RunSummary, error) { return finishRun(root, runID, "ABORTED") }

func Seal(root, packageRoot, runID, liveBefore, liveAfter string) (RunSummary, error) {
	path := RunStatePath(root, runID)
	release, err := acquireStateLock(path)
	if err != nil {
		return RunSummary{}, err
	}
	defer release()
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunSummary{}, err
	}
	if err := requireActive(state); err != nil {
		return RunSummary{}, err
	}
	if _, err := requireCurrentDefinitions(root, state, packageRoot); err != nil {
		return RunSummary{}, err
	}
	if !state.RequirementConfirmed {
		return RunSummary{}, fmt.Errorf("current requirement is not confirmed")
	}
	if liveBefore != state.CurrentSnapshot || liveAfter != state.CurrentSnapshot {
		return RunSummary{}, fmt.Errorf("live VCS identity must match the current snapshot before and after aggregation")
	}
	state.Status = "SEALED"
	if err := SaveRunState(root, state); err != nil {
		return RunSummary{}, err
	}
	if err := SaveRunSummary(root, state); err != nil {
		return RunSummary{}, err
	}
	summary := runSummary(state)
	if err := DeleteRun(root, runID); err != nil {
		return RunSummary{}, err
	}
	return summary, nil
}

func finishRun(root, runID, status string) (RunSummary, error) {
	path := RunStatePath(root, runID)
	release, err := acquireStateLock(path)
	if err != nil {
		return RunSummary{}, err
	}
	defer release()
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunSummary{}, err
	}
	if err := requireActive(state); err != nil {
		return RunSummary{}, err
	}
	state.Status = status
	if err := SaveRunState(root, state); err != nil {
		return RunSummary{}, err
	}
	if err := SaveRunSummary(root, state); err != nil {
		return RunSummary{}, err
	}
	summary := runSummary(state)
	if err := DeleteRun(root, runID); err != nil {
		return RunSummary{}, err
	}
	return summary, nil
}

func mutateRun(root, runID string, change func(*RunState) error) (RunState, error) {
	path := RunStatePath(root, runID)
	release, err := acquireStateLock(path)
	if err != nil {
		return RunState{}, err
	}
	defer release()
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunState{}, err
	}
	if err := requireActive(state); err != nil {
		return RunState{}, err
	}
	if err := change(&state); err != nil {
		return RunState{}, err
	}
	if err := SaveRunState(root, state); err != nil {
		return RunState{}, err
	}
	return state, nil
}

func acquireStateLock(statePath string) (func(), error) {
	lockPath := statePath + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			file.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for another run-state update")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func requireActive(state RunState) error {
	if state.Status != "ACTIVE" {
		return fmt.Errorf("run %s is %s", state.RunID, state.Status)
	}
	return nil
}

func requireCurrentCatalog(state RunState, packageRoot string) (PromptCatalog, error) {
	catalog, err := LoadPromptCatalog(packageRoot)
	if err != nil {
		return PromptCatalog{}, err
	}
	if state.BasePromptRevision != catalog.BaseRevision || state.CatalogRevision != catalog.CatalogRevision {
		return PromptCatalog{}, fmt.Errorf("installed prompt catalog changed; start a new run")
	}
	return catalog, nil
}

func requireCurrentDefinitions(root string, state RunState, packageRoot string) (PromptCatalog, error) {
	catalog, err := requireCurrentCatalog(state, packageRoot)
	if err != nil {
		return PromptCatalog{}, err
	}
	revision, err := RequirementRevision(resolveFromRoot(cleanRoot(root), state.RequirementSource))
	if err != nil {
		return PromptCatalog{}, fmt.Errorf("requirement: %w", err)
	}
	if revision != state.RequirementRevision {
		return PromptCatalog{}, fmt.Errorf("requirement changed; resume the run before continuing")
	}
	return catalog, nil
}

func requireLiveSnapshot(state RunState, live string) error {
	if strings.TrimSpace(live) == "" {
		return fmt.Errorf("live VCS identity is required")
	}
	if live != state.CurrentSnapshot {
		return fmt.Errorf("live VCS identity does not match current snapshot")
	}
	return nil
}

func requireSourceBinding(state RunState, sourceRevision, sourceCatalogRevision, sourceSnapshot string, snapshotBound bool) error {
	if strings.TrimSpace(sourceRevision) == "" {
		return fmt.Errorf("source requirement revision is required")
	}
	if sourceRevision != state.RequirementRevision {
		return fmt.Errorf("source requirement revision does not match the current requirement")
	}
	if strings.TrimSpace(sourceCatalogRevision) == "" {
		return fmt.Errorf("source catalog revision is required")
	}
	if sourceCatalogRevision != state.CatalogRevision {
		return fmt.Errorf("source catalog revision does not match the current catalog")
	}
	if !snapshotBound {
		return nil
	}
	if strings.TrimSpace(sourceSnapshot) == "" {
		return fmt.Errorf("source snapshot is required")
	}
	if sourceSnapshot != state.CurrentSnapshot {
		return fmt.Errorf("source snapshot does not match the current snapshot")
	}
	return nil
}

func routeForState(root string, state RunState) PromptRoute {
	return PromptRoute{RequirementSource: state.RequirementSource, RequirementRevision: state.RequirementRevision, CatalogRevision: state.CatalogRevision, Worktree: absPath(cleanRoot(root)), VCS: state.VCS, BaseSnapshot: state.BaseSnapshot, CurrentSnapshot: state.CurrentSnapshot, PreRepairSnapshot: state.PreRepairSnapshot}
}

func semanticActionResult(status, message string, findings []FindingInput, state *RunState) (ActionResult, error) {
	normalized, converted, err := validateSemanticResult(status, message, findings)
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Status: normalized, Message: strings.TrimSpace(message), Findings: converted}, nil
}

func semanticGateResult(status, message string, findings []FindingInput, state *RunState) (GateResult, error) {
	normalized, converted, err := validateSemanticResult(status, message, findings)
	if err != nil {
		return GateResult{}, err
	}
	return GateResult{Status: normalized, Message: strings.TrimSpace(message), Snapshot: state.CurrentSnapshot, Findings: converted}, nil
}

func validateSemanticResult(status, message string, findings []FindingInput) (string, []Finding, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "PASS" && status != "FAIL" && status != "RUNTIME_ERROR" {
		return "", nil, fmt.Errorf("status must be PASS, FAIL, or RUNTIME_ERROR")
	}
	if status == "PASS" && len(findings) != 0 {
		return "", nil, fmt.Errorf("PASS cannot include findings")
	}
	if status == "FAIL" && len(findings) == 0 {
		return "", nil, fmt.Errorf("FAIL requires at least one finding")
	}
	if status == "RUNTIME_ERROR" {
		if len(findings) != 0 {
			return "", nil, fmt.Errorf("RUNTIME_ERROR cannot include reviewer findings")
		}
		if strings.TrimSpace(message) == "" {
			return "", nil, fmt.Errorf("RUNTIME_ERROR requires a message")
		}
	}
	converted := make([]Finding, 0, len(findings))
	for _, input := range findings {
		if strings.TrimSpace(input.Message) == "" {
			return "", nil, fmt.Errorf("finding message is required")
		}
		locations := make([]string, 0, len(input.Locations))
		for _, location := range input.Locations {
			if err := validateFindingLocation(location); err != nil {
				return "", nil, err
			}
			locations = append(locations, strings.TrimSpace(location))
		}
		converted = append(converted, Finding{Message: strings.TrimSpace(input.Message), Locations: locations})
	}
	return status, converted, nil
}

func validateFindingLocation(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("finding location is empty")
	}
	if strings.Contains(value, `\`) || strings.Contains(value, "://") || strings.HasPrefix(value, "/") || (len(value) > 1 && value[1] == ':') {
		return fmt.Errorf("finding location must be repository-relative: %s", value)
	}
	path := value
	for count := 0; count < 2; count++ {
		index := strings.LastIndex(path, ":")
		if index <= 0 {
			break
		}
		if !suffixIsDigits(path[index+1:]) {
			break
		}
		path = path[:index]
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("finding location must be repository-relative: %s", value)
	}
	return nil
}

func suffixIsDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func invalidateRequirementResults(state *RunState, gateIDs []string) {
	state.Actions = pendingRequirementActions()
	state.QACases = []QACase{}
	state.QAExecution = QAExecutionResult{Status: "PENDING"}
	state.Carry = map[string]CarryResult{}
	state.PreRepairSnapshot = ""
	state.Gates = map[string]GateResult{}
	for _, id := range gateIDs {
		state.Gates[id] = GateResult{Status: "PENDING"}
	}
}

func eligibleCarryGates(state RunState) []string {
	var ids []string
	if state.PreRepairSnapshot == "" {
		return ids
	}
	for id, result := range state.Gates {
		if result.Status == "PASS" && result.Snapshot == state.PreRepairSnapshot {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func sortedGateIDs(gates map[string]GateResult) []string {
	ids := make([]string, 0, len(gates))
	for id := range gates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func resolveFromRoot(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.FromSlash(path))
}

func absPath(path string) string {
	full, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(full)
}

func newRunID() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return strings.ToLower(time.Now().UTC().Format("20060102t150405000z")) + "-" + hex.EncodeToString(suffix[:]), nil
}
