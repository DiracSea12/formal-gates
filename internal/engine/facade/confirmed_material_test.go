package facade

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/harness"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestConfirmedMaterialsReachOriginalWorkerReader(t *testing.T) {
	root := t.TempDir()
	snapshotRepoCommits(t, root)
	for path, content := range map[string]string{"design.md": "# Selected solution\nUse exact alpha protocol.\n", "reference.md": "# Selected reference\nOriginal beta constraint.\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "design.md", "reference.md"}, {"commit", "-qm", "Selected material source"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if body, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(body))
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	head, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareIntake(root, "requirements.md", "design.md", []string{"reference.md"}, true)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := Start(StartOptions{Root: root, ArtifactRoot: root, Capacity: 2, Request: StartRequest{RunID: "confirmed-material", Route: "regular", Provider: "zcode", VCS: "git", BaseSnapshot: strings.TrimSpace(string(head)), DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: preview.IntakeCandidate}, Admission: testAdmission(t, root, "material-runtime", "sha256:package", "zcode")})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if len(run.WorkerMaterials) != 1 {
		t.Fatalf("confirmed regular material did not activate original review: %+v", run.Next)
	}
	var ref WorkerMaterial
	for _, value := range run.WorkerMaterials {
		ref = value
	}
	material, err := readWorkerMaterial(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(material.Input.Materials, preview.Materials) || !reflect.DeepEqual(material.Input.Intake, &preview.IntakeCandidate) {
		t.Fatal("worker payload changed confirmed source identity or bytes")
	}
	if strings.Contains(material.Prompt, preview.IntakeCandidate.Digest) || !strings.Contains(material.Prompt, "solution") {
		t.Fatal("model material selection leaked machine digest or lost selected solution")
	}
	request, _, directory, _, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "native-request.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	call := func(arguments map[string]any) map[string]any {
		t.Helper()
		frame := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "workspace", "arguments": arguments}}
		raw, _ := json.Marshal(frame)
		var out, errout bytes.Buffer
		if code := harness.NativeTools(directory, bytes.NewReader(append(raw, '\n')), &out, &errout, harness.ExecutionServices{Admit: AdmitExecution}); code != 0 {
			t.Fatal("original MCP failed", errout.String())
		}
		var response struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err = json.Unmarshal(out.Bytes(), &response); err != nil || response.Result.IsError || len(response.Result.Content) != 1 {
			t.Fatal("original MCP result", out.String(), err)
		}
		var value map[string]any
		if err = json.Unmarshal([]byte(response.Result.Content[0].Text), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	reads := make([]any, len(preview.Materials))
	for i, source := range preview.Materials {
		value := call(map[string]any{"action": "requirement", "material": i + 1})
		encoded, _ := json.Marshal(value["lines"])
		var lines []string
		if err = json.Unmarshal(encoded, &lines); err != nil {
			t.Fatal(err)
		}
		if value["path"] != source.Path || value["contentDigest"] != source.Revision || strings.Join(lines, "") != source.Content {
			t.Fatal("original MCP material differed", source.Path, value)
		}
		reads[i] = value["read"]
	}
	// An owned invocation keeps its already published source on process recovery.
	if err = os.WriteFile(filepath.Join(directory, "owner"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"design.md", "reference.md"} {
		if err = os.WriteFile(filepath.Join(root, path), []byte("unconfirmed new bytes\n"), 0600); err != nil {
			t.Fatal(err)
		}
		state, err := f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		input, current := state.State.WorkerInput(ref.ActionID, f.definition)
		if !current {
			t.Fatal("original issued action disappeared")
		}
		catalog, err := prompts.LoadPromptCatalog(f.admission.Target)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.workerMaterial(input, catalog, true); err == nil {
			t.Fatal("new publication accepted unconfirmed source changes", path)
		}
		recovered, err := Open(root, run.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, restored, err := recovered.WorkerExecutionRequest(ref, ""); err != nil || restored != ref {
			t.Fatal("owned recovery changed original material source", err)
		}
		for i, source := range preview.Materials {
			value := call(map[string]any{"action": "requirement", "material": i + 1})
			if value["contentDigest"] != source.Revision {
				t.Fatal("restart changed original material read identity")
			}
		}
		for _, source := range preview.Materials {
			if source.Path == path {
				if err = os.WriteFile(filepath.Join(root, path), []byte(source.Content), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	contextBody, err := os.ReadFile(filepath.Join(directory, "native-tool-context.json"))
	if err != nil {
		t.Fatal(err)
	}
	var context struct {
		Facts []struct {
			Kind    string   `json:"kind"`
			Paths   []string `json:"paths"`
			Content []byte   `json:"content"`
		} `json:"facts"`
	}
	if err = json.Unmarshal(contextBody, &context); err != nil {
		t.Fatal(err)
	}
	for i, source := range preview.Materials {
		fact := context.Facts[int(reads[i].(float64))-1]
		if string(fact.Content) != source.Content {
			t.Fatal("original read position changed its frozen bytes")
		}
		if source.Path == "requirements.md" {
			if fact.Kind != "requirement" || len(fact.Paths) != 0 {
				t.Fatal("formal requirement acquired physical file permission")
			}
		} else if fact.Kind != "material" || !reflect.DeepEqual(fact.Paths, []string{source.Path}) {
			t.Fatal("confirmed semantic source lost its original identity")
		}
	}
	after, err := os.ReadFile(ref.Path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("immutable worker payload changed on publication/recovery", err)
	}
}

// The public material producer must carry a user's settled finding into the
// following review without editing the immutable original worker material.
func TestSettledFindingReachesReadinessMaterial(t *testing.T) {
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root)
	content, err := os.ReadFile(filepath.Join(root, "requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := requirement.ParseRequirementProjection("requirements.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Requirements) != 1 || len(document.Requirements[0].AcceptanceConditions) != 1 {
		t.Fatal("the standard REQ/AC producer did not freeze the expected obligation")
	}
	preview, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RequirementDocument == nil || !reflect.DeepEqual(*preview.RequirementDocument, document) {
		t.Fatal("typed intake differs from the standard document parser")
	}
	f, _, err := Start(StartOptions{Root: root, ArtifactRoot: root, Capacity: 2,
		Request:   StartRequest{RunID: "settled-material", Route: "regular", Provider: "zcode", VCS: "git", BaseSnapshot: identities[0], DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: preview.IntakeCandidate},
		Admission: testAdmission(t, root, "material-runtime", "sha256:package", "zcode"),
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.Validation == nil || !reflect.DeepEqual(run.Validation.Document, document) {
		t.Fatal("first drive's original REQUIREMENT event did not carry the parsed confirmed document")
	}
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, registered := snap.State.Events["intake.requirement:"+preview.Revision]; !registered {
		t.Fatal("the original public producer did not persist its REQUIREMENT event")
	}
	if snap.State.IntakeReceipt == nil || !reflect.DeepEqual(snap.State.IntakeReceipt.Confirmation, preview.IntakeCandidate) {
		t.Fatal("first drive lost the original typed intake receipt")
	}
	if len(run.WorkerMaterials) != 1 {
		t.Fatalf("product material count = %d", len(run.WorkerMaterials))
	}
	var productRef WorkerMaterial
	for _, ref := range run.WorkerMaterials {
		productRef = ref
	}
	productMaterial, err := readWorkerMaterial(productRef)
	if err != nil {
		t.Fatal(err)
	}
	if productMaterial.Input.Review == nil || productMaterial.Input.Review.Action != review.ProductReview || productMaterial.Input.Review.RequirementRevision != preview.Revision || !reflect.DeepEqual(productMaterial.Input.Materials, preview.Materials) {
		t.Fatal("product material differs from original current issued identity or confirmed bytes")
	}
	productBytes, err := os.ReadFile(productRef.Path)
	if err != nil {
		t.Fatal(err)
	}
	d := &validationDriver{t: t, f: f, run: run, revision: preview.Revision, provider: "zcode"}
	// Existing exact-provider fixtures publish zero native usage and lifecycle
	// facts. No host process or real model call is performed by this test.
	d.meteredSpawn(root, productRef.ActionID)
	finding := review.Finding{Severity: "P2", Message: "输入示例是否需要额外约束", Requirement: "REQ-001 / AC-001", NormalPath: "阅读已确认输入验收", ActualError: "需要核实示例是否形成额外约束", Locations: []string{"requirements.md:5"}}
	event, err := protocol.NewReviewWorkerResultEvent(d.eventID(), productRef.ActionID, "zcode", "sha256:semantic-evidence", protocol.ReviewResultEvidence{
		Action: productMaterial.Input.Review.Action, DispatchID: productRef.ActionID, RequirementRevision: productMaterial.Input.Review.RequirementRevision, Status: "PASS", Message: "完整产品审已核实",
		Findings: []review.Finding{finding},
	})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(event)
	verifyReviewFixture(t, d, event)
	request, err := f.PrepareControl(ControlInput{Request: protocol.ControlReviewFindingDecision, Review: 1, Candidate: 1, Finding: 1, Choice: "DISMISS", Reason: "AC-001已经完整约束正常输入与错误输入"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = f.SubmitControl(request)
	if err != nil {
		t.Fatal(err)
	}
	ask := 0
	for i, value := range run.PendingAsks {
		if value.Control == protocol.ControlReviewFindingDecision {
			ask = i + 1
		}
	}
	if ask == 0 {
		t.Fatal("typed finding request did not create the original user Ask")
	}
	decision, err := f.PrepareControl(ControlInput{Ask: ask, Review: 1, Candidate: 1, Finding: 1, Choice: "DISMISS", Reason: "AC-001已经完整约束正常输入与错误输入"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = f.SubmitControl(decision)
	if err != nil {
		t.Fatal(err)
	}
	snap, err = f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	series := snap.State.ReviewSeries.ProductReview
	if len(series.RawCandidates) != 1 || len(series.Adjudications) != 1 || series.Adjudications[0].Decision != "DISMISS" {
		t.Fatalf("the original typed settlement did not persist: %+v", series)
	}
	stableID := series.RawCandidates[0].Findings[0].ID
	var readinessRef WorkerMaterial
	for _, ref := range run.WorkerMaterials {
		value, err := readWorkerMaterial(ref)
		if err != nil {
			t.Fatal(err)
		}
		if value.Input.Review != nil && value.Input.Review.Action == review.StartReadiness {
			readinessRef = ref
		}
	}
	if readinessRef.Path == "" {
		t.Fatal("product PASS with original settlement did not publish readiness material")
	}
	readiness, err := readWorkerMaterial(readinessRef)
	if err != nil {
		t.Fatal(err)
	}
	readinessBytes, err := os.ReadFile(readinessRef.Path)
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Input.Review.RequirementRevision != preview.Revision || !reflect.DeepEqual(readiness.Input.Materials, preview.Materials) || !reflect.DeepEqual(readiness.Input.RequirementDocument, document) {
		t.Fatal("readiness lost original frozen REQ/AC material or original revision")
	}
	reopened, err := Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := reopened.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if again.WorkerMaterials[readinessRef.ActionID] != readinessRef {
		t.Fatal("normal reopen changed the readiness material identity")
	}
	actual, err := os.ReadFile(readinessRef.Path)
	if err != nil || !bytes.Equal(actual, readinessBytes) {
		t.Fatal("normal reopen changed published readiness bytes", err)
	}
	actual, err = os.ReadFile(productRef.Path)
	if err != nil || !bytes.Equal(actual, productBytes) {
		t.Fatal("settlement rewrote original product material", err)
	}
	inputBytes, err := json.Marshal(readiness.Input)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("public intake/REQUIREMENT→product P2→typed DISMISS→readiness; stableID=%s decision=%s revision=%s; inputHasID=%t promptHasID=%t promptHasReason=%t; reopenSameMaterial=true originalProductUnchanged=true", stableID, series.Adjudications[0].Decision, preview.Revision, bytes.Contains(inputBytes, []byte(stableID)), strings.Contains(readiness.Prompt, stableID), strings.Contains(readiness.Prompt, series.Adjudications[0].Reason))
	for _, required := range []string{stableID, finding.Message, finding.Locations[0], series.Adjudications[0].Decision, series.Adjudications[0].Reason} {
		if !bytes.Contains(inputBytes, []byte(required)) || !strings.Contains(readiness.Prompt, required) {
			t.Errorf("original settled fact absent from typed input or readiness prompt: %q", required)
		}
	}
}
