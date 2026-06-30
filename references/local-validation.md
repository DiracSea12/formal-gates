# Local Validation

This file collects the repository's local self-check commands for maintainers.
It is separate from install-and-hooks guidance and from the AI skill entrypoint.

## Self-Check Chain

```bash
go test ./...
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
bin/formal-gates behavior evaluate --root . --cases examples/skill-behavior-prompts.json --answers examples/skill-behavior-answers.json
```

## Notes

- `go test ./...` verifies the Go unit tests for the repository.
- `package validate` checks package shape and required files.
- `canary portable` checks the portable package evidence for the current platform.
- `behavior evaluate` checks the behavior answer fixture against the portable cases.

## Companion Material

- [Install And Hooks](install-and-hooks.md)
- [`examples/package-validation-demo.md`](../examples/package-validation-demo.md)
