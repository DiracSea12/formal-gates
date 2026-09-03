package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

func TestCLIRecordActionRequiresExplicitOperatorVerification(t *testing.T) {
	root, pkg, state := startConfirmedSemanticCLI(t, "operator-verification")
	dispatch := prepareSemanticCLI(t, root, pkg, state.RunID, "product-review", false, "")
	base := []string{"workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "product-review", "--dispatch", dispatch, "--status", "PASS"}
	if errText := failingCLI(t, base...); !strings.Contains(errText, "operator verification") || !strings.Contains(errText, "operator-evidence") {
		t.Fatalf("record-action accepted a candidate without Operator evidence: %s", errText)
	}
	partial := append(append([]string{}, base...), "--operator-evidence", "checked the candidate", "--operator-check", "binding")
	if errText := failingCLI(t, partial...); !strings.Contains(errText, "missing --operator-check values") {
		t.Fatalf("record-action accepted an incomplete Operator checklist: %s", errText)
	}
	complete := append(append([]string{}, base...), cliOperatorVerificationArgs()...)
	state = decodeSemanticState(t, runCLI(t, complete...))
	verification := state.PreDevelopmentReviewSeries["product-review"].RawCandidates[0].OperatorVerification
	if len(verification.Checks) != 8 || verification.Evidence == "" || verification.Source != "" || verification.Reason != "" {
		t.Fatalf("public Operator verification was not preserved without fixed duplicate fields: %#v", verification)
	}
	help := runCLI(t, "workflow", "record-action", "--help")
	if !strings.Contains(help, "operator-check") || !strings.Contains(help, "operator-evidence") {
		t.Fatalf("record-action help omitted Operator verification inputs: %s", help)
	}
}

// TestCLIPreDevelopmentReviewSeriesBoundaries covers AC-036..AC-041 and
// AC-058 through the public workflow commands: independent action series,
// three automatic completed rounds, runtime retry, one-round authorization,
// requirement-revision persistence, workflow show, explicit reset, and strict
// missing/inconsistent state rejection.
func TestCLIPreDevelopmentReviewSeriesBoundaries(t *testing.T) {
	root, pkg, state := startConfirmedSemanticCLI(t, "pre-review-series")

	for round := 1; round <= 3; round++ {
		dispatch := prepareSemanticCLI(t, root, pkg, state.RunID, "product-review", round > 1, "user requests round "+strconv.Itoa(round))
		state = recordSemanticCLI(t, root, pkg, state.RunID, "product-review", dispatch, "PASS", "", "")
		series := state.PreDevelopmentReviewSeries["product-review"]
		if series.Completed != round || series.RemainingAutomatic != 3-round {
			t.Fatalf("round %d projection is wrong: %#v", round, series)
		}
	}
	if start := state.PreDevelopmentReviewSeries["start-readiness"]; start.Completed != 0 || start.CurrentLimit != 3 || start.ActivationSource == "" {
		t.Fatalf("start-readiness series was not independently initialized: %#v", start)
	}

	if errText := failingCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "product-review"); !strings.Contains(errText, "requires --user-requested") {
		t.Fatalf("fourth automatic round was not hard-stopped: %s", errText)
	}
	if errText := failingCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "product-review", "--user-requested"); !strings.Contains(errText, "non-empty --user-reason") {
		t.Fatalf("empty fourth-round authorization reason was accepted: %s", errText)
	}

	extra := prepareSemanticCLI(t, root, pkg, state.RunID, "product-review", true, "user authorizes exactly one fourth semantic round")
	state = recordSemanticCLI(t, root, pkg, state.RunID, "product-review", extra, "RUNTIME_ERROR", "temporary reviewer outage", "")
	series := state.PreDevelopmentReviewSeries["product-review"]
	if series.Completed != 3 || series.ExtraAuthorized != 1 || series.ExtraConsumed != 0 || series.RemainingCapacity != 1 {
		t.Fatalf("runtime failure consumed an extra round: %#v", series)
	}
	retry := prepareSemanticCLI(t, root, pkg, state.RunID, "product-review", false, "")
	state = recordSemanticCLI(t, root, pkg, state.RunID, "product-review", retry, "PASS", "", "")
	series = state.PreDevelopmentReviewSeries["product-review"]
	if series.Completed != 4 || series.CurrentLimit != 4 || series.ExtraConsumed != 1 || len(series.Authorizations) != 1 || len(series.Authorizations[0].DispatchIDs) != 2 {
		t.Fatalf("authorized runtime retry did not consume exactly one action round: %#v", series)
	}
	if state.CompletedReviewWaves != 0 || state.ExtraReviewWaves != 0 {
		t.Fatalf("pre-development rounds leaked into post-development waves: %#v", state)
	}

	readiness := prepareSemanticCLI(t, root, pkg, state.RunID, "start-readiness", false, "")
	state = recordSemanticCLI(t, root, pkg, state.RunID, "start-readiness", readiness, "PASS", "", "")
	if state.PreDevelopmentReviewSeries["start-readiness"].Completed != 1 || state.PreDevelopmentReviewSeries["product-review"].Completed != 4 {
		t.Fatalf("action series were coupled: %#v", state.PreDevelopmentReviewSeries)
	}

	shown := decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID))
	if shown.PreDevelopmentReviewSeries["product-review"].CurrentLimit != 4 || shown.PreDevelopmentReviewSeries["product-review"].Authorizations[0].Source != "USER_REQUESTED" || shown.PreDevelopmentReviewSeries["product-review"].NextRoundRequirement != "USER_DECISION_REQUIRED" {
		t.Fatalf("workflow show omitted review capacity/source: %#v", shown.PreDevelopmentReviewSeries)
	}

	mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliStructuredRequirement("Meaning-preserved requirement revision."))
	cliGit(t, root, "add", "requirements.md")
	cliGit(t, root, "commit", "-m", "meaning-preserved requirement revision")
	state = decodeSemanticState(t, runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed", "--meaning", "preserved"))
	if state.PreDevelopmentReviewSeries["product-review"].Completed != 4 || len(state.PreDevelopmentReviewSeries["product-review"].Authorizations) != 1 {
		t.Fatalf("meaning-preserved revision reset review history: %#v", state.PreDevelopmentReviewSeries["product-review"])
	}
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliStructuredRequirement("Meaning-changing requirement revision."))
	cliGit(t, root, "add", "requirements.md")
	cliGit(t, root, "commit", "-m", "meaning-changing requirement revision")
	state = decodeSemanticState(t, runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--meaning", "changed"))
	if state.PreDevelopmentReviewSeries["product-review"].Completed != 4 || len(state.PreDevelopmentReviewSeries["product-review"].Authorizations) != 1 || state.PreDevelopmentReviewSeries["product-review"].EffectiveReviewStatus.Status != "PENDING" {
		t.Fatalf("meaning-changing invalidation reset review history instead of only staling its effective projection: %#v", state.PreDevelopmentReviewSeries["product-review"])
	}

	var reset struct {
		State validate.RunState `json:"state"`
	}
	if err := json.Unmarshal([]byte(runCLI(t, "workflow", "reset", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--user-approve")), &reset); err != nil {
		t.Fatal(err)
	}
	if got := reset.State.PreDevelopmentReviewSeries["product-review"]; got.Completed != 0 || got.CurrentLimit != 3 || len(got.RawCandidates) != 0 || len(got.Authorizations) != 0 {
		t.Fatalf("explicit reset did not establish a fresh quota: %#v", got)
	}

	// Missing and inconsistent/partially initialized series fail closed. These
	// acceptance checks intentionally simulate the explicitly required corrupt
	// state case; no compatibility/backfill behavior is expected.
	corruptPath := validate.RunStatePath(root, state.RunID)
	data, err := os.ReadFile(corruptPath)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	delete(envelope, "preDevelopmentReviewSeries")
	envelope["stateIntegrity"] = ""
	writeJSONFixture(t, corruptPath, envelope)
	if errText := failingCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID); !strings.Contains(errText, "preDevelopmentReviewSeries") {
		t.Fatalf("missing series did not fail closed: %s", errText)
	}
	if err := os.WriteFile(corruptPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	delete(envelope["preDevelopmentReviewSeries"].(map[string]any), "start-readiness")
	envelope["stateIntegrity"] = ""
	writeJSONFixture(t, corruptPath, envelope)
	if errText := failingCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID); !strings.Contains(errText, "expected exactly") {
		t.Fatalf("partially initialized series did not fail closed: %s", errText)
	}
	if err := os.WriteFile(corruptPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	product := envelope["preDevelopmentReviewSeries"].(map[string]any)["product-review"].(map[string]any)
	product["completed"] = float64(99)
	envelope["stateIntegrity"] = ""
	writeJSONFixture(t, corruptPath, envelope)
	if errText := failingCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID); !strings.Contains(errText, "does not match accepted candidate projection") {
		t.Fatalf("inconsistent completed count did not fail closed: %s", errText)
	}
}

// TestCLIReviewEvidenceAdjudicationAndCorrection covers AC-049..AC-055: raw
// candidate/finding identity, Operator receipt, exact user-bound disposition,
// deterministic blocker-to-PASS without re-review, CAS/conflict rejection,
// superseding severity correction, PASS-to-blocked downstream impact, evidence
// preservation, and whole-result invalidation without duplicate round counting.
func TestCLIReviewEvidenceAdjudicationAndCorrection(t *testing.T) {
	root, pkg, state := startConfirmedSemanticCLI(t, "review-correction")
	dispatch := prepareSemanticCLI(t, root, pkg, state.RunID, "product-review", false, "")
	state = recordSemanticCLI(t, root, pkg, state.RunID, "product-review", dispatch, "FAIL", "blocking candidate", "P1")
	series := state.PreDevelopmentReviewSeries["product-review"]
	if len(series.RawCandidates) != 1 || len(series.RawCandidates[0].Findings) != 1 || len(series.RawCandidates[0].OperatorVerification.Checks) != 8 || series.RawCandidates[0].OperatorVerification.Evidence == "" || series.EffectiveReviewStatus.Status != "FAIL" || series.Completed != 1 {
		t.Fatalf("raw/operator/effective layers were not separated: %#v", series)
	}
	candidate := series.RawCandidates[0]
	finding := candidate.Findings[0]
	state = decodeSemanticState(t, runCLI(t, exactCorrectionArgs(root, pkg, state.RunID, candidate, finding.ID, "DISMISS", "", "", "", series.EffectiveReviewStatus.Revision)...))
	series = state.PreDevelopmentReviewSeries["product-review"]
	if series.EffectiveReviewStatus.Status != "PASS" || state.Actions["product-review"].Status != "PASS" || series.Completed != 1 || len(series.Adjudications) != 1 || len(series.RawCandidates) != 1 {
		t.Fatalf("dismissing the only blocker did not directly project PASS: %#v", series)
	}
	if errText := failingCLI(t, exactCorrectionArgs(root, pkg, state.RunID, candidate, finding.ID, "CONFIRM", "", "", series.Adjudications[0].ID, 1)...); !strings.Contains(errText, "conflict") {
		t.Fatalf("stale correction CAS was accepted: %s", errText)
	}

	// Establish real downstream evidence through the public workflow before the
	// PASS-to-blocked correction. The correction must stale these dependent
	// results while preserving the immutable review ledgers and upstream facts.
	readiness := prepareSemanticCLI(t, root, pkg, state.RunID, "start-readiness", false, "")
	state = recordSemanticCLI(t, root, pkg, state.RunID, "start-readiness", readiness, "PASS", "", "")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "single bounded delivery")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "full")
	state, err := validate.LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	state.Actions["development-worker"] = validate.ActionResult{Status: "VERIFIED"}
	state.QADesignByMode["blackbox"] = validate.ActionResult{Status: "PASS"}
	state.QAReviewByMode["blackbox"] = validate.ActionResult{Status: "PASS"}
	state.QAExecutionByMode = map[string]validate.QAExecutionResult{}
	state.QAExecutionByMode["blackbox"] = validate.QAExecutionResult{Status: "PASS", Snapshot: state.CurrentSnapshot}
	for id := range state.Gates {
		state.Gates[id] = validate.GateResult{Status: "PASS", Snapshot: state.CurrentSnapshot}
	}
	state.CompletedReviewWaves = 1
	state.ExtraReviewWaves = 1
	if err := validate.SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}

	// Supersede the dismissal with a P1 severity correction: PASS becomes
	// blocking, every dependent result becomes stale/PENDING, and unrelated raw
	// evidence plus the independent action counters remain.
	state = decodeSemanticState(t, runCLI(t, exactCorrectionArgs(root, pkg, state.RunID, candidate, finding.ID, "RESEVERITIZE", "P1", "", series.Adjudications[0].ID, series.EffectiveReviewStatus.Revision)...))
	series = state.PreDevelopmentReviewSeries["product-review"]
	if series.EffectiveReviewStatus.Status != "FAIL" || len(series.EffectiveReviewStatus.DownstreamImpact) == 0 || len(series.Corrections) != 1 || series.Corrections[0].OldValue == "" || series.Corrections[0].NewValue == "" || series.Completed != 1 || series.RawCandidates[0].RawStatus != "FAIL" {
		t.Fatalf("PASS-to-blocked correction did not stale dependent evidence only: %#v", series)
	}
	if state.Actions["requirements-clarification"].Status != "PASS" || state.Actions["start-readiness"].Status != "PENDING" || state.PreDevelopmentReviewSeries["start-readiness"].Completed != 1 || state.PreDevelopmentReviewSeries["start-readiness"].EffectiveReviewStatus.Status != "PENDING" {
		t.Fatalf("PASS-to-blocked correction reset unrelated upstream evidence or failed to stale dependent start-readiness: %#v", state)
	}
	if state.Slicing != nil || state.RouteMode != "" || len(state.SelectedGates) != 0 || state.Actions["development-worker"].Status != "PENDING" || len(state.QADesignByMode) != 0 || len(state.QAReviewByMode) != 0 || len(state.QAExecutionByMode) != 0 {
		t.Fatalf("PASS-to-blocked correction retained dependent slicing/route/development/QA evidence: %#v", state)
	}
	if state.CompletedReviewWaves != 1 || state.ExtraReviewWaves != 1 {
		t.Fatalf("pre-development correction changed independent post-development review-wave counters: %#v", state)
	}
	for id, gate := range state.Gates {
		if gate.Status != "PENDING" {
			t.Fatalf("PASS-to-blocked correction retained dependent gate %s: %#v", id, gate)
		}
	}
	// A later correction that remains blocking must keep the already-staled
	// dependency list visible in workflow show/status data.
	latest := series.Corrections[len(series.Corrections)-1].ID
	state = decodeSemanticState(t, runCLI(t, exactCorrectionArgs(root, pkg, state.RunID, candidate, finding.ID, "CONFIRM", "", "", latest, series.EffectiveReviewStatus.Revision)...))
	series = state.PreDevelopmentReviewSeries["product-review"]
	if series.EffectiveReviewStatus.Status != "FAIL" || len(series.EffectiveReviewStatus.DownstreamImpact) == 0 || len(series.Corrections) != 2 {
		t.Fatalf("still-blocking correction erased the existing downstream impact: %#v", series)
	}

	// A second correction clears the false blocker without another review or
	// another completed round, and immediately reopens the legal next action.
	latest = series.Corrections[len(series.Corrections)-1].ID
	state = decodeSemanticState(t, runCLI(t, exactCorrectionArgs(root, pkg, state.RunID, candidate, finding.ID, "RESEVERITIZE", "P2", "", latest, series.EffectiveReviewStatus.Revision)...))
	series = state.PreDevelopmentReviewSeries["product-review"]
	if series.EffectiveReviewStatus.Status != "PASS" || series.Completed != 1 || len(series.RawCandidates) != 1 || len(series.Corrections) != 3 {
		t.Fatalf("blocker-clearing correction required or counted a fresh review: %#v", series)
	}
	prepareSemanticCLI(t, root, pkg, state.RunID, "start-readiness", false, "")

	// The shorthand decision after a precise re-severity must inherit corrected
	// P2, not raw P1. Its immutable receipt, effective advisory projection and
	// legacy re-review marker must agree.
	state = decodeSemanticState(t, runCLI(t, "workflow", "settle-findings", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "product-review", "--confirm", finding.Message))
	series = state.PreDevelopmentReviewSeries["product-review"]
	lastCorrection := series.Corrections[len(series.Corrections)-1]
	if lastCorrection.NewValue != "CONFIRM:P2" || series.EffectiveReviewStatus.Status != "PASS" || len(series.EffectiveReviewStatus.AdvisoryFindingIDs) != 1 || state.NeedsReReview["product-review"] != "" {
		t.Fatalf("decision after re-severity did not preserve the current effective severity in receipt/projection: receipt=%#v effective=%#v", lastCorrection, series.EffectiveReviewStatus)
	}
	state = decodeSemanticState(t, runCLI(t, "workflow", "settle-findings", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "product-review", "--dismiss", finding.Message))
	series = state.PreDevelopmentReviewSeries["product-review"]
	if got := state.SettledFindings["product-review"]; len(got) != 1 || got[0].Disposition != "dismiss" {
		t.Fatalf("legacy prompt projection accumulated superseded decisions: %#v", got)
	}
	prompt := runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "product-review", "--user-requested", "--user-reason", "user verifies corrected prompt context")
	if !strings.Contains(prompt, "[dismiss] "+finding.Message) || strings.Contains(prompt, "[confirm] "+finding.Message) || strings.Count(prompt, finding.Message) != 1 {
		t.Fatalf("review prompt did not expose exactly the latest finding decision: %s", prompt)
	}

	latest = series.Corrections[len(series.Corrections)-1].ID
	if errText := failingCLI(t, exactCorrectionArgs(root, pkg, state.RunID, candidate, finding.ID, "CONFIRM", "", "", latest, series.EffectiveReviewStatus.Revision, "--user-authorized=false")...); !strings.Contains(errText, "explicit user authorization") {
		t.Fatalf("unauthorized semantic correction was accepted: %s", errText)
	}
	misbound := candidate
	misbound.DispatchID += "-wrong"
	if errText := failingCLI(t, exactCorrectionArgs(root, pkg, state.RunID, misbound, finding.ID, "CONFIRM", "", "", latest, series.EffectiveReviewStatus.Revision)...); !strings.Contains(errText, "binding does not match") {
		t.Fatalf("misbound correction was accepted: %s", errText)
	}
	emptyReason := exactCorrectionArgs(root, pkg, state.RunID, candidate, finding.ID, "CONFIRM", "", "", latest, series.EffectiveReviewStatus.Revision)
	for index := range emptyReason {
		if emptyReason[index] == "--reason" && index+1 < len(emptyReason) {
			emptyReason[index+1] = ""
		}
	}
	if errText := failingCLI(t, emptyReason...); !strings.Contains(errText, "non-empty reason") {
		t.Fatalf("empty correction reason was accepted: %s", errText)
	}

	state = decodeSemanticState(t, runCLI(t, exactCorrectionArgs(root, pkg, state.RunID, candidate, "", "INVALIDATE_RESULT", "", "STALE", "", series.EffectiveReviewStatus.Revision)...))
	series = state.PreDevelopmentReviewSeries["product-review"]
	if series.Completed != 0 || series.EffectiveReviewStatus.Status != "PENDING" || state.Actions["product-review"].Status != "PENDING" || len(series.RawCandidates) != 1 {
		t.Fatalf("whole-result invalidation did not remove only the counted projection: %#v", series)
	}

	shown := decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID))
	shownSeries := shown.PreDevelopmentReviewSeries["product-review"]
	if len(shownSeries.RawCandidates) != 1 || len(shownSeries.Adjudications) != 1 || len(shownSeries.Corrections) != 6 || shownSeries.EffectiveReviewStatus.Source == "" {
		t.Fatalf("workflow show omitted raw/adjudication/correction/effective layers: %#v", shownSeries)
	}
}

// TestCLICorrectionInvalidatesRequirementGuaranteeReview covers the B2/B1
// integration edge of AC-055 through public commands: a product-review
// PASS-to-blocking correction must invalidate both the ordinary QA review view
// and B1's bound guarantee review/report projection.
func TestCLICorrectionInvalidatesRequirementGuaranteeReview(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), cliGuaranteeRequirement)
	cliGit(t, root, "add", "requirements.md")
	cliGit(t, root, "commit", "-m", "structured correction requirement")

	state := decodeSemanticState(t, runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "correction-guarantee", "--requirement", "requirements.md", "--vcs", "git", "--split", "no"))
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	state = decodeSemanticState(t, runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed", "--activate-guarantee"))

	product := prepareSemanticCLI(t, root, pkg, state.RunID, "product-review", false, "")
	recordArgs := []string{"workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "product-review", "--dispatch", product, "--status", "FAIL", "--finding", "guarantee blocker", "--severity", "P1", "--item", "REQ-001 obligation-to-AC completeness", "--item-status", "PASS"}
	recordArgs = append(recordArgs, cliOperatorVerificationArgs()...)
	state = decodeSemanticState(t, runCLI(t, recordArgs...))
	series := state.PreDevelopmentReviewSeries["product-review"]
	candidate := series.RawCandidates[0]
	finding := candidate.Findings[0]
	state = decodeSemanticState(t, runCLI(t, exactCorrectionArgs(root, pkg, state.RunID, candidate, finding.ID, "DISMISS", "", "", "", series.EffectiveReviewStatus.Revision)...))

	readiness := prepareSemanticCLI(t, root, pkg, state.RunID, "start-readiness", false, "")
	state = recordSemanticCLI(t, root, pkg, state.RunID, "start-readiness", readiness, "PASS", "", "")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "one bounded delivery")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "custom", "--gate", "blackbox")

	design := cliPrepareAction(t, root, pkg, state.RunID, "qa-design")
	runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", design,
		"--case", "both public outcomes", "--mode", "blackbox", "--procedure", "run the public command", "--oracle", "both outcomes pass", "--ac", "AC-001", "--ac", "AC-009")
	review := cliPrepareAction(t, root, pkg, state.RunID, "qa-review")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", review, "--reviewer", "correction-guarantee-reviewer")
	runCLI(t, "workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", review,
		"--case", "CASE-001", "--outcome", "PASS", "--source-decision", "REQ-001=PASS",
		"--point-decision", "AC-001=PASS", "--point-decision", "AC-009=PASS", "--case-decision", "CASE-001=PASS")
	shown := decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID))
	if shown.RequirementGuarantee == nil || len(shown.RequirementGuarantee.ReviewsByMode) != 1 {
		t.Fatalf("guarantee QA Review PASS was not established: %#v", shown.RequirementGuarantee)
	}

	series = shown.PreDevelopmentReviewSeries["product-review"]
	state = decodeSemanticState(t, runCLI(t, exactCorrectionArgs(root, pkg, state.RunID, candidate, finding.ID, "RESEVERITIZE", "P1", "", series.Adjudications[0].ID, series.EffectiveReviewStatus.Revision)...))
	if state.RequirementGuarantee == nil || len(state.RequirementGuarantee.ReviewsByMode) != 0 {
		t.Fatalf("correction response retained stale guarantee review/report state: %#v", state.RequirementGuarantee)
	}
	for _, item := range state.RequirementGuarantee.Report.Items {
		if item.ReviewStatus == "PASS" {
			t.Fatalf("correction response retained stale guarantee report PASS: %#v", state.RequirementGuarantee.Report)
		}
	}
	shown = decodeSemanticState(t, runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID))
	if shown.RequirementGuarantee == nil || len(shown.RequirementGuarantee.ReviewsByMode) != 0 || len(shown.QAReviewByMode) != 0 {
		t.Fatalf("PASS-to-blocked correction left conflicting guarantee/ordinary QA Review PASS state: %#v", shown)
	}
	for _, item := range shown.RequirementGuarantee.Report.Items {
		if item.ReviewStatus == "PASS" {
			t.Fatalf("guarantee report retained stale QA Review PASS after upstream correction: %#v", shown.RequirementGuarantee.Report)
		}
	}
}

func startConfirmedSemanticCLI(t *testing.T, runID string) (string, string, validate.RunState) {
	t.Helper()
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, runID)
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	state = decodeSemanticState(t, runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", runID, "--confirmed"))
	clearCLITestRequirementGuarantee(t, root, runID)
	state.RequirementGuarantee = nil
	return root, pkg, state
}

func prepareSemanticCLI(t *testing.T, root, pkg, runID, action string, userRequested bool, reason string) string {
	t.Helper()
	args := []string{"workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", action}
	if userRequested {
		args = append(args, "--user-requested", "--user-reason", reason)
	}
	prompt := runCLI(t, args...)
	state, err := validate.LoadRunState(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := cliOpenDispatch(state, "action", action)
	if dispatch == "" || !strings.Contains(prompt, dispatch) {
		t.Fatalf("semantic review prompt omitted dispatch: %s", prompt)
	}
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", runID, "--dispatch", dispatch, "--reviewer", "reviewer-"+dispatch)
	return dispatch
}

func recordSemanticCLI(t *testing.T, root, pkg, runID, action, dispatch, status, message, severity string) validate.RunState {
	t.Helper()
	args := []string{"workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", action, "--dispatch", dispatch, "--status", status}
	if status != "RUNTIME_ERROR" {
		args = append(args, cliOperatorVerificationArgs()...)
	}
	if message != "" {
		if status == "RUNTIME_ERROR" {
			args = append(args, "--message", message)
		} else {
			args = append(args, "--finding", message, "--severity", severity)
		}
	}
	if action == "product-review" {
		state, err := validate.LoadRunState(root, runID)
		if err != nil {
			t.Fatal(err)
		}
		if state.RequirementGuarantee != nil && state.RequirementGuarantee.Projection != nil {
			for _, requirement := range state.RequirementGuarantee.Projection.Requirements {
				args = append(args, "--item", requirement.ID+" obligation-to-AC completeness", "--item-status", "PASS")
			}
		}
	}
	return decodeSemanticState(t, runCLI(t, args...))
}

func exactCorrectionArgs(root, pkg, runID string, candidate validate.SemanticReviewCandidate, findingID, decision, severity, invalidity, supersedes string, expected int, extras ...string) []string {
	args := []string{"workflow", "settle-findings", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", candidate.Action, "--dispatch", candidate.DispatchID, "--result-digest", candidate.ResultDigest, "--requirement-revision", candidate.RequirementRevision, "--decision", decision, "--reason", "user verified correction", "--source", "user decision receipt", "--user-authorized", "--expected-effective-revision", strconv.Itoa(expected)}
	if findingID != "" {
		args = append(args, "--finding-id", findingID)
	}
	if severity != "" {
		args = append(args, "--severity", severity)
	}
	if invalidity != "" {
		args = append(args, "--invalidity", invalidity)
	}
	if supersedes != "" {
		args = append(args, "--supersedes", supersedes)
	}
	return append(args, extras...)
}

func decodeSemanticState(t *testing.T, output string) validate.RunState {
	t.Helper()
	var state validate.RunState
	if err := json.Unmarshal([]byte(output), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func failingCLI(t *testing.T, args ...string) string {
	t.Helper()
	clearHostEnv(t)
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr}); code == 0 {
		t.Fatalf("expected CLI failure for %s, got stdout=%s", strings.Join(args, " "), stdout.String())
	}
	return stderr.String()
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
