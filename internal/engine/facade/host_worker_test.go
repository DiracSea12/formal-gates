package facade

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/harness"
	"formal-gates/internal/settings"
)

func writeHostWorkerRollout(t *testing.T, home, session, parent, turn, model, effort string) string {
	t.Helper()
	path := filepath.Join(home, "sessions", "2026", "10", "22", "rollout-local-"+session+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	records := []map[string]any{
		{"type": "session_meta", "payload": map[string]any{"id": session, "source": map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": parent, "agent_path": "/root/original-worker"}}}}},
		{"type": "event_msg", "payload": map[string]any{"type": "task_started", "turn_id": turn}},
		{"type": "turn_context", "payload": map[string]any{"turn_id": turn, "model": model, "effort": effort}},
		{"type": "token_usage_record", "payload": map[string]any{"thread_id": session, "turn_id": turn, "response_id": "response-" + session, "usage": map[string]any{"input_tokens": 13, "cached_input_tokens": 5, "output_tokens": 3}, "thread_token_usage": map[string]any{"input_tokens": 13, "cached_input_tokens": 5, "output_tokens": 3}}},
	}
	var data bytes.Buffer
	for _, record := range records {
		body, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		data.Write(body)
		data.WriteByte('\n')
	}
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHostWorkerClaimCompetitionRetainsWinningOriginalSource(t *testing.T) {
	const parent = "11111111-1111-4111-8111-111111111111"
	f, root, material := hostWorkerFacade(t, parent)
	nativeHome := t.TempDir()
	hostWorkerClaimCompetition(t, f, root, material, nativeHome, parent)
}

func hostWorkerFacade(t *testing.T, parent string) (*Facade, string, WorkerMaterial) {
	t.Helper()
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("USERPROFILE", isolatedHome)
	f, root := workerMaterialHostFacade(t, "codex", OwnerBinding{Session: parent})
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(root, home, "project", run.Configuration.Name, nil, func(c *settings.Configuration) error {
		c.Execution = map[string]settings.ExecutionChoice{"product-review": {Provider: "codex", Model: "worker-model", ReasoningEffort: "high"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	var material WorkerMaterial
	for _, value := range run.WorkerMaterials {
		material = value
	}
	if material.Path == "" {
		t.Fatal("no original product worker")
	}
	return f, root, material
}

func hostWorkerClaimCompetition(t *testing.T, f *Facade, root string, material WorkerMaterial, nativeHome, parent string) {
	t.Helper()
	for _, test := range []struct{ name, session, parent, model, effort string }{
		{"different-parent", "66666666-6666-4666-8666-666666666666", "99999999-9999-4999-8999-999999999999", "worker-model", "high"},
		{"different-model", "77777777-7777-4777-8777-777777777777", parent, "other-model", "high"},
		{"different-effort", "88888888-8888-4888-8888-888888888888", parent, "worker-model", "low"},
	} {
		t.Run(test.name, func(t *testing.T) {
			writeHostWorkerRollout(t, nativeHome, test.session, test.parent, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", test.model, test.effort)
			if _, err := f.ClaimHostWorker(material, "", test.session, nativeHome); err == nil {
				t.Fatal("unrelated native facts claimed original work")
			}
			if _, err := os.Stat(filepath.Join(WorkerExecutionDirectory(material, ""), "native-request.json")); !os.IsNotExist(err) {
				t.Fatal("rejected child persisted an invocation", err)
			}
		})
	}
	oldChild := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	oldTurn := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	oldPath := writeHostWorkerRollout(t, nativeHome, oldChild, parent, oldTurn, "worker-model", "high")
	oldData, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	newline := bytes.IndexByte(oldData, '\n') + 1
	prefix, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": parent, "source": "vscode"}})
	withHistory := append(append(append([]byte{}, oldData[:newline]...), append(prefix, '\n')...), oldData[newline:]...)
	if err := os.WriteFile(oldPath, withHistory, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ClaimHostWorker(material, "", oldChild, nativeHome); err == nil {
		t.Fatal("forked parent history became a fresh worker")
	}
	oldPath = writeHostWorkerRollout(t, nativeHome, oldChild, parent, oldTurn, "worker-model", "high")
	appendHostWorkerRecord(t, oldPath, "event_msg", map[string]any{"type": "task_complete", "turn_id": oldTurn})
	appendHostWorkerRecord(t, oldPath, "event_msg", map[string]any{"type": "task_started", "turn_id": "dddddddd-dddd-4ddd-8ddd-dddddddddddd"})
	appendHostWorkerRecord(t, oldPath, "turn_context", map[string]any{"turn_id": "dddddddd-dddd-4ddd-8ddd-dddddddddddd", "model": "worker-model", "effort": "high"})
	if _, err := f.ClaimHostWorker(material, "", oldChild, nativeHome); err == nil {
		t.Fatal("previous unrelated local work became a fresh worker")
	}
	children := []string{"22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"}
	turns := []string{"44444444-4444-4444-8444-444444444444", "55555555-5555-4555-8555-555555555555"}
	paths := make([]string, 2)
	for i := range children {
		paths[i] = writeHostWorkerRollout(t, nativeHome, children[i], parent, turns[i], "worker-model", "high")
	}
	start := make(chan struct{})
	type claim struct {
		session, directory string
		err                error
	}
	results := make(chan claim, 2)
	var group sync.WaitGroup
	for _, session := range children {
		group.Add(1)
		go func(session string) {
			defer group.Done()
			<-start
			directory, err := f.ClaimHostWorker(material, "", session, nativeHome)
			results <- claim{session, directory, err}
		}(session)
	}
	close(start)
	group.Wait()
	close(results)
	var winner claim
	failed := 0
	for result := range results {
		if result.err == nil {
			if winner.session != "" {
				t.Fatal("two children claimed one original action")
			}
			winner = result
		} else {
			failed++
		}
	}
	if winner.session == "" || failed != 1 {
		t.Fatalf("claim competition did not preserve one execution: winner=%+v failed=%d", winner, failed)
	}
	original, err := os.ReadFile(filepath.Join(winner.directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range children {
		if session != winner.session {
			if _, err := f.ClaimHostWorker(material, "", session, nativeHome); err == nil {
				t.Fatal("different child replaced original winner")
			}
		}
	}
	after, err := os.ReadFile(filepath.Join(winner.directory, "native-request.json"))
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("loser changed winning original request", err)
	}
	var pointer struct {
		Job string `json:"job"`
	}
	pointerPath := filepath.Join(root, ".gates", "host-workers", winner.session+".json")
	data, err := os.ReadFile(pointerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &pointer); err != nil || pointer.Job != winner.directory {
		t.Fatal("hook reference does not locate original job", err)
	}
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	if directory, err := reopened.ClaimHostWorker(material, "", winner.session, nativeHome); err != nil || directory != winner.directory {
		t.Fatal("same-child recovery did not reuse original execution", directory, err)
	}
	if _, err := harness.InvokeHostWorkerTool(winner.directory, "submit", winner.session, json.RawMessage(`{"findings":[]}`), harness.ExecutionServices{Admit: AdmitExecution}); err != nil {
		t.Fatal(err)
	}
	var winningPath, winningTurn string
	for i, session := range children {
		if session == winner.session {
			winningPath, winningTurn = paths[i], turns[i]
		}
	}
	file, err := os.OpenFile(winningPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	complete, _ := json.Marshal(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "task_complete", "turn_id": winningTurn}})
	if _, err := file.Write(append(complete, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reopened.ObserveWorkerStart(material, winner.directory, reopened.HostWorkerStartEvidence); err != nil {
		t.Fatal(err)
	}
	source, err := harness.HostWorkerResultSource(winner.directory)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		acceptance, _, err := reopened.SubmitWorkerResult(WorkerResultEventID(material, ""), material, winner.directory, source, harness.NativeWorkerResultEvidence)
		if err != nil {
			t.Fatal(err)
		}
		if acceptance.Status != "ACCEPTED" && acceptance.Status != "DUPLICATE" && acceptance.Status != protocol.StatusCostPending {
			t.Fatalf("original host result not admitted: %+v", acceptance)
		}
	}
	for _, name := range []string{"owner", "process.json", "exit.json", "identity.json", "native-source.json"} {
		if _, err := os.Stat(filepath.Join(winner.directory, name)); !os.IsNotExist(err) {
			t.Fatal("host execution fabricated independent source", name, err)
		}
	}
	resolved := CostSourceBridge(root, f.runID, harness.NativeResultEvidence)(protocol.CostSourceRef{RunID: f.runID, ActionID: material.ActionID, Provider: "codex"})
	if resolved.State != protocol.CostSourceReady || resolved.Usage.InputCacheHitTokens != 5 || resolved.Usage.InputCacheMissTokens != 8 || resolved.Usage.OutputTokens != 3 {
		t.Fatalf("actual child metering changed: %+v", resolved)
	}
}

func appendHostWorkerRecord(t *testing.T, path, kind string, payload any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"type": kind, "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write(append(body, '\n'))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
}

func TestHostWorkerPreparePreservesEmptyOwnerAndCLIInlineArguments(t *testing.T) {
	f, _, material := hostWorkerFacade(t, "")
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	nativeHome := run.Settings.Values.Native["codexConfigRoot"]
	const parent = "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa"
	const child = "bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb"
	const controllerTurn = "cccccccc-3333-4333-8333-cccccccccccc"
	const childTurn = "dddddddd-4444-4444-8444-dddddddddddd"
	controllerPath := writeHostWorkerRollout(t, nativeHome, parent, "eeeeeeee-5555-4555-8555-eeeeeeeeeeee", controllerTurn, "controller-model", "medium")
	body, err := os.ReadFile(controllerPath)
	if err != nil {
		t.Fatal(err)
	}
	end := bytes.IndexByte(body, '\n')
	rootMeta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": parent, "source": "vscode"}})
	if err := os.WriteFile(controllerPath, append(append(rootMeta, '\n'), body[end+1:]...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.PrepareHostWorker(material, "", parent, nativeHome); err != nil {
		t.Fatal(err)
	}
	childPath := writeHostWorkerRollout(t, nativeHome, child, parent, childTurn, "worker-model", "high")
	opened, stable, err := OpenWorkerMaterial(material.Path)
	if err != nil || stable != material || opened.runID != f.runID {
		t.Fatal("stable original material lost scope", stable, err)
	}
	directory, err := opened.ClaimHostWorker(stable, "", child, nativeHome)
	if err != nil {
		t.Fatal(err)
	}
	message := "first \"quoted\" line\nsecond 'single' | pipe"
	arguments, _ := json.Marshal(map[string]any{"findings": []any{}, "message": message})
	name := "formal-gates"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	command := exec.Command(filepath.Join(f.admission.Target, "bin", name), "workflow", "worker", "--material", material.Path, "--operation", "submit", "--arguments", string(arguments))
	command.Env = append(os.Environ(), "CODEX_THREAD_ID="+child, "CODEX_HOME="+nativeHome)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual CLI inline request failed: %v %s", err, output)
	}
	appendHostWorkerRecord(t, childPath, "event_msg", map[string]any{"type": "task_complete", "turn_id": childTurn})
	if err := opened.ObserveWorkerStart(material, directory, opened.HostWorkerStartEvidence); err != nil {
		t.Fatal(err)
	}
	source, err := harness.HostWorkerResultSource(directory)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := harness.NativeWorkerResultEvidence("codex", directory, source)
	if err != nil {
		t.Fatal(err)
	}
	var actual struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(evidence.Answer, &actual); err != nil || actual.Message != message {
		t.Fatalf("CLI changed original quotes/newline: %q %v", actual.Message, err)
	}
	state, err := opened.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.State.OwnerSession != "" {
		t.Fatal("current controller rewrote historical owner scope")
	}
	if !bytes.Contains(readWorkerFile(t, filepath.Join(directory, "native-request.json")), []byte(`"controller"`)) {
		t.Fatal("prepared original controller was not retained")
	}
}

func TestHostWorkerInterruptionUsesOriginalRecoveryAndIdentity(t *testing.T) {
	const parent = "11111111-aaaa-4111-8111-111111111111"
	const child = "22222222-bbbb-4222-8222-222222222222"
	const firstTurn = "33333333-cccc-4333-8333-333333333333"
	const resumedTurn = "44444444-dddd-4444-8444-444444444444"
	f, root, material := hostWorkerFacade(t, parent)
	home := t.TempDir()
	path := writeHostWorkerRollout(t, home, child, parent, firstTurn, "worker-model", "high")
	if _, err := f.ClaimHostWorker(material, "", child, home); err != nil {
		t.Fatal(err)
	}
	appendHostWorkerRecord(t, path, "event_msg", map[string]any{"type": "turn_aborted", "turn_id": firstTurn, "reason": "interrupted"})
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	position := askPosition(t, run, protocol.ControlRecovery)
	candidate, err := f.PrepareControl(ControlInput{Ask: position, Choice: "resume", Reason: "Continue the original interrupted native child"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = submitControlWithHost(t, f, candidate)
	if err != nil {
		t.Fatal(err)
	}
	var resume string
	for _, intent := range run.AdapterIntents {
		if intent.Resume != nil && intent.Resume.Identity == child {
			resume = intent.ActionID
		}
	}
	if resume == "" {
		t.Fatal("original recovery did not issue same-child resume")
	}
	appendHostWorkerRecord(t, path, "event_msg", map[string]any{"type": "task_started", "turn_id": resumedTurn})
	appendHostWorkerRecord(t, path, "turn_context", map[string]any{"turn_id": resumedTurn, "model": "worker-model", "effort": "high"})
	_, _, directory, ref, err := f.WorkerExecutionRequest(material, resume)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := f.ClaimHostWorker(ref, resume, child, home)
	if err != nil || claimed != directory {
		t.Fatal("same-child resume changed source", claimed, err)
	}
	selected, selectedTurn, err := f.HostWorkerExecution(ref, child, false)
	if err != nil || selected != directory || selectedTurn.ID != resumedTurn {
		t.Fatal("normal tools did not select original signed resume", selected, selectedTurn, err)
	}
	if _, err := harness.InvokeHostWorkerTool(directory, "submit", child, json.RawMessage(`{"findings":[]}`), harness.ExecutionServices{Admit: AdmitExecution}); err != nil {
		t.Fatal(err)
	}
	appendHostWorkerRecord(t, path, "event_msg", map[string]any{"type": "task_complete", "turn_id": resumedTurn})
	if _, err := f.Drive(); err != nil {
		t.Fatal("normal reentry could not observe original/resumed sources", err)
	}
	reader, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.ObserveWorkerStart(ref, directory, reader.HostWorkerStartEvidence); err != nil {
		t.Fatal(err)
	}
	source, err := harness.HostWorkerResultSource(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reader.SubmitWorkerResult(WorkerResultEventID(ref, resume), ref, directory, source, harness.NativeWorkerResultEvidence); err != nil {
		t.Fatal(err)
	}
}

func TestHostWorkerTerminationRequiresNativeTerminalFact(t *testing.T) {
	const parent = "11111111-aaaa-4111-8111-111111111111"
	const child = "22222222-bbbb-4222-8222-222222222222"
	const turn = "33333333-cccc-4333-8333-333333333333"
	f, _, material := hostWorkerFacade(t, parent)
	home := t.TempDir()
	path := writeHostWorkerRollout(t, home, child, parent, turn, "worker-model", "high")
	if _, err := f.ClaimHostWorker(material, "", child, home); err != nil {
		t.Fatal(err)
	}
	request, err := f.PrepareControl(ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := submitControlWithHost(t, f, request)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := f.PrepareControl(ControlInput{Ask: askPosition(t, run, protocol.ControlAbort), Choice: "abort", Reason: "Stop original native child"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = submitControlWithHost(t, f, decision)
	if err != nil {
		t.Fatal(err)
	}
	var termination protocol.HostActionIntent
	for _, intent := range run.AdapterIntents {
		if intent.Terminate != nil {
			termination = intent
		}
	}
	if termination.ActionID == "" {
		t.Fatal("abort did not issue original termination")
	}
	if handled, stopped, err := f.HostWorkerTermination(termination); err != nil || !handled || stopped {
		t.Fatal("running native child was treated as stopped", handled, stopped, err)
	}
	appendHostWorkerRecord(t, path, "event_msg", map[string]any{"type": "turn_aborted", "turn_id": turn, "reason": "interrupted"})
	if handled, stopped, err := f.HostWorkerTermination(termination); err != nil || !handled || !stopped {
		t.Fatal("actual native stop was not admitted", handled, stopped, err)
	}
	snap, err := f.engine.Load()
	if err != nil || snap.State.HostActionReceipts[termination.ActionID].Status != protocol.HostActionStatusExecuted {
		t.Fatal("actual stop did not follow original termination admission", err)
	}
	run, err = f.Drive()
	if err != nil || run.Status != "ABORTED" {
		t.Fatal("native termination did not finish original abort", run.Status, err)
	}
}

func TestHostWorkerMaterialKeepsParallelTaskAfterPeerCompletion(t *testing.T) {
	isolated := t.TempDir()
	t.Setenv("HOME", isolated)
	t.Setenv("USERPROFILE", isolated)
	plan := commandPlan()
	independent := commandPlan().Batches[0]
	independent.ID, independent.Artifacts = "independent", []string{"independent.go"}
	independent.Subtasks = []protocol.DevelopmentSubtask{{ID: "utility", Description: "Deliver the independent utility"}}
	plan.Batches = append(plan.Batches, independent)
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "Independent command and utility"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality"}))
	d.drive()
	first, input := planBatchAction(d, "command")
	second, _ := planBatchAction(d, "independent")
	firstRef, secondRef := d.run.WorkerMaterials[first], d.run.WorkerMaterials[second]
	if firstRef.Path == secondRef.Path || firstRef.ActionID == secondRef.ActionID {
		t.Fatal("parallel tasks did not retain distinct original materials")
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nfunc main() { println(\"ready\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(first, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
	d.drive()
	reader, ref, err := OpenWorkerMaterial(secondRef.Path)
	if err != nil || ref != secondRef {
		t.Fatal("peer completion changed the late original material", ref, err)
	}
	request, _, _, original, err := reader.WorkerExecutionRequest(ref, "")
	if err != nil || original != secondRef {
		t.Fatal("late worker did not retain its exact issued task", original, err)
	}
	var binding struct {
		Engine struct {
			WorkerMaterial WorkerMaterial `json:"workerMaterial"`
		} `json:"engine"`
	}
	body, _ := json.Marshal(request)
	if err := json.Unmarshal(body, &binding); err != nil || binding.Engine.WorkerMaterial != secondRef {
		t.Fatal("stable path selected another parallel task", err)
	}
}

func TestHostWorkerContinuesAcceptedPreparationInOriginalChild(t *testing.T) {
	originalVerifier := NativeSourceVerifier
	NativeSourceVerifier = func(provider, directory string, source map[string]any) ([]byte, string, string, error) {
		if source["source"] == "host" {
			return harness.NativeResultEvidence(provider, directory, source)
		}
		return originalVerifier(provider, directory, source)
	}
	t.Cleanup(func() { NativeSourceVerifier = originalVerifier })
	const parent = "11111111-1111-4111-8111-111111111111"
	const child = "22222222-2222-4222-8222-222222222222"
	const first = "33333333-3333-4333-8333-333333333333"
	const next = "44444444-4444-4444-8444-444444444444"
	f, root, product := hostWorkerFacade(t, parent)
	original, err := readWorkerMaterial(product)
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	d := &validationDriver{t: t, f: f, run: run, revision: original.Input.RequirementDocument.ContentDigest, provider: "codex", git: vcs.New(root, nil)}
	plan := commandPlan()
	commands := []string{}
	for i := range plan.Batches[0].Checks {
		check := &plan.Batches[0].Checks[i]
		commands = append(commands, check.Command)
		check.Goal = check.Command
		check.Command = ""
	}
	passPreDevelopmentReviews(t, d, plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command"}})
	d.confirmRoute(routeDecisionForGates(nil))
	d.drive()
	action, _ := d.action("engine.development.batch.v6")
	material := d.run.WorkerMaterials[action]
	home := t.TempDir()
	value, err := readWorkerMaterial(material)
	if err != nil {
		t.Fatal(err)
	}
	choice := value.Input.Execution
	model, effort := choice.Model, choice.ReasoningEffort
	if model == "" {
		model = "worker-model"
	}
	if effort == "" {
		effort = "high"
	}
	path := writeHostWorkerRollout(t, home, child, parent, first, model, effort)
	directory, err := f.ClaimHostWorker(material, "", child, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.InvokeHostWorkerTool(directory, "submit", child, func() json.RawMessage {
		data, _ := json.Marshal(map[string]any{"summary": "Prepared checks", "commands": commands, "remainingWork": "Implement the command"})
		return data
	}(), harness.ExecutionServices{Admit: AdmitExecution}); err != nil {
		t.Fatal(err)
	}
	appendHostWorkerRecord(t, path, "event_msg", map[string]any{"type": "task_complete", "turn_id": first})
	if err := f.ObserveWorkerStart(material, directory, f.HostWorkerStartEvidence); err != nil {
		t.Fatal(err)
	}
	source, err := harness.HostWorkerResultSource(directory)
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := f.SubmitWorkerResult(WorkerResultEventID(material, ""), material, directory, source, harness.NativeWorkerResultEvidence)
	if err != nil || accepted.Status != "ACCEPTED" {
		snap, _ := f.engine.Load()
		t.Logf("rejection: %+v", snap.State.RejectedResults[material.ActionID])
		t.Fatal(accepted, err, CostSourceBridge(root, f.runID, harness.NativeResultEvidence)(protocol.CostSourceRef{RunID: f.runID, ActionID: material.ActionID, Provider: "codex"}))
	}
	_, _, nextDirectory, current, err := f.WorkerExecutionRequest(material, "")
	if err != nil {
		t.Fatal(err)
	}
	if current == material || current.ActionID != material.ActionID || current.AttemptID != material.AttemptID {
		t.Fatal("normal preparation changed action/attempt or did not advance")
	}
	if _, err := f.ClaimHostWorker(current, "", child, home); err == nil {
		t.Fatal("accepted prior turn was reused as another invocation")
	}
	const other = "55555555-5555-4555-8555-555555555555"
	writeHostWorkerRollout(t, home, other, parent, "66666666-6666-4666-8666-666666666666", model, effort)
	if _, err := f.ClaimHostWorker(current, "", other, home); err == nil {
		t.Fatal("another child claimed a continuation")
	}
	identity, err := f.HostWorkerContinuation(current)
	if err != nil || identity != child {
		t.Fatal("continuation projection lost original identity", identity, err)
	}

	appendHostWorkerRecord(t, path, "event_msg", map[string]any{"type": "task_started", "turn_id": next})
	appendHostWorkerRecord(t, path, "turn_context", map[string]any{"turn_id": next, "model": model, "effort": effort})
	claimed, err := f.ClaimHostWorker(current, "", child, home)
	if err != nil || claimed != nextDirectory {
		t.Fatal("normal continuation could not use original child", claimed, err)
	}
	selected, turn, err := f.HostWorkerExecution(current, child, false)
	if err != nil || selected != nextDirectory || turn.ID != next {
		t.Fatal("tools did not bind current invocation", selected, turn, err)
	}
	if repeated, err := f.ClaimHostWorker(current, "", child, home); err != nil || repeated != claimed {
		t.Fatal("duplicate current claim changed invocation", repeated, err)
	}
	if _, err := harness.InvokeHostWorkerTool(claimed, "submit", child, json.RawMessage(`{"summary":"Continued implementation","remainingWork":"Complete the command"}`), harness.ExecutionServices{Admit: AdmitExecution}); err != nil {
		t.Fatal(err)
	}
	appendHostWorkerRecord(t, path, "token_usage_record", map[string]any{"thread_id": child, "turn_id": next, "response_id": "response-continued", "usage": map[string]any{"input_tokens": 13, "cached_input_tokens": 5, "output_tokens": 3}})
	appendHostWorkerRecord(t, path, "event_msg", map[string]any{"type": "task_complete", "turn_id": next})
	if err := f.ObserveWorkerStart(current, claimed, f.HostWorkerStartEvidence); err != nil {
		t.Fatal(err)
	}
	source, err = harness.HostWorkerResultSource(claimed)
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err = f.SubmitWorkerResult(WorkerResultEventID(current, ""), current, claimed, source, harness.NativeWorkerResultEvidence)
	if err != nil || accepted.Status != "ACCEPTED" {
		t.Fatal("continued result", accepted, err)
	}
	_, _, lastDirectory, last, err := f.WorkerExecutionRequest(current, "")
	if err != nil {
		t.Fatal(err)
	}
	const lastTurn = "77777777-7777-4777-8777-777777777777"
	appendHostWorkerRecord(t, path, "event_msg", map[string]any{"type": "task_started", "turn_id": lastTurn})
	appendHostWorkerRecord(t, path, "turn_context", map[string]any{"turn_id": lastTurn, "model": model, "effort": effort})
	if repeated, err := f.ClaimHostWorker(last, "", child, home); err != nil || repeated != lastDirectory {
		t.Fatal("second normal continuation", repeated, err)
	}

}
