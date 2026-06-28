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
		PromptText: "Review this package from the supplied files and report only evidence-backed findings.",
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

	if err := writeCanaryGateArtifact(worktree, "qa-test-gate", "Execution", "wf", "snap"); err != nil {
		addCheck("workflow-record-fixture", false, err.Error())
	} else {
		addResult("workflow-record-qa-execution", WorkflowRecordStage(WorkflowRecordStageOptions{
			Worktree:       worktree,
			Gate:           "qa-test-gate",
			Verdict:        "PASS",
			Mode:           "formal",
			Stage:          "Execution",
			Artifact:       "qa-test-gate.md",
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
	}
	attemptPath := filepath.Join(worktree, ".claude", "gates", "artifacts", "attempt.json")
	if err := writeCanaryFile(attemptPath, `{"ok":true}`+"\n"); err != nil {
		addCheck("final-verification-fixture", false, err.Error())
	} else {
		_, finalResult := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
			Worktree:       worktree,
			AttemptsJSON:   `[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/attempt.json"}]`,
			OutputArtifact: ".claude/gates/artifacts/final-verification.json",
			WorkflowID:     "wf",
			ChangeSnapshot: "snap",
		})
		addResult("workflow-final-verification", finalResult)
		if err := writeCanaryGateArtifact(worktree, "qa-test-gate", "FinalExecution", "wf", "snap"); err != nil {
			addCheck("final-qa-record-fixture", false, err.Error())
		} else {
			_, finalQAResult := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
				Worktree:        worktree,
				AttemptsJSON:    `[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/attempt.json"}]`,
				OutputArtifact:  ".claude/gates/artifacts/final-verification-record-final-qa.json",
				FinalQAArtifact: "qa-test-gate.md",
				RecordFinalQA:   true,
				Actor:           "native-canary",
				WorkflowID:      "wf",
				ChangeSnapshot:  "snap",
			})
			addResult("workflow-final-execution-record", finalQAResult)
		}
	}

	receiptWorktree := filepath.Join(tempRoot, "receipt-worktree")
	if err := os.MkdirAll(receiptWorktree, 0o700); err != nil {
		addCheck("receipt-worktree", false, err.Error())
	} else if err := writeCanaryComplexityArtifact(receiptWorktree, "wf", "snap"); err != nil {
		addCheck("receipt-fixture", false, err.Error())
	} else {
		registration, registerResult := ReceiptRegisterDispatch(ReceiptRegisterOptions{
			Worktree:   receiptWorktree,
			Provider:   "codex",
			WorkflowID: "wf",
			Gate:       "complexity-gate",
			Artifact:   "complexity.md",
		})
		addResult("receipt-register", registerResult)
		if registerResult.OK() {
			payload := fmt.Sprintf(`{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":%q,"dispatchRegistrationArtifact":%q}`, registration.DispatchID, registration.DispatchRegistrationArtifact)
			_, startResult := ReceiptCapture(ReceiptCaptureOptions{Worktree: receiptWorktree, Provider: "codex", Event: "SubagentStart", Payload: []byte(payload)})
			addResult("receipt-capture-start", startResult)
			_, stopResult := ReceiptCapture(ReceiptCaptureOptions{Worktree: receiptWorktree, Provider: "codex", Event: "SubagentStop", Payload: []byte(payload)})
			addResult("receipt-capture-stop", stopResult)
			receipt, finalizeResult := ReceiptFinalize(ReceiptFinalizeOptions{
				Worktree:   receiptWorktree,
				Provider:   "codex",
				WorkflowID: "wf",
				Gate:       "complexity-gate",
				Artifact:   "complexity.md",
			})
			addResult("receipt-finalize", finalizeResult)
			if finalizeResult.OK() {
				addResult("receipt-validate", ReceiptValidate(ReceiptValidateOptions{
					Worktree:       receiptWorktree,
					Receipt:        receipt.ReceiptArtifact,
					Artifact:       "complexity.md",
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
	addCheck("receipt-preflight-unproven-not-pass", preflight.Status == "UNSUPPORTED_HOST_RECEIPT", preflight.Status)
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
	return writeCanaryFile(filepath.Join(root, gate+".md"), canaryGateArtifactText(gate, stage, workflowID, snapshot))
}

func writeCanaryComplexityArtifact(root, workflowID, snapshot string) error {
	bundle := filepath.Join(root, "bundle.md")
	prompt := filepath.Join(root, "prompt.md")
	if err := writeCanaryFile(bundle, "bundle\n"); err != nil {
		return err
	}
	if err := writeCanaryFile(prompt, "formal_gate_dispatch: true\n"); err != nil {
		return err
	}
	return writeCanaryFile(filepath.Join(root, "complexity.md"), canaryComplexityArtifactText(
		workflowID,
		snapshot,
		"bundle.md sha256="+sha256File(bundle),
		"prompt.md sha256="+sha256File(prompt),
		"",
	))
}

func canaryGateArtifactText(gate, stage, workflowID, snapshot string) string {
	lines := []string{
		"Review mode: ZERO_CONTEXT_FORMAL",
		"Prompt contamination check: PASS",
		"Semantic anti-anchor check: PASS",
		"Prompt source: " + expectedPromptSource(gate),
		"Zero-context reviewer: YES",
		"Independent agent: YES",
		"Context bundle: bundle.md sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"Dispatch prompt artifact: prompt.md sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"No-anchor prompt: YES",
	}
	if gate == "qa-test-gate" {
		lines = append(lines,
			"Approved case set: cases.md",
			"QA-owned evidence: qa-evidence.md",
			"Case-to-artifact binding: bound",
		)
	}
	if gate == "complexity-gate" {
		lines = append(lines, canaryComplexitySpecificFields()...)
	}
	lines = append(lines,
		"gate_route:",
		"  workflow_id: "+workflowID,
		"  change_snapshot: "+snapshot,
		"  next_action: proceed",
		"  rework_owner: none",
		"  rerun_from: none",
	)
	if stage == "FinalExecution" {
		lines[len(lines)-3] = "  next_action: seal"
	}
	return strings.Join(lines, "\n") + "\n"
}

func canaryComplexityArtifactText(workflowID, snapshot, bundleRef, dispatchRef, receiptRef string) string {
	lines := []string{
		"Review mode: ZERO_CONTEXT_FORMAL",
		"Prompt contamination check: PASS",
		"Semantic anti-anchor check: PASS",
		"Prompt source: agents/complexity-gate.md",
		"Zero-context reviewer: YES",
		"Independent agent: YES",
		"Reviewer proof receipt: " + receiptRef,
		"Context bundle: " + bundleRef,
		"Dispatch prompt artifact: " + dispatchRef,
		"No-anchor prompt: YES",
	}
	lines = append(lines, canaryComplexitySpecificFields()...)
	lines = append(lines,
		"gate_route:",
		"  workflow_id: "+workflowID,
		"  change_snapshot: "+snapshot,
		"  next_action: proceed",
		"  rework_owner: none",
		"  rerun_from: none",
	)
	return strings.Join(lines, "\n") + "\n"
}

func canaryComplexitySpecificFields() []string {
	return []string{
		"Script result: PASS",
		"Diff shape judgment: focused",
		"Impact surface health: bounded",
		"Public/config surface: none",
		"New concepts: none",
		"Shrink opportunities: none",
		"Decision evidence: diff",
	}
}

func writeCanaryFile(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o600)
}
