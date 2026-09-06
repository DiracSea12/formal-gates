package validate

import (
	"fmt"
	"strings"
)

// QASharedStep is the single design/review record for common preparation or
// operations. Cases retain their own differences, evidence and oracle. A changed
// shared record invalidates every referencing case, never unrelated approvals.
type QASharedStep struct {
	ID           string `json:"id"`
	Procedure    string `json:"procedure"`
	ReviewStatus string `json:"reviewStatus"`
	ReviewReason string `json:"reviewReason,omitempty"`
}

// Shared definitions travel with the cases that reference them. In particular,
// a concrete blackbox dispatch can read the earlier empty-mode design. When no
// cases exist yet, keep the mode's own definitions so design can prepare them
// before adding cases. Review status and suggestions still belong to the dispatch
// mode; only common definitions and their decisions use this storage key.
func qaSharedStepStorageKey(state RunState, mode string) string {
	key, cases := state.qaModeCasesWithKey(mode)
	if len(cases) == 0 {
		return mode
	}
	return key
}

func sharedStep(state RunState, storageKey, id string) QASharedStep {
	for _, step := range state.QACaseSets[storageKey].SharedSteps {
		if step.ID == id {
			return step
		}
	}
	return QASharedStep{}
}

func formatQASharedSteps(steps []QASharedStep) string {
	var b strings.Builder
	for _, step := range steps {
		fmt.Fprintf(&b, "shared step: %s\nprocedure: %s\nreview status: %s\nreview reason: %s\n\n", step.ID, step.Procedure, step.ReviewStatus, step.ReviewReason)
	}
	return b.String()
}

// The retained master's execution and delivery views combine independent
// slices. Expand each case using its own slice's definitions so equal shared
// IDs in different slices cannot cross-reference one another. The slice
// sidecar keeps the original shared design/review once; only this copy expands.
func inlineQASharedSteps(c QACase, shared []QASharedStep) QACase {
	var procedures []string
	for _, id := range c.SharedSteps {
		for _, step := range shared {
			if step.ID == id {
				procedures = append(procedures, "Shared step "+id+":\n"+step.Procedure)
			}
		}
	}
	c.Procedure = strings.Join(append(procedures, c.Procedure), "\n\n")
	c.SharedSteps = nil
	return c
}

func mergeQASharedSteps(state *RunState, mode string, inputs []QASharedStep, cases []QACase) (bool, error) {
	ensureConvergenceState(state)
	set := state.QACaseSets[mode]
	set.SharedSteps = append([]QASharedStep{}, set.SharedSteps...)
	changed, seen := map[string]bool{}, map[string]bool{}
	for _, input := range inputs {
		id, procedure := strings.TrimSpace(input.ID), strings.TrimSpace(input.Procedure)
		if id == "" || procedure == "" || seen[id] {
			return false, fmt.Errorf("shared step requires a unique non-empty id and procedure")
		}
		seen[id] = true
		found := false
		for i, step := range set.SharedSteps {
			if step.ID != id {
				continue
			}
			found = true
			if step.Procedure != procedure {
				set.SharedSteps[i] = QASharedStep{ID: id, Procedure: procedure, ReviewStatus: "PENDING"}
				changed[id] = true
			}
		}
		if !found {
			set.SharedSteps = append(set.SharedSteps, QASharedStep{ID: id, Procedure: procedure, ReviewStatus: "PENDING"})
			changed[id] = true
		}
	}
	for i := range cases {
		for _, id := range cases[i].SharedSteps {
			if changed[id] {
				cases[i].ReviewStatus, cases[i].ApprovedSource = "PENDING", ""
				cases[i].SuggestionIDs = nil
			}
		}
	}
	state.QACaseSets[mode] = set
	return len(changed) != 0, nil
}

func validateSharedStepReferences(state RunState, mode string, c QACase) error {
	for _, id := range c.SharedSteps {
		if sharedStep(state, mode, id).ID == "" {
			return fmt.Errorf("QA case %s references unknown shared step %s", c.ID, id)
		}
	}
	return nil
}

func reviewQASharedSteps(state *RunState, mode string, inputs []QAReviewInput) error {
	storageKey := qaSharedStepStorageKey(*state, mode)
	set := state.QACaseSets[storageKey]
	pending := map[string]int{}
	for i, step := range set.SharedSteps {
		if step.ReviewStatus != "PASS" {
			pending[step.ID] = i
		}
	}
	if len(inputs) != len(pending) {
		return fmt.Errorf("QA Review must decide all %d pending shared steps", len(pending))
	}
	for _, input := range inputs {
		i, ok := pending[input.CaseID]
		if !ok {
			return fmt.Errorf("unknown, approved or duplicate shared step %s", input.CaseID)
		}
		if input.Outcome != "PASS" && input.Outcome != "FAIL" {
			return fmt.Errorf("shared step outcome must be PASS or FAIL")
		}
		if strings.TrimSpace(input.Reason) == "" {
			return fmt.Errorf("shared step %s requires a review reason", input.CaseID)
		}
		set.SharedSteps[i].ReviewStatus, set.SharedSteps[i].ReviewReason = input.Outcome, strings.TrimSpace(input.Reason)
		delete(pending, input.CaseID)
	}
	state.QACaseSets[storageKey] = set
	return nil
}

func qaSuggestionDisposed(s QASuggestion) bool {
	return s.Status == "ABSORBED" || ((s.Status == "ACCEPT" || s.Status == "DEFER" || s.Status == "DECLINE") && strings.TrimSpace(s.Reason) != "")
}

// Disposition does not approve a failed case. ACCEPT with affected case ids
// reopens their design/review; unchanged optional suggestions need only a reason.
func RecordQASuggestion(root, packageRoot, runID, mode, id, decision, reason string, affected []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentDefinitions(root, *state, packageRoot); err != nil {
			return err
		}
		if decision != "ACCEPT" && decision != "DEFER" && decision != "DECLINE" {
			return fmt.Errorf("QA suggestion decision must be ACCEPT, DEFER or DECLINE")
		}
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("QA suggestion disposition requires a reason")
		}
		if len(affected) != 0 && decision != "ACCEPT" {
			return fmt.Errorf("affected cases apply only to ACCEPT")
		}
		set, ok := state.QACaseSets[mode]
		if !ok || (set.Status != caseSetReviewed && set.Status != caseSetDraft) {
			return fmt.Errorf("QA suggestion disposition requires a PASS QA Review with pending suggestions")
		}
		found := false
		for i, suggestion := range set.Suggestions {
			if suggestion.ID == id {
				if qaSuggestionDisposed(suggestion) {
					return fmt.Errorf("suggestion %s is already disposed", id)
				}
				set.Suggestions[i].Status, set.Suggestions[i].Reason = decision, strings.TrimSpace(reason)
				found = true
			}
		}
		if !found {
			return fmt.Errorf("unknown QA suggestion id %s", id)
		}
		storageKey, cases := state.qaModeCasesWithKey(mode)
		wanted := map[string]bool{}
		for _, id := range affected {
			wanted[id] = true
		}
		for i := range cases {
			if wanted[cases[i].ID] {
				cases[i].ReviewStatus, cases[i].ApprovedSource = "PENDING", ""
				delete(wanted, cases[i].ID)
			}
		}
		if len(wanted) != 0 {
			return fmt.Errorf("suggestion references unknown affected case")
		}
		state.setQACasesForReview(mode, storageKey, cases)
		all := true
		for _, s := range set.Suggestions {
			all = all && qaSuggestionDisposed(s)
		}
		if len(affected) != 0 {
			set.Status, set.Version, set.BoundSnapshot = caseSetDraft, "", ""
			state.setQAReview(mode, ActionResult{Status: "PENDING"})
			state.setQADesign(mode, ActionResult{Status: "PENDING"})
			retainQAProgress(state, mode)
			invalidateActiveReviewWave(state, "accepted QA suggestion changes case meaning or executability")
		} else if all && state.qaReview(mode).Status == "PASS" {
			if err := freezeQACaseSet(state, mode, &set, false); err != nil {
				return err
			}
		}
		state.QACaseSets[mode] = set
		return nil
	})
}

// Checkpoints share QAExecutionResult with the final result. They do not create
// a task or finish its attempt. A runtime interruption can close that attempt;
// the existing bounded retry reuses only rows with an exact evidence binding.
type QAExecutionRecordOptions struct {
	Checkpoint  bool
	Tool        string
	Environment string
}

func qaExecutionBinding(state RunState, mode string, c QACase, tool, environment string) string {
	c.ReviewStatus, c.ApprovedSource, c.SuggestionIDs = "", "", nil
	storageKey := qaSharedStepStorageKey(state, mode)
	steps := []QASharedStep{}
	for _, id := range c.SharedSteps {
		step := sharedStep(state, storageKey, id)
		step.ReviewStatus, step.ReviewReason = "", ""
		steps = append(steps, step)
	}
	return digestValue(struct {
		Snapshot, Requirement, Tool, Environment string
		Case                                     QACase
		Steps                                    []QASharedStep
	}{state.CurrentSnapshot, state.RequirementRevision, tool, environment, c, steps})
}

func retainQAProgress(state *RunState, mode string) {
	result := state.qaExecution(mode)
	if result.Tool != "" && result.Status != "PASS" && result.Status != "FAIL" {
		result.Status = "PENDING"
		state.setQAExecution(mode, result)
		return
	}
	state.setQAExecution(mode, QAExecutionResult{Status: "PENDING"})
}

func mergeQAExecutionProgress(state RunState, mode string, required []QACase, inputs []QAResultInput, options QAExecutionRecordOptions) ([]QAResultInput, QAExecutionResult, error) {
	prior := state.qaExecution(mode)
	progress := QAExecutionResult{Tool: options.Tool, Environment: options.Environment, InvalidationReasons: append([]string{}, prior.InvalidationReasons...)}
	enabled := options.Checkpoint || options.Tool != "" || options.Environment != "" || prior.Tool != ""
	if !enabled {
		return inputs, progress, nil
	}
	if strings.TrimSpace(options.Tool) == "" || strings.TrimSpace(options.Environment) == "" {
		return nil, progress, fmt.Errorf("QA checkpoint/resume requires --tool and --environment identities")
	}
	expected := map[string]QACase{}
	for _, c := range required {
		expected[c.ID] = c
	}
	rows := map[string]QAResultInput{}
	for _, r := range prior.Cases {
		c, ok := expected[r.CaseID]
		if !ok || r.Binding == "" || r.Binding != qaExecutionBinding(state, mode, c, options.Tool, options.Environment) {
			progress.InvalidationReasons = appendUniqueStrings(progress.InvalidationReasons, r.CaseID+": candidate, requirement, case/shared steps, tool or environment binding changed")
			continue
		}
		item := QAResultInput{CaseID: r.CaseID, Outcome: r.Outcome, Procedure: r.Procedure, Observation: r.Observation, OracleResult: r.OracleResult}
		if (r.Outcome != "PASS" && r.Outcome != "FAIL") || r.Procedure == "" || r.Observation == "" || r.OracleResult == "" || validateQAExecutionEvidence(item) != nil {
			progress.InvalidationReasons = appendUniqueStrings(progress.InvalidationReasons, r.CaseID+": incomplete evidence")
			continue
		}
		rows[r.CaseID] = item
		progress.ReuseReasons = append(progress.ReuseReasons, r.CaseID+": exact candidate/case/tool/environment binding and complete evidence")
	}
	for _, item := range inputs {
		if _, ok := expected[item.CaseID]; !ok {
			return nil, progress, fmt.Errorf("unknown QA result %s", item.CaseID)
		}
		if _, ok := rows[item.CaseID]; ok {
			return nil, progress, fmt.Errorf("duplicate QA result for %s (already complete; resume only unfinished cases)", item.CaseID)
		}
		rows[item.CaseID] = item
	}
	merged := []QAResultInput{}
	for _, c := range required {
		if r, ok := rows[c.ID]; ok {
			merged = append(merged, r)
		} else {
			progress.PendingCases = append(progress.PendingCases, c.ID)
		}
	}
	return merged, progress, nil
}

// PrepareCandidate moves commit organization ahead of final QA/gates. It is a
// workflow write under the existing admission/lock owner. Seal never changes
// this identity. No-op ranges do not advance a snapshot merely for an artifact.
func PrepareCandidate(root, packageRoot, runID, message string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		if state.Actions["development-worker"].Status != developmentComplete && state.Actions["development-worker"].Status != developmentVerified && !state.RetainedOverall {
			return fmt.Errorf("prepare-candidate requires completed development")
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if err := verifySnapshotReady(root, state.VCS); err != nil {
			return err
		}
		if state.VCS != "git" {
			return nil
		}
		count, err := gitCommitCountInRange(root, state.BaseSnapshot, state.CurrentSnapshot)
		if err != nil {
			return err
		}
		if count <= 1 {
			return nil
		}
		if strings.TrimSpace(message) == "" {
			return fmt.Errorf("prepare-candidate requires --squash-message for multiple commits")
		}
		if err := squashGitRangeToBase(root, state.BaseSnapshot, strings.TrimSpace(message)); err != nil {
			return err
		}
		snapshot, err := resolveNativeSnapshot(root, state.VCS)
		if err != nil {
			return err
		}
		old := state.CurrentSnapshot
		state.CurrentSnapshot = snapshot
		resetSnapshotReviewSurface(state, old, false, false)
		for id, result := range state.Gates {
			result.Status = "PENDING"
			state.Gates[id] = result
		}
		state.PreRepairSnapshot = ""
		invalidateActiveReviewWave(state, "final candidate organized before mandatory verification")
		bindFrozenCaseSetsToSnapshot(state)
		staleAllDispatches(state)
		refreshRequirementGuarantee(root, state)
		return nil
	})
}

func qaSharedStepsByMode(state RunState) map[string][]QASharedStep {
	result := map[string][]QASharedStep{}
	for mode, set := range state.QACaseSets {
		if len(set.SharedSteps) != 0 {
			result[mode] = set.SharedSteps
		}
	}
	return result
}
