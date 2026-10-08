package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// Native registration must survive the ordinary filesystem spelling of a
// workspace when Git reports its canonical directory spelling.
func TestNativeWorkspaceObservationUsesDirectoryIdentity(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Workspace Fixture", "GIT_AUTHOR_EMAIL=workspace@example.invalid", "GIT_COMMITTER_NAME=Workspace Fixture", "GIT_COMMITTER_EMAIL=workspace@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("native git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.name", "Native Fixture")
	git("config", "user.email", "native@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("native workspace\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "--", "app.txt")
	git("commit", "-qm", "Workspace source")
	base := git("rev-parse", "HEAD")
	workspace := filepath.Join(t.TempDir(), "editing")
	git("worktree", "add", "--detach", workspace, base)
	before := git("worktree", "list", "--porcelain")
	t.Logf("workspace requested=%s canonical-registration=%s", workspace, before)
	want, err := os.Stat(workspace)
	if err != nil {
		t.Fatal(err)
	}
	registered := false
	for _, line := range strings.Split(before, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			actual, err := os.Stat(strings.TrimPrefix(line, "worktree "))
			registered = registered || err == nil && os.SameFile(want, actual)
		}
	}
	if !registered {
		t.Fatal("fixture did not create the actual registered workspace")
	}
	f := &Facade{root: root, vcs: vcs.New(root, vcs.ExecRunner{})}
	found, observeErr := f.observeWorktree(workspace, base)
	if after := git("worktree", "list", "--porcelain"); after != before {
		t.Fatal("observation changed native registration")
	}
	if observeErr != nil || !found {
		t.Fatalf("actual registered workspace was not observed by directory identity: found=%v err=%v", found, observeErr)
	}
}

// Each observation is a real Git prefix of the issued operation. Missing
// inputs are incomplete; a different native ref remains a conflict.
func TestNativeWorkspaceObservationDistinguishesIncompleteAndConflict(t *testing.T) {
	for _, prefix := range []string{"requirement", "ref", "mirror", "conflict"} {
		t.Run(prefix, func(t *testing.T) {
			root := t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Workspace Fixture", "GIT_AUTHOR_EMAIL=workspace@example.invalid", "GIT_COMMITTER_NAME=Workspace Fixture", "GIT_COMMITTER_EMAIL=workspace@example.invalid")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("native git %v: %v %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("init", "-q")
			git("config", "user.name", "Native Fixture")
			git("config", "user.email", "native@example.invalid")
			for path, body := range map[string]string{"app.txt": "native input\n", ".gitignore": ".gates/\n"} {
				if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			git("add", "--", "app.txt", ".gitignore")
			git("commit", "-qm", "Workspace source")
			base := git("rev-parse", "HEAD")
			git("commit", "--allow-empty", "-qm", "Different ref identity")
			other := git("rev-parse", "HEAD")
			path := filepath.Join(t.TempDir(), "editing")
			git("worktree", "add", "--detach", path, base)
			body := "confirmed requirement\n"
			if err := os.MkdirAll(filepath.Join(root, ".gates"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".gates", "requirements.md"), []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
			r := protocol.NativeResource{ID: "workspace", Owner: "run", Role: "blackbox-design", Path: path, Ref: "refs/formal-gates/run/workspace", Source: base, Status: "PLANNED"}
			s := &protocol.State{RunID: "run", NativeResources: map[string]protocol.NativeResource{r.ID: r}, IntakeConfirmationReceipt: &protocol.IntakeConfirmationReceipt{Artifacts: []protocol.IntakeArtifact{{Path: ".gates/requirements.md", Revision: fmt.Sprintf("%x", sha256.Sum256([]byte(body)))}}}}
			params := protocol.WorkspaceParameters{Resource: r}
			if prefix != "requirement" {
				if err := os.MkdirAll(filepath.Join(path, ".gates"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, ".gates", "requirements.md"), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if prefix == "mirror" {
				params.Manifest = &coverage.AcceptanceManifest{}
				git("update-ref", r.Ref, base)
			}
			if prefix == "conflict" {
				git("update-ref", r.Ref, other)
			}
			raw, _ := json.Marshal(params)
			var values map[string]any
			if err := json.Unmarshal(raw, &values); err != nil {
				t.Fatal(err)
			}
			intent := protocol.HostActionIntent{PayloadDigest: "sha256:workspace-input", Adapter: &protocol.AdapterHostAction{Operation: "op.workspace", Params: values}}
			f := &Facade{root: root, vcs: vcs.New(root, vcs.ExecRunner{})}
			before := git("worktree", "list", "--porcelain") + git("for-each-ref", "--format=%(refname) %(objectname)", "refs/formal-gates/")
			_, complete, err := f.nativeEvidence(s, intent, false)
			if complete || (err != nil) != (prefix == "conflict") {
				t.Fatalf("prefix %s: complete=%v err=%v", prefix, complete, err)
			}
			after := git("worktree", "list", "--porcelain") + git("for-each-ref", "--format=%(refname) %(objectname)", "refs/formal-gates/")
			if before != after {
				t.Fatal("observation changed a native prefix")
			}
			if prefix == "requirement" {
				if _, err := os.Stat(filepath.Join(path, ".gates", "requirements.md")); !os.IsNotExist(err) {
					t.Fatal("readonly observation injected a missing requirement")
				}
			}
			if prefix == "mirror" {
				if _, err := os.Stat(filepath.Join(path, ".gates", "cases", "blackbox.md")); !os.IsNotExist(err) {
					t.Fatal("readonly observation wrote a missing mirror")
				}
			}
		})
	}
}

// HEAD identity alone does not prove that a mutable authoring package graph
// describes the immutable source commit used to authorize a removed test.
func TestNativeOwnershipRejectsMutableGraphAsImmutableSource(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Ownership Fixture", "GIT_AUTHOR_EMAIL=ownership@example.invalid", "GIT_COMMITTER_NAME=Ownership Fixture", "GIT_COMMITTER_EMAIL=ownership@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("native git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.name", "Native Fixture")
	git("config", "user.email", "native@example.invalid")
	for name, body := range map[string]string{"go.mod": "module ownership\n\ngo 1.22\n", "app.go": "package app\n", "removed_test.go": "package app\nimport \"testing\"\nfunc TestOld(t *testing.T){}\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "--all")
	git("commit", "-qm", "Immutable graph source")
	base := git("rev-parse", "HEAD")
	authoring := filepath.Join(t.TempDir(), "authoring")
	git("worktree", "add", "--detach", authoring, base)
	if err := os.Remove(filepath.Join(authoring, "removed_test.go")); err != nil {
		t.Fatal(err)
	}
	t.Logf("source=%s authoring HEAD=%s dirty graph status=%s", base, git("-C", authoring, "rev-parse", "HEAD"), git("-C", authoring, "status", "--porcelain"))
	s := &protocol.State{NativeResources: map[string]protocol.NativeResource{"authoring": {ID: "authoring", Owner: "run", Role: "whitebox-authoring", Path: authoring, Source: base, Identity: base, Status: "READY"}}}
	f := &Facade{root: root, vcs: vcs.New(root, vcs.ExecRunner{})}
	owned, err := f.ownershipAt(s, base)
	t.Logf("ownership at exact immutable source=%v err=%v", owned, err)
	if err != nil || !owned["removed_test.go"] {
		t.Fatal("mutable authoring graph was accepted as immutable source ownership and lost the deleted test")
	}
}

// An unchanged freeze can lose its response without producing a commit. Its
// clean native tree and original ref prove completion without another write.
func TestNativeUnchangedFreezeObservedWithoutWrites(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Candidate Fixture", "GIT_AUTHOR_EMAIL=candidate@example.invalid", "GIT_COMMITTER_NAME=Candidate Fixture", "GIT_COMMITTER_EMAIL=candidate@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("native git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.name", "Native Fixture")
	git("config", "user.email", "native@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("unchanged input\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", "--", "input.txt")
	git("commit", "-qm", "Candidate source")
	base := git("rev-parse", "HEAD")
	ref := "refs/formal-gates/run/candidate"
	git("update-ref", ref, base)
	r := protocol.NativeResource{ID: "candidate", Owner: "run", Role: "development", Path: root, Ref: ref, Source: base, Identity: base, Status: "READY"}
	s := &protocol.State{RunID: "run", NativeResources: map[string]protocol.NativeResource{r.ID: r}}
	p := protocol.CandidateParameters{Stage: "DEVELOPMENT", WorkspaceID: r.ID, Base: base, Paths: []string{"input.txt"}}
	params, err := evidenceFields(p)
	if err != nil {
		t.Fatal(err)
	}
	intent := protocol.HostActionIntent{ActionID: "freeze", PayloadDigest: "sha256:freeze-input", Adapter: &protocol.AdapterHostAction{Operation: "op.candidate", Params: params}}
	f := &Facade{root: root, vcs: vcs.New(root, vcs.ExecRunner{})}
	before := git("count-objects", "-v") + git("write-tree") + git("show-ref", ref)
	result, complete, err := f.nativeEvidence(s, intent, false)
	if err != nil || !complete {
		t.Fatalf("unchanged native freeze was not observed: complete=%v err=%v", complete, err)
	}
	var proof protocol.CandidateEvidence
	raw, err := json.Marshal(result.Candidate)
	if err != nil || json.Unmarshal(raw, &proof) != nil || proof.ParametersDigest != intent.PayloadDigest || proof.Identity != base || proof.Digest != git("rev-parse", base+"^{tree}") || proof.WorkspaceID != r.ID || len(proof.Changes) != 0 {
		t.Fatalf("unchanged candidate proof is not exact: %+v err=%v", proof, err)
	}
	if before != git("count-objects", "-v")+git("write-tree")+git("show-ref", ref) {
		t.Fatal("readonly no-op reconciliation wrote native objects or refs")
	}
}

// Native existence and ownership are independent: embedding an existing
// runtime input in a test cannot authorize changing it as a QA artifact.
func TestWhiteboxOwnershipRequiresEveryExistingEndpoint(t *testing.T) {
	for _, scenario := range []string{"runtime-input", "production-input", "new-fixture", "removed-fixture", "test-mode"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			git := func(dir string, args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Ownership Fixture", "GIT_AUTHOR_EMAIL=ownership@example.invalid", "GIT_COMMITTER_NAME=Ownership Fixture", "GIT_COMMITTER_EMAIL=ownership@example.invalid")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			write := func(dir, name, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			git(root, "init", "-q")
			git(root, "config", "user.name", "Ownership Fixture")
			git(root, "config", "user.email", "ownership@example.invalid")
			write(root, "go.mod", "module ownership\n\ngo 1.22\n")
			write(root, "app.go", "package app\nimport \"os\"\nfunc Enabled() bool { b,_:=os.ReadFile(\"config.json\");return string(b)==\"enabled\" }\n")
			write(root, "config.json", "enabled")
			write(root, "old_test.go", "package app\nimport (_ \"embed\";\"testing\")\n//go:embed old.fixture\nvar old string\nfunc TestOld(t *testing.T) { if old==\"\" {t.Fatal(old)} }\n")
			write(root, "old.fixture", "old input")
			git(root, "add", "--all")
			git(root, "commit", "-qm", "Ownership inputs")
			base := git(root, "rev-parse", "HEAD")
			path := filepath.Join(t.TempDir(), "authoring")
			git(root, "worktree", "add", "--detach", path, base)
			paths := []string{}
			files := []string{}
			switch scenario {
			case "runtime-input":
				write(path, "config.json", "disabled")
				write(path, "config_fixture_test.go", "package app\nimport (_ \"embed\";\"testing\")\n//go:embed config.json\nvar data string\nfunc TestConfiguration(t *testing.T) { if data==\"\" {t.Fatal(data)} }\n")
				paths = []string{"config.json", "config_fixture_test.go"}
			case "production-input":
				write(path, "app.go", "package app\nfunc Enabled() bool { return false }\n")
				paths = []string{"app.go"}
				files = []string{"app.go"}
			case "new-fixture":
				write(path, "new.fixture", "new input")
				write(path, "new_fixture_test.go", "package app\nimport (_ \"embed\";\"testing\")\n//go:embed new.fixture\nvar data string\nfunc TestNewInput(t *testing.T) { if data==\"\" {t.Fatal(data)} }\n")
				paths = []string{"new.fixture", "new_fixture_test.go"}
			case "removed-fixture":
				for _, name := range []string{"old.fixture", "old_test.go"} {
					if err := os.Remove(filepath.Join(path, name)); err != nil {
						t.Fatal(err)
					}
				}
				paths = []string{"old.fixture", "old_test.go"}
			case "test-mode":
				if err := os.Chmod(filepath.Join(path, "old_test.go"), 0755); err != nil {
					t.Fatal(err)
				}
				paths = []string{"old_test.go"}
			}
			r := protocol.NativeResource{ID: "authoring", Owner: "ownership", Role: "whitebox-authoring", Path: path, Ref: "refs/formal-gates/ownership/authoring", Source: base, Identity: base, Status: "READY"}
			git(root, "update-ref", r.Ref, base)
			s := &protocol.State{RunID: "ownership", NativeResources: map[string]protocol.NativeResource{r.ID: r}}
			params, err := evidenceFields(protocol.CandidateParameters{Stage: "QA_ARTIFACT", WorkspaceID: r.ID, Base: base, Paths: paths, TestFiles: files})
			if err != nil {
				t.Fatal(err)
			}
			intent := protocol.HostActionIntent{ActionID: "artifact", PayloadDigest: "sha256:artifact", Adapter: &protocol.AdapterHostAction{Operation: "op.candidate", Params: params}}
			f := &Facade{root: root, vcs: vcs.New(root, nil), admission: *testAdmission(t, root, "ownership", "sha256:package", DefaultProvider)}
			before := git(path, "write-tree") + git(path, "rev-parse", "HEAD") + git(root, "show-ref", r.Ref)
			result, complete, err := f.candidateEvidence(s, intent, true, new(bool))
			if scenario == "runtime-input" || scenario == "production-input" {
				if err == nil || complete || result.Candidate != nil {
					t.Fatalf("existing UNKNOWN input gained ownership: %+v complete=%v err=%v", result, complete, err)
				}
				if after := git(path, "write-tree") + git(path, "rev-parse", "HEAD") + git(root, "show-ref", r.Ref); after != before {
					t.Fatal("ownership rejection staged or committed an input")
				}
				if body, err := os.ReadFile(filepath.Join(path, "config.json")); err != nil || scenario == "runtime-input" && string(body) != "disabled" {
					t.Fatal("rejection changed authoring bytes")
				}
				return
			}
			if err != nil || !complete || result.Candidate == nil || result.Candidate.TestOnly || result.Candidate.ProductionViewEqual || result.Candidate.GateView != nil {
				t.Fatalf("native test transition lost proof: %+v complete=%v err=%v", result, complete, err)
			}
			want, err := f.vcs.At(path).Changes(base, result.Candidate.Identity)
			if err != nil || len(want) != len(paths) || !reflect.DeepEqual(result.Candidate.Changes, want) || result.Candidate.Digest != git(path, "rev-parse", "HEAD^{tree}") {
				t.Fatalf("artifact proof lacks complete native content: %+v %v", result.Candidate, err)
			}
			if scenario == "test-mode" && (want[0].BaseMode != "100644" || want[0].Mode != "100755" || want[0].BaseObject != want[0].Object) {
				t.Fatalf("mode transition was not preserved: %+v", want)
			}
			before = git(path, "rev-parse", "HEAD") + git(root, "show-ref", r.Ref)
			recovered, observed, err := f.candidateEvidence(s, intent, false, new(bool))
			if err != nil || !observed || !reflect.DeepEqual(recovered, result) || before != git(path, "rev-parse", "HEAD")+git(root, "show-ref", r.Ref) {
				t.Fatalf("normal and lost-response proof differ: %+v %v", recovered, err)
			}
		})
	}
}

// A failed native call needs a positive effect observation to keep its writer.
// An unavailable observation is UNKNOWN; a proven missing effect is FAILED.
type nativeFailureObservationRunner struct {
	inner       vcs.Runner
	message     string
	unavailable bool
	failed      bool
	commits     int
}

func (r *nativeFailureObservationRunner) Run(dir, command string, args ...string) (string, error) {
	if command == "git" && len(args) == 3 && args[0] == "commit" && args[1] == "-m" && args[2] == r.message {
		r.commits++
		r.failed = true
		return "", fmt.Errorf("fixture commit failed before its native effect")
	}
	if r.failed && r.unavailable {
		return "", fmt.Errorf("fixture native observation unavailable")
	}
	return r.inner.Run(dir, command, args...)
}

func (r *nativeFailureObservationRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	if r.failed && r.unavailable {
		return nil, fmt.Errorf("fixture native observation unavailable")
	}
	return r.inner.RunRaw(dir, command, input, args...)
}

func TestNativeFailureClassificationRequiresPositiveObservation(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("unavailable=%t", unavailable), func(t *testing.T) {
			root := t.TempDir()
			identities := snapshotRepoCommits(t, root, "implementation", "verified candidate")
			d := completePromotionPipeline(t, nil, root, identities[0])
			runner := &nativeFailureObservationRunner{inner: vcs.ExecRunner{}, message: "bound organization", unavailable: unavailable}
			prepareApprovedPromotionPrefix(t, d, runner.message)
			d.f.vcsRunner = runner
			d.f.vcs = vcs.New(root, runner)
			if _, err := d.f.PrepareCandidate(runner.message); err == nil || !runner.failed || runner.commits != 1 {
				t.Fatalf("native failure did not reach the exact commit input: %v", err)
			}
			state := driverState(t, d)
			intent, pending := state.PromotionIntent()
			if !pending || state.NativeActionWritable(intent, d.f.definition) {
				t.Fatal("failed or UNKNOWN responsibility exposed a writer")
			}
			if unavailable {
				if state.HostActionReceipts[intent.ActionID].Status != protocol.HostActionStatusUnknown || len(state.HostActionFailures[intent.ActionID]) != 0 {
					t.Fatal("unavailable observation was declared a failed effect")
				}
			} else {
				failures := state.HostActionFailures[intent.ActionID]
				if len(failures) != 1 || failures[0].Status != protocol.HostActionStatusFailed || failures[0].FailureClass != "USER_ACTION_REQUIRED" {
					t.Fatalf("proven missing effect has no original failure fact: %+v", failures)
				}
			}
			d.reopen()
			before, err := d.f.observePromotion()
			if err != nil || before.Head != identities[0] || before.Clean || before.IndexTree != state.Validation.Candidate.Digest {
				t.Fatalf("failed native call changed its expected prefix: %+v %v", before, err)
			}
			if _, err := d.f.PrepareCandidate(runner.message); err == nil {
				t.Fatal("failed or UNKNOWN reentry executed without authorization")
			}
			_, driveErr := d.f.Drive()
			want := "native responsibility has no remaining execution allowance"
			if unavailable {
				want = "candidate promotion remains incomplete"
			}
			if driveErr == nil || !strings.Contains(driveErr.Error(), want) {
				t.Fatalf("blocked native drive: %v", driveErr)
			}
			after, err := d.f.observePromotion()
			if err != nil || !reflect.DeepEqual(before, after) || driverState(t, d).DeliveryComplete(d.f.definition) {
				t.Fatalf("failed reentry changed native bytes or admitted delivery: %+v %v", after, err)
			}
		})
	}
}

// A candidate cannot freeze a workspace with an undeclared verification artifact.
func TestNativeCandidateRejectsUndeclaredChangesWithoutWrites(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Candidate Fixture", "GIT_AUTHOR_EMAIL=candidate@example.invalid", "GIT_COMMITTER_NAME=Candidate Fixture", "GIT_COMMITTER_EMAIL=candidate@example.invalid")
		body, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, body)
		}
		return strings.TrimSpace(string(body))
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("command.go", "package main\nfunc main() {}\n")
	git("add", "command.go")
	git("commit", "-qm", "Candidate input")
	base := git("rev-parse", "HEAD")
	write("command.go", "package main\nfunc main() { println(\"verified\") }\n")
	write("verification-output", "temporary verification executable")
	r := protocol.NativeResource{ID: "candidate", Role: "development", Path: root, Source: base, Identity: base, Status: "READY"}
	state := &protocol.State{RunID: "candidate-run", NativeResources: map[string]protocol.NativeResource{r.ID: r}}
	params, err := evidenceFields(protocol.CandidateParameters{Stage: "DEVELOPMENT", WorkspaceID: r.ID, Base: base, Paths: []string{"command.go"}})
	if err != nil {
		t.Fatal(err)
	}
	intent := protocol.HostActionIntent{ActionID: "freeze", PayloadDigest: "candidate-input", Adapter: &protocol.AdapterHostAction{Operation: "op.candidate", Params: params}}
	f := &Facade{root: root, vcs: vcs.New(root, nil)}
	before := git("status", "--porcelain=v1", "--untracked-files=all") + git("write-tree") + git("rev-parse", "HEAD")
	for range 2 {
		invoked := false
		result, complete, refusal := f.candidateEvidence(state, intent, true, &invoked)
		_, _, shared := f.vcs.ValidateDeclaredChanges(base, []string{"command.go"})
		if refusal == nil || shared == nil || refusal.Error() != shared.Error() || refusal.Error() != "candidate contains undeclared change: verification-output" || invoked || complete || result.Candidate != nil {
			t.Fatalf("candidate admission wrote or changed refusal: %v %+v complete=%v invoked=%v", refusal, result, complete, invoked)
		}
		if now := git("status", "--porcelain=v1", "--untracked-files=all") + git("write-tree") + git("rev-parse", "HEAD"); now != before {
			t.Fatal("candidate rejection changed workspace")
		}
	}
	if body, err := os.ReadFile(filepath.Join(root, "verification-output")); err != nil || string(body) != "temporary verification executable" {
		t.Fatal("candidate removed or changed undeclared bytes")
	}
}
