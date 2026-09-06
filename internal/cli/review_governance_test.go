package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

func TestCLIA1FindingContractWritesOnlyRejectedReceipt(t *testing.T) {
	root, pkg, state := startConfirmedSemanticCLI(t, "cli-a1-invalid-result")
	dispatch := prepareSemanticCLI(t, root, pkg, state.RunID, "product-review", false, "")
	args := []string{
		"workflow", "record-action", "--root", root, "--package-root", pkg,
		"--run-id", state.RunID, "--action", "product-review", "--dispatch", dispatch,
		"--status", "FAIL", "--finding", "confirmed requirement cannot be completed",
		"--severity", "P1", "--requirement", "REQ-001",
		"--normal-path", "run the documented public workflow from a clean checkout",
		"--deferral-impact", "users remain blocked from the documented workflow",
		"--verify-scope", "checked the complete requested scope",
		"--verify-severity", "confirmed the normal path is blocked",
		"--verify-causality", "traced the failure to the cited requirement",
		"--verify-reproduction", "reproduced on the current candidate",
		"--location", "requirements.md:1",
	}
	args = append(args, cliOperatorVerificationArgs()...)
	if errText := failingCLI(t, args...); !strings.Contains(errText, "REJECTED_INVALID_RESULT") {
		t.Fatalf("CLI did not return the invalid-result receipt: %s", errText)
	}
	loaded, err := validate.LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.RejectedResults) != 1 || loaded.Actions["product-review"].Status != "PENDING" || loaded.Dispatches[dispatch].ResultStatus != "REJECTED_INVALID_RESULT" {
		t.Fatalf("CLI invalid result changed semantic state or lost receipt: %#v", loaded)
	}
	for _, command := range []string{"review-delta-preview", "requirement-impact-preview", "authorize-retry"} {
		if !strings.Contains(runCLI(t, "workflow", command, "--help"), command) {
			t.Fatalf("workflow %s help is not registered", command)
		}
	}
}

// AC-022 runs the documented JSON through the public parser and command. All
// run/finding/revision identifiers come from command output in a temporary repo;
// this fixture is developer verification, not a recorded independent review.
func TestCLIGateP2ToP1DocumentedAdjudication(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	runID := "cli-gate-p1-correction"
	state := decodeSemanticState(t, runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", runID, "--requirement", "requirements.md", "--vcs", "git", "--split", "no"))
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	state = decodeSemanticState(t, runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", runID, "--confirmed"))
	for _, action := range []string{"product-review", "start-readiness"} {
		dispatch := prepareSemanticCLI(t, root, pkg, runID, action, false, "")
		state = recordSemanticCLI(t, root, pkg, runID, action, dispatch, "PASS", "", "")
	}
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", runID, "--decision", "no-split", "--note", "single gate correction fixture")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", runID, "--mode", "custom", "--gate", "quality")
	development := cliPrepareAction(t, root, pkg, runID, "development-worker")
	mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "delivery\n")
	cliGit(t, root, "add", "delivery.txt")
	cliGit(t, root, "commit", "-m", "delivery for gate correction")
	state = decodeSemanticState(t, runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", runID, "--dispatch", development))
	runCLI(t, "workflow", "prepare-review-wave", "--root", root, "--package-root", pkg, "--run-id", runID)
	state = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", runID))
	gateDispatch := state.ReviewWaves[state.ActiveReviewWaveID].Expected["gate:quality"].DispatchID
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", runID, "--dispatch", gateDispatch, "--reviewer", "cli-gate-correction-reviewer")
	runCLI(t, "workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", runID, "--gate", "quality", "--dispatch", gateDispatch, "--status", "PASS", "--compared", state.BaseSnapshot+".."+state.CurrentSnapshot, "--finding", "incorrect first result", "--severity", "P2", "--location", "delivery.txt:1")
	state = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", runID))
	var findingID string
	for id := range state.PostReviewFindings {
		findingID = id
	}
	doc, err := os.ReadFile(filepath.Join("..", "..", "references", "formal-flow.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(doc), "<!-- gate-p1-evidence-input-example -->")
	if !ok {
		t.Fatal("documented gate P1 example missing")
	}
	_, section, _ = strings.Cut(section, "```json\n")
	example, _, _ := strings.Cut(section, "\n```")
	example = strings.NewReplacer("${wave_id}", state.ActiveReviewWaveID, "${state_revision}", state.StateIntegrity, "${finding_id}", findingID).Replace(example)
	inputPath := filepath.Join(t.TempDir(), "gate-p1.json")
	var input validate.ReviewWaveAdjudicationInput
	if err := json.Unmarshal([]byte(example), &input); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(validate.RunStatePath(root, runID))
	if err != nil {
		t.Fatal(err)
	}
	input.Findings[0].Verification.Reproduction = ""
	invalid, _ := json.Marshal(input)
	mustWriteCLI(t, inputPath, string(invalid))
	args := []string{"workflow", "adjudicate-wave", "--root", root, "--package-root", pkg, "--run-id", runID, "--input", inputPath}
	if failure := failingCLI(t, args...); !strings.Contains(failure, "reproduction") {
		t.Fatalf("missing independent reproduction was not rejected: %s", failure)
	}
	after, _ := os.ReadFile(validate.RunStatePath(root, runID))
	if string(after) != string(before) {
		t.Fatal("rejected operator correction wrote workflow state")
	}
	mustWriteCLI(t, inputPath, example)
	corrected := decodeSemanticState(t, runCLI(t, args...))
	finding := corrected.PostReviewFindings[findingID]
	if corrected.Gates["quality"].Status != "FAIL" || finding.Severity != "P1" || len(finding.Adjudications) != 1 || finding.Adjudications[0].GateP1Evidence == nil || finding.Adjudications[0].OriginalFinding.Severity != "P2" || corrected.CompletedReviewWaves != state.CompletedReviewWaves || len(corrected.Dispatches) != len(state.Dispatches) {
		t.Fatalf("documented correction did not produce a bound P1 without dispatch: %+v", finding)
	}
	if failure := failingCLI(t, "workflow", "seal", "--root", root, "--package-root", pkg, "--run-id", runID); !strings.Contains(failure, "before Seal") {
		t.Fatalf("public Seal did not observe corrected P1: %s", failure)
	}
	if help := runCLI(t, "workflow", "adjudicate-wave", "--help"); !strings.Contains(help, "gateP1Evidence") {
		t.Fatal("public help omitted the correction input")
	}
}
