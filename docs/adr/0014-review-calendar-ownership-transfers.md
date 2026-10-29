# 0014 — Review and journal calendar ownership transfers

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

The dormant runner can deliver current intent after ownership is established.
Taking control from TIMER and returning it must survive stale reviews, competing
household actions, uncertain writes and process loss. Native timer-boundary and
Sunday clock evidence are still missing.

## Decision

Implement an internal, dormant handoff coordinator. A review binds the saved
calendar revision, ownership version, canonical controller fingerprint, immediate
action and expiry. The review expires after at most one minute, or at the current
intent boundary if sooner. Review reads cached state and never contacts hardware.

Transfer through the existing worker and verified OFF/play/TIMER driver. Record a
paused ownership checkpoint and pending handoff audit atomically before sending.
Activate Calendar only with verified completion and unchanged ownership version.
Returning to TIMER disables Calendar only after verified completion. An intervening
pause wins. Failure or interruption leaves ownership paused, without retry or an
automatic compensating command. Preserve native schedules and library bytes.

Keep the production gate from ADR-0013: no activation endpoint, flag or startup
runner. The coordinator is exercised with synthetic hardware until physical
protocol evidence permits a separate decision to expose it to the household.

## Consequences

Takeover and return use the same durable audit and recovery path as ordinary
controls. Existing schema 7 JSON records can retain the additional handoff
provenance without a migration. Reviews are server-created internal values, not
client authority; a future HTTP workflow must bind them to the authenticated user.
Native timer return resumes the controller's existing schedule, not a restoration
of the previous visible scene. Server outage still has no verified off fallback.

## Alternatives considered

- Claim ownership after a separate unaudited play: a crash could lose the transfer
  intent, and a concurrent pause could be overwritten.
- Restore TIMER automatically on failure: ambiguous delivery is not permission for
  another physical write, and native state may have changed meanwhile.
- Expose an override flag now: operator acknowledgement cannot replace missing
  protocol evidence.

## References

- Builds on: [ADR-0011](0011-audited-household-native-controls.md),
  [ADR-0012](0012-prepare-native-calendar-handoff.md),
  [ADR-0013](0013-journal-calendar-execution.md).
- Shapes: [application design](../design/application.md#scheduling-and-control-ownership),
  [handoff contract](../specs/calendar-handoff.md),
  [runtime contract](../specs/calendar-runtime.md),
  [roadmap](../plans/roadmap.md#phase-4--full-scheduling-and-first-household-beta-l).
