package facade

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
)

func nativeQACheckDriver(t *testing.T, configure func(*coverage.AcceptanceManifest)) (*validationDriver, coverage.AcceptanceManifest) {
	t.Helper()
	return nativeQACheckDriverAt(t, t.TempDir(), configure)
}

func nativeQACheckDriverAt(t *testing.T, root string, configure func(*coverage.AcceptanceManifest)) (*validationDriver, coverage.AcceptanceManifest) {
	t.Helper()
	base := snapshotRepoCommits(t, root)[0]
	d := nativeMasterAt(t, root, base, nil)
	plan := commandPlan()
	plan.Batches[0].Checks[0].Command = nativeFixtureCommand("printf checked", "[Console]::Write('checked')")
	d.reviewPass(plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "Approved native QA inputs"}})
	d.confirmRoute(routeDecisionForGates([]string{"blackbox"}))
	d.drive()
	development, di := d.action("engine.development.batch.v6")
	design, qi := d.action("engine.qa.design.v6")
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
			if input.Handler == "engine.qa.execution.v4" && ready(input) {
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
	t.Parallel()
	d, manifest := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
		m.Cases[0].Operations = nil
		m.Cases[0].Steps = "Print accepted input and capture both streams and the exit"
		m.Cases[0].Oracle = "stdout is accepted"
		m.Cases[1].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: nativeFixtureCommand("printf rejected; printf detail >&2; exit 7", "[Console]::Write('rejected'); [Console]::Error.Write('detail'); exit 7")}}
		m.Cases[1].Oracle = "stdout is rejected, stderr is detail and exit is 7"
	})
	action, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.CommandCases) > 0 })
	if !slices.Equal(input.CommandCases, []string{"normal"}) || d.run.Validation.Sets[coverage.ReviewBlackbox].Status != "FROZEN" {
		t.Fatal("missing invocation was not approved and frozen before execution input")
	}
	raw, err := json.Marshal(map[string]any{"commands": [][]coverage.Operation{{{Kind: coverage.OperationShell, Command: nativeFixtureCommand(`printf unexpected; printf x > "$FORMAL_GATES_QA_OUTPUT_DIR/invocations"`, `[Console]::Write('unexpected'); [IO.File]::WriteAllText(($env:FORMAL_GATES_QA_OUTPUT_DIR+'/invocations'),'x')`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	issued := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: input.Handler, Provider: d.host(), Pending: d.run.PendingActions[action], Validation: &input}
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
	if _, err := worker.DecodeResult(protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: comparison.Handler, Provider: d.host(), Pending: d.run.PendingActions[action], Validation: &comparison}, d.eventID(), raw, "attempt-command-replacement"); err == nil {
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
	event, err = worker.DecodeResult(protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: comparison.Handler, Provider: d.host(), Pending: d.run.PendingActions[action], Validation: &comparison}, d.eventID(), judgment, "original-oracle-comparison")
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

func buildPreparationExecutable(t *testing.T, directory, source string) string {
	t.Helper()
	file := filepath.Join(directory, "prepare.go")
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(directory, "prepare")
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	build := exec.Command("go", "build", "-o", tool, file)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build preparation executable: %v %s", err, out)
	}
	return tool
}

func TestShellPreparationExecutesEachCaseAndIsolatesCaseWork(t *testing.T) {
	t.Parallel()
	d, _ := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
		m.SharedSteps = []coverage.SharedStep{{ID: "prepare", Procedure: "Prepare a common product once", Artifacts: []string{"prepared"}, Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: nativeFixtureCommand("mkdir -p prepared; printf x >> prepared/runs; printf common > prepared/product", "[IO.Directory]::CreateDirectory('prepared') > $null; [IO.File]::AppendAllText('prepared/runs','x'); [IO.File]::WriteAllText('prepared/product','common')")}}}}
		for index := range m.Cases {
			m.Cases[index].SharedSteps = []string{"prepare"}
			m.Cases[index].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: nativeFixtureCommand(`test ! -e "$FORMAL_GATES_QA_WORK_DIR/state"; printf private > "$FORMAL_GATES_QA_WORK_DIR/state"; cat prepared/product; printf '%s' "$FORMAL_GATES_QA_WORK_DIR"; printf '%s' "$TMPDIR" >&2`, `if ([IO.File]::Exists($env:FORMAL_GATES_QA_WORK_DIR+'/state')) { exit 1 }; [IO.File]::WriteAllText(($env:FORMAL_GATES_QA_WORK_DIR+'/state'),'private'); [Console]::Write([IO.File]::ReadAllText('prepared/product')); [Console]::Write($env:FORMAL_GATES_QA_WORK_DIR); [Console]::Error.Write($env:TMPDIR)`)}}
		}
	})
	_, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	first, second := input.ExecutionFacts[0].Operations[0], input.ExecutionFacts[1].Operations[0]
	if first.Evidence.PreparationInput != "" || second.Evidence.PreparationInput != "" || first.Evidence.PreparationSourceAction != "" || second.Evidence.PreparationSourceAction != "" || second.ActionID == first.ActionID {
		t.Fatalf("SHELL preparation did not retain each actual execution: %+v %+v", first, second)
	}
	if first.Evidence.Directory == second.Evidence.Directory || first.Evidence.RequestDigest == second.Evidence.RequestDigest {
		t.Fatal("SHELL preparation reused another process identity")
	}
	data, err := os.ReadFile(filepath.Join(first.Parameters.QA.WorkspaceRoot, "prepared", "runs"))
	if err != nil || string(data) != "xx" {
		t.Fatal("SHELL preparation did not execute once per case", string(data), err)
	}
	progress, err := d.f.ProgressSince(0, nil)
	if err != nil || progress.PreparationExecuted != 2 || progress.PreparationReused != 0 {
		t.Fatalf("actual SHELL preparation count: %+v %v", progress, err)
	}
	assertCaseWorkIsolationAndSharedExecutionRecovery(t, d, input)
	data, _ = os.ReadFile(filepath.Join(first.Parameters.QA.WorkspaceRoot, "prepared", "runs"))
	if string(data) != "xx" {
		t.Fatal("recovery repeated shared preparation")
	}
}

func TestSharedPreparationReusesOriginalARGVJobAndIsolatesCaseWork(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	tool := buildPreparationExecutable(t, t.TempDir(), `package main
import "os"
func main() {
	if err := os.MkdirAll("prepared", 0700); err != nil { panic(err) }
	f, err := os.OpenFile("prepared/runs", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil { panic(err) }
	if _, err := f.WriteString("x"); err != nil { panic(err) }
	if err := f.Close(); err != nil { panic(err) }
	if err := os.WriteFile("prepared/product", []byte("common"), 0600); err != nil { panic(err) }
}`)
	d, _ := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
		m.SharedSteps = []coverage.SharedStep{{ID: "prepare", Procedure: "Prepare a common product once", Artifacts: []string{"prepared"}, Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{tool}}}}}
		for index := range m.Cases {
			m.Cases[index].SharedSteps = []string{"prepare"}
			m.Cases[index].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: nativeFixtureCommand(`test ! -e "$FORMAL_GATES_QA_WORK_DIR/state"; printf private > "$FORMAL_GATES_QA_WORK_DIR/state"; cat prepared/product; printf '%s' "$FORMAL_GATES_QA_WORK_DIR"; printf '%s' "$TMPDIR" >&2`, `if ([IO.File]::Exists($env:FORMAL_GATES_QA_WORK_DIR+'/state')) { exit 1 }; [IO.File]::WriteAllText(($env:FORMAL_GATES_QA_WORK_DIR+'/state'),'private'); [Console]::Write([IO.File]::ReadAllText('prepared/product')); [Console]::Write($env:FORMAL_GATES_QA_WORK_DIR); [Console]::Error.Write($env:TMPDIR)`)}}
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
	progress, err := d.f.ProgressSince(0, nil)
	if err != nil || progress.PreparationExecuted != 1 || progress.PreparationReused != 1 {
		t.Fatalf("actual shared preparation count: %+v %v", progress, err)
	}
	assertCaseWorkIsolationAndSharedExecutionRecovery(t, d, input)
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
	qa := *p.QA
	step := *qa.Preparation
	step.Operations = []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{tool}}}
	qa.Preparation, qa.Operation = &step, step.Operations[0]
	p.QA = &qa
	toolInput, err := d.f.qaPreparationInput(s, p, request)
	if err != nil {
		t.Fatal(err)
	}
	buildPreparationExecutable(t, filepath.Dir(tool), "package main\nfunc main() { println(\"changed\") }\n")
	changed, err = d.f.qaPreparationInput(s, p, request)
	if err != nil || changed == toolInput {
		t.Fatal("in-place tool update reused the original preparation", err)
	}
}

func assertCaseWorkIsolationAndSharedExecutionRecovery(t *testing.T, d *validationDriver, input protocol.ValidationInput) {
	t.Helper()
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
}

func TestIndependentPreparationActuallyRunsInEachPrivateEnvironment(t *testing.T) {
	t.Parallel()
	d, _ := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
		m.SharedSteps = []coverage.SharedStep{{ID: "install", Procedure: "Exercise independent mutable installation", Independent: true, Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: nativeFixtureCommand(`test ! -e "$FORMAL_GATES_QA_WORK_DIR/install"; printf x > "$FORMAL_GATES_QA_WORK_DIR/install"; printf '%s' "$FORMAL_GATES_QA_WORK_DIR"`, `if ([IO.File]::Exists($env:FORMAL_GATES_QA_WORK_DIR+'/install')) { exit 1 }; [IO.File]::WriteAllText(($env:FORMAL_GATES_QA_WORK_DIR+'/install'),'x'); [Console]::Write($env:FORMAL_GATES_QA_WORK_DIR)`)}}}}
		for index := range m.Cases {
			m.Cases[index].SharedSteps = []string{"install"}
			m.Cases[index].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: nativeFixtureCommand(`cat "$FORMAL_GATES_QA_WORK_DIR/install"`, `[Console]::Write([IO.File]::ReadAllText($env:FORMAL_GATES_QA_WORK_DIR+'/install'))`)}}
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
	progress, err := d.f.ProgressSince(0, nil)
	if err != nil || progress.PreparationExecuted != 2 || progress.PreparationReused != 0 {
		t.Fatalf("actual independent preparation count: %+v %v", progress, err)
	}
}

func TestSharedPreparationReusesCompleteRecipeOrRebuildsAllProducts(t *testing.T) {
	t.Parallel()
	first := buildPreparationExecutable(t, t.TempDir(), `package main
import "os"
func main() {
	if err := os.MkdirAll("prepared", 0700); err != nil { panic(err) }
	f, err := os.OpenFile("prepared/runs", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil { panic(err) }
	if _, err := f.WriteString("x"); err != nil { panic(err) }
	if err := f.Close(); err != nil { panic(err) }
	if err := os.WriteFile("prepared/stage", []byte("common"), 0600); err != nil { panic(err) }
}`)
	second := buildPreparationExecutable(t, t.TempDir(), `package main
import "os"
func main() {
	body, err := os.ReadFile("prepared/stage")
	if err != nil { panic(err) }
	if err := os.WriteFile("prepared/intermediate", body, 0600); err != nil { panic(err) }
}`)
	last := buildPreparationExecutable(t, t.TempDir(), `package main
import "os"
func main() {
	body, err := os.ReadFile("prepared/intermediate")
	if err != nil { panic(err) }
	if err := os.WriteFile("prepared/product", body, 0600); err != nil { panic(err) }
}`)
	for _, scenario := range []struct{ name, cleanup, windowsCleanup string }{
		{"unchanged", "", ""},
		{"removed", "; rm -f prepared/stage prepared/intermediate prepared/product", "; [IO.File]::Delete('prepared/stage'); [IO.File]::Delete('prepared/intermediate'); [IO.File]::Delete('prepared/product')"},
		{"changed", "; printf changed > prepared/intermediate", "; [IO.File]::WriteAllText('prepared/intermediate','changed')"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			d, _ := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
				m.SharedSteps = []coverage.SharedStep{{ID: "prepare", Procedure: "Create stage, intermediate and common product", Artifacts: []string{"prepared"}, Operations: []coverage.Operation{
					{Kind: coverage.OperationArgv, Argv: []string{first}},
					{Kind: coverage.OperationArgv, Argv: []string{second}},
					{Kind: coverage.OperationArgv, Argv: []string{last}},
				}}}
				for index := range m.Cases {
					m.Cases[index].SharedSteps = []string{"prepare"}
					m.Cases[index].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: nativeFixtureCommand("cat prepared/product"+scenario.cleanup, "[Console]::Write([IO.File]::ReadAllText('prepared/product'))"+scenario.windowsCleanup)}}
				}
			})
			_, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
			for _, fact := range input.ExecutionFacts {
				if len(fact.Operations) != 4 || fact.Operations[3].Evidence.StdoutText != "common" || fact.Operations[3].Evidence.StderrText != "" {
					t.Fatalf("case did not receive all prepared products: %+v", fact)
				}
			}
			for ordinal := range 3 {
				original, next := input.ExecutionFacts[0].Operations[ordinal], input.ExecutionFacts[1].Operations[ordinal]
				if original.Evidence.ExitCode != 0 || next.Evidence.ExitCode != 0 || next.Parameters.QA.Operation.Argv[0] != []string{first, second, last}[ordinal] {
					t.Fatalf("actual recipe operation differs: %+v %+v", original, next)
				}
				if scenario.cleanup == "" {
					if next.ActionID != original.ActionID || next.Evidence.PreparationSourceAction != original.ActionID || next.Evidence.Directory != original.Evidence.Directory || next.Evidence.RequestDigest != original.Evidence.RequestDigest || next.Evidence.Stdout != original.Evidence.Stdout || next.Evidence.Stderr != original.Evidence.Stderr {
						t.Fatalf("complete preparation did not retain its original execution: %+v %+v", original, next)
					}
				} else if next.ActionID == original.ActionID || next.Evidence.PreparationSourceAction != "" {
					t.Fatalf("changed preparation did not execute the whole recipe: %+v %+v", original, next)
				}
			}
			wantRuns, executed, reused := "x", 3, 3
			if scenario.cleanup != "" {
				wantRuns, executed, reused = "xx", 6, 0
			}
			root := input.ExecutionFacts[0].Operations[0].Parameters.QA.WorkspaceRoot
			runs, err := os.ReadFile(filepath.Join(root, "prepared", "runs"))
			if err != nil || string(runs) != wantRuns {
				t.Fatalf("preparation actually ran %q, expected %q: %v", runs, wantRuns, err)
			}
			progress, err := d.f.ProgressSince(0, nil)
			if err != nil || progress.PreparationExecuted != executed || progress.PreparationReused != reused {
				t.Fatalf("complete recipe counts differ: %+v %v", progress, err)
			}
			d.reopen()
			_, restored := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
			runs, err = os.ReadFile(filepath.Join(root, "prepared", "runs"))
			if err != nil || string(runs) != wantRuns || !reflect.DeepEqual(input.ExecutionFacts, restored.ExecutionFacts) {
				t.Fatal("recovery repeated or rebound completed preparation", string(runs), err)
			}
		})
	}
}

func TestPreparationToolFromEnvironmentExecutesAfterToolChanges(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	assertPreparationExecutesChangedExternalInput(t, "SHELL")
}

func TestPreparationInterpreterScriptExecutesAfterScriptChanges(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	assertPreparationExecutesChangedExternalInput(t, "SCRIPT_ARGUMENT")
}

func TestPreparationScriptExecutableExecutesAfterEnvironmentToolChanges(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	assertPreparationExecutesChangedExternalInput(t, "SCRIPT_EXECUTABLE")
}

func assertPreparationExecutesChangedExternalInput(t *testing.T, invocation string) {
	t.Helper()
	directory := t.TempDir()
	tool := filepath.Join(directory, "prepare.sh")
	ready, release := filepath.Join(directory, "ready"), filepath.Join(directory, "release")
	t.Setenv("QA_PREPARATION_READY", ready)
	t.Setenv("QA_PREPARATION_RELEASE", release)
	old := "#!/bin/sh\nmkdir -p prepared; printf x >> prepared/runs; printf old > prepared/product; printf old; printf ready > \"$QA_PREPARATION_READY\"; while test ! -e \"$QA_PREPARATION_RELEASE\"; do sleep 0.01; done\n"
	updated := "#!/bin/sh\nmkdir -p prepared; printf x >> prepared/runs; printf new > prepared/product; printf new\n"
	interpreter := []string{"/bin/sh"}
	if runtime.GOOS == "windows" {
		tool = filepath.Join(directory, "prepare.ps1")
		old = "[IO.Directory]::CreateDirectory('prepared') > $null; [IO.File]::AppendAllText('prepared/runs','x'); [IO.File]::WriteAllText('prepared/product','old'); [Console]::Write('old'); [IO.File]::WriteAllText($env:QA_PREPARATION_READY,'ready'); while (-not [IO.File]::Exists($env:QA_PREPARATION_RELEASE)) { Start-Sleep -Milliseconds 10 }\n"
		updated = "[IO.Directory]::CreateDirectory('prepared') > $null; [IO.File]::AppendAllText('prepared/runs','x'); [IO.File]::WriteAllText('prepared/product','new'); [Console]::Write('new')\n"
		interpreter = []string{"powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File"}
	}
	t.Setenv("QA_PREPARATION_TOOL", tool)
	if err := os.WriteFile(tool, []byte(old), 0700); err != nil {
		t.Fatal(err)
	}
	operation := coverage.Operation{Kind: coverage.OperationArgv, Argv: append(append([]string{}, interpreter...), tool)}
	switch invocation {
	case "SHELL":
		operation = coverage.Operation{Kind: coverage.OperationShell, Command: nativeFixtureCommand(`"$QA_PREPARATION_TOOL"`, `& powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $env:QA_PREPARATION_TOOL`)}
	case "SCRIPT_EXECUTABLE":
		script := filepath.Join(directory, "invoke.sh")
		body := "#!/bin/sh\n\"$QA_PREPARATION_TOOL\"\n"
		if runtime.GOOS == "windows" {
			script = filepath.Join(directory, "invoke.cmd")
			body = "@powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File \"%QA_PREPARATION_TOOL%\"\r\n"
		}
		if err := os.WriteFile(script, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		operation = coverage.Operation{Kind: coverage.OperationArgv, Argv: []string{script}}
		if runtime.GOOS == "windows" {
			operation.Argv = coverage.ShellCommand(`"` + script + `"`)
		}
	}
	d, _ := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
		m.SharedSteps = []coverage.SharedStep{{ID: "prepare", Procedure: "Prepare the product using the configured external tool", Artifacts: []string{"prepared"}, Operations: []coverage.Operation{operation}}}
		for index := range m.Cases {
			m.Cases[index].SharedSteps = []string{"prepare"}
			m.Cases[index].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: nativeFixtureCommand("cat prepared/product", "[Console]::Write([IO.File]::ReadAllText('prepared/product'))")}}
		}
	})
	changed := make(chan error, 1)
	go func() {
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			} else if !os.IsNotExist(err) {
				changed <- err
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := os.WriteFile(tool, []byte(updated), 0700); err != nil {
			changed <- err
			return
		}
		changed <- os.WriteFile(release, []byte("continue"), 0600)
	}()
	_, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, fact := range input.ExecutionFacts {
		preparation := fact.Operations[0]
		if preparation.Evidence.PreparationInput != "" || preparation.Evidence.PreparationSourceAction != "" || preparation.Evidence.ExitCode != 0 || seen[preparation.ActionID] {
			t.Fatalf("external inputs reused an old job: %+v", preparation)
		}
		seen[preparation.ActionID] = true
	}
	first, second := input.ExecutionFacts[0].Operations[0], input.ExecutionFacts[1].Operations[0]
	if first.Evidence.StdoutText != "old" || second.Evidence.StdoutText != "new" {
		t.Fatalf("changed external contents were not actually executed: %q %q", first.Evidence.StdoutText, second.Evidence.StdoutText)
	}
	root := first.Parameters.QA.WorkspaceRoot
	product, err := os.ReadFile(filepath.Join(root, "prepared", "product"))
	if err != nil || string(product) != "new" {
		t.Fatal("changed external input did not produce the current product", string(product), err)
	}
	runs, err := os.ReadFile(filepath.Join(root, "prepared", "runs"))
	if err != nil || string(runs) != "xx" {
		t.Fatal("each preparation was not executed exactly once", string(runs), err)
	}
	if input.ExecutionFacts[1].Operations[1].Evidence.StdoutText != "new" {
		t.Fatal("case did not consume the changed preparation product")
	}
	d.reopen()
	_, restored := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	if !reflect.DeepEqual(input.ExecutionFacts, restored.ExecutionFacts) {
		t.Fatal("recovery changed original non-reusable preparation receipts")
	}
	runs, _ = os.ReadFile(filepath.Join(root, "prepared", "runs"))
	if string(runs) != "xx" {
		t.Fatal("recovery repeated a completed preparation", string(runs))
	}
}

func TestQACommandCheckpointExecutesEveryBoundCase(t *testing.T) {
	t.Parallel()
	d, _ := nativeQACheckDriver(t, func(m *coverage.AcceptanceManifest) {
		for index := range m.Cases {
			m.Cases[index].Operations = nil
			m.Cases[index].PublicEntry = "platform command fixture"
			m.Cases[index].Steps = "Print this case's fixed streams and exit, and append its private invocation proof"
		}
		m.Cases[0].Oracle = "stdout alpha, stderr one, exit zero"
		m.Cases[1].Oracle = "stdout beta, stderr two, exit seven"
	})
	action, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.CommandCases) == 2 })
	commands := [][]coverage.Operation{}
	for _, id := range input.CommandCases {
		command := nativeFixtureCommand(`printf alpha; printf one >&2; printf x >> "$FORMAL_GATES_QA_OUTPUT_DIR/executed"`, `[Console]::Write('alpha'); [Console]::Error.Write('one'); [IO.File]::AppendAllText(($env:FORMAL_GATES_QA_OUTPUT_DIR+'/executed'),'x')`)
		if id == "invalid" {
			command = nativeFixtureCommand(`printf beta; printf two >&2; printf y >> "$FORMAL_GATES_QA_OUTPUT_DIR/executed"; exit 7`, `[Console]::Write('beta'); [Console]::Error.Write('two'); [IO.File]::AppendAllText(($env:FORMAL_GATES_QA_OUTPUT_DIR+'/executed'),'y'); exit 7`)
		}
		commands = append(commands, []coverage.Operation{{Kind: coverage.OperationShell, Command: command}})
	}
	body, _ := json.Marshal(map[string]any{"commands": commands})
	event, err := worker.DecodeResult(protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: input.Handler, Provider: d.host(), Pending: d.run.PendingActions[action], Validation: &input}, d.eventID(), body, "original-two-case-commands")
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

func TestWhiteboxRecipeFreezesTestMaterialAndRetainsNativeExecution(t *testing.T) {
	t.Parallel()
	python := nativePython(t)
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := nativeMasterAt(t, root, base, nil)
	d.reviewPass(commandPlan())
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "One command behavior"}})
	d.confirmRoute(routeDecisionForGates([]string{"whitebox"}))
	d.drive()
	development, di := d.action("engine.development.batch.v6")
	d.business(development, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: batchEvidence(di)})
	d.drive()
	design, qi := d.action("engine.qa.design.v6")
	body := "import os\nimport subprocess\nimport unittest\ncommand = [os.environ['COMSPEC'], '/D', '/S', '/C', r'.\\app.cmd'] if os.name == 'nt' else ['./app']\nclass Behavior(unittest.TestCase):\n    def test_normal(self):\n        self.assertEqual(subprocess.run(command + ['run', 'valid']).returncode, 0)\n    def test_invalid(self):\n        self.assertNotEqual(subprocess.run(command + ['run', 'invalid']).returncode, 0)\n"
	if err := os.WriteFile(filepath.Join(qi.Workspace.Path, "test_recipe.py"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := qi.Manifest
	for i, name := range []string{"test_normal", "test_invalid"} {
		id, scenario := "normal", "SUCCESS"
		if i == 1 {
			id, scenario = "invalid", "FAILURE"
		}
		manifest.Cases = append(manifest.Cases, coverage.AcceptanceCase{CaseID: id, Mode: coverage.CaseWhitebox, Scenario: scenario, Files: []string{"test_recipe.py"}, Oracle: "Python unittest runs one test, passes its assertion and reports OK", Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{python, "-m", "unittest", "test_recipe.Behavior." + name}}}})
		manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: id})
	}
	paths, err := vcs.New(qi.Workspace.Path, nil).ChangedPaths()
	if err != nil {
		t.Fatal(err)
	}
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: paths}})
	d.drive()
	review, ri := d.action("engine.qa.review.rationale.v2")
	approved := approvedReview(manifest)
	d.business(review, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &approved})
	_, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	if len(input.Manifest.Cases) != 2 || d.run.Validation.Sets[coverage.ReviewWhitebox].Status != "FROZEN" {
		t.Fatal("whitebox recipe did not freeze its complete test set")
	}
	candidate := d.run.Validation.Candidate.Identity
	actual, err := (exec.Command("git", "-C", root, "show", candidate+":test_recipe.py")).Output()
	if err != nil || string(actual) != body {
		t.Fatal("candidate omitted or changed approved Python test material", err)
	}
	for _, fact := range input.ExecutionFacts {
		if len(fact.Operations) != 1 {
			t.Fatal("recipe invoked extra operations")
		}
		evidence := fact.Operations[0].Evidence
		if evidence.ExitCode != 0 || evidence.GoResult != "" || !strings.Contains(evidence.StderrText, "Ran 1 test") || !strings.Contains(evidence.StderrText, "OK") {
			t.Fatal("Python test did not actually run and pass", evidence)
		}
	}
	d.reopen()
	_, restored := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	if !reflect.DeepEqual(input.ExecutionFacts, restored.ExecutionFacts) {
		t.Fatal("recovery changed or repeated the original recipe execution")
	}
}

// The shared invocation checkpoint binds only missing invocations before native
// execution, and same-source commands run there in order without folding.
func TestSharedInvocationCheckpointExecutesApprovedPrefixAndDeclaredCase(t *testing.T) {
	t.Parallel()
	readProducts := nativeFixtureCommand("cat prepared-input prepared-marker", "[Console]::Write([IO.File]::ReadAllText('prepared-input')+[IO.File]::ReadAllText('prepared-marker'))")
	writeMarker := nativeFixtureCommand("printf checked > prepared-marker", "[IO.File]::WriteAllText('prepared-marker','checked')")
	writeReady := nativeFixtureCommand("printf ready > prepared-input", "[IO.File]::WriteAllText('prepared-input','ready')")
	for _, scenario := range []struct{ name, first, second string }{
		{"same-source duplicate commands are not folded", writeReady, writeReady},
		{"same-source commands run in order", nativeFixtureCommand("printf re > prepared-input", "[IO.File]::WriteAllText('prepared-input','re')"), nativeFixtureCommand("sleep 0.1; printf ady >> prepared-input", "Start-Sleep -Milliseconds 100; [IO.File]::AppendAllText('prepared-input','ady')")},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			d, manifest := nativeQACheckDriverAt(t, testkit.ShortRoot(t), func(m *coverage.AcceptanceManifest) {
				m.SharedSteps = []coverage.SharedStep{{ID: "prepare-input", Procedure: "Create prepared-input with exact content ready", Artifacts: []string{"prepared-input"}}, {ID: "prepare-marker", Procedure: "Create prepared-marker with content checked", Artifacts: []string{"prepared-marker"}}}
				for i := range m.Cases {
					m.Cases[i].SharedSteps = []string{"prepare-input", "prepare-marker"}
					m.Cases[i].PublicEntry = "cat prepared-input prepared-marker"
					m.Cases[i].Preconditions = "shared input and marker are present"
					m.Cases[i].Steps = "read the input and marker"
					m.Cases[i].Oracle = "stdout is readychecked and exit zero"
				}
				m.Cases[0].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: readProducts}}
				m.Cases[1].Operations = nil
			})
			action, input := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.CommandCases) > 0 })
			if !reflect.DeepEqual(input.CommandSources, []string{"shared:prepare-input", "shared:prepare-marker", "case:invalid"}) {
				t.Fatalf("wrong missing source identities: %+v", input)
			}
			raw, err := json.Marshal(map[string]any{"commands": [][]coverage.Operation{
				{{Kind: coverage.OperationShell, Command: scenario.first}, {Kind: coverage.OperationShell, Command: scenario.second}},
				{{Kind: coverage.OperationShell, Command: writeMarker}},
				{{Kind: coverage.OperationShell, Command: readProducts}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			issued := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: input.Handler, Provider: d.host(), Pending: d.run.PendingActions[action], Validation: &input}
			event, err := worker.DecodeResult(issued, d.eventID(), raw, "actual-source-bound-invocations")
			if err != nil {
				t.Fatal(err)
			}
			if event.Result.Validation.QACommands[0].Source != "shared:prepare-input" || event.Result.Validation.QACommands[2].Source != "" {
				t.Fatal("program did not bind missing invocation source")
			}
			d.receipt(action)
			if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
				t.Fatalf("checkpoint rejected %+v", accepted)
			}
			_, done := awaitQAInput(t, d, func(in protocol.ValidationInput) bool {
				return len(in.CommandCases) == 0 && len(in.ExecutionFacts) == 2
			})
			if !reflect.DeepEqual(d.run.Validation.Sets[coverage.ReviewBlackbox].Manifest.SharedSteps, manifest.SharedSteps) {
				t.Fatal("frozen shared procedures were changed")
			}
			for _, fact := range done.ExecutionFacts {
				if len(fact.Operations) != 4 {
					t.Fatalf("prefix incomplete %+v", fact)
				}
				for i, source := range []string{"shared:prepare-input", "shared:prepare-input", "shared:prepare-marker", "case:" + fact.CaseID} {
					p := fact.Operations[i].Parameters
					if p.QA.Source != source || p.QA.Ordinal != i {
						t.Fatalf("source/order lost %+v", p.QA)
					}
				}
				last := fact.Operations[3]
				if last.Evidence.ExitCode != 0 || last.Evidence.StdoutText != "readychecked" || last.Evidence.StderrText != "" {
					t.Fatalf("actual native invocation did not follow preparation %+v", last.Evidence)
				}
				if fact.CaseID == "normal" && !reflect.DeepEqual(last.Parameters.QA.Operation, manifest.Cases[0].Operations[0]) {
					t.Fatal("known case command was rewritten")
				}
			}
		})
	}
}
