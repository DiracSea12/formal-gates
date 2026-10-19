package protocol

import (
	"bytes"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/settings"
)

func configurationIOFailure() error {
	return &fs.PathError{Op: "read", Path: "configuration-library.json", Err: errors.New("temporarily unavailable")}
}

func TestConfigurationIOFirstEvaluationIssuesTheAppliedModelOnce(t *testing.T) {
	e, dir := readyReviewEngine(t)
	library := bindLibrary(t, e, executionLibrary("product-review", ExecutionChoice{Model: "applied-model"}))
	before := e.LoadMustSucceed(t)
	library.value.Execution["product-review"] = ExecutionChoice{Model: "unread-model"}
	library.err = configurationIOFailure()
	library.reads = nil

	plan, _, err := e.Drive(Observation{})
	if err != nil || plan.Next.Wait == nil || plan.Next.Wait.Reason == decision.WaitConfigurationUnavailable {
		t.Fatal("first failed evaluation stopped instead of using the applied value", plan, err)
	}
	if len(library.reads) != 1 {
		t.Fatal("one Drive sampled the library more than once", library.reads)
	}
	after := e.LoadMustSucceed(t)
	if len(after.State.PendingActions) != 1 || len(after.State.AppliedConfigurations) != len(before.State.AppliedConfigurations) {
		t.Fatal("first failure did not issue exactly the outstanding call on unchanged applied history")
	}
	for id := range after.State.PendingActions {
		actual, known := after.State.ActionExecutionChoice(id)
		if !known || actual.Model != "applied-model" {
			t.Fatal("first I/O failure issued an unread model", actual)
		}
	}
	failure := after.State.ConfigurationReadFailure()
	if failure == nil || failure.Detail != library.err.Error() || failure.Revision != before.Revision+1 {
		t.Fatal("first evaluation did not retain its original failure and revision", failure)
	}
	if len(after.State.RecoveryRecords) != len(before.State.RecoveryRecords)+1 || failure.ActionID != "" || failure.AttemptID != "" {
		t.Fatal("the library failure fabricated a worker identity or changed other recovery facts")
	}

	reopened, err := New(mustStore(t, dir), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	library.reads = nil
	durable := stateBytesOf(t, dir)
	snapshot := reopened.LoadMustSucceed(t)
	projection, err := reopened.ProjectSnapshot(snapshot, Observation{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(durable, stateBytesOf(t, dir)) || snapshot.State.ConfigurationReadFailure() == nil || *snapshot.State.ConfigurationReadFailure() != *failure {
		t.Fatal("read-only projection wrote or cleared the prior failure")
	}
	assertConfigurationIOWait(t, projection.Plan)
	if len(library.reads) != 1 {
		t.Fatal("Plan and node projection did not share their operation's sample", library.reads)
	}
}

func TestConfigurationIOPersistsAcrossOpenAndAcceptsClaimedResultsWithoutRefill(t *testing.T) {
	e, dir := readyReviewEngine(t)
	library := bindLibrary(t, e, executionLibrary("product-review", ExecutionChoice{Model: "original-model"}))
	library.err = configurationIOFailure()
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	first := e.LoadMustSucceed(t)
	var action string
	for id := range first.State.PendingActions {
		action = id
	}
	if action == "" {
		t.Fatal("first failure did not actually sign a review call")
	}
	original := first.State.Attempts[first.State.PendingActions[action].Task]

	// The original signed call is claimed after the library first failed.
	if _, err := e.Submit(spawnReceipt(t, "spawn-io-original-result", action, "agent-io-original-result"), Observation{}); err != nil {
		t.Fatal(err)
	}
	e.cfg.CostSource = func(ref CostSourceRef) ResolvedCostSource {
		if ref.ActionID != action {
			return ResolvedCostSource{State: CostSourceUnavailable, Reason: "not claimed"}
		}
		if ref.Provider != original.Bindings.Responsibility || ref.Correlation != "agent-io-original-result" {
			t.Fatal("cost lookup lost the original claimed identity", ref)
		}
		return ResolvedCostSource{State: CostSourceReady, Usage: cost.Usage{OutputTokens: 7}}
	}
	reopened, err := New(mustStore(t, dir), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	beforeRetry := reopened.LoadMustSucceed(t).Revision
	library.reads = nil
	plan, _, err := reopened.Drive(Observation{})
	if err != nil {
		t.Fatal(err)
	}
	assertConfigurationIOWait(t, plan)
	if len(library.reads) != 1 {
		t.Fatal("persistent failure was repeatedly sampled inside one evaluation", library.reads)
	}
	claimed := reopened.LoadMustSucceed(t).State
	if claimed.ConfigurationReadFailure() == nil || claimed.ConfigurationReadFailure().Revision != beforeRetry+1 {
		t.Fatal("one failed Drive did not record exactly its own evaluation revision")
	}
	current := claimed.Attempts[original.Task]
	if actual, known := claimed.ActionExecutionChoice(action); !known || actual.Model != "original-model" || current.ID != original.ID || !current.Bindings.Equal(original.Bindings) {
		t.Fatal("persistent I/O failure changed the claimed attempt or its issued model", actual)
	}
	submitVerifiedReviewResult(t, reopened, "io-original-result", action, plannedReviewEvidence(review.ProductReview, action, "", "PASS", "complete", nil))
	stopped := reopened.LoadMustSucceed(t)
	result := stopped.State.Results[action]
	if len(stopped.State.ReviewSeries.ProductReview.RawCandidates) != 1 || result.Review == nil || result.Review.Message != "complete" || result.Provider != original.Bindings.Responsibility || result.Outcome != OutcomePass || len(stopped.State.PendingActions) != 0 {
		t.Fatal("persistent read failure rejected an issued result or refilled another worker")
	}
	if stopped.State.Cost == nil || stopped.State.Cost.OutputTokens != 7 {
		t.Fatal("persistent I/O failure prevented original dispatch cost accounting", stopped.State.Cost)
	}
	for _, intent := range stopped.State.PendingHostActions {
		if intent.Terminate != nil {
			t.Fatal("configuration I/O failure withdrew an already claimed call")
		}
	}
	durable := stateBytesOf(t, dir)
	plan, _, err = reopened.Plan(Observation{})
	if err != nil {
		t.Fatal(err)
	}
	assertConfigurationIOWait(t, plan)
	if !bytes.Equal(durable, stateBytesOf(t, dir)) {
		t.Fatal("read-only Next changed state")
	}

	library.err = nil
	library.value.Execution["start-readiness"] = ExecutionChoice{Model: "recovered-model"}
	decisions := len(stopped.State.Decisions)
	if _, _, err := reopened.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	recovered := reopened.LoadMustSucceed(t)
	if recovered.State.ConfigurationReadFailure() != nil || len(recovered.State.Decisions) != decisions || len(recovered.State.PendingActions) != 1 {
		t.Fatal("successful library read did not recover without a user authorization")
	}
	for id := range recovered.State.PendingActions {
		actual, known := recovered.State.ActionExecutionChoice(id)
		if !known || actual.Model != "recovered-model" {
			t.Fatal("recovered dispatch did not use the now-readable model", actual)
		}
	}
	stable := stateBytesOf(t, dir)
	if _, _, err := reopened.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stable, stateBytesOf(t, dir)) {
		t.Fatal("unchanged healthy evaluation produced an empty commit")
	}
}

func TestConfigurationIORecoveryClearsFailureWithoutChangingAppliedValues(t *testing.T) {
	e, dir := readyReviewEngine(t)
	library := bindLibrary(t, e, settings.DefaultConfiguration())
	initial := e.LoadMustSucceed(t)
	retained := RecoveryRecord{Detail: "capacity refill deferred after an internal error: retained fact", Revision: initial.Revision + 1}
	initial.State.RecoveryRecords = append(initial.State.RecoveryRecords, retained)
	if _, err := e.commit(initial.State, initial.Revision, Observation{}); err != nil {
		t.Fatal(err)
	}
	library.err = configurationIOFailure()
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(mustStore(t, dir), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := reopened.LoadMustSucceed(t)
	durable := stateBytesOf(t, dir)
	library.err = nil
	plan, _, err := reopened.Plan(Observation{})
	if err != nil || plan.Next.Wait == nil || plan.Next.Wait.Reason == decision.WaitConfigurationUnavailable {
		t.Fatal("healthy read-only view stayed stopped", plan, err)
	}
	if !bytes.Equal(durable, stateBytesOf(t, dir)) || reopened.LoadMustSucceed(t).State.ConfigurationReadFailure() == nil {
		t.Fatal("read-only recovery cleared the durable fact")
	}
	if _, _, err := reopened.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	after := reopened.LoadMustSucceed(t)
	if after.State.ConfigurationReadFailure() != nil || !reflect.DeepEqual(before.State.AppliedConfigurations, after.State.AppliedConfigurations) || after.Revision != before.Revision+1 {
		t.Fatal("unchanged-value recovery failed to clear only the I/O failure")
	}
	found := false
	for _, record := range after.State.RecoveryRecords {
		found = found || record == retained
	}
	if !found {
		t.Fatal("clearing the library failure removed another recovery fact")
	}
	library.err = configurationIOFailure()
	plan, _, err = reopened.Drive(Observation{})
	if err != nil || plan.Next.Wait == nil || plan.Next.Wait.Reason == decision.WaitConfigurationUnavailable {
		t.Fatal("a new first failure inherited a cleared failure", plan, err)
	}
	if next := reopened.LoadMustSucceed(t); next.State.ConfigurationReadFailure() == nil || next.State.ConfigurationReadFailure().Revision != after.Revision+1 {
		t.Fatal("a new failure did not establish a fresh evaluation fact")
	}
}

func assertConfigurationIOWait(t *testing.T, plan *decision.Plan) {
	t.Helper()
	if plan.Next.Wait == nil || plan.Next.Wait.Reason != decision.WaitConfigurationUnavailable || !strings.Contains(plan.Next.Wait.Detail, "consecutive evaluations") || !strings.Contains(plan.Next.Wait.Detail, "configuration-library.json") {
		t.Fatal("persistent I/O failure did not report its specific cause", plan.Next, plan.Next.Wait)
	}
}
