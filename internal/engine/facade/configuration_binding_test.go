package facade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/settings"
)

func configurationBindingIntake(t *testing.T, root string) IntakeConfirmationReceipt {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareIntake(root, "requirements.md", "", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return preview.IntakeCandidate
}

func waitForConfigurationWindow(t *testing.T, path string, finished <-chan error) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case err := <-finished:
			t.Fatalf("operation did not wait in its configuration window: %v", err)
		case <-deadline.C:
			t.Fatal("operation did not acquire its configuration window")
		case <-ticker.C:
			release, err := coordination.AcquirePath(path, "observe configuration window")
			if err == nil {
				release()
				continue
			}
			var held *coordination.LockHeldError
			if !errors.As(err, &held) {
				t.Fatal(err)
			}
			return
		}
	}
}

func TestConfigurationChangesRejectACommittingStart(t *testing.T) {
	for _, operation := range []string{"delete", "rename"} {
		t.Run(operation, func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			admission := testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)
			configureLightweight(t, root)
			effective, err := settings.Load(root, home)
			if err != nil {
				t.Fatal(err)
			}
			name := effective.ConfigurationName
			receipt := configurationBindingIntake(t, root)
			releaseAdmission, err := coordination.AcquirePath(admission.RegistryPath+".lock", "installation write")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if releaseAdmission != nil {
					releaseAdmission()
				}
			}()
			started := make(chan error, 1)
			go func() {
				_, _, err := Start(StartOptions{Root: root, ArtifactRoot: root, Configuration: name, Admission: admission, Request: StartRequest{
					RunID: "binding-start", IntakeConfirmationReceipt: receipt,
				}})
				started <- err
			}()
			userLock := filepath.Join(home, ".config", "formal-gates", "config.json.lock")
			waitForConfigurationWindow(t, userLock, started)
			changed := make(chan error, 1)
			go func() {
				resolveBindings := func() (map[string][]string, error) { return BoundConfigurations(root) }
				var err error
				if operation == "delete" {
					_, err = settings.DeleteConfiguration(root, home, "project", name, resolveBindings, effective.Revisions)
				} else {
					_, err = settings.RenameConfiguration(root, home, "project", name, "新名称", resolveBindings, effective.Revisions)
				}
				changed <- err
			}()
			releaseAdmission()
			releaseAdmission = nil
			select {
			case err := <-started:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("startup deadlocked with configuration edit")
			}
			select {
			case err := <-changed:
				if err == nil || !strings.Contains(err.Error(), "bound to unfinished runs binding-start") {
					t.Fatalf("%s did not reject the committed binding: %v", operation, err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("configuration edit deadlocked with startup")
			}
			reopened, err := Open(root, "binding-start")
			if err != nil {
				t.Fatal(err)
			}
			run, err := reopened.Show()
			if err != nil || run.Status != "ACTIVE" || run.Configuration.Name != name {
				t.Fatalf("bound run changed: status=%s binding=%q error=%v", run.Status, run.Configuration.Name, err)
			}
			bindings, err := BoundConfigurations(root)
			if err != nil || !reflect.DeepEqual(bindings, map[string][]string{name: {"binding-start"}}) {
				t.Fatalf("unexpected unfinished bindings: %v %v", bindings, err)
			}
			for boundName := range bindings {
				if _, err := configurationLibrary(root)(boundName); err != nil {
					t.Fatal("unfinished binding is not in the library", err)
				}
			}
		})
	}
}

func TestConfigurationChangesPrecedeAConfirmedWaitingStart(t *testing.T) {
	for _, operation := range []string{"delete", "rename"} {
		t.Run(operation, func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			admission := testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)
			if _, err := settings.SaveConfiguration(root, home, "project", "工作", "", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := settings.UseConfiguration(root, home, "project", "工作", nil); err != nil {
				t.Fatal(err)
			}
			configureLightweight(t, root)
			options := StartOptions{Root: root, ArtifactRoot: root, Configuration: "工作", Admission: admission, Request: StartRequest{
				RunID: "configured-start", IntakeConfirmationReceipt: configurationBindingIntake(t, root),
			}}
			revisions, err := settings.Revisions(root, home)
			if err != nil {
				t.Fatal(err)
			}
			projectLock := filepath.Join(root, ".gates", "config.json.lock")
			releaseProject, err := coordination.AcquirePath(projectLock, "configuration edit")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if releaseProject != nil {
					releaseProject()
				}
			}()
			changed := make(chan error, 1)
			go func() {
				resolveBindings := func() (map[string][]string, error) { return BoundConfigurations(root) }
				var err error
				if operation == "delete" {
					_, err = settings.DeleteConfiguration(root, home, "project", "工作", resolveBindings, revisions)
				} else {
					_, err = settings.RenameConfiguration(root, home, "project", "工作", "新名称", resolveBindings, revisions)
				}
				changed <- err
			}()
			waitForConfigurationWindow(t, filepath.Join(home, ".config", "formal-gates", "config.json.lock"), changed)
			confirmations := 0
			confirmed := make(chan struct{})
			options.Confirm = func(value any) error {
				confirmations++
				if value.(map[string]any)["route"] != "lightweight" {
					return fmt.Errorf("confirmation lost the selected route")
				}
				close(confirmed)
				return nil
			}
			started := make(chan error, 1)
			go func() {
				_, _, err := Start(options)
				started <- err
			}()
			select {
			case <-confirmed:
			case err := <-started:
				t.Fatalf("startup failed before confirmation: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("startup waited for overlays before user confirmation")
			}
			releaseProject()
			releaseProject = nil
			select {
			case err := <-changed:
				if err != nil {
					t.Fatal("configuration change failed", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("configuration change deadlocked with startup")
			}
			select {
			case err = <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("startup deadlocked with configuration change")
			}
			if err == nil || !strings.Contains(err.Error(), "工作") || !strings.Contains(err.Error(), "does not exist") || confirmations != 1 {
				t.Fatalf("stale startup was not rejected: confirmations=%d error=%v", confirmations, err)
			}
			for _, path := range []string{filepath.Join(root, EngineNamespace, options.Request.RunID), startIntentPath(root, options.Request.RunID)} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("rejected startup left %s: %v", path, err)
				}
			}
			if _, err := settings.SaveConfiguration(root, home, "project", "工作", "", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := settings.UseConfiguration(root, home, "project", "工作", nil); err != nil {
				t.Fatal(err)
			}
			configureLightweight(t, root)
			options.Confirm = func(any) error { confirmations++; return nil }
			f, run, err := Start(options)
			if err != nil || run.Status != "ACTIVE" || run.Configuration.Name != "工作" || confirmations != 2 {
				t.Fatalf("same-path restart failed: status=%s binding=%q confirmations=%d error=%v", run.Status, run.Configuration.Name, confirmations, err)
			}
			snapshot, err := f.engine.Load()
			if err != nil || snapshot.State.ConfigurationName() != "工作" || len(snapshot.State.AppliedConfigurations) == 0 {
				t.Fatal("startup did not persist its binding and initial application", err)
			}
		})
	}
}

func TestStartRechecksRouteAndInitialApplicationAfterConfirmation(t *testing.T) {
	for _, changeRoute := range []bool{false, true} {
		t.Run(fmt.Sprintf("route-changed=%t", changeRoute), func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			admission := testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)
			configureLightweight(t, root)
			effective, err := settings.Load(root, home)
			if err != nil {
				t.Fatal(err)
			}
			updated := settings.CloneConfiguration(effective.Configuration)
			updated.RoundLimits = map[string]int{"product-review": 5}
			if changeRoute {
				updated.FlowSelection = nil
			}
			options := StartOptions{Root: root, ArtifactRoot: root, Configuration: effective.ConfigurationName, Admission: admission, Request: StartRequest{
				RunID: "confirmed-start", IntakeConfirmationReceipt: configurationBindingIntake(t, root),
			}, Confirm: func(any) error {
				_, err := settings.EditConfiguration(root, home, "project", effective.ConfigurationName, nil, func(configuration *settings.Configuration) error {
					*configuration = updated
					return nil
				})
				if err != nil {
					return err
				}
				fields, err := settings.EditableFields(effective)
				if err != nil {
					return err
				}
				for _, field := range fields {
					if field.Name == "capacity" {
						_, err = settings.EditField(root, home, "project", field.Position, "12", false, nil)
						return err
					}
				}
				return fmt.Errorf("capacity field is missing")
			}}
			f, _, err := Start(options)
			if changeRoute {
				if err == nil || !strings.Contains(err.Error(), "route changed") {
					t.Fatal("changed route reached startup commit", err)
				}
				if _, err := os.Stat(filepath.Join(root, EngineNamespace, options.Request.RunID)); !os.IsNotExist(err) {
					t.Fatal("changed route left run state", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(snapshot.State.AppliedConfigurations[0].Configuration, updated) ||
				snapshot.State.Settings.Values.Capacity != effective.Values.Capacity {
				t.Fatal("startup lost the current initial application or changed frozen values")
			}
		})
	}
}

func TestRegularStartConfirmsBeforeBindingItsConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	original, root := workerMaterialFacade(t)
	request, err := original.request()
	if err != nil {
		t.Fatal(err)
	}
	request.RunID, request.FlowSelection = "confirmed-regular", nil
	confirmations := 0
	f, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Configuration: "工作", Admission: &original.admission, Request: request, Confirm: func(value any) error {
		confirmations++
		if value.(map[string]any)["route"] != "regular" {
			return fmt.Errorf("confirmation lost the regular route")
		}
		for _, path := range []string{
			filepath.Join(home, ".config", "formal-gates", "config.json.lock"),
			filepath.Join(root, ".gates", "config.json.lock"),
		} {
			release, err := coordination.AcquirePath(path, "confirmation remains unlocked")
			if err != nil {
				return err
			}
			release()
		}
		_, err := settings.EditConfiguration(root, home, "project", "工作", nil, func(configuration *settings.Configuration) error {
			configuration.RoundLimits = map[string]int{"product-review": 5}
			return nil
		})
		return err
	}})
	if err != nil || confirmations != 1 || run.Status != "ACTIVE" || run.Configuration.Name != "工作" {
		t.Fatalf("regular configuration startup failed: confirmations=%d status=%s binding=%q error=%v", confirmations, run.Status, run.Configuration.Name, err)
	}
	snapshot, err := f.engine.Load()
	if err != nil || snapshot.State.ConfigurationName() != "工作" || snapshot.State.AppliedConfigurations[0].Configuration.RoundLimit("product-review") != 5 {
		t.Fatal("regular startup did not persist the current initial application", err)
	}
}

func TestStartRequiresAConfigurationBindingName(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	configureLightweight(t, root)
	effective, err := settings.Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	effective.ConfigurationName = ""
	_, _, err = Start(StartOptions{Root: root, Settings: &effective, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider), Request: StartRequest{
		RunID: "unnamed-start", IntakeConfirmationReceipt: configurationBindingIntake(t, root),
	}})
	if err == nil || !strings.Contains(err.Error(), "configuration binding name is required") {
		t.Fatal("startup accepted an empty configuration binding", err)
	}
	if _, err := os.Stat(filepath.Join(root, EngineNamespace, "unnamed-start")); !os.IsNotExist(err) {
		t.Fatal("unnamed startup left run state", err)
	}
}
