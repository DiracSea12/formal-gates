package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/harness"
	"formal-gates/internal/host"
	"formal-gates/internal/prompts"
	"formal-gates/internal/settings"
	"formal-gates/internal/validate"
)

// confirmationCall is a protocol fixture. It verifies the actual displayed
// request before responding; it does not stand in for a real host UI test.
func confirmationCall(t *testing.T, name string, arguments any, capability bool, action string, display func(map[string]any), metadata ...any) map[string]any {
	t.Helper()
	return serveConfirmationCall(t, nil, name, arguments, capability, action, display, nil, metadata...)
}

func startedToolRunID(t *testing.T, result map[string]any) string {
	t.Helper()
	if result["isError"] != false {
		t.Fatal("startup failed", result)
	}
	var view struct {
		RunID string `json:"runId"`
	}
	message := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if err := json.Unmarshal([]byte(message), &view); err != nil || view.RunID == "" {
		t.Fatal("startup lost its run identity", message, err)
	}
	return view.RunID
}

// serveConfirmationCall is confirmationCall bound to explicit serve flags
// (an installed --package-root selects the serving host). When elicitations
// is non-nil it counts the service's elicitation requests: a permission-gate
// host confirms before the call arrives and must emit none.
func serveConfirmationCall(t *testing.T, serveArgs []string, name string, arguments any, capability bool, action string, display func(map[string]any), elicitations *int, metadata ...any) map[string]any {
	t.Helper()
	in, client, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); client.Close() })
	host, out, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close(); out.Close() })
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		code := Run("formal-gates", append([]string{"workflow", "serve"}, serveArgs...), IO{Stdin: in, Stdout: out, Stderr: &stderr})
		out.Close()
		done <- code
	}()
	encoder, decoder := json.NewEncoder(client), json.NewDecoder(host)
	capabilities := map[string]any{}
	if capability {
		capabilities["elicitation"] = map[string]any{"form": map[string]any{}}
	}
	if err := encoder.Encode(map[string]any{"id": "initialize", "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": capabilities}}); err != nil {
		t.Fatal(err)
	}
	var initialized any
	if err := decoder.Decode(&initialized); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"name": name, "arguments": arguments}
	if len(metadata) != 0 {
		params["_meta"] = metadata[0]
	}
	if err := encoder.Encode(map[string]any{"id": "original-tool-call", "method": "tools/call", "params": params}); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	for {
		var frame map[string]any
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if frame["method"] == nil {
			if frame["id"] != "original-tool-call" {
				t.Fatal("tool response lost the original request identity", frame)
			}
			result = frame["result"].(map[string]any)
			break
		}
		if frame["method"] != "elicitation/create" || frame["id"] == nil || !capability {
			t.Fatal("unexpected host request", frame)
		}
		if elicitations != nil {
			*elicitations++
		}
		params := frame["params"].(map[string]any)
		message := params["message"].(string)
		if utf8.RuneCountInString(message) > 800 || strings.Count(message, "\n")+1 > 12 {
			t.Fatal("confirmation exceeded its display budget")
		}
		schema := params["requestedSchema"].(map[string]any)
		if params["mode"] != "form" || len(schema["properties"].(map[string]any)) != 0 || schema["required"] != nil {
			t.Fatal("confirmation must leave the approval decision to the host", params)
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(params["message"].(string), "Confirm this exact operation:\n")), &value); err != nil {
			t.Fatal(err)
		}
		if display != nil {
			display(value)
		}
		id := frame["id"]
		if action == "wrong identity" {
			id, action = "different-confirmation", "accept"
		}
		if err := encoder.Encode(map[string]any{"id": id, "result": map[string]any{"action": action, "content": map[string]any{}}}); err != nil {
			t.Fatal(err)
		}
	}
	client.Close()
	if code := <-done; code != 0 || stderr.Len() != 0 {
		t.Fatal(code, stderr.String())
	}
	return result
}

func TestHostConfirmationPreservesCompleteJSONFrames(t *testing.T) {
	arguments := []string{"workflow", "control", "--root", ""}
	request := map[string]any{"id": 2, "method": "tools/call", "params": map[string]any{"name": "invoke", "arguments": arguments}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	arguments[3] = strings.Repeat("x", 1535-len(body))
	body, err = json.Marshal(request)
	if err != nil || len(body) != 1535 {
		t.Fatal("request does not end at the JSON read boundary", len(body), err)
	}
	candidate := map[string]any{"operation": "confirm exact original", "text": strings.Repeat("原始确认内容\n", 1024)}
	confirmed := false
	var confirmationErr error
	withHostConfirmation(t, arguments, func(confirm func(any) error) {
		confirmationErr = confirm(candidate)
		confirmed = confirmationErr == nil
	})
	if !confirmed || confirmationErr != nil {
		t.Fatal("host did not confirm the complete original frame", confirmationErr)
	}
}

func TestWorkflowConfirmationStartPreservesCurrentHostOwner(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	saveReviewOnlyConfiguration(t, root)
	cliStartRepo(t, root)
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, "", "intake"), mustJSON(t, preview.IntakeCandidate))
	if err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(t.TempDir(), "owner.jsonl")
	mustWriteCLI(t, transcript, `{"sessionId":"dsh-owner","event":{"type":"assistant/message","seq":1,"data":{"usage":{"inputTokens":100,"outputTokens":10,"cacheReadTokens":50}}}}`+"\n")
	owner := facade.OwnerBinding{Session: "dsh-owner", Transcript: transcript, Provider: "deepseek-harness"}
	for _, test := range []struct {
		metadata any
		owner    facade.OwnerBinding
	}{
		{map[string]any{"formal-gates/owner": owner}, owner},
		{map[string]any{"session_id": "zcode-owner", "turn_id": "original-turn", "com.zcode/request-context": map[string]any{"session_id": "zcode-owner", "turn_id": "original-turn", "workspace_path": root, "runtime_scope": "main"}}, facade.OwnerBinding{Session: "zcode-owner"}},
		{map[string]any{"x-codex-turn-metadata": map[string]any{"thread_id": "codex-owner", "turn_id": "original-turn"}}, facade.OwnerBinding{Session: "codex-owner"}},
		{map[string]any{"x-codex-turn-metadata": `{"thread_id":"codex-string-owner","turn_id":"original-turn"}`}, facade.OwnerBinding{Session: "codex-string-owner"}},
		{map[string]any{"thread_id": "codex-flat-owner", "threadId": "codex-flat-owner"}, facade.OwnerBinding{Session: "codex-flat-owner"}},
	} {
		result := confirmationCall(t, "confirm_start", map[string]any{"root": root, "intake": position, "configuration": "仅评审"}, true, "accept", func(value map[string]any) {
			t.Fatal("startup requested a native confirmation", value)
		}, test.metadata)
		if result["isError"] != false {
			t.Fatal("the host owner could not reach the original start owner", result)
		}
		runID := startedToolRunID(t, result)
		var saved struct {
			Content protocol.State `json:"content"`
		}
		data, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, runID, "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		state := saved.Content
		if state.OwnerSession != test.owner.Session || state.OwnerTranscript != test.owner.Transcript || state.OwnerProvider != test.owner.Provider {
			t.Fatal("the current host owner changed across the MCP start boundary", state.OwnerSession, state.OwnerTranscript, state.OwnerProvider)
		}
		if test.owner.Transcript == "" {
			if state.OwnerBaseline != nil || state.OwnerStartedAt != "" {
				t.Fatal("session-only host identity fabricated a metering source", state.OwnerBaseline, state.OwnerStartedAt)
			}
		} else if state.OwnerBaseline == nil || state.OwnerStartedAt == "" || state.OwnerBaseline.Usage.InputCacheMissTokens != 100 || state.OwnerBaseline.Usage.OutputTokens != 10 || state.OwnerBaseline.Usage.InputCacheHitTokens != 50 {
			t.Fatal("the original host transcript lost its metering baseline", state.OwnerBaseline, state.OwnerStartedAt)
		}
	}
	result := confirmationCall(t, "confirm_start", map[string]any{"root": root, "intake": position, "configuration": "仅评审", "owner": owner}, true, "accept", func(map[string]any) { t.Fatal("model-authored owner reached confirmation") })
	if result["isError"] != true {
		t.Fatal("model arguments supplied a host owner", result)
	}
}

func TestWorkflowConfirmationServiceKeepsAgentRequestsSeparateFromDecisions(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	request, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	_, pending, err := f.SubmitControl(request)
	if err != nil || len(pending.PendingAsks) != 1 {
		t.Fatal("request did not create its Ask", err)
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "decline", Reason: "keep this specific run"})
	if err != nil {
		t.Fatal(err)
	}
	_, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, pending.RunID, "control"), mustJSON(t, candidate))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := facade.OriginalPayloadPaths(workflowPayloadDirectory(root, pending.RunID, "control"))
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotEngineRunTree(t, root, pending.RunID)
	for _, flags := range [][]string{{"--confirm"}, {"--confirmation-candidate", "1"}, {"--confirmation", paths[position-1]}} {
		var output, stderr bytes.Buffer
		args := append([]string{"workflow", "submit", "--root", root, "--single-step"}, flags...)
		if code := Run("formal-gates", args, IO{Stdout: &output, Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "NATIVE_CONFIRMATION_UNAVAILABLE") || !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, pending.RunID)) {
			t.Fatal("agent CLI approved its own decision", code, output.String(), stderr.String())
		}
	}
	arguments := map[string]any{"root": root, "candidate": position}
	for _, test := range []struct {
		action string
		cap    bool
	}{{"decline", true}, {"cancel", true}, {"accept", false}, {"wrong identity", true}} {
		result := confirmationCall(t, "confirm_control", arguments, test.cap, test.action, func(value map[string]any) {
			shown := strings.Split(value["root"].(string), " … ")
			if !strings.HasPrefix(root, shown[0]) || !strings.HasSuffix(root, shown[len(shown)-1]) || value["event"] != nil || value["decision"] != "decline" || value["input"].(map[string]any)["choice"] != "decline" {
				t.Fatal("control display lost its exact operation or exposed the internal candidate", value)
			}
		})
		if result["isError"] != true || !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, pending.RunID)) {
			t.Fatal("unconfirmed host response changed the run", test.action, result)
		}
	}
	result := confirmationCall(t, "confirm_control", arguments, true, "accept", nil)
	if result["isError"] != false {
		t.Fatal("host could not directly submit its confirmed candidate", result)
	}
	after, err := f.Show()
	if err != nil || len(after.PendingAsks) != 0 || after.Revision != pending.Revision+1 {
		t.Fatal("host response did not settle the original Ask exactly once", err, after)
	}
	accepted := snapshotEngineRunTree(t, root, pending.RunID)
	replay := confirmationCall(t, "confirm_control", arguments, false, "", func(map[string]any) { t.Fatal("accepted replay requested another confirmation") })
	if replay["isError"] != false || !reflect.DeepEqual(accepted, snapshotEngineRunTree(t, root, pending.RunID)) {
		t.Fatal("accepted replay changed the original decision", replay)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestControlConfirmationBoundsLongCandidateAndKeepsChoice(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	request, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := f.SubmitControl(request)
	if err != nil {
		t.Fatal(err)
	}
	reason := strings.Repeat("保留原始长理由\n", 2000)
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "decline", Reason: reason})
	if err != nil {
		t.Fatal(err)
	}
	_, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, run.RunID, "control"), mustJSON(t, candidate))
	if err != nil {
		t.Fatal(err)
	}
	result := confirmationCall(t, "confirm_control", map[string]any{"root": root, "candidate": position}, true, "accept", func(view map[string]any) {
		if view["decision"] != "decline" || view["run"].(map[string]any)["runId"] != run.RunID || view["_display"] == nil {
			t.Fatal("bounded control confirmation lost its decision or run binding", view)
		}
	})
	if result["isError"] != false {
		t.Fatal("bounded display could not confirm the original control", result)
	}
	reopened, err := facade.Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.Show()
	if err != nil || len(current.PendingAsks) != 0 || current.Revision != run.Revision+1 {
		t.Fatal("bounded display changed control acceptance", current.Revision, err)
	}
	var state struct {
		Content protocol.State `json:"content"`
	}
	stateBody, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, run.RunID, "state.json"))
	if err != nil || json.Unmarshal(stateBody, &state) != nil {
		t.Fatal("could not read the original accepted state", err)
	}
	digest, err := candidate.Event.Digest()
	if err != nil || state.Content.Events[string(candidate.Event.ID)].Digest != digest {
		t.Fatal("bounded display changed the accepted original event including its full reason", err)
	}
}

func TestWorkflowConfirmationViewDisplaysTheOriginalCandidateProject(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	candidate := facade.ControlCandidate{Root: second, Run: facade.Run{RunID: "selected-project"}}
	view := workflowConfirmationView(candidate)
	if view["root"] != second || view["root"] == first || view["event"] != nil {
		t.Fatal("confirmation did not show its actual target project", view)
	}
}

func TestWorkflowConfirmationRejectsChangedCandidateAndCurrentRevision(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	request, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := f.SubmitControl(request)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "decline"})
	if err != nil {
		t.Fatal(err)
	}
	path, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, run.RunID, "control"), mustJSON(t, candidate))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, run.RunID, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	original := mustJSON(t, candidate)
	result := confirmationCall(t, "confirm_control", map[string]any{"root": root, "candidate": position}, true, "accept", func(map[string]any) {
		mustWriteCLI(t, path, string(original)+"\n")
	})
	after, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, run.RunID, "state.json"))
	if err != nil || result["isError"] != true || !bytes.Equal(before, after) {
		t.Fatal("changed candidate reused its original approval", result, err)
	}
	mustWriteCLI(t, path, string(original))
	result = confirmationCall(t, "confirm_control", map[string]any{"root": root, "candidate": position}, true, "accept", func(map[string]any) {
		other, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlReset})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.SubmitControl(other); err != nil {
			t.Fatal(err)
		}
	})
	if result["isError"] != true {
		t.Fatal("concurrent revision change accepted a stale confirmation", result)
	}
	current, err := f.Show()
	if err != nil || len(current.PendingAsks) != 2 {
		t.Fatal("stale confirmation settled an Ask", err, current)
	}
}

func TestConversationStartUsesOnlyTheDisplayedCompleteMaterial(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	saveReviewOnlyConfiguration(t, root)
	cliStartRepo(t, root)
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, "", "intake"), mustJSON(t, preview.IntakeCandidate))
	if err != nil {
		t.Fatal(err)
	}
	var output, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--run-id", "conversation-cli", "--configuration", "仅评审", "--intake", "1"}, IO{Stdout: &output, Stderr: &stderr}); code != 0 {
		t.Fatal("conversation-authorized CLI startup failed", code, stderr.String())
	}
	var cliRequest facade.StartRequest
	if err := readJSONFileInto(filepath.Join(root, facade.EngineNamespace, "conversation-cli", "request.json"), &cliRequest); err != nil || cliRequest.IntakeConfirmationReceipt.Confirmation != "conversation" || cliRequest.IntakeConfirmationReceipt.Digest != preview.IntakeCandidate.Digest {
		t.Fatal("CLI startup lost its exact intake or conversation provenance", cliRequest, err)
	}
	arguments := map[string]any{"root": root, "intake": position, "configuration": "仅评审"}
	before, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), preview.Content+"\nchanged after display\n")
	result := confirmationCall(t, "confirm_start", arguments, false, "cancel", nil)
	after, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*", "state.json"))
	if err != nil || result["isError"] != true || !reflect.DeepEqual(before, after) {
		t.Fatal("changed displayed material created a run", result, err)
	}
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), preview.Content)
	elicitations := 0
	result = serveConfirmationCall(t, nil, "confirm_start", arguments, true, "cancel", nil, &elicitations)
	runID := startedToolRunID(t, result)
	if elicitations != 0 {
		t.Fatal("startup requested a native form", elicitations)
	}
	var request facade.StartRequest
	body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, runID, "request.json"))
	if err != nil || json.Unmarshal(body, &request) != nil || request.RunID != runID || request.FlowSelection == nil || request.IntakeConfirmationReceipt.Digest != preview.IntakeCandidate.Digest || request.IntakeConfirmationReceipt.Confirmation != "conversation" {
		t.Fatal("started run lost its exact displayed inputs or provenance", request, err)
	}
}

func TestWorkflowConfirmationStartsBindIndependentRunIdentities(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	saveReviewOnlyConfiguration(t, root)
	cliStartRepo(t, root)
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, "", "intake"), mustJSON(t, preview.IntakeCandidate))
	if err != nil {
		t.Fatal(err)
	}
	arguments := mustJSON(t, map[string]any{"root": root, "intake": position, "configuration": "仅评审"})
	var starts [2]struct {
		id  string
		err error
	}
	ready := make(chan struct{})
	var workers sync.WaitGroup
	for i := range starts {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-ready
			result, err := callConfirmationTool("confirm_start", arguments, IO{}, func(any) error {
				return fmt.Errorf("startup requested a native form")
			}, "", "")
			if err == nil {
				var view struct {
					RunID string `json:"runId"`
				}
				var body []byte
				body, err = json.Marshal(result)
				if err == nil {
					err = json.Unmarshal(body, &view)
					starts[i].id = view.RunID
				}
			}
			starts[i].err = err
		}(i)
	}
	close(ready)
	workers.Wait()
	if starts[0].id == "" || starts[1].id == "" || starts[0].id == starts[1].id {
		t.Fatalf("independent confirmations did not bind independent run identities: %+v", starts)
	}
	for _, start := range starts {
		if start.err != nil {
			t.Fatal("independent confirmation could not start its displayed run", start.err)
		}
		var request facade.StartRequest
		if err := readJSONFileInto(filepath.Join(root, facade.EngineNamespace, start.id, "request.json"), &request); err != nil {
			t.Fatal(err)
		}
		if request.RunID != start.id || request.FlowSelection == nil || request.IntakeConfirmationReceipt.Digest != preview.IntakeCandidate.Digest {
			t.Fatal("started run lost its exact displayed identity or intake", request, start.id)
		}
	}
}

func TestWorkflowStartPreservesInstallationAdmissionBeforeRunCreation(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	saveReviewOnlyConfiguration(t, root)
	cliStartRepo(t, root)
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, "", "intake"), mustJSON(t, preview.IntakeCandidate))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, executable, wantError string
	}{
		{name: "registered launcher conversational startup", executable: os.Args[0]},
		{name: "unregistered candidate", executable: filepath.Join(t.TempDir(), "formal-gates"), wantError: "UNREGISTERED_INSTALL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := os.Args[0]
			os.Args[0] = test.executable
			t.Cleanup(func() { os.Args[0] = original })
			before := snapshotEngineRunTree(t, root, "writer-exclusive")
			var output, stderr bytes.Buffer
			startID := "rejected-start"
			if test.wantError == "" {
				startID = "conversation-cli"
			}
			code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--run-id", startID, "--configuration", "仅评审", "--intake", strconv.Itoa(position)}, IO{Stdout: &output, Stderr: &stderr})
			if test.wantError == "" {
				if code != 0 {
					t.Fatal("registered CLI startup failed", code, stderr.String())
				}
				var request facade.StartRequest
				if err := readJSONFileInto(filepath.Join(root, facade.EngineNamespace, startID, "request.json"), &request); err != nil || request.IntakeConfirmationReceipt.Confirmation != "conversation" || request.IntakeConfirmationReceipt.Digest != preview.IntakeCandidate.Digest {
					t.Fatal("CLI startup changed the intake binding", request, err)
				}
				return
			}
			if code == 0 || output.Len() != 0 || !strings.Contains(stderr.String(), test.wantError) || !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
				t.Fatal("start lost its original pre-state rejection", code, output.String(), stderr.String())
			}
			if _, err := os.Stat(filepath.Join(root, facade.EngineNamespace, "rejected-start")); !os.IsNotExist(err) {
				t.Fatal("rejected start created a run directory", err)
			}
			if test.wantError == "UNREGISTERED_INSTALL" {
				before, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*"))
				if err != nil {
					t.Fatal(err)
				}
				result := confirmationCall(t, "confirm_start", map[string]any{"root": root, "intake": position, "configuration": "仅评审"}, true, "accept", func(map[string]any) { t.Fatal("unregistered candidate opened a confirmation") })
				message := result["content"].([]any)[0].(map[string]any)["text"].(string)
				if result["isError"] != true || !strings.Contains(message, "UNREGISTERED_INSTALL") {
					t.Fatal("native capability bypassed installation admission", result)
				}
				after, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*"))
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("unregistered native candidate created a run", after, err)
				}
			}
		})
	}
}

func TestWorkflowConfirmationStartUsesOriginalNativeBaseSelection(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	saveReviewOnlyConfiguration(t, root)
	cliGit(t, root, "init", "-q")
	cliGit(t, root, "config", "user.email", "test@example.test")
	cliGit(t, root, "config", "user.name", "Test User")
	cliGit(t, root, "add", "requirements.md")
	cliGit(t, root, "commit", "-q", "-m", "base")
	base := cliGit(t, root, "rev-parse", "HEAD")
	mustWriteCLI(t, filepath.Join(root, "work.txt"), "current work\n")
	cliGit(t, root, "add", "work.txt")
	cliGit(t, root, "commit", "-q", "-m", "current")
	head := cliGit(t, root, "rev-parse", "HEAD")
	cliGit(t, root, "checkout", "-q", "--detach", base)
	mustWriteCLI(t, filepath.Join(root, "other.txt"), "divergent work\n")
	cliGit(t, root, "add", "other.txt")
	cliGit(t, root, "commit", "-q", "-m", "divergent")
	divergent := cliGit(t, root, "rev-parse", "HEAD")
	cliGit(t, root, "checkout", "-q", "--detach", head)
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, "", "intake"), mustJSON(t, preview.IntakeCandidate))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, configuration, base, vcs, wantBase, wantVCS, wantInitial, wantError string
	}{
		{name: "current native snapshot", configuration: settings.DefaultConfigurationName, wantBase: head, wantVCS: "git", wantInitial: head},
		{name: "explicit ancestor", configuration: settings.DefaultConfigurationName, base: strings.ToUpper(base), vcs: "GIT", wantBase: base, wantVCS: "git", wantInitial: head},
		{name: "restricted selection still binds base", configuration: "仅评审", base: "not-a-native-identity", vcs: "git", wantError: "invalid git snapshot identity"},
		{name: "invalid native identity", configuration: settings.DefaultConfigurationName, base: "not-a-native-identity", vcs: "git", wantError: "invalid git snapshot identity"},
		{name: "divergent native identity", configuration: settings.DefaultConfigurationName, base: divergent, vcs: "git", wantError: "is not an ancestor of the current snapshot"},
		{name: "unsupported native vcs", configuration: settings.DefaultConfigurationName, base: base, vcs: "unsupported-vcs", wantError: `unsupported VCS "unsupported-vcs"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*", "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			arguments := map[string]any{"root": root, "intake": position, "configuration": test.configuration}
			if test.base != "" {
				arguments["baseSnapshot"] = test.base
			}
			if test.vcs != "" {
				arguments["vcs"] = test.vcs
			}
			result := confirmationCall(t, "confirm_start", arguments, false, "cancel", nil)
			if test.wantError != "" {
				message := result["content"].([]any)[0].(map[string]any)["text"].(string)
				if result["isError"] != true || !strings.Contains(message, test.wantError) {
					t.Fatal("native selection lost its original error", result, test.wantError)
				}
				after, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*", "state.json"))
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("invalid native selection created a run", after, err)
				}
				return
			}
			runID := startedToolRunID(t, result)
			var request facade.StartRequest
			if err := readJSONFileInto(filepath.Join(root, facade.EngineNamespace, runID, "request.json"), &request); err != nil {
				t.Fatal(err)
			}
			var saved struct {
				Content protocol.State `json:"content"`
			}
			data, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, runID, "state.json"))
			if err != nil || json.Unmarshal(data, &saved) != nil {
				t.Fatal("the original start did not persist its native selection", err)
			}
			if request.BaseSnapshot != test.wantBase || request.VCS != test.wantVCS || saved.Content.BaseSnapshot != test.wantBase || saved.Content.InitialSnapshot != test.wantInitial {
				t.Fatal("the native selection changed between confirmation and its authoritative start", request, saved.Content.BaseSnapshot, saved.Content.InitialSnapshot)
			}
		})
	}
}

func TestWorkflowConfirmationDeliversOnlyTheOriginalPendingNativeAnswer(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	job := filepath.Join(root, facade.EngineNamespace, run.RunID, "jobs", "permission")
	pending := `[{"connection":1,"request_id":"original-permission","status":"pending","method":"session/request_permission","summary":"permission for original operation","toolCall":{"toolCallId":"original-tool","rawInput":{"command":"original-operation"}},"options":[{"optionId":"allow-once","name":"Allow once","kind":"allow_once"}]}]`
	mustWriteCLI(t, filepath.Join(job, "interaction-pending.json"), pending)
	var output, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"native", "respond", "--root", root, "--question", "1", "--choice", "1"}, IO{Stdout: &output, Stderr: &stderr}); code != 0 {
		t.Fatal(code, stderr.String())
	}
	directory, err := nativeRequestPayloadDirectory(filepath.Join(root, facade.EngineNamespace, run.RunID), 1)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := facade.OriginalPayloadPaths(filepath.Join(directory, "decisions"))
	if err != nil || len(paths) != 1 {
		t.Fatal("native preparation did not save its original candidate", paths, err)
	}
	for _, args := range [][]string{
		{"native", "respond", "--root", root, "--decision", "1"},
		{"native", "respond", "--decision-file", paths[0]},
		{"harness", "native-respond", "--decision-file", paths[0]},
	} {
		output.Reset()
		stderr.Reset()
		code := 0
		if args[0] == "harness" {
			code = harness.RunCommand(args[1:], strings.NewReader(""), &output, &stderr)
		} else {
			code = Run("formal-gates", args, IO{Stdout: &output, Stderr: &stderr})
		}
		if code == 0 || !strings.Contains(stderr.String(), "NATIVE_CONFIRMATION_UNAVAILABLE") {
			t.Fatal("native CLI approved its own saved answer", args, code, stderr.String())
		}
	}
	arguments := map[string]any{"root": root, "question": 1, "decision": 1}
	check := func(value map[string]any) {
		if value["pending"].(map[string]any)["request_id"] != "original-permission" || value["selection"] != "Allow once" {
			t.Fatal("native confirmation lost the original pending question or selection", value)
		}
	}
	result := confirmationCall(t, "confirm_native", arguments, true, "cancel", check)
	if result["isError"] != true {
		t.Fatal("cancelled native answer was not stopped", result)
	}
	result = confirmationCall(t, "confirm_native", arguments, true, "accept", func(value map[string]any) {
		check(value)
		mustWriteCLI(t, filepath.Join(job, "interaction-pending.json"), strings.ReplaceAll(pending, "original-permission", "replacement-permission"))
	})
	if result["isError"] != true {
		t.Fatal("changed pending question reused approval", result)
	}
	if _, err := os.Stat(filepath.Join(job, "interaction-answer.json")); !os.IsNotExist(err) {
		t.Fatal("unconfirmed or replaced native question received an answer", err)
	}
	mustWriteCLI(t, filepath.Join(job, "interaction-pending.json"), pending)
	result = confirmationCall(t, "confirm_native", arguments, true, "accept", check)
	if result["isError"] != false {
		t.Fatal("native host could not directly deliver its confirmed answer", result)
	}
	var answer map[string]any
	body, err := os.ReadFile(filepath.Join(job, "interaction-answer.json"))
	if err != nil || json.Unmarshal(body, &answer) != nil || answer["request_id"] != "original-permission" || answer["option"] != "allow-once" {
		t.Fatal("native answer changed its original request or choice", answer, err)
	}
}

func TestConfirmStartPackageRootSelectsRegisteredHost(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	source := writeInstallSource(t, "source")
	if err := os.Remove(filepath.Join(source, "prompts", "actions", "sample-action.md")); err != nil {
		t.Fatal(err)
	}
	for _, id := range prompts.RequiredActionIDs {
		mustWriteCLI(t, filepath.Join(source, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	for _, platform := range []string{"claude", "codex"} {
		var stdout, stderr bytes.Buffer
		if code := runInstalledFixture(t, []string{"install", "--source", source, "--host", platform, "--scope", "global", "--force"}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
			t.Fatalf("install %s failed (rc=%d): %s%s", platform, code, stdout.String(), stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
		if code := runInstalledFixture(t, []string{"install", "--source", source, "--host", platform, "--scope", "global", "--bootstrap"}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
			t.Fatalf("bootstrap %s failed (rc=%d): %s%s", platform, code, stdout.String(), stderr.String())
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Args[0]
	os.Args[0] = filepath.Join(home, ".local", "bin", installTestBinaryName())
	t.Cleanup(func() { os.Args[0] = original })
	codexTarget := filepath.Join(home, ".codex", "skills", "formal-gates")
	saveReviewOnlyConfiguration(t, root)
	cliStartRepo(t, root)
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliStructuredRequirement("Engine admission selects the installed package root.")+"\n## 方案\n\nUse the installed package root.\n")
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, "", "intake"), mustJSON(t, preview.IntakeCandidate))
	if err != nil {
		t.Fatal(err)
	}
	arguments := mustJSON(t, map[string]any{"root": root, "intake": position, "configuration": "仅评审"})
	confirm := func(any) error { return nil }
	if _, err := callConfirmationTool("confirm_start", arguments, IO{}, confirm, "", ""); err == nil || !strings.Contains(err.Error(), "conflicting active registry records") {
		t.Fatalf("shared launcher admission did not conflict: %v", err)
	}
	admission, err := validate.ResolveEngineAdmission(root, codexTarget)
	if err != nil || admission.Provider != "codex" {
		t.Fatalf("package root did not select the codex record: %+v %v", admission, err)
	}
	if _, err := callConfirmationTool("confirm_start", arguments, IO{}, confirm, "", codexTarget); err != nil && strings.Contains(err.Error(), "conflicting active registry records") {
		t.Fatalf("package root admission still conflicts: %v", err)
	}
}

// installConfirmationHost installs the named host at project scope the way its
// launcher registers it and returns the installed package target; the fixture
// keeps the registered launcher as the process identity for later engine opens.
func installConfirmationHost(t *testing.T, root, hostName string) string {
	t.Helper()
	clearHostEnv(t)
	source := writeInstallSource(t, "Host permission fixture")
	if err := os.Remove(filepath.Join(source, "prompts", "actions", "sample-action.md")); err != nil {
		t.Fatal(err)
	}
	for _, id := range prompts.RequiredActionIDs {
		mustWriteCLI(t, filepath.Join(source, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Args[0]
	os.Args[0] = filepath.Join(home, ".local", "bin", installTestBinaryName())
	t.Cleanup(func() { os.Args[0] = original })
	option := validate.InstallOptions{Source: source, Host: hostName, Scope: "project", Project: root, BinaryTarget: os.Args[0], Force: true}
	report, err := validate.Install(option)
	if err != nil {
		t.Fatal(err)
	}
	target := report.Targets[0].TargetPath
	option.Bootstrap = true
	if _, err := validate.Install(option); err != nil {
		t.Fatal(err)
	}
	// Path freezing resolves the installed host's native executable from its own
	// configuration fields; the test binary is that executable so the fixture
	// does not depend on an installed host CLI.
	adapter, err := host.LookupAdapter(hostName)
	if err != nil {
		t.Fatal(err)
	}
	fixture, ok := adapter.(host.NativeFixtureProvider)
	if !ok {
		t.Fatalf("native path fixture unavailable for %q", hostName)
	}
	native, err := fixture.FixtureNativePaths(host.NativeFixture{Executable: executable})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gates", "config.json")
	values := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &values); err != nil {
			t.Fatal(err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for field, value := range native {
		values[field] = value
	}
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return target
}

// hostPermissionProject returns a project with a saved intake candidate and a
// startable configuration ready for a confirmation tool call.
func hostPermissionProject(t *testing.T, root string) (position int) {
	t.Helper()
	saveReviewOnlyConfiguration(t, root)
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliStructuredRequirement("Confirm through the host permission gate.")+"\n## 方案\n\nRecord the confirming channel with the run.\n")
	cliStartRepo(t, root)
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, position, err = facade.SaveOriginalPayload(workflowPayloadDirectory(root, "", "intake"), mustJSON(t, preview.IntakeCandidate))
	if err != nil {
		t.Fatal(err)
	}
	return position
}

// Startup on a permission-gate host uses the same conversation provenance
// regardless of whether the connection advertises forms.
func TestConversationStartOnPermissionHostSkipsElicitation(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	target := installConfirmationHost(t, root, "devin")
	position := hostPermissionProject(t, root)
	for _, capability := range []bool{false, true} {
		elicitation := 0
		result := serveConfirmationCall(t, []string{"--package-root", target}, "confirm_start", map[string]any{"root": root, "intake": position, "configuration": "仅评审"}, capability, "accept", nil, &elicitation)
		if result["isError"] != false {
			t.Fatalf("permission-gate start failed with capability=%v: %v", capability, result)
		}
		if elicitation != 0 {
			t.Fatalf("permission-gate start emitted %d elicitation requests with capability=%v", elicitation, capability)
		}
		message := result["content"].([]any)[0].(map[string]any)["text"].(string)
		var view map[string]any
		if err := json.Unmarshal([]byte(message), &view); err != nil || view["runId"] == "" {
			t.Fatalf("start output lost its run identity: %q %v", message, err)
		}
		runID := view["runId"].(string)
		var request facade.StartRequest
		if err := readJSONFileInto(filepath.Join(root, facade.EngineNamespace, runID, "request.json"), &request); err != nil || request.IntakeConfirmationReceipt.Confirmation != "conversation" {
			t.Fatalf("start request lost the conversation source: %+v %v", request, err)
		}
		f, err := facade.Open(root, runID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Drive(); err != nil {
			t.Fatal(err)
		}
		var state struct {
			Content protocol.State `json:"content"`
		}
		body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, runID, "state.json"))
		if err != nil || json.Unmarshal(body, &state) != nil || state.Content.IntakeReceipt == nil || state.Content.IntakeReceipt.Confirmation.Confirmation != "conversation" {
			t.Fatalf("run state lost the conversation intake source: %+v %v", state.Content.IntakeReceipt, err)
		}
	}
}

// Control decisions and native answers retain the serving host permission
// source independently of conversational startup; replay stays idempotent.
func TestHostPermissionConfirmControlAndNativeAnswer(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	for _, platform := range []string{"devin", "dsh"} {
		t.Run(platform, func(t *testing.T) { checkHostPermissionConfirmControlAndNativeAnswer(t, platform) })
	}
}

func checkHostPermissionConfirmControlAndNativeAnswer(t *testing.T, platform string) {
	t.Helper()
	root := t.TempDir()
	target := installConfirmationHost(t, root, platform)
	position := hostPermissionProject(t, root)
	elicitation := 0
	result := serveConfirmationCall(t, []string{"--package-root", target}, "confirm_start", map[string]any{"root": root, "intake": position, "configuration": "仅评审"}, false, "accept", nil, &elicitation)
	if result["isError"] != false {
		t.Fatalf("permission-gate start failed: %v", result)
	}
	message := result["content"].([]any)[0].(map[string]any)["text"].(string)
	var view map[string]any
	if err := json.Unmarshal([]byte(message), &view); err != nil || view["runId"] == "" {
		t.Fatalf("start output lost its run identity: %q %v", message, err)
	}
	runID := view["runId"].(string)
	f, err := facade.Open(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, facade.EngineNamespace, runID)
	job := filepath.Join(directory, "jobs", "interaction")
	if err := os.MkdirAll(job, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(job, "interaction-pending.json"), `[{"connection":1,"method":"session/request_permission","options":[{"optionId":"allow-once","name":"Allow once","kind":"allow_once"}],"request_id":"41","status":"pending"}]`)
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"native", "respond", "--root", root, "--question", "1", "--choice", "1"}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("decision save failed: %s", stderr.String())
	}
	result = serveConfirmationCall(t, []string{"--package-root", target}, "confirm_native", map[string]any{"root": root, "run": 1, "question": 1, "decision": 1}, false, "accept", nil, &elicitation)
	if result["isError"] != false {
		t.Fatalf("permission-gate native answer failed: %v", result)
	}
	var answer map[string]any
	if err := readJSONFileInto(filepath.Join(job, "interaction-answer.json"), &answer); err != nil || answer["request_id"] != "41" || answer["option"] != "allow-once" || answer["confirmation"] != "host-permission" {
		t.Fatalf("delivered native answer lost the host-permission source: %v %v", answer, err)
	}
	request, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	if _, pending, err := f.SubmitControl(request); err != nil || len(pending.PendingAsks) != 1 {
		t.Fatal("control request did not create its Ask", pending, err)
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "decline"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, runID, "control"), mustJSON(t, candidate)); err != nil {
		t.Fatal(err)
	}
	arguments := map[string]any{"root": root, "run": 1, "candidate": 1}
	result = serveConfirmationCall(t, []string{"--package-root", target}, "confirm_control", arguments, false, "accept", nil, &elicitation)
	if result["isError"] != false {
		t.Fatalf("permission-gate control submit failed: %v", result)
	}
	var state struct {
		Content protocol.State `json:"content"`
	}
	body, err := os.ReadFile(filepath.Join(directory, "state.json"))
	if err != nil || json.Unmarshal(body, &state) != nil {
		t.Fatal(err)
	}
	confirmed := 0
	for _, record := range state.Content.Events {
		if record.Acceptance.Confirmation == "host-permission" {
			confirmed++
		}
	}
	if confirmed != 1 {
		t.Fatalf("control decision lost its host-permission source: %d acceptances", confirmed)
	}
	before := snapshotEngineRunTree(t, root, runID)
	result = serveConfirmationCall(t, []string{"--package-root", target}, "confirm_control", arguments, false, "accept", nil, &elicitation)
	if result["isError"] != false {
		t.Fatalf("replayed control submit failed: %v", result)
	}
	if after := snapshotEngineRunTree(t, root, runID); !reflect.DeepEqual(before, after) {
		t.Fatal("replayed host-permission control changed the run")
	}
	if elicitation != 0 {
		t.Fatalf("permission-gate tools emitted %d elicitation requests", elicitation)
	}
}

// Conversational startup does not depend on a host form capability. The
// persisted receipt records its source without asserting native approval.
func TestConversationStartDoesNotRequestNativeConfirmation(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	target := installConfirmationHost(t, root, "codex")
	position := hostPermissionProject(t, root)
	elicitations := 0
	result := serveConfirmationCall(t, []string{"--package-root", target}, "confirm_start", map[string]any{"root": root, "intake": position, "configuration": "仅评审"}, false, "cancel", nil, &elicitations)
	if result["isError"] != false || elicitations != 0 {
		t.Fatalf("conversation-authorized startup requested a native confirmation: %v forms=%d", result, elicitations)
	}
	var view struct {
		RunID string `json:"runId"`
	}
	message := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if err := json.Unmarshal([]byte(message), &view); err != nil || view.RunID == "" {
		t.Fatal("start lost its run identity", message, err)
	}
	var request facade.StartRequest
	if err := readJSONFileInto(filepath.Join(root, facade.EngineNamespace, view.RunID, "request.json"), &request); err != nil || request.IntakeConfirmationReceipt.Confirmation != "conversation" {
		t.Fatal("start receipt must record conversation provenance", request, err)
	}
}

// A serve started with -root owns that project directory: a confirmation call
// that omits its own root still displays and starts the serve's project.
func TestWorkflowServeRootSuppliesOmittedProjectRoot(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	saveReviewOnlyConfiguration(t, root)
	cliStartRepo(t, root)
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(root, "", "intake"), mustJSON(t, preview.IntakeCandidate))
	if err != nil {
		t.Fatal(err)
	}
	result := serveConfirmationCall(t, []string{"--root", root}, "confirm_start", map[string]any{"intake": position, "configuration": "仅评审"}, false, "cancel", nil, nil)
	runID := startedToolRunID(t, result)
	var request facade.StartRequest
	if err := readJSONFileInto(filepath.Join(root, facade.EngineNamespace, runID, "request.json"), &request); err != nil || request.RunID != runID {
		t.Fatal("omitted call root did not persist the run in the serve's project", request, err)
	}
	if _, err := os.Stat(filepath.Join(root, facade.EngineNamespace, runID, "state.json")); err != nil {
		t.Fatal("the serve's project did not receive its run", err)
	}
}
