package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/testkit"
)

func TestWorkflowTerminalCleanupRetiresProgramPayloads(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	for _, status := range []string{"ABORTED", "COMPLETE"} {
		t.Run(status, func(t *testing.T) {
			fixture := t.TempDir()
			startAdmittedEngineRun(t, fixture)
			root, runID := fixture, "writer-exclusive"
			payloadKind, payloadPosition := "control", 0
			call := func(args ...string) []byte {
				t.Helper()
				var out, stderr bytes.Buffer
				args = append(append([]string{"workflow"}, args...), "--root", root)
				if code := runWithHostConfirmation(t, args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
					t.Fatalf("original CLI %v exit %d: %s", args, code, &stderr)
				}
				return out.Bytes()
			}
			candidate := func(body []byte, field string) int {
				t.Helper()
				var value map[string]json.RawMessage
				var position int
				if err := json.Unmarshal(body, &value); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(value[field], &position); err != nil || position < 1 {
					t.Fatal("original CLI did not publish an input position", string(body), err)
				}
				return position
			}
			if status == "COMPLETE" {
				root, runID = t.TempDir(), "payload-seal"
				mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliStructuredRequirement("Save the original progress before terminal cleanup."))
				intake := candidate(call("start", "--precheck", "--route", "lightweight", "--document", "1"), "intake")
				call("start", "--route", "lightweight", "--run-id", runID, "--intake", strconv.Itoa(intake))
				payloadKind = "progress"
				payloadPosition = candidate(call("progress", "--save-progress"), "progress")
			} else {
				position := candidate(call("next", "--request", "ABORT"), "confirmationCandidate")
				call("submit", "--confirmation-candidate", strconv.Itoa(position), "--single-step")
				position = candidate(call("next", "--ask", "1", "--choice", "abort"), "confirmationCandidate")
				payloadPosition = position
				call("submit", "--confirmation-candidate", strconv.Itoa(position), "--single-step")
				before := snapshotEngineRunTree(t, root, runID)
				call("submit", "--confirmation-candidate", strconv.Itoa(position), "--single-step")
				if !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, runID)) {
					t.Fatal("repeated original confirmation changed its accepted projection")
				}
			}
			directory := filepath.Join(root, facade.EngineNamespace, runID)
			payloads, err := facade.OriginalPayloadPaths(workflowPayloadDirectory(root, runID, payloadKind))
			if err != nil || payloadPosition < 1 || payloadPosition > len(payloads) {
				t.Fatal("public CLI did not create its original owned payload", payloads, err)
			}
			original, err := os.ReadFile(payloads[payloadPosition-1])
			if err != nil || !json.Valid(original) || !bytes.Contains(original, []byte(`"runId"`)) {
				t.Fatal("program payload lost its original run content", err)
			}
			if payloadKind == "progress" {
				var progress facade.Progress
				if err := json.Unmarshal(original, &progress); err != nil || progress.Root != root || progress.RunID != runID {
					t.Fatal("original progress belongs to another project or run", err, progress)
				}
			}
			unrelated := filepath.Join(root, "payloads", "user-data")
			mustWriteCLI(t, unrelated, "unrelated user bytes\r\n原文  ")
			call("drive")
			if _, err := os.Stat(filepath.Join(directory, "payloads")); !os.IsNotExist(err) {
				t.Fatal("terminal cleanup retained the program payload tree", err)
			}
			receiptPath := filepath.Join(directory, "cleanup.receipt.json")
			receipt, err := os.ReadFile(receiptPath)
			if err != nil || string(receipt) != "{\"entries\":[],\"residue\":false,\"status\":\"reconciled\"}\n" {
				t.Fatal("terminal receipt does not prove physical cleanup", string(receipt), err)
			}
			summaryPath := filepath.Join(directory, "terminal-summary.json")
			summary, err := os.ReadFile(summaryPath)
			if err != nil {
				t.Fatal(err)
			}
			var terminal facade.TerminalSummary
			if err := json.Unmarshal(summary, &terminal); err != nil || terminal.RunID != runID || terminal.Status != status {
				t.Fatal("terminal content differs from original run", err)
			}
			for _, command := range []string{"show", "status", "next", "drive", "drive"} {
				call(command)
			}
			for path, expected := range map[string][]byte{receiptPath: receipt, summaryPath: summary, unrelated: []byte("unrelated user bytes\r\n原文  ")} {
				actual, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(actual, expected) {
					t.Fatal("terminal replay changed receipt, summary or unrelated bytes", path, err)
				}
			}
			if _, err := os.Stat(filepath.Join(directory, "state.json")); !os.IsNotExist(err) {
				t.Fatal("terminal replay recreated active state", err)
			}
		})
	}
}
