//go:build !windows

package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
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
			params := objectOrEmpty(frame["params"])
			schema := objectOrEmpty(params["requestedSchema"])
			if params["mode"] != "form" || !valuesEqual(schema["required"], []any{"confirmOperation"}) || getOf(getOf(schema, "properties"), "confirmOperation") == nil {
				return nil, fmt.Errorf("service omitted its explicit original form: %v", frame)
			}
			var original map[string]any
			message := strings.TrimPrefix(textOf(params["message"]), "Confirm this exact operation:\n")
			if err := json.Unmarshal([]byte(message), &original); err != nil {
				return nil, fmt.Errorf("service did not display the original object: %w", err)
			}
			if check != nil {
				check(original)
			}
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": frame["id"], "result": map[string]any{"action": "accept", "content": map[string]any{"confirmOperation": true}}}); err != nil {
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
	return position
}

func confirmInstalledFixtureControl(t *testing.T, command *exec.Cmd, root, owner string, candidate facade.ControlCandidate) ([]byte, error) {
	t.Helper()
	position := fixtureControlPosition(t, root, owner, candidate)
	return installedConfirmationFixture(t, command, "confirm_control", map[string]any{"root": root, "candidate": position}, func(original map[string]any) {
		input, _ := json.Marshal(candidate.Input)
		var expectedInput any
		_ = json.Unmarshal(input, &expectedInput)
		if original["root"] != candidate.Root || getOf(original["run"], "runId") != candidate.Run.RunID || getOf(original["run"], "revision") != float64(candidate.Run.Revision) || !reflect.DeepEqual(original["input"], expectedInput) {
			t.Fatal("confirmation changed the original control target, revision or choice", original)
		}
		if plan := candidate.DevelopmentPlan; plan != nil {
			proposal, _ := json.Marshal(plan.Proposal)
			var expected any
			_ = json.Unmarshal(proposal, &expected)
			if original["planDigest"] != plan.Confirmation.Digest || !reflect.DeepEqual(original["proposal"], expected) {
				t.Fatal("confirmation omitted the complete original development plan", original)
			}
		}
	})
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
	displayedID := ""
	body, err := installedConfirmationFixture(t, command, "confirm_start", map[string]any{"root": root, "intake": position, "route": "regular"}, func(original map[string]any) {
		materials := objectOrEmpty(original["materials"])
		if original["root"] != root || original["route"] != "regular" || materials["content"] != document || materials["revision"] != preview.Revision {
			t.Fatal("start form omitted the exact complete source and operation", original)
		}
		displayedID = textOf(original["runId"])
	})
	if err != nil {
		return facade.Run{}, err
	}
	var response struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.RunID == "" || response.RunID != displayedID {
		return facade.Run{}, fmt.Errorf("start response changed its displayed run identity: %s", body)
	}
	f, err := facade.Open(root, response.RunID)
	if err != nil {
		return facade.Run{}, err
	}
	return f.Show()
}

func confirmFacadeFixtureControl(t *testing.T, f *facade.Facade, candidate facade.ControlCandidate) (protocol.Acceptance, facade.Run, error) {
	t.Helper()
	serverInput, hostOutput := io.Pipe()
	hostInput, serverOutput := io.Pipe()
	defer hostInput.Close()
	defer hostOutput.Close()
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
			acceptance, run, submitErr = f.SubmitControl(candidate, confirm)
			return map[string]any{"acceptance": acceptance, "run": run}, submitErr
		}, nil)
		_ = serverOutput.Close()
		_ = serverInput.Close()
		done <- code
	}()
	_, exchangeErr := exchangeConfirmationFixture(t, hostInput, hostOutput, "facade_control", candidate, func(displayed map[string]any) {
		expected, _ := json.Marshal(candidate)
		var original any
		_ = json.Unmarshal(expected, &original)
		if !reflect.DeepEqual(displayed, original) {
			t.Fatal("facade confirmation changed the displayed original candidate")
		}
	})
	_ = hostOutput.Close()
	if code := <-done; code != 0 {
		return acceptance, run, fmt.Errorf("facade confirmation service exit %d", code)
	}
	if submitErr != nil {
		return acceptance, run, submitErr
	}
	return acceptance, run, exchangeErr
}
