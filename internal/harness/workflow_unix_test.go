//go:build !windows

package harness

// K5 acceptance — the four scenario families over the full composition, all
// deterministic, all real: a real formal-gates binary supervising real
// native job processes over a scripted fake host (the adapter tests' own
// helper-process peer, re-used — no second fake), a real Git project with
// really executed approved checks, real SQLite state, and real process
// identities for the crash-recovery and cancellation families. POSIX-only
// behind the supervise/native-test precedent.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/runner"
)

// scriptedAnswer is one deterministic host answer. Writes land in the
// worker's workspace (the development fixture's implementation).
type scriptedAnswer struct {
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	Findings  []Finding `json:"findings,omitempty"`
	SleepSecs float64   `json:"sleep_secs,omitempty"`
	// SessionUnavailable turns the session open step into the host's
	// explicit persistent-identity refusal (-32004).
	SessionUnavailable bool `json:"session_unavailable,omitempty"`
	// Garbage makes the framed answer fail the worker contract (R9's
	// contract-rejected classification).
	Garbage bool `json:"garbage,omitempty"`
	// Writes are absolute-path files the peer drops when its turn fires.
	Writes []peerFile `json:"writes,omitempty"`
	// CheckIDs overrides the answer's check ids (QA roles must return
	// every authorized id).
	CheckIDs []string `json:"check_ids,omitempty"`
	// NoWrite suppresses the development fixture's implementation write —
	// the "declared PASS but wrote nothing" scenario.
	NoWrite bool `json:"no_write,omitempty"`
}

var scriptedFindings = []Finding{{ID: "f-1", Severity: "P1", Message: "the implementation is broken", Paths: []string{"main.go"}}}

func passAnswer() scriptedAnswer {
	return scriptedAnswer{Status: "PASS", Summary: "green", Findings: []Finding{}}
}

// composeScripts maps a task's call to its answer. Three key shapes, tried
// in order: "task#N" (the task's Nth call, 1-based — stable however the
// kernel's generation counters move), "task:generation[:attempt]", and the
// bare task (every call). PASS is the default.
type composeScripts map[string]scriptedAnswer

func (s composeScripts) forCall(call NativeCall, ordinal int) scriptedAnswer {
	for _, key := range []string{
		fmt.Sprintf("%s#%d", call.TaskID, ordinal),
		fmt.Sprintf("%s:%d:%d", call.TaskID, call.Generation, call.Attempt),
		fmt.Sprintf("%s:%d", call.TaskID, call.Generation),
		call.TaskID,
	} {
		if answer, ok := s[key]; ok {
			return answer
		}
	}
	return passAnswer()
}

// dshScriptSteps renders one scripted answer as the fake peer's protocol
// sequence, the adapter tests' shape. Identity empty is a new session; a
// continuation resumes it.
func dshScriptSteps(jobDir string, answer scriptedAnswer) []peerStep {
	worker := WorkerResult{
		Status:   answer.Status,
		Summary:  answer.Summary,
		Findings: answer.Findings,
		CheckIDs: answer.CheckIDs,
	}
	if worker.Findings == nil {
		worker.Findings = []Finding{}
	}
	if worker.CheckIDs == nil {
		worker.CheckIDs = []string{}
	}
	answerJSON, _ := json.Marshal(worker)
	if answer.Garbage {
		answerJSON = []byte(`{"status": "PASS", "summary": 7}`)
	}
	events := peerEvent("s-1", "assistant/message", map[string]any{
		"turn": 1, "step": 0,
		"usage": map[string]any{"inputTokens": 12, "outputTokens": 3},
		"message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "```json\n"},
			map[string]any{"type": "tool_use", "id": "t-1"},
			map[string]any{"type": "text", "text": string(answerJSON)},
			map[string]any{"type": "text", "text": "\n```"},
		}}}) + "\n" +
		peerEvent("s-1", "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}) + "\n"
	// The fake host answers whichever session-open method the adapter
	// uses: a recovered continuation resumes its remembered session on the
	// FIRST attempt of a fresh invocation, so "attempt 1 means session/new"
	// does not hold — both handlers stand and the unused one never fires
	// (a step matches by substring, at most once).
	openNew := peerStep{
		On:   `"method":"session/new"`,
		Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`,
	}
	openResume := peerStep{
		On:   `"method":"session/resume"`,
		Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`,
	}
	if answer.SessionUnavailable {
		// The host refuses the persistent identity on whichever method the
		// call opens the session with.
		openNew = peerStep{On: openNew.On, Send: `{"id": "@ID@", "error": {"code": -32004, "message": "Session not found: s-1"}}`}
		openResume = peerStep{On: openResume.On, Send: `{"id": "@ID@", "error": {"code": -32004, "message": "Session not found: s-1"}}`}
	}
	step := peerStep{
		On: `"method":"session/prompt"`, SleepSecs: answer.SleepSecs, Send: `{"id": "@ID@", "result": {"stopReason": "end_turn"}}`,
		Files: []peerFile{
			{Path: filepath.Join(jobDir, "native-source.json"),
				Body: `{"sessionId": "s-1", "inputId": "in-1", "turn": 1, "transcriptPath": ""}`},
			{Path: filepath.Join(jobDir, "native-events.jsonl"), Body: events},
		},
	}
	step.Files = append(step.Files, answer.Writes...)
	return []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		openNew,
		openResume,
		step,
		{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
	}
}

// fixedPeerPath writes one scripted peer whose wrapper carries its own
// environment baked in (the composition spawns peers from several
// concurrent job processes, so a process-global script path cannot select
// between them).
func fixedPeerPath(t testReporter, steps []peerStep, echoPath string) string {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "peer.json")
	script := peerScript{Steps: steps}
	if echoPath == "" {
		echoPath = filepath.Join(dir, "echo.txt")
	}
	script.Echo = echoPath
	body, err := json.Marshal(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "dsh")
	wrapperBody := fmt.Sprintf(
		"#!/bin/sh\nFORMAL_GATES_NATIVE_PEER_SCRIPT=%s\nPEER_BIN=%s\nexport FORMAL_GATES_NATIVE_PEER_SCRIPT PEER_BIN\nexec \"$PEER_BIN\" -test.run='^TestScriptedPeer$' -- \"$@\"\n",
		scriptPath, testBinary)
	if err := os.WriteFile(wrapper, []byte(wrapperBody), 0o755); err != nil {
		t.Fatal(err)
	}
	return wrapper
}

// testReporter is the two methods fixedPeerPath needs from *testing.T.
type testReporter interface {
	TempDir() string
	Fatal(args ...any)
}

// dshBindingFor builds the native request's binding around one peer.
func dshBindingFor(t testReporter, executable string) map[string]any {
	proof, err := fileProofOf(executable)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"provider": "dsh",
		"launch":   map[string]any{"executable": executable},
		"files":    map[string]any{executable: proof},
	}
}

// sampleProject creates the Git project fixture: a base commit whose
// state.txt is broken and one authorized check that demands it be ok.
func sampleProject(t *testing.T) (project, base string) {
	t.Helper()
	project = t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", project}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@localhost",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@localhost",
			"commit.gpgsign=false")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", args, err, out)
		}
	}
	run("init", "--initial-branch=main", "--quiet")
	if err := os.WriteFile(filepath.Join(project, "state.txt"), []byte("broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "check.sh"), []byte("#!/bin/sh\ntest \"$(cat state.txt)\" = ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".gitignore"), []byte("__pycache__/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "fixture base")
	out, err := exec.Command("git", "-C", project, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return project, strings.TrimSpace(string(out))
}

// openWorkflow builds the composition over a fresh run of the sample
// project.
func openWorkflow(t *testing.T, runID string, scripts composeScripts, repairLimit int) *Compose {
	t.Helper()
	project, base := sampleProject(t)
	bin := supervisorBinary(t)
	dir := t.TempDir()
	devWorkspace := filepath.Join(dir, "workspaces", "development")
	calls := map[string]int{}
	var c *Compose
	c, err := ComposeHarnessV1(ComposeConfig{
		Dir:   dir,
		RunID: runID,
		Spec: ComposeSpec{
			Project:     project,
			Base:        base,
			Requirement: "Make state.txt contain ok. Do not change anything else.",
			Checks:      []Check{{ID: "state", Argv: []string{"/bin/sh", "check.sh"}}},
			Gates:       []string{"complexity-gate"},
			RepairLimit: repairLimit,
		},
		Binary: bin,
		Binding: func(call NativeCall) (map[string]any, error) {
			calls[call.TaskID]++
			answer := scripts.forCall(call, calls[call.TaskID])
			// The QA roles must account for every authorized check id.
			if answer.CheckIDs == nil && (call.Role == "qa-design" || call.Role == "qa-review") {
				answer.CheckIDs = []string{"state"}
			}
			// The development fixture's implementation lands in its
			// workspace; a repair round's fix lands the same way.
			if (call.Role == "development" || call.Role == "repair") && !answer.Garbage && !answer.NoWrite {
				answer.Writes = append(answer.Writes, peerFile{
					Path: filepath.Join(devWorkspace, "state.txt"), Body: "ok\n",
				})
			}
			// The fake host answers whichever session-open method the call
			// uses (see dshScriptSteps): a continuation, a repair round and
			// a replacement after an explicit restart are all answered
			// exactly as opened — a fresh open with a fresh session, a
			// resume with the same session id.
			return dshBindingFor(t, fixedPeerPath(t, dshScriptSteps(call.JobDir, answer),
				filepath.Join(call.JobDir, "peer-echo.txt"))), nil
		},
		Parallelism: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// answerAllowing answers a waiting stop's questions with their allowing
// choice (except a named override), stopping un-answered at any question
// task named in stopAt, and returns the first stop that is not answered
// here.
func answerAllowing(t *testing.T, c *Compose, ctx context.Context, overrides map[string]string, stopAt ...string) ComposeState {
	t.Helper()
	stop := map[string]bool{}
	for _, task := range stopAt {
		stop[task] = true
	}
	for i := 0; i < 32; i++ {
		state := c.Drive(ctx)
		if state.Status != StatusWaitingDecision || len(state.Pending) == 0 {
			return state
		}
		if stop[state.Pending[0].TaskID] {
			return state
		}
		for _, question := range state.Pending {
			choice := ""
			for _, option := range question.Options {
				switch option {
				case "approve", "continue", "retry", "restart-session":
					choice = option
				}
			}
			if override, ok := overrides[question.TaskID]; ok {
				choice = override
			}
			if choice == "" {
				t.Fatalf("ask without a known choice: %+v", question)
			}
			if _, err := c.Answer(ctx, question.DecisionID, choice); err != nil {
				t.Fatalf("answer %s (%s): %v", question.DecisionID, question.TaskID, err)
			}
		}
	}
	events, _ := os.ReadFile(filepath.Join(c.dir, "events.jsonl"))
	lines := strings.Split(string(events), "\n")
	if len(lines) > 30 {
		lines = lines[len(lines)-30:]
	}
	t.Fatalf("drive did not settle after its questions were answered")
	return ComposeState{}
}

// findTaskJSON scans one workspace's materials for a task.json whose
// feedback carries the wanted purpose.
func findTaskJSON(t *testing.T, workspace, purpose string) map[string]any {
	t.Helper()
	root := filepath.Join(workspace, ".formal-gates-input")
	var found map[string]any
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "task.json" || found != nil {
			return nil
		}
		value, err := readJSONFile(path)
		if err != nil {
			return nil
		}
		task := objectOrEmpty(value)
		// The task-input judgement wraps feedback in its untrusted envelope;
		// the purpose travels inside the wrapped evidence — directly, or in
		// one of the paired wrappers a retry brief carries.
		content := objectOrEmpty(objectOrEmpty(task["feedback"])["content"])
		if textOf(content["purpose"]) == purpose {
			found = task
			return nil
		}
		for _, nested := range content {
			if inner := objectOrEmpty(nested); textOf(inner["purpose"]) == purpose {
				found = task
			}
		}
		return nil
	})
	return found
}

// turnDirs lists one task's turn directories.
func turnDirs(t *testing.T, c *Compose, taskID string) []string {
	t.Helper()
	var dirs []string
	root := filepath.Join(c.dir, "jobs", pathSafe(taskID))
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() && strings.HasPrefix(entry.Name(), "turn-") {
			dirs = append(dirs, path)
		}
		return nil
	})
	return dirs
}

// ---- 正常: the full chain, green end to end --------------------------------

// TestWorkflowGreenChainSealsAndArchives drives the whole flow: the
// requirement gate holds the chain; the reviews run serially with the
// product evidence reaching the technical review; the plan approval
// carries the review evidence and the exact check command; development
// and QA run in parallel with their own materials; the candidate is a real
// frozen Git commit; the authorized check really executes against it; the
// gate review reads the frozen comparison; and the run seals with a
// harness-result.json and a verified physical archive.
func TestWorkflowGreenChainSealsAndArchives(t *testing.T) {
	c := openWorkflow(t, "wf-green", composeScripts{}, 1)
	ctx := context.Background()

	first := c.Drive(ctx)
	if first.Status != StatusWaitingDecision || len(first.Pending) != 1 || first.Pending[0].TaskID != taskRequirement {
		t.Fatalf("first stop = %+v, want the requirement gate", first)
	}
	// The question carries the creation facts (R3).
	var body map[string]any
	if err := json.Unmarshal([]byte(first.Pending[0].Body), &body); err != nil {
		t.Fatalf("requirement body: %v", err)
	}
	if textOf(body["base"]) == "" || textOf(getOf(body, "project")) == "" || len(listOf(body["checks"])) != 1 {
		t.Fatalf("requirement body = %v", body)
	}
	if _, err := c.Answer(ctx, first.Pending[0].DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}

	// The chain advances to the plan approval.
	second := c.Drive(ctx)
	if second.Status != StatusWaitingDecision || len(second.Pending) != 1 || second.Pending[0].TaskID != askTask(taskPlan) {
		t.Fatalf("second stop = %+v, want the plan gate (pending %+v)", second, second.Pending)
	}
	if err := json.Unmarshal([]byte(second.Pending[0].Body), &body); err != nil {
		t.Fatalf("plan body: %v", err)
	}
	if textOf(body["product"]) == "" || textOf(body["technical"]) == "" || len(listOf(body["checks"])) != 1 {
		t.Fatalf("plan body = %v", body)
	}
	// The product review reached the technical review as evidence (R2).
	baseline := filepath.Join(c.dir, "workspaces", "baseline")
	if findTaskJSON(t, baseline, "prior-product-review") == nil {
		_ = filepath.WalkDir(filepath.Join(c.dir), func(path string, entry os.DirEntry, err error) error {
			return nil
		})
		t.Fatal("the technical review never received the product evidence")
	}
	if _, err := c.Answer(ctx, second.Pending[0].DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}

	final := answerAllowing(t, c, ctx, nil)
	if final.Status != StatusComplete || final.Terminal != statusHarnessComplete {
		t.Fatalf("final stop = %+v (err %v)", final, final.Err)
	}
	// Development received the technical plan; QA review received the
	// design (R2).
	development := filepath.Join(c.dir, "workspaces", "development")
	if findTaskJSON(t, development, "reviewed-technical-plan") == nil {
		t.Fatal("development never received the technical plan evidence")
	}
	if findTaskJSON(t, baseline, "qa-design") == nil {
		t.Fatal("the QA review never received the QA design evidence")
	}
	// The candidate is a real commit and the result names it.
	result := readResultFile(t, c)
	candidate := textOf(result["candidate"])
	if len(candidate) != 40 {
		t.Fatalf("candidate = %q", candidate)
	}
	head, err := exec.Command("git", "-C", development, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != candidate {
		t.Fatalf("development HEAD = %q (%v), want the sealed candidate %q", head, err, candidate)
	}
	// The authorized check really executed against the frozen candidate
	// and only passed because development wrote the implementation.
	checks := turnDirs(t, c, checkTaskID("state"))
	if len(checks) == 0 {
		t.Fatal("the authorized check never executed")
	}
	receipt, err := readJSONFile(filepath.Join(checks[0], "exit.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(getOf(receipt, "exit_code"), 0) {
		t.Fatalf("check receipt = %v", receipt)
	}
	// The gate review read the frozen comparison materials.
	found := false
	_ = filepath.WalkDir(filepath.Join(c.dir, "workspaces"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.Name() == "comparison.json" {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("the gate review never received comparison materials")
	}
	// The physical archive exists and verifies (R8).
	destination := filepath.Join(t.TempDir(), "wf-green.zip")
	report, err := c.Archive(destination)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if report.Status != "ARCHIVED" || report.FileCount == 0 {
		t.Fatalf("archive report = %+v", report)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	verifyArchiveFile(t, destination)
	// A second archive to the same destination is refused.
	if _, err := c.Archive(destination); err == nil {
		t.Fatal("an existing destination was overwritten")
	}
}

// readResultFile reads harness-result.json.
func readResultFile(t *testing.T, c *Compose) map[string]any {
	t.Helper()
	value, err := readJSONFile(filepath.Join(c.dir, "harness-result.json"))
	if err != nil {
		t.Fatalf("harness-result.json: %v", err)
	}
	result := objectOrEmpty(value)
	if len(result) == 0 {
		t.Fatal("harness-result.json is not an object")
	}
	return result
}

// verifyArchiveFile independently re-reads an archive and checks the
// manifest's member proofs.
func verifyArchiveFile(t *testing.T, path string) {
	t.Helper()
	reader, err := openZip(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	manifestValue, err := readJSONBytes(readZipMember(t, reader, "archive-manifest.json"))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	manifest := objectOrEmpty(manifestValue)
	files := objectOrEmpty(manifest["files"])
	if len(files) == 0 {
		t.Fatal("archive manifest has no files")
	}
	if textOf(manifest["format"]) != "formal-gates-archive-v1" {
		t.Fatalf("format = %v", manifest["format"])
	}
	for name, expected := range files {
		body := readZipMember(t, reader, "run/"+name)
		proof, _ := expected.(map[string]any)
		if textOf(proof["sha256"]) != digestBytes(body) {
			t.Fatalf("member %s differs from its manifest proof", name)
		}
	}
}

// ---- 异常: asks, retries, the repair loop, crash recovery -------------------

// TestWorkflowPreReviewFailAskRetryAbort: a failing product review asks
// its retry question; abort ends the run ABORTED, and a reopen derives the
// same terminal from the persisted facts (R1, R5).
func TestWorkflowPreReviewFailAskRetryAbort(t *testing.T) {
	scripts := composeScripts{taskProduct: {Status: "FAIL", Summary: "scope is incoherent", Findings: scriptedFindings}}
	c := openWorkflow(t, "wf-prereview", scripts, 1)
	ctx := context.Background()

	state := answerAllowing(t, c, ctx, map[string]string{taskRequirement: "approve"}, askTask(taskProduct))
	if state.Status != StatusWaitingDecision || len(state.Pending) != 1 {
		t.Fatalf("stop = %+v, want the pre-review ask (pending %+v)", state, state.Pending)
	}
	if state.Pending[0].TaskID != askTask(taskProduct) {
		t.Fatalf("ask task = %s, want %s", state.Pending[0].TaskID, askTask(taskProduct))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(state.Pending[0].Body), &body); err != nil {
		t.Fatal(err)
	}
	if textOf(body["kind"]) != "pre-review" || textOf(body["evidence"]) == "" {
		t.Fatalf("ask body = %v", body)
	}
	// Nothing downstream ran while the question stood.
	if outcome := c.latestOutcome(taskTechnical); outcome != nil {
		t.Fatalf("technical review ran on a failed product review: %+v", outcome)
	}
	if _, err := c.Answer(ctx, state.Pending[0].DecisionID, "abort"); err != nil {
		t.Fatal(err)
	}
	after := c.Drive(ctx)
	if after.Status != StatusAborted || after.Terminal != statusAborted {
		t.Fatalf("stop after abort = %+v", after)
	}
	// The persisted facts alone reproduce the terminal (R5).
	dir := c.dir
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c2, err := ComposeHarnessV1(ComposeConfig{Dir: dir, RunID: "wf-prereview", Spec: c.cfg.Spec, Binary: c.binary, Binding: c.cfg.Binding, Parallelism: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	again := c2.Drive(ctx)
	if again.Status != StatusAborted {
		t.Fatalf("reopened stop = %+v, want aborted", again)
	}
}

// TestWorkflowPreReviewRetryThenPass: the retry answer revises the review
// with the prior findings as feedback, and the chain continues.
func TestWorkflowPreReviewRetryThenPass(t *testing.T) {
	scripts := composeScripts{
		taskProduct + "#1": {Status: "FAIL", Summary: "scope is incoherent", Findings: scriptedFindings},
	}
	c := openWorkflow(t, "wf-prereview-retry", scripts, 1)
	ctx := context.Background()

	state := answerAllowing(t, c, ctx, map[string]string{taskRequirement: "approve"}, askTask(taskProduct))
	if state.Status != StatusWaitingDecision || state.Pending[0].TaskID != askTask(taskProduct) {
		t.Fatalf("stop = %+v", state)
	}
	if _, err := c.Answer(ctx, state.Pending[0].DecisionID, "retry"); err != nil {
		t.Fatal(err)
	}
	// The retry brief carried the prior findings (R2: evidence feedback).
	final := answerAllowing(t, c, ctx, nil)
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
	baseline := filepath.Join(c.dir, "workspaces", "baseline")
	task := findTaskJSON(t, baseline, "prior-review-findings")
	if task == nil {
		t.Fatal("the retried review never received its prior findings")
	}
	if len(turnDirs(t, c, taskProduct)) < 2 {
		t.Fatal("the product review did not re-run after its retry")
	}
}

// TestWorkflowQADesignLoop: a failing QA review asks the design retry; the
// design re-runs with the review's findings and the review re-runs after it.
func TestWorkflowQADesignLoop(t *testing.T) {
	scripts := composeScripts{
		taskQAReview + "#1": {Status: "FAIL", Summary: "coverage is insufficient", Findings: scriptedFindings},
	}
	c := openWorkflow(t, "wf-qaloop", scripts, 1)
	ctx := context.Background()

	state := answerAllowing(t, c, ctx, nil, askTask(taskQADesign))
	for state.Status == StatusWaitingDecision && state.Pending[0].TaskID == askTask(taskQADesign) {
		if _, err := c.Answer(ctx, state.Pending[0].DecisionID, "retry"); err != nil {
			t.Fatal(err)
		}
		state = c.Drive(ctx)
	}
	// Continue: answer everything allowing until complete.
	final := answerAllowing(t, c, ctx, nil)
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
	baseline := filepath.Join(c.dir, "workspaces", "baseline")
	if findTaskJSON(t, baseline, "qa-review-findings") == nil {
		t.Fatal("the retried QA design never received the review's findings")
	}
	if findTaskJSON(t, baseline, "prior-qa-design") == nil {
		t.Fatal("the retried QA design never received its prior design")
	}
}

// TestWorkflowQADesignFailReviewPassAsksDesignRetry: a needs-repair QA
// design fails the final verification even when the review itself passes —
// the review admits only when its own result AND the design it reviewed
// are both clean, and the design's judgment lives nowhere else. The retry
// re-runs the design with the review's findings and the prior design.
func TestWorkflowQADesignFailReviewPassAsksDesignRetry(t *testing.T) {
	scripts := composeScripts{
		taskQADesign + "#1": {Status: "FAIL", Summary: "a coverage gap remains", Findings: scriptedFindings},
	}
	c := openWorkflow(t, "wf-qadesign-pair", scripts, 1)
	ctx := context.Background()

	state := answerAllowing(t, c, ctx, nil, askTask(taskQADesign))
	if state.Status != StatusWaitingDecision || len(state.Pending) != 1 ||
		state.Pending[0].TaskID != askTask(taskQADesign) {
		t.Fatalf("stop = %+v (pending %+v), want the qa-design ask", state, state.Pending)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(state.Pending[0].Body), &body); err != nil {
		t.Fatal(err)
	}
	if textOf(body["kind"]) != "qa-design" {
		t.Fatalf("ask kind = %v (body %v)", body["kind"], body)
	}
	// The chain held: no candidate formed on a needs-repair design.
	if outcome := c.latestOutcome(taskCandidate); outcome != nil {
		t.Fatalf("candidate formed on a needs-repair design: %+v", outcome)
	}
	if _, err := c.Answer(ctx, state.Pending[0].DecisionID, "retry"); err != nil {
		t.Fatal(err)
	}
	final := answerAllowing(t, c, ctx, nil)
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
	// The retried design carried the review's findings and its prior self.
	baseline := filepath.Join(c.dir, "workspaces", "baseline")
	if findTaskJSON(t, baseline, "qa-review-findings") == nil {
		t.Fatal("the retried QA design never received the review's findings")
	}
	if findTaskJSON(t, baseline, "prior-qa-design") == nil {
		t.Fatal("the retried QA design never received its prior design")
	}
	if len(turnDirs(t, c, taskQADesign)) < 2 {
		t.Fatal("the QA design did not re-run after its retry")
	}
}

// TestWorkflowDevelopmentFindingsAsk: unresolved development findings ask
// retry/abort and the retry re-runs development with the findings as
// feedback.
func TestWorkflowDevelopmentFindingsAsk(t *testing.T) {
	scripts := composeScripts{
		taskDevelopment + "#1": {Status: "FAIL", Summary: "work is unfinished", Findings: scriptedFindings},
	}
	c := openWorkflow(t, "wf-devfindings", scripts, 1)
	ctx := context.Background()

	state := answerAllowing(t, c, ctx, nil, askTask(taskDevelopment))
	if state.Status != StatusWaitingDecision || len(state.Pending) == 0 ||
		state.Pending[0].TaskID != askTask(taskDevelopment) {
		t.Fatalf("stop = %+v (pending %+v), want the development-findings ask", state, state.Pending)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(state.Pending[0].Body), &body); err != nil {
		t.Fatal(err)
	}
	if textOf(body["kind"]) != "development-findings" {
		t.Fatalf("ask body = %v", body)
	}
	// The candidate cannot form while the findings stand.
	if outcome := c.latestOutcome(taskCandidate); outcome != nil {
		t.Fatalf("candidate formed on unresolved findings: %+v", outcome)
	}
	if _, err := c.Answer(ctx, state.Pending[0].DecisionID, "retry"); err != nil {
		t.Fatal(err)
	}
	final := answerAllowing(t, c, ctx, nil)
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
	development := filepath.Join(c.dir, "workspaces", "development")
	if findTaskJSON(t, development, "unresolved-findings") == nil {
		t.Fatal("the retried development never received its findings")
	}
}

// TestWorkflowRepairRoundDrivesNewCandidate: a failing check routes the
// repair round; the repair role runs with the failed-verification bundle,
// the candidate re-freezes, the check re-runs against it, and the run
// seals. The repair limit of one authorizes the first round directly.
func TestWorkflowRepairRoundDrivesNewCandidate(t *testing.T) {
	scripts := composeScripts{
		// Round 0: development declares PASS but writes nothing, so the
		// authorized check fails against the frozen candidate.
		taskDevelopment + "#1": {Status: "PASS", Summary: "nothing written", Findings: []Finding{}, NoWrite: true},
	}
	// RepairLimit 2: the plan approval's re-arm consumes one of
	// development's revision rounds in the kernel's allowance chain, so the
	// first repair round is direct with a limit of two.
	c := openWorkflow(t, "wf-repair", scripts, 2)
	ctx := context.Background()

	final := answerAllowing(t, c, ctx, nil)
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
	// The repair round ran: development executed at least twice, the last
	// under the repair role.
	generations := c.outcomeGenerations(taskDevelopment)
	if len(generations) < 2 {
		t.Fatalf("development generations = %v, want a repair round", generations)
	}
	second := c.readOutcome(taskDevelopment, generations[len(generations)-1])
	if second == nil || second.Output == nil || second.Output.Role != "repair" {
		t.Fatalf("the repair round = %+v", second)
	}
	// The repair brief carried the failed verification (R4: derived from
	// the persistent bundle, at the round's own moment).
	development := filepath.Join(c.dir, "workspaces", "development")
	if findTaskJSON(t, development, "failed-verification") == nil {
		t.Fatal("the repair round never received the failed-verification bundle")
	}
	// The check ran in both rounds; the second passed.
	checks := turnDirs(t, c, checkTaskID("state"))
	if len(checks) < 2 {
		t.Fatalf("check runs = %d, want one per round", len(checks))
	}
	result := readResultFile(t, c)
	if textOf(result["status"]) != statusHarnessComplete {
		t.Fatalf("result status = %v", result["status"])
	}
}

// TestWorkflowRepairLimitAskContinueAndAbort: with the allowance spent, the
// next repair round asks; continue authorizes one more round, abort ends
// the run.
func TestWorkflowRepairLimitAskContinueAndAbort(t *testing.T) {
	// Round 0 and repair round 1 both fail (nothing written); the limit of
	// one authorizes round 1 directly and asks before round 2.
	nothingWritten := scriptedAnswer{Status: "PASS", Summary: "nothing written", Findings: []Finding{}, NoWrite: true}
	never := composeScripts{taskDevelopment: nothingWritten}
	// continue branch: the authorized round 2 writes the fix.
	continued := composeScripts{
		taskDevelopment:        nothingWritten,
		taskDevelopment + "#3": passAnswer(),
	}
	t.Run("continue", func(t *testing.T) {
		c := openWorkflow(t, "wf-limit-continue", continued, 1)
		ctx := context.Background()
		final := answerAllowing(t, c, ctx, nil)
		if final.Status != StatusComplete {
			t.Fatalf("final = %+v (err %v)", final, final.Err)
		}
		if len(c.outcomeGenerations(taskDevelopment)) < 3 {
			t.Fatalf("development generations = %v, want the authorized extra round", c.outcomeGenerations(taskDevelopment))
		}
	})
	t.Run("abort", func(t *testing.T) {
		c := openWorkflow(t, "wf-limit-abort", never, 1)
		ctx := context.Background()
		state := answerAllowing(t, c, ctx, nil, taskDevelopment)
		if state.Status != StatusWaitingDecision || len(state.Pending) == 0 || state.Pending[0].TaskID != taskDevelopment {
			t.Fatalf("stop = %+v (pending %+v), want the repair-limit ask", state, state.Pending)
		}
		for _, option := range state.Pending[0].Options {
			if option != "continue" && option != "abort" {
				t.Fatalf("repair-limit options = %v", state.Pending[0].Options)
			}
		}
		if _, err := c.Answer(ctx, state.Pending[0].DecisionID, "abort"); err != nil {
			t.Fatal(err)
		}
		after := c.Drive(ctx)
		if after.Status != StatusAborted {
			t.Fatalf("stop after abort = %+v", after)
		}
	})
}

// TestWorkflowSessionRefusalAsksRestart: a host that refuses the resumed
// session (the repair round's remembered development identity) parks the
// call without any business question, and the kernel's restart request
// asks; restart-session re-runs the round to success.
func TestWorkflowSessionRefusalAsksRestart(t *testing.T) {
	scripts := composeScripts{
		// Round 0 writes nothing so a repair round runs; the repair's
		// resumed session is the one the host refuses.
		taskDevelopment + "#1": {Status: "PASS", Summary: "nothing written", Findings: []Finding{}, NoWrite: true},
		taskDevelopment + "#2": {SessionUnavailable: true, Status: "PASS", Summary: "never reached"},
		taskDevelopment + "#3": passAnswer(),
	}
	// RepairLimit 2 for the same allowance reason as the repair-round test.
	c := openWorkflow(t, "wf-session", scripts, 2)
	ctx := context.Background()

	state := answerAllowing(t, c, ctx, nil, taskDevelopment)
	if state.Status != StatusWaitingDecision || len(state.Pending) == 0 || state.Pending[0].TaskID != taskDevelopment {
		t.Fatalf("stop = %+v (pending %+v), want the restart ask", state, state.Pending)
	}
	for _, option := range state.Pending[0].Options {
		if option != "restart-session" && option != "abort" {
			t.Fatalf("restart options = %v", state.Pending[0].Options)
		}
	}
	if _, err := c.Answer(ctx, state.Pending[0].DecisionID, "restart-session"); err != nil {
		t.Fatal(err)
	}
	final := answerAllowing(t, c, ctx, nil)
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
}

// TestWorkflowContractRejectedIsARuntimeQuestion: an exit-0 answer the
// worker contract rejects becomes a runtime question of its own (R9) —
// retry / restart-session / abort — never a disguised session failure, and
// the retry re-runs the stage.
func TestWorkflowContractRejectedIsARuntimeQuestion(t *testing.T) {
	scripts := composeScripts{
		taskProduct + "#1": {Garbage: true, Status: "PASS", Summary: "unparseable"},
	}
	c := openWorkflow(t, "wf-contract", scripts, 1)
	ctx := context.Background()

	state := answerAllowing(t, c, ctx, map[string]string{taskRequirement: "approve"}, askTask(taskProduct))
	if state.Status != StatusWaitingDecision || len(state.Pending) == 0 {
		t.Fatalf("stop = %+v, want a runtime ask (pending %+v)", state, state.Pending)
	}
	question := state.Pending[0]
	if question.TaskID != askTask(taskProduct) {
		t.Fatalf("ask task = %s", question.TaskID)
	}
	joined := strings.Join(question.Options, ",")
	if !strings.Contains(joined, "restart-session") || !strings.Contains(joined, "retry") {
		t.Fatalf("runtime options = %v (must be retry/restart-session/abort)", question.Options)
	}
	if strings.Join(question.Options, ",") == "restart-session,abort" {
		t.Fatal("a contract rejection masqueraded as a session failure")
	}
	if _, err := c.Answer(ctx, question.DecisionID, "retry"); err != nil {
		t.Fatal(err)
	}
	final := answerAllowing(t, c, ctx, nil)
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
}

// TestWorkflowCrashTakeoverClassifications: the takeover consultation (the
// recovery seam) classifies a pending invocation from its job directory's
// facts — a live supervisor under its recorded identity is observed, a
// settled answer the worker contract rejects becomes the runtime question
// (R9), and a turn with no evidence at all is the zero fact whose kernel
// restart request asks. The consultation is exercised directly over real
// fabricated job directories, plus one reopened-owner integration: the
// runtime question survives the owner change.
func TestWorkflowCrashTakeoverClassifications(t *testing.T) {
	briefOf := func(role string) []byte {
		body, err := CanonicalJSON(map[string]any{
			"stage": role, "role": role, "revision": "fixture", "feedback": nil,
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	invocationOf := func(role, invocation string) runner.Invocation {
		return runner.Invocation{
			InvocationID: invocation, TaskID: role, Generation: 1,
			InputDigest: "fixture", Input: briefOf(role),
		}
	}
	t.Run("alive-supervisor-is-observed", func(t *testing.T) {
		c := openWorkflow(t, "wf-crash", composeScripts{}, 1)
		live := fabricatePredecessor(t, c, taskProduct, "inv-alive", 10)
		defer signalTurn(t, live)
		outcome, err := c.consult(context.Background(), invocationOf(taskProduct, "inv-alive"))
		if err != nil {
			t.Fatal(err)
		}
		if !outcome.Observe || outcome.Adopt {
			t.Fatalf("consult = %+v, want observed", outcome)
		}
	})
	t.Run("contract-rejected-is-a-runtime-question", func(t *testing.T) {
		c := openWorkflow(t, "wf-crash", composeScripts{}, 1)
		turn := filepath.Join(c.dir, "jobs", pathSafe(taskProduct), "inv-rejected", "turn-01")
		makeRejectedAnswer(t, c, turn)
		outcome, err := c.consult(context.Background(), invocationOf(taskProduct, "inv-rejected"))
		if err != nil {
			t.Fatal(err)
		}
		if !outcome.Observe || outcome.Adopt {
			t.Fatalf("consult = %+v, want observed", outcome)
		}
		pending, err := c.store.PendingDecisions(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var question *runner.DecisionView
		for i := range pending {
			if pending[i].TaskID == askTask(taskProduct) {
				question = &pending[i]
			}
		}
		if question == nil {
			t.Fatalf("pending = %+v, want the runtime question", pending)
		}
		joined := strings.Join(question.Options, ",")
		if !strings.Contains(joined, "retry") || !strings.Contains(joined, "restart-session") {
			t.Fatalf("runtime options = %v", question.Options)
		}
	})
	t.Run("no-evidence-is-the-zero-fact", func(t *testing.T) {
		c := openWorkflow(t, "wf-crash", composeScripts{}, 1)
		if err := os.MkdirAll(filepath.Join(c.dir, "jobs", pathSafe(taskProduct), "inv-empty", "turn-01"), 0o755); err != nil {
			t.Fatal(err)
		}
		outcome, err := c.consult(context.Background(), invocationOf(taskProduct, "inv-empty"))
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Observe || outcome.Adopt {
			t.Fatalf("consult = %+v, want the zero outcome (the restart fact)", outcome)
		}
	})
	t.Run("reopened-owner-surfaces-the-runtime-question", func(t *testing.T) {
		// The product review parks on an answer the contract rejects; the
		// owner closes with the question standing, and the reopened owner
		// surfaces the same runtime question (R9: never a session failure).
		c := openWorkflow(t, "wf-crash", composeScripts{
			taskProduct + "#1": {Garbage: true, Status: "PASS", Summary: "unparseable"},
		}, 1)
		ctx := context.Background()
		state := answerAllowing(t, c, ctx, map[string]string{taskRequirement: "approve"}, askTask(taskProduct))
		if state.Status != StatusWaitingDecision || len(state.Pending) != 1 {
			t.Fatalf("runtime ask not reached: %+v", state)
		}
		cfg := c.cfg
		dir := c.dir
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := ComposeHarnessV1(ComposeConfig{
			Dir: dir, RunID: cfg.RunID, Spec: cfg.Spec, Binary: cfg.Binary, Binding: cfg.Binding, Parallelism: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		again := reopened.Drive(ctx)
		if again.Status != StatusWaitingDecision || len(again.Pending) != 1 ||
			again.Pending[0].TaskID != askTask(taskProduct) {
			t.Fatalf("reopened stop = %+v (pending %+v), want the runtime ask", again, again.Pending)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(again.Pending[0].Body), &body); err != nil {
			t.Fatal(err)
		}
		if textOf(body["kind"]) != "runtime" {
			t.Fatalf("ask kind = %v (body %v)", body["kind"], body)
		}
	})
}

// signalTurn terminates a fabricated live supervisor's process group and
// waits for its receipt.
func signalTurn(t *testing.T, turnDir string) {
	t.Helper()
	value, err := readJSONFile(filepath.Join(turnDir, "process.json"))
	if err != nil {
		return
	}
	if pid := intOf(getOf(value, "pid")); pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(turnDir, "exit.json")); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// state0 returns the single pending question of a waiting stop.
func state0(c *Compose, ctx context.Context) runner.DecisionView {
	pending, err := c.PendingDecisions(ctx)
	if err != nil || len(pending) == 0 {
		return runner.DecisionView{}
	}
	return pending[0]
}

// makeRejectedAnswer completes one turn with a real supervised job whose
// framed answer fails the worker contract.
func makeRejectedAnswer(t *testing.T, c *Compose, jobDir string) {
	t.Helper()
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	answer := scriptedAnswer{Garbage: true, Status: "PASS", Summary: "unparseable"}
	peer := fixedPeerPath(t, dshScriptSteps(jobDir, answer), filepath.Join(jobDir, "peer-echo.txt"))
	binding := dshBindingFor(t, peer)
	request := map[string]any{
		"kind":  "native",
		"argv":  []any{c.binary, "harness", "native", jobDir},
		"cwd":   c.dir,
		"stdin": "",
		"environment": map[string]any{
			"LANGSMITH_TRACING":           "false",
			"LANGCHAIN_TRACING_V2":        "false",
			"FORMAL_GATES_HARNESS_BINARY": c.binary,
		},
	}
	if err := writeAtomicJSON(filepath.Join(jobDir, "request.json"), request); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(jobDir, "native-request.json"), map[string]any{
		"provider":  textOf(getOf(binding, "provider")),
		"binding":   binding,
		"workspace": c.dir,
		"read_only": true,
		"identity":  "",
		"prompt":    "fixture",
		"schema":    plainWorkerSchema(),
	}); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(c.binary, "harness", "supervise", jobDir).CombinedOutput(); err != nil {
		t.Fatalf("supervise: %v: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(jobDir, "answer.json")); err != nil {
		t.Fatal("the fabricated job produced no answer")
	}
}

// fabricatePredecessor creates a real supervised job directory under one
// task — a predecessor's call — with a peer that sleeps the given seconds.
func fabricatePredecessor(t *testing.T, c *Compose, taskID, invocation string, sleep float64) string {
	t.Helper()
	jobDir := filepath.Join(c.dir, "jobs", pathSafe(taskID), invocation, "turn-01")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	answer := scriptedAnswer{Status: "PASS", Summary: "green", SleepSecs: sleep}
	peer := fixedPeerPath(t, dshScriptSteps(jobDir, answer), filepath.Join(jobDir, "peer-echo.txt"))
	binding := dshBindingFor(t, peer)
	request := map[string]any{
		"kind":  "native",
		"argv":  []any{c.binary, "harness", "native", jobDir},
		"cwd":   c.dir,
		"stdin": "",
		"environment": map[string]any{
			"LANGSMITH_TRACING":           "false",
			"LANGCHAIN_TRACING_V2":        "false",
			"FORMAL_GATES_HARNESS_BINARY": c.binary,
		},
	}
	if err := writeAtomicJSON(filepath.Join(jobDir, "request.json"), request); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(jobDir, "native-request.json"), map[string]any{
		"provider":  textOf(getOf(binding, "provider")),
		"binding":   binding,
		"workspace": c.dir,
		"read_only": true,
		"identity":  "",
		"prompt":    "fixture",
		"schema":    plainWorkerSchema(),
	}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(c.binary, "harness", "supervise", jobDir)
	cmd.SysProcAttr = supervisorSysProcAttr()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Detach: the predecessor's supervisor outlives this command.
	if err := cmd.Process.Release(); err != nil {
		t.Fatal(err)
	}
	// Wait for the supervisor's identity to be recorded; leave it running
	// (the predecessor's in-flight call).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(jobDir, "process.json")); err == nil {
			return jobDir
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("fabricated supervisor never started")
	return jobDir
}

// ---- 取消 -------------------------------------------------------------------

// TestWorkflowCancelSettlesEveryInFlightCall: cancellation stops
// dispatching, signals every supervisor of the run — including a
// predecessor's, found by its recorded identity — and reports only settled
// receipts.
func TestWorkflowCancelSettlesEveryInFlightCall(t *testing.T) {
	c := openWorkflow(t, "wf-cancel", composeScripts{
		taskProduct: {Status: "PASS", Summary: "green", SleepSecs: 8, Findings: []Finding{}},
	}, 1)
	ctx := context.Background()
	if state := c.Drive(ctx); state.Status != StatusWaitingDecision {
		t.Fatalf("first stop = %+v", state)
	}
	if _, err := c.Answer(ctx, state0(c, ctx).DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}
	// A predecessor's live call exists alongside this owner's.
	predecessor := fabricatePredecessor(t, c, taskTechnical, "inv-old", 30)

	var state ComposeState
	for i := 0; i < 40; i++ {
		state = c.Drive(ctx)
		if state.Status == StatusWaitingInteraction || state.Status == StatusObserved {
			break
		}
		if state.Status == StatusWaitingDecision {
			break
		}
	}
	settled, err := c.Cancel(context.Background())
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	seen := map[string]bool{}
	for _, job := range settled {
		seen[job.JobDir] = true
	}
	if !seen[predecessor] {
		t.Fatalf("the predecessor's call was not settled: %+v", settled)
	}
	for _, job := range settled {
		receipt, err := readJSONFile(filepath.Join(job.JobDir, "exit.json"))
		if err != nil {
			t.Fatalf("%s has no receipt after cancellation: %v", job.JobDir, err)
		}
		if !valuesEqual(getOf(receipt, "exit_code"), 130) {
			t.Fatalf("%s settled with exit %v, want the cancellation receipt", job.JobDir, getOf(receipt, "exit_code"))
		}
	}
}

// ---- 归档 -------------------------------------------------------------------

// TestWorkflowArchiveRefusesNonTerminal: an active run cannot be archived.
func TestWorkflowArchiveRefusesNonTerminal(t *testing.T) {
	c := openWorkflow(t, "wf-archive-active", composeScripts{}, 1)
	if _, err := c.Archive(filepath.Join(t.TempDir(), "x.zip")); err == nil {
		t.Fatal("an active run was archived")
	} else if !strings.Contains(err.Error(), "RUN_NOT_TERMINAL") {
		t.Fatalf("refusal = %v", err)
	}
}

// TestWorkflowReopenBindingRefused: a reopened run with a changed binding
// is refused by name (R7).
func TestWorkflowReopenBindingRefused(t *testing.T) {
	c := openWorkflow(t, "wf-binding", composeScripts{}, 1)
	if state := c.Drive(context.Background()); state.Status != StatusWaitingDecision {
		t.Fatalf("first stop = %+v", state)
	}
	dir := c.dir
	binary, binding := c.binary, c.cfg.Binding
	changed := c.cfg.Spec
	changed.Base = "0000000000000000000000000000000000000000"
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ComposeHarnessV1(ComposeConfig{Dir: dir, RunID: "wf-binding", Spec: changed, Binary: binary, Binding: binding, Parallelism: 1}); err == nil {
		t.Fatal("a changed base was silently adopted")
	} else if !strings.Contains(err.Error(), "RUN_BINDING") {
		t.Fatalf("refusal = %v", err)
	}
	if _, err := ComposeHarnessV1(ComposeConfig{Dir: dir, RunID: "other", Spec: c.cfg.Spec, Binary: binary, Binding: binding, Parallelism: 1}); err == nil {
		t.Fatal("a changed run id was silently adopted")
	}
}

// openZip opens an archive for verification.
func openZip(path string) (*zip.ReadCloser, error) { return zip.OpenReader(path) }

// readJSONBytes decodes raw JSON bytes.
func readJSONBytes(body []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// readZipMember reads one member's bytes.
func readZipMember(t *testing.T, reader *zip.ReadCloser, name string) []byte {
	t.Helper()
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		open, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(open)
		open.Close()
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	t.Fatalf("member %s is missing", name)
	return nil
}
