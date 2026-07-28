# 本地验证

修改 formal-gates 包自身时，使用这份清单：

```bash
gofmt -w <changed-go-files>
go test ./...
go test -race ./internal/validate ./internal/cli
go vet ./...
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
bin/formal-gates behavior evaluate --root . \
  --cases examples/skill-behavior-prompts.json \
  --answers examples/skill-behavior-answers.json
git diff --check
git diff --cached --check
```

Windows 上使用 `bin\formal-gates.exe`。实机 host hook canary 是独立的一项，只
有在修改或声明某个 host 的自动拦截时才需要。这些检查不能替代用户授权的正式 run
中的独立审查。
