package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/host"
)

const ownerAdditionalUsage = `{"timestamp":"after-start","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":60,"cached_input_tokens":5,"output_tokens":4}}}}
`

func TestOwnerHookInstallationBindsProviderAndReplacesOldHook(t *testing.T) {
	for _, provider := range []string{host.Claude, host.Codex} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			target := installTarget{host: provider, targetPath: filepath.Join(root, "skill"), launcherPath: filepath.Join(root, nativeBinaryName()), hookConfig: filepath.Join(root, "hooks.json")}
			descriptor, _ := host.Lookup(provider)
			old := nestedHookEntry(descriptor.Hook.GateMatcher, nativeInstallCommand(target.launcherPath, "hook", "decide"), descriptor.Hook.GateTimeout)
			if err := writeHookConfig(target.hookConfig, map[string]any{"hooks": map[string]any{"PreToolUse": []any{old}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := configureInstallHook(target); err != nil {
				t.Fatal(err)
			}
			config, err := readHookConfig(target.hookConfig)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(hookObject(config)["PreToolUse"])
			if strings.Count(string(data), "hook decide") != 1 || !strings.Contains(string(data), "--provider "+provider) {
				t.Fatalf("owner provider hook missing or duplicated: %s", data)
			}
			if changed, err := configureInstallHook(target); err != nil || changed {
				t.Fatalf("reinstall changed the same owner hook: %v, %v", changed, err)
			}
		})
	}
}

func startMeteredOwner(t *testing.T, root, pkg, id, provider, transcript, route string) RunState {
	t.Helper()
	payload := fmt.Sprintf(`{"tool_name":"Bash","tool_input":{"command":%q},"transcript_path":%q,"session_id":"owner-session"}`, "formal-gates workflow start --root "+fmt.Sprintf("%q", root)+" --split no", transcript)
	if _, err := Hook([]byte(payload), provider); err != nil {
		t.Fatal(err)
	}
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", Split: "no", Route: route})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func appendOwnerUsage(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(ownerAdditionalUsage); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func requireOwnerUnavailable(t *testing.T, projection *cost.RunCost) {
	t.Helper()
	if projection == nil || projection.Owner == nil || *projection.Owner != (cost.OwnerCost{Source: cost.SourceUnavailable}) {
		t.Fatalf("want explicit unavailable owner with zero numbers, got %+v", projection)
	}
}

func TestOwnerStartAbortPersistsBaselineAndCombinesDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	transcript := writeCostFixture(t)
	state := startMeteredOwner(t, root, pkg, "owner-abort", "codex", transcript, "")
	persisted, err := LoadRunState(root, state.RunID)
	if err != nil || persisted.OwnerTranscript != transcript || persisted.OwnerProvider != "codex" || persisted.OwnerCostBaseline == nil || persisted.OwnerCostBaseline.Usage.OutputTokens != 80 || persisted.OwnerCostCompleted {
		t.Fatalf("start did not persist baseline and source: %+v, %v", persisted.OwnerCostBaseline, err)
	}
	state = confirmAndRoute(t, root, pkg, state, "custom", []string{"quality"})
	dispatchID := prepareClaimedAction(t, root, pkg, state, "start-readiness", "owner-cost-dispatch")
	stubCodexTranscript(t, writeCostFixture(t))
	state, err = RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	dispatches, _ := json.Marshal(state.Cost.Dispatches)
	appendOwnerUsage(t, transcript)
	summary, err := Abort(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	owner := cost.OwnerCost{Source: cost.SourceTranscript, TotalInputTokens: 60, InputCacheHitTokens: 5, InputCacheMissTokens: 55, OutputTokens: 4}
	if summary.Cost.Owner == nil || *summary.Cost.Owner != owner || summary.Cost.TotalInputTokens != 310 || summary.Cost.OutputTokens != 84 {
		t.Fatalf("wrong owner/dispatch total: %+v", summary.Cost)
	}
	after, _ := json.Marshal(summary.Cost.Dispatches)
	if string(after) != string(dispatches) {
		t.Fatal("owner changed dispatch details")
	}
	if !reflect.DeepEqual(summary.Gates, state.Gates) || !reflect.DeepEqual(summary.QA, qaOverallResult(state)) {
		t.Fatal("owner changed gate or QA results")
	}
	retained, err := LoadRunSummary(root, state.RunID)
	if err != nil || !reflect.DeepEqual(retained.Cost, summary.Cost) {
		t.Fatalf("summary lost cost: %+v, %v", retained.Cost, err)
	}
}

func TestOwnerUnavailableNormalSourceFailuresAndOldState(t *testing.T) {
	for _, name := range []string{"missing provider", "unsupported provider", "missing transcript", "unparseable baseline", "missing final source", "truncated", "rewritten", "partial end", "old state"} {
		t.Run(name, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			transcript := writeCostFixture(t)
			provider := "codex"
			switch name {
			case "missing provider":
				provider = ""
			case "unsupported provider":
				provider = "cursor"
			case "missing transcript":
				transcript = ""
			case "unparseable baseline":
				writeTestFile(t, transcript, "{}\n")
			}
			var state RunState
			if name == "old state" {
				state = mustStart(t, root, pkg, "owner-unavailable")
				data, err := os.ReadFile(RunStatePath(root, state.RunID))
				if err != nil || strings.Contains(string(data), "ownerCost") {
					t.Fatalf("old-shape state unexpectedly has owner metadata: %v", err)
				}
				if _, err := LoadRunState(root, state.RunID); err != nil {
					t.Fatalf("old state failed integrity/schema checks: %v", err)
				}
			} else {
				state = startMeteredOwner(t, root, pkg, "owner-unavailable", provider, transcript, "")
			}
			switch name {
			case "missing final source":
				if err := os.Remove(transcript); err != nil {
					t.Fatal(err)
				}
			case "truncated":
				writeTestFile(t, transcript, "{}\n")
			case "rewritten":
				writeTestFile(t, transcript, strings.ReplaceAll(costCodexFixture, "250", "350")+ownerAdditionalUsage)
			case "partial end":
				writeTestFile(t, transcript, costCodexFixture+`{"type":`)
			}
			summary, err := Abort(root, state.RunID)
			if err != nil {
				t.Fatal(err)
			}
			requireOwnerUnavailable(t, summary.Cost)
			if summary.Cost.TotalInputTokens != 0 || summary.Cost.OutputTokens != 0 {
				t.Fatal("unavailable owner inflated totals")
			}
		})
	}
}

func TestOwnerOverlapSurvivesEitherCompletionOrder(t *testing.T) {
	for _, first := range []string{"owner-a", "owner-b"} {
		t.Run(first, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			transcript := writeCostFixture(t)
			startMeteredOwner(t, root, pkg, "owner-a", "codex", transcript, "")
			startMeteredOwner(t, root, pkg, "owner-b", "codex", transcript, "")
			appendOwnerUsage(t, transcript)
			second := "owner-a"
			if first == second {
				second = "owner-b"
			}
			for _, id := range []string{first, second} {
				state, err := LoadRunState(root, id)
				if err != nil || !state.OwnerCostOverlapped {
					t.Fatalf("lost overlap marker for %s: %v", id, err)
				}
				summary, err := Abort(root, id)
				if err != nil {
					t.Fatal(err)
				}
				requireOwnerUnavailable(t, summary.Cost)
			}
			// A later, non-overlapping run gets its own fresh baseline.
			state := startMeteredOwner(t, root, pkg, "owner-c", "codex", transcript, "")
			summary, err := Abort(root, state.RunID)
			if err != nil || summary.Cost.Owner.Source != cost.SourceTranscript || summary.Cost.TotalInputTokens != 0 {
				t.Fatalf("sequential owner was treated as overlapping: %+v, %v", summary.Cost, err)
			}
		})
	}
}

func TestOwnerSeparateTranscriptsDoNotOverlap(t *testing.T) {
	root, pkg := workflowFixture(t)
	for _, id := range []string{"separate-a", "separate-b"} {
		transcript := writeCostFixture(t)
		startMeteredOwner(t, root, pkg, id, "codex", transcript, "")
		appendOwnerUsage(t, transcript)
	}
	for _, id := range []string{"separate-a", "separate-b"} {
		summary, err := Abort(root, id)
		if err != nil || summary.Cost.Owner.Source != cost.SourceTranscript || summary.Cost.TotalInputTokens != 60 {
			t.Fatalf("independent source was not metered: %+v, %v", summary.Cost, err)
		}
	}
}

func TestOwnerTerminalRetryKeepsCompletedInterval(t *testing.T) {
	for _, terminal := range []string{"SEALED", "ABORTED"} {
		t.Run(terminal, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			transcript := writeCostFixture(t)
			state := startMeteredOwner(t, root, pkg, "owner-terminal", "codex", transcript, "lightweight")
			if _, err := PrepareAction(root, pkg, state.RunID, "requirements-clarification", "", false, ""); err != nil {
				t.Fatal(err)
			}
			state, _ = LoadRunState(root, state.RunID)
			var err error
			state, err = RecordAction(root, pkg, state.RunID, "requirements-clarification", openDispatchID(state, "action", "requirements-clarification"), "PASS", "", nil, false, "")
			if err != nil {
				t.Fatal(err)
			}
			state, err = UpdateRequirement(root, pkg, state.RunID, "", true, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			finish := func() (RunSummary, error) {
				if terminal == "SEALED" {
					return Seal(root, pkg, state.RunID, nil, false, "")
				}
				return Abort(root, state.RunID)
			}
			// Hold up summary creation after the durable terminal checkpoint,
			// then resume through the ordinary public terminal operation.
			if err := os.MkdirAll(RunSummaryPath(root, state.RunID), 0o700); err != nil {
				t.Fatal(err)
			}
			appendOwnerUsage(t, transcript)
			if _, err := finish(); err == nil {
				t.Fatal("expected summary path conflict")
			}
			checkpoint, err := LoadRunState(root, state.RunID)
			if err != nil || checkpoint.Status != terminal || !checkpoint.OwnerCostCompleted || checkpoint.Cost.Owner.TotalInputTokens != 60 {
				t.Fatalf("terminal checkpoint lost owner interval: %+v, %v", checkpoint.Cost, err)
			}
			if !reflect.DeepEqual(checkpoint.Actions, state.Actions) || !reflect.DeepEqual(checkpoint.QAExecutionByMode, state.QAExecutionByMode) || !reflect.DeepEqual(checkpoint.Gates, state.Gates) {
				t.Fatal("owner metering changed final workflow results")
			}
			if err := os.Remove(RunSummaryPath(root, state.RunID)); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, transcript, costCodexFixture+ownerAdditionalUsage+strings.ReplaceAll(ownerAdditionalUsage, "after-start", "after-terminal"))
			summary, err := finish()
			if err != nil || !reflect.DeepEqual(checkpoint.Cost, summary.Cost) {
				t.Fatalf("terminal retry re-metered owner: %+v, %v", summary.Cost, err)
			}
			before, _ := os.ReadFile(RunSummaryPath(root, state.RunID))
			if _, err := finish(); err == nil {
				t.Fatal("cleaned run should retain existing already-terminated response")
			}
			after, _ := os.ReadFile(RunSummaryPath(root, state.RunID))
			if string(before) != string(after) {
				t.Fatal("duplicate terminal call rewrote retained cost")
			}
			if _, err := os.Stat(filepath.Join(RunDir(root, state.RunID), "state.json")); !os.IsNotExist(err) {
				t.Fatal("terminal cleanup did not finish")
			}
		})
	}
}
