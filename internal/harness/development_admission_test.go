//go:build !windows

package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// Writable completion feedback uses actual issued-workspace changes before
// admission, so the worker can remove a verification artifact and answer again.
func TestCanaryDevelopmentRejectsUndeclaredChangesBeforeAdmission(t *testing.T) {
	for _, role := range []string{"development", "repair", "repair-member"} {
		t.Run(role, func(t *testing.T) {
			var sample struct {
				Answer map[string]any
				Input  protocol.ValidationInput
			}
			if err := json.Unmarshal(readTestFileBytes(t, filepath.Join("testdata", "development_declared_changes.json")), &sample); err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			workspace := filepath.Join(home, ".formal-gates-resources", "issued")
			if err := os.MkdirAll(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = workspace
				cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Workspace Fixture", "GIT_AUTHOR_EMAIL=workspace@example.invalid", "GIT_COMMITTER_NAME=Workspace Fixture", "GIT_COMMITTER_EMAIL=workspace@example.invalid")
				body, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, body)
				}
				return strings.TrimSpace(string(body))
			}
			write := func(path, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(workspace, path), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			git("init", "-q")
			write("frob.go", "package main\nfunc main() {}\n")
			git("add", "frob.go")
			git("commit", "-qm", "Workspace input")
			base := git("rev-parse", "HEAD")
			sample.Input.Workspace.Path = workspace
			sample.Input.Workspace.Source, sample.Input.Workspace.Identity = base, base
			sample.Input.Binding.Candidate = base
			if role != "repair" {
				sample.Input.Development = &protocol.DevelopmentInput{}
			}
			if role == "development" {
				sample.Input.Repair = nil
			}
			write("frob.go", "package main\nfunc main() { println(\"verified\") }\n")
			write("frob", "temporary verification executable")
			before := git("status", "--porcelain=v1", "--untracked-files=all") + git("write-tree") + git("rev-parse", "HEAD")
			answerBytes, err := json.Marshal(sample.Answer)
			if err != nil {
				t.Fatal(err)
			}
			approvalRead, approvalWrite := io.Pipe()
			defer approvalRead.Close()
			defer approvalWrite.Close()
			c := &engineCanary{t: t, home: home, project: t.TempDir(), jobs: t.TempDir(), worker: true, deadline: time.Now().Add(time.Minute), vcs: vcs.New(workspace, nil), approvalInput: bufio.NewReader(approvalRead), approvalOutput: canaryLocalApproval{approvalWrite}}
			calls := 0
			c.execute = func(value any, directory string, writeSource bool) (map[string]any, error) {
				request := value.(map[string]any)
				calls++
				if request["workspace"] != workspace || request["read_only"] != false || writeSource {
					t.Fatal("completion call changed issued protocol inputs")
				}
				if calls == 1 {
					if strings.Contains(request["prompt"].(string), "Your previous answer was refused") {
						t.Fatal("initial completion inherited refusal")
					}
				} else if calls == 2 {
					if !strings.Contains(request["prompt"].(string), "Your previous answer was refused: candidate contains undeclared change: frob") {
						t.Fatal("worker was not told the actual undeclared change")
					}
					if now := git("status", "--porcelain=v1", "--untracked-files=all") + git("write-tree") + git("rev-parse", "HEAD"); now != before {
						t.Fatal("admission mutated workspace before worker correction")
					}
					if err := os.Remove(filepath.Join(workspace, "frob")); err != nil {
						t.Fatal(err)
					}
				} else {
					t.Fatalf("unexpected completion call %d", calls)
				}
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"answer.json", "answer.raw.txt"} {
					if err := os.WriteFile(filepath.Join(directory, name), answerBytes, 0600); err != nil {
						t.Fatal(err)
					}
				}
				return map[string]any{"identity": fmt.Sprintf("worker-%d", calls)}, nil
			}
			var result protocol.ValidationResult
			if role == "development" {
				result = c.developmentResult("completion", sample.Input)
			} else {
				result = c.repairResult("completion", sample.Input)
			}
			if calls != 2 || !slices.Equal(result.Development.Paths, []string{"frob.go"}) || result.Binding != sample.Input.Binding {
				t.Fatalf("completion admitted without worker correction: calls=%d result=%+v", calls, result)
			}
			for _, attempt := range []int{1, 2} {
				for _, name := range []string{"answer.json", "answer.raw.txt"} {
					actual := readTestFileBytes(t, filepath.Join(c.jobDirectory("completion"), fmt.Sprintf("attempt-%d", attempt), name))
					if !bytes.Equal(actual, answerBytes) {
						t.Fatal("consumer rewrote native answer evidence")
					}
				}
			}
			if status := git("status", "--porcelain=v1", "--untracked-files=all"); status != "M frob.go" {
				t.Fatalf("worker correction changed declared content: %s", status)
			}
		})
	}
}

// Completion declarations retain exact path normalization and skip the
// immutable final self-run and planning-only boundaries.
func TestCanaryDeclaredDevelopmentChangesPreserveWorkspaceBoundaries(t *testing.T) {
	workspace := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Workspace Fixture", "GIT_AUTHOR_EMAIL=workspace@example.invalid", "GIT_COMMITTER_NAME=Workspace Fixture", "GIT_COMMITTER_EMAIL=workspace@example.invalid")
		body, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, body)
		}
		return strings.TrimSpace(string(body))
	}
	write := func(path, body string) {
		t.Helper()
		target := filepath.Join(workspace, path)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("command.go", "package main\n")
	write("removed.go", "package main\n")
	write(".gates/engine/run/input.json", "tracked runtime input")
	git("add", ".")
	git("commit", "-qm", "Workspace inputs")
	base := git("rev-parse", "HEAD")
	write("command.go", "package main\nfunc command() {}\n")
	if err := os.Remove(filepath.Join(workspace, "removed.go")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".gates/engine/run/state.json", ".gates/tmp/run/state.json", ".gates/results/result.json"} {
		write(path, "runtime evidence")
	}
	alias := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(workspace, alias); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(alias)
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.ValidationInput{Binding: protocol.WorkBinding{WorkspaceID: "issued"}, Workspace: &protocol.NativeResource{ID: "issued", Path: alias, Source: base, Identity: base, Status: "READY"}}
	c := &engineCanary{t: t, project: t.TempDir(), vcs: vcs.New(workspace, nil)}
	for _, paths := range [][]string{{"command.go", "removed.go"}, {filepath.Join(alias, "command.go"), filepath.Join(alias, "removed.go")}, {filepath.Join(resolved, "command.go"), filepath.Join(resolved, "removed.go")}} {
		answer := canaryDevelopmentAnswer{Paths: paths}
		before, _ := json.Marshal(answer)
		if err := c.declaredDevelopmentChanges(input, &answer); err != nil {
			t.Fatal(err)
		}
		after, _ := json.Marshal(answer)
		if !bytes.Equal(before, after) {
			t.Fatal("completion changed answer paths")
		}
	}
	write(".gates/engine/run/input.json", "changed tracked runtime input")
	answer := canaryDevelopmentAnswer{Paths: []string{"command.go", "removed.go"}}
	if err := c.declaredDevelopmentChanges(input, &answer); err == nil || err.Error() != "candidate contains undeclared change: .gates/engine/run/input.json" {
		t.Fatalf("tracked runtime mutation escaped declaration: %v", err)
	}
	write("temporary-output", "verification artifact")
	if err := c.declaredDevelopmentChanges(protocol.ValidationInput{}, &canaryDevelopmentAnswer{Plan: &protocol.GranularityReview{}}); err != nil {
		t.Fatalf("pure plan gained workspace admission: %v", err)
	}
	input.Repair = &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{}}
	if err := c.declaredDevelopmentChanges(input, &answer); err != nil {
		t.Fatalf("final self-run gained freeze admission: %v", err)
	}
	input.Repair = nil
	answer.Paths = []string{filepath.Join(c.project, "command.go")}
	if err := c.declaredDevelopmentChanges(input, &answer); err == nil || !strings.Contains(err.Error(), "not inside the workspace") {
		t.Fatalf("other workspace accepted: %v", err)
	}
}
