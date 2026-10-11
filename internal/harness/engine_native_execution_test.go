//go:build !windows

package harness

// These cases run the installed command against the original local app-server
// peer. Its RPC log counts actual model turns; it never starts an installed host.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/validate"
)

func engineNativeFixture(t *testing.T, installed ...bool) (*facade.Facade, facade.WorkerMaterial, *codexScenario, string) {
	t.Helper()
	s := newCodexScenario(t)
	project := s.workspace
	if err := os.WriteFile(filepath.Join(project, "requirements.md"), []byte(canaryRequirementDocument), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "app.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".gitignore"), []byte(".gates/\n.codex/\nAGENTS.md\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = project
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=NativeFixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=NativeFixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.name", "Native fixture")
	git("config", "user.email", "fixture@example.invalid")
	git("add", ".")
	git("commit", "-qm", "Native fixture input")
	base := git("rev-parse", "HEAD")
	registry, target, generation, lease, token := testkit.Installation(t, project, "native-target", "sha256:native-package", "codex")
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"prompts", "gates"} {
		if err := filepath.WalkDir(filepath.Join(source, name), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			out := filepath.Join(target, rel)
			if entry.IsDir() {
				return os.MkdirAll(out, 0700)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(out, body, 0600)
		}); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(target, "bin", "formal-gates")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = source
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("candidate build: %v %s", err, out)
	}
	admission := facade.Admission{RegistryPath: registry, Target: target, Root: project, Provider: "codex", PackageDigest: "sha256:native-package", InstalledTargetIdentity: "native-target", Generation: generation, Lease: lease, Token: token}
	if len(installed) > 0 && installed[0] {
		candidate := t.TempDir()
		inventory, err := validate.PackageReceipt(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range inventory.Entries {
			body, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(entry.Path)))
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(candidate, filepath.FromSlash(entry.Path))
			if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(out, body, os.FileMode(entry.Mode)); err != nil {
				t.Fatal(err)
			}
		}
		body, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(candidate, "bin"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(candidate, "bin", "formal-gates"), body, 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", s.home)
		t.Setenv("USERPROFILE", s.home)
		binary = filepath.Join(s.home, ".local", "bin", "formal-gates")
		options := validate.InstallOptions{Source: candidate, Host: "codex", Scope: "project", Project: project, BinaryTarget: binary, Force: true, SkipHooks: true}
		if _, err := validate.Install(options); err != nil {
			t.Fatal(err)
		}
		options.Bootstrap = true
		report, err := validate.Install(options)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := validate.LoadRegistry(report.Registry)
		if err != nil || len(registry.Records) != 1 {
			t.Fatal("installed registry", err)
		}
		record := registry.Records[0]
		admission = facade.Admission{RegistryPath: report.Registry, Target: record.Target, Root: project, Provider: "codex", PackageDigest: record.PackageDigest, InstalledTargetIdentity: record.ID, Generation: record.Generation, Lease: record.Lease, Token: record.Token}
	}
	c := engineCanary{t: t, doc: []byte(canaryRequirementDocument)}
	f, _, err := facade.Start(facade.StartOptions{Root: project, Request: facade.StartRequest{RunID: "native-execution", Route: "regular", VCS: "git", Provider: "codex", BaseSnapshot: base, DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: c.confirmedIntake()}, Admission: &admission, Capacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	var ref facade.WorkerMaterial
	for _, value := range run.WorkerMaterials {
		ref = value
	}
	if ref.ActionID == "" {
		t.Fatal("fixture did not issue its actual worker")
	}
	request, _, directory, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	s.request = request
	s.directory = directory
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	// Native capture resolves this real peer executable and its isolated config.
	t.Setenv("HOME", s.home)
	t.Setenv("CODEX_HOME", s.config)
	t.Setenv("PATH", filepath.Dir(s.home)+string(os.PathListSeparator)+os.Getenv("PATH"))
	binding, err := NativeBinding("codex")
	if err != nil {
		t.Fatal(err)
	}
	s.request["binding"] = binding
	s.peer.Answer = codexPlain(t, map[string]any{"interruptionReason": "The local peer needs an explicit user decision"}).(map[string]any)
	s.runtime = filepath.Join(admission.Target, "bin", "formal-gates")
	s.save(t)
	return f, ref, s, binary
}

func nativePeerTurns(t *testing.T, s *codexScenario) int {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatal(err)
		}
		if value["method"] == "turn/start" {
			count++
		}
	}
	return count
}

func TestEngineNativeRejectsChangedInputsBeforeTurn(t *testing.T) {
	_, _, s, binary := engineNativeFixture(t)
	statePath := filepath.Join(s.workspace, facade.EngineNamespace, "native-execution", "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"attempt", "prompt", "scope", "probe"} {
		t.Run(field, func(t *testing.T) {
			body, err := json.Marshal(s.request)
			if err != nil {
				t.Fatal(err)
			}
			value, err := decodeNativeJSON(body)
			if err != nil {
				t.Fatal(err)
			}
			request := objectOrEmpty(value)
			switch field {
			case "attempt":
				engine := objectOrEmpty(request["engine"])
				material := objectOrEmpty(engine["workerMaterial"])
				material["attemptId"] = "different-attempt"
				engine["workerMaterial"] = material
				request["engine"] = engine
			case "prompt":
				request["prompt"] = "different worker payload"
			case "scope":
				request["scope"] = "FULL"
			case "probe":
				request["probe"] = true
			}
			if err := writeAtomicJSON(filepath.Join(s.directory, "native-request.json"), request); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "harness", "native", s.directory)
			if out, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("changed input accepted: %s", out)
			}
			if nativePeerTurns(t, s) != 0 {
				t.Fatal("rejected input reached the native turn")
			}
			if _, err := os.Stat(filepath.Join(s.directory, "owner")); !os.IsNotExist(err) {
				t.Fatal("rejected input claimed execution", err)
			}
			after, err := os.ReadFile(statePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rejection changed engine facts", err)
			}
		})
	}
}

func TestWorkflowDefaultConsumesNativeAndSingleStepDoesNot(t *testing.T) {
	_, _, s, binary := engineNativeFixture(t, true)
	call := func(args ...string) []byte {
		t.Helper()
		if args[0] == "workflow" {
			args = append(args, "--machine")
		}
		cmd := exec.Command(binary, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("installed %v: %v %s", args, err, out)
		}
		return out
	}
	call("workflow", "drive", "--root", s.workspace, "--run-id", "native-execution", "--single-step")
	if nativePeerTurns(t, s) != 0 {
		t.Fatal("diagnostic step started a model turn")
	}
	body := call("workflow", "drive", "--root", s.workspace, "--run-id", "native-execution")
	var consumed struct {
		Boundary string     `json:"boundary"`
		Run      facade.Run `json:"run"`
	}
	if err := json.Unmarshal(body, &consumed); err != nil {
		t.Fatal(err)
	}
	if nativePeerTurns(t, s) != 1 || consumed.Boundary != "ASK" || len(consumed.Run.PendingAsks) == 0 {
		t.Fatalf("default did not consume then stop for the actual decision: %s", body)
	}
	call("workflow", "drive", "--root", s.workspace, "--run-id", "native-execution")
	call("harness", "native", s.directory)
	if nativePeerTurns(t, s) != 1 {
		t.Fatal("accepted native result was executed again")
	}
	if _, err := os.Stat(filepath.Join(s.directory, "exit.json")); err != nil {
		t.Fatal("original supervisor exit receipt is missing", err)
	}
	if err := os.Remove(filepath.Join(s.directory, "source.json")); err != nil {
		t.Fatal(err)
	}
	call("harness", "native", s.directory)
	if nativePeerTurns(t, s) != 1 {
		t.Fatal("lost response observation started another turn")
	}
}

func TestWorkflowFailedNativeReachesRecoveryAndKeepsOriginalProof(t *testing.T) {
	f, ref, s, binary := engineNativeFixture(t, true)
	s.peer.Mode = "bad_json"
	s.save(t)
	for range 2 {
		out, err := exec.Command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", "native-execution", "--machine").CombinedOutput()
		if err != nil {
			t.Fatalf("failed native observation: %v %s", err, out)
		}
		var response struct {
			Boundary string            `json:"boundary"`
			Run      facade.Run        `json:"run"`
			Blocked  map[string]string `json:"blocked"`
		}
		if err := json.Unmarshal(out, &response); err != nil {
			t.Fatal(err)
		}
		if response.Boundary != "ASK" || len(response.Blocked) != 0 || nativePeerTurns(t, s) != 1 {
			t.Fatalf("completed failure did not reach original recovery once: %s", out)
		}
		verification, err := lifecycle.VerifyDispatch(s.workspace, "native-execution", ref.ActionID)
		if err != nil || !verification.StartObserved || !verification.StopObserved {
			t.Fatalf("original terminal facts absent: %+v %v", verification, err)
		}
		body, digest, identity, err := NativeFailureEvidence("codex", s.directory)
		if err != nil || identity != verification.Identity || len(body) == 0 || digest == "" {
			t.Fatal("original failed invocation proof lost", err)
		}
		entry := response.Run.Cost.Dispatches[ref.ActionID]
		if entry.Source != "unavailable" || entry.Reason == "" {
			t.Fatalf("missing failed-call meter misqualified: %+v", entry)
		}
		matched := false
		for _, record := range response.Run.RecoveryRecords {
			matched = matched || record.ActionID == ref.ActionID && record.Class == authoring.FailureUserActionRequired && record.InterruptionReason != ""
		}
		if !matched {
			t.Fatal("failure class/cause did not reach original recovery owner")
		}
	}
	originalDirectory := s.directory
	originalBody, originalDigest, originalIdentity, err := NativeFailureEvidence("codex", originalDirectory)
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	askPosition := 0
	for i, ask := range run.PendingAsks {
		if ask.Control == protocol.ControlRecovery {
			askPosition = i + 1
		}
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: askPosition, Choice: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = f.SubmitControl(candidate)
	if err != nil {
		t.Fatal(err)
	}
	resume := ""
	for _, intent := range run.AdapterIntents {
		if intent.Resume != nil {
			resume = intent.ActionID
		}
	}
	if resume == "" {
		t.Fatal("original recovery did not sign a resume")
	}
	request, _, directory, err := f.WorkerExecutionRequest(ref, resume)
	if err != nil {
		t.Fatal(err)
	}
	request["binding"] = s.request["binding"]
	s.request, s.directory = request, directory
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	s.save(t)
	for range 2 {
		out, err := exec.Command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", "native-execution", "--machine").CombinedOutput()
		if err != nil {
			t.Fatalf("resumed original failed native observation: %v %s", err, out)
		}
		var response struct {
			Boundary string     `json:"boundary"`
			Run      facade.Run `json:"run"`
		}
		if err := json.Unmarshal(out, &response); err != nil {
			t.Fatal(err)
		}
		if response.Boundary != "ASK" || len(response.Run.PendingAsks) == 0 || nativePeerTurns(t, s) != 2 {
			t.Fatalf("separate same-error resume did not reach its own recovery once: %s", out)
		}
		body, digest, identity, err := NativeFailureEvidence("codex", directory)
		if err != nil || !bytes.Equal(body, originalBody) || digest == originalDigest || identity != originalIdentity {
			t.Fatalf("failure invocation identity lost: %q %s %s, %v", body, digest, identity, err)
		}
	}
	s.directory = originalDirectory
	path := filepath.Join(s.directory, "exit.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := NativeFailureEvidence("codex", s.directory); err == nil {
		t.Fatal("missing terminal receipt became a proved failure")
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.directory, "stderr.log"), []byte("different evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := NativeFailureEvidence("codex", s.directory); err == nil {
		t.Fatal("changed stream became a proved failure")
	}
}

func TestWorkflowInvalidSemanticResultUsesOriginalFiniteAttempt(t *testing.T) {
	_, ref, s, binary := engineNativeFixture(t, true)
	s.peer.Mode = "workflow-invalid-review"
	s.peer.Answer = nil
	s.save(t)
	for range 2 {
		out, err := exec.Command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", "native-execution", "--machine").CombinedOutput()
		if err != nil {
			t.Fatalf("invalid semantic routing: %v %s", err, out)
		}
		var response struct {
			Run      facade.Run        `json:"run"`
			Boundary string            `json:"boundary"`
			Blocked  map[string]string `json:"blocked"`
		}
		if err := json.Unmarshal(out, &response); err != nil {
			t.Fatal(err)
		}
		if response.Boundary != "ASK" || len(response.Blocked) != 0 {
			t.Fatalf("invalid source-verified body remained blocked: %s", out)
		}
		rejected := response.Run.RejectedResults[ref.ActionID]
		if rejected.Status != "REJECTED_INVALID_RESULT" || rejected.AttemptID != ref.AttemptID || !strings.Contains(rejected.Reason, "verification evidence is missing") || rejected.Payload.InvalidReason != rejected.Reason {
			t.Fatalf("original invalid result reason/attempt lost: %+v", rejected)
		}
		exhausted := false
		for _, attempt := range response.Run.EndedAttempts {
			exhausted = exhausted || attempt.RetryExhausted && attempt.Attempts == attempt.MaxAttempts
		}
		if !exhausted {
			t.Fatal("original finite invalid-result policy was not exhausted")
		}
		invalid := 0
		for _, result := range response.Run.RejectedResults {
			if result.Status == "REJECTED_INVALID_RESULT" && strings.Contains(result.Reason, "verification evidence is missing") {
				invalid++
			}
		}
		if invalid != 2 {
			t.Fatalf("both original attempts must be rejected: %d", invalid)
		}
		decision := false
		for _, ask := range response.Run.PendingAsks {
			decision = decision || ask.Control == protocol.ControlValidation && ask.Validation != nil && ask.Validation.Kind == "RETRY"
		}
		if !decision {
			t.Fatal("finite exhaustion did not preserve its typed retry decision")
		}
		if nativePeerTurns(t, s) != 2 {
			t.Fatalf("finite policy did not execute exactly original two attempts: %d", nativePeerTurns(t, s))
		}
	}
	if _, err := NativeJobSource("codex", s.directory); err != nil {
		t.Fatal("invalid semantic source must remain valid transport", err)
	}
}

func TestEngineNativeOriginalJobOwnsConcurrentTurns(t *testing.T) {
	_, _, s, binary := engineNativeFixture(t)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := exec.Command(binary, "harness", "native", s.directory).CombinedOutput()
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	succeeded := false
	for err := range errors {
		succeeded = succeeded || err == nil
	}
	if !succeeded || nativePeerTurns(t, s) != 1 {
		t.Fatal("original job did not own exactly one concurrent native turn")
	}
	if out, err := exec.Command(binary, "harness", "native", s.directory).CombinedOutput(); err != nil {
		t.Fatalf("completed source observation: %v %s", err, out)
	}
	if nativePeerTurns(t, s) != 1 {
		t.Fatal("completed original native source was replayed as another turn")
	}
}

func TestEngineNativeUnknownOwnerNeverStartsTurn(t *testing.T) {
	_, _, s, binary := engineNativeFixture(t)
	if err := os.WriteFile(filepath.Join(s.directory, "owner"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(binary, "harness", "native", s.directory).CombinedOutput(); err == nil || !bytes.Contains(out, []byte("UNKNOWN")) {
		t.Fatalf("unknown original owner: %v %s", err, out)
	}
	if nativePeerTurns(t, s) != 0 {
		t.Fatal("unknown original effect started a native turn")
	}
}

func TestWorkflowReportsRejectedSupervisorBeforeNativeTurn(t *testing.T) {
	f, ref, s, binary := engineNativeFixture(t, true)
	_, request, directory, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	request["environment"] = map[string]any{"UNISSUED": "value"}
	if err := writeAtomicJSON(filepath.Join(directory, "request.json"), request); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", "native-execution", "--machine")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("blocked native response: %v %s", err, out)
	}
	var response struct {
		Boundary string            `json:"boundary"`
		Blocked  map[string]string `json:"blocked"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		t.Fatal(err)
	}
	if response.Boundary != "BLOCKED" || !strings.Contains(response.Blocked[ref.ActionID], "without an exit receipt") {
		t.Fatalf("missing original supervisor failure: %s", out)
	}
	if nativePeerTurns(t, s) != 0 {
		t.Fatal("rejected original supervisor reached a native turn")
	}
	if _, err := os.Stat(filepath.Join(directory, "owner")); !os.IsNotExist(err) {
		t.Fatal("rejected supervisor claimed an effect", err)
	}
}

// Setup accepts explicit review/control fixtures without claiming native calls.
// Only the subsequent developer and gate execute against the actual RPC peer.
func stageNativeReviews(t *testing.T, f *facade.Facade, s *codexScenario, owners ...string) *protocol.GranularityReview {
	t.Helper()
	plan := &protocol.GranularityReview{Reason: "One complete command delivery", VerificationTier: "FINAL", Batches: []protocol.DevelopmentBatch{{ID: "command", Subtasks: []protocol.DevelopmentSubtask{{ID: "implementation", Description: "Deliver app.go"}}, Deliverable: "app.go delivery", PublicEntry: "go run app.go", Artifacts: []string{"app.go"}, Interfaces: "Existing command", StateBoundary: "No persistent state", Risk: "Retain command behavior", DoD: []protocol.DevelopmentCriterion{{ID: "delivery", Description: "Deliver the original app.go task"}}, Checks: []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "go build -o " + filepath.Join(t.TempDir(), "app") + " app.go"}, {ID: "tests", Purpose: protocol.CheckFullTests, Command: "go test -timeout=0 -count=1 app.go"}}, Rollback: "Restore original app.go", Recovery: "Continue original task", Handoff: "Report original delivery", ContextCost: "One file and its behavior"}}}
	if len(owners) > 0 {
		original := plan.Batches[0]
		plan.Batches = nil
		for _, owner := range owners {
			batch := original
			batch.ID, batch.Owner = owner, owner
			batch.Artifacts = []string{owner + ".go"}
			batch.Subtasks = []protocol.DevelopmentSubtask{{ID: owner + "-implementation", Description: "Deliver the confirmed " + owner + " artifact"}}
			plan.Batches = append(plan.Batches, batch)
		}
	}
	for _, role := range []review.Action{review.ProductReview, review.StartReadiness} {
		run, err := f.Drive()
		if err != nil {
			t.Fatal(err)
		}
		var ref facade.WorkerMaterial
		for id, pending := range run.PendingActions {
			if pending.ReviewSeries == role {
				ref = run.WorkerMaterials[id]
			}
		}
		var material worker.Material
		body, err := os.ReadFile(ref.Path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &material); err != nil {
			t.Fatal(err)
		}
		identity := "setup:" + ref.ActionID
		spawn, err := protocol.NewSpawnReceiptEvent(protocol.EventID(identity), ref.ActionID, "codex", identity, protocol.SpawnStatusSpawned)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.Submit(spawn); err != nil {
			t.Fatal(err)
		}
		if err := lifecycle.ObserveNativeCompletion(s.workspace, "native-execution", ref.ActionID, "codex", identity); err != nil {
			t.Fatal(err)
		}
		evidence := protocol.ReviewResultEvidence{Action: role, DispatchID: ref.ActionID, RequirementRevision: material.Input.Review.RequirementRevision, Status: "PASS", Findings: []review.Finding{}, Verification: review.OperatorVerification{Checks: review.RequiredVerificationChecks(), Evidence: "Explicit fixture review for the native continuation scenario"}}
		if role == review.StartReadiness {
			evidence.GranularityReview = plan
		}
		event, err := protocol.NewReviewWorkerResultEvent(protocol.EventID("setup-result:"+ref.ActionID), ref.ActionID, "codex", "sha256:fixture-review", evidence)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.Submit(event); err != nil {
			t.Fatal(err)
		}
	}
	return plan
}

func stageNativeDevelopment(t *testing.T, f *facade.Facade, s *codexScenario) (facade.WorkerMaterial, protocol.WorkerInput) {
	t.Helper()
	stageNativeReviews(t, f, s)
	request, err := protocol.NewRequestEvent("setup-topology", protocol.ControlConfirmTopology, protocol.AskOption{ID: "no-split", Label: "one command"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(request); err != nil {
		t.Fatal(err)
	}
	token, err := f.Freshness(string(request.ID))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := protocol.NewReviewDecideEvent("setup-topology-decision", protocol.RequestID(request.ID), token, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "One command scenario"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(decision); err != nil {
		t.Fatal(err)
	}
	route := protocol.RouteDecision{Mode: "custom", Gates: []string{"blackbox", "whitebox", "implementation-quality-gate"}, Selected: []string{"implementation-quality-gate"}}
	preview, err := f.DevelopmentPlan(route)
	if err != nil {
		t.Fatal(err)
	}
	route.DevelopmentPlan = &preview.Confirmation
	request, err = protocol.NewRouteRequestEvent("setup-route", route.Gates, protocol.AskOption{ID: "custom", Label: "command gate"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(request); err != nil {
		t.Fatal(err)
	}
	token, err = f.Freshness(string(request.ID))
	if err != nil {
		t.Fatal(err)
	}
	decision, err = protocol.NewReviewDecideEvent("setup-route-decision", protocol.RequestID(request.ID), token, "custom", protocol.ReviewDecisionPayload{Route: &route})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(decision); err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	for id, input := range run.ValidationInputs {
		if input.Handler == "engine.development.batch.v3" {
			ref := run.WorkerMaterials[id]
			body, err := os.ReadFile(ref.Path)
			if err != nil {
				t.Fatal(err)
			}
			var material worker.Material
			if err := json.Unmarshal(body, &material); err != nil {
				t.Fatal(err)
			}
			return ref, material.Input
		}
	}
	t.Fatal("fixture did not issue original developer")
	return facade.WorkerMaterial{}, protocol.WorkerInput{}
}

func TestTypedObservationReopensOriginalNativeAcknowledgement(t *testing.T) {
	f, _, s, binary := engineNativeFixture(t, true)
	development, original := stageNativeDevelopment(t, f, s)
	root := fRoot(development)
	submit := func(event protocol.Event) {
		t.Helper()
		if _, _, err := f.Submit(event); err != nil {
			t.Fatal(err)
		}
	}
	spawn, err := protocol.NewSpawnReceiptEvent("development-start", development.ActionID, "codex", "setup-development", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	submit(spawn)
	if err := lifecycle.ObserveNativeCompletion(root, "native-execution", development.ActionID, "codex", "setup-development"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original.Validation.Workspace.Path, "app.go"), []byte("package main\nfunc main() { println(\"delivered\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	completion := &protocol.DevelopmentCompletion{DoD: map[string]string{}, Handoff: "The original command is ready for its approved checks"}
	for _, criterion := range original.Validation.Development.Batch.DoD {
		completion.DoD[criterion.ID] = "Delivered the original command artifact"
	}
	submit(protocol.Event{ID: "development-result", Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: development.ActionID, Provider: "codex", Outcome: protocol.OutcomePass, PayloadDigest: "sha256:development-result", Validation: &protocol.ValidationResult{Binding: original.Validation.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "Delivered the command", Completion: completion}}}})
	var run facade.Run
	var gate string
	for gate == "" {
		run, err = f.Drive()
		if err != nil {
			t.Fatal(err)
		}
		for id, input := range run.ValidationInputs {
			if input.Handler == "engine.gate.v2" {
				gate = id
			}
		}
		if gate == "" {
			time.Sleep(100 * time.Millisecond)
		}
	}
	spawn, err = protocol.NewSpawnReceiptEvent("gate-start", gate, "codex", "setup-gate", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	submit(spawn)
	if err := lifecycle.ObserveNativeCompletion(root, "native-execution", gate, "codex", "setup-gate"); err != nil {
		t.Fatal(err)
	}
	submit(protocol.Event{ID: "gate-failure", Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: gate, Provider: "codex", Outcome: protocol.OutcomePass, PayloadDigest: "sha256:gate-failure", Validation: &protocol.ValidationResult{Binding: run.ValidationInputs[gate].Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "Reported command ordering differs from the confirmed ordering"}}}}})
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	var ref facade.WorkerMaterial
	var observation *protocol.ObservationInput
	for id, input := range run.ValidationInputs {
		if input.Handler == "engine.validation.observation.v2" {
			ref, observation = run.WorkerMaterials[id], input.Observation
		}
	}
	if observation == nil || len(observation.Findings) != 1 {
		t.Fatal("original gate did not issue its actual observation duty", run.ValidationInputs)
	}
	request, supervised, directory, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	s.request, s.directory = request, directory
	s.workspace = request["workspace"].(string)
	binding, err := NativeBinding("codex")
	if err != nil {
		t.Fatal(err)
	}
	s.request["binding"] = binding
	s.peer.Mode = "workflow-held"
	s.peer.Answer = nil
	s.save(t)
	if err := writeAtomicJSON(filepath.Join(directory, "request.json"), supervised); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "harness", "supervise", directory)
	DetachSupervisor(cmd)
	log, err := os.Create(filepath.Join(directory, "supervisor.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { err := cmd.Wait(); log.Close(); finished <- err }()
	t.Cleanup(func() { _ = StopNativeJob(directory) })
	for {
		if _, err := os.Stat(filepath.Join(s.config, "held-turn")); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case err := <-finished:
			t.Fatalf("original native worker exited before its acknowledgement: %v", err)
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
	identity, err := NativeStartEvidence("codex", directory)
	if err != nil {
		t.Fatal(err)
	}
	previous := facade.NativeStartVerifier
	facade.NativeStartVerifier = NativeStartEvidence
	t.Cleanup(func() { facade.NativeStartVerifier = previous })
	f, err = facade.Open(root, "native-execution")
	if err != nil {
		t.Fatal(err)
	}
	resolution := protocol.FindingResolution{FindingID: observation.Findings[0].Finding.ID, Decision: "DISMISSED", Requirement: original.RequirementDocument.Requirements[0].AcceptanceConditions[0].ID, NormalPath: "go run app.go", Module: "command", RootCause: "The report applies an ordering condition absent from the confirmed command contract", Consequence: "The permitted command output was classified as a defect", Severity: "P3", SeverityRationale: "The additional ordering preference is outside the confirmed scope", Verification: review.FindingVerification{Scope: "confirmed command contract", Severity: "no blocked command behavior", Causality: "the report uses an additional ordering condition", Reproduction: "compare the actual command output with the confirmed acceptance", Classification: "the confirmed contract permits this output"}, Reason: "The confirmed requirement permits the reported ordering"}
	submit(protocol.Event{ID: "typed-ordering-fact", Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: observation.WaveID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
	run, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, "native-execution", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Content protocol.State `json:"content"`
	}
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatal(err)
	}
	if run.PendingActions[ref.ActionID].AttemptID != ref.AttemptID || state.Content.SpawnReceipts[ref.ActionID].Correlation != identity || nativePeerTurns(t, s) != 1 {
		t.Fatal("cold acknowledgement lost the actual native worker", state.Content.SpawnReceipts)
	}
	if err := f.ObserveWorkerStart(ref, directory, NativeStartEvidence); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.config, "release-turn"), []byte("continue"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal("original acknowledged worker did not finish", err)
	}
	source, err := NativeJobSource("codex", directory)
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := f.SubmitWorkerResult("late-native-observation", ref, directory, source, NativeWorkerResultEvidence)
	if err != nil || accepted.Status != "OBSOLETE_RESULT" {
		t.Fatal("actual late native source bypassed obsolete admission", accepted, err)
	}
	if err := f.ObserveWorkerTerminal(ref, identity); err != nil {
		t.Fatal(err)
	}
	for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
		event, err := protocol.NewLifecycleEventEvent(protocol.EventID("observed:"+edge), "codex", identity, edge)
		if err != nil {
			t.Fatal(err)
		}
		submit(event)
	}
	run, err = f.Show()
	if err != nil || run.PendingActions[ref.ActionID].ActionID != "" {
		t.Fatal("verified native completion did not retire its old task", err)
	}
}

func TestWorkflowContinuesOriginalNativeTaskUntilComplete(t *testing.T) {
	f, _, s, binary := engineNativeFixture(t, true)
	ref, input := stageNativeDevelopment(t, f, s)
	request, _, directory, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	s.request = request
	s.directory = directory
	s.workspace = input.Validation.Workspace.Path
	binding, err := NativeBinding("codex")
	if err != nil {
		t.Fatal(err)
	}
	s.request["binding"] = binding
	s.peer.Mode = "workflow-progress"
	s.peer.WorkerAction = ref.ActionID
	s.peer.WorkerAttempt = ref.AttemptID
	s.peer.Answer = nil
	s.peer.Configuration["sandbox"] = map[string]any{"type": "workspaceWrite"}
	s.save(t)
	cmd := exec.Command(binary, "workflow", "drive", "--root", fRoot(ref), "--run-id", "native-execution", "--machine")
	out, err := cmd.CombinedOutput()
	if err := os.WriteFile(filepath.Join(s.config, "consumer.json"), out, 0600); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatalf("automatic continuation: %v %s", err, out)
	}
	var response struct {
		Boundary string            `json:"boundary"`
		Run      facade.Run        `json:"run"`
		Blocked  map[string]string `json:"blocked"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		t.Fatal(err)
	}
	if response.Boundary != "ASK" || len(response.Blocked) != 0 {
		t.Fatalf("original continuation did not reach its actual next decision: %s", out)
	}
	actionRoot := filepath.Dir(directory)
	entries, err := os.ReadDir(actionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("wanted two partials and complete in one action: %d", len(entries))
	}
	identity := ""
	digests := map[string]bool{}
	for _, entry := range entries {
		job := filepath.Join(actionRoot, entry.Name())
		source, err := NativeJobSource("codex", job)
		if err != nil {
			t.Fatal(err)
		}
		nativeIdentity := source["identity"].(string)
		if identity != "" && identity != nativeIdentity {
			t.Fatal("continuation replaced its original native identity")
		}
		identity = nativeIdentity
		var req map[string]any
		body, err := os.ReadFile(filepath.Join(job, "native-request.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		material := objectOrEmpty(getOf(getOf(req, "engine"), "workerMaterial"))
		if material["actionId"] != ref.ActionID || material["attemptId"] != ref.AttemptID {
			t.Fatal("continuation changed original action or attempt")
		}
		digests[textOf(material["digest"])] = true
	}
	if len(digests) != 3 || nativePeerTurns(t, s) != 4 {
		t.Fatalf("native task progress or independent gate counts differ: materials=%d turns=%d", len(digests), nativePeerTurns(t, s))
	}
	var envelope struct {
		Content protocol.State `json:"content"`
	}
	stateBody, err := os.ReadFile(filepath.Join(fRoot(ref), facade.EngineNamespace, "native-execution", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stateBody, &envelope); err != nil {
		t.Fatal(err)
	}
	usage, err := cost.ParseTranscript("codex", filepath.Join(s.config, identity+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	entry, recorded := envelope.Content.Cost.Dispatches[ref.ActionID]
	if !recorded || entry.InputCacheMissTokens != usage.InputCacheMissTokens || entry.InputCacheHitTokens != usage.InputCacheHitTokens || entry.OutputTokens != usage.OutputTokens {
		t.Fatalf("original dispatch cost lost native continuation usage: ledger=%+v actual=%+v", entry, usage)
	}
	for _, record := range response.Run.RecoveryRecords {
		if record.ActionID == ref.ActionID {
			t.Fatal("normal unfinished work was routed as a recovery attempt")
		}
	}
	t.Logf("original_action=%s original_attempt=%s native_identity=%s material_count=%d real_turns=%d", ref.ActionID, ref.AttemptID, identity, len(digests), nativePeerTurns(t, s))
}

func TestWorkflowCarriesOriginalLiveNativeTask(t *testing.T) {
	f, _, s, binary := engineNativeFixture(t, true)
	ref, input := stageNativeDevelopment(t, f, s)
	request, _, directory, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	s.request, s.directory, s.workspace = request, directory, input.Validation.Workspace.Path
	s.request["binding"], err = NativeBinding("codex")
	if err != nil {
		t.Fatal(err)
	}
	s.peer.Mode, s.peer.WorkerAction, s.peer.WorkerAttempt, s.peer.Answer = "workflow-held", ref.ActionID, ref.AttemptID, nil
	s.peer.Configuration["sandbox"] = map[string]any{"type": "workspaceWrite"}
	s.save(t)
	cmd := exec.Command(binary, "workflow", "drive", "--root", fRoot(ref), "--run-id", "native-execution", "--machine")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	state := func() protocol.State {
		t.Helper()
		var envelope struct {
			Content protocol.State `json:"content"`
		}
		body, err := os.ReadFile(filepath.Join(fRoot(ref), facade.EngineNamespace, "native-execution", "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Content
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	release := func() {
		if err := os.WriteFile(filepath.Join(s.config, "release-turn"), nil, 0600); err != nil {
			t.Error(err)
		}
	}
	defer release()
	for {
		select {
		case err := <-done:
			t.Fatalf("consumer ended before original held turn: %v %s", err, out.Bytes())
		default:
		}
		if state().SpawnReceipts[ref.ActionID].Status == protocol.SpawnStatusSpawned {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	document := canaryRequirementDocument + "\n"
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	var choices []protocol.RequirementWorkDecision
	for _, work := range run.RequirementWork {
		choices = append(choices, protocol.RequirementWorkDecision{Work: work, Disposition: protocol.RequirementWorkCarry, Reason: "Formatting preserves this original duty"})
	}
	impact := &protocol.RequirementImpact{ChangedItems: []string{"document formatting"}, TransitiveImpacts: []string{}, CarryReasons: []string{"same original inputs and obligations"}}
	pkg := protocol.RequirementDecisionPackage{Impact: impact, CandidateRevision: revision, DeltaDigest: "sha256:formatting", ImpactDigest: "sha256:unchanged-work", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: choices}
	event, err := protocol.NewRequirementRequestEvent("held-requirement", pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = f.Submit(event)
	if err != nil {
		t.Fatal(err)
	}
	for {
		run, err = f.Show()
		if err != nil {
			t.Fatal(err)
		}
		ask := run.PendingRequirementAsk
		if ask == nil {
			t.Fatal("original requirement ask missing")
		}
		_, _, err = f.Submit(protocol.Event{ID: "held-confirmation", Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Impact: impact, BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: choices, Reason: "Explicitly carry every unchanged original duty"}}})
		if err == nil {
			break
		}
		var rejected *protocol.RejectedError
		if !errors.As(err, &rejected) || rejected.Code != protocol.CodeStaleFreshness {
			t.Fatal(err)
		}
	}
	_, run, err = f.Submit(protocol.Event{ID: "held-document", Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
	if err != nil {
		t.Fatal(err)
	}
	carried, ok := run.WorkerMaterials[ref.ActionID]
	if !ok || carried.AttemptID != ref.AttemptID || carried.Digest == ref.Digest {
		t.Fatal("explicit carry did not preserve logical identity with new material")
	}
	// Keep the actual host turn active across foreground polling of the carried input.
	time.Sleep(300 * time.Millisecond)
	if !NativeJobAlive(directory) {
		t.Fatal("original turn ended before carry was exercised")
	}
	release()
	if err := <-done; err != nil {
		t.Fatalf("carried consumer: %v %s", err, out.Bytes())
	}
	var response struct {
		Boundary string            `json:"boundary"`
		Run      facade.Run        `json:"run"`
		Blocked  map[string]string `json:"blocked"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Boundary != "ASK" || len(response.Blocked) != 0 {
		t.Fatalf("carried original task blocked: %s", out.Bytes())
	}
	entries, err := os.ReadDir(filepath.Dir(directory))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("carried live logical duty started another invocation: %d", len(entries))
	}
	source, err := NativeJobSource("codex", directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := NativeResultEvidence("codex", directory, source); err != nil {
		t.Fatal(err)
	}
	if state().Results[ref.ActionID].Outcome != protocol.OutcomePass {
		t.Fatal("original carried result not accepted")
	}
	if nativePeerTurns(t, s) != 2 {
		t.Fatalf("original developer plus independent gate turns=%d", nativePeerTurns(t, s))
	}
	if _, err := exec.Command(binary, "workflow", "drive", "--root", fRoot(ref), "--run-id", "native-execution", "--machine").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	if nativePeerTurns(t, s) != 2 {
		t.Fatal("repeat drive replayed carried native task")
	}
	t.Logf("action=%s attempt=%s material_before=%s material_after=%s original_identity=%s", ref.ActionID, ref.AttemptID, ref.Digest, carried.Digest, source["identity"])
}

func fRoot(ref facade.WorkerMaterial) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(ref.Path)))))
}

func TestWorkflowExecutesOnlyOriginalSignedResumeAndTermination(t *testing.T) {
	for _, scenario := range []struct {
		name, choice string
		failed       bool
	}{{"resume", "resume", false}, {"abort", "abort", false}, {"failed-resume", "resume", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			choice := scenario.choice
			f, _, s, binary := engineNativeFixture(t, true)
			if scenario.failed {
				s.peer.Mode = "failed"
				s.save(t)
			}
			call := func(input []byte, args ...string) []byte {
				t.Helper()
				if args[0] == "workflow" {
					args = append(args, "--machine")
				}
				cmd := exec.Command(binary, args...)
				cmd.Stdin = bytes.NewReader(input)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("installed %v: %v %s", args, err, out)
				}
				return out
			}
			call(nil, "workflow", "drive", "--root", s.workspace, "--run-id", "native-execution")
			run, err := f.Show()
			if err != nil {
				t.Fatal(err)
			}
			askPosition := 0
			for index, pending := range run.PendingAsks {
				if pending.Control == protocol.ControlRecovery {
					askPosition = index + 1
				}
			}
			if askPosition == 0 {
				t.Fatal("native interruption produced no original recovery ask")
			}
			candidate, err := f.PrepareControl(facade.ControlInput{Ask: askPosition, Choice: choice})
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			confirmation := filepath.Join(s.config, "recovery-confirmation.json")
			if err := os.WriteFile(confirmation, body, 0600); err != nil {
				t.Fatal(err)
			}
			call(nil, "workflow", "submit", "--root", s.workspace, "--run-id", "native-execution", "--confirmation", confirmation, "--single-step")
			run, err = f.Show()
			if err != nil {
				t.Fatal(err)
			}
			var intent protocol.HostActionIntent
			for _, signed := range run.AdapterIntents {
				if choice == "resume" && signed.Resume != nil || choice == "abort" && signed.Terminate != nil {
					intent = signed
				}
			}
			if intent.ActionID == "" && choice == "resume" {
				t.Fatal("decision did not sign its original native responsibility")
			}
			if choice == "resume" {
				ref := run.WorkerMaterials[intent.Resume.WorkerActionID]
				request, _, directory, err := f.WorkerExecutionRequest(ref, intent.ActionID)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				request["binding"], err = NativeBinding("codex")
				if err != nil {
					t.Fatal(err)
				}
				s.request = request
				s.directory = directory
				s.peer.Mode = ""
				s.peer.Answer = codexPlain(t, map[string]any{"interruptionReason": "The original resumed task needs another user decision"}).(map[string]any)
				s.save(t)
			}
			out := call(nil, "workflow", "drive", "--root", s.workspace, "--run-id", "native-execution")
			var response struct {
				Run      facade.Run        `json:"run"`
				Boundary string            `json:"boundary"`
				Blocked  map[string]string `json:"blocked"`
			}
			if err := json.Unmarshal(out, &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Blocked) > 0 {
				t.Fatalf("signed original effect did not execute: %s", out)
			}
			want := 1
			if choice == "resume" {
				want = 2
			}
			if nativePeerTurns(t, s) != want {
				t.Fatalf("signed %s duplicated or lost native turns: %d", choice, nativePeerTurns(t, s))
			}
			if choice == "abort" && response.Run.Status != "ABORTED" {
				t.Fatalf("termination did not finish original abort: %s", out)
			}
			if choice == "resume" && response.Boundary != "ASK" {
				t.Fatalf("resumed original task lost its next decision: %s", out)
			}
			call(nil, "workflow", "drive", "--root", s.workspace, "--run-id", "native-execution")
			if nativePeerTurns(t, s) != want {
				t.Fatal("completed native responsibility was executed twice")
			}
		})
	}
}

func TestWorkflowCancelsOriginalAcknowledgedNativeTurn(t *testing.T) {
	f, ref, s, binary := engineNativeFixture(t, true)
	s.peer.Mode = "wait"
	s.save(t)
	cmd := exec.Command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", "native-execution", "--machine")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	// The actual host acknowledgement must be recorded before abort can target it.
	for {
		select {
		case err := <-done:
			t.Fatalf("consumer ended before native acknowledgement: %v %s", err, out.Bytes())
		default:
		}
		var envelope struct {
			Content protocol.State `json:"content"`
		}
		body, err := os.ReadFile(filepath.Join(s.workspace, facade.EngineNamespace, "native-execution", "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		if receipt := envelope.Content.SpawnReceipts[ref.ActionID]; receipt.Status == protocol.SpawnStatusSpawned {
			observed := false
			for _, event := range envelope.Content.LifecycleEvents {
				observed = observed || event.Identity == receipt.Correlation && event.Event == protocol.LifecycleStart
			}
			if observed {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	request, err := protocol.NewRequestEvent("native-active-abort", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort this active test task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(request); err != nil {
		t.Fatal(err)
	}
	token, err := f.Freshness(string(request.ID))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := protocol.NewDecideEvent("native-active-abort-decision", protocol.RequestID(request.ID), token, "abort")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(decision); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("original native cancellation: %v %s", err, out.Bytes())
	}
	var response struct {
		Run     facade.Run        `json:"run"`
		Blocked map[string]string `json:"blocked"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Run.Status != "ABORTED" || len(response.Blocked) != 0 || nativePeerTurns(t, s) != 1 {
		t.Fatalf("original cancellation did not complete exactly once: %s", out.Bytes())
	}
	if NativeJobAlive(s.directory) {
		t.Fatal("original native supervisor still runs")
	}
	var receipt map[string]any
	raw, err := os.ReadFile(filepath.Join(s.directory, "exit.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["exit_code"] != float64(130) {
		t.Fatalf("original cancellation receipt lost actual process outcome: %s", raw)
	}
}

func TestNativeStartEvidenceUsesActualAcknowledgement(t *testing.T) {
	scenarios := map[string]func(*testing.T) string{
		"codex": func(t *testing.T) string {
			s := newCodexScenario(t)
			s.save(t)
			if _, err := s.execute(t); err != nil {
				t.Fatal(err)
			}
			return s.directory
		},
		"dsh": func(t *testing.T) string {
			s := newDSHScenario(t, successSteps(), func(request map[string]any) { request["model"] = "test-model"; request["reasoning_effort"] = "high" })
			if _, err := runNativeJob(s.directory); err != nil {
				t.Fatal(err)
			}
			return s.directory
		},
		"zcode": func(t *testing.T) string {
			s := newZcodeScenario(t, zcodeSuccessSteps, func(request map[string]any) { request["read_only"] = false })
			if _, err := runNativeJob(s.directory); err != nil {
				t.Fatal(err)
			}
			return s.directory
		},
		"claude": func(t *testing.T) string {
			s := newClaudeScenario(t, "session-a", true)
			if _, err := runNativeJob(s.directory); err != nil {
				t.Fatal(err)
			}
			return s.directory
		},
		"cursor": func(t *testing.T) string {
			s := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", true)
			if _, err := runNativeJob(s.directory); err != nil {
				t.Fatal(err)
			}
			return s.directory
		},
	}
	for provider, scenario := range scenarios {
		t.Run(provider, func(t *testing.T) {
			directory := scenario(t)
			source, err := NativeJobSource(provider, directory)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := NativeStartEvidence(provider, directory)
			if err != nil {
				t.Fatalf("actual acknowledgement rejected: %v", err)
			}
			if identity != source["identity"] {
				t.Fatal("native start and completed source identities differ")
			}
			start, err := readJSONFile(filepath.Join(directory, "identity.json"))
			if err != nil {
				t.Fatal(err)
			}
			delete(objectOrEmpty(start), "acknowledgement")
			if err := writeAtomicJSON(filepath.Join(directory, "identity.json"), start); err != nil {
				t.Fatal(err)
			}
			if _, err := NativeStartEvidence(provider, directory); err == nil {
				t.Fatal("identity file alone replaced the actual native acknowledgement")
			}
		})
	}
}

func TestWorkflowConsumesReleasedChildAndShowsOriginalPlan(t *testing.T) {
	f, _, s, binary := engineNativeFixture(t, true)
	plan := stageNativeReviews(t, f, s, "core", "tooling", "master-merge")
	run, err := f.Show()
	if err != nil || !reflect.DeepEqual(run.Review.ReadinessProposal, plan) {
		t.Fatal("original accepted readiness proposal is not public", err)
	}
	decide := func(owner *facade.Facade, kind protocol.ControlKind, choice string, payload protocol.ReviewDecisionPayload) {
		t.Helper()
		ask, err := protocol.NewRequestEvent(protocol.EventID("child-choice:"+choice), kind, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
		if kind == protocol.ControlSelectRoute {
			ask, err = protocol.NewRouteRequestEvent(protocol.EventID("child-choice:"+choice), payload.Route.Gates, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := owner.Submit(ask); err != nil {
			t.Fatal(err)
		}
		token, err := owner.Freshness(string(ask.ID))
		if err != nil {
			t.Fatal(err)
		}
		event, err := protocol.NewReviewDecideEvent(protocol.EventID("child-decision:"+choice), protocol.RequestID(ask.ID), token, protocol.AskOptionID(choice), payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := owner.Submit(event); err != nil {
			t.Fatal(err)
		}
	}
	topology := &protocol.TopologyDecision{Decision: "split", SplitCount: 2, Slices: []string{"core", "tooling"}, Parallel: "Two independent deliveries", ACOwners: []protocol.ACOwner{{AC: "AC-001", Owner: "master-merge"}, {AC: "AC-002", Owner: "master-merge"}}}
	preview, err := f.DevelopmentPlan(protocol.RouteDecision{}, *topology)
	if err != nil {
		t.Fatal(err)
	}
	topology.DevelopmentPlan = &preview.Confirmation
	decide(f, protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: topology})
	route := protocol.RouteDecision{Mode: "custom", Gates: []string{"blackbox", "whitebox", "implementation-quality-gate"}, Selected: []string{"implementation-quality-gate"}}
	s.peer.Mode = "workflow-decision"
	s.peer.Answer = nil
	s.peer.Configuration["sandbox"] = map[string]any{"type": "workspaceWrite"}
	s.save(t)
	call := func() map[string]any {
		t.Helper()
		cmd := exec.Command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", "native-execution", "--machine")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("automatic released children: %v %s", err, out)
		}
		var response map[string]any
		if err := json.Unmarshal(out, &response); err != nil {
			t.Fatal(err)
		}
		blocked := objectOrEmpty(response["blocked"])
		for _, child := range objectOrEmpty(response["children"]) {
			for id, reason := range objectOrEmpty(getOf(child, "blocked")) {
				blocked[id] = reason
			}
		}
		if len(blocked) != 0 {
			filepath.WalkDir(filepath.Join(s.workspace, facade.EngineNamespace), func(path string, entry os.DirEntry, err error) error {
				if err == nil && !entry.IsDir() && (entry.Name() == "stderr.log" || entry.Name() == "supervisor.log" || entry.Name() == "native-error.json") {
					body, _ := os.ReadFile(path)
					t.Logf("original_native_failure=%s %s", path, body)
				}
				if err == nil && !entry.IsDir() && entry.Name() == "source.json" {
					_, observeErr := NativeJobSource("codex", filepath.Dir(path))
					t.Logf("original_completed_source=%s verifier=%v", path, observeErr)
				}
				return err
			})
		}
		return response
	}
	response := call()
	children := objectOrEmpty(response["children"])
	if response["boundary"] != "ASK" || len(children) != 2 || nativePeerTurns(t, s) != 1 {
		t.Logf("original turns=%d parent asks=%s", nativePeerTurns(t, s), textOf(getOf(response["run"], "pendingAsks")))
		t.Fatalf("children lost their own original route decisions: %s", textOf(response))
	}
	run, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	child, err := facade.Open(s.workspace, run.SliceChildren["core"])
	if err != nil {
		t.Fatal(err)
	}
	childRun, err := child.Show()
	if err != nil || len(childRun.PendingAsks) != 1 || childRun.PendingAsks[0].Control != protocol.ControlSelectRoute {
		t.Fatal("child did not retain its typed route Ask", err)
	}
	childRoute := protocol.RouteDecision{Mode: "custom", Gates: childRun.PendingAsks[0].RouteGates, Selected: route.Selected}
	preview, err = child.DevelopmentPlan(childRoute)
	if err != nil {
		t.Fatal(err)
	}
	childRoute.DevelopmentPlan = &preview.Confirmation
	ask := childRun.PendingAsks[0]
	event, err := protocol.NewReviewDecideEvent("approve-child-route", protocol.RequestID(ask.RequestID), ask.Token, "custom", protocol.ReviewDecisionPayload{Route: &childRoute})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := child.Submit(event); err != nil {
		t.Fatal(err)
	}
	s.peer.Mode = "workflow-decision"
	s.peer.Answer = nil
	s.peer.Configuration["sandbox"] = map[string]any{"type": "workspaceWrite"}
	s.save(t)
	response = call()
	children = objectOrEmpty(response["children"])
	core := objectOrEmpty(children[run.SliceChildren["core"]])
	if response["boundary"] != "ASK" || getOf(core, "boundary") != "ASK" || nativePeerTurns(t, s) != 2 {
		t.Fatalf("parent did not consume the original released child once: %s", textOf(response))
	}
	if childRun, err = child.Show(); err != nil || len(childRun.PendingAsks) == 0 {
		t.Fatal("native result did not reach the original child decision", err)
	}
	var envelope struct {
		Content protocol.State `json:"content"`
	}
	body, err := os.ReadFile(filepath.Join(s.workspace, facade.EngineNamespace, childRun.RunID, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	accepted := false
	for id, receipt := range envelope.Content.SpawnReceipts {
		if receipt.Status != protocol.SpawnStatusSpawned {
			continue
		}
		material := childRun.WorkerMaterials[id]
		actionDir := filepath.Dir(facade.WorkerExecutionDirectory(material, ""))
		jobs, err := os.ReadDir(actionDir)
		if err != nil || len(jobs) != 1 {
			t.Fatal("child original invocation evidence differs", err)
		}
		directory := filepath.Join(actionDir, jobs[0].Name())
		var request struct {
			Engine struct {
				WorkerMaterial facade.WorkerMaterial `json:"workerMaterial"`
			} `json:"engine"`
		}
		body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		if request.Engine.WorkerMaterial.ActionID != id || request.Engine.WorkerMaterial.AttemptID != material.AttemptID {
			t.Fatal("child source lost its original action/attempt")
		}
		source, err := NativeJobSource("codex", directory)
		if err != nil {
			t.Fatal(err)
		}
		if source["identity"] != receipt.Correlation || source["provider"] != "codex" {
			t.Fatal("child receipt differs from actual native source")
		}
		_, digest, _, err := NativeResultEvidence("codex", directory, source)
		if err != nil {
			t.Fatal(err)
		}
		cause := "The gate peer needs an explicit user decision"
		resultMatched := false
		for _, result := range envelope.Content.RecoverableResults[id] {
			resultMatched = resultMatched || result.PayloadDigest == digest && result.FailureClass == authoring.FailureUserActionRequired && result.InterruptionReason == cause
		}
		askMatched := false
		for _, ask := range childRun.PendingAsks {
			if ask.Control != protocol.ControlRecovery {
				continue
			}
			for _, record := range envelope.Content.RecoveryRecords {
				if record.ActionID == id && record.RequestID == ask.RequestID && record.Class == authoring.FailureUserActionRequired && record.InterruptionReason == cause {
					askMatched = true
				}
			}
			projected, _ := json.Marshal(getOf(getOf(core, "run"), "pendingAsks"))
			if !strings.Contains(string(projected), ask.RequestID) {
				t.Fatal("parent concealed the original child request")
			}
		}
		if !resultMatched || !askMatched {
			t.Fatal("child Ask lost its actual inability cause/result/request lineage")
		}
		accepted = true
	}
	if !accepted {
		t.Fatal("child has no actual native spawn/source owner")
	}
	call()
	if nativePeerTurns(t, s) != 2 {
		t.Fatal("parent replay duplicated child native work")
	}
}
