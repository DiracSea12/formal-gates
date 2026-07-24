# Project Agent Rules

This is a personal project. Guarantee normal documented use and common operator mistakes only.

Unless the user explicitly requests adversarial or security hardening, coordinated evidence tampering, malicious local edits, manual rewriting of internal state, permission or immutable-file fault injection, attack-style inputs, and scenarios that require violating the documented workflow are out of scope. They cannot block PASS or trigger implementation work.

Repeat this boundary explicitly in every subagent dispatch. Out-of-scope findings are advisory; if no in-scope blocker remains, the result must PASS. Do not add defenses, compatibility paths, tests, or process steps for abnormal modification.

QA case design and execution use the same project boundary. Test each behavior at the lowest layer that directly owns it. Missing higher-level repetition is advisory when existing direct automated coverage already exercises the same deterministic rule; it may block only when the higher layer adds normal-use behavior that the lower layer cannot cover. Do not require tests whose main purpose is to retest other tests or validators. Do not add adversarial, internal-state-rewriting, permission, immutable-file, or unsupported-platform cases unless the user explicitly requests that coverage.

Every blocker must include an end-to-end reproduction starting from a documented public entrypoint and using only normal user actions or common mistakes. A reproduction that requires manually creating or rewriting internal state, temporary run data, or attack-style inputs is advisory only. Before reporting that a change cannot be pushed, the main agent must independently verify the claimed normal-use reproduction path.

Never hurry or pressure a review subagent. Do not tell it to stop reading or checking, converge early, use only evidence already collected, return immediately, or otherwise shorten its assigned review. A status request may only ask for progress and must explicitly tell the subagent to continue until every assigned check is complete. A verdict returned after improper pressure is invalid, must be discarded and rerun cleanly, and does not count as a completed review-repair cycle.
