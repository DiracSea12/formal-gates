package protocol

import (
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

func TestCompletedCallPreservesIssuedExecutionChoice(t *testing.T) {
	explicit := ExecutionChoice{Provider: host.Claude, Model: "issued-model", ReasoningEffort: "high"}
	other := ExecutionChoice{Provider: host.Codex, Model: "other-model", ReasoningEffort: "low"}
	for _, test := range []struct {
		name    string
		issued  ExecutionChoice
		changed ExecutionChoice
		claim   string
		want    ExecutionChoice
	}{
		{"issued", explicit, explicit, "", explicit},
		{"native-defaults", ExecutionChoice{Provider: host.Claude}, ExecutionChoice{Provider: host.Claude}, "", ExecutionChoice{Provider: host.Claude}},
		{"model-only", ExecutionChoice{Provider: host.Claude, Model: "issued-model"}, ExecutionChoice{Provider: host.Claude, Model: "issued-model"}, "", ExecutionChoice{Provider: host.Claude, Model: "issued-model"}},
		{"effort-only", ExecutionChoice{Provider: host.Claude, ReasoningEffort: "high"}, ExecutionChoice{Provider: host.Claude, ReasoningEffort: "high"}, "", ExecutionChoice{Provider: host.Claude, ReasoningEffort: "high"}},
		{"unclaimed", other, explicit, "", explicit},
		{"receipted", explicit, other, "receipt", explicit},
		{"claimed-before-receipt", explicit, other, "native", explicit},
	} {
		t.Run(test.name, func(t *testing.T) {
			e, dir := readyReviewEngine(t)
			snapshot := e.LoadMustSucceed(t)
			snapshot.State.Settings = &settings.Effective{ConfigurationName: settings.DefaultConfigurationName, Values: settings.Values{Model: "run-model", ReasoningEffort: "run-effort"}}
			if _, err := e.commit(snapshot.State, snapshot.Revision, Observation{}); err != nil {
				t.Fatal(err)
			}
			library := bindLibrary(t, e, executionLibrary("product-review", test.issued))
			action := issueReady(t, e)[0]
			before := e.LoadMustSucceed(t).State
			original := before.Attempts[before.PendingActions[action].Task]
			receipt, err := NewSpawnReceiptEvent("spawn-history", action, test.want.Provider, "history-agent", SpawnStatusSpawned)
			if err != nil {
				t.Fatal(err)
			}
			if test.claim == "receipt" {
				if _, err := e.Submit(receipt, Observation{}); err != nil {
					t.Fatal(err)
				}
			}
			if test.claim == "native" {
				e.cfg.WorkerExecution = func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
					if p.ActionID != action || a.ID != original.ID || !a.Bindings.Equal(original.Bindings) {
						t.Fatal("claim lookup changed the signed execution", p, a)
					}
					return receipt.Spawn, nil
				}
			}
			library.value = executionLibrary("product-review", test.changed)
			if _, _, err := e.Drive(Observation{}); err != nil {
				t.Fatal(err)
			}
			reopened, err := New(mustStore(t, dir), e.cfg)
			if err != nil {
				t.Fatal(err)
			}
			assertChoice := func(stage string) {
				t.Helper()
				if actual, known := reopened.LoadMustSucceed(t).State.ActionExecutionChoice(action); !known || actual != test.want {
					t.Fatalf("%s call lost its issued execution choice: %+v/%t, want %+v", stage, actual, known, test.want)
				}
			}
			assertChoice("recovered")
			if input, ok := reopened.LoadMustSucceed(t).State.WorkerInput(action, reopened.cfg.Definition); !ok || input.Execution != test.want || input.Provider != test.want.Provider {
				t.Fatal("recovered worker input changed the native execution choice", input.Execution, ok)
			}
			reopened.cfg.WorkerExecution = nil
			reopened.cfg.CostSource = func(ref CostSourceRef) ResolvedCostSource {
				if ref.ActionID != action || ref.Provider != test.want.Provider || ref.Correlation != "history-agent" {
					t.Fatal("cost lookup changed the claimed execution", ref)
				}
				return ResolvedCostSource{State: CostSourceReady, Usage: cost.Usage{OutputTokens: 7}}
			}
			if _, err := reopened.Submit(receipt, Observation{}); err != nil {
				t.Fatal(err)
			}
			evidence := plannedReviewEvidence(review.ProductReview, action, "", "PASS", "complete", nil)
			result, err := NewReviewWorkerResultEvent("completed-model", action, test.want.Provider, "sha256:completed-model", evidence)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reopened.Submit(result, Observation{}); err != nil {
				t.Fatal(err)
			}
			state := reopened.LoadMustSucceed(t).State
			source := state.Results[action]
			verification, judgments := fixtureIndependentReview(t, state, source)
			observation := Event{ID: "verified-history", Kind: KindOperatorObservation, Observation: &OperatorObservationPayload{Subject: action, Review: &ReviewVerificationObservation{ResultDigest: source.Digest, Verification: verification, Findings: judgments}}}
			if _, err := reopened.Submit(observation, Observation{}); err != nil {
				t.Fatal(err)
			}
			completed := reopened.LoadMustSucceed(t).State
			if completed.Results[action].Outcome != OutcomePass || completed.Cost == nil || completed.Cost.OutputTokens != 7 {
				t.Fatal("completed call lost result or original dispatch cost", completed.Cost)
			}
			if execution := completed.SpawnReceipts[action].Execution; execution == nil || *execution != test.want {
				t.Fatal("durable receipt lost the actual signed choice", execution)
			}
			if _, _, current := completed.workerActionTarget(action); current {
				t.Fatal("completed call still owns a live attempt")
			}
			assertChoice("completed")
			library.value = executionLibrary("product-review", other)
			reconcile(t, reopened)
			assertChoice("library-changed")
			snapshot = reopened.LoadMustSucceed(t)
			snapshot.State.retireAction(action, "", "Reset authorized by user")
			if _, err := reopened.commit(snapshot.State, snapshot.Revision, Observation{}); err != nil {
				t.Fatal(err)
			}
			reopened, err = New(mustStore(t, dir), reopened.cfg)
			if err != nil {
				t.Fatal(err)
			}
			assertChoice("retired-and-recovered")
		})
	}
}
