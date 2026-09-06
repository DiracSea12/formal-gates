package guard

// Independent QA Design dispatch-2fe71c878cf4bb6c672661a8.
// All fixtures and expected values below were designed from REQ-013 and Calculate.
import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/runtime"
)

func qa35bBasis() Basis {
	c := func(id string) ConfirmedIdentity { return ConfirmedIdentity{Identity{id, "r1"}, "r1"} }
	return Basis{SchemaVersion: 1, Requirement: c("requirement"), Batch: c("batch"), Route: c("route"), Topology: c("topology"),
		Definition: Definition{Identity{"definition", "r1"}, []runtime.TaskKey{{Node: "review", Step: "agent"}}},
		ApprovedQA: []Identity{{"qa", "r1"}}, SelectedGates: []Identity{{"gate", "r1"}},
		EstablishedObligations: []Identity{{"obligation", "r1"}}, EstablishedRepairs: []Identity{{"repair", "r1"}}}
}

func qa35bTask(scope string, attempts *int) ExternalTask {
	return ExternalTask{runtime.TaskKey{Node: "review", Step: "agent", Scope: scope}, Provenance{DefinitionSource, Identity{"definition", "r1"}}, attempts}
}

func TestQA35BGuardKnownAllowances(t *testing.T) {
	one, two, five := 1, 2, 5
	for _, tt := range []struct {
		name  string
		tasks []ExternalTask
		want  [3]int
	}{
		{"empty", nil, [3]int{0, 0, 0}},
		{"one-no-retry", []ExternalTask{qa35bTask("a", &one)}, [3]int{1, 0, 1}},
		{"one-default", []ExternalTask{qa35bTask("a", nil)}, [3]int{1, 1, 2}},
		{"mixed", []ExternalTask{qa35bTask("a", &one), qa35bTask("b", &two), qa35bTask("c", &five)}, [3]int{3, 5, 8}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := qa35bBasis()
			b.ExternalTasks = tt.tasks
			r, err := Calculate(b)
			if err != nil {
				t.Fatal(err)
			}
			if got := [3]int{r.RequestAllowance, r.RetryAllowance, r.TotalAllowance}; got != tt.want || r.DistinctExternalTaskKeys != tt.want[0] {
				t.Fatalf("got %+v, want %v", r, tt.want)
			}
		})
	}
}

func TestQA35BGuardDuplicateAndRetryDelta(t *testing.T) {
	b := qa35bBasis()
	b.ExternalTasks = []ExternalTask{qa35bTask("a", nil), qa35bTask("a", nil)}
	r, err := Calculate(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.RequestAllowance != 1 || r.RetryAllowance != 1 || r.TotalAllowance != 2 {
		t.Fatalf("duplicate counted: %+v", r)
	}
	b.ExternalTasks = append(b.ExternalTasks, qa35bTask("b", nil))
	r, err = Calculate(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.RequestAllowance != 2 || r.RetryAllowance != 2 || r.TotalAllowance != 4 {
		t.Fatalf("new key: %+v", r)
	}
	three := 3
	b.ExternalTasks[2].MaxAttempts = &three
	r, err = Calculate(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.RequestAllowance != 2 || r.RetryAllowance != 3 || r.TotalAllowance != 5 {
		t.Fatalf("retry changed base: %+v", r)
	}
}

func TestQA35BGuardCanonicalOrderAndInputOwnership(t *testing.T) {
	b := qa35bBasis()
	b.ExternalTasks = []ExternalTask{qa35bTask("z", nil), qa35bTask("a", nil)}
	before, _ := json.Marshal(b)
	r, err := Calculate(b)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(b)
	if string(before) != string(after) {
		t.Fatal("Calculate mutated caller input")
	}
	two := 2
	b.ExternalTasks = []ExternalTask{qa35bTask("a", &two), qa35bTask("z", &two), qa35bTask("a", nil)}
	b.ApprovedQA = append(b.ApprovedQA, b.ApprovedQA[0])
	b.Definition.ExternalSteps = append(b.Definition.ExternalSteps, b.Definition.ExternalSteps[0])
	r2, err := Calculate(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r, r2) || !strings.HasPrefix(r.BudgetBasisDigest, "sha256:") || len(r.BudgetBasisDigest) != 71 {
		t.Fatalf("noncanonical results: %+v / %+v", r, r2)
	}
	if r.Basis.ExternalTasks[0].TaskKey.Scope != "a" || r.Basis.ExternalTasks[1].TaskKey.Scope != "z" {
		t.Fatal("tasks not sorted")
	}
}

func TestQA35BGuardEveryBasisIdentityBindsDigest(t *testing.T) {
	mutations := map[string]func(*Basis){
		"requirement": func(b *Basis) { b.Requirement.ID += "-new" }, "batch": func(b *Basis) { b.Batch.ID += "-new" },
		"route": func(b *Basis) { b.Route.ID += "-new" }, "topology": func(b *Basis) { b.Topology.ID += "-new" },
		"definition": func(b *Basis) { b.Definition.Revision = "r2"; b.ExternalTasks[0].Provenance.Revision = "r2" },
		"qa":         func(b *Basis) { b.ApprovedQA[0].Revision = "r2" }, "gate": func(b *Basis) { b.SelectedGates[0].Revision = "r2" },
		"obligation": func(b *Basis) { b.EstablishedObligations[0].Revision = "r2" }, "repair": func(b *Basis) { b.EstablishedRepairs[0].Revision = "r2" },
		"provenance": func(b *Basis) { b.ExternalTasks[0].Provenance = Provenance{BatchSource, b.Batch.Identity} },
		"retry":      func(b *Basis) { n := 3; b.ExternalTasks[0].MaxAttempts = &n },
	}
	base := qa35bBasis()
	base.ExternalTasks = []ExternalTask{qa35bTask("a", nil)}
	want, err := Calculate(base)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range mutations {
		t.Run(name, func(t *testing.T) {
			b := qa35bBasis()
			b.ExternalTasks = []ExternalTask{qa35bTask("a", nil)}
			change(&b)
			r, err := Calculate(b)
			if err != nil {
				t.Fatal(err)
			}
			if r.BudgetBasisDigest == want.BudgetBasisDigest {
				t.Fatal("identity omitted from digest")
			}
		})
	}
}

func TestQA35BGuardRejectsCommonIncompleteDeclarations(t *testing.T) {
	for _, tt := range []struct {
		name, fragment string
		change         func(*Basis)
	}{
		{"schema", "schemaVersion", func(b *Basis) { b.SchemaVersion = 0 }},
		{"definition", "definition.externalSteps", func(b *Basis) { b.Definition.ExternalSteps = nil }},
		{"confirmation", "batch.confirmedRevision", func(b *Basis) { b.Batch.ConfirmedRevision = "" }},
		{"identity", "requirement.id", func(b *Basis) { b.Requirement.ID = "" }},
		{"task", "taskKey", func(b *Basis) { b.ExternalTasks[0].TaskKey.Step = "" }},
		{"undeclared-step", "not in definition.externalSteps", func(b *Basis) { b.ExternalTasks[0].TaskKey.Step = "other" }},
		{"provenance", "provenance", func(b *Basis) { b.ExternalTasks[0].Provenance.Kind = "RESULT" }},
		{"unestablished-repair", "provenance", func(b *Basis) { b.ExternalTasks[0].Provenance = Provenance{RepairSource, Identity{"pending", "r1"}} }},
		{"zero-retry", "maxAttempts", func(b *Basis) { n := 0; b.ExternalTasks[0].MaxAttempts = &n }},
		{"negative-retry", "maxAttempts", func(b *Basis) { n := -1; b.ExternalTasks[0].MaxAttempts = &n }},
		{"conflicting-retry", "conflicting declarations", func(b *Basis) { n := 3; b.ExternalTasks = append(b.ExternalTasks, qa35bTask("a", &n)) }},
		{"conflicting-provenance", "conflicting declarations", func(b *Basis) {
			other := qa35bTask("a", nil)
			other.Provenance = Provenance{BatchSource, b.Batch.Identity}
			b.ExternalTasks = append(b.ExternalTasks, other)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := qa35bBasis()
			b.ExternalTasks = []ExternalTask{qa35bTask("a", nil)}
			tt.change(&b)
			r, err := Calculate(b)
			if err == nil || !strings.Contains(err.Error(), tt.fragment) || !reflect.DeepEqual(r, Result{}) {
				t.Fatalf("result=%+v err=%v", r, err)
			}
		})
	}
}

func TestQA35BGuardAllowedSourceKinds(t *testing.T) {
	b := qa35bBasis()
	for _, p := range []Provenance{{DefinitionSource, b.Definition.Identity}, {BatchSource, b.Batch.Identity}, {RouteSource, b.Route.Identity}, {TopologySource, b.Topology.Identity}, {QASource, b.ApprovedQA[0]}, {GateSource, b.SelectedGates[0]}, {ObligationSource, b.EstablishedObligations[0]}, {RepairSource, b.EstablishedRepairs[0]}} {
		t.Run(string(p.Kind), func(t *testing.T) {
			task := qa35bTask("a", nil)
			task.Provenance = p
			b.ExternalTasks = []ExternalTask{task}
			r, err := Calculate(b)
			if err != nil || r.TotalAllowance != 2 {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}
