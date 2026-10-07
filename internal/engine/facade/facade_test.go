package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/testkit"
)

func testReceipt() IntakeConfirmationReceipt {
	return IntakeConfirmationReceipt{
		Source:            DefaultIntakeSource,
		Authority:         DefaultIntakeAuthority,
		Transport:         DefaultIntakeTransport,
		RequirementSource: "requirements.md", RequirementRevision: fmt.Sprintf("%x", sha256.Sum256([]byte(validationDocument))),
		Artifacts:        []IntakeArtifact{{Path: "requirements.md", Revision: fmt.Sprintf("%x", sha256.Sum256([]byte(validationDocument)))}},
		SolutionRevision: "solution-rev", SolutionDigest: "sha256:solution",
	}
}

func TestLightweightVerticalLoopAndReplay(t *testing.T) {
	root := t.TempDir()
	request := StartRequest{
		RunID: "lightweight-replay", Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}
	f, run, err := Start(StartOptions{Root: root, Request: request, Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
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
	if _, err := os.Stat(filepath.Join(stateDir, "state.json.intent")); !os.IsNotExist(err) {
		t.Fatalf("terminal drive retained protocol file state.json.intent: %v", err)
	}
	// write.lock 是持久的锁工件（内核文件锁，永不删除），保留是预期。
	if _, err := os.Stat(filepath.Join(stateDir, "write.lock")); err != nil {
		t.Fatalf("terminal drive lost the persistent lock file: %v", err)
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
	}, Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
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
	admission := testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)
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
	if err := writeJSONAtomic(startIntentPath(root, runID), startIntent{RunID: runID, Admission: *testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)}); err != nil {
		t.Fatal(err)
	}
	f, run, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID: runID, Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}, Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
	if err != nil || f == nil || run.Status != "ACTIVE" {
		t.Fatalf("recovered start = facade=%v run=%+v err=%v", f != nil, run, err)
	}
	if _, err := os.Stat(startIntentPath(root, runID)); !os.IsNotExist(err) {
		t.Fatalf("start intent remained after successful start: %v", err)
	}
}

// TestCompletedRunEvidenceSurvivesSameIDStartRetry：run 已完成（终态清理删
// state.json、留不可变 terminal-summary.json）后同 ID Start 重试不得把终态
// 当半启动清除——可区分 already-exists 拒绝、终态摘要字节不变、Open 仍可
// 用、过期 intent 被清。
func TestCompletedRunEvidenceSurvivesSameIDStartRetry(t *testing.T) {
	root := t.TempDir()
	options := StartOptions{Root: root, Request: StartRequest{
		RunID: "summary-retention", Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}, Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)}
	f, _, err := Start(options)
	if err != nil {
		t.Fatalf("initial start: %v", err)
	}
	run, err := f.Drive()
	if err != nil || run.Status != "COMPLETE" {
		t.Fatalf("drive to completion = %+v err=%v", run, err)
	}
	summaryPath := filepath.Join(root, EngineNamespace, run.RunID, "terminal-summary.json")
	before, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	// 重造 start intent：代表成功启动后、清理 intent 前中断所遗留的持久盘
	// 面（该窗口内 state.json 已写、run 已成立；终态清理只删 state，不清
	// 此残留）。
	if err := writeJSONAtomic(startIntentPath(root, run.RunID), startIntent{RunID: run.RunID, Admission: f.admission}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Start(options); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("same-ID start on the completed run = %v", err)
	}
	after, readErr := os.ReadFile(summaryPath)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatal("same-ID start retry destroyed or rewrote the completed run's terminal evidence")
	}
	if _, err := Open(root, run.RunID); err != nil {
		t.Fatalf("open after the rejected retry: %v", err)
	}
	if _, err := os.Stat(startIntentPath(root, run.RunID)); !os.IsNotExist(err) {
		t.Fatal("stale start intent survived the rejected retry")
	}
}

func TestStartHonorsAdmissionLockAndRechecksGeneration(t *testing.T) {
	root := t.TempDir()
	admission := testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)
	unlock, err := coordination.AcquirePath(admission.RegistryPath+".lock", "admission")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, _, err := Start(StartOptions{Root: root, Request: StartRequest{
			RunID: "admission-lock", Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
			DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
		}, Admission: admission})
		result <- err
	}()
	select {
	case err := <-result:
		unlock()
		t.Fatalf("start did not wait for its admission window: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	if _, statErr := os.Stat(filepath.Join(root, EngineNamespace, "admission-lock")); !os.IsNotExist(statErr) {
		unlock()
		t.Fatalf("admission-locked start created state: %v", statErr)
	}
	data, err := os.ReadFile(admission.RegistryPath)
	if err != nil {
		unlock()
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		unlock()
		t.Fatal(err)
	}
	document["records"].([]any)[0].(map[string]any)["generation"] = admission.Generation + 1
	if err := writeJSONAtomic(admission.RegistryPath, document); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	select {
	case err := <-result:
		if !errors.Is(err, errStaleFencingToken) {
			t.Fatalf("stale admission error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("start admission window did not release")
	}
	if _, err := os.Stat(startIntentPath(root, "admission-lock")); !os.IsNotExist(err) {
		t.Fatalf("stale start left intent: %v", err)
	}
}

func TestDriveRecoversTerminalCleanupIntentBeforePublishingSummary(t *testing.T) {
	root := t.TempDir()
	runID := "recover-terminal-intent"
	f, _, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID: runID, Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}, Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
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
	checkWaiting := func(reader *Facade) {
		t.Helper()
		for _, read := range []func() (Run, error){reader.Show, reader.Status} {
			current, err := read()
			if err != nil || current.Status != "FINALIZING_CLEANUP" || current.Next.Kind != decision.KindWait {
				t.Fatalf("unpublished terminal: %+v %v", current, err)
			}
		}
		next, err := reader.Next()
		if err != nil || next.Kind != decision.KindWait {
			t.Fatalf("unpublished Next: %+v %v", next, err)
		}
	}
	checkWaiting(f)
	if _, err := os.Stat(terminalIntentPath(f.stateDir)); !os.IsNotExist(err) {
		t.Fatal("read created cleanup intent")
	}
	if err := writeJSONAtomic(terminalIntentPath(f.stateDir), summaryForRun(run, ownerInterval{})); err != nil {
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
	checkWaiting(opened)
	if _, err := os.Stat(terminalSummaryPath(f.stateDir)); !os.IsNotExist(err) {
		t.Fatal("read published terminal summary")
	}
	if _, err := os.Stat(filepath.Join(f.stateDir, "state.json")); !os.IsNotExist(err) {
		t.Fatal("read recreated active state")
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
	right.Artifacts = []IntakeArtifact{{Path: "design.md", Revision: "design-rev"}, {Path: "requirements.md", Revision: left.RequirementRevision}}
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
	}, Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
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
	}, Admission: testAdmission(t, badRoot, "target-1", "sha256:package", DefaultProvider)})
	if err == nil || !strings.Contains(err.Error(), InvalidIntakeConfirmation) {
		t.Fatalf("bad solution digest error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(badRoot, EngineNamespace)); !os.IsNotExist(statErr) {
		t.Fatalf("bad solution digest created engine namespace: %v", statErr)
	}
}

// newEngineRouteFacade 在包内直接构建 engine 路由的 façade（Start 在
// phase 3 只接 lightweight 路由；屏障投影的正路径是 engine 路由的
// Show/Status/Next/Drive）。
func newEngineRouteFacade(t *testing.T, runID string) *Facade {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, EngineNamespace, runID)
	admission := testAdmission(t, root, "target-1", "sha256:package", "provider.test")
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	store, err := persistence.NewStore(stateDir, persistence.Config{
		PackageDigest: "sha256:package", DefinitionSource: DefaultDefinitionSource, InstalledTargetIdentity: "target-1", AcquireAdmission: admission.acquire,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	engine, err := protocol.New(store, protocol.Config{Definition: cd, Registry: definition.Registry(), Capacity: 4}, nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	confirmation := &protocol.IntakeConfirmationReceipt{
		Authority: protocol.IntakeAuthorityStableDriver, Transport: protocol.IntakeTransportStableLauncher,
		Source: "stable-driver", RequirementSource: "requirement.md", RequirementRevision: "rev-1",
		Artifacts:        []protocol.IntakeArtifact{{Path: "requirement.md", Revision: "r1"}},
		SolutionRevision: "s1", SolutionDigest: "sha256:solution",
	}
	fp, err := engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if err := engine.InitWithMetadata(view, "provider.test", fp, runID, "", confirmation, "", "", nil); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := engine.RecordIntakeReceipt(protocol.IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:intake"}, fp); err != nil {
		t.Fatalf("intake receipt: %v", err)
	}
	f := &Facade{
		root: root, runID: runID, stateDir: stateDir, metadata: filepath.Join(stateDir, "request.json"),
		store: store, engine: engine, envelope: Envelope{}, definition: cd, capacity: 4, admission: *admission,
	}
	release, err := coordination.AcquirePath(f.promotionLockPath(), promotionLockDescription)
	if err != nil {
		t.Fatal(err)
	}
	release()
	return f
}

// TestFacadeProjectsThePendingRequirementAsk：修订屏障在位时，未决需求变
// 化 Ask 即 run 的当前外部边界——Show/Status/Next 呈 ask 面与确认包而不
// 报错，Drive 冻结签发也不报错（等待用户而非错误）；Ask 解决后投影恢复
// 常规边界。
func TestFacadeProjectsThePendingRequirementAsk(t *testing.T) {
	f := newEngineRouteFacade(t, "pending-ask-face")
	pkg := protocol.RequirementDecisionPackage{
		CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-face", ImpactDigest: "sha256:impact-face",
		Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved,
	}
	askEvent, err := protocol.NewRequirementRequestEvent("req-face-ask", pkg,
		protocol.AskOption{ID: "proceed", Label: "确认变更包"}, protocol.AskOption{ID: "abort", Label: "放弃变更"})
	if err != nil {
		t.Fatalf("ask event: %v", err)
	}
	if _, _, err := f.Submit(askEvent); err != nil {
		t.Fatalf("submit ask: %v", err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatalf("show during the barrier: %v", err)
	}
	if run.PendingRequirementAsk == nil {
		t.Fatal("the pending requirement ask was not projected")
	}
	face := run.PendingRequirementAsk
	if face.RequestID != "req-face-ask" || face.Package == nil || !reflect.DeepEqual(*face.Package, pkg) ||
		len(face.Options) != 2 || face.BaseRevision != "rev-1" {
		t.Fatalf("ask face = %+v", face)
	}
	if run.Next.Kind != decision.KindAsk || run.Next.Ask == nil {
		t.Fatalf("next during the barrier = %+v, want the ask boundary", run.Next)
	}
	if len(run.AvailableActions) != 1 || run.AvailableActions[0] != "submit" {
		t.Fatalf("available actions during the barrier = %v", run.AvailableActions)
	}
	if statusRun, err := f.Status(); err != nil || statusRun.PendingRequirementAsk == nil {
		t.Fatalf("status during the barrier = %v %v", statusRun.PendingRequirementAsk, err)
	}
	if next, err := f.Next(); err != nil || next.Kind != decision.KindAsk {
		t.Fatalf("next during the barrier = %+v %v", next, err)
	}
	// 屏障期 Drive：签发冻结＝等待用户，不是错误。
	driven, err := f.Drive()
	if err != nil {
		t.Fatalf("drive during the barrier: %v", err)
	}
	if driven.PendingRequirementAsk == nil {
		t.Fatal("drive during the barrier lost the ask face")
	}
	// 放弃变更：屏障解除，投影恢复常规边界。
	token, err := f.Freshness("req-face-ask")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	abort, err := protocol.NewDecideEvent("evt-face-ask", "req-face-ask", token, "abort")
	if err != nil {
		t.Fatalf("abort event: %v", err)
	}
	if _, lifted, err := f.Submit(abort); err != nil || lifted.PendingRequirementAsk != nil {
		t.Fatalf("abort lifted the barrier: %v %+v", err, lifted.PendingRequirementAsk)
	}
}

// TestFacadeProjectsEveryPendingAsk：泛化 pending-ask 面覆盖一切未决受
// 限控制 Ask（requestID/control/options/即时 token，按 requestID 排序）
// ——非需求变化的 Ask 同样可见；携包特化面只留给需求变化 Ask。
func TestFacadeProjectsEveryPendingAsk(t *testing.T) {
	f := newEngineRouteFacade(t, "pending-asks-face")
	options := []protocol.AskOption{{ID: "no-split", Label: "不拆分"}, {ID: "split", Label: "拆分"}}
	topology, err := protocol.NewRequestEvent("req-face-topology", protocol.ControlConfirmTopology, options...)
	if err != nil {
		t.Fatalf("topology ask event: %v", err)
	}
	if _, _, err := f.Submit(topology); err != nil {
		t.Fatalf("submit topology ask: %v", err)
	}
	grant, err := protocol.NewRequestEvent("req-face-grant", protocol.ControlReviewRoundGrant,
		protocol.AskOption{ID: "grant", Label: "授权恰好一轮"}, protocol.AskOption{ID: "decline", Label: "拒绝"})
	if err != nil {
		t.Fatalf("grant ask event: %v", err)
	}
	if _, _, err := f.Submit(grant); err != nil {
		t.Fatalf("submit grant ask: %v", err)
	}

	run, err := f.Show()
	if err != nil {
		t.Fatalf("show with pending asks: %v", err)
	}
	if len(run.PendingAsks) != 2 {
		t.Fatalf("pending ask faces = %+v, want both asks", run.PendingAsks)
	}
	// 确定性输出：按 requestID 排序。
	if run.PendingAsks[0].RequestID != "req-face-grant" || run.PendingAsks[1].RequestID != "req-face-topology" {
		t.Fatalf("pending ask faces are not sorted: %+v", run.PendingAsks)
	}
	topologyFace := run.PendingAsks[1]
	if topologyFace.Control != protocol.ControlConfirmTopology || len(topologyFace.Options) != 2 ||
		topologyFace.Options[0].ID != options[0].ID || topologyFace.Options[1].ID != options[1].ID {
		t.Fatalf("topology ask face = %+v", topologyFace)
	}
	grantFace := run.PendingAsks[0]
	if grantFace.Control != protocol.ControlReviewRoundGrant || len(grantFace.Options) != 2 {
		t.Fatalf("grant ask face = %+v", grantFace)
	}
	for _, face := range run.PendingAsks {
		token, err := f.Freshness(face.RequestID)
		if err != nil || token == "" || face.Token != token {
			t.Fatalf("ask face token = %q, freshness = %q err=%v (request %s)", face.Token, token, err, face.RequestID)
		}
	}
	if run.PendingRequirementAsk != nil {
		t.Fatal("non-requirement asks must not populate the package-carrying face")
	}
	if statusRun, err := f.Status(); err != nil || len(statusRun.PendingAsks) != 2 {
		t.Fatalf("status with pending asks = %d %v", len(statusRun.PendingAsks), err)
	}
}

// testAdmission supplies the same immutable registry/bootstrap contract used by
// installed runs. Only the installed payload and VCS/host execution are fixtures.
func testAdmission(t *testing.T, root, recordID, packageDigest, provider string) *Admission {
	t.Helper()
	admissionFixtureMutex.Lock()
	defer admissionFixtureMutex.Unlock()
	key := strings.Join([]string{root, recordID, provider}, "\x00")
	if a := admissionFixtures[key]; a != nil {
		return a
	}
	registryPath, target, generation, lease, token := testkit.Installation(t, root, recordID, packageDigest, provider)

	source, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"prompts", "gates"} {
		err := filepath.WalkDir(filepath.Join(source, directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, _ := filepath.Rel(source, path)
			destination := filepath.Join(target, relative)
			if entry.IsDir() {
				return os.MkdirAll(destination, 0700)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(destination, body, 0600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Unit workflows issue this declared custom gate from their private catalog.
	quality, err := os.ReadFile(filepath.Join(source, "gates", "implementation-quality-gate.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range []string{"quality", "security", "performance"} {
		if err := os.WriteFile(filepath.Join(target, "gates", gate+".md"), quality, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := &Admission{RegistryPath: registryPath, Target: target, Root: root,
		Provider: provider, PackageDigest: packageDigest, InstalledTargetIdentity: recordID,
		Generation: generation, Lease: lease, Token: token}
	admissionFixtures[key] = a
	t.Cleanup(func() {
		admissionFixtureMutex.Lock()
		defer admissionFixtureMutex.Unlock()
		delete(admissionFixtures, key)
	})
	return a
}

var admissionFixtures = map[string]*Admission{}
var admissionFixtureMutex sync.Mutex
