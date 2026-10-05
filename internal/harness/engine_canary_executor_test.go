//go:build !windows

package harness

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

// Local answers replace only host transport. Installed CLI commands, issued
// workspaces, all command observations and every Git effect execute normally.
func TestCanaryInstalledGitFlows(t *testing.T) {
	for _, split := range []bool{true, false} {
		name := "split"
		if !split {
			name = "no-split"
		}
		t.Run(name, func(t *testing.T) {
			_, file, _, _ := runtime.Caller(0)
			source := filepath.Dir(filepath.Dir(filepath.Dir(file)))
			home := t.TempDir()
			evidence := t.TempDir()
			if root := os.Getenv("FORMAL_GATES_CANARY_LOCAL_DIR"); root != "" {
				home = filepath.Join(root, name, "home")
				evidence = filepath.Join(root, name, "evidence")
				if err := os.MkdirAll(home, 0700); err != nil {
					t.Fatal(err)
				}
			}
			writeCanaryHostFixture(t, home, "local-only-provider")
			runtimeRoot := t.TempDir()
			script := filepath.Join(runtimeRoot, "glm", "zcode.cjs")
			bundled := filepath.Join(runtimeRoot, "config", "provider", "zcode-builtin.json")
			for _, path := range []string{script, bundled} {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(script, []byte(canaryNativePathPeer), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bundled, readTestFileBytes(t, zcodeTestCatalogue(t, "7")), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FORMAL_GATES_ZCODE_SCRIPT", script)
			c := &engineCanary{t: t, split: split, deadline: time.Now().Add(8 * time.Minute), repairAllowances: 1, caseArgs: map[string][]string{}, caseTests: map[string]string{}}
			if err := c.prepareHome(home, t.TempDir(), source); err != nil {
				t.Fatal(err)
			}
			c.project = filepath.Join(home, ".formal-gates-resources", "project")
			c.jobs = filepath.Join(evidence, "jobs")
			for _, path := range []string{c.project, c.jobs} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			input, approval := io.Pipe()
			defer input.Close()
			defer approval.Close()
			c.approvalInput = bufio.NewReader(input)
			c.approvalOutput = canaryLocalApproval{approval}
			executor := &canaryLocalExecutor{t: t, split: split, developmentStarted: make(chan struct{})}
			c.execute = executor.execute
			var base string
			if split {
				var intake protocol.IntakeConfirmationReceipt
				base, intake = c.buildSplitFixture()
				c.install(base, intake)
			} else {
				base = c.buildFixtureProject()
				c.doc = []byte(canaryRequirementDocument)
				c.revision = canaryBytesRevision(c.doc)
				solution := canaryBytesRevision([]byte(canaryFrobSource))
				c.install(base, protocol.IntakeConfirmationReceipt{Source: "stable-driver", Authority: protocol.IntakeAuthorityStableDriver, Transport: protocol.IntakeTransportStableLauncher, RequirementSource: "requirements.md", RequirementRevision: c.revision, Artifacts: []protocol.IntakeArtifact{{Path: "requirements.md", Revision: c.revision}, {Path: "frob.go", Revision: solution}}, SolutionRevision: solution, SolutionDigest: "sha256:" + solution})
			}
			c.drive()
			c.loop()
			if split {
				c.assertSplitTerminal(base)
				executor.assertOverlap(t)
			} else {
				c.assertTerminal(base, evidence)
			}
			t.Logf("local installed identity=%s package=%s evidence=%s actual_model_calls=0 actual_spend=unknown", c.installed.Targets[0].TargetPath, c.installed.PackageDigest, evidence)
		})
	}
}

type canaryLocalApproval struct{ w io.Writer }

func (a canaryLocalApproval) Write(data []byte) (int, error) {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Type ") {
			token, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(line, "Type "), " to authorize this call; any other line denies it."))
			if err != nil {
				return 0, err
			}
			go fmt.Fprintln(a.w, token)
		}
	}
	return len(data), nil
}

type canaryLocalCall struct {
	RunID, ActionID, Handler string
	Start, End               time.Time
}
type canaryLocalExecutor struct {
	t                  *testing.T
	split              bool
	mu                 sync.Mutex
	calls              []canaryLocalCall
	developmentStarted chan struct{}
	developmentOnce    sync.Once
}

func (e *canaryLocalExecutor) execute(value any, dir string, probe bool) (source map[string]any, err error) {
	request := value.(map[string]any)
	lines := strings.Split(textOf(request["prompt"]), "\n")
	if len(lines) < 3 || probe {
		return nil, fmt.Errorf("local executor requires exact dispatched material")
	}
	data, err := os.ReadFile(lines[2])
	if err != nil {
		return nil, err
	}
	var material canaryMaterials
	if err := json.Unmarshal(data, &material); err != nil {
		return nil, err
	}
	workspace := textOf(request["workspace"])
	handler := material.ReviewAction
	readOnly := true
	if material.Input != nil {
		input := material.Input
		handler = input.Handler
		if input.Workspace == nil || input.Workspace.Status != "READY" || input.Binding.WorkspaceID != input.Workspace.ID || workspace != input.Workspace.Path || material.RequirementRevision != input.Binding.RequirementRevision || !canaryLocalDocumentMatches(material.RequirementPath, input.RequirementDocument.Content) {
			return nil, fmt.Errorf("local dispatch lost issued run/workspace/requirement identity")
		}
		readOnly = handler != "engine.development.batch" && handler != "engine.repair" && handler != "engine.merge.resolve" && !(handler == "engine.qa.design" && input.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox)
	}
	if request["read_only"] != readOnly || material.ActionID == "" || material.RunID == "" || material.Pending == nil || material.Pending.ActionID != material.ActionID || filepath.Base(filepath.Dir(dir)) != canaryName(material.ActionID) || filepath.Base(filepath.Dir(filepath.Dir(dir))) != material.RunID {
		return nil, fmt.Errorf("local executor saw wrong method or dispatch identity")
	}
	call := canaryLocalCall{RunID: material.RunID, ActionID: material.ActionID, Handler: handler, Start: time.Now()}
	defer func() { call.End = time.Now(); e.mu.Lock(); e.calls = append(e.calls, call); e.mu.Unlock() }()
	if e.split && handler == "engine.development.batch" {
		e.developmentOnce.Do(func() { close(e.developmentStarted) })
	}
	if e.split && handler == "engine.qa.design" {
		select {
		case <-e.developmentStarted:
		case <-time.After(time.Minute):
			return nil, fmt.Errorf("independent child development did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	answer, err := e.answer(material, workspace)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	identity := "local-executor:" + material.RunID + "/" + material.ActionID + ":" + filepath.Base(dir)
	source = map[string]any{"provider": "zcode", "identity": identity, "model_calls": 0, "usage_evidence": map[string]any{"source": "unavailable"}}
	for name, value := range map[string]any{"answer.json": answer, "identity.json": map[string]any{"identity": identity, "workspace": workspace, "runId": material.RunID, "actionId": material.ActionID, "handler": handler}, "native-source.json": source, "native-events.jsonl": map[string]any{"sessionId": identity, "payload": map[string]any{"fixture": "local-only", "runId": material.RunID, "actionId": material.ActionID}}} {
		body, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(dir, name), append(body, '\n'), 0600); err != nil {
			return nil, err
		}
	}
	raw, _ := json.Marshal(answer)
	err = os.WriteFile(filepath.Join(dir, "answer.raw.txt"), raw, 0600)
	return source, err
}

func (e *canaryLocalExecutor) answer(m canaryMaterials, workspace string) (map[string]any, error) {
	if m.ReviewAction != "" {
		answer := map[string]any{"status": "PASS", "findings": []any{}, "verification": map[string]any{"checks": []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, "evidence": "Read exact confirmed document: " + m.RequirementPath + " revision " + m.RequirementRevision}}
		if m.ReviewAction == string(review.StartReadiness) {
			plan := canaryLocalPlan(e.split)
			answer["plan"] = plan
		}
		return answer, nil
	}
	input := *m.Input
	switch input.Handler {
	case "engine.development.batch", "engine.repair":
		paths := []string{}
		dod, checks := map[string]string{}, map[string]string{}
		if input.Development != nil {
			for _, path := range input.Development.Batch.Artifacts {
				content := ""
				switch path {
				case "uppercase.go":
					content = "package main\nimport \"strings\"\nfunc UpperWord(word string)string{return strings.ToUpper(word)}\n"
				case "size.go":
					content = "package main\nfunc WordSize(word string)int{return len(word)}\n"
				case "analytics.go":
					content = "package main\nimport \"fmt\"\nfunc usage(word string){fmt.Printf(\"usage: runs=1 words=%d\\n\",len(word))}\n"
				default:
					return nil, fmt.Errorf("unexpected planned artifact %s", path)
				}
				if err := os.WriteFile(filepath.Join(workspace, path), []byte(content), 0600); err != nil {
					return nil, err
				}
				paths = append(paths, path)
			}
			if !e.split {
				main := canaryFrobSource
				main = strings.Replace(main, "fmt.Printf(\"frob: %s\\n\", os.Args[1])", "fmt.Printf(\"frob: %s\\n\", os.Args[1])\n\tusage(os.Args[1])", 1)
				if err := os.WriteFile(filepath.Join(workspace, "frob.go"), []byte(main), 0600); err != nil {
					return nil, err
				}
				paths = append(paths, "frob.go")
			}
			for _, criterion := range input.Development.Batch.DoD {
				dod[criterion.ID] = "Implemented " + criterion.Description
			}
			for _, check := range input.Development.Batch.Checks {
				observation := canaryLocalObserve(workspace, strings.Fields(check.Command))
				if observation.ExitCode != 0 {
					return nil, fmt.Errorf("development check failed: %+v", observation)
				}
				checks[check.ID] = observation.Output + " exit=0"
			}
		} else {
			path := filepath.Join(workspace, "frob.go")
			body, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			content := strings.Replace(string(body), "fmt.Printf(\"frob: %s\\n\", os.Args[1])", "if len(os.Args[1])>0 && os.Args[1][0]=='-' {fmt.Fprintln(os.Stderr,\"unknown flag\");os.Exit(2)}\n\tfmt.Printf(\"frob: %s\\n\", os.Args[1])", 1)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				return nil, err
			}
			paths = []string{"frob.go"}
		}
		build := canaryLocalObserve(workspace, []string{"go", "build", "./..."})
		if build.ExitCode != 0 {
			return nil, fmt.Errorf("local build failed: %+v", build)
		}
		tests := canaryLocalObserve(workspace, []string{"go", "test", "./..."})
		answer := map[string]any{"paths": paths, "summary": "implemented declared artifact contract", "build": []string{fmt.Sprintf("go build ./... exit=%d %s", build.ExitCode, build.Output)}, "tests": []string{fmt.Sprintf("go test ./... exit=%d %s", tests.ExitCode, tests.Output)}}
		if input.Development != nil {
			answer["dod"], answer["checks"], answer["handoff"] = dod, checks, "declared artifacts ready"
		} else {
			answer["affectedCases"] = []string{}
		}
		return answer, nil
	case "engine.qa.design":
		return e.design(input, workspace)
	case "engine.qa.review.rationale":
		decisions := func(ids []string) []any {
			out := []any{}
			for _, id := range ids {
				out = append(out, map[string]any{"id": id, "status": "PASS", "reason": "case contract independently covers " + id})
			}
			return out
		}
		points, cases := []string{}, []string{}
		for _, p := range input.Manifest.Points {
			points = append(points, p.PointID)
		}
		for _, c := range input.Manifest.Cases {
			cases = append(cases, c.CaseID)
		}
		return map[string]any{"sourceDecisions": decisions(input.Manifest.Sources), "pointDecisions": decisions(points), "caseDecisions": decisions(cases)}, nil
	case "engine.qa.execution":
		records := []any{}
		for _, observation := range m.HostObservations {
			caseID := strings.TrimPrefix(observation.Label, "case ")
			passed := false
			if input.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
				passed = observation.ExitCode == 0
			} else {
				point := canaryCasePoint(input, caseID)
				args := observation.Command[1:]
				passed = e.matches(point, args, observation)
			}
			result := "EXECUTED_FAIL"
			if passed {
				result = "EXECUTED_PASS"
			}
			records = append(records, map[string]any{"caseId": caseID, "result": result, "procedure": strings.Join(observation.Command, " "), "observation": fmt.Sprintf("exit=%d output=%s", observation.ExitCode, observation.Output), "comparison": "compared exact output and exit against the bound point oracle"})
		}
		return map[string]any{"records": records}, nil
	case "engine.gate":
		passed := true
		for _, observation := range m.HostObservations {
			if input.Gate.Gate == "regression" && e.split {
				passed = passed && observation.ExitCode == 0
				continue
			}
			point := "AC-002"
			if e.split {
				point = "AC-003"
				if m.Slice == "uppercase" {
					point = "AC-001"
				} else if m.Slice == "word-size" {
					point = "AC-002"
				}
			}
			passed = passed && e.matches(point, observation.Command[1:], observation)
		}
		if passed {
			return map[string]any{"status": "PASS", "findings": []any{}}, nil
		}
		finding := map[string]any{"severity": "P1", "message": "unknown flags are accepted", "severityRationale": "the public flag contract fails on its normal command entry", "requirement": "AC-002", "normalPath": "frob --unknown-flag canary", "actualError": "exit=0 and frob output", "deferralImpact": "unknown flags execute as words", "locations": []string{"frob.go"}}
		return map[string]any{"status": "FAIL", "findings": []any{finding}}, nil
	}
	return nil, fmt.Errorf("local executor has no answer for issued handler %s", input.Handler)
}

func canaryLocalPlan(split bool) *protocol.GranularityReview {
	batch := func(id, artifact, deliverable string) protocol.DevelopmentBatch {
		return protocol.DevelopmentBatch{ID: id, Artifacts: []string{artifact}, Subtasks: []protocol.DevelopmentSubtask{{ID: id + "-implementation", Description: deliverable}}, Deliverable: deliverable, PublicEntry: "frob word", Interfaces: "function called by seeded main", StateBoundary: "no persistent state", Risk: "wrong text result", DoD: []protocol.DevelopmentCriterion{{ID: "output", Description: deliverable}}, Checks: []protocol.DevelopmentCheck{{ID: "build", Command: "go build ./..."}}, Rollback: "restore artifact from base", Recovery: "repeat declared implementation", Handoff: "function ready for command integration", ContextCost: "one function contract"}
	}
	plan := &protocol.GranularityReview{Reason: "independent declared artifacts", VerificationTier: "FINAL"}
	if split {
		plan.Batches = []protocol.DevelopmentBatch{batch("uppercase", "uppercase.go", "UpperWord returns Unicode uppercase including empty input"), batch("word-size", "size.go", "WordSize returns UTF-8 byte length including empty input")}
	} else {
		plan.Batches = []protocol.DevelopmentBatch{batch("usage", "analytics.go", "usage prints the byte count and main invokes it")}
	}
	return plan
}
func (e *canaryLocalExecutor) design(input protocol.ValidationInput, workspace string) (map[string]any, error) {
	cases := []any{}
	whitebox := input.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox
	var tests strings.Builder
	if whitebox {
		tests.WriteString("package main\nimport(\"testing\";\"os/exec\";\"strings\")\n")
	}
	for _, point := range input.Manifest.Points {
		samples := [][]string{{"hello"}, {""}}
		if e.split {
			samples = [][]string{{"héLLo"}, {""}}
		} else if point.PointID == "AC-002" {
			samples = [][]string{{"--unknown-flag", "canary"}, {"--bogus"}}
		}
		for index, args := range samples {
			id := "case-" + point.PointID + "-" + strconv.Itoa(index)
			scenario := "SUCCESS"
			if index == 1 {
				scenario = "BOUNDARY"
			}
			if !e.split && point.PointID == "AC-002" {
				scenario = "MISUSE"
			}
			oracle := e.oracle(point.PointID, args)
			designed := map[string]any{"caseId": id, "pointId": point.PointID, "scenario": scenario, "oracle": oracle}
			if whitebox {
				ref := "TestContract" + strings.ReplaceAll(point.PointID, "-", "") + strconv.Itoa(index)
				argv := []string{"run", "."}
				argv = append(argv, args...)
				quoted := []string{}
				for _, arg := range argv {
					quoted = append(quoted, strconv.Quote(arg))
				}
				expected := e.expected(point.PointID, args)
				assertion := fmt.Sprintf("if err!=nil||strings.TrimSpace(string(out))!=%q{t.Fatalf(\"command: %%v %%s\",err,out)}", expected)
				if point.PointID == "AC-002" {
					assertion = "if err==nil||strings.Contains(string(out),\"frob:\"){t.Fatalf(\"flag contract: %v %s\",err,out)}"
				}
				fmt.Fprintf(&tests, "func %s(t *testing.T){out,err:=exec.Command(\"go\",%s).CombinedOutput();%s}\n", ref, strings.Join(quoted, ","), assertion)
				designed["testRef"] = ref
			} else {
				designed["args"], designed["preconditions"], designed["steps"] = args, "compiled candidate in issued workspace", "run frob with exact argv"
			}
			cases = append(cases, designed)
		}
	}
	answer := map[string]any{"cases": cases}
	if whitebox {
		path := "frob_contract_test.go"
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(tests.String()), 0600); err != nil {
			return nil, err
		}
		build, run := canaryLocalObserve(workspace, []string{"go", "build", "./..."}), canaryLocalObserve(workspace, []string{"go", "test", "./..."})
		answer["paths"], answer["summary"], answer["build"], answer["tests"] = []string{path}, "authored bound command contract cases", []string{fmt.Sprintf("exit=%d %s", build.ExitCode, build.Output)}, []string{fmt.Sprintf("exit=%d %s", run.ExitCode, run.Output)}
	}
	return answer, nil
}
func (e *canaryLocalExecutor) expected(point string, args []string) string {
	word := args[0]
	if e.split {
		return fmt.Sprintf("frob: %s bytes=%d", strings.ToUpper(word), len(word))
	}
	return fmt.Sprintf("frob: %s\nusage: runs=1 words=%d", word, len(word))
}
func (e *canaryLocalExecutor) oracle(point string, args []string) string {
	if !e.split && point == "AC-002" {
		return "nonzero exit and no frob: output"
	}
	return "exit=0 output=" + e.expected(point, args)
}
func (e *canaryLocalExecutor) matches(point string, args []string, observation canaryObservation) bool {
	if !e.split && point == "AC-002" {
		return observation.ExitCode != 0 && !strings.Contains(observation.Output, "frob:")
	}
	if observation.ExitCode != 0 || len(args) == 0 {
		return false
	}
	if e.split && point == "AC-001" {
		return strings.HasPrefix(observation.Output, "frob: "+strings.ToUpper(args[0])+" bytes=")
	}
	if e.split && point == "AC-002" {
		return strings.HasSuffix(observation.Output, fmt.Sprintf(" bytes=%d", len(args[0])))
	}
	return observation.Output == e.expected(point, args)
}
func canaryCasePoint(input protocol.ValidationInput, caseID string) string {
	for _, edge := range input.Manifest.Edges {
		if edge.CaseID == caseID {
			return edge.PointID
		}
	}
	return ""
}
func canaryLocalObserve(workspace string, argv []string) canaryObservation {
	if len(argv) == 3 && argv[0] == "go" && argv[1] == "build" {
		output, err := os.MkdirTemp("", "canary-build-")
		if err != nil {
			return canaryObservation{Directory: workspace, Command: argv, ExitCode: 1, Output: err.Error()}
		}
		defer os.RemoveAll(output)
		argv = []string{"go", "build", "-o", filepath.Join(output, "frob"), argv[2]}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		exit = 1
		if failure, ok := err.(*exec.ExitError); ok {
			exit = failure.ExitCode()
		}
	}
	return canaryObservation{Directory: workspace, Command: argv, ExitCode: exit, Output: strings.TrimSpace(string(output))}
}
func canaryBytesRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (e *canaryLocalExecutor) assertOverlap(t *testing.T) {
	e.mu.Lock()
	defer e.mu.Unlock()
	overlapping := false
	for _, dev := range e.calls {
		if dev.Handler != "engine.development.batch" {
			continue
		}
		for _, qa := range e.calls {
			if (qa.Handler == "engine.qa.design" || qa.Handler == "engine.qa.review.rationale") && dev.RunID != qa.RunID && dev.Start.Before(qa.End) && qa.Start.Before(dev.End) {
				overlapping = true
			}
		}
	}
	if !overlapping {
		t.Fatal("child development and master merge case preparation did not execute concurrently")
	}
	for _, call := range e.calls {
		t.Logf("local actual call interval run=%s action=%s handler=%s start=%s end=%s", call.RunID, call.ActionID, call.Handler, call.Start.UTC().Format(time.RFC3339Nano), call.End.UTC().Format(time.RFC3339Nano))
	}
}

func canaryLocalDocumentMatches(path, content string) bool {
	data, err := os.ReadFile(path)
	return err == nil && string(data) == content
}

// Equal local action IDs belong to different runs. A blocked failing worker
// still reports a typed completion while independent work can finish and refill.
func TestCanaryWorkerCompletionKeepsRunIdentity(t *testing.T) {
	home := t.TempDir()
	jobs := t.TempDir()
	input, approval := io.Pipe()
	defer input.Close()
	defer approval.Close()
	shared := &sync.Mutex{}
	reader := bufio.NewReader(input)
	completed := make(chan canaryCompletion)
	release := make(chan struct{})
	started := make(chan struct{})
	executor := &canaryLocalExecutor{t: t}
	launch := func(runID string) {
		project := filepath.Join(home, ".formal-gates-resources", runID)
		if err := os.MkdirAll(project, 0700); err != nil {
			t.Fatal(err)
		}
		doc := []byte(canarySplitRequirement)
		if err := os.WriteFile(filepath.Join(project, "requirements.md"), doc, 0600); err != nil {
			t.Fatal(err)
		}
		pending := protocol.PendingAction{ActionID: "same-action", ReviewSeries: review.ProductReview}
		c := &engineCanary{t: t, worker: true, home: home, project: project, jobs: jobs, run: facade.Run{RunID: runID, PendingActions: map[string]protocol.PendingAction{"same-action": pending}}, revision: canaryBytesRevision(doc), deadline: time.Now().Add(time.Minute), approvalMu: shared, approvalInput: reader, approvalOutput: canaryLocalApproval{approval}, caseArgs: map[string][]string{}, caseTests: map[string]string{}}
		// One reader is shared together with its authorization mutex.
		if runID == "failed" {
			c.execute = func(any, string, bool) (map[string]any, error) {
				select {
				case <-started:
				default:
					close(started)
				}
				<-release
				return nil, fmt.Errorf("local worker transport failed")
			}
		} else {
			c.execute = executor.execute
		}
		go func() { completed <- c.dispatch("same-action", pending, protocol.ValidationInput{}) }()
	}
	launch("failed")
	<-started
	launch("independent")
	done := <-completed
	if done.runID != "independent" || done.event.Result.Outcome != protocol.OutcomePass {
		t.Fatalf("independent completion=%+v", done)
	}
	launch("refilled")
	refill := <-completed
	if refill.runID != "refilled" || refill.event.Result.Outcome != protocol.OutcomePass {
		t.Fatalf("refill waited for failing worker: %+v", refill)
	}
	close(release)
	failed := <-completed
	if failed.runID != "failed" || failed.event.Result.Outcome != protocol.OutcomeRuntimeError {
		t.Fatalf("failure lost completion: %+v", failed)
	}
	for _, runID := range []string{"independent", "refilled"} {
		identity, err := readJSONFile(filepath.Join(jobs, runID, "same-action", "attempt-1", "identity.json"))
		if err != nil || getOf(identity, "runId") != runID || getOf(identity, "actionId") != "same-action" {
			t.Fatalf("job identity=%#v error=%v", identity, err)
		}
	}
}
