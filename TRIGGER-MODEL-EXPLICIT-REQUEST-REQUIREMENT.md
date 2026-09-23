# 触发模型改动 + 移除轻量流程需求

> 本文件登记一次正式流程的已确认需求：把 formal-gates 的受理触发模型改为「主代理不
> 自动触发、仅判断需求过大过复杂时提醒用户可触发、用户明确要求才走正式流程」，并整体
> 移除轻量流程（含主代理高置信跳过受理的判定权、受理阶段轻量路线选项、会话级「不走流
> 程」声明）。整理日期：2026-08-11。

---

## 背景

当前规则要求主代理对每一次内容修改请求自动执行受理流程，并允许主代理高置信判断请求
应走「轻量」时跳过整个受理流程；受理阶段还提供「轻量 / 正式」二选一的路线选择，以及
会话级「不走流程」声明。用户希望：formal-gates 不应被主代理主动触发，也不应自动触发
——主代理收到内容修改请求时按常规方式直接处理；只有当主代理判断需求过大、过复杂时，
才提醒用户可以选择触发 formal-gates 正式流程；用户明确要求走正式流程时，仍走完整受
理流程。轻量流程因此不再需要，整体移除。

本改动只涉及**当前生效规则**（宿主全局指令、SKILL.md、README、references、agents 元
数据与联动测试），不涉及设计稿（TRANSITION-TABLE-PROPOSAL、HARD-PIPELINE、
WORKFLOW-TOPOLOGY、RESEARCH-*）、历史记录（CHANGELOG、openspec、P2 需求文档）与流程
状态（.gates）。

---

## 需求 1：触发模型改为「不自动触发、仅大需求提醒」

1. **不自动触发**：主代理收到创建、编辑、移动或删除项目内容的请求时，不自动启动受
   理流程，按常规方式直接处理。删除当前「所有内容修改请求都要走受理流程」的默认要求。
2. **仅大需求才提醒**：主代理判断当前请求过大、过复杂（规模、耦合、风险、验证复杂
   度足以超过普通直接处理的能力）时，提醒用户可以选择触发 formal-gates 正式流程；
   普通请求不提。提醒只是一次性提及、**不要求用户回应**——用户不回应即继续按常规方
   式直接处理，用户想回应/触发时自行提出即可。主代理不得自己触发、不得反复提及。
3. **用户明确要求才走正式流程**：用户明确要求走正式流程（或明确要求触发 formal-gates）
   时，仍走完整受理流程（澄清 → 呈现完整方案 → 单独确认 → 评估工作量）。受理阶段只
   决定是否进入正式流程；进入后路线 full/custom 在拆分决定之后确认（与现状一致）。这
   条路径保留。
4. **删除主代理高置信跳过受理的判定权**：不再有「主代理高置信判断应走轻量、或根本不
   涉及修改时，跳过整个受理流程」这条规则。
5. **删除会话级「不走流程」声明**：不再有【流程提示】、会话级「不走流程」声明及其
   「默认按正式流程执行」的配套表述。

## 需求 2：整体移除轻量流程

1. 受理阶段只决定是否进入正式流程（是 / 否），不再提供「轻量（不创建正式流程）」
   选项；路线 full / custom 在拆分决定之后确认。
2. 删除「`轻量` 就是普通的 vibe coding 流程」定义段。
3. 删除「受理阶段只决定轻量或正式」表述，改为「受理阶段只决定是否进入正式流程（是 /
   否），进入后路线 full/custom 在拆分决定之后确认」。
4. README / README_EN 的「分流」「轻量和 formal 是什么关系」「小改动可直接走轻量」等
   轻量相关内容同步改为新触发模型。
5. agents/openai.yaml 的 default_prompt 去掉 "lightweight" 路线；`internal/validate/
   package_test.go` 的 metadata 断言同步调整（不联动改会挂）。manifest（repo 与
   .claude/.codex/.cursor 三份副本）经核实不含 lightweight 路线，无此改动项；其描述
   性措辞顺带对齐、非必需。

## 需求 3：改动范围（当前生效规则）

- **A. 宿主全局指令**：`~/.claude/CLAUDE.md`（两行受理条款，第 1 行含高置信跳过句）、
  `~/.codex/AGENTS.md`（一行受理条款）。
- **B. 工作区 `SKILL.md`**（20642B 最新版）：description、受理段（高置信跳过 + 会话级
  声明）、step 4 轻量/正式选项、轻量定义段、「受理阶段只决定轻量或正式」。
- **C. 已安装 `SKILL.md` 三份**（`~/.claude/skills/formal-gates/SKILL.md` 15716B 旧版：
  lightweight/full/custom 一次路由、无会话声明；`~/.codex/skills/formal-gates/SKILL.md`
  与 `~/.cursor/formal-gates/SKILL.md` 17054B 相同：轻量/正式分流 + 会话声明、无高置信）
  ——各按自身版本内容改。
- **D. README 中英 8 份**（repo + `.claude` + `.codex` + `.cursor` 各 README.md /
  README_EN.md）：分流段、受理段、FAQ。
- **E. references**（repo + `.codex`/`.cursor`）：`example-run.md`（选择轻量/正式段、不
  走流程分支段）、`formal-flow.md` line 6（会话声明引用）、`managed-rules.json`（受理
  强制条款原文）。悬空引用清理点经核实：repo `formal-flow.md:6`（引用 example-run §1）
  与 `.codex`/`.cursor` 副本 `formal-flow.md:6`（引用 SKILL.md 会话声明）；`.claude` 副
  本 `formal-flow.md:6` 是目录锚点、全文无会话声明/example-run 引用，无此清理项。
- **F. 联动**（repo + 3 副本）：`agents/openai.yaml`、`formal-gates.manifest.json`、
  `internal/validate/package_test.go`。

明确不动：`canary.go` 的 "lightweight-workflow"（快速端到端正式流程 canary，非轻量路
线）、`requirements-clarification.md` 的「轻量澄清兜底」（受理阶段对明显缺口仍提问，非
轻量路线）、CLI 的 full/custom 路线、`hook decide`（只保护活动 run 内写入纪律，不强制
受理）、release 发货目录 `~/.formal-gates/releases/`。

## 验收

- 四位置（工作区 + `.claude` + `.codex` + `.cursor`）的 SKILL.md / README / openai.yaml
  / manifest / managed-rules 不再出现「轻量流程路线」「自动受理」「会话级不走流程声明」
  「高置信跳过受理」等表述（非路线语义的 lightweight-workflow canary 名与「轻量澄清兜
  底」除外）。
- `internal/validate/package_test.go` 通过（metadata 断言与新表述一致）。
- 用 10 个以上假任务假对话实测新触发模型：普通内容修改请求主代理不触发、直接处理；大
  需求主代理提醒用户可触发但不自己触发；用户明确要求时进入正式流程；轻量选项不再出现。
