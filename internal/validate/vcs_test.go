package validate

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNativeVCSResolvers(t *testing.T) {
	for _, vcs := range []string{"git", "svn", "p4"} {
		if _, err := resolverForVCS(vcs, nil); err != nil {
			t.Fatalf("resolver %s: %v", vcs, err)
		}
	}
	if _, err := resolverForVCS("other", nil); err == nil {
		t.Fatal("unsupported resolver was accepted")
	}
	if !nativeSnapshotsEqual("git", strings.Repeat("a", 40), strings.Repeat("A", 40)) {
		t.Fatal("Git commit identity comparison became case-sensitive")
	}
	if nativeSnapshotsEqual("svn", `{"revision":"3","url":"https://svn.example/Trunk"}`, `{"revision":"3","url":"https://svn.example/trunk"}`) {
		t.Fatal("SVN repository URL context was compared case-insensitively")
	}
}

func TestNativeVCSResolverCommandShapes(t *testing.T) {
	gitID := strings.Repeat("a", 40)
	svnID := `{"revision":"123","url":"https://svn.example/project/trunk"}`
	p4ID := `{"change":"456","client":"workspace","view":["//depot/project/... //workspace/project/..."]}`
	p4Client := "... Client workspace\n... View0 //depot/project/... //workspace/project/..."
	for _, test := range []struct {
		name     string
		identity string
		outputs  []string
		want     [][]string
	}{
		{name: "git", identity: gitID, outputs: []string{"/repo", gitID, "/repo", gitID, "/repo", ""}, want: [][]string{{"git", "rev-parse", "--show-toplevel"}, {"git", "rev-parse", "HEAD"}, {"git", "rev-parse", "--show-toplevel"}, {"git", "rev-parse", "--verify", gitID + "^{commit}"}, {"git", "rev-parse", "--show-toplevel"}, {"git", "status", "--porcelain=v1", "--untracked-files=no"}}},
		{name: "svn", identity: svnID, outputs: []string{"/repo", "123", "https://svn.example/project/trunk", "/repo", "123", "/repo", ""}, want: [][]string{{"svn", "info", "--show-item", "wc-root", "/repo"}, {"svnversion", "/repo"}, {"svn", "info", "--show-item", "url", "/repo"}, {"svn", "info", "--show-item", "wc-root", "/repo"}, {"svn", "info", "--show-item", "revision", "-r", "123", "https://svn.example/project/trunk"}, {"svn", "info", "--show-item", "wc-root", "/repo"}, {"svn", "status", "--quiet", "/repo"}}},
		{name: "p4", identity: p4ID, outputs: []string{"/repo", "456", p4Client, "", "/repo", "456", "/repo", ""}, want: [][]string{{"p4", "-d", "/repo", "-ztag", "-F", "%clientRoot%", "info"}, {"p4", "-d", "/repo", "-ztag", "-F", "%change%", "changes", "-m", "1", "...#have"}, {"p4", "-d", "/repo", "-ztag", "client", "-o"}, {"p4", "-d", "/repo", "-ztag", "-F", "%depotFile%", "sync", "-n", "...@456"}, {"p4", "-d", "/repo", "-ztag", "-F", "%clientRoot%", "info"}, {"p4", "-d", "/repo", "-ztag", "-F", "%Change%", "change", "-o", "456"}, {"p4", "-d", "/repo", "-ztag", "-F", "%clientRoot%", "info"}, {"p4", "-d", "/repo", "-ztag", "-F", "%depotFile%", "opened", "..."}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedNativeRunner{outputs: test.outputs}
			resolver, err := resolverForVCS(test.name, runner)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := resolver.Resolve("/repo")
			if err != nil || identity != test.identity {
				t.Fatalf("resolve identity=%q err=%v", identity, err)
			}
			if err := resolver.Verify("/repo", identity); err != nil {
				t.Fatal(err)
			}
			if err := resolver.VerifyReady("/repo"); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runner.calls, test.want) {
				t.Fatalf("calls=%v want=%v", runner.calls, test.want)
			}
		})
	}
}

func TestSVNSnapshotIdentityBindsRepositoryURL(t *testing.T) {
	resolve := func(repositoryURL string) string {
		t.Helper()
		runner := &scriptedNativeRunner{outputs: []string{"/repo", "3", repositoryURL}}
		resolver, err := resolverForVCS("svn", runner)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := resolver.Resolve("/repo")
		if err != nil {
			t.Fatal(err)
		}
		return identity
	}
	trunk := resolve("https://svn.example/project/trunk")
	branch := resolve("https://svn.example/project/branches/release")
	if trunk == branch {
		t.Fatalf("same revision on switched repository URLs produced one identity: %s", trunk)
	}
}

func TestP4SnapshotIdentityBindsCompleteClientView(t *testing.T) {
	resolve := func(clientSpec string) string {
		t.Helper()
		runner := &scriptedNativeRunner{outputs: []string{"/repo", "456", clientSpec, ""}}
		resolver, err := resolverForVCS("p4", runner)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := resolver.Resolve("/repo")
		if err != nil {
			t.Fatal(err)
		}
		return identity
	}
	first := resolve("... Client workspace\n... View1 -//depot/project/private/... //workspace/private/...\n... View0 //depot/project/... //workspace/project/...\n... ChangeView0 //depot/project/legacy/...@100")
	second := resolve("... Client workspace\n... View0 //depot/other/... //workspace/project/...")
	if first == second {
		t.Fatalf("same changelist on changed client views produced one identity: %s", first)
	}
	if !strings.Contains(first, `"view":["//depot/project/... //workspace/project/...","-//depot/project/private/... //workspace/private/..."]`) ||
		!strings.Contains(first, `"changeView":["//depot/project/legacy/...@100"]`) {
		t.Fatalf("client view was not encoded canonically: %s", first)
	}
}

func TestNativeVCSResolverRejectsNonUniformWorkspace(t *testing.T) {
	for _, test := range []struct {
		name    string
		outputs []string
		want    string
	}{
		{name: "svn", outputs: []string{"/repo", "2:3"}, want: "not at one uniform revision"},
		{name: "p4", outputs: []string{"/repo", "456", "... Client workspace\n... View0 //depot/project/... //workspace/project/...", "//depot/project/older.txt"}, want: "not uniformly synced"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedNativeRunner{outputs: test.outputs}
			resolver, err := resolverForVCS(test.name, runner)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := resolver.Resolve("/repo"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("non-uniform workspace was accepted: %v", err)
			}
		})
	}
}

func TestNativeVCSResolverRejectsUnsubmittedChanges(t *testing.T) {
	for _, test := range []struct {
		name    string
		pending string
	}{
		{name: "git", pending: " M delivery.txt"},
		{name: "svn", pending: "M       delivery.txt"},
		{name: "p4", pending: "//depot/project/delivery.txt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedNativeRunner{outputs: []string{"/repo", test.pending}}
			resolver, err := resolverForVCS(test.name, runner)
			if err != nil {
				t.Fatal(err)
			}
			if err := resolver.VerifyReady("/repo"); err == nil || !strings.Contains(err.Error(), "unsubmitted "+test.name+" changes") {
				t.Fatalf("pending changes were accepted: %v", err)
			}
		})
	}
}

func TestP4ResolverAcceptsNoOpenedFiles(t *testing.T) {
	runner := &scriptedNativeRunner{outputs: []string{"/repo"}, errors: []error{nil, errors.New("p4: File(s) not opened on this client.")}}
	resolver, err := resolverForVCS("p4", runner)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("standard clean P4 result was rejected: %v", err)
	}
}

func TestNativeVCSResolverPropagatesCommandFailure(t *testing.T) {
	runner := &scriptedNativeRunner{errors: []error{errors.New("not a working copy")}}
	resolver, err := resolverForVCS("svn", runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve("/repo"); err == nil || !strings.Contains(err.Error(), "not a working copy") {
		t.Fatalf("native error was lost: %v", err)
	}
}

type scriptedNativeRunner struct {
	outputs []string
	errors  []error
	calls   [][]string
}

func (r *scriptedNativeRunner) Run(_ string, command string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{command}, args...))
	index := len(r.calls) - 1
	if index < len(r.errors) && r.errors[index] != nil {
		return "", r.errors[index]
	}
	if index >= len(r.outputs) {
		return "", nil
	}
	return r.outputs[index], nil
}
