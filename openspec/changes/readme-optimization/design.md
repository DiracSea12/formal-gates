# Design

目标：README.md / README_EN.md 重构为纯人类着陆页；首屏价值先行，真实运行转录
作为「示例产物」节证据。

## 目标章节结构（两版同步）

1. 标题 + 标语 + 徽章
2. 首屏（价值句 + 紧凑流程示意 + 一句话总结；不以大段 CLI 转录开场）
3. 为什么需要它（含「独立 session 消除盲区」机制一句）
4. 特性（每条一句「体现在哪」）
5. 它是怎么工作的（7 个设计概念，各人类语言 1-2 句，无操作规则）
6. 示例产物（自定义门 md 示例 + 封板摘要的人类语言说明，不展示 JSON 字段）
7. 安装 / 安装位置 / 卸载
8. 使用方式（人类路径：安装 → AI 代理驱动 → 审阅结果与摘要）
9. 常见问题
10. 范围 / 状态声明
11. 本地校验 / 贡献 / 许可

## 移出 README 的内容（AI 文档已持有，不重复创建）

- 20 个 workflow 子命令的 CLI 走查 → `references/formal-flow.md` 已覆盖
- P0/P1/P2 操作语义、result 校验规则、carry/任务切片/QA 生命周期操作规则 →
  SKILL.md 已覆盖
- VCS 命令 → `references/vcs-snapshots.md` 已覆盖
- 本地校验细节 → `references/local-validation.md` 已覆盖
- 安装/hook 细节 → `references/install-and-hooks.md` 已覆盖

## 事实修复（对应 RQ-005）

- 删除 `--package-root ~/.config/formal-gates`（正确位置：全局 claude 安装为
  `~/.claude/skills/formal-gates`；package-root 默认 `.`）
- 人类不再手输 workflow CLI 走查，该错误路径整体消失

## demo 计划（RQ-010）

在 `/tmp` 建 scratch git 仓库（含初始提交），以本仓库作为 `--package-root`，
真实跑通一轮正式流程的公开入口序列，验证「示例产物」节对封板摘要的人类语言描述
（哪些门通过、finding 严重级与位置、QA 结果）与真实执行一致。示例只展示真实发
生过的输出，不编造；不保留长命令转录、不展示 JSON 字段。该运行同时作为 QA LIVE
用例证据。

## 验证计划

- `go test ./...`、`go test -race ./internal/validate ./internal/cli`、
  `go vet ./...`、`go build`、`package validate --root .`、
  `canary portable --root . --format json` 全部通过
- QA STATIC 用例：锚点解析、禁止字符串（`~/.config/formal-gates`）、命令与
  `bin/formal-gates <cmd> -h` 一致、双语章节一一对应
- QA LIVE 用例：scratch 仓库端到端跑通（即 demo）
