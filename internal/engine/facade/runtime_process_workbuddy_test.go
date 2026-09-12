package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Native-process fixture only: this executable never invokes a model or host CLI.
func TestWorkBuddyProcessChild(t *testing.T) {
	mode := os.Getenv("FORMAL_GATES_WORKBUDDY_PROCESS_FIXTURE")
	if mode == "" {
		return
	}
	fmt.Println(`{"type":"system","subtype":"init","session_id":"native-session"}`)
	if mode == "attach" {
		for {
			if _, err := os.Stat("attached"); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	fmt.Fprintln(os.Stderr, "fixture diagnostic")
	if mode == "auth" {
		fmt.Println(`{"type":"assistant","session_id":"native-session","message":{"content":[{"type":"text","text":"Authentication required. Please use /login command to sign in to your account"}],"usage":{"input_tokens":0,"output_tokens":0}}}`)
		time.Sleep(time.Hour) // Native authentication rejection may leave stdout open.
		os.Exit(8)
	}
	if mode == "broken" {
		fmt.Print("{")
		os.Exit(0)
	}
	fmt.Println(`{"type":"result","subtype":"success","uuid":"r","session_id":"native-session","is_error":false,"structured_output":{"status":"PASS"}}`)
	if mode == "exit-error" {
		os.Exit(7)
	}
	os.Exit(0)
}

func TestWorkBuddyProcessRecordsAndAttaches(t *testing.T) {
	for _, mode := range []string{"attach", "broken", "exit-error", "auth"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			job := filepath.Join(dir, "job")
			cmd := exec.Command(os.Args[0], "-test.run=^TestWorkBuddyProcessChild$")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "FORMAL_GATES_WORKBUDDY_PROCESS_FIXTURE="+mode)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			attached := 0
			observed, err := runWorkBuddyProcess(ctx, cmd, job, "action", "native-session", "fixture", func(identity string) error {
				attached++
				data, readErr := os.ReadFile(filepath.Join(job, "stdout.jsonl"))
				if readErr != nil {
					return readErr
				}
				if identity != "native-session" || !strings.Contains(string(data), identity) {
					return fmt.Errorf("identity attached before journaling")
				}
				return os.WriteFile(filepath.Join(dir, "attached"), nil, 0600)
			})
			if (err == nil) != (mode == "attach") {
				t.Fatalf("mode=%s observed=%+v err=%v", mode, observed, err)
			}
			if mode == "auth" && (ctx.Err() != nil || !strings.Contains(err.Error(), "WORKBUDDY_AUTH:")) {
				t.Fatalf("authentication failure waited for cancellation instead of native rejection: %v", err)
			}
			if attached != 1 {
				t.Fatalf("attach count=%d", attached)
			}
			data, readErr := os.ReadFile(filepath.Join(job, "process.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			var process struct {
				ActionID string `json:"actionId"`
				Stopped  bool   `json:"stopped"`
				ExitCode int    `json:"exitCode"`
			}
			if json.Unmarshal(data, &process) != nil || !process.Stopped || process.ActionID != "action" {
				t.Fatalf("process=%s", data)
			}
			if mode == "exit-error" && process.ExitCode != 7 {
				t.Fatalf("exit=%d", process.ExitCode)
			}
			_, answerErr := os.Stat(filepath.Join(job, "answer.json"))
			if (answerErr == nil) != (mode == "attach") {
				t.Fatalf("answer err=%v", answerErr)
			}
			if _, err := runWorkBuddyProcess(ctx, cmd, job, "action", "native-session", "fixture", nil); err == nil {
				t.Fatal("repeated invocation overwrote existing journal")
			}
		})
	}
}
