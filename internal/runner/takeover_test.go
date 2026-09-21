package runner

// K4 rework acceptance tests, F2/F4/F5 — 调用接回 over real subprocesses
// and real durable state: an owner child really exits (os.Exit) right
// after its executor spawns a supervisor subprocess that owns the
// registered invocation — slowly writing its result, still sleeping, or
// dying with partial progress only.  The parent reopens the same
// directory, consults Takeover over the work artifacts, and must adopt
// the ORIGINAL call without re-executing, or observe it while it lives
// (ErrCallObserved, the fact PERSISTED so a later nil-consult owner
// gets ErrTakeoverRequired instead of a blind retry), while a dead
// session is a FACT that asks through a restart request — only an
// allowing answer replaces the execution identity (§8.2/§8.3).
// TestMain re-executes this binary for the children (crash_test.go's
// appended branches); the takeover scenarios stay separate from revision
// — no generation ever moves.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// waitForMarker polls until the task's marker file holds exactly want
// execution lines.  The supervisor's writeResult writes the result
// artifact first and appends the marker line last, so the marker is its
// final durable write: waiting for it fences the result that precedes
// it.  Polling the result file instead (waitForGlob) leaves a window
// where the marker has not landed yet, and a same-instant markerLines
// assertion right after would flake.
func waitForMarker(t *testing.T, dir, task string, want int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if n := markerLines(t, dir, task); n == want {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("marker lines for task %q in %s = %d after 15s, want %d", task, dir, n, want)
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

// restartQuestion is the parent's Restart declaration for the
// dead-session asks: restart-session allows the replacement, abort
// refuses it (§8.2/§8.3).
func restartQuestion() *Confirmation {
	return &Confirmation{
		Options:  []string{"restart-session", "abort"},
		Allow:    "restart-session",
		Revision: "rev-1",
		Body:     []byte("the session owning task a's call is dead — restart it under a new identity?"),
	}
}

// 结果已存在→接纳原调用: the supervisor finished after the owner died; the
// reopened parent adopts the original invocation — same id, same stored
// result, marker exactly once, zero re-execution, task finished — and no
// generation moves (plain takeover and revision stay separate).
func TestK4TakeoverAdoptsExistingResultWithoutReExecution(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	runTakeoverOwner(t, dir, "write-result")
	waitForMarker(t, dir, "a", 1)

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
	// The observation is a PERSISTED fact now (F4): the row names the
	// original call, so the fact survives owners that never consult.
	var observed int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM call_observations WHERE invocation_id=?", orig).Scan(&observed); err != nil {
		t.Fatal(err)
	}
	if observed != 1 {
		t.Fatalf("call_observations rows for the observed call = %d, want 1", observed)
	}
	// Re-observing the same call is idempotent.
	if err := r.Run(ctx, takeoverSpecs()); !errors.Is(err, ErrCallObserved) {
		t.Fatalf("second run err = %v, want ErrCallObserved", err)
	}
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM call_observations WHERE invocation_id=?", orig).Scan(&observed); err != nil {
		t.Fatal(err)
	}
	if observed != 1 {
		t.Fatalf("call_observations rows after re-observation = %d, want still 1", observed)
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
	waitForMarker(t, dir, "a", 1)
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

// 失效会话的事实需要用户决定（F5 rework）: the supervisor wrote partial
// progress and a session-invalid marker, then died without a result.
// The zero outcome is the dead-session FACT — it no longer authorizes a
// replacement: recovery creates a pending restart request and waits.
// Answering restart-session voids the dead call and re-runs under a NEW
// invocation id — the retry carries the newest native identity and picks
// the partial work up (§8.3); answering abort voids it, re-runs nothing
// and the run reports ErrCallAborted, durably.
func TestK4TakeoverDeadSessionRequiresReplacementDecision(t *testing.T) {
	// partialExecutor picks the dead session's partial work up — the
	// §8.3 carry the replacement must be able to perform.
	partialExecutor := func(dir string, execs *int) Executor {
		return func(ctx context.Context, inv Invocation) (Result, error) {
			*execs++
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
		}
	}
	// setup drives the child, reopens, and reaches the durable ask.
	setup := func(t *testing.T) (*Store, string, string, *int) {
		t.Helper()
		dir := t.TempDir()
		runTakeoverOwner(t, dir, "die-partial")
		waitForGlob(t, dir, "dead-*")
		st, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		orig := pendingInvocationID(t, st, "a")
		parentExecs := 0
		r := NewRunner(st, partialExecutor(dir, &parentExecs))
		r.Takeover = fileTakeover(dir)
		r.Restart = restartQuestion()
		if err := r.Run(context.Background(), takeoverSpecs()); !errors.Is(err, ErrWaitingDecision) {
			t.Fatalf("run after the dead-session fact err = %v, want ErrWaitingDecision (the ask)", err)
		}
		// The fact neither voided nor re-ran anything, and the ask is
		// listed with the frozen question.
		if parentExecs != 0 {
			t.Fatalf("parent executed task a %d times on the dead-session fact, want 0", parentExecs)
		}
		if n := markerLines(t, dir, "a"); n != 0 {
			t.Fatalf("marker lines on the dead-session fact = %d, want 0", n)
		}
		if state, _ := invocationState(t, st, orig); state != invPending {
			t.Fatalf("dead call state after the fact = %q, want still pending (nothing voided)", state)
		}
		pending, err := st.PendingDecisions(context.Background())
		if err != nil || len(pending) != 1 {
			t.Fatalf("pending after the dead-session fact = %v (err %v), want the restart ask", pending, err)
		}
		ask := pending[0]
		if ask.TaskID != "a" || !containsChoice(ask.Options, "restart-session") || !containsChoice(ask.Options, "abort") {
			t.Fatalf("restart ask = %+v, want task a offering restart-session and abort", ask)
		}
		return st, dir, ask.DecisionID, &parentExecs
	}

	t.Run("restart-session replaces under a new identity", func(t *testing.T) {
		st, dir, askID, parentExecs := setup(t)
		defer st.Close()
		ctx := context.Background()
		orig := pendingInvocationID(t, st, "a")
		if _, err := st.Answer(ctx, askID, "restart-session"); err != nil {
			t.Fatal(err)
		}
		// Recovery settles the stored decision alone — no consult and no
		// Restart declaration needed — and the replacement runs under a
		// fresh identity, carrying the partial work in.
		plain := NewRunner(st, partialExecutor(dir, parentExecs))
		if err := plain.Run(ctx, takeoverSpecs()); err != nil {
			t.Fatal(err)
		}
		if *parentExecs != 1 {
			t.Fatalf("replacement executions = %d, want exactly 1", *parentExecs)
		}
		if state, res := invocationState(t, st, orig); state != invSuperseded || res != "" {
			t.Errorf("dead session row = (%q,%q), want (superseded, no result)", state, res)
		}
		newID, gen, _ := admittedInvocation(t, st, "a")
		if newID == orig {
			t.Fatal("replacement reused the invalidated invocation id; the retry must carry a fresh native identity")
		}
		if gen != 1 {
			t.Errorf("replacement generation = %d, want 1 (replacement, no revision)", gen)
		}
		if _, res := invocationState(t, st, newID); res != `{"payload":"rerun-a+half-done"}` {
			t.Errorf("replacement result = %s, want the partial work carried in", res)
		}
		if n := markerLines(t, dir, "a"); n != 1 {
			t.Errorf("marker lines = %d, want exactly the replacement's 1", n)
		}
		if n := invocationTotal(t, st); n != 2 {
			t.Errorf("invocation rows = %d, want 2 (dead original plus replacement)", n)
		}
		if s, eff := taskState(t, st, "a"); s != taskFinished || eff != newID {
			t.Errorf("task a = (%q,%q), want finished with the replacement effective", s, eff)
		}
		var reqState string
		if err := st.db.QueryRow(
			"SELECT state FROM user_requests WHERE request_id=?", askID).Scan(&reqState); err != nil {
			t.Fatal(err)
		}
		if reqState != reqExecuted {
			t.Errorf("restart request state after the replacement = %q, want executed", reqState)
		}
	})

	t.Run("abort voids and never re-runs", func(t *testing.T) {
		st, dir, askID, parentExecs := setup(t)
		defer st.Close()
		ctx := context.Background()
		orig := pendingInvocationID(t, st, "a")
		if _, err := st.Answer(ctx, askID, "abort"); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := NewRunner(st, partialExecutor(dir, parentExecs)).Run(ctx, takeoverSpecs()); !errors.Is(err, ErrCallAborted) {
				t.Fatalf("run %d after abort err = %v, want ErrCallAborted", i, err)
			}
		}
		if *parentExecs != 0 {
			t.Fatalf("parent executed task a %d times after abort, want 0", *parentExecs)
		}
		if n := markerLines(t, dir, "a"); n != 0 {
			t.Fatalf("marker lines after abort = %d, want 0", n)
		}
		if state, _ := invocationState(t, st, orig); state != invSuperseded {
			t.Errorf("aborted call state = %q, want superseded (voided, row kept)", state)
		}
		if s, _ := taskState(t, st, "a"); s == taskRunning {
			t.Errorf("task a still running after the abort void")
		}
		if n := countInvocations(t, st, "a", invPending); n != 0 {
			t.Errorf("pending invocations after abort = %d, want 0", n)
		}
		if n := invocationTotal(t, st); n != 1 {
			t.Errorf("invocation rows after abort = %d, want exactly the voided original", n)
		}
	})
}

// The nil-Takeover recovery, reversed out of its unsafe half (the K4
// re-review's named test): with NO persisted observation the K1 blind
// supersede is exactly what the accepted baselines do — the pending
// invocation is superseded and the task re-executed; with a persisted
// observation (a previous owner vouched for the call) a nil-consult
// owner must supersede nothing, re-run nothing, and get
// ErrTakeoverRequired (F4).
func TestK4NilTakeoverSupersedesOnlyUnobservedCalls(t *testing.T) {
	t.Run("nil consult, no observation: K1 blind supersede as before", func(t *testing.T) {
		ctx := context.Background()
		dir := t.TempDir()
		runTakeoverOwner(t, dir, "slow-result") // never observed: the nil owner never consults
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
	})

	t.Run("nil consult, persisted observation: zero re-execution, ErrTakeoverRequired", func(t *testing.T) {
		ctx := context.Background()
		dir := t.TempDir()
		specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a")}}
		st := mustCreate(t, dir, "k4-nilobs", specs)
		calls := 0
		execute := func(context.Context, Invocation) (Result, error) {
			calls++
			if calls == 1 {
				return Result{}, errors.New("owner disconnected while the original call still ran")
			}
			return Result{}, nil
		}
		observing := NewRunner(st, execute)
		observing.Takeover = func(context.Context, Invocation) (TakeoverOutcome, error) {
			return TakeoverOutcome{Observe: true}, nil
		}
		if err := observing.Run(ctx, specs); err == nil || !strings.Contains(err.Error(), "disconnected") {
			t.Fatalf("first run err = %v, want the executor failure", err)
		}
		if err := observing.Run(ctx, specs); !errors.Is(err, ErrCallObserved) {
			t.Fatalf("observing run err = %v, want ErrCallObserved", err)
		}
		// A later owner's reconstruction omits the optional consult — the
		// persisted fact alone must stop the blind retry.
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		st, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		for i := 0; i < 2; i++ {
			if err := NewRunner(st, execute).Run(ctx, specs); !errors.Is(err, ErrTakeoverRequired) {
				t.Fatalf("nil-consult run %d err = %v, want ErrTakeoverRequired", i, err)
			}
		}
		if calls != 1 {
			t.Errorf("total executions = %d, want exactly the original 1", calls)
		}
		if n := countInvocations(t, st, "a", invPending); n != 1 {
			t.Errorf("observed invocation state rows = %d pending, want still 1 (nothing superseded)", n)
		}
		if s, _ := taskState(t, st, "a"); s != taskRunning {
			t.Errorf("task a state = %q, want running (out of the ready set)", s)
		}
	})
}
