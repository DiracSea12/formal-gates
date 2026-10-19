package panel

import (
	"reflect"
	"testing"

	"formal-gates/internal/settings"
)

func TestPanelSaveComparesLockedNodesToLibraryValue(t *testing.T) {
	home := panelHome(t)
	root, _, run := panelRun(t, home)
	if !nodeState(run.NodeStates, "repair").Completed {
		t.Fatal("fixture must have a completed repair responsibility")
	}
	_, err := settings.EditConfiguration(root, home, "project", "工作", nil, func(configuration *settings.Configuration) error {
		configuration.Execution = map[string]settings.ExecutionChoice{
			"repair": {Provider: "zcode", Model: "library-repair"},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	backend := Backend{Root: root, Home: home}
	state, err := backend.State()
	if err != nil {
		t.Fatal(err)
	}
	var candidate settings.Configuration
	for _, option := range state.Configurations {
		if option.Name == "工作" {
			candidate = settings.CloneConfiguration(option.Configuration)
		}
	}
	if candidate.Execution["repair"].Model != "library-repair" {
		t.Fatal("panel did not read the current library")
	}
	candidate.Execution["gate"] = settings.ExecutionChoice{Provider: "zcode", Model: "next-gate"}
	if _, err := backend.Save("project", "工作", candidate, true, state.Revisions, run.RunID); err != nil {
		t.Fatalf("changing only the editable gate rejected the unchanged locked library value: %v", err)
	}
}

func TestPanelStateRevisionsDescribePublishedValuesDuringEdits(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	configuration := settings.DefaultConfiguration()
	if _, err := settings.WriteConfiguration(root, home, "project", "work", configuration, false, nil); err != nil {
		t.Fatal(err)
	}
	backend := Backend{Root: root, Home: home}
	stop := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		for index := 0; ; index++ {
			select {
			case <-stop:
				finished <- nil
				return
			default:
			}
			configuration.Execution = map[string]settings.ExecutionChoice{
				"gate": {Provider: "zcode", Model: "first"},
			}
			if index%2 != 0 {
				configuration.Execution["gate"] = settings.ExecutionChoice{Provider: "zcode", Model: "second"}
			}
			if _, err := settings.WriteConfiguration(root, home, "project", "work", configuration, true, nil); err != nil {
				finished <- err
				return
			}
		}
	}()
	defer func() {
		close(stop)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	for index := 0; index < 500; index++ {
		state, err := backend.State()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(state.Revisions, state.Effective.Revisions) {
			t.Fatalf("published revisions do not describe the published configuration bytes: values=%v revisions=%v", state.Effective.Revisions, state.Revisions)
		}
	}
}
