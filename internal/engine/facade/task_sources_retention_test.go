package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

// Exercise the normal control path; all model results and transport are local fixtures.
func retainedSourceFixture(t *testing.T, mode string) (*Facade, string, []SubagentSource, []byte) {
	t.Helper()
	modes := []string{"blackbox"}
	if mode == "whitebox" {
		modes = append(modes, mode)
	}
	f := facadePreparation(t, modes)
	r, _ := f.Show()
	dev := r.Regular.Preparation.Resources["development"]
	writePreparationFile(t, dev, "go.mod", "module example.invalid/hello\n\ngo 1.23\n")
	writePreparationFile(t, dev, "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"hello\")}\n")
	r = facadeResult(t, f, facadeExecution(t, f, "development-worker"), protocol.PreparationResult{Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "local predecessor fixture", Observation: "source prepared", Result: "PASS"}}, Handoff: "none"})
	r, driveErr := f.Drive()
	if driveErr != nil {
		t.Fatal(driveErr)
	}
	if mode == "whitebox" {
		bd := facadeDesign(r, "blackbox")
		r = facadeResult(t, f, facadeExecution(t, f, "blackbox-design"), protocol.PreparationResult{Design: &bd})
		facadeResult(t, f, facadeExecution(t, f, "blackbox-review"), protocol.PreparationResult{Review: facadeReview(r, "blackbox")})
		r, driveErr = f.Drive()
		if driveErr != nil {
			t.Fatal(driveErr)
		}
	}
	x := facadeExecution(t, f, mode+"-design")
	var sources []SubagentSource
	for i := 0; i < 4; i++ {
		content := fmt.Sprintf("Public contract %d: observable behavior.\n", i)
		sources = append(sources, SubagentSource{Kind: "public-contract", Path: fmt.Sprintf("/missing/original-contract-%d", i), Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content))), Content: content})
	}
	job := f.codexJob(x.Claim.ActionID)
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(job, "sources.json"), sources); err != nil {
		t.Fatal(err)
	}
	d := facadeDesign(r, mode)
	result := protocol.PreparationResult{Design: &d}
	if mode == "whitebox" {
		writePreparationFile(t, r.Regular.Preparation.Resources[mode], "main_test.go", "package main\nimport \"testing\"\nfunc TestHello(t *testing.T){ main() }\n")
		result.Manifest = []string{"main_test.go"}
		result.Reuse = []protocol.TestReuse{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "test calls command", Procedure: "go test -run TestHello", Observation: "PASS", Comparison: "hello output", Result: "EXECUTED_PASS"}}
	}
	r = facadeResult(t, f, x, result)
	r, driveErr = f.Drive()
	if driveErr != nil {
		t.Fatal(driveErr)
	}
	r = facadeResult(t, f, facadeExecution(t, f, mode+"-review"), protocol.PreparationResult{Review: facadeReview(r, mode)})
	r, driveErr = f.Drive()
	if driveErr != nil {
		t.Fatal(driveErr)
	}
	if !r.Regular.Preparation.QA[mode].Frozen {
		t.Fatal("QA not frozen")
	}
	original, err := json.Marshal(r.Executions[x.Claim.ActionID])
	if err != nil {
		t.Fatal(err)
	}
	impact := map[string]string{"product-review": "unaffected", "start-readiness": "unaffected", "development": "unaffected", mode + "-review": "unaffected", "blackbox-review": "unaffected"}
	for id := range r.Regular.Preparation.Batches {
		impact["batch:"+id] = "unaffected"
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "clarification with unchanged observable behavior"})
	doc := protocol.Document(r.Regular.Requirement.Path, []byte(r.Regular.Requirement.Text+"\n用户说明：现有 hello 行为保持。\n"))
	writePreparationFile(t, f.root, r.Regular.Requirement.Path, doc.Text)
	facadeChoice(t, f, protocol.RegularRequest{Action: "apply-requirement", Origin: "USER_INITIATED_CHANGE", Meaning: "changed", Document: &doc, Keep: []string{"product-review", "start-readiness", mode + "-review"}, Impact: impact, Reason: "retain reviewed QA for unchanged behavior"})

	r, _ = f.Show()
	facadeChoice(t, f, protocol.RegularRequest{Action: "topology", Target: "no-split", PlanDigest: r.Regular.Plan.Digest, Reason: "one output capability"})
	r, _ = f.Show()
	q := r.Regular.Preparation.QA[mode]
	if !q.Frozen || q.DesignActionID != x.Claim.ActionID || reflect.DeepEqual(q.Design, result.Design) {
		t.Fatal("fixture did not retain and rebind QA")
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "qa-revise", Target: mode, Meaning: "stable-execution", Reason: "revise documented command path"})
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	f, err = Open(f.root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	return f, x.Claim.ActionID, sources, original
}

func TestRetainedDesignSourcesNormalRevisionDispatch(t *testing.T) {
	for _, mode := range []string{"blackbox", "whitebox"} {
		t.Run(mode, func(t *testing.T) {
			f, action, want, original := retainedSourceFixture(t, mode)
			r, _ := f.Show()
			x := facadeExecution(t, f, mode+"-design")
			q := r.Regular.Preparation.QA[mode]
			if q.PreviousDesignActionID != action {
				t.Fatal("lost exact design action")
			}
			var got []SubagentSource
			var err error
			for _, kind := range []string{"append", "replace"} {
				bad := append([]SubagentSource(nil), want...)
				if kind == "append" {
					bad = append(bad, want[0])
				} else {
					bad[0] = want[1]
				}
				if _, err := f.planActionSources(r, x.Claim, bad); err == nil || !strings.Contains(err.Error(), "ACTION_SOURCE_BINDING") {
					t.Fatalf("%s sources accepted: %v", kind, err)
				}
			}
			for _, change := range []string{"body", "cases", "sources", "source-binding", "route", "kind", "topology", "missing-action", "other-action"} {
				data, _ := json.Marshal(r)
				var bad Run
				if err := json.Unmarshal(data, &bad); err != nil {
					t.Fatal(err)
				}
				bq := bad.Regular.Preparation.QA[mode]
				switch change {
				case "body":
					bq.PreviousDesign.CoverageJudgment += " unauthorized addition"
				case "cases":
					bq.PreviousDesign.Manifest.Cases = append(bq.PreviousDesign.Manifest.Cases, bq.PreviousDesign.Manifest.Cases[0])
				case "sources":
					bq.PreviousDesign.Manifest.Sources = append(bq.PreviousDesign.Manifest.Sources, "unapproved-contract")
				case "source-binding":
					bq.PreviousDesign.Manifest.Binding.SourceBinding.RequirementRevision = "unapproved"
				case "route":
					bq.PreviousDesign.Manifest.Binding.RouteScope = "unapproved"
				case "kind":
					bq.PreviousDesign.Manifest.Binding.ReviewKind = "OTHER"
				case "topology":
					bq.PreviousDesign.Manifest.Binding.TopologyScope = "other"
				case "missing-action":
					bq.PreviousDesignActionID = ""
				case "other-action":
					bq.PreviousDesignActionID = x.Claim.ActionID
				}
				if _, err := f.planActionSources(bad, x.Claim, nil); err == nil || !strings.Contains(err.Error(), "ACTION_SOURCE_BINDING") {
					t.Fatalf("%s accepted: %v", change, err)
				}
			}
			seen := false
			stop := fmt.Errorf("fixture stops after actual dispatch")
			f.fault = func(point persistence.FaultPoint) error {
				if point == persistence.FaultExecuteAfter && seen {
					return stop
				}
				return nil
			}
			if out, err := f.executeNativeTransport(x.Claim.ActionID, false, retainedSourceTransport{receipt: sourceReceiptTransport{t: t, seen: &seen}}); err != stop {
				t.Fatalf("dispatch err=%v seen=%v next=%+v", err, seen, out.Next)
			}
			if !seen {
				t.Fatal("transport not reached")
			}
			got, err = f.capturedActionSources(x.Claim.ActionID)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("dispatch changed captured inputs", err)
			}
			after, err := f.Show()
			if err != nil {
				t.Fatal(err)
			}
			saved, err := json.Marshal(after.Executions[action])
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(original, saved) {
				t.Fatal("original action/result/identity changed")
			}
			got, err = f.capturedActionSources(action)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("original captures changed", err)
			}
		})
	}
}

// The whitebox prerequisite is a separate deterministic fixture turn. The actual
// design turn uses the same captured-input checks as blackbox.
type retainedSourceTransport struct {
	savedCoverageTransport
	receipt sourceReceiptTransport
}

func (a retainedSourceTransport) turn(q nativeTurn) (string, error) {
	if !q.NoTools {
		a.receipt.turn(q)
		return "native-retained-source-coverage", nil
	}
	if err := q.Attached("native-retained-source-coverage"); err != nil {
		return "", err
	}
	answer := []byte(`{"coverageJudgment":"independent command contract","behaviorOracles":[{"behavior":"command output","oracle":"stdout hello, stderr empty, exit 0"}]}`)
	if err := os.WriteFile(filepath.Join(q.Job, "answer.json"), answer, 0600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(q.Job, "stdout.jsonl"), []byte("fixture coverage evidence"), 0600); err != nil {
		return "", err
	}
	return "native-retained-source-coverage", nil
}
