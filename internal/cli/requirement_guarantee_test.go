package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

func TestCLIPlainRequirementConfirmationRunsStructuredPrecheck(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	bad := "## 需求点\n\n### REQ-001：first\n#### 要求\nfirst\n#### 验收条件\n- AC-001：first\n#### 来源\nuser\n\n### REQ-001：second\n#### 要求\nsecond\n#### 验收条件\n- AC-002：second\n#### 来源\nuser\n"
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), bad)
	cliGit(t, root, "add", "requirements.md")
	cliGit(t, root, "commit", "-m", "malformed requirement fixture")
	state := decodeSemanticState(t, runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "plain-precheck", "--requirement", "requirements.md", "--vcs", "git", "--split", "no"))
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	errText := failingCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	if !strings.Contains(errText, "requirements.md:11") || !strings.Contains(errText, "REQ-001") || !strings.Contains(errText, "first declared at line 3") || !strings.Contains(errText, "duplicate REQ ID") {
		t.Fatalf("plain confirmation omitted duplicate id and both exact locations: %s", errText)
	}
	state = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID))
	if state.RequirementConfirmed || state.RequirementGuarantee != nil {
		t.Fatalf("failed public precheck partially committed state: %#v", state)
	}
}

const cliGuaranteeRequirement = `# 正式需求

自然语言背景继续保留。

## 需求点

### REQ-001：公开行为

#### 要求

公开入口必须返回两个可观察结果。

#### 验收条件

- AC-001：第一个结果通过。
- AC-009：第二个结果通过。

#### 来源

用户确认。
`

func TestCLIFormalConfirmationDoesNotInferGuaranteeWithoutFlag(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliGuaranteeRequirement)
	cliGit(t, root, "add", "requirements.md")
	cliGit(t, root, "commit", "-m", "structured requirement")

	state := decodeSemanticState(t, runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "explicit-guarantee", "--requirement", "requirements.md", "--vcs", "git", "--split", "no"))
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	state = decodeSemanticState(t, runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed"))
	if state.RequirementGuarantee != nil {
		t.Fatalf("confirmation without --activate-guarantee inferred an activation: %#v", state.RequirementGuarantee)
	}
	state = cliRecordAction(t, root, pkg, state, "product-review", "PASS")
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")
	state = decodeSemanticState(t, runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "one bounded delivery"))

	for _, args := range [][]string{
		{"workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "full"},
		{"workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "custom", "--gate", "blackbox"},
	} {
		if errText := failingCLI(t, args...); !strings.Contains(errText, "explicit REQ/AC guarantee activation") {
			t.Fatalf("QA route without explicit guarantee activation returned %q", errText)
		}
	}
	state = decodeSemanticState(t, runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "custom", "--gate", "quality"))
	if errText := failingCLI(t, "workflow", "route-add", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--gate", "blackbox"); !strings.Contains(errText, "explicit REQ/AC guarantee activation") {
		t.Fatalf("adding QA without explicit guarantee activation returned %q", errText)
	}
	if errText := failingCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed", "--activate-guarantee"); !strings.Contains(errText, "must be activated before Product Review") {
		t.Fatalf("late guarantee activation returned %q", errText)
	}
	state = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID))
	if state.RequirementGuarantee != nil || state.SelectedGates[0] != "quality" {
		t.Fatalf("rejected QA activation changed route or guarantee state: %#v", state)
	}

	lightweight := decodeSemanticState(t, runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "lightweight-no-guarantee", "--requirement", "requirements.md", "--vcs", "git", "--route", "lightweight"))
	lightweight = decodeSemanticState(t, runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", lightweight.RunID, "--confirmed"))
	if lightweight.RequirementGuarantee != nil {
		t.Fatalf("lightweight confirmation entered the guarantee: %#v", lightweight.RequirementGuarantee)
	}
}

func TestCLIRequirementGuaranteePublicFlagsAndShow(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliGuaranteeRequirement)
	cliGit(t, root, "add", "requirements.md")
	cliGit(t, root, "commit", "-m", "structured requirement")

	out := runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "cli-guarantee", "--requirement", "requirements.md", "--vcs", "git", "--split", "no")
	var state validate.RunState
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	out = runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed", "--activate-guarantee")
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	if state.RequirementGuarantee == nil || state.RequirementGuarantee.Activation != "frozen" || state.RequirementGuarantee.Projection == nil {
		t.Fatalf("CLI confirmation did not freeze guarantee: %#v", state.RequirementGuarantee)
	}

	product := cliPrepareAction(t, root, pkg, state.RunID, "product-review")
	recordArgs := []string{"workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "product-review", "--dispatch", product, "--status", "PASS", "--item", "REQ-001 obligation-to-AC completeness", "--item-status", "PASS"}
	recordArgs = append(recordArgs, cliOperatorVerificationArgs()...)
	out = runCLI(t, recordArgs...)
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "one bounded delivery")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "custom", "--gate", "blackbox")

	design := cliPrepareAction(t, root, pkg, state.RunID, "qa-design")
	runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", design,
		"--case", "both public outcomes", "--mode", "blackbox", "--procedure", "run the public command", "--oracle", "both outcomes pass", "--ac", "AC-001", "--ac", "AC-009")
	state, _ = validate.LoadRunState(root, state.RunID)
	if len(state.QACasesByMode[""]) != 1 || strings.Join(state.QACasesByMode[""][0].AcceptanceCriteria, ",") != "AC-001,AC-009" {
		t.Fatalf("repeatable CLI AC bindings were not recorded: %#v", state.QACasesByMode)
	}

	review := cliPrepareAction(t, root, pkg, state.RunID, "qa-review")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", review, "--reviewer", "cli-guarantee-reviewer")
	runCLI(t, "workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", review,
		"--case", "CASE-001", "--outcome", "PASS",
		"--source-decision", "REQ-001=PASS",
		"--point-decision", "AC-001=PASS", "--point-decision", "AC-009=PASS",
		"--case-decision", "CASE-001=PASS")

	var shown validate.RunState
	if err := json.Unmarshal([]byte(runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID)), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.RequirementGuarantee == nil || shown.RequirementGuarantee.Report.RequirementCount != 1 || shown.RequirementGuarantee.Report.AcceptanceCount != 2 || len(shown.RequirementGuarantee.Report.Items) != 2 {
		t.Fatalf("workflow show omitted itemized guarantee projection: %#v", shown.RequirementGuarantee)
	}
}

func TestCLIRequirementGuaranteeFlagValidationAndHelp(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "qa-design", "--ac", "AC-001"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "must follow --case") {
		t.Fatalf("--ac before --case was accepted: %q", stderr.String())
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"workflow", "requirement", "--help"}, "activate-guarantee"},
		{[]string{"workflow", "slicing", "--help"}, "ac-owner"},
		{[]string{"workflow", "qa-review", "--help"}, "source-decision"},
		{[]string{"workflow", "seal", "--help"}, "guarantee-waiver-reason"},
	} {
		var stdout bytes.Buffer
		if code := Run("formal-gates", tc.args, IO{Stdout: &stdout}); code != 0 || !strings.Contains(stdout.String(), tc.want) {
			t.Fatalf("%v help omitted %q: %s", tc.args, tc.want, stdout.String())
		}
	}
}
