//go:build !windows

package harness

// These cases run the installed command against the original local app-server
// peer. Its RPC log counts original turn/start requests; it never starts an installed host.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
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
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/requirement"
	"formal-gates/internal/settings"
	"formal-gates/internal/validate"
)

func engineNativeFixture(t *testing.T, installed ...bool) (*facade.Facade, facade.WorkerMaterial, *codexScenario, string) {
	t.Helper()
	return engineNativeFixtureWith(t, len(installed) > 0 && installed[0], nil)
}

func engineNativeFixtureWith(t *testing.T, installed bool, qaModes []string) (*facade.Facade, facade.WorkerMaterial, *codexScenario, string) {
	t.Helper()
	// Independent Codex adapter calls require a different controlling platform.
	const controller = host.WorkBuddy
	s := newCodexScenario(t)
	s.runID = "native-execution"
	project := s.workspace
	const document = "# 命令交付\n\n## 需求点\n\n### REQ-001：完成消息\n\n#### 要求\n公开执行 app.go 时退出 0，输出一行非空完成文本；不约束文本内容或顺序。\n\n#### 验收条件\n- AC-001：go run app.go 退出 0，并输出一行非空完成文本。\n\n#### 来源\n原生命令夹具的执行职责。\n\n## 方案\n在 app.go 的 main 中输出完成消息。\n"
	if err := os.WriteFile(filepath.Join(project, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "app.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".gitignore"), []byte(".gates/\n.codebuddy/\nCODEBUDDY.md\n"), 0600); err != nil {
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
	registry, target, generation, lease, token := testkit.Installation(t, project, "native-target", "sha256:native-package", controller)
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
	if err := os.Link(supervisorBinary(t), binary); err != nil {
		t.Fatal(err)
	}
	admission := facade.Admission{RegistryPath: registry, Target: target, Root: project, Provider: controller, PackageDigest: "sha256:native-package", InstalledTargetIdentity: "native-target", Generation: generation, Lease: lease, Token: token}
	if installed {
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
		binary = filepath.Join(s.home, ".local", "bin", "formal-gates")
		options := validate.InstallOptions{Source: candidate, Host: controller, Scope: "project", Project: project, BinaryTarget: binary, RegistryPath: filepath.Join(s.home, ".formal-gates", "registry.json"), Force: true, SkipHooks: true}
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
		admission = facade.Admission{RegistryPath: report.Registry, Target: record.Target, Root: project, Provider: controller, PackageDigest: record.PackageDigest, InstalledTargetIdentity: record.ID, Generation: record.Generation, Lease: record.Lease, Token: record.Token}
	}
	if err := writeAtomicJSON(filepath.Join(project, ".gates", "config.json"), map[string]any{"capacity": 2, "codexExecutable": filepath.Join(filepath.Dir(s.home), "codex")}); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.SaveConfiguration(project, s.home, "project", "命令", settings.DefaultConfigurationName, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(project, s.home, "project", "命令", nil, func(configuration *settings.Configuration) error {
		gates := []string{"implementation-quality-gate"}
		configuration.Gates = &gates
		configuration.Execution = map[string]settings.ExecutionChoice{}
		for _, node := range definition.ExecutionNodes() {
			configuration.Execution[node.ID] = settings.ExecutionChoice{Provider: host.Codex}
		}
		if len(qaModes) > 0 {
			configuration.QAModes = slices.Clone(qaModes)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(project, s.home, "project", "命令", nil); err != nil {
		t.Fatal(err)
	}
	c := engineCanary{t: t, doc: []byte(document)}
	var f *facade.Facade
	if installed {
		started, startErr := confirmInstalledFixtureStart(t, s.command(binary, "workflow", "serve"), project, document)
		if startErr != nil {
			t.Fatalf("original installed start through the simulated MCP host: %v", startErr)
		}
		s.runID = started.RunID
		f, err = facade.Open(project, s.runID)
	} else {
		effective, loadErr := settings.Load(project, s.home)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		effective.Values.Native = s.nativeSettings().Native
		f, _, err = facade.Start(facade.StartOptions{Root: project, Request: facade.StartRequest{RunID: s.runID, VCS: "git", Provider: controller, BaseSnapshot: base, DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: c.confirmedIntake()}, Admission: &admission, Settings: &effective, Capacity: 2})
	}
	if err != nil {
		t.Fatal(err)
	}
	var start facade.StartRequest
	startBody, err := os.ReadFile(filepath.Join(project, facade.EngineNamespace, s.runID, "request.json"))
	if err != nil || json.Unmarshal(startBody, &start) != nil || start.Provider != controller {
		t.Fatal("independent native fixture lost its original controller", start, err)
	}
	f, err = facade.Open(project, s.runID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.Configuration.Name != "命令" || len(run.Configuration.Configuration.Execution) != len(definition.ExecutionNodes()) {
		t.Fatal("reopened native fixture lost its complete saved execution configuration", run.Configuration)
	}
	for _, node := range definition.ExecutionNodes() {
		if choice := run.Configuration.Configuration.Execution[node.ID]; choice != (settings.ExecutionChoice{Provider: host.Codex}) {
			t.Fatal("reopened native fixture changed its worker platform", node.ID, choice)
		}
	}
	var ref facade.WorkerMaterial
	for _, value := range run.WorkerMaterials {
		ref = value
	}
	if ref.ActionID == "" {
		t.Fatal("fixture did not issue its actual worker")
	}
	request, _, directory, _, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if request["provider"] != host.Codex || request["provider"] == start.Provider {
		t.Fatal("independent native fixture did not retain distinct controller and worker platforms", start.Provider, request["provider"])
	}
	s.request = request
	s.directory = directory
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	// Native capture resolves this real peer executable and its isolated config.
	binding, err := NativeBinding("codex", s.nativeSettings())
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
	t.Parallel()
	_, _, s, binary := engineNativeFixture(t)
	statePath := filepath.Join(s.workspace, facade.EngineNamespace, s.runID, "state.json")
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
			cmd := s.command(binary, "harness", "native", s.directory)
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
	t.Parallel()
	_, _, s, binary := engineNativeFixture(t, true)
	call := func(args ...string) []byte {
		t.Helper()
		if args[0] == "workflow" {
			args = append(args, "--machine")
		}
		cmd := s.command(binary, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("installed %v: %v %s", args, err, out)
		}
		return out
	}
	call("workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--single-step")
	if nativePeerTurns(t, s) != 0 {
		t.Fatal("diagnostic step started a model turn")
	}
	body := call("workflow", "drive", "--root", s.workspace, "--run-id", s.runID)
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
	call("workflow", "drive", "--root", s.workspace, "--run-id", s.runID)
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
	t.Parallel()
	f, ref, s, binary := engineNativeFixture(t, true)
	s.peer.Mode = "bad_json"
	s.save(t)
	for range 2 {
		out, err := s.command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--machine").CombinedOutput()
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
		verification, err := lifecycle.VerifyDispatch(s.workspace, s.runID, ref.ActionID)
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
	_, run, err = confirmFacadeFixtureControl(t, f, candidate)
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
	request, _, directory, _, err := f.WorkerExecutionRequest(ref, resume)
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
		out, err := s.command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--machine").CombinedOutput()
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
	t.Parallel()
	_, ref, s, binary := engineNativeFixture(t, true)
	s.peer.Mode = "workflow-invalid-review"
	s.peer.Answer = nil
	s.save(t)
	for range 2 {
		out, err := s.command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--machine").CombinedOutput()
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
		if rejected.Status != "REJECTED_INVALID_RESULT" || rejected.AttemptID != ref.AttemptID || !strings.Contains(rejected.Reason, "severity must be P0-P3 and message non-empty") || rejected.Payload.Review == nil || len(rejected.Payload.Review.Findings) != 1 || rejected.Payload.Review.Findings[0].Severity != "P9" || rejected.Payload.InvalidReason != "" {
			t.Fatalf("original invalid result reason/attempt lost: %+v", rejected)
		}
		exhausted := false
		for _, attempt := range response.Run.EndedAttempts {
			exhausted = exhausted || attempt.RetryExhausted && attempt.Attempts == attempt.MaxAttempts
		}
		if !exhausted {
			t.Fatalf("original finite invalid-result policy was not exhausted: ended=%+v attempts=%+v asks=%+v", response.Run.EndedAttempts, response.Run.Attempts, response.Run.PendingAsks)
		}
		invalid := 0
		for _, result := range response.Run.RejectedResults {
			if result.Status == "REJECTED_INVALID_RESULT" && strings.Contains(result.Reason, "severity must be P0-P3 and message non-empty") {
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
	t.Parallel()
	_, _, s, binary := engineNativeFixture(t)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.command(binary, "harness", "native", s.directory).CombinedOutput()
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
	if out, err := s.command(binary, "harness", "native", s.directory).CombinedOutput(); err != nil {
		t.Fatalf("completed source observation: %v %s", err, out)
	}
	if nativePeerTurns(t, s) != 1 {
		t.Fatal("completed original native source was replayed as another turn")
	}
}

func TestEngineNativeUnknownOwnerNeverStartsTurn(t *testing.T) {
	t.Parallel()
	_, _, s, binary := engineNativeFixture(t)
	if err := os.WriteFile(filepath.Join(s.directory, "owner"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := s.command(binary, "harness", "native", s.directory).CombinedOutput(); err == nil || !bytes.Contains(out, []byte("UNKNOWN")) {
		t.Fatalf("unknown original owner: %v %s", err, out)
	}
	if nativePeerTurns(t, s) != 0 {
		t.Fatal("unknown original effect started a native turn")
	}
}

func TestNativeIdentityInheritanceUsesOnlyClaimedInvocations(t *testing.T) {
	t.Parallel()
	for _, claimed := range []bool{false, true} {
		t.Run(fmt.Sprintf("claimed=%t", claimed), func(t *testing.T) {
			root := t.TempDir()
			prior, current := filepath.Join(root, "prepared"), filepath.Join(root, "selected")
			for _, directory := range []string{prior, current} {
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
			}
			request := map[string]any{"provider": "zcode", "engine": map[string]any{"workerMaterial": map[string]any{"actionId": "original-action", "attemptId": "original-attempt"}}}
			if err := writeAtomicJSON(filepath.Join(prior, "native-request.json"), request); err != nil {
				t.Fatal(err)
			}
			if claimed {
				if err := os.WriteFile(filepath.Join(prior, "owner"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			impl, err := nativeImplementationFor("zcode")
			if err != nil {
				t.Fatal(err)
			}
			err = prepareNativeInvocation(current, request, impl)
			if claimed {
				if err == nil || !strings.Contains(err.Error(), "original native invocation is UNKNOWN") {
					t.Fatalf("claimed unknown invocation was replaced: %v", err)
				}
				if _, err := os.Stat(filepath.Join(current, "owner")); !os.IsNotExist(err) {
					t.Fatal("blocked invocation acquired ownership", err)
				}
			} else if err != nil {
				t.Fatal("unclaimed preparation blocked the selected invocation", err)
			} else if _, err := os.Stat(filepath.Join(current, "owner")); err != nil {
				t.Fatal("selected invocation did not acquire ownership", err)
			}
		})
	}
}

func TestWorkflowReportsRejectedSupervisorBeforeNativeTurn(t *testing.T) {
	t.Parallel()
	f, ref, s, binary := engineNativeFixture(t, true)
	_, request, directory, _, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	request["environment"] = map[string]any{"UNISSUED": "value"}
	if err := writeAtomicJSON(filepath.Join(directory, "request.json"), request); err != nil {
		t.Fatal(err)
	}
	cmd := s.command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--machine")
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
	return stageNativeReviewsWithEdit(t, f, s, "", true, owners...)
}

func stageNativeReviewsWithEdit(t *testing.T, f *facade.Facade, s *codexScenario, command string, split bool, owners ...string) *protocol.GranularityReview {
	t.Helper()
	plan := &protocol.GranularityReview{Reason: "One complete command delivery", VerificationTier: "FINAL", Batches: []protocol.DevelopmentBatch{{ID: "command", Subtasks: []protocol.DevelopmentSubtask{{ID: "implementation", Description: "Deliver app.go"}}, Deliverable: "app.go delivery", PublicEntry: "go run app.go", Artifacts: []string{"app.go"}, Interfaces: "Existing command", StateBoundary: "No persistent state", Risk: "Retain command behavior", DoD: []protocol.DevelopmentCriterion{{ID: "delivery", Description: "Deliver the original app.go task"}}, Checks: []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "go build -o " + filepath.Join(t.TempDir(), "app") + " app.go"}, {ID: "tests", Purpose: protocol.CheckFullTests, Command: "go test -timeout=0 -count=1 app.go"}}, Rollback: "Restore original app.go", Recovery: "Continue original task", Handoff: "Report original delivery", ContextCost: "One file and its behavior"}}}
	if command != "" {
		plan.Batches[0].Checks = append(plan.Batches[0].Checks, protocol.DevelopmentCheck{ID: "edit", Purpose: protocol.CheckEdit, Command: command})
	}
	if len(owners) > 0 {
		original := plan.Batches[0]
		plan.Batches = nil
		for _, owner := range owners {
			batch := original
			batch.ID, batch.Owner = owner, owner
			if !split {
				batch.Owner = ""
			}
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
		if material.Input.Pending.ActionID != ref.ActionID || material.Input.Provider != "codex" || material.Input.Review == nil || material.Input.Review.Action != role || material.Input.Intake == nil || material.Input.Review.RequirementRevision != material.Input.Intake.RequirementRevision || material.Input.RequirementDocument.Content != strings.ReplaceAll(string(readTestFileBytes(t, filepath.Join(s.workspace, material.Input.Intake.RequirementSource))), "\r\n", "\n") {
			t.Fatal("synthetic review differs from its original action and complete confirmed input")
		}
		identity := "setup:" + ref.ActionID
		spawn, err := protocol.NewSpawnReceiptEvent(protocol.EventID(identity), ref.ActionID, "codex", identity, protocol.SpawnStatusSpawned)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.Submit(spawn); err != nil {
			t.Fatal(err)
		}
		if err := lifecycle.ObserveNativeCompletion(s.workspace, s.runID, ref.ActionID, "codex", identity); err != nil {
			t.Fatal(err)
		}
		evidence := protocol.ReviewResultEvidence{Action: role, DispatchID: ref.ActionID, RequirementRevision: material.Input.Review.RequirementRevision, Status: "PASS", Findings: []review.Finding{}}
		if role == review.StartReadiness {
			for i := range plan.Batches {
				if plan.Batches[i].Owner == "" || plan.Batches[i].Owner == "master-merge" {
					plan.Batches[i].RequirementPoints = requirement.PointIDs(material.Input.RequirementPoints)
				}
			}
			evidence.GranularityReview = plan
		}
		event, err := protocol.NewReviewWorkerResultEvent(protocol.EventID("setup-result:"+ref.ActionID), ref.ActionID, "codex", "sha256:fixture-review", evidence)
		if err != nil {
			t.Fatal(err)
		}
		accepted, run, err := f.Submit(event)
		if err != nil || accepted.Status != "ACCEPTED" {
			t.Fatal("original synthetic review was rejected", accepted, err)
		}
		if len(run.ReviewVerifications) != 1 || run.ReviewVerifications[0].Review == nil || !reflect.DeepEqual(*run.ReviewVerifications[0].Review, evidence) || len(evidence.Findings) != 0 {
			t.Fatal("independent actor did not receive the exact original zero-finding fixture", run.ReviewVerifications)
		}
		if role == review.StartReadiness {
			if err := protocol.ValidateGranularity(plan); err != nil {
				t.Fatal(err)
			}
			if err := protocol.ValidateProjectChecks(plan); err != nil {
				t.Fatal(err)
			}
		}
		candidate, err := f.PrepareControl(facade.ControlInput{ReviewVerification: &facade.ReviewVerificationInput{Result: 1, Findings: []protocol.ReviewFindingVerification{}, Verification: []string{
			"Compared the original Codex action and current confirmed revision in its exact issued material.",
			"The original synthetic report has no findings; readiness preserves complete finite duties and passes project-check validation.",
			"Read the complete original confirmed document and compared the retained raw report with the submitted fixture.",
			"The original zero-finding report has no locations; its complete confirmed source bytes were compared directly.",
			"The fixture's native Git input contains app.go; its declared command entry and component artifacts supply the normal responsibility.",
			"Readiness assigns the original points to command or master integration while component DoD remains explicit.",
			"The checked content concerns the issued product or readiness action and its confirmed input only.",
			"The original report has zero findings, so no severity or user disposition is introduced.",
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if accepted, _, err := confirmFacadeFixtureControl(t, f, candidate); err != nil || accepted.Status != "ACCEPTED" {
			t.Fatal("independent synthetic review acceptance failed", accepted, err)
		}
	}
	return plan
}

func stageNativeDevelopment(t *testing.T, f *facade.Facade, s *codexScenario) (facade.WorkerMaterial, protocol.WorkerInput) {
	t.Helper()
	stageNativeReviews(t, f, s)
	return stageNativeDevelopmentReady(t, f, s)
}

func stageNativeDevelopmentReady(t *testing.T, f *facade.Facade, s *codexScenario) (facade.WorkerMaterial, protocol.WorkerInput) {
	t.Helper()
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
	if requested, err := f.RequestWorkflowChoice(); err != nil || !requested {
		t.Fatal("engine did not issue its actual initial route Ask", err)
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.DevelopmentPlan == nil || !slices.Equal(candidate.Event.Decide.Review.Route.Selected, []string{"implementation-quality-gate"}) {
		t.Fatal("initial route lost the original development plan or bound gate")
	}
	if _, _, err := confirmFacadeFixtureControl(t, f, candidate); err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	for id, input := range run.ValidationInputs {
		if input.Handler == "engine.development.batch.v6" {
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
	if testkit.RunNativePackageTest(t) {
		return
	}
	f, _, s, binary := engineNativeFixture(t, true)
	development, original := stageNativeDevelopment(t, f, s)
	root := fRoot(development)
	submit := func(event protocol.Event) {
		t.Helper()
		if accepted, _, err := f.Submit(event); err != nil || accepted.Status != "ACCEPTED" && accepted.Status != "DUPLICATE" {
			t.Fatal("original native setup event was rejected", accepted, err)
		}
	}
	spawn, err := protocol.NewSpawnReceiptEvent("development-start", development.ActionID, "codex", "setup-development", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	submit(spawn)
	if err := lifecycle.ObserveNativeCompletion(root, s.runID, development.ActionID, "codex", "setup-development"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original.Validation.Workspace.Path, "app.go"), []byte("package main\nfunc main() { println(\"delivered\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	completion := &protocol.DevelopmentCompletion{DoD: map[string]string{}, Handoff: "The original command is ready for its approved checks"}
	if len(original.RequirementPoints) != 1 || original.RequirementPoints[0].ID != "AC-001" || original.RequirementPoints[0].Text != "go run app.go 退出 0，并输出一行非空完成文本。" {
		t.Fatal("native fixture received another requirement duty", original.RequirementPoints)
	}
	command := exec.Command("go", "run", "app.go")
	command.Dir = original.Validation.Workspace.Path
	output, err := command.CombinedOutput()
	if err != nil || len(bytes.Split(bytes.TrimSpace(output), []byte{'\n'})) != 1 || len(bytes.TrimSpace(output)) == 0 {
		t.Fatal("actual native setup command did not satisfy its single duty", err, string(output))
	}
	completion.RequirementResults = []requirement.PointResult{{PointID: original.RequirementPoints[0].ID, Status: "PASS", Evidence: "go run app.go exited 0 and returned one completion line: " + string(output)}}
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
			if input.Handler == "engine.gate.v3" {
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
	if err := lifecycle.ObserveNativeCompletion(root, s.runID, gate, "codex", "setup-gate"); err != nil {
		t.Fatal(err)
	}
	submit(protocol.Event{ID: "gate-failure", Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: gate, Provider: "codex", Outcome: protocol.OutcomePass, PayloadDigest: "sha256:gate-failure", Validation: &protocol.ValidationResult{Binding: run.ValidationInputs[gate].Binding, Status: "FAIL", RequirementResults: []protocol.RequirementDecision{{PointID: original.RequirementPoints[0].ID, Status: "FAIL", Evidence: "The synthetic gate report asserts an additional ordering constraint for the observed completion line", FindingIndexes: []int{0}}}, Findings: []review.Finding{{Severity: "P2", Requirement: original.RequirementPoints[0].ID, Message: "Reported command ordering differs from the confirmed ordering"}}}}})
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
	request, supervised, directory, _, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	s.request, s.directory = request, directory
	s.workspace = request["workspace"].(string)
	binding, err := NativeBinding("codex", s.nativeSettings())
	if err != nil {
		t.Fatal(err)
	}
	s.request["binding"] = binding
	resolution := protocol.FindingResolution{FindingID: observation.Findings[0].Finding.ID, Decision: "DISMISSED", Requirement: original.RequirementDocument.Requirements[0].AcceptanceConditions[0].ID, NormalPath: "go run app.go", Module: "command", RootCause: "The report applies an ordering condition absent from the confirmed command contract", Consequence: "The permitted command output was classified as a defect", Severity: "P3", SeverityRationale: "The additional ordering preference is outside the confirmed scope", Verification: review.FindingVerification{Scope: "confirmed command contract", Severity: "no blocked command behavior", Causality: "the report uses an additional ordering condition", Reproduction: "compare the actual command output with the confirmed acceptance", Classification: "the confirmed contract permits this output"}, Reason: "The confirmed requirement permits the reported ordering"}
	judgments := worker.ObservationJudgments{Findings: []worker.FindingJudgment{{Decision: resolution.Decision, Requirement: resolution.Requirement, NormalPath: resolution.NormalPath, Module: resolution.Module, RootCause: resolution.RootCause, Consequence: resolution.Consequence, Severity: resolution.Severity, SeverityRationale: resolution.SeverityRationale, Verification: resolution.Verification, Reason: resolution.Reason}}, Scopes: []worker.ScopeJudgment{}, GateImpacts: []worker.ImpactJudgment{}}
	s.peer.Mode = "workflow-held"
	s.peer.Answer = codexPlain(t, judgments).(map[string]any)
	s.peer.WaitAfterSubmission = true
	s.save(t)
	if err := writeAtomicJSON(filepath.Join(directory, "request.json"), supervised); err != nil {
		t.Fatal(err)
	}
	cmd := s.command(binary, "harness", "supervise", directory)
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
	if evidence := os.Getenv("CODEX_NATIVE_TEST_EVIDENCE"); evidence != "" {
		t.Cleanup(func() {
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Error(err)
				return
			}
			target := filepath.Join(evidence, t.Name()+"_native")
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Error(err)
				return
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				body, err := os.ReadFile(filepath.Join(directory, entry.Name()))
				if err != nil {
					t.Error(err)
					continue
				}
				if err := os.WriteFile(filepath.Join(target, entry.Name()), body, 0644); err != nil {
					t.Error(err)
				}
			}
		})
	}
	for {
		if _, err := os.Stat(filepath.Join(s.config, "held-turn")); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case err := <-finished:
			failure, _ := os.ReadFile(filepath.Join(directory, "native-error.json"))
			stderr, _ := os.ReadFile(filepath.Join(directory, "stderr.log"))
			t.Fatalf("original native worker exited before its acknowledgement: %v %s %s", err, failure, stderr)
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
	identity, err := NativeStartEvidence("codex", directory)
	if err != nil {
		t.Fatal(err)
	}
	held, err := os.ReadFile(filepath.Join(s.config, "held-turn"))
	if err != nil {
		t.Fatal(err)
	}
	ids := strings.Split(string(held), "\n")
	if len(ids) != 2 || ids[0] != identity {
		t.Fatal("held completion lost its original native identity", string(held), identity)
	}
	calls, err := os.ReadFile(filepath.Join(directory, nativeToolCallsFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(calls), []byte{'\n'})
	if len(lines) != 1 {
		t.Fatal("held invocation did not submit exactly one original semantic call", string(calls))
	}
	var call map[string]any
	if err := json.Unmarshal(lines[0], &call); err != nil {
		t.Fatal(err)
	}
	params := getOf(getOf(call, "request"), "params")
	if getOf(getOf(call, "request"), "method") != "item/tool/call" || getOf(params, "tool") != nativeSemanticTool || getOf(params, "threadId") != ids[0] || getOf(params, "turnId") != ids[1] || !valuesEqual(getOf(params, "arguments"), s.peer.Answer) || getOf(getOf(call, "result"), "success") != true {
		t.Fatal("held marker preceded the successful original semantic RPC", string(calls))
	}
	if _, err := os.Stat(filepath.Join(directory, "exit.json")); !os.IsNotExist(err) {
		t.Fatal("held turn already had a terminal process receipt", err)
	}
	beforeFacts, err := os.ReadFile(filepath.Join(filepath.Dir(directory), "tool-facts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	previous := facade.NativeStartVerifier
	facade.NativeStartVerifier = NativeStartEvidence
	t.Cleanup(func() { facade.NativeStartVerifier = previous })
	f, err = facade.Open(root, s.runID)
	if err != nil {
		t.Fatal(err)
	}
	submit(protocol.Event{ID: "typed-ordering-fact", Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: observation.WaveID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
	run, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, s.runID, "state.json"))
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
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: facade.AdmitExecution})
	if err != nil {
		t.Fatal(err)
	}
	semanticBody, err := json.Marshal(s.peer.Answer)
	if err != nil {
		t.Fatal(err)
	}
	for _, invocation := range []struct {
		name string
		body json.RawMessage
	}{
		{nativeSemanticTool, semanticBody},
		{nativeWorkspaceTool, json.RawMessage(`{"action":"list"}`)},
		{nativeWorkspaceTool, json.RawMessage(`{"action":"create","name":"retired.txt","content":"unissued work"}`)},
	} {
		if _, err := tools.invoke(invocation.name, invocation.body); err == nil || !strings.Contains(err.Error(), "no current issued action/attempt") {
			t.Fatal("retired worker admitted a new tool operation", invocation.name, err)
		} else {
			t.Logf("retired %s %s rejected: %s", invocation.name, invocation.body, err)
		}
	}
	afterFacts, err := os.ReadFile(filepath.Join(filepath.Dir(directory), "tool-facts.jsonl"))
	if err != nil || !bytes.Equal(beforeFacts, afterFacts) {
		t.Fatal("retired tool rejection altered the original completion evidence", err)
	}
	if _, err := os.Stat(filepath.Join(s.workspace, "retired.txt")); !os.IsNotExist(err) {
		t.Fatal("retired invocation wrote a workspace artifact", err)
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
	evidence, err := NativeWorkerResultEvidence("codex", directory, source)
	if err != nil || !bytes.Equal(evidence.Answer, semanticBody) || source["identity"] != identity || nativePeerTurns(t, s) != 1 {
		t.Fatal("late completion changed its original semantic source or invoked new work", evidence, err)
	}
	recovered, err := NativeJobSource("codex", directory)
	if err != nil || !valuesEqual(source, recovered) {
		t.Fatal("recovery changed the original late native source", err)
	}
	recoveredEvidence, err := NativeWorkerResultEvidence("codex", directory, recovered)
	if err != nil || !bytes.Equal(evidence.Answer, recoveredEvidence.Answer) || evidence.Digest != recoveredEvidence.Digest {
		t.Fatal("recovery changed original completion bytes or digest", err)
	}
	accepted, _, err := f.SubmitWorkerResult("late-native-observation", ref, directory, source, NativeWorkerResultEvidence)
	if err != nil || accepted.Status != "OBSOLETE_RESULT" {
		t.Fatal("actual late native source bypassed obsolete admission", accepted, err)
	}
	t.Logf("original action=%s attempt=%s identity=%s semanticDigest=%s calls=%d result=%s", ref.ActionID, ref.AttemptID, identity, evidence.Digest, nativePeerTurns(t, s), accepted.Status)
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
	t.Parallel()
	f, _, s, binary := engineNativeFixture(t, true)
	ref, input := stageNativeDevelopment(t, f, s)
	request, _, directory, _, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	s.request = request
	s.directory = directory
	s.workspace = input.Validation.Workspace.Path
	binding, err := NativeBinding("codex", s.nativeSettings())
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
	cmd := s.command(binary, "workflow", "drive", "--root", fRoot(ref), "--run-id", s.runID, "--machine")
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
	entries = slices.DeleteFunc(entries, func(entry os.DirEntry) bool { return !entry.IsDir() })
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
	stateBody, err := os.ReadFile(filepath.Join(fRoot(ref), facade.EngineNamespace, s.runID, "state.json"))
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
	t.Parallel()
	f, _, s, binary := engineNativeFixture(t, true)
	ref, input := stageNativeDevelopment(t, f, s)
	request, _, directory, _, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	s.request, s.directory, s.workspace = request, directory, input.Validation.Workspace.Path
	s.request["binding"], err = NativeBinding("codex", s.nativeSettings())
	if err != nil {
		t.Fatal(err)
	}
	s.peer.Mode, s.peer.WorkerAction, s.peer.WorkerAttempt, s.peer.Answer = "workflow-held", ref.ActionID, ref.AttemptID, nil
	s.peer.Configuration["sandbox"] = map[string]any{"type": "workspaceWrite"}
	s.save(t)
	cmd := s.command(binary, "workflow", "drive", "--root", fRoot(ref), "--run-id", s.runID, "--machine")
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
		body, err := os.ReadFile(filepath.Join(fRoot(ref), facade.EngineNamespace, s.runID, "state.json"))
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
	documentPath := filepath.Join(fRoot(ref), filepath.FromSlash(input.Intake.RequirementSource))
	documentBytes, err := os.ReadFile(documentPath)
	if err != nil {
		t.Fatal(err)
	}
	documentBytes = append(documentBytes, '\n')
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(documentPath, documentBytes, 0600); err != nil {
		t.Fatal(err)
	}
	intake, err := facade.PrepareIntake(fRoot(ref), input.Intake.RequirementSource, input.Intake.SolutionSource, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var choices []facade.WorkChoice
	for _, work := range run.RequirementWork {
		choices = append(choices, facade.WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "Formatting preserves this original duty " + work.ActionID})
	}
	impact := &protocol.RequirementImpact{ChangedItems: []string{"document formatting"}, TransitiveImpacts: []string{}, CarryReasons: []string{"same original inputs and obligations"}}
	change, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlRequestRequirementChange, Requirement: &facade.RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Work: choices, Impact: *impact}})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = confirmFacadeFixtureControl(t, f, change)
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
		askPosition := slices.IndexFunc(run.PendingAsks, func(pending facade.PendingAskFace) bool { return pending.RequestID == ask.RequestID }) + 1
		confirmation, err := f.PrepareControl(facade.ControlInput{Ask: askPosition, Choice: "proceed", Reason: "Explicitly carry every unchanged original duty"})
		if err != nil {
			t.Fatal(err)
		}
		_, run, err = confirmFacadeFixtureControl(t, f, confirmation)
		if err == nil {
			break
		}
		var rejected *protocol.RejectedError
		if !errors.As(err, &rejected) || rejected.Code != protocol.CodeStaleFreshness {
			t.Fatal(err)
		}
	}
	carried, ok := run.WorkerMaterials[ref.ActionID]
	if !ok || carried.AttemptID != ref.AttemptID || carried.Digest == ref.Digest {
		t.Fatal("explicit carry did not preserve logical identity with new material")
	}
	confirmedBytes, err := os.ReadFile(documentPath)
	if err != nil || !bytes.Equal(confirmedBytes, documentBytes) {
		t.Fatal("confirmed carry changed its original source document", err)
	}
	materialBytes, err := os.ReadFile(carried.Path)
	if err != nil {
		t.Fatal(err)
	}
	var material worker.Material
	if err := json.Unmarshal(materialBytes, &material); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(material.Input.RequirementDocument, *intake.RequirementDocument) || !reflect.DeepEqual(material.Input.Intake.Artifacts, intake.IntakeCandidate.Artifacts) || !reflect.DeepEqual(material.Input.Materials, intake.Materials) {
		t.Fatal("carried material differs from its confirmed source document", material.Input.Intake.Artifacts, material.Input.Materials)
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
	// The requirement Ask returns foreground control while the original job
	// runs. Its confirmed carry is collected by the same public consumer.
	collected, err := s.command(binary, "workflow", "drive", "--root", fRoot(ref), "--run-id", s.runID, "--machine").CombinedOutput()
	if err != nil {
		t.Fatalf("collect original carried job: %v %s", err, collected)
	}
	out.Reset()
	out.Write(collected)
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
	entries = slices.DeleteFunc(entries, func(entry os.DirEntry) bool { return !entry.IsDir() })
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
	if _, err := s.command(binary, "workflow", "drive", "--root", fRoot(ref), "--run-id", s.runID, "--machine").CombinedOutput(); err != nil {
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
	t.Parallel()
	for _, scenario := range []struct {
		name, choice string
		failed       bool
	}{{"resume", "resume", false}, {"abort", "abort", false}, {"failed-resume", "resume", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
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
				cmd := s.command(binary, args...)
				cmd.Stdin = bytes.NewReader(input)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("installed %v: %v %s", args, err, out)
				}
				return out
			}
			call(nil, "workflow", "drive", "--root", s.workspace, "--run-id", s.runID)
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
			var original facade.ControlCandidate
			if err := json.Unmarshal(body, &original); err != nil {
				t.Fatal(err)
			}
			if _, err := confirmInstalledFixtureControl(t, s.command(binary, "workflow", "serve"), s.workspace, s.runID, original); err != nil {
				t.Fatal(err)
			}
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
				request, _, directory, _, err := f.WorkerExecutionRequest(ref, intent.ActionID)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				request["binding"], err = NativeBinding("codex", s.nativeSettings())
				if err != nil {
					t.Fatal(err)
				}
				s.request = request
				s.directory = directory
				s.peer.Mode = ""
				s.peer.Answer = codexPlain(t, map[string]any{"interruptionReason": "The original resumed task needs another user decision"}).(map[string]any)
				s.save(t)
			}
			out := call(nil, "workflow", "drive", "--root", s.workspace, "--run-id", s.runID)
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
			call(nil, "workflow", "drive", "--root", s.workspace, "--run-id", s.runID)
			if nativePeerTurns(t, s) != want {
				t.Fatal("completed native responsibility was executed twice")
			}
		})
	}
}

func TestWorkflowCancelsOriginalAcknowledgedNativeTurn(t *testing.T) {
	t.Parallel()
	f, ref, s, binary := engineNativeFixture(t, true)
	s.peer.Mode = "wait"
	s.save(t)
	cmd := s.command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--machine")
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
		body, err := os.ReadFile(filepath.Join(s.workspace, facade.EngineNamespace, s.runID, "state.json"))
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
	if err := <-done; err != nil {
		t.Fatalf("original foreground Ask: %v %s", err, out.Bytes())
	}
	var foreground struct {
		Boundary string     `json:"boundary"`
		Run      facade.Run `json:"run"`
	}
	if err := json.Unmarshal(out.Bytes(), &foreground); err != nil || foreground.Boundary != "ASK" || foreground.Run.RunID != s.runID || !NativeJobAlive(s.directory) {
		t.Fatalf("original Ask lost the live acknowledged turn: %v %s", err, out.Bytes())
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "abort"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := confirmInstalledFixtureControl(t, s.command(binary, "workflow", "serve"), s.workspace, s.runID, candidate); err != nil {
		t.Fatal(err)
	}
	completed, err := s.command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--machine").CombinedOutput()
	if err != nil {
		t.Fatalf("original native cancellation: %v %s", err, completed)
	}
	out.Reset()
	out.Write(completed)
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
	t.Parallel()
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
	t.Parallel()
	// Merge QA exists only while a QA mode is enabled, so this split scenario enables blackbox to keep its integration QA duty.
	f, _, s, binary := engineNativeFixtureWith(t, true, []string{"blackbox"})
	plan := stageNativeReviews(t, f, s, "core", "tooling", "master-merge")
	run, err := f.Show()
	if err != nil || !reflect.DeepEqual(run.Review.ReadinessProposal, plan) {
		t.Fatal("original accepted readiness proposal is not public", err)
	}
	decide := func(owner *facade.Facade, kind protocol.ControlKind, choice string, payload protocol.ReviewDecisionPayload) {
		t.Helper()
		ask, err := protocol.NewRequestEvent(protocol.EventID("child-choice:"+choice), kind, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
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
	topology := &protocol.TopologyDecision{Decision: "split", SplitCount: 2, Slices: []string{"core", "tooling"}, Parallel: "Two independent deliveries", ACOwners: []protocol.ACOwner{{AC: "AC-001", Owner: "master-merge"}}}
	preview, err := f.DevelopmentPlan(protocol.RouteDecision{}, *topology)
	if err != nil {
		t.Fatal(err)
	}
	topology.DevelopmentPlan = &preview.Confirmation
	decide(f, protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: topology})
	s.peer.Mode = "workflow-held"
	s.peer.Answer = nil
	s.peer.Configuration["sandbox"] = map[string]any{"type": "workspaceWrite"}
	s.save(t)
	startJob := func(owner *facade.Facade, material facade.WorkerMaterial) string {
		t.Helper()
		request, supervised, directory, _, err := owner.WorkerExecutionRequest(material, "")
		if err != nil {
			t.Fatal(err)
		}
		request["binding"], err = NativeBinding("codex", s.nativeSettings())
		if err != nil {
			t.Fatal(err)
		}
		s.request, s.directory = request, directory
		s.save(t)
		if err := writeAtomicJSON(filepath.Join(directory, "request.json"), supervised); err != nil {
			t.Fatal(err)
		}
		command := s.command(binary, "harness", "supervise", directory)
		DetachSupervisor(command)
		journal, err := os.Create(filepath.Join(directory, "supervisor.log"))
		if err != nil {
			t.Fatal(err)
		}
		command.Stderr = journal
		if err := command.Start(); err != nil {
			journal.Close()
			t.Fatal(err)
		}
		finished := make(chan error, 1)
		go func() { err := command.Wait(); journal.Close(); finished <- err }()
		t.Cleanup(func() {
			if err := os.WriteFile(filepath.Join(s.config, "release-turn"), nil, 0600); err != nil {
				t.Error(err)
			}
			if err := <-finished; err != nil {
				t.Error("original supervisor", err)
			}
		})
		return directory
	}
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	var merge facade.WorkerMaterial
	for id, input := range run.ValidationInputs {
		if input.Handler == "engine.qa.design.v6" && input.Manifest.Binding.ReviewKind == "MERGE" {
			merge = run.WorkerMaterials[id]
		}
	}
	if merge.ActionID == "" {
		t.Fatal("original independent merge duty was not issued")
	}
	mergeDirectory := startJob(f, merge)
	for {
		if _, err := NativeStartEvidence("codex", mergeDirectory); err == nil {
			break
		} else if _, exitErr := os.Stat(filepath.Join(mergeDirectory, "exit.json")); exitErr == nil {
			t.Fatal("original merge job ended without its acknowledgement", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	call := func() map[string]any {
		t.Helper()
		cmd := s.command(binary, "workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--machine")
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
	if response["boundary"] != "ASK" || len(children) != 2 {
		t.Logf("original turns=%d parent asks=%s", nativePeerTurns(t, s), textOf(getOf(response["run"], "pendingAsks")))
		t.Fatalf("children lost their own original route decisions: %s", textOf(response))
	}
	run, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if !NativeJobAlive(mergeDirectory) || nativePeerTurns(t, s) != 1 {
		t.Fatal("child Ask stopped the original independent parent duty")
	}
	if err := os.WriteFile(filepath.Join(s.config, "release-turn"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := NativeJobSource("codex", mergeDirectory); err == nil {
			break
		} else if _, exitErr := os.Stat(filepath.Join(mergeDirectory, "exit.json")); exitErr == nil {
			t.Fatal("original merge job ended without its source", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	call()
	child, err := facade.Open(s.workspace, run.SliceChildren["core"])
	if err != nil {
		t.Fatal(err)
	}
	childRun, err := child.Show()
	if err != nil || len(childRun.PendingAsks) != 1 || childRun.PendingAsks[0].Control != protocol.ControlSelectRoute {
		t.Fatal("child did not retain its typed route Ask", err)
	}
	ask := childRun.PendingAsks[0]
	candidate, err := child.PrepareControl(facade.ControlInput{Ask: 1, Choice: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.DevelopmentPlan == nil || candidate.Event.Decide.Request != protocol.RequestID(ask.RequestID) || candidate.Event.Decide.Token != ask.Token ||
		candidate.Run.Configuration.Name != run.Configuration.Name || !slices.Equal(candidate.Event.Decide.Review.Route.Selected, []string{"implementation-quality-gate"}) {
		t.Fatal("child route lost its original Ask, plan or inherited configuration")
	}
	if _, _, err := confirmFacadeFixtureControl(t, child, candidate); err != nil {
		t.Fatal(err)
	}
	s.peer.Mode = "workflow-decision"
	s.peer.Answer = nil
	s.peer.Configuration["sandbox"] = map[string]any{"type": "workspaceWrite"}
	s.save(t)
	childRun, err = child.Drive()
	if err != nil {
		t.Fatal(err)
	}
	for _, material := range childRun.WorkerMaterials {
		directory := startJob(child, material)
		for {
			if _, err := NativeJobSource("codex", directory); err == nil {
				break
			} else if _, exitErr := os.Stat(filepath.Join(directory, "exit.json")); exitErr == nil {
				t.Fatal("original child job ended without its source", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
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
		if err != nil {
			t.Fatal("child original invocation evidence differs", err)
		}
		jobs = slices.DeleteFunc(jobs, func(entry os.DirEntry) bool { return !entry.IsDir() })
		if len(jobs) != 1 {
			t.Fatal("child original invocation count differs", len(jobs))
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

func TestWorkflowSupplementaryDeliveryRetainsAcknowledgementAfterResultFailure(t *testing.T) {
	t.Parallel()
	f, ref, s, binary := engineNativeFixture(t, true)
	call := func(args ...string) []byte {
		t.Helper()
		cmd := s.command(binary, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		t.Logf("argv=%v exit=%v stdout=%s stderr=%s", cmd.Args, err, stdout.Bytes(), stderr.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		return stdout.Bytes()
	}
	s.peer.Mode = "bad_json"
	s.save(t)
	call("workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--machine")
	run, err := f.Show()
	if err != nil || len(run.WorkerMaterials) != 1 {
		t.Fatalf("original worker: %+v %v", run.WorkerMaterials, err)
	}
	text := "Keep this exact supplementary text.\n原文和空格  "
	var message protocol.TaskMessage
	raw := call("workflow", "message", "--root", s.workspace, "--run-id", s.runID, "--worker", "1", "--text", text)
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatal(err)
	}
	ask := 0
	for i, request := range run.PendingAsks {
		if request.Control == protocol.ControlRecovery {
			ask = i + 1
		}
	}
	if ask == 0 {
		t.Fatal("original recovery Ask missing")
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Ask: ask, Choice: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = confirmFacadeFixtureControl(t, f, candidate)
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
		t.Fatal("original resume intent missing")
	}
	request, _, directory, original, err := f.WorkerExecutionRequest(ref, resume)
	if err != nil {
		t.Fatal(err)
	}
	request["binding"] = s.request["binding"]
	s.request, s.directory = request, directory
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	s.save(t)
	effective := map[string]any{}
	for k, v := range request {
		effective[k] = v
	}
	effective["prompt"] = request["prompt"].(string) + "\n\n[Supplementary user message]\n" + text
	s.peer.TurnParams, err = codexTurnParameters(effective, "")
	if err != nil {
		t.Fatal(err)
	}
	delete(s.peer.TurnParams, "threadId")
	if err := writeAtomicJSON(filepath.Join(s.config, "peer.json"), s.peer); err != nil {
		t.Fatal(err)
	}
	call("workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--machine")
	source, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil || getOf(getOf(source, "turn_ack"), "id") == nil {
		t.Fatalf("original turn acknowledgement missing: %v %v", source, err)
	}
	expected, err := codexTurnParameters(effective, message.Identity)
	if err != nil || !valuesEqual(getOf(source, "turn_request"), expected) {
		t.Fatalf("supplementary input differs: %v %v", getOf(source, "turn_request"), err)
	}
	body, _, identity, err := NativeFailureEvidence("codex", directory)
	if err != nil || identity != message.Identity || !bytes.Contains(body, []byte("NATIVE_RESULT: current invocation has no submitted semantic tool evidence")) {
		t.Fatalf("original native error changed: %s %s %v", body, identity, err)
	}
	check := func() {
		t.Helper()
		current, err := f.Show()
		actual := current.TaskMessages[message.ID]
		if err != nil || actual.Status != "ACCEPTED" || actual.Reason != "" || actual.ActionID != original.ActionID || actual.AttemptID != original.AttemptID || actual.Identity != message.Identity || actual.Text != text || actual.Revision != message.Revision || nativePeerTurns(t, s) != 2 {
			t.Fatalf("delivery facts: %+v %v", actual, err)
		}
	}
	check()
	f, err = facade.Open(s.workspace, s.runID)
	if err != nil {
		t.Fatal(err)
	}
	call("workflow", "drive", "--root", s.workspace, "--run-id", s.runID, "--machine")
	check()
}
