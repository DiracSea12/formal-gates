# formal-gates: Promotional Content for GitHub

## Short Tagline (for GitHub description)

Stop AI from shipping broken code. 5 quality gates that actually work—with independent review AI that can't rubber-stamp itself.

---

## Medium Description (for GitHub About section)

AI code quality gates that prevent direction drift, over-engineering, fake tests, and silent scope creep. One pre-work gate aligns requirements before coding starts. Four post-work gates validate quality through independent AI review, with no self-endorsement allowed. Core skill docs are Agent Skill compatible; bundled installer and hook targets are Claude Code, Codex, and Cursor. Hook enforcement must be proven per host by live canary.

---

## Full Introduction (for README top, blog posts, social media)

### The Problem Every AI-Assisted Developer Faces

You ask AI to build a feature. It writes code. It says "looks good!" You merge it. Then you discover:
- The implementation solves a different problem than you asked for
- It created 5 new abstractions when you needed a 10-line function
- Tests check if variables exist, not if the feature works
- Half the requirements silently disappeared

**Why?** Because AI reviews its own work. And AI, like humans, is terrible at finding its own mistakes.

### The Solution: Independent AI Review

**formal-gates** is a quality gate system for AI-assisted development that enforces one critical rule: **AI cannot approve its own work.**

Instead of letting your coding AI say "looks good," formal-gates:
1. **Blocks work before it starts** if requirements aren't clear (Requirements Clarification Gate)
2. **Dispatches independent review AI** that doesn't know what the coding AI was thinking
3. **Validates through 4 sequential gates**: Testing, Complexity, Architecture, Code Quality
4. **Enforces with machine validation**: PowerShell scripts check gate artifacts—no fake approvals allowed

### What Makes This Different

🚫 **No Self-Review**  
The AI that writes code never judges if it's good. Independent, zero-context review AI evaluates every gate.

🎯 **Catches Real Problems**  
Not style guides. Not formatting. Actual issues: wrong direction, bloated scope, broken architecture, fake tests.

⚙️ **Machine-Enforced**  
PowerShell validators reject bad gate artifacts. Configured and live-tested hooks can block missing evidence, placeholder verdicts, and reused stale approvals at command time.

📋 **One Pre-Work Gate**  
Requirements Clarification runs before coding starts—the only gate that matters more than all four post-work gates combined.

### Who Should Use This

✅ **Use formal-gates if you:**
- Build production systems with AI assistance
- Need to validate AI work before release
- Want to catch over-engineering before it ships
- Need real test coverage, not assertion theater
- Work on refactors, new systems, or full module development

❌ **Skip it for:**
- Quick prototypes and experiments
- UI tweaks and small bug fixes
- Casual exploration and learning
- Single-file typo corrections

### Real Impact

**Before formal-gates:**
- "Add authentication" → 15 files, 3 new abstractions, tests that only check field existence
- "Refactor the API" → scope creeps to redesigning half the system
- "Fix the bug" → requirements drift mid-implementation, different problem solved

**With formal-gates:**
- Requirements Clarification catches unclear goals before wasting tokens
- Complexity Gate blocks unnecessary abstractions and scope creep
- Architecture Gate validates boundaries and ownership
- QA Gate demands real evidence, not "it should work"
- Code Quality Gate catches bugs, edge cases, and maintenance issues

### Quick Start

```powershell
# Install to Claude Code (global)
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary -ConfigureHook
```

Then tell your AI: "run four gates" or "validate before seal"

Requirements clarification runs proactively before formal document work. The four post-work gates run when formal validation, release, or seal review is requested.

### Technical Details

- **Platform**: Windows + PowerShell 5/7
- **VCS**: Git, SVN, or file-hash snapshots
- **Hosts**: core skill docs are Agent Skill compatible; bundled installer and hook targets are Claude Code, Codex, and Cursor; hook enforcement requires per-host live canary
- **Languages**: Works with any codebase—gates validate behavior, not syntax

---

## Social Media Snippets

### Twitter/X (280 chars)

Stop AI from rubber-stamping its own code. formal-gates enforces independent review through 5 quality gates: 1 before coding starts, 4 after. Claude/Codex/Cursor are host targets; hooks need per-host live canary. https://github.com/DiracSea12/formal-gates

### LinkedIn

AI writes great code. AI is terrible at reviewing its own code.

formal-gates solves this with independent AI review through 5 quality gates:
• Requirements Clarification (before coding)
• Test Quality (real evidence, not fake assertions)
• Complexity Control (stop over-engineering)
• Architecture Health (boundaries & ownership)
• Code Quality (correctness & maintainability)

The key rule: AI that writes code cannot approve it. Zero-context review AI validates every gate. PowerShell scripts enforce evidence requirements—no placeholder approvals allowed.

Core skill docs are Agent Skill compatible. Bundled installer and hook targets are Claude Code, Codex, and Cursor. Hook enforcement must be proven on the target host with a live canary. Open source.

Perfect for: production systems, refactors, new features, release validation
Skip for: quick prototypes, UI tweaks, typo fixes

https://github.com/DiracSea12/formal-gates

### Reddit/HN Post Title

formal-gates: Stop AI from rubber-stamping its own code with independent review gates

### Reddit/HN Post Body

If you use AI to write code, you've probably seen this pattern:

1. Ask AI to build something
2. AI writes code and says "looks good!"
3. You merge it
4. Later discover it solved the wrong problem, over-engineered the solution, or has fake tests

The core issue: AI reviews its own work. And AI (like humans) is bad at finding its own mistakes.

**formal-gates** enforces independent review:
- The AI that writes code never judges if it passes
- Independent "zero-context" AI validates through 5 gates
- PowerShell scripts enforce evidence requirements
- Machine validation prevents fake approvals

**1 pre-work gate:**
- Requirements Clarification: Aligns goals/scope/acceptance before coding starts

**4 post-work gates (sequential):**
- QA: Real test evidence, not assertion theater
- Complexity: Blocks scope creep and over-engineering
- Architecture: Validates boundaries and ownership
- Code Quality: Catches bugs, edge cases, maintainability issues

Core skill docs are Agent Skill compatible. Bundled installer and hook targets are Claude Code, Codex, and Cursor. Hook enforcement must be proven on the target host with a live canary. Windows + PowerShell. Git/SVN/no-VCS supported.

Built for production systems, refactors, and release validation. Stays silent for small changes.

https://github.com/DiracSea12/formal-gates

---

## GitHub Topics/Tags

`ai-code-review` `code-quality` `claude-code` `quality-gates` `ai-development` `code-validation` `software-quality` `ci-cd` `development-tools` `ai-assisted-development` `cursor` `codex` `testing` `architecture` `refactoring`

---

## One-Liner Variants

**Concise**: Independent AI review gates that prevent self-endorsed code from shipping.

**Technical**: Multi-gate validation system enforcing zero-context review by independent AI agents.

**Value-focused**: Catch direction drift, over-engineering, and fake tests before they reach production.

**Differentiator**: The only AI code review system that blocks self-approval at the machine level.
