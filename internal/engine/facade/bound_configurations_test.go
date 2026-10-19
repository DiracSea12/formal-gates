package facade

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

func TestBoundConfigurationsListUnfinishedRunsUnderTheirMaster(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	admission := testAdmission(t, root, "candidate", "sha256:package", host.Default)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := settings.SaveConfiguration(root, home, "project", "工作", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(root, home, "project", "工作", nil, func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: []string{"start-readiness", "development", "qa", "gate", "repair"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	master := nativeMasterAt(t, root, base, nil)
	passPreDevelopmentReviews(t, master, splitCommandPlan())
	master.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})})
	_, child, err := createdSliceFacade(t, master.f, "lane-core", OwnerBinding{})
	if err != nil || child.MasterRunID != master.f.runID {
		t.Fatal("slice child was not created under its master", child.MasterRunID, err)
	}
	reopened, err := Open(root, child.RunID)
	if err == nil {
		child, err = reopened.Show()
	}
	if err != nil || child.Status == "COMPLETE" || child.Status == "ABORTED" {
		t.Fatal("slice child is not an unfinished run the engine can open", child.Status, err)
	}
	start := func(id string) *Facade {
		t.Helper()
		request := StartRequest{RunID: id, BaseSnapshot: base, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}
		f, _, err := Start(StartOptions{Root: root, Configuration: "工作", Request: request, Admission: admission})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	start("named-run")
	finished := start("finished-run")
	if run, err := abortVerifiedRun(t, finished); err != nil || run.Status != "ABORTED" {
		t.Fatal("run did not reach its terminal state", run.Status, err)
	}
	broken := filepath.Join(root, EngineNamespace, "broken-run")
	if err := os.MkdirAll(broken, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "request.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	bound, err := BoundConfigurations(root)
	want := map[string][]string{driverState(t, master).ConfigurationName(): {master.f.runID}, "工作": {"named-run"}}
	if err != nil || !reflect.DeepEqual(bound, want) {
		t.Fatalf("bound configurations = %v, want %v (%v)", bound, want, err)
	}
}
