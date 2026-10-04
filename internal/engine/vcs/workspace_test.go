package vcs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The native package graph must map files back to their own workspace even
// when the runtime reports a canonical spelling of the temporary directory.
func TestNativeTestOwnershipUsesWorkspaceDirectoryIdentity(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":      "module workspace\n\ngo 1.22\n",
		"app.go":      "package app\nfunc Valid(s string) bool { return s != \"\" }\n",
		"app_test.go": "package app\nimport \"testing\"\nfunc TestValid(t *testing.T) { if !Valid(\"ok\") { t.Fatal(\"valid\") } }\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("go", "list", "-test", "-json", "./...")
	command.Dir = root
	command.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	graph, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual package graph: %v %s", err, graph)
	}
	t.Logf("workspace root=%s actual graph=%s", root, graph)
	owned, err := New(root, ExecRunner{}).InputOwnership()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("observed ownership=%v", owned)
	if !owned["app_test.go"] || owned["app.go"] || len(owned) != 2 {
		t.Fatalf("native graph did not prove exact relative test ownership: %v", owned)
	}
}

// Assembly preserves an already staged removal from an immutable artifact.
func TestNativeArtifactRemovalRemainsStageable(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Artifact Fixture", "GIT_AUTHOR_EMAIL=artifact@example.invalid", "GIT_COMMITTER_NAME=Artifact Fixture", "GIT_COMMITTER_EMAIL=artifact@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("native git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	for _, path := range []string{"app.go", "removed_test.go"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte("package app\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "--all")
	git("commit", "-qm", "Artifact base")
	base := git("rev-parse", "HEAD")
	git("rm", "--", "removed_test.go")
	git("commit", "-qm", "Declared test removal")
	artifact := git("rev-parse", "HEAD")
	git("reset", "--hard", base)
	p := New(root, ExecRunner{})
	if err := p.ApplyArtifact(artifact, []string{"removed_test.go"}); err != nil {
		t.Fatal(err)
	}
	t.Logf("before Track native status=%s staged removal=%s", git("status", "--porcelain"), git("diff", "--cached", "--name-status"))
	if err := p.Track([]string{"removed_test.go"}); err != nil {
		t.Fatalf("declared artifact deletion failed common assembly Track: %v", err)
	}
	if got := git("diff", "--cached", "--name-status"); got != "D\tremoved_test.go" {
		t.Fatalf("staged native deletion changed: %q", got)
	}
}

// A staged edit can differ from both HEAD and the working bytes. All three
// native boundaries must remain visible before a declaration is committed.
func TestChangedPathsIncludesIndexAndWorkingBoundaries(t *testing.T) {
	root := t.TempDir()
	for _, field := range []string{"AUTHOR", "COMMITTER"} {
		t.Setenv("GIT_"+field+"_NAME", "Candidate Fixture")
		t.Setenv("GIT_"+field+"_EMAIL", "candidate@example.invalid")
	}
	p := New(root, nil)
	git := func(args ...string) string {
		t.Helper()
		out, err := p.runner.Run(root, "git", args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	git("init", "-q")
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("staged.txt", "base\n")
	write("working.txt", "base\n")
	git("add", "--all")
	git("commit", "-qm", "Inputs")
	write("staged.txt", "index edit\n")
	git("add", "--", "staged.txt")
	write("staged.txt", "base\n")
	write("working.txt", "working edit\n")
	write("new.txt", "untracked\n")
	if net := git("diff", "HEAD", "--", "staged.txt"); net != "" {
		t.Fatalf("fixture net change: %s", net)
	}
	before := git("write-tree")
	paths, err := p.ChangedPaths()
	if err != nil || !slices.Equal(paths, []string{"new.txt", "staged.txt", "working.txt"}) {
		t.Fatalf("complete change boundary %v %v", paths, err)
	}
	if git("write-tree") != before {
		t.Fatal("read changed index")
	}
}

func TestProductionEmbedOverridesTestOwnership(t *testing.T) {
	root := t.TempDir()
	for path, body := range map[string]string{
		"go.mod":      "module embedded\n\ngo 1.22\n",
		"app.go":      "package app\nimport _ \"embed\"\n//go:embed old_test.go\nvar content string\n",
		"old_test.go": "package app\nimport \"testing\"\nfunc TestOld(t *testing.T) {}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	p := New(root, nil)
	inputs, err := p.InputOwnership()
	if err != nil {
		t.Fatal(err)
	}
	if test, known := inputs["old_test.go"]; !known || test {
		t.Fatalf("embedded production input classified as test: %v", inputs)
	}
}

// Compiler-resolved package and external-test embed inputs stay test-owned;
// an input also embedded by production must remain in the production view.
func TestInputOwnershipIncludesEmbeddedTestInputs(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                "module embedded\n\ngo 1.22\n",
		"app.go":                "package app\nimport _ \"embed\"\n//go:embed testdata/shared.txt\nvar Shared string\n",
		"app_test.go":           "package app\nimport (\"testing\"; _ \"embed\")\n//go:embed testdata/internal.txt\nvar inputs string\n//go:embed testdata/shared.txt\nvar shared string\nfunc TestInputs(t *testing.T) {if inputs != \"internal\" || shared != Shared {t.Fatal(inputs, shared)}}\n",
		"external_test.go":      "package app_test\nimport (\"testing\"; _ \"embed\"; app \"embedded\")\n//go:embed testdata/external.txt\nvar inputs string\n//go:embed testdata/shared.txt\nvar shared string\nfunc TestExternalInputs(t *testing.T) {if inputs != \"external\" || shared != app.Shared {t.Fatal(inputs, shared)}}\n",
		"testdata/internal.txt": "internal",
		"testdata/external.txt": "external",
		"testdata/shared.txt":   "shared",
	}
	for path, body := range files {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = root
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compiler rejected embed inputs: %v %s", err, out)
	}
	want := map[string]bool{"app.go": false, "app_test.go": true, "external_test.go": true, "testdata/internal.txt": true, "testdata/external.txt": true, "testdata/shared.txt": false}
	alias := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, alias} {
		got, err := New(path, nil).InputOwnership()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("native input ownership at %s: %v %v; want %v", path, got, err, want)
		}
		if _, known := got["go.mod"]; known {
			t.Fatal("unknown build input gained test ownership")
		}
		t.Logf("native compiler inputs at %s: %v", path, got)
	}
}

func TestDirectoryIdentityPersistsAfterLeafRemoval(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	actual, alternate := filepath.Join(root, "workspace"), filepath.Join(alias, "workspace")
	if err := os.Mkdir(actual, 0755); err != nil {
		t.Fatal(err)
	}
	if !SamePath(actual, alternate) {
		t.Fatal("same native directory not recognized")
	}
	if err := os.Remove(actual); err != nil {
		t.Fatal(err)
	}
	if !SamePath(actual, alternate) {
		t.Fatal("deleted leaf registration lost directory identity")
	}
	if SamePath(actual, filepath.Join(root, "other")) {
		t.Fatal("distinct native resource conflated")
	}
}

// Native configuration distinguishes a missing resolver capability from a
// successful empty graph and an actual process or decoding failure.
func TestOwnershipCapabilityAndQueryFailuresRemainDistinct(t *testing.T) {
	failure := errors.New("ownership query failed")
	for _, scenario := range []struct {
		name, configuration, graph      string
		configurationError, graphError  error
		unavailable, wantError, queried bool
	}{
		{name: "no-module", configuration: fmt.Sprintf(`{"GOMOD":%q,"GOWORK":""}`, os.DevNull), unavailable: true},
		{name: "missing-tool", configurationError: exec.ErrNotFound, unavailable: true},
		{name: "empty-module-graph", configuration: `{"GOMOD":"/project/go.mod","GOWORK":""}`, queried: true},
		{name: "gopath", configuration: `{"GOMOD":"","GOWORK":""}`, queried: true},
		{name: "workspace", configuration: fmt.Sprintf(`{"GOMOD":%q,"GOWORK":"/project/go.work"}`, os.DevNull), queried: true},
		{name: "configuration-failure", configurationError: failure, wantError: true},
		{name: "configuration-json", configuration: `{`, wantError: true},
		{name: "query-failure", configuration: `{"GOMOD":"/project/go.mod"}`, graphError: failure, wantError: true, queried: true},
		{name: "graph-json", configuration: `{"GOMOD":"/project/go.mod"}`, graph: `{`, wantError: true, queried: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			queried := false
			runner := ownershipRunner(func(dir, command string, args ...string) (string, error) {
				if dir != root || command != "go" {
					t.Fatalf("unexpected ownership input: %s %s %v", dir, command, args)
				}
				switch strings.Join(args, " ") {
				case "env -json GOMOD GOWORK":
					return scenario.configuration, scenario.configurationError
				case "list -test -json ./...":
					queried = true
					return scenario.graph, scenario.graphError
				default:
					t.Fatalf("unexpected ownership method: %v", args)
					return "", nil
				}
			})
			got, err := New(root, runner).InputOwnership()
			if (err != nil) != scenario.wantError || queried != scenario.queried || !scenario.wantError && (got == nil) != scenario.unavailable {
				t.Fatalf("ownership outcome=%v err=%v queried=%v", got, err, queried)
			}
			if scenario.configurationError == failure || scenario.graphError == failure {
				if !errors.Is(err, failure) {
					t.Fatal("underlying failure identity lost")
				}
			}
		})
	}
}

type ownershipRunner func(string, string, ...string) (string, error)

func (r ownershipRunner) Run(dir, command string, args ...string) (string, error) {
	return r(dir, command, args...)
}
