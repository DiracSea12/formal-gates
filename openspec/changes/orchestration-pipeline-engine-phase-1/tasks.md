# Tasks

## 前置（prerequisite，开发开始前）

- [x] 记录本阶段基线 identity（db1822b）与 stable driver 重冻结证据（main HEAD 7929891 构建、package validate/canary PASS、旧树备份位置）。
- [x] 批次计划与档位留痕（批次 0–4 + 收口段；档位 = 单次全量门禁，结构性理由：生产逻辑集中单链、测试占比高、独立验收单元 1）。

## 批次 0（两支并行）

- [x] **0A｜测试隔离修复**：触及用户级 registry/安装路径的测试改用临时 HOME/registry root；复现 launcher 桩污染的回归用例（隔离环境下证明不再发生）。（根因：3 个测试漏 HOME 隔离——TestInstallReproducesV2ModelAtTarget（事故主因，桩经 Install 覆盖真实 launcher）、cli/install_test.go 两个 uninstall 测试回写真实 registry；修复为 t.Setenv 隔离 + install_isolation_regression_test.go 两个回归用例；批边界全量测试绿且真实安装 sha256 前后一致）
- [x] **0B｜Compiler spike（探路首批）**：六种代表性 step（engine local、durable side effect、host action、agent task、human ask、parallel/join）的小编译器；产出 IR 字段集、registry 绑定、encoder 字节稳定性、止损指标四项结论留痕（结论入 `refactor-plan/spike-notes-compiler.md`，spike 代码不 commit）。（spike 1817+1035 行在 /tmp/fg-spike 全绿；关键结论：ordinal 必须由 compiler 派生（Kahn+字典序）防 assembly 顺序泄漏；"inputs source bindings == dependencies"不变量使删依赖必拒；新增业务节点 0 改核心）

## 批次 1（串行主干，spike 冻结的 IR 为切分缝）

- [ ] **1a｜Authoring**：六种封闭变体 + constructor + 显式表；authority/runner 派生物化。配套：constructor 非法状态测试。
- [ ] **1b｜Closed-world compiler**：registry 解析、图不变量（可达性/循环/依赖/join 覆盖/版本绑定）、归一化。配套：八类拒绝的 enforcement matrix 测试。
- [ ] **1c｜Canonical encoder + 制品/常量同源生成**（bump workflowDefinitionVersion）。配套：freshness/assembly-order/round-trip 测试。

## 批次 2（2a 可与批次 1 并行先行；2b 依赖 1c）

- [ ] **2a｜RunPhase/TaskKey/TaskTransitionTable**：纯数据结构（含 batch 作为 TaskKey 分组与依赖信息、完成状态派生）。
- [ ] **2b｜决策核心**：Observe/Decide/SelectIssued + NextResult 六类 Kind 校验；canonical Plan 字节稳定。

## 批次 3（依赖 1+2）

- [ ] **验收套件补全**：digest 分离、digest 语义敏感性、registry 完备性、mutation tests、跨进程确定性。
- [ ] **golden/property tests**：合法边、非法事件、step 乱序/遗漏/重复拒绝、非终态无空结果。
- [ ] `MISSING_ENGINE_ADAPTER` diagnostic-only 与 `BLOCKED_BUG` 路由测试；最终候选 marker 扫描。

## 批次 4（依赖 2b）

- [ ] **Shadow harness**：只读 legacy 状态 → 预测 frontier → 差异报告；telemetry 落独立目录，不写权威 state。

## 收口（closure，开发完成后；独立于开发批次）

- [ ] 隔离安装构建（从已提交快照）→ 独立测试项目由 installed binary 执行 legacy 回归 + shadow harness → namespace disjoint proof。
- [ ] 开发后 QA、选定 gates、必要修复和 Seal；证据绑定候选 identity 与本阶段 VCS identity。（产品审/技术审为开发前流程步骤，已完成，不属批次。）

---

批次纪律（ADR-002）：本 run 声明的检查与回归——批内每次编辑后 `go build ./... && go vet ./...`；每批边界 `go test ./...` 全绿；单批一个关注点、批内 commit 原子。
