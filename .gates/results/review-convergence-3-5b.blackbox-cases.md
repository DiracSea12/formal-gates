# Blackbox QA cases (run: review-convergence-3-5b)

Derived from the run state at qa-design record time. Review these cases against the current confirmed requirement.

CASE-001
mode: blackbox
description: 缺少阻塞证据、引用悬空或拿审查者原文冒充主代理核实时，P0/P1 门结果只能成为可重试的无效回执。
procedure: 【全套公共准备协议】

本协议随本用例集保存。各用例仅使用自身专用步骤选定的准备分支、配方与材料；引用本协议不表示执行CASE-001测试。

公共写入分支：仅在QA Execution运行。从本次规范派发取候选C、产品快照S和基线B，真实git -C C rev-parse HEAD核对S。整组复用执行宿主按正常安装建立的S窗口，写workflow用已登记stable launcher BIN=/Users/suxinyang/.local/bin/formal-gates及实际安装包I；只读命令可用从S构建的产物。Q=$(mktemp -d)，本条每个独立变体各建R=Q/repos/<case>-<variant>、RUN=qa-<case>-<variant>、E=Q/evidence/<case>-<variant>，E不进Git。git init R，真实提交需求/fixture得BR/SR；这两个fixture快照不替代S。除专测start的分支，可先用一次workflow start --root R --package-root I --run-id RUN --flow formal --vcs git --split no --requirement requirement.md取得该R合法admission外壳，再直接手工装配下述目标前置，不重跑前序正式流程。只编辑此临时fixture，保留外壳原有admission字段；按候选实际序列化格式及底层编码/保存规则重算完整性/CAS，不能预运行待测业务判断填答案。用真实workflow show逐项确认准备状态可读且满足下列配方，保留构造清单及输入；准备封存后不手改状态修复结果。所有workflow命令默认带--root R --package-root I --run-id RUN（show仅root/run-id），argv/JSON按候选公开--help与formal-flow模板的真实字段填写。记录即时前后show、argv、输入、退出码、stdout/stderr；只有零写入判据才cmp即时state字节。独立负向各首attempt，未知参数/身份/快照/前置/额度错误不算目标校验通过。

模拟输入与来源：需求确认、审查status/items/findings、历史PASS、授权和所需身份/生命周期均可直接构造，标记TEST-CONSTRUCTED:<本CASE>:<variant>。每个角色给不同测试身份，绑定同R/RUN/dispatch/requirement revision/BR/SR；需派发prompt本身作证据时真实prepare，只将更早状态直接装配。需要start/stop前置时手工生成该provider正常格式的配对观测及模拟transcript（cwd=R、session_id=fixture-owner、agent_id=该测试身份、dispatch=本次D），或经公开lifecycle capture提交同形模拟host输入；不启动原生代理、不查真实身份、不等待指定等级。授权输入精确绑定该fixture实际revision/digest/scope，缺授权分支就不提供，不向真人确认虚拟项目。这些材料只代表隔离前置，真实被测公共调用和观察必须另行留存；不以已有测试套件为黑盒主体。

公共前置配方（仅按各用例专用步骤选用）：
A0=唯一requirement.md及其真实revision已绑定，requirements-clarification PASS、requirementConfirmed=true，product-review PENDING；确认是TEST-CONSTRUCTED全文/精确revision事件。
A1=A0加无finding的product-review PASS，start-readiness PENDING。
A2=A1加start-readiness PASS、单Batch/no-split、custom路线；默认只选implementation-quality-gate（G），要两门则另选complexity-gate（G2）；mode按本条明确选择，未选QA不产生QA前置。
DEV=A2加已完成development-worker与真实Git交付SR，currentSnapshot=SR，工作树干净；只有专测中间态时保留本条指定未完成项。
WAVE=DEV与已选mode的合法冻结前置上的COLLECTING wave，精确targets为本条所选QA execution/G/G2；派发、attempt、状态按本条列举。专测初始化时不预建wave。
QA(mode)=该mode的Q1/Q2/Q3分别覆盖fixture AC-001正常输出、AC-002缺文件、AC-003无关文档，步骤/判据为python3 demo.py emit输出alpha\n、python3 demo.py missing不存在文件时非零且stdout空、README含固定字符串Demo。先前逐source/point/case批准、集合PASS、FROZEN/version、执行/carry仅按本条所需前置装配；被测review/冻结本身不提前填它的结果。白盒引用只指本次fixture新写的独立测试文件/各不相同函数。
CLOSED=WAVE全部必需target已结束，无未声明在途/错误；finding是否核实裁决、problem的OPEN/RESOLVED依本条配方，不自动补齐待测缺项。
FINAL=所选必需action/gate/QA PASS，P2已有完成/carry、无阻塞/错误/返修；route要求FULL时已有最终SR的FULL前置，真实SR干净且单条交付commit。专测某Seal阻断时只缺该项。

默认小fixture：仅在本条没有专门正文/文件时使用。唯一requirement.md按项目标准“目标/范围/方案/需求点/验收条件/来源/完成定义”写明单Batch的Demo：REQ-001要求emit精确输出alpha+LF，missing子命令访问不存在文件时非零且stdout为空，README包含Demo；AC-001/002/003分别验收上述三项，来源写TEST-CONSTRUCTED本条。demo.py使用Python标准库：参数emit时print("alpha")；参数missing时pathlib.Path(sys.argv[2]).read_text()；README正文为Demo。被测错误按本条明确的文件替换构造。需求文件真实预检取得revision；确认与保证映射是隔离数据，不请求真人。

固定阻塞载荷的需求与文件构造（仅本条明确选用时生效）：

默认 demo 需求没有隐含授权 totals.py 或 formatter.py。凡同时选用“默认小fixture、固定阻塞载荷”的分支，必须在创建 BR、登记唯一 requirement.md 和构造该 revision 的确认/审查前置之前，把下列两个需求点及其来源、完成条件并入同一完整 requirement.md；不能在 run 已绑定需求后只追加脚本、把未确认行为用作 P1 证据。保留默认 REQ-001/AC-001/002/003 的 demo 语义不变。目标/范围/方案同时明确本 fixture 交付 demo.py、totals.py、formatter.py 和两个 TSV 示例，均为 Python 3 标准库本地 CLI、同一个 Batch。以下 fixture 内 REQ/AC ID 只用于构造产品输入，不替代正式用例的 primary AC bindings：

### REQ-002：完整汇总正常 TSV 输入
要求：公开入口为 python3 totals.py <输入文件路径>。输入为可读 UTF-8 文件，1至100行，每行是非空 ASCII 标识、一个 TAB、0至999999的十进制整数；行间 LF，最后一行允许有或没有 LF。每个正常行恰好计入一次；成功退出0，stdout只有十进制总量和一个LF，stderr为空；不修改输入。缺文件时非零退出且stdout为空；其他格式不在本 fixture 范围。
验收条件：
- AC-004：精确输入 alpha<TAB>10000<LF>beta<TAB>2500，有或没有最终LF，均输出12500<LF>，exit0且stderr为空。
- AC-005：上述两种输入调用前后字节相同；不存在路径导致非零退出且stdout为空。
来源：TEST-CONSTRUCTED:<本CASE>:<variant>，固定阻塞载荷的正常使用验收。
完成条件：两种正常末尾形式和缺文件路径均满足上述要求。

### REQ-003：输出无标签的固定总量
要求：公开入口为 python3 formatter.py，无参数，输出供后续整数读取使用的固定十进制12500，后跟一个LF，退出0且stderr为空。
验收条件：
- AC-006：stdout精确为12500<LF>，不得附加total=、空格或其他文本。
来源：TEST-CONSTRUCTED:<本CASE>:<variant>，独立于TSV求和的输出格式验收。
完成条件：该公开调用满足精确字节判据。

若本条已给出自己的完整 canonical fixture（如CASE-019），以该条全文及其ID为准，不追加另一套需求或更改其ID。本公共载荷仅作用于明确选用它的默认 fixture。若其同时使用 QA(mode) 前置，除默认 Q1/Q2/Q3 外，还要装配独立 Q4/Q5/Q6/Q7/Q8：Q4执行python3 totals.py examples/with-lf.tsv并精确比对12500+LF/exit0/stderr空，Q5对examples/no-final-lf.tsv作同一比对，二者主绑定fixture AC-004；Q6分别保存这两份输入的调用前字节、实际调用后cmp，判据两份均未变，主绑定AC-005；Q7调用python3 totals.py examples/not-present.tsv，判据非零且stdout空，主绑定AC-005；Q8调用python3 formatter.py并精确比对12500+LF/exit0/stderr空，主绑定AC-006。所有命令cwd=R。source/point/case和approved-whitelist前置要覆盖该 fixture 的全部实际需求，不能只保留demo三AC却声称扩展后的集合覆盖完整。历史批准/执行仍明确是TEST-CONSTRUCTED隔离输入，不声称错误载荷已真实通过；当前待测QA Review、冻结或实际执行的结果不提前填入。

totals.py完整内容如下（第6行是待验证的末段丢弃）：
import pathlib
import sys

text = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
total = 0
for line in text.split("\n")[:-1]:
    sku, quantity = line.split("\t")
    total += int(quantity)
print(total)

formatter.py完整内容：
print("total=12500")

examples/with-lf.tsv精确UTF-8字节为"alpha\t10000\nbeta\t2500\n"；examples/no-final-lf.tsv为"alpha\t10000\nbeta\t2500"，末尾无LF。SR真实包含这些脚本和示例。需要阻塞复现的分支以R为cwd实际运行python3 totals.py examples/with-lf.tsv、python3 totals.py examples/no-final-lf.tsv、python3 formatter.py，分别保存argv、exit、stdout/stderr、两份输入调用前后字节；实际输出应分别为12500\n、10000\n、total=12500\n，均exit0/stderr空。未运行或输出不符不能把上述预定值写成实际观察。

F1精确引用本fixture已确认REQ-002/AC-004、totals.py:6及examples/no-final-lf.tsv:2，其正常路径为python3 totals.py examples/no-final-lf.tsv；F2引用已确认REQ-003/AC-006及formatter.py:1，其正常路径为python3 formatter.py。正常数量汇总应12500而实际成功返回10000，和整数接口实际输出total=12500，是两个独立的核心验收后果；每项分别说明为何P2不足，不共用或错绑另一项需求。完整开发后记录每条独立包含finding/severity/rationale/requirement/normal-path/actual-error/location/deferral-impact和verify-scope/verify-severity/verify-causality/verify-reproduction，参数紧随所属--finding。负向变体仅删除本用例指定目标字段或改为指定无效引用，其他字段保持绑定本fixture真实需求与SR。

开发前分支使用本条明确的需求/方案矛盾，不把上述故意错误的实现作为开发前actual-error；技术审补公开category/verify-classification。P0专用分支须另构造只有一项核心验收且无任何可用路径的完整需求冲突并给出阈值理由，不把TSV P1改名为P0。

公共只读分支：仅在QA Execution执行。由规范派发取得C/S/B并核对git -C C rev-parse HEAD=S；Q=$(mktemp -d)，从C以go build -o "$Q/formal-gates" ./cmd/formal-gates构建BIN，整组可复用同一候选产物；输入与输出只放本条临时E。schema/枚举依候选公开guard-preview文档模板填写，所有身份声明为TEST-CONSTRUCTED，准备输入不调用待测计算生成答案。无需正式run、审查代理或真人确认；命令真实运行并记录argv/输入/退出码/stdout/stderr，不能以既有go test替代。

【全套公共准备协议结束】

【CASE-001专用步骤】

准备选用：默认小fixture、固定阻塞载荷。

准备：为每个缺项/无效引用变体分别装配 DEV+WAVE，只有 G，G 有绑定 SR 的未记录 CLAIMED dispatch、模拟 start/stop，首 attempt。按公共协议的固定审查输入写完整 P0/P1 报告及独立核实，不等待真实 finding；仅实际运行相应小脚本取得作为字段基准的正常路径错误。P0/P1 各自展开：缺 requirement、normal-path、actual-error、location、deferral-impact、四个 verify 字段之一；location 分别为不存在文件、超出文件末行、不存在测试定位；另一个请求仅在 finding 文字里声称已核实而不传四个 verify 字段。操作：每个新 fixture 经 record-gate --gate G --dispatch D --compared BR..SR --status FAIL 提交一次目标请求，保留 rejected receipt；随后只实际 prepare-gate 一次，比较新旧 TaskKey/attempt。正向完整对照另建 fixture。没有真人报告等级或重试额度的随机前置。
oracle: 每个缺项或无效引用变体的记录均非零退出；没有有效 finding、gate FAIL、wave problem 或 guard basis，只新增绑定原 dispatch/attempt/TaskKey、列明缺失或无效字段的 `REJECTED_INVALID_RESULT` 回执。旧 attempt 已终结，新派发复用同一 TaskKey 并消耗有限 retry。 每个变体的拒绝必须定位该缺项/无效引用；仅命令非零或额度耗尽不构成通过。 完整字段对照退出0并保存完整finding及绑定，不出现rejected receipt。所有P1字段必须引用本fixture已确认的对应REQ/AC；未绑定的脚本行为、需求或完整QA映射缺失造成的拒绝不算目标证据校验通过。
primary AC bindings: AC-001, AC-004
review status: PASS

CASE-002
mode: blackbox
description: 证据和主代理核实完整的开发后 P0/P1 能被持久化并完整展示。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture、固定阻塞载荷。

准备：装配 DEV+WAVE，G 的派发已模拟认领和完成、尚未记录结果。在临时 TSV 工具上制造末行遗漏并实际调用，取得精确输出、输入字节和 totals.py:6；准备一条 TEST-CONSTRUCTED P1，填齐需求/正常路径/当前错误/位置/延后后果、该条 severity rationale 与四项独立核实，字段使用各不相同的可辨识文本。操作：record-gate --gate G --dispatch D --compared BR..SR --status FAIL 提交完整请求；workflow show 后把这一条存储结果逐字段与提交 JSON/argv 比较。只把真实 record/show 输出当成被测证据。
oracle: 记录成功；show 中保留原始 finding、全部结构化字段和位置、需求 revision、dispatch/result 绑定、severity rationale 与主代理核实记录，内容可逐字段机械比对。
primary AC bindings: AC-002
review status: PASS

CASE-003
mode: blackbox
description: P2/P3 不因缺少 P0/P1 专用阻塞证据而被拒绝或升级。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：分别装配两个 DEV+WAVE 首 attempt 的 G 派发，写入模拟认领/完成。E 中各准备一条 TEST-CONSTRUCTED P2、P3 报告及其等级理由，不填写 P0/P1 专用 actual-error、deferral-impact 和 operator reproduction 字段；其余协议绑定有效。操作：各执行 record-gate --gate G --dispatch D --compared BR..SR --status PASS，按各自等级提交 finding；读取 show 的等级、有效结果和 receipts。不运行前序审查、不把 P2/P3 是否出现交给模型。
oracle: 两次记录均按各自 P2/P3 语义成功，严重等级保持不变，没有 `REJECTED_INVALID_RESULT`，也没有自动升级为 P0/P1。
primary AC bindings: AC-003
review status: PASS

CASE-004
mode: blackbox
description: start-readiness 的分类缺失、非法阻塞分类及实现细节越级均被一致拒绝。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：各变体独立装配 A1，使 start-readiness 是首 attempt 的合法未记录派发，补齐模拟身份和结束事件。固定输入为 TEST-CONSTRUCTED finding，提供该阶段适用的需求/方案引用、逐条等级理由及核实。缺分类分支分别取 P0/P1/P2/P3；越级分支将字段、容器、辅助函数、两种等价实现、命名排版、文档精度、fixture 细节分别分类为 IMPLEMENTATION_DETAIL 并提交 P0/P1。操作：先实际 prepare-action --action start-readiness 留存其 prompt，再用 record-action 的 --category、--verify-classification、各 --verify-* 提交目标请求一次；缺分类分支只不传 category，越级分支只保留被测非法等级组合。检查目标拒绝后最多重派一次。不要求真实审查者制造非法输入。
oracle: prompt/result contract 对四种等级都要求分类及正常使用后果。所有缺分类结果和所有实现细节 P0/P1 均非零退出，只形成 rejected receipt；错误明确指出实现细节最高 P2，同一 TaskKey 可有限重派。 缺分类与实现细节越级须分别命中其目标校验；额度、来源或参数解析拒绝不能代替。
primary AC bindings: AC-005, AC-006, AC-008
review status: PASS

CASE-005
mode: blackbox
description: 三种允许分类只有具备需求、正常使用影响、不可延后理由和独立核实时才能阻塞开工。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：三个独立 A1 fixture 的 requirement.md/方案分别写入相互矛盾的核心输出要求、模块循环依赖导致核心数据无法到达、核心验收要求没有可调用入口；每个文档有真实可定位行。为各自 start-readiness 派发构造完成记录和对应 TEST-CONSTRUCTED P1 报告。操作：逐个 record-action --action start-readiness --status FAIL，分类分别为候选公开矩阵中的需求冲突、架构职责/依赖根本错误、验收入口缺失；填写该条正常路径后果、不可留到开发决定的理由、classification/severity 核实，省略开发后 actual-error。show 检查三类都能记录并阻塞开工。
oracle: 三类结果均可形成有效 P1 并阻塞 start-readiness；show 保留分类和核实证据，且不会要求或伪造开发前不存在的实现错误。
primary AC bindings: AC-007
review status: PASS

CASE-006
mode: blackbox
description: review-ready 快照只在全部已选 QA 用例集冻结后初始化固定 expected-set，并一次公开全部并行目标。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：装配 DEV，route 选 blackbox、G、G2；黑盒有合法用例和未完成 review，尚未 FROZEN，未创建 wave。真实 SR 已提交。操作：先调用 prepare-review-wave，以及相应 prepare-action --action qa-execution --mode blackbox 和 prepare-gate，保留拒绝/无新派发观察。另建同样输入但 QA(blackbox)=FROZEN 的 fixture，实际 prepare-review-wave；读 show 的 expected-set/digest 与全部 ready dispatch，应只有 blackbox execution、G、G2。分别在新独立同波前置通过公开 route/QA 配置变更入口尝试增门、删门或换 mode，检查拒绝或旧 wave 失效。未完成/已冻结两种前置直接构造，不为它们跑 QA 审查。
oracle: 冻结前不能初始化 wave。冻结后 expected-set 恰好包含所选 QA execution 和两个门，均可一次准备且不存在人为串行依赖；集合和 digest 固定，同 wave 修改目标被拒绝或明确使旧 wave 失效。
primary AC bindings: AC-009
review status: PASS

CASE-007
mode: blackbox
description: review-wave 有未解决目标时不能返修，错误处置不伪装 PASS，晚到结果不能重开已关闭 wave。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：独立装配各 WAVE，目标仅 G/G2。G 已 PASS，G2 分别为未准备、PENDING、CLAIMED、RUNTIME_ERROR、REJECTED_INVALID_RESULT、RETRY_EXHAUSTED；错误 receipt 与 attempt 计数直接按正常到达的终结状态构造。另备两个在途目标处置变体和一个错误终结/晚到报告变体。操作：逐一调用公开 prepare-action --action development-worker，保存缺失目标；对 PENDING/CLAIMED 调用正式 wave 错误处置入口，提交模拟用户非空理由。对真正错误目标用同一入口提交精确绑定的 TEST-CONSTRUCTED 用户处置，再以旧 dispatch 实际 record-gate 送入晚到 PASS。比较收集状态、处置理由和晚到审计；不真实消耗若干轮失败来建立耗尽前置。
oracle: 每次提前返修都失败并精确列出未解决目标；PENDING/在途不能被处置。错误被合法处置后目标终结但状态不是 PASS，理由可审计；晚到结果只留审计记录，不改变 closed wave 或自动重开。
primary AC bindings: AC-009
review status: PASS

CASE-008
mode: blackbox
description: 收齐结果后必须完成逐 finding 核实和问题裁决，才会生成包含整轮问题的唯一返修内容。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture、固定阻塞载荷。

准备：CLOSED wave 内直接装配四项已记录结果：Q1 实际执行会失败的 QA FAIL、G 的完整 P1、G2 的 P2，以及将 DISMISS 的额外 finding。每项 ID、原报告和来源均为独立 TEST-CONSTRUCTED；给 CLI 的更早 PASS 与生命周期为模拟前置。依次独立构造缺 scope、缺 severity、缺 reproduction、缺 NEW/SAME_AS/DISMISSED 的返修前状态。操作：各调用 prepare-action --action development-worker，检查针对缺核实/裁决的拒绝。完整对照以 adjudicate-wave 实际提交四条裁决：QA/P1/P2 归入各自有效 problem，额外项 DISMISSED；再实际调用 prepare-action --action development-worker 并读取 prompt，逐项数出有效问题恰好一次。
oracle: 任一核实或裁决缺失时返修被拒绝。补齐后只产生一份返修，包含 QA FAIL、有效 P1 和 P2 各一次，不包含 DISMISSED finding。
primary AC bindings: AC-010
review status: PASS

CASE-009
mode: blackbox
description: 同一快照只能创建一个逻辑返修义务，返修运行错误只能重试该义务。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）。选默认小fixture。分为两个独立R/RUN/E，测试唯一计划与错误后重试；两个分支之间不恢复或修改已经接受待测操作的state。

A. 唯一计划：装配CLOSED wave、一个已完成全部核实及NEW裁决的OPEN P1 problem、没有本快照返修计划，自动修复与attempt额度足够。公开workflow show核对wave已收齐、问题ID、当前SR和无在途目标；保存前置清单。真实执行BIN workflow prepare-action --root R --package-root I --run-id RUN --action development-worker，取得计划P、TaskKey K和dispatch D；相同参数再执行一次，保存两次argv、退出码、输出与即时show。比较计划数、P/K、问题有序集合及派发。第二次拒绝或返回既有待办均可，但不能创建第二个逻辑计划。

B. 错误后重试：另建独立fixture，真实Git的BR/SR、已确认需求、CLOSED wave、已裁决OPEN problem与合法唯一返修计划P都完整。直接构造该计划首次dispatch D1已因TEST-CONSTRUCTED RUNTIME_ERROR终结的历史：D1绑定P/K/SR/同一问题集合，有配对模拟生命周期、不可变错误记录及终结attempt；逻辑返修义务仍有效，已用attempt=1、MaxAttempts=2，无其他CLAIMED工作者、无第二计划、没有开始第二attempt，也没有新快照或新增授权。更早审查、裁决和错误终结均是隔离前置，不是本条被测操作；不要求先通过record-action development-worker写入RUNTIME_ERROR，不猜测或新增错误登记专用命令。前置准备一次完成，完整性/CAS按公共协议形成。真实workflow show必须读到D1的错误终态、未完成的同一返修义务P/K、1/2额度及无在途冲突；保存state原文、构造清单和show。

前置封存后，B只实际执行BIN workflow prepare-action --root R --package-root I --run-id RUN --action development-worker一次，保存argv、退出码、stdout/stderr、返回prompt及后续show；比较新D2与D1、P/K、问题集合和attempt计数。不能在该调用之后把D1改为错误终态、重置额度或手填D2以补成功。

判定边界：A/B中公开show与真实文件证明各自前置和参数适用后，待测prepare被拒绝就是相应成功路径未满足的真实FAIL证据，保留产品具体拒绝和前置依据；不得因为错误文案称“专用入口”“worker未就绪”就自动改叫环境问题。若确实无法建立/读出上述前置，具体列出缺失绑定或序列化材料，B记未执行；先前错误记录入口的拒绝不替代B的执行证据。
oracle: A第一次prepare产生一个逻辑返修计划，第二次拒绝或返回既有待办，逻辑计划数仍为1。B在合法RUNTIME_ERROR终结前置后prepare必须成功：D2不同于D1，P、K和问题集合均不变，第二attempt消耗唯一剩余retry，原错误及D1审计保留；不创建第二计划、不新增基础义务或授权。前置已独立核实后B仍被拒绝、复用已终结D1、丢失错误或变更P/K，均为FAIL。不能用未建立错误终态后的普通prepare成功代替重试通过。
primary AC bindings: AC-011
review status: PASS

CASE-010
mode: blackbox
description: 选中白盒 QA 时，wave 必须绑定最终包含白盒测试且 case-set 已冻结的快照。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：装配 A2+已完成开发与真实 S1，选择 blackbox、whitebox、G，黑盒已 FROZEN，白盒尚未设计。操作：在 S1 实际尝试 prepare-review-wave/QA execution/门并记录屏障。实际准备白盒 qa-design；用模拟设计身份完成前置，在 R 写入本例新建结构测试文件（例如 Python unittest 分别检查 demo 输出和输入不改写），HEAD 暂留 S1。通过 qa-design --dispatch D1 --mode whitebox 提交设计和唯一 --test 文件::函数；宿主随后真实 git add/commit 得 S2，再 workflow snapshot 不带设计 dispatch。模拟 QA Review 的第一份输入固定为某条 case FAIL，实际 qa-review 记录；增量修改该条设计/文件，先登记设计再由宿主提交 S3/snapshot。第二份模拟 review 固定为逐 case PASS 加一个有明确测试修改内容的 P2 suggestion，实际记录并取得稳定 suggestion ID；设计者在本执行者角色内独立决定修改，以 qa-design --per-suggestion --suggestion-id 提交吸收，随后宿主提交测试变化 S4/snapshot。另设无建议 PASS 的独立对照以验证 S2 即最终快照。各阶段真实尝试 wave 准备；只在全部 mode FROZEN 且版本绑定最终快照后允许初始化。以上S1→S4与无建议S2分支分别保存其实际结果；新的review-ready快照隔离另用下述独立分支，不能在上述某次目标拒绝后手改state救活同一轨迹。所有审查输入/身份是隔离构造，不启动白盒审查模型或等待特定建议。

【独立新快照分支】
1. 在另一个R/RUN/E中用真实Git建立含本fixture新写白盒测试的旧快照T4、以及其后普通修复快照T5；不要把原分支的S4/S2冒充此分支已真实执行的历史。测试分别通过新文件中的独立函数验收demo emit、缺文件和README，引用均能在T4/T5定位；T4的demo emit故意输出beta+LF，T5只把该实现修正为需求的alpha+LF，其他公开行为和测试方法/步骤/oracle不变。T4/T5的文件和commit真实存在，当前HEAD=T5且干净。
2. 一次性装配正常可达的历史：同一run的旧wave W4绑定T4，expected-set恰为blackbox execution、whitebox execution、G；两个mode的emit用例FAIL，其余用例PASS，G有与beta输出相符的完整P1及逐条rationale/核实。W4已收齐、已完成裁决、创建过且只有一个返修计划；该返修已交付T5，对应problem已RESOLVED，开发完成、最高强度自测和生命周期是明确的TEST-CONSTRUCTED前置。自动审查已用1/总3，后续验证有正常额度，无待处置错误、额外返修或在途工作者。
3. 当前快照投影直接装配为T5已登记：两个mode仍用各自原冻结case-set内容/version，全部白盒测试位于T5；其冻结版本与当前快照的合法绑定/影响判定完整，均为FROZEN。普通实现修复不THAW、不改测试函数名、不重新qa-design/qa-review。两个旧emit FAIL都列入当前AFFECTED待执行范围，G待重审；T4的历史结果保留原绑定，没有把它们写成T5结果或为这些目标作PASS carry。T5没有wave、没有target结果、没有已准备的新目标dispatch。
4. 准备封存前真实workflow show，逐项核对T4/W4的完整历史、唯一返修已完成、T5/currentSnapshot与原生HEAD、两mode FROZEN/version对T5的绑定、白盒引用存在、待重跑目标和剩余额度。留构造清单与show；当前绑定、冻结状态或开发完成项欠缺时先完成这个全新fixture的前置，不能带缺项进入被测调用，也不需要从旧执行者留下的半完成轨迹绕行carry/THAW/worker/snapshot。
5. 封存后真实执行BIN workflow prepare-review-wave --root R --package-root I --run-id RUN；保存返回、规范prompt及show。比较新W5与W4、快照、三个目标的dispatch/compared和结果来源，再相同参数调用一次确认不产生第二个T5 wave。不为T5预填任何结果。W5的门比较范围必须为BR..T5，两个QA执行绑定T5；旧wave结果只能保留在T4历史，不能把W5直接判已收齐。
6. 合法前置下prepare-review-wave拒绝T5、错误要求普通修复重新设计、或以W4结果关闭W5，均保留为该分支真实FAIL，不通过改写前置后的结果来抹掉。若前置本身未能读出具体绑定，则列出缺项而非宣布所有构造方式不可行。两条原S4/S2分支的已执行证据可继续使用，本分支只补新快照隔离。
oracle: S1、未完成 review 的 S2/S3 都不能初始化 wave。最终 wave 只绑定含最终白盒测试的 S4，全部门比较 base..S4，QA execution 也绑定 S4；新 review-ready 快照拥有独立 wave，旧结果不算已收齐。 测试提交者为 host；每份 QA Design 都在其派发源快照未漂移时先登记，host 随后提交并登记快照，再进行 QA Review。若操作者让设计者提前提交或漏做快照登记而产生漂移，属于前置不成立，不能计作被测产品错误。 独立分支的T5满足全部review-ready前置后，必须新建W5≠W4且绑定T5；expected-set精确为两种QA execution和G，不使用T4结果把W5判为已收齐，相同T5再次准备不新增wave。两mode case-set内容/version在普通实现修复中保持不变，但当前快照绑定为T5；G比较BR..T5。任一已核实前置下的待测快照推进或wave准备被产品拒绝，按真实拒绝记录对应分支FAIL；构造的旧历史不充作这些调用成功的证据。
primary AC bindings: AC-012
review status: PASS

CASE-011
mode: blackbox
description: NEW、SAME_AS、DISMISSED 裁决分别持久化 finding、problem 和五维语义关系。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：装配 CLOSED 的三条 TEST-CONSTRUCTED finding，分别取 F1/F2/F3，F1/F2 的五维内容相同但原报告不同，F3 经核实不在需求范围；均未裁决。操作：一次公开 adjudicate-wave 将 F1 设 NEW，F2 SAME_AS 指向 F1 所建 canonical problem（按公开允许的引用顺序分两次提交并刷新 CAS），F3 DISMISSED；每项填完整五维、引用、verification、非空理由。实际 show 并 prepare-action --action development-worker，比较三个稳定 finding ID、一个 problem 和一次返修义务。
oracle: 三项有不同稳定 finding ID；NEW 产生稳定 problem ID 和 OPEN 状态，SAME_AS 保留自身证据但绑定同一 problem，DISMISSED 不创建 problem。返修只包含一个 canonical obligation。
primary AC bindings: AC-013
review status: PASS

CASE-012
mode: blackbox
description: 与已解决 problem 或已驳回 finding 相同的新报告只增加审计记录。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：装配下一 CLOSED wave；历史中直接给 problem P1=RESOLVED，另有已 DISMISSED 的 F0。本 wave 有两个新 TEST-CONSTRUCTED finding，其五维分别与历史 P1/F0 一致，尚未裁决；历史修复完成/carry 仅为前置，不执行实际返修。操作：用 adjudicate-wave 记录新 finding SAME_AS P1，另一条按公开关联历史 dismissed 项的协议记录同语义驳回及引用。show 后实际尝试准备返修，核对只新增审计、没有 OPEN problem/新修复义务。
oracle: 新 finding 和裁决可见，但不重新打开已解决 problem、不为已驳回语义创建 problem，也不产生返修义务。
primary AC bindings: AC-014
review status: PASS

CASE-013
mode: blackbox
description: 根因或实际后果不同的报告可形成新问题，较晚 wave 的新 P0/P1 被标记为漏审并阻塞。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture、固定阻塞载荷。

准备：装配有历史 problem 的下一 CLOSED wave，前轮计数设1；新 F1 与旧问题同需求/模块但根因不同，新 F2 根因相同但实际后果不同。F2 固定为具有完整证据的 TEST-CONSTRUCTED P1，文本和所引用小脚本文件一致。操作：通过 adjudicate-wave 显式 NEW 两项并给出各自五维与验证；读取 show 并实际准备返修，检查新的两个 problem ID、迟到 P1 的前轮漏审标记及阻塞。不为制造“较晚轮”真实重跑第一轮。
oracle: 两项获得新的 problem ID，不被文本相似性吞并；较晚首次出现的 P1 明确标记前轮漏审，并作为当前 wave blocker 进入返修。
primary AC bindings: AC-015
review status: PASS

CASE-014
mode: blackbox
description: CLI 不用文本哈希自动判断 finding 语义重复。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：分别构造三个 CLOSED fixture，每个有两个字节相同的 TEST-CONSTRUCTED finding、不同 ID 和来源绑定；不裁决。操作：第一份直接 prepare-action --action development-worker，观察缺裁决阻断及仍独立的两条记录。第二份经 adjudicate-wave 将二者明确 NEW；第三份先 NEW 第一条，再刷新 CAS 将第二条 SAME_AS。show 比较 problem 数2和1。不借文本相同预写 CLI 的归并输出。
oracle: 未裁决时两项保持独立且返修被阻止；显式 NEW 会生成两个问题，显式 SAME_AS 才会归并。文本相同本身不改变语义状态。
primary AC bindings: AC-016
review status: PASS

CASE-015
mode: blackbox
description: 错误问题归并可通过 revision/CAS 纠正，并根据返修是否已派发决定归入当前或下一 wave。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：两个 CLOSED fixture 均有一条已被错误 SAME_AS 的 TEST-CONSTRUCTED finding，完整保留旧 problem/裁决 revision；一个尚未派发返修，另一个已存在真实格式的唯一返修计划与 CLAIMED 修复 dispatch。后一份计划及生命周期直接构造。操作：分别以错误 revision、随后新取的正确 revision通过公开问题裁决纠正入口改为 NEW，提交完整五维、证据引用与非空理由。错误请求比较即时 state 字节；合法请求比较历史、当前计划与待下一 wave 的 obligation。对已派发分支真实提交一个低成本修复 Git 快照并推进，检查新增 obligation 在该新快照的下一 wave，原快照没有第二计划。
oracle: 过期 CAS 被拒绝且零状态变化；合法纠正保留原 finding、problem 和历史裁决。派发前纠正更新同一逻辑返修计划，派发后新 OPEN obligation 进入返修后快照的下一 wave，不为原快照创建第二返修。
primary AC bindings: AC-017
review status: PASS

CASE-016
mode: blackbox
description: 纯 gate P2 触发一次返修，但提出该 P2 的门不复审且不增加审查轮次。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：装配 CLOSED，所有 QA/gate PASS，仅 G 有一个已裁决 OPEN P2；两个已通过门中 G2 被确定为未受影响，QA 设 Q1 与修复有关、Q2/Q3 无关。模拟前序报告/生命周期，不启动任何审查模型。操作：真实 prepare-action --action development-worker，读取最高强度自测要求。给测试修复派发提供 TEST-CONSTRUCTED 的已完成构建/全量项目测试/受影响用例自跑三份记录，文件和任务 ID 可定位；它们只是 carry 前置，本黑盒不执行 formal-gates 既有全量套件。对小 demo 实际修改一个提交取得 SR2，经公开修复完成/快照入口提交对应材料；通过公开影响判定/carry 操作推进。比较 G 没有新派发、P2 的 problem/修复快照/原 gate PASS 绑定、review-wave 计数不变、G2 按无影响 carry、QA scope=AFFECTED 且只包含 Q1。
oracle: 只生成一份返修；prompt 明确最高强度自测。提出 P2 的门没有新审查派发，原 PASS 通过绑定 problem ID、修复快照的完成/carry 凭据延续；审查轮数不变，其他门按影响判断，QA 仅 AFFECTED，未受影响结果继承。
primary AC bindings: AC-018
review status: PASS

CASE-017
mode: blackbox
description: 同一 wave 的 QA FAIL、有效 P0/P1 和 gate P2 合并为一份返修。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：装配 CLOSED；逐目标结果固定为 blackbox Q1 FAIL、G P1、G2 P2，三个 finding 各有稳定 ID、不同问题及完整核实，均为 TEST-CONSTRUCTED。需求/文件保证正常可达，review 预算仍有一次自动修复。操作：实际 adjudicate-wave 对三项完成 NEW，再 prepare-action --action development-worker 两次。读取第一次 prompt 与第二次返回，逐项核对同一任务恰含 QA FAIL、P1、P2，logical plan/TaskKey 只有一个。不真实运行多个审查来收集这三种等级。
oracle: 三类问题均在同一逻辑返修任务中，P2 不产生独立任务，重复准备不增加 repair obligation。
primary AC bindings: AC-019
review status: PASS

CASE-018
mode: blackbox
description: 纯 gate P3 不返修、不增加 wave，并在 Seal 时一次性展示。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：装配 FINAL，无 P2/阻塞/未解决错误；G/G2 的 PASS 各带一个固定 TEST-CONSTRUCTED P3，未处置，两条消息有不同哨兵。真实 Git 候选干净，BR..SR 保持单条交付提交，满足该路线现有 FULL 前置。操作：show 保存 wave/repair 数，再真实 workflow seal；保存终态输出和所返回报告，清理后不要求不存在的 state 继续可读。在本次Seal的当前P3建议汇总中按稳定finding ID逐项核对，两项各列一次且没有修复/wave增加；gate历史、原始finding和当前advisory等不同JSON投影中的同一ID属于同一记录的引用，不把对整个序列化JSON搜索消息字符串的总次数当作提示用户的次数。保存各投影路径、ID对应及用户可见建议汇总，检查没有重复处置请求。不向用户确认虚拟建议，也不真实准备门审。
oracle: Seal可成功，未产生返修或额外review wave；当前未处置P3建议汇总恰含两个稳定finding ID，各列一次且无遗漏，不要求逐项处置，展示不改变门PASS或终态。同一ID在gate原始报告/审计/当前advisory等不同结构投影中出现不等于重复催办；同一当前建议列表重复列项或再次要求用户处置仍不符合AC-021。
primary AC bindings: AC-020, AC-021, AC-049
review status: PASS

CASE-019
mode: blackbox
description: 隔离构造的原始 gate P2 经公开记录后，只有补齐阻塞证据及独立实际复现才能重定级为 P1 并阻塞；缺项请求保持状态不变。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备：在执行宿主已建立的同一候选安装窗口，在独立 R/RUN 装配 A2+DEV+WAVE，custom 仅 implementation-quality-gate（G），不选 QA；真实 BR/SR 文件使用下面精确载荷。需求确认、产品审/技术审 PASS、单 Batch/no-split、开发完成及 G 的 CLAIMED/模拟生命周期全部直接构造为 TEST-CONSTRUCTED，G 尚未记录结果，wave 仅 G 一个 target，未建立 problem 或返修。这里不创建协调模型，不启动 P/T/W/G，不查 agent_id，不等待真实审查给出 P2，不向真人确认 TSV 虚拟项目。

2. fixture 内容如下。唯一 requirement.md 的完整正文：
# TSV 数量汇总工具

## 目标与使用场景

为个人本地导出的 TSV 清单计算数量总和。用户通过 python3 totals.py <输入文件路径> 调用工具，以 stdout 的整数结果作为本次清单总量。

## 范围与约束

使用 Python 3 标准库。输入是现有、可读取的 UTF-8 文件，包含 1 至 100 行；每行恰好是非空 ASCII 标识、一个制表符和 0 至 999999 的十进制整数。行间使用 LF，最后一行允许有或没有 LF。两种末尾形式都是正常输入。工具只读取输入，不修改文件。不要求其他数据格式、并发访问或网络来源。

## 方案

单个 totals.py 读取文件、逐行取得数量、求和并输出。这是一个 Batch，不拆分。交付脚本和两个示例输入文件。文件不存在时非零退出，stdout 为空；stderr 可采用 Python 原生错误说明。其他不符合上述格式的输入不在范围内。

## 需求点

### REQ-001：完整汇总全部输入行

#### 要求

每个正常输入行恰好计入一次。文件末尾有没有 LF 不改变结果。成功时退出码 0，stdout 是十进制总量及一个 LF，stderr 为空。

#### 验收条件

- AC-001：alpha<TAB>10000<LF>beta<TAB>2500，无论末尾是否再有 LF，stdout 均为 12500<LF>。不得遗漏最后一行或把不完整汇总作为成功结果返回。
- AC-002：输入调用前后字节一致；不存在的文件路径使程序非零退出且 stdout 为空。

#### 来源

CASE-019规定的TSV隔离测试载荷与验收输入；原始P2反馈使用本轮用户明确授权的TEST-CONSTRUCTED输入。

## 完成定义

上述正常输入与文件错误行为满足要求，脚本和示例文件可直接使用。

待审 totals.py 的完整内容（第 6 行为末段删除）：
import pathlib
import sys

text = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
total = 0
for line in text.split("\n")[:-1]:
    sku, quantity = line.split("\t")
    total += int(quantity)
print(total)

examples/with-lf.tsv 精确 UTF-8 字节为 "alpha\t10000\nbeta\t2500\n"；examples/no-final-lf.tsv 为 "alpha\t10000\nbeta\t2500"，末尾无 LF。这是故意提供的错误测试交付，不是声称满足 fixture 需求的正确实现。BR 包含正式需求及必要宿主配置；SR 相比 BR 新增这三个交付文件。安装包和 E 不进入交付 diff，SR 工作树干净。该 fixture 自己的 REQ-001/AC-001/AC-002 不替代本用例主绑定 AC-022。


被测操作依次为：
1. 使用该 SR 实际执行 python3 R/totals.py R/examples/with-lf.tsv 与 python3 R/totals.py R/examples/no-final-lf.tsv，保存精确输出和输入调用前后字节；有LF应得到12500\n、无末尾LF应得到10000\n，两者exit=0/stderr空。这些必须是实际输出，不以 fixture 推导代替。
2. 对已经构造合法认领前置的 G 派发 D，实际执行：
BIN workflow record-gate --root R --package-root I --run-id RUN --gate implementation-quality-gate --dispatch D --compared BR..SR --status PASS --message 'TEST-CONSTRUCTED CASE-019：隔离测试输入，非正式审查结论' --finding 'TEST-CONSTRUCTED CASE-019：totals.py:6 丢弃无末尾 LF 输入的最后一行' --severity P2 --severity-rationale '将固定测试问题的初始输入设为P2，以验证补证据后的重定级；非真实审查者定级' --location totals.py:6
读取 show，从本 wave、sourceKind=gate、severity=P2、带该哨兵的 postReviewFindings 取得稳定 F；不用 raw/dispatch ID。
3. 核对 wave 全部 target 已终结，F 未裁决/未驳回/未归并、没有返修。按本次实际复现填完整 NEW 请求；下面占位符换成该 fixture 实际值，所有等级/来源措辞明确是构造的原始 P2：
{"waveId":"<show.activeReviewWaveId>","stateRevision":"<show.stateIntegrity>","findings":[{"findingId":"<F>","decision":"NEW","dimensions":{"requirement":"REQ-001","normalFailurePath":"python3 totals.py examples/no-final-lf.tsv","affectedModule":"totals.py 的 TSV 行读取与求和","rootCause":"split(\"\\n\")[:-1] 无条件删除最后一段，无末尾 LF 时删除有效末行","actualConsequence":"正常两行输入应输出12500换行，实际成功退出却输出10000换行，遗漏beta行2500"},"verification":{"scope":"<实际核实本次已确认REQ-001、无末尾LF正常输入和目标F对应关系>","severity":"<实际核实错误总量和成功状态掩盖遗漏的核心验收后果，说明为何P2不足>","causality":"<实际核实两份输入只差末尾LF、输出差2500及SR中第6行>","reproduction":"<两次实际argv、退出码、精确输出、输入字节不变与E中日志引用>"},"references":["requirement.md","totals.py:6","examples/with-lf.tsv:2","examples/no-final-lf.tsv:2"],"reason":"以本次独立复现和完整阻塞证据，纠正用户授权构造的原始P2；该输入从未冒充真实审查结论","gateP1Evidence":{"severityRationale":"REQ-001要求末尾无LF的正常输入也完整汇总；本次实际调用成功返回10000而非12500，错误结果破坏核心汇总验收且成功状态掩盖遗漏，因此P2不足","requirement":"REQ-001","normalPath":"python3 totals.py examples/no-final-lf.tsv","actualError":"SR中的totals.py:6丢弃有效末行，实际成功退出但输出10000换行，预期12500换行","deferralImpact":"延后处理会交付一个对已确认正常输入成功返回不完整总量的工具，核心汇总验收不能通过","locations":["totals.py:6","examples/no-final-lf.tsv:2"]}}]}
gateP1Evidence.requirement/normalPath 分别与 dimensions.requirement/normalFailurePath 完全一致。
4. 在同一 fixture 依次提交 A/B/C：从完整 JSON 复制并分别用 jq 'del(.findings[0].gateP1Evidence.requirement)'、jq 'del(.findings[0].verification.reproduction)'、jq 'del(.findings[0].gateP1Evidence.severityRationale)' 产生请求。每次先重新 show 取得当前 wave/CAS，再实际 BIN workflow adjudicate-wave --root R --package-root I --run-id RUN --input <JSON>；即时前后 state 用 cmp。只有确实零写入、仍为原 P2 才继续下一变体；不恢复 state、不重报 gate、不消费 reviewer retry。
5. 刷新 CAS，完整载荷 D 经同一 adjudicate-wave 提交一次，保存前后 show。核对 F 变 P1、G 与 wave target 有效结果 FAIL、原始 P2/构造标记与完整纠正证据仍可追溯。实际 repair-preview，要求 blockingFindings 含 F。
6. 前置已保证其余 Seal 条件满足、HEAD=SR且干净；执行不带skip的workflow seal，要求阻止带未解决P1的Seal；即时show必须证明其他条件已满足，剩余G/wave未通过由本次F升级P1所致。错误可以指向该P1，也可通过G/wave投影定位到同一未解决F，不要求stderr逐字打印finding ID或P1字符串。独立的dirty/授权/缺结果等阻断不能作此证据。比较纠正前后派发、wave、retry、额度没有增加。到此结束，不启动真实返修；版本/身份/前序/dirty/CAS 等错误不算补证据校验或P1阻塞证据。
诊断与判定：A/B/C分别只删一个指定字段，保留每次输入差异、非零退出、即时state字节不变以及完整D成功的对照；据此锁定缺项校验。保存stderr/receipt中的实际诊断，但AC-022没有规定adjudicate-wave错误必须逐字使用三个字段名，也没有要求Seal的stderr重复列出F。不能用本条取消record-gate在AC-001中明确要求的rejectedFields和原因。
oracle: P2经实际record-gate登记并保留TEST-CONSTRUCTED标记；实际TSV调用有LF=12500\n、无末尾LF=10000\n，均exit0/stderr空且输入字节不变。A/B/C只缺gateP1Evidence.requirement、verification.reproduction、severityRationale各一项，均非零且即时state字节不变，F仍P2，无新裁决/P1/dispatch/retry/rejected-result回执；完整D成功形成单变量有效对照。D退出0，同一F变P1，G及wave target有效结果变FAIL，原P2/dispatch结果及完整纠正证据保留，repair-preview.blockingFindings包含F；在其他Seal条件成立且G/wave未通过可由F的未解决P1唯一解释时，Seal必须被拒。缺项校验、完整补证据、P1阻塞与原审计要求均不放宽，但不把某个错误字符串格式或在stderr重复打印ID作为额外验收。前置/版本/身份/dirty/CAS/额度错误不能替代目标证据，未执行不判通过。
primary AC bindings: AC-022
review status: PASS

CASE-020
mode: blackbox
description: QA Review中一个实际有步骤缺陷的用例被记为FAIL时，集合不能冻结，修正该用例后必须重新审查。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）。只选默认小fixture，不选固定阻塞载荷。本条独立验证逐用例FAIL。

1. 在独立R/RUN/E装配DEV及QA(blackbox)设计已提交、尚未review的前置，真实需求只有默认REQ-001与AC-001/002/003。Q1/Q2/Q3都有有效ID和各自主AC绑定。Q1故意把步骤写成“在R执行python3 demo.py，不传子命令，保存退出码与stdout/stderr”，判据仍是“exit0且stdout精确alpha+LF”；Q2保留缺文件调用和非零/stdout空判据，Q3保留README中Demo判据。Q1错误是测试操作漏掉公开必需的emit子命令，不是产品实现错误。保存三条完整设计、需求与当前case-set version V0；不能仍装配步骤完整的原Q1后声称它缺参数。该R需要的黑盒隔离工作区、注入需求、身份和更早历史依公共协议构造并核对，QA Review结果不预写。
2. 实际执行BIN workflow prepare-action --root R --package-root I --run-id RUN --action qa-review --mode blackbox，保存规范prompt和返回dispatch DR；按公共协议只为DR补齐TEST-CONSTRUCTED配对生命周期。准备固定review输入：Q1 FAIL，理由明确“步骤python3 demo.py遗漏emit，无法用于验证要求的正常emit调用”；Q2/Q3 PASS；source-decision REQ-001=FAIL；point-decision AC-001=FAIL、AC-002=PASS、AC-003=PASS；case-decision逐条与上述Q1/Q2/Q3一致。本次提交不主动提供集合级 finding，验证 Q1 的逐用例 FAIL 能被有效记录；不要求记录结果排除其他合格集合级 finding。
3. 实际执行BIN workflow qa-review --root R --package-root I --run-id RUN --dispatch DR，依次给每个真实Q ID传--case ID --outcome FAIL|PASS（Q1紧随--reason），并逐项传--source-decision、--point-decision、--case-decision。参数与DR实际注入的完整合同一致；保存完整argv、固定输入、退出码、stdout/stderr及show。有效FAIL记录应被接受，不能拿unknown flag、身份、版本、映射缺项等协议拒绝当成QA Review FAIL。
4. 实际调用BIN workflow prepare-action --root R --package-root I --run-id RUN --action qa-execution --mode blackbox，保存因QA Review未通过/集合未冻结而不允许执行的结果。随后实际prepare-action --action qa-design --mode blackbox取得DD；以BIN workflow qa-design --root R --package-root I --run-id RUN --dispatch DD --case 'demo emit正常输出' --case-id <Q1实际ID> --mode blackbox --procedure '在R执行python3 demo.py emit，保存argv、退出码和stdout/stderr' --oracle '退出0、stdout精确alpha加一个LF、stderr为空' --ac AC-001提交修正，保留Q2/Q3原定义不变。相关设计身份与生命周期按公共协议准备。
5. 保存设计后的show，再实际prepare-action --action qa-review --mode blackbox，取得DR2并读取其规范prompt，比较V0与修正后的V1、DR与DR2及待审Q1。到取得绑定V1的新QA Review派发为止；不把尚未返回的DR2记为PASS，不运行交付已有测试或启动真实审查模型。
oracle: 第一份qa-review有效记录被接受，Q1=FAIL、Q2/Q3=PASS、mode总体FAIL，V0不是FROZEN且qa-execution准备被拒；必须保留 Q1=FAIL 及其遗漏 emit 的具体理由、Q2/Q3=PASS、mode=FAIL、V0 未冻结和执行准备被拒。若另有集合级 P0/P1，保存其完整记录，并按 REQ-018 独立核对需求、正常验收路径、实际覆盖后果、等级阈值及绑定；仅凭条目 FAIL 定级仍不合格，但合格集合 P1 的存在本身不构成本例失败。修正 Q1 后版本变化、Q2/Q3 批准保留、Q1 待审及新 DR2 绑定要求全部保留。仅修正Q1后V1不同于V0，Q2/Q3定义与PASS保留，Q1回到待审，不能靠设计提交直接变成QA Review PASS或FROZEN；新DR2不同于DR并绑定V1。参数、身份、快照或结果合同拒绝不构成本例通过证据。
primary AC bindings: AC-023
review status: PASS

CASE-021
mode: blackbox
description: 集合级 P2/P3 不使 QA Review FAIL，但未吸收建议前不能冻结；无建议的 PASS 立即冻结。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：两份相同的 DEV+QA(blackbox)，review 未记录。A 的模拟结果为完整逐 case/source/point PASS，加集合 P2“补一个正常输入边界”及 P3“澄清证据输出路径”，各有具体建议文本；B 逐项 PASS 且无建议。操作：实际 qa-review 分别提交，保存返回的稳定 suggestion ID、待吸收状态与 show 中 case-set version/FROZEN。A 不执行吸收，B 不创建空吸收任务；直接观察二者区别。
oracle: 首个 review 总体 PASS，P2/P3 均有稳定 ID 和未吸收状态，但 case-set 未 FROZEN；第二个 fixture 在 PASS 后立即生成 FROZEN version，不要求空吸收轮。
primary AC bindings: AC-024, AC-025
review status: PASS

CASE-022
mode: blackbox
description: suggestion absorption 仅在绑定 review PASS 后逐条执行，并在覆盖重算通过后冻结。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：独立构造以下 fixture，不启动审查：review 尚未 PASS；已 PASS 且有明确要求新增/修改/删除冗余用例的三条稳定 P2/P3 suggestion；本次规范吸收派发绑定三条建议却在返回中遗漏一条；重复建议 ID；无关 ID；某建议已吸收；删除唯一 AC 覆盖。每个已 PASS fixture 有完整 source/point/case 映射、批准 whitelist 和准确来源 review/version；删除合法分支预设有另一已批准用例覆盖同一 AC，缺覆盖分支只有唯一用例。操作：实际 prepare qa-design 的 suggestion-absorption 后，以 qa-design --per-suggestion --suggestion-id 和 --case/--case-id/--remove-case 提交对应增量。重复吸收分支先真实吸收一次再重复同 ID。每一负向分支独立首 attempt，检查目标错误及即时状态；合法完整吸收对照比较冻结版本、来源与零新 QA Review。不用等待模型碰巧提出三种编辑。
“遗漏”的具体构造与观察：用独立fixture装配该QA Review已经PASS、三条尚未吸收的稳定suggestion S1/S2/S3及完整覆盖；实际prepare本次suggestion-absorption，并保存规范prompt，确认它明确把S1/S2/S3都列为本派发须处理的范围。然后只提交S1/S2的合法变更、不提交S3，使该返回确实漏掉本派发绑定项；这是被测的不完整返回，不能把输入预先改成完整。按AC-027要求该遗漏返回被拒，保存实际退出码、dispatch/吸收记录及S3仍未处理的证据；即使产品接受后保持REVIEWED_PENDING_SUGGESTIONS，也不以“未冻结”替代此处的遗漏拒绝判据。若某次公开派发实际上只绑定S1/S2，review全局尚有S3不能单凭这一点算该派发遗漏；该输入不满足本负向fixture，须另建并取得绑定三项的规范派发，不能虚称范围。完整三项对照另用初始独立fixture一次提交。缺PASS、重复/无关ID、重复吸收与覆盖损失的各原分支继续执行，不改变其拒绝及全部吸收后才能冻结的要求。
oracle: PASS 前及所有非法吸收均非零退出。合法吸收保留 suggestion 来源、直接获批且不派新 QA Review；只有全部建议吸收且完整映射、unbound AC 和 whitelist 校验通过后才生成新 FROZEN version。 遗漏拒绝针对本次规范派发明确绑定而返回中未处理的suggestion ID；必须保存真实派发范围与提交集合的差集。仅有全局待吸收项不足以证明本次返回遗漏，但本次确有遗漏时，返回0且仅保持未冻结不能代替AC-027要求的拒绝。
primary AC bindings: AC-026, AC-027
review status: PASS

CASE-023
mode: blackbox
description: 普通代码返修不会改变冻结 QA 用例版本或重新开放设计。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：直接构造 blackbox/whitebox 均已 FROZEN、批准状态及旧 execution/carry 有效的 CLOSED fixture，需求/oracle/用例可执行性不变。为 demo 的等价实现做一个真实 Git 修改提交，借公开 snapshot/合法修复完成入口固定新快照；开发结束、自测和先前审查仅为构造前置。操作：比较两种 mode 的 case-set version，再实际 prepare-action --action qa-design/qa-review --mode blackbox/whitebox。保存冻结拒绝和合法继承；不先 THAW，不为了新快照重设计。
oracle: version 完全相同，重新设计和复审被拒绝；旧批准状态按合法继承继续存在。
primary AC bindings: AC-028
review status: PASS

CASE-024
mode: blackbox
description: 只有四种稳定变化可 THAW；临时环境错误和其他理由不能解冻。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：六个独立 QA(blackbox)=FROZEN fixture，分别准备四种有实际新文件/需求差异支持的变化：含义、外部行为、oracle、稳定可执行性；另一个类别为未知值，一个只有一次性网络/工具不可用而文件和判据没变。Q1受影响、Q2/Q3未受影响，已有执行/carry记录。操作：通过候选公开 THAW 入口提交该类别、非空理由、来源revision和Q1；保存合法四项与非法两项返回。合法分支再真实 qa-design 修Q1，以构造的逐项review PASS通过真实 qa-review 记录并重新冻结，比较受影响/未受影响记录。临时错误分支通过执行结果入口记录 TEST-CONSTRUCTED RUNTIME_ERROR，不以它申请稳定变化。
oracle: 四种合法 THAW 只把受影响用例置为待设计/待审并使相关旧 execution/carry 失效，未受影响批准状态保留；未知类别被拒绝，临时错误只记 RUNTIME_ERROR。重新满足 QA Review 规则后才生成新 FROZEN version。
primary AC bindings: AC-029, AC-032
review status: PASS

CASE-025
mode: blackbox
description: 普通中间返修固定执行 AFFECTED，保证路线在最终 Seal 前另行要求 FULL。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：构造 guarantee 与非保证 route 两份普通修复后 fixture：已批准 Q1先前FAIL、Q2先前PASS且对应本次实际改动文件、Q3先前PASS且无关；旧执行/自测/修复完成直接构造，新 SR是真实提交。操作：实际准备 qa-execution，保存 scope与case名单，再用每条新编的小demo驱动实际执行Q1/Q2并记录 observation/oracle-result；检查Q3继承。guarantee分支随后在缺最终FULL时真实尝试seal，再按公开FULL入口执行Q1/Q2/Q3三条轻量产品调用并记录，重新seal。非保证分支按其route公开要求观察，不凭本例增加FULL。模拟前序中的PASS不替代这里的实际执行。
oracle: 中间 scope 自动为 AFFECTED，恰含失败和相关 PASS，用例选择无需用户重问；无关 PASS 继承。guarantee 最终候选缺 FULL 时 Seal 被拒，FULL PASS 后放行；非保证 route 不被本需求额外强制 FULL。
primary AC bindings: AC-030, AC-031
review status: PASS

CASE-026
mode: blackbox
description: 开发后门 prompt 不泄露 QA 用例和逐用例执行材料。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：直接装配 DEV+QA(blackbox/whitebox)=FROZEN 与两门可准备前置；用例正文、逐用例执行观察、白盒引用各植入不同独特哨兵字符串。已有批准/执行作为 TEST-CONSTRUCTED 前置，不跑测试。操作：实际 prepare-gate --gate complexity-gate 和 prepare-gate --gate implementation-quality-gate，记录返回的精确规范prompt路径并只读该文件。搜索全部哨兵、CASE ID、测试引用及执行材料，同时核对本次确认需求revision、BR/SR、各自门规则。只观察prepare的实际产物，不手写它。
oracle: 两次 prepare 均成功且目标 ID 分别为 `complexity-gate` 与 `implementation-quality-gate`；两份门 prompt 都包含确认需求、快照范围和各自门规则，但不包含任何 QA 用例正文、逐用例证据或测试实现细节。
primary AC bindings: AC-033
review status: PASS

CASE-027
mode: blackbox
description: 额外返修授权预览完整、只读、版本绑定且在阶段 4 前显示 UNAVAILABLE。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）。

准备选用：默认小fixture；custom 只选 blackbox、implementation-quality-gate（G）和 complexity-gate（G2）。仅在 QA Execution 阶段，在独立 R/RUN/E 中构造以下隔离前置；所有历史审查、建议、吸收、执行、裁决、轮次和确认均标记 TEST-CONSTRUCTED:CASE-027，不修改当前项目正式 run，不真实耗尽审查额度。

1. 先按合法时序装配 QA 历史：blackbox 的 QA Review RQ 对版本 V0 的全部 source/point/case 判定及集合结果均 PASS，同时有两个不同稳定 suggestion ID，分别为 P2 和 P3。P2 建议把 Q1 的输出比较写清为精确 alpha+LF，P3 建议把 Q2 的证据输出路径写清；对应两项 QA Design suggestion-absorption 均已完成，各自引用 RQ 的正确 suggestion ID，保存修改内容、来源 review/version 和 SUGGESTION_APPLIED 记录。吸收后完整 source/point/case 映射、unbound AC 和 approved-whitelist 校验通过，得到当前 V1=FROZEN，并绑定真实候选 fixture 快照 SR；Q1/Q2/Q3 均保有合法批准覆盖。RQ 的两个建议在历史中保留，当前状态都是已吸收，待吸收数为 0。随后才装配基于 SR/V1 的 WAVE 和 CLOSED；不得把 V0 的待吸收阶段当作该 wave 的当前 QA 状态，也不通过 THAW 混入另一未冻结版本。

2. 当前 wave 的 expected-set 精确为 blackbox execution、G、G2，全部目标已终结：blackbox 的 Q1 FAIL、Q2/Q3 PASS；G 有一个有效 P1 和一个 P2；G2 有一个 P3。为四项设置不同稳定 finding ID、可辨识消息及正常可达的完整来源与核实/裁决材料；QA FAIL、P1、P2 对应待返修 OPEN problem，P2 尚未修复；P3 保留未实施 advisory 状态，不列入返修内容。所有须裁决的 finding 已合法裁决，“未修复/未实施”不表示“未裁决”。按公共固定错误载荷或默认 fixture 的明确错误版本提供相符文件/证据；它们是摘要输入前置，不把模拟观察声称为本条实际执行。历史连同当前已完成自动审查轮次固定为 3、剩余自动轮次为 0，没有未解决目标、在途任务、额外返修授权或本快照已派发的返修。派发数量 N 由准备清单中各实际装配的 dispatch ID 逐项计数，开始时间 Tstart 固定为准备时刻前 600 秒，并保持合法时序。用真实 workflow show 逐项核对 SR、V1/FROZEN、RQ 两建议已吸收、wave 收齐及裁决、轮次和 N，保存构造清单及 show；前置不满足时不进入授权成功对照。

3. 预计重跑范围固定为 build-target="python3 -m py_compile demo.py"、test-target="python3 demo.py emit"、qa-case="blackbox=Q1,Q2"；Q1/Q2 替换为该 fixture 真实批准的 CASE ID，保持顺序，Q3 不入本次重跑。构建与项目测试目标是待返修后的计划，在本条只验证其摘要传递，不声称已执行或已通过。记录调用前时刻 t1、即时 state 原始字节、run 目录文件清单和授权/repair/wave 计数，E 放在 run 目录之外；实际执行：
BIN workflow repair-preview --root R --package-root I --run-id RUN --build-target "python3 -m py_compile demo.py" --test-target "python3 demo.py emit" --qa-case "blackbox=<Q1-ID>,<Q2-ID>"
保存完整 argv、输入、退出码、stdout/stderr 和调用后时刻 t2。逐项比较摘要与准备清单；用 cmp 核对即时 state 字节，并比较 run 文件清单、回执、授权记录和 revision，确认预览没有任何状态写入。把返回的摘要 digest 记为 P、生成预览时的 state revision 记为 V，不自行计算或预填摘要输出。

4. 在 P/V 所绑定的状态未发生写入变化时，向 E 保存一份明确覆盖该完整摘要、P/V 和仅一轮额外返修的 TEST-CONSTRUCTED 用户确认，作为隔离授权输入；按公开协议实际执行：
BIN workflow authorize-repair --root R --package-root I --run-id RUN --cycles 1 --preview-digest P --state-revision V --build-target "python3 -m py_compile demo.py" --test-target "python3 demo.py emit" --qa-case "blackbox=<Q1-ID>,<Q2-ID>"
所有 root/run/ID/P/V 均替换为该 fixture 实值，授权与预览的重跑参数逐字相同。保存实际返回和后续 show，比较授权前后增量、摘要绑定及既有结果。到此结束，不实际派发额外修复；其他用例承担旧 digest 失效和返修执行验证。
oracle: repair-preview 退出 0，摘要的当前/已完成自动审查轮次为 3、剩余为 0、派发次数精确为准备清单的 N；运行时长落在 t1-Tstart 至 t2-Tstart 的区间内（按公开输出单位核对舍入）。本轮 QA FAIL 精确包含 Q1，有效 P0/P1 集合精确包含该 P1；门审 P2=1 且待修复，门审 P3=1 且为未实施 advisory；QA Review P2=1、P3=1，均显示已吸收，未吸收数为 0，不把 RQ 的历史建议显示为阻止当前 V1 冻结的待办。构建/项目测试目标与传入字符串逐字一致，QA mode 只有 blackbox、case 有序列表恰为 Q1/Q2，不包含 Q3；engine 累计派发额度明确为 UNAVAILABLE，没有估算剩余额度。输出包含非空 digest P 和当前 state revision V。预览前后权威 state 字节、revision、回执、授权/repair/wave 计数及 run 文件清单不变，没有新 run 文件。紧接的真实 authorize-repair 退出 0，只增加一次一轮授权并保存对 P/V 的绑定；不产生新 dispatch、不执行修复、不把 QA FAIL/P1/P2 改成 PASS，不改变 V1/FROZEN 或已吸收建议状态。未吸收建议阻止冻结、错误版本/CAS、前序缺项、身份、未知参数或额度前置错误均不是本例的授权成功证据。 QA Review的RQ/suggestion稳定来源继续在既有用例集与吸收记录中核对；AC-036要求摘要展示数量和处置状态，未要求摘要再次展开每个review/suggestion来源ID。摘要中缺该重复来源字段本身不判FAIL，数量/状态缺失或错误仍判FAIL。
primary AC bindings: AC-036, AC-037, AC-039
review status: PASS

CASE-028
mode: blackbox
description: 授权预览后任何合法状态变化都会使旧 digest 失效。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：直接装配自动审查轮次耗尽、一个QA FAIL和一个P1已裁决、具有明确重跑清单的可授权 CLOSED fixture及一个合法待处置P3，不真实跑耗尽历史。操作：实际取得返修授权摘要P0；通过公开finding处置入口提交该fixture模拟用户的有效决定，使真实state revision推进。用P0的digest/revision申请authorize-repair，保存拒绝且零授权变化；重新预览得到P1，使用P1精确绑定再申请。两个确认都是隔离测试输入；摘要和状态变化必须由公开入口实际产生。
oracle: 旧授权非零退出且不增加授权次数、repair wave 或其他状态；新预览反映变化，新 digest/revision 可成功授权。
primary AC bindings: AC-038
review status: PASS

CASE-029
mode: blackbox
description: 相同 signature 的 OPEN/PENDING/CLAIMED 派发复用既有记录，PASS 后拒绝重复。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：分别为 product-review、qa-review、qa-execution、G 构造首次prepare前的正确阶段。操作：对每类先真实prepare取得D/signature，再在该逻辑义务的独立副本fixture中装配正常可达的OPEN、PENDING、CLAIMED状态（仅在被测重复prepare前准备一次，不恢复结果），分别实际重复相同prepare。PASS分支直接构造同输入的合法最终结果、完整对应来源和结束状态，再实际prepare。比较D、signature、派发数与额度；签名基准来自第一次真实prepare，不由测试复制算法计算。
oracle: 在途状态均只返回同一 dispatch，不创建并行项；PASS 后命令非零并明确已有最终结果，dispatch 数和额度不变。
primary AC bindings: AC-040
review status: PASS

CASE-030
mode: blackbox
description: 相同 FAIL 按动作类型返回正确待办，不跨类型创建返修或重置额度。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：分别直接装配 product-review FAIL、start-readiness FAIL、QA Review FAIL、开发后qa-execution FAIL、G FAIL；各自有正确阶段的未处置问题及同signature最终结果。另备DISMISSED finding和归并RESOLVED problem的两份fixture。报告固定TEST-CONSTRUCTED，各阶段均在原额度内；QA/gate波分为收集未齐与已齐/已裁决。操作：实际重复各自prepare，读取类型对应待办、问题/修复投影、额度和dispatch数量；开发后只有收齐且裁决后可指向统一返修，开发前或QA Review不能跳转开发返修。
oracle: 产品/技术审返回 finding 处置和需求修订/重审待办；QA Review 返回 QA Design 修改和重审待办；开发后 QA/gate 只有 wave 收齐并裁决后才指向统一返修。DISMISSED/RESOLVED 项不触发任务，所有分支都不增加或重置轮次、attempt 或授权。
primary AC bindings: AC-041
review status: PASS

CASE-031
mode: blackbox
description: 非 wave 与 wave 义务共享有限错误重试、RETRY_EXHAUSTED 和一次性追加 attempt。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：两个模式各一组独立fixture，分别是非wave product-review义务与COLLECTING wave内G；直接装配“默认attempt只余最后一次”的合法历史，错误记录标TEST-CONSTRUCTED。操作：实际记录这最后一次RUNTIME_ERROR或缺字段造成的REJECTED_INVALID_RESULT，再普通prepare观察RETRY_EXHAUSTED。随后按公开追加attempt入口分别提交错误revision、空理由、以及正确revision/非空模拟用户确认；合法授权只执行一次，再实际prepare并记录一次失败，尝试第二次追加/普通prepare。wave分支通过公开错误处置非PASS终结后提交旧派发晚到结果。保留全部即时计数，不真实运行前面N次失败，也不增加默认数字。
oracle: 重派始终复用 TaskKey并保留历史；耗尽后 show 展示原因、已用/总 attempt 和下一步，普通重派被拒。只有一次正确绑定、非空理由的单 attempt 授权成功并改变 retry policy/basis digest而不新增基础义务；再次失败后不能扩张，必需 action/QA Review 不通过，wave 只能非 PASS 终结，晚到结果只审计。
primary AC bindings: AC-042
review status: PASS

CASE-032
mode: blackbox
description: 用户主动重审必须有理由并遵守审查与返修授权上限。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）。选默认小fixture，以下各请求用独立R/RUN/E；历史PASS/FAIL、额度和身份按正常可达状态直接构造，不真实消耗三轮。

1. 以product-review为明确目标分别建立已有最终PASS和已有最终FAIL的前置，每种再分“自动审查额度仍剩一轮”和“自动三轮已用完”两组。原需求revision、最终报告、finding状态、签名、已用/上限及授权记录均留E；没有RUNTIME_ERROR重试义务，不能把CASE-031的一次追加attempt政策混入主动重审。用真实workflow show确认这些前置，保证用户重审以外的条件齐备。
2. 各组的空理由变体，实际执行BIN workflow prepare-action --root R --package-root I --run-id RUN --action product-review --user-requested，不传--user-reason；必须拒绝且不形成新授权或派发。在各组另一独立变体中不带--user-requested重复原prepare，记录普通去重/额度拒绝及零新授权；不能从普通重复请求推导用户已经授权重审。
3. 有剩余自动额度的正向变体，向E保存TEST-CONSTRUCTED用户事件：“因<具体本次理由>，要求对当前run、当前需求revision的product-review再审一次”，再实际执行同一prepare-action，传--user-requested --user-reason '<上述非空理由及当前run/revision/target>'。保存前后show及新dispatch；核对原最终结果仍在、形成override审计并正常消耗现有额度，不因重审理由重置已用轮次或凭空补充自动额度。
4. 已耗尽的正向变体，E中的独立TEST-CONSTRUCTED用户事件必须明确：“已知当前三轮自动审查耗尽，因<具体理由>，授权当前run/当前需求revision的product-review额外一轮并要求执行”。用同一公开prepare-action --action product-review --user-requested --user-reason '<含上述耗尽后额外一轮授权的非空理由>'提交；这个请求本身承载明确额外重审授权，不能把只有主代理建议的文字当授权。命令可以在同一事务先登记该有界授权/override，再准备新派发；不要求它先失败，也不添加需求未规定且公开入口不存在的“另一步重审授权”命令或新flag。查看返回和show：新派发只能依托本次适用授权及正常额度消耗，原最终结果、已用轮次和历史必须保留；若成功却没有可审计的额外授权、清零旧消耗或赋予无界额度，则FAIL。若公开协议将授权与准备分开，按其明确已文档化入口先记录同一事件；没有该入口不构成缺少第二步。
5. 额外返修另建CLOSED fixture：整轮已收齐且完成核实/裁决，只有一个当前OPEN P1需要返修，自动修复额度耗尽，尚无绑定repair-preview摘要的authorize-repair授权。真实执行BIN workflow prepare-action --root R --package-root I --run-id RUN --action development-worker --user-requested --user-reason 'TEST-CONSTRUCTED：请求再次开发，但未提供本次返修摘要授权'。必须拒绝、无返修dispatch/计划扩张和无隐式authorize-repair；重审的--user-requested不能代替返修所需最新preview digest/state revision。返修授权成功本身由CASE-027的独立协议覆盖，不在此重跑。
6. 全部保留argv、用户事件、前后授权/额度/原结果、退出码和stderr/stdout；未知参数或前置不可读需分别说明。对于前置有效且具有所需显式授权的正向请求，产品拒绝可形成真实FAIL，不能以“还要找另一授权入口”为由无限推迟判定。
oracle: 空理由或没有用户重审授权的普通重复请求被拒绝且不新增授权/派发。明确用户重审在有自动额度时使用剩余额度，在自动额度耗尽时必须依托一份适用、有界且可审计的额外授权；授权可以由承载同一明确用户事件的公开重审命令先登记并在该事务中消费，不强制另一条命令。所有成功重审均保留原PASS/FAIL、finding和消耗历史及override记录，不重置额度、不无限扩张。仅有operator建议不能冒充该用户事件。额外返修缺最新摘要digest/state revision绑定的authorize-repair时仍被拒；重审override不替代返修授权。
primary AC bindings: AC-043
review status: PASS

CASE-033
mode: blackbox
description: dispatch signature 覆盖全部权威输入且仅对完全相同输入去重。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）。选默认小fixture。本例控制可合法独立修改的输入；对于公开操作必然联动的派生身份，记录整个有效输入组合，不手填派生hash要求非法单字段隔离。以下均仅在QA Execution执行。

1. 由规范派发取程序快照S。允许用S的相同程序与独立测试目录创建配置窗口W0/W1/W2，经文档化正常install切换；W0准备两个正文相同的测试门qa-test-a/qa-test-b，及一个与产品审公开模板同正文、ID为product-review的测试门。W1只在W0增加未选门qa-test-unused；W2只修改W0的qa-test-a模板中一段测试说明。只改隔离测试模板并真实Git提交，保留安装回执、配置身份和S程序身份，禁止改生产实现、手改安装registry或沿用不匹配的admission。各窗口的fixture在该窗口正常start后建立。不得把“一组只能用一个安装窗口”当成额外约束。
2. 所有比较先保存真实workflow show、对应公开模板/需求/QA用例内容、HEAD、完整argv及适用额度；从公开投影记录type、target、mode、BR、SR、requirement revision、case-set version、catalog revision、目标模板revision和动作专属输入身份。目标模板revision使用公开show的目标模板绑定（例如promptHashes[type:target]）；完整规范派发文件自身的hash另存，不把含root/run/dispatch文本的全文hash当成模板revision。不读取源码或计算/复制签名算法。
3. 对可独立控制的八类输入分别做合法配对并实际prepare：①W0中qa-test-a与qa-test-b的prepare-gate（仅target）；②同一真实SR有两个祖先BR的prepare-gate；③同一BR之后两个真实候选SR的prepare-gate；④分别绑定两份真实完整需求的等价阶段fixture；⑤在均无既有用例、其他绑定相同的合法黑/白盒设计fixture中prepare-action --action qa-design --mode blackbox|whitebox；⑥实际用例内容变化后对新派生case-set version准备qa-review；⑦W0/W1均对qa-test-a准备（目标模板保持不变，只增加未选目录内容）；⑧同阶段product-review采用默认全部范围与明确--scope REQ-001准备。每份请求均带其R/I/RUN，gate用--gate，action用--action和适用mode/scope。配对状态除目标输入及实际公开操作产生的必要派生项外相同，保持正常额度；先核对实际字段差异再命名比较，不宣称未观测到的单字段控制。
4. prompt维度：在W0/W2分别对qa-test-a执行真实prepare-gate。保存目标模板的精确实际变更、两个公开catalog revision和目标模板revision，以及其余绑定；要求签名随这个合法模板输入变化。若目录revision同时变化，就如实记录“模板变更联动目录revision”，不强填旧catalog、不寻求hash碰撞。W0/W1另有模板不变的目录单独变化对照，不能把后者替代模板变更调用。
5. type维度：在W0的独立合法阶段fixture中，一份用prepare-action --action product-review，一份用prepare-gate --gate product-review；两者目标ID与测试模板正文对应，分别具备该动作/门所需的正常阶段和输入。保存公开type、模板revision及ActionInput的实际值；若类型变化联动模板身份或动作输入编码，如实列出，不伪造一种正常产品没有的第三类型来固定派生值。实际两次调用的签名必须不同；类型与其他字段没有合法独立输入通道时，不宣称已完成type-only内部因果证明。
6. 每份有效输入先实际prepare一次，再在该输入仍为OPEN、权威绑定未变时重复完全相同请求，比较dispatch/signature及派发/attempt数；再用原输入的未终结独立fixture作返回原输入对照，不能恢复已终结状态。正常新义务仍按原有限attempt和适用授权计算，不因测试配对重置额度。不存在新计划或身份变化时不手改TaskKey。
7. E保存逐维表：真实改变的用户/文件输入、全部权威字段前后值、签名、dispatch、额度、安装回执、argv及输出。合法配对下签名未变、相同输入增加重复派发、或前置完整的待测prepare被拒均可记录实际FAIL。源码中是否独立把两个联动字段分别加入签名不能由这种黑盒配对证明；对此明确标注证据边界，不以执行者的源码分析代替观测，也不降低AC-044“signature覆盖全部所列身份”的要求；其独立结构证明由主代理核对既有白盒覆盖，本例不另开任务。
oracle: 每个合法权威输入变化配对都得到不同dispatch signature；完全相同且仍有效的OPEN输入重复准备返回同一dispatch/signature，派发与attempt计数不增加；实际新输入遵守现有额度和授权。公开操作造成的必然派生字段联动必须完整展示，不能要求或伪造非法的type-only/prompt-only状态。模板变更联动catalog、type变更联动模板/动作输入时，判据仍要求该合法输入变化使签名改变，但结果只证明这个公开输入组合的行为，不声称证明某个内部字段独立参与算法；AC-044的完整字段覆盖要求不删除。前置成立后的产品拒绝或错误去重按实际观察判FAIL，不能以尚未完成不可能的单字段隔离为由记未执行。
primary AC bindings: AC-044
review status: PASS

CASE-034
mode: blackbox
description: 安装包文档、提示词和公开 CLI 对收敛规则一致，项目 AGENTS 不复制流程实现，且不出现 legacy 累计限额。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：复用整组一次候选安装；对文档所需示例状态直接构造三个独立fixture：P2/P3结果待记录、普通修复后的AFFECTED/最终缺FULL、产品审PASS后的preserved/changed候选。全部确认/审查历史模拟，P2/P3输入固定。操作：实际package validate --root I、workflow --help、prepare-action、prepare-gate，并在小fixture真实执行一次结果记录、scope准备和requirement影响登记。只读指定SKILL/formal-flow/README/README_EN/action和gate prompts/AGENTS，按原oracle逐规则对照实际入口输出；不用完整正式开发流程生成文档示例，不运行既有回归套件。
oracle: 公共行为与所有文档无冲突；稳定规则各有唯一人类可读 canonical owner，prompt 只保留代理执行所需内容，AGENTS 仅保留项目事实纪律/边界/引用。帮助和状态中没有本批非目标的 legacy 累计 token/时间/派发上限。
primary AC bindings: AC-045, AC-046, AC-048
review status: PASS

CASE-035
mode: blackbox
description: 稳定 owner 来源只统计 workflow start 到 Seal/Abort 的本 run 用量，旧状态仍可读取。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备：Seal、Abort、旧状态各用独立R/RUN/E。终态授权和需求确认固定为TEST-CONSTRUCTED fixture事件，不询问真人。Seal/Abort使用claude-code provider及不同SESSION/T，T启动前只有一行：
{"type":"assistant","uuid":"case035-before-start","message":{"role":"assistant","usage":{"input_tokens":1000,"output_tokens":200,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
实际start前，以R为cwd经BIN hook decide --provider claude-code提交PreToolUse JSON，字段hook_event_name=PreToolUse、cwd=R、session_id=SESSION、transcript_path=T、tool_name=Bash、tool_input.command为马上实际执行的完整workflow start命令。随后真实执行该start：--flow formal --vcs git --route lightweight --requirement requirement.md；保存owner来源与1000/200启动基线。此start/来源捕获是被测操作，不能预写owner结果。通过前置装配只补lightweight需求已确认状态，保留真实start生成的owner字段不动。向T实际追加一行uuid=case035-after-start、同格式usage input_tokens=70/output_tokens=30/cache字段0；不重写首行。分别真实seal与abort --user-confirm，保留输出和终态报告，比较owner差值。旧状态分支直接手工构造正常旧schema的run：保留旧格式有效字段、需求/action/gate/QA已知哨兵，省略本次新增owner启动元数据，按该旧格式序列化及完整性规则落盘；无需构建旧产品或真实跑旧流程。实际用候选show/resume读取此旧格式fixture，比较原投影。模拟transcript只能作为真实parser/start/终态计算的输入，70/30不能写进输出。
oracle: Seal/Abort两个分支的实际start均绑定各自公开hook输入的T/claude-code，解析基线input=1000/output=200，启动时未完成。实际终态命令退出零，cost.owner.source=transcript，totalInputTokens=70、inputCacheMissTokens=70、inputCacheHitTokens=0、outputTokens=30；不是1070/230、1000/200、0/0或unavailable。来源/边界与输入两行可核对，Owner与dispatch字段分开。直接构造的正常旧schema、缺新增owner启动元数据的fixture可被候选show/resume读取，无缺字段或反序列化错误，原需求/action/gate/QA哨兵投影保留，不因旧格式变FAIL；正常catalog delta不算读取失败。fixture模拟确认和旧格式是准备材料，实际start/解析/终态差值/兼容读取才是执行证据。
primary AC bindings: AC-050
review status: PASS

CASE-036
mode: blackbox
description: owner 来源缺失、不可支持、截断重写或区间重叠时明确 UNAVAILABLE。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备：六种独立owner fixture，终态其他条件均满足。①缺启动来源基线；②启动时来源有效，终态前移走该fixture的T；③provider为候选明确不支持的测试值；④T启动后截短为原字节前缀；⑤T同路径改写原消息内容且保留合法JSON；⑥两个活动测试run明确共用T且启动/终态区间重叠。使用固定TEST-CONSTRUCTED usage JSON构造输入与历史基线，不复制真实账户transcript；涉及启动后变化时用真实start取得基线后只改隔离T。操作：分别真实seal/abort，保存前后T、来源绑定及实际cost.owner，检查UNAVAILABLE标记、零数值及既有结果未受影响；每个失效来源由该分支实际输入与前后T证明，若产品另有原因输出则一并保存；不改写输出数字。
oracle: 每种情况Owner明确为UNAVAILABLE、数字为零，不写估算token，不把零值冒充实际零消耗，也不阻断或改写既有workflow结果；六种失效前置分别有输入/来源绑定与真实终态调用证据。AC-051未规定另设原因字段或固定错误文案，故单独缺少这种附加诊断不判FAIL；UNAVAILABLE标记缺失、数值非零或错误记成可用的零消耗仍判FAIL。
primary AC bindings: AC-051
review status: PASS

CASE-037
mode: blackbox
description: owner 终态计量幂等且在单一 RunCost 中与 dispatch 各计一次。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备：独立FINAL fixture，owner有明确同一T启动解析基线和可计算终态增量70/30，两条dispatch各为固定TEST-CONSTRUCTED已记录成本，分别input/output=11/3、17/5，终态前原始action/gate/QA/dispatch投影留存。操作：实际seal或abort并保存返回报告；随后重复同一终态命令及候选公开resume/show路径。状态正常被清理后，以原终态报告及命令的已终结/不存在返回核对，不重新造run使它重复累计。检查Owner独立、总量98/38、各来源恰一次、原语义结果不变。所有数字作为可解析输入及历史前置，不能先写入待测终态合计。
oracle: Owner 是独立可选字段，不伪装成 dispatch；顶层总数恰等于各 dispatch 与本 run owner 各一次。重复终态不再累计，dispatch 明细和所有 action/gate/QA 最终结果逐字不变，没有第二账本或 child/master owner 汇总。
primary AC bindings: AC-052, AC-053
review status: PASS

CASE-038
mode: blackbox
description: guard 对 0、1、N 个 TaskKey 及重复、新增和 retry 增量给出精确额度。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：公共只读分支。

准备：无需run/审查/安装写状态。按候选公开guard-preview JSON模板填齐schema、requirement、Batch/route/topology、definition、来源与有限retry声明；ID均为TEST-CONSTRUCTED。准备四份tasks：空集；单个K1/MaxAttempts1；K1/1,K2/2,K3/4；该三项加完全相同K2；另两份在三项上加K4/1或只将K3的4改5。操作：逐份实际运行从S构建的BIN workflow guard-preview --input <file>。分别机械比对(request,retry,total)=(0,0,0)、(1,0,1)、(3,4,7)、(3,4,7)、(4,4,8)、(3,5,8)。不运行已有guard测试，不在驱动里复制计算公式。
oracle: requestAllowance 等于 distinct TaskKey 数，retryAllowance 等于各 `MaxAttempts-1` 非负和，total 为二者之和；重复 TaskKey 不变，新增 TaskKey 只增一个基础槽位，单 task retry 加一只增一个 retry slot。
primary AC bindings: AC-055, AC-056
review status: PASS

CASE-039
mode: blackbox
description: guard 使用默认 MaxAttempts=2，并拒绝非法或冲突的 retry/provenance。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：公共只读分支。

准备：无需workflow前序。从公开完整合法basis模板制作5份输入：K1省略MaxAttempts；K1=0；K1=-1；同K1分别声明MaxAttempts1和2；同K1和相同retry但两个不同provenance。其余身份、schema字段全有效，文件名对应变体。操作：逐个真实guard-preview --input，保存原JSON、退出码与输出。首个机械比对规范化MaxAttempts2及allowance1/1/2，其余检查具体TaskKey/字段冲突拒绝。provenance全部是合成输入，不用真实代理证明。
oracle: 省略值规范化为 2，产生一个基础和一个 retry；所有非法/冲突输入非零退出，错误定位到对应 TaskKey 和字段，不输出成功额度。
primary AC bindings: AC-057
review status: PASS

CASE-040
mode: blackbox
description: canonical basis 的 digest 对相同输入稳定，对每类权威身份变化敏感。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：公共只读分支。

准备：无需run。按公开basis模板填写一份所有身份完备的TEST-CONSTRUCTED basis，再用JSON格式化/键重排生成语义相同副本；为requirement、Batch、route、topology、closed-world definition、approved QA、selected gate、established obligation/repair、TaskKey provenance、retry policy各准备只改变该身份且其关联字段仍自洽的独立副本。操作：原输入和重排副本各真实运行guard-preview两次，再运行每个单维变化副本。比较返回budgetBasisDigest：前四次相同，各有效身份变化与基准不同。不得预计算待测digest或复制生产算法。
oracle: 语义相同输入得到完全相同 budgetBasisDigest；每一种权威身份的有效变化都产生不同 digest。
primary AC bindings: AC-058
review status: PASS

CASE-041
mode: blackbox
description: guard 拒绝不完整或未经允许的 basis source，重试只扩 retry、不扩基础 TaskKey。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：公共只读分支。

准备：无需正式run。按公开basis模板构造缺definition、缺confirmed revision声明、缺provenance、缺TaskKey身份、raw finding/result/receipt/resume作为非法增量source的独立输入。合法对照为同一个TaskKey的默认retry、仅MaxAttempts加1、真正换snapshot/binding/责任的新TaskKey，来源是已裁决且收齐wave的TEST-CONSTRUCTED obligation声明。操作：实际guard-preview --input所有输入，保存退出码/错误/allowance/digest及输入目录前后文件清单；低成本合法输入只验证纯核心的结构自洽，不创建前序wave或审查。原retry错误/追加attempt不会增加request slot，合法新身份才增加。
oracle: 所有非法 source 非零退出且不写文件/状态、不签发任务。错误重试保持一个 request slot并消耗 retry；追加 attempt 只增加 retry allowance并改变 digest；真正改变身份的新义务才增加基础槽位。
primary AC bindings: AC-059
review status: PASS

CASE-042
mode: blackbox
description: guard-preview 是正式、只读、非权威的生产计算入口。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：公共只读分支。

准备：无需run或模拟审查。依公开schema写一份完整合法basis.json，所有身份为可区分TEST-CONSTRUCTED字符串，retry有限；选择空临时工作目录。操作：从S真实构建的BIN运行workflow guard-preview --input basis.json，保存输入、stdout、退出码、前后文件清单；若目录原有独立测试state，保存其即时字节并cmp。检查规范化摘要、allowance、稳定digest和NOT_INSTALLED。输入身份均是结构合法、未创建对应真实run或真实确认事件的TEST-CONSTRUCTED值；记录命令在该输入上仍能只读成功，以验证非权威计算语义。若输出附有非权威说明一并保存，但不要求新增说明字段或固定自然语言句子。连续同输入运行一次验证稳定；不生成任何workflow状态前置。
oracle: 退出码为零，JSON包含规范化basis摘要、三类allowance、稳定digest与runtimeIntegration:"NOT_INSTALLED"；完整结构合法的合成身份无需对应真实run或真实确认事件也可完成计算。没有创建run/state/回执/任务，已有测试state字节不变，输出不得声称真实确认或运行时止损已生效。非权威成功含义由该公开行为及NOT_INSTALLED约束验证，AC-060/061未要求JSON另外重复一条固定自然语言免责声明；缺这条附加句子本身不判FAIL。
primary AC bindings: AC-060, AC-061, AC-063
review status: PASS

CASE-043
mode: blackbox
description: guard-preview 对不可读文件和各类无效输入返回可定位错误且零写入。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：公共只读分支。

准备：无需run。准备不存在路径、不能作为可读JSON文件的路径、破损JSON、错误schema version、缺身份、非法retry、冲突TaskKey七类输入；除文件/JSON错误分支外，其余仅改变目标字段，完整合法对照按公开模板填写。操作：分别实际guard-preview --input，记录退出码/stderr及前后测试目录清单/已有state字节；判据定位该文件或字段，无成功allowance/digest。不使用额外账户、修改真实文件权限或正式配置来制造不可读；只用临时fixture路径。
oracle: 每次均非零退出，错误指出文件或具体 schema/TaskKey 字段；不输出成功 allowance/digest，不创建或修改任何 run state。
primary AC bindings: AC-062
review status: PASS

CASE-044
mode: blackbox
description: engine 权威需求和方案完整承接 Operator adjudication 与收敛生命周期。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：一份完整合法TEST-CONSTRUCTED guard basis；一个正常可达且有已记录finding/problem/历史裁决的独立CLOSED fixture（F1 NEW→P1、F2 SAME_AS、F3 DISMISSED），全部前序审查与确认直接构造。操作：实际guard-preview与workflow show；随后逐份只读 refactor-plan/stage-3-requirements.md、final-implementation-draft.md、stage-3-5b-runtime-guardrails.md，按原oracle逐项核对公开入口、Operator字段与收敛生命周期。对文档记录路径/行号/条款，对命令保留实际输出，不真实制造一轮审查与返修历史。
oracle: 文档包含 guard-preview；Operator 保存 raw finding、逐条 rationale、范围/严重度/因果/复现、finding/problem ID、三类裁决及历史，Controller 只做机械校验。非 wave/wave 错误重试、白盒最终快照屏障、wave 收集、唯一返修、P2 carry、P3 advisory、QA 冻结/THAW、signature 和授权 digest 均有明确合同。
primary AC bindings: AC-065, AC-066
review status: PASS

CASE-045
mode: blackbox
description: 后续 engine Ask 与 guard basis 计划保持 Operator、用户和 Controller 的责任边界。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：A1中product-review已PASS，candidate把demo emit从alpha换行改为beta换行；完整base及确认来源是TEST-CONSTRUCTED，候选文本/Git文件真实存在。另写完整合法guard basis。操作：实际requirement-impact-preview并guard-preview，保存NOT_INSTALLED/UNAVAILABLE及影响预览；只读权威需求、最终方案、3.5b子计划和阶段4计划，逐项核对Operator、用户Ask、Controller、basis来源和两类UNAVAILABLE责任。仅预览不执行需求回退，不等待真实产品审PASS。
oracle: 开发后严重度/范围/根因/重复核实属于 Operator，仅分支不唯一时询问用户；meaning-preserved 无 Ask，meaning-changed 必须有绑定影响预览的 typed Ask。增量 basis 只接受已裁决且 wave 收齐后的 obligation/唯一返修 TaskKey；raw 结果不扩容。阶段 3.5b 固定 NOT_INSTALLED，阶段 4 才切权威投影，owner 与 token-cap 的 UNAVAILABLE 独立。
primary AC bindings: AC-067, AC-068, AC-089
review status: PASS

CASE-046
mode: blackbox
description: 公开阶段计划、route matrix 与只读预览保持 engine 单写者及阶段边界；本用例只承担公开行为和公开文档的黑盒覆盖，不冒充实现结构覆盖。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：公共只读分支。

准备：直接使用S的真实构建产物和公开schema的完整TEST-CONSTRUCTED basis，无workflow历史。操作：真实workflow --help与guard-preview，保存入口及NOT_INSTALLED。逐份只读 incremental-seal-plan.md、route-matrix.md、stage-3-5b-runtime-guardrails.md及阶段5–7条款。用git show B:<path>导出ADR-001/002与stage-records.md公开基线内容，cmp两份ADR，按基线字节长度比较stage-records前缀并核对仅追加一条实施验证完成后、最终确认/Seal前的checkpoint。继续只承担公开文档/入口覆盖；实现结构、已有测试和提前阶段4写路径由原白盒职责承担，不在本例新派任务。
oracle: 帮助文本公开 `guard-preview`，合法调用退出为零且 JSON 明确包含 `runtimeIntegration:"NOT_INSTALLED"`。增量计划与 route matrix 明确阶段 4 的只读 guard-preview、requirement-impact 用户确认迁移、engine 单一 writer，以及 legacy 写入口不得绕过 meaning-changed 确认；阶段 5–7 文档继承稳定 finding/problem ID、归并、每快照唯一返修、P3 advisory 和 QA 冻结语义。ADR-001/002 与基线字节完全一致；`stage-records.md` 的既有历史字节完全一致，且只追加一条处于 Batch A/B 实施与验证完成后、最终候选确认或 Seal 前的 checkpoint。本黑盒结果仅对上述公开行为与公开文档作结论，不把 AC-034/AC-035/AC-047/AC-054/AC-064/AC-078 或候选实现中的阶段 4 写路径记为已覆盖。
primary AC bindings: AC-069, AC-070
review status: PASS

CASE-047
mode: blackbox
description: 首次受理只确认唯一正式需求文件全文，附件不能成为第二需求源。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

全部仅在未来 QA Execution 的独立 R/RUN/E 执行。本例的 start 是被测操作，不能预先装配其输出。

临时仓库包含完整 requirement.md；acceptance-one.md 仅写 emit 输出示例，acceptance-two.md 仅写缺文件结果示例，不新增需求。真实提交后执行：
BIN workflow start --root R --package-root I --run-id RUN --flow formal --vcs git --split no --requirement requirement.md --requirement-artifact acceptance-one.md --requirement-artifact acceptance-two.md

读取 show，保存唯一 source、确认状态及附件绑定。执行 workflow requirement --source requirement.md 完整预检，取得 revision H；在 E 保存该文件全文、H 的展示记录及仅确认这份完整文件的 TEST-CONSTRUCTED 用户事件。按公共协议只补齐澄清 PASS 等前置，不预置或改写被测 source、确认状态及附件投影。

执行 workflow requirement --confirmed，随后执行 workflow prepare-action --action product-review；两条命令均带同一 R/I/RUN。保存完整 argv、退出码、stdout/stderr、前后 show 和规范 prompt。全程不提供附件独立确认，不把附件路径传给 --source。
oracle: 合法 start、完整预检、canonical 全文确认及后续产品审准备成功；当前唯一 requirement source 始终为 requirement.md，当前确认 revision 精确为 H。两个附件保留验收产物身份，不产生第二个当前需求源、独立需求确认状态或额外确认前置。附件各自的产物 revision 不等于独立需求确认。出现第二源并存、附件自动晋升为 source、要求另行确认附件，或首次确认仅绑定摘要/delta，均为失败。参数、身份等无关拒绝不能充当唯一源约束通过的证据。
primary AC bindings: AC-071
review status: PASS

CASE-048
mode: blackbox
description: 审查后 review-delta 基于编辑前精确 base，穷尽展示多 finding 的新增、删除、修改和移动。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：装配已确认base的A1/技术审阶段，E保存编辑前规范化完整base并核对实际revision。直接放入两个TEST-CONSTRUCTED且已CONFIRM的来源finding：F1要求修正输出示例/验收，F2要求重排章节并删除被新例替代的重复说明；二者合计授权全部变动。base含旧输出示例、重复说明段和位于方案前的使用步骤；candidate改示例、增加对应AC说明、删除重复段、将使用步骤移到方案后，保证新增/删除/修改/移动均有实际diff。操作：执行者真实比较完整base/candidate，逐hunk填来源action/ID/CONFIRM、REQ/AC或章节、影响、verification；按公开review-delta JSON模板真实review-delta-preview取得绑定，再用明确覆盖完整delta的TEST-CONSTRUCTED确认调用requirement --review-delta。保存实际diff、包、preview和show，检查无遗漏。无需真实审查碰巧提出两个指定等级finding。
oracle: 全部实际 hunks 及其必要上下文与变更包一一对应，来源覆盖无遗漏；重新确认只要求完整 delta，未变全文继承。CLI 回执绑定精确 base/candidate、digest、sources 和逐 hunk 核实，最终仍是同一完整 candidate/唯一需求源；base 保存与 diff 生成的责任及证据归于主代理。
primary AC bindings: AC-072
review status: PASS

CASE-049
mode: blackbox
description: review-delta 回执精确绑定且在候选、base、finding 或核实变化时失效。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：四个独立review-driven fixture与完整对照。每个直接装配一个已CONFIRM的TEST-CONSTRUCTED来源finding、已确认完整base，执行者保存base并制作合法candidate/hunks。实际review-delta-preview得到绑定，模拟用户确认同一包。四分支随后分别只改变candidate真实字节、通过合法requirement推进base、通过公开settle-findings/纠错改变来源有效处置、改变包内一条verification。操作：每次前置变动完成后保存即时state，用旧包/digest实际requirement --review-delta，检查具体过期绑定错误及cmp零写入。完整对照无变化，真实应用一次并show。不得靠破损JSON或未知字段制造拒绝，也不等待真人决定来源finding。
oracle: 合法回执绑定原例要求的 base/candidate、delta digest、来源 finding 集合、逐 hunk 核实与用户确认来源，state 不持有 base 全文/diff 或第二需求源。各过期调用非零且指向实际绑定失效，被测调用即时前后的权威状态无部分写入；前置、语法或额度错误不计作本例通过。
primary AC bindings: AC-073
review status: PASS

CASE-050
mode: blackbox
description: 增量确认对完整候选执行同等预检，并在缺失可信 base 或逐 hunk 核实时拒绝。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：各分支直接构造同一review-driven阶段、模拟已CONFIRM来源finding及完整合法candidate。合法对照保留精确base，逐hunk核实；负向①只缺一条verification，②candidate删必需AC且重新生成实际文件绑定，③base未保存且未进VCS、仅留REQ/AC摘要，④保留全文与base revision不匹配，⑤恢复来源明确不能证明可信。操作：合法与①②真实review-delta-preview/requirement --review-delta，记录候选结构/绑定目标结果；合法后实际prepare下一动作读取完整candidate。③④⑤执行者实际检查已有材料、比对可获得全文与revision；在不能得到精确可信base时不生成声称核实的包，不模拟其通过。这里检查host应停止的责任，不要求CLI恢复旧全文，也不询问真实用户补虚拟确认。
oracle: 合法路径预检完整 candidate、冻结完整 revision/envelope，后续动作读取完整新文件。缺 verification、候选结构非法的请求在对应公开校验处被拒；无法证明完整可信 base 的 host 路径不得产生声称已核实的 delta 或用户确认。摘要、REQ/AC 投影和猜测不能代替原文；这些责任由实际处理主体分别验证，不以 CLI 自动语义判断未提供全文为判据。
primary AC bindings: AC-074
review status: PASS

CASE-051
mode: blackbox
description: 与已处置 finding 无关的 hunk 不能混入 review-delta，主动查看全文不扩大确认范围。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：review-driven fixture含一个已CONFIRM的TEST-CONSTRUCTED来源F1，仅授权修改demo输出说明；真实candidate另加与F1无关的新功能hunk。保存真实base和全量diff；模拟用户事件另明确要求查看candidate全文。操作：执行者逐hunk核对来源，实际定位额外功能无授权，移除它后重新生成完整delta/绑定；按模拟查看请求展示候选文本到E，再以同一delta scope的TEST-CONSTRUCTED确认真实requirement --review-delta。只读原例指定SKILL/formal-flow/prompts/README/engine计划，核对host/CLI边界。不得让真实模型或真人为虚拟hunk做决定。
oracle: 无关 hunk 由主代理排除或另行对齐，本例撤回后最终包只包含有已确认来源的改动；不得把未核实的来源关系交给 CLI 自动猜测。自愿展示全文不扩大正式确认范围，回执仍绑定当前完整 delta 和两个 revision。各文档采用相同的 host/CLI 责任边界。
primary AC bindings: AC-075
review status: PASS

CASE-052
mode: blackbox
description: 未处置 finding 投影始终显示稳定 ID、当前等级、逐条理由和消息，并保留重定级历史。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：product-review与start-readiness各一个正确阶段fixture，未记录当前结果。固定模拟报告各含3条不同ID/P1/P2/P3 finding，逐条不同message和rationale，技术审补分类。操作：通过真实record-action登记，再show逐项核对ID、等级、理由、message。对其中一条按公开重定级/纠正入口提交TEST-CONSTRUCTED新裁决及完整新理由，重新show并比较当前投影和历史。若P1需要独立后果核实，使用可定位fixture需求矛盾；不等待真实审查产生三个等级。
oracle: 每项都独立含 finding ID、当前有效 severity、severity rationale 和 message；重定级后显示新等级及新理由，同时保留旧等级、理由和裁决审计。
primary AC bindings: AC-076
review status: PASS

CASE-053
mode: blackbox
description: finding 处置前的 canonical host 规则要求同一决策消息完整展示 ID、等级、逐条理由和摘要。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：一个最小可读取的A0 fixture即可；如展示finding投影有助定位，可直接构造TEST-CONSTRUCTED记录，不要求特定数量或等级。操作：真实workflow show保存可读结果，直接读取候选SKILL.md canonical finding处置规则和formal-flow settle-findings段，逐项保存路径/行号/原文。核对同一决策消息的ID、等级、独立理由、摘要与分组无歧义，以及漏项后需补齐并重新取得决定。验收对象是文档规则，不生成模拟聊天再派模型评判；本条仅判断文档条款，实际show仅作为公开入口可读观测。
oracle: 文档允许无歧义分组但要求每个 ID 映射到等级、独立理由和摘要；任何漏项后的用户回复明确不可作为授权，必须完整重述后再决定。该规则不新增 finding 账本，也不声称 CLI 推断自然语言展示质量。
primary AC bindings: AC-077
review status: PASS

CASE-054
mode: blackbox
description: P0/P1 severity rationale 必须逐 finding 独立存在，整批套话、item FAIL 或“违反需求”不合格。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture、固定阻塞载荷。

准备：四个独立首attempt fixture，固定有两个不同TEST-CONSTRUCTED P1：F1对应TSV末行遗漏，F2对应另一脚本输出带标签而非要求整数；需求、文件、正常路径及各自rationale分别完整，不复制F1冒充F2。先实际运行两段小脚本为证据字段取得输出。目标坏输入仅为①F2缺rationale，②每条仅复述严重度定义，③每条仅写item FAIL，④每条仅写违反需求。操作：按所属record-action/record-gate真实入口每个fixture提交一次，逐--finding分组紧跟自己的severity/rationale/证据/verify字段，所有其他字段有效。校验针对具体rationale的rejected receipt，不用额度耗尽算第3/4支通过。两条P1报告直接构造，不等待审查结果的数量。
oracle: 所有变体均非零退出，只形成对应 rejected receipt，不产生有效 finding 或 action/gate FAIL；错误指出具体缺少独立 rationale 的 finding。 四种分支均须到达对应 rationale 校验；参数解析、身份、快照或 retry 用尽的错误不算本例通过。
primary AC bindings: AC-079
review status: PASS

CASE-055
mode: blackbox
description: 合格 P1/P0 理由必须证明正常路径后果和等级阈值，开发前不得虚构实现错误。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture、固定阻塞载荷。

准备：product-review、start-readiness、开发后G各自独立fixture，固定TEST-CONSTRUCTED P1，理由具体到已确认需求/正常路径/后果/为何P2不足。开发前用“同一stdout同时要求两个互斥精确值”的需求或“核心验收无入口”的方案作为可核对材料；开发后用实际运行会漏TSV末行的错误脚本。另准备P0对照，其唯一核心验收按fixture约束没有任何可行路径并给出逐条证明，不以item FAIL代替。操作：真实record-action或record-gate提交完整结果并show；start-readiness补合法分类，开发前不填actual-error，开发后填真实观察。检查原oracle的持久化和阶段字段边界。
oracle: 完整理由可记录并在 show 中逐项绑定 finding/revision/dispatch/evidence；P0 具有额外阈值证明。开发前结果不会因没有实现错误字段被拒，也不会生成虚构字段。
primary AC bindings: AC-080
review status: PASS

CASE-056
mode: blackbox
description: record-action、QA Review 集合 finding 和 record-gate 都拒绝缺失定级理由，QA execution 逐用例 FAIL 不受该字段约束。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture、固定阻塞载荷。

准备：四类记录入口各两个独立首attempt fixture：product-review、start-readiness、qa-review集合P1、开发后G P1；固定模拟P1分别省略rationale或仅写“违反需求”，其余合同含QA完整映射/技术分类/门actual-error与compared均有效。每份派发前序和生命周期直接构造。操作：通过对应真实record-action/qa-review/record-gate提交一次，检查目标receipt并至多实际prepare一次默认retry。QA execution对照另用已批准Q1前置，Q1要求demo输出alpha换行而真实脚本输出beta换行；真实运行脚本后经qa-execution记录procedure/实际observation/oracle-result=FAIL，不填severity rationale。不运行交付已有测试或预写观察。
oracle: 四类 severity finding 均只形成 rejected receipt并按同 TaskKey有限重试；不写有效 FAIL/wave/problem/basis。QA execution 的真实逐用例 FAIL 可正常记录，不被错误要求 severity rationale。
primary AC bindings: AC-081
review status: PASS

CASE-057
mode: blackbox
description: 产品审 obligation-to-AC item FAIL 不自动升级 P1或使总体 FAIL。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：两个A0产品审fixture。A的固定报告有一个obligation item FAIL，对应非核心说明缺口，完整后果仅P2；B的同类缺口明确使唯一核心验收没有可执行路径，固定P1并附该条符合AC-080的理由。items来源于各自实际prepare的规范ID，原报告和模拟身份均留E。操作：实际record-action --action product-review提交item-status FAIL及对应finding等级/rationale，八项operator-check和证据按本次fixture填写。show比较A总体PASS加advisory与B总体FAIL，不让item FAIL自动代替等级推导，也不派真实产品审。
oracle: 首个结果记录为 PASS 加 P2/P3 advisory，不生成 P1 blocker；对照项只有在完整 P1 理由成立时才使 product-review FAIL。
primary AC bindings: AC-082
review status: PASS

CASE-058
mode: blackbox
description: 主代理必须逐条核实每个 P0/P1 rationale，不能用一份批量 operator-evidence 替代。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture、固定阻塞载荷。

准备：两个DEV+WAVE/G首attempt fixture，各有固定两个独立TEST-CONSTRUCTED P1报告：F1=TSV末行遗漏，F2=整数输出格式错误；分别真实运行小脚本并填写需求/正常路径/错误/位置/延后影响及独立rationale。操作：负向record-gate --status FAIL中，F1组填四个--verify-*并在文字笼统声称核实两项，F2组完全不填自己的核实；不传不存在的record-gate --operator-evidence。完整对照另一个fixture给F1/F2分别填四项独立核实并实际记录/show。检查F2缺核实的receipt与对照完整持久化。只读原例指定规则文档核对逐条责任；无需真实报告碰巧含两个P1。
oracle: 批量核实变体被拒且指出未逐项绑定；逐 finding 核实成功。所有人类规则和派发 prompt 对该要求一致。 负向判据只接受第二个目标 finding F2 缺独立核实的协议校验；unknown flag 等参数解析失败不是证据。
primary AC bindings: AC-083
review status: PASS

CASE-059
mode: blackbox
description: meaning-preserved 不需要回退授权，保留产品审 PASS，但需求正文仍须确认 delta。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：A1中product-review已PASS，start-readiness为未完成或FAIL，固定一个已CONFIRM的TEST-CONSTRUCTED技术finding，只要求等价技术方案说明修订。保存精确base后真实改candidate，生成与该来源绑定的完整delta但尚未确认。操作：先实际requirement --meaning preserved尝试缺正文确认，保存拒绝；再真实review-delta-preview，用TEST-CONSTRUCTED delta确认通过requirement --meaning preserved --review-delta登记，不提供独立impact回退授权。比较产品审PASS、受影响技术审状态、轮次和授权；不向用户再次确认虚拟项目。
oracle: 未确认正文变化时登记被拒；确认后无需独立 impact 授权，产品审 PASS 保留，仅实际受影响且未通过/未完成的 start-readiness 状态回到待做，额度和授权不增加。
primary AC bindings: AC-084
review status: PASS

CASE-060
mode: blackbox
description: meaning-changed 影响预览完整且完全只读。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：直接构造已确认base且product-review PASS的A1；用实际文件形成四种candidate变化：目标、范围、外部行为、验收标准，各自内容具体且只有本例要展示的影响。来源为新TEST-CONSTRUCTED用户需求，不造review-driven finding。操作：真实requirement-impact-preview --source requirement.md并填公开四类影响参数中的对应值；记录base/candidate、失效PASS、回退步骤、额度、digest/state revision，cmp即时前后权威state。这里只生成预览，不提前写changed或授权。
oracle: 预览包含 base/candidate revision、具体含义变化、将作废的 PASS、回退位置、产品审已用/剩余额度及适用授权状态，并返回 digest；运行不修改任何 state、结果、额度或 requirement revision。
primary AC bindings: AC-085
review status: PASS

CASE-061
mode: blackbox
description: meaning-changed 只有精确绑定的用户确认才能原子回退，过期或缺失确认零状态变化。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：独立A1 fixture，product-review PASS直接构造；变更为普通来源新TEST-CONSTRUCTED需求把demo emit stdout从alpha换行改beta换行，没有来源finding/review-delta。真实编辑candidate，并实际requirement-impact-preview --source requirement.md --external-behavior-impact 'demo emit stdout从alpha换行改beta换行，回退产品审'。各负向独立fixture：缺用户确认、只有operator建议、错误digest、错误stateRevision、base已合法推进、candidate预览后变化、其他合法状态变化；变化由对应公共命令或候选文件编辑真实完成。操作：绑定即时前后状态，真实requirement --meaning changed --source requirement.md及公开--impact-preview-digest/--impact-state-revision/--impact-user-confirmation/同影响字段申请一次。合法对照用精确覆盖当前preview/digest/revision/回退后果的TEST-CONSTRUCTED确认，先按普通来源登记changed（不带--confirmed/--review-delta），再用模拟确认完成后续需求澄清。核对原PASS失效、product-review PENDING与额度不增加。无需真人交互或真实产品审。
oracle: 所有无效绑定均非零退出且字节级保持权威状态不变。合法确认原子登记 candidate、作废旧产品语义的 PASS、把 product-review 置 PENDING并失效依赖结果；不重置/增加额度，也不自动签发新产品审。 普通来源路径不应因缺 review-delta 或来源 finding 而被迫改走审查修订；本例不将它替换成仅有 review-driven 的路径。
primary AC bindings: AC-086
review status: PASS

CASE-062
mode: blackbox
description: 一份明确确认可同时覆盖完整 review-delta 和 meaning-changed 回退后果，但仅确认文字变化不足以回退。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：两份独立review-driven A1 fixture，均直接构造已CONFIRM的TEST-CONSTRUCTED来源finding和原产品审PASS；保存实际base、编辑candidate、生成完整hunks并逐项核实。A在第一次delta preview前设confirmationScope=REVIEW_DELTA_AND_REQUIREMENT_IMPACT，填真实影响；B设REVIEW_DELTA，只提供文本变化确认。操作：A真实review-delta-preview后以其digest调用requirement-impact-preview --review-delta-digest，给一次TEST-CONSTRUCTED确认同时覆盖完整delta和回退后果，将确认来源和impactPreviewDigest写入公开包，再真实requirement --meaning changed --confirmed --review-delta confirmed.json --impact-preview-digest P --impact-state-revision V及相同影响字段。B以只有文本scope的包申请changed，保留拒绝。两份不共用确认，不要求真实审查给指定等级或真人回答两次。
oracle: 首个 fixture 一次完成正文确认和合法回退，不要求第二次确认；第二个只完成或保留文本确认，meaning-changed 状态转换被拒。
primary AC bindings: AC-087
review status: PASS

CASE-063
mode: blackbox
description: CLI 不自动判断需求语义变化，主代理建议不能冒充用户决定。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）

准备选用：默认小fixture。

准备：两份直接构造产品审PASS的独立fixture，candidate文字变化相近；A只改等价技术说明，B改一项正常输出含义。按真实文件保留完整base/candidate；若来源为模拟已CONFIRM finding就附其合法delta，普通新需求不强制review-driven。操作：A由执行者显式requirement --meaning preserved并提供该来源必要的TEST-CONSTRUCTED正文确认，不提供独立回退授权。B显式建议changed并真实生成impact preview，先仅用operator建议申请changed、观察不转换；随后用精确绑定的TEST-CONSTRUCTED用户回退确认完成公开登记。只读原指定CLI输出/SKILL/formal-flow/prompts/engine计划核对建议与决定责任。两种选择预先明确，不测试需求未规定的省略--meaning默认行为，不让真实用户替虚拟项目选分支。
oracle: CLI 不根据自然语言 hash 或文本相似度自行选择分支；preserved 由主代理显式登记，changed 在用户授权前不转换。文档始终区分主代理建议与用户决定。
primary AC bindings: AC-088
review status: PASS

CASE-147
mode: blackbox
description: 现有用例全部PASS但缺少核心emit验收覆盖时，合格集合级P1使QA Review FAIL；补齐覆盖后仍须重新审查。
procedure: 本用例集CASE-001的公共准备协议（仅准备规则，不执行CASE-001测试）。只选默认小fixture，不选固定阻塞载荷；使用独立R/RUN/E。本条独立验证集合级P1，所有已有用例本身均PASS。

1. 装配DEV及QA(blackbox)设计已提交、review尚未记录的前置。唯一真实requirement.md仍完整包含REQ-001和AC-001/002/003；设计集合只放Q2、Q3：Q2通过python3 demo.py missing <确实不存在的路径>验收非零退出且stdout为空，绑定AC-002；Q3读取README验收固定字符串Demo，绑定AC-003。不放Q1，不放任何调用demo emit或主/附加绑定AC-001的其他用例。保存实际完整case-set、source/point/case映射、批准名单的准备清单和版本V0；不能沿用QA默认三用例的完整覆盖集合。AC-001 仍存在于已确认完整需求中，但当前候选只有 Q2→AC-002、Q3→AC-003，AC-001 属于未绑定验收点。通过 DR 指向的完整需求核实其存在，通过候选映射核实其未绑定，不要求它同时属于 point-decision 集合。不能通过删除需求规避覆盖缺口。
2. 实际执行BIN workflow prepare-action --root R --package-root I --run-id RUN --action qa-review --mode blackbox，取得DR并读取其完整候选用例与需求；通过 DR 指向的完整需求核实 AC-001 的存在，通过候选映射核实当前候选只有 Q2→AC-002、Q3→AC-003，AC-001 未绑定，不要求它同时属于 point-decision 集合。按公共协议准备DR的TEST-CONSTRUCTED完成身份及配对生命周期。固定review输入：Q2/Q3逐用例PASS；case-decision二者PASS；--source-decision REQ-001=FAIL；--point-decision AC-002=PASS；--point-decision AC-003=PASS；--unbound-point AC-001。集合finding为“TEST-CONSTRUCTED：当前只有missing和README用例，核心emit精确输出AC-001没有合法批准覆盖”，severity=P1。
3. 为这一条集合finding单独给出severity rationale：“REQ-001/AC-001要求正常demo emit输出alpha加LF；当前批准候选只有Q2缺文件和Q3文档检查，QA execution不会调用emit，因而即使emit返回beta也无法由当前集合区分，核心公开输出验收没有可执行验证路径，无法据此冻结并声明需求已通过；仅作为P2建议保留该缺口不足以完成本fixture的核心验收。”引用实际requirement.md中AC-001的存在行及本次DR候选清单，按DR合同逐条填写范围、等级阈值和覆盖事实的独立核实；不虚构当前实现真的输出beta，也不填写开发后gate actual-error。
4. 实际运行BIN workflow qa-review --root R --package-root I --run-id RUN --dispatch DR，对Q2/Q3分别传--case <ID> --outcome PASS，传--source-decision REQ-001=FAIL、--point-decision AC-002=PASS、--point-decision AC-003=PASS、--unbound-point AC-001，并逐项传上述--case-decision，并将该条--finding、--severity P1、--severity-rationale与其合同要求的引用/独立核实成组提交。保存完整argv、原输入、退出码和show。只接受该合格集合finding被正常登记后的QA Review FAIL；身份、映射参数、引用、rationale不合格形成的REJECTED_INVALID_RESULT不是本例目标结果。
5. 实际prepare-action --action qa-execution --mode blackbox应因该mode未通过review/未冻结而拒绝。然后真实prepare-action --action qa-design --mode blackbox取得DD，以qa-design --dispatch DD --case 'demo emit核心输出' --mode blackbox --procedure '在R执行python3 demo.py emit并保存argv、exit、stdout/stderr' --oracle 'exit0，stdout精确alpha加一个LF，stderr为空' --ac AC-001新增用例，不传--case-id，由CLI分配新ID。全部命令带同R/I/RUN，设计生命周期按公共协议提供；Q2/Q3原定义及其PASS保留。
6. 保存增量后的完整映射和V1，实际prepare-action --action qa-review --mode blackbox并读取新DR2，确认新emit用例和AC-001覆盖已进入本轮完整集合。到DR2准备成功为止，尚未取得新审查结果不算FROZEN或PASS。无需真实审查碰巧给出P1，不运行交付已有测试。
oracle: 初始完整候选集合恰为Q2/Q3且均无主/附加AC-001绑定，AC-001仍是已确认需求。合格qa-review记录被接受，Q2/Q3逐用例PASS与集合P1分别保留，mode总体FAIL、V0未FROZEN且qa-execution准备被拒；不是REJECTED_INVALID_RESULT。新增emit用例取得CLI分配的新ID并绑定AC-001，原Q2/Q3及其PASS保留；V1不同于V0，完整集合补齐AC-001/002/003，但新增用例尚待批准，必须准备绑定V1的新DR2且不得直接冻结或把旧FAIL改成PASS。

合格提交退出0，覆盖记录的未绑定点精确包含该 mode 的 AC-001（公开规范化表示为 BLACKBOX::AC-001）；原提交的集合 P1 被有效保存，不能以另一个附加 finding 替代。Q2/Q3 均 PASS、mode FAIL、V0 未冻结、执行准备被拒，以及补齐 emit 覆盖后仍须生成 V1 并准备新 DR2 的要求全部保留。协议拒绝不算有效集合 FAIL；前置和完整合同成立后仍拒绝上述提交，应记录实际失败。
primary AC bindings: AC-023
review status: PASS

