package gitprep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repository(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		if _, err := Git(r, a...); err != nil {
			t.Fatal(err)
		}
	}
	write(t, r, "go.mod", "module example.invalid/fixture\n\ngo 1.23\n")
	write(t, r, "main.go", "package main\nfunc main() {}\n")
	if _, err := Git(r, "add", "go.mod", "main.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(r, "commit", "-qm", "base"); err != nil {
		t.Fatal(err)
	}
	return r
}
func write(t *testing.T, r, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(r, p)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, p), []byte(s), 0644); err != nil {
		t.Fatal(err)
	}
}
func TestCommitManifestRecoveryAndSquash(t *testing.T) {
	r := repository(t)
	base, _ := Head(r)
	write(t, r, "answer.go", "package main\nconst answer = 42\n")
	if _, err := Commit(r, base, "one", []string{"main.go"}); err == nil {
		t.Fatal("incomplete manifest accepted")
	}
	head, _ := Head(r)
	if head != base {
		t.Fatal("failure committed")
	}
	staged, _ := Git(r, "diff", "--cached", "--name-only")
	if staged != "" {
		t.Fatal("failure staged paths")
	}
	one, err := Commit(r, base, "one", []string{"answer.go"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := Commit(r, base, "one", []string{"answer.go"})
	if err != nil || again.After != one.After {
		t.Fatalf("commit recovery: %+v %v", again, err)
	}
	if _, err = Commit(r, base, "different", []string{"answer.go"}); err == nil {
		t.Fatal("adopted unrelated operation")
	}
	write(t, r, "notes.md", "Delivery\n")
	two, err := Commit(r, one.After, "two", []string{"notes.md"})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := Squash(r, base, "final")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.View.Digest != two.View.Digest || candidate.After == two.After {
		t.Fatal("squash identity/view contract")
	}
	count, _ := Git(r, "rev-list", "--count", base+".."+candidate.After)
	if count != "1" {
		t.Fatal(count)
	}
	same, err := Squash(r, base, "final")
	if err != nil || same.After != candidate.After {
		t.Fatal("squash replay changed identity")
	}
}
func TestOwnershipFrameworkInputsAndRenames(t *testing.T) {
	r := repository(t)
	write(t, r, "main_test.go", "package main\nimport \"testing\"\nfunc TestAnswer(t *testing.T) {}\n")
	write(t, r, "testdata/input.txt", "before\n")
	write(t, r, "latest.go", "package main\nconst latest = 1\n")
	_, err := Git(r, "add", "main_test.go", "testdata/input.txt", "latest.go")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Git(r, "commit", "-qm", "tests")
	if err != nil {
		t.Fatal(err)
	}
	base, _ := Head(r)
	before, _ := Snapshot(r, base)
	old, err := TestOwnershipResolver(r)
	if err != nil {
		t.Fatal(err)
	}
	if old["latest.go"] || !old["testdata/input.txt"] || !old["main_test.go"] {
		t.Fatal(old)
	}
	if err = os.Rename(filepath.Join(r, "testdata/input.txt"), filepath.Join(r, "testdata/renamed.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, r, "main_test.go", "package main\nimport \"testing\"\nfunc TestAnswer(t *testing.T) { t.Log(42) }\n")
	now, err := TestOwnershipResolver(r)
	if err != nil {
		t.Fatal(err)
	}
	after, err := WorkingView(r, base)
	if err != nil {
		t.Fatal(err)
	}
	o, err := TestOnly(before, after, old, now)
	if err != nil || o.Status != "TEST_ONLY" || len(o.Paths) != 3 {
		t.Fatalf("ownership=%+v %v", o, err)
	}
	for _, path := range []string{"latest.go", "go.mod", ".gitattributes", ".codex/config.toml"} {
		t.Run(path, func(t *testing.T) {
			production := after
			production.Entries = append([]Entry(nil), after.Entries...)
			found := false
			for i := range production.Entries {
				if production.Entries[i].Path == path {
					production.Entries[i].Object = "changed"
					found = true
				}
			}
			if !found {
				production.Entries = append(production.Entries, Entry{Path: path, Mode: "100644", Type: "blob", Object: "new"})
			}
			o, _ := TestOnly(before, production, old, now)
			if o.Status != "PRODUCTION_CHANGED" {
				t.Fatal(path, o)
			}
		})
	}
}
func TestWorkspaceBaselineAndUnknown(t *testing.T) {
	r := repository(t)
	base, _ := Head(r)
	path := filepath.Join(t.TempDir(), "baseline")
	if err := Workspace(r, path, base); err != nil {
		t.Fatal(err)
	}
	write(t, r, "main.go", "package main\nfunc main() { println(42) }\n")
	data, err := os.ReadFile(filepath.Join(path, "main.go"))
	if err != nil || strings.Contains(string(data), "42") {
		t.Fatal("baseline polluted")
	}
	if err = Workspace(r, path, base); err != nil {
		t.Fatal(err)
	}
	if err = Cleanup(r, path); err != nil {
		t.Fatal(err)
	}
	if _, err = TestOwnershipResolver(t.TempDir()); err == nil {
		t.Fatal("unsupported project ownership guessed")
	}
}
