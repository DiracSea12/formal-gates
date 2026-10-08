# QA 固定操作契约

`coverage.AcceptanceCase.operations` 与 `coverage.SharedStep.operations` 是 QA 设计
创作的有序操作载荷。它们随完整 case/shared-step 内容进入既有审查身份、manifest digest
和冻结；不是执行阶段从自然语言临时提取的命令。步骤、前置说明与 oracle 仍表达语义。

每个操作只允许一种形态，可附带原样发送的 `stdin`：

```json
{"kind":"ARGV","argv":["tool","","two words","\"quoted\""],"stdin":"input\n"}
{"kind":"SHELL","command":"printf '%s' \"$value\"\n"}
```

`ARGV` 必须提供非空数组和非空 executable，不能提供 `command`；后续参数可以是空
字符串，空格、引号和特殊字符都是该参数的原值。`SHELL` 必须提供非空 `command`，
不能提供非空 `argv`。操作不含 cwd、env、插件、条件或循环字段。完整 shell 文本可以
表达已批准的环境设置或目录切换；程序不改写文本。执行目录由 registered QA workspace
提供。shared-step 的操作由引用它的 case 使用，载荷本身不表示全局只执行一次。

Go 白盒测试可以只保留精确引用 `<仓库相对路径>_test.go::TestName`。标准库 resolver
读取指定候选文件，解析 Go 源码，核对该文件中唯一的同名包级测试函数及
`func(*testing.T)` 签名，支持普通、别名和 dot import。测试名遵守 Go 的 `Test` 命名
规则。文件目录确定 package 工作目录，固定 invocation 为：

```text
go test -count=1 -json -run ^<精确测试名>$ .
```

测试名经 `regexp.QuoteMeta` 转义。该协议只接受完整包级测试，不接受裸函数名、`#`
分隔符、子测试路径或其他语言的引用，也不搜索其他文件来猜测目标。Go 精确引用不能
同时声明 case operations；共享准备操作仍由 shared-step 单独持有。其他不透明 testRef
保留语义引用，需要已批准 operations 才能解析执行动作。

`coverage.ResolveCaseOperations` 只返回 `ResolvedOperation`（原操作、派生目录与 Go
精确测试名），不启动进程或判断 oracle。缺文件、缺测试、同名函数歧义、错误签名、
不支持的引用且没有操作配方，都会返回明确错误。设计合同允许尚未具备配方的部分设计，
这种设计不能由 resolver 推断操作来执行。

执行与接纳方须保留真实 exit、stdout/stderr 原始证据。对 Go invocation，须核到
`GoTestName` 对应的实际 run 和 pass/fail 事件；无匹配或 skip 不能当作执行 PASS。
普通操作的非零 exit 可以是失败用例的预期观察，零 exit 也不能代替 oracle 比较。
resolver 不负责调度、实际执行、receipt 接纳或语义比较。
