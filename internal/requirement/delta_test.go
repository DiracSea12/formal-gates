package requirement

import (
	"reflect"
	"strings"
	"testing"
)

func TestTextDeltaPreservesDisjointChangesAndOriginalLineBytes(t *testing.T) {
	base := "保留\n旧一\n共同行\n旧二\n尾部"
	candidate := "保留\n新一🙂\n共同行\n新增\n新二\n尾部\n"
	want := []TextHunk{
		{BaseStart: 2, CandidateStart: 2, Removed: []string{"旧一\n"}, Added: []string{"新一🙂\n"}},
		{BaseStart: 4, CandidateStart: 4, Removed: []string{"旧二\n", "尾部"}, Added: []string{"新增\n", "新二\n", "尾部\n"}},
	}
	if got := TextDelta(base, candidate); !reflect.DeepEqual(got, want) {
		t.Fatalf("hunks=%#v, want %#v", got, want)
	}
}

func TestTextDeltaReconstructsCompleteCandidate(t *testing.T) {
	for _, texts := range [][2]string{
		{"", "新增\n"}, {"删除", ""}, {"a\na\nb\n", "a\nb\na\n"},
		{"same\n", "same\n"}, {"a\nb\nc\nd\n", "a\n替换\nc\n新增\nd\n"},
		{"末尾\n", "末尾"}, {"末尾", "末尾\n"},
	} {
		hunks := TextDelta(texts[0], texts[1])
		lines := strings.SplitAfter(texts[0], "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		var reconstructed strings.Builder
		at := 0
		for _, hunk := range hunks {
			start := hunk.BaseStart - 1
			reconstructed.WriteString(strings.Join(lines[at:start], ""))
			if !reflect.DeepEqual(lines[start:start+len(hunk.Removed)], hunk.Removed) {
				t.Fatalf("removed original bytes differ: %#v", hunk)
			}
			reconstructed.WriteString(strings.Join(hunk.Added, ""))
			at = start + len(hunk.Removed)
		}
		reconstructed.WriteString(strings.Join(lines[at:], ""))
		if reconstructed.String() != texts[1] {
			t.Fatalf("candidate %q reconstructed as %q", texts[1], reconstructed.String())
		}
	}
}
