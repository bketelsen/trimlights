# 0013 — Journal calendar execution before controller delivery

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

The pure calendar evaluator, immutable pattern references, native controls and
handoff preparation exist. Runtime delivery needs durable ownership, crash
recovery and an explicit response to external controller changes. Sunday encoding
and native timer-boundary behavior are still unverified.

## Decision

Implement a dormant calendar runner using the existing serialized controller
worker and native control driver. Persist ownership and delivery provenance in
SQLite. Record pending delivery with the observation and operation in one
transaction before any command. Commit completion and ownership checkpoint with
verified observations. An interrupted delivery pauses ownership; never replay it.

At every reconciliation, read fresh controller state, require the last verified
ownership fingerprint, and evaluate current calendar intent. Resolve exact pinned
native slots, recheck calendar revision, ownership version and interval at the
pre-write boundary, then send at most one command. Read failures back off; reconnect
re-evaluates the present instant. External changes, unsupported intent, ambiguous
writes and persistence errors pause execution. Ordinary household controls pause
Calendar ownership in the same transaction that records their pending intent.

Keep production activation unavailable: no enable endpoint or runtime flag, and
no runner started by the service until the protocol and handoff workflow are
verified. Synthetic tests can arm ownership on supported MANUAL/OFF snapshots.
A deployment or restore must not turn this into permission to take over TIMER.

## Consequences

The delivery/recovery machinery can be exercised now without changing the working
household schedule. Schema 7 and portable backups must retain ownership history;
restored ownership is paused in addition to the existing controller-disabled guard.
A server outage can leave lights in their last state; no native fallback is assumed.
Calendar edits made after a durable delivery begins take effect on the next pass,
not by pretending a physical write can be atomically canceled with a database save.

## Alternatives considered

- Replay due events after restart: stale transitions can flash or leave the wrong
  scene and revive expired overrides.
- Retry ambiguous commands: transport failure does not establish non-delivery.
- Enable a runtime flag now: missing protocol evidence would make a seven-day
  household scheduler unreliable.

## References

- Builds on: [ADR-0006](0006-explicit-calendar-intent.md),
  [ADR-0011](0011-audited-household-native-controls.md),
  [ADR-0012](0012-prepare-native-calendar-handoff.md).
- Shapes: [application design](../design/application.md#scheduling-and-control-ownership),
  [runtime contract](../specs/calendar-runtime.md), [backup contract](../specs/backups.md),
  [roadmap](../plans/roadmap.md#phase-4--full-scheduling-and-first-household-beta-l).
