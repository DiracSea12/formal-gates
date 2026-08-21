# Tasks

## 基线与环境

- [ ] 记录本阶段基线 identity（db1822b）与 stable driver 重冻结证据（main HEAD 7929891 构建、package validate/canary PASS、旧树备份位置）。
- [ ] 测试隔离修复：触及用户级 registry/安装路径的测试全部改用临时 HOME/registry root；复现本次 launcher 桩污染的回归用例在隔离环境通过。

## Spike

- [ ] 六种代表性 step 的 compiler spike：IR 字段集、registry 绑定、encoder 字节稳定、止损指标验证；结论留痕，代码不进 production。

## 内核实现

- [ ] 封闭变体 authoring：六种 Step 类型 + constructor + 显式节点/步骤表；authority/runner 派生物化。
- [ ] Closed-world compiler：registry 解析、图不变量、归一化、canonical 编码；八类拒绝按 enforcement matrix 分层拦截。
- [ ] 单一 canonical encoder 与同源生成：`definitions/workflow.json` + 身份常量，bump workflowDefinitionVersion。
- [ ] RunPhase、TaskKey、TaskTransitionTable、Observe/Decide/SelectIssued、NextResult 六类 Kind 校验。
- [ ] `MISSING_ENGINE_ADAPTER` diagnostic-only marker 与 `BLOCKED_BUG` 路由。

## 验收测试

- [ ] 十条独立验收：freshness CI、assembly-order、round-trip、跨进程确定性、digest 分离、digest 语义敏感性、registry 完备性、constructor 非法状态、mutation tests、复杂度止损。
- [ ] golden traces/property tests：合法边、非法事件、step 乱序/遗漏/重复拒绝、非终态无空结果、canonical Plan 字节稳定。
- [ ] Shadow harness：只读预测与差异报告，不写权威 state。

## 验证与退出

- [ ] 从阶段候选 installed binary 在独立测试项目执行 legacy 回归、shadow/diagnostic harness；namespace disjoint proof。
- [ ] 独立产品审、技术审、QA、选定 gates、必要修复和 Seal；证据绑定候选 identity 与本阶段 VCS identity。
