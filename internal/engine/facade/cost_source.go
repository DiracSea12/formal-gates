package facade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

// NativeSourceVerifier is installed once by the native command assembly. It
// retains the adapters' original source verification and has no workflow state.
var NativeSourceVerifier func(string, string, map[string]any) ([]byte, string, string, error)

// CostSourceBridge resolves original dispatch evidence. Managed native jobs
// share their provider verifier and raw streams; other dispatches use lifecycle.
func CostSourceBridge(root, runID string, verifier ...func(string, string, map[string]any) ([]byte, string, string, error)) protocol.CostSourceResolver {
	verify := NativeSourceVerifier
	if len(verifier) > 0 {
		verify = verifier[0]
	}
	return func(ref protocol.CostSourceRef) protocol.ResolvedCostSource {
		if ref.RunID != runID {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: "cost source differs from its owning run"}
		}
		provider, transcriptPath, err := lifecycle.DispatchTranscriptPath(root, ref.RunID, ref.ActionID)
		if err != nil {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: err.Error()}
		}
		if strings.TrimSpace(provider) == "" {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: "dispatch has no lifecycle binding"}
		}
		if provider != ref.Provider {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: "cost source provider does not match dispatch provider"}
		}
		verification, err := lifecycle.VerifyDispatch(root, ref.RunID, ref.ActionID)
		if err != nil {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: err.Error()}
		}
		if !verification.StopObserved {
			if verification.Outcome == lifecycle.Unavailable {
				return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: "host lifecycle evidence is unavailable"}
			}
			return protocol.ResolvedCostSource{State: protocol.CostSourcePending}
		}
		paths, managed, sourceErr := nativeCostSourcePaths(root, ref, verification.Identity, verify)
		if managed {
			usage, parseErr := cost.ParseTranscriptFiles(provider, paths...)
			if err := errors.Join(sourceErr, parseErr); err != nil {
				return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Usage: usage, Reason: err.Error()}
			}
			return protocol.ResolvedCostSource{State: protocol.CostSourceReady, Usage: usage}
		}
		if strings.TrimSpace(transcriptPath) == "" {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: "dispatch stop has no transcript path"}
		}
		usage, parseErr := cost.ParseTranscript(provider, transcriptPath)
		if parseErr != nil {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: parseErr.Error()}
		}
		return protocol.ResolvedCostSource{State: protocol.CostSourceReady, Usage: usage}
	}
}

// dispatchClaimBridge 在派发签出落账时，以宿主回执的实际派发身份建立
// lifecycle sidecar 绑定；identity 不可变，宿主 start/stop 事件按它配对。
func dispatchClaimBridge(root, owningRunID string, admission Admission) protocol.DispatchClaim {
	return func(runID, actionID, identity, provider string) error {
		if runID != owningRunID || provider != admission.Provider {
			return fmt.Errorf("UNREGISTERED_INSTALL: dispatch claim does not match its owning run/provider")
		}
		return admission.write(func() error { return lifecycle.BindDispatchWithProvider(root, runID, actionID, identity, provider) })
	}
}

func nativeCostSourcePaths(root string, ref protocol.CostSourceRef, identity string, verify func(string, string, map[string]any) ([]byte, string, string, error)) ([]string, bool, error) {
	stateDir := filepath.Join(root, EngineNamespace, ref.RunID)
	actionDir := workerActionDirectory(stateDir, ref.ActionID)
	entries, err := os.ReadDir(actionDir)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	if verify == nil {
		return nil, true, fmt.Errorf("native cost source verification is unavailable")
	}
	descriptor, err := host.Lookup(ref.Provider)
	if err != nil {
		return nil, true, err
	}
	paths := []string{}
	attempt := ""
	var missing error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(actionDir, entry.Name())
		requestBytes, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
		if err != nil {
			missing = errors.Join(missing, err)
			continue
		}
		var request struct {
			Provider string `json:"provider"`
			Engine   struct {
				Root           string         `json:"root"`
				RunID          string         `json:"runId"`
				WorkerMaterial WorkerMaterial `json:"workerMaterial"`
				ResumeActionID string         `json:"resumeActionId"`
			} `json:"engine"`
		}
		if err := json.Unmarshal(requestBytes, &request); err != nil {
			missing = errors.Join(missing, err)
			continue
		}
		material := request.Engine.WorkerMaterial
		if !sameExecutionPath(request.Engine.Root, root) || request.Engine.RunID != ref.RunID || material.ActionID != ref.ActionID || request.Provider != descriptor.InstallName || !sameExecutionPath(filepath.Dir(filepath.Dir(material.Path)), stateDir) || !sameExecutionPath(directory, WorkerExecutionDirectory(material, request.Engine.ResumeActionID)) {
			missing = errors.Join(missing, fmt.Errorf("native cost request differs from its original run/action/provider/material"))
			continue
		}
		value, err := readWorkerMaterial(material)
		if err != nil {
			missing = errors.Join(missing, err)
			continue
		}
		if value.Input.RunID != ref.RunID || value.Input.Provider != ref.Provider || attempt != "" && material.AttemptID != attempt {
			missing = errors.Join(missing, fmt.Errorf("native cost material differs from its original run/attempt/provider"))
			continue
		}
		attempt = material.AttemptID
		file, err := os.Open(filepath.Join(directory, "source.json"))
		if err != nil {
			missing = errors.Join(missing, err)
			continue
		}
		var source map[string]any
		decoder := json.NewDecoder(file)
		decoder.UseNumber()
		err = decoder.Decode(&source)
		file.Close()
		if err != nil {
			missing = errors.Join(missing, err)
			continue
		}
		_, _, nativeIdentity, err := verify(descriptor.InstallName, directory, source)
		if err != nil {
			missing = errors.Join(missing, err)
			continue
		}
		if nativeIdentity != identity {
			missing = errors.Join(missing, fmt.Errorf("native cost source differs from its original lifecycle identity"))
			continue
		}
		path, _ := source["transcript"].(string)
		if path == "" {
			missing = errors.Join(missing, fmt.Errorf("native completion has no metering stream"))
			continue
		}
		if _, err := cost.ParseTranscript(ref.Provider, path); err != nil {
			missing = errors.Join(missing, fmt.Errorf("%s: %w", path, err))
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 && missing == nil {
		missing = fmt.Errorf("native dispatch has no completed metering source")
	}
	return paths, true, missing
}
