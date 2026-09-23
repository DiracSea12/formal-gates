// review_projection_test.go 覆盖 façade Run 投影的审查面：分动作的
// completed/base/extra/effective/remaining/NextRoundRequirement、raw/
// adjudication/correction 计数、effective 状态与来源，以及拓扑/路线绑定
// 的逐项投影；lightweight run 的完整接线面。
package facade

import (
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// TestReviewFaceProjectsLedgerAndBindings：投影纯函数逐项断言。
func TestReviewFaceProjectsLedgerAndBindings(t *testing.T) {
	set := review.NewSet()
	set, _, err := review.RecordCandidate(set, "run-face", review.ProductReview,
		review.Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"},
		review.Result{Status: "FAIL", Findings: []review.Finding{{Severity: "P1", Message: "blocker"}}},
		review.OperatorVerification{
			Checks:   []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"},
			Evidence: "operator verified the dispatch",
		})
	if err != nil {
		t.Fatalf("record candidate: %v", err)
	}
	series, _ := set.Series(review.ProductReview)
	candidate := series.RawCandidates[0]
	set, err = review.ApplyCorrection(set, "run-face", "rev-1", review.CorrectionInput{
		Action: review.ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "DISMISS", Reason: "not a real blocker", Source: "engine-decide:req-1",
		UserAuthorized: true, ExpectedEffectiveRevision: 1,
	})
	if err != nil {
		t.Fatalf("apply correction: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	state := protocol.NewState(*view)
	state.RunID = "run-face"
	state.ReviewSeries = set
	state.Topology = &protocol.TopologyBinding{Decision: "no-split", Note: "建议不拆：模块小"}
	state.RouteMode = "custom"
	state.SelectedGates = []string{"gate.quality"}

	face := reviewFace(state)
	if len(face.Actions) != 2 {
		t.Fatalf("actions = %d, want both series", len(face.Actions))
	}
	product := face.Actions[0]
	if product.Action != "product-review" || product.Completed != 1 || product.BaseLimit != 3 ||
		product.ExtraAuthorized != 0 || product.EffectiveLimit != 3 || product.Remaining != 2 ||
		product.NextRoundRequirement != "AUTOMATIC_CAPACITY" || product.RawResults != 1 ||
		product.Adjudications != 1 || product.Corrections != 0 || product.EffectiveStatus != "PASS" {
		t.Fatalf("product face = %+v", product)
	}
	if product.EffectiveSource == "" || product.EffectiveSource == candidate.ResultDigest {
		t.Fatalf("effective source must name the adjudication: %q", product.EffectiveSource)
	}
	readiness := face.Actions[1]
	if readiness.Action != "start-readiness" || readiness.Completed != 0 || readiness.EffectiveStatus != "PENDING" ||
		readiness.Remaining != 3 || readiness.NextRoundRequirement != "AUTOMATIC_CAPACITY" ||
		readiness.RawResults != 0 || readiness.Adjudications != 0 || readiness.Corrections != 0 {
		t.Fatalf("readiness face = %+v", readiness)
	}
	if face.Topology == nil || face.Topology.Decision != "no-split" || face.RouteMode != "custom" ||
		len(face.SelectedGates) != 1 || face.SelectedGates[0] != "gate.quality" {
		t.Fatalf("bindings = %+v %q %v", face.Topology, face.RouteMode, face.SelectedGates)
	}
}

// TestLightweightRunCarriesReviewFace：lightweight run 的 show/drive 投影
// 带完整审查面（两条初始 series、无拓扑、无路线选择）。
func TestLightweightRunCarriesReviewFace(t *testing.T) {
	root := t.TempDir()
	request := StartRequest{
		RunID: "lightweight-review-face", Route: "lightweight", DefinitionSource: DefaultDefinitionSource,
		DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt(),
	}
	f, run, err := Start(StartOptions{Root: root, Request: request, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "target-1"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(run.Review.Actions) != 2 {
		t.Fatalf("review actions = %d, want both series", len(run.Review.Actions))
	}
	for _, face := range run.Review.Actions {
		if face.Completed != 0 || face.BaseLimit != 3 || face.Remaining != 3 ||
			face.NextRoundRequirement != "AUTOMATIC_CAPACITY" || face.EffectiveStatus != "PENDING" {
			t.Fatalf("initial series face = %+v", face)
		}
	}
	if run.Review.Topology != nil || run.Review.RouteMode != "" || run.Review.SelectedGates != nil {
		t.Fatalf("lightweight run must not carry topology or route: %+v", run.Review)
	}
	completed, err := f.Drive()
	if err != nil {
		t.Fatalf("drive: %v", err)
	}
	if completed.Status != "COMPLETE" {
		t.Fatalf("terminal status = %s", completed.Status)
	}
	if len(completed.Review.Actions) != 2 || completed.Review.RouteMode != "" {
		t.Fatalf("terminal review face = %+v", completed.Review)
	}
}
