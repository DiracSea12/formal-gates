# G2b 验收记录 v2（2026-09-19 深夜，去仿真化重写；原生关口已按授权重取关闭）

候选 `3d769a0`（分支 `codex/go-engine-v1`，合同 v2）。上一验收基线：`G2a`（候选 `df33e9c`）。
九项验收全过（尝试 `.harness/validation/acceptance/G2b/20260919T123352Z-12066`，`overall=PASS`、
`failed=[]`；运行时身份 `cc213960…`、驱动 sha256 `29889205…`）。

**为什么重签**：首次收口（候选 `9d12645`，记录 `b22e5c3`）被外部 AI 评审驳回——三项验收缺口
（原生能力覆盖 not_verified、ADM-E15 负控制失效、回退保全证据未闭合）＋两条 P3；随后机主裁定
（原话见合同 v2 头部）：**不做机械 Python 仿真，用 Go 实现功能，简洁优雅可维护优先**。合同由
字节等同改为**值级功能等价**，本候选按 v2 重写后整链重验。

## v2 重写搬了什么（候选 `3d769a0`，净 −1105 行）

| 归属 | 内容 |
| --- | --- |
| **删除** | `pydump.go`＋`pydump_test.go` 整体（CPython 词法解析器、`JSONDecodeError` 行列号文本、默认分隔符渲染、`repr`/`KeyError` 文本、`OrdObj` 键序结构、big.Rat 真值/相等仿真） |
| **显式化（行为不变）** | 空绑定→`len(binding)==0`；换行归一→`normalizeNewlines`（CRLF/孤 CR）；`1==1.0` 仅在解码字面量与 Go int 真实相遇处经既有 `valuesEqual` 比较；`true==1` 删除；响应判定（error 成员、closure 键、method）显式规则 |
| **改规范形** | `harness native` stdout、prompt 内嵌 schema、错误文本内嵌 JSON 全部 canonical 紧凑形（值相等；消费者按值解析） |
| **流式恢复（评审 P3）** | `sessionEvents`→`forEachSessionEvent` 逐行消费（64KB bufio、单行生命周期），长回合不再整读整存 |
| **测试跟随** | 差分改值级（双侧 `json.loads` 比值、prompt 头/schema 解析比对）；Go 测试断言结构化结果或 Go 文本 |
| **ADM-E15（评审 P2，本单元内修）** | `admission-controls.py:308` 篡改值 `"dsh"`→`"control-contradiction"`（恒与记录原值矛盾） |
| **不动** | `canonical.go`、`admission.py`/`acceptance.py`、zcode 生产路径、`native_dsh.mjs`、`replace_file_windows.go`（独立登记项） |

## 主代理独立复核（不采信子代理自述，全部亲跑）

| 项 | 结果 |
| --- | --- |
| 改动范围 | 13 文件全在授权清单；排除文件零触碰；仿真符号全仓零残留 |
| gofmt / vet | 触及文件干净；darwin/windows/linux 三平台 vet 过 |
| `go test ./...` | exit 0（validate 342.7s 全新跑过） |
| Python 全套 | 261 tests OK（exit 0，含差分 34 项值级） |
| ADM-E15 决策逻辑 | 直调 `native_verification_problem` 亲验：篡改 `"dsh"`→空（复现缺陷）；`"control-contradiction"`→拒绝文本含 `"run report provider"` 标记 |
| 关键点亲读 | `forEachSessionEvent` 真流式；`len(binding)>0`、`CanonicalJSON` schema、`normalizeNewlines` 落地；错误族前缀与 DSH 协议文本（功能数据）保留未误删 |

## 有界差异登记（八条，全量见 `go-engine-G2b.json`）

prompt 内嵌 schema canonical 化；错误文本内嵌 JSON canonical 化；native-error 文本 Go 风格
（族前缀与结构化字段不变）；解析错误不再有 CPython 行列号；退化输入判定显式化；`true==1`
删除；stdout canonical（承前登记）；**旧运行时身份绑定全部失效→原生证据需重取**。

## 回退保全（评审缺口③，已闭合）

G2a 冻结候选 `df33e9c` 于隔离 worktree 重建（六项身份文件摘要逐一相符；重建二进制
`d7a23dc9…` 与签押件 `5bd9e243…` 之差＝go build 内嵌构建目录路径，源状态精确核验），公开入口
`scripts/harness demo --approve-sample` 全流程冒烟 **DEMO_COMPLETE**、exit 0、零模型调用
（含确认边界、一次返修回合、seal）。证据：`.harness/validation/g2b-rollback-g2a-20260919/`
（summary.json＋完整 demo run）；回退入口已落机器记录 `rollback_preservation`。

## 原生关口（已按机主授权重取，PASS）

- 前一次授权运行（`harness-20260919-161642-52992e09`，身份 `a1edad6a…`）随 v2 重写**按设计解绑**，
  其授权与结论归属 v1 候选，不再作为本候选证据。
- **机主 2026-09-19 逐字授权「授权」**（对应点名上述两项运行的请求，全文存
  `native_verification.authorized`）。已在 launchd 非继承 runner＋候选逐字节副本 `/tmp/g2b-v2-copy`
  执行（副本 HEAD `590039a` 仅记账领先候选；驱动 `29889205…` 与记录相等；六项身份文件相符 ⇒
  内容寻址身份 `cc213960…` 相等且被 run manifest 绑定）。
- **① 7 角色完整流程**：run `harness-20260919-210130-0164b260`，`HARNESS_COMPLETE`、**7/7 PASS**、
  0 中断、exit 0；7 个原生作业 argv 全为 Go 窄命令、exit 0、pgid 齐备（完成关联亲验）。
  证据 `/private/tmp/g2b-v2-copy/.harness/validation/g2b-native-v2-flow-20260919T210124`。
- **② `--tools` 能力探针**（评审缺口①三项全闭，效果＋工具事件双证）：
  - `read_only_tools` **VERIFIED**——只读阶段真实 bash 尝试 `printf DENIAL_TEST >> guard.txt`（两次，
    第二次抓 stderr），得 `Operation not permitted`/exit=1，回读确认；guard.txt 与 child-guard.txt
    逐字节未变。
  - `subagents` **VERIFIED**——development 用原生 subagent 工具委派 child-seed→child.txt（根会话被
    禁代劳），`delegated_file:true`；只读阶段另委派子代理在继承策略下尝试写 child-guard.txt，未变。
  - `resume` **VERIFIED（同会话成功续用）**——resume 阶段以 development 身份 `session/resume` 续写
    resume.txt 与种子逐字节同（`resumed_write:true`），补齐 native_missing_session 只证"失效重建"的
    缺口。
  - network 仅回环（2 次命中令牌）；mcp 本机未配置＝如实不适用不虚报。
  证据 `/private/tmp/g2b-v2-copy/.harness/validation/g2b-native-v2-tools-20260919T210124`。
- 模型调用支出＝7 角色流程＋3 个探针阶段（授权范围内全部支出）。

## 提交链

`9d12645`（v1 候选，被驳回形态）→ `b22e5c3`（v1 记录）→ `3d769a0`（v2 候选：去仿真重写＋
ADM-E15 修复＋合同 v2）→ 本次记录提交（G2b.{json,md}＋索引更正块）。

> 2026-09-20 机主指示清理 tmp：`/tmp/g2b-v2-copy`（回退冒烟用整仓冻结副本，1GB）与 `/tmp/g2b-runner.app` 已删除；本记录转录的结论不受影响，原始证据路径自此失效。
