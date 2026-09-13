package facade

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativePlatformBoundaryHasNoWorkflowOwner(t *testing.T) {
	names, err := filepath.Glob("runtime_adapter_*.go")
	if err != nil {
		t.Fatal(err)
	}
	names = append(names, "runtime_bridge_codex.go", "runtime_bridge_workbuddy.go", "runtime_recovery_codex.go")
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, owner := range nativeWorkflowOwnerTypes(file) {
			t.Errorf("%s exposes workflow owner %s to a platform adapter", name, owner)
		}
	}
	// Coverage/claim/result sequencing is invoked by the common executor, not
	// connected again by every host. Test the actual production source surface.
	for _, name := range []string{"runtime_context.go", "../protocol/source_adapters.go", "../protocol/development_continuity.go", "../../cost/parse.go", "../../settings/settings.go", "../../lifecycle/provider.go", "../../validate/hook.go", "runtime_bridge.go", "runtime_stream.go", "runtime_reconcile_stream.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, obsolete := range []string{"adapterFactories", "hookProtocolAdapters", "sourceAdapters =", "recoveryAdapters =", "switch provider", "switch binding.Provider", "native-source.json", "native-events.jsonl", "coverage-answer.json"} {
			if strings.Contains(string(data), obsolete) {
				t.Errorf("%s restored a separate platform dispatcher %q", name, obsolete)
			}
		}
	}
}

// Check dependencies in type positions, not unrelated native methods such as
// exec.Cmd.Run or an SDK's Run result type. This is a maintenance regression,
// not a whole-program type checker or an adversarial import policy.
func nativeWorkflowOwnerTypes(file *ast.File) []string {
	var owners []string
	ast.Inspect(file, func(node ast.Node) bool {
		var typ ast.Expr
		switch n := node.(type) {
		case *ast.Field:
			typ = n.Type
		case *ast.TypeSpec:
			typ = n.Type
		case *ast.ValueSpec:
			typ = n.Type
		case *ast.CompositeLit:
			typ = n.Type
		case *ast.TypeAssertExpr:
			typ = n.Type
		}
		if typ == nil {
			return true
		}
		ast.Inspect(typ, func(n ast.Node) bool {
			// Nested fields are inspected by the outer walk through their Type;
			// do not mistake interface method or struct field names for types.
			if _, field := n.(*ast.Field); field {
				return false
			}
			if selector, ok := n.(*ast.SelectorExpr); ok {
				pkg, ok := selector.X.(*ast.Ident)
				if ok && pkg.Name == "protocol" && (selector.Sel.Name == "RegularState" || selector.Sel.Name == "RegularExecution") {
					owners = append(owners, pkg.Name+"."+selector.Sel.Name)
				}
				return false
			}
			if id, ok := n.(*ast.Ident); ok && (id.Name == "Facade" || id.Name == "Run" || id.Name == "RegularState" || id.Name == "RegularExecution") {
				owners = append(owners, id.Name)
			}
			return true
		})
		return true
	})
	return owners
}

func TestNativePlatformBoundaryAllowsNativeRunMethod(t *testing.T) {
	for _, tc := range []struct {
		source  string
		blocked bool
	}{
		{"package facade; func invoke(cmd interface{ Run() error }) error { return cmd.Run() }", false},
		{"package facade; import sdk \"example.invalid/sdk\"; func invoke() sdk.Run { return sdk.Run{} }", false},
		{"package facade; func invoke(owner *Facade) {}", true},
		{"package facade; func invoke(run Run) {}", true},
		{"package facade; import \"formal-gates/internal/engine/protocol\"; func invoke(r *protocol.RegularState) {}", true},
		{"package facade; type adapter struct { state protocol.RegularExecution }", true},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "adapter.go", tc.source, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(nativeWorkflowOwnerTypes(file)) > 0; got != tc.blocked {
			t.Fatalf("blocked=%v, want %v for %s", got, tc.blocked, tc.source)
		}
	}
}
