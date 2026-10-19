package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/requirement"
)

func TestWorkflowViewReportsConfigurationStopDetails(t *testing.T) {
	run := facade.Run{Next: decision.NextResult{Kind: decision.KindWait, Wait: &decision.WaitPayload{Reason: decision.WaitConfigurationUnavailable, Detail: "bound configuration 工作 does not exist"}}}
	next := workflowRunView(run)["next"].(map[string]any)
	if next["reason"] != decision.WaitConfigurationUnavailable || next["detail"] != "bound configuration 工作 does not exist" {
		t.Fatal("next/show hid the configuration failure", next)
	}
}

func TestWorkflowViewShowsCurrentSemanticEvidenceAndChoices(t *testing.T) {
	t.Parallel()
	run := facade.Run{RunID: "run", Status: "ACTIVE", Next: decision.NextResult{Kind: decision.KindAsk}, PendingAsks: []facade.PendingAskFace{{RequestID: "opaque-request", Token: "opaque-token", Control: protocol.ControlReviewFindingDecision, Options: []protocol.AskOption{{ID: "confirm:opaque-finding", Label: "Confirm the finding"}}}}, WorkerMaterials: map[string]facade.WorkerMaterial{"opaque-action": {Path: "other-role-material"}}}
	finding := review.Finding{ID: "opaque-finding", Digest: "opaque-digest", Severity: "P1", Message: "Normal command drops output", SeverityRationale: "Required output is unusable without a workaround", Requirement: "Required stdout", NormalPath: "app valid", ActualError: "Empty output", DeferralImpact: "No usable result", Locations: []string{"app.go:3"}, Verification: review.FindingVerification{Reproduction: "Observed actual stdout"}}
	run.Review.Actions = []facade.ReviewActionFace{{Action: "product-review", Completed: 2, Candidates: []facade.ReviewCandidateFace{{Excluded: true}, {DispatchID: "opaque-dispatch", Findings: []facade.ReviewFindingFace{{ID: finding.ID, Digest: finding.Digest, Severity: finding.Severity, Message: finding.Message, Evidence: finding}}}}}}
	before, _ := json.Marshal(run)
	view := workflowRunView(run)
	body, _ := json.Marshal(view)
	for _, forbidden := range []string{"opaque-", "workerMaterials", "other-role-material", "completed", "dispatchId", "token"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("default workflow view exposed unrelated transport: %s", body)
		}
	}
	question := view["questions"].([]map[string]any)[0]
	if question["ask"] != 1 || question["options"].([]map[string]any)[0]["choice"] != "confirm" {
		t.Fatal("default question lost the current semantic selection")
	}
	choice := question["reviews"].([]map[string]any)[0]
	entry := choice["findings"].([]map[string]any)[0]
	if choice["review"] != 1 || choice["candidate"] != 2 || entry["finding"] != 1 || entry["normalPath"] != finding.NormalPath || entry["severityRationale"] != finding.SeverityRationale || !reflect.DeepEqual(entry["locations"], finding.Locations) || entry["verification"] != finding.Verification {
		t.Fatal("default review view lost original evidence or source positions", choice)
	}
	after, _ := json.Marshal(run)
	if !bytes.Equal(before, after) {
		t.Fatal("presentation mutated the program projection")
	}
}

func TestWorkflowConfirmationShowsOnlyTheRequestedRecoveryEvidence(t *testing.T) {
	t.Parallel()
	f, root, _ := startEngineRunForWriterTest(t)
	run, err := f.Drive()
	if err != nil || len(run.WorkerMaterials) != 1 {
		t.Fatal("the original worker was not issued", err, run.WorkerMaterials)
	}
	var action, provider string
	for id, material := range run.WorkerMaterials {
		action = id
		var issued worker.Material
		if err := readJSONFileInto(material.Path, &issued); err != nil {
			t.Fatal(err)
		}
		provider = issued.Input.Provider
	}
	spawn, err := protocol.NewSpawnReceiptEvent("recovery-view-spawn", action, provider, action, protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(spawn); err != nil {
		t.Fatal(err)
	}
	const cause = "The native invocation stopped before completing its original review."
	failure, err := protocol.NewWorkerResultEvent("recovery-view-failure", action, provider, protocol.OutcomeRuntimeError, "original-recovery-view-evidence", "USER_ACTION_REQUIRED")
	if err != nil {
		t.Fatal(err)
	}
	failure.Result.InterruptionReason = cause
	_, run, err = f.Submit(failure)
	if err != nil || len(run.PendingAsks) != 1 || run.PendingAsks[0].Control != protocol.ControlRecovery {
		t.Fatal("the original interruption did not create its recovery Ask", err, run.PendingAsks)
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	var original protocol.RecoveryRecord
	for _, record := range candidate.Run.RecoveryRecords {
		if record.Action == protocol.RecoveryAsk && record.RequestID == string(candidate.Event.Decide.Request) {
			original = record
		}
	}
	if original.ActionID != action || original.AttemptID == "" || original.InterruptionReason != cause {
		t.Fatal("the recovery lost its original execution evidence", original)
	}
	historical := original
	historical.RequestID, historical.InterruptionReason = "another-request", "unrelated historical interruption"
	nonAsk := original
	nonAsk.Action, nonAsk.InterruptionReason = protocol.RecoveryResumeAttempt, "another recovery route"
	candidate.Run.RecoveryRecords = append([]protocol.RecoveryRecord{historical, nonAsk}, candidate.Run.RecoveryRecords...)
	before, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, run.RunID, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	view := workflowConfirmationView(candidate)
	questions := view["run"].(map[string]any)["questions"].([]map[string]any)
	want := map[string]any{"actionId": original.ActionID, "attemptId": original.AttemptID, "task": original.Task, "interruptionReason": cause, "detail": original.Detail}
	if len(questions) != 1 || !reflect.DeepEqual(questions[0]["recovery"], want) {
		t.Fatal("confirmation did not select its original recovery record", questions)
	}
	body, err := json.Marshal(view)
	if err != nil || strings.Contains(string(body), historical.InterruptionReason) || strings.Contains(string(body), nonAsk.InterruptionReason) {
		t.Fatal("confirmation included unrelated recovery history", string(body), err)
	}
	after, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, run.RunID, "state.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("recovery confirmation display changed authoritative state", err)
	}
}

func TestWorkflowAbortConfirmationIncludesOnlyItsCurrentOperation(t *testing.T) {
	t.Parallel()
	f, root, _ := startEngineRunForWriterTest(t)
	run, err := f.Drive()
	if err != nil || len(run.WorkerMaterials) == 0 {
		t.Fatal("the original run did not issue worker material", err)
	}
	request, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = f.SubmitControl(request)
	if err != nil || len(run.PendingAsks) != 1 {
		t.Fatal("the original abort request did not create its Ask", err, run.PendingAsks)
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: string(run.PendingAsks[0].Options[0].ID), Reason: "Stop the current workflow"})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Run.Settings == nil || len(candidate.Run.FlowNodes) == 0 || len(candidate.Run.ExecutionNodes) == 0 || len(candidate.Run.BusinessNodes) == 0 || len(candidate.Run.WorkerMaterials) == 0 {
		t.Fatal("the original run omitted the unrelated projection used by this check")
	}
	before, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	view := workflowConfirmationView(candidate)
	current := view["run"].(map[string]any)
	questions := current["questions"].([]map[string]any)
	if view["root"] != root || !reflect.DeepEqual(view["input"], candidate.Input) || len(current) != 5 || current["runId"] != candidate.Run.RunID || current["revision"] != candidate.Run.Revision || current["route"] != candidate.Run.Route || current["status"] != candidate.Run.Status || len(questions) != 1 || questions[0]["control"] != protocol.ControlAbort || !reflect.DeepEqual(questions[0]["options"], workflowRunView(candidate.Run)["questions"].([]map[string]any)[0]["options"]) {
		t.Fatal("abort confirmation lost its exact operation, run or original options", view)
	}
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"settings"`, `"configuration"`, `"sources"`, `"files"`, `"summaryPath"`, `"workers"`, `"workerMaterials"`, `"businessNodes"`, `"flowNodes"`, `"executionNodes"`, `"availableActions"`, `"next"`} {
		if strings.Contains(string(body), forbidden) {
			t.Fatal("abort confirmation exposed unrelated run details", forbidden, string(body))
		}
	}
	for _, path := range candidate.Run.Settings.Files {
		if path != "" && strings.Contains(string(body), path) {
			t.Fatal("abort confirmation exposed a host settings path", path)
		}
	}
	for _, material := range candidate.Run.WorkerMaterials {
		if material.Path != "" && strings.Contains(string(body), material.Path) {
			t.Fatal("abort confirmation exposed another role's material", material.Path)
		}
	}
	after, err := json.Marshal(candidate)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("confirmation display changed the original saved candidate", err)
	}
}

func TestWorkflowPublicViewKeepsMachineOutputAndSavedCandidate(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	statePath := filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"show", "status", "next"} {
		var out, stderr bytes.Buffer
		args := []string{"workflow", command, "--root", root, "--run-id", "writer-exclusive"}
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(stderr.String())
		}
		if strings.Contains(out.String(), "envelope") || strings.Contains(out.String(), "workerMaterials") || strings.Contains(out.String(), "adapterIntents") {
			t.Fatal("default CLI exposed program dispatch data", out.String())
		}
		out.Reset()
		if code := Run("formal-gates", append(args, "--machine"), IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(stderr.String())
		}
		if command == "next" {
			var actual decision.NextResult
			expected, err := f.Next()
			if err != nil || json.Unmarshal(out.Bytes(), &actual) != nil || !reflect.DeepEqual(actual, expected) {
				t.Fatal("machine Next changed the original projection", err)
			}
		} else {
			if !strings.Contains(out.String(), "envelope") {
				t.Fatal("machine run omitted program identity")
			}
		}
	}
	var out, stderr bytes.Buffer
	saved := filepath.Join(t.TempDir(), "confirmation.json")
	if code := Run("formal-gates", []string{"workflow", "next", "--root", root, "--run-id", "writer-exclusive", "--decision", "-", "--save-candidate", saved}, IO{Stdin: strings.NewReader(`{"request":"ABORT"}`), Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	if strings.Contains(out.String(), `"event"`) || strings.Contains(out.String(), "opaque") || strings.Contains(out.String(), "workerMaterials") {
		t.Fatal("confirmation display exposed the event or other roles", out.String())
	}
	body, err := os.ReadFile(saved)
	var candidate facade.ControlCandidate
	if err != nil || json.Unmarshal(body, &candidate) != nil || candidate.Event.ID == "" || candidate.Event.Request == nil || candidate.Run.RunID != "writer-exclusive" {
		t.Fatal("saved confirmation lost its complete original program object", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only presentation/preparation changed workflow state", err)
	}
}

func TestWorkflowNodeSelectionSavesOriginalPreviewWithoutWriting(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	run, err := f.Show()
	if err != nil || len(run.BusinessNodes) == 0 {
		t.Fatal("current business targets unavailable", err)
	}
	statePath := filepath.Join(root, facade.EngineNamespace, run.RunID, "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	saved := filepath.Join(t.TempDir(), "node-choice.json")
	args := []string{"workflow", "next", "--root", root, "--run-id", run.RunID, "--node", "1", "--save-candidate", saved}
	if code := runWithHostConfirmation(t, args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	body, err := os.ReadFile(saved)
	var candidate facade.ControlCandidate
	if err != nil || json.Unmarshal(body, &candidate) != nil || candidate.Continuation == nil || candidate.Continuation.To.ID != run.BusinessNodes[0].ID || candidate.Event.Request.Control != protocol.ControlContinueNode {
		t.Fatal("selected number lost its exact program-generated preview", err)
	}
	if strings.Contains(out.String(), "businessNodes") || strings.Contains(out.String(), candidate.Continuation.Digest) || strings.Contains(out.String(), string(candidate.Event.ID)) || strings.Contains(out.String(), `"freshnessToken"`) {
		t.Fatal("default view did not present semantic choices", out.String())
	}
	var view struct {
		Root         string `json:"root"`
		Continuation struct {
			From    []string                           `json:"from"`
			To      protocol.BusinessNode              `json:"to"`
			Work    []protocol.RequirementWorkDecision `json:"work"`
			Effects []string                           `json:"effects"`
		} `json:"continuation"`
	}
	if err := json.Unmarshal(out.Bytes(), &view); err != nil || view.Root != root || !reflect.DeepEqual(view.Continuation.From, candidate.Continuation.From) || !reflect.DeepEqual(view.Continuation.To, candidate.Continuation.To) || !reflect.DeepEqual(view.Continuation.Work, candidate.Continuation.Work) || !reflect.DeepEqual(view.Continuation.Effects, candidate.Continuation.Effects) {
		t.Fatal("continuation display lost the actual project or effects", err, view)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("node preview authorized or changed current", err)
	}
}

func TestWorkflowViewPreservesExecutionExitAndMachineConsumption(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	run, err := f.Drive()
	if err != nil || len(run.WorkerMaterials) != 1 {
		t.Fatal("original issued material is unavailable", err)
	}
	value := workflowConsumption{Run: run, Boundary: "BLOCKED", Blocked: map[string]string{"action": "original execution failure"}}
	before, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	state := snapshotEngineRunTree(t, root, run.RunID)
	for _, machine := range []bool{false, true} {
		for _, failure := range []error{nil, context.Canceled, errors.New("original execution error")} {
			var out, stderr bytes.Buffer
			code, err := printCurrentWorkflowConsumption(IO{Stdout: &out, Stderr: &stderr}, f, value, machine, failure)
			want := 0
			if failure == context.Canceled {
				want = 130
			} else if failure != nil {
				want = 1
			}
			if err != nil || code != want || !strings.Contains(out.String(), "original execution failure") {
				t.Fatalf("presentation changed the execution boundary: %d %v %s", code, err, out.String())
			}
			if strings.Contains(out.String(), "workerMaterials") != machine {
				t.Fatal("machine and default execution views have wrong visibility")
			}
			if failure == nil && stderr.Len() != 0 || failure != nil && stderr.String() != failure.Error()+"\n" {
				t.Fatal("presentation changed original execution error output", stderr.String())
			}
			if machine {
				var actual bytes.Buffer
				if err := json.Compact(&actual, out.Bytes()); err != nil || !bytes.Equal(actual.Bytes(), before) {
					t.Fatal("machine consumption changed the complete original projection", err)
				}
			}
		}
	}
	after, err := json.Marshal(value)
	if err != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(state, snapshotEngineRunTree(t, root, run.RunID)) {
		t.Fatal("execution presentation changed the original value or authoritative files", err)
	}
}

func TestWorkflowConfirmationUsesPreparedAskAndPreservesPreparationErrors(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	statePath := filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var request facade.ControlCandidate
	for _, machine := range []bool{false, true} {
		args := []string{"workflow", "next", "--root", root, "--run-id", "writer-exclusive", "--decision", "-"}
		if machine {
			args = append(args, "--machine")
		}
		var out, stderr bytes.Buffer
		if code := Run("formal-gates", args, IO{Stdin: strings.NewReader(`{"ask":1,"choice":"proceed"}`), Stdout: &out, Stderr: &stderr}); code == 0 || out.Len() != 0 || !strings.Contains(stderr.String(), "outside the current") {
			t.Fatalf("preparation error was lost: code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
		}
		out.Reset()
		stderr.Reset()
		saved := filepath.Join(t.TempDir(), "request.json")
		if code := Run("formal-gates", append(args, "--save-candidate", saved), IO{Stdin: strings.NewReader(`{"request":"ABORT","ask":1}`), Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(stderr.String())
		}
		data, err := os.ReadFile(saved)
		if err != nil || json.Unmarshal(data, &request) != nil || request.Event.Request == nil || request.Event.Decide != nil || request.Input.Ask != 1 {
			t.Fatal("request preview changed the complete saved candidate", err)
		}
		if !machine {
			var view struct {
				Run struct {
					Questions []any `json:"questions"`
				} `json:"run"`
			}
			if err := json.Unmarshal(out.Bytes(), &view); err != nil || len(view.Run.Questions) != 0 {
				t.Fatal("new request claimed an unbound Ask", err)
			}
		}
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preparation or error display changed run state", err)
	}
	_, run, err := submitCLIControlWithHost(t, f, request)
	if err != nil {
		t.Fatal(err)
	}
	ask := run.PendingAsks[0]
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: string(ask.Options[0].ID), Reason: "Choose the original offered control"})
	if err != nil {
		t.Fatal(err)
	}
	// The event owns its original Ask; input.Ask supplies the display number.
	candidate.Input.Ask = 99
	questions := workflowConfirmationView(candidate)["run"].(map[string]any)["questions"].([]map[string]any)
	if len(questions) != 1 || questions[0]["ask"] != 99 || candidate.Event.Decide.Request != protocol.RequestID(ask.RequestID) {
		t.Fatal("confirmation did not use the prepared event's original Ask", questions)
	}
}

func TestWorkflowConfirmationUsesSavedTargetAndOriginalBindings(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	saved := filepath.Join(t.TempDir(), "confirmation.json")
	var out, stderr bytes.Buffer
	if code := runWithHostConfirmation(t, []string{"workflow", "next", "--root", root, "--run-id", "writer-exclusive", "--decision", "-", "--save-candidate", saved}, IO{Stdin: strings.NewReader(`{"request":"ABORT"}`), Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	for _, target := range [][]string{{"--root", t.TempDir()}, {"--run-id", "different-run"}} {
		out.Reset()
		stderr.Reset()
		args := append([]string{"workflow", "submit", "--confirmation", saved, "--single-step"}, target...)
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "confirmation belongs to a different project or run") || out.Len() != 0 {
			t.Fatalf("conflicting explicit target was accepted: %d %s", code, stderr.String())
		}
		if !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
			t.Fatal("conflicting target changed the original run")
		}
	}
	for _, decision := range []bool{false, true} {
		if decision {
			run, err := f.Show()
			if err != nil || len(run.PendingAsks) != 1 || run.PendingAsks[0].Control != protocol.ControlAbort {
				t.Fatal("saved request did not create its original user Ask", err)
			}
			out.Reset()
			stderr.Reset()
			if code := runWithHostConfirmation(t, []string{"workflow", "next", "--root", root, "--run-id", run.RunID, "--decision", "-", "--save-candidate", saved}, IO{Stdin: strings.NewReader(`{"ask":1,"choice":"decline","reason":"Keep the active run"}`), Stdout: &out, Stderr: &stderr}); code != 0 {
				t.Fatal(stderr.String())
			}
		}
		out.Reset()
		stderr.Reset()
		args := []string{"workflow", "submit", "--confirmation", saved, "--single-step"}
		if code := runWithHostConfirmation(t, args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatalf("saved target did not submit: %d %s", code, stderr.String())
		}
		accepted := snapshotEngineRunTree(t, root, "writer-exclusive")
		var err error
		f, err = facade.Open(root, "writer-exclusive")
		if err != nil {
			t.Fatal(err)
		}
		out.Reset()
		stderr.Reset()
		if code := runWithHostConfirmation(t, args, IO{Stdout: &out, Stderr: &stderr}); code != 0 || !reflect.DeepEqual(accepted, snapshotEngineRunTree(t, root, "writer-exclusive")) {
			t.Fatalf("reopened replay changed the original event or run: %d %s", code, stderr.String())
		}
	}
}

func TestWorkflowOperatorViewLeavesInternalFactsToDrive(t *testing.T) {
	t.Parallel()
	run := facade.Run{RunID: "run", Revision: 12, Next: decision.NextResult{Kind: decision.KindOperator, Operator: &decision.OperatorPayload{Facts: []decision.Fact{{Value: "submit EXECUTION_SCOPE observation for original-wave"}, {Value: "native effect is UNKNOWN; observe and submit its typed completion evidence"}}}}}
	original, _ := json.Marshal(run)
	view := workflowRunView(run)
	next := view["next"].(map[string]any)
	if next["action"] != "drive" || next["kind"] != decision.KindOperator {
		t.Fatal("operator boundary lost its program-owned continuation", next)
	}
	if _, exists := next["facts"]; exists {
		t.Fatal("main-agent view instructs internal observation submission")
	}
	after, _ := json.Marshal(run)
	if !bytes.Equal(original, after) {
		t.Fatal("display changed authoritative operator facts")
	}
}

// An actual install, Git baseline and ordinary split producer supply both run
// identities. The CLI receives only the displayed node and Ask numbers.
func TestWorkflowDirectNodeRequestSelectsOriginalChild(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	runner := vcs.ExecRunner{}
	mustWriteCLI(t, filepath.Join(root, "README.md"), "two independent command components\n")
	for _, args := range [][]string{{"init"}, {"add", "README.md"}, {"-c", "user.name=Node Test", "-c", "user.email=node@example.invalid", "commit", "-m", "initial components"}} {
		if _, err := runner.Run(root, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	base, err := runner.Run(root, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	f := startAdmittedEngineRun(t, root, strings.TrimSpace(base))
	workflowSplitFixture(t, f, root)
	runs, err := f.ControlRuns()
	if err != nil || len(runs) < 2 {
		t.Fatal("real child was not created", err)
	}
	childRun := runs[1]
	node := len(runs[0].BusinessNodes) + 1
	for i, n := range childRun.BusinessNodes {
		if n.Kind == "route" {
			node = len(runs[0].BusinessNodes) + i + 1
			break
		}
	}
	child, err := facade.Open(root, childRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"workflow", "submit", "--root", root, "--node", fmt.Sprint(node), "--single-step"}
	if code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	current, err := child.Show()
	if err != nil {
		t.Fatal(err)
	}
	var nodeAsk facade.PendingAskFace
	for _, a := range current.PendingAsks {
		if a.Control == protocol.ControlContinueNode {
			nodeAsk = a
		}
	}
	if nodeAsk.RequestID == "" || nodeAsk.Continuation.To.Kind != "route" {
		t.Fatal("direct parent request was not bound to actual child")
	}
	runs, err = f.ControlRuns()
	if err != nil {
		t.Fatal(err)
	}
	question := 0
	found := false
	for _, r := range runs {
		for _, a := range r.PendingAsks {
			question++
			if a.RequestID == nodeAsk.RequestID {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	saved := filepath.Join(t.TempDir(), "child-confirmation.json")
	stdout.Reset()
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "next", "--root", root, "--ask", fmt.Sprint(question), "--choice", "approve", "--reason", "continue the child route responsibility", "--save-candidate", saved}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var candidate facade.ControlCandidate
	if err := readJSONFileInto(saved, &candidate); err != nil || candidate.Run.RunID != childRun.RunID {
		t.Fatal("confirmation lost actual child identity", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runWithHostConfirmation(t, []string{"workflow", "submit", "--confirmation", saved, "--single-step"}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	current, err = child.Show()
	if err != nil || current.NodeContinuation == nil || current.NodeContinuation.Receipt.RequestID != nodeAsk.RequestID || current.NodeContinuation.Status != "APPLIED" {
		t.Fatal("original child authority was not applied", err)
	}
	t.Logf("actual parent global node %d -> original child Ask -> saved confirmation owner %s", node, childRun.RunID)
}

func workflowSplitFixture(t *testing.T, f *facade.Facade, root string) {
	t.Helper()
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	var requestBinding facade.StartRequest
	if err := readJSONFileInto(filepath.Join(root, facade.EngineNamespace, run.RunID, "request.json"), &requestBinding); err != nil {
		t.Fatal(err)
	}
	provider := requestBinding.Provider
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	submit := func(event protocol.Event) {
		t.Helper()
		var accepted protocol.Acceptance
		accepted, run, err = f.Submit(event)
		if err != nil || accepted.Status != "ACCEPTED" {
			t.Fatal("original node fixture event was not accepted", accepted, err)
		}
	}
	plan := &protocol.GranularityReview{Reason: "two independent components and their mainline integration", VerificationTier: "FINAL"}
	for _, owner := range []string{"core", "tooling", "master-merge"} {
		plan.Batches = append(plan.Batches, protocol.DevelopmentBatch{ID: owner, Owner: owner, Subtasks: []protocol.DevelopmentSubtask{{ID: owner + "-entry", Description: "implement the public " + owner + " responsibility"}}, Deliverable: owner + " public command behavior", PublicEntry: owner + " command", Artifacts: []string{owner + ".go"}, Interfaces: "documented command arguments", StateBoundary: "no persistent command state", Risk: "incorrect command output", DoD: []protocol.DevelopmentCriterion{{ID: owner + "-contract", Description: "public valid and invalid inputs have documented outcomes"}}, Checks: []protocol.DevelopmentCheck{{ID: owner + "-build", Purpose: protocol.CheckFullBuild, Goal: "Build the complete component project"}, {ID: owner + "-tests", Purpose: protocol.CheckFullTests, Goal: "Run the complete component project suite"}}, Rollback: "revert this component", Recovery: "retry from the confirmed source", Handoff: "document the component interface and checks", ContextCost: "one independent command component"})
	}
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		id := ""
		for key, pending := range run.PendingActions {
			if pending.ReviewSeries == action {
				id = key
			}
		}
		if id == "" {
			t.Fatal("ordinary review was not issued", action)
		}
		material := run.WorkerMaterials[id]
		body, err := os.ReadFile(material.Path)
		if err != nil {
			t.Fatal(err)
		}
		var issued worker.Material
		if err := json.Unmarshal(body, &issued); err != nil {
			t.Fatal(err)
		}
		if issued.Input.Pending.ActionID != id || issued.Input.Provider != provider || issued.Input.Review == nil || issued.Input.Review.Action != action || issued.Input.Review.RequirementRevision != run.IntakeConfirmationReceipt.RequirementRevision || issued.Input.RequirementDocument.Content == "" {
			t.Fatal("independent check found a different or incomplete original review input")
		}
		if action == review.StartReadiness {
			for i := range plan.Batches {
				if plan.Batches[i].Owner == "master-merge" {
					plan.Batches[i].RequirementPoints = requirement.PointIDs(issued.Input.RequirementPoints)
				}
			}
		}
		identity := "review-session:" + id
		event, err := protocol.NewSpawnReceiptEvent(protocol.EventID("spawn:"+id), id, provider, identity, protocol.SpawnStatusSpawned)
		if err != nil {
			t.Fatal(err)
		}
		submit(event)
		transcript := filepath.Join(t.TempDir(), "usage.jsonl")
		usage := []byte(`{"type":"assistant","uuid":"zero-usage","message":{"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}}` + "\n")
		mustWriteCLI(t, transcript, string(usage))
		directory := facade.WorkerExecutionDirectory(material, "")
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		descriptor, err := host.Lookup(provider)
		if err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]any{"native-request.json": map[string]any{"provider": descriptor.InstallName, "engine": map[string]any{"root": root, "runId": run.RunID, "workerMaterial": material, "resumeActionId": ""}}, "source.json": map[string]any{"provider": descriptor.InstallName, "identity": identity, "transcript": transcript, "sha256": fmt.Sprintf("%x", sha256.Sum256(usage))}} {
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			mustWriteCLI(t, filepath.Join(directory, name), string(raw))
		}
		if err := lifecycle.ObserveNativeCompletion(root, run.RunID, id, provider, identity); err != nil {
			t.Fatal(err)
		}
		evidence := protocol.ReviewResultEvidence{Action: action, DispatchID: id, RequirementRevision: run.IntakeConfirmationReceipt.RequirementRevision, Status: "PASS", Findings: []review.Finding{}}
		if action == review.StartReadiness {
			evidence.GranularityReview = plan
		}
		event, err = protocol.NewReviewWorkerResultEvent(protocol.EventID("review:"+id), id, provider, "review-output:"+id, evidence)
		if err != nil {
			t.Fatal(err)
		}
		submit(event)
		if len(run.ReviewVerifications) != 1 {
			t.Fatal("original raw review did not require independent acceptance", run.ReviewVerifications)
		}
		raw := run.ReviewVerifications[0]
		if raw.ActionID != id || raw.Provider != provider || raw.Review == nil || !reflect.DeepEqual(*raw.Review, evidence) || len(raw.Review.Findings) != 0 {
			t.Fatal("independent check found changed raw content or an unexpected finding", raw)
		}
		if action == review.StartReadiness {
			if err := protocol.ValidateGranularity(raw.Review.GranularityReview); err != nil {
				t.Fatal(err)
			}
			if err := protocol.ValidateProjectChecks(raw.Review.GranularityReview); err != nil {
				t.Fatal(err)
			}
			for _, batch := range raw.Review.GranularityReview.Batches {
				var owned []string
				if batch.Owner == "master-merge" {
					owned = requirement.PointIDs(issued.Input.RequirementPoints)
				}
				if !reflect.DeepEqual(batch.RequirementPoints, owned) {
					t.Fatal("component duty differs from the original confirmed points", batch)
				}
			}
		}
		candidate, err := f.PrepareControl(facade.ControlInput{ReviewVerification: &facade.ReviewVerificationInput{Result: 1, Findings: []protocol.ReviewFindingVerification{}, Verification: []string{
			"Compared the original action, provider and confirmed requirement revision in the complete issued material.",
			"The original raw PASS has no findings; the readiness contract, when supplied, passes the finite-duty and project-check validators.",
			"Read the complete original requirement and compared the retained raw result with the submitted component contract.",
			"The original report has no finding rows or locations to validate; its input points come from the confirmed document.",
			"The native Git baseline declares independent command components and readiness names their public entries.",
			"Compared readiness component duties with the original issued requirement point IDs.",
			"The submitted content concerns this product or readiness action and its original run only.",
			"The original report has zero findings; no unsupported severity or disposition is introduced.",
		}}})
		if err != nil {
			t.Fatal(err)
		}
		accepted, next, err := submitCLIControlWithHost(t, f, candidate)
		if err != nil || accepted.Status != "ACCEPTED" {
			t.Fatal("independent original review acceptance failed", accepted, err)
		}
		run = next
		run, err = f.Drive()
		if err != nil {
			t.Fatal(err)
		}
	}
	request, err := protocol.NewRequestEvent("split-request", protocol.ControlConfirmTopology, protocol.AskOption{ID: "split", Label: "two components"})
	if err != nil {
		t.Fatal(err)
	}
	submit(request)
	ask := run.PendingAsks[0]
	topology := &protocol.TopologyDecision{Decision: "split", Slices: []string{"core", "tooling"}, SplitCount: 2, Parallel: "two independent components", ACOwners: []protocol.ACOwner{{AC: "AC-001", Owner: "master-merge"}}}
	confirmedPlan, err := f.DevelopmentPlan(protocol.RouteDecision{}, *topology)
	if err != nil {
		t.Fatal(err)
	}
	topology.DevelopmentPlan = &confirmedPlan.Confirmation
	decision, err := protocol.NewReviewDecideEvent("split-confirmation", protocol.RequestID(ask.RequestID), ask.Token, "split", protocol.ReviewDecisionPayload{Topology: topology})
	if err != nil {
		t.Fatal(err)
	}
	submit(decision)
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowViewSeparatesQAExecutionGroupsFromFlowSwitch(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	view := workflowRunView(run)
	flow := view["flowNodes"].([]map[string]any)
	execution := view["executionNodes"].([]map[string]any)
	if len(flow) != 6 || len(execution) != 7 || execution[3]["id"] != "qa-blackbox" || execution[4]["id"] != "qa-whitebox" {
		t.Fatal("view lost independent QA execution choices or changed flow switches", view)
	}
	for _, node := range flow {
		if _, exists := node["execution"]; exists {
			t.Fatal("flow switch displayed a shared execution tuple", node)
		}
	}
	for index, node := range execution {
		if node["node"] != index+1 || node["description"] == "" || node["execution"] != run.ExecutionNodes[index].Execution {
			t.Fatal("execution choice lost its selection identity or effective tuple", node)
		}
	}
}
