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
	"formal-gates/internal/harness"
	"formal-gates/internal/prompts"
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
