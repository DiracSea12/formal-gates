// Package testkit contains test-only adapters for the phase-2 engine protocol.
// It deliberately has no public workflow or CLI write entry: callers must
// construct an Engine and point it at an explicitly isolated state directory.
package testkit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
)

var ErrInjected = errors.New("testkit: injected fault")

// FaultPlan is a deterministic, named, single-trigger fault controller. A
// point is armed once and consumes itself on the first matching call. The
// call log is useful in tests because it proves which boundary was exercised.
type FaultPlan struct {
	mu        sync.Mutex
	armed     map[persistence.FaultPoint]error
	calls     []persistence.FaultPoint
	triggered []persistence.FaultPoint
	consumed  map[persistence.FaultPoint]int
}

func NewFaultPlan() *FaultPlan {
	return &FaultPlan{
		armed:    map[persistence.FaultPoint]error{},
		consumed: map[persistence.FaultPoint]int{},
	}
}

func (p *FaultPlan) Arm(point persistence.FaultPoint) { p.ArmError(point, ErrInjected) }

// ArmCrash leaves persistence protocol artifacts for a subsequent Recover,
// matching a process loss at the named boundary.
func (p *FaultPlan) ArmCrash(point persistence.FaultPoint) {
	p.ArmError(point, &persistence.InjectedCrashError{Point: point})
}

func (p *FaultPlan) ArmError(point persistence.FaultPoint, err error) {
	if err == nil {
		err = ErrInjected
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.armed[point] = err
}

func (p *FaultPlan) Inject(point persistence.FaultPoint) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, point)
	err, ok := p.armed[point]
	if !ok {
		return nil
	}
	delete(p.armed, point)
	p.consumed[point]++
	p.triggered = append(p.triggered, point)
	return err
}

func (p *FaultPlan) Calls() []persistence.FaultPoint {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]persistence.FaultPoint(nil), p.calls...)
}

func (p *FaultPlan) Count(point persistence.FaultPoint) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.consumed[point]
}

// TriggeredCalls reports only armed fault points that actually interrupted a
// boundary. Calls() intentionally remains the raw injection census for unit
// tests; harness reports use this admission-facing view.
func (p *FaultPlan) TriggeredCalls() []persistence.FaultPoint {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]persistence.FaultPoint(nil), p.triggered...)
}

func (p *FaultPlan) PersistenceInjector() func(persistence.FaultPoint) error {
	return p.Inject
}

// FakeHost models the host side of spawn and HostAction protocols. It counts
// side effects before returning receipts, so a lost response can be retried
// and checked for duplicate execution.
type FakeHost struct {
	Engine   *protocol.Engine
	Provider string
	Faults   *FaultPlan
	VCS      *FakeVCS

	seq          atomic.Uint64
	mu           sync.Mutex
	spawnCalls   map[string]int
	spawned      map[string]string
	actionCalls  map[string]int
	observations map[string]string
}

func NewFakeHost(engine *protocol.Engine, provider string, faults *FaultPlan) *FakeHost {
	if faults == nil {
		faults = NewFaultPlan()
	}
	return &FakeHost{
		Engine: engine, Provider: provider, Faults: faults,
		spawnCalls: map[string]int{}, spawned: map[string]string{},
		actionCalls: map[string]int{}, observations: map[string]string{},
	}
}

type SpawnResult struct {
	Acceptance protocol.Acceptance
	ActionID   string
	Identity   string
	Calls      int
}

func (h *FakeHost) Spawn(actionID, identity string, status string) (SpawnResult, error) {
	eventID := fmt.Sprintf("fake-host-spawn-%d", h.seq.Add(1))
	return h.SpawnEvent(protocol.EventID(eventID), actionID, identity, status)
}

// SpawnEvent is Spawn with a caller-owned event ID. It lets the installed
// harness replay the exact same receipt across process boundaries.
func (h *FakeHost) SpawnEvent(eventID protocol.EventID, actionID, identity, status string) (SpawnResult, error) {
	return h.spawnEvent(eventID, actionID, identity, status, false)
}

// SpawnEventWithoutSubmitResponseFault is used by the response-loss harness
// scenario to reserve the shared response-loss point for the worker result
// that follows this already committed spawn receipt.
func (h *FakeHost) SpawnEventWithoutSubmitResponseFault(eventID protocol.EventID, actionID, identity, status string) (SpawnResult, error) {
	return h.spawnEvent(eventID, actionID, identity, status, true)
}

func (h *FakeHost) spawnEvent(eventID protocol.EventID, actionID, identity, status string, skipSubmitResponseFault bool) (SpawnResult, error) {
	if h.Engine == nil {
		return SpawnResult{}, fmt.Errorf("testkit: fake host engine is required")
	}
	if status == "" {
		status = protocol.SpawnStatusSpawned
	}
	if h.VCS != nil {
		name := sha256.Sum256([]byte(actionID))
		calls, err := h.VCS.ApplyOnce("fake-host.spawn:"+actionID, filepath.Join("fake-host", "spawn", hex.EncodeToString(name[:])+".txt"), []byte(identity+"\n"))
		if err != nil {
			return SpawnResult{}, err
		}
		h.mu.Lock()
		h.spawnCalls[actionID] = calls
		h.mu.Unlock()
	}
	h.mu.Lock()
	identityAlreadySpawned := h.spawned[actionID]
	calls := h.spawnCalls[actionID]
	if identityAlreadySpawned == "" {
		h.spawned[actionID] = identity
		if h.VCS == nil {
			h.spawnCalls[actionID]++
		}
		calls = h.spawnCalls[actionID]
	}
	h.mu.Unlock()
	if identityAlreadySpawned != "" && identityAlreadySpawned != identity {
		return SpawnResult{ActionID: actionID, Identity: identity, Calls: calls}, fmt.Errorf("testkit: action %q already spawned as %q", actionID, identityAlreadySpawned)
	}
	if fault := h.Faults.Inject(persistence.FaultSpawnAfterAttach); fault != nil {
		return SpawnResult{ActionID: actionID, Identity: identity, Calls: calls}, fault
	}
	ev, err := protocol.NewSpawnReceiptEvent(eventID, actionID, h.Provider, identity, status)
	if err != nil {
		return SpawnResult{}, err
	}
	fp, err := h.Engine.ObserveFingerprint()
	if err != nil {
		return SpawnResult{}, err
	}
	acceptance, err := h.Engine.Submit(ev, fp)
	if err != nil {
		return SpawnResult{Acceptance: acceptance, ActionID: actionID, Identity: identity, Calls: calls}, err
	}
	if !skipSubmitResponseFault {
		if fault := h.Faults.Inject(persistence.FaultSubmitResponseLost); fault != nil {
			return SpawnResult{Acceptance: acceptance, ActionID: actionID, Identity: identity, Calls: calls}, fault
		}
	}
	return SpawnResult{Acceptance: acceptance, ActionID: actionID, Identity: identity, Calls: calls}, nil
}

func (h *FakeHost) SpawnCalls(actionID string) int {
	if h.VCS != nil {
		count, err := h.VCS.OperationCount("fake-host.spawn:" + actionID)
		if err == nil {
			return count
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.spawnCalls[actionID]
}

type HostActionResult struct {
	Intent      protocol.HostActionIntent
	Acceptance  protocol.Acceptance
	SideEffects int
}

func (h *FakeHost) Execute(operation authoring.OperationID, params any) (protocol.HostActionIntent, error) {
	fp, err := h.Engine.ObserveFingerprint()
	if err != nil {
		return protocol.HostActionIntent{}, err
	}
	intent, _, err := h.Engine.ExecuteHostAction(operation, params, fp)
	if err != nil {
		return protocol.HostActionIntent{}, err
	}
	if h.VCS != nil {
		name := sha256.Sum256([]byte(intent.ActionID))
		calls, applyErr := h.VCS.ApplyOnce("fake-host.action:"+intent.ActionID, filepath.Join("fake-host", "action", hex.EncodeToString(name[:])+".txt"), []byte(string(operation)+"\n"))
		if applyErr != nil {
			return intent, applyErr
		}
		h.mu.Lock()
		h.actionCalls[intent.ActionID] = calls
		h.mu.Unlock()
	} else {
		h.mu.Lock()
		h.actionCalls[intent.ActionID]++
		h.mu.Unlock()
	}
	if err := h.Faults.Inject(persistence.FaultHostCommitBefore); err != nil {
		return intent, err
	}
	if err := h.Faults.Inject(persistence.FaultHostCommitAfter); err != nil {
		return intent, err
	}
	return intent, nil
}

func (h *FakeHost) ActionCalls(actionID string) int {
	if h.VCS != nil {
		count, err := h.VCS.OperationCount("fake-host.action:" + actionID)
		if err == nil {
			return count
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.actionCalls[actionID]
}

func (h *FakeHost) Receipt(intent protocol.HostActionIntent, status string) (protocol.Acceptance, error) {
	return h.ReceiptWithCorrelation(intent, status, "")
}

func (h *FakeHost) ReceiptWithCorrelation(intent protocol.HostActionIntent, status, correlation string) (protocol.Acceptance, error) {
	if status == "" {
		status = protocol.HostActionStatusExecuted
	}
	seq := h.seq.Add(1)
	if correlation == "" {
		correlation = fmt.Sprintf("fake-correlation-%d", seq)
	}
	return h.ReceiptEvent(protocol.EventID(fmt.Sprintf("fake-host-receipt-%d", seq)), intent, status, correlation)
}

// ReceiptEvent is ReceiptWithCorrelation with a caller-owned event ID.
func (h *FakeHost) ReceiptEvent(eventID protocol.EventID, intent protocol.HostActionIntent, status, correlation string) (protocol.Acceptance, error) {
	if status == "" {
		status = protocol.HostActionStatusExecuted
	}
	if intent.Adapter == nil {
		return protocol.Acceptance{}, fmt.Errorf("testkit: ReceiptWithCorrelation requires adapter HostAction intent")
	}
	ev, err := protocol.NewHostActionReceiptEvent(
		eventID, intent.ActionID,
		intent.Adapter.Operation, h.Provider, correlation,
		intent.PayloadDigest, status,
	)
	if err != nil {
		return protocol.Acceptance{}, err
	}
	fp, err := h.Engine.ObserveFingerprint()
	if err != nil {
		return protocol.Acceptance{}, err
	}
	acceptance, err := h.Engine.Submit(ev, fp)
	if err != nil {
		return acceptance, err
	}
	if fault := h.Faults.Inject(persistence.FaultSubmitResponseLost); fault != nil {
		return acceptance, fault
	}
	return acceptance, nil
}

func (h *FakeHost) Observe(actionID, digest string) (string, error) {
	if err := h.Faults.Inject(persistence.FaultHostObserveBefore); err != nil {
		return "", err
	}
	h.mu.Lock()
	h.observations[actionID] = digest
	h.mu.Unlock()
	return digest, nil
}

func (h *FakeHost) Reconcile(actionID, digest string, fulfilled, conflict bool) (protocol.RecoveryPlan, error) {
	if err := h.Faults.Inject(persistence.FaultHostReconcileBefore); err != nil {
		return protocol.RecoveryPlan{}, err
	}
	fp, err := h.Engine.ObserveFingerprint()
	if err != nil {
		return protocol.RecoveryPlan{}, err
	}
	plan, _, err := h.Engine.ReconcileHostAction(actionID, digest, fulfilled, conflict, fp)
	return plan, err
}

// FakeWorker submits typed results and deliberately supports result-before-
// receipt. The response-loss point fires after Engine.Submit has committed.
type FakeWorker struct {
	Engine   *protocol.Engine
	Provider string
	Faults   *FaultPlan
	seq      atomic.Uint64
}

func NewFakeWorker(engine *protocol.Engine, provider string, faults *FaultPlan) *FakeWorker {
	if faults == nil {
		faults = NewFaultPlan()
	}
	return &FakeWorker{Engine: engine, Provider: provider, Faults: faults}
}

func (w *FakeWorker) Result(actionID, outcome, payloadDigest string, class authoring.FailureClass) (protocol.Acceptance, error) {
	wireID := protocol.EventID(fmt.Sprintf("fake-worker-result-%d", w.seq.Add(1)))
	return w.ResultEvent(wireID, actionID, outcome, payloadDigest, class)
}

// ResultEvent submits a typed worker result with a caller-owned event ID.
func (w *FakeWorker) ResultEvent(eventID protocol.EventID, actionID, outcome, payloadDigest string, class authoring.FailureClass) (protocol.Acceptance, error) {
	ev, err := protocol.NewWorkerResultEvent(eventID, actionID, w.Provider, outcome, payloadDigest, class)
	if err != nil {
		return protocol.Acceptance{}, err
	}
	fp, err := w.Engine.ObserveFingerprint()
	if err != nil {
		return protocol.Acceptance{}, err
	}
	acceptance, err := w.Engine.Submit(ev, fp)
	if err != nil {
		return acceptance, err
	}
	if fault := w.Faults.Inject(persistence.FaultSubmitResponseLost); fault != nil {
		return acceptance, fault
	}
	return acceptance, nil
}

// ResultBeforeReceipt is named for the protocol boundary it exercises: the
// worker result is submitted before the host sends the SpawnReceipt.
func (w *FakeWorker) ResultBeforeReceipt(actionID, outcome, payloadDigest string, class authoring.FailureClass) (protocol.Acceptance, error) {
	if fault := w.Faults.Inject(persistence.FaultResultBeforeReceipt); fault != nil {
		return protocol.Acceptance{}, fault
	}
	return w.Result(actionID, outcome, payloadDigest, class)
}

func (w *FakeWorker) SubmitEvent(ev protocol.Event) (protocol.Acceptance, error) {
	fp, err := w.Engine.ObserveFingerprint()
	if err != nil {
		return protocol.Acceptance{}, err
	}
	acceptance, err := w.Engine.Submit(ev, fp)
	if err != nil {
		return acceptance, err
	}
	if fault := w.Faults.Inject(persistence.FaultSubmitResponseLost); fault != nil {
		return acceptance, fault
	}
	return acceptance, nil
}

// SubmitConcurrently runs two deterministic callers against one state
// directory. The persistence lock/CAS protocol decides the winner; callers
// receive both results without test timing or sleep-based races.
func SubmitConcurrently(first, second func() (protocol.Acceptance, error)) [2]error {
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	done.Add(2)
	var results [2]error
	go func() { defer done.Done(); start.Wait(); _, results[0] = first() }()
	go func() { defer done.Done(); start.Wait(); _, results[1] = second() }()
	start.Done()
	done.Wait()
	return results
}

// StateSummary is the stable, human-readable projection used by harness
// reports. It intentionally reports ledger counts instead of private state
// representation details.
type StateSummary struct {
	Revision          uint64 `json:"revision"`
	Expected          int    `json:"expected"`
	Attempts          int    `json:"attempts"`
	PendingActions    int    `json:"pendingActions"`
	PendingResults    int    `json:"pendingResults"`
	Results           int    `json:"results"`
	SpawnReceipts     int    `json:"spawnReceipts"`
	PendingHostAction int    `json:"pendingHostActions"`
	HostReceipts      int    `json:"hostActionReceipts"`
	RecoveryRecords   int    `json:"recoveryRecords"`
}

func Summarize(engine *protocol.Engine) (StateSummary, error) {
	snapshot, err := engine.Load()
	if err != nil {
		return StateSummary{}, err
	}
	state := snapshot.State
	return StateSummary{
		Revision: snapshot.Revision, Expected: len(state.Expected), Attempts: len(state.Attempts),
		PendingActions: len(state.PendingActions), PendingResults: len(state.StagedResults),
		Results: len(state.Results), SpawnReceipts: len(state.SpawnReceipts),
		PendingHostAction: len(state.PendingHostActions), HostReceipts: len(state.HostActionReceipts),
		RecoveryRecords: len(state.RecoveryRecords),
	}, nil
}

// FakeVCS provides the small VCS surface needed by isolated harnesses: track
// explicit delivery paths, take content snapshots, compute ownership-aware
// diffs, and create deterministic commit IDs. It never touches a real VCS.
type FakeVCS struct {
	Root    string
	mu      sync.Mutex
	tracked map[string]struct{}
	commits []FakeCommit
}

type fakeVCSOperation struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Calls int    `json:"calls"`
}

type FakeCommit struct {
	ID      string
	Message string
	Digest  string
}

type FileSnapshot map[string]string

type FileChange struct {
	Path   string
	Before string
	After  string
}

func NewFakeVCS(root string) (*FakeVCS, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, err
	}
	return &FakeVCS{Root: abs, tracked: map[string]struct{}{}}, nil
}

func (v *FakeVCS) Track(paths ...string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, path := range paths {
		rel, err := v.relative(path)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(v.Root, rel)); err != nil {
			return err
		}
		v.tracked[rel] = struct{}{}
	}
	return nil
}

func (v *FakeVCS) Tracked() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	paths := make([]string, 0, len(v.tracked))
	for path := range v.tracked {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (v *FakeVCS) Snapshot() (FileSnapshot, error) {
	files := FileSnapshot{}
	err := filepath.WalkDir(v.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != v.Root && (entry.Name() == ".git" || entry.Name() == ".gates") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(v.Root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	return files, err
}

func (v *FakeVCS) Diff(before, after FileSnapshot) []FileChange {
	paths := map[string]struct{}{}
	for path := range before {
		paths[path] = struct{}{}
	}
	for path := range after {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	changes := make([]FileChange, 0, len(ordered))
	for _, path := range ordered {
		if before[path] != after[path] {
			changes = append(changes, FileChange{Path: path, Before: before[path], After: after[path]})
		}
	}
	return changes
}

func (v *FakeVCS) Commit(message string) (FakeCommit, error) {
	snapshot, err := v.Snapshot()
	if err != nil {
		return FakeCommit{}, err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return FakeCommit{}, err
	}
	sum := sha256.Sum256(append(data, []byte("\n"+message)...))
	digest := sha256.Sum256(data)
	commit := FakeCommit{ID: hex.EncodeToString(sum[:]), Message: message, Digest: hex.EncodeToString(digest[:])}
	v.mu.Lock()
	v.commits = append(v.commits, commit)
	v.mu.Unlock()
	return commit, nil
}

func (v *FakeVCS) Fingerprint() (string, error) {
	snapshot, err := v.Snapshot()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (v *FakeVCS) Write(path string, data []byte) error {
	rel, err := v.relative(path)
	if err != nil {
		return err
	}
	full := filepath.Join(v.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o600)
}

// ApplyOnce performs one project-local fake VCS side effect and records a
// durable call count. Replaying the same operation after a harness restart is
// a no-op, which makes at-most-once recovery externally observable.
func (v *FakeVCS) ApplyOnce(name, path string, data []byte) (int, error) {
	if strings.TrimSpace(name) == "" {
		return 0, fmt.Errorf("testkit: fake VCS operation name is required")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	ledgerPath := filepath.Join(v.Root, ".fake-vcs-operations.json")
	operations := map[string]fakeVCSOperation{}
	if encoded, err := os.ReadFile(ledgerPath); err == nil {
		if err := json.Unmarshal(encoded, &operations); err != nil {
			return 0, err
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	if operation, ok := operations[name]; ok {
		return operation.Calls, nil
	}
	rel, err := v.relative(path)
	if err != nil {
		return 0, err
	}
	full := filepath.Join(v.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return 0, err
	}
	if err := os.WriteFile(full, data, 0o600); err != nil {
		return 0, err
	}
	operations[name] = fakeVCSOperation{Name: name, Path: rel, Calls: 1}
	encoded, err := json.MarshalIndent(operations, "", "  ")
	if err != nil {
		return 0, err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(ledgerPath, encoded, 0o600); err != nil {
		return 0, err
	}
	return 1, nil
}

func (v *FakeVCS) OperationCount(name string) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(v.Root, ".fake-vcs-operations.json"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	operations := map[string]fakeVCSOperation{}
	if err := json.Unmarshal(data, &operations); err != nil {
		return 0, err
	}
	return operations[name].Calls, nil
}

func (v *FakeVCS) relative(path string) (string, error) {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(v.Root, abs)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(v.Root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("testkit: path %q is outside fake VCS root", path)
	}
	return filepath.ToSlash(rel), nil
}

// IsolatedProject owns all writable namespaces used by an installed harness.
// Stable paths are intentionally siblings, making accidental writes visible
// in a before/after snapshot without requiring a real registry.
type IsolatedProject struct {
	Root        string
	HostConfig  string
	State       string
	Resources   string
	Stable      string
	StableState string
	StableRun   string
}

func NewIsolatedProject(parent string) (*IsolatedProject, error) {
	root, err := os.MkdirTemp(parent, "engine-test-project-")
	if err != nil {
		return nil, err
	}
	p := &IsolatedProject{
		Root:        root,
		HostConfig:  filepath.Join(root, "host-config"),
		State:       filepath.Join(root, "state"),
		Resources:   filepath.Join(root, "resources"),
		Stable:      filepath.Join(root, "stable"),
		StableState: filepath.Join(root, "stable", "state"),
		StableRun:   filepath.Join(root, "stable", "run"),
	}
	for _, dir := range []string{p.HostConfig, p.State, p.Resources, p.StableState, p.StableRun} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *IsolatedProject) Snapshot() (FileSnapshot, error) {
	vcs, err := NewFakeVCS(p.Root)
	if err != nil {
		return nil, err
	}
	return vcs.Snapshot()
}

func (p *IsolatedProject) RunInstalled(binary string, args ...string) ([]byte, error) {
	cmd := exec.Command(binary, args...)
	cmd.Dir = p.Root
	cmd.Env = append(os.Environ(),
		"FORMAL_GATES_TEST_PROJECT="+p.Root,
		"FORMAL_GATES_HOST_CONFIG="+p.HostConfig,
		"FORMAL_GATES_ENGINE_STATE="+p.State,
		"FORMAL_GATES_ENGINE_RESOURCES="+p.Resources,
	)
	return cmd.CombinedOutput()
}

// NewProtocolFixture constructs a fully isolated engine fixture without
// reaching the legacy runtime or a stable registry.
type ProtocolFixture struct {
	Root       string
	Store      *persistence.Store
	Engine     *protocol.Engine
	Faults     *FaultPlan
	Host       *FakeHost
	Worker     *FakeWorker
	VCS        *FakeVCS
	Definition *compiler.CompiledDefinition
	Registry   *compiler.Registry
	Capacity   int
}

func NewProtocolFixture(root string) (*ProtocolFixture, error) {
	compiled, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		return nil, err
	}
	return NewProtocolFixtureWithDefinition(root, compiled, definition.Registry(), 4)
}

// NewProtocolFixtureWithDefinition is the test-only constructor for scenarios
// that need a small, explicit definition fixture (for example a bounded retry
// policy). The normal harness remains bound to definition.Workflow().
func NewProtocolFixtureWithDefinition(root string, compiled *compiler.CompiledDefinition, registry *compiler.Registry, capacity int) (*ProtocolFixture, error) {
	if compiled == nil || registry == nil {
		return nil, fmt.Errorf("testkit: compiled definition and registry are required")
	}
	if capacity < 0 {
		return nil, fmt.Errorf("testkit: fixture capacity %d invalid", capacity)
	}
	faults := NewFaultPlan()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return nil, err
	}
	vcs, err := NewFakeVCS(workspace)
	if err != nil {
		return nil, err
	}
	stateDir := filepath.Join(root, "engine-state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	store, err := persistence.NewStore(stateDir, persistence.Config{PackageDigest: "sha256:testkit", FaultInjector: faults.PersistenceInjector()})
	if err != nil {
		return nil, err
	}
	engine, err := protocol.New(store, protocol.Config{
		Definition: compiled, Registry: registry, Capacity: capacity,
	}, vcs.Fingerprint)
	if err != nil {
		return nil, err
	}
	fixture := &ProtocolFixture{Root: root, Store: store, Engine: engine, Faults: faults, VCS: vcs, Definition: compiled, Registry: registry, Capacity: capacity}
	fixture.Host = NewFakeHost(engine, "fake-host", faults)
	fixture.Worker = NewFakeWorker(engine, "fake-host", faults)
	return fixture, nil
}

func (f *ProtocolFixture) Restart() (*ProtocolFixture, persistence.RecoveryReport, error) {
	report, err := f.Store.Recover()
	if err != nil {
		return nil, report, err
	}
	restarted, err := NewProtocolFixtureWithDefinition(f.Root, f.Definition, f.Registry, f.Capacity)
	return restarted, report, err
}

func (f *ProtocolFixture) Initialize(view *decision.State, provider string) error {
	fingerprint, err := f.Engine.ObserveFingerprint()
	if err != nil {
		return err
	}
	return f.Engine.Init(view, provider, fingerprint)
}

func (f *ProtocolFixture) PrepareReady() (decision.IssuedSet, error) {
	if f.Definition == nil {
		return nil, fmt.Errorf("testkit: fixture definition is required")
	}
	view, err := decision.NewState(f.Definition.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		return nil, err
	}
	compiled := f.Definition
	for _, step := range []authoring.StepID{"entry.parse", "entry.persist"} {
		found := false
		for _, candidate := range compiled.Steps {
			if candidate.Header.ID == step {
				found = true
				break
			}
		}
		if found {
			if err := view.CompleteStep(step, compiled); err != nil {
				return nil, err
			}
		}
	}
	if err := f.Initialize(view, "fake-host"); err != nil {
		return nil, err
	}
	plan, err := decision.Decide(view, decision.Observation{}, compiled)
	if err != nil {
		return nil, err
	}
	fingerprint, err := f.Engine.ObserveFingerprint()
	if err != nil {
		return nil, err
	}
	issued, _, err := f.Engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fingerprint)
	return issued, err
}

// Keep the imported runtime visible to package consumers that build fixtures
// around the checked-in task key without duplicating its import path.
func TaskKey(node, step string) runtime.TaskKey {
	return runtime.TaskKey{Node: authoring.NodeID(node), Step: authoring.StepID(step)}
}
