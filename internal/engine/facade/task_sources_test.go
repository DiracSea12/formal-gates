package facade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func acceptedCapturedDesign(t *testing.T) (*Facade, protocol.RegularExecution, []SubagentSource) {
	t.Helper()
	f := facadePreparation(t, []string{"blackbox"})
	x := facadeExecution(t, f, "blackbox-design")
	r, _ := f.Show()
	d := facadeDesign(r, "blackbox")
	sources := []SubagentSource{}
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
	facadeResult(t, f, x, protocol.PreparationResult{Design: &d})
	root, id := f.root, f.runID
	f = nil
	var err error
	f, err = Open(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return f, x, sources
}

type sourceReceiptTransport struct {
	codexRuntimeAdapter
	t    *testing.T
	seen *bool
}

func (a sourceReceiptTransport) turn(q nativeTurn) (string, error) {
	*a.seen = true
	for i := 0; i < 4; i++ {
		body := fmt.Sprintf("Public contract %d: observable behavior.\n", i)
		path := filepath.Join(q.Job, fmt.Sprintf("public-source-%d.txt", i))
		if strings.Contains(q.Prompt, body) || !strings.Contains(q.Prompt, path) {
			a.t.Fatal("contract must be handed off by captured path, not body")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != body {
			a.t.Fatal("captured contract unreadable", err)
		}
	}
	return "", fmt.Errorf("fixture stops after inspecting actual turn input")
}
func TestCandidateSourcesDispatchAfterReopen(t *testing.T) {
	f, _, want := acceptedCapturedDesign(t)
	x := facadeExecution(t, f, "blackbox-review")
	seen := false
	_, err := f.executeNativeTransport(x.Claim.ActionID, false, sourceReceiptTransport{t: t, seen: &seen})
	if err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("transport not called")
	}
	got, err := f.capturedActionSources(x.Claim.ActionID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("capture not fixed", err)
	}
}
func TestCandidateSourcesMissingBeforeClaim(t *testing.T) {
	f, d, _ := acceptedCapturedDesign(t)
	if err := os.RemoveAll(f.codexJob(d.Claim.ActionID)); err != nil {
		t.Fatal(err)
	}
	x := facadeExecution(t, f, "blackbox-review")
	before, _ := f.Show()
	seen := false
	_, err := f.executeNativeTransport(x.Claim.ActionID, false, sourceReceiptTransport{t: t, seen: &seen})
	if err == nil || !strings.Contains(err.Error(), "ACTION_SOURCE_BINDING") {
		t.Fatal("missing preclaim failure", err)
	}
	after, _ := f.Show()
	if seen || before.Revision != after.Revision || after.Executions[x.Claim.ActionID].Claimed {
		t.Fatal("missing input consumed or started action")
	}
}
func TestCandidateUnstartedClaimRecovery(t *testing.T) {
	for _, evidence := range []string{"", "process.json", "subagent.json", "prompt.txt"} {
		t.Run(evidence, func(t *testing.T) {
			f := readyRegularFacade(t)
			r, _ := f.Show()
			var x protocol.RegularExecution
			for _, v := range r.Executions {
				x = v
				break
			}
			preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
			if evidence != "" {
				job := f.codexJob(x.Claim.ActionID)
				os.MkdirAll(job, 0700)
				os.WriteFile(filepath.Join(job, evidence), nil, 0600)
			}
			before, _ := f.Show()
			seen := false
			r, err := f.executeNativeTransport(x.Claim.ActionID, true, sourceReceiptTransport{t: t, seen: &seen})
			if seen {
				t.Fatal("recovery started another transport")
			}
			if evidence != "" {
				after, _ := f.Show()
				if err == nil || !strings.Contains(err.Error(), "UNKNOWN") || after.Revision != before.Revision {
					t.Fatal("evidenced claim recovered", err)
				}
				return
			}
			if err != nil || !r.Executions[x.Claim.ActionID].Finished {
				t.Fatal("unstarted claim not settled", err)
			}
			found := false
			for id, next := range r.Executions {
				if id != x.Claim.ActionID && next.Claim.Task == x.Claim.Task && !next.Claimed {
					found = true
				}
			}
			if !found {
				t.Fatal("missing engine retry")
			}
		})
	}
}
func TestActionSourcesExactDesignAndRole(t *testing.T) {
	f, x, want := acceptedCapturedDesign(t)
	r, _ := f.Show()
	review := facadeExecution(t, f, "blackbox-review")
	// A newer unrelated design result is not the accepted design's source.
	other := r.Executions[x.Claim.ActionID]
	copyResult := *other.PreparationResult
	copyDesign := *copyResult.Design
	copyDesign.CoverageJudgment = "different revision"
	copyResult.Design = &copyDesign
	other.PreparationResult = &copyResult
	r.Executions["other-design"] = other
	got, err := f.planActionSources(r, review.Claim, nil)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("selected unrelated design", err)
	}
	q := r.Regular.Preparation.QA["blackbox"]
	q.DesignActionID = ""
	if _, err = f.planActionSources(r, review.Claim, nil); err == nil {
		t.Fatal("unbound design was inferred from history")
	}
	q.PreviousDesign, q.PreviousDesignActionID = q.Design, x.Claim.ActionID
	q.Design = nil
	q.DesignActionID = ""
	claim := review.Claim
	claim.Task.Step = "blackbox-design"
	got, err = f.planActionSources(r, claim, nil)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("revision lost prior captures", err)
	}
	claim.Task.Step = "development-worker"
	got, err = f.planActionSources(r, claim, nil)
	if err != nil || len(got) != 0 {
		t.Fatal("cross-role inheritance")
	}
	bad := append([]SubagentSource(nil), want...)
	bad[0].Content = "another public contract"
	bad[0].Digest = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(bad[0].Content)))
	claim.Task.Step = "blackbox-design"
	if _, err = f.planActionSources(r, claim, bad); err == nil {
		t.Fatal("caller replaced accepted input")
	}
}

func TestActionSourcesEmptyRegistrationDiffersFromMissingCapture(t *testing.T) {
	f, d, _ := acceptedCapturedDesign(t)
	job := f.codexJob(d.Claim.ActionID)
	if err := writeJSONAtomic(filepath.Join(job, "sources.json"), []SubagentSource{}); err != nil {
		t.Fatal(err)
	}
	got, err := f.capturedActionSources(d.Claim.ActionID)
	if err != nil || len(got) != 0 {
		t.Fatal("valid empty registration rejected", err)
	}
	if err = os.Remove(filepath.Join(job, "sources.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = f.capturedActionSources(d.Claim.ActionID); err == nil {
		t.Fatal("missing capture silently treated as empty")
	}
}

func TestCandidateContinuousSkipsCompletedDesktopSource(t *testing.T) {
	f, d, _ := acceptedCapturedDesign(t)
	r, _ := f.Show()
	facadeResult(t, f, facadeExecution(t, f, "blackbox-review"), protocol.PreparationResult{Review: facadeReview(r, "blackbox")})
	dev := facadeExecution(t, f, "development-worker")
	facadeChoice(t, f, protocol.RegularRequest{Action: "executor-handoff", Target: dev.Claim.ActionID, Reason: "fixture externally owned batch", Handoff: &protocol.ExecutorHandoff{To: protocol.ExecutorTarget{Kind: "external", Provider: "chatgpt", Label: "fixture"}}})
	path := filepath.Join(f.codexJob(d.Claim.ActionID), "sources.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := f.Show()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := f.DriveContinuous(ctx)
	if err != nil || result.Boundary != "EXTERNAL_EXECUTOR" || len(result.Blocked) != 0 {
		t.Fatalf("completed desktop action was reconciled: %s %v %v", result.Boundary, result.Blocked, err)
	}
	if !reflect.DeepEqual(before.Executions[d.Claim.ActionID], result.Run.Executions[d.Claim.ActionID]) {
		t.Fatal("completed desktop execution changed")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("desktop capture changed", err)
	}
}

func TestCandidateUnstartedClaimRejectsExecutorHistory(t *testing.T) {
	x := protocol.RegularExecution{Claimed: true, Claim: protocol.ExecutionClaim{Operation: "spawn"}}
	job := filepath.Join(t.TempDir(), "missing")
	if !unstartedActionClaim(x, job) {
		t.Fatal("initial empty claim rejected")
	}
	for _, change := range []func(*protocol.RegularExecution){
		func(x *protocol.RegularExecution) { x.SupersededBy = "replacement" },
		func(x *protocol.RegularExecution) { x.ExecutorHandoffID = "handoff" },
		func(x *protocol.RegularExecution) { x.Executor = &protocol.ExecutorTarget{Kind: "external"} },
	} {
		copy := x
		change(&copy)
		if unstartedActionClaim(copy, job) {
			t.Fatal("executor evidence accepted as initial start")
		}
	}
}

func TestCanonicalSourcesNeverReadsLegacyDispatch(t *testing.T) {
	f, x, want := acceptedCapturedDesign(t)
	if err := os.WriteFile(filepath.Join(f.codexJob(x.Claim.ActionID), "subagent.json"), []byte("invalid old data"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := f.capturedActionSources(x.Claim.ActionID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("normal reader used legacy", err)
	}
	if err = os.Remove(filepath.Join(f.codexJob(x.Claim.ActionID), "sources.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = f.capturedActionSources(x.Claim.ActionID); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatal("missing canonical did not fail")
	}
}
