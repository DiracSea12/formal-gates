package cli

import (
	"encoding/json"
	"formal-gates/internal/validate"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairCLINewCorrectionUpdatesDispatchedDiagnosis(t *testing.T) {
	root, pkg, state := repairCLIDelivery(t, "correct-new-diagnosis")
	state = repairCLIGate(t, root, pkg, state, "P2")
	state = repairCLIAdjudicate(t, root, pkg, state, "NEW", "")
	var findingID string
	for id := range state.PostReviewFindings {
		findingID = id
	}
	first := state.PostReviewFindings[findingID].Adjudications[0]
	item := validate.FindingAdjudicationInput{FindingID: findingID, Decision: "NEW", Dimensions: first.Dimensions, Verification: first.Verification, References: []string{"delivery.txt:1"}, Reason: "correct the diagnosis before assigning the repair"}
	item.Dimensions.RootCause = "corrected result-selection diagnosis"
	data, err := json.Marshal(validate.ReviewWaveAdjudicationInput{WaveID: state.ActiveReviewWaveID, StateRevision: state.StateIntegrity, Findings: []validate.FindingAdjudicationInput{item}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "correction.json")
	mustWriteCLI(t, path, string(data))
	state = decodeSemanticState(t, runCLI(t, "workflow", "adjudicate-wave", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--input", path))
	history := state.PostReviewFindings[findingID].Adjudications
	if len(state.Problems) != 1 || len(history) != 2 || history[0].Dimensions != first.Dimensions || history[1].Supersedes != 1 || state.Problems[first.ProblemID].Dimensions != item.Dimensions {
		t.Fatal("NEW correction lost history, changed identity, or left stale canonical dimensions")
	}
	prompt := runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "development-worker")
	if !strings.Contains(prompt, item.Dimensions.RootCause) || strings.Contains(prompt, "root cause="+first.Dimensions.RootCause+";") {
		t.Fatalf("repair prompt did not use the corrected diagnosis: %s", prompt)
	}
}

func TestRepairCLINoFirstWaveCannotBypassAdjudication(t *testing.T) {
	root, pkg, state := repairCLIDelivery(t, "missing-first-wave", true)
	runCLI(t, "workflow", "prepare-gate", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--gate", "quality")
	state = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID))
	d := cliOpenDispatch(state, "gate", "quality")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", d, "--reviewer", "first-quality")
	state = decodeSemanticState(t, runCLI(t, "workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--gate", "quality", "--dispatch", d, "--status", "PASS", "--compared", state.BaseSnapshot+".."+state.CurrentSnapshot, "--finding", "output needs correction", "--severity", "P2", "--location", "delivery.txt:1"))
	if len(state.ReviewWaves) != 0 || state.QAExecutionByMode["blackbox"].Status != "PENDING" {
		t.Fatal("fixture did not establish a gate P2 with pending QA before wave initialization")
	}
	before, err := os.ReadFile(validate.RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	message := failingCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "development-worker")
	after, err := os.ReadFile(validate.RunStatePath(root, state.RunID))
	if err != nil || string(before) != string(after) || !strings.Contains(message, "prepare-review-wave") || !strings.Contains(message, "adjudication") {
		t.Fatalf("early repair bypassed collection/adjudication or mutated state: %v, %s", err, message)
	}
}

func TestRepairCLIDuplicateFailReportsCurrentWaveWork(t *testing.T) {
	for _, decision := range []string{"NEW", "DISMISSED"} {
		t.Run(decision, func(t *testing.T) {
			root, pkg, state := repairCLIDelivery(t, "fail-next-step-"+strings.ToLower(decision))
			state = repairCLIGate(t, root, pkg, state, "P1")
			args := []string{"workflow", "prepare-gate", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--gate", "quality"}
			if message := failingCLI(t, args...); !strings.Contains(message, "workflow adjudicate-wave") {
				t.Fatalf("missing adjudication guidance: %s", message)
			}
			state = repairCLIAdjudicate(t, root, pkg, state, decision, "")
			before, err := os.ReadFile(validate.RunStatePath(root, state.RunID))
			if err != nil {
				t.Fatal(err)
			}
			message := failingCLI(t, args...)
			want := "workflow prepare-action --action development-worker"
			if decision == "DISMISSED" {
				want = "no OPEN problem requiring repair"
			}
			after, err := os.ReadFile(validate.RunStatePath(root, state.RunID))
			if err != nil || string(before) != string(after) || !strings.Contains(message, want) || strings.Contains(message, "wait for") {
				t.Fatalf("repeated FAIL changed state or returned stale guidance: %v, %s", err, message)
			}
		})
	}
}

// Disposable review repairCLIs: all workflow state is established by public CLI
// commands in fixture repositories; no run-state field is manually changed.
func repairCLIDelivery(t *testing.T, id string, withQA ...bool) (string, string, validate.RunState) {
	t.Helper()
	root, pkg := cliWorkflowFixture(t)
	state := decodeSemanticState(t, runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", id, "--requirement", "requirements.md", "--vcs", "git", "--split", "no"))
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	state = decodeSemanticState(t, runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", id, "--confirmed"))
	for _, action := range []string{"product-review", "start-readiness"} {
		d := prepareSemanticCLI(t, root, pkg, id, action, false, "")
		state = recordSemanticCLI(t, root, pkg, id, action, d, "PASS", "", "")
	}
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", id, "--decision", "no-split", "--note", "single delivery")
	route := []string{"workflow", "route", "--root", root, "--package-root", pkg, "--run-id", id, "--mode", "custom", "--gate", "quality"}
	if len(withQA) > 0 && withQA[0] {
		route = append(route, "--gate", "blackbox")
	}
	state = decodeSemanticState(t, runCLI(t, route...))
	if len(withQA) > 0 && withQA[0] {
		qaTree := filepath.Join(t.TempDir(), "qa")
		cliGit(t, root, "worktree", "add", "--detach", qaTree, state.BaseSnapshot)
		runCLI(t, "workflow", "qa-worktree", "--root", root, "--package-root", pkg, "--run-id", id, "--worktree", qaTree)
		design := repairCLIPrepareMode(t, root, pkg, id, "qa-design", "blackbox")
		state = decodeSemanticState(t, runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", id, "--dispatch", design, "--case", "public operation", "--mode", "blackbox", "--procedure", "run the public command", "--oracle", "expected output", "--ac", "AC-001"))
		review := repairCLIPrepareMode(t, root, pkg, id, "qa-review", "blackbox")
		state = decodeSemanticState(t, runCLI(t, "workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", id, "--dispatch", review, "--case", state.QACasesByMode["blackbox"][0].ID, "--outcome", "PASS", "--source-decision", "REQ-001=PASS", "--point-decision", "AC-001=PASS", "--case-decision", state.QACasesByMode["blackbox"][0].ID+"=PASS"))
	}
	d := cliPrepareAction(t, root, pkg, id, "development-worker")
	mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "delivery\n")
	cliGit(t, root, "add", "delivery.txt")
	cliGit(t, root, "commit", "-m", "delivery")
	state = decodeSemanticState(t, runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", id, "--dispatch", d))
	return root, pkg, state
}

func repairCLIPrepareMode(t *testing.T, root, pkg, id, action, mode string) string {
	t.Helper()
	runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", id, "--action", action, "--mode", mode)
	s := decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", id))
	d := cliOpenDispatch(s, "action", action)
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", id, "--dispatch", d, "--reviewer", "agent-"+d)
	return d
}

func repairCLIGate(t *testing.T, root, pkg string, state validate.RunState, severity string) validate.RunState {
	t.Helper()
	runCLI(t, "workflow", "prepare-review-wave", "--root", root, "--package-root", pkg, "--run-id", state.RunID)
	state = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID))
	d := state.ReviewWaves[state.ActiveReviewWaveID].Expected["gate:quality"].DispatchID
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", d, "--reviewer", "reviewer-"+d)
	status := "PASS"
	if severity == "P1" {
		status = "FAIL"
	}
	args := []string{"workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--gate", "quality", "--dispatch", d, "--status", status, "--compared", state.BaseSnapshot + ".." + state.CurrentSnapshot}
	if severity != "" {
		args = append(args, "--finding", "output requires correction", "--severity", severity)
		if severity == "P1" {
			args = append(args, cliP1FindingEvidence("delivery.txt:1", true)...)
		} else {
			args = append(args, "--location", "delivery.txt:1")
		}
	}
	return decodeSemanticState(t, runCLI(t, args...))
}

func repairCLIAdjudicate(t *testing.T, root, pkg string, state validate.RunState, decision, problem string) validate.RunState {
	t.Helper()
	input := validate.ReviewWaveAdjudicationInput{WaveID: state.ActiveReviewWaveID, StateRevision: state.StateIntegrity}
	for id, f := range state.PostReviewFindings {
		if f.WaveID == input.WaveID {
			input.Findings = append(input.Findings, validate.FindingAdjudicationInput{FindingID: id, Decision: decision, ProblemID: problem,
				Dimensions:   validate.ProblemDimensions{Requirement: "REQ-001", NormalFailurePath: "run the public command", AffectedModule: "delivery", RootCause: "output branch", ActualConsequence: "incorrect output"},
				Verification: validate.FindingVerification{Scope: "checked requirement", Severity: "checked consequence", Causality: "traced branch", Reproduction: "independently ran command"}, References: []string{"delivery.txt:1"}, Reason: "independent operator judgment"})
		}
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "adjudication.json")
	mustWriteCLI(t, path, string(data))
	return decodeSemanticState(t, runCLI(t, "workflow", "adjudicate-wave", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--input", path))
}

func repairCLIPrepareDevelopment(t *testing.T, root, pkg, runID string) string {
	t.Helper()
	runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", "development-worker")
	s := decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", runID))
	d := cliOpenDispatch(s, "action", "development-worker")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", runID, "--dispatch", d, "--reviewer", "worker-"+d)
	return d
}
func TestRepairCLIDuplicateAndRuntimeRetry(t *testing.T) {
	root, pkg, s := repairCLIDelivery(t, "repair-cli-retry")
	s = repairCLIGate(t, root, pkg, s, "P2")
	s = repairCLIAdjudicate(t, root, pkg, s, "NEW", "")
	args := []string{"workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--action", "development-worker"}
	firstPrompt := runCLI(t, args...)
	s = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", s.RunID))
	first := s.ReviewWaves[s.ActiveReviewWaveID].RepairPlan.DispatchID
	count, taskCount := len(s.Dispatches), len(s.DispatchObligations)
	if repeated := runCLI(t, args...); repeated != firstPrompt {
		t.Fatal("OPEN repair did not reuse its canonical prompt")
	}
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", first, "--reviewer", "worker-"+first)
	if repeated := runCLI(t, args...); repeated != firstPrompt {
		t.Fatal("CLAIMED repair did not reuse its canonical prompt")
	}
	s = decodeSemanticState(t, runCLI(t, "workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--action", "development-worker", "--dispatch", first, "--status", "RUNTIME_ERROR", "--message", "worker service unavailable"))
	key := s.Dispatches[first].TaskKey
	if s.DispatchObligations[key].Status != "RETRYABLE" || s.Dispatches[first].Status != "RUNTIME_ERROR" || len(s.Dispatches) != count || len(s.DispatchObligations) != taskCount {
		t.Fatal("development runtime failure lost its obligation or repeat preparation minted work")
	}
	second := repairCLIPrepareDevelopment(t, root, pkg, s.RunID)
	s = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", s.RunID))
	if second == first || s.Dispatches[second].TaskKey != key || s.Dispatches[second].Attempt != 2 || s.ReviewWaves[s.ActiveReviewWaveID].RepairPlan.DispatchID != second {
		t.Fatal("retry did not retain the plan and bind its second attempt")
	}
	s = decodeSemanticState(t, runCLI(t, "workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--action", "development-worker", "--dispatch", second, "--status", "RUNTIME_ERROR", "--message", "worker still unavailable"))
	if s.DispatchObligations[key].Status != "RETRY_EXHAUSTED" || !strings.Contains(failingCLI(t, args...), "RETRY_EXHAUSTED") {
		t.Fatal("development retry did not stop at the finite attempt limit")
	}
}

func TestRepairCLIDismissedGateCanSeal(t *testing.T) {
	root, pkg, s := repairCLIDelivery(t, "repair-cli-dismiss")
	s = repairCLIGate(t, root, pkg, s, "P1")
	s = repairCLIAdjudicate(t, root, pkg, s, "DISMISSED", "")
	if s.Gates["quality"].Status != "FAIL" || len(s.Problems) != 0 {
		t.Fatal("dismissal lost the raw gate audit or created a problem")
	}
	result := runCLI(t, "workflow", "seal", "--root", root, "--package-root", pkg, "--run-id", s.RunID)
	if !strings.Contains(result, "SEALED") {
		t.Fatalf("dismissed gate did not seal: %s", result)
	}
}

func TestRepairCLILateDisposedGateStaysAuditOnly(t *testing.T) {
	root, pkg, s := repairCLIDelivery(t, "repair-cli-late")
	runCLI(t, "workflow", "prepare-review-wave", "--root", root, "--package-root", pkg, "--run-id", s.RunID)
	s = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", s.RunID))
	waveID := s.ActiveReviewWaveID
	d := s.ReviewWaves[waveID].Expected["gate:quality"].DispatchID
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", d, "--reviewer", "late-gate-reviewer")
	s = decodeSemanticState(t, runCLI(t, "workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--gate", "quality", "--dispatch", d, "--status", "RUNTIME_ERROR", "--message", "provider timeout"))
	s = decodeSemanticState(t, runCLI(t, "workflow", "resolve-wave-target", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--target", "gate:quality", "--state-revision", s.StateIntegrity, "--reason", "user ends the unavailable target"))
	before := s.DispatchObligations[s.Dispatches[d].TaskKey]
	for range 2 {
		s = decodeSemanticState(t, runCLI(t, "workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--gate", "quality", "--dispatch", d, "--status", "PASS", "--compared", s.BaseSnapshot+".."+s.CurrentSnapshot, "--message", "late provider response"))
	}
	wave := s.ReviewWaves[waveID]
	if len(wave.LateResults) != 1 || wave.LateResults[0].Status != "PASS" || wave.Expected["gate:quality"].Status != "DISPOSED" || wave.Status != "COMPLETE" || s.Gates["quality"].Status != "RUNTIME_ERROR" || s.Dispatches[d].Status != "RUNTIME_ERROR" || s.DispatchObligations[s.Dispatches[d].TaskKey].BasisDigest != before.BasisDigest {
		t.Fatal("late report changed authoritative results, wave disposition or attempt budget")
	}
}

func TestRepairCLIResolvedProblemDoesNotReopen(t *testing.T) {
	root, pkg, s := repairCLIDelivery(t, "repair-cli-resolved")
	s = repairCLIGate(t, root, pkg, s, "P1")
	s = repairCLIAdjudicate(t, root, pkg, s, "NEW", "")
	var problem string
	for id := range s.Problems {
		problem = id
	}
	d := repairCLIPrepareDevelopment(t, root, pkg, s.RunID)
	mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "corrected delivery\n")
	cliGit(t, root, "add", "delivery.txt")
	cliGit(t, root, "commit", "-m", "fix delivery")
	s = decodeSemanticState(t, runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", d))
	s = repairCLIGate(t, root, pkg, s, "")
	if s.Problems[problem].Status != "RESOLVED" || s.Problems[problem].ResolvedAt != s.CurrentSnapshot {
		t.Fatal("successful repair verification did not resolve the canonical problem")
	}
	runCLI(t, "workflow", "prepare-candidate", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--squash-message", "final candidate")
	s = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", s.RunID))
	s = repairCLIGate(t, root, pkg, s, "P2")
	s = repairCLIAdjudicate(t, root, pkg, s, "SAME_AS", problem)
	if s.ReviewWaves[s.ActiveReviewWaveID].Status != "COMPLETE" || s.Problems[problem].Status != "RESOLVED" {
		t.Fatal("SAME_AS reopened a previously resolved problem on the final candidate")
	}
}

func TestRepairCLIResetRebuildsReviewWave(t *testing.T) {
	root, pkg, s := repairCLIDelivery(t, "repair-cli-reset", true)
	s = repairCLIGate(t, root, pkg, s, "P2")
	qa := repairCLIPrepareMode(t, root, pkg, s.RunID, "qa-execution", "blackbox")
	s = repairCLIRecordQA(t, root, pkg, s, qa, "PASS")
	s = repairCLIAdjudicate(t, root, pkg, s, "NEW", "")
	runCLI(t, "workflow", "reset", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--user-approve")
	s = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", s.RunID))
	if len(s.ReviewWaves) != 0 || s.ActiveReviewWaveID != "" || len(s.QACaseSets) != 0 || len(s.Problems) != 0 || len(s.PostReviewFindings) != 0 || len(s.P2CompletionCredentials) != 0 {
		t.Fatal("reset retained state whose source results were cleared")
	}
	s = cliRecordAction(t, root, pkg, s, "requirements-clarification", "PASS")
	s = decodeSemanticState(t, runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--confirmed"))
	for _, action := range []string{"product-review", "start-readiness"} {
		d := prepareSemanticCLI(t, root, pkg, s.RunID, action, false, "")
		s = recordSemanticCLI(t, root, pkg, s.RunID, action, d, "PASS", "", "")
	}
	// Restore the cleared QA design/review through its ordinary public commands.
	qaTree := filepath.Join(t.TempDir(), "qa-reset")
	cliGit(t, root, "worktree", "add", "--detach", qaTree, s.BaseSnapshot)
	runCLI(t, "workflow", "qa-worktree", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--worktree", qaTree)
	d := repairCLIPrepareMode(t, root, pkg, s.RunID, "qa-design", "blackbox")
	s = decodeSemanticState(t, runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", d, "--case", "public operation", "--mode", "blackbox", "--procedure", "run public command", "--oracle", "expected output", "--ac", "AC-001"))
	d = repairCLIPrepareMode(t, root, pkg, s.RunID, "qa-review", "blackbox")
	cid := s.QACasesByMode["blackbox"][0].ID
	runCLI(t, "workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", d, "--case", cid, "--outcome", "PASS", "--source-decision", "REQ-001=PASS", "--point-decision", "AC-001=PASS", "--case-decision", cid+"=PASS")
	runCLI(t, "workflow", "prepare-review-wave", "--root", root, "--package-root", pkg, "--run-id", s.RunID)
	s = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", s.RunID))
	if s.ReviewWaves[s.ActiveReviewWaveID].Status != "COLLECTING" || s.ReviewWaves[s.ActiveReviewWaveID].Expected["gate:quality"].Status != "PREPARED" {
		t.Fatal("reset could not prepare a fresh review of the retained snapshot")
	}
}

func repairCLIRecordQA(t *testing.T, root, pkg string, s validate.RunState, dispatch, outcome string) validate.RunState {
	t.Helper()
	cid := s.QACasesByMode["blackbox"][0].ID
	return decodeSemanticState(t, runCLI(t, "workflow", "qa-execution", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", dispatch, "--case-result", cid, "--outcome", outcome, "--procedure", "ran the documented command", "--observation", "the command returned the observed output", "--oracle-result", "compared output to the approved oracle"))
}

func TestRepairCLIP2VerificationFailureCanRepair(t *testing.T) {
	for _, outcome := range []string{"PASS", "FAIL"} {
		t.Run(outcome, func(t *testing.T) {
			root, pkg, s := repairCLIDelivery(t, "repair-cli-p2-"+strings.ToLower(outcome), true)
			s = repairCLIGate(t, root, pkg, s, "P2")
			d := repairCLIPrepareMode(t, root, pkg, s.RunID, "qa-execution", "blackbox")
			s = repairCLIRecordQA(t, root, pkg, s, d, "PASS")
			s = repairCLIAdjudicate(t, root, pkg, s, "NEW", "")
			oldWave, count := s.ActiveReviewWaveID, s.CompletedReviewWaves
			d = repairCLIPrepareDevelopment(t, root, pkg, s.RunID)
			mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "corrected delivery\n")
			cliGit(t, root, "add", "delivery.txt")
			cliGit(t, root, "commit", "-m", "P2 correction")
			s = decodeSemanticState(t, runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--dispatch", d))
			cid := s.QACasesByMode["blackbox"][0].ID
			runCLI(t, "workflow", "complete-p2-repair", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--wave", oldWave, "--build-evidence", "build passed", "--test-evidence", "all project tests passed", "--affected-case", cid)
			runCLI(t, "workflow", "qa-execution-scope", "--root", root, "--package-root", pkg, "--run-id", s.RunID, "--mode", "blackbox", "--decision", "AFFECTED", "--cases", cid, "--reason", "repair affects the public output")
			d = repairCLIPrepareMode(t, root, pkg, s.RunID, "qa-execution", "blackbox")
			s = repairCLIRecordQA(t, root, pkg, s, d, outcome)
			wave := s.ReviewWaves[s.ActiveReviewWaveID]
			if wave.Snapshot != s.CurrentSnapshot || s.Gates["quality"].Status != "PASS" || wave.Expected["gate:quality"].DispatchID != "" {
				t.Fatal("P2 verification lost the current snapshot or re-dispatched the completed gate")
			}
			if outcome == "PASS" {
				if s.CompletedReviewWaves != count || wave.Status != "COMPLETE" {
					t.Fatal("P2 verification PASS consumed a review round")
				}
			} else {
				if wave.Status != "AWAITING_ADJUDICATION" {
					t.Fatal("P2 QA failure did not become adjudicable")
				}
				s = repairCLIAdjudicate(t, root, pkg, s, "NEW", "")
				repairCLIPrepareDevelopment(t, root, pkg, s.RunID)
			}
		})
	}
}
