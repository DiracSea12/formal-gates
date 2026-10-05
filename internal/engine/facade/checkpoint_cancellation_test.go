package facade

import (
	"errors"
	"formal-gates/internal/engine/protocol"
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
			acceptedParent := driverState(t, master)
			authority, ok := acceptedParent.AbortEvent()
			if !ok {
				t.Fatal("parent abort has no accepted authority")
			}
			decision, ok := acceptedParent.AbortDecision()
			request, requested := acceptedParent.Events[decision.RequestID]
			if !ok || !requested {
				t.Fatal("parent abort has no accepted decision and request")
			}
			abort := &protocol.AbortAuthority{Decision: decision, Event: authority, RequestDigest: request.Digest}
			if lost {
				if err := child.f.engine.CancelChild(driverState(t, master), emptyFingerprint()); err != nil {
					t.Fatal(err)
				}
				responseLost := errors.New("native cleanup response lost")
				transport := &checkpointCleanupInterruption{Runner: vcs.ExecRunner{}, responseLost: responseLost}
				child.f.vcs = vcs.New(child.f.vcs.Root(), transport)
				if _, err := child.f.Drive(); !errors.Is(err, responseLost) || len(transport.removed) != 1 {
					t.Fatal("native cleanup did not reach the actual lost response", err)
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
			terminal := master.run.Termination
			if terminal == nil || !terminal.ResourcesCleaned() || len(terminal.SliceReceiptHistory) != 1 || !reflect.DeepEqual(terminal.SliceReceiptHistory[0], receipt) {
				t.Fatal("master audit lost the complete original checkpoint or cleanup")
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
				current := run.Termination.SliceReceipt
				if current == nil || current.Receipt.Status != "ABORTED" || !reflect.DeepEqual(*current, terminal.SliceReceipts[plan.RunID]) || !reflect.DeepEqual(current.Receipt.Abort, abort) || run.Termination.SliceChild == nil || !reflect.DeepEqual(run.Termination.SliceChild.ParentAbort, abort) {
					t.Fatal("current child receipt lost the exact accepted abort authority")
				}
				if plan.RunID == child.f.runID {
					ready, aborted := receipt.Receipt, current.Receipt
					if aborted.Snapshot != ready.Snapshot || aborted.Tree != ready.Tree || aborted.Digest != ready.Digest {
						t.Fatal("abort changed the checkpoint's native identity")
					}
					if aborted.RequirementRevision != ready.RequirementRevision || aborted.OriginRequirementRevision != ready.OriginRequirementRevision || aborted.ChildRunID != ready.ChildRunID || aborted.SliceID != ready.SliceID || aborted.MasterRunID != ready.MasterRunID || aborted.Envelope != ready.Envelope || !reflect.DeepEqual(aborted.Topology, ready.Topology) {
						t.Fatal("abort changed the checkpoint's requirement, ownership or version bindings")
					}
					if aborted.RouteMode != ready.RouteMode || !reflect.DeepEqual(aborted.SelectedGates, ready.SelectedGates) || !reflect.DeepEqual(aborted.Cost, ready.Cost) {
						t.Fatal("abort changed the checkpoint's route or cost")
					}
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
			if err := child.f.handoffSliceReceipt(receipt); err != nil {
				t.Fatal(err)
			}
			master.reopen()
			master.drive()
			if master.run.Status != "ABORTED" || master.run.Termination.SliceReceiptCurrent(receipt) || !reflect.DeepEqual(before, installationFiles(t, master.f.stateDir)) {
				t.Fatal("terminal replay rewrote parent audit")
			}
		})
	}
}

// The interruption follows an actual successful worktree removal.
type checkpointCleanupInterruption struct {
	vcs.Runner
	removed      []string
	responseLost error
}

func (r *checkpointCleanupInterruption) Run(dir, command string, args ...string) (string, error) {
	out, err := r.Runner.Run(dir, command, args...)
	if err == nil && command == "git" && len(args) == 4 && args[0] == "worktree" && args[1] == "remove" {
		r.removed = append(r.removed, args[3])
		if len(r.removed) == 1 {
			return out, r.responseLost
		}
	}
	return out, err
}
