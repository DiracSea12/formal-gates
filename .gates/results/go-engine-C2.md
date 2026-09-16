# C2 验收记录（重签）：证据内容摘要原语（G1a 第二项）

> **本记录取代同一路径下的上一版 C2 记录。** 上版绑定的候选 `2ed75d4` 存在一处由独立验收发现的 C1 回归（见下），修复后候选前移到 `87895e4`，因此上版结论不适用于新组合。上版记录内容保留在 git 历史中；其尝试目录未被覆盖。

## 结论

替换单元 C2 通过；**同时修正了 C1 的一项回归缺陷与一项漏检**。G1a **尚未完成**，详见"G1a 范围更正"。

| 检查 | 结果 |
| --- | --- |
| 本块合同（Go 单元测试） | 通过 |
| 文件样本差分对照（参考实现 vs Go） | 通过，15 类样本证明与拒绝原因一致 |
| 结果框架差分对照 | 通过，74 个输入（含本轮新增 11 个） |
| 累计组合完整流程（fixture） | 实际达到 `DEMO_COMPLETE` |
| 累计自动化回归（Go 无缓存全量） | 通过：26 包、1932 用例通过事件、0 失败、无停滞 |
| 累计自动化回归（Python 全套） | 通过，117 项 |
| SQLite 探针 | 通过，CGO 开/关各 8/8 |
| DSH / ZCode `doctor` | 通过，接入探测、`model_calls=0` |
| 原生定向验收 | **不适用**：纯计算，不涉及宿主协议、进程、权限或工具继承 |
| 真实原生行为复验 | **NOT_RUN**（授权关口在 G2b 之前） |
| **总体** | **`PASS`** |

## 独立验收指出的三项，逐项处置

### P1（真实回归）：说明中提及 NaN/Infinity 会让合法结果被拒绝——已修复

复现确认：`Checked edge cases: NaN and Infinity.` 后接一个干净对象，参考实现接受、Go 拒绝、两个宿主的结果落盘都抛 `RuntimeBlocked` 且不产出 `answer.json`。

根因是我在 C1 写的 `firstNonFinite()`：它在**选定 JSON 片段之前**扫描整条消息，只要前一个非空白字符是 `:`/`,`/`[` 就把 `NaN` 当成值的起点，于是普通说明里的冒号也会命中。

修复：非有限数字的判定移到**解析器实际所在的位置**——`classify()` 依据 walker 的 `decoder.InputOffset()` 检查**正在解析的那段文本**。fenced 与裸对象两条回退路径都从同一批类型化错误取拒绝，所以"对象内确有非有限值"仍照旧拒绝。

对照用例按评审要求补齐了**两个方向**：

| 方向 | 用例 |
| --- | --- |
| 说明中提及、对象干净 → 必须接受 | 对象前提及 NaN/Infinity；冒号后列举 `NaN, Infinity`；对象后提及；围栏前提及 |
| 对象内/围栏内实际包含 → 必须拒绝 | 对象后散文 + 对象内 `NaN`；围栏内 `Infinity`；裸对象内 `-Infinity` |

### P2（真实漏检）：重复空字符串键未被拒绝——已修复

复现确认：`{"":1,"":2}` 参考实现拒绝（`duplicate JSON field `），Go 接受为 `{"":2}`。原因是我用空字符串同时表示"没有发现重复"和"重复键本身为空"。

修复：把"是否发现重复"与"重复键的值"分成两个变量。顺带发现并修掉同一处的第二个隐患——非字符串键原先伪装成"空键重复"，现在有独立错误。

### 第三项：我先前对 G1a 范围的理由站不住——已更正

上版记录与 G1a 索引称：把 JSON 规范化搬到 Go 会改变 Python 侧每一个摘要，因此必须另作版本化迁移。**这个理由过强，评审的质疑成立。**

评审指出：把 Python 的 `canonical` 字节交给已有的 Go `digest-bytes` 得到相同摘要。这本身只证明"哈希与语言无关"，不证明 Go 能**产出**同样的字节。但我据此做了真正的可行性实测——**实现一个复现 Python `json.dumps(ensure_ascii=False, sort_keys=True, separators=(",",":"))` 的 Go 编码器，并与 Python 逐字节对照**：

| 样本 | 结果 |
| --- | --- |
| 448 个（含 200 个跨约 40 个数量级的随机浮点、大整数、次正规数 `5e-324`、`1e15/1e16/1e21` 边界、Unicode 非 BMP／组合字符／RTL、控制字符、`<`/`&`、U+2028/U+2029、空键、150 个随机嵌套结构） | **逐字节不一致 0 个** |

关键点是浮点：Python 用 `repr(float)` 的规则（`-4 <= 十进制指数 < 16` 用定点并保底一位小数，否则科学计数法且指数至少两位），与 Go `strconv.FormatFloat` 的默认阈值不同，需要用自定义格式化复现。字符串转义、键排序、整数与 `json.Number` 的处理也都需要对齐。

**结论更正：Go 可以逐字节复现既有摘要合同，"搬动必然改变摘要"不成立。** 因此 `verify_process` 所依赖的 `digest(request)` 可以在 Go 内计算，证据校验**可以在 G1a 内继续补齐**。

## G1a 范围更正与剩余工作

上版索引把 G1a 记为"已搬完"，**该表述撤回**。G1a 按方案要求需"解码、证据校验分别验收"；本记录只完成了证据的**内容摘要原语**。

| 剩余单元 | 内容 | 前置 |
| --- | --- | --- |
| C3 | `canonical` / `digest` 迁入 Go（用上面实测过的编码器），逐字节等价对照 | 无 |
| C4 | `verify_process` / `verify_evidence` 迁入 Go（请求/回执绑定、证明集合检查、结果比较） | 依赖 C3 |

`verify_source` / `validate_binding` 仍是宿主编配器生命周期策略，随 G2b/G2c。

## 附带验证：重签路径在真实流程中生效

本轮记录写入时工作树状态为 `M .gates/results/go-engine-C2.json`——已跟踪文件被修改的形态。上一版 `issue_record` 会因为 `.strip()` 吃掉前导空格而在此误判失败；**这次写入成功**，等于在真实流程里验证了 P2 的修复。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     87895e411194d2266fb8e1a7293f1febe5babd9d
上一基线 20b0422e1292c0a2812f03410e3390c4a129f58d（C1）
运行时身份 feb07dde8320e58e7e00328dff9801a49e7b237db1a890945f91633720178f16
驱动二进制 2aa0fcb65db661d713bfe32710677e541fc1c102d70d994e1c9633a9f0b09330
证据清单 64 个文件
```

## 累计回归的实际结果

| 项 | 耗时 | 说明 |
| --- | --- | --- |
| `go-test-full` | 410.2s | 26 包、1932 用例通过事件、0 失败、`stall=None`；`internal/validate` 403.1s |
| `python-unittest` | 47.3s | 117 项 |
| `sqlite-probe` | 2.7s | CGO 开/关各 8/8 |
| `demo-fixture` | 3.3s | 达 `DEMO_COMPLETE` |
| 合计 | 470.1s | |

## 未解决问题

沿用既有项。本单元变更：

| 编号 | 状态 |
| --- | --- |
| `CANONICALISATION-FORMAT-BLOCKED` | **撤回**。实测表明 Go 可逐字节复现 Python 的规范化字节（448 样本 0 差异），该"阻塞"不成立；改为 C3/C4 两个待做单元 |
| `PROOF-CALL-CROSSES-PROCESS` | 保留（6.1 ms/次，已测量） |
| G1a 未完成 | 明确登记：C3、C4 待做，本记录不宣布阶段结束 |

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard 2ed75d4eeefdfcd97b4f3a5d71e6a0f96219ef77
```

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-C2.json --next-unit C3
go test ./internal/harness -run 'TestFrameResult|TestFileProof' -v
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests \
  -p 'test_result_frame_differential.py' -v
```

本记录只汇总已观测结果；日志保留原始失败，不以最终通过覆盖中间尝试。
