package validate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestGuardAndCostDependencyBoundaries(t *testing.T) {
	pkg, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"internal/engine/guard", "internal/cost"} {
		entries, err := os.ReadDir(filepath.Join(pkg, dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(pkg, dir, entry.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if dir == "internal/cost" && (strings.Contains(path, "/validate") || strings.Contains(path, "/engine")) {
					t.Errorf("cost reverse dependency %s", path)
				}
				if dir == "internal/engine/guard" && !slices.Contains([]string{"crypto/sha256", "encoding/json", "fmt", "sort", "strings", "formal-gates/internal/engine/runtime"}, path) {
					t.Errorf("pure guard acquired dependency %s", path)
				}
			}
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(pkg, "internal/cli/guard_preview.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		owner, ok := selector.X.(*ast.Ident)
		if ok && owner.Name == "guard" && selector.Sel.Name == "Calculate" {
			calls++
		}
		return true
	})
	if calls != 1 {
		t.Fatalf("preview must delegate exactly once to production Calculate, got %d calls", calls)
	}
}
