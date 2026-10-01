package facade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
)

// 本文件是真 git 仓上的快照公开链：regular Start 的 base 校验、终态多 commit
// 拒绝与晋升（含 squash）、QA 隔离工作区登记与清空转换。全部走生产 exec
// runner，无模型调用。

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
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".gates/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "--all")
	messages := append([]string{"base"}, commits...)
	identities := make([]string, 0, len(messages))
	for i, message := range messages {
		if i > 0 {
			if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // "+message+"\n"), 0o600); err != nil {
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

// snapshotCommitOne 在已建仓库上追加一个提交并返回新身份。
func snapshotCommitOne(t *testing.T, root, message string) string {
	t.Helper()
	runner := vcs.ExecRunner{}
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // "+message+"\n"), 0o600); err != nil {
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
	}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "candidate"}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestRegularStartAcceptsAncestorBaseAndRejectsDivergent：regular Start 把
// BaseSnapshot 变成事实——合法（可复现且为 HEAD 祖先或相等）接纳并规范化为小
// 写；分叉身份与非法形态可区分拒绝。
func TestRegularStartAcceptsAncestorBaseAndRejectsDivergent(t *testing.T) {
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
	if _, err := runner.Run(root, "git", "checkout", "-q", "master"); err != nil {
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
			}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "candidate"}})
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
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "dev commit", "review commit")
	base := identities[0]
	runner := vcs.ExecRunner{}
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	d.reviewReady()
	qa, qi := d.action("engine.qa.execution")
	gate, gi := d.action("engine.gate")
	report := executionFor(qi)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	d.drive()
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if wave.Status != "COMPLETE" {
		t.Fatalf("wave status = %s, want COMPLETE", wave.Status)
	}
	// 晋升等待在位：终态不可达。
	pending, err := d.f.Drive()
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
	if _, err := d.f.PrepareCandidate(""); err == nil || !strings.Contains(err.Error(), "squash message") {
		t.Fatalf("multi-commit promotion without message: %v", err)
	}
	promoted, err := d.f.PrepareCandidate("organized final candidate")
	if err != nil {
		t.Fatal(err)
	}
	if promoted == identities[len(identities)-1] {
		t.Fatal("squash kept the pre-squash identity")
	}
	if count, err := vcs.New(root, nil).CommitCount(base, promoted); err != nil || count != 1 {
		t.Fatalf("post-squash count = %d, %v", count, err)
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
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.Phase != runtime.PhaseTerminal {
		t.Fatalf("engine phase = %s, want TERMINAL", snap.State.Phase)
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
	// 晋升后新增提交：终态边界最后防线拒绝（多 commit）。
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // late\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, "git", "commit", "-q", "-m", "late commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.f.terminalSnapshotGuard(run); err == nil || !strings.Contains(err.Error(), "organize the final candidate") {
		t.Fatalf("post-promotion commit escaped the terminal guard: %v", err)
	}
}

// TestCandidatePromotionNoOpConfirmsSingleCommit：区间单提交时晋升为确认当前
// HEAD（身份不变），等待解除。
func TestCandidatePromotionNoOpConfirmsSingleCommit(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	d.reviewReady()
	qa, qi := d.action("engine.qa.execution")
	gate, gi := d.action("engine.gate")
	report := executionFor(qi)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	d.drive()
	head, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if head != base {
		t.Fatalf("repo head %s moved from base %s before promotion", head, base)
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

// TestPrepareCandidateRejectsUncommittedChanges：晋升要求工作区就绪（镜像
// legacy verifySnapshotReady 语义：被跟踪修改与未跟踪交付文件都拒绝）。
func TestPrepareCandidateRejectsUncommittedChanges(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root, "dev commit")[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.f.PrepareCandidate("message"); err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") {
		t.Fatalf("dirty worktree promotion: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.f.PrepareCandidate("message"); err == nil {
		t.Fatal("untracked deliverable accepted by promotion")
	}
}

// TestRegisterQAWorktreeValidatesIdentityAndArtifacts：登记制——工作区原生身份
// 必须等于 BaseSnapshot、隔离区内需求产物哈希必须等于登记修订；登记事实进入
// 公开投影。
func TestRegisterQAWorktreeValidatesIdentityAndArtifacts(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	runner := vcs.ExecRunner{}
	worktree := filepath.Join(t.TempDir(), "qa-worktree")
	if _, err := runner.Run(root, "git", "worktree", "add", "--detach", worktree, base); err != nil {
		t.Fatal(err)
	}
	d := startValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil)
	if err := d.f.RegisterQAWorktree(worktree); err != nil {
		t.Fatalf("register: %v", err)
	}
	run, err := d.f.Status()
	if err != nil {
		t.Fatal(err)
	}
	if run.QAWorktree != worktree {
		t.Fatalf("registered worktree = %q, want %q", run.QAWorktree, worktree)
	}
	// 快照不符：工作区检出到另一提交（新增提交后 HEAD≠base）。
	snapshotCommitOne(t, root, "move on")
	drifted := filepath.Join(t.TempDir(), "drifted-worktree")
	if _, err := runner.Run(root, "git", "worktree", "add", "--detach", drifted, "HEAD"); err != nil {
		t.Fatal(err)
	}
	if err := d.f.RegisterQAWorktree(drifted); err == nil || !strings.Contains(err.Error(), "does not match the base snapshot") {
		t.Fatalf("drifted worktree registration: %v", err)
	}
	// 产物修订不符：工作区在 base 上但需求文档被改写（工作树状态）。
	if err := os.WriteFile(filepath.Join(worktree, "requirements.md"), []byte("# rewritten\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.f.RegisterQAWorktree(worktree); err == nil || !strings.Contains(err.Error(), "does not match the run revision") {
		t.Fatalf("artifact drift registration: %v", err)
	}
	// 空路径拒绝。
	if err := d.f.RegisterQAWorktree("  "); err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("empty worktree registration: %v", err)
	}
}

// TestQAWorktreeClearsWhenRouteDropsBlackbox：最终路线不含黑盒时隔离工作区
// 登记随路线确认清空（镜像 legacy 清空点；登记在路线确认之前完成）。
func TestQAWorktreeClearsWhenRouteDropsBlackbox(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	runner := vcs.ExecRunner{}
	worktree := filepath.Join(t.TempDir(), "qa-worktree")
	if _, err := runner.Run(root, "git", "worktree", "add", "--detach", worktree, base); err != nil {
		t.Fatal(err)
	}
	d := startValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil)
	d.reviewPass(commandPlan())
	if err := d.f.RegisterQAWorktree(worktree); err != nil {
		t.Fatal(err)
	}
	run, err := d.f.Status()
	if err != nil || run.QAWorktree != worktree {
		t.Fatalf("worktree registration = %q, %v", run.QAWorktree, err)
	}
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
	d.confirmRoute(routeDecisionForGates([]string{"whitebox"}))
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.revision, Document: validationDocument}}})
	d.drive()
	run, err = d.f.Status()
	if err != nil {
		t.Fatal(err)
	}
	if run.QAWorktree != "" {
		t.Fatalf("worktree registration survived a route without blackbox: %q", run.QAWorktree)
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
				return plan
			}
			return nil
		}(), Action: action, DispatchID: id, RequirementRevision: d.revision, Status: "PASS", Findings: []review.Finding{}, Verification: review.OperatorVerification{Checks: []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, Evidence: "checked the exact submitted requirement and all listed acceptance conditions"}})
		if err != nil {
			d.t.Fatal(err)
		}
		d.submit(e)
		d.drive()
	}
}

// TestQAWorktreeClearsWhenBlackboxSetFreezes：黑盒用例集审过冻结时隔离工作区
// 登记清空（镜像 legacy 审过清空点）。
func TestQAWorktreeClearsWhenBlackboxSetFreezes(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	runner := vcs.ExecRunner{}
	worktree := filepath.Join(t.TempDir(), "qa-worktree")
	if _, err := runner.Run(root, "git", "worktree", "add", "--detach", worktree, base); err != nil {
		t.Fatal(err)
	}
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	if err := d.f.RegisterQAWorktree(worktree); err != nil {
		t.Fatal(err)
	}
	d.reviewReady()
	run, err := d.f.Status()
	if err != nil {
		t.Fatal(err)
	}
	if run.QAWorktree != "" {
		t.Fatalf("worktree registration survived blackbox set freeze: %q", run.QAWorktree)
	}
}

// TestPrepareCandidateChainPassesWithoutGitignore：宿主仓库未 gitignore 运行
// 期目录时，引擎自建目录（.gates/engine/ 等）不阻断晋升——无 .gitignore 的干
// 净仓上 Start→PrepareCandidate 全链通过（幂等；晋升后区间再累积可重复组织）。
func TestPrepareCandidateChainPassesWithoutGitignore(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "--all")
	run("commit", "-m", "base")
	base, err := vcs.New(root, nil).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	f := startSnapshotRun(t, root, "no-gitignore", base)
	promoted, err := f.PrepareCandidate("")
	if err != nil {
		t.Fatalf("promotion on a clean repo without .gitignore failed: %v", err)
	}
	if promoted != base {
		t.Fatalf("single-commit promotion moved HEAD: %s -> %s", base, promoted)
	}
	again, err := f.PrepareCandidate("")
	if err != nil || again != promoted {
		t.Fatalf("repeat promotion = %q, %v", again, err)
	}
	projection, err := f.Show()
	if err != nil || projection.PromotedSnapshot != promoted {
		t.Fatalf("public projection = %q, %v", projection.PromotedSnapshot, err)
	}
	// 晋升后新增提交：可再次组织（重复绑定），压回单提交。
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package main // later\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "--all")
	run("commit", "-m", "later work")
	rePromoted, err := f.PrepareCandidate("re-organized candidate")
	if err != nil {
		t.Fatalf("re-promotion after new commits failed: %v", err)
	}
	if count, err := vcs.New(root, nil).CommitCount(base, rePromoted); err != nil || count != 1 {
		t.Fatalf("re-promotion left %d commits, %v", count, err)
	}
}
