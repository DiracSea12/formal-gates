package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"formal-gates/internal/prompts"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"formal-gates/internal/atomicfile"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

// NativeStartVerifier is installed by the native command assembly. It retains
// the provider's acknowledgement validation without workflow state.
var NativeStartVerifier func(string, string) (string, error)

type nativeWorkerBinding struct {
	Root           string          `json:"root"`
	RunID          string          `json:"runId"`
	WorkerMaterial *WorkerMaterial `json:"workerMaterial"`
	ActionID       string          `json:"actionId"`
	ResumeActionID string          `json:"resumeActionId"`
}

type nativeWorkerInvocation struct {
	directory string
	request   map[string]any
	binding   nativeWorkerBinding
	material  worker.Material
}

func readNativeWorkerInvocation(root, runID, directory string) (nativeWorkerInvocation, error) {
	invocation := nativeWorkerInvocation{directory: directory}
	body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return invocation, err
	}
	if err := json.Unmarshal(body, &invocation.request); err != nil {
		return invocation, err
	}
	binding, err := json.Marshal(invocation.request["engine"])
	if err != nil {
		return invocation, err
	}
	decoder := json.NewDecoder(bytes.NewReader(binding))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&invocation.binding); err != nil {
		return invocation, err
	}
	ref := invocation.binding.WorkerMaterial
	if ref == nil {
		return invocation, fmt.Errorf("native worker requires its original material reference")
	}
	invocation.material, err = readWorkerMaterial(*ref)
	if err != nil {
		return invocation, err
	}
	provider := invocation.material.Input.Provider
	if descriptor, lookupErr := host.Lookup(provider); lookupErr == nil {
		provider = descriptor.InstallName
	}
	stateDir := filepath.Join(root, EngineNamespace, runID)
	workspace := root
	if invocation.material.Input.Validation != nil && invocation.material.Input.Validation.Workspace != nil {
		workspace = invocation.material.Input.Validation.Workspace.Path
	}
	if !sameExecutionPath(invocation.binding.Root, root) || invocation.binding.RunID != runID || invocation.material.Input.RunID != runID || invocation.request["provider"] != provider || !sameExecutionPath(filepath.Dir(filepath.Dir(ref.Path)), stateDir) || !sameExecutionPath(directory, WorkerExecutionDirectory(*ref, invocation.binding.ResumeActionID)) || invocation.request["prompt"] != invocation.material.Prompt || !sameExecutionPath(fmt.Sprint(invocation.request["workspace"]), workspace) || invocation.request["read_only"] != invocation.material.ReadOnly || !jsonValuesEqual(invocation.request["schema"], invocation.material.ResultSchema) {
		return invocation, fmt.Errorf("native worker invocation differs from its original run/material/request")
	}
	return invocation, nil
}

func claimedWorkerInvocations(root, runID, actionID, attemptID string) ([]nativeWorkerInvocation, error) {
	actionDir := workerActionDirectory(filepath.Join(root, EngineNamespace, runID), actionID)
	entries, err := os.ReadDir(actionDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var invocations []nativeWorkerInvocation
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(actionDir, entry.Name())
		if _, err := os.Stat(filepath.Join(directory, "owner")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		invocation, err := readNativeWorkerInvocation(root, runID, directory)
		if err != nil {
			return nil, err
		}
		ref := invocation.binding.WorkerMaterial
		if ref.ActionID != actionID || ref.AttemptID != attemptID {
			return nil, fmt.Errorf("native worker owner differs from its original action/attempt")
		}
		invocations = append(invocations, invocation)
	}
	return invocations, nil
}

// workerExecutionBridge observes only jobs claimed under AdmitExecution's native window.
func workerExecutionBridge(root, runID string) func(protocol.PendingAction, protocol.Attempt) (*protocol.SpawnReceiptPayload, error) {
	return func(pending protocol.PendingAction, attempt protocol.Attempt) (*protocol.SpawnReceiptPayload, error) {
		invocations, err := claimedWorkerInvocations(root, runID, pending.ActionID, attempt.ID)
		if err != nil {
			return nil, err
		}
		for _, invocation := range invocations {
			provider := invocation.request["provider"].(string)
			fact := &protocol.SpawnReceiptPayload{ActionID: pending.ActionID, Provider: invocation.material.Input.Provider, Correlation: invocation.directory, Status: protocol.SpawnStatusUnknown}
			if _, err := os.Stat(filepath.Join(invocation.directory, "identity.json")); err == nil && NativeStartVerifier != nil {
				identity, err := NativeStartVerifier(provider, invocation.directory)
				if err != nil {
					return nil, err
				}
				fact.Correlation, fact.Status = identity, protocol.SpawnStatusSpawned
			} else if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			return fact, nil
		}
		return nil, nil
	}
}

func WorkerResultEventID(material WorkerMaterial, resumeActionID string) protocol.EventID {
	return protocol.EventID("native-result:" + material.Digest + ":" + resumeActionID)
}
func WorkerFailureEventID(material WorkerMaterial, resumeActionID string) protocol.EventID {
	return protocol.EventID("native-failure:" + material.Digest + ":" + resumeActionID)
}
func WorkerSpawnFailureEventID(material WorkerMaterial) protocol.EventID {
	return protocol.EventID("native-failed-spawn:" + material.Digest)
}

func workerInvocationDelivered(state *protocol.State, invocation nativeWorkerInvocation) bool {
	ref := *invocation.binding.WorkerMaterial
	for _, eventID := range []protocol.EventID{WorkerResultEventID(ref, invocation.binding.ResumeActionID), WorkerFailureEventID(ref, invocation.binding.ResumeActionID), WorkerSpawnFailureEventID(ref)} {
		acceptance := state.Events[string(eventID)].Acceptance
		if acceptance.ActionID != ref.ActionID {
			continue
		}
		switch acceptance.Status {
		case "ACCEPTED", "DUPLICATE", "OBSOLETE_RESULT":
			return true
		case "STAGED", protocol.StatusCostPending, "QUARANTINED":
			// Delivered evidence remains in its original holding projection until
			// the same acceptance path promotes it; it cannot start another host call.
			return true
		}
	}
	return false
}

// AdmitExecution is the native/supervisor command's pre-launch integration.
// A nil release identifies independent harness work, outside an engine run.
// A non-nil release holds the existing native window through job ownership.
func AdmitExecution(directory string, request map[string]any) (release func(), err error) {
	ref, managed := request["engine"]
	if !managed {
		if executionInEngineWorkspace(directory, request) {
			return nil, fmt.Errorf("UNSUPPORTED_EXECUTION: engine work requires its original issued reference")
		}
		return nil, nil
	}
	var binding nativeWorkerBinding
	body, err := json.Marshal(ref)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&binding); err != nil {
		return nil, err
	}
	if binding.Root == "" || binding.RunID == "" {
		return nil, fmt.Errorf("engine execution requires its root and run")
	}
	outer := request
	if request["kind"] == "native" {
		body, readErr := os.ReadFile(filepath.Join(directory, "native-request.json"))
		if readErr != nil {
			return nil, readErr
		}
		request = nil
		if err := json.Unmarshal(body, &request); err != nil {
			return nil, err
		}
		if !jsonValuesEqual(request["engine"], ref) {
			return nil, fmt.Errorf("supervised native request has a different engine reference")
		}
	}
	f, err := Open(binding.Root, binding.RunID)
	if err != nil {
		return nil, err
	}
	release, err = f.acquireWriteWindow()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			release()
			release = nil
		}
	}()
	snap, err := f.engine.Load()
	if err != nil {
		return release, err
	}
	if binding.WorkerMaterial == nil {
		return release, f.admitCheckExecution(snap.State, binding.ActionID, directory, request)
	}
	material := *binding.WorkerMaterial
	if request["probe"] == true {
		return release, fmt.Errorf("an issued worker requires execution; host probes are independent harness work")
	}
	input, identity, err := snap.State.WorkerExecutionIdentity(material.ActionID, material.AttemptID, binding.ResumeActionID, f.definition)
	if err != nil {
		return release, err
	}
	var value worker.Material
	var originalRequest map[string]any
	if _, ownerErr := os.Stat(filepath.Join(directory, "owner")); ownerErr == nil {
		original, err := readNativeWorkerInvocation(f.root, f.runID, directory)
		if err != nil {
			return release, err
		}
		if *original.binding.WorkerMaterial != material || original.binding.ResumeActionID != binding.ResumeActionID || !reflect.DeepEqual(original.material.Input.Pending, input.Pending) || workerInvocationDelivered(snap.State, original) {
			return release, fmt.Errorf("worker execution differs from its claimed original action/attempt")
		}
		value = original.material
		originalRequest = original.request
	} else if !os.IsNotExist(ownerErr) {
		return release, ownerErr
	} else {
		if binding.ResumeActionID != "" && snap.State.HostActionReceipts[binding.ResumeActionID].Status != "" {
			return release, fmt.Errorf("worker resume is already executed; observe its original claimed invocation")
		}
		catalog, err := prompts.LoadPromptCatalog(f.admission.Target)
		if err != nil {
			return release, err
		}
		expected, err := f.workerMaterial(input, catalog, false)
		if err != nil {
			return release, err
		}
		if expected != material {
			return release, fmt.Errorf("worker execution material differs from current issuance")
		}
		value, err = readWorkerMaterial(material)
		if err != nil {
			return release, err
		}
	}
	if !sameExecutionPath(directory, WorkerExecutionDirectory(material, binding.ResumeActionID)) {
		return release, fmt.Errorf("worker execution must use its original owned job directory")
	}
	provider := value.Input.Provider
	if descriptor, lookupErr := host.Lookup(provider); lookupErr == nil {
		provider = descriptor.InstallName
	}
	workspace := f.root
	if value.Input.Validation != nil && value.Input.Validation.Workspace != nil {
		workspace = value.Input.Validation.Workspace.Path
	}
	known := map[string]any{"provider": provider, "prompt": value.Prompt, "schema": value.ResultSchema, "read_only": value.ReadOnly, "workspace": workspace, "runtime_files": f.runtimeFiles}
	known["model"] = value.Input.Execution.Model
	known["reasoning_effort"] = value.Input.Execution.ReasoningEffort
	if originalRequest != nil {
		for _, key := range []string{"binding", "permission_mode", "native_store", "model", "reasoning_effort", "runtime_files"} {
			known[key] = originalRequest[key]
		}
	}
	for key, expected := range known {
		if actual, supplied := request[key]; supplied && !jsonValuesEqual(actual, expected) {
			return release, fmt.Errorf("worker execution %s differs from issued material", key)
		}
		if expected == nil || originalRequest == nil && (key == "model" || key == "reasoning_effort") && expected == "" {
			delete(request, key)
		} else {
			request[key] = expected
		}
	}
	for key := range request {
		switch key {
		case "engine", "provider", "prompt", "schema", "read_only", "workspace", "identity", "binding", "model", "reasoning_effort", "permission_mode", "native_store", "probe", "runtime_files":
		default:
			return release, fmt.Errorf("worker execution has unissued field %s", key)
		}
	}
	if requested, _ := request["identity"].(string); identity != "" && requested != "" && requested != identity {
		return release, fmt.Errorf("worker resume identity differs from its signed intent")
	}
	if identity != "" {
		request["identity"] = identity
	}
	if outer["kind"] == "native" {
		expected := f.workerSupervisorRequest(directory, workspace, ref)
		if !jsonValuesEqual(outer, expected) {
			return release, fmt.Errorf("supervised native invocation differs from the issued worker")
		}
		if err := f.admission.write(func() error {
			body, err := json.Marshal(request)
			if err != nil {
				return err
			}
			return writeFileAtomic(filepath.Join(directory, "native-request.json"), body, 0600)
		}); err != nil {
			return release, err
		}
	}
	return release, nil
}

func sameExecutionPath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	aa, err := os.Stat(a)
	if err != nil {
		return false
	}
	bb, err := os.Stat(b)
	return err == nil && os.SameFile(aa, bb)
}

// WorkerExecutionRequest assembles only the original worker payload. Native
// host configuration is captured separately by its existing transport owner.
func (f *Facade) WorkerExecutionRequest(material WorkerMaterial, resumeActionID string) (map[string]any, map[string]any, string, WorkerMaterial, error) {
	release, err := f.acquireWriteWindow()
	if err != nil {
		return nil, nil, "", WorkerMaterial{}, err
	}
	snap, err := f.engine.Load()
	var input protocol.WorkerInput
	if err == nil {
		input, _, err = snap.State.WorkerExecutionIdentity(material.ActionID, material.AttemptID, resumeActionID, f.definition)
	}
	var selected *nativeWorkerInvocation
	var resumeRequest map[string]any
	if err == nil {
		var invocations []nativeWorkerInvocation
		invocations, err = claimedWorkerInvocations(f.root, f.runID, material.ActionID, material.AttemptID)
		for i := range invocations {
			invocation := &invocations[i]
			if resumeActionID != "" && invocation.binding.ResumeActionID == "" {
				resumeRequest = invocation.request
			}
			if resumeActionID != "" && invocation.binding.ResumeActionID != resumeActionID || workerInvocationDelivered(snap.State, *invocation) {
				continue
			}
			if selected != nil {
				err = fmt.Errorf("worker has more than one undelivered original invocation")
				break
			}
			selected = invocation
		}
		if err == nil && selected == nil {
			catalog, loadErr := prompts.LoadPromptCatalog(f.admission.Target)
			if loadErr != nil {
				err = loadErr
			} else {
				material, err = f.workerMaterial(input, catalog, true)
			}
		}
	}
	release()
	if err != nil {
		return nil, nil, "", WorkerMaterial{}, err
	}
	if selected != nil {
		material = *selected.binding.WorkerMaterial
		release, err := AdmitExecution(selected.directory, selected.request)
		if err != nil {
			return nil, nil, "", WorkerMaterial{}, err
		}
		release()
		return selected.request, f.workerSupervisorRequest(selected.directory, selected.request["workspace"].(string), selected.request["engine"]), selected.directory, material, nil
	}
	directory := WorkerExecutionDirectory(material, resumeActionID)
	request := map[string]any{"engine": map[string]any{"root": f.root, "runId": f.runID, "workerMaterial": material, "resumeActionId": resumeActionID}}
	if resumeRequest != nil {
		for _, key := range []string{"binding", "permission_mode", "native_store"} {
			if value, ok := resumeRequest[key]; ok {
				request[key] = value
			}
		}
	}
	release, err = AdmitExecution(directory, request)
	if err != nil {
		return nil, nil, "", WorkerMaterial{}, err
	}
	release()
	return request, f.workerSupervisorRequest(directory, request["workspace"].(string), request["engine"]), directory, material, nil
}

func (f *Facade) workerSupervisorRequest(directory, workspace string, ref any) map[string]any {
	return map[string]any{"kind": "native", "argv": []string{filepath.Join(f.admission.Target, "bin", "formal-gates"), "harness", "native", directory}, "cwd": workspace, "stdin": "", "environment": map[string]any{}, "engine": ref}
}

// ObserveWorkerTerminal records a verified invocation's final identity in the
// original normalized journal. Result/source validation belongs to the caller.
func (f *Facade) ObserveWorkerTerminal(material WorkerMaterial, identity string) error {
	value, err := readWorkerMaterial(material)
	if err != nil {
		return err
	}
	return f.admission.write(func() error {
		return lifecycle.ObserveNativeCompletion(f.root, f.runID, material.ActionID, value.Input.Provider, identity)
	})
}

// ObserveWorkerStart records the original invocation's verified start and any
// observed exit. Native execution facts are independent of semantic results.
func (f *Facade) ObserveWorkerStart(material WorkerMaterial, directory string, verifyStart func(string, string) (string, error)) error {
	release, err := f.acquireWriteWindow()
	if err != nil {
		return err
	}
	defer release()
	return f.observeWorkerExecution(material, directory, verifyStart)
}

func (f *Facade) observeWorkerExecution(material WorkerMaterial, directory string, verifyStart func(string, string) (string, error)) error {
	invocation, err := readNativeWorkerInvocation(f.root, f.runID, directory)
	if err != nil {
		return err
	}
	if *invocation.binding.WorkerMaterial != material {
		return fmt.Errorf("native observation differs from its original worker material")
	}
	value := invocation.material
	provider := value.Input.Provider
	if descriptor, lookupErr := host.Lookup(provider); lookupErr == nil {
		provider = descriptor.InstallName
	}
	identity, err := verifyStart(provider, directory)
	if err != nil {
		return err
	}
	event, err := protocol.NewSpawnReceiptEvent(protocol.EventID("native-spawn:"+material.ActionID), material.ActionID, value.Input.Provider, identity, protocol.SpawnStatusSpawned)
	if err != nil {
		return err
	}
	if _, err := f.engine.Submit(event, protocol.Observation{}); err != nil {
		return err
	}
	resumeID := invocation.binding.ResumeActionID
	correlation := identity
	if resumeID != "" {
		correlation = resumeID
	}
	start, err := protocol.NewCorrelatedLifecycleEvent(protocol.EventID("native-start:"+material.Digest+":"+resumeID), value.Input.Provider, correlation, identity, protocol.LifecycleStart)
	if err != nil {
		return err
	}
	if _, err = f.engine.Submit(start, protocol.Observation{}); err != nil {
		return err
	}
	if err := f.admission.write(func() error {
		return lifecycle.ObserveNativeStart(f.root, f.runID, material.ActionID, value.Input.Provider, identity)
	}); err != nil {
		return err
	}
	if resumeID != "" {
		snap, err := f.engine.Load()
		if err != nil {
			return err
		}
		intent := snap.State.PendingHostActions[resumeID]
		if intent.Resume != nil {
			if intent.Resume.WorkerActionID != material.ActionID || intent.Resume.AttemptID != material.AttemptID || intent.Resume.Identity != identity {
				return fmt.Errorf("native resume observation differs from its original signed target")
			}
			event, err := protocol.NewAgentHostActionReceiptEvent(protocol.EventID("native-resume:"+resumeID), resumeID, protocol.HostActionResumeAgent, value.Input.Provider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: identity, Event: protocol.LifecycleStart})
			if err != nil {
				return err
			}
			if _, err := f.engine.Submit(event, protocol.Observation{}); err != nil {
				return err
			}
		}
	}
	var exit struct {
		ExitCode      *int   `json:"exit_code"`
		RequestDigest string `json:"request_digest"`
	}
	body, err := atomicfile.ReadFile(filepath.Join(directory, "exit.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, &exit); err != nil || exit.ExitCode == nil {
		return fmt.Errorf("native terminal observation requires its original exit receipt")
	}
	var process struct {
		RequestDigest string `json:"request_digest"`
	}
	body, err = atomicfile.ReadFile(filepath.Join(directory, "process.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, &process); err != nil || process.RequestDigest == "" || exit.RequestDigest != process.RequestDigest {
		return fmt.Errorf("native terminal observation differs from its original owned process")
	}
	if err := f.ObserveWorkerTerminal(material, identity); err != nil {
		return err
	}
	stop, err := protocol.NewCorrelatedLifecycleEvent(protocol.EventID("native-stopped:"+material.Digest+":"+resumeID), value.Input.Provider, correlation, identity, protocol.LifecycleStop)
	if err != nil {
		return err
	}
	_, err = f.engine.Submit(stop, protocol.Observation{})
	return err
}

// RequestWorkflowChoice creates only an original control Ask. The current
// engine predicate owns whether a user choice is actually due.
func (f *Facade) RequestWorkflowChoice() (bool, error) {
	snap, err := f.engine.Load()
	if err != nil {
		return false, err
	}
	control := snap.State.NextWorkflowChoice(f.definition)
	if control == "" {
		return false, nil
	}
	id := protocol.EventID(fmt.Sprintf("native-choice:%s:%d", control, snap.Revision))
	var event protocol.Event
	if control == protocol.ControlConfirmTopology {
		event, err = protocol.NewRequestEvent(id, control, protocol.AskOption{ID: "no-split", Label: "Keep one development workspace"}, protocol.AskOption{ID: "split", Label: "Use the reviewed development slices"})
	} else {
		request, loadErr := f.routeRequestPayload(snap.State.SliceChild != nil)
		if loadErr != nil {
			return false, loadErr
		}
		event, err = protocol.NewRouteRequestEvent(id, request.RouteGates, request.Options...)
	}
	if err != nil {
		return false, err
	}
	_, _, err = f.Submit(event)
	return err == nil, err
}

// AgentExecution reads the signed native responsibility and its original jobs
// under the same window used by worker starts and control decisions.
func (f *Facade) AgentExecution(intent protocol.HostActionIntent) (provider string, directories []string, release func(), err error) {
	release, err = f.acquireWriteWindow()
	if err != nil {
		return "", nil, nil, err
	}
	defer func() {
		if err != nil {
			release()
			release = nil
		}
	}()
	snap, err := f.engine.Load()
	if err != nil {
		return "", nil, release, err
	}
	if current, known := snap.State.ExecutableHostActions(f.definition)[intent.ActionID]; !known || !reflect.DeepEqual(current, intent) {
		return "", nil, release, fmt.Errorf("native agent effect has no current signed intent")
	}
	target := intent.Terminate
	if target == nil {
		target = intent.Resume
	}
	if target == nil {
		return "", nil, release, fmt.Errorf("native agent effect has no original target")
	}
	invocations, err := claimedWorkerInvocations(f.root, f.runID, target.WorkerActionID, target.AttemptID)
	if err != nil {
		return "", nil, release, err
	}
	for _, invocation := range invocations {
		directories = append(directories, invocation.directory)
	}
	choice, ok := snap.State.ActionExecutionChoice(target.WorkerActionID)
	if !ok {
		return "", nil, release, fmt.Errorf("native agent effect has no original execution choice")
	}
	return choice.Provider, directories, release, nil
}

// WorkerExecutionDirectory locates evidence for the original material or signed
// resume. Directory selection cannot create another execution of that input.
func WorkerExecutionDirectory(material WorkerMaterial, resumeActionID string) string {
	call := sha256.Sum256([]byte(material.Digest + "\n" + resumeActionID))
	return filepath.Join(workerActionDirectory(filepath.Dir(filepath.Dir(material.Path)), material.ActionID), hex.EncodeToString(call[:]))
}

func workerActionDirectory(stateDir, actionID string) string {
	action := sha256.Sum256([]byte(actionID))
	return filepath.Join(stateDir, "native", hex.EncodeToString(action[:]))
}

func jsonValuesEqual(a, b any) bool {
	aa, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	var av, bv any
	if json.Unmarshal(aa, &av) != nil || json.Unmarshal(bb, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

func readWorkerMaterial(material WorkerMaterial) (worker.Material, error) {
	value, err := readWorkerMaterialFile(material.Path, material.Digest)
	if err != nil {
		return value, err
	}
	if value.Input.Pending.ActionID != material.ActionID || value.Input.Pending.AttemptID != material.AttemptID || value.ReadOnly != material.ReadOnly {
		return value, fmt.Errorf("worker material identity differs from its issued reference")
	}
	return value, nil
}

func readWorkerMaterialFile(path, digest string) (worker.Material, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return worker.Material{}, err
	}
	sum := sha256.Sum256(body)
	if digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return worker.Material{}, fmt.Errorf("worker material proof differs")
	}
	var value worker.Material
	if err := json.Unmarshal(body, &value); err != nil {
		return value, err
	}
	return value, nil
}

// Missing references are classified only through original engine-owned paths.
// Plain project commands do not prove an engine task and remain independent.
func executionInEngineWorkspace(directory string, request map[string]any) bool {
	workspace, _ := request["workspace"].(string)
	if workspace == "" {
		workspace, _ = request["cwd"].(string)
	}
	for _, path := range []string{directory, workspace} {
		path, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		for parent := path; parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
			if filepath.Base(filepath.Dir(parent)) == "engine" && filepath.Base(filepath.Dir(filepath.Dir(parent))) == ".gates" {
				if _, err := os.Stat(filepath.Join(parent, "request.json")); err == nil {
					return true
				}
			}
			if filepath.Base(filepath.Dir(parent)) != "qa-isolation" || filepath.Base(filepath.Dir(filepath.Dir(parent))) != ".gates" {
				continue
			}
			root := filepath.Dir(filepath.Dir(filepath.Dir(parent)))
			ids, err := lifecycle.ActiveRunIDs(root)
			if err != nil {
				continue
			}
			for _, id := range ids {
				f, err := Open(root, id)
				if err != nil {
					continue
				}
				snap, err := f.engine.Load()
				if err != nil {
					continue
				}
				for _, resource := range snap.State.NativeResources {
					if sameExecutionPath(workspace, resource.Path) {
						return true
					}
				}
			}
		}
	}
	return false
}

func (f *Facade) admitCheckExecution(s *protocol.State, actionID, directory string, request map[string]any) error {
	intent, issued := s.PendingHostActions[actionID]
	if !issued || intent.Adapter == nil || intent.Adapter.Operation != "op.check" || !s.NativeActionWritable(intent, f.definition) {
		return fmt.Errorf("check execution has no current signed operation")
	}
	var p protocol.CheckParameters
	if err := decodeNativeParams(intent, &p); err != nil {
		return err
	}
	expected, err := f.checkRequest(s, intent, p)
	if err != nil {
		return err
	}
	if filepath.Clean(directory) != filepath.Join(f.stateDir, "checks", strings.ReplaceAll(actionID, ":", "-")) || !jsonValuesEqual(request, expected) {
		return fmt.Errorf("check execution differs from its original approved operation")
	}
	return nil
}

// WorkerMaterial is a thin pointer to immutable evidence for an issued action.
// Input and completion authority remain in protocol.State, not this file.
type WorkerMaterial struct {
	ActionID  string `json:"actionId"`
	AttemptID string `json:"attemptId"`
	Path      string `json:"path"`
	Digest    string `json:"digest"`
	ReadOnly  bool   `json:"readOnly"`
}

// ReviewVerificationContext exposes original immutable semantic input. Native
// invocation ownership selects its material; typed inputs use the confirmed
// intake receipt in force when their dispatch was issued.
type ReviewVerificationContext struct {
	Material *WorkerMaterial       `json:"material,omitempty"`
	Input    *protocol.WorkerInput `json:"input,omitempty"`
	Intake   *IntakePreview        `json:"intake,omitempty"`
}

func (f *Facade) reviewVerificationContexts(state *protocol.State) (map[string]ReviewVerificationContext, error) {
	contexts := map[string]ReviewVerificationContext{}
	for _, result := range state.PendingReviewVerifications() {
		attempt := state.EndedAttempts[state.SpawnReceipts[result.ActionID].Task]
		invocations, err := claimedWorkerInvocations(f.root, f.runID, result.ActionID, attempt.ID)
		if err != nil {
			return nil, err
		}
		for _, invocation := range invocations {
			ref := invocation.binding.WorkerMaterial
			id := WorkerResultEventID(*ref, invocation.binding.ResumeActionID)
			event := protocol.Event{ID: id, Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: result.ActionID, Provider: result.Provider, Outcome: result.Outcome, PayloadDigest: result.PayloadDigest, SourceResult: result.SourceResult, InterruptionReason: result.InterruptionReason, InvalidReason: result.InvalidReason, FailureClass: result.FailureClass, Review: result.Review, Validation: result.Validation}}
			digest, err := event.Digest()
			if err != nil {
				return nil, err
			}
			recorded := state.Events[string(id)]
			if recorded.Acceptance.ActionID == result.ActionID && recorded.Digest == digest {
				if original := contexts[result.ActionID].Material; original != nil && original.Digest != ref.Digest {
					return nil, fmt.Errorf("original review result is bound to multiple different native input materials")
				}
				contexts[result.ActionID] = ReviewVerificationContext{Material: ref, Input: &invocation.material.Input}
			}
		}
		if contexts[result.ActionID].Material != nil {
			continue
		}
		receipt := state.IntakeReceipt.Confirmation
		for _, entry := range state.RequirementRevisions {
			if entry.StateRevision <= state.DispatchIssueRevisions[result.ActionID] && entry.Intake != nil {
				receipt = *entry.Intake
			}
		}
		originals, err := f.originalWorkerMaterials()
		if err != nil {
			return nil, err
		}
		for ref, material := range originals {
			input := material.Input
			if ref.ActionID != result.ActionID || ref.AttemptID != attempt.ID || input.Review == nil || input.Review.Action != result.Review.Action || input.Review.RequirementRevision != result.Review.RequirementRevision || !reflect.DeepEqual(input.Intake, &receipt) {
				continue
			}
			if prior := contexts[result.ActionID].Material; prior != nil && prior.Digest != ref.Digest {
				return nil, fmt.Errorf("completed typed review has multiple different original material bindings")
			}
			contexts[result.ActionID] = ReviewVerificationContext{Material: &ref, Input: &input}
		}
		if contexts[result.ActionID].Material != nil {
			continue
		}
		if strings.TrimPrefix(receipt.RequirementRevision, "sha256:") != strings.TrimPrefix(result.Review.RequirementRevision, "sha256:") {
			return nil, fmt.Errorf("original review has no matching confirmed intake at its issuance")
		}
		intake, err := f.confirmedIntake(Run{IntakeConfirmationReceipt: &receipt})
		if err != nil {
			return nil, err
		}
		contexts[result.ActionID] = ReviewVerificationContext{Intake: &intake}
	}
	return contexts, nil
}

func (f *Facade) workerMaterials(s *protocol.State, write bool) (map[string]WorkerMaterial, error) {
	refs := map[string]WorkerMaterial{}
	inputs := s.WorkerInputs(f.definition)
	if len(inputs) == 0 {
		return refs, nil
	}
	if !write {
		materials, err := f.originalWorkerMaterials()
		if err != nil {
			return nil, err
		}
		for ref, value := range materials {
			input, current := inputs[ref.ActionID]
			if !current {
				continue
			}
			input.Materials = value.Input.Materials
			input.AvailableRouteCandidates = value.Input.AvailableRouteCandidates
			if jsonValuesEqual(input, value.Input) {
				refs[ref.ActionID] = ref
			}
		}
		return refs, nil
	}
	catalog, err := prompts.LoadPromptCatalog(f.admission.Target)
	if err != nil {
		return nil, err
	}
	for id, input := range inputs {
		ref, err := f.workerMaterial(input, catalog, write)
		if err != nil {
			return nil, err
		}
		refs[id] = ref
	}
	return refs, nil
}

func (f *Facade) workerMaterial(input protocol.WorkerInput, catalog prompts.PromptCatalog, write bool) (WorkerMaterial, error) {
	if input.Intake != nil {
		for _, artifact := range input.Intake.Artifacts {
			var data []byte
			if artifact.Path == input.RequirementDocument.Path {
				data = []byte(input.RequirementDocument.Content)
			} else {
				var err error
				data, err = os.ReadFile(filepath.Join(f.root, filepath.FromSlash(artifact.Path)))
				if err != nil {
					return WorkerMaterial{}, err
				}
			}
			if requirementContentRevision(data) != artifact.Revision {
				return WorkerMaterial{}, fmt.Errorf("confirmed material %s differs from its original revision", artifact.Path)
			}
			input.Materials = append(input.Materials, protocol.IntakeMaterial{Path: artifact.Path, Content: strings.ReplaceAll(string(data), "\r\n", "\n"), Revision: artifact.Revision})
		}
	}
	material, err := worker.Render(input, catalog)
	if err != nil {
		return WorkerMaterial{}, err
	}
	body, err := json.MarshalIndent(material, "", "  ")
	if err != nil {
		return WorkerMaterial{}, err
	}
	body = append(body, '\n')
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	// Content identity allows carried work to read current confirmed context
	// without overwriting the input bytes of an already executing invocation.
	path := filepath.Join(f.stateDir, "materials", digest+".json")
	ref := WorkerMaterial{ActionID: input.Pending.ActionID, AttemptID: input.Pending.AttemptID, Path: path, Digest: "sha256:" + digest, ReadOnly: material.ReadOnly}
	if !write {
		return ref, nil
	}
	existing, err := os.ReadFile(path)
	if err == nil {
		if !bytes.Equal(existing, body) {
			return WorkerMaterial{}, fmt.Errorf("worker material bytes differ from their content identity")
		}
		return ref, nil
	}
	if !os.IsNotExist(err) {
		return WorkerMaterial{}, err
	}
	if err := f.admission.write(func() error {
		return writeFileAtomic(path, body, 0600)
	}); err != nil {
		return WorkerMaterial{}, err
	}
	return ref, nil
}

// registerIntakeRequirement uses the same typed requirement admission as
// externally observed confirmations. It never confirms different file bytes.
func (f *Facade) registerIntakeRequirement() error {
	snapshot, err := f.engine.Load()
	if err != nil {
		return err
	}
	s := snapshot.State
	if s.Route != "regular" || s.RequirementGuarantee == nil || s.Validation != nil || s.IntakeConfirmationReceipt == nil || s.RequirementBarrierActive() || s.ResetPending() {
		return nil
	}
	if _, aborted := s.AbortDecision(); aborted || s.Phase == runtime.PhaseTerminal || s.Phase == runtime.PhasePreparing {
		return nil
	}
	receipt := s.CurrentIntakeConfirmation()
	revision := s.RequirementRevision()
	path := filepath.Join(f.root, receipt.RequirementSource)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	event := protocol.Event{ID: protocol.EventID("intake.requirement:" + revision), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: string(data)}}}
	_, err = f.engine.Submit(event, intakeObservation(f.root, []IntakeMaterial{{Path: receipt.RequirementSource, Revision: requirementContentRevision(data)}}))
	return err
}

// SubmitWorkerResult consumes the source-verified native answer and uses the
// existing Submit transaction. The original spawn correlation owns session
// correlation; native source verifies the separate host session identity.
func (f *Facade) SubmitWorkerResult(eventID protocol.EventID, material WorkerMaterial, directory string, source map[string]any, verifyNative func(string, string, map[string]any) (worker.ResultSource, error)) (protocol.Acceptance, Run, error) {
	if verifyNative == nil {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("native source verification is required")
	}
	invocation, err := readNativeWorkerInvocation(f.root, f.runID, directory)
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	if *invocation.binding.WorkerMaterial != material {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("native result differs from its original material reference")
	}
	value := invocation.material
	provider := invocation.request["provider"].(string)
	evidence, err := verifyNative(provider, directory, source)
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	if evidence.Identity == "" {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("native result session proof is missing")
	}
	var paths []string
	if !value.ReadOnly {
		facts, err := json.Marshal(source["effects"])
		if err != nil {
			return protocol.Acceptance{}, Run{}, err
		}
		var captured struct {
			Paths []string `json:"paths"`
		}
		if err := json.Unmarshal(facts, &captured); err != nil {
			return protocol.Acceptance{}, Run{}, err
		}
		paths = captured.Paths
	}
	event, err := worker.DecodeResultWithFacts(value.Input, eventID, evidence.Answer, evidence.Digest, paths, evidence.LocationFacts)
	if err != nil {
		event, err = worker.InvalidResult(value.Input, eventID, evidence.Digest, err.Error())
		if err != nil {
			return protocol.Acceptance{}, Run{}, err
		}
	}
	// The verified original material retains the run/action/attempt binding.
	// Submit alone owns current, terminal, duplicate and obsolete-result admission.
	// The native session proof is distinct from the ledger's spawn correlation.
	return f.Submit(event)
}

func (f *Facade) routeRequestPayload(child bool) (*protocol.RequestPayload, error) {
	catalog, err := prompts.LoadPromptCatalog(f.admission.Target)
	if err != nil {
		return nil, err
	}
	candidates := catalog.RouteCandidates()
	if child {
		candidates = slices.DeleteFunc(candidates, func(id string) bool { return id == "blackbox" || id == "whitebox" })
	}
	return &protocol.RequestPayload{Control: protocol.ControlSelectRoute, RouteGates: candidates, Options: []protocol.AskOption{{ID: "approve", Label: "Confirm the selected gates and development plan"}}}, nil
}

func (f *Facade) originalWorkerMaterials() (map[WorkerMaterial]worker.Material, error) {
	materials := map[WorkerMaterial]worker.Material{}
	entries, err := os.ReadDir(filepath.Join(f.stateDir, "materials"))
	if os.IsNotExist(err) {
		return materials, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(f.stateDir, "materials", entry.Name())
		digest := "sha256:" + strings.TrimSuffix(entry.Name(), ".json")
		value, err := readWorkerMaterialFile(path, digest)
		if err != nil {
			return nil, err
		}
		ref := WorkerMaterial{ActionID: value.Input.Pending.ActionID, AttemptID: value.Input.Pending.AttemptID, Path: path, Digest: digest, ReadOnly: value.ReadOnly}
		materials[ref] = value
	}
	return materials, nil
}
