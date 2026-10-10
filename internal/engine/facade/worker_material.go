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

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

// NativeStartVerifier is installed by the native command assembly. It retains
// the provider's acknowledgement validation without workflow state.
var NativeStartVerifier func(string, string) (string, error)

// workerExecutionBridge reads jobs claimed under AdmitExecution's native
// window. Prepared requests without an owner cannot execute after retirement:
// their original admission still requires the current worker input.
func workerExecutionBridge(root, runID string) func(protocol.PendingAction, protocol.Attempt) (*protocol.SpawnReceiptPayload, error) {
	stateDir := filepath.Join(root, EngineNamespace, runID)
	return func(pending protocol.PendingAction, attempt protocol.Attempt) (*protocol.SpawnReceiptPayload, error) {
		entries, err := os.ReadDir(workerActionDirectory(stateDir, pending.ActionID))
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			directory := filepath.Join(workerActionDirectory(stateDir, pending.ActionID), entry.Name())
			if _, err := os.Stat(filepath.Join(directory, "owner")); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, err
			}
			body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
			if err != nil {
				return nil, err
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
			if err := json.Unmarshal(body, &request); err != nil {
				return nil, err
			}
			ref := request.Engine.WorkerMaterial
			material, err := readWorkerMaterial(ref)
			if err != nil {
				return nil, err
			}
			provider := material.Input.Provider
			if descriptor, lookupErr := host.Lookup(provider); lookupErr == nil {
				provider = descriptor.InstallName
			}
			if !sameExecutionPath(request.Engine.Root, root) || request.Engine.RunID != runID || ref.ActionID != pending.ActionID || ref.AttemptID != attempt.ID || material.Input.RunID != runID || request.Provider != provider || !sameExecutionPath(filepath.Dir(filepath.Dir(ref.Path)), stateDir) || !sameExecutionPath(directory, WorkerExecutionDirectory(ref, request.Engine.ResumeActionID)) {
				return nil, fmt.Errorf("native worker owner differs from its original run/action/attempt/material")
			}
			fact := &protocol.SpawnReceiptPayload{ActionID: pending.ActionID, Provider: material.Input.Provider, Correlation: directory, Status: protocol.SpawnStatusUnknown}
			if _, err := os.Stat(filepath.Join(directory, "identity.json")); err == nil && NativeStartVerifier != nil {
				identity, err := NativeStartVerifier(provider, directory)
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
	var binding struct {
		Root           string          `json:"root"`
		RunID          string          `json:"runId"`
		WorkerMaterial *WorkerMaterial `json:"workerMaterial"`
		ActionID       string          `json:"actionId"`
		ResumeActionID string          `json:"resumeActionId"`
	}
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
	identity, err := snap.State.WorkerExecutionIdentity(material.ActionID, material.AttemptID, binding.ResumeActionID, f.definition)
	if err != nil {
		return release, err
	}
	refs, err := f.workerMaterials(snap.State, false)
	if err != nil {
		return release, err
	}
	if expected, known := refs[material.ActionID]; !known || expected != material {
		return release, fmt.Errorf("worker execution material differs from current issuance")
	}
	value, err := readWorkerMaterial(material)
	if err != nil {
		return release, err
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
	known := map[string]any{"provider": provider, "prompt": value.Prompt, "schema": value.ResultSchema, "read_only": value.ReadOnly, "workspace": workspace}
	for key, expected := range known {
		if actual, supplied := request[key]; supplied && !jsonValuesEqual(actual, expected) {
			return release, fmt.Errorf("worker execution %s differs from issued material", key)
		}
		request[key] = expected
	}
	for key := range request {
		switch key {
		case "engine", "provider", "prompt", "schema", "read_only", "workspace", "identity", "binding", "model", "reasoning_effort", "permission_mode", "native_store", "probe":
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
func (f *Facade) WorkerExecutionRequest(material WorkerMaterial, resumeActionID string) (map[string]any, map[string]any, string, error) {
	directory := WorkerExecutionDirectory(material, resumeActionID)
	request := map[string]any{"engine": map[string]any{"root": f.root, "runId": f.runID, "workerMaterial": material, "resumeActionId": resumeActionID}}
	release, err := AdmitExecution(directory, request)
	if err != nil {
		return nil, nil, "", err
	}
	release()
	return request, f.workerSupervisorRequest(directory, request["workspace"].(string), request["engine"]), directory, nil
}

func (f *Facade) workerSupervisorRequest(directory, workspace string, ref any) map[string]any {
	return map[string]any{"kind": "native", "argv": []string{filepath.Join(f.admission.Target, "bin", "formal-gates"), "harness", "native", directory}, "cwd": workspace, "stdin": "", "environment": map[string]any{}, "engine": ref}
}

// ObserveWorkerSource uses the original provider verifier before exposing native
// completion to the existing lifecycle/cost source owner.
func (f *Facade) ObserveWorkerSource(material WorkerMaterial, directory string, source map[string]any, verifyNative func(string, string, map[string]any) ([]byte, string, string, error)) error {
	value, err := readWorkerMaterial(material)
	if err != nil {
		return err
	}
	provider := value.Input.Provider
	if descriptor, lookupErr := host.Lookup(provider); lookupErr == nil {
		provider = descriptor.InstallName
	}
	_, _, identity, err := verifyNative(provider, directory, source)
	if err != nil {
		return err
	}
	return f.ObserveWorkerTerminal(material, identity)
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

// ObserveWorkerStart records only the native adapter's verified live identity;
// completed source and semantic result admission remain separate.
func (f *Facade) ObserveWorkerStart(material WorkerMaterial, directory string, verifyStart func(string, string) (string, error)) error {
	value, err := readWorkerMaterial(material)
	if err != nil {
		return err
	}
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
	if _, _, err := f.Submit(event); err != nil {
		return err
	}
	start, err := protocol.NewLifecycleEventEvent(protocol.EventID("native-start:"+material.Digest), value.Input.Provider, identity, protocol.LifecycleStart)
	if err != nil {
		return err
	}
	_, _, err = f.Submit(start)
	if err != nil {
		return err
	}
	return f.admission.write(func() error {
		return lifecycle.ObserveNativeStart(f.root, f.runID, material.ActionID, value.Input.Provider, identity)
	})
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
		catalog, loadErr := prompts.LoadPromptCatalog(f.admission.Target)
		if loadErr != nil {
			return false, loadErr
		}
		candidates := catalog.RouteCandidates()
		if snap.State.SliceChild != nil {
			candidates = slices.DeleteFunc(candidates, func(id string) bool { return id == "blackbox" || id == "whitebox" })
		}
		event, err = protocol.NewRouteRequestEvent(id, candidates, protocol.AskOption{ID: "full", Label: "Run all proposed gates"}, protocol.AskOption{ID: "custom", Label: "Choose the proposed gates"})
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
	if current, known := snap.State.ExecutableHostActions()[intent.ActionID]; !known || !reflect.DeepEqual(current, intent) {
		return "", nil, release, fmt.Errorf("native agent effect has no current signed intent")
	}
	target := intent.Terminate
	if target == nil {
		target = intent.Resume
	}
	if target == nil {
		return "", nil, release, fmt.Errorf("native agent effect has no original target")
	}
	action := sha256.Sum256([]byte(target.WorkerActionID))
	root := filepath.Join(f.stateDir, "native", hex.EncodeToString(action[:]))
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", nil, release, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, filepath.Join(root, entry.Name()))
		}
	}
	return snap.State.RunProvider, directories, release, nil
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
	body, err := os.ReadFile(material.Path)
	if err != nil {
		return worker.Material{}, err
	}
	sum := sha256.Sum256(body)
	if material.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return worker.Material{}, fmt.Errorf("worker material proof differs")
	}
	var value worker.Material
	if err := json.Unmarshal(body, &value); err != nil {
		return value, err
	}
	if value.Input.Pending.ActionID != material.ActionID || value.Input.Pending.AttemptID != material.AttemptID || value.ReadOnly != material.ReadOnly {
		return value, fmt.Errorf("worker material identity differs from its issued reference")
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

func (f *Facade) workerMaterials(s *protocol.State, write bool) (map[string]WorkerMaterial, error) {
	refs := map[string]WorkerMaterial{}
	inputs := s.WorkerInputs(f.definition)
	if len(inputs) == 0 {
		return refs, nil
	}
	catalog, err := prompts.LoadPromptCatalog(f.admission.Target)
	if err != nil {
		return nil, err
	}
	for id, input := range inputs {
		material, err := worker.Render(input, catalog)
		if err != nil {
			return nil, err
		}
		body, err := json.MarshalIndent(material, "", "  ")
		if err != nil {
			return nil, err
		}
		body = append(body, '\n')
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		// Content identity allows carried work to read current confirmed context
		// without overwriting the input bytes of an already executing invocation.
		path := filepath.Join(f.stateDir, "materials", digest+".json")
		refs[id] = WorkerMaterial{ActionID: id, AttemptID: input.Pending.AttemptID, Path: path, Digest: "sha256:" + digest, ReadOnly: material.ReadOnly}
		if !write {
			continue
		}
		existing, err := os.ReadFile(path)
		if err == nil {
			if !bytes.Equal(existing, body) {
				return nil, fmt.Errorf("worker material bytes differ from their content identity")
			}
			continue
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := f.admission.write(func() error {
			return writeFileAtomic(path, body, 0600)
		}); err != nil {
			return nil, err
		}
	}
	return refs, nil
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
	receipt := s.IntakeConfirmationReceipt
	revision := s.RequirementRevision()
	data, err := os.ReadFile(filepath.Join(f.root, receipt.RequirementSource))
	if err != nil {
		return err
	}
	event := protocol.Event{ID: protocol.EventID("intake.requirement:" + revision), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: string(data)}}}
	_, err = f.engine.Submit(event, emptyFingerprint())
	return err
}

// SubmitWorkerResult consumes the source-verified native answer and uses the
// existing Submit transaction. The original spawn correlation owns session
// correlation; native source verifies the separate host session identity.
func (f *Facade) SubmitWorkerResult(eventID protocol.EventID, material WorkerMaterial, directory string, source map[string]any, verifyNative func(string, string, map[string]any) (worker.ResultSource, error)) (protocol.Acceptance, Run, error) {
	if verifyNative == nil {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("native source verification is required")
	}
	body, err := os.ReadFile(material.Path)
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	sum := sha256.Sum256(body)
	if material.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("worker material proof differs")
	}
	var value worker.Material
	if err := json.Unmarshal(body, &value); err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	if value.Input.Pending.ActionID != material.ActionID || value.Input.Pending.AttemptID != material.AttemptID || value.Input.RunID != f.runID {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("worker material identity differs from its issued reference or run")
	}
	provider := value.Input.Provider
	if descriptor, lookupErr := host.Lookup(provider); lookupErr == nil {
		provider = descriptor.InstallName
	}
	requestBytes, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	var request struct {
		Prompt    string `json:"prompt"`
		Workspace string `json:"workspace"`
		ReadOnly  bool   `json:"read_only"`
	}
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	workspace := f.root
	if value.Input.Validation != nil && value.Input.Validation.Workspace != nil {
		workspace = value.Input.Validation.Workspace.Path
	}
	if request.Prompt != value.Prompt || filepath.Clean(request.Workspace) != filepath.Clean(workspace) || request.ReadOnly != value.ReadOnly {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("native invocation differs from issued worker material")
	}
	evidence, err := verifyNative(provider, directory, source)
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	if evidence.Identity == "" {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("native result session proof is missing")
	}
	var paths []string
	if !request.ReadOnly {
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
