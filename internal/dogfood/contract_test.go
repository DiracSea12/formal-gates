package dogfood

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func validSpec() Spec {
	return Spec{Version: 1, Project: "/project", Base: strings.Repeat("a", 40), RequirementDigest: strings.Repeat("b", 64), Checks: []Check{{ID: "test", Argv: []string{"python3", "test.py"}, Dependencies: []string{"src", "test.py"}}}, Gates: []string{"complexity-gate"}, Provider: "dsh", RepairLimit: 2}
}
func pass() WorkerResult {
	return WorkerResult{Status: "PASS", Summary: "Observed", Findings: []Finding{}, CheckIDs: []string{}}
}
func TestSpecValidation(t *testing.T) {
	if e := ValidateSpec(validSpec()); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		change func(*Spec)
	}{
		{"version", func(s *Spec) { s.Version = 9 }}, {"provider", func(s *Spec) { s.Provider = "unknown-host" }},
		{"duplicate check", func(s *Spec) { s.Checks = append(s.Checks, s.Checks[0]) }},
		{"traversal", func(s *Spec) { s.Checks[0].Dependencies = []string{"../state"} }},
		{"glob", func(s *Spec) { s.Checks[0].Dependencies = []string{"src/**"} }},
		{"no checks", func(s *Spec) { s.Checks = nil }}, {"no gate", func(s *Spec) { s.Gates = nil }},
		{"shell nul", func(s *Spec) { s.Checks[0].Argv = []string{"python3\x00"} }},
		{"unknown gate", func(s *Spec) { s.Gates = []string{"pretend"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := validSpec()
			tc.change(&s)
			if ValidateSpec(s) == nil {
				t.Fatal("accepted invalid spec")
			}
		})
	}
}
func TestNoModelControlFields(t *testing.T) {
	for _, body := range []string{
		`{"status":"PASS","summary":"x","findings":[],"check_ids":[],"goto":"seal"}`,
		`{"status":"PASS","summary":"x","findings":[],"check_ids":[],"state":{}}`,
		`{"status":"PASS","status":"FAIL","summary":"x","findings":[],"check_ids":[]}`,
		`{"status":"PASS","summary":"x","findings":[],"check_ids":[]} {}`,
	} {
		var r WorkerResult
		if StrictDecode([]byte(body), &r) == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestRoleResultContract(t *testing.T) {
	checks := validSpec().Checks
	if e := ValidateWorker("development", pass(), checks); e != nil {
		t.Fatal(e)
	}
	if ValidateWorker("qa-review", pass(), checks) == nil {
		t.Fatal("approved missing coverage")
	}
	r := pass()
	r.CheckIDs = []string{"test"}
	if e := ValidateWorker("qa-review", r, checks); e != nil {
		t.Fatal(e)
	}
	r.CheckIDs = []string{"test", "test"}
	if ValidateWorker("qa-review", r, checks) == nil {
		t.Fatal("duplicate case accepted")
	}
	r = pass()
	r.Findings = []Finding{{ID: "f1", Severity: "P1", Message: "Bug", Paths: []string{"a.go"}}}
	if ValidateWorker("complexity-gate", r, checks) == nil {
		t.Fatal("PASS with blocker accepted")
	}
	r.Findings[0].Severity = "P2"
	if !NeedsRepair(r) {
		t.Fatal("P2 ignored")
	}
	r.Findings[0].Severity = "P3"
	if NeedsRepair(r) {
		t.Fatal("P3 blocks")
	}
	r = pass()
	r.Status = "CONTINUE"
	if e := ValidateWorker("development", r, checks); e != nil {
		t.Fatal(e)
	}
	if ValidateWorker("qa-review", r, checks) == nil {
		t.Fatal("invalid continuation")
	}
}
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "--quiet", root)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, out)
	}
	put := func(name, content string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	put("a.py", "value=1\n")
	put("b.py", "value=1\n")
	put("test.py", "print('test')\n")
	if _, e := git(root, "add", "."); e != nil {
		t.Fatal(e)
	}
	if _, e := git(root, "-c", "user.name=Dogfood Test", "-c", "user.email=dogfood@localhost", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "Test baseline"); e != nil {
		t.Fatal(e)
	}
	_, e := snapshot(workspaceInput{Root: root, OperationID: strings.Repeat("c", 64)})
	if e != nil {
		t.Fatal(e)
	}
	return root
}
func TestSnapshotAndBindings(t *testing.T) {
	root := repo(t)
	head, e := git(root, "rev-parse", "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	q := bindingInput{Root: root, Candidate: head, Revision: strings.Repeat("d", 64), Check: Check{ID: "a", Argv: []string{"python3", "test.py"}, Dependencies: []string{"a.py", "test.py"}}}
	before, e := scopeBinding(q)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "b.py"), []byte("value=2\n"), 0600)
	value, e := snapshot(workspaceInput{Root: root, OperationID: strings.Repeat("e", 64)})
	if e != nil {
		t.Fatal(e)
	}
	q.Candidate = value.(map[string]any)["candidate"].(string)
	after, e := scopeBinding(q)
	if e != nil || before != after {
		t.Fatalf("unrelated change invalidated explicit scope %v", e)
	}
	replay, e := snapshot(workspaceInput{Root: root, OperationID: strings.Repeat("e", 64)})
	if e != nil || !replay.(map[string]any)["replayed"].(bool) {
		t.Fatal("snapshot was not idempotent", e)
	}
	os.WriteFile(filepath.Join(root, "a.py"), []byte("value=3\n"), 0600)
	changed, e := snapshot(workspaceInput{Root: root, OperationID: strings.Repeat("f", 64)})
	if e != nil {
		t.Fatal(e)
	}
	q.Candidate = changed.(map[string]any)["candidate"].(string)
	last, e := scopeBinding(q)
	if e != nil || last == before {
		t.Fatal("affected check reused", e)
	}
}
func TestCertificateRejectsMissingAndStaleEvidence(t *testing.T) {
	root := repo(t)
	head, _ := git(root, "rev-parse", "HEAD")
	spec := validSpec()
	spec.Project = root
	spec.Base = head
	spec.Checks = []Check{{ID: "test", Argv: []string{"python3", "test.py"}, Dependencies: []string{}}}
	revision := Digest(spec)
	binding, e := scopeBinding(bindingInput{root, head, revision, spec.Checks[0]})
	if e != nil {
		t.Fatal(e)
	}
	q := sealInput{Spec: spec, Revision: revision, Root: root, Candidate: head, Checks: []CheckResult{{ID: "test", Status: "PASS", Candidate: head, Binding: binding, Evidence: strings.Repeat("a", 64)}}, Gates: []GateResult{{ID: "complexity-gate", Candidate: head, Revision: revision, Evidence: strings.Repeat("b", 64), Result: pass()}}}
	v, e := certify(q)
	if e != nil {
		t.Fatal(e)
	}
	if v.(map[string]any)["status"] != "DOGFOOD_COMPLETE" || v.(map[string]any)["full_3_5c_equivalence"] != false {
		t.Fatal("misleading certificate")
	}
	q.Checks[0].Binding = "stale"
	if _, e = certify(q); e == nil {
		t.Fatal("stale evidence certified")
	}
	q.Checks = nil
	if _, e = certify(q); e == nil {
		t.Fatal("missing result certified")
	}
}
func TestBridgeRejectsUnknownOperation(t *testing.T) {
	var out bytes.Buffer
	code := Bridge(strings.NewReader(`{"operation":"skip","payload":{}}`), &out)
	if code != 2 {
		t.Fatal(code)
	}
	var r map[string]any
	if e := json.Unmarshal(out.Bytes(), &r); e != nil || r["ok"] != false {
		t.Fatal(out.String())
	}
}

// Host identity validation is shared with existing installation integration;
// execution availability is owned by the driver registry, not the domain.
func TestKnownHostDescriptorsAndSpec(t *testing.T) {
	for _, provider := range []string{"dsh", "zcode", "codex", "cursor", "claude"} {
		s := validSpec()
		s.Provider = provider
		if err := ValidateSpec(s); err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
	}
	for _, provider := range []string{"unknown-host", "deepseek-harness", "z-code"} {
		s := validSpec()
		s.Provider = provider
		if err := ValidateSpec(s); err == nil {
			t.Fatalf("noncanonical/unknown %q accepted", provider)
		}
	}
	var out bytes.Buffer
	if code := Bridge(strings.NewReader(`{"operation":"host-descriptors","payload":{}}`), &out); code != 0 {
		t.Fatalf("host catalog: %s", out.String())
	}
	var catalog struct {
		OK    bool                               `json:"ok"`
		Value []struct{ ID, InstallName string } `json:"value"`
	}
	if err := json.Unmarshal(out.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range catalog.Value {
		if item.ID == "deepseek-harness" && item.InstallName == "dsh" {
			found = true
		}
	}
	if !catalog.OK || !found {
		t.Fatalf("existing descriptor not reused: %s", out.String())
	}
}
