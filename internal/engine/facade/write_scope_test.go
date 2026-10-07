package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"os/exec"
)

func TestWriteScopeUsesIssuedEngineWorkAndOwnerSession(t *testing.T) {
	root := t.TempDir()
	repo := newFrozenGitRepo()
	sum := sha256.Sum256([]byte(validationDocument))
	receipt := testReceipt()
	receipt.RequirementRevision = hex.EncodeToString(sum[:])
	receipt.Artifacts[0].Revision = receipt.RequirementRevision
	f, run, err := semanticValidationFacade(t, StartOptions{Root: root, Capacity: 16, Owner: OwnerBinding{Session: "owner-session"}, VCSRunner: repo, Admission: testAdmission(t, root, "scope-owner", "sha256:scope-package", DefaultProvider), Request: StartRequest{RunID: "scope", Route: "regular", Provider: DefaultProvider, BaseSnapshot: frozenSnapshot, IntakeConfirmationReceipt: receipt}})
	if err != nil {
		t.Fatal(err)
	}
	d := &validationDriver{t: t, f: f, run: run, revision: receipt.RequirementRevision, provider: DefaultProvider, git: vcs.New(root, repo), frozen: repo}
	binary := filepath.Join(t.TempDir(), "formal-gates")
	source, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = source
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build hook consumer: %v %s", err, output)
	}
	decision := func(session, role, path string) struct {
		Decision   string `json:"decision"`
		Permission string `json:"permission"`
	} {
		t.Helper()
		payload, err := json.Marshal(map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, path)}, "cwd": root, "session_id": session, "agent_type": role})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, "hook", "decide")
		cmd.Stdin = bytes.NewReader(payload)
		output, runErr := cmd.CombinedOutput()
		var exit *exec.ExitError
		if runErr != nil && !errors.As(runErr, &exit) {
			t.Fatalf("public hook: %v %s", runErr, output)
		}
		actualExit := 0
		if exit != nil {
			actualExit = exit.ExitCode()
		}
		if actualExit != 0 && actualExit != 2 {
			t.Fatalf("generic hook process exit=%d response=%s", actualExit, output)
		}
		var result struct {
			Decision   string `json:"decision"`
			Permission string `json:"permission"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("hook response: %v %s", err, output)
		}
		wantExit := 0
		if result.Permission == "deny" {
			wantExit = 2
		}
		if actualExit != wantExit {
			t.Fatalf("generic hook protocol exit=%d want=%d response=%s", actualExit, wantExit, output)
		}
		return result
	}
	predev, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	gotPredev := decision("owner-session", "", "app.go")
	t.Logf("pre-development hook=%+v phase=%s issuedBindings=%+v", gotPredev, predev.State.Phase, predev.State.DispatchBindings)
	if gotPredev.Decision != "approve" {
		t.Fatal("pre-development owner scope blocked ordinary work")
	}
	d.drive()
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.revision, Document: validationDocument}}})
	passPreDevelopmentReviews(t, d, commandPlan())
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command"}})
	d.confirmRoute(routeDecisionForGates([]string{"blackbox"}))
	d.drive()
	snapshot, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State.OwnerSession != "owner-session" || !snapshot.State.DevelopmentStarted() {
		t.Fatal("bootstrap scope or issued development fact is missing")
	}
	if _, _, _, _, _, metered := f.engine.OwnerCostFacts(); metered {
		t.Fatal("session-only scope invented a metering source")
	}
	if got := decision("owner-session", "", "app.go"); got.Decision != "block" {
		t.Fatal("owner bypassed the current engine development wall")
	}
	if got := decision("another-session", "", "app.go"); got.Decision != "approve" {
		t.Fatal("another conversation was incorrectly owner-scoped")
	}
	if got := decision("owner-session", "", "requirements.md"); got.Decision != "approve" {
		t.Fatal("confirmed document path exemption was lost")
	}
	if got := decision("owner-session", "product-review", "app.go"); got.Decision != "block" {
		t.Fatal("reviewer bypassed the current engine wall")
	}
	if got := decision("owner-session", "development-worker", "app.go"); got.Decision != "approve" {
		t.Fatal("issued development role was blocked")
	}
	before, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Show(); err != nil {
		t.Fatal(err)
	}
	if got := decision("owner-session", "", "app.go"); got.Decision != "block" {
		t.Fatal("reopen lost owner write scope")
	}
	after, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("auxiliary scope observation wrote authority")
	}
}
