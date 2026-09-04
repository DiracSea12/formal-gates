package guard

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/engine/runtime"
)

func attempts(n int) *int { return &n }

func testBasis() Basis {
	confirmed := func(id string) ConfirmedIdentity {
		return ConfirmedIdentity{Identity{id, "r1"}, "r1"}
	}
	return Basis{
		SchemaVersion: 1, Requirement: confirmed("req"), Batch: confirmed("batch"),
		Route: confirmed("route"), Topology: confirmed("topology"),
		Definition: Definition{Identity{"workflow", "v1"}, []runtime.TaskKey{{Node: "n", Step: "work"}}},
		ApprovedQA: []Identity{{"case-1", "r1"}}, SelectedGates: []Identity{{"gate-1", "r1"}},
		EstablishedObligations: []Identity{{"obligation-1", "r1"}},
		EstablishedRepairs:     []Identity{{"repair-1", "r1"}},
	}
}

func testTask(scope string, max *int) ExternalTask {
	return ExternalTask{runtime.TaskKey{Node: "n", Step: "work", Scope: scope},
		Provenance{BatchSource, Identity{"batch", "r1"}}, max}
}

func mustCalculate(t *testing.T, b Basis) Result {
	t.Helper()
	r, err := Calculate(b)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCalculateKnownAnswers(t *testing.T) {
	// Literal answers exercise the production calculation, not a test copy of it.
	for _, tc := range []struct{ count, max, request, retry, total int }{
		{0, 1, 0, 0, 0}, {0, 2, 0, 0, 0}, {0, 5, 0, 0, 0},
		{1, 1, 1, 0, 1}, {1, 2, 1, 1, 2}, {1, 5, 1, 4, 5},
		{3, 1, 3, 0, 3}, {3, 2, 3, 3, 6}, {3, 5, 3, 12, 15},
	} {
		b := testBasis()
		for i := 0; i < tc.count; i++ {
			b.ExternalTasks = append(b.ExternalTasks, testTask(strconv.Itoa(i), attempts(tc.max)))
		}
		r := mustCalculate(t, b)
		if r.RequestAllowance != tc.request || r.RetryAllowance != tc.retry || r.TotalAllowance != tc.total || r.DistinctExternalTaskKeys != tc.count {
			t.Fatalf("%+v: got %+v", tc, r)
		}
	}
	b := testBasis()
	b.ExternalTasks = []ExternalTask{testTask("a", attempts(1)), testTask("b", nil), testTask("c", attempts(4))}
	r := mustCalculate(t, b)
	if r.RequestAllowance != 3 || r.RetryAllowance != 4 || r.TotalAllowance != 7 {
		t.Fatalf("mixed policies: %+v", r)
	}
	b.ExternalTasks = append(b.ExternalTasks, testTask("d", attempts(1)))
	r = mustCalculate(t, b)
	if r.RequestAllowance != 4 || r.RetryAllowance != 4 || r.TotalAllowance != 8 {
		t.Fatalf("new key: %+v", r)
	}
	b.ExternalTasks[2].MaxAttempts = attempts(5)
	r = mustCalculate(t, b)
	if r.RequestAllowance != 4 || r.RetryAllowance != 5 || r.TotalAllowance != 9 {
		t.Fatalf("extra attempt: %+v", r)
	}
}

func TestCalculateDocumentedDigest(t *testing.T) {
	data, err := os.ReadFile("../../../references/guard-preview-basis.json")
	if err != nil {
		t.Fatal(err)
	}
	var b Basis
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	// Independently checked with jq normalization of the documented fixture and
	// shasum -a 256; this literal also pins canonical bytes across later changes.
	const expected = "sha256:692f7b9bb402a8762c0f1ccffdfbaab6c7e9f48964e2f4ca54b2236e45e1bcef"
	if got := mustCalculate(t, b); got.BudgetBasisDigest != expected {
		t.Fatalf("documented digest: %s", got.BudgetBasisDigest)
	}
}

func TestCalculateCanonicalAndPure(t *testing.T) {
	b := testBasis()
	b.ExternalTasks = []ExternalTask{testTask("b", nil), testTask("a", attempts(3))}
	before, _ := json.Marshal(b)
	want := mustCalculate(t, b)
	after, _ := json.Marshal(b)
	if string(before) != string(after) {
		t.Fatal("Calculate mutated its input")
	}
	b.ExternalTasks = []ExternalTask{testTask("a", attempts(3)), testTask("b", attempts(2)), testTask("b", nil)}
	b.Definition.ExternalSteps = append(b.Definition.ExternalSteps, b.Definition.ExternalSteps[0])
	b.ApprovedQA = append(b.ApprovedQA, b.ApprovedQA[0])
	for i := 0; i < 10; i++ {
		if got := mustCalculate(t, b); !reflect.DeepEqual(want, got) {
			t.Fatalf("noncanonical calculation: %+v", got)
		}
	}
	// Normalization must also detach returned slices and retry pointers.
	want.Basis.ApprovedQA[0].ID = "changed"
	want.Basis.Definition.ExternalSteps[0].Step = "changed"
	*want.Basis.ExternalTasks[0].MaxAttempts = 99
	if b.ApprovedQA[0].ID != "case-1" || b.Definition.ExternalSteps[0].Step != "work" || *b.ExternalTasks[0].MaxAttempts != 3 {
		t.Fatal("result aliases caller data")
	}
	empty := testBasis()
	one := mustCalculate(t, empty)
	empty.ExternalTasks = []ExternalTask{}
	if two := mustCalculate(t, empty); !reflect.DeepEqual(one, two) {
		t.Fatal("nil versus empty tasks changes basis")
	}
}

func TestCalculateEveryBasisIdentityChangesDigest(t *testing.T) {
	for name, mutate := range map[string]func(*Basis){
		"requirement": func(b *Basis) { b.Requirement.Revision, b.Requirement.ConfirmedRevision = "r2", "r2" },
		"batch": func(b *Basis) {
			b.Batch.Revision, b.Batch.ConfirmedRevision = "r2", "r2"
			b.ExternalTasks[0].Provenance.Revision = "r2"
		},
		"route":      func(b *Basis) { b.Route.Revision, b.Route.ConfirmedRevision = "r2", "r2" },
		"topology":   func(b *Basis) { b.Topology.Revision, b.Topology.ConfirmedRevision = "r2", "r2" },
		"definition": func(b *Basis) { b.Definition.Revision = "v2" },
		"approvedQA": func(b *Basis) { b.ApprovedQA[0].Revision = "r2" },
		"gate":       func(b *Basis) { b.SelectedGates[0].Revision = "r2" },
		"obligation": func(b *Basis) { b.EstablishedObligations[0].Revision = "r2" },
		"repair":     func(b *Basis) { b.EstablishedRepairs[0].Revision = "r2" },
		"provenance": func(b *Basis) { b.ExternalTasks[0].Provenance = Provenance{RouteSource, b.Route.Identity} },
		"taskKey":    func(b *Basis) { b.ExternalTasks[0].TaskKey.Scope = "other" },
		"retry":      func(b *Basis) { b.ExternalTasks[0].MaxAttempts = attempts(3) },
	} {
		t.Run(name, func(t *testing.T) {
			b := testBasis()
			b.ExternalTasks = []ExternalTask{testTask("a", nil)}
			before := mustCalculate(t, b)
			mutate(&b)
			after := mustCalculate(t, b)
			if before.BudgetBasisDigest == after.BudgetBasisDigest {
				t.Fatal("basis identity omitted from digest")
			}
			if before.RequestAllowance != after.RequestAllowance {
				t.Fatal("changed identity creates base slot")
			}
		})
	}
}

func TestCalculateRejectsInvalidDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		mutate      func(*Basis)
	}{
		{"schema", "schemaVersion", func(b *Basis) { b.SchemaVersion = 0 }},
		{"definition", "definition.id", func(b *Basis) { b.Definition.ID = "" }},
		{"confirmation", "route.confirmedRevision", func(b *Basis) { b.Route.ConfirmedRevision = "" }},
		{"stale confirmation", "batch.confirmedRevision", func(b *Basis) { b.Batch.Revision = "r2" }},
		{"identity", "requirement.revision", func(b *Basis) { b.Requirement.Revision = "" }},
		{"step identity", "definition.externalSteps[0]", func(b *Basis) { b.Definition.ExternalSteps[0].Node = "" }},
		{"missing closed world", "definition.externalSteps", func(b *Basis) { b.Definition.ExternalSteps = nil }},
		{"not external", "externalTasks[0].taskKey", func(b *Basis) { b.Definition.ExternalSteps = []runtime.TaskKey{} }},
		{"task identity", "externalTasks[0].taskKey", func(b *Basis) { b.ExternalTasks[0].TaskKey.Step = "" }},
		{"zero retry", "externalTasks[0].maxAttempts", func(b *Basis) { b.ExternalTasks[0].MaxAttempts = attempts(0) }},
		{"negative retry", "externalTasks[0].maxAttempts", func(b *Basis) { b.ExternalTasks[0].MaxAttempts = attempts(-1) }},
		{"raw finding", "externalTasks[0].provenance", func(b *Basis) { b.ExternalTasks[0].Provenance.Kind = "RAW_FINDING" }},
		{"unknown source", "externalTasks[0].provenance", func(b *Basis) { b.ExternalTasks[0].Provenance.ID = "unknown" }},
		{"stale source", "externalTasks[0].provenance", func(b *Basis) { b.ExternalTasks[0].Provenance.Revision = "r0" }},
		{"retry conflict", "externalTasks[1]", func(b *Basis) { b.ExternalTasks = append(b.ExternalTasks, testTask("a", attempts(3))) }},
		{"provenance conflict", "externalTasks[1]", func(b *Basis) {
			extra := testTask("a", nil)
			extra.Provenance = Provenance{RouteSource, b.Route.Identity}
			b.ExternalTasks = append(b.ExternalTasks, extra)
		}},
		{"source conflict", "approvedQA[1]", func(b *Basis) { b.ApprovedQA = append(b.ApprovedQA, Identity{"case-1", "r2"}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testBasis()
			b.ExternalTasks = []ExternalTask{testTask("a", nil)}
			tc.mutate(&b)
			if _, err := Calculate(b); err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("want field %s, got %v", tc.field, err)
			}
		})
	}
}

func TestCalculateAllowedProvenance(t *testing.T) {
	b := testBasis()
	for _, source := range []Provenance{
		{DefinitionSource, b.Definition.Identity}, {BatchSource, b.Batch.Identity},
		{RouteSource, b.Route.Identity}, {TopologySource, b.Topology.Identity},
		{QASource, b.ApprovedQA[0]}, {GateSource, b.SelectedGates[0]},
		{ObligationSource, b.EstablishedObligations[0]}, {RepairSource, b.EstablishedRepairs[0]},
	} {
		task := testTask("a", nil)
		task.Provenance = source
		b.ExternalTasks = []ExternalTask{task}
		if r := mustCalculate(t, b); r.TotalAllowance != 2 {
			t.Fatalf("%s: %+v", source.Kind, r)
		}
	}
}

func TestGuardCoreHasNoIO(t *testing.T) {
	// Keep the production core limited to deterministic computation and the existing
	// TaskKey type. Run/transcript/network adapters belong outside this package.
	source, err := os.ReadFile("guard.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "guard.go", source, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"crypto/sha256": true, "encoding/json": true, "fmt": true, "sort": true, "strings": true, "formal-gates/internal/engine/runtime": true}
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if !allowed[path] {
			t.Errorf("guard core gained dependency %s; verify pure-computation boundary", path)
		}
	}
}
