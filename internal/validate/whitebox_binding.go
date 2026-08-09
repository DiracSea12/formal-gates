package validate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// whiteboxDeliveredTestCode is the whitebox designer's delivered structural test
// code used by fixtures and the portable canary (RQ-013 binding): the functions
// bound by whitebox cases are declared here, so the CLI's existence check (test
// exists in delivered test code) resolves against it.
const whiteboxDeliveredTestCode = `package whiteboxfixture

import "testing"

func TestWhiteboxDirectRules(t *testing.T) {}

func TestWhiteboxStructure(t *testing.T) {}

func TestWhiteboxStructureRevised(t *testing.T) {}

func TestWhiteboxDirectBehavior(t *testing.T) {}

func TestWhiteboxFailurePaths(t *testing.T) {}

func TestWhiteboxDuplicate(t *testing.T) {}
`

// whiteboxTestFunctionExists reports whether the whitebox designer's delivered
// structural test code under root declares a Go top-level function with the
// given name (RQ-013). It parses every .go file under root (excluding the run's
// own .gates state and hidden directories) and matches a FuncDecl by name. The
// parse is the CLI's "解析/编译检查": a whitebox case's Test field must name a
// test the designer actually delivered, so "测 A 的测试给 B 用例标 PASS" is
// detectable instead of only a text-non-empty check. A file that does not parse
// contributes no match (normal use delivers parseable test code); a whitespace
// name reports false.
func whiteboxTestFunctionExists(root, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	root = cleanRoot(root)
	// 深度受限遍历：跳过隐藏目录与运行期状态目录，避免把 .gates/.git 等无关文件算入。
	var walk func(string, int) bool
	walk = func(dir string, depth int) bool {
		if depth > 24 {
			return false
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if strings.HasPrefix(entry.Name(), ".") || entry.Name() == ".gates" {
					continue
				}
				if walk(path, depth+1) {
					return true
				}
				continue
			}
			if !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			if goFileDeclaresFunction(path, name) {
				return true
			}
		}
		return false
	}
	return walk(root, 0)
}

// goFileDeclaresFunction parses a single .go file and reports whether it
// declares a top-level function with the given name. Files that do not parse
// (foreign content, partial snippets, generated scaffolding) contribute no
// match; normal-use whitebox test code parses cleanly.
func goFileDeclaresFunction(path, name string) bool {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Recv != nil {
			continue
		}
		if funcDecl.Name != nil && funcDecl.Name.Name == name {
			return true
		}
	}
	return false
}
