//go:build !windows

package harness

// Round-2 external-review probes, adopted into the tree by the orchestrating
// agent as the repair's failing baseline. Ported verbatim from the review
// snapshot (/private/tmp/k5-review2/snapshot) — expectations unchanged, only
// this header added. They drive the public composition surfaces with real
// owner-process exits and real supervised jobs: the recovery edges the main
// suites do not reach.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func independentConfig(t *testing.T, scripts composeScripts) ComposeConfig {
	t.Setenv("FORMAL_GATES_HARNESS_ROOT", t.TempDir())
	project, base := sampleProject(t)
	cfg := ComposeConfig{Dir: t.TempDir(), RunID: "independent-k5", Spec: ComposeSpec{Project: project, Base: base, Requirement: "Make state.txt contain ok. Do not change anything else.", Checks: []Check{{ID: "state", Argv: []string{"/bin/sh", "check.sh"}}}, Gates: []string{"complexity-gate"}, RepairLimit: 2}, Binary: supervisorBinary(t), Parallelism: 2}
	cfg.Binding = independentBinding(t, cfg, scripts, nil)
	return cfg
}
func independentBinding(t testReporter, cfg ComposeConfig, scripts composeScripts, observed func(NativeCall)) func(NativeCall) (map[string]any, error) {
	var mu sync.Mutex
	counts := map[string]int{}
	return func(call NativeCall) (map[string]any, error) {
		mu.Lock()
		counts[call.TaskID]++
		ordinal := counts[call.TaskID]
		mu.Unlock()
		if observed != nil {
			observed(call)
		}
		answer := scripts.forCall(call, ordinal)
		if answer.CheckIDs == nil && (call.Role == "qa-design" || call.Role == "qa-review") {
			answer.CheckIDs = []string{"state"}
		}
		if (call.Role == "development" || call.Role == "repair") && !answer.NoWrite && !answer.Garbage {
			answer.Writes = append(answer.Writes, peerFile{Path: filepath.Join(cfg.Dir, "workspaces", "development", "state.txt"), Body: "ok\n"})
		}
		return dshBindingFor(t, fixedPeerPath(t, dshScriptSteps(call.JobDir, answer), filepath.Join(call.JobDir, "peer-echo.txt"))), nil
	}
}
func independentOpen(t *testing.T, cfg ComposeConfig) *Compose {
	c, err := ComposeHarnessV1(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestIndependentK5RuntimeStops(t *testing.T) {
	for _, kind := range []string{"check-runtime", "gate-blocked"} {
		t.Run(kind, func(t *testing.T) {
			scripts := composeScripts{}
			if kind == "gate-blocked" {
				scripts[gateTaskID("complexity-gate")] = scriptedAnswer{Status: "BLOCKED", Summary: "temporary access problem"}
			}
			cfg := independentConfig(t, scripts)
			wantTask := askTask(gateTaskID("complexity-gate"))
			if kind == "check-runtime" {
				cfg.Spec.Checks[0].Argv = []string{"/bin/sh", "-c", "exit 127"}
				wantTask = askTask(checkTaskID("state"))
			}
			c := independentOpen(t, cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			got := answerAllowing(t, c, ctx, nil, wantTask)
			t.Logf("stop=%+v", got)
			if got.Status != StatusWaitingDecision {
				t.Errorf("normal %s should surface retry/abort, got %s: %v", kind, got.Status, got.Err)
			}
		})
	}
}
func TestIndependentK5FreshSessionAfterRestart(t *testing.T) {
	scripts := composeScripts{"development#1": {Status: "PASS", Summary: "not fixed", NoWrite: true}, "development#2": {Status: "PASS", Summary: "unavailable", SessionUnavailable: true}}
	cfg := independentConfig(t, scripts)
	var mu sync.Mutex
	var calls []NativeCall
	cfg.Binding = independentBinding(t, cfg, scripts, func(call NativeCall) {
		if call.TaskID == taskDevelopment {
			mu.Lock()
			calls = append(calls, call)
			mu.Unlock()
		}
	})
	c := independentOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	state := answerAllowing(t, c, ctx, nil, taskDevelopment)
	t.Logf("restart stop=%+v", state)
	if state.Status != StatusWaitingDecision || len(state.Pending) == 0 {
		t.Fatalf("no restart question")
	}
	if _, err := c.Answer(ctx, state.Pending[0].DecisionID, "restart-session"); err != nil {
		t.Fatal(err)
	}
	final := answerAllowing(t, c, ctx, nil)
	t.Logf("after restart=%+v", final)
	mu.Lock()
	copyCalls := append([]NativeCall(nil), calls...)
	mu.Unlock()
	if len(copyCalls) < 3 {
		t.Fatalf("only %d development calls", len(copyCalls))
	}
	for i, call := range copyCalls {
		var req struct{ Identity string }
		b, err := os.ReadFile(filepath.Join(call.JobDir, "native-request.json"))
		if err != nil {
			t.Fatal(err)
		}
		json.Unmarshal(b, &req)
		t.Logf("development call %d generation %d role=%s identity=%q", i+1, call.Generation, call.Role, req.Identity)
		if i >= 2 && req.Identity != "" {
			t.Errorf("explicit restart resumed the refused identity %q", req.Identity)
		}
	}
}

// Public owner helper for actual owner-crash tests (not synthetic store rows).
type independentOwnerSpec struct{ Dir, RunID, Binary, Project, Base, Mode string }

func TestIndependentK5Owner(t *testing.T) {
	file := os.Getenv("INDEPENDENT_K5_OWNER")
	if file == "" {
		return
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var s independentOwnerSpec
	if err = json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	cfg := ComposeConfig{Dir: s.Dir, RunID: s.RunID, Binary: s.Binary, Spec: ComposeSpec{Project: s.Project, Base: s.Base, Requirement: "Make state.txt contain ok. Do not change anything else.", Checks: []Check{{ID: "state", Argv: []string{"/bin/sh", "check.sh"}}}, Gates: []string{"complexity-gate"}, RepairLimit: 1}, Parallelism: 2}
	scripts := composeScripts{}
	if s.Mode == "repair" {
		scripts["development#1"] = scriptedAnswer{Status: "PASS", Summary: "no fix", NoWrite: true}
		scripts["development#2"] = scriptedAnswer{Status: "PASS", Summary: "repair in flight", SleepSecs: 60}
	}
	if s.Mode == "check" {
		cfg.Spec.Checks[0].Argv = []string{"/bin/sh", "-c", "sleep 2; test \"$(cat state.txt)\" = ok"}
	}
	if s.Mode == "gate" {
		scripts[gateTaskID("complexity-gate")] = scriptedAnswer{Status: "PASS", Summary: "gate in flight", SleepSecs: 2}
	}
	cfg.Binding = independentBinding(t, cfg, scripts, func(call NativeCall) {
		if call.Role == "repair" {
			os.WriteFile(filepath.Join(cfg.Dir, "repair-call-started"), []byte(call.JobDir), 0600)
		}
	})
	if s.Mode == "check-runtime" {
		cfg.Spec.Checks[0].Argv = []string{"/bin/sh", "-c", "sleep 2; exit 127"}
	}
	if s.Mode == "gate-blocked" {
		scripts[gateTaskID("complexity-gate")] = scriptedAnswer{Status: "BLOCKED", Summary: "temporary access problem", SleepSecs: 2}
	}
	if s.Mode == "product-garbage" {
		scripts[taskProduct] = scriptedAnswer{Status: "PASS", Summary: "malformed output", Garbage: true, SleepSecs: 2}
	}
	if s.Mode == "product-fail" {
		scripts[taskProduct] = scriptedAnswer{Status: "FAIL", Summary: "requirements need correction", Findings: scriptedFindings, SleepSecs: 2}
	}
	if s.Mode == "repair-continue-gap" {
		scripts["development#1"] = scriptedAnswer{Status: "PASS", Summary: "not fixed", NoWrite: true}
		scripts["development#2"] = scriptedAnswer{Status: "CONTINUE", Summary: "continuing repair", NoWrite: true}
	}
	if s.Mode == "continue-gap" || s.Mode == "repair-continue-gap" {
		if s.Mode == "continue-gap" {
			scripts["development#1"] = scriptedAnswer{Status: "CONTINUE", Summary: "continuing", NoWrite: true}
		}
		normal := cfg.Binding
		cfg.Binding = func(call NativeCall) (map[string]any, error) {
			if call.TaskID == taskDevelopment && call.Attempt == 2 {
				os.WriteFile(filepath.Join(cfg.Dir, "continue-gap"), []byte(call.JobDir), 0600)
				select {}
			}
			return normal(call)
		}
	}
	if s.Mode == "held-after-retry" {
		scripts["development#1"] = scriptedAnswer{Status: "CONTINUE", Summary: "continue", NoWrite: true}
		scripts["development#2"] = scriptedAnswer{Status: "BLOCKED", Summary: "temporary access problem", NoWrite: true}
	}
	if s.Mode == "held-gate" || s.Mode == "held-after-retry" {
		normal := cfg.Binding
		ordinal := 0
		cfg.Binding = func(call NativeCall) (map[string]any, error) {
			target := gateTaskID("complexity-gate")
			if s.Mode == "held-after-retry" {
				target = taskDevelopment
			}
			if call.TaskID != target {
				return normal(call)
			}
			ordinal++
			if s.Mode == "held-after-retry" && ordinal <= 2 {
				return normal(call)
			}
			steps := dshScriptSteps(call.JobDir, passAnswer())
			openResume := steps[2]
			response := steps[2]
			response.On = `"outcome":"selected"`
			response.Send = `{"id":3,"result":{"stopReason":"end_turn"}}`
			hold := peerStep{On: `"method":"session/prompt"`, Send: `{"jsonrpc":"2.0","id":41,"method":"session/request_permission","params":{"sessionId":"s-1","toolCall":{"toolCallId":"call-9"},"options":[{"optionId":"allow-once","name":"Allow once","kind":"allow_once"},{"optionId":"reject-once","name":"Reject","kind":"reject_once"}]}}`}
			// Both session-open handlers stand: a same-work retry resumes
			// its recorded identity (session/resume), a fresh round opens
			// session/new — the scripted peer must answer whichever the
			// call actually sends, or a legitimate resume hangs.
			steps = []peerStep{steps[0], steps[1], openResume, hold, response, steps[3]}
			return dshBindingFor(t, fixedPeerPath(t, steps, filepath.Join(call.JobDir, "peer-echo.txt"))), nil
		}
	}
	c, err := ComposeHarnessV1(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	state := answerAllowing(t, c, context.Background(), nil)
	if (s.Mode == "held-gate" || s.Mode == "held-after-retry") && state.Status == StatusWaitingInteraction {
		select {}
	}
	os.WriteFile(filepath.Join(cfg.Dir, "owner-ended"), []byte(fmt.Sprintf("%+v", state)), 0600)
}
func startIndependentOwner(t *testing.T, cfg ComposeConfig, mode string) *exec.Cmd {
	s := independentOwnerSpec{cfg.Dir, cfg.RunID, cfg.Binary, cfg.Spec.Project, cfg.Spec.Base, mode}
	b, _ := json.Marshal(s)
	file := filepath.Join(t.TempDir(), "owner.json")
	os.WriteFile(file, b, 0600)
	bin, _ := os.Executable()
	cmd := exec.Command(bin, "-test.run=^TestIndependentK5Owner$", "-test.timeout=120s")
	cmd.Env = append(os.Environ(), "INDEPENDENT_K5_OWNER="+file)
	log, err := os.Create(filepath.Join(cfg.Dir, "owner.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait(); log.Close() })
	return cmd
}
func waitIndependentFile(t *testing.T, root, pattern string, bound time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		if len(matches) > 0 {
			return matches[0]
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting %s", pattern)
	return ""
}
func TestIndependentK5RepairCrash(t *testing.T) {
	cfg := independentConfig(t, nil)
	cfg.Spec.RepairLimit = 1
	owner := startIndependentOwner(t, cfg, "repair")
	marker := waitIndependentFile(t, cfg.Dir, "repair-call-started", 40*time.Second)
	body, _ := os.ReadFile(marker)
	job := strings.TrimSpace(string(body))
	waitIndependentFile(t, job, "native-process.json", 10*time.Second)
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, job)
	c := independentOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	before := supervisorAlive(job)
	got := c.Drive(ctx)
	after := supervisorAlive(job)
	t.Logf("repair supervisor alive before=%v after=%v resumed=%+v", before, after, got)
	if got.Status != StatusObserved {
		t.Errorf("live in-flight repair must be observed without charging another repair allowance; got %+v", got)
	}
	if got.Status == StatusWaitingDecision && len(got.Pending) > 0 {
		if _, err := c.Answer(context.Background(), got.Pending[0].DecisionID, "continue"); err != nil {
			t.Fatal(err)
		}
		moreCtx, moreCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer moreCancel()
		next := c.Drive(moreCtx)
		t.Logf("after extra continue=%+v originalRepairStillAlive=%v developmentTurns=%d", next, supervisorAlive(job), len(turnDirs(t, c, taskDevelopment)))
		if supervisorAlive(job) && len(turnDirs(t, c, taskDevelopment)) > 2 {
			t.Errorf("a duplicate repair ran while the original repair supervisor remained alive")
		}
	}
}
func TestIndependentK5CompletedCheckCrash(t *testing.T) {
	cfg := independentConfig(t, nil)
	cfg.Spec.RepairLimit = 1
	cfg.Spec.Checks[0].Argv = []string{"/bin/sh", "-c", "sleep 2; test \"$(cat state.txt)\" = ok"}
	owner := startIndependentOwner(t, cfg, "check")
	proc := waitIndependentFile(t, cfg.Dir, "jobs/check-state/*/turn-*/process.json", 40*time.Second)
	job := filepath.Dir(proc)
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, job)
	waitIndependentFile(t, job, "exit.json", 10*time.Second)
	c := independentOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got := c.Drive(ctx)
	t.Logf("recovered completed check=%+v", got)
	for _, q := range got.Pending {
		if q.TaskID == checkTaskID("state") && strings.Contains(q.Body, "session") {
			t.Errorf("completed check with receipt is misclassified as unavailable native session")
		}
	}
}
func TestIndependentK5CompletedGateCrash(t *testing.T) {
	cfg := independentConfig(t, nil)
	cfg.Spec.RepairLimit = 1
	owner := startIndependentOwner(t, cfg, "gate")
	proc := waitIndependentFile(t, cfg.Dir, "jobs/gate-complexity-gate/*/turn-*/native-process.json", 40*time.Second)
	job := filepath.Dir(proc)
	waitIndependentFile(t, cfg.Dir, "outcomes/check-state/*.json", 10*time.Second)
	time.Sleep(80 * time.Millisecond)
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, job)
	waitIndependentFile(t, job, "exit.json", 10*time.Second)
	c := independentOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got := c.Drive(ctx)
	t.Logf("recovered completed gate=%+v", got)
	if got.Status != StatusComplete {
		t.Errorf("valid gate receipt should recover to completion without another native call, got %+v", got)
	}
}

func TestIndependentK5PlanEvidence(t *testing.T) {
	cfg := independentConfig(t, nil)
	c := independentOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got := answerAllowing(t, c, ctx, nil, askTask(taskPlan))
	t.Logf("plan question=%+v", got)
	if got.Status != StatusWaitingDecision || len(got.Pending) == 0 {
		t.Fatalf("no plan question")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got.Pending[0].Body), &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"product", "technical"} {
		ref, _ := body[field].(string)
		root, _ := body["artifacts"].(string)
		path := filepath.Join(root, ref+".json")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("plan approval %s reference does not resolve to reviewable artifact: %s (%v)", field, path, err)
		}
	}
}
func TestIndependentK5RepeatedPlanAfterReviewRetry(t *testing.T) {
	scripts := composeScripts{"product-review#1": {Status: "FAIL", Summary: "first blocker", Findings: scriptedFindings}, "product-review#2": {Status: "FAIL", Summary: "second blocker", Findings: scriptedFindings}}
	cfg := independentConfig(t, scripts)
	c := independentOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got := answerAllowing(t, c, ctx, nil)
	t.Logf("repeat review retry final=%+v", got)
	if got.Status != StatusComplete {
		t.Errorf("two ordinary pre-review retries do not complete: %v", got.Err)
	}
}

func TestIndependentK5RecoveredGatePermission(t *testing.T) {
	cfg := independentConfig(t, nil)
	cfg.Spec.RepairLimit = 1
	owner := startIndependentOwner(t, cfg, "held-gate")
	pending := waitIndependentFile(t, cfg.Dir, "jobs/gate-complexity-gate/*/turn-*/interaction-pending.json", 40*time.Second)
	job := filepath.Dir(pending)
	waitIndependentFile(t, cfg.Dir, "outcomes/check-state/*.json", 10*time.Second)
	time.Sleep(80 * time.Millisecond)
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, job)
	c := independentOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var got ComposeState
	found := false
	for ctx.Err() == nil {
		got = c.Drive(ctx)
		if got.Status == StatusWaitingInteraction && len(got.Held) > 0 {
			found = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("gate native permission still alive=%v final=%+v", supervisorAlive(job), got)
	if !found {
		t.Errorf("public Drive never exposes the recovered gate's existing native permission")
	}
}
