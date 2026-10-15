// P4 delete/modify conflicts preserve native source metadata and semantic choices
// through observation, resolution, submission and reopened providers.
package vcs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestP4DeleteModifyConflictCarriesNativeResolution(t *testing.T) {
	t.Parallel()
	for _, operation := range []struct {
		direction, choice string
		lost              bool
	}{
		{"incoming-deletion", "keep-file", false},
		{"incoming-deletion", "delete-file", false},
		{"target-deletion", "keep-file", false},
		{"target-deletion", "delete-file", false},
		{"target-deletion", "keep-file", true},
	} {
		name := operation.direction + "/" + operation.choice
		if operation.lost {
			name += "/lost-response"
		}
		t.Run(name, func(t *testing.T) {
			direction, choice := operation.direction, operation.choice
			p, runner := p4Fixture(t)
			base := nativeIdentityAt(t, p)
			target, handle := nativeWorkspaceAt(t, p, base, "conflict-target")
			incoming, _ := nativeWorkspaceAt(t, p, base, "conflict-incoming")
			deleting, editing := incoming, target
			if direction == "target-deletion" {
				deleting, editing = target, incoming
			}
			if err := os.Remove(filepath.Join(deleting.root, "body.txt")); err != nil {
				t.Fatal(err)
			}
			if err := deleting.Track([]string{"body.txt"}); err != nil {
				t.Fatal(err)
			}
			if err := deleting.Commit("delete selected file"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(editing.root, "body.txt"), []byte("changed input\n"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := editing.Track([]string{"body.txt"}); err != nil {
				t.Fatal(err)
			}
			if err := editing.Commit("edit selected file"); err != nil {
				t.Fatal(err)
			}
			prefix := nativeIdentityAt(t, target)
			source := nativeIdentityAt(t, incoming)
			if err := target.Merge(source); err != nil {
				t.Fatal(err)
			}
			observed, err := target.ObserveIntegration(prefix, source)
			if err != nil {
				t.Fatal(err)
			}
			position := slices.IndexFunc(observed.Entries, func(entry IndexEntry) bool { return entry.Path == "body.txt" && entry.Stage != 0 })
			if !observed.Started || position < 0 {
				t.Fatalf("native delete/modify conflict missing: %+v", observed)
			}
			var entry nativeEntry
			if err := json.Unmarshal([]byte(observed.Entries[position].Metadata), &entry); err != nil {
				t.Fatal(err)
			}
			sourceID, _ := decodeNative(source)
			unresolvedSource := false
			for key, value := range entry.Resolve {
				if strings.HasPrefix(key, "resolveAction") && value == "unresolved" {
					source := entry.Resolve["resolveFromFile"+strings.TrimPrefix(key, "resolveAction")]
					unresolvedSource = unresolvedSource || slices.ContainsFunc(sourceID.Files, func(a nativeAddress) bool { return a.Depot == source })
				}
			}
			if entry.Action == "" || !unresolvedSource {
				t.Fatalf("native conflict action/source lost: %+v", entry)
			}
			repeated, err := target.ObserveIntegration(prefix, source)
			if err != nil || !reflect.DeepEqual(repeated, observed) {
				t.Fatalf("repeat observation changed conflict: %+v %v", repeated, err)
			}
			wanted := []byte("semantic selected content\n")
			if choice == "keep-file" {
				if err := os.WriteFile(filepath.Join(target.root, "body.txt"), wanted, 0755); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(filepath.Join(target.root, "body.txt")); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			if operation.lost {
				runner.lost = "resolve"
				if err := target.ResolveConflicts([]string{"body.txt"}); err == nil || !runner.didLose {
					t.Fatalf("native completed response not lost: %v", err)
				}
				actual, err := os.ReadFile(filepath.Join(target.root, "body.txt"))
				if err != nil || !bytes.Equal(actual, wanted) {
					t.Fatalf("lost response changed selected bytes: %q %v", actual, err)
				}
			}
			target = p.At(target.root, handle)
			if err := target.ResolveConflicts([]string{"body.txt"}); err != nil {
				t.Fatal(err)
			}
			resolved, err := target.ObserveIntegration(prefix, source)
			if err != nil || slices.ContainsFunc(resolved.Entries, func(entry IndexEntry) bool { return entry.Stage != 0 }) {
				t.Fatalf("semantic result still unresolved: %+v %v", resolved, err)
			}
			if choice == "delete-file" && slices.ContainsFunc(resolved.Entries, func(entry IndexEntry) bool { return entry.Path == "body.txt" }) {
				t.Fatalf("resolved deletion retained material: %+v", resolved)
			}
			repeated, err = target.ObserveIntegration(prefix, source)
			if err != nil || !reflect.DeepEqual(repeated, resolved) {
				t.Fatalf("reopened resolution changed native index: %+v %v", repeated, err)
			}
			message := "integrate selected conflict result"
			if err := target.Commit(message); err != nil {
				t.Fatal(err)
			}
			final := nativeIdentityAt(t, target)
			if err := target.VerifyIntegration(prefix, source, final, message); err != nil {
				t.Fatal(err)
			}
			values, err := target.TreeEntries(final)
			if err != nil {
				t.Fatal(err)
			}
			if choice == "delete-file" {
				if _, present := values["body.txt"]; present {
					t.Fatalf("chosen deletion materialized: %+v", values)
				}
			} else {
				var result nativeEntry
				if err := json.Unmarshal([]byte(values["body.txt"]), &result); err != nil || !bytes.Equal(result.Content, wanted) || result.Filetype != "text+x" {
					t.Fatalf("semantic content/filetype changed: %+v %v", result, err)
				}
			}
			for i := 0; i < 2; i++ {
				observed, found, err := target.ObservedCommit(prefix, message)
				count, countErr := target.CommitCount(prefix, final)
				if err != nil || !found || observed != final || countErr != nil || count != 1 {
					t.Fatalf("committed resolution replayed or changed: %q %t %d %v %v", observed, found, count, err, countErr)
				}
			}
			if err := target.VerifyReady(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
