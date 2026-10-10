package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestWorkflowViewShowsCurrentSemanticEvidenceAndChoices(t *testing.T) {
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

func TestWorkflowPublicViewKeepsMachineOutputAndSavedCandidate(t *testing.T) {
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

func TestWorkflowViewPreservesExecutionExitAndMachineConsumption(t *testing.T) {
	value := workflowConsumption{Run: facade.Run{Status: "ACTIVE", WorkerMaterials: map[string]facade.WorkerMaterial{"action": {Path: "worker-source"}}}, Boundary: "BLOCKED", Blocked: map[string]string{"action": "original execution failure"}}
	for _, machine := range []bool{false, true} {
		for _, failure := range []error{nil, context.Canceled} {
			var out, stderr bytes.Buffer
			code, err := printWorkflowView(IO{Stdout: &out, Stderr: &stderr}, value, machine, failure)
			if err != nil || failure == nil && code != 0 || failure != nil && code != 130 || !strings.Contains(out.String(), "original execution failure") {
				t.Fatalf("presentation changed the execution boundary: %d %v %s", code, err, out.String())
			}
			if strings.Contains(out.String(), "worker-source") != machine {
				t.Fatal("machine and default execution views have wrong visibility")
			}
		}
	}
}

func TestWorkflowConfirmationUsesPreparedAskAndPreservesPreparationErrors(t *testing.T) {
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
	_, run, err := f.SubmitControl(request)
	if err != nil {
		t.Fatal(err)
	}
	ask := run.PendingAsks[0]
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: string(ask.Options[0].ID), Reason: "Choose the original offered control"})
	if err != nil {
		t.Fatal(err)
	}
	// The successful event owns the binding; unused draft fields are not identity.
	candidate.Input.Ask = 99
	questions := workflowConfirmationView(candidate)["run"].(map[string]any)["questions"].([]map[string]any)
	if len(questions) != 1 || questions[0]["ask"] != 1 || candidate.Event.Decide.Request != protocol.RequestID(ask.RequestID) {
		t.Fatal("confirmation did not use the prepared event's original Ask", questions)
	}
}
