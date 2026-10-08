//go:build !windows

package harness

// The context that travels with each flow edge: a user retry resumes the
// blocked session instead of replacing it, a continuation (live or
// recovered) carries the unfinished-work feedback under the same session
// identity, ordinary retries and continuations consume no business repair
// round, the gate prompt references the generated comparison inputs, and
// the terminal boundary holds against reopening.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecoveredContinuationKeepsSessionAndFeedback(t *testing.T) {
	cfg := recoveryConfig(t, nil)
	cfg.Spec.RepairLimit = 1
	owner := startRecoveryOwner(t, cfg, "continue-gap")
	marker := waitForPattern(t, cfg.Dir, "continue-gap")
	b, _ := os.ReadFile(marker)
	previous := filepath.Join(filepath.Dir(string(b)), "turn-01")
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, previous)
	c := openScenario(t, cfg)
	ctx := context.Background()
	var got ComposeState
	for {
		got = c.Drive(ctx)
		if got.Status != StatusObserved {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Logf("recovered state=%+v", got)
	dirs := turnDirs(t, c, taskDevelopment)
	for _, dir := range dirs {
		if dir == previous {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "native-request.json"))
		if err != nil {
			t.Fatal(err)
		}
		var req struct {
			Identity string
			Prompt   string
		}
		if err = json.Unmarshal(data, &req); err != nil {
			t.Fatal(err)
		}
		t.Logf("next native identity=%q job=%s", req.Identity, dir)
		if req.Identity != "s-1" {
			t.Errorf("continuation lost original identity")
		}
		for _, line := range strings.Split(req.Prompt, "\n") {
			if strings.HasSuffix(line, "/task.json") {
				task, err := os.ReadFile(line)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("continuation task.json=%s", task)
				if !strings.Contains(string(task), "unfinished-work") {
					t.Errorf("recovered continuation lost unfinished-work feedback")
				}
			}
		}
	}
	if len(dirs) != 2 {
		t.Errorf("expected exactly 2 development turns; got %d", len(dirs))
	}
	if got.Status != StatusComplete {
		t.Errorf("recovered continuation never completed: %+v", got)
	}
	if c.repairRounds() != 0 {
		t.Errorf("CONTINUE consumed a repair round: %d", c.repairRounds())
	}
}

func TestBlockedRetryResumesTheSession(t *testing.T) {
	for _, task := range []string{taskDevelopment, taskProduct, taskQAReview, gateTaskID("complexity-gate")} {
		t.Run(task, func(t *testing.T) {
			scripts := composeScripts{task + "#1": {Status: "BLOCKED", Summary: "temporary access issue", NoWrite: true}}
			cfg := recoveryConfig(t, scripts)
			c := openScenario(t, cfg)
			ctx := context.Background()
			got := answerAllowing(t, c, ctx, nil)
			if got.Status != StatusComplete {
				t.Fatalf("retry control failed: %+v", got)
			}
			dirs := turnDirs(t, c, task)
			for _, dir := range dirs {
				ans, _ := os.ReadFile(filepath.Join(dir, "answer.json"))
				if strings.Contains(string(ans), "BLOCKED") {
					continue
				}
				var req struct{ Identity string }
				data, _ := os.ReadFile(filepath.Join(dir, "native-request.json"))
				json.Unmarshal(data, &req)
				echo, _ := os.ReadFile(filepath.Join(dir, "peer-echo.txt"))
				t.Logf("after user retry: task=%s native identity=%q session/new=%v session/resume=%v", task, req.Identity, strings.Contains(string(echo), `"method":"session/new"`), strings.Contains(string(echo), `"method":"session/resume"`))
				if req.Identity != "s-1" {
					t.Errorf("retry silently replaced the blocked session")
				}
			}
		})
	}
}

func TestOrdinaryRetryIsNotARepairRound(t *testing.T) {
	scripts := composeScripts{
		"development#1": {Status: "PASS", Summary: "not fixed", NoWrite: true},
		"development#2": {Status: "BLOCKED", Summary: "temporary access issue", NoWrite: true},
	}
	cfg := recoveryConfig(t, scripts)
	c := openScenario(t, cfg)
	ctx := context.Background()
	got := answerAllowing(t, c, ctx, nil)
	t.Logf("one repair with a retry: state=%+v reportedRounds=%d", got, c.repairRounds())
	if got.Status != StatusComplete {
		t.Fatalf("control failed: %+v", got)
	}
	findings := evaluateSingleDriverFindings(cfg.Dir)
	t.Logf("invariant findings=%v", findings)
	if len(findings) > 0 {
		t.Errorf("ordinary retry counted as a duplicate business repair")
	}
	t.Logf("current check brief=%s", c.lastBrief(checkTaskID("state")))
	before := len(turnDirs(t, c, checkTaskID("state")))
	again := c.Drive(ctx)
	after := len(turnDirs(t, c, checkTaskID("state")))
	t.Logf("Drive after COMPLETE: beforeCheckCalls=%d afterCheckCalls=%d state=%+v checkBrief=%s", before, after, again, c.lastBrief(checkTaskID("state")))
	if after != before {
		t.Errorf("terminal Drive executed checks again")
	}

	if c.repairRounds() != 1 {
		t.Errorf("BLOCKED retry inflated completed repair rounds to %d", c.repairRounds())
	}
}

func TestRecoveredRepairContinuationCompletesOneRound(t *testing.T) {
	cfg := recoveryConfig(t, nil)
	cfg.Spec.RepairLimit = 1
	owner := startRecoveryOwner(t, cfg, "repair-continue-gap")
	marker := waitForPattern(t, cfg.Dir, "continue-gap")
	b, _ := os.ReadFile(marker)
	previous := filepath.Join(filepath.Dir(string(b)), "turn-01")
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, previous)
	c := openScenario(t, cfg)
	ctx := context.Background()
	var got ComposeState
	for {
		got = c.Drive(ctx)
		if got.Status != StatusObserved {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Logf("continued repair state=%+v rounds=%d", got, c.repairRounds())
	if got.Status != StatusComplete {
		t.Fatalf("control failed: %+v", got)
	}
	findings := evaluateSingleDriverFindings(cfg.Dir)
	t.Logf("invariant findings=%v", findings)
	if len(findings) > 0 {
		t.Errorf("ordinary continuation counted as a duplicate business repair")
	}
	if c.repairRounds() != 1 {
		t.Errorf("CONTINUE inflated completed repair rounds")
	}
}

func TestGatePromptReferencesComparisonInputs(t *testing.T) {
	cfg := recoveryConfig(t, nil)
	c := openScenario(t, cfg)
	ctx := context.Background()
	got := answerAllowing(t, c, ctx, nil)
	if got.Status != StatusComplete {
		t.Fatalf("control failed: %+v", got)
	}
	dirs := turnDirs(t, c, gateTaskID("complexity-gate"))
	if len(dirs) != 1 {
		t.Fatalf("gate calls=%d", len(dirs))
	}
	data, err := os.ReadFile(filepath.Join(dirs[0], "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var req struct{ Prompt string }
	json.Unmarshal(data, &req)
	for _, line := range strings.Split(req.Prompt, "\n") {
		if !strings.HasSuffix(line, "/task.json") {
			continue
		}
		var task map[string]any
		b, err := os.ReadFile(line)
		if err != nil {
			t.Fatal(err)
		}
		json.Unmarshal(b, &task)
		_, exists := os.Stat(filepath.Join(filepath.Dir(line), "comparison", "comparison.json"))
		t.Logf("gate task=%s; comparison file exists=%v; input=%s", line, exists == nil, b)
		if _, ok := task["comparison"]; !ok {
			t.Errorf("gate prompt task.json does not reference the generated comparison inputs")
		}
	}
}

func TestLiveContinuationCarriesUnfinishedWorkFeedback(t *testing.T) {
	cfg := recoveryConfig(t, composeScripts{"development#1": {Status: "CONTINUE", Summary: "continuing", NoWrite: true}})
	c := openScenario(t, cfg)
	ctx := context.Background()
	got := answerAllowing(t, c, ctx, nil)
	if got.Status != StatusComplete {
		t.Fatalf("control failed: %+v", got)
	}
	found := false
	for _, dir := range turnDirs(t, c, taskDevelopment) {
		if filepath.Base(dir) != "turn-02" {
			continue
		}
		var req struct{ Prompt string }
		data, _ := os.ReadFile(filepath.Join(dir, "native-request.json"))
		json.Unmarshal(data, &req)
		for _, line := range strings.Split(req.Prompt, "\n") {
			if strings.HasSuffix(line, "/task.json") {
				data, err := os.ReadFile(line)
				if err != nil {
					t.Fatal(err)
				}
				found = strings.Contains(string(data), "unfinished-work")
				t.Logf("live continuation contains unfinished-work=%v", found)
			}
		}
	}
	if !found {
		t.Fatal("live control also lacks feedback")
	}
}

func TestTerminalReopenExecutesNothingNew(t *testing.T) {
	for _, repair := range []bool{false, true} {
		name := "no-repair"
		scripts := composeScripts{}
		if repair {
			name = "one-repair"
			scripts["development#1"] = scriptedAnswer{Status: "PASS", Summary: "not fixed", NoWrite: true}
		}
		t.Run(name, func(t *testing.T) {
			cfg := recoveryConfig(t, scripts)
			c := openScenario(t, cfg)
			ctx := context.Background()
			got := answerAllowing(t, c, ctx, nil)
			if got.Status != StatusComplete {
				t.Fatalf("control failed: %+v", got)
			}
			before := len(turnDirs(t, c, checkTaskID("state")))
			beforeBrief := string(c.lastBrief(checkTaskID("state")))
			c.Close()
			reopened := openScenario(t, cfg)
			again := reopened.Drive(ctx)
			after := len(turnDirs(t, reopened, checkTaskID("state")))
			t.Logf("repair=%v terminal before/after=%s/%s; checks=%d/%d; round=%d; briefs=%s -> %s", repair, got.Terminal, again.Terminal, before, after, reopened.repairRounds(), beforeBrief, reopened.lastBrief(checkTaskID("state")))
			if before != after {
				t.Error("reopening COMPLETE executed a new check")
			}
		})
	}
}
