package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActionMaterialPathsExcludeBodiesAndUnrelatedFields(t *testing.T) {
	job := t.TempDir()
	body := strings.Repeat("required-contract-text ", 100000)
	req := protocol.RequirementDocument{Text: body, Revision: "bound-revision"}
	m := nativePromptMaterial{instructions: "Read the specified files for this action.", input: map[string]any{"requirement": req, "design": map[string]any{"manifest": body}}}
	if err := m.bindFiles(job, req); err != nil {
		t.Fatal(err)
	}
	prompt, err := m.render(protocol.ExecutionClaim{})
	if err != nil {
		t.Fatal(err)
	}
	if len(prompt) > 4096 || strings.Contains(prompt, "required-contract-text") {
		t.Fatal("body entered initial input", len(prompt))
	}
	if err := m.writeFiles(job); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(job, "requirement.md"))
	if err != nil || string(got) != body {
		t.Fatal("requirement path unavailable", err)
	}
	data, err := os.ReadFile(filepath.Join(job, "design.json"))
	if err != nil {
		t.Fatal(err)
	}
	var design map[string]string
	if err = json.Unmarshal(data, &design); err != nil || design["manifest"] != body {
		t.Fatal("design body changed", err)
	}
	if _, ok := m.files["previous-design.json"]; ok {
		t.Fatal("unrelated history materialized")
	}
	if err := m.writeFiles(job); err != nil {
		t.Fatal("identical replay", err)
	}
	m.files["requirement.md"] = []byte("changed")
	if err := m.writeFiles(job); err == nil {
		t.Fatal("changed replay accepted")
	}
}
func TestNativeReviewInputUsesCapturedContractPaths(t *testing.T) {
	f, _, _ := acceptedCapturedDesign(t)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	x := facadeExecution(t, f, "blackbox-review")
	plan, err := f.prepareNativeActionInput(run, x.Claim, x, f.codexJob(x.Claim.ActionID))
	if err != nil {
		t.Fatal(err)
	}
	if err = plan.preflight(x.Claim, codexRuntimeAdapter{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan.prompt, "observable behavior.") {
		t.Fatal("public contracts inlined")
	}
	if !strings.Contains(plan.prompt, "public-source-3.txt") {
		t.Fatal("missing contract path")
	}
	for name := range plan.material.files {
		if strings.Contains(name, "production") || strings.Contains(name, "sourceResults") {
			t.Fatal("unrelated materials admitted", name)
		}
	}
}

// Explicit opt-in uses a read-only copy of the real run to measure the same
// shared assembly with current instructions, without claiming or changing it.
func TestActualStagePromptPathsReadOnly(t *testing.T) {
	state := os.Getenv("FORMAL_GATES_PROMPT_READONLY_STATE")
	if state == "" {
		t.Skip("explicit read-only state required")
	}
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	var run Run
	if err = json.Unmarshal(data, &run); err != nil {
		t.Fatal(err)
	}
	run.Regular.ActionInstructions, err = loadActionInstructions("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"product-review", "start-readiness", "development-worker", "blackbox-design", "blackbox-review"} {
		var selected protocol.RegularExecution
		for _, x := range run.Executions {
			if string(x.Claim.Task.Step) == step && x.Claim.ActionID > selected.Claim.ActionID {
				selected = x
			}
		}
		if selected.Claim.ActionID == "" {
			continue
		}
		var m nativePromptMaterial
		if preparationAction(step) {
			m, err = preparePreparationPrompt(run, selected.Claim)
		} else {
			m, err = prepareReviewPrompt(run, selected.Claim)
		}
		if err != nil {
			t.Fatal(step, err)
		}
		job := filepath.Join(t.TempDir(), step)
		if err = m.bindFiles(job, run.Regular.Requirement); err != nil {
			t.Fatal(err)
		}
		if step == "blackbox-review" {
			sourcePath := os.Getenv("FORMAL_GATES_PROMPT_READONLY_SOURCES")
			if sourcePath != "" {
				b, e := os.ReadFile(sourcePath)
				if e != nil {
					t.Fatal(e)
				}
				var sources []SubagentSource
				if e = json.Unmarshal(b, &sources); e != nil {
					t.Fatal(e)
				}
				m.bindPublicSources(job, sources)
			}
		}
		prompt, e := m.render(selected.Claim)
		if e != nil {
			t.Fatal(e)
		}
		if len(prompt) > 16000 {
			t.Fatalf("%s unexpectedly large input: %d bytes", step, len(prompt))
		}
		if err = os.MkdirAll(job, 0700); err != nil {
			t.Fatal(err)
		}
		if err = m.writeFiles(job); err != nil {
			t.Fatal(err)
		}
		for name, want := range m.files {
			got, e := os.ReadFile(filepath.Join(job, name))
			if e != nil || string(got) != string(want) {
				t.Fatal("unreadable material", name, e)
			}
		}
		t.Logf("%s initial_chars=%d material_files=%d", step, len([]rune(prompt)), len(m.files))
	}
}
