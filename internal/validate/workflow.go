package validate

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type WorkflowSnapshotOptions struct {
	Worktree           string
	VCS                string
	BaseRef            string
	HeadRef            string
	IncludeWorkingTree bool
}

type WorkflowSnapshotRecord struct {
	VCS                string `json:"vcs"`
	BaseRef            string `json:"baseRef,omitempty"`
	BaseCommit         string `json:"baseCommit,omitempty"`
	HeadRef            string `json:"headRef,omitempty"`
	HeadCommit         string `json:"headCommit,omitempty"`
	RangeHash          string `json:"rangeHash"`
	IncludeWorkingTree bool   `json:"includeWorkingTree"`
	WorkingTreeHash    string `json:"workingTreeHash,omitempty"`
	ChangeSnapshot     string `json:"changeSnapshot"`
}

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

type WorkflowFinalVerificationOptions struct {
	Worktree        string
	StatePath       string
	RunDir          string
	AttemptsJSON    string
	AttemptsFile    string
	OutputArtifact  string
	FinalQAArtifact string
	RecordFinalQA   bool
	Actor           string
	WorkflowID      string
	ChangeSnapshot  string
}

type WorkflowFinalVerificationAttempt map[string]any

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
	Paths    []string
	Execute  bool
}

type WorkflowCompactOptions struct {
	Worktree       string
	RunDir         string
	WorkflowID     string
	ChangeSnapshot string
	Execute        bool
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

type WorkflowCompactArchive struct {
	SchemaVersion   int                     `json:"schemaVersion"`
	WorkflowID      string                  `json:"workflowId"`
	ChangeSnapshot  string                  `json:"changeSnapshot,omitempty"`
	RunDir          string                  `json:"runDir"`
	Files           []WorkflowCompactFile   `json:"files"`
	Cleanup         []WorkflowCleanupRecord `json:"cleanup"`
	OtherRunCleanup []WorkflowCleanupRecord `json:"otherRunCleanup,omitempty"`
	DryRun          bool                    `json:"dryRun"`
}

type WorkflowCompactFile struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"contentBase64,omitempty"`
}

func WorkflowSnapshot(options WorkflowSnapshotOptions) (WorkflowSnapshotRecord, Result) {
	worktree := cleanRoot(options.Worktree)
	var result Result
	if !isDir(worktree) {
		result.add("worktree", "worktree does not exist: "+worktree)
		return WorkflowSnapshotRecord{}, result
	}
	vcs := strings.TrimSpace(options.VCS)
	if vcs == "" {
		vcs = "auto"
	}
	switch vcs {
	case "auto":
		if isGitWorktree(worktree) {
			vcs = "git"
		} else if hasAncestorDir(worktree, ".svn") {
			vcs = "svn"
		} else {
			vcs = "file-hash"
		}
	case "file-hash", "git", "svn":
	default:
		result.add("vcs", "unsupported --vcs value: "+vcs)
		return WorkflowSnapshotRecord{}, result
	}

	if vcs != "git" {
		snapshot, err := fileHashSnapshot(worktree, vcs)
		if err != nil {
			result.add("workflow snapshot", err.Error())
		}
		return snapshot, result
	}

	snapshot, err := gitSnapshot(worktree, options)
	if err != nil {
		result.add("workflow snapshot", err.Error())
	}
	return snapshot, result
}

func WorkflowSnapshotJSON(snapshot WorkflowSnapshotRecord) ([]byte, error) {
	return json.MarshalIndent(snapshot, "", "  ")
}

func WorkflowRecordStage(options WorkflowRecordStageOptions) Result {
	worktree := cleanRoot(options.Worktree)
	var result Result
	runDir := ""
	if strings.TrimSpace(options.RunDir) != "" {
		var err error
		runDir, err = resolveWorkflowRunDir(worktree, options.WorkflowID, options.RunDir)
		if err != nil {
			result.add("run-dir", err.Error())
			return result
		}
		if err := requireWorkflowPathUnderRunDir(worktree, runDir, "artifact", options.Artifact, false); err != nil {
			result.add("artifact", err.Error())
			return result
		}
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
	runDir := ""
	if strings.TrimSpace(options.RunDir) != "" {
		var err error
		runDir, err = resolveWorkflowRunDir(worktree, options.WorkflowID, options.RunDir)
		if err != nil {
			result.add("run-dir", err.Error())
			return result
		}
		if err := requireWorkflowPathUnderRunDir(worktree, runDir, "decision-artifact", options.DecisionArtifact, false); err != nil {
			result.add("decision-artifact", err.Error())
			return result
		}
	}
	return GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         worktree,
		StatePath:        workflowStatePath(worktree, options.StatePath, runDir),
		RunDir:           runDir,
		WorkflowID:       options.WorkflowID,
		FromSnapshot:     options.FromSnapshot,
		ToSnapshot:       options.ToSnapshot,
		RerunFromGate:    options.RerunFromGate,
		FlowMode:         options.FlowMode,
		WorkflowMode:     options.WorkflowMode,
		DecisionArtifact: options.DecisionArtifact,
		Reason:           options.Reason,
	})
}

func WorkflowVerifyAdmission(options WorkflowVerifyAdmissionOptions) Result {
	worktree := cleanRoot(options.Worktree)
	var result Result
	runDir := ""
	if strings.TrimSpace(options.RunDir) != "" {
		var err error
		runDir, err = resolveWorkflowRunDir(worktree, options.WorkflowID, options.RunDir)
		if err != nil {
			result.add("run-dir", err.Error())
			return result
		}
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
	if strings.TrimSpace(options.RunDir) != "" {
		var err error
		runDir, err = resolveWorkflowRunDir(worktree, options.WorkflowID, options.RunDir)
		if err != nil {
			result.add("run-dir", err.Error())
			return WorkflowFinalVerificationArtifact{}, result
		}
		if err := requireWorkflowPathUnderRunDir(worktree, runDir, "attempts-file", options.AttemptsFile, false); err != nil {
			result.add("attempts-file", err.Error())
		}
		if err := requireWorkflowPathUnderRunDir(worktree, runDir, "output", options.OutputArtifact, true); err != nil {
			result.add("output", err.Error())
		}
		if err := requireWorkflowPathUnderRunDir(worktree, runDir, "final-qa-artifact", options.FinalQAArtifact, false); err != nil {
			result.add("final-qa-artifact", err.Error())
		}
		if !result.OK() {
			return WorkflowFinalVerificationArtifact{}, result
		}
	}
	attemptText := strings.TrimSpace(options.AttemptsJSON)
	if strings.TrimSpace(options.AttemptsFile) != "" {
		if attemptText != "" {
			result.add("attempts", "use only one of --attempts-json or --attempts-file")
			return WorkflowFinalVerificationArtifact{}, result
		}
		path := resolvePath(worktree, options.AttemptsFile)
		data, err := os.ReadFile(path)
		if err != nil {
			result.add("attempts-file", "cannot read attempts file: "+err.Error())
			return WorkflowFinalVerificationArtifact{}, result
		}
		attemptText = strings.TrimSpace(string(data))
	}
	if attemptText == "" {
		result.add("attempts", "--attempts-json or --attempts-file is required")
		return WorkflowFinalVerificationArtifact{}, result
	}

	var attempts []WorkflowFinalVerificationAttempt
	if err := json.Unmarshal([]byte(attemptText), &attempts); err != nil {
		result.add("attempts", "attempts JSON must be an array: "+err.Error())
		return WorkflowFinalVerificationArtifact{}, result
	}
	if len(attempts) == 0 {
		result.add("attempts", "at least one attempt is required")
		return WorkflowFinalVerificationArtifact{}, result
	}

	accepted := make([]WorkflowFinalVerificationAttempt, 0, len(attempts))
	for i, attempt := range attempts {
		if attemptAccepted(attempt) {
			accepted = append(accepted, attempt)
			artifact := strings.TrimSpace(attemptString(attempt, "artifact"))
			if artifact == "" {
				result.add(fmt.Sprintf("attempts[%d].artifact", i), "accepted attempt is missing artifact")
				continue
			}
			if runDir != "" {
				if err := requireWorkflowPathUnderRunDir(worktree, runDir, fmt.Sprintf("attempts[%d].artifact", i), artifact, false); err != nil {
					result.add(fmt.Sprintf("attempts[%d].artifact", i), err.Error())
					continue
				}
			}
			artifactPath := resolvePath(worktree, artifact)
			if cleanupScratchPath(worktree, artifactPath) {
				result.add(fmt.Sprintf("attempts[%d].artifact", i), "accepted attempt artifact cannot be under cleanup scratch: "+slash(artifactPath))
				continue
			}
			if !isFile(artifactPath) {
				result.add(fmt.Sprintf("attempts[%d].artifact", i), "accepted attempt artifact does not exist: "+slash(artifactPath))
			}
		}
	}
	if len(accepted) == 0 {
		result.add("acceptedAttempts", "at least one accepted PASS attempt is required")
	}

	status := "PASS"
	if !result.OK() {
		status = "FAIL"
	}
	artifact := WorkflowFinalVerificationArtifact{
		SchemaVersion:    1,
		WorkflowID:       options.WorkflowID,
		ChangeSnapshot:   options.ChangeSnapshot,
		Status:           status,
		Attempts:         attempts,
		AcceptedAttempts: accepted,
	}
	output := strings.TrimSpace(options.OutputArtifact)
	if output == "" {
		if runDir != "" {
			output = relativePath(worktree, filepath.Join(runDir, "final-verification.json"))
		} else {
			suffix := strings.TrimSpace(options.WorkflowID)
			if suffix == "" {
				suffix = "workflow"
			}
			output = filepath.ToSlash(filepath.Join(".claude", "gates", "artifacts", "final-verification-"+suffix+".json"))
		}
	}
	outputPath := resolvePath(worktree, output)
	if cleanupScratchPath(worktree, outputPath) {
		result.add("output", "final verification artifact cannot be under cleanup scratch: "+slash(outputPath))
		return artifact, result
	}
	if err := writeFinalVerificationArtifact(outputPath, artifact); err != nil {
		result.add("output", err.Error())
	}
	if options.RecordFinalQA {
		recordResult := recordFinalQA(worktree, runDir, artifact.Status, options)
		result.Failures = append(result.Failures, recordResult.Failures...)
	}
	return artifact, result
}

func recordFinalQA(worktree, runDir, status string, options WorkflowFinalVerificationOptions) Result {
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
	if !isFile(finalQAPath) {
		result.add("final-qa-artifact", "final QA artifact does not exist: "+finalQA)
		return result
	}
	actor := strings.TrimSpace(options.Actor)
	if actor == "" {
		actor = "gate-workflow"
	}
	record := GateRecord(GateRecordOptions{
		Worktree:       worktree,
		StatePath:      workflowStatePath(worktree, options.StatePath, runDir),
		RunDir:         runDir,
		Gate:           "qa-test-gate",
		Verdict:        status,
		Mode:           "formal",
		Stage:          "FinalExecution",
		Artifact:       finalQA,
		Actor:          actor,
		WorkflowID:     options.WorkflowID,
		ChangeSnapshot: options.ChangeSnapshot,
	})
	return record
}

func WorkflowCleanup(options WorkflowCleanupOptions) (WorkflowCleanupReport, Result) {
	worktree := cleanRoot(options.Worktree)
	var result Result
	if !isDir(worktree) {
		result.add("worktree", "worktree does not exist: "+worktree)
		return WorkflowCleanupReport{}, result
	}
	paths := options.Paths
	if len(paths) == 0 {
		paths = defaultCleanupPaths(worktree)
	}
	report := WorkflowCleanupReport{
		SchemaVersion: 1,
		Worktree:      slash(absPath(worktree)),
		DryRun:        !options.Execute,
		Paths:         make([]WorkflowCleanupRecord, 0, len(paths)),
	}
	for _, value := range paths {
		full, err := allowedCleanupPath(worktree, value)
		if err != nil {
			result.add("cleanup", err.Error())
			continue
		}
		record := WorkflowCleanupRecord{Path: slash(full)}
		if !exists(full) {
			record.Status = "missing"
			report.Paths = append(report.Paths, record)
			continue
		}
		if !options.Execute {
			record.Status = "would-remove"
			report.Paths = append(report.Paths, record)
			continue
		}
		if err := os.RemoveAll(full); err != nil {
			result.add(slash(full), "cleanup remove failed: "+err.Error())
			record.Status = "remove-failed"
		} else {
			record.Status = "removed"
		}
		report.Paths = append(report.Paths, record)
	}
	return report, result
}

func WorkflowCompact(options WorkflowCompactOptions) (WorkflowCompactArchive, Result) {
	worktree := cleanRoot(options.Worktree)
	var result Result
	if !isDir(worktree) {
		result.add("worktree", "worktree does not exist: "+worktree)
		return WorkflowCompactArchive{}, result
	}
	workflowID := strings.TrimSpace(options.WorkflowID)
	if workflowID == "" {
		result.add("workflow-id", "--workflow-id is required")
		return WorkflowCompactArchive{}, result
	}
	runDir, err := resolveWorkflowRunDir(worktree, workflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return WorkflowCompactArchive{}, result
	}
	if !isDir(runDir) {
		result.add("run-dir", "run directory does not exist: "+slash(runDir))
		return WorkflowCompactArchive{}, result
	}
	output := filepath.Join(runDir, "formal-gates-workflow-archive.json")

	paths, err := workflowRunFiles(runDir, output)
	if err != nil {
		result.add("compact", err.Error())
		return WorkflowCompactArchive{}, result
	}
	files := make([]WorkflowCompactFile, 0, len(paths))
	cleanup := make([]WorkflowCleanupRecord, 0, len(paths))
	for _, path := range paths {
		if samePath(path, output) {
			continue
		}
		file := WorkflowCompactFile{Path: relativePath(worktree, path)}
		files = append(files, file)
		cleanup = append(cleanup, WorkflowCleanupRecord{Path: file.Path, Status: "would-remove"})
	}
	otherCleanup, err := orphanedArchivedRunFiles(worktree, runDir)
	if err != nil {
		result.add("runs", err.Error())
		return WorkflowCompactArchive{}, result
	}
	if !result.OK() {
		return WorkflowCompactArchive{}, result
	}
	archive := WorkflowCompactArchive{
		SchemaVersion:   1,
		WorkflowID:      workflowID,
		ChangeSnapshot:  strings.TrimSpace(options.ChangeSnapshot),
		RunDir:          relativePath(worktree, runDir),
		Files:           files,
		Cleanup:         cleanup,
		OtherRunCleanup: otherCleanup,
		DryRun:          !options.Execute,
	}
	if !options.Execute {
		return archive, result
	}
	if err := writeWorkflowCompactArchive(worktree, output, archive); err != nil {
		result.add("output", err.Error())
		return archive, result
	}
	if err := verifyWorkflowCompactArchive(output, len(archive.Files)); err != nil {
		result.add("output", "archive verification failed: "+err.Error())
		return archive, result
	}
	removeWorkflowFiles(worktree, archive.Cleanup, &result)
	removeWorkflowFiles(worktree, archive.OtherRunCleanup, &result)
	archive.DryRun = false
	if err := removeEmptyDirsUnder(runDir); err != nil {
		result.add("run-dir", "cannot remove empty run directories: "+err.Error())
	}
	runsRoot := filepath.Join(absPath(worktree), ".claude", "gates", "runs")
	if isDir(runsRoot) {
		if err := removeEmptyDirsUnder(runsRoot); err != nil {
			result.add("runs", "cannot remove empty archived run directories: "+err.Error())
		}
	}
	return archive, result
}

func workflowStatePath(worktree, statePath, runDir string) string {
	if strings.TrimSpace(statePath) != "" {
		return resolveStatePath(worktree, statePath)
	}
	if strings.TrimSpace(runDir) != "" {
		return filepath.Join(runDir, "gate-state.json")
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
		runDir = filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID))
	}
	full := absPath(resolvePath(worktreeAbs, runDir))
	runsRoot := filepath.Join(worktreeAbs, ".claude", "gates", "runs")
	if samePath(full, runsRoot) || !pathUnder(full, runsRoot) {
		return "", fmt.Errorf("run directory must be under .claude/gates/runs: %s", slash(full))
	}
	return full, nil
}

func requireWorkflowPathUnderRunDir(worktree, runDir, label, value string, allowEmpty bool) error {
	if strings.TrimSpace(value) == "" {
		if allowEmpty {
			return nil
		}
		return nil
	}
	return requireAbsPathUnderRunDir(runDir, label, resolvePath(worktree, value))
}

func requireAbsPathUnderRunDir(runDir, label, path string) error {
	full := absPath(path)
	if samePath(full, runDir) || !pathUnder(full, runDir) {
		return fmt.Errorf("%s must be under --run-dir: %s", label, slash(full))
	}
	return nil
}

func workflowRunFiles(runDir, output string) ([]string, error) {
	paths := map[string]bool{}
	err := filepath.WalkDir(runDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeType != 0 {
			return nil
		}
		full := absPath(path)
		if !samePath(full, output) {
			paths[full] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func removeWorkflowFiles(worktree string, records []WorkflowCleanupRecord, result *Result) {
	for i, record := range records {
		path := resolvePath(worktree, record.Path)
		if !isFile(path) {
			records[i].Status = "missing"
			continue
		}
		if err := os.Remove(path); err != nil {
			records[i].Status = "remove-failed"
			result.add(record.Path, "cleanup remove failed: "+err.Error())
			continue
		}
		records[i].Status = "removed"
	}
}

func writeWorkflowCompactArchive(worktree, path string, archive WorkflowCompactArchive) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	out := archive
	out.Files = append([]WorkflowCompactFile(nil), archive.Files...)
	for _, item := range archive.Files {
		data, err := os.ReadFile(resolvePath(worktree, item.Path))
		if err != nil {
			return err
		}
		for i := range out.Files {
			if out.Files[i].Path == item.Path {
				out.Files[i].ContentBase64 = base64.StdEncoding.EncodeToString(data)
				break
			}
		}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func verifyWorkflowCompactArchive(path string, expectedFiles int) error {
	var archive WorkflowCompactArchive
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &archive); err != nil {
		return err
	}
	if len(archive.Files) != expectedFiles {
		return fmt.Errorf("expected %d archived files, got %d", expectedFiles, len(archive.Files))
	}
	for _, file := range archive.Files {
		if _, err := base64.StdEncoding.DecodeString(file.ContentBase64); err != nil {
			return fmt.Errorf("invalid content for %s: %w", file.Path, err)
		}
	}
	return nil
}

func removeEmptyDirsUnder(root string) error {
	var dirs []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && !samePath(path, root) {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, dir := range dirs {
		if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
			if entries, readErr := os.ReadDir(dir); readErr == nil && len(entries) > 0 {
				continue
			}
			return err
		}
	}
	return nil
}

func orphanedArchivedRunFiles(worktree, currentRunDir string) ([]WorkflowCleanupRecord, error) {
	worktreeAbs := absPath(worktree)
	runsRoot := filepath.Join(worktreeAbs, ".claude", "gates", "runs")
	if !isDir(runsRoot) {
		return nil, nil
	}
	var records []WorkflowCleanupRecord
	err := filepath.WalkDir(runsRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() || samePath(path, runsRoot) {
			return nil
		}
		if samePath(path, currentRunDir) || pathUnder(path, currentRunDir) {
			return filepath.SkipDir
		}
		archive := workflowArchiveInDir(path)
		if archive == "" {
			return nil
		}
		return filepath.WalkDir(path, func(candidate string, candidateEntry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if candidateEntry.IsDir() {
				return nil
			}
			if candidateEntry.Type()&os.ModeType != 0 {
				return nil
			}
			if samePath(candidate, archive) {
				return nil
			}
			records = append(records, WorkflowCleanupRecord{
				Path:   relativePath(worktreeAbs, candidate),
				Status: "would-remove",
			})
			return nil
		})
	})
	sort.Slice(records, func(i, j int) bool {
		return records[i].Path < records[j].Path
	})
	return records, err
}

func workflowArchiveInDir(dir string) string {
	for _, name := range []string{"formal-gates-workflow-archive.zip", "formal-gates-workflow-archive.json"} {
		path := filepath.Join(dir, name)
		if isFile(path) {
			return path
		}
	}
	return ""
}

func fileHashSnapshot(worktree, detectedVCS string) (WorkflowSnapshotRecord, error) {
	digest, err := fileTreeDigest(worktree)
	if err != nil {
		return WorkflowSnapshotRecord{}, err
	}
	treeHash := textHash(digest)
	prefix := "files"
	if detectedVCS == "svn" {
		prefix = "svn-files"
	}
	return WorkflowSnapshotRecord{
		VCS:                detectedVCS,
		RangeHash:          treeHash,
		IncludeWorkingTree: true,
		WorkingTreeHash:    treeHash,
		ChangeSnapshot:     prefix + "." + treeHash[:12],
	}, nil
}

func gitSnapshot(worktree string, options WorkflowSnapshotOptions) (WorkflowSnapshotRecord, error) {
	baseRef := strings.TrimSpace(options.BaseRef)
	if baseRef == "" {
		return WorkflowSnapshotRecord{}, fmt.Errorf("--base-ref is required for git snapshot")
	}
	headRef := strings.TrimSpace(options.HeadRef)
	if headRef == "" {
		headRef = "HEAD"
	}
	baseCommit, err := gitText(worktree, "rev-parse", baseRef)
	if err != nil {
		return WorkflowSnapshotRecord{}, fmt.Errorf("git rev-parse base failed: %w", err)
	}
	headCommit, err := gitText(worktree, "rev-parse", headRef)
	if err != nil {
		return WorkflowSnapshotRecord{}, fmt.Errorf("git rev-parse head failed: %w", err)
	}
	status, err := gitText(worktree, "status", "--short")
	if err != nil {
		return WorkflowSnapshotRecord{}, fmt.Errorf("git status failed: %w", err)
	}
	if strings.TrimSpace(status) != "" && !options.IncludeWorkingTree {
		return WorkflowSnapshotRecord{}, fmt.Errorf("git worktree is dirty; pass --include-working-tree to include it")
	}
	rangeDiff, err := gitText(worktree, "diff", "--binary", baseCommit+".."+headCommit)
	if err != nil {
		return WorkflowSnapshotRecord{}, fmt.Errorf("git diff range failed: %w", err)
	}
	rangeHash := textHash(rangeDiff)
	snapshot := WorkflowSnapshotRecord{
		VCS:                "git",
		BaseRef:            baseRef,
		BaseCommit:         baseCommit,
		HeadRef:            headRef,
		HeadCommit:         headCommit,
		RangeHash:          rangeHash,
		IncludeWorkingTree: options.IncludeWorkingTree,
		ChangeSnapshot:     baseCommit[:12] + ".." + headCommit[:12] + "+" + rangeHash[:12],
	}
	if options.IncludeWorkingTree {
		workingDiff, err := gitText(worktree, "diff", "--binary")
		if err != nil {
			return WorkflowSnapshotRecord{}, fmt.Errorf("git diff working tree failed: %w", err)
		}
		cachedDiff, err := gitText(worktree, "diff", "--binary", "--cached")
		if err != nil {
			return WorkflowSnapshotRecord{}, fmt.Errorf("git diff cached failed: %w", err)
		}
		untracked, err := gitText(worktree, "ls-files", "--others", "--exclude-standard")
		if err != nil {
			return WorkflowSnapshotRecord{}, fmt.Errorf("git ls-files untracked failed: %w", err)
		}
		untrackedDigest, err := untrackedContentDigest(worktree, untracked)
		if err != nil {
			return WorkflowSnapshotRecord{}, err
		}
		workingHash := textHash(status + "\n" + cachedDiff + "\n" + workingDiff + "\n" + untrackedDigest)
		snapshot.WorkingTreeHash = workingHash
		snapshot.ChangeSnapshot = baseCommit[:12] + ".." + headCommit[:12] + "+wt." + workingHash[:12]
	}
	return snapshot, nil
}

func fileTreeDigest(worktree string) (string, error) {
	var entries []string
	err := filepath.WalkDir(worktree, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == worktree {
			return nil
		}
		rel, err := filepath.Rel(worktree, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if ignoredSnapshotDir(rel, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeType != 0 {
			return nil
		}
		hash := sha256File(path)
		if hash == "" {
			return fmt.Errorf("cannot hash file: %s", rel)
		}
		entries = append(entries, rel+" sha256="+hash)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n"), nil
}

func ignoredSnapshotDir(rel, name string) bool {
	switch name {
	case ".git", ".svn", ".hg", "node_modules", "__pycache__":
		return true
	}
	switch rel {
	case ".claude/gates", ".artifacts/tmp", ".artifacts/scratch", ".artifacts/cleanup":
		return true
	}
	return strings.HasPrefix(rel, ".claude/gates/") ||
		strings.HasPrefix(rel, ".artifacts/tmp/") ||
		strings.HasPrefix(rel, ".artifacts/scratch/") ||
		strings.HasPrefix(rel, ".artifacts/cleanup/")
}

func attemptAccepted(attempt WorkflowFinalVerificationAttempt) bool {
	return attemptBool(attempt, "accepted") && strings.EqualFold(strings.TrimSpace(attemptString(attempt, "status")), "PASS")
}

func attemptBool(attempt WorkflowFinalVerificationAttempt, key string) bool {
	value, ok := attempt[key]
	if !ok {
		return false
	}
	if b, ok := value.(bool); ok {
		return b
	}
	if s, ok := value.(string); ok {
		return strings.EqualFold(strings.TrimSpace(s), "true")
	}
	return false
}

func attemptString(attempt WorkflowFinalVerificationAttempt, key string) string {
	value, ok := attempt[key]
	if !ok || value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func writeFinalVerificationArtifact(path string, artifact WorkflowFinalVerificationArtifact) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func allowedCleanupPath(worktree, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("cleanup path is required")
	}
	worktreeAbs := absPath(worktree)
	full := resolvePath(worktreeAbs, value)
	fullAbs := absPath(full)
	if samePath(fullAbs, worktreeAbs) {
		return "", fmt.Errorf("cleanup refuses repo root: %s", slash(fullAbs))
	}
	if !pathUnder(fullAbs, worktreeAbs) {
		return "", fmt.Errorf("cleanup refuses path outside worktree: %s", slash(fullAbs))
	}
	artifactsRoot := filepath.Join(worktreeAbs, ".artifacts")
	if samePath(fullAbs, artifactsRoot) {
		return "", fmt.Errorf("cleanup refuses .artifacts root: %s", slash(fullAbs))
	}
	gateRoot := filepath.Join(worktreeAbs, ".claude", "gates")
	if samePath(fullAbs, gateRoot) || pathUnder(fullAbs, gateRoot) {
		return "", fmt.Errorf("cleanup refuses formal gate evidence: %s", slash(fullAbs))
	}
	for _, rel := range []string{".artifacts/tmp", ".artifacts/scratch", ".artifacts/cleanup"} {
		root := filepath.Join(worktreeAbs, filepath.FromSlash(rel))
		if pathUnder(fullAbs, root) {
			return fullAbs, nil
		}
		if samePath(fullAbs, root) {
			return "", fmt.Errorf("cleanup path must be a descendant under %s: %s", rel, slash(fullAbs))
		}
	}
	return "", fmt.Errorf("cleanup path must be under .artifacts/tmp, .artifacts/scratch, or .artifacts/cleanup: %s", slash(fullAbs))
}

func defaultCleanupPaths(worktree string) []string {
	worktreeAbs := absPath(worktree)
	var paths []string
	for _, rel := range []string{".artifacts/tmp", ".artifacts/scratch", ".artifacts/cleanup"} {
		root := filepath.Join(worktreeAbs, filepath.FromSlash(rel))
		if !isDir(root) {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || path == root {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			paths = append(paths, path)
			return nil
		})
	}
	sort.Strings(paths)
	return paths
}

func cleanupScratchPath(worktree, path string) bool {
	full := absPath(path)
	worktreeAbs := absPath(worktree)
	if !pathUnder(full, worktreeAbs) {
		return false
	}
	for _, rel := range []string{".artifacts/tmp", ".artifacts/scratch", ".artifacts/cleanup"} {
		root := filepath.Join(worktreeAbs, filepath.FromSlash(rel))
		if samePath(full, root) || pathUnder(full, root) {
			return true
		}
	}
	return false
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
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

func untrackedContentDigest(worktree, untracked string) (string, error) {
	var entries []string
	for _, rel := range strings.Split(untracked, "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		path := filepath.Join(worktree, filepath.FromSlash(rel))
		if !isFile(path) {
			continue
		}
		hash := sha256File(path)
		if hash == "" {
			return "", fmt.Errorf("cannot hash untracked file: %s", rel)
		}
		entries = append(entries, rel+" sha256="+hash)
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n"), nil
}

func isGitWorktree(worktree string) bool {
	out, err := gitText(worktree, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

func hasAncestorDir(root, marker string) bool {
	current, err := filepath.Abs(root)
	if err != nil {
		current = filepath.Clean(root)
	}
	for {
		if isDir(filepath.Join(current, marker)) {
			return true
		}
		next := filepath.Dir(current)
		if next == current {
			return false
		}
		current = next
	}
}

func gitText(worktree string, args ...string) (string, error) {
	all := append([]string{"-C", worktree}, args...)
	cmd := exec.Command("git", all...)
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

func textHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
