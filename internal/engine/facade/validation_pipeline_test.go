package facade

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

// Semantic fixtures bind an explicit generic platform; execution and lifecycle
// evidence retain the same provider identity as their issued calls.
func init() {
	descriptor, err := host.Lookup(host.Default)
	if err != nil {
		panic(err)
	}
	platform := descriptor
	platform.ID, platform.Aliases = DefaultProvider, nil
	if err := host.Register(&platform); err != nil {
		panic(err)
	}
}

const validationDocument = "# Delivery\n\n## 需求点\n\n### REQ-001：命令\n\n#### 要求\n命令验证输入。\n\n#### 验收条件\n- AC-001：正常输入成功，错误输入拒绝。\n\n#### 来源\n用户要求。\n"

// frozenSnapshot 是冻结快照夹具身份：40 位 hex，单链冻结仓的 base。
const frozenSnapshot = "0123456789abcdef0123456789abcdef01234567"

// frozenTree 是 frozenSnapshot 所指向提交的树身份（base 内容的冻结摘要）。
const frozenTree = "0123456789abcdef0123456789abcdef01234568"

// frozenGitRepo 是冻结场景的 Git 应答夹具：单链提交仓（链首即 base），按命令
// 与参数精确区分身份——rev-parse HEAD 是当前提交，<rev>^{tree} 是该提交的树
// 身份，merge-base 返回链上较早实参，区间计数＝链上距离（base==HEAD 如实为
// 0）。候选冻结生成独立提交，目标 HEAD 只随原生 merge/commit/reset 推进。
// 未推进时应答与单提交仓一致。真实 git 行为（含排除
// 规则与 squash）由 internal/engine/vcs 的真仓冒烟覆盖。
type frozenGitRepo struct {
	commits []string
	head    string
	trees   map[string]string
	objects map[string]string
	// sequence 是身份发生的单调计数：squash 截断链后仍不复用已出现的提交
	// 身份（悬空提交与新提交是不同对象）。
	sequence int
	// pendingTree 是 reset --soft 后待重提交保留的树身份（squash 保持最终
	// 树不变）。
	pendingTree string
}

func newFrozenGitRepo() *frozenGitRepo {
	repo := &frozenGitRepo{trees: map[string]string{}, objects: map[string]string{frozenTree: "abcdef0123456789abcdef0123456789abcdef0123"}}
	repo.commits = append(repo.commits, frozenSnapshot)
	repo.head = frozenSnapshot
	repo.trees[frozenSnapshot] = frozenTree
	return repo
}

// advance 提交一次内容变化：新提交与其树身份唯一入链并成为当前 HEAD，返回
// 新提交身份。
func (r *frozenGitRepo) advance() string {
	r.sequence++
	commit := fmt.Sprintf("%040d", r.sequence)
	r.commits = append(r.commits, commit)
	r.head = commit
	r.trees[commit] = fmt.Sprintf("%064d", r.sequence)
	r.objects[r.trees[commit]] = fmt.Sprintf("%040x", r.sequence+1000000)
	return commit
}

func (r *frozenGitRepo) indexOf(rev string) (int, bool) {
	for i, commit := range r.commits {
		if strings.EqualFold(commit, rev) {
			return i, true
		}
	}
	return 0, false
}

func (r *frozenGitRepo) Run(dir, command string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	if command != "git" {
		return "", fmt.Errorf("frozen git fixture: unexpected command %s %s", command, joined)
	}
	head := r.head
	switch {
	case joined == "rev-parse --show-toplevel":
		return dir, nil
	case joined == "rev-parse HEAD":
		return head, nil
	case len(args) == 3 && args[0] == "rev-parse" && args[1] == "--verify" && strings.HasSuffix(args[2], "^{commit}"):
		rev := strings.TrimSuffix(args[2], "^{commit}")
		if _, ok := r.indexOf(rev); ok {
			return rev, nil
		}
		return "", fmt.Errorf("frozen git fixture: unknown commit %s", rev)
	case len(args) == 2 && args[0] == "rev-parse" && strings.HasSuffix(args[1], "^{tree}"):
		if tree, ok := r.trees[strings.ToLower(strings.TrimSuffix(args[1], "^{tree}"))]; ok {
			return tree, nil
		}
		return "", fmt.Errorf("frozen git fixture: unknown commit %s", args[1])
	case len(args) == 3 && args[0] == "merge-base":
		a, aok := r.indexOf(args[1])
		b, bok := r.indexOf(args[2])
		if !aok || !bok {
			return "", fmt.Errorf("frozen git fixture: unknown commit in %s", joined)
		}
		if a <= b {
			return r.commits[a], nil
		}
		return r.commits[b], nil
	case joined == "status --porcelain=v1 --untracked-files=all":
		if r.pendingTree != "" && r.pendingTree != r.trees[head] {
			return "M  app.go", nil
		}
		return "", nil
	case joined == "status --porcelain=v1 --branch --untracked-files=all":
		if r.pendingTree != "" && r.pendingTree != r.trees[head] {
			return "## HEAD (no branch)\nM  app.go", nil
		}
		return "## HEAD (no branch)", nil
	case len(args) == 8 && strings.Join(args[:5], " ") == "diff --raw --no-abbrev --no-renames -z" && args[7] == "--":
		object := func(identity string) (string, bool) {
			if tree, ok := r.trees[identity]; ok {
				identity = tree
			}
			entry, ok := r.objects[identity]
			return entry, ok
		}
		base, baseExists := object(args[5])
		next, nextExists := object(args[6])
		if !baseExists || !nextExists {
			return "", fmt.Errorf("frozen git fixture: unknown diff endpoint %v", args)
		}
		if base == next {
			return "", nil
		}
		return ":100644 100644 " + base + " " + next + " M\x00app.go\x00", nil
	case joined == "write-tree":
		// 暂存区树：reset --soft 后 index 保留截链前内容（pendingTree）；干净
		// index 的暂存树＝HEAD 提交的树。
		if r.pendingTree != "" {
			return r.pendingTree, nil
		}
		return r.trees[head], nil
	case len(args) == 3 && args[0] == "merge" && args[1] == "--ff-only":
		if _, ok := r.indexOf(args[2]); !ok {
			return "", fmt.Errorf("frozen git fixture: unknown merge source %s", args[2])
		}
		r.head = args[2]
		return "", nil
	case len(args) == 3 && args[0] == "reset" && args[1] == "--soft":
		// squash 第一步：区间压回 base，当前树留给重提交。
		bi, ok := r.indexOf(args[2])
		if !ok {
			return "", fmt.Errorf("frozen git fixture: unknown commit %s", args[2])
		}
		r.pendingTree = r.trees[r.head]
		r.head = args[2]
		r.commits = r.commits[:bi+1]
		return "", nil
	case len(args) == 3 && args[0] == "commit" && args[1] == "-m":
		// squash 第二步：以保留树重新提交——内容不变、身份更新。
		r.sequence++
		commit := fmt.Sprintf("%040d", r.sequence)
		tree := r.pendingTree
		if tree == "" {
			tree = fmt.Sprintf("%064d", r.sequence)
			r.objects[tree] = fmt.Sprintf("%040x", r.sequence+1000000)
		}
		r.pendingTree = ""
		r.commits = append(r.commits, commit)
		r.trees[commit] = tree
		r.head = commit
		return "", nil
	case len(args) == 3 && args[0] == "rev-list" && args[1] == "--count":
		base, tip, ok := strings.Cut(args[2], "..")
		if !ok {
			return "", fmt.Errorf("frozen git fixture: unexpected range %s", args[2])
		}
		bi, bok := r.indexOf(base)
		hi, hok := r.indexOf(tip)
		if !bok || !hok || hi < bi {
			return "", fmt.Errorf("frozen git fixture: not an ancestor range %s", args[2])
		}
		return strconv.Itoa(hi - bi), nil
	}
	return "", fmt.Errorf("frozen git fixture: unexpected command git %s", joined)
}

type validationDriver struct {
	mockNative               bool
	fixtureTool              string
	fixtureEnvironment       string
	fixtureQAResults         map[string]protocol.CheckEvidence
	fixtureCandidateObserver func(protocol.HostActionIntent, protocol.CandidateParameters) protocol.Event
	t                        *testing.T
	f                        *Facade
	run                      Run
	seq                      int
	revision                 string
	// provider 是本 driver 的宿主 provider 身份（事件/回执按它提交）；缺省
	// DefaultProvider，可计量 provider（transcript 计量链）按需覆写。
	provider string
	// git 是本 driver 场景固定的 Git provider（root＋注入 runner）：候选回执
	// 的身份/树摘要取自它——reopen 后的 façade 持生产 exec runner，测试替身
	// 不随重开丢失。frozen 非空时 materialize 冻结候选提交，保持目标 HEAD。
	git    *vcs.Provider
	frozen *frozenGitRepo
	// routeRedos 记录本 driver 已走过的路线重走次数：每次重走需要一个
	// 全新的需求文档 revision（账本要求严格单调），递增换行数产生唯一
	// revision。
	routeRedos int
}

// host 返回本 driver 使用的宿主 provider 身份。
func (d *validationDriver) host() string {
	if d.provider != "" {
		return d.provider
	}
	return d.f.admission.Provider
}

func (d *validationDriver) eventID() protocol.EventID {
	d.seq++
	return protocol.EventID(fmt.Sprintf("event-%d", d.seq))
}
func (d *validationDriver) submit(e protocol.Event) protocol.Acceptance {
	d.t.Helper()
	if e.ID == "" {
		e.ID = d.eventID()
	}
	a, r, err := d.f.Submit(e)
	if err != nil {
		d.t.Fatalf("submit %s: %v", e.Kind, err)
	}
	d.run = r
	return a
}
func (d *validationDriver) drive() {
	d.t.Helper()
	for {
		var r Run
		var err error
		if d.mockNative {
			snapshot, loadErr := d.f.engine.Load()
			if loadErr != nil {
				d.t.Fatal(loadErr)
			}
			if err := d.f.recordIntakeReceipt(snapshot.State); err != nil {
				d.t.Fatal(err)
			}
			_, _, err = d.f.engine.Drive(protocol.Observation{})
			if err == nil {
				r, err = d.f.project()
			}
		} else {
			r, err = d.f.Drive()
		}
		if err != nil {
			d.t.Fatal(err)
		}
		d.run = r
		if d.mockNative {
			if d.observeFixtureAdapter() {
				continue
			}
			return
		}
		waiting := false
		if r.Next.Kind == decision.KindHostAction && r.Next.HostAction != nil {
			for _, intent := range r.AdapterIntents {
				if intent.Adapter != nil && intent.Adapter.Operation == "op.check" && slices.Contains(r.Next.HostAction.Steps, intent.Step) {
					waiting = true
					break
				}
			}
		}
		if !waiting {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (d *validationDriver) receipt(id string) {
	d.t.Helper()
	e, err := protocol.NewSpawnReceiptEvent(d.eventID(), id, d.host(), id, protocol.SpawnStatusSpawned)
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(e)
}

// Resume receipts cite the signed call's start and original execution provider.
func (d *validationDriver) resumeReceipt(intent protocol.HostActionIntent) protocol.Event {
	d.t.Helper()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}
	if intent.Resume == nil {
		d.t.Fatal("resume receipt requires its original signed target")
	}
	choice, ok := snapshot.State.ActionExecutionChoice(intent.Resume.WorkerActionID)
	if !ok {
		d.t.Fatal("resume receipt has no original execution owner")
	}
	started, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), choice.Provider, intent.ActionID, intent.Resume.Identity, protocol.LifecycleStart)
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(started)
	receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), intent.ActionID, intent.Operation, choice.Provider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Resume.Identity, Event: protocol.LifecycleStart})
	if err != nil {
		d.t.Fatal(err)
	}
	return receipt
}

// Termination evidence uses each signed intent's original worker identity.
func (d *validationDriver) completeWorkerTerminations() {
	d.t.Helper()
	for {
		var id string
		var intent protocol.HostActionIntent
		for action, candidate := range d.run.AdapterIntents {
			if candidate.Terminate != nil {
				id, intent = action, candidate
				break
			}
		}
		if id == "" {
			return
		}
		for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
			event, err := protocol.NewLifecycleEventEvent(protocol.EventID("lifecycle:"+id+":"+edge), d.host(), intent.Terminate.Identity, edge)
			if err != nil {
				d.t.Fatal(err)
			}
			d.submit(event)
		}
		correlation := intent.Correlation
		if correlation == "" {
			correlation = intent.ActionID
		}
		event, err := protocol.NewAgentHostActionReceiptEvent(protocol.EventID("termination-receipt:"+id), id, intent.Operation, d.host(), correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Terminate.Identity, Event: protocol.LifecycleStop})
		if err != nil {
			d.t.Fatal(err)
		}
		d.submit(event)
	}
}
func (d *validationDriver) business(id string, p protocol.ValidationResult) {
	d.t.Helper()

	d.receipt(id)
	worker, issued := driverState(d.t, d).WorkerInput(id, d.f.definition)
	if !issued {
		d.t.Fatalf("positive business fixture has no signed worker input: %s", id)
	}
	if p.Status == "PASS" || p.Status == "FAIL" {
		if p.GranularityReview != nil {
			fixturePlanRequirements(p.GranularityReview, worker.RequirementPoints)
		}
		if p.Development != nil && p.Development.Completion == nil && p.GranularityReview == nil && worker.Validation != nil && worker.Validation.Repair != nil && p.Development.RequirementResults == nil {
			p.Development.RequirementResults = fixtureImplementedPoints(worker.Validation.Repair.RequiredPoints, p.Development.Summary)
		}
		if p.Development != nil && p.Development.Completion != nil && p.Development.Completion.RequirementResults == nil {
			p.Development.Completion.RequirementResults = fixtureImplementedPoints(worker.RequirementPoints, p.Development.Summary)
		}
		if worker.Validation != nil && worker.Validation.Gate != nil && p.RequirementResults == nil {
			p.RequirementResults = fixtureGatePointDecisions(worker.RequirementPoints, worker.Validation.Gate.Candidate.Identity, p.Findings)
		}
	}
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: string(d.eventID()), Validation: &p}})
	if d.mockNative && p.Development != nil {
		d.drive()
	}
}
func (d *validationDriver) control(kind protocol.ControlKind, choice string, p protocol.ReviewDecisionPayload) {
	d.t.Helper()
	if p.Topology != nil && p.Topology.Decision == "split" && p.Topology.DevelopmentPlan == nil {
		preview, err := d.f.DevelopmentPlan(protocol.RouteDecision{}, *p.Topology)
		if err != nil {
			d.t.Fatal(err)
		}
		p.Topology.DevelopmentPlan = &preview.Confirmation
	}
	id := d.eventID()
	var e protocol.Event
	var err error
	if kind == protocol.ControlSelectRoute && p.Route != nil {
		e, err = protocol.NewRouteRequestEvent(id, p.Route.Gates, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
		if err == nil {
			e.Request.Configuration = p.Route.Configuration
		}
	} else {
		e, err = protocol.NewRequestEvent(id, kind, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
	}
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(e)
	var token string
	for _, a := range d.run.PendingAsks {
		if a.RequestID == string(id) {
			token = a.Token
		}
	}
	e, err = protocol.NewReviewDecideEvent(d.eventID(), protocol.RequestID(id), token, protocol.AskOptionID(choice), p)
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(e)
}
func startValidationDriver(t *testing.T, inventory *protocol.CoverageInventory) *validationDriver {
	return startValidationDriverAt(t, t.TempDir(), DefaultProvider, inventory)
}
func startValidationDriverAt(t *testing.T, root, provider string, inventory *protocol.CoverageInventory) *validationDriver {
	repo := newFrozenGitRepo()
	d := startValidationDriverAtVCS(t, root, provider, repo, frozenSnapshot, inventory)
	d.frozen = repo
	return d
}

// startValidationDriverAtVCS 以注入的 Git runner 与显式 base 身份建立 regular
// 驱动；真仓链路传 nil runner（生产 exec），base 由调用方从仓库解析。
func startValidationDriverAtVCS(t *testing.T, root, provider string, runner vcs.Runner, base string, inventory *protocol.CoverageInventory, solutionPaths ...string) *validationDriver {
	t.Helper()
	sum := sha256.Sum256([]byte(validationDocument))
	// 修订语言＝裸 hex：与 intake 工件绑定校验和 QA 工作区登记的重算同源。
	revision := fmt.Sprintf("%x", sum)
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
		t.Fatal(err)
	}
	solution := ""
	if len(solutionPaths) > 0 {
		solution = solutionPaths[0]
	}
	preview, err := PrepareIntake(root, "requirements.md", solution, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	receipt := preview.IntakeCandidate
	receipt.Coverage = inventory
	_, mocked := runner.(*frozenGitRepo)
	var f *Facade
	var r Run
	if mocked {
		f, r, err = abstractValidationFacade(t, root, provider, runner, base, receipt)
	} else {
		f, r, err = Start(StartOptions{Root: root, ArtifactRoot: root, Capacity: 16, VCSRunner: runner, Admission: testAdmission(t, root, "candidate", "sha256:package", provider), Request: StartRequest{RunID: "validation", Route: "regular", Provider: provider, BaseSnapshot: base, IntakeConfirmationReceipt: receipt}})
	}
	if err != nil {
		t.Fatal(err)
	}
	d := &validationDriver{t: t, f: f, run: r, revision: revision, provider: provider, git: vcs.New(root, runner), mockNative: mocked}
	d.drive()
	if d.mockNative {
		d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: validationDocument}}})
	}
	return d
}
func newValidationDriver(t *testing.T, gates ...string) *validationDriver {
	return configuredValidationDriver(t, nil, gates...)
}
func configuredValidationDriver(t *testing.T, inventory *protocol.CoverageInventory, gates ...string) *validationDriver {
	return plannedValidationDriver(t, inventory, commandPlan(), gates...)
}

// routeDecisionForGates pins the candidate catalogue and the requested obligations.
func routeDecisionForGates(gates []string) protocol.RouteDecision {
	candidates := []string{"blackbox", "whitebox"}
	for _, gate := range gates {
		if !slices.Contains(candidates, gate) {
			candidates = append(candidates, gate)
		}
	}
	return protocol.RouteDecision{Gates: candidates, Selected: gates}
}
func plannedValidationDriver(t *testing.T, inventory *protocol.CoverageInventory, plan *protocol.GranularityReview, gates ...string) *validationDriver {
	return plannedValidationDriverAt(t, t.TempDir(), DefaultProvider, inventory, plan, gates...)
}
func plannedValidationDriverAt(t *testing.T, root, provider string, inventory *protocol.CoverageInventory, plan *protocol.GranularityReview, gates ...string) *validationDriver {
	repo := newFrozenGitRepo()
	d := plannedValidationDriverAtVCS(t, root, provider, repo, frozenSnapshot, inventory, plan, gates...)
	d.frozen = repo
	return d
}

// passPreDevelopmentReviews 完成两条开发前审查系列的 PASS 轮（产品审→
// start-readiness，后者携结构化粒度计划），驱动至拓扑确认起点。
func passPreDevelopmentReviews(t *testing.T, d *validationDriver, plan *protocol.GranularityReview, actions ...review.Action) {
	t.Helper()
	revision := d.revision
	if len(actions) == 0 {
		actions = []review.Action{review.ProductReview, review.StartReadiness}
	}
	for _, action := range actions {
		id := ""
		for key, pending := range d.run.PendingActions {
			if pending.ReviewSeries == action {
				id = key
			}
		}
		if id == "" {
			t.Fatalf("missing %s dispatch: %+v", action, d.run)
		}
		if d.host() != DefaultProvider {
			// 可计量 provider：审查派发补零用量 transcript 与 sidecar 生命周期
			// 事件，结果完成链走 bridge READY。
			d.meteredSpawn(d.f.root, id)
		} else {
			d.receipt(id)
		}
		e, err := protocol.NewReviewWorkerResultEvent(d.eventID(), id, d.host(), string(d.eventID()), protocol.ReviewResultEvidence{GranularityReview: func() *protocol.GranularityReview {
			if action == review.StartReadiness {
				worker, issued := driverState(t, d).WorkerInput(id, d.f.definition)
				if !issued {
					t.Fatal("readiness fixture lacks its issued input")
				}
				fixturePlanRequirements(plan, worker.RequirementPoints)
				return plan
			}
			return nil
		}(), Action: action, DispatchID: id, RequirementRevision: revision, Status: "PASS", Findings: []review.Finding{}})
		if err != nil {
			t.Fatal(err)
		}
		d.submit(e)
		verifyReviewFixture(t, d, e)
		d.drive()
	}
}

// plannedValidationDriverAtVCS 以注入的 Git runner 建立已过审查/拓扑/路线/需
// 求确认的驱动（runner nil＝生产 exec，用于真仓链路）。
func plannedValidationDriverAtVCS(t *testing.T, root, provider string, runner vcs.Runner, base string, inventory *protocol.CoverageInventory, plan *protocol.GranularityReview, gates ...string) *validationDriver {
	t.Helper()
	d := startValidationDriverAtVCS(t, root, provider, runner, base, inventory)
	passPreDevelopmentReviews(t, d, plan)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
	d.confirmRoute(routeDecisionForGates(gates))
	d.drive()
	return d
}

// meteredSpawn 以可计量 provider 完成一次派发的宿主侧观察：SPAWNED 回执、
// 零用量 transcript 与 sidecar start/stop 事件（派发计量来源解析 READY）。
func (d *validationDriver) meteredSpawn(root, id string) {
	d.t.Helper()
	transcript := filepath.Join(root, ".gates", "tmp", d.f.runID, "metering", digestOf(id)+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0700); err != nil {
		d.t.Fatal(err)
	}
	writeTranscript(d.t, transcript, []string{`{"type":"assistant","uuid":"metered-zero","message":{"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}}`})
	e, err := protocol.NewSpawnReceiptEvent(d.eventID(), id, d.host(), id, protocol.SpawnStatusSpawned)
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(e)
	completeMeteredSource(d.t, root, d.host(), id, transcript)

}

func (d *validationDriver) action(handler string) (string, protocol.ValidationInput) {
	d.t.Helper()
	for i := 0; i < 2; i++ {
		for id, input := range d.run.ValidationInputs {
			if input.Handler == handler {
				return id, input
			}
		}
		if i == 0 {
			d.drive()
		}
	}
	d.t.Fatalf("missing %s; inputs=%+v, next=%+v, validation=%+v", handler, d.run.ValidationInputs, d.run.Next, d.run.Validation)
	return "", protocol.ValidationInput{}
}

func (d *validationDriver) materialize() {
	d.t.Helper()
	d.drive()
	if !d.mockNative {
		return
	}
	snapshot := driverState(d.t, d)
	ids := make([]string, 0, len(d.run.AdapterIntents))
	for id := range d.run.AdapterIntents {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Compare(d.run.AdapterIntents[a].Revision, d.run.AdapterIntents[b].Revision)
	})
	for _, id := range ids {
		intent := d.run.AdapterIntents[id]
		if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
			continue
		}
		if !snapshot.NativeActionWritable(intent, d.f.definition) {
			continue
		}
		data, _ := json.Marshal(intent.Adapter.Params)
		var p protocol.CandidateParameters
		if err := json.Unmarshal(data, &p); err != nil {
			d.t.Fatal(err)
		}
		if p.Stage == "PROMOTION" {
			continue
		}
		d.fixtureCandidate(id, intent, p)
		d.drive()
		return
	}
	d.t.Fatalf("missing candidate intent: %+v", d.run)
}
func simpleManifest(input protocol.ValidationInput) coverage.AcceptanceManifest {
	m := input.Manifest
	m.Cases = []coverage.AcceptanceCase{{Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"./app", "run", "valid"}}}, CaseID: "normal", Mode: coverage.CaseBlackbox, Scenario: "SUCCESS", PublicEntry: "app run", Preconditions: "clean project", Steps: "run with valid input", Oracle: "exits zero"}, {Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"./app", "run", "invalid"}}}, CaseID: "invalid", Mode: coverage.CaseBlackbox, Scenario: "FAILURE", PublicEntry: "app run", Preconditions: "clean project", Steps: "run with invalid input", Oracle: "rejects invalid input"}}
	for _, c := range m.Cases {
		m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
	}
	return m
}
func approvedReview(m coverage.AcceptanceManifest) coverage.ReviewResult {
	r := coverage.ReviewResult{Binding: m.Binding, Scope: coverage.ScopeFull, SetStatus: coverage.StatusPass}
	for _, s := range m.Sources {
		r.SourceDecisions = append(r.SourceDecisions, coverage.ReviewDecision{ID: s, Status: coverage.StatusPass})
	}
	for _, p := range m.Points {
		r.PointDecisions = append(r.PointDecisions, coverage.ReviewDecision{ID: p.PointID, Status: coverage.StatusPass})
	}
	for _, c := range m.Cases {
		r.CaseDecisions = append(r.CaseDecisions, coverage.ReviewDecision{ID: c.CaseID, Status: coverage.StatusPass, Reason: fmt.Sprintf("Public entry and parameters, setup order and environment support %s; procedure %v records its evidence; oracle %v directly verifies the declared case scope.", c.CaseID, c.Steps, c.Oracle)})
	}
	return r
}
func (d *validationDriver) reviewReady() {
	d.t.Helper()
	_, input := d.action("engine.qa.design.v6")
	d.reviewReadyWithManifest(simpleManifest(input))
}
func (d *validationDriver) reviewReadyWithManifest(m coverage.AcceptanceManifest) {
	d.t.Helper()
	dev, di := d.action("engine.development.batch.v6")
	design, qi := d.action("engine.qa.design.v6")
	if len(d.run.PendingActions) != 2 {
		d.t.Fatalf("development/design must be concurrent: %+v", d.run.PendingActions)
	}
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	reviewID, ri := d.action("engine.qa.review.rationale.v2")
	r := approvedReview(m)
	d.business(reviewID, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &r})
	d.drive()
	if _, ok := d.run.ValidationInputs[dev]; !ok {
		d.t.Fatal("QA review disturbed development")
	}
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented input checks"}})
	d.drive()
	d.materialize()
	d.materialize()
}
func executionFor(input protocol.ValidationInput) coverage.ExecutionReport {
	r := coverage.ExecutionReport{Binding: *input.Execution}
	settled := append(slices.Clone(input.Completed), input.Inherited...)
	for _, edge := range input.Whitelist.Entries {
		inherited := slices.IndexFunc(settled, func(row coverage.ExecutionRecord) bool {
			return row.ReviewKind == edge.ReviewKind && row.SourceID == edge.SourceID && row.PointID == edge.PointID && row.CaseID == edge.CaseID
		})
		if inherited >= 0 {
			r.Records = append(r.Records, settled[inherited])
		} else {
			index := slices.IndexFunc(input.ExecutionFacts, func(f protocol.ExecutionFact) bool { return f.CaseID == edge.CaseID })
			if index < 0 {
				panic("execution fixture has no native facts for " + edge.CaseID)
			}
			procedure, observation, err := input.ExecutionFacts[index].Render()
			if err != nil {
				panic(err)
			}
			r.Records = append(r.Records, coverage.ExecutionRecord{ReviewKind: edge.ReviewKind, SourceID: edge.SourceID, PointID: edge.PointID, CaseID: edge.CaseID, Result: coverage.ExecutedPass, Provenance: coverage.ProvenanceExecuted, Procedure: procedure, Observation: observation, Comparison: "exit status and output match the specified oracle"})
		}
		if !slices.Contains(r.Binding.ActualCaseIDs, edge.CaseID) {
			r.Binding.ActualCaseIDs = append(r.Binding.ActualCaseIDs, edge.CaseID)
		}
	}
	return r
}

// Execution metadata is projected from the same original receipts as the rows.
func nativeExecutionResult(input protocol.ValidationInput, result protocol.ValidationResult) protocol.ValidationResult {
	result.Tool, result.Environment = protocol.ExecutionIdentities(input.ExecutionFacts)
	return result
}

// Canonical receipt admission is exercised through Facade.Submit. The driver
// supplies typed native facts; physical Facade.Drive is covered by the native
// public-workflow checks.
func TestValidationPipelineAdmitsCanonicalReceiptFacts(t *testing.T) {
	t.Parallel()
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	qa, qi := d.action("engine.qa.execution.v4")
	gate, gi := d.action("engine.gate.v3")
	if len(d.run.PendingActions) != 2 {
		t.Fatal("QA and gates were not issued together")
	}
	report := executionFor(qi)
	tool, environment := protocol.ExecutionIdentities(qi.ExecutionFacts)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
	d.drive()
	if d.run.Validation.Waves[0].Status != "COLLECTING" {
		t.Fatal("wave closed before gate arrived")
	}
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	d.drive()
	w := d.run.Validation.Waves[0]
	if w.Status != "COMPLETE" || d.run.Status == "COMPLETE" || len(d.run.AdapterIntents) != 0 {
		t.Fatalf("expected validated candidate waiting for promotion: %+v", d.run)
	}
	if w.Candidate != *d.run.Validation.Candidate || w.ExpectedDigest == "" {
		t.Fatal("final candidate binding missing")
	}
}

func TestMissingReviewClosesFiniteAttemptsAndAllowsOneBoundExtra(t *testing.T) {
	t.Parallel()
	d := startValidationDriver(t, nil)
	var task protocol.PendingAction
	for i := 0; i < 3; i++ {
		if len(d.run.PendingActions) != 1 {
			t.Fatalf("expected one execution: %+v", d.run.PendingActions)
		}
		var id string
		for key, p := range d.run.PendingActions {
			id = key
			if i == 0 {
				task = p
			} else if p.Task != task.Task {
				t.Fatal("invalid result changed the logical review duty")
			}
		}
		d.receipt(id)
		a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: fmt.Sprintf("missing-%d", i)}})
		if a.Status != "REJECTED_INVALID_RESULT" || d.run.RejectedResults[id].Reason == "" {
			t.Fatalf("missing evidence accepted: %+v", a)
		}
		d.drive()
		if i == 1 {
			var ask PendingAskFace
			for _, p := range d.run.PendingAsks {
				if p.Validation != nil && p.Validation.Kind == "RETRY" {
					ask = p
				}
			}
			if ask.Validation == nil || len(d.run.PendingActions) != 0 {
				t.Fatalf("retry did not stop: %+v", d.run)
			}
			payload := &protocol.ValidationDecision{Reason: "one more execution with corrected payload", Digest: ask.Validation.Digest, StateRevision: d.run.Revision}
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "approve", Validation: payload}})
			d.drive()
		}
	}
	if len(d.run.PendingActions) != 0 {
		t.Fatal("extra attempt expanded after exhaustion")
	}
	ask := d.validationAsk("RETRY")
	for _, option := range ask.Options {
		if option.ID == "approve" {
			t.Fatal("extra attempt authorization expanded after exhaustion")
		}
	}
	for _, a := range d.run.EndedAttempts {
		if a.Attempts != 3 || a.MaxAttempts != 3 || !a.RetryExhausted {
			t.Fatalf("wrong retry state %+v", a)
		}
	}
}

func TestConfirmedSolutionCoverageAndNonQAEvidenceReachWhitelist(t *testing.T) {
	t.Parallel()
	inventory := &protocol.CoverageInventory{
		Sources:      []coverage.RequiredSource{{SourceID: "SOL-001", Category: coverage.SolutionConstraint, Applicability: coverage.QAApplicability}, {SourceID: "SOL-002", Category: coverage.SolutionConstraint, Applicability: coverage.NonQAApplicability}},
		Points:       []coverage.AcceptancePoint{{PointID: "SP-001", ObservableBehavior: "operation records its result", Oracle: "record exists after success"}},
		Links:        []protocol.SourcePoint{{SourceID: "SOL-001", PointID: "AC-001"}, {SourceID: "SOL-001", PointID: "SP-001"}},
		Alternatives: []coverage.AlternativeVerification{{SourceID: "SOL-002", Reason: "release policy is document verification", Method: "inspect release policy", Status: coverage.StatusPass, Evidence: "policy contains required supported platform statement"}},
	}
	d := configuredValidationDriver(t, inventory, "blackbox", "quality")
	design, qi := d.action("engine.qa.design.v6")
	dev, di := d.action("engine.development.batch.v6")
	m := simpleManifest(qi)
	m.Edges = nil
	for _, link := range qi.SourcePoints {
		for _, c := range m.Cases {
			m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: link.SourceID, PointID: link.PointID, CaseID: c.CaseID})
		}
	}
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	id, ri := d.action("engine.qa.review.rationale.v2")
	r := approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &r})
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented records"}})
	d.materialize()
	d.materialize()
	_, input := d.action("engine.qa.execution.v4")
	if len(input.Contract.RequiredSources.Sources) != 2 || len(input.Whitelist.Entries) != 6 {
		t.Fatalf("source/point edges lost: %+v", input)
	}
	if got := d.run.IntakeConfirmationReceipt.Coverage; got == nil || got.Alternatives[0].Evidence != inventory.Alternatives[0].Evidence {
		t.Fatal("confirmed non-QA evidence lost")
	}
}

func (d *validationDriver) carryFormattingChange() string {
	d.t.Helper()
	confirmValidationDocument(d, "", nil, validationDocument+"\n")
	d.revision = driverState(d.t, d).RequirementRevision()
	d.drive()
	return d.revision
}

func TestConfirmedCarryKeepsIssuedDevelopmentAndDesign(t *testing.T) {
	t.Parallel()
	d := newValidationDriver(t, "blackbox", "quality")
	old := map[string]protocol.PendingAction{}
	for id, a := range d.run.PendingActions {
		old[id] = a
	}
	revision := d.carryFormattingChange()
	for _, input := range d.run.ValidationInputs {
		assertConfirmedMaterial(t, d, input, validationDocument+"\n")
		if input.Binding.RequirementRevision == revision {
			t.Fatal("carried work replaced its original issued identity")
		}
	}
	if len(d.run.PendingActions) != 2 {
		t.Fatalf("carry duplicated obligations: %+v", d.run.PendingActions)
	}
	for id, a := range old {
		if d.run.PendingActions[id] != a {
			t.Fatal("carry replaced issued identity")
		}
	}
	// Carried source bindings are projected after verifying the original result's dispatch.
	id, input := d.action("engine.qa.design.v6")
	m := simpleManifest(input)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &m})
	if d.run.Validation.Sets[coverage.ReviewBlackbox].Manifest.Binding.RequirementRevision != revision {
		t.Fatal("carried design did not bind the confirmed revision")
	}
}

func TestDualQACandidatePreservesReviewedTestBytesAndDeletion(t *testing.T) {
	t.Parallel()
	d := candidateSourceDriver(t, "blackbox", "whitebox", "quality")
	request, err := d.f.request()
	if err != nil {
		t.Fatal(err)
	}
	base := request.BaseSnapshot
	id, input := d.action("engine.qa.design.v6")
	m := simpleManifest(input)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	id, input = d.action("engine.qa.review.rationale.v2")
	r := approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &r})
	id, input = d.action("engine.development.batch.v6")
	trees := map[string]map[string]string{base: candidateSourceTree(t, d, base)}
	observe := func(intent protocol.HostActionIntent, p protocol.CandidateParameters) protocol.Event {
		t.Helper()
		if trees[p.Base] == nil {
			trees[p.Base] = candidateSourceTree(t, d, p.Base)
		}
		tree := map[string]string{}
		for path, content := range trees[p.Base] {
			tree[path] = content
		}
		stage := p.Stage
		if stage == "CHECK_SOURCE" {
			stage = "QA_ARTIFACT"
			if slices.Contains(p.Paths, "app.go") {
				stage = "DEVELOPMENT"
			}
		}
		switch stage {
		case "DEVELOPMENT":
			tree["app.go"] = "validated production bytes"
			tree["old_test.go"] = "obsolete test"
		case "QA_ARTIFACT":
			tree["app_test.go"] = "reviewed assertion bytes"
			delete(tree, "old_test.go")
		}
		for mode, a := range p.QAArtifacts {
			if a.Identity != d.run.Validation.Sets[mode].Candidate {
				t.Fatal("assembly used an unreviewed artifact")
			}
			for _, path := range a.Paths {
				if content, ok := trees[a.Identity][path]; ok {
					tree[path] = content
				} else {
					delete(tree, path)
				}
			}
		}
		identity, digest := freezeCandidateSource(t, d, p, tree)
		trees[identity] = tree
		e := protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, Identity: identity, Digest: digest, WorkspaceID: p.WorkspaceID, Paths: p.Paths, TestOnly: true, ProductionViewEqual: true}
		e.GateView = gateReviewView(p)
		data, _ := json.Marshal(e)
		values := map[string]any{}
		_ = json.Unmarshal(data, &values)
		event, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", d.host(), intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", values)
		if err != nil {
			t.Fatal(err)
		}
		return event
	}
	d.fixtureCandidateObserver = observe
	materialize := func() (string, protocol.CandidateParameters) {
		t.Helper()
		d.drive()
		if len(d.run.AdapterIntents) != 1 {
			t.Fatalf("expected one typed candidate intent: %+v; next=%+v rejected=%+v sets=%+v", d.run.AdapterIntents, d.run.Next, d.run.RejectedResults, d.run.Validation.Sets)
		}
		var intent protocol.HostActionIntent
		for _, i := range d.run.AdapterIntents {
			intent = i
		}
		data, _ := json.Marshal(intent.Adapter.Params)
		var p protocol.CandidateParameters
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatal(err)
		}
		event := observe(intent, p)
		identity := event.HostAction.AdapterEvidence.Values["identity"].(string)
		d.submit(event)
		d.drive()
		return identity, p
	}
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go", "old_test.go"}, Summary: "production and existing tests"}})
	materialize()
	materialize()
	id, input = d.action("engine.qa.design.v6")
	m = input.Manifest
	for _, row := range []struct{ id, scenario, test string }{{"white-success", "SUCCESS", "TestSuccess"}, {"white-misuse", "MISUSE", "TestMisuse"}} {
		m.Cases = append(m.Cases, coverage.AcceptanceCase{CaseID: row.id, Mode: coverage.CaseWhitebox, Scenario: row.scenario, TestRef: "app_test.go::" + row.test, Oracle: "specified input outcome"})
		m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: row.id})
	}
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go", "old_test.go"}}})
	materialize()
	id, input = d.action("engine.qa.review.rationale.v2")
	r = approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &r})
	final, p := materialize()
	if p.Stage != "REVIEW_READY" || trees[final]["app.go"] != "validated production bytes" || trees[final]["app_test.go"] != "reviewed assertion bytes" {
		t.Fatalf("final artifact contents: %+v", trees[final])
	}
	if _, exists := trees[final]["old_test.go"]; exists {
		t.Fatal("reviewed deletion disappeared during assembly")
	}
	_, gate := d.gateAction("quality")
	if p.ReviewBase != base || p.Base == p.ReviewBase || gate.Gate == nil || gate.Gate.Gate != "quality" || gate.Gate.BaseSnapshot != p.ReviewBase || gate.Gate.Candidate != d.run.Validation.Waves[0].Candidate || !reflect.DeepEqual(gate.Gate.TestOwnedPaths, []string{"app_test.go", "old_test.go"}) {
		t.Fatalf("gate range lost native base, candidate, or explicit artifact ownership: %+v", gate.Gate)
	}
	if len(gate.Manifest.Cases) != 0 || len(gate.Completed) != 0 || gate.Whitelist != nil {
		t.Fatal("gate received QA case content")
	}
	if len(d.run.Validation.Waves) != 1 || len(d.run.Validation.Waves[0].Targets) != 3 {
		t.Fatalf("shared AC did not reach dual-QA validation: %+v", d.run.Validation.Waves)
	}
}

func TestConfirmedCarryAcceptsOriginalReviewAndWaveEvidence(t *testing.T) {
	t.Parallel()
	for _, handler := range []string{"engine.qa.review.rationale.v2", "engine.qa.execution.v4", "engine.gate.v3"} {
		t.Run(handler, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			var action string
			var input protocol.ValidationInput
			var result protocol.ValidationResult
			if handler == "engine.qa.review.rationale.v2" {
				design, designed := d.action("engine.qa.design.v6")
				manifest := simpleManifest(designed)
				d.business(design, protocol.ValidationResult{Binding: designed.Binding, Status: "PASS", Manifest: &manifest})
				d.drive()
				action, input = d.action(handler)
				reviewed := approvedReview(manifest)
				result = protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &reviewed}
			} else {
				d.reviewReady()
				action, input = d.action(handler)
				result = protocol.ValidationResult{Binding: input.Binding, Status: "PASS"}
				if handler == "engine.gate.v3" {
					worker, issued := driverState(t, d).WorkerInput(action, d.f.definition)
					if !issued {
						t.Fatal("original gate has no signed requirement points")
					}
					result.RequirementResults = fixtureGatePointDecisions(worker.RequirementPoints, input.Gate.Candidate.Identity, nil)
				}
				if handler == "engine.qa.execution.v4" {
					report := executionFor(input)
					result.Execution, result.Tool, result.Environment = &report, "app-v1", "isolated-project"
				} else {
					qa, qi := d.action("engine.qa.execution.v4")
					report := executionFor(qi)
					d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
				}
			}
			oldAction := d.run.PendingActions[action]
			before := driverState(t, d)
			revision := d.carryFormattingChange()
			if d.run.PendingActions[action] != oldAction {
				t.Fatal("carry replaced the signed execution")
			}
			current := d.run.ValidationInputs[action]
			if current.Binding != input.Binding {
				t.Fatal("carry rewrote the issued work binding")
			}
			if handler == "engine.qa.execution.v4" {
				before, _ := json.Marshal(input.Execution)
				after, _ := json.Marshal(current.Execution)
				if string(before) != string(after) {
					t.Fatal("carry changed the frozen execution contract")
				}
			}
			d.receipt(action)
			accepted := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: action, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "carried-result", Validation: &result}})
			if accepted.Status != "ACCEPTED" || len(d.run.RejectedResults) != 0 {
				t.Fatalf("carried result rejected: %+v %+v", accepted, d.run.RejectedResults)
			}
			if handler == "engine.qa.review.rationale.v2" {
				set := d.run.Validation.Sets[coverage.ReviewBlackbox]
				if set.Status != "FROZEN" || set.Review.Binding.RequirementRevision != revision {
					t.Fatal("carried review did not fulfill current obligation")
				}
				if len(set.ReviewHistory) != 1 || set.ReviewHistory[0].Review.Binding != result.Review.Binding {
					t.Fatal("raw review history lost the original binding")
				}
			} else if handler == "engine.gate.v3" {
				current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				state := driverState(t, d)
				if current.Status != "COMPLETE" || current.Scope != coverage.ScopeFull || current.RequirementRevision != revision || current.Generation != state.Validation.Generation || current.Candidate != before.Validation.Waves[0].Candidate || d.run.Validation.Waves[0].ExpectedDigest != before.Validation.Waves[0].ExpectedDigest || !reflect.DeepEqual(state.Results[action].Validation, &result) {
					t.Fatal("carried gate did not fulfill current FULL qualification with original evidence")
				}
				for id, original := range before.Results {
					if !reflect.DeepEqual(original, state.Results[id]) {
						t.Fatal("carry changed a previously accepted raw result")
					}
				}
			}
			for _, pending := range d.run.PendingActions {
				if pending.Task == oldAction.Task {
					t.Fatal("accepted carry left a duplicate task or retry")
				}
			}
		})
	}
}

func TestValidationRequirementRemedyUsesOneConfirmableAsk(t *testing.T) {
	t.Parallel()
	for _, choice := range []protocol.AskOptionID{"proceed", "abort"} {
		t.Run(string(choice), func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			qa, qi := d.action("engine.qa.execution.v4")
			gate, gi := d.action("engine.gate.v3")
			report := executionFor(qi)
			d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "Output ordering needs correction"}}})
			wave := d.run.Validation.Waves[0]
			var findingID string
			for id := range d.run.Validation.Findings {
				findingID = id
			}
			resolution := protocol.FindingResolution{FindingID: findingID, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run", Module: "output", RootCause: "ordering", Consequence: "wrong ordering", Severity: "P2", SeverityRationale: "limited output issue", Verification: review.FindingVerification{Scope: "in scope", Severity: "limited", Causality: "verified", Reproduction: "app run", Classification: "implementation"}, Reason: "verified"}
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
			d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Choose remedy"}}, Validation: &protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: wave.ID, Remedies: []string{"REPAIR", "REQUEST_REQUIREMENT_CHANGE"}}}})
			var selected bool
			for _, ask := range d.run.PendingAsks {
				if ask.Validation != nil && ask.Validation.Kind == "VALIDATION_DETAIL_DISPOSITION" {
					d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "approve", Validation: &protocol.ValidationDecision{Reason: "Clarify the required ordering before further repair", Digest: ask.Validation.Digest, StateRevision: d.run.Revision, Remedy: "REQUEST_REQUIREMENT_CHANGE"}}})
					selected = true
					break
				}
			}
			if !selected || d.run.PendingRequirementAsk == nil {
				t.Fatal("remedy did not create the requirement barrier")
			}
			ask := *d.run.PendingRequirementAsk
			t.Logf("created requirement ask=%s options=%+v package=%+v", ask.RequestID, ask.Options, ask.Package)
			d.drive()
			if len(d.run.PendingActions) != 0 || d.run.PendingRequirementAsk.RequestID != ask.RequestID {
				t.Fatal("requirement barrier dispatched repair or changed Ask identity")
			}
			document := validationDocument + "\n"
			revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
			work := []protocol.RequirementWorkDecision{}
			for _, w := range d.run.RequirementWork {
				work = append(work, protocol.RequirementWorkDecision{Work: w, Disposition: protocol.RequirementWorkCarry, Reason: "same obligations and execution inputs"})
			}
			receipt := driverState(t, d).CurrentIntakeConfirmation()
			if err := os.WriteFile(filepath.Join(d.f.root, receipt.RequirementSource), []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			intake, err := prepareConfirmedIntake(d.f.root, receipt, true)
			if err != nil {
				t.Fatal(err)
			}
			pkg := protocol.RequirementDecisionPackage{Intake: &intake.IntakeCandidate, CandidateRevision: revision, DeltaDigest: "sha256:editorial", ImpactDigest: "sha256:unchanged-work", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: work}
			d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlRequestRequirementChange, Request: protocol.RequestID(ask.RequestID), Token: d.run.PendingRequirementAsk.Token, Package: &pkg}})
			if d.run.PendingRequirementAsk.RequestID != ask.RequestID || d.run.PendingRequirementAsk.Package == nil {
				t.Fatal("package update replaced or failed to bind the existing Ask")
			}
			t.Logf("package bound to same ask=%s", ask.RequestID)
			decision := &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: d.run.PendingRequirementAsk.Token, Choice: choice}
			if choice == "proceed" {
				decision.Requirement = &protocol.RequirementDecisionPayload{Intake: pkg.Intake, BaseRevision: d.revision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: work, Reason: "Confirmed unchanged ordering requirement"}
			}
			confirmation := protocol.Event{Kind: protocol.KindDecide, Decide: decision}
			if choice == "proceed" {
				confirmation = confirmation.WithRequirementDocument(document)
			}
			d.submit(confirmation)
			if d.run.PendingRequirementAsk != nil {
				t.Fatal("resolved Ask retained the requirement barrier")
			}
			expected := d.revision
			if choice == "proceed" {
				expected = revision
			}
			if d.run.Review.RequirementRevision != expected {
				t.Fatalf("requirement=%s want %s", d.run.Review.RequirementRevision, expected)
			}
			d.drive()
			_, repair := d.action("engine.repair.v7")
			if repair.Binding.RequirementRevision != expected {
				t.Fatal("repair resumed against the wrong confirmed requirement")
			}
			t.Logf("resolved %s; current requirement=%s; repair resumed", choice, expected)
		})
	}
}

// The adapter fixture classifies the explicitly declared QA artifacts. It does
// not infer ownership from path spelling or provide evidence in Submit/Drive.
func gateReviewView(params protocol.CandidateParameters) *protocol.GateReviewView {
	if params.Stage != "REVIEW_READY" {
		return nil
	}
	view := &protocol.GateReviewView{TestOwnedPaths: []string{}}
	for _, artifact := range params.QAArtifacts {
		for _, path := range artifact.Paths {
			if !slices.Contains(view.TestOwnedPaths, path) {
				view.TestOwnedPaths = append(view.TestOwnedPaths, path)
			}
		}
	}
	slices.Sort(view.TestOwnedPaths)
	return view
}

// abstractValidationFacade composes the protocol and injected Git facts for
// semantic admission tests. Native workspace effects have real-repository
// public-chain coverage; these fixtures provide typed operation observations.
func abstractValidationFacade(t *testing.T, root, provider string, runner vcs.Runner, base string, receipt IntakeConfirmationReceipt) (*Facade, Run, error) {
	return semanticValidationFacade(t, StartOptions{Capacity: 16, Root: root, ArtifactRoot: root, VCSRunner: runner, Admission: testAdmission(t, root, "candidate", "sha256:package", provider), Request: StartRequest{RunID: "validation", Route: "regular", Provider: provider, BaseSnapshot: base, IntakeConfirmationReceipt: receipt}})
}

// semanticValidationFacade supplies typed native observations in owner tests.
// Start's business initializer retains the actual budget and lifecycle rules;
// installed public tests own the physical workspace lifecycle coverage.
func semanticValidationFacade(t *testing.T, options StartOptions) (*Facade, Run, error) {
	t.Helper()
	root := options.Root
	admitted := *options.Admission
	req := options.Request
	req.DefinitionSource = DefaultDefinitionSource
	req.DefinitionDigest = definition.WorkflowDefinitionDigest
	req.PackageDigest, req.InstalledTargetIdentity = admitted.PackageDigest, admitted.InstalledTargetIdentity
	req.AdmissionRegistryPath, req.AdmissionTarget, req.AdmissionRoot = admitted.RegistryPath, admitted.Target, admitted.Root
	req.AdmissionGeneration, req.AdmissionLease, req.AdmissionToken = admitted.Generation, admitted.Lease, admitted.Token
	if req.MasterRunID != "" {
		master, err := Open(root, req.MasterRunID)
		if err != nil {
			return nil, Run{}, err
		}
		if admitted != master.admission {
			return nil, Run{}, fmt.Errorf("child fixture requires the exact parent admission")
		}
		return createdSliceFacade(t, master, req.SliceID, options.Owner)
	}
	var sliceStart *protocol.SliceRunStart
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		return nil, Run{}, err
	}
	stateDir := filepath.Join(root, EngineNamespace, req.RunID)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, Run{}, err
	}
	if err := os.WriteFile(filepath.Join(stateDir, "promotion.lock"), nil, 0o600); err != nil {
		return nil, Run{}, err
	}
	store, err := persistence.NewStore(stateDir, persistence.Config{PackageDigest: admitted.PackageDigest, DefinitionSource: DefaultDefinitionSource, InstalledTargetIdentity: admitted.InstalledTargetIdentity, AcquireAdmission: admitted.acquire})
	if err != nil {
		return nil, Run{}, err
	}
	if err := os.WriteFile(filepath.Join(root, req.IntakeConfirmationReceipt.RequirementSource), []byte(validationDocument), 0600); err != nil {
		return nil, Run{}, err
	}
	engine, err := protocol.New(store, protocol.Config{NativeRoot: root, Definition: cd, Registry: definition.Registry(), Capacity: options.Capacity, CostSource: CostSourceBridge(root, req.RunID), ClaimDispatch: dispatchClaimBridge(root, req.RunID, admitted), WorkerExecution: workerExecutionBridge(root, req.RunID)})
	if err != nil {
		return nil, Run{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, Run{}, err
	}
	effective, err := settings.Load(root, home)
	tokenCap := effective.Values.TokenCap
	if err != nil {
		return nil, Run{}, err
	}
	initialSnapshot, err := vcs.New(root, options.VCSRunner).Resolve()
	if err != nil {
		return nil, Run{}, err
	}
	var initialMaterials *IntakePreview
	if options.ArtifactRoot != "" {
		preview, err := captureIntake(options.ArtifactRoot, &req.IntakeConfirmationReceipt)
		if err != nil {
			return nil, Run{}, err
		}
		initialMaterials = &preview
	}
	if err := initializeRun(engine, root, req, initialSnapshot, sliceStart, options.Owner, tokenCap, admitted, cd, initialMaterials); err != nil {
		return nil, Run{}, err
	}
	if err := writeJSONAtomic(filepath.Join(stateDir, "request.json"), req); err != nil {
		return nil, Run{}, err
	}
	f := &Facade{admission: admitted, root: root, runID: req.RunID, stateDir: stateDir, metadata: filepath.Join(stateDir, "request.json"), store: store, engine: engine, envelope: store.Envelope(), definition: cd, capacity: options.Capacity, vcsRunner: options.VCSRunner, vcs: vcs.New(root, options.VCSRunner)}
	run, err := f.project()
	return f, run, err
}

func (r *frozenGitRepo) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	out, err := r.Run(dir, command, args...)
	return []byte(out), err
}

func (d *validationDriver) candidateReceipt(id string, intent protocol.HostActionIntent, p protocol.CandidateParameters) protocol.Event {
	assertCandidateStep(d.t, d, intent)
	if d.fixtureCandidateObserver != nil {
		return d.fixtureCandidateObserver(intent, p)
	}
	// 候选回执用当前原生身份与树摘要作证据（与晋升的内容绑定同源）。
	// 冻结场景产生候选对象而不改变目标 HEAD；真仓语义夹具
	// 从场景的 source provider 读取候选身份与其树摘要。
	identity, err := d.git.Resolve()
	if err != nil {
		d.t.Fatal(err)
	}
	if d.frozen != nil {
		target := d.frozen.head
		identity = d.frozen.advance()
		d.frozen.head = target
	}
	digest, err := d.git.TreeDigest(identity)
	if err != nil {
		d.t.Fatal(err)
	}
	evidence := protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, Identity: identity, Digest: digest, Paths: p.Paths, WorkspaceID: p.WorkspaceID, TestOnly: true, ProductionViewEqual: true}
	evidence.GateView = gateReviewView(p)
	data, _ := json.Marshal(evidence)
	fields := map[string]any{}
	_ = json.Unmarshal(data, &fields)
	e, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), id, "op.candidate", d.host(), intent.Correlation, intent.PayloadDigest, "EXECUTED", "", fields)
	if err != nil {
		d.t.Fatal(err)
	}
	return e
}

func (d *validationDriver) fixtureCandidate(id string, intent protocol.HostActionIntent, p protocol.CandidateParameters) {
	d.submit(d.candidateReceipt(id, intent, p))
	snapshot, err := d.f.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}
	if snapshot.State.HostActionReceipts[id].Step != intent.Step {
		d.t.Fatal("candidate completion lost its original compiled step")
	}
}

// Typed adapter fixtures observe each original operation and submit its own
// parameter-bound facts. Physical execution is covered by native workflow tests.
func (d *validationDriver) observeFixtureAdapter() bool {
	snapshot, err := d.f.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}

	ids := make([]string, 0, len(d.run.AdapterIntents))
	for id := range d.run.AdapterIntents {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		intent := d.run.AdapterIntents[id]
		if receipt, known := snapshot.State.HostActionReceipts[id]; known && receipt.Status == protocol.HostActionStatusExecuted {
			continue
		}
		if intent.Adapter != nil && !snapshot.State.NativeActionWritable(intent, d.f.definition) {
			continue
		}
		if intent.Adapter == nil {
			continue
		}
		raw, err := json.Marshal(intent.Adapter.Params)
		if err != nil {
			d.t.Fatal(err)
		}
		var value any
		switch intent.Adapter.Operation {
		case "op.workspace":
			var p protocol.WorkspaceParameters
			if err := json.Unmarshal(raw, &p); err != nil {
				d.t.Fatal(err)
			}
			r := p.Resource
			r.Identity, r.Status = r.Source, "READY"
			if p.Manifest != nil {
				r.MirrorDigest = protocol.BlackboxCasesDigest(r.Owner, *p.Manifest)
			}
			digest, err := d.git.TreeDigest(r.Source)
			if err != nil {
				d.t.Fatal(err)
			}
			value = protocol.WorkspaceEvidence{ParametersDigest: intent.PayloadDigest, Resource: r, Digest: digest, SourceDigest: digest}
		case "op.candidate":
			var p protocol.CandidateParameters
			if err := json.Unmarshal(raw, &p); err != nil {
				d.t.Fatal(err)
			}
			if p.Stage != "CHECK_SOURCE" {
				continue
			}
			d.fixtureCandidate(id, intent, p)
			return true
		case "op.check":
			var p protocol.CheckParameters
			if err := json.Unmarshal(raw, &p); err != nil {
				d.t.Fatal(err)
			}
			stdout, stderr, code := "completed approved command: "+p.Check.Command, "", 0
			if p.Check.Command == "false" {
				code = 1
			}
			evidence := protocol.CheckEvidence{ParametersDigest: intent.PayloadDigest, SourceActionID: p.SourceActionID, SourceIdentity: p.SourceIdentity, SourceDigest: p.SourceDigest, WorkspaceID: p.WorkspaceID, Directory: filepath.Join(d.f.stateDir, "fixture-checks", id), RequestDigest: fixtureDigest(intent.Adapter.Params), ExitCode: code}
			if p.QA != nil {
				operation := p.QA.Operation
				goName := ""
				if p.QA.GoTestRef != "" {
					resolved, err := coverage.GoTestOperation(p.QA.GoTestRef)
					if err != nil {
						d.t.Fatal(err)
					}
					operation, goName = resolved.Operation, resolved.GoTestName
				}
				argv := operation.Argv
				if operation.Kind == coverage.OperationShell {
					argv = []string{"/bin/sh", "-c", operation.Command}
				}
				invocation, err := json.Marshal(argv)
				if err != nil {
					d.t.Fatal(err)
				}
				stdout = "completed approved operation: " + string(invocation)
				if slices.Contains(argv, "invalid") {
					code = 2
					stdout = "rejected invalid input"
				}
				evidence.Tool, evidence.Environment = d.fixtureTool, d.fixtureEnvironment
				if evidence.Tool == "" {
					if argv[0] == "app" || argv[0] == "./app" {
						evidence.Tool = "app-v1"
					} else {
						evidence.Tool = "fixture-executable:" + fixtureDigest(argv[0])
					}
				}
				if evidence.Environment == "" {
					evidence.Environment = "isolated-project"
				}

				evidence.Invocation, evidence.WorkingDirectory = string(invocation), filepath.Join(p.QA.WorkspaceRoot, p.QA.Directory)
				if goName != "" {
					runEvent, err := json.Marshal(map[string]string{"Action": "run", "Test": goName})
					if err != nil {
						d.t.Fatal(err)
					}
					terminalEvent, err := json.Marshal(map[string]string{"Action": "pass", "Test": goName})
					if err != nil {
						d.t.Fatal(err)
					}
					stdout = string(runEvent) + "\n" + string(terminalEvent) + "\n"
					evidence.GoTestName, evidence.GoResult = goName, "pass"
				}
				if result, configured := d.fixtureQAResults[p.QA.CaseID]; configured {
					code, stdout, stderr = result.ExitCode, result.StdoutText, result.StderrText
					if goName != "" {
						evidence.GoResult = coverage.GoTestExecutionResult([]byte(stdout), goName)
					}
				}
			}
			evidence.ExitCode = code
			evidence.StdoutText, evidence.StderrText = stdout, stderr
			evidence.Stdout = fixtureStreamProof(stdout)
			evidence.Stderr = fixtureStreamProof(stderr)
			value = evidence
		default:
			continue
		}
		values, err := evidenceFields(value)
		if err != nil {
			d.t.Fatal(err)
		}
		event, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), id, intent.Adapter.Operation, d.host(), intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", values)
		if err != nil {
			d.t.Fatal(err)
		}
		d.submit(event)
		return true
	}
	return false
}
func fixtureDigest(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(body)
	return fmt.Sprintf("sha256:%x", sum)
}
func fixtureStreamProof(value string) protocol.CheckStreamProof {
	sum := sha256.Sum256([]byte(value))
	return protocol.CheckStreamProof{SHA256: fmt.Sprintf("%x", sum), Size: len(value)}
}

// Repair comparison uses its own issued role and the retained native facts.
func (d *validationDriver) verifyRepair(edit protocol.ValidationResult) {
	d.t.Helper()
	d.drive()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}
	id := ""
	var input protocol.WorkerInput
	for action, issued := range snapshot.State.WorkerInputs(d.f.definition) {
		if issued.Handler != "engine.repair.verify.v2" || issued.Validation == nil {
			continue
		}
		original := snapshot.State.Results[issued.Validation.Binding.VerificationSource]
		if original.Validation != nil && original.Validation.Binding.Task == edit.Binding.Task {
			id, input = action, issued
		}
	}
	if id == "" {
		d.t.Fatal("repair edit has no issued comparison for its original source")
	}
	observations := map[string]worker.Observation{}
	for _, row := range edit.Development.CaseRuns {
		observations[row.CaseID] = worker.Observation{Result: row.Result, Comparison: row.Comparison}
	}
	records := []*worker.Observation{}
	for _, caseID := range input.Validation.Repair.VerificationCases {
		value, exists := observations[caseID]
		if !exists {
			d.t.Fatalf("original repair comparison lacks issued case %s", caseID)
		}
		records = append(records, &value)
	}
	raw, err := json.Marshal(worker.Execution{Records: records})
	if err != nil {
		d.t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	event, err := worker.DecodeResult(input, d.eventID(), raw, fmt.Sprintf("sha256:%x", sum))
	if err != nil {
		d.t.Fatal(err)
	}
	d.receipt(id)
	accepted := d.submit(event)
	if accepted.Status != "ACCEPTED" {
		d.t.Fatalf("repair comparison refused: %+v", accepted)
	}
	d.drive()
}

// Required validation includes the approved complement's original PASS facts.
func (d *validationDriver) assertRequiredValidationComplete(scope coverage.ScopeMode) {
	d.t.Helper()
	if d.mockNative {
		d.drive()
	} else {
		for i := 0; i < 12; i++ {
			if _, _, err := d.f.engine.Drive(protocol.Observation{}); err != nil {
				d.t.Fatal(err)
			}
			if _, err := d.f.driveNative(); err != nil {
				d.t.Fatal(err)
			}
			current, err := d.f.engine.Load()
			if err != nil {
				d.t.Fatal(err)
			}
			if current.State.FinalValidationComplete(d.f.definition) {
				break
			}
		}
		var err error
		d.run, err = d.f.project()
		if err != nil {
			d.t.Fatal(err)
		}
	}
	snapshot, err := d.f.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if wave.Status != "COMPLETE" || wave.Scope != scope || !snapshot.State.FinalValidationComplete(d.f.definition) {
		facts, _ := json.Marshal(struct {
			Wave     protocol.ValidationWave
			Operator any
		}{wave, d.run.Next.Operator})
		d.t.Fatalf("required validation did not qualify for promotion: scope=%s status=%s next=%+v facts=%s", wave.Scope, wave.Status, d.run.Next, facts)
	}
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.qa.execution.v4" || input.Handler == "engine.gate.v3" {
			d.t.Fatal("completed validation issued an additional QA or gate")
		}
	}
}

// verifyReviewFixture submits independent evidence after comparing the exact
// completed worker source with the fixture's declared review and requirement.
func verifyReviewFixture(t *testing.T, d *validationDriver, event protocol.Event) {
	t.Helper()
	position := 0
	var original protocol.WorkerResult
	for index, source := range d.run.ReviewVerifications {
		if source.ActionID == event.Result.ActionID {
			if source.PayloadDigest != event.Result.PayloadDigest || !reflect.DeepEqual(source.Review, event.Result.Review) {
				t.Fatal("independent fixture received a different original review or requirement")
			}
			position = index + 1
			original = source
		}
	}
	if position == 0 {
		t.Fatal("complete review has no independent verification duty")
	}
	context := d.run.ReviewVerificationContexts[original.ActionID]
	var document string
	if context.Input != nil {
		input := context.Input
		if input.Review == nil || input.Review.Action != original.Review.Action || input.Review.RequirementRevision != original.Review.RequirementRevision {
			t.Fatal("independent fixture received another original responsibility")
		}
		document = input.RequirementDocument.Content
	} else if context.Intake != nil {
		document = context.Intake.Content
	}
	if document == "" || strings.TrimPrefix(requirementContentRevision([]byte(document)), "sha256:") != strings.TrimPrefix(original.Review.RequirementRevision, "sha256:") {
		t.Fatal("independent fixture lacks the complete original confirmed requirement")
	}
	if err := review.ValidateResult(original.Review.Action, review.Result{Status: original.Review.Status, Message: original.Review.Message, Findings: original.Review.Findings}); err != nil {
		t.Fatal(err)
	}
	if original.Review.Action == review.StartReadiness {
		if err := protocol.ValidateGranularity(original.Review.GranularityReview); err != nil {
			t.Fatal(err)
		}
	}
	evidence := []string{}
	for _, check := range review.RequiredVerificationChecks() {
		evidence = append(evidence, "Independent synthetic actor checked "+check+" against the complete original confirmed requirement and declared normal fixture scenario; exact source and original plan were compared separately")
	}
	judgments := []protocol.ReviewFindingVerification{}
	for _, finding := range event.Result.Review.Findings {
		judgments = append(judgments, protocol.ReviewFindingVerification{Valid: true, Reason: "Independent fixture verifies the declared normal entry and actual evidence: " + finding.NormalPath + "; " + finding.ActualError})
	}
	candidate, err := d.f.PrepareControl(ControlInput{ReviewVerification: &ReviewVerificationInput{Result: position, Verification: evidence, Findings: judgments}})
	if err != nil {
		t.Fatal(err)
	}
	accepted, run, err := submitControlWithHost(t, d.f, candidate)
	if err != nil || accepted.Status != "ACCEPTED" {
		t.Fatalf("independent fixture verification: %+v %v", accepted, err)
	}
	d.run = run
}
