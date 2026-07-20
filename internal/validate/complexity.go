package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ComplexityOptions struct {
	Worktree          string
	VCS               string
	TaskType          string
	MaxNet            *int
	MaxNewProdFiles   *int
	MaxProdInsertions *int
	Staged            bool
}

type ComplexityStatisticsOptions struct {
	ComplexityOptions
	RunDir         string
	WorkflowID     string
	ChangeSnapshot string
	Output         string
}

type ComplexityReport struct {
	Status          string                   `json:"status"`
	VCS             string                   `json:"vcs"`
	Worktree        string                   `json:"worktree"`
	TaskType        string                   `json:"task_type"`
	Budget          *ComplexityBudget        `json:"budget,omitempty"`
	BudgetSource    string                   `json:"budget_source"`
	BudgetOverrides ComplexityBudgetOverride `json:"budget_overrides"`
	Summary         ComplexitySummary        `json:"summary"`
	Failures        []string                 `json:"failures"`
	ReviewRequired  []string                 `json:"review_required"`
	Warnings        []string                 `json:"warnings"`
	LargestFiles    []ComplexityFileChange   `json:"largest_files"`
}

type ComplexityBudget struct {
	MaxNet            int `json:"max_net"`
	MaxNewProdFiles   int `json:"max_new_prod_files"`
	MaxProdInsertions int `json:"max_prod_insertions"`
}

type ComplexityBudgetOverride struct {
	MaxNet            bool `json:"max_net"`
	MaxNewProdFiles   bool `json:"max_new_prod_files"`
	MaxProdInsertions bool `json:"max_prod_insertions"`
}

type ComplexitySummary struct {
	Insertions                    int `json:"insertions"`
	Deletions                     int `json:"deletions"`
	Net                           int `json:"net"`
	ProductionInsertions          int `json:"production_insertions"`
	NewProductionFiles            int `json:"new_production_files"`
	UntrackedProductionFiles      int `json:"untracked_production_files"`
	UntrackedProductionInsertions int `json:"untracked_production_insertions"`
	ChangedFiles                  int `json:"changed_files"`
	UntrackedFiles                int `json:"untracked_files"`
}

type ComplexityFileChange struct {
	Path           string `json:"path"`
	Insertions     int    `json:"insertions"`
	Deletions      int    `json:"deletions"`
	Status         string `json:"status"`
	Category       string `json:"category"`
	SuspiciousName bool   `json:"suspicious_name"`
}

var complexityTaskTypes = map[string]bool{
	"delete-or-consolidate": true,
	"bugfix":                true,
	"small-feature":         true,
	"refactor":              true,
	"new-system":            true,
}

var productionExts = map[string]bool{
	".c": true, ".cc": true, ".cpp": true, ".cxx": true,
	".h": true, ".hpp": true, ".cs": true, ".py": true,
	".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".go": true,
}

var docExts = map[string]bool{
	".md": true, ".txt": true, ".rst": true,
}

var testHints = []string{"test", "tests", "spec", "automation"}

var suspiciousTerms = []string{
	"Manager",
	"Service",
	"Report",
	"Evidence",
	"Policy",
	"Registry",
	"Cache",
	"Context",
	"Provider",
	"Orchestrator",
}

func Complexity(options ComplexityOptions) (ComplexityReport, Result) {
	worktree := cleanRoot(options.Worktree)
	worktreeAbs, err := filepath.Abs(worktree)
	if err != nil {
		worktreeAbs = filepath.Clean(worktree)
	}
	worktreeAbs = filepath.Clean(worktreeAbs)
	var result Result

	if !complexityTaskTypes[options.TaskType] {
		result.add("task-type", "unsupported task type: "+options.TaskType)
		return ComplexityReport{}, result
	}
	overrides := ComplexityBudgetOverride{
		MaxNet:            options.MaxNet != nil,
		MaxNewProdFiles:   options.MaxNewProdFiles != nil,
		MaxProdInsertions: options.MaxProdInsertions != nil,
	}
	var budget *ComplexityBudget
	if overrides.MaxNet || overrides.MaxNewProdFiles || overrides.MaxProdInsertions {
		if !overrides.MaxNet || !overrides.MaxNewProdFiles || !overrides.MaxProdInsertions {
			result.add("budget", "--max-net, --max-new-prod-files, and --max-prod-insertions must be passed together")
			return ComplexityReport{}, result
		}
		budget = &ComplexityBudget{
			MaxNet:            *options.MaxNet,
			MaxNewProdFiles:   *options.MaxNewProdFiles,
			MaxProdInsertions: *options.MaxProdInsertions,
		}
	}

	vcs, detectResult := detectComplexityVCS(worktreeAbs, options.VCS)
	if !detectResult.OK() {
		return ComplexityReport{}, detectResult
	}

	var changes []ComplexityFileChange
	var untracked []string
	var manualReviewReason string
	switch vcs {
	case "git":
		changes, result = parseGitComplexityChanges(worktreeAbs, options.Staged)
		if !result.OK() {
			return ComplexityReport{}, result
		}
		if !options.Staged {
			untracked, result = gitUntrackedComplexity(worktreeAbs)
			if !result.OK() {
				return ComplexityReport{}, result
			}
		}
	case "svn":
		changes, untracked, result = parseSVNComplexityChanges(worktreeAbs)
		if !result.OK() {
			return ComplexityReport{}, result
		}
		if options.Staged {
			untracked = nil
			manualReviewReason = "SVN has no staged index; --staged was ignored for SVN complexity review"
		}
	default:
		vcs = "none"
		manualReviewReason = "no git or svn working copy detected; provide manual diff evidence for complexity review"
	}

	report := buildComplexityReport(worktreeAbs, vcs, options.TaskType, budget, overrides, changes, untracked, manualReviewReason)
	return report, result
}

func ComplexityJSON(report ComplexityReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func ComplexityStatistics(options ComplexityStatisticsOptions) (EvidenceRef, ComplexityReport, Result) {
	var result Result
	root := cleanWorktree(options.Worktree)
	if strings.TrimSpace(options.RunDir) == "" || !meaningful(options.WorkflowID) || !meaningful(options.ChangeSnapshot) || strings.TrimSpace(options.Output) == "" {
		result.add("complexity", "--run-dir, --workflow-id, --change-snapshot, and --output are required for formal statistics")
		return EvidenceRef{}, ComplexityReport{}, result
	}
	runDir, err := resolveWorkflowRunDir(root, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return EvidenceRef{}, ComplexityReport{}, result
	}
	if options.MaxNet != nil || options.MaxNewProdFiles != nil || options.MaxProdInsertions != nil {
		result.add("complexity", "formal post-development statistics must omit all numeric budget flags")
		return EvidenceRef{}, ComplexityReport{}, result
	}
	report, complexityResult := Complexity(options.ComplexityOptions)
	if !complexityResult.OK() {
		return EvidenceRef{}, ComplexityReport{}, complexityResult
	}
	ref, writeResult := writeComplexityStatisticsReport(root, runDir, options.WorkflowID, options.ChangeSnapshot, options.Output, report)
	return ref, report, writeResult
}

func writeComplexityStatisticsReport(root, runDir, workflowID, snapshot, output string, report ComplexityReport) (EvidenceRef, Result) {
	var result Result
	if err := validatePostDevelopmentComplexityReport(root, report); err != nil {
		result.add("complexity", err.Error())
		return EvidenceRef{}, result
	}
	outputPath, err := prospectiveRestrictedPath(runDir, output)
	if err != nil {
		result.add(output, "output must be under the active run restricted directory")
		return EvidenceRef{}, result
	}
	data, err := ComplexityJSON(report)
	if err != nil {
		result.add(output, err.Error())
		return EvidenceRef{}, result
	}
	data = append(data, '\n')
	if err := writeBytesExclusive(outputPath, data); err != nil {
		result.add(output, err.Error())
		return EvidenceRef{}, result
	}
	logical, err := logicalPathInRun(runDir, outputPath)
	if err != nil {
		_ = os.Remove(outputPath)
		result.add(output, err.Error())
		return EvidenceRef{}, result
	}
	ref := EvidenceRef{Path: logical, SHA256: sha256File(outputPath)}
	if _, err := writeCompositionProof(root, runDir, "complexity-statistics.v1", workflowID, snapshot, outputPath, []EvidenceRef{ref}); err != nil {
		_ = os.Remove(outputPath)
		result.add(output, err.Error())
		return EvidenceRef{}, result
	}
	return ref, result
}

func decodeComplexityReport(data []byte) (ComplexityReport, error) {
	var report ComplexityReport
	if err := strictContractJSON(data, &report); err != nil {
		return ComplexityReport{}, err
	}
	if err := validateComplexityReport(report); err != nil {
		return ComplexityReport{}, err
	}
	return report, nil
}

func validateComplexityReport(report ComplexityReport) error {
	if !map[string]bool{"PASS": true, "REVIEW": true, "FAIL": true}[report.Status] {
		return fmt.Errorf("complexity report status is invalid")
	}
	if !map[string]bool{"git": true, "svn": true, "none": true}[report.VCS] {
		return fmt.Errorf("complexity report VCS is invalid")
	}
	if strings.TrimSpace(report.Worktree) == "" || !filepath.IsAbs(report.Worktree) {
		return fmt.Errorf("complexity report worktree must be an absolute path")
	}
	if !complexityTaskTypes[report.TaskType] {
		return fmt.Errorf("complexity report task type is invalid")
	}
	if report.BudgetSource != "none" && report.BudgetSource != "explicit" {
		return fmt.Errorf("complexity report budget source is invalid")
	}
	hasBudget := report.Budget != nil
	if (report.BudgetSource == "explicit") != hasBudget || report.BudgetOverrides.MaxNet != hasBudget || report.BudgetOverrides.MaxNewProdFiles != hasBudget || report.BudgetOverrides.MaxProdInsertions != hasBudget {
		return fmt.Errorf("complexity report budget fields are inconsistent")
	}
	summary := report.Summary
	if summary.Insertions < 0 || summary.Deletions < 0 || summary.ProductionInsertions < 0 || summary.NewProductionFiles < 0 || summary.UntrackedProductionFiles < 0 || summary.UntrackedProductionInsertions < 0 || summary.ChangedFiles < 0 || summary.UntrackedFiles < 0 || summary.Net != summary.Insertions-summary.Deletions || summary.UntrackedProductionFiles > summary.UntrackedFiles {
		return fmt.Errorf("complexity report summary is invalid")
	}
	for _, values := range [][]string{report.Failures, report.ReviewRequired, report.Warnings} {
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("complexity report messages must be non-empty")
			}
		}
	}
	if report.Status == "PASS" && (len(report.Failures) != 0 || len(report.ReviewRequired) != 0) || report.Status == "REVIEW" && (len(report.Failures) != 0 || len(report.ReviewRequired) == 0) || report.Status == "FAIL" && len(report.Failures) == 0 {
		return fmt.Errorf("complexity report status contradicts its findings")
	}
	if len(report.LargestFiles) > 10 || len(report.LargestFiles) > report.Summary.ChangedFiles {
		return fmt.Errorf("complexity report largest_files is inconsistent")
	}
	for _, change := range report.LargestFiles {
		path := strings.TrimSpace(change.Path)
		if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") || strings.HasPrefix(path, "../") || strings.Contains(path, "/../") || change.Insertions < 0 || change.Deletions < 0 || strings.TrimSpace(change.Status) == "" || !map[string]bool{"production": true, "test": true, "doc": true, "other": true}[change.Category] {
			return fmt.Errorf("complexity report largest_files entry is invalid")
		}
	}
	return nil
}

func validatePostDevelopmentComplexityReport(root string, report ComplexityReport) error {
	if err := validateComplexityReport(report); err != nil {
		return err
	}
	if report.Budget != nil || report.BudgetSource != "none" || report.BudgetOverrides.MaxNet || report.BudgetOverrides.MaxNewProdFiles || report.BudgetOverrides.MaxProdInsertions {
		return fmt.Errorf("post-development complexity evidence must be statistics-only")
	}
	if report.Status == "FAIL" {
		return fmt.Errorf("budget-free post-development complexity statistics cannot have FAIL status")
	}
	if !samePath(report.Worktree, cleanWorktree(root)) {
		return fmt.Errorf("complexity report worktree does not match the reviewed repository")
	}
	return nil
}

func validateComplexityStatisticsEvidence(root, runDir, workflowID, snapshot string, ref EvidenceRef) error {
	if !restrictedEvidencePath(root, runDir, ref.Path) {
		return fmt.Errorf("complexity statistics must be under the active run restricted directory")
	}
	path, err := safeEvidencePath(runDir, ref.Path)
	if err != nil || sha256File(path) != ref.SHA256 {
		return fmt.Errorf("complexity statistics path or hash is invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	report, err := decodeComplexityReport(data)
	if err != nil {
		return fmt.Errorf("complexity statistics report is incomplete or invalid: %w", err)
	}
	if err := validatePostDevelopmentComplexityReport(root, report); err != nil {
		return err
	}
	if err := validateStandaloneCompositionProof(root, runDir, "complexity-statistics.v1", workflowID, snapshot, ref); err != nil {
		return err
	}
	return nil
}

func ComplexityText(report ComplexityReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Complexity Gate: %s\n", report.Status)
	fmt.Fprintf(&b, "insertions=%d deletions=%d net=%d prod_insertions=%d changed_files=%d untracked=%d\n",
		report.Summary.Insertions,
		report.Summary.Deletions,
		report.Summary.Net,
		report.Summary.ProductionInsertions,
		report.Summary.ChangedFiles,
		report.Summary.UntrackedFiles,
	)
	fmt.Fprintf(&b, "budget_source=%s\n", report.BudgetSource)
	for _, failure := range report.Failures {
		fmt.Fprintf(&b, "FAIL: %s\n", failure)
	}
	for _, item := range report.ReviewRequired {
		fmt.Fprintf(&b, "REVIEW: %s\n", item)
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(&b, "WARN: %s\n", warning)
	}
	if len(report.LargestFiles) > 0 {
		fmt.Fprintln(&b, "Largest files:")
		for _, change := range report.LargestFiles {
			fmt.Fprintf(&b, "  %5d + %5d - %s [%s]\n", change.Insertions, change.Deletions, change.Path, change.Category)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func ComplexityExitCode(status string) int {
	switch status {
	case "PASS":
		return 0
	case "REVIEW":
		return 2
	default:
		return 1
	}
}

func detectComplexityVCS(worktree, requested string) (string, Result) {
	var result Result
	switch requested {
	case "", "auto":
		if isGitWorktree(worktree) {
			return "git", result
		}
		if svnInfo(worktree) {
			return "svn", result
		}
		return "none", result
	case "git", "svn", "none":
		return requested, result
	default:
		result.add("vcs", "unsupported --vcs value: "+requested)
		return "", result
	}
}

func parseGitComplexityChanges(worktree string, staged bool) ([]ComplexityFileChange, Result) {
	var result Result
	args := []string{"diff", "--numstat"}
	if staged {
		args = append(args, "--cached")
	}
	raw, err := gitText(worktree, args...)
	if err != nil {
		result.add("git", "git diff --numstat failed: "+err.Error())
		return nil, result
	}
	statusRaw, err := gitText(worktree, "status", "--short")
	if err != nil {
		result.add("git", "git status --short failed: "+err.Error())
		return nil, result
	}
	statuses := map[string]string{}
	for _, line := range strings.Split(statusRaw, "\n") {
		if strings.TrimSpace(line) == "" || len(line) < 4 {
			continue
		}
		status := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			parts := strings.SplitN(path, " -> ", 2)
			path = parts[1]
		}
		statuses[slash(path)] = status
	}
	var changes []ComplexityFileChange
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		insertions := parseComplexityCount(parts[0])
		deletions := parseComplexityCount(parts[1])
		path := slash(parts[2])
		if strings.Contains(path, " => ") {
			chunks := strings.SplitN(path, " => ", 2)
			path = chunks[1]
		}
		changes = append(changes, makeComplexityFileChange(path, insertions, deletions, statuses[path]))
	}
	return changes, result
}

func gitUntrackedComplexity(worktree string) ([]string, Result) {
	var result Result
	raw, err := gitText(worktree, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		result.add("git", "git ls-files untracked failed: "+err.Error())
		return nil, result
	}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, slash(strings.TrimSpace(line)))
		}
	}
	return out, result
}

func parseSVNComplexityChanges(worktree string) ([]ComplexityFileChange, []string, Result) {
	var result Result
	statusRaw, err := runTextCommand(worktree, "svn", "status")
	if err != nil {
		result.add("svn", "svn status failed: "+err.Error())
		return nil, nil, result
	}
	statuses := map[string]string{}
	var untracked []string
	for _, line := range strings.Split(statusRaw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		status := string(line[0])
		path := ""
		if len(line) > 8 {
			path = strings.TrimSpace(line[8:])
		} else if len(line) > 1 {
			path = strings.TrimSpace(line[1:])
		}
		if path == "" {
			continue
		}
		path = slash(path)
		if status == "?" {
			untracked = append(untracked, path)
			continue
		}
		if strings.Contains("AMDRC!~", status) {
			statuses[path] = status
		}
	}
	diffRaw, err := runTextCommand(worktree, "svn", "diff")
	if err != nil {
		result.add("svn", "svn diff failed: "+err.Error())
		return nil, nil, result
	}
	counts := map[string][2]int{}
	current := ""
	for _, line := range strings.Split(diffRaw, "\n") {
		if strings.HasPrefix(line, "Index: ") {
			current = slash(strings.TrimSpace(strings.TrimPrefix(line, "Index: ")))
			counts[current] = [2]int{}
			continue
		}
		if current == "" || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		count := counts[current]
		if strings.HasPrefix(line, "+") {
			count[0]++
		} else if strings.HasPrefix(line, "-") {
			count[1]++
		}
		counts[current] = count
	}
	var changes []ComplexityFileChange
	seen := map[string]bool{}
	for path, count := range counts {
		seen[path] = true
		changes = append(changes, makeComplexityFileChange(path, count[0], count[1], statuses[path]))
	}
	for path, status := range statuses {
		if !seen[path] {
			changes = append(changes, makeComplexityFileChange(path, 0, 0, status))
		}
	}
	return changes, untracked, result
}

func svnInfo(worktree string) bool {
	_, err := runTextCommand(worktree, "svn", "info")
	return err == nil
}

func runTextCommand(worktree, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = worktree
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func buildComplexityReport(worktree, vcs, taskType string, budget *ComplexityBudget, overrides ComplexityBudgetOverride, changes []ComplexityFileChange, untracked []string, manualReviewReason string) ComplexityReport {
	totalInsertions := 0
	totalDeletions := 0
	prodInsertions := 0
	var newProdFiles []string
	var suspicious []string
	for _, change := range changes {
		totalInsertions += change.Insertions
		totalDeletions += change.Deletions
		if change.Category == "production" {
			prodInsertions += change.Insertions
			if change.Status == "A" || change.Status == "??" {
				newProdFiles = append(newProdFiles, change.Path)
			}
		}
		if change.SuspiciousName && (change.Status == "A" || change.Status == "??") {
			suspicious = append(suspicious, change.Path)
		}
	}
	var untrackedProdFiles []string
	untrackedProdInsertions := 0
	for _, path := range untracked {
		if categorizeComplexityPath(path) != "production" {
			continue
		}
		untrackedProdFiles = append(untrackedProdFiles, path)
		untrackedProdInsertions += countFileLines(filepath.Join(worktree, filepath.FromSlash(path)))
		if hasSuspiciousComplexityName(path) {
			suspicious = append(suspicious, path)
		}
	}
	prodInsertions += untrackedProdInsertions
	newProdFiles = append(newProdFiles, untrackedProdFiles...)
	net := totalInsertions - totalDeletions
	failures := []string{}
	reviewRequired := []string{}
	var warnings []string
	if budget != nil {
		if len(changes) > 0 && net > budget.MaxNet {
			failures = append(failures, fmt.Sprintf("net diff %d exceeds budget %d for %s", net, budget.MaxNet, taskType))
		}
		if prodInsertions > budget.MaxProdInsertions {
			reviewRequired = append(reviewRequired, fmt.Sprintf("production insertions %d exceed budget %d", prodInsertions, budget.MaxProdInsertions))
		}
		if len(newProdFiles) > budget.MaxNewProdFiles {
			failures = append(failures, fmt.Sprintf("new production files %d exceed budget %d: %s", len(newProdFiles), budget.MaxNewProdFiles, strings.Join(firstN(newProdFiles, 8), ", ")))
		}
	}
	if len(suspicious) > 0 && taskType != "new-system" {
		reviewRequired = append(reviewRequired, "suspicious subsystem-like new names: "+strings.Join(firstN(suspicious, 12), ", "))
	}
	if len(untracked) > 0 {
		warnings = append(warnings, "untracked files present: "+strings.Join(firstN(untracked, 12), ", "))
	}
	if budget == nil {
		warnings = append(warnings, "no numeric budget supplied; report is diff statistics only")
	}
	if manualReviewReason != "" {
		reviewRequired = append(reviewRequired, manualReviewReason)
	}
	status := "PASS"
	if len(failures) > 0 {
		status = "FAIL"
	} else if len(reviewRequired) > 0 {
		status = "REVIEW"
	}
	largest := append([]ComplexityFileChange{}, changes...)
	sort.Slice(largest, func(i, j int) bool {
		left := largest[i].Insertions + largest[i].Deletions
		right := largest[j].Insertions + largest[j].Deletions
		if left == right {
			return largest[i].Path < largest[j].Path
		}
		return left > right
	})
	if len(largest) > 10 {
		largest = largest[:10]
	}
	source := "none"
	if budget != nil {
		source = "explicit"
	}
	return ComplexityReport{
		Status:          status,
		VCS:             vcs,
		Worktree:        slash(worktree),
		TaskType:        taskType,
		Budget:          budget,
		BudgetSource:    source,
		BudgetOverrides: overrides,
		Summary: ComplexitySummary{
			Insertions:                    totalInsertions,
			Deletions:                     totalDeletions,
			Net:                           net,
			ProductionInsertions:          prodInsertions,
			NewProductionFiles:            len(newProdFiles),
			UntrackedProductionFiles:      len(untrackedProdFiles),
			UntrackedProductionInsertions: untrackedProdInsertions,
			ChangedFiles:                  len(changes),
			UntrackedFiles:                len(untracked),
		},
		Failures:       failures,
		ReviewRequired: reviewRequired,
		Warnings:       warnings,
		LargestFiles:   largest,
	}
}

func makeComplexityFileChange(path string, insertions, deletions int, status string) ComplexityFileChange {
	path = slash(path)
	return ComplexityFileChange{
		Path:           path,
		Insertions:     insertions,
		Deletions:      deletions,
		Status:         status,
		Category:       categorizeComplexityPath(path),
		SuspiciousName: hasSuspiciousComplexityName(path),
	}
}

func categorizeComplexityPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	lower := strings.ToLower(path)
	if docExts[ext] {
		return "doc"
	}
	for _, hint := range testHints {
		if strings.Contains(lower, hint) {
			return "test"
		}
	}
	if productionExts[ext] {
		return "production"
	}
	return "other"
}

func hasSuspiciousComplexityName(path string) bool {
	base := filepath.Base(slash(path))
	for _, term := range suspiciousTerms {
		if strings.Contains(base, term) {
			return true
		}
	}
	return false
}

func parseComplexityCount(value string) int {
	if value == "-" {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return n
}

func countFileLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return 0
	}
	count := strings.Count(string(data), "\n")
	if data[len(data)-1] != '\n' {
		count++
	}
	return count
}

func firstN(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	return values[:n]
}
