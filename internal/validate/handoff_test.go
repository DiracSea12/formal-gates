package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func basicHandoffOptions(root string) HandoffComposeOptions {
	return HandoffComposeOptions{
		Root: root, WorkflowID: "wf", ChangeSnapshot: "snap", Output: "restricted/handoff.md",
		VCS: "git", RequirementTarget: "openspec/changes/example", VerificationRequirements: "go test ./...",
		ForbiddenContext: "prior findings", FormalFlowMode: "none", TriggerSource: "user",
	}
}

func TestComposeHandoffContainsOnlySemanticWorkerContract(t *testing.T) {
	dir := t.TempDir()
	ref, result := ComposeHandoff(basicHandoffOptions(dir))
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	path := filepath.Join(dir, ".gates", "runs", "wf", filepath.FromSlash(ref.Path))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"VCS: git", "Verification requirements: go test ./...", "Forbidden context: prior findings"} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated handoff missing %q: %s", want, text)
		}
	}
	if result := Handoff(HandoffOptions{Root: dir, File: filepath.Join(".gates", "runs", "wf", filepath.FromSlash(ref.Path)), WorkflowID: "wf", ChangeSnapshot: "snap"}); !result.OK() {
		t.Fatalf("generated handoff did not validate: %#v", result.Failures)
	}
}

func TestComposeHandoffPreservesExplicitRunDir(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, ".gates", "runs", "custom-run")
	options := basicHandoffOptions(dir)
	options.RunDir = runDir
	ref, result := ComposeHandoff(options)
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	file := filepath.Join(".gates", "runs", "custom-run", filepath.FromSlash(ref.Path))
	if result := Handoff(HandoffOptions{Root: dir, File: file, WorkflowID: "wf", ChangeSnapshot: "snap"}); !result.OK() {
		t.Fatal(result.Failures)
	}
}

func TestHandoffRequiresAcceptedDesignReviewChainForFormalFlow(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, ".gates", "runs", "custom-run")
	caseSet, designReview, err := writeCanaryDesignReviewClosure(dir, runDir, "wf", "design-snap")
	if err != nil {
		t.Fatal(err)
	}
	options := basicHandoffOptions(dir)
	options.RunDir, options.ChangeSnapshot, options.FormalFlowMode = runDir, "design-snap", "seal"
	options.QACaseSet, options.DesignReview = caseSet.Path, designReview.Path
	ref, result := ComposeHandoff(options)
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	file := filepath.Join(".gates", "runs", "custom-run", filepath.FromSlash(ref.Path))
	if result := Handoff(HandoffOptions{Root: dir, File: file, WorkflowID: "wf", ChangeSnapshot: "design-snap"}); !result.OK() {
		t.Fatal(result.Failures)
	}
}

func TestComposeHandoffRejectsFormalFlowWithoutVCS(t *testing.T) {
	options := basicHandoffOptions(t.TempDir())
	options.FormalFlowMode, options.VCS = "seal", "none"
	if _, result := ComposeHandoff(options); result.OK() {
		t.Fatal("formal handoff without external VCS passed")
	}
}

func TestComposeHandoffRejectsLineBreakingScalars(t *testing.T) {
	for name, apply := range map[string]func(*HandoffComposeOptions){
		"workflow-id":  func(o *HandoffComposeOptions) { o.WorkflowID += "\nother" },
		"vcs":          func(o *HandoffComposeOptions) { o.VCS += "\rnone" },
		"requirement":  func(o *HandoffComposeOptions) { o.RequirementTarget += "\nother" },
		"verification": func(o *HandoffComposeOptions) { o.VerificationRequirements += "\r" },
		"forbidden":    func(o *HandoffComposeOptions) { o.ForbiddenContext += "\nother" },
	} {
		t.Run(name, func(t *testing.T) {
			options := basicHandoffOptions(t.TempDir())
			apply(&options)
			if _, result := ComposeHandoff(options); result.OK() {
				t.Fatal("line-breaking scalar passed")
			}
		})
	}
}

func TestHandoffEvidenceRefKeepsRunLocalPathSpaces(t *testing.T) {
	hash := strings.Repeat("a", 64)
	ref, ok := handoffEvidenceRef("path=qa design/cases.md sha256=" + hash)
	if !ok || ref.Path != "qa design/cases.md" || ref.SHA256 != hash {
		t.Fatalf("unexpected ref: %#v ok=%v", ref, ok)
	}
}
