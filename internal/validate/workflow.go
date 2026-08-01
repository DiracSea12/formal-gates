package validate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type StartOptions struct {
	Root, PackageRoot, RunID, Flow, RequirementSource, VCS, BaseSnapshot string
	RequirementArtifacts                                                 []string
	RequirementConfirmed, RetainedOverall                                bool
}

type FindingInput struct {
	Severity  string
	Message   string
	Locations []string
}

type QACaseInput struct{ Kind, Description, Procedure, Oracle string }

type QAReviewInput struct{ CaseID, Outcome, Reason string }

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
	for name, value := range map[string]string{"flow": options.Flow, "requirement": options.RequirementSource, "VCS": options.VCS} {
		if strings.TrimSpace(value) == "" {
			return RunState{}, fmt.Errorf("%s is required", name)
		}
	}
	if strings.TrimSpace(options.Flow) != formalFlow {
		return RunState{}, fmt.Errorf("flow must be formal")
	}
	if options.RequirementConfirmed {
		return RunState{}, fmt.Errorf("a run cannot start with a pre-confirmed requirement; record Requirements Clarification first")
	}
	vcs := strings.ToLower(strings.TrimSpace(options.VCS))
	resolver, err := resolverForVCS(vcs, nil)
	if err != nil {
		return RunState{}, err
	}
	currentSnapshot, err := resolver.Resolve(root)
	if err != nil {
		return RunState{}, err
	}
	if supplied := strings.TrimSpace(options.BaseSnapshot); supplied != "" {
		if err := resolver.Verify(root, supplied); err != nil {
			return RunState{}, err
		}
		if !strings.EqualFold(supplied, currentSnapshot) {
			return RunState{}, fmt.Errorf("native current snapshot does not match the requested base snapshot")
		}
	}
	catalog, err := LoadPromptCatalog(options.PackageRoot)
	if err != nil {
		return RunState{}, err
	}
	artifacts, err := requirementArtifactSet(root, options.RequirementSource, options.RequirementArtifacts)
	if err != nil {
		return RunState{}, err
	}
	revision := artifactRevision(artifacts, normalizeArtifactPath(root, options.RequirementSource))
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
	if err := workflowLifecycle.Begin(root, runID); err != nil {
		_ = os.RemoveAll(RunDir(root, runID))
		return RunState{}, err
	}
	state := NewRunState(runID, strings.TrimSpace(options.Flow), normalizeArtifactPath(root, options.RequirementSource), revision, vcs, currentSnapshot, currentSnapshot, catalog.BaseRevision, catalog.CatalogRevision, options.RequirementConfirmed, catalog.GateIDs(), artifacts)
	state.RetainedOverall = options.RetainedOverall
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
	changed, err := requirementArtifactsChanged(root, state.RequirementArtifacts)
	return state, changed, err
}

func UpdateRequirement(root, packageRoot, runID, source string, confirmed bool, semanticEffect string, artifactPaths []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentCatalog(*state, packageRoot)
		if err != nil {
			return err
		}
		oldSource := state.RequirementSource
		if strings.TrimSpace(source) == "" {
			source = state.RequirementSource
		}
		source = normalizeArtifactPath(root, source)
		additional := artifactPaths
		if additional == nil {
			for _, artifact := range state.RequirementArtifacts {
				if artifact.Path != oldSource && artifact.Path != source {
					additional = append(additional, artifact.Path)
				}
			}
		}
		artifacts, err := requirementArtifactSet(cleanRoot(root), source, additional)
		if err != nil {
			return err
		}
		revision := artifactRevision(artifacts, source)
		changed := revision != state.RequirementRevision || source != state.RequirementSource || !sameArtifactSet(artifacts, state.RequirementArtifacts)
		semanticEffect = strings.ToLower(strings.TrimSpace(semanticEffect))
		if changed {
			if semanticEffect != "preserved" && semanticEffect != "changed" {
				return fmt.Errorf("changed requirement requires semantic effect preserved or changed")
			}
			liveSnapshot, err := resolveNativeSnapshot(root, state.VCS)
			if err != nil {
				return err
			}
			if developmentStarted(*state) && semanticEffect == "preserved" {
				return fmt.Errorf("meaning-preserved requirement rebinding is unavailable after development starts")
			}
			state.RequirementSource, state.RequirementRevision, state.RequirementArtifacts = source, revision, artifacts
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

func PrepareGate(root, packageRoot, runID, gateID string) (string, error) {
	return prepareBoundPrompt(root, packageRoot, runID, gateID, "gate", true, func(state *RunState, catalog PromptCatalog, route PromptRoute) (string, error) {
		if err := requireTransition(*state, "gate", gateID); err != nil {
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
		return ComposeGatePrompt(catalog, gateID, route)
	})
}

func PrepareAction(root, packageRoot, runID, actionID string) (string, error) {
	if actionID == "development-worker" {
		return prepareDevelopmentAction(root, packageRoot, runID)
	}
	reviewerRequired := actionID != "requirements-clarification"
	return prepareBoundPrompt(root, packageRoot, runID, actionID, "action", reviewerRequired, func(state *RunState, catalog PromptCatalog, route PromptRoute) (string, error) {
		if err := requireTransition(*state, actionID, ""); err != nil {
			return "", err
		}
		if actionID == "qa-execution" && semanticResultRecorded(state.QAExecution.Status, state.QAExecution.Snapshot, state.CurrentSnapshot) {
			return "", fmt.Errorf("QA Execution already has an authoritative %s result for the current snapshot", state.QAExecution.Status)
		}
		detail, err := actionPromptDetail(*state, catalog, actionID)
		if err != nil {
			return "", err
		}
		return ComposeActionPrompt(catalog, actionID, route, detail)
	})
}

func prepareBoundPrompt(root, packageRoot, runID, target, targetKind string, reviewerRequired bool, compose func(*RunState, PromptCatalog, PromptRoute) (string, error)) (string, error) {
	prompt := ""
	_, err := mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		wave := 0
		if targetKind == "gate" {
			wave = currentGateReviewWave(*state)
		}
		attempt := nextDispatchAttempt(*state, targetKind, target, wave)
		dispatchID, err := newDispatchID()
		if err != nil {
			return err
		}
		route := routeForState(root, *state)
		route.DispatchID, route.DispatchAttempt, route.ReviewWave = dispatchID, attempt, wave
		prompt, err = compose(state, catalog, route)
		if err != nil {
			return err
		}
		staleOpenDispatches(state, targetKind, target)
		sum := sha256.Sum256([]byte(prompt))
		state.Dispatches[dispatchID] = PreparedDispatch{ID: dispatchID, Target: target, TargetKind: targetKind, Attempt: attempt, ReviewWave: wave, PromptHash: hex.EncodeToString(sum[:]), RequirementRevision: state.RequirementRevision, CatalogRevision: state.CatalogRevision, SourceSnapshot: state.CurrentSnapshot, ReviewerRequired: reviewerRequired, Status: "OPEN"}
		return nil
	})
	return prompt, err
}

func actionPromptDetail(state RunState, catalog PromptCatalog, actionID string) (string, error) {
	if actionID == "qa-design" && len(state.QACases) != 0 && state.Actions["qa-design"].Status != "PASS" {
		lines := []string{"Review the complete current requirement and every prior case below. Return the complete resulting case set. Retain exact unaffected passing cases and add, modify, or remove only affected cases when impact is reliably bounded; replace the complete set when it is not or the overall workflow changed."}
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
			lines = append(lines, formatQACase(testCase, true))
		}
		return strings.Join(lines, "\n\n"), nil
	}
	if actionID == "qa-review" {
		if len(state.QACases) == 0 {
			return "", fmt.Errorf("QA cases are missing")
		}
		accepted := []string{"Accepted coverage context; do not return new decisions for these cases:"}
		pending := []string{"Return one decision for every pending case below:"}
		for _, testCase := range state.QACases {
			if testCase.ReviewStatus == "PASS" {
				accepted = append(accepted, fmt.Sprintf("%s: %s", testCase.ID, testCase.Description))
			} else {
				pending = append(pending, formatQACase(testCase, false))
			}
		}
		if len(pending) == 1 {
			accepted = append(accepted, "There are no pending case decisions. Review the corrected complete set for set-level missing or duplicated coverage and return no case decisions.")
			return strings.Join(accepted, "\n\n"), nil
		}
		if len(accepted) == 1 {
			return strings.Join(pending, "\n\n"), nil
		}
		return strings.Join(append(accepted, pending...), "\n\n"), nil
	}
	if actionID == "qa-execution" {
		if len(state.QACases) == 0 {
			return "", fmt.Errorf("QA cases are missing")
		}
		var lines []string
		for _, testCase := range state.QACases {
			lines = append(lines, formatQACase(testCase, false))
		}
		return strings.Join(lines, "\n\n"), nil
	}
	if actionID == "carry" {
		eligible := eligibleCarryGates(state)
		if len(eligible) == 0 {
			return "", fmt.Errorf("no prior passing gates require a Carry decision")
		}
		lines := []string{"Decide INHERIT or RERUN for each gate below:"}
		for _, id := range eligible {
			gate, _ := catalog.Gate(id)
			lines = append(lines, fmt.Sprintf("\n[Gate: %s]\n%s", id, gate.Content))
		}
		return strings.Join(lines, "\n"), nil
	}
	return "", nil
}

func formatQACase(testCase QACase, includeReview bool) string {
	value := fmt.Sprintf("%s\nkind: %s\ndescription: %s\nprocedure: %s\noracle: %s", testCase.ID, testCase.Kind, testCase.Description, testCase.Procedure, testCase.Oracle)
	if includeReview {
		value += "\nreview status: " + testCase.ReviewStatus
	}
	return value
}

func prepareDevelopmentAction(root, packageRoot, runID string) (string, error) {
	return prepareBoundPrompt(root, packageRoot, runID, "development-worker", "action", true, func(state *RunState, catalog PromptCatalog, route PromptRoute) (string, error) {
		if state.RetainedOverall {
			return "", fmt.Errorf("a retained overall run keeps implementation and repair ownership in slice runs; record merged slice snapshots with workflow snapshot")
		}
		if err := requireTransition(*state, "development-worker", ""); err != nil {
			return "", err
		}
		detail := ""
		status := state.Actions["development-worker"].Status
		if status == developmentComplete || status == developmentVerified || status == developmentRepairPrepared {
			detail = repairInput(*state)
		}
		prompt, err := ComposeActionPrompt(catalog, "development-worker", route, detail)
		if err != nil {
			return "", err
		}
		if status == developmentComplete || status == developmentVerified {
			state.Actions["development-worker"] = ActionResult{Status: developmentRepairPrepared}
		} else if status == developmentPending {
			state.Actions["development-worker"] = ActionResult{Status: developmentPrepared}
		}
		return prompt, nil
	})
}

func ClaimDispatch(root, packageRoot, runID, dispatchID, reviewerIdentity string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		dispatchID, reviewerIdentity = strings.TrimSpace(dispatchID), strings.TrimSpace(reviewerIdentity)
		if dispatchID == "" {
			return fmt.Errorf("dispatch id is required")
		}
		dispatch, ok := state.Dispatches[dispatchID]
		if !ok {
			return fmt.Errorf("unknown dispatch %q", dispatchID)
		}
		if !dispatch.ReviewerRequired {
			return fmt.Errorf("dispatch %q does not require a reviewer claim", dispatchID)
		}
		if dispatch.Status != "OPEN" {
			return fmt.Errorf("dispatch %q is %s and cannot be claimed", dispatchID, dispatch.Status)
		}
		if reviewerIdentity == "" {
			return fmt.Errorf("reviewer identity is required")
		}
		for priorID, prior := range state.Dispatches {
			if prior.ReviewerIdentity == reviewerIdentity {
				return fmt.Errorf("reviewer identity is already reserved by dispatch %s", priorID)
			}
		}
		if err := workflowLifecycle.Bind(root, state.RunID, dispatchID, reviewerIdentity); err != nil {
			return err
		}
		dispatch.ReviewerIdentity, dispatch.Status = reviewerIdentity, "CLAIMED"
		state.Dispatches[dispatchID] = dispatch
		return nil
	})
}

func RecordAction(root, packageRoot, runID, actionID, dispatchID, status, message string, findings []FindingInput) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if _, ok := catalog.Action(actionID); !ok {
			return fmt.Errorf("unknown action prompt %q", actionID)
		}
		if actionID != "requirements-clarification" && actionID != "start-readiness" {
			return fmt.Errorf("action %q has a dedicated workflow command and cannot use record-action", actionID)
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", actionID)
		if err != nil {
			return err
		}
		if err := requireTransition(*state, actionID, ""); err != nil {
			return err
		}
		if actionID == "start-readiness" {
			if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
				return err
			}
		}
		backfillDispatchCost(root, state, dispatch)
		result, err := semanticActionResult(status, message, findings, state)
		if err != nil {
			return err
		}
		result.DispatchID = dispatch.ID
		state.Actions[actionID] = result
		completeDispatch(state, dispatch.ID)
		return nil
	})
}

func RecordGate(root, packageRoot, runID, gateID, dispatchID, status, message string, findings []FindingInput) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if _, ok := catalog.Gate(gateID); !ok {
			return fmt.Errorf("gate %q is not discovered", gateID)
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "gate", gateID)
		if err != nil {
			return err
		}
		if err := requireTransition(*state, "gate", gateID); err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
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
		if err := rejectFrozenArtifactFindings(*state, result.Findings); err != nil {
			return err
		}
		result.DispatchID = dispatch.ID
		state.Gates[gateID] = result
		completeDispatch(state, dispatch.ID)
		completeReviewWaveIfReady(state)
		return nil
	})
}

func RecordQADesign(root, packageRoot, runID, dispatchID string, cases []QACaseInput, runtimeError string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if err := requireTransition(*state, "qa-design", ""); err != nil {
			return err
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", "qa-design")
		if err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
		if strings.TrimSpace(runtimeError) != "" {
			if len(cases) != 0 {
				return fmt.Errorf("QA Design runtime error cannot include cases")
			}
			state.QAExecution = QAExecutionResult{Status: "PENDING"}
			state.Actions["qa-design"] = ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), DispatchID: dispatch.ID}
			completeDispatch(state, dispatch.ID)
			return nil
		}
		if len(cases) == 0 {
			return fmt.Errorf("at least one QA case is required")
		}
		seen := map[string]bool{}
		priorByKey := map[string]QACase{}
		usedIDs := map[string]bool{}
		for _, prior := range state.QACases {
			usedIDs[prior.ID] = true
			priorByKey[qaCaseSemanticKey(prior.Kind, prior.Description, prior.Procedure, prior.Oracle)] = prior
		}
		nextID := 1
		updated := make([]QACase, 0, len(cases))
		kinds := map[string]bool{}
		for index, item := range cases {
			normalized := QACase{
				Kind:        strings.ToUpper(strings.TrimSpace(item.Kind)),
				Description: strings.TrimSpace(item.Description),
				Procedure:   strings.TrimSpace(item.Procedure),
				Oracle:      strings.TrimSpace(item.Oracle),
			}
			if normalized.Kind != "STATIC" && normalized.Kind != "LIVE" {
				return fmt.Errorf("QA case %d kind must be STATIC or LIVE", index+1)
			}
			for name, value := range map[string]string{"description": normalized.Description, "procedure": normalized.Procedure, "oracle": normalized.Oracle} {
				if value == "" {
					return fmt.Errorf("QA case %d %s is required", index+1, name)
				}
			}
			key := qaCaseSemanticKey(normalized.Kind, normalized.Description, normalized.Procedure, normalized.Oracle)
			if seen[key] {
				return fmt.Errorf("duplicate QA case %d", index+1)
			}
			seen[key] = true
			kinds[normalized.Kind] = true
			if prior, ok := priorByKey[key]; ok {
				normalized.ID = prior.ID
				if prior.ReviewStatus == "PASS" {
					normalized.ReviewStatus = "PASS"
				} else {
					normalized.ReviewStatus = "PENDING"
				}
			} else {
				for usedIDs[fmt.Sprintf("CASE-%03d", nextID)] {
					nextID++
				}
				normalized.ID, normalized.ReviewStatus = fmt.Sprintf("CASE-%03d", nextID), "PENDING"
				usedIDs[normalized.ID] = true
				nextID++
			}
			updated = append(updated, normalized)
		}
		if !kinds["STATIC"] || !kinds["LIVE"] {
			return fmt.Errorf("complete QA case set requires at least one STATIC and one LIVE case")
		}
		if state.Actions["qa-review"].Status == "FAIL" {
			pending := false
			for _, testCase := range updated {
				if testCase.ReviewStatus != "PASS" {
					pending = true
					break
				}
			}
			if !pending {
				if len(updated) == len(state.QACases) {
					return fmt.Errorf("QA Design rework must add or revise a case, or remove an obsolete or duplicated case")
				}
			}
		}
		state.QACases = updated
		state.QAExecution = QAExecutionResult{Status: "PENDING"}
		state.Actions["qa-design"] = ActionResult{Status: "PASS", DispatchID: dispatch.ID}
		state.Actions["qa-review"] = ActionResult{Status: "PENDING"}
		completeDispatch(state, dispatch.ID)
		return nil
	})
}

func RecordQAReview(root, packageRoot, runID, dispatchID string, decisions []QAReviewInput, runtimeError string, setFindings []FindingInput) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if err := requireTransition(*state, "qa-review", ""); err != nil {
			return err
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", "qa-review")
		if err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
		if strings.TrimSpace(runtimeError) != "" {
			if len(decisions) != 0 || len(setFindings) != 0 {
				return fmt.Errorf("QA Review runtime error cannot include case decisions or findings")
			}
			state.Actions["qa-review"] = ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), DispatchID: dispatch.ID}
			completeDispatch(state, dispatch.ID)
			return nil
		}
		pending := map[string]int{}
		for index, testCase := range state.QACases {
			if testCase.ReviewStatus != "PASS" {
				pending[testCase.ID] = index
			}
		}
		if len(decisions) != len(pending) {
			return fmt.Errorf("QA Review must decide all %d pending cases", len(pending))
		}
		seen := map[string]bool{}
		findings := make([]Finding, 0, len(decisions)+len(setFindings))
		status := "PASS"
		for _, input := range decisions {
			caseID := strings.TrimSpace(input.CaseID)
			index, ok := pending[caseID]
			if !ok {
				return fmt.Errorf("QA Review case %q is not pending in this dispatch", input.CaseID)
			}
			if seen[caseID] {
				return fmt.Errorf("duplicate QA Review decision for %s", caseID)
			}
			seen[caseID] = true
			outcome := strings.ToUpper(strings.TrimSpace(input.Outcome))
			if outcome != "PASS" && outcome != "FAIL" {
				return fmt.Errorf("QA Review outcome for %s must be PASS or FAIL", caseID)
			}
			reason := strings.TrimSpace(input.Reason)
			if outcome == "FAIL" && reason == "" {
				return fmt.Errorf("QA Review FAIL for %s requires a reason", caseID)
			}
			state.QACases[index].ReviewStatus = outcome
			if outcome == "FAIL" {
				status = "FAIL"
				findings = append(findings, Finding{Message: caseID + ": " + reason})
			}
		}
		for _, input := range setFindings {
			if strings.TrimSpace(input.Severity) != "" {
				return fmt.Errorf("QA Review findings do not accept severity")
			}
			if strings.TrimSpace(input.Message) == "" {
				return fmt.Errorf("QA Review finding message is required")
			}
			locations := make([]string, 0, len(input.Locations))
			for _, location := range input.Locations {
				if err := validateFindingLocation(location); err != nil {
					return err
				}
				locations = append(locations, strings.TrimSpace(location))
			}
			findings = append(findings, Finding{Message: strings.TrimSpace(input.Message), Locations: locations})
			status = "FAIL"
		}
		state.Actions["qa-review"] = ActionResult{Status: status, Findings: findings, DispatchID: dispatch.ID}
		if status == "FAIL" {
			state.Actions["qa-design"] = ActionResult{Status: "PENDING"}
			state.QAExecution = QAExecutionResult{Status: "PENDING"}
		}
		completeDispatch(state, dispatch.ID)
		return nil
	})
}

func RecordQAExecution(root, packageRoot, runID, dispatchID string, results []QAResultInput, runtimeError string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if err := requireTransition(*state, "qa-execution", ""); err != nil {
			return err
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", "qa-execution")
		if err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
		if semanticResultRecorded(state.QAExecution.Status, state.QAExecution.Snapshot, state.CurrentSnapshot) {
			return fmt.Errorf("QA Execution already has an authoritative %s result for the current snapshot", state.QAExecution.Status)
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(results) != 0 {
				return fmt.Errorf("QA runtime error cannot include case results")
			}
			state.QAExecution = QAExecutionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), Snapshot: state.CurrentSnapshot}
			completeDispatch(state, dispatch.ID)
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
			recorded = append(recorded, QAResultRecord{CaseID: item.CaseID, Kind: testCase.Kind, Outcome: item.Outcome, Procedure: strings.TrimSpace(item.Procedure), Observation: strings.TrimSpace(item.Observation), OracleResult: strings.TrimSpace(item.OracleResult)})
		}
		state.QAExecution = QAExecutionResult{Status: status, Snapshot: state.CurrentSnapshot, Cases: recorded, Findings: findings}
		completeDispatch(state, dispatch.ID)
		completeReviewWaveIfReady(state)
		return nil
	})
}

func AdvanceSnapshot(root, packageRoot, runID, dispatchID string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		currentSnapshot, err := resolveNativeSnapshot(root, state.VCS)
		if err != nil {
			return err
		}
		if err := verifySnapshotReady(root, state.VCS); err != nil {
			return err
		}
		developmentStatus := state.Actions["development-worker"].Status
		if currentSnapshot == state.CurrentSnapshot && developmentStatus != developmentPrepared {
			return fmt.Errorf("a new current snapshot is required")
		}
		if err := verifyNativeSnapshot(root, state.VCS, state.CurrentSnapshot); err != nil {
			return err
		}
		if err := requireTransition(*state, "snapshot", ""); err != nil {
			return err
		}
		var developmentDispatch PreparedDispatch
		if state.RetainedOverall {
			if strings.TrimSpace(dispatchID) != "" {
				return fmt.Errorf("a retained overall snapshot does not accept a development dispatch")
			}
		} else {
			developmentDispatch, err = requirePreparedDispatch(*state, dispatchID, "action", "development-worker")
			if err != nil {
				return err
			}
			if err := requireLifecycleVerification(root, *state, developmentDispatch); err != nil {
				return err
			}
			backfillDispatchCost(root, state, developmentDispatch)
		}
		oldSnapshot := state.CurrentSnapshot
		isRepair := developmentStatus == developmentRepairPrepared ||
			(state.RetainedOverall && (developmentStatus == developmentComplete || developmentStatus == developmentVerified))
		state.CurrentSnapshot = currentSnapshot
		state.Actions["development-worker"] = ActionResult{Status: developmentComplete, DispatchID: developmentDispatch.ID}
		if developmentDispatch.ID != "" {
			completeDispatch(state, developmentDispatch.ID)
		}
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
			if isSealScopedAuthorization(authorization) {
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

func RecordCarry(root, packageRoot, runID, dispatchID string, decisions []CarryInput, runtimeError string, mainAgent bool, mainReason string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if err := requireTransition(*state, "carry", ""); err != nil {
			return err
		}
		if mainAgent {
			if len(decisions) != 0 || strings.TrimSpace(runtimeError) != "" {
				return fmt.Errorf("main-agent Carry cannot include independent decisions or a runtime error")
			}
			if strings.TrimSpace(dispatchID) != "" {
				return fmt.Errorf("main-agent Carry does not accept an independent dispatch")
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
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", "carry")
		if err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
		eligible := eligibleCarryGates(*state)
		if len(eligible) == 0 {
			return fmt.Errorf("no prior passing gates require a Carry decision")
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(decisions) != 0 {
				return fmt.Errorf("Carry runtime error cannot include decisions")
			}
			state.Actions["carry"] = ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), DispatchID: dispatch.ID}
			completeDispatch(state, dispatch.ID)
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
		state.Actions["carry"] = ActionResult{Status: "PASS", DispatchID: dispatch.ID}
		completeDispatch(state, dispatch.ID)
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
		if cycles != 1 {
			return fmt.Errorf("each extra repair authorization must add exactly one review wave")
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

func Seal(root, packageRoot, runID string, skips []string, userRequested bool) (RunSummary, error) {
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
	before, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return RunSummary{}, err
	}
	if before != state.CurrentSnapshot {
		return RunSummary{}, fmt.Errorf("native VCS identity does not match the current snapshot before aggregation")
	}
	if err := authorizeSealSkips(&state, skips, userRequested); err != nil {
		return RunSummary{}, err
	}
	if err := requireTransition(state, "seal", ""); err != nil {
		if saveErr := SaveRunState(root, state); saveErr != nil {
			return RunSummary{}, saveErr
		}
		return RunSummary{}, err
	}
	after, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return RunSummary{}, err
	}
	if after != state.CurrentSnapshot {
		return RunSummary{}, fmt.Errorf("native VCS identity does not match the current snapshot after aggregation")
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
	_, _ = CleanupTempRuns(root) // best-effort sweep of residual terminated runs
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
	_, _ = CleanupTempRuns(root) // best-effort sweep of residual terminated runs
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
	changed, err := requirementArtifactsChanged(root, state.RequirementArtifacts)
	if err != nil {
		return PromptCatalog{}, err
	}
	if changed {
		if developmentStarted(state) {
			return PromptCatalog{}, fmt.Errorf("frozen requirement artifact changed; return to requirement clarification before continuing")
		}
		return PromptCatalog{}, fmt.Errorf("requirement artifacts changed; resume the run before continuing")
	}
	return catalog, nil
}

func requireNativeCurrent(root string, state RunState) (string, error) {
	current, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return "", err
	}
	if current != state.CurrentSnapshot {
		return "", fmt.Errorf("native VCS identity does not match the current snapshot")
	}
	return current, nil
}

func routeForState(root string, state RunState) PromptRoute {
	return PromptRoute{RequirementSource: state.RequirementSource, RequirementRevision: state.RequirementRevision, CatalogRevision: state.CatalogRevision, Worktree: absPath(cleanRoot(root)), VCS: state.VCS, BaseSnapshot: state.BaseSnapshot, CurrentSnapshot: state.CurrentSnapshot, PreRepairSnapshot: state.PreRepairSnapshot, RequirementArtifacts: append([]RequirementArtifact{}, state.RequirementArtifacts...)}
}

func requirePreparedDispatch(state RunState, dispatchID, targetKind, target string) (PreparedDispatch, error) {
	dispatchID = strings.TrimSpace(dispatchID)
	if dispatchID == "" {
		return PreparedDispatch{}, fmt.Errorf("dispatch id is required")
	}
	dispatch, ok := state.Dispatches[dispatchID]
	if !ok {
		return PreparedDispatch{}, fmt.Errorf("unknown dispatch %q", dispatchID)
	}
	if dispatch.TargetKind != targetKind || dispatch.Target != target {
		return PreparedDispatch{}, fmt.Errorf("dispatch %q does not belong to %s %q", dispatchID, targetKind, target)
	}
	wantedStatus := "OPEN"
	if dispatch.ReviewerRequired {
		wantedStatus = "CLAIMED"
		if strings.TrimSpace(dispatch.ReviewerIdentity) == "" {
			return PreparedDispatch{}, fmt.Errorf("dispatch %q has no claimed reviewer identity", dispatchID)
		}
	}
	if dispatch.Status != wantedStatus {
		return PreparedDispatch{}, fmt.Errorf("dispatch %q is %s and cannot record a result", dispatchID, dispatch.Status)
	}
	if dispatch.RequirementRevision != state.RequirementRevision || dispatch.CatalogRevision != state.CatalogRevision || dispatch.SourceSnapshot != state.CurrentSnapshot {
		return PreparedDispatch{}, fmt.Errorf("dispatch %q has stale source bindings", dispatchID)
	}
	return dispatch, nil
}

func completeDispatch(state *RunState, dispatchID string) {
	dispatch := state.Dispatches[dispatchID]
	dispatch.Status = "COMPLETED"
	state.Dispatches[dispatchID] = dispatch
}

func staleOpenDispatches(state *RunState, targetKind, target string) {
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind == targetKind && dispatch.Target == target && (dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED") {
			dispatch.Status = "STALE"
			state.Dispatches[id] = dispatch
		}
	}
}

func nextDispatchAttempt(state RunState, targetKind, target string, wave int) int {
	attempt := 1
	for _, dispatch := range state.Dispatches {
		if dispatch.TargetKind == targetKind && dispatch.Target == target && dispatch.ReviewWave == wave && dispatch.Attempt >= attempt {
			attempt = dispatch.Attempt + 1
		}
	}
	return attempt
}

func currentGateReviewWave(state RunState) int {
	if state.CompletedReviewWaves > 0 && state.Actions["development-worker"].Status == developmentVerified {
		return state.CompletedReviewWaves
	}
	return state.CompletedReviewWaves + 1
}

func newDispatchID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "dispatch-" + hex.EncodeToString(value[:]), nil
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

func rejectFrozenArtifactFindings(state RunState, findings []Finding) error {
	excluded := map[string]bool{}
	for _, artifact := range state.RequirementArtifacts {
		excluded[artifact.Path] = true
	}
	for _, finding := range findings {
		for _, location := range finding.Locations {
			path := location
			for count := 0; count < 2; count++ {
				index := strings.LastIndex(path, ":")
				if index <= 0 || !suffixIsDigits(path[index+1:]) {
					break
				}
				path = path[:index]
			}
			if excluded[filepath.ToSlash(filepath.Clean(path))] {
				return fmt.Errorf("finding location %s is a frozen acceptance artifact and not a review target", location)
			}
		}
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
	for id, dispatch := range state.Dispatches {
		if dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED" {
			dispatch.Status = "STALE"
			state.Dispatches[id] = dispatch
		}
	}
}

func rebindCurrentSnapshot(state *RunState, snapshot string) {
	previous := state.CurrentSnapshot
	state.CurrentSnapshot = snapshot
	if previous == snapshot {
		return
	}
	for id, dispatch := range state.Dispatches {
		if dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED" {
			dispatch.Status = "STALE"
			state.Dispatches[id] = dispatch
		}
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
		if isSealScopedAuthorization(authorization) && authorization.Snapshot == previous {
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
			if (developmentStatus != developmentVerified || hasSelectedRuntimeError(state)) && !runtimeErrorsAuthorizedForRepair(state) {
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
		adoptingMergedSlices := state.RetainedOverall && developmentStatus == developmentPending
		adoptingSliceRepair := state.RetainedOverall && (developmentStatus == developmentComplete || developmentStatus == developmentVerified)
		if !adoptingMergedSlices && !adoptingSliceRepair && developmentStatus != developmentPrepared && developmentStatus != developmentRepairPrepared {
			return fmt.Errorf("development worker must be prepared before a snapshot")
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
		if developmentStatus == developmentRepairPrepared || adoptingSliceRepair {
			if !reviewWaveRecorded(state) {
				return fmt.Errorf("all selected review results must be recorded before repair")
			}
			if !hasRepairableBlocker(state) && !hasP2Recommendation(state) {
				return fmt.Errorf("no recorded result requires repair")
			}
			if adoptingSliceRepair && (developmentStatus != developmentVerified || hasSelectedRuntimeError(state)) && !runtimeErrorsAuthorizedForRepair(state) {
				return fmt.Errorf("the current review wave is not complete")
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

func hasSelectedRuntimeError(state RunState) bool {
	for id := range selectedSet(state) {
		if selectedResultStatus(state, id) == "RUNTIME_ERROR" {
			return true
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

// authorizeSealSkips records seal-time skip authorizations for the named
// non-passing selected gates. FAIL and RUNTIME_ERROR results may be skipped;
// a FAIL skip is only allowed once the shared review-wave limit is
// exhausted, unless the user explicitly requested the skip (userRequested),
// which records a distinguishable SEAL-USER origin. A RUNTIME_ERROR is
// always manually skippable and keeps the SEAL origin. The authorizations
// are bound to the current snapshot and cleared by the next repair snapshot.
func authorizeSealSkips(state *RunState, skips []string, userRequested bool) error {
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
		if status == "FAIL" && !userRequested && state.CompletedReviewWaves < effectiveReviewWaveLimit(*state) {
			return fmt.Errorf("selected gate %q cannot be skipped before the review-wave limit is exhausted", id)
		}
		wanted[id] = true
	}
	for id := range wanted {
		origin := "SEAL"
		if userRequested && selectedResultStatus(*state, id) == "FAIL" {
			origin = "SEAL-USER"
		}
		state.SkipAuthorizations[id] = SkipAuthorization{Origin: origin, Status: selectedResultStatus(*state, id), Snapshot: state.CurrentSnapshot}
	}
	return nil
}

// isSealScopedAuthorization reports whether the authorization is a seal-time
// skip authorization bound to the current snapshot: SEAL for limit-exhausted
// and RUNTIME_ERROR skips, SEAL-USER for FAIL skips the user explicitly
// requested before the limit is exhausted.
func isSealScopedAuthorization(authorization SkipAuthorization) bool {
	return authorization.Origin == "SEAL" || authorization.Origin == "SEAL-USER"
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
		if !ok || !isSealScopedAuthorization(authorization) || authorization.Status != status || authorization.Snapshot != state.CurrentSnapshot {
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

func requirementArtifactSet(root, primary string, additional []string) ([]RequirementArtifact, error) {
	root = cleanWorktree(root)
	paths := append([]string{primary}, additional...)
	seen := map[string]bool{}
	artifacts := make([]RequirementArtifact, 0, len(paths))
	for _, raw := range paths {
		path, err := validatedArtifactPath(root, raw)
		if err != nil {
			return nil, err
		}
		if seen[path] {
			return nil, fmt.Errorf("duplicate requirement artifact %q", path)
		}
		seen[path] = true
		revision, err := RequirementRevision(resolveFromRoot(root, path))
		if err != nil {
			return nil, fmt.Errorf("requirement artifact %s: %w", path, err)
		}
		artifacts = append(artifacts, RequirementArtifact{Path: path, Revision: revision})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func validatedArtifactPath(root, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("requirement artifact path is required")
	}
	full := resolveFromRoot(root, strings.TrimSpace(raw))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("requirement artifact must be a file under the repository root: %s", raw)
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("requirement artifact %s: %w", filepath.ToSlash(rel), err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("requirement artifact %s is not a regular file", filepath.ToSlash(rel))
	}
	return filepath.ToSlash(filepath.Clean(rel)), nil
}

func normalizeArtifactPath(root, raw string) string {
	full := resolveFromRoot(cleanWorktree(root), strings.TrimSpace(raw))
	rel, err := filepath.Rel(cleanWorktree(root), full)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(raw))
	}
	return filepath.ToSlash(filepath.Clean(rel))
}

func artifactRevision(artifacts []RequirementArtifact, path string) string {
	for _, artifact := range artifacts {
		if artifact.Path == path {
			return artifact.Revision
		}
	}
	return ""
}

func requirementArtifactsChanged(root string, artifacts []RequirementArtifact) (bool, error) {
	if len(artifacts) == 0 {
		return false, fmt.Errorf("requirement artifact set is empty")
	}
	for _, artifact := range artifacts {
		revision, err := RequirementRevision(resolveFromRoot(cleanWorktree(root), artifact.Path))
		if err != nil {
			return false, fmt.Errorf("requirement artifact %s: %w", artifact.Path, err)
		}
		if revision != artifact.Revision {
			return true, nil
		}
	}
	return false, nil
}

func sameArtifactSet(left, right []RequirementArtifact) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func qaCaseSemanticKey(kind, description, procedure, oracle string) string {
	return strings.Join([]string{strings.ToUpper(strings.TrimSpace(kind)), strings.TrimSpace(description), strings.TrimSpace(procedure), strings.TrimSpace(oracle)}, "\x00")
}
