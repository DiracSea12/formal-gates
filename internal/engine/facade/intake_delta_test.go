package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func materialFindingDriver(t *testing.T, severity string) *validationDriver {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "solution.md"), []byte("# 方案\n原一\n共同\n原二"), 0600); err != nil {
		t.Fatal(err)
	}
	d := startValidationDriverAtVCS(t, root, DefaultProvider, newFrozenGitRepo(), frozenSnapshot, nil, "solution.md")
	id := ""
	for key, p := range d.run.PendingActions {
		if p.ReviewSeries == review.ProductReview {
			id = key
		}
	}
	if id == "" {
		t.Fatal("missing original product review")
	}
	d.receipt(id)
	event, err := protocol.NewReviewWorkerResultEvent(d.eventID(), id, d.host(), "actual-review-evidence", protocol.ReviewResultEvidence{
		Action: review.ProductReview, DispatchID: id, RequirementRevision: d.revision, Status: "PASS",
		Findings: []review.Finding{{Severity: severity, Message: "已确认文档的说明需要修订", Requirement: "REQ-001", NormalPath: "阅读需求与独立方案", ActualError: "说明文本不完整", Locations: []string{"solution.md:2"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(event)
	verifyReviewFixture(t, d, event)
	request, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlReviewFindingDecision, Review: 1, Candidate: 1, Finding: 1, Choice: "CONFIRM", Reason: "用户确认问题与唯一完整修法"})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(t, d.f, request)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlReviewFindingDecision), Review: 1, Candidate: 1, Finding: 1, Choice: "CONFIRM", Reason: "用户确认原问题"})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(t, d.f, decision)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func prepareMaterialRevision(t *testing.T, d *validationDriver, document, solution string, blocking bool) ControlCandidate {
	t.Helper()
	freeze, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(t, d.f, freeze)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.f.root, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.f.root, "solution.md"), []byte(solution), 0600); err != nil {
		t.Fatal(err)
	}
	work := make([]WorkChoice, len(d.run.RequirementWork))
	for i := range work {
		work[i] = WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "说明变更保留原工作输入含义"}
	}
	input := RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Review: 1, Candidate: 1, Finding: 1, Work: work, Impact: protocol.RequirementImpact{ChangedItems: []string{"说明文本"}, CarryReasons: []string{"同一产品义务"}}}
	if blocking {
		input.Classification = protocol.RequirementClassificationChanged
		input.Redo = []string{"product-review"}
		for i := range work {
			work[i].Disposition = protocol.RequirementWorkRedo
			work[i].Reason = "确认问题需要原阶段新鲜复审"
		}
	}
	candidate, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Requirement: &input})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(t, d.f, candidate)
	if err != nil {
		t.Fatal(err)
	}
	confirm, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "用户确认逐hunk已核来源的完整变更包"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(confirm.ReviewDelta, candidate.ReviewDelta) {
		t.Fatal("confirmation changed original hunks")
	}
	return confirm
}

func TestReviewFindingMaterialDeltaRetainsLatestSolutionWithoutFreshWorker(t *testing.T) {
	t.Parallel()
	d := materialFindingDriver(t, "P2")
	original, err := d.f.confirmedIntake(d.run)
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Materials) != 2 || original.Materials[1].Content != "# 方案\n原一\n共同\n原二" {
		t.Fatalf("missing original full solution: %#v", original.Materials)
	}
	completed := d.run.Review.Actions[0].Completed
	firstDoc := validationDocument + "\n说明一。\n"
	firstSolution := "# 方案\n新一🙂\n共同\n新二\n"
	first := prepareMaterialRevision(t, d, firstDoc, firstSolution, false)
	if first.Event.Decide.Requirement.Source != protocol.RequirementSourceReviewFindingP2 || len(first.ReviewDelta) != 2 || len(first.ReviewDelta[1].Hunks) != 2 {
		t.Fatalf("missing real source or exhaustive multi-file delta: %#v", first)
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var restored ControlCandidate
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	accepted, run, err := submitControlWithHost(t, d.f, restored)
	if err != nil {
		t.Fatal(err)
	}
	d.run = run
	if d.run.Review.Actions[0].Completed != completed {
		t.Fatal("P2 material confirmation forced a fresh review")
	}
	f, err := Open(d.f.root, d.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	d.f = f
	stored, err := d.f.confirmedIntake(d.run)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Materials[1].Content != firstSolution || stored.Content != firstDoc {
		t.Fatal("reopen lost actual latest confirmed full texts")
	}
	second := prepareMaterialRevision(t, d, firstDoc, firstSolution+"继续说明", false)
	if len(second.ReviewDelta) != 1 || second.ReviewDelta[0].Path != "solution.md" || second.ReviewDelta[0].BaseRevision != stored.Materials[1].Revision {
		t.Fatal("next revision used an old worker solution")
	}
	if _, d.run, err = submitControlWithHost(t, d.f, second); err != nil {
		t.Fatal(err)
	}
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	replay, _, err := submitControlWithHost(t, d.f, restored)
	if err != nil || !reflect.DeepEqual(replay, accepted) {
		t.Fatal("late exact original confirmation lost stable acceptance", err)
	}
	after, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("late original replay changed current state", err)
	}
}

func TestReviewFindingMaterialDeltaRejectsStaleConfirmationWithoutStateChange(t *testing.T) {
	t.Parallel()
	d := materialFindingDriver(t, "P2")
	confirm := prepareMaterialRevision(t, d, validationDocument+"\n完整说明。\n", "# 方案\n完整说明。", false)
	request, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = submitControlWithHost(t, d.f, request)
	if err != nil {
		t.Fatal(err)
	}
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = submitControlWithHost(t, d.f, confirm); err == nil {
		t.Fatal("accepted obsolete confirmation token")
	}
	after, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("stale confirmation changed authority", err)
	}
}

func TestReviewFindingBlockingMaterialPackageUsesOriginalPublicProducer(t *testing.T) {
	t.Parallel()
	d := materialFindingDriver(t, "P1")
	confirm := prepareMaterialRevision(t, d, validationDocument+"\n改变产品说明。\n", "# 方案\n同步完整方案。", true)
	if confirm.Event.Decide.Requirement.Source != protocol.RequirementSourceReviewFindingP1 || !confirm.Event.Decide.Requirement.RedoProductReview {
		t.Fatal("public producer lost confirmed blocking source")
	}
	if _, _, err := submitControlWithHost(t, d.f, confirm); err != nil {
		t.Fatal(err)
	}
}

func TestReviewFindingUnchangedMaterialsDoNotCreateConfirmation(t *testing.T) {
	t.Parallel()
	d := materialFindingDriver(t, "P2")
	confirm := prepareMaterialRevision(t, d, validationDocument, "# 方案\n原一\n共同\n原二", false)
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := submitControlWithHost(t, d.f, confirm); err == nil {
		t.Fatal("unchanged material package became a new confirmation")
	}
	after, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("unchanged material rejection changed authority", err)
	}
}

func TestLightweightOrdinaryMaterialsRetainSolutionRevision(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for path, content := range map[string]string{"notes.txt": "普通需求正文。\n", "design.txt": "原完整方案。\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := PrepareIntake(root, "notes.txt", "design.txt", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	configureLightweight(t, root)
	f, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "ordinary-material-change", Route: "lightweight", IntakeConfirmationReceipt: preview.IntakeCandidate}, Admission: testAdmission(t, root, "ordinary-intake", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	request, err := f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = submitControlWithHost(t, f, request)
	if err != nil {
		t.Fatal(err)
	}
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "design.txt"), []byte("新完整方案🙂"), 0600); err != nil {
		t.Fatal(err)
	}
	candidate, err := f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Ask: askPosition(t, run, protocol.ControlRequestRequirementChange), Requirement: &RequirementChangeInput{Classification: protocol.RequirementClassificationPreserved, Impact: protocol.RequirementImpact{ChangedItems: []string{"方案说明"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.ReviewDelta) != 1 || candidate.ReviewDelta[0].Path != "design.txt" || candidate.Intake.Revision != preview.Revision {
		t.Fatal("ordinary material change lost its actual solution-only difference")
	}
	_, run, err = submitControlWithHost(t, f, candidate)
	if err != nil {
		t.Fatal(err)
	}
	confirm, err := f.PrepareControl(ControlInput{Ask: askPosition(t, run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "用户确认原逐hunk核实的方案变更包"})
	if err != nil {
		t.Fatal(err)
	}
	accepted, run, err := submitControlWithHost(t, f, confirm)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reopened.confirmedIntake(run)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Materials[0].Content != "新完整方案🙂" || stored.Materials[1].Content != preview.Content {
		t.Fatal("ordinary latest full material lost after reopen")
	}
	replay, _, err := submitControlWithHost(t, reopened, confirm)
	if err != nil || !reflect.DeepEqual(replay, accepted) {
		t.Fatal("ordinary exact confirmation replay changed acceptance", err)
	}
}
