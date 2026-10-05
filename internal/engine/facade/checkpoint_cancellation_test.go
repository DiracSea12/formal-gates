package facade

import (
	"errors"
	"formal-gates/internal/engine/vcs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Cancellation retires published evidence and native resources, while audit
// content and terminal replay remain bound to the accepted parent decision.
func TestCheckpointCancellationRetiresItsPublishedEvidence(t *testing.T) {
	for _, lost := range []bool{false, true} {
		name := "normal"
		if lost {
			name = "native-response-lost"
		}
		t.Run(name, func(t *testing.T) {
			master, child, parent, receipt := deliveredChildCheckpoint(t, validationDocument)
			event := abortCreationChild(t, master.f)
			authority, ok := driverState(t, master).AbortEvent()
			if !ok {
				t.Fatal("parent abort has no accepted authority")
			}
			if lost {
				if err := child.f.engine.CancelChild(driverState(t, master), emptyFingerprint()); err != nil {
					t.Fatal(err)
				}
				transport := &checkpointCleanupInterruption{Runner: vcs.ExecRunner{}}
				child.f.vcs = vcs.New(child.f.vcs.Root(), transport)
				panicked := false
				func() {
					defer func() {
						if got := recover(); got != nil {
							if got != "native cleanup response lost" {
								panic(got)
							}
							panicked = true
						}
					}()
					if _, err := child.f.Drive(); err != nil {
						t.Fatal(err)
					}
				}()
				if !panicked || len(transport.removed) != 1 {
					t.Fatal("native cleanup did not reach the actual lost response")
				}
				if _, err := os.Stat(transport.removed[0]); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("interruption preceded the cleanup side effect", err)
				}
				if _, err := os.Stat(filepath.Join(child.f.stateDir, "slice-receipt.json")); err != nil {
					t.Fatal("checkpoint retired before terminal native cleanup", err)
				}
				var err error
				child.f, err = Open(master.f.root, child.f.runID)
				if err != nil {
					t.Fatal(err)
				}
				child.f.vcs = vcs.New(child.f.vcs.Root(), transport)
				if _, err := child.f.Drive(); err != nil {
					t.Fatal(err)
				}
				for i, path := range transport.removed {
					for _, later := range transport.removed[i+1:] {
						if path == later {
							t.Fatal("recovery repeated native cleanup", path)
						}
					}
				}
			}
			master.drive()
			if master.run.Status != "ABORTED" {
				t.Fatal("parent cancellation did not finish", master.run.Status)
			}
			for _, plan := range parent.ChildCreation.Children {
				f, err := Open(master.f.root, plan.RunID)
				if err != nil {
					t.Fatal(err)
				}
				run, err := f.Show()
				if err != nil || run.Status != "ABORTED" || run.Termination == nil || !run.Termination.ResourcesCleaned() {
					t.Fatal("child cancellation did not retire responsibility", err)
				}
				if !reflect.DeepEqual(run.lastEvent, &authority) {
					t.Fatal("child summary lost accepted parent authority")
				}
				if _, copied := run.Termination.Events[string(event.ID)]; copied {
					t.Fatal("child copied parent replay identity")
				}
				if plan.RunID == child.f.runID && (run.Termination.SliceReceipt == nil || !reflect.DeepEqual(*run.Termination.SliceReceipt, receipt)) {
					t.Fatal("audit summary lost the actual checkpoint")
				}
				for _, name := range []string{"slice-receipt.json", "state.json", "cleanup.intent.json", "terminal-summary.intent.json"} {
					if _, err := os.Stat(filepath.Join(f.stateDir, name)); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("active payload survived cancellation", name, err)
					}
				}
				for _, resource := range run.Termination.NativeResources {
					if _, err := os.Stat(resource.Path); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("child worktree survived cancellation", resource.Path, err)
					}
					if _, err := (vcs.ExecRunner{}).Run(master.f.root, "git", "show-ref", "--verify", resource.Ref); err == nil {
						t.Fatal("child ref survived cancellation", resource.Ref)
					}
				}
				resource := parent.NativeResources[plan.ResourceID]
				if _, err := os.Stat(resource.Path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("child target survived cancellation", err)
				}
				if _, err := (vcs.ExecRunner{}).Run(master.f.root, "git", "show-ref", "--verify", resource.Ref); err == nil {
					t.Fatal("child target ref survived cancellation")
				}
				before := installationFiles(t, f.stateDir)
				if _, err := f.Drive(); err != nil {
					t.Fatal(err)
				}
				f, err = Open(master.f.root, plan.RunID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.Drive(); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, installationFiles(t, f.stateDir)) {
					t.Fatal("terminal replay rewrote child audit")
				}
			}
			before := installationFiles(t, master.f.stateDir)
			master.reopen()
			master.drive()
			if master.run.Status != "ABORTED" || !reflect.DeepEqual(before, installationFiles(t, master.f.stateDir)) {
				t.Fatal("terminal replay rewrote parent audit")
			}
		})
	}
}

// The interruption follows an actual successful worktree removal.
type checkpointCleanupInterruption struct {
	vcs.Runner
	removed []string
}

func (r *checkpointCleanupInterruption) Run(dir, command string, args ...string) (string, error) {
	out, err := r.Runner.Run(dir, command, args...)
	if err == nil && command == "git" && len(args) == 4 && args[0] == "worktree" && args[1] == "remove" {
		r.removed = append(r.removed, args[3])
		if len(r.removed) == 1 {
			panic("native cleanup response lost")
		}
	}
	return out, err
}
