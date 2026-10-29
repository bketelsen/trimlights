# 0012 — Prepare calendar handoff without changing controller ownership

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

Authenticated saved-slot playback and return to TIMER are verified. Unattended
execution still depends on Sunday clock encoding and observing native timer
boundaries while in MANUAL/OFF. Calendar rules currently reference application
revisions, which may be custom designs or imports of slots that have since changed.

## Decision

Add a read-only preparation workflow to Calendar. Copy supported cached native
schedule entries into individually reviewable rules; append only explicitly chosen
rules to the draft and retain existing edits. Preserve every native entry in the
report, including disabled and unsupported entries. Require an explicit date for
native “today only”; reject ambiguous equal/overnight times rather than infer the
controller's meaning. Saving remains a separate compare-and-swap action.

Resolve pinned application revisions against canonical cached controller bytes.
A native reference must match both its original slot ID and all opaque parameters;
never substitute by name, latest revision or a different matching slot. Report
custom effects, changed slots, stale observations, conflicts and protocol gaps.
Preparation uses no controller or worker access and enables no execution. Keep
runtime delivery and native timer handoff as the next separate implementation and
hardware-verification step.

## Consequences

The household can move its existing routines into the shared Calendar without
losing native fallback or assuming that an editor preview can play on hardware.
No new schema or automatic hardware access is needed. Cached readiness is advisory:
future execution must repeat checks against fresh state before each write. A weekly
report does not certify conflicts outside its stated horizon.

## Alternatives considered

- Automatically enable scheduling after copying: native ownership and Sunday
  behavior remain unverified.
- Select slots by display name: names can change or be duplicated, and a reused
  slot may no longer contain the pinned effect.
- Replace the whole draft: unrelated household edits would be lost.

## References

- Builds on: [ADR-0006](0006-explicit-calendar-intent.md),
  [ADR-0011](0011-audited-household-native-controls.md).
- Shapes: [application design](../design/application.md#scheduling-and-control-ownership),
  [preparation contract](../specs/calendar-preparation.md),
  [roadmap](../plans/roadmap.md#phase-4--full-scheduling-and-first-household-beta-l).
