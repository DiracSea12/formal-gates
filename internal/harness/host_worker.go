package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
)

func hostWorkerInvocation(directory string) (host.CodexInvocation, host.CodexTurn, error) {
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return host.CodexInvocation{}, host.CodexTurn{}, err
	}
	if getOf(request, "provider") != "codex" {
		return host.CodexInvocation{}, host.CodexTurn{}, fmt.Errorf("host worker original provider is not Codex")
	}
	var ref host.CodexInvocation
	body, err := json.Marshal(getOf(request, "host"))
	if err != nil {
		return ref, host.CodexTurn{}, err
	}
	if err := json.Unmarshal(body, &ref); err != nil {
		return ref, host.CodexTurn{}, err
	}
	_, turn, err := ref.Observe()
	return ref, turn, err
}

func HostWorkerToolSchemas(directory string) ([]any, error) {
	tools, err := newNativeWorkspaceTools(directory)
	if err != nil {
		return nil, err
	}
	return tools.schemas(), nil
}

// InvokeHostWorkerTool shares the original managed workspace and semantic
// tools. The receipt describes the actual host worker operation, not a native
// app-server tool call.
func InvokeHostWorkerTool(directory, operation, session string, arguments json.RawMessage, services ...ExecutionServices) (any, error) {
	ref, turn, err := hostWorkerInvocation(directory)
	if err != nil {
		return nil, err
	}
	if ref.Session != session || turn.State != "running" {
		return nil, fmt.Errorf("host worker tool requires its original running native child")
	}
	tools, err := newNativeWorkspaceTools(directory, services...)
	if err != nil {
		return nil, err
	}
	name := nativeWorkspaceTool
	if operation == "submit" {
		name = nativeSemanticTool
	} else if operation != "workspace" {
		return nil, fmt.Errorf("unknown host worker operation %s", operation)
	}
	value, callErr := tools.invoke(name, arguments)
	request, err := json.Marshal(map[string]any{"method": "workflow/worker", "params": map[string]any{"operation": operation, "session": session, "arguments": arguments}})
	if err != nil {
		return nil, err
	}
	result := map[string]any{"isError": callErr != nil, "value": value}
	if callErr != nil {
		result["error"] = callErr.Error()
	}
	if err := tools.recordCall(request, result); err != nil {
		return nil, err
	}
	return value, callErr
}

// HostWorkerResultSource captures completed original child facts and managed
// evidence. It never creates process, exit or app-server acknowledgements.
func HostWorkerResultSource(directory string) (map[string]any, error) {
	ref, turn, err := hostWorkerInvocation(directory)
	if err != nil {
		return nil, err
	}
	if turn.State != "completed" {
		return nil, fmt.Errorf("host worker original turn is %s", turn.State)
	}
	if value, err := readJSONFile(filepath.Join(directory, "source.json")); err == nil {
		source := objectOrEmpty(value)
		_, err := HostWorkerResultEvidence("codex", directory, source)
		return source, err
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	answer, err := submittedSemantics(directory)
	if err != nil {
		return nil, err
	}
	facts, err := nativePathEvidence(directory, string(answer), true)
	if err != nil {
		return nil, err
	}
	files := map[string]any{}
	names := []string{"native-request.json", nativeToolCallsFile, nativeToolContextFile}
	toolFiles, err := nativeToolEvidenceFiles(directory)
	if err != nil {
		return nil, err
	}
	names = append(names, toolFiles...)
	if facts != nil {
		names = append(names, nativePathsFile)
	}
	for _, name := range names {
		proof, err := fileProofOf(filepath.Join(directory, name))
		if err != nil {
			return nil, err
		}
		files[name] = proof
	}
	source := map[string]any{"source": "host", "provider": "codex", "identity": ref.Session, "host": ref, "turn": turn, "transcript": ref.Transcript, "files": files}
	if facts != nil {
		source["effects"] = facts
	}
	body, err := canonicalBody(source)
	if err != nil {
		return nil, err
	}
	plain, err := decodeNativeJSON(body)
	if err != nil {
		return nil, err
	}
	source = objectOrEmpty(plain)
	if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return nil, err
	}
	return source, nil
}

func HostWorkerResultEvidence(provider, directory string, source map[string]any) (worker.ResultSource, error) {
	ref, turn, err := hostWorkerInvocation(directory)
	if err != nil {
		return worker.ResultSource{}, err
	}
	if provider != "codex" || source["provider"] != provider || source["source"] != "host" || source["identity"] != ref.Session || turn.State != "completed" || !hostEvidenceEqual(source["host"], ref) || !hostEvidenceEqual(source["turn"], turn) {
		return worker.ResultSource{}, fmt.Errorf("host result differs from original native child facts")
	}
	for name, expected := range objectOrEmpty(source["files"]) {
		proof, err := fileProofOf(filepath.Join(directory, name))
		if err != nil || !valuesEqual(proof, expected) {
			return worker.ResultSource{}, fmt.Errorf("host worker original %s evidence differs", name)
		}
	}
	for _, name := range []string{"native-request.json", nativeToolCallsFile, nativeToolContextFile} {
		if getOf(source["files"], name) == nil {
			return worker.ResultSource{}, fmt.Errorf("host worker original %s proof is missing", name)
		}
	}
	answer, err := submittedSemantics(directory)
	if err != nil {
		return worker.ResultSource{}, err
	}
	paths, err := nativePathEvidence(directory, string(answer), false)
	if err != nil {
		return worker.ResultSource{}, err
	}
	if paths != nil && !hostEvidenceEqual(source["effects"], paths) {
		return worker.ResultSource{}, fmt.Errorf("host worker original terminal paths differ")
	}
	if paths != nil && getOf(source["files"], nativePathsFile) == nil {
		return worker.ResultSource{}, fmt.Errorf("host worker original terminal paths proof is missing")
	}
	facts, err := nativeToolFacts(directory)
	if err != nil {
		return worker.ResultSource{}, err
	}
	digest, err := CanonicalDigest(source)
	return worker.ResultSource{Answer: answer, Digest: "sha256:" + digest, Identity: ref.Session, LocationFacts: workerLocationFacts(facts)}, err
}

func hostEvidenceEqual(a, b any) bool {
	left, err := canonicalBody(a)
	if err != nil {
		return false
	}
	right, err := canonicalBody(b)
	return err == nil && bytes.Equal(left, right)
}

func hostWorkerFailureEvidence(provider, directory string) ([]byte, string, string, error) {
	ref, turn, err := hostWorkerInvocation(directory)
	if err != nil {
		return nil, "", "", err
	}
	if provider != "codex" || turn.State != "completed" {
		return nil, "", "", fmt.Errorf("host failure requires its original completed child")
	}
	if _, err := os.Stat(filepath.Join(directory, nativeToolContextFile)); err == nil {
		if _, err := os.Stat(filepath.Join(directory, nativeToolCallsFile)); err != nil {
			return nil, "", "", err
		}
	} else if !os.IsNotExist(err) {
		return nil, "", "", err
	}
	if _, err := submittedSemantics(directory); !errors.Is(err, ErrSemanticSubmissionMissing) {
		if err == nil {
			err = fmt.Errorf("host invocation has a submitted result")
		}
		return nil, "", "", err
	}
	proof, err := fileProofOf(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, "", "", err
	}
	failure := &nativeInvocationError{err: ErrSemanticSubmissionMissing}
	evidence := map[string]any{"host": ref, "turn": turn, "request": proof, "invoked": true, "failure_class": failure.failureClass(), "error": failure.Error()}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return nil, "", "", err
	}
	plain, err := decodeNativeJSON(encoded)
	if err != nil {
		return nil, "", "", err
	}
	body, err := canonicalBody(plain)
	if err != nil {
		return nil, "", "", err
	}
	digest, err := CanonicalDigest(plain)
	return body, "sha256:" + digest, ref.Session, err
}
