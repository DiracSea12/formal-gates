package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManagedRuleRequiresSingleCurrentRule(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, filepath.FromSlash(managedRuleSourceRelativePath))

	writeManagedRuleTestFile(t, skillPath, "current rule")
	got, err := LoadManagedRule(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "current rule" {
		t.Fatalf("managed rule=%q want=%q", got, "current rule")
	}

	for _, invalid := range []string{
		"",
		" ",
		"\nleading",
		"trailing\n",
		hostInstructionsStartMarker,
		hostInstructionsEndMarker,
	} {
		t.Run(strings.ReplaceAll(invalid, "\n", "\\n"), func(t *testing.T) {
			writeManagedRuleTestFile(t, skillPath, invalid)
			if _, err := LoadManagedRule(root); err == nil {
				t.Fatalf("invalid managed rule %q was accepted", invalid)
			}
		})
	}
	if err := os.WriteFile(skillPath, []byte("# Skill without markers\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManagedRule(root); err == nil {
		t.Fatal("SKILL.md without managed rule markers was accepted")
	}
	duplicate := hostInstructionsStartMarker + "\none\n" + hostInstructionsEndMarker + "\n" +
		hostInstructionsStartMarker + "\ntwo\n" + hostInstructionsEndMarker + "\n"
	if err := os.WriteFile(skillPath, []byte(duplicate), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManagedRule(root); err == nil {
		t.Fatal("SKILL.md with duplicate managed rule blocks was accepted")
	}
}

func TestManagedRuleMarkersCollapseDuplicatesAndPreserveNewlineStyle(t *testing.T) {
	original := "before\r\n" +
		hostInstructionsStartMarker + "\r\nOLD\r\n" + hostInstructionsEndMarker + "\r\n" +
		"keep\r\n" +
		hostInstructionsStartMarker + "\r\nSTALE\r\n" + hostInstructionsEndMarker + "\r\n" +
		"after\r\n"
	updated, err := replaceManagedRuleBlock(original, "CURRENT")
	if err != nil {
		t.Fatal(err)
	}
	want := "before\r\n" +
		hostInstructionsStartMarker + "\r\nCURRENT\r\n" + hostInstructionsEndMarker + "\r\n" +
		"keep\r\nafter\r\n"
	if updated != want {
		t.Fatalf("managed marker migration=%q want=%q", updated, want)
	}
	if strings.Count(updated, hostInstructionsStartMarker) != 1 ||
		strings.Count(updated, hostInstructionsEndMarker) != 1 ||
		strings.Count(updated, "CURRENT") != 1 {
		t.Fatalf("managed block did not converge: %q", updated)
	}
}

func TestManagedRuleMarkersMigrateExactUnmarkedCurrentRule(t *testing.T) {
	original := "before\nCURRENT\nkeep CURRENT embedded\nafter\n"
	updated, err := replaceManagedRuleBlock(original, "CURRENT")
	if err != nil {
		t.Fatal(err)
	}
	want := "before\n" +
		hostInstructionsStartMarker + "\nCURRENT\n" + hostInstructionsEndMarker + "\n" +
		"keep CURRENT embedded\nafter\n"
	if updated != want {
		t.Fatalf("unmarked migration=%q want=%q", updated, want)
	}
}

func TestRemoveManagedRuleMarkersPreservesUnrelatedContent(t *testing.T) {
	original := "unrelated\n" +
		hostInstructionsStartMarker + "\nCURRENT\n" + hostInstructionsEndMarker + "\n" +
		"end\n"
	updated, removed, err := removeManagedRuleBlocks(original)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected managed marker block to be removed")
	}
	if want := "unrelated\nend\n"; updated != want {
		t.Fatalf("uninstalled managed block=%q want=%q", updated, want)
	}
}

func TestManagedRuleMarkersRejectMalformedBlocks(t *testing.T) {
	for name, input := range map[string]string{
		"missing-end":   hostInstructionsStartMarker + "\nCURRENT\n",
		"missing-start": hostInstructionsEndMarker + "\n",
		"nested":        hostInstructionsStartMarker + "\n" + hostInstructionsStartMarker + "\n" + hostInstructionsEndMarker + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := replaceManagedRuleBlock(input, "CURRENT"); err == nil {
				t.Fatalf("malformed marker block %q was accepted", input)
			}
		})
	}
}

func TestRemoveManagedRuleFileCanRemoveEmptyCursorRuleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "formal-gates.mdc")
	text := hostInstructionsStartMarker + "\nCURRENT\n" + hostInstructionsEndMarker + "\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedRuleFile(path, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected empty Cursor rule file to be removed, err=%v", err)
	}
}

func writeManagedRuleTestFile(t *testing.T, path, current string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := "# Skill\n" + hostInstructionsStartMarker + "\n" + current + "\n" + hostInstructionsEndMarker + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
