package harness

import (
	"encoding/json"
	"testing"
)

const zero40 = "0000000000000000000000000000000000000000"

// raw wraps a header + NUL + zero or more paths + trailing NUL, matching the
// shape git diff --raw --no-abbrev -z produces.
func raw(records ...string) string {
	out := ""
	for _, r := range records {
		out += r
	}
	return out
}

func TestPlanComparisonRecordSplitting(t *testing.T) {
	r := raw(
		":100644 100644 "+zero40+" "+zero40+" M\x00a.txt\x00",
		":100644 100644 "+zero40+" "+zero40+" M\x00b.txt\x00",
	)
	res, err := planComparison(comparisonPlanInput{Base: "B", Candidate: "C", Diff: "full.diff", Raw: r})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	changes := res["changes"].([]any)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	first := changes[0].(map[string]any)
	if first["status"] != "M" {
		t.Fatalf("expected status M, got %v", first["status"])
	}
	base := first["base"].(map[string]any)
	if base["path"] != "a.txt" || base["material"] != "base/a.txt" {
		t.Fatalf("unexpected base side: %v", base)
	}
}

func TestPlanComparisonRenameReadsTwoPaths(t *testing.T) {
	r := raw(":100644 100644 " + zero40 + " " + zero40 + " R100\x00old.txt\x00new.txt\x00")
	res, err := planComparison(comparisonPlanInput{Raw: r})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	changes := res["changes"].([]any)
	change := changes[0].(map[string]any)
	if change["status"] != "R100" {
		t.Fatalf("expected R100, got %v", change["status"])
	}
	base := change["base"].(map[string]any)
	cand := change["candidate"].(map[string]any)
	if base["path"] != "old.txt" || base["material"] != "base/old.txt" {
		t.Fatalf("unexpected base side: %v", base)
	}
	if cand["path"] != "new.txt" || cand["material"] != "candidate/new.txt" {
		t.Fatalf("unexpected candidate side: %v", cand)
	}
}

func TestPlanComparisonCopyReadsTwoPaths(t *testing.T) {
	r := raw(":100644 100644 " + zero40 + " " + zero40 + " C50\x00src.txt\x00dst.txt\x00")
	res, err := planComparison(comparisonPlanInput{Raw: r})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	change := res["changes"].([]any)[0].(map[string]any)
	if change["status"] != "C50" {
		t.Fatalf("expected C50, got %v", change["status"])
	}
	base := change["base"].(map[string]any)
	cand := change["candidate"].(map[string]any)
	if base["path"] != "src.txt" || cand["path"] != "dst.txt" {
		t.Fatalf("copy paths wrong: %v / %v", base["path"], cand["path"])
	}
	if cand["material"] != "candidate/dst.txt" {
		t.Fatalf("unexpected candidate material: %v", cand["material"])
	}
}

func TestPlanComparisonMissingSide(t *testing.T) {
	// Added file: before mode 000000 -> base is null.
	add := raw(":000000 100644 " + zero40 + " " + zero40 + " A\x00new.txt\x00")
	res, err := planComparison(comparisonPlanInput{Raw: add})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	change := res["changes"].([]any)[0].(map[string]any)
	if change["base"] != nil {
		t.Fatalf("expected base null for add, got %v", change["base"])
	}
	cand := change["candidate"].(map[string]any)
	if cand["path"] != "new.txt" || cand["material"] != "candidate/new.txt" {
		t.Fatalf("unexpected candidate side: %v", cand)
	}

	// Deleted file: after mode 000000 -> candidate is null.
	del := raw(":100644 000000 " + zero40 + " " + zero40 + " D\x00gone.txt\x00")
	res, err = planComparison(comparisonPlanInput{Raw: del})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	change = res["changes"].([]any)[0].(map[string]any)
	if change["candidate"] != nil {
		t.Fatalf("expected candidate null for delete, got %v", change["candidate"])
	}

	// Both sides missing (crafted): both become null.
	both := raw(":000000 000000 " + zero40 + " " + zero40 + " X\x00weird.txt\x00")
	res, err = planComparison(comparisonPlanInput{Raw: both})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	change = res["changes"].([]any)[0].(map[string]any)
	if change["base"] != nil || change["candidate"] != nil {
		t.Fatalf("expected both sides null, got %v / %v", change["base"], change["candidate"])
	}
}

func TestPlanComparisonGitlinkHasNoteNoMaterial(t *testing.T) {
	r := raw(":160000 160000 " + zero40 + " " + zero40 + " M\x00module\x00")
	res, err := planComparison(comparisonPlanInput{Raw: r})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	change := res["changes"].([]any)[0].(map[string]any)
	for _, sideKey := range []string{"base", "candidate"} {
		side := change[sideKey].(map[string]any)
		if side["mode"] != "160000" {
			t.Fatalf("%s mode wrong: %v", sideKey, side["mode"])
		}
		if _, ok := side["material"]; ok {
			t.Fatalf("%s gitlink must not carry material", sideKey)
		}
		if side["note"] != "Gitlink only; nested repository content is not included." {
			t.Fatalf("%s gitlink note wrong: %v", sideKey, side["note"])
		}
	}
}

func TestPlanComparisonEmptyRaw(t *testing.T) {
	res, err := planComparison(comparisonPlanInput{Base: "B", Candidate: "C", Diff: "full.diff", Raw: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	changes := res["changes"].([]any)
	if len(changes) != 0 {
		t.Fatalf("expected empty changes, got %d", len(changes))
	}
	index, ok := res["index"].(string)
	if !ok {
		t.Fatalf("index missing")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(index), &decoded); err != nil {
		t.Fatalf("index not parseable JSON: %v", err)
	}
	got, ok := decoded["changes"].([]any)
	if !ok || len(got) != 0 {
		t.Fatalf("index changes not empty: %v", decoded["changes"])
	}
}

func TestPlanComparisonUnicodePath(t *testing.T) {
	r := raw(":100644 100644 " + zero40 + " " + zero40 + " M\x00café.txt\x00")
	res, err := planComparison(comparisonPlanInput{Raw: r})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	change := res["changes"].([]any)[0].(map[string]any)
	base := change["base"].(map[string]any)
	if base["path"] != "café.txt" {
		t.Fatalf("unicode path wrong: %q", base["path"])
	}
	if base["material"] != "base/café.txt" {
		t.Fatalf("unicode material wrong: %q", base["material"])
	}

	r2 := raw(":100644 100644 " + zero40 + " " + zero40 + " A\x00日本語/ファイル.txt\x00")
	res, err = planComparison(comparisonPlanInput{Raw: r2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cand := res["changes"].([]any)[0].(map[string]any)["candidate"].(map[string]any)
	if cand["path"] != "日本語/ファイル.txt" {
		t.Fatalf("CJK path wrong: %q", cand["path"])
	}
}

func TestPlanComparisonIllegalRecordErrors(t *testing.T) {
	// Header with too few fields.
	if _, err := planComparison(comparisonPlanInput{Raw: raw(":100644 100644 " + zero40 + " Z\x00file.txt\x00")}); err == nil {
		t.Fatal("expected error for malformed header, got nil")
	}
	// Rename record missing its new path.
	if _, err := planComparison(comparisonPlanInput{Raw: raw(":100644 100644 " + zero40 + " " + zero40 + " R100\x00onlyone.txt\x00")}); err == nil {
		t.Fatal("expected error for missing rename target, got nil")
	}
	// Record missing its path entirely.
	if _, err := planComparison(comparisonPlanInput{Raw: raw(":100644 100644 " + zero40 + " " + zero40 + " M\x00")}); err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}
