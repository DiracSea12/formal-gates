//go:build !windows

package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

// The packet carries a real accepted review FAIL, without changing its raw
// candidate, requirement revision, finding or issued dispatch.
func canaryReviewPacket(t *testing.T) ([]byte, persistence.Envelope, protocol.State) {
	t.Helper()
	body := readTestFileBytes(t, filepath.Join("testdata", "operator_review_state.json"))
	var envelope persistence.Envelope
	var packet struct{ Content protocol.State }
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &packet); err != nil {
		t.Fatal(err)
	}
	return body, envelope, packet.Content
}

func canaryReviewRun(state *protocol.State, envelope persistence.Envelope, revision uint64) facade.Run {
	run := facade.Run{RunID: state.RunID, Revision: revision, Envelope: envelope, PendingActions: state.PendingActions}
	for _, series := range []review.Series{state.ReviewSeries.ProductReview, state.ReviewSeries.StartReadiness} {
		action := facade.ReviewActionFace{Action: string(series.Action), EffectiveStatus: series.Effective.Status, EffectiveSource: series.Effective.Source, EffectiveRevision: series.Effective.Revision}
		for _, candidate := range series.RawCandidates {
			face := facade.ReviewCandidateFace{DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, RawStatus: candidate.RawStatus, Excluded: review.ResultExcluded(series, candidate.ResultDigest)}
			for _, finding := range candidate.Findings {
				effective, severity, _ := review.EffectiveFindingDecision(series, candidate.ResultDigest, finding)
				face.Findings = append(face.Findings, facade.ReviewFindingFace{ID: finding.ID, Digest: finding.Digest, Message: finding.Message, Severity: severity, EffectiveDecision: effective, EffectiveDecisionID: review.LatestDecisionID(series, candidate.ResultDigest, finding.ID)})
			}
			action.Candidates = append(action.Candidates, face)
		}
		run.Review.Actions = append(run.Review.Actions, action)
	}
	return run
}

func TestCanaryReviewSubmissionFixture(t *testing.T) {
	args := flag.Args()
	if len(args) == 0 {
		t.Skip("public command fixture")
	}
	if len(args) != 6 || args[0] != "workflow" || args[1] != "submit" || args[2] != "--root" || args[4] != "--run-id" {
		t.Fatalf("public argv: %q", args)
	}
	dir := filepath.Join(args[3], filepath.FromSlash(facade.EngineNamespace), args[5])
	var envelope persistence.Envelope
	if err := json.Unmarshal(readTestFileBytes(t, filepath.Join(dir, "state.json")), &envelope); err != nil {
		t.Fatal(err)
	}
	var event protocol.Event
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil || (event.Kind != protocol.KindRequestControl && event.Kind != protocol.KindDecide) {
		t.Fatalf("typed review input: %v", err)
	}
	engine := canaryOperatorEngine(t, dir, envelope)
	fingerprint, err := engine.ObserveFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	acceptance, err := engine.Submit(event, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	run := canaryReviewRun(snapshot.State, envelope, snapshot.Revision)
	for id, ask := range snapshot.State.PendingAsks {
		if ask.Resolved {
			continue
		}
		token, err := snapshot.Freshness(protocol.RequestID(id))
		if err != nil {
			t.Fatal(err)
		}
		run.PendingAsks = append(run.PendingAsks, facade.PendingAskFace{RequestID: id, Control: ask.Control, Token: token, Options: ask.Options})
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Acceptance protocol.Acceptance `json:"acceptance"`
		Run        facade.Run          `json:"run"`
	}{acceptance, run}); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestCanaryReviewCorrectionPreservesRawAndContinues(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "pending-request-reopen"}[pending], func(t *testing.T) {
			body, envelope, state := canaryReviewPacket(t)
			project := t.TempDir()
			dir := filepath.Join(project, filepath.FromSlash(facade.EngineNamespace), state.RunID)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "state.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
			run := canaryReviewRun(&state, envelope, 0)
			action := run.Review.Actions[1]
			candidate := action.Candidates[0]
			finding := candidate.Findings[0]
			if action.EffectiveStatus != "FAIL" || finding.EffectiveDecision != "" {
				t.Fatal("packet has no undecided real FAIL")
			}
			correction := review.CorrectionInput{Action: review.Action(action.Action), DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, FindingID: finding.ID, Decision: "DISMISS", Reason: "Operator checked the confirmed run boundary and its explicit validation-and-repair authorization; this finding confuses initial batch scope with final run acceptance", ExpectedEffectiveRevision: action.EffectiveRevision}
			line, _ := json.Marshal(correction)
			if pending {
				engine := canaryOperatorEngine(t, dir, envelope)
				choice := protocol.AskOptionID("DISMISS:" + finding.ID)
				request, err := protocol.NewRequestEvent("operator-pending", protocol.ControlReviewFindingDecision, protocol.AskOption{ID: choice, Label: string(choice)})
				if err != nil {
					t.Fatal(err)
				}
				fp, err := engine.ObserveFingerprint()
				if err != nil {
					t.Fatal(err)
				}
				if _, err = engine.Submit(request, fp); err != nil {
					t.Fatal(err)
				}
				reopened := canaryOperatorEngine(t, dir, envelope)
				snapshot, err := reopened.Load()
				if err != nil {
					t.Fatal(err)
				}
				token, err := snapshot.Freshness(protocol.RequestID(request.ID))
				if err != nil {
					t.Fatal(err)
				}
				run.PendingAsks = append(run.PendingAsks, facade.PendingAskFace{RequestID: string(request.ID), Control: protocol.ControlReviewFindingDecision, Token: token, Options: request.Request.Options})
			}
			binary := filepath.Join(t.TempDir(), "submit")
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run=^TestCanaryReviewSubmissionFixture$ -- \"$@\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			c := &engineCanary{t: t, binary: binary, project: project, home: t.TempDir(), jobs: t.TempDir(), run: run, approvalDeadline: time.Now().Add(10 * time.Second), approvalMu: &sync.Mutex{}, approvalInput: bufio.NewReader(bytes.NewReader(append(line, '\n'))), approvalOutput: &output, seq: 1000}
			if !c.reviewFindingsWithOperator() {
				t.Fatal("undecided review was not handed to Operator")
			}
			engine := canaryOperatorEngine(t, dir, envelope)
			snapshot, err := engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(state.ReviewSeries.StartReadiness.RawCandidates, snapshot.State.ReviewSeries.StartReadiness.RawCandidates) || !reflect.DeepEqual(state.Results, snapshot.State.Results) {
				t.Fatal("correction rewrote raw review or result")
			}
			if snapshot.State.ReviewSeries.StartReadiness.Effective.Status != "PASS" || snapshot.State.ReviewSeries.StartReadiness.Completed != state.ReviewSeries.StartReadiness.Completed || len(c.run.PendingAsks) != 0 {
				t.Fatal("typed correction did not resolve its finding without new review")
			}
			if c.reviewFindingsWithOperator() {
				t.Fatal("completed correction consumed human input again")
			}
			before := readTestFileBytes(t, filepath.Join(dir, "state.json"))
			reopened := canaryOperatorEngine(t, dir, envelope)
			fp, err := reopened.ObserveFingerprint()
			if err != nil {
				t.Fatal(err)
			}
			plan, _, err := reopened.Drive(fp)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Next.Kind == decision.KindWait {
				t.Fatal("corrected review remains dependency-blocked")
			}
			if !strings.Contains(output.String(), candidate.ResultDigest) || !strings.Contains(output.String(), finding.ID) || !strings.Contains(output.String(), "state=") {
				t.Fatal("handoff omitted original evidence identity")
			}
			if bytes.Equal(body, before) {
				t.Fatal("explicit correction was not persisted")
			}
		})
	}
}

func TestCanaryReviewInputKeepsBindingAndDeadline(t *testing.T) {
	_, envelope, state := canaryReviewPacket(t)
	action := canaryReviewRun(&state, envelope, 0).Review.Actions[1]
	candidate := action.Candidates[0]
	finding := candidate.Findings[0]
	valid := review.CorrectionInput{Action: review.Action(action.Action), DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, FindingID: finding.ID, Decision: "DISMISS", Reason: "Operator's explicit reason", ExpectedEffectiveRevision: action.EffectiveRevision}
	for _, name := range []string{"valid", "wrong-result", "wrong-finding", "wrong-revision", "stale-generation", "no-progress", "unknown-field", "eof", "deadline"} {
		t.Run(name, func(t *testing.T) {
			value := valid
			switch name {
			case "wrong-result":
				value.ResultDigest = "different"
			case "wrong-finding":
				value.FindingID = "different"
			case "wrong-revision":
				value.RequirementRevision = "different"
			case "stale-generation":
				value.ExpectedEffectiveRevision++
			case "no-progress":
				value.Supersedes = "unrecorded"
			}
			body, _ := json.Marshal(value)
			line := string(body) + "\n"
			if name == "unknown-field" {
				line = strings.TrimSuffix(string(body), "}") + `,"instruction":"dismiss automatically"}` + "\n"
			}
			if name == "eof" {
				line = ""
			}
			deadline := time.Now().Add(time.Second)
			if name == "deadline" {
				deadline = time.Now().Add(-time.Second)
			}
			c := &engineCanary{t: t, project: t.TempDir(), jobs: t.TempDir(), run: facade.Run{RunID: state.RunID}, approvalDeadline: deadline, approvalInput: bufio.NewReader(strings.NewReader(line)), approvalOutput: io.Discard}
			_, err := c.readReviewCorrection(action, candidate, finding)
			if (name == "valid") != (err == nil) {
				t.Fatalf("input %s: %v", name, err)
			}
		})
	}
}

func TestCanaryReviewDecisionKeepsCoreRejections(t *testing.T) {
	body, envelope, state := canaryReviewPacket(t)
	candidate := state.ReviewSeries.StartReadiness.RawCandidates[0]
	finding := candidate.Findings[0]
	for _, name := range []string{"missing-reason", "stale-generation", "unknown-finding"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "state.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
			engine := canaryOperatorEngine(t, dir, envelope)
			id := finding.ID
			if name == "unknown-finding" {
				id = "unissued-finding"
			}
			choice := protocol.AskOptionID("DISMISS:" + id)
			request, err := protocol.NewRequestEvent("operator-request", protocol.ControlReviewFindingDecision, protocol.AskOption{ID: choice, Label: string(choice)})
			if err != nil {
				t.Fatal(err)
			}
			fp, err := engine.ObserveFingerprint()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = engine.Submit(request, fp); err != nil {
				t.Fatal(err)
			}
			snapshot, err := engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			token, err := snapshot.Freshness(protocol.RequestID(request.ID))
			if err != nil {
				t.Fatal(err)
			}
			correction := review.CorrectionInput{Action: review.StartReadiness, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, FindingID: id, Decision: "DISMISS", Reason: "explicit Operator judgment", ExpectedEffectiveRevision: state.ReviewSeries.StartReadiness.Effective.Revision}
			if name == "missing-reason" {
				correction.Reason = ""
			}
			if name == "stale-generation" {
				correction.ExpectedEffectiveRevision++
			}
			event, err := protocol.NewReviewDecideEvent("operator-decision", protocol.RequestID(request.ID), token, choice, protocol.ReviewDecisionPayload{Correction: &correction})
			if err != nil {
				t.Fatal(err)
			}
			before := readTestFileBytes(t, filepath.Join(dir, "state.json"))
			fp, err = engine.ObserveFingerprint()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = engine.Submit(event, fp); err == nil {
				t.Fatal("core accepted invalid correction")
			} else {
				t.Logf("core refusal: %v", err)
			}
			if !bytes.Equal(before, readTestFileBytes(t, filepath.Join(dir, "state.json"))) {
				t.Fatal("rejected correction changed accepted packet")
			}
		})
	}
}

func TestCanaryReviewPassAdvisoryDoesNotReadOperator(t *testing.T) {
	var series review.Series
	if err := json.Unmarshal(readTestFileBytes(t, filepath.Join("testdata", "operator_review_advisory.json")), &series); err != nil {
		t.Fatal(err)
	}
	if series.Effective.Status != "PASS" || len(series.RawCandidates) != 1 || len(series.RawCandidates[0].Findings) == 0 {
		t.Fatal("original PASS advisory missing")
	}
	state := protocol.State{ReviewSeries: review.Set{ProductReview: series}}
	run := canaryReviewRun(&state, persistence.Envelope{}, 0)
	var output bytes.Buffer
	c := &engineCanary{t: t, run: run, approvalDeadline: time.Now().Add(time.Second), approvalInput: bufio.NewReader(bytes.NewReader(nil)), approvalOutput: &output}
	if c.reviewFindingsWithOperator() || output.Len() != 0 {
		t.Fatal("PASS advisory created a mandatory Operator handoff")
	}
}

// Accepted candidates from the review owner exercise continuation across
// multiple decisions; the captured packet's source results remain unchanged.
func TestCanaryReviewContinuesAcrossFindingDecisions(t *testing.T) {
	for _, target := range []review.Action{review.ProductReview, review.StartReadiness} {
		t.Run(string(target), func(t *testing.T) {
			for _, pending := range []bool{false, true} {
				t.Run(map[bool]string{false: "normal", true: "pending-reopen"}[pending], func(t *testing.T) {
					body, envelope, state := canaryReviewPacket(t)
					original := state.ReviewSeries.StartReadiness.RawCandidates[0]
					first := original.Findings[0]
					first.ID, first.Digest = "", ""
					first.Message = "first independently checked review blocker"
					second := first
					second.Message = "second independently checked review blocker"
					updated, _, err := review.RecordCandidate(state.ReviewSeries, state.RunID, target, review.Dispatch{ID: "independent-review", RequirementRevision: original.RequirementRevision}, review.Result{Status: "FAIL", Findings: []review.Finding{first, second}}, original.OperatorVerification)
					if err != nil {
						t.Fatal(err)
					}
					project := t.TempDir()
					dir := filepath.Join(project, filepath.FromSlash(facade.EngineNamespace), state.RunID)
					if err = os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(filepath.Join(dir, "state.json"), body, 0600); err != nil {
						t.Fatal(err)
					}
					engine := canaryOperatorEngine(t, dir, envelope)
					before, err := engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					fp, err := engine.ObserveFingerprint()
					if err != nil {
						t.Fatal(err)
					}
					state.ReviewSeries = updated
					store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: envelope.PackageDigest, DefinitionSource: envelope.DefinitionSource, InstalledTargetIdentity: envelope.InstalledTargetIdentity})
					if err != nil {
						t.Fatal(err)
					}
					if _, err = store.Save(persistence.Transaction{ExpectedRevision: before.Revision, ExpectedFingerprint: fp, CollectFingerprint: engine.ObserveFingerprint, Content: &state}); err != nil {
						t.Fatal(err)
					}
					snapshot, err := engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					run := canaryReviewRun(snapshot.State, envelope, snapshot.Revision)
					var lines bytes.Buffer
					count := 0
					for _, action := range run.Review.Actions {
						decisions := 0
						for _, candidate := range action.Candidates {
							if candidate.Excluded || candidate.RequirementRevision != original.RequirementRevision {
								continue
							}
							for _, finding := range candidate.Findings {
								if finding.EffectiveDecision != "" {
									continue
								}
								correction := review.CorrectionInput{Action: review.Action(action.Action), DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, FindingID: finding.ID, Decision: "DISMISS", Reason: "explicit Operator judgment for this particular finding", ExpectedEffectiveRevision: action.EffectiveRevision + decisions}
								if err = json.NewEncoder(&lines).Encode(correction); err != nil {
									t.Fatal(err)
								}
								decisions++
								count++
							}
						}
					}

					if count < 2 {
						t.Fatal("multi-finding prerequisite missing")
					}
					executable, err := os.Executable()
					if err != nil {
						t.Fatal(err)
					}
					binary := filepath.Join(t.TempDir(), "submit")
					quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
					if err = os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run=^TestCanaryReviewSubmissionFixture$ -- \"$@\"\n"), 0700); err != nil {
						t.Fatal(err)
					}
					var output bytes.Buffer
					c := &engineCanary{t: t, binary: binary, home: t.TempDir(), project: project, jobs: t.TempDir(), run: run, seq: 2000, approvalDeadline: time.Now().Add(10 * time.Second), approvalMu: &sync.Mutex{}, approvalInput: bufio.NewReader(&lines), approvalOutput: &output}
					for i := 0; i < count; i++ {
						if pending && i == 1 {
							var next review.CorrectionInput
							nextLine, err := c.approvalInput.ReadString('\n')
							if err != nil {
								t.Fatal(err)
							}
							if err = json.Unmarshal([]byte(nextLine), &next); err != nil {
								t.Fatal(err)
							}
							c.approvalInput = bufio.NewReader(io.MultiReader(strings.NewReader(nextLine), c.approvalInput))
							choice := protocol.AskOptionID(next.Decision + ":" + next.FindingID)
							request, err := protocol.NewRequestEvent(c.eventID(), protocol.ControlReviewFindingDecision, protocol.AskOption{ID: choice, Label: string(choice)})
							if err != nil {
								t.Fatal(err)
							}
							c.submit(request)
							engine = canaryOperatorEngine(t, dir, envelope)
						}
						if !c.reviewFindingsWithOperator() {
							t.Fatalf("finding %d disappeared after prior decision", i+1)
						}
						current := c.run.Review.Actions[0]
						if target == review.StartReadiness {
							current = c.run.Review.Actions[1]
						}
						if current.EffectiveSource == current.Candidates[len(current.Candidates)-1].ResultDigest {
							t.Fatal("test projection concealed the adjudication Source")
						}
					}
					if c.reviewFindingsWithOperator() {
						t.Fatal("resolved findings consumed human input again")
					}
					snapshot, err = engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					final, _ := snapshot.State.ReviewSeries.Series(target)
					if final.Effective.Status != "PASS" {
						t.Fatal("per-finding decisions did not close the blockers")
					}
					expected := updated
					actual := snapshot.State.ReviewSeries
					if !reflect.DeepEqual(expected.ProductReview.RawCandidates, actual.ProductReview.RawCandidates) || !reflect.DeepEqual(expected.StartReadiness.RawCandidates, actual.StartReadiness.RawCandidates) || !reflect.DeepEqual(state.Results, snapshot.State.Results) {
						t.Fatal("decisions changed raw candidate or source result")
					}
					if strings.Count(output.String(), "canary review correction accepted:") != count {
						t.Fatal("explicit per-finding progress missing")
					}
				})
			}
		})
	}
}

func TestCanaryWholeReviewInvalidationPreservesEvidenceAndRequestsFreshWork(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "pending-reopen"}[pending], func(t *testing.T) {
			body, envelope, state := canaryReviewPacket(t)
			project := t.TempDir()
			dir := filepath.Join(project, filepath.FromSlash(facade.EngineNamespace), state.RunID)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "state.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
			run := canaryReviewRun(&state, envelope, 0)
			action := run.Review.Actions[1]
			candidate := action.Candidates[0]
			correction := review.CorrectionInput{Action: review.StartReadiness, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, Decision: "INVALIDATE_RESULT", Invalidity: "INVALID", Reason: "Operator checked that this proposed initial batch contradicts the confirmed scope", ExpectedEffectiveRevision: action.EffectiveRevision}
			line, err := json.Marshal(correction)
			if err != nil {
				t.Fatal(err)
			}
			if pending {
				engine := canaryOperatorEngine(t, dir, envelope)
				choice := protocol.AskOptionID("INVALIDATE_RESULT")
				request, err := protocol.NewRequestEvent("whole-pending", protocol.ControlReviewFindingDecision, protocol.AskOption{ID: choice, Label: string(choice)})
				if err != nil {
					t.Fatal(err)
				}
				fp, err := engine.ObserveFingerprint()
				if err != nil {
					t.Fatal(err)
				}
				if _, err = engine.Submit(request, fp); err != nil {
					t.Fatal(err)
				}
				reopened := canaryOperatorEngine(t, dir, envelope)
				snapshot, err := reopened.Load()
				if err != nil {
					t.Fatal(err)
				}
				token, err := snapshot.Freshness(protocol.RequestID(request.ID))
				if err != nil {
					t.Fatal(err)
				}
				run.PendingAsks = []facade.PendingAskFace{{RequestID: string(request.ID), Control: protocol.ControlReviewFindingDecision, Token: token, Options: request.Request.Options}}
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(t.TempDir(), "submit")
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
			if err = os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run=^TestCanaryReviewSubmissionFixture$ -- \"$@\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			c := &engineCanary{t: t, binary: binary, home: t.TempDir(), project: project, jobs: t.TempDir(), run: run, seq: 3000, approvalDeadline: time.Now().Add(10 * time.Second), approvalMu: &sync.Mutex{}, approvalInput: bufio.NewReader(bytes.NewReader(append(line, '\n'))), approvalOutput: &output}
			if !c.reviewFindingsWithOperator() {
				t.Fatal("whole-result Operator decision was not consumed")
			}
			engine := canaryOperatorEngine(t, dir, envelope)
			snapshot, err := engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			series := snapshot.State.ReviewSeries.StartReadiness
			if series.Effective.Status != "PENDING" || !review.ResultExcluded(series, candidate.ResultDigest) || len(series.Corrections) != 1 {
				t.Fatal("whole result was not excluded by a core receipt")
			}
			if !reflect.DeepEqual(state.ReviewSeries.StartReadiness.RawCandidates, series.RawCandidates) || !reflect.DeepEqual(state.Results, snapshot.State.Results) {
				t.Fatal("invalidation rewrote raw candidate or source result")
			}
			found := false
			for _, decide := range snapshot.State.Decisions {
				if decide.Review != nil && decide.Review.Correction != nil && decide.Review.Correction.Decision == "INVALIDATE_RESULT" {
					found = true
					if decide.Choice != "INVALIDATE_RESULT" || decide.Review.Correction.FindingID != "" || decide.Review.Correction.Supersedes != "" {
						t.Fatal("consumer changed whole-result decision semantics")
					}
				}
			}
			if !found || c.reviewFindingsWithOperator() {
				t.Fatal("whole decision missing or excluded candidate requested again")
			}
			fp, err := engine.ObserveFingerprint()
			if err != nil {
				t.Fatal(err)
			}
			plan, _, err := engine.Drive(fp)
			if err != nil {
				t.Fatal(err)
			}
			fresh := false
			for id, pending := range snapshot.State.PendingActions {
				if id != candidate.DispatchID && pending.Step == "review.readiness" && pending.ReviewSeries == review.StartReadiness && pending.Task.Scope != "" {
					fresh = true
				}
			}
			if !fresh {
				t.Fatal("whole invalidation did not issue a distinct fresh readiness obligation")
			}
			if plan.Next.Kind != decision.KindWait || plan.Next.Wait == nil || plan.Next.Wait.Reason != decision.WaitTasksInFlight {
				t.Fatalf("issued fresh review must remain in flight: %+v", plan.Next)
			}
			if !strings.Contains(output.String(), "INVALIDATE_RESULT requires empty findingId and supersedes") {
				t.Fatal("whole-result instruction missing")
			}
		})
	}
}

func TestCanaryWholeReviewInputKeepsBothDecisionBindings(t *testing.T) {
	_, envelope, state := canaryReviewPacket(t)
	action := canaryReviewRun(&state, envelope, 0).Review.Actions[1]
	candidate := action.Candidates[0]
	finding := candidate.Findings[0]
	valid := review.CorrectionInput{Action: review.StartReadiness, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, Decision: "INVALIDATE_RESULT", Invalidity: "INVALID", Reason: "explicit Operator reason", ExpectedEffectiveRevision: action.EffectiveRevision}
	for _, name := range []string{"whole", "wrong-action", "wrong-dispatch", "wrong-result", "wrong-revision", "stale-cas", "whole-finding", "whole-supersedes", "finding-empty", "finding-exact", "finding-wrong"} {
		t.Run(name, func(t *testing.T) {
			value := valid
			switch name {
			case "wrong-action":
				value.Action = review.ProductReview
			case "wrong-dispatch":
				value.DispatchID = "different"
			case "wrong-result":
				value.ResultDigest = "different"
			case "wrong-revision":
				value.RequirementRevision = "different"
			case "stale-cas":
				value.ExpectedEffectiveRevision++
			case "whole-finding":
				value.FindingID = finding.ID
			case "whole-supersedes":
				value.Supersedes = "decision"
			case "finding-empty":
				value.Decision = "DISMISS"
			case "finding-exact":
				value.Decision = "DISMISS"
				value.FindingID = finding.ID
			case "finding-wrong":
				value.Decision = "DISMISS"
				value.FindingID = "different"
			}
			body, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			c := &engineCanary{t: t, run: facade.Run{RunID: state.RunID}, approvalDeadline: time.Now().Add(time.Second), approvalInput: bufio.NewReader(bytes.NewReader(append(body, '\n'))), approvalOutput: io.Discard}
			_, err = c.readReviewCorrection(action, candidate, finding)
			if (name == "whole" || name == "finding-exact") != (err == nil) {
				t.Fatalf("decision %s: %v", name, err)
			}
		})
	}
}
