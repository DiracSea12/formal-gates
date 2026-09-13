package gitprep

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPromotionPreservesConfirmedDocumentBytesAcrossCheckoutFilters(t *testing.T) {
	for _, name := range []string{"requirements.md", "需求 说明.md"} {
		t.Run(name, func(t *testing.T) {
			promotionPreservesConfirmedDocument(t, name)
		})
	}
}

func promotionPreservesConfirmedDocument(t *testing.T, name string) {
	root := repository(t)
	if _, err := Git(root, "config", "core.quotepath", "true"); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(root, "config", "core.autocrlf", "true"); err != nil {
		t.Fatal(err)
	}
	write(t, root, name, "original requirement\n")
	if _, err := Git(root, "add", name); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(root, "commit", "-qm", "requirement baseline"); err != nil {
		t.Fatal(err)
	}
	base, _ := Head(root)
	workspace := filepath.Join(t.TempDir(), "candidate")
	if err := Workspace(root, workspace, base); err != nil {
		t.Fatal(err)
	}
	want := []byte("confirmed requirement\nwith another line\n")
	write(t, workspace, name, string(want))
	commit, err := Commit(workspace, base, "confirmed requirement", []string{name})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, name, string(want))
	for _, operation := range []string{"promote", "replay"} {
		if _, err := Promote(root, commit.After, base, operation); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("confirmed bytes changed after %s: %q %v", operation, got, err)
		}
	}
}

func TestPromotionEntireCandidateAndRecovery(t *testing.T) {
	root := repository(t)
	base, _ := Head(root)
	workspace := filepath.Join(t.TempDir(), "candidate")
	if err := Workspace(root, workspace, base); err != nil {
		t.Fatal(err)
	}
	write(t, workspace, "feature.go", "package main\nconst feature = true\n")
	commit, err := Commit(workspace, base, "candidate", []string{"feature.go"})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := Promote(root, commit.After, base, "promote")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Target.Identity != commit.After {
		t.Fatal("promotion changed source identity")
	}
	replay, err := Promote(root, commit.After, base, "promote")
	if err != nil || replay.Target.Identity != receipt.Target.Identity {
		t.Fatal("native promotion recovery failed", err)
	}
}
func TestPromotionNewIdentityEquivalentAndRejectBranches(t *testing.T) {
	root := repository(t)
	source, err := Snapshot(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Git(root, "commit-tree", source.Tree, "-p", source.Identity, "-m", "Equivalent integrated identity")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := Promote(root, second, source.Identity, "native-equivalent")
	if err != nil || actual.Source.Identity == actual.Target.Identity || actual.Target.Identity != source.Identity {
		t.Fatal("native equivalent target did not preserve distinct identities", err)
	}
	target, err := Snapshot(root, second)
	if err != nil {
		t.Fatal(err)
	}
	receipt := PromotionReceipt{OperationID: "op", ExpectedTargetBase: source.Identity, Source: source, Target: target, Ref: "refs/heads/main", Status: "COMPLETE", ConflictFree: true}
	if err := ValidatePromotion(receipt, source, source.Identity, "op"); err != nil {
		t.Fatal("equivalent new identity rejected", err)
	}
	for _, name := range []string{"unknown", "conflict", "drift", "digest", "mode", "metadata"} {
		t.Run(name, func(t *testing.T) {
			r := receipt
			switch name {
			case "unknown":
				r.Status = "UNKNOWN"
			case "conflict":
				r.ConflictFree = false
			case "drift":
				r.ExpectedTargetBase = "other"
			case "digest":
				r.Target.Digest = "different"
			case "mode":
				r.Target.Entries = append([]Entry{}, target.Entries...)
				r.Target.Entries[0].Mode = "100755"
			case "metadata":
				r.Target.ProviderMetadata = map[string]string{"core.autocrlf": "true"}
			}
			if ValidatePromotion(r, source, source.Identity, "op") == nil {
				t.Fatal("invalid promotion carried evidence")
			}
		})
	}
}
