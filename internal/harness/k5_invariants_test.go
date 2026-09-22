//go:build !windows

package harness

// K5 返修验收框架：可复用不变量断言 + 中断/不中断轨迹对照测试。
//
// 这个文件只写测试，不改产品代码。它先于返修修复落地，作为后续每一批
// 修复的验收约束：修复把中断场景的红消掉，同时不许把不中断场景弄红。
//
// 五条业务不变量（每条都有“违反时给出证据路径”的检查函数，任何测试都
// 可直接调用；对应的评审发现编号 R1–R6 见 /private/tmp/k5-review2/review.md）：
//
//   - 单活写入者（R1）：同一开发工作区至多一个活动写入调用。写入者只有
//     development 任务（development 与 repair 都是它的世代）；检查在各自
//     一次性克隆里跑、门审只读，因此不算这个工作区的写入者。
//   - evaluate 单驱动（R1；判据按三轮评审 R5 修正）：每个 evaluate 判断
//     （按代次，outcomes/evaluate/<gen>.json 一判断）至多驱动一轮返修——
//     内核 task_revisions 里引用某反馈包的修订输入数 ≤ 发布该反馈包的
//     判断数。反馈 ref 只是内容摘要（同发现＋同候选 ⇒ 同 ref），负责内容
//     绑定，不是判断身份：不同判断共享内容合法，同一判断驱动两次违例。
//   - COMPLETE 零未结算（R1）：终态 HARNESS_COMPLETE 时，jobs/ 下不存在
//     “收据缺失（无 exit.json）且进程身份存在（有 process.json）”的目录。
//   - 结果绑候选（R5）：seal 引用的每个 check/gate 结果都绑定同一个最终
//     候选（outcomes 里对应任务的 Candidate 与 harness-result.json 一致）。
//   - 重建换身份（R4）：restart-session 已答后，该任务在内核 task_sessions
//     的身份行必须已清除，其后发出的 native-request.json Identity 必须为空。
//
// “活动”的判据（教训来自既有测试）：process.json 存在 && exit.json 缺失，
// 且记录的 PID 仍对 signal 0 应答。PID＋创建时间容差（±2s，同
// supervisorAlive）用来排除 PID 复用；kill(pid,0) 对未被收割的僵尸进程同样
// 成功——收据缺失＋僵尸与活监督者不可分辨，按“无法证明已结算”计活并
// 在证据里如实注明，绝不猜“应该已经结束”。
//
// 轨迹对照：同一场景跑两遍——不中断跑到底记录终态/每任务 turn 数/返修
// 轮数；中断版在可观察边界杀掉 owner 子进程（复用评审探针的
// startIndependentOwner 模式：test 二进制重入 + 环境变量 + marker 文件同
// 步），用同一 ComposeConfig 重开续跑到终态，断言终态相同、每任务执行
// 次数不增加、全部不变量成立。中断版在修复落地前预期失败——这是失败
// 基线，不许弱化断言换绿；不中断版当前必须全绿。

// ─────────────────────────────────────────────────────────────────────────────
// 行为对照表：参考实现行为 → 验证它的 Go 测试（导航用，逐行注明谁在验证）
//
// 来源标注：参考＝runtime/langgraph/formal_gates_harness/graph.py 与
// infra.py 的 Store.job/handover（行号为本次编写时快照）；评审＝
// internal/harness/k5_independent_test.go（外部复审探针，验收前为红）。
// “现状”以 2026-09-21 最后一次全家系运行为准（本文件与返修修复并行落地，
// 状态会随后续批次移动，读表时以当次 `go test` 为准）。
//
// │ 参考行为（graph.py / infra.py）                     │ 现有测试                                             │ 本文件新增                                     │ 现状        │
// │────────────────────────────────────────────────────│──────────────────────────────────────────────────────│────────────────────────────────────────────────│─────────────│
// │ 主干绿链 build() 263–293                             │ TestWorkflowGreenChainSealsAndArchives               │ …UninterruptedRepairTrajectory / …CheckTrajectory（对照组） │ 绿          │
// │ 需求确认 approval() 106–109                          │ TestWorkflowGreenChain…（首停即需求门）              │ —                                              │ 绿          │
// │ 前置审 FAIL 拦截 pre_review 110–118                  │ TestReviewK5UpstreamFAIL、TestWorkflowPreReviewFailAskRetryAbort │ —                                    │ 绿          │
// │ 前置审 FAIL 重试（retry 反馈再跑）                   │ TestWorkflowPreReviewRetryThenPass                    │ —                                              │ 绿          │
// │ 计划确认工件可读 plan_approval 119–123               │ TestWorkflowGreenChain…（body 字段）                 │ —                                              │ 绿          │
// │ 计划确认工件落盘（R6 要求的真实文件）                │ 评审 TestIndependentK5PlanEvidence                   │ —                                              │ 红（R6）    │
// │ CONTINUE 续轮 invoke_worker 88–93                    │ 判定层 domain_test/contract_test；组合层无专项场景   │ —（诚实记录缺口，非本批范围）                  │ 判定层绿    │
// │ BLOCKED 处置 invoke_worker 94–97                     │ 评审 TestIndependentK5RuntimeStops/gate-blocked      │ —                                              │ 绿          │
// │ 检查运行错误处置 checks 195–196→ask                  │ 评审 TestIndependentK5RuntimeStops/check-runtime     │ —                                              │ 绿          │
// │ 运行时错误重试 invoke_worker 81–84                   │ TestWorkflowContractRejectedIsARuntimeQuestion、TestWorkflowCrashTakeoverClassifications │ —              │ 绿          │
// │ 会话失效→重建询问 invoke_worker 75–80                │ TestWorkflowSessionRefusalAsksRestart                │ —                                              │ 随批波动    │
// │ 重建后换执行身份（R4）                                │ 评审 TestIndependentK5FreshSessionAfterRestart       │ k5RestartTaskFindings（不变量助手）             │ 绿（R4 修复后）│
// │ QA 设计/评审循环 qa_review 132–142                    │ TestWorkflowQADesignLoop、TestWorkflowQADesignFailReviewPassAsksDesignRetry │ —                     │ 绿          │
// │ 开发未决发现拦截 98–104                               │ TestWorkflowDevelopmentFindingsAsk                   │ —                                              │ 绿          │
// │ 返修循环 evaluate→repair→candidate 291–292           │ TestWorkflowRepairRoundDrivesNewCandidate、TestWorkflowRepairLimitAskContinueAndAbort、TestReviewK5GateRepair、TestReviewK5RepairBrief | …InterruptedRepairInFlight + evaluate 单驱动（判断按代次计，同内容多判断合法） | 中断版绿（R1 修复后由本测试确认）│
// │ 崩溃接回 Store.job 288–324（收据轮询/UNKNOWN 不盲重） │ TestWorkflowCrashTakeoverClassifications             │ …InterruptedRepairInFlight / …InterruptedCheckReceipt | 检查侧绿，门审在途侧红│
// │ 已完成检查/门审接回（R5：同候选接纳）                │ 评审 TestIndependentK5CompletedCheckCrash / CompletedGateCrash | …InterruptedCheckReceipt + 结果绑候选不变量 | 检查绿（R5A 修复后）；门审在途重跑仍红│
// │ 恢复后的 Held 权限请求                                │ TestReviewK5RecoveredHeld、评审 RecoveredGatePermission | —                                              │ 绿          │
// │ 取消结算 cancel_jobs 325–332                          │ TestWorkflowCancelSettlesEveryInFlightCall、TestReviewK5RecoveredCancel │ —                                 │ 绿          │
// │ 归档（物理化 + 拒绝非终态）                           │ TestWorkflowGreenChain…（归档段）、TestReviewK5ArchiveCreatesArtifact、TestWorkflowArchiveRefusesNonTerminal │ — │ 绿          │
// │ 重开绑定不可变                                        │ TestWorkflowReopenBindingRefused、TestReviewK5ReopenChangedBinding │ —                              │ 绿          │
// │ 真实宿主全链路                                        │ TestWorkflowLiveAcceptanceGreenChain（live 门）      │ —                                              │ 环境门控    │
// │ 单活写入者 / COMPLETE 零未结算（总方案 §4.1/§3.2）   │ （此前无）                                           │ k5SingleActiveWriterFindings、k5CompleteSettledFindings | 见各场景   │
// ─────────────────────────────────────────────────────────────────────────────

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/runner"
)

// ---- 1. 不变量助手 ----------------------------------------------------------

// k5TurnFact is one supervised turn's settlement fact, read from the run
// directory (no Compose needed — the same scan works on a live run, a reopened
// run and a post-mortem directory).
type k5TurnFact struct {
	Task string // jobs/<task> 目录段（pathSafe 后的任务名）
	Dir  string
	PID  int
	// Started is process.json 的 started_at（epoch 秒）。
	Started float64
	// HasProcess/HasExit: process.json / exit.json 是否存在。
	HasProcess bool
	HasExit    bool
	// Active: 无法证明已结算——收据缺失，且记录 PID 仍对 signal 0 应答。
	// 僵尸进程同样应答（既有测试的教训），不可分辨即计活，见 Note。
	Active bool
	Note   string
}

// k5ScanTurns walks jobs/<task>/<invocation>/turn-*/ and reads each turn's
// settlement facts. 目录名到任务身份的换算沿用 pathSafe 的落盘事实。
func k5ScanTurns(runDir string) []k5TurnFact {
	var facts []k5TurnFact
	tasks, err := os.ReadDir(filepath.Join(runDir, "jobs"))
	if err != nil {
		return nil
	}
	for _, dir := range tasks {
		if !dir.IsDir() {
			continue
		}
		task := dir.Name()
		invocations, err := os.ReadDir(filepath.Join(runDir, "jobs", task))
		if err != nil {
			continue
		}
		for _, inv := range invocations {
			if !inv.IsDir() {
				continue
			}
			turns, err := os.ReadDir(filepath.Join(runDir, "jobs", task, inv.Name()))
			if err != nil {
				continue
			}
			for _, turn := range turns {
				if !turn.IsDir() || !strings.HasPrefix(turn.Name(), "turn-") {
					continue
				}
				dir := filepath.Join(runDir, "jobs", task, inv.Name(), turn.Name())
				fact := k5TurnFact{Task: task, Dir: dir}
				if _, err := os.Stat(filepath.Join(dir, "process.json")); err == nil {
					fact.HasProcess = true
					if value, err := readJSONFile(filepath.Join(dir, "process.json")); err == nil {
						fact.PID = intOf(getOf(value, "pid"))
						fact.Started = floatOf(getOf(value, "started_at"))
					}
				}
				if _, err := os.Stat(filepath.Join(dir, "exit.json")); err == nil {
					fact.HasExit = true
				}
				if fact.HasProcess && !fact.HasExit {
					fact.Note = "receipt missing with a recorded process identity"
					if fact.PID > 0 {
						switch {
						case !coordination.ProcessAlive(fact.PID):
							fact.Note = fmt.Sprintf("pid %d no longer answers signal 0 (dead without a receipt)", fact.PID)
						default:
							created, ok := processCreationTime(fact.PID)
							switch {
							case !ok:
								fact.Active = true // kill(pid,0) 成功但创建时间不可验证：无法证明已结算
								fact.Note = "kill(pid,0) succeeds but the creation time is unverifiable: counted active"
							default:
								delta := created.Unix() - int64(fact.Started)
								if delta < 0 {
									delta = -delta
								}
								if delta <= 2 {
									fact.Active = true // 记录身份存活（PID＋创建时间都在容差内）
									fact.Note = "recorded identity alive (pid and creation time match)"
								} else {
									fact.Note = fmt.Sprintf("pid %d was reused by a different process (creation time differs)", fact.PID)
								}
							}
						}
					}
				}
				facts = append(facts, fact)
			}
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].Dir < facts[j].Dir })
	return facts
}

// k5SingleActiveWriterFindings checks 单活写入者：同一开发工作区至多一个
// 活动写入调用。写入者＝jobs/development/ 下的 turn（development 与 repair
// 都是 development 任务的世代）；检查任务写各自的一次性克隆、门审只读，
// 不写这个工作区。返回的每条 finding 都是带证据路径的违例。
func k5SingleActiveWriterFindings(runDir string) []string {
	var actives []k5TurnFact
	for _, fact := range k5ScanTurns(runDir) {
		if fact.Task == pathSafe(taskDevelopment) && fact.Active {
			actives = append(actives, fact)
		}
	}
	if len(actives) <= 1 {
		return nil
	}
	findings := []string{fmt.Sprintf(
		"%d simultaneous active writing calls on the development workspace:", len(actives))}
	for _, fact := range actives {
		findings = append(findings, fmt.Sprintf(
			"  %s: pid=%d started_at=%.3f (%s)", fact.Dir, fact.PID, fact.Started, fact.Note))
	}
	return findings
}

// k5EvaluateVerdict is one evaluate verdict's identity facts: the
// generation it ran at and the feedback bundle ref it published.
type k5EvaluateVerdict struct {
	// Generation is 判断的代次身份——outcomes/evaluate/<gen>.json 的 gen。
	Generation int64
	// Ref is 反馈包的内容摘要（artifacts/<ref>.json），只做内容绑定。
	Ref string
}

// k5EvaluateVerdicts reads every evaluate verdict from the outcome tree,
// one per generation payload (outcomes/evaluate/<gen>.json), sorted by
// generation. 判断的身份是代次，不是 ref：ref 是内容摘要——同样的失败
// 发现加未变的候选哈希出同一个 ref，所以“两轮返修都没让发现变化”这条
// 合法轨迹会由两个不同判断发布同一个 ref。一个代次恰有一个载荷文件，
// 文件集就是判断的全集。
func k5EvaluateVerdicts(runDir string) []k5EvaluateVerdict {
	paths, _ := filepath.Glob(filepath.Join(runDir, "outcomes", "evaluate", "*.json"))
	var verdicts []k5EvaluateVerdict
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record outcomeRecord
		if json.Unmarshal(body, &record) != nil || record.Feedback == "" {
			continue
		}
		verdicts = append(verdicts, k5EvaluateVerdict{Generation: record.Generation, Ref: record.Feedback})
	}
	sort.Slice(verdicts, func(i, j int) bool { return verdicts[i].Generation < verdicts[j].Generation })
	return verdicts
}

// k5RevisionInputs reads the kernel's persisted revision inputs from a
// run directory's database (read-only, no ownership taken) — the
// post-mortem spelling of Store.RevisionInputs, the same rows a live
// owner reads. 任何读失败如实报错：单驱动不变量的证据源不可读本身
// 就是要报告的事实。
func k5RevisionInputs(runDir string) ([]runner.RevisionInput, error) {
	db, err := sql.Open("sqlite", databaseURI(filepath.Join(runDir, "run.db")))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT task_id, generation, input FROM task_revisions ORDER BY task_id, generation")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []runner.RevisionInput
	for rows.Next() {
		var row runner.RevisionInput
		if err := rows.Scan(&row.TaskID, &row.Generation, &row.Input); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// k5EvaluateSingleDriverFindings checks evaluate 单驱动 by verdict
// identity, not content identity: 每个 evaluate 判断（一个代次）至多驱动
// 一轮返修。ref 只把判断绑定到消费了它的修订行——返修 brief 逐字内嵌
// wrapper 的 source.artifact，所以按 ref 字符串匹配；不按任务或用途过滤
// （任何引用该反馈包的修订都算消费它），与账本时代同强或更强。
//
// 判据：对每个 ref，引用它的修订输入行数 ≤ 发布过该 ref 的判断数（按
// 代次计，一个代次载荷一判断）。为什么这是“一项判断只消费一次”而“同
// 内容多判断合法”：修订行和判断都随轮次单调前移——每轮返修把开发任务
// 的修订序列和 evaluate 的代次一起推一格——所以第 i 条引用行只可能由
// 第 i 个或更早的同 ref 判断驱动；行数一旦超过判断数，必有某个判断驱动
// 了第二轮（一项判断被消费两次），必报。行数不超过判断数时，每条行都
// 能配到一个单独的判断，两个判断共享同一个 ref 各走各的一轮，合法——
// 判断身份（代次）与内容身份（ref）分开，内容相等绝不合并两个判断。
// 证据行与修订落库在同一事务，内核读不到本身就是要报告的事实。
func k5EvaluateSingleDriverFindings(runDir string) []string {
	verdicts := k5EvaluateVerdicts(runDir)
	if len(verdicts) == 0 {
		return nil
	}
	rows, err := k5RevisionInputs(runDir)
	if err != nil {
		return []string{"run.db task_revisions is unreadable: " + err.Error()}
	}
	issuers := map[string][]int64{} // ref → 发布过它的判断代次（升序）
	for _, verdict := range verdicts {
		issuers[verdict.Ref] = append(issuers[verdict.Ref], verdict.Generation)
	}
	evidence := map[string][]string{} // ref → 引用了它的修订输入行（按行序）
	for i, row := range rows {
		brief := string(row.Input)
		for ref := range issuers {
			if strings.Contains(brief, ref) {
				evidence[ref] = append(evidence[ref], fmt.Sprintf(
					"  run.db task_revisions row %d: task=%s generation=%d binds artifacts/%s.json",
					i+1, row.TaskID, row.Generation, ref))
			}
		}
	}
	var refs []string
	for ref := range issuers {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	var findings []string
	for _, ref := range refs {
		consumed, issued := len(evidence[ref]), len(issuers[ref])
		if consumed <= issued {
			continue // 每条引用行都配得到各自的判断：一轮一判断，合法
		}
		generations := make([]string, 0, len(issuers[ref]))
		for _, gen := range issuers[ref] {
			generations = append(generations, fmt.Sprintf("%d", gen))
		}
		findings = append(findings, fmt.Sprintf(
			"artifacts/%s.json was bound by %d revision intents but only %d evaluate verdict(s) (generation(s) %s) issued it — one verdict drove more than its single round:\n%s",
			ref, consumed, issued, strings.Join(generations, ","), strings.Join(evidence[ref], "\n")))
	}
	return findings
}

// k5CompleteSettledFindings checks COMPLETE 零未结算：终态 HARNESS_COMPLETE 时
// jobs/ 全部目录中不存在“收据缺失且进程身份存在”者。判据只看文件事实
// （process.json 在、exit.json 缺），进程死活写进证据——死了没收据同样是洞。
// 非COMPLETE 终态不适用本条，直接通过。
func k5CompleteSettledFindings(runDir, terminal string) []string {
	if terminal != statusHarnessComplete {
		return nil
	}
	var findings []string
	for _, fact := range k5ScanTurns(runDir) {
		if fact.HasProcess && !fact.HasExit {
			findings = append(findings, fmt.Sprintf(
				"  %s: process.json (pid=%d) with no exit.json while the run reports HARNESS_COMPLETE (%s)",
				fact.Dir, fact.PID, fact.Note))
		}
	}
	if len(findings) > 0 {
		return append([]string{"HARNESS_COMPLETE with unsettled own executions:"}, findings...)
	}
	return nil
}

// k5KernelGenerations reads the kernel's current generation per task,
// keyed by the outcome tree's storage name (pathSafe of the logical
// task id) — the authority for "which outcome of this task is currently
// effective". 读不到内核＝空表：孤儿载荷目录按自身文件核对。
func k5KernelGenerations(runDir string) map[string]int64 {
	generations := map[string]int64{}
	db, err := sql.Open("sqlite", databaseURI(filepath.Join(runDir, "run.db")))
	if err != nil {
		return generations
	}
	defer db.Close()
	rows, err := db.Query("SELECT task_id, generation FROM tasks")
	if err != nil {
		return generations
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var generation int64
		if rows.Scan(&id, &generation) == nil {
			generations[pathSafe(id)] = generation
		}
	}
	return generations
}

// k5DirOutcomeRecord reads one outcome directory's currently effective
// record: the payload at the kernel's current generation when the kernel
// knows the task, else the directory's newest payload by generation —
// the only evidence an orphan directory (no kernel row) offers, so a
// fabricated result still faces the check.
func k5DirOutcomeRecord(runDir, name string, generation int64) (outcomeRecord, bool) {
	if generation > 0 {
		body, err := os.ReadFile(filepath.Join(runDir, "outcomes", name, fmt.Sprintf("%d.json", generation)))
		if err != nil {
			return outcomeRecord{}, false
		}
		var record outcomeRecord
		if json.Unmarshal(body, &record) != nil {
			return outcomeRecord{}, false
		}
		return record, true
	}
	paths, _ := filepath.Glob(filepath.Join(runDir, "outcomes", name, "*.json"))
	if len(paths) == 0 {
		return outcomeRecord{}, false
	}
	sort.Strings(paths) // 代数文件名字典序＝代序（仅孤儿目录的兜底）
	var record outcomeRecord
	body, err := os.ReadFile(paths[len(paths)-1])
	if err != nil || json.Unmarshal(body, &record) != nil {
		return outcomeRecord{}, false
	}
	return record, true
}

// k5SealBindsCandidateFindings checks 结果绑候选：harness-result.json 的候选
// 与 seal 所引用的每个 check/gate 结果的 Candidate 一致——哪个结果有效由
// 内核当前代决定，孤儿载荷目录按其全部载荷核对。缺失候选（空串）同样
// 违例——R5 的接回接纳曾不写 Candidate。
func k5SealBindsCandidateFindings(runDir string) []string {
	value, err := readJSONFile(filepath.Join(runDir, "harness-result.json"))
	if err != nil {
		return nil // 没有封存结果：终态断言管那条，这里不重复
	}
	candidate := textOf(getOf(value, "candidate"))
	if candidate == "" {
		return []string{"harness-result.json carries no candidate"}
	}
	generations := k5KernelGenerations(runDir)
	taskDirs, err := os.ReadDir(filepath.Join(runDir, "outcomes"))
	if err != nil {
		return nil
	}
	var findings []string
	for _, taskDir := range taskDirs {
		name := taskDir.Name()
		if !taskDir.IsDir() {
			continue
		}
		if !strings.HasPrefix(name, "check-") && !strings.HasPrefix(name, "gate-") {
			continue
		}
		if strings.HasSuffix(name, "-ask") {
			continue // 问题任务（…:ask）的 outcome 是问答回执，不是 seal 引用的验证结果
		}
		record, ok := k5DirOutcomeRecord(runDir, name, generations[name])
		if !ok {
			continue
		}
		if record.Candidate != candidate {
			findings = append(findings, fmt.Sprintf(
				"  outcomes/%s: current generation binds candidate %q, the sealed result names %q",
				name, record.Candidate, candidate))
		}
	}
	if len(findings) > 0 {
		return append([]string{"sealed result and its check/gate results disagree on the candidate:"}, findings...)
	}
	return nil
}

// k5SessionIdentity reads one task's remembered session identity from the
// run directory's database (read-only) — 重建换身份的证据源。空串＝该任务
// 当前无被记住的执行身份（一个不存在的数据库同样读作空：没有 run.db 的
// 目录不曾有过会话）。
func k5SessionIdentity(runDir, task string) string {
	db, err := sql.Open("sqlite", databaseURI(filepath.Join(runDir, "run.db")))
	if err != nil {
		return ""
	}
	defer db.Close()
	var identity string
	if err := db.QueryRow(
		"SELECT identity FROM task_sessions WHERE task_id=?", task).Scan(&identity); err != nil {
		return ""
	}
	return identity
}

// k5RestartTaskFindings checks 重建换身份 for one task after the user answered
// restart-session: (a) 内核 task_sessions 里该任务的身份行必须已清除；(b)
// 若调用方给出回答时刻的锚点（answeredAt 非零），其后落盘的
// native-request.json 的 Identity 必须为空。锚点用文件 mtime 比对——回答是
// 调用方亲历的事实，它知道时刻；行本身无时间戳。
func k5RestartTaskFindings(runDir, task string, answeredAt time.Time) []string {
	var findings []string
	if identity := k5SessionIdentity(runDir, task); identity != "" {
		findings = append(findings, fmt.Sprintf(
			"  run.db task_sessions still remembers session %q for %s after restart-session was answered",
			identity, task))
	}
	if !answeredAt.IsZero() {
		requests, _ := filepath.Glob(filepath.Join(runDir, "jobs", pathSafe(task), "*", "turn-*", "native-request.json"))
		for _, request := range requests {
			info, err := os.Stat(request)
			if err != nil || !info.ModTime().After(answeredAt) {
				continue
			}
			value, err := readJSONFile(request)
			if err != nil {
				continue
			}
			if identity := textOf(getOf(value, "identity")); identity != "" {
				findings = append(findings, fmt.Sprintf(
					"  %s (written after the restart answer) still requests the refused identity %q",
					request, identity))
			}
		}
	}
	if len(findings) > 0 {
		return append([]string{fmt.Sprintf("restart-session did not clear %s's execution identity:", task)}, findings...)
	}
	return nil
}

// k5RestartAnsweredFindings derives 重建换身份 from the kernel's settled
// questions alone: every task the user ever answered restart-session — a
// runtime question's decisions row or a granted restart request — gets the
// (a) check. The rows are read across ALL generations (any answer ever
// settled, stale or current), the same totality the retired answer ledger
// had; the full check with the time anchor stays k5RestartTaskFindings.
func k5RestartAnsweredFindings(runDir string) []string {
	db, err := sql.Open("sqlite", databaseURI(filepath.Join(runDir, "run.db")))
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT task_id FROM decisions WHERE state='answered' AND answer='restart-session'
		UNION
		SELECT task_id FROM user_requests WHERE answer='restart-session'`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	seen := map[string]bool{}
	var tasks []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil
		}
		task := strings.TrimSuffix(taskID, askSuffix)
		if !seen[task] {
			seen[task] = true
			tasks = append(tasks, task)
		}
	}
	var findings []string
	for _, task := range tasks {
		findings = append(findings, k5RestartTaskFindings(runDir, task, time.Time{})...)
	}
	return findings
}

// assertK5Invariants runs the full invariant set over one terminal observation
// of a scenario. 违例逐条以证据路径报出；全部成立则静默通过。
func assertK5Invariants(t *testing.T, runDir string, state ComposeState) {
	t.Helper()
	checks := []struct {
		name     string
		findings []string
	}{
		{"single active writer", k5SingleActiveWriterFindings(runDir)},
		{"evaluate drives one repair round", k5EvaluateSingleDriverFindings(runDir)},
		{"HARNESS_COMPLETE settles every own execution", k5CompleteSettledFindings(runDir, state.Terminal)},
		{"sealed result binds one candidate", k5SealBindsCandidateFindings(runDir)},
		{"restart-session clears the execution identity", k5RestartAnsweredFindings(runDir)},
	}
	for _, check := range checks {
		if len(check.findings) > 0 {
			t.Errorf("K5 invariant %q violated:\n%s", check.name, strings.Join(check.findings, "\n"))
		}
	}
}

// ---- 2. 轨迹对照 ------------------------------------------------------------

// k5Trajectory is one scenario run's observable shape: the stop it ended on and
// the per-task execution counts (turn directories) plus repair rounds.
type k5Trajectory struct {
	Status       ComposeStatus
	Terminal     string
	Turns        map[string]int
	RepairRounds int
}

// k5SnapshotTrajectory reads a run directory's trajectory facts.
func k5SnapshotTrajectory(runDir string, state ComposeState) k5Trajectory {
	traj := k5Trajectory{Status: state.Status, Terminal: state.Terminal, Turns: map[string]int{}}
	for _, fact := range k5ScanTurns(runDir) {
		traj.Turns[fact.Task]++
	}
	for _, path := range func() []string {
		paths, _ := filepath.Glob(filepath.Join(runDir, "outcomes", pathSafe(taskDevelopment), "*.json"))
		return paths
	}() {
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record outcomeRecord
		if json.Unmarshal(body, &record) == nil && record.Output != nil && record.Output.Role == "repair" {
			traj.RepairRounds++
		}
	}
	return traj
}

func (traj k5Trajectory) String() string {
	tasks := make([]string, 0, len(traj.Turns))
	for task := range traj.Turns {
		tasks = append(tasks, task)
	}
	sort.Strings(tasks)
	parts := make([]string, 0, len(tasks))
	for _, task := range tasks {
		parts = append(parts, fmt.Sprintf("%s=%d", task, traj.Turns[task]))
	}
	return fmt.Sprintf("status=%s terminal=%q repairRounds=%d turns[%s]",
		traj.Status, traj.Terminal, traj.RepairRounds, strings.Join(parts, " "))
}

// k5DriveToTerminal drives allowing answers until a terminal/failed stop.
// StatusObserved polls（被接回观察的原调用会自行结算）；fixture 不会出现
// Held 交互。有界迭代防止在永远观察不结算的调用上挂死。
func k5DriveToTerminal(t *testing.T, c *Compose, ctx context.Context) ComposeState {
	t.Helper()
	var last ComposeState
	for i := 0; i < 200; i++ {
		last = c.Drive(ctx)
		switch last.Status {
		case StatusWaitingDecision:
			if len(last.Pending) == 0 {
				return last
			}
			for _, question := range last.Pending {
				choice := k5AllowingChoice(question.Options)
				if choice == "" {
					t.Fatalf("ask without a known allowing choice: %+v", question)
				}
				if _, err := c.Answer(ctx, question.DecisionID, choice); err != nil {
					t.Fatalf("answer %s (%s): %v", question.DecisionID, question.TaskID, err)
				}
			}
		case StatusObserved:
			if ctx.Err() != nil {
				return last
			}
			time.Sleep(150 * time.Millisecond)
		case StatusWaitingInteraction:
			t.Fatalf("the fixture raises no held interactions, got %+v", last)
		default:
			return last
		}
	}
	return last
}

// k5AllowingChoice picks a question's allowing choice（approve/continue/retry/
// restart-session），与 answerAllowing 同一词汇表。
func k5AllowingChoice(options []string) string {
	for _, option := range options {
		switch option {
		case "approve", "continue", "retry", "restart-session":
			return option
		}
	}
	return ""
}

// k5WriterWatch samples the single-writer invariant while a run advances:
// 重复调用只在两调用同时在途时可见，终态扫描会错过重叠窗口。采样只可能
// 多报违例（两个未结算的开发树 turn 本身就是重叠证据），不会漏掉后让测试
// 假绿——次数对照与未结算检查兜底。
type k5WriterWatch struct {
	mu       sync.Mutex
	findings []string
	done     chan struct{}
}

func startK5WriterWatch(runDir string) *k5WriterWatch {
	watch := &k5WriterWatch{done: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		last := ""
		for {
			select {
			case <-watch.done:
				return
			case <-ticker.C:
			}
			findings := k5SingleActiveWriterFindings(runDir)
			joined := strings.Join(findings, "\n")
			if joined == "" || joined == last {
				continue // 连续样本相同只记一次：证据不重复刷屏
			}
			last = joined
			watch.mu.Lock()
			watch.findings = append(watch.findings, joined)
			watch.mu.Unlock()
		}
	}()
	return watch
}

func (watch *k5WriterWatch) stop() []string {
	close(watch.done)
	watch.mu.Lock()
	defer watch.mu.Unlock()
	return watch.findings
}

// k5CompareTrajectories asserts the interrupted trajectory against the control:
// 终态相同、每任务实际执行次数不增加（可接回的原调用不重跑）、返修轮数不增。
func k5CompareTrajectories(t *testing.T, control, interrupted k5Trajectory) {
	t.Helper()
	if interrupted.Status != control.Status || interrupted.Terminal != control.Terminal {
		t.Errorf("interrupted run ended status=%s terminal=%q (err path), control ended status=%s terminal=%q: "+
			"a crash the run claims to absorb must not change the outcome",
			interrupted.Status, interrupted.Terminal, control.Status, control.Terminal)
	}
	tasks := map[string]bool{}
	for task := range control.Turns {
		tasks[task] = true
	}
	for task := range interrupted.Turns {
		tasks[task] = true
	}
	var order []string
	for task := range tasks {
		order = append(order, task)
	}
	sort.Strings(order)
	for _, task := range order {
		if interrupted.Turns[task] > control.Turns[task] {
			t.Errorf("task %s executed %d turns after the interruption, the uninterrupted control needed %d: "+
				"a reconnectable original call was re-run", task, interrupted.Turns[task], control.Turns[task])
		}
	}
	if interrupted.RepairRounds > control.RepairRounds {
		t.Errorf("interrupted repair rounds = %d, control = %d", interrupted.RepairRounds, control.RepairRounds)
	}
}

// ---- 场景夹具（owner 子进程 + 同配置重开）-----------------------------------

// invariantRequirement 与评审探针共用同一份措辞：绑定校验把 requirement 摘
// 进 run 身份，owner 与重开必须逐字一致。
const invariantRequirement = "Make state.txt contain ok. Do not change anything else."

// invariantScenarioConfig builds the A/B scenario's ComposeConfig: 绿链夹具
// （sampleProject + supervisorBinary + scripted peer），dev#1 不写实现迫使一
// 轮返修。checkArgv 为 nil 时用仓库自带的 check.sh。
func invariantScenarioConfig(t *testing.T, runID string, scripts composeScripts, checkArgv []string) ComposeConfig {
	t.Helper()
	t.Setenv("FORMAL_GATES_HARNESS_ROOT", t.TempDir())
	project, base := sampleProject(t)
	if checkArgv == nil {
		checkArgv = []string{"/bin/sh", "check.sh"}
	}
	cfg := ComposeConfig{
		Dir:   t.TempDir(),
		RunID: runID,
		Spec: ComposeSpec{
			Project:     project,
			Base:        base,
			Requirement: invariantRequirement,
			Checks:      []Check{{ID: "state", Argv: checkArgv}},
			Gates:       []string{"complexity-gate"},
			RepairLimit: 1,
		},
		Binary:      supervisorBinary(t),
		Parallelism: 2,
	}
	cfg.Binding = independentBinding(t, cfg, scripts, nil)
	return cfg
}

// invariantOpen opens (or reopens) a composition, closing it on cleanup.
func invariantOpen(t *testing.T, cfg ComposeConfig) *Compose {
	t.Helper()
	c, err := ComposeHarnessV1(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// invariantRunControl runs one scenario to its terminal without interruption,
// watching the single-writer invariant throughout. 对照组自己的形状也断言：
// 一次开发＋恰好一轮返修。对照组失败＝夹具坏了，直接 Fatal。
func invariantRunControl(t *testing.T, cfg ComposeConfig, wantDevTurns int) (k5Trajectory, ComposeState) {
	t.Helper()
	watch := startK5WriterWatch(cfg.Dir)
	c := invariantOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	state := k5DriveToTerminal(t, c, ctx)
	for _, finding := range watch.stop() {
		t.Errorf("single-writer violation while the uninterrupted control ran:\n%s", finding)
	}
	traj := k5SnapshotTrajectory(cfg.Dir, state)
	t.Logf("control trajectory: %s", traj)
	if state.Status != StatusComplete || state.Terminal != statusHarnessComplete {
		t.Fatalf("control run did not complete: %+v (err %v)", state, state.Err)
	}
	if traj.Turns[pathSafe(taskDevelopment)] != wantDevTurns {
		t.Fatalf("control development turns = %d, want %d (fixture shape): %s",
			traj.Turns[pathSafe(taskDevelopment)], wantDevTurns, traj)
	}
	assertK5Invariants(t, cfg.Dir, state)
	return traj, state
}

// k5InvariantOwnerSpec is the owner subprocess's scenario description: it
// rebuilds the exact ComposeConfig the test built (the reopen binding check
// refuses anything less).
type k5InvariantOwnerSpec struct {
	Dir, RunID, Binary, Project, Base, Mode string
	RepairLimit                             int
	CheckArgv                               []string
	RepairSleepSecs                         float64
}

// TestInvariantK5Owner is the crash-boundary owner, started as a subprocess of
// this same test binary (the startIndependentOwner pattern). Without
// K5_INVARIANT_OWNER it returns immediately. Modes:
//
//   - repair-in-flight: dev#1 不写实现、返修轮睡眠 RepairSleepSecs；binding
//     在返修调用启动时把 job 目录写进 cfg.Dir/invariant-repair-started。
//   - check-receipt: 默认脚本＋慢检查（CheckArgv 自带 sleep），供“收据落盘
//     后”边界同步 jobs/check-state/*/turn-*/process.json。
func TestInvariantK5Owner(t *testing.T) {
	file := os.Getenv("K5_INVARIANT_OWNER")
	if file == "" {
		return
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var spec k5InvariantOwnerSpec
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatal(err)
	}
	scripts := composeScripts{}
	if spec.Mode == "repair-in-flight" {
		scripts["development#1"] = scriptedAnswer{Status: "PASS", Summary: "nothing written", NoWrite: true}
		scripts["development#2"] = scriptedAnswer{Status: "PASS", Summary: "repair in flight", SleepSecs: spec.RepairSleepSecs}
	}
	cfg := ComposeConfig{
		Dir:    spec.Dir,
		RunID:  spec.RunID,
		Binary: spec.Binary,
		Spec: ComposeSpec{
			Project:     spec.Project,
			Base:        spec.Base,
			Requirement: invariantRequirement,
			Checks:      []Check{{ID: "state", Argv: spec.CheckArgv}},
			Gates:       []string{"complexity-gate"},
			RepairLimit: spec.RepairLimit,
		},
		Parallelism: 2,
	}
	cfg.Binding = independentBinding(t, cfg, scripts, func(call NativeCall) {
		if call.Role == "repair" {
			os.WriteFile(filepath.Join(cfg.Dir, "invariant-repair-started"), []byte(call.JobDir), 0600)
		}
	})
	c, err := ComposeHarnessV1(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	state := answerAllowing(t, c, context.Background(), nil)
	os.WriteFile(filepath.Join(cfg.Dir, "invariant-owner-ended"), []byte(fmt.Sprintf("%+v", state)), 0600)
}

// startInvariantOwner launches the owner subprocess and parks its cleanup.
func startInvariantOwner(t *testing.T, spec k5InvariantOwnerSpec) *exec.Cmd {
	t.Helper()
	body, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "invariant-owner.json")
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	bin, _ := os.Executable()
	cmd := exec.Command(bin, "-test.run=^TestInvariantK5Owner$", "-test.timeout=120s")
	cmd.Env = append(os.Environ(), "K5_INVARIANT_OWNER="+file)
	log, err := os.Create(filepath.Join(spec.Dir, "invariant-owner.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		log.Close()
	})
	return cmd
}

// ---- 3. 轨迹对照测试 --------------------------------------------------------

// TestInvariantK5HelperFabricatedFacts unit-checks the invariant helpers over
// fabricated run-directory facts: 每条助手对违例目录必须报出证据、对干净目
// 录必须静默。这保证轨迹测试里的红确实来自产品行为，不是助手失灵。
func TestInvariantK5HelperFabricatedFacts(t *testing.T) {
	dir := t.TempDir()
	writeTurn := func(task, invocation string, pid int, started float64, exit bool) string {
		turn := filepath.Join(dir, "jobs", task, invocation, "turn-01")
		if err := os.MkdirAll(turn, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeAtomicJSON(filepath.Join(turn, "process.json"),
			map[string]any{"pid": pid, "started_at": started}); err != nil {
			t.Fatal(err)
		}
		if exit {
			if err := writeAtomicJSON(filepath.Join(turn, "exit.json"), map[string]any{"exit_code": 0}); err != nil {
				t.Fatal(err)
			}
		}
		return turn
	}
	// 本测试进程的真实创建时间：伪造“身份存活”必须用它，不能用 now——
	// 全家系跑批时本进程已运行超过 2s 容差，now 会被正确判成 PID 复用。
	selfCreated, ok := processCreationTime(os.Getpid())
	if !ok {
		t.Fatal("cannot read this test process's own creation time")
	}
	selfStarted := float64(selfCreated.Unix())
	// 两个身份存活的开发树写入调用 → 单活写入者违例。
	writeTurn("development", "inv-a", os.Getpid(), selfStarted, false)
	writeTurn("development", "inv-b", os.Getpid(), selfStarted, false)
	if findings := k5SingleActiveWriterFindings(dir); len(findings) == 0 {
		t.Errorf("two simultaneous development writers were not reported")
	} else {
		t.Logf("single-writer findings:\n%s", strings.Join(findings, "\n"))
	}
	if findings := k5CompleteSettledFindings(dir, statusHarnessComplete); len(findings) == 0 {
		t.Errorf("HARNESS_COMPLETE over unsettled executions was not reported")
	}
	if findings := k5CompleteSettledFindings(dir, statusAborted); len(findings) != 0 {
		t.Errorf("the COMPLETE-scoped invariant fired on a non-COMPLETE terminal: %v", findings)
	}
	// 一个结算后 → 单活写入者成立；PID 存活但创建时间超出容差（复用）也
	// 不计活——同一判据的反向分支。
	writeTurn("development", "inv-b", os.Getpid(), selfStarted, true)
	writeTurn("development", "inv-c", os.Getpid(), selfStarted-3600, false)
	if findings := k5SingleActiveWriterFindings(dir); len(findings) != 0 {
		t.Errorf("one live writer plus one pid-reuse-shaped turn must satisfy the invariant: %v", findings)
	}
	// 同一 evaluate 判断驱动两轮返修 → 违例；只驱动一轮 → 通过。判断身份
	// 是代次（一个代次载荷一判断），ref 只做内容绑定。伪造走内核自己的
	// 写路径（ReviseTask），与真实运行落下的 task_revisions 行同源。
	ref := strings.Repeat("ab", 32)
	if err := os.MkdirAll(filepath.Join(dir, "outcomes", "evaluate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(dir, "outcomes", "evaluate", "1.json"),
		outcomeRecord{Task: taskEvaluate, Generation: 1, Route: "repair", Feedback: ref}); err != nil {
		t.Fatal(err)
	}
	kernel, err := runner.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer kernel.Close()
	kctx := context.Background()
	if err := kernel.CreateRun(kctx, "fabricated", []runner.TaskSpec{
		{TaskID: taskDevelopment, Input: []byte(`{"stage":"development","role":"development"}`)},
	}); err != nil {
		t.Fatal(err)
	}
	brief := fmt.Sprintf(`{"stage":"development","role":"repair","feedback":{"source":{"artifact":%q}}}`, ref)
	if err := kernel.ReviseTask(kctx, taskDevelopment, runner.TaskSpec{TaskID: taskDevelopment, Input: []byte(brief)}); err != nil {
		t.Fatal(err)
	}
	if findings := k5EvaluateSingleDriverFindings(dir); len(findings) != 0 {
		t.Errorf("one repair round driven by one feedback bundle must pass: %v", findings)
	}
	if err := kernel.ReviseTask(kctx, taskDevelopment, runner.TaskSpec{TaskID: taskDevelopment, Input: []byte(brief)}); err != nil {
		t.Fatal(err)
	}
	if findings := k5EvaluateSingleDriverFindings(dir); len(findings) == 0 {
		t.Errorf("the same feedback bundle driving a second repair round was not reported")
	} else {
		t.Logf("evaluate-driver findings:\n%s", strings.Join(findings, "\n"))
	}
	// 同内容、不同判断：第二个判断（代次 2）发布同一个 ref，两条修订行各
	// 归一个判断 → 必须通过。这是判据的正面分支——判断身份是代次，内容
	// 相同不合并两个判断（三轮评审 R5 的合法场景）。
	if err := writeAtomicJSON(filepath.Join(dir, "outcomes", "evaluate", "2.json"),
		outcomeRecord{Task: taskEvaluate, Generation: 2, Route: "repair", Feedback: ref}); err != nil {
		t.Fatal(err)
	}
	if findings := k5EvaluateSingleDriverFindings(dir); len(findings) != 0 {
		t.Errorf("two distinct verdicts sharing one feedback ref must each drive their own round: %v", findings)
	}
	// 封存候选与检查/门审结果不一致 → 违例；一致 → 通过。
	if err := os.MkdirAll(filepath.Join(dir, "outcomes", "check-state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(dir, "outcomes", "check-state", "1.json"),
		outcomeRecord{Task: checkTaskID("state"), Generation: 1, Candidate: "candidate-x"}); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(dir, "harness-result.json"),
		map[string]any{"candidate": "candidate-y", "status": statusHarnessComplete}); err != nil {
		t.Fatal(err)
	}
	if findings := k5SealBindsCandidateFindings(dir); len(findings) == 0 {
		t.Errorf("a check result bound to a different candidate than the seal was not reported")
	} else {
		t.Logf("seal-binding findings:\n%s", strings.Join(findings, "\n"))
	}
	// restart-session 已答：内核身份行残留 → 违例；清除后仅剩锚点后的
	// 请求检查（锚点为零值时跳过）。伪造走内核写路径（SetSession）。
	if err := kernel.SetSession(kctx, taskDevelopment, "s-1"); err != nil {
		t.Fatal(err)
	}
	if findings := k5RestartTaskFindings(dir, taskDevelopment, time.Time{}); len(findings) == 0 {
		t.Errorf("a remembered refused identity after restart-session was not reported")
	}
	if err := kernel.ClearSession(kctx, taskDevelopment); err != nil {
		t.Fatal(err)
	}
	if findings := k5RestartTaskFindings(dir, taskDevelopment, time.Time{}); len(findings) != 0 {
		t.Errorf("cleared identity must pass: %v", findings)
	}
}

// TestInvariantK5UninterruptedRepairTrajectory: 对照组——绿链带一轮返修跑
// 到底。当前必须全绿：绿链本来就该满足全部不变量；这里红了是夹具或既有
// 行为的回归，不是中断基线。
func TestInvariantK5UninterruptedRepairTrajectory(t *testing.T) {
	scripts := composeScripts{
		"development#1": {Status: "PASS", Summary: "nothing written", NoWrite: true},
	}
	cfg := invariantScenarioConfig(t, "inv-repair-control", scripts, nil)
	invariantRunControl(t, cfg, 2) // 一次开发＋恰好一轮返修
}

// TestInvariantK5InterruptedRepairInFlight: 边界一（R1）——返修 intent 落盘、
// 原返修调用在途时杀 owner，同配置重开续跑到终态。编写时为失败基线（评审
// R1：旧失败判断被重复消费，重复返修与提前 COMPLETE）；当前状态见文件头
// 对照表——R1 修复落地后本测试转绿即修复满足约束的确认。
//
// 边界同步点：invariant-repair-started marker（binding 在返修调用启动时写
// 下 job 目录）→ 该 job 的 native-process.json。杀在“intent 已绑定＋请求已
// 落盘＋进程身份已记录”之后——这是 intent 落盘后第一个可稳定观察的边界
// （intent 与 process 之间没有可同步的中间文件）。
func TestInvariantK5InterruptedRepairInFlight(t *testing.T) {
	controlScripts := composeScripts{
		"development#1": {Status: "PASS", Summary: "nothing written", NoWrite: true},
	}
	controlCfg := invariantScenarioConfig(t, "inv-repair-a-control", controlScripts, nil)
	controlTraj, _ := invariantRunControl(t, controlCfg, 2)

	// 中断版：重开后的 binding 用默认脚本（任何 development 调用都写 ok），
	// 与对照组业务语义一致；睡眠只存在于 owner 子进程里。
	cfg := invariantScenarioConfig(t, "inv-repair-b", nil, nil)
	owner := startInvariantOwner(t, k5InvariantOwnerSpec{
		Dir: cfg.Dir, RunID: cfg.RunID, Binary: cfg.Binary,
		Project: cfg.Spec.Project, Base: cfg.Spec.Base,
		Mode: "repair-in-flight", RepairLimit: cfg.Spec.RepairLimit,
		CheckArgv: cfg.Spec.Checks[0].Argv, RepairSleepSecs: 25,
	})
	marker := waitIndependentFile(t, cfg.Dir, "invariant-repair-started", 40*time.Second)
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	job := strings.TrimSpace(string(body))
	waitIndependentFile(t, job, "native-process.json", 10*time.Second)
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, job) // 测试结束后结算遗留的原返修监督者

	// 崩溃边界上的事实：此刻恰好一个活动写入者（原返修）。
	if findings := k5SingleActiveWriterFindings(cfg.Dir); len(findings) != 0 {
		t.Errorf("single-writer invariant broken at the crash boundary (only the original repair may be active):\n%s",
			strings.Join(findings, "\n"))
	}
	watch := startK5WriterWatch(cfg.Dir)
	c := invariantOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	state := k5DriveToTerminal(t, c, ctx)
	for _, finding := range watch.stop() {
		t.Errorf("single-writer violation while the recovered run advanced (duplicate call overlapped the original):\n%s", finding)
	}
	traj := k5SnapshotTrajectory(cfg.Dir, state)
	t.Logf("interrupted(repair-in-flight) trajectory: %s (control: %s)", traj, controlTraj)
	k5CompareTrajectories(t, controlTraj, traj)
	assertK5Invariants(t, cfg.Dir, state)
}

// TestInvariantK5UninterruptedCheckTrajectory: 对照组——慢检查（2s）绿链跑
// 到底，与“收据落盘后”边界的中断版共用同一配置。
func TestInvariantK5UninterruptedCheckTrajectory(t *testing.T) {
	checkArgv := []string{"/bin/sh", "-c", `sleep 2; test "$(cat state.txt)" = ok`}
	cfg := invariantScenarioConfig(t, "inv-check-control", nil, checkArgv)
	invariantRunControl(t, cfg, 1) // 一次开发，无返修（检查通过）
}

// TestInvariantK5InterruptedCheckReceipt: 边界二（R5）——检查收据落盘后杀
// owner，同配置重开续跑到终态。编写时为失败基线（评审 R5：已完成的检查/
// 门审接回被当普通 worker 处理，丢失领域结果）；当前状态见文件头对照表。
//
// 边界同步点：jobs/check-state/*/turn-*/process.json（检查监督者身份落
// 盘）→ 杀 owner → 等 exit.json（收据落盘）。此刻重开，接回应凭真实回执
// 接纳已完成检查；门审可能同刻在途或已结算，两种状态都必须被正确处置。
func TestInvariantK5InterruptedCheckReceipt(t *testing.T) {
	checkArgv := []string{"/bin/sh", "-c", `sleep 2; test "$(cat state.txt)" = ok`}
	controlCfg := invariantScenarioConfig(t, "inv-check-a-control", nil, checkArgv)
	controlTraj, _ := invariantRunControl(t, controlCfg, 1)

	cfg := invariantScenarioConfig(t, "inv-check-b", nil, checkArgv)
	owner := startInvariantOwner(t, k5InvariantOwnerSpec{
		Dir: cfg.Dir, RunID: cfg.RunID, Binary: cfg.Binary,
		Project: cfg.Spec.Project, Base: cfg.Spec.Base,
		Mode: "check-receipt", RepairLimit: cfg.Spec.RepairLimit,
		CheckArgv: checkArgv,
	})
	proc := waitIndependentFile(t, cfg.Dir, "jobs/check-state/*/turn-*/process.json", 40*time.Second)
	job := filepath.Dir(proc)
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, job)
	waitIndependentFile(t, job, "exit.json", 10*time.Second)

	watch := startK5WriterWatch(cfg.Dir)
	c := invariantOpen(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	state := k5DriveToTerminal(t, c, ctx)
	for _, finding := range watch.stop() {
		t.Errorf("single-writer violation while the recovered run advanced:\n%s", finding)
	}
	traj := k5SnapshotTrajectory(cfg.Dir, state)
	t.Logf("interrupted(check-receipt) trajectory: %s (control: %s)", traj, controlTraj)
	k5CompareTrajectories(t, controlTraj, traj)
	assertK5Invariants(t, cfg.Dir, state)
}
