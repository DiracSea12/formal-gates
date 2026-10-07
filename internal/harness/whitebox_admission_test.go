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

// The native whitebox author receives the freeze predicate's actual refusal
// and cleans its own build output; admission preserves its original answer.
func TestCanaryWhiteboxAuthorCleansUndeclaredBuildOutputs(t *testing.T) {
	for _, boundary := range []string{"authored-only", "default-output", "named-output", "reissued-input"} {
		t.Run(boundary, func(t *testing.T) {
			scenario := newZcodeScenario(t, nil, nil)
			home := resolveNativePath(t.TempDir())
			writeCanaryHostFixture(t, home, "peer-provider")
			workspace := filepath.Join(home, ".formal-gates-resources", "whitebox")
			if err := os.MkdirAll(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			command := func(name string, args ...string) string {
				t.Helper()
				cmd := exec.Command(name, args...)
				cmd.Dir = workspace
				cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Workspace Fixture", "GIT_AUTHOR_EMAIL=workspace@example.invalid", "GIT_COMMITTER_NAME=Workspace Fixture", "GIT_COMMITTER_EMAIL=workspace@example.invalid")
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s %v: %v %s", name, args, err, output)
				}
				return strings.TrimSpace(string(output))
			}
			write := func(path, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(workspace, path), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			command("git", "init", "-q")
			write("go.mod", "module frob\n\ngo 1.22\n")
			write("frob.go", "package main\nfunc main() {}\n")
			command("git", "add", ".")
			command("git", "commit", "-qm", "Command input")
			base := command("git", "rev-parse", "HEAD")
			input := protocol.ValidationInput{Handler: "engine.qa.design", Binding: protocol.WorkBinding{WorkspaceID: "whitebox", Candidate: base}, Workspace: &protocol.NativeResource{ID: "whitebox", Path: workspace, Source: base, Identity: base, Status: "READY"}, Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}}, SourcePoints: []protocol.SourcePoint{{SourceID: "requirement", PointID: "command"}}}
			if boundary == "reissued-input" {
				path := filepath.Join(t.TempDir(), "input.json")
				if err := writeAtomicJSON(path, input); err != nil {
					t.Fatal(err)
				}
				input = protocol.ValidationInput{}
				if err := json.Unmarshal(readTestFileBytes(t, path), &input); err != nil {
					t.Fatal(err)
				}
			}
			approvalRead, approvalWrite := io.Pipe()
			defer approvalRead.Close()
			defer approvalWrite.Close()
			c := &engineCanary{t: t, nativeHost: canaryProvider, binding: objectOrEmpty(scenario.request["binding"]), home: home, project: t.TempDir(), jobs: t.TempDir(), worker: true, deadline: time.Now().Add(time.Minute), vcs: vcs.New(workspace, nil), caseTests: map[string]string{}, approvalInput: bufio.NewReader(approvalRead), approvalOutput: canaryLocalApproval{approvalWrite}}
			environment := map[string]any{}
			for key, value := range c.hostEnvironment() {
				environment[key] = value
			}
			environment["FORMAL_GATES_HARNESS_ROOT"] = t.TempDir()
			c.binding["environment"] = environment
			executor := &canaryLocalExecutor{t: t, canary: c}
			outputName := "frob"
			if boundary == "named-output" {
				outputName = "command-output"
			}
			answer := map[string]any{"paths": []string{filepath.Join(workspace, "command_test.go")}, "summary": "Authored command acceptance test", "build": []string{"go build exit=0"}, "tests": []string{"go test ./... exit=0"}, "cases": []any{map[string]any{"caseId": "empty-command", "pointId": "command", "scenario": "BOUNDARY", "testRef": "TestCommand", "oracle": "The command returns normally."}}}
			body, err := json.Marshal(answer)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(body, &answer); err != nil {
				t.Fatal(err)
			}
			answerBytes := []byte(mustCanonical(t, answer))
			calls := 0
			var before string
			c.execute = func(value any, directory string, writeSource bool) (map[string]any, error) {
				request := value.(map[string]any)
				calls++
				if request["workspace"] != workspace || request["read_only"] != false || writeSource {
					t.Fatal("whitebox call changed issued native inputs")
				}
				var material canaryMaterials
				if err := json.Unmarshal(readTestFileBytes(t, filepath.Join(c.jobDirectory("design"), "materials.json")), &material); err != nil || !canaryValuesEqual(material.Input, &input) {
					t.Fatalf("author did not receive the exact issued input: %v", err)
				}
				if calls == 1 {
					if strings.Contains(textOf(request["prompt"]), "Your previous answer was refused") {
						t.Fatal("initial design inherited refusal")
					}
					write("command_test.go", "package main\nimport \"testing\"\nfunc TestCommand(t *testing.T) { main() }\n")
					buildArgs := []string{"build", "./..."}
					if boundary == "authored-only" {
						buildArgs = []string{"build", "-o", filepath.Join(t.TempDir(), outputName), "./..."}
					} else if boundary == "named-output" {
						buildArgs = []string{"build", "-o", outputName, "./..."}
					}
					command("go", buildArgs...)
					command("go", "test", "./...")
					before = command("git", "status", "--porcelain=v1", "--untracked-files=all") + command("git", "write-tree") + command("git", "rev-parse", "HEAD")
				} else if calls == 2 && boundary != "authored-only" {
					refusal := "Your previous answer was refused: candidate contains undeclared change: " + outputName
					if !strings.Contains(textOf(request["prompt"]), refusal) {
						t.Fatalf("author did not receive original refusal: %s", request["prompt"])
					}
					if now := command("git", "status", "--porcelain=v1", "--untracked-files=all") + command("git", "write-tree") + command("git", "rev-parse", "HEAD"); now != before {
						t.Fatal("consumer mutated source/index/HEAD before author correction")
					}
					if info, err := os.Stat(filepath.Join(workspace, outputName)); err != nil || info.Size() < 1024 {
						t.Fatalf("real build output was absent before author cleanup: %v", err)
					}
					if err := os.Remove(filepath.Join(workspace, outputName)); err != nil {
						t.Fatal(err)
					}
				} else {
					t.Fatalf("unexpected author attempt %d", calls)
				}
				if err := os.MkdirAll(directory, 0700); err != nil {
					return nil, err
				}
				if err := executor.prepareNativePeer(request, directory, answer); err != nil {
					return nil, err
				}
				source, err := runNativeJob(directory)
				if err != nil {
					return nil, err
				}
				implementation, err := nativeImplementationFor(c.nativeName())
				if err != nil {
					return nil, err
				}
				if err := implementation.verifySource(directory, source); err != nil {
					return nil, err
				}
				return source, nil
			}
			result := c.designResult("design", input)
			wantCalls := 2
			if boundary == "authored-only" {
				wantCalls = 1
			}
			if calls != wantCalls || result.Binding != input.Binding || !slices.Equal(result.Development.Paths, []string{"command_test.go"}) || result.Development.Summary != answer["summary"] || !canaryValuesEqual(result.Development.Build, answer["build"]) || !canaryValuesEqual(result.Development.Tests, answer["tests"]) || len(result.Manifest.Cases) != 1 || result.Manifest.Cases[0].TestRef != "TestCommand" {
				t.Fatalf("author evidence changed or bypassed correction: calls=%d result=%+v", calls, result)
			}
			for attempt := 1; attempt <= calls; attempt++ {
				for _, name := range []string{"answer.json", "answer.raw.txt"} {
					actual := bytes.TrimSpace(readTestFileBytes(t, filepath.Join(c.jobDirectory("design"), fmt.Sprintf("attempt-%d", attempt), name)))
					if !bytes.Equal(actual, answerBytes) {
						t.Fatalf("consumer changed original native evidence: %s", name)
					}
				}
			}
			if status := command("git", "status", "--porcelain=v1", "--untracked-files=all"); status != "?? command_test.go" || command("git", "rev-parse", "HEAD") != base || command("git", "diff", "--cached", "--name-only") != "" {
				t.Fatalf("completion froze or changed declared content: %s", status)
			}
			if _, _, err := c.vcs.ValidateDeclaredChanges(base, result.Development.Paths); err != nil {
				t.Fatalf("corrected native answer still fails candidate freeze predicate: %v", err)
			}
			t.Logf("actual_model_calls=0 native_author_attempts=%d build_and_test_exit=0 final_paths=%v", calls, result.Development.Paths)
		})
	}
}
