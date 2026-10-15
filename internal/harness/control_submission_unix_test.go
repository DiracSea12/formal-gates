//go:build !windows

package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

func splitControlWorkflow(t *testing.T) (*facade.Facade, *codexScenario, string) {
	t.Helper()
	master, _, scenario, binary := engineNativeFixture(t, true)
	stageNativeReviews(t, master, scenario, "core", "tooling", "master-merge")
	if requested, err := master.RequestWorkflowChoice(); err != nil || !requested {
		t.Fatal("engine did not issue its actual topology Ask", err)
	}
	run, err := master.Show()
	if err != nil {
		t.Fatal(err)
	}
	askPosition := func(t *testing.T, run facade.Run, control protocol.ControlKind) int {
		t.Helper()
		for index, ask := range run.PendingAsks {
			if ask.Control == control {
				return index + 1
			}
		}
		t.Fatalf("missing actual %s Ask", control)
		return 0
	}
	if run.Review.ReadinessProposal == nil || run.Validation == nil {
		t.Fatal("original readiness plan and acceptance conditions are required")
	}
	var proposed []string
	pointOwners := map[string]string{}
	for _, batch := range run.Review.ReadinessProposal.Batches {
		if batch.Owner != "" && batch.Owner != "master-merge" && !slices.Contains(proposed, batch.Owner) {
			proposed = append(proposed, batch.Owner)
		}
		for _, point := range batch.RequirementPoints {
			if owner, exists := pointOwners[point]; exists && owner != batch.Owner {
				t.Fatal("original plan has ambiguous acceptance ownership", point)
			}
			pointOwners[point] = batch.Owner
		}
	}
	var owners []int
	for _, requirement := range run.Validation.Document.Requirements {
		for _, condition := range requirement.AcceptanceConditions {
			owner := pointOwners[condition.ID]
			if owner == "" {
				t.Fatal("original plan has no acceptance owner", condition.ID)
			}
			position := 0
			if owner != "master-merge" {
				position = slices.Index(proposed, owner) + 1
			}
			owners = append(owners, position)
		}
	}
	topology, err := master.PrepareControl(facade.ControlInput{Ask: askPosition(t, run, protocol.ControlConfirmTopology), Choice: "split", Parallel: "Two independent deliveries", Owners: owners})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := confirmFacadeFixtureControl(t, master, topology); err != nil {
		t.Fatal(err)
	}
	run, err = master.Drive()
	if err != nil || len(run.SliceChildren) != 2 {
		t.Fatal("Controller did not create the actual original children", err)
	}
	return master, scenario, binary
}

func TestWorkflowAskSelectionBindsActualChild(t *testing.T) {
	master, scenario, binary := splitControlWorkflow(t)
	run, err := master.Show()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(run.SliceChildren))
	for _, id := range run.SliceChildren {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		child, err := facade.Open(scenario.workspace, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := child.Drive(); err != nil {
			t.Fatal(err)
		}
		if requested, err := child.RequestWorkflowChoice(); err != nil || !requested {
			t.Fatal("child did not issue its actual route Ask", err)
		}
	}
	current, err := master.ControlRuns()
	if err != nil || len(current) != 3 || len(current[1].PendingAsks) != 1 || len(current[2].PendingAsks) != 1 {
		t.Fatal("current query did not contain both real child Asks", err)
	}
	target := current[2]
	number := len(current[0].PendingAsks) + 2
	files := func() map[string]string {
		t.Helper()
		all := map[string]string{}
		err := filepath.WalkDir(filepath.Join(scenario.workspace, facade.EngineNamespace), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			body, err := os.ReadFile(path)
			all[path] = string(body)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return all
	}
	invoke := func(input string, args ...string) ([]byte, error) {
		command := exec.Command(binary, append([]string{"workflow"}, args...)...)
		command.Stdin = strings.NewReader(input)
		return command.CombinedOutput()
	}
	before := files()
	for _, entry := range []string{"show", "status", "next"} {
		body, err := invoke("", entry, "--root", scenario.workspace, "--run-id", run.RunID)
		if err != nil {
			t.Fatalf("%s: %v %s", entry, err, body)
		}
		var view struct {
			Children []struct {
				Run struct {
					RunID     string
					Questions []struct {
						Ask     int
						Control protocol.ControlKind
					}
				}
			}
		}
		if err := json.Unmarshal(body, &view); err != nil {
			t.Fatal(err)
		}
		if len(view.Children) != 2 || view.Children[1].Run.RunID != target.RunID || len(view.Children[1].Run.Questions) != 1 || view.Children[1].Run.Questions[0].Ask != number || view.Children[1].Run.Questions[0].Control != protocol.ControlSelectRoute {
			t.Fatalf("%s did not display the shared actual target: %s", entry, body)
		}
	}
	if !reflect.DeepEqual(before, files()) {
		t.Fatal("pure control projection wrote workflow evidence")
	}
	for _, input := range []string{`{"ask":99,"choice":"full"}`, `{"ask":2,"choice":"unoffered"}`} {
		if body, err := invoke(input, "next", "--root", scenario.workspace, "--run-id", run.RunID, "--decision", "-"); err == nil {
			t.Fatalf("invalid current choice accepted: %s", body)
		}
	}
	input, err := json.Marshal(facade.ControlInput{Ask: number, Choice: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(t.TempDir(), "confirmation.json"), filepath.Join(t.TempDir(), "confirmation.json")}
	var wg sync.WaitGroup
	results := make(chan error, len(paths))
	for _, path := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := invoke(string(input), "next", "--root", scenario.workspace, "--run-id", run.RunID, "--decision", "-", "--save-candidate", path)
			if err != nil {
				err = fmt.Errorf("%w: %s", err, body)
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var candidate facade.ControlCandidate
	for _, path := range paths {
		body, err := os.ReadFile(path)
		var actual facade.ControlCandidate
		if err != nil || json.Unmarshal(body, &actual) != nil {
			t.Fatal("original candidate unavailable", err)
		}
		if actual.Root != scenario.workspace || actual.Run.RunID != target.RunID || actual.Input.Ask != number || actual.Event.Decide.Request != protocol.RequestID(target.PendingAsks[0].RequestID) || actual.Event.Decide.Token != target.PendingAsks[0].Token || actual.DevelopmentPlan == nil {
			t.Fatal("global selection lost the original target, Ask, token or full plan")
		}
		if candidate.Event.ID != "" && !reflect.DeepEqual(candidate, actual) {
			t.Fatal("parallel preparation changed original binding")
		}
		candidate = actual
	}
	if !reflect.DeepEqual(before, files()) {
		t.Fatal("candidate preparation wrote workflow state")
	}
	body, err := confirmInstalledFixtureControl(t, binary, scenario.workspace, scenario.runID, candidate)
	if err != nil {
		t.Fatalf("original confirmation rejected: %v %s", err, body)
	}
	accepted := files()
	if _, err := facade.Open(scenario.workspace, target.RunID); err != nil {
		t.Fatal(err)
	}
	if body, err := confirmInstalledFixtureControl(t, binary, scenario.workspace, scenario.runID, candidate); err != nil || !reflect.DeepEqual(accepted, files()) {
		t.Fatalf("reopened confirmation replay changed original state: %v %s", err, body)
	}
	remaining, err := master.PrepareCurrentControl(facade.ControlInput{Ask: len(current[0].PendingAsks) + 1, Choice: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(t.TempDir(), "confirmation.json")
	if err := writeAtomicJSON(stalePath, remaining); err != nil {
		t.Fatal(err)
	}
	owner, err := facade.Open(scenario.workspace, remaining.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	request, err := owner.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := confirmFacadeFixtureControl(t, owner, request); err != nil {
		t.Fatal(err)
	}
	_ = fixtureControlPosition(t, scenario.workspace, scenario.runID, remaining)
	prior := files()
	if body, err := confirmInstalledFixtureControl(t, binary, scenario.workspace, scenario.runID, remaining); err == nil || !strings.Contains(string(body), "freshness") || !reflect.DeepEqual(prior, files()) {
		t.Fatalf("stale original candidate changed state: %v %s", err, body)
	}
	if turns := nativePeerTurns(t, scenario); turns != 0 {
		t.Fatalf("control selection executed %d native turns", turns)
	}
}

func TestSavedControlSubmissionTargetsParentAndChild(t *testing.T) {
	master, scenario, binary := splitControlWorkflow(t)
	run, err := master.Show()
	if err != nil {
		t.Fatal(err)
	}
	child, err := facade.Open(scenario.workspace, run.SliceChildren["core"])
	if err != nil {
		t.Fatal(err)
	}
	askPosition := func(t *testing.T, run facade.Run, control protocol.ControlKind) int {
		t.Helper()
		for index, ask := range run.PendingAsks {
			if ask.Control == control {
				return index + 1
			}
		}
		t.Fatalf("missing actual %s Ask", control)
		return 0
	}
	invoke := func(t *testing.T, input string, args ...string) []byte {
		t.Helper()
		command := exec.Command(binary, append([]string{"workflow"}, args...)...)
		command.Stdin = bytes.NewBufferString(input)
		body, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("CLI %v failed: %v: %s", args, err, body)
		}
		return body
	}
	for _, target := range []*facade.Facade{master, child} {
		current, err := target.Show()
		if err != nil {
			t.Fatal(err)
		}
		t.Run(current.RunID, func(t *testing.T) {
			saved := filepath.Join(t.TempDir(), "confirmation.json")
			invoke(t, `{"request":"ABORT"}`, "next", "--root", scenario.workspace, "--run-id", current.RunID, "--decision", "-", "--save-candidate", saved)
			for _, decision := range []bool{false, true} {
				if decision {
					run, err := target.Show()
					if err != nil {
						t.Fatal(err)
					}
					input, err := json.Marshal(facade.ControlInput{Ask: askPosition(t, run, protocol.ControlAbort), Choice: "decline", Reason: "Keep the active workflow"})
					if err != nil {
						t.Fatal(err)
					}
					invoke(t, string(input), "next", "--root", scenario.workspace, "--run-id", current.RunID, "--decision", "-", "--save-candidate", saved)
				}
				body, err := os.ReadFile(saved)
				var candidate facade.ControlCandidate
				if err != nil || json.Unmarshal(body, &candidate) != nil || candidate.Root != scenario.workspace || candidate.Run.RunID != current.RunID {
					t.Fatal("public preparation lost the original target", err)
				}
				if body, err := confirmInstalledFixtureControl(t, binary, scenario.workspace, scenario.runID, candidate); err != nil {
					t.Fatalf("original saved candidate: %v %s", err, body)
				}
				path := filepath.Join(scenario.workspace, facade.EngineNamespace, current.RunID, "state.json")
				accepted, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var state struct {
					Content protocol.State `json:"content"`
				}
				if err := json.Unmarshal(accepted, &state); err != nil {
					t.Fatal(err)
				}
				if _, exists := state.Content.Events[string(candidate.Event.ID)]; !exists {
					t.Fatal("submission did not accept the original candidate event")
				}
				target, err = facade.Open(scenario.workspace, current.RunID)
				if err != nil {
					t.Fatal(err)
				}
				if body, err := confirmInstalledFixtureControl(t, binary, scenario.workspace, scenario.runID, candidate); err != nil {
					t.Fatalf("original saved candidate: %v %s", err, body)
				}
				replayed, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(accepted, replayed) {
					t.Fatal("reopened submission replay added an event or changed state", err)
				}
			}
		})
	}
}
