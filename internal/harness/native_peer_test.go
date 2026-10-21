package harness

// Scripted fake-host peers for process-spawning transport tests (helper-
// process pattern: the test binary re-execs itself behind a small launcher
// wrapper, so nothing depends on a real native host being installed). The
// machinery is platform-neutral; each platform's peerExecutable writes the
// launcher its exec can start (a #!/bin/sh wrapper on POSIX, a .cmd on
// Windows).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// peerStep is one scripted action of the fake host peer. A step with an empty
// On fires at startup; otherwise it fires (once) when a received line contains
// the On substring. In Send, the token "@ID@" (bare, unquoted) is replaced by
// the received frame's id, so one script answers whatever id the client used.
type peerStep struct {
	Semantics   any        `json:"semantics,omitempty"`
	SemanticJob string     `json:"semanticJob,omitempty"`
	Params      any        `json:"params,omitempty"`
	On          string     `json:"on,omitempty"`
	Send        string     `json:"send,omitempty"`
	Garbage     string     `json:"garbage,omitempty"`
	Stderr      string     `json:"stderr,omitempty"`
	SleepSecs   float64    `json:"sleep,omitempty"`
	Close       bool       `json:"close,omitempty"`
	IgnoreEOF   bool       `json:"ignore_eof,omitempty"`
	ExitCode    int        `json:"exitcode,omitempty"`
	Files       []peerFile `json:"files,omitempty"`
}

// peerFile is a file the peer writes into the evidence directory when its step
// fires (the fake host's native evidence).
type peerFile struct {
	Path string `json:"path"`
	Body string `json:"body"`
}

type peerScript struct {
	Args        []string          `json:"args,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Workspace   string            `json:"workspace,omitempty"`
	Steps       []peerStep        `json:"steps"`
	Echo        string            `json:"echo"`
	// PeerPID names a file the peer writes its own process id into at
	// startup, so a cancellation test can assert the child really died.
	PeerPID string `json:"peerPid,omitempty"`
}

// TestScriptedPeer is the helper-process entry: when
// FORMAL_GATES_NATIVE_PEER_SCRIPT is set the test binary becomes the fake host
// peer instead of running the suite.
func TestScriptedPeer(t *testing.T) {
	scriptPath := os.Getenv("FORMAL_GATES_NATIVE_PEER_SCRIPT")
	if scriptPath == "" {
		return
	}
	os.Exit(runScriptedPeer(scriptPath))
}

func runScriptedPeer(scriptPath string) int {
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		return 2
	}
	var script peerScript
	if err := json.Unmarshal(body, &script); err != nil {
		return 2
	}
	if script.PeerPID != "" {
		if err := os.WriteFile(script.PeerPID, []byte(fmt.Sprint(os.Getpid())), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "peer cannot write pid file: %v\n", err)
		}
	}
	if script.Args != nil {
		args := os.Args
		for len(args) > 0 && args[0] != "--" {
			args = args[1:]
		}
		if len(args) > 0 {
			args = args[1:]
		}
		if !reflect.DeepEqual(args, script.Args) {
			fmt.Fprintf(os.Stderr, "native argv differs: %q != %q\n", args, script.Args)
			return 91
		}
	}
	for key, expected := range script.Environment {
		if os.Getenv(key) != expected {
			fmt.Fprintf(os.Stderr, "native env %s differs\n", key)
			return 91
		}
	}
	if script.Workspace != "" {
		cwd, err := os.Getwd()
		if err != nil || resolveNativePath(cwd) != resolveNativePath(script.Workspace) {
			fmt.Fprintf(os.Stderr, "native cwd differs: %s != %s\n", cwd, script.Workspace)
			return 91
		}
	}
	var echo *os.File
	if script.Echo != "" {
		echo, _ = os.OpenFile(script.Echo, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}
	ignoreEOF := false
	for i := range script.Steps {
		step := script.Steps[i]
		if step.On != "" {
			continue
		}
		if step.IgnoreEOF {
			ignoreEOF = true
		}
		applyPeerStep(step, "")
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if line == "" && err != nil {
			break
		}
		if echo != nil {
			echo.WriteString(line)
		}
		for i := range script.Steps {
			step := script.Steps[i]
			if step.On == "" || !strings.Contains(line, step.On) {
				continue
			}
			if step.Params != nil {
				frame, decodeErr := decodeNativeJSON([]byte(line))
				if decodeErr != nil || !valuesEqual(getOf(frame, "params"), step.Params) {
					fmt.Fprintf(os.Stderr, "native params differ: %s != %s\n", line, textOf(step.Params))
					return 91
				}
			}
			script.Steps[i].On = "" // each step fires at most once
			if step.IgnoreEOF {
				ignoreEOF = true
			}
			applyPeerStep(step, line)
		}
		if err != nil {
			break
		}
	}
	if ignoreEOF {
		time.Sleep(60 * time.Second)
	}
	return 0
}

func applyPeerStep(step peerStep, line string) {
	if step.SleepSecs > 0 {
		time.Sleep(time.Duration(step.SleepSecs * float64(time.Second)))
	}
	if step.Stderr != "" {
		fmt.Fprint(os.Stderr, step.Stderr)
	}
	for _, file := range step.Files {
		if err := os.WriteFile(file.Path, []byte(file.Body), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "peer cannot write %s: %v\n", file.Path, err)
		}
	}
	if step.Semantics != nil {
		peerSubmitSemantics(step.SemanticJob, step.Semantics)
	}
	send := step.Send
	if strings.Contains(send, "@ID@") && line != "" {
		if message, err := decodeNativeJSON([]byte(line)); err == nil {
			if idJSON, err := CanonicalJSON(getOf(message, "id")); err == nil {
				send = strings.ReplaceAll(send, `"@ID@"`, string(idJSON))
			}
		}
	}
	if step.Garbage != "" {
		os.Stdout.WriteString(step.Garbage + "\n")
	}
	if send != "" {
		os.Stdout.WriteString(send + "\n")
	}
	if step.ExitCode != 0 {
		os.Exit(step.ExitCode)
	}
	if step.Close {
		os.Stdout.Close()
		os.Exit(0)
	}
}

// startPeer writes the script and returns the executable (a launcher that
// re-execs this test binary with only the helper test selected). The client's
// argv therefore keeps the real host shape: [executable, <provider args>].
func startPeer(t *testing.T, script peerScript) (executable, echoPath string) {
	t.Helper()
	dir := t.TempDir()
	echoPath = filepath.Join(dir, "echo.txt")
	script.Echo = echoPath
	body, err := json.Marshal(script)
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(dir, "peer.json")
	if err := os.WriteFile(scriptPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return peerExecutable(t, dir, scriptPath), echoPath
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func blockedText(t *testing.T, err error) string {
	t.Helper()
	blocked, ok := err.(*blockedError)
	if !ok {
		if unavailable, isUnavailable := err.(*sessionUnavailableError); isUnavailable {
			return unavailable.message
		}
		t.Fatalf("expected blockedError, got %T: %v", err, err)
	}
	return blocked.message
}
