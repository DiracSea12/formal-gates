package validate

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/lifecycle"
)

// TestPrepareDoesNotStaleAnyDispatch covers RQ-013 item 1: prepare SHALL NOT
// invalidate any dispatch. Re-preparing the same function leaves the prior
// ticket untouched (staling now happens at claim time).
func TestPrepareDoesNotStaleAnyDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "prepare-no-stale")
	first := prepareDispatch(t, root, pkg, state.RunID, "qa-review")
	loaded, _ := LoadRunState(root, state.RunID)
	if loaded.Dispatches[first].Status != "OPEN" {
		t.Fatalf("first dispatch must be OPEN: %#v", loaded.Dispatches[first])
	}
	// 同功能再次 prepare（旧 OPEN 空票不触发续用强制）：prepare 不作废旧票。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-review", "", false, ""); err != nil {
		t.Fatal(err)
	}
	loaded, _ = LoadRunState(root, state.RunID)
	if loaded.Dispatches[first].Status != "OPEN" {
		t.Fatalf("prepare must not stale the prior OPEN ticket: %#v", loaded.Dispatches[first])
	}
}

// TestClaimModeScopedCleanupAndWhiteboxPrepareDoesNotStaleBlackbox covers RQ-013
// items 1 & 2 & 4: prepare never stales, and the claim-time OPEN cleanup is
// mode-scoped across every target — claiming the blackbox dispatch leaves the
// whitebox OPEN ticket of the same target untouched (fixes "whitebox review
// prepare staled the blackbox review").
func TestClaimModeScopedCleanupAndWhiteboxPrepareDoesNotStaleBlackbox(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "mode-scoped")
	// 黑盒与白盒各自独立派发、并行执行（R 修复清单 item 3）。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "blackbox", false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "whitebox", false, ""); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadRunState(root, state.RunID)
	blackbox := qaExecutionDispatchByMode(t, root, state.RunID, "blackbox")
	whitebox := qaExecutionDispatchByMode(t, root, state.RunID, "whitebox")
	if blackbox == "" || whitebox == "" || blackbox == whitebox {
		t.Fatalf("per-mode dispatches were not prepared: %#v", loaded.Dispatches)
	}
	// RQ-013 item 1：prepare 不作废——两个 mode 的票都保持 OPEN。
	if loaded.Dispatches[blackbox].Status != "OPEN" || loaded.Dispatches[whitebox].Status != "OPEN" {
		t.Fatalf("prepare must not stale another mode's dispatch: %#v", loaded.Dispatches)
	}
	// RQ-013 items 2/4：认领黑盒时只清理同 mode 的 OPEN 空票，白盒 OPEN 票不被作废。
	if _, err := ClaimDispatch(root, pkg, state.RunID, blackbox, "blackbox-executor"); err != nil {
		t.Fatal(err)
	}
	loaded, _ = LoadRunState(root, state.RunID)
	if loaded.Dispatches[blackbox].Status != "CLAIMED" || loaded.Dispatches[whitebox].Status != "OPEN" {
		t.Fatalf("claim cleanup must be mode-scoped: %#v", loaded.Dispatches)
	}
}

// TestClaimRejectsParallelSameFunction covers RQ-013 item 3: claiming a fresh
// dispatch is rejected while a same-function dispatch is already CLAIMED and its
// subagent has not been terminated (no recorded stop event).
func TestClaimRejectsParallelSameFunction(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "claim-dedup", "custom", []string{"quality"})
	first := prepareAndClaim(t, root, pkg, state.RunID, "quality", "gate-reviewer-one")
	// 同功能新派发（用户授权重开）：prepare 不作废旧派发。
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", true, "user reopened the gate"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadRunState(root, state.RunID)
	fresh := openDispatchID(loaded, "gate", "quality")
	if fresh == "" || fresh == first {
		t.Fatalf("fresh gate dispatch was not prepared: %#v", loaded.Dispatches)
	}
	if loaded.Dispatches[first].Status != "CLAIMED" {
		t.Fatalf("prepare must not stale the in-flight dispatch: %#v", loaded.Dispatches[first])
	}
	// 无 stop 事件（子代理仍在途）→ 默认拒绝两个同功能子代理并行。
	if _, err := ClaimDispatch(root, pkg, state.RunID, fresh, "gate-reviewer-two"); err == nil || !strings.Contains(err.Error(), "already in flight") {
		t.Fatalf("parallel same-function claim was not rejected: %v", err)
	}
}

// TestClaimCleansUpOldOpenEmptyTickets covers RQ-013 item 4: claiming a fresh
// dispatch automatically stales old OPEN empty tickets of the same function (no
// subagent / no start event) so they never block a claim.
func TestClaimCleansUpOldOpenEmptyTickets(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "open-cleanup", "custom", []string{"quality"})
	// 第一张 OPEN 空票（未认领、无子代理）。
	first := prepareDispatch(t, root, pkg, state.RunID, "quality")
	loaded, _ := LoadRunState(root, state.RunID)
	if loaded.Dispatches[first].Status != "OPEN" {
		t.Fatalf("first dispatch must be OPEN: %#v", loaded.Dispatches[first])
	}
	// 同功能第二张票：OPEN 空票不触发续用强制，prepare 也不作废第一张。
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err != nil {
		t.Fatal(err)
	}
	loaded, _ = LoadRunState(root, state.RunID)
	fresh := openDispatchID(loaded, "gate", "quality")
	if fresh == "" || fresh == first {
		t.Fatalf("fresh gate dispatch was not prepared: %#v", loaded.Dispatches)
	}
	// 认领新派发：同功能旧 OPEN 空票自动作废清掉，认领不被挡。
	if _, err := ClaimDispatch(root, pkg, state.RunID, fresh, "gate-reviewer"); err != nil {
		t.Fatal(err)
	}
	loaded, _ = LoadRunState(root, state.RunID)
	if loaded.Dispatches[first].Status != "STALE" || loaded.Dispatches[fresh].Status != "CLAIMED" {
		t.Fatalf("old OPEN ticket was not cleaned up at claim: %#v", loaded.Dispatches)
	}
}

// TestClaimAfterManualTerminationAllowsFresh covers RQ-013 item 5: after the main
// agent directly terminates the prior same-function subagent (lifecycle records
// its stop event / interruption reason), claiming the same-function fresh
// dispatch marks the prior STALE and proceeds. Without the termination evidence
// the claim is rejected (see TestClaimRejectsParallelSameFunction).
func TestClaimAfterManualTerminationAllowsFresh(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "manual-terminate", "custom", []string{"quality"})
	prior := workflowLifecycle
	workflowLifecycle = &workflowLifecycleStub{verification: lifecycle.Verification{Outcome: lifecycle.Verified}, interruptionReason: "user abort"}
	t.Cleanup(func() { workflowLifecycle = prior })
	first := prepareAndClaim(t, root, pkg, state.RunID, "quality", "gate-reviewer-one")
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", true, "user reopened the gate"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadRunState(root, state.RunID)
	fresh := openDispatchID(loaded, "gate", "quality")
	if fresh == "" || fresh == first {
		t.Fatalf("fresh gate dispatch was not prepared: %#v", loaded.Dispatches)
	}
	// 前子代理已被终结（生命周期记录了中断原因）：认领新派发 → 前派发 STALE、新派发可认领。
	if _, err := ClaimDispatch(root, pkg, state.RunID, fresh, "gate-reviewer-two"); err != nil {
		t.Fatalf("claim after manual termination was rejected: %v", err)
	}
	loaded, _ = LoadRunState(root, state.RunID)
	if loaded.Dispatches[first].Status != "STALE" || loaded.Dispatches[fresh].Status != "CLAIMED" {
		t.Fatalf("manual-terminated prior was not staled at claim: %#v", loaded.Dispatches)
	}
}

// TestStaleClaimedDispatchResultCanBeRecorded covers RQ-013 item 6 (恢复路径): a
// dispatch the reviewer had already claimed, whose results were produced, can
// still be recorded when it has been staled — no re-dispatch, identity and
// source bindings verified, record lands on the current snapshot.
func TestStaleClaimedDispatchResultCanBeRecorded(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "stale-record", "custom", []string{"quality"})
	prior := workflowLifecycle
	workflowLifecycle = &workflowLifecycleStub{verification: lifecycle.Verification{Outcome: lifecycle.Verified}}
	t.Cleanup(func() { workflowLifecycle = prior })
	a := prepareAndClaim(t, root, pkg, state.RunID, "quality", "stale-reviewer")
	// 构造"审查者已认领但派发被作废"（快照未变、source 绑定仍匹配）的状态。
	loaded, _ := LoadRunState(root, state.RunID)
	dispatch := loaded.Dispatches[a]
	dispatch.Status = "STALE"
	loaded.Dispatches[a] = dispatch
	if err := SaveRunState(root, loaded); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordGate(root, pkg, state.RunID, "quality", a, "PASS", "recovered result", comparedRange(state), nil); err != nil {
		t.Fatalf("stale claimed dispatch result could not be recorded: %v", err)
	}
	recorded, _ := LoadRunState(root, state.RunID)
	if recorded.Gates["quality"].Status != "PASS" || recorded.Dispatches[a].Status != "COMPLETED" {
		t.Fatalf("recovery record did not complete the dispatch: %#v", recorded.Gates["quality"])
	}
}

// TestStaleRecordRejectedWhenReplacementInFlight covers RQ-013 item 6's
// double-record guard: a STALE dispatch result is rejected while a same-function
// replacement dispatch is in flight (OPEN or CLAIMED), so the stale record and
// the replacement cannot both land.
func TestStaleRecordRejectedWhenReplacementInFlight(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "stale-double", "custom", []string{"quality"})
	prior := workflowLifecycle
	workflowLifecycle = &workflowLifecycleStub{verification: lifecycle.Verification{Outcome: lifecycle.Verified}, interruptionReason: "user abort"}
	t.Cleanup(func() { workflowLifecycle = prior })
	a := prepareAndClaim(t, root, pkg, state.RunID, "quality", "stale-reviewer")
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", true, "user reopened the gate"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadRunState(root, state.RunID)
	b := openDispatchID(loaded, "gate", "quality")
	if b == "" || b == a {
		t.Fatalf("replacement dispatch was not prepared: %#v", loaded.Dispatches)
	}
	// 前子代理已终结：认领替换派发 b 把 a 标 STALE、b 在途。
	if _, err := ClaimDispatch(root, pkg, state.RunID, b, "replacement-reviewer"); err != nil {
		t.Fatal(err)
	}
	loaded, _ = LoadRunState(root, state.RunID)
	if loaded.Dispatches[a].Status != "STALE" || loaded.Dispatches[b].Status != "CLAIMED" {
		t.Fatalf("replacement claim did not stale the prior: %#v", loaded.Dispatches)
	}
	// 防双记：STALE 记录与替换派发并行记录被拒。
	if _, err := RecordGate(root, pkg, state.RunID, "quality", a, "PASS", "stale result", comparedRange(state), nil); err == nil || !strings.Contains(err.Error(), "replacement") {
		t.Fatalf("stale record with an in-flight replacement was not rejected: %v", err)
	}
}

// TestParallelAdvicePostSnapshot covers RQ-014's stage→should-parallel rule for
// the post-development stage: blackbox QA execution + whitebox QA execution +
// each selected gate, all parallel.
func TestParallelAdvicePostSnapshot(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "parallel-post", "custom", []string{"blackbox", "whitebox", "quality"})
	advice := ParallelAdviceFor(state)
	if !advice.Remind {
		t.Fatalf("post-snapshot stage should remind about insufficient parallelism: %#v", advice)
	}
	if advice.Stage != "开发后审查" {
		t.Fatalf("stage=%q, want 开发后审查", advice.Stage)
	}
	want := []string{"QA 执行（blackbox）", "QA 执行（whitebox）", "门审查（quality）"}
	if !reflect.DeepEqual(advice.ShouldTasks, want) {
		t.Fatalf("should-parallel set=%v, want %v", advice.ShouldTasks, want)
	}
	if advice.InFlight != 0 {
		t.Fatalf("in-flight=%d, want 0", advice.InFlight)
	}
	if !strings.Contains(advice.Message, "可并行 3 项") || !strings.Contains(advice.Message, "当前并行 0 项") {
		t.Fatalf("reminder message=%q", advice.Message)
	}
}

// TestParallelReminderInsufficientParallelism covers RQ-014: ParallelCheck
// reports a stderr reminder when the should-parallel set is non-empty and fewer
// tasks are in flight than should be.
func TestParallelReminderInsufficientParallelism(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "parallel-remind", "custom", []string{"blackbox", "whitebox", "quality"})
	// 只并行了 1 个（黑盒 QA 执行已在途），其余 2 个未派发 → 提醒。
	prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	now := time.Now()
	parallelCooldown = 0
	t.Cleanup(func() { parallelCooldown = 60 * time.Second })
	advice, remind := ParallelCheck(root, state.RunID, now)
	if !remind {
		t.Fatalf("insufficient parallelism was not reminded: %#v", advice)
	}
	if advice.InFlight != 1 || len(advice.ShouldTasks) != 3 {
		t.Fatalf("advice=%#v", advice)
	}
}

// TestParallelNoReminderWhenSufficient covers RQ-014: no reminder when every
// should-parallel task is already in flight.
func TestParallelNoReminderWhenSufficient(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "parallel-full", "custom", []string{"blackbox", "whitebox", "quality"})
	// 三个应并行任务全部在途。
	prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "whitebox")
	quality := prepareDispatch(t, root, pkg, state.RunID, "quality")
	if _, err := ClaimDispatch(root, pkg, state.RunID, quality, "quality-reviewer"); err != nil {
		t.Fatal(err)
	}
	parallelCooldown = 0
	t.Cleanup(func() { parallelCooldown = 60 * time.Second })
	if _, remind := ParallelCheck(root, state.RunID, time.Now()); remind {
		t.Fatalf("full parallelism was still reminded")
	}
}

// TestParallelCheckIsReadOnly covers RQ-014's lifecycle-safety requirement: the
// parallel check is read-only for the workflow — it never disturbs in-flight
// (CLAIMED) dispatches or other run state (the only write is the independent
// cooldown marker file).
func TestParallelCheckIsReadOnly(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "parallel-readonly", "custom", []string{"blackbox", "whitebox", "quality"})
	blackbox := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	before, _ := LoadRunState(root, state.RunID)
	parallelCooldown = 0
	t.Cleanup(func() { parallelCooldown = 60 * time.Second })
	if _, remind := ParallelCheck(root, state.RunID, time.Now()); !remind {
		t.Fatalf("insufficient parallelism was not reminded")
	}
	after, _ := LoadRunState(root, state.RunID)
	if after.Dispatches[blackbox].Status != "CLAIMED" {
		t.Fatalf("parallel check disturbed an in-flight dispatch: %#v", after.Dispatches[blackbox])
	}
	if !reflect.DeepEqual(before.Dispatches, after.Dispatches) {
		t.Fatalf("parallel check mutated the dispatch set")
	}
}

// TestParallelCooldownDedup covers RQ-014's cooldown/dedup: the same-signature
// reminder is not repeated within the cooldown window, but fires again after the
// window elapses.
func TestParallelCooldownDedup(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "parallel-cooldown", "custom", []string{"blackbox", "whitebox", "quality"})
	parallelCooldown = time.Hour
	t.Cleanup(func() { parallelCooldown = 60 * time.Second })
	now := time.Now()
	if _, remind := ParallelCheck(root, state.RunID, now); !remind {
		t.Fatalf("first reminder was suppressed")
	}
	if _, remind := ParallelCheck(root, state.RunID, now.Add(time.Minute)); remind {
		t.Fatalf("same-signature reminder within the cooldown was not deduplicated")
	}
	// 冷却窗口过后同签名可再次提醒。
	if _, remind := ParallelCheck(root, state.RunID, now.Add(2*time.Hour)); !remind {
		t.Fatalf("cooldown-elapsed reminder was suppressed")
	}
}
