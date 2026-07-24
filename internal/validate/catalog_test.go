package validate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
		{"non-markdown", func(root string) { writeTestFile(t, filepath.Join(root, "gates", "notes.txt"), "bad") }},
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

func TestGatePromptContainsEachPromptExactlyOnce(t *testing.T) {
	root := promptPackage(t, map[string]string{"quality": "UNIQUE_GATE_CHECK"})
	writeTestFile(t, filepath.Join(root, "prompts", "reviewer-base.md"), "UNIQUE_SHARED_RULE\n")
	catalog, err := LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := ComposeGatePrompt(catalog, "quality", PromptRoute{RequirementSource: "requirements.md", RequirementRevision: "rev", CatalogRevision: catalog.CatalogRevision, Worktree: "/repo", VCS: "git", BaseSnapshot: "a", CurrentSnapshot: "b"})
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"UNIQUE_SHARED_RULE", "UNIQUE_GATE_CHECK"} {
		if strings.Count(prompt, marker) != 1 {
			t.Fatalf("marker %q count=%d\n%s", marker, strings.Count(prompt, marker), prompt)
		}
	}
	if !strings.Contains(prompt, `"status":"PASS|FAIL|RUNTIME_ERROR"`) || !strings.Contains(prompt, "RUNTIME_ERROR requires a non-empty message") {
		t.Fatalf("gate prompt is missing its runtime-error result contract:\n%s", prompt)
	}
	if !strings.Contains(prompt, "catalog revision: "+catalog.CatalogRevision) {
		t.Fatalf("gate prompt is missing its catalog revision:\n%s", prompt)
	}
	for _, forbidden := range []string{"previous finding", "repair explanation", "target verdict"} {
		if strings.Contains(strings.ToLower(prompt), forbidden) {
			t.Fatalf("prompt contains prior-review context %q", forbidden)
		}
	}
}

func TestActionPromptsDescribeTheirSemanticReturn(t *testing.T) {
	root := promptPackage(t, map[string]string{"quality": "checks"})
	catalog, err := LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	route := PromptRoute{RequirementSource: "requirements.md", RequirementRevision: "rev", CatalogRevision: catalog.CatalogRevision, Worktree: "/repo", VCS: "git", BaseSnapshot: "a", CurrentSnapshot: "b", PreRepairSnapshot: "old"}
	for action, want := range map[string]string{"qa-design": "description, procedure, and oracle", "qa-execution": "case ID, PASS or FAIL outcome", "carry": "INHERIT or RERUN", "development-worker": "delivery path names", "start-readiness": "PASS with no findings"} {
		prompt, err := ComposeActionPrompt(catalog, action, route, "input")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(prompt, want) {
			t.Fatalf("%s prompt missing %q\n%s", action, want, prompt)
		}
		if strings.Count(prompt, "catalog revision: "+catalog.CatalogRevision) != 1 {
			t.Fatalf("%s prompt does not contain exactly one catalog revision:\n%s", action, prompt)
		}
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
