//go:build !windows

package harness

// 运行不变量与中断/不中断轨迹对照测试：可复用的不变量断言（任何测试
// 都可直接调用）＋同一场景的两遍对照。
//
// 五条业务不变量（每条都有“违反时给出证据路径”的检查函数）：
//
//   - 单活写入者：同一开发工作区至多一个活动写入调用。写入者只有
//     development 任务（development 与 repair 都是它的世代）；检查在各自
//     一次性克隆里跑、门审只读，因此不算这个工作区的写入者。
//   - evaluate 单驱动：每个 evaluate 判断
//     （按代次，outcomes/evaluate/<gen>.json 一判断）至多驱动一轮业务返修
//     ——业务返修的唯一权威事实是内核 user_requests 的业务 revision 行
//     （RequestRevision 落的：直批行 state=executed、问询行经答案结算）。
//     消费计数只数这些行绑定的修订输入：普通 retry/continue 的再武装走裸
//     ReviseTask、不落 user_requests，天然排除。反馈 ref 只是内容摘要（同
//     发现＋同候选 ⇒ 同 ref），负责内容绑定，不是判断身份：不同判断共享
//     内容合法，同一判断驱动两次违例。
//   - COMPLETE 零未结算：终态 HARNESS_COMPLETE 时，jobs/ 下不存在
//     “收据缺失（无 exit.json）且进程身份存在（有 process.json）”的目录。
//   - 结果绑候选：seal 引用的每个 check/gate 结果都绑定同一个最终
//     候选（outcomes 里对应任务的 Candidate 与 harness-result.json 一致）。
//   - 重建换身份：restart-session 已答后，该任务在内核 task_sessions
//     的身份行必须已清除，其后发出的 native-request.json Identity 必须为空。
//
// “活动”的判据：process.json 存在 && exit.json 缺失，
// 且记录的 PID 仍对 signal 0 应答。PID＋创建时间容差（±2s，同
// supervisorAlive）用来排除 PID 复用；kill(pid,0) 对未被收割的僵尸进程同样
// 成功——收据缺失＋僵尸与活监督者不可分辨，按“无法证明已结算”计活并
// 在证据里如实注明，绝不猜“应该已经结束”。
//
// 轨迹对照：同一场景跑两遍——不中断跑到底记录终态/每任务 turn 数/返修
// 轮数；中断版在可观察边界杀掉 owner 子进程（test 二进制重入＋环境变
// 量＋marker 文件同步），用同一 ComposeConfig 重开续跑到终态，断言终态
// 相同、每任务执行次数不增加、全部不变量成立。

// ─────────────────────────────────────────────────────────────────────────────
// 行为对照表：业务行为 → 验证它的测试（导航用，逐行注明谁在验证）
//
// │ 业务行为                         │ 验证测试                                                                   │
// │──────────────────────────────────│────────────────────────────────────────────────────────────────────────────│
// │ 主干绿链封存归档                 │ TestWorkflowGreenChainSealsAndArchives；TestUninterruptedRepairRunHoldsInvariants / TestUninterruptedCheckRunHoldsInvariants（对照组） │
// │ 需求确认（首停即需求门）         │ TestWorkflowGreenChain…（首停即需求门）                                    │
// │ 前置审 FAIL 拦截                 │ TestUpstreamFAILHoldsTheChain、TestWorkflowPreReviewFailAskRetryAbort      │
// │ 前置审 FAIL 重试（反馈再跑）     │ TestWorkflowPreReviewRetryThenPass                                         │
// │ 计划确认工件可读                 │ TestWorkflowGreenChain…（body 字段）                                       │
// │ 计划确认指向真实落盘工件         │ TestPlanApprovalReferencesReadableEvidence                                 │
// │ CONTINUE 续轮（续跑反馈）        │ 判定层 domain_test/contract_test；组合层 TestLiveContinuationCarriesUnfinishedWorkFeedback、TestRecoveredContinuationKeepsSessionAndFeedback、TestRecoveredContinuationNeedsNoReplacement │
// │ BLOCKED 处置                     │ TestRuntimeStopsSurfaceRetryQuestion/gate-blocked                          │
// │ 检查运行错误处置（ask）          │ TestRuntimeStopsSurfaceRetryQuestion/check-runtime                         │
// │ 运行时错误重试                   │ TestWorkflowContractRejectedIsARuntimeQuestion、TestWorkflowCrashTakeoverClassifications │
// │ 会话失效→重建询问                │ TestWorkflowSessionRefusalAsksRestart                                      │
// │ 重建后换执行身份                 │ TestRestartSessionOpensFreshIdentity；restartTaskFindings（不变量助手）    │
// │ QA 设计/评审循环                 │ TestWorkflowQADesignLoop、TestWorkflowQADesignFailReviewPassAsksDesignRetry │
// │ 开发未决发现拦截                 │ TestWorkflowDevelopmentFindingsAsk                                         │
// │ 返修循环 evaluate→repair→candidate │ TestWorkflowRepairRoundDrivesNewCandidate、TestWorkflowRepairLimitAskContinueAndAbort、TestGateFindingsEnterTheRepairLoop、TestRepairBriefCarriesItsRoundsFindings；TestInterruptedRepairInFlightRecoversToSameTerminal + evaluate 单驱动（判断按代次计，同内容多判断合法） │
// │ 崩溃接回（收据轮询、不可证明不盲重） │ TestWorkflowCrashTakeoverClassifications；TestInterruptedRepairInFlightRecoversToSameTerminal / TestInterruptedAfterCheckReceiptRecoversToSameTerminal │
// │ 已完成检查/门审接回（同候选接纳） │ TestCrashAfterCheckReceiptAdoptsCheckResult / TestCrashAfterGateReceiptAdoptsGateVerdict；TestInterruptedAfterCheckReceiptRecoversToSameTerminal + 结果绑候选不变量 │
// │ 恢复后的 Held 权限请求           │ TestRecoveredHeldInteractionIsSurfaced、TestRecoveredGatePermissionIsHeldForUser │
// │ 取消结算                         │ TestWorkflowCancelSettlesEveryInFlightCall、TestCancelSettlesRecoveredInFlightCall │
// │ 归档（物理化 + 拒绝非终态）      │ TestWorkflowGreenChain…（归档段）、TestArchiveCreatesVerifiedArtifact、TestWorkflowArchiveRefusesNonTerminal │
// │ 重开绑定不可变                   │ TestWorkflowReopenBindingRefused、TestReopenWithChangedBindingIsRefused    │
// │ 真实宿主全链路                   │ TestWorkflowLiveAcceptanceGreenChain（live 门）                            │
// │ 单活写入者 / COMPLETE 零未结算   │ singleActiveWriterFindings、completeSettledFindings（不变量助手，随各场景断言） │
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

// turnFact is one supervised turn's settlement fact, read from the run
// directory (no Compose needed — the same scan works on a live run, a reopened
// run and a post-mortem directory).
type turnFact struct {
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

// scanTurnFacts walks jobs/<task>/<invocation>/turn-*/ and reads each turn's
// settlement facts. 目录名到任务身份的换算沿用 pathSafe 的落盘事实。
func scanTurnFacts(runDir string) []turnFact {
	var facts []turnFact
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
				fact := turnFact{Task: task, Dir: dir}
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

// singleActiveWriterFindings checks 单活写入者：同一开发工作区至多一个
// 活动写入调用。写入者＝jobs/development/ 下的 turn（development 与 repair
// 都是 development 任务的世代）；检查任务写各自的一次性克隆、门审只读，
// 不写这个工作区。返回的每条 finding 都是带证据路径的违例。
func singleActiveWriterFindings(runDir string) []string {
	var actives []turnFact
	for _, fact := range scanTurnFacts(runDir) {
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

// evaluateVerdict is one evaluate verdict's identity facts: the
// generation it ran at and the feedback bundle ref it published.
type evaluateVerdict struct {
	// Generation is 判断的代次身份——outcomes/evaluate/<gen>.json 的 gen。
	Generation int64
	// Ref is 反馈包的内容摘要（artifacts/<ref>.json），只做内容绑定。
	Ref string
}

// evaluateVerdicts reads every evaluate verdict from the outcome tree,
// one per generation payload (outcomes/evaluate/<gen>.json), sorted by
// generation. 判断的身份是代次，不是 ref：ref 是内容摘要——同样的失败
// 发现加未变的候选哈希出同一个 ref，所以“两轮返修都没让发现变化”这条
// 合法轨迹会由两个不同判断发布同一个 ref。一个代次恰有一个载荷文件，
// 文件集就是判断的全集。
func evaluateVerdicts(runDir string) []evaluateVerdict {
	paths, _ := filepath.Glob(filepath.Join(runDir, "outcomes", "evaluate", "*.json"))
	var verdicts []evaluateVerdict
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record outcomeRecord
		if json.Unmarshal(body, &record) != nil || record.Feedback == "" {
			continue
		}
		verdicts = append(verdicts, evaluateVerdict{Generation: record.Generation, Ref: record.Feedback})
	}
	sort.Slice(verdicts, func(i, j int) bool { return verdicts[i].Generation < verdicts[j].Generation })
	return verdicts
}

// businessRevisionInputs reads one run's BUSINESS revision rounds: the
// task_revisions rows that rode a RequestRevision execution — joined, in
// the kernel's own storage, to the executed user_requests row whose grant
// wrote them (the request's target generation + 1 is the generation the
// revision bumped the task to; reviseScope writes both in one
// transaction). Ordinary re-arms (a blocked retry, a continuation) revise
// through bare ReviseTask and land no request row, so they never appear
// here: this is the consumed-side spelling of the same fact
// Store.RevisionRounds counts. 任何读失败如实报错：单驱动不变量的证据
// 源不可读本身就是要报告的事实。
func businessRevisionInputs(runDir string) ([]runner.RevisionInput, error) {
	db, err := sql.Open("sqlite", databaseURI(filepath.Join(runDir, "run.db")))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT t.task_id, t.generation, t.input
		  FROM task_revisions t
		  JOIN user_requests r
		    ON r.run_id = t.run_id AND r.task_id = t.task_id
		   AND r.kind = 'revision' AND r.state = 'executed'
		   AND t.generation = r.target_generation + 1
		 ORDER BY t.task_id, t.generation`)
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

// evaluateSingleDriverFindings checks evaluate 单驱动 by verdict
// identity over business repair facts: 每个 evaluate 判断（一个代次）至多
// 驱动一轮业务返修。业务返修＝内核 user_requests 业务 revision 行绑定的
// 修订输入（businessRevisionInputs——返修 brief 逐字内嵌 wrapper 的
// source.artifact，所以按 ref 字符串匹配）；普通 retry/continue 的再武装
// 不落 user_requests，不计入。ref 只把判断绑定到消费了它的修订行。
//
// 判据：对每个 ref，绑定它的业务修订行数 ≤ 发布过该 ref 的判断数（按
// 代次计，一个代次载荷一判断）。为什么这是“一项判断只消费一次”而“同
// 内容多判断合法”：业务修订行和判断都随轮次单调前移——每轮业务返修把
// 开发任务的请求序列和 evaluate 的代次一起推一格——所以第 i 条业务行只
// 可能由第 i 个或更早的同 ref 判断驱动；行数一旦超过判断数，必有某个
// 判断驱动了第二轮（一项判断被消费两次），必报。行数不超过判断数时，
// 每条行业务行都能配到一个单独的判断，两个判断共享同一个 ref 各走各的
// 一轮，合法——判断身份（代次）与内容身份（ref）分开，内容相等绝不合并
// 两个判断。业务行与请求落库在同一事务，内核读不到本身就是要报告的事实。
func evaluateSingleDriverFindings(runDir string) []string {
	verdicts := evaluateVerdicts(runDir)
	if len(verdicts) == 0 {
		return nil
	}
	rows, err := businessRevisionInputs(runDir)
	if err != nil {
		return []string{"run.db business revision rows are unreadable: " + err.Error()}
	}
	issuers := map[string][]int64{} // ref → 发布过它的判断代次（升序）
	for _, verdict := range verdicts {
		issuers[verdict.Ref] = append(issuers[verdict.Ref], verdict.Generation)
	}
	evidence := map[string][]string{} // ref → 绑定了它的业务修订行（按行序）
	for i, row := range rows {
		brief := string(row.Input)
		for ref := range issuers {
			if strings.Contains(brief, ref) {
				evidence[ref] = append(evidence[ref], fmt.Sprintf(
					"  run.db business revision row %d: task=%s generation=%d binds artifacts/%s.json",
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
			continue // 每条业务行都配得到各自的判断：一轮一判断，合法
		}
		generations := make([]string, 0, len(issuers[ref]))
		for _, gen := range issuers[ref] {
			generations = append(generations, fmt.Sprintf("%d", gen))
		}
		findings = append(findings, fmt.Sprintf(
			"artifacts/%s.json was bound by %d business revision rounds but only %d evaluate verdict(s) (generation(s) %s) issued it — one verdict drove more than its single round:\n%s",
			ref, consumed, issued, strings.Join(generations, ","), strings.Join(evidence[ref], "\n")))
	}
	return findings
}

// completeSettledFindings checks COMPLETE 零未结算：终态 HARNESS_COMPLETE 时
// jobs/ 全部目录中不存在“收据缺失且进程身份存在”者。判据只看文件事实
// （process.json 在、exit.json 缺），进程死活写进证据——死了没收据同样是洞。
// 非COMPLETE 终态不适用本条，直接通过。
func completeSettledFindings(runDir, terminal string) []string {
	if terminal != statusHarnessComplete {
		return nil
	}
	var findings []string
	for _, fact := range scanTurnFacts(runDir) {
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

// kernelGenerations reads the kernel's current generation per task,
// keyed by the outcome tree's storage name (pathSafe of the logical
// task id) — the authority for "which outcome of this task is currently
// effective". 读不到内核＝空表：孤儿载荷目录按自身文件核对。
func kernelGenerations(runDir string) map[string]int64 {
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

// effectiveOutcomeRecord reads one outcome directory's currently effective
// record: the payload at the kernel's current generation when the kernel
// knows the task, else the directory's newest payload by generation —
// the only evidence an orphan directory (no kernel row) offers, so a
// fabricated result still faces the check.
func effectiveOutcomeRecord(runDir, name string, generation int64) (outcomeRecord, bool) {
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

// sealBindsCandidateFindings checks 结果绑候选：harness-result.json 的候选
// 与 seal 所引用的每个 check/gate 结果的 Candidate 一致——哪个结果有效由
// 内核当前代决定，孤儿载荷目录按其全部载荷核对。缺失候选（空串）同样
// 违例——接回接纳不写 Candidate 同样是错。
func sealBindsCandidateFindings(runDir string) []string {
	value, err := readJSONFile(filepath.Join(runDir, "harness-result.json"))
	if err != nil {
		return nil // 没有封存结果：终态断言管那条，这里不重复
	}
	candidate := textOf(getOf(value, "candidate"))
	if candidate == "" {
		return []string{"harness-result.json carries no candidate"}
	}
	generations := kernelGenerations(runDir)
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
		record, ok := effectiveOutcomeRecord(runDir, name, generations[name])
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

// sessionIdentity reads one task's remembered session identity from the
// run directory's database (read-only) — 重建换身份的证据源。空串＝该任务
// 当前无被记住的执行身份（一个不存在的数据库同样读作空：没有 run.db 的
// 目录不曾有过会话）。
func sessionIdentity(runDir, task string) string {
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

// restartTaskFindings checks 重建换身份 for one task after the user answered
// restart-session: (a) 内核 task_sessions 里该任务的身份行必须已清除；(b)
// 若调用方给出回答时刻的锚点（answeredAt 非零），其后落盘的
// native-request.json 的 Identity 必须为空。锚点用文件 mtime 比对——回答是
// 调用方亲历的事实，它知道时刻；行本身无时间戳。
func restartTaskFindings(runDir, task string, answeredAt time.Time) []string {
	var findings []string
	if identity := sessionIdentity(runDir, task); identity != "" {
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

// restartAnsweredFindings derives 重建换身份 from the kernel's own
// event order (events.seq — no wall clocks): for every task the user ever
// answered restart-session,
//
//   - the grant's REPLACEMENT — an invocation admitted after the answer
//     with no task_revised event for the task in between (a revision
//     separates its invocation from the grant: a blocked retry or a
//     continuation re-arm legitimately resumes the recorded identity) —
//     must have opened FRESH: its first turn's native request carries no
//     identity;
//   - a task with no admitted invocation after the answer has had no new
//     result, so the grant's row clear is its last session fact and a
//     standing task_sessions row is the refused identity surviving.
//
// A result admitted after the answer legitimately records its own
// identity (the session rule's single update point), so the bare row
// check never runs against one. The strong per-call form with the
// caller's exact time anchor stays restartTaskFindings.
func restartAnsweredFindings(runDir string) []string {
	db, err := sql.Open("sqlite", databaseURI(filepath.Join(runDir, "run.db")))
	if err != nil {
		return nil
	}
	defer db.Close()
	type invocationFact struct {
		id  string
		seq int64
	}
	answerSeq := map[string]int64{}    // task → its last restart answer's event seq
	revisedSeq := map[string][]int64{} // task → its task_revised event seqs
	admitted := map[string][]invocationFact{}
	rows, err := db.Query(`SELECT seq, kind, payload_json FROM events ORDER BY seq`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var kind, payload string
		if err := rows.Scan(&seq, &kind, &payload); err != nil {
			return nil
		}
		switch kind {
		case "decision_answered", "request_answered":
			var fact struct {
				TaskID string `json:"task_id"`
				Choice string `json:"choice"`
			}
			if json.Unmarshal([]byte(payload), &fact) != nil || fact.Choice != "restart-session" {
				continue
			}
			answerSeq[strings.TrimSuffix(fact.TaskID, askSuffix)] = seq
		case "task_revised":
			var fact struct {
				TaskID string `json:"task_id"`
			}
			if json.Unmarshal([]byte(payload), &fact) != nil {
				continue
			}
			task := strings.TrimSuffix(fact.TaskID, askSuffix)
			revisedSeq[task] = append(revisedSeq[task], seq)
		case "invocation_admitted":
			var fact struct {
				InvocationID string `json:"invocation_id"`
				TaskID       string `json:"task_id"`
			}
			if json.Unmarshal([]byte(payload), &fact) != nil {
				continue
			}
			task := strings.TrimSuffix(fact.TaskID, askSuffix)
			admitted[task] = append(admitted[task], invocationFact{id: fact.InvocationID, seq: seq})
		}
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	var tasks []string
	for task := range answerSeq {
		tasks = append(tasks, task)
	}
	sort.Strings(tasks)
	separated := func(task string, from, to int64) bool {
		for _, seq := range revisedSeq[task] {
			if seq > from && seq < to {
				return true
			}
		}
		return false
	}
	var findings []string
	for _, task := range tasks {
		answer := answerSeq[task]
		replaced := false
		for _, invocation := range admitted[task] {
			if invocation.seq <= answer || separated(task, answer, invocation.seq) {
				continue
			}
			replaced = true
			first := filepath.Join(runDir, "jobs", pathSafe(task), invocation.id, "turn-01", "native-request.json")
			value, err := readJSONFile(first)
			if err != nil {
				continue // no request survived: nothing to judge here
			}
			if identity := textOf(getOf(value, "identity")); identity != "" {
				findings = append(findings, fmt.Sprintf(
					"  %s: the restart replacement (%s) resumed the refused identity %q",
					first, invocation.id, identity))
			}
		}
		if replaced {
			continue
		}
		findings = append(findings, restartTaskFindings(runDir, task, time.Time{})...)
	}
	if len(findings) > 0 {
		return append([]string{"restart-session answers did not produce fresh replacement identities:"}, findings...)
	}
	return nil
}

// assertRunInvariants runs the full invariant set over one terminal observation
// of a scenario. 违例逐条以证据路径报出；全部成立则静默通过。
func assertRunInvariants(t *testing.T, runDir string, state ComposeState) {
	t.Helper()
	checks := []struct {
		name     string
		findings []string
	}{
		{"single active writer", singleActiveWriterFindings(runDir)},
		{"evaluate drives one repair round", evaluateSingleDriverFindings(runDir)},
		{"HARNESS_COMPLETE settles every own execution", completeSettledFindings(runDir, state.Terminal)},
		{"sealed result binds one candidate", sealBindsCandidateFindings(runDir)},
		{"restart-session clears the execution identity", restartAnsweredFindings(runDir)},
	}
	for _, check := range checks {
		if len(check.findings) > 0 {
			t.Errorf("invariant %q violated:\n%s", check.name, strings.Join(check.findings, "\n"))
		}
	}
}

// ---- 2. 轨迹对照 ------------------------------------------------------------

// runTrajectory is one scenario run's observable shape: the stop it ended on and
// the per-task execution counts (turn directories) plus repair rounds.
type runTrajectory struct {
	Status       ComposeStatus
	Terminal     string
	Turns        map[string]int
	RepairRounds int
}

// snapshotTrajectory reads a run directory's trajectory facts. The
// repair-round count is the business fact — development's executed
// user_requests revision rows (businessRevisionInputs, the same rows
// Store.RevisionRounds reads) — never a count of result files: a parked
// repair result is not a completed round.
func snapshotTrajectory(runDir string, state ComposeState) runTrajectory {
	traj := runTrajectory{Status: state.Status, Terminal: state.Terminal, Turns: map[string]int{}}
	for _, fact := range scanTurnFacts(runDir) {
		traj.Turns[fact.Task]++
	}
	if rows, err := businessRevisionInputs(runDir); err == nil {
		for _, row := range rows {
			if row.TaskID == taskDevelopment {
				traj.RepairRounds++
			}
		}
	}
	return traj
}

func (traj runTrajectory) String() string {
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

// driveToTerminal drives allowing answers until a terminal/failed stop.
// StatusObserved polls（被接回观察的原调用会自行结算）；fixture 不会出现
// Held 交互。原调用持续观察到真实业务终态或失败。
func driveToTerminal(t *testing.T, c *Compose, ctx context.Context) ComposeState {
	t.Helper()
	for {
		last := c.Drive(ctx)
		switch last.Status {
		case StatusWaitingDecision:
			if len(last.Pending) == 0 {
				return last
			}
			for _, question := range last.Pending {
				choice := allowingChoice(question.Options)
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
}

// allowingChoice picks a question's allowing choice（approve/continue/retry/
// restart-session），与 answerAllowing 同一词汇表。
func allowingChoice(options []string) string {
	for _, option := range options {
		switch option {
		case "approve", "continue", "retry", "restart-session":
			return option
		}
	}
	return ""
}

// writerWatch samples the single-writer invariant while a run advances:
// 重复调用只在两调用同时在途时可见，终态扫描会错过重叠窗口。采样只可能
// 多报违例（两个未结算的开发树 turn 本身就是重叠证据），不会漏掉后让测试
// 假绿——次数对照与未结算检查兜底。
type writerWatch struct {
	mu       sync.Mutex
	findings []string
	done     chan struct{}
}

func startWriterWatch(runDir string) *writerWatch {
	watch := &writerWatch{done: make(chan struct{})}
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
			findings := singleActiveWriterFindings(runDir)
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

func (watch *writerWatch) stop() []string {
	close(watch.done)
	watch.mu.Lock()
	defer watch.mu.Unlock()
	return watch.findings
}

// compareTrajectories asserts the interrupted trajectory against the control:
// 终态相同、每任务实际执行次数不增加（可接回的原调用不重跑）、返修轮数不增。
func compareTrajectories(t *testing.T, control, interrupted runTrajectory) {
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

// invariantRequirement 是本组夹具的固定措辞：绑定校验把 requirement 摘进
// run 身份，owner 与重开必须逐字一致。
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
	cfg.Binding = scriptedBinding(t, cfg, scripts, nil)
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
func invariantRunControl(t *testing.T, cfg ComposeConfig, wantDevTurns int) (runTrajectory, ComposeState) {
	t.Helper()
	watch := startWriterWatch(cfg.Dir)
	c := invariantOpen(t, cfg)
	ctx := context.Background()
	state := driveToTerminal(t, c, ctx)
	for _, finding := range watch.stop() {
		t.Errorf("single-writer violation while the uninterrupted control ran:\n%s", finding)
	}
	traj := snapshotTrajectory(cfg.Dir, state)
	t.Logf("control trajectory: %s", traj)
	if state.Status != StatusComplete || state.Terminal != statusHarnessComplete {
		t.Fatalf("control run did not complete: %+v (err %v)", state, state.Err)
	}
	if traj.Turns[pathSafe(taskDevelopment)] != wantDevTurns {
		t.Fatalf("control development turns = %d, want %d (fixture shape): %s",
			traj.Turns[pathSafe(taskDevelopment)], wantDevTurns, traj)
	}
	assertRunInvariants(t, cfg.Dir, state)
	return traj, state
}

// invariantOwnerSpec is the owner subprocess's scenario description: it
// rebuilds the exact ComposeConfig the test built (the reopen binding check
// refuses anything less).
type invariantOwnerSpec struct {
	Dir, RunID, Binary, Project, Base, Mode string
	RepairLimit                             int
	CheckArgv                               []string
	RepairSleepSecs                         float64
}

// TestInvariantOwnerSubprocess is the crash-boundary owner, started as a subprocess of
// this same test binary (the startRecoveryOwner pattern). Without
// INVARIANT_OWNER_SPEC it returns immediately. Modes:
//
//   - repair-in-flight: dev#1 不写实现、返修轮睡眠 RepairSleepSecs；binding
//     在返修调用启动时把 job 目录写进 cfg.Dir/invariant-repair-started。
//   - check-receipt: 默认脚本＋慢检查（CheckArgv 自带 sleep），供“收据落盘
//     后”边界同步 jobs/check-state/*/turn-*/process.json。
func TestInvariantOwnerSubprocess(t *testing.T) {
	file := os.Getenv("INVARIANT_OWNER_SPEC")
	if file == "" {
		return
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var spec invariantOwnerSpec
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
	cfg.Binding = scriptedBinding(t, cfg, scripts, func(call NativeCall) {
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
func startInvariantOwner(t *testing.T, spec invariantOwnerSpec) *exec.Cmd {
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
	cmd := exec.Command(bin, "-test.run=^TestInvariantOwnerSubprocess$", "-test.timeout=0")
	cmd.Env = append(os.Environ(), "INVARIANT_OWNER_SPEC="+file)
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

// TestInvariantHelpersOnFabricatedFacts unit-checks the invariant helpers over
// fabricated run-directory facts: 每条助手对违例目录必须报出证据、对干净目
// 录必须静默。这保证轨迹测试里的红确实来自产品行为，不是助手失灵。
func TestInvariantHelpersOnFabricatedFacts(t *testing.T) {
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
	if findings := singleActiveWriterFindings(dir); len(findings) == 0 {
		t.Errorf("two simultaneous development writers were not reported")
	} else {
		t.Logf("single-writer findings:\n%s", strings.Join(findings, "\n"))
	}
	if findings := completeSettledFindings(dir, statusHarnessComplete); len(findings) == 0 {
		t.Errorf("HARNESS_COMPLETE over unsettled executions was not reported")
	}
	if findings := completeSettledFindings(dir, statusAborted); len(findings) != 0 {
		t.Errorf("the COMPLETE-scoped invariant fired on a non-COMPLETE terminal: %v", findings)
	}
	// 一个结算后 → 单活写入者成立；PID 存活但创建时间超出容差（复用）也
	// 不计活——同一判据的反向分支。
	writeTurn("development", "inv-b", os.Getpid(), selfStarted, true)
	writeTurn("development", "inv-c", os.Getpid(), selfStarted-3600, false)
	if findings := singleActiveWriterFindings(dir); len(findings) != 0 {
		t.Errorf("one live writer plus one pid-reuse-shaped turn must satisfy the invariant: %v", findings)
	}
	// 同一 evaluate 判断驱动两轮业务返修 → 违例；只驱动一轮 → 通过。判断身份
	// 是代次（一个代次载荷一判断），ref 只做内容绑定。伪造走内核自己的业务
	// 写路径（RequestRevision 直批），与真实运行落下的 user_requests＋
	// task_revisions 行同源；普通裸 ReviseTask 的再武装不落请求行，不算业务
	// 返修——这正是业务返修与普通再武装的边界。
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
	approve := func(round, allowance int) bool { return false } // direct approval below the allowance
	if err := kernel.RequestRevision(kctx, taskDevelopment,
		runner.TaskSpec{TaskID: taskDevelopment, Input: []byte(brief)}, nil, approve, 1); err != nil {
		t.Fatal(err)
	}
	if findings := evaluateSingleDriverFindings(dir); len(findings) != 0 {
		t.Errorf("one business repair round driven by one feedback bundle must pass: %v", findings)
	}
	if err := kernel.RequestRevision(kctx, taskDevelopment,
		runner.TaskSpec{TaskID: taskDevelopment, Input: []byte(brief)}, nil, approve, 1); err != nil {
		t.Fatal(err)
	}
	if findings := evaluateSingleDriverFindings(dir); len(findings) == 0 {
		t.Errorf("the same feedback bundle driving a second business repair round was not reported")
	} else {
		t.Logf("evaluate-driver findings:\n%s", strings.Join(findings, "\n"))
	}
	// 同内容、不同判断：第二个判断（代次 2）发布同一个 ref，两条业务行各
	// 归一个判断 → 必须通过。这是判据的正面分支——判断身份是代次，内容
	// 相同不合并两个判断（同内容多判断的合法场景）。
	if err := writeAtomicJSON(filepath.Join(dir, "outcomes", "evaluate", "2.json"),
		outcomeRecord{Task: taskEvaluate, Generation: 2, Route: "repair", Feedback: ref}); err != nil {
		t.Fatal(err)
	}
	if findings := evaluateSingleDriverFindings(dir); len(findings) != 0 {
		t.Errorf("two distinct verdicts sharing one feedback ref must each drive their own round: %v", findings)
	}
	// 普通再武装不是业务返修：一条裸 ReviseTask 行绑定了同一个 ref（一个
	// blocked retry 或 continuation 的再武装就是这条形状），不落请求行、不
	// 计消费——把它计成第二轮就是误报。
	if err := kernel.ReviseTask(kctx, taskDevelopment,
		runner.TaskSpec{TaskID: taskDevelopment, Input: []byte(brief)}); err != nil {
		t.Fatal(err)
	}
	if findings := evaluateSingleDriverFindings(dir); len(findings) != 0 {
		t.Errorf("an ordinary bare re-arm binding the same ref is not a business repair round: %v", findings)
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
	if findings := sealBindsCandidateFindings(dir); len(findings) == 0 {
		t.Errorf("a check result bound to a different candidate than the seal was not reported")
	} else {
		t.Logf("seal-binding findings:\n%s", strings.Join(findings, "\n"))
	}
	// restart-session 已答：内核身份行残留 → 违例；清除后仅剩锚点后的
	// 请求检查（锚点为零值时跳过）。伪造走内核写路径（SetSession）。
	if err := kernel.SetSession(kctx, taskDevelopment, "s-1"); err != nil {
		t.Fatal(err)
	}
	if findings := restartTaskFindings(dir, taskDevelopment, time.Time{}); len(findings) == 0 {
		t.Errorf("a remembered refused identity after restart-session was not reported")
	}
	if err := kernel.ClearSession(kctx, taskDevelopment); err != nil {
		t.Fatal(err)
	}
	if findings := restartTaskFindings(dir, taskDevelopment, time.Time{}); len(findings) != 0 {
		t.Errorf("cleared identity must pass: %v", findings)
	}
}

// TestUninterruptedRepairRunHoldsInvariants: 对照组——绿链带一轮返修跑
// 到底。当前必须全绿：绿链本来就该满足全部不变量；这里红了是夹具或既有
// 行为的回归，不是中断基线。
func TestUninterruptedRepairRunHoldsInvariants(t *testing.T) {
	scripts := composeScripts{
		"development#1": {Status: "PASS", Summary: "nothing written", NoWrite: true},
	}
	cfg := invariantScenarioConfig(t, "inv-repair-control", scripts, nil)
	invariantRunControl(t, cfg, 2) // 一次开发＋恰好一轮返修
}

// TestInterruptedRepairInFlightRecoversToSameTerminal: 返修 intent 落盘、
// 原返修调用在途时杀 owner，同配置重开续跑到终态：一次判断一轮返修，
// 重开不重复消费旧失败判断，也不提前 COMPLETE。
//
// 边界同步点：invariant-repair-started marker（binding 在返修调用启动时写
// 下 job 目录）→ 该 job 的 native-process.json。杀在“intent 已绑定＋请求已
// 落盘＋进程身份已记录”之后——这是 intent 落盘后第一个可稳定观察的边界
// （intent 与 process 之间没有可同步的中间文件）。
func TestInterruptedRepairInFlightRecoversToSameTerminal(t *testing.T) {
	controlScripts := composeScripts{
		"development#1": {Status: "PASS", Summary: "nothing written", NoWrite: true},
	}
	controlCfg := invariantScenarioConfig(t, "inv-repair-a-control", controlScripts, nil)
	controlTraj, _ := invariantRunControl(t, controlCfg, 2)

	// 中断版：重开后的 binding 用默认脚本（任何 development 调用都写 ok），
	// 与对照组业务语义一致；睡眠只存在于 owner 子进程里。
	cfg := invariantScenarioConfig(t, "inv-repair-b", nil, nil)
	owner := startInvariantOwner(t, invariantOwnerSpec{
		Dir: cfg.Dir, RunID: cfg.RunID, Binary: cfg.Binary,
		Project: cfg.Spec.Project, Base: cfg.Spec.Base,
		Mode: "repair-in-flight", RepairLimit: cfg.Spec.RepairLimit,
		CheckArgv: cfg.Spec.Checks[0].Argv, RepairSleepSecs: 25,
	})
	marker := waitForPattern(t, cfg.Dir, "invariant-repair-started")
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	job := strings.TrimSpace(string(body))
	waitForPattern(t, job, "native-process.json")
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, job) // 测试结束后结算遗留的原返修监督者

	// 崩溃边界上的事实：此刻恰好一个活动写入者（原返修）。
	if findings := singleActiveWriterFindings(cfg.Dir); len(findings) != 0 {
		t.Errorf("single-writer invariant broken at the crash boundary (only the original repair may be active):\n%s",
			strings.Join(findings, "\n"))
	}
	watch := startWriterWatch(cfg.Dir)
	c := invariantOpen(t, cfg)
	ctx := context.Background()
	state := driveToTerminal(t, c, ctx)
	for _, finding := range watch.stop() {
		t.Errorf("single-writer violation while the recovered run advanced (duplicate call overlapped the original):\n%s", finding)
	}
	traj := snapshotTrajectory(cfg.Dir, state)
	t.Logf("interrupted(repair-in-flight) trajectory: %s (control: %s)", traj, controlTraj)
	compareTrajectories(t, controlTraj, traj)
	assertRunInvariants(t, cfg.Dir, state)
}

// TestUninterruptedCheckRunHoldsInvariants: 对照组——慢检查（2s）绿链跑
// 到底，与“收据落盘后”边界的中断版共用同一配置。
func TestUninterruptedCheckRunHoldsInvariants(t *testing.T) {
	checkArgv := []string{"/bin/sh", "-c", `sleep 2; test "$(cat state.txt)" = ok`}
	cfg := invariantScenarioConfig(t, "inv-check-control", nil, checkArgv)
	invariantRunControl(t, cfg, 1) // 一次开发，无返修（检查通过）
}

// TestInterruptedAfterCheckReceiptRecoversToSameTerminal: 检查收据落盘后杀
// owner，同配置重开续跑到终态：已完成的检查/门审按其领域结果接回，
// 不当普通 worker 处理。
//
// 边界同步点：jobs/check-state/*/turn-*/process.json（检查监督者身份落
// 盘）→ 杀 owner → 等 exit.json（收据落盘）。此刻重开，接回应凭真实回执
// 接纳已完成检查；门审可能同刻在途或已结算，两种状态都必须被正确处置。
func TestInterruptedAfterCheckReceiptRecoversToSameTerminal(t *testing.T) {
	checkArgv := []string{"/bin/sh", "-c", `sleep 2; test "$(cat state.txt)" = ok`}
	controlCfg := invariantScenarioConfig(t, "inv-check-a-control", nil, checkArgv)
	controlTraj, _ := invariantRunControl(t, controlCfg, 1)

	cfg := invariantScenarioConfig(t, "inv-check-b", nil, checkArgv)
	owner := startInvariantOwner(t, invariantOwnerSpec{
		Dir: cfg.Dir, RunID: cfg.RunID, Binary: cfg.Binary,
		Project: cfg.Spec.Project, Base: cfg.Spec.Base,
		Mode: "check-receipt", RepairLimit: cfg.Spec.RepairLimit,
		CheckArgv: checkArgv,
	})
	proc := waitForPattern(t, cfg.Dir, "jobs/check-state/*/turn-*/process.json")
	job := filepath.Dir(proc)
	owner.Process.Kill()
	owner.Wait()
	defer signalTurn(t, job)
	waitForPattern(t, job, "exit.json")

	watch := startWriterWatch(cfg.Dir)
	c := invariantOpen(t, cfg)
	ctx := context.Background()
	state := driveToTerminal(t, c, ctx)
	for _, finding := range watch.stop() {
		t.Errorf("single-writer violation while the recovered run advanced:\n%s", finding)
	}
	traj := snapshotTrajectory(cfg.Dir, state)
	t.Logf("interrupted(check-receipt) trajectory: %s (control: %s)", traj, controlTraj)
	compareTrajectories(t, controlTraj, traj)
	assertRunInvariants(t, cfg.Dir, state)
}
