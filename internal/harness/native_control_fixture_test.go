//go:build !windows

package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

// These peers simulate a host protocol response; they do not prove a human UI.
func exchangeConfirmationFixture(t *testing.T, input io.Reader, output io.Writer, name string, arguments any, check func(map[string]any)) ([]byte, error) {
	t.Helper()
	encoder, decoder := json.NewEncoder(output), json.NewDecoder(input)
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"elicitation": map[string]any{"form": map[string]any{}}}}}); err != nil {
		return nil, err
	}
	var initialized map[string]any
	if err := decoder.Decode(&initialized); err != nil {
		return nil, err
	}
	if initialized["id"] != float64(1) || getOf(getOf(initialized, "result"), "serverInfo") == nil {
		return nil, fmt.Errorf("unexpected service initialization: %v", initialized)
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}}); err != nil {
		return nil, err
	}
	for {
		var frame map[string]any
		if err := decoder.Decode(&frame); err != nil {
			return nil, err
		}
		if frame["method"] == "elicitation/create" {
			if name == "confirm_start" {
				return nil, fmt.Errorf("conversation-authorized startup unexpectedly requested a native form")
			}
			params := objectOrEmpty(frame["params"])
			schema := objectOrEmpty(params["requestedSchema"])
			if params["mode"] != "form" || len(schema["properties"].(map[string]any)) != 0 || schema["required"] != nil {
				return nil, fmt.Errorf("service omitted its original confirmation-only schema: %v", frame)
			}
			message := textOf(params["message"])
			if utf8.RuneCountInString(message) > 800 || strings.Count(message, "\n")+1 > 12 {
				return nil, fmt.Errorf("native confirmation exceeded its display budget")
			}
			var original map[string]any
			message = strings.TrimPrefix(message, "Confirm this exact operation:\n")
			if err := json.Unmarshal([]byte(message), &original); err != nil {
				return nil, fmt.Errorf("service did not display a confirmation summary: %w", err)
			}
			if check != nil {
				check(original)
			}
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": frame["id"], "result": map[string]any{"action": "accept", "content": map[string]any{}}}); err != nil {
				return nil, err
			}
			continue
		}
		if frame["id"] != float64(2) {
			return nil, fmt.Errorf("response belongs to another call: %v", frame)
		}
		if frame["error"] != nil {
			return nil, fmt.Errorf("%v", frame["error"])
		}
		result := objectOrEmpty(frame["result"])
		var body []byte
		for _, content := range listOf(result["content"]) {
			if getOf(content, "type") == "text" {
				body = append(body, []byte(textOf(getOf(content, "text")))...)
			}
		}
		if result["isError"] == true {
			return body, fmt.Errorf("%s", body)
		}
		return body, nil
	}
}

func installedConfirmationFixture(t *testing.T, command *exec.Cmd, tool string, arguments any, check func(map[string]any)) ([]byte, error) {
	t.Helper()
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	defer command.Process.Kill()
	body, exchangeErr := exchangeConfirmationFixture(t, output, input, tool, arguments, check)
	_ = input.Close()
	waitErr := command.Wait()
	if exchangeErr != nil {
		return body, fmt.Errorf("%w; service stderr=%s", exchangeErr, stderr.String())
	}
	if waitErr != nil {
		return body, fmt.Errorf("%w; service stderr=%s", waitErr, stderr.String())
	}
	return body, nil
}

func fixtureControlPosition(t *testing.T, root, owner string, candidate facade.ControlCandidate) int {
	t.Helper()
	position, err := facade.SaveOriginalCandidate(filepath.Join(root, facade.EngineNamespace, owner, "payloads", "control"), candidate)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := facade.OriginalPayloadPaths(filepath.Join(root, facade.EngineNamespace, owner, "payloads", "control"))
	if err != nil || position < 1 || position > len(paths) {
		t.Fatal("original control position does not resolve to its saved candidate", position, err)
	}
	body, err := os.ReadFile(paths[position-1])
	expected, marshalErr := json.MarshalIndent(candidate, "", "  ")
	if err != nil || marshalErr != nil || !bytes.Equal(body, append(expected, '\n')) {
		t.Fatal("saved control candidate changed its complete original payload", err, marshalErr)
	}
	return position
}

func confirmInstalledFixtureControl(t *testing.T, command *exec.Cmd, root, owner string, candidate facade.ControlCandidate) ([]byte, error) {
	t.Helper()
	position := fixtureControlPosition(t, root, owner, candidate)
	body, err := installedConfirmationFixture(t, command, "confirm_control", map[string]any{"root": root, "candidate": position}, func(displayed map[string]any) {
		checkFixtureControlDisplay(t, displayed, candidate, map[string]any{"runId": candidate.Run.RunID, "revision": candidate.Run.Revision, "status": candidate.Run.Status})
		if candidate.Input.Choice != "" {
			checkFixtureDisplayValue(t, displayed, "decision", candidate.Input.Choice)
		}
		if candidate.DevelopmentPlan != nil {
			checkFixtureDisplayValue(t, displayed, "planDigest", candidate.DevelopmentPlan.Confirmation.Digest)
		}
	})
	if err == nil {
		f, openErr := facade.Open(root, candidate.Run.RunID)
		if openErr != nil {
			return body, openErr
		}
		checkFixtureControlReplay(t, f, candidate)
	}
	return body, err
}

// Display fields may be excerpts; the saved candidate and accepted event own
// the complete target, revision, choice and plan.
func checkFixtureControlDisplay(t *testing.T, displayed map[string]any, candidate facade.ControlCandidate, run any) {
	t.Helper()
	for key, value := range map[string]any{"root": candidate.Root, "input": candidate.Input, "run": run} {
		checkFixtureDisplayValue(t, displayed, key, value)
	}
	if candidate.DevelopmentPlan != nil {
		if _, present := displayed["proposal"]; present {
			checkFixtureDisplayValue(t, displayed, "proposal", candidate.DevelopmentPlan.Proposal)
		}
	}
}

func checkFixtureDisplayValue(t *testing.T, displayed map[string]any, key string, expected any) {
	t.Helper()
	body, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(displayed[key], value) {
		return
	}
	text, ok := value.(string)
	if !ok {
		compact, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		text = string(compact)
	}
	excerpt, ok := displayed[key].(string)
	parts := strings.Split(excerpt, " \u2026 ")
	text = strings.Join(strings.Fields(text), " ")
	notice, _ := displayed["_display"].(string)
	if !ok || notice == "" || len(parts) != 2 || parts[0] == "" || parts[1] == "" || !strings.HasPrefix(text, parts[0]) || !strings.HasSuffix(text, parts[1]) {
		t.Fatalf("confirmation %s differs from the original value: %v", key, displayed[key])
	}
}

func checkFixtureControlReplay(t *testing.T, f *facade.Facade, candidate facade.ControlCandidate) {
	t.Helper()
	accepted, _, err := f.SubmitControl(candidate, func(any) error {
		return fmt.Errorf("an accepted original event unexpectedly requested confirmation again")
	})
	if err != nil || accepted.Status != "ACCEPTED" || accepted.EventID != string(candidate.Event.ID) {
		t.Fatal("confirmation did not retain acceptance of the complete original event", accepted, err)
	}
}

func confirmInstalledFixtureStart(t *testing.T, command *exec.Cmd, root, document string) (facade.Run, error) {
	t.Helper()
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		return facade.Run{}, err
	}
	position, err := facade.SaveOriginalCandidate(filepath.Join(root, ".gates", "tmp", "intake"), preview.IntakeCandidate)
	if err != nil {
		return facade.Run{}, err
	}
	if preview.Content != document || preview.Revision != preview.IntakeCandidate.RequirementRevision {
		t.Fatal("prepared intake differs from the complete fixture document")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return facade.Run{}, err
	}
	for _, entry := range command.Environ() {
		if value, ok := strings.CutPrefix(entry, "HOME="); ok {
			home = value
		}
	}
	_, configurations, err := settings.LoadConfigurations(root, home)
	if err != nil {
		return facade.Run{}, err
	}
	configuration, err := settings.SelectConfiguration(configurations, "")
	if err != nil {
		return facade.Run{}, err
	}
	baseCommand := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	baseCommand.Env = command.Env
	base, err := baseCommand.Output()
	if err != nil {
		return facade.Run{}, err
	}
	body, err := installedConfirmationFixture(t, command, "confirm_start", map[string]any{"root": root, "intake": position}, nil)
	if err != nil {
		return facade.Run{}, err
	}
	var response struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.RunID == "" {
		return facade.Run{}, fmt.Errorf("start response omitted its run identity: %s", body)
	}
	f, err := facade.Open(root, response.RunID)
	if err != nil {
		return facade.Run{}, err
	}
	run, err := f.Show()
	if err != nil {
		return run, err
	}
	var request facade.StartRequest
	requestBody, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, response.RunID, "request.json"))
	if err != nil || json.Unmarshal(requestBody, &request) != nil {
		t.Fatal("started run omitted its original request", err)
	}
	expectedReceipt := preview.IntakeCandidate
	expectedReceipt.Confirmation = "conversation"
	if request.RunID != response.RunID || run.RunID != response.RunID || request.AdmissionRoot != root || request.BaseSnapshot != strings.TrimSpace(string(base)) || run.BaseSnapshot != request.BaseSnapshot || !reflect.DeepEqual(request.IntakeConfirmationReceipt, expectedReceipt) || !reflect.DeepEqual(run.IntakeConfirmationReceipt, &expectedReceipt) {
		t.Fatal("started run changed the original root, identity, baseline, intake, configuration or confirmation source", request, run)
	}
	if run.Configuration.Name != configuration.Name || !reflect.DeepEqual(run.Configuration.Configuration, configuration.Configuration) || !reflect.DeepEqual(request.FlowSelection, configuration.Configuration.FlowSelection) || !reflect.DeepEqual(request.FlowSelection, run.FlowSelection) {
		t.Fatal("started run changed the selected saved configuration", configuration, run.Configuration)
	}
	paths, err := facade.OriginalPayloadPaths(filepath.Join(root, facade.EngineNamespace, response.RunID, "payloads", "intake"))
	if err != nil || len(paths) != 1 {
		t.Fatal("started run omitted its complete original intake", paths, err)
	}
	var retained facade.IntakePreview
	intakeBody, err := os.ReadFile(paths[0])
	if err != nil || json.Unmarshal(intakeBody, &retained) != nil {
		t.Fatal("cannot read the retained original intake", err)
	}
	preview.IntakeCandidate = expectedReceipt
	if !reflect.DeepEqual(retained, preview) {
		t.Fatal("started run changed its complete original material or revision", retained)
	}
	return run, nil
}

func confirmFacadeFixtureControl(t *testing.T, f *facade.Facade, candidate facade.ControlCandidate) (protocol.Acceptance, facade.Run, error) {
	t.Helper()
	serverInput, hostOutput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverInput.Close()
	defer hostOutput.Close()
	hostInput, serverOutput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer hostInput.Close()
	defer serverOutput.Close()
	var acceptance protocol.Acceptance
	var run facade.Run
	var submitErr error
	done := make(chan int, 1)
	go func() {
		var errors bytes.Buffer
		code := ServeTools(serverInput, serverOutput, &errors, nil, func(name string, raw json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
			var selected, original any
			expected, _ := json.Marshal(candidate)
			_ = json.Unmarshal(expected, &original)
			if name != "facade_control" || json.Unmarshal(raw, &selected) != nil || !reflect.DeepEqual(selected, original) {
				return nil, fmt.Errorf("facade fixture changed the original control request")
			}
			acceptance, run, submitErr = f.SubmitControl(candidate, func(value any) error {
				confirmed, err := json.Marshal(value)
				if err != nil || !bytes.Equal(confirmed, expected) {
					return fmt.Errorf("facade confirmation changed the complete original candidate")
				}
				return confirm(value)
			})
			return map[string]any{"acceptance": acceptance, "run": run}, submitErr
		}, nil)
		_ = serverOutput.Close()
		_ = serverInput.Close()
		done <- code
	}()
	_, exchangeErr := exchangeConfirmationFixture(t, hostInput, hostOutput, "facade_control", candidate, func(displayed map[string]any) {
		checkFixtureControlDisplay(t, displayed, candidate, candidate.Run)
	})
	_ = hostOutput.Close()
	if code := <-done; code != 0 {
		return acceptance, run, fmt.Errorf("facade confirmation service exit %d", code)
	}
	if submitErr != nil {
		return acceptance, run, submitErr
	}
	if exchangeErr == nil {
		if acceptance.EventID != string(candidate.Event.ID) || acceptance.Status != "ACCEPTED" {
			t.Fatal("facade confirmation accepted a different event", acceptance)
		}
		checkFixtureControlReplay(t, f, candidate)
	}
	return acceptance, run, exchangeErr
}
