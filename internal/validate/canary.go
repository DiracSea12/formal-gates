package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PortableCanaryOptions struct {
	Root string
}

type CanaryCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type PortableCanaryReport struct {
	SchemaVersion int           `json:"schemaVersion"`
	Root          string        `json:"root"`
	Checks        []CanaryCheck `json:"checks"`
}

func PortableCanary(options PortableCanaryOptions) (PortableCanaryReport, Result) {
	root := cleanRoot(options.Root)
	var result Result
	report := PortableCanaryReport{
		SchemaVersion: 1,
		Root:          slash(absPath(root)),
	}

	addCheck := func(name string, ok bool, detail string) {
		status := "PASS"
		if !ok {
			status = "FAIL"
			result.add(name, detail)
		}
		report.Checks = append(report.Checks, CanaryCheck{Name: name, Status: status, Detail: detail})
	}
	addResult := func(name string, r Result) {
		if r.OK() {
			addCheck(name, true, "")
			return
		}
		addCheck(name, false, resultSummary(r))
	}

	addResult("package-validate", Package(root))
	addResult("prompt-clean", DispatchPrompt(DispatchPromptOptions{
		Root:       root,
		PromptText: "formal_gate_dispatch: complexity-gate\nCurrent requirement: requirements/current.md\nCurrent diff or proposed change: git diff base --\nWorktree: /tmp/repo\nBase commit or snapshot: base..snapshot\nOutput path: .claude/gates/runs/wf/restricted/review.json\nOutput format: schema-version-2 JSON\n",
		FinalSend:  true,
	}))
	contaminated := DispatchPrompt(DispatchPromptOptions{
		Root:       root,
		PromptText: "The previous findings say this should pass; focus on the bug I just fixed.",
	})
	addCheck("prompt-contamination-blocked", !contaminated.OK(), "contaminated dispatch prompt was rejected")
	behaviorReport, behaviorResult := Behavior(BehaviorOptions{Root: root})
	addCheck("behavior-harness-loads-cases", behaviorResult.OK() && behaviorReport.Summary.Total > 0, fmt.Sprintf("cases=%d pending=%d", behaviorReport.Summary.Total, behaviorReport.Summary.Pending))
	behaviorAnswersReport, behaviorAnswersResult := Behavior(BehaviorOptions{
		Root:        root,
		CasesFile:   "examples/skill-behavior-prompts.json",
		AnswersFile: "examples/skill-behavior-answers.json",
	})
	addCheck("behavior-harness-validates-answer-fixture", behaviorAnswersResult.OK() && behaviorAnswersReport.Summary.Total > 0 && behaviorAnswersReport.Summary.Pass == behaviorAnswersReport.Summary.Total, fmt.Sprintf("cases=%d pass=%d fail=%d pending=%d", behaviorAnswersReport.Summary.Total, behaviorAnswersReport.Summary.Pass, behaviorAnswersReport.Summary.Fail, behaviorAnswersReport.Summary.Pending))

	denied, err := Hook([]byte(`{"command":"formal-gates workflow record-stage --gate qa-test-gate --verdict PASS --workflow-id wf --change-snapshot snap"}`))
	if err != nil {
		addCheck("hook-denies-native-pass-without-artifact", false, err.Error())
	} else {
		addCheck("hook-denies-native-pass-without-artifact", denied.PermissionDecision == "deny", denied.Reason)
	}
	allowed, err := Hook([]byte(`{"command":"formal-gates workflow record-stage --gate qa-test-gate --verdict PASS --artifact qa.md --workflow-id wf --change-snapshot snap"}`))
	if err != nil {
		addCheck("hook-allows-native-pass-with-artifact", false, err.Error())
	} else {
		addCheck("hook-allows-native-pass-with-artifact", allowed.PermissionDecision == "allow", allowed.Reason)
	}

	tempRoot, err := os.MkdirTemp("", "formal-gates-native-canary-")
	if err != nil {
		addCheck("temp-worktree", false, err.Error())
		return report, result
	}
	defer os.RemoveAll(tempRoot)

	worktree := filepath.Join(tempRoot, "worktree")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		addCheck("temp-worktree", false, err.Error())
		return report, result
	}
	if err := writeCanaryFile(filepath.Join(worktree, "src.txt"), "source\n"); err != nil {
		addCheck("temp-worktree", false, err.Error())
		return report, result
	}
	firstSnapshot, snapshotResult := WorkflowSnapshot(WorkflowSnapshotOptions{Worktree: worktree, VCS: "file-hash"})
	addResult("workflow-file-hash-snapshot", snapshotResult)
	secondSnapshot, secondSnapshotResult := WorkflowSnapshot(WorkflowSnapshotOptions{Worktree: worktree, VCS: "file-hash"})
	addCheck("workflow-file-hash-stable", secondSnapshotResult.OK() && firstSnapshot.ChangeSnapshot == secondSnapshot.ChangeSnapshot, "file-hash snapshots are stable across repeated runs")

	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	restrictedRel := filepath.ToSlash(filepath.Join(runRel, "restricted"))
	if err := writeCanaryGateArtifact(worktree, "qa-test-gate", "Execution", "wf", "snap"); err != nil {
		addCheck("workflow-record-fixture", false, err.Error())
	} else {
		addResult("workflow-record-qa-execution", WorkflowRecordStage(WorkflowRecordStageOptions{
			Worktree:       worktree,
			Gate:           "qa-test-gate",
			Verdict:        "PASS",
			Mode:           "formal",
			Stage:          "Execution",
			Artifact:       filepath.ToSlash(filepath.Join(restrictedRel, "qa-test-gate.md")),
			Actor:          "native-canary",
			WorkflowID:     "wf",
			ChangeSnapshot: "snap",
		}))
		addResult("workflow-admission-after-qa", WorkflowVerifyAdmission(WorkflowVerifyAdmissionOptions{
			Worktree:       worktree,
			Gate:           "complexity-gate",
			WorkflowID:     "wf",
			ChangeSnapshot: "snap",
		}))
		for _, gate := range []string{"complexity-gate", "architecture-health-gate", "code-quality-gate"} {
			if err := writeCanaryGateArtifact(worktree, gate, "", "wf", "snap"); err != nil {
				addCheck("workflow-"+gate+"-fixture", false, err.Error())
				continue
			}
			addResult("workflow-record-"+gate, WorkflowRecordStage(WorkflowRecordStageOptions{
				Worktree:       worktree,
				Gate:           gate,
				Verdict:        "PASS",
				Artifact:       filepath.ToSlash(filepath.Join(restrictedRel, gate+".md")),
				Actor:          "native-canary",
				WorkflowID:     "wf",
				ChangeSnapshot: "snap",
			}))
		}
	}
	attemptRel := filepath.ToSlash(filepath.Join(restrictedRel, "attempt.json"))
	attemptPath := filepath.Join(worktree, filepath.FromSlash(attemptRel))
	if err := writeCanaryFile(attemptPath, `{"ok":true}`+"\n"); err != nil {
		addCheck("final-verification-fixture", false, err.Error())
	} else {
		attemptsJSON := `[{"status":"PASS","accepted":true,"artifact":"` + attemptRel + `","artifactHash":"` + sha256File(attemptPath) + `"}]`
		_, finalResult := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
			Worktree:       worktree,
			RunDir:         runRel,
			AttemptsJSON:   attemptsJSON,
			OutputArtifact: filepath.ToSlash(filepath.Join(restrictedRel, "final-verification.json")),
			WorkflowID:     "wf",
			ChangeSnapshot: "snap",
		})
		addResult("workflow-final-verification", finalResult)
		_, finalQAResult := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
			Worktree:        worktree,
			RunDir:          runRel,
			AttemptsJSON:    attemptsJSON,
			OutputArtifact:  filepath.ToSlash(filepath.Join(restrictedRel, "final-verification-record-final-qa.json")),
			FinalQAArtifact: filepath.ToSlash(filepath.Join(restrictedRel, "final-execution.md")),
			RecordFinalQA:   true,
			Actor:           "gate-workflow",
			WorkflowID:      "wf",
			ChangeSnapshot:  "snap",
		})
		addResult("workflow-final-execution-record", finalQAResult)
	}

	receiptWorktree := filepath.Join(tempRoot, "receipt-worktree")
	receiptArtifact := filepath.ToSlash(filepath.Join(restrictedRel, "complexity.json"))
	if err := os.MkdirAll(filepath.Dir(resolvePath(receiptWorktree, receiptArtifact)), 0o700); err != nil {
		addCheck("receipt-worktree", false, err.Error())
	} else {
		patterns, patternErr := os.ReadFile(filepath.Join(root, "hooks", "pollution-patterns.json"))
		if patternErr != nil {
			addCheck("receipt-prompt-patterns", false, patternErr.Error())
		} else if err := writeCanaryFile(filepath.Join(receiptWorktree, "hooks", "pollution-patterns.json"), string(patterns)); err != nil {
			addCheck("receipt-prompt-patterns", false, err.Error())
		}
		receiptInput := filepath.Join(receiptWorktree, filepath.FromSlash(restrictedRel), "receipt-context.txt")
		receiptBundle := filepath.ToSlash(filepath.Join("restricted", "receipt-context-bundle.json"))
		receiptPrompt := filepath.ToSlash(filepath.Join("restricted", "receipt-final-send.txt"))
		if err := writeCanaryFile(receiptInput, "context\n"); err != nil {
			addCheck("receipt-context-bundle", false, err.Error())
		} else if err := writeCanaryJSON(filepath.Join(receiptWorktree, filepath.FromSlash(runRel), filepath.FromSlash(receiptBundle)), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{{Path: filepath.ToSlash(filepath.Join("restricted", "receipt-context.txt")), SHA256: sha256File(receiptInput)}}}); err != nil {
			addCheck("receipt-context-bundle", false, err.Error())
		}
		if err := writeCanaryPreparedPrompt(receiptWorktree, "wf", "snap", "complexity-gate", "", receiptArtifact, receiptBundle, receiptPrompt); err != nil {
			addCheck("receipt-final-send-prompt", false, err.Error())
		}
		registration, registerResult := ReceiptRegisterDispatch(ReceiptRegisterOptions{
			Worktree:       receiptWorktree,
			Provider:       "codex",
			WorkflowID:     "wf",
			ChangeSnapshot: "snap",
			Gate:           "complexity-gate",
			Artifact:       receiptArtifact,
			ContextBundle:  receiptBundle,
			Prompt:         receiptPrompt,
		})
		addResult("receipt-register", registerResult)
		if registerResult.OK() {
			payload := fmt.Sprintf(`{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":%q,"dispatchRegistrationArtifact":%q}`, registration.DispatchID, registration.DispatchRegistrationArtifact)
			_, startResult := ReceiptCapture(ReceiptCaptureOptions{Worktree: receiptWorktree, Provider: "codex", Event: "SubagentStart", Payload: []byte(payload)})
			addResult("receipt-capture-start", startResult)
			if err := writeCanaryComplexityArtifact(receiptWorktree, "wf", "snap"); err != nil {
				addCheck("receipt-fixture", false, err.Error())
			}
			_, stopResult := ReceiptCapture(ReceiptCaptureOptions{Worktree: receiptWorktree, Provider: "codex", Event: "SubagentStop", Payload: []byte(payload)})
			addResult("receipt-capture-stop", stopResult)
			receipt, finalizeResult := ReceiptFinalize(ReceiptFinalizeOptions{
				Worktree:   receiptWorktree,
				Provider:   "codex",
				WorkflowID: "wf",
				Gate:       "complexity-gate",
				Artifact:   receiptArtifact,
			})
			addResult("receipt-finalize", finalizeResult)
			if finalizeResult.OK() {
				addResult("receipt-validate", ReceiptValidate(ReceiptValidateOptions{
					Worktree:       receiptWorktree,
					Receipt:        receipt.ReceiptArtifact,
					Artifact:       receiptArtifact,
					Gate:           "complexity-gate",
					WorkflowID:     "wf",
					ChangeSnapshot: "snap",
				}))
			}
		}
	}

	addInstallChecks(root, tempRoot, addCheck)
	preflight, preflightResult := ReceiptPreflight(ReceiptPreflightOptions{Host: "codex", Worktree: worktree})
	addResult("receipt-preflight-diagnostic", preflightResult)
	addCheck("receipt-preflight-unproven-not-pass", preflight.Status == "HOST_AUTO_CAPTURE_UNPROVEN", preflight.Status)
	complexityReport, complexityResult := Complexity(ComplexityOptions{Worktree: worktree, VCS: "none", TaskType: "bugfix"})
	addResult("complexity-manual-review-diagnostic", complexityResult)
	addCheck("complexity-no-vcs-is-review", complexityReport.Status == "REVIEW", complexityReport.Status)

	return report, result
}

func PortableCanaryJSON(report PortableCanaryReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func addInstallChecks(root, tempRoot string, addCheck func(string, bool, string)) {
	for _, tc := range []struct {
		name string
		host string
	}{
		{name: "install-claude-codex-native-runtime", host: "both"},
		{name: "install-cursor-native-runtime", host: "cursor"},
	} {
		project := filepath.Join(tempRoot, tc.name)
		if err := os.MkdirAll(project, 0o700); err != nil {
			addCheck(tc.name, false, err.Error())
			continue
		}
		report, err := Install(InstallOptions{
			Source:         root,
			Host:           tc.host,
			Scope:          "project",
			Project:        project,
			Force:          true,
			ConfigureHooks: true,
		})
		if err != nil {
			addCheck(tc.name, false, err.Error())
			continue
		}
		if detail := installedScriptRuntimeDetail(report); detail != "" {
			addCheck(tc.name, false, detail)
			continue
		}
		addCheck(tc.name, true, "installed runtime has no script files and hook config uses native commands")
	}
}

func installedScriptRuntimeDetail(report InstallReport) string {
	for _, target := range report.Targets {
		var found []string
		err := filepath.WalkDir(target.TargetPath, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if isScriptRuntimeExtension(entry.Name()) {
				found = append(found, slash(path))
			}
			return nil
		})
		if err != nil {
			return err.Error()
		}
		if len(found) > 0 {
			return "installed script runtime files: " + strings.Join(found, ", ")
		}
		if strings.TrimSpace(target.HookConfig) != "" {
			text, err := readText(target.HookConfig)
			if err != nil {
				return err.Error()
			}
			lower := strings.ToLower(text)
			for _, marker := range []string{".ps1", "powershell", "pwsh", "python", "node", "bash"} {
				if strings.Contains(lower, marker) {
					return "hook config contains script runtime marker " + marker
				}
			}
		}
	}
	return ""
}

func isScriptRuntimeExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ps1", ".psm1", ".psd1", ".py", ".pyc", ".pyo", ".sh", ".bash", ".bat", ".cmd", ".js", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

func resultSummary(result Result) string {
	messages := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		messages = append(messages, failure.Path+": "+failure.Message)
	}
	return strings.Join(messages, "; ")
}

func writeCanaryGateArtifact(root, gate, stage, workflowID, snapshot string) error {
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
	restrictedDir := filepath.Join(runDir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	prefix := strings.TrimSuffix(gate, "-gate")
	bundleName := prefix + "-bundle.json"
	for name, text := range map[string]string{prefix + "-input.txt": "input", prefix + "-changed.txt": "changed", prefix + "-verification.txt": "verified"} {
		if err := writeCanaryFile(filepath.Join(restrictedDir, name), text); err != nil {
			return err
		}
	}
	ref := func(name string) EvidenceRef {
		return EvidenceRef{Path: logical(name), SHA256: sha256File(filepath.Join(restrictedDir, name))}
	}
	if gate == "qa-test-gate" {
		approved, designReview, err := writeCanaryDesignReviewClosure(root, runDir, workflowID, snapshot)
		if err != nil {
			return err
		}
		if err := writeCanaryJSON(filepath.Join(restrictedDir, "qa-results.json"), map[string]any{
			"owner": "QA", "workflowId": workflowID, "changeSnapshot": snapshot, "stage": "Execution", "status": "COMPLETE", "overallOutcome": "PASS",
			"executions":  []any{map[string]any{"id": "E-001", "outcome": "PASS", "procedure": "Run the approved case", "result": "The case passed"}},
			"caseResults": []any{map[string]any{"caseId": "P1-001", "status": "PASS", "procedures": []string{"E-001"}, "oracle": "The approved behavior is observed"}},
		}); err != nil {
			return err
		}
		results := ref("qa-results.json")
		if err := writeCanaryJSON(filepath.Join(restrictedDir, "qa-case-binding.json"), map[string]any{
			"workflowId": workflowID, "changeSnapshot": snapshot, "approvedCaseSet": approved, "qaOwnedResults": results, "complete": true,
			"bindings": []any{map[string]any{"caseId": "P1-001", "resultPointer": "/caseResults/0", "status": "PASS", "executionRefs": []string{"E-001"}, "procedures": []string{"E-001"}, "oracle": "The approved behavior is observed"}},
		}); err != nil {
			return err
		}
		payload, _ := json.Marshal(QAExecutionPayload{ApprovedCaseSet: approved, DesignReview: designReview, QAOwnedResults: results, CaseResultBinding: ref("qa-case-binding.json"), ChangedFiles: ref(prefix + "-changed.txt"), Verification: ref(prefix + "-verification.txt")})
		return writeCanaryJSON(filepath.Join(restrictedDir, gate+".md"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_EXECUTION", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Verdict: "PASS", Payload: payload})
	}
	if err := writeCanaryJSON(filepath.Join(restrictedDir, bundleName), ContextBundle{BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: snapshot, Inputs: []EvidenceRef{ref(prefix + "-input.txt")}}); err != nil {
		return err
	}
	policyID := map[string]string{"qa-test-gate": "qa.execution.v2", "complexity-gate": "complexity.post-development.v2", "architecture-health-gate": "architecture.post-development.v2", "code-quality-gate": "code-quality.post-development.v2"}[gate]
	policy, ok := policyByID(policyID)
	if !ok {
		return fmt.Errorf("missing canary policy %s", policyID)
	}
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	statsName := prefix + "-statistics.json"
	if err := writeCanaryJSON(filepath.Join(restrictedDir, statsName), ComplexityReport{Status: "PASS", VCS: "none", Worktree: root, TaskType: "refactor", BudgetSource: "none", BudgetOverrides: ComplexityBudgetOverride{}, Summary: ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []ComplexityFileChange{}}); err != nil {
		return err
	}
	for _, id := range policy.RequiredCheckIDs {
		check := ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
		if id == "complexity.statistics" {
			check.EvidenceRefs = []EvidenceRef{ref(statsName)}
		}
		checks = append(checks, check)
	}
	changed, verification := ref(prefix+"-changed.txt"), ref(prefix+"-verification.txt")
	payload := ReviewerPayload{ContextBundle: ref(bundleName), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: &changed, Verification: &verification}
	payloadData, _ := json.Marshal(payload)
	artifact := relativePath(root, filepath.Join(restrictedDir, gate+".md"))
	promptName := prefix + "-final-send.txt"
	if err := writeCanaryPreparedPrompt(root, workflowID, snapshot, gate, stage, artifact, logical(bundleName), logical(promptName)); err != nil {
		return err
	}
	registration, rr := ReceiptRegisterDispatch(ReceiptRegisterOptions{Worktree: root, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Artifact: artifact, ContextBundle: logical(bundleName), Prompt: logical(promptName)})
	if !rr.OK() {
		return fmt.Errorf("%s", resultSummary(rr))
	}
	raw, _ := json.Marshal(map[string]any{"workflowId": workflowID, "changeSnapshot": snapshot, "gate": gate, "stage": stage, "subagentId": prefix + "-agent", "dispatchId": registration.DispatchID, "dispatchRegistrationArtifact": registration.DispatchRegistrationArtifact})
	if _, r := ReceiptCapture(ReceiptCaptureOptions{Worktree: root, Provider: "codex", Event: "SubagentStart", Payload: raw}); !r.OK() {
		return fmt.Errorf("%s", resultSummary(r))
	}
	if err := writeCanaryJSON(filepath.Join(restrictedDir, gate+".md"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Verdict: "PASS", Payload: payloadData}); err != nil {
		return err
	}
	if _, r := ReceiptCapture(ReceiptCaptureOptions{Worktree: root, Provider: "codex", Event: "SubagentStop", Payload: raw}); !r.OK() {
		return fmt.Errorf("%s", resultSummary(r))
	}
	if _, r := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: root, Provider: "codex", WorkflowID: workflowID, Gate: gate, Stage: stage, Artifact: artifact}); !r.OK() {
		return fmt.Errorf("%s", resultSummary(r))
	}
	return nil
}

func writeCanaryDesignReviewClosure(root, runDir, workflowID, snapshot string) (EvidenceRef, EvidenceRef, error) {
	patterns := filepath.Join(root, "hooks", "pollution-patterns.json")
	if !isFile(patterns) {
		if err := writeCanaryJSON(patterns, map[string]any{"english": map[string]any{"patternGroups": []any{}}, "chinese": map[string]any{"termGroups": []any{}}}); err != nil {
			return EvidenceRef{}, EvidenceRef{}, err
		}
	}
	inputName, bundleName := "design-input.txt", "design-bundle.json"
	restrictedDir := filepath.Join(runDir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	if err := writeCanaryFile(filepath.Join(restrictedDir, inputName), "requirements\n"); err != nil {
		return EvidenceRef{}, EvidenceRef{}, err
	}
	input := EvidenceRef{Path: logical(inputName), SHA256: sha256File(filepath.Join(restrictedDir, inputName))}
	if err := writeCanaryJSON(filepath.Join(restrictedDir, bundleName), ContextBundle{BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: snapshot, Inputs: []EvidenceRef{input}}); err != nil {
		return EvidenceRef{}, EvidenceRef{}, err
	}
	caseArtifact := relativePath(root, filepath.Join(restrictedDir, "approved-cases.md"))
	designReceipt, err := writeCanaryReceiptBoundOutput(root, workflowID, snapshot, "Design", caseArtifact, logical(bundleName), "design-agent", func() error {
		return writeCanaryFile(filepath.Join(restrictedDir, "approved-cases.md"), "# Cases\n\nCase ID: P1-001\n\nOracle: approved behavior\n")
	})
	if err != nil {
		return EvidenceRef{}, EvidenceRef{}, err
	}
	approved := EvidenceRef{Path: logical("approved-cases.md"), SHA256: sha256File(filepath.Join(restrictedDir, "approved-cases.md"))}
	policy, _ := policyByID("qa.design-review.v2")
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		check := ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
		if id == "qa.design.case-set-binding" {
			check.EvidenceRefs = []EvidenceRef{approved, designReceipt}
		}
		checks = append(checks, check)
	}
	payload, _ := json.Marshal(ReviewerPayload{ContextBundle: EvidenceRef{Path: logical(bundleName), SHA256: sha256File(filepath.Join(restrictedDir, bundleName))}, ReviewPolicyID: policy.ID, Checks: checks})
	reviewArtifact := relativePath(root, filepath.Join(restrictedDir, "design-review.json"))
	_, err = writeCanaryReceiptBoundOutput(root, workflowID, snapshot, "Design Review", reviewArtifact, logical(bundleName), "design-review-agent", func() error {
		return writeCanaryJSON(filepath.Join(restrictedDir, "design-review.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_REVIEW", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: "Design Review", Verdict: "PASS", Payload: payload})
	})
	if err != nil {
		return EvidenceRef{}, EvidenceRef{}, err
	}
	data, err := os.ReadFile(filepath.Join(restrictedDir, "design-review.json"))
	if err != nil {
		return EvidenceRef{}, EvidenceRef{}, err
	}
	options := ArtifactOptions{Root: root, RunDir: runDir, File: reviewArtifact, Gate: "qa-test-gate", Stage: "Design Review", Flow: "pre-development", WorkflowID: workflowID, ChangeSnapshot: snapshot}
	var result Result
	decoded := decodeArtifact(options, data, &result)
	if !result.OK() {
		return EvidenceRef{}, EvidenceRef{}, fmt.Errorf("%s", resultSummary(result))
	}
	reviewReceipt, err := matchingReceiptRef(options, decoded)
	if err != nil {
		return EvidenceRef{}, EvidenceRef{}, err
	}
	closure, err := buildClosure(options, decoded, &reviewReceipt)
	return approved, closure, err
}

func writeCanaryReceiptBoundOutput(root, workflowID, snapshot, stage, artifact, bundle, subagentID string, writeOutput func() error) (EvidenceRef, error) {
	options := ReceiptRegisterOptions{Worktree: root, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: stage, Artifact: artifact, ContextBundle: bundle}
	if reviewJudgmentLifecycle(options.Gate, options.Stage) {
		name := "design-review-final-send.txt"
		if err := writeCanaryPreparedPrompt(root, workflowID, snapshot, options.Gate, stage, artifact, bundle, filepath.ToSlash(filepath.Join("restricted", name))); err != nil {
			return EvidenceRef{}, err
		}
		options.Prompt = filepath.ToSlash(filepath.Join("restricted", name))
	}
	registration, result := ReceiptRegisterDispatch(options)
	if !result.OK() {
		return EvidenceRef{}, fmt.Errorf("%s", resultSummary(result))
	}
	raw, _ := json.Marshal(map[string]any{"workflowId": workflowID, "changeSnapshot": snapshot, "gate": "qa-test-gate", "stage": stage, "subagentId": subagentID, "dispatchId": registration.DispatchID, "dispatchRegistrationArtifact": registration.DispatchRegistrationArtifact})
	if _, result = ReceiptCapture(ReceiptCaptureOptions{Worktree: root, Provider: "codex", Event: "SubagentStart", Payload: raw}); !result.OK() {
		return EvidenceRef{}, fmt.Errorf("%s", resultSummary(result))
	}
	if err := writeOutput(); err != nil {
		return EvidenceRef{}, err
	}
	if _, result = ReceiptCapture(ReceiptCaptureOptions{Worktree: root, Provider: "codex", Event: "SubagentStop", Payload: raw}); !result.OK() {
		return EvidenceRef{}, fmt.Errorf("%s", resultSummary(result))
	}
	output, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: root, Provider: "codex", WorkflowID: workflowID, Gate: "qa-test-gate", Stage: stage, Artifact: artifact})
	if !result.OK() {
		return EvidenceRef{}, fmt.Errorf("%s", resultSummary(result))
	}
	runDir, _ := resolveWorkflowRunDir(root, workflowID, "")
	logical, err := logicalPathInRun(runDir, resolvePath(root, output.ReceiptArtifact))
	if err != nil {
		return EvidenceRef{}, err
	}
	return EvidenceRef{Path: logical, SHA256: output.ReceiptSha256}, nil
}

func writeCanaryPreparedPrompt(worktree, workflowID, snapshot, gate, stage, artifact, bundle, prompt string) error {
	runDir, err := resolveWorkflowRunDir(worktree, workflowID, "")
	if err != nil {
		return err
	}
	role, policies := dispatchOutputContracts(gate, stage)
	if role == "" || len(policies) == 0 {
		return fmt.Errorf("missing dispatch output contract for %s / %s", gate, stage)
	}
	template := filepath.ToSlash(filepath.Join("restricted", strings.TrimSuffix(filepath.Base(prompt), filepath.Ext(prompt))+"-template.txt"))
	text := "formal_gate_dispatch: " + expectedDispatchRole(gate, stage) + "\nCurrent requirement: requirements/current.md\nCurrent diff or proposed change: git diff base --\nWorktree: " + worktree + "\nBase commit or snapshot: " + snapshot + "\nOutput path: " + artifact + "\nOutput format: closed schema-version-2 " + role + " JSON for " + policies[0] + "\n"
	if err := writeCanaryFile(filepath.Join(runDir, filepath.FromSlash(template)), text); err != nil {
		return err
	}
	_, result := PrepareDispatchPrompt(PrepareDispatchPromptOptions{Root: worktree, DispatchFile: filepath.Join(runDir, filepath.FromSlash(template)), OutputFile: filepath.Join(runDir, filepath.FromSlash(prompt)), Bindings: []DispatchPromptBinding{{Name: "contextBundle", Path: filepath.Join(runDir, filepath.FromSlash(bundle))}}})
	if !result.OK() {
		return fmt.Errorf("%s", resultSummary(result))
	}
	return nil
}

func writeCanaryComplexityArtifact(root, workflowID, snapshot string) error {
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
	restricted := filepath.Join(runDir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	ref := func(name string) EvidenceRef {
		return EvidenceRef{Path: logical(name), SHA256: sha256File(filepath.Join(restricted, name))}
	}
	if err := writeCanaryFile(filepath.Join(restricted, "receipt-changed.txt"), "changed\n"); err != nil {
		return err
	}
	if err := writeCanaryFile(filepath.Join(restricted, "receipt-verification.txt"), "verified\n"); err != nil {
		return err
	}
	if err := writeCanaryJSON(filepath.Join(restricted, "receipt-statistics.json"), ComplexityReport{Status: "PASS", VCS: "none", Worktree: root, TaskType: "refactor", BudgetSource: "none", BudgetOverrides: ComplexityBudgetOverride{}, Summary: ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []ComplexityFileChange{}}); err != nil {
		return err
	}
	policy, _ := policyByID("complexity.post-development.v2")
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		check := ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
		if id == "complexity.statistics" {
			check.EvidenceRefs = []EvidenceRef{ref("receipt-statistics.json")}
		}
		checks = append(checks, check)
	}
	context := EvidenceRef{Path: logical("receipt-context-bundle.json"), SHA256: sha256File(filepath.Join(restricted, "receipt-context-bundle.json"))}
	changed, verification := ref("receipt-changed.txt"), ref("receipt-verification.txt")
	payload, _ := json.Marshal(ReviewerPayload{ContextBundle: context, ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: &changed, Verification: &verification})
	return writeCanaryJSON(filepath.Join(runDir, "restricted", "complexity.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "COMPLEXITY_REVIEW", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "complexity-gate", Stage: "", Verdict: "PASS", Payload: payload})
}

func writeCanaryJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeCanaryFile(path, string(data)+"\n")
}

func writeCanaryFile(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o600)
}
