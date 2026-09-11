package facade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubagentSourcesAreCapturedAndImmutableAcrossReplay(t *testing.T) {
	f := facadePreparation(t, nil)
	x := facadeExecution(t, f, "development-worker")
	dir := t.TempDir()
	path := filepath.Join(dir, "help.txt")
	refs := filepath.Join(dir, "sources.json")
	if err := os.WriteFile(path, []byte("native --resume SESSION\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refs, []byte(`[{"kind":"environment-observation","path":"help.txt"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := f.PrepareSubagent(x.Claim.ActionID, "parent", false, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Subagent.Sources) != 1 || r.Subagent.Sources[0].Path != path || r.Subagent.Sources[0].Content != "native --resume SESSION\n" || !strings.Contains(r.Subagent.Prompt, "fullTextPath") {
		t.Fatal("missing immutable full source")
	}
	full, err := os.ReadFile(filepath.Join(f.codexJob(x.Claim.ActionID), "subagent-source-0.txt"))
	if err != nil || string(full) != "native --resume SESSION\n" || strings.Contains(r.Subagent.Prompt, string(full)) {
		t.Fatalf("full source missing or repeated inline: %v", err)
	}
	revision := r.Revision
	if r, err = f.PrepareSubagent(x.Claim.ActionID, "parent", false, refs); err != nil || r.Subagent.Operation != "observe" || r.Revision != revision {
		t.Fatalf("replay changed claim: %v", err)
	}
	if err = os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = f.PrepareSubagent(x.Claim.ActionID, "parent", false, refs); err == nil || !strings.Contains(err.Error(), "INTENT_MISMATCH") {
		t.Fatalf("changed sources accepted: %v", err)
	}
	r, err = f.PrepareSubagent(x.Claim.ActionID, "parent", false)
	if err != nil || r.Subagent.Sources[0].Content != "native --resume SESSION\n" || r.Revision != revision {
		t.Fatalf("original input lost: %v", err)
	}
}

func TestSubagentSourcesRejectWrongRoleBeforeClaim(t *testing.T) {
	f := facadePreparation(t, []string{"blackbox"})
	x := facadeExecution(t, f, "blackbox-design")
	dir := t.TempDir()
	refs := filepath.Join(dir, "sources.json")
	if err := os.WriteFile(filepath.Join(dir, "contract.md"), []byte("public usage"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refs, []byte(`[{"kind":"environment-observation","path":"contract.md"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := f.Show()
	if _, err := f.PrepareSubagent(x.Claim.ActionID, "parent", false, refs); err == nil || !strings.Contains(err.Error(), "SUBAGENT_SOURCE_SCOPE") {
		t.Fatalf("role leak: %v", err)
	}
	after, _ := f.Show()
	if before.Revision != after.Revision || after.Executions[x.Claim.ActionID].Claimed {
		t.Fatal("invalid sources consumed claim")
	}
	if err := os.WriteFile(refs, []byte(`[{"kind":"public-contract","path":"contract.md"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.PrepareSubagent(x.Claim.ActionID, "parent", false, refs); err != nil {
		t.Fatal(err)
	}
	if _, err := readSubagentSources(refs, "product-review"); err == nil {
		t.Fatal("intake review gained external inputs")
	}
}
