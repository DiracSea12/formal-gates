# Public Validation Result

This file is a public evidence summary for readers evaluating the package. It is not a formal gate verdict, not a release seal, and not proof that hooks enforce policy on any host. Hook enforcement still requires same-host live canary evidence.

## Snapshot

- Package: `formal-gates`
- Review method: public skill polish review
- Source commit at review start: `81b5bb8`

## Checks

| Check | Result | Evidence |
|---|---|---|
| Skill structure check | PASS with WARN | `PASS: 9`, `WARN: 4`, `FAIL: 0` |
| Go package validation | PASS | `PASS formal-gates package validation` |
| Portable OpenSpec canary | PASS | `passedChecks: 117`, `failedChecks: 0` |

## Structure Check Warnings

The skill structure check reported four public-packaging warnings:

- `.claude-plugin/marketplace.json` is missing.
- Demo GIF/video is missing.
- README lacks `npx skills add` installation wording.
- README lacks a skills.sh badge.

These are not package validation failures. This package does not currently claim marketplace, skills.sh, or `npx skills add` distribution support. Adding those channels should be a separate release-trust and distribution slice.

## Reader Notes

- Use `go run ./cmd/formal-gates-validate package --root .` to verify package structure locally.
- Use `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\test-portable-openspec-canary.ps1 -SkillPath .` to run the Windows portable canary.
- Do not treat this summary as a substitute for live validation in your target host or repository.
