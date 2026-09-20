# G3a 领域路线（轻装模式记录，2026-09-20）

方案 §13.1 G3a：把仍在 Python 的领域判断逐节点迁到 Go；图边与流程确认仍归 LangGraph。
本次按机主既定轻装口径执行：工程本体（差分＋全量回归＋独立复核＋单提交），不跑方案自造的
acceptance/admission 仪式。实施由子代理完成，主代理定合同、独立复核并亲跑全部验证。

## 迁移的五项判断（graph.py @ c60b635 → internal/harness/domain.go）

| 桥接操作 | 原位置 | 内容 |
| --- | --- | --- |
| `evaluate-route` | `Flow.evaluate`（graph.py:212-224） | 返修路线：非 PASS 检查＋NeedsRepair 门 → repair/seal；返回 id/门名投影（保持 spec 顺序），Python 按投影组装 feedback 记录（内容与键序不变） |
| `worker-outcome` | `Flow.invoke_worker`（graph.py:87-102） | CONTINUE→BLOCKED→（development/repair 且 needs_repair）→accept 的分支选择；循环机制（continuation/ask/attempt）留 Python |
| `check-retention` | `Flow.one_check`（graph.py:154-170） | 增量保留资格，两阶段单操作：无 `evidence_valid` 判资格（binding 相等且 status∈PASS/FAIL 才核证据），带 `evidence_valid` 判 retain/fail/run（含 inherited_from 置值）。`verify_evidence` 本身留 Python，首试证据无效仍重抛原异常 |
| `repair-allowance` | `Flow.repair`（graph.py:226-229） | 轮次边界：allowance=null→spec 缺省；round>=allowance 才问询；用户 continue 后 +1 留流程侧 |
| `candidate-admission` | `Flow.candidate`（graph.py:142-144） | 候选准入拒绝（development 先查，两条消息逐字）；C4 值内类型化拒绝经 `refuse()` 映射回 HarnessError |

`evaluate-route` 的门阻断条件直接复用 `NeedsRepair`，未写第二份。`candidate` 节点的
repair-no-change 观测事件判定留在 Python（事件非关口，本单元不移）。

## 等价证据

- `runtime/langgraph/tests/reference_domain.py`：五项判断按 HEAD c60b635 逐字复制的迁移前逻辑（参照实现，仅测试用）。
- `test_domain_judgments_differential.py`（13 用例）：逐项经真实二进制 `driver_bridge().call` 整返回值 canonical 对比参照；语料可判别——顺序钉死（投影名故意逆字典序）、CONTINUE/BLOCKED 优先于 needs_repair、attempt 0/1 边界、allowance 等值边界与显式覆盖、双真时 development 措辞优先。
- `internal/harness/domain_test.go`（18 测试）覆盖同一批边界。
- 全图正常/异常路径由既有 test_driver.py（夹具全图）与 test_nonhappy_regressions.py 覆盖，节点换芯后原样通过。

## 主代理亲验（2026-09-20，最终树上）

- `gofmt -l internal/harness/` 干净；`go vet ./...` 零输出。
- `go build -o .harness/bin/formal-gates ./cmd/formal-gates` OK。
- `go test ./...` 全 ok（internal/harness 84.187s、internal/validate 372.757s，均非缓存）。
- Python 全套 `PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=$PWD/runtime/langgraph TMPDIR=/private/tmp .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests`：**301 项 OK**（基线 288＋新增 13）。
- 差分子集 `-p "test_*differential*.py"`：**154 项 OK**。
- 负控：`worker-outcome` 载荷带未知字段 → `INVALID_HARNESS_OPERATION`、退出码 2（StrictDecode 生效）。
- 基线（c60b635 干净树）双套件亦全绿；模型调用 **0** 次。

## 有界差异（子代理披露、主代理复核认可）

1. `candidate` 现在先读 development 与 qa_review 两条记录再判（原实现在 development 拒绝时不读 qa_review）。`Store.get` 为纯读（两次桥验证＋文件读，不发事件），无可观测差异；仅当 development 拒绝且 qa_review 键缺失时异常类型由 HarnessError 变 KeyError——图上 candidate 恒在双节点汇合后运行，不可达。
2. `repair-allowance` 对"键存在且值为 None"按缺省处理（原实现该状态下 `round >= None` 直接 TypeError）；round_allowance 只由 repair 节点写 int，不可达。
3. `check-retention` 阶段2若 `old=null` 且 `evidence_valid=true` 会返回 retain＋空 inherited_from——阶段2仅在阶段1=verify 后调用，不可达；按"不加 Python 从未做过的防御"未加哨兵。
4. `one_check` 以 `raise exc` 重抛捕获对象替代原裸 `raise`——同一异常对象与 traceback，两条证据丢失回归用例确认不变。

## 未做与后续

- 未跑 acceptance.py/admission.py 仪式（轻装裁定）；未做实机原生运行（本单元为纯计算，无宿主协议/进程/交互改动，按方案 §13.1"纯计算单元主要依靠固定输入与确定性全图回归"不需要）。
- 后续单元：G3b（终态资源管理：归档/独立核验/预览/回收）→ G4（Go 内核 K1–K5）→ G5（切换发布）。
