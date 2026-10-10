//go:build !windows

package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
)

type canaryPromotionRecovery struct {
	directory  string
	intent     protocol.HostActionIntent
	parameters protocol.CandidateParameters
	final      string
	recovered  bool
}
type canaryPromotionCommand struct{ Native, Root, Directory, Provider string }
type canaryPromotionWindow struct {
	Argv           []string                     `json:"argv"`
	Directory      string                       `json:"directory"`
	ParentPID      int                          `json:"parentPid"`
	NativeExit     int                          `json:"nativeExit"`
	Output         []byte                       `json:"output"`
	ErrorOutput    []byte                       `json:"errorOutput"`
	ElapsedSeconds float64                      `json:"elapsedSeconds"`
	Intent         protocol.HostActionIntent    `json:"intent"`
	Parameters     protocol.CandidateParameters `json:"parameters"`
	Snapshot       json.RawMessage              `json:"snapshot"`
}

func installCanaryPromotionRecovery(c *engineCanary) *canaryPromotionRecovery {
	c.t.Helper()
	native, err := exec.LookPath(c.vcs.Name())
	if err != nil {
		c.fail("promotion native CLI: %v", err)
	}
	directory := filepath.Join(c.jobs, "promotion-recovery")
	if err := os.MkdirAll(directory, 0700); err != nil {
		c.fail("promotion evidence directory: %v", err)
	}
	spec := canaryPromotionCommand{Native: native, Root: c.project, Directory: directory, Provider: c.vcs.Name()}
	data, _ := json.Marshal(spec)
	path := filepath.Join(directory, "native-command.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		c.fail("promotion native specification: %v", err)
	}
	binary, _ := os.Executable()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	operation := "commit"
	if c.vcs.Name() == "p4" {
		operation = "submit"
	}
	// Other operations go straight to the native CLI. This private interception
	// cannot change the production runner or affect another installation.
	script := "#!/bin/sh\nfor arg in \"$@\"; do\nif [ \"$arg\" = " + quote(operation) + " ]; then\nexport CANARY_PROMOTION_COMMAND_SPEC=" + quote(path) + "\nexec " + quote(binary) + " -test.run='^TestCanaryPromotionNativeCommand$' -- \"$@\"\nfi\ndone\nexec " + quote(native) + " \"$@\"\n"
	launcher := filepath.Join(c.home, ".local", "bin", c.vcs.Name())
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		c.fail("private native launcher: %v", err)
	}
	return &canaryPromotionRecovery{directory: directory}
}

// This peer is used only by the private native launcher. It executes the real
// command with untouched argv/stdin and records the successful effect before
// killing its current installed CLI parent, which is still awaiting output.
func TestCanaryPromotionNativeCommand(t *testing.T) {
	path := os.Getenv("CANARY_PROMOTION_COMMAND_SPEC")
	if path == "" {
		t.Skip("private native command peer")
	}
	var spec canaryPromotionCommand
	if err := json.Unmarshal(readTestFileBytes(t, path), &spec); err != nil {
		t.Fatal(err)
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var window *canaryPromotionWindow
	if cwd == spec.Root {
		snapshot, readErr := os.ReadFile(filepath.Join(spec.Root, ".gates", "engine", "engine-canary", "state.json"))
		if readErr == nil {
			var envelope struct {
				Content protocol.State `json:"content"`
			}
			if err := json.Unmarshal(snapshot, &envelope); err != nil {
				t.Fatal(err)
			}
			intent, pending := envelope.Content.PromotionIntent()
			if pending {
				var params protocol.CandidateParameters
				raw, _ := json.Marshal(intent.Adapter.Params)
				if err := json.Unmarshal(raw, &params); err != nil {
					t.Fatal(err)
				}
				if params.Promotion != nil && params.Promotion.TargetRoot == spec.Root && slices.Contains(args, params.Promotion.Message) {
					window = &canaryPromotionWindow{Argv: append([]string{spec.Native}, args...), Directory: cwd, ParentPID: os.Getppid(), Intent: intent, Parameters: params, Snapshot: snapshot}
				}
			}
		}
	}
	command := exec.Command(spec.Native, args...)
	command.Stdin = os.Stdin
	var output, errorOutput bytes.Buffer
	command.Stdout = &output
	command.Stderr = &errorOutput
	started := time.Now()
	err = command.Run()
	code := 0
	if err != nil {
		code = 1
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		}
	}
	if window != nil {
		window.NativeExit = code
		window.Output = output.Bytes()
		window.ErrorOutput = errorOutput.Bytes()
		window.ElapsedSeconds = time.Since(started).Seconds()
		data, _ := json.Marshal(window)
		journal, openErr := os.OpenFile(filepath.Join(spec.Directory, "native-delivery-commands.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if openErr != nil {
			t.Fatal(openErr)
		}
		if _, err := journal.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
		if err := journal.Close(); err != nil {
			t.Fatal(err)
		}
		if code == 0 {
			file, createErr := os.OpenFile(filepath.Join(spec.Directory, "interruption.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if createErr == nil {
				if _, err := file.Write(data); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Kill(window.ParentPID, syscall.SIGKILL); err != nil {
					t.Fatal(err)
				}
			} else if !os.IsExist(createErr) {
				t.Fatal(createErr)
			}
		}
	}
	os.Stdout.Write(output.Bytes())
	os.Stderr.Write(errorOutput.Bytes())
	os.Exit(code)
}

func (r *canaryPromotionRecovery) recover(c *engineCanary, args []string, commandError error) bool {
	c.t.Helper()
	var window canaryPromotionWindow
	data, err := os.ReadFile(filepath.Join(r.directory, "interruption.json"))
	if err != nil {
		return false
	}
	if r.recovered {
		c.fail("installed command failed after promotion recovery: %v", commandError)
	}
	exit, ok := commandError.(*exec.ExitError)
	if !ok || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL || len(args) < 2 || args[0] != "workflow" || args[1] != "drive" && args[1] != "submit" {
		c.fail("unexpected promotion interruption command/error: %v %v", args, commandError)
	}
	if err := json.Unmarshal(data, &window); err != nil {
		c.fail("native interruption evidence: %v", err)
	}
	snapshot, err := os.ReadFile(filepath.Join(c.project, ".gates", "engine", "engine-canary", "state.json"))
	if err != nil {
		c.fail("original promotion snapshot: %v", err)
	}
	var envelope struct {
		Content protocol.State `json:"content"`
	}
	if err := json.Unmarshal(snapshot, &envelope); err != nil {
		c.fail("promotion snapshot decode: %v", err)
	}
	intent, pending := envelope.Content.PromotionIntent()
	if window.NativeExit != 0 || !pending || !canaryValuesEqual(intent, window.Intent) || window.Parameters.Promotion == nil {
		c.fail("interruption lost its successful native effect/original intent")
	}
	if _, accepted := envelope.Content.HostActionReceipts[intent.ActionID]; accepted || envelope.Content.CandidatePromotion != nil {
		c.fail("interruption happened after receipt acceptance")
	}
	params := window.Parameters.Promotion
	observed, found, err := c.vcs.ObservedCommit(params.TargetStart.Head, params.Message)
	if err != nil || !found {
		c.fail("native delivery effect was not independently observed: %s %t %v", observed, found, err)
	}
	actual, err := c.vcs.TreeEntries(observed)
	if err != nil {
		c.fail("observed delivery tree: %v", err)
	}
	source, err := c.vcs.TreeEntries(params.Source)
	if err != nil || !canaryValuesEqual(actual, source) {
		c.fail("interrupted native effect differs from exact source tree: %v", err)
	}
	proof := map[string]any{"intent": intent, "parameters": window.Parameters, "nativeFinal": observed, "nativeEntries": actual, "receiptAbsent": true, "argv": args, "error": commandError.Error()}
	body, _ := json.MarshalIndent(proof, "", "  ")
	if err := os.WriteFile(filepath.Join(r.directory, "before-reentry.json"), body, 0600); err != nil {
		c.fail("reentry evidence: %v", err)
	}
	r.intent, r.parameters, r.final, r.recovered = intent, window.Parameters, observed, true
	c.t.Logf("actual installed promotion interruption: provider=%s intent=%s final=%s original receipt absent; recovering through same public drive", c.vcs.Name(), intent.ActionID, observed)
	return true
}

func (r *canaryPromotionRecovery) assertTerminal(c *engineCanary) {
	c.t.Helper()
	if !r.recovered || c.run.Termination == nil || c.run.Termination.CandidatePromotion == nil {
		c.fail("canary did not exercise actual promotion interruption/reentry")
	}
	proof := c.run.Termination.CandidatePromotion
	if proof.OperationID != r.intent.ActionID || proof.Source != r.parameters.Promotion.Source || proof.Final != r.final || proof.SourceDigest != r.parameters.Promotion.SourceDigest {
		c.fail("reentry replaced the original native promotion identity")
	}
	receipt, ok := c.run.Termination.HostActionReceipts[r.intent.ActionID]
	if !ok || receipt.Status != protocol.HostActionStatusReconciled && receipt.Status != protocol.HostActionStatusExecuted || receipt.Digest == "" || receipt.PayloadDigest != r.intent.PayloadDigest {
		c.fail("reentry lacks its original native receipt: %+v", receipt)
	}
	commands := strings.Split(strings.TrimSpace(string(readTestFileBytes(c.t, filepath.Join(r.directory, "native-delivery-commands.jsonl")))), "\n")
	if len(commands) != 1 {
		c.fail("reentry executed native delivery %d times", len(commands))
	}
	result := map[string]any{"promotion": proof, "receipt": receipt, "nativeDeliveryCommands": len(commands), "resourcesCleaned": c.run.Termination.ResourcesCleaned(), "status": c.run.Status}
	body, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(filepath.Join(r.directory, "recovered-terminal.json"), body, 0600); err != nil {
		c.fail("terminal recovery evidence: %v", err)
	}
	c.t.Logf("installed promotion recovery confirmed: provider=%s original intent=%s native delivery commands=1", c.vcs.Name(), r.intent.ActionID)
}
