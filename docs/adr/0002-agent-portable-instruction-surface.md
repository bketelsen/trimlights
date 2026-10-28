# 0002 — Agent-portable instruction surface

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

The project is maintained by whichever coding agent is available — each tool
reads its own instruction file (`CLAUDE.md`, `.github/copilot-instructions.md`,
`GEMINI.md`, `AGENTS.md`) and its own skills location (`.claude/skills/`).
Duplicating conventions per tool guarantees drift, and drift means different
agents follow different law.

## Decision

One canonical surface, tool paths as symlinks:

- **`AGENTS.md`** (the emerging cross-tool standard) is the real file holding
  all conventions. `CLAUDE.md`, `GEMINI.md`, and
  `.github/copilot-instructions.md` are symlinks to it.
- **`.agents/skills/`** is the real skills directory (`<skill>/SKILL.md` with
  YAML frontmatter). `.claude/skills` is a symlink to it.
- Content is written tool-agnostically: plain markdown, no tool-specific
  directives; anything only one tool understands stays out of the shared
  files.

## Consequences

- Conventions and skills are edited in exactly one place; every agent sees
  the same law.
- Symlinks are committed to git — fine on Linux/macOS; GitHub's web renderer
  may show link targets rather than content, an accepted cosmetic cost.
  Native Windows checkouts need `core.symlinks=true` or WSL.
- The skill format is the lowest common denominator (frontmatter + markdown
  steps); agents without native skill support are pointed at the directory
  from AGENTS.md.

## Alternatives considered

- **Per-tool copies kept in sync by convention:** guaranteed drift. Rejected.
- **Instructions for one tool only:** wastes every other agent exactly when
  it's needed. Rejected.

## References

- Builds on: [ADR-0001](0001-record-architecture-decisions.md)
- Shapes: `AGENTS.md`, [.agents/skills/](../../.agents/skills/)
