package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestComposeRequirementsGeneratesStaticContractAndRefs(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-compose-requirements")
	options := validRequirementsComposeOptions(root, runDir, "wf-compose-requirements", "snapshot-1", "restricted/generated/requirements")
	output, result := ComposeRequirements(options)
	if !result.OK() {
		t.Fatalf("compose requirements failed: %+v", result.Failures)
	}

	alignmentPath := filepath.Join(runDir, filepath.FromSlash(output.Alignment.Path))
	decisionPath := filepath.Join(runDir, filepath.FromSlash(output.Decision.Path))
	requirementsPath := filepath.Join(runDir, filepath.FromSlash(output.Requirements.Path))
	if output.Alignment.SHA256 != sha256File(alignmentPath) || output.Decision.SHA256 != sha256File(decisionPath) || output.Requirements.SHA256 != sha256File(requirementsPath) {
		t.Fatal("compose output did not report the generated evidence hashes")
	}

	var alignment AlignmentArtifact
	readComposeJSON(t, alignmentPath, &alignment)
	wantItems := projectAlignmentSubmissions(options.Alignments, options.AlignmentIDs)
	if alignment.SchemaVersion != 2 || alignment.WorkflowID != "wf-compose-requirements" || alignment.ChangeSnapshot != "snapshot-1" || !reflect.DeepEqual(alignment.Items, wantItems) {
		t.Fatalf("alignment static contract was not generated correctly: %+v", alignment)
	}

	var decision RequirementsDecision
	readComposeJSON(t, decisionPath, &decision)
	if decision.SchemaVersion != 2 || decision.WorkflowID != "wf-compose-requirements" || decision.ChangeSnapshot != "snapshot-1" || decision.DecisionType != "USER_CONFIRMATION" || !decision.UserConfirmation || decision.ApprovalScope != "requirements-clarification-gate" || decision.Alignment != output.Alignment || !reflect.DeepEqual(decision.ApprovedAlignmentIDs, []string{"RQ-064", "RQ-065"}) || len(decision.ApprovedDroppedIDs) != 0 {
		t.Fatalf("decision static contract was not generated correctly: %+v", decision)
	}

	var envelope FormalGateEvidence
	readComposeJSON(t, requirementsPath, &envelope)
	if envelope.SchemaVersion != 2 || envelope.ArtifactRole != "REQUIREMENTS_PASS" || envelope.WorkflowID != "wf-compose-requirements" || envelope.ChangeSnapshot != "snapshot-1" || envelope.Gate != "requirements-clarification-gate" || envelope.Stage != "" || envelope.Verdict != "PASS" {
		t.Fatalf("requirements envelope was not generated correctly: %+v", envelope)
	}
	var payload RequirementsPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RequirementSource != "openspec/changes/phase-2-5" || !reflect.DeepEqual(payload.CoveredTargets, []string{"openspec/changes/phase-2-5/design.md"}) || payload.Alignment != output.Alignment || payload.Decision != output.Decision || payload.TotalAlignmentItems != len(options.Alignments) || payload.PreviousAlignment != nil || len(payload.OpenQuestionIDs) != 0 || payload.DownstreamPermission != "READY_TO_DRAFT" {
		t.Fatalf("requirements static payload was not generated correctly: %+v", payload)
	}
	if len(payload.DimensionCoverage) != 13 {
		t.Fatalf("got %d generated dimensions, want 13", len(payload.DimensionCoverage))
	}
	for index, dimension := range payload.DimensionCoverage {
		if dimension.ID != dimensionIDs[index] || dimension.Status != options.Dimensions[index].Status || !reflect.DeepEqual(dimension.AlignmentIDs, []string{"RQ-064", "RQ-065"}) || dimension.Message != options.Dimensions[index].Message {
			t.Fatalf("dimension %d was not generated from the fixed catalog and semantic judgment: %+v", index, dimension)
		}
	}
	if validation := Artifact(ArtifactOptions{Root: root, RunDir: runDir, File: relativePath(root, requirementsPath), Gate: "requirements-clarification-gate", WorkflowID: "wf-compose-requirements", ChangeSnapshot: "snapshot-1"}); !validation.OK() {
		t.Fatalf("generated requirements artifact is invalid: %+v", validation.Failures)
	}
}

func TestComposeRequirementsGeneratesPreviousAlignmentBindings(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-compose-continuity")
	options := validRequirementsComposeOptions(root, runDir, "wf-compose-continuity", "snapshot-1", "restricted/generated/continuity")
	previous := AlignmentArtifact{
		SchemaVersion: 2, WorkflowID: "wf-compose-continuity", ChangeSnapshot: "snapshot-0",
		Items: append([]AlignmentItem{{
			ID: "RQ-063", RequirementOrQuestion: "Old requirement", Source: "user", WhyItMatters: "old scope",
			Status: "WITHDRAWN", UserAnswer: "withdrawn", DownstreamEffect: "remove", DocumentImpact: "openspec/changes/phase-2-5/design.md", EvidenceNeeded: "alignment continuity",
		}}, projectAlignmentSubmissions(options.Alignments, options.AlignmentIDs)...),
	}
	previousPath := filepath.Join(runDir, "restricted", "previous-alignment.json")
	writeJSONTest(t, previousPath, previous)

	options.PreviousAlignment = "restricted/previous-alignment.json"
	options.ApprovedDroppedIDs = []string{"RQ-063"}
	output, result := ComposeRequirements(options)
	if !result.OK() {
		t.Fatalf("composition with previous alignment failed: %+v", result.Failures)
	}
	var decision RequirementsDecision
	readComposeJSON(t, filepath.Join(runDir, filepath.FromSlash(output.Decision.Path)), &decision)
	if !reflect.DeepEqual(decision.ApprovedAlignmentIDs, []string{"RQ-064", "RQ-065"}) || !reflect.DeepEqual(decision.ApprovedDroppedIDs, []string{"RQ-063"}) {
		t.Fatalf("decision bindings were not mechanically derived: %+v", decision)
	}
	var envelope FormalGateEvidence
	readComposeJSON(t, filepath.Join(runDir, filepath.FromSlash(output.Requirements.Path)), &envelope)
	var payload RequirementsPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	wantPrevious := EvidenceRef{Path: "restricted/previous-alignment.json", SHA256: sha256File(previousPath)}
	if payload.PreviousAlignment == nil || *payload.PreviousAlignment != wantPrevious || !reflect.DeepEqual(payload.DroppedQuestionIDs, []string{"RQ-063"}) || !payload.DroppedQuestionApproval {
		t.Fatalf("continuity bindings were not mechanically derived: %+v", payload)
	}
}

func TestComposeRequirementsRejectsUnapprovedPreviousAlignmentRemoval(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-compose-continuity-missing-approval")
	options := validRequirementsComposeOptions(root, runDir, "wf-compose-continuity-missing-approval", "snapshot-1", "restricted/generated/continuity")
	previous := AlignmentArtifact{
		SchemaVersion: 2, WorkflowID: options.WorkflowID, ChangeSnapshot: "snapshot-0",
		Items: append([]AlignmentItem{{ID: "RQ-063", RequirementOrQuestion: "Old requirement", Source: "user", WhyItMatters: "old scope", Status: "WITHDRAWN", UserAnswer: "withdrawn", DownstreamEffect: "remove", DocumentImpact: "openspec/changes/phase-2-5/design.md", EvidenceNeeded: "alignment continuity"}}, projectAlignmentSubmissions(options.Alignments, options.AlignmentIDs)...),
	}
	previousPath := filepath.Join(runDir, "restricted", "previous-alignment.json")
	writeJSONTest(t, previousPath, previous)
	options.PreviousAlignment = "restricted/previous-alignment.json"
	if _, result := ComposeRequirements(options); result.OK() {
		t.Fatal("missing explicit dropped approval was accepted")
	}
	assertNoComposeOutputs(t, filepath.Join(runDir, "restricted", "generated", "continuity"))
}

func TestComposeRequirementsRejectsMissingAndDuplicatePositionsWithoutOutput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ComposeRequirementsOptions)
	}{
		{name: "missing-alignment", mutate: func(options *ComposeRequirementsOptions) { options.Alignments = options.Alignments[:1] }},
		{name: "duplicate-alignment", mutate: func(options *ComposeRequirementsOptions) { options.Alignments[1].Position = 1 }},
		{name: "missing-dimension", mutate: func(options *ComposeRequirementsOptions) { options.Dimensions = options.Dimensions[:12] }},
		{name: "duplicate-dimension", mutate: func(options *ComposeRequirementsOptions) { options.Dimensions[12].Position = 12 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, runDir := composeTestRun(t, "wf-compose-positions-"+test.name)
			outputDir := "restricted/generated/" + test.name
			options := validRequirementsComposeOptions(root, runDir, "wf-compose-positions-"+test.name, "snapshot-1", outputDir)
			test.mutate(&options)
			if _, result := ComposeRequirements(options); result.OK() {
				t.Fatal("invalid positioned requirements submission was accepted")
			}
			assertNoComposeOutputs(t, filepath.Join(runDir, filepath.FromSlash(outputDir)))
			if entries, err := os.ReadDir(receiptProofDir(root, runDir, "compositions")); err == nil && len(entries) != 0 {
				t.Fatalf("rejected requirements composition left proofs: %v", entries)
			} else if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		})
	}
}

func TestComposeRequirementsDoesNotOverwriteAndCleansFailedComposition(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-compose-atomic")
	options := validRequirementsComposeOptions(root, runDir, "wf-compose-atomic", "snapshot-1", "restricted/generated/complete")
	output, result := ComposeRequirements(options)
	if !result.OK() {
		t.Fatalf("initial composition failed: %+v", result.Failures)
	}
	outputDir := filepath.Join(runDir, "restricted", "generated", "complete")
	before := map[string][]byte{}
	for _, name := range []string{"alignment.json", "decision.json", "requirements.json"} {
		data, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = data
	}
	proofPath, err := compositionProofPath(root, runDir, "requirements.v1", filepath.Join(runDir, filepath.FromSlash(output.Requirements.Path)))
	if err != nil {
		t.Fatal(err)
	}
	proofBefore, err := os.ReadFile(proofPath)
	if err != nil {
		t.Fatal(err)
	}
	options.UserOriginal = "different confirmation"
	if _, result := ComposeRequirements(options); result.OK() {
		t.Fatal("composition overwrote an existing generated target")
	}
	for name, want := range before {
		got, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("existing %s changed after rejected composition", name)
		}
	}
	proofAfter, err := os.ReadFile(proofPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(proofAfter, proofBefore) {
		t.Fatal("existing requirements composition proof changed after rejected overwrite")
	}

	invalidOptions := options
	invalidOptions.OutputDir = "restricted/generated/failed"
	invalidOptions.Dimensions = append([]RequirementsDimensionSubmission{}, options.Dimensions...)
	invalidOptions.Dimensions[7].Status = "TYPO"
	if _, result := ComposeRequirements(invalidOptions); result.OK() {
		t.Fatal("invalid semantic judgment produced a requirements artifact")
	}
	assertNoComposeOutputs(t, filepath.Join(runDir, "restricted", "generated", "failed"))
}

func TestComposeQAExecutionGeneratesEnvelopeAndEvidenceRefs(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-compose-qa")
	_, inputs := qaExecutionPolicyFixture(t, runDir, "wf-compose-qa", "snapshot-qa")
	qaOwned, qaOwnedResult := ComposeQAOwnedEvidence(ComposeQAOwnedEvidenceOptions{
		Root: root, RunDir: runDir, WorkflowID: "wf-compose-qa", ChangeSnapshot: "snapshot-qa",
		ApprovedCaseSet: inputs.ApprovedCaseSet.Path, OutputDir: "restricted/generated/qa-owned",
		Cases: []QAExecutionCaseSubmission{{Position: 1, Outcome: "PASS", Procedure: "Run the approved case", Observation: "The case passed", OracleResult: "The approved behavior is observed"}},
	})
	if !qaOwnedResult.OK() {
		t.Fatalf("compose QA-owned fixture failed: %+v", qaOwnedResult.Failures)
	}
	inputs.QAOwnedResults = qaOwned.Results
	inputs.CaseResultBinding = qaOwned.Binding
	options := ComposeQAExecutionOptions{
		Root: root, RunDir: runDir, WorkflowID: "wf-compose-qa", ChangeSnapshot: "snapshot-qa",
		Output: "restricted/generated/qa-execution.json", ApprovedCaseSet: inputs.ApprovedCaseSet.Path,
		DesignReview: inputs.DesignReview.Path, QAOwnedResults: inputs.QAOwnedResults.Path,
		CaseResultBinding: inputs.CaseResultBinding.Path, ChangedFiles: inputs.ChangedFiles.Path, Verification: inputs.Verification.Path,
	}
	ref, result := ComposeQAExecution(options)
	if !result.OK() {
		t.Fatalf("compose QA Execution failed: %+v", result.Failures)
	}
	path := filepath.Join(runDir, filepath.FromSlash(ref.Path))
	if ref.SHA256 != sha256File(path) {
		t.Fatal("QA Execution output did not report its generated hash")
	}
	var envelope FormalGateEvidence
	readComposeJSON(t, path, &envelope)
	if envelope.SchemaVersion != 2 || envelope.ArtifactRole != "QA_EXECUTION" || envelope.WorkflowID != "wf-compose-qa" || envelope.ChangeSnapshot != "snapshot-qa" || envelope.Gate != "qa-test-gate" || envelope.Stage != "Execution" || envelope.Verdict != "PASS" {
		t.Fatalf("QA Execution envelope was not generated correctly: %+v", envelope)
	}
	var payload QAExecutionPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload, inputs) {
		t.Fatalf("QA Execution evidence refs were not generated from current files:\ngot  %+v\nwant %+v", payload, inputs)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, result := ComposeQAExecution(options); result.OK() {
		t.Fatal("composition overwrote an existing QA Execution artifact")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("existing QA Execution artifact changed after rejected composition")
	}

	options.Output = "restricted/generated/missing-evidence.json"
	options.Verification = "restricted/not-present.json"
	if _, result := ComposeQAExecution(options); result.OK() {
		t.Fatal("composition accepted a missing evidence reference")
	}
	if _, err := os.Lstat(filepath.Join(runDir, "restricted", "generated", "missing-evidence.json")); !os.IsNotExist(err) {
		t.Fatalf("failed QA composition left an output: %v", err)
	}
}

func TestComposeQAExecutionRequiresMatchingQAOwnedEvidencePairProof(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-compose-qa-pair")
	_, inputs := qaExecutionPolicyFixture(t, runDir, "wf-compose-qa-pair", "snapshot-qa")
	composePair := func(outputDir string) ComposeQAOwnedEvidenceOutput {
		output, result := ComposeQAOwnedEvidence(ComposeQAOwnedEvidenceOptions{
			Root: root, RunDir: runDir, WorkflowID: "wf-compose-qa-pair", ChangeSnapshot: "snapshot-qa",
			ApprovedCaseSet: inputs.ApprovedCaseSet.Path, OutputDir: outputDir,
			Cases: []QAExecutionCaseSubmission{{Position: 1, Outcome: "PASS", Procedure: "Run the approved case", Observation: "The case passed", OracleResult: "The approved behavior is observed"}},
		})
		if !result.OK() {
			t.Fatalf("compose QA-owned pair failed: %+v", result.Failures)
		}
		return output
	}
	first := composePair("restricted/generated/qa-owned-first")
	second := composePair("restricted/generated/qa-owned-second")
	options := ComposeQAExecutionOptions{
		Root: root, RunDir: runDir, WorkflowID: "wf-compose-qa-pair", ChangeSnapshot: "snapshot-qa",
		Output: "restricted/generated/mixed-qa-execution.json", ApprovedCaseSet: inputs.ApprovedCaseSet.Path,
		DesignReview: inputs.DesignReview.Path, QAOwnedResults: first.Results.Path,
		CaseResultBinding: second.Binding.Path, ChangedFiles: inputs.ChangedFiles.Path, Verification: inputs.Verification.Path,
	}
	if _, result := ComposeQAExecution(options); result.OK() {
		t.Fatal("QA Execution composition accepted results and binding from different generated pairs")
	}
	if _, err := os.Lstat(filepath.Join(runDir, filepath.FromSlash(options.Output))); !os.IsNotExist(err) {
		t.Fatalf("mixed QA-owned pair left a QA Execution output: %v", err)
	}

	firstResultsPath := filepath.Join(runDir, filepath.FromSlash(first.Results.Path))
	proofPath, err := compositionProofPath(root, runDir, "qa-owned-evidence.v1", firstResultsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(proofPath); err != nil {
		t.Fatal(err)
	}
	options.Output = "restricted/generated/unproven-qa-execution.json"
	options.CaseResultBinding = first.Binding.Path
	if _, result := ComposeQAExecution(options); result.OK() {
		t.Fatal("QA Execution composition accepted a QA-owned pair without its CLI composition proof")
	}
	if _, err := os.Lstat(filepath.Join(runDir, filepath.FromSlash(options.Output))); !os.IsNotExist(err) {
		t.Fatalf("unproven QA-owned pair left a QA Execution output: %v", err)
	}
}

func TestComposeQAExecutionRequiresRecordedRerunForPriorExecutionPass(t *testing.T) {
	const workflowID, sourceSnapshot, targetSnapshot = "wf-rerun-qa", "source", "target"
	tests := []struct {
		name           string
		setup          func(*testing.T, string)
		wantOK         bool
		wantTransition string
		staleOutput    bool
	}{
		{name: "first run", wantOK: true},
		{name: "prior Design Review", setup: func(t *testing.T, root string) {
			recordDesignReviewPassFixture(t, root, workflowID, sourceSnapshot)
		}, wantOK: true},
		{name: "missing transition", setup: func(t *testing.T, root string) {
			recordPostDevelopmentPassFixture(t, root, workflowID, sourceSnapshot, "qa-test-gate")
		}, wantTransition: "required for new snapshot", staleOutput: true},
		{name: "RERUN_REQUIRED transition", setup: func(t *testing.T, root string) {
			recordPostDevelopmentPassFixture(t, root, workflowID, sourceSnapshot, "qa-test-gate")
			if result := recordCarryDecisionTransitionFixture(t, root, workflowID, sourceSnapshot, targetSnapshot, "qa-test-gate", "RERUN_REQUIRED"); !result.OK() {
				t.Fatalf("cannot record QA RERUN_REQUIRED transition: %#v", result.Failures)
			}
		}, wantOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.setup != nil {
				test.setup(t, root)
			}
			runDir, err := resolveWorkflowRunDir(root, workflowID, "")
			if err != nil {
				t.Fatal(err)
			}
			inputs := qaExecutionComposeInputsFixture(t, root, runDir, workflowID, targetSnapshot, "target")
			qaOwned, qaOwnedResult := ComposeQAOwnedEvidence(ComposeQAOwnedEvidenceOptions{
				Root: root, RunDir: runDir, WorkflowID: workflowID, ChangeSnapshot: targetSnapshot,
				ApprovedCaseSet: inputs.ApprovedCaseSet.Path, OutputDir: "restricted/generated/target-qa-owned",
				Cases: []QAExecutionCaseSubmission{{Position: 1, Outcome: "PASS", Procedure: "Run the approved case", Observation: "The case passed", OracleResult: "The approved behavior is observed"}},
			})
			if !qaOwnedResult.OK() {
				t.Fatalf("cannot compose target QA-owned evidence: %#v", qaOwnedResult.Failures)
			}
			options := ComposeQAExecutionOptions{
				Root: root, RunDir: runDir, WorkflowID: workflowID, ChangeSnapshot: targetSnapshot,
				Output: "restricted/generated/target-qa-execution.json", ApprovedCaseSet: inputs.ApprovedCaseSet.Path,
				DesignReview: inputs.DesignReview.Path, QAOwnedResults: qaOwned.Results.Path, CaseResultBinding: qaOwned.Binding.Path,
				ChangedFiles: inputs.ChangedFiles.Path, Verification: inputs.Verification.Path,
			}
			outputPath := filepath.Join(runDir, filepath.FromSlash(options.Output))
			proofPath := filepath.Join(receiptProofDir(root, runDir, "compositions"), sha256Bytes([]byte("qa-execution.v1\n"+options.Output))+".json")
			var before []byte
			if test.staleOutput {
				before = []byte("existing target bytes\n")
				if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(outputPath, before, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, result := ComposeQAExecution(options)
			if result.OK() != test.wantOK {
				t.Fatalf("composition OK=%v, want %v: %#v", result.OK(), test.wantOK, result.Failures)
			}
			if test.wantTransition != "" && !strings.Contains(resultSummary(result), test.wantTransition) {
				t.Fatalf("missing transition failure %q: %#v", test.wantTransition, result.Failures)
			}
			if test.wantOK {
				if !isFile(outputPath) || !isFile(proofPath) {
					t.Fatal("successful QA composition did not create output and proof")
				}
				return
			}
			after, err := os.ReadFile(outputPath)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("rejected QA composition changed existing output: err=%v got=%q want=%q", err, after, before)
			}
			if _, err := os.Lstat(proofPath); !os.IsNotExist(err) {
				t.Fatalf("rejected QA composition left proof: %v", err)
			}
		})
	}
}

func qaExecutionComposeInputsFixture(t *testing.T, root, runDir, workflowID, snapshot, prefix string) QAExecutionPayload {
	t.Helper()
	restricted := filepath.Join(runDir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	changedName, verificationName := prefix+"-qa-changed.txt", prefix+"-qa-verification.txt"
	for name, content := range map[string]string{changedName: "changed\n", verificationName: "verified\n"} {
		mustWrite(t, filepath.Join(restricted, name), content)
		composer := map[string]string{changedName: "changed-files.v1", verificationName: "verification.v1"}[name]
		path := filepath.Join(restricted, name)
		ref := testRef(t, runDir, logical(name))
		if _, err := writeCompositionProof(root, runDir, composer, workflowID, snapshot, path, []EvidenceRef{ref}); err != nil {
			t.Fatal(err)
		}
	}
	casesName := prefix + "-approved-cases.md"
	mustWrite(t, filepath.Join(restricted, casesName), "# Cases\n\nStatus: APPROVED_FOR_EXECUTION\n\n## Login flow\n\nCase ID: P1-001\n")
	approved := testRef(t, runDir, logical(casesName))

	designInputName, designBundleName := prefix+"-design-input.txt", prefix+"-design-bundle.json"
	mustWrite(t, filepath.Join(restricted, designInputName), "requirements\n")
	writeJSONTest(t, filepath.Join(restricted, designBundleName), ContextBundle{BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: snapshot + "-design", Inputs: []EvidenceRef{testRef(t, runDir, logical(designInputName))}})
	designReceipt := writeProofReceiptFixture(t, runDir, workflowID, snapshot+"-design", "qa-test-gate", "Design", approved.Path, prefix+"-design")
	policy, _ := policyByID("qa.design-review.v2")
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		check := ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
		if id == "qa.design.case-set-binding" {
			check.EvidenceRefs = []EvidenceRef{approved, designReceipt}
		}
		checks = append(checks, check)
	}
	designArtifactName := prefix + "-design-review.json"
	designPayload := ReviewerPayload{ContextBundle: testRef(t, runDir, logical(designBundleName)), ReviewPolicyID: policy.ID, Checks: checks}
	writeEnvelopeTest(t, filepath.Join(restricted, designArtifactName), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_REVIEW", WorkflowID: workflowID, ChangeSnapshot: snapshot + "-design", Gate: "qa-test-gate", Stage: "Design Review", Verdict: "PASS"}, designPayload)
	reviewReceipt := writeProofReceiptFixture(t, runDir, workflowID, snapshot+"-design", "qa-test-gate", "Design Review", logical(designArtifactName), prefix+"-design-review")
	designArtifactPath := filepath.Join(restricted, designArtifactName)
	designData, err := os.ReadFile(designArtifactPath)
	if err != nil {
		t.Fatal(err)
	}
	options := ArtifactOptions{Root: root, RunDir: runDir, File: filepath.ToSlash(filepath.Join(".gates", "runs", workflowID, "restricted", designArtifactName)), Gate: "qa-test-gate", Stage: "Design Review", Flow: "pre-development", WorkflowID: workflowID, ChangeSnapshot: snapshot + "-design"}
	var result Result
	decoded := decodeArtifact(options, designData, &result)
	if !result.OK() {
		t.Fatalf("target Design Review fixture is invalid: %#v", result.Failures)
	}
	designClosure, err := buildClosure(options, decoded, &reviewReceipt)
	if err != nil {
		t.Fatal(err)
	}
	return QAExecutionPayload{
		ApprovedCaseSet: approved, DesignReview: designClosure,
		ChangedFiles: testRef(t, runDir, logical(changedName)), Verification: testRef(t, runDir, logical(verificationName)),
	}
}

func composeTestRun(t *testing.T, workflowID string) (string, string) {
	t.Helper()
	root := t.TempDir()
	runDir := filepath.Join(root, ".gates", "runs", workflowID)
	if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root, runDir
}

func validRequirementsComposeOptions(root, runDir, workflowID, snapshot, outputDir string) ComposeRequirementsOptions {
	dimensions := make([]RequirementsDimensionSubmission, 13)
	for index := range dimensions {
		dimensions[index] = RequirementsDimensionSubmission{Position: index + 1, Status: "COVERED", AlignmentItemPositions: []int{1, 2}, Message: "confirmed coverage"}
	}
	return ComposeRequirementsOptions{
		Root: root, RunDir: runDir, WorkflowID: workflowID, ChangeSnapshot: snapshot, OutputDir: outputDir,
		RequirementSource: "openspec/changes/phase-2-5", AlignmentIDs: []string{"RQ-064", "RQ-065"}, CoveredTargets: []string{"openspec/changes/phase-2-5/design.md"},
		Alignments: []RequirementsAlignmentSubmission{
			{Position: 1, RequirementOrQuestion: "Generate static formal fields", Source: "user", WhyItMatters: "prevents omissions", Status: "CONFIRMED", UserAnswer: "approved", DownstreamEffect: "compose with CLI", DocumentImpact: "openspec/changes/phase-2-5/design.md", EvidenceNeeded: "direct composition tests"},
			{Position: 2, RequirementOrQuestion: "Keep semantic input scalar", Source: "user", WhyItMatters: "prevents formatting failures", Status: "CONFIRMED", UserAnswer: "approved", DownstreamEffect: "submit positions", DocumentImpact: "openspec/changes/phase-2-5/tasks.md", EvidenceNeeded: "public CLI tests"},
		},
		UserOriginal: "all static content must be script generated", OpenBlockers: []string{}, CoverageScan: "PASS",
		ScopePreservation: PassOrNA{Status: "PASS", Message: "scope preserved"}, TaskProof: PassOrNA{Status: "PASS", Message: "task proof present"}, Dimensions: dimensions,
	}
}

func projectAlignmentSubmissions(submissions []RequirementsAlignmentSubmission, ids []string) []AlignmentItem {
	items := make([]AlignmentItem, len(submissions))
	for _, submission := range submissions {
		index := submission.Position - 1
		items[index] = AlignmentItem{
			ID: ids[index], RequirementOrQuestion: submission.RequirementOrQuestion, Source: submission.Source,
			WhyItMatters: submission.WhyItMatters, Status: submission.Status, UserAnswer: submission.UserAnswer,
			DownstreamEffect: submission.DownstreamEffect, DocumentImpact: submission.DocumentImpact, EvidenceNeeded: submission.EvidenceNeeded,
		}
	}
	return items
}

func readComposeJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func assertNoComposeOutputs(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"alignment.json", "decision.json", "requirements.json"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("failed composition left %s: %v", name, err)
		}
	}
}
