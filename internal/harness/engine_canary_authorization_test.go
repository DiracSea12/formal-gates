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
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
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
			if !strings.Contains(text, `model="unavailable" reasoning_effort="unavailable" permission_mode="unavailable"`) {
				t.Fatal("unobserved host selection was not marked unavailable")
			}
			if strings.Contains(tc.name, "execute") && !strings.Contains(text, `usage={"attempt":1,"source":"provider"}`) {
				t.Fatal("failed execution lost its own native usage evidence")
			}
		})
	}
}

// The substitute checks the exact native request and writes distinct native
// answer/usage material for each attempt, so retries cannot inherit identity.
func TestCanaryTurnAuthorizationFixture(t *testing.T) {
	scenario := os.Getenv("FORMAL_GATES_CANARY_APPROVAL_FIXTURE")
	if scenario == "" {
		t.Skip("local approval-boundary fixture child")
	}
	project, workspace, jobs := t.TempDir(), t.TempDir(), t.TempDir()
	actionID, role, prompt := "fixture-action-"+scenario, "fixture role "+scenario, "fixture prompt "+scenario
	readOnly := !strings.Contains(scenario, "execute")
	binding := map[string]any{"provider": canaryProvider, "launch": map[string]any{"executable": "fixture-node", "script": "fixture-host"}}
	schema := map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"type": "string"}}}
	c := &engineCanary{t: t, project: project, jobs: jobs, binding: binding, deadline: time.Now().Add(time.Minute), approvalInput: bufio.NewReader(os.Stdin), approvalOutput: os.Stdout}
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
			wantPrompt += "\n\nYour previous answer was refused: " + refusal.Error() + "\nReturn one corrected JSON object only."
		}
		want := map[string]any{"provider": canaryProvider, "binding": binding, "workspace": workspace, "read_only": readOnly, "identity": "", "prompt": wantPrompt, "schema": schema}
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
		write("identity.json", map[string]any{"identity": identity})
		write("native-events.jsonl", map[string]any{"sessionId": identity, "payload": map[string]any{"usage": usage}})
		if strings.Contains(scenario, "execute") && calls == 1 {
			refusal = errors.New("fixture execution failed")
			return nil, refusal
		}
		if strings.Contains(scenario, "answer") && calls == 1 {
			refusal = fmt.Errorf("native answer is unavailable: %w", &os.PathError{Op: "open", Path: filepath.Join(dir, "answer.json"), Err: os.ErrNotExist})
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
