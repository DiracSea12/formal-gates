package facade

import (
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/settings"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configureFlowNodes(t *testing.T, root string, disabled ...string) {
	t.Helper()
	ensureEditableConfiguration(t, root)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	effective, err := settings.Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(root, home, "project", effective.ConfigurationName, nil, func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: disabled}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// verifiedStartFixture 提供可启动的最小夹具：一个命名的可编辑配置（默认全
// 部节点启用）与 requirements.md 承载的确认需求文档。Start 另需调用方在
// StartOptions 注入 Git runner 与 BaseSnapshot。
func verifiedStartFixture(t *testing.T, root string) {
	t.Helper()
	ensureEditableConfiguration(t, root)
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestStartConfigurationValidationCoversEnabledNodeCombinations(t *testing.T) {
	for _, test := range []struct {
		name     string
		disabled []string
		modes    []string
		wantErr  string
	}{
		{"default", nil, nil, ""},
		{"all-off", []string{"product-review", "start-readiness", "development", "qa", "gate", "repair"}, nil, "at least one enabled"},
		{"review-only", []string{"start-readiness", "development", "qa", "gate", "repair"}, nil, ""},
		{"qa-without-development", []string{"development", "gate", "repair"}, []string{"whitebox"}, "require an enabled development"},
		{"gate-without-development", []string{"development", "qa", "repair"}, nil, "require an enabled development"},
		{"repair-without-development", []string{"development", "qa", "gate"}, nil, "require an enabled development"},
		{"qa-without-modes", []string{"product-review", "start-readiness", "development", "gate", "repair"}, nil, "at least one enabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateStartConfiguration(settings.Configuration{FlowSelection: &settings.FlowSelection{Disabled: test.disabled}, QAModes: test.modes})
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("valid combination rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestStartRejectsAllDisabledConfigurationBeforeRunWrite(t *testing.T) {
	root := t.TempDir()
	admission := testAdmission(t, root, "configured-start", "sha256:package", DefaultProvider)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.SaveConfiguration(root, home, "project", "全禁", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(root, home, "project", "全禁", nil, func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: []string{"product-review", "start-readiness", "development", "qa", "gate", "repair"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	options := StartOptions{Root: root, ArtifactRoot: root, Configuration: "全禁", Admission: admission, VCSRunner: testkit.NewFrozenGitRepo(), Request: StartRequest{RunID: "rejected-start", BaseSnapshot: testkit.FrozenSnapshot, IntakeConfirmationReceipt: preview.IntakeCandidate}}
	f, run, err := Start(options)
	if err == nil || !strings.Contains(err.Error(), "at least one enabled") || f != nil || run.RunID != "" {
		t.Fatal("all-disabled configuration started a run", run, err)
	}
	if _, statErr := os.Stat(filepath.Join(root, EngineNamespace)); !os.IsNotExist(statErr) {
		t.Fatalf("rejected start created engine namespace: %v", statErr)
	}
}

func TestIntakeAlwaysProjectsStructuredRequirementWhenAvailable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareIntake(root, "requirements.md", "", nil, false)
	if err != nil || preview.RequirementDocument == nil || len(preview.RequirementDocument.Requirements) != 1 {
		t.Fatal("precheck skipped structured projection", err)
	}
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte("Plain material"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareIntake(root, "requirements.md", "", nil, true); err == nil {
		t.Fatal("strict precheck accepted unstructured material")
	}
}
