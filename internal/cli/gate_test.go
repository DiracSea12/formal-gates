package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestCLIGateRunAndReportStandalone covers detached CLI commands: gate
// run assembles the standalone prompt (no requirement or dispatch block) and gate
// report validates and displays a reviewer result without persisting anything.
func TestCLIGateRunAndReportStandalone(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	prompt := runCLI(t, "gate", "run", "--root", root, "--package-root", pkg, "--vcs", "git", "quality")
	for _, block := range []string{"[Shared reviewer contract]", "[Gate: quality]", "[Current change]", "[Result contract]"} {
		if !strings.Contains(prompt, block) {
			t.Fatalf("gate run prompt missing %s: %s", block, prompt)
		}
	}
	if strings.Contains(prompt, "[Current requirement]") || strings.Contains(prompt, "[Dispatch]") {
		t.Fatalf("gate run prompt must not carry requirement or dispatch blocks: %s", prompt)
	}

	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"gate", "run", "--root", root, "--package-root", pkg, "missing"}, IO{Stdout: &stdout, Stderr: &stderr}); code == 0 {
		t.Fatal("gate run accepted an unknown gate id")
	}

	for _, sample := range []struct{ status, body, message string }{
		{"PASS", `{"findings":[]}`, ""},
		{"FAIL", `{"findings":[{"severity":"P1","message":"blocker","locations":["internal/cli/cli.go:1"]}]}`, "blocker"},
		{"RUNTIME_ERROR", `{"interruptionReason":"Native VCS unavailable"}`, "Native VCS unavailable"},
	} {
		t.Run(sample.status, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			if code := Run("formal-gates", []string{"gate", "report"}, IO{Stdin: strings.NewReader(sample.body), Stdout: &stdout, Stderr: &stderr}); code != 0 {
				t.Fatalf("gate report rejected a valid %s result: %s", sample.status, stderr.String())
			}
			for _, want := range []string{"脱离 run 的快速检查", "status: " + sample.status, sample.message} {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("gate report display missing %q: %s", want, stdout.String())
				}
			}
		})
	}
}
