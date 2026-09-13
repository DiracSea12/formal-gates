package facade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

// The transport substitutes only the model/process. Claims, native tool calls,
// checkpoint collection, restart and resume all use the ordinary engine path.
type checkpointMaterialTransport struct {
	codexRuntimeAdapter
	t     *testing.T
	f     *Facade
	turns int
	paths []string
}

func (a *checkpointMaterialTransport) source(string, string, *protocol.HostRuntimeContext) (string, error) {
	return "", fmt.Errorf("deterministic transport fixture has no model transcript")
}

func (a *checkpointMaterialTransport) turn(q nativeTurn) (string, error) {
	t := a.t
	identity := "checkpoint-material-fixture"
	if q.Resume != (a.turns > 0) || (q.Resume && q.Claim.Identity != identity) {
		t.Fatalf("resume lost original identity: %+v", q.Claim)
	}
	if err := q.Attached(identity); err != nil {
		t.Fatal(err)
	}
	var input struct {
		Binding  protocol.ExecutionClaim `json:"binding"`
		CaseFile struct {
			Path string `json:"path"`
		} `json:"caseFile"`
		CheckpointFile struct {
			Path string `json:"path"`
		} `json:"checkpointFile"`
		Requirement struct {
			Path string `json:"path"`
		} `json:"requirement"`
		PublicContracts []struct {
			Path string `json:"path"`
		} `json:"publicContracts"`
		RequiredInputs []struct {
			Path string `json:"path"`
		} `json:"requiredInputs"`
		Remaining []string `json:"remaining"`
	}
	start := strings.Index(q.Prompt, "\n\n{")
	if start < 0 {
		t.Fatal("missing action input")
	}
	if err := json.Unmarshal([]byte(q.Prompt[start+2:]), &input); err != nil {
		t.Fatal(err)
	}
	wantBinding := q.Claim
	wantBinding.Context = nil
	if !reflect.DeepEqual(input.Binding, wantBinding) {
		t.Fatal("message binding differs from dispatched claim")
	}
	read := func(path string) []byte {
		t.Helper()
		if !filepath.IsAbs(path) {
			t.Fatalf("non-absolute material reference: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("actual dispatched material is unreadable: %s: %v", path, err)
		}
		return data
	}
	run, err := a.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	originalJob := a.f.codexJob(q.Claim.ActionID)
	if input.Requirement.Path != filepath.Join(originalJob, "requirement.md") || string(read(input.Requirement.Path)) != run.Regular.Requirement.Text {
		t.Fatal("resume changed frozen requirement binding")
	}
	for _, contract := range input.PublicContracts {
		if filepath.Dir(contract.Path) != originalJob {
			t.Fatal("resume moved frozen public contract")
		}
		read(contract.Path)
	}
	var saved struct {
		Checkpoint protocol.VerificationResult `json:"checkpoint"`
	}
	if a.turns == 0 {
		if input.CheckpointFile.Path != "" {
			t.Fatal("initial input unexpectedly has checkpoint")
		}
		if _, err := os.Stat(filepath.Join(q.Job, "checkpoint.json")); !os.IsNotExist(err) {
			t.Fatal("initial checkpoint file exists", err)
		}
	} else {
		if err := json.Unmarshal(read(input.CheckpointFile.Path), &saved); err != nil {
			t.Fatal(err)
		}
		cp := verificationWave(run).Checkpoints["blackbox"]
		if !reflect.DeepEqual(saved.Checkpoint.Cases, cp.Cases) || saved.Checkpoint.Digest != cp.Digest || saved.Checkpoint.ActionID != q.Claim.ActionID {
			t.Fatal("checkpoint material lost exact accepted evidence/binding")
		}
		a.paths = append(a.paths, input.CheckpointFile.Path)
	}
	var target struct {
		Execute []string                  `json:"execute"`
		Cases   []coverage.AcceptanceCase `json:"cases"`
	}
	if err := json.Unmarshal(read(input.CaseFile.Path), &target); err != nil {
		t.Fatal(err)
	}
	want := []string{"BLACKBOX:C1", "BLACKBOX:C2", "BLACKBOX:C3"}[a.turns:]
	if !reflect.DeepEqual(target.Execute, want) || !reflect.DeepEqual(input.Remaining, want) || len(target.Cases) != len(want) {
		t.Fatalf("stale remaining tasks: message=%v file=%v want=%v", input.Remaining, target.Execute, want)
	}
	if !reflect.DeepEqual(target.Cases, run.Regular.Preparation.QA["blackbox"].Design.Manifest.Cases[a.turns:]) {
		t.Fatal("resume changed frozen case contracts")
	}
	if len(input.RequiredInputs) != 1 || input.RequiredInputs[0].Path != input.CaseFile.Path {
		t.Fatal("required input does not bind current case file")
	}
	a.paths = append(a.paths, input.CaseFile.Path)
	var receipt protocol.TaskOperationReceipt
	for _, kind := range []string{"qa.prepare", "qa.target", "qa.collect"} {
		args, _ := json.Marshal(map[string]any{"kind": kind, "caseId": want[0], "prompt": "", "resume": false})
		value, err := q.Operation(identity, want[0]+":"+kind, args)
		if err != nil {
			t.Fatal(kind, err)
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &receipt); err != nil || receipt.Status != "COMPLETED" {
			t.Fatal(kind, receipt, err)
		}
	}
	if receipt.Output != "actual" || receipt.Stderr != "semantic" || receipt.ExitCode != 0 {
		t.Fatalf("actual command evidence: %+v", receipt)
	}
	row := protocol.CaseExecution{CaseID: want[0], Status: "PASS", Procedure: "frozen shell command", Output: receipt.Output, Comparison: "stdout actual, stderr semantic, exit 0", Environment: "local fixture", Tool: "sh", Workspace: receipt.Workspace}
	status := "CHECKPOINT"
	if len(want) == 1 {
		status = "PASS"
	}
	c := q.Claim
	result := protocol.VerificationResult{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, Identity: identity, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, BindingDigest: c.PrerequisiteDigest, Status: status, Message: "deterministic checkpoint judgment with real command evidence", Cases: append(saved.Checkpoint.Cases, row), Remaining: want[1:]}
	if err := writeJSONAtomic(filepath.Join(q.Job, "answer.json"), result); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(q.Job, "process.json"), map[string]any{"actionId": c.ActionID, "identity": identity, "pid": os.Getpid(), "stopped": true, "exitCode": 0}); err != nil {
		t.Fatal(err)
	}
	a.turns++
	t.Logf("turn %d: readable exact material, remaining=%v; executed only %s (stdout=actual stderr=semantic exit=0)", a.turns, want, want[0])
	return identity, nil
}

func TestVerificationCheckpointResumeMaterialBinding(t *testing.T) {
	f := frozenVerificationCommandFixture(t, false, nil, func(m *coverage.AcceptanceManifest) {
		first, edge := m.Cases[0], m.Edges[0]
		m.Cases, m.Edges = nil, nil
		for i := 1; i <= 3; i++ {
			c, e := first, edge
			c.CaseID = fmt.Sprintf("BLACKBOX:C%d", i)
			c.WorkspaceMode = "isolated"
			c.Command = &coverage.Command{Executable: "sh", Args: []string{"-c", "printf actual; printf semantic >&2"}}
			e.CaseID = c.CaseID
			m.Cases, m.Edges = append(m.Cases, c), append(m.Edges, e)
		}
	})
	x := facadeExecution(t, f, "blackbox-execution")
	a := &checkpointMaterialTransport{t: t, f: f}
	frozen := map[string][]byte{}
	for turn := 0; turn < 3; turn++ {
		before, err := f.Show()
		if err != nil {
			t.Fatal(err)
		}
		run, err := f.executeNativeTransport(x.Claim.ActionID, turn > 0, a)
		if err != nil {
			t.Fatal(err)
		}
		after := run.Executions[x.Claim.ActionID]
		if !reflect.DeepEqual(before.Regular.Preparation.QA, run.Regular.Preparation.QA) {
			t.Fatal("checkpoint/resume changed frozen QA")
		}
		if after.Claim.AttemptID != x.Claim.AttemptID || after.Identity != "checkpoint-material-fixture" {
			t.Fatal("resume allocated another attempt/identity")
		}
		for id, op := range before.Executions[x.Claim.ActionID].Operations {
			if !reflect.DeepEqual(after.Operations[id], op) {
				t.Fatalf("completed QA operation changed: %s", id)
			}
		}
		if len(after.Operations) != (turn+1)*3 {
			t.Fatalf("completed QA reran: %d operations", len(after.Operations))
		}
		for path, data := range frozen {
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("frozen material/receipt overwritten: %s: %v", path, err)
			}
		}
		// Capture every invocation/material file, including original prompt,
		// answer/process receipts and all earlier resume inputs, byte for byte.
		capture := func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			frozen[path] = data
			return nil
		}
		if err := filepath.WalkDir(f.codexJob(x.Claim.ActionID), capture); err != nil {
			t.Fatal(err)
		}
		for id := range after.Operations {
			if err := filepath.WalkDir(f.taskOperationDir(id), capture); err != nil {
				t.Fatal(err)
			}
		}
		if turn == 2 && (!after.Finished || verificationWave(run).Results["blackbox"].Status != "PASS") {
			t.Fatal("remaining QA did not finish")
		}
		f, err = Open(f.root, f.runID)
		if err != nil {
			t.Fatal(err)
		}
		a.f = f
	}
	seen := map[string]bool{}
	for _, path := range a.paths {
		if seen[path] {
			t.Fatalf("resume reused mutable material path: %s", path)
		}
		seen[path] = true
	}
	if a.turns != 3 {
		t.Fatal("missing native invocation")
	}
}
