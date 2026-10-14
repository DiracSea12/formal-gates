package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/lifecycle"
)

func TestCancelledStoppedDispatchRetainsManagedNativeCost(t *testing.T) {
	for _, scenario := range []struct {
		control protocol.ControlKind
		source  string
	}{
		{protocol.ControlAbort, protocol.CostSourceReady},
		{protocol.ControlReset, protocol.CostSourceReady},
		{protocol.ControlAbort, "missing"},
		{protocol.ControlAbort, "invalid-usage"},
		{protocol.ControlAbort, protocol.CostSourcePending},
	} {
		t.Run(string(scenario.control)+"/"+scenario.source, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
				t.Fatal(err)
			}
			f, run, err := Start(StartOptions{Capacity: 1, Root: root, VCSRunner: newFrozenGitRepo(), Request: StartRequest{RunID: "cancelled-cost", Route: "regular", Provider: meteredProvider, BaseSnapshot: frozenSnapshot, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", meteredProvider)})
			if err != nil {
				t.Fatal(err)
			}
			run, err = f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			action := ""
			for id := range run.PendingActions {
				action = id
			}
			if len(run.PendingActions) != 1 || action == "" {
				t.Fatal("fixture did not issue one original signed worker")
			}
			submit := func(event protocol.Event) {
				t.Helper()
				_, run, err = f.Submit(event)
				if err != nil {
					t.Fatal(err)
				}
			}
			spawn, err := protocol.NewSpawnReceiptEvent("spawn", action, meteredProvider, "original-worker", protocol.SpawnStatusSpawned)
			if err != nil {
				t.Fatal(err)
			}
			submit(spawn)
			want := cost.Usage{InputCacheMissTokens: 5, InputCacheHitTokens: 1, OutputTokens: 2}
			switch scenario.source {
			case protocol.CostSourceReady, "invalid-usage":
				transcript := filepath.Join(root, "native.jsonl")
				line := `{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":1}}}`
				if scenario.source == "invalid-usage" {
					line = `{"type":"assistant","uuid":"u1","message":{"usage":{"input_tokens":"unknown"}}}`
				}
				writeTranscript(t, transcript, []string{line})
				completeMeteredSource(t, root, meteredProvider, "original-worker", transcript)
			case "missing":
				if err := lifecycle.ObserveNativeCompletion(root, "cancelled-cost", action, meteredProvider, "original-worker"); err != nil {
					t.Fatal(err)
				}
			case protocol.CostSourcePending:
				observeMeteredStart(t, root, meteredProvider, "original-worker")
			}
			ref := protocol.CostSourceRef{RunID: "cancelled-cost", ActionID: action, Provider: meteredProvider, Correlation: "original-worker"}
			resolved := CostSourceBridge(root, "cancelled-cost")(ref)
			if scenario.source == protocol.CostSourceReady && (resolved.State != protocol.CostSourceReady || resolved.Usage != want) || scenario.source == protocol.CostSourcePending && resolved.State != protocol.CostSourcePending || (scenario.source == "missing" || scenario.source == "invalid-usage") && (resolved.State != protocol.CostSourceUnavailable || resolved.Reason == "") {
				t.Fatalf("managed original source: %+v", resolved)
			}
			for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
				event, err := protocol.NewLifecycleEventEvent(protocol.EventID("observed-"+edge), meteredProvider, "original-worker", edge)
				if err != nil {
					t.Fatal(err)
				}
				submit(event)
			}
			request, err := f.PrepareControl(ControlInput{Request: scenario.control})
			if err != nil {
				t.Fatal(err)
			}
			submit(request.Event)
			choice := "abort"
			if scenario.control == protocol.ControlReset {
				choice = "proceed"
			}
			candidate, err := f.PrepareControl(ControlInput{Ask: askPosition(t, run, scenario.control), Choice: choice, Reason: "User cancels unfinished work"})
			if err != nil {
				t.Fatal(err)
			}
			originalCandidate, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			_, run, err = f.SubmitControl(candidate)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			state := snapshot.State
			if len(state.PendingActions) != 0 || len(state.PendingHostActions) != 0 {
				t.Fatal("stopped cancellation remained blocked on cost evidence")
			}
			if scenario.source == protocol.CostSourcePending {
				if state.Cost != nil {
					t.Fatal("pending native source was converted to measured or unavailable cost")
				}
			} else {
				if state.Cost == nil || len(state.Cost.Dispatches) != 1 {
					t.Fatalf("cancelled original dispatch has no cost entry: %+v", state.Cost)
				}
				entry := state.Cost.Dispatches[action]
				if scenario.source == protocol.CostSourceReady {
					if entry.Source != cost.SourceTranscript || entry.InputCacheMissTokens != want.InputCacheMissTokens || entry.InputCacheHitTokens != want.InputCacheHitTokens || entry.OutputTokens != want.OutputTokens || entry.TotalInputTokens != 6 || state.Cost.TotalInputTokens != 6 || state.Cost.OutputTokens != 2 {
						t.Fatalf("cancelled original usage: %+v", state.Cost)
					}
				} else if entry.Source != cost.SourceUnavailable || entry.Reason != resolved.Reason || entry.TotalInputTokens != 0 || entry.OutputTokens != 0 {
					t.Fatalf("unavailable native source lost its qualification: %+v", entry)
				}
			}
			before := state.Cost
			var replayCandidate ControlCandidate
			if err := json.Unmarshal(originalCandidate, &replayCandidate); err != nil {
				t.Fatal(err)
			}
			if _, _, err := f.SubmitControl(replayCandidate); err != nil {
				t.Fatal(err)
			}
			after, err := f.engine.Load()
			if err != nil || !reflect.DeepEqual(before, after.State.Cost) {
				t.Fatal("repeated control changed original frozen usage", err)
			}
			if scenario.control == protocol.ControlAbort {
				if _, err := f.Drive(); err != nil {
					t.Fatal(err)
				}
				summary, err := f.readSummary()
				if err != nil || !reflect.DeepEqual(before, summary.Cost) {
					t.Fatal("terminal publication lost pre-cleanup original cost", err)
				}
			}
		})
	}
}
