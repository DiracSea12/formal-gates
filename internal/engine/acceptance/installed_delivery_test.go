package acceptance_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/prompts"
)

func nativeWorktreeAddCount(trace string, resource protocol.NativeResource) int {
	count := 0
	for _, line := range strings.Split(trace, "\n") {
		prefix, arguments, found := strings.Cut(line, "\tworktree\tadd\t")
		if !found {
			continue
		}
		fields := strings.Split(arguments, "\t")
		boundary := slices.Index(fields, "--")
		if boundary < 0 || len(fields) != boundary+3 {
			continue
		}
		path := fields[boundary+1]
		if !filepath.IsAbs(path) {
			cwd, _, _ := strings.Cut(prefix, "\t")
			path = filepath.Join(cwd, path)
		}
		if vcs.SamePath(path, resource.Path) && fields[boundary+2] == resource.Source {
			count++
		}
	}
	return count
}

func TestNativeWorktreeAddCountUsesRecordedIdentity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	resource := protocol.NativeResource{Path: filepath.Join(root, "workspace"), Source: strings.Repeat("a", 40)}
	line := func(cwd, prefix, path, source string, options ...string) string {
		if len(options) == 0 {
			options = []string{"--detach"}
		}
		return cwd + prefix + "\tworktree\tadd\t" + strings.Join(options, "\t") + "\t--\t" + path + "\t" + source + "\n"
	}
	relative := line(root, "", "workspace", resource.Source)
	for _, test := range []struct {
		name, trace string
		count       int
	}{
		{"absolute", line(root, "", resource.Path, resource.Source), 1},
		{"relative", relative, 1},
		{"global-config", line(root, "\t-c\tcore.longpaths=true", "workspace", resource.Source), 1},
		{"checkout-options", line(root, "", "workspace", resource.Source, "--no-checkout", "--detach"), 1},
		{"reordered-options", line(root, "", "workspace", resource.Source, "--detach", "--no-checkout"), 1},
		{"different-directory", line(filepath.Join(root, "other"), "", "workspace", resource.Source), 0},
		{"different-path", line(root, "", "other-workspace", resource.Source), 0},
		{"different-source", line(root, "", "workspace", strings.Repeat("b", 40)), 0},
		{"duplicate", relative + relative, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if count := nativeWorktreeAddCount(test.trace, resource); count != test.count {
				t.Fatalf("native add count=%d, want=%d; trace=%q", count, test.count, test.trace)
			}
		})
	}
}

// Probe edits stay in their issued root until normal resource cleanup. The
// successor consumes the original conclusion and shares delivery edits only.
func TestInstalledSpikeArtifactsRemainOutsideDelivery(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name, tier, mode string
		interrupt, abort bool
	}{
		{name: "final", tier: "FINAL", mode: "none"},
		{name: "checkpoint", tier: "CHECKPOINT", mode: "both"},
		{name: "workspace-resume", tier: "FINAL", mode: "none", interrupt: true},
		{name: "abort", tier: "FINAL", mode: "none", abort: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			d := newCandidateDriver(t, scenario.mode)
			d.plan = candidatePlan(nil)
			command := d.plan.Batches[0]
			spike := command
			spike.ID, spike.Spike = "investigate", true
			spike.RequirementPoints = nil
			spike.Subtasks = []protocol.DevelopmentSubtask{{ID: "uncertainty", Description: "Establish the command input boundary"}}
			spike.Deliverable, spike.Artifacts, spike.Handoff = "Input boundary conclusion", []string{"probe.go"}, "Use the established input boundary"
			command.DependsOn = []string{spike.ID}
			extension := command
			extension.ID, extension.DependsOn, extension.Artifacts = "extension", []string{command.ID}, []string{"app.go", "extension.go"}
			extension.Subtasks = []protocol.DevelopmentSubtask{{ID: "extend", Description: "Consume the implemented input boundary"}}
			d.plan.Batches = []protocol.DevelopmentBatch{spike, command, extension}
			d.plan.VerificationTier = scenario.tier
			if scenario.tier == "CHECKPOINT" {
				d.plan.CheckpointGroups = [][]string{{spike.ID}, {command.ID, extension.ID}}
				d.plan.CheckpointReason = "The implementation consumes the established input boundary"
			}
			stage := "observe"
			if scenario.interrupt {
				stage = "workspace"
			}
			d.breakNative(stage)
			d.reviews()
			find := func(batchID string) (string, protocol.ValidationInput) {
				t.Helper()
				for id, input := range d.run.ValidationInputs {
					if input.Handler == "engine.development.batch.v6" && input.Development != nil && input.Development.Batch.ID == batchID {
						return id, input
					}
				}
				t.Fatalf("missing issued development input %s", batchID)
				return "", protocol.ValidationInput{}
			}
			spikeAction, spikeInput := find(spike.ID)
			probe := "package main\nfunc uncertainInput(s string) bool { return len(s)>0 }\n"
			d.write(spikeInput.Workspace.Path, "probe.go", probe, 0644)
			build := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "probe"), "./...")
			build.Dir = spikeInput.Workspace.Path
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("probe build: %v %s", err, out)
			}
			conclusion := protocol.ValidationResult{Binding: spikeInput.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{
				Paths: []string{"probe.go"}, Summary: "Nonempty command input is accepted; empty input is rejected",
				Commands:   []string{"go build ./...", "go test -timeout=0 ./..."},
				Completion: &protocol.DevelopmentCompletion{DoD: map[string]string{"behavior": "Input boundary established"}, Handoff: spike.Handoff},
			}}
			d.business(spikeAction, conclusion)
			if scenario.abort {
				d.abort()
				d.drive()
				d.assertTargetUnchanged()
				d.assertAbortedResources()
				if _, ok := d.run.Termination.NativeResources[spikeInput.Workspace.ID]; !ok || !reflect.DeepEqual(d.run.Termination.Results[spikeAction].Validation, &conclusion) {
					t.Fatal("abort omitted the spike resource responsibility or its conclusion")
				}
				return
			}
			var group sync.WaitGroup
			for n := 0; n < 2; n++ {
				group.Add(1)
				go func() {
					defer group.Done()
					cmd := d.p.command(nil, "workflow", "drive", "--root", d.p.project, "--run-id", d.runID)
					cmd.Env = append(cmd.Env, d.env...)
					if out, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(out), "REVISION_CONFLICT") && !strings.Contains(string(out), "LOCK_HELD") {
						t.Errorf("concurrent resource drive: %v %s", err, out)
					}
				}()
			}
			group.Wait()
			d.drive()
			commandAction, commandInput := find(command.ID)
			_, state := d.p.bindings(t, d.runID, "codex")
			if !reflect.DeepEqual(commandInput.Development.Dependencies[spike.ID], state.Results[spikeAction]) || !reflect.DeepEqual(state.Results[spikeAction].Validation, &conclusion) {
				t.Fatal("successor lost the original spike action, binding or full conclusion")
			}
			if spikeInput.Workspace.ID == commandInput.Workspace.ID || spikeInput.Workspace.Path == commandInput.Workspace.Path || spikeInput.Workspace.Ref == commandInput.Workspace.Ref || commandInput.Workspace.Source != d.p.base {
				t.Fatal("probe edits share the delivery resource or change its source")
			}
			if state.Validation.Candidate != nil || len(state.Validation.Waves) != 0 || d.run.PromotedSnapshot != "" {
				t.Fatal("spike-only completion produced a candidate or validation wave")
			}
			trace := string(readInstalledFile(t, d.breakLog))
			if strings.Count(trace, "\tworktree\tadd\t") != len(state.NativeResources) {
				t.Fatalf("resource resume or concurrent drive repeated a native add:\n%s", trace)
			}
			for _, resource := range state.NativeResources {
				if nativeWorktreeAddCount(trace, resource) != 1 {
					t.Fatalf("native add differs from its unique registered path and source: %+v\n%s", resource, trace)
				}
			}
			if scenario.interrupt {
				if _, err := os.Stat(d.breakMarker); err != nil {
					t.Fatalf("spike workspace was not interrupted after a native prefix: %v", err)
				}
				found := false
				for id, receipt := range state.HostActionReceipts {
					if receipt.AdapterOperation != "op.workspace" || receipt.AdapterEvidence == nil {
						continue
					}
					var evidence protocol.WorkspaceEvidence
					raw := installedJSON(t, receipt.AdapterEvidence.Values)
					if err := json.Unmarshal(raw, &evidence); err != nil {
						t.Fatal(err)
					}
					if evidence.Resource.ID == spikeInput.Workspace.ID {
						found = receipt.ActionID == id && receipt.Status == protocol.HostActionStatusExecuted && evidence.Resource == *spikeInput.Workspace && evidence.ParametersDigest == receipt.PayloadDigest
					}
				}
				if !found {
					t.Fatal("native resume omitted the original spike typed workspace receipt")
				}
			}
			d.develop(commandAction, commandInput)
			d.drive()
			catalog, err := prompts.LoadPromptCatalog(d.p.source)
			if err != nil {
				t.Fatal(err)
			}
			var gateNames []string
			for _, id := range catalog.RouteCandidates() {
				if _, found := catalog.Gate(id); found {
					gateNames = append(gateNames, id)
				}
			}
			expected := d.run.Configuration.SelectedGates(gateNames)
			var issued, gateActions []string
			var commandSource string
			for id, input := range d.run.ValidationInputs {
				if input.Development != nil && input.Development.Batch.ID == extension.ID {
					t.Fatal("dependent development was issued before its predecessor gates passed")
				}
				if input.Handler != "engine.gate.v3" {
					continue
				}
				if input.Binding.BatchID != command.ID || input.Gate == nil || len(input.Gate.Batches) != 1 || !reflect.DeepEqual(input.Gate.Batches[0], commandInput.Development.Batch) {
					t.Fatal("development gate lost its exact predecessor batch")
				}
				if input.Binding.Candidate == "" || commandSource != "" && commandSource != input.Binding.Candidate {
					t.Fatal("predecessor gates do not share the exact immutable source")
				}
				commandSource = input.Binding.Candidate
				issued = append(issued, input.Gate.Gate)
				gateActions = append(gateActions, id)
			}
			slices.Sort(expected)
			slices.Sort(issued)
			if len(expected) == 0 || !slices.Equal(issued, expected) {
				t.Fatalf("development gates differ from saved configuration: issued=%v expected=%v", issued, expected)
			}
			slices.Sort(gateActions)
			for _, id := range gateActions {
				d.business(id, d.gateResult(id, d.run.ValidationInputs[id]))
			}
			d.drive()
			_, commandState := d.p.bindings(t, d.runID, "codex")
			extensionAction, extensionInput := find(extension.ID)
			if extensionInput.Workspace.Source != commandSource || extensionInput.Workspace.Identity != commandSource || extensionInput.Workspace.ID == commandInput.Workspace.ID || extensionInput.Workspace.Path == commandInput.Workspace.Path {
				t.Fatal("dependent development did not receive the exact validated predecessor source in its own resource")
			}
			if !reflect.DeepEqual(extensionInput.Development.Dependencies[command.ID], commandState.Results[commandAction]) || extensionInput.Development.Dependencies[command.ID].Validation.Development.Completion.Handoff == "" {
				t.Fatal("ordinary members lost the complete original dependency or handoff")
			}
			source := exec.Command("git", "-C", commandInput.Workspace.Path, "show", commandSource+":app.go")
			original, err := source.Output()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(readInstalledFile(t, filepath.Join(extensionInput.Workspace.Path, "app.go")), original) {
				t.Fatal("dependent development lost validated predecessor content")
			}
			d.write(extensionInput.Workspace.Path, "extension.go", "package main\nfunc extended(s string) bool { return len(s)>0 }\n", 0644)
			d.implementation = "package main\nimport \"os\"\nfunc main(){if len(os.Args)!=2 || !valid(os.Args[1]){os.Exit(2)}}\nfunc valid(s string)bool{return s!=\"\" && extended(s)}\n"
			d.develop(extensionAction, extensionInput, "extension.go")
			d.drive()
			d.untilValidation()
			_, state = d.p.bindings(t, d.runID, "codex")
			if string(readInstalledFile(t, filepath.Join(spikeInput.Workspace.Path, "probe.go"))) != probe {
				t.Fatal("candidate preparation altered the retained probe source")
			}
			for _, snapshot := range []string{state.Validation.DevelopmentSnapshot, d.candidate} {
				if snapshot == "" || d.p.git(t, "ls-tree", "--name-only", snapshot, "--", "probe.go") != "" || !strings.Contains(d.p.git(t, "show", snapshot+":app.go"), "extended(s)") || d.p.git(t, "show", snapshot+":extension.go") != "package main\nfunc extended(s string) bool { return len(s)>0 }" {
					t.Fatal("development or validation snapshot lost delivery content isolation")
				}
			}
			wave := state.Validation.Waves[len(state.Validation.Waves)-1]
			if len(wave.SourceTasks) != 2 || slices.Contains(wave.SourceTasks, spikeInput.Binding.Task) || !slices.Contains(wave.SourceTasks, commandInput.Binding.Task) || !slices.Contains(wave.SourceTasks, extensionInput.Binding.Task) || wave.Scope != coverage.ScopeFull || len(wave.CheckpointTasks) != 0 {
				t.Fatal("final validation sources differ from the complete delivery plan")
			}
			d.execute()
			d.assertClean()
			terminal := d.run.Termination
			if terminal.NativeResources[spikeInput.Workspace.ID].Status != "CLEANED" || !reflect.DeepEqual(terminal.Results[spikeAction].Validation, &conclusion) || d.p.git(t, "ls-tree", "--name-only", "HEAD", "--", "probe.go") != "" {
				t.Fatal("delivery or terminal cleanup lost spike isolation or its conclusion")
			}
			for _, receipt := range terminal.HostActionReceipts {
				if receipt.AdapterOperation == "op.cleanup" {
					var proof protocol.WorkspaceCleanupReceipt
					if err := json.Unmarshal(installedJSON(t, receipt.AdapterEvidence.Values), &proof); err != nil || len(proof.Resources) != len(terminal.NativeResources) {
						t.Fatal("cleanup receipt omitted an owned resource")
					}
					for _, resource := range proof.Resources {
						if resource != terminal.NativeResources[resource.ID] {
							t.Fatal("cleanup evidence differs from the native resource registry")
						}
					}
				}
			}
		})
	}
}

// Checkpoint validation releases the next confirmed development group while
// preserving the target. Delivery includes every group and approved artifact.
func TestInstalledCheckpointDeliversTheCompletePlan(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"none", "blackbox"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			d := newCandidateDriver(t, mode)
			d.plan = candidatePlan(nil)
			extension := d.plan.Batches[0]
			extension.ID = "extension"
			extension.Subtasks = []protocol.DevelopmentSubtask{{ID: "extend", Description: "Use the validated command in an extension"}}
			extension.DependsOn = []string{"command"}
			extension.Artifacts = []string{"app.go", "extension.go"}
			d.plan.Batches = append(d.plan.Batches, extension)
			d.plan.VerificationTier = "CHECKPOINT"
			d.plan.CheckpointGroups = [][]string{{"command"}, {"extension"}}
			d.plan.CheckpointReason = "The extension consumes the verified command boundary"
			d.breakNative("observe")
			d.reviews()
			d.untilValidation()
			intermediate := d.candidate
			d.executeWorkers()
			d.drive()
			d.assertTargetUnchanged()
			if d.run.PromotedSnapshot != "" || d.run.Status != "ACTIVE" {
				t.Fatalf("checkpoint closed delivery: %+v", d.run)
			}
			wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if wave.Status != "COMPLETE" || wave.Scope != coverage.ScopeFull || len(wave.SourceTasks) != 1 || len(wave.CheckpointTasks) != 1 {
				t.Fatalf("checkpoint lost its exact group: %+v", wave)
			}
			var action string
			var input protocol.ValidationInput
			for id, in := range d.run.ValidationInputs {
				if in.Handler == "engine.development.batch.v6" && in.Development != nil && in.Development.Batch.ID == "extension" {
					action, input = id, in
				}
			}
			if action == "" || input.Workspace == nil || input.Workspace.Source != intermediate || len(input.Development.Dependencies) != 1 || input.Development.Dependencies["command"].Validation.Development.Completion.Handoff == "" {
				t.Fatal("checkpoint omitted the exact successor workspace or dependency evidence")
			}
			before := installedJSON(t, d.run)
			worktrees := d.p.git(t, "worktree", "list", "--porcelain")
			refs := d.p.git(t, "for-each-ref", "--format=%(refname) %(objectname)", "refs/formal-gates/")
			var group sync.WaitGroup
			for n := 0; n < 2; n++ {
				group.Add(1)
				go func() {
					defer group.Done()
					cmd := d.p.command(nil, "workflow", "drive", "--root", d.p.project, "--run-id", d.runID)
					cmd.Env = append(cmd.Env, d.env...)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Errorf("parallel checkpoint drive: %v %s", err, out)
					}
				}()
			}
			group.Wait()
			d.drive()
			if string(installedJSON(t, d.run)) != string(before) || d.p.git(t, "worktree", "list", "--porcelain") != worktrees || d.p.git(t, "for-each-ref", "--format=%(refname) %(objectname)", "refs/formal-gates/") != refs {
				t.Fatal("checkpoint replay changed frontier or native resource responsibility")
			}
			d.assertTargetUnchanged()
			trace := string(readInstalledFile(t, d.breakLog))
			if strings.Contains(trace, "\tmerge\t--ff-only\t") || strings.Contains(trace, "\tworktree\tremove\t") || strings.Contains(trace, "\tupdate-ref\t-d\t") {
				t.Fatal("intermediate checkpoint promoted or cleaned resources")
			}
			d.write(input.Workspace.Path, "extension.go", "package main\nfunc extended(s string) bool {return len(s)>0}\n", 0644)
			d.implementation = "package main\nimport \"os\"\nfunc main(){if len(os.Args)!=2 || !valid(os.Args[1]){os.Exit(2)}}\nfunc valid(s string)bool{return s!=\"\" && extended(s)}\n"
			d.develop(action, input, "extension.go")
			binary := executablePath(filepath.Join(t.TempDir(), "command"))
			build := exec.Command("go", "build", "-o", binary, "./...")
			build.Dir = input.Workspace.Path
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("successor build: %v %s", err, out)
			}
			if out, err := exec.Command(binary, "ok").CombinedOutput(); err != nil {
				t.Fatalf("successor valid input: %v %s", err, out)
			}
			if out, err := exec.Command(binary, "").CombinedOutput(); err == nil {
				t.Fatalf("successor invalid input accepted: %s", out)
			}
			d.drive()
			d.untilValidation()
			if d.candidate == intermediate || !strings.Contains(d.p.git(t, "show", d.candidate+":app.go"), "extended(s)") || d.p.git(t, "show", d.candidate+":extension.go") != "package main\nfunc extended(s string) bool {return len(s)>0}" {
				t.Fatal("final candidate omitted a confirmed group")
			}
			final := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if final.Scope != coverage.ScopeFull || len(final.SourceTasks) != 2 || len(final.CheckpointTasks) != 0 {
				t.Fatalf("final candidate retained an intermediate validation scope: %+v", final)
			}
			d.execute()
			d.assertClean()
			promotion := d.run.Termination.CandidatePromotion
			if promotion == nil || promotion.Source != d.candidate || promotion.SourceDigest != d.candidateTree || promotion.Final != d.run.PromotedSnapshot || promotion.FinalDigest != d.candidateTree || promotion.TargetBase != d.p.base {
				t.Fatal("final promotion omitted its native content binding")
			}
		})
	}
}

func (d *candidateDriver) abort() {
	d.t.Helper()
	_, original := d.p.bindings(d.t, d.runID, "codex")
	// Unsigned workers are never presumed stopped; the transport reports the
	// observable fact that no agent was started before the user cancels.
	for id := range d.run.PendingActions {
		if _, spawned := original.SpawnReceipts[id]; spawned {
			observed, err := lifecycle.VerifyDispatch(d.p.project, d.runID, id)
			if err != nil || !observed.StartObserved || !observed.StopObserved {
				d.t.Fatalf("spawned worker has no original start/stop evidence: %+v %v", observed, err)
			}
			for _, kind := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
				event, err := protocol.NewCorrelatedLifecycleEvent(d.id(), "codex", id, observed.Identity, kind)
				if err != nil {
					d.t.Fatal(err)
				}
				d.submit(event)
			}
			continue
		}
		event, err := protocol.NewSpawnReceiptWithFailureClass(d.id(), id, "codex", id, protocol.SpawnStatusFailed, "BUSINESS_REJECT")
		if err != nil {
			d.t.Fatal(err)
		}
		d.submit(event)
	}
	d.control(facade.ControlInput{Request: protocol.ControlAbort})
	d.control(facade.ControlInput{Ask: d.ask(protocol.ControlAbort), Choice: "abort", Reason: "Stop interrupted run"})
}

func (d *candidateDriver) showTerminal() {
	d.t.Helper()
	shown := d.p.run(d.t, "show", d.runID)
	if shown.Status != d.run.Status || shown.Revision != d.run.Revision || !reflect.DeepEqual(shown.Termination, d.run.Termination) {
		d.t.Fatal("terminal show changed the completed run identity or evidence")
	}
	d.run = shown
}

func (d *candidateDriver) assertAbortedResources() {
	d.t.Helper()
	d.showTerminal()
	state := d.run.Termination
	if d.run.Status != "ABORTED" || state == nil || !state.ResourcesCleaned() || len(state.PendingHostActions) != 0 || len(state.PendingActions) != 0 {
		d.t.Fatalf("abort retained an execution responsibility: %+v", d.run)
	}
	cleanupProofs := 0
	for _, r := range state.NativeResources {
		if _, err := os.Stat(r.Path); !os.IsNotExist(err) || d.p.git(d.t, "for-each-ref", "--format=%(objectname)", r.Ref) != "" {
			d.t.Fatalf("aborted resource remains: %+v err=%v", r, err)
		}
	}
	if trees := d.p.git(d.t, "worktree", "list", "--porcelain"); strings.Count(trees, "worktree ") != 1 {
		d.t.Fatalf("abort native registrations remain: %s", trees)
	}
	for _, receipt := range state.HostActionReceipts {
		if receipt.AdapterOperation == "op.cleanup" {
			cleanupProofs++
			if receipt.Status != protocol.HostActionStatusExecuted || receipt.AdapterEvidence == nil {
				d.t.Fatal("cleanup has no actual typed completion")
			}
			var proof protocol.WorkspaceCleanupReceipt
			raw, err := json.Marshal(receipt.AdapterEvidence.Values)
			if err != nil || json.Unmarshal(raw, &proof) != nil || proof.ParametersDigest != receipt.PayloadDigest || len(proof.Resources) != len(state.NativeResources) {
				d.t.Fatal("cleanup receipt omitted its exact resource set")
			}
			for _, r := range proof.Resources {
				if !reflect.DeepEqual(r, state.NativeResources[r.ID]) {
					d.t.Fatal("cleanup proof differs from the final registry")
				}
			}
		}
	}
	if len(state.NativeResources) != 0 && cleanupProofs == 0 {
		d.t.Fatal("abort cleaned resources without a native cleanup receipt")
	}
	d.t.Logf("aborted native proof: %s", installedJSON(d.t, state))
	d.assertAcceptedReplay(true)
	summary := readInstalledFile(d.t, d.run.SummaryPath)
	d.drive()
	d.showTerminal()
	if string(summary) != string(readInstalledFile(d.t, d.run.SummaryPath)) {
		d.t.Fatal("abort replay changed the frozen summary")
	}
}

// Abort consumes the original intent after a real successful native prefix;
// it does not finish that cancelled operation to manufacture a success receipt.
func TestInstalledAbortReclaimsInterruptedNativePrefixes(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"workspace", "pin", "track", "freeze"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			d := newCandidateDriver(t, "none", ".gates/input/requirements.md")
			d.pauseOnInterrupt = true
			if stage == "workspace" || stage == "pin" {
				d.breakNative(stage)
				d.reviews()
			} else {
				d.reviews()
				for id, in := range d.run.ValidationInputs {
					if in.Handler == "engine.development.batch.v6" {
						d.develop(id, in)
					}
				}
				d.breakNative(stage)
				d.drive()
			}
			if _, err := os.Stat(d.breakMarker); err != nil {
				t.Fatalf("native prefix was not interrupted: %v", err)
			}
			trace := string(readInstalledFile(t, d.breakLog))
			_, state := d.p.bindings(t, d.runID, "codex")
			if len(state.PendingHostActions) == 0 {
				t.Fatal("interruption lost its original native intent")
			}
			d.abort()
			d.drive()
			d.assertTargetUnchanged()
			d.assertAbortedResources()
			after := string(readInstalledFile(t, d.breakLog))
			for _, command := range []string{"\tworktree\tadd\t", "\tadd\t--all\t", "\tcommit\t-m\t", "\tupdate-ref\trefs/"} {
				if strings.Count(after, command) != strings.Count(trace, command) {
					t.Fatalf("cancelled native prefix continued %q", command)
				}
			}
			t.Logf("interruption trace:\n%s\nfinal trace:\n%s", trace, after)
		})
	}
}

func TestInstalledUnknownIncompleteWorkspaceRemainsObserved(t *testing.T) {
	t.Parallel()
	d := newCandidateDriver(t, "none", ".gates/input/requirements.md")
	d.pauseOnInterrupt = true
	d.unknownReceipt = true
	d.breakNative("workspace")
	d.reviews()
	before := string(readInstalledFile(t, d.breakLog))
	statePath := filepath.Join(d.p.project, facade.EngineNamespace, d.runID, "state.json")
	stateBytes := readInstalledFile(t, statePath)
	revision := d.run.Revision
	_, state := d.p.bindings(t, d.runID, "codex")
	intent, ok := state.PendingHostActions[d.unknownAction]
	if !ok {
		t.Fatal("unknown original responsibility is missing")
	}
	for n := 0; n < 2; n++ {
		d.drive()
	}
	_, state = d.p.bindings(t, d.runID, "codex")
	if d.run.Revision != revision || !reflect.DeepEqual(stateBytes, readInstalledFile(t, statePath)) {
		t.Fatal("UNKNOWN observation changed its durable state")
	}
	if state.HostActionReceipts[d.unknownAction].Status != protocol.HostActionStatusUnknown || !reflect.DeepEqual(state.PendingHostActions[d.unknownAction], intent) || len(state.HostActionFailures[d.unknownAction]) != 0 || d.run.Status == "ABORTED" {
		t.Fatal("incomplete UNKNOWN was falsely completed or cancelled")
	}
	after := string(readInstalledFile(t, d.breakLog))
	for _, command := range []string{"\tworktree\tadd\t", "\tworktree\tremove\t", "\tupdate-ref\t"} {
		if strings.Count(before, command) != strings.Count(after, command) {
			t.Fatalf("unknown observation wrote native state %q", command)
		}
	}
	t.Logf("unknown observed responsibility: %s", installedJSON(t, state))
}

// Actual Abort withdraws an exact partial UNKNOWN and then cleans its resource.
// The uncertain response and original intent remain immutable evidence.
func TestInstalledAbortWithdrawsIncompleteUnknownWorkspace(t *testing.T) {
	t.Parallel()
	d := newCandidateDriver(t, "none", ".gates/input/requirements.md")
	d.pauseOnInterrupt = true
	d.unknownReceipt = true
	d.breakNative("checkout")
	d.reviews()
	_, state := d.p.bindings(t, d.runID, "codex")
	intent, pending := state.PendingHostActions[d.unknownAction]
	unknown := state.HostActionReceipts[d.unknownAction]
	if !pending || unknown.Status != protocol.HostActionStatusUnknown || len(state.HostActionFailures[d.unknownAction]) != 0 {
		t.Fatal("partial UNKNOWN has no original unresolved responsibility")
	}
	var params protocol.WorkspaceParameters
	if err := json.Unmarshal(installedJSON(t, intent.Adapter.Params), &params); err != nil {
		t.Fatal(err)
	}
	r := params.Resource
	head := d.p.git(t, "-C", r.Path, "rev-parse", "HEAD")
	tree := d.p.git(t, "-C", r.Path, "rev-parse", "HEAD^{tree}")
	index := d.p.git(t, "-C", r.Path, "write-tree")
	clean := d.p.git(t, "-C", r.Path, "status", "--porcelain=v1") == ""
	ref := d.p.git(t, "for-each-ref", "--format=%(objectname)", r.Ref)
	if head != r.Source || tree != index || !clean || ref != "" {
		t.Fatal("UNKNOWN fixture does not have the exact incomplete workspace prefix")
	}
	before := string(readInstalledFile(t, d.breakLog))
	d.abort()
	accepted := d.accepted[len(d.accepted)-1].receipt
	d.drive()
	d.assertTargetUnchanged()
	d.assertAbortedResources()
	terminal := d.run.Termination
	effect := terminal.ReconciledEffects[intent.ActionID]
	if effect.Status != "WITHDRAWN" || !reflect.DeepEqual(effect.Intent, &intent) || !reflect.DeepEqual(effect.UnknownReceipt, &unknown) || !reflect.DeepEqual(terminal.HostActionReceipts[intent.ActionID], unknown) || len(terminal.HostActionFailures[intent.ActionID]) != 0 {
		t.Fatal("Abort changed the original UNKNOWN fact or manufactured a native result")
	}
	observation := effect.Resource
	if observation == nil || observation.ParametersDigest != intent.PayloadDigest || observation.Resource != r || !observation.WorktreeExists || observation.RefIdentity != ref || observation.Prefix == nil || observation.Prefix.Head != head || observation.Prefix.Tree != tree || observation.Prefix.IndexTree != index || !observation.Prefix.Clean || observation.Prefix.StagedOnly || len(observation.Prefix.Changes) != 0 {
		t.Fatalf("withdrawal omitted its exact original resource/prefix: %+v", observation)
	}
	var summary facade.TerminalSummary
	if err := json.Unmarshal(readInstalledFile(t, d.run.SummaryPath), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.LastEvent == nil || !reflect.DeepEqual(summary.LastEvent.Acceptance, accepted) {
		t.Fatal("Abort summary changed the original accepted DECIDE")
	}
	after := string(readInstalledFile(t, d.breakLog))
	for _, command := range []string{"\tworktree\tadd\t", "\treset\t--hard\t", "\tadd\t--all\t", "\tcommit\t-m\t", "\tupdate-ref\trefs/"} {
		if strings.Count(before, command) != strings.Count(after, command) {
			t.Fatalf("withdrawn UNKNOWN continued native work %q", command)
		}
	}
	t.Logf("original UNKNOWN=%s withdrawal=%s summary=%s native trace=%s", installedJSON(t, unknown), installedJSON(t, effect), installedJSON(t, summary), after)
}

// A completed UNKNOWN operation supplies its original typed evidence during
// Abort; observation cannot re-execute it or replace the accepted decision.
func TestInstalledAbortObservesCompletedUnknownWorkspace(t *testing.T) {
	t.Parallel()
	d := newCandidateDriver(t, "none", ".gates/input/requirements.md")
	d.pauseOnInterrupt = true
	d.unknownReceipt = true
	d.breakNative("pin")
	d.reviews()
	if _, err := os.Stat(d.breakMarker); err != nil {
		t.Fatalf("completed native prefix not interrupted: %v", err)
	}
	d.abort()
	before := string(readInstalledFile(t, d.breakLog))
	d.drive()
	d.assertTargetUnchanged()
	d.assertAbortedResources()
	receipt, ok := d.run.Termination.HostActionReceipts[d.unknownAction]
	if !ok || receipt.Status != protocol.HostActionStatusExecuted || receipt.AdapterOperation != "op.workspace" || receipt.AdapterEvidence == nil {
		t.Fatalf("UNKNOWN responsibility lost its original typed completion: %+v", receipt)
	}
	after := string(readInstalledFile(t, d.breakLog))
	for _, command := range []string{"\tworktree\tadd\t", "\tadd\t--all\t", "\tcommit\t-m\t", "\tupdate-ref\trefs/"} {
		if strings.Count(before, command) != strings.Count(after, command) {
			t.Fatalf("completed UNKNOWN responsibility repeated %q", command)
		}
	}
	t.Logf("UNKNOWN typed completion: %s\nraw native trace:\n%s", installedJSON(t, receipt), after)
}

// Lost cleanup responses preserve authoritative control and the original
// business receipt until the user keeps the requirement or aborts the run.
func TestInstalledCleanupControlRemainsAuthoritative(t *testing.T) {
	t.Parallel()
	for _, control := range []protocol.ControlKind{protocol.ControlRequestRequirementChange, protocol.ControlAbort} {
		t.Run(string(control), func(t *testing.T) {
			t.Parallel()
			d := newCandidateDriver(t, "none")
			d.reviews()
			d.untilValidation()
			d.executeWorkers()
			d.pauseOnInterrupt = true
			d.breakNative("cleanup")
			d.drive()
			if d.run.Status != "FINALIZING_CLEANUP" {
				t.Fatalf("no genuine cleanup prefix: %s", d.run.Status)
			}
			d.control(facade.ControlInput{Request: control})
			target := func() string {
				return strings.Join([]string{d.p.git(t, "rev-parse", "HEAD"), d.p.git(t, "rev-parse", "HEAD^{tree}"), d.p.git(t, "write-tree"), d.p.git(t, "status", "--porcelain=v1")}, "\n")
			}
			targetBefore := target()
			_, originalState := d.p.bindings(t, d.runID, "codex")
			promotion := originalState.CandidatePromotion
			if promotion == nil || promotion.Final != d.p.git(t, "rev-parse", "HEAD") || promotion.FinalDigest != d.p.git(t, "rev-parse", "HEAD^{tree}") {
				t.Fatal("cleanup has no exact completed promotion")
			}
			promotionReceipt := originalState.HostActionReceipts[promotion.OperationID]
			before := d.p.git(t, "worktree", "list", "--porcelain") + d.p.git(t, "for-each-ref", "--format=%(refname) %(objectname)", "refs/formal-gates/")
			statePath := filepath.Join(d.p.project, facade.EngineNamespace, d.runID, "state.json")
			stateBytes := readInstalledFile(t, statePath)
			var next decision.NextResult
			for _, command := range []string{"next", "show", "status"} {
				out := d.call(nil, "workflow", command, "--machine", "--root", d.p.project, "--run-id", d.runID)
				if command == "next" {
					if err := json.Unmarshal(out, &next); err != nil {
						t.Fatal(err)
					}
				} else {
					var run facade.Run
					if err := json.Unmarshal(out, &run); err != nil {
						t.Fatal(err)
					}
					next = run.Next
				}
				if next.Kind != decision.KindAsk {
					t.Fatalf("%s hid the active Ask: %s", command, out)
				}
			}
			if !reflect.DeepEqual(stateBytes, readInstalledFile(t, statePath)) {
				t.Fatal("read projection wrote State")
			}
			if control == protocol.ControlRequestRequirementChange {
				d.drive()
				if after := d.p.git(t, "worktree", "list", "--porcelain") + d.p.git(t, "for-each-ref", "--format=%(refname) %(objectname)", "refs/formal-gates/"); after != before {
					t.Fatal("requirement barrier wrote native cleanup")
				}
				_, held := d.p.bindings(t, d.runID, "codex")
				if !reflect.DeepEqual(held.CandidatePromotion, promotion) || !reflect.DeepEqual(held.HostActionReceipts[promotion.OperationID], promotionReceipt) || !reflect.DeepEqual(held.NativeResources, originalState.NativeResources) || !reflect.DeepEqual(held.PendingHostActions, originalState.PendingHostActions) {
					t.Fatal("requirement barrier changed original native facts")
				}
			}
			decide := d.control(facade.ControlInput{Ask: d.ask(control), Choice: "abort", Reason: "Resolve the active cleanup control"})
			original := d.accepted[len(d.accepted)-1].receipt
			_, decided := d.p.bindings(t, d.runID, "codex")
			repeated, _ := d.replayAccepted(decide)
			_, replayed := d.p.bindings(t, d.runID, "codex")
			if !reflect.DeepEqual(repeated, original) || !reflect.DeepEqual(decided, replayed) {
				t.Fatal("duplicate control decision changed its acceptance or durable state")
			}
			d.drive()
			want := "COMPLETE"
			if control == protocol.ControlAbort {
				want = "ABORTED"
			}
			if d.run.Status != want || d.run.Termination == nil || !d.run.Termination.ResourcesCleaned() || len(d.run.Termination.PendingHostActions) != 0 {
				t.Fatalf("control did not close its genuine debt: %s %+v", d.run.Status, d.run.Termination)
			}
			var summary facade.TerminalSummary
			d.showTerminal()
			if err := json.Unmarshal(readInstalledFile(t, d.run.SummaryPath), &summary); err != nil {
				t.Fatal(err)
			}
			if control == protocol.ControlAbort && (summary.LastEvent == nil || !reflect.DeepEqual(summary.LastEvent.Acceptance, original)) {
				t.Fatalf("abort identity changed: %+v != %+v", summary.LastEvent, original)
			}
			if target() != targetBefore || !reflect.DeepEqual(d.run.Termination.CandidatePromotion, promotion) || !reflect.DeepEqual(d.run.Termination.HostActionReceipts[promotion.OperationID], promotionReceipt) {
				t.Fatal("cleanup control changed delivered native content or its original promotion proof")
			}
			if control == protocol.ControlAbort {
				d.assertAbortedResources()
			} else {
				d.assertClean()
			}
		})
	}
}
