package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in live acceptance invokes the installed Codex executable through the same
// claim → native exec → thread observation → result → source path as the CLI.
// It is deliberately not an ordinary unit-test dependency or a fake provider.
func TestRegularCodexNative(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("set FORMAL_GATES_REAL_CODEX=1 for the bounded native review")
	}
	f := readyRegularFacade(t)
	run, _ := f.Show()
	action := ""
	for id := range run.Executions {
		action = id
	}
	result, err := f.ExecuteNative(action, false)
	if err != nil {
		t.Fatal(err)
	}
	execution := result.Executions[action]
	if execution.Identity == "" || !execution.Finished || execution.Source == nil || execution.Source.Status != "STOPPED" {
		t.Fatalf("native execution missing completion/source: %+v", execution)
	}
	if result.Regular.Reviews["product-review"].Completed != 1 || len(result.Cost.Dispatches) != 1 {
		t.Fatalf("native result not admitted: %+v", result)
	}
	if _, err = f.ExecuteNative(action, false); err == nil || !strings.Contains(err.Error(), "DUPLICATE_ACTION") {
		t.Fatalf("duplicate native call=%v", err)
	}
	if evidence := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); evidence != "" {
		data, _ := json.MarshalIndent(result, "", "  ")
		if err = os.MkdirAll(filepath.Dir(evidence), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(evidence, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("native action=%s identity=%s transcript=%s input=%d output=%d", action, execution.Identity, execution.Source.TranscriptPath, result.Cost.TotalInputTokens, result.Cost.OutputTokens)
}

func TestRegularCodexNativeChild(t *testing.T) {
	if os.Getenv("FORMAL_GATES_NATIVE_CHILD") != "1" {
		t.Skip("native subprocess helper")
	}
	f, err := Open(os.Getenv("FORMAL_GATES_NATIVE_ROOT"), "regular")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.ExecuteNative(os.Getenv("FORMAL_GATES_NATIVE_ACTION"), false)
	if err != nil {
		t.Fatal(err)
	}
}
func TestRegularCodexNativeInterruptedResume(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in real Codex interruption")
	}
	f := readyRegularFacade(t)
	run, _ := f.Show()
	id := ""
	for action := range run.Executions {
		id = action
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestRegularCodexNativeChild$", "-test.v")
	child.Env = append(os.Environ(), "FORMAL_GATES_NATIVE_CHILD=1", "FORMAL_GATES_NATIVE_ROOT="+f.root, "FORMAL_GATES_NATIVE_ACTION="+id)
	log, err := os.Create(filepath.Join(t.TempDir(), "child.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	child.Stdout = log
	child.Stderr = log
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	deadline := time.Now().Add(45 * time.Second)
	identity := ""
	var receipt codexProcess
	for time.Now().Before(deadline) {
		current, readErr := f.Show()
		if readErr == nil && current.Executions[id].Identity != "" {
			identity = current.Executions[id].Identity
			data, err := os.ReadFile(filepath.Join(f.codexJob(id), "process.json"))
			journal, _ := os.ReadFile(filepath.Join(f.codexJob(id), "stdout.jsonl"))
			// A created thread can precede its first durable provider turn. This
			// recovery case interrupts an actually started turn, while the separate
			// before-call case covers the unobserved/UNKNOWN interval.
			if err == nil && json.Unmarshal(data, &receipt) == nil && strings.Contains(string(journal), `"method":"turn/started"`) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if identity == "" || receipt.PID == 0 {
		t.Fatal("native startup did not expose bound identity and process receipt")
	}
	// End the orchestration process first: native completion cannot be turned into
	// a synthetic result by the original adapter after the injected interruption.
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	if err = stopCodexProcess(receipt.PID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for codexProcessAlive(receipt.PID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if codexProcessAlive(receipt.PID) {
		t.Fatal("native process group did not stop")
	}
	reopened, err := Open(f.root, "regular")
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := reopened.ExecuteNative(id, true)
	if err != nil {
		t.Fatal(err)
	}
	x := resumed.Executions[id]
	if x.Identity != identity || !x.ResumeClaimed || !x.Finished || x.Source == nil || x.Source.Status != "STOPPED" || resumed.Regular.Reviews["product-review"].Completed != 1 || len(resumed.Executions) != 1 {
		t.Fatalf("resume did not preserve one task/native identity: %+v", resumed)
	}
	if _, err = reopened.ExecuteNative(id, true); err == nil || !strings.Contains(err.Error(), "DUPLICATE_ACTION") {
		t.Fatalf("duplicate resume=%v", err)
	}
	if evidence := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); evidence != "" {
		data, _ := json.MarshalIndent(resumed, "", "  ")
		if err = os.WriteFile(strings.TrimSuffix(evidence, ".json")+"-resume.json", data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("real interrupted action=%s same-native-identity=%s input=%d output=%d", id, identity, resumed.Cost.TotalInputTokens, resumed.Cost.OutputTokens)
}

func TestRegularCodexNativeInterruptedBeforeCall(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in actual host process interruption")
	}
	f := readyRegularFacade(t)
	r, _ := f.Show()
	id := ""
	for action := range r.Executions {
		id = action
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestRegularCodexNativeChild$", "-test.v")
	child.Env = append(os.Environ(), "FORMAL_GATES_NATIVE_CHILD=1", "FORMAL_GATES_NATIVE_ROOT="+f.root, "FORMAL_GATES_NATIVE_ACTION="+id)
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	// Observe the real atomic state replacement directly, without waiting for the
	// writer lock. This catches the durable claim before the adapter gets its
	// response and creates the execution directory (which precedes exec.Start).
	seen := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
		var doc struct {
			Content struct {
				Executions map[string]protocol.RegularExecution `json:"regularExecutions"`
			} `json:"content"`
		}
		if json.Unmarshal(data, &doc) == nil && doc.Content.Executions[id].Claimed {
			seen = true
			_ = child.Process.Kill()
			break
		}
		time.Sleep(100 * time.Microsecond)
	}
	_ = child.Wait()
	if !seen {
		t.Fatal("did not observe durable claim")
	}
	if _, err = os.Stat(f.codexJob(id)); !os.IsNotExist(err) {
		// Keep the opt-in interruption bounded even if a slow observer missed this
		// narrow pre-call window; this is a failed experiment, never zero-call proof.
		data, _ := os.ReadFile(filepath.Join(f.codexJob(id), "process.json"))
		var p codexProcess
		if json.Unmarshal(data, &p) == nil && p.PID > 0 {
			_ = stopCodexProcess(p.PID)
		}
		t.Fatal("missed pre-call window: execution directory already exists")
	}
	f, err = Open(f.root, "regular")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.ExecuteNative(id, true); err == nil || !strings.Contains(err.Error(), "UNKNOWN") {
		t.Fatalf("unknown claim resumed: %v", err)
	}
	if _, err = os.Stat(f.codexJob(id)); !os.IsNotExist(err) {
		t.Fatal("UNKNOWN recovery made a native call")
	}
	// The independent observation above proves the missing receipt is a pre-call
	// interruption. Record that finite unavailable source after user abort, then
	// settle the original attempt and let ordinary terminal cleanup finish.
	facadeChoice(t, f, protocol.RegularRequest{Action: "abort", Reason: "End pre-call interruption validation"})
	r, _ = f.Show()
	x := r.Executions[id]
	src := protocol.ExecutionSource{ActionID: id, AttemptID: x.Claim.AttemptID, Provider: "codex", Status: "UNAVAILABLE", Reason: "Observer killed actual host after durable claim; transport directory never existed, so exec.Start was not reached"}
	if err = f.submitSource(src); err != nil {
		t.Fatal(err)
	}
	r, _ = f.Show()
	raw := protocol.RawReviewCandidate{ActionID: id, AttemptID: x.Claim.AttemptID, Provider: "codex", Action: "product-review", RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "RUNTIME_ERROR", Message: src.Reason}
	ev, _ := protocol.NewRegularEvent("pre-call-result", protocol.RegularEvent{Operation: "REVIEW_RESULT", ExpectedRevision: r.Revision, Review: &raw})
	if _, _, err = f.Submit(ev); err != nil {
		t.Fatal(err)
	}
	r, err = f.Drive()
	if err != nil || r.Status != "COMPLETE" {
		t.Fatalf("pre-call cleanup %s %v", r.Status, err)
	}
	t.Logf("actual host pid=%d killed after claim; native execution directory absent before and after UNKNOWN recovery; cleanup COMPLETE", child.Process.Pid)
}

func TestRegularCodexNativeAbortCollectsOnce(t *testing.T) {
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		t.Skip("opt-in native cancellation race")
	}
	f := readyRegularFacade(t)
	r, _ := f.Show()
	action := ""
	for id := range r.Executions {
		action = id
	}
	done := make(chan error, 1)
	go func() { _, err := f.ExecuteNative(action, false); done <- err }()
	deadline := time.Now().Add(45 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(f.codexJob(action), "stdout.jsonl"))
		if strings.Contains(string(data), `"method":"turn/started"`) {
			started = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !started {
		t.Fatal("native turn did not start")
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "abort", Reason: "Cancel this actual native canary turn and collect its observed failure once"})
	r, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if r.Regular.Terminal != "ABORTED" || r.Status != "COMPLETE" {
		t.Fatalf("abort did not settle: %+v", r.Next)
	}
	if len(r.Regular.RejectedResults) != 1 || !r.Executions[action].Finished {
		t.Fatal("cancellation did not retain exactly one failed native result")
	}
	if path := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); path != "" {
		b, _ := json.MarshalIndent(r, "", "  ")
		if err = os.WriteFile(strings.TrimSuffix(path, ".json")+"-abort.json", b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// The native contract must carry known bindings and the existing plan semantics;
// it must not ask a model to infer either from generic string fields.
func TestNativeReviewContractUsesClaimAndBatchIDs(t *testing.T) {
	f := readyRegularFacade(t)
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	var claim protocol.ExecutionClaim
	for _, x := range r.Executions {
		claim = x.Claim
	}
	for _, step := range []string{"product-review", "start-readiness"} {
		claim.Task.Step = authoring.StepID(step)
		schema := reviewSchema(claim)
		props := schema["properties"].(map[string]any)
		for field, want := range map[string]string{"actionId": claim.ActionID, "attemptId": claim.AttemptID, "provider": claim.Provider, "identity": "NATIVE_IDENTITY", "action": step, "requirementRevision": claim.RequirementRevision, "candidate": claim.Candidate} {
			spec := props[field].(map[string]any)
			enum, ok := spec["enum"].([]string)
			if !ok || len(enum) != 1 || enum[0] != want || spec["type"] != "string" {
				t.Fatalf("%s %s permits an invented binding: %+v", step, field, spec)
			}
		}
		prompt, err := reviewPrompt(r, claim)
		if err != nil {
			t.Fatal(err)
		}
		for _, binding := range []string{`"task": "` + claim.Task.String() + `"`, `"requirementRevision": "` + claim.RequirementRevision + `"`, "identity 填 NATIVE_IDENTITY", "由传输层绑定真实身份"} {
			if !strings.Contains(prompt, binding) {
				t.Fatalf("prompt omits binding %q", binding)
			}
		}
		if step == "start-readiness" {
			for _, meaning := range []string{"only other Batch IDs declared in this plan", "use [] when there are no cross-batch dependencies", "ordered subtasks array, not dependencies", "Do not put Subtask IDs, environment prerequisites or explanatory sentences"} {
				if !strings.Contains(prompt, meaning) {
					t.Fatalf("technical prompt omits %q", meaning)
				}
			}
			plan := props["granularityReview"].(map[string]any)["properties"].(map[string]any)
			if got := plan["requirementRevision"].(map[string]any)["enum"].([]string); len(got) != 1 || got[0] != claim.RequirementRevision {
				t.Fatalf("plan revision is not bound: %v", got)
			}
			batch := plan["batches"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
			desc, _ := batch["dependencies"].(map[string]any)["description"].(string)
			for _, meaning := range []string{"Only other Batch IDs declared in this plan", "use []", "ordered subtasks", "Exclude Subtask IDs, environment prerequisites and explanatory sentences"} {
				if !strings.Contains(desc, meaning) {
					t.Fatalf("dependency schema omits %q: %s", meaning, desc)
				}
			}
		}
	}
}

func copyPromptPackage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"prompts", "gates"} {
		err := filepath.WalkDir(filepath.Join("../../..", dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel("../../..", path)
			if err != nil {
				return err
			}
			target := filepath.Join(root, rel)
			if entry.IsDir() {
				return os.MkdirAll(target, 0700)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestMarkdownInstructionsAreBoundAtStartAndRetainedOnReopen(t *testing.T) {
	pkg := copyPromptPackage(t)
	path := filepath.Join(pkg, "prompts", "actions", "product-review.md")
	firstText := "# Product review\nFirst package instruction: check the public result.\n"
	if err := os.WriteFile(path, []byte(firstText), 0600); err != nil {
		t.Fatal(err)
	}
	f := newRegularFacadePackage(t, b1Document, pkg)
	secondText := "# Product review\nSecond package instruction: changed for new runs.\n"
	if err := os.WriteFile(path, []byte(secondText), 0600); err != nil {
		t.Fatal(err)
	}
	second := newRegularFacadePackage(t, b1Document, pkg)
	// Reopen after the source file is gone: recovery must depend only on bound state.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	claim := protocol.ExecutionClaim{ActionID: "action-from-engine", AttemptID: "attempt-from-engine", Provider: "codex", RequirementRevision: "revision-from-engine", Candidate: "candidate-from-engine", Task: runtime.TaskKey{Node: "review", Step: "product-review"}}
	for _, tc := range []struct {
		f    *Facade
		want string
		omit string
	}{{f, firstText, secondText}, {second, secondText, firstText}} {
		reopened, err := Open(tc.f.root, tc.f.runID)
		if err != nil {
			t.Fatal(err)
		}
		run, err := reopened.Show()
		if err != nil {
			t.Fatal(err)
		}
		prompt, err := reviewPrompt(run, claim)
		if err != nil || !strings.Contains(prompt, tc.want) || strings.Contains(prompt, tc.omit) {
			t.Fatalf("bound Markdown not retained: %v\n%s", err, prompt)
		}
		start := strings.Index(prompt, "\n\n{\n")
		if start < 0 {
			t.Fatal("missing structured input")
		}
		var input struct {
			Binding     protocol.ExecutionClaim      `json:"binding"`
			Requirement protocol.RequirementDocument `json:"requirement"`
		}
		if err := json.Unmarshal([]byte(prompt[start+2:]), &input); err != nil {
			t.Fatal(err)
		}
		wantBinding := claim
		wantBinding.Context = nil
		if input.Binding != wantBinding || input.Requirement.Text != b1Document {
			t.Fatal("Markdown assembly changed the dynamic binding or complete requirement")
		}
	}
}

func TestMissingOrEmptyMarkdownDoesNotFallBackToCode(t *testing.T) {
	pkg := copyPromptPackage(t)
	path := filepath.Join(pkg, "prompts", "actions", "development-worker.md")
	for _, data := range [][]byte{[]byte(" \n"), {0xff}} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadActionInstructions(pkg); err == nil || !strings.Contains(err.Error(), "development-worker.md") {
			t.Fatalf("invalid Markdown was accepted: %v", err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := loadActionInstructions(pkg); err == nil || !strings.Contains(err.Error(), "development-worker.md") {
		t.Fatalf("missing Markdown was accepted: %v", err)
	}
	if _, err := reviewPrompt(Run{Regular: &protocol.RegularState{}}, protocol.ExecutionClaim{Task: runtime.TaskKey{Node: "review", Step: "product-review"}}); err == nil || !strings.Contains(err.Error(), "PROMPT_CONFIG_MISSING") {
		t.Fatalf("unbound prompt silently fell back: %v", err)
	}
}

func TestMarkdownRoleAssemblyKeepsInputBoundaries(t *testing.T) {
	f := frozenVerificationFixture(t, false)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	claim := facadeExecution(t, f, "blackbox-execution").Claim
	p := run.Regular.Preparation
	p.QA["whitebox"] = p.QA["blackbox"]
	for i := range run.Regular.Verification.Waves {
		wave := &run.Regular.Verification.Waves[i]
		if wave.ID == run.Regular.Verification.Current {
			for _, target := range wave.Targets {
				if target.Target == "blackbox" {
					target.Target = "whitebox"
					wave.Targets = append(wave.Targets, target)
					break
				}
			}
		}
	}
	p.Resources["whitebox"] = p.Resources["candidate"]
	repair := protocol.RepairPlan{ID: "repair-fixture", WaveID: run.Regular.Verification.Current}
	run.Regular.Verification.Repairs = []protocol.RepairPlan{repair}
	p.Resources[repair.ID] = p.Resources["candidate"]
	for path := range run.Regular.ActionInstructions {
		run.Regular.ActionInstructions[path] = "Markdown source: " + path
	}
	const common = "# 通用执行约定\nCommon instructions.\n"
	run.Regular.ActionInstructions["actions/development-worker.md"] = common + "\n# 开发工作者\nDevelopment only.\n\n# 修复工作者\nRepair only.\n\n# 工具说明\nTool only.\n"
	run.Regular.ActionInstructions["actions/qa-design.md"] = "# QA 设计\nDesign only.\n\n# 黑盒设计\nBlackbox design.\n\n# 白盒设计\nWhitebox design.\n\n# 白盒覆盖判断\nCoverage only.\n"
	run.Regular.ActionInstructions["actions/qa-review.md"] = "# QA 审查\nReview only.\n\n# 黑盒审查\nBlackbox review.\n\n# 白盒改动与最终候选\nWhitebox only.\n"
	run.Regular.ActionInstructions["actions/qa-execution.md"] = "# QA 执行\nExecute only.\n\n# 黑盒执行\nBlackbox execution.\n\n# 白盒执行\nWhitebox execution.\n"
	for _, tc := range []struct{ step, path string }{
		{"product-review", "actions/product-review.md"}, {"start-readiness", "actions/start-readiness.md"},
		{"development-worker", "actions/development-worker.md#开发工作者"},
		{"blackbox-design", "actions/qa-design.md#QA 设计"}, {"whitebox-design", "actions/qa-design.md#QA 设计"},
		{"blackbox-review", "actions/qa-review.md"}, {"whitebox-review", "actions/qa-review.md"},
		{"blackbox-execution", "actions/qa-execution.md"}, {"whitebox-execution", "actions/qa-execution.md"},
		{"repair-worker", "actions/development-worker.md#修复工作者"},
		{"complexity-gate", ""}, {"implementation-quality-gate", ""},
	} {
		t.Run(tc.step, func(t *testing.T) {
			current := claim
			current.Task.Step = authoring.StepID(tc.step)
			var prompt string
			var err error
			switch {
			case preparationAction(tc.step):
				prompt, _, err = preparationPrompt(run, current)
			case verificationAction(tc.step):
				prompt, _, err = verificationPrompt(run, current)
			default:
				prompt, err = reviewPrompt(run, current)
			}
			if err != nil {
				t.Fatal(err)
			}
			want := common + "\n\n"
			if tc.path != "" {
				role := "Markdown source: " + tc.path
				switch tc.step {
				case "development-worker":
					role = "# 开发工作者\nDevelopment only.\n"
				case "repair-worker":
					role = "# 修复工作者\nRepair only.\n"
				case "blackbox-design":
					role = "# QA 设计\nDesign only.\n\n\n# 黑盒设计\nBlackbox design.\n"
				case "whitebox-design":
					role = "# QA 设计\nDesign only.\n\n\n# 白盒设计\nWhitebox design.\n"
				case "blackbox-review":
					role = "# QA 审查\nReview only.\n\n\n# 黑盒审查\nBlackbox review.\n"
				case "blackbox-execution":
					role = "# QA 执行\nExecute only.\n\n\n# 黑盒执行\nBlackbox execution.\n"
				case "whitebox-execution":
					role = "# QA 执行\nExecute only.\n\n\n# 白盒执行\nWhitebox execution.\n"
				case "whitebox-review":
					role = "# QA 审查\nReview only.\n\n\n# 白盒改动与最终候选\nWhitebox only.\n"
				}
				want += role + "\n\n"
			}
			if !strings.HasPrefix(prompt, want) {
				t.Fatalf("role did not consume its Markdown: %s", prompt)
			}
			var input map[string]json.RawMessage
			if err := json.Unmarshal([]byte(strings.TrimPrefix(prompt, want)), &input); err != nil {
				t.Fatal(err)
			}
			if _, has := input["sourceResults"]; has {
				t.Fatal("repair evidence crossed an independent input boundary")
			}
			if strings.HasSuffix(tc.step, "-gate") {
				var shared, gate string
				if err := json.Unmarshal(input["reviewerInstructions"], &shared); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(input["gateInstructions"], &gate); err != nil {
					t.Fatal(err)
				}
				if shared == "" || shared != run.Regular.ReviewerInstructions || gate == "" || gate != run.Regular.CatalogGateInstructions[tc.step] {
					t.Fatal("gate did not consume its existing base and gate document")
				}
				if input["implementation"] == nil || input["fullImplementationDiff"] != nil || input["reviewerInstructions"] == nil || input["gateInstructions"] == nil || input["design"] != nil || input["frozenQA"] != nil {
					t.Fatal("gate lost its implementation inputs or received QA material")
				}
			}
		})
	}
}

func TestMarkdownSectionsKeepRoleAndCoverageInstructionsSeparate(t *testing.T) {
	instructions, err := loadActionInstructions("../../..")
	if err != nil {
		t.Fatal(err)
	}
	run := Run{Regular: &protocol.RegularState{ActionInstructions: instructions}}
	for _, tc := range []struct {
		selector string
		want     string
		omit     []string
	}{
		{"actions/development-worker.md#通用执行约定", "当前 action", []string{"完整 Batch", "canonical problems", "Call a bounded operation"}},
		{"actions/development-worker.md#开发工作者", "完整 Batch", []string{"canonical problems", "Call a bounded operation"}},
		{"actions/development-worker.md#修复工作者", "canonical problems", []string{"完整 Batch", "Call a bounded operation"}},
		{"actions/development-worker.md#工具说明", "Call a bounded operation", []string{"完整 Batch", "canonical problems"}},
		{"actions/qa-design.md#QA 设计", "草稿增量", []string{"design.testFiles 必须", "whitebox-design："}},
		{"actions/qa-design.md#黑盒设计", "文档化产品入口", []string{"design.testFiles", "whitebox-design："}},
		{"actions/qa-design.md#白盒设计", "先独立确定", []string{"每一条黑盒用例"}},
		{"actions/qa-design.md#白盒覆盖判断", "candidate workspace", []string{"caseDrafts", "冻结命令与模式执行"}},
		{"actions/qa-review.md#QA 审查", "每条用例固定核验五项", []string{"逐项读取 design.testFiles", "必须执行 testChanges.diffArgs"}},
		{"actions/qa-review.md#白盒改动与最终候选", "必须执行 testChanges.diffArgs", []string{"每条用例固定核验五项"}},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			got, err := actionInstructions(run, tc.selector)
			if err != nil || !strings.Contains(got, tc.want) {
				t.Fatalf("missing selected instruction: %v\n%s", err, got)
			}
			for _, omit := range tc.omit {
				if strings.Contains(got, omit) {
					t.Fatalf("unselected instructions leaked: %s", omit)
				}
			}
		})
	}
	for _, content := range []string{"# 开发工作者\nOnly development.\n", "# 修复工作者\n\n# 工具说明\nTool.\n"} {
		run.Regular.ActionInstructions["actions/development-worker.md"] = content
		if _, err := actionInstructions(run, "actions/development-worker.md#修复工作者"); err == nil || !strings.Contains(err.Error(), "PROMPT_CONFIG_MISSING") {
			t.Fatalf("missing or empty section silently fell back to entire worker document: %v", err)
		}
	}
}

func TestCodexJobDirectoryForIssuedBatchAction(t *testing.T) {
	f := &Facade{stateDir: t.TempDir()}
	first := f.codexJob("act:entry/development-worker/digest:B1-0/attempt-1")
	second := f.codexJob("act:entry/development-worker/digest:B1-0/attempt-2")
	if first == second {
		t.Fatal("different attempts share a job directory")
	}
	for _, directory := range []string{first, second} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "process.json"), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
