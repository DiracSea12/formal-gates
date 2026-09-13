package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"formal-gates/internal/coordination"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

// This helper is the fake transport endpoint behind the production Codex
// adapter. It records received turn/start calls, not engine-issued projections.
func TestCLIConsumerFakeServer(t *testing.T) {
	if os.Getenv("FORMAL_GATES_CLI_FAKE") != "1" {
		return
	}
	home := os.Getenv("CODEX_HOME")
	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 65536), 32*1024*1024)
	identity := ""
	for scanner.Scan() {
		var q struct {
			ID     any
			Method string
			Params map[string]json.RawMessage
		}
		if err := json.Unmarshal(scanner.Bytes(), &q); err != nil {
			os.Exit(2)
		}
		send := func(v any) {
			if enc.Encode(v) != nil {
				os.Exit(3)
			}
		}
		reply := func(v any) { send(map[string]any{"id": q.ID, "result": v}) }
		switch q.Method {
		case "initialize":
			reply(map[string]any{})
		case "initialized":
		case "model/list":
			reply(map[string]any{"data": []any{map[string]any{"model": "fixture", "isDefault": true}}})
		case "config/read":
			reply(map[string]any{"config": map[string]any{}})
		case "thread/start", "thread/resume":
			if q.Method == "thread/resume" {
				_ = json.Unmarshal(q.Params["threadId"], &identity)
			} else {
				identity = fmt.Sprintf("cli-fixture-%d", os.Getpid())
			}
			reply(map[string]any{"thread": map[string]any{"id": identity}})
		case "turn/start":
			var schema struct {
				Properties map[string]struct{ Enum []string }
			}
			_ = json.Unmarshal(q.Params["outputSchema"], &schema)
			answer := map[string]any{"status": "PASS", "complete": true, "message": "local fake transport; not review evidence", "findings": []any{}}
			for k, v := range schema.Properties {
				if len(v.Enum) == 1 {
					answer[k] = v.Enum[0]
				}
			}
			step, _ := answer["action"].(string)
			if step == "" {
				var inputs []struct{ Text string }
				_ = json.Unmarshal(q.Params["input"], &inputs)
				for _, name := range []string{"development-worker", "blackbox-design", "whitebox-design"} {
					if len(inputs) > 0 && strings.Contains(inputs[0].Text, name) {
						step = name
						break
					}
				}
			}
			if step == "start-readiness" {
				answer["granularityReview"] = protocol.DevelopmentPlan{RequirementRevision: fmt.Sprint(answer["requirementRevision"]), Rationale: "one fixture batch", Batches: []protocol.DevelopmentBatch{{ID: "B1", Task: "hello", Subtasks: []protocol.DevelopmentSubtask{{ID: "S1", Task: "hello"}}, Dependencies: []string{}, Interfaces: "stdout", StateBoundary: "one command", Risks: "wrong output", DefinitionOfDone: "hello", Validation: "run", Recovery: "same identity", Handoff: "none", ContextCost: "small"}}}
			}
			record, _ := json.Marshal(map[string]string{"identity": identity, "step": step})
			recordPath := filepath.Join(home, fmt.Sprintf("turn-%s-%d.json", identity, time.Now().UnixNano()))
			if err := os.WriteFile(recordPath+".tmp", record, 0600); err != nil {
				os.Exit(4)
			}
			if err := os.Rename(recordPath+".tmp", recordPath); err != nil {
				os.Exit(4)
			}
			reply(map[string]any{"turn": map[string]any{"id": "turn-1"}})
			send(map[string]any{"method": "turn/started", "params": map[string]any{"turn": map[string]any{"id": "turn-1"}}})
			transcript := filepath.Join(home, "sessions", "2026", "01", "01", "fixture-"+identity+".jsonl")
			_ = os.MkdirAll(filepath.Dir(transcript), 0700)
			transcriptRows := []any{
				map[string]any{"type": "session_meta", "payload": map[string]any{"id": identity}},
				map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "type": "event_msg", "payload": map[string]any{"type": "task_started", "turn_id": "turn-1"}},
			}
			var data bytes.Buffer
			for _, row := range transcriptRows {
				_ = json.NewEncoder(&data).Encode(row)
			}
			_ = os.WriteFile(transcript, data.Bytes(), 0600)
			deadline := time.Now().Add(20 * time.Second)
			for {
				_, held := os.Stat(filepath.Join(home, "hold-"+step))
				if os.IsNotExist(held) {
					break
				}
				if time.Now().After(deadline) {
					os.Exit(5)
				}
				time.Sleep(10 * time.Millisecond)
			}
			_ = json.NewEncoder(&data).Encode(map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "type": "event_msg", "payload": map[string]any{"type": "task_complete", "turn_id": "turn-1"}})
			_ = json.NewEncoder(&data).Encode(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "token_count", "info": map[string]any{"total_token_usage": map[string]int{"input_tokens": 10, "output_tokens": 3}}}})
			_ = os.WriteFile(transcript, data.Bytes(), 0600)
			raw, _ := json.Marshal(answer)
			send(map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]any{"type": "agentMessage", "text": string(raw)}}})
			send(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
		default:
			os.Exit(6)
		}
	}
	os.Exit(0)
}

type consumerFixture struct {
	root, home string
	f          *facade.Facade
}

func newConsumerFixture(t *testing.T) *consumerFixture {
	t.Helper()
	root, _ := cliWorkflowFixture(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "fake-codex")
	script := "#!/bin/sh\nFORMAL_GATES_CLI_FAKE=1 exec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestCLIConsumerFakeServer$ -- \"$@\"\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"codexExecutable": fake, "capacity": 4})
	mustWriteCLI(t, filepath.Join(root, ".gates", "config.json"), string(config))
	mustWriteCLI(t, filepath.Join(root, ".gitignore"), ".gates/\n.formal-gates-resources/\n")
	cliGit(t, root, "add", ".gitignore")
	cliGit(t, root, "-c", "core.hooksPath=/dev/null", "commit", "-qm", "local consumer fixture")
	pkg, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := facade.Start(facade.StartOptions{Root: root, PackageRoot: pkg, Request: facade.StartRequest{RunID: "cli-consumer", Provider: "codex", RequirementPath: "requirements.md"}, Admission: &facade.Admission{PackageDigest: "sha256:fixture", InstalledTargetIdentity: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	return &consumerFixture{root, home, f}
}

func (c *consumerFixture) invoke(ctx context.Context, input string, sub string, flags ...string) (int, []byte, string) {
	var out, stderr bytes.Buffer
	args := append([]string{"workflow", sub, "--root", c.root, "--run-id", "cli-consumer"}, flags...)
	code := Run("formal-gates", args, IO{Context: ctx, Stdin: strings.NewReader(input), Stdout: &out, Stderr: &stderr, workflowOpen: func(root, id string) (*facade.Facade, bool, error) { f, e := facade.Open(root, id); return f, true, e }})
	return code, out.Bytes(), stderr.String()
}

func (c *consumerFixture) decodeConsumer(t *testing.T, raw []byte) facade.Run {
	t.Helper()
	var v struct {
		facade.Run
		Nested *facade.Run `json:"run"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("JSON: %v: %s", err, raw)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("extra output: %v %s", err, raw)
	}
	if v.Nested != nil {
		return *v.Nested
	}
	if v.Regular != nil {
		return v.Run // Explicit single-step diagnostic output.
	}
	var response workflowConsumption
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"run", "regular", "executions", "envelope"} {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		if _, ok := fields[field]; ok {
			t.Fatalf("full history field %s in consumption: %s", field, raw)
		}
	}
	// Lifecycle assertions inspect persisted state separately from the CLI's
	// public response. Decisions below use the Ask returned by the CLI.
	run, err := c.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if run.Revision != response.Progress.Revision || !reflect.DeepEqual(run.Next, response.Next) {
		t.Fatalf("lost current revision/next: %s", raw)
	}
	if run.Regular != nil {
		if !reflect.DeepEqual(run.Regular.Ask, response.Ask) {
			t.Fatalf("lost exact Ask: %s", raw)
		}
		run.Regular.Ask = response.Ask
	}
	return run
}
func (c *consumerFixture) call(t *testing.T, sub string, flags ...string) facade.Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	code, out, stderr := c.invoke(ctx, "", sub, flags...)
	if code != 0 || stderr != "" {
		t.Fatalf("%s code=%d err=%s out=%s", sub, code, stderr, out)
	}
	return c.decodeConsumer(t, out)
}
func (c *consumerFixture) submit(t *testing.T, v protocol.RegularEvent, flags ...string) facade.Run {
	t.Helper()
	run, err := c.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	v.ExpectedRevision = run.Revision
	ev, err := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("cli-%d", run.Revision)), v)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(ev)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	code, out, stderr := c.invoke(ctx, string(input), "submit", flags...)
	if code != 0 || stderr != "" {
		t.Fatalf("submit %s code=%d err=%s out=%s", v.Operation, code, stderr, out)
	}
	if len(flags) == 0 && workflowControlEvent(ev) {
		var response workflowConsumption
		if err := json.Unmarshal(out, &response); err != nil {
			t.Fatal(err)
		}
		if response.Progress.AfterRevision != run.Revision || response.Acceptance == nil ||
			response.Acceptance.EventID != string(ev.ID) || response.Acceptance.Revision <= response.Progress.AfterRevision ||
			response.Acceptance.Revision > response.Progress.Revision {
			t.Fatalf("lost pre-submit cursor or current acceptance: %s", out)
		}
	}
	return c.decodeConsumer(t, out)
}
func (c *consumerFixture) choose(t *testing.T, q protocol.RegularRequest, flags ...string) facade.Run {
	t.Helper()
	run := c.submit(t, protocol.RegularEvent{Operation: "REQUEST", Request: &q}, flags...)
	ask := run.Regular.Ask
	if ask == nil {
		t.Fatal("missing Ask")
	}
	return c.submit(t, protocol.RegularEvent{Operation: "DECIDE", Decision: &protocol.RegularDecision{RequestID: ask.ID, Token: ask.Token, Digest: ask.Digest, Choice: "confirm"}}, flags...)
}
func (c *consumerFixture) turns(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(c.home, "turn-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	turns := map[string]string{}
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]string
		if err = json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		turns[p] = v["step"] + ":" + v["identity"]
	}
	return turns
}

func TestCLIConsumerRejectsCallerPromptFieldsBeforeDispatch(t *testing.T) {
	c := newConsumerFixture(t)
	before, err := c.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		`{"ID":"extra-prompt","Kind":"REGULAR","prompt":"caller instructions","regular":{"operation":"REQUEST","request":{"action":"confirm-requirement"}}}`,
		`{"ID":"extra-task","Kind":"REGULAR","regular":{"operation":"REQUEST","request":{"action":"confirm-requirement","task":"caller instructions"}}}`,
	} {
		code, out, stderr := c.invoke(context.Background(), input, "submit", "--single-step")
		if code != 1 || !strings.Contains(stderr, "unknown field") {
			t.Fatalf("extra dispatch text was not rejected: code=%d out=%s err=%s", code, out, stderr)
		}
		after, err := c.f.Show()
		if err != nil {
			t.Fatal(err)
		}
		if after.Revision != before.Revision {
			t.Fatal("rejected input advanced the workflow")
		}
	}
}

func TestCLIConsumerDecisionAndAdjudicationAdvance(t *testing.T) {
	c := newConsumerFixture(t)
	c.call(t, "drive")
	c.choose(t, protocol.RegularRequest{Action: "confirm-requirement"})
	r := c.choose(t, protocol.RegularRequest{Action: "mode", Mode: "regular"})
	if len(c.turns(t)) != 1 || r.Regular.Reviews["product-review"].Completed != 1 {
		t.Fatalf("decision did not execute review: %+v", r.Regular.Reviews)
	}
	r = c.submit(t, protocol.RegularEvent{Operation: "ADJUDICATE", Adjudication: &protocol.ReviewAdjudication{Action: "product-review", ResultDigest: r.Regular.Reviews["product-review"].CurrentDigest, Findings: []protocol.FindingAdjudication{}, CompletenessEvidence: "local fixture"}})
	if len(c.turns(t)) != 2 || r.Regular.Reviews["start-readiness"].Completed != 1 {
		t.Fatalf("adjudication did not execute next review: %+v", r.Regular.Reviews)
	}
	before := c.turns(t)
	for _, flags := range [][]string{nil, {"--continuous"}, nil} {
		prior, err := c.f.Show()
		if err != nil {
			t.Fatal(err)
		}
		code, out, stderr := c.invoke(context.Background(), "", "drive", flags...)
		if code != 0 || stderr != "" {
			t.Fatalf("repeat code=%d stderr=%s out=%s", code, stderr, out)
		}
		c.decodeConsumer(t, out)
		var response workflowConsumption
		if err := json.Unmarshal(out, &response); err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(out, &fields)
		for _, key := range []string{"run", "regular", "executions", "envelope"} {
			if _, exists := fields[key]; exists {
				t.Fatalf("repeated history field %s: %s", key, out)
			}
		}
		if response.Progress.AfterRevision != prior.Revision || len(response.Progress.NewResults) != 0 ||
			response.Progress.Completed == 0 || len(response.Progress.Messages) == 0 {
			t.Fatalf("repeat lost cursor/counts or replayed results: %s", out)
		}
		for _, message := range response.Progress.Messages {
			prompt, err := os.ReadFile(message.Path)
			if err != nil || len(prompt) == 0 {
				t.Fatalf("missing fixture prompt: %v", err)
			}
			encoded, _ := json.Marshal(string(prompt))
			if bytes.Contains(out, encoded) {
				t.Fatal("repeated prompt body in drive output")
			}
		}
	}
	if len(c.turns(t)) != len(before) {
		t.Fatal("completed turns repeated")
	}
}

func TestCLIConsumerDefaultDriveAndResumeIdentity(t *testing.T) {
	c := newConsumerFixture(t)
	c.call(t, "drive", "--single-step")
	c.choose(t, protocol.RegularRequest{Action: "confirm-requirement"}, "--single-step")
	start := c.choose(t, protocol.RegularRequest{Action: "mode", Mode: "regular"}, "--single-step")
	mustWriteCLI(t, filepath.Join(c.home, "hold-product-review"), "hold")
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		code   int
		out    []byte
		stderr string
	}
	done := make(chan result, 1)
	go func() { code, out, stderr := c.invoke(ctx, "", "drive"); done <- result{code, out, stderr} }()
	deadline := time.After(15 * time.Second)
	for len(c.turns(t)) != 1 {
		select {
		case r := <-done:
			t.Fatalf("returned without starting: %+v", r)
		case <-deadline:
			cancel()
			t.Fatal("no real turn/start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	original := c.turns(t)
	// A worker receipt arrives during this drive through the actual submit
	// entry. The local fixture reports a failure without invoking a model.
	active, err := c.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	var binding protocol.ExecutionClaim
	for _, x := range active.Executions {
		if x.Claimed && !x.Finished {
			binding = x.Claim
			binding.Identity = x.Identity
		}
	}
	q := protocol.TaskOperationRequest{RunID: active.RunID, ExpectedRevision: active.Revision,
		Binding: binding, RequestID: "cli-increment-fixture", Kind: "model.run", Prompt: "fixture-only-prompt-must-not-be-echoed"}
	c.submit(t, protocol.RegularEvent{Operation: "TASK_OPERATION_CLAIM", TaskOperation: &q})
	receipt := protocol.TaskOperationReceipt{ID: protocol.TaskOperationID(q), ActionID: binding.ActionID,
		RequestDigest: protocol.TaskOperationDigest(q), Status: "FAILED", Message: "local fixture operation failure"}
	observed, _ := json.Marshal(receipt)
	mustWriteCLI(t, filepath.Join(c.root, facade.EngineNamespace, "cli-consumer", "artifacts", "task-operations",
		strings.TrimPrefix(receipt.ID, "hact:task:"), "observed.json"), string(observed))
	c.submit(t, protocol.RegularEvent{Operation: "TASK_OPERATION_RECEIPT", TaskOperationReceipt: &receipt})
	cancel()
	r := <-done
	if r.code != 130 || !bytes.Contains(r.out, []byte(`"boundary": "INTERRUPTED"`)) {
		t.Fatalf("interrupt: %+v", r)
	}
	var incremental workflowConsumption
	if err := json.Unmarshal(r.out, &incremental); err != nil {
		t.Fatal(err)
	}
	if incremental.Progress.AfterRevision != start.Revision || len(incremental.Progress.NewResults) != 1 || incremental.Progress.NewResults[0].OperationID != receipt.ID ||
		incremental.Progress.NewResults[0].Error != receipt.Message || incremental.Progress.Blocked[receipt.ID] != receipt.Message ||
		bytes.Contains(r.out, []byte(q.Prompt)) {
		t.Fatalf("lost current receipt/exception or leaked prompt: %s", r.out)
	}
	interrupted := c.decodeConsumer(t, r.out)
	identity := ""
	for _, x := range interrupted.Executions {
		if x.Claimed {
			identity = x.Identity
		}
	}
	if identity == "" {
		t.Fatal("started identity not bound")
	}
	if err := os.Remove(filepath.Join(c.home, "hold-product-review")); err != nil {
		t.Fatal(err)
	}
	recovered := c.call(t, "drive")
	if recovered.Regular.Reviews["product-review"].Completed != 1 || len(c.turns(t)) != len(original) {
		t.Fatal("resume duplicated turn or failed collection")
	}
	for _, x := range recovered.Executions {
		if x.Claimed && x.Identity != identity {
			t.Fatal("identity changed")
		}
	}
	code, out, stderr := c.invoke(context.Background(), "", "drive")
	if code != 0 || stderr != "" {
		t.Fatalf("repeat drive code=%d err=%s out=%s", code, stderr, out)
	}
	if err := json.Unmarshal(out, &incremental); err != nil || len(incremental.Progress.NewResults) != 0 {
		t.Fatalf("replayed prior receipt: %v %s", err, out)
	}
	if len(c.turns(t)) != 1 {
		t.Fatal("completed call repeated")
	}
}

func TestCLIConsumerIndependentTurnsOverlap(t *testing.T) {
	c := newConsumerFixture(t)
	c.call(t, "drive")
	c.choose(t, protocol.RegularRequest{Action: "confirm-requirement"})
	r := c.choose(t, protocol.RegularRequest{Action: "mode", Mode: "regular"})
	for _, step := range []string{"product-review", "start-readiness"} {
		r = c.submit(t, protocol.RegularEvent{Operation: "ADJUDICATE", Adjudication: &protocol.ReviewAdjudication{Action: step, ResultDigest: r.Regular.Reviews[step].CurrentDigest, Findings: []protocol.FindingAdjudication{}, CompletenessEvidence: "local fixture"}})
	}
	c.choose(t, protocol.RegularRequest{Action: "topology", PlanDigest: r.Regular.Plan.Digest, Reason: "one fixture batch"})
	c.choose(t, protocol.RegularRequest{Action: "route", Route: &protocol.RegularRoute{Kind: "custom", QA: []string{"blackbox"}}}, "--single-step")
	for _, step := range []string{"development-worker", "blackbox-design"} {
		mustWriteCLI(t, filepath.Join(c.home, "hold-"+step), "hold")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { code, _, _ := c.invoke(ctx, "", "drive"); done <- code }()
	deadline := time.After(15 * time.Second)
	seen := map[string]bool{}
	for !seen["development-worker"] || !seen["blackbox-design"] {
		for _, v := range c.turns(t) {
			seen[strings.SplitN(v, ":", 2)[0]] = true
		}
		select {
		case code := <-done:
			t.Fatalf("returned before overlap: %d %v", code, seen)
		case <-deadline:
			t.Fatalf("missing overlap: %v", seen)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if code := <-done; code != 130 {
		t.Fatalf("cancel=%d", code)
	}
	for _, step := range []string{"development-worker", "blackbox-design"} {
		_ = os.Remove(filepath.Join(c.home, "hold-"+step))
	}
	// Let the already-started owners settle before TempDir cleanup.
	until := time.Now().Add(10 * time.Second)
	for {
		r, err := c.f.Show()
		if err != nil {
			t.Fatal(err)
		}
		active := false
		for _, x := range r.Executions {
			if x.Claimed && !x.Finished {
				active = true
			}
		}
		if !active {
			break
		}
		if time.Now().After(until) {
			t.Fatal("fake owners did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The child process runs the same public CLI boundary against a fixture run;
// no installation, registry mutation, real app-server or model is involved.
func TestCLIConsumerDriverProcess(t *testing.T) {
	root := os.Getenv("FORMAL_GATES_CLI_DRIVER")
	if root == "" {
		return
	}
	c := &consumerFixture{root: root}
	code, out, stderr := c.invoke(context.Background(), "", "drive")
	_, _ = os.Stdout.Write(out)
	_, _ = os.Stderr.WriteString(stderr)
	os.Exit(code)
}

func TestCLIConsumerProcessRestartUsesOriginalIdentity(t *testing.T) {
	c := newConsumerFixture(t)
	c.call(t, "drive", "--single-step")
	c.choose(t, protocol.RegularRequest{Action: "confirm-requirement"}, "--single-step")
	c.choose(t, protocol.RegularRequest{Action: "mode", Mode: "regular"}, "--single-step")
	hold := filepath.Join(c.home, "hold-product-review")
	mustWriteCLI(t, hold, "hold")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestCLIConsumerDriverProcess$")
	cmd.Env = append(os.Environ(), "FORMAL_GATES_CLI_DRIVER="+c.root)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	until := time.Now().Add(15 * time.Second)
	for len(c.turns(t)) != 1 {
		if time.Now().After(until) {
			t.Fatal("child never started native turn")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 130 {
		t.Fatalf("interrupt exit=%v out=%s err=%s", err, &stdout, &stderr)
	}
	r := c.decodeConsumer(t, stdout.Bytes())
	original := ""
	for _, x := range r.Executions {
		if x.Claimed {
			original = x.Identity
		}
	}
	if original == "" {
		t.Fatal("missing original identity")
	}
	// The consumer exited while the fake process was in its turn. Releasing it
	// closes its broken stdout pipe; the next CLI must reconcile that dead owner.
	pid := 0
	err = filepath.WalkDir(filepath.Join(c.root, facade.EngineNamespace, "cli-consumer"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "process.json" {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			var p struct{ PID int }
			if json.Unmarshal(b, &p) == nil && p.PID > 0 {
				pid = p.PID
			}
		}
		return nil
	})
	if err != nil || pid == 0 {
		t.Fatalf("process receipt: pid=%d err=%v", pid, err)
	}
	if err = os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	until = time.Now().Add(10 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(until) {
			t.Fatal("fake process did not exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r = c.call(t, "drive")
	if r.Regular.Reviews["product-review"].Completed != 1 {
		t.Fatal("resumed review not collected")
	}
	for _, x := range r.Executions {
		if x.Claimed && x.Identity != original {
			t.Fatal("resume replaced identity")
		}
	}
	turns := c.turns(t)
	if len(turns) != 2 {
		t.Fatalf("expected one interrupted and one resumed turn: %v", turns)
	}
	for _, v := range turns {
		if v != "product-review:"+original {
			t.Fatalf("wrong resume identity: %v", turns)
		}
	}
	c.call(t, "drive")
	if !reflect.DeepEqual(turns, c.turns(t)) {
		t.Fatal("completed resume invoked again")
	}
}

func TestCLIConsumerSingleJSONOnErrorsAndCanceledControl(t *testing.T) {
	c := newConsumerFixture(t)
	c.call(t, "drive")
	unlock, err := coordination.AcquirePath(filepath.Join(c.root, facade.EngineNamespace, "cli-consumer", "executor.lock"), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	code, out, stderr := c.invoke(context.Background(), "", "drive", "--continuous")
	unlock()
	if code != 1 || stderr == "" {
		t.Fatalf("lock code=%d err=%s out=%s", code, stderr, out)
	}
	c.decodeConsumer(t, out)
	var result workflowConsumption
	if err = json.Unmarshal(out, &result); err != nil || result.Progress.RunID != "cli-consumer" || result.Boundary != "BLOCKED" {
		t.Fatalf("continuous contract %v %s", err, out)
	}
	r := c.submit(t, protocol.RegularEvent{Operation: "REQUEST", Request: &protocol.RegularRequest{Action: "confirm-requirement"}})
	a := r.Regular.Ask
	event, err := protocol.NewRegularEvent("cancel-context-control", protocol.RegularEvent{Operation: "DECIDE", ExpectedRevision: r.Revision, Decision: &protocol.RegularDecision{RequestID: a.ID, Token: a.Token, Digest: a.Digest, Choice: "confirm"}})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(event)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, out, stderr = c.invoke(ctx, string(input), "submit")
	if code != 130 {
		t.Fatalf("cancel code=%d err=%s out=%s", code, stderr, out)
	}
	r = c.decodeConsumer(t, out)
	var envelope struct {
		Acceptance protocol.Acceptance
		Boundary   string
	}
	if err = json.Unmarshal(out, &envelope); err != nil || envelope.Acceptance.EventID != "cancel-context-control" || envelope.Boundary != "INTERRUPTED" || !r.Regular.Requirement.Confirmed {
		t.Fatalf("lost accepted control: %s", out)
	}
	if len(c.turns(t)) != 0 {
		t.Fatal("canceled consumer started work")
	}
	event, err = protocol.NewRegularEvent("accepted-before-lock-error", protocol.RegularEvent{
		Operation: "REQUEST", ExpectedRevision: r.Revision,
		Request: &protocol.RegularRequest{Action: "mode", Mode: "regular"},
	})
	if err != nil {
		t.Fatal(err)
	}
	input, _ = json.Marshal(event)
	unlock, err = coordination.AcquirePath(filepath.Join(c.root, facade.EngineNamespace, "cli-consumer", "executor.lock"), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	code, out, stderr = c.invoke(context.Background(), string(input), "submit")
	unlock()
	if code != 1 || stderr == "" {
		t.Fatalf("accepted control lock error: code=%d err=%s out=%s", code, stderr, out)
	}
	r = c.decodeConsumer(t, out)
	if err = json.Unmarshal(out, &envelope); err != nil ||
		envelope.Acceptance.EventID != "accepted-before-lock-error" ||
		envelope.Boundary != "BLOCKED" || r.Regular.Ask == nil || r.Regular.Ask.Kind != "mode" {
		t.Fatalf("lost acceptance after execution error: %s", out)
	}
}

func TestCLIConsumerDiagnosticFlags(t *testing.T) {
	for _, flags := range [][]string{{"--continuous"}, {"--action-id", "x"}, {"--resume"}, {"--external-executor"}, {"--operation-request", "missing"}, {"--operation-native", "missing"}, {"--operation-mcp"}, {"--native-identity", "x"}} {
		var out, stderr bytes.Buffer
		args := append([]string{"workflow", "drive", "--single-step"}, flags...)
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "--single-step") {
			t.Fatalf("flags %v out=%s err=%s", flags, &out, &stderr)
		}
	}
}

func TestCLIConsumerLightweightDecisionCleansUpWithLease(t *testing.T) {
	c := newConsumerFixture(t)
	c.call(t, "drive")
	c.choose(t, protocol.RegularRequest{Action: "confirm-requirement"})
	r := c.choose(t, protocol.RegularRequest{Action: "mode", Mode: "lightweight"})
	if r.Status != "COMPLETE" {
		t.Fatalf("ordinary lightweight decision did not finish: %+v", r.Next)
	}
	if len(c.turns(t)) != 0 {
		t.Fatal("lightweight invoked a native turn")
	}
	stateDir := filepath.Join(c.root, facade.EngineNamespace, "cli-consumer")
	for _, name := range []string{"state.json", "executor.lock", "cleanup.intent.json", "terminal-summary.intent.json"} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); !os.IsNotExist(err) {
			t.Fatalf("terminal residue %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(stateDir, "terminal-summary.json")); err != nil {
		t.Fatal(err)
	}
	replay := c.call(t, "drive")
	if replay.Status != "COMPLETE" {
		t.Fatalf("terminal replay: %+v", replay.Next)
	}
}

func TestCLIConsumerRepeatedAskCanDecideFromOutput(t *testing.T) {
	c := newConsumerFixture(t)
	c.call(t, "drive")
	r := c.submit(t, protocol.RegularEvent{Operation: "REQUEST", Request: &protocol.RegularRequest{Action: "confirm-requirement"}})
	for _, flags := range [][]string{nil, {"--continuous"}} {
		code, out, stderr := c.invoke(context.Background(), "", "drive", flags...)
		if code != 0 || stderr != "" {
			t.Fatalf("Ask drive code=%d err=%s out=%s", code, stderr, out)
		}
		var response workflowConsumption
		if err := json.Unmarshal(out, &response); err != nil {
			t.Fatal(err)
		}
		if response.Boundary != "ASK" || response.Progress.AfterRevision != r.Revision ||
			!reflect.DeepEqual(response.Ask, r.Regular.Ask) || !reflect.DeepEqual(response.Next, r.Next) {
			t.Fatalf("inexact repeated control payload: %s", out)
		}
		r.Revision = response.Progress.Revision
		r.Regular.Ask = response.Ask
	}
	a := r.Regular.Ask
	event, err := protocol.NewRegularEvent("decide-from-drive-output", protocol.RegularEvent{
		Operation: "DECIDE", ExpectedRevision: r.Revision,
		Decision: &protocol.RegularDecision{RequestID: a.ID, Token: a.Token, Digest: a.Digest, Choice: "confirm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(event)
	code, out, stderr := c.invoke(context.Background(), string(input), "submit")
	if code != 0 || stderr != "" {
		t.Fatalf("decision from output code=%d err=%s out=%s", code, stderr, out)
	}
	var response workflowConsumption
	if err := json.Unmarshal(out, &response); err != nil {
		t.Fatal(err)
	}
	if response.Ask != nil || response.Acceptance == nil || response.Acceptance.EventID != string(event.ID) ||
		response.Progress.AfterRevision != r.Revision || response.Acceptance.Revision <= r.Revision {
		t.Fatalf("lost accepted decision or stale Ask: %s", out)
	}
	if !c.decodeConsumer(t, out).Regular.Requirement.Confirmed {
		t.Fatal("decision not persisted")
	}
}
