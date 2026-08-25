// Command harness is the installed test-only phase-2 protocol driver. It stays
// outside cmd/formal-gates so workflow drive/submit cannot become public write
// paths before their owning phase.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"formal-gates/internal/engine/testkit"
)

func main() {
	var options testkit.HarnessOptions
	flag.StringVar(&options.ProjectRoot, "project-root", os.Getenv("FORMAL_GATES_TEST_PROJECT"), "isolated test project root")
	flag.StringVar(&options.Scenario, "scenario", "smoke", "scenario: smoke, initialize, load, envelope, envelope-write, revision-sequence, submit-request, submit-decision, submit-worker, submit-spawn, submit-lifecycle, submit-operator, idempotency, freshness, concurrent-submit, cas, fingerprint, fault, recover, lock-recovery, host-action, reconcile-host-action, lifecycle, interruption, unknown-receipt, result-before-receipt, capacity-refill, invalid-events, terminal, query-terminal, terminal-replay, next-sequence, failure-routing, definition-declaredness, receipt-file, wait-user-action, or full")
	flag.StringVar(&options.EventID, "event-id", "", "caller-owned event ID")
	flag.StringVar(&options.ActionID, "action-id", "", "issued action ID")
	flag.StringVar(&options.RequestID, "request-id", "", "pending request ID")
	flag.StringVar(&options.Control, "control", "", "RESET, ABORT, or RECOVER_ATTEMPT")
	flag.StringVar(&options.Choice, "choice", "", "Ask option ID")
	flag.StringVar(&options.Provider, "provider", "", "event provider identity")
	flag.StringVar(&options.Correlation, "correlation", "", "receipt/lifecycle correlation")
	flag.StringVar(&options.Identity, "identity", "", "fake host lifecycle identity")
	flag.StringVar(&options.Status, "status", "", "receipt status")
	flag.StringVar(&options.Outcome, "outcome", "", "typed worker outcome")
	flag.StringVar(&options.PayloadDigest, "payload-digest", "", "typed payload digest or freshness token")
	flag.StringVar(&options.FailureClass, "failure-class", "", "declared failure class")
	flag.StringVar(&options.LifecycleEvent, "lifecycle-event", "", "subagent_start or subagent_stop")
	flag.StringVar(&options.Fault, "fault", "", "named deterministic fault point")
	flag.StringVar(&options.MissingField, "missing-field", "", "envelope field to remove")
	flag.StringVar(&options.Target, "target", "", "envelope-write target path")
	flag.StringVar(&options.Target, "target-path", "", "alias for --target")
	flag.StringVar(&options.Interruption, "interruption", "", "transient, nontransient, unknown, receipt-one, receipt-multiple, or receipt-none")
	flag.StringVar(&options.Fixture, "fixture", "", "JSON fixture for a registered harness scenario")
	flag.StringVar(&options.DefinitionFixture, "definition-fixture", "", "workflow definition fixture for declaredness scenarios")
	flag.StringVar(&options.Declared, "declared", "", "declaredness override: true/false or declared/undeclared")
	flag.StringVar(&options.Declaredness, "declaredness", "", "alias for --declared")
	flag.StringVar(&options.ReceiptFile, "receipt-file", "", "JSON HostAction receipt file")
	flag.StringVar(&options.Prepare, "prepare", "", "prepare a receipt template: adapter or terminate")
	flag.StringVar(&options.Template, "template", "", "path for a prepared receipt template")
	var bindTemplatePath string
	flag.StringVar(&bindTemplatePath, "bind-template", "", "write a receipt template for the current pending HostAction")
	flag.BoolVar(&options.Continue, "continue", false, "continue a registered fixture-driven scenario after recovery")
	flag.Uint64Var(&options.ExpectedRevision, "expected-revision", 0, "caller-observed revision for CAS scenarios")
	flag.IntVar(&options.Capacity, "capacity", 0, "scenario admission capacity")
	flag.StringVar(&options.LifecycleMatches, "lifecycle-matches", "", "unknown receipt lifecycle match count")
	flag.StringVar(&options.Fact, "fact", "", "reconciliation fact observed")
	flag.StringVar(&options.Expected, "expected", "", "reconciliation expected fact")
	flag.StringVar(&options.Conflict, "conflict", "", "reconciliation conflict boolean")
	flag.Parse()
	if bindTemplatePath != "" {
		options.BindTemplate = true
		options.Template = bindTemplatePath
	}

	if options.ProjectRoot == "" {
		fatal(fmt.Errorf("project root is required"))
	}
	report, err := testkit.RunHarness(options)
	if encodeErr := json.NewEncoder(os.Stdout).Encode(report); encodeErr != nil {
		fatal(encodeErr)
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
