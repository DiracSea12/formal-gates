package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

func TestWorkflowReentrySelectsExistingRunWithoutCopyingIdentity(t *testing.T) {
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	for _, entry := range []string{"show", "status", "next"} {
		var out, stderr bytes.Buffer
		if code := Run("formal-gates", []string{"workflow", entry, "--root", root}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(entry, stderr.String())
		}
		var view map[string]any
		if json.Unmarshal(out.Bytes(), &view) != nil || view["runId"] != "writer-exclusive" || !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
			t.Fatal("read-only reentry changed authority or selected another run", entry, out.String())
		}
	}
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "drive", "--root", root, "--single-step"}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal("drive did not reenter its existing run", stderr.String())
	}
	out.Reset()
	if code := Run("formal-gates", []string{"workflow", "submit", "--root", root, "--single-step"}, IO{Stdin: strings.NewReader(`{"request":"ABORT"}`), Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal("active request required a copied run ID", stderr.String())
	}
	run, err := f.Show()
	if err != nil || len(run.PendingAsks) != 1 || run.PendingAsks[0].Control != "ABORT" {
		t.Fatal("request reached another run or was not accepted", err)
	}
}

func TestWorkflowMultipleRunsRequireProgramPositionBeforeAnyWrite(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request facade.StartRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	request.RunID = "another-target"
	a := &facade.Admission{RegistryPath: request.AdmissionRegistryPath, Target: request.AdmissionTarget, Root: request.AdmissionRoot, Provider: request.Provider, PackageDigest: request.PackageDigest, InstalledTargetIdentity: request.InstalledTargetIdentity, Generation: request.AdmissionGeneration, Lease: request.AdmissionLease, Token: request.AdmissionToken}
	if _, _, err := facade.Start(facade.StartOptions{Root: root, ArtifactRoot: root, Request: request, Admission: a, VCSRunner: frozenGitRunner{}}); err != nil {
		t.Fatal(err)
	}
	before := map[string]map[string]string{}
	for _, id := range []string{"another-target", "writer-exclusive"} {
		before[id] = snapshotEngineRunTree(t, root, id)
	}
	for _, entry := range []string{"show", "status", "next", "drive", "submit"} {
		var out, stderr bytes.Buffer
		if code := Run("formal-gates", []string{"workflow", entry, "--root", root}, IO{Stdin: strings.NewReader(`{"request":"ABORT"}`), Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(entry, stderr.String())
		}
		var view struct {
			Runs []struct {
				Run   int    `json:"run"`
				RunID string `json:"runId"`
			} `json:"runs"`
		}
		if json.Unmarshal(out.Bytes(), &view) != nil || len(view.Runs) != 2 || view.Runs[0].Run != 1 || view.Runs[0].RunID != "another-target" || view.Runs[1].RunID != "writer-exclusive" {
			t.Fatal("ambiguous target was guessed or copied", entry, out.String())
		}
		for id, original := range before {
			if !reflect.DeepEqual(original, snapshotEngineRunTree(t, root, id)) {
				t.Fatal("enumeration changed a run", entry, id)
			}
		}
	}
	for _, selection := range [][]string{{"--run", "3"}, {"--run", "-1"}, {"--run", "1", "--run-id", "writer-exclusive"}} {
		var out, stderr bytes.Buffer
		args := append([]string{"workflow", "submit", "--root", root, "--single-step"}, selection...)
		if code := Run("formal-gates", args, IO{Stdin: strings.NewReader(`{"request":"ABORT"}`), Stdout: &out, Stderr: &stderr}); code == 0 {
			t.Fatal("invalid target selection accepted", selection)
		}
		for id, original := range before {
			if !reflect.DeepEqual(original, snapshotEngineRunTree(t, root, id)) {
				t.Fatal("rejected selection changed a run", id)
			}
		}
	}
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "submit", "--root", root, "--run", "2", "--single-step"}, IO{Stdin: strings.NewReader(`{"request":"ABORT"}`), Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal("numbered request failed", stderr.String())
	}
	if !reflect.DeepEqual(before["another-target"], snapshotEngineRunTree(t, root, "another-target")) || reflect.DeepEqual(before["writer-exclusive"], snapshotEngineRunTree(t, root, "writer-exclusive")) {
		t.Fatal("numbered request did not preserve its exact selected target")
	}
}

func TestWorkflowReentryReadsTerminalAuthorityWithoutRunIdentity(t *testing.T) {
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	request, err := protocol.NewRequestEvent("request-abort", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(request); err != nil {
		t.Fatal(err)
	}
	token, err := f.Freshness(string(request.ID))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := protocol.NewDecideEvent("decide-abort", protocol.RequestID(request.ID), token, "abort")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(decision); err != nil {
		t.Fatal(err)
	}
	if run, err := f.Drive(); err != nil || run.Status != "ABORTED" {
		t.Fatal("original terminal path failed", err, run.Status)
	}
	if _, err := os.Stat(filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "state.json")); !os.IsNotExist(err) {
		t.Fatal("terminal state was not retired", err)
	}
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	for _, entry := range []string{"show", "status", "next", "drive"} {
		var out, stderr bytes.Buffer
		if code := Run("formal-gates", []string{"workflow", entry, "--root", root}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(entry, stderr.String())
		}
		after := snapshotEngineRunTree(t, root, "writer-exclusive")
		if entry == "drive" {
			// The foreground consumer claims its existing executor lock; immutable
			// terminal authority and all other retained evidence stay unchanged.
			delete(after, "executor.lock")
		}
		if !strings.Contains(out.String(), "ABORTED") || !reflect.DeepEqual(before, after) {
			t.Fatal("terminal reentry altered original authority", entry, out.String())
		}
	}
}
