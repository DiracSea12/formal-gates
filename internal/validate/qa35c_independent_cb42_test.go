package validate

import

// Independent QA design for dispatch-cb42af60f339a817f5c648d7. These tests
// exercise the owning rules with private fixtures; they do not use earlier QA
// helpers. Persisted fixture states describe prerequisites, never target results.
(
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"testing"
)

func qa35cIndependentOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func qa35cIndependentWrite(t *testing.T, path, value string) {
	t.Helper()
	qa35cIndependentOK(t, os.MkdirAll(filepath.Dir(path), 0755))
	qa35cIndependentOK(t, os.WriteFile(path, []byte(value), 0600))
}

func qa35cIndependentGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = root
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Independent QA", "GIT_COMMITTER_NAME=Independent QA", "GIT_AUTHOR_EMAIL=independent@example.invalid", "GIT_COMMITTER_EMAIL=independent@example.invalid")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// The historical checkpoint test is itself the changed product of REQ-010.
// Execute that exact function in a tiny private module with fixture commit IDs;
// do not duplicate its rule in a newly invented checker or alter real history.
func TestQA35CIndependentHistoricalCheckpointOracle(t *testing.T) {
	data, err := os.ReadFile("qa35b_contract_2fe71c_test.go")
	qa35cIndependentOK(t, err)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "checkpoint.go", data, 0)
	qa35cIndependentOK(t, err)
	var function string
	for _, decl := range parsed.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == "TestQA35BCheckpointDoesNotImplementStage4OrRewriteHistory" {
			function = string(data[fset.Position(f.Pos()).Offset:fset.Position(f.End()).Offset])
		}
	}
	if function == "" {
		t.Fatal("historical checkpoint owner not found")
	}
	for _, scenario := range []struct{ name, path, document, wantError string }{
		{"append with later worktree rewrite", "internal/engine/guard/check.go", "historical base\ncheckpoint append\n", ""},
		{"stage boundary", "internal/engine/runtime/check.go", "historical base\ncheckpoint append\n", "stage 4 runtime touched"},
		{"ADR boundary", "refactor-plan/ADR-001.md", "historical base\ncheckpoint append\n", "ADR changed"},
		{"historical rewrite", "notes.md", "rewritten historical base\ncheckpoint append\n", "stage record history changed"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			qa35cIndependentGit(t, root, "init", "-q")
			qa35cIndependentWrite(t, filepath.Join(root, "refactor-plan/stage-records.md"), "historical base\n")
			qa35cIndependentGit(t, root, "add", ".")
			qa35cIndependentGit(t, root, "commit", "-qm", "base")
			base := qa35cIndependentGit(t, root, "rev-parse", "HEAD")
			qa35cIndependentWrite(t, filepath.Join(root, scenario.path), "checkpoint change\n")
			qa35cIndependentWrite(t, filepath.Join(root, "refactor-plan/stage-records.md"), scenario.document)
			qa35cIndependentGit(t, root, "add", ".")
			qa35cIndependentGit(t, root, "commit", "-qm", "delivered")
			delivered := qa35cIndependentGit(t, root, "rev-parse", "HEAD")
			qa35cIndependentWrite(t, filepath.Join(root, "refactor-plan/stage-records.md"), "later current document rewrite\n")
			body := strings.ReplaceAll(strings.ReplaceAll(function, "860ac5eefe1c501aa9caae42de788af05a7f6809", base), "b7eab3b16036a0a5fdeb32efc33c9d93d740392c", delivered)
			harness := fmt.Sprintf("package checkpoint\nimport (\"testing\";\"strings\";\"os/exec\")\nfunc qa35bPackage(t *testing.T) string{return %q}\nfunc qa35bGit(t *testing.T,root string,args ...string)string{t.Helper();c:=exec.Command(\"git\",args...);c.Dir=root;b,e:=c.CombinedOutput();if e!=nil{t.Fatalf(\"git: %%v %%s\",e,b)};return strings.TrimSpace(string(b))}\n", root) + body + "\n"
			module := t.TempDir()
			qa35cIndependentWrite(t, filepath.Join(module, "go.mod"), "module checkpoint-fixture\ngo 1.22\n")
			qa35cIndependentWrite(t, filepath.Join(module, "checkpoint_test.go"), harness)
			c := exec.Command("go", "test", "-count=1", "-run", "^TestQA35BCheckpointDoesNotImplementStage4OrRewriteHistory$", "-v", ".")
			c.Dir = module
			out, runErr := c.CombinedOutput()
			if scenario.wantError == "" {
				if runErr != nil {
					t.Fatalf("valid fixed pair rejected: %v\n%s", runErr, out)
				}
			} else if runErr == nil || !strings.Contains(string(out), scenario.wantError) {
				t.Fatalf("known bad checkpoint not rejected by owning rule %q: %v\n%s", scenario.wantError, runErr, out)
			}
			t.Logf("owning checkpoint execution: %s", out)
		})
	}
}

func TestQA35CIndependentFixtureBuildAndCopy(t *testing.T) {
	script, err := filepath.Abs("../../scripts/phase3-qa-fixture.sh")
	qa35cIndependentOK(t, err)
	source := t.TempDir()
	qa35cIndependentGit(t, source, "init", "-q")
	qa35cIndependentWrite(t, filepath.Join(source, "go.mod"), "module fixture-product\ngo 1.22\n")
	qa35cIndependentWrite(t, filepath.Join(source, "cmd/formal-gates/main.go"), "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"immutable candidate\")}\n")
	qa35cIndependentWrite(t, filepath.Join(source, "state.txt"), "clean prerequisite\n")
	qa35cIndependentGit(t, source, "add", ".")
	qa35cIndependentGit(t, source, "commit", "-qm", "candidate")
	identity := qa35cIndependentGit(t, source, "rev-parse", "HEAD")
	qa35cIndependentWrite(t, filepath.Join(source, "state.txt"), "dirty development state\n")
	stage := filepath.Join(t.TempDir(), "stage")
	call := func(args ...string) ([]byte, error) {
		return exec.Command("sh", append([]string{script}, args...)...).CombinedOutput()
	}
	out, err := call("stage", "--source", source, "--snapshot", identity, "--output", stage)
	if err != nil {
		t.Fatalf("stage: %v\n%s", err, out)
	}
	var metadata struct {
		Snapshot   string `json:"snapshot"`
		BuildCount int    `json:"buildCount"`
	}
	qa35cIndependentOK(t, json.Unmarshal(out, &metadata))
	if metadata.Snapshot != identity || metadata.BuildCount != 1 {
		t.Fatalf("wrong stage identity/count: %s", out)
	}
	for _, suffix := range []string{"first", "second"} {
		copy := filepath.Join(t.TempDir(), suffix)
		out, err := call("copy", "--source", stage, "--output", copy)
		if err != nil {
			t.Fatalf("copy: %v\n%s", err, out)
		}
		state, err := os.ReadFile(filepath.Join(copy, "state.txt"))
		qa35cIndependentOK(t, err)
		if string(state) != "clean prerequisite\n" {
			t.Fatal("copy inherited mutable development or previous-case state")
		}
		out, err = exec.Command(filepath.Join(copy, "bin/formal-gates")).CombinedOutput()
		if err != nil || string(out) != "immutable candidate\n" {
			t.Fatalf("copied candidate is not independently runnable: %v %s", err, out)
		}
		qa35cIndependentWrite(t, filepath.Join(copy, "state.txt"), "case changed state\n")
		if out, err = call("copy", "--source", stage, "--output", copy); err == nil || !strings.Contains(string(out), "output must be a new directory") {
			t.Fatal("copy silently reused a dirty destination")
		}
	}
	if _, err := call("stage", "--source", source, "--snapshot", "missing-snapshot", "--output", filepath.Join(t.TempDir(), "bad")); err == nil {
		t.Fatal("invalid archive source masked by successful later command")
	}
	qa35cIndependentWrite(t, filepath.Join(source, "cmd/formal-gates/main.go"), "known invalid Go source\n")
	qa35cIndependentGit(t, source, "add", ".")
	qa35cIndependentGit(t, source, "commit", "-qm", "known build failure")
	bad := qa35cIndependentGit(t, source, "rev-parse", "HEAD")
	out, err = call("stage", "--source", source, "--snapshot", bad, "--output", filepath.Join(t.TempDir(), "bad-build"))
	if err == nil || strings.Contains(string(out), "\"buildCount\": 1") {
		t.Fatalf("known compile failure claimed prepared candidate: %v %s", err, out)
	}
}
