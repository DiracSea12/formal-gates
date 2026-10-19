package facade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
)

// 本文件是真 git 仓上的快照公开链：regular Start 的 base 校验、终态多 commit
// 拒绝与晋升（含 squash）、QA 原生工作区投影与清空转换。全部走生产 exec
// runner，无模型调用。

// PrepareCandidate exercises the signed promotion handler within its native window.
func (f *Facade) PrepareCandidate(_ string) (string, error) {
	if f.vcs == nil {
		return "", fmt.Errorf("candidate promotion requires a regular run with a native VCS provider")
	}
	unlock, err := f.acquireWriteWindow()
	if err != nil {
		return "", err
	}
	defer unlock()
	return f.prepareCandidate()
}

// snapshotRepoCommits 在 root 建立真 git 仓并按序提交（首提交即 base，内容含
// requirements.md），返回逐次提交身份（[0]=base）。
func snapshotRepoCommits(t *testing.T, root string, commits ...string) []string {
	t.Helper()
	runner := vcs.ExecRunner{}
	run := func(args ...string) string {
		t.Helper()
		out, err := runner.Run(root, "git", args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	run("init")
	run("config", "user.email", "snapshot-wiring@example.invalid")
	run("config", "user.name", "Snapshot Wiring Test")
	run("config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".gates/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.22\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writeSnapshotCommand(t, root)
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "--all")
	messages := append([]string{"base"}, commits...)
	identities := make([]string, 0, len(messages))
	for i, message := range messages {
		if i > 0 {
			if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // "+message+"\nfunc main() {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			run("add", "--all")
		}
		run("commit", "-m", message)
		identity, err := vcs.New(root, nil).Resolve()
		if err != nil {
			t.Fatal(err)
		}
		identities = append(identities, identity)
	}
	return identities
}

func writeSnapshotCommand(t *testing.T, root string) {
	t.Helper()
	app, body := "app", "#!/bin/sh\n[ \"$1\" = run ] && [ \"$2\" = valid ]\n"
	if goruntime.GOOS == "windows" {
		app, body = "app.cmd", "@echo off\r\nif \"%~1\"==\"run\" if \"%~2\"==\"valid\" exit /b 0\r\nexit /b 1\r\n"
	}
	if err := os.WriteFile(filepath.Join(root, app), []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
}

// snapshotCommitOne 在已建仓库上追加一个提交并返回新身份。
func snapshotCommitOne(t *testing.T, root, message string) string {
	t.Helper()
	runner := vcs.ExecRunner{}
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // "+message+"\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "commit", "-m", message); err != nil {
		t.Fatal(err)
	}
	identity, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func startSnapshotRun(t *testing.T, root, runID, base string) *Facade {
	t.Helper()
	receipt := testReceipt()
	f, _, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID: runID, Route: "regular", BaseSnapshot: base,
		DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest,
		IntakeConfirmationReceipt: receipt,
	}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestRegularStartAcceptsAncestorBaseAndRejectsDivergent：regular Start 把
// BaseSnapshot 变成事实——合法（可复现且为 HEAD 祖先或相等）接纳并规范化为小
// 写；分叉身份与非法形态可区分拒绝。
func TestRegularStartAcceptsAncestorBaseAndRejectsDivergent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "second")
	base, head := identities[0], identities[len(identities)-1]
	runner := vcs.ExecRunner{}
	if _, err := runner.Run(root, "git", "checkout", "-q", "-b", "divergent", base); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "commit", "-q", "-m", "divergent"); err != nil {
		t.Fatal(err)
	}
	divergent, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "checkout", "-q", "--detach", head); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		base    string
		wantErr string
	}{
		"ancestor base":    {base: strings.ToUpper(base)},
		"head as base":     {base: head},
		"divergent base":   {base: divergent, wantErr: "is not an ancestor of the current snapshot"},
		"malformed base":   {base: "not-a-commit", wantErr: "invalid git snapshot identity"},
		"foreign snapshot": {base: "0123456789abcdef0123456789abcdef01234567", wantErr: "cannot verify git snapshot"},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.wantErr == "" {
				f := startSnapshotRun(t, root, "snapshot-"+strings.ReplaceAll(strings.ReplaceAll(name, " ", "-"), "/", "-"), tc.base)
				initial, err := f.engine.Load()
				if err != nil || initial.State.InitialSnapshot != head {
					t.Fatalf("accepted startup HEAD = %+v, %v; want %s", initial, err, head)
				}
				run, err := f.Drive()
				if err != nil {
					t.Fatal(err)
				}
				if run.BaseSnapshot != strings.ToLower(tc.base) {
					t.Fatalf("base snapshot = %q, want lowercased %q", run.BaseSnapshot, tc.base)
				}
				return
			}
			_, _, err := Start(StartOptions{Root: root, Request: StartRequest{
				RunID: "snapshot-reject-" + strings.ReplaceAll(strings.ReplaceAll(name, " ", "-"), "/", "-"), Route: "regular", BaseSnapshot: tc.base,
				DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest,
				IntakeConfirmationReceipt: testReceipt(),
			}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("start error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

// TestCandidatePromotionSquashesMultiCommitRangeAndCompletesTerminal：validation
// 完成后的晋升——区间单提交确认当前 HEAD；多提交先拒无消息、再以 squash 消息
// 压回单提交并重解析绑定；晋升后经真实 Drive 收口到 COMPLETE（引擎 phase
// TERMINAL、终态摘要落盘），晋升后新增提交在终态边界被最后防线拒绝。
func TestCandidatePromotionSquashesMultiCommitRangeAndCompletesTerminal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	runner := vcs.ExecRunner{}
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	d.reviewReady()
	qa, qi := d.action("engine.qa.execution.v4")
	gate, gi := d.action("engine.gate.v3")
	report := executionFor(qi)
	tool, environment := protocol.ExecutionIdentities(qi.ExecutionFacts)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if wave.Status != "COMPLETE" {
		t.Fatalf("wave status = %s, want COMPLETE", wave.Status)
	}
	// 晋升等待在位：终态不可达。
	pending, err := d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status == "COMPLETE" {
		t.Fatal("terminal reached without candidate promotion")
	}
	promotionWait := false
	if pending.Next.Operator != nil {
		for _, fact := range pending.Next.Operator.Facts {
			if fact.Key == "validation" && strings.Contains(fact.Value, "candidate promotion") {
				promotionWait = true
			}
		}
	}
	if !promotionWait {
		t.Fatalf("terminal boundary = %+v, want promotion wait", pending.Next)
	}
	approvedSource := d.run.Validation.Candidate.Identity
	promoted, err := d.f.PrepareCandidate("")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	proof := snapshot.State.CandidatePromotion
	intent := snapshot.State.ReconciledEffects[proof.OperationID].Intent
	if intent == nil {
		t.Fatal("native organization lost its original signed intent")
	}
	var params protocol.CandidateParameters
	if err := decodeNativeParams(*intent, &params); err != nil {
		t.Fatal(err)
	}
	wantMessage := "formal-gates delivery " + d.run.RunID + " " + base + " " + approvedSource
	if params.Promotion == nil || params.Promotion.Message != wantMessage {
		t.Fatalf("default native organization message: %+v", params.Promotion)
	}
	if promoted == identities[len(identities)-1] {
		t.Fatal("squash kept the pre-squash identity")
	}
	if count, err := vcs.New(root, nil).CommitCount(base, promoted); err != nil || count != 1 {
		t.Fatalf("post-squash count = %d, %v", count, err)
	}
	// squash 保留最终树：晋升内容摘要与已验证最终候选的树摘要相等（digest
	// 绑定的真实验证——squash 只改提交历史，不改内容）。
	reviewTree := d.run.Validation.Candidate.Digest
	promotedTree, err := vcs.New(root, nil).TreeDigest(promoted)
	if err != nil {
		t.Fatal(err)
	}
	if promotedTree != reviewTree {
		t.Fatalf("squash changed the final tree: promoted %s, verified %s", promotedTree, reviewTree)
	}
	bound, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if bound.State.PromotedDigest != promotedTree {
		t.Fatalf("promoted digest = %s, want the verified tree %s", bound.State.PromotedDigest, promotedTree)
	}
	// 晋升绑定后收口：等待事实消失，真实 Drive 到达终态并发布摘要。
	run, err := d.f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.PromotedSnapshot != promoted {
		t.Fatalf("promoted snapshot = %s, want %s", run.PromotedSnapshot, promoted)
	}
	if run.Status != "COMPLETE" {
		t.Fatalf("run status = %s, want COMPLETE (next = %+v)", run.Status, run.Next)
	}
	if run.Next.Operator != nil {
		for _, fact := range run.Next.Operator.Facts {
			if fact.Key == "validation" && strings.Contains(fact.Value, "candidate promotion") {
				t.Fatalf("promotion wait survived promotion: %+v", fact)
			}
		}
	}
	summary, err := d.f.readSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Termination == nil || summary.Termination.Phase != runtime.PhaseTerminal {
		t.Fatalf("retained terminal State: %+v", summary.Termination)
	}
	if _, err := os.Stat(filepath.Join(d.f.stateDir, "terminal-summary.json")); err != nil {
		t.Fatalf("terminal summary missing: %v", err)
	}
	// 终态快照边界消费真实 Drive 结果：晋升后区间单提交，guard 通过且发布
	// 段身份恒等。
	guard, err := d.f.terminalSnapshotGuard(run)
	if err != nil {
		t.Fatalf("guard after promotion: %v", err)
	}
	if err := guard(); err != nil {
		t.Fatalf("guard re-resolve: %v", err)
	}
	// 晋升后新增提交：发布检查点按同一身份判据拒绝漂移的目标。
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // late\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "commit", "-q", "-m", "late commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.f.terminalSnapshotGuard(run); err == nil || !strings.Contains(err.Error(), "does not match the promoted snapshot") {
		t.Fatalf("guard accepted a drifted publication baseline: %v", err)
	}
}

// TestCandidatePromotionNoOpConfirmsSingleCommit：区间单提交时晋升为确认当前
// HEAD（身份不变），等待解除。
func TestCandidatePromotionNoOpConfirmsSingleCommit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	d.reviewReady()
	qa, qi := d.action("engine.qa.execution.v4")
	gate, gi := d.action("engine.gate.v3")
	report := executionFor(qi)
	tool, environment := protocol.ExecutionIdentities(qi.ExecutionFacts)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	prepareApprovedPromotionPrefix(t, d, "confirm single source")
	head, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if head != d.run.Validation.Candidate.Identity {
		t.Fatalf("repo head %s != approved source %s before promotion", head, d.run.Validation.Candidate.Identity)
	}
	promoted, err := d.f.PrepareCandidate("")
	if err != nil {
		t.Fatal(err)
	}
	if promoted != head {
		t.Fatalf("single-commit promotion = %s, want current head %s", promoted, head)
	}
	again, err := d.f.PrepareCandidate("")
	if err != nil || again != promoted {
		t.Fatalf("repeat promotion = %q, %v", again, err)
	}
	run, err := d.f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.PromotedSnapshot != promoted {
		t.Fatalf("promoted snapshot = %s, want %s", run.PromotedSnapshot, promoted)
	}
	if _, err := d.f.terminalSnapshotGuard(run); err != nil {
		t.Fatalf("single-commit guard: %v", err)
	}
}

// TestPrepareCandidateRejectsUncommittedChanges：晋升拒绝被跟踪修改与未跟踪
// 交付文件；wave COMPLETE 准入后、执行前拒绝，不产生 Git 副作用。
func TestPrepareCandidateRejectsUncommittedChanges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root, "dev commit")[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	d.reviewReady()
	qa, qi := d.action("engine.qa.execution.v4")
	gate, gi := d.action("engine.gate.v3")
	report := executionFor(qi)
	tool, environment := protocol.ExecutionIdentities(qi.ExecutionFacts)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	if wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]; wave.Status != "COMPLETE" {
		t.Fatalf("premise lost: wave status = %s", wave.Status)
	}
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	head, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.f.PrepareCandidate("message"); err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") {
		t.Fatalf("dirty worktree promotion: %v", err)
	}
	if after, err := vcs.New(root, nil).Resolve(); err != nil || after != head {
		t.Fatalf("dirty promotion moved HEAD: %s -> %s (%v)", head, after, err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.f.PrepareCandidate("message"); err == nil {
		t.Fatal("untracked deliverable accepted by promotion")
	}
}

// A native blackbox workspace receipt owns the active QA projection and source.
func TestBlackboxWorkspaceReceiptProjectsOriginalSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	_, input := d.action("engine.qa.design.v6")
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	resource := snap.State.NativeResources[input.Binding.WorkspaceID]
	if resource.ID != input.Binding.WorkspaceID || resource.Owner != d.run.RunID || resource.Role != "blackbox-design" || resource.Status != "READY" || resource.Path == "" || resource.Path == root || resource.Source != base || resource.Identity != base || resource.Ref == "" {
		t.Fatalf("blackbox resource: %+v", resource)
	}
	var receipt protocol.HostActionReceipt
	var intent protocol.HostActionIntent
	for id, effect := range snap.State.ReconciledEffects {
		if effect.Intent == nil || effect.Intent.Adapter == nil || effect.Intent.Adapter.Operation != "op.workspace" {
			continue
		}
		var params protocol.WorkspaceParameters
		if err := decodeNativeParams(*effect.Intent, &params); err != nil {
			t.Fatal(err)
		}
		if params.Resource.ID == resource.ID {
			intent, receipt = *effect.Intent, snap.State.HostActionReceipts[id]
			expected := params.Resource
			expected.Identity, expected.Status = expected.Source, "READY"
			if params.Task == nil || params.RequirementRevision != snap.State.RequirementRevision() || params.Generation != snap.State.Validation.Generation || params.RouteScope == "" || params.Manifest != nil || expected != resource {
				t.Fatalf("signed blackbox parameters: %+v", params)
			}
		}
	}
	if intent.ActionID == "" || receipt.ActionID != intent.ActionID || receipt.AdapterOperation != "op.workspace" || receipt.Status != protocol.HostActionStatusExecuted || receipt.Provider != snap.State.RunProvider || receipt.Correlation != intent.Correlation || receipt.PayloadDigest != intent.PayloadDigest || receipt.AdapterEvidence == nil {
		t.Fatalf("workspace receipt: %+v", receipt)
	}
	var evidence protocol.WorkspaceEvidence
	raw, err := json.Marshal(receipt.AdapterEvidence.Values)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatal(err)
	}
	tree, err := d.f.vcs.TreeDigest(resource.Source)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ParametersDigest != intent.PayloadDigest || evidence.Resource != resource || evidence.Digest != tree || evidence.SourceDigest != tree {
		t.Fatalf("workspace proof: %+v", evidence)
	}
	if head, err := d.f.vcs.At(resource.Path, resource.Handle).Resolve(); err != nil || head != resource.Source {
		t.Fatalf("workspace source: %s %v", head, err)
	}
	if ref, err := d.f.vcs.Ref(resource.Ref); err != nil || ref != resource.Source {
		t.Fatalf("workspace ref: %s %v", ref, err)
	}
	for _, projection := range []func() (Run, error){d.f.Status, d.f.Show} {
		run, err := projection()
		if err != nil || run.QAWorktree != resource.Path {
			t.Fatalf("QA projection: %q %v", run.QAWorktree, err)
		}
	}
	drifted := snapshotCommitOne(t, root, "another source")
	if _, err := (vcs.ExecRunner{}).Run(resource.Path, "git", "checkout", "--detach", drifted); err != nil {
		t.Fatal(err)
	}
	before := installationFiles(t, d.f.stateDir)
	nativeBefore := installationFiles(t, resource.Path)
	if exists, err := d.f.observeWorktree(resource.Path, resource.Source, resource.Handle); exists || err == nil || !strings.Contains(err.Error(), "identity differs from original source") {
		t.Fatalf("source conflict observation: %t %v", exists, err)
	}
	if !reflect.DeepEqual(before, installationFiles(t, d.f.stateDir)) || !reflect.DeepEqual(nativeBefore, installationFiles(t, resource.Path)) {
		t.Fatal("source observation changed engine or native bytes")
	}
	if head, err := d.f.vcs.At(resource.Path, resource.Handle).Resolve(); err != nil || head != drifted {
		t.Fatalf("source observation moved HEAD: %s %v", head, err)
	}
}

// reviewPass 完成产品审与 start-readiness 两轮审查（planned 驱动的前半段，
// 停在拓扑确认前——供需要在路线确认前后分别注入操作的用例）。
func (d *validationDriver) reviewPass(plan *protocol.GranularityReview) {
	d.t.Helper()
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		id := ""
		for key, pending := range d.run.PendingActions {
			if pending.ReviewSeries == action {
				id = key
			}
		}
		if id == "" {
			d.t.Fatalf("missing %s dispatch: %+v", action, d.run)
		}
		d.receipt(id)
		e, err := protocol.NewReviewWorkerResultEvent(d.eventID(), id, d.host(), string(d.eventID()), protocol.ReviewResultEvidence{GranularityReview: func() *protocol.GranularityReview {
			if action == review.StartReadiness {
				input, ok := driverState(d.t, d).WorkerInput(id, d.f.definition)
				if !ok {
					d.t.Fatal("review fixture lacks signed input")
				}
				fixturePlanRequirements(plan, input.RequirementPoints)
				return plan
			}
			return nil
		}(), Action: action, DispatchID: id, RequirementRevision: d.revision, Status: "PASS", Findings: []review.Finding{}})
		if err != nil {
			d.t.Fatal(err)
		}
		d.submit(e)
		verifyReviewFixture(d.t, d, e)
		d.drive()
	}
}

// A frozen blackbox set retires its projection while retaining native ownership.
func TestQAWorktreeClearsWhenBlackboxSetFreezes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	_, input := d.action("engine.qa.design.v6")
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	resource := snap.State.NativeResources[input.Binding.WorkspaceID]
	if resource.ID != input.Binding.WorkspaceID || resource.Role != "blackbox-design" || resource.Owner != d.run.RunID || resource.Path == "" || resource.Source != base || resource.Identity != base || resource.Status != "READY" || snap.State.QAWorktree != resource.Path {
		t.Fatalf("active design resource: %+v", resource)
	}
	if set := snap.State.Validation.Sets[coverage.ReviewBlackbox]; set != nil && set.Status == "FROZEN" {
		t.Fatal("design fixture already has a frozen case set before its result")
	}
	proved := false
	for id, effect := range snap.State.ReconciledEffects {
		if effect.Intent == nil || effect.Intent.Adapter == nil || effect.Intent.Adapter.Operation != "op.workspace" {
			continue
		}
		var params protocol.WorkspaceParameters
		if err := decodeNativeParams(*effect.Intent, &params); err != nil {
			t.Fatal(err)
		}
		if params.Resource.ID != resource.ID {
			continue
		}
		receipt := snap.State.HostActionReceipts[id]
		if receipt.ActionID != effect.Intent.ActionID || receipt.AdapterOperation != "op.workspace" || receipt.Status != protocol.HostActionStatusExecuted || receipt.PayloadDigest != effect.Intent.PayloadDigest || receipt.AdapterEvidence == nil {
			t.Fatalf("design receipt: %+v", receipt)
		}
		var evidence protocol.WorkspaceEvidence
		raw, err := json.Marshal(receipt.AdapterEvidence.Values)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &evidence); err != nil {
			t.Fatal(err)
		}
		if evidence.ParametersDigest != effect.Intent.PayloadDigest || evidence.Resource != resource || evidence.Digest == "" || evidence.SourceDigest != evidence.Digest {
			t.Fatalf("design proof: %+v", evidence)
		}
		proved = true
	}
	if !proved {
		t.Fatal("active design workspace lacks its executed native receipt")
	}
	d.reviewReady()
	snap, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	set := snap.State.Validation.Sets[coverage.ReviewBlackbox]
	if set == nil || set.Status != "FROZEN" {
		t.Fatalf("blackbox set did not freeze: %+v", set)
	}
	expected := resource
	expected.MirrorDigest = protocol.BlackboxCasesDigest(snap.State.RunID, set.Manifest)
	if actual := snap.State.NativeResources[resource.ID]; actual != expected {
		t.Fatalf("freeze changed native ownership or lost its case mirror: actual=%+v expected=%+v", actual, expected)
	}
	for _, projection := range []func() (Run, error){d.f.Status, d.f.Show} {
		run, err := projection()
		if err != nil {
			t.Fatal(err)
		}
		if run.QAWorktree != "" {
			t.Fatalf("QA projection survived blackbox set freeze: %q", run.QAWorktree)
		}
	}
}

// TestPrepareCandidateChainPassesWithoutGitignore：宿主仓库未 gitignore 运行
// 期目录时，引擎自建目录（.gates/engine/ 等）不阻断晋升——无 .gitignore 的干
// 净仓上验证管线完成后的 Start→PrepareCandidate 全链通过（幂等）。晋升绑定的
// 是已验证候选：区间累积了改变内容的新提交时，重新组织因内容与已验证最终候
// 选不符被拒（漂移须重验）。
func TestPrepareCandidateChainPassesWithoutGitignore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := vcs.ExecRunner{}
	run := func(args ...string) {
		t.Helper()
		if _, err := runner.Run(root, "git", args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init")
	run("config", "user.email", "no-gitignore@example.invalid")
	run("config", "user.name", "No Gitignore Test")
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.22\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writeSnapshotCommand(t, root)
	run("add", "--all")
	run("commit", "-m", "base")
	base, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	d.reviewReady()
	qa, qi := d.action("engine.qa.execution.v4")
	gate, gi := d.action("engine.gate.v3")
	report := executionFor(qi)
	tool, environment := protocol.ExecutionIdentities(qi.ExecutionFacts)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	approved := d.run.Validation.Candidate.Identity
	prepareApprovedPromotionPrefix(t, d, "confirm single source")
	promoted, err := d.f.PrepareCandidate("")
	if err != nil {
		t.Fatalf("promotion on a clean repo without .gitignore failed: %v", err)
	}
	if promoted != approved {
		t.Fatalf("single-commit promotion moved approved HEAD: %s -> %s", approved, promoted)
	}
	again, err := d.f.PrepareCandidate("")
	if err != nil || again != promoted {
		t.Fatalf("repeat promotion = %q, %v", again, err)
	}
	projection, err := d.f.Show()
	if err != nil || projection.PromotedSnapshot != promoted {
		t.Fatalf("public projection = %q, %v", projection.PromotedSnapshot, err)
	}
	// 晋升后新增提交改变了内容：重新组织不再绑定已验证候选——拒绝且零
	// Git 副作用（HEAD 保持在新提交上，恢复路径＝回退到已验证内容或重验）。
	// 漂移提交只携交付文件：把运行期目录一并提交会让引擎自身的状态写入表
	// 现为被跟踪修改，那是另一个边界（就绪检查如实报告）。
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // later\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "app.go")
	run("commit", "-m", "later work")
	drifted, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.f.PrepareCandidate("re-organized candidate"); err == nil || !strings.Contains(err.Error(), "does not match the promoted snapshot") {
		t.Fatalf("content-drifted re-organization: %v", err)
	}
	if after, err := vcs.New(root, nil).Resolve(); err != nil || after != drifted {
		t.Fatalf("rejected re-organization moved HEAD: %s -> %s (%v)", drifted, after, err)
	}
}

// completePromotionPipeline 把真仓 regular run 推进到「终 wave COMPLETE、待晋
// 升」的边界（验证收集完成、区间保持 base..HEAD 多提交——晋升需要 squash，
// 使「被拒时零 Git 副作用」可观察）。runner 注入门面 Git provider（nil＝生产
// exec）。
func completePromotionPipeline(t *testing.T, runner vcs.Runner, root, base string) *validationDriver {
	t.Helper()
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, runner, base, nil, commandPlan(), "blackbox", "quality")
	d.reviewReady()
	qa, qi := d.action("engine.qa.execution.v4")
	gate, gi := d.action("engine.gate.v3")
	report := executionFor(qi)
	tool, environment := protocol.ExecutionIdentities(qi.ExecutionFacts)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	if wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]; wave.Status != "COMPLETE" {
		t.Fatalf("wave status = %s, want COMPLETE", wave.Status)
	}
	return d
}

// prepareApprovedPromotionPrefix retains the signed source and target start,
// then prepares the approved bytes before the tested squash window.
func prepareApprovedPromotionPrefix(t *testing.T, d *validationDriver, message string) {
	t.Helper()
	candidate := d.run.Validation.Candidate
	params := promotionInputForTest(t, d, candidate.Identity, message)
	if err := d.f.engine.OpenPromotion(params, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	if err := d.f.admission.write(func() error { return d.f.vcs.FastForward(candidate.Identity) }); err != nil {
		t.Fatal(err)
	}
	tree, err := d.f.vcs.TreeDigest("HEAD")
	if err != nil || tree != candidate.Digest {
		t.Fatalf("prepared target %s != approved %s: %v", tree, candidate.Digest, err)
	}
}

// TestPromotionFrozenByRequirementChangeBarrierLeavesHistoryUntouched：需求变
// 化屏障期间的晋升在意图准入处拒绝（Phase A 停止 candidate promotion）——多提
// 交区间保持原样（零 Git 副作用），且 REQUEST 落账撤销写资格、保留原观察债务。
func TestPromotionFrozenByRequirementChangeBarrierLeavesHistoryUntouched(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	d := completePromotionPipeline(t, nil, root, base)
	head, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if count, err := vcs.New(root, nil).CommitCount(base, head); err != nil || count != 2 {
		t.Fatalf("premise lost: interval count = %d, %v", count, err)
	}
	// 屏障到达前存在未完成意图：REQUEST 落账撤销写资格，保留原观察债务。
	if err := d.f.engine.OpenPromotion(promotionInputForTest(t, d, d.run.Validation.Candidate.Identity, "organized final candidate"), protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Revise"}, protocol.AskOption{ID: "abort", Label: "Keep"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pendingPromotionSource(snap.State) != d.run.Validation.Candidate.Identity {
		t.Fatalf("requirement change lost original native debt: %q", pendingPromotionSource(snap.State))
	}
	if _, err := d.f.PrepareCandidate("organized under barrier"); err == nil || !strings.Contains(err.Error(), "frozen while a requirement change ask is pending") {
		t.Fatalf("promotion under barrier: %v", err)
	}
	// 零 Git 副作用：HEAD 与区间提交数逐字节不变（多提交未被动过）。
	after, err := vcs.New(root, nil).Resolve()
	if err != nil || after != head {
		t.Fatalf("rejected promotion moved HEAD: %s -> %s (%v)", head, after, err)
	}
	if count, err := vcs.New(root, nil).CommitCount(base, after); err != nil || count != 2 {
		t.Fatalf("rejected promotion rewrote history: count = %d, %v", count, err)
	}
}

// TestTerminalRunRejectsPromotionWithoutTouchingHistory：终态 run 的晋升误调
// 用在意图准入处拒绝（活动期要求），被改写前的 Git 历史保持原样。
func TestTerminalRunRejectsPromotionWithoutTouchingHistory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	d := completePromotionPipeline(t, nil, root, base)
	if _, err := d.f.PrepareCandidate("organized final candidate"); err != nil {
		t.Fatal(err)
	}
	promoted, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.f.Drive()
	if err != nil || run.Status != "COMPLETE" || run.PromotedSnapshot != promoted {
		t.Fatalf("terminal drive = %s promoted = %s, want COMPLETE %s (%v)", run.Status, run.PromotedSnapshot, promoted, err)
	}
	// 公开终态清理移除活动投影；内容保持型提交后的晋升误调用仍被拒绝。
	runner := vcs.ExecRunner{}
	if _, err := runner.Run(root, "git", "commit", "-q", "--allow-empty", "-m", "late work"); err != nil {
		t.Fatal(err)
	}
	late, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	var rejected *protocol.RejectedError
	if _, err := d.f.PrepareCandidate("reorganized after terminal"); !errors.As(err, &rejected) || rejected.Code != protocol.CodeNotInitialized {
		t.Fatalf("terminal promotion: %v", err)
	}
	if after, err := vcs.New(root, nil).Resolve(); err != nil || after != late {
		t.Fatalf("terminal promotion moved HEAD: %s -> %s (%v)", late, after, err)
	}
}

// TestTerminalSealingRejectsDirtyTreeAndDriftUntilReorganized：终态核验先于相
// 变——脏树与身份漂移都拒绝且 run 保持活动（phase 非终态、错误可辨识）；回
// 到已绑定晋升身份后 Drive 恢复 COMPLETE。
func TestTerminalSealingRejectsDirtyTreeAndDriftUntilReorganized(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := vcs.ExecRunner{}
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	d := completePromotionPipeline(t, nil, root, base)
	promoted, err := d.f.PrepareCandidate("organized final candidate")
	if err != nil {
		t.Fatal(err)
	}
	// 脏树：HEAD 未移动但工作区有未提交内容——终态拒绝、保持活动。
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.f.Drive(); err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") {
		t.Fatalf("dirty terminal sealing: %v", err)
	}
	if snap, err := d.f.engine.Load(); err != nil || snap.State.Phase == runtime.PhaseTerminal {
		t.Fatalf("dirty rejection sealed terminal: %v (%v)", snap.State.Phase, err)
	}
	if _, err := runner.Run(root, "git", "checkout", "--", "app.go"); err != nil {
		t.Fatal(err)
	}
	// 身份漂移：区间再累积一个提交（区间计数仍为 1）——终态拒绝、保持活
	// 动，重新组织晋升的恢复路径敞开。
	snapshotCommitOne(t, root, "drift work")
	if _, err := d.f.Drive(); err == nil || !strings.Contains(err.Error(), "does not match the promoted snapshot") {
		t.Fatalf("drifted terminal sealing: %v", err)
	}
	if snap, err := d.f.engine.Load(); err != nil || snap.State.Phase == runtime.PhaseTerminal {
		t.Fatalf("drift rejection sealed terminal: %v (%v)", snap.State.Phase, err)
	}
	// 恢复：回到已验证内容（reset 到晋升身份），Drive 收口 COMPLETE。
	if _, err := runner.Run(root, "git", "reset", "-q", "--hard", promoted); err != nil {
		t.Fatal(err)
	}
	run, err := d.f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "COMPLETE" || run.PromotedSnapshot != promoted {
		t.Fatalf("recovered terminal = %s promoted = %s, want COMPLETE %s", run.Status, run.PromotedSnapshot, promoted)
	}
}

// TestPromotionIntentSurvivesExecuteCrashAndReentryConverges：意图幂等（同
// target 重开零变化）；execute 后崩溃（squash 已发生、意图仍在）经重入收敛——
// 重解析得新 HEAD、覆盖意图、区间单提交确认、提交绑定，终态可达。
func TestPromotionIntentSurvivesExecuteCrashAndReentryConverges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := vcs.ExecRunner{}
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	d := completePromotionPipeline(t, nil, root, base)
	head, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	// 意图幂等：同 target 重开不报错、不产生第二意图。
	if err := d.f.engine.OpenPromotion(promotionInputForTest(t, d, d.run.Validation.Candidate.Identity, "organized final candidate"), protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	if err := d.f.engine.OpenPromotion(promotionInputForTest(t, d, d.run.Validation.Candidate.Identity, "organized final candidate"), protocol.Observation{}); err != nil {
		t.Fatalf("intent reopen: %v", err)
	}
	snap, err := d.f.engine.Load()
	if err != nil || pendingPromotionSource(snap.State) != d.run.Validation.Candidate.Identity {
		t.Fatalf("intent reopen changed the target: %q (%v)", pendingPromotionSource(snap.State), err)
	}
	prepareApprovedPromotionPrefix(t, d, "organized final candidate")
	// execute 后崩溃：宿主已完成 squash（真实 reset --soft＋commit），意图
	// 保留、引擎未提交。重入经 PrepareCandidate 收敛。
	if _, err := runner.Run(root, "git", "reset", "-q", "--soft", base); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "commit", "-q", "-m", "organized final candidate"); err != nil {
		t.Fatal(err)
	}
	squashed, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if squashed == head {
		t.Fatal("premise lost: squash did not move the identity")
	}
	promoted, err := d.f.PrepareCandidate("")
	if err != nil {
		t.Fatalf("reentry promotion: %v", err)
	}
	if promoted != squashed {
		t.Fatalf("reentry reorganized again: %s -> %s", squashed, promoted)
	}
	bound, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pendingPromotionSource(bound.State) != "" || bound.State.PromotedSnapshot != squashed {
		t.Fatalf("reentry did not converge: pending=%q promoted=%q", pendingPromotionSource(bound.State), bound.State.PromotedSnapshot)
	}
	run, err := d.f.Drive()
	if err != nil || run.Status != "COMPLETE" {
		t.Fatalf("post-crash terminal = %s, %v", run.Status, err)
	}
}

// A delivered target awaits seal after actual native cleanup. Content drift
// rejects publication; restoring the approved identity resumes the same path.
func TestTerminalCheckpointDriftRejectsPublicationUntilContentRestored(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := vcs.ExecRunner{}
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	d := completePromotionPipeline(t, nil, root, base)
	promoted, err := d.f.PrepareCandidate("organized final candidate")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := d.f.acquireNativeWindow()
	if err != nil {
		t.Fatal(err)
	}
	err = func() error {
		defer unlock()
		if err := d.f.engine.BeginCleanup(protocol.Observation{}); err != nil {
			return err
		}
		snapshot, err := d.f.engine.Load()
		if err != nil {
			return err
		}
		for _, intent := range snapshot.State.PendingHostActions {
			if intent.Adapter != nil && intent.Adapter.Operation == "op.cleanup" {
				completed, err := d.f.completeNativeIntent(snapshot.State, intent, true)
				if err != nil {
					return err
				}
				if !completed {
					return fmt.Errorf("native cleanup did not produce its actual receipt")
				}
			}
		}
		return nil
	}()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := d.f.engine.Load()
	if err != nil || !checkpoint.State.ResourcesCleaned() || checkpoint.State.Phase == runtime.PhaseTerminal {
		t.Fatalf("checkpoint is not cleaned and awaiting seal: %+v %v", checkpoint, err)
	}
	summaryPath := terminalSummaryPath(d.f.stateDir)
	if _, err := os.Stat(summaryPath); !os.IsNotExist(err) {
		t.Fatalf("unsealed checkpoint published summary: %v", err)
	}
	// 用户 amend HEAD（改写内容）后重开：同一核验拒绝漂移。
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // amended\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "commit", "-q", "--amend", "-m", "amended delivery"); err != nil {
		t.Fatal(err)
	}
	d.reopen()
	if _, err := d.f.Drive(); err == nil || !strings.Contains(err.Error(), "does not match the promoted snapshot") {
		t.Fatalf("amended terminal checkpoint reached COMPLETE: %v", err)
	}
	if _, statErr := os.Stat(summaryPath); !os.IsNotExist(statErr) {
		t.Fatal("drifted terminal published the summary")
	}
	// 恢复已验证内容（reset --hard 晋升身份）后重放：COMPLETE 且摘要发布。
	if _, err := runner.Run(root, "git", "reset", "-q", "--hard", promoted); err != nil {
		t.Fatal(err)
	}
	run, err := d.f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "COMPLETE" || run.PromotedSnapshot != promoted {
		t.Fatalf("restored replay = %s promoted = %s, want COMPLETE %s", run.Status, run.PromotedSnapshot, promoted)
	}
	if _, err := os.Stat(summaryPath); err != nil {
		t.Fatalf("restored replay did not publish the summary: %v", err)
	}
}

// publicationDriftRunner 在 sealDelivered 核验完成后（merge-base 是其最后一
// 条 Git 命令）、终态发布基线 resolve 之前落一次宿主动作，其余命令直通内层
// runner——seal 窗口与发布窗口之间竞态的测试载体。
type publicationDriftRunner struct {
	inner  vcs.Runner
	land   func() error
	landed bool
}

func (r *publicationDriftRunner) Run(dir, command string, args ...string) (string, error) {
	if !r.landed && command == "git" && len(args) == 3 && args[0] == "merge-base" {
		r.landed = true
		if err := r.land(); err != nil {
			return "", err
		}
	}
	return r.inner.Run(dir, command, args...)
}

// TestTerminalPublicationRejectsDriftAfterSealVerification：sealDelivered 核
// 验通过与终态摘要发布之间的窗口内普通用户提交落地——已验证交付身份必须贯穿
// seal 窗口与发布窗口：Drive 以同一身份判据拒绝（不呈现 COMPLETE、摘要不落
// 盘）；恢复已验证内容（reset --hard 晋升身份）后重放发布 COMPLETE。
func TestTerminalPublicationRejectsDriftAfterSealVerification(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := vcs.ExecRunner{}
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	d := completePromotionPipeline(t, nil, root, identities[0])
	promoted, err := d.f.PrepareCandidate("organized final candidate")
	if err != nil {
		t.Fatal(err)
	}
	// 用户提交落在 seal 核验之后、发布基线 resolve 之前。
	drift := &publicationDriftRunner{inner: vcs.ExecRunner{}}
	drift.land = func() error {
		if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // parallel user work\n"), 0o600); err != nil {
			return err
		}
		if _, err := runner.Run(root, "git", "add", "app.go"); err != nil {
			return err
		}
		_, err := runner.Run(root, "git", "commit", "-q", "-m", "parallel user work")
		return err
	}
	d.f.vcs = vcs.New(root, drift)
	run, err := d.f.Drive()
	if err == nil || !strings.Contains(err.Error(), "does not match the promoted snapshot") {
		t.Fatalf("publication adopted a head the seal window did not verify: run=%s err=%v", run.Status, err)
	}
	if run.Status == "COMPLETE" {
		t.Fatal("drifted publication returned COMPLETE")
	}
	if _, statErr := os.Stat(filepath.Join(d.f.stateDir, "terminal-summary.json")); !os.IsNotExist(statErr) {
		t.Fatal("drifted publication wrote the terminal summary")
	}
	if !drift.landed {
		t.Fatal("premise lost: the drift window was not reached")
	}
	// 恢复已验证内容后重放：COMPLETE 且摘要发布。
	if _, err := runner.Run(root, "git", "reset", "-q", "--hard", promoted); err != nil {
		t.Fatal(err)
	}
	run, err = d.f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "COMPLETE" || run.PromotedSnapshot != promoted {
		t.Fatalf("restored replay = %s promoted = %s, want COMPLETE %s", run.Status, run.PromotedSnapshot, promoted)
	}
	if _, err := os.Stat(filepath.Join(d.f.stateDir, "terminal-summary.json")); err != nil {
		t.Fatalf("restored replay did not publish the summary: %v", err)
	}
}

// promotionWindowObserver 是晋升窗口双执行流的观察者：在窗口内的 write-tree
// （内容前置门——此时晋升锁已持有）处放出窗口已开信号，供需求变化执行流开
// 始提交；并按接纳时序分窗计数 reset/commit 写命令（git 命令只由 PrepareCandidate
// 所在线程发出，计数与标志不存在跨线程字段访问）。
type promotionWindowObserver struct {
	inner                vcs.Runner
	windowOpened         chan struct{}
	admitted             chan struct{}
	windowSignalled      bool
	writes               int
	writesAfterAdmission int
}

func (r *promotionWindowObserver) Run(dir, command string, args ...string) (string, error) {
	if !r.windowSignalled && command == "git" && len(args) == 1 && args[0] == "write-tree" {
		r.windowSignalled = true
		close(r.windowOpened)
	}
	if command == "git" && len(args) > 0 && (args[0] == "reset" || args[0] == "commit") {
		r.writes++
		select {
		case <-r.admitted:
			r.writesAfterAdmission++
		default:
		}
	}
	return r.inner.Run(dir, command, args...)
}

// TestRequirementChangeAdmissionWaitsForThePromotionWindow：屏障与晋升执行窗
// 互斥的双执行流形态——需求变化 REQUEST 在窗口内（内容前置门处）经独立执行
// 流提交，接纳等待至窗口关闭：PrepareCandidate 正常成功（若互斥失效，Record
// 会被屏障拒绝或 squash 落在接纳之后，两处断言任一捕获）、晋升绑定在接纳后
// 存活、接纳之后零个 reset/commit 写命令。窗口内的同步提交会等待调用方自身
// 持有的锁（死锁），屏障竞争只能以双执行流表示。
func TestRequirementChangeAdmissionWaitsForThePromotionWindow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	observer := &promotionWindowObserver{inner: vcs.ExecRunner{}, windowOpened: make(chan struct{}), admitted: make(chan struct{})}
	d := completePromotionPipeline(t, nil, root, base)
	prepareApprovedPromotionPrefix(t, d, "organized final candidate")
	d.f.vcsRunner, d.f.vcs = observer, vcs.New(root, observer)
	head, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	expectedCount, err := vcs.New(root, nil).CommitCount(base, d.run.Validation.Candidate.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := vcs.New(root, nil).CommitCount(base, head); err != nil || count != expectedCount {
		t.Fatalf("premise lost: interval count = %d, %v", count, err)
	}
	request, err := protocol.NewRequestEvent(protocol.EventID("window-barrier"), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Revise"}, protocol.AskOption{ID: "abort", Label: "Keep"})
	if err != nil {
		t.Fatal(err)
	}
	type admission struct {
		acceptance protocol.Acceptance
		err        error
	}
	admitted := make(chan admission, 1)
	go func() {
		<-observer.windowOpened
		acceptance, _, err := d.f.Submit(request)
		close(observer.admitted)
		admitted <- admission{acceptance: acceptance, err: err}
	}()
	promoted, err := d.f.PrepareCandidate("organized under a waiting barrier")
	if err != nil {
		t.Fatalf("promotion with a barrier waiting on the window: %v", err)
	}
	result := <-admitted
	if result.err != nil {
		t.Fatalf("requirement change admission after the window: %v", result.err)
	}
	if result.acceptance.EventID != string(request.ID) || result.acceptance.Kind != string(protocol.KindRequestControl) {
		t.Fatalf("barrier acceptance = %+v", result.acceptance)
	}
	// 接纳之后零个新 Git 写：组织（reset＋commit 各一）全部发生在窗口内。
	if observer.writes != 2 || observer.writesAfterAdmission != 0 {
		t.Fatalf("git writes = %d total, %d after admission; want 2 before admission and none after", observer.writes, observer.writesAfterAdmission)
	}
	if promoted == head {
		t.Fatal("promotion kept the pre-squash identity")
	}
	if count, err := vcs.New(root, nil).CommitCount(base, promoted); err != nil || count != 1 {
		t.Fatalf("post-window interval count = %d, %v", count, err)
	}
	verifiedTree := d.run.Validation.Candidate.Digest
	promotedTree, err := vcs.New(root, nil).TreeDigest(promoted)
	if err != nil {
		t.Fatal(err)
	}
	if promotedTree != verifiedTree {
		t.Fatalf("promotion changed the final tree: %s -> %s", verifiedTree, promotedTree)
	}
	if err := vcs.New(root, nil).VerifyReady(); err != nil {
		t.Fatalf("promotion left pending changes: %v", err)
	}
	// 屏障作用于已完成晋升：绑定在接纳后存活、意图已消费、屏障 Ask 在账。
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.PromotedSnapshot != promoted || snap.State.PromotedDigest != promotedTree {
		t.Fatalf("barrier admission disturbed the recorded promotion: promoted=%q digest=%q", snap.State.PromotedSnapshot, snap.State.PromotedDigest)
	}
	if pendingPromotionSource(snap.State) != "" {
		t.Fatalf("window kept a pending promotion intent: %q", pendingPromotionSource(snap.State))
	}
	barrierAsk := false
	for _, ask := range snap.State.PendingAsks {
		if !ask.Resolved && ask.Control == protocol.ControlRequestRequirementChange {
			barrierAsk = true
		}
	}
	if !barrierAsk {
		t.Fatal("requirement change admission did not land after the window")
	}
}

// An accepted Abort Ask precedes the promotion window. Its decision waits for
// that window and must use the freshness token after the intervening receipts.
func TestAbortDecisionAdmissionWaitsForThePromotionWindow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	observer := &promotionWindowObserver{inner: vcs.ExecRunner{}, windowOpened: make(chan struct{}), admitted: make(chan struct{})}
	d := completePromotionPipeline(t, nil, root, base)
	prepareApprovedPromotionPrefix(t, d, "organized final candidate")
	d.f.vcsRunner, d.f.vcs = observer, vcs.New(root, observer)
	head, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	expectedCount, err := vcs.New(root, nil).CommitCount(base, d.run.Validation.Candidate.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := vcs.New(root, nil).CommitCount(base, head); err != nil || count != expectedCount {
		t.Fatalf("premise lost: interval count = %d, %v", count, err)
	}
	request, err := protocol.NewRequestEvent(protocol.EventID("window-abort"), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop run"})
	if err != nil {
		t.Fatal(err)
	}
	requestAcceptance, _, err := d.f.Submit(request)
	if err != nil {
		t.Fatal(err)
	}
	type abortAdmission struct {
		acceptance protocol.Acceptance
		staleRetry bool
		err        error
	}
	decided := make(chan abortAdmission, 1)
	go func() {
		<-observer.windowOpened
		acceptance := requestAcceptance
		decide, err := protocol.NewDecideEvent(protocol.EventID("window-abort-decide"), protocol.RequestID(request.ID), acceptance.FreshnessToken, "abort")
		if err != nil {
			close(observer.admitted)
			decided <- abortAdmission{err: err}
			return
		}
		_, _, err = d.f.Submit(decide)
		staleRetry := false
		var rejected *protocol.RejectedError
		if errors.As(err, &rejected) && rejected.Code == protocol.CodeStaleFreshness {
			staleRetry = true
			var token string
			if token, err = d.f.Freshness(string(request.ID)); err == nil {
				if decide, err = protocol.NewDecideEvent(protocol.EventID("window-abort-retry"), protocol.RequestID(request.ID), token, "abort"); err == nil {
					acceptance, _, err = d.f.Submit(decide)
				}
			}
		}
		close(observer.admitted)
		decided <- abortAdmission{acceptance: acceptance, staleRetry: staleRetry, err: err}
	}()
	promoted, err := d.f.PrepareCandidate("organized under a waiting abort decision")
	if err != nil {
		t.Fatalf("promotion with an abort decision waiting on the window: %v", err)
	}
	result := <-decided
	if result.err != nil {
		t.Fatalf("abort decision admission after the window: %v", result.err)
	}
	if !result.staleRetry {
		t.Fatal("the waiting decide was not superseded by the window's promotion commits")
	}
	if result.acceptance.RequestID != string(request.ID) || result.acceptance.Kind != string(protocol.KindDecide) {
		t.Fatalf("abort acceptance = %+v", result.acceptance)
	}
	// 接纳之后零个新 Git 写：组织（reset＋commit 各一）全部发生在窗口内。
	if observer.writes != 2 || observer.writesAfterAdmission != 0 {
		t.Fatalf("git writes = %d total, %d after admission; want 2 before admission and none after", observer.writes, observer.writesAfterAdmission)
	}
	if promoted == head {
		t.Fatal("promotion kept the pre-squash identity")
	}
	if count, err := vcs.New(root, nil).CommitCount(base, promoted); err != nil || count != 1 {
		t.Fatalf("post-window interval count = %d, %v", count, err)
	}
	// abort 作用于已完成晋升：绑定在接纳后存活、意图已消费、决定在账。
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.PromotedSnapshot != promoted || pendingPromotionSource(snap.State) != "" {
		t.Fatalf("abort admission disturbed the recorded promotion: promoted=%q pending=%q", snap.State.PromotedSnapshot, pendingPromotionSource(snap.State))
	}
	if _, aborted := snap.State.AbortDecision(); !aborted {
		t.Fatal("abort decision was not recorded")
	}
	d.drive()
	if d.run.Status != "ABORTED" || d.run.PromotedSnapshot != promoted {
		t.Fatalf("post-admission terminal = %s promoted = %s, want ABORTED %s", d.run.Status, d.run.PromotedSnapshot, promoted)
	}
}

// TestPromotionFrozenByAbortDecisionLeavesHistoryUntouched：abort 决定先于晋升
// 窗口落账，真实终止与清理完成后晋升在意图准入处拒绝：零 Git 写、
// 多提交区间原样、拒绝可辨识。
func TestPromotionFrozenByAbortDecisionLeavesHistoryUntouched(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	observer := &promotionWindowObserver{inner: vcs.ExecRunner{}, windowOpened: make(chan struct{}), admitted: make(chan struct{})}
	d := completePromotionPipeline(t, nil, root, base)
	d.f.vcs = vcs.New(root, observer)
	head, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop run"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	token := ""
	for _, ask := range d.run.PendingAsks {
		if ask.RequestID == string(request.ID) {
			token = ask.Token
		}
	}
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(request.ID), Token: token, Choice: "abort"}})
	d.completeWorkerTerminations()
	if err := d.f.engine.BeginCleanup(protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	settleAbortForPublication(t, d)
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, aborted := snap.State.AbortDecision(); !aborted || snap.State.Phase != runtime.PhaseTerminal {
		t.Fatalf("premise lost: aborted=%t phase=%s", aborted, snap.State.Phase)
	}
	if _, err := d.f.PrepareCandidate("organized after an abort decision"); err == nil || !strings.Contains(err.Error(), "requires an active run") {
		t.Fatalf("promotion after an abort decision: %v", err)
	}
	// 零 Git 副作用：HEAD 与区间提交数逐字节不变（多提交未被动过）。
	if observer.writes != 0 {
		t.Fatalf("rejected promotion issued %d git writes", observer.writes)
	}
	after, err := vcs.New(root, nil).Resolve()
	if err != nil || after != head {
		t.Fatalf("rejected promotion moved HEAD: %s -> %s (%v)", head, after, err)
	}
	if count, err := vcs.New(root, nil).CommitCount(base, after); err != nil || count != 2 {
		t.Fatalf("rejected promotion rewrote history: count = %d, %v", count, err)
	}
}

// TestPromotionResumeAfterResetInterruptionBindsOrganizedHead：squash 的 reset
// 已完成、commit 未执行时中断（soft reset 保树保 index）——意图存活期间重入
// PrepareCandidate 识别自己的中间态（暂存树＝已验证候选）并以 commit 完成组
// 织：意图消费为空、晋升身份＝组织后 HEAD、内容摘要与候选绑定、工作区干
// 净，终态可达。
func TestPromotionResumeAfterResetInterruptionBindsOrganizedHead(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := vcs.ExecRunner{}
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	d := completePromotionPipeline(t, nil, root, base)
	if err := d.f.engine.OpenPromotion(promotionInputForTest(t, d, d.run.Validation.Candidate.Identity, "organized final candidate"), protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	// squash 第一步（reset --soft base）已执行、commit 未执行——组织中断的
	// 中间态。
	prepareApprovedPromotionPrefix(t, d, "organized final candidate")
	if _, err := runner.Run(root, "git", "reset", "-q", "--soft", base); err != nil {
		t.Fatal(err)
	}
	promoted, err := d.f.PrepareCandidate("resumed organization")
	if err != nil {
		t.Fatalf("interrupted promotion resume: %v", err)
	}
	organized, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if promoted != organized {
		t.Fatalf("resumed promotion = %s, want the organized head %s", promoted, organized)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pendingPromotionSource(snap.State) != "" || snap.State.PromotedSnapshot != organized {
		t.Fatalf("resumed promotion did not bind: pending=%q promoted=%q", pendingPromotionSource(snap.State), snap.State.PromotedSnapshot)
	}
	digest, err := vcs.New(root, nil).TreeDigest(organized)
	if err != nil {
		t.Fatal(err)
	}
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if digest != wave.Candidate.Digest || snap.State.PromotedDigest != digest {
		t.Fatalf("organized tree %s does not bind the verified candidate %s (promoted digest %s)", digest, wave.Candidate.Digest, snap.State.PromotedDigest)
	}
	if err := vcs.New(root, nil).VerifyReady(); err != nil {
		t.Fatalf("resumed promotion left the worktree dirty: %v", err)
	}
	if count, err := vcs.New(root, nil).CommitCount(base, organized); err != nil || count != 1 {
		t.Fatalf("resumed organization interval = %d, %v", count, err)
	}
	run, err := d.f.Drive()
	if err != nil || run.Status != "COMPLETE" {
		t.Fatalf("post-resume terminal = %s, %v", run.Status, err)
	}
}

// commitFailureRunner returns one classified EINTR before the selected commit.
// The preceding native writes and every other command use the real runner.
type commitFailureRunner struct {
	inner   vcs.Runner
	message string
	failed  bool
}

func (r *commitFailureRunner) Run(dir, command string, args ...string) (string, error) {
	if !r.failed && command == "git" && len(args) == 3 && args[0] == "commit" && args[1] == "-m" && args[2] == r.message {
		r.failed = true
		return "", fmt.Errorf("injected commit interruption: %w", syscall.EINTR)
	}
	return r.inner.Run(dir, command, args...)
}

// TestPromotionResumeAfterCommitFailureBindsOrganizedHead：squash 的 commit 命
// 令执行失败＝窗口中断（意图存活、reset 已完成、commit 未执行）。重入按持久
// 事实对账：纯暂存中间态（前置门已核暂存树＝候选）以一次 commit 完成组织并
// 绑定原意图——无补偿写（组织头建立在 base 上、HEAD 不是意图目标）、意图消
// 费为空、树摘要绑定、终态可达。
func TestPromotionResumeAfterCommitFailureBindsOrganizedHead(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	crashed := &commitFailureRunner{inner: vcs.ExecRunner{}, message: "interrupted organization"}
	d := completePromotionPipeline(t, nil, root, base)
	prepareApprovedPromotionPrefix(t, d, crashed.message)
	d.f.vcs = vcs.New(root, crashed)
	if _, err := d.f.PrepareCandidate("interrupted organization"); err == nil || !strings.Contains(err.Error(), "cannot squash") {
		t.Fatalf("injected commit failure did not interrupt the window: %v", err)
	}
	// 中断中间态：HEAD 回到 base、未提交内容恰为纯暂存形态、意图存活。
	if head, err := vcs.New(root, nil).Resolve(); err != nil || head != base {
		t.Fatalf("interrupted head = %s, want base %s (%v)", head, base, err)
	}
	stagedOnly, err := vcs.New(root, nil).StagedOnly()
	if err != nil || !stagedOnly {
		t.Fatalf("interrupted midstate staged-only = %t (%v)", stagedOnly, err)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pendingPromotionSource(snap.State) == "" {
		t.Fatal("interrupted window lost the promotion intent")
	}
	// 重入（重开后生产 exec runner）：完成组织并绑定原意图。
	d.reopen()
	promoted, err := d.f.PrepareCandidate("interrupted organization")
	if err != nil {
		t.Fatalf("resumed promotion: %v", err)
	}
	organized, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if promoted != organized || organized == base || organized == identities[len(identities)-1] {
		t.Fatalf("resumed promotion = %s (head %s, base %s, intent %s)", promoted, organized, base, identities[len(identities)-1])
	}
	if count, err := vcs.New(root, nil).CommitCount(base, organized); err != nil || count != 1 {
		t.Fatalf("resumed organization interval = %d, %v", count, err)
	}
	snap, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pendingPromotionSource(snap.State) != "" || snap.State.PromotedSnapshot != organized {
		t.Fatalf("resumed promotion did not bind the original intent: pending=%q promoted=%q", pendingPromotionSource(snap.State), snap.State.PromotedSnapshot)
	}
	digest, err := vcs.New(root, nil).TreeDigest(organized)
	if err != nil {
		t.Fatal(err)
	}
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if digest != wave.Candidate.Digest || snap.State.PromotedDigest != digest {
		t.Fatalf("organized tree %s does not bind the verified candidate %s (promoted digest %s)", digest, wave.Candidate.Digest, snap.State.PromotedDigest)
	}
	if err := vcs.New(root, nil).VerifyReady(); err != nil {
		t.Fatalf("resumed promotion left the worktree dirty: %v", err)
	}
	run, err := d.f.Drive()
	if err != nil || run.Status != "COMPLETE" {
		t.Fatalf("post-resume terminal = %s, %v", run.Status, err)
	}
}

// TestPromotionRejectsUserWorkspaceChangesUntilCleaned：中断中间态上叠加用户
// 未暂存修改＝不是本操作的中间态：重入拒绝（指明先处理工作区）、HEAD 不动、
// 用户修改原样保留；用户清理后重入完成组织。
func TestPromotionRejectsUserWorkspaceChangesUntilCleaned(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := vcs.ExecRunner{}
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	d := completePromotionPipeline(t, nil, root, base)
	head, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	prepareApprovedPromotionPrefix(t, d, "organized final candidate")
	if _, err := runner.Run(root, "git", "reset", "-q", "--soft", base); err != nil {
		t.Fatal(err)
	}
	// 用户在中断中间态上新增未暂存修改。
	userEdit := "package main // user edit\n"
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte(userEdit), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.f.PrepareCandidate("resumed organization"); err == nil || !strings.Contains(err.Error(), "changes it did not stage") {
		t.Fatalf("user-dirty midstate promotion: %v", err)
	}
	if after, err := vcs.New(root, nil).Resolve(); err != nil || after != base {
		t.Fatalf("rejected resume moved HEAD: %s -> %s (%v)", base, after, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "app.go"))
	if err != nil || string(data) != userEdit {
		t.Fatalf("rejected resume touched the user change: %q (%v)", data, err)
	}
	if stagedOnly, err := vcs.New(root, nil).StagedOnly(); err != nil || stagedOnly {
		t.Fatalf("user-dirty midstate classified staged-only = %t (%v)", stagedOnly, err)
	}
	// 用户处理（丢弃未暂存修改回到纯暂存中间态）后重入成功。
	if _, err := runner.Run(root, "git", "checkout", "--", "app.go"); err != nil {
		t.Fatal(err)
	}
	promoted, err := d.f.PrepareCandidate("resumed organization")
	if err != nil {
		t.Fatalf("cleaned re-entry promotion: %v", err)
	}
	organized, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if promoted != organized || organized == base || organized == head {
		t.Fatalf("cleaned re-entry = %s (head %s, base %s, intent %s)", promoted, organized, base, head)
	}
	if count, err := vcs.New(root, nil).CommitCount(base, organized); err != nil || count != 1 {
		t.Fatalf("cleaned re-entry interval = %d, %v", count, err)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pendingPromotionSource(snap.State) != "" || snap.State.PromotedSnapshot != organized {
		t.Fatalf("cleaned re-entry did not bind: pending=%q promoted=%q", pendingPromotionSource(snap.State), snap.State.PromotedSnapshot)
	}
	run, err := d.f.Drive()
	if err != nil || run.Status != "COMPLETE" {
		t.Fatalf("post-clean terminal = %s, %v", run.Status, err)
	}
}

// TestPromotionBindsParkedOrganizationAfterBarrierRelease：晋升窗口在组织完成
// 后、Record 前中断（意图存活、历史已组织）；随后需求变化屏障到达撤销意图
// （组织形态驻留、原观察债务保留），屏障解除后重入——原意图经 count==1 无 squash 直绑已
// 组织形态（§9.2「外部事实已精确满足预期时只提交结果」）。
func TestPromotionBindsParkedOrganizationAfterBarrierRelease(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := vcs.ExecRunner{}
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	d := completePromotionPipeline(t, nil, root, base)
	head, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.f.engine.OpenPromotion(promotionInputForTest(t, d, d.run.Validation.Candidate.Identity, "organized final candidate"), protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	// 窗口中断已越过组织：reset --soft＋commit 已完成、Record 未执行。
	prepareApprovedPromotionPrefix(t, d, "organized final candidate")
	if _, err := runner.Run(root, "git", "reset", "-q", "--soft", base); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "commit", "-q", "-m", "organized final candidate"); err != nil {
		t.Fatal(err)
	}
	organized, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if organized == head || organized == base {
		t.Fatalf("premise lost: parked organization identity = %s", organized)
	}
	// 屏障到达：REQUEST 撤销写资格，保留驻留债务；随后解除（Keep 需求）。
	request := holdRequirement(t, d)
	releaseRequirement(t, d, request)
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pendingPromotionSource(snap.State) != d.run.Validation.Candidate.Identity || snap.State.PromotedSnapshot != "" {
		t.Fatalf("barrier release lost original native responsibility: pending=%q promoted=%q", pendingPromotionSource(snap.State), snap.State.PromotedSnapshot)
	}
	promoted, err := d.f.PrepareCandidate("re-bound organization")
	if err != nil {
		t.Fatalf("parked organization re-bind: %v", err)
	}
	if promoted != organized {
		t.Fatalf("re-bind reorganized history: %s -> %s", organized, promoted)
	}
	snap, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pendingPromotionSource(snap.State) != "" || snap.State.PromotedSnapshot != organized {
		t.Fatalf("re-bind did not bind the parked form: pending=%q promoted=%q", pendingPromotionSource(snap.State), snap.State.PromotedSnapshot)
	}
	digest, err := vcs.New(root, nil).TreeDigest(organized)
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.PromotedDigest != digest {
		t.Fatalf("re-bind digest = %s, want the organized tree %s", snap.State.PromotedDigest, digest)
	}
	run, err := d.f.Drive()
	if err != nil || run.Status != "COMPLETE" || run.PromotedSnapshot != organized {
		t.Fatalf("post-rebind terminal = %s promoted = %s, want COMPLETE %s (%v)", run.Status, run.PromotedSnapshot, organized, err)
	}
}

// TestPromotionContentDriftWithLiveIntentRejectsBeforeGitWrites：意图存活期间
// HEAD 内容漂移（净树≠已验证候选）时重入——内容前置门先行拒绝：零 Git 副作
// 用（HEAD 不动）且不重开、不覆盖未决意图。
func TestPromotionContentDriftWithLiveIntentRejectsBeforeGitWrites(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	d := completePromotionPipeline(t, nil, root, base)
	source := d.run.Validation.Candidate.Identity
	if err := d.f.engine.OpenPromotion(promotionInputForTest(t, d, d.run.Validation.Candidate.Identity, "organized final candidate"), protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	// 内容漂移：区间累积改写内容的新提交（工作区干净）。
	drifted := snapshotCommitOne(t, root, "drift work")
	if _, err := d.f.PrepareCandidate("re-organized candidate"); err == nil || !strings.Contains(err.Error(), "does not match the verified final candidate") {
		t.Fatalf("drifted re-entry promotion: %v", err)
	}
	after, err := vcs.New(root, nil).Resolve()
	if err != nil || after != drifted {
		t.Fatalf("rejected re-entry moved HEAD: %s -> %s (%v)", drifted, after, err)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pendingPromotionSource(snap.State) != source {
		t.Fatalf("rejected re-entry reopened the intent: %q, want the original source %q", pendingPromotionSource(snap.State), source)
	}
}

func pendingPromotionSource(s *protocol.State) string {
	intent, ok := s.PromotionIntent()
	if !ok {
		return ""
	}
	var p protocol.CandidateParameters
	if err := decodeNativeParams(intent, &p); err != nil {
		panic(err)
	}
	return p.Promotion.Source
}
func promotionInputForTest(t *testing.T, d *validationDriver, sourceIdentity, message string) protocol.PromotionParameters {
	t.Helper()
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	observed, err := d.f.observePromotion()
	if err != nil {
		t.Fatal(err)
	}
	source := snap.State.Validation.Waves[len(snap.State.Validation.Waves)-1].Candidate
	if source.Identity != sourceIdentity {
		t.Fatalf("fixture source identity %s != verified %s", sourceIdentity, source.Identity)
	}
	workspace := ""
	for _, resource := range snap.State.NativeResources {
		if resource.Role == "candidate" && resource.Identity == source.Identity && resource.Status == "READY" {
			workspace = resource.ID
			break
		}
	}
	return protocol.PromotionParameters{SourceWorkspace: workspace, Source: source.Identity, SourceDigest: source.Digest, TargetRoot: d.f.root, TargetBase: snap.State.BaseSnapshot, TargetStart: observed, Message: message}
}

// Completed native effects retain their original receipts across new source
// qualification. Late and repeated receipts cannot change the current mapping.
func TestPromotionReceiptsRetainEvidenceAfterSourceReplacement(t *testing.T) {
	t.Parallel()
	d, first, originalProof := completedPromotionAwaitingReceipt(t)
	if err := d.f.engine.RecordCandidatePromotion(first.ActionID, originalProof, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	requalifyPromotionSource(t, d, "replacement", 2)
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	source := *snapshot.State.Validation.Candidate
	if !snapshot.State.FinalValidationComplete(d.f.definition) {
		t.Fatal("replacement source is not fully qualified")
	}
	path := snapshot.State.RequirementArtifactPath()
	document, err := os.ReadFile(filepath.Join(d.f.root, path))
	if err != nil {
		t.Fatal(err)
	}
	candidateDocument, err := (vcs.ExecRunner{}).RunRaw(d.f.root, "git", nil, "show", source.Identity+":"+path)
	if err != nil || requirementContentRevision(candidateDocument) != strings.TrimPrefix(snapshot.State.Validation.RequirementRevision, "sha256:") || string(candidateDocument) != string(document) {
		t.Fatalf("current candidate %s requirement revision=%s bytes=%q, confirmed revision=%s bytes=%q: %v", source.Identity, requirementContentRevision(candidateDocument), candidateDocument, snapshot.State.Validation.RequirementRevision, document, err)
	}
	originalApplication, err := os.ReadFile(filepath.Join(d.f.root, "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"unconfirmed document", "other dirty path"} {
		t.Run(invalid, func(t *testing.T) {
			if invalid == "unconfirmed document" {
				if err := os.WriteFile(filepath.Join(d.f.root, path), append(append([]byte(nil), document...), '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(d.f.root, "app.go"), []byte("package main // user input\n"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := d.f.observePromotion()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.f.Drive(); err == nil {
				t.Fatal("unapproved target input was delivered")
			}
			after, err := d.f.observePromotion()
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected target input changed native values: %+v %v", after, err)
			}
			state, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if _, pending := state.State.PromotionIntent(); pending {
				t.Fatal("unapproved target input opened a writer")
			}
			if err := os.WriteFile(filepath.Join(d.f.root, path), document, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d.f.root, "app.go"), originalApplication, 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
	run, err := d.f.Drive()
	if err != nil || run.Status != "COMPLETE" || run.Termination == nil || run.Termination.CandidatePromotion == nil {
		t.Fatalf("public delivery after confirmed development redo: %s %v", run.Status, err)
	}
	currentProof := *run.Termination.CandidatePromotion
	if currentProof.Source != source.Identity || currentProof.SourceDigest != source.Digest || currentProof.FinalDigest != source.Digest {
		t.Fatal("replacement delivery lost its complete validated source")
	}
	if count, err := d.f.vcs.CommitCount(currentProof.TargetBase, currentProof.Final); err != nil || count != 1 {
		t.Fatalf("final organization range: %d %v", count, err)
	}
	if err := d.f.vcs.VerifyReady(); err != nil {
		t.Fatal(err)
	}
	effect := run.Termination.ReconciledEffects[first.ActionID]
	if effect.Status != "FULFILLED" || effect.Intent == nil || !reflect.DeepEqual(*effect.Intent, first) || effect.UnknownReceipt == nil || effect.UnknownReceipt.Status != protocol.HostActionStatusUnknown {
		t.Fatal("replacement lost its original completion or UNKNOWN history")
	}
	evidence := protocol.CandidateEvidence{ParametersDigest: first.PayloadDigest, WorkspaceID: originalProof.SourceWorkspace, Identity: originalProof.Final, Digest: originalProof.FinalDigest, Promotion: &originalProof}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	event, err := protocol.NewAdapterHostActionReceiptEvent(protocol.EventID("native:"+first.ActionID), first.ActionID, "op.candidate", d.host(), first.Correlation, first.PayloadDigest, protocol.HostActionStatusExecuted, "", values)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, replay, err := d.f.Submit(event)
		if err != nil || replay.Termination == nil || !reflect.DeepEqual(*replay.Termination.CandidatePromotion, currentProof) || replay.PromotedSnapshot != currentProof.Final {
			t.Fatalf("late original receipt changed the delivered mapping: %v", err)
		}
	}
}

func completedPromotionAwaitingReceipt(t *testing.T) (*validationDriver, protocol.HostActionIntent, protocol.CandidatePromotionReceipt) {
	t.Helper()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "development", "review")
	base := identities[0]
	d := completePromotionPipeline(t, nil, root, base)
	prepareApprovedPromotionPrefix(t, d, "lost delivery")
	if _, err := (vcs.ExecRunner{}).Run(root, "git", "reset", "--soft", base); err != nil {
		t.Fatal(err)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	first, ok := snap.State.PromotionIntent()
	if !ok {
		t.Fatal("partial delivery lost its intent")
	}
	partial, err := d.f.observePromotion()
	if err != nil {
		t.Fatal(err)
	}
	if partial.Head != base || partial.Clean || !partial.StagedOnly || partial.IndexTree != snap.State.Validation.Candidate.Digest {
		t.Fatalf("partial native prefix: %+v", partial)
	}
	// Prepare the actual completion before recording the lost response. UNKNOWN
	// observation must not execute the remaining commit on the fixture's behalf.
	var firstParams protocol.CandidateParameters
	if err := decodeNativeParams(first, &firstParams); err != nil {
		t.Fatal(err)
	}
	if err := vcs.New(root, nil).Commit(firstParams.Promotion.Message); err != nil {
		t.Fatal(err)
	}
	unknown, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), first.ActionID, "op.candidate", d.host(), first.Correlation, first.PayloadDigest, protocol.HostActionStatusUnknown, "", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(unknown)
	snap, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	firstResult, complete, err := d.f.promotionEvidence(snap.State, first, false, new(bool))
	if err != nil || !complete {
		t.Fatalf("observe completed original delivery: %t %v", complete, err)
	}
	originalProof := *firstResult.Candidate.Promotion
	return d, first, originalProof
}

func requalifyPromotionSource(t *testing.T, d *validationDriver, message string, revision int) {
	t.Helper()
	// Qualify the new source through its signed workspace, candidate and check
	// responsibilities before promotion.
	advance := func() {
		for {
			progress, checking := func() (progress, checking bool) {
				unlock, err := d.f.acquireNativeWindow()
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
				if _, _, err := d.f.engine.Drive(protocol.Observation{}); err != nil {
					t.Fatal(err)
				}
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				for _, execute := range []bool{false, true} {
					for _, intent := range snapshot.State.PendingHostActions {
						if intent.Adapter == nil {
							continue
						}
						if intent.Adapter.Operation == "op.candidate" {
							var params protocol.CandidateParameters
							if err := decodeNativeParams(intent, &params); err != nil {
								t.Fatal(err)
							}
							if params.Stage == "PROMOTION" {
								continue
							}
						}
						checking = checking || intent.Adapter.Operation == "op.check" && snapshot.State.NativeActionWritable(intent, d.f.definition)
						progress, err = d.f.completeNativeIntent(snapshot.State, intent, execute)
						if err != nil {
							t.Fatal(err)
						}
						if progress {
							break
						}
					}
					if progress {
						break
					}
				}
				return
			}()
			if !progress && !checking {
				run, err := d.f.Show()
				if err != nil {
					t.Fatal(err)
				}
				d.run = run
				return
			}
			if !progress {
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
	var err error
	d.run, err = d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	var development string
	for _, work := range d.run.RequirementWork {
		for _, result := range d.run.Validation.Waves {
			for _, source := range result.SourceTasks {
				if source == work.Task {
					development = work.ActionID
				}
			}
		}
	}
	if development == "" {
		t.Fatal("confirmed plan omitted the original development duty")
	}
	confirmValidationDocument(d, development, nil, validationDocument+strings.Repeat("\n", revision))
	advance()
	id, input := d.action("engine.development.batch.v6")
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	workspace := snapshot.State.NativeResources[input.Binding.WorkspaceID]
	if workspace.Role != "development" || workspace.Status != "READY" {
		t.Fatalf("development source is not ready: %+v", workspace)
	}
	if err := os.WriteFile(filepath.Join(workspace.Path, "app.go"), []byte("package main // "+message+"\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "updated input behavior"}})
	advance()
	for id, input := range d.run.ValidationInputs {
		switch input.Handler {
		case "engine.qa.execution.v4":
			report := executionFor(input)
			tool, environment := protocol.ExecutionIdentities(input.ExecutionFacts)
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report, Tool: tool, Environment: environment})
		case "engine.gate.v3":
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
		}
	}
	d.run, err = d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	state, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !state.State.FinalValidationComplete(d.f.definition) {
		t.Fatalf("new current source did not complete FULL qualification: next=%+v", d.run.Next)
	}
}

// UNKNOWN with a real staged remainder permits observation, never another
// native execution. Public reentry preserves its original debt and receipt.
func TestUnknownPromotionPartialPrefixDoesNotExecuteOnReentry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "development", "validated output")
	d := completePromotionPipeline(t, nil, root, identities[0])
	prepareApprovedPromotionPrefix(t, d, "lost delivery")
	if _, err := (vcs.ExecRunner{}).Run(root, "git", "reset", "--soft", identities[0]); err != nil {
		t.Fatal(err)
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	intent, pending := snap.State.PromotionIntent()
	if !pending {
		t.Fatal("partial delivery has no signed responsibility")
	}
	if len(snap.State.HostActionFailures[intent.ActionID]) != 0 {
		t.Fatal("lost-response fixture has a known FAILED fact")
	}
	before, err := d.f.observePromotion()
	if err != nil {
		t.Fatal(err)
	}
	if before.Clean || !before.StagedOnly || before.Head != identities[0] || before.IndexTree != snap.State.Validation.Candidate.Digest {
		t.Fatalf("fixture did not establish an incomplete staged prefix: %+v", before)
	}
	unknown, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", d.host(), intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusUnknown, "", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(unknown)
	original, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := d.f.PrepareCandidate("lost delivery"); err == nil {
			t.Fatal("partial UNKNOWN was executed on public promotion reentry")
		}
	}
	if _, err := d.f.Drive(); err == nil || !strings.Contains(err.Error(), "candidate promotion remains incomplete") {
		t.Fatalf("UNKNOWN native responsibility was not blocked: %v", err)
	}
	after, err := d.f.observePromotion()
	if err != nil {
		t.Fatal(err)
	}
	current, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(current.State.PendingHostActions[intent.ActionID], intent) || !reflect.DeepEqual(current.State.HostActionReceipts[intent.ActionID], original.State.HostActionReceipts[intent.ActionID]) || current.State.DeliveryComplete(d.f.definition) {
		t.Fatal("UNKNOWN reentry changed native bytes, the original responsibility, receipt or delivery qualification")
	}

	confirmValidationDocument(d, "", nil, validationDocument+"\n\n")
	pendingInput, err := d.f.observePromotion()
	if err != nil {
		t.Fatal(err)
	}
	if pendingInput.Head != before.Head || pendingInput.Tree != before.Tree || pendingInput.IndexTree != before.IndexTree || pendingInput.Clean || pendingInput.StagedOnly {
		t.Fatalf("confirmed input did not preserve the incomplete native prefix: %+v", pendingInput)
	}
	for i := 0; i < 2; i++ {
		if _, err := d.f.Drive(); err == nil {
			t.Fatal("incomplete UNKNOWN advanced after requirement confirmation")
		}
	}
	after, err = d.f.observePromotion()
	if err != nil {
		t.Fatal(err)
	}
	current, err = d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, pendingInput) || !reflect.DeepEqual(current.State.PendingHostActions[intent.ActionID], intent) || !reflect.DeepEqual(current.State.HostActionReceipts[intent.ActionID], original.State.HostActionReceipts[intent.ActionID]) || current.State.CandidatePromotion != nil {
		t.Fatal("incomplete UNKNOWN became an execution or overwrote the confirmed input")
	}
}

func (r *publicationDriftRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	return r.inner.RunRaw(dir, command, input, args...)
}

func (r *promotionWindowObserver) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	return r.inner.RunRaw(dir, command, input, args...)
}

func (r *commitFailureRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	return r.inner.RunRaw(dir, command, input, args...)
}

// An immutable delivery is reconciled through public Drive without replacing
// either form of the newly confirmed document with the historical value.
func TestCompletedPromotionReconcilesAfterRequirementConfirmation(t *testing.T) {
	t.Parallel()
	for _, stage := range []bool{false, true} {
		t.Run(fmt.Sprintf("staged=%t", stage), func(t *testing.T) {
			d, first, originalProof := completedPromotionAwaitingReceipt(t)
			var development string
			for _, work := range d.run.RequirementWork {
				for _, wave := range d.run.Validation.Waves {
					for _, source := range wave.SourceTasks {
						if source == work.Task {
							development = work.ActionID
						}
					}
				}
			}
			if development == "" {
				t.Fatal("no original development duty")
			}
			confirmValidationDocument(d, development, nil, validationDocument+"\n\n")
			if stage {
				if err := d.f.vcs.Track([]string{"requirements.md"}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := d.f.observePromotion()
			if err != nil {
				t.Fatal(err)
			}
			if before.Clean || before.StagedOnly != stage || before.Head != originalProof.Final {
				t.Fatalf("missing normal dirty requirement premise: %+v", before)
			}
			_, err = d.f.Drive()
			if err != nil {
				t.Fatalf("public drive after confirmed requirement: %v", err)
			}
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if _, pending := after.State.PendingHostActions[first.ActionID]; pending {
				t.Fatal("completed original promotion still unresolved")
			}
			if after.State.ReconciledEffects[first.ActionID].Status != "FULFILLED" || after.State.ReconciledEffects[first.ActionID].UnknownReceipt == nil {
				t.Fatal("original completion or UNKNOWN evidence lost")
			}
			if !reflect.DeepEqual(*after.State.CandidatePromotion, originalProof) {
				t.Fatal("original receipt differs from immutable completion")
			}
			if current, err := d.f.observePromotion(); err != nil || !reflect.DeepEqual(current, before) {
				t.Fatalf("observation wrote the user's next input: %+v %v", current, err)
			}
			for i := 0; i < 2; i++ {
				if err := d.f.engine.RecordCandidatePromotion(first.ActionID, originalProof, protocol.Observation{}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := d.f.Drive(); err != nil {
				t.Fatal(err)
			}
			if current, err := d.f.observePromotion(); err != nil || !reflect.DeepEqual(current, before) {
				t.Fatalf("replay wrote the user's next input: %+v %v", current, err)
			}

		})
	}
}
