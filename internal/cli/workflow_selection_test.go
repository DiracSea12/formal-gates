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
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/validate"
)

func TestWorkflowReentrySelectsExistingRunWithoutCopyingIdentity(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
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

func TestWorkflowReentryRetainsUnavailableRunAndSelectsCurrentRun(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	oldID := "writer-exclusive"
	body, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, oldID, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request facade.StartRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	var installation validate.InstallReport
	if err := readJSONFileInto(request.AdmissionRegistryPath+".install.json", &installation); err != nil {
		t.Fatal(err)
	}
	source := installation.Targets[0].SourceRoot
	mustWriteCLI(t, filepath.Join(source, "README.md"), "Updated installed documentation.\n")
	for _, bootstrap := range []bool{false, true} {
		args := []string{"install", "--source", source, "--host", "zcode", "--scope", "global", "--force"}
		if bootstrap {
			args = append(args, "--bootstrap")
		}
		var out, stderr bytes.Buffer
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal("normal installation update failed", stderr.String())
		}
	}
	oldBefore := snapshotEngineRunTree(t, root, oldID)
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "show", "--root", root}, IO{Stdout: &out, Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "UNREGISTERED_INSTALL") {
		t.Fatal("single unavailable run bypassed its admission barrier", out.String(), stderr.String())
	}
	barrier := stderr.String()
	admission, err := validate.ResolveEngineAdmission(root, "")
	if err != nil {
		t.Fatal(err)
	}
	request = facade.StartRequest{RunID: "current-run", BaseSnapshot: request.BaseSnapshot, Provider: admission.Provider, DefinitionSource: request.DefinitionSource, DefinitionDigest: request.DefinitionDigest, IntakeConfirmationReceipt: request.IntakeConfirmationReceipt}
	a := &facade.Admission{RegistryPath: admission.RegistryPath, Target: admission.Target, Provider: admission.Provider, PackageDigest: admission.PackageDigest, InstalledTargetIdentity: admission.RecordID, Generation: admission.Generation, Lease: admission.Lease, Token: admission.Token}
	if _, _, err := facade.Start(facade.StartOptions{Root: root, ArtifactRoot: root, Request: request, Admission: a, VCSRunner: frozenGitRunner{}}); err != nil {
		t.Fatal(err)
	}
	currentBefore := snapshotEngineRunTree(t, root, request.RunID)
	out.Reset()
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "show", "--root", root}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal("unavailable run blocked current choices", stderr.String())
	}
	var choices struct {
		Runs []map[string]any `json:"runs"`
	}
	if err := json.Unmarshal(out.Bytes(), &choices); err != nil || len(choices.Runs) != 2 || choices.Runs[0]["runId"] != request.RunID || choices.Runs[1]["runId"] != oldID || choices.Runs[1]["available"] != false || choices.Runs[1]["error"] != strings.TrimSpace(barrier) {
		t.Fatal("unavailable run lost its position or original error", err, out.String())
	}
	if _, present := choices.Runs[1]["diagnostic"]; present {
		t.Fatal("run selection exposed a full raw diagnostic")
	}
	for position, wantCode := range map[string]int{"1": 0, "2": 1} {
		out.Reset()
		stderr.Reset()
		if code := Run("formal-gates", []string{"workflow", "show", "--root", root, "--run", position}, IO{Stdout: &out, Stderr: &stderr}); code != wantCode || wantCode != 0 && stderr.String() != barrier || wantCode == 0 && !strings.Contains(out.String(), request.RunID) {
			t.Fatal("selected run did not use its original admission", position, code, out.String(), stderr.String())
		}
	}
	if !reflect.DeepEqual(oldBefore, snapshotEngineRunTree(t, root, oldID)) || !reflect.DeepEqual(currentBefore, snapshotEngineRunTree(t, root, request.RunID)) {
		t.Fatal("listing or selection changed a run")
	}
}

func TestWorkflowRetiredRunReplaysAfterInstallationUpdate(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	requestPath := filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "request.json")
	var request facade.StartRequest
	if err := readJSONFileInto(requestPath, &request); err != nil {
		t.Fatal(err)
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := submitCLIControlWithHost(t, f, candidate); err != nil {
		t.Fatal(err)
	}
	candidate, err = f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "abort", Reason: "Stop this local fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := submitCLIControlWithHost(t, f, candidate); err != nil {
		t.Fatal(err)
	}
	run, err := f.Drive()
	if err != nil || run.Status != "ABORTED" {
		t.Fatal(run.Status, err)
	}
	// The same public consumer owns its existing local lock before the update.
	var initial, initialError bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "drive", "--root", root, "--run", "1", "--machine"}, IO{Stdout: &initial, Stderr: &initialError}); code != 0 {
		t.Fatal(initialError.String())
	}
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	var installation validate.InstallReport
	if err := readJSONFileInto(request.AdmissionRegistryPath+".install.json", &installation); err != nil {
		t.Fatal(err)
	}
	source := installation.Targets[0].SourceRoot
	mustWriteCLI(t, filepath.Join(source, "README.md"), "Updated installed documentation.\n")
	for _, bootstrap := range []bool{false, true} {
		args := []string{"install", "--source", source, "--host", "zcode", "--scope", "global", "--force"}
		if bootstrap {
			args = append(args, "--bootstrap")
		}
		var out, stderr bytes.Buffer
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(stderr.String())
		}
	}
	for _, entry := range []string{"show", "status", "next", "drive"} {
		var out, stderr bytes.Buffer
		args := []string{"workflow", entry, "--root", root, "--run", "1"}
		if entry == "drive" {
			args = append(args, "--machine")
		}
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal("retired run failed after normal update", entry, stderr.String())
		}
		if !strings.Contains(out.String(), "COMPLETE") && !strings.Contains(out.String(), "ABORTED") {
			t.Fatal("lost terminal projection", entry, out.String())
		}
		if !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
			t.Fatal("replay changed terminal authority", entry)
		}
	}
	var controlOut, controlError bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "submit", "--root", root, "--run", "1", "--single-step"}, IO{Stdin: strings.NewReader(`{"request":"ABORT"}`), Stdout: &controlOut, Stderr: &controlError}); code == 0 || !strings.Contains(controlError.String(), "STALE_FENCING_TOKEN") {
		t.Fatal("retired read granted a new control writer", controlOut.String(), controlError.String())
	}
	if !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
		t.Fatal("rejected control changed original terminal authority")
	}
	// An unfinished cleanup intent must retain the original installation fence.
	intent := filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "terminal-summary.intent.json")
	mustWriteCLI(t, intent, before["terminal-summary.json"])
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "drive", "--root", root, "--run", "1", "--machine"}, IO{Stdout: &out, Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "UNREGISTERED_INSTALL") {
		t.Fatal("unfinished cleanup bypassed admission", out.String(), stderr.String())
	}
}

func TestWorkflowMultipleRunsRequireProgramPositionBeforeAnyWrite(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
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
	if testkit.RunNativePackageTest(t) {
		return
	}
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
