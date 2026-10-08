package coverage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ResolvedOperation describes an invocation without executing it. GoTestName
// identifies the exact test whose real run and terminal events must be observed.
type ResolvedOperation struct {
	Directory  string
	Operation  Operation
	GoTestName string
}

// ResolveCaseOperations uses only an approved recipe or the fixed Go test
// reference protocol. Natural-language instructions never supply commands.
func ResolveCaseOperations(workspace string, qaCase AcceptanceCase) ([]ResolvedOperation, error) {
	if err := validateCaseOperations(qaCase, "case"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(workspace) == "" {
		return nil, fmt.Errorf("QA operation workspace is required")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	if isGoTestRef(qaCase.TestRef) {
		operation, err := resolveGoTest(root, qaCase.TestRef)
		if err != nil {
			return nil, err
		}
		return []ResolvedOperation{operation}, nil
	}
	if len(qaCase.Operations) == 0 {
		return nil, fmt.Errorf("case %s has no approved operations or supported Go test reference", qaCase.CaseID)
	}
	operations := make([]ResolvedOperation, 0, len(qaCase.Operations))
	for _, operation := range qaCase.Operations {
		operations = append(operations, ResolvedOperation{Directory: root, Operation: operation})
	}
	return operations, nil
}

func resolveGoTest(workspace, ref string) (ResolvedOperation, error) {
	file, name, _ := strings.Cut(ref, "::")
	if !filepath.IsLocal(file) || !token.IsIdentifier(name) || !strings.HasPrefix(name, "Test") {
		return ResolvedOperation{}, fmt.Errorf("Go test reference must be relative <path>_test.go::TestName: %s", ref)
	}
	if suffix := strings.TrimPrefix(name, "Test"); suffix != "" {
		first, _ := utf8.DecodeRuneInString(suffix)
		if unicode.IsLower(first) {
			return ResolvedOperation{}, fmt.Errorf("Go test function %s is not a Go test name", name)
		}
	}
	filename := filepath.Join(workspace, file)
	data, err := os.ReadFile(filename)
	if err != nil {
		return ResolvedOperation{}, fmt.Errorf("read Go test reference %s: %w", ref, err)
	}
	source, err := parser.ParseFile(token.NewFileSet(), filename, data, 0)
	if err != nil {
		return ResolvedOperation{}, fmt.Errorf("parse Go test reference %s: %w", ref, err)
	}
	var test *ast.FuncDecl
	for _, declaration := range source.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != name {
			continue
		}
		if test != nil {
			return ResolvedOperation{}, fmt.Errorf("Go test reference %s has multiple function declarations", ref)
		}
		test = fn
	}
	if test == nil || !goTestSignature(source, test) {
		return ResolvedOperation{}, fmt.Errorf("Go test reference %s needs an exact function with signature func(*testing.T)", ref)
	}
	return ResolvedOperation{
		Directory:  filepath.Dir(filename),
		Operation:  Operation{Kind: OperationArgv, Argv: []string{"go", "test", "-count=1", "-json", "-run", "^" + regexp.QuoteMeta(name) + "$", "."}},
		GoTestName: name,
	}, nil
}

func goTestSignature(source *ast.File, fn *ast.FuncDecl) bool {
	if fn.Recv != nil || fn.Type.TypeParams != nil || (fn.Type.Results != nil && len(fn.Type.Results.List) != 0) || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	parameter := fn.Type.Params.List[0]
	if len(parameter.Names) > 1 {
		return false
	}
	pointer, ok := parameter.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	for _, imported := range source.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil || path != "testing" {
			continue
		}
		alias := "testing"
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		if alias == "." {
			typeName, ok := pointer.X.(*ast.Ident)
			return ok && typeName.Name == "T"
		}
		selector, ok := pointer.X.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "T" {
			return false
		}
		packageName, ok := selector.X.(*ast.Ident)
		return ok && alias != "_" && packageName.Name == alias
	}
	return false
}
