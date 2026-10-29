# 0011 — Route verified native controls through the household worker

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

The bounded Go hardware test verified OFF, saved-slot playback and TIMER with
physical confirmation and unchanged saved state. The owner requested integration
into the hosted application. Calendar execution and arbitrary effect uploads are
not verified. Two signed-in household users can act on the same cached state.

## Decision

Expose only OFF, existing native slot playback and return to the controller timer.
Use household authentication/CSRF and the existing serialized worker. Each request
carries the displayed snapshot fingerprint; check it against fresh controller state
before edit mode or any control send. Require the reviewed layout and explicit
clock-sync acknowledgment. Do not automatically refresh or write on startup.

Persist the actor, action, reviewed baseline and pending intent before sending.
Add schema 6 control audit metadata, retaining schema 5 backup compatibility.
Complete observation and audit together. Mark interrupted pending operations
uncertain on service restart without replay. Audit persistence failure blocks
further controls in that process. Never report a command verified without readback.

After an ambiguous send, retain the client gate and reconcile with one bounded
fresh read. Never automatically resend or restore the prior effect: an ordinary
control expresses the user's desired lasting state, unlike the diagnostic test.
Expose uncertainty and require refresh after failures. A verified TIMER result
confirms mode, not which schedule is active or whether lights are illuminated.

## Consequences

The household can use verified native controls while the broader scheduler is
unfinished. Simultaneous edits or manufacturer-app changes produce a conflict
instead of selecting a slot whose content changed. A second identical request
can be rejected as stale; the UI does not retry writes. Process loss after a send
can leave the outcome uncertain until an explicit refresh. The last controller
state persists when the server is unavailable.

## Alternatives considered

- Direct browser/controller connections: bypass serialization and server audit.
- Automatic write retries: can restart effects after an ambiguous successful send.
- Restore after every control: contradicts the user's requested lasting state.
- Enable Calendar execution with native controls: requires additional ownership,
  handoff and reconciliation verification and remains separate work.

## References

- Builds on: [ADR-0003](0003-shared-homelab-application.md),
  [ADR-0010](0010-bounded-playback-verification.md).
- Shapes: [application design](../design/application.md),
  [native control contract](../specs/native-controls.md),
  [backup contract](../specs/backups.md), [roadmap](../plans/roadmap.md).
