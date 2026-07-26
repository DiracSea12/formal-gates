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
}

func TestNativeVCSResolverCommandShapes(t *testing.T) {
	gitID := strings.Repeat("a", 40)
	for _, test := range []struct {
		name     string
		identity string
		want     [][]string
	}{
		{name: "git", identity: gitID, want: [][]string{{"git", "rev-parse", "--show-toplevel"}, {"git", "rev-parse", "HEAD"}, {"git", "rev-parse", "--show-toplevel"}, {"git", "rev-parse", "--verify", gitID + "^{commit}"}, {"git", "rev-parse", "--show-toplevel"}, {"git", "status", "--porcelain=v1", "--untracked-files=no"}}},
		{name: "svn", identity: "123", want: [][]string{{"svn", "info", "--show-item", "wc-root", "/repo"}, {"svn", "info", "--show-item", "revision", "/repo"}, {"svn", "info", "--show-item", "wc-root", "/repo"}, {"svn", "info", "--show-item", "revision", "-r", "123", "/repo"}, {"svn", "info", "--show-item", "wc-root", "/repo"}, {"svn", "status", "--quiet", "/repo"}}},
		{name: "p4", identity: "456", want: [][]string{{"p4", "-d", "/repo", "-ztag", "-F", "%clientRoot%", "info"}, {"p4", "-d", "/repo", "-ztag", "-F", "%change%", "changes", "-m", "1", "...#have"}, {"p4", "-d", "/repo", "-ztag", "-F", "%clientRoot%", "info"}, {"p4", "-d", "/repo", "-ztag", "-F", "%change%", "changes", "-m", "1", "...@456"}, {"p4", "-d", "/repo", "-ztag", "-F", "%clientRoot%", "info"}, {"p4", "-d", "/repo", "-ztag", "-F", "%depotFile%", "opened", "..."}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedNativeRunner{outputs: []string{"/repo", test.identity, "/repo", test.identity, "/repo", ""}}
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
