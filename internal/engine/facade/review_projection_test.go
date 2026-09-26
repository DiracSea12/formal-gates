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
	// 裁决身份面：候选/派发/需求 revision/raw 状态/排除态与逐 finding 的
	// 有效裁决（DISMISS 已落）与投影代数全部在面内。
	if len(product.Candidates) != 1 || product.Candidates[0].ResultDigest != candidate.ResultDigest ||
		product.Candidates[0].DispatchID != "dispatch-1" || product.Candidates[0].RequirementRevision != "rev-1" ||
		product.Candidates[0].RawStatus != "FAIL" || product.Candidates[0].Excluded {
		t.Fatalf("candidate face = %+v", product.Candidates)
	}
	finding := product.Candidates[0].Findings[0]
	if finding.ID != candidate.Findings[0].ID || finding.Digest != candidate.Findings[0].Digest ||
		finding.Severity != "P1" || finding.Message != "blocker" || finding.EffectiveDecision != "DISMISS" {
		t.Fatalf("finding face = %+v", finding)
	}
	series, _ = set.Series(review.ProductReview)
	if product.EffectiveRevision != series.Effective.Revision {
		t.Fatalf("effective revision = %d, want the projected %d", product.EffectiveRevision, series.Effective.Revision)
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

// TestReviewFaceComposesTheCorrectionInput：仅凭 face 字段构造
// CorrectionInput 完成一次 DISMISS 的包内走查——候选摘要、派发、需求
// revision、finding 身份与 CAS 代数全部在审查面内，Show 的审查面自足
// 于裁决输入；纠正后重投影把有效裁决如实带出面。
func TestReviewFaceComposesTheCorrectionInput(t *testing.T) {
	set := review.NewSet()
	set, _, err := review.RecordCandidate(set, "run-compose", review.ProductReview,
		review.Dispatch{ID: "dispatch-compose", RequirementRevision: "rev-1"},
		review.Result{Status: "FAIL", Findings: []review.Finding{{Severity: "P1", Message: "compose blocker"}}},
		review.OperatorVerification{
			Checks:   []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"},
			Evidence: "operator verified the dispatch",
		})
	if err != nil {
		t.Fatalf("record candidate: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	state := protocol.NewState(*view)
	state.RunID = "run-compose"
	state.ReviewSeries = set

	product := reviewFace(state).Actions[0]
	candidate := product.Candidates[0]
	dismiss := review.CorrectionInput{
		Action: review.ProductReview, DispatchID: candidate.DispatchID,
		RequirementRevision: candidate.RequirementRevision,
		ResultDigest:        candidate.ResultDigest,
		FindingID:           candidate.Findings[0].ID,
		Decision:            "DISMISS", Reason: "the blocker does not survive re-verification",
		Source: "face-walkthrough", UserAuthorized: true,
		ExpectedEffectiveRevision: product.EffectiveRevision,
	}
	set, err = review.ApplyCorrection(set, "run-compose", candidate.RequirementRevision, dismiss)
	if err != nil {
		t.Fatalf("face-composed dismissal: %v", err)
	}
	series, _ := set.Series(review.ProductReview)
	if series.Effective.Status != "PASS" || !series.FirstPass {
		t.Fatalf("the face-composed dismissal must conclude the series: %+v", series.Effective)
	}
	state.ReviewSeries = set
	if decision := reviewFace(state).Actions[0].Candidates[0].Findings[0].EffectiveDecision; decision != "DISMISS" {
		t.Fatalf("the effective decision must be face-visible after the dismissal: %q", decision)
	}
}

// TestReviewFaceProjectsRequirementState：当前权威需求 revision 与最近一次
// 修订确认（revision/classification/scope/来源）进入只读投影；只有受理
// 初始账目（或账本为空）时不带最近修订。
func TestReviewFaceProjectsRequirementState(t *testing.T) {
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	state := protocol.NewState(*view)
	state.RunID = "run-requirement-face"
	state.IntakeConfirmationReceipt = &protocol.IntakeConfirmationReceipt{RequirementRevision: "rev-1"}

	// 只有回填的初始账目（或账本为空）时：投影带受理 revision、无最近修订。
	seeded := reviewFace(state)
	if seeded.RequirementRevision != "rev-1" || seeded.LastRequirementChange != nil {
		t.Fatalf("intake-only face = %+v", seeded)
	}

	state.RequirementRevisions = append(state.RequirementRevisions,
		protocol.ConfirmedRevision{Revision: "rev-1", Source: protocol.RequirementSourceIntake, Classification: protocol.RequirementClassificationIntake, StateRevision: 2},
		protocol.ConfirmedRevision{
			Revision: "rev-2", DeltaDigest: "sha256:delta", BaseRevision: "rev-1",
			Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationChanged,
			DecidedScope: []string{"product-review", "topology"}, RequestID: "req-change-1", StateRevision: 5,
		})
	face := reviewFace(state)
	if face.RequirementRevision != "rev-2" {
		t.Fatalf("requirement revision = %q, want rev-2", face.RequirementRevision)
	}
	change := face.LastRequirementChange
	if change == nil || change.Revision != "rev-2" || change.Classification != protocol.RequirementClassificationChanged ||
		change.Source != protocol.RequirementSourceUserInitiated || len(change.Scope) != 2 || change.Scope[0] != "product-review" || change.Scope[1] != "topology" {
		t.Fatalf("last change face = %+v", change)
	}
}

// TestReviewFaceProjectsGrantProvenance：每条额外轮授权都以 id/reason/占用
// 态投影——占用态随整轮失效回收而回落，未占用授权在位则如实可见。
func TestReviewFaceProjectsGrantProvenance(t *testing.T) {
	runID := "run-grants"
	verification := review.OperatorVerification{
		Checks:   []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"},
		Evidence: "operator verified the dispatch",
	}
	set := review.NewSet()
	for round := 1; round <= 3; round++ {
		next, _, err := review.RecordCandidate(set, runID, review.ProductReview,
			review.Dispatch{ID: "dispatch-" + string(rune('0'+round)), RequirementRevision: "rev-1"},
			review.Result{Status: "PASS"}, verification)
		if err != nil {
			t.Fatalf("record round %d: %v", round, err)
		}
		set = next
	}
	// 三轮自动容量耗尽、未授权：façade 面如实可见 Remaining=0 与"需要用
	// 户"（签发闸扣发期间用户看到的就是这个面）。
	exhaustedView, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	exhaustedState := protocol.NewState(*exhaustedView)
	exhaustedState.RunID = runID
	exhaustedState.ReviewSeries = set
	exhausted := reviewFace(exhaustedState).Actions[0]
	if exhausted.Remaining != 0 || exhausted.NextRoundRequirement != "USER_DECISION_REQUIRED" {
		t.Fatalf("exhausted capacity face = %+v", exhausted)
	}

	set, err = review.AppendAuthorization(set, runID, review.ProductReview, "fourth round for the open concern")
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	set, err = review.BindDispatch(set, runID, review.ProductReview, "dispatch-4", func(string) bool { return false })
	if err != nil {
		t.Fatalf("bind fourth: %v", err)
	}
	series, _ := set.Series(review.ProductReview)
	fourthGrantID := series.Authorizations[0].ID
	next, _, err := review.RecordCandidate(set, runID, review.ProductReview,
		review.Dispatch{ID: "dispatch-4", RequirementRevision: "rev-1", AuthorizationID: fourthGrantID},
		review.Result{Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "advisory concern"}}}, verification)
	if err != nil {
		t.Fatalf("record fourth: %v", err)
	}
	set = next
	set, err = review.AppendAuthorization(set, runID, review.ProductReview, "fifth round while the concern stays open")
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	set, err = review.BindDispatch(set, runID, review.ProductReview, "dispatch-5", func(string) bool { return false })
	if err != nil {
		t.Fatalf("bind fifth: %v", err)
	}

	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	state := protocol.NewState(*view)
	state.RunID = runID
	state.ReviewSeries = set

	face := reviewFace(state)
	grants := face.Actions[0].Grants
	if len(grants) != 2 {
		t.Fatalf("grants = %d, want the occupied and the fresh one", len(grants))
	}
	if grants[0].ID != fourthGrantID || grants[0].Reason != "fourth round for the open concern" || !grants[0].Occupied {
		t.Fatalf("occupied grant face = %+v", grants[0])
	}
	if grants[1].ID == fourthGrantID || grants[1].Reason != "fifth round while the concern stays open" || grants[1].Occupied {
		t.Fatalf("unoccupied grant face = %+v", grants[1])
	}
	if readiness := face.Actions[1]; len(readiness.Grants) != 0 {
		t.Fatalf("readiness grants = %+v, want none", readiness.Grants)
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
