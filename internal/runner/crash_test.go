package runner

// Crash-recovery tests for the §13.2 acceptance sample: two sequential
// tasks A→B; a child process owns the run and really exits (os.Exit —
// never a panic) at one of four injection points; the parent reopens the
// same directory and must finish the run with no missed execution and no
// double admission.
//
// The test binary re-executes itself: TestMain branches on
// RUNNER_CRASH_POINT before running any test, so the child is this same
// binary in a special mode.

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
)

const (
	envCrashPoint = "RUNNER_CRASH_POINT"
	envCrashDir   = "RUNNER_CRASH_DIR"
	childRunID    = "crash-sample"
)

// Distinct exit codes per injection point, so the parent can assert the
// child really died at the requested spot (codes 80+ mark wiring bugs in
// the child itself).
const (
	exitAfterIntent  = 70 // ① intent registered, not yet executed
	exitAfterResult  = 71 // ② result obtained, admission tx not started
	exitBeforeCommit = 72 // ③ admission tx written, not yet committed
	exitAfterCommit  = 73 // ④ admission tx committed
)

func TestMain(m *testing.M) {
	if point := os.Getenv(envCrashPoint); point != "" {
		crashChild(point) // never returns
	}
	// K2's decision scenarios use their own env and child (decisions_test.go);
	// the K1 crash path above is untouched.
	if scenario := os.Getenv(envK2Scenario); scenario != "" {
		k2Child(scenario) // never returns
	}
	// K3's parallel crash scenario follows the same re-exec pattern with
	// its own env (parallel_test.go); the branches above are untouched.
	if os.Getenv(envK3Scenario) != "" {
		k3Child() // never returns
	}
	// K4's revision-takeover scenario follows the same re-exec pattern
	// with its own env (revision_test.go); the branches above are
	// untouched.
	if os.Getenv(envK4Scenario) != "" {
		k4Child() // never returns
	}
	// The K4 repair's takeover scenarios follow the same re-exec pattern
	// (takeover_test.go): a real supervisor subprocess, then an owner
	// child that dies right after handing it the invocation; the
	// branches above are untouched.
	if os.Getenv(envK4SupervisorMode) != "" {
		k4Supervisor(os.Getenv(envK4SupervisorMode)) // never returns
	}
	if os.Getenv(envK4TakeoverScenario) != "" {
		k4TakeoverOwner(os.Getenv(envK4TakeoverScenario)) // never returns
	}
	os.Exit(m.Run())
}

// crashChild builds the two-task run in RUNNER_CRASH_DIR and drives it with
// a hook that os.Exit's at the requested injection point.
func crashChild(point string) {
	dir := os.Getenv(envCrashDir)
	if dir == "" {
		fmt.Fprintln(os.Stderr, "child: missing", envCrashDir)
		os.Exit(80)
	}
	st, err := Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "child open:", err)
		os.Exit(80)
	}
	specs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a")},
		{TaskID: "b", Name: "task-b", DependsOn: []string{"a"}, Input: []byte("input-b")},
	}
	if err := st.CreateRun(context.Background(), childRunID, specs); err != nil {
		fmt.Fprintln(os.Stderr, "child create:", err)
		os.Exit(81)
	}
	r := NewRunner(st, markerExecutor(dir))
	switch point {
	case "after-intent":
		r.hooks.afterRegisterIntent = func() { os.Exit(exitAfterIntent) }
	case "after-result":
		r.hooks.afterResult = func() { os.Exit(exitAfterResult) }
	case "before-commit":
		r.hooks.beforeAdmitCommit = func() { os.Exit(exitBeforeCommit) }
	case "after-commit":
		r.hooks.afterAdmit = func() { os.Exit(exitAfterCommit) }
	default:
		fmt.Fprintln(os.Stderr, "child: unknown crash point:", point)
		os.Exit(82)
	}
	// Every injection point exits from inside Run; reaching either exit
	// below means the hook wiring is broken, not the kernel.
	if err := r.Run(context.Background(), specs); err != nil {
		fmt.Fprintln(os.Stderr, "child run:", err)
		os.Exit(83)
	}
	os.Exit(84)
}

// markerExecutor appends one line per execution to marker-<task> — the
// externally observable side effect §13.2 asks for.  Line counts separate
// "executed once" from "re-executed because the owner died without
// receipt"; admission counts stay exactly one in every scenario.
func markerExecutor(dir string) Executor {
	return func(_ context.Context, inv Invocation) (Result, error) {
		f, err := os.OpenFile(
			filepath.Join(dir, "marker-"+inv.TaskID),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return Result{}, err
		}
		fmt.Fprintf(f, "exec %s\n", inv.TaskID)
		if err := f.Close(); err != nil {
			return Result{}, err
		}
		return Result{Payload: json.RawMessage(`"done-` + inv.TaskID + `"`)}, nil
	}
}

func TestCrashRecoveryFourPoints(t *testing.T) {
	cases := []struct {
		point     string
		childExit int
		aExecs    int // executions of task a after the parent finishes
		bExecs    int
		aSup      int // superseded invocation rows of task a
		bSup      int
	}{
		// ① exit after registering a's intent: a never ran in the
		// child, the parent re-registers and runs it once.
		{"after-intent", exitAfterIntent, 1, 1, 1, 0},
		// ② exit after obtaining a's result: a ran in the child but
		// the receipt never arrived, so the parent re-runs it.
		{"after-result", exitAfterResult, 2, 1, 1, 0},
		// ③ exit before the admission commit: the whole transaction
		// rolls back — same durable state as ②.
		{"before-commit", exitBeforeCommit, 2, 1, 1, 0},
		// ④ exit after a's admission committed: a is settled, the
		// parent only has to run b.
		{"after-commit", exitAfterCommit, 1, 1, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.point, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(),
				envCrashPoint+"="+tc.point, envCrashDir+"="+dir)
			out, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != tc.childExit {
				t.Fatalf("child exit = %v (want code %d); output:\n%s", err, tc.childExit, out)
			}

			// The child died holding the directory flock; Open
			// succeeding now is the kernel having released it —
			// exactly the ownership recovery K1 requires.
			st, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			if st.runID != childRunID {
				t.Fatalf("reopened run id = %q, want %q", st.runID, childRunID)
			}
			specs := []TaskSpec{
				{TaskID: "a", Name: "task-a", Input: []byte("input-a")},
				{TaskID: "b", Name: "task-b", DependsOn: []string{"a"}, Input: []byte("input-b")},
			}
			if err := NewRunner(st, markerExecutor(dir)).Run(context.Background(), specs); err != nil {
				t.Fatal(err)
			}

			// Side effects observable, with the exact execution
			// counts that distinguish re-execution from misses.
			if n := markerLines(t, dir, "a"); n != tc.aExecs {
				t.Errorf("task a executed %d times, want %d", n, tc.aExecs)
			}
			if n := markerLines(t, dir, "b"); n != tc.bExecs {
				t.Errorf("task b executed %d times, want %d", n, tc.bExecs)
			}
			// Exactly one admitted invocation per task, no
			// unsettled intent left, superseded rows preserved.
			for _, task := range []string{"a", "b"} {
				if n := countInvocations(t, st, task, invAdmitted); n != 1 {
					t.Errorf("task %s admitted %d times, want exactly 1", task, n)
				}
				if n := countInvocations(t, st, task, invPending); n != 0 {
					t.Errorf("task %s left %d unsettled intents", task, n)
				}
			}
			if n := countInvocations(t, st, "a", invSuperseded); n != tc.aSup {
				t.Errorf("task a superseded rows = %d, want %d", n, tc.aSup)
			}
			if n := countInvocations(t, st, "b", invSuperseded); n != tc.bSup {
				t.Errorf("task b superseded rows = %d, want %d", n, tc.bSup)
			}
			// Every task finished with an effective result, and
			// the events agree with the admitted invocations.
			for _, task := range []string{"a", "b"} {
				s, eff := taskState(t, st, task)
				if s != taskFinished || eff == "" {
					t.Errorf("task %s state = (%q,%q), want finished with effective result", task, s, eff)
				}
				state, _ := invocationState(t, st, eff)
				if state != invAdmitted {
					t.Errorf("task %s effective result points at invocation in state %q", task, state)
				}
			}
			events := loadEvents(t, st)
			if len(events) != 2 {
				t.Fatalf("events = %d, want one per admitted task (2)", len(events))
			}
			seen := map[string]bool{}
			for i, e := range events {
				if e.kind != eventKindAdmitted {
					t.Errorf("event %d kind = %q, want %q", i, e.kind, eventKindAdmitted)
				}
				var p admittedEvent
				if err := json.Unmarshal([]byte(e.payloadJSON), &p); err != nil {
					t.Fatalf("event %d payload: %v", i, err)
				}
				if _, eff := taskState(t, st, p.TaskID); eff != p.InvocationID {
					t.Errorf("event %d (%s) does not match task %s effective result", i, p.InvocationID, p.TaskID)
				}
				seen[p.TaskID] = true
			}
			if len(seen) != 2 {
				t.Errorf("admission events covered tasks %v, want both a and b", seen)
			}
		})
	}
}

// markerLines counts execution lines in the task's marker file (0 when the
// file was never created).
func markerLines(t *testing.T, dir, task string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "marker-"+task))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
