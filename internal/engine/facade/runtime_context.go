package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
	"os"
	"os/exec"
	"strings"
)

// The existing execution owns the binding and admission. Its platform adapter
// supplies native configuration, process transport and transcript observation;
// it cannot allocate attempts, change authorization, or grant descendants tools.
// Installation support alone does not imply a regular execution transport.
// nativeTurn is a single native invocation, not a workflow or an executor.
// The driver supplies immutable inputs and two bounded callbacks. Adapters never
// decide the next task, write a verdict, or connect the whitebox preflight.
type nativeTurn struct {
	Claim                                                protocol.ExecutionClaim
	Settings                                             settings.Values
	Root, RunID, Job, Workspace, Prompt, ToolDescription string
	Schema                                               map[string]any
	Resume, NoTools                                      bool
	Attached                                             func(string) error
	Operation                                            func(string, string, json.RawMessage) (any, error)
}

type nativeProbe struct {
	Context                                               *protocol.HostRuntimeContext
	Root, RunID, Job, Workspace, ActionID, Step, Identity string
}

type nativeRuntimeAdapter interface {
	nativeExecutionTransport
	capture(settings.Values) (*protocol.HostRuntimeContext, error)
	validate(*protocol.HostRuntimeContext) error
	transcript(string, *protocol.HostRuntimeContext) (string, error)
	toolConfig(json.RawMessage) (map[string]any, error)
	runModel(context.Context, settings.Values, protocol.TaskOperation, string, protocol.TaskOperationReceipt) protocol.TaskOperationReceipt
	observeModel(protocol.TaskOperation, string, protocol.TaskOperationReceipt) (protocol.TaskOperationReceipt, error)
}

type nativePlatform struct {
	*host.Descriptor
	nativeRuntimeAdapter
}

func registerNativePlatform(descriptor *host.Descriptor, adapter nativeRuntimeAdapter) error {
	if descriptor == nil || adapter == nil || descriptor.CheckIdentity == nil || descriptor.ReadUsage == nil || descriptor.ObserveRecovery == nil {
		return fmt.Errorf("native platform requires runtime, identity, recovery and usage capabilities")
	}
	return host.Register(&nativePlatform{descriptor, adapter})
}

func registerBuiltinRuntime(provider string, adapter nativeRuntimeAdapter) {
	base, err := host.LookupAdapter(provider)
	if err == nil {
		err = registerNativePlatform(base.HostDescriptor(), adapter)
	}
	if err != nil {
		panic(err)
	}
}

// Execution claims use canonical provider IDs. Do not admit an installation alias
// that would later disagree with the platform's persisted native identity binding.
func runtimeAdapter(provider string) (nativeRuntimeAdapter, error) {
	entry, err := host.LookupAdapter(provider)
	if err == nil {
		if platform, ok := entry.(*nativePlatform); ok && platform.ID == provider {
			// Preserve optional capabilities on the concrete adapter (for example
			// managed resource binding), not just the mandatory interface methods.
			return platform.nativeRuntimeAdapter, nil
		}
	}
	return nil, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: no regular runtime adapter for %s", provider)
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
	return f.executeNativeTransport(actionID, resume, adapter)
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
