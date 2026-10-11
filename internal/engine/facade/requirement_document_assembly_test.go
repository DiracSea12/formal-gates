package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfirmedRequirementDocumentAssembly(t *testing.T) {
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "implementation", "review input")
	d := completePromotionPipeline(t, nil, root, identities[0])
	original, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	oldCandidate := *d.run.Validation.Candidate
	submit := func(input ControlInput) {
		t.Helper()
		c, e := d.f.PrepareControl(input)
		if e != nil {
			t.Fatal(e)
		}
		_, d.run, e = d.f.SubmitControl(c)
		if e != nil {
			t.Fatal(e)
		}
	}
	submit(ControlInput{Request: protocol.ControlRequestRequirementChange})
	req, err := d.f.request()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, req.IntakeConfirmationReceipt.RequirementSource)
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	newDocument := append(document, '\n')
	if err = os.WriteFile(path, newDocument, 0600); err != nil {
		t.Fatal(err)
	}
	work := make([]WorkChoice, len(d.run.RequirementWork))
	for i := range work {
		work[i] = WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "Explicitly retain all existing PASS for the same public command"}
	}
	submit(ControlInput{Request: protocol.ControlRequestRequirementChange, Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationChanged, Work: work, Impact: protocol.RequirementImpact{ChangedItems: []string{"new confirmed document"}, TransitiveImpacts: []string{"previous validation affected or unknown"}, CarryReasons: []string{"explicit user selection"}}}})
	submit(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Confirm all retained PASS and the new full document"})

	d.reopen()
	lostReceipt := false
	for i := 0; i < 12; i++ {
		if _, _, err := d.f.engine.Drive(emptyFingerprint()); err != nil {
			t.Fatal(err)
		}

		before, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !lostReceipt {
			for _, intent := range before.State.PendingHostActions {
				if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
					continue
				}
				var p protocol.CandidateParameters
				if err := decodeNativeParams(intent, &p); err != nil {
					t.Fatal(err)
				}
				if p.Stage != "REVIEW_READY" || p.Base != oldCandidate.Identity {
					continue
				}
				_, complete, err := d.f.nativeEvidence(before.State, intent, true)
				if err != nil || !complete {
					t.Fatalf("candidate completion prefix: %v %t", err, complete)
				}
				persisted, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if _, known := persisted.State.HostActionReceipts[intent.ActionID]; known {
					t.Fatal("lost response unexpectedly has a receipt")
				}
				lostReceipt = true
				d.reopen()
			}
		}
		if _, err := d.f.driveNative(); err != nil {
			t.Fatal(err)
		}
		current, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if current.State.FinalValidationComplete(d.f.definition) {
			break
		}
	}
	current, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !lostReceipt {
		t.Fatal("native assembly receipt window was not exercised")
	}
	if !reflect.DeepEqual(original.State.Results, current.State.Results) {
		t.Fatal("raw results changed")
	}
	if !current.State.FinalValidationComplete(d.f.definition) {
		t.Fatal("document candidate did not complete")
	}

	wave := &current.State.Validation.Waves[len(current.State.Validation.Waves)-1]
	if !reflect.DeepEqual(original.State.Validation.Waves[0], current.State.Validation.Waves[0]) || !reflect.DeepEqual(original.State.DispatchBindings, current.State.DispatchBindings) {
		t.Fatal("original frozen wave or dispatch identity changed")
	}
	assembly := current.State.RequirementAssemblyReceipt(&current.State.Validation.Waves[0], wave)
	if assembly == "" || wave.Scope != coverage.ScopeAffected {
		t.Fatal("missing actual assembly inheritance")
	}
	for _, target := range wave.Targets {
		if target.Status != "PASS" {
			t.Fatal("original PASS not carried")
		}
		if target.Execution == nil {
			continue
		}
		if len(target.RequiredCases) != 0 {
			t.Fatal("document assembly claims a new execution")
		}
		for _, row := range target.Execution.Records {
			if row.Provenance != coverage.ProvenanceInherited || row.InheritedFromCandidate != oldCandidate.Identity || row.ReuseEvidence != assembly {
				t.Fatal("original execution provenance lost")
			}
		}
	}
	data, _ := json.Marshal(wave)
	var other protocol.ValidationWave
	if err = json.Unmarshal(data, &other); err != nil {
		t.Fatal(err)
	}
	other.Candidate.Identity = "different-production-candidate"
	if current.State.RequirementAssemblyReceipt(&current.State.Validation.Waves[0], &other) != "" {
		t.Fatal("receipt authorizes an unrelated candidate")
	}
	if err = json.Unmarshal(data, &other); err != nil {
		t.Fatal(err)
	}
	other.Coverage.Manifests[0].Cases[0].Oracle += "changed criterion"
	other.Versions[coverage.ReviewBlackbox] = "changed-cases"
	if current.State.RequirementAssemblyReceipt(&current.State.Validation.Waves[0], &other) != "" {
		t.Fatal("changed case semantics inherited")
	}
	receipt := current.State.HostActionReceipts[assembly]
	event, err := protocol.NewAdapterHostActionReceiptEvent(protocol.EventID("native:"+assembly), assembly, "op.candidate", current.State.RunProvider, receipt.Correlation, receipt.PayloadDigest, receipt.Status, "", receipt.AdapterEvidence.Values)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.f.engine.Submit(event, emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	d.reopen()
	head, err := d.f.vcs.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	reject := func(path string, body []byte) {
		t.Helper()
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = d.f.PrepareCandidate("formal-gates delivery " + d.f.runID); err == nil {
			t.Fatal("unconfirmed target bytes promoted")
		}
		if after, err := d.f.vcs.Resolve(); err != nil || after != head {
			t.Fatal("rejected target changed native head")
		}
		if err = os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
	}
	reject(filepath.Join(root, "app.go"), []byte("package main\nfunc main(){println(1)}\n"))
	reject(path, append(append([]byte(nil), newDocument...), []byte("unconfirmed")...))
	message := "formal-gates delivery " + d.f.runID + " " + current.State.BaseSnapshot + " " + wave.Candidate.Identity
	interrupted := &commitFailureRunner{inner: vcs.ExecRunner{}, message: message}
	d.f.vcs = vcs.New(root, interrupted)
	if _, err = d.f.PrepareCandidate(message); err == nil || !strings.Contains(err.Error(), "injected commit interruption") {
		t.Fatalf("expected interrupted native prefix: %v", err)
	}
	if !interrupted.failed {
		t.Fatal("recovery did not cross the commit boundary")
	}
	d.reopen()
	run, err := d.f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	d.run = run
	if d.run.Status != "COMPLETE" {
		t.Fatalf("not sealed: %+v", d.run.Next)
	}
	if d.run.Validation.Candidate.Identity == oldCandidate.Identity {
		t.Fatal("new document did not enter its own candidate")
	}

	delivered, err := d.f.vcs.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	body, err := exec.Command("git", "-C", root, "show", delivered+":"+req.IntakeConfirmationReceipt.RequirementSource).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(newDocument) {
		t.Fatal("delivered document differs from confirmation")
	}
}
