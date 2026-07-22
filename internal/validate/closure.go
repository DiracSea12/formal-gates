package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type EvidenceClosure struct {
	SchemaVersion  int            `json:"schemaVersion"`
	WorkflowID     string         `json:"workflowId"`
	ChangeSnapshot string         `json:"changeSnapshot"`
	Gate           string         `json:"gate"`
	Stage          string         `json:"stage"`
	Verdict        string         `json:"verdict"`
	RootRole       string         `json:"rootRole"`
	RootArtifact   string         `json:"rootArtifact"`
	Receipt        string         `json:"receipt,omitempty"`
	Entries        []ClosureEntry `json:"entries"`
}

type ClosureEntry struct {
	Path       string   `json:"path"`
	SHA256     string   `json:"sha256"`
	References []string `json:"references"`
}

func buildClosure(options ArtifactOptions, artifact decodedArtifact, receipt *EvidenceRef) (EvidenceRef, error) {
	rootPath, err := logicalPathInRun(artifact.RunDir, resolvePath(options.Root, options.File))
	if err != nil {
		return EvidenceRef{}, err
	}
	refs := map[string][]EvidenceRef{}
	for owner, values := range artifact.References {
		ownerPath := owner
		if owner == options.File {
			ownerPath = rootPath
		}
		refs[ownerPath] = append([]EvidenceRef{}, values...)
	}
	if receipt != nil {
		dependencies, err := receiptClosureDependencies(options, artifact.RunDir, *receipt)
		if err != nil {
			return EvidenceRef{}, err
		}
		refs[receipt.Path] = dependencies
	}
	entries := map[string]ClosureEntry{}
	var visit func(string) error
	visit = func(logical string) error {
		if _, ok := entries[logical]; ok {
			return nil
		}
		path, err := safeEvidencePath(artifact.RunDir, logical)
		if err != nil {
			return err
		}
		entry := ClosureEntry{Path: logical, SHA256: sha256File(path), References: []string{}}
		childHashes := map[string]string{}
		for _, ref := range refs[logical] {
			if previous, exists := childHashes[ref.Path]; exists {
				if previous != ref.SHA256 {
					return fmt.Errorf("conflicting evidence hashes for %s", ref.Path)
				}
				continue
			}
			childHashes[ref.Path] = ref.SHA256
			entry.References = append(entry.References, ref.Path)
		}
		sort.Strings(entry.References)
		entries[logical] = entry
		for _, child := range entry.References {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(rootPath); err != nil {
		return EvidenceRef{}, err
	}
	receiptPath := ""
	if receipt != nil {
		receiptPath = receipt.Path
		if err := visit(receipt.Path); err != nil {
			return EvidenceRef{}, err
		}
	}
	list := make([]ClosureEntry, 0, len(entries))
	for _, entry := range entries {
		list = append(list, entry)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	closure := EvidenceClosure{
		SchemaVersion: 2, WorkflowID: artifact.Envelope.WorkflowID,
		ChangeSnapshot: artifact.Envelope.ChangeSnapshot, Gate: artifact.Envelope.Gate,
		Stage: artifact.Envelope.Stage, Verdict: artifact.Envelope.Verdict,
		RootRole: artifact.Envelope.ArtifactRole, RootArtifact: rootPath,
		Receipt: receiptPath, Entries: list,
	}
	if err := verifyClosure(options, artifact.RunDir, closure); err != nil {
		return EvidenceRef{}, err
	}
	data, err := json.MarshalIndent(closure, "", "  ")
	if err != nil {
		return EvidenceRef{}, err
	}
	name := strings.ReplaceAll(artifact.Envelope.Gate+"-"+artifact.Envelope.Stage, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.Trim(name, "-") + "-" + sha256Bytes(data) + ".json"
	path := filepath.Join(artifact.RunDir, "restricted", "closures", name)
	if err := writeFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return EvidenceRef{}, err
	}
	logical, err := logicalPathInRun(artifact.RunDir, path)
	if err != nil {
		return EvidenceRef{}, err
	}
	return EvidenceRef{Path: logical, SHA256: sha256File(path)}, nil
}

func receiptClosureDependencies(options ArtifactOptions, runDir string, ref EvidenceRef) ([]EvidenceRef, error) {
	path, err := safeEvidencePath(runDir, ref.Path)
	if err != nil || sha256File(path) != ref.SHA256 {
		return nil, fmt.Errorf("closure receipt path or hash is invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var receipt reviewerProofReceipt
	if err := strictContractJSON(data, &receipt); err != nil {
		return nil, fmt.Errorf("closure receipt is invalid JSON")
	}
	values := []EvidenceRef{
		{Path: receipt.DispatchRegistrationArtifact, SHA256: receipt.DispatchRegistrationSha256},
	}
	if providerRequiresLifecycle(receipt.Provider) {
		values = append(values,
			EvidenceRef{Path: receipt.StartEventArtifact, SHA256: receipt.StartEventSha256},
			EvidenceRef{Path: receipt.StopEventArtifact, SHA256: receipt.StopEventSha256},
		)
	}
	if reviewJudgmentLifecycle(receipt.Gate, receipt.Stage) {
		var result Result
		if !validateFinalSendPrompt(options.Root, runDir, receipt.PromptArtifact, receipt.PromptSha256, receipt.Gate, receipt.Stage, &result, ref.Path) {
			return nil, fmt.Errorf("closure final-send prompt validation failed: %s", result.Failures[0].Message)
		}
		values = append(values, EvidenceRef{Path: receipt.PromptArtifact, SHA256: receipt.PromptSha256})
	}
	dependencies := make([]EvidenceRef, 0, len(values))
	for _, value := range values {
		if activeWorkflowRun(options.Root, runDir) && !restrictedRepoPath(options.Root, runDir, value.Path) {
			return nil, fmt.Errorf("closure receipt dependency is outside the active run restricted directory: %s", value.Path)
		}
		resolved := resolvePath(options.Root, value.Path)
		logical, err := logicalPathInRun(runDir, resolved)
		if err != nil || logical == ref.Path || !isSHA256(value.SHA256) || sha256File(resolved) != value.SHA256 {
			return nil, fmt.Errorf("closure receipt dependency is invalid: %s", value.Path)
		}
		dependencies = append(dependencies, EvidenceRef{Path: logical, SHA256: value.SHA256})
	}
	return dependencies, nil
}

func verifyClosure(options ArtifactOptions, runDir string, closure EvidenceClosure) error {
	if closure.SchemaVersion != 2 || closure.WorkflowID == "" || closure.ChangeSnapshot == "" || closure.Verdict != "PASS" || closure.RootArtifact == "" || len(closure.Entries) == 0 {
		return fmt.Errorf("closure header is invalid")
	}
	entries := map[string]ClosureEntry{}
	resolvedPaths := map[string]string{}
	reviewerClosure := strings.HasSuffix(closure.RootRole, "_REVIEW")
	receiptRequired := reviewerClosure || closure.RootRole == "CARRY_ARBITER"
	for _, entry := range closure.Entries {
		if _, exists := entries[entry.Path]; exists {
			return fmt.Errorf("closure contains duplicate path: %s", entry.Path)
		}
		if activeWorkflowRun(options.Root, runDir) && !restrictedEvidencePath(options.Root, runDir, entry.Path) {
			return fmt.Errorf("closure entry is outside the active run restricted directory: %s", entry.Path)
		}
		path, err := safeEvidencePath(runDir, entry.Path)
		if err != nil {
			return err
		}
		if previous, exists := resolvedPaths[path]; exists && previous != entry.Path {
			return fmt.Errorf("closure paths %s and %s resolve to the same file", previous, entry.Path)
		}
		resolvedPaths[path] = entry.Path
		if !isSHA256(entry.SHA256) || sha256File(path) != entry.SHA256 {
			return fmt.Errorf("closure entry hash mismatch: %s", entry.Path)
		}
		if !sort.StringsAreSorted(entry.References) || hasDuplicate(entry.References) {
			return fmt.Errorf("closure references must be sorted and unique: %s", entry.Path)
		}
		entries[entry.Path] = entry
	}
	if _, ok := entries[closure.RootArtifact]; !ok {
		return fmt.Errorf("closure root artifact is missing")
	}
	if closure.Receipt != "" {
		if _, ok := entries[closure.Receipt]; !ok {
			return fmt.Errorf("closure receipt is missing")
		}
	}
	if receiptRequired && closure.Receipt == "" {
		return fmt.Errorf("receipt-bound closure is missing its receipt")
	}
	state := map[string]int{}
	var walk func(string) error
	walk = func(path string) error {
		if state[path] == 1 {
			return fmt.Errorf("closure reference cycle at %s", path)
		}
		if state[path] == 2 {
			return nil
		}
		state[path] = 1
		for _, child := range entries[path].References {
			if _, ok := entries[child]; !ok {
				return fmt.Errorf("closure reference is missing: %s", child)
			}
			if err := walk(child); err != nil {
				return err
			}
		}
		state[path] = 2
		return nil
	}
	for path := range entries {
		if err := walk(path); err != nil {
			return err
		}
	}
	return nil
}

func logicalPathInRun(runDir, path string) (string, error) {
	base, err := filepath.Abs(runDir)
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	if resolved, err := filepath.EvalSymlinks(full); err == nil {
		full = resolved
	}
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside active run: %s", path)
	}
	return filepath.ToSlash(rel), nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".formal-gates-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceCompletedFile(tmpPath, path)
}
