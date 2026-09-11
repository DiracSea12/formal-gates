package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const b1Document = "# Task\n\n## 需求点\n\n### REQ-001：显示结果\n\n#### 要求\n命令输出字符串 hello。\n\n#### 验收条件\n- AC-001：运行后输出恰好是 hello。\n\n#### 来源\n用户要求。\n"

func newRegularFacade(t *testing.T) *Facade {
	t.Helper()
	return newRegularFacadeDocument(t, b1Document)
}

func newRegularFacadeDocument(t *testing.T, document string, owner ...string) *Facade {
	t.Helper()
	return newRegularFacadePackage(t, document, "../../..", owner...)
}

func newRegularFacadePackage(t *testing.T, document, packagePath string, owner ...string) *Facade {
	t.Helper()
	root := regularFixtureRoot(t)
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "b1@example.invalid"}, {"config", "user.name", "B1 fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "requirements.md"}, {"-c", "core.hooksPath=/dev/null", "commit", "-qm", "Fixture requirement"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	if len(owner) > 0 {
		if err := os.MkdirAll(filepath.Join(root, ".gates"), 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(map[string]string{"provider": "codex", "transcript": owner[0], "session": "owner-test"})
		if err := os.WriteFile(filepath.Join(root, ".gates", "start-owner.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Protocol fixtures bind an existing executable without invoking a model.
	if os.Getenv("FORMAL_GATES_REAL_CODEX") != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, ".gates"), 0700); err != nil {
			t.Fatal(err)
		}
		if fixture := os.Getenv("FORMAL_GATES_TEST_CODEX_EXECUTABLE"); fixture != "" {
			executable = fixture
		}
		data, err := json.Marshal(map[string]string{"codexExecutable": executable})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	packageRoot, err := filepath.Abs(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	f, run, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, Request: StartRequest{RunID: "regular", RequirementPath: "requirements.md"}, Admission: &Admission{PackageDigest: "sha256:test", InstalledTargetIdentity: "test-target"}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Regular == nil || run.Regular.Requirement.Confirmed || run.Route != "" {
		t.Fatal("not an unbound intake")
	}
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	return f
}
func facadeChoice(t *testing.T, f *Facade, q protocol.RegularRequest) {
	t.Helper()
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	ev, _ := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("request-%d", r.Revision)), protocol.RegularEvent{Operation: "REQUEST", ExpectedRevision: r.Revision, Request: &q})
	_, r, err = f.Submit(ev)
	if err != nil {
		t.Fatal(err)
	}
	a := r.Regular.Ask
	ev, _ = protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("decision-%d", r.Revision)), protocol.RegularEvent{Operation: "DECIDE", ExpectedRevision: r.Revision, Decision: &protocol.RegularDecision{RequestID: a.ID, Token: a.Token, Digest: a.Digest, Choice: "confirm"}})
	if _, _, err = f.Submit(ev); err != nil {
		t.Fatal(err)
	}
}
func readyRegularFacade(t *testing.T) *Facade {
	t.Helper()
	f := newRegularFacade(t)
	facadeChoice(t, f, protocol.RegularRequest{Action: "confirm-requirement"})
	facadeChoice(t, f, protocol.RegularRequest{Action: "mode", Mode: "regular"})
	return f
}
func TestRegularFacadeFreshnessAndLightweightChoice(t *testing.T) {
	f := newRegularFacade(t)
	if err := os.WriteFile(filepath.Join(f.root, "requirements.md"), []byte(b1Document+"\nChanged."), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Drive(); err == nil || !strings.Contains(err.Error(), "REQUIREMENT_CHANGED") {
		t.Fatalf("drift=%v", err)
	}
	os.WriteFile(filepath.Join(f.root, "requirements.md"), []byte(b1Document), 0600)
	facadeChoice(t, f, protocol.RegularRequest{Action: "confirm-requirement"})
	facadeChoice(t, f, protocol.RegularRequest{Action: "mode", Mode: "lightweight"})
	r, err := f.Drive()
	if err != nil || r.Status != "COMPLETE" || !r.Unverified || r.Regular.Requirement.Path != "requirements.md" {
		t.Fatalf("lightweight %+v %v", r, err)
	}
	open, err := Open(f.root, "regular")
	if err != nil {
		t.Fatal(err)
	}
	again, err := open.Drive()
	if err != nil || again.Revision != r.Revision {
		t.Fatalf("terminal replay %+v %v", again, err)
	}
}
func TestRegularFacadeIllegalNativeActionDoesNotCreateExecution(t *testing.T) {
	f := readyRegularFacade(t)
	before, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.ExecuteNative("not-issued", false); err == nil {
		t.Fatal("unknown native action executed")
	}
	after, _ := f.Show()
	if after.Revision != before.Revision {
		t.Fatal("illegal action wrote state")
	}
	if _, err = os.Stat(filepath.Join(f.stateDir, "executions")); !os.IsNotExist(err) {
		t.Fatal("illegal native action created transport resources")
	}
}

func TestRegularChangeSupersedesPendingConfirmation(t *testing.T) {
	f := newRegularFacade(t)
	r, _ := f.Show()
	ev, _ := protocol.NewRegularEvent("initial-confirm", protocol.RegularEvent{Operation: "REQUEST", ExpectedRevision: r.Revision, Request: &protocol.RegularRequest{Action: "confirm-requirement"}})
	if _, r, err := f.Submit(ev); err != nil {
		t.Fatal(err)
	} else if r.Regular.Ask == nil {
		t.Fatal("no pending confirmation")
	}
	changed := strings.Replace(b1Document, "hello", "goodbye", -1)
	if err := os.WriteFile(filepath.Join(f.root, "requirements.md"), []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "User changes output while confirmation is pending"})
	doc := protocol.Document("requirements.md", []byte(changed))
	facadeChoice(t, f, protocol.RegularRequest{Action: "apply-requirement", Document: &doc, Meaning: "changed", Origin: "USER_INITIATED_CHANGE", Impact: map[string]string{"product-review": "affected", "start-readiness": "affected"}, Reason: "Confirm complete new requirement"})
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.Decisions["initial-confirm"].Choice != "superseded" || !snap.State.Regular.Requirement.Confirmed || snap.State.Regular.Requirement.Revision != doc.Revision {
		t.Fatal("pending old confirmation blocked or survived the new requirement")
	}
}

func TestRegularOwnerTerminalIntervalAndOverlap(t *testing.T) {
	row := func(input int) string {
		return fmt.Sprintf(`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":%d,"cached_input_tokens":5,"output_tokens":4}}}}`+"\n", input)
	}
	for _, mode := range []string{"single", "same-owner-first", "same-peer-first", "different"} {
		t.Run(mode, func(t *testing.T) {
			overlap := strings.HasPrefix(mode, "same-")
			transcript := filepath.Join(t.TempDir(), "owner.jsonl")
			if err := os.WriteFile(transcript, []byte(row(100)), 0600); err != nil {
				t.Fatal(err)
			}
			f := newRegularFacadeDocument(t, b1Document, transcript)
			var peer *Facade
			peerTranscript := transcript
			if mode == "different" {
				peerTranscript = filepath.Join(t.TempDir(), "peer.jsonl")
				if err := os.WriteFile(peerTranscript, []byte(row(100)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "single" {
				data, _ := json.Marshal(map[string]string{"provider": "codex", "transcript": peerTranscript, "session": "owner-test"})
				if err := os.WriteFile(filepath.Join(f.root, ".gates", "start-owner.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				peer, _, err = Start(StartOptions{Root: f.root, Request: StartRequest{RunID: "peer", RequirementPath: "requirements.md"}, Admission: &Admission{PackageDigest: "sha256:test", InstalledTargetIdentity: "test-target"}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = peer.Drive(); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(transcript, []byte(row(100)+row(60)), 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "different" {
				if err := os.WriteFile(peerTranscript, []byte(row(100)+row(60)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			finish := func(f *Facade) {
				facadeChoice(t, f, protocol.RegularRequest{Action: "confirm-requirement"})
				facadeChoice(t, f, protocol.RegularRequest{Action: "mode", Mode: "lightweight"})
				r, err := f.Drive()
				if err != nil {
					t.Fatal(err)
				}
				if r.Cost.Owner == nil {
					t.Fatal("missing owner terminal projection")
				}
				if overlap {
					if r.Cost.Owner.Source != cost.SourceUnavailable {
						t.Fatal("overlap double-counted owner")
					}
				} else if r.Cost.Owner.Source != cost.SourceTranscript || r.Cost.Owner.TotalInputTokens != 60 || r.Cost.Owner.OutputTokens != 4 {
					t.Fatalf("owner interval %+v", r.Cost.Owner)
				}
			}
			if mode == "same-peer-first" {
				finish(peer)
				finish(f)
			} else {
				finish(f)
				if peer != nil {
					finish(peer)
				}
			}
		})
	}
}

// Keep Windows Git worktree administration paths below Git's internal limit.
func regularFixtureRoot(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		return t.TempDir()
	}
	root, err := os.MkdirTemp("", "fg-engine-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	return root
}
