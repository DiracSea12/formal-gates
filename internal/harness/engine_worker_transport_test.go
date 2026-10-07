//go:build !windows

package harness

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"formal-gates/internal/prompts"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/requirement"
)

func TestProductionWorkerNativeInputAndCoreResult(t *testing.T) {
	s := newCodexScenario(t)
	home := s.home
	workspace := filepath.Join(home, ".formal-gates-resources", "project")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	s.workspace = workspace
	s.peer.Workspace = workspace
	s.request["workspace"] = workspace
	catalogRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := prompts.LoadPromptCatalog(catalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := requirement.ParseRequirementProjection("requirements.md", []byte(canaryRequirementDocument))
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.WorkerInput{RunID: "native-worker", Provider: "codex", VCS: "git", Handler: "engine.review.worker", Pending: protocol.PendingAction{ActionID: "review-action", AttemptID: "review-attempt", Task: runtime.TaskKey{Node: "review", Step: "review.worker"}, ReviewSeries: review.ProductReview}, RequirementDocument: doc, Review: &protocol.WorkerReviewInput{Action: review.ProductReview, RequirementRevision: doc.ContentDigest}}
	material, err := worker.Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.MarshalIndent(material, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	hash := sha256.Sum256(body)
	materialPath := filepath.Join(home, "material.json")
	if err := os.WriteFile(materialPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	ref := facade.WorkerMaterial{ActionID: input.Pending.ActionID, AttemptID: input.Pending.AttemptID, Path: materialPath, Digest: "sha256:" + hex.EncodeToString(hash[:]), ReadOnly: true}
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	c := &engineCanary{t: t, nativeHost: "codex", localPeer: true, home: home, project: workspace, jobs: t.TempDir(), worker: true, binding: objectOrEmpty(s.request["binding"]), deadline: time.Now().Add(time.Minute), run: facade.Run{RunID: input.RunID, PendingActions: map[string]protocol.PendingAction{input.Pending.ActionID: input.Pending}, WorkerMaterials: map[string]facade.WorkerMaterial{input.Pending.ActionID: ref}}, approvalInput: bufio.NewReader(r), approvalOutput: canaryLocalApproval{w}}
	payload := material.ResultTemplate
	payload.Outcome = protocol.OutcomePass
	payload.Review.Status = "PASS"
	payload.Review.Findings = []review.Finding{}
	payload.Review.Verification = review.OperatorVerification{Checks: []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, Evidence: "read complete confirmed delivery document"}
	answer, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(answer, &s.peer.Answer); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c.execute = func(request any, directory string, probe bool) (map[string]any, error) {
		calls++
		if probe || getOf(request, "prompt") != material.Prompt || !valuesEqual(getOf(request, "schema"), codexPlain(t, material.ResultSchema)) || getOf(request, "workspace") != workspace || getOf(request, "read_only") != true {
			t.Fatal("native call did not consume exact production prompt/schema/role input")
		}
		s.request = request.(map[string]any)
		s.directory = directory
		s.peer.StrictSchema = true
		return s.execute(t)
	}
	event := c.dispatchProduction(input.Pending.ActionID, material)
	if calls != 1 || event.Result == nil || event.Result.ActionID != input.Pending.ActionID || event.Result.Review.DispatchID != input.Pending.ActionID || event.Result.Review.RequirementRevision != doc.ContentDigest {
		t.Fatal("complete core native payload was converted or rebound")
	}
	raw := readTestFileBytes(t, filepath.Join(c.transcript, "answer.raw.txt"))
	rawHash := sha256.Sum256(raw)
	if event.Result.PayloadDigest != "sha256:"+hex.EncodeToString(rawHash[:]) {
		t.Fatal("final event used a shape/decoded digest instead of original native bytes")
	}
	if event.Result.Review.Verification.Evidence != payload.Review.Verification.Evidence {
		t.Fatal("native core evidence was rewritten")
	}
}

func TestWorkerRoleSchemasUseNativeStrictTransport(t *testing.T) {
	for _, handler := range []string{"engine.development.batch", "engine.repair", "engine.merge.resolve", "engine.qa.design", "engine.qa.review.rationale", "engine.qa.execution", "engine.gate"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{Handler: handler, Pending: protocol.PendingAction{ActionID: "issued"}, Provider: "codex", Validation: &protocol.ValidationInput{}}
			schema, err := worker.ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(schema)
			wire, err := codexOutputSchema(schema)
			if err != nil {
				t.Fatal(err)
			}
			if err := codexPeerStrictSchema(codexPlain(t, wire)); err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(schema)
			if string(before) != string(after) {
				t.Fatal("native adapter changed domain result schema")
			}
		})
	}
}

func bindCanaryReviewMaterial(t *testing.T, c *engineCanary, pending protocol.PendingAction, content []byte) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := prompts.LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := requirement.ParseRequirementProjection(filepath.Join(c.project, "requirements.md"), content)
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.WorkerInput{RunID: c.run.RunID, Provider: c.provider(), VCS: "git", Handler: "engine.review.worker", Pending: pending, RequirementDocument: doc, Review: &protocol.WorkerReviewInput{Action: pending.ReviewSeries, RequirementRevision: doc.ContentDigest}}
	material, err := worker.Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "worker-material.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	c.run.WorkerMaterials = map[string]facade.WorkerMaterial{pending.ActionID: {ActionID: pending.ActionID, AttemptID: pending.AttemptID, Path: path, Digest: "sha256:" + hex.EncodeToString(hash[:]), ReadOnly: material.ReadOnly}}
}
