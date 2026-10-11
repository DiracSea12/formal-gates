package facade

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
)

func nativeQACheckDriver(t *testing.T, configure func(*coverage.AcceptanceManifest)) (*validationDriver, coverage.AcceptanceManifest) {
	t.Helper()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := nativeMasterAt(t, root, base, nil)
	build := exec.Command("go", "build", "-o", filepath.Join(d.f.admission.Target, "bin", "formal-gates"), "./cmd/formal-gates")
	build.Dir = "../../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build runtime: %v %s", err, out)
	}
	plan := commandPlan()
	plan.Batches[0].Checks[0].Command = "printf checked"
	d.reviewPass(plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "Approved native QA inputs"}})
	d.confirmRoute(routeDecisionForGates([]string{"blackbox"}))
	d.drive()
	development, di := d.action("engine.development.batch.v2")
	design, qi := d.action("engine.qa.design.v2")
	manifest := simpleManifest(qi)
	configure(&manifest)
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &manifest})
	review, ri := d.action("engine.qa.review.rationale.v2")
	approved := approvedReview(manifest)
	for _, step := range manifest.SharedSteps {
		approved.SharedStepDecisions = append(approved.SharedStepDecisions, coverage.ReviewDecision{ID: step.ID, Status: coverage.StatusPass, Reason: "The public preparation, declared products and independence match the approved case prerequisites"})
	}
	d.business(review, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &approved})
	if err := os.WriteFile(filepath.Join(di.Workspace.Path, "app.go"), []byte("package main // candidate input\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(development, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: batchEvidence(di)})
	return d, manifest
}

func awaitQAInput(t *testing.T, d *validationDriver, ready func(protocol.ValidationInput) bool) (string, protocol.ValidationInput) {
	t.Helper()
	for {
		d.drive()
		for id, input := range d.run.ValidationInputs {
			if input.Handler == "engine.qa.execution.v2" && ready(input) {
				return id, input
			}
		}
		if d.run.Next.Operator != nil {
			t.Fatalf("native QA blocked: %+v", d.run.Next.Operator)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQAExecutionBindsCommandsThenComparesOriginalFacts(t *testing.T) {
	d, manifest := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
		m.Cases[0].Operations = nil
		m.Cases[0].Steps = "Print accepted input and capture both streams and the exit"
		m.Cases[0].Oracle = "stdout is accepted"
		m.Cases[1].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf rejected; printf detail >&2; exit 7"}}
		m.Cases[1].Oracle = "stdout is rejected, stderr is detail and exit is 7"
	})
	action, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.CommandCases) > 0 })
	if !slices.Equal(input.CommandCases, []string{"normal"}) || d.run.Validation.Sets[coverage.ReviewBlackbox].Status != "FROZEN" {
		t.Fatal("missing invocation was not approved and frozen before execution input")
	}
	raw := []byte(`{"commands":[[{"kind":"SHELL","command":"printf unexpected; printf x > \"$FORMAL_GATES_QA_OUTPUT_DIR/invocations\""}]]}`)
	issued := protocol.WorkerInput{Handler: input.Handler, Provider: d.host(), Pending: d.run.PendingActions[action], Validation: &input}
	event, err := worker.DecodeResult(issued, d.eventID(), raw, "original-command-author-output")
	if err != nil {
		t.Fatal(err)
	}
	d.receipt(action)
	if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
		t.Fatalf("execution invocation rejected: %+v", accepted)
	}
	before, _ := d.f.engine.Load()
	d.submit(event)
	after, _ := d.f.engine.Load()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("command checkpoint replay created another native intent")
	}
	d.reopen()
	resumedAction, comparison := awaitQAInput(t, d, func(in protocol.ValidationInput) bool {
		return len(in.CommandCases) == 0 && len(in.ExecutionFacts) == 2
	})
	if resumedAction != action || comparison.Binding != input.Binding || !reflect.DeepEqual(d.run.Validation.Sets[coverage.ReviewBlackbox].Manifest.Cases, manifest.Cases) {
		t.Fatal("command binding changed original QA duty or approved semantic content")
	}
	if _, err := worker.DecodeResult(protocol.WorkerInput{Handler: comparison.Handler, Provider: d.host(), Pending: d.run.PendingActions[action], Validation: &comparison}, d.eventID(), raw, "attempt-command-replacement"); err == nil {
		t.Fatal("already bound command was offered for replacement")
	}
	observations := make([]*worker.Observation, len(comparison.ExecutionFacts))
	for index, fact := range comparison.ExecutionFacts {
		operation := fact.Operations[len(fact.Operations)-1]
		evidence := operation.Evidence
		result := coverage.ExecutedPass
		if fact.CaseID == "normal" {
			if evidence.ExitCode != 0 || evidence.StdoutText != "unexpected" {
				t.Fatalf("actual missing-command execution facts: %+v", evidence)
			}
			result = coverage.ExecutedFail
		} else if evidence.ExitCode != 7 || evidence.StdoutText != "rejected" || evidence.StderrText != "detail" {
			t.Fatalf("nonzero negative case lost original facts: %+v", evidence)
		}
		observations[index] = &worker.Observation{Result: result, Comparison: "Compared the actual streams and exit with the unchanged approved oracle"}
		invoked := false
		state := driverState(t, d)
		intent := state.PendingHostActions[operation.ActionID]
		if intent.ActionID == "" {
			for _, effect := range state.ReconciledEffects {
				if effect.Intent != nil && effect.Intent.ActionID == operation.ActionID {
					intent = *effect.Intent
				}
			}
		}
		observed, done, err := d.f.checkEvidence(state, intent, false, &invoked)
		if err != nil || !done || invoked || !reflect.DeepEqual(*observed.Check, evidence) {
			t.Fatal("recovery did not observe the original admitted job", err)
		}
	}
	judgment, _ := json.Marshal(worker.ExecutionJudgments{Execution: worker.Execution{Records: observations}})
	event, err = worker.DecodeResult(protocol.WorkerInput{Handler: comparison.Handler, Provider: d.host(), Pending: d.run.PendingActions[action], Validation: &comparison}, d.eventID(), judgment, "original-oracle-comparison")
	if err != nil {
		t.Fatal(err)
	}
	if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
		t.Fatalf("semantic comparison rejected: %+v", accepted)
	}
	target := d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Targets[comparison.Binding.Task.String()]
	if target.Status != "FAIL" || target.Execution == nil {
		t.Fatal("zero process exit replaced semantic FAIL")
	}
}

func TestSharedPreparationReusesOriginalJobAndIsolatesCaseWork(t *testing.T) {
	d, _ := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
		m.SharedSteps = []coverage.SharedStep{{ID: "prepare", Procedure: "Prepare a common product once", Artifacts: []string{"prepared"}, Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: "mkdir -p prepared; printf x >> prepared/runs; printf common > prepared/product"}}}}
		for index := range m.Cases {
			m.Cases[index].SharedSteps = []string{"prepare"}
			m.Cases[index].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: `test ! -e "$FORMAL_GATES_QA_WORK_DIR/state"; printf private > "$FORMAL_GATES_QA_WORK_DIR/state"; cat prepared/product; printf '%s' "$FORMAL_GATES_QA_WORK_DIR"; printf '%s' "$TMPDIR" >&2`}}
		}
	})
	_, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	first, second := input.ExecutionFacts[0].Operations[0], input.ExecutionFacts[1].Operations[0]
	if first.Evidence.PreparationInput == "" || first.Evidence.PreparationSourceAction != "" || second.Evidence.PreparationSourceAction != first.ActionID || second.ActionID != first.ActionID || !reflect.DeepEqual(first.Parameters, second.Parameters) {
		t.Fatalf("shared use did not retain its actual original action/parameters: %+v %+v", first, second)
	}
	if first.Evidence.Directory != second.Evidence.Directory || first.Evidence.RequestDigest != second.Evidence.RequestDigest || first.Evidence.Stdout != second.Evidence.Stdout || first.Evidence.Stderr != second.Evidence.Stderr {
		t.Fatal("shared preparation rebound original process or stream identity")
	}
	data, err := os.ReadFile(filepath.Join(first.Parameters.QA.WorkspaceRoot, "prepared", "runs"))
	if err != nil || string(data) != "x" {
		t.Fatal("shared preparation executed more than once", string(data), err)
	}
	paths := []string{}
	for _, fact := range input.ExecutionFacts {
		operation := fact.Operations[1]
		if operation.Evidence.ExitCode != 0 || !strings.HasPrefix(operation.Evidence.StdoutText, "common") {
			t.Fatal("case work was contaminated or lost shared product", operation.Evidence)
		}
		path := strings.TrimPrefix(operation.Evidence.StdoutText, "common")
		paths = append(paths, path)
		data, err := os.ReadFile(filepath.Join(path, "state"))
		if err != nil || string(data) != "private" || operation.Evidence.StderrText != filepath.Join(filepath.Dir(path), "temp") {
			t.Fatal("private mutable paths were not used by the actual process", err)
		}
	}
	if paths[0] == paths[1] {
		t.Fatal("different cases shared mutable work")
	}
	d.reopen()
	_, restored := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	if !reflect.DeepEqual(input.ExecutionFacts, restored.ExecutionFacts) {
		t.Fatal("recovery changed the original shared execution reference")
	}
	data, _ = os.ReadFile(filepath.Join(first.Parameters.QA.WorkspaceRoot, "prepared", "runs"))
	if string(data) != "x" {
		t.Fatal("recovery repeated shared preparation")
	}
	s := driverState(t, d)
	p := first.Parameters
	request, err := d.f.checkRequest(s, protocol.HostActionIntent{PayloadDigest: first.Evidence.ParametersDigest}, p)
	if err != nil || request["qaPreparationInput"] != first.Evidence.PreparationInput {
		t.Fatal("declared products or private case paths changed effective preparation input", err)
	}
	source := filepath.Join(p.QA.WorkspaceRoot, "app.go")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, append(slices.Clone(original), []byte("// uncommitted input\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := d.f.qaPreparationInput(s, p, request)
	if err != nil || changed == first.Evidence.PreparationInput {
		t.Fatal("uncommitted source change reused old preparation input", err)
	}
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QA_PREPARATION_SETTING", "private-setting-value")
	changed, err = d.f.qaPreparationInput(s, p, request)
	if err != nil || changed == first.Evidence.PreparationInput || strings.Contains(changed, "private-setting-value") {
		t.Fatal("effective environment change was omitted or its value was exposed", err)
	}
	t.Setenv("QA_PREPARATION_SETTING", "")
	tool := filepath.Join(t.TempDir(), "preparation-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf first\n"), 0700); err != nil {
		t.Fatal(err)
	}
	qa := *p.QA
	step := *qa.Preparation
	step.Operations = []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{tool}}}
	qa.Preparation, qa.Operation = &step, step.Operations[0]
	p.QA = &qa
	toolInput, err := d.f.qaPreparationInput(s, p, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf second\n"), 0700); err != nil {
		t.Fatal(err)
	}
	changed, err = d.f.qaPreparationInput(s, p, request)
	if err != nil || changed == toolInput {
		t.Fatal("in-place tool update reused the original preparation", err)
	}
}

func TestIndependentPreparationActuallyRunsInEachPrivateEnvironment(t *testing.T) {
	d, _ := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
		m.SharedSteps = []coverage.SharedStep{{ID: "install", Procedure: "Exercise independent mutable installation", Independent: true, Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: `test ! -e "$FORMAL_GATES_QA_WORK_DIR/install"; printf x > "$FORMAL_GATES_QA_WORK_DIR/install"; printf '%s' "$FORMAL_GATES_QA_WORK_DIR"`}}}}
		for index := range m.Cases {
			m.Cases[index].SharedSteps = []string{"install"}
			m.Cases[index].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: `cat "$FORMAL_GATES_QA_WORK_DIR/install"`}}
		}
	})
	_, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	first, second := input.ExecutionFacts[0].Operations[0], input.ExecutionFacts[1].Operations[0]
	if first.ActionID == second.ActionID || first.Evidence.PreparationSourceAction != "" || second.Evidence.PreparationSourceAction != "" || first.Evidence.StdoutText == second.Evidence.StdoutText {
		t.Fatal("independent target was treated as reusable common preparation")
	}
	for _, fact := range input.ExecutionFacts {
		if fact.Operations[0].Evidence.ExitCode != 0 || fact.Operations[1].Evidence.StdoutText != "x" {
			t.Fatal("independent preparation or its private product was not actually consumed")
		}
	}
}

func TestQACommandCheckpointExecutesEveryBoundCase(t *testing.T) {
	d, _ := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
		for index := range m.Cases {
			m.Cases[index].Operations = nil
			m.Cases[index].PublicEntry = "/bin/sh fixture"
			m.Cases[index].Steps = "Print this case's fixed streams and exit, and append its private invocation proof"
		}
		m.Cases[0].Oracle = "stdout alpha, stderr one, exit zero"
		m.Cases[1].Oracle = "stdout beta, stderr two, exit seven"
	})
	action, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.CommandCases) == 2 })
	commands := worker.CommandBindings{}
	for _, id := range input.CommandCases {
		command := `printf alpha; printf one >&2; printf x >> "$FORMAL_GATES_QA_OUTPUT_DIR/executed"`
		if id == "invalid" {
			command = `printf beta; printf two >&2; printf y >> "$FORMAL_GATES_QA_OUTPUT_DIR/executed"; exit 7`
		}
		commands.Commands = append(commands.Commands, []coverage.Operation{{Kind: coverage.OperationShell, Command: command}})
	}
	body, _ := json.Marshal(commands)
	event, err := worker.DecodeResult(protocol.WorkerInput{Handler: input.Handler, Provider: d.host(), Pending: d.run.PendingActions[action], Validation: &input}, d.eventID(), body, "original-two-case-commands")
	if err != nil {
		t.Fatal(err)
	}
	d.receipt(action)
	if result := d.submit(event); result.Status != "ACCEPTED" {
		t.Fatal("dual command binding rejected", result)
	}
	state := driverState(t, d)
	var revision uint64
	bound := 0
	for _, intent := range state.PendingHostActions {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.check" {
			continue
		}
		var p protocol.CheckParameters
		if err := decodeNativeParams(intent, &p); err != nil {
			t.Fatal(err)
		}
		if p.QA == nil || p.Binding != input.Binding || len(p.QA.Recipe) == 0 {
			continue
		}
		bound++
		if revision != 0 && revision != intent.Revision {
			t.Fatal("one command checkpoint invented multiple state revisions")
		}
		revision = intent.Revision
	}
	d.reopen()
	_, completed := awaitQAInput(t, d, func(in protocol.ValidationInput) bool {
		return len(in.ExecutionFacts) == bound
	})
	for _, fact := range completed.ExecutionFacts {
		op := fact.Operations[0]
		raw, _ := json.Marshal(op)
		t.Logf("original executed case %s: %s", fact.CaseID, raw)
		if root := os.Getenv("QA_INPUT_REUSE_EVIDENCE"); root != "" {
			if err := os.MkdirAll(filepath.Join(root, fact.CaseID), 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"request.json", "process.json", "exit.json", "stdout.log", "stderr.log"} {
				data, err := os.ReadFile(filepath.Join(op.Evidence.Directory, name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, fact.CaseID, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if bound != 2 || len(completed.ExecutionFacts) != 2 || len(completed.CommandCases) != 0 {
		t.Fatalf("same-revision intent overwrite: bound=%d executed=%d still missing=%v", bound, len(completed.ExecutionFacts), completed.CommandCases)
	}
	seen := map[string]bool{}
	for _, fact := range completed.ExecutionFacts {
		op := fact.Operations[0]
		if seen[op.ActionID] {
			t.Fatal("different cases share an execution identity")
		}
		seen[op.ActionID] = true
		out, stderr, exit := "alpha", "one", 0
		if fact.CaseID == "invalid" {
			out, stderr, exit = "beta", "two", 7
		}
		if op.Evidence.StdoutText != out || op.Evidence.StderrText != stderr || op.Evidence.ExitCode != exit {
			t.Fatal("case lost its own actual streams or exit", op.Evidence)
		}
	}
	before, _ := d.f.engine.Load()
	d.submit(event)
	after, _ := d.f.engine.Load()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("command event replay changed completed native effects")
	}
	d.reopen()
	_, recovered := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	if !reflect.DeepEqual(completed.ExecutionFacts, recovered.ExecutionFacts) {
		t.Fatal("recovery changed either original execution")
	}
	for _, fact := range recovered.ExecutionFacts {
		var request struct {
			Environment map[string]string `json:"environment"`
		}
		if err := readCheckDocument(filepath.Join(fact.Operations[0].Evidence.Directory, "request.json"), &request); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(request.Environment["FORMAL_GATES_QA_OUTPUT_DIR"], "executed"))
		if err != nil || len(data) != 1 {
			t.Fatal("replay or recovery repeated a case command", string(data), err)
		}
	}
}
