package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
)

func coverageCLIFixture(t *testing.T) (coverage.CoverageContract, []coverage.ReviewResult, coverage.ApprovedWhitelist, coverage.ExecutionReport) {
	t.Helper()
	sourceBinding := coverage.SourceBinding{RunID: "run-cli", RequirementRevision: "req-cli", SolutionRevision: "sol-cli"}
	required := coverage.RequiredSources{Binding: sourceBinding, Sources: []coverage.RequiredSource{{SourceID: "REQ-CLI", Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability}}}
	manifestBinding := coverage.ManifestBinding{SourceBinding: sourceBinding, ReviewKind: coverage.ReviewBlackbox, RouteScope: "regular", TopologyScope: "master"}
	manifest := coverage.AcceptanceManifest{
		Binding: manifestBinding,
		Sources: []string{"REQ-CLI"},
		Points:  []coverage.AcceptancePoint{{PointID: "POINT-CLI", ObservableBehavior: "command returns JSON", Oracle: "ok is true"}},
		Cases:   []coverage.AcceptanceCase{{CaseID: "CASE-CLI", Mode: coverage.CaseBlackbox, PublicEntry: "formal-gates coverage validate", Preconditions: "valid JSON contract", Steps: "pipe contract JSON to the command", Oracle: "JSON result has ok true"}},
		Edges:   []coverage.CoverageEdge{{ReviewKind: coverage.ReviewBlackbox, SourceID: "REQ-CLI", PointID: "POINT-CLI", CaseID: "CASE-CLI"}},
	}
	candidate := coverage.ValidationCandidate{Identity: "candidate-cli"}
	contract := coverage.CoverageContract{RequiredSources: required, SelectedKinds: []coverage.ReviewKind{coverage.ReviewBlackbox}, Manifests: []coverage.AcceptanceManifest{manifest}, Candidate: &candidate}
	reviews := []coverage.ReviewResult{{Binding: manifestBinding, Scope: coverage.ScopeFull, SourceDecisions: []coverage.ReviewDecision{{ID: "REQ-CLI", Status: coverage.StatusPass}}, PointDecisions: []coverage.ReviewDecision{{ID: "POINT-CLI", Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: "CASE-CLI", Status: coverage.StatusPass}}, SetStatus: coverage.StatusPass}}
	whitelist, err := contract.ValidateReviews(reviews)
	if err != nil {
		t.Fatalf("fixture whitelist: %v", err)
	}
	requiredDigest, err := contract.RequiredSourcesDigest()
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest, err := contract.ManifestDigest()
	if err != nil {
		t.Fatal(err)
	}
	mapDigest, err := contract.MapDigest()
	if err != nil {
		t.Fatal(err)
	}
	whitelistDigest, err := whitelist.WhitelistDigest()
	if err != nil {
		t.Fatal(err)
	}
	execution := coverage.ExecutionReport{
		Binding: coverage.ExecutionBinding{
			RequiredSourcesDigest: requiredDigest,
			ManifestDigest:        manifestDigest,
			MapDigest:             mapDigest,
			WhitelistDigest:       whitelistDigest,
			Candidate:             coverage.ValidationCandidate{Identity: "candidate-cli"},
			Scope:                 coverage.ScopeFull,
			ExpectedCaseIDs:       []string{"CASE-CLI"},
			ActualCaseIDs:         []string{"CASE-CLI"},
		},
		Records: []coverage.ExecutionRecord{{ReviewKind: coverage.ReviewBlackbox, SourceID: "REQ-CLI", PointID: "POINT-CLI", CaseID: "CASE-CLI", Result: coverage.ExecutedPass, Provenance: coverage.ProvenanceExecuted}},
	}
	return contract, reviews, whitelist, execution
}

func TestCLICoverageValidateEmitsJSONResult(t *testing.T) {
	contract, _, _, _ := coverageCLIFixture(t)
	payload, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"coverage", "validate"}, IO{Stdin: bytes.NewReader(payload), Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("coverage validate code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var response struct {
		OK     bool `json:"ok"`
		Result struct {
			Valid   bool `json:"valid"`
			Digests struct {
				Required string `json:"requiredSourcesDigest"`
				Manifest string `json:"manifestDigest"`
				Map      string `json:"mapDigest"`
			} `json:"digests"`
		} `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode result: %v (%s)", err, stdout.String())
	}
	if !response.OK || !response.Result.Valid || !strings.HasPrefix(response.Result.Digests.Required, "sha256:") || !strings.HasPrefix(response.Result.Digests.Manifest, "sha256:") || !strings.HasPrefix(response.Result.Digests.Map, "sha256:") {
		t.Fatalf("unexpected validation response: %+v", response)
	}
	if stderr.Len() != 0 {
		t.Fatalf("successful coverage command wrote stderr: %s", stderr.String())
	}
}

func TestCLICoverageProjectWhitelistAndReconcile(t *testing.T) {
	contract, reviews, wantWhitelist, execution := coverageCLIFixture(t)
	projectPayload, err := json.Marshal(struct {
		Contract coverage.CoverageContract `json:"contract"`
		Reviews  []coverage.ReviewResult   `json:"reviews"`
	}{contract, reviews})
	if err != nil {
		t.Fatal(err)
	}
	var projectOut bytes.Buffer
	if code := Run("formal-gates", []string{"coverage", "project-whitelist"}, IO{Stdin: bytes.NewReader(projectPayload), Stdout: &projectOut}); code != 0 {
		t.Fatalf("project-whitelist code=%d output=%s", code, projectOut.String())
	}
	var projected struct {
		OK     bool                       `json:"ok"`
		Result coverage.ApprovedWhitelist `json:"result"`
	}
	if err := json.Unmarshal(projectOut.Bytes(), &projected); err != nil {
		t.Fatalf("decode whitelist: %v", err)
	}
	if !projected.OK || projected.Result.Digest != wantWhitelist.Digest || len(projected.Result.Entries) != 1 {
		t.Fatalf("unexpected whitelist response: %+v", projected)
	}
	reconcilePayload, err := json.Marshal(struct {
		Contract  coverage.CoverageContract  `json:"contract"`
		Whitelist coverage.ApprovedWhitelist `json:"whitelist"`
		Execution coverage.ExecutionReport   `json:"execution"`
	}{contract, wantWhitelist, execution})
	if err != nil {
		t.Fatal(err)
	}
	var reconcileOut bytes.Buffer
	if code := Run("formal-gates", []string{"coverage", "reconcile-execution"}, IO{Stdin: bytes.NewReader(reconcilePayload), Stdout: &reconcileOut}); code != 0 {
		t.Fatalf("reconcile-execution code=%d output=%s", code, reconcileOut.String())
	}
	var reconciled struct {
		OK     bool `json:"ok"`
		Result struct {
			Valid   bool                      `json:"valid"`
			Binding coverage.ExecutionBinding `json:"binding"`
		} `json:"result"`
	}
	if err := json.Unmarshal(reconcileOut.Bytes(), &reconciled); err != nil {
		t.Fatalf("decode reconciliation: %v", err)
	}
	if !reconciled.OK || !reconciled.Result.Valid || reconciled.Result.Binding.Candidate.Identity != "candidate-cli" {
		t.Fatalf("unexpected reconciliation response: %+v", reconciled)
	}
}

func TestCLICoverageExecutionRequiresMatchingContractCandidate(t *testing.T) {
	contract, _, whitelist, execution := coverageCLIFixture(t)
	for _, test := range []struct {
		name   string
		mutate func(*coverage.CoverageContract)
		path   string
	}{
		{name: "missing", mutate: func(contract *coverage.CoverageContract) { contract.Candidate = nil }, path: "contract.candidate"},
		{name: "mismatched", mutate: func(contract *coverage.CoverageContract) {
			candidate := coverage.ValidationCandidate{Identity: "different-candidate"}
			contract.Candidate = &candidate
		}, path: "execution.binding.candidate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.mutate(&contract)
			payload, err := json.Marshal(struct {
				Contract  coverage.CoverageContract  `json:"contract"`
				Whitelist coverage.ApprovedWhitelist `json:"whitelist"`
				Execution coverage.ExecutionReport   `json:"execution"`
			}{contract, whitelist, execution})
			if err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			if code := Run("formal-gates", []string{"coverage", "reconcile-execution"}, IO{Stdin: bytes.NewReader(payload), Stdout: &stdout}); code == 0 {
				t.Fatalf("reconcile-execution unexpectedly succeeded: %s", stdout.String())
			}
			var response coverageErrorEnvelope
			if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
				t.Fatalf("decode execution error: %v (%s)", err, stdout.String())
			}
			if response.Code != coverage.CodeCandidateMismatch || response.Path != test.path {
				t.Fatalf("candidate error = %+v", response)
			}
		})
		contract, _, whitelist, execution = coverageCLIFixture(t)
	}
}

func TestCLICoverageValidateAndProjectDoNotRequireCandidate(t *testing.T) {
	contract, reviews, _, _ := coverageCLIFixture(t)
	contract.Candidate = nil
	validatePayload, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	var validateOut bytes.Buffer
	if code := Run("formal-gates", []string{"coverage", "validate"}, IO{Stdin: bytes.NewReader(validatePayload), Stdout: &validateOut}); code != 0 {
		t.Fatalf("coverage validate required a candidate: %s", validateOut.String())
	}
	projectPayload, err := json.Marshal(struct {
		Contract coverage.CoverageContract `json:"contract"`
		Reviews  []coverage.ReviewResult   `json:"reviews"`
	}{contract, reviews})
	if err != nil {
		t.Fatal(err)
	}
	var projectOut bytes.Buffer
	if code := Run("formal-gates", []string{"coverage", "project-whitelist"}, IO{Stdin: bytes.NewReader(projectPayload), Stdout: &projectOut}); code != 0 {
		t.Fatalf("coverage project-whitelist required a candidate: %s", projectOut.String())
	}
}

func TestCLICoverageValidationFailureIsStableJSON(t *testing.T) {
	contract, _, _, _ := coverageCLIFixture(t)
	contract.Manifests[0].Sources = nil
	payload, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"coverage", "validate"}, IO{Stdin: bytes.NewReader(payload), Stdout: &stdout, Stderr: &stderr}); code == 0 {
		t.Fatalf("invalid contract unexpectedly succeeded: %s", stdout.String())
	}
	var response struct {
		Code    string `json:"code"`
		Path    string `json:"path"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v (%s)", err, stdout.String())
	}
	if response.Code != coverage.CodeMissingCoverage || response.Path == "" || response.Message == "" {
		t.Fatalf("unexpected error response: %+v", response)
	}
	if stderr.Len() != 0 {
		t.Fatalf("validation failure wrote stderr: %s", stderr.String())
	}
}

func TestCLICoverageMalformedJSONUsesStableErrorEnvelope(t *testing.T) {
	var stdout bytes.Buffer
	if code := Run("formal-gates", []string{"coverage", "validate"}, IO{Stdin: strings.NewReader("{"), Stdout: &stdout}); code == 0 {
		t.Fatal("malformed JSON unexpectedly succeeded")
	}
	var response coverageErrorEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode malformed JSON response: %v", err)
	}
	if response.Code != "INVALID_JSON" || response.Path != "" || response.Message == "" {
		t.Fatalf("unexpected malformed JSON response: %+v", response)
	}
}

func TestCLICoverageAffectedAuthorizedSkipDiagnosticNamesStatus(t *testing.T) {
	contract, _, whitelist, execution := coverageCLIFixture(t)
	execution.Binding.Scope = coverage.ScopeAffected
	execution.Records[0].Provenance = coverage.ProvenanceInherited
	execution.Records[0].InheritedFromCandidate = "candidate-previous"
	execution.Records[0].ReuseEvidence = "view-receipt"
	execution.Records[0].Result = coverage.AuthorizedSkip
	payload, err := json.Marshal(struct {
		Contract  coverage.CoverageContract  `json:"contract"`
		Whitelist coverage.ApprovedWhitelist `json:"whitelist"`
		Execution coverage.ExecutionReport   `json:"execution"`
	}{contract, whitelist, execution})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if code := Run("formal-gates", []string{"coverage", "reconcile-execution"}, IO{Stdin: bytes.NewReader(payload), Stdout: &stdout}); code == 0 {
		t.Fatalf("AFFECTED AUTHORIZED_SKIP unexpectedly succeeded: %s", stdout.String())
	}
	var response coverageErrorEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode execution error: %v (%s)", err, stdout.String())
	}
	if response.Code != coverage.CodeExecutionNotPass {
		t.Fatalf("expected %s, got %+v", coverage.CodeExecutionNotPass, response)
	}
	if !strings.Contains(response.Message, string(coverage.AuthorizedSkip)) {
		t.Fatalf("diagnostic omitted %s: %q", coverage.AuthorizedSkip, response.Message)
	}
}
