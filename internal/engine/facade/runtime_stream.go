package facade

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"formal-gates/internal/engine/protocol"
)

const nativeSourceRoot = "FORMAL_GATES_NATIVE_SOURCES"

// streamRuntimeAdapter supplies the identical transport support shared by stream
// hosts. Native protocol, configuration and result semantics stay with the host.
type streamRuntimeAdapter struct{}

func (streamRuntimeAdapter) bindResources(binding *protocol.HostRuntimeContext, root string) {
	bindStreamResources(binding, root)
}
func (streamRuntimeAdapter) transcript(identity string, binding *protocol.HostRuntimeContext) (string, error) {
	return streamTranscript(identity, binding)
}
func (streamRuntimeAdapter) toolConfig(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func captureStreamContext(provider, executable string, extra map[string]string) (*protocol.HostRuntimeContext, error) {
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return nil, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	environment := map[string]string{}
	for _, key := range []string{"HOME", "PATH", "XDG_CONFIG_HOME"} {
		environment[key] = os.Getenv(key)
	}
	for key, value := range extra {
		environment[key] = value
	}
	return &protocol.HostRuntimeContext{Provider: provider, Executable: resolved, Environment: environment}, nil
}
func validateStreamContext(binding *protocol.HostRuntimeContext, provider string) error {
	if binding == nil || binding.Provider != provider || !filepath.IsAbs(binding.Executable) {
		return fmt.Errorf("RUNTIME_CONTEXT_MISSING: %s original executable binding required", provider)
	}
	stat, err := os.Stat(binding.Executable)
	if err != nil {
		return err
	}
	if stat.IsDir() || stat.Mode()&0111 == 0 {
		return fmt.Errorf("RUNTIME_CONTEXT_MISSING: executable unavailable: %s", binding.Executable)
	}
	return nil
}
func bindStreamResources(binding *protocol.HostRuntimeContext, stateDirectory string) {
	binding.Environment[nativeSourceRoot] = filepath.Join(stateDirectory, "executions", "native-sources", binding.Provider)
}
func streamTranscript(identity string, binding *protocol.HostRuntimeContext) (string, error) {
	if binding == nil || identity == "" || !filepath.IsAbs(binding.Environment[nativeSourceRoot]) {
		return "", fmt.Errorf("SOURCE_UNAVAILABLE: original managed native source binding missing")
	}
	return filepath.Join(binding.Environment[nativeSourceRoot], base64.RawURLEncoding.EncodeToString([]byte(identity))+".jsonl"), nil
}

// Append each invocation to the same identity's native evidence, so existing
// baseline/delta accounting observes cumulative usage across continuation.
func appendStreamTranscript(job, identity, stream string, binding *protocol.HostRuntimeContext) (string, error) {
	path, err := streamTranscript(identity, binding)
	if err != nil {
		return "", err
	}
	var process map[string]any
	raw, err := os.ReadFile(filepath.Join(job, "process.json"))
	if err != nil {
		return path, err
	}
	if err := json.Unmarshal(raw, &process); err != nil {
		return path, err
	}
	if process["transcriptPath"] == path {
		return path, nil
	}
	input, err := os.Open(filepath.Join(job, stream))
	if err != nil {
		return path, err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return path, err
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return path, err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	for _, err := range []error{copyErr, syncErr, closeErr} {
		if err != nil {
			return path, err
		}
	}
	process["transcriptPath"] = path
	return path, writeJSONAtomic(filepath.Join(job, "process.json"), process)
}

func (f *Facade) attachStreamIdentity(claim protocol.ExecutionClaim, identity string) error {
	return f.submitSource(protocol.ExecutionSource{ActionID: claim.ActionID, AttemptID: claim.AttemptID, Provider: claim.Provider, Identity: identity, Status: "RUNNING"})
}

func (streamRuntimeAdapter) source(identity, job string, binding *protocol.HostRuntimeContext) (string, error) {
	return appendStreamTranscript(job, identity, "stdout.jsonl", binding)
}
