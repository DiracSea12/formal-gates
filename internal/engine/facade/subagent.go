package facade

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/protocol"
)

// SubagentDispatch is a transport handoff for an already claimed engine action.
// It is not a second scheduler. The desktop's native tools own the actual spawn
// and send_input calls; SOURCE and result admission still belong to the engine.
type SubagentDispatch struct {
	ActionID   string           `json:"actionId"`
	Parent     string           `json:"parentThreadId"`
	Identity   string           `json:"identity,omitempty"`
	Operation  string           `json:"operation"`
	Workspace  string           `json:"workspace"`
	Prompt     string           `json:"prompt"`
	SchemaPath string           `json:"schemaPath"`
	Marker     string           `json:"marker"`
	Sources    []SubagentSource `json:"sources,omitempty"`
}

func (f *Facade) subagentPath(action string) string {
	return filepath.Join(f.codexJob(action), "subagent.json")
}

// PrepareSubagent consumes the ordinary CLAIM before exposing a native dispatch.
// Reopening a claimed handoff only allows observation: a lost spawn response is
// not authorization to create another child or replay a turn.
func (f *Facade) PrepareSubagent(action, parent string, resume bool, sourceFile ...string) (Run, error) {
	if len(sourceFile) > 1 {
		return Run{}, fmt.Errorf("SUBAGENT_SOURCES: expected one source manifest")
	}
	if parent == "" {
		return Run{}, fmt.Errorf("SUBAGENT_PARENT_REQUIRED: use the current native CODEX_THREAD_ID")
	}
	r, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	x, ok := r.Executions[action]
	if !ok {
		return Run{}, fmt.Errorf("UNKNOWN_ACTION: %s", action)
	}
	if x.Claim.Provider != "codex" {
		return Run{}, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: native subagent transport requires Codex")
	}
	if x.Claimed {
		// Sources are fixed by the first claim, just like the native identity.
		d, err := f.readSubagentDispatch(action)
		if err != nil {
			return Run{}, fmt.Errorf("UNKNOWN: claimed action has no subagent handoff; reconcile its original transport: %w", err)
		}
		if d.Parent != parent {
			return Run{}, fmt.Errorf("INTENT_MISMATCH: subagent parent changed")
		}
		if len(sourceFile) == 1 && sourceFile[0] != "" {
			sources, err := readSubagentSources(sourceFile[0], string(x.Claim.Task.Step))
			if err != nil {
				return Run{}, err
			}
			if !reflect.DeepEqual(sources, d.Sources) {
				return Run{}, fmt.Errorf("INTENT_MISMATCH: claimed subagent sources are immutable; observe without new sources")
			}
		}
		d.Operation = "observe"
		d.Identity = x.Identity
		r.Subagent = &d
		return r, nil
	}
	if x.Claim.Operation == "resume" {
		if !resume {
			return Run{}, fmt.Errorf("DEVELOPMENT_CONTINUITY: use --resume for this same native identity")
		}
		old, err := f.readSubagentDispatch(x.PreviousAction)
		if err != nil || old.Parent != parent {
			return Run{}, fmt.Errorf("DEVELOPMENT_CONTINUITY: cannot switch an existing developer into a different transport or parent")
		}
		if _, err = f.observeSubagent(old, x.Identity, true); err != nil {
			return Run{}, err
		}
	} else if resume {
		return Run{}, fmt.Errorf("INTENT_MISMATCH: no issued native continuation")
	}
	if len(sourceFile) == 1 && sourceFile[0] != "" {
		f.subagentSources, err = readSubagentSources(sourceFile[0], string(x.Claim.Task.Step))
		if err != nil {
			return Run{}, err
		}
	}
	f.subagentParent = parent
	defer func() { f.subagentParent = ""; f.subagentSources = nil }()
	return f.ExecuteNative(action, resume)
}

func (f *Facade) prepareSubagentDispatch(run Run, claim protocol.ExecutionClaim, job, workspace, prompt string, schema map[string]any) (Run, error) {
	d := SubagentDispatch{ActionID: claim.ActionID, Parent: f.subagentParent, Identity: claim.Identity, Operation: claim.Operation, Workspace: workspace, SchemaPath: filepath.Join(job, "result-schema.json")}
	d.Sources = f.subagentSources
	d.Marker = "formal-gates native action: " + run.RunID + " / " + claim.ActionID + "\nEND_ACTION_BINDING"
	contract, err := json.Marshal(schema)
	if err != nil {
		return Run{}, err
	}
	// No ancestor history: the generated role prompt remains the sole task input.
	// The native tool API inherits the parent's permissions. These are role
	// instructions, not a claim that the API supplies a per-child tool sandbox.
	transport := fmt.Sprintf(`%s
You are the independent native worker for this engine-issued action. Do not delegate or invoke workflow scheduling/decision commands. Do not inspect another role's workspace. Use only the supplied role input and assigned workspace %q; set every shell command's workdir explicitly. Product-review and start-readiness roles must use only their supplied text, without shell, web, connectors, or other project reads. Other roles retain the workspace and tool access authorized by their supplied role instructions, including QA access to its isolated workspace; blackbox must not read development code or development evidence. Return one JSON object matching the supplied schema as your FINAL message; no markdown fences or extra commentary in the final. The engine collects it from your native transcript, not from a parent-written result file.
For roles authorized to use formal_gates_operation, the desktop equivalent is the existing CLI task-operation boundary: write {"requestId":"stable-id","kind":"model.run|qa.prepare|qa.target|qa.collect|whitebox.coverage","caseId":"","prompt":""} to a request file, then run formal-gates workflow drive --root %q --run-id %q --action-id %q --subagent-operation REQUEST_FILE. Reuse requestId when reconciling an interrupted operation and add --resume; never repeat a target using a new id. Before the first operation, bind this action using the same root/run/action arguments plus --subagent-bind "$CODEX_THREAD_ID". Ordinary authoring shell is allowed only when the role permits it. QA targets and nested models must use this CLI boundary. Never use free shell to run a frozen QA target or another model.
Schema: %s

`, d.Marker, workspace, f.root, f.runID, claim.ActionID, contract)
	if claim.Task.Step == "whitebox-design" && !protocol.WhiteboxCoverageReady(run.Executions[claim.ActionID]) {
		// Preserve coverage-before-tests. The operation validates the actual
		// production binding before the design result can be admitted.
		transport += "Before inspecting ANY existing test, independently read requirements and production source only; submit whitebox.coverage with prompt containing JSON {coverageJudgment,behaviorOracles:[{behavior,oracle}]}. Only after its accepted receipt may you inspect/run tests or author missing coverage.\n"
	}
	d.Prompt = transport + prompt
	if len(d.Sources) != 0 {
		// Keep the full captured inputs available without repeating long documents
		// in every native handoff message.
		var entries []map[string]string
		for i, source := range d.Sources {
			path := filepath.Join(job, fmt.Sprintf("subagent-source-%d.txt", i))
			if err := os.WriteFile(path, []byte(source.Content), 0600); err != nil {
				return Run{}, err
			}
			entries = append(entries, map[string]string{"kind": source.Kind, "path": source.Path, "digest": source.Digest, "fullTextPath": path})
		}
		data, err := json.Marshal(entries)
		if err != nil {
			return Run{}, err
		}
		d.Prompt += "\nSupplementalSources: " + string(data)
	}
	if err = writeJSONAtomic(f.subagentPath(claim.ActionID), d); err != nil {
		return Run{}, err
	}
	r, err := f.Show()
	r.Subagent = &d
	return r, err
}

func (f *Facade) readSubagentDispatch(action string) (SubagentDispatch, error) {
	var d SubagentDispatch
	b, err := os.ReadFile(f.subagentPath(action))
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal(b, &d); err != nil {
		return d, err
	}
	if d.ActionID != action || d.Parent == "" || d.Marker == "" {
		return d, fmt.Errorf("INTENT_MISMATCH: subagent handoff binding differs")
	}
	return d, nil
}

type subagentObservation struct {
	Transcript, Answer string
	Interrupted        bool
}

// observeSubagent verifies native parentage AND this action's actual user input.
// A task_complete record is mandatory; a final-looking partial message is not
// completion, and an older turn's PASS cannot satisfy a new continuation.
func (f *Facade) observeSubagent(d SubagentDispatch, identity string, completed bool) (subagentObservation, error) {
	r, err := f.Show()
	if err != nil {
		return subagentObservation{}, err
	}
	x, ok := r.Executions[d.ActionID]
	if !ok || !x.Claimed {
		return subagentObservation{}, fmt.Errorf("UNKNOWN_ACTION: subagent action is not claimed")
	}
	if identity == "" || (x.Identity != "" && x.Identity != identity) || (d.Identity != "" && d.Identity != identity) {
		return subagentObservation{}, fmt.Errorf("INTENT_MISMATCH: native identity differs")
	}
	p, err := findBoundTranscript(identity, x.Claim.Context)
	if err != nil {
		return subagentObservation{}, err
	}
	file, err := os.Open(p)
	if err != nil {
		return subagentObservation{}, err
	}
	defer file.Close()
	return readSubagentTranscript(file, p, identity, d.Parent, d.Marker, completed)
}

func readSubagentTranscript(file *os.File, path, identity, parent, marker string, completed bool) (subagentObservation, error) {
	observation := subagentObservation{Transcript: path}
	s := bufio.NewScanner(file)
	s.Buffer(make([]byte, 65536), 32*1024*1024)
	validParent, inputSeen, turnInput, done := false, false, false, false
	for s.Scan() {
		var e struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(s.Bytes(), &e) != nil {
			continue
		} // writer may still be appending
		if e.Type == "session_meta" {
			var m struct {
				ID     string `json:"id"`
				Parent string `json:"parent_thread_id"`
				Source struct {
					Subagent struct {
						Spawn struct {
							Parent string `json:"parent_thread_id"`
						} `json:"thread_spawn"`
					} `json:"subagent"`
				} `json:"source"`
			}
			if json.Unmarshal(e.Payload, &m) == nil {
				validParent = m.ID == identity && m.Source.Subagent.Spawn.Parent == parent && (m.Parent == "" || m.Parent == parent)
			}
		}
		if e.Type == "response_item" {
			var m struct {
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if json.Unmarshal(e.Payload, &m) == nil && m.Role == "user" {
				for _, c := range m.Content {
					if strings.Contains(c.Text, marker) {
						inputSeen, turnInput = true, true
					}
				}
			}
		}
		if e.Type == "event_msg" {
			var m struct {
				Type   string `json:"type"`
				Answer string `json:"last_agent_message"`
			}
			if json.Unmarshal(e.Payload, &m) != nil {
				continue
			}
			if m.Type == "task_started" {
				done, turnInput, observation.Answer = false, false, ""
				observation.Interrupted = false
			}
			if m.Type == "turn_aborted" && turnInput {
				done, observation.Interrupted = true, true
			}
			if m.Type == "task_complete" && turnInput {
				done, observation.Answer = true, m.Answer
			}
		}
	}
	if err := s.Err(); err != nil {
		return observation, err
	}
	if !validParent || !inputSeen {
		return observation, fmt.Errorf("INTENT_MISMATCH: native transcript does not prove this parent and action input")
	}
	if completed && !done {
		return observation, fmt.Errorf("WAIT: this subagent turn has no native completion; do not respawn or reconstruct its result")
	}
	return observation, nil
}

func (f *Facade) BindSubagent(action, identity string) (Run, error) {
	unlock, err := coordination.AcquirePath(f.codexJob(action)+".lock", "native action")
	if err != nil {
		return Run{}, err
	}
	defer unlock()
	d, err := f.readSubagentDispatch(action)
	if err != nil {
		return Run{}, err
	}
	if _, err = f.observeSubagent(d, identity, false); err != nil {
		return Run{}, err
	}
	r, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	x := r.Executions[action]
	if x.Finished || (x.Identity == identity && x.Source != nil) {
		return r, nil
	}
	if err = f.submitSource(protocol.ExecutionSource{ActionID: action, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: identity, Status: "RUNNING"}); err != nil {
		return Run{}, err
	}
	return f.Show()
}

func (f *Facade) CollectSubagent(action string) (Run, error) {
	unlock, err := coordination.AcquirePath(f.codexJob(action)+".lock", "native action")
	if err != nil {
		return Run{}, err
	}
	defer unlock()
	d, err := f.readSubagentDispatch(action)
	if err != nil {
		return Run{}, err
	}
	r, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	x := r.Executions[action]
	o, err := f.observeSubagent(d, x.Identity, true)
	if err != nil {
		return Run{}, err
	}
	if x.Finished && x.Source != nil && x.Source.Status == "STOPPED" {
		return r, nil
	}
	job := f.codexJob(action)
	if err = os.WriteFile(filepath.Join(job, "answer.json"), []byte(o.Answer), 0600); err != nil {
		return Run{}, err
	}
	var nativeErr error
	if o.Interrupted {
		nativeErr = fmt.Errorf("native subagent turn_aborted observed in %s", o.Transcript)
	}
	return f.finishCodex(action, x.Claim, x.Identity, job, nativeErr)
}

// ExecuteSubagentOperation fills the binding from authoritative state, not from
// worker-supplied candidate/attempt fields. The existing operation path owns
// allowed kinds, frozen argv, CAS, checkpoints and receipts.
func (f *Facade) ExecuteSubagentOperation(action, identity, requestID, kind, caseID, prompt string, resume bool) (protocol.TaskOperationReceipt, error) {
	d, err := f.readSubagentDispatch(action)
	if err != nil {
		return protocol.TaskOperationReceipt{}, err
	}
	if _, err = f.observeSubagent(d, identity, false); err != nil {
		return protocol.TaskOperationReceipt{}, err
	}
	r, err := f.Show()
	if err != nil {
		return protocol.TaskOperationReceipt{}, err
	}
	x := r.Executions[action]
	if x.Identity != identity {
		return protocol.TaskOperationReceipt{}, fmt.Errorf("INTENT_MISMATCH: bind the native subagent before operations")
	}
	bound := x.Claim
	bound.Identity = identity
	q := protocol.TaskOperationRequest{RunID: r.RunID, ExpectedRevision: r.Revision, Binding: bound, RequestID: requestID, Kind: kind, CaseID: caseID, Prompt: prompt}
	if resume {
		for _, old := range x.Operations {
			if old.Request.RequestID == requestID && old.Request.Kind == kind && old.Request.CaseID == caseID && old.Request.Prompt == prompt {
				q = old.Request
				break
			}
		}
	}
	return f.ExecuteTaskOperation(q, resume)
}
