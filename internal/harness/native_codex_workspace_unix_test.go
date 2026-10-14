//go:build !windows

package harness

// The original native thread retains its role contract while issued material
// grants workspace writes only after the declared EDIT calls are prepared.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestCodexDevelopmentContinuationRetainsWorkspaceContract(t *testing.T) {
	for _, sample := range []struct {
		name string
		exit int
	}{{"successful-check", 0}, {"failed-check", 7}} {
		t.Run(sample.name, func(t *testing.T) {
			s := newCodexScenario(t)
			s.directory = filepath.Join(filepath.Dir(s.directory), "action", "preparation")
			git := func(args ...string) string {
				t.Helper()
				command := exec.Command("git", args...)
				command.Dir = s.workspace
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, output)
				}
				return strings.TrimSpace(string(output))
			}
			git("init", "-q")
			git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "base")
			if err := os.MkdirAll(filepath.Join(s.workspace, ".gates", "tmp"), 0700); err != nil {
				t.Fatal(err)
			}
			input := protocol.WorkerInput{
				InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, RunID: "normal-run", Provider: "codex", VCS: "git", Handler: "engine.development.batch.v6",
				Pending:             protocol.PendingAction{ActionID: "original-action", AttemptID: "original-attempt"},
				RequirementDocument: requirement.FrozenRequirementProjection{Content: "Create app.go and run the declared check after every edit"},
				Validation: &protocol.ValidationInput{
					Workspace: &protocol.NativeResource{ID: "original-workspace", Path: s.workspace, Source: git("rev-parse", "HEAD")},
					Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "confirmed-batch", Checks: []protocol.DevelopmentCheck{
						{ID: "edit", Purpose: protocol.CheckEdit, Goal: "Run the declared source check after every edit"},
						{ID: "build", Purpose: protocol.CheckFullBuild, Command: "true"},
						{ID: "tests", Purpose: protocol.CheckFullTests, Command: "true"},
					}, DoD: []protocol.DevelopmentCriterion{{ID: "delivery", Description: "The confirmed app.go exists"}}}},
				},
			}
			catalog := prompts.PromptCatalog{Base: "Independent responsibility", Actions: []prompts.PromptDefinition{{ID: "development-worker"}}}
			execute := func(answer map[string]any) (map[string]any, worker.Material) {
				t.Helper()
				if err := os.MkdirAll(s.directory, 0700); err != nil {
					t.Fatal(err)
				}
				material, err := worker.Render(input, catalog)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(s.directory, "material.json")
				if err := writeAtomicJSON(path, material); err != nil {
					t.Fatal(err)
				}
				s.request["read_only"], s.request["schema"], s.request["prompt"] = material.ReadOnly, material.ResultSchema, material.Prompt
				s.request["engine"] = map[string]any{"workerMaterial": map[string]any{"path": path, "actionId": input.Pending.ActionID, "attemptId": input.Pending.AttemptID, "readOnly": material.ReadOnly}}
				s.peer.Answer = answer
				s.save(t)
				if err := writeAtomicJSON(filepath.Join(s.directory, "process.json"), map[string]any{"pid": os.Getpid()}); err != nil {
					t.Fatal(err)
				}
				original := codexPlain(t, s.request)
				service := ExecutionServices{Admit: func(directory string, request map[string]any) (func(), error) {
					if directory != s.directory || !valuesEqual(request, original) {
						return nil, fmt.Errorf("execution differs from its original issued request")
					}
					return func() {}, nil
				}}
				source, err := codexExecute(s.request, s.directory, false, service)
				if err != nil {
					stderr, _ := os.ReadFile(filepath.Join(s.directory, "host.stderr"))
					t.Fatalf("native call: %v %s", err, stderr)
				}
				if err := codexVerifySource(s.directory, source); err != nil {
					t.Fatal(err)
				}
				return source, material
			}
			mutation := map[string]any{"action": "create", "name": "app.go", "content": "package app\n"}
			s.peer.Reverse = []any{map[string]any{"method": "item/tool/call", "params": map[string]any{"callId": "create", "tool": nativeWorkspaceTool, "arguments": mutation}}}
			s.peer.Replies = []any{map[string]any{"success": false, "contentItems": []any{map[string]any{"type": "inputText", "text": "issued action is read-only"}}}}
			command := fmt.Sprintf("printf checked; printf diagnostic >&2; printf x >> \"$FORMAL_GATES_WORKSPACE/.gates/tmp/check-count\"; exit %d", sample.exit)
			first, initial := execute(map[string]any{"summary": "Prepared the declared source check", "remainingWork": "Create the confirmed app.go", "commands": []string{command}})
			if !initial.ReadOnly {
				t.Fatal("unprepared EDIT call allowed workspace writes")
			}
			for _, name := range []string{"app.go", ".gates/tmp/check-count"} {
				if _, err := os.Stat(filepath.Join(s.workspace, name)); !os.IsNotExist(err) {
					t.Fatal("preparation performed a write or check", name, err)
				}
			}
			toolPath := filepath.Join(s.config, textOf(first["identity"])+".tools.json")
			declaration, err := os.ReadFile(toolPath)
			if err != nil {
				t.Fatal(err)
			}
			var tools []any
			if err := json.Unmarshal(declaration, &tools); err != nil || len(tools) != 2 || getOf(tools[0], "name") != nativeWorkspaceTool {
				t.Fatalf("original workspace declaration: %s %v", declaration, err)
			}
			var actions []string
			for _, branch := range listOf(getOf(getOf(tools[0], "inputSchema"), "oneOf")) {
				actions = append(actions, textOf(getOf(getOf(getOf(branch, "properties"), "action"), "const")))
			}
			if !slices.Contains(actions, "create") || !slices.Contains(actions, "replace") {
				t.Fatalf("persistent development contract cannot edit after preparation: %v", actions)
			}
			evidence, err := NativeWorkerResultEvidence("codex", s.directory, first)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := worker.DecodeResultWithPaths(input, "prepared", evidence.Answer, evidence.Digest, []string{})
			if err != nil || prepared.Result.Validation.Status != "CONTINUE" || prepared.Result.Validation.Development.Completion != nil {
				t.Fatal("preparation did not retain the original unfinished development duty", err)
			}
			input.Validation.Development.Result = &protocol.WorkerResult{ActionID: prepared.Result.ActionID, Digest: evidence.Digest, Outcome: prepared.Result.Outcome, Validation: prepared.Result.Validation}
			s.request["identity"] = first["identity"]
			s.directory = filepath.Join(filepath.Dir(s.directory), "continued")
			result, err := json.Marshal(map[string]any{"written": true, "checks": []any{map[string]any{"check": 1, "exitCode": sample.exit, "error": "", "stdout": "checked", "stderr": "diagnostic"}}})
			if err != nil {
				t.Fatal(err)
			}
			s.peer.Replies = []any{map[string]any{"success": true, "contentItems": []any{map[string]any{"type": "inputText", "text": string(result)}}}}
			second, current := execute(map[string]any{"summary": "Created the confirmed app.go", "remainingWork": "Complete the confirmed development duty"})
			if current.ReadOnly || current.Input.Pending != initial.Input.Pending || second["identity"] != first["identity"] || valuesEqual(second["native_turn"], first["native_turn"]) {
				t.Fatal("prepared development did not continue its original writable action and thread")
			}
			retained, err := os.ReadFile(toolPath)
			if err != nil || !bytes.Equal(declaration, retained) {
				t.Fatal("resume replaced the original tool contract", err)
			}
			for name, expected := range map[string]string{"app.go": "package app\n", ".gates/tmp/check-count": "x"} {
				body, err := os.ReadFile(filepath.Join(s.workspace, name))
				if err != nil || string(body) != expected {
					t.Fatalf("original mutation/check %s = %q: %v", name, body, err)
				}
			}
			context, err := readJSONFile(filepath.Join(s.directory, nativeToolContextFile))
			if err != nil {
				t.Fatal(err)
			}
			edits := 0
			for number, fact := range listOf(getOf(context, "facts")) {
				if getOf(fact, "kind") != "edit" {
					continue
				}
				edits++
				directory := filepath.Join(s.directory, "edit-checks", fmt.Sprint(number+1), "1")
				request, err := readJSONFile(filepath.Join(directory, "request.json"))
				if err != nil || getOf(getOf(request, "check"), "command") != command || getOf(getOf(request, "check"), "goal") != nil {
					t.Fatal("edit did not execute the accepted original command", err)
				}
				receipt, err := editCheckReceipt(directory, objectOrEmpty(request))
				if err != nil || !valuesEqual(getOf(receipt, "exit_code"), sample.exit) {
					t.Fatal("original edit check receipt changed its actual exit", err, receipt)
				}
				for name, expected := range map[string]string{"stdout.log": "checked", "stderr.log": "diagnostic"} {
					body, err := os.ReadFile(filepath.Join(directory, name))
					if err != nil || string(body) != expected {
						t.Fatalf("actual check %s = %q: %v", name, body, err)
					}
				}
			}
			if edits != 1 {
				t.Fatal("original thread did not perform exactly one authorized mutation", edits)
			}
			rpc, _ := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
			recovered, err := codexReconcile(s.directory, map[string]any{"exit_code": 0})
			if err != nil || !valuesEqual(recovered, second) {
				t.Fatal("recovery changed the original source", err)
			}
			repeated, _ := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
			count, _ := os.ReadFile(filepath.Join(s.workspace, ".gates/tmp/check-count"))
			if !bytes.Equal(rpc, repeated) || string(count) != "x" {
				t.Fatal("recovery replayed a native turn or the original edit check")
			}
		})
	}
}

func TestCodexReviewerKeepsReadOnlyWorkspaceContract(t *testing.T) {
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Provider: "codex", Handler: "engine.review.worker.v3", Pending: protocol.PendingAction{ActionID: "review", AttemptID: "review-attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Review the confirmed requirement"}, Review: &protocol.WorkerReviewInput{Action: review.ProductReview}}
	material, err := worker.Render(input, prompts.PromptCatalog{Base: "Independent responsibility", Actions: []prompts.PromptDefinition{{ID: "product-review"}}})
	if err != nil || !material.ReadOnly {
		t.Fatal("reviewer material is not read-only", err)
	}
	tools, err := codexToolSchemas(&nativeWorkspaceTools{readOnly: material.ReadOnly, material: &material})
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range listOf(getOf(getOf(tools[0], "inputSchema"), "oneOf")) {
		action := textOf(getOf(getOf(getOf(branch, "properties"), "action"), "const"))
		if !slices.Contains([]string{"list", "read", "search", "requirement", "comparison"}, action) {
			t.Fatal("read-only reviewer received a workspace mutation", action)
		}
	}
}
