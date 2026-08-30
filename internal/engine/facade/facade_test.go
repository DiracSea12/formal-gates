package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
)

func testReceipt() IntakeConfirmationReceipt {
	return IntakeConfirmationReceipt{
		Source:            DefaultIntakeSource,
		Authority:         DefaultIntakeAuthority,
		Transport:         DefaultIntakeTransport,
		RequirementSource: "requirements.md", RequirementRevision: "req-rev",
		Artifacts:        []IntakeArtifact{{Path: "requirements.md", Revision: "req-rev"}},
		SolutionRevision: "solution-rev", SolutionDigest: "sha256:solution",
	}
}

func TestLightweightVerticalLoopAndReplay(t *testing.T) {
	root := t.TempDir()
	request := StartRequest{
		RunID: "lightweight-replay", Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}
	f, run, err := Start(StartOptions{Root: root, Request: request, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if run.Status != "ACTIVE" || run.IntakeReceipt != nil {
		t.Fatalf("initial projection = %+v", run)
	}
	completed, err := f.Drive()
	if err != nil {
		t.Fatalf("drive: %v", err)
	}
	if completed.Status != "COMPLETE" || !completed.Unverified || string(completed.Next.Kind) != "COMPLETE" {
		t.Fatalf("terminal projection = %+v", completed)
	}
	stateDir := filepath.Join(root, EngineNamespace, request.RunID)
	statePath := filepath.Join(stateDir, "state.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("terminal drive retained active state: %v", err)
	}
	for _, name := range []string{"state.json.intent", "write.lock"} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); !os.IsNotExist(err) {
			t.Fatalf("terminal drive retained protocol file %s: %v", name, err)
		}
	}
	if temps, err := filepath.Glob(filepath.Join(stateDir, ".state.json.*.tmp")); err != nil {
		t.Fatal(err)
	} else if len(temps) != 0 {
		t.Fatalf("terminal drive retained protocol temp files: %v", temps)
	}
	second, err := f.Drive()
	if err != nil {
		t.Fatalf("terminal replay drive: %v", err)
	}
	if second.Revision != completed.Revision {
		t.Fatalf("terminal replay mutated state: first=%d second=%d", completed.Revision, second.Revision)
	}
	var summary TerminalSummary
	data, err := os.ReadFile(filepath.Join(root, EngineNamespace, request.RunID, "terminal-summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if !summary.Unverified || string(summary.Next.Kind) != "COMPLETE" {
		t.Fatalf("summary = %+v", summary)
	}
	firstSummary := string(data)
	if _, err := f.Drive(); err != nil {
		t.Fatalf("second terminal replay drive: %v", err)
	}
	secondSummary, err := os.ReadFile(filepath.Join(root, EngineNamespace, request.RunID, "terminal-summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(secondSummary) != firstSummary {
		t.Fatal("terminal summary changed during replay")
	}
	readOnly, err := f.Show()
	if err != nil || readOnly.Status != "COMPLETE" || string(readOnly.Next.Kind) != "COMPLETE" {
		t.Fatalf("summary-only terminal replay = %+v err=%v", readOnly, err)
	}
}

func TestStartRejectsInvalidReceiptBeforeStateWrite(t *testing.T) {
	root := t.TempDir()
	receipt := testReceipt()
	receipt.RequirementRevision = ""
	_, _, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID:            "invalid-intake",
		DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest,
		IntakeConfirmationReceipt: receipt,
	}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1"}})
	if err == nil || err.Error() == "" || !strings.Contains(err.Error(), InvalidIntakeConfirmation) {
		t.Fatalf("invalid receipt error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, EngineNamespace)); !os.IsNotExist(statErr) {
		t.Fatalf("invalid receipt created engine namespace: %v", statErr)
	}
}

func TestConcurrentStartSameRunIDHasSingleOwner(t *testing.T) {
	root := t.TempDir()
	request := StartRequest{
		RunID: "concurrent-start", Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}
	admission := &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1"}
	start := make(chan struct{})
	type result struct {
		f   *Facade
		run Run
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			f, run, err := Start(StartOptions{Root: root, Request: request, Admission: admission})
			results <- result{f: f, run: run, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var winner *Facade
	var failures int
	for item := range results {
		if item.err == nil {
			if winner != nil {
				t.Fatal("concurrent starts both reported success")
			}
			winner = item.f
			if item.run.Status != "ACTIVE" {
				t.Fatalf("winning start projection = %+v", item.run)
			}
			continue
		}
		failures++
		if !strings.Contains(item.err.Error(), "already exists") {
			t.Fatalf("losing concurrent start error = %v", item.err)
		}
	}
	if winner == nil || failures != 1 {
		t.Fatalf("concurrent starts yielded winner=%v failures=%d", winner != nil, failures)
	}
	completed, err := winner.Drive()
	if err != nil || completed.Status != "COMPLETE" {
		t.Fatalf("winning run after loser rejection = %+v err=%v", completed, err)
	}
}

func TestStartRecoversIncompleteStartIntent(t *testing.T) {
	root := t.TempDir()
	runID := "recover-start-intent"
	stateDir := filepath.Join(root, EngineNamespace, runID)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "partial-state"), []byte("incomplete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(startIntentPath(root, runID), startIntent{RunID: runID}); err != nil {
		t.Fatal(err)
	}
	f, run, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID: runID, Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1"}})
	if err != nil || f == nil || run.Status != "ACTIVE" {
		t.Fatalf("recovered start = facade=%v run=%+v err=%v", f != nil, run, err)
	}
	if _, err := os.Stat(startIntentPath(root, runID)); !os.IsNotExist(err) {
		t.Fatalf("start intent remained after successful start: %v", err)
	}
}

func TestStartHonorsAdmissionLockAndRechecksGeneration(t *testing.T) {
	root := t.TempDir()
	registry := filepath.Join(root, "registry.json")
	registryData := `{"records":[{"id":"target-1","status":"active","packageDigest":"sha256:package","generation":2,"lease":"lease-2","token":"token-2"}]}`
	if err := os.WriteFile(registry, []byte(registryData), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := coordination.AcquirePath(registry+".lock", "admission")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Start(StartOptions{Root: root, Request: StartRequest{
		RunID: "admission-lock", Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}, Admission: &Admission{RegistryPath: registry, PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1", Generation: 2, Lease: "lease-2", Token: "token-2"}})
	unlock()
	if err == nil || !strings.Contains(err.Error(), "admission lock held") {
		t.Fatalf("start under admission lock error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, EngineNamespace, "admission-lock")); !os.IsNotExist(statErr) {
		t.Fatalf("admission-locked start created state: %v", statErr)
	}
	if err := os.WriteFile(registry, []byte(strings.Replace(registryData, `"generation":2`, `"generation":3`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = Start(StartOptions{Root: root, Request: StartRequest{
		RunID: "admission-generation", Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}, Admission: &Admission{RegistryPath: registry, PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1", Generation: 2, Lease: "lease-2", Token: "token-2"}})
	if err == nil || !strings.Contains(err.Error(), "admission changed") {
		t.Fatalf("stale admission error = %v", err)
	}
}

func TestDriveRecoversTerminalCleanupIntentBeforePublishingSummary(t *testing.T) {
	root := t.TempDir()
	runID := "recover-terminal-intent"
	f, _, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID: runID, Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	receipt := testReceipt()
	digest, err := IntakeDigest(receipt)
	if err != nil {
		t.Fatalf("intake digest: %v", err)
	}
	if _, err := f.engine.RecordIntakeReceipt(protocol.IntakeReceipt{Confirmation: receipt, IntakeDigest: digest}, emptyFingerprint()); err != nil {
		t.Fatalf("record intake receipt: %v", err)
	}
	if _, err := f.engine.CompleteAll(emptyFingerprint()); err != nil {
		t.Fatalf("complete engine: %v", err)
	}
	run, err := f.project()
	if err != nil {
		t.Fatalf("project complete run: %v", err)
	}
	if err := writeJSONAtomic(terminalIntentPath(f.stateDir), summaryForRun(run)); err != nil {
		t.Fatalf("write terminal intent: %v", err)
	}
	pending, err := f.Show()
	if err != nil || pending.Status != "FINALIZING_CLEANUP" || pending.Next.Kind != decision.KindWait || pending.Next.Wait.Reason != decision.WaitEngineInternal {
		t.Fatalf("terminal cleanup projection = %+v err=%v", pending, err)
	}
	if err := f.store.CleanupTerminal(); err != nil {
		t.Fatalf("simulate cleanup: %v", err)
	}
	if _, err := os.Stat(terminalSummaryPath(f.stateDir)); !os.IsNotExist(err) {
		t.Fatalf("summary unexpectedly exists before recovery: %v", err)
	}
	opened, err := Open(root, runID)
	if err != nil {
		t.Fatalf("open terminal intent: %v", err)
	}
	recovered, err := opened.Drive()
	if err != nil || recovered.Status != "COMPLETE" || recovered.Next.Kind != decision.KindComplete {
		t.Fatalf("recovered terminal run = %+v err=%v", recovered, err)
	}
	if _, err := os.Stat(terminalIntentPath(f.stateDir)); !os.IsNotExist(err) {
		t.Fatalf("terminal intent remained after recovery: %v", err)
	}
	if _, err := os.Stat(terminalSummaryPath(f.stateDir)); err != nil {
		t.Fatalf("terminal summary missing after recovery: %v", err)
	}
}

func TestIntakeDigestCanonicalizesArtifactOrderAndIgnoresTimestamps(t *testing.T) {
	left := testReceipt()
	left.Artifacts = append(left.Artifacts, IntakeArtifact{Path: "design.md", Revision: "design-rev"})
	left.ConfirmedAt = "2026-01-01T00:00:00Z"
	right := left
	right.Artifacts = []IntakeArtifact{{Path: "design.md", Revision: "design-rev"}, {Path: "requirements.md", Revision: "req-rev"}}
	right.ConfirmedAt = "2027-01-01T00:00:00Z"
	a, err := IntakeDigest(left)
	if err != nil {
		t.Fatal(err)
	}
	b, err := IntakeDigest(right)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("canonical digest differs by artifact order/timestamp: %s vs %s", a, b)
	}
	changedAuthority := left
	changedAuthority.Authority = IntakeAuthority("other-driver")
	c, err := IntakeDigest(changedAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("canonical digest ignored authority binding")
	}
	changedTransport := left
	changedTransport.Transport = IntakeTransport("other-transport")
	d, err := IntakeDigest(changedTransport)
	if err != nil {
		t.Fatal(err)
	}
	if d == a {
		t.Fatal("canonical digest ignored transport binding")
	}
}

func TestStartCanonicalizesArtifactsAndChecksSolutionDigest(t *testing.T) {
	root := t.TempDir()
	requirement := []byte("# requirement\n")
	solution := []byte("# solution\n")
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), requirement, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "design.md"), solution, 0o600); err != nil {
		t.Fatal(err)
	}
	revision := func(data []byte) string {
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}
	receipt := IntakeConfirmationReceipt{
		Source: DefaultIntakeSource, Authority: DefaultIntakeAuthority, Transport: DefaultIntakeTransport,
		RequirementSource: "./requirements.md", RequirementRevision: revision(requirement),
		Artifacts: []IntakeArtifact{
			{Path: "./requirements.md", Revision: revision(requirement)},
			{Path: "design.md", Revision: revision(solution)},
		},
		SolutionRevision: revision(solution), SolutionDigest: "sha256:" + revision(solution),
	}
	f, _, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{
		RunID: "canonical-artifacts", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: receipt,
	}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1"}})
	if err != nil {
		t.Fatalf("start with valid normalized bindings: %v", err)
	}
	var request StartRequest
	data, err := os.ReadFile(filepath.Join(root, EngineNamespace, "canonical-artifacts", "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	if got := request.IntakeConfirmationReceipt.RequirementSource; got != "requirements.md" {
		t.Fatalf("canonical requirement source = %q", got)
	}
	if got := request.IntakeConfirmationReceipt.Artifacts[0].Path; got != "design.md" {
		t.Fatalf("canonical artifact order = %+v", request.IntakeConfirmationReceipt.Artifacts)
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	var summary TerminalSummary
	data, err = os.ReadFile(filepath.Join(root, EngineNamespace, "canonical-artifacts", "terminal-summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if got := summary.IntakeReceipt.Confirmation.Artifacts[0].Path; got != "design.md" {
		t.Fatalf("summary artifact order = %+v", summary.IntakeReceipt.Confirmation.Artifacts)
	}

	badRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(badRoot, "requirements.md"), requirement, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badRoot, "design.md"), solution, 0o600); err != nil {
		t.Fatal(err)
	}
	receipt.SolutionDigest = "sha256:" + fmt.Sprintf("%064d", 0)
	_, _, err = Start(StartOptions{Root: badRoot, ArtifactRoot: badRoot, Request: StartRequest{
		RunID: "bad-solution-digest", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: receipt,
	}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1"}})
	if err == nil || !strings.Contains(err.Error(), InvalidIntakeConfirmation) {
		t.Fatalf("bad solution digest error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(badRoot, EngineNamespace)); !os.IsNotExist(statErr) {
		t.Fatalf("bad solution digest created engine namespace: %v", statErr)
	}
}
