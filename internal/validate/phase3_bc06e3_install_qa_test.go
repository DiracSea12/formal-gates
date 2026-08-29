package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
)

const bc06InstallManifest = `{"name":"formal-gates","hosts":[{"name":"Claude Code","support":"host-target"}]}` + "\n"

func bc06InstallWriteFile(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func bc06InstallSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	bc06InstallWriteFile(t, filepath.Join(root, "SKILL.md"), "---\nname: formal-gates\ndescription: bc06 test fixture\n---\n"+hostInstructionsStartMarker+"\nbc06 managed rule\n"+hostInstructionsEndMarker+"\n", 0o600)
	bc06InstallWriteFile(t, filepath.Join(root, "README.md"), "runtime fixture\n", 0o600)
	bc06InstallWriteFile(t, filepath.Join(root, "README_EN.md"), "runtime fixture\n", 0o600)
	bc06InstallWriteFile(t, filepath.Join(root, "formal-gates.manifest.json"), bc06InstallManifest, 0o600)
	bc06InstallWriteFile(t, filepath.Join(root, "bin", nativeBinaryName()), "#!/bin/sh\nexit 0\n", 0o700)
	for _, entry := range []string{"agents/agent.md", "prompts/action.md", "gates/gate.md", "references/reference.md"} {
		bc06InstallWriteFile(t, filepath.Join(root, filepath.FromSlash(entry)), entry+"\n", 0o600)
	}
	return root
}

func bc06InstallLegacyActiveState(t *testing.T, project, runID string) string {
	t.Helper()
	state := RunState{RunID: runID, Status: "ACTIVE"}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, ".gates", "tmp", runID, "state.json")
	bc06InstallWriteFile(t, path, string(data)+"\n", 0o600)
	return path
}

func bc06InstallCandidateActiveState(t *testing.T, project, runID string) string {
	t.Helper()
	receipt := facade.IntakeConfirmationReceipt{
		Source:              facade.DefaultIntakeSource,
		Authority:           facade.DefaultIntakeAuthority,
		Transport:           facade.DefaultIntakeTransport,
		RequirementSource:   "requirements.md",
		RequirementRevision: "req-bc06-install",
		Artifacts:           []facade.IntakeArtifact{{Path: "requirements.md", Revision: "req-bc06-install"}},
		SolutionRevision:    "sol-bc06-install",
		SolutionDigest:      "sha256:sol-bc06-install",
	}
	_, run, err := facade.Start(facade.StartOptions{
		Root: project,
		Request: facade.StartRequest{
			RunID:                     runID,
			Route:                     "lightweight",
			DefinitionSource:          facade.DefaultDefinitionSource,
			DefinitionDigest:          definition.WorkflowDefinitionDigest,
			IntakeConfirmationReceipt: receipt,
		},
		Admission: &facade.Admission{PackageDigest: "sha256:bc06-candidate", InstalledTargetIdentity: "bc06-candidate-target"},
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

// TestPhase3BC06InstallFencesActiveLegacyAndCandidateRuns verifies that an
// install which would replace the registered runtime is rejected while either
// runtime has an active run. The candidate fixture is produced through the
// façade so its nested content.phase is the real engine state shape; the
// legacy fixture uses the documented ACTIVE RunState shape.
func TestPhase3BC06InstallFencesActiveLegacyAndCandidateRuns(t *testing.T) {
	cases := []struct {
		name       string
		runID      string
		makeActive func(*testing.T, string, string) string
	}{
		{name: "legacy-active", runID: "legacy-active", makeActive: bc06InstallLegacyActiveState},
		{name: "candidate-intake", runID: "candidate-intake", makeActive: bc06InstallCandidateActiveState},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			project := t.TempDir()
			statePath := testCase.makeActive(t, project, testCase.runID)
			beforeState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			source := bc06InstallSource(t)
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
			_, err = Install(InstallOptions{
				Source: source, Host: "claude", Scope: "project", Project: project,
				RegistryPath: registry, BinaryTarget: launcher, Force: true, SkipHooks: true,
			})
			if err == nil {
				t.Error("install unexpectedly changed registry while an active run existed")
			} else {
				for _, want := range []string{"active workflow run", testCase.runID, "fences install"} {
					if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
						t.Errorf("install error=%q, want substring %q", err, want)
					}
				}
			}
			afterState, readErr := os.ReadFile(statePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(afterState) != string(beforeState) {
				t.Fatal("active run state changed during rejected install")
			}
			for _, path := range []string{registry, filepath.Join(project, ".claude", "skills", "formal-gates"), launcher} {
				if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
					t.Errorf("rejected install left mutation at %s: %v", path, statErr)
				}
			}
		})
	}
}
