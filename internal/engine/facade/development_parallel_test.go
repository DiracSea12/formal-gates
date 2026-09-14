package facade

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

func TestIndependentDevelopersUseSeparateWorkspacesAndJoin(t *testing.T) {
	testIndependentDevelopers(t, false)
}

func TestIndependentDevelopersPreserveConflictingResults(t *testing.T) {
	testIndependentDevelopers(t, true)
}

func testIndependentDevelopers(t *testing.T, conflict bool) {
	f := facadePreparationPlan(t, nil, 2, "", func(p *protocol.DevelopmentPlan) {
		p.Batches[1].Dependencies = nil
		p.Batches[1].Task = "Write independent copper output"
	})
	initial := facadeExecution(t, f, "development-worker")
	r, _ := f.Show()
	common := r.Regular.Preparation.Resources["development"]
	writePreparationFile(t, common, "amber.txt", "original")
	facadeResult(t, f, initial, protocol.PreparationResult{Manifest: []string{"amber.txt"}, Handoff: "No unresolved decisions", Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "read amber.txt", Observation: "original", Result: "PASS"}}})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	primary := facadeExecution(t, f, "development-worker")
	attach := func(x protocol.RegularExecution) {
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: "native-" + x.Claim.ActionID, Status: "RUNNING"}})
	}
	attach(primary)
	// Both real local child processes remain alive at the same time. Their
	// workspaces come from engine-issued claims; the fixture does not pick paths.
	start := func(workspace, script string) (*exec.Cmd, io.WriteCloser) {
		cmd := exec.Command("sh", "-c", script)
		cmd.Dir = workspace
		in, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = in.Close()
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
		line, err := bufio.NewReader(out).ReadString('\n')
		if err != nil || line != "ready\n" {
			t.Fatalf("worker ready: %q %v", line, err)
		}
		return cmd, in
	}
	primaryPath := "copper.txt"
	if conflict {
		primaryPath = "amber.txt"
	}
	first, firstIn := start(common, "printf copper > "+primaryPath+"; printf 'ready\n'; read finish")
	before, _ := f.Show()
	old := before.Executions[primary.Claim.ActionID]
	facadeChoice(t, f, protocol.RegularRequest{Action: "development-increment", Target: "B1", Meaning: "preserved", Reason: "independent amber correction", Increment: &protocol.DevelopmentIncrement{Kind: "optimize", BatchID: "B1"}})
	r, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	var second protocol.RegularExecution
	for _, x := range r.Executions {
		if x.Claim.Task == r.Regular.Preparation.BatchTasks["B1"] && !x.Finished {
			second = x
		}
	}
	if second.Claim.ActionID == "" {
		t.Fatal("no parallel developer action")
	}
	payload, isolated, err := preparationInput(r, second.Claim)
	if err != nil {
		t.Fatal(err)
	}
	if isolated == common || payload["batch"].(protocol.DevelopmentBatch).ID != "B1" {
		t.Fatal("wrong workspace or task material")
	}
	if _, err := os.Stat(filepath.Join(isolated, "copper.txt")); !os.IsNotExist(err) {
		t.Fatal("uncommitted primary input leaked")
	}
	if !reflect.DeepEqual(old, r.Executions[primary.Claim.ActionID]) {
		t.Fatal("active primary was rebound")
	}
	attach(second)
	other, otherIn := start(isolated, "printf revised > amber.txt; printf 'ready\n'; read finish")
	finish := func(cmd *exec.Cmd, in io.WriteCloser) {
		if _, err := io.WriteString(in, "finish\n"); err != nil {
			t.Fatal(err)
		}
		_ = in.Close()
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	deliver := func(x protocol.RegularExecution, path, subtask, observation string) {
		v := protocol.PreparationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: "native-" + x.Claim.ActionID, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "PASS", Manifest: []string{path}, Handoff: "No unresolved decisions", Validation: []protocol.BatchValidation{{SubtaskID: subtask, Procedure: "sh writes bound output; exit 0", Observation: observation, Result: "PASS"}}}
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: v.Identity, Status: "UNAVAILABLE", Reason: "local fixture process, no host transcript"}})
	}
	finish(other, otherIn)
	deliver(second, "amber.txt", "S1", "revised")
	r, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if r.Regular.Preparation.DevelopmentSnapshot != primary.Claim.Candidate {
		t.Fatal("joined while the common workspace still had a writer")
	}
	retained := r.Regular.Preparation.Batches["B1"]
	f, err = Open(f.root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	finish(first, firstIn)
	deliver(primary, primaryPath, "S2", "copper")
	r, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if conflict {
		if _, err := f.Drive(); err == nil || !strings.Contains(err.Error(), "ARTIFACT_CONFLICT: amber.txt") {
			t.Fatalf("missing concrete shared-file conflict: %v", err)
		}
		current, err := f.Show()
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(common, "amber.txt"))
		if err != nil || string(got) != "copper" || !reflect.DeepEqual(retained, current.Regular.Preparation.Batches["B1"]) || current.Regular.Preparation.Commits["B1"].After != "" || current.Regular.Preparation.Operation == nil {
			t.Fatal("conflict lost valid work, claimed success, or discarded recovery intent")
		}
		return
	}
	// Interrupt after native integration and before its persisted receipt. The
	// existing operation trailer must reconcile both source and common commits.
	f.fault = func(point persistence.FaultPoint) error {
		if point == persistence.FaultExecuteAfter {
			return fmt.Errorf("fixture integration receipt interrupted")
		}
		return nil
	}
	if _, err := f.Drive(); err == nil {
		t.Fatal("missing integration interruption")
	}
	f, err = Open(f.root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"amber.txt": "revised", "copper.txt": "copper"} {
		got, err := gitprep.Git(common, "show", r.Regular.Preparation.DevelopmentSnapshot+":"+name)
		if err != nil || got != want {
			t.Fatal(fmt.Sprintf("joined %s: %q %v", name, got, err))
		}
	}
	if !reflect.DeepEqual(retained, r.Regular.Preparation.Batches["B1"]) {
		t.Fatal("join rewrote original result evidence")
	}
}
