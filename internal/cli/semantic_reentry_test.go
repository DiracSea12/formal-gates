package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

func TestSemanticControlPublicConfirmationBindsOriginalAskAndReplays(t *testing.T) {
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	saved := filepath.Join(t.TempDir(), "confirmation.json")
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "next", "--root", root, "--request", "ABORT", "--save-candidate", saved}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var request facade.ControlCandidate
	if err := readJSONFileInto(saved, &request); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(request.Input, facade.ControlInput{Request: protocol.ControlAbort}) {
		t.Fatal("request flags were not retained as only semantic input", request.Input)
	}
	if request.Event.ID == "" || request.Event.Request == nil || request.Event.Request.Control != protocol.ControlAbort || request.Root != root || request.Run.RunID != "writer-exclusive" || request.Run.Envelope.PackageDigest == "" || !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
		t.Fatal("program preparation lost original identity or wrote authoritative state", request)
	}
	if strings.Contains(out.String(), `"event"`) || strings.Contains(out.String(), string(request.Event.ID)) {
		t.Fatal("semantic display requires the caller to reconstruct event identity", out.String())
	}
	out.Reset()
	if code := Run("formal-gates", []string{"workflow", "submit", "--confirmation", saved, "--single-step"}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	run, err := f.Show()
	if err != nil || len(run.PendingAsks) != 1 {
		t.Fatal("request did not create its original Ask", err)
	}
	ask := run.PendingAsks[0]
	if ask.Control != protocol.ControlAbort || ask.Token == "" || ask.RequestID != string(request.Event.ID) {
		t.Fatal("Ask lacks its program-issued identity", ask)
	}
	before = snapshotEngineRunTree(t, root, run.RunID)
	reason := "保留当前任务 \"原文\"\n最后两格  "
	out.Reset()
	if code := Run("formal-gates", []string{"workflow", "next", "--root", root, "--ask", "1", "--choice", "decline", "--reason", reason, "--save-candidate", saved}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var decision facade.ControlCandidate
	if err := readJSONFileInto(saved, &decision); err != nil {
		t.Fatal(err)
	}
	if decision.Event.Decide == nil || decision.Event.Decide.Request != protocol.RequestID(ask.RequestID) || decision.Event.Decide.Token != ask.Token || decision.Event.Decide.Choice != "decline" || decision.Input.Reason != reason || decision.Event.ID == "" || !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, run.RunID)) {
		t.Fatal("ordinal flags lost the original Ask or exact semantic text", decision)
	}
	for _, identity := range []string{ask.RequestID, ask.Token, string(decision.Event.ID)} {
		if strings.Contains(out.String(), identity) {
			t.Fatal("decision display exposed a transport field for reconstruction", identity, out.String())
		}
	}
	args := []string{"workflow", "submit", "--confirmation", saved, "--single-step"}
	out.Reset()
	if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	run, err = f.Show()
	if err != nil || len(run.PendingAsks) != 0 || run.Status != "ACTIVE" {
		t.Fatal("confirmed decline did not settle the selected Ask", err, run.Status)
	}
	accepted := snapshotEngineRunTree(t, root, run.RunID)
	out.Reset()
	if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 0 || !reflect.DeepEqual(accepted, snapshotEngineRunTree(t, root, run.RunID)) {
		t.Fatal("saved original confirmation replay changed authority", code, stderr.String())
	}
	t.Logf("next flags -> saved request %s -> original Ask %s -> saved decision %s -> confirmation -> unchanged replay", request.Event.ID, ask.RequestID, decision.Event.ID)
}

func TestSemanticControlPublicInputModesRejectMixturesBeforeWrite(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	for _, test := range []struct {
		args    []string
		message string
	}{
		{[]string{"next", "--request", "ABORT", "--decision", "unread-machine-input"}, "select semantic flags or machine decision input"},
		{[]string{"replan", "--classification", "meaning-preserved", "--decision", "unread-machine-input"}, "select semantic flags or machine decision input"},
		{[]string{"submit", "--request", "ABORT", "--confirmation", "unread-candidate"}, "semantic flags cannot modify a saved confirmation or worker result"},
		{[]string{"submit", "--request", "ABORT", "--worker-material", "unread-material"}, "semantic flags cannot modify a saved confirmation or worker result"},
		{[]string{"submit", "--ask", "1", "--choice", "decline", "--single-step"}, "user decisions require the complete candidate"},
	} {
		var out, stderr bytes.Buffer
		args := append(append([]string{"workflow"}, test.args...), "--root", root)
		code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr})
		if code == 0 || out.Len() != 0 || !strings.Contains(stderr.String(), test.message) || !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
			t.Fatalf("input mode rejection lost its no-write boundary: args=%v code=%d stdout=%s stderr=%s", args, code, out.String(), stderr.String())
		}
	}
}

func TestSemanticReentryMessageSelectsOriginalBytesForRunAndWorker(t *testing.T) {
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request facade.StartRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	request.RunID = "another-target"
	admission := &facade.Admission{RegistryPath: request.AdmissionRegistryPath, Target: request.AdmissionTarget, Root: request.AdmissionRoot, Provider: request.Provider, PackageDigest: request.PackageDigest, InstalledTargetIdentity: request.InstalledTargetIdentity, Generation: request.AdmissionGeneration, Lease: request.AdmissionLease, Token: request.AdmissionToken}
	if _, _, err := facade.Start(facade.StartOptions{Root: root, ArtifactRoot: root, Request: request, Admission: admission, VCSRunner: frozenGitRunner{}}); err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(run.WorkerMaterials))
	for id := range run.WorkerMaterials {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		t.Fatal("normal drive did not issue a worker")
	}
	material := run.WorkerMaterials[ids[0]]
	identity := "original-native-session"
	receipt, err := protocol.NewSpawnReceiptEvent("worker-started", material.ActionID, request.Provider, identity, protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(receipt); err != nil {
		t.Fatal(err)
	}
	untouched := snapshotEngineRunTree(t, root, "another-target")
	text := "保留 \"原文\"\r\n\n  中间缩进\t与尾随空格  "
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "message", "--root", root, "--run", "2", "--worker", "1", "--prepare-text"}, IO{Stdin: strings.NewReader(text), Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	prepared, err := f.Show()
	if err != nil || len(prepared.TaskMessages) != 0 {
		t.Fatal("saving original input sent a message", prepared.TaskMessages, err)
	}
	directory, err := workerTextDirectory(f, root, "writer-exclusive", 1)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := originalPayloadPaths(directory)
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	body, err = os.ReadFile(paths[0])
	if err != nil || string(body) != text {
		t.Fatal("stored original text bytes changed", err)
	}
	out.Reset()
	if code := Run("formal-gates", []string{"workflow", "message", "--root", root, "--run", "2", "--worker", "1", "--text-payload", "1"}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var message protocol.TaskMessage
	if err := json.Unmarshal(out.Bytes(), &message); err != nil {
		t.Fatal(err)
	}
	reopened, err := facade.Open(root, "writer-exclusive")
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.Show()
	if err != nil {
		t.Fatal(err)
	}
	if message.Text != text || message.ActionID != material.ActionID || message.AttemptID != material.AttemptID || message.Identity != identity || message.Status != "PENDING" || after.TaskMessages[message.ID] != message || !reflect.DeepEqual(untouched, snapshotEngineRunTree(t, root, "another-target")) {
		t.Fatal("file bytes or original selected execution binding changed", message)
	}
	t.Logf("run ordinal 2, worker ordinal 1 -> original action %s, attempt %s, session %s, %d exact bytes persisted", message.ActionID, message.AttemptID, message.Identity, len(text))
}

func TestSemanticReentryProgressUsesCompleteOriginalOutputAsCursor(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	for _, machine := range []bool{false, true} {
		var out, stderr bytes.Buffer
		args := []string{"workflow", "progress", "--root", root}
		if machine {
			args = append(args, "--machine")
		}
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(stderr.String())
		}
		original := append([]byte(nil), out.Bytes()...)
		path := filepath.Join(t.TempDir(), "original-progress.json")
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if code := Run("formal-gates", []string{"workflow", "progress", "--root", root, "--after-file", path, "--machine"}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatalf("complete program progress output cannot be reused: %s\noriginal=%s", stderr.String(), original)
		}
		var cursor struct {
			Root     string `json:"root"`
			RunID    string `json:"runId"`
			Revision uint64 `json:"revision"`
		}
		var next facade.Progress
		if json.Unmarshal(original, &cursor) != nil || json.Unmarshal(out.Bytes(), &next) != nil || next.AfterRevision != cursor.Revision || next.Root != root || next.RunID != "writer-exclusive" || next.Revision != cursor.Revision || len(next.NewResults) != 0 || !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
			t.Fatal("original cursor lost identity, revision, result boundary or changed authority", out.String())
		}
		t.Logf("machine=%v complete original output reused revision %d without rewriting cursor or state", machine, cursor.Revision)
	}
}
