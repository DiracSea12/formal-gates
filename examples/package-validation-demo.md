# Package Validation Demo

This demo is a local, rerunnable smoke test for the public package shape.

Run it from the repository root:

```bash
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates canary portable --root .
```

On Windows, use the `.exe` binary name:

```cmd
go build -o bin\formal-gates.exe ./cmd/formal-gates
bin\formal-gates.exe canary portable --root .
```

The native canary runs package validation, dispatch-prompt checks, hook decision checks, workflow state checks, FinalExecution recording from a supplied artifact, receipt checks, and install-shape checks without PowerShell.

## What This Proves

- The package structure validates with the Go package validator on this machine.
- The portable native canary passes against this checkout on this machine.
- The README-visible demo command is reproducible without script runtime helpers.

## What This Does Not Prove

- It does not prove host hook enforcement in Claude Code, Codex, Cursor, or any other runtime.
- It does not replace same-host live canary proof for hook behavior.
- It does not prove code quality, formal release/seal approval, or independent gate PASS.
- It does not prove public registry, marketplace, `npx`, signing, provenance, checksum, attestation, or release-trust distribution support.
