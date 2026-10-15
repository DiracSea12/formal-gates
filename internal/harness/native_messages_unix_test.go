//go:build !windows

package harness

// Native peers acknowledge the exact submitted input. Receipt interruption
// leaves the original payload and acknowledgement available to recovery.
import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
)

func supplementaryMaterial(t *testing.T, directory string, request map[string]any) {
	t.Helper()
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: "engine.gate.v3", Provider: textOf(request["provider"]), Pending: protocol.PendingAction{ActionID: "issued-action", AttemptID: "issued-attempt"}, Validation: &protocol.ValidationInput{}}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(directory), "material.json")
	if err := writeAtomicJSON(path, worker.Material{ReadOnly: true, Input: input, ResultSchema: schema}); err != nil {
		t.Fatal(err)
	}
	request["read_only"] = true
	request["schema"] = schema
	request["engine"] = map[string]any{"workerMaterial": map[string]any{"path": path, "actionId": input.Pending.ActionID, "attemptId": input.Pending.AttemptID}}
}

func supplementaryServices(t *testing.T, directory string, identity string, receipt func(protocol.TaskMessage) error) ExecutionServices {
	t.Helper()
	claimed := false
	return ExecutionServices{
		ClaimMessage: func(actual, session string, begin bool) (*protocol.TaskMessage, error) {
			if actual != directory || session != identity || begin == claimed {
				t.Fatalf("original claim differs: %s %s %t", actual, session, begin)
			}
			if claimed {
				return nil, nil
			}
			claimed = true
			return &protocol.TaskMessage{ID: "original-message", ActionID: "issued-action", AttemptID: "issued-attempt", Identity: identity, Text: "Original supplementary text\n原文  ", Status: "SENDING", Revision: 7}, nil
		},
		ReceiptMessage: func(actual string, message protocol.TaskMessage) error {
			if actual != directory || message.ID != "original-message" || message.ActionID != "issued-action" || message.AttemptID != "issued-attempt" || message.Identity != identity || message.Text != "Original supplementary text\n原文  " || message.Revision != 7 {
				t.Fatalf("original receipt differs: %s %+v", actual, message)
			}
			return receipt(message)
		},
	}
}

func TestNativeSupplementaryInputAcknowledgementPrecedesResult(t *testing.T) {
	for _, provider := range []string{"dsh", "zcode", "zcode-unacknowledged"} {
		t.Run(provider, func(t *testing.T) {
			var request map[string]any
			var directory, echo string
			var execute func(any, string, bool, ...ExecutionServices) (map[string]any, error)
			if provider == "dsh" {
				steps := successSteps()
				steps[1].On = `"method":"session/resume"`
				steps[3].Semantics = nil
				steps[3].Send = `{"id": "@ID@", "result": {"stopReason": "max_tokens"}}`
				scenario := newDSHScenario(t, steps, func(request map[string]any) { request["identity"] = "s-1" })
				request, directory, echo, execute = scenario.request, scenario.directory, scenario.echoPath, dshExecute
			} else {
				scenario := newZcodeScenario(t, func(input string) []peerStep {
					steps := zcodeSuccessSteps(input)
					steps[0].On = `"method":"session/resume"`
					if provider == "zcode-unacknowledged" {
						steps[2] = peerStep{On: `"method":"session/send"`, Close: true}
					} else {
						steps[5] = peerStep{On: `"method":"session/send"`, Close: true}
					}
					return steps
				}, func(request map[string]any) { request["identity"] = "s-1" })
				request, directory, echo, execute = scenario.request, scenario.directory, scenario.echoPath, zcodeExecute
			}
			supplementaryMaterial(t, directory, request)
			if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
				t.Fatal(err)
			}
			receipts := []protocol.TaskMessage{}
			services := supplementaryServices(t, directory, "s-1", func(message protocol.TaskMessage) error { receipts = append(receipts, message); return nil })
			prepared, err := queueNativeMessages(directory, request, []ExecutionServices{services})
			if err != nil {
				t.Fatal(err)
			}
			_, turnErr := execute(prepared, directory, false, services)
			if turnErr == nil {
				t.Fatal("peer unexpectedly completed the result")
			}
			t.Logf("provider=%s native_error=%v receipts=%+v", provider, turnErr, receipts)
			expected := 1
			if provider == "zcode-unacknowledged" {
				expected = 0
			}
			if len(receipts) != expected || expected == 1 && receipts[0].Status != "ACCEPTED" {
				t.Fatalf("acknowledgement was coupled to result: %+v", receipts)
			}
			raw, err := os.ReadFile(echo)
			if err != nil {
				t.Fatal(err)
			}
			prompt := request["prompt"].(string) + "\n\n[Supplementary user message]\nOriginal supplementary text\n原文  "
			method := "session/send"
			params := map[string]any{"sessionId": "s-1", "inputId": "formal-gates-" + filepath.Base(directory), "content": prompt}
			if provider == "dsh" {
				method = "session/prompt"
				params = map[string]any{"sessionId": "s-1", "prompt": []any{map[string]any{"type": "text", "text": prompt}}}
			}
			frame := frameByMethod(t, string(raw), method)
			if !valuesEqual(getOf(frame, "params"), params) {
				t.Fatalf("native input differs: %v != %v", frame, params)
			}
			if expected == 0 {
				if err := settleQueuedMessages(directory, request, turnErr, []ExecutionServices{services}); err != nil {
					t.Fatal(err)
				}
				if len(receipts) != 1 || receipts[0].Status != "FAILED" || !strings.Contains(receipts[0].Reason, "delivery outcome unknown; not automatically resent") || !strings.Contains(receipts[0].Reason, turnErr.Error()) {
					t.Fatalf("missing acknowledgement became a known outcome: %+v", receipts)
				}
			}
		})
	}
}

func TestCodexSupplementaryReceiptRecoversOriginalAcknowledgement(t *testing.T) {
	f, ref, scenario, binary := engineNativeFixture(t, true)
	scenario.peer.Mode = "bad_json"
	scenario.save(t)
	command := exec.Command(binary, "workflow", "drive", "--root", scenario.workspace, "--run-id", scenario.runID, "--machine")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("original native failure: %v %s", err, output)
	}
	message, err := f.QueueWorkerMessage(1, "Original supplementary text\n原文  ")
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	ask := 0
	for i, request := range run.PendingAsks {
		if request.Control == protocol.ControlRecovery {
			ask = i + 1
		}
	}
	if ask == 0 {
		t.Fatal("original recovery Ask missing")
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: ask, Choice: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = confirmFacadeFixtureControl(t, f, candidate)
	if err != nil {
		t.Fatal(err)
	}
	resume := ""
	for _, intent := range run.AdapterIntents {
		if intent.Resume != nil {
			resume = intent.ActionID
		}
	}
	if resume == "" {
		t.Fatal("original resume intent missing")
	}
	request, _, directory, _, err := f.WorkerExecutionRequest(ref, resume)
	if err != nil {
		t.Fatal(err)
	}
	request["binding"] = scenario.request["binding"]
	scenario.request, scenario.directory = request, directory
	scenario.runtime = ""
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	scenario.save(t)
	effective := map[string]any{}
	for k, v := range request {
		effective[k] = v
	}
	effective["prompt"] = request["prompt"].(string) + "\n\n[Supplementary user message]\n" + message.Text
	scenario.peer.TurnParams, err = codexTurnParameters(effective, "")
	if err != nil {
		t.Fatal(err)
	}
	delete(scenario.peer.TurnParams, "threadId")
	if err := writeAtomicJSON(filepath.Join(scenario.config, "peer.json"), scenario.peer); err != nil {
		t.Fatal(err)
	}
	services := ExecutionServices{Admit: facade.AdmitExecution, ClaimMessage: facade.PendingNativeMessage, ReceiptMessage: func(actual string, receipt protocol.TaskMessage) error {
		if actual != directory || receipt.ID != message.ID || receipt.ActionID != message.ActionID || receipt.AttemptID != message.AttemptID || receipt.Identity != message.Identity || receipt.Text != message.Text || receipt.Revision != message.Revision || receipt.Status != "ACCEPTED" {
			t.Errorf("original receipt differs: %s %+v", actual, receipt)
		}
		runtime.Goexit()
		return nil
	}}
	interrupted := make(chan struct{})
	go func() {
		defer close(interrupted)
		_, err := runNativeJob(directory, services)
		t.Errorf("receipt interruption did not stop invocation: %v", err)
	}()
	<-interrupted
	current, err := f.Show()
	if err != nil || current.TaskMessages[message.ID].Status != "SENDING" {
		t.Fatalf("interruption completed delivery: %+v %v", current.TaskMessages[message.ID], err)
	}
	source, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil || getOf(getOf(source, "turn_ack"), "id") == nil {
		t.Fatalf("original acknowledged turn missing: %v %v", source, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "source.json")); !os.IsNotExist(err) {
		t.Fatal("interrupted invocation fabricated a completed result")
	}
	f, err = facade.Open(scenario.workspace, scenario.runID)
	if err != nil {
		t.Fatal(err)
	}
	recovery := ExecutionServices{ReceiptMessage: facade.RecordNativeMessage}
	var acceptedRevision uint64
	for i := range 2 {
		_, reconcileErr := codexReconcile(directory, map[string]any{"exit_code": 1, "error": "invocation interrupted before message receipt"}, recovery)
		current, err = f.Show()
		delivery := current.TaskMessages[message.ID]
		expected := message
		expected.Status = "ACCEPTED"
		if err != nil || reconcileErr == nil || !strings.Contains(reconcileErr.Error(), "original native process did not complete successfully") || delivery != expected || nativePeerTurns(t, scenario) != 2 || i == 1 && current.Revision != acceptedRevision {
			t.Fatalf("recovery conflated input with result: %+v %v %v", delivery, reconcileErr, err)
		}
		acceptedRevision = current.Revision
	}
	t.Logf("original_turn_ack=%v recovered_delivery=%+v", getOf(source, "turn_ack"), current.TaskMessages[message.ID])
}
