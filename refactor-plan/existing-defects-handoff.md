# 现有实现缺陷交接单

> 来源：formal-gates「编排流水线化」重构方案审查（第 8 轮）时，审查者在审视现状过程中顺带发现的 4 个现有实现缺陷。
> 性质：**现有 bug**，与「编排流水线化」重构本身无关，需单独修复，不进重构方案的任何阶段。
> 核实：已逐条对照源码核实，代码位置见各条「证据」。

---

## 1. 白盒 QA 测试代码不进最终快照（交付物静默丢失）

- **现象**：白盒 QA 设计者「开发后写结构测试代码」，但这份测试代码实际以**未跟踪文件**身份留在工作树，既不被 gate 审查、也不进 Seal 后的交付快照。「流程能正常推进」的真相是测试代码被当幽灵文件忽略了，不是被正确交付了。
- **根因**：
  - 白盒 QA 记录走 `requireNativeCurrent`（主工作区 HEAD == CurrentSnapshot），见 [workflow.go:1734-1739](internal/validate/workflow.go#L1734-L1739)。
  - 测试代码 `git add`+`commit` → HEAD 变 → 记录被拒；不提交 → 快照脏检查 `git status --untracked-files=no` 忽略它 → 记录通过、但代码不在 HEAD 里。
  - [qa-design.md:12-19](prompts/actions/qa-design.md#L12-L19) 规定白盒「开发后在主工作区直接写测试代码」，与此冲突。
- **影响**：白盒这个核心交付物（结构测试代码）在最终交付里不存在，且没被 gate 审查。
- **证据**：
  - [qa-design.md:12-19](prompts/actions/qa-design.md#L12-L19)（白盒开发后主工作区写测试）
  - [workflow.go:1734-1739](internal/validate/workflow.go#L1734-L1739)（白盒记录走 requireNativeCurrent）
  - [workflow.go:1637-1646](internal/validate/workflow.go#L1637-L1646)（requireNativeCurrent：HEAD != CurrentSnapshot 即拒）
  - [vcs.go:126](internal/validate/vcs.go#L126)（脏检查 `--untracked-files=no`）
  - [workflow_test.go:2827-2829](internal/validate/workflow_test.go#L2827-L2829)（fixture 把测试写进 baseline，掩盖了真实写入路径，测试通过是假象）
- **建议修法**：改白盒拓扑——候选实现 → 白盒在隔离 worktree 编写并审查测试 → 合入候选实现 → 冻结最终验证快照 → 所有 QA/gate 只读执行。

---

## 2. split 决定被提前到 start，脱离拆分依据

- **现象**：`workflow start` 强制 `--split yes/no`（`yes` 还需 `--retained-overall` 或 `--master`），但「该不该拆、拆几片」的真正依据要到 Product Review / Start Readiness 之后才产生。
- **根因**：[workflow.go:100-126](internal/validate/workflow.go#L100-L126) 的「需求 4」有意在启动时钉死拆分意向（为了启动即暴露「忘带 retained-overall」），副作用是把拆分决定提前到没有依据的时点。
- **影响**：拆分决定与拆分依据错位——用户被要求在还不知道该不该拆时就拍板拆不拆。
- **证据**：[workflow.go:100-126](internal/validate/workflow.go#L100-L126)。
- **建议修法**：改成 `split=UNDECIDED`，Product Review + Start Readiness 之后再物化「单 run」或「master/slices」。

---

## 3. legacy "qa" 合并态绕过黑盒隔离与白盒时序

- **现象**：legacy "qa"（旧目录把 QA 当门登记时的保留名）的合并态仍通过 `isSelected(legacyQAID)` 兼容存在，把黑白盒混成一次派发，绕过黑盒的隔离 worktree 与白盒的开发后时序。
- **根因**：
  - [catalog.go:33](internal/validate/catalog.go#L33)（`legacyQAID = "qa"` 保留名，CLI 当作内置 QA 识别）
  - [workflow_qa.go:118](internal/validate/workflow_qa.go#L118)（`blackboxReviewPassed` 里 `isSelected(state, legacyQAID)`）
  - [runstate.go:55-64](internal/validate/runstate.go#L55-L64)（`mode == ""` 键对应合并/单派发存储布局）
- **影响**：旧目录绑定的 run 仍走合并态；若新 run 也能进入，黑盒隔离、白盒时序被绕过。新 run 的正常路线（`selectedQAModes`）已只返回 blackbox/whitebox，问题集中在 legacy "qa" 兼容路径。
- **证据**：[catalog.go:33](internal/validate/catalog.go#L33)、[workflow_qa.go:118](internal/validate/workflow_qa.go#L118)、[runstate.go:55-64](internal/validate/runstate.go#L55-L64)。
- **建议修法**：新 run 禁止进入这条路径；legacy "qa" 仅为旧状态迁移保留兼容读取。

---

## 4. 快照检查忽略未跟踪文件，漏 git add 的新交付文件静默丢失

- **现象**：快照身份取 HEAD、脏检查 `--untracked-files=no`，导致「新增交付文件漏了 git add」时，文件既不算脏、也不进快照，静默丢失、无任何告警。
- **根因**：[vcs.go:126](internal/validate/vcs.go#L126)（脏检查 `--untracked-files=no`）+ [vcs.go:152](internal/validate/vcs.go#L152)（快照身份 = `git rev-parse HEAD`）。
- **影响**：常见漏 `git add` 会让新增交付文件没有进入最终快照。
- **证据**：[vcs.go:126](internal/validate/vcs.go#L126)、[vcs.go:152](internal/validate/vcs.go#L152)。
- **建议修法**：冻结语义前修掉——检测未跟踪文件，强制 add 或明确报错，不让「漏 add」静默通过。
