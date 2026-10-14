package coverage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// GoTestInvocation binds a fixed Go test command to its package and exact test.
type GoTestInvocation struct {
	Directory  string
	Operation  Operation
	GoTestName string
}

// ValidateGoTestReference verifies the exact test declaration in the workspace.
func ValidateGoTestReference(workspace, ref string) error {
	if strings.TrimSpace(workspace) == "" {
		return fmt.Errorf("QA operation workspace is required")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return err
	}
	if _, err := GoTestOperation(ref); err != nil {
		return err
	}
	file, name, _ := strings.Cut(ref, "::")
	filename := filepath.Join(root, file)
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read Go test reference %s: %w", ref, err)
	}
	source, err := parser.ParseFile(token.NewFileSet(), filename, data, 0)
	if err != nil {
		return fmt.Errorf("parse Go test reference %s: %w", ref, err)
	}
	var test *ast.FuncDecl
	for _, declaration := range source.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != name {
			continue
		}
		if test != nil {
			return fmt.Errorf("Go test reference %s has multiple function declarations", ref)
		}
		test = fn
	}
	if test == nil || !goTestSignature(source, test) {
		return fmt.Errorf("Go test reference %s needs an exact function with signature func(*testing.T)", ref)
	}
	return nil
}

// GoTestSymbols lists executable test declarations in their source order.
// The same declaration signature and reference rules govern execution.
func GoTestSymbols(file string, body []byte) ([]string, error) {
	source, err := parser.ParseFile(token.NewFileSet(), file, body, 0)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, declaration := range source.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Recv == nil {
			counts[fn.Name.Name]++
		}
	}
	names := []string{}
	for _, declaration := range source.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || !goTestSignature(source, fn) {
			continue
		}
		if _, err := GoTestOperation(file + "::" + fn.Name.Name); err != nil {
			continue
		}
		if counts[fn.Name.Name] != 1 {
			return nil, fmt.Errorf("Go test symbol %s has multiple function declarations", fn.Name.Name)
		}
		names = append(names, fn.Name.Name)
	}
	return names, nil
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

// GoTestOperation derives the fixed invocation without reading source files.
// ValidateGoTestReference verifies the referenced workspace declaration.
func GoTestOperation(ref string) (GoTestInvocation, error) {
	file, name, exact := strings.Cut(ref, "::")
	if !exact || !strings.HasSuffix(file, "_test.go") || !filepath.IsLocal(file) || !token.IsIdentifier(name) || !strings.HasPrefix(name, "Test") {
		return GoTestInvocation{}, fmt.Errorf("Go test reference must be relative <path>_test.go::TestName: %s", ref)
	}
	if suffix := strings.TrimPrefix(name, "Test"); suffix != "" {
		first, _ := utf8.DecodeRuneInString(suffix)
		if unicode.IsLower(first) {
			return GoTestInvocation{}, fmt.Errorf("Go test function %s is not a Go test name", name)
		}
	}

	return GoTestInvocation{Directory: filepath.Dir(file), Operation: Operation{Kind: OperationArgv, Argv: []string{"go", "test", "-timeout=0", "-count=1", "-json", "-run", "^" + regexp.QuoteMeta(name) + "$", "."}}, GoTestName: name}, nil
}

// GoTestExecutionResult requires the exact test's real run and terminal event.
func GoTestExecutionResult(stdout []byte, test string) string {
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	ran, result := false, "not-executed"
	for {
		var event struct {
			Action string
			Test   string
		}
		if err := decoder.Decode(&event); err != nil {
			if err != io.EOF {
				return "not-executed"
			}
			break
		}
		if event.Test != test {
			continue
		}
		switch event.Action {
		case "run":
			ran = true
		case "pass", "fail":
			if ran {
				result = event.Action
			}
		case "skip":
			result = "not-executed"
		}
	}
	return result
}
