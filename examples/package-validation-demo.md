# Package Validation Demo

This demo is a local, rerunnable smoke test for the public package shape.

Run it from the repository root:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File examples\package-validation-demo.ps1
```

The script runs existing validation commands only:

```powershell
go run ./cmd/formal-gates-validate package --root .
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\test-portable-openspec-canary.ps1 -SkillPath .
```

It writes the local run output to:

```text
examples/package-validation-demo-output.txt
```

## What This Proves

- The package structure validates with the Go package validator on this machine.
- The portable OpenSpec canary passes against this checkout on this machine.
- The README-visible demo command is reproducible without editing core scripts.

## What This Does Not Prove

- It does not prove host hook enforcement in Claude Code, Codex, Cursor, or any other runtime.
- It does not replace same-host live canary proof for hook behavior.
- It does not prove code quality, formal release/seal approval, or independent gate PASS.
- It does not prove public registry, marketplace, `npx`, signing, provenance, checksum, attestation, or release-trust distribution support.
