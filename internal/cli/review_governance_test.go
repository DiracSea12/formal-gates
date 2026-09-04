package cli

import (
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
