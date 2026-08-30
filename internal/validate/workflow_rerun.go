package validate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"formal-gates/internal/lifecycle"
)

// startQAOnlyRerun starts a new active run from a prior sealed formal result.
// It is deliberately narrow: this is a user-authorized handoff for a regular,
// no-split run, not a second route or an alternate way to seal without QA.
// Upstream stages are recorded as skipped provenance, never as fresh reviewer
// conclusions; QA design/review/execution and every selected gate are reopened.
func startQAOnlyRerun(options StartOptions) (RunState, error) {
	root := lifecycle.CleanRoot(options.Root)
	sourceID := strings.TrimSpace(options.FromSealedRun)
	if sourceID == "" {
		return RunState{}, fmt.Errorf("source sealed run id is required")
	}
	source, err := LoadRunSummary(root, sourceID)
	if err != nil {
		return RunState{}, fmt.Errorf("cannot load source sealed run %q: %w", sourceID, err)
	}
	if source.Status != "SEALED" {
		return RunState{}, fmt.Errorf("source run %q must be SEALED, got %s", sourceID, source.Status)
	}
	if source.Flow != formalFlow {
		return RunState{}, fmt.Errorf("source run %q is not a formal run", sourceID)
	}
	if source.RouteMode == "lightweight" {
		return RunState{}, fmt.Errorf("a lightweight run has no QA/gate candidate to rerun")
	}
	if source.Slicing == nil || source.Slicing.Decision != "no-split" {
		return RunState{}, fmt.Errorf("QA-only rerun currently requires a sealed no-split run")
	}
	if strings.TrimSpace(options.RequirementSource) == "" {
		return RunState{}, fmt.Errorf("--requirement is required for a QA-only rerun")
	}
	if strings.TrimSpace(options.VCS) == "" {
		options.VCS = source.VCS
	}
	if !strings.EqualFold(strings.TrimSpace(options.VCS), strings.TrimSpace(source.VCS)) {
		return RunState{}, fmt.Errorf("QA-only rerun VCS %q does not match source run VCS %q", options.VCS, source.VCS)
	}
	if strings.TrimSpace(options.BaseSnapshot) == "" {
		options.BaseSnapshot = source.BaseSnapshot
	}
	resolver, err := resolverForVCS(strings.ToLower(strings.TrimSpace(options.VCS)), nil)
	if err != nil {
		return RunState{}, err
	}
	liveSnapshot, err := resolver.Resolve(root)
	if err != nil {
		return RunState{}, err
	}
	if err := resolver.Verify(root, source.CurrentSnapshot); err != nil {
		return RunState{}, fmt.Errorf("source run current snapshot is unavailable: %w", err)
	}
	if err := resolver.IsAncestorOrEqual(root, source.CurrentSnapshot, liveSnapshot); err != nil {
		return RunState{}, fmt.Errorf("source run snapshot %s is not an ancestor of the current candidate %s: %w", source.CurrentSnapshot, liveSnapshot, err)
	}
	if strings.TrimSpace(options.RunID) == "" {
		return RunState{}, fmt.Errorf("run id is required for a QA-only rerun")
	}
	if strings.EqualFold(strings.TrimSpace(options.RunID), sourceID) {
		return RunState{}, fmt.Errorf("QA-only rerun must use a new run id, not %q", sourceID)
	}

	// Reuse the ordinary start admission, locking, and snapshot checks. The
	// transformed options only pin the source's no-split declaration; the state
	// is then reopened below through the CLI mutation path.
	options.FromSealedRun = ""
	options.FromSealedReason = ""
	options.RequirementConfirmed = false
	options.RetainedOverall = false
	options.MasterRunID = ""
	options.Split = "no"
	options.Route = ""
	state, err := startRegular(options)
	if err != nil {
		return RunState{}, err
	}
	if err := resolver.IsAncestorOrEqual(root, source.CurrentSnapshot, state.CurrentSnapshot); err != nil {
		_, _ = CleanupTempRun(root, state.RunID)
		return RunState{}, fmt.Errorf("source run snapshot %s is not an ancestor of the selected rerun candidate %s: %w", source.CurrentSnapshot, state.CurrentSnapshot, err)
	}
	cases, err := reusableQACases(root, source)
	if err != nil {
		_, _ = CleanupTempRun(root, state.RunID)
		return RunState{}, err
	}
	if err := verifySourceArtifactsPresent(state, source); err != nil {
		_, _ = CleanupTempRun(root, state.RunID)
		return RunState{}, err
	}
	updated, err := mutateRun(root, state.RunID, func(state *RunState) error {
		catalog, err := requireCurrentCatalog(*state, options.PackageRoot)
		if err != nil {
			return err
		}
		selected := append([]string{}, source.SelectedGates...)
		known := map[string]bool{}
		for _, id := range catalog.RouteCandidates() {
			known[id] = true
		}
		for _, id := range selected {
			if !known[id] {
				return fmt.Errorf("source run selected gate %q, which is absent from the current catalog", id)
			}
		}
		state.RequirementConfirmed = true
		state.Slicing = copySlicing(source.Slicing)
		state.RouteMode = source.RouteMode
		state.SelectedGates = selected
		state.SkipAuthorizations = map[string]SkipAuthorization{}
		chosen := map[string]bool{}
		for _, id := range selected {
			chosen[id] = true
		}
		for _, id := range catalog.GateIDs() {
			if !chosen[id] {
				state.SkipAuthorizations[id] = SkipAuthorization{Origin: "ROUTE", Status: "UNSELECTED"}
			}
		}
		state.QAOnlyRerun = &QAOnlyRerunRecord{
			SourceRunID:       source.RunID,
			SourceSnapshot:    source.CurrentSnapshot,
			SkippedStages:     []string{"product-review", "start-readiness", "development-worker"},
			AuthorizationNote: strings.TrimSpace(options.FromSealedReason),
		}
		if state.QAOnlyRerun.AuthorizationNote == "" {
			state.QAOnlyRerun.AuthorizationNote = "user authorized QA/gate-only rerun from the sealed source run"
		}
		for _, actionID := range []string{"requirements-clarification", "product-review", "start-readiness"} {
			state.Actions[actionID] = ActionResult{Status: "PASS", Message: "skipped by explicit QA-only rerun authorization from " + source.RunID}
		}
		state.Actions["development-worker"] = ActionResult{Status: developmentComplete, Message: "existing development snapshot inherited from " + source.RunID}
		state.QACasesByMode = map[string][]QACase{
			"blackbox": cases["blackbox"],
			"whitebox": cases["whitebox"],
		}
		state.QADesignByMode = map[string]ActionResult{
			"blackbox": {Status: "PASS", Message: "approved case inventory inherited from " + source.RunID},
			"whitebox": {Status: "PASS", Message: "approved case inventory inherited from " + source.RunID},
		}
		state.QAReviewByMode = map[string]ActionResult{
			"blackbox": {Status: "PASS", Message: "approved case inventory inherited from " + source.RunID},
			"whitebox": {Status: "PASS", Message: "approved case inventory inherited from " + source.RunID},
		}
		state.QADesignChangesByMode = map[string]QADesignChange{}
		state.QAExecutionByMode = reusableQAExecutions(source.QA)
		state.PriorQAExecutionByMode = map[string]*QAExecutionResult{}
		state.ExecutionScopes = map[string]QAExecutionScope{}
		state.Gates = map[string]GateResult{}
		for _, id := range catalog.GateIDs() {
			state.Gates[id] = GateResult{Status: "PENDING"}
		}
		state.Carry = map[string]CarryResult{}
		state.Dispatches = map[string]PreparedDispatch{}
		state.CompletedReviewWaves = 0
		state.ExtraReviewWaves = 0
		state.QAWorktree = ""
		state.SnapshotOverride = nil
		return nil
	})
	if err != nil {
		_, _ = CleanupTempRun(root, state.RunID)
		return RunState{}, err
	}
	return updated, nil
}

func verifySourceArtifactsPresent(state RunState, source RunSummary) error {
	current := map[string]bool{}
	for _, artifact := range state.RequirementArtifacts {
		current[artifact.Path] = true
	}
	for _, artifact := range source.RequirementArtifacts {
		if !current[artifact.Path] {
			return fmt.Errorf("source requirement artifact %q is not registered in the new run", artifact.Path)
		}
	}
	return nil
}

func copySlicing(slicing *Slicing) *Slicing {
	if slicing == nil {
		return nil
	}
	copy := *slicing
	copy.Slices = append([]string{}, slicing.Slices...)
	copy.InheritedReviews = append([]string{}, slicing.InheritedReviews...)
	return &copy
}

func reusableQAExecutions(result QAExecutionResult) map[string]QAExecutionResult {
	executions := map[string]QAExecutionResult{}
	byMode := map[string][]QAResultRecord{}
	for _, record := range result.Cases {
		byMode[record.Mode] = append(byMode[record.Mode], record)
	}
	for _, mode := range []string{"blackbox", "whitebox"} {
		if len(byMode[mode]) == 0 {
			continue
		}
		status := "PASS"
		var findings []Finding
		for _, record := range byMode[mode] {
			if record.Outcome == "FAIL" {
				status = "FAIL"
				findings = append(findings, Finding{Message: record.CaseID + ": " + record.Observation})
			}
		}
		executions[mode] = QAExecutionResult{Status: status, Snapshot: result.Snapshot, Cases: byMode[mode], Findings: findings}
	}
	return executions
}

func reusableQACases(root string, source RunSummary) (map[string][]QACase, error) {
	result := map[string][]QACase{"blackbox": {}, "whitebox": {}}
	blackboxPath := filepath.Join(lifecycle.CleanRoot(root), ".gates", "results", source.RunID+".blackbox-cases.md")
	blackbox, err := parseBlackboxCaseLedger(blackboxPath)
	if err != nil {
		return nil, fmt.Errorf("cannot reuse blackbox case ledger from %q: %w", source.RunID, err)
	}
	result["blackbox"] = blackbox
	sourceModes := map[string]string{}
	for _, record := range source.QA.Cases {
		sourceModes[record.CaseID] = record.Mode
	}
	seen := map[string]bool{}
	for _, testCase := range blackbox {
		if seen[testCase.ID] {
			return nil, fmt.Errorf("source blackbox case ledger contains duplicate %s", testCase.ID)
		}
		if sourceModes[testCase.ID] != "blackbox" {
			return nil, fmt.Errorf("source blackbox case %s is not present as a blackbox execution record", testCase.ID)
		}
		seen[testCase.ID] = true
	}
	for _, record := range source.QA.Cases {
		if record.Mode != "whitebox" {
			continue
		}
		if seen[record.CaseID] {
			return nil, fmt.Errorf("source QA inventory contains duplicate case %s", record.CaseID)
		}
		ref, err := findWhiteboxTestReference(root, record.Procedure)
		if err != nil {
			return nil, err
		}
		result["whitebox"] = append(result["whitebox"], QACase{
			ID:           record.CaseID,
			Mode:         "whitebox",
			Description:  "inherited approved whitebox case " + record.CaseID + " from " + source.RunID,
			Procedure:    record.Procedure,
			Oracle:       "the referenced whitebox test passes and enforces the documented invariant",
			Test:         ref,
			ReviewStatus: "PASS",
		})
		seen[record.CaseID] = true
	}
	for _, record := range source.QA.Cases {
		if !seen[record.CaseID] {
			return nil, fmt.Errorf("source QA result case %s is missing from the reusable case inventory", record.CaseID)
		}
	}
	return result, nil
}

func parseBlackboxCaseLedger(path string) ([]QACase, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var cases []QACase
	var current *QACase
	flush := func() {
		if current != nil {
			cases = append(cases, *current)
		}
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "CASE-") && !strings.Contains(line, " ") {
			flush()
			current = &QACase{ID: line, Mode: "blackbox", ReviewStatus: "PASS"}
			continue
		}
		if current == nil {
			continue
		}
		for _, field := range []struct {
			prefix string
			dest   *string
		}{
			{"mode:", &current.Mode},
			{"description:", &current.Description},
			{"procedure:", &current.Procedure},
			{"oracle:", &current.Oracle},
		} {
			if strings.HasPrefix(line, field.prefix) {
				*field.dest = strings.TrimSpace(strings.TrimPrefix(line, field.prefix))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	for _, testCase := range cases {
		if testCase.ID == "" || testCase.Mode != "blackbox" || testCase.Description == "" || testCase.Procedure == "" || testCase.Oracle == "" {
			return nil, fmt.Errorf("blackbox case ledger contains an incomplete case %q", testCase.ID)
		}
	}
	return cases, nil
}

var whiteboxFunctionPattern = regexp.MustCompile(`Test[A-Za-z0-9_]+`)

func findWhiteboxTestReference(root, procedure string) (string, error) {
	matches := whiteboxFunctionPattern.FindStringSubmatch(procedure)
	if len(matches) == 0 {
		return "", fmt.Errorf("cannot recover whitebox test reference from procedure %q", procedure)
	}
	function := matches[0]
	var reference string
	err := filepath.WalkDir(lifecycle.CleanRoot(root), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if reference != "" || entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(data), "func "+function+"(") {
			return nil
		}
		rel, err := filepath.Rel(lifecycle.CleanRoot(root), path)
		if err != nil {
			return err
		}
		reference = filepath.ToSlash(rel) + "::" + function
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("cannot locate whitebox test %s: %w", function, err)
	}
	if reference == "" {
		return "", fmt.Errorf("cannot locate whitebox test %s in the current tree", function)
	}
	return reference, nil
}
