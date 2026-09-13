# QA 设计

为冻结前可达性检查写清每步所需前态、产生后态及取证入口，区分被验收目标与可简化前置。公共操作按当前任务已绑定的材料核对依赖、参数和适用版本；候选与身份来自当前绑定。静态检查只解析，不导入或 source 会启动任务的脚本。状态前置冲突、必要精确接口或取证缺口沿现有增量修订解决，执行者不得自行改判据。

从已确认需求出发，为已选 QA 模式设计用例集。两种模式各自可选、均由独立代理执行，
时序按模式而定：

- **黑盒**：真实 QA。不关心代码是什么；只从「产品是什么、怎么用、需求是什么」出发，
  通过实际使用产品（文档化的正常使用方式操作产品、覆盖常见操作失误）验证需求是否完成
  并通过。黑盒设计在 QA 隔离工作区中进行：工作区从基线快照创建、恒等于基线、始终不含
  本次开发代码，设计阶段无可运行产品可"实际使用产品"——用例以注入到隔离工作区的**当前
  需求文档**为准（不要以基线产品文档为准，后者仍含旧时序与旧表述）；"实际使用产品"是
  执行阶段的描述：快照后对主工作区已构建产品执行。
- **白盒**：开发后由独立代理读实现设计面向实现结构的测试（单元、系统、集成等职责式定
  义，不限定具体类型），在指定的白盒隔离工作区进行。结构测试充分性依赖实现，白盒设计阶段读
  取实现；开发前无实现可读，白盒设计只在开发后进行。

本次新增或修改的产品入口在开发前按已确认接口契约设计步骤，不要求基线帮助已包含
该入口；测试准备可构造的边界见下文，被测功能与执行证据仍须在快照后真实运行取得。

白盒用例先独立确定应验证的行为、分支和 oracle，再检查已有测试，复用真正有效的覆盖。
每条白盒用例用 testRef 定位实际测试（可引用独立子测试）；引用可用项目适用的测试标识，
例如 `<文件路径>::<函数名>`，不规定语言或解析格式。CLI 校验引用非空且唯一，QA Review
检查输入、断言和对应性，QA Execution 独立运行并保留输出。引擎不从 testRef 推断文件路径。
**用例集是否充分由 QA 审查（review）判断，不设机械化质量下限**——被选中模式零用例是
覆盖缺失，由 review 的 set-level 覆盖判定承担。

**测试准备可构造（黑盒与白盒共用）。** 低成本时优先真实链路；前置真实链路需要较高的
时间、token 或重复派发成本时，可以构造或手工准备。按具体用例判断，不设硬成本阈值或新
配置，不要求用户逐例授权，也不要求一律模拟或一律真实。可构造的材料包括测试环境、
fixture、输入、已有前置状态，以及模拟审查结果、授权、生命周期等测试数据。在用例既有
步骤中明确构造部分为隔离测试材料，写明构造方法及所代表的前置；测试准备不要求必须有公开
生成入口。选择构造时，无需为造数据真实跑前序产品审、技术审或开发流程、等待模型输出
特定等级，也无需重新要求用户确认虚拟测试项目。

- 构造前置后，被测操作或逻辑仍须实际运行并观察结果：黑盒通过文档化产品入口，白盒运行
  绑定的结构测试。不得把被测判断或输出预先写成预期值后声称通过。若验证的就是宿主真实
  集成、真实派发或身份捕获，必须取得相应真实观测，不能用构造输入或模拟身份冒充。
- 手工构造正常可达的前置状态不等于破坏状态，不能因此自动降级或否定发现；将不受支持的
  状态破坏当作产品应保证的新场景，仍受项目边界限制。输入与失败路径同样以已确认需求、
  文档化正常使用和常见操作失误为准。
- 构造仅用于隔离测试环境，不得篡改当前项目的正式 run，或将模拟材料冒充正式审查、授权
  或正式 PASS。测试材料与实际运行所得证据须明确区分。

**白盒先独立判断覆盖，再复用已有测试。** 先列出应验证的行为、关键分支和 oracle，再读取
已有测试的输入、真实断言与覆盖对应。有效测试显式绑定并独立运行；仅缺口或错误才新增、
修正。普通逻辑优先单测，跨模块用集成测试，公开路径由黑盒承担；交叉验证须有不同验证
目标。不能仅凭测试名、覆盖率或开发者声明放行。没有测试变动时不增加文件或推进快照；
有变动则由 adapter 提交并纳入最终含测试快照。

黑盒仍必须通过文档化产品入口实际使用产物，不能以既有测试套件为主体或主要证据；
独立的短驱动脚本可调用公共入口、采集真实输出并断言。

白盒 design.testFiles 必须明确列出全部测试专用文件及测试专用 fixture 的项目相对路径，
包括开发者编写、直接复用而没有改动的测试，以及本轮新增、修改、删除的测试文件。
只列精确文件路径，不列目录或 glob；生产文件、生产配置及生产共用输入即使被测试引用，
也不能列入。允许在混合文件内新增、修正测试内容，但不得修改生产行为、生产配置或共用输入；
混合文件列入实际变更 manifest，不列入 testFiles，仍保留在实现门 diff 中。没有项目内测试专用文件时
显式返回 []。这是待独立白盒 review 核对的声明，不能单凭作者声明排除文件。
顶层 manifest 只列实际新增、修改、删除的路径；零改动复用时 manifest 为 []，testFiles
仍列出复用的测试文件。两者不得混用，不为标记测试归属修改已有文件。

设计结果使用当前 schema 的 design（coverageJudgment、manifest、alternatives，白盒另含 testFiles）；manifest 中
包含 binding、sources、points、cases、edges、sharedSteps。所选模式合起来须覆盖全部 AC，
每个已覆盖 AC 的 pointId 为 REVIEWKIND:AC-ID，例如 BLACKBOX:AC-001；case ID 同样带
REVIEWKIND 前缀，并以 edge 连接需求来源和验收点。每个 case 有独立 oracle。

把行为主张写进描述；把文档化的入口、必要的前置条件、用户操作和需要留存的证据写进
步骤；把可观察的预期结果和失败信号写进预期判据。代码检视、模拟输出或开发者自测主
张都不能替代黑盒的实际使用执行。共同准备和操作在 manifest.sharedSteps 保存一份，case.sharedSteps 按 ID 引用；逐例保留特殊前提、操作差异、证据与 oracle。

**每一条黑盒用例都必须写出真实、完整、可照做的执行步骤。** 步骤必须给出实际使用
的公开入口、命令或调用、必要参数和输入、环境与 fixture 前置、操作顺序，以及需要留
存的输出或产物；不能只写场景名称、检查项、类别名或“执行相应测试”。设计阶段不能
运行产品，不等于可以省略执行细节；这些步骤必须能在快照后的真实产品上直接照做。

覆盖文档化的正常使用和常见操作失误，并逐验收点设计：正向生效、失败路径或不得
误报、边界/中间态、以及本次改动可能破坏的既有文档化正常使用行为的回归面——凡对应
不同可观察行为、或需要不同公开步骤才能验证的角度，都是独立用例，不得合并成一条顺带
带过，也不得只留正向路径。在直接拥有该行为的最低层测试每个行为，且要测得深、不止测得到：预期判据必须落到
可机械比对的具体观察——精确的消息片段、字节/摘要、列表内容与顺序、数值与边界——不
得用「行为正确/符合预期/正常工作」级别的空判据；失败信号必须能在真实执行中把对错区
分开。步骤必须具体到他人无需追问即可照做，不得以类别名或「相应测试/相应入口」的方
式含糊引用。每个验收点从典型输入、边界/空/极端、错误输入三个层面考虑；已有强判据用
例覆盖的层面不必重复。当既有
的直接自动化覆盖已经验证同一条确定性规则时，不要在更高层重复覆盖，除非更高层引入
了不同的正常使用行为。除非已确认需求明确要求，不要发明对抗性、改写内部状态、权限、
不可变文件或不受支持平台的用例；这里的改写内部状态指制造不受支持的状态破坏，不包括
上述隔离测试前置的构造。

QA Review 可选建议由 engine 按 suggestion ID 接受用户处置，不在本动作调用旧推进命令。
采纳改变验收含义时由 engine 修订并重审受影响部分；普通代码返修不解冻既有 case-set。

共同说明只设计、审查一份，但每条未批准用例仍须逐条判断；共同说明修改后所有引用者
重新检查。对 source、验收点、case 的双向映射完整对账，不抽样或按组背书。
一次简单检查直接执行，重复步骤才写短脚本。脚本限于准备、真实调用、取证和断言；核对
它与已批准用例一致且不会漏报失败。缺陷回归须能发现原错误；关键成功/失败判断用已知
错误样例校验，不递归为全部测试写测试；同版本未改脚本无需重新完整设计审批。

`FROZEN` case-set 不接受普通 QA Design。只有需求含义、对外行为、成功/失败判据或原用例的
稳定可执行性变化经编排层合法 THAW 后，才设计受影响用例；普通代码返修和一次性环境不可用
都不属于 THAW 理由。

## 冻结命令与模式执行

Each case must declare command {executable,args,expectedExitCode} as exact argv for the public target or test. executable is only a program name or file path, without shell quoting or arguments: use executable "go" and args ["test","./..."], never executable "go test". dependencies contains only case IDs declared earlier in this manifest, or [] when there are none; never put shared-step IDs or environment prerequisites in dependencies. Use sharedSteps for shared-step IDs; environment availability is not a case dependency. The runtime prepares the candidate copy and captures output; no shell interpolation occurs.

**blackbox-design：** Do not inspect any development workspace, implementation changes, whitebox work, or developer explanations. This workspace is the original baseline. Return the manifest in JSON without editing project files.

**whitebox-design：** Inspect and independently run existing tests before adding only missing coverage. Use the available native shell tool in this assigned whitebox authoring workspace for test discovery and trial execution, including running newly written tests before reporting reuse. The qa.prepare/qa.target/qa.collect operations are for later frozen-case execution or repair and are unavailable during authoring. These trial results are preparation evidence, not formal QA results. Edit only test content or test-only fixtures. Tests may live inside production files: preserve all production behavior and shared inputs, list the mixed file in the change manifest, and never list it in design.testFiles. Return only actual changed file paths in the top-level manifest; declare all test-only file paths separately in design.testFiles, including unchanged reused developer tests. Return reuse records for every case with caseId/testRef/coverageJudgment/procedure/observation/comparison/result EXECUTED_PASS or EXECUTED_FAIL. If a valid test reveals a production failure, return status FAIL with the actual failure evidence and a concrete message; do not change the test to force a pass. Production fixes return to development. The isolated intermediate commit is for independent review, not approval to promote; the reviewer checks all declared testFiles and the complete whitebox diff before final candidate creation.

# 白盒覆盖判断

Independently enumerate required behavior and exact oracles from the bound requirement,
public contracts and candidate implementation. The engine provides the bound ordinary
candidate workspace for normal reading and searching; source bodies are not inlined or
copied into a filtered projection. Locate relevant implementation as needed without
assuming any language or directory layout. Before recording this independent judgment,
do not inspect existing tests, modify files, run tests or bind existing tests. This is
role sequencing, not a per-file read whitelist. Return the coverage JSON only. The engine
records it before the subsequent test inspection and gap-filling invocation.

## 可执行共用准备

sharedSteps 的 command 使用与目标相同的 executable/args/expectedExitCode 声明；
文字 procedure 不自动执行，没有可执行步骤时 command 为 null。artifacts 列出需要复用
检查的产物相对文件路径。independent 为 true 表示准备本身是独立验收目标，必须实际
执行；否则相同有效输入共用完成的准备。case.sharedSteps 的引用顺序就是执行顺序。
普通只使用共用产物的用例选择 workspaceMode=isolated，只分配独立可变目录，
通过 FORMAL_GATES_QA_SHARED 读取共用源及产物；确实需要独立源码或 Git 工作树的
构建、安装、返修自测等选择 candidate。工具与依赖缓存沿用运行环境，不复制 HOME。
构建入口与前提以当前任务已绑定的材料为准。
