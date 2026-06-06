# Harden Public Release Readiness

## Why

The package is being prepared as an open-source candidate, so public metadata and docs must be accurate, portable, and not overstate host support.

## Goal

Make `formal-gates` safe to present as an open-source candidate package without overstating platform support.

## Scope

- Add missing repository hygiene needed before public release.
- Fix invalid OpenAI metadata.
- Add a machine-readable package manifest for humans and agents.
- Include read-only behavior-check prompt examples as public package examples without making them formal gates.
- Clarify Claude/Codex/Cursor support boundaries and local-path hygiene in public docs, including `references/install-and-hooks.md`.
- Keep the current skill structure: short `SKILL.md`, detailed `references/`, scripts, hooks, agents, and examples.

## What Changes

- Add public release hygiene files and a compact package manifest/index.
- Fix OpenAI host metadata so it is valid YAML.
- Align README, promotional, and public installation/hook documentation with Claude Code, Codex, and Cursor as host targets that each require their own live canary evidence for hook enforcement claims.
- Document `examples/skill-behavior-prompts.json` as a read-only skill behavior-check sample that can be packaged publicly but does not define release gates.
- Add a minimal OpenSpec spec delta covering public release readiness.

## Non-goals

- Do not rewrite the gate system.
- Do not add a new installer framework.
- Do not port PowerShell scripts to another language.
- Do not claim full Codex plugin distribution unless a plugin manifest and validation are actually added.
- Do not remove existing Claude/Codex/Cursor support paths.
- Do not treat behavior-check prompts as formal QA, release, or seal gates.

## Acceptance

- The repository has a clear open-source license file.
- Local gate artifacts and review scratch directories are ignored by git.
- `agents/openai.yaml` is valid YAML and starts with `interface:`.
- README files describe Claude Code, Codex, and Cursor as host targets with per-host live canary requirements.
- Public promotional and installation/hook docs do not overstate platform support or expose maintainer-local absolute paths; hook enforcement claims require target-host hook/config plus live canary evidence.
- A small manifest or index lists package parts, supported hosts, install commands, and verification commands.
- README, manifest, and OpenSpec agree that `examples/skill-behavior-prompts.json` is a public read-only behavior-check sample, not a formal gate.
- The package still passes the portable canary and skill validation.
