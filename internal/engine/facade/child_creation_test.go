package facade

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/lifecycle"
)

func nativeMasterDriver(t *testing.T) *validationDriver {
	t.Helper()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	return nativeMasterAt(t, root, base)
}

func nativeMasterAt(t *testing.T, root, base string) *validationDriver {
	t.Helper()
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(validationDocument)))
	receipt := testReceipt()
	receipt.RequirementRevision, receipt.Artifacts[0].Revision = revision, revision
	f, run, err := Start(StartOptions{Root: root, Capacity: 16, Admission: testAdmission(t, root, "candidate", "sha256:package", lifecycle.ProviderDefault), Request: StartRequest{RunID: "validation", Route: "regular", Provider: lifecycle.ProviderDefault, BaseSnapshot: base, IntakeConfirmationReceipt: receipt}})
	if err != nil {
		t.Fatal(err)
	}
	d := &validationDriver{t: t, f: f, run: run, revision: revision, provider: f.admission.Provider, git: f.vcs}
	d.drive()
	return d
}

// createdSliceFacade consumes the Controller's actual planned identity. An
// owner source, when supplied, is bound before the child's first dispatch.
func createdSliceFacade(t *testing.T, master *Facade, sliceID string, owner OwnerBinding) (*Facade, Run, error) {
	t.Helper()
	unlock, err := master.acquireNativeWindow()
	if err != nil {
		return nil, Run{}, err
	}
	snap, err := master.engine.Load()
	if err == nil && snap.State.ChildCreation == nil {
		var digest string
		digest, err = master.vcs.TreeDigest(snap.State.AcceptedNativeSnapshot())
		if err == nil {
			err = master.engine.PrepareChildren(digest, emptyFingerprint())
		}
		if err == nil {
			snap, err = master.engine.Load()
		}
	}
	var id string
	if err == nil {
		for _, plan := range snap.State.ChildCreation.Children {
			if plan.SliceID != sliceID {
				continue
			}
			id = plan.RunID
			_, err = master.prepareChild(snap.State, plan, owner)
			break
		}
		if id == "" {
			err = fmt.Errorf("slice is not part of the confirmed topology")
		}
	}
	unlock()
	if err != nil {
		return nil, Run{}, err
	}
	if _, err := master.Drive(); err != nil {
		return nil, Run{}, err
	}
	child, err := Open(master.root, id)
	if err != nil {
		return nil, Run{}, err
	}
	run, err := child.Show()
	return child, run, err
}

func prepareChildFrontier(t *testing.T, master *validationDriver) *protocol.State {
	t.Helper()
	unlock, err := master.f.acquireNativeWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := master.f.driveChildren(); err != nil {
		t.Fatal(err)
	}
	s := driverState(t, master)
	if s.ChildCreation == nil || len(s.SliceChildren) != 0 || len(s.PendingHostActions) != len(s.Topology.Slices) {
		t.Fatalf("preparing frontier: %+v", s.ChildCreation)
	}
	for _, plan := range s.ChildCreation.Children {
		child, err := Open(master.f.root, plan.RunID)
		if err != nil {
			t.Fatal(err)
		}
		state, err := child.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if state.State.Phase != runtime.PhasePreparing || state.State.InitialSnapshot != "" || len(state.State.PendingActions) != 0 {
			t.Fatal("unproven child was unlocked")
		}
	}
	return s
}

func TestControllerCreatesIndependentChildrenFromAcceptedContent(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	content := []byte("startup project content\n")
	if err := os.WriteFile(filepath.Join(root, "startup.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tool"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	runner := vcs.ExecRunner{}
	if _, err := runner.Run(root, "git", "add", "startup.txt", "tool"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "commit", "-m", "startup contents"); err != nil {
		t.Fatal(err)
	}
	h, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	d := nativeMasterAt(t, root, base)
	passPreDevelopmentReviews(t, d, commandPlan())
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.revision, Document: validationDocument}}})
	d.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})})
	prepared := prepareChildFrontier(t, d)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := d.f.Drive(); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	s := driverState(t, d)
	if len(s.SliceChildren) != 2 || !reflect.DeepEqual(s.ChildCreation, prepared.ChildCreation) {
		t.Fatal("repeated controller changed intent or released a partial map")
	}
	paths := map[string]bool{root: true}
	for _, plan := range s.ChildCreation.Children {
		child, err := Open(root, s.SliceChildren[plan.SliceID])
		if err != nil {
			t.Fatal(err)
		}
		actual, err := child.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		c := actual.State
		if paths[c.NativeRoot] || c.BaseSnapshot != base || c.InitialSnapshot != h || c.Phase != runtime.PhaseTopologyAndRoute || child.root != root || child.admission.Root != root || child.vcs.Root() != c.NativeRoot || child.envelope != d.f.envelope {
			t.Fatalf("child root/identity carrier: %+v", c.SliceChild)
		}
		paths[c.NativeRoot] = true
		body, err := os.ReadFile(filepath.Join(c.NativeRoot, "startup.txt"))
		if err != nil || !bytes.Equal(body, content) {
			t.Fatal("child lost startup content", err)
		}
		mode, err := runner.Run(c.NativeRoot, "git", "ls-tree", h, "tool")
		if err != nil || !strings.HasPrefix(mode, "100755 ") {
			t.Fatal("child lost executable mode", mode, err)
		}
		if _, proven := s.ChildWorkspaceProof(plan); !proven {
			t.Fatal("child has no exact typed workspace proof")
		}
	}
	current, err := d.f.vcs.Resolve()
	if err != nil || current != h {
		t.Fatal("child creation moved parent HEAD")
	}
	before, _ := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
	if _, err := d.f.Drive(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("released creation replay rewrote master state")
	}
	plan := s.ChildCreation.Children[0]
	f, err := Open(root, s.SliceChildren[plan.SliceID])
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	child := &validationDriver{t: t, f: f, run: run, revision: d.revision, git: f.vcs}
	child.confirmSliceRoute(protocol.RouteDecision{Mode: "default", Gates: []string{"quality"}}, sliceLanePlan())
	child.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: child.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: child.revision, Document: validationDocument}}})
	child.drive()
	id, input := planBatchAction(child, "command")
	w := input.Workspace.Path
	if w == root || w == f.vcs.Root() || input.Workspace.Source != h {
		t.Fatal("child development did not use an independent source from its accepted startup")
	}
	body, err := os.ReadFile(filepath.Join(w, "startup.txt"))
	if err != nil || !bytes.Equal(body, content) {
		t.Fatal("development source lost startup bytes", err)
	}
	if err := os.WriteFile(filepath.Join(w, "app_test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshotCommitOne(t, w, "implement child command")
	child.git = vcs.New(w, nil)
	evidence := batchEvidence(input)
	evidence.Paths = append(evidence.Paths, "app_test.go")
	child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: evidence})
	child.drive()
	completeSliceGates(t, child)
	if _, err := f.PrepareCandidate("child delivery"); err != nil {
		t.Fatal(err)
	}
	child.drive()
	receipt := readSliceReceipt(t, child)
	if receipt.Receipt.ChildRunID != s.SliceChildren[plan.SliceID] || receipt.Receipt.MasterRunID != d.f.runID || receipt.Receipt.SliceID != plan.SliceID {
		t.Fatal("checkpoint lost the assigned child identity")
	}
	for _, target := range []string{w, f.vcs.Root()} {
		body, err := runner.Run(target, "git", "show", receipt.Receipt.Snapshot+":startup.txt")
		mode, modeErr := runner.Run(target, "git", "ls-tree", receipt.Receipt.Snapshot, "tool")
		tree, treeErr := vcs.New(target, nil).TreeDigest(receipt.Receipt.Snapshot)
		if err != nil || body != string(content) || modeErr != nil || !strings.HasPrefix(mode, "100755 ") || treeErr != nil || tree != receipt.Receipt.Tree {
			t.Fatal("candidate/promotion/checkpoint lost accepted startup content", target, err, modeErr, treeErr)
		}
	}
	accepted, err := d.f.engine.Load()
	if err != nil || accepted.State.SliceReceipts[f.runID].Digest != receipt.Digest {
		t.Fatal("master did not accept the actual child checkpoint", err)
	}
	current, err = d.f.vcs.Resolve()
	if err != nil || current != h {
		t.Fatal("child delivery moved parent HEAD", err)
	}
}

func TestPublicStartRejectsChildBeforeRunLockOrWrites(t *testing.T) {
	d := splitMaster(t)
	for _, route := range []string{"regular", "lightweight", ""} {
		opts := sliceChildStart(d, "caller-child", "lane-core", sliceChildReceipt(d.revision))
		opts.Request.Route = route
		if _, _, err := Start(opts); err == nil || !strings.Contains(err.Error(), "master controller") {
			t.Fatal("public child start accepted", err)
		}
	}
	if _, err := os.Stat(filepath.Join(d.f.root, EngineNamespace, "caller-child")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("public child start wrote a prefix")
	}
	if len(driverState(t, d).SliceChildren) != 0 {
		t.Fatal("public child start published a map")
	}
}

type childWorkspaceInterruption struct {
	vcs.Runner
	point       string
	interrupted bool
}

func (r *childWorkspaceInterruption) Run(dir, command string, args ...string) (string, error) {
	out, err := r.Runner.Run(dir, command, args...)
	if err == nil && !r.interrupted && command == "git" && len(args) > 0 && args[0] == r.point {
		r.interrupted = true
		panic("native process response lost")
	}
	return out, err
}

func TestChildWorkspaceReopenPreservesExecutedPrefixes(t *testing.T) {
	for _, point := range []string{"worktree", "update-ref"} {
		t.Run(point, func(t *testing.T) {
			d := splitMaster(t)
			before := prepareChildFrontier(t, d)
			r := &childWorkspaceInterruption{Runner: vcs.ExecRunner{}, point: point}
			d.f.vcsRunner = r
			d.f.vcs = vcs.New(d.f.root, r)
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("interruption was not reached")
					}
				}()
				_, _ = d.f.Drive()
			}()
			if !r.interrupted {
				t.Fatal("native side effect was not reached")
			}
			parent, err := Open(d.f.root, d.f.runID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parent.Drive(); err != nil {
				t.Fatal(err)
			}
			after, err := parent.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before.ChildCreation, after.State.ChildCreation) || len(after.State.SliceChildren) != 2 {
				t.Fatal("recovery changed planned identities or released partial membership")
			}
			for _, plan := range after.State.ChildCreation.Children {
				if _, proven := after.State.ChildWorkspaceProof(plan); !proven {
					t.Fatal("executed prefix has no original receipt")
				}
			}
		})
	}
}

func TestChildWorkspaceReceiptRejectsWrongContentWithoutRelease(t *testing.T) {
	d := splitMaster(t)
	s := prepareChildFrontier(t, d)
	for _, intent := range s.PendingHostActions {
		var p protocol.WorkspaceParameters
		if err := decodeNativeParams(intent, &p); err != nil {
			t.Fatal(err)
		}
		r := p.Resource
		r.Identity = r.Source
		r.Status = "READY"
		v := protocol.WorkspaceEvidence{ParametersDigest: intent.PayloadDigest, Resource: r, Digest: strings.Repeat("f", 40)}
		values, err := evidenceFields(v)
		if err != nil {
			t.Fatal(err)
		}
		event, err := protocol.NewAdapterHostActionReceiptEvent("wrong-content", intent.ActionID, "op.workspace", s.RunProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", values)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
		if _, _, err := d.f.Submit(event); err == nil {
			t.Fatal("wrong child source digest accepted")
		}
		after, _ := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
		if !bytes.Equal(before, after) {
			t.Fatal("wrong receipt changed creation state")
		}
		break
	}
	if len(driverState(t, d).SliceChildren) != 0 {
		t.Fatal("wrong receipt released children")
	}
}

func TestChildReleaseReopenUsesCommittedMapAfterLostResponse(t *testing.T) {
	for _, point := range []persistence.FaultPoint{persistence.FaultIntentBefore, persistence.FaultReplaceAfter, persistence.FaultCommitResponseLost} {
		t.Run(string(point), func(t *testing.T) {
			d := splitMaster(t)
			prepareChildFrontier(t, d)
			unlock, err := d.f.acquireNativeWindow()
			if err != nil {
				t.Fatal(err)
			}
			for {
				progress, err := d.f.driveNative()
				if err != nil {
					unlock()
					t.Fatal(err)
				}
				if !progress {
					break
				}
			}
			unlock()
			once := false
			store, err := persistence.NewStore(d.f.stateDir, persistence.Config{PackageDigest: d.f.envelope.PackageDigest, DefinitionSource: d.f.envelope.DefinitionSource, InstalledTargetIdentity: d.f.envelope.InstalledTargetIdentity, AcquireAdmission: d.f.admission.acquire, FaultInjector: func(at persistence.FaultPoint) error {
				if at == point && !once {
					once = true
					return fmt.Errorf("creation commit interrupted")
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			eng, err := protocol.New(store, protocol.Config{NativeRoot: d.f.root, Definition: d.f.definition, Registry: definition.Registry(), Capacity: 16, CostSource: CostSourceBridge(d.f.root, d.f.runID), ClaimDispatch: dispatchClaimBridge(d.f.root, d.f.runID, d.f.admission)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			d.f.store, d.f.engine = store, eng
			if _, err := d.f.Drive(); err == nil {
				t.Fatal("commit interruption did not surface")
			}
			parent, err := Open(d.f.root, d.f.runID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parent.Drive(); err != nil {
				t.Fatal(err)
			}
			s, err := parent.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(s.State.SliceChildren) != 2 {
				t.Fatal("recovered map is partial")
			}
			for _, id := range s.State.SliceChildren {
				child, err := Open(parent.root, id)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := child.Drive(); err != nil {
					t.Fatal(err)
				}
				actual, err := child.engine.Load()
				if err != nil || actual.State.Phase == runtime.PhasePreparing {
					t.Fatal("committed map did not activate child", err)
				}
			}
		})
	}
}

func abortCreationChild(t *testing.T, child *Facade) {
	t.Helper()
	request, err := protocol.NewRequestEvent("abort-request", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := child.Submit(request)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	for _, ask := range run.PendingAsks {
		if ask.RequestID == "abort-request" {
			token = ask.Token
		}
	}
	decide, err := protocol.NewDecideEvent("abort-decision", "abort-request", token, "abort")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := child.Submit(decide); err != nil {
		t.Fatal(err)
	}
}

func TestPreparingChildAbortStopsCreationAndMasterAbortCleansTargets(t *testing.T) {
	d := splitMaster(t)
	s := prepareChildFrontier(t, d)
	plan := s.ChildCreation.Children[0]
	child, err := Open(d.f.root, plan.RunID)
	if err != nil {
		t.Fatal(err)
	}
	abortCreationChild(t, child)
	if _, err := d.f.Drive(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.GitResources[plan.ResourceID].Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled child acquired a workspace")
	}
	if len(driverState(t, d).SliceChildren) != 0 {
		t.Fatal("cancelled child released the map")
	}
	abortCreationChild(t, d.f)
	if _, err := d.f.Drive(); err != nil {
		t.Fatal(err)
	}
	for _, p := range s.ChildCreation.Children {
		c, err := Open(d.f.root, p.RunID)
		if err != nil {
			t.Fatal(err)
		}
		run, err := c.Show()
		if err != nil || run.Status != "ABORTED" {
			t.Fatal("orphan preparing child", err)
		}
		if _, err := os.Stat(s.GitResources[p.ResourceID].Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("child target survived master cancellation")
		}
	}
}

func TestPreparingChildDriveUsesParentThenChildWindow(t *testing.T) {
	d := splitMaster(t)
	s := prepareChildFrontier(t, d)
	child, err := Open(d.f.root, s.ChildCreation.Children[0].RunID)
	if err != nil {
		t.Fatal(err)
	}
	unparent, err := d.f.acquireNativeWindow()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := child.Drive(); done <- err }()
	unchild, err := child.acquireNativeWindow()
	if err != nil {
		unparent()
		t.Fatal(err)
	}
	unchild()
	unparent()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child Drive waited in reverse window order")
	}
	state, err := child.engine.Load()
	if err != nil || state.State.Phase != runtime.PhasePreparing {
		t.Fatal("unreleased child activated", err)
	}
}

func TestMasterAbortWaitsForReleasedChildEffectsBeforeTargetCleanup(t *testing.T) {
	for _, inFlight := range []bool{false, true} {
		t.Run(fmt.Sprintf("worker=%t", inFlight), func(t *testing.T) {
			master := splitMaster(t)
			master.drive()
			parent := driverState(t, master)
			children := make([]*validationDriver, len(parent.ChildCreation.Children))
			actions := make([][]string, len(children))
			var paths []string
			for i, plan := range parent.ChildCreation.Children {
				f, err := Open(master.f.root, plan.RunID)
				if err != nil {
					t.Fatal(err)
				}
				run, err := f.Show()
				if err != nil {
					t.Fatal(err)
				}
				child := &validationDriver{t: t, f: f, run: run, revision: master.revision, git: f.vcs}
				children[i] = child
				paths = append(paths, parent.GitResources[plan.ResourceID].Path)
				if !inFlight {
					continue
				}
				child.confirmSliceRoute(protocol.RouteDecision{Mode: "default", Gates: []string{"quality"}}, sliceLanePlan())
				child.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: child.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: child.revision, Document: validationDocument}}})
				child.drive()
				for id, input := range child.run.ValidationInputs {
					actions[i] = append(actions[i], id)
					paths = append(paths, input.Workspace.Path)
					child.receipt(id)
				}
				if len(actions[i]) == 0 {
					t.Fatal("child has no actual issued worker")
				}
			}
			abortCreationChild(t, master.f)
			master.drive()
			if inFlight {
				if master.run.Status != "ABORTING" {
					t.Fatal("master ended while child work was in flight")
				}
				for _, path := range paths {
					if _, err := os.Stat(path); err != nil {
						t.Fatal("master removed an active child workspace", err)
					}
				}
				if err := master.f.engine.BeginCleanup(emptyFingerprint()); err == nil {
					t.Fatal("master cleanup accepted without actual child terminal facts")
				}
				for _, child := range children {
					state := driverState(t, child)
					if state.SliceChild.ParentAbort == nil || len(state.ExecutableHostActions()) == 0 {
						t.Fatal("waiting sibling blocked parent cancellation propagation")
					}
				}
				var err error
				master.f, err = Open(master.f.root, master.f.runID)
				if err != nil {
					t.Fatal(err)
				}
				master.drive()
				for i := len(children) - 1; i >= 0; i-- {
					child := children[i]
					child.run, err = child.f.Show()
					if err != nil {
						t.Fatal(err)
					}
					for _, action := range actions[i] {
						child.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: action, Provider: child.host(), Outcome: protocol.OutcomeRuntimeError, PayloadDigest: "late-" + action, FailureClass: "TRANSIENT_ENGINE_ERROR"}})
					}
					for id, intent := range child.run.AdapterIntents {
						if intent.Terminate == nil {
							t.Fatal("parent cancellation signed business work")
						}
						for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
							event, err := protocol.NewLifecycleEventEvent(child.eventID(), child.host(), intent.Terminate.Identity, edge)
							if err != nil {
								t.Fatal(err)
							}
							child.submit(event)
						}
						event, err := protocol.NewAgentHostActionReceiptEvent(child.eventID(), id, intent.Operation, child.host(), intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Terminate.Identity, Event: protocol.LifecycleStop})
						if err != nil {
							t.Fatal(err)
						}
						child.submit(event)
					}
					master.drive()
					if i != 0 {
						if master.run.Status != "ABORTING" {
							t.Fatal("master ignored another active child")
						}
						for _, plan := range parent.ChildCreation.Children {
							if _, err := os.Stat(parent.GitResources[plan.ResourceID].Path); err != nil {
								t.Fatal("master cleaned targets before every child ended", err)
							}
						}
					}
				}
			}
			if master.run.Status != "ABORTED" {
				t.Fatal("master cancellation did not finish", master.run.Status)
			}
			for _, plan := range parent.ChildCreation.Children {
				c, err := Open(master.f.root, plan.RunID)
				if err != nil {
					t.Fatal(err)
				}
				run, err := c.Show()
				if err != nil || run.Status != "ABORTED" || run.Termination == nil || !run.Termination.ResourcesCleaned() {
					t.Fatal("released child survived parent cancellation", err)
				}
				if _, err := os.Stat(parent.GitResources[plan.ResourceID].Path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("child target survived cancellation")
				}
			}
		})
	}
}
