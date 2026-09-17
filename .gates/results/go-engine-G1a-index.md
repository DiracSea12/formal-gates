# G1 状态索引（供外部评审）

> 文件名沿用 `go-engine-G1a-index.md`（C4 记录的 `bookkeeping` 引用它），内容自 M1 起覆盖 G1b。

G1a 是分块替换的第一阶段：**只读计算**。方案要求解码与证据校验**分别验收**。

> **G1a 已完成。** 四个替换单元（C1 结果解码、C2 证据内容摘要原语、C3 规范化与内容摘要、C4 证据校验）各自通过并被固定为下一基线。
>
> **G1b 未收口。** 方案要求比较材料、反馈包装、工件发布**分别验收**，三项中**只有两项通过**：**M1 比较材料**（候选 `f51d1ac`）、**M2 反馈包装**（候选 `dfa23e7`）；**M3 工件发布（候选 `d71113e`）的原 `PASS` 已于 2026-09-17 撤回**——外部独立验收与代码审查查出 **1 项 P1 功能回归**（读回容量小于发布容量，已发布并被引用的工件读不回来）与 **1 项未解释的必需回归错误**（第一次完整回归末态 `KeyError('terminal')`）。返修候选已产出但**尚未验收**，故 G1b 三项未全部通过。
>
> **G2a 未收口（命令作业半通过，承载原生传输半受阻）。** G2a 有两半：**命令作业**（Go 监督器的直接 argv 路径）在候选 `63b0650` 上**通过**；**验证它承载固定的原生传输**（方案 §13.1：G2a"先验命令监督，**再验其承载原生传输**"）在 2026-09-17 经授权执行一次真实 DSH 运行后**受阻**——运行在第一个角色处停在宿主权限请求上，宿主返回 `DSH_INTERACTION_REQUIRED`。按方案 §13.1"必要原生验证受权限、服务或费用授权阻塞时，当前单元保持未验收"与 §14.3"必需测试未结束、被跳过或原生验证受阻 | 保持未验收；不将缺测视为 PASS 或改标不适用"，**G2a 保持未验收，G2b 不得开始**。
>
> **G2a 的记录基线已失效**：其 `previous_accepted_baseline` 写作 `M3`，而 M3 现已 `FAIL`。命令作业半是在含该 P1 的候选（驱动二进制 `8bca2e2c…`）上签署的，因此不能再按"建立在已通过基线之上"来读。G2a 的处置（`BLOCKED`）本身不变。
>
> **ZCode 宿主接入：我方两层已修完，剩下的是宿主账号状态（2026-09-18 更新）。** 本机 ZCode 升到 3.12.3，app-server 的**打包与协议面都变了**。`doctor-zcode` 是验收器 9 个必需项之一、**每个单元都要过**。三层成因已按证据分开：①**我方陈旧假设**——内置 provider 目录位置（`6757231`，定点灵敏度 + 独立探针验证）；②**我方未跟上协议面**——`workspace/readState` 被移除、`session/create` 不再给出已选模型，已在 G2c 范围内适配（`8a06fc1`：直接取 `session/resume`/`session/create`、校验 `ZCode Protocol v1` 握手、只经 `session/setModel` 施加选择并回读确认）；③**宿主账号状态——已判定，不再是"待用户裁定"**——app-server 的 worker provider registry 在本机**没有任何可选 provider**。可选集合＝`account:*`（宿主自己的就绪事件每次都写 `providerCount: 0`，8 个 account provider **全部 `entitled:false`**；宿主自己的别名映射把 `builtin:bigmodel-coding-plan` 指向 `account:bigmodel-individual-coding-plan`）∪ personal 规则（全机唯一一份 `~/.zcode/v2/provider_config.json`，`providerRules: []`）。宿主连**它自己的 `model.main`** 都回 `-32603 Provider Registry 中不存在 Model` ⇒ 是"没有可选项"，不是"没选中"。启动侧每一种可控输入都已实测排除（包内 seed／host 的 active catalogue／成对命名／桌面端转发的 `ZAI_*` 与 endpoint 变量／桌面端 `spawnArgs` 里的 `--stdio`／隔离数据根内放真实凭据与设置副本）；**阳性对照未能构造出非空注册表**，故不宣称任何解锁步骤。按方案 §12.5，被宿主状态阻塞的必需原生验证 ⇒ **M3 返修候选保持未验收**，且因实测契约等价而**不改代码**（§12.5 禁止为无法证成的差异叠加改动）。`doctor-zcode` 现在的失败点是 ③（`ZCODE_CONFIG: native selected model missing`），不是我们的闭世界假设。**上一轮把成因记成"宿主缺件、产品无关"的 `M3-REPAIR-BLOCKED-BY-HOST-ZCODE` 已撤回。** 详细证据与判定见 `.harness/validation/zcode-protocol-3.12.3-probe/DIAGNOSIS.md` **第 6 节**（本轮把上一轮的"待用户裁定"换成了判定），探针为同目录 `selection-path.py`、`launch-contract.py`、`positive-control.py`、`positive-control-legacy.py`、`argv-stdio.py`。**本轮复核（第 7 节）**另用 `current-snapshot.py` 直接读宿主自己的 `session/create` 应答（两种启动契约均为 `current: null`／`available: 0`、stderr 空），确认本机三份目录同为 revision 28 且无 `builtin:*` 规则；并在**不含 `6757231` 的旧分支**上复跑同一命令，得到 `ZCODE_TRANSPORT: Native output closed` ＋ 宿主 stderr `无法定位 CLI ZCode Built-in Provider Config` ⇒ 该提交的必要性可复现。表格中 `…PROVIDER-REGISTRY-EMPTY` 一行原先的"待定 (a)/(b)"已按判定改写。  **【更正 2026-09-18】③ 的判定已被推翻。** 桌面端（用户确认"能正常对话"）自己的日志显示**同账号下宿主机 `providerCount: 2`**，而我方**每一次无人值守启动都是 0**；补齐三个此前从未试过的输入——桌面端的可执行文件 （`ZCode Helper`）、其逐字 argv（`app-server --stdio --surface desktop`）、以及 `ZCODE_SERVICE_AUTHORITY_MODE` 的三种取值——后**仍是 0**。⇒ 不是"本机没有可选 provider"，而是**无人值守启动拿不到桌面客户端在启动时递进来的那份 provider 运行时上下文**；第六节"启动侧输入已逐条排除"的结论**不成立**（漏了这三个输入）。阻塞性质随之变为**方案层面**（§12.4 单列的那一类），不是"用户没配好"。详见 `DIAGNOSIS.md` **第 8 节**（撤回第六/七节），探针 `desktop-contract.py`、`authority-mode.py`。 **【根因已定 2026-09-18】不是本机没 provider，也不是用户没配**：ZCode 桌面端宿主进程在运行时把 provider 配置**投递**给 agent（日志 `zcode-agent-service`：`account provider config 已交付到 ZCode agent {"providerCount":8,"reason":"startup_ready:model_selection_changed"}`；`provider/model 就绪后已启动等待中的 ZCode agent {"providerCount":1}`），并订阅 `onDynamicWorkspaceProviderRuntimeHeadersRequest` 供 agent **反向索取授权头**。⇒ agent 自身**没有**注册表，**无人值守的裸启动缺少投递方**；本传输对 `interaction/requestProviderRuntimeHeaders` 按设计直接拒绝（`ZCODE_INTERACTION_REQUIRED`），即不实现该反向通道。详见 `DIAGNOSIS.md` **第 8.4 节**。结论：`doctor-zcode` **无法靠本机配置解决**，需方案层面裁定（桌面端在场并提供投递通道＝§12.4 那类能力，或对原生执行不在范围的单元重新定性）。
>
> **命名更正**：本索引此前把 G2b 写作"原生定向验收关口"，这是错的。方案 §13.1 的表把 **G2b 定义为"DSH 薄适配"**；原生定向验收是 **G2a 自己的后半**。G2a 的记录也同错，已一并更正。**同一错误还在单元合同里**（`go-engine-G2a-contract.md` 第 178 行"授权关口在 G2b 之前"）：合同是派发前固定、与候选同批落地的历史件，改写它会动到候选，故**不改动**，更正以本索引与 G2a 记录为准。

## 基线与验收链

每个替换单元在上一已验收基线上实施，累计组合通过后固定为下一基线。源码提交与验收结论分开提交。

| 单元 | 内容 | 候选提交 | 记录提交 | 结论 |
| --- | --- | --- | --- | --- |
| B0 | 基线冻结与标定（含验收工具、SQLite 锁定） | `41d3375` | `284ceff` | `PASS` |
| C1 | 结果解码 | `20b0422` | `a756cde` | `PASS`（后被独立验收发现回归，见 C2 重签） |
| C2 | 证据内容摘要原语 + C1 回归修复 | `87895e4` | 见 `go-engine-C2.json` | `PASS` |
| C3 | 规范化序列化与内容摘要 | `7492c01` | `6ae01c2` | `PASS`（重签；见下） |
| C4 | 证据校验（G1a 收尾） | `9687945` | 见 `go-engine-C4.json` | `PASS`（重签；见下） |
| M1 | 比较材料（G1b 第一项） | `f51d1ac` | 见 `go-engine-M1.json` | `PASS` |
| M2 | 反馈包装（G1b 第二项） | `dfa23e7` | `72b5aba` | `PASS` |
| M3 | 工件发布（G1b 第三项，G1b 收口） | `d71113e` | 见 `go-engine-M3.json` | `FAIL`（原 `PASS` 已撤回，见下） |
| G2a | 命令作业（Go 监督器的直接 argv 路径）+ 验证它承载原生传输 | `63b0650` | 见 `go-engine-G2a.json` | `BLOCKED`（命令作业半 `PASS`；承载原生传输半受阻，见下） |

**M2 行是本次补的**：M2 的记录提交改了本索引的两张表，却漏掉了上表的 M2 行。记录为 `INDEX-MISSING-M2-ROW`。

**M3 记录的 `PASS` 由外部独立验收与代码审查撤回**（不是其自身运行器发现的——该次运行的九项检查当时全绿）。两项发现：`M3-PUBLISHED-ARTIFACT-UNREADABLE-B64-LIMIT`（P1，读回容量小于发布容量）与 `M3-CUMULATIVE-WORKFLOW-NO-TERMINAL`（未解释的必需回归错误）。返修候选已产出（`c4bac49` + 未提交补丁），**尚未验收**，故本行保持 `FAIL`。机理与证据见 `.harness/validation/m3-repair-capacity-20260917T2330Z/CLASSIFICATION.md`。

C3 记录取代了绑定 `04f343d` 的上版记录：该候选存在一处由独立验收发现的 P1 合同回归（大整数在桥接路径上被拒），修复后候选前移到 `7492c01`，原结论不再适用。**C4 记录取代了绑定 `0f1cd2b` 的上版记录**：该候选存在一处由独立验收发现的 P1 数值语义缺陷（证据大小按两种不一致的整数转换规则比较，既误拒绝又误接受），修复后候选前移到 `9687945`。C2 记录同样取代了绑定 `2ed75d4` 的上版记录。**M3 记录取代了它自己原来的 `PASS`**：该候选存在一处由独立验收发现的 P1 功能回归（读回容量小于发布容量）与一处未解释的必需回归错误，原结论不再适用；返修候选尚未验收。分支 `codex/go-engine-v1`，基线起点 `c6d49323`。

## 三次由独立验收发现、我未自查出的回归

| 单元 | 缺陷 | 我的测试为何没发现 |
| --- | --- | --- |
| C1 | 说明中提及 NaN/Infinity 会让合法结果被拒 | 语料偏向"结构变体"而非"语义相邻文本" |
| C3 | 超出 float64 的整数在桥接路径被拒 | 单测直接调编码器，绕过了前置扫描器 |
| C4 | 证据大小按两种整数转换规则比较（`7.0` 误拒、`7.75` 截断误接受） | 两套测试都只写整数大小；`0` vs `0.0` 的用例**两边都设成了 `0.0`** |

三次都在**公开调用链上**成立、而在**直接函数测试**下看不出来。C4 起已把"经桥接的端到端等价用例"作为纪律，并对新增用例补了**负控制**（撤回实现后确认用例确实失败）。

**M1、M2、M3 是前三条由子代理实施、我独立复核的单元。** M1 复核中未查出该级别回归；**M2 查出 3 处**（`role` 非字符串被改写、revision 绑定把数字字符串化、残留的内联 `__import__`），其中前两处与 C4 的 `7.0`／布尔大小**属同一模式**：类型边界上的静默收窄。这说明该模式被反复触发，而"经桥接的端到端等价用例 + 负控制"是**由我**在外部评审之前发现的——但不代表外部评审会找不到别的问题，本表的其余三条仍全部来自外部。M2 的这两处已各配回归，并以负控制证明回退后用例会失败。

**M3 查出 1 处（D1）**：交付的差分测试让两个实现共用同一个工件目录，使被测的写路径从未按值被差分、两棵工件树也从未被互相比较。它没有造成可观察行为偏差，但让"重入不覆盖已绑定内容"的唯一直接证据退化为顺带成立，故按缺陷处理。D1 同样是**我在外部评审之前**查出的；修复的第二次派发没有产生任何改动（子代理返回空），我因此自行实施修复，并已在 M3 记录中如实登记。

**G2a 查出 3 处（D1、D2、D3），三处都被子代理的自述报告为"全绿"。**

| 编号 | 缺陷 | 性质 |
| --- | --- | --- |
| D1 | PATH 走查在第一个"存在"的候选处停住（`os.Stat` 预筛），而 `execvp` 会继续搜索 | **实现缺陷**，自建 oracle 16 个场景中 5 个分歧 |
| D2 | 组信号取消时收据不确定：`130` 与子进程自身的 `-15` 交替 | **实现缺陷**，参考实现 20/20 恒定 |
| D3 | 四个取消用例等到 `process.json`（写在 handler 装载**之前**）就发信号，抢跑启动窗口 | **用例缺陷**，只在完整验收的负载下 20 轮中失败 1 轮 |

D1、D2 由自建 oracle 在验收前查出，修复由第二次派发完成。**D3 特殊之处在于它只在完整验收里才现形**：单独跑该包 5 次全过，而 `go test ./...` 的负载下失败——这正是"以完整验收为准、不以单包通过为准"的经验依据。D3 的处置是**修用例、不动实现**，因为那个窗口是参考实现固有的，在窗口里补写收据会让 Go 在参考实现根本没有收据的地方给出收据（`Store.job` 对缺失收据的处置是 `RuntimeBlocked(UNKNOWN_JOB)`，可观察地不同）。

**与既有模式的对应关系**：

- **D1 是"类型边界上的静默收窄"的第 7 次出现**（前六次：C1 的 NaN 预扫描、C3 的 float64 收窄、C4 的整数截断与 `7.0` 误拒、M2 的 `role` 改写与 revision 字符串化）。它把**内核对候选的可执行性判定**换成了 `os.Stat` 的存在性判定——动态语义被实现成更窄的静态形状，与前六次同型。
- **D2、D3 是新的一类**：不是类型收窄，而是**把间接证据当成直接证据**。D2 用一个并发边界上共享的标志代替"这次死亡是否由取消引起"的直接判定；D3 用一个更早写入的标记（`process.json`）代替"handler 确已装载"的就绪证据。两者都是"信号比证据先到"的同一个错误。
- 三处都不是笔误。三处已各配回归用例（D1：`path_search_oracle.py`；D2：`cancel_oracle.py` + `TestSuperviseCancelGroup`；D3：四个取消用例的 marker 等待）。

## 独立验收三项，逐项处置结果

| 项 | 结论 | 处置 |
| --- | --- | --- |
| P1 说明中提及 NaN/Infinity 会拒绝合法结果 | **真实回归，已复现** | 非有限数字判定从"整条消息预扫描"改为"在解析器所在位置检查正在解析的片段"；补齐"说明中提及→接受"与"对象内实际包含→拒绝"两个方向共 7 个用例 |
| P2 重复空字符串键未被拒绝 | **真实漏检，已复现** | "是否发现重复"与"重复键的值"分离；同处第二个隐患（非字符串键伪装成空键重复）一并修掉 |
| G1a 范围的理由不足 | **成立** | 撤回原理由，并给出实测依据（见下） |

## 范围更正：原"阻塞"不成立

原索引称"把 JSON 规范化搬到 Go 会改变 Python 侧每一个摘要，必须另作版本化迁移"。**该理由过强。**

真正的问题只是"Go 的默认编码器与 Python 不同"。我据此实现了复现 Python `json.dumps(ensure_ascii=False, sort_keys=True, separators=(",",":"))` 的 Go 编码器并逐字节对照：

| 样本 | 结果 |
| --- | --- |
| 448 个：200 个跨约 40 个数量级的随机浮点、大整数、次正规数 `5e-324`、`1e15/1e16/1e21` 阈值、Unicode 非 BMP／组合字符／RTL、控制字符、`<`/`&`、U+2028/U+2029、空键、150 个随机嵌套结构 | **逐字节不一致 0 个** |

关键难点是浮点格式化（Python `repr(float)` 的定点/科学计数法阈值与指数位数规则）与字符串转义规则，均可复现。

**因此证据校验可以在 G1a 内继续补齐。**

### C3 的逐字节等价证据

迁移前实现保存在仅测试使用的 `reference_canonical.py`，450 个样本（其中 300 个是固定种子、跨约 40 个数量级的随机浮点，另有次正规数、`1e15/1e16/1e21` 与 `1e-4/1e-5` 阈值、Unicode、控制字符、`<>&`、U+2028/9、空键、100 个随机嵌套结构）**字节与摘要 0 差异**。另有一层独立证据：122 项 Python 测试全过，工件引用、证据摘要、作业请求摘要与增量绑定路径全部仍然吻合。

关键边界：桥接信封原先用 `canonical` 序列化自己，会无限递归；现改用私有**传输编码**（紧凑、保留非 ASCII、**不排序**），并有测试断言它不等于规范字节——防止信封悄悄变成第二实现。

## G2 的剩余工作

| 单元 | 内容（方案 §13.1 的定义） | 状态 / 前置 |
| --- | --- | --- |
| G2a | 命令作业：**先**交付 Go 监督器的直接 argv 检查路径，**再**验证它承载固定的原生传输；不同时更换宿主协议 | **未收口**：命令作业半通过（候选 `63b0650`）；承载原生传输半 **受阻**，见下 |
| G2b | **DSH 薄适配**：在已验收监督器下移植 DSH 的请求、身份、结果和错误映射；ZCode 保持原实现 | 前置为 **G2a 收口**（当前未满足） |
| G2c | ZCode 薄适配：使用同一公共边界移植 ZCode；DSH 实现不同时重构 | G2b 之后 |
| G2d | 原生交互：单独交付原生请求的持有、展示、投递和原调用继续；按宿主逐个验收 | 按宿主逐个验收 |

**更正（2026-09-17）：G2a 后半的真实受阻成因已定位，此前的归因是错的。** 2026-09-17 经授权执行的一次真实 DSH 运行在第一个角色（产品审）处停住。此前的叙述把触发者写成"模型自己请求越出只读沙箱"——**不成立**。该角色先正常 `read` 了绑定输入，紧接着的 `bash`（只读勘察）**被宿主自身拒绝**：工具结果原文为 `sandbox mode "read-only" is requested but no sandbox backend is usable on this host; refusing to run the command unconfined` + `Runner failure: sandbox-exec: sandbox_apply: Operation not permitted`，并附 `{"name":"SandboxUnavailableError","code":"SANDBOX_UNAVAILABLE"}`。模型是**在 `bash` 完全无法执行之后**、按该结果自己打印的提示（"otherwise switch the consumer to danger-full-access"）请求最小放宽的；DSH 据此发出 `session/request_permission`，而本版本**没有**原生答复通路，故如实报告 `DSH_INTERACTION_REQUIRED`。**独立探针**（不含 harness/Python/Go 监督器）确证故障在外层上下文：`/usr/bin/sandbox-exec -p '(version 1) (allow default)' /bin/echo OK` → `sandbox_apply: Operation not permitted`，退出 71。**"这不是回归"的结论不变，依据换成该裸探针**；路径范围与配置差异的排除仍然有效。

这与既有的 `ZCODE-HEADLESS-BLOCK` 同型——先前验收记录对 ZCode 在真实 Edit 审批处的同类阻塞写的正是"这符合既定交互边界"。方案 §12.4/§13.1 把原生交互**单列为 G2d**，正是为了让 G2a/G2b 不顺手获得该能力；§6.2 与 §14.2 明令"不靠免审批模式让测试通过""测试不靠临时强制授权凑通过"。

本次阻塞**可复现**：19:59 在同一配置下重跑（驱动二进制 `8bca2e2c…`、`runtime_identity a50f2289…`、DSH `0.1.5-rc.1` 均与首次逐字相同），停在同一位置、理由逐字相同。证据在 `.harness/validation/g2a-native-rerun-20260917T115915Z/`（`DIAGNOSIS.md`）。

对照同一宿主、同一版本下先前那次七个角色全部通过的运行（`live-20260915-2050`）：工具策略逐项相同（仅 job 目录路径不同），该次 32 次 `bash`（其中 **16 次**越出 job 目录、**13 次**运行同类检查脚本）**0 次**提权、七个作业**0 次**权限请求。**这份对照只用来排除"路径范围"与"配置差异"**，不能用来反驳本次运行的成因：09-15 那次运行的外层上下文能 apply 只读沙箱，本次不能。

**Go 监督器的那一半是被证明的**：它把原生入口 argv 当普通子进程执行、写出了六字段收据（`exit_code=1`、`error=""`、`request_digest` 与两份流证明齐备），且 `native-process.json` 显示**监督器是进程组组长**（pgid == 自身 pid）而原生子进程是它的子进程；迁移前的 Python 监督器已不可运行（模块已删），故那份收据只能来自 Go 二进制。**缺的是完成关联**——没有任何角色抵达完成的轮次，而方案 §13.1 给该检查规定的最小证据含"原生请求及**完成关联**"。证据在 `.harness/validation/g2a-native-20260917T103650Z/`。

`Store.job` 在本单元只改了**一行** spawn argv，其余（owner 锁、存活探测、收据轮询）与 `Store.cancel_jobs` 全部未动。原生传输本身由 Python 侧的 `formal_gates_harness.native_entry` 承载，Go 只负责把它当普通子进程执行——G2a 后半要验收的正是这一点。

仍是宿主编配器生命周期策略、随 G2b/G2c 迁移的：`verify_source` / `validate_binding`。

## 本阶段搬动的八项

| 单元 | 新归属（Go） | 桥接操作 | Python 侧 |
| --- | --- | --- | --- |
| C1 | `internal/harness.FrameResult` | `frame-result` | `native_result.decode_result` 变为适配器 |
| C2 | `internal/harness.FileProof` | `file-proof` | `infra.file_proof` 变为适配器 |
| C3 | `internal/harness.CanonicalJSON` / `CanonicalDigest` | `canonical`、`digest` | `infra.canonical` / `infra.digest` 变为转发 |
| C4 | `internal/harness.VerifyProcess` / `VerifyFiles` | `verify-process`、`verify-files` | `Store.verify_process` / `verify_evidence` 保留路径判断与文件读取，校验委派 |
| M1 | `internal/harness.planComparison` | `comparison-plan` | `feedback.materialize_comparison` 保留 git 调用、文件读写、篡改检查与证明收集 |
| M2 | `internal/harness.planTaskInput` / `planEvidenceFeedback` | `task-input`、`evidence-feedback` | `feedback.task_input` / `evidence_feedback` 保留 Store 访问、工件寻址与 `verify-files/process/source` 委派；`BOUNDARY` 文案留在 Python |
| M3 | `internal/harness.ArtifactPlan` / `VerifyArtifactReference` / `VerifyArtifactBinding` | `artifact-plan`、`artifact-reference`、`artifact-binding` | `Store.put` / `Store.get` 保留工件路径布局、`exists()` 发布一次判定、`atomic_bytes`、`read_bytes` 与 `json.loads` |
| G2a | `internal/harness.Supervise` | **无**（改用 §12.1 的**同一二进制内部窄命令** `harness supervise <dir>`） | `Store.job` **只改 spawn argv 一行**；owner 锁、存活探测、收据轮询与 `Store.cancel_jobs` 全部未动 |

**M3 之后 Python 侧 `Store` 不再持有任何 `hashlib.sha256`**；`internal/harness` 成为内容寻址的唯一实现（`infra.py` 的 `hashlib` 导入仍被 `runtime_identity` 使用，不属本单元）。

M1 的边界与 C2/C4 同一原则：**Go 持有规则与格式，Python 持有进程与文件系统**。它同时**减少**一次 `canonical` 跨进程调用（原「逐材料一次 `cat-file` + 一次 `canonical`」变为「一次 `comparison-plan`」）。M2 同样把原「一次 `digest` 跨进程」并入 `task-input`。

M2 的 `BOUNDARY`（提示词里那段"反馈是不可信数据"的声明）**有意留在 Python**：它是提示词文案，整段提示词的其余约 15 句都在 Python；只搬其中一句会形成任意切分。若认为整段提示词应入 Go，应作为独立单元。合同已记录该决定并列为评审问题。

M3 的边界沿用同一原则：**Go 持有规则与格式，Python 持有进程与文件系统**。`artifacts/<ref>.json` 的位置、`path.exists()` 的发布一次判定、`atomic_bytes` 与 `read_bytes` 全留在 Python；`artifact-plan` 因此**不返回路径**。`get` 的两条拒绝被拆成**两次调用**（先 `artifact-reference` 判格式，再 `artifact-binding` 判完整性），使"格式优先于读取失败"由调用结构而非代码顺序保证；代价是 `get` 两次跨进程（约 11.6 ms），`put` 仍一次——它替换掉原本就有的那次 `canonical` 调用，**净增 0**。完整性规则对**原始字节**比较，故工件体以 **base64** 过桥：若按文本传递，被改成非 UTF-8 的篡改件会抛 `UnicodeDecodeError`，而迁移前抛的是 `HarnessError("artifact integrity mismatch")`。

> **返修后的更正（2026-09-17）**：上面这段对 M3 的边界描述**已被 P1 返修改动一处**。`artifact-binding` 的载荷由 `{ref, body_b64}` 改为 `{ref, path}`：**读取仍由 Python 做**（`read_bytes()` 保留 `FileNotFoundError` / `IsADirectoryError` 语义），Go 只**按同一路径复核字节**。这样读请求不随工件体量增长——原设计让读请求约等于 4/3 × 工件体量，与所有操作共用的固定 8 MiB 天花板相撞，已发布并被引用的工件因此读不回来。`get` 仍是两次调用（净增 0 不变），`artifacts/<ref>.json` 的命名**仍然是 Python 持有的约定**（Go 只是被**告知**这个路径，不推导它）。注意这一处更正使原"让 Go 读文件会逼 Go 复现异常语义"的否决理由失效：返修让 Go 读文件**做比对**而不做**返回**，避开该代价。

G2a **是这条原则的第一次边界位移，而不是它的例外**。前七项都在"Go 持有规则与格式，Python 持有进程与文件系统"下划界；G2a 搬动的恰恰**就是进程所有权**，与桥接操作的前提（请求/响应、短调用）直接冲突——监督器要长期存活、要在收到信号时自己改写收据，子进程必须是**监督器的**子进程而非 Python 适配器的。因此改用方案 §12.1 的另一条路径：**同一二进制的内部窄命令**。代价是引入第二类进程间接口（argv + 退出码，无信封），收益是进程所有权与信号语义不必被翻译。

边界随之变为：**Go 持有"取得所有权到写出收据"的全过程**，Python 只保留"何时启动、如何轮询、如何取消"。

这也意味着 §12.1 的两条路径在本项目里都已实际使用（桥接操作 C1–M3、内部窄命令 G2a），**"新单元该走哪条路径"本身成为需要论证的设计决定**，已列入下方评审问题。

**前提变更（有意，非删除）**：`tests/test_boundary_recovery.py::test_artifact_integrity` 原先用 `Bridge('unused')` 证明"工件读写不需要桥接"；M3 之后 `put`/`get` 都走桥接，该用例改用真实 bridge（`driver_bridge()`），断言逐字保留。合同第 117 行已记录该变更。

三处拒绝**不走桥接失败形状**，而走 `value` 里的类型化结果（`{"accepted": false, "refusal": ...}` / `{"ok": false, "refusal": ...}`）。C4 与 M3 的拒绝另带 `kind`，由 Python `refuse()` 映射回原异常类。失败形状（`{ok:false, code}` + 退出码 2）保留原义，用于"桥接本身坏了"——M3 中一份**非法 base64 的工件体**走这条路径，因为那是调用方错误、不是业务拒绝。

## 复核命令

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：读取最新一条记录（G2a）并尝试放行 G2b
# **预期被拒绝**：G2a 的总体结论是 BLOCKED（后半受阻），故不得放行 G2b。
# 实际输出：`ADMISSION REFUSED: G2a: recorded overall conclusion is 'BLOCKED'`，退出码 1。
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-G2a.json --next-unit G2b

# 完整验收（新尝试目录，不覆盖历史）
# 注意：`--previous-baseline M3` 现已无效——M3 的结论已撤回为 FAIL，
# G2a 不能再按"建立在已验收基线之上"重跑；重跑需先有一个通过的 M3 返修记录。
python3 scripts/go-engine/acceptance.py --unit G2a --previous-baseline M3

# 两个工具自身的控制项（需要干净工作树）
sh scripts/go-engine/negative-controls.sh "" .gates/results/go-engine-G2a.json

# 历史单元的复核入口仍有效，逐条读取各自记录即可
# **预期被拒绝**：M3 的总体结论已于 2026-09-17 撤回为 FAIL（P1 容量回归 + 未解释的必需回归错误）。
# 实际输出：`ADMISSION REFUSED: M3: recorded overall conclusion is 'FAIL'`，退出码 1。
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-M3.json --next-unit G2a
```

> G2a 之前的单元（B0、C1–C4、M1–M3）的准入命令只在其记录中的候选仍是 `HEAD` 祖先时通过；`admission.py` 会因"候选已移动"拒绝，这是**预期行为**，不是故障。要复核历史单元请用其记录内的"复核入口"在对应的候选提交上操作。

> 手工跑 Python 套件请同时设两件事：**已解析**的临时目录 `TMPDIR=/private/tmp`，以及 `PYTHONPATH="$PWD/runtime/langgraph"`。前者避开 `test_output_recovery.py` 的一个用例报错（未解析符号链接），后者覆盖掉宿主注入的 `sitecustomize` 垫片（见 `HOST-SHIM-BREAKS-PATH-OPS`）。两者都与本迁移无关，验收器自身已按此设置。

| 文件 | 内容 |
| --- | --- |
| `.gates/results/go-engine-B0.md` / `.json` | 基线冻结记录 |
| `.gates/results/go-engine-C1-contract.md` … `C4-contract.md`、`M1-contract.md`、`M2-contract.md`、`M3-contract.md`、`G2a-contract.md` | 实施前固定的单元合同 |
| `.gates/results/go-engine-C1.md` … `C4.md`、`M1.md`、`M2.md`、`M3.md`、`G2a.md` 与同名 `.json` | 单元验收记录 |
| `.gates/results/g2a-probes/` | **G2a 的独立裁判与实测脚本**：`reference_semantics.py`（迁移前语义的重新实测）、`path_search_oracle.py`（D1）、`exec_semantics_oracle.py`（`-N` 退出码与空 PATH 分量）、`cancel_oracle.py`（D2）。四者都**不依赖**交付的差分测试与参考模块的调用方式 |
| `scripts/go-engine/README.md` | 验收与准入工具的约定、判定条件、控制项清单 |
| `scripts/go-engine/dependency-lock.json` | Go/SQLite 驱动与依赖锁定 |

原始证据在记录的 `attempt` 字段所指目录，含 `evidence.json`（逐文件摘要）、各必需项 `summary.json`、每个命令的 stdout/stderr/exit/cmd。每次尝试使用全新目录，已存在即拒绝，**不被后续运行覆盖**。

## 明确未验证的事项

- **真实原生模型流水线**：`NATIVE-REVERIFY-AT-BASELINE` 未执行。**G2a 需要有原生定向验收**——它搬的是**进程所有权**，按方案 §13.1 第 513 行属"改动涉及…进程…时，实测受影响宿主的对应路径"，按同章第 520 行属"涉及宿主或进程行为的单元必须增加对应真实调用，不能用录制数据代替"。2026-09-17 经授权执行过一次，**受阻**于宿主权限请求（见"G2 的剩余工作"），故仍是未验证事项。C1–C4、M1–M3 则确实**不需要**：C1–C4 是纯计算，M1 另有 Python 侧本地 git 调用，M3 另有 Python 侧文件系统读写；它们的差分与独立裁判全部在本地子进程上完成，`demo-fixture` 达到 `DEMO_COMPLETE` 也只是夹具流程，不算原生验收。
  - **本索引此前在这里写错了一句**：原文称"G2a 也**不需要**原生定向验收……授权关口在 G2b 之前"。那是把 G2a 的后半误记成了 G2b 的关口（见文首命名更正），并且与 §13.1 第 513/520 行冲突。更正后的读法见上。
  - `live-20260915-2050` 的原生证据绑定更早的提交与运行时，**不可**当作当前候选的原生验收；它在本索引中的用途只有两个——排除"路径范围"与"配置差异"，以及给出"同一宿主同版本下七个角色可以跑通"的对照。**它不能用来判断本次运行为什么停下来**：09-15 那次的外层上下文能 apply 只读沙箱，本次不能（见"G2 的剩余工作"的更正）。
- **跨平台**：只在本地 macOS 实测。
- **M3 的返修候选**：已产出（定点证明、Go 全量、Python 全套 199 项、隔离回放均通过），但**尚未完成验收**，因此 `M3-PUBLISHED-ARTIFACT-UNREADABLE-B64-LIMIT` 的关闭**尚未被确认**。在完成验收并把记录结论改为 `PASS` 之前，G1b 不得按已收口处理。挡路的是 `doctor-zcode`：`ZCODE-APP-SERVER-CATALOGUE-LOCATION-STALE`（已修 `6757231`）与 `ZCODE-APP-SERVER-PROTOCOL-SURFACE-CHANGED`（已在 G2c 范围内适配 `8a06fc1`）都已不再挡路，现在挡路的是**宿主自身的 provider 注册表为空**。
- **ZCode 宿主接入（剩余阻塞已换性质）**：协议面已适配（`8a06fc1`），`doctor-zcode` 现在过会话建立与 `ZCode Protocol v1` 握手，失败在宿主自报"没有已选模型"。**已判定**：这不是我们的命名造成的——3.12.3 按 `resolveNodeProviderRuntimePaths` 要求 built-in 与 personal 两个 provider 配置文件**同时提供或同时不提供**，三态探针显示三种情况下注册表**都为空**。宿主真正读到的那份 personal provider config（`~/.zcode/v2/provider_config.json`）规则为空；用户可见的 provider 定义在 `~/.zcode/v2/config.json` 与 `~/.zcode/cli/config.json` 里（但对 app-server 不可见）。**未验证**：宿主重新具备可用 provider 是否足以解锁（即"在 ZCode 里配置/启用 provider"是否会写进 personal provider config）——本轮没有构造出非空注册表，故不宣称任何解锁步骤。 **【2026-09-18 更正】本节成因已撤回**（见 `DIAGNOSIS.md` 第 8 节）：桌面端能正常对话、其宿主机自报 `providerCount: 2`，而无人值守启动恒为 0；补试桌面端可执行文件／`--surface desktop`／`ZCODE_SERVICE_AUTHORITY_MODE` 三值均无变化 ⇒ 剩下的是"无人值守拿不到桌面端启动时递进来的 provider 运行时上下文"。
- **身份已因该修复前移**：传输属运行时身份的一部分，故 `6757231` 使身份由 `37194cd2…`（记录里 8/9 那次尝试在 `9e4a622` 上绑定的值）变为 **`2b29d6d5…`**（HEAD `0eebcc6` 上重算）。按 §12.5，**记录中那次尝试已不再描述当前树**；其九项结果作为"那一次尝试的观测"保留，任何重新验收都必须在返修候选上运行并绑定它自己算出的身份。驱动二进制摘要未变（`a4d9ac94…`，可重复构建）。
- **`M3-CUMULATIVE-WORKFLOW-NO-TERMINAL` 的触发点**：**未恢复**。类别已由确定性复现确定（check 侧错误经 `check-runtime` 中断浮出、末态无 `terminal`），但三个候选触发站点签名相同、保留物不足。该用例在第二次全套、返修后全套与 10 次隔离回放中均通过——**这些通过不解释第一次失败**，本项保持"未解释"。

## 已知有界差异与风险

| 编号 | 内容 |
| --- | --- |
| `LONE-SURROGATE-ESCAPE` | 文本含转义 `\ud800` 时两侧都接受，但 Python 保留代理项码位、Go 为 U+FFFD |
| `NON-FINITE-CROSSES-TRANSPORT` | 合法 JSON 不允许 `Infinity`，故含非有限浮点的值可经桥接进入编码器、但信封载不动它（C3 起）；不可达——结果路径不会产出非有限值 |
| `INPUT-PATH-NOT-NORMALISED` | C4：参考实现按 `Path` 规范化形式命名缺失的输入文件，Go 用传入的原样字符串；不可达——键由 `str(Path(...))` 写出且工件内容寻址 |
| `BOOL-SIZE-IS-A-NUMBER-IN-PYTHON` | C4：证据 `size` 为布尔值时两侧判定可能不同（Python 中 `True == 1`）；仅手工改写可达，无法由正常写入者产生。经评审定为 P3、非阻塞 |
| `COPY-STATUS-UNREACHABLE` | **M1（覆盖限制）**：生产只传 `--find-renames`，`C`／拷贝记录不可达，故差分语料**不可能**覆盖该分支。该分支由 Go 单元测试与一份逐字取自迁移前解析循环的独立裁判（17 个样本、0 分歧）覆盖 |
| `MALFORMED-RECORD-ERROR-SHAPE` | **M1（P3，不可达）**：畸形记录在迁移前触发**未捕获异常**（`ValueError`／`StopIteration`），现在返回受控的桥接错误。git 正常输出不产生该输入 |
| `TMPDIR-UNRESOLVED-SYMLINK` | **M2（环境陷阱，非迁移缺陷）**：手工跑套件时 `TMPDIR=/tmp`（未解析符号链接）会让 `test_output_recovery.py::test_explicit_adoption_keeps_manifest_and_checkpoint_bytes` 报 1 个错误，`TMPDIR=/private/tmp` 时全过。**已证与本迁移无关**（把 `feedback.py` 回退到 M2 之前，同样报错）。验收器自身使用已解析目录，不影响判定 |
| `HOST-SHIM-BREAKS-PATH-OPS` | **M3（环境陷阱，非迁移缺陷）**：本机宿主经 `PYTHONPATH` 注入 `sitecustomize` 垫片，令 `Path.mkdir(exist_ok=True)` 在目录已存在时抛 `PermissionError`、令 `shutil.rmtree` 走回收站守卫。直接探针复现；用它跑全套报 10–15 个错误（集中在 `test_retention`、`test_comparison_inputs`、`test_checkpoint_startup`）。**以 `PYTHONPATH=<repo>/runtime/langgraph` 覆盖后 174 项全过**；验收器自建 `PYTHONPATH`，判定不受影响。任何手工跑套件的人都会撞上 |
| `ARTIFACT-READ-COSTS-TWO-CALLS` | **M3（有界代价）**：`get` 由 0 次跨进程调用变为 2 次（约 11.6 ms），`put` 净增 0。`graph.py` 中 `get` 会被调用数十次（含列表推导内多次），单次 run 量级为百毫秒、远小于模型调用。合并为一次须接受"格式优先"只靠代码顺序成立，或让 Go 读文件并复现 `FileNotFoundError`/`IsADirectoryError` 的类型与消息。经评审定为三项方案中唯一不需要为异常语义写例外者 |
| `ARTIFACT-LOCATION-STAYS-IN-PYTHON` | **M3（待评审；返修后需重新表述）**：`artifacts/<ref>.json` 的命名未入 Go。§7 只说"正文…在工件目录中"、未规定命名，故按"存储布局属 Python"处理，`artifact-plan` 不返回路径。若认为该命名属寻址格式，应作为独立单元。**返修后**：读侧改为按路径交 Go 复核，该命名已成为**调用方可见的约定**（Go 被告知路径），故"只是存储布局细节"这一读法需要重新论证 |
| `M3-PUBLISHED-ARTIFACT-UNREADABLE-B64-LIMIT` | **M3（P1 功能回归，原 `PASS` 因此撤回）**：读回容量小于发布容量。`artifact-binding` 把工件**体**的 base64 放进请求，与所有操作共用的固定 8 MiB 天花板相撞；约 6 MiB 以上的工件写入成功却读不回来，已完成的工作无法重入。**返修候选已产出、尚未验收**：载荷改为 `{ref, path}`，Python 仍做读取、Go 按同路径复核字节，读请求成为固定大小信封；8 MiB 天花板保留 |
| `M3-CUMULATIVE-WORKFLOW-NO-TERMINAL` | **M3（未解释的必需回归错误，类别已定、触发点未恢复）**：第一次完整回归（174 项）末态无 `terminal`，在 `test_driver.py:54` 抛 `KeyError('terminal')`。**类别已确定性复现**：check 侧运行期错误经 `check-runtime` 中断浮出，`guard` 因此不写 `stage-complete checks`，`evaluate`/`seal` 从未运行。**触发点未恢复**：`UNKNOWN_JOB`、`UNKNOWN_START`、收据之后的 Git 校验三个站点签名相同，而测试自己的 tearDown 删掉了作业目录。已排除的归因：与 M3 的 base64 缺陷同源（`round=0` 的 check 路径不读已发布工件）、"环境慢撞上超时"（固定 10 秒预算 vs 实测监督器启动中位 38 毫秒，约 250 倍余量，而观测到的变慢只有约 9 倍） |
| `M3-JOB-DIRECTORY-LOST-ON-FAILURE` | **M3（流程，导致上一项无法归因）**：`test_driver` 的 `TemporaryDirectory` tearDown 会删掉 `jobs/` 目录，失败时的 `wrapper.stderr`、`request.json`、`process.json` 一并丢失。**建议**：任何 check 侧失败必须先保留作业目录再清理 |
| `ARTIFACT-READ-BECOMES-SIZE-INDEPENDENT` | **M3 返修（结构性收益）**：读请求不再随工件增长，因此"两端容量不一致"这一类缺陷在结构上不再可能；代价是 Go 需要**被信任地**按给定路径读取（读的语义仍由 Python 的 `read_bytes()` 负责，Go 只比对字节） |
| `M3-REPAIR-BLOCKED-BY-HOST-ZCODE` | **M3（已于 2026-09-17 晚撤回并拆分）**：原写作"宿主阻塞，非产品缺陷"。**"非本次返修引入"成立，"非产品缺陷"不成立**，见下两条 |
| `ZCODE-APP-SERVER-CATALOGUE-LOCATION-STALE` | **ZCode（我方陈旧假设，已修 `6757231`）**：3.12.3 把 app-server 的内置 provider 目录从 `<Resources>/glm/provider/` 搬到 `<Resources>/config/provider/`，而 app-server 只按**自己的脚本目录**推两个候选（`<script dir>/provider/…` 与 `<script dir>/../../../../../config/provider/…`，本机规约成 `/config/provider/zcode-builtin.json`），两个都够不到 ⇒ **在任何协议之前**退出 1。**文件在盘上存在**，不是宿主缺件。修复：仅在宿主自己的候选都缺失时，把新位置作为 `ZCODE_BUILTIN_PROVIDER_CONFIG_FILE`（宿主为此定义的入口）交给子进程；不写 app 包、不写 `~/.zcode`；目录**不进** `files` 证明（宿主自行刷新它，钉住字节会把正常刷新误判成 `HOST_CHANGED`）。验证：基线退出 1 且零协议；命名后存活并发出 5 条 `startup/storageState`。点灵敏度：回退实现 → 4 个新用例中 3 个失败（2 error + 1 failure）。`go vet` 0；Python 全套 **199 OK**（195 + 4） |
| `ZCODE-APP-SERVER-PROTOCOL-SURFACE-CHANGED` | **ZCode（宿主协议变更，已适配 `8a06fc1`，属 G2c）**：同一构建**移除了 `workspace/readState`**（3.12.3 的 `zcode.cjs` 中出现 0 次；实调返回 `-32601`），而它曾是本传输的**首次调用**。适配：不再调用它，快照直接取 `session/resume`/`session/create`；同时**改为校验**快照一直携带的 `ZCode Protocol v1` 握手（此前只是假设），选择只经 `session/setModel`（`persistAsWorkspaceLastUsed:false`）施加并**从宿主自报回读确认**，传输不再替宿主拼装 provider 运行配置。**已完成的部分**：会话建立、握手、读快照都通过；`./scripts/harness doctor --provider zcode` 的失败点由 `ZCODE_NATIVE workspace/readState: … Method not found` 前移为 `ZCODE_CONFIG: native selected model missing`。**已排除**："我们的 fallback 答复清空了目录"（改答空 result → `model=null`，同样无 `current`）；**已判定**："我们只命名了 built-in 一个 env var 导致注册表为空"不成立——宿主自己的 `resolveNodeProviderRuntimePaths` 要求两个 provider 配置文件成对提供，三态探针显示三种情况下注册表**都为空**。**剩余**的是宿主 provider 注册表为空（宿主账号/凭据状态，传输按设计不写），见下一条。**影响面：`doctor-zcode` 是每个单元都要过的必需项，故在此事解决前任何单元都不能收口** |
| `ZCODE-APP-SERVER-PROVIDER-REGISTRY-EMPTY` | **ZCode（宿主账号状态，待定）**：`session/create` 返回 `settings.model={"available": []}` 且无 `current`，`session/setModel` 对宿主自己配置的 `builtin:bigmodel-coding-plan/GLM-5.3` 报 `-32603`。宿主实际读取的 personal provider config（`~/.zcode/v2/provider_config.json`）规则为空数组；用户可见的 provider 定义在 `~/.zcode/v2/config.json` 与 `~/.zcode/cli/config.json`（后者含 `builtin:bigmodel` 的 `GLM-5.3`/`GLM-5.3-Flash`），但 `builtin:bigmodel.options.apiKey` 为空串。填充它等于写宿主账号/凭据，与"适配器只命名宿主自有、由宿主自行维护的文件，不创建也不修改"的既定边界冲突。**已判定（不再是"待用户裁定"）**：注册表为空是**宿主当前状态**，不是我们的命名或协议假设造成的（见 `DIAGNOSIS.md` 第 6 节）。本轮复核用 `current-snapshot.py` 直接读了宿主自己的 `session/create` 应答：两种启动契约（只命名 built-in＝本适配器现状；built-in＋personal＝桌面端契约）下，宿主都报 `settings.model.current: null`、`available: 0`，且 stderr 为空（宿主健康）。本机三份目录（包内 seed 与两份 vendored active）**同为 revision 28、形状相同**：20 `templateRules` ＋ 8 条全为 `account:*` 的 `providerRules`，**零条 `builtin:*`** ⇒ 换目录不可能引入可选 provider。按 §12.5：被宿主状态阻塞的必需项 ⇒ M3 返修候选保持未验收，且**不再叠加改动**（实测契约等价）。用户侧唯一可能改变它的动作是让宿主重新具备可用 provider，**该动作在本机未验证**，不当作已确认的解锁方式。证据：`.harness/validation/zcode-protocol-3.12.3-probe/`（`DIAGNOSIS.md` 第 6–7 节、`selection-path.py`、`launch-contract.py`、`positive-control.py`、`positive-control-legacy.py`、`argv-stdio.py`、`current-snapshot.py`） | **【2026-09-18 更正】此判定已撤回，见 `DIAGNOSIS.md` 第 8 节**：桌面端同账号下宿主机自报 `providerCount: 2`，我方无人值守启动恒为 0；补齐桌面端可执行文件、`--surface desktop` 与 `ZCODE_SERVICE_AUTHORITY_MODE`（三值）后仍为 0 ⇒ 缺的是**启动时递入的运行时上下文**，不是宿主账号状态，也不是用户配置问题（用户桌面端能正常对话）。探针：`desktop-contract.py`、`authority-mode.py`。 **【根因已定 2026-09-18】不是本机没 provider，也不是用户没配**：ZCode 桌面端宿主进程在运行时把 provider 配置**投递**给 agent（日志 `zcode-agent-service`：`account provider config 已交付到 ZCode agent {"providerCount":8,"reason":"startup_ready:model_selection_changed"}`；`provider/model 就绪后已启动等待中的 ZCode agent {"providerCount":1}`），并订阅 `onDynamicWorkspaceProviderRuntimeHeadersRequest` 供 agent **反向索取授权头**。⇒ agent 自身**没有**注册表，**无人值守的裸启动缺少投递方**；本传输对 `interaction/requestProviderRuntimeHeaders` 按设计直接拒绝（`ZCODE_INTERACTION_REQUIRED`），即不实现该反向通道。详见 `DIAGNOSIS.md` **第 8.4 节**。结论：`doctor-zcode` **无法靠本机配置解决**，需方案层面裁定（桌面端在场并提供投递通道＝§12.4 那类能力，或对原生执行不在范围的单元重新定性）。
| `CANCEL-WINDOW-REFERENCE-INHERITED` | **G2a（有意保留的继承行为）**：`process.json` 写在 SIGTERM handler 装载**之前**（Go 与 `reference_job.py` 第 31/38 行同序），落在该窗口的组信号会让监督器**无收据**地以 `signal: terminated` 结束。两侧一致，故**不关闭**——在窗口里补写收据会让 Go 在参考实现根本没有收据处给出收据，而 `Store.job` 对缺失收据的处置是 `RuntimeBlocked(UNKNOWN_JOB)`，可观察地不同。实测 6 轮中两侧均未落进窗口（0/6），但窗口客观存在；G2a 的 D3 即由此而来 |
| `CANCEL-POSTCHILD-WINDOW` | **G2a（已知有界差异，未构造用例）**：参考实现的取消 handler 以 `raise KeyboardInterrupt` 实现，会在**任意**执行点抛出；若信号落在外层 `try` 结束之后、写 `exit.json` 之前，参考实现带 traceback 退出且**不写收据**，而 Go 在等待循环返回后不再理会已缓冲的信号、会**正常写收据**。窗口极窄（子进程收割与收据落盘之间），Go 侧更稳定；按"实测差异 + 不可达论证"记录，未加机制 |
| `SUPERVISE-NATIVE-FORM-DIGEST` | **G2a（测试约定，非差异）**：原生入口形态下，两侧 `argv` 里嵌的是**各自**的作业目录，故 `request_digest` **合法地不同**；差分用例只断言**形状**相同（退出 1、`error=""`、写出 `native-error.json`、两份流证明齐备）。这与 M3 的 D1 同属"两侧不能共享目录"的约束 |
| `EMPTY-PATH-COMPONENT-ORACLE-BLIND` | **G2a（覆盖边界）**：`PATH` 空分量（表示当前目录）这一分支在 macOS 上**不被**执行语义 oracle 区分——见下条；它只由 Go 单元测试覆盖 |
| `EQUIVALENT-MUTANT-NOT-CAUGHT` | **G2a（负控制方法论）**：一次定向变异（把空 PATH 分量分支简化为 `filepath.Join(dir, name)`）**未被** oracle 检出，因为它实际是**等价变异**：`filepath.Join("", "tool")` 得到 `"tool"`，而 `execve` 对不含分隔符的相对路径按 cwd 解析（PATH 搜索是 `execvp` 的行为，不是 `execve` 的）。记录它是为了区分"oracle 失效"与"变异等价"——两者都表现为"没被检出"，但处置完全不同 |
| `NATIVE-GATE-BLOCKED-BY-MODEL-ESCALATION` | **G2a（已于 2026-09-17 撤回并改名）**：原写作"触发者是模型自己的提权请求"。**该归因不成立**，见 `SANDBOX-UNAVAILABLE-IN-OUTER-CONTEXT` |
| `SANDBOX-UNAVAILABLE-IN-OUTER-CONTEXT` | **G2a（本单元真实阻塞项，成因已定位）**：只读沙箱在**外层运行上下文**里无法 apply。裸探针（无 harness、无 Python、无 Go 监督器参与）复现：`/usr/bin/sandbox-exec -p '(version 1) (allow default)' /bin/echo OK` → `sandbox-exec: sandbox_apply: Operation not permitted`，退出 71；而该二进制存在且可执行。09-15 同一台机器上只读沙箱是正常工作的（进程能起、写入被拒），故变的是**启动上下文**。本次阻塞**可复现**：19:59 同一配置重跑停在同一位置、理由逐字相同。**保持未验收**；解阻条件是在 `sandbox_apply` 可用的上下文里重跑 |
| `NATIVE-ANSWER-PATH-ABSENT` | **G2a（既定的能力缺口，非本单元回归）**：本版本**不能**回答宿主的原生权限请求；既有工作的记录是 `permission_bridge: BLOCKED`、`completion: PARTIAL … live permission forwarding not implemented`、`model_calls: 0`。方案 §12.4/§13.1 把该项单列为 **G2d：原生交互**。与 `ZCODE-HEADLESS-BLOCK` 同型 |
| `NATIVE-TRANSPORT-CARRIED-BUT-NO-COMPLETION` | **G2a（后半的精确状态）**：Go 监督器**确实**承载了原生传输（原生入口 argv 被当普通子进程执行、`exit.json` 六字段齐备、流证明正确、监督器是进程组组长），但**没有完成关联**——无角色抵达完成的轮次。§13.1 第 513 行要求的最小证据含"完成关联"，故该半为**受阻**而非通过 |
| `MODEL-ESCALATION-JUSTIFICATION-UNSUPPORTED` | **G2a（此条已于 2026-09-17 撤回）**：原称模型为提权给出的理由"与证据不符"。**不成立**：本次运行自己的工具结果里就有 `SandboxUnavailableError` / `SANDBOX_UNAVAILABLE` 与 `sandbox-exec: sandbox_apply: Operation not permitted`，模型是按工具自己打印的提示请求了最小放宽。替换为 `SANDBOX-UNAVAILABLE-IN-OUTER-CONTEXT` |
| `G2B-MISLABELLED-AS-NATIVE-GATE` | **G2a（文档，已修）**：G2a 的记录与本索引都把"验证监督器承载原生传输"写成了"下一单元 G2b 的关口"，而方案 §13.1 定义 **G2b 为 DSH 薄适配**、原生传输是 **G2a 自己的后半**。该错误让一个**属于本单元**的必需关口被顺延到了下一个单元，是"记录写错会改变流程"的实例。已连同记录一并更正 |
| `INDEX-MISSING-M2-ROW` | **M3（文档，已修）**：M2 的记录提交改了索引的两张表，却漏掉"基线与验收链"表的 M2 行；已在 M3 索引更新中补上。它说明"索引已更新"这句自述不足以证明更新完整 |
| `M3-CONTRACT-AMENDED-AFTER-DISPATCH` | **过程项（两条合同、同一情形、两种处置）**：`go-engine-M3-contract.md` 是随候选提交 `d71113e` 一起落地的**历史件**，却在撤回提交 `9e4a622` 里被**追加了 10 行更正**；而 G2a 的同类错误处理是**刻意不改**（见文首命名更正）。两条合同的差别带来一个可观察后果：M3 的 `bookkeeping` 只声明了 `M3.json`/`M3.md`/索引，**不含该合同**，所以若 M3 记录某天结论为 `PASS`，`admission.py` 会以"候选已移动"拒绝，而理由会指向一处**为记录而做的文档更正**。今天它被更靠前的 `overall == FAIL` 拒绝掩盖了。建议：要么统一为"合同一律不改、更正只进索引与记录"，要么把此类更正显式写进 `bookkeeping` |
| `WORKTREE-LOCATION-AMBIGUOUS` | **过程项**：本项目以独立 worktree 开发，绑定 `codex/go-engine-v1` 的目录才是当前工作区；按会话默认目录侦察会误判为"工作丢失"。以 `git worktree list` 为准 |
| `ENV-THROUGHPUT-UNIDENTIFIED` | 独立环境整体吞吐约为本机 1/4，成因未查明；回归判定已改为不依赖吞吐 |
| `BUILD-PATH-IN-RUNTIME-IDENTITY` | `go build` 不加 `-trimpath` 时运行时身份随构建路径变化 |
| `NODE-PATH-AMBIGUOUS` | 两份 node 摘要不同，跨 shell 恢复可能触发 `HOST_CHANGED` |
| `CANONICAL-CALL-CROSSES-PROCESS` / `PROOF-CALL-CROSSES-PROCESS` | 细粒度原语跨进程约 6 ms/次，已测量 |
| `PYTHON-SUITE-FLAKY-UNREPRODUCED` | 上一轮评审看到 1 FAIL + 1 ERROR，本机未复现；该启动链上的 0.53s CLI 导入已移除，其后 6 次全套（C4 期间）全部通过、0 复现 |
| `ZCODE-HEADLESS-BLOCK` | ZCode 无头在原生 Edit 审批处按合同返回 `ZCODE_INTERACTION_REQUIRED` |
| `GOFMT-WINDOWS-FILE-PREEXISTING` | **既有，非本迁移**：`internal/engine/definition/replace_file_windows.go` 不满足 `gofmt`。已在 M2 基线提交 `dfa23e7` 上复现（将该版本取出后 `gofmt -l` 仍报它），故与 G1a/G1b 无关；各单元合同的 gofmt 要求只覆盖 `internal/harness/`，该目录**干净**。留作独立修复项 |

## 评审时值得重点看的点

1. **C4 的边界划得对不对**：哪些属证据校验（已入 Go）、哪些属存储层（路径判断与文件读取留在 Python），以及"有序证明列表"是否为最小充分的表达方式。
2. **两处不可达差异的记录方式**：`NON-FINITE-CROSSES-TRANSPORT` 与 `INPUT-PATH-NOT-NORMALISED` 都以"实测差异 + 不可达论证"记录，而非加机制。是否认可这种处置，或应改为结构性消除。
3. **跨进程原语的取舍**：`file_proof` 从 0.036 ms 变为 6.1 ms；C4 未新增高频路径，但校验仍逐文件跨进程。是否应在此阶段就引入批量操作。
4. **类型边界上的静默收窄为何反复出现**：C1 的 NaN 预扫描、C3 的 float64 收窄、C4 的整数截断与 `7.0` 误拒、M2 的 `role` 改写与 revision 字符串化、G2a 的 PATH 走查以 `os.Stat` 存在性替代内核可执行性判定——**七处，同一个模式**：Go 侧把"参考实现的动态类型/动态语义"实现成了更窄的静态形状。C4 起补的"经桥接的端到端等价用例 + 负控制"在 M2 确实生效（两处由我在外部评审前查出），G2a 的三处也都由独立 oracle 或完整验收查出，但模式仍在复发，说明**逐例修补不够**。是否需要一条结构性纪律（例如：凡从 JSON 取出的值，禁止在 Go 侧做类型断言式的默认值替换，只能按参考实现的取值语义原样透传；凡"某候选能否运行"这类判定，禁止用预筛替代内核的最终判定），是本阶段最值得质疑的一点。
5. **G1b 的拆分与各项边界**：M1 把"解析+表示规则"入 Go、把"git 调用与文件系统"留在 Python；M2 把两个包装形状入 Go、把 Store 访问与 `BOUNDARY` 文案留在 Python；M3 把内容寻址与两条判定入 Go、把工件路径布局与原子写留在 Python。这三个切法是否合理；`COPY-STATUS-UNREACHABLE` 该按"已由单元测试与独立裁判覆盖"接受，还是应改为结构性消除（例如断言选项集不可能产生 `C`，或让 Go 拒绝未知状态）。
6. **`BOUNDARY` 留在 Python 是否可接受**：它是否算"反馈包装"的一部分（§9.4 的措辞），还是纯提示词文案。若应入 Go，范围是整段提示词而非其中一句。
7. **M3 的"两次调用"是否为正确取舍**：`get` 拆成 `artifact-reference` + `artifact-binding`，使"格式优先于读取失败"成为结构性质，代价是每次读 2 次跨进程（约 11.6 ms，`graph.py` 内单次 run 数十次调用）。被否的两案是"合成一次调用、靠代码顺序保序"与"让 Go 读文件并复现异常语义"。若评审认为该代价不可接受，需要指认一个不会为异常语义写例外的替代。
8. **工件路径为何不入 Go**：`artifact-plan` 返回引用与规范字节、不返回 `artifacts/<ref>.json`。依据是 §7 只规定"正文在工件目录中"、未规定命名。是否应把该命名视为寻址格式的一部分而搬入 Go（即 `ARTIFACT-LOCATION-STAYS-IN-PYTHON` 是否应升级为阻塞项）。
9. **`test_boundary_recovery::test_artifact_integrity` 的前提变更是否可接受**：该用例原先断言"工件读写不需要桥接"，M3 后改用真实 bridge 而断言逐字保留。这是有意的前提变更（合同已登记），但它是**唯一一处**为迁移而改动既有测试前提的地方，值得单独审视。
10. **D1 的处置是否恰当**：交付的差分测试令两侧共用同一工件目录（写路径未被按值差分、两棵树未被互比）。我按缺陷处理并重写，但也可以论证它"没有造成可观察行为偏差"故应算测试质量问题。此外，修复的第二次派发返回空、由我自行实施——是否需要在流程上要求"派发失败必须显式回报"。
11. **G2a 为什么走内部窄命令而不是桥接操作**：理由是"搬动的对象就是进程所有权，与请求/响应的桥接前提冲突"。§12.1 的两条路径因此在本项目里都被使用了，这意味着**"新单元走哪条路径"本身成为一项需要论证的设计决定**。判据是否应写成明文规则（例如：凡需要跨调用存活的状态、或需要接收信号者，走内部窄命令），还是继续逐单元论证。
12. **D3 的处置：修用例、不关闭窗口，是否可接受**：关闭窗口（把 handler 提前到写 `process.json` 之前）会让 Go 在参考实现**没有收据**的地方产出收据，因此我选择保持忠实并改用例。但也可以论证"在该窗口多写一份收据"是对调用方更有利的行为、应作为**有意的行为改进**单独提出（那将是一次显式的语义变更，而非迁移）。若认可后者，它应作为独立单元而不是塞进 G2a。
13. **`childSignalGrace = 250 ms` 这个有界宽限是否该存在**：它是 D2 的修复手段——子进程被信号打死时，先给信号通道 250 ms 确认组信号是否**同样**到达监督器，以区分"取消"与"子进程自己死于 SIGTERM"。它引入了一个时间参数。替代方案是让监督器在信号到达时**无条件**优先报告取消（不设宽限），代价是失去"子进程自杀 vs 被取消"的区分能力。是否应改用不依赖时间常数的结构性做法，值得评审。
14. **"只在完整验收的负载下才复现"是否应升级为流程纪律**：D3 单独跑该包 5 次全过，只在 `go test ./...` 下失败。这提示"单包通过"不足以作为放行依据。是否需要把"竞态类缺陷必须由完整验收复现一次"写成明文要求（本次是碰巧撞上的，不是设计出来的）。
15. **G2a 是否应读作两个验收单元**：这是本次把 G2a 判为"未收口"的核心分歧。本索引按 §13.1 的正文（"G2a **先验命令监督，再验其承载原生传输**"）与第 513 行给"原生定向验收"规定的最小证据（含"**完成关联**"）判定：后半受阻 ⇒ 本单元保持未验收。**另一种读法**是 §13.1 把 G2a 列为**一行**、表内出口条件只有命令作业的三条属性，故后半只由合同第 178 行的结构性证据（原生入口 argv 逐字跑通 + 监督器不读 `kind`）支撑即可，G2a 可收口。若评审采后一读法，需要明确裁定，并说明该裁定与第 513/520 行"涉及进程行为的单元必须增加对应真实调用"如何并存。
16. **模型的提权请求是否应在适配层就转成"能力不足"**：当前实现把 `sandbox_permissions: danger-full-access` 当一次普通原生请求送达上层，由上层报 `DSH_INTERACTION_REQUIRED`（已建立会话、已消耗模型调用）。另一案是在适配层识别该字段并**直接**报能力不足，不建立会话、不消耗调用。两者可观察行为不同，费用也不同。是否要引入这条路径，还是保持"如实转述宿主请求"。
17. **"模型自述的原因"该占什么证据地位（本条曾写反，已更正）**：原问题以"模型的理由与证据不符"为前设。**该前设不成立**——G2a 那次运行**自己的**工具结果里就写着 `sandbox-exec: sandbox_apply: Operation not permitted` 与 `{"code":"SANDBOX_UNAVAILABLE"}`，模型是在 `bash` 完全无法执行后、按该结果自己打印的提示（"otherwise switch the consumer to danger-full-access"）请求了最小放宽。真正的教训要反过来写：**当模型给出一个可核验的阻塞原因时，先去找运行自己的工具结果，不要用"另一次运行里同类命令没提权"去反驳它**——那是拿**另一天**的上下文给**这次**的上下文下结论。同时保留原问题的另一半：模型的自述**也不能**替代独立探针（本次的正解是那条不含 harness 的裸 `sandbox-exec` 探针）。是否应把这条写成原生验收的明文纪律：**任何"宿主不可用"的结论都必须由独立探针复现，且必须引用本次运行自己的工具结果**。
18. **M3 返修后的载荷形状是否可接受**：`artifact-binding` 由"传字节"改为"传路径"，代价是**读侧自己按路径取文件**、且命名 `artifacts/<ref>.json` 因此成为调用方可见约定（原"存储布局属 Python"的读法需重新论证）。替代案是"把 8 MiB 天花板按操作分开"，但那会引入**两个容量**，而本 P1 的教训正是"两端不共享同一容量"。
19. **是否应要求任何随输入增长的操作显式声明上限**：本次缺陷的成因是"某个操作的请求体随输入增长，而另一侧不知道"。当前的固定共用天花板对此没有表达力。是否应把它升级为"每个操作声明自己的上限，且发布侧与读取侧必须用同一份声明"这类结构性纪律——这才可能让同类不对称**不可能**出现，而不是靠用例逐个去撞。
20. **`checks` 侧运行期错误是否应中止而不是转为决策中断**：当前语义下，check 出错会转成一次用户决策中断，图返回时末态**没有 `terminal`**，测试因此以 `KeyError` 而非断言失败表现。这是既定中断语义的产物，但"图正常返回"与"图被中断"在测试里只能靠 `KeyError` 区分。是否需要一个显式的"终态缺失"分类。
21. **失败时保留子进程作业目录是否应写成明文纪律**：`M3-JOB-DIRECTORY-LOST-ON-FAILURE` 是本次"触发点无法归因"的直接原因——上一项被定级为"未解释"而不是"已解释"，几乎完全由此造成。是否要求所有 check/gate 作业目录在失败时先移动到证据目录再清理。
22. **"本机沙箱是否有可用后端"这句话的作用域**：同一个短语在本项目里至少指三个**不同**的上下文，互相不能引用——(a) **远端 DSH 会话**按 harness 的只读约束起的沙箱：09-15 可用（进程能起、写入被拒），09-17 两次运行的**外层上下文**不可用（`sandbox_apply: Operation not permitted`）；(b) **本代理自身的 shell**：它已处在宿主的代理沙箱内，在其中**再叠加** `sandbox-exec` 以 `Operation not permitted`（退出 71）失败——这是本条的观察，与 (a) 无关；(c) **工具沙箱被显式放开后**的同一会话：本会话未能真正脱离沙箱，重试结果逐字相同。三者不可互相引用，而此前的记录正是把 (a) 的另一次运行与本次运行混为一谈（见已撤回的 `MODEL-ESCALATION-JUSTIFICATION-UNSUPPORTED`）。是否应把"作用域必须写明"写成原生验收的记录要求。（本项不改变 G2a 的处置：任何情况下 harness 都不得自行授予提权。）

23. **ZCode 协议适配该切成哪一个单元、以及 `doctor-zcode` 是否应当拦住纯计算单元**：3.12.3 移除了 `workspace/readState` 并改掉了 `session/create` 的模型语义，这是**宿主协议面变更**。按 §12.2 它不该和当前单元混改；按 §13.1 它落在 **G2c（ZCode 薄适配）**——但 G2c 的前置是"G2a 收口"，而 G2a 本身卡在同一条链上。那么在这个适配完成前，是（i）把它作为一个**独立的前置单元**插到 G1b/G2a 之前（宿主接入是所有单元的公共前置），还是（ii）按 §12.5"原生定向验收按影响确定"论证：C1–C4、M1–M3 属纯计算/本地子进程，**本不需要**原生 ZCode 验收，故验收器固定要求的 `doctor-zcode` 对它们是**超出方案的更强要求**？两种读法给不同结论，需要明确裁定。**注意**（ii）不能读成"改判据放行"：`doctor-zcode` 此前一直通过，改标不适用会把"宿主不可用"写成"不需要测"；若采（ii），应改的是**每个单元的必需项清单**（并要求把 ZCode 就绪单独作为一条前置验收），不是把失败改标。

    **2026-09-18 更新**：按用户指示，适配已**代做完成**（`8a06fc1`，G2c 范围），因此"(i) 还是 (ii)"不再是"要不要现在就适配"的问题。但本问题**未被这次适配解答**，反而被更清楚地暴露：适配完成后 `doctor-zcode` 仍不通过，而失败原因**既不是我们的协议假设、也不是我们的命名**，而是**宿主 app-server 自身的 provider 注册表为空**（见 `ZCODE-APP-SERVER-PROVIDER-REGISTRY-EMPTY`）。由于 `doctor-zcode` 在本机此前一直通过，这说明**注册表为空是该宿主当前的状态**（3.12.3 改读 v2 personal provider config，而该文件规则为空），不是"ZCode 天生不能用于验收"。故 (i)/(ii) 仍需裁定，但裁定的对象已变成：**"宿主的账号配置就绪"是否应当作为每个单元的前置条件**，而不是"我们该不该适配协议"。
24. **启动时给子进程命名宿主自己的内置目录，是否算 §5.3 的"改变宿主行为"**：本轮把 `<Resources>/config/provider/zcode-builtin.json` 通过宿主自己定义的 `ZCODE_BUILTIN_PROVIDER_CONFIG_FILE` 传给子进程。论据是：它是宿主为此**专门定义**的入口，宿主随后仍自行计算并维护 active 目录（我们把值放在 bundled seed 位），只覆盖"宿主自己的查找失败"这一种情形，且不写任何文件。反方读法可主张"适配器不得用环境变量参与宿主配置解析"。是否认可，或应改为在适配层报"宿主不兼容"而不提供该路径（那会让 ZCode 完全不可用）。

25. **一个 provider 的接入何时算"搬完"（用户 2026-09-18 提出，本条据其意见改写）**：用户指出"现在做的不就是用 Go 重写 LangGraph 引擎吗？那最后肯定都是要收到 Go 的"，并质疑这不是规不规则的问题。**该指正成立，本条的定性随之改写**：这是**迁移出口判据**问题，不是需要另立一条跨语言纪律。方向在方案里早已写明——§1（第 11 行）"最终交付一个 Go 产品二进制……控制链**不加载 Python、LangGraph 或 LangChain**"；§2.1（第 69–78 行）把 `internal/native/{contract,registry,result,interaction,transport}.go` 与 `adapter/{dsh.go,zcode.go}` 全放 Go，`zcode.go` 的职责就写着"ZCode 协议与配置映射"；§13.4（第 615 行）把独立性判据写成"控制进程树不依赖 Python/LangGraph"。⇒ **目标态＝一个 provider、一种语言、一个文件**；今天的 Go（安装/hooks：`internal/host`＋`internal/lifecycle`＋`internal/validate`）与 Python（协议/传输：`native_zcode.py`＋`native_transport.py`）是**同一迁移的两个时间点，不是分工**。**真正的缺口是出口判据**：§13.1 给 G2c 的验收出口只写"冷恢复、原生模式保留、工具继承和原生完成证据通过，无隐式供应商或模式切换"，**没有一项要求"该 provider 的 Python 路径退役、不留双活实现"**——缺了它，混合窗口可以在"通过验收"的同时永久化。**约束依据已经存在**（故只需落到出口，不必新立规则）：§12.2（第 478 行）"直接复用，不另写第二份"；§12.3（第 482 行）"**同一 run、同一职责只启用一个实际实现**"——Go 适配器一旦可用，Python 路径就不能再是活动实现。**建议**：在 §13.1 的 G2b/G2c 验收出口各补一项"该 host 的 Python 执行路径退役，只余（或不余）§12.1 的包装职责"。**不属于遗留、不应被误读成"没收到 Go"的部分**：开发期自狗粮与行为对照用的 LangGraph harness（§1 第 5 行"不进入独立版的产品执行链"）与仍服务历史 run 的冻结版本（§13.4 第 615 行"不是立刻删除所有仍服务历史 run 的冻结版本"）——它们在**引擎执行链之外**。**本条与问题 23 同链**：让 ZCode 变"全 Go"的正是 G2b→G2c，前置是 G2a 收口，而 G2a 卡在被宿主状态阻塞的 `doctor-zcode` 上。

本索引只汇总各单元记录中已观测的结果；不含密钥或认证材料。
