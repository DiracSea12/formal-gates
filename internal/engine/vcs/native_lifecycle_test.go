package vcs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	path := filepath.Join(t.TempDir(), id)
	if err := p.CreateWorkspace(path, base, "workspace "+id, h); err != nil {
		t.Fatal(err)
	}
	return p.At(path, h), h
}
func TestNativeWorkspaceRecoversCompletedSideEffects(t *testing.T) {
	for _, name := range []string{"svn", "p4"} {
		t.Run(name, func(t *testing.T) {
			p, runner := nativeFixture(t, name)
			base := nativeIdentityAt(t, p)
			h := PlanWorkspace(name, base, "response", "candidate")
			path := filepath.Join(t.TempDir(), "candidate")
			runner.lost = "copy"
			if name == "p4" {
				runner.lost = "populate"
			}
			if err := p.CreateWorkspace(path, base, "bound creation", h); err == nil || !runner.didLose {
				t.Fatalf("completed side effect was not interrupted: %v", err)
			}
			if err := p.CreateWorkspace(path, base, "bound creation", h); err != nil {
				t.Fatal(err)
			}
			q := p.At(path, h)
			created := nativeIdentityAt(t, q)
			if nativeDigestAt(t, p, base) != nativeDigestAt(t, q, created) {
				t.Fatal("recovered copy changed values")
			}
			if err := p.CreateWorkspace(path, base, "bound creation", h); err != nil || nativeIdentityAt(t, q) != created {
				t.Fatalf("repetition changed identity: %v", err)
			}
			for _, message := range []string{"bound freeze", "bound creation"} {
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
			if err := q.Commit("bound freeze"); err == nil || !runner.didLose {
				t.Fatalf("completed commit was not interrupted: %v", err)
			}
			observed, found, err := q.ObservedCommit(created, "bound freeze")
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
			if err := p.CleanupWorkspace(path, "bound cleanup", h); err == nil || !runner.didLose {
				t.Fatalf("cleanup was not interrupted: %v", err)
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
	for _, name := range []string{"svn", "p4"} {
		t.Run(name, func(t *testing.T) {
			p, _ := nativeFixture(t, name)
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
			if err := first.Commit("resolved merge"); err != nil {
				t.Fatal(err)
			}
			if err := first.VerifyIntegration(prefix, incoming, nativeIdentityAt(t, first), "resolved merge"); err != nil {
				t.Fatal(err)
			}
			entry, err := first.Entry(nativeIdentityAt(t, first), "body.txt")
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
		})
	}
}
func TestP4OrderedViewAndStreamIdentity(t *testing.T) {
	for _, mode := range []string{"ordered-view", "stream"} {
		t.Run(mode, func(t *testing.T) {
			p, r := p4Fixture(t)
			if mode == "ordered-view" {
				overlayRoot := t.TempDir()
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
				root := filepath.Join(t.TempDir(), "stream")
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
