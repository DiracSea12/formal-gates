package runner

// K4 repair acceptance tests, F2 — 调用接回 over real subprocesses and real
// durable state: an owner child really exits (os.Exit) right after its
// executor spawns a supervisor subprocess that owns the registered
// invocation — slowly writing its result, still sleeping, or dying with
// partial progress only.  The parent reopens the same directory, consults
// Takeover over the work artifacts, and must adopt the ORIGINAL call
// without re-executing, observe it while it lives (ErrCallObserved), or
// void the dead session and re-run under a NEW invocation id that picks
// the partial work up (§8.3).  TestMain re-executes this binary for both
// children (crash_test.go's appended branches); the takeover scenarios
// stay separate from revision — no generation ever moves.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	envK4TakeoverScenario = "RUNNER_K4_TAKEOVER"
	envK4SupervisorMode   = "RUNNER_K4_SUPERVISOR"
	envK4InvocationID     = "RUNNER_K4_INVOCATION"
	k4TakeoverRunID       = "k4-takeover-f2"
)

// Takeover-child exit codes: 98 is the owner's on-purpose exit right
// after spawning the supervisor; 99+ mark wiring bugs.  Codes stay clear
// of the 70–97 range K1–K4 use.
const (
	exitK4TakeoverCrash  = 98
	exitK4TakeoverWiring = 99
)

// k4TakeoverOwner creates the single-task run in RUNNER_K4_DIR and drives
// it until the executor — instead of executing — hands the registered
// invocation to a real supervisor subprocess and the owner really exits:
// the durable state is one pending generation-1 intent and a running
// task, with the supervisor working on outside this process.
func k4TakeoverOwner(scenario string) {
	dir := os.Getenv(envK4Dir)
	if dir == "" {
		fmt.Fprintln(os.Stderr, "owner: missing", envK4Dir)
		os.Exit(exitK4TakeoverWiring)
	}
	st, err := Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "owner open:", err)
		os.Exit(exitK4TakeoverWiring)
	}
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a")}}
	if err := st.CreateRun(context.Background(), k4TakeoverRunID, specs); err != nil {
		fmt.Fprintln(os.Stderr, "owner create:", err)
		os.Exit(exitK4TakeoverWiring)
	}
	r := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(),
			envK4SupervisorMode+"="+scenario,
			envK4Dir+"="+dir,
			envK4InvocationID+"="+inv.InvocationID)
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "owner spawn:", err)
			os.Exit(exitK4TakeoverWiring)
		}
		// The supervisor now owns the call; this owner is deliberately dead.
		os.Exit(exitK4TakeoverCrash)
		return Result{}, nil // unreachable
	})
	if err := r.Run(context.Background(), specs); err != nil {
		fmt.Fprintln(os.Stderr, "owner run:", err)
		os.Exit(exitK4TakeoverWiring)
	}
	fmt.Fprintln(os.Stderr, "owner: run completed without exiting at the handoff")
	os.Exit(exitK4TakeoverWiring)
}

// k4Supervisor is the real out-of-process worker for one invocation id:
// write-result slowly writes the result artifact and the marker;
// slow-result sleeps long enough that the parent's first advance observes
// the call still in flight; die-partial writes a session-invalid marker
// and partial progress, then dies without any result.
func k4Supervisor(mode string) {
	dir := os.Getenv(envK4Dir)
	inv := os.Getenv(envK4InvocationID)
	if dir == "" || inv == "" {
		fmt.Fprintln(os.Stderr, "supervisor: missing env")
		os.Exit(exitK4TakeoverWiring)
	}
	writeResult := func() {
		if err := os.WriteFile(filepath.Join(dir, "work-"+inv+".result"),
			[]byte(`"supervised-a"`), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "supervisor write:", err)
			os.Exit(exitK4TakeoverWiring)
		}
		if _, err := markerExecutor(dir)(context.Background(), Invocation{TaskID: "a"}); err != nil {
			fmt.Fprintln(os.Stderr, "supervisor marker:", err)
			os.Exit(exitK4TakeoverWiring)
		}
	}
	switch mode {
	case "write-result":
		time.Sleep(300 * time.Millisecond)
		writeResult()
	case "slow-result":
		time.Sleep(1500 * time.Millisecond)
		writeResult()
	case "die-partial":
		if err := os.WriteFile(filepath.Join(dir, "dead-"+inv), []byte("session-invalid"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "supervisor write:", err)
			os.Exit(exitK4TakeoverWiring)
		}
		if err := os.WriteFile(filepath.Join(dir, "partial-"+inv), []byte("half-done"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "supervisor write:", err)
			os.Exit(exitK4TakeoverWiring)
		}
	default:
		fmt.Fprintln(os.Stderr, "supervisor: unknown mode:", mode)
		os.Exit(exitK4TakeoverWiring)
	}
	os.Exit(0)
}

// runTakeoverOwner executes the owner child and asserts it exited exactly
// at the supervisor handoff.
func runTakeoverOwner(t *testing.T, dir, scenario string) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), envK4TakeoverScenario+"="+scenario, envK4Dir+"="+dir)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != exitK4TakeoverCrash {
		t.Fatalf("owner child exit = %v (want code %d); output:\n%s", err, exitK4TakeoverCrash, out)
	}
}

// waitForGlob polls until the supervisor's asynchronously written file
// appears and returns its path.
func waitForGlob(t *testing.T, dir, pattern string) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if m, _ := filepath.Glob(filepath.Join(dir, pattern)); len(m) > 0 {
			return m[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("supervisor never wrote %s within 15s", pattern)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// pendingInvocationID returns the task's single pending invocation id.
func pendingInvocationID(t *testing.T, st *Store, taskID string) string {
	t.Helper()
	var id string
	if err := st.db.QueryRow(`
		SELECT invocation_id FROM invocations
		 WHERE run_id=? AND task_id=? AND state=?`,
		st.runID, taskID, invPending).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// fileTakeover mirrors the K5 supervisor judgement over the work
// directory: a result artifact adopts the original call, a
// session-invalid marker means dead, anything else is still in flight.
func fileTakeover(dir string) Takeover {
	return func(_ context.Context, inv Invocation) (TakeoverOutcome, error) {
		if b, err := os.ReadFile(filepath.Join(dir, "work-"+inv.InvocationID+".result")); err == nil {
			return TakeoverOutcome{Adopt: true, Result: Result{Payload: json.RawMessage(b)}}, nil
		} else if !os.IsNotExist(err) {
			return TakeoverOutcome{}, err
		}
		if _, err := os.Stat(filepath.Join(dir, "dead-"+inv.InvocationID)); err == nil {
			return TakeoverOutcome{}, nil // dead/invalidated session
		} else if !os.IsNotExist(err) {
			return TakeoverOutcome{}, err
		}
		return TakeoverOutcome{Observe: true}, nil
	}
}

// takeoverSpecs are the run's unchanged specs: adoption voids and adopts
// nothing but the pending call, so the input never moves.
func takeoverSpecs() []TaskSpec {
	return []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a")}}
}

// 结果已存在→接纳原调用: the supervisor finished after the owner died; the
// reopened parent adopts the original invocation — same id, same stored
// result, marker exactly once, zero re-execution, task finished — and no
// generation moves (plain takeover and revision stay separate).
func TestK4TakeoverAdoptsExistingResultWithoutReExecution(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	runTakeoverOwner(t, dir, "write-result")
	waitForGlob(t, dir, "work-*.result")
	if n := markerLines(t, dir, "a"); n != 1 {
		t.Fatalf("supervisor marker lines before takeover = %d, want 1", n)
	}

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.runID != k4TakeoverRunID {
		t.Fatalf("reopened run id = %q, want %q", st.runID, k4TakeoverRunID)
	}
	orig := pendingInvocationID(t, st, "a")
	parentExecs := 0
	r := NewRunner(st, func(ctx context.Context, inv Invocation) (Result, error) {
		parentExecs++
		return markerExecutor(dir)(ctx, inv)
	})
	r.Takeover = fileTakeover(dir)
	if err := r.Run(ctx, takeoverSpecs()); err != nil {
		t.Fatal(err)
	}
	if parentExecs != 0 {
		t.Errorf("parent re-executed task a %d times on adoption, want 0", parentExecs)
	}
	if n := markerLines(t, dir, "a"); n != 1 {
		t.Errorf("marker lines after adoption = %d, want still exactly 1", n)
	}
	if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
		t.Errorf("task a admitted %d times, want 1", n)
	}
	if id, gen, _ := admittedInvocation(t, st, "a"); id != orig || gen != 1 {
		t.Errorf("admitted invocation = (%q, gen %d), want the ORIGINAL pending call %q at gen 1", id, gen, orig)
	}
	if state, res := invocationState(t, st, orig); state != invAdmitted || res != `{"payload":"supervised-a"}` {
		t.Errorf("original invocation row = (%q,%q), want (admitted, the supervised result)", state, res)
	}
	if s, eff := taskState(t, st, "a"); s != taskFinished || eff != orig {
		t.Errorf("task a = (%q,%q), want finished with the original call effective", s, eff)
	}
	if n := invocationTotal(t, st); n != 1 {
		t.Errorf("invocation rows = %d, want exactly the original one (no re-registration)", n)
	}
	if gen := taskGeneration(t, st, "a"); gen != 1 {
		t.Errorf("task a generation = %d after takeover, want 1 (no revision involved)", gen)
	}
}

// 原调用仍在→观察: the supervisor is still sleeping when the parent first
// advances: the pending invocation survives, no second executor starts
// and settle reports ErrCallObserved; once the supervisor finishes, the
// next advance adopts the original result.
func TestK4TakeoverObservesOriginalCallStillInFlight(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	runTakeoverOwner(t, dir, "slow-result")

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	orig := pendingInvocationID(t, st, "a")
	parentExecs := 0
	r := NewRunner(st, func(ctx context.Context, inv Invocation) (Result, error) {
		parentExecs++
		return markerExecutor(dir)(ctx, inv)
	})
	r.Takeover = fileTakeover(dir)
	if err := r.Run(ctx, takeoverSpecs()); !errors.Is(err, ErrCallObserved) {
		t.Fatalf("first run err = %v, want ErrCallObserved", err)
	}
	if n := countInvocations(t, st, "a", invPending); n != 1 {
		t.Errorf("pending invocations after observation = %d, want still 1", n)
	}
	if s, _ := taskState(t, st, "a"); s != taskRunning {
		t.Errorf("task a state = %q under observation, want running (out of the ready set)", s)
	}
	if n := invocationTotal(t, st); n != 1 {
		t.Errorf("invocation rows after observation = %d, want 1 (zero re-dispatch)", n)
	}
	if parentExecs != 0 {
		t.Errorf("parent executed task a %d times while observing, want 0", parentExecs)
	}
	if n := markerLines(t, dir, "a"); n != 0 {
		t.Errorf("marker lines while the supervisor still works = %d, want 0", n)
	}

	// The supervisor finishes; the original call is adopted, not re-run.
	waitForGlob(t, dir, "work-*.result")
	if err := r.Run(ctx, takeoverSpecs()); err != nil {
		t.Fatal(err)
	}
	if parentExecs != 0 {
		t.Errorf("parent executed task a %d times after the wait, want 0 (adoption)", parentExecs)
	}
	if id, _, _ := admittedInvocation(t, st, "a"); id != orig {
		t.Errorf("admitted invocation = %q, want the observed original %q", id, orig)
	}
	if n := markerLines(t, dir, "a"); n != 1 {
		t.Errorf("marker lines after adoption = %d, want exactly the supervisor's 1", n)
	}
}

// 失效会话＋部分工作接手: the supervisor wrote partial progress and a
// session-invalid marker, then died without a result: the zero outcome
// voids the original invocation (row kept, superseded, result-less) and
// the re-run registers a NEW invocation id — the retry carries the
// newest native identity, not the invalidated one — whose executor picks
// the partial work up (§8.3's minimal proof on the kernel's identity
// machinery).
func TestK4TakeoverDeadSessionRerunsUnderNewIdentity(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	runTakeoverOwner(t, dir, "die-partial")
	waitForGlob(t, dir, "dead-*")

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	orig := pendingInvocationID(t, st, "a")
	r := NewRunner(st, func(ctx context.Context, inv Invocation) (Result, error) {
		partials, err := filepath.Glob(filepath.Join(dir, "partial-*"))
		if err != nil {
			return Result{}, err
		}
		carry := ""
		if len(partials) > 0 {
			b, err := os.ReadFile(partials[0])
			if err != nil {
				return Result{}, err
			}
			carry = "+" + string(b)
		}
		if _, err := markerExecutor(dir)(ctx, inv); err != nil {
			return Result{}, err
		}
		return Result{Payload: json.RawMessage(`"rerun-a` + carry + `"`)}, nil
	})
	r.Takeover = fileTakeover(dir)
	if err := r.Run(ctx, takeoverSpecs()); err != nil {
		t.Fatal(err)
	}
	// The dead session's row survives as voided history.
	if state, res := invocationState(t, st, orig); state != invSuperseded || res != "" {
		t.Errorf("dead session row = (%q,%q), want (superseded, no result)", state, res)
	}
	newID, gen, _ := admittedInvocation(t, st, "a")
	if newID == orig {
		t.Fatal("re-run reused the invalidated invocation id; the retry must carry a fresh native identity")
	}
	if gen != 1 {
		t.Errorf("re-run generation = %d, want 1 (void-and-rerun, no revision)", gen)
	}
	if _, res := invocationState(t, st, newID); res != `{"payload":"rerun-a+half-done"}` {
		t.Errorf("re-run result = %s, want the partial work carried in", res)
	}
	if n := markerLines(t, dir, "a"); n != 1 {
		t.Errorf("marker lines = %d, want exactly the re-run's 1", n)
	}
	if n := invocationTotal(t, st); n != 2 {
		t.Errorf("invocation rows = %d, want 2 (dead original plus re-run)", n)
	}
	if s, eff := taskState(t, st, "a"); s != taskFinished || eff != newID {
		t.Errorf("task a = (%q,%q), want finished with the re-run effective", s, eff)
	}
}

// A nil Takeover keeps the K1 recovery byte for byte on the same durable
// state: the pending invocation is superseded and the task re-executed —
// the contrast that proves the consult, not the state, makes the
// difference.
func TestK4NilTakeoverSupersedesAsBefore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	runTakeoverOwner(t, dir, "slow-result") // the supervisor will land late; irrelevant here
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	orig := pendingInvocationID(t, st, "a")
	r := NewRunner(st, markerExecutor(dir)) // Takeover deliberately nil
	if err := r.Run(ctx, takeoverSpecs()); err != nil {
		t.Fatal(err)
	}
	if state, _ := invocationState(t, st, orig); state != invSuperseded {
		t.Errorf("orphaned invocation state after nil-Takeover recovery = %q, want superseded", state)
	}
	if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
		t.Errorf("task a admitted %d times, want 1 (re-registered and re-executed)", n)
	}
	if id, _, _ := admittedInvocation(t, st, "a"); id == orig {
		t.Error("nil-Takeover recovery admitted the orphaned invocation itself; want a fresh registration")
	}
}
