package gitprep

import (
	"reflect"
	"strings"
	"testing"
)

func TestMarkdownHardBreakChangesAndCommit(t *testing.T) {
	for _, state := range []string{"unstaged", "staged", "untracked"} {
		t.Run(state, func(t *testing.T) {
			r := repository(t)
			if state != "untracked" {
				write(t, r, "README.md", "before\n")
				if _, err := Git(r, "add", "README.md"); err != nil {
					t.Fatal(err)
				}
				if _, err := Git(r, "commit", "-qm", "markdown baseline"); err != nil {
					t.Fatal(err)
				}
			}
			base, err := Head(r)
			if err != nil {
				t.Fatal(err)
			}
			text := "first line  \nsecond line\n"
			write(t, r, "README.md", text)
			if state == "staged" {
				if _, err := Git(r, "add", "README.md"); err != nil {
					t.Fatal(err)
				}
			}
			paths, err := Changes(r, base)
			if err != nil || !reflect.DeepEqual(paths, []string{"README.md"}) {
				t.Fatalf("Changes: %v %v", paths, err)
			}
			receipt, err := Commit(r, base, "markdown", paths)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Git(r, "show", receipt.After+":README.md")
			if err != nil || got != "first line  \nsecond line" {
				t.Fatalf("committed Markdown: %q %v", got, err)
			}
			replayed, err := Commit(r, base, "markdown", paths)
			if err != nil || replayed.After != receipt.After {
				t.Fatalf("replay: %+v %v", replayed, err)
			}
			t.Logf("%s: Markdown hard break retained; exact manifest committed and replayed", state)
		})
	}
}

func TestChangesStillRejectsUnresolvedIndex(t *testing.T) {
	r := repository(t)
	git := func(args ...string) string {
		t.Helper()
		out, err := Git(r, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	base := git("rev-parse", "HEAD")
	git("checkout", "-qb", "conflicting-side")
	write(t, r, "main.go", "side\n")
	git("commit", "-am", "side")
	git("checkout", "--detach", base)
	write(t, r, "main.go", "main\n")
	git("commit", "-am", "main")
	head := git("rev-parse", "HEAD")
	if _, err := Git(r, "merge", "conflicting-side"); err == nil {
		t.Fatal("expected merge conflict")
	}
	if _, err := Changes(r, head); err == nil || !strings.Contains(err.Error(), "unresolved Git index") {
		t.Fatalf("unresolved index: %v", err)
	}
}
