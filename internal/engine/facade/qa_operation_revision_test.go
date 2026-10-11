package facade

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
)

func TestRevisedQAOperationsUseOriginalNativeReceiptPipeline(t *testing.T) {
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
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "Approved native operations"}})
	d.confirmRoute(routeDecisionForGates([]string{"blackbox"}))
	development, di := d.action("engine.development.batch.v3")
	design, qi := d.action("engine.qa.design.v2")
	manifest := simpleManifest(qi)
	original := []coverage.Operation{
		{Kind: coverage.OperationArgv, Argv: []string{"/bin/sh", "-c", `printf '<%s>' "$1" "$2"; cat; printf a >> qa-executions`, "fixture", "", "two words"}, Stdin: " original input\n"},
		{Kind: coverage.OperationShell, Command: "printf '%s' 'shell original'; cat; printf b >> qa-executions", Stdin: " shell input\n"},
	}
	manifest.Cases[0].Operations = original
	manifest.Cases[0].Oracle = "The approved operations and final new observation produce their declared streams"
	manifest.Cases[1].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf rejected; exit 7"}}
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &manifest})
	review, ri := d.action("engine.qa.review.rationale.v2")
	rejected := approvedReview(manifest)
	rejected.CaseDecisions[0].Status = coverage.StatusFail
	rejected.PointDecisions[0].Status = coverage.StatusFail
	rejected.SourceDecisions[0].Status = coverage.StatusFail
	rejected.SetStatus = coverage.StatusFail
	d.business(review, protocol.ValidationResult{Binding: ri.Binding, Status: "FAIL", Review: &rejected})
	design, qi = d.action("engine.qa.design.v2")
	workerInput := protocol.WorkerInput{Handler: qi.Handler, Provider: d.host(), Pending: d.run.PendingActions[design], Validation: &qi}
	raw := []byte(`{"cases":[{"case":1,"operations":[{"reuse":1,"stdin":" changed input\n"},{"reuse":2},{"kind":"SHELL","command":"printf new-observation; printf c >> qa-executions"}]}],"coverage":[null]}`)
	event, err := worker.DecodeResultWithPaths(workerInput, d.eventID(), raw, "original-revision-output", []string{})
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]coverage.Operation{}, original...), coverage.Operation{Kind: coverage.OperationShell, Command: "printf new-observation; printf c >> qa-executions"})
	want[0].Stdin = " changed input\n"
	if !reflect.DeepEqual(event.Result.Validation.Manifest.Cases[0].Operations, want) || !reflect.DeepEqual(qi.Manifest.Cases[0].Operations, original) {
		t.Fatal("revision changed unmodified invocation bytes or its original manifest")
	}
	d.receipt(design)
	if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
		t.Fatalf("revised operations failed original worker admission: %+v", accepted)
	}
	before := driverState(t, d)
	d.submit(event)
	if after := driverState(t, d); !reflect.DeepEqual(before, after) {
		t.Fatal("replaying accepted design changed state")
	}
	d.reopen()
	review, ri = d.action("engine.qa.review.rationale.v2")
	approved := approvedReview(ri.Manifest)
	d.business(review, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &approved})
	if err := os.WriteFile(filepath.Join(di.Workspace.Path, "app.go"), []byte("package main // candidate input\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(development, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: batchEvidence(di)})
	_, comparison := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	var operations []protocol.ExecutionOperationFact
	for _, fact := range comparison.ExecutionFacts {
		if fact.CaseID == "normal" {
			operations = fact.Operations
		}
	}
	if len(operations) != len(want) {
		t.Fatal("revised sequence did not reach every original native operation", operations)
	}
	state := driverState(t, d)
	for index, operation := range operations {
		if operation.Parameters.QA == nil || !reflect.DeepEqual(operation.Parameters.QA.Operation, want[index]) || operation.Parameters.QA.Ordinal != index {
			t.Fatal("execution changed revised order or approved invocation values", operation)
		}
		receipt := state.HostActionReceipts[operation.ActionID]
		if receipt.Status != protocol.HostActionStatusExecuted || receipt.AdapterOperation != "op.check" || receipt.PayloadDigest != operation.Evidence.ParametersDigest {
			t.Fatal("revised operation bypassed original native receipt admission", receipt)
		}
		var request struct {
			Argv  []string
			Stdin string
		}
		if err := readCheckDocument(filepath.Join(operation.Evidence.Directory, "request.json"), &request); err != nil {
			t.Fatal(err)
		}
		if request.Stdin != want[index].Stdin || want[index].Kind == coverage.OperationArgv && !reflect.DeepEqual(request.Argv, want[index].Argv) || want[index].Kind == coverage.OperationShell && (len(request.Argv) != 3 || request.Argv[2] != want[index].Command) {
			t.Fatal("persistent native request changed original invocation bytes", request)
		}
		if operation.Evidence.ExitCode != 0 || operation.Evidence.StdoutText != []string{"<><two words> changed input\n", "shell original shell input\n", "new-observation"}[index] {
			t.Fatal("native streams do not reflect each distinct revised operation", operation.Evidence)
		}
	}
	data, err := os.ReadFile(filepath.Join(comparison.Workspace.Path, "qa-executions"))
	if err != nil || string(data) != "abc" {
		t.Fatal("revised sequence did not execute exactly once in its requested order", string(data), err)
	}
	d.reopen()
	_, recovered := awaitQAInput(t, d, func(in protocol.ValidationInput) bool { return len(in.ExecutionFacts) == 2 })
	if !reflect.DeepEqual(comparison.ExecutionFacts, recovered.ExecutionFacts) {
		t.Fatal("recovery changed original native receipts")
	}
	data, err = os.ReadFile(filepath.Join(recovered.Workspace.Path, "qa-executions"))
	if err != nil || string(data) != "abc" {
		t.Fatal("recovery reexecuted approved operations", string(data), err)
	}
}
