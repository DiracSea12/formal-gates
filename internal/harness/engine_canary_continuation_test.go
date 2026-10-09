//go:build !windows

package harness

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

// A stopped developer turn resumes through the same native executor. The
// accepted result and its lifecycle transcript select the original source;
// output directories are evidence identities, not another turn counter.
func (c *engineCanary) developmentCallSource(action, workspace string) (string, string, any) {
	root := c.jobDirectory(action)
	input := c.run.ValidationInputs[action]
	if input.Development == nil || input.Development.Result == nil {
		return root, "", nil
	}
	result := input.Development.Result
	matched := false
	for _, work := range c.run.RequirementWork {
		matched = matched || work.ActionID == action && work.ResultDigest == result.Digest
	}
	if !matched {
		c.fail("development continuation differs from current result")
	}
	provider, transcript, err := lifecycle.DispatchTranscriptPath(c.project, c.currentRunID(), action)
	if err != nil || provider != c.provider() || transcript == "" {
		c.fail("development continuation has no original lifecycle source: %v", err)
	}
	directory := filepath.Dir(transcript)
	if prior := result.SourceResult; prior != "" {
		directory = ""
		for attempt := 1; attempt <= canaryTurnBudget; attempt++ {
			path := filepath.Join(root, "continuations", strings.TrimPrefix(prior, "sha256:"), fmt.Sprintf("attempt-%d", attempt))
			raw, err := os.ReadFile(filepath.Join(path, "answer.raw.txt"))
			if err == nil && fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) == result.PayloadDigest {
				directory = path
				break
			}
		}
		if directory == "" {
			c.fail("development continuation has no exact accepted native turn")
		}
	}
	source, err := readJSONFile(filepath.Join(directory, "source.json"))
	if err != nil {
		c.fail("development continuation source unavailable: %v", err)
	}
	impl, err := nativeImplementationFor(c.nativeName())
	if err != nil {
		c.fail("native continuation implementation: %v", err)
	}
	if err := impl.verifySource(directory, source); err != nil {
		c.fail("development continuation original native proof: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, "answer.raw.txt"))
	if err != nil || fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) != result.PayloadDigest {
		c.fail("development continuation source differs from accepted raw result: %v", err)
	}
	identity, err := readJSONFile(filepath.Join(directory, "identity.json"))
	if err != nil || canaryCanonicalPath(textOf(getOf(identity, "workspace"))) != canaryCanonicalPath(workspace) || textOf(getOf(identity, "identity")) != textOf(getOf(source, "identity")) || textOf(getOf(source, "identity")) == "" {
		c.fail("development continuation lost original workspace/session: %v", err)
	}
	return filepath.Join(root, "continuations", strings.TrimPrefix(result.Digest, "sha256:")), textOf(getOf(source, "identity")), getOf(source, "native_store")
}

func TestDevelopmentConsumerResumesOriginalNativeSession(t *testing.T) {
	s := newCodexScenario(t)
	workspace := filepath.Join(s.home, ".formal-gates-resources", "project")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = workspace
		body, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, body, err)
		}
		return strings.TrimSpace(string(body))
	}
	if err := os.WriteFile(filepath.Join(workspace, "app.go"), []byte("package main\n// baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("add", "app.go")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Initial artifact")
	base := git("rev-parse", "HEAD")
	s.workspace, s.peer.Workspace = workspace, workspace
	catalogRoot, _ := filepath.Abs("../..")
	catalog, err := prompts.LoadPromptCatalog(catalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.WorkerInput{RunID: "native-worker", Provider: "codex", Handler: "engine.development.batch", VCS: "git", Pending: protocol.PendingAction{ActionID: "development-action", AttemptID: "development-attempt", Task: runtime.TaskKey{Node: "validation", Step: "development.worker"}}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "complete confirmed requirement"}, Validation: &protocol.ValidationInput{Handler: "engine.development.batch", Workspace: &protocol.NativeResource{Path: workspace}, Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "command"}}}}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	jobs := filepath.Join(filepath.Dir(s.home), "engine-jobs")
	if err := os.MkdirAll(jobs, 0700); err != nil {
		t.Fatal(err)
	}
	c := &engineCanary{t: t, nativeHost: "codex", localPeer: true, home: s.home, project: workspace, jobs: jobs, worker: true, binding: objectOrEmpty(s.request["binding"]), approvalDeadline: time.Now().Add(time.Minute), approvalInput: bufio.NewReader(reader), approvalOutput: canaryLocalApproval{writer}, run: facade.Run{RunID: input.RunID, Status: "DEVELOPMENT_PARALLEL", PendingActions: map[string]protocol.PendingAction{input.Pending.ActionID: input.Pending}, Attempts: map[runtime.TaskKey]protocol.Attempt{input.Pending.Task: {ID: input.Pending.AttemptID, IssuedAction: decision.IssuedAction{ActionID: input.Pending.ActionID, Task: input.Pending.Task}}}, WorkerMaterials: map[string]facade.WorkerMaterial{}, ValidationInputs: map[string]protocol.ValidationInput{}}}
	c.vcs = vcs.New(workspace, vcs.ExecRunner{})
	input.Validation.Workspace = &protocol.NativeResource{ID: "edit", Status: "READY", Path: workspace, Identity: base}
	input.Validation.Binding.WorkspaceID = "edit"
	putMaterial := func() worker.Material {
		material, err := worker.Render(input, catalog)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(material)
		sum := sha256.Sum256(body)
		path := filepath.Join(c.jobs, fmt.Sprintf("%x.json", sum))
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		c.run.WorkerMaterials[input.Pending.ActionID] = facade.WorkerMaterial{ActionID: input.Pending.ActionID, AttemptID: input.Pending.AttemptID, Path: path, Digest: fmt.Sprintf("sha256:%x", sum)}
		c.run.ValidationInputs[input.Pending.ActionID] = *input.Validation
		answer, _ := json.Marshal(worker.Result{Outcome: protocol.OutcomePass, Validation: &worker.Validation{Status: "PASS", Development: &worker.Development{Paths: []string{"app.go"}, Summary: "actual local peer semantic edit"}}})
		if err := json.Unmarshal(answer, &s.peer.Answer); err != nil {
			t.Fatal(err)
		}
		return material
	}
	material := putMaterial()
	calls := 0
	var originalIdentity string
	c.execute = func(request any, directory string, probe bool) (map[string]any, error) {
		calls++
		if err := os.WriteFile(filepath.Join(workspace, "app.go"), []byte(fmt.Sprintf("package main\n// delivered %d\n", calls)), 0600); err != nil {
			t.Fatal(err)
		}
		if probe || getOf(request, "prompt") != material.Prompt || getOf(request, "workspace") != workspace {
			t.Fatal("consumer did not use current issued material")
		}
		if calls == 1 && getOf(request, "identity") != "" || calls == 2 && getOf(request, "identity") != originalIdentity {
			t.Fatal("consumer opened a new session for unfinished batch")
		}
		s.request, s.directory = request.(map[string]any), directory
		s.peer.StrictSchema = true
		return s.execute(t)
	}
	receipt, err := protocol.NewSpawnReceiptEvent("spawn", input.Pending.ActionID, c.provider(), c.dispatchKey(input.Pending.ActionID), protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	receiptBytes, _ := json.Marshal(receipt)
	if err := os.MkdirAll(c.jobDirectory(input.Pending.ActionID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.jobDirectory(input.Pending.ActionID), "spawn-receipt.json"), receiptBytes, 0600); err != nil {
		t.Fatal(err)
	}
	first := c.dispatchProduction(input.Pending.ActionID, material)
	originalDirectory := c.transcript
	source, err := readJSONFile(filepath.Join(originalDirectory, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	originalIdentity = textOf(getOf(source, "identity"))
	if err := lifecycle.BindDispatchWithProvider(workspace, input.RunID, input.Pending.ActionID, c.dispatchKey(input.Pending.ActionID), c.provider()); err != nil {
		t.Fatal(err)
	}
	c.captureDispatchLifecycle(input.Pending.ActionID, originalDirectory)
	resultBytes, _ := json.Marshal(first.Result)
	prior := protocol.WorkerResult{ActionID: input.Pending.ActionID, Provider: "codex", Outcome: protocol.OutcomePass, PayloadDigest: first.Result.PayloadDigest, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(resultBytes)), Validation: first.Result.Validation}
	c.run.RequirementWork = []protocol.RequirementWork{{ActionID: input.Pending.ActionID, ResultDigest: prior.Digest}}
	input.Validation.Development.Result = &prior
	input.Validation.Development.Checks = []protocol.CheckEvidence{{SourceActionID: input.Pending.ActionID, ExitCode: 1, Directory: "actual-check-directory"}}
	material = putMaterial()
	originalRaw, err := os.ReadFile(filepath.Join(originalDirectory, "answer.raw.txt"))
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan canaryCompletion, 1)
	if !c.dispatchPending(map[string]bool{}, completed) {
		t.Fatal("normal consumer skipped same-action completion material")
	}
	done := <-completed
	if calls != 2 || done.event.Result == nil || done.event.Result.SourceResult != prior.Digest || done.actionID != input.Pending.ActionID || done.transcript == originalDirectory {
		t.Fatal("normal consumer lost original action/source-CAS or native output identity")
	}
	resumed, err := readJSONFile(filepath.Join(done.transcript, "source.json"))
	if err != nil || textOf(getOf(resumed, "identity")) != originalIdentity {
		t.Fatalf("native resume source/session: %v", err)
	}
	if current, _ := os.ReadFile(filepath.Join(originalDirectory, "answer.raw.txt")); string(current) != string(originalRaw) {
		t.Fatal("native continuation changed original immutable raw")
	}
	if len(c.dispatched) != 0 {
		t.Fatal("same batch was respawned")
	}
}
