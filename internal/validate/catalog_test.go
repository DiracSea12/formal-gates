package validate

import (
	"os"
	"path/filepath"
	"reflect"

	"testing"
)

func TestGateDiscoveryIsLexicalAndFileDriven(t *testing.T) {
	root := promptPackage(t, map[string]string{"z-last": "last checks", "a-first": "first checks"})
	catalog, err := LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := catalog.GateIDs(), []string{"a-first", "z-last"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gate order=%v want=%v", got, want)
	}
	writeTestFile(t, filepath.Join(root, "gates", "middle.md"), "middle checks\n")
	added, err := LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := added.GateIDs(), []string{"a-first", "middle", "z-last"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("added gate order=%v", got)
	}
	if err := os.Remove(filepath.Join(root, "gates", "a-first.md")); err != nil {
		t.Fatal(err)
	}
	removed, err := LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := removed.GateIDs(), []string{"middle", "z-last"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("removed gate order=%v", got)
	}
	// Replacement changes the next catalog without rewriting an already loaded one.
	writeTestFile(t, filepath.Join(root, "gates", "middle.md"), "replacement body without any prescribed wording\n")
	replaced, err := LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := removed.Gate("middle")
	after, _ := replaced.Gate("middle")
	if before.Content != "middle checks" || after.Content != "replacement body without any prescribed wording" {
		t.Fatalf("replacement lost catalog snapshot: %#v %#v", before, after)
	}

}

func TestPackageRouteCandidatesUsePromptCatalog(t *testing.T) {
	root := promptPackage(t, map[string]string{"z-last": "last checks", "a-first": "first checks"})
	candidates, err := PackageRouteCandidates(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := candidates, []string{"blackbox", "whitebox", "a-first", "z-last"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates=%v want=%v", got, want)
	}

	emptyRoot := promptPackage(t, nil)
	emptyCandidates, err := PackageRouteCandidates(emptyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := emptyCandidates, []string{"blackbox", "whitebox"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("empty candidates=%v want=%v", got, want)
	}

	if _, err := PackageRouteCandidates(t.TempDir()); err == nil {
		t.Fatal("invalid prompt package was accepted")
	}
}

func TestGateDiscoveryIgnoresUnrelatedDirectEntries(t *testing.T) {
	root := promptPackage(t, map[string]string{"quality": "checks"})
	if err := os.Mkdir(filepath.Join(root, "gates", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "gates", "notes.txt"), "not a gate\n")

	catalog, err := LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := catalog.GateIDs(), []string{"quality"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gate IDs=%v want=%v", got, want)
	}
}

func TestGateDiscoveryRejectsInvalidDirectEntries(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(string)
	}{
		{"bad-id", func(root string) { writeTestFile(t, filepath.Join(root, "gates", "Bad.md"), "bad") }},
		{"empty", func(root string) { writeTestFile(t, filepath.Join(root, "gates", "empty.md"), " \n") }},
		{"invalid-utf8", func(root string) {
			if err := os.WriteFile(filepath.Join(root, "gates", "bad.md"), []byte{0xff}, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory", func(root string) {
			if err := os.Mkdir(filepath.Join(root, "gates", "nested.md"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"reserved-qa", func(root string) {
			writeTestFile(t, filepath.Join(root, "gates", "qa.md"), "collides with built-in QA")
		}},
		{"reserved-blackbox", func(root string) {
			writeTestFile(t, filepath.Join(root, "gates", "blackbox.md"), "collides with the blackbox QA mode")
		}},
		{"reserved-whitebox", func(root string) {
			writeTestFile(t, filepath.Join(root, "gates", "whitebox.md"), "collides with the whitebox QA mode")
		}},
		{"reserved-merge-qa", func(root string) {
			writeTestFile(t, filepath.Join(root, "gates", "merge-qa.md"), "collides with the merge QA mode")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := promptPackage(t, map[string]string{"good": "checks"})
			tc.make(root)
			if _, err := LoadPromptCatalog(root); err == nil {
				t.Fatal("invalid gate entry was accepted")
			}
		})
	}
}

func TestActionCatalogIsFixedWhileGatesRemainFileDriven(t *testing.T) {
	for _, mutate := range []func(string){
		func(root string) {
			if err := os.Remove(filepath.Join(root, "prompts", "actions", "carry.md")); err != nil {
				t.Fatal(err)
			}
		},
		func(root string) {
			writeTestFile(t, filepath.Join(root, "prompts", "actions", "extra.md"), "extra action\n")
		},
	} {
		root := promptPackage(t, map[string]string{"quality": "checks"})
		mutate(root)
		if _, err := LoadPromptCatalog(root); err == nil {
			t.Fatal("changed fixed action catalog was accepted")
		}
	}
}

func promptPackage(t *testing.T, gates map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "prompts", "reviewer-base.md"), "shared contract\n")
	if err := os.MkdirAll(filepath.Join(root, "gates"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range requiredActionIDs {
		writeTestFile(t, filepath.Join(root, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	for id, text := range gates {
		writeTestFile(t, filepath.Join(root, "gates", id+".md"), text+"\n")
	}
	return root
}
func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
