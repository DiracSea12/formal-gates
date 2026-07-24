# Local Validation

Use this checklist when changing the formal-gates package itself:

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

Use `bin\formal-gates.exe` on Windows. A live host-hook canary is separate and
required only when changing or claiming that host's automatic interception.
These checks do not replace independent review in a user-authorized formal run.
