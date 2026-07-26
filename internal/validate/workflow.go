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
	Severity  string
	Message   string
	Locations []string
}

type QACaseInput struct{ Description, Procedure, Oracle string }

type QAResultInput struct{ CaseID, Outcome, Procedure, Observation, OracleResult string }

type CarryInput struct{ GateID, Decision, Message string }

const (
	carryOriginIndependent  = "INDEPENDENT"
	carryOriginMainShortcut = "MAIN_SHORTCUT"
)

const formalFlow = "formal"
const automaticReviewWaveLimit = 3

const (
	developmentPending        = "PENDING"
	developmentPrepared       = "PREPARED"
	developmentRepairPrepared = "REPAIR_PREPARED"
	developmentComplete       = "PASS"
	developmentVerified       = "VERIFIED"
)

var routeModes = map[string]bool{"full": true, "custom": true}

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
	if options.RequirementConfirmed {
		return RunState{}, fmt.Errorf("a run cannot start with a pre-confirmed requirement; record Requirements Clarification first")
	}
	currentSnapshot := strings.TrimSpace(options.CurrentSnapshot)
	if currentSnapshot == "" {
		currentSnapshot = strings.TrimSpace(options.BaseSnapshot)
	}
	if currentSnapshot != strings.TrimSpace(options.BaseSnapshot) {
		return RunState{}, fmt.Errorf("a new run's current snapshot must equal its base snapshot")
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
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunState{}, false, err
	}
	if err := requireActive(state); err != nil {
		return RunState{}, false, err
	}
	if _, err := requireCurrentCatalog(state, packageRoot); err != nil {
		return RunState{}, false, err
	}
	revision, err := RequirementRevision(resolveFromRoot(cleanRoot(root), state.RequirementSource))
	if err != nil {
		return RunState{}, false, fmt.Errorf("requirement: %w", err)
	}
	return state, revision != state.RequirementRevision, nil
}

func UpdateRequirement(root, packageRoot, runID, source string, confirmed bool, semanticEffect, liveSnapshot string) (RunState, error) {
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
		changed := revision != state.RequirementRevision || source != state.RequirementSource
		semanticEffect = strings.ToLower(strings.TrimSpace(semanticEffect))
		if changed {
			if semanticEffect != "preserved" && semanticEffect != "changed" {
				return fmt.Errorf("changed requirement requires semantic effect preserved or changed")
			}
			liveSnapshot = strings.TrimSpace(liveSnapshot)
			if liveSnapshot == "" {
				return fmt.Errorf("changed requirement requires the current live VCS snapshot")
			}
			state.RequirementSource, state.RequirementRevision = source, revision
			if semanticEffect == "preserved" {
				if !state.RequirementConfirmed {
					return fmt.Errorf("meaning can be preserved only for a previously confirmed requirement")
				}
				rebindCurrentSnapshot(state, liveSnapshot)
				return nil
			}
			state.CurrentSnapshot = liveSnapshot
			invalidateRequirementResults(state, catalog.GateIDs())
			state.RequirementConfirmed = false
			if confirmed {
				return fmt.Errorf("a meaning-changing requirement must return to Requirements Clarification")
			}
			return nil
		}
		if semanticEffect != "" {
			return fmt.Errorf("semantic effect is accepted only when the requirement revision changed")
		}
		if strings.TrimSpace(liveSnapshot) != "" {
			return fmt.Errorf("live VCS snapshot is accepted only when the requirement revision changed")
		}
		if confirmed && state.Actions["requirements-clarification"].Status != "PASS" {
			return fmt.Errorf("Requirements Clarification must pass before requirement confirmation")
		}
		state.RequirementConfirmed = confirmed
		return nil
	})
}

func RouteCandidates(root, packageRoot, runID string) ([]string, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return nil, err
	}
	catalog, err := requireCurrentDefinitions(root, state, packageRoot)
	if err != nil {
		return nil, err
	}
	if !state.RequirementConfirmed {
		return nil, fmt.Errorf("the current requirement is not confirmed")
	}
	return catalog.RouteCandidates(), nil
}

func SetRoute(root, packageRoot, runID, mode string, selected []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireTransition(*state, "route", ""); err != nil {
			return err
		}
		mode = strings.ToLower(strings.TrimSpace(mode))
		if !routeModes[mode] {
			return fmt.Errorf("route mode must be full or custom")
		}
		candidates := catalog.RouteCandidates()
		if mode == "full" {
			if len(selected) != 0 {
				return fmt.Errorf("full route selects the complete discovered list without --gate")
			}
			selected = candidates
		} else {
			var err error
			selected, err = normalizeSelected(selected, candidates)
			if err != nil {
				return err
			}
			if len(selected) == 0 || len(selected) == len(candidates) {
				return fmt.Errorf("custom route must select a non-empty proper subset; use full for the complete list")
			}
		}
		state.RouteMode = mode
		state.SelectedGates = append([]string{}, selected...)
		state.SkipAuthorizations = map[string]SkipAuthorization{}
		chosen := selectedSet(*state)
		for _, id := range candidates {
			if !chosen[id] {
				state.SkipAuthorizations[id] = SkipAuthorization{Origin: "ROUTE", Status: "UNSELECTED"}
			}
		}
		return nil
	})
}

func AddRouteGates(root, packageRoot, runID string, additions []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireTransition(*state, "route-add", ""); err != nil {
			return err
		}
		if len(additions) == 0 {
			return fmt.Errorf("at least one gate addition is required")
		}
		candidates := catalog.RouteCandidates()
		normalized, err := normalizeSelected(additions, candidates)
		if err != nil {
			return err
		}
		chosen := selectedSet(*state)
		for _, id := range normalized {
			if chosen[id] {
				return fmt.Errorf("gate %q is already selected", id)
			}
			if id == "qa" && developmentStarted(*state) {
				return fmt.Errorf("QA cannot be added after development begins")
			}
			chosen[id] = true
			delete(state.SkipAuthorizations, id)
		}
		state.SelectedGates = orderedSelection(chosen, candidates)
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
	catalog, err := requireCurrentDefinitions(root, state, packageRoot)
	if err != nil {
		return "", err
	}
	if err := requireLiveSnapshot(state, liveSnapshot); err != nil {
		return "", err
	}
	if err := requireTransition(state, "gate", gateID); err != nil {
		return "", err
	}
	result, ok := state.Gates[gateID]
	if !ok {
		return "", fmt.Errorf("gate %q is not in this run's discovered catalog", gateID)
	}
	if result.Status == "PASS" && result.Snapshot != state.CurrentSnapshot {
		return "", fmt.Errorf("gate %q is awaiting a Carry decision", gateID)
	}
	if semanticResultRecorded(result.Status, result.Snapshot, state.CurrentSnapshot) {
		return "", fmt.Errorf("gate %q already has an authoritative %s result for the current snapshot", gateID, result.Status)
	}
	return ComposeGatePrompt(catalog, gateID, routeForState(root, state))
}

func PrepareAction(root, packageRoot, runID, actionID, liveSnapshot string) (string, error) {
	if actionID == "development-worker" {
		return prepareDevelopmentAction(root, packageRoot, runID, liveSnapshot)
	}
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
	if actionID == "qa-execution" || actionID == "carry" {
		if err := requireLiveSnapshot(state, liveSnapshot); err != nil {
			return "", err
		}
	}
	if err := requireTransition(state, actionID, ""); err != nil {
		return "", err
	}
	if actionID == "qa-execution" && semanticResultRecorded(state.QAExecution.Status, state.QAExecution.Snapshot, state.CurrentSnapshot) {
		return "", fmt.Errorf("QA Execution already has an authoritative %s result for the current snapshot", state.QAExecution.Status)
	}
	detail := ""
	if actionID == "qa-design" && len(state.QACases) != 0 && state.Actions["qa-design"].Status != "PASS" {
		lines := []string{"Review the complete current requirement and every prior case below. Return the complete resulting case set. Retain confirmed unaffected cases and add, modify, or remove only affected cases when impact is reliably bounded; replace the complete set when it is not or the overall workflow changed."}
		if review := state.Actions["qa-review"]; review.Status == "FAIL" {
			lines = append(lines, "Address these QA Review findings while redesigning the complete case set:")
			for _, finding := range review.Findings {
				line := "- " + finding.Message
				if len(finding.Locations) != 0 {
					line += " (" + strings.Join(finding.Locations, ", ") + ")"
				}
				lines = append(lines, line)
			}
		}
		for _, testCase := range state.QACases {
			lines = append(lines, fmt.Sprintf("%s\ndescription: %s\nprocedure: %s\noracle: %s", testCase.ID, testCase.Description, testCase.Procedure, testCase.Oracle))
		}
		detail = strings.Join(lines, "\n\n")
	} else if actionID == "qa-review" || actionID == "qa-execution" {
		if len(state.QACases) == 0 {
			return "", fmt.Errorf("QA cases are missing")
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

func prepareDevelopmentAction(root, packageRoot, runID, liveSnapshot string) (string, error) {
	prompt := ""
	_, err := mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireLiveSnapshot(*state, liveSnapshot); err != nil {
			return err
		}
		if err := requireTransition(*state, "development-worker", ""); err != nil {
			return err
		}
		detail := ""
		status := state.Actions["development-worker"].Status
		if status == developmentComplete || status == developmentVerified || status == developmentRepairPrepared {
			detail = repairInput(*state)
		}
		prompt, err = ComposeActionPrompt(catalog, "development-worker", routeForState(root, *state), detail)
		if err != nil {
			return err
		}
		if status == developmentComplete || status == developmentVerified {
			state.Actions["development-worker"] = ActionResult{Status: developmentRepairPrepared}
		} else if status == developmentPending {
			state.Actions["development-worker"] = ActionResult{Status: developmentPrepared}
		}
		return nil
	})
	return prompt, err
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
		if actionID != "requirements-clarification" && actionID != "start-readiness" && actionID != "qa-review" {
			return fmt.Errorf("action %q has a dedicated workflow command and cannot use record-action", actionID)
		}
		if err := requireTransition(*state, actionID, ""); err != nil {
			return err
		}
		result, err := semanticActionResult(status, message, findings, state)
		if err != nil {
			return err
		}
		state.Actions[actionID] = result
		if actionID == "qa-review" && result.Status == "FAIL" {
			state.Actions["qa-design"] = ActionResult{Status: "PENDING"}
			state.QAExecution = QAExecutionResult{Status: "PENDING"}
		}
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
		if _, ok := catalog.Gate(gateID); !ok {
			return fmt.Errorf("gate %q is not discovered", gateID)
		}
		if err := requireTransition(*state, "gate", gateID); err != nil {
			return err
		}
		existing := state.Gates[gateID]
		if existing.Status == "PASS" && existing.Snapshot != state.CurrentSnapshot {
			return fmt.Errorf("gate %q requires a Carry decision before rerun", gateID)
		}
		if semanticResultRecorded(existing.Status, existing.Snapshot, state.CurrentSnapshot) {
			return fmt.Errorf("gate %q already has an authoritative %s result for the current snapshot", gateID, existing.Status)
		}
		result, err := semanticGateResult(status, message, findings, state)
		if err != nil {
			return err
		}
		state.Gates[gateID] = result
		completeReviewWaveIfReady(state)
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
		if err := requireTransition(*state, "qa-design", ""); err != nil {
			return err
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(cases) != 0 {
				return fmt.Errorf("QA Design runtime error cannot include cases")
			}
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
		state.Actions["qa-review"] = ActionResult{Status: "PENDING"}
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
		if err := requireTransition(*state, "qa-execution", ""); err != nil {
			return err
		}
		if semanticResultRecorded(state.QAExecution.Status, state.QAExecution.Snapshot, state.CurrentSnapshot) {
			return fmt.Errorf("QA Execution already has an authoritative %s result for the current snapshot", state.QAExecution.Status)
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(results) != 0 {
				return fmt.Errorf("QA runtime error cannot include case results")
			}
			state.QAExecution = QAExecutionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), Snapshot: state.CurrentSnapshot}
			completeReviewWaveIfReady(state)
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
		completeReviewWaveIfReady(state)
		return nil
	})
}

func AdvanceSnapshot(root, packageRoot, runID, currentSnapshot, liveSnapshot string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		currentSnapshot = strings.TrimSpace(currentSnapshot)
		developmentStatus := state.Actions["development-worker"].Status
		if currentSnapshot == "" || (currentSnapshot == state.CurrentSnapshot && developmentStatus != developmentPrepared) {
			return fmt.Errorf("a new current snapshot is required")
		}
		liveSnapshot = strings.TrimSpace(liveSnapshot)
		if liveSnapshot == "" || liveSnapshot != currentSnapshot {
			return fmt.Errorf("live VCS identity must match the new current snapshot")
		}
		if err := requireTransition(*state, "snapshot", ""); err != nil {
			return err
		}
		oldSnapshot := state.CurrentSnapshot
		isRepair := state.Actions["development-worker"].Status == developmentRepairPrepared
		state.CurrentSnapshot = currentSnapshot
		state.Actions["development-worker"] = ActionResult{Status: developmentComplete}
		if isRepair {
			state.PreRepairSnapshot = oldSnapshot
		} else {
			state.PreRepairSnapshot = ""
		}
		if !isRepair || state.QAExecution.Status != "PASS" || state.QAExecution.Snapshot != oldSnapshot {
			state.QAExecution = QAExecutionResult{Status: "PENDING"}
		}
		state.Carry = map[string]CarryResult{}
		for id, authorization := range state.SkipAuthorizations {
			if authorization.Origin == "SEAL" {
				delete(state.SkipAuthorizations, id)
			}
		}
		for id, result := range state.Gates {
			if !isSelected(*state, id) {
				continue
			}
			if result.Status != "PASS" {
				state.Gates[id] = GateResult{Status: "PENDING"}
			}
		}
		if isRepair && len(eligibleCarryGates(*state)) != 0 {
			state.Actions["carry"] = ActionResult{Status: "PENDING"}
		} else {
			delete(state.Actions, "carry")
		}
		return nil
	})
}

func RecordCarry(root, packageRoot, runID string, decisions []CarryInput, runtimeError string, mainAgent bool, mainReason, sourceRevision, sourceCatalogRevision, sourceSnapshot, liveSnapshot string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if err := requireLiveSnapshot(*state, liveSnapshot); err != nil {
			return err
		}
		if err := requireTransition(*state, "carry", ""); err != nil {
			return err
		}
		if mainAgent {
			if len(decisions) != 0 || strings.TrimSpace(runtimeError) != "" {
				return fmt.Errorf("main-agent Carry cannot include independent decisions or a runtime error")
			}
			if strings.TrimSpace(sourceRevision) != "" || strings.TrimSpace(sourceCatalogRevision) != "" || strings.TrimSpace(sourceSnapshot) != "" {
				return fmt.Errorf("main-agent Carry does not accept independent source bindings")
			}
			reason := strings.TrimSpace(mainReason)
			if reason == "" {
				return fmt.Errorf("main-agent Carry requires a reason")
			}
			if len(state.Carry) != 0 || repairRerunRecorded(*state) {
				return fmt.Errorf("main-agent Carry must be recorded before independent Carry or repair reruns")
			}
			eligible := eligibleMainCarryResults(*state)
			if len(eligible) == 0 {
				return fmt.Errorf("no prior passing selected results are eligible for main-agent Carry")
			}
			for _, id := range eligible {
				inheritCarryResult(state, id, carryOriginMainShortcut, reason)
			}
			state.Actions["carry"] = ActionResult{Status: "PASS"}
			completeReviewWaveIfReady(state)
			return nil
		}
		if strings.TrimSpace(mainReason) != "" {
			return fmt.Errorf("--main-reason requires main-agent Carry")
		}
		if err := requireSourceBinding(*state, sourceRevision, sourceCatalogRevision, sourceSnapshot, true); err != nil {
			return err
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
			completeReviewWaveIfReady(state)
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
			if decision.Decision == "INHERIT" {
				inheritCarryResult(state, decision.GateID, carryOriginIndependent, strings.TrimSpace(decision.Message))
			} else {
				state.Carry[decision.GateID] = CarryResult{Decision: decision.Decision, Origin: carryOriginIndependent, SourceSnapshot: state.PreRepairSnapshot, TargetSnapshot: state.CurrentSnapshot, Message: strings.TrimSpace(decision.Message)}
				state.Gates[decision.GateID] = GateResult{Status: "PENDING"}
			}
		}
		state.Actions["carry"] = ActionResult{Status: "PASS"}
		completeReviewWaveIfReady(state)
		return nil
	})
}

func eligibleMainCarryResults(state RunState) []string {
	if state.PreRepairSnapshot == "" {
		return nil
	}
	ids := []string{}
	if isSelected(state, "qa") && state.QAExecution.Status == "PASS" && state.QAExecution.Snapshot == state.PreRepairSnapshot {
		ids = append(ids, "qa")
	}
	return append(ids, eligibleCarryGates(state)...)
}

func repairRerunRecorded(state RunState) bool {
	if isSelected(state, "qa") && state.QAExecution.Snapshot == state.CurrentSnapshot && state.QAExecution.Status != "" && state.QAExecution.Status != "PENDING" {
		return true
	}
	for id := range selectedSet(state) {
		if id == "qa" {
			continue
		}
		result := state.Gates[id]
		if result.Snapshot == state.CurrentSnapshot && result.Status != "" && result.Status != "PENDING" {
			return true
		}
	}
	return false
}

func inheritCarryResult(state *RunState, id, origin, reason string) {
	state.Carry[id] = CarryResult{Decision: "INHERIT", Origin: origin, SourceSnapshot: state.PreRepairSnapshot, TargetSnapshot: state.CurrentSnapshot, Message: reason}
	if id == "qa" {
		state.QAExecution.Snapshot = state.CurrentSnapshot
		return
	}
	prior := state.Gates[id]
	prior.SourceSnapshot = state.PreRepairSnapshot
	prior.Snapshot = state.CurrentSnapshot
	state.Gates[id] = prior
}

func AuthorizeExtraRepair(root, packageRoot, runID string, cycles int) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if cycles <= 0 {
			return fmt.Errorf("extra review waves must be positive")
		}
		if state.CompletedReviewWaves < effectiveReviewWaveLimit(*state) {
			return fmt.Errorf("automatic review waves are not exhausted")
		}
		if !hasRepairableBlocker(*state) && !hasP2Recommendation(*state) {
			return fmt.Errorf("no recorded result requires another repair")
		}
		state.ExtraReviewWaves += cycles
		return nil
	})
}

func Abort(root, runID string) (RunSummary, error) { return finishRun(root, runID, "ABORTED") }

func Seal(root, packageRoot, runID, liveBefore, liveAfter string, skips []string) (RunSummary, error) {
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
	if liveBefore != state.CurrentSnapshot || liveAfter != state.CurrentSnapshot {
		return RunSummary{}, fmt.Errorf("live VCS identity must match the current snapshot before and after aggregation")
	}
	if err := authorizeSealSkips(&state, skips); err != nil {
		return RunSummary{}, err
	}
	if err := SaveRunState(root, state); err != nil {
		return RunSummary{}, err
	}
	if err := requireTransition(state, "seal", ""); err != nil {
		return RunSummary{}, err
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
	normalized, converted, err := validateSemanticResult(status, message, findings, false)
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Status: normalized, Message: strings.TrimSpace(message), Findings: converted}, nil
}

func semanticGateResult(status, message string, findings []FindingInput, state *RunState) (GateResult, error) {
	normalized, converted, err := validateSemanticResult(status, message, findings, true)
	if err != nil {
		return GateResult{}, err
	}
	return GateResult{Status: normalized, Message: strings.TrimSpace(message), Snapshot: state.CurrentSnapshot, Findings: converted}, nil
}

func validateSemanticResult(status, message string, findings []FindingInput, gateResult bool) (string, []Finding, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "PASS" && status != "FAIL" && status != "RUNTIME_ERROR" {
		return "", nil, fmt.Errorf("status must be PASS, FAIL, or RUNTIME_ERROR")
	}
	if status == "PASS" && len(findings) != 0 && !gateResult {
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
	hasBlocking := false
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
		severity := strings.ToUpper(strings.TrimSpace(input.Severity))
		if gateResult {
			if severity != "P0" && severity != "P1" && severity != "P2" {
				return "", nil, fmt.Errorf("gate finding severity must be P0, P1, or P2")
			}
			if severity == "P0" || severity == "P1" {
				hasBlocking = true
			}
		} else if severity != "" {
			return "", nil, fmt.Errorf("severity is accepted only for discovered-gate findings")
		}
		converted = append(converted, Finding{Severity: severity, Message: strings.TrimSpace(input.Message), Locations: locations})
	}
	if gateResult && status == "PASS" && hasBlocking {
		return "", nil, fmt.Errorf("PASS can include only P2 findings")
	}
	if gateResult && status == "FAIL" && !hasBlocking {
		return "", nil, fmt.Errorf("FAIL requires at least one P0 or P1 finding")
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
	routeMode := state.RouteMode
	selected := append([]string{}, state.SelectedGates...)
	routeSkips := map[string]SkipAuthorization{}
	for id, authorization := range state.SkipAuthorizations {
		if authorization.Origin == "ROUTE" {
			routeSkips[id] = authorization
		}
	}
	state.Actions = pendingRequirementActions()
	state.QAExecution = QAExecutionResult{Status: "PENDING"}
	state.Carry = map[string]CarryResult{}
	state.PreRepairSnapshot = ""
	state.Gates = map[string]GateResult{}
	for _, id := range gateIDs {
		state.Gates[id] = GateResult{Status: "PENDING"}
	}
	state.RouteMode = routeMode
	state.SelectedGates = selected
	state.SkipAuthorizations = routeSkips
	state.CompletedReviewWaves = 0
	state.ExtraReviewWaves = 0
}

func rebindCurrentSnapshot(state *RunState, snapshot string) {
	previous := state.CurrentSnapshot
	state.CurrentSnapshot = snapshot
	if previous == snapshot {
		return
	}
	if state.QAExecution.Snapshot == previous {
		state.QAExecution.Snapshot = snapshot
	}
	for id, result := range state.Gates {
		if result.Snapshot == previous {
			result.Snapshot = snapshot
			state.Gates[id] = result
		}
	}
	for id, authorization := range state.SkipAuthorizations {
		if authorization.Origin == "SEAL" && authorization.Snapshot == previous {
			authorization.Snapshot = snapshot
			state.SkipAuthorizations[id] = authorization
		}
	}
	for id, result := range state.Carry {
		if result.TargetSnapshot == previous {
			result.TargetSnapshot = snapshot
			state.Carry[id] = result
		}
	}
}

func eligibleCarryGates(state RunState) []string {
	var ids []string
	if state.PreRepairSnapshot == "" {
		return ids
	}
	for id, result := range state.Gates {
		if isSelected(state, id) && result.Status == "PASS" && result.Snapshot == state.PreRepairSnapshot {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func requireTransition(state RunState, operation, target string) error {
	if operation == "requirements-clarification" {
		if state.RequirementConfirmed {
			return fmt.Errorf("the current requirement is already confirmed")
		}
		return nil
	}
	if !state.RequirementConfirmed {
		return fmt.Errorf("the current requirement is not confirmed")
	}
	if operation == "route" {
		if state.RouteMode != "" {
			return fmt.Errorf("the run already has its one route decision")
		}
		return nil
	}
	if state.RouteMode == "" {
		return fmt.Errorf("the gate route is not confirmed")
	}
	switch operation {
	case "route-add":
		developmentStatus := state.Actions["development-worker"].Status
		if developmentStatus == developmentPrepared || developmentStatus == developmentRepairPrepared {
			return fmt.Errorf("the gate route cannot change while a development worker is prepared")
		}
		if state.PreRepairSnapshot != "" {
			return fmt.Errorf("the gate route cannot change while a repair snapshot requires verification")
		}
		return nil
	case "start-readiness":
		if developmentStarted(state) {
			return fmt.Errorf("Start Readiness must be recorded before development")
		}
	case "qa-design":
		if !isSelected(state, "qa") {
			return fmt.Errorf("QA is not selected")
		}
		if developmentStarted(state) {
			return fmt.Errorf("QA Design must be recorded before development")
		}
		if state.Actions["qa-design"].Status == "PASS" {
			return fmt.Errorf("the complete QA case set is awaiting QA Review")
		}
	case "qa-review":
		if !isSelected(state, "qa") {
			return fmt.Errorf("QA is not selected")
		}
		if developmentStarted(state) {
			return fmt.Errorf("QA Review must be recorded before development")
		}
		if state.Actions["qa-design"].Status != "PASS" || len(state.QACases) == 0 {
			return fmt.Errorf("a complete QA case set is required before QA Review")
		}
		if status := state.Actions["qa-review"].Status; status == "PASS" || status == "FAIL" {
			return fmt.Errorf("QA Review already has an authoritative %s result for the current case set", status)
		}
	case "development-worker":
		developmentStatus := state.Actions["development-worker"].Status
		if developmentStatus != developmentPending && developmentStatus != developmentPrepared && developmentStatus != developmentRepairPrepared && developmentStatus != developmentComplete && developmentStatus != developmentVerified {
			return fmt.Errorf("development worker is already prepared")
		}
		if state.Actions["start-readiness"].Status != "PASS" {
			return fmt.Errorf("Start Readiness must pass before development")
		}
		if isSelected(state, "qa") && state.Actions["qa-review"].Status != "PASS" {
			return fmt.Errorf("QA Review must pass before development starts")
		}
		if developmentStatus == developmentPrepared || developmentStatus == developmentRepairPrepared {
			return nil
		}
		if developmentStatus == developmentComplete || developmentStatus == developmentVerified {
			if !reviewWaveRecorded(state) {
				if state.PreRepairSnapshot != "" {
					return fmt.Errorf("the current repair still requires verification")
				}
				return fmt.Errorf("all selected review results must be recorded before repair")
			}
			if !hasRepairableBlocker(state) && !hasP2Recommendation(state) {
				if state.PreRepairSnapshot != "" {
					return fmt.Errorf("the current repair still requires verification")
				}
				return fmt.Errorf("no recorded result requires repair")
			}
			hasRuntimeError := false
			for id := range selectedSet(state) {
				if selectedResultStatus(state, id) == "RUNTIME_ERROR" {
					hasRuntimeError = true
					break
				}
			}
			if (developmentStatus != developmentVerified || hasRuntimeError) && !runtimeErrorsAuthorizedForRepair(state) {
				if state.PreRepairSnapshot != "" {
					return fmt.Errorf("the current repair still requires verification")
				}
				return fmt.Errorf("the current review wave is not complete")
			}
			if state.CompletedReviewWaves >= effectiveReviewWaveLimit(state) {
				return fmt.Errorf("review-wave limit is exhausted; explicit additional repair authorization is required")
			}
		}
	case "snapshot":
		developmentStatus := state.Actions["development-worker"].Status
		if developmentStatus != developmentPending && developmentStatus != developmentPrepared && developmentStatus != developmentRepairPrepared {
			return fmt.Errorf("development must be pending or prepared before a snapshot")
		}
		if state.Actions["start-readiness"].Status != "PASS" {
			return fmt.Errorf("Start Readiness must pass before a development snapshot")
		}
		if isSelected(state, "qa") && state.Actions["qa-review"].Status != "PASS" {
			return fmt.Errorf("QA Review must pass before a development snapshot")
		}
		if state.PreRepairSnapshot != "" && !runtimeErrorsAuthorizedForRepair(state) {
			return fmt.Errorf("the current repair still requires verification")
		}
		if developmentStatus == developmentRepairPrepared {
			if !reviewWaveRecorded(state) {
				return fmt.Errorf("all selected review results must be recorded before repair")
			}
			if !hasRepairableBlocker(state) && !hasP2Recommendation(state) {
				return fmt.Errorf("no recorded result requires repair")
			}
			if state.CompletedReviewWaves >= effectiveReviewWaveLimit(state) {
				return fmt.Errorf("review-wave limit is exhausted; explicit additional repair authorization is required")
			}
		}
	case "qa-execution":
		if !isSelected(state, "qa") {
			return fmt.Errorf("QA is not selected")
		}
		if !hasDevelopmentSnapshot(state) {
			return fmt.Errorf("an immutable development snapshot is required before QA Execution")
		}
		if state.Actions["qa-design"].Status != "PASS" {
			return fmt.Errorf("QA Design must pass before QA Execution")
		}
	case "gate":
		if !isSelected(state, target) {
			return fmt.Errorf("gate %q is not selected", target)
		}
		if !hasDevelopmentSnapshot(state) {
			return fmt.Errorf("an immutable development snapshot is required before post-development review")
		}
		if state.Actions["start-readiness"].Status != "PASS" {
			return fmt.Errorf("Start Readiness must pass before post-development review")
		}
	case "carry":
		if !hasDevelopmentSnapshot(state) || state.PreRepairSnapshot == "" {
			return fmt.Errorf("a repaired immutable snapshot is required before Carry")
		}
	case "seal":
		if !hasDevelopmentSnapshot(state) {
			return fmt.Errorf("an immutable development snapshot is required before Seal")
		}
		if state.Actions["start-readiness"].Status != "PASS" {
			return fmt.Errorf("Start Readiness must pass before Seal")
		}
		if err := requireSelectedResultsResolved(state); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown workflow transition %q", operation)
	}
	return nil
}

func developmentStarted(state RunState) bool {
	return state.Actions["development-worker"].Status != developmentPending
}

func hasDevelopmentSnapshot(state RunState) bool {
	status := state.Actions["development-worker"].Status
	return status == developmentComplete || status == developmentVerified
}

func semanticResultRecorded(status, snapshot, currentSnapshot string) bool {
	return snapshot == currentSnapshot && (status == "PASS" || status == "FAIL")
}

func normalizeSelected(values, candidates []string) ([]string, error) {
	allowed := map[string]bool{}
	for _, id := range candidates {
		allowed[id] = true
	}
	chosen := map[string]bool{}
	for _, value := range values {
		id := strings.TrimSpace(value)
		if !allowed[id] {
			return nil, fmt.Errorf("gate %q is not in the current route candidates", id)
		}
		if chosen[id] {
			return nil, fmt.Errorf("duplicate selected gate %q", id)
		}
		chosen[id] = true
	}
	return orderedSelection(chosen, candidates), nil
}

func orderedSelection(chosen map[string]bool, candidates []string) []string {
	selected := []string{}
	for _, id := range candidates {
		if chosen[id] {
			selected = append(selected, id)
		}
	}
	return selected
}

func selectedSet(state RunState) map[string]bool {
	selected := map[string]bool{}
	for _, id := range state.SelectedGates {
		selected[id] = true
	}
	return selected
}

func isSelected(state RunState, id string) bool { return selectedSet(state)[id] }

func reviewWaveRecorded(state RunState) bool {
	if isSelected(state, "qa") && (state.QAExecution.Snapshot != state.CurrentSnapshot || state.QAExecution.Status == "PENDING" || state.QAExecution.Status == "") {
		return false
	}
	for id := range selectedSet(state) {
		if id == "qa" {
			continue
		}
		result := state.Gates[id]
		if result.Snapshot != state.CurrentSnapshot || result.Status == "PENDING" || result.Status == "" {
			return false
		}
	}
	return true
}

func hasRepairableBlocker(state RunState) bool {
	if isSelected(state, "qa") && state.QAExecution.Status == "FAIL" && state.QAExecution.Snapshot == state.CurrentSnapshot {
		return true
	}
	for id := range selectedSet(state) {
		if id != "qa" && state.Gates[id].Status == "FAIL" && state.Gates[id].Snapshot == state.CurrentSnapshot {
			return true
		}
	}
	return false
}

func hasP2Recommendation(state RunState) bool {
	for id := range selectedSet(state) {
		if id == "qa" {
			continue
		}
		result := state.Gates[id]
		if result.Snapshot != state.CurrentSnapshot {
			continue
		}
		for _, finding := range result.Findings {
			if finding.Severity == "P2" {
				return true
			}
		}
	}
	return false
}

func runtimeErrorsAuthorizedForRepair(state RunState) bool {
	foundRuntime := false
	for id := range selectedSet(state) {
		if selectedResultStatus(state, id) != "RUNTIME_ERROR" {
			continue
		}
		foundRuntime = true
		authorization, ok := state.SkipAuthorizations[id]
		if !ok || authorization.Origin != "SEAL" || authorization.Status != "RUNTIME_ERROR" || authorization.Snapshot != state.CurrentSnapshot {
			return false
		}
	}
	return foundRuntime
}

func repairInput(state RunState) string {
	lines := []string{"Repair the complete recorded wave below. P2 recommendations are included whenever this wave has a blocker or the user explicitly requested their repair."}
	if isSelected(state, "qa") && state.QAExecution.Status == "FAIL" {
		for _, finding := range state.QAExecution.Findings {
			lines = append(lines, "QA FAIL: "+finding.Message)
		}
	}
	for _, id := range state.SelectedGates {
		if id == "qa" {
			continue
		}
		for _, finding := range state.Gates[id].Findings {
			lines = append(lines, fmt.Sprintf("%s %s: %s", id, finding.Severity, finding.Message))
		}
	}
	return strings.Join(lines, "\n")
}

func effectiveReviewWaveLimit(state RunState) int {
	return automaticReviewWaveLimit + state.ExtraReviewWaves
}

func completeReviewWaveIfReady(state *RunState) {
	if len(state.SelectedGates) == 0 || state.Actions["development-worker"].Status != developmentComplete || !reviewWaveRecorded(*state) {
		return
	}
	if isSelected(*state, "qa") && state.QAExecution.Status == "RUNTIME_ERROR" {
		return
	}
	for id := range selectedSet(*state) {
		if id != "qa" && state.Gates[id].Status == "RUNTIME_ERROR" {
			return
		}
	}
	if len(eligibleCarryGates(*state)) != 0 || state.Actions["carry"].Status == "RUNTIME_ERROR" {
		return
	}
	state.CompletedReviewWaves++
	state.Actions["development-worker"] = ActionResult{Status: developmentVerified}
	state.PreRepairSnapshot = ""
}

func authorizeSealSkips(state *RunState, skips []string) error {
	wanted := map[string]bool{}
	for _, raw := range skips {
		id := strings.TrimSpace(raw)
		if wanted[id] {
			return fmt.Errorf("duplicate Seal skip %q", id)
		}
		if !isSelected(*state, id) {
			return fmt.Errorf("Seal skip %q is not a selected gate", id)
		}
		status := selectedResultStatus(*state, id)
		if status == "PENDING" || status == "" {
			return fmt.Errorf("selected gate %q is PENDING and cannot be skipped", id)
		}
		if status == "PASS" {
			return fmt.Errorf("selected gate %q already passed", id)
		}
		if status == "FAIL" && state.CompletedReviewWaves < effectiveReviewWaveLimit(*state) {
			return fmt.Errorf("selected gate %q cannot be skipped before the review-wave limit is exhausted", id)
		}
		wanted[id] = true
	}
	for id := range wanted {
		state.SkipAuthorizations[id] = SkipAuthorization{Origin: "SEAL", Status: selectedResultStatus(*state, id), Snapshot: state.CurrentSnapshot}
	}
	return nil
}

func requireSelectedResultsResolved(state RunState) error {
	for id := range selectedSet(state) {
		status := selectedResultStatus(state, id)
		if status == "PENDING" || status == "" {
			return fmt.Errorf("selected gate %q is PENDING", id)
		}
		snapshot := state.Gates[id].Snapshot
		if id == "qa" {
			snapshot = state.QAExecution.Snapshot
		}
		if snapshot != state.CurrentSnapshot {
			return fmt.Errorf("selected gate %q has not been verified at the current snapshot", id)
		}
		if status == "PASS" {
			continue
		}
		authorization, ok := state.SkipAuthorizations[id]
		if !ok || authorization.Origin != "SEAL" || authorization.Status != status || authorization.Snapshot != state.CurrentSnapshot {
			return fmt.Errorf("selected gate %q with status %s requires explicit Seal skip authorization", id, status)
		}
	}
	return nil
}

func selectedResultStatus(state RunState, id string) string {
	if id == "qa" {
		return state.QAExecution.Status
	}
	return state.Gates[id].Status
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
