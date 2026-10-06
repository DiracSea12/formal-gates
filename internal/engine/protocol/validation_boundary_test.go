package protocol

import (
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"strings"
	"testing"
)

// Abort authority removes ordinary delivery hints without changing the native
// failure boundary or the planner's legal disposition precedence.
func TestAbortRetiresOrdinaryValidationBoundary(t *testing.T) {
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, waiting string
		topology      *TopologyBinding
		route         string
		validation    *Validation
	}{
		{"topology", "confirm topology", nil, "", nil},
		{"plan", "development plan", &TopologyBinding{Decision: "no-split"}, "full", nil},
		{"slice-receipts", "slice receipts: 0/2", &TopologyBinding{Decision: "split", SplitCount: 2, Slices: []string{"core", "tooling"}}, routeModeMerge, &Validation{}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s := &State{RequirementRevisions: []ConfirmedRevision{{Revision: "confirmed", StateRevision: 1}}, Topology: scenario.topology, RouteMode: scenario.route, Validation: scenario.validation}
			s.ReviewSeries.ProductReview.Effective.Status = "PASS"
			s.ReviewSeries.ProductReview.Effective.RequirementRevision = "confirmed"
			s.ReviewSeries.StartReadiness.Effective.Status = "PASS"
			s.ReviewSeries.StartReadiness.Effective.RequirementRevision = "confirmed"
			view := decision.State{RequiredTasks: map[authoring.StepID][]decision.RequiredTask{}}
			s.projectValidationBoundary(&view, cd)
			if len(view.OperatorFacts) != 1 || !strings.Contains(view.OperatorFacts[0].Value, scenario.waiting) {
				t.Fatalf("ordinary obligation absent: %+v", view.OperatorFacts)
			}
			s.Decisions = map[string]RecordedDecision{"stop": {Control: ControlAbort, Choice: "abort", Revision: 2}}
			view = decision.State{RequiredTasks: map[authoring.StepID][]decision.RequiredTask{}}
			s.projectValidationBoundary(&view, cd)
			if len(view.OperatorFacts) != 0 {
				t.Fatalf("abort retained delivery obligation: %+v", view.OperatorFacts)
			}
		})
	}
}

func TestAbortKeepsNativeCleanupDispositionBoundary(t *testing.T) {
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"UNKNOWN", "FAILED"} {
		t.Run(status, func(t *testing.T) {
			intent := HostActionIntent{ActionID: "cleanup", Revision: 3, Correlation: "cleanup", Adapter: &AdapterHostAction{Operation: "op.cleanup"}}
			receipt := HostActionReceipt{ActionID: intent.ActionID, AdapterOperation: "op.cleanup", Correlation: intent.Correlation, Status: status, FailureClass: authoring.FailureUserActionRequired}
			s := &State{Decisions: map[string]RecordedDecision{"stop": {Control: ControlAbort, Choice: "abort", Revision: 2}}, NativeResources: map[string]NativeResource{"workspace": {ID: "workspace", Status: "READY"}}, PendingHostActions: map[string]HostActionIntent{intent.ActionID: intent}, HostActionReceipts: map[string]HostActionReceipt{}, HostActionFailures: map[string][]HostActionReceipt{}}
			if status == "UNKNOWN" {
				s.HostActionReceipts[intent.ActionID] = receipt
			} else {
				s.HostActionFailures[intent.ActionID] = []HostActionReceipt{receipt}
			}
			if !s.NativeActionCurrent(intent, cd) {
				t.Fatal("fixture is not an eligible cleanup action")
			}
			view := decision.State{RequiredTasks: map[authoring.StepID][]decision.RequiredTask{}}
			s.projectValidationBoundary(&view, cd)
			if len(view.OperatorFacts) != 1 || view.OperatorFacts[0].Key != intent.ActionID {
				t.Fatalf("native disposition disappeared: %+v", view.OperatorFacts)
			}
			for _, kind := range []decision.Kind{decision.KindOperator, decision.KindAsk} {
				plan := &decision.Plan{Next: decision.NextResult{Kind: kind}, Frontier: []decision.FrontierEntry{{Step: "ordinary"}}}
				s.abortPlan(plan)
				if plan.Next.Kind != kind || len(plan.Frontier) != 0 {
					t.Fatal("abort changed native disposition precedence")
				}
			}
			retry := &ValidationRequest{Kind: "RETRY", Target: intent.ActionID}
			if status == "FAILED" && !s.cleanupRetryRequest(retry) {
				t.Fatal("cleanup retry no longer owned by accepted abort")
			}
		})
	}
}
