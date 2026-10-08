package facade

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
)

func TestQAApprovedOperationsObserveOriginalFacts(t *testing.T) {
	f, s, base := developmentCheckFixture(t)
	files := map[string]string{
		"go.mod":          "module fixture\n\ngo 1.23\n",
		"app_test.go":     "package fixture\nimport \"testing\"\nfunc TestPass(t *testing.T) { t.Log(\"actual output\") }\nfunc TestFail(t *testing.T) { t.Fatal(\"actual failure\") }\nfunc TestSkip(t *testing.T) { t.Skip(\"unavailable\") }\n",
		"ignored_test.go": "//go:build unavailable\n\npackage fixture\nimport \"testing\"\nfunc TestIgnored(t *testing.T) {}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(f.root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(directory string, argv ...string) string {
		t.Helper()
		cmd := exec.Command("git", argv...)
		cmd.Dir = directory
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", argv, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(f.root, "add", ".")
	run(f.root, "commit", "-qm", "Executable tests")
	base = run(f.root, "rev-parse", "HEAD")
	workspace := s.NativeResources["check"]
	run(workspace.Path, "reset", "--hard", base)
	workspace.Source, workspace.Identity = base, base
	s.NativeResources[workspace.ID] = workspace
	cases := []struct {
		name                string
		operation           coverage.Operation
		ref, output, status string
		exit                int
	}{
		{name: "argv", operation: coverage.Operation{Kind: coverage.OperationArgv, Argv: []string{"/bin/sh", "-c", "printf '<%s>' \"$1\" \"$2\" \"$3\"; cat; printf x >> executions", "fixture", "", "two words", "\"quoted\""}, Stdin: "stdin\n"}, output: "<><two words><\"quoted\">stdin\n"},
		{name: "shell", operation: coverage.Operation{Kind: coverage.OperationShell, Command: "printf '%s' 'line with quotes'\nprintf '\\n'; cat", Stdin: "exact input\n"}, output: "line with quotes\nexact input\n"},
		{name: "negative", operation: coverage.Operation{Kind: coverage.OperationShell, Command: "printf rejection; exit 7"}, output: "rejection", exit: 7},
		{name: "GoPass", ref: "app_test.go::TestPass", status: "pass"},
		{name: "GoFail", ref: "app_test.go::TestFail", status: "fail", exit: 1},
		{name: "GoSkip", ref: "app_test.go::TestSkip", status: "not-executed"},
		{name: "GoNoMatch", ref: "ignored_test.go::TestIgnored", status: "not-executed"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			intent := developmentCheckIntent(t, f, s, base, item.name, "")
			var p protocol.CheckParameters
			raw, _ := json.Marshal(intent.Adapter.Params)
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			p.Check, p.SourceActionID = protocol.DevelopmentCheck{}, ""
			p.QA = &protocol.QACheckParameters{CaseID: item.name, Source: "case:" + item.name, Operation: item.operation, Directory: ".", GoTestRef: item.ref, WorkspaceRoot: workspace.Path}
			p.TimeoutNanos = int64(20 * time.Second)
			intent.Adapter.Params, _ = evidenceFields(p)
			first := observeDevelopmentCheck(t, f, s, intent, true)
			if first.ExitCode != item.exit || first.Error != "" || first.GoResult != item.status || first.Tool == "" || first.Environment == "" {
				t.Fatalf("native execution facts: %+v", first)
			}
			if item.output != "" && first.StdoutText != item.output {
				t.Fatalf("raw stdout %q, expected %q", first.StdoutText, item.output)
			}
			var request struct {
				Argv  []string
				Cwd   string
				Stdin string
			}
			if err := readCheckDocument(filepath.Join(first.Directory, "request.json"), &request); err != nil {
				t.Fatal(err)
			}
			if item.operation.Kind == coverage.OperationArgv && !reflect.DeepEqual(request.Argv, item.operation.Argv) {
				t.Fatalf("argv values changed: %q", request.Argv)
			}
			if item.operation.Kind == coverage.OperationShell && (len(request.Argv) != 3 || request.Argv[2] != item.operation.Command) {
				t.Fatalf("shell bytes changed: %q", request.Argv)
			}
			if request.Stdin != item.operation.Stdin || request.Cwd != workspace.Path {
				t.Fatalf("issued stdin/cwd changed: %+v", request)
			}
			if item.ref != "" && (!strings.Contains(first.StdoutText, `"Action":"run"`) && item.status != "not-executed" || first.GoTestName != strings.Split(item.ref, "::")[1]) {
				t.Fatalf("Go exact event/name missing: %+v", first)
			}
			second := observeDevelopmentCheck(t, f, s, intent, true)
			if first != second {
				t.Fatalf("original receipt changed across recovery: first %+v second %+v", first, second)
			}
		})
	}
	body, err := os.ReadFile(filepath.Join(workspace.Path, "executions"))
	if err != nil || string(body) != "x" {
		t.Fatalf("lost response re-executed argv operation: %q %v", body, err)
	}
}

func TestQAOperationPublicWorkflowUsesNativeReceipts(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	plan := commandPlan()
	plan.Batches[0].Checks[0].Command = "printf checked"
	d := nativeMasterAt(t, root, base, nil)
	binary := filepath.Join(d.f.admission.Target, "bin", "formal-gates")
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = "../../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build runtime: %v %s", err, out)
	}
	d.reviewPass(plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "One approved command case"}})
	d.confirmRoute(routeDecisionForGates([]string{"blackbox"}))
	d.drive()
	dev, di := d.action("engine.development.batch")
	design, qi := d.action("engine.qa.design")
	manifest := simpleManifest(qi)
	manifest.Cases[0].Operations = []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"/bin/sh", "-c", `printf '%s' "$1"; cat; printf x >> qa-executions`, "fixture", "quoted words"}, Stdin: " actual-input"}}
	manifest.Cases[0].Oracle = "stdout is quoted words actual-input"
	manifest.Cases[1].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf rejected; exit 7"}}
	manifest.Cases[1].Oracle = "stdout is rejected and actual exit is 7"
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	reviewAction, ri := d.action("engine.qa.review.rationale")
	approved := approvedReview(manifest)
	d.business(reviewAction, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &approved})
	d.drive()
	if err := os.WriteFile(filepath.Join(di.Workspace.Path, "app.go"), []byte("package main // delivered input contract\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: batchEvidence(di)})
	var action string
	var issued protocol.ValidationInput
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		d.drive()
		for id, input := range d.run.ValidationInputs {
			if input.Handler == "engine.qa.execution" {
				action, issued = id, input
			}
		}
		if action != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if action == "" {
		t.Fatalf("native operations never released comparison: next=%+v", d.run.Next)
	}
	if len(issued.ExecutionFacts) != 2 {
		t.Fatalf("comparison lacks native case facts: %+v", issued.ExecutionFacts)
	}
	for _, fact := range issued.ExecutionFacts {
		if len(fact.Operations) != 1 {
			t.Fatalf("case did not execute exactly once: %+v", fact)
		}
		op := fact.Operations[0]
		state := driverState(t, d)
		receipt, present := state.HostActionReceipts[op.ActionID]
		if !present || receipt.Status != protocol.HostActionStatusExecuted || receipt.AdapterOperation != "op.check" || receipt.PayloadDigest != op.Evidence.ParametersDigest {
			t.Fatalf("ready did not originate from admitted native receipt: %+v", receipt)
		}
		if fact.CaseID == "invalid" && (op.Evidence.ExitCode != 7 || op.Evidence.StdoutText != "rejected") {
			t.Fatalf("negative oracle lost actual facts: %+v", op.Evidence)
		}
		if fact.CaseID == "normal" && op.Evidence.StdoutText != "quoted words actual-input" {
			t.Fatalf("approved argv/stdin changed: %+v", op.Evidence)
		}
	}
	d.receipt(action)
	pending := d.run.PendingActions[action]
	workerInput := protocol.WorkerInput{Handler: "engine.qa.execution", Provider: d.host(), Pending: pending, Validation: &issued}
	compare := worker.Result{Outcome: protocol.OutcomePass, Validation: &worker.Validation{Status: "PASS", Checkpoint: true, Execution: &worker.Execution{Records: map[string]worker.Observation{"invalid": {Result: coverage.ExecutedPass, Comparison: "Actual stdout rejected and exit 7 match the declared negative oracle"}}}}}
	raw, err := json.Marshal(compare)
	if err != nil {
		t.Fatal(err)
	}
	event, err := worker.DecodeResult(workerInput, d.eventID(), raw, "verified-semantic-output")
	if err != nil {
		t.Fatal(err)
	}
	if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
		t.Fatalf("semantic checkpoint rejected: %+v", accepted)
	}
	d.reopen()
	d.drive()
	resumedAction, resumed := d.action("engine.qa.execution")
	if resumedAction != action || len(resumed.Completed) != 1 || resumed.Completed[0].CaseID != "invalid" || !reflect.DeepEqual(resumed.PendingCases, []string{"normal"}) {
		t.Fatalf("native checkpoint partition changed: %+v", resumed)
	}
	compare.Validation.Checkpoint = false
	compare.Validation.Execution.Records = map[string]worker.Observation{"normal": {Result: coverage.ExecutedPass, Comparison: "Actual stdout quoted words actual-input matches the declared oracle"}}
	workerInput.Pending, workerInput.Validation = d.run.PendingActions[action], &resumed
	raw, err = json.Marshal(compare)
	if err != nil {
		t.Fatal(err)
	}
	event, err = worker.DecodeResult(workerInput, d.eventID(), raw, "verified-final-semantic-output")
	if err != nil {
		t.Fatal(err)
	}
	if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
		t.Fatalf("final semantic comparison rejected: %+v", accepted)
	}
	state := driverState(t, d)
	target := state.Validation.Waves[0].Targets[issued.Binding.Task.String()]
	if target.Status != "PASS" || len(target.Execution.Records) != 2 {
		t.Fatalf("native facts/comparison failed to close QA: %+v", target)
	}
	executions, err := os.ReadFile(filepath.Join(issued.Workspace.Path, "qa-executions"))
	if err != nil || string(executions) != "x" {
		t.Fatalf("public checkpoint recovery reexecuted completed case: %q %v", executions, err)
	}
	before := map[string][]byte{}
	for _, fact := range issued.ExecutionFacts {
		for _, op := range fact.Operations {
			path := filepath.Join(op.Evidence.Directory, "exit.json")
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			before[path] = body
		}
	}
	d.reopen()
	d.drive()
	for path, original := range before {
		actual, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(actual, original) {
			t.Fatalf("terminal cleanup changed original native evidence: %s %v", path, err)
		}
	}
}

func TestQAGoPublicWorkflowKeepsUnmatchedCasePending(t *testing.T) {
	root := t.TempDir()
	snapshotRepoCommits(t, root)
	files := map[string]string{
		"go.mod":          "module fixture\n\ngo 1.23\n",
		"app_test.go":     "package main\nimport \"testing\"\nfunc TestFailure(t *testing.T) { t.Fatal(\"observed failure\") }\n",
		"ignored_test.go": "//go:build unavailable\n\npackage main\nimport \"testing\"\nfunc TestIgnored(t *testing.T) {}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("add", ".")
	run("commit", "-qm", "Frozen tests")
	base := run("rev-parse", "HEAD")
	plan := commandPlan()
	plan.Batches[0].Checks[0].Command = "printf checked"
	d := nativeMasterAt(t, root, base, nil)
	binary := filepath.Join(d.f.admission.Target, "bin", "formal-gates")
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = "../../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build runtime: %v %s", err, out)
	}
	d.reviewPass(plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "Frozen precise Go cases"}})
	d.confirmRoute(routeDecisionForGates([]string{"whitebox"}))
	d.drive()
	dev, input := d.action("engine.development.batch")
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main // delivered source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(dev, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
	waitAction := func(handler string) (string, protocol.ValidationInput) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			d.drive()
			for id, input := range d.run.ValidationInputs {
				if input.Handler == handler {
					return id, input
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("missing %s: next=%+v", handler, d.run.Next)
		return "", protocol.ValidationInput{}
	}
	design, authored := waitAction("engine.qa.design")
	manifest := authored.Manifest
	manifest.Cases = []coverage.AcceptanceCase{{CaseID: "failure", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestFailure", Oracle: "test passes"}, {CaseID: "unmatched", Mode: coverage.CaseWhitebox, Scenario: "BOUNDARY", TestRef: "ignored_test.go::TestIgnored", Oracle: "test executes and passes"}}
	for _, item := range manifest.Cases {
		manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: item.CaseID})
	}
	d.business(design, protocol.ValidationResult{Binding: authored.Binding, Status: "PASS", Manifest: &manifest})
	review, reviewed := waitAction("engine.qa.review.rationale")
	approved := approvedReview(manifest)
	d.business(review, protocol.ValidationResult{Binding: reviewed.Binding, Status: "PASS", Review: &approved})
	action, issued := waitAction("engine.qa.execution")
	if len(issued.ExecutionFacts) != 1 || issued.ExecutionFacts[0].CaseID != "failure" || issued.ExecutionFacts[0].Operations[0].Evidence.GoResult != "fail" || issued.ExecutionFacts[0].Operations[0].Evidence.ExitCode != 1 {
		t.Fatalf("failed/no-match native readiness: %+v", issued.ExecutionFacts)
	}
	state := driverState(t, d)
	unmatched := false
	for _, receipt := range state.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" {
			continue
		}
		var evidence protocol.CheckEvidence
		if decodeReceiptEvidence(receipt, &evidence) == nil && evidence.GoTestName == "TestIgnored" {
			unmatched = evidence.GoResult == "not-executed" && evidence.ExitCode == 0
		}
	}
	if !unmatched {
		t.Fatal("no-match actual operation receipt was lost")
	}
	d.receipt(action)
	workerInput := protocol.WorkerInput{Handler: "engine.qa.execution", Provider: d.host(), Pending: d.run.PendingActions[action], Validation: &issued}
	compare := worker.Result{Outcome: protocol.OutcomePass, Validation: &worker.Validation{Status: "FAIL", Checkpoint: true, Execution: &worker.Execution{Records: map[string]worker.Observation{"failure": {Result: coverage.ExecutedFail, Comparison: "The exact test ran and failed its assertion, contrary to the passing oracle"}}}}}
	raw, _ := json.Marshal(compare)
	event, err := worker.DecodeResult(workerInput, d.eventID(), raw, "verified-go-comparison")
	if err != nil {
		t.Fatal(err)
	}
	if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
		t.Fatalf("actual Go failure checkpoint rejected: %+v", accepted)
	}
	state = driverState(t, d)
	target := state.Validation.Waves[0].Targets[issued.Binding.Task.String()]
	if len(target.Execution.Records) != 1 || target.Execution.Records[0].Result != coverage.ExecutedFail || state.Validation.Waves[0].Status != "COLLECTING" {
		t.Fatalf("failed case/no-match partition was falsified: %+v", target)
	}
	compare.Validation.Execution.Records = map[string]worker.Observation{"unmatched": {Result: coverage.ExecutedFail, Comparison: "claimed execution"}}
	raw, _ = json.Marshal(compare)
	if _, err := worker.DecodeResult(workerInput, d.eventID(), raw, "verified-invalid-comparison"); err == nil {
		t.Fatal("no-match test became a completed FAIL row")
	}
	d.reopen()
	d.drive()
	restored := driverState(t, d)
	var before, after protocol.CheckEvidence
	before = issued.ExecutionFacts[0].Operations[0].Evidence
	if err := decodeReceiptEvidence(restored.HostActionReceipts[issued.ExecutionFacts[0].Operations[0].ActionID], &after); err != nil || before != after {
		t.Fatalf("reopen repeated or changed the actual Go execution: %+v %v", after, err)
	}
}
