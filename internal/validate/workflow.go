package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type WorkflowRecordStageOptions struct {
	Worktree       string
	StatePath      string
	Gate           string
	Verdict        string
	Mode           string
	Stage          string
	Artifact       string
	Actor          string
	WorkflowID     string
	ChangeSnapshot string
	Reason         string
	RunDir         string
}

type WorkflowVerifyAdmissionOptions struct {
	Worktree       string
	StatePath      string
	Gate           string
	Mode           string
	WorkflowID     string
	ChangeSnapshot string
	RunDir         string
}

type WorkflowRecordTransitionOptions struct {
	Worktree       string
	StatePath      string
	RunDir         string
	Artifact       string
	WorkflowID     string
	ChangeSnapshot string
}

type WorkflowFinalVerificationOptions struct {
	Worktree         string
	StatePath        string
	RunDir           string
	AttemptArtifacts []string
	OutputArtifact   string
	FinalQAArtifact  string
	RecordFinalQA    bool
	Actor            string
	WorkflowID       string
	ChangeSnapshot   string
}

type WorkflowFinalVerificationAttempt struct {
	Status       string `json:"status"`
	Accepted     bool   `json:"accepted"`
	Artifact     string `json:"artifact"`
	ArtifactHash string `json:"artifactHash"`
}

type WorkflowFinalVerificationArtifact struct {
	SchemaVersion    int                                `json:"schemaVersion"`
	WorkflowID       string                             `json:"workflowId"`
	ChangeSnapshot   string                             `json:"changeSnapshot"`
	Status           string                             `json:"status"`
	Attempts         []WorkflowFinalVerificationAttempt `json:"attempts"`
	AcceptedAttempts []WorkflowFinalVerificationAttempt `json:"acceptedAttempts"`
}

type WorkflowCleanupOptions struct {
	Worktree string
	FlowID   string
	Execute  bool
}

type WorkflowCleanupRecord struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type WorkflowCleanupReport struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Worktree      string                  `json:"worktree"`
	DryRun        bool                    `json:"dryRun"`
	Paths         []WorkflowCleanupRecord `json:"paths"`
}

func WorkflowRecordStage(options WorkflowRecordStageOptions) Result {
	worktree := cleanRoot(options.Worktree)
	var result Result
	runDir, err := resolveWorkflowRunDir(worktree, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return result
	}
	addWorkflowPathFailure(&result, worktree, runDir, "artifact", options.Artifact, false)
	addWorkflowPathFailure(&result, worktree, runDir, "state", options.StatePath, true)
	if !result.OK() {
		return result
	}
	record := GateRecordOptions{
		Worktree:       worktree,
		StatePath:      workflowStatePath(worktree, options.StatePath, runDir),
		RunDir:         runDir,
		Gate:           options.Gate,
		Verdict:        options.Verdict,
		Mode:           options.Mode,
		Stage:          options.Stage,
		Artifact:       options.Artifact,
		Actor:          options.Actor,
		WorkflowID:     options.WorkflowID,
		ChangeSnapshot: options.ChangeSnapshot,
		Reason:         options.Reason,
	}
	return GateRecord(record)
}

func WorkflowRecordTransition(options WorkflowRecordTransitionOptions) Result {
	worktree := cleanRoot(options.Worktree)
	var result Result
	runDir, err := resolveWorkflowRunDir(worktree, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return result
	}
	addWorkflowPathFailure(&result, worktree, runDir, "artifact", options.Artifact, false)
	addWorkflowPathFailure(&result, worktree, runDir, "state", options.StatePath, true)
	if !result.OK() {
		return result
	}
	options.Worktree, options.StatePath, options.RunDir = worktree, workflowStatePath(worktree, options.StatePath, runDir), runDir
	return GateRecordTransition(options)
}

func WorkflowVerifyAdmission(options WorkflowVerifyAdmissionOptions) Result {
	worktree := cleanRoot(options.Worktree)
	var result Result
	runDir, err := resolveWorkflowRunDir(worktree, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return result
	}
	addWorkflowPathFailure(&result, worktree, runDir, "state", options.StatePath, true)
	if !result.OK() {
		return result
	}
	return GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       worktree,
		StatePath:      workflowStatePath(worktree, options.StatePath, runDir),
		RunDir:         runDir,
		Gate:           options.Gate,
		Mode:           options.Mode,
		WorkflowID:     options.WorkflowID,
		ChangeSnapshot: options.ChangeSnapshot,
	})
}

func WorkflowFinalVerification(options WorkflowFinalVerificationOptions) (WorkflowFinalVerificationArtifact, Result) {
	worktree := cleanRoot(options.Worktree)
	var result Result
	if !isDir(worktree) {
		result.add("worktree", "worktree does not exist: "+worktree)
		return WorkflowFinalVerificationArtifact{}, result
	}
	runDir := ""
	if strings.TrimSpace(options.RunDir) != "" || strings.TrimSpace(options.WorkflowID) != "" {
		var err error
		runDir, err = resolveWorkflowRunDir(worktree, options.WorkflowID, options.RunDir)
		if err != nil {
			result.add("run-dir", err.Error())
			return WorkflowFinalVerificationArtifact{}, result
		}
		for _, artifact := range options.AttemptArtifacts {
			addWorkflowPathFailure(&result, worktree, runDir, "attempt-artifact", artifact, false)
		}
		addWorkflowPathFailure(&result, worktree, runDir, "output", options.OutputArtifact, true)
		addWorkflowPathFailure(&result, worktree, runDir, "final-qa-artifact", options.FinalQAArtifact, false)
		addWorkflowPathFailure(&result, worktree, runDir, "state", options.StatePath, true)
		if !result.OK() {
			return WorkflowFinalVerificationArtifact{}, result
		}
	}
	if len(options.AttemptArtifacts) == 0 {
		result.add("attempts", "at least one --attempt-artifact is required")
		return WorkflowFinalVerificationArtifact{}, result
	}
	attempts := make([]WorkflowFinalVerificationAttempt, 0, len(options.AttemptArtifacts))
	accepted := make([]WorkflowFinalVerificationAttempt, 0, len(options.AttemptArtifacts))
	seenAttempts := map[string]bool{}
	for i, artifact := range options.AttemptArtifacts {
		artifact = strings.TrimSpace(artifact)
		where := fmt.Sprintf("attempt-artifact[%d]", i)
		if artifact == "" || seenAttempts[artifact] {
			result.add(where, "verification attempt artifacts must be non-empty and unique")
			continue
		}
		seenAttempts[artifact] = true
		if runDir != "" {
			if err := requireWorkflowPathUnderRunDir(worktree, runDir, where, artifact, false); err != nil {
				result.add(where, err.Error())
				continue
			}
		}
		artifactPath := resolvePath(worktree, artifact)
		if cleanupScratchPath(worktree, artifactPath) {
			result.add(where, "verification attempt artifact cannot be under cleanup scratch: "+slash(artifactPath))
			continue
		}
		if !isFile(artifactPath) {
			result.add(where, "verification attempt artifact does not exist: "+slash(artifactPath))
			continue
		}
		data, err := os.ReadFile(artifactPath)
		if err != nil {
			result.add(where, "verification attempt artifact cannot be read: "+err.Error())
			continue
		}
		attempt := WorkflowFinalVerificationAttempt{Status: "PASS", Accepted: true, Artifact: artifact, ArtifactHash: sha256File(artifactPath)}
		if failure := verificationAttemptFailure(data); failure != "" {
			attempt.Status = "FAIL"
			attempt.Accepted = false
			result.add(where, failure)
			attempts = append(attempts, attempt)
			continue
		}
		attempts = append(attempts, attempt)
		accepted = append(accepted, attempt)
	}
	if len(accepted) == 0 {
		result.add("acceptedAttempts", "at least one accepted PASS attempt is required")
	}

	status := "PASS"
	if !result.OK() {
		status = "FAIL"
	}
	artifact := WorkflowFinalVerificationArtifact{
		SchemaVersion:    2,
		WorkflowID:       options.WorkflowID,
		ChangeSnapshot:   options.ChangeSnapshot,
		Status:           status,
		Attempts:         attempts,
		AcceptedAttempts: accepted,
	}
	output := strings.TrimSpace(options.OutputArtifact)
	if output == "" {
		if runDir != "" {
			output = relativePath(worktree, filepath.Join(runDir, "restricted", "final-verification.json"))
		} else {
			suffix := strings.TrimSpace(options.WorkflowID)
			if suffix == "" {
				suffix = "workflow"
			}
			output = filepath.ToSlash(filepath.Join(".gates", "artifacts", "final-verification-"+suffix+".json"))
		}
	}
	outputPath := resolvePath(worktree, output)
	if cleanupScratchPath(worktree, outputPath) {
		result.add("output", "final verification artifact cannot be under cleanup scratch: "+slash(outputPath))
		return artifact, result
	}
	if _, err := os.Lstat(outputPath); err == nil || !os.IsNotExist(err) {
		result.add("output", "generated final verification output already exists")
		return artifact, result
	}
	if err := writeFinalVerificationArtifact(outputPath, artifact); err != nil {
		result.add("output", err.Error())
		return artifact, result
	}
	if artifact.Status == "PASS" && runDir != "" {
		logical, logicalErr := logicalPathInRun(runDir, outputPath)
		if logicalErr != nil {
			_ = os.Remove(outputPath)
			result.add("output", logicalErr.Error())
			return artifact, result
		}
		ref := EvidenceRef{Path: logical, SHA256: sha256File(outputPath)}
		if _, proofErr := writeCompositionProof(worktree, runDir, "verification.v1", options.WorkflowID, options.ChangeSnapshot, outputPath, []EvidenceRef{ref}); proofErr != nil {
			_ = os.Remove(outputPath)
			result.add("output", proofErr.Error())
			return artifact, result
		}
	}
	if options.RecordFinalQA {
		recordResult := recordFinalQA(worktree, runDir, output, artifact.Status, options)
		result.Failures = append(result.Failures, recordResult.Failures...)
	}
	return artifact, result
}

func recordFinalQA(worktree, runDir, finalVerification, status string, options WorkflowFinalVerificationOptions) Result {
	return recordFinalQAWith(worktree, runDir, finalVerification, status, options, gateRecord)
}

func recordFinalQAWith(worktree, runDir, finalVerification, status string, options WorkflowFinalVerificationOptions, record func(GateRecordOptions) Result) Result {
	var result Result
	finalQA := strings.TrimSpace(options.FinalQAArtifact)
	if finalQA == "" {
		result.add("final-qa-artifact", "--final-qa-artifact is required when --record-final-qa is used")
		return result
	}
	finalQAPath := resolvePath(worktree, finalQA)
	if cleanupScratchPath(worktree, finalQAPath) {
		result.add("final-qa-artifact", "final QA artifact cannot be under cleanup scratch: "+slash(finalQAPath))
		return result
	}
	if _, err := os.Lstat(finalQAPath); err == nil {
		result.add("final-qa-artifact", "final QA artifact already exists; use a distinct output path")
		return result
	} else if !os.IsNotExist(err) {
		result.add("final-qa-artifact", err.Error())
		return result
	}
	statePath := workflowStatePath(worktree, options.StatePath, runDir)
	state, err := loadGateState(statePath)
	if err != nil {
		result.add("gate-state", err.Error())
		return result
	}
	policy, _ := fixedPolicy("FINAL_EXECUTION", "qa-test-gate", "FinalExecution")
	matrix := make([]FinalGateRow, 0, len(policy.Prerequisites))
	for _, prerequisite := range policy.Prerequisites {
		entries := entriesForGateNewestFirst(state, prerequisite.Gate)
		var selected *GateStateEntry
		for i := range entries {
			entry := entries[i]
			if entry.Verdict == "PASS" && entry.WorkflowID == options.WorkflowID && entry.ChangeSnapshot == options.ChangeSnapshot && entry.Mode == prerequisite.Flow && normalizeStage(entry.Stage) == normalizeStage(prerequisite.Stage) {
				selected = &entry
				break
			}
		}
		if selected == nil {
			decision, carryRef, err := acceptedCarryForGate(worktree, runDir, state, options.WorkflowID, options.ChangeSnapshot, prerequisite.Gate)
			if err != nil {
				result.add("gate-state", "missing current-snapshot PASS closure or accepted Carry for "+prerequisite.Gate+": "+err.Error())
				continue
			}
			matrix = append(matrix, FinalGateRow{Gate: prerequisite.Gate, ResultKind: "CARRIED_PASS", SourceSnapshot: decision.SourceSnapshot, TargetSnapshot: options.ChangeSnapshot, GateEvidence: decision.SourceGateEvidence, CarryDecision: &carryRef})
			continue
		}
		logical, err := logicalPathInRun(runDir, resolvePath(worktree, selected.Artifact))
		if err != nil {
			result.add("gate-state", err.Error())
			continue
		}
		matrix = append(matrix, FinalGateRow{Gate: prerequisite.Gate, ResultKind: "FRESH_PASS", SourceSnapshot: options.ChangeSnapshot, TargetSnapshot: options.ChangeSnapshot, GateEvidence: EvidenceRef{Path: logical, SHA256: selected.ArtifactHash}})
	}
	finalLogical, err := logicalPathInRun(runDir, resolvePath(worktree, finalVerification))
	if err != nil {
		result.add("final-verification", err.Error())
	}
	if !result.OK() {
		return result
	}
	payload, _ := json.Marshal(FinalExecutionPayload{Mode: "MECHANICAL_CLOSEOUT", GateMatrix: matrix, FinalVerification: EvidenceRef{Path: finalLogical, SHA256: sha256File(resolvePath(worktree, finalVerification))}, ReleaseJudgment: "SEAL"})
	envelope := FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot, Gate: policy.Gate, Stage: policy.Stage, Verdict: status, Payload: payload}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		result.add("final-qa-artifact", err.Error())
		return result
	}
	decodeArtifact(ArtifactOptions{Root: worktree, File: finalQA, Gate: policy.Gate, Stage: policy.Stage, WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot, Flow: policy.Flow, RunDir: runDir}, data, &result)
	if !result.OK() {
		return result
	}
	if err := writeBytesExclusive(finalQAPath, append(data, '\n')); err != nil {
		result.add("final-qa-artifact", err.Error())
		return result
	}
	actor := strings.TrimSpace(options.Actor)
	if actor == "" {
		actor = "gate-workflow"
	}
	recordResult := record(GateRecordOptions{
		Worktree:       worktree,
		StatePath:      statePath,
		RunDir:         runDir,
		Gate:           policy.Gate,
		Verdict:        status,
		Mode:           "formal",
		Stage:          policy.Stage,
		Artifact:       finalQA,
		Actor:          actor,
		WorkflowID:     options.WorkflowID,
		ChangeSnapshot: options.ChangeSnapshot,
	})
	if !recordResult.OK() {
		if err := os.Remove(finalQAPath); err != nil && !os.IsNotExist(err) {
			recordResult.add("final-qa-artifact", "cannot remove failed FinalExecution: "+err.Error())
		}
	}
	return recordResult
}

func WorkflowCleanup(options WorkflowCleanupOptions) (WorkflowCleanupReport, Result) {
	worktree := cleanRoot(options.Worktree)
	var result Result
	if !isDir(worktree) {
		result.add("worktree", "worktree does not exist: "+worktree)
		return WorkflowCleanupReport{}, result
	}
	path, err := cleanupPath(worktree, options.FlowID)
	if err != nil {
		result.add("cleanup", err.Error())
		return WorkflowCleanupReport{}, result
	}
	report := WorkflowCleanupReport{
		SchemaVersion: 1,
		Worktree:      slash(absPath(worktree)),
		DryRun:        !options.Execute,
		Paths:         make([]WorkflowCleanupRecord, 0, 1),
	}
	record := WorkflowCleanupRecord{Path: slash(path)}
	if !exists(path) {
		record.Status = "missing"
		report.Paths = append(report.Paths, record)
		return report, result
	}
	if !options.Execute {
		record.Status = "would-remove"
		report.Paths = append(report.Paths, record)
		return report, result
	}
	if err := os.RemoveAll(path); err != nil {
		result.add(slash(path), "cleanup remove failed: "+err.Error())
		record.Status = "remove-failed"
	} else {
		record.Status = "removed"
	}
	report.Paths = append(report.Paths, record)
	return report, result
}

func workflowStatePath(worktree, statePath, runDir string) string {
	if strings.TrimSpace(statePath) != "" {
		return resolveStatePath(worktree, statePath)
	}
	if strings.TrimSpace(runDir) != "" {
		return filepath.Join(runDir, "restricted", "gate-state.json")
	}
	return resolveStatePath(worktree, "")
}

func resolveWorkflowRunDir(worktree, workflowID, value string) (string, error) {
	worktreeAbs := absPath(worktree)
	runDir := strings.TrimSpace(value)
	if runDir == "" {
		if strings.TrimSpace(workflowID) == "" {
			return "", fmt.Errorf("--workflow-id is required when using a default workflow run directory")
		}
		runDir = filepath.ToSlash(filepath.Join(".gates", "runs", workflowID))
	}
	full := absPath(resolvePath(worktreeAbs, runDir))
	runsRoot := filepath.Join(worktreeAbs, ".gates", "runs")
	if samePath(full, runsRoot) || !pathUnder(full, runsRoot) {
		return "", fmt.Errorf("run directory must be under .gates/runs: %s", slash(full))
	}
	return full, nil
}

func requireWorkflowPathUnderRunDir(worktree, runDir, label, value string, allowEmpty bool) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return requireAbsPathUnderRunDir(runDir, label, resolvePath(worktree, value))
}

func addWorkflowPathFailure(result *Result, worktree, runDir, label, value string, allowEmpty bool) {
	if err := requireWorkflowPathUnderRunDir(worktree, runDir, label, value, allowEmpty); err != nil {
		result.add(label, err.Error())
	}
}

func requireAbsPathUnderRunDir(runDir, label, path string) error {
	full := absPath(path)
	restricted := filepath.Join(absPath(runDir), "restricted")
	if samePath(full, restricted) || !pathUnder(full, restricted) {
		return fmt.Errorf("%s must be under the active run restricted directory: %s", label, slash(full))
	}
	return nil
}

func writeFinalVerificationArtifact(path string, artifact WorkflowFinalVerificationArtifact) error {
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o600)
}

// verificationAttemptFailure recognizes output lines that unambiguously report
// a failed command. Empty output remains valid for commands such as go build
// and go vet, whose successful runs normally produce no stdout or stderr.
func verificationAttemptFailure(data []byte) string {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		upper := strings.ToUpper(line)
		switch {
		case upper == "FAIL", strings.HasPrefix(upper, "FAIL "), strings.HasPrefix(upper, "FAIL:"), strings.HasPrefix(upper, "FAIL\t"), strings.HasPrefix(upper, "--- FAIL:"):
			return "verification attempt artifact contains a FAIL result"
		case strings.HasPrefix(upper, "FAILED"), strings.HasPrefix(upper, "FAILURE"):
			return "verification attempt artifact contains a failed-command result"
		case strings.HasPrefix(upper, "ERROR:"), strings.HasPrefix(upper, "ERROR "), strings.HasPrefix(upper, "ERROR\t"), strings.HasPrefix(upper, "FATAL:"), strings.HasPrefix(upper, "FATAL "), strings.HasPrefix(upper, "FATAL\t"), strings.HasPrefix(upper, "PANIC:"), strings.HasPrefix(upper, "PANIC "), strings.HasPrefix(upper, "PANIC\t"):
			return "verification attempt artifact contains a fatal or error result"
		case strings.Contains(upper, "COMMAND FAILED"), hasNonZeroExitMarker(upper):
			return "verification attempt artifact reports a non-zero command exit"
		}
		if strings.HasPrefix(line, "# ") && index+1 < len(lines) && looksLikeGoDiagnostic(lines[index+1]) {
			return "verification attempt artifact contains a Go compiler or vet diagnostic"
		}
	}
	return ""
}

func hasNonZeroExitMarker(text string) bool {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ':' || r == '=' || r == '(' || r == ')'
	})
	for index, field := range fields {
		if field != "STATUS" && field != "CODE" || index == 0 || index+1 >= len(fields) {
			continue
		}
		exitWord := false
		for previous := index - 1; previous >= 0 && previous >= index-3; previous-- {
			if fields[previous] == "EXIT" || fields[previous] == "EXITED" {
				exitWord = true
				break
			}
		}
		if !exitWord {
			continue
		}
		value := fields[index+1]
		value = strings.TrimLeft(value, "0")
		if value != "" {
			allDigits := true
			for _, digit := range value {
				if digit < '0' || digit > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return true
			}
		}
	}
	return false
}

func looksLikeGoDiagnostic(raw string) bool {
	line := strings.TrimSpace(raw)
	marker := strings.Index(line, ".go:")
	if marker < 0 {
		return false
	}
	rest := line[marker+len(".go:"):]
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	return digits > 0 && digits < len(rest) && rest[digits] == ':'
}

func cleanupPath(worktree, flowID string) (string, error) {
	worktreeAbs := absPath(worktree)
	root := filepath.Join(worktreeAbs, ".gates", "tmp")
	flowID = strings.TrimSpace(flowID)
	if flowID == "" {
		return root, nil
	}
	if filepath.Base(flowID) != flowID || flowID == "." || flowID == ".." {
		return "", fmt.Errorf("cleanup flow id must be one directory name: %s", flowID)
	}
	return filepath.Join(root, flowID), nil
}

func cleanupScratchPath(worktree, path string) bool {
	full := absPath(resolvePath(worktree, path))
	worktreeAbs := absPath(worktree)
	if !pathUnder(full, worktreeAbs) {
		return false
	}
	root := filepath.Join(worktreeAbs, ".gates", "tmp")
	return samePath(full, root) || pathUnder(full, root)
}

func samePath(a, b string) bool {
	a = absPath(a)
	b = absPath(b)
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func pathUnder(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}
