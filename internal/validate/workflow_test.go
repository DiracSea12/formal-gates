package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowRecordStageCallsGateState(t *testing.T) {
	dir := t.TempDir()
	artifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")

	result := WorkflowRecordStage(WorkflowRecordStageOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       artifact,
		Actor:          "qa",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !result.OK() {
		t.Fatalf("expected workflow record-stage to pass, got %#v", result.Failures)
	}
	runRel := filepath.ToSlash(filepath.Join(".gates", "runs", "wf"))
	state, show := GateShow(GateShowOptions{Worktree: dir, StatePath: filepath.ToSlash(filepath.Join(runRel, "restricted", "gate-state.json"))})
	if !show.OK() {
		t.Fatalf("expected show to pass, got %#v", show.Failures)
	}
	if state.Gates["qa-test-gate"].Actor != "qa" {
		t.Fatalf("expected gate-state entry from workflow record-stage, got %#v", state.Gates["qa-test-gate"])
	}
	if isFile(filepath.Join(dir, ".gates", "gate-state.json")) {
		t.Fatal("omitted --run-dir wrote repository-level gate state")
	}
}

func TestWorkflowRecordStageWithoutRunDirRejectsArtifactOutsideDefaultRun(t *testing.T) {
	dir := t.TempDir()
	envelope, payload := qaExecutionPolicyFixture(t, dir, "wf", "snap")
	writeEnvelopeTest(t, filepath.Join(dir, "qa-test-gate.md"), envelope, payload)

	result := WorkflowRecordStage(WorkflowRecordStageOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       "qa-test-gate.md",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if result.OK() || !strings.Contains(resultSummary(result), "artifact must be under the active run restricted directory") {
		t.Fatalf("repository-level artifact was accepted: %#v", result.Failures)
	}
	if isFile(filepath.Join(dir, ".gates", "gate-state.json")) || isFile(filepath.Join(dir, ".gates", "runs", "wf", "gate-state.json")) {
		t.Fatal("rejected repository-level artifact changed gate state")
	}
}

func TestArtifactRejectsActiveRunRootFile(t *testing.T) {
	dir := t.TempDir()
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	path := filepath.Join(runDir, "review.json")
	mustWrite(t, path, "{}\n")
	result := Artifact(ArtifactOptions{Root: dir, RunDir: runDir, File: relativePath(dir, path), Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() || !strings.Contains(resultSummary(result), "active run restricted directory") {
		t.Fatalf("active-run root artifact was accepted: %#v", result.Failures)
	}
}

func TestWorkflowVerifyAdmissionPositiveAndNegative(t *testing.T) {
	dir := t.TempDir()
	allowedBeforeQA := WorkflowVerifyAdmission(WorkflowVerifyAdmissionOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !allowedBeforeQA.OK() {
		t.Fatalf("independent complexity admission was blocked before QA: %#v", allowedBeforeQA.Failures)
	}

	artifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	record := WorkflowRecordStage(WorkflowRecordStageOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       artifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !record.OK() {
		t.Fatalf("expected record-stage to pass, got %#v", record.Failures)
	}

	allowed := WorkflowVerifyAdmission(WorkflowVerifyAdmissionOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !allowed.OK() {
		t.Fatalf("expected admission to pass, got %#v", allowed.Failures)
	}
}

func TestWorkflowFinalVerificationAcceptedAttempt(t *testing.T) {
	dir := t.TempDir()
	finalRunRel, finalRun := workflowRunTestPath(t, dir, "wf", "final-run.json")
	mustWrite(t, finalRun, `{"ok":true}`+"\n")
	output, outputPath := workflowRunTestPath(t, dir, "wf", "final-verification.json")

	artifact, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:         dir,
		AttemptArtifacts: []string{finalRunRel},
		OutputArtifact:   output,
		WorkflowID:       "wf",
		ChangeSnapshot:   "snap",
	})
	if !result.OK() {
		t.Fatalf("expected final verification to pass, got %#v", result.Failures)
	}
	if artifact.Status != "PASS" || len(artifact.AcceptedAttempts) != 1 || len(artifact.Attempts) != 1 {
		t.Fatalf("unexpected aggregate: %#v", artifact)
	}
	var written WorkflowFinalVerificationArtifact
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatal(err)
	}
	if written.WorkflowID != "wf" || written.ChangeSnapshot != "snap" || written.Status != "PASS" {
		t.Fatalf("unexpected written artifact: %#v", written)
	}
	if strings.Contains(string(data), "generatedAt") {
		t.Fatalf("final verification artifact must be deterministic, got %s", string(data))
	}
}

func TestWorkflowFinalVerificationAcceptsEmptySuccessfulAttemptOutput(t *testing.T) {
	dir := t.TempDir()
	attemptRel, attemptPath := workflowRunTestPath(t, dir, "wf", "go-build.txt")
	mustWrite(t, attemptPath, "")
	output, _ := workflowRunTestPath(t, dir, "wf", "final-verification.json")

	artifact, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:         dir,
		AttemptArtifacts: []string{attemptRel},
		OutputArtifact:   output,
		WorkflowID:       "wf",
		ChangeSnapshot:   "snap",
	})
	if !result.OK() || artifact.Status != "PASS" || len(artifact.AcceptedAttempts) != 1 {
		t.Fatalf("expected empty successful output to pass, artifact=%#v failures=%#v", artifact, result.Failures)
	}
}

func TestWorkflowFinalVerificationRejectsFailedAttemptOutput(t *testing.T) {
	dir := t.TempDir()
	attemptRel, attemptPath := workflowRunTestPath(t, dir, "wf", "failed-command.txt")
	mustWrite(t, attemptPath, "FAIL: command exited with status 1\n")
	output, outputPath := workflowRunTestPath(t, dir, "wf", "final-verification.json")

	artifact, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:         dir,
		AttemptArtifacts: []string{attemptRel},
		OutputArtifact:   output,
		WorkflowID:       "wf",
		ChangeSnapshot:   "snap",
	})
	if result.OK() {
		t.Fatal("expected failed command output to be rejected")
	}
	if artifact.Status != "FAIL" || len(artifact.AcceptedAttempts) != 0 || len(artifact.Attempts) != 1 || artifact.Attempts[0].Status != "FAIL" || artifact.Attempts[0].Accepted {
		t.Fatalf("unexpected failed aggregate: %#v", artifact)
	}
	if !strings.Contains(resultSummary(result), "contains a FAIL result") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
	if !isFile(outputPath) {
		t.Fatal("expected deterministic failure artifact to be written")
	}
}

func TestWorkflowFinalVerificationWithoutRunDirRejectsRepositoryPaths(t *testing.T) {
	dir := t.TempDir()
	runAttempt, runAttemptPath := workflowRunTestPath(t, dir, "wf", "attempt.json")
	runOutput, _ := workflowRunTestPath(t, dir, "wf", "final-verification.json")
	mustWrite(t, runAttemptPath, `{"ok":true}`+"\n")
	rootAttempt := filepath.Join(dir, "attempt.json")
	mustWrite(t, rootAttempt, `{"ok":true}`+"\n")
	for _, test := range []struct {
		name    string
		options WorkflowFinalVerificationOptions
		want    string
	}{
		{name: "attempt artifact", options: WorkflowFinalVerificationOptions{AttemptArtifacts: []string{"attempt.json"}, OutputArtifact: runOutput}, want: "attempt-artifact"},
		{name: "output", options: WorkflowFinalVerificationOptions{AttemptArtifacts: []string{runAttempt}, OutputArtifact: "final-verification.json"}, want: "output"},
		{name: "final QA artifact", options: WorkflowFinalVerificationOptions{AttemptArtifacts: []string{runAttempt}, OutputArtifact: runOutput, FinalQAArtifact: "final-execution.json", RecordFinalQA: true}, want: "final-qa-artifact"},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.options.Worktree = dir
			test.options.WorkflowID = "wf"
			test.options.ChangeSnapshot = "snap"
			_, result := WorkflowFinalVerification(test.options)
			if result.OK() || !strings.Contains(resultSummary(result), test.want) {
				t.Fatalf("repository path was accepted: %#v", result.Failures)
			}
		})
	}
}

func TestWorkflowFinalVerificationRecordsFinalQA(t *testing.T) {
	dir := t.TempDir()
	runRel := filepath.ToSlash(filepath.Join(".gates", "runs", "wf"))
	finalRunRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-run.json"))
	finalRun := filepath.Join(dir, filepath.FromSlash(finalRunRel))
	mustWrite(t, finalRun, `{"ok":true}`+"\n")
	recordFourGatePrerequisites(t, dir, "wf", "snap")

	artifact, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:         dir,
		RunDir:           runRel,
		AttemptArtifacts: []string{finalRunRel},
		OutputArtifact:   filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json")),
		FinalQAArtifact:  filepath.ToSlash(filepath.Join(runRel, "restricted", "final-execution.md")),
		RecordFinalQA:    true,
		Actor:            "gate-workflow",
		WorkflowID:       "wf",
		ChangeSnapshot:   "snap",
	})
	if !result.OK() {
		t.Fatalf("expected final QA record to pass, got %#v", result.Failures)
	}
	if artifact.Status != "PASS" {
		t.Fatalf("expected PASS aggregate, got %#v", artifact)
	}
	state, show := GateShow(GateShowOptions{Worktree: dir, StatePath: filepath.ToSlash(filepath.Join(runRel, "restricted", "gate-state.json"))})
	if !show.OK() {
		t.Fatalf("expected gate state to show, got %#v", show.Failures)
	}
	entry := state.Gates["qa-test-gate"]
	if entry.Stage != "FinalExecution" || entry.Actor != "gate-workflow" || entry.Artifact != filepath.ToSlash(filepath.Join(runRel, "restricted", "final-execution.md")) {
		t.Fatalf("unexpected final QA gate entry: %#v", entry)
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(runRel), "restricted", "final-execution.md"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var payload FinalExecutionPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	policy, ok := fixedPolicy("FINAL_EXECUTION", envelope.Gate, envelope.Stage)
	if !ok || len(payload.GateMatrix) != len(policy.Prerequisites) {
		t.Fatalf("FinalExecution did not consume its typed policy: policy=%#v payload=%#v", policy, payload)
	}
	for i, prerequisite := range policy.Prerequisites {
		row := payload.GateMatrix[i]
		if row.Gate != prerequisite.Gate || row.ResultKind != "FRESH_PASS" || row.SourceSnapshot != "snap" || row.TargetSnapshot != "snap" || row.CarryDecision != nil {
			t.Fatalf("FinalExecution prerequisite %d drifted: policy=%#v row=%#v", i, prerequisite, payload.GateMatrix[i])
		}
	}
}

func TestWorkflowFinalVerificationBuildsMixedCarryMatrix(t *testing.T) {
	dir := t.TempDir()
	fixture := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:1])
	finalizeCarryTestArtifact(t, dir, fixture)
	if result := WorkflowRecordTransition(WorkflowRecordTransitionOptions{Worktree: dir, RunDir: fixture.RunDir, Artifact: fixture.Artifact, WorkflowID: "wf", ChangeSnapshot: "target"}); !result.OK() {
		t.Fatalf("accepted Carry transition failed: %#v", result.Failures)
	}
	for _, gate := range postDevelopmentGateOrder[1:] {
		artifact := writeGateArtifact(t, dir, gate, "", "wf", "target")
		if result := WorkflowRecordStage(WorkflowRecordStageOptions{Worktree: dir, Gate: gate, Verdict: "PASS", Artifact: artifact, WorkflowID: "wf", ChangeSnapshot: "target"}); !result.OK() {
			t.Fatalf("fresh %s record failed: %#v", gate, result.Failures)
		}
	}
	runRel := filepath.ToSlash(filepath.Join(".gates", "runs", "wf"))
	attemptRel, attemptPath := workflowRunTestPath(t, dir, "wf", "final-run.json")
	mustWrite(t, attemptPath, "{}\n")
	finalRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-execution.json"))
	closuresBefore, _ := filepath.Glob(filepath.Join(fixture.RunDir, "closures", "*.json"))
	_, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree: dir, RunDir: runRel, WorkflowID: "wf", ChangeSnapshot: "target", RecordFinalQA: true,
		AttemptArtifacts: []string{attemptRel},
		OutputArtifact:   filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json")), FinalQAArtifact: finalRel,
	})
	if !result.OK() {
		t.Fatalf("mixed FinalExecution failed: %#v", result.Failures)
	}
	data, err := os.ReadFile(resolvePath(dir, finalRel))
	if err != nil {
		t.Fatal(err)
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var payload FinalExecutionPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	carried := payload.GateMatrix[0]
	if carried.ResultKind != "CARRIED_PASS" || carried.SourceSnapshot != "source" || carried.TargetSnapshot != "target" || carried.CarryDecision == nil {
		t.Fatalf("unexpected carried row: %#v", carried)
	}
	for _, row := range payload.GateMatrix[1:] {
		if row.ResultKind != "FRESH_PASS" || row.SourceSnapshot != "target" || row.TargetSnapshot != "target" || row.CarryDecision != nil {
			t.Fatalf("unexpected fresh row: %#v", row)
		}
	}
	closuresAfter, _ := filepath.Glob(filepath.Join(fixture.RunDir, "closures", "*.json"))
	state, _ := GateShow(GateShowOptions{Worktree: dir, StatePath: filepath.ToSlash(filepath.Join(runRel, "restricted", "gate-state.json"))})
	if len(closuresAfter) != len(closuresBefore) || len(state.Transitions) != 1 {
		t.Fatalf("FinalExecution added a closure or another Carry review: closures=%d/%d transitions=%d", len(closuresBefore), len(closuresAfter), len(state.Transitions))
	}

	for _, test := range []struct {
		name   string
		mutate func(*FinalGateRow)
	}{
		{name: "source evidence", mutate: func(row *FinalGateRow) { row.GateEvidence = payload.GateMatrix[1].GateEvidence }},
		{name: "Carry closure", mutate: func(row *FinalGateRow) { row.CarryDecision.SHA256 = strings.Repeat("a", 64) }},
		{name: "target", mutate: func(row *FinalGateRow) { row.TargetSnapshot = "other" }},
		{name: "result kind", mutate: func(row *FinalGateRow) { row.ResultKind = "FRESH_PASS" }},
	} {
		t.Run("rejects mismatched "+test.name, func(t *testing.T) {
			var mutated FormalGateEvidence
			if err := strictContractJSON(data, &mutated); err != nil {
				t.Fatal(err)
			}
			var rows FinalExecutionPayload
			if err := strictContractJSON(mutated.Payload, &rows); err != nil {
				t.Fatal(err)
			}
			test.mutate(&rows.GateMatrix[0])
			mutated.Payload, _ = json.Marshal(rows)
			path := filepath.Join(fixture.RunDir, "invalid-final.json")
			writeJSONTest(t, path, mutated)
			invalid := Artifact(ArtifactOptions{Root: dir, RunDir: fixture.RunDir, File: relativePath(dir, path), Gate: "qa-test-gate", Stage: "FinalExecution", Flow: "finalization", WorkflowID: "wf", ChangeSnapshot: "target"})
			if invalid.OK() {
				t.Fatal("mismatched carried row was accepted")
			}
		})
	}
}

func TestWorkflowFinalVerificationWriteFailureDoesNotRecordFinalExecution(t *testing.T) {
	dir := t.TempDir()
	finalRunRel, finalRun := workflowRunTestPath(t, dir, "wf", "final-run.json")
	mustWrite(t, finalRun, `{"ok":true}`+"\n")
	finalExecutionRel, finalExecution := workflowRunTestPath(t, dir, "wf", "final-execution.json")
	output, outputPath := workflowRunTestPath(t, dir, "wf", "final-verification.json")
	if err := os.Mkdir(outputPath, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(outputPath, "sentinel"), "preserved\n")
	mustWrite(t, finalExecution, "previous FinalExecution\n")

	_, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:         dir,
		AttemptArtifacts: []string{finalRunRel},
		OutputArtifact:   output,
		FinalQAArtifact:  finalExecutionRel,
		RecordFinalQA:    true,
		WorkflowID:       "wf",
		ChangeSnapshot:   "snap",
	})
	if result.OK() || len(result.Failures) == 0 || result.Failures[0].Path != "output" {
		t.Fatalf("expected write failure, got %#v", result.Failures)
	}
	if data, err := os.ReadFile(finalExecution); err != nil || string(data) != "previous FinalExecution\n" {
		t.Fatalf("write failure changed FinalExecution: data=%q err=%v", data, err)
	}
}

func TestRecordFinalQARollsBackArtifactWhenStateRecordingFails(t *testing.T) {
	for _, previousExists := range []bool{true, false} {
		name := "removes newly created artifact"
		if previousExists {
			name = "restores previous artifact"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			runRel := filepath.ToSlash(filepath.Join(".gates", "runs", "wf"))
			finalRunRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-run.json"))
			finalRun := filepath.Join(dir, filepath.FromSlash(finalRunRel))
			mustWrite(t, finalRun, `{"ok":true}`+"\n")
			recordFourGatePrerequisites(t, dir, "wf", "snap")
			finalVerification := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json"))
			_, verificationResult := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
				Worktree:         dir,
				RunDir:           runRel,
				AttemptArtifacts: []string{finalRunRel},
				OutputArtifact:   finalVerification,
				WorkflowID:       "wf",
				ChangeSnapshot:   "snap",
			})
			if !verificationResult.OK() {
				t.Fatal(verificationResult.Failures)
			}
			finalExecutionRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-execution.json"))
			finalExecution := filepath.Join(dir, filepath.FromSlash(finalExecutionRel))
			if previousExists {
				mustWrite(t, finalExecution, "previous FinalExecution\n")
			}
			recordCalled := false
			runDir, _ := resolveWorkflowRunDir(dir, "wf", runRel)
			if previousExists {
				result := recordFinalQAWith(dir, runDir, finalVerification, "PASS", WorkflowFinalVerificationOptions{
					FinalQAArtifact: finalExecutionRel,
					WorkflowID:      "wf",
					ChangeSnapshot:  "snap",
				}, func(GateRecordOptions) Result {
					recordCalled = true
					return Result{}
				})
				if result.OK() || recordCalled {
					t.Fatalf("existing FinalExecution was overwritten or recorded: called=%v failures=%#v", recordCalled, result.Failures)
				}
				data, err := os.ReadFile(finalExecution)
				if err != nil || string(data) != "previous FinalExecution\n" {
					t.Fatalf("existing FinalExecution changed after rejection: data=%q err=%v", data, err)
				}
				return
			}
			result := recordFinalQAWith(dir, runDir, finalVerification, "PASS", WorkflowFinalVerificationOptions{
				FinalQAArtifact: finalExecutionRel,
				WorkflowID:      "wf",
				ChangeSnapshot:  "snap",
			}, func(GateRecordOptions) Result {
				recordCalled = true
				data, err := os.ReadFile(finalExecution)
				if err != nil || strings.Contains(string(data), "previous FinalExecution") {
					t.Fatalf("state recording did not observe generated FinalExecution: data=%q err=%v", data, err)
				}
				var failed Result
				failed.add("gate-state", "forced state recording failure")
				return failed
			})
			if result.OK() || !recordCalled || !strings.Contains(resultSummary(result), "forced state recording failure") {
				t.Fatalf("expected state recording failure, got called=%v failures=%#v", recordCalled, result.Failures)
			}
			data, err := os.ReadFile(finalExecution)
			if !os.IsNotExist(err) {
				t.Fatalf("new FinalExecution was not removed: data=%q err=%v", data, err)
			}
		})
	}
}

func TestWriteFileAtomicCreatesReplacesAndPreservesFailedDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := writeFileAtomic(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "second\n" {
		t.Fatalf("atomic replacement was incomplete: data=%q err=%v", data, err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(blocked, "sentinel")
	if err := os.WriteFile(sentinel, []byte("preserved\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(blocked, []byte("replacement\n"), 0o600); err == nil {
		t.Fatal("expected replacement of a non-empty directory to fail")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "preserved\n" {
		t.Fatalf("failed replacement changed prior destination state: data=%q err=%v", data, err)
	}
}

func TestWorkflowFinalVerificationRecordFinalQARequiresFourGatePrerequisites(t *testing.T) {
	dir := t.TempDir()
	finalRunRel, finalRun := workflowRunTestPath(t, dir, "wf", "final-run.json")
	output, _ := workflowRunTestPath(t, dir, "wf", "final-verification.json")
	finalQA, _ := workflowRunTestPath(t, dir, "wf", "final-execution.md")
	mustWrite(t, finalRun, `{"ok":true}`+"\n")

	_, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:         dir,
		AttemptArtifacts: []string{finalRunRel},
		OutputArtifact:   output,
		FinalQAArtifact:  finalQA,
		RecordFinalQA:    true,
		Actor:            "gate-workflow",
		WorkflowID:       "wf",
		ChangeSnapshot:   "snap",
	})
	if result.OK() {
		t.Fatal("expected FinalExecution record to require four gate prerequisites")
	}
}

func TestWorkflowFinalVerificationRecordFinalQARequiresGateClosures(t *testing.T) {
	dir := t.TempDir()
	finalRunRel, finalRun := workflowRunTestPath(t, dir, "wf", "final-run.json")
	output, _ := workflowRunTestPath(t, dir, "wf", "final-verification.json")
	finalQA, _ := workflowRunTestPath(t, dir, "wf", "final-qa-execution.md")
	mustWrite(t, finalRun, `{"ok":true}`+"\n")

	_, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:         dir,
		AttemptArtifacts: []string{finalRunRel},
		OutputArtifact:   output,
		FinalQAArtifact:  finalQA,
		RecordFinalQA:    true,
		WorkflowID:       "wf",
		ChangeSnapshot:   "snap",
	})
	if result.OK() {
		t.Fatal("expected missing final QA artifact to fail")
	}
	if !strings.Contains(result.Failures[0].Message, "missing current-snapshot PASS closure") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestWorkflowFinalVerificationMissingAcceptedArtifactFails(t *testing.T) {
	dir := t.TempDir()
	missing, _ := workflowRunTestPath(t, dir, "wf", "missing.json")
	output, outputPath := workflowRunTestPath(t, dir, "wf", "final-verification.json")

	artifact, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:         dir,
		AttemptArtifacts: []string{missing},
		OutputArtifact:   output,
		WorkflowID:       "wf",
		ChangeSnapshot:   "snap",
	})
	if result.OK() {
		t.Fatal("expected missing accepted artifact to fail")
	}
	if artifact.Status != "FAIL" {
		t.Fatalf("expected failed aggregate, got %#v", artifact)
	}
	if !strings.Contains(result.Failures[0].Message, "does not exist") {
		t.Fatalf("expected missing artifact failure, got %#v", result.Failures)
	}
	if !isFile(outputPath) {
		t.Fatal("expected deterministic failure artifact to be written")
	}
}

func TestWorkflowFinalVerificationNoAcceptedFails(t *testing.T) {
	dir := t.TempDir()
	output, _ := workflowRunTestPath(t, dir, "wf", "final-verification.json")

	_, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:         dir,
		AttemptArtifacts: []string{},
		OutputArtifact:   output,
		WorkflowID:       "wf",
		ChangeSnapshot:   "snap",
	})
	if result.OK() {
		t.Fatal("expected no accepted attempts to fail")
	}
}

func TestWorkflowCleanupDryRunExecuteAndDeny(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, ".gates", "tmp", "post-development-1", "scratch.txt")
	closure := filepath.Join(dir, ".gates", "runs", "sealed", "restricted", "final-execution.json")
	mustWrite(t, tmpFile, "scratch\n")
	mustWrite(t, closure, "formal closure\n")

	dryRun, result := WorkflowCleanup(WorkflowCleanupOptions{Worktree: dir})
	if !result.OK() {
		t.Fatalf("expected cleanup dry-run to pass, got %#v", result.Failures)
	}
	if !dryRun.DryRun || len(dryRun.Paths) != 1 || dryRun.Paths[0].Status != "would-remove" {
		t.Fatalf("unexpected dry-run report: %#v", dryRun)
	}
	if !isFile(tmpFile) {
		t.Fatal("dry-run removed scratch file")
	}

	_, denied := WorkflowCleanup(WorkflowCleanupOptions{Worktree: dir, FlowID: "../outside"})
	if denied.OK() {
		t.Fatal("expected invalid flow id to be denied")
	}

	executed, result := WorkflowCleanup(WorkflowCleanupOptions{Worktree: dir, FlowID: "post-development-1", Execute: true})
	if !result.OK() {
		t.Fatalf("expected cleanup execute to pass, got %#v", result.Failures)
	}
	if executed.DryRun || len(executed.Paths) != 1 || executed.Paths[0].Status != "removed" {
		t.Fatalf("unexpected execute report: %#v", executed)
	}
	if exists(tmpFile) {
		t.Fatal("execute did not remove scratch file")
	}
	if !isFile(closure) {
		t.Fatal("cleanup removed formal closure evidence")
	}

	remainingTmp := filepath.Join(dir, ".gates", "tmp", "pre-development-1", "scratch.txt")
	mustWrite(t, remainingTmp, "scratch\n")
	executed, result = WorkflowCleanup(WorkflowCleanupOptions{Worktree: dir, Execute: true})
	if !result.OK() || len(executed.Paths) != 1 || executed.Paths[0].Status != "removed" {
		t.Fatalf("expected temporary root cleanup, report=%#v failures=%#v", executed, result.Failures)
	}
	if exists(remainingTmp) || !isFile(closure) {
		t.Fatal("temporary-root cleanup did not preserve the sealed closure")
	}
}

func recordFourGatePrerequisites(t *testing.T, dir, workflowID, snapshot string) {
	t.Helper()
	for _, item := range []struct {
		gate  string
		stage string
		mode  string
	}{
		{gate: "qa-test-gate", stage: "Execution", mode: "formal"},
		{gate: "complexity-gate"},
		{gate: "architecture-health-gate"},
		{gate: "code-quality-gate"},
	} {
		artifact := writeGateArtifact(t, dir, item.gate, item.stage, workflowID, snapshot)
		result := WorkflowRecordStage(WorkflowRecordStageOptions{
			Worktree:       dir,
			Gate:           item.gate,
			Verdict:        "PASS",
			Mode:           item.mode,
			Stage:          item.stage,
			Artifact:       artifact,
			Actor:          item.gate,
			WorkflowID:     workflowID,
			ChangeSnapshot: snapshot,
		})
		if !result.OK() {
			t.Fatalf("expected prerequisite %s to record, got %#v", item.gate, result.Failures)
		}
	}
}

func workflowRunTestPath(t *testing.T, worktree, workflowID, name string) (string, string) {
	t.Helper()
	rel := filepath.ToSlash(filepath.Join(".gates", "runs", workflowID, "restricted", name))
	path := filepath.Join(worktree, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return rel, path
}
