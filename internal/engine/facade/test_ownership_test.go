package facade

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

func TestReferencedPythonArtifactsPreserveProductionAndImmutableProof(t *testing.T) {
	for _, variant := range []string{"case", "shared", "production", "unknown", "unassociated", "removed", "renamed", "mode", "unknown-receipt"} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			git := func(dir string, args ...string) string {
				t.Helper()
				c := exec.Command("git", args...)
				c.Dir = dir
				c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=External Project", "GIT_AUTHOR_EMAIL=external@example.invalid", "GIT_COMMITTER_NAME=External Project", "GIT_COMMITTER_EMAIL=external@example.invalid")
				b, e := c.CombinedOutput()
				if e != nil {
					t.Fatalf("git %v: %v %s", args, e, b)
				}
				return strings.TrimSpace(string(b))
			}
			git(root, "init", "-q")
			write := func(dir, file, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write(root, "app.py", "def valid(value):\n    return bool(value)\n")
			write(root, "runtime.json", "enabled")
			git(root, "add", "--all")
			git(root, "commit", "-qm", "External source")
			base := git(root, "rev-parse", "HEAD")
			path := filepath.Join(t.TempDir(), "authoring")
			git(root, "worktree", "add", "--detach", path, base)
			write(path, "test_app.py", "from app import valid\ndef test_valid():\n    assert valid('ok')\n")
			r := protocol.NativeResource{ID: "authoring", Owner: "external", Role: "whitebox-authoring", Path: path, Ref: "refs/formal-gates/external/authoring", Source: base, Identity: base, Status: "READY"}
			git(root, "update-ref", r.Ref, base)
			s := &protocol.State{RunID: "external", NativeResources: map[string]protocol.NativeResource{r.ID: r}, HostActionReceipts: map[string]protocol.HostActionReceipt{}}
			f := &Facade{root: root, vcs: vcs.New(root, nil), admission: *testAdmission(t, root, "external", "sha256:package", DefaultProvider)}
			p := protocol.CandidateParameters{Stage: "QA_ARTIFACT", WorkspaceID: r.ID, Base: base, Paths: []string{"test_app.py"}, ReviewCases: map[string]string{"valid": "test_app.py::test_valid"}}
			freeze := func(action string) (nativeResult, bool, error) {
				t.Helper()
				values, err := evidenceFields(p)
				if err != nil {
					t.Fatal(err)
				}
				intent := protocol.HostActionIntent{ActionID: action, PayloadDigest: "sha256:" + action, Adapter: &protocol.AdapterHostAction{Operation: "op.candidate", Params: values}}
				return f.candidateEvidence(s, intent, true, new(bool))
			}
			if variant == "removed" || variant == "renamed" || variant == "mode" || variant == "unknown-receipt" {
				proof, complete, err := freeze("initial")
				if err != nil || !complete {
					t.Fatalf("normal Python test rejected: %v", err)
				}
				evidence := proof.Candidate
				values, err := evidenceFields(evidence)
				if err != nil {
					t.Fatal(err)
				}
				status := protocol.HostActionStatusExecuted
				if variant == "unknown-receipt" {
					status = protocol.HostActionStatusUnknown
				}
				s.HostActionReceipts["initial"] = protocol.HostActionReceipt{ActionID: "initial", AdapterOperation: "op.candidate", PayloadDigest: "sha256:initial", Status: status, AdapterEvidence: &protocol.AdapterEvidence{Values: values}}
				p.Base = evidence.Identity
				r.Source, r.Identity = evidence.Identity, evidence.Identity
				s.NativeResources[r.ID] = r
				switch variant {
				case "removed":
					if err := os.Remove(filepath.Join(path, "test_app.py")); err != nil {
						t.Fatal(err)
					}
					p.ReviewCases = nil
				case "renamed":
					if err := os.Rename(filepath.Join(path, "test_app.py"), filepath.Join(path, "verified.py")); err != nil {
						t.Fatal(err)
					}
					p.Paths = []string{"test_app.py", "verified.py"}
					p.ReviewCases = map[string]string{"valid": "verified.py::test_valid"}
				default:
					if err := os.Chmod(filepath.Join(path, "test_app.py"), 0755); err != nil {
						t.Fatal(err)
					}
				}
			}
			if variant == "shared" {
				write(path, "fixture.data", "input")
				p.Paths = append(p.Paths, "fixture.data")
				p.TestFiles = []string{"fixture.data"}
			}
			if variant == "production" {
				write(path, "app.py", "def valid(value):\n    return False\n")
				p.Paths = append(p.Paths, "app.py")
				p.TestFiles = []string{"app.py"}
			}
			if variant == "unknown" {
				write(path, "runtime.json", "disabled")
				p.Paths = append(p.Paths, "runtime.json")
				p.TestFiles = []string{"runtime.json"}
			}
			if variant == "unassociated" {
				write(path, "helper.data", "extra")
				p.Paths = append(p.Paths, "helper.data")
			}
			before := git(path, "write-tree") + git(path, "rev-parse", "HEAD") + git(root, "show-ref", r.Ref)
			proof, complete, err := freeze("artifact")
			rejected := variant == "production" || variant == "unknown" || variant == "unassociated" || variant == "unknown-receipt"
			if rejected {
				if err == nil || complete || proof.Candidate != nil {
					t.Fatalf("unproved endpoint gained ownership: complete=%v err=%v", complete, err)
				}
				if now := git(path, "write-tree") + git(path, "rev-parse", "HEAD") + git(root, "show-ref", r.Ref); now != before {
					t.Fatal("refusal changed native source/index/ref")
				}
				return
			}
			if err != nil || !complete || proof.Candidate == nil || !proof.Candidate.TestOnly || !proof.Candidate.ProductionViewEqual {
				t.Fatalf("approved external test transition rejected: complete=%v err=%v", complete, err)
			}
			actual, err := f.vcs.At(path).Changes(p.Base, proof.Candidate.Identity)
			if err != nil || !reflect.DeepEqual(proof.Candidate.Changes, actual) || len(actual) != len(p.Paths) {
				t.Fatalf("native mode/blob transition not retained: %+v %v", proof.Candidate, err)
			}
			original, err := f.vcs.Entry(base, "app.py")
			if err != nil {
				t.Fatal(err)
			}
			production, err := f.vcs.Entry(proof.Candidate.Identity, "app.py")
			if err != nil || production != original {
				t.Fatal("production mode/blob changed", err)
			}
		})
	}
}
