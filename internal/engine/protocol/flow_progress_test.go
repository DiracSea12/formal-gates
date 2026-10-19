package protocol

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/runtime"
)

func TestFlowProgressReportsConfirmedBatches(t *testing.T) {
	t.Parallel()
	s, _, task := developmentCheckState(t)
	cd := compiledCanonicalWorkflow(t)
	progress := s.flowProgress(cd)
	if !progress.PlanConfirmed || len(progress.DevelopmentBatches) != 2 {
		t.Fatalf("confirmed plan projected %d batches, confirmed=%v", len(progress.DevelopmentBatches), progress.PlanConfirmed)
	}
	source, consumer := progress.DevelopmentBatches[0], progress.DevelopmentBatches[1]
	if source.ID != "source" || source.Status != runtime.TaskTerminal {
		t.Fatalf("finished batch projected %+v", source)
	}
	if consumer.ID != "consumer" || consumer.Status != runtime.TaskQueued || !slices.Equal(consumer.DependsOn, []string{"source"}) {
		t.Fatalf("dependent batch projected %+v", consumer)
	}
	if got := s.flowProgress(cd).DevelopmentBatches[0].Status; got != s.TaskStatusOf(task) {
		t.Fatalf("batch status %q does not match the task ledger %q", got, s.TaskStatusOf(task))
	}
}

func TestFlowProgressMarksPlanPendingBeforeConfirmation(t *testing.T) {
	t.Parallel()
	s := &State{}
	progress := s.flowProgress(compiledCanonicalWorkflow(t))
	if progress.PlanConfirmed || len(progress.DevelopmentBatches) != 0 {
		t.Fatalf("unconfirmed run projected batches: %+v", progress)
	}
}

func TestFlowProgressMarksChildOwnedBatchesWithoutStatusUnderSplitTopology(t *testing.T) {
	t.Parallel()
	s, _, _ := developmentCheckState(t)
	cd := compiledCanonicalWorkflow(t)
	s.Topology = &TopologyBinding{Decision: topologySplit, SplitCount: 2, Slices: []string{"core", "tools"}}
	_, plan, _ := s.confirmedDevelopmentPlan()
	plan.Batches = append(plan.Batches, DevelopmentBatch{ID: "merged", Owner: masterMergeOwner, Deliverable: "merged tree", Checks: []DevelopmentCheck{{ID: "contract"}}})
	plan.Batches[0].Owner = "core"
	progress := s.flowProgress(cd)
	if len(progress.DevelopmentBatches) != 3 {
		t.Fatalf("split plan projected %+v", progress.DevelopmentBatches)
	}
	// Child-owned batches stay listed so the owner plan's dependency graph is
	// visible, but carry no master task status; the slice run reports its own.
	if progress.DevelopmentBatches[0].ID != "source" || progress.DevelopmentBatches[0].Owner != "core" ||
		progress.DevelopmentBatches[0].Required || progress.DevelopmentBatches[0].Status != "" {
		t.Fatalf("child-owned batch projected %+v", progress.DevelopmentBatches[0])
	}
	merged := progress.DevelopmentBatches[2]
	if merged.ID != "merged" || merged.Owner != masterMergeOwner || !merged.Required {
		t.Fatalf("master-merge batch projected %+v", merged)
	}
	// The required flag must survive real JSON marshalling so the panel can
	// tell slice-owned batches apart from master batches over HTTP.
	body, err := json.Marshal(progress)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"required":false`) {
		t.Fatalf("marshal dropped required=false: %s", body)
	}
	if !strings.Contains(string(body), `"required":true`) {
		t.Fatalf("marshal dropped required=true: %s", body)
	}
}

func TestFlowProgressReportsActualMainlineEntry(t *testing.T) {
	t.Parallel()
	s := &State{RunID: "master", SliceChildren: map[string]string{"core": "core-run", "tools": "tools-run"}}
	intent := HostActionIntent{ActionID: "merge-core", Adapter: &AdapterHostAction{Operation: "op.candidate", Params: map[string]any{
		"stage": "MERGE_FINISH", "base": "prefix", "integration": map[string]any{"childRunId": "core-run", "incoming": "incoming"},
	}}}
	s.PendingHostActions = map[string]HostActionIntent{intent.ActionID: intent}
	s.HostActionReceipts = map[string]HostActionReceipt{intent.ActionID: {Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: map[string]any{
		"integration": map[string]any{"incoming": "incoming", "prefix": "prefix"},
	}}}}
	progress := s.flowProgress(compiledCanonicalWorkflow(t))
	if !slices.Equal(progress.EnteredMainlineSlice, []string{"core"}) {
		t.Fatalf("mainline entry projected %v", progress.EnteredMainlineSlice)
	}
	// A completed-looking child without an accepted merge receipt stays out.
	s.SliceChildren["extra"] = "done-run"
	s.HostActionReceipts["done-run"] = HostActionReceipt{Status: HostActionStatusExecuted}
	progress = s.flowProgress(compiledCanonicalWorkflow(t))
	if slices.Contains(progress.EnteredMainlineSlice, "extra") {
		t.Fatalf("unmerged child projected as entered: %v", progress.EnteredMainlineSlice)
	}
}
