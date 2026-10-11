package cli

import (
	"bytes"
	"encoding/json"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/requirement"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

var retiredWorkflowCommands = []string{"resume", "abort", "reset", "requirement", "review-delta-preview", "requirement-impact-preview", "authorize-retry", "route-candidates", "slicing", "settle-findings", "route", "route-add", "qa-worktree", "prepare-gate", "prepare-action", "claim-dispatch", "record-action", "record-gate", "qa-design", "qa-review", "qa-execution", "qa-execution-scope", "qa-thaw", "qa-suggestion", "prepare-candidate", "prepare-review-wave", "adjudicate-wave", "resolve-wave-target", "snapshot", "cleanup", "candidate", "carry", "authorize-repair", "repair-preview", "complete-p2-repair", "seal"}

func TestWorkflowPublicSurface(t *testing.T) {
	actual := []string{}
	for entry := range workflowSubcommands {
		actual = append(actual, entry)
	}
	slices.Sort(actual)
	expected := []string{"diagnose", "drive", "guard-preview", "handoff", "message", "next", "progress", "replan", "show", "start", "status", "submit"}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("public workflow entries=%v want=%v", actual, expected)
	}
	for _, entry := range retiredWorkflowCommands {
		var out, err bytes.Buffer
		code := Run("formal-gates", []string{"workflow", entry, "--root", t.TempDir()}, IO{Stdout: &out, Stderr: &err})
		if code == 0 || !strings.Contains(err.String(), "unknown workflow subcommand: "+entry) {
			t.Fatalf("removed entry %s is still reachable: %d %s %s", entry, code, out.String(), err.String())
		}
	}
}

func TestWorkflowPrecheckIsReadOnlyAndShowsExactConfirmationContent(t *testing.T) {
	root := t.TempDir()
	body := cliStructuredRequirement("Implement the documented command behavior.") + "\n## 方案\n\nImplement only the confirmed behavior and verify its public entry.\n"
	path := filepath.Join(root, "requirements.md")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	projection, err := requirement.ParseRequirementProjection("requirements.md", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "start", "--precheck", "--root", root, "--requirement", "requirements.md"}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var report struct {
		Content   string                                  `json:"content"`
		Revision  string                                  `json:"revision"`
		Document  requirement.FrozenRequirementProjection `json:"requirementDocument"`
		Materials []facade.IntakeMaterial                 `json:"materials"`
		Candidate facade.IntakeConfirmationReceipt        `json:"intakeCandidate"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Content != projection.Content || report.Revision != projection.ContentDigest || !reflect.DeepEqual(report.Document, projection) {
		t.Fatal("precheck changed the full content, revision or typed projection")
	}
	if len(report.Materials) != 1 || report.Materials[0].Content != report.Content || report.Materials[0].Revision != report.Revision || report.Candidate.RequirementRevision != report.Revision || report.Candidate.SolutionRevision != report.Revision || report.Candidate.Digest == "" {
		t.Fatal("precheck did not prepare its exact single-file intake candidate")
	}
	if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
		t.Fatal("precheck created runtime or bootstrap state")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != body {
		t.Fatal("precheck changed the source bytes")
	}
}

func TestWorkflowPrecheckAssignsMissingIdentitiesWithoutChangingMaterial(t *testing.T) {
	root := t.TempDir()
	body := strings.ReplaceAll(strings.ReplaceAll(cliStructuredRequirement("Implement the documented behavior."), "REQ-001：", ""), "AC-001：", "")
	path := filepath.Join(root, "requirements.md")
	mustWriteCLI(t, path, body)
	var out, stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "start", "--precheck", "--root", root, "--requirement", "requirements.md"}, IO{Stdout: &out, Stderr: &stderr})
	if code != 0 {
		t.Fatal(stderr.String())
	}
	var preview facade.IntakePreview
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	document := preview.RequirementDocument
	if document == nil || len(document.Requirements) != 1 || document.Requirements[0].ID != "REQ-001" || len(document.Requirements[0].AcceptanceConditions) != 1 || document.Requirements[0].AcceptanceConditions[0].ID != "AC-001" {
		t.Fatalf("precheck did not assign the semantic source identities: %+v", document)
	}
	if preview.Content != body || document.Content != body || preview.Revision != document.ContentDigest || preview.IntakeCandidate.RequirementRevision != preview.Revision {
		t.Fatal("assigned identities changed the original confirmation content/binding")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != body {
		t.Fatal("precheck rewrote the source material", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
		t.Fatal("precheck started a run or wrote runtime state")
	}
}

func TestWorkflowPrecheckMaterialSelectionDoesNotConfirmOrStart(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{"request.md": "# Actual request\n", "design.md": "# Actual solution\n", "reference.md": "# Selected reference\n"} {
		mustWriteCLI(t, filepath.Join(root, path), content)
	}
	var out, stderr bytes.Buffer
	args := []string{"workflow", "start", "--precheck", "--root", root, "--route", "lightweight", "--requirement", "request.md", "--solution", "design.md", "--artifact", "reference.md"}
	if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var preview facade.IntakePreview
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Materials) != 3 || preview.RequirementDocument != nil || preview.IntakeCandidate.RequirementSource != "request.md" || preview.IntakeCandidate.SolutionRevision != preview.Materials[0].Revision {
		t.Fatal("precheck lost explicit material selection")
	}
	out.Reset()
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--run-id", "unconfirmed", "--route", "lightweight"}, IO{Stdout: &out, Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "requires --intake-receipt") {
		t.Fatalf("preview substituted for explicit confirmation receipt: %s", stderr.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 3 {
		t.Fatal("unconfirmed material selection wrote receipt, registry or runtime state", err)
	}
}

func TestWorkflowUnsupportedStateIsDiagnosticOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gates", "tmp", "unsupported", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"runId":"unsupported","status":"ACTIVE","actions":{}}`)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"show", "status", "next", "drive", "submit"} {
		var out, stderr bytes.Buffer
		code := Run("formal-gates", []string{"workflow", entry, "--root", root, "--run-id", "unsupported"}, IO{Stdout: &out, Stderr: &stderr})
		if code == 0 || !strings.Contains(stderr.String(), "UNSUPPORTED_RUN_VERSION") {
			t.Fatalf("%s accepted unsupported state: %d %s", entry, code, stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "diagnose", "--root", root, "--run-id", "unsupported"}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var report struct {
		Path         string `json:"path"`
		JSONReadable bool   `json:"jsonReadable"`
		Integrity    string `json:"integrity"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Path != path || !report.JSONReadable || report.Integrity != "unsupported" {
		t.Fatalf("raw diagnostic=%s", out.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(body, after) {
		t.Fatal("diagnostic or unsupported entry rewrote old state")
	}
}

func TestWorkflowPrecheckSavesExactCandidateWithoutStartingRun(t *testing.T) {
	root := t.TempDir()
	body := cliStructuredRequirement("Implement only the documented command behavior.")
	mustWriteCLI(t, filepath.Join(root, "request.md"), body)
	saved := filepath.Join(root, "confirmation.json")
	var out, stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "start", "--precheck", "--root", root, "--requirement", "request.md", "--save-candidate", saved}, IO{Stdout: &out, Stderr: &stderr})
	if code != 0 {
		t.Fatal(stderr.String())
	}
	var preview facade.IntakePreview
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	var receipt facade.IntakeConfirmationReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(receipt, preview.IntakeCandidate) {
		t.Fatal("saved candidate differed from complete program output")
	}
	if _, err = os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
		t.Fatal("saving an intake candidate started a run or wrote admission", err)
	}
}
