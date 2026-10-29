# 0010 — Verify captured playback with a separate bounded operation

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

An official-app capture correlated with the owner's physical observations now
identifies OFF, saved-slot playback and TIMER commands. Independent Go writes
still need verification. The existing diagnostic restriction permits only a
disabled schedule-slot experiment. The owner explicitly authorized a separate
bounded OFF → selected saved slot → original effect/TIMER test on 2026-10-28.

## Decision

Add `verify-playback` as a separate diagnostic, leaving `probe-schedule2` unchanged.
Require the reviewed full non-clock fingerprint, the observed layout, TIMER mode,
one unambiguous original parameter match and a distinct existing target effect.
Persist the baseline before edit mode or writes. Hold OFF and target playback for
ten seconds each after verified readback for physical observation. Restore the
original effect and TIMER even after cancellation or an ambiguous send, using a
bounded independent cleanup context and reconnect reconciliation. Keep the client
gate for the whole operation, including recovery and independent verification.

Require unchanged non-playback fields at every readback. A changed library or
schedule invalidates restoration assumptions: stop writing and report that
restoration was not verified. Never rewrite those fields to force a match. Keep
the service worker disabled and the official phone app closed during diagnostics.

This extends the diagnostic scope from [ADR-0007](0007-public-repository-and-private-deployment.md)
only for this operation. It does not enable ordinary HTTP controls, custom effect
writes, calendar execution, brightness or physical mapping calibration.

## Consequences

Readback and private evidence can distinguish controller-reported success from
the owner's physical observations. Cleanup cannot guarantee restoration during a
persistent outage or external state change; failure must be explicit. Process kill
or host loss can bypass cleanup, requiring restoration with the official app.
Synthetic tests cover failure recovery; they cannot replace the physical test.

## Alternatives considered

- Broaden `probe-schedule2`: would mix unrelated mutations and weaken its contract.
- Replay captured bytes without baseline checks: would ignore current library and
  timer state and provide no verified recovery.
- Enable the hosted controls immediately: would expose unverified behavior before
  the paced Go sequence has been checked on the actual controller.

## References

- Builds on: [ADR-0003](0003-shared-homelab-application.md),
  [ADR-0007](0007-public-repository-and-private-deployment.md).
- Shapes: [application design](../design/application.md#observed-native-playback--2026-10-28),
  [verification contract](../specs/playback-verification.md),
  [roadmap Phase 2](../plans/roadmap.md#phase-2--reliable-controller-service-l-highest-uncertainty).
