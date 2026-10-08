//go:build !windows

package harness

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

// Native peers preserve the complete issued result while authors correct their
// own validation residue before the production dispatch accepts delivery.
func TestCanaryProductionArtifactDelivery(t *testing.T) {
	for _, role := range []string{"engine.development.batch", "engine.repair", "engine.merge.resolve", "engine.qa.design"} {
		for _, residue := range []string{"", "notes.txt", "frob", "validation-command"} {
			t.Run(role+"/"+firstNonEmpty(residue, "clean"), func(t *testing.T) {
				material, payload := canaryArtifactMaterial(t, role, coverage.ReviewWhitebox)
				runCanaryArtifactDelivery(t, material, payload, residue, residue != "")
			})
		}
	}
	for _, name := range []string{"whitebox-reuse", "whitebox-omitted-tests", "blackbox", "execution", "repair-plan", "repair-self-run"} {
		t.Run(name, func(t *testing.T) {
			role := "engine.qa.design"
			if strings.HasPrefix(name, "repair") {
				role = "engine.repair"
			}
			if name == "execution" {
				role = "engine.qa.execution"
			}
			kind := coverage.ReviewWhitebox
			if name == "blackbox" {
				kind = coverage.ReviewBlackbox
			}
			material, payload := canaryArtifactMaterial(t, role, kind)
			residue, refuse := "notes.txt", false
			switch name {
			case "whitebox-reuse", "whitebox-omitted-tests":
				payload.Validation.Development = nil
				residue = ""
				if name == "whitebox-omitted-tests" {
					residue, refuse = "omitted_test.go", true
				}
			case "blackbox":
				payload.Validation.Development = nil
			case "execution":
				payload.Validation.Development = nil
			case "repair-plan":
				payload.Validation.Development = nil
				payload.Validation.GranularityReview = &worker.Granularity{}
			case "repair-self-run":
				material.Input.Validation.Repair = &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{}}
			}
			runCanaryArtifactDelivery(t, material, payload, residue, refuse)
		})
	}
}

func canaryArtifactMaterial(t *testing.T, handler string, kind coverage.ReviewKind) (worker.Material, worker.Result) {
	t.Helper()
	input := protocol.WorkerInput{RunID: "delivery", Handler: handler, Provider: "zcode", VCS: "git", Pending: protocol.PendingAction{ActionID: "author", AttemptID: "issued-attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Deliver the authorized artifact only."}, Validation: &protocol.ValidationInput{Handler: handler, Binding: protocol.WorkBinding{WorkspaceID: "author-workspace"}, Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: kind}}}}
	if handler == "engine.qa.execution" {
		input.Validation.Execution = &coverage.ExecutionBinding{}
		input.Validation.Whitelist = &coverage.ApprovedWhitelist{}
	}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "development-worker"}, {ID: "qa-design"}, {ID: "qa-execution"}}}
	material, err := worker.Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	payload := material.ResultTemplate
	payload.Outcome, payload.Validation.Status = protocol.OutcomePass, protocol.OutcomePass
	payload.Validation.Development = &worker.Development{Paths: []string{"delivered_test.go"}, Summary: "authorized artifact"}
	if handler == "engine.merge.resolve" {
		payload.Validation.Development = nil
		payload.Validation.Resolution = &protocol.ConflictResolution{Paths: []string{"delivered_test.go"}, Summary: "resolved issued conflict"}
	}
	return material, payload
}

func runCanaryArtifactDelivery(t *testing.T, material worker.Material, payload worker.Result, residue string, refuse bool) {
	t.Helper()
	s := newZcodeScenario(t, zcodeSuccessSteps, nil)
	home := resolveNativePath(t.TempDir())
	workspace := filepath.Join(home, ".formal-gates-resources", "author")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = workspace
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %q: %v %s", name, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write("go.mod", "module frob\n\ngo 1.22\n")
	write("main.go", "package main\nfunc main() {}\n")
	command("git", "init", "-q")
	command("git", "add", ".")
	command("git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Initial command")
	base := command("git", "rev-parse", "HEAD")
	material.Input.Validation.Workspace = &protocol.NativeResource{ID: material.Input.Validation.Binding.WorkspaceID, Path: workspace, Status: "READY", Identity: base}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "development-worker"}, {ID: "qa-design"}, {ID: "qa-execution"}}}
	var err error
	material, err = worker.Render(material.Input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	// Reading persisted material uses the same issued identity and dispatch entry.
	materialPath := filepath.Join(t.TempDir(), "material.json")
	if err := writeAtomicJSON(materialPath, material); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(readTestFileBytes(t, materialPath), &material); err != nil {
		t.Fatal(err)
	}
	approvalRead, approvalWrite := io.Pipe()
	defer approvalRead.Close()
	defer approvalWrite.Close()
	c := &engineCanary{t: t, home: home, project: workspace, nativeHost: "zcode", binding: objectOrEmpty(s.request["binding"]), jobs: t.TempDir(), worker: true, approvalDeadline: time.Now().Add(time.Minute), run: facade.Run{RunID: material.Input.RunID}, approvalInput: bufio.NewReader(approvalRead), approvalOutput: canaryLocalApproval{approvalWrite}, vcs: vcs.NewNative(workspace, "git", nil)}
	writeCanaryHostFixture(t, home, "peer-provider")
	environment := map[string]any{}
	for key, value := range c.hostEnvironment() {
		environment[key] = value
	}
	environment["FORMAL_GATES_HARNESS_ROOT"] = t.TempDir()
	c.binding["environment"] = environment
	peer := &canaryLocalExecutor{t: t, canary: c}
	calls := 0
	c.execute = func(value any, dir string, probe bool) (map[string]any, error) {
		calls++
		request := value.(map[string]any)
		if probe || request["workspace"] != workspace || request["read_only"] != material.ReadOnly || !valuesEqual(request["schema"], c.plainSchema(material.Input.Pending.ActionID, material.ResultSchema)) {
			t.Fatal("dispatch changed issued method/workspace/schema")
		}
		if calls == 1 {
			if request["prompt"] != material.Prompt {
				t.Fatal("dispatch changed production material")
			}
			if dev := payload.Validation.Development; dev != nil {
				for _, path := range dev.Paths {
					write(path, "package main\n")
				}
			}
			if resolution := payload.Validation.Resolution; resolution != nil {
				for _, path := range resolution.Paths {
					write(path, "package main\n")
				}
			}
			if residue == "frob" {
				command("go", "build", "./...")
			} else if residue == "validation-command" {
				command("go", "build", "-o", residue, "./...")
			} else if residue != "" {
				body := "temporary author output\n"
				if strings.HasSuffix(residue, "_test.go") {
					body = "package main\n"
				}
				write(residue, body)
			}
		} else {
			if !refuse || calls != 2 || !strings.Contains(textOf(request["prompt"]), "Your previous answer was refused: candidate contains undeclared change: "+residue) {
				t.Fatal("correction lost exact VCS diagnostic or finite dispatch identity")
			}
			if _, err := os.Stat(filepath.Join(workspace, residue)); err != nil {
				t.Fatal("consumer removed author's output")
			}
			if err := os.Remove(filepath.Join(workspace, residue)); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		answer := objectOrEmpty(decodeTestJSON(t, string(body)))
		if err := peer.prepareNativePeer(request, dir, answer); err != nil {
			return nil, err
		}
		return runNativeJob(dir)
	}
	event := c.dispatchProduction(material.Input.Pending.ActionID, material)
	wantCalls := 1
	if refuse {
		wantCalls = 2
	}
	if calls != wantCalls || calls > canaryTurnBudget {
		t.Fatalf("delivery calls=%d want=%d", calls, wantCalls)
	}
	raw := readTestFileBytes(t, filepath.Join(c.transcript, "answer.raw.txt"))
	sum := sha256.Sum256(raw)
	if event.Result.ActionID != material.Input.Pending.ActionID || event.Result.Provider != material.Input.Provider || event.Result.PayloadDigest != fmt.Sprintf("sha256:%x", sum) || event.Result.Validation.Binding != material.Input.Validation.Binding {
		t.Fatal("dispatch lost original input and raw evidence binding")
	}
	got := event.Result.Validation
	if got.Status != payload.Validation.Status || !reflect.DeepEqual(got.Development, payload.Validation.Development) || !reflect.DeepEqual(got.Resolution, payload.Validation.Resolution) || !reflect.DeepEqual(canarySemanticPlan(got.GranularityReview), payload.Validation.GranularityReview) {
		t.Fatal("dispatch changed semantic author evidence")
	}

	for i := 1; i <= calls; i++ {
		dir := filepath.Join(c.jobDirectory(material.Input.Pending.ActionID), fmt.Sprintf("attempt-%d", i))
		var observed worker.Result
		if err := json.Unmarshal(readTestFileBytes(t, filepath.Join(dir, "answer.json")), &observed); err != nil || !reflect.DeepEqual(observed, payload) {
			t.Fatalf("native author answer changed: %v", err)
		}
	}
	if refuse {
		if _, err := os.Stat(filepath.Join(workspace, residue)); !os.IsNotExist(err) {
			t.Fatal("author did not clean validation output")
		}
	}
	if command("git", "rev-parse", "HEAD") != base || command("git", "diff", "--cached", "--name-only") != "" {
		t.Fatal("delivery performed VCS writes")
	}
}
