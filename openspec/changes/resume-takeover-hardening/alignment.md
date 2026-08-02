# Requirements Alignment

Date: 2026-08-02
Status: confirmed

## RQ-001 - 接手基线可指定祖先

`workflow start --base-snapshot` SHALL 接受任何可验证且为当前 HEAD 祖先（或相等）的提
交；run 的基线 SHALL 为该提交，当前快照 SHALL 为当前 HEAD。已提交的在途工作因此落入
"基线到当前"的审查 diff。

## RQ-002 - 接手 = 新 run + 显式基线 + 重走需求澄清

接手中断的 run SHALL 以新建 run 进行（resume 降级为备用路径）。新 run SHALL 先跑目标
项目自己的构建/测试便宜健全性检查，再重走需求澄清并持久化对齐结果；审查 SHALL 以整个
功能为单位看"需求有没有做好"，无关改动由需求匹配框架过滤。

## RQ-003 - 提示词/文档改动优先修改、保持精简

提示词与文档文件的改动 SHALL 优先修改既有文件、不新增；新增文本 SHALL 精简克制。

## RQ-004 - run 记录逐门/逐 action 的 prompt 哈希

run 状态 SHALL 在启动时记录每个已发现门与每个 action 的 prompt 内容哈希，使 catalog
delta 可逐门计算。旧状态文件无该字段 SHALL 兼容加载。

## RQ-005 - 中途任意修改由主代理判定继承

run 中途任何项目文件发生修改（提示词目录、源码、需求产物、任意 VCS 提交）SHALL NOT 自
动使结果全量失效。主代理 SHALL 检视实际改动范围，逐结果判定：能证明不受影响 → 继承并
记录理由；受影响或不确定 → 该结果重跑或派独立判定。仅改动未选门/未选 action 的 catalog
变更 SHALL NOT 阻塞 run；已选门 prompt 内容变化按本规则判定。

## RQ-006 - 采纳外部 VCS 改动

主代理 SHALL 能以记录理由显式重绑当前快照来采纳外部 VCS 改动，并继承其能证明不受影响
的结果。

## RQ-007 - 开发后 meaning-preserved 重绑定

开发开始后 SHALL 允许语义未变（meaning-preserved）的需求重绑定，前提是主代理认定语义
未变且用户确认；不受影响的 PASS 结果 SHALL 保留。

## RQ-008 - 统一继承判定入口

carry 的 INHERIT/RERUN + origin/reason 原语 SHALL 成为任何重绑定时刻（修复、采纳、中
断恢复）的统一继承判定入口。主代理 SHALL 只继承其能证明不受影响的结果并记录理由；不
确定的 SHALL 派独立判定或重跑。

## RQ-009 - 返修重跑门用完整 base→current

返修后重跑的门 SHALL 审查完整的"基线到当前"交付；组装提示词 SHALL 显式声明此范围，
SHALL NOT 将审查限定于返修增量。

## RQ-010 - 门审必须报告比较的快照对

门审查结果 SHALL 返回审查者实际比较的快照对；报告与指定范围不匹配的结果 SHALL 被主代
理丢弃。

## RQ-011 - carry 范围保持 pre-repair→current

继承判定 SHALL 继续比较"修复前紧邻快照→当前"，与门重跑范围（base→current）在提示词
中明确区分。

## RQ-012 - 边界

本变更 SHALL NOT 引入对抗性加固、权限/不可变文件故障注入、不受支持平台的兼容路径或框
架层，除非已确认需求明确要求。

## RQ-013 - SKILL 层需求澄清修正（Phase 1）

SKILL.md 正式流程第 2 步措辞 SHALL 明确：该步只负责登记已对齐需求并记录 PASS；澄清问答、
呈现整合需求、取得用户最终确认、把对齐后完整需求持久化到需求文件，全部在受理阶段完成。

## RQ-014 - 需求澄清提示词文件修正（Phase 2）

`prompts/actions/requirements-clarification.md` 的复盘修正 SHALL 作为 Phase 2，在
Phase 1（含 B2 目录 delta 容忍）落地后的后续 run 交付；本变更 SHALL 记录该后续交付的
追踪。

## RQ-015 - 中断子代理续跑优先（Phase 3）

被中断且未产出结果的子代理派发，宿主支持续用（如 Claude Code SendMessage）时 SHALL
优先恢复原代理继续，不首先重开新代理；仅当续用不可用时 SHALL 才将原派发标 STALE 并重
开新派发 + 新零上下文代理。作为后续 run（Phase 3）落地并追踪。
