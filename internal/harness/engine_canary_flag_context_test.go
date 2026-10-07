//go:build !windows

package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
)

// Probe causality comes from the accepted issuance, without changing raw reports.
func TestCanaryFlagContextUsesIssuedObservation(t *testing.T) {
	var sample struct {
		Finding   protocol.ValidationFinding         `json:"finding"`
		Target    protocol.ValidationTarget          `json:"target"`
		Result    protocol.WorkerResult              `json:"result"`
		Resources map[string]protocol.NativeResource `json:"nativeResources"`
		Issued    uint64                             `json:"issuedRevision"`
		Attempt   string                             `json:"derivedAttemptID"`
		RunID     string                             `json:"runId"`
	}
	if err := json.Unmarshal(readTestFileBytes(t, filepath.Join("testdata", "issued_material_finding.json")), &sample); err != nil {
		t.Fatal(err)
	}
	jobs, workspace := filepath.Join(t.TempDir(), "jobs with spaces"), t.TempDir()
	state := &protocol.State{RunID: sample.RunID, Results: map[string]protocol.WorkerResult{sample.Target.ResultAction: sample.Result}, DispatchIssueRevisions: map[string]uint64{sample.Target.ResultAction: sample.Issued}}
	dir, err := canaryFindingMaterialDirectory(jobs, state, &sample.Target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	binding := sample.Result.Validation.Binding
	material := canaryMaterials{RunID: sample.RunID, ActionID: sample.Result.ActionID, RequirementRevision: binding.RequirementRevision, Pending: &protocol.PendingAction{ActionID: sample.Result.ActionID, Task: binding.Task, AttemptID: sample.Attempt}, Input: &protocol.ValidationInput{Binding: binding, Gate: &protocol.GateInput{Gate: sample.Target.Gate}}}
	material.Input.Gate.Candidate.Identity = binding.Candidate
	resource := sample.Resources[binding.WorkspaceID]
	material.Input.Workspace = &resource
	material.Input.Workspace.Path = workspace
	probe := canaryObservation{Label: "public flag-contract probe", Directory: workspace, Command: append([]string{filepath.Join(dir, "frob-candidate")}, canaryGateArgs...), Output: "frob: --UNKNOWN-FLAG bytes=14"}
	write := func(m canaryMaterials) {
		t.Helper()
		body, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "materials.json"), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	material.HostObservations = []canaryObservation{probe}
	write(material)
	accepted, err := canaryFlagFindingContext(dir, workspace, state, &sample.Target)
	if err != nil || accepted.RootCause != "unknown flags are accepted by the argument handling" || !strings.Contains(accepted.Verification.Reproduction, "exit=0") {
		t.Fatalf("accepted probe: %+v %v", accepted, err)
	}
	for _, exit := range []int{1, 2, 7} {
		material.HostObservations[0].ExitCode = exit
		material.HostObservations[0].Output = "frob: unknown flag \"--unknown-flag\"\nusage: frob <word>"
		write(material)
		rejected, err := canaryFlagFindingContext(dir, workspace, state, &sample.Target)
		if err != nil || rejected.RootCause == accepted.RootCause || rejected.Severity != "P2" || !strings.Contains(rejected.Verification.Causality, "exits nonzero") {
			t.Fatalf("rejected probe: %+v %v", rejected, err)
		}
		accepted.Requirement, accepted.Module = "AC-004", "flag handling"
		rejected.Requirement, rejected.Module = accepted.Requirement, accepted.Module
		accepted.ProblemID = "accepted-problem"
		validation := &protocol.Validation{Findings: map[string]*protocol.ValidationFinding{"accepted": {History: []protocol.FindingResolution{accepted}}}, Problems: map[string]*protocol.ValidationProblem{"accepted-problem": {ID: "accepted-problem"}}}
		if id, err := canaryFlagProblem(validation, rejected); err != nil || id != "" {
			t.Fatalf("distinct cause merged: %s %v", id, err)
		}
		rejected.ProblemID = "diagnostic-problem"
		validation.Findings["diagnostic"] = &protocol.ValidationFinding{History: []protocol.FindingResolution{rejected}}
		validation.Problems[rejected.ProblemID] = &protocol.ValidationProblem{ID: rejected.ProblemID}
		for duplicate := 0; duplicate < 2; duplicate++ {
			if id, err := canaryFlagProblem(validation, rejected); err != nil || id != rejected.ProblemID {
				t.Fatalf("same cause identity changed: %s %v", id, err)
			}
		}
	}
	resource.ID = binding.WorkspaceID
	state.NativeResources = map[string]protocol.NativeResource{resource.ID: resource}
	wave := &protocol.ValidationWave{ID: binding.WaveID, Targets: map[string]*protocol.ValidationTarget{"merge": &sample.Target}}
	wave.Candidate.Identity = binding.Candidate
	correctness := sample.Target
	correctness.Gate, correctness.SliceID, correctness.ResultAction = "correctness", "uppercase", "correctness-action"
	wave.Targets["correctness"] = &correctness
	write(material)
	shared, err := canaryFlagWaveContext(jobs, state, wave)
	if err != nil || shared.RootCause != "unknown-flag rejection emits the command output prefix" {
		t.Fatalf("master slice correctness lost shared wave cause: %+v %v", shared, err)
	}
	wave.Candidate.Identity = "other-wave-candidate"
	if _, err := canaryFlagWaveContext(jobs, state, wave); err == nil {
		t.Fatal("another wave candidate supplied the cause")
	}
	wave.Candidate.Identity = binding.Candidate

	for _, mismatch := range []string{"action", "attempt", "candidate", "binding", "workspace", "argv", "binary", "conforming"} {
		t.Run(mismatch, func(t *testing.T) {
			body, _ := json.Marshal(material)
			var changed canaryMaterials
			if err := json.Unmarshal(body, &changed); err != nil {
				t.Fatal(err)
			}
			switch mismatch {
			case "action":
				changed.ActionID = "other-action"
			case "attempt":
				changed.Pending.AttemptID += "-other"
			case "candidate":
				changed.Input.Gate.Candidate.Identity = "other-candidate"
			case "binding":
				changed.Input.Binding.WaveID = "other-wave"
			case "workspace":
				changed.HostObservations[0].Directory = t.TempDir()
			case "argv":
				changed.HostObservations[0].Command[1] = "hello"
			case "binary":
				changed.HostObservations[0].Command[0] = filepath.Join(t.TempDir(), "frob-candidate")
			case "conforming":
				changed.HostObservations[0].Output = "unknown flag"
			}
			write(changed)
			if _, err := canaryFlagFindingContext(dir, workspace, state, &sample.Target); err == nil {
				t.Fatal("unverified context accepted")
			}
		})
	}
}

func TestCanaryAnnotatedMaterialFindingPreservesRawReport(t *testing.T) {
	var sample struct {
		Finding   protocol.ValidationFinding         `json:"finding"`
		Target    protocol.ValidationTarget          `json:"target"`
		Result    protocol.WorkerResult              `json:"result"`
		Resources map[string]protocol.NativeResource `json:"nativeResources"`
		Issuance  map[string]uint64                  `json:"dispatchIssueRevisions"`
		RunID     string                             `json:"runId"`
		Jobs      string                             `json:"jobs"`
		Materials canaryMaterials                    `json:"materials"`
	}
	if err := json.Unmarshal(readTestFileBytes(t, filepath.Join("testdata", "annotated_flag_material_finding.json")), &sample); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(sample.Finding)
	state := &protocol.State{RunID: sample.RunID, Results: map[string]protocol.WorkerResult{sample.Target.ResultAction: sample.Result}, NativeResources: sample.Resources, DispatchIssueRevisions: sample.Issuance}
	workspace, err := canaryFindingWorkspace(&sample.Target, sample.Result, sample.Resources)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := canaryFindingMaterialDirectory(sample.Jobs, state, &sample.Target)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := canaryFlagFindingResolution(workspace, dir, &sample.Target, &sample.Finding, "AC-004", false)
	if err != nil || resolution.Decision != "SAME_AS" || sample.Target.Status != "FAIL" || sample.Finding.Raw.Severity != "P1" {
		t.Fatalf("annotated issued finding: %+v %v", resolution, err)
	}
	jobs := t.TempDir()
	localDir, err := canaryFindingMaterialDirectory(jobs, state, &sample.Target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(localDir, 0700); err != nil {
		t.Fatal(err)
	}
	for i := range sample.Materials.HostObservations {
		sample.Materials.HostObservations[i].Command[0] = filepath.Join(localDir, "frob-candidate")
	}
	body, _ := json.Marshal(sample.Materials)
	if err := os.WriteFile(filepath.Join(localDir, "materials.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	context, err := canaryFlagFindingContext(localDir, workspace, state, &sample.Target)
	if err != nil || context.RootCause != "unknown-flag rejection emits the command output prefix" || context.Severity != "P2" {
		t.Fatalf("physical issued rejection: %+v %v", context, err)
	}
	after, _ := json.Marshal(sample.Finding)
	if string(before) != string(after) {
		t.Fatal("typed adjudication changed the raw finding")
	}
}
