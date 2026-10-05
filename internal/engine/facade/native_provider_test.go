package facade

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	log               *os.File
	lost              bool
	interruptDelivery bool
}

func (r *nativeProviderRunner) Run(dir, command string, args ...string) (string, error) {
	b, e := r.RunRaw(dir, command, nil, args...)
	return strings.TrimSpace(string(b)), e
}
func (r *nativeProviderRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	start := time.Now()
	body, err := (vcs.ExecRunner{}).RunRaw(dir, command, input, args...)
	fmt.Fprintf(r.log, "%s %q dir=%q stdin=%q elapsed=%s error=%v\nstdout=%q\n", command, args, dir, input, time.Since(start), err, body)
	if err == nil && r.interruptDelivery && !r.lost {
		for _, arg := range args {
			if strings.HasPrefix(arg, "formal-gates delivery ") {
				r.lost = true
				return nil, fmt.Errorf("fixture lost completed delivery response")
			}
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
	passMainlineTargets(t, d)
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
				d.drive()
				nativeWhiteboxReady(t, d)
				candidate := d.run.Validation.Candidate.Identity
				digest := d.run.Validation.Candidate.Digest
				runner.interruptDelivery = lost
				_, err := d.f.PrepareCandidate("verified native candidate")
				if lost {
					if err == nil || !runner.lost {
						t.Fatalf("native completed delivery response not lost: %v", err)
					}
					d.reopen()
					state := driverState(t, d)
					d.f.vcsRunner = runner
					d.f.vcs = vcs.NewNative(d.f.root, name, runner).At(state.NativeRoot, state.NativeHandle)
				} else if err != nil {
					t.Fatal(err)
				}
				if _, err := d.f.PrepareCandidate("verified native candidate"); err != nil {
					t.Fatal(err)
				}
				state := driverState(t, d)
				if state.CandidatePromotion == nil || state.CandidatePromotion.Provider != name || state.PromotedDigest != digest {
					t.Fatal("promotion receipt lost native provider/full digest")
				}
				d.drive()
				if d.run.Status != "COMPLETE" {
					t.Fatalf("native cleanup not complete: %+v", d.run.Next)
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
