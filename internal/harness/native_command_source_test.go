package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/worker"
)

func TestNativeRequirementReadOwnsOriginalQAValues(t *testing.T) {
	for _, entrypoint := range []string{"design", "commands", "revision"} {
		t.Run(entrypoint, func(t *testing.T) {
			directory, workspace := nativeToolFixture(t)
			command := "printf '<原命令 \"quoted\">\\n'; cat\n"
			stdin := " 中文 \"input\"\r\n\r\n"
			argument := " two words \"quoted\"\r\n"
			original := "Confirmed values:\n" + command + stdin + argument + "/usr/bin/printf"
			input := protocol.WorkerInput{RunID: "issued-run", Handler: "engine.qa.design.v4", Provider: "dsh", Pending: protocol.PendingAction{ActionID: "issued-action", AttemptID: "original-attempt"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}}}}
			input.RequirementDocument.Path = "requirements.md"
			input.RequirementDocument.Content = original
			input.RequirementDocument.ContentDigest = "confirmed-original"
			if entrypoint == "commands" {
				input.Handler = "engine.qa.execution.v3"
				input.Validation.CommandCases = []string{"approved-case"}
			} else if entrypoint == "revision" {
				input.Validation.Manifest.Cases = []coverage.AcceptanceCase{{CaseID: "approved-case", Scenario: "SUCCESS", Oracle: "approved output", Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: "other invocation"}}}}
				input.Validation.AffectedCases = []string{"approved-case"}
			}
			schema, err := worker.ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			materialPath := filepath.Join(directory, "material.json")
			if err := writeAtomicJSON(materialPath, worker.Material{Input: input, ResultSchema: schema}); err != nil {
				t.Fatal(err)
			}
			request := map[string]any{"provider": "dsh", "workspace": workspace, "read_only": false, "schema": schema, "engine": map[string]any{"workerMaterial": map[string]any{"path": materialPath, "actionId": input.Pending.ActionID}}}
			if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspace, "requirements.md"), []byte("unconfirmed mutable bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			call := func(name string, arguments any) map[string]any {
				t.Helper()
				wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
				if err != nil {
					t.Fatal(err)
				}
				var out, errout bytes.Buffer
				if code := NativeTools(directory, bytes.NewReader(wire), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
					t.Fatalf("original MCP: %d %s", code, &errout)
				}
				reply, err := decodeNativeJSON(out.Bytes())
				if err != nil || getOf(getOf(reply, "result"), "isError") != false {
					t.Fatalf("original tool rejected: %s %v", &out, err)
				}
				content := listOf(getOf(getOf(reply, "result"), "content"))
				value, err := decodeNativeJSON([]byte(textOf(getOf(content[0], "text"))))
				if err != nil {
					t.Fatal(err)
				}
				return objectOrEmpty(value)
			}
			read := call(nativeWorkspaceTool, map[string]any{"action": "requirement", "from": 2, "to": 4})
			number := intOf(read["read"])
			if number < 1 {
				t.Fatalf("bound original requirement bytes have no selectable source: %v", read)
			}
			if intOf(read["from"]) != 2 || intOf(read["to"]) != 4 || read["contentDigest"] != "confirmed-original" {
				t.Fatal("requirement source changed its original line coordinates or identity", read)
			}
			repeated := call(nativeWorkspaceTool, map[string]any{"action": "requirement", "from": 6, "to": 6})
			if intOf(repeated["read"]) <= number {
				t.Fatal("repeated requirement read replaced its original source position")
			}
			selectLines := func(from, to int) any { return map[string]any{"read": number, "from": from, "to": to} }
			operations := []any{map[string]any{"kind": "SHELL", "command": selectLines(2, 2), "stdin": selectLines(3, 4)}, map[string]any{"kind": "ARGV", "argv": []any{selectLines(6, 6), "%s", selectLines(5, 5)}}}
			value := map[string]any{"cases": []any{map[string]any{"scenario": "SUCCESS", "oracle": "approved output", "operations": operations}}, "sharedSteps": []any{map[string]any{"procedure": "original preparation", "artifacts": []any{selectLines(5, 5)}, "operations": operations}}, "coverage": []any{}}
			if entrypoint == "commands" {
				value = map[string]any{"commands": []any{operations}}
			} else if entrypoint == "revision" {
				value = map[string]any{"cases": []any{map[string]any{"operations": operations}}}
			}
			call(nativeSemanticTool, value)
			if err := verifyNativeToolEvidence(directory); err != nil {
				t.Fatal("original published requirement evidence rejected", err)
			}
			facts, err := nativeToolFacts(directory)
			if err != nil {
				t.Fatal(err)
			}
			fact := facts[number-1]
			if fact.Kind != "requirement" || len(fact.Paths) != 0 || string(fact.Content) != original {
				t.Fatal("requirement source acquired physical file identity or lost original bytes", fact)
			}
			answer, err := submittedSemantics(directory)
			if err != nil {
				t.Fatal(err)
			}
			event, err := worker.DecodeResultWithFacts(input, "result", answer, "original-output", nil, workerLocationFacts(facts))
			if err != nil {
				t.Fatal(err)
			}
			var actual []coverage.Operation
			if entrypoint == "commands" {
				actual = event.Result.Validation.QACommands[0].Operations
			} else {
				actual = event.Result.Validation.Manifest.Cases[0].Operations
				if entrypoint == "design" && event.Result.Validation.Manifest.SharedSteps[0].Artifacts[0] != argument {
					t.Fatal("original artifact bytes changed")
				}
			}
			want := []coverage.Operation{{Kind: coverage.OperationShell, Command: command, Stdin: stdin}, {Kind: coverage.OperationArgv, Argv: []string{"/usr/bin/printf", "%s", argument}}}
			if !reflect.DeepEqual(actual, want) {
				t.Fatal("original operation values changed", actual)
			}
			for _, operation := range actual {
				argv := operation.Argv
				wantOutput := argument
				if operation.Kind == coverage.OperationShell {
					argv = coverage.ShellCommand(operation.Command)
					wantOutput = "<原命令 \"quoted\">\n" + stdin
				}
				cmd := exec.Command(argv[0], argv[1:]...)
				cmd.Dir, cmd.Stdin = workspace, strings.NewReader(operation.Stdin)
				output, err := cmd.CombinedOutput()
				if err != nil || string(output) != wantOutput {
					t.Fatalf("selected original invocation output: %q %v", output, err)
				}
			}
			resumed, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
			if err != nil {
				t.Fatal(err)
			}
			replayed, err := worker.DecodeResultWithFacts(input, "result", answer, "original-output", nil, workerLocationFacts(resumed.facts))
			if err != nil || !reflect.DeepEqual(event, replayed) {
				t.Fatal("native restart changed original value assembly", err)
			}
			for _, args := range []string{fmt.Sprintf(`{"action":"replace","read":%d,"from":1,"to":1,"content":"changed"}`, number), fmt.Sprintf(`{"action":"executable","read":%d,"executable":true}`, number)} {
				if _, err := resumed.call(json.RawMessage(args)); err == nil {
					t.Fatal("bound requirement source admitted physical mutation")
				}
			}
			if _, err := worker.LocationPaths([]worker.Location{{Read: number}}, workerLocationFacts(facts), input.RequirementDocument); err == nil {
				t.Fatal("bound requirement source acquired physical location capability")
			}
			if body, err := os.ReadFile(filepath.Join(workspace, "requirements.md")); err != nil || string(body) != "unconfirmed mutable bytes" {
				t.Fatal("bound requirement selection changed workspace content", err)
			}
			t.Logf("%s original read=%d: command/stdin/argv byte equality, actual output and native restart verified", entrypoint, number)
		})
	}
}

func nativeProjectCommandSource(t *testing.T, input protocol.WorkerInput, semantic func(int) any) (protocol.Event, string, string) {
	t.Helper()
	directory, workspace := nativeToolFixture(t)
	command := "./checks/project-check --label \"原命令 two words\" ''\nprintf 'next-line\\n'\n"
	if err := os.Mkdir(filepath.Join(workspace, "checks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "checks", "project-check"), []byte("#!/bin/sh\nprintf '<%s>\\n' \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("Project checks:\n"+command), 0600); err != nil {
		t.Fatal(err)
	}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	material := worker.Material{Input: input, ReadOnly: true, ResultSchema: schema}
	materialPath := filepath.Join(directory, "material.json")
	if err := writeAtomicJSON(materialPath, material); err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"provider": "dsh", "workspace": workspace, "read_only": true, "schema": schema, "engine": map[string]any{"workerMaterial": map[string]any{"path": materialPath, "actionId": input.Pending.ActionID, "attemptId": input.Pending.AttemptID}}}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	call := func(name string, arguments any) map[string]any {
		t.Helper()
		wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
		if err != nil {
			t.Fatal(err)
		}
		var out, errout bytes.Buffer
		if code := NativeTools(directory, bytes.NewReader(wire), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
			t.Fatalf("original MCP: %d %s", code, &errout)
		}
		reply, err := decodeNativeJSON(out.Bytes())
		if err != nil || getOf(getOf(reply, "result"), "isError") != false {
			t.Fatalf("original tool rejected: %s %v", &out, err)
		}
		content := listOf(getOf(getOf(reply, "result"), "content"))
		value, err := decodeNativeJSON([]byte(textOf(getOf(content[0], "text"))))
		if err != nil {
			t.Fatal(err)
		}
		return objectOrEmpty(value)
	}
	listing := call(nativeWorkspaceTool, map[string]any{"action": "list"})
	entry := 0
	for _, value := range listOf(listing["entries"]) {
		if getOf(value, "name") == "README.md" {
			entry = intOf(getOf(value, "entry"))
		}
	}
	if entry == 0 {
		t.Fatal("original project README not published")
	}
	read := call(nativeWorkspaceTool, map[string]any{"action": "read", "listing": intOf(listing["listing"]), "entry": entry})
	number := intOf(read["read"])
	value := semantic(number)
	call(nativeSemanticTool, value)
	if err := os.WriteFile(filepath.Join(directory, "native-source.json"), []byte(`{"sessionId":"original-session","turn":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	events := `{"sessionId":"original-session","event":{"type":"assistant/message","data":{"turn":1,"message":{"content":[{"type":"text","text":"Done"}]},"usage":{"inputTokens":23,"outputTokens":7}}}}` + "\n" + `{"sessionId":"original-session","event":{"type":"turn/end","data":{"turn":1,"reason":{"kind":"completed"}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(directory, "native-events.jsonl"), []byte(events), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := observeNative(directory, "original-session")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := NativeWorkerResultEvidence("dsh", directory, source)
	if err != nil {
		t.Fatal(err)
	}
	event, err := worker.DecodeResultWithFacts(input, "result", evidence.Answer, evidence.Digest, nil, evidence.LocationFacts)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := observeNative(directory, "original-session")
	if err != nil || !reflect.DeepEqual(source, recovered) {
		t.Fatal("native recovery changed source", err)
	}
	stored, _ := json.Marshal(evidence.LocationFacts)
	var facts []worker.LocationFact
	if err := json.Unmarshal(stored, &facts); err != nil {
		t.Fatal(err)
	}
	replayed, err := worker.DecodeResultWithFacts(input, "result", evidence.Answer, evidence.Digest, nil, facts)
	if err != nil || !reflect.DeepEqual(event, replayed) {
		t.Fatal("source-bound replay changed command", err)
	}
	if err := os.WriteFile(filepath.Join(directory, nativeToolCallsFile), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NativeWorkerResultEvidence("dsh", directory, source); err == nil {
		t.Fatal("lost original read/tool evidence admitted")
	}
	return event, workspace, command
}

func TestNativeReadOwnsDeclaredProjectCheckCommand(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.readiness.plan.v4", Provider: "dsh", Pending: protocol.PendingAction{ActionID: "original-action", AttemptID: "original-attempt"}, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness, RequirementRevision: "confirmed"}}
	event, workspace, command := nativeProjectCommandSource(t, input, func(number int) any {
		verification := []string{}
		for range review.RequiredVerificationChecks() {
			verification = append(verification, "Explicit fixture evidence for the current project and plan")
		}
		checks := []any{}
		for _, purpose := range []string{protocol.CheckEdit, protocol.CheckFullBuild, protocol.CheckFullTests} {
			checks = append(checks, map[string]any{"read": number, "from": 2, "to": 3, "purpose": purpose})
		}
		batch := map[string]any{"subtasks": []any{map[string]any{"description": "Deliver the public command"}}, "deliverable": "Public command", "publicEntry": "Project command", "artifacts": []any{map[string]any{"read": number}}, "interfaces": "Public arguments", "stateBoundary": "Project source", "risk": "Keep existing behavior", "dod": []any{map[string]any{"description": "Public behavior matches the oracle"}}, "checks": checks, "rollback": "Restore project source", "recovery": "Continue the original task", "handoff": "Original command checks are available", "contextCost": "One capability"}
		return map[string]any{"findings": []any{}, "verification": verification, "granularityReview": map[string]any{"reason": "One independent capability", "verificationTier": "FINAL", "batches": []any{batch}}}
	})
	plan := event.Result.Review.GranularityReview
	if err := protocol.ValidateGranularity(plan); err != nil {
		t.Fatal(err)
	}
	for _, check := range plan.Batches[0].Checks {
		if check.Command != command {
			t.Fatalf("original command bytes changed: %q", check.Command)
		}
	}
	argv := coverage.ShellCommand(plan.Batches[0].Checks[0].Command)
	actual := exec.Command(argv[0], argv[1:]...)
	actual.Dir = workspace
	out, err := actual.CombinedOutput()
	if err != nil || string(out) != "<--label>\n<原命令 two words>\n<>\nnext-line\n" {
		t.Fatalf("original approved invocation: %q %v", out, err)
	}
}

func TestNativeReadOwnsOriginalQAInvocation(t *testing.T) {
	for _, kind := range []string{"design", "commands"} {
		t.Run(kind, func(t *testing.T) {
			input := protocol.WorkerInput{RunID: "issued-run", Handler: "engine.qa.design.v4", Provider: "dsh", Pending: protocol.PendingAction{ActionID: "original-action", AttemptID: "original-attempt"}, Validation: &protocol.ValidationInput{Binding: protocol.WorkBinding{RequirementRevision: "confirmed", Generation: 7}, Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{SourceBinding: coverage.SourceBinding{RunID: "issued-run", RequirementRevision: "confirmed", SolutionRevision: "solution"}, ReviewKind: coverage.ReviewBlackbox}, Sources: []string{"source"}, Points: []coverage.AcceptancePoint{{PointID: "point", ObservableBehavior: "the documented behavior", Oracle: "the documented output"}}}, SourcePoints: []protocol.SourcePoint{{SourceID: "source", PointID: "point"}}}}
			if kind == "commands" {
				input.Handler = "engine.qa.execution.v3"
				input.Validation.CommandCases = []string{"approved-case"}
			}
			event, workspace, original := nativeProjectCommandSource(t, input, func(read int) any {
				operation := map[string]any{"kind": "SHELL", "command": map[string]any{"read": read, "from": 2, "to": 3}}
				if kind == "commands" {
					return map[string]any{"commands": [][]any{{operation}}}
				}
				return map[string]any{"cases": []any{map[string]any{"scenario": "SUCCESS", "oracle": "matches documented output", "operations": []any{operation}, "sharedSteps": []int{1}}}, "sharedSteps": []any{map[string]any{"procedure": "run the original preparation", "operations": []any{operation}}}, "coverage": [][]int{{1}}}
			})
			var operation coverage.Operation
			if kind == "commands" {
				if !event.Result.Validation.Checkpoint || event.Result.Validation.QACommands[0].CaseID != "approved-case" {
					t.Fatal("original missing-invocation responsibility changed")
				}
				operation = event.Result.Validation.QACommands[0].Operations[0]
			} else {
				manifest := event.Result.Validation.Manifest
				operation = manifest.Cases[0].Operations[0]
				if manifest.SharedSteps[0].Operations[0].Command != original {
					t.Fatal("shared preparation original bytes changed")
				}
			}
			if operation.Command != original {
				t.Fatalf("original recipe bytes changed: %q", operation.Command)
			}
			argv := coverage.ShellCommand(operation.Command)
			actual := exec.Command(argv[0], argv[1:]...)
			actual.Dir = workspace
			out, err := actual.CombinedOutput()
			if err != nil || string(out) != "<--label>\n<原命令 two words>\n<>\nnext-line\n" {
				t.Fatalf("original QA invocation: %q %v", out, err)
			}
		})
	}
}
