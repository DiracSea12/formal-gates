package coverage

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeGoTestSource(t *testing.T, workspace, file, source string) {
	t.Helper()
	filename := filepath.Join(workspace, file)
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveGoTestUsesExactFileFunctionAndPackageDirectory(t *testing.T) {
	for name, source := range map[string]string{
		"empty result list":     "package sample\nimport \"testing\"\nfunc TestValue(t *testing.T) () {}\n",
		"method with same name": "package sample\nimport \"testing\"\ntype sample struct{}\nfunc TestValue(t *testing.T) {}\nfunc (sample) TestValue() {}\n",
		"ordinary import":       "package sample\nimport \"testing\"\nfunc TestValue(t *testing.T) {}\nfunc TestValueOther(t *testing.T) {}\n",
		"external package":      "package sample_test\nimport check \"testing\"\nfunc TestValue(t *check.T) {}\n",
		"unnamed parameter":     "package sample\nimport \"testing\"\nfunc TestValue(*testing.T) {}\n",
		"dot import":            "package sample\nimport . \"testing\"\nfunc TestValue(t *T) {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			file := "nested/value_test.go"
			writeGoTestSource(t, workspace, file, source)
			got, err := ResolveCaseOperations(workspace, AcceptanceCase{CaseID: "value", TestRef: file + "::TestValue"})
			if err != nil {
				t.Fatal(err)
			}
			want := []ResolvedOperation{{Directory: filepath.Join(workspace, "nested"), GoTestName: "TestValue", Operation: Operation{Kind: OperationArgv, Argv: []string{"go", "test", "-count=1", "-json", "-run", "^TestValue$", "."}}}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("invocation = %#v; want %#v", got, want)
			}
		})
	}
}

func TestResolveGoTestRejectsMissingAmbiguousOrInvalidTestDeclarations(t *testing.T) {
	for name, source := range map[string]string{
		"missing":        "package sample\nimport \"testing\"\nfunc TestOther(t *testing.T) {}\n",
		"duplicate":      "package sample\nimport \"testing\"\nfunc TestValue(t *testing.T) {}\nfunc TestValue(t *testing.T) {}\n",
		"method":         "package sample\nimport \"testing\"\ntype sample struct{}\nfunc (sample) TestValue(t *testing.T) {}\n",
		"generic":        "package sample\nimport \"testing\"\nfunc TestValue[T any](t *testing.T) {}\n",
		"no parameter":   "package sample\nfunc TestValue() {}\n",
		"two parameters": "package sample\nimport \"testing\"\nfunc TestValue(t, other *testing.T) {}\n",
		"non pointer":    "package sample\nimport \"testing\"\nfunc TestValue(t testing.T) {}\n",
		"wrong type":     "package sample\nimport \"testing\"\nfunc TestValue(t *testing.B) {}\n",
		"wrong import":   "package sample\nimport testing \"elsewhere\"\nfunc TestValue(t *testing.T) {}\n",
		"no import":      "package sample\nfunc TestValue(t *testing.T) {}\n",
		"result":         "package sample\nimport \"testing\"\nfunc TestValue(t *testing.T) bool { return true }\n",
		"syntax":         "package sample\nfunc TestValue( {\n",
	} {
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			writeGoTestSource(t, workspace, "value_test.go", source)
			if got, err := ResolveCaseOperations(workspace, AcceptanceCase{TestRef: "value_test.go::TestValue"}); err == nil || got != nil {
				t.Fatalf("invalid declaration resolved: %#v %v", got, err)
			}
		})
	}
}

func TestResolveGoTestRejectsInvalidReferencesWithoutGuessing(t *testing.T) {
	workspace := t.TempDir()
	writeGoTestSource(t, workspace, "value_test.go", "package sample\nimport \"testing\"\nfunc TestValue(t *testing.T) {}\nfunc Testlower(t *testing.T) {}\n")
	for _, ref := range []string{
		"missing_test.go::TestValue",
		"../value_test.go::TestValue",
		filepath.Join(workspace, "value_test.go") + "::TestValue",
		"value_test.go::Testlower",
		"value_test.go::Value",
		"value_test.go::TestValue::extra",
		"value_test.go::TestValue/subtest",
		"value_test.go#TestValue",
		"value.go::TestValue",
		"behavior.spec.ts::TestValue",
		"TestValue",
	} {
		t.Run(ref, func(t *testing.T) {
			if got, err := ResolveCaseOperations(workspace, AcceptanceCase{TestRef: ref, Steps: "go test -run TestValue"}); err == nil || got != nil {
				t.Fatalf("unsupported reference guessed an invocation: %#v %v", got, err)
			}
		})
	}
}

func TestResolveApprovedOperationsPreservesSequenceAndValues(t *testing.T) {
	workspace := t.TempDir()
	operations := []Operation{{Kind: OperationArgv, Argv: []string{"tool", "", "a b", `"quoted"`, "$(command)"}, Stdin: "input\n"}, {Kind: OperationShell, Command: "printf '%s' \"$value\"\n", Stdin: "next\n"}}
	got, err := ResolveCaseOperations(workspace, AcceptanceCase{CaseID: "value", TestRef: "behavior.spec.ts::runs", Operations: operations})
	if err != nil {
		t.Fatal(err)
	}
	want := []ResolvedOperation{{Directory: workspace, Operation: operations[0]}, {Directory: workspace, Operation: operations[1]}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("approved sequence or values changed: %#v != %#v", got, want)
	}
	if _, err := ResolveCaseOperations(workspace, AcceptanceCase{TestRef: "value_test.go::TestValue", Operations: operations}); err == nil || !strings.Contains(err.Error(), "cannot also declare") {
		t.Fatalf("Go reference with another recipe accepted: %v", err)
	}
}
