package facade

import (
	"bytes"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Confirmation identity is checked through actual independent Git delivery and a retained user control.
func deliveredChildCheckpoint(t *testing.T, currentDocument string) (*validationDriver, *validationDriver, *protocol.State, protocol.SliceReceipt) {
	t.Helper()
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
	d := nativeMasterAt(t, root, base, nil)
	passPreDevelopmentReviews(t, d, splitCommandPlan())
	originalIntake := driverState(t, d).IntakeConfirmationReceipt
	if currentDocument != validationDocument {
		if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(currentDocument), 0600); err != nil {
			t.Fatal(err)
		}
		confirmValidationDocument(d, "", nil, currentDocument)
		d.revision = driverState(t, d).RequirementRevision()
		if d.revision == originalIntake.RequirementRevision {
			t.Fatal("normal confirmation did not change revision")
		}
	}
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
		if c.RequirementRevision() != d.revision || c.SliceChild.RequirementRevision != d.revision || !reflect.DeepEqual(c.IntakeConfirmationReceipt, originalIntake) {
			t.Fatal("inherited current requirement or original intake changed")
		}
		paths[c.NativeRoot] = true
		body, err := os.ReadFile(filepath.Join(c.NativeRoot, "startup.txt"))
		if err != nil || !bytes.Equal(body, content) {
			t.Fatal("child lost startup content", err)
		}
		body, err = os.ReadFile(filepath.Join(c.NativeRoot, "requirements.md"))
		if err != nil || string(body) != currentDocument {
			t.Fatal("child lost its complete confirmed requirement material", err)
		}
		committed, err := runner.RunRaw(c.NativeRoot, "git", nil, "show", h+":requirements.md")
		if err != nil || string(committed) != validationDocument {
			t.Fatal("child replaced the committed native requirement artifact", err)
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

	child.drive()
	id, input := planBatchAction(child, "command")
	assertConfirmedMaterial(t, child, input, currentDocument)
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
	if head, err := vcs.New(w, nil).Resolve(); err != nil || head != h {
		t.Fatal("worker changed the issued source HEAD", err)
	}
	child.git = vcs.New(w, nil)
	evidence := batchEvidence(input)
	evidence.Paths = append(evidence.Paths, "app_test.go")
	child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: evidence})
	child.drive()
	for _, gate := range driverState(t, child).SelectedGates {
		id, input := child.gateAction(gate)
		assertConfirmedMaterial(t, child, input, currentDocument)
		child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	}
	if _, err := f.PrepareCandidate("child delivery"); err != nil {
		t.Fatal(err)
	}
	child.drive()
	receipt := readSliceReceipt(t, child)
	if receipt.Receipt.ChildRunID != s.SliceChildren[plan.SliceID] || receipt.Receipt.MasterRunID != d.f.runID || receipt.Receipt.SliceID != plan.SliceID {
		t.Fatal("checkpoint lost the assigned child identity")
	}
	for _, target := range []string{w, f.vcs.Root()} {
		for path, want := range map[string][]byte{"startup.txt": content, "app_test.go": []byte("package main\n")} {
			command := exec.Command("git", "show", receipt.Receipt.Snapshot+":"+path)
			command.Dir = target
			body, err := command.Output()
			if err != nil || !bytes.Equal(body, want) {
				t.Fatalf("delivered blob %s lost its complete bytes: %q (%v)", path, body, err)
			}
		}
		mode, modeErr := runner.Run(target, "git", "ls-tree", receipt.Receipt.Snapshot, "tool")
		tree, treeErr := vcs.New(target, nil).TreeDigest(receipt.Receipt.Snapshot)
		if modeErr != nil || !strings.HasPrefix(mode, "100755 ") || treeErr != nil || tree != receipt.Receipt.Tree {
			t.Fatal("candidate/promotion/checkpoint lost accepted startup mode or tree", target, modeErr, treeErr)
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
	if receipt.Receipt.RequirementRevision != d.revision || receipt.Receipt.Validation.RequirementRevision != d.revision {
		t.Fatal("checkpoint did not carry the current requirement")
	}
	return d, child, s, receipt
}

func assertConfirmedMaterial(t *testing.T, d *validationDriver, input protocol.ValidationInput, document string) {
	t.Helper()
	current := driverState(t, d).Validation.Document
	if input.RequirementDocument.Path != "requirements.md" || input.RequirementDocument.Content != strings.ReplaceAll(document, "\r\n", "\n") || input.RequirementDocument.ContentDigest != strings.TrimPrefix(d.revision, "sha256:") || !reflect.DeepEqual(input.RequirementDocument, current) {
		t.Fatal("worker input lost the complete current material or its independent identity", input.RequirementDocument)
	}
}

// Confirmed full material and native content survive delivery and retained user control.
func TestConfirmedRequirementDeliveryRetainsHumanBoundary(t *testing.T) {
	d, child, _, _ := deliveredChildCheckpoint(t, validationDocument+"\n")
	f, root := child.f, d.f.root
	request, err := protocol.NewRequestEvent("checkpoint-user-control", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Cancel"})
	if err != nil {
		t.Fatal(err)
	}
	_, pending, err := f.Submit(request)
	if err != nil {
		t.Fatal(err)
	}
	next, err := f.Next()
	if err != nil {
		t.Fatal(err)
	}
	if next.Kind != decision.KindAsk || len(pending.AvailableActions) == 0 {
		t.Fatalf("checkpoint user boundary=%s available=%v", next.Kind, pending.AvailableActions)
	}
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	next, err = reopened.Next()
	if err != nil || next.Kind != decision.KindAsk {
		t.Fatal("reopen lost checkpoint human decision", err)
	}

	abortCreationChild(t, d.f)
	final, cancelErr := d.f.Drive()
	if cancelErr != nil {
		t.Fatalf("normal parent cancellation after checkpoint: %v", cancelErr)
	}
	if final.Status != "ABORTED" {
		t.Fatalf("parent checkpoint cancellation status=%s", final.Status)
	}

}
