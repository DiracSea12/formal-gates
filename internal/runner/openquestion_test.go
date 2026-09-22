package runner

// OpenQuestion tests — the mid-flight question seam's own contract.  The
// surrounding rules (Answer's replay/conflict/stale handling, the gate read
// of dispatchableTasks, the revision invalidation) are already covered by the
// gated-decision and revision suites; these cases pin only what the call
// itself adds.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openQuestionRun(t *testing.T) (*Store, *Runner) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "run")
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	specs := []TaskSpec{
		{TaskID: "review", Name: "review", Input: []byte(`{"v":1}`)},
		{TaskID: "work", Name: "work", Input: []byte(`{"v":1}`), DependsOn: []string{"review"}},
	}
	if err := store.CreateRun(context.Background(), "openq", specs); err != nil {
		t.Fatal(err)
	}
	return store, NewRunner(store, func(context.Context, Invocation) (Result, error) {
		return Result{Payload: []byte(`{}`)}, nil
	})
}

func question() *Confirmation {
	return &Confirmation{
		Options: []string{"retry", "abort"},
		Allow:   "retry",
		Body:    []byte("Findings remain."),
	}
}

func TestOpenQuestionBindsCurrentGeneration(t *testing.T) {
	store, _ := openQuestionRun(t)
	ctx := context.Background()
	id, err := store.OpenQuestion(ctx, "review", question())
	if err != nil || id == "" {
		t.Fatalf("open: id=%q err=%v", id, err)
	}
	// The question is a live gate read: it shows up as the run's open ask.
	pending, err := store.PendingDecisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TaskID != "review" {
		t.Fatalf("pending = %+v", pending)
	}
	// Idempotent while pending: the same id, not a second question.
	again, err := store.OpenQuestion(ctx, "review", question())
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Fatalf("re-ask id = %q, want %q", again, id)
	}
}

func TestOpenQuestionAnsweredRefusesReask(t *testing.T) {
	store, _ := openQuestionRun(t)
	ctx := context.Background()
	id, err := store.OpenQuestion(ctx, "review", question())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Answer(ctx, id, "retry"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenQuestion(ctx, "review", question()); !errors.Is(err, ErrQuestionAnswered) {
		t.Fatalf("re-ask after answer: err=%v, want ErrQuestionAnswered", err)
	}
	// After a revision moves the generation, asking is legal again and the
	// old answer authorizes nothing of the new generation.
	if err := store.ReviseTask(ctx, "review", TaskSpec{TaskID: "review", Input: []byte(`{"v":2}`)}); err != nil {
		t.Fatal(err)
	}
	next, err := store.OpenQuestion(ctx, "review", question())
	if err != nil {
		t.Fatal(err)
	}
	if next == id {
		t.Fatal("new generation reused the answered question's id")
	}
	if _, err := store.Answer(ctx, id, "abort"); !errors.Is(err, ErrDecisionStale) {
		t.Fatalf("old answer after revision: err=%v, want ErrDecisionStale", err)
	}
}

func TestOpenQuestionRefusesUnknownTaskAndBadQuestion(t *testing.T) {
	store, _ := openQuestionRun(t)
	ctx := context.Background()
	if _, err := store.OpenQuestion(ctx, "nope", question()); err == nil {
		t.Fatal("unknown task accepted")
	}
	bad := []*Confirmation{
		{Options: nil, Allow: "retry"},
		{Options: []string{"retry"}, Allow: ""},
		{Options: []string{"retry"}, Allow: "nope"},
	}
	for i, c := range bad {
		if _, err := store.OpenQuestion(ctx, "review", c); err == nil {
			t.Fatalf("malformed question %d accepted", i)
		}
	}
	if _, err := store.OpenQuestion(ctx, "review", nil); err == nil {
		t.Fatal("nil question accepted")
	}
}

func TestOpenQuestionGateReadBlocksTask(t *testing.T) {
	store, runner := openQuestionRun(t)
	ctx := context.Background()
	// The question is opened while the task is mid-flight (pending
	// invocation left behind by a no-receipt executor): the ready read must
	// not hand the task out again while the question stands.
	inv, err := runner.registerIntent(ctx, &taskRow{taskID: "review", generation: 1}, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenQuestion(ctx, "review", question()); err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(ctx, []TaskSpec{
		{TaskID: "review", Input: []byte(`{"v":1}`)},
		{TaskID: "work", Input: []byte(`{"v":1}`), DependsOn: []string{"review"}},
	}); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("run with open question: err=%v, want ErrWaitingDecision", err)
	}
	// Answering retry lets the caller revise the task; the kernel's
	// gate-carry rule then re-freezes the question PENDING at the new
	// generation (an approval authorizes one generation only), so the run
	// waits again until that carried gate is answered — the composition
	// avoids the cycle by never revising a question-holding task without
	// declaring its next question.
	if id, err := store.OpenQuestion(ctx, "review", question()); err != nil || id == "" {
		t.Fatalf("id=%q err=%v", id, err)
	} else if _, err := store.Answer(ctx, id, "retry"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReviseTask(ctx, "review", TaskSpec{TaskID: "review", Input: []byte(`{"v":1}`)}); err != nil {
		t.Fatal(err)
	}
	_ = inv
	if err := runner.Run(ctx, []TaskSpec{
		{TaskID: "review", Input: []byte(`{"v":1}`)},
		{TaskID: "work", Input: []byte(`{"v":1}`), DependsOn: []string{"review"}},
	}); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("run after carried gate: err=%v, want ErrWaitingDecision", err)
	}
	pending, err := store.PendingDecisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TaskID != "review" {
		t.Fatalf("carried question = %+v", pending)
	}
	if _, err := store.Answer(ctx, pending[0].DecisionID, "retry"); err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(ctx, []TaskSpec{
		{TaskID: "review", Input: []byte(`{"v":1}`)},
		{TaskID: "work", Input: []byte(`{"v":1}`), DependsOn: []string{"review"}},
	}); err != nil {
		t.Fatalf("run after carried answer: %v", err)
	}
}
