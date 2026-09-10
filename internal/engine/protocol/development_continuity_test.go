package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/runtime"
)

func rejectContinuity(t *testing.T, e *Engine, v RegularEvent) {
	t.Helper()
	s, _ := e.Load()
	v.ExpectedRevision = s.Revision
	ev, err := NewRegularEvent(EventID(fmt.Sprintf("reject-%d", s.Revision)), v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Submit(ev, fingerprintOf(t, e)); err == nil {
		t.Fatalf("unexpected acceptance: %+v", v)
	}
}
func unfinishedDeveloper(t *testing.T, e *Engine, status string) (RegularExecution, RegularExecution) {
	t.Helper()
	x := currentExecution(t, e, "development-worker")
	claimAndAttach(t, e, x)
	raw := preparationResult(x)
	raw.Status = status
	raw.Message = "Actual unfinished work remains"
	raw.Manifest = []string{"partial.go"}
	submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &raw})
	sourceUnavailable(t, e, x)
	y := currentExecution(t, e, "development-worker")
	if y.Claim.Operation != "resume" || y.Identity != raw.Identity || y.Claim.Task != x.Claim.Task || y.Number != 2 {
		t.Fatalf("binding reset: %+v", y)
	}
	return x, y
}
func TestDeveloperContinuationCannotResetByStatusOrRetry(t *testing.T) {
	for _, status := range []string{"CONTINUE", "FAIL", "RUNTIME_ERROR"} {
		t.Run(status, func(t *testing.T) {
			e := preparationEngine(t)
			gitReceipt(t, e, "git-base")
			first, next := unfinishedDeveloper(t, e, status)
			spawn := next.Claim
			spawn.Operation = "spawn"
			spawn.Identity = ""
			rejectContinuity(t, e, RegularEvent{Operation: "CLAIM", Claim: &spawn})
			replace := next.Claim
			replace.Operation = "replace"
			rejectContinuity(t, e, RegularEvent{Operation: "CLAIM", Claim: &replace})
			submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &next.Claim})
			src := ExecutionSource{ActionID: next.Claim.ActionID, AttemptID: next.Claim.AttemptID, Provider: "codex", Identity: "another-developer", Status: "RUNNING"}
			rejectContinuity(t, e, RegularEvent{Operation: "SOURCE", Source: &src})
			src.Identity = next.Identity
			submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &src})
			raw := preparationResult(next)
			raw.Identity = next.Identity
			raw.Status = "FAIL"
			raw.Message = "self-test still fails"
			submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &raw})
			src.Status = "UNAVAILABLE"
			src.Reason = "fixture source"
			submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &src})
			regularChoice(t, e, RegularRequest{Action: "preparation-retry", Target: next.Claim.Task.String(), Reason: "authorized same developer continuation"}, "confirm")
			third := currentExecution(t, e, "development-worker")
			if third.Identity != next.Identity || third.Number != 3 || third.Claim.Task != first.Claim.Task {
				t.Fatal("retry reset", third)
			}
			s, _ := e.Load()
			if s.State.RegularExecutions[first.Claim.ActionID].PreparationResult.Manifest[0] != "partial.go" {
				t.Fatal("lost old result")
			}
			spawn = third.Claim
			spawn.Operation = "spawn"
			spawn.Identity = ""
			rejectContinuity(t, e, RegularEvent{Operation: "CLAIM", Claim: &spawn})
		})
	}
}
func TestDeveloperRecoveryRequiresExactNativeFailureAndSingleConsumption(t *testing.T) {
	for _, outcome := range []string{"missing", "timeout", "rate-limit", "resumable", "wrong-identity"} {
		t.Run(outcome, func(t *testing.T) {
			e := preparationEngine(t)
			gitReceipt(t, e, "git-base")
			first, next := unfinishedDeveloper(t, e, "CONTINUE")
			submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &next.Claim})
			response := map[string]any{"id": 3, "error": map[string]any{"code": -32600, "message": "no rollout found for thread id " + next.Identity}}
			if outcome == "resumable" {
				response = map[string]any{"id": 3, "result": map[string]any{"thread": map[string]string{"id": next.Identity}}}
			}
			if outcome == "rate-limit" {
				response["error"] = map[string]any{"code": 429, "message": "rate limited"}
			}
			raw, _ := json.Marshal(response)
			recovery := DeveloperRecovery{Claim: next.Claim, Identity: next.Identity, StoppedAction: first.Claim.ActionID, ProcessID: 123, WorkspaceView: "sha256:view", NativeMethod: "thread/resume", NativeResponse: raw}
			if outcome == "timeout" {
				recovery.TransportError = "deadline exceeded"
			}
			if outcome == "wrong-identity" {
				recovery.Identity = "other"
				rejectContinuity(t, e, RegularEvent{Operation: "DEVELOPER_RECOVERY", DeveloperRecovery: &recovery})
				return
			}
			submitRegular(t, e, RegularEvent{Operation: "DEVELOPER_RECOVERY", DeveloperRecovery: &recovery})
			claim := next.Claim
			claim.Operation = "replace"
			if outcome != "missing" {
				rejectContinuity(t, e, RegularEvent{Operation: "CLAIM", Claim: &claim})
				return
			}
			submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &claim})
			rejectContinuity(t, e, RegularEvent{Operation: "CLAIM", Claim: &claim})
			rejectContinuity(t, e, RegularEvent{Operation: "DEVELOPER_RECOVERY", DeveloperRecovery: &recovery})
			src := ExecutionSource{ActionID: next.Claim.ActionID, AttemptID: next.Claim.AttemptID, Provider: "codex", Identity: "replacement-native", Status: "RUNNING"}
			submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &src})
			s, _ := e.Load()
			if len(s.State.PendingHostActions) != 0 || !s.State.RegularExecutions[next.Claim.ActionID].ReplacementClaimed {
				t.Fatal("replacement intent not consumed")
			}
		})
	}
}
func TestDeveloperPlanRebindRetainsOpenTaskAndArtifacts(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "git-base")
	_, next := unfinishedDeveloper(t, e, "FAIL")
	s, _ := e.Load()
	r := s.State.Regular
	old := r.Preparation
	key := *old.DevelopmentTask
	old.ExtraAttempts = map[string]int{key.String(): 1}
	r.Plan.Batches[0].ID = "renumbered"
	r.Plan.Digest = "revised-plan"
	prepareProjection(r)
	if *r.Preparation.DevelopmentTask != key || batchTask(r.Preparation, "renumbered") != key || r.Preparation.Resources["development"] != old.Resources["development"] || r.Preparation.ExtraAttempts[key.String()] != 1 || currentPreparationClaim(r, next.Claim) {
		t.Fatal("plan rebind reset open development")
	}
	binding := r.Preparation.BindingDigest
	prepareProjection(r)
	if r.Preparation.BindingDigest != binding || *r.Preparation.DevelopmentTask != key {
		t.Fatal("repeated plan reset")
	}
}
func TestDeveloperContinuationChargesOnlyNewNativeTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	first := `{"type":"session_meta","payload":{"id":"same-session"}}
{"timestamp":"2026-09-06T16:00:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"one"}}
{"type":"token_usage_record","payload":{"response_id":"first","usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":20}}}
{"timestamp":"2026-09-06T16:00:03Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"one"}}
`
	if err := os.WriteFile(path, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	s := &State{Cost: &cost.RunCost{}, RegularExecutions: map[string]RegularExecution{"one": {Identity: "same-session"}, "two": {Identity: "same-session"}}}
	src := ExecutionSource{ActionID: "one", Identity: "same-session", Status: "STOPPED", TranscriptPath: path}
	if err := settleRegularSource(s, src, s.RegularExecutions["one"]); err != nil {
		t.Fatal(err)
	}
	second := `{"timestamp":"2026-09-06T16:10:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"two"}}
{"type":"token_usage_record","payload":{"response_id":"second","usage":{"input_tokens":200,"cached_input_tokens":80,"output_tokens":30}}}
{"timestamp":"2026-09-06T16:10:02Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"two"}}
`
	if err := os.WriteFile(path, []byte(first+second), 0600); err != nil {
		t.Fatal(err)
	}
	src.ActionID = "two"
	if err := settleRegularSource(s, src, s.RegularExecutions["two"]); err != nil {
		t.Fatal(err)
	}
	entry := s.Cost.Dispatches["two"]
	if s.Cost.TotalInputTokens != 300 || s.Cost.OutputTokens != 50 || entry.TotalInputTokens != 200 || entry.Timing == nil || entry.Timing.WallTimeMillis != 2000 {
		t.Fatalf("usage=%+v entry=%+v", s.Cost, entry)
	}
	after, _ := time.Parse(time.RFC3339, "2026-09-06T16:00:03Z")
	timing, err := cost.CodexTaskTimingAfter(path, after)
	if err != nil || timing.WallTimeMillis != 2000 {
		t.Fatal(timing, err)
	}
}

func TestDeveloperRenumberedPlanNeedsExistingQuotaAndIdentity(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "git-base")
	first, next := unfinishedDeveloper(t, e, "RUNTIME_ERROR")
	regularChoice(t, e, RegularRequest{Action: "plan-change", Reason: "rename batch without changing unfinished scope"}, "confirm")
	review := currentExecution(t, e, "start-readiness")
	claimAndAttach(t, e, review)
	raw := resultFor(review)
	raw.GranularityReview = planForReview(review.Claim.RequirementRevision)
	raw.GranularityReview.Batches[0].ID = "renumbered"
	submitRegular(t, e, RegularEvent{Operation: "REVIEW_RESULT", Review: &raw})
	sourceUnavailable(t, e, review)
	adjudicatePass(t, e, "start-readiness")
	s, _ := e.Load()
	regularChoice(t, e, RegularRequest{Action: "topology", PlanDigest: s.State.Regular.Plan.Digest, Reason: "same repository"}, "confirm")
	s, _ = e.Load()
	if !s.State.Attempts[next.Claim.Task].RetryExhausted || *s.State.Regular.Preparation.DevelopmentTask != next.Claim.Task {
		t.Fatal("plan reset consumed quota")
	}
	regularChoice(t, e, RegularRequest{Action: "preparation-retry", Target: next.Claim.Task.String(), Reason: "continue within existing identity"}, "confirm")
	third := currentExecution(t, e, "development-worker")
	if third.Number != 3 || third.Identity != next.Identity || third.PreviousAction != first.Claim.ActionID {
		t.Fatalf("renumbered plan lost native history: %+v", third)
	}
	spawn := third.Claim
	spawn.Operation = "spawn"
	spawn.Identity = ""
	rejectContinuity(t, e, RegularEvent{Operation: "CLAIM", Claim: &spawn})
}

func TestDeveloperReplacementRetainsConsumedTokenContinue(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "git-base")
	first, next := unfinishedDeveloper(t, e, "CONTINUE")
	snap, _ := e.Load()
	s := snap.State
	cap := int64(1)
	s.Cost.Dispatches[first.Claim.ActionID] = cost.DispatchCost{TotalInputTokens: 1, Source: cost.SourceTranscript}
	s.Regular.Settings.Values.TokenCap = &cap
	s.Regular.Guard.ContinueTasks = []runtime.TaskKey{next.Claim.Task}
	if err := e.claimRegular(s, next.Claim); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"id": 3, "error": map[string]any{"code": -32600, "message": "no rollout found for thread id " + next.Identity}})
	recovery := DeveloperRecovery{Claim: next.Claim, Identity: next.Identity, StoppedAction: first.Claim.ActionID, ProcessID: 1, WorkspaceView: "view", NativeMethod: "thread/resume", NativeResponse: raw}
	if err := acceptDeveloperRecovery(s, recovery); err != nil {
		t.Fatal(err)
	}
	replacement := next.Claim
	replacement.Operation = "replace"
	if err := e.claimRegular(s, replacement); err != nil {
		t.Fatal(err)
	}
	if len(s.Regular.Guard.ContinueTasks) != 0 {
		t.Fatal("replacement fabricated another continue authorization")
	}
	if err := e.claimRegular(s, replacement); err == nil {
		t.Fatal("replacement authorization replayed")
	}
}
