package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"formal-gates/internal/coordination"
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

type nativeSourceAppend struct {
	Path        string `json:"path"`
	Offset      int64  `json:"offset"`
	InputDigest string `json:"inputDigest"`
}

// The invocation journal records the append position before copying bytes.
// Recovery verifies the observed prefix and copies only what is missing, so a
// stopped collector cannot duplicate a turn or leave partial JSONL. Distinct
// invocations retain distinct positions even when their response bytes match.
func appendStreamTranscript(job, identity, stream string, binding *protocol.HostRuntimeContext) (string, error) {
	path, err := streamTranscript(identity, binding)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return path, err
	}
	unlock, err := coordination.AcquirePath(path+".lock", "native source append")
	if err != nil {
		return path, err
	}
	defer unlock()
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
	input, err := os.ReadFile(filepath.Join(job, stream))
	if err != nil {
		return path, err
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return path, err
	}
	defer output.Close()
	info, err := output.Stat()
	if err != nil {
		return path, err
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(input))
	intent := nativeSourceAppend{Path: path, Offset: info.Size(), InputDigest: digest}
	if previous, ok := process["sourceAppend"]; ok {
		raw, err := json.Marshal(previous)
		if err != nil {
			return path, err
		}
		if err := json.Unmarshal(raw, &intent); err != nil {
			return path, err
		}
		if intent.Path != path || intent.InputDigest != digest {
			return path, fmt.Errorf("SOURCE_APPEND_UNKNOWN: native source or destination changed after the recorded append intent")
		}
	} else {
		process["sourceAppend"] = intent
		if err := writeJSONAtomic(filepath.Join(job, "process.json"), process); err != nil {
			return path, err
		}
	}
	if intent.Offset < 0 || info.Size() < intent.Offset {
		return path, fmt.Errorf("SOURCE_APPEND_UNKNOWN: managed source is shorter than its recorded append position")
	}
	observedSize := min(info.Size()-intent.Offset, int64(len(input)))
	observed := make([]byte, observedSize)
	if observedSize > 0 {
		if _, err := output.ReadAt(observed, intent.Offset); err != nil {
			return path, err
		}
	}
	if !bytes.Equal(observed, input[:observedSize]) {
		return path, fmt.Errorf("SOURCE_APPEND_UNKNOWN: observed source bytes conflict with the native invocation journal")
	}
	if observedSize < int64(len(input)) {
		remainder := input[observedSize:]
		if n, err := output.WriteAt(remainder, intent.Offset+observedSize); err != nil {
			return path, err
		} else if n != len(remainder) {
			return path, io.ErrShortWrite
		}
	}
	if err := output.Sync(); err != nil {
		return path, err
	}
	if err := output.Close(); err != nil {
		return path, err
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
