package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// parallelStage 定义"阶段 → 应并行任务"的一条规则。规则集在 Go 内硬编码为一张
// 解耦可扩展的数据表，独立于流程逻辑：流程/规则经常修改（SKILL.md / formal-flow.md 仅为
// 参考）时，只改这张表即可；规则集不依赖是否已 prepare——只要当前处于该阶段、对应任务尚
// 未出结果，就应当并行派发。
type parallelStage struct {
	// name 是该阶段的显示名，用于提醒文案。
	name string
	// active 报告 run 当前是否处于该阶段。
	active func(RunState) bool
	// tasks 返回该阶段"应并行任务"集合（已过滤掉已出结果/无需再派发的任务）。
	tasks func(RunState) []parallelTask
}

// parallelTask 描述一个应当并行的派发任务及其在途判定（是否有已认领的子代理在跑）。
type parallelTask struct {
	// name 是任务显示名。
	name string
	// match 判定一个派发是否对应本任务（同 target kind + target + mode）。
	match func(PreparedDispatch) bool
}

// parallelStageTable 是"阶段 → 应并行任务"的硬编码数据表。按优先级从上到下取
// 第一个命中的阶段：
//   - 修复轮：修复快照推进后各门的重审与 QA 执行并行；
//   - 开发后审查：首个开发快照后黑盒 QA 执行 + 白盒 QA + 各已选门全部并行；
//   - 开发与黑盒 QA 并行：开发子代理与黑盒 QA 设计/审查在隔离工作区并发推进。
//
// 开发前两段式（Part 1 产品审 → Part 2 start-readiness）是顺序依赖，不并行，故无表项。
var parallelStageTable = []parallelStage{
	{
		name: "修复轮",
		active: func(state RunState) bool {
			return state.PreRepairSnapshot != ""
		},
		tasks: pendingReviewParallelTasks,
	},
	{
		name: "开发后审查",
		active: func(state RunState) bool {
			return hasDevelopmentSnapshot(state)
		},
		tasks: pendingReviewParallelTasks,
	},
	{
		name: "开发与黑盒 QA 并行",
		active: func(state RunState) bool {
			return developmentStarted(state) && !hasDevelopmentSnapshot(state)
		},
		tasks: func(state RunState) []parallelTask {
			var tasks []parallelTask
			if state.Actions["development-worker"].Status == developmentPrepared {
				tasks = append(tasks, parallelTask{name: "开发子代理", match: matchDispatch("action", "development-worker", "")})
			}
			// 并行提示按 blackbox mode 读黑盒 review 权威结果。
			if isSelected(state, blackboxQAID) && state.qaReview("blackbox").Status != "PASS" {
				tasks = append(tasks, parallelTask{name: "黑盒 QA 设计/审查", match: func(d PreparedDispatch) bool {
					return d.TargetKind == "action" && d.Mode == "blackbox" && (d.Target == "qa-design" || d.Target == "qa-review")
				}})
			}
			return tasks
		},
	},
}

// pendingReviewParallelTasks 返回快照后 / 修复轮应当并行的任务：每个仍需结果的已选门 +
// 每个仍待执行的已选 QA 派发 mode（黑盒/白盒各自独立派发、并行执行）。合并 QA（merge-qa /
// 旧 "qa"）是单派发合并集，不拆并行。
func pendingReviewParallelTasks(state RunState) []parallelTask {
	var tasks []parallelTask
	for id := range selectedSet(state) {
		switch {
		case isQAMode(id):
			mode := qaDispatchMode(id)
			if mode == "" {
				continue // merge-qa / legacy "qa"：合并单派发，无并行拆分
			}
			if !qaModeHasRecorded(state, mode) {
				tasks = append(tasks, parallelTask{name: "QA 执行（" + mode + "）", match: matchDispatch("action", "qa-execution", mode)})
			}
		default:
			if state.Gates[id].Status != "PASS" {
				tasks = append(tasks, parallelTask{name: "门审查（" + id + "）", match: matchDispatch("gate", id, "")})
			}
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].name < tasks[j].name })
	return tasks
}

// matchDispatch 构造一个"派发对应某任务"的判定（同 target kind + target + mode；空 mode
// 只匹配合并/单派发票）。
func matchDispatch(kind, target, mode string) func(PreparedDispatch) bool {
	return func(d PreparedDispatch) bool {
		return d.TargetKind == kind && d.Target == target && d.Mode == mode
	}
}

// inFlightParallelCount 统计应并行任务中当前在途（已认领/有子代理在跑）的数量。每个任务
// 至多计一次；COMPLETED / STALE / OPEN（未认领、无子代理）不计入在途。
func inFlightParallelCount(state RunState, tasks []parallelTask) int {
	count := 0
	for _, task := range tasks {
		for _, dispatch := range state.Dispatches {
			if dispatch.Status == "CLAIMED" && task.match(dispatch) {
				count++
				break
			}
		}
	}
	return count
}

// ParallelAdvice 是一次并行性检查的结果。
type ParallelAdvice struct {
	// Stage 是命中的阶段名。
	Stage string `json:"stage"`
	// ShouldTasks 是当前阶段"应并行任务"列表（仍需结果）。
	ShouldTasks []string `json:"shouldTasks"`
	// InFlight 是当前在途并行数（已认领/在途派发数）。
	InFlight int `json:"inFlight"`
	// Remind 为 true 表示当前并行不足、需要提醒主代理。
	Remind bool `json:"remind"`
	// Message 是要在 stderr 呈现的提醒文案（不污染 stdout 的机器 JSON）。
	Message string `json:"message,omitempty"`
}

// ParallelAdviceFor 按流程阶段数据表计算并行性建议。纯内存计算、只读，不依赖
// 是否已 prepare。规则集不命中任何阶段、或应并行任务集为空时返回空建议（不提醒）。
func ParallelAdviceFor(state RunState) ParallelAdvice {
	for _, stage := range parallelStageTable {
		if !stage.active(state) {
			continue
		}
		tasks := stage.tasks(state)
		if len(tasks) == 0 {
			return ParallelAdvice{}
		}
		names := make([]string, 0, len(tasks))
		for _, task := range tasks {
			names = append(names, task.name)
		}
		inFlight := inFlightParallelCount(state, tasks)
		advice := ParallelAdvice{Stage: stage.name, ShouldTasks: names, InFlight: inFlight}
		if inFlight < len(tasks) {
			advice.Remind = true
			advice.Message = fmt.Sprintf("可并行 %d 项（%s），当前并行 %d 项，建议补足。", len(tasks), strings.Join(names, "、"), inFlight)
		}
		return advice
	}
	return ParallelAdvice{}
}

// parallelCooldown 是同签名提醒的最小间隔（冷却/去重，避免连发刷屏）。测试可覆盖。
var parallelCooldown = 60 * time.Second

// parallelMarker 是 run 作用域的提醒冷却标记，记录上一次提醒的签名与时间。
type parallelMarker struct {
	Signature string `json:"signature"`
	Unix      int64  `json:"unix"`
}

// ParallelCheck 读取 run 状态、计算并行建议并应用冷却/去重，返回是否应在 stderr
// 提醒。实现保持便宜（只读一份小状态文件 + 一个冷却标记文件）且生命周期安全（不写派发、
// 生命周期事件或审查结果；冷却标记是独立的无副作用文件）。读不到 run 状态（如非 workflow
// 触发面）时静默返回不提醒。
func ParallelCheck(root, runID string, now time.Time) (ParallelAdvice, bool) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return ParallelAdvice{}, false
	}
	return parallelCheckState(root, state, now)
}

func parallelCheckState(root string, state RunState, now time.Time) (ParallelAdvice, bool) {
	advice := ParallelAdviceFor(state)
	if !advice.Remind {
		return advice, false
	}
	// 冷却/去重：同阶段 + 同任务列表 + 同并行数的同一提醒在冷却窗口内不重复连发；状态前
	// 进（签名变化）后立即重新提醒，避免漏掉新的补足机会。
	markerPath := filepath.Join(RunDir(root, state.RunID), "parallel.json")
	signature := advice.Stage + "|" + strings.Join(advice.ShouldTasks, ",") + "|" + fmt.Sprintf("%d", advice.InFlight)
	last, err := readParallelMarker(markerPath)
	if err == nil && last.Signature == signature && now.Sub(time.Unix(last.Unix, 0)) < parallelCooldown {
		return advice, false
	}
	_ = writeParallelMarker(markerPath, parallelMarker{Signature: signature, Unix: now.Unix()})
	return advice, true
}

func readParallelMarker(path string) (parallelMarker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return parallelMarker{}, err
	}
	var marker parallelMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return parallelMarker{}, err
	}
	return marker, nil
}

func writeParallelMarker(path string, marker parallelMarker) error {
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}
