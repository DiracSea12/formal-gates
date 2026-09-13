package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/lifecycle"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRegularConfigurationUsesExistingDiagnoseSurface(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	os.MkdirAll(filepath.Join(root, ".gates"), 0700)
	os.WriteFile(filepath.Join(root, ".gates", "config.json"), []byte(`{"tokenCap":1234,"capacity":2}`), 0600)
	var out, stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "diagnose", "--root", root, "--config"}, IO{Stdout: &out, Stderr: &stderr})
	if code != 0 || !strings.Contains(out.String(), `"tokenCap": 1234`) || !strings.Contains(out.String(), "config.json") {
		t.Fatalf("config %d %s %s", code, out.String(), stderr.String())
	}
	os.WriteFile(filepath.Join(root, ".gates", "config.json"), []byte(`{"tokenCap":"1234"}`), 0600)
	out.Reset()
	stderr.Reset()
	if code = Run("formal-gates", []string{"workflow", "diagnose", "--root", root, "--config"}, IO{Stdout: &out, Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "tokenCap") {
		t.Fatalf("configuration typo not located: %d %s", code, stderr.String())
	}
}

// Every operation here executes the installed public binary. The default source
// events are explicitly deterministic fixtures; the opt-in product review uses
// the real Codex transport and its real transcript through workflow drive.
func TestRegularInstalledPublicIntakeToSeal(t *testing.T) {
	native := os.Getenv("FORMAL_GATES_REAL_CODEX") == "1"
	authHome := os.Getenv("CODEX_HOME")
	if authHome == "" {
		authHome = filepath.Join(os.Getenv("HOME"), ".codex")
	}
	root, _ := cliWorkflowFixture(t)
	if native {
		t.Setenv("CODEX_HOME", authHome)
	}
	binary, _ := installHostCLI(t, lifecycle.ProviderCodex, root)
	if !native {
		configureProtocolFixtureRuntime(t, root)
	}
	cliGit(t, root, "rm", "whitebox_delivered_test.go")
	mustWriteCLI(t, filepath.Join(root, ".gitignore"), ".gates/\n.codex/\n.formal-gates-resources/\n")
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliStructuredRequirement("The hello command prints exactly hello followed by a newline. No options or other output are required."))
	cliGit(t, root, "add", ".gitignore", "AGENTS.md", "requirements.md")
	cliGit(t, root, "commit", "-m", "B1 installed intake fixture")
	const runID = "b1-installed"
	var nativeEnv []string
	if native {
		nativeEnv = []string{"CODEX_HOME=" + authHome}
	}
	call := func(sub string, tail ...string) facade.Run {
		args := []string{"workflow", sub, "--root", root, "--run-id", runID}
		if sub == "drive" && len(tail) == 0 {
			args = append(args, "--single-step")
		}
		args = append(args, tail...)
		out := runInstalledCLI(t, binary, root, nativeEnv, "", args...)
		var r facade.Run
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := call("start", "--requirement", "requirements.md", "--vcs", "git")
	r = call("drive")
	submit := func(v protocol.RegularEvent) {
		v.ExpectedRevision = r.Revision
		ev, _ := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("public-%d", r.Revision)), v)
		data, _ := json.Marshal(ev)
		out := runInstalledCLI(t, binary, root, nativeEnv, string(data), "workflow", "submit", "--single-step", "--root", root, "--run-id", runID)
		var response struct {
			Run facade.Run `json:"run"`
		}
		if err := json.Unmarshal([]byte(out), &response); err != nil {
			t.Fatal(err)
		}
		r = response.Run
	}
	choose := func(q protocol.RegularRequest) {
		submit(protocol.RegularEvent{Operation: "REQUEST", Request: &q})
		a := r.Regular.Ask
		submit(protocol.RegularEvent{Operation: "DECIDE", Decision: &protocol.RegularDecision{RequestID: a.ID, Token: a.Token, Digest: a.Digest, Choice: "confirm"}})
	}
	choose(protocol.RegularRequest{Action: "confirm-requirement"})
	choose(protocol.RegularRequest{Action: "mode", Mode: "regular"})
	for _, action := range []string{"product-review", "start-readiness"} {
		var x protocol.RegularExecution
		for _, v := range r.Executions {
			if string(v.Claim.Task.Step) == action && !v.Finished {
				x = v
			}
		}
		if action == "product-review" {
			before := r.Revision
			cmd := exec.Command(binary, "workflow", "drive", "--root", root, "--run-id", runID, "--action-id", "unissued")
			cmd.Env = hostFilteredEnv(nil)
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "UNKNOWN_ACTION") {
				t.Fatalf("native rejection %s %v", output, err)
			}
			r = call("show")
			if r.Revision != before {
				t.Fatal("unissued action mutated state")
			}
		}
		if action == "product-review" {
			submit(protocol.RegularEvent{Operation: "REQUEST", Request: &protocol.RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "Exercise native rejection while paused"}})
			cmd := exec.Command(binary, "workflow", "drive", "--root", root, "--run-id", runID, "--action-id", x.Claim.ActionID)
			cmd.Env = hostFilteredEnv(nativeEnv)
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "EVENT_NOT_CURRENT") {
				t.Fatalf("paused native entry: %s %v", output, err)
			}
			a := r.Regular.Ask
			submit(protocol.RegularEvent{Operation: "DECIDE", Decision: &protocol.RegularDecision{RequestID: a.ID, Token: a.Token, Digest: a.Digest, Choice: "cancel"}})
		}
		if native && action == "product-review" {
			r = call("drive", "--action-id", x.Claim.ActionID)
			if r.Regular.Reviews[action].Completed != 1 {
				t.Fatalf("native review was not accepted: %+v", r.Regular.RejectedResults)
			}
		} else {
			submit(protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
			identity := "fixture-" + action
			submit(protocol.RegularEvent{Operation: "SOURCE", Source: &protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: identity, Status: "RUNNING"}})
			raw := protocol.RawReviewCandidate{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: identity, Action: action, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "PASS", Message: "Full current requirement and normal-path acceptance checked.", Complete: true, Findings: []protocol.ReviewFinding{}}
			if action == "start-readiness" {
				raw.GranularityReview = &protocol.DevelopmentPlan{RequirementRevision: x.Claim.RequirementRevision, Rationale: "One output behavior", Batches: []protocol.DevelopmentBatch{{ID: "B1", Task: "Print hello", Subtasks: []protocol.DevelopmentSubtask{{ID: "S1", Task: "Implement output"}}, Interfaces: "CLI", StateBoundary: "Output", Risks: "Wrong string", DefinitionOfDone: "AC-001", Validation: "Run command", Recovery: "Revert commit", Handoff: "No unresolved issues", ContextCost: "One responsibility"}}}
			}
			submit(protocol.RegularEvent{Operation: "REVIEW_RESULT", Review: &raw})
			submit(protocol.RegularEvent{Operation: "SOURCE", Source: &protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: identity, Status: "UNAVAILABLE", Reason: "Deterministic CLI contract fixture has no native transcript"}})
		}
		series := r.Regular.Reviews[action]
		findings := []protocol.FindingAdjudication{}
		for _, raw := range series.RawCandidates {
			if raw.Digest == series.CurrentDigest {
				for _, f := range raw.Findings {
					findings = append(findings, protocol.FindingAdjudication{FindingID: f.ID, Decision: "DISMISSED", Severity: f.Severity, Category: f.Category, Reason: "Fixture explicitly specifies this one output behavior", Evidence: "Read complete single-command requirement"})
				}
			}
		}
		submit(protocol.RegularEvent{Operation: "ADJUDICATE", Adjudication: &protocol.ReviewAdjudication{Action: action, ResultDigest: series.CurrentDigest, Findings: findings, CompletenessEvidence: "Checked full requirement, output boundary and acceptance entry"}})
	}
	choose(protocol.RegularRequest{Action: "topology", PlanDigest: r.Regular.Plan.Digest, Reason: "One output responsibility"})
	choose(protocol.RegularRequest{Action: "route", Route: &protocol.RegularRoute{Kind: "full"}})
	if r.Regular.Guarantee.Status != "ACTIVE" || r.Regular.Topology != "no-split" || len(r.Regular.RequiredSources.Sources) == 0 {
		t.Fatalf("not ready: %+v", r.Regular)
	}
	prepared := call("drive")
	if prepared.Regular.Preparation == nil || len(prepared.Regular.Preparation.Resources) != 2 {
		t.Fatal("preparation did not create development and baseline QA workspaces")
	}
	replay := call("drive")
	if replay.Revision != prepared.Revision {
		t.Fatal("prepared replay changed revision")
	}
	r = replay
	if native {
		if evidence := os.Getenv("FORMAL_GATES_REAL_EVIDENCE"); evidence != "" {
			data, _ := json.MarshalIndent(r, "", "  ")
			if err := os.WriteFile(strings.TrimSuffix(evidence, ".json")+"-installed.json", data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The complete installed route below uses explicit worker result fixtures and
	// actual Git/build/test/command side effects. Native worker behavior has its
	// own opt-in facade test, so these identities never claim model execution.
	currentExecution := func(step string) protocol.RegularExecution {
		for _, x := range r.Executions {
			if string(x.Claim.Task.Step) == step && !x.Finished {
				return x
			}
		}
		t.Fatalf("missing installed action %s: %+v", step, r.Next)
		return protocol.RegularExecution{}
	}
	claim := func(x protocol.RegularExecution) protocol.ExecutionSource {
		submit(protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
		source := protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: "installed-protocol-fixture:" + x.Claim.ActionID, Status: "RUNNING"}
		submit(protocol.RegularEvent{Operation: "SOURCE", Source: &source})
		return source
	}
	stop := func(source protocol.ExecutionSource) {
		source.Status = "UNAVAILABLE"
		source.Reason = "Deterministic installed CLI worker fixture; actual commands recorded separately"
		submit(protocol.RegularEvent{Operation: "SOURCE", Source: &source})
	}
	prep := func(step string, result protocol.PreparationResult) {
		x := currentExecution(step)
		var source protocol.ExecutionSource
		if x.Claimed {
			source = *x.Source
		} else {
			source = claim(x)
		}
		result.ActionID = x.Claim.ActionID
		result.AttemptID = x.Claim.AttemptID
		result.Identity = source.Identity
		result.Provider = "codex"
		result.RequirementRevision = x.Claim.RequirementRevision
		result.Candidate = x.Claim.Candidate
		result.Status = "PASS"
		if strings.HasSuffix(step, "-design") {
			result.Status = "DRAFT"
		}
		submit(protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
		stop(source)
	}
	runGo := func(dir string, args ...string) string {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go %v: %s %v", args, out, err)
		}
		return string(out)
	}
	// Use the installed external-author entry in this existing complete fixture.
	// This is a real CLI/Git lifecycle, not native-model or independent QA evidence.
	previous := currentExecution("development-worker")
	choose(protocol.RegularRequest{Action: "executor-handoff", Target: previous.Claim.ActionID, Reason: "Fixture user explicitly assigns this unfinished batch to the local external author", Handoff: &protocol.ExecutorHandoff{To: protocol.ExecutorTarget{Kind: "external", Provider: "chatgpt", Label: "installed CLI fixture author"}}})
	assigned := currentExecution("development-worker")
	externalInput := func() facade.ExternalDevelopment {
		output := runInstalledCLI(t, binary, root, nativeEnv, "", "workflow", "drive", "--root", root, "--run-id", runID, "--action-id", assigned.Claim.ActionID, "--external-executor")
		var input facade.ExternalDevelopment
		if err := json.Unmarshal([]byte(output), &input); err != nil {
			t.Fatal(err)
		}
		return input
	}
	input := externalInput()
	r = call("show")
	claimedRevision := r.Revision
	if input.Binding.Provider != "chatgpt" || input.Binding.Identity == "" || input.Binding.Task != previous.Claim.Task || input.Binding.ActionID == previous.Claim.ActionID || input.Workspace != r.Regular.Preparation.Resources["development"] || len(input.Input) == 0 || len(input.Schema) == 0 {
		t.Fatalf("installed external input lost its issued binding/materials: %+v", input.Binding)
	}
	if again := externalInput(); !reflect.DeepEqual(again.Binding, input.Binding) {
		t.Fatal("reading an already claimed external input changed identity")
	}
	r = call("show")
	if r.Revision != claimedRevision {
		t.Fatal("reading external input consumed another attempt")
	}
	development := input.Workspace
	mustWriteCLI(t, filepath.Join(development, "go.mod"), "module example.invalid/installed\n\ngo 1.22\n")
	mustWriteCLI(t, filepath.Join(development, "main.go"), "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"hello\")}\n")
	runGo(development, "build", "-o", filepath.Join(t.TempDir(), "hello"), ".")
	if runGo(development, "run", ".") != "hello\n" {
		t.Fatal("actual installed fixture output wrong")
	}
	c := input.Binding
	result := protocol.PreparationResult{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, Identity: c.Identity, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Status: "PASS", Message: "Explicit external fixture author; no native or independent QA claim", Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "go build ./...; go run .", Observation: "build succeeded; stdout hello newline", Result: "PASS"}}, Handoff: "No unresolved decisions"}
	delivery, err := protocol.NewRegularEvent("installed-external-delivery", protocol.RegularEvent{Operation: "PREPARATION_RESULT", ExpectedRevision: r.Revision, PreparationResult: &result})
	if err != nil {
		t.Fatal(err)
	}
	deliveryBytes, err := json.Marshal(delivery)
	if err != nil {
		t.Fatal(err)
	}
	var accepted struct {
		Acceptance protocol.Acceptance `json:"acceptance"`
		Run        facade.Run          `json:"run"`
	}
	output := runInstalledCLI(t, binary, root, nativeEnv, string(deliveryBytes), "workflow", "submit", "--single-step", "--root", root, "--run-id", runID)
	if err := json.Unmarshal([]byte(output), &accepted); err != nil || accepted.Run.Regular == nil || accepted.Run.Regular.Preparation.Batches["B1"].Identity != c.Identity {
		t.Fatal("installed external result was not accepted", err)
	}
	r = call("drive")
	var replayed struct {
		Acceptance protocol.Acceptance `json:"acceptance"`
		Run        facade.Run          `json:"run"`
	}
	output = runInstalledCLI(t, binary, root, nativeEnv, string(deliveryBytes), "workflow", "submit", "--single-step", "--root", root, "--run-id", runID)
	if err := json.Unmarshal([]byte(output), &replayed); err != nil || !reflect.DeepEqual(replayed.Acceptance, accepted.Acceptance) || replayed.Run.Revision != r.Revision || !reflect.DeepEqual(replayed.Run.Regular.Preparation.Commits, r.Regular.Preparation.Commits) {
		t.Fatal("installed delivery replay changed its receipt, commit or revision", err)
	}
	t.Logf("installed external action=%s identity=%s commit=%s; exact delivery replay preserved revision %d", c.ActionID, c.Identity, r.Regular.Preparation.Commits["B1"].After, r.Revision)
	design := func(mode string) protocol.QADesign {
		prefix := strings.ToUpper(mode)
		m := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{SourceBinding: r.Regular.RequiredSources.Binding, ReviewKind: coverage.ReviewKind(prefix), RouteScope: gitprep.Digest(r.Regular.Route), TopologyScope: r.Regular.Topology}, Sources: []string{r.Regular.Requirement.Path}, Points: []coverage.AcceptancePoint{{PointID: prefix + ":AC-001", ObservableBehavior: "documented hello command", Oracle: "hello newline"}}, Cases: []coverage.AcceptanceCase{{CaseID: prefix + ":C1", Command: &coverage.Command{Executable: "go", Args: []string{"run", "."}}, Mode: coverage.CaseMode(prefix), PublicEntry: "go run .", Preconditions: "Go installed", Steps: "execute command and compare stdout", Oracle: "hello newline"}}, Edges: []coverage.CoverageEdge{{SourceID: r.Regular.Requirement.Path, PointID: prefix + ":AC-001", CaseID: prefix + ":C1"}}}
		if mode == "whitebox" {
			m.Cases[0] = coverage.AcceptanceCase{CaseID: prefix + ":C1", Command: &coverage.Command{Executable: "go", Args: []string{"test", "./..."}}, Mode: coverage.CaseWhitebox, TestRef: "main_test.go::TestHello", Oracle: "hello newline"}
		}
		d := protocol.QADesign{Manifest: m, CoverageJudgment: "This fixture independently maps the sole public output AC"}
		if mode == "whitebox" {
			d.TestFiles = []string{"main_test.go"}
		}
		return d
	}
	review := func(mode string) {
		q := r.Regular.Preparation.QA[mode]
		prefix := strings.ToUpper(mode)
		v := coverage.ReviewResult{Binding: q.Design.Manifest.Binding, Scope: q.ReviewScope, SourceDecisions: []coverage.ReviewDecision{{ID: r.Regular.Requirement.Path, Status: coverage.StatusPass}}, PointDecisions: []coverage.ReviewDecision{{ID: prefix + ":AC-001", Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: prefix + ":C1", Status: coverage.StatusPass}}}
		prep(mode+"-review", protocol.PreparationResult{Review: &v})
	}
	black := design("blackbox")
	black.Manifest.Cases[0].CaseID = "local-output"
	black.Manifest.Edges[0].CaseID = "local-output"
	prep("blackbox-design", protocol.PreparationResult{Design: &black})
	review("blackbox")
	r = call("drive")
	whiteWorker := currentExecution("whitebox-design")
	claim(whiteWorker)
	bound := r.Executions[whiteWorker.Claim.ActionID].Claim
	bound.Identity = r.Executions[whiteWorker.Claim.ActionID].Identity
	operation := protocol.TaskOperationRequest{RunID: runID, ExpectedRevision: r.Revision, Binding: bound, RequestID: "coverage-before-tests", Kind: "whitebox.coverage", Prompt: `{"coverageJudgment":"independent hello behavior","behaviorOracles":[{"behavior":"hello command","oracle":"stdout exactly hello newline"}]}`}
	operationPath := filepath.Join(t.TempDir(), "coverage-operation.json")
	operationData, _ := json.Marshal(operation)
	mustWriteCLI(t, operationPath, string(operationData))
	runInstalledCLI(t, binary, root, nativeEnv, "", "workflow", "drive", "--root", root, "--run-id", runID, "--operation-request", operationPath)
	r = call("show")
	whiteRoot := r.Regular.Preparation.Resources["whitebox"]
	mustWriteCLI(t, filepath.Join(whiteRoot, "main_test.go"), "package main\nimport (\"os/exec\";\"testing\")\nfunc TestHello(t *testing.T){b,e:=exec.Command(\"go\",\"run\",\".\").CombinedOutput();if e!=nil||string(b)!=\"hello\\n\"{t.Fatalf(\"%q %v\",b,e)}}\n")
	actualTests := runGo(whiteRoot, "test", "./...")
	white := design("whitebox")
	prep("whitebox-design", protocol.PreparationResult{Manifest: []string{"main_test.go"}, Design: &white, Reuse: []protocol.TestReuse{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "subprocess asserts actual output", Procedure: "go test ./...", Observation: actualTests, Comparison: "output equals hello newline", Result: "EXECUTED_PASS"}}})
	r = call("drive")
	review("whitebox")
	r = call("drive")
	var wave *protocol.VerificationWave
	for i := range r.Regular.Verification.Waves {
		if r.Regular.Verification.Waves[i].ID == r.Regular.Verification.Current {
			wave = &r.Regular.Verification.Waves[i]
		}
	}
	if wave == nil || len(wave.Targets) != 4 {
		t.Fatal("full installed route did not freeze two QA and two gates")
	}
	targetSteps := []string{}
	for _, target := range wave.Targets {
		targetSteps = append(targetSteps, string(target.Task.Step))
	}
	for _, step := range targetSteps {
		x := currentExecution(step)
		source := claim(x)
		result := protocol.VerificationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: source.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, BindingDigest: x.Claim.PrerequisiteDigest, Status: "PASS", Message: "Installed protocol fixture with actual public command execution"}
		if strings.HasSuffix(step, "-execution") {
			prefix := strings.ToUpper(strings.TrimSuffix(step, "-execution"))
			var receipt protocol.TaskOperationReceipt
			for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
				r = call("show")
				bound := r.Executions[x.Claim.ActionID].Claim
				bound.Identity = source.Identity
				q := protocol.TaskOperationRequest{RunID: runID, ExpectedRevision: r.Revision, Binding: bound, RequestID: kind, Kind: kind, CaseID: prefix + ":C1"}
				path := filepath.Join(t.TempDir(), "operation.json")
				data, _ := json.Marshal(q)
				mustWriteCLI(t, path, string(data))
				runInstalledCLI(t, binary, root, nativeEnv, "", "workflow", "drive", "--root", root, "--run-id", runID, "--operation-request", path)
				r = call("show")
				receipt = *r.Executions[x.Claim.ActionID].Operations[protocol.TaskOperationID(q)].Receipt
				if receipt.Status != "COMPLETED" || receipt.ExitCode != 0 {
					t.Fatalf("%s: %+v", kind, receipt)
				}
			}
			result.Cases = []protocol.CaseExecution{{CaseID: prefix + ":C1", Status: "PASS", Procedure: "frozen command through task operation", Output: receipt.Output, Comparison: "documented output/test assertion passed", Environment: "installed package isolated Go project", Tool: "go", Workspace: receipt.Workspace}}
		}
		submit(protocol.RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &result})
		stop(source)
	}
	r = call("drive")
	if r.Regular.Terminal != "SEALED" || r.Regular.Verification.Seal == nil {
		t.Fatalf("installed full route failed to Seal: %+v", r.Next)
	}
	if runGo(root, "run", ".") != "hello\n" {
		t.Fatal("actual installed promoted candidate differs")
	}
	head, err := gitprep.Head(root)
	if err != nil || head != r.Regular.Verification.Seal.Candidate {
		t.Fatal("installed Seal does not bind actual HEAD", err)
	}
	r = call("drive")
	if r.Status != "COMPLETE" {
		t.Fatalf("cleanup: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates", "engine", runID, "executions")); !os.IsNotExist(err) {
		t.Fatal("owned native resources survived cleanup")
	}
	call("show")
}

// Protocol-only installed tests must not depend on a host Codex installation.
func configureProtocolFixtureRuntime(t *testing.T, root string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]string{"codexExecutable": executable})
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(root, ".gates", "config.json"), string(data))
}
