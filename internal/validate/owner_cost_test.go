package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
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
	state, err = RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "PASS", "", nil, false, "", fixtureOperatorVerification())
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

// 跨代区间协调（迁移期 legacy validate 与 engine 并存）：共享 owner transcript
// 的两代 run 区间相交时双方 UNAVAILABLE，消失交错经留存摘要/终态重扫闭合，
// 各自的工件只由各自合法 writer 落盘。

func startEngineOwnerRun(t *testing.T, root, id, transcript string) *facade.Facade {
	t.Helper()
	f, _, err := facade.Start(facade.StartOptions{
		Root:  root,
		Owner: facade.OwnerBinding{Transcript: transcript, Provider: "codex"},
		Request: facade.StartRequest{
			RunID: id, Route: "lightweight",
			DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest,
			IntakeConfirmationReceipt: facade.IntakeConfirmationReceipt{
				Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport,
				RequirementSource: "requirements.md", RequirementRevision: "req",
				Artifacts:        []facade.IntakeArtifact{{Path: "requirements.md", Revision: "req"}},
				SolutionRevision: "sol", SolutionDigest: "sha256:sol",
			},
		},
		Admission: &facade.Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "candidate"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// snapshotIntervalTree 固定目录内全部文件的字节（相对路径→内容），用于断言
// 跨代操作不改写对方工件。
func snapshotIntervalTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return files
}

func requireEngineOwnerUnavailable(t *testing.T, owner *cost.OwnerCost) {
	t.Helper()
	if owner == nil || owner.Source != cost.SourceUnavailable || owner.TotalInputTokens != 0 || owner.OutputTokens != 0 {
		t.Fatalf("want unavailable engine owner with zero numbers, got %+v", owner)
	}
}

// TestCrossGenerationOwnerOverlapBothOrdersReportUnavailable：两代两顺序——共享
// transcript 的 engine 与 legacy run 区间相交，双方终态都报 UNAVAILABLE；
// engine 终态不改写 legacy ACTIVE state 字节，legacy 终态不改写 engine 文件集。
func TestCrossGenerationOwnerOverlapBothOrdersReportUnavailable(t *testing.T) {
	for _, engineFirst := range []bool{true, false} {
		name := "legacy-first"
		if engineFirst {
			name = "engine-first"
		}
		t.Run(name, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			transcript := writeCostFixture(t)
			var engine *facade.Facade
			var legacy RunState
			if engineFirst {
				engine = startEngineOwnerRun(t, root, "engine-owner", transcript)
				legacy = startMeteredOwner(t, root, pkg, "legacy-owner", "codex", transcript, "")
			} else {
				legacy = startMeteredOwner(t, root, pkg, "legacy-owner", "codex", transcript, "")
				engine = startEngineOwnerRun(t, root, "engine-owner", transcript)
			}
			appendOwnerUsage(t, transcript)
			legacyStateBefore, err := os.ReadFile(RunStatePath(root, legacy.RunID))
			if err != nil {
				t.Fatal(err)
			}
			engineRun, err := engine.Drive()
			if err != nil {
				t.Fatal(err)
			}
			requireEngineOwnerUnavailable(t, engineRun.Cost.Owner)
			legacyStateAfter, err := os.ReadFile(RunStatePath(root, legacy.RunID))
			if err != nil {
				t.Fatal(err)
			}
			if string(legacyStateBefore) != string(legacyStateAfter) {
				t.Fatal("engine terminal rewrote legacy state bytes")
			}
			engineDir := filepath.Join(root, ".gates", "engine")
			engineTreeBefore := snapshotIntervalTree(t, engineDir)
			legacySummary, err := Abort(root, legacy.RunID)
			if err != nil {
				t.Fatal(err)
			}
			requireOwnerUnavailable(t, legacySummary.Cost)
			if engineTree := snapshotIntervalTree(t, engineDir); !reflect.DeepEqual(engineTreeBefore, engineTree) {
				t.Fatal("legacy terminal rewrote engine files")
			}
		})
	}
}

// TestVanishingLegacyOwnerIntervalMarksEngineUnavailable：legacy 在 engine 启动
// 后启动、先于 engine 终态消失（run 目录随完成清理，留存摘要是唯一区间事实）—
// —engine 终态重扫经摘要 StartedAt 检出并报 UNAVAILABLE，且不改写摘要字节。
func TestVanishingLegacyOwnerIntervalMarksEngineUnavailable(t *testing.T) {
	root, pkg := workflowFixture(t)
	transcript := writeCostFixture(t)
	engine := startEngineOwnerRun(t, root, "engine-owner", transcript)
	startMeteredOwner(t, root, pkg, "legacy-owner", "codex", transcript, "")
	appendOwnerUsage(t, transcript)
	legacySummary, err := Abort(root, "legacy-owner")
	if err != nil {
		t.Fatal(err)
	}
	requireOwnerUnavailable(t, legacySummary.Cost)
	summaryBefore, err := os.ReadFile(RunSummaryPath(root, "legacy-owner"))
	if err != nil {
		t.Fatal(err)
	}
	engineRun, err := engine.Drive()
	if err != nil {
		t.Fatal(err)
	}
	requireEngineOwnerUnavailable(t, engineRun.Cost.Owner)
	summaryAfter, err := os.ReadFile(RunSummaryPath(root, "legacy-owner"))
	if err != nil {
		t.Fatal(err)
	}
	if string(summaryBefore) != string(summaryAfter) {
		t.Fatal("engine terminal rewrote the retained legacy summary bytes")
	}
}

// TestVanishingEngineOwnerIntervalMarksLegacyUnavailable：engine 在 legacy 启动
// 后启动、先于 legacy 终态消失——engine 终态摘要携带区间端点，legacy 终态重扫
// 判定区间相交报 UNAVAILABLE，且不改写 engine 文件集。
func TestVanishingEngineOwnerIntervalMarksLegacyUnavailable(t *testing.T) {
	root, pkg := workflowFixture(t)
	transcript := writeCostFixture(t)
	startMeteredOwner(t, root, pkg, "legacy-owner", "codex", transcript, "")
	engine := startEngineOwnerRun(t, root, "engine-owner", transcript)
	appendOwnerUsage(t, transcript)
	engineRun, err := engine.Drive()
	if err != nil {
		t.Fatal(err)
	}
	requireEngineOwnerUnavailable(t, engineRun.Cost.Owner)
	engineDir := filepath.Join(root, ".gates", "engine")
	engineTreeBefore := snapshotIntervalTree(t, engineDir)
	legacySummary, err := Abort(root, "legacy-owner")
	if err != nil {
		t.Fatal(err)
	}
	requireOwnerUnavailable(t, legacySummary.Cost)
	if engineTree := snapshotIntervalTree(t, engineDir); !reflect.DeepEqual(engineTreeBefore, engineTree) {
		t.Fatal("legacy terminal rewrote engine files")
	}
}

// TestSequentialCrossGenerationOwnersStayPrecise：精确性负控（两代）——对端先于
// 我启动且已终态（同 transcript），双方的 owner 都报精确差值（后者基线含前者
// 用量，差值为零也是精确）。
func TestSequentialCrossGenerationOwnersStayPrecise(t *testing.T) {
	during := cost.OwnerCost{Source: cost.SourceTranscript, TotalInputTokens: 60, InputCacheHitTokens: 5, InputCacheMissTokens: 55, OutputTokens: 4}
	for _, name := range []string{"legacy then engine", "engine then legacy"} {
		t.Run(name, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			transcript := writeCostFixture(t)
			if name == "legacy then engine" {
				startMeteredOwner(t, root, pkg, "legacy-owner", "codex", transcript, "")
				appendOwnerUsage(t, transcript)
				summary, err := Abort(root, "legacy-owner")
				if err != nil || summary.Cost.Owner == nil || *summary.Cost.Owner != during {
					t.Fatalf("earlier legacy owner = %+v, %v", summary.Cost, err)
				}
				engine := startEngineOwnerRun(t, root, "engine-owner", transcript)
				appendOwnerUsage(t, transcript)
				engineRun, err := engine.Drive()
				if err != nil || engineRun.Cost.Owner == nil || *engineRun.Cost.Owner != (cost.OwnerCost{Source: cost.SourceTranscript}) {
					t.Fatalf("later engine owner = %+v, %v", engineRun.Cost.Owner, err)
				}
				return
			}
			engine := startEngineOwnerRun(t, root, "engine-owner", transcript)
			appendOwnerUsage(t, transcript)
			engineRun, err := engine.Drive()
			if err != nil || engineRun.Cost.Owner == nil || *engineRun.Cost.Owner != during {
				t.Fatalf("earlier engine owner = %+v, %v", engineRun.Cost.Owner, err)
			}
			startMeteredOwner(t, root, pkg, "legacy-owner", "codex", transcript, "")
			appendOwnerUsage(t, transcript)
			summary, err := Abort(root, "legacy-owner")
			if err != nil || summary.Cost.Owner == nil || *summary.Cost.Owner != (cost.OwnerCost{Source: cost.SourceTranscript}) {
				t.Fatalf("later legacy owner = %+v, %v", summary.Cost.Owner, err)
			}
		})
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
			state, err = RecordAction(root, pkg, state.RunID, "requirements-clarification", openDispatchID(state, "action", "requirements-clarification"), "PASS", "", nil, false, "", fixtureOperatorVerification())
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

// TestEngineTerminalCheckpointKeepsLegacyIntervalUnavailable：legacy 启动扫描与
// engine 侧共用同一可排除条件——engine peer 的 state.json 已终态但终态摘要缺
// 失（或端点不可解析）＝区间仍开放，legacy run 不得据此声称精确拆分；摘要端
// 点可解析的终态 peer 区间已闭合，可排除。
func TestEngineTerminalCheckpointKeepsLegacyIntervalUnavailable(t *testing.T) {
	for _, name := range []string{"terminal without summary", "terminal with unparseable endpoints", "terminal with parseable endpoints"} {
		t.Run(name, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			transcript := filepath.Join(root, "shared-owner.jsonl")
			if err := os.WriteFile(transcript, []byte(costCodexFixture), 0o600); err != nil {
				t.Fatal(err)
			}
			engineDir := filepath.Join(root, ".gates", "engine", "engine-peer")
			if err := os.MkdirAll(engineDir, 0o700); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "terminal with unparseable endpoints":
				writeEnginePeerSummary(t, engineDir, transcript, "not-a-time", "also-not-a-time")
			case "terminal with parseable endpoints":
				writeEnginePeerSummary(t, engineDir, transcript, "2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z")
			}
			envelope := `{"content":{"phase":"TERMINAL","ownerTranscript":` + strconv.Quote(transcript) + `}}`
			if err := os.WriteFile(filepath.Join(engineDir, "state.json"), []byte(envelope), 0o600); err != nil {
				t.Fatal(err)
			}
			state := startMeteredOwner(t, root, pkg, "legacy-open-interval", "codex", transcript, "")
			wantOverlapped := name != "terminal with parseable endpoints"
			if state.OwnerCostOverlapped != wantOverlapped {
				t.Fatalf("overlap = %t, want %t for %s", state.OwnerCostOverlapped, wantOverlapped, name)
			}
		})
	}
}

// writeEnginePeerSummary 落一份 engine peer 的终态摘要工件（区间端点载荷）。
func writeEnginePeerSummary(t *testing.T, engineDir, transcript, startedAt, endedAt string) {
	t.Helper()
	summary := map[string]string{"ownerTranscript": transcript, "ownerStartedAt": startedAt, "ownerEndedAt": endedAt}
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(engineDir, "terminal-summary.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
