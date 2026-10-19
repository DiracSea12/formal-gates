package facade

import (
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
	"os"
	"path/filepath"
	"reflect"
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

func configureLightweight(t *testing.T, root string) {
	t.Helper()
	configureFlowNodes(t, root, protocol.FlowNodeIDs()...)
}

func TestConfigurationRouteUsesEnabledResponsibilities(t *testing.T) {
	for _, test := range []struct {
		name     string
		disabled []string
		modes    []string
		route    string
	}{
		{"default", nil, nil, "regular"},
		{"all-off", []string{"product-review", "start-readiness", "development", "qa", "gate", "repair"}, nil, "lightweight"},
		{"review-only", []string{"start-readiness", "development", "qa", "gate", "repair"}, nil, "regular"},
		{"qa-without-development", []string{"development", "gate", "repair"}, []string{"whitebox"}, ""},
		{"gate-without-development", []string{"development", "qa", "repair"}, nil, ""},
		{"repair-without-development", []string{"development", "qa", "gate"}, nil, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			route, err := ConfigurationRoute(settings.Configuration{FlowSelection: &settings.FlowSelection{Disabled: test.disabled}, QAModes: test.modes})
			if route != test.route || (err != nil) != (test.route == "") {
				t.Fatalf("route=%q error=%v", route, err)
			}
		})
	}
}

func TestStartDerivesLightweightFromAllDisabledConfiguration(t *testing.T) {
	root := t.TempDir()
	admission := testAdmission(t, root, "configured-start", "sha256:package", DefaultProvider)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("A plain material note."), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareIntake(root, "note.md", "", nil, false)
	if err != nil || preview.RequirementDocument != nil {
		t.Fatal("unstructured intake did not retain a lightweight candidate", err)
	}
	if _, err := settings.SaveConfiguration(root, home, "project", "轻量", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(root, home, "project", "轻量", nil, func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: []string{"product-review", "start-readiness", "development", "qa", "gate", "repair"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	confirmations := 0
	options := StartOptions{Root: root, ArtifactRoot: root, Configuration: "轻量", Admission: admission, Request: StartRequest{RunID: "derived-start", IntakeConfirmationReceipt: preview.IntakeCandidate}, Confirm: func(value any) error {
		confirmations++
		if value.(map[string]any)["route"] != "lightweight" {
			t.Fatal("confirmation did not show the derived route")
		}
		return nil
	}}
	f, run, err := Start(options)
	if err != nil || run.Route != "lightweight" || confirmations != 1 {
		t.Fatal("all-disabled startup was not derived and confirmed", run.Route, confirmations, err)
	}
	if done, err := f.Drive(); err != nil || done.Status != "COMPLETE" {
		t.Fatal("all-disabled run did not finish the lightweight path", done.Status, err)
	}
	shown, err := f.Show()
	if err != nil || shown.Configuration.Name != "轻量" || !reflect.DeepEqual(shown.FlowSelection, shown.Configuration.Configuration.FlowSelection) {
		t.Fatal("terminal view lost the bound node configuration", err)
	}
	options.Request.RunID, options.Request.Route = "mismatched-start", "regular"
	if _, _, err := Start(options); err == nil || !strings.Contains(err.Error(), "differs") || confirmations != 1 {
		t.Fatal("explicit inconsistent route reached confirmation", confirmations, err)
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
		t.Fatal("strict regular confirmation accepted unstructured material")
	}
}
