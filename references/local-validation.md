# 本地验证

修改 formal-gates 包自身时，使用这份清单：

```bash
gofmt -w <changed-go-files>
go test ./...
go test -race ./internal/validate ./internal/cli
go test -tags=qastructure ./internal/validate
go vet ./...
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
git diff --check
git diff --cached --check
```

Windows 上使用 `bin\formal-gates.exe`。实机 host hook canary 是独立的一项，只
有在修改或声明某个 host 的自动拦截时才需要。这些检查不能替代用户授权的正式 run
中的独立审查。

QA 固定操作载荷与 Go 精确测试引用的解析契约见
[QA 固定操作契约](qa-operations.md)。解析只描述 invocation；实际执行与证据接纳
由对应执行入口负责。
