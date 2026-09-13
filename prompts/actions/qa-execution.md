# QA 执行

针对指定的当前 VCS 快照，独立执行每一个已批准的 QA 用例。按用例建立前置并执行被测步骤，
记录实际观察结果，并与写明的预期判据比较。不要编辑交付文件、不要在执行过程中发明
新用例、不要检视无关的实现细节，也不要用开发者自测证据替换一个失败的用例。

白盒用例通过其指定的直接归属者结构测试（单元、系统、集成）来运行。黑盒用例在引擎绑定的独立工作
区对当前候选，通过文档化的公开入口实际使用产品来验证需求——代码检视、模拟
输出和开发者自测主张都不算黑盒的实际使用执行。

测试准备遵循 `prompts/actions/qa-design.md` 的“测试准备可构造”规则，可在隔离环境中
按用例构造或手工设置前置，不要求公开生成入口或重跑前序流程；被测逻辑须实际运行，
验证真实集成时须取得真实观测。

某个用例失败后，继续执行其余每一个可安全执行的用例。在已批准的用例集中搜索处于同
一条用户可见行为链上的其他用例，并把所有相关失败一并报告。一个失败的前置条件只能
阻塞那些确实因此无法执行的用例。

每条结果只能记录本次实际执行该 case 的具体步骤、实际观察和逐项 oracle 比较。禁止
使用 `skipped`、`authorized PASS`、`executed the approved procedure`、`behavior matched`
一类跳过、授权或模板文案冒充逐用例证据；用户授权可以改变流程是否继续，不能把未执行
用例改写为执行 PASS。未执行、setup 失败、入口不存在或所需真实宿主能力缺失时，完整已完成行照实保存 PASS/FAIL，
未完成行保持未完成；记录中断原因与未完成集合，整体维持 PENDING/RUNTIME_ERROR，不能 PASS。
通过 formal_gates_operation 的 qa.checkpoint 保存新增完整结果：caseId 为空，prompt 为
包含 cases、remaining、message 及可选 findings、invalidated 的 JSON。该操作不结束当前 worker。
中断时按当前 schema 返回 CHECKPOINT 和真实原因；续跑只补 remaining，既有有效结果由引擎保留。
工具或环境变化先在 invalidated 逐项给出 caseId、previousTool、previousEnvironment、tool、environment、reason。
最终按当前结果 schema 交回剩余完整行；引擎合并有效进度并核对完整 expected-set。授权跳过不是 PASS。

同一不可变候选构建一次，复用不属于验收目标的共同准备；独立用例从干净副本开始。
有序场景显式声明依赖且共享同一副本；一条失败仅停止依赖操作，其余独立用例继续。
逐条保存真实命令、输出与断言结果。构建、安装、启动、真实宿主派发本身是验收目标时
必须在本轮真实执行，不能以复用准备替代。批量只在本次 QA 派发内部组织，不新增逐例
代理、TaskKey 或额度。可用既有 fixture 的 stage/copy/prepare --reuse-build 减少准备。

不要声称程序能判断自然语言 Operator 是否撒谎。以故意伪造 Operator 核验或证据作为被测场景属于项目
边界外的对抗性输入；遇到这类批准错误应如实报告用例准入问题或运行错误，不为它构造
语义测谎结果。

白盒执行已批准用例绑定的已有或新增结构测试，独立运行并记录本轮真实输出。
黑盒证据仍来自公开产品入口的实际使用；未检查真实断言的“已有测试全绿”不构成覆盖判断。

## 重跑与受影响子集

普通中间返修固定使用 AFFECTED，提示词会列出派发前由引擎确定的受影响子集：上一轮失败
用例，以及与本轮修复路径相关的既往通过用例。只执行该子集；其余合法 PASS 继承且不在本次
执行范围内。不得补跑、改判、判定名单外用例或临时改选子集。首次执行和明确授权的 FULL 回归
则执行全部已批准用例；不得把 AFFECTED 当成执行者可自行改为 FULL 的建议。

## 项目边界

项目边界同 `prompts/reviewer-base.md`：只验证文档化正常使用与常见操作失误。除非
用户明确要求加固，对抗性输入、恶意编辑、手工改写状态制造不受支持的破坏、权限或不可变文件失败、不
受支持的平台，以及其他人为构造的攻击或违规，不为其新增用例或阻塞执行，也不引出
新机制、兼容路径或测试。

## 当前引擎操作与结果

读取 requirement.path、caseFile.path；只执行 remaining 中的批准用例。通过
formal_gates_operation 按 qa.prepare → qa.target → qa.collect 执行；引擎选择工作目录，
绑定候选和用例，记录实际 stdout、stderr、退出码。所有 QA 操作 prompt 为空。
case.command 为 null 时，由执行者按批准步骤在 qa.target 的 command 提供
{executable,args,expectedExitCode}，包含必要前置准备和实际目标调用；简单操作直接调用，
仅重复或多步取证需要短脚本，不改变已批准步骤或判据。executable 是程序名或路径，参数逐项放 args。
已有 case.command 时省略操作的 command，不替换批准的命令。

qa.run 可以自动消费已有命令和准备/采集操作。返回 pending.input 的 case 需要执行者按上述
方式提交 qa.target；pending.judgment 需要逐例比较 oracle 并 checkpoint。输入待补或等待不等于 FAIL。
引擎仍拥有动作派发、依赖、剩余集合、并发、恢复和结果接纳；执行者只完成当前已批准用例。

每条结果按 schema 返回 caseId、status PASS/FAIL/BLOCKED、procedure、output、comparison、
environment、tool、workspace、dependencies、reusedPreparation。逐条记录真实命令、实际观察与判据比较。
FAIL 附 raw finding，requirement 使用对应 caseId 并给出 remedy。最终 remaining 为空；
无法执行的用例保持未完成并说明真实阻塞，不以空结果或模板文案冒充 PASS。
