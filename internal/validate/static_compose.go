package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func writeBytesExclusive(path string, data []byte) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	return file.Close()
}

type ComposeContextBundleOptions struct {
	Root, RunDir, WorkflowID, ChangeSnapshot, Output string
	Inputs                                           []string
}

type CompositionProof struct {
	ProofVersion   int           `json:"proofVersion"`
	Composer       string        `json:"composer"`
	WorkflowID     string        `json:"workflowId"`
	ChangeSnapshot string        `json:"changeSnapshot"`
	Outputs        []EvidenceRef `json:"outputs"`
}

func compositionProofPath(root, runDir, composer, artifactPath string) (string, error) {
	logical, err := logicalPathInRun(runDir, artifactPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(receiptProofDir(root, runDir, "compositions"), sha256Bytes([]byte(composer+"\n"+logical))+".json"), nil
}

func writeCompositionProof(root, runDir, composer, workflowID, snapshot, artifactPath string, outputs []EvidenceRef) (EvidenceRef, error) {
	path, err := compositionProofPath(root, runDir, composer, artifactPath)
	if err != nil {
		return EvidenceRef{}, err
	}
	proof := CompositionProof{ProofVersion: 1, Composer: composer, WorkflowID: workflowID, ChangeSnapshot: snapshot, Outputs: outputs}
	if err := writeJSONExclusive(path, proof); err != nil {
		return EvidenceRef{}, err
	}
	logical, err := logicalPathInRun(runDir, path)
	if err != nil {
		_ = os.Remove(path)
		return EvidenceRef{}, err
	}
	return EvidenceRef{Path: logical, SHA256: sha256File(path)}, nil
}

func validateStandaloneCompositionProof(root, runDir, composer, workflowID, snapshot string, output EvidenceRef) error {
	return validateCompositionProofOutputs(root, runDir, composer, workflowID, snapshot, output, []EvidenceRef{output})
}

func validateCompositionProofOutputs(root, runDir, composer, workflowID, snapshot string, anchor EvidenceRef, outputs []EvidenceRef) error {
	anchorPath, err := safeEvidencePath(runDir, anchor.Path)
	if err != nil {
		return fmt.Errorf("generated output path or hash is invalid")
	}
	for _, output := range outputs {
		artifactPath, outputErr := safeEvidencePath(runDir, output.Path)
		if outputErr != nil || sha256File(artifactPath) != output.SHA256 {
			return fmt.Errorf("generated output path or hash is invalid")
		}
	}
	proofPath, err := compositionProofPath(root, runDir, composer, anchorPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(proofPath)
	if err != nil {
		return fmt.Errorf("CLI composition proof is missing")
	}
	var proof CompositionProof
	if err := strictContractJSON(data, &proof); err != nil || proof.ProofVersion != 1 || proof.Composer != composer || proof.WorkflowID != workflowID || proof.ChangeSnapshot != snapshot || !reflect.DeepEqual(proof.Outputs, outputs) {
		return fmt.Errorf("CLI composition proof is invalid")
	}
	return nil
}

type ComposeChangedFilesOptions struct {
	Root, RunDir, WorkflowID, ChangeSnapshot, Output string
	Paths                                            []string
}

func ComposeChangedFiles(options ComposeChangedFilesOptions) (EvidenceRef, Result) {
	var result Result
	root := cleanWorktree(options.Root)
	runDir, err := resolveWorkflowRunDir(root, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return EvidenceRef{}, result
	}
	if !meaningful(options.WorkflowID) || !meaningful(options.ChangeSnapshot) || len(options.Paths) == 0 {
		result.add("changed-files", "--workflow-id, --change-snapshot, and at least one --path are required")
		return EvidenceRef{}, result
	}
	if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
		result.add("run-dir", err.Error())
		return EvidenceRef{}, result
	}
	pathSet := make(map[string]bool, len(options.Paths))
	for _, value := range options.Paths {
		path, pathErr := normalizeRepositoryRelativePath(value)
		if pathErr != nil {
			result.add("path", pathErr.Error())
			continue
		}
		pathSet[path] = true
	}
	if !result.OK() {
		return EvidenceRef{}, result
	}
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	outputPath, pathErr := prospectiveRestrictedPath(runDir, options.Output)
	if pathErr != nil {
		result.add(options.Output, pathErr.Error())
		return EvidenceRef{}, result
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		result.add(options.Output, err.Error())
		return EvidenceRef{}, result
	}
	if err := writeBytesExclusive(outputPath, []byte(strings.Join(paths, "\n")+"\n")); err != nil {
		result.add(options.Output, err.Error())
		return EvidenceRef{}, result
	}
	logical, _ := logicalPathInRun(runDir, outputPath)
	ref := EvidenceRef{Path: logical, SHA256: sha256File(outputPath)}
	if _, proofErr := writeCompositionProof(root, runDir, "changed-files.v1", options.WorkflowID, options.ChangeSnapshot, outputPath, []EvidenceRef{ref}); proofErr != nil {
		_ = os.Remove(outputPath)
		result.add(options.Output, proofErr.Error())
		return EvidenceRef{}, result
	}
	return ref, result
}

func validateCompositionProof(options ArtifactOptions, artifact *decodedArtifact, result *Result) {
	composer := ""
	wantOutputs := []EvidenceRef{}
	switch artifact.Envelope.ArtifactRole {
	case "REQUIREMENTS_PASS":
		composer = "requirements.v1"
		if artifact.Requirements != nil {
			wantOutputs = append(wantOutputs, artifact.Requirements.Alignment, artifact.Requirements.Decision)
		}
	case "QA_EXECUTION":
		composer = "qa-execution.v1"
	default:
		return
	}
	artifactPath := resolvePath(options.Root, options.File)
	logical, err := logicalPathInRun(artifact.RunDir, artifactPath)
	if err != nil {
		result.add(options.File, "formal artifact composition proof path is invalid")
		return
	}
	rootRef := EvidenceRef{Path: logical, SHA256: sha256File(artifactPath)}
	wantOutputs = append(wantOutputs, rootRef)
	proofPath, err := compositionProofPath(options.Root, artifact.RunDir, composer, artifactPath)
	if err != nil {
		result.add(options.File, err.Error())
		return
	}
	data, err := os.ReadFile(proofPath)
	if err != nil {
		result.add(options.File, "CLI composition proof is missing")
		return
	}
	var proof CompositionProof
	if err := strictContractJSON(data, &proof); err != nil || proof.ProofVersion != 1 || proof.Composer != composer || proof.WorkflowID != artifact.Envelope.WorkflowID || proof.ChangeSnapshot != artifact.Envelope.ChangeSnapshot || !reflect.DeepEqual(proof.Outputs, wantOutputs) {
		result.add(options.File, "CLI composition proof does not match the formal artifact")
		return
	}
	proofLogical, err := logicalPathInRun(artifact.RunDir, proofPath)
	if err != nil {
		result.add(options.File, "CLI composition proof path is invalid")
		return
	}
	artifact.References[options.File] = append(artifact.References[options.File], EvidenceRef{Path: proofLogical, SHA256: sha256File(proofPath)})
}

func ComposeContextBundle(options ComposeContextBundleOptions) (EvidenceRef, Result) {
	var result Result
	root := cleanWorktree(options.Root)
	runDir, err := resolveWorkflowRunDir(root, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return EvidenceRef{}, result
	}
	if !meaningful(options.WorkflowID) || !meaningful(options.ChangeSnapshot) || len(options.Inputs) == 0 {
		result.add("compose", "--workflow-id, --change-snapshot, and at least one --input are required")
		return EvidenceRef{}, result
	}
	refs := make([]EvidenceRef, 0, len(options.Inputs))
	seen := map[string]bool{}
	for _, logical := range options.Inputs {
		ref, refErr := registeredEvidenceRef(root, runDir, logical)
		if refErr != nil {
			result.add(logical, refErr.Error())
			continue
		}
		if seen[ref.Path] {
			result.add(logical, "duplicate context-bundle input")
			continue
		}
		seen[ref.Path] = true
		refs = append(refs, ref)
	}
	if !result.OK() {
		return EvidenceRef{}, result
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	outputPath, err := prospectiveRestrictedPath(runDir, options.Output)
	if err != nil {
		result.add(options.Output, "output must be under the active run restricted directory")
		return EvidenceRef{}, result
	}
	if _, err := os.Lstat(outputPath); err == nil || !os.IsNotExist(err) {
		result.add(options.Output, "generated context bundle already exists")
		return EvidenceRef{}, result
	}
	bundle := ContextBundle{BundleVersion: 1, WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot, Inputs: refs}
	if err := writeJSONExclusive(outputPath, bundle); err != nil {
		result.add(options.Output, err.Error())
		return EvidenceRef{}, result
	}
	logical, logicalErr := logicalPathInRun(runDir, outputPath)
	if logicalErr != nil {
		_ = os.Remove(outputPath)
		result.add(options.Output, logicalErr.Error())
		return EvidenceRef{}, result
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		_ = os.Remove(outputPath)
		result.add(options.Output, err.Error())
		return EvidenceRef{}, result
	}
	validateContextBundle(ArtifactOptions{Root: root, File: relativePath(root, outputPath)}, runDir, logical, data, options.WorkflowID, options.ChangeSnapshot, &result)
	if !result.OK() {
		_ = os.Remove(outputPath)
		return EvidenceRef{}, result
	}
	ref := EvidenceRef{Path: logical, SHA256: sha256File(outputPath)}
	if _, err := writeCompositionProof(root, runDir, "context-bundle.v1", options.WorkflowID, options.ChangeSnapshot, outputPath, []EvidenceRef{ref}); err != nil {
		_ = os.Remove(outputPath)
		result.add(options.Output, "cannot write context-bundle composition proof: "+err.Error())
		return EvidenceRef{}, result
	}
	return ref, result
}

type TransitionHopSource struct {
	FromSnapshot, ToSnapshot   string
	ChangedFiles, Verification string
}

type ComposeTransitionChainOptions struct {
	Root, RunDir, WorkflowID, TargetSnapshot, Output string
	Hops                                             []TransitionHopSource
}

func ComposeTransitionChain(options ComposeTransitionChainOptions) (EvidenceRef, Result) {
	var result Result
	root := cleanWorktree(options.Root)
	runDir, err := resolveWorkflowRunDir(root, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return EvidenceRef{}, result
	}
	if !meaningful(options.WorkflowID) || !meaningful(options.TargetSnapshot) || len(options.Hops) == 0 {
		result.add("compose", "--workflow-id, --target-snapshot, and at least one complete transition hop are required")
		return EvidenceRef{}, result
	}
	resolve := func(logical, composer, snapshot string) EvidenceRef {
		ref, refErr := registeredEvidenceRef(root, runDir, logical)
		if refErr != nil {
			result.add(logical, refErr.Error())
			return ref
		}
		if proofErr := validateStandaloneCompositionProof(root, runDir, composer, options.WorkflowID, snapshot, ref); proofErr != nil {
			result.add(logical, proofErr.Error())
		}
		return ref
	}
	hops := make([]TransitionHop, 0, len(options.Hops))
	seenEvidencePaths := map[string]bool{}
	for index, source := range options.Hops {
		if !meaningful(source.FromSnapshot) || !meaningful(source.ToSnapshot) || source.FromSnapshot == source.ToSnapshot {
			result.add("compose", "transition hop snapshots must be meaningful and different")
		}
		if index > 0 && options.Hops[index-1].ToSnapshot != source.FromSnapshot {
			result.add("compose", "transition hops must be contiguous")
		}
		for _, logical := range []string{source.ChangedFiles, source.Verification} {
			if seenEvidencePaths[logical] {
				result.add(logical, "transition hop evidence paths must be unique")
			}
			seenEvidencePaths[logical] = true
		}
		hops = append(hops, TransitionHop{
			FromSnapshot: source.FromSnapshot, ToSnapshot: source.ToSnapshot,
			ChangedFiles: resolve(source.ChangedFiles, "changed-files.v1", source.ToSnapshot),
			Verification: resolve(source.Verification, "verification.v1", source.ToSnapshot),
		})
	}
	if options.Hops[len(options.Hops)-1].ToSnapshot != options.TargetSnapshot {
		result.add("compose", "last transition hop must end at --target-snapshot")
	}
	if !result.OK() {
		return EvidenceRef{}, result
	}
	outputPath, err := prospectiveRestrictedPath(runDir, options.Output)
	if err != nil {
		result.add(options.Output, "output must be under the active run restricted directory")
		return EvidenceRef{}, result
	}
	if _, err := os.Lstat(outputPath); err == nil || !os.IsNotExist(err) {
		result.add(options.Output, "generated transition chain already exists")
		return EvidenceRef{}, result
	}
	chain := TransitionChain{SchemaVersion: 2, WorkflowID: options.WorkflowID, TargetSnapshot: options.TargetSnapshot, Hops: hops}
	if err := writeJSONExclusive(outputPath, chain); err != nil {
		result.add(options.Output, err.Error())
		return EvidenceRef{}, result
	}
	logical, _ := logicalPathInRun(runDir, outputPath)
	ref := EvidenceRef{Path: logical, SHA256: sha256File(outputPath)}
	if _, err := writeCompositionProof(root, runDir, "transition-chain.v1", options.WorkflowID, options.TargetSnapshot, outputPath, []EvidenceRef{ref}); err != nil {
		_ = os.Remove(outputPath)
		result.add(options.Output, "cannot write transition-chain composition proof: "+err.Error())
		return EvidenceRef{}, result
	}
	return ref, result
}

type QAExecutionCaseSubmission struct {
	Position     int
	Outcome      string
	Procedure    string
	Observation  string
	OracleResult string
}

type ComposeQAOwnedEvidenceOptions struct {
	Root, RunDir, WorkflowID, ChangeSnapshot string
	ApprovedCaseSet, OutputDir               string
	Cases                                    []QAExecutionCaseSubmission
}

type ComposeQAOwnedEvidenceOutput struct {
	Results EvidenceRef `json:"results"`
	Binding EvidenceRef `json:"binding"`
}

func ComposeQAOwnedEvidence(options ComposeQAOwnedEvidenceOptions) (ComposeQAOwnedEvidenceOutput, Result) {
	var result Result
	root := cleanWorktree(options.Root)
	runDir, err := resolveWorkflowRunDir(root, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	if !meaningful(options.WorkflowID) || !meaningful(options.ChangeSnapshot) {
		result.add("compose", "--workflow-id and --change-snapshot are required")
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	approvedRef, err := registeredEvidenceRef(root, runDir, options.ApprovedCaseSet)
	if err != nil {
		result.add(options.ApprovedCaseSet, err.Error())
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	approvedData, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(approvedRef.Path)))
	if err != nil {
		result.add(options.ApprovedCaseSet, err.Error())
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	approvedIDs := []string{}
	for _, match := range qaCaseIDPattern.FindAllStringSubmatch(string(approvedData), -1) {
		approvedIDs = append(approvedIDs, match[1])
	}
	if len(approvedIDs) == 0 || hasDuplicate(approvedIDs) {
		result.add(options.ApprovedCaseSet, "approved case set must contain unique case IDs")
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	if len(options.Cases) != len(approvedIDs) {
		result.add("case", "exactly one positioned QA submission is required for every approved case")
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	executions := make([]QAExecutionObservation, len(approvedIDs))
	caseResults := make([]QACaseObservation, len(approvedIDs))
	seenPositions := map[int]bool{}
	overall := "PASS"
	for _, submission := range options.Cases {
		if submission.Position < 1 || submission.Position > len(approvedIDs) || seenPositions[submission.Position] {
			result.add("case", "case positions must be unique and cover the approved case set from 1 through its case count")
			continue
		}
		seenPositions[submission.Position] = true
		values := []string{submission.Outcome, submission.Procedure, submission.Observation, submission.OracleResult}
		if (submission.Outcome != "PASS" && submission.Outcome != "FAIL") || !allMeaningful(values) || containsPending(values) {
			result.add("case", fmt.Sprintf("case position %d requires PASS/FAIL and non-empty resolved procedure, observation, and oracle result", submission.Position))
			continue
		}
		index := submission.Position - 1
		executionID := fmt.Sprintf("EXEC-%03d", submission.Position)
		executions[index] = QAExecutionObservation{ID: executionID, Outcome: submission.Outcome, Procedure: submission.Procedure, Result: submission.Observation}
		caseResults[index] = QACaseObservation{CaseID: approvedIDs[index], Status: submission.Outcome, Procedures: []string{executionID}, Oracle: submission.OracleResult}
		if submission.Outcome != "PASS" {
			overall = "FAIL"
		}
	}
	if len(seenPositions) != len(approvedIDs) {
		result.add("case", "case positions must cover every approved case exactly once")
	}
	if !result.OK() {
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	outputDir, err := prospectiveRestrictedPath(runDir, options.OutputDir)
	if err != nil {
		result.add(options.OutputDir, "output directory must be under the active run restricted directory")
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	resultsPath := filepath.Join(outputDir, "qa-results.json")
	bindingPath := filepath.Join(outputDir, "case-result-binding.json")
	for _, path := range []string{resultsPath, bindingPath} {
		if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
			result.add(relativePath(root, path), "generated QA-owned output already exists")
		}
	}
	if !result.OK() {
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	results := QAResultsArtifact{
		Owner: "QA", WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot,
		Stage: "Execution", Status: "COMPLETE", OverallOutcome: overall,
		Executions: executions, CaseResults: caseResults,
	}
	if err := writeJSONExclusive(resultsPath, results); err != nil {
		result.add(relativePath(root, resultsPath), err.Error())
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(resultsPath)
			_ = os.Remove(bindingPath)
			_ = os.Remove(outputDir)
		}
	}()
	resultsLogical, _ := logicalPathInRun(runDir, resultsPath)
	resultsRef := EvidenceRef{Path: resultsLogical, SHA256: sha256File(resultsPath)}
	bindings := make([]QACaseBinding, 0, len(caseResults))
	for index, observation := range caseResults {
		bindings = append(bindings, QACaseBinding{
			CaseID: observation.CaseID, ResultPointer: "/caseResults/" + strconv.Itoa(index), Status: observation.Status,
			ExecutionRefs: append([]string{}, observation.Procedures...), Procedures: append([]string{}, observation.Procedures...), Oracle: observation.Oracle,
		})
	}
	binding := QACaseBindingArtifact{
		WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot,
		ApprovedCaseSet: approvedRef, QAOwnedResults: resultsRef, Complete: true, Bindings: bindings,
	}
	if err := writeJSONExclusive(bindingPath, binding); err != nil {
		result.add(relativePath(root, bindingPath), err.Error())
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	bindingLogical, _ := logicalPathInRun(runDir, bindingPath)
	bindingRef := EvidenceRef{Path: bindingLogical, SHA256: sha256File(bindingPath)}
	if _, err := writeCompositionProof(root, runDir, "qa-owned-evidence.v1", options.WorkflowID, options.ChangeSnapshot, resultsPath, []EvidenceRef{resultsRef, bindingRef}); err != nil {
		result.add(relativePath(root, resultsPath), "cannot write QA-owned evidence composition proof: "+err.Error())
		return ComposeQAOwnedEvidenceOutput{}, result
	}
	cleanup = false
	return ComposeQAOwnedEvidenceOutput{Results: resultsRef, Binding: bindingRef}, result
}

func containsPending(values []string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), "PENDING") {
			return true
		}
	}
	return false
}
