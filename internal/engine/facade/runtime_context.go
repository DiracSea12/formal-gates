package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
	"os"
	"os/exec"
	"strings"
)

// The existing execution owns the binding and admission. Its platform adapter
// supplies native configuration, process transport and transcript observation;
// it cannot allocate attempts, change authorization, or grant descendants tools.
// Installation support alone does not imply a regular execution transport.
type nativeRuntimeAdapter interface {
	capture(settings.Values) (*protocol.HostRuntimeContext, error)
	validate(*protocol.HostRuntimeContext) error
	transcript(string, *protocol.HostRuntimeContext) (string, error)
	toolConfig(json.RawMessage) (map[string]any, error)
	execute(*Facade, string, bool) (Run, error)
	runModel(context.Context, Run, protocol.TaskOperation, string, protocol.TaskOperationReceipt) protocol.TaskOperationReceipt
	observeModel(protocol.TaskOperation, string, protocol.TaskOperationReceipt) (protocol.TaskOperationReceipt, error)
}

func runtimeAdapter(provider string) (nativeRuntimeAdapter, error) {
	switch provider {
	case "codex":
		return codexRuntimeAdapter{}, nil
	case "workbuddy":
		return workBuddyRuntimeAdapter{}, nil
	case "zcode":
		return zcodeRuntimeAdapter{}, nil
	case "deepseek-harness":
		return dshRuntimeAdapter{}, nil
	default:
		return nil, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: no regular runtime adapter for %s", provider)
	}
}

// ExecuteNative dispatches creation and recovery through the same platform
// boundary. Unsupported hosts are rejected before any claim or native call.
func (f *Facade) ExecuteNative(actionID string, resume bool) (Run, error) {
	run, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	x, ok := run.Executions[actionID]
	if !ok {
		return Run{}, fmt.Errorf("UNKNOWN_ACTION: %s", actionID)
	}
	adapter, err := runtimeAdapter(x.Claim.Provider)
	if err != nil {
		return Run{}, err
	}
	return adapter.execute(f, actionID, resume)
}

func captureRuntimeContext(provider string, config settings.Values, stateDirectory ...string) (*protocol.HostRuntimeContext, error) {
	adapter, err := runtimeAdapter(provider)
	if err != nil {
		return nil, err
	}
	binding, err := adapter.capture(config)
	if err != nil {
		return nil, err
	}
	if len(stateDirectory) > 0 {
		if managed, ok := adapter.(interface {
			bindResources(*protocol.HostRuntimeContext, string)
		}); ok {
			managed.bindResources(binding, stateDirectory[0])
			if len(stateDirectory) > 1 {
				binding.Environment["FORMAL_GATES_ENGINE_ROOT"] = stateDirectory[1]
			}
		}
	}
	return binding, nil
}

func runtimeToolConfig(provider string, raw json.RawMessage) (map[string]any, error) {
	adapter, err := runtimeAdapter(provider)
	if err != nil {
		return nil, err
	}
	return adapter.toolConfig(raw)
}

func runtimeCommandContext(cmd *exec.Cmd, binding *protocol.HostRuntimeContext) error {
	if binding == nil {
		return fmt.Errorf("RUNTIME_CONTEXT_MISSING: native execution needs its original adapter binding")
	}
	adapter, err := runtimeAdapter(binding.Provider)
	if err != nil {
		return err
	}
	if err = adapter.validate(binding); err != nil {
		return err
	}
	cmd.Path = binding.Executable
	cmd.Args[0] = binding.Executable
	cmd.Env = os.Environ()
	for key, value := range binding.Environment {
		cmd.Env = replaceEnvironment(cmd.Env, key, value)
	}

	return nil
}

func findBoundTranscript(identity string, binding *protocol.HostRuntimeContext) (string, error) {
	if binding == nil {
		return "", fmt.Errorf("RUNTIME_CONTEXT_MISSING: transcript lookup needs the original runtime binding")
	}
	adapter, err := runtimeAdapter(binding.Provider)
	if err != nil {
		return "", err
	}
	return adapter.transcript(identity, binding)
}

func replaceEnvironment(environment []string, key, value string) []string {
	kept := environment[:0]
	for _, pair := range environment {
		if !strings.HasPrefix(pair, key+"=") {
			kept = append(kept, pair)
		}
	}
	return append(kept, key+"="+value)
}
