//go:build !windows

package harness

// K5 acceptance — the four scenario families over the full composition, all
// deterministic, all real: a real formal-gates binary supervising a real
// native job process over a scripted fake host (the adapter tests' own
// helper-process peer, re-used — no second fake), real SQLite state, and
// real process exits for the crash-recovery family. POSIX-only behind the
// supervise/native-test precedent: the peers run behind #!/bin/sh wrappers
// and cancellation asserts the SIGTERM process-group path.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/runner"
)

// composeWorkerTasks, in graph order.
var composeWorkerTasks = []string{
	stageProductReview, stageTechnicalRev, stageDevelopment,
	stageQADesign, stageQAReview, stageGateReview,
}

// scriptedAnswer is one deterministic host answer, shared by every scenario
// (the in-process tests and the re-exec'd owner helper encode it in their
// scenario JSON).
type scriptedAnswer struct {
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	Findings  []Finding `json:"findings,omitempty"`
	SleepSecs float64   `json:"sleep_secs,omitempty"`
	// SessionUnavailable turns the resume step of a continuation into the
	// host's explicit persistent-identity refusal (-32004).
	SessionUnavailable bool `json:"session_unavailable,omitempty"`
}

var scriptedFindings = []Finding{{ID: "f-1", Severity: "P2", Message: "the implementation is broken", Paths: []string{"main.go"}}}

func passAnswer() scriptedAnswer {
	return scriptedAnswer{Status: "PASS", Summary: "green", Findings: []Finding{}}
}

// dshScriptSteps renders one scripted answer as the fake dsh peer's protocol
// sequence. Identity empty is a new session; a continuation resumes it. The
// evidence files land in jobDir, exactly the way the adapter tests' fixtures
// place them.
func dshScriptSteps(jobDir string, answer scriptedAnswer, identity string) []peerStep {
	worker := WorkerResult{
		Status:   answer.Status,
		Summary:  answer.Summary,
		Findings: answer.Findings,
		CheckIDs: []string{},
	}
	if worker.Findings == nil {
		worker.Findings = []Finding{}
	}
	answerJSON, _ := json.Marshal(worker)
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
	openSession := peerStep{
		On:   `"method":"session/new"`,
		Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`,
	}
	if identity != "" {
		if answer.SessionUnavailable {
			openSession = peerStep{
				On:   `"method":"session/resume"`,
				Send: `{"id": "@ID@", "error": {"code": -32004, "message": "Session not found: s-1"}}`,
			}
		} else {
			openSession = peerStep{
				On:   `"method":"session/resume"`,
				Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`,
			}
		}
	}
	return []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		openSession,
		{On: `"method":"session/prompt"`, SleepSecs: answer.SleepSecs, Files: []peerFile{
			{Path: filepath.Join(jobDir, "native-source.json"),
				Body: `{"sessionId": "s-1", "inputId": "in-1", "turn": 1, "transcriptPath": ""}`},
			{Path: filepath.Join(jobDir, "native-events.jsonl"), Body: events},
		}, Send: `{"id": "@ID@", "result": {"stopReason": "end_turn"}}`},
		{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
	}
}

// fixedPeer writes one scripted peer whose wrapper carries its own
// environment baked in (unlike startPeer, which relies on the process env —
// the composition spawns peers from several concurrent job processes, so a
// process-global script path cannot select between them).
func fixedPeer(t *testing.T, steps []peerStep) string {
	t.Helper()
	return fixedPeerPath(t, steps, "")
}

// fixedPeerPath is fixedPeer without testing.T (the owner helper uses it).
// echoPath names the peer's protocol echo file; empty keeps it beside the
// script, a path inside the job directory makes it part of the evidence.
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

// testReporter is the two methods fixedPeerPath needs from *testing.T —
// the owner helper's wrapped log/fatal fits it too.
type testReporter interface {
	TempDir() string
	Fatal(args ...any)
}

// dshBindingFor builds the native request's binding around one peer
// executable: provider, launch and the file proof the adapter validates.
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

// composeScripts maps each worker task (and optional task:gen:attempt keys)
// to its scripted answer. The default, when a task has no entry, is PASS.
type composeScripts map[string]scriptedAnswer

// bindingFor is the ComposeConfig.Binding closure over one script set: the
// most specific key wins (task:generation:attempt, then task), continuations
// included. It must never call testing.T's Fatal — it runs on executor
// goroutines — so failures come back as errors.
func (s composeScripts) bindingFor(t testReporter, call NativeCall) (map[string]any, error) {
	answer, ok := s[fmt.Sprintf("%s:%d:%d", call.TaskID, call.Generation, call.Attempt)]
	if !ok {
		answer, ok = s[call.TaskID]
	}
	if !ok {
		answer = passAnswer()
	}
	identity := ""
	if call.Attempt > 1 {
		identity = "s-1" // continuations resume the session the first turn opened
	}
	// The peer's protocol echo lands in the job directory, part of the
	// evidence the assertions read.
	return dshBindingFor(t, fixedPeerPath(t, dshScriptSteps(call.JobDir, answer, identity),
		filepath.Join(call.JobDir, "peer-echo.txt"))), nil
}

// openCompose builds the composition over a fresh run directory with one
// script set. The formal-gates binary is built once per test (the accepted
// supervisor helper) and the harness root is an empty temp directory, the
// same shape the adapter tests use.
func openCompose(t *testing.T, runID string, scripts composeScripts, repairLimit int) *Compose {
	t.Helper()
	bin := supervisorBinary(t)
	t.Setenv("FORMAL_GATES_HARNESS_ROOT", t.TempDir())
	c, err := ComposeHarnessV1(ComposeConfig{
		Dir:         t.TempDir(),
		RunID:       runID,
		Requirement: "K5 composition fixture requirement",
		Binary:      bin,
		Binding:     func(call NativeCall) (map[string]any, error) { return scripts.bindingFor(t, call) },
		RepairLimit: repairLimit,
		Parallelism: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// driveAndApproveGates answers the confirm and approve gates with their
// allowing choice and returns the first stop that is not one of them.
func driveAndApproveGates(t *testing.T, c *Compose, ctx context.Context) ComposeState {
	t.Helper()
	for i := 0; i < 16; i++ {
		state := c.Drive(ctx)
		if state.Status != StatusWaitingDecision || len(state.Pending) == 0 {
			return state
		}
		// Only the composition's own gates (confirm, approve) are answered
		// here; repair and restart asks are each scenario's business.
		if state.Pending[0].TaskID != stageConfirm && state.Pending[0].TaskID != stageApprove {
			return state
		}
		allow := "approve"
		if _, err := c.Answer(ctx, state.Pending[0].DecisionID, allow); err != nil {
			t.Fatalf("answer %s: %v", state.Pending[0].TaskID, err)
		}
	}
	t.Fatal("drive did not settle after the gates were approved")
	return ComposeState{}
}

// outcomeAction reads one task's latest outcome action ("" when none).
func outcomeAction(t *testing.T, c *Compose, taskID string) string {
	t.Helper()
	record := c.latestOutcome(taskID)
	if record == nil {
		return ""
	}
	return record.Action
}

// countInvocations counts job invocation directories of one task.
func countInvocations(t *testing.T, c *Compose, taskID string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(c.cfg.Dir, "jobs", taskID))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	n := 0
	for _, entry := range entries {
		if entry.IsDir() {
			n++
		}
	}
	return n
}

// waitForJobFile waits until some job directory of one task holds the named
// file, and returns its containing turn directory ("" on timeout).
func waitForJobFile(t *testing.T, c *Compose, taskID, name string, bound time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		turn := findJobFile(t, c, taskID, name)
		if turn != "" {
			return turn
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ""
}

// findJobFile returns the first turn directory of one task holding name.
func findJobFile(t *testing.T, c *Compose, taskID, name string) string {
	t.Helper()
	taskDir := filepath.Join(c.cfg.Dir, "jobs", taskID)
	invs, err := os.ReadDir(taskDir)
	if err != nil {
		return ""
	}
	for _, inv := range invs {
		if !inv.IsDir() {
			continue
		}
		turns, err := os.ReadDir(filepath.Join(taskDir, inv.Name()))
		if err != nil {
			continue
		}
		for _, turn := range turns {
			if !turn.IsDir() {
				continue
			}
			turnDir := filepath.Join(taskDir, inv.Name(), turn.Name())
			if _, err := os.Stat(filepath.Join(turnDir, name)); err == nil {
				return turnDir
			}
		}
	}
	return ""
}

// waitForOutcomeFile waits until one task's generation has landed its
// outcome record ("" on timeout).
func waitForOutcomeFile(t *testing.T, c *Compose, taskID string, generation int64, bound time.Duration) string {
	t.Helper()
	path := outcomePath(c.cfg.Dir, taskID, generation)
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ""
}

// ---- 正常: the full chain, green end to end --------------------------------

// TestComposeNormalChainGreenEndToEnd drives the whole harness-v1 graph:
// the confirmation gate holds the chain until answered, the two review and
// the development/QA pairs actually execute as parallel supervised native
// calls, the candidate is admitted, the approval gate holds the tail, the
// gate review seals, and the retention path accepts the terminal run.
func TestComposeNormalChainGreenEndToEnd(t *testing.T) {
	c := openCompose(t, "k5-normal", composeScripts{}, 1)
	ctx := context.Background()

	// Nothing executes before the requirement/plan confirmation answers.
	first := c.Drive(ctx)
	if first.Status != StatusWaitingDecision || len(first.Pending) != 1 || first.Pending[0].TaskID != stageConfirm {
		t.Fatalf("first stop = %+v, want the confirm gate", first)
	}
	if _, err := c.Answer(ctx, first.Pending[0].DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}
	if outcomeAction(t, c, stageProductReview) != "" {
		t.Fatal("a worker executed before the gate opened")
	}

	// The middle of the chain runs to the approval gate.
	second := c.Drive(ctx)
	if second.Status != StatusWaitingDecision || len(second.Pending) == 0 {
		t.Fatalf("second stop = %+v, want the approval gate", second)
	}
	var approveID string
	for _, pending := range second.Pending {
		if pending.TaskID == stageApprove {
			approveID = pending.DecisionID
		}
	}
	if approveID == "" {
		t.Fatalf("approval gate not pending: %+v", second.Pending)
	}
	for _, task := range composeWorkerTasks[:len(composeWorkerTasks)-1] {
		if outcomeAction(t, c, task) != "accept" {
			t.Fatalf("%s action = %q, want accept", task, outcomeAction(t, c, task))
		}
	}
	if candidate := c.latestOutcome(stageCandidate); candidate == nil || candidate.Admission == nil || !candidate.Admission.OK {
		t.Fatalf("candidate admission = %+v, want ok", candidate)
	}
	if outcomeAction(t, c, stageGateReview) != "" {
		t.Fatal("the gate review ran before the candidate was approved")
	}
	if _, err := c.Answer(ctx, approveID, "approve"); err != nil {
		t.Fatal(err)
	}

	final := c.Drive(ctx)
	if final.Status != StatusComplete || final.Terminal != statusHarnessComplete {
		t.Fatalf("final stop = %+v (err %v), want complete", final, final.Err)
	}

	// Every supervised call left a verified receipt behind.
	for _, task := range composeWorkerTasks {
		turn := findJobFile(t, c, task, "exit.json")
		if turn == "" {
			t.Fatalf("%s left no exit receipt", task)
		}
		receipt, err := readJSONFile(filepath.Join(turn, "exit.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !valuesEqual(getOf(receipt, "exit_code"), 0) {
			t.Fatalf("%s receipt = %v", task, receipt)
		}
	}

	// 归档判定走过: a terminal run passes the retention judgements.
	plan, err := c.Archive(filepath.Join(t.TempDir(), "k5-normal.zip"))
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if plan.StatusName != statusHarnessComplete || plan.FileCount == 0 {
		t.Fatalf("archive plan = %+v", plan)
	}
	joined := strings.Join(plan.Files, "\n")
	for _, want := range []string{"run.db", "compose-specs.json", "outcomes/development-1.json"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("archive inventory missing %s:\n%s", want, joined)
		}
	}
	if textOf(getOf(plan.Skeleton, "format")) != "formal-gates-archive-v1" {
		t.Fatalf("archive skeleton = %v", plan.Skeleton)
	}
}

// ---- 正常: held native interaction routing ----------------------------------

// TestComposeHeldInteractionRoutedAndAnswered pins §6.2 in the composition:
// the host's mid-turn permission request is routed out while the original
// call stays alive, the user's optionId travels back through the accepted
// native-respond path, and the same turn then completes.
func TestComposeHeldInteractionRoutedAndAnswered(t *testing.T) {
	scripts := composeScripts{}
	c := openCompose(t, "k5-hold", scripts, 1)
	ctx := context.Background()

	// The development peer raises one permission request before finishing
	// its turn; the fixture is injected through the script set's binding.
	c.cfg.Binding = func(call NativeCall) (map[string]any, error) {
		if call.TaskID != stageDevelopment {
			return scripts.bindingFor(t, call)
		}
		answer := passAnswer()
		steps := dshPermissionHoldSteps(call.JobDir, answer)
		return dshBindingFor(t, fixedPeer(t, steps)), nil
	}

	first := c.Drive(ctx)
	if first.Status != StatusWaitingDecision || first.Pending[0].TaskID != stageConfirm {
		t.Fatalf("first stop = %+v", first)
	}
	if _, err := c.Answer(ctx, first.Pending[0].DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}

	held := c.Drive(ctx)
	if held.Status != StatusWaitingInteraction || len(held.Held) != 1 {
		t.Fatalf("stop = %+v, want one held interaction", held)
	}
	interaction := held.Held[0]
	if interaction.TaskID != stageDevelopment || interaction.RequestID != "41" ||
		interaction.Method != "session/request_permission" {
		t.Fatalf("held = %+v", interaction)
	}
	// The exact host option id travels back — no interpretation.
	var errout bytes.Buffer
	if code := NativeRespond([]string{
		"--job", interaction.JobDir, "--request", interaction.RequestID, "--option", "allow-once"}, &errout); code != 0 {
		t.Fatalf("native-respond %d: %s", code, errout.String())
	}

	final := driveAndApproveGates(t, c, ctx)
	if final.Status != StatusComplete {
		t.Fatalf("final stop = %+v (err %v)", final, final.Err)
	}
	pendings, err := readNativePendings(interaction.JobDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pendings) != 1 || textOf(getOf(objectOrEmpty(pendings[0]), "status")) == "pending" {
		t.Fatalf("pending records = %s", mustCanonical(t, pendings))
	}
}

// dshPermissionHoldSteps is the mid-turn permission sequence: the prompt is
// answered only after the held request's selected option came back on the
// original connection. The prompt response carries the request's own frame
// id (the dsh client's third request), which the fixture hard-codes the way
// the adapter tests pin frame shapes.
func dshPermissionHoldSteps(jobDir string, answer scriptedAnswer) []peerStep {
	worker := WorkerResult{Status: answer.Status, Summary: answer.Summary, Findings: []Finding{}, CheckIDs: []string{}}
	answerJSON, _ := json.Marshal(worker)
	events := peerEvent("s-1", "assistant/message", map[string]any{
		"turn": 1, "step": 0,
		"message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "```json\n" + string(answerJSON) + "\n```"},
		}}}) + "\n" +
		peerEvent("s-1", "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}) + "\n"
	return []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		{On: `"method":"session/new"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
		{On: `"method":"session/prompt"`, Send: `{"jsonrpc": "2.0", "id": 41, "method": "session/request_permission", "params": {"sessionId": "s-1", "toolCall": {"toolCallId": "call-9"}, "options": [{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"}, {"optionId": "reject-once", "name": "Reject", "kind": "reject_once"}]}}`},
		{On: `"outcome":"selected"`, Files: []peerFile{
			{Path: filepath.Join(jobDir, "native-source.json"),
				Body: `{"sessionId": "s-1", "inputId": "in-1", "turn": 1, "transcriptPath": ""}`},
			{Path: filepath.Join(jobDir, "native-events.jsonl"), Body: events},
		}, Send: `{"id": 3, "result": {"stopReason": "end_turn"}}`},
		{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
	}
}

// ---- 异常 ①: FAIL drives the repair loop through the allowance -------------

// TestComposeRepairLoopToAllowanceApprove runs development FAIL→FAIL→PASS
// with allowance 1: the first repair round is directly approved below the
// allowance, the second reaches it and asks, the user allows one more round,
// the granted revision completes, the sibling QA branch never re-executes,
// and the chain seals.
func TestComposeRepairLoopToAllowanceApprove(t *testing.T) {
	scripts := composeScripts{
		stageDevelopment + ":1:1": {Status: "FAIL", Summary: "first defects", Findings: scriptedFindings},
		stageDevelopment + ":2:1": {Status: "FAIL", Summary: "second defects", Findings: scriptedFindings},
		stageDevelopment + ":3:1": passAnswer(),
	}
	c := openCompose(t, "k5-repair", scripts, 1)
	ctx := context.Background()

	state := driveAndApproveGates(t, c, ctx)
	// The stop past the gates is the exhaustion ask of round 2 (round 1 was
	// directly approved below the allowance).
	if state.Status != StatusWaitingDecision {
		t.Fatalf("stop = %+v (err %v), want the allowance ask", state, state.Err)
	}
	var ask *int
	for i, pending := range state.Pending {
		if pending.TaskID == stageDevelopment {
			ask = &i
		}
	}
	if ask == nil {
		t.Fatalf("no development ask pending: %+v", state.Pending)
	}
	if len(state.Pending[*ask].Options) != 2 || state.Pending[*ask].Options[0] != "continue-repair" {
		t.Fatalf("ask options = %v", state.Pending[*ask].Options)
	}
	if _, err := c.Answer(ctx, state.Pending[*ask].DecisionID, "continue-repair"); err != nil {
		t.Fatal(err)
	}

	final := driveAndApproveGates(t, c, ctx)
	if final.Status != StatusComplete || final.Terminal != statusHarnessComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
	// Three executed development generations; the QA sibling executed once.
	if gen := c.maxOutcomeGeneration(stageDevelopment); gen != 3 {
		t.Fatalf("development generations = %d, want 3", gen)
	}
	if invocations := countInvocations(t, c, stageQADesign); invocations != 1 {
		t.Fatalf("qa-design invocations = %d, want 1 (sibling PASS is never re-run)", invocations)
	}
}

// TestComposeRepairRefusedAborts is the refusal branch: the allowance ask
// answered with abort stops the run as ABORTED, without a terminal success
// and without further rounds.
func TestComposeRepairRefusedAborts(t *testing.T) {
	scripts := composeScripts{
		stageDevelopment: {Status: "FAIL", Summary: "persistent defects", Findings: scriptedFindings},
	}
	c := openCompose(t, "k5-refuse", scripts, 1)
	ctx := context.Background()

	state := driveAndApproveGates(t, c, ctx)
	if state.Status != StatusWaitingDecision {
		t.Fatalf("stop = %+v (err %v), want the allowance ask", state, state.Err)
	}
	var askID string
	for _, pending := range state.Pending {
		if pending.TaskID == stageDevelopment {
			askID = pending.DecisionID
		}
	}
	if askID == "" {
		t.Fatalf("no development ask: %+v", state.Pending)
	}
	if _, err := c.Answer(ctx, askID, "abort"); err != nil {
		t.Fatal(err)
	}
	final := c.Drive(ctx)
	if final.Status != StatusAborted || final.Terminal != statusAborted {
		t.Fatalf("final = %+v, want aborted", final)
	}
	// The refused run is still a terminal — one the retention path accepts.
	if _, err := c.Archive(filepath.Join(t.TempDir(), "k5-refuse.zip")); err != nil {
		t.Fatalf("archive of an aborted run: %v", err)
	}
}

// TestComposeContinueResumesSessionWithoutRepairRounds pins CONTINUE as a
// query: the same invocation continues on the resumed session, no repair
// round, no generation bump, no allowance ask — and the chain still seals.
func TestComposeContinueResumesSessionWithoutRepairRounds(t *testing.T) {
	scripts := composeScripts{
		stageDevelopment + ":1:1": {Status: "CONTINUE", Summary: "half done"},
		stageDevelopment + ":1:2": passAnswer(),
	}
	c := openCompose(t, "k5-continue", scripts, 1)
	ctx := context.Background()

	final := driveAndApproveGates(t, c, ctx)
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
	if gen := c.maxOutcomeGeneration(stageDevelopment); gen != 1 {
		t.Fatalf("development generation = %d, want 1 (a continuation is not a round)", gen)
	}
	if invocations := countInvocations(t, c, stageDevelopment); invocations != 1 {
		t.Fatalf("development invocations = %d, want 1", invocations)
	}
	// The second turn resumed the first turn's session: the peer's echo
	// shows session/resume carrying s-1 between the two session/new frames
	// of the independent sibling tasks.
	turn2 := filepath.Join(c.cfg.Dir, "jobs", stageDevelopment)
	echoText := findEchoUnder(t, turn2)
	if !strings.Contains(echoText, `"method":"session/resume"`) {
		t.Fatalf("no resumed session in the development jobs:\n%s", echoText)
	}
}

// findEchoUnder concatenates every peer's protocol echo under dir (each
// scripted peer writes its own).
func findEchoUnder(t *testing.T, dir string) string {
	t.Helper()
	var out strings.Builder
	filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "peer-echo.txt" {
			return nil
		}
		body, _ := os.ReadFile(path)
		out.Write(body)
		return nil
	})
	return out.String()
}

// ---- 异常 ②: crash recovery, the three takeover verdicts --------------------

// composeOwnerScenario is the JSON contract between a test and the
// re-exec'd owner helper process (TestComposeOwner): one composition run the
// helper drives, answering only the confirm/approve gates, until the test
// kills it or the chain settles.
type composeOwnerScenario struct {
	Dir         string                    `json:"dir"`
	RunID       string                    `json:"run_id"`
	Binary      string                    `json:"binary"`
	HarnessRoot string                    `json:"harness_root"`
	RepairLimit int                       `json:"repair_limit"`
	Scripts     map[string]scriptedAnswer `json:"scripts"`
	Done        string                    `json:"done"`
}

// TestComposeOwner is the helper-process entry: with
// FORMAL_GATES_COMPOSE_OWNER set it becomes one composition owner; otherwise
// it is an ordinary pass-through test.
func TestComposeOwner(t *testing.T) {
	scenarioPath := os.Getenv("FORMAL_GATES_COMPOSE_OWNER")
	if scenarioPath == "" {
		return
	}
	os.Exit(runComposeOwner(scenarioPath))
}

// ownerReporter adapts the fatal-only helper to the testReporter the peer
// builders use.
type ownerReporter struct{}

func (ownerReporter) TempDir() string   { dir, _ := os.MkdirTemp("", "compose-owner"); return dir }
func (ownerReporter) Fatal(args ...any) { fmt.Println(args...); os.Exit(3) }

func runComposeOwner(scenarioPath string) int {
	body, err := os.ReadFile(scenarioPath)
	if err != nil {
		return 2
	}
	var scenario composeOwnerScenario
	if err := json.Unmarshal(body, &scenario); err != nil {
		return 2
	}
	os.Setenv("FORMAL_GATES_HARNESS_ROOT", scenario.HarnessRoot)
	scripts := composeScripts(scenario.Scripts)
	c, err := ComposeHarnessV1(ComposeConfig{
		Dir:         scenario.Dir,
		RunID:       scenario.RunID,
		Requirement: "K5 composition fixture requirement",
		Binary:      scenario.Binary,
		Binding:     func(call NativeCall) (map[string]any, error) { return scripts.bindingFor(ownerReporter{}, call) },
		RepairLimit: scenario.RepairLimit,
		Parallelism: 2,
	})
	if err != nil {
		return 2
	}
	defer c.Close()
	ctx := context.Background()
	for i := 0; i < 16; i++ {
		state := c.Drive(ctx)
		if state.Status != StatusWaitingDecision || len(state.Pending) == 0 {
			break
		}
		if state.Pending[0].TaskID != stageConfirm && state.Pending[0].TaskID != stageApprove {
			break
		}
		if _, err := c.Answer(ctx, state.Pending[0].DecisionID, "approve"); err != nil {
			return 2
		}
	}
	if scenario.Done != "" {
		os.WriteFile(scenario.Done, []byte("done"), 0o644)
	}
	return 0
}

// startOwnerProcess spawns the helper with one scenario and returns it.
func startOwnerProcess(t *testing.T, scenario *composeOwnerScenario) *exec.Cmd {
	t.Helper()
	body, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	scenarioPath := filepath.Join(t.TempDir(), "owner.json")
	if err := os.WriteFile(scenarioPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(testBinary, "-test.run=^TestComposeOwner$", "-test.timeout=120s")
	cmd.Env = append(os.Environ(), "FORMAL_GATES_COMPOSE_OWNER="+scenarioPath)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

// ownerCompose reopens the crashed run inside the test process with the same
// scripts, the way a fresh owner would.
func ownerCompose(t *testing.T, scenario *composeOwnerScenario) *Compose {
	t.Helper()
	scripts := composeScripts(scenario.Scripts)
	c, err := ComposeHarnessV1(ComposeConfig{
		Dir:         scenario.Dir,
		RunID:       scenario.RunID,
		Requirement: "K5 composition fixture requirement",
		Binary:      scenario.Binary,
		Binding:     func(call NativeCall) (map[string]any, error) { return scripts.bindingFor(t, call) },
		RepairLimit: scenario.RepairLimit,
		Parallelism: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// newOwnerScenario prepares a fresh run directory driven by an owner helper.
func newOwnerScenario(t *testing.T, runID string, scripts composeScripts, repairLimit int) (*composeOwnerScenario, *Compose) {
	t.Helper()
	bin := supervisorBinary(t)
	harnessRoot := t.TempDir()
	t.Setenv("FORMAL_GATES_HARNESS_ROOT", harnessRoot)
	scenario := &composeOwnerScenario{
		Dir:         t.TempDir(),
		RunID:       runID,
		Binary:      bin,
		HarnessRoot: harnessRoot,
		RepairLimit: repairLimit,
		Scripts:     map[string]scriptedAnswer(scripts),
	}
	// Open once here so the directory is created and inspectable before the
	// owner takes over (it adopts the same run).
	probe := ownerCompose(t, scenario)
	return scenario, probe
}

// killOwnerWhenHostStarts kills the owner process once the development host
// has provably started (its native-process.json exists) — the accepted
// positive-proof pattern.
func killOwnerWhenHostStarts(t *testing.T, c *Compose, cmd *exec.Cmd) {
	t.Helper()
	if waitForJobFile(t, c, stageDevelopment, "native-process.json", 30*time.Second) == "" {
		t.Fatal("the development host never started")
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
}

// TestComposeCrashAdoptVerifiableResult: the owner dies mid-call, the
// orphaned supervisor finishes the turn, and the reopened owner adopts the
// verifiable result under the original call identity — one execution, no
// re-run, chain complete.
func TestComposeCrashAdoptVerifiableResult(t *testing.T) {
	scenario, probe := newOwnerScenario(t, "k5-adopt",
		composeScripts{stageDevelopment: {Status: "PASS", Summary: "green", Findings: []Finding{}, SleepSecs: 2}}, 1)
	probe.Close()
	owner := startOwnerProcess(t, scenario)
	killOwnerWhenHostStarts(t, probe, owner)

	// The orphaned supervisor completes the turn on its own; its exit
	// receipt is the adoptable fact.
	if waitForJobFile(t, probe, stageDevelopment, "exit.json", 30*time.Second) == "" {
		t.Fatal("the orphaned turn never completed")
	}
	// Reopen as a fresh owner and adopt.
	c := ownerCompose(t, scenario)
	final := driveAndApproveGates(t, c, context.Background())
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v), want adoption to complete the chain", final, final.Err)
	}
	if invocations := countInvocations(t, c, stageDevelopment); invocations != 1 {
		t.Fatalf("development invocations = %d, want 1 (adoption never re-executes)", invocations)
	}
	if outcomeAction(t, c, stageDevelopment) != "accept" {
		t.Fatalf("adopted action = %q", outcomeAction(t, c, stageDevelopment))
	}
}

// TestComposeCrashObserveLiveSupervisor: the owner dies while its supervisor
// is still working; the reopened owner observes the live call — no second
// executor — and adopts it once it settles.
func TestComposeCrashObserveLiveSupervisor(t *testing.T) {
	scenario, probe := newOwnerScenario(t, "k5-observe",
		composeScripts{stageDevelopment: {Status: "PASS", Summary: "green", Findings: []Finding{}, SleepSecs: 5}}, 1)
	probe.Close()
	owner := startOwnerProcess(t, scenario)
	killOwnerWhenHostStarts(t, probe, owner)

	c := ownerCompose(t, scenario)
	observed := c.Drive(context.Background())
	if observed.Status != StatusObserved {
		t.Fatalf("stop = %+v (err %v), want observed", observed, observed.Err)
	}
	if invocations := countInvocations(t, c, stageDevelopment); invocations != 1 {
		t.Fatalf("development invocations = %d, want 1 (observation starts nothing)", invocations)
	}
	// The original supervisor finishes; the next drive adopts its result.
	if waitForJobFile(t, c, stageDevelopment, "exit.json", 30*time.Second) == "" {
		t.Fatal("the observed turn never settled")
	}
	final := driveAndApproveGates(t, c, context.Background())
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
	if invocations := countInvocations(t, c, stageDevelopment); invocations != 1 {
		t.Fatalf("development invocations = %d, want 1 (adoption after observation)", invocations)
	}
}

// TestComposeSessionUnavailableAsksRestart pins the SESSION_UNAVAILABLE
// classification: a continuation whose session the host explicitly refuses
// leaves no receipt, the takeover consult turns the fact into the restart
// request (Runner.Restart's question), and both answers behave —
// restart-session replaces the execution identity, abort ends ABORTED.
func TestComposeSessionUnavailableAsksRestart(t *testing.T) {
	// statefulRestartBinding answers CONTINUE on the first generation-1
	// spawn, refuses the resumed session on its continuation, and passes the
	// replacement invocation (the second spawn of the same key).
	statefulRestartBinding := func(t *testing.T) func(NativeCall) (map[string]any, error) {
		var mu sync.Mutex
		spawns := map[string]int{}
		scripts := composeScripts{
			stageDevelopment + ":1:1": {Status: "CONTINUE", Summary: "half done"},
			stageDevelopment + ":1:2": {Status: "PASS", Summary: "resumed", Findings: []Finding{}, SessionUnavailable: true},
		}
		return func(call NativeCall) (map[string]any, error) {
			// The binding runs on parallel executor goroutines.
			mu.Lock()
			key := fmt.Sprintf("%s:%d:%d", call.TaskID, call.Generation, call.Attempt)
			spawns[key]++
			second := key == stageDevelopment+":1:1" && spawns[key] == 2
			mu.Unlock()
			if second {
				return dshBindingFor(t, fixedPeer(t, dshScriptSteps(call.JobDir, passAnswer(), ""))), nil
			}
			return scripts.bindingFor(t, call)
		}
	}
	t.Run("restart-session", func(t *testing.T) {
		bin := supervisorBinary(t)
		t.Setenv("FORMAL_GATES_HARNESS_ROOT", t.TempDir())
		binding := statefulRestartBinding(t)
		c, err := ComposeHarnessV1(ComposeConfig{
			Dir:         t.TempDir(),
			RunID:       "k5-restart",
			Requirement: "K5 composition fixture requirement",
			Binary:      bin,
			Binding:     binding,
			RepairLimit: 1,
			Parallelism: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		ctx := context.Background()

		state := driveAndApproveGates(t, c, ctx)
		if state.Status != StatusWaitingDecision {
			t.Fatalf("stop = %+v (err %v), want the restart ask", state, state.Err)
		}
		var restart *runner.DecisionView
		for i := range state.Pending {
			if state.Pending[i].TaskID == stageDevelopment {
				restart = &state.Pending[i]
			}
		}
		if restart == nil {
			t.Fatalf("no restart ask: %+v", state.Pending)
		}
		if len(restart.Options) != 2 || restart.Options[0] != "restart-session" || restart.Options[1] != "abort" {
			t.Fatalf("restart options = %v", restart.Options)
		}
		// The refused session is the durable fact behind the ask.
		if turn := findJobFile(t, c, stageDevelopment, "native-error.json"); turn == "" {
			t.Fatal("no native-error.json for the refused session")
		} else {
			record, err := readJSONFile(filepath.Join(turn, "native-error.json"))
			if err != nil {
				t.Fatal(err)
			}
			if textOf(getOf(record, "error_kind")) != "SESSION_UNAVAILABLE" {
				t.Fatalf("error record = %s", mustCanonical(t, record))
			}
		}
		if _, err := c.Answer(ctx, restart.DecisionID, "restart-session"); err != nil {
			t.Fatal(err)
		}
		final := driveAndApproveGates(t, c, ctx)
		if final.Status != StatusComplete {
			t.Fatalf("final = %+v (err %v)", final, final.Err)
		}
		// Two execution identities: the dead call's and the replacement's.
		if invocations := countInvocations(t, c, stageDevelopment); invocations != 2 {
			t.Fatalf("development invocations = %d, want 2 (dead call + replacement)", invocations)
		}
	})
	t.Run("abort", func(t *testing.T) {
		bin := supervisorBinary(t)
		t.Setenv("FORMAL_GATES_HARNESS_ROOT", t.TempDir())
		c, err := ComposeHarnessV1(ComposeConfig{
			Dir:         t.TempDir(),
			RunID:       "k5-restart-abort",
			Requirement: "K5 composition fixture requirement",
			Binary:      bin,
			Binding:     statefulRestartBinding(t),
			RepairLimit: 1,
			Parallelism: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		ctx := context.Background()
		state := driveAndApproveGates(t, c, ctx)
		if state.Status != StatusWaitingDecision {
			t.Fatalf("stop = %+v (err %v)", state, state.Err)
		}
		for _, pending := range state.Pending {
			if pending.TaskID == stageDevelopment {
				if _, err := c.Answer(ctx, pending.DecisionID, "abort"); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
		final := c.Drive(ctx)
		if final.Status != StatusAborted || final.Terminal != statusAborted {
			t.Fatalf("final = %+v, want aborted", final)
		}
	})
}

// ---- 异常 ③: one FAIL is never covered by a sibling PASS --------------------

// TestComposeFailNotCoveredBySiblingPass: development FAILs while the QA
// branch passes; the candidate gate refuses on the development result, the
// repair targets development alone, and the sibling's result survives
// untouched. With both branches failing, development's refusal wins the
// wording (candidateAdmission's order) and each branch is repaired in turn.
func TestComposeFailNotCoveredBySiblingPass(t *testing.T) {
	t.Run("development-fail", func(t *testing.T) {
		scripts := composeScripts{
			stageDevelopment + ":1:1": {Status: "FAIL", Summary: "defects", Findings: scriptedFindings},
			stageDevelopment + ":2:1": passAnswer(),
		}
		c := openCompose(t, "k5-sibling", scripts, 1)
		final := driveAndApproveGates(t, c, context.Background())
		if final.Status != StatusComplete {
			t.Fatalf("final = %+v (err %v)", final, final.Err)
		}
		if invocations := countInvocations(t, c, stageQADesign); invocations != 1 {
			t.Fatalf("qa-design invocations = %d, want 1", invocations)
		}
		if gen := c.maxOutcomeGeneration(stageDevelopment); gen != 2 {
			t.Fatalf("development generations = %d, want 2", gen)
		}
		refusal := c.readOutcome(stageCandidate, 1)
		if refusal == nil || refusal.Admission == nil ||
			refusal.Admission.Refusal != "Unresolved development result cannot create a verification candidate" {
			t.Fatalf("first refusal = %+v", refusal)
		}
	})
	t.Run("both-branches-fail-in-order", func(t *testing.T) {
		scripts := composeScripts{
			stageDevelopment + ":1:1": {Status: "FAIL", Summary: "defects", Findings: scriptedFindings},
			stageDevelopment + ":2:1": passAnswer(),
			stageQAReview + ":1:1":    {Status: "FAIL", Summary: "coverage insufficient", Findings: scriptedFindings},
			// qa-design's second generation (after the QA repair) and the
			// dependent qa-review's second generation both pass.
			stageQADesign + ":2:1": passAnswer(),
			stageQAReview + ":2:1": passAnswer(),
		}
		c := openCompose(t, "k5-sibling-both", scripts, 2)
		final := driveAndApproveGates(t, c, context.Background())
		if final.Status != StatusComplete {
			t.Fatalf("final = %+v (err %v)", final, final.Err)
		}
		first := c.readOutcome(stageCandidate, 1)
		second := c.readOutcome(stageCandidate, 2)
		if first == nil || first.Admission == nil ||
			first.Admission.Refusal != "Unresolved development result cannot create a verification candidate" {
			t.Fatalf("first refusal = %+v (development wins the wording)", first)
		}
		if second == nil || second.Admission == nil || second.Admission.Refusal != "QA not approved" {
			t.Fatalf("second refusal = %+v, want QA", second)
		}
		if gen := c.maxOutcomeGeneration(stageQADesign); gen != 2 {
			t.Fatalf("qa-design generations = %d, want 2", gen)
		}
	})
}

// ---- 取消: stop dispatch, settle the in-flight supervised calls -------------

// TestComposeCancelSettlesInFlightCalls cancels a run whose development call
// is mid-turn: dispatch stops, the supervisor's SIGTERM path settles the
// in-flight job with the deterministic cancellation receipt, and only then
// is the settlement reported. The run never claims completion.
func TestComposeCancelSettlesInFlightCalls(t *testing.T) {
	scripts := composeScripts{
		stageDevelopment: {Status: "PASS", Summary: "green", Findings: []Finding{}, SleepSecs: 60},
	}
	c := openCompose(t, "k5-cancel", scripts, 1)
	ctx := context.Background()

	first := c.Drive(ctx)
	if first.Status != StatusWaitingDecision || first.Pending[0].TaskID != stageConfirm {
		t.Fatalf("first stop = %+v", first)
	}
	if _, err := c.Answer(ctx, first.Pending[0].DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}

	driven := make(chan ComposeState, 1)
	go func() { driven <- c.Drive(ctx) }()
	if waitForJobFile(t, c, stageDevelopment, "native-process.json", 30*time.Second) == "" {
		t.Fatal("the development host never started")
	}
	// Let the fast QA branch finish so the sleeping development call is the
	// one live supervised job at cancellation.
	if waitForOutcomeFile(t, c, stageQAReview, 1, 30*time.Second) == "" {
		t.Fatal("the QA branch never settled")
	}
	before := countJobDirs(t, c)

	settled, err := c.Cancel(ctx)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if len(settled) != 1 || settled[0].TaskID != stageDevelopment {
		t.Fatalf("settlements = %+v, want the in-flight development job", settled)
	}
	if settled[0].ExitCode != 130 || settled[0].Error != "owned job was cancelled" {
		t.Fatalf("settlement = %+v, want the deterministic cancellation receipt", settled[0])
	}
	select {
	case state := <-driven:
		if state.Status == StatusComplete {
			t.Fatal("a cancelled run claimed completion")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the drive did not return after cancellation")
	}
	// Dispatch stopped: no new job directories appeared after the cancel.
	time.Sleep(300 * time.Millisecond)
	if after := countJobDirs(t, c); after != before {
		t.Fatalf("job dirs before = %d, after = %d (dispatch continued)", before, after)
	}
}

// countJobDirs counts every turn directory of the run.
func countJobDirs(t *testing.T, c *Compose) int {
	t.Helper()
	root := filepath.Join(c.cfg.Dir, "jobs")
	count := 0
	filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() && strings.HasPrefix(entry.Name(), "turn-") {
			count++
		}
		return nil
	})
	return count
}

// ---- 归档: terminal walks the retention path, non-terminal refuses ----------

// TestComposeArchiveRefusesNonTerminalRun: a run still waiting on its
// confirmation gate is not archived — the settlement judgement's own
// refusal, not a composition-side shortcut.
func TestComposeArchiveRefusesNonTerminalRun(t *testing.T) {
	c := openCompose(t, "k5-archive", composeScripts{}, 1)
	state := c.Drive(context.Background())
	if state.Status != StatusWaitingDecision {
		t.Fatalf("stop = %+v", state)
	}
	_, err := c.Archive(filepath.Join(t.TempDir(), "k5-archive.zip"))
	if err == nil || !strings.Contains(err.Error(), "RUN_NOT_TERMINAL") {
		t.Fatalf("archive err = %v, want RUN_NOT_TERMINAL", err)
	}
}

// ---- 两宿主: the zcode adapter carries a stage too --------------------------

// TestComposeZcodeHostCarriesAStage runs the normal chain with the product
// review bound to the zcode adapter's scripted app-server — the composition
// is host-agnostic through the binding, exactly as the two accepted adapters
// define it.
func TestComposeZcodeHostCarriesAStage(t *testing.T) {
	bin := supervisorBinary(t)
	harnessRoot := t.TempDir()
	t.Setenv("FORMAL_GATES_HARNESS_ROOT", harnessRoot)
	dir := t.TempDir()
	scripts := composeScripts{}
	c, err := ComposeHarnessV1(ComposeConfig{
		Dir:         dir,
		RunID:       "k5-zcode",
		Requirement: "K5 composition fixture requirement",
		Binary:      bin,
		RepairLimit: 1,
		Parallelism: 2,
		Binding: func(call NativeCall) (map[string]any, error) {
			if call.TaskID == stageProductReview {
				return zcodeBindingFor(t, call.JobDir), nil
			}
			return scripts.bindingFor(t, call)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	final := driveAndApproveGates(t, c, context.Background())
	if final.Status != StatusComplete {
		t.Fatalf("final = %+v (err %v)", final, final.Err)
	}
	record := c.latestOutcome(stageProductReview)
	if record == nil || record.Provider != "zcode" || record.Action != "accept" {
		t.Fatalf("product review outcome = %+v, want the zcode host's accepted result", record)
	}
}

// zcodeBindingFor builds the zcode binding around a scripted app-server
// peer: the launch names the peer executable and a proofed dummy script,
// the shape the adapter's own tests use.
func zcodeBindingFor(t *testing.T, jobDir string) map[string]any {
	inputID := "formal-gates-" + filepath.Base(resolveNativePath(jobDir))
	answer := WorkerResult{Status: "PASS", Summary: "green on zcode", Findings: []Finding{}, CheckIDs: []string{}}
	answerJSON, _ := json.Marshal(answer)
	// The completion's response is a JSON string carrying the framed worker
	// text — the adapter unwraps the string, then FrameResult reads the object.
	responseText, _ := json.Marshal(string(answerJSON))
	steps := []peerStep{
		{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
		{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
		{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
		// turn.started must precede turn.completed: the source receipt's
		// matcher refuses a terminal event without its start.
		{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 1, "turn.started", "t-1", zcodeTurnPayload(inputID, ""))},
		{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
			zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+string(responseText)+
				`, "usage": {"inputTokens": 9, "outputTokens": 2}`))},
		{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
	}
	executable := fixedPeer(t, steps)
	appServer := filepath.Join(filepath.Dir(executable), "zcode.cjs")
	if err := os.WriteFile(appServer, []byte("// fake app-server bundle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executableProof, err := fileProofOf(executable)
	if err != nil {
		t.Fatal(err)
	}
	scriptProof, err := fileProofOf(appServer)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"provider": "zcode",
		"launch":   map[string]any{"executable": executable, "script": appServer},
		"files":    map[string]any{executable: executableProof, appServer: scriptProof},
	}
}
