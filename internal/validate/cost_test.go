package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/lifecycle"
)

// costCodexFixture mirrors the current real Codex rollout token_count shape
// with incremental last_token_usage; the third event is a duplicate used to
// prove event dedupe at the ledger level.
const costCodexFixture = `{"timestamp":"2026-07-19T23:17:10.000Z","type":"session_meta","payload":{"session_id":"s-1"}}
{"timestamp":"2026-07-19T23:17:11.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"cache_write_input_tokens":0,"output_tokens":30,"reasoning_output_tokens":5,"total_tokens":130},"last_token_usage":{"input_tokens":100,"cached_input_tokens":10,"cache_write_input_tokens":0,"output_tokens":30,"reasoning_output_tokens":5,"total_tokens":130},"model_context_window":353400}}}
{"timestamp":"2026-07-19T23:17:12.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":250,"cached_input_tokens":25,"cache_write_input_tokens":3,"output_tokens":80,"reasoning_output_tokens":12,"total_tokens":330},"last_token_usage":{"input_tokens":150,"cached_input_tokens":15,"cache_write_input_tokens":3,"output_tokens":50,"reasoning_output_tokens":7,"total_tokens":200}}}}
{"timestamp":"2026-07-19T23:17:12.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":150,"cached_input_tokens":15,"cache_write_input_tokens":3,"output_tokens":50,"reasoning_output_tokens":7,"total_tokens":200}}}}
`

func writeCostFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte(costCodexFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// stubCodexTranscript replaces the native lifecycle with one that reports the
// given file as the dispatch's recorded Codex transcript. An uninstalled test
// binary binds as the lenient default provider (which has no Codex transcript
// path), so cost tests simulate an installed Codex host through the stub; the
// real capture→transcript round-trip is owned by the lifecycle package tests.
func stubCodexTranscript(t *testing.T, transcript string) {
	t.Helper()
	prior := workflowLifecycle
	workflowLifecycle = &workflowLifecycleStub{verification: lifecycle.Verification{Outcome: lifecycle.Verified}, transcript: transcript}
	t.Cleanup(func() { workflowLifecycle = prior })
}

// prepareClaimedAction prepares the action dispatch and claims it under the
// given reviewer identity so the lifecycle binding carries that identity.
func prepareClaimedAction(t *testing.T, root, pkg string, state RunState, target, reviewer string) string {
	t.Helper()
	if _, err := PrepareAction(root, pkg, state.RunID, target); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	dispatchID := openDispatchID(loaded, "action", target)
	if _, err := ClaimDispatch(root, pkg, state.RunID, dispatchID, reviewer); err != nil {
		t.Fatal(err)
	}
	return dispatchID
}

func TestCostBackfillParsesCapturedTranscript(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "cost-backfill"), "custom", []string{"quality"})
	dispatchID := prepareClaimedAction(t, root, pkg, state, "start-readiness", "cost-readiness")
	stubCodexTranscript(t, writeCostFixture(t))
	state, err := RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "PASS", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := state.Cost.Dispatches[dispatchID]
	if !ok {
		t.Fatalf("dispatch cost entry missing: %#v", state.Cost)
	}
	// Priced categories only: hit = cached (10+15), miss = input minus
	// cached (90+135), output = output_tokens (30+50); the cache write (3)
	// is not a priced category and is not recorded.
	want := cost.DispatchCost{Target: "start-readiness", Kind: "action", InputCacheHitTokens: 25, InputCacheMissTokens: 225, OutputTokens: 80, TotalInputTokens: 250, Source: cost.SourceTranscript}
	if entry != want {
		t.Fatalf("entry=%+v want %+v", entry, want)
	}
	if state.Cost.TotalInputTokens != 250 || state.Cost.InputCacheHitTokens != 25 || state.Cost.InputCacheMissTokens != 225 || state.Cost.OutputTokens != 80 {
		t.Fatalf("run totals=%+v", state.Cost)
	}
	persisted, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Cost == nil || persisted.Cost.Dispatches[dispatchID] != entry {
		t.Fatalf("cost was not persisted: %#v", persisted.Cost)
	}
}

func TestProductReviewCostBackfillParsesCapturedTranscript(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "cost-product-review"))
	dispatchID := prepareClaimedAction(t, root, pkg, state, "product-review", "cost-product-review")
	stubCodexTranscript(t, writeCostFixture(t))
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := state.Cost.Dispatches[dispatchID]
	if !ok {
		t.Fatalf("dispatch cost entry missing: %#v", state.Cost)
	}
	want := cost.DispatchCost{Target: "product-review", Kind: "action", InputCacheHitTokens: 25, InputCacheMissTokens: 225, OutputTokens: 80, TotalInputTokens: 250, Source: cost.SourceTranscript}
	if entry != want {
		t.Fatalf("entry=%+v want %+v", entry, want)
	}
}

func TestCostBackfillMarksUnavailableWithoutTranscript(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "cost-unavailable"), "custom", []string{"quality"})
	dispatchID := prepareClaimedAction(t, root, pkg, state, "start-readiness", "cost-readiness")
	state, err := RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "PASS", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Cost.Dispatches[dispatchID]
	want := cost.DispatchCost{Target: "start-readiness", Kind: "action", Source: cost.SourceUnavailable}
	if entry != want {
		t.Fatalf("entry=%+v want %+v", entry, want)
	}
	if state.Cost.TotalInputTokens != 0 || state.Cost.InputCacheHitTokens != 0 || state.Cost.InputCacheMissTokens != 0 || state.Cost.OutputTokens != 0 {
		t.Fatalf("unavailable entry must not carry numbers: %+v", state.Cost)
	}
}

func TestCostBackfillIgnoresUnparseableTranscript(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "cost-parse-fail"), "custom", []string{"quality"})
	dispatchID := prepareClaimedAction(t, root, pkg, state, "start-readiness", "cost-readiness")
	fixture := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(fixture, []byte("{\"type\":\"session_meta\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stubCodexTranscript(t, fixture)
	state, err := RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "PASS", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Cost.Dispatches[dispatchID]
	want := cost.DispatchCost{Target: "start-readiness", Kind: "action", Source: cost.SourceUnavailable}
	if entry != want {
		t.Fatalf("unparseable transcript must be unavailable: %+v", entry)
	}
}

func TestCostBackfillIsIdempotentPerDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "cost-idempotent"), "custom", []string{"quality"})
	dispatchID := prepareClaimedAction(t, root, pkg, state, "start-readiness", "cost-readiness")
	stubCodexTranscript(t, writeCostFixture(t))
	state, err := RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "PASS", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := state.Cost
	// A repeat recording of the same dispatch is rejected by the workflow,
	// so idempotence is enforced at the ledger level: recording the same id
	// again must not re-add numbers.
	cost.Record(state.Cost, dispatchID, state.Cost.Dispatches[dispatchID])
	if state.Cost.TotalInputTokens != before.TotalInputTokens || state.Cost.InputCacheHitTokens != before.InputCacheHitTokens || state.Cost.InputCacheMissTokens != before.InputCacheMissTokens || state.Cost.OutputTokens != before.OutputTokens || state.Cost.Dispatches[dispatchID] != before.Dispatches[dispatchID] {
		t.Fatalf("repeat recording re-added numbers: %+v", state.Cost)
	}
}

func TestRunStateWithoutCostLoadsAndSavesUnchanged(t *testing.T) {
	root := t.TempDir()
	state := NewRunState("legacy", "formal", "requirements.md", "rev", "git", "base", "base", "prompt", "catalog", true, []string{"quality"}, nil)
	if state.Cost != nil {
		t.Fatalf("fresh state must have no cost projection: %+v", state.Cost)
	}
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"cost"`) {
		t.Fatalf("state without cost was saved with a cost key: %s", data)
	}
	loaded, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Cost != nil {
		t.Fatalf("legacy state gained a cost projection: %+v", loaded.Cost)
	}
}

func TestSealedSummaryCarriesCostProjection(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "cost-seal")
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, dispatchID, passingExecution(state.QACases), "")
	if err != nil {
		t.Fatal(err)
	}
	for index, gate := range []string{"architecture", "quality"} {
		dispatchID = prepareAndClaim(t, root, pkg, state.RunID, gate, fmt.Sprintf("gate-%d", index+1))
		state, err = RecordGate(root, pkg, state.RunID, gate, dispatchID, "PASS", "", comparedRange(state), nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	completed := map[string]bool{}
	for id, dispatch := range state.Dispatches {
		if dispatch.Status == "COMPLETED" {
			completed[id] = true
		}
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Cost == nil {
		t.Fatalf("seal summary lost the cost projection")
	}
	if len(summary.Cost.Dispatches) != len(completed) {
		t.Fatalf("summary cost dispatches=%d want every completed dispatch (%d)", len(summary.Cost.Dispatches), len(completed))
	}
	for id, entry := range summary.Cost.Dispatches {
		if !completed[id] || entry.Source != cost.SourceUnavailable || entry.TotalInputTokens != 0 {
			t.Fatalf("unmetered dispatch %s must be unavailable with zero numbers: %+v", id, entry)
		}
	}
	if summary.Cost.TotalInputTokens != 0 {
		t.Fatalf("run totals must be zero without transcripts: %+v", summary.Cost)
	}
	data, err := os.ReadFile(RunSummaryPath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"cost"`) {
		t.Fatalf("seal summary file lacks cost: %s", data)
	}
}
