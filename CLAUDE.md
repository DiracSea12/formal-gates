# Project Agent Rules

This is a personal project. Guarantee normal documented use and common operator mistakes only.

Unless the user explicitly requests adversarial or security hardening, coordinated evidence tampering, malicious local edits, manual rewriting of internal state, permission or immutable-file fault injection, attack-style inputs, and scenarios that require violating the documented workflow are out of scope. They cannot block PASS or trigger implementation work.

Repeat this boundary explicitly in every subagent dispatch. Out-of-scope findings are advisory; if no in-scope blocker remains, the result must PASS. Do not add defenses, compatibility paths, tests, or process steps for abnormal modification.

QA case design and execution use the same project boundary. Do not add adversarial, internal-state-rewriting, permission, immutable-file, or unsupported-platform cases unless the user explicitly requests that coverage.

Every blocker must include an end-to-end reproduction starting from a documented public entrypoint and using only normal user actions or common mistakes. A reproduction that requires manually creating or rewriting internal artifacts, state, receipts, run directories, or attack-style inputs is advisory only. Before reporting that a change cannot be pushed, the main agent must independently verify the claimed normal-use reproduction path.
