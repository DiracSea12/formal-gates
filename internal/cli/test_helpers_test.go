package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/harness"
	"formal-gates/internal/lifecycle"
)

// Startup fixtures carry a conversational user decision. Other user-control
// fixtures verify the displayed operation through the actual MCP transport.
func runWithHostConfirmation(t *testing.T, args []string, streams IO) int {
	t.Helper()
	if len(args) >= 2 && args[0] == "workflow" && args[1] == "start" {
		return Run("formal-gates", args, streams)
	}
	code := -1
	withHostConfirmation(t, args, func(confirm func(any) error) {
		streams.confirmation = confirm
		code = Run("formal-gates", args, streams)
	})
	return code
}

func submitCLIControlWithHost(t *testing.T, f *facade.Facade, candidate facade.ControlCandidate) (protocol.Acceptance, facade.Run, error) {
	t.Helper()
	if candidate.Event.Decide == nil {
		return f.SubmitControl(candidate)
	}
	var accepted protocol.Acceptance
	var run facade.Run
	var err error
	withHostConfirmation(t, candidate, func(confirm func(any) error) {
		accepted, run, err = f.SubmitControl(candidate, confirm)
	})
	return accepted, run, err
}

func withHostConfirmation(t *testing.T, arguments any, execute func(func(any) error)) {
	t.Helper()
	expectedArguments, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
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
	var transportErrors bytes.Buffer
	var displayed []byte
	done := make(chan int, 1)
	go func() {
		served := harness.ServeTools(in, out, &transportErrors, nil, func(name string, raw json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
			if name != "invoke" || !bytes.Equal(raw, expectedArguments) {
				return nil, fmt.Errorf("confirmation fixture received a different command: %s %s", name, raw)
			}
			execute(func(value any) error {
				var err error
				displayed, err = json.Marshal(value)
				if err != nil {
					return err
				}
				if err := confirm(value); err != nil {
					return err
				}
				after, err := json.Marshal(value)
				if err != nil {
					return err
				}
				if !bytes.Equal(displayed, after) {
					return fmt.Errorf("confirmation changed the complete original candidate")
				}
				return nil
			})
			return nil, nil
		}, nil)
		out.Close()
		done <- served
	}()
	encoder, decoder := json.NewEncoder(client), json.NewDecoder(host)
	if err := encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"elicitation": map[string]any{"form": map[string]any{}}}}}); err != nil {
		t.Fatal(err)
	}
	var initialized any
	if err := decoder.Decode(&initialized); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(map[string]any{"id": 2, "method": "tools/call", "params": map[string]any{"name": "invoke", "arguments": arguments}}); err != nil {
		t.Fatal(err)
	}
	for {
		var frame map[string]any
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if frame["method"] == nil {
			if frame["id"] != float64(2) {
				t.Fatal("fixture response belongs to another command", frame)
			}
			break
		}
		params := frame["params"].(map[string]any)
		schema := params["requestedSchema"].(map[string]any)
		message := params["message"].(string)
		if frame["method"] != "elicitation/create" || frame["id"] == nil || params["mode"] != "form" || utf8.RuneCountInString(message) > 800 || strings.Count(message, "\n")+1 > 12 || len(schema["properties"].(map[string]any)) != 0 || schema["required"] != nil {
			t.Fatal("fixture did not receive its actual original operation", frame)
		}
		var visible map[string]any
		if !json.Valid(displayed) || !strings.HasPrefix(message, "Confirm this exact operation:\n") || json.Unmarshal([]byte(strings.TrimPrefix(message, "Confirm this exact operation:\n")), &visible) != nil {
			t.Fatal("confirmation did not describe its original operation")
		}
		if err := encoder.Encode(map[string]any{"id": frame["id"], "result": map[string]any{"action": "accept", "content": map[string]any{}}}); err != nil {
			t.Fatal(err)
		}
	}
	client.Close()
	if served := <-done; served != 0 {
		t.Fatal(served, transportErrors.String())
	}
}

func clearHostEnv(t *testing.T) {
	t.Helper()
	for _, key := range lifecycle.ProviderEnvironmentKeys() {
		t.Setenv(key, "")
	}
}

func isHostEnvKey(key string) bool {
	for _, candidate := range lifecycle.ProviderEnvironmentKeys() {
		if key == candidate {
			return true
		}
	}
	return false
}

// hostFilteredEnv strips host lifecycle keys from the inherited environment so
// a subprocess under test observes the lenient default provider.
func hostFilteredEnv(environment []string) []string {
	filtered := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if key := strings.SplitN(kv, "=", 2)[0]; !isHostEnvKey(key) {
			filtered = append(filtered, kv)
		}
	}
	return append(filtered, environment...)
}

func mustWriteCLI(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Install CLI fixtures supply the documented transaction launcher's invocation
// identity. The production launcher checker and registry owner remain active.
func runInstalledFixture(t *testing.T, args []string, streams IO) int {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, ".local", "bin", installTestBinaryName())
	for i, arg := range args {
		if arg == "--binary-target" && i+1 < len(args) {
			launcher = args[i+1]
		}
	}
	original := os.Args[0]
	os.Args[0] = launcher
	defer func() { os.Args[0] = original }()
	return Run("formal-gates", args, streams)
}
