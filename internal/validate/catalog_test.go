package validate

import (
	"formal-gates/internal/prompts"
	"os"
	"path/filepath"
	"reflect"

	"testing"
)

func TestGateDiscoveryIsLexicalAndFileDriven(t *testing.T) {
	root := promptPackage(t, map[string]string{"z-last": "last checks", "a-first": "first checks", "a-first-child": "child checks"})
	catalog, err := prompts.LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := catalog.Gates, []prompts.PromptDefinition{{ID: "a-first", Content: "first checks"}, {ID: "a-first-child", Content: "child checks"}, {ID: "z-last", Content: "last checks"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gate order=%v want=%v", got, want)
	}
	writeTestFile(t, filepath.Join(root, "gates", "middle.md"), "middle checks\n")
	added, err := prompts.LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := added.Gates, []prompts.PromptDefinition{{ID: "a-first", Content: "first checks"}, {ID: "a-first-child", Content: "child checks"}, {ID: "middle", Content: "middle checks"}, {ID: "z-last", Content: "last checks"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("added gate order=%v", got)
	}
	if err := os.Remove(filepath.Join(root, "gates", "a-first.md")); err != nil {
		t.Fatal(err)
	}
	removed, err := prompts.LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := removed.Gates, []prompts.PromptDefinition{{ID: "a-first-child", Content: "child checks"}, {ID: "middle", Content: "middle checks"}, {ID: "z-last", Content: "last checks"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("removed gate order=%v", got)
	}
}

func TestPackageRouteCandidatesUsePromptCatalog(t *testing.T) {
	root := promptPackage(t, map[string]string{"z-last": "last checks", "a-first": "first checks"})
	candidates, err := prompts.PackageRouteCandidates(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := candidates, []string{"blackbox", "whitebox", "a-first", "z-last"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates=%v want=%v", got, want)
	}

	emptyRoot := promptPackage(t, nil)
	emptyCandidates, err := prompts.PackageRouteCandidates(emptyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := emptyCandidates, []string{"blackbox", "whitebox"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("empty candidates=%v want=%v", got, want)
	}

	if _, err := prompts.PackageRouteCandidates(t.TempDir()); err == nil {
		t.Fatal("invalid prompt package was accepted")
	}
}

func TestGateDiscoveryIgnoresUnrelatedDirectEntries(t *testing.T) {
	root := promptPackage(t, map[string]string{"quality": "checks"})
	if err := os.Mkdir(filepath.Join(root, "gates", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "gates", "notes.txt"), "not a gate\n")

	catalog, err := prompts.LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := catalog.Gates, []prompts.PromptDefinition{{ID: "quality", Content: "checks"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gates=%v want=%v", got, want)
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
			if _, err := prompts.LoadPromptCatalog(root); err == nil {
				t.Fatal("invalid gate entry was accepted")
			}
		})
	}
}

func TestActionCatalogIsFixedWhileGatesRemainFileDriven(t *testing.T) {
	for _, mutate := range []func(string){
		func(root string) {
			if err := os.Remove(filepath.Join(root, "prompts", "actions", "qa-review.md")); err != nil {
				t.Fatal(err)
			}
		},
		func(root string) {
			writeTestFile(t, filepath.Join(root, "prompts", "actions", "extra.md"), "extra action\n")
		},
	} {
		root := promptPackage(t, map[string]string{"quality": "checks"})
		mutate(root)
		if _, err := prompts.LoadPromptCatalog(root); err == nil {
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
	for _, id := range prompts.RequiredActionIDs {
		writeTestFile(t, filepath.Join(root, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, rules := range catalog.RoleRules {
		writeTestFile(t, filepath.Join(root, "prompts", "roles", rules.ID+".md"), rules.Content)
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

func TestRoleRulesStayPrivateAndBindCatalogAndPackageContent(t *testing.T) {
	root := promptPackage(t, map[string]string{"quality": "checks"})
	before, err := prompts.LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := PackageReceipt(root)
	if err != nil {
		t.Fatal(err)
	}
	for index, rules := range before.RoleRules {
		t.Run(rules.ID, func(t *testing.T) {
			if _, public := before.Action(rules.ID); public {
				t.Fatal("role payload became a public action")
			}
			if _, gate := before.Gate(rules.ID); gate {
				t.Fatal("role payload became a gate")
			}
			path := filepath.Join(root, "prompts", "roles", rules.ID+".md")
			found := false
			for _, entry := range inventory.Entries {
				if entry.Path == filepath.ToSlash(filepath.Join("prompts", "roles", rules.ID+".md")) {
					found = true
				}
			}
			if !found {
				t.Fatal("role payload missing from original package inventory")
			}
			writeTestFile(t, path, rules.Content+"\nAdditional role duty.\n")
			after, err := prompts.LoadPromptCatalog(root)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := PackageReceipt(root)
			if err != nil {
				t.Fatal(err)
			}
			if inventory.Digest == receipt.Digest {
				t.Fatal("role body change escaped package binding")
			}
			wantRules := append([]prompts.PromptDefinition(nil), before.RoleRules...)
			wantRules[index].Content = rules.Content + "\nAdditional role duty."
			if !reflect.DeepEqual(after.RoleRules, wantRules) {
				t.Fatal("catalog did not load the exact changed role payload and unchanged other roles")
			}
			if got, ok := after.RoleRule(rules.ID); !ok || got != wantRules[index] {
				t.Fatal("role lookup did not return the exact changed payload")
			}
			if before.Base != after.Base || !reflect.DeepEqual(before.Gates, after.Gates) || !reflect.DeepEqual(before.Actions, after.Actions) || !reflect.DeepEqual(before.RouteCandidates(), after.RouteCandidates()) {
				t.Fatal("private role payload changed shared/gate/action/route content")
			}
			writeTestFile(t, path, rules.Content)
		})
	}
}

func TestRoleRuleCatalogRejectsIncompletePayloads(t *testing.T) {
	for _, missing := range []string{"gate", "qa", "qa-design-blackbox", "qa-design-whitebox", "qa-execution-blackbox", "qa-execution-whitebox", "qa-review-blackbox", "qa-review-whitebox"} {
		t.Run(missing, func(t *testing.T) {
			root := promptPackage(t, nil)
			if err := os.Remove(filepath.Join(root, "prompts", "roles", missing+".md")); err != nil {
				t.Fatal(err)
			}
			if _, err := prompts.LoadPromptCatalog(root); err == nil {
				t.Fatal("catalog accepted a missing required role payload")
			}
		})
	}
}
