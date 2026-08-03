package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManagedRulesRequiresOrderedUniqueCompleteVersions(t *testing.T) {
	root := t.TempDir()
	catalogPath := filepath.Join(root, filepath.FromSlash(managedRulesRelativePath))

	valid := []string{"old rule", "new rule"}
	writeManagedRulesTestFile(t, catalogPath, valid)
	got, err := LoadManagedRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "\x00") != strings.Join(valid, "\x00") {
		t.Fatalf("managed rules=%v want=%v", got, valid)
	}

	for _, invalid := range [][]string{
		nil,
		{"same", "same"},
		{" "},
		{"\nleading"},
		{"trailing\n"},
	} {
		t.Run(strings.ReplaceAll(strings.Join(invalid, ","), "\n", "\\n"), func(t *testing.T) {
			writeManagedRulesTestFile(t, catalogPath, invalid)
			if _, err := LoadManagedRules(root); err == nil {
				t.Fatalf("invalid managed rule catalog %v was accepted", invalid)
			}
		})
	}
	os.WriteFile(catalogPath, []byte(`{"versions":["rule"]}`), 0o600)
	if _, err := LoadManagedRules(root); err == nil {
		t.Fatal("managed rule catalog object format was accepted")
	}
}

func TestManagedRuleBlocksMigrateExactDuplicatesAndPreserveNewlineStyle(t *testing.T) {
	versions := []string{"OLD_RULE", "NEW_RULE"}
	original := "before\r\nOLD_RULE\r\nkeep OLD_RULE embedded\r\nNEW_RULE\r\nOLD_RULE\r\nafter\r\n"
	updated, err := replaceManagedRuleBlocks(original, versions, versions[len(versions)-1])
	if err != nil {
		t.Fatal(err)
	}
	want := "before\r\nkeep OLD_RULE embedded\r\nafter\r\nNEW_RULE\r\n"
	if updated != want {
		t.Fatalf("migrated managed rules=%q want=%q", updated, want)
	}
	if strings.Count(updated, "NEW_RULE") != 1 {
		t.Fatalf("latest rule count=%d in %q", strings.Count(updated, "NEW_RULE"), updated)
	}
}

func TestRemoveManagedRuleBlocksPreservesUnrelatedContent(t *testing.T) {
	versions := []string{"OLD_LINE", "NEW_LINE\nSECOND_LINE"}
	original := "unrelated\nOLD_LINE\nNEW_LINE\nSECOND_LINE\nend\n"
	updated, removed := removeManagedRuleBlocks(original, versions)
	if !removed {
		t.Fatal("expected managed blocks to be removed")
	}
	if want := "unrelated\nend\n"; updated != want {
		t.Fatalf("uninstalled managed rules=%q want=%q", updated, want)
	}
}

func TestRemoveManagedRuleFileCanRemoveEmptyCursorRuleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "formal-gates.mdc")
	if err := os.WriteFile(path, []byte("ONLY_RULE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedRuleFile(path, []string{"ONLY_RULE"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected empty Cursor rule file to be removed, err=%v", err)
	}
}

func writeManagedRulesTestFile(t *testing.T, path string, versions []string) {
	t.Helper()
	data, err := json.Marshal(versions)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
