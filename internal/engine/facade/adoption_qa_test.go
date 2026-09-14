package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

// Every source, instruction, Git repository and native receipt is test-owned.
func adoptionQAFixture(t *testing.T, phase string) (*Facade, string) {
	t.Helper()
	f := facadePreparation(t, []string{"whitebox"})
	r, _ := f.Show()
	dev := r.Regular.Preparation.Resources["development"]
	writePreparationFile(t, dev, "go.mod", "module example.invalid/hello\n\ngo 1.23\n")
	writePreparationFile(t, dev, "main.go", "package main\nfunc main(){}\n")
	facadeResult(t, f, facadeExecution(t, f, "development-worker"), protocol.PreparationResult{Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "prepare fixture", Observation: "local source", Result: "PASS"}}, Handoff: "none"})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	r, _ = f.Show()
	x := facadeExecution(t, f, "whitebox-design")
	var sources []SubagentSource
	for i := 0; i < 4; i++ {
		body := fmt.Sprintf("Public contract %d: observable behavior.\n", i)
		sources = append(sources, SubagentSource{Kind: "public-contract", Path: fmt.Sprintf("fixture-contract-%d", i), Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(body))), Content: body})
	}
	if err := os.MkdirAll(f.codexJob(x.Claim.ActionID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(f.codexJob(x.Claim.ActionID), "sources.json"), sources); err != nil {
		t.Fatal(err)
	}
	writePreparationFile(t, r.Regular.Preparation.Resources["whitebox"], "main_test.go", "package main\nimport \"testing\"\nfunc TestHello(t *testing.T){main()}\n")
	d := facadeDesign(r, "whitebox")
	facadeResult(t, f, x, protocol.PreparationResult{Design: &d, Manifest: []string{"main_test.go"}, Reuse: []protocol.TestReuse{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "fixture calls command", Procedure: "go test", Observation: "fixture", Comparison: "command", Result: "EXECUTED_PASS"}}})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	if phase != "design" {
		r, _ = f.Show()
		review := facadeReview(r, "whitebox")
		facadeResult(t, f, facadeExecution(t, f, "whitebox-review"), protocol.PreparationResult{Review: review})
		if phase == "revision" {
			facadeChoice(t, f, protocol.RegularRequest{Action: "qa-revise", Target: "whitebox", Meaning: "stable-execution", Reason: "fixture revise command details"})
		}
	}
	return f, x.Claim.ActionID
}

func adoptQACandidate(t *testing.T, f *Facade, production string) string {
	t.Helper()
	r, _ := f.Show()
	// The external delivery descends from the run's base and contains the current
	// implementation, without importing the QA branch's production tree.
	for _, path := range []string{"go.mod", "main.go"} {
		data, err := os.ReadFile(filepath.Join(r.Regular.Preparation.Resources["development"], path))
		if err != nil {
			t.Fatal(err)
		}
		writePreparationFile(t, f.root, path, string(data))
	}
	writePreparationFile(t, f.root, "main.go", production)
	for _, args := range [][]string{{"add", "go.mod", "main.go"}, {"-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixture external candidate"}} {
		if out, err := gitprep.Git(f.root, args...); err != nil {
			t.Fatal(out, err)
		}
	}
	head, err := gitprep.Head(f.root)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := exec.Command("git", "-C", f.root, "diff", "--binary", r.Regular.Candidate, head, "--").Output()
	if err != nil {
		t.Fatal(err)
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "adopt-external", External: &protocol.ExternalObservation{Identity: head, Lineage: r.Regular.BaseSnapshot, DiffDigest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(diff)))}, Impact: map[string]string{"product-review": "unaffected", "start-readiness": "unaffected", "development": "unaffected", "whitebox-review": "unaffected"}, Reason: "fixture explicit retention"})
	r, _ = f.Show()
	facadeChoice(t, f, protocol.RegularRequest{Action: "topology", Target: "no-split", PlanDigest: r.Regular.Plan.Digest, Reason: "fixture unchanged topology"})
	return head
}

func TestAdoptionRetainedQAThroughTopologyAndDrive(t *testing.T) {
	for _, phase := range []string{"design", "revision", "frozen"} {
		t.Run(phase, func(t *testing.T) {
			f, designAction := adoptionQAFixture(t, phase)
			before, _ := f.Show()
			old := before.Regular.Preparation
			qaBytes, _ := json.Marshal(old.QA["whitebox"])
			testBytes, err := os.ReadFile(filepath.Join(old.Resources["whitebox"], "main_test.go"))
			if err != nil {
				t.Fatal(err)
			}
			production := "package main\n// adopted implementation\nfunc main(){}\n"
			head := adoptQACandidate(t, f, production)
			r, err := f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			r, err = f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			p := r.Regular.Preparation
			q := p.QA["whitebox"]
			if q.TestArtifactsFrom == "" || q.TestArtifactsBase != old.DevelopmentSnapshot || q.AuthoringBase != "" || p.DevelopmentSnapshot != head {
				t.Fatalf("incorrect projection: %+v", q)
			}
			for path, want := range map[string][]byte{"main.go": []byte(production), "main_test.go": testBytes} {
				got, err := os.ReadFile(filepath.Join(p.Resources["whitebox"], path))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("%s not carried correctly: %s %v", path, got, err)
				}
			}
			if !reflect.DeepEqual(p.Batches, old.Batches) || !reflect.DeepEqual(p.Commits, old.Commits) || !reflect.DeepEqual(r.Cost, before.Cost) || !reflect.DeepEqual(r.Executions[designAction], before.Executions[designAction]) {
				t.Fatal("adoption rewrote accepted results or cost")
			}
			history, _ := json.Marshal(r.Regular.PreparationHistory[len(r.Regular.PreparationHistory)-1].QA["whitebox"])
			if !bytes.Equal(history, qaBytes) {
				t.Fatal("adoption rewrote historical QA")
			}
			if phase == "revision" {
				x := facadeExecution(t, f, "whitebox-design")
				if x.Claim.Candidate != head {
					t.Fatal("new action bound old production")
				}
				seen := false
				stop := fmt.Errorf("fixture stops after native dispatch")
				f.fault = func(point persistence.FaultPoint) error {
					if point == persistence.FaultExecuteAfter && seen {
						return stop
					}
					return nil
				}
				if _, err := f.executeNativeTransport(x.Claim.ActionID, false, retainedSourceTransport{receipt: sourceReceiptTransport{t: t, seen: &seen}}); err != stop || !seen {
					t.Fatalf("normal dispatch blocked: %v seen=%v", err, seen)
				}
				return
			}
			// Both a pending review and an already approved design need a new QA
			// snapshot containing just the retained delta on the adopted candidate.
			r, err = f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			q = r.Regular.Preparation.QA["whitebox"]
			if q.Snapshot == "" || q.Snapshot == head || q.Frozen != old.QA["whitebox"].Frozen || !reflect.DeepEqual(q.Review, old.QA["whitebox"].Review) {
				t.Fatal("retained design/review not committed on new implementation")
			}
			if _, err := gitprep.Git(f.root, "merge-base", "--is-ancestor", head, q.Snapshot); err != nil {
				t.Fatal("QA snapshot is not based on adopted candidate", err)
			}
		})
	}
}

func TestAdoptionSettlesClaimedObsoleteQA(t *testing.T) {
	for _, phase := range []string{"revision", "design"} {
		for _, measured := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/measured=%t", phase, measured), func(t *testing.T) {
				testAdoptionSettlesQA(t, phase, measured)
			})
		}
	}
}

func testAdoptionSettlesQA(t *testing.T, phase string, measured bool) {
	t.Helper()
	t.Setenv("CODEX_HOME", t.TempDir())
	f, _ := adoptionQAFixture(t, phase)
	step := "whitebox-design"
	if phase == "design" {
		step = "whitebox-review"
	}
	x := facadeExecution(t, f, step)
	r, _ := f.Show()
	binding, err := captureRuntimeContext("codex", r.Regular.Settings.Values, f.stateDir, f.root)
	if err != nil {
		t.Fatal(err)
	}
	x.Claim.Context = binding
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	identity := "fixture-obsolete-qa"
	transcript := filepath.Join(binding.Environment["CODEX_HOME"], "sessions", "2000", "01", "01", identity+".jsonl")
	body := []byte(`{"type":"session_meta","payload":{"id":"fixture-obsolete-qa"}}` + "\n" + `{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":11,"cached_input_tokens":5,"output_tokens":3}}}}` + "\n")
	if measured {
		if err := os.MkdirAll(filepath.Dir(transcript), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(transcript, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: identity, Status: "RUNNING"}})
	job := f.codexJob(x.Claim.ActionID)
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	// A real exited process with a stale RUNNING receipt, no answer, no resume.
	cmd := exec.Command(binding.Executable, "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(job, "process.json"), map[string]any{"actionId": x.Claim.ActionID, "identity": identity, "pid": cmd.Process.Pid, "stopped": false}); err != nil {
		t.Fatal(err)
	}
	before, _ := f.Show()
	adoptQACandidate(t, f, "package main\n// latest implementation\nfunc main(){}\n")
	f, err = Open(f.root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	settled := r.Executions[x.Claim.ActionID]
	if !settled.Finished || settled.Source == nil || settled.Source.Status == "RUNNING" || settled.Identity != identity || !reflect.DeepEqual(settled.Claim, before.Executions[x.Claim.ActionID].Claim) || settled.ResumeCount != 0 {
		t.Fatalf("obsolete action not settled under original binding: %+v", settled)
	}
	usage := r.Cost.Dispatches[x.Claim.ActionID]
	if measured {
		got, err := os.ReadFile(transcript)
		if err != nil || !bytes.Equal(got, body) || settled.Source.Status != "STOPPED" || usage.Source != "transcript" || usage.TotalInputTokens != 11 || usage.OutputTokens != 3 {
			t.Fatalf("original source/cost lost: %+v %v", usage, err)
		}
	} else if settled.Source.Status != "UNAVAILABLE" || usage.Source != "unavailable" || usage.SourceReason == "" {
		t.Fatal("missing source was treated as free execution")
	}
	for id, previous := range before.Cost.Dispatches {
		if !reflect.DeepEqual(previous, r.Cost.Dispatches[id]) {
			t.Fatal("previous cost changed", id)
		}
	}
	if _, err := os.Stat(codexResumeJob(job, 1)); !os.IsNotExist(err) {
		t.Fatal("obsolete action resumed")
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	after, _ := f.Show()
	if !reflect.DeepEqual(settled, after.Executions[x.Claim.ActionID]) || !reflect.DeepEqual(r.Cost, after.Cost) {
		t.Fatal("replay changed old execution or cost")
	}
}
