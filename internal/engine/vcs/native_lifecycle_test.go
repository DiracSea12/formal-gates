package vcs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

func nativeFixture(t *testing.T, name string) (*Provider, *nativeTestRunner) {
	t.Helper()
	if name == "svn" {
		return svnFixture(t)
	}
	return p4Fixture(t)
}
func nativeIdentityAt(t *testing.T, p *Provider) string {
	t.Helper()
	id, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func nativeDigestAt(t *testing.T, p *Provider, id string) string {
	t.Helper()
	d, err := p.TreeDigest(id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func nativeWorkspaceAt(t *testing.T, p *Provider, base, id string) (*Provider, WorkspaceHandle) {
	t.Helper()
	h := PlanWorkspace(p.Name(), base, "owned", id)
	path := filepath.Join(nativeFixtureDirectory(t), id)
	if err := p.CreateWorkspace(path, base, "workspace "+id, h); err != nil {
		t.Fatal(err)
	}
	return p.At(path, h), h
}
func TestNativeWorkspaceRecoversCompletedSideEffects(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"svn", "p4"} {
		t.Run(name, func(t *testing.T) {
			p, runner := nativeFixture(t, name)
			base := nativeIdentityAt(t, p)
			h := PlanWorkspace(name, base, "response", "candidate")
			path := filepath.Join(nativeFixtureDirectory(t), "candidate")
			message := "bound creation " + strings.Repeat("exact source operation ", 8)
			if identity, found, err := p.ObservedWorkspace(path, base, message, h); err != nil || found || identity != "" {
				t.Fatalf("unperformed copy was observed: %q %t %v", identity, found, err)
			}
			runner.lost = "copy"
			if name == "p4" {
				runner.lost = "populate"
			}
			if err := p.CreateWorkspace(path, base, "bound creation "+strings.Repeat("exact source operation ", 8), h); err == nil || !runner.didLose {
				t.Fatalf("completed side effect was not interrupted: %v", err)
			}
			observedCopy, found, err := p.ObservedWorkspace(path, base, message, h)
			if err != nil || !found || nativeDigestAt(t, p, observedCopy) != nativeDigestAt(t, p, base) {
				t.Fatalf("exact native copy not observed before local continuation: %q %t %v", observedCopy, found, err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("readonly copy observation created a local workspace: %v", err)
			}
			if err := p.CreateWorkspace(path, base, "bound creation "+strings.Repeat("exact source operation ", 8), h); err != nil {
				t.Fatal(err)
			}
			q := p.At(path, h)
			created := nativeIdentityAt(t, q)
			if nativeDigestAt(t, p, base) != nativeDigestAt(t, q, created) {
				t.Fatal("recovered copy changed values")
			}
			if err := p.CreateWorkspace(path, base, "bound creation "+strings.Repeat("exact source operation ", 8), h); err != nil || nativeIdentityAt(t, q) != created {
				t.Fatalf("repetition changed identity: %v", err)
			}
			for _, message := range []string{"bound freeze " + strings.Repeat("exact submitted operation ", 8), "bound creation " + strings.Repeat("exact source operation ", 8)} {
				if observed, found, err := q.ObservedCommit(created, message); err != nil || found || observed != "" {
					t.Fatalf("no-effect prefix has a later commit: %q %t %v", observed, found, err)
				}
			}
			if err := os.WriteFile(filepath.Join(path, "added"), []byte(" leading\n\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := q.Track([]string{"added"}); err != nil {
				t.Fatal(err)
			}
			runner.didLose = false
			runner.lost = "commit"
			if name == "p4" {
				runner.lost = "submit"
			}
			if err := q.Commit("bound freeze " + strings.Repeat("exact submitted operation ", 8)); err == nil || !runner.didLose {
				t.Fatalf("completed commit was not interrupted: %v", err)
			}
			observed, found, err := q.ObservedCommit(created, "bound freeze "+strings.Repeat("exact submitted operation ", 8))
			if err != nil || !found {
				t.Fatalf("committed identity not observed: %v", err)
			}
			if err := q.Synchronize(observed); err != nil {
				t.Fatal(err)
			}
			if err := q.VerifyReady(); err != nil {
				t.Fatal(err)
			}
			count, err := q.CommitCount(created, observed)
			if err != nil || count != 1 {
				t.Fatalf("freeze side effect repeated: count %d %v", count, err)
			}
			want := nativeDigestAt(t, q, observed)
			runner.didLose = false
			runner.lost = "delete"
			if name == "p4" {
				runner.lost = "submit"
			}
			if err := p.CleanupWorkspace(path, "bound cleanup", h); err == nil || !runner.didLose {
				t.Fatalf("cleanup was not interrupted: %v", err)
			}
			if observed, err := p.ObservedWorkspaceCleanup(base, "bound cleanup", h); err != nil || !observed {
				t.Fatalf("exact native cleanup not observed before local continuation: %t %v", observed, err)
			}
			if observed, err := p.ObservedWorkspaceCleanup(base, "unsubmitted cleanup", h); err != nil || observed {
				t.Fatalf("unperformed native cleanup was observed: %t %v", observed, err)
			}
			if err := p.CleanupWorkspace(path, "bound cleanup", h); err != nil {
				t.Fatal(err)
			}
			absent, err := p.WorkspaceAbsent(path, h)
			if err != nil || !absent {
				t.Fatalf("resource remains: %v", err)
			}
			if nativeDigestAt(t, p, observed) != want {
				t.Fatal("cleanup changed historical values")
			}
		})
	}
}
func TestNativeIndependentWorkspacesIntegrateFixedInputs(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"svn", "p4"} {
		t.Run(name, func(t *testing.T) {
			p, _ := nativeFixture(t, name)
			base := nativeIdentityAt(t, p)
			master, mh := nativeWorkspaceAt(t, p, base, "mainline")
			incoming := make([]string, 2)
			var workers sync.WaitGroup
			for i, lane := range []string{"alpha", "beta"} {
				workers.Add(1)
				go func() {
					defer workers.Done()
					child, h := nativeWorkspaceAt(t, p, base, lane)
					if err := os.WriteFile(filepath.Join(child.root, lane), []byte(lane+"\n"), 0644); err != nil {
						t.Error(err)
						return
					}
					if err := child.Track([]string{lane}); err != nil {
						t.Error(err)
						return
					}
					if err := child.Commit("child " + lane); err != nil {
						t.Error(err)
						return
					}
					incoming[i] = nativeIdentityAt(t, child)
					t.Cleanup(func() {
						if err := p.CleanupWorkspace(child.root, "cleanup "+lane, h); err != nil {
							t.Error(err)
						}
					})
				}()
			}
			workers.Wait()
			if t.Failed() {
				return
			}
			for i, id := range incoming {
				prefix := nativeIdentityAt(t, master)
				if err := master.Merge(id); err != nil {
					t.Fatal(err)
				}
				observation, err := master.ObserveIntegration(prefix, id)
				if err != nil || !observation.Started {
					t.Fatalf("incoming effect not observed: %+v %v", observation, err)
				}
				if slices.ContainsFunc(observation.Entries, func(e IndexEntry) bool { return e.Stage != 0 }) {
					t.Fatal("independent child produced a conflict")
				}
				message := fmt.Sprintf("merge child %d", i)
				if err := master.Commit(message); err != nil {
					t.Fatal(err)
				}
				if err := master.VerifyIntegration(prefix, id, nativeIdentityAt(t, master), message); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := master.TreeEntries(nativeIdentityAt(t, master))
			if err != nil {
				t.Fatal(err)
			}
			if entries["alpha"] == "" || entries["beta"] == "" {
				t.Fatal("fixed merge omitted a child")
			}
			if err := p.CleanupWorkspace(master.root, "cleanup mainline", mh); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestNativeConflictResolutionPreservesObservedInputs(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"svn", "p4"} {
		for _, lost := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/lost=%t", name, lost), func(t *testing.T) {
				p, runner := nativeFixture(t, name)
				base := nativeIdentityAt(t, p)
				first, h1 := nativeWorkspaceAt(t, p, base, "first")
				second, h2 := nativeWorkspaceAt(t, p, base, "second")
				for i, q := range []*Provider{first, second} {
					if err := os.WriteFile(filepath.Join(q.root, "body.txt"), []byte(fmt.Sprintf("choice %d\n", i)), 0644); err != nil {
						t.Fatal(err)
					}
					if err := q.Track([]string{"body.txt"}); err != nil {
						t.Fatal(err)
					}
					if err := q.Commit(fmt.Sprintf("choice %d", i)); err != nil {
						t.Fatal(err)
					}
				}
				prefix := nativeIdentityAt(t, first)
				incoming := nativeIdentityAt(t, second)
				if err := first.Merge(incoming); err != nil {
					t.Fatal(err)
				}
				proof, err := first.ObserveIntegration(prefix, incoming)
				if err != nil {
					t.Fatal(err)
				}
				if !proof.Started || !slices.ContainsFunc(proof.Entries, func(e IndexEntry) bool { return e.Stage != 0 && e.Path == "body.txt" }) {
					t.Fatalf("content conflict not observed: %+v", proof)
				}
				if err := os.WriteFile(filepath.Join(first.root, "body.txt"), []byte("semantic choice\n"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := first.ResolveConflicts([]string{"body.txt"}); err != nil {
					t.Fatal(err)
				}
				resolved, err := first.ObserveIntegration(prefix, incoming)
				if err != nil || slices.ContainsFunc(resolved.Entries, func(e IndexEntry) bool { return e.Stage != 0 }) {
					t.Fatalf("native resolution remains unresolved: %+v %v", resolved, err)
				}
				if name == "p4" {
					records, err := first.p4JSON("fstat", "-Or", "//"+first.p4Client()+"/body.txt")
					if err != nil || len(records) != 1 || records[0]["resolveAction0"] != "edit from" || records[0]["type"] != "text+x" {
						t.Fatalf("semantic merge lacks native edit lineage/type: %+v %v", records, err)
					}
				}
				message := "resolved merge " + strings.Repeat("bound semantic input ", 8)
				if lost {
					runner.didLose = false
					runner.lost = "commit"
					if name == "p4" {
						runner.lost = "submit"
					}
				}
				if err := first.Commit(message); lost {
					if err == nil || !runner.didLose {
						t.Fatalf("completed semantic commit response not lost: %v", err)
					}
					observed, found, err := first.ObservedCommit(prefix, message)
					if err != nil || !found {
						t.Fatalf("semantic commit effect not observed: %v", err)
					}
					if err := first.Synchronize(observed); err != nil {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if err := first.VerifyIntegration(prefix, incoming, nativeIdentityAt(t, first), message); err != nil {
					t.Fatal(err)
				}
				final := nativeIdentityAt(t, first)
				for i := 0; i < 2; i++ {
					observed, found, err := first.ObservedCommit(prefix, message)
					count, countErr := first.CommitCount(prefix, final)
					if err != nil || !found || observed != final || countErr != nil || count != 1 {
						t.Fatalf("semantic effect repeated or changed: %q %t %d %v %v", observed, found, count, err, countErr)
					}
				}
				entry, err := first.Entry(final, "body.txt")
				var value nativeEntry
				if err != nil || json.Unmarshal([]byte(entry), &value) != nil || string(value.Content) != "semantic choice\n" {
					t.Fatalf("resolved native content differs: %+v %v", value, err)
				}
				for _, q := range []*Provider{first, second} {
					if err := q.VerifyReady(); err != nil {
						t.Fatal(err)
					}
				}
				if err := p.CleanupWorkspace(first.root, "cleanup first", h1); err != nil {
					t.Fatal(err)
				}
				if err := p.CleanupWorkspace(second.root, "cleanup second", h2); err != nil {
					t.Fatal(err)
				}
				if historical, err := p.Entry(final, "body.txt"); err != nil || historical != entry {
					t.Fatalf("cleanup changed semantic native history: %q %v", historical, err)
				}
			})
		}
	}
}
func TestP4OrderedViewAndStreamIdentity(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"ordered-view", "stream"} {
		t.Run(mode, func(t *testing.T) {
			p, r := p4Fixture(t)
			if mode == "ordered-view" {
				overlayRoot := nativeFixtureDirectory(t)
				overlaySpec := "Client: overlay-supplier\nOwner: fixture\nRoot: " + overlayRoot + "\nView:\n\t//depot/overlay/... //overlay-supplier/...\n"
				if _, err := r.RunRaw(overlayRoot, "p4", []byte(overlaySpec), "client", "-i"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(overlayRoot, "body.txt"), []byte("overlay value\n"), 0755); err != nil {
					t.Fatal(err)
				}
				nativeCommand(t, r, overlayRoot, "p4", "-c", "overlay-supplier", "add", "-t", "text+x", "body.txt")
				nativeCommand(t, r, overlayRoot, "p4", "-c", "overlay-supplier", "submit", "-d", "overlay values")
				spec := "Client: main\nOwner: fixture\nRoot: " + p.root + "\nLineEnd: unix\nView:\n\t//depot/main/... //main/...\n\t-//depot/main/excluded/... //main/excluded/...\n\t+//depot/overlay/... //main/...\n\t&//depot/main/body.txt //main/duplicate.txt\n"
				if _, err := r.RunRaw(p.root, "p4", []byte(spec), "client", "-i"); err != nil {
					t.Fatal(err)
				}
				nativeCommand(t, r, p.root, "p4", "sync", "-f")
			} else {
				depot := "Depot: stream\nOwner: fixture\nType: stream\nStreamDepth: //stream/1\nMap: stream/...\n"
				if _, err := r.RunRaw(p.root, "p4", []byte(depot), "depot", "-i"); err != nil {
					t.Fatal(err)
				}
				stream := "Stream: //stream/main\nOwner: fixture\nName: main\nParent: none\nType: mainline\nParentView: inherit\nPaths:\n\tshare ...\n\timport vendor/... //depot/main/...\n\texclude excluded/...\nRemapped:\n\tassets/... material/...\nIgnored:\n\t.tmp\n"
				if _, err := r.RunRaw(p.root, "p4", []byte(stream), "stream", "-i"); err != nil {
					t.Fatal(err)
				}
				root := filepath.Join(nativeFixtureDirectory(t), "stream")
				if err := os.MkdirAll(root, 0755); err != nil {
					t.Fatal(err)
				}
				spec := "Client: stream-main\nOwner: fixture\nRoot: " + root + "\nLineEnd: unix\nStream: //stream/main\n"
				if _, err := r.RunRaw(root, "p4", []byte(spec), "client", "-i"); err != nil {
					t.Fatal(err)
				}
				p = NewNative(root, "p4", r).At(root, WorkspaceHandle{Client: "stream-main"})
				if err := os.WriteFile(filepath.Join(root, "own.txt"), []byte(" stream value \n"), 0644); err != nil {
					t.Fatal(err)
				}
				if _, err := p.p4Raw(nil, "add", "own.txt"); err != nil {
					t.Fatal(err)
				}
				if err := p.Commit("stream fixture"); err != nil {
					t.Fatal(err)
				}
			}
			base := nativeIdentityAt(t, p)
			source, _ := decodeNative(base)
			q, h := nativeWorkspaceAt(t, p, base, "native-view")
			created := nativeIdentityAt(t, q)
			if nativeDigestAt(t, p, base) != nativeDigestAt(t, q, created) {
				t.Fatalf("%s copy changed complete View/translation", mode)
			}
			if mode == "ordered-view" && (len(source.View) != 4 || !strings.Contains(source.View[1], "-//") || !strings.Contains(source.View[2], "+//") || !strings.Contains(source.View[3], "&//")) {
				t.Fatal("ordered View was not retained")
			}
			if mode == "ordered-view" {
				values, err := p.TreeEntries(base)
				if err != nil {
					t.Fatal(err)
				}
				// Native sync selects the overlay at body.txt and the ditto at
				// duplicate.txt. Check their effective values and original origins.
				for _, expected := range []struct{ path, content, origin string }{
					{"body.txt", "overlay value\n", "//depot/overlay/body.txt"},
					{"duplicate.txt", " leading\ntrailing\n\n", "//depot/main/body.txt"},
				} {
					var value nativeEntry
					if err := json.Unmarshal([]byte(values[expected.path]), &value); err != nil || string(value.Content) != expected.content || value.Filetype != "text+x" || value.Origin != expected.origin {
						t.Fatalf("effective mapping %s differs: %+v %v", expected.path, value, err)
					}
					for _, root := range []string{p.root, q.root} {
						body, err := os.ReadFile(filepath.Join(root, expected.path))
						if err != nil || string(body) != expected.content {
							t.Fatalf("synced mapping %s differs at %s: %q %v", expected.path, root, body, err)
						}
					}
				}
			}
			before, err := q.TreeEntries(created)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.CleanupWorkspace(q.root, "view cleanup", h); err != nil {
				t.Fatal(err)
			}
			after, err := p.TreeEntries(created)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(before)
			b, _ := json.Marshal(after)
			if string(a) != string(b) {
				t.Fatal("deleted spec changed historical View")
			}
		})
	}
}

// A synced snapshot includes submitted removals, even when no project file remains.
func TestP4WorkspaceSnapshotPreservesMovesAndDeletions(t *testing.T) {
	t.Parallel()
	p, _ := p4Fixture(t)
	base := nativeIdentityAt(t, p)
	isolated, handle := nativeWorkspaceAt(t, p, base, "isolated")
	isolatedBase := nativeIdentityAt(t, isolated)
	nativeCommand(t, p.runner, p.root, "p4", "edit", "body.txt")
	nativeCommand(t, p.runner, p.root, "p4", "move", "body.txt", "renamed.txt")
	if err := p.Commit("move project file"); err != nil {
		t.Fatal(err)
	}
	moved := nativeIdentityAt(t, p)
	entries, err := p.TreeEntries(moved)
	var value nativeEntry
	if err != nil || entries["body.txt"] != "" || json.Unmarshal([]byte(entries["renamed.txt"]), &value) != nil || string(value.Content) != " leading\ntrailing\n\n" || value.Filetype != "text+x" {
		t.Fatalf("move snapshot lost native values: %+v %v", entries, err)
	}
	if err := os.Remove(filepath.Join(p.root, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	if err := p.Track([]string{"renamed.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("remove all project files"); err != nil {
		t.Fatal(err)
	}
	empty := nativeIdentityAt(t, p)
	id, err := decodeNative(empty)
	if err != nil || id.Revision <= 1 {
		t.Fatalf("deleted workspace has no submitted identity: %+v %v", id, err)
	}
	message, err := p.Message(empty)
	if err != nil || message != "remove all project files" {
		t.Fatalf("snapshot is not the actual deletion submission: %q %v", message, err)
	}
	entries, err = p.TreeEntries(empty)
	if err != nil || len(entries) != 1 || entries["."] == "" {
		t.Fatalf("fully deleted snapshot contains project files: %+v %v", entries, err)
	}
	if nativeDigestAt(t, p, empty) == nativeDigestAt(t, p, moved) || nativeIdentityAt(t, p) != empty {
		t.Fatal("removal digest or repeated identity differs")
	}
	if nativeIdentityAt(t, isolated) != isolatedBase || nativeDigestAt(t, isolated, isolatedBase) != nativeDigestAt(t, p, base) {
		t.Fatal("independent workspace followed another client's submission")
	}
	if err := os.WriteFile(filepath.Join(isolated.root, "body.txt"), []byte("independent contents\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := isolated.Track([]string{"body.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := isolated.Commit("independent modification"); err != nil {
		t.Fatal(err)
	}
	if nativeIdentityAt(t, p) != empty {
		t.Fatal("workspace snapshot followed the latest global change")
	}
	if err := p.Synchronize(base); err != nil {
		t.Fatal(err)
	}
	if nativeIdentityAt(t, p) != base {
		t.Fatal("fixed old workspace followed a later client submission")
	}
	if err := p.Synchronize(moved); err != nil {
		t.Fatal(err)
	}
	if nativeIdentityAt(t, p) != moved || nativeDigestAt(t, p, empty) == nativeDigestAt(t, p, moved) {
		t.Fatal("fixed moved revision lost its deletion boundary")
	}
	if err := p.CleanupWorkspace(isolated.root, "cleanup isolated workspace", handle); err != nil {
		t.Fatal(err)
	}
}

func TestP4DeletionObservationRetainsSubmittedIdentity(t *testing.T) {
	t.Parallel()
	p, runner := p4Fixture(t)
	base := nativeIdentityAt(t, p)
	master, handle := nativeWorkspaceAt(t, p, base, "mainline")
	prefix := nativeIdentityAt(t, master)
	if err := os.Remove(filepath.Join(p.root, "body.txt")); err != nil {
		t.Fatal(err)
	}
	if err := p.Track([]string{"body.txt"}); err != nil {
		t.Fatal(err)
	}
	runner.lost = "submit"
	if err := p.Commit("exact complete deletion"); err == nil || !runner.didLose {
		t.Fatalf("completed deletion response was not lost: %v", err)
	}
	observed, found, err := p.ObservedCommit(base, "exact complete deletion")
	if err != nil || !found {
		t.Fatalf("exact deletion submission was not observed: %t %v", found, err)
	}
	for i := 0; i < 2; i++ {
		if err := p.Synchronize(observed); err != nil {
			t.Fatal(err)
		}
		if nativeIdentityAt(t, p) != observed {
			t.Fatal("recovery resolved another native identity")
		}
		entries, err := p.TreeEntries(observed)
		count, countErr := p.CommitCount(base, observed)
		if err != nil || len(entries) != 1 || entries["."] == "" || countErr != nil || count != 1 {
			t.Fatalf("recovery changed content or resubmitted: %+v count=%d %v %v", entries, count, err, countErr)
		}
	}
	if err := master.Merge(observed); err != nil {
		t.Fatal(err)
	}
	integration, err := master.ObserveIntegration(prefix, observed)
	if err != nil || !integration.Started {
		t.Fatalf("submitted deletion integration was not observed: %+v %v", integration, err)
	}
	if err := master.Commit("integrate complete deletion"); err != nil {
		t.Fatal(err)
	}
	final := nativeIdentityAt(t, master)
	if err := master.VerifyIntegration(prefix, observed, final, "integrate complete deletion"); err != nil {
		t.Fatal(err)
	}
	if nativeDigestAt(t, master, final) != nativeDigestAt(t, p, observed) {
		t.Fatal("integration changed the complete deletion snapshot")
	}
	if err := p.CleanupWorkspace(master.root, "cleanup deletion mainline", handle); err != nil {
		t.Fatal(err)
	}
}

func TestP4WorkspaceMaterialSurvivesAbsentPathHistory(t *testing.T) {
	t.Parallel()
	p, _ := p4Fixture(t)
	base := nativeIdentityAt(t, p)
	path := filepath.Join(p.root, "temporary.txt")
	if err := os.WriteFile(path, []byte("temporary contents\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := p.Track([]string{"temporary.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("add temporary project file"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := p.Track([]string{"temporary.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("remove temporary project file"); err != nil {
		t.Fatal(err)
	}
	if err := p.Synchronize(base); err != nil {
		t.Fatal(err)
	}
	current := nativeIdentityAt(t, p)
	originalEntries, err := p.TreeEntries(base)
	if err != nil {
		t.Fatal(err)
	}
	observedEntries, err := p.TreeEntries(current)
	if err != nil || !reflect.DeepEqual(originalEntries, observedEntries) || nativeDigestAt(t, p, base) != nativeDigestAt(t, p, current) {
		t.Fatalf("absent path history changed complete native material: original=%v observed=%v error=%v", originalEntries, observedEntries, err)
	}
	if bound, err := p.BindIdentity(base); err != nil || bound != base {
		t.Fatalf("explicit native identity changed: %q %v", bound, err)
	}
	var value nativeEntry
	if err := json.Unmarshal([]byte(originalEntries["body.txt"]), &value); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(p.root, "body.txt"))
	if err != nil || !bytes.Equal(body, value.Content) {
		t.Fatalf("workspace content differs from bound material: %q %v", body, err)
	}
	info, err := os.Stat(filepath.Join(p.root, "body.txt"))
	if err != nil || value.Filetype != "text+x" || runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		t.Fatalf("workspace lost its executable filetype: %v %v", info, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("absent path returned to the workspace: %v", err)
	}
	t.Logf("original=%s observed=%s completeEntries=%v", base, current, originalEntries)
}

func TestSVNMissingTreeConflictPreservesChosenMaterialAndLineage(t *testing.T) {
	t.Parallel()
	for _, restore := range []bool{false, true} {
		for _, lost := range []bool{false, true} {
			t.Run(fmt.Sprintf("restore=%t/lost=%t", restore, lost), func(t *testing.T) {
				p, runner := svnFixture(t)
				base := nativeIdentityAt(t, p)
				target, th := nativeWorkspaceAt(t, p, base, "deleted-target")
				source, sh := nativeWorkspaceAt(t, p, base, "modified-source")
				if err := os.Remove(filepath.Join(target.root, "body.txt")); err != nil {
					t.Fatal(err)
				}
				if err := target.Track([]string{"body.txt"}); err != nil {
					t.Fatal(err)
				}
				if err := target.Commit("delete target node"); err != nil {
					t.Fatal(err)
				}
				prefix := nativeIdentityAt(t, target)
				if err := os.WriteFile(filepath.Join(source.root, "body.txt"), []byte("incoming material\n"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := source.Track([]string{"body.txt"}); err != nil {
					t.Fatal(err)
				}
				if err := source.Commit("edit incoming node"); err != nil {
					t.Fatal(err)
				}
				incoming := nativeIdentityAt(t, source)
				if err := target.Merge(incoming); err != nil {
					t.Fatal(err)
				}
				proof, err := target.ObserveIntegration(prefix, incoming)
				if err != nil || !proof.Started || !slices.ContainsFunc(proof.Entries, func(e IndexEntry) bool { return e.Path == "body.txt" && e.Stage != 0 && e.Mode == "000000" }) {
					t.Fatalf("missing native tree conflict omitted: %+v %v", proof, err)
				}
				if restore {
					if err := os.WriteFile(filepath.Join(target.root, "body.txt"), []byte("chosen material\n"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				if lost {
					runner.lost = "resolve"
					runner.didLose = false
				}
				err = target.ResolveConflicts([]string{"body.txt"})
				if lost {
					if err == nil || !runner.didLose {
						t.Fatalf("completed resolve response loss absent: %v", err)
					}
					runner.lost = ""
					if err = target.ResolveConflicts([]string{"body.txt"}); err != nil {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				resolved, err := target.ObserveIntegration(prefix, incoming)
				if err != nil || slices.ContainsFunc(resolved.Entries, func(e IndexEntry) bool { return e.Stage != 0 }) {
					t.Fatalf("chosen resolution remains conflicted: %+v %v", resolved, err)
				}
				message := "bound native tree resolution"
				if lost {
					runner.lost = "commit"
					runner.didLose = false
				}
				err = target.Commit(message)
				if lost {
					if err == nil || !runner.didLose {
						t.Fatalf("completed commit response loss absent: %v", err)
					}
					runner.lost = ""
					observed, found, err := target.ObservedCommit(prefix, message)
					if err != nil || !found {
						t.Fatalf("native resolved commit absent: %t %v", found, err)
					}
					if err = target.Synchronize(observed); err != nil {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				final := nativeIdentityAt(t, target)
				if err = target.VerifyIntegration(prefix, incoming, final, message); err != nil {
					t.Fatal(err)
				}
				if err = target.VerifyReady(); err != nil {
					t.Fatal(err)
				}
				entry, err := target.Entry(final, "body.txt")
				if err != nil {
					t.Fatal(err)
				}
				if restore {
					var value nativeEntry
					if err = json.Unmarshal([]byte(entry), &value); err != nil || string(value.Content) != "chosen material\n" {
						t.Fatalf("restored choice changed: %+v %v", value, err)
					}
				} else if entry != "" {
					t.Fatal("chosen deletion was replaced with content")
				}
				count, err := target.CommitCount(prefix, final)
				if err != nil || count != 1 {
					t.Fatalf("resolution submitted %d commits: %v", count, err)
				}
				if err = p.CleanupWorkspace(target.root, "cleanup resolved target", th); err != nil {
					t.Fatal(err)
				}
				if err = p.CleanupWorkspace(source.root, "cleanup modified source", sh); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestSVNLiteralTargetsPreserveEncodedNativePaths(t *testing.T) {
	t.Parallel()
	p, runner := svnFixture(t)
	renamed := filepath.Join(filepath.Dir(p.root), "main @% names")
	if err := os.Rename(p.root, renamed); err != nil {
		t.Fatal(err)
	}
	p = p.At(renamed)
	base := nativeIdentityAt(t, p)
	name := "script file@% names.sh"
	if err := os.WriteFile(filepath.Join(p.root, name), []byte("echo source\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := p.Track([]string{name}); err != nil {
		t.Fatal(err)
	}
	nativeCommand(t, runner, p.root, "svn", "propset", "svn:executable", "true", "--", svnLiteral(name))
	if err := p.Commit("literal path delivery"); err != nil {
		t.Fatal(err)
	}
	source := nativeIdentityAt(t, p)
	values, err := p.TreeEntries(source)
	if err != nil {
		t.Fatal(err)
	}
	var value nativeEntry
	if err = json.Unmarshal([]byte(values[name]), &value); err != nil || value.Properties["svn:executable"] == nil || string(value.Content) != "echo source\n" {
		t.Fatalf("native path/property bytes differ: %+v %v", value, err)
	}
	bound, err := decodeNative(source)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := svnRepositoryURL(bound.Address, "/project @% names")
	if err != nil {
		t.Fatal(err)
	}
	nativeCommand(t, runner, p.root, "svn", "copy", svnURL(bound), svnLiteral(scope), "-m", "fixture project scope")
	scopedRoot := filepath.Join(nativeFixtureDirectory(t), "scoped @% names")
	nativeCommand(t, runner, p.root, "svn", "checkout", svnLiteral(scope), scopedRoot)
	scoped := p.At(scopedRoot)
	scopedBase := nativeIdentityAt(t, scoped)
	h := PlanWorkspace("svn", scopedBase, "literal", "candidate")
	h.URL, err = svnRepositoryURL(bound.Address, "/candidate @% names")
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(nativeFixtureDirectory(t), "candidate @% names")
	if err = scoped.CreateWorkspace(workspace, scopedBase, "bound literal copy", h); err != nil {
		t.Fatal(err)
	}
	candidate := scoped.At(workspace, h)
	copyID := nativeIdentityAt(t, candidate)
	if err = candidate.IsAncestorOrEqual(scopedBase, copyID); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(workspace, name), []byte("echo candidate\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = candidate.Track([]string{name}); err != nil {
		t.Fatal(err)
	}
	if err = candidate.Commit("bound literal candidate"); err != nil {
		t.Fatal(err)
	}
	current := nativeIdentityAt(t, candidate)
	if err = scoped.ApplyArtifact(current, []string{name}); err != nil {
		t.Fatal(err)
	}
	pending, err := scoped.Pending(scopedBase, current)
	if err != nil || !pending.Equal || !pending.Prefix {
		t.Fatalf("native literal apply differs: %+v %v", pending, err)
	}
	if err = scoped.Commit("bound literal promotion"); err != nil {
		t.Fatal(err)
	}
	if nativeDigestAt(t, scoped, nativeIdentityAt(t, scoped)) != nativeDigestAt(t, candidate, current) {
		t.Fatal("literal promotion changed complete values")
	}
	if err = scoped.CleanupWorkspace(workspace, "bound literal cleanup", h); err != nil {
		t.Fatal(err)
	}
	absent, err := scoped.WorkspaceAbsent(workspace, h)
	if err != nil || !absent {
		t.Fatalf("literal workspace remains: %t %v", absent, err)
	}
	if err = scoped.IsAncestorOrEqual(base, source); err != nil {
		t.Fatal(err)
	}
}

func TestSVNNodeKindReplacementPreservesDeclaredMaterial(t *testing.T) {
	t.Parallel()
	for _, change := range []struct{ name, path, kind string }{
		{"file-directory", "body.txt", "dir"}, {"directory-file", "empty", "file"},
		{"file-link", "body.txt", "link"}, {"link-file", "link", "file"},
	} {
		t.Run(change.name, func(t *testing.T) {
			p, runner := svnFixture(t)
			if change.kind == "file" && change.path == "empty" {
				if err := os.WriteFile(filepath.Join(p.root, "empty", "child"), []byte("old child\n"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := p.Track([]string{"empty/child"}); err != nil {
					t.Fatal(err)
				}
				if err := p.Commit("fixture directory contents"); err != nil {
					t.Fatal(err)
				}
			}
			base := nativeIdentityAt(t, p)
			q, h := nativeWorkspaceAt(t, p, base, "replacement")
			before := nativeIdentityAt(t, q)
			target := filepath.Join(q.root, change.path)
			if err := os.RemoveAll(target); err != nil {
				t.Fatal(err)
			}
			paths := []string{change.path}
			if change.kind == "file" && change.path == "empty" {
				paths = append(paths, "empty/child")
			}
			switch change.kind {
			case "dir":
				if err := os.Mkdir(target, 0755); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, change.path+"/child@value")
				if err := os.WriteFile(filepath.Join(target, "child@value"), []byte("replacement\n"), 0644); err != nil {
					t.Fatal(err)
				}
			case "link":
				svnWriteLink(t, target, "empty")
			default:
				if err := os.WriteFile(target, []byte("replacement\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if runtime.GOOS == "windows" {
				if change.kind == "link" {
					nativeCommand(t, runner, q.root, "svn", "propset", "svn:special", "true", "--", svnLiteral(change.path))
				} else if change.path == "link" {
					nativeCommand(t, runner, q.root, "svn", "propdel", "svn:special", "--", svnLiteral(change.path))
				}
			}
			actualPaths, err := q.ChangedPathsFrom(before)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(actualPaths, paths) {
				t.Fatalf("mechanical replacement paths=%q declared=%q", actualPaths, paths)
			}
			declared, pending, err := q.ValidateDeclaredChanges(before, paths)
			if err != nil {
				t.Fatal(err)
			}
			if len(declared) == 0 || pending.Clean {
				t.Fatal("replacement was not observed")
			}
			if change.kind == "dir" {
				runner.lost = "delete"
				runner.didLose = false
			}
			err = q.Track(paths)
			if change.kind == "dir" {
				if err == nil || !runner.didLose {
					t.Fatalf("completed replacement schedule response loss absent: %v", err)
				}
				runner.lost = ""
			} else if err != nil {
				t.Fatal(err)
			}
			if err = q.Track(paths); err != nil {
				t.Fatal("repeated replacement track", err)
			}
			if err = q.Commit("bound replacement"); err != nil {
				t.Fatal(err)
			}
			candidate := nativeIdentityAt(t, q)
			if change.kind == "link" {
				entry, err := q.Entry(candidate, change.path)
				if err != nil {
					t.Fatal(err)
				}
				var link nativeEntry
				if err := json.Unmarshal([]byte(entry), &link); err != nil || string(link.Content) != "link empty" || string(link.Properties["svn:special"]) != "*" || entryMode(entry) != "120000" {
					t.Fatalf("replacement link material = %+v, %v", link, err)
				}
			}
			delta, err := p.Changes(base, candidate)
			if err != nil {
				t.Fatal(err)
			}
			paths = nil
			for _, change := range delta {
				paths = append(paths, change.Path)
			}
			if change.kind == "file" && change.path == "empty" {
				note := filepath.Join(p.root, "empty", "private-note")
				if err := os.WriteFile(note, []byte("private material\n"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := p.ApplyArtifact(candidate, paths); err == nil {
					t.Fatal("replacement removed undeclared directory material")
				}
				body, err := os.ReadFile(note)
				if err != nil || string(body) != "private material\n" {
					t.Fatalf("undeclared material changed: %q %v", body, err)
				}
				if err = os.Remove(note); err != nil {
					t.Fatal(err)
				}
			}
			if err = p.ApplyArtifact(candidate, paths); err != nil {
				t.Fatal(err)
			}
			if err = p.ApplyArtifact(candidate, paths); err != nil {
				t.Fatal("repeated replacement apply", err)
			}
			pending, err = p.Pending(base, candidate)
			if err != nil || !pending.Equal || !pending.Prefix {
				t.Fatalf("replacement native values differ: %+v %v", pending, err)
			}
			if err = p.Commit("bound replacement promotion"); err != nil {
				t.Fatal(err)
			}
			if nativeDigestAt(t, p, nativeIdentityAt(t, p)) != nativeDigestAt(t, q, candidate) {
				t.Fatal("replacement promotion changed values")
			}
			if err = p.CleanupWorkspace(q.root, "bound replacement cleanup", h); err != nil {
				t.Fatal(err)
			}
		})
	}
}
