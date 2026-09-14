package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/runtime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

func TestRuntimeContextKeepsOriginalEnvironmentAndRejectsMissingExecutable(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	t.Setenv("CODEX_HOME", first)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binding, err := captureRuntimeContext("codex", settings.Values{Native: map[string]string{"codexExecutable": executable}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", second)
	cmd := exec.Command(executable, "-test.run=^TestRuntimeContextEnvironmentChild$")
	if err = runtimeCommandContext(cmd, binding); err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(cmd.Env, "FORMAL_GATES_CONTEXT_CHILD=1")
	out, err := cmd.Output()
	if err != nil || string(out) != first {
		t.Fatalf("context=%q err=%v", out, err)
	}
	sessions := filepath.Join(first, "sessions", "2026", "09", "10")
	if err = os.MkdirAll(sessions, 0700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(sessions, "rollout-native-id.jsonl")
	if err = os.WriteFile(want, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := findBoundTranscript("native-id", binding); err != nil || got != want {
		t.Fatalf("lookup=%s %v", got, err)
	}
	binding.Executable = filepath.Join(t.TempDir(), "missing")
	if err = runtimeCommandContext(exec.Command(executable), binding); err == nil {
		t.Fatal("missing original executable accepted")
	}
}

func TestRecoveryJSONRoundTripPreservesNativeDigest(t *testing.T) {
	receipt := protocol.DeveloperRecovery{NativeResponse: json.RawMessage(`{"result":{"text":"a && b < c > d"}}`)}
	path := filepath.Join(t.TempDir(), "receipt.json")
	if err := writeJSONAtomic(path, receipt); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var got protocol.DeveloperRecovery
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if gitprep.Digest(got) != gitprep.Digest(receipt) {
		t.Fatal("native receipt digest changed during persistence")
	}
}

func TestClosedToolConfigDisablesEveryResolvedServerAndPlugin(t *testing.T) {
	config, err := runtimeToolConfig("codex", json.RawMessage(`{"config":{"features":{"multi_agent":true,"multi_agent_v2":true,"enable_mcp_apps":true},"mcp_servers":{"ordinary":{"command":"x"},"name.with.dots":{"url":"x"}},"plugins":{"plugin@market":{"enabled":true}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if config["agents"].(map[string]any)["max_depth"] != 0 {
		t.Fatal("native delegation remained enabled")
	}
	for category, names := range map[string][]string{"mcp_servers": {"ordinary", "name.with.dots"}, "plugins": {"plugin@market"}} {
		entries := config[category].(map[string]any)
		for _, name := range names {
			if entries[name].(map[string]any)["enabled"] != false {
				t.Fatalf("tool still allowed: %s", name)
			}
		}
	}
	if _, err = runtimeToolConfig("codex", json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing effective configuration accepted")
	}
}

func TestProductPromptIncludesIndependentObligationChecks(t *testing.T) {
	instructions, err := loadActionInstructions("../../..")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := reviewPrompt(Run{Regular: &protocol.RegularState{ActionInstructions: instructions, Requirement: protocol.RequirementDocument{Text: "mandatory body"}}}, protocol.ExecutionClaim{Task: runtime.TaskKey{Node: "review", Step: "product-review"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"outside REQ sections", "same REQ", "minor formatting", "mandatory body"} {
		if !strings.Contains(prompt, text) {
			t.Fatal(text)
		}
	}
}

func TestReadOnlyReviewPromptDescribesActualToolBoundary(t *testing.T) {
	instructions, err := loadActionInstructions("../../..")
	if err != nil {
		t.Fatal(err)
	}
	run := Run{Regular: &protocol.RegularState{ActionInstructions: instructions,
		Requirement: protocol.RequirementDocument{Text: "complete mandatory input"}}}
	for _, task := range []runtime.TaskKey{{Node: "entry", Step: "product-review"}, {Node: "entry", Step: "start-readiness"}} {
		step := string(task.Step)
		if nativeRoleAccess(step) != nativeReadOnly {
			t.Fatal("fixture must correspond to a text-only native role", step)
		}
		prompt, err := reviewPrompt(run, protocol.ExecutionClaim{Task: task})
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"普通读取、搜索能力，禁止写入", "不要用普通文本模拟工具调用", "按当前 schema 返回直接 JSON，不加围栏", "complete mandatory input"} {
			if !strings.Contains(prompt, text) {
				t.Fatalf("%s did not receive its bound text-only instructions: %s", step, text)
			}
		}
	}
}

func facadeCoverage(t *testing.T, f *Facade, action string) {
	t.Helper()
	q := operationRequest(t, f, action, "whitebox.coverage", "coverage-before-tests")
	q.CaseID = ""
	q.Prompt = `{"coverageJudgment":"independent output behavior","behaviorOracles":[{"behavior":"hello command","oracle":"stdout hello, empty stderr, exit 0"}]}`
	if _, err := f.ExecuteTaskOperation(q, false); err != nil {
		t.Fatal(err)
	}
}

func TestCollectRetainsSeparateStderr(t *testing.T) {
	f := &Facade{stateDir: t.TempDir()}
	target := protocol.TaskOperation{ID: "target", Request: protocol.TaskOperationRequest{Kind: "qa.target", CaseID: "C"}, Receipt: &protocol.TaskOperationReceipt{ExitCode: 2, Workspace: "actual workspace"}}
	job := f.taskOperationDir(target.ID)
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"stdout.log": "", "stderr.log": "usage\n"} {
		if err := os.WriteFile(filepath.Join(job, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := Run{Executions: map[string]protocol.RegularExecution{"A": {Operations: map[string]protocol.TaskOperation{target.ID: target}}}}
	op := protocol.TaskOperation{ID: "collect", Request: protocol.TaskOperationRequest{Kind: "qa.collect", CaseID: "C", Binding: protocol.ExecutionClaim{ActionID: "A"}}}
	got := f.performTaskOperation(run, op, t.TempDir())
	if got.Status != "COMPLETED" || got.Output != "" || got.Stderr != "usage\n" || got.ExitCode != 2 {
		t.Fatalf("collection=%+v", got)
	}
}

func TestAbortAfterPrecheckFailureCompletesCleanup(t *testing.T) {
	f := newRegularFacade(t)
	// Start another run with an actually malformed requirement; no projection can
	// be produced, but the same public abort request must still finish terminal work.
	bad := filepath.Join(f.root, "bad.md")
	if err := os.WriteFile(bad, []byte("not a requirement"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := gitprep.Git(f.root, "add", "bad.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitprep.Git(f.root, "-c", "core.hooksPath=/dev/null", "commit", "-qm", "malformed intake fixture"); err != nil {
		t.Fatal(err)
	}
	packageRoot, _ := filepath.Abs("../../..")
	broken, _, err := Start(StartOptions{Root: f.root, PackageRoot: packageRoot, Request: StartRequest{RunID: "bad-intake", RequirementPath: "bad.md"}, Admission: &Admission{PackageDigest: "sha256:test", InstalledTargetIdentity: "test-target"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = broken.Drive(); err == nil {
		t.Fatal("malformed precheck passed")
	}
	facadeChoice(t, broken, protocol.RegularRequest{Action: "abort", Reason: "cancel malformed intake"})
	run, err := broken.Drive()
	if err != nil || run.Status != "COMPLETE" {
		t.Fatalf("abort=%+v %v", run, err)
	}
	if _, err = os.Stat(filepath.Join(broken.stateDir, "terminal-summary.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(broken.stateDir, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("active state survived terminal cleanup: %v", err)
	}
}

func TestInvalidRequirementEncodingReportsOriginalBytes(t *testing.T) {
	f := newRegularFacade(t)
	bad := append([]byte("## 需求点\n"), 0xff)
	if err := os.WriteFile(filepath.Join(f.root, "bad.md"), bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := gitprep.Git(f.root, "add", "bad.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitprep.Git(f.root, "-c", "core.hooksPath=/dev/null", "commit", "-qm", "encoding fixture"); err != nil {
		t.Fatal(err)
	}
	packageRoot, _ := filepath.Abs("../../..")
	broken, _, err := Start(StartOptions{Root: f.root, PackageRoot: packageRoot, Request: StartRequest{RunID: "bad-encoding", RequirementPath: "bad.md"}, Admission: &Admission{PackageDigest: "sha256:test", InstalledTargetIdentity: "test-target"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = broken.Drive(); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("encoding diagnostic: %v", err)
	}
	snap, err := broken.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.Regular.Requirement.Confirmed || snap.State.Regular.Requirement.Projection != nil {
		t.Fatal("invalid encoding advanced intake")
	}
}

// Host installation and native execution have different published capability
// sets. An unsupported native host must never fall through to the Codex adapter.
func TestNativeRuntimeBoundaryForEveryInstalledHost(t *testing.T) {
	for _, provider := range host.InstallableIDs() {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			adapter, err := runtimeAdapter(provider)
			if provider == host.WorkBuddy || provider == host.ZCode || provider == host.DeepSeek {
				if err != nil || adapter == nil {
					t.Fatal("native runtime missing", err)
				}
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				script := filepath.Join(root, "zcode.cjs")
				if err := os.WriteFile(script, []byte("// capture only; never executed"), 0600); err != nil {
					t.Fatal(err)
				}
				binding, err := captureRuntimeContext(provider, settings.Values{Native: map[string]string{"workbuddyExecutable": executable, "zcodeExecutable": executable, "zcodeScript": script, "dshExecutable": executable}}, filepath.Join(root, ".gates", "engine", "run"), root)
				if err != nil || binding.Provider != provider || binding.Executable != executable || binding.Environment[nativeSourceRoot] == "" || binding.Environment["FORMAL_GATES_ENGINE_ROOT"] != root {
					t.Fatalf("binding=%+v err=%v", binding, err)
				}
				if _, ok := binding.Environment["CODEX_HOME"]; ok {
					t.Fatal("native host inherited Codex context")
				}
				return
			}
			if provider == host.Codex {
				if err != nil || adapter == nil {
					t.Fatal("supported native runtime missing", err)
				}
				cfg, err := runtimeToolConfig(provider, json.RawMessage(`{"config":{}}`))
				if err != nil || cfg["agents"].(map[string]any)["max_depth"] != 0 {
					t.Fatal("native descendant boundary differs", cfg, err)
				}
				return
			}
			if err == nil || adapter != nil || !strings.Contains(err.Error(), "UNSUPPORTED_ENGINE_ENTRY") {
				t.Fatal("installation support silently became native support", err)
			}
			if _, err = prepareRegular(root, StartRequest{Provider: provider}); err == nil || !strings.Contains(err.Error(), "UNSUPPORTED_ENGINE_ENTRY") {
				t.Fatal("unsupported native intake accepted", err)
			}
			if _, err = captureRuntimeContext(provider, settings.Values{Native: map[string]string{"codexExecutable": "must-not-execute"}}); err == nil || !strings.Contains(err.Error(), "UNSUPPORTED_ENGINE_ENTRY") {
				t.Fatal("unsupported creation captured Codex environment", err)
			}
			if _, err = runtimeToolConfig(provider, json.RawMessage(`{"config":{}}`)); err == nil {
				t.Fatal("unsupported recovery received Codex tool configuration")
			}
			cmd := &exec.Cmd{Path: "untouched", Args: []string{"untouched"}}
			if err = runtimeCommandContext(cmd, &protocol.HostRuntimeContext{Provider: provider}); err == nil || cmd.Path != "untouched" || cmd.Args[0] != "untouched" {
				t.Fatal("unsupported model operation mutated native invocation", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected platform created runtime resources", err)
			}
		})
	}
}

func TestRuntimeContextEnvironmentChild(t *testing.T) {
	if os.Getenv("FORMAL_GATES_CONTEXT_CHILD") != "1" {
		return
	}
	_, _ = os.Stdout.WriteString(os.Getenv("CODEX_HOME"))
	os.Exit(0)
}

func TestTechnicalReviewReceivesConfirmedPlanChange(t *testing.T) {
	instructions, err := loadActionInstructions("../../..")
	if err != nil {
		t.Fatal(err)
	}
	run := Run{
		reviewDecisions: map[string]protocol.RecordedDecision{
			"approved": {Choice: "confirm"}, "cancelled": {Choice: "cancel"},
		},
		Regular: &protocol.RegularState{
			ActionInstructions: instructions,
			Requirement:        protocol.RequirementDocument{Text: "unchanged requirement", Revision: "current"},
			Requests: []protocol.RegularAsk{
				{ID: "approved", Request: protocol.RegularRequest{Action: "plan-change", Reason: "Remove outer Seal from development completion"}},
				{ID: "cancelled", Request: protocol.RegularRequest{Action: "plan-change", Reason: "cancelled instruction"}},
				{ID: "pending", Request: protocol.RegularRequest{Action: "plan-change", Reason: "unconfirmed instruction"}},
			},
			Reviews: map[string]*protocol.PreDevelopmentReviewSeries{"start-readiness": {
				RawCandidates: []protocol.RawReviewCandidate{
					{RequirementRevision: "current", GranularityReview: &protocol.DevelopmentPlan{Rationale: "original six batches"}},
					{RequirementRevision: "old", GranularityReview: &protocol.DevelopmentPlan{Rationale: "stale plan"}},
					{RequirementRevision: "current", Quarantined: true, GranularityReview: &protocol.DevelopmentPlan{Rationale: "quarantined plan"}},
				},
			}},
		},
	}
	hasInput := func(prompt, key string) bool {
		t.Helper()
		start := strings.LastIndex(prompt, "\n\n{")
		if start < 0 {
			t.Fatal("missing structured review input")
		}
		var input map[string]json.RawMessage
		if err := json.Unmarshal([]byte(prompt[start+2:]), &input); err != nil {
			t.Fatal(err)
		}
		_, ok := input[key]
		return ok
	}
	claim := protocol.ExecutionClaim{RequirementRevision: "current", Task: runtime.TaskKey{Node: "review", Step: "start-readiness"}}
	prompt, err := reviewPrompt(run, claim)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"unchanged requirement", "Remove outer Seal from development completion", "original six batches", "Return the complete revised plan"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing revision input %q", want)
		}
	}
	for _, forbidden := range []string{"cancelled instruction", "unconfirmed instruction", "stale plan", "quarantined plan"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("unapproved or stale input %q", forbidden)
		}
	}
	claim.Task.Step = "product-review"
	prompt, err = reviewPrompt(run, claim)
	if err != nil {
		t.Fatal(err)
	}
	if hasInput(prompt, "confirmedPlanChange") {
		t.Fatal("technical plan change leaked into product review")
	}
	claim.Task.Step = "start-readiness"
	run.Regular.Requests = append(run.Regular.Requests, protocol.RegularAsk{ID: "new-requirement", Request: protocol.RegularRequest{Action: "apply-requirement"}})
	run.reviewDecisions["new-requirement"] = protocol.RecordedDecision{Choice: "confirm"}
	prompt, err = reviewPrompt(run, claim)
	if err != nil {
		t.Fatal(err)
	}
	if hasInput(prompt, "confirmedPlanChange") {
		t.Fatal("old plan change leaked across requirement revision")
	}
	run.reviewDecisions = nil
	prompt, err = reviewPrompt(run, claim)
	if err != nil {
		t.Fatal(err)
	}
	if hasInput(prompt, "confirmedPlanChange") || hasInput(prompt, "previousPlans") {
		t.Fatal("unconfirmed request treated as authorization")
	}
}
