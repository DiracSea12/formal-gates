# 正式 run ledger 目录说明

- 本目录存放正式流程 run 的 ledger（`workflow seal` / `workflow abort` 产物）。
- **canary 输出不在本目录**：canary 文件见 `../canary/`（schema 与 run ledger 不同，混放易混淆）。
- **cost schema 两代并存**：旧代 run 用 `totalTokens`，新代 run 用 `totalInputTokens`（RQ 迁移后）。
  各 run 内部自洽，两代字段并存是历史演进遗留，不视为冲突。
