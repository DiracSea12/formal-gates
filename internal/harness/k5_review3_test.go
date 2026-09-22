//go:build !windows

package harness

// Round-3 external-review probes, adopted into the tree by the orchestrating
// agent as this repair's failing baseline (verbatim from
// /private/tmp/k5-review3/snapshot — only this header added). They drive the
// recovery of non-success results, multi-invocation identities and repeated
// questions through public surfaces with real owner exits.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReview3RecoveredNonSuccess(t *testing.T) {
	for _, mode := range []string{"check-runtime", "gate-blocked", "product-fail"} {
		t.Run(mode, func(t *testing.T) {
			cfg := independentConfig(t, nil)
			cfg.Spec.RepairLimit = 1
			task := gateTaskID("complexity-gate")
			if mode == "check-runtime" {
				task = checkTaskID("state")
				cfg.Spec.Checks[0].Argv = []string{"/bin/sh", "-c", "sleep 2; exit 127"}
			}
			if mode == "product-fail" {
				task = taskProduct
			}
			owner := startIndependentOwner(t, cfg, mode)
			marker := "native-process.json"
			if mode == "check-runtime" {
				marker = "process.json"
			}
			file := waitIndependentFile(t, cfg.Dir, "jobs/"+pathSafe(task)+"/*/turn-*/"+marker, 40*time.Second)
			job := filepath.Dir(file)
			if mode == "gate-blocked" {
				waitIndependentFile(t, cfg.Dir, "outcomes/check-state/*.json", 10*time.Second)
				time.Sleep(80 * time.Millisecond)
			}
			owner.Process.Kill()
			owner.Wait()
			defer signalTurn(t, job)
			waitIndependentFile(t, job, "exit.json", 10*time.Second)
			c := independentOpen(t, cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			got := c.Drive(ctx)
			t.Logf("recovered %s => %+v", mode, got)
			if mode == "gate-blocked" {
				for _, g := range c.outcomeGenerations(task) {
					out := c.readOutcome(task, g)
					t.Logf("gate generation=%d outcome=%+v result=%+v", g, out, out.Worker)
				}
				t.Logf("developmentCalls=%d gateCalls=%d repairRounds=%d", len(turnDirs(t, c, taskDevelopment)), len(turnDirs(t, c, task)), c.repairRounds())
			}
			found := false
			for _, q := range got.Pending {
				if q.TaskID == askTask(task) && strings.Join(q.Options, ",") == "retry,abort" {
					found = true
				}
				if q.TaskID == task {
					t.Errorf("known %s result classified as lost session: %+v", mode, q)
				}
			}
			if !found {
				t.Errorf("missing the original result's retry/abort question for %s", mode)
			}
		})
	}
}

func TestReview3ContinueGap(t *testing.T) {
	cfg := independentConfig(t, nil)
	cfg.Spec.RepairLimit = 1
	owner := startIndependentOwner(t, cfg, "continue-gap")
	marker := waitIndependentFile(t, cfg.Dir, "continue-gap", 40*time.Second)
	b, _ := os.ReadFile(marker)
	nextJob := string(b)
	previous := filepath.Join(filepath.Dir(nextJob), "turn-01")
	waitIndependentFile(t, previous, "exit.json", 10*time.Second)
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, previous)
	c := independentOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	got := c.Drive(ctx)
	t.Logf("after completed CONTINUE => %+v", got)
	for _, q := range got.Pending {
		if q.TaskID == taskDevelopment {
			t.Errorf("healthy unfinished session incorrectly requires replacement: %+v", q)
		}
	}
}

func TestReview3HeldAfterRetry(t *testing.T) {
	cfg := independentConfig(t, nil)
	cfg.Spec.RepairLimit = 1
	owner := startIndependentOwner(t, cfg, "held-after-retry")
	file := waitIndependentFile(t, cfg.Dir, "jobs/development/*/turn-01/interaction-pending.json", 40*time.Second)
	job := filepath.Dir(file)
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
	t.Logf("held call=%s alive=%v recovered=%+v", job, supervisorAlive(job), got)
	t.Logf("watcher selected=%s", latestTurnDir(filepath.Join(cfg.Dir, "jobs", "development")))
	if !found {
		t.Errorf("prior invocation turn-02 hides current invocation turn-01 permission")
	}
}

func TestReview3RepeatedVerdictInvariant(t *testing.T) {
	scripts := composeScripts{
		"gate:complexity-gate#1": {Status: "FAIL", Summary: "same remaining finding", Findings: scriptedFindings},
		"gate:complexity-gate#2": {Status: "FAIL", Summary: "same remaining finding", Findings: scriptedFindings},
		"development#2":          {Status: "PASS", Summary: "no change", NoWrite: true},
		"development#3":          {Status: "PASS", Summary: "no change", NoWrite: true},
	}
	cfg := independentConfig(t, scripts)
	c := independentOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got := answerAllowing(t, c, ctx, nil)
	t.Logf("two legitimate repair rounds => %+v rounds=%d", got, c.repairRounds())
	if got.Status != StatusComplete {
		t.Fatalf("normal control failed: %+v", got)
	}
	findings := k5EvaluateSingleDriverFindings(cfg.Dir)
	t.Logf("invariant findings=%v", findings)
	if len(findings) > 0 {
		t.Errorf("invariant conflates distinct evaluate verdicts with identical feedback content")
	}
}

func TestReview3RecoveredRuntimeQuestionRepeated(t *testing.T) {
	cfg := independentConfig(t, nil)
	cfg.Spec.RepairLimit = 1
	owner := startIndependentOwner(t, cfg, "product-garbage")
	file := waitIndependentFile(t, cfg.Dir, "jobs/product-review/*/turn-01/native-process.json", 40*time.Second)
	job := filepath.Dir(file)
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, job)
	waitIndependentFile(t, job, "exit.json", 10*time.Second)
	c := independentOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	first := c.Drive(ctx)
	t.Logf("first rejected answer recovery=%+v", first)
	var questionID string
	for _, q := range first.Pending {
		if q.TaskID == askTask(taskProduct) {
			questionID = q.DecisionID
		}
	}
	if questionID == "" {
		t.Fatalf("first runtime question absent")
	}
	if _, err := c.Answer(ctx, questionID, "retry"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	owner2 := startIndependentOwner(t, cfg, "product-garbage")
	nextJob := ""
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		files, _ := filepath.Glob(filepath.Join(cfg.Dir, "jobs/product-review/*/turn-01/native-process.json"))
		for _, f := range files {
			if filepath.Dir(f) != job {
				nextJob = filepath.Dir(f)
				break
			}
		}
		if nextJob != "" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if nextJob == "" {
		t.Fatal("authorized second product call did not start")
	}
	owner2.Process.Kill()
	owner2.Wait()
	defer signalTurn(t, nextJob)
	waitIndependentFile(t, nextJob, "exit.json", 10*time.Second)
	c2 := independentOpen(t, cfg)
	second := c2.Drive(ctx)
	t.Logf("second rejected answer recovery=%+v", second)
	fresh := false
	for _, q := range second.Pending {
		if q.TaskID == askTask(taskProduct) && q.DecisionID != questionID {
			fresh = true
		}
	}
	next := c2.Drive(ctx)
	t.Logf("next Drive without another answer=%+v productCalls=%d", next, len(turnDirs(t, c2, taskProduct)))
	if !fresh {
		t.Errorf("second distinct runtime failure has no fresh bound question")
	}
	if len(turnDirs(t, c2, taskProduct)) > 2 {
		t.Errorf("old retry answer authorized a third call without a new answer")
	}
}
