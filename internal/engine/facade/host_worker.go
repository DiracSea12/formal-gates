package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
)

func hostInvocation(request map[string]any) (host.CodexInvocation, error) {
	var ref host.CodexInvocation
	body, err := json.Marshal(request["host"])
	if err != nil {
		return ref, err
	}
	err = json.Unmarshal(body, &ref)
	return ref, err
}

// OpenWorkerMaterial derives the scope from a published immutable reference.
// Display positions are not execution identities and may change in parallel.
func OpenWorkerMaterial(path string) (*Facade, WorkerMaterial, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, WorkerMaterial{}, err
	}
	materialDirectory := filepath.Dir(abs)
	stateDirectory := filepath.Dir(materialDirectory)
	root := filepath.Dir(filepath.Dir(filepath.Dir(stateDirectory)))
	if filepath.Base(materialDirectory) != "materials" || filepath.Clean(filepath.Join(root, EngineNamespace, filepath.Base(stateDirectory), "materials")) != filepath.Clean(materialDirectory) {
		return nil, WorkerMaterial{}, fmt.Errorf("select an original published worker material path")
	}
	f, err := Open(root, filepath.Base(stateDirectory))
	if err != nil {
		return nil, WorkerMaterial{}, err
	}
	materials, err := f.originalWorkerMaterials()
	if err != nil {
		return nil, WorkerMaterial{}, err
	}
	for ref := range materials {
		if sameExecutionPath(ref.Path, abs) {
			return f, ref, nil
		}
	}
	return nil, WorkerMaterial{}, fmt.Errorf("worker material is not an original signed reference")
}

func (f *Facade) verifyHostWorker(request map[string]any, material worker.Material) (host.CodexInvocation, host.CodexTurn, error) {
	ref, err := hostInvocation(request)
	if err != nil {
		return ref, host.CodexTurn{}, err
	}
	if request["provider"] != "codex" {
		return ref, host.CodexTurn{}, fmt.Errorf("HOST_SUBAGENT_UNAVAILABLE: native host worker facts are unavailable for %v", request["provider"])
	}
	snap, err := f.engine.Load()
	if err != nil {
		return ref, host.CodexTurn{}, err
	}
	parent := snap.State.OwnerSession
	if request["controller"] != nil {
		body, err := json.Marshal(request["controller"])
		if err != nil {
			return ref, host.CodexTurn{}, err
		}
		var controller host.CodexInvocation
		if err := json.Unmarshal(body, &controller); err != nil {
			return ref, host.CodexTurn{}, err
		}
		if _, _, err := controller.Observe(); err != nil {
			return ref, host.CodexTurn{}, err
		}
		parent = controller.Session
	}
	if parent == "" || ref.Parent != parent {
		return ref, host.CodexTurn{}, fmt.Errorf("host worker parent differs from the actual prepared controller; run workflow drive from the native host first")
	}
	_, turn, err := ref.Observe()
	if err != nil {
		return ref, turn, err
	}
	choice := material.Input.Execution
	if turn.Model == "" || turn.Effort == "" || choice.Model != "" && choice.Model != turn.Model || choice.ReasoningEffort != "" && choice.ReasoningEffort != turn.Effort {
		return ref, turn, fmt.Errorf("host worker native model/effort differs or is unavailable: observed %s/%s, issued %s/%s", turn.Model, turn.Effort, choice.Model, choice.ReasoningEffort)
	}
	return ref, turn, nil
}

// ClaimHostWorker binds the current issued material to the actual calling
// child. The original native rollout supplies parent, turn and model facts.
func (f *Facade) ClaimHostWorker(material WorkerMaterial, resumeActionID, session, nativeHome string) (string, error) {
	request, _, directory, original, err := f.WorkerExecutionRequest(material, resumeActionID)
	if err != nil {
		return "", err
	}
	if original != material {
		return "", fmt.Errorf("host worker material changed; read the current issued material")
	}
	if body, readErr := os.ReadFile(filepath.Join(directory, "native-request.json")); readErr == nil {
		var prepared map[string]any
		if err := json.Unmarshal(body, &prepared); err != nil {
			return "", err
		}
		if prepared["controller"] != nil {
			invocation, err := readNativeWorkerInvocation(f.root, f.runID, directory)
			if err != nil {
				return "", err
			}
			if *invocation.binding.WorkerMaterial != material {
				return "", fmt.Errorf("prepared host worker material changed")
			}
			request["controller"] = prepared["controller"]
		}
	} else if !os.IsNotExist(readErr) {
		return "", readErr
	}
	if request["provider"] != "codex" {
		return "", fmt.Errorf("HOST_SUBAGENT_UNAVAILABLE: native host worker facts are unavailable for %v", request["provider"])
	}
	claimed, err := f.HasNativeWorkerExecution(material)
	if err != nil {
		return "", err
	}
	if claimed {
		return "", fmt.Errorf("worker already has an independent native invocation; observe that original source")
	}
	thread, err := host.FindCodexThread(nativeHome, session)
	if err != nil {
		return "", err
	}
	if !thread.SourceSubagent || len(thread.Turns) == 0 {
		return "", fmt.Errorf("host worker requires a real native child with a local turn")
	}
	continuation := ""
	if request["host"] == nil && resumeActionID == "" {
		continuation, err = f.HostWorkerContinuation(material)
		if err != nil {
			return "", err
		}
	}
	if continuation != "" && continuation != session {
		return "", fmt.Errorf("host continuation belongs to its original child")
	}
	if request["host"] == nil && resumeActionID == "" && continuation == "" && (len(thread.Turns) != 1 || thread.InheritedHistory) {
		return "", fmt.Errorf("host worker requires a fresh native child with fork_turns=none; an existing conversation cannot claim another action")
	}

	latest := thread.Turns[len(thread.Turns)-1]
	ref := host.CodexInvocation{Transcript: thread.Path, Session: thread.ID, Parent: thread.ParentThreadID, Turn: latest.ID}
	if previous, exists := request["host"]; exists {
		encoded, _ := json.Marshal(previous)
		var bound host.CodexInvocation
		if err := json.Unmarshal(encoded, &bound); err != nil {
			return "", err
		}
		if bound.Session != ref.Session || bound.Turn != ref.Turn {
			return "", fmt.Errorf("worker is already bound to another native child")
		}
		ref = bound
	}
	request["host"] = ref
	value, err := readWorkerMaterial(material)
	if err != nil {
		return "", err
	}
	if _, _, err := f.verifyHostWorker(request, value); err != nil {
		return "", err
	}
	release, err := f.acquireWriteWindow()
	if err != nil {
		return "", err
	}
	snap, err := f.engine.Load()
	if err == nil {
		if receipt, found := snap.State.SpawnReceipts[material.ActionID]; found && receipt.Correlation != ref.Session {
			err = fmt.Errorf("worker already belongs to another original execution")
		}
		if err == nil && continuation != "" {
			previous, checkErr := f.hostWorkerContinuation(snap.State, material)
			err = checkErr
			if err == nil {
				if previous == nil {
					err = fmt.Errorf("host continuation changed before claim")
				} else {
					prior, _ := hostInvocation(previous.request)
					if prior.Session != ref.Session || prior.Turn == ref.Turn {
						err = fmt.Errorf("host continuation requires a new original local turn")
					}
				}
			}
		}
		if err == nil {
			var existing map[string]any
			if body, readErr := os.ReadFile(filepath.Join(directory, "native-request.json")); readErr == nil {
				err = json.Unmarshal(body, &existing)
				if err == nil && existing["host"] != nil {
					bound, decodeErr := hostInvocation(existing)
					if decodeErr != nil {
						err = decodeErr
					} else if bound.Session != ref.Session || bound.Turn != ref.Turn {
						err = fmt.Errorf("worker is already bound to another native child")
					} else {
						request, ref = existing, bound
					}
				} else if err == nil {
					if existing["controller"] != nil {
						request["controller"] = existing["controller"]
					}
					if _, ownerErr := os.Stat(filepath.Join(directory, "owner")); ownerErr == nil {
						err = fmt.Errorf("worker already has an independent native owner")
					} else if !os.IsNotExist(ownerErr) {
						err = ownerErr
					}
				}
			} else if !os.IsNotExist(readErr) {
				err = readErr
			}
		}
		if err == nil {
			_, _, err = f.verifyHostWorker(request, value)
		}
		var input protocol.WorkerInput
		if err == nil {
			input, _, err = snap.State.WorkerExecutionIdentity(material.ActionID, material.AttemptID, resumeActionID, f.definition)
		}
		if err == nil && !reflect.DeepEqual(input.Pending, value.Input.Pending) {
			err = fmt.Errorf("host worker action/attempt changed before receipt")
		}
	}
	if err == nil {
		err = f.admission.write(func() error {
			if err := os.MkdirAll(directory, 0700); err != nil {
				return err
			}
			body, err := json.Marshal(request)
			if err != nil {
				return err
			}
			return writeFileAtomic(filepath.Join(directory, "native-request.json"), body, 0600)
		})
	}
	if err == nil {
		err = f.observeWorkerExecution(material, directory, f.HostWorkerStartEvidence)
	}
	release()
	if err != nil {
		return "", err
	}
	return directory, nil
}

func (f *Facade) hostWorkerReferencePath(session string) (string, error) {
	var registry struct {
		Records []struct {
			ID        string `json:"id"`
			StateRoot string `json:"stateRoot"`
		} `json:"records"`
	}
	body, err := os.ReadFile(f.admission.RegistryPath)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(body, &registry); err != nil {
		return "", err
	}
	for _, record := range registry.Records {
		if record.ID != f.admission.InstalledTargetIdentity {
			continue
		}
		if record.StateRoot == "" {
			return "", fmt.Errorf("owning installation has no hook state root")
		}
		return filepath.Join(record.StateRoot, "host-workers", session+".json"), nil
	}
	return "", fmt.Errorf("owning installation record is unavailable")
}

func (f *Facade) hostWorkerContinuation(state *protocol.State, material WorkerMaterial) (*nativeWorkerInvocation, error) {
	input, _, err := state.WorkerExecutionIdentity(material.ActionID, material.AttemptID, "", f.definition)
	if err != nil {
		return nil, err
	}
	value, err := readWorkerMaterial(material)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(input.Pending, value.Input.Pending) || !reflect.DeepEqual(input.Execution, value.Input.Execution) || !reflect.DeepEqual(input.Validation, value.Input.Validation) {
		return nil, fmt.Errorf("host continuation input changed")
	}
	var result *protocol.WorkerResult
	if input.Validation != nil {
		if input.Validation.Development != nil {
			result = input.Validation.Development.Result
		}
		if input.Validation.Repair != nil {
			result = input.Validation.Repair.Result
		}
	}
	if result == nil {
		return nil, nil
	}
	if !reflect.DeepEqual(*result, state.Results[material.ActionID]) {
		return nil, fmt.Errorf("host continuation differs from its accepted result")
	}
	receipt, exists := state.SpawnReceipts[material.ActionID]
	if !exists {
		return nil, nil
	}
	path, err := f.hostWorkerReferencePath(receipt.Correlation)
	if err != nil {
		return nil, err
	}
	var pointer struct {
		Job string `json:"job"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &pointer); err != nil {
		return nil, err
	}
	previous, err := readNativeWorkerInvocation(f.root, f.runID, pointer.Job)
	if err != nil {
		return nil, err
	}
	old := *previous.binding.WorkerMaterial
	if old == material || old.ActionID != material.ActionID || old.AttemptID != material.AttemptID {
		return nil, fmt.Errorf("host continuation has no original previous invocation")
	}
	acceptance := state.Events[string(WorkerResultEventID(old, previous.binding.ResumeActionID))].Acceptance
	if acceptance.Status != "ACCEPTED" || acceptance.ActionID != material.ActionID {
		return nil, fmt.Errorf("host continuation requires its accepted original result")
	}
	ref, turn, err := f.verifyHostWorker(previous.request, previous.material)
	if err != nil {
		return nil, err
	}
	if ref.Session != receipt.Correlation || turn.State != "completed" {
		return nil, fmt.Errorf("host continuation requires the original completed child")
	}
	return &previous, nil
}

// HostWorkerContinuation projects the original child for a newly issued call.
func (f *Facade) HostWorkerContinuation(material WorkerMaterial) (string, error) {
	snap, err := f.engine.Load()
	if err != nil {
		return "", err
	}
	previous, err := f.hostWorkerContinuation(snap.State, material)
	if err != nil || previous == nil {
		return "", err
	}
	ref, err := hostInvocation(previous.request)
	return ref.Session, err
}

func (f *Facade) publishHostWorkerReference(session, directory, resumeActionID string) error {
	return f.admission.write(func() error {
		path, err := f.hostWorkerReferencePath(session)
		if err != nil {
			return err
		}
		var previous struct {
			Job string `json:"job"`
		}
		if body, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(body, &previous); err != nil {
				return err
			}
			if sameExecutionPath(previous.Job, directory) {
				return nil
			}
			old, err := readNativeWorkerInvocation(f.root, f.runID, previous.Job)
			if err != nil {
				return err
			}
			current, err := readNativeWorkerInvocation(f.root, f.runID, directory)
			if err != nil {
				return err
			}
			if old.binding.WorkerMaterial.ActionID != current.binding.WorkerMaterial.ActionID || old.binding.WorkerMaterial.AttemptID != current.binding.WorkerMaterial.AttemptID {
				return fmt.Errorf("native child already references another issued worker")
			}
			prior, err := hostInvocation(old.request)
			if err != nil {
				return err
			}
			candidate, err := hostInvocation(current.request)
			if err != nil {
				return err
			}
			if prior.Session != session || candidate.Session != session {
				return fmt.Errorf("host reference child identity differs")
			}
			thread, _, err := candidate.Observe()
			if err != nil {
				return err
			}
			priorIndex, candidateIndex := -1, -1
			for index, turn := range thread.Turns {
				if turn.ID == prior.Turn {
					priorIndex = index
				}
				if turn.ID == candidate.Turn {
					candidateIndex = index
				}
			}
			if priorIndex >= candidateIndex && candidateIndex >= 0 {
				return nil
			}
			if resumeActionID == "" {
				snap, err := f.engine.Load()
				if err != nil {
					return err
				}
				continuation, err := f.hostWorkerContinuation(snap.State, *current.binding.WorkerMaterial)
				if err != nil {
					return err
				}
				if continuation == nil || !sameExecutionPath(continuation.directory, old.directory) {
					return fmt.Errorf("host reference replacement has no accepted original continuation")
				}
			}
			if priorIndex < 0 || candidateIndex < 0 {
				return fmt.Errorf("host reference replacement has no ordered original turns")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		body, err := json.Marshal(map[string]string{"job": directory})
		if err != nil {
			return err
		}
		return writeFileAtomic(path, body, 0600)
	})
}

func (f *Facade) HostWorkerStartEvidence(provider, directory string) (string, error) {
	invocation, err := readNativeWorkerInvocation(f.root, f.runID, directory)
	if err != nil {
		return "", err
	}
	if invocation.request["provider"] != provider {
		return "", fmt.Errorf("host worker provider differs")
	}
	ref, _, err := f.verifyHostWorker(invocation.request, invocation.material)
	return ref.Session, err
}

// HostWorkerExecution locates an original claimed child using real caller
// scope. Observers may supply its original material; tools require its identity.
func (f *Facade) HostWorkerExecution(material WorkerMaterial, session string, observer bool) (string, host.CodexTurn, error) {
	invocations, err := claimedWorkerInvocations(f.root, f.runID, material.ActionID, material.AttemptID)
	if err != nil {
		return "", host.CodexTurn{}, err
	}
	snap, err := f.engine.Load()
	if err != nil {
		return "", host.CodexTurn{}, err
	}
	var selected *nativeWorkerInvocation
	for i := range invocations {
		invocation := &invocations[i]
		if invocation.request["host"] == nil {
			continue
		}
		if workerInvocationDelivered(snap.State, *invocation) {
			if *invocation.binding.WorkerMaterial != material {
				continue
			}
			result := snap.State.Events[string(WorkerResultEventID(*invocation.binding.WorkerMaterial, invocation.binding.ResumeActionID))].Acceptance
			if result.ActionID != material.ActionID {
				continue
			}
		}
		if selected != nil {
			return "", host.CodexTurn{}, fmt.Errorf("worker has more than one current original host invocation")
		}
		selected = invocation
	}
	if selected != nil {
		invocation := selected
		ref, turn, err := f.verifyHostWorker(invocation.request, invocation.material)
		if err != nil {
			return "", turn, err
		}
		if !observer && ref.Session != session {
			return "", turn, fmt.Errorf("worker tools belong to another native child")
		}
		return invocation.directory, turn, nil
	}
	return "", host.CodexTurn{}, fmt.Errorf("host worker has no original claimed child")
}

func (f *Facade) HostWorkerMaterial(session string) (WorkerMaterial, error) {
	materials, err := f.originalWorkerMaterials()
	if err != nil {
		return WorkerMaterial{}, err
	}
	for material := range materials {
		invocations, err := claimedWorkerInvocations(f.root, f.runID, material.ActionID, material.AttemptID)
		if err != nil {
			return WorkerMaterial{}, err
		}
		for _, invocation := range invocations {
			if invocation.request["host"] == nil {
				continue
			}
			ref, _, err := f.verifyHostWorker(invocation.request, invocation.material)
			if err != nil {
				return WorkerMaterial{}, err
			}
			if ref.Session == session {
				return material, nil
			}
		}
	}
	return WorkerMaterial{}, fmt.Errorf("calling native child has no issued worker binding")
}

// HostWorkerTermination observes the signed effect. The controlling host owns
// interrupting its actual child; terminal native turns supply the stop fact.
func (f *Facade) HostWorkerTermination(intent protocol.HostActionIntent) (handled, stopped bool, err error) {
	release, err := f.acquireWriteWindow()
	if err != nil {
		return false, false, err
	}
	defer release()
	snap, err := f.engine.Load()
	if err != nil {
		return false, false, err
	}
	if current, known := snap.State.ExecutableHostActions(f.definition)[intent.ActionID]; !known || !reflect.DeepEqual(current, intent) {
		return false, false, fmt.Errorf("host termination has no current signed intent")
	}
	return f.observeHostWorkerTermination(intent)
}

func (f *Facade) observeHostWorkerTermination(intent protocol.HostActionIntent) (handled, stopped bool, err error) {
	if intent.Terminate == nil {
		return false, false, nil
	}
	target := intent.Terminate
	invocations, err := claimedWorkerInvocations(f.root, f.runID, target.WorkerActionID, target.AttemptID)
	if err != nil {
		return false, false, err
	}
	if len(invocations) == 0 {
		return false, false, nil
	}
	for _, invocation := range invocations {
		if invocation.request["host"] == nil {
			return false, false, nil
		}
	}
	for _, invocation := range invocations {
		ref, turn, err := f.verifyHostWorker(invocation.request, invocation.material)
		if err != nil {
			return true, false, err
		}
		if ref.Session != target.Identity {
			return true, false, fmt.Errorf("host termination identity differs from its original child")
		}
		if turn.State == "running" {
			return true, false, nil
		}
	}
	for _, invocation := range invocations {
		if err := f.observeWorkerExecution(*invocation.binding.WorkerMaterial, invocation.directory, f.HostWorkerStartEvidence); err != nil {
			return true, false, err
		}
	}
	provider := invocations[0].material.Input.Provider
	correlation := intent.Correlation
	if correlation == "" {
		correlation = intent.ActionID
	}
	event, err := protocol.NewAgentHostActionReceiptEvent(protocol.EventID("native-termination:"+intent.ActionID), intent.ActionID, protocol.HostActionTerminateAgent, provider, correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: target.Identity, Event: protocol.LifecycleStop})
	if err != nil {
		return true, false, err
	}
	_, err = f.engine.Submit(event, protocol.Observation{})
	return true, err == nil, err
}
