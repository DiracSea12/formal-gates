package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

func nativeFixtureDirectory(t *testing.T) string {
	t.Helper()
	if evidence := os.Getenv("NATIVE_VCS_EVIDENCE"); evidence != "" {
		path, err := os.MkdirTemp(evidence, strings.ReplaceAll(t.Name(), "/", "-")+"-")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("native fixture: %s", path)
		return path
	}
	return t.TempDir()
}

type nativeProviderRunner struct {
	log                 *os.File
	lost                bool
	interruptDelivery   bool
	interruptOperation  string
	deliverySubmissions []string
	synchronizations    int
}

func (r *nativeProviderRunner) Run(dir, command string, args ...string) (string, error) {
	b, e := r.RunRaw(dir, command, nil, args...)
	return strings.TrimSpace(string(b)), e
}
func (r *nativeProviderRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	start := time.Now()
	body, err := (vcs.ExecRunner{}).RunRaw(dir, command, input, args...)
	fmt.Fprintf(r.log, "%s %q dir=%q stdin=%q elapsed=%s error=%v\nstdout=%q\n", command, args, dir, input, time.Since(start), err, body)
	if command == "svn" && len(args) > 0 && args[0] == "update" || command == "p4" && slices.Contains(args, "sync") {
		r.synchronizations++
	}
	operation := ""
	if r.interruptDelivery {
		operation = "delivery"
	}
	if r.interruptOperation != "" {
		operation = r.interruptOperation
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "formal-gates delivery ") && (command == "svn" && args[0] == "commit" || command == "p4" && slices.Contains(args, "submit")) {
			r.deliverySubmissions = append(r.deliverySubmissions, arg)
		}
		if err == nil && !r.lost && operation != "" && strings.HasPrefix(arg, "formal-gates "+operation+" ") {
			r.lost = true
			return nil, fmt.Errorf("fixture lost completed %s response", operation)
		}
	}
	return body, err
}
func nativeProviderFixture(t *testing.T, name string) (string, string, *nativeProviderRunner) {
	t.Helper()
	directory := os.Getenv("NATIVE_VCS_EVIDENCE")
	if directory == "" {
		directory = nativeFixtureDirectory(t)
	}
	log, err := os.Create(filepath.Join(directory, strings.ReplaceAll(t.Name(), "/", "-")+".native-commands.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	runner := &nativeProviderRunner{log: log}
	root := filepath.Join(nativeFixtureDirectory(t), "main")
	command := func(dir, cmd string, args ...string) string {
		t.Helper()
		out, err := runner.Run(dir, cmd, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if name == "svn" {
		repo := filepath.Join(nativeFixtureDirectory(t), "repository")
		command("", "svnadmin", "create", repo)
		command("", "svn", "mkdir", "file://"+repo+"/trunk", "-m", "fixture trunk")
		command("", "svn", "checkout", "file://"+repo+"/trunk", root)
	} else {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().String()
		listener.Close()
		serverRoot := nativeFixtureDirectory(t)
		server := exec.Command("p4d", "-r", serverRoot, "-p", port)
		server.Stdout, server.Stderr = log, log
		if err := server.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { server.Process.Kill(); server.Wait() })
		t.Setenv("P4PORT", port)
		t.Setenv("P4USER", "fixture")
		t.Setenv("P4CLIENT", "main")
		ready := false
		for i := 0; i < 50; i++ {
			if _, err := runner.Run("", "p4", "info"); err == nil {
				ready = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !ready {
			t.Fatal("private p4d did not start")
		}
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		spec := "Client: main\nOwner: fixture\nRoot: " + root + "\nLineEnd: unix\nView:\n\t//depot/main/... //main/...\n"
		if _, err := runner.RunRaw(root, "p4", []byte(spec), "client", "-i"); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{"go.mod": "module command\n\ngo 1.22\n", "app.go": "package command\nfunc Valid(s string) bool { return s != \"\" }\n", ".gates/requirements.md": validationDocument} {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	p := vcs.NewNative(root, name, runner)
	if err := p.Track([]string{"go.mod", "app.go", ".gates/requirements.md"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit("fixture project"); err != nil {
		t.Fatal(err)
	}
	base, err := p.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return root, base, runner
}
func startNativeProviderDriver(t *testing.T, name string) (*validationDriver, *nativeProviderRunner) {
	t.Helper()
	root, base, runner := nativeProviderFixture(t, name)
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(validationDocument)))
	receipt := testReceipt()
	receipt.RequirementSource, receipt.Artifacts[0].Path = ".gates/requirements.md", ".gates/requirements.md"
	receipt.RequirementRevision, receipt.Artifacts[0].Revision = revision, revision
	f, run, err := Start(StartOptions{Root: root, Capacity: 16, VCSRunner: runner, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider), Request: StartRequest{VCS: name, RunID: "validation", Route: "regular", Provider: DefaultProvider, BaseSnapshot: base, IntakeConfirmationReceipt: receipt}})
	if err != nil {
		t.Fatal(err)
	}
	d := &validationDriver{t: t, f: f, run: run, revision: revision, git: f.vcs}
	d.drive()
	return d, runner
}
func reopenNativeProviderDriver(t *testing.T, d *validationDriver, name string, runner *nativeProviderRunner) {
	t.Helper()
	d.reopen()
	state := driverState(t, d)
	d.f.vcsRunner = runner
	d.f.vcs = vcs.NewNative(d.f.root, name, runner).At(state.NativeRoot, state.NativeHandle)
}

func nativeIncompleteState(t *testing.T, d *validationDriver, operation string) protocol.HostActionIntent {
	t.Helper()
	state := driverState(t, d)
	for _, intent := range state.PendingHostActions {
		if intent.Adapter == nil || string(intent.Adapter.Operation) != operation {
			continue
		}
		if _, accepted := state.HostActionReceipts[intent.ActionID]; accepted {
			continue
		}
		if len(state.HostActionFailures[intent.ActionID]) != 0 || !state.NativeActionWritable(intent, d.f.definition) {
			t.Fatal("observed effect lost its original execution responsibility")
		}
		if directory := os.Getenv("NATIVE_VCS_EVIDENCE"); directory != "" {
			body, err := json.MarshalIndent(state, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, strings.ReplaceAll(t.Name(), "/", "-")+"-"+operation+"-incomplete-state.json")
			if err := os.WriteFile(path, body, 0644); err != nil {
				t.Fatal(err)
			}
		}
		return intent
	}
	t.Fatal("lost response has no original pending native intent")
	return protocol.HostActionIntent{}
}

func nativeWhiteboxReady(t *testing.T, d *validationDriver) {
	t.Helper()
	dev, di := d.action("engine.development.batch")
	design, qi := d.action("engine.qa.design")
	if len(d.run.PendingActions) != 2 {
		t.Fatal("development and blackbox design are not concurrent")
	}
	manifest := simpleManifest(qi)
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	id, in := d.action("engine.qa.review.rationale")
	review := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &review})
	d.drive()
	if err := os.WriteFile(filepath.Join(di.Workspace.Path, "app.go"), []byte("package command\nfunc Valid(s string) bool { return len(s) > 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: batchEvidence(di)})
	d.drive()
	id, in = d.action("engine.qa.design")
	if in.Manifest.Binding.ReviewKind != coverage.ReviewWhitebox {
		t.Fatal("whitebox authoring not selected")
	}
	manifest = in.Manifest
	for _, row := range []struct{ id, scenario, ref string }{{"white-success", "SUCCESS", "TestValid"}, {"white-misuse", "MISUSE", "TestInvalid"}} {
		manifest.Cases = append(manifest.Cases, coverage.AcceptanceCase{CaseID: row.id, Mode: coverage.CaseWhitebox, Scenario: row.scenario, TestRef: "app_test.go#" + row.ref, Oracle: "specified input outcome"})
		manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: row.id})
	}
	body := "package command\nimport \"testing\"\nfunc TestValid(t *testing.T){if !Valid(\"ok\"){t.Fatal(\"valid input\")}}\nfunc TestInvalid(t *testing.T){if Valid(\"\"){t.Fatal(\"invalid input\")}}\n"
	if err := os.WriteFile(filepath.Join(in.Workspace.Path, "app_test.go"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.drive()
	id, in = d.action("engine.qa.review.rationale")
	review = approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &review})
	d.drive()
	if d.run.Validation.Candidate == nil {
		t.Fatal("final native candidate not frozen")
	}
	if len(d.run.PendingActions) != 3 {
		t.Fatal("blackbox/whitebox/quality did not issue together")
	}
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.qa.execution" || input.Handler == "engine.gate" {
			if _, err := (vcs.ExecRunner{}).Run(input.Workspace.Path, "go", "test", "-buildvcs=false", "./..."); err != nil {
				t.Fatal(err)
			}
		}
	}
	for len(d.run.ValidationInputs) > 0 {
		var id string
		var input protocol.ValidationInput
		for action, in := range d.run.ValidationInputs {
			if in.Handler == "engine.qa.execution" || in.Handler == "engine.gate" {
				id, input = action, in
				break
			}
		}
		if id == "" {
			t.Fatalf("unexpected native validation task: %+v", d.run.ValidationInputs)
		}
		result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS"}
		if input.Handler == "engine.qa.execution" {
			report := executionFor(input)
			result.Execution = &report
			result.Tool, result.Environment = "go test -buildvcs=false ./...", "native isolated candidate"
		}
		d.business(id, result)
		if len(d.run.ValidationInputs) > 0 {
			d.drive()
		}
	}
}
func TestNativeProviderPublicCandidatePromotionAndCleanup(t *testing.T) {
	for _, name := range []string{"svn", "p4"} {
		for _, lost := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/lost=%t", name, lost), func(t *testing.T) {
				d, runner := startNativeProviderDriver(t, name)
				passPreDevelopmentReviews(t, d, commandPlan())
				d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one native command"}})
				d.confirmRoute(routeDecisionForGates([]string{"blackbox", "whitebox", "quality"}))
				d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.revision, Document: validationDocument}}})
				if lost {
					runner.interruptOperation = "workspace"
					if _, err := d.f.Drive(); err == nil || !runner.lost {
						t.Fatalf("completed workspace response not lost: %v", err)
					}
					nativeIncompleteState(t, d, "op.workspace")
					reopenNativeProviderDriver(t, d, name, runner)
					runner.interruptOperation, runner.lost = "", false
				}
				d.drive()
				nativeWhiteboxReady(t, d)
				candidate := d.run.Validation.Candidate.Identity
				digest := d.run.Validation.Candidate.Digest
				runner.interruptDelivery = lost
				_, err := d.f.PrepareCandidate("verified native candidate")
				var original protocol.HostActionIntent
				if lost {
					if err == nil || !runner.lost {
						t.Fatalf("native completed delivery response not lost: %v", err)
					}
					original = nativeIncompleteState(t, d, "op.candidate")
					reopenNativeProviderDriver(t, d, name, runner)
				} else if err != nil {
					t.Fatal(err)
				}
				errors := make(chan error, 2)
				for i := 0; i < 2; i++ {
					go func() { _, err := d.f.PrepareCandidate("verified native candidate"); errors <- err }()
				}
				for i := 0; i < 2; i++ {
					if err := <-errors; err != nil {
						t.Fatal(err)
					}
				}
				state := driverState(t, d)
				if state.CandidatePromotion == nil || state.CandidatePromotion.Provider != name || state.PromotedDigest != digest {
					t.Fatal("promotion receipt lost native provider/full digest")
				}
				proof := state.CandidatePromotion
				if proof.Source != candidate || proof.SourceDigest != digest || proof.FinalDigest != digest || lost && proof.OperationID != original.ActionID {
					t.Fatalf("promotion did not preserve the original candidate/operation: %+v", proof)
				}
				retained := state.ReconciledEffects[proof.OperationID].Intent
				if retained == nil || retained.ActionID != proof.OperationID {
					t.Fatal("promotion did not retain its settled original intent")
				}
				var params protocol.CandidateParameters
				if err := decodeNativeParams(*retained, &params); err != nil || params.Promotion == nil {
					t.Fatalf("promotion has no original signed parameters: %v", err)
				}
				if len(runner.deliverySubmissions) != 1 || runner.deliverySubmissions[0] != params.Promotion.Message {
					t.Fatalf("native submitted another operation: %q", runner.deliverySubmissions)
				}
				count, err := d.f.vcs.CommitCount(proof.TargetStart.Head, proof.Final)
				if err != nil || count != 1 {
					t.Fatalf("delivery history does not have one native commit: %d %v", count, err)
				}
				before, syncs := len(runner.deliverySubmissions), runner.synchronizations
				if _, err := d.f.PrepareCandidate("verified native candidate"); err != nil || len(runner.deliverySubmissions) != before || runner.synchronizations != syncs {
					t.Fatalf("repeated promotion performed another native writer: %v", err)
				}
				if lost {
					runner.interruptDelivery = false
					runner.interruptOperation, runner.lost = "cleanup", false
					if _, err := d.f.Drive(); err == nil || !runner.lost {
						t.Fatalf("completed cleanup response not lost: %v", err)
					}
					nativeIncompleteState(t, d, "op.cleanup")
					reopenNativeProviderDriver(t, d, name, runner)
					runner.interruptOperation = ""
				}
				d.drive()
				if d.run.Status != "COMPLETE" {
					t.Fatalf("native cleanup not complete: %+v", d.run.Next)
				}
				if d.run.Termination == nil || d.run.Termination.CandidatePromotion == nil || !reflect.DeepEqual(*d.run.Termination.CandidatePromotion, *proof) {
					t.Fatal("terminal summary lost the exact promotion receipt")
				}
				for _, path := range []string{"app.go", "app_test.go", ".gates/results/validation.blackbox-cases.md"} {
					validated, err := d.f.vcs.Entry(candidate, path)
					if err != nil || validated == "" {
						t.Fatalf("validated native content is missing: %s %v", path, err)
					}
					final, err := d.f.vcs.Entry(proof.Final, path)
					if err != nil || final != validated {
						t.Fatalf("final native bytes/metadata differ: %s %v", path, err)
					}
				}
				observed, err := vcs.NewNative(d.f.root, name, runner).TreeDigest(candidate)
				if err != nil || observed != digest {
					t.Fatalf("cleaned native candidate not reproducible: %v", err)
				}
			})
		}
	}
}

func TestSVNCandidateDirectoryPermissions(t *testing.T) {
	for _, scenario := range []string{"new-parents", "existing-parent", "explicit-empty", "explicit-property", "undeclared-sibling", "extra-empty", "new-parent-property", "existing-parent-property", "test-package"} {
		t.Run(scenario, func(t *testing.T) {
			root, base, runner := nativeProviderFixture(t, "svn")
			native := vcs.NewNative(root, "svn", runner)
			write := func(path, body string) {
				t.Helper()
				target := filepath.Join(root, filepath.FromSlash(path))
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			paths := []string{"nested/input.txt"}
			stage := "DEVELOPMENT"
			switch scenario {
			case "existing-parent", "existing-parent-property":
				paths = []string{".gates/input.txt"}
			case "explicit-empty":
				paths = []string{"empty"}
			case "explicit-property":
				paths = []string{".gates"}
			case "test-package":
				paths, stage = []string{"nested/input_test.go"}, "QA_ARTIFACT"
			}
			if scenario == "explicit-empty" {
				if err := os.Mkdir(filepath.Join(root, "empty"), 0755); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "explicit-property" {
				if _, err := runner.Run(root, "svn", "propset", "custom:directory", " exact \n", ".gates"); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "test-package" {
				write(paths[0], "package nested\nimport \"testing\"\nfunc TestInput(t *testing.T) {}\n")
			} else {
				write(paths[0], " exact input \n\n")
			}
			switch scenario {
			case "undeclared-sibling":
				write("nested/other.txt", "undeclared")
			case "extra-empty":
				if err := os.Mkdir(filepath.Join(root, "extra"), 0755); err != nil {
					t.Fatal(err)
				}
			case "new-parent-property":
				if err := native.Track(paths); err != nil {
					t.Fatal(err)
				}
				if _, err := runner.Run(root, "svn", "propset", "custom:directory", "unauthorized", "nested"); err != nil {
					t.Fatal(err)
				}
			case "existing-parent-property":
				if _, err := runner.Run(root, "svn", "propset", "custom:directory", "unauthorized", ".gates"); err != nil {
					t.Fatal(err)
				}
			}
			r := protocol.NativeResource{ID: "candidate", Owner: "directory", Role: "development", Path: root, Source: base, Identity: base, Status: "READY"}
			s := &protocol.State{RunID: "directory", VCSName: "svn", NativeResources: map[string]protocol.NativeResource{r.ID: r}}
			params, err := evidenceFields(protocol.CandidateParameters{Stage: stage, WorkspaceID: r.ID, Base: base, Paths: paths})
			if err != nil {
				t.Fatal(err)
			}
			intent := protocol.HostActionIntent{ActionID: "freeze", PayloadDigest: "sha256:directories", Adapter: &protocol.AdapterHostAction{Operation: "op.candidate", Params: params}}
			f := &Facade{root: root, vcs: native, admission: *testAdmission(t, root, "directory", "sha256:package", DefaultProvider)}
			result, complete, err := f.candidateEvidence(s, intent, true, new(bool))
			rejected := scenario == "undeclared-sibling" || scenario == "extra-empty" || scenario == "new-parent-property" || scenario == "existing-parent-property"
			if rejected {
				if err == nil || complete || result.Candidate != nil {
					t.Fatalf("undeclared native value accepted: %+v %t %v", result, complete, err)
				}
				head, resolveErr := native.Resolve()
				if resolveErr != nil || head != base {
					t.Fatalf("rejection committed values: %s %v", head, resolveErr)
				}
				return
			}
			if err != nil || !complete || result.Candidate == nil {
				t.Fatalf("declared native values rejected: %+v %t %v", result, complete, err)
			}
			want, err := native.Changes(base, result.Candidate.Identity)
			if err != nil || !reflect.DeepEqual(want, result.Candidate.Changes) {
				t.Fatalf("directory evidence omitted native values: %+v %v", result.Candidate, err)
			}
			if scenario == "test-package" && (!result.Candidate.TestOnly || !result.Candidate.ProductionViewEqual) {
				t.Fatal("test directory containers lost test-only proof")
			}
			recovered, observed, err := f.candidateEvidence(s, intent, false, new(bool))
			if err != nil || !observed || !reflect.DeepEqual(recovered, result) {
				t.Fatalf("observation changed native proof: %+v %v", recovered, err)
			}
		})
	}
}

// The public workflow must validate and deliver the same complete deletion snapshot.
func TestP4PublicDeletionPreservesCandidateAndDelivery(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost=%t", lost), func(t *testing.T) {
			root, _, runner := nativeProviderFixture(t, "p4")
			path := filepath.Join(root, "unused.txt")
			if err := os.WriteFile(path, []byte("retired command notes\n"), 0644); err != nil {
				t.Fatal(err)
			}
			native := vcs.NewNative(root, "p4", runner)
			if err := native.Track([]string{"unused.txt"}); err != nil {
				t.Fatal(err)
			}
			if err := native.Commit("tracked command notes"); err != nil {
				t.Fatal(err)
			}
			base, err := native.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			revision := fmt.Sprintf("%x", sha256.Sum256([]byte(validationDocument)))
			receipt := testReceipt()
			receipt.RequirementSource, receipt.Artifacts[0].Path = ".gates/requirements.md", ".gates/requirements.md"
			receipt.RequirementRevision, receipt.Artifacts[0].Revision = revision, revision
			f, run, err := Start(StartOptions{Root: root, Capacity: 16, VCSRunner: runner, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider), Request: StartRequest{VCS: "p4", RunID: "validation", Route: "regular", Provider: DefaultProvider, BaseSnapshot: base, IntakeConfirmationReceipt: receipt}})
			if err != nil {
				t.Fatal(err)
			}
			d := &validationDriver{t: t, f: f, run: run, revision: revision, git: f.vcs}
			d.drive()
			plan := commandPlan()
			plan.Batches[0].Artifacts = []string{"unused.txt"}
			plan.Batches[0].Deliverable = "Remove retired command notes"
			passPreDevelopmentReviews(t, d, plan)
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command removal"}})
			d.confirmRoute(routeDecisionForGates([]string{"quality"}))
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: validationDocument}}})
			d.drive()
			action, input := d.action("engine.development.batch")
			if err := os.Remove(filepath.Join(input.Workspace.Path, "unused.txt")); err != nil {
				t.Fatal(err)
			}
			if _, err := (vcs.ExecRunner{}).Run(input.Workspace.Path, "go", "test", "-buildvcs=false", "./..."); err != nil {
				t.Fatal(err)
			}
			d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
			d.drive()
			if d.run.Validation.Candidate == nil {
				t.Fatal("no immutable deletion candidate")
			}
			candidate, digest := d.run.Validation.Candidate.Identity, d.run.Validation.Candidate.Digest
			for _, identity := range []string{d.run.Validation.DevelopmentSnapshot, candidate} {
				entry, err := d.f.vcs.Entry(identity, "unused.txt")
				if err != nil || entry != "" {
					t.Fatalf("frozen snapshot retained deleted content: %q %v", entry, err)
				}
			}
			gate, gateInput := d.action("engine.gate")
			if _, err := (vcs.ExecRunner{}).Run(gateInput.Workspace.Path, "go", "test", "-buildvcs=false", "./..."); err != nil {
				t.Fatal(err)
			}
			d.business(gate, protocol.ValidationResult{Binding: gateInput.Binding, Status: "PASS"})
			runner.interruptDelivery = lost
			_, err = d.f.PrepareCandidate("verified command notes removal")
			var original protocol.HostActionIntent
			if lost {
				if !runner.lost {
					t.Fatal("completed deletion promotion response was not lost")
				}
				// Complete read-only observation may settle a lost response inline.
				// A pending local continuation retains the same original intent.
				if err != nil {
					original = nativeIncompleteState(t, d, "op.candidate")
					reopenNativeProviderDriver(t, d, "p4", runner)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if _, err := d.f.PrepareCandidate("verified command notes removal"); err != nil {
					t.Fatal(err)
				}
			}
			state := driverState(t, d)
			proof := state.CandidatePromotion
			if proof == nil || proof.Source != candidate || proof.SourceDigest != digest || proof.FinalDigest != digest || original.ActionID != "" && proof.OperationID != original.ActionID {
				t.Fatalf("deletion promotion lost its exact candidate and operation: %+v", proof)
			}
			retained := state.ReconciledEffects[proof.OperationID].Intent
			if retained == nil || retained.ActionID != proof.OperationID {
				t.Fatal("deletion promotion lost its settled original intent")
			}
			var params protocol.CandidateParameters
			if err := decodeNativeParams(*retained, &params); err != nil || params.Promotion == nil {
				t.Fatalf("deletion promotion lost its signed inputs: %v", err)
			}
			count, err := d.f.vcs.CommitCount(proof.TargetStart.Head, proof.Final)
			if err != nil || count != 1 || len(runner.deliverySubmissions) != 1 || runner.deliverySubmissions[0] != params.Promotion.Message {
				t.Fatalf("deletion promotion resubmitted: count=%d submissions=%q %v", count, runner.deliverySubmissions, err)
			}
			d.drive()
			if d.run.Status != "COMPLETE" || d.run.Termination == nil || !reflect.DeepEqual(d.run.Termination.CandidatePromotion, proof) {
				t.Fatal("public completion lost the deletion promotion receipt")
			}
			if _, err := os.ReadFile(path); !os.IsNotExist(err) {
				t.Fatalf("delivered deleted file remains: %v", err)
			}
			for _, file := range []string{"app.go", "go.mod", ".gates/requirements.md"} {
				validated, err := d.f.vcs.Entry(candidate, file)
				if err != nil || validated == "" {
					t.Fatalf("candidate content is missing: %s %v", file, err)
				}
				final, err := d.f.vcs.Entry(proof.Final, file)
				if err != nil || final != validated {
					t.Fatalf("delivered native bytes or metadata differ: %s %v", file, err)
				}
			}
			finalDigest, err := d.f.vcs.TreeDigest(proof.Final)
			if err != nil || finalDigest != digest {
				t.Fatalf("final deletion content differs from candidate: %s %v", finalDigest, err)
			}
		})
	}
}
