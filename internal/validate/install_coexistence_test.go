package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/settings"
)

const coexistenceManifest = `{"name":"formal-gates","package_parts":["SKILL.md","README.md","README_EN.md","formal-gates.manifest.json","bin/","agents/","prompts/","gates/","references/","internal/harness/native_dsh.mjs","internal/harness/zcode_credential.mjs"],"hosts":[{"name":"Claude Code","support":"host-target"}]}` + "\n"

func coexistenceWriteFile(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func coexistenceSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	coexistenceWriteFile(t, filepath.Join(root, "SKILL.md"), "---\nname: formal-gates\ndescription: install coexistence fixture\n---\n"+hostInstructionsStartMarker+"\ncoexistence managed rule\n"+hostInstructionsEndMarker+"\n", 0o600)
	coexistenceWriteFile(t, filepath.Join(root, "README.md"), "runtime fixture\n", 0o600)
	coexistenceWriteFile(t, filepath.Join(root, "README_EN.md"), "runtime fixture\n", 0o600)
	coexistenceWriteFile(t, filepath.Join(root, "formal-gates.manifest.json"), coexistenceManifest, 0o600)
	coexistenceWriteFile(t, filepath.Join(root, "bin", nativeBinaryName()), string(stubBinaryPayload(t)), 0o700)
	for _, entry := range []string{"agents/agent.md", "prompts/action.md", "gates/gate.md", "references/reference.md"} {
		coexistenceWriteFile(t, filepath.Join(root, filepath.FromSlash(entry)), entry+"\n", 0o600)
	}
	for _, name := range []string{"native_dsh.mjs", "zcode_credential.mjs"} {
		copyValidateTestFile(t, repoRootValidateTest(t), root, "internal/harness/"+name)
	}
	return root
}

func coexistenceLegacyActiveState(t *testing.T, project, runID string) string {
	t.Helper()
	state := map[string]string{"runId": runID, "status": "ACTIVE"}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, ".gates", "tmp", runID, "state.json")
	coexistenceWriteFile(t, path, string(data)+"\n", 0o600)
	return path
}

func coexistenceCandidateActiveState(t *testing.T, project, runID string) string {
	t.Helper()
	home := t.TempDir()
	name, err := settings.SaveConfiguration(project, home, "project", "coexistence", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(project, home, "project", name, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "requirements.md"), []byte(testRequirementDocument("Sample requirement.")), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := facade.IntakeConfirmationReceipt{
		Source:              facade.DefaultIntakeSource,
		Authority:           facade.DefaultIntakeAuthority,
		Transport:           facade.DefaultIntakeTransport,
		RequirementSource:   "requirements.md",
		RequirementRevision: "req-coexistence",
		Artifacts:           []facade.IntakeArtifact{{Path: "requirements.md", Revision: "req-coexistence"}},
		SolutionRevision:    "sol-coexistence",
		SolutionDigest:      "sha256:sol-coexistence",
	}
	admission := engineAdmissionFixture(t, project, "sha256:coexistence-candidate", "coexistence-candidate-target")
	source := repoRootValidateTest(t)
	for _, directory := range []string{"prompts", "gates"} {
		if err := filepath.WalkDir(filepath.Join(source, directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				relative, err := filepath.Rel(source, path)
				if err != nil {
					return err
				}
				copyValidateTestFile(t, source, admission.Target, relative)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	_, run, err := facade.Start(facade.StartOptions{
		Root:      project,
		VCSRunner: testkit.NewFrozenGitRepo(),
		Request: facade.StartRequest{
			RunID:                     runID,
			BaseSnapshot:              testkit.FrozenSnapshot,
			DefinitionSource:          facade.DefaultDefinitionSource,
			DefinitionDigest:          definition.WorkflowDefinitionDigest,
			IntakeConfirmationReceipt: receipt,
		},
		Admission: admission,
	})
	if err != nil {
		t.Fatalf("create candidate fixture: %v", err)
	}
	if run.Status != "ACTIVE" {
		t.Fatalf("candidate fixture status=%q, want ACTIVE", run.Status)
	}
	path := filepath.Join(project, filepath.FromSlash(facade.EngineNamespace), runID, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Content struct {
			Phase string `json:"phase"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatalf("decode candidate state: %v", err)
	}
	if probe.Content.Phase != "INTAKE_REGISTERED" {
		t.Fatalf("candidate fixture content.phase=%q, want INTAKE_REGISTERED", probe.Content.Phase)
	}
	return path
}

// TestInstallAllowsActiveLegacyAndCandidateRuns verifies that an
// install can replace the registered runtime while either state shape has an
// active run, without rewriting that run. The candidate fixture is produced
// through the façade so its nested content.phase is the real engine state
// shape; the unsupported fixture is inert raw JSON and never opens a writer.
func TestInstallAllowsActiveLegacyAndCandidateRuns(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		runID      string
		makeActive func(*testing.T, string, string) string
	}{
		{name: "legacy-active", runID: "legacy-active", makeActive: coexistenceLegacyActiveState},
		{name: "candidate-intake", runID: "candidate-intake", makeActive: coexistenceCandidateActiveState},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			project := t.TempDir()
			statePath := testCase.makeActive(t, project, testCase.runID)
			beforeState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			source := coexistenceSource(t)
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
			report, err := Install(InstallOptions{
				Source: source, Host: "claude", Scope: "project", Project: project,
				RegistryPath: registry, BinaryTarget: launcher, Force: true, SkipHooks: true,
			})
			if err != nil {
				t.Fatalf("install with active run %s failed: %v", testCase.runID, err)
			}
			if len(report.Targets) != 1 || report.Targets[0].Smoke != "PASS" {
				t.Fatalf("install report=%#v", report)
			}
			afterState, readErr := os.ReadFile(statePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(afterState) != string(beforeState) {
				t.Fatal("install changed the active run state")
			}
			if testCase.name == "legacy-active" {
				if _, err := facade.Open(project, testCase.runID); err == nil {
					t.Fatal("unsupported raw state opened as a current run")
				}
			}
			for _, path := range []string{registry, filepath.Join(project, ".claude", "skills", "formal-gates"), launcher} {
				if _, statErr := os.Stat(path); statErr != nil {
					t.Errorf("successful install did not create %s: %v", path, statErr)
				}
			}
		})
	}
}
