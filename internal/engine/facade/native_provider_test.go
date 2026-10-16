package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
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
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/testkit"
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
	port, user, client  string
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
	if command == "p4" {
		prefix := []string{"-p", r.port, "-u", r.user}
		hasClient := false
		for i := 0; i < len(args) && strings.HasPrefix(args[i], "-"); i++ {
			if args[i] == "-c" {
				hasClient = true
				break
			}
			if args[i] == "-p" || args[i] == "-u" || args[i] == "-C" {
				i++
			}
		}
		if !hasClient && r.client != "" {
			prefix = append(prefix, "-c", r.client)
		}
		args = append(prefix, args...)
	}
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
		repositoryURL := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(repo), "/")}).String()
		command("", "svn", "mkdir", repositoryURL+"/trunk", "-m", "fixture trunk")
		command("", "svn", "checkout", repositoryURL+"/trunk", root)
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
		runner.port, runner.user, runner.client = port, "fixture", "main"
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

// Startup metadata and persisted state carry the selected native provider into
// reopened workspace planning and creation on real repositories.
func TestRegularStartPersistsSelectedVCS(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	for _, name := range []string{"git", "svn", "p4"} {
		t.Run(name, func(t *testing.T) {
			var root, base, requirement string
			var runner vcs.Runner
			if name == "git" {
				root = nativeFixtureDirectory(t)
				base = snapshotRepoCommits(t, root)[0]
				requirement = "requirements.md"
			} else {
				var nativeRunner *nativeProviderRunner
				root, base, nativeRunner = nativeProviderFixture(t, name)
				runner = nativeRunner
				if name == "p4" {
					t.Setenv("P4PORT", nativeRunner.port)
					t.Setenv("P4USER", nativeRunner.user)
					t.Setenv("P4CLIENT", nativeRunner.client)
				}
				requirement = ".gates/requirements.md"
			}
			preview, err := PrepareIntake(root, requirement, "", nil, true)
			if err != nil {
				t.Fatal(err)
			}
			admission := testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)
			inputs := []string{name, strings.ToUpper(name)}
			if name == "git" {
				inputs = append([]string{""}, inputs...)
			}
			for i, input := range inputs {
				runID := fmt.Sprintf("selected-%s-%d", name, i)
				f, _, err := Start(StartOptions{Root: root, ArtifactRoot: root, Capacity: 16, VCSRunner: runner, Admission: admission, Request: StartRequest{VCS: input, RunID: runID, Route: "regular", BaseSnapshot: base, IntakeConfirmationReceipt: preview.IntakeCandidate}})
				if err != nil {
					t.Fatalf("start with VCS %q: %v", input, err)
				}
				request, err := f.request()
				if err != nil || request.VCS != name || f.vcs.Name() != name || request.BaseSnapshot != base {
					t.Fatalf("persisted request for VCS %q: %+v, %v; provider %s", input, request, err, f.vcs.Name())
				}
				statePath := filepath.Join(f.stateDir, "state.json")
				body, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				var persisted struct {
					Content protocol.State `json:"content"`
				}
				if err := json.Unmarshal(body, &persisted); err != nil {
					t.Fatal(err)
				}
				if persisted.Content.VCSName != name || persisted.Content.NativeVCS() != name || persisted.Content.BaseSnapshot != base {
					t.Fatalf("persisted native identity for VCS %q: %s %s %s", input, persisted.Content.VCSName, persisted.Content.NativeVCS(), persisted.Content.BaseSnapshot)
				}
				reopened, err := Open(root, runID)
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := reopened.engine.Load()
				if err != nil || snapshot.State.VCSName != name || reopened.vcs.Name() != name {
					t.Fatalf("reopen VCS %q: %+v, %v", input, snapshot, err)
				}
				handle := vcs.PlanWorkspace(snapshot.State.NativeVCS(), base, runID, "candidate")
				if handle.Source != base || name == "svn" && handle.URL == "" || name == "p4" && (handle.Client == "" || handle.Depot == "") {
					t.Fatalf("workspace handle for VCS %q: %+v", input, handle)
				}
				workspace := filepath.Join(nativeFixtureDirectory(t), "candidate-"+runID)
				if err := reopened.vcs.CreateWorkspace(workspace, base, "formal-gates workspace "+runID, handle); err != nil {
					t.Fatalf("create reopened VCS %q workspace: %v", input, err)
				}
				identity, exists, err := reopened.vcs.ObservedWorkspace(workspace, base, "formal-gates workspace "+runID, handle)
				if err != nil || !exists || identity == "" {
					t.Fatalf("native copy lineage for VCS %q: %s %t %v", input, identity, exists, err)
				}
				t.Logf("input VCS=%q request/state/reopened provider=%s handle=%+v", input, name, handle)
			}
		})
	}
}

func TestLightweightStartDoesNotSelectNativeVCS(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	f, _, err := Start(StartOptions{Root: root, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider), Request: StartRequest{VCS: "SVN", RunID: "lightweight", Route: "lightweight", IntakeConfirmationReceipt: testReceipt()}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := f.request()
	if err != nil || request.VCS != "SVN" || f.vcs != nil {
		t.Fatalf("lightweight selected a native provider: %+v %v", request, err)
	}
	reopened, err := Open(root, f.runID)
	if err != nil || reopened.vcs != nil {
		t.Fatalf("reopened lightweight native provider: %+v %v", reopened, err)
	}
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
	dev, di := d.action("engine.development.batch.v6")
	design, qi := d.action("engine.qa.design.v6")
	if len(d.run.PendingActions) != 2 {
		t.Fatal("development and blackbox design are not concurrent")
	}
	manifest := simpleManifest(qi)
	for i := range manifest.Cases {
		test := "TestValid"
		if manifest.Cases[i].CaseID == "invalid" {
			test = "TestInvalid"
		}
		manifest.Cases[i].PublicEntry = "public Valid input function"
		manifest.Cases[i].Steps = "invoke the reviewed public input contract test " + test
		manifest.Cases[i].Operations = []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"go", "test", "-timeout=0", "-count=1", "-buildvcs=false", "-run", "^" + test + "$", "."}}}
	}
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	id, in := d.action("engine.qa.review.rationale.v2")
	review := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &review})
	d.drive()
	if err := os.WriteFile(filepath.Join(di.Workspace.Path, "app.go"), []byte("package command\nfunc Valid(s string) bool { return len(s) > 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: batchEvidence(di)})
	d.drive()
	id, in = d.action("engine.qa.design.v6")
	if in.Manifest.Binding.ReviewKind != coverage.ReviewWhitebox {
		t.Fatal("whitebox authoring not selected")
	}
	manifest = in.Manifest
	for _, row := range []struct{ id, scenario, ref string }{{"white-success", "SUCCESS", "TestValid"}, {"white-misuse", "MISUSE", "TestInvalid"}} {
		manifest.Cases = append(manifest.Cases, coverage.AcceptanceCase{CaseID: row.id, Mode: coverage.CaseWhitebox, Scenario: row.scenario, TestRef: "app_test.go::" + row.ref, Oracle: "specified input outcome"})
		manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: row.id})
	}
	body := "package command\nimport \"testing\"\nfunc TestValid(t *testing.T){if !Valid(\"ok\"){t.Fatal(\"valid input\")}}\nfunc TestInvalid(t *testing.T){if Valid(\"\"){t.Fatal(\"invalid input\")}}\n"
	if err := os.WriteFile(filepath.Join(in.Workspace.Path, "app_test.go"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.drive()
	id, in = d.action("engine.qa.review.rationale.v2")
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
		if input.Handler == "engine.qa.execution.v4" || input.Handler == "engine.gate.v3" {
			if _, err := (vcs.ExecRunner{}).Run(input.Workspace.Path, "go", "test", "-timeout=0", "-buildvcs=false", "./..."); err != nil {
				t.Fatal(err)
			}
		}
	}
	for len(d.run.ValidationInputs) > 0 {
		var id string
		var input protocol.ValidationInput
		for action, in := range d.run.ValidationInputs {
			if in.Handler == "engine.qa.execution.v4" || in.Handler == "engine.gate.v3" {
				id, input = action, in
				break
			}
		}
		if id == "" {
			t.Fatalf("unexpected native validation task: %+v", d.run.ValidationInputs)
		}
		result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS"}
		if input.Handler == "engine.qa.execution.v4" {
			report := executionFor(input)
			result.Execution = &report
			result.Tool, result.Environment = protocol.ExecutionIdentities(input.ExecutionFacts)
		}
		d.business(id, result)
		if len(d.run.ValidationInputs) > 0 {
			d.drive()
		}
	}
}
func TestNativeProviderPublicCandidatePromotionAndCleanup(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"svn", "p4"} {
		for _, lost := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/lost=%t", name, lost), func(t *testing.T) {
				t.Parallel()
				d, runner := startNativeProviderDriver(t, name)
				passPreDevelopmentReviews(t, d, commandPlan())
				d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one native command"}})
				d.confirmRoute(routeDecisionForGates([]string{"blackbox", "whitebox", "quality"}))

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
	t.Parallel()
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
			if scenario == "test-package" && (result.Candidate.TestOnly || result.Candidate.ProductionViewEqual || result.Candidate.GateView != nil) {
				t.Fatal("unreviewed test directory asserted ownership approval")
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
	t.Parallel()
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost=%t", lost), func(t *testing.T) {
			t.Parallel()
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
			d.drive()
			action, input := d.action("engine.development.batch.v6")
			if err := os.Remove(filepath.Join(input.Workspace.Path, "unused.txt")); err != nil {
				t.Fatal(err)
			}
			if _, err := (vcs.ExecRunner{}).Run(input.Workspace.Path, "go", "test", "-timeout=0", "-buildvcs=false", "./..."); err != nil {
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
			gate, gateInput := d.action("engine.gate.v3")
			if _, err := (vcs.ExecRunner{}).Run(gateInput.Workspace.Path, "go", "test", "-timeout=0", "-buildvcs=false", "./..."); err != nil {
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

// Native child admission and delivery preserve each registered scope, original
// workspace proof and complete slice content through the common final candidate.
func TestNativeProviderSplitDeliveryPreservesWorkspaceProofs(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"svn", "p4"} {
		for _, lost := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/lost=%t", name, lost), func(t *testing.T) {
				t.Parallel()
				master, runner := startNativeProviderDriver(t, name)
				passPreDevelopmentReviews(t, master, splitCommandPlan("core", "tooling"))

				topology := &protocol.TopologyDecision{Decision: "split", Slices: []string{"core", "tooling"}, ACOwners: []protocol.ACOwner{{AC: "AC-001", Owner: "master-merge"}}, SplitCount: 2, Parallel: "independent command modules"}
				master.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: topology})
				prepared := prepareChildFrontier(t, master)
				if lost {
					runner.interruptOperation = "workspace"
					if _, err := master.f.Drive(); err == nil || !runner.lost {
						t.Fatal("completed workspace response was not lost", err)
					}
					nativeIncompleteState(t, master, "op.workspace")
					reopenNativeProviderDriver(t, master, name, runner)
					runner.interruptOperation = ""
				}
				master.drive()
				parent := driverState(t, master)
				if !reflect.DeepEqual(parent.ChildCreation, prepared.ChildCreation) || len(parent.SliceChildren) != len(topology.Slices) {
					t.Fatal("join changed the original creation or released partial membership")
				}
				for _, plan := range parent.ChildCreation.Children {
					proof, found := parent.ChildWorkspaceProof(plan)
					resource := parent.NativeResources[plan.ResourceID]
					child, err := Open(master.f.root, plan.RunID)
					if err != nil {
						t.Fatal(err)
					}
					state, err := child.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					exists, err := master.f.vcs.WorkspaceExists(resource.Path, resource.Handle)
					if err != nil || !exists || !found || proof.Resource != resource || proof.Digest != parent.ChildCreation.Digest || proof.SourceDigest != parent.ChildCreation.Digest || state.State.InitialSnapshot != proof.Resource.Identity || state.State.NativeRoot != resource.Path || state.State.NativeHandle != resource.Handle || state.State.NativeVCS() != name || state.State.Phase != runtime.PhaseTopologyAndRoute || state.State.SliceChild.MasterRunID != parent.RunID || state.State.SliceChild.SliceID != plan.SliceID || state.State.RequirementRevision() != master.revision {
						t.Fatal("child did not consume its exact original native proof/scope", err)
					}
					body, err := os.ReadFile(filepath.Join(resource.Path, ".gates", "requirements.md"))
					if err != nil || string(body) != validationDocument {
						t.Fatal("child lost complete requirement bytes", err)
					}
					digest, err := child.vcs.TreeDigest(state.State.InitialSnapshot)
					if err != nil || digest != proof.Digest {
						t.Fatal("child initial native values differ from its proof", err)
					}
					if _, err := child.Drive(); err != nil {
						t.Fatal(err)
					}
				}
				before := installationFiles(t, master.f.root)
				reopenNativeProviderDriver(t, master, name, runner)
				master.drive()
				if !reflect.DeepEqual(before, installationFiles(t, master.f.root)) {
					t.Fatal("released join replay changed retained material")
				}
				design, input := master.action("engine.qa.design.v6")
				manifest := simpleManifest(input)
				for i := range manifest.Cases {
					manifest.Cases[i].Mode = coverage.CaseMerge
					argument := "valid"
					if manifest.Cases[i].CaseID == "invalid" {
						argument = ""
					}
					manifest.Cases[i].PublicEntry = "public Valid input function"
					manifest.Cases[i].Steps = "invoke the public input function through its isolated caller"
					command := "mkdir -p .gates/input-call; cat > .gates/input-call/main.go <<'GO'\npackage main\nimport(\"command\";\"os\")\nfunc main(){if len(os.Args)!=2 || !command.Valid(os.Args[1]){os.Exit(2)}}\nGO\ngo run -buildvcs=false ./.gates/input-call " + fmt.Sprintf("%q", argument)
					manifest.Cases[i].Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: command}}
				}
				master.business(design, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
				master.drive()
				id, input := master.action("engine.qa.review.rationale.v2")
				review := approvedReview(manifest)
				master.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &review})
				master.drive()
				receipts := make(map[string]protocol.SliceReceipt)
				entries := make(map[string]string)
				for index, plan := range parent.ChildCreation.Children {
					f, err := Open(master.f.root, plan.RunID)
					if err != nil {
						t.Fatal(err)
					}
					run, err := f.Show()
					if err != nil {
						t.Fatal(err)
					}
					child := &validationDriver{t: t, f: f, run: run, revision: master.revision, git: f.vcs}
					reopenNativeProviderDriver(t, child, name, runner)
					route := protocol.RouteDecision{Gates: []string{"quality", "security"}, Selected: []string{"quality", "security"}}
					if index == 1 {
						route.Selected = []string{"quality"}
					}
					development := sliceLanePlan()
					path := plan.SliceID + ".go"
					development.Batches[0].Artifacts = []string{path}
					child.confirmSliceRoute(route, development)

					child.drive()
					id, input := planBatchAction(child, "command")
					body := "package command\n// " + plan.SliceID + " behavior\n"
					if err := os.WriteFile(filepath.Join(input.Workspace.Path, path), []byte(body), 0644); err != nil {
						t.Fatal(err)
					}
					if _, err := (vcs.ExecRunner{}).Run(input.Workspace.Path, "go", "test", "-timeout=0", "-buildvcs=false", "./..."); err != nil {
						t.Fatal(err)
					}
					child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
					child.drive()
					completeSliceGates(t, child)
					if _, err := child.f.PrepareCandidate("verified slice delivery"); err != nil {
						t.Fatal(err)
					}
					child.drive()
					if child.run.Status != "SLICE_READY" {
						t.Fatal("child delivery did not freeze its checkpoint")
					}
					receipt := readSliceReceipt(t, child)
					if receipt.Receipt.ChildRunID != plan.RunID || receipt.Receipt.MasterRunID != parent.RunID || receipt.Receipt.SliceID != plan.SliceID || receipt.Receipt.RequirementRevision != master.revision || receipt.Receipt.CanonicalDigest() != receipt.Digest || driverState(t, master).SliceReceipts[plan.RunID].Digest != receipt.Digest {
						t.Fatal("checkpoint/handoff lost original identity or material")
					}
					digest, err := child.f.vcs.TreeDigest(receipt.Receipt.Snapshot)
					if err != nil || digest != receipt.Receipt.Tree {
						t.Fatal("checkpoint did not retain its complete native values", err)
					}
					entries[path], err = child.f.vcs.Entry(receipt.Receipt.Snapshot, path)
					if err != nil || entries[path] == "" {
						t.Fatal("checkpoint lacks slice bytes/metadata", err)
					}
					bytes, err := os.ReadFile(filepath.Join(child.f.vcs.Root(), path))
					if err != nil || string(bytes) != body {
						t.Fatal("promoted slice bytes differ from development", err)
					}
					receipts[plan.RunID] = receipt
				}
				master.drive()
				state := driverState(t, master)
				if state.Validation.Candidate == nil || len(state.Validation.Waves) != 1 || len(state.Validation.Waves[0].Targets) != 5 {
					t.Fatal("common final candidate did not include merge QA/gate and all selected slice gates")
				}
				candidate := state.Validation.Candidate
				for _, input := range master.run.ValidationInputs {
					if input.Binding.Candidate != candidate.Identity {
						t.Fatal("final validation mixed native candidates")
					}
					if _, err := (vcs.ExecRunner{}).Run(input.Workspace.Path, "go", "test", "-timeout=0", "-buildvcs=false", "./..."); err != nil {
						t.Fatal(err)
					}
				}
				for path, want := range entries {
					got, err := master.f.vcs.Entry(candidate.Identity, path)
					if err != nil || got != want {
						t.Fatal("fixed merge changed slice bytes or metadata", path, err)
					}
				}
				passMainlineTargets(t, master)
				master.drive()
				if master.run.Status != "COMPLETE" || master.run.Termination.PromotedDigest != candidate.Digest {
					t.Fatal("native split promotion/cleanup did not complete")
				}
				for childID, receipt := range receipts {
					if !reflect.DeepEqual(master.run.Termination.SliceReceipts[childID], receipt) {
						t.Fatal("terminal authority changed the complete slice receipt")
					}
					for _, name := range []string{"state.json", "slice-receipt.json"} {
						if _, err := os.Stat(filepath.Join(master.f.root, EngineNamespace, childID, name)); !os.IsNotExist(err) {
							t.Fatal("child active evidence survived terminal cleanup", err)
						}
					}
				}
				for _, resource := range master.run.Termination.NativeResources {
					if resource.Status != "CLEANED" {
						t.Fatal("native resource did not clean up")
					}
					if exists, err := master.f.vcs.WorkspaceExists(resource.Path, resource.Handle); err != nil || exists {
						t.Fatal("native workspace survived cleanup", err)
					}
				}
				for path, want := range entries {
					got, err := master.f.vcs.Entry(master.run.Termination.PromotedSnapshot, path)
					if err != nil || got != want {
						t.Fatal("final promotion changed complete slice content", err)
					}
				}
			})
		}
	}
}

// An exact committed operation explains only WC revision skew. Foreign file,
// property and untracked content cannot be normalized by its continuation.
func TestSVNPromotionRejectsForeignWorkingChanges(t *testing.T) {
	t.Parallel()
	root, base, runner := nativeProviderFixture(t, "svn")
	native := vcs.NewNative(root, "svn", runner)
	if _, err := runner.Run(root, "svn", "propset", "custom:delivery", "changed", root); err != nil {
		t.Fatal(err)
	}
	message := "formal-gates delivery owned operation"
	runner.interruptDelivery = true
	if err := native.Commit(message); err == nil || !runner.lost {
		t.Fatal("actual committed SVN response loss not reached")
	}
	runner.interruptDelivery = false
	observed, found, err := native.ObservedCommit(base, message)
	if err != nil || !found {
		t.Fatal("original operation not found")
	}
	head, err := native.Resolve()
	if err != nil || head != observed {
		t.Fatal("root revision does not identify the successful operation")
	}
	digest, err := native.TreeDigest(observed)
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.CandidateParameters{Stage: "PROMOTION", Promotion: &protocol.PromotionParameters{Source: observed, SourceDigest: digest, TargetRoot: root, TargetBase: base, TargetStart: protocol.PromotionObservation{Head: base}, Message: message}}
	f := &Facade{vcs: native}
	invoked := false
	result, complete, err := f.nativePromotionEvidence(&protocol.State{}, protocol.HostActionIntent{}, params, false, &invoked)
	if err != nil || complete || !result.observedEffect || invoked {
		t.Fatalf("mixed WC operation prematurely complete: %+v %t %v", result, complete, err)
	}
	for _, scenario := range []string{"file", "property", "untracked"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(root, "app.go")
			switch scenario {
			case "file":
				if err := os.WriteFile(path, []byte("package main // foreign edit\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "property":
				if _, err := runner.Run(root, "svn", "propset", "custom:foreign", "unrelated", path); err != nil {
					t.Fatal(err)
				}
			case "untracked":
				if err := os.WriteFile(filepath.Join(root, "foreign.txt"), []byte("foreign"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := runner.synchronizations
			_, complete, err := f.nativePromotionEvidence(&protocol.State{}, protocol.HostActionIntent{}, params, true, &invoked)
			if err == nil || complete || invoked || runner.synchronizations != before {
				t.Fatalf("foreign %s accepted/synchronized: %t %v", scenario, complete, err)
			}
			if _, err := runner.Run(root, "svn", "revert", "-R", root); err != nil {
				t.Fatal(err)
			}
			if scenario == "untracked" {
				if err := os.Remove(filepath.Join(root, "foreign.txt")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	if err := native.VerifyReady(); err == nil {
		t.Fatal("ordinary mixed revision was accepted without its signed continuation")
	}
}

func TestSVNCompletedPromotionAcceptsNextWorkingInput(t *testing.T) {
	t.Parallel()
	root, base, runner := nativeProviderFixture(t, "svn")
	native := vcs.NewNative(root, "svn", runner)
	baseRevision, err := runner.Run(root, "svn", "info", "--show-item", "revision", root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "svn", "propset", "custom:delivery", "changed", root); err != nil {
		t.Fatal(err)
	}
	message := "formal-gates delivery completed operation"
	runner.interruptDelivery = true
	if err := native.Commit(message); err == nil || !runner.lost {
		t.Fatal("actual committed SVN response loss not reached")
	}
	runner.interruptDelivery = false
	observed, found, err := native.ObservedCommit(base, message)
	if err != nil || !found {
		t.Fatal("original operation not found", err)
	}
	if err := native.Synchronize(observed); err != nil {
		t.Fatal(err)
	}
	digest, err := native.TreeDigest(observed)
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.CandidateParameters{Stage: "PROMOTION", WorkspaceID: "candidate", Promotion: &protocol.PromotionParameters{SourceWorkspace: "source", Source: observed, SourceDigest: digest, TargetRoot: root, TargetBase: base, TargetStart: protocol.PromotionObservation{Head: base}, Message: message}}
	intent := protocol.HostActionIntent{ActionID: "delivery", PayloadDigest: "parameters"}
	f := &Facade{vcs: native}
	path := filepath.Join(root, ".gates", "requirements.md")
	input := validationDocument + "\n\n"
	if err := os.WriteFile(path, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	before := runner.synchronizations
	var observation protocol.Observation
	for _, mayWrite := range []bool{false, true} {
		invoked := false
		result, complete, err := f.nativePromotionEvidence(&protocol.State{}, intent, params, mayWrite, &invoked)
		if err != nil || !complete || result.Candidate == nil || invoked {
			t.Fatalf("completed operation denied by next input: %+v %t %v", result, complete, err)
		}
		want := protocol.CandidatePromotionReceipt{OperationID: intent.ActionID, Provider: "svn", SourceWorkspace: "source", Source: observed, SourceDigest: digest, TargetRoot: root, TargetBase: base, TargetStart: params.Promotion.TargetStart, Final: observed, FinalDigest: digest}
		if !reflect.DeepEqual(result.Candidate.Promotion, &want) || result.Candidate.ParametersDigest != intent.PayloadDigest || result.Candidate.WorkspaceID != params.WorkspaceID || result.Candidate.Identity != observed || result.Candidate.Digest != digest {
			t.Fatal("receipt did not retain the original complete operation")
		}
		observation = result.observation
		actual, err := observation.Fingerprint()
		expected, digestErr := observation.Facts.Digest()
		if err != nil || digestErr != nil || actual != expected {
			t.Fatal("completed observation does not reread its sampled sources", err, digestErr)
		}
	}
	if runner.synchronizations != before || len(runner.deliverySubmissions) != 1 {
		t.Fatal("late receipt repeated a completed native effect")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != input {
		t.Fatal("late receipt changed the next working input", err)
	}
	if _, err := runner.Run(root, "svn", "update", "-r", baseRevision, filepath.Join(root, "app.go")); err != nil {
		t.Fatal(err)
	}
	head, err := native.Resolve()
	if err != nil || head != observed {
		t.Fatal("mixed node probe changed the observed root", err)
	}
	actual, err := observation.Fingerprint()
	expected, digestErr := observation.Facts.Digest()
	if err != nil || digestErr != nil || actual == expected {
		t.Fatal("completion observation failed to bind the actual node synchronization", err, digestErr)
	}
}

// Candidate freezing shares the same unfinished SVN commit boundary as delivery.
func TestSVNCandidateResponseLossKeepsOriginalOperation(t *testing.T) {
	t.Parallel()
	root, base, runner := nativeProviderFixture(t, "svn")
	native := vcs.NewNative(root, "svn", runner)
	if _, err := runner.Run(root, "svn", "propset", "custom:candidate", "changed", root); err != nil {
		t.Fatal(err)
	}
	intent := protocol.HostActionIntent{ActionID: "candidate", PayloadDigest: "parameters"}
	message := "formal-gates candidate run candidate parameters"
	runner.interruptOperation = "candidate"
	if err := native.Commit(message); err == nil || !runner.lost {
		t.Fatal("actual candidate commit response-loss window not reached")
	}
	runner.interruptOperation = ""
	observed, found, err := native.ObservedCommit(base, message)
	if err != nil || !found {
		t.Fatal("original native candidate commit not observed")
	}
	head, err := native.Resolve()
	if err != nil || head != observed {
		t.Fatal("candidate root did not move to observed operation")
	}
	params := protocol.CandidateParameters{Stage: "DEVELOPMENT", WorkspaceID: "workspace", Base: base, Paths: []string{"app.go"}}
	body, _ := json.Marshal(params)
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	intent.Adapter = &protocol.AdapterHostAction{Operation: "op.candidate", Params: fields}
	state := &protocol.State{RunID: "run", NativeResources: map[string]protocol.NativeResource{"workspace": {ID: "workspace", Path: root, Source: base, Identity: base, Status: "READY", Role: "candidate"}}}
	f := &Facade{vcs: native}
	invoked := false
	result, complete, err := f.candidateEvidence(state, intent, false, &invoked)
	if err != nil || complete || !result.observedEffect || invoked {
		t.Fatalf("partially synchronized candidate was prematurely accepted: %+v %t %v", result, complete, err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package command // foreign edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := runner.synchronizations
	_, complete, err = f.candidateEvidence(state, intent, true, &invoked)
	if err == nil || complete || invoked || runner.synchronizations != before {
		t.Fatalf("candidate continuation absorbed foreign content: %t %v", complete, err)
	}
}
