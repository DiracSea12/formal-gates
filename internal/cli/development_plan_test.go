package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

func TestCLIDevelopmentPlanRegistration(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "editable-plan")
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	state = cliRecordAction(t, root, pkg, state, "product-review", "PASS")
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")
	path := filepath.Join(t.TempDir(), "plan.json")
	mustWriteCLI(t, path, `{
  "reason": "single coherent parser change",
  "subtasks": [{"id":"parse", "task":"implement parsing"}, {"id":"check", "task":"validate parsing"}],
  "batches": [{"id":"parser", "task":"deliver parser", "subtaskIds":["parse","check"], "dependsOn":[],
    "interfaces":"parse(input)", "stateBoundary":"no shared state", "risks":"malformed input",
    "dod":"valid input parsed and invalid input rejected", "validation":["go test ./..."],
    "recovery":"revert commit", "handoff":"document decisions", "contextCost":"one module"}]
}`)
	args := []string{"workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "single unit", "--batch-plan", path}
	var stderr bytes.Buffer
	if code := Run("formal-gates", args, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "--user-confirm") {
		t.Fatalf("missing confirmation accepted: %s", stderr.String())
	}
	runCLI(t, append(args, "--user-confirm")...)
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "full")
	state, err := validate.LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	plan := state.DevelopmentBatchPlan
	if plan == nil || plan.Version != 1 || len(plan.Batches[0].Subtasks) != 2 || plan.TechnicalReviewDispatch == "" || plan.RequirementRevision != state.RequirementRevision {
		t.Fatalf("incomplete registered contract: %+v", plan)
	}
	prompt := runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "development-worker")
	if !strings.Contains(prompt, "1. [parse]") || !strings.Contains(prompt, "2. [check]") {
		t.Fatal("ordered subtasks missing from canonical prompt")
	}
}
