package facade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

// NativeSourceVerifier is installed once by the native command assembly. It
// retains the adapters' original source verification and has no workflow state.
var NativeSourceVerifier func(string, string, map[string]any) ([]byte, string, string, error)

// CostSourceBridge resolves original dispatch evidence. Managed native jobs
// share their provider verifier and raw streams.
func CostSourceBridge(root, runID string, verifier ...func(string, string, map[string]any) ([]byte, string, string, error)) protocol.CostSourceResolver {
	verify := NativeSourceVerifier
	if len(verifier) > 0 {
		verify = verifier[0]
	}
	return func(ref protocol.CostSourceRef) protocol.ResolvedCostSource {
		if ref.RunID != runID {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: "cost source differs from its owning run"}
		}
		verification, err := lifecycle.VerifyDispatch(root, ref.RunID, ref.ActionID)
		if err != nil {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: err.Error()}
		}
		if verification.Provider == "" {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: "dispatch has no lifecycle binding"}
		}
		provider := verification.Provider
		if provider != ref.Provider {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: "cost source provider does not match dispatch provider"}
		}
		if !verification.StopObserved {
			if verification.Outcome == lifecycle.Unavailable {
				return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Reason: "host lifecycle evidence is unavailable"}
			}
			return protocol.ResolvedCostSource{State: protocol.CostSourcePending}
		}
		paths, sourceErr := nativeCostSourcePaths(root, ref, verification.Identity, verify)
		usage, parseErr := cost.ParseTranscriptFiles(provider, paths...)
		if err := errors.Join(sourceErr, parseErr); err != nil {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable, Usage: usage, Reason: err.Error()}
		}
		return protocol.ResolvedCostSource{State: protocol.CostSourceReady, Usage: usage}

	}
}

// dispatchClaimBridge 在派发签出落账时，以宿主回执的实际派发身份建立
// lifecycle journal 绑定；identity 不可变，native 观察核对该身份。
func dispatchClaimBridge(root, owningRunID string, admission Admission) protocol.DispatchClaim {
	return func(runID, actionID, identity, provider string) error {
		if runID != owningRunID {
			return fmt.Errorf("UNREGISTERED_INSTALL: dispatch claim does not match its owning run")
		}
		return admission.write(func() error { return lifecycle.BindDispatchWithProvider(root, runID, actionID, identity, provider) })
	}
}

func nativeCostSourcePaths(root string, ref protocol.CostSourceRef, identity string, verify func(string, string, map[string]any) ([]byte, string, string, error)) ([]string, error) {
	stateDir := filepath.Join(root, EngineNamespace, ref.RunID)
	actionDir := workerActionDirectory(stateDir, ref.ActionID)
	entries, err := os.ReadDir(actionDir)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("native dispatch has no completed metering source")
	}
	if err != nil {
		return nil, err
	}
	if verify == nil {
		return nil, fmt.Errorf("native cost source verification is unavailable")
	}
	descriptor, err := host.Lookup(ref.Provider)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	attempt := ""
	var missing error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(actionDir, entry.Name())
		if _, err := os.Stat(filepath.Join(directory, "owner")); os.IsNotExist(err) {
			var source struct {
				Host *host.CodexInvocation `json:"host"`
			}
			body, readErr := os.ReadFile(filepath.Join(directory, "native-request.json"))
			if os.IsNotExist(readErr) {
				continue
			}
			if readErr != nil {
				missing = errors.Join(missing, readErr)
				continue
			}
			if err := json.Unmarshal(body, &source); err != nil {
				missing = errors.Join(missing, err)
				continue
			}
			if source.Host == nil {
				continue
			}
			if _, _, err := source.Host.Observe(); err != nil {
				missing = errors.Join(missing, err)
				continue
			}
		} else if err != nil {
			missing = errors.Join(missing, err)
			continue
		}
		requestBytes, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
		if err != nil {
			missing = errors.Join(missing, err)
			continue
		}
		var request struct {
			Provider string                `json:"provider"`
			Host     *host.CodexInvocation `json:"host"`
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
		if request.Host != nil {
			if _, err := readNativeWorkerInvocation(root, ref.RunID, directory); err != nil {
				missing = errors.Join(missing, err)
				continue
			}
			_, turn, err := request.Host.Observe()
			if err != nil || request.Host.Session != identity || turn.State == "running" {
				missing = errors.Join(missing, err, fmt.Errorf("host metering source has no terminal original identity"))
				continue
			}
			path := request.Host.Transcript
			if _, err := cost.ParseTranscript(ref.Provider, path); err != nil {
				missing = errors.Join(missing, err)
				continue
			}
			if !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
			continue
		}
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
			usageEvidence, _ := source["usage_evidence"].(map[string]any)
			reason, _ := usageEvidence["unavailableReason"].(string)
			if reason == "" {
				reason = "native completion has no metering stream"
			}
			missing = errors.Join(missing, errors.New(reason))
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
	return paths, missing
}
