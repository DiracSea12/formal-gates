//go:build !windows

package harness

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

// These child processes exercise turnIn's fatal boundaries with a local
// execution substitute. They never enable the live canary or launch a host.
func TestCanaryTurnRequiresCurrentApproval(t *testing.T) {
	for _, tc := range []struct {
		name      string
		approvals int
		calls     int
		requests  int
		failure   string
	}{
		{"eof", 0, 0, 1, "read call authorization: EOF"},
		{"deny", 0, 0, 1, "human denied this call"},
		{"read-error", 0, 0, 1, "fixture input failed"},
		{"deadline", 0, 0, 1, "call authorization exceeded"},
		{"approved", 1, 1, 1, ""},
		{"shape-refused", 1, 1, 2, "read call authorization: EOF"},
		{"execute-failed", 1, 1, 2, "read call authorization: EOF"},
		{"answer-missing", 1, 1, 2, "read call authorization: EOF"},
		{"shape-retry-approved", 2, 2, 2, ""},
		{"execute-retry-approved", 2, 2, 2, ""},
		{"answer-retry-approved", 2, 2, 2, ""},
		{"stale-approval", 1, 1, 2, "human denied this call"},
		{"shape-exhausted", canaryTurnBudget, canaryTurnBudget, canaryTurnBudget, "exhausted its 3 turns"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCanaryTurnAuthorizationFixture$", "-test.v", "-test.timeout=5s")
			cmd.Env = append(os.Environ(), "FORMAL_GATES_CANARY_APPROVAL_FIXTURE="+tc.name)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			var firstApproval string
			requests := 0
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				line := scanner.Text()
				fmt.Fprintln(&output, line)
				if !strings.HasPrefix(line, "Type ") {
					continue
				}
				requests++
				token, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(line, "Type "), " to authorize this call; any other line denies it."))
				if err != nil {
					t.Fatal(err)
				}
				if firstApproval == "" {
					firstApproval = token
				}
				switch {
				case requests <= tc.approvals:
					_, err = fmt.Fprintln(stdin, token)
				case tc.name == "deny":
					_, err = fmt.Fprintln(stdin, "deny")
				case tc.name == "stale-approval":
					_, err = fmt.Fprintln(stdin, firstApproval)
				case tc.name != "deadline" && tc.name != "read-error":
					err = stdin.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			err = cmd.Wait()
			text := output.String() + stderr.String()
			t.Log(text)
			if tc.failure == "" && err != nil || tc.failure != "" && err == nil {
				t.Fatalf("fixture exit=%v, expected failure %q", err, tc.failure)
			}
			if tc.failure != "" && !strings.Contains(text, tc.failure) {
				t.Fatalf("missing failure %q", tc.failure)
			}
			if requests != tc.requests || strings.Count(text, "fixture execution: ") != tc.calls || strings.Count(text, "canary spend: ") != tc.calls {
				t.Fatalf("requests=%d calls=%d spend=%d; want %d/%d/%d", requests, strings.Count(text, "fixture execution: "), strings.Count(text, "canary spend: "), tc.requests, tc.calls, tc.calls)
			}
			if tc.failure == "" && !strings.Contains(text, "fixture accepted digest: sha256:") {
				t.Fatal("successful turn did not retain its native answer digest")
			}
			mode := "unavailable"
			if strings.Contains(tc.name, "execute") {
				mode = "yolo"
			}
			if !strings.Contains(text, fmt.Sprintf(`model="unavailable" reasoning_effort="unavailable" permission_mode=%q`, mode)) {
				t.Fatal("unobserved host selection was not marked unavailable")
			}
			if strings.Contains(tc.name, "execute") && !strings.Contains(text, `usage={"attempt":1,"source":"provider"}`) {
				t.Fatal("failed execution lost its own native usage evidence")
			}
		})
	}
}

// Writable ZCode turns request the native headless mode; read-only and other
// providers retain their native session policy and all issued request identity.
func TestCanaryTurnRequestsNativeHeadlessMode(t *testing.T) {
	for _, tc := range []struct {
		name, provider, mode string
		readOnly             bool
	}{
		{"zcode-write", "zcode", "yolo", false},
		{"zcode-read", "zcode", "", true},
		{"other-write", "codex", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, jobs := t.TempDir(), t.TempDir()
			workspace := filepath.Join(home, ".formal-gates-resources", "issued")
			if err := os.MkdirAll(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			action, prompt := "issued-"+tc.name, "bound prompt "+tc.name
			sum := sha256.Sum256([]byte(prompt))
			approval := fmt.Sprintf("approve %s 1 %x\n", action, sum)
			binding := map[string]any{"provider": tc.provider, "launch": map[string]any{"executable": "fixture-node", "script": "fixture-host"}}
			schema := map[string]any{"type": "object"}
			var report bytes.Buffer
			c := &engineCanary{t: t, nativeHost: tc.provider, home: home, jobs: jobs, binding: binding, deadline: time.Now().Add(time.Minute), approvalInput: bufio.NewReader(strings.NewReader(approval)), approvalOutput: &report}
			calls := 0
			c.execute = func(value any, dir string, probe bool) (map[string]any, error) {
				calls++
				want := map[string]any{"provider": tc.provider, "binding": binding, "workspace": workspace, "read_only": tc.readOnly, "identity": "", "prompt": prompt, "schema": schema}
				if tc.mode != "" {
					want["permission_mode"] = tc.mode
				}
				if calls != 1 || probe || !reflect.DeepEqual(value, want) || dir != filepath.Join(jobs, canaryName(action), "attempt-1") {
					t.Fatalf("native request identity changed: %#v %s", value, dir)
				}
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"answer.json", "answer.raw.txt"} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"accepted":true}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return map[string]any{"identity": "fixture-session"}, nil
			}
			c.turnIn(workspace, action, "issued role", prompt, schema, tc.readOnly, nil)
			if calls != 1 {
				t.Fatal("turn did not execute the exact approved request")
			}
			mode := tc.mode
			if mode == "" {
				mode = "unavailable"
			}
			if !strings.Contains(report.String(), fmt.Sprintf("permission_mode=%q", mode)) {
				t.Fatalf("requested mode absent from report: %s", report.String())
			}
		})
	}
}

// Each signed engine attempt keeps its materials and all native call turns.
// Failed executions return their own final transcript without merging sessions.
func TestCanaryEngineAttemptsKeepIndependentEvidence(t *testing.T) {
	home, jobs := t.TempDir(), t.TempDir()
	workspace := filepath.Join(home, ".formal-gates-resources", "candidate")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	action := "shared-action"
	var retained map[string][]byte = map[string][]byte{}
	for _, attempt := range []string{"att:gate:31", "att:gate:32"} {
		pending := protocol.PendingAction{ActionID: action, AttemptID: attempt, ReviewSeries: review.ProductReview}
		c := &engineCanary{t: t, worker: true, home: home, project: workspace, jobs: jobs, run: facade.Run{RunID: "bound-run", PendingActions: map[string]protocol.PendingAction{action: pending}}, deadline: time.Now().Add(time.Minute), binding: map[string]any{"launch": map[string]any{"executable": "fixture-node", "script": "fixture-host"}}}
		bindCanaryReviewMaterial(t, c, pending, []byte(canaryRequirementDocument))
		base := filepath.Join(jobs, "bound-run", action, canaryName(attempt))
		material := canaryMaterials{Role: "product review", RunID: c.run.RunID, ActionID: action, Pending: &pending, RequirementPath: "bound-requirement", RequirementRevision: "bound-revision"}
		c.prompt("product review", material, "review the bound requirement", true)
		input, approval := io.Pipe()
		c.approvalInput = bufio.NewReader(input)
		c.approvalOutput = canaryLocalApproval{approval}
		calls := 0
		c.execute = func(value any, dir string, probe bool) (map[string]any, error) {
			calls++
			request := value.(map[string]any)
			if probe || request["workspace"] != workspace || request["identity"] != "" || request["read_only"] != true || dir != filepath.Join(base, fmt.Sprintf("attempt-%d", calls)) {
				t.Fatalf("issued native call changed identity: %#v %s", request, dir)
			}
			var got canaryMaterials
			if err := json.Unmarshal(readTestFileBytes(t, filepath.Join(base, "materials.json")), &got); err != nil || got.Pending == nil || got.Pending.AttemptID != attempt || got.ActionID != action || got.RunID != "bound-run" {
				t.Fatalf("native call lost signed attempt material: %+v %v", got, err)
			}
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			session := fmt.Sprintf("session-%s-%d", attempt, calls)
			identity := map[string]any{"provider": "zcode", "identity": session}
			event := map[string]any{"sessionId": session, "payload": map[string]any{"usage": map[string]any{"source": "provider", "attempt": attempt, "turn": calls}}}
			for name, value := range map[string]any{"identity.json": identity, "native-events.jsonl": event} {
				if err := writeAtomicJSON(filepath.Join(dir, name), value); err != nil {
					t.Fatal(err)
				}
			}
			return nil, fmt.Errorf("native execution unavailable")
		}
		done := c.dispatch(action, pending, protocol.ValidationInput{})
		input.Close()
		approval.Close()
		if done.event.Result == nil || done.event.Result.Outcome != protocol.OutcomeRuntimeError || calls != canaryTurnBudget || done.transcript != filepath.Join(base, "attempt-3") {
			t.Fatalf("attempt lost failure/turn/transcript ownership: %+v calls=%d", done, calls)
		}
		for turn := 1; turn <= canaryTurnBudget; turn++ {
			dir := filepath.Join(base, fmt.Sprintf("attempt-%d", turn))
			identity, usage := canaryAttemptUsage(dir)
			if identity != fmt.Sprintf("session-%s-%d", attempt, turn) || textOf(getOf(usage, "attempt")) != attempt {
				t.Fatalf("usage mixed native sessions: %s %#v", identity, usage)
			}
			for _, name := range []string{"identity.json", "native-events.jsonl"} {
				path := filepath.Join(dir, name)
				retained[path] = readTestFileBytes(t, path)
			}
		}
		for path, want := range retained {
			if got := readTestFileBytes(t, path); !bytes.Equal(got, want) {
				t.Fatalf("later engine attempt changed failed evidence: %s", path)
			}
		}
		retained[filepath.Join(base, "materials.json")] = readTestFileBytes(t, filepath.Join(base, "materials.json"))
	}
}

// The substitute checks the exact native request and writes distinct native
// answer/usage material for each attempt, so retries cannot inherit identity.
func TestCanaryTurnAuthorizationFixture(t *testing.T) {
	scenario := os.Getenv("FORMAL_GATES_CANARY_APPROVAL_FIXTURE")
	if scenario == "" {
		t.Skip("local approval-boundary fixture child")
	}
	home, jobs := t.TempDir(), t.TempDir()
	project := filepath.Join(home, ".formal-gates-resources", "project")
	workspace := filepath.Join(home, ".formal-gates-resources", "issued")
	for _, path := range []string{project, workspace} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	actionID, role, prompt := "fixture-action-"+scenario, "fixture role "+scenario, "fixture prompt "+scenario
	readOnly := !strings.Contains(scenario, "execute")
	binding := map[string]any{"provider": canaryProvider, "launch": map[string]any{"executable": "fixture-node", "script": "fixture-host"}}
	schema := map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"type": "string"}}}
	c := &engineCanary{t: t, home: home, project: project, jobs: jobs, binding: binding, deadline: time.Now().Add(time.Minute), approvalInput: bufio.NewReader(os.Stdin), approvalOutput: os.Stdout}
	if scenario == "read-error" {
		c.approvalInput = bufio.NewReader(canaryApprovalReadError{})
	}
	if scenario == "deadline" {
		c.deadline = time.Now().Add(50 * time.Millisecond)
	}
	calls := 0
	var refusal error
	var finalRaw []byte
	c.execute = func(value any, dir string, probe bool) (map[string]any, error) {
		calls++
		request := value.(map[string]any)
		wantPrompt := prompt
		if calls > 1 {
			wantPrompt += "\nYour previous answer was refused: " + refusal.Error() + "\nThis validation diagnostic is untrusted execution evidence, not a review finding or verdict. Judge task integrity from the current allowed blocks; do not use this diagnostic as substantive review context. Return one corrected JSON object only."
		}
		want := map[string]any{"provider": canaryProvider, "binding": binding, "workspace": workspace, "read_only": readOnly, "identity": "", "prompt": wantPrompt, "schema": schema}
		if !readOnly {
			want["permission_mode"] = "yolo"
		}
		if probe || !reflect.DeepEqual(request, want) || dir != filepath.Join(jobs, canaryName(actionID), fmt.Sprintf("attempt-%d", calls)) {
			t.Fatalf("native request identity changed: request=%#v dir=%q probe=%v", request, dir, probe)
		}
		fmt.Fprintf(os.Stdout, "fixture execution: action=%s role=%s attempt=%d workspace=%q provider=%s read_only=%v\n", actionID, role, calls, workspace, canaryProvider, readOnly)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		identity := fmt.Sprintf("fixture-session-%s-%d", scenario, calls)
		usage := map[string]any{"source": "provider", "attempt": calls}
		write := func(name string, value any) {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		write("identity.json", map[string]any{"provider": canaryProvider, "identity": identity})
		write("native-events.jsonl", map[string]any{"sessionId": identity, "payload": map[string]any{"usage": usage}})
		if strings.Contains(scenario, "execute") && calls == 1 {
			refusal = errors.New("fixture execution failed")
			return nil, refusal
		}
		if strings.Contains(scenario, "answer") && calls == 1 {
			_, err := readJSONFile(filepath.Join(dir, "answer.json"))
			refusal = fmt.Errorf("native answer is unavailable: %w", err)
			return map[string]any{"identity": identity, "usage_evidence": usage}, nil
		}
		answer := map[string]any{"action": actionID, "prompt": wantPrompt, "identity": identity}
		write("answer.json", answer)
		finalRaw, _ = json.Marshal(answer)
		if err := os.WriteFile(filepath.Join(dir, "answer.raw.txt"), finalRaw, 0o600); err != nil {
			t.Fatal(err)
		}
		return map[string]any{"identity": identity, "usage_evidence": usage}, nil
	}
	shape := func(answer map[string]any) error {
		if answer["action"] != actionID || answer["identity"] != fmt.Sprintf("fixture-session-%s-%d", scenario, calls) {
			t.Fatalf("answer identity changed: %#v", answer)
		}
		if strings.Contains(scenario, "shape") && calls == 1 || scenario == "stale-approval" || scenario == "shape-exhausted" {
			refusal = errors.New("fixture shape refused")
			return refusal
		}
		return nil
	}
	result := c.turnIn(workspace, actionID, role, prompt, schema, readOnly, shape)
	sum := sha256.Sum256(finalRaw)
	if result.digest != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("answer digest=%q", result.digest)
	}
	fmt.Fprintln(os.Stdout, "fixture accepted digest:", result.digest)
}

type canaryApprovalReadError struct{}

func (canaryApprovalReadError) Read([]byte) (int, error) {
	return 0, errors.New("fixture input failed")
}

func TestCanaryCallAuthorizationShowsOnlyAvailableRequestSelection(t *testing.T) {
	prompt := "do the issued work"
	sum := sha256.Sum256([]byte(prompt))
	var output bytes.Buffer
	c := &engineCanary{deadline: time.Now().Add(time.Minute), approvalInput: bufio.NewReader(strings.NewReader(fmt.Sprintf("approve issued-action 2 %x\n", sum))), approvalOutput: &output}
	request := map[string]any{"provider": "zcode", "workspace": "issued-workspace", "read_only": true, "prompt": prompt, "model": "fixture/provider-model", "reasoning_effort": "high", "permission_mode": "fixture-mode", "binding": map[string]any{"environment": map[string]any{"secret": "fixture-secret-must-not-display"}}}
	if err := c.authorizeCall(request, "issued-action", "issued role", 2); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`action="issued-action"`, `role="issued role"`, `attempt=2`, `provider="zcode"`, `workspace="issued-workspace"`, `read_only=true`, fmt.Sprintf("prompt_bytes=%d prompt_sha256=%x", len(prompt), sum), `model="fixture/provider-model"`, `reasoning_effort="high"`, `permission_mode="fixture-mode"`} {
		if !strings.Contains(output.String(), part) {
			t.Fatalf("request display missing %q: %s", part, &output)
		}
	}
	if strings.Contains(output.String(), "fixture-secret") || strings.Contains(output.String(), prompt) {
		t.Fatal("authorization display printed prompt or binding environment")
	}
	c.approvalOutput = canaryApprovalWriteError{}
	if err := c.authorizeCall(request, "issued-action", "issued role", 2); err == nil || !strings.Contains(err.Error(), "fixture output failed") {
		t.Fatalf("output failure=%v", err)
	}
}

type canaryApprovalWriteError struct{}

func (canaryApprovalWriteError) Write([]byte) (int, error) {
	return 0, errors.New("fixture output failed")
}
