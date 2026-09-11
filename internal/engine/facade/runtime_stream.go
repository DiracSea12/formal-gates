package facade

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
)

const nativeSourceRoot = "FORMAL_GATES_NATIVE_SOURCES"

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
func (f *Facade) finishStreamNative(actionID string, claim protocol.ExecutionClaim, identity, job, stream string, waitErr error) (Run, error) {
	path, err := appendStreamTranscript(job, identity, stream, claim.Context)
	source := protocol.ExecutionSource{ActionID: actionID, AttemptID: claim.AttemptID, Provider: claim.Provider, Identity: identity, Status: "STOPPED", TranscriptPath: path}
	if err == nil {
		_, err = cost.ParseTranscript(claim.Provider, path)
	}
	if err != nil {
		source.Status = "UNAVAILABLE"
		source.Reason = err.Error()
	}
	return f.finishNativeResult(actionID, claim, identity, filepath.Join(job, "answer.json"), waitErr, source)
}
func (f *Facade) attachStreamIdentity(claim protocol.ExecutionClaim, identity string) error {
	return f.submitSource(protocol.ExecutionSource{ActionID: claim.ActionID, AttemptID: claim.AttemptID, Provider: claim.Provider, Identity: identity, Status: "RUNNING"})
}

func finishStreamCoverage(job, identity string, binding *protocol.HostRuntimeContext, stream string) error {
	if _, err := appendStreamTranscript(job, identity, stream, binding); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(job, "answer.json"), filepath.Join(job, "coverage-answer.json")); err != nil {
		return err
	}
	process, err := os.ReadFile(filepath.Join(job, "process.json"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(job, "coverage-process.json"), process, 0600); err != nil {
		return err
	}
	for _, name := range []string{"stderr.log", "session-request.json", "native-source.json", "native-events.jsonl"} {
		if err := os.Rename(filepath.Join(job, name), filepath.Join(job, "coverage-"+name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
