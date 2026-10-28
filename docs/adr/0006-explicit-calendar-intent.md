# 0006 — Evaluate calendar rules as explicit household intent

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

Scheduling is the household's primary complaint. Native controller writes and
timer handoff are not yet verified. Both phones must edit one calendar without
silent lost updates, and previews must use the same evaluator as future execution.

## Decision

Persist an immutable, versioned calendar document with timezone, optional location,
rules and an optional expiring manual override. Rules reference retained pattern
revisions, never a mutable latest pattern. Use compare-and-swap for the whole
document. Keep all revisions and an indexed relation to each referenced pattern.

Evaluate intervals in the household timezone, with inclusive start dates and
exclusive end instants. An explicit next-day flag represents overnight windows.
Resolve nonexistent clock times by shifting forward by the DST gap; choose the
first occurrence of a repeated start and the last occurrence of a repeated end.
Report these adjustments. Skip an occurrence with missing solar events, a skipped
civil date or non-positive duration and report why.

Precedence is manual override, one-day exception, seasonal range, weekly rule,
then default off. Within a category, higher numeric priority wins. Equal-ranked
rules with identical actions agree; different actions produce a visible conflict
with off as the proposed intent. Rule order must not affect the result.

Keep evaluation independent of hardware. Saving, previewing and expiring an
override do not send commands. Future execution must evaluate current intent on
restart/reconnect, not replay missed transitions, and must be gated on verified
controller ownership and timer handoff.

## Consequences

Calendar edits and preview can be delivered before hardware takeover. Pinned
patterns require an explicit revision update in the calendar. Archived patterns
remain available to existing references; new references to archived patterns are
rejected. Pattern edits and archives must display their calendar impact.
One document means simultaneous edits to unrelated rules can conflict; preserve
the unsaved draft and offer an explicit reload rather than merge invisibly.

## Alternatives considered

- Latest-pattern references: a palette edit would change scheduled behavior without
  a calendar decision.
- Last saved rule wins: rule ordering would conceal conflicts.
- Native-only scheduling: cannot deliver the required calendar and precedence.

## References

- Builds on: [ADR-0003](0003-shared-homelab-application.md),
  [ADR-0005](0005-versioned-household-patterns.md).
- Shapes: [application design](../design/application.md),
  [calendar contract](../specs/schedules.md), [roadmap](../plans/roadmap.md).
