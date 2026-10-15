package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
)

func TestReadOnlyDevelopmentPreparationRetainsNativeContinuation(t *testing.T) {
	t.Parallel()
	for _, receiptFirst := range []bool{true, false} {
		name := "receipt-before-result"
		if !receiptFirst {
			name = "result-before-receipt-reopen"
		}
		t.Run(name, func(t *testing.T) {
			d := nativeMasterDriver(t)
			plan := commandPlan()
			plan.Batches[0].Checks = []protocol.DevelopmentCheck{
				{ID: "edit", Purpose: protocol.CheckEdit, Goal: "Check the declared source after every edit"},
				{ID: "build", Purpose: protocol.CheckFullBuild, Goal: "Build the complete project"},
				{ID: "tests", Purpose: protocol.CheckFullTests, Goal: "Test the complete project"},
			}
			passPreDevelopmentReviews(t, d, plan)
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one declared artifact"}})
			d.confirmRoute(routeDecisionForGates([]string{"quality"}))
			d.drive()
			id, input := d.action("engine.development.batch.v6")
			state := driverState(t, d)
			pending, attempt := state.PendingActions[id], state.Attempts[input.Binding.Task]
			ref := d.run.WorkerMaterials[id]
			material, err := readWorkerMaterial(ref)
			if err != nil || !material.ReadOnly || material.Input.Pending != pending || material.Input.Validation.Binding != input.Binding {
				t.Fatal("issued preparation lost its read-only material or original action/attempt", err)
			}
			workspace := material.Input.Validation.Workspace
			native := vcs.NewNative(workspace.Path, material.Input.VCS, nil).At(workspace.Path, workspace.Handle)
			beforePaths, err := native.ChangedPaths()
			if err != nil {
				t.Fatal(err)
			}
			commands := []string{"printf '%s' 'edit 中文'\r\n", "printf 'project build\\n'", "printf 'project tests\\n'"}
			progress := worker.DevelopmentContinuation{Commands: commands, Summary: "Prepared the declared calls without changing files", RemainingWork: "Implement the confirmed command and run its approved checks"}
			answer, err := json.Marshal(progress)
			if err != nil {
				t.Fatal(err)
			}
			request, _, directory, selected, err := d.f.WorkerExecutionRequest(ref, "")
			if err != nil || selected != ref || request["read_only"] != true {
				t.Fatal("native preparation request differs from its issued material", err)
			}
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			requestBody, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "native-request.json"), requestBody, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "answer.json"), answer, 0600); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(answer)
			digest := "sha256:" + hex.EncodeToString(sum[:])
			source := map[string]any{"session": "original-native-session", "answerDigest": digest}
			verify := func(provider, path string, actual map[string]any) (worker.ResultSource, error) {
				if provider != request["provider"] || path != directory || !reflect.DeepEqual(actual, source) || actual["effects"] != nil {
					t.Fatal("native fixture changed original provider, request directory, session or source")
				}
				raw, err := os.ReadFile(filepath.Join(path, "answer.json"))
				if err != nil {
					return worker.ResultSource{}, err
				}
				actualSum := sha256.Sum256(raw)
				if "sha256:"+hex.EncodeToString(actualSum[:]) != actual["answerDigest"] {
					t.Fatal("native fixture source does not bind the original answer bytes")
				}
				return worker.ResultSource{Answer: raw, Digest: digest, Identity: actual["session"].(string)}, nil
			}
			if receiptFirst {
				d.receipt(id)
			}
			accepted, run, err := d.f.SubmitWorkerResult(d.eventID(), ref, directory, source, verify)
			want := "ACCEPTED"
			if !receiptFirst {
				want = "STAGED"
			}
			if err != nil || accepted.Status != want {
				t.Fatalf("original native preparation = %+v %v", accepted, err)
			}
			d.run = run
			if !receiptFirst {
				staged := driverState(t, d).StagedResults[id]
				if staged.PayloadDigest != digest || staged.Validation.Development.Paths != nil || !slices.Equal(staged.Validation.Development.Commands, commands) {
					t.Fatal("staging invented path facts or lost the original preparation")
				}
				d.reopen()
				d.receipt(id)
			}
			state = driverState(t, d)
			first, receipt := state.Results[id], state.SpawnReceipts[id]
			if first.PayloadDigest != digest || first.Validation.Status != "CONTINUE" || first.Validation.Binding != input.Binding || first.Validation.Development.Paths != nil || first.Validation.Development.Completion != nil || first.Validation.Development.Summary != progress.Summary || first.Validation.Development.RemainingWork != progress.RemainingWork || !slices.Equal(first.Validation.Development.Commands, commands) || state.PendingActions[id] != pending || !reflect.DeepEqual(state.Attempts[input.Binding.Task], attempt) || len(state.EndedAttempts) != 0 || len(state.RejectedResults) != 0 {
				t.Fatal("read-only native preparation changed its original obligation or invented completion/path evidence")
			}
			accepted, run, err = d.f.SubmitWorkerResult(d.eventID(), ref, directory, source, verify)
			if err != nil || accepted.Status != "DUPLICATE" {
				t.Fatalf("repeated original preparation = %+v %v", accepted, err)
			}
			d.run = run
			d.reopen()
			d.drive()
			continuedRef := d.run.WorkerMaterials[id]
			continued, err := readWorkerMaterial(continuedRef)
			if err != nil || continued.ReadOnly || continuedRef.AttemptID != ref.AttemptID || continued.Input.Pending != pending || continued.Input.Validation.Binding != input.Binding || !reflect.DeepEqual(continued.Input.Validation.Development.Result, &first) || !strings.Contains(continued.Prompt, progress.RemainingWork) {
				t.Fatal("persistent preparation did not continue the same writable action with its exact feedback", err)
			}
			state = driverState(t, d)
			if !reflect.DeepEqual(state.Results[id], first) || !reflect.DeepEqual(state.SpawnReceipts[id], receipt) || !reflect.DeepEqual(state.Attempts[input.Binding.Task], attempt) || len(state.RecoverableResults[id]) != 0 {
				t.Fatal("repeat/reopen changed the preparation, native receipt or original attempt")
			}
			afterPaths, err := native.ChangedPaths()
			if err != nil || !reflect.DeepEqual(afterPaths, beforePaths) {
				t.Fatal("read-only preparation changed the VCS workspace", err)
			}

			// Writable continuation uses the same source-bound native codec and
			// must reject absent terminal facts before admitting any new progress.
			request, _, directory, selected, err = d.f.WorkerExecutionRequest(continuedRef, "")
			if err != nil || selected != continuedRef || request["read_only"] != false {
				t.Fatal("prepared calls did not open the original writable continuation", err)
			}
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			requestBody, err = json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "native-request.json"), requestBody, 0600); err != nil {
				t.Fatal(err)
			}
			answer = []byte(`{"summary":"Reported writable progress","remainingWork":"Finish the original implementation"}`)
			if err := os.WriteFile(filepath.Join(directory, "answer.json"), answer, 0600); err != nil {
				t.Fatal(err)
			}
			sum = sha256.Sum256(answer)
			digest = "sha256:" + hex.EncodeToString(sum[:])
			source = map[string]any{"session": "original-native-session", "answerDigest": digest}
			accepted, run, err = d.f.SubmitWorkerResult(d.eventID(), continuedRef, directory, source, verify)
			if err != nil || accepted.Status != "REJECTED_INVALID_RESULT" {
				t.Fatalf("writable continuation without terminal facts = %+v %v", accepted, err)
			}
			d.run = run
			state = driverState(t, d)
			if !strings.Contains(state.RejectedResults[id].Reason, "original native terminal path facts") || !reflect.DeepEqual(state.Results[id], first) || !reflect.DeepEqual(state.SpawnReceipts[id], receipt) {
				t.Fatal("writable rejection lost its original evidence or replaced accepted preparation")
			}
		})
	}
}

func TestDevelopmentFailedCheckRetainsIssuedObligation(t *testing.T) {
	t.Parallel()
	d := nativeMasterDriver(t)
	plan := commandPlan()
	plan.Batches[0].Artifacts = []string{"app.go", "handoff.txt"}
	plan.Batches[0].Checks = []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "printf 'build: actual source check\\n'; grep -q '^// ready$' app.go"}, {ID: "tests", Purpose: protocol.CheckFullTests, Command: "printf 'tests: actual source check\\n'; grep -q '^// ready$' app.go"}}
	passPreDevelopmentReviews(t, d, plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one declared artifact"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality"}))
	d.drive()
	id, input := d.action("engine.development.batch.v6")
	issued := driverState(t, d).Attempts[input.Binding.Task]
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\n// unfinished\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "handoff.txt"), []byte("retained delivery\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.receipt(id)
	originalMaterial := d.run.WorkerMaterials[id]
	priorCompletion := batchEvidence(input).Completion
	completion := &worker.Completion{Handoff: priorCompletion.Handoff}
	for _, criterion := range input.Development.Batch.DoD {
		completion.DoD = append(completion.DoD, priorCompletion.DoD[criterion.ID])
	}
	for _, point := range priorCompletion.RequirementResults {
		completion.Requirements = append(completion.Requirements, point.Evidence)
	}
	original := submitDevelopmentSemanticFixture(t, d, originalMaterial, worker.Development{Summary: "delivered edit; approved source checks still own completion", Completion: completion}, "fixture-native-session")
	d.drive()
	failed := driverState(t, d)
	var failedReceipt protocol.HostActionReceipt
	for _, receipt := range failed.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" || receipt.AdapterEvidence == nil {
			continue
		}
		body, _ := json.Marshal(receipt.AdapterEvidence.Values)
		var check protocol.CheckEvidence
		if json.Unmarshal(body, &check) == nil && check.SourceActionID == id && check.ExitCode == 1 {
			failedReceipt = receipt
		}
	}
	if failedReceipt.ActionID == "" {
		t.Fatal("fixture did not observe a real failing approved check")
	}
	if failed.TaskStatusOf(input.Binding.Task) == runtime.TaskTerminal || failed.PendingActions[id].AttemptID != issued.ID || failed.Attempts[input.Binding.Task].ID != issued.ID {
		t.Fatalf("unfinished batch lost its original action/attempt: task=%s pending=%+v attempt=%+v", failed.TaskStatusOf(input.Binding.Task), failed.PendingActions[id], failed.Attempts[input.Binding.Task])
	}
	if _, ok := d.run.WorkerMaterials[id]; !ok {
		t.Fatal("original developer has no completion material after its actual check failure")
	}
	result := failed.Results[id]
	failedBytes, _ := json.Marshal(failedReceipt)
	spawnBytes, _ := json.Marshal(failed.SpawnReceipts[id])
	before, _ := json.Marshal(failed)
	d.submit(original)
	if after, _ := json.Marshal(driverState(t, d)); !bytes.Equal(before, after) {
		t.Fatal("duplicate edit changed failed source or original dispatch")
	}
	var group sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() { defer group.Done(); _, err := d.f.Drive(); errors <- err }()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	d.reopen()
	d.drive()
	continuation := d.run.WorkerMaterials[id]
	if continuation.AttemptID != originalMaterial.AttemptID || continuation.Digest == originalMaterial.Digest {
		t.Fatal("continuation did not retain original attempt with new immutable feedback material")
	}
	body, err := os.ReadFile(continuation.Path)
	if err != nil {
		t.Fatal(err)
	}
	var material worker.Material
	if err := json.Unmarshal(body, &material); err != nil {
		t.Fatal(err)
	}
	feedback := material.Input.Validation.Development
	if !reflect.DeepEqual(feedback.Batch, input.Development.Batch) || feedback.Result == nil || feedback.Result.Digest != result.Digest || len(feedback.Checks) != 1 || feedback.Checks[0].ExitCode != 1 || feedback.Checks[0].Directory == "" {
		t.Fatalf("continuation lacks exact confirmed batch and authentic failed-check feedback: %+v", feedback)
	}
	if material.Input.Validation.Binding != input.Binding || material.Input.Pending.ActionID != id || material.Input.Pending.AttemptID != issued.ID {
		t.Fatal("continuation changed issued work identity")
	}
	stdout, err := os.ReadFile(filepath.Join(feedback.Checks[0].Directory, "stdout.log"))
	if err != nil || !strings.Contains(string(stdout), "build: actual source check") {
		t.Fatalf("original failed stdout: %s %v", stdout, err)
	}
	var contextInput protocol.WorkerInput
	contextJSON := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
	if err := json.Unmarshal([]byte(contextJSON), &contextInput); err != nil {
		t.Fatal(err)
	}
	projected := contextInput.Validation.Development
	if !reflect.DeepEqual(projected.Batch, feedback.Batch) || projected.Result == nil || projected.Result.Outcome != feedback.Result.Outcome || projected.Result.FailureClass != feedback.Result.FailureClass || projected.Result.InterruptionReason != feedback.Result.InterruptionReason || projected.Result.InvalidReason != feedback.Result.InvalidReason || projected.Result.Validation == nil || projected.Result.Validation.Status != feedback.Result.Validation.Status || !reflect.DeepEqual(projected.Result.Validation.Development, feedback.Result.Validation.Development) || len(projected.Checks) != 1 || projected.Checks[0].ExitCode != feedback.Checks[0].ExitCode || projected.Checks[0].Stdout != feedback.Checks[0].Stdout || projected.Checks[0].Stderr != feedback.Checks[0].Stderr {
		t.Fatal("material lost confirmed work or actual check observations")
	}
	if contextInput.Validation.Workspace != nil || projected.Checks[0].Directory != "" || strings.Contains(material.Prompt, input.Workspace.Path) {
		t.Fatal("semantic prompt requires program-owned physical paths")
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\n// incomplete change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	next := submitDevelopmentSemanticFixture(t, d, continuation, worker.Development{Summary: "changed original implementation; actual checks still own completion", Completion: completion}, "fixture-native-session")
	d.drive()
	second := driverState(t, d)
	if second.Results[id].Digest == result.Digest || second.PendingActions[id].AttemptID != issued.ID {
		t.Fatal("next failed edit changed dispatch or did not advance source")
	}
	stale := next
	stale.ID = d.eventID()
	payload := *next.Result
	stale.Result = &payload
	stale.Result.PayloadDigest = "late payload based on obsolete feedback"
	priorState, _ := json.Marshal(second)
	if _, _, err := d.f.Submit(stale); err == nil || !strings.Contains(err.Error(), "RECEIPT_CONFLICT") {
		t.Fatalf("late old-feedback result not rejected: %v", err)
	}
	if after, _ := json.Marshal(driverState(t, d)); !bytes.Equal(priorState, after) {
		t.Fatal("old-feedback result changed current source or completed duty")
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\n// ready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	final := submitDevelopmentSemanticFixture(t, d, d.run.WorkerMaterials[id], worker.Development{Summary: "completed original authorized implementation", Completion: completion}, "fixture-native-session")
	d.drive()
	completed := driverState(t, d)
	if completed.TaskStatusOf(input.Binding.Task) != runtime.TaskTerminal || completed.PendingActions[id].ActionID != "" || len(completed.RecoverableResults[id]) != 2 || completed.RecoverableResults[id][0].Digest != result.Digest {
		t.Fatal("actual checks did not settle original batch with retained edit evidence")
	}
	if completed.Results[id].Digest == result.Digest || !slices.Equal(completed.Results[id].Validation.Development.Paths, []string{"app.go", "handoff.txt"}) {
		t.Fatal("new result lost its source identity or programmatically retained delivery paths")
	}
	if receiptBytes, _ := json.Marshal(completed.HostActionReceipts[failedReceipt.ActionID]); !bytes.Equal(receiptBytes, failedBytes) {
		t.Fatal("old failure receipt was overwritten")
	}
	if receiptBytes, _ := json.Marshal(completed.SpawnReceipts[id]); !bytes.Equal(receiptBytes, spawnBytes) {
		t.Fatal("batch continuation respawned or changed native identity")
	}
	passed := 0
	for _, receipt := range completed.HostActionReceipts {
		if receipt.AdapterOperation != "op.check" || receipt.AdapterEvidence == nil {
			continue
		}
		body, _ := json.Marshal(receipt.AdapterEvidence.Values)
		var check protocol.CheckEvidence
		if json.Unmarshal(body, &check) == nil && check.SourceActionID == id && check.ExitCode == 0 {
			if check.SourceIdentity == feedback.Checks[0].SourceIdentity || check.SourceDigest == feedback.Checks[0].SourceDigest {
				t.Fatal("new checks reused failed immutable source")
			}
			passed++
		}
	}
	if passed != 2 {
		t.Fatalf("approved full checks not executed on new source: %d", passed)
	}
	if issued.Attempts != failed.Attempts[input.Binding.Task].Attempts || failed.Attempts[input.Binding.Task].RetryExhausted {
		t.Fatal("business continuation consumed runtime retry allowance")
	}
	settled, _ := json.Marshal(completed)
	final.ID = d.eventID()
	if acceptance := d.submit(final); acceptance.Status != "DUPLICATE" {
		t.Fatalf("final edit replay: %+v", acceptance)
	}
	next.ID = d.eventID()
	if acceptance := d.submit(next); acceptance.Status != "DUPLICATE" {
		t.Fatalf("new edit replay: %+v", acceptance)
	}
	original.ID = d.eventID()
	if acceptance := d.submit(original); acceptance.Status != "DUPLICATE" {
		t.Fatalf("old edit replay: %+v", acceptance)
	}
	// Fresh event IDs have their own immutable event receipts; business facts
	// and native effects remain identical.
	completed.Events = driverState(t, d).Events
	settled, _ = json.Marshal(completed)
	d.submit(final)
	d.submit(next)
	d.submit(original)
	d.reopen()
	d.drive()
	if after, _ := json.Marshal(driverState(t, d)); !bytes.Equal(settled, after) {
		t.Fatal("duplicate/reopen changed completed batch or repeated native checks")
	}
}

// Semantic worker answers and the native verifier are local test fixtures;
// candidate freezing and approved checks use the normal physical adapters.
func submitDevelopmentSemanticFixture(t *testing.T, d *validationDriver, ref WorkerMaterial, development worker.Development, session string) protocol.Event {
	t.Helper()
	body, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	var material worker.Material
	if err := json.Unmarshal(body, &material); err != nil {
		t.Fatal(err)
	}
	answer, err := json.Marshal(development)
	if err != nil {
		t.Fatal(err)
	}
	request, _, directory, selected, err := d.f.WorkerExecutionRequest(ref, "")
	if err != nil || selected != ref {
		t.Fatalf("semantic fixture lost its original material: %+v %v", selected, err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "native-request.json"), requestBody, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(answer)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	eventID := d.eventID()
	verify := func(provider, path string, source map[string]any) (worker.ResultSource, error) {
		descriptor, err := host.Lookup(d.host())
		if err != nil || provider != descriptor.InstallName || path != directory || source["session"] != "fixture-native-session" || session != source["session"] {
			t.Fatal("semantic fixture changed native session/provider/source")
		}
		return worker.ResultSource{Answer: answer, Digest: digest, Identity: session}, nil
	}
	resource := material.Input.Validation.Workspace
	paths, err := vcs.NewNative(resource.Path, material.Input.VCS, nil).At(resource.Path, resource.Handle).ChangedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if paths == nil {
		paths = []string{}
	}
	source := map[string]any{"session": session, "effects": map[string]any{"paths": paths}}
	acceptance, run, err := d.f.SubmitWorkerResult(eventID, ref, directory, source, verify)
	if err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("semantic result admission: %+v %v", acceptance, err)
	}
	d.run = run
	event, err := worker.DecodeResultWithPaths(material.Input, eventID, answer, digest, paths)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestRepairFailedProjectCheckRetainsIssuedObligation(t *testing.T) {
	t.Parallel()
	d, input, id := nativeRepairReady(t, "P1")
	d.receipt(id)
	issued := driverState(t, d).Attempts[input.Binding.Task]
	spawn := driverState(t, d).SpawnReceipts[id]
	partial := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "read-current-repair", Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "CONTINUE", Development: &protocol.DevelopmentEvidence{Paths: []string{}, Summary: "Read the authorized repair", RemainingWork: "Correct the command output"}}}}
	d.submit(partial)
	d.reopen()
	d.drive()
	if d.run.ValidationInputs[id].Repair.Result == nil || d.run.ValidationInputs[id].Repair.Result.Digest != driverState(t, d).Results[id].Digest {
		t.Fatal("repair continuation lost original progress")
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nfunc main(){ missing syntax\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result := gateRepairResult(input)
	result.Development.RequirementResults = fixtureImplementedPoints(input.Repair.RequiredPoints, "Report this repair edit; the actual approved checks determine whether it can complete")
	submit := func(body protocol.ValidationResult, digest string) protocol.Event {
		t.Helper()
		event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: digest, SourceResult: driverState(t, d).Results[id].Digest, Validation: &body}}
		if actual := d.submit(event); actual.Status != "ACCEPTED" {
			t.Fatalf("repair edit was not accepted for its original approved checks: %+v", actual)
		}
		d.drive()
		return event
	}
	failedEvent := submit(result, "actual-repair-edit")
	failed := driverState(t, d)
	if failed.PendingActions[id].AttemptID != issued.ID || failed.TaskStatusOf(input.Binding.Task) == runtime.TaskTerminal {
		t.Fatal("repair PASS removed its original execution before approved checks passed")
	}
	d.submit(failedEvent)
	d.reopen()
	d.drive()
	material := d.run.WorkerMaterials[id]
	body, err := os.ReadFile(material.Path)
	if err != nil {
		t.Fatal(err)
	}
	var rendered worker.Material
	if err := json.Unmarshal(body, &rendered); err != nil {
		t.Fatal(err)
	}
	feedback := rendered.Input.Validation.Repair
	if feedback.Result == nil || feedback.Result.Digest != failed.Results[id].Digest || len(feedback.Checks) != 1 || feedback.Checks[0].ExitCode == 0 || feedback.Checks[0].Stdout.SHA256 == "" || feedback.Checks[0].Stderr.SHA256 == "" {
		t.Fatalf("original repair has no exact failed native-check feedback: %+v", feedback)
	}
	if !strings.Contains(rendered.Prompt, "checks") || strings.Contains(rendered.Prompt, feedback.Checks[0].Directory) {
		t.Fatal("repair prompt did not project original check facts without physical evidence paths")
	}
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nimport \"os\"\nfunc main(){if len(os.Args)>1&&os.Args[1]==\"invalid\"{os.Exit(2)}}\n// repaired command\n"), 0600); err != nil {
		t.Fatal(err)
	}
	final := submit(result, "completed-repair-edit")
	completed := driverState(t, d)
	if completed.TaskStatusOf(input.Binding.Task) != runtime.TaskTerminal || completed.PendingActions[id].ActionID != "" || completed.Attempts[input.Binding.Task].ID != "" || !reflect.DeepEqual(completed.SpawnReceipts[id], spawn) {
		t.Fatal("successful native full checks did not settle the original repair")
	}
	if len(completed.RecoverableResults[id]) != 2 || completed.RecoverableResults[id][0].Digest != failed.RecoverableResults[id][0].Digest {
		t.Fatal("repair lost its previous original-action evidence")
	}
	before, _ := json.Marshal(completed.HostActionReceipts)
	d.submit(final)
	d.submit(failedEvent)
	d.submit(partial)
	d.reopen()
	d.drive()
	after, _ := json.Marshal(driverState(t, d).HostActionReceipts)
	if !bytes.Equal(before, after) {
		t.Fatal("duplicate/reload re-executed settled repair checks")
	}
}
