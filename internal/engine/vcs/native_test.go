package vcs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func nativeFixtureDirectory(t *testing.T) string {
	t.Helper()
	if evidence := os.Getenv("NATIVE_VCS_EVIDENCE"); evidence != "" {
		path, err := os.MkdirTemp(evidence, strings.ReplaceAll(t.Name(), "/", "-")+"-")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("native fixture: %s", path)
		return path
	}
	return t.TempDir()
}

type nativeTestRunner struct {
	port, user, client string
	log                *os.File
	lost               string
	didLose            bool
}

func (r *nativeTestRunner) Run(dir, command string, args ...string) (string, error) {
	body, err := r.RunRaw(dir, command, nil, args...)
	return strings.TrimSpace(string(body)), err
}
func (r *nativeTestRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	if command == "p4" {
		prefix := []string{"-p", r.port, "-u", r.user}
		hasClient := false
		for _, arg := range args {
			hasClient = hasClient || arg == "-c"
		}
		if !hasClient && r.client != "" {
			prefix = append(prefix, "-c", r.client)
		}
		args = append(prefix, args...)
	}
	start := time.Now()
	body, err := (ExecRunner{}).RunRaw(dir, command, input, args...)
	if r.log != nil {
		fmt.Fprintf(r.log, "%s %q stdin=%q elapsed=%s error=%v\nstdout=%q\n", command, args, input, time.Since(start), err, body)
	}
	if err == nil && r.lost != "" && !r.didLose {
		for _, arg := range args {
			if arg == r.lost {
				r.didLose = true
				return nil, fmt.Errorf("fixture lost the completed native response")
			}
		}
	}
	return body, err
}
func nativeCommand(t *testing.T, r Runner, dir, command string, args ...string) string {
	t.Helper()
	out, err := r.Run(dir, command, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func nativeFixtureLog(t *testing.T) *os.File {
	t.Helper()
	directory := os.Getenv("NATIVE_VCS_EVIDENCE")
	if directory == "" {
		directory = nativeFixtureDirectory(t)
	}
	file, err := os.Create(filepath.Join(directory, strings.ReplaceAll(t.Name(), "/", "-")+".native-commands.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native commands: %s", file.Name())
	t.Cleanup(func() { file.Close() })
	return file
}
func svnFixture(t *testing.T) (*Provider, *nativeTestRunner) {
	t.Helper()
	for _, tool := range []string{"svn", "svnadmin"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s unavailable", tool)
		}
	}
	root := nativeFixtureDirectory(t)
	repo := filepath.Join(root, "repository")
	wc := filepath.Join(root, "main")
	runner := &nativeTestRunner{log: nativeFixtureLog(t)}
	nativeCommand(t, runner, root, "svnadmin", "create", repo)
	url := "file://" + repo
	nativeCommand(t, runner, root, "svn", "mkdir", url+"/trunk", url+"/branches", "-m", "fixture roots")
	nativeCommand(t, runner, root, "svn", "checkout", url+"/trunk", wc)
	if err := os.MkdirAll(filepath.Join(wc, "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wc, "body.txt"), []byte(" leading\ntrailing\n\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("body.txt", filepath.Join(wc, "link")); err != nil {
		t.Fatal(err)
	}
	nativeCommand(t, runner, wc, "svn", "add", "body.txt", "empty", "link")
	nativeCommand(t, runner, wc, "svn", "propset", "svn:executable", "*", "body.txt")
	property := filepath.Join(root, "property")
	if err := os.WriteFile(property, []byte{' ', 0, 1, '\n'}, 0644); err != nil {
		t.Fatal(err)
	}
	nativeCommand(t, runner, wc, "svn", "propset", "custom:binary", "-F", property, ".")
	nativeCommand(t, runner, wc, "svn", "propset", "custom:directory", " directory \n", "empty")
	nativeCommand(t, runner, wc, "svn", "commit", "-m", "fixture values")
	nativeCommand(t, runner, wc, "svn", "update")
	return NewNative(wc, "svn", runner), runner
}
func TestSVNImmutableValuesWorkspacePromotionCleanup(t *testing.T) {
	t.Parallel()
	p, _ := svnFixture(t)
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := p.TreeEntries(base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entries["."], "custom:binary") || !strings.Contains(entries["empty"], "custom:directory") || !strings.Contains(entries["body.txt"], "IGxlYWRpbmcKdHJhaWxpbmcKCg==") {
		t.Fatalf("native values omitted: %v", entries)
	}
	workspace := filepath.Join(nativeFixtureDirectory(t), "candidate")
	handle := PlanWorkspace("svn", base, "owned", "candidate")
	if err = p.CreateWorkspace(workspace, base, "bound copy", handle); err != nil {
		t.Fatal(err)
	}
	candidate := p.At(workspace, handle)
	created, err := candidate.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if created == base {
		t.Fatal("URL copy must keep its actual native identity")
	}
	a, _ := p.TreeDigest(base)
	b, _ := candidate.TreeDigest(created)
	if a != b {
		t.Fatal("native copy changed complete values")
	}
	if err = candidate.IsAncestorOrEqual(base, created); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(workspace, "body.txt"), []byte(" changed \n\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(workspace, "binary"), []byte{0, 1, 2, 255}, 0644); err != nil {
		t.Fatal(err)
	}
	if err = candidate.Track([]string{"body.txt", "binary"}); err != nil {
		t.Fatal(err)
	}
	if err = candidate.Commit("bound candidate"); err != nil {
		t.Fatal(err)
	}
	identity, err := candidate.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err = candidate.VerifyReady(); err != nil {
		t.Fatal(err)
	}
	changes, err := p.Changes(base, identity)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	if err = p.ApplyArtifact(identity, paths); err != nil {
		t.Fatal(err)
	}
	pending, err := p.Pending(base, identity)
	if err != nil {
		t.Fatal(err)
	}
	if !pending.Equal {
		t.Fatal("promotion did not preserve native values")
	}
	if err = p.Commit("bound promotion"); err != nil {
		t.Fatal(err)
	}
	final, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	q, _ := p.TreeDigest(identity)
	f, _ := p.TreeDigest(final)
	if q != f {
		t.Fatal("candidate/final values differ")
	}
	if err = p.CleanupWorkspace(workspace, "bound cleanup", handle); err != nil {
		t.Fatal(err)
	}
	absent, err := p.WorkspaceAbsent(workspace, handle)
	if err != nil || !absent {
		t.Fatalf("native resources remain: %v", err)
	}
	again, err := p.TreeDigest(identity)
	if err != nil || again != q {
		t.Fatal("cleanup destroyed immutable evidence")
	}
}
func TestNativeRawRunnerPreservesBytesAndStdin(t *testing.T) {
	t.Parallel()
	input := []byte(" leading\n\n")
	body, err := (ExecRunner{}).RunRaw(nativeFixtureDirectory(t), "cat", input)
	if err != nil || !bytes.Equal(body, input) {
		t.Fatalf("raw bytes changed: %q %v", body, err)
	}
}
func p4Fixture(t *testing.T) (*Provider, *nativeTestRunner) {
	t.Helper()
	for _, tool := range []string{"p4", "p4d"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s unavailable", tool)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().String()
	listener.Close()
	root := nativeFixtureDirectory(t)
	serverRoot := filepath.Join(root, "server")
	if err = os.MkdirAll(serverRoot, 0755); err != nil {
		t.Fatal(err)
	}
	log := nativeFixtureLog(t)
	server := exec.Command("p4d", "-r", serverRoot, "-p", port)
	server.Stdout, server.Stderr = log, log
	if err = server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	runner := &nativeTestRunner{port: port, user: "fixture", client: "main", log: log}
	ready := false
	for i := 0; i < 50; i++ {
		if _, err = runner.Run(root, "p4", "info"); err == nil {
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		body, readErr := os.ReadFile(log.Name())
		t.Fatalf("private p4d did not start: address=%s pid=%d probe=%v log=%q readError=%v\n%s", port, server.Process.Pid, err, log.Name(), readErr, body)
	}
	wc := filepath.Join(root, "main")
	if err = os.MkdirAll(wc, 0755); err != nil {
		t.Fatal(err)
	}
	spec := "Client: main\nOwner: fixture\nRoot: " + wc + "\nLineEnd: local\nView:\n\t//depot/main/... //main/...\n"
	if _, err = runner.RunRaw(wc, "p4", []byte(spec), "client", "-i"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(wc, "body.txt"), []byte(" leading\ntrailing\n\n"), 0755); err != nil {
		t.Fatal(err)
	}
	nativeCommand(t, runner, wc, "p4", "add", "-t", "text+x", "body.txt")
	nativeCommand(t, runner, wc, "p4", "submit", "-d", "fixture values")
	return NewNative(wc, "p4", runner).At(wc, WorkspaceHandle{Client: "main"}), runner
}
func TestP4ImmutableFiletypeWorkspacePromotionCleanup(t *testing.T) {
	t.Parallel()
	p, runner := p4Fixture(t)
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(nativeFixtureDirectory(t), "candidate")
	handle := PlanWorkspace("p4", base, "owned", "candidate")
	if err = p.CreateWorkspace(workspace, base, "bound populate", handle); err != nil {
		t.Fatal(err)
	}
	candidate := p.At(workspace, handle)
	created, err := candidate.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := p.TreeDigest(base)
	b, _ := candidate.TreeDigest(created)
	if a != b {
		t.Fatalf("P4 copy values changed: %s %s", a, b)
	}
	if err = os.WriteFile(filepath.Join(workspace, "body.txt"), []byte{0, 1, 255, '\n'}, 0755); err != nil {
		t.Fatal(err)
	}
	if err = candidate.Track([]string{"body.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err = candidate.p4Raw(nil, "reopen", "-t", "binary+x", filepath.Join(workspace, "body.txt")); err != nil {
		t.Fatal(err)
	}
	if err = candidate.Commit("bound candidate"); err != nil {
		t.Fatal(err)
	}
	identity, err := candidate.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := candidate.TreeEntries(identity)
	if err != nil {
		t.Fatal(err)
	}
	var value nativeEntry
	if err := json.Unmarshal([]byte(entries["body.txt"]), &value); err != nil || value.Filetype != "binary+x" || !bytes.Equal(value.Content, []byte{0, 1, 255, '\n'}) {
		t.Fatalf("immutable binary bytes/filetype differ: %+v %v", value, err)
	}
	if err = p.ApplyArtifact(identity, []string{"body.txt"}); err != nil {
		t.Fatal(err)
	}
	pending, err := p.Pending(base, identity)
	if err != nil {
		t.Fatal(err)
	}
	if !pending.Equal {
		t.Fatalf("P4 pending values differ from candidate: %+v", pending)
	}
	if err = p.Commit("bound promotion"); err != nil {
		t.Fatal(err)
	}
	final, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	q, _ := p.TreeDigest(identity)
	f, _ := p.TreeDigest(final)
	if q != f {
		t.Fatal("P4 final values differ")
	}
	if err = p.CleanupWorkspace(workspace, "bound cleanup", handle); err != nil {
		t.Fatal(err)
	}
	absent, err := p.WorkspaceAbsent(workspace, handle)
	if err != nil || !absent {
		t.Fatalf("P4 resource remains %v", err)
	}
	after, err := p.TreeDigest(identity)
	if err != nil || after != q {
		t.Fatal("P4 cleanup destroyed candidate reproduction")
	}
	if runner.didLose {
		t.Fatal("unexpected response loss")
	}
}

// A successful root-property commit moves the root while untouched WC nodes
// retain their base revisions until the exact submitted operation is updated.
func TestSVNObservedCommitRequiresWorkingCopySynchronization(t *testing.T) {
	t.Parallel()
	p, runner := svnFixture(t)
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	nativeCommand(t, runner, p.root, "svn", "propset", "custom:delivery", "changed", p.root)
	message := "bound interrupted delivery"
	runner.lost = message
	if err := p.Commit(message); err == nil || !runner.didLose {
		t.Fatal("actual SVN commit response-loss window not reached")
	}
	runner.lost = ""
	observed, found, err := p.ObservedCommit(base, message)
	if err != nil || !found {
		t.Fatalf("native operation absent: %s %t %v", observed, found, err)
	}
	head, err := p.Resolve()
	if err != nil || head != observed {
		t.Fatal("working-copy root did not move to the actual commit")
	}
	status, err := p.Status()
	if err != nil || status != "mixed-revision working copy" {
		t.Fatalf("actual interrupted prefix is not the expected mixed revision: %s %v", status, err)
	}
	pending, err := p.Pending(base, observed)
	if err != nil || !pending.Clean || !pending.Equal {
		t.Fatalf("native operation has independent business changes: %+v %v", pending, err)
	}
	t.Logf("actual commit exists, root equals observed identity, business pending clean/equal, strict readiness rejects: %v", p.VerifyReady())
	// Another repository commit cannot move this continuation to HEAD.
	other := filepath.Join(nativeFixtureDirectory(t), "other")
	nativeCommand(t, runner, "", "svn", "checkout", mustNativeScope(t, observed), other)
	nativeCommand(t, runner, other, "svn", "propset", "custom:later", "unrelated", other)
	nativeCommand(t, runner, other, "svn", "commit", "-m", "unrelated later operation", other)
	if err := p.Synchronize(observed); err != nil {
		t.Fatal(err)
	}
	if err := p.VerifyReady(); err != nil {
		t.Fatal(err)
	}
	after, err := p.Resolve()
	if err != nil || after != observed {
		t.Fatal("continuation updated beyond its exact operation")
	}
	count, err := p.CommitCount(base, observed)
	if err != nil || count != 1 {
		t.Fatal("continuation submitted another operation")
	}
}

func mustNativeScope(t *testing.T, identity string) string {
	t.Helper()
	value, err := decodeNative(identity)
	if err != nil {
		t.Fatal(err)
	}
	return value.Scope
}
