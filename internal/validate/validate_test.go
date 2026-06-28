package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootForCanaryTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("go.mod not found")
		}
		dir = next
	}
}

func TestArtifactRequirementsClarification(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "requirements.md")
	text := `Requirement source: user brief
Alignment table artifact: .claude/gates/artifacts/alignment.md
Total alignment items: 1
Open question IDs: none
User confirmation: YES
Dimension coverage:
  DIM-01 Goal: covered | RQ-001
Decision record: .claude/gates/artifacts/requirements-user-decision.md
Covered formal targets: openspec/changes/example/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf
  change_snapshot: snap
  next_action: proceed
  rework_owner: none
  rerun_from: none
`
	if err := os.WriteFile(artifact, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	result := Artifact(ArtifactOptions{
		Root:           dir,
		File:           "requirements.md",
		Gate:           "requirements-clarification-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !result.OK() {
		t.Fatalf("expected valid artifact, got %#v", result.Failures)
	}
}

func TestArtifactRejectsPlaceholders(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "complexity.md")
	text := `Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/complexity-gate.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: <reviewer>
Context bundle: bundle.md sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
Dispatch prompt artifact: prompt.md sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
No-anchor prompt: YES
Script result: PASS
Diff shape judgment: focused
Impact surface health: bounded
Public/config surface: none
New concepts: none
Shrink opportunities: none
Decision evidence: diff
gate_route:
  workflow_id: wf
  change_snapshot: snap
  next_action: proceed
`
	if err := os.WriteFile(artifact, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	result := Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() {
		t.Fatal("expected placeholder reviewer to fail")
	}
}

func TestArtifactAllowsLegacyReviewerAgentIDWhenNotUsedAsProof(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle.md")
	prompt := filepath.Join(dir, "prompt.md")
	mustWrite(t, bundle, "bundle")
	mustWrite(t, prompt, "formal_gate_dispatch: true\n")
	artifact := filepath.Join(dir, "complexity.md")
	text := complexityArtifactText(
		"wf",
		"snap",
		"bundle.md sha256="+sha256FileForTest(t, bundle),
		"prompt.md sha256="+sha256FileForTest(t, prompt),
		"",
	)
	text = strings.Replace(text, "Independent agent: YES\n", "Independent agent: YES\nReviewer agent id: legacy-agent\n", 1)
	mustWrite(t, artifact, text)

	result := Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if !result.OK() {
		t.Fatalf("expected legacy reviewer id metadata without proof claim to pass, got %#v", result.Failures)
	}
}

func TestComplexityArtifactRequiresBudgetStatus(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle.md")
	prompt := filepath.Join(dir, "prompt.md")
	mustWrite(t, bundle, "bundle")
	mustWrite(t, prompt, "formal_gate_dispatch: true\n")
	artifact := filepath.Join(dir, "complexity.md")
	text := complexityArtifactText(
		"wf",
		"snap",
		"bundle.md sha256="+sha256FileForTest(t, bundle),
		"prompt.md sha256="+sha256FileForTest(t, prompt),
		"",
	)
	text = strings.Replace(text, "Budget/expansion status: within contract; no expansion requested\n", "", 1)
	mustWrite(t, artifact, text)

	result := Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() {
		t.Fatal("expected missing budget status to fail")
	}
}

func TestComplexityArtifactRequiresApprovalEvidenceForApprovedExpansion(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle.md")
	prompt := filepath.Join(dir, "prompt.md")
	mustWrite(t, bundle, "bundle")
	mustWrite(t, prompt, "formal_gate_dispatch: true\n")
	artifact := filepath.Join(dir, "complexity.md")
	text := complexityArtifactText(
		"wf",
		"snap",
		"bundle.md sha256="+sha256FileForTest(t, bundle),
		"prompt.md sha256="+sha256FileForTest(t, prompt),
		"",
	)
	text = strings.Replace(text, "Budget/expansion status: within contract; no expansion requested\n", "Budget/expansion status: APPROVE expansion to max-net 800\n", 1)
	mustWrite(t, artifact, text)

	result := Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() {
		t.Fatal("expected approved expansion without evidence to fail")
	}

	approval := filepath.Join(dir, "approval.md")
	mustWrite(t, approval, "Anti-Complexity Review\nVerdict: APPROVE_SMALLER\n")
	text += "Budget expansion approval: approval.md sha256=" + sha256FileForTest(t, approval) + "\n"
	mustWrite(t, artifact, text)

	result = Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if !result.OK() {
		t.Fatalf("expected approved expansion with hashed evidence to pass, got %#v", result.Failures)
	}
}

func TestArtifactAcceptsCRLFGateRoute(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle.md")
	prompt := filepath.Join(dir, "prompt.md")
	mustWrite(t, bundle, "bundle")
	mustWrite(t, prompt, "formal_gate_dispatch: true\n")
	artifact := filepath.Join(dir, "complexity.md")
	text := complexityArtifactText(
		"wf",
		"snap",
		"bundle.md sha256="+sha256FileForTest(t, bundle),
		"prompt.md sha256="+sha256FileForTest(t, prompt),
		"",
	)
	text = strings.ReplaceAll(text, "\n", "\r\n")
	mustWrite(t, artifact, text)

	result := Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if !result.OK() {
		t.Fatalf("expected CRLF artifact to pass, got %#v", result.Failures)
	}
}

func TestArtifactRejectsLegacySelfReportedProof(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle.md")
	prompt := filepath.Join(dir, "prompt.md")
	mustWrite(t, bundle, "bundle")
	mustWrite(t, prompt, "formal_gate_dispatch: true\n")
	artifact := filepath.Join(dir, "complexity.md")
	text := complexityArtifactText(
		"wf",
		"snap",
		"bundle.md sha256="+sha256FileForTest(t, bundle),
		"prompt.md sha256="+sha256FileForTest(t, prompt),
		"",
	)
	text = strings.Replace(text, "Independent agent: YES\n", "Independent agent: YES\nReviewer proof: reviewer-agent-id-only\n", 1)
	mustWrite(t, artifact, text)

	result := Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() {
		t.Fatal("expected legacy self-reported proof field to fail")
	}
}

func TestArtifactValidatesReviewerProofReceiptWhenPresent(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "complexity.md")
	bundle := filepath.Join(dir, "bundle.md")
	prompt := filepath.Join(dir, "prompt.md")
	dispatch := filepath.Join(dir, ".claude", "gates", "proofs", "dispatch", "dispatch.json")
	start := filepath.Join(dir, "start.json")
	stop := filepath.Join(dir, "stop.json")
	receipt := filepath.Join(dir, ".claude", "gates", "proofs", "receipt.json")
	mustWrite(t, bundle, "bundle")
	mustWrite(t, prompt, "formal_gate_dispatch: true\n")
	mustWrite(t, start, `{"workflowId":"wf","gate":"complexity-gate","stage":"","normalizedEvent":"subagent_start","subagentId":"subagent-1","dispatchId":"dispatch-1","dispatchRegistrationArtifact":".claude/gates/proofs/dispatch/dispatch.json"}`)
	mustWrite(t, stop, `{"workflowId":"wf","gate":"complexity-gate","stage":"","normalizedEvent":"subagent_stop","subagentId":"subagent-1","dispatchId":"dispatch-1","dispatchRegistrationArtifact":".claude/gates/proofs/dispatch/dispatch.json"}`)

	textWithoutReceipt := complexityArtifactText(
		"wf",
		"snap",
		"bundle.md sha256="+sha256FileForTest(t, bundle),
		"prompt.md sha256="+sha256FileForTest(t, prompt),
		"",
	)
	mustWrite(t, artifact, textWithoutReceipt)

	result := Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if !result.OK() {
		t.Fatalf("expected ordinary artifact without receipt to pass, got %#v", result.Failures)
	}

	canonical := canonicalReviewArtifactHash(textWithoutReceipt)
	dispatchText := `{
  "proofVersion": 1,
  "dispatchId": "dispatch-1",
  "provider": "codex",
  "workflowId": "wf",
  "gate": "complexity-gate",
  "stage": "",
  "reviewArtifact": "complexity.md",
  "receiptArtifact": ".claude/gates/proofs/receipt.json",
  "status": "open"
}`
	mustWrite(t, dispatch, dispatchText)
	receiptText := `{
  "proofVersion": 1,
  "provider": "codex",
  "workflowId": "wf",
  "gate": "complexity-gate",
  "stage": "",
  "worktree": "` + filepath.ToSlash(dir) + `",
  "dispatchId": "dispatch-1",
  "dispatchRegistrationArtifact": ".claude/gates/proofs/dispatch/dispatch.json",
  "dispatchRegistrationSha256": "` + sha256FileForTest(t, dispatch) + `",
  "normalizedEvents": ["subagent_start", "subagent_stop"],
  "rawEventNames": ["SubagentStart", "SubagentStop"],
  "startEventArtifact": "start.json",
  "startEventSha256": "` + sha256FileForTest(t, start) + `",
  "stopEventArtifact": "stop.json",
  "stopEventSha256": "` + sha256FileForTest(t, stop) + `",
  "reviewArtifact": "complexity.md",
  "reviewArtifactCanonicalSha256": "` + canonical + `",
  "status": "completed"
}`
	mustWrite(t, receipt, receiptText)
	withReceipt := complexityArtifactText(
		"wf",
		"snap",
		"bundle.md sha256="+sha256FileForTest(t, bundle),
		"prompt.md sha256="+sha256FileForTest(t, prompt),
		".claude/gates/proofs/receipt.json sha256="+sha256FileForTest(t, receipt),
	)
	mustWrite(t, artifact, withReceipt)

	result = Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if !result.OK() {
		t.Fatalf("expected receipt-bound artifact to pass, got %#v", result.Failures)
	}

	mustWrite(t, artifact, withReceipt+"\nDecision evidence: modified after receipt\n")
	result = Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() {
		t.Fatal("expected modified artifact to fail receipt binding")
	}
}

func TestArtifactRejectsReviewerProofReceiptTampering(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*receiptFixture)
	}{
		{name: "proof-version", mutate: func(f *receiptFixture) { f.receiptProofVersion = 2 }},
		{name: "provider-unsupported", mutate: func(f *receiptFixture) { f.receiptProvider = "unsupported" }},
		{name: "receipt-hash-wrong", mutate: func(f *receiptFixture) { f.receiptHashOverride = strings.Repeat("0", 64) }},
		{name: "dispatch-hash-wrong", mutate: func(f *receiptFixture) { f.dispatchHashOverride = strings.Repeat("0", 64) }},
		{name: "review-artifact-rebound", mutate: func(f *receiptFixture) { f.receiptReviewArtifact = "other.md" }},
		{name: "workflow-wrong", mutate: func(f *receiptFixture) { f.receiptWorkflow = "wf-other" }},
		{name: "gate-wrong", mutate: func(f *receiptFixture) { f.receiptGate = "qa-test-gate" }},
		{name: "stage-wrong", mutate: func(f *receiptFixture) { f.receiptStage = "Execution" }},
		{name: "missing-start", mutate: func(f *receiptFixture) { f.normalizedEvents = []string{"subagent_stop"} }},
		{name: "missing-stop", mutate: func(f *receiptFixture) { f.normalizedEvents = []string{"subagent_start"} }},
		{name: "event-hash-wrong", mutate: func(f *receiptFixture) { f.startHashOverride = strings.Repeat("0", 64) }},
		{name: "event-workflow-wrong", mutate: func(f *receiptFixture) { f.startWorkflow = "wf-other" }},
		{name: "event-gate-wrong", mutate: func(f *receiptFixture) { f.startGate = "qa-test-gate" }},
		{name: "event-stage-wrong", mutate: func(f *receiptFixture) { f.startStage = "Execution" }},
		{name: "event-dispatch-id-wrong", mutate: func(f *receiptFixture) { f.startDispatchID = "dispatch-other" }},
		{name: "event-dispatch-artifact-wrong", mutate: func(f *receiptFixture) { f.startDispatchArtifact = ".claude/gates/proofs/dispatch/other.json" }},
		{name: "event-dispatch-id-without-receipt-dispatch", mutate: func(f *receiptFixture) { f.receiptDispatchID = ""; f.startDispatchID = "dispatch-other" }},
		{name: "start-stop-subagent-mismatch", mutate: func(f *receiptFixture) { f.stopSubagent = "subagent-2" }},
		{name: "dispatch-missing", mutate: func(f *receiptFixture) { f.dispatchArtifact = ".claude/gates/proofs/dispatch/missing.json" }},
		{name: "dispatch-not-json", mutate: func(f *receiptFixture) { f.dispatchTextOverride = "not json" }},
		{name: "dispatch-id-wrong", mutate: func(f *receiptFixture) { f.dispatchID = "dispatch-other" }},
		{name: "dispatch-provider-wrong", mutate: func(f *receiptFixture) { f.dispatchProvider = "claude-code" }},
		{name: "dispatch-workflow-wrong", mutate: func(f *receiptFixture) { f.dispatchWorkflow = "wf-other" }},
		{name: "dispatch-gate-wrong", mutate: func(f *receiptFixture) { f.dispatchGate = "qa-test-gate" }},
		{name: "dispatch-stage-wrong", mutate: func(f *receiptFixture) { f.dispatchStage = "Execution" }},
		{name: "dispatch-review-artifact-wrong", mutate: func(f *receiptFixture) { f.dispatchReviewArtifact = "other.md" }},
		{name: "dispatch-receipt-artifact-wrong", mutate: func(f *receiptFixture) { f.dispatchReceiptArtifact = ".claude/gates/proofs/other.json" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := newReceiptFixture(t, dir)
			tc.mutate(f)
			f.write(t)
			result := Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
			if result.OK() {
				t.Fatalf("expected tampered receipt to fail")
			}
		})
	}
}

func TestCanonicalReviewArtifactHashIgnoresReceiptLine(t *testing.T) {
	base := "A: one\nReviewer proof receipt: old sha256=" + strings.Repeat("a", 64) + "\nB: two\n"
	changed := "A: one\nReviewer proof receipt: new sha256=" + strings.Repeat("b", 64) + "\nB: two\n"
	if canonicalReviewArtifactHash(base) != canonicalReviewArtifactHash(changed) {
		t.Fatal("expected canonical hash to ignore Reviewer proof receipt line")
	}
}

type receiptFixture struct {
	dir                     string
	artifact                string
	bundle                  string
	prompt                  string
	dispatch                string
	start                   string
	stop                    string
	receipt                 string
	receiptProofVersion     int
	receiptProvider         string
	receiptWorkflow         string
	receiptGate             string
	receiptStage            string
	receiptReviewArtifact   string
	receiptHashOverride     string
	receiptDispatchID       string
	dispatchArtifact        string
	dispatchID              string
	dispatchHashOverride    string
	dispatchTextOverride    string
	dispatchProvider        string
	dispatchWorkflow        string
	dispatchGate            string
	dispatchStage           string
	dispatchReviewArtifact  string
	dispatchReceiptArtifact string
	normalizedEvents        []string
	startWorkflow           string
	startGate               string
	startStage              string
	startSubagent           string
	startDispatchID         string
	startDispatchArtifact   string
	startHashOverride       string
	stopWorkflow            string
	stopGate                string
	stopStage               string
	stopSubagent            string
	stopDispatchID          string
	stopDispatchArtifact    string
}

func newReceiptFixture(t *testing.T, dir string) *receiptFixture {
	t.Helper()
	return &receiptFixture{
		dir:                     dir,
		artifact:                filepath.Join(dir, "complexity.md"),
		bundle:                  filepath.Join(dir, "bundle.md"),
		prompt:                  filepath.Join(dir, "prompt.md"),
		dispatch:                filepath.Join(dir, ".claude", "gates", "proofs", "dispatch", "dispatch.json"),
		start:                   filepath.Join(dir, "start.json"),
		stop:                    filepath.Join(dir, "stop.json"),
		receipt:                 filepath.Join(dir, ".claude", "gates", "proofs", "receipt.json"),
		receiptProofVersion:     1,
		receiptProvider:         "codex",
		receiptWorkflow:         "wf",
		receiptGate:             "complexity-gate",
		receiptStage:            "",
		receiptReviewArtifact:   "complexity.md",
		receiptDispatchID:       "dispatch-1",
		dispatchArtifact:        ".claude/gates/proofs/dispatch/dispatch.json",
		dispatchID:              "dispatch-1",
		dispatchProvider:        "codex",
		dispatchWorkflow:        "wf",
		dispatchGate:            "complexity-gate",
		dispatchStage:           "",
		dispatchReviewArtifact:  "complexity.md",
		dispatchReceiptArtifact: ".claude/gates/proofs/receipt.json",
		normalizedEvents:        []string{"subagent_start", "subagent_stop"},
		startWorkflow:           "wf",
		startGate:               "complexity-gate",
		startStage:              "",
		startSubagent:           "subagent-1",
		startDispatchID:         "dispatch-1",
		startDispatchArtifact:   ".claude/gates/proofs/dispatch/dispatch.json",
		stopWorkflow:            "wf",
		stopGate:                "complexity-gate",
		stopStage:               "",
		stopSubagent:            "subagent-1",
		stopDispatchID:          "dispatch-1",
		stopDispatchArtifact:    ".claude/gates/proofs/dispatch/dispatch.json",
	}
}

func (f *receiptFixture) write(t *testing.T) {
	t.Helper()
	mustWrite(t, f.bundle, "bundle")
	mustWrite(t, f.prompt, "formal_gate_dispatch: true\n")
	artifactText := complexityArtifactText("wf", "snap", "bundle.md sha256="+sha256FileForTest(t, f.bundle), "prompt.md sha256="+sha256FileForTest(t, f.prompt), "")
	mustWrite(t, f.artifact, artifactText)
	dispatchText := f.dispatchTextOverride
	if dispatchText == "" {
		dispatchText = `{"proofVersion":1,"dispatchId":"` + f.dispatchID + `","provider":"` + f.dispatchProvider + `","workflowId":"` + f.dispatchWorkflow + `","gate":"` + f.dispatchGate + `","stage":"` + f.dispatchStage + `","reviewArtifact":"` + f.dispatchReviewArtifact + `","receiptArtifact":"` + f.dispatchReceiptArtifact + `"}`
	}
	mustWrite(t, f.dispatch, dispatchText)
	mustWrite(t, f.start, `{"workflowId":"`+f.startWorkflow+`","gate":"`+f.startGate+`","stage":"`+f.startStage+`","normalizedEvent":"subagent_start","subagentId":"`+f.startSubagent+`","dispatchId":"`+f.startDispatchID+`","dispatchRegistrationArtifact":"`+f.startDispatchArtifact+`"}`)
	mustWrite(t, f.stop, `{"workflowId":"`+f.stopWorkflow+`","gate":"`+f.stopGate+`","stage":"`+f.stopStage+`","normalizedEvent":"subagent_stop","subagentId":"`+f.stopSubagent+`","dispatchId":"`+f.stopDispatchID+`","dispatchRegistrationArtifact":"`+f.stopDispatchArtifact+`"}`)
	dispatchHash := sha256FileForTest(t, f.dispatch)
	if f.dispatchHashOverride != "" {
		dispatchHash = f.dispatchHashOverride
	}
	startHash := sha256FileForTest(t, f.start)
	if f.startHashOverride != "" {
		startHash = f.startHashOverride
	}
	events, err := json.Marshal(f.normalizedEvents)
	if err != nil {
		t.Fatal(err)
	}
	receiptText := `{"proofVersion":` + fmt.Sprint(f.receiptProofVersion) + `,"provider":"` + f.receiptProvider + `","workflowId":"` + f.receiptWorkflow + `","gate":"` + f.receiptGate + `","stage":"` + f.receiptStage + `","dispatchId":"` + f.receiptDispatchID + `","dispatchRegistrationArtifact":"` + f.dispatchArtifact + `","dispatchRegistrationSha256":"` + dispatchHash + `","normalizedEvents":` + string(events) + `,"startEventArtifact":"start.json","startEventSha256":"` + startHash + `","stopEventArtifact":"stop.json","stopEventSha256":"` + sha256FileForTest(t, f.stop) + `","reviewArtifact":"` + f.receiptReviewArtifact + `","reviewArtifactCanonicalSha256":"` + canonicalReviewArtifactHash(artifactText) + `"}`
	mustWrite(t, f.receipt, receiptText)
	receiptHash := sha256FileForTest(t, f.receipt)
	if f.receiptHashOverride != "" {
		receiptHash = f.receiptHashOverride
	}
	withReceipt := complexityArtifactText("wf", "snap", "bundle.md sha256="+sha256FileForTest(t, f.bundle), "prompt.md sha256="+sha256FileForTest(t, f.prompt), ".claude/gates/proofs/receipt.json sha256="+receiptHash)
	mustWrite(t, f.artifact, withReceipt)
}

func complexityArtifactText(workflowID, snapshot, bundleRef, dispatchRef, receiptRef string) string {
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
		"Script result: PASS",
		"Diff shape judgment: focused",
		"Budget/expansion status: within contract; no expansion requested",
		"Impact surface health: bounded",
		"Public/config surface: none",
		"New concepts: none",
		"Minimum sufficient implementation: yes",
		"Shrink opportunities: none",
		"Decision evidence: diff",
		"gate_route:",
		"  workflow_id: " + workflowID,
		"  change_snapshot: " + snapshot,
		"  next_action: proceed",
		"  rework_owner: none",
		"  rerun_from: none",
	}
	return strings.Join(lines, "\n") + "\n"
}

func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
