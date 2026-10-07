//go:build !windows

package harness

import (
	"bufio"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/host"
)

// Local answers replace only host transport. Installed CLI commands, issued
// workspaces, all command observations and every native VCS effect execute normally.
func TestCanaryInstalledGitFlows(t *testing.T) {
	for _, split := range []bool{true, false} {
		name := "split"
		if !split {
			name = "no-split"
		}
		t.Run(name, func(t *testing.T) { runCanaryInstalledFlow(t, "git", split, canaryProvider) })
	}
}

// Early cancellation retains actual native stop evidence before slice delivery.
func TestCanaryInstalledAbortBeforeSliceDelivery(t *testing.T) {
	runCanaryInstalledFlow(t, "git", true, canaryProvider, func(e *canaryLocalExecutor) { e.abortBeforeDelivery = true })
}

// A rejected case oracle must reopen design before the ordinary flag repair.
func TestCanaryInstalledCaseReviewRedesign(t *testing.T) {
	runCanaryInstalledFlow(t, "git", true, canaryProvider, func(e *canaryLocalExecutor) { e.rejectBadOracle = true })
}

func TestCanaryInstalledHostGitFlows(t *testing.T) {
	for _, nativeHost := range host.InstallableNames() {
		t.Run(nativeHost, func(t *testing.T) { runCanaryInstalledFlow(t, "git", false, nativeHost) })
	}
}

func runCanaryInstalledFlow(t *testing.T, provider string, split bool, nativeHost string, configure ...func(*canaryLocalExecutor)) {
	name := "split"
	if !split {
		name = "no-split"
	}
	if provider != "git" {
		name = filepath.Join(provider, name)
	}
	if nativeHost != canaryProvider {
		name = filepath.Join(nativeHost, name)
	}
	_, file, _, _ := runtime.Caller(0)
	source := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	home := t.TempDir()
	evidence := t.TempDir()
	if root := os.Getenv("FORMAL_GATES_CANARY_LOCAL_DIR"); root != "" {
		home = filepath.Join(root, name, "home")
		evidence = filepath.Join(root, name, "evidence")
		if err := os.MkdirAll(home, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if nativeHost == canaryProvider {
		writeCanaryHostFixture(t, home, "local-only-provider")
	}
	runtimeRoot := t.TempDir()
	script := filepath.Join(runtimeRoot, "glm", "zcode.cjs")
	bundled := filepath.Join(runtimeRoot, "config", "provider", "zcode-builtin.json")
	for _, path := range []string{script, bundled} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(script, []byte(canaryNativePathPeer), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, readTestFileBytes(t, zcodeTestCatalogue(t, "7")), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORMAL_GATES_ZCODE_SCRIPT", script)
	c := &engineCanary{t: t, nativeHost: nativeHost, localPeer: true, split: split, deadline: time.Now().Add(8 * time.Minute), repairAllowances: 1, caseArgs: map[string][]string{}, caseTests: map[string]string{}}
	if nativeHost != canaryProvider {
		nativeBinary := nativeHost
		if nativeHost == "cursor" {
			nativeBinary = "cursor-agent"
		}
		peerTest := map[string]string{"claude": "TestClaudeCLI", "codex": "TestCodexRPCPeer", "cursor": "TestCursorCLI", "dsh": "TestScriptedPeer"}[nativeHost]
		if err := os.MkdirAll(filepath.Join(home, ".local", "bin"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".local", "bin", nativeBinary), []byte(canaryPeerWrapper(peerTest, "", "")), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.prepareHome(home, t.TempDir(), source); err != nil {
		t.Fatal(err)
	}
	c.project = filepath.Join(home, ".formal-gates-resources", "project")
	c.jobs = filepath.Join(evidence, "jobs")
	for _, path := range []string{c.project, c.jobs} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	c.vcs = vcs.NewNative(c.project, provider, nil)
	input, approval := io.Pipe()
	defer input.Close()
	defer approval.Close()
	c.approvalInput = bufio.NewReader(input)
	c.approvalOutput = canaryLocalApproval{approval}
	executor := &canaryLocalExecutor{t: t, canary: c, split: split, developmentStarted: make(chan struct{})}
	for _, setup := range configure {
		setup(executor)
	}
	c.execute = executor.execute
	if failureMode := os.Getenv("FORMAL_GATES_CANARY_LOCAL_FAILURE_FIXTURE"); failureMode != "" {
		started, release := make(chan struct{}), make(chan struct{})
		var startedOnce, releaseOnce sync.Once
		failedAttempt := ""
		executor.transportFailure = func(material canaryMaterials) error {
			if material.Input != nil && material.Input.Handler == "engine.development.batch" {
				if material.Slice == "uppercase" {
					startedOnce.Do(func() { close(started) })
					<-release
				}
				if material.Slice == "word-size" {
					if failedAttempt == "" {
						failedAttempt = material.Pending.AttemptID
					}
					<-started
					if material.Pending.AttemptID != failedAttempt {
						// A new issued attempt proves the previous failure was
						// accepted by the public engine retry owner.
						releaseOnce.Do(func() { close(release) })
					}
					if failureMode == "permanent" || material.Pending.AttemptID == failedAttempt {
						return fmt.Errorf("local host transport unavailable")
					}
				}
			}
			return nil
		}
		defer func() {
			master := c.readRunID("show", "engine-canary")
			for slice, id := range master.SliceChildren {
				child := c.readRunID("show", id)
				data, err := json.Marshal(child)
				if err == nil {
					err = os.WriteFile(filepath.Join(c.jobs, slice+"-after-controller.json"), data, 0600)
				}
				if err != nil {
					t.Error(err)
				}
			}
		}()
	}
	var base string
	if split {
		var intake protocol.IntakeConfirmationReceipt
		base, intake = c.buildSplitFixture()
		c.install(base, intake)
	} else {
		base = c.buildFixtureProject()
		c.doc = []byte(canaryRequirementDocument)
		c.install(base, c.confirmedIntake())
	}
	c.drive()
	if executor.abortBeforeDelivery {
		executor.abortBeforeSliceDelivery()
		return
	}
	c.loop()
	if split {
		c.assertSplitTerminal(base)
		executor.assertOverlap(t)
	} else {
		c.assertTerminal(base, evidence)
		wantMain := strings.Replace(canaryFrobSource, "fmt.Printf(\"frob: %s\\n\", os.Args[1])", "if len(os.Args[1])>0 && os.Args[1][0]=='-' {fmt.Fprintln(os.Stderr,\"unknown flag\");os.Exit(2)}\n\tfmt.Printf(\"frob: %s\\n\", os.Args[1])\n\tusage(os.Args[1])", 1)
		for path, want := range map[string]string{"analytics.go": "package main\nimport \"fmt\"\nfunc usage(word string){fmt.Printf(\"usage: runs=1 words=%d\\n\",len(word))}\n", "frob.go": wantMain} {
			if actual, err := os.ReadFile(filepath.Join(c.project, path)); err != nil || string(actual) != want {
				t.Fatalf("promoted artifact bytes differ: %s %q %v", path, actual, err)
			}
		}
		executor.assertOverlap(t)
	}
	if executor.rejectBadOracle {
		set := c.run.Termination.Validation.Sets[coverage.ReviewMerge]
		if executor.rejectedReview == nil || !executor.redesignFeedback || set == nil || set.Review == nil || set.Review.Verdict() != coverage.StatusPass || len(set.ReviewHistory) < 2 || !canaryValuesEqual(set.ReviewHistory[0], *executor.rejectedReview) {
			t.Fatalf("review rejection/redesign did not survive promotion: %+v", set)
		}
		t.Logf("review FAIL retained original evidence, DRAFT design consumed feedback, new review PASS, flag repair and native cleanup completed")
	}
	t.Logf("local installed identity=%s package=%s evidence=%s actual_model_calls=0 actual_spend=unknown", c.installed.Targets[0].TargetPath, c.installed.PackageDigest, evidence)
}

// The subprocess preserves the normal fatal canary boundary. Its actual public
// receipts must accept errors and settle the independent run through its gates.
func TestCanaryInstalledTransportFailureKeepsIndependentFrontier(t *testing.T) {
	for _, mode := range []string{"transient", "permanent"} {
		t.Run(mode, func(t *testing.T) { runCanaryInstalledTransportFailure(t, "git", mode) })
	}
}

func runCanaryInstalledTransportFailure(t *testing.T, provider, mode string) {
	root := t.TempDir()
	if base := os.Getenv("FORMAL_GATES_CANARY_LOCAL_DIR"); base != "" {
		root = filepath.Join(base, filepath.Join(provider, "transport-"+mode))
	}
	selection := "^TestCanaryInstalledGitFlows$/^split$"
	if provider != "git" {
		selection = "^TestCanaryInstalledNativeFlows$/^" + provider + "$/^split$"
	}
	cmd := exec.Command(os.Args[0], "-test.run="+selection, "-test.v", "-test.timeout=5m")
	cmd.Env = append(os.Environ(), "FORMAL_GATES_CANARY_LOCAL_DIR="+root, "FORMAL_GATES_CANARY_LOCAL_FAILURE_FIXTURE="+mode)
	output, err := cmd.CombinedOutput()
	t.Logf("installed %s transport fixture: exit=%v\n%s", mode, err, output)
	if mode == "transient" && err != nil || mode == "permanent" && (err == nil || !strings.Contains(string(output), "unexpected ask boundary")) {
		t.Fatalf("transport fixture reached the wrong public boundary: %v", err)
	}
	cell := "split"
	if provider != "git" {
		cell = filepath.Join(provider, cell)
	}
	jobs := filepath.Join(root, cell, "evidence", "jobs")
	journal := readTestFileBytes(t, filepath.Join(jobs, "installed-commands.jsonl"))
	failures := 0
	var failedRun facade.Run
	var failedPending protocol.PendingAction
	for _, line := range strings.Split(strings.TrimSpace(string(journal)), "\n") {
		var command struct {
			Exit          int `json:"exit"`
			Input, Output string
		}
		if err := json.Unmarshal([]byte(line), &command); err != nil || command.Exit != 0 {
			t.Fatalf("actual public command failed: %s (%v)", line, err)
		}
		var event protocol.Event
		if command.Input == "" || json.Unmarshal([]byte(command.Input), &event) != nil || event.Result == nil || event.Result.Outcome != protocol.OutcomeRuntimeError {
			continue
		}
		if err := event.Validate(); err != nil || event.Result.FailureClass != authoring.FailureTransientEngine || !strings.Contains(event.Result.InterruptionReason, "local host transport unavailable") {
			t.Fatalf("failure source contract was lost: %+v (%v)", event.Result, err)
		}
		var response struct {
			Acceptance protocol.Acceptance `json:"acceptance"`
			Run        facade.Run          `json:"run"`
		}
		if err := json.Unmarshal([]byte(command.Output), &response); err != nil || response.Acceptance.Status != protocol.OutcomeRuntimeError || response.Acceptance.FailureClass != string(event.Result.FailureClass) {
			t.Fatalf("public failure was not accepted: %s (%v)", command.Output, err)
		}
		rejected, ok := response.Run.RejectedResults[event.Result.ActionID]
		var material canaryMaterials
		if err := json.Unmarshal(readTestFileBytes(t, filepath.Join(canaryJobDirectory(jobs, response.Run.RunID, event.Result.ActionID, rejected.AttemptID), "materials.json")), &material); err != nil {
			t.Fatal(err)
		}
		if !ok || rejected.Task != material.Pending.Task || rejected.AttemptID != material.Pending.AttemptID || rejected.Payload.InterruptionReason != event.Result.InterruptionReason || rejected.Payload.FailureClass != event.Result.FailureClass {
			t.Fatalf("failure lost its issued task/attempt/source diagnostic: %+v material=%+v", rejected, material.Pending)
		}
		attempt, ok := response.Run.EndedAttempts[material.Pending.Task]
		if len(response.Acceptance.Refill) > 0 {
			attempt, ok = response.Run.Attempts[material.Pending.Task]
			if !slices.Contains(response.Acceptance.Refill, attempt.ActionID) || attempt.PreviousAction != event.Result.ActionID {
				t.Fatalf("public refill lost its originating action: %+v", attempt)
			}
		}
		if !ok || attempt.Task != material.Pending.Task || attempt.Attempts != failures+1 || attempt.MaxAttempts <= 0 || attempt.Attempts > attempt.MaxAttempts {
			t.Fatalf("failure changed finite accounting: %+v material=%+v failures=%d", attempt, material.Pending, failures)
		}
		if failures > 0 {
			if failedPending.Task != material.Pending.Task || material.Input.Interruption == nil || material.Input.Interruption.ActionID != failedPending.ActionID || !strings.Contains(material.Input.Interruption.InterruptionReason, "local host transport unavailable") {
				t.Fatalf("retry changed the logical task or lost unfinished feedback: previous=%+v material=%+v", failedPending, material)
			}
		}
		failures++
		failedRun = response.Run
		failedPending = *material.Pending
	}
	if failures == 0 {
		t.Fatal("transport errors did not reach public submit")
	}
	if mode == "transient" {
		if failures != 1 || !strings.Contains(string(output), "ordinary split completed:") {
			t.Fatal("finite transport failure did not resume to the normal terminal contract")
		}
		return
	}
	for _, attempt := range failedRun.EndedAttempts {
		if !attempt.RetryExhausted || failures != attempt.MaxAttempts {
			t.Fatalf("permanent failure did not retain the engine's finite bound: %+v failures=%d", attempt, failures)
		}
	}
	var independent, failed facade.Run
	if err := json.Unmarshal(readTestFileBytes(t, filepath.Join(jobs, "uppercase-after-controller.json")), &independent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(readTestFileBytes(t, filepath.Join(jobs, "word-size-after-controller.json")), &failed); err != nil {
		t.Fatal(err)
	}
	if independent.Status != "SLICE_READY" || independent.SliceReceipt == nil || independent.SliceReceipt.Receipt.Validation.Status != "COMPLETE" || len(independent.PendingActions) != 0 || failed.Next.Kind != decision.KindAsk {
		t.Fatalf("independent frontier was abandoned or failed frontier hidden: independent=%+v failed=%+v", independent, failed)
	}
	t.Logf("public runtime failures=%d independent run=%s status=%s failed run=%s boundary=%s", failures, independent.RunID, independent.Status, failed.RunID, failed.Next.Kind)
}

type canaryLocalApproval struct{ w io.Writer }

func (a canaryLocalApproval) Write(data []byte) (int, error) {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Type ") {
			token, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(line, "Type "), " to authorize this call; any other line denies it."))
			if err != nil {
				return 0, err
			}
			go fmt.Fprintln(a.w, token)
		}
	}
	return len(data), nil
}

type canaryLocalCall struct {
	RunID, ActionID, Handler string
	Start, End               time.Time
}
type canaryLocalExecutor struct {
	t                   *testing.T
	split               bool
	mu                  sync.Mutex
	calls               []canaryLocalCall
	developmentStarted  chan struct{}
	developmentOnce     sync.Once
	transportFailure    func(canaryMaterials) error
	canary              *engineCanary
	publicNative        sync.Once
	abortBeforeDelivery bool
	rejectBadOracle     bool
	rejectedReview      *protocol.CaseReviewEvidence
	redesignFeedback    bool
}

// Only the pre-development reviews and one actual stopped native design turn
// are needed to cancel a master whose children have not delivered any receipts.
func (e *canaryLocalExecutor) abortBeforeSliceDelivery() {
	c := e.canary
	for len(c.run.SliceChildren) != 2 {
		if time.Now().After(c.deadline) {
			c.fail("early abort preparation exceeded its budget")
		}
		advanced := false
		for id, pending := range c.run.PendingActions {
			if pending.ReviewSeries != review.ProductReview && pending.ReviewSeries != review.StartReadiness {
				continue
			}
			if !c.receipt(id) {
				continue
			}
			event := c.dispatchReview(id, pending.ReviewSeries)
			if event.Result.Review.GranularityReview != nil {
				c.masterPlan = event.Result.Review.GranularityReview
			}
			c.submit(event)
			advanced = true
			break
		}
		if !advanced && !c.serveOperator() {
			c.fail("early abort cannot advance preparation: %+v", c.run.Next)
		}
		c.drive()
	}
	c.run = c.readRunID("show", c.run.RunID)
	if c.run.Next.Kind != decision.KindOperator || c.run.Next.Operator == nil || !strings.Contains(c.run.Next.Operator.Facts[0].Value, "slice receipts: 0/2") {
		c.fail("master did not wait for undelivered slices: %+v", c.run.Next)
	}
	observed := false
	for id, input := range c.run.ValidationInputs {
		if input.Handler != "engine.qa.design" {
			continue
		}
		close(e.developmentStarted)
		if !c.receipt(id) {
			continue
		}
		// The actual local native turn is observed stopped, but its business result
		// remains unsubmitted when the user cancels this delivery obligation.
		c.dispatchPipeline(id, input)
		observed = true
		break
	}
	if !observed {
		c.fail("early abort has no issued native design turn")
	}
	request, err := protocol.NewRequestEvent(c.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop before slice delivery"})
	if err != nil {
		c.fail("abort request: %v", err)
	}
	c.submit(request)
	for _, ask := range c.run.PendingAsks {
		if ask.RequestID == string(request.ID) {
			c.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(request.ID), Token: ask.Token, Choice: "abort"}})
			break
		}
	}
	c.drive()
	if c.run.Status != "ABORTING" || c.run.Next.Kind != decision.KindHostAction || len(c.run.ValidationInputs) != 0 {
		c.fail("accepted abort did not expose termination alone: %+v", c.run.Next)
	}
	processFiles, err := filepath.Glob(filepath.Join(c.jobs, c.run.RunID, "*", "*", "attempt-*", "native-process.json"))
	if err != nil || len(processFiles) != 3 {
		c.fail("early abort lacks its three actual native process records: %v %v", processFiles, err)
	}
	for _, path := range processFiles {
		process, err := readJSONFile(path)
		if err != nil {
			c.fail("native process record: %v", err)
		}
		pid, err := strconv.Atoi(textOf(getOf(process, "pid")))
		if err != nil || syscall.Kill(pid, 0) != syscall.ESRCH {
			c.fail("native stop has not been observed for %s: %v %v", path, process, err)
		}
	}
	var captures []string
	for _, event := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
		var paths []string
		paths, err = filepath.Glob(filepath.Join(c.project, ".gates", "tmp", c.run.RunID, "lifecycle", "events", "*", event+".json"))
		if err != nil {
			break
		}
		captures = append(captures, paths...)
	}
	if err != nil || len(captures) != 6 {
		c.fail("early abort lacks its six captured lifecycle facts: %v %v", captures, err)
	}
	stopped := map[string]bool{}
	for _, path := range captures {
		var captured struct {
			Provider, Correlation, Identity, Event string
			TranscriptPath                         string
		}
		if err := json.Unmarshal(readTestFileBytes(c.t, path), &captured); err != nil {
			c.fail("captured lifecycle record: %v", err)
		}
		if captured.Provider != c.provider() || captured.Identity != captured.Correlation || !slices.Contains(c.dispatched, captured.Correlation) || (captured.Event != protocol.LifecycleStart && captured.Event != protocol.LifecycleStop) {
			c.fail("captured lifecycle differs from the actual native dispatch: %+v", captured)
		}
		if captured.Event == protocol.LifecycleStop {
			processPath := filepath.Join(filepath.Dir(captured.TranscriptPath), "native-process.json")
			if !slices.Contains(processFiles, processPath) {
				c.fail("captured stop has no observed native process: %+v", captured)
			}
			stopped[captured.Identity] = true
		}
		event, err := protocol.NewCorrelatedLifecycleEvent(c.eventID(), captured.Provider, captured.Correlation, captured.Identity, captured.Event)
		if err != nil {
			c.fail("captured lifecycle protocol event: %v", err)
		}
		c.submit(event)
	}
	for id, intent := range c.run.AdapterIntents {
		if intent.Terminate == nil || !stopped[intent.Terminate.Identity] {
			c.fail("termination has no actual stopped native source: %+v", intent)
		}
		receipt, err := protocol.NewAgentHostActionReceiptEvent(c.eventID(), id, intent.Operation, c.provider(), id, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Terminate.Identity, Event: protocol.LifecycleStop})
		if err != nil {
			c.fail("observed termination receipt: %v", err)
		}
		c.submit(receipt)
	}
	c.drive()
	final := c.terminalRun()
	if final.Status != "ABORTED" || final.Termination == nil || !final.Termination.ResourcesCleaned() || len(final.Termination.SliceReceipts) != 2 {
		c.fail("early abort failed to clean released children: %+v", final.Next)
	}
	stops := 0
	for _, event := range final.Termination.LifecycleEvents {
		if event.Event == protocol.LifecycleStop {
			stops++
		}
	}
	if stops < 3 {
		c.fail("terminal evidence lost the actual native review/design stops")
	}
	for _, receipt := range final.Termination.SliceReceipts {
		if receipt.Receipt.Status != "ABORTED" {
			c.fail("aborted child was projected as ready")
		}
	}
	for _, resource := range final.Termination.NativeResources {
		if resource.Status != "CLEANED" {
			c.fail("native resource has no cleanup receipt")
		}
		if _, err := os.Stat(resource.Path); !os.IsNotExist(err) {
			c.fail("native workspace survives abort: %s %v", resource.Path, err)
		}
	}
	c.assertTerminalReplay(final)
	c.t.Logf("installed early abort retained %d actual native stops, two ABORTED child receipts, native cleanup and repeated reopened drive/show; actual_model_calls=0 evidence=%s", stops, c.jobs)
}

func (e *canaryLocalExecutor) execute(value any, dir string, probe bool) (source map[string]any, err error) {
	request := value.(map[string]any)
	lines := strings.Split(textOf(request["prompt"]), "\n")
	if len(lines) < 3 || probe {
		return nil, fmt.Errorf("local executor requires exact dispatched material")
	}
	data, err := os.ReadFile(lines[2])
	if err != nil {
		return nil, err
	}
	var material canaryMaterials
	if err := json.Unmarshal(data, &material); err != nil {
		return nil, err
	}
	workspace := textOf(request["workspace"])
	handler := material.ReviewAction
	readOnly := true
	if material.Input != nil {
		input := material.Input
		handler = input.Handler
		if input.Workspace == nil || input.Workspace.Status != "READY" || input.Binding.WorkspaceID != input.Workspace.ID || workspace != input.Workspace.Path || material.RequirementRevision != input.Binding.RequirementRevision || !canaryLocalDocumentMatches(material.RequirementPath, input.RequirementDocument.Content) {
			return nil, fmt.Errorf("local dispatch lost issued run/workspace/requirement identity")
		}
		readOnly = handler != "engine.development.batch" && handler != "engine.repair" && handler != "engine.merge.resolve" && !(handler == "engine.qa.design" && input.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox)
	}
	if request["read_only"] != readOnly || material.ActionID == "" || material.RunID == "" || material.Pending == nil || material.Pending.ActionID != material.ActionID || filepath.Dir(dir) != canaryJobDirectory(e.canary.jobs, material.RunID, material.ActionID, material.Pending.AttemptID) {
		return nil, fmt.Errorf("local executor saw wrong method or dispatch identity")
	}
	call := canaryLocalCall{RunID: material.RunID, ActionID: material.ActionID, Handler: handler, Start: time.Now()}
	defer func() { call.End = time.Now(); e.mu.Lock(); e.calls = append(e.calls, call); e.mu.Unlock() }()
	if handler == "engine.development.batch" {
		e.developmentOnce.Do(func() { close(e.developmentStarted) })
	}
	if e.transportFailure != nil {
		if err := e.transportFailure(material); err != nil {
			return nil, err
		}
	}
	if handler == "engine.qa.design" && (e.split || material.Input.Manifest.Binding.ReviewKind == coverage.ReviewBlackbox) {
		select {
		case <-e.developmentStarted:
		case <-time.After(time.Minute):
			return nil, fmt.Errorf("independent child development did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	answer, err := e.answer(material, workspace)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	body, err := json.Marshal(answer)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeNativeJSON(body)
	if err != nil {
		return nil, err
	}
	answer = objectOrEmpty(decoded)
	if err := e.prepareNativePeer(request, dir, answer); err != nil {
		return nil, err
	}
	public := false
	e.publicNative.Do(func() { public = true })
	if public {
		command := exec.Command(e.canary.binary, "harness", "native", dir)
		command.Dir = workspace
		output, err := command.CombinedOutput()
		if writeErr := os.WriteFile(filepath.Join(dir, "public-native.log"), output, 0600); writeErr != nil {
			return nil, writeErr
		}
		if err != nil {
			return nil, fmt.Errorf("installed native: %w %s", err, output)
		}
		var observed any
		observed, err = readJSONFile(filepath.Join(dir, "source.json"))
		source = objectOrEmpty(observed)
	} else {
		source, err = runNativeJob(dir)
	}
	if err != nil {
		return nil, err
	}
	impl, err := nativeImplementationFor(e.canary.nativeName())
	if err != nil {
		return nil, err
	}
	if err := impl.verifySource(dir, source); err != nil {
		return nil, err
	}
	observed, err := readJSONFile(filepath.Join(dir, "answer.json"))
	if err != nil || !valuesEqual(observed, answer) || source["provider"] != e.canary.nativeName() || getOf(source, "selection") == nil {
		return nil, fmt.Errorf("native observer lost exact role answer/provider/selection: %v", err)
	}
	return source, nil
}

func (e *canaryLocalExecutor) answer(m canaryMaterials, workspace string) (map[string]any, error) {
	if m.ReviewAction != "" {
		answer := map[string]any{"status": "PASS", "findings": []any{}, "verification": map[string]any{"checks": []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, "evidence": "Read exact confirmed document: " + m.RequirementPath + " revision " + m.RequirementRevision}}
		if m.ReviewAction == string(review.StartReadiness) {
			plan := canaryLocalPlan(e.split)
			answer["plan"] = plan
		}
		return answer, nil
	}
	input := *m.Input
	switch input.Handler {
	case "engine.development.batch", "engine.repair":
		paths := []string{}
		dod, checks := map[string]string{}, map[string]string{}
		if input.Development != nil {
			for _, path := range input.Development.Batch.Artifacts {
				content := ""
				switch path {
				case "uppercase.go":
					content = "package main\nimport \"strings\"\nfunc UpperWord(word string)string{return strings.ToUpper(word)}\n"
				case "size.go":
					content = "package main\nfunc WordSize(word string)int{return len(word)}\n"
				case "analytics.go":
					content = "package main\nimport \"fmt\"\nfunc usage(word string){fmt.Printf(\"usage: runs=1 words=%d\\n\",len(word))}\n"
				default:
					return nil, fmt.Errorf("unexpected planned artifact %s", path)
				}
				if err := os.WriteFile(filepath.Join(workspace, path), []byte(content), 0600); err != nil {
					return nil, err
				}
				paths = append(paths, path)
			}
			if !e.split {
				main := canaryFrobSource
				main = strings.Replace(main, "fmt.Printf(\"frob: %s\\n\", os.Args[1])", "fmt.Printf(\"frob: %s\\n\", os.Args[1])\n\tusage(os.Args[1])", 1)
				if err := os.WriteFile(filepath.Join(workspace, "frob.go"), []byte(main), 0600); err != nil {
					return nil, err
				}
				paths = append(paths, "frob.go")
			}
			for _, criterion := range input.Development.Batch.DoD {
				dod[criterion.ID] = "Implemented " + criterion.Description
			}
			for _, check := range input.Development.Batch.Checks {
				observation := canaryLocalObserve(workspace, strings.Fields(check.Command))
				if observation.ExitCode != 0 {
					return nil, fmt.Errorf("development check failed: %+v", observation)
				}
				checks[check.ID] = observation.Output + " exit=0"
			}
		} else {
			path := filepath.Join(workspace, "frob.go")
			body, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			content := strings.Replace(string(body), "fmt.Printf(\"frob: %s\\n\", os.Args[1])", "if len(os.Args[1])>0 && os.Args[1][0]=='-' {fmt.Fprintln(os.Stderr,\"unknown flag\");os.Exit(2)}\n\tfmt.Printf(\"frob: %s\\n\", os.Args[1])", 1)
			if e.split {
				content = strings.Replace(string(body), "func main(){", "func main(){if len(os.Args)>1 && len(os.Args[1])>0 && os.Args[1][0]=='-' {fmt.Fprintln(os.Stderr,\"unknown flag\");os.Exit(2)};", 1)
			}
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				return nil, err
			}
			paths = []string{"frob.go"}
		}
		build := canaryLocalObserve(workspace, []string{"go", "build", "./..."})
		if build.ExitCode != 0 {
			return nil, fmt.Errorf("local build failed: %+v", build)
		}
		tests := canaryLocalObserve(workspace, []string{"go", "test", "./..."})
		for i, path := range paths {
			paths[i] = filepath.Join(workspace, path)
		}
		answer := map[string]any{"paths": paths, "summary": "implemented declared artifact contract", "build": []string{fmt.Sprintf("go build ./... exit=%d %s", build.ExitCode, build.Output)}, "tests": []string{fmt.Sprintf("go test ./... exit=%d %s", tests.ExitCode, tests.Output)}}
		if input.Development != nil {
			answer["dod"], answer["checks"], answer["handoff"] = dod, checks, "declared artifacts ready"
		} else {
			answer["affectedCases"] = []string{}
		}
		return answer, nil
	case "engine.qa.design":
		answer, err := e.design(input, workspace)
		if err != nil || !e.rejectBadOracle || input.Manifest.Binding.ReviewKind != coverage.ReviewMerge {
			return answer, err
		}
		if len(m.ReviewHistory) == 0 {
			if e.rejectedReview != nil {
				return nil, fmt.Errorf("new design lost failed review feedback")
			}
			for _, raw := range answer["cases"].([]any) {
				row := raw.(map[string]any)
				if row["pointId"] == "AC-001" && row["scenario"] == "SUCCESS" {
					row["args"] = []string{"straße"}
					row["oracle"] = "exit=0 output=frob: STRASSE bytes=7"
				}
			}
		} else {
			run := e.canary.readRunID("show", m.RunID)
			set := run.Validation.Sets[coverage.ReviewMerge]
			if e.rejectedReview == nil || len(m.ReviewHistory) != 1 || !canaryValuesEqual(m.ReviewHistory[0], *e.rejectedReview) || set.Status != "DRAFT" || set.Review != nil || set.Candidate != "" || !canaryValuesEqual(input.Manifest, e.rejectedReview.Manifest) {
				return nil, fmt.Errorf("reopened design lost original failed manifest/review/candidate or DRAFT boundary")
			}
			e.redesignFeedback = true
		}
		return answer, nil
	case "engine.qa.review.rationale":
		decisions := func(ids []string) []any {
			out := []any{}
			for _, id := range ids {
				out = append(out, map[string]any{"id": id, "status": "PASS", "reason": "case contract independently covers " + id})
			}
			return out
		}
		points, cases := []string{}, []string{}
		for _, p := range input.Manifest.Points {
			points = append(points, p.PointID)
		}
		for _, c := range input.Manifest.Cases {
			cases = append(cases, c.CaseID)
		}
		answer := map[string]any{"sourceDecisions": decisions(input.Manifest.Sources), "pointDecisions": decisions(points), "caseDecisions": decisions(cases)}
		if e.rejectBadOracle && input.Manifest.Binding.ReviewKind == coverage.ReviewMerge {
			for _, designed := range input.Manifest.Cases {
				if designed.Oracle != "exit=0 output=frob: STRASSE bytes=7" {
					continue
				}
				if e.rejectedReview != nil {
					return nil, fmt.Errorf("redesign repeated the rejected oracle")
				}
				reason := "strings.ToUpper leaves ß unchanged; the STRASSE oracle rejects a conformant implementation"
				for layer, id := range map[string]string{"sourceDecisions": "REQ-001", "pointDecisions": "AC-001", "caseDecisions": designed.CaseID} {
					for _, raw := range answer[layer].([]any) {
						row := raw.(map[string]any)
						if row["id"] == id {
							row["status"], row["reason"] = "FAIL", reason
						}
					}
				}
				var submitted struct{ SourceDecisions, PointDecisions, CaseDecisions []canaryReviewDecision }
				body, _ := json.Marshal(answer)
				if err := json.Unmarshal(body, &submitted); err != nil {
					return nil, err
				}
				e.rejectedReview = &protocol.CaseReviewEvidence{Candidate: input.Binding.Candidate, Manifest: input.Manifest, Review: coverage.ReviewResult{Binding: input.Manifest.Binding, Scope: coverage.ScopeFull, SourceDecisions: e.canary.modelReviewDecisions("source", submitted.SourceDecisions), PointDecisions: e.canary.modelReviewDecisions("point", submitted.PointDecisions), CaseDecisions: e.canary.modelReviewDecisions("case", submitted.CaseDecisions)}}
			}
		}
		return answer, nil
	case "engine.qa.execution":
		records := []any{}
		for _, observation := range m.HostObservations {
			caseID := strings.TrimPrefix(observation.Label, "case ")
			passed := false
			if input.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
				passed = observation.ExitCode == 0
			} else {
				point := canaryCasePoint(input, caseID)
				args := observation.Command[1:]
				passed = e.matches(point, args, observation)
			}
			result := "EXECUTED_FAIL"
			if passed {
				result = "EXECUTED_PASS"
			}
			records = append(records, map[string]any{"caseId": caseID, "result": result, "procedure": strings.Join(observation.Command, " "), "observation": fmt.Sprintf("exit=%d output=%s", observation.ExitCode, observation.Output), "comparison": "compared exact output and exit against the bound point oracle"})
		}
		return map[string]any{"records": records}, nil
	case "engine.gate":
		// The projected slice gates describe the master-owned flag gap without
		// taking it as slice work; the merge gate is its blocking responsibility.
		if e.split && m.Slice == "" {
			index := -1
			if input.Gate.Gate == "correctness" && strings.Contains(input.Binding.Task.Scope, ":slice:uppercase:") {
				index = 0
			}
			if input.Gate.Gate == "correctness" && strings.Contains(input.Binding.Task.Scope, ":slice:word-size:") {
				index = 1
			}
			if input.Gate.Gate == "merge-gate" {
				index = 2
			}
			if index >= 0 {
				var probe canaryObservation
				found := false
				for _, observation := range m.HostObservations {
					if observation.Label == "public flag-contract probe" {
						probe, found = observation, true
						break
					}
				}
				if !found {
					for _, observation := range m.HostObservations {
						if observation.Label == "command contract" && len(observation.Command) > 0 {
							probe = canaryLocalObserve(workspace, append([]string{observation.Command[0]}, canaryGateArgs...))
							found = true
							break
						}
					}
				}
				if !found {
					return nil, fmt.Errorf("flag finding has no issued candidate probe")
				}
				if probe.ExitCode == 0 && strings.Contains(probe.Output, "frob:") {
					var samples []struct {
						Raw review.Finding `json:"raw"`
					}
					if err := json.Unmarshal(readTestFileBytes(e.t, filepath.Join("testdata", "split_flag_findings.json")), &samples); err != nil {
						return nil, err
					}
					raw := samples[index].Raw
					status := "PASS"
					if index == 2 {
						status = "FAIL"
					}
					found := canaryFindingAnswer{Severity: raw.Severity, Message: raw.Message, SeverityRationale: raw.SeverityRationale, Requirement: raw.Requirement, NormalPath: raw.NormalPath, ActualError: raw.ActualError, DeferralImpact: raw.DeferralImpact, Locations: raw.Locations}
					return map[string]any{"status": status, "findings": []any{found}}, nil
				}
			}
		}
		passed := true
		for _, observation := range m.HostObservations {
			if input.Gate.Gate == "regression" && e.split {
				passed = passed && observation.ExitCode == 0
				continue
			}
			point := "AC-002"
			if e.split {
				point = "AC-003"
				if m.Slice == "uppercase" {
					point = "AC-001"
				} else if m.Slice == "word-size" {
					point = "AC-002"
				}
			}
			if observation.Label == "public flag-contract probe" {
				point = "AC-004"
			}
			passed = passed && e.matches(point, observation.Command[1:], observation)
		}
		if passed {
			return map[string]any{"status": "PASS", "findings": []any{}}, nil
		}
		requirement := "AC-002"
		if e.canary.splitFlagContract() {
			requirement = "AC-004"
		}
		finding := map[string]any{"severity": "P1", "message": "unknown flags are accepted", "severityRationale": "the public flag contract fails on its normal command entry", "requirement": requirement, "normalPath": "frob --unknown-flag canary", "actualError": "exit=0 and frob output", "deferralImpact": "unknown flags execute as words", "locations": []string{"frob.go"}}
		return map[string]any{"status": "FAIL", "findings": []any{finding}}, nil
	}
	return nil, fmt.Errorf("local executor has no answer for issued handler %s", input.Handler)
}

func canaryLocalPlan(split bool) *protocol.GranularityReview {
	batch := func(id, artifact, deliverable string) protocol.DevelopmentBatch {
		return protocol.DevelopmentBatch{ID: id, Artifacts: []string{artifact}, Subtasks: []protocol.DevelopmentSubtask{{ID: id + "-implementation", Description: deliverable}}, Deliverable: deliverable, PublicEntry: "frob word", Interfaces: "function called by seeded main", StateBoundary: "no persistent state", Risk: "wrong text result", DoD: []protocol.DevelopmentCriterion{{ID: "output", Description: deliverable}}, Checks: []protocol.DevelopmentCheck{{ID: "build", Command: "go build ./..."}}, Rollback: "restore artifact from base", Recovery: "repeat declared implementation", Handoff: "function ready for command integration", ContextCost: "one function contract"}
	}
	plan := &protocol.GranularityReview{Reason: "independent declared artifacts", VerificationTier: "FINAL"}
	if split {
		plan.Batches = []protocol.DevelopmentBatch{batch("uppercase", "uppercase.go", "UpperWord returns Unicode uppercase including empty input"), batch("word-size", "size.go", "WordSize returns UTF-8 byte length including empty input")}
	} else {
		plan.Batches = []protocol.DevelopmentBatch{batch("usage", "analytics.go", "usage prints the byte count and main invokes it")}
	}
	return plan
}
func (e *canaryLocalExecutor) design(input protocol.ValidationInput, workspace string) (map[string]any, error) {
	cases := []any{}
	kind := input.Manifest.Binding.ReviewKind
	whitebox := kind == coverage.ReviewWhitebox
	var tests strings.Builder
	if whitebox {
		tests.WriteString("package main\nimport(\"testing\";\"os/exec\";\"strings\")\n")
	}
	for _, point := range input.Manifest.Points {
		type sample struct {
			behavior, scenario string
			args               []string
		}
		samples := []sample{{"OrdinaryWord", "SUCCESS", []string{"hello"}}, {"EmptyWord", "BOUNDARY", []string{""}}}
		if e.split {
			samples[0] = sample{"UnicodeWord", "SUCCESS", []string{"héLLo"}}
		}
		if e.flagPoint(point.PointID) {
			samples = []sample{{"UnknownFlagWithWord", "MISUSE", []string{"--unknown-flag", "canary"}}, {"UnknownFlag", "MISUSE", []string{"--bogus"}}}
		}
		for _, sample := range samples {
			args := sample.args
			id := "case-" + string(kind) + "-" + point.PointID + "-" + sample.behavior
			oracle := e.oracle(point.PointID, args)
			designed := map[string]any{"caseId": id, "pointId": point.PointID, "scenario": sample.scenario, "oracle": oracle}
			if whitebox {
				ref := "TestContract" + string(kind) + strings.ReplaceAll(point.PointID, "-", "") + sample.behavior
				argv := []string{"run", "."}
				argv = append(argv, args...)
				quoted := []string{}
				for _, arg := range argv {
					quoted = append(quoted, strconv.Quote(arg))
				}
				expected := e.expected(point.PointID, args)
				assertion := fmt.Sprintf("if err!=nil||strings.TrimSpace(string(out))!=%q{t.Fatalf(\"command: %%v %%s\",err,out)}", expected)
				if e.flagPoint(point.PointID) {
					assertion = "if err==nil||strings.Contains(string(out),\"frob:\"){t.Fatalf(\"flag contract: %v %s\",err,out)}"
				}
				fmt.Fprintf(&tests, "func %s(t *testing.T){out,err:=exec.Command(\"go\",%s).CombinedOutput();%s}\n", ref, strings.Join(quoted, ","), assertion)
				designed["testRef"] = ref
			} else {
				designed["args"], designed["preconditions"], designed["steps"] = args, "compiled candidate in issued workspace", "run frob with exact argv"
			}
			cases = append(cases, designed)
		}
	}
	answer := map[string]any{"cases": cases}
	if whitebox {
		path := "frob_contract_test.go"
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(tests.String()), 0600); err != nil {
			return nil, err
		}
		build, run := canaryLocalObserve(workspace, []string{"go", "build", "./..."}), canaryLocalObserve(workspace, []string{"go", "test", "./..."})
		answer["paths"], answer["summary"], answer["build"], answer["tests"] = []string{path}, "authored bound command contract cases", []string{fmt.Sprintf("exit=%d %s", build.ExitCode, build.Output)}, []string{fmt.Sprintf("exit=%d %s", run.ExitCode, run.Output)}
	}
	return answer, nil
}
func (e *canaryLocalExecutor) expected(point string, args []string) string {
	word := args[0]
	if e.split {
		return fmt.Sprintf("frob: %s bytes=%d", strings.ToUpper(word), len(word))
	}
	return fmt.Sprintf("frob: %s\nusage: runs=1 words=%d", word, len(word))
}
func (e *canaryLocalExecutor) oracle(point string, args []string) string {
	if e.flagPoint(point) {
		return "nonzero exit and no frob: output"
	}
	return "exit=0 output=" + e.expected(point, args)
}
func (e *canaryLocalExecutor) matches(point string, args []string, observation canaryObservation) bool {
	if e.flagPoint(point) {
		return observation.ExitCode != 0 && !strings.Contains(observation.Output, "frob:")
	}
	if observation.ExitCode != 0 || len(args) == 0 {
		return false
	}
	if e.split && point == "AC-001" {
		return strings.HasPrefix(observation.Output, "frob: "+strings.ToUpper(args[0])+" bytes=")
	}
	if e.split && point == "AC-002" {
		return strings.HasSuffix(observation.Output, fmt.Sprintf(" bytes=%d", len(args[0])))
	}
	return observation.Output == e.expected(point, args)
}
func canaryCasePoint(input protocol.ValidationInput, caseID string) string {
	for _, edge := range input.Manifest.Edges {
		if edge.CaseID == caseID {
			return edge.PointID
		}
	}
	return ""
}
func canaryLocalObserve(workspace string, argv []string) canaryObservation {
	if len(argv) == 3 && argv[0] == "go" && argv[1] == "build" {
		output, err := os.MkdirTemp("", "canary-build-")
		if err != nil {
			return canaryObservation{Directory: workspace, Command: argv, ExitCode: 1, Output: err.Error()}
		}
		defer os.RemoveAll(output)
		argv = []string{"go", "build", "-o", filepath.Join(output, "frob"), argv[2]}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		exit = 1
		if failure, ok := err.(*exec.ExitError); ok {
			exit = failure.ExitCode()
		}
	}
	return canaryObservation{Directory: workspace, Command: argv, ExitCode: exit, Output: strings.TrimSpace(string(output))}
}
func canaryBytesRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (e *canaryLocalExecutor) assertOverlap(t *testing.T) {
	e.mu.Lock()
	defer e.mu.Unlock()
	overlapping := false
	for _, dev := range e.calls {
		if dev.Handler != "engine.development.batch" {
			continue
		}
		for _, qa := range e.calls {
			if (qa.Handler == "engine.qa.design" || qa.Handler == "engine.qa.review.rationale") && (!e.split || dev.RunID != qa.RunID) && dev.Start.Before(qa.End) && qa.Start.Before(dev.End) {
				overlapping = true
			}
		}
	}
	if !overlapping {
		t.Fatal("development and independent case preparation did not execute concurrently")
	}
	for _, call := range e.calls {
		t.Logf("local actual call interval run=%s action=%s handler=%s start=%s end=%s", call.RunID, call.ActionID, call.Handler, call.Start.UTC().Format(time.RFC3339Nano), call.End.UTC().Format(time.RFC3339Nano))
	}
}

func canaryLocalDocumentMatches(path, content string) bool {
	data, err := os.ReadFile(path)
	return err == nil && string(data) == content
}

// Equal local action IDs belong to different runs. A blocked failing worker
// still reports a typed completion while independent work can finish and refill.
func TestCanaryWorkerCompletionKeepsRunIdentity(t *testing.T) {
	home := resolveNativePath(t.TempDir())
	jobs := resolveNativePath(t.TempDir())
	input, approval := io.Pipe()
	defer input.Close()
	defer approval.Close()
	shared := &sync.Mutex{}
	reader := bufio.NewReader(input)
	completed := make(chan canaryCompletion)
	release := make(chan struct{})
	started := make(chan struct{})
	launch := func(runID string) {
		project := filepath.Join(home, ".formal-gates-resources", runID)
		if err := os.MkdirAll(project, 0700); err != nil {
			t.Fatal(err)
		}
		doc := []byte(canarySplitRequirement)
		if err := os.WriteFile(filepath.Join(project, "requirements.md"), doc, 0600); err != nil {
			t.Fatal(err)
		}
		pending := protocol.PendingAction{ActionID: "same-action", ReviewSeries: review.ProductReview}
		config := filepath.Join(home, ".claude")
		if err := os.MkdirAll(config, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(config, "settings.json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		c := &engineCanary{t: t, nativeHost: "claude", localPeer: true, binding: map[string]any{"environment": map[string]any{"HOME": home, "CLAUDE_CONFIG_DIR": config}}, worker: true, home: home, project: project, jobs: jobs, run: facade.Run{RunID: runID, PendingActions: map[string]protocol.PendingAction{"same-action": pending}}, revision: canaryBytesRevision(doc), deadline: time.Now().Add(time.Minute), approvalMu: shared, approvalInput: reader, approvalOutput: canaryLocalApproval{approval}, caseArgs: map[string][]string{}, caseTests: map[string]string{}}
		// One reader is shared together with its authorization mutex.
		if runID == "failed" {
			c.execute = func(any, string, bool) (map[string]any, error) {
				select {
				case <-started:
				default:
					close(started)
				}
				<-release
				return nil, fmt.Errorf("local worker transport failed")
			}
		} else {
			executor := &canaryLocalExecutor{t: t, canary: c}
			executor.publicNative.Do(func() {})
			c.execute = executor.execute
		}
		go func() { completed <- c.dispatch("same-action", pending, protocol.ValidationInput{}) }()
	}
	launch("failed")
	<-started
	launch("independent")
	done := <-completed
	if done.runID != "independent" || done.event.Result.Outcome != protocol.OutcomePass {
		t.Fatalf("independent completion=%+v", done)
	}
	launch("refilled")
	refill := <-completed
	if refill.runID != "refilled" || refill.event.Result.Outcome != protocol.OutcomePass {
		t.Fatalf("refill waited for failing worker: %+v", refill)
	}
	close(release)
	failed := <-completed
	failed.event.ID = "validate-worker-failure"
	if failed.runID != "failed" || failed.event.Result.Outcome != protocol.OutcomeRuntimeError || failed.event.Result.FailureClass != authoring.FailureTransientEngine || !strings.Contains(failed.event.Result.InterruptionReason, "local worker transport failed") || failed.event.Validate() != nil {
		t.Fatalf("failure lost completion: %+v", failed)
	}
	for _, runID := range []string{"independent", "refilled"} {
		identity, err := readJSONFile(filepath.Join(jobs, runID, "same-action", "attempt-1", "identity.json"))
		request, requestErr := readJSONFile(filepath.Join(jobs, runID, "same-action", "attempt-1", "native-request.json"))
		material, materialErr := readJSONFile(filepath.Join(jobs, runID, "same-action", "materials.json"))
		dir := filepath.Join(jobs, runID, "same-action", "attempt-1")
		if err != nil || requestErr != nil || materialErr != nil || getOf(material, "runId") != runID || getOf(material, "actionId") != "same-action" || getOf(identity, "identity") != codexPeerID(dir+textOf(getOf(request, "workspace"))+textOf(getOf(request, "prompt"))) {
			t.Fatalf("job identity=%#v material=%#v errors=%v %v %v", identity, material, err, requestErr, materialErr)
		}
	}
}

func (e *canaryLocalExecutor) flagPoint(point string) bool {
	return !e.split && point == "AC-002" || e.canary.splitFlagContract() && point == "AC-004"
}

// Each native call owns its peer script. The wrapper fixes that script before
// process launch, including DSH whose adapter uses the calling environment.
func canaryPeerWrapper(testName, key, script string) string {
	binary, _ := os.Executable()
	quote := func(text string) string { return "'" + strings.ReplaceAll(text, "'", "'\\''") + "'" }
	prefix := ""
	if key != "" {
		prefix = "export " + key + "=" + quote(script) + "\n"
	}
	return "#!/bin/sh\n" + prefix + "exec " + quote(binary) + " -test.run='^" + testName + "$' -- \"$@\"\n"
}

func (e *canaryLocalExecutor) prepareNativePeer(request map[string]any, dir string, answer map[string]any) error {
	c := e.canary
	workspace := resolveNativePath(textOf(request["workspace"]))
	identity := codexPeerID(dir + workspace + textOf(request["prompt"]))
	binding := maps.Clone(objectOrEmpty(request["binding"]))
	environment := maps.Clone(objectOrEmpty(binding["environment"]))
	launch := maps.Clone(objectOrEmpty(binding["launch"]))
	script := filepath.Join(dir, "peer.json")
	wrapper := filepath.Join(dir, "native-peer")
	testName, key := "TestScriptedPeer", "FORMAL_GATES_NATIVE_PEER_SCRIPT"
	switch c.nativeName() {
	case "claude":
		testName, key = "TestClaudeCLI", "CLAUDE_PEER_SCRIPT"
	case "codex":
		testName, key = "TestCodexRPCPeer", "CODEX_PEER_SCRIPT"
	case "cursor":
		testName, key = "TestCursorCLI", "CURSOR_PEER_SCRIPT"
	}
	if err := os.WriteFile(wrapper, []byte(canaryPeerWrapper(testName, key, script)), 0755); err != nil {
		return err
	}
	launch["executable"] = wrapper
	var err error
	binding, err = buildCaptureBinding(c.nativeName(), launch)
	if err != nil {
		return err
	}
	binding["environment"] = environment
	request["binding"] = binding
	paths := c.hostPaths()
	answerJSON := mustCanonical(e.t, answer)
	switch c.nativeName() {
	case "claude":
		peer := claudePeer{Workspace: workspace, Calls: filepath.Join(dir, "peer-calls.jsonl"), Environment: map[string]string{"HOME": c.home, "CLAUDE_CONFIG_DIR": paths["config"]}, Events: []any{
			map[string]any{"type": "system", "subtype": "init", "session_id": identity, "cwd": workspace, "model": "claude-fixture-model", "effort": "high", "uuid": "init-" + identity},
			map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": identity, "uuid": "result-" + identity, "num_turns": 1, "structured_output": answer, "usage": map[string]any{"input_tokens": len(textOf(request["prompt"])), "output_tokens": len(answerJSON)}},
		}}
		s := claudeScenario{request: request, directory: dir, workspace: workspace, script: script, peer: peer}
		s.save(e.t)
	case "codex":
		s := codexScenario{request: request, directory: dir, workspace: workspace, home: c.home, config: paths["config"], peer: codexPeerInput{Workspace: workspace, Home: c.home, Config: paths["config"], Answer: answer,
			Configuration: map[string]any{"model": "peer-model", "reasoningEffort": "high", "sandbox": map[string]any{"type": "workspaceWrite", "writableRoots": []any{workspace}, "networkAccess": false}, "approvalPolicy": "never", "approvalsReviewer": "user"}}}
		s.peer.ThreadParams = map[string]any{"cwd": workspace, "ephemeral": false}
		if request["read_only"] == true {
			s.peer.ThreadParams["sandbox"] = "read-only"
		}
		if model, _ := request["model"].(string); model != "" {
			s.peer.ThreadParams["model"] = model
		}
		s.peer.TurnParams = map[string]any{"input": []any{map[string]any{"type": "text", "text": request["prompt"]}}, "outputSchema": request["schema"]}
		for _, pair := range [][2]string{{"model", "model"}, {"reasoning_effort", "effort"}} {
			if value, _ := request[pair[0]].(string); value != "" {
				s.peer.TurnParams[pair[1]] = value
			}
		}
		if err := writeAtomicJSON(script, s.peer); err != nil {
			return err
		}
	case "cursor":
		sum := md5.Sum([]byte(workspace))
		peer := cursorPeer{UUID: identity, Workspace: workspace, Environment: map[string]string{"HOME": c.home, "CURSOR_CONFIG_DIR": paths["config"], "CURSOR_DATA_DIR": paths["data"], "NODE_COMPILE_CACHE": paths["cache"]}, Calls: filepath.Join(dir, "peer-calls.jsonl"), Store: filepath.Join(paths["config"], "chats", hex.EncodeToString(sum[:]), identity, "store.db"),
			Config: map[string]any{"model": map[string]any{"modelId": "model-a", "displayModelId": "model-a", "displayName": "model-a high"}, "selectedModel": map[string]any{"modelId": "model-a", "parameters": []any{map[string]any{"id": "effort", "value": "high"}}}},
			Events: []any{map[string]any{"type": "system", "subtype": "init", "session_id": identity, "cwd": workspace, "model": "model-a high", "permissionMode": "default"}, map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": identity, "request_id": "request-" + identity, "result": answerJSON, "usage": map[string]any{"inputTokens": len(textOf(request["prompt"])), "outputTokens": len(answerJSON)}}}}
		s := cursorScenario{request: request, directory: dir, workspace: workspace, script: script, peer: peer}
		s.save(e.t)
	case "dsh":
		peerEnvironment := map[string]string{}
		for _, key := range []string{"HOME", "DSH_HOME", "DSH_PROJECT_DIR", "FORMAL_GATES_HARNESS_ROOT"} {
			peerEnvironment[key] = environment[key].(string)
		}
		schema := mustCanonical(e.t, request["schema"])
		events := peerEvent(identity, "assistant/message", map[string]any{"turn": 1, "step": 0, "usage": map[string]any{"inputTokens": len(textOf(request["prompt"])), "outputTokens": len(answerJSON)}, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": answerJSON}}}}) + "\n" + peerEvent(identity, "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}) + "\n"
		peer := peerScript{Args: []string{"--profile", "acp", "--patch", filepath.Join(dir, "profile.json")}, Environment: peerEnvironment, Workspace: workspace, Echo: filepath.Join(dir, "peer-input.jsonl"), Steps: []peerStep{
			{On: `"method":"initialize"`, Params: map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}, Send: `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"sessionCapabilities":{"resume":{},"close":{}}}}}`},
			{On: `"method":"session/new"`, Params: map[string]any{"cwd": workspace, "mcpServers": []any{}}, Send: `{"id":"@ID@","result":{"sessionId":"` + identity + `","configOptions":[{"id":"model","currentValue":"test-model"},{"id":"reasoning_effort","currentValue":"high"}]}}`},
			{On: `"method":"session/prompt"`, Params: map[string]any{"sessionId": identity, "prompt": []any{map[string]any{"type": "text", "text": textOf(request["prompt"]) + "\nReturn ONLY one JSON object matching this result schema:\n" + schema}}}, Files: []peerFile{{Path: filepath.Join(dir, "native-source.json"), Body: `{"sessionId":"` + identity + `","inputId":"input-` + identity + `","turn":1}`}, {Path: filepath.Join(dir, "native-events.jsonl"), Body: events}}, Send: `{"id":"@ID@","result":{"stopReason":"end_turn"}}`},
			{On: `"method":"session/close"`, Params: map[string]any{"sessionId": identity}, Send: `{"id":"@ID@","result":{}}`},
		}}
		if err := writeAtomicJSON(script, peer); err != nil {
			return err
		}
	case "zcode":
		peerEnvironment := map[string]string{}
		for _, key := range []string{"HOME", "USERPROFILE", "ZCODE_DATA_BASE_DIR", "ZCODE_HOME", "ZCODE_STORAGE_DIR", "ZCODE_SESSION_DB_PATH", "ZCODE_SESSION_DB", "ZCODE_LOG_DIR", "ZCODE_MAILBOX_ROOT", zcodeBuiltinCatalogueEnv, zcodePersonalConfigEnv, zcodeCredentialHelperEnv, "ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE", "FORMAL_GATES_HARNESS_ROOT"} {
			peerEnvironment[key] = environment[key].(string)
		}
		input := "formal-gates-" + filepath.Base(dir)
		turn := "turn-" + identity
		schema, err := schemaForPrompt(request["schema"])
		if err != nil {
			return err
		}
		create := map[string]any{"workspace": map[string]any{"workspacePath": workspace, "workspaceKey": workspace}, "persistence": "immediate"}
		if request["read_only"] == true {
			create["toolAllowlist"] = zcodeReadOnlyToolsValue()
		}
		steps := []peerStep{
			{On: `"method":"runtime/capabilities"`, Params: map[string]any{}, Send: `{"id":"@ID@","result":{}}`},
			{On: `"method":"session/create"`, Params: create, Send: zcodeSnapshotFrame(identity, zcodeSettings)},
			{On: `"method":"session/subscribe"`, Params: map[string]any{"sessionId": identity, "deliveryKind": "desktop-continuous", "includeSnapshot": false}, Send: `{"id":"@ID@","result":{"sessionId":"` + identity + `"}}`},
			{On: `"method":"session/send"`, Params: map[string]any{"sessionId": identity, "inputId": input, "content": textOf(request["prompt"]) + "\nReturn ONLY one JSON object matching this schema:\n" + string(schema)}, Send: `{"id":"@ID@","result":{"sessionId":"` + identity + `","accepted":true}}` + "\n" + zcodeEventLine(identity, 1, "turn.started", turn, zcodeTurnPayload(input, "")) + "\n" + zcodeEventLine(identity, 2, "turn.completed", turn, zcodeTurnPayload(input, `,"resultType":"success","response":`+mustCanonical(e.t, answerJSON)+`,"usage":{"inputTokens":`+strconv.Itoa(len(textOf(request["prompt"])))+`,"outputTokens":`+strconv.Itoa(len(answerJSON))+`}`))},
			{On: `"method":"session/close"`, Params: map[string]any{"sessionId": identity}, Send: `{"id":"@ID@","result":{}}`},
		}
		if mode, _ := request["permission_mode"].(string); mode != "" {
			steps = append(steps, peerStep{On: `"method":"session/setMode"`, Params: map[string]any{"sessionId": identity, "mode": mode}, Send: `{"id":"@ID@","result":{"settings":{"mode":{"current":` + strconv.Quote(mode) + `}}}}`})
		}
		delivered, err := zcodeAccountProviderConfiguration(paths["catalogue"], nil)
		if err != nil {
			return err
		}
		if delivered != nil {
			steps = append(steps, peerStep{On: `"method":"` + zcodeAccountConfigMethod + `"`, Params: delivered["configuration"], Send: `{"id":"@ID@","result":{"status":"received","providerCount":` + strconv.Itoa(len(listOf(delivered["providers"]))) + `}}`})
		}
		if err := writeAtomicJSON(script, peerScript{Args: []string{textOf(launch["script"]), "--cwd", workspace, "app-server"}, Environment: peerEnvironment, Workspace: workspace, Echo: filepath.Join(dir, "peer-input.jsonl"), Steps: steps}); err != nil {
			return err
		}
	}
	return writeAtomicJSON(filepath.Join(dir, "native-request.json"), request)
}

// Cancellation uses the same public supervisor and native job as completion.
func TestCanaryACPAndAppServerCancellation(t *testing.T) {
	binary := supervisorBinary(t)
	for _, nativeHost := range []string{"dsh", "zcode"} {
		t.Run(nativeHost, func(t *testing.T) {
			var request map[string]any
			var dir, workspace string
			if nativeHost == "dsh" {
				steps := successSteps()
				steps[3] = peerStep{On: `"method":"session/prompt"`, IgnoreEOF: true}
				s := newDSHScenario(t, steps, nil)
				request, dir, workspace = s.request, s.directory, s.workspace
			} else {
				s := newZcodeScenario(t, func(input string) []peerStep {
					steps := zcodeSuccessSteps(input)
					steps = append(steps[:2], peerStep{On: `"method":"session/send"`, IgnoreEOF: true})
					return steps
				}, nil)
				request, dir, workspace = s.request, s.directory, s.workspace
			}
			writeRequest(t, dir, map[string]any{"kind": "native", "argv": []string{binary, "harness", "native", dir}, "cwd": workspace, "stdin": ""})
			command := exec.Command(binary, "harness", "supervise", dir)
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			waitForFile(t, filepath.Join(dir, "identity.json"))
			if err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); err != nil {
				t.Fatal(err)
			}
			receipt := readReceipt(t, dir)
			if !valuesEqual(receipt["exit_code"], 130) || receipt["error"] != "owned job was cancelled" {
				t.Fatalf("cancel receipt: %v", receipt)
			}
			if _, err := settleNativeReceipt(dir, receipt); err == nil {
				t.Fatal("cancelled native output admitted")
			}
			process, err := readJSONFile(filepath.Join(dir, "native-process.json"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(textOf(getOf(process, "pid")))
			if err != nil || syscall.Kill(pid, 0) == nil {
				t.Fatalf("native process survived: %v %v", process, err)
			}
			if got, err := readJSONFile(filepath.Join(dir, "native-request.json")); err != nil || !valuesEqual(got, request) {
				t.Fatalf("cancel rewrote request: %v %v", got, err)
			}
		})
	}
}

// Public native jobs reject each wrong launch input before the peer can emit
// its canned session or write successful evidence; binding proofs stay valid.
func TestCanaryNativePeersValidateLaunchInputs(t *testing.T) {
	binary := supervisorBinary(t)
	for _, nativeHost := range []string{"dsh", "zcode"} {
		t.Run(nativeHost, func(t *testing.T) {
			cases := []string{"valid", "argv", "cwd", "home"}
			if nativeHost == "dsh" {
				cases = append(cases, "dsh-home")
			}
			for _, dimension := range cases {
				t.Run(dimension, func(t *testing.T) {
					home := resolveNativePath(t.TempDir())
					t.Setenv("HOME", home)
					t.Setenv("DSH_HOME", filepath.Join(home, ".dsh"))
					var request map[string]any
					var dir, workspace, echo string
					if nativeHost == "dsh" {
						s := newDSHScenario(t, successSteps(), func(request map[string]any) { request["model"], request["reasoning_effort"] = "test-model", "high" })
						request, dir, workspace, echo = s.request, s.directory, s.workspace, s.echoPath
					} else {
						s := newZcodeScenario(t, zcodeSuccessSteps, nil)
						request, dir, workspace, echo = s.request, s.directory, s.workspace, s.echoPath
					}
					binding := objectOrEmpty(request["binding"])
					launch := objectOrEmpty(binding["launch"])
					expected := peerScript{Workspace: workspace, Environment: map[string]string{"HOME": home}}
					if nativeHost == "dsh" {
						expected.Args = []string{"--profile", "acp", "--patch", filepath.Join(dir, "profile.json")}
						expected.Environment["DSH_HOME"] = filepath.Join(home, ".dsh")
					} else {
						expected.Args = []string{textOf(launch["script"]), "--cwd", workspace, "app-server"}
						binding["environment"] = map[string]any{"HOME": home}
					}
					scriptPath := os.Getenv("FORMAL_GATES_NATIVE_PEER_SCRIPT")
					body := readTestFileBytes(t, scriptPath)
					var script peerScript
					if err := json.Unmarshal(body, &script); err != nil {
						t.Fatal(err)
					}
					script.Args, script.Environment, script.Workspace = expected.Args, expected.Environment, expected.Workspace
					if err := writeAtomicJSON(scriptPath, script); err != nil {
						t.Fatal(err)
					}
					wrapper := textOf(launch["executable"])
					wrapperBody := readTestFile(t, wrapper)
					prefix, suffix, refusal := "", "", ""
					switch dimension {
					case "argv":
						suffix, refusal = " --unexpected", "native argv differs"
					case "cwd":
						prefix, refusal = "cd '"+resolveNativePath(t.TempDir())+"'\n", "native cwd differs"
					case "home":
						prefix, refusal = "export HOME='"+resolveNativePath(t.TempDir())+"'\n", "native env HOME differs"
					case "dsh-home":
						prefix, refusal = "export DSH_HOME='"+resolveNativePath(t.TempDir())+"'\n", "native env DSH_HOME differs"
					}
					wrapperBody = strings.Replace(wrapperBody, "#!/bin/sh\n", "#!/bin/sh\n"+prefix, 1)
					wrapperBody = strings.Replace(wrapperBody, "\"$@\"\n", "\"$@\""+suffix+"\n", 1)
					if err := os.WriteFile(wrapper, []byte(wrapperBody), 0755); err != nil {
						t.Fatal(err)
					}
					environment := binding["environment"]
					bound, err := buildCaptureBinding(nativeHost, launch)
					if err != nil {
						t.Fatal(err)
					}
					if environment != nil {
						bound["environment"] = environment
					}
					request["binding"] = bound
					impl, err := nativeImplementationFor(nativeHost)
					if err != nil {
						t.Fatal(err)
					}
					if err := impl.validateBinding(bound); err != nil {
						t.Fatalf("negative control has invalid binding: %v", err)
					}
					if err := writeAtomicJSON(filepath.Join(dir, "native-request.json"), request); err != nil {
						t.Fatal(err)
					}
					command := exec.Command(binary, "harness", "native", dir)
					command.Dir = workspace
					output, err := command.CombinedOutput()
					t.Logf("public native host=%s input=%s exit=%v output=%s", nativeHost, dimension, err, output)
					if dimension == "valid" {
						if err != nil {
							t.Fatal(err)
						}
						source, err := readJSONFile(filepath.Join(dir, "source.json"))
						if err != nil || getOf(source, "provider") != nativeHost {
							t.Fatalf("valid launch source: %v %v", source, err)
						}
						if err := impl.verifySource(dir, source); err != nil {
							t.Fatal(err)
						}
					} else {
						if err == nil {
							t.Fatal("wrong launch returned successful source")
						}
						stderr := readTestFile(t, filepath.Join(dir, "host.stderr"))
						if !strings.Contains(stderr, refusal) {
							t.Fatalf("expected peer launch refusal %q, got %s", refusal, stderr)
						}
						if _, err := os.Stat(filepath.Join(dir, "source.json")); !os.IsNotExist(err) {
							t.Fatal("wrong launch wrote successful source")
						}
						if _, err := os.Stat(filepath.Join(dir, "identity.json")); !os.IsNotExist(err) {
							t.Fatal("wrong launch emitted a session identity")
						}
						if _, err := os.Stat(echo); !os.IsNotExist(err) {
							t.Fatal("wrong launch processed RPC before refusal")
						}
					}
				})
			}
		})
	}
}
