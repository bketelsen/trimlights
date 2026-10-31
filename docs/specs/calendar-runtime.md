# Spec: Dormant calendar execution worker

This contract governs the implemented delivery engine and its synthetic tests.
Production activation is unavailable pending verified protocol and native handoff.

## Interface

Schema 7 adds singleton `calendar_runtime(id,body)` with version, status
(`disabled`, `active`, `paused`), actor, owned fingerprint, pending operation,
last operation and reason. Control audit JSON can contain `calendar` provenance:
ownership version, calendar revision, pinned action, and intended interval.
`GET /api/v1/status` reports this persisted state separately from the always-false
`calendar_execution` capability. There is no enable API, CLI flag or startup runner.

The internal runner provides a single reconciliation step and a cancellable loop.
Successful passes run at most every 30 seconds, shortened for the next boundary.
Read-only failures back off from 5 seconds to at most 5 minutes. Any resumed pass
re-evaluates the current instant; elapsed transitions are not queued.

## Rules

- Directly arming internal test ownership requires a supported, error-free, recent
  cached MANUAL/OFF snapshot and an exact fingerprint. The separate dormant
  [handoff coordinator](calendar-handoff.md) tests reviewed TIMER takeover through
  the worker; neither path is exposed in production.
- Before delivery, a fresh worker read MUST match the persisted owned fingerprint.
  A different mode, library, native schedule or other non-clock byte pauses the
  runner without trying to restore or overwrite it.
- Resolve all enabled pattern references and unexpired overrides by exact original
  slot and parameter bytes. Custom/unmatched designs and current conflicts or
  upcoming skipped occurrences pause. Resolved DST shifts and repeated clock times
  follow the existing evaluator semantics. Calendar preview remains independent.
- Evaluate current intent after the read and again before recording pending intent.
  A calendar save, ownership change or interval expiry while queued MUST prevent
  a stale write. Revision/ownership checks and pending intent commit together.
- A pending operation belongs to one ownership version. Completion updates its
  checkpoint atomically with the final observation/audit. Only verified/reconciled
  results advance the owned fingerprint. Errors pause; no ambiguous write retries.
- Identical desired OFF/MANUAL parameters produce no repeated control operation;
  periodic reads still detect external changes. Observed TIMER is never adopted.
- Ordinary OFF/play/TIMER controls pause active calendar ownership before sending.
  If persistence fails, neither the manual nor scheduled command may proceed.
- Startup converts interrupted deliveries to uncertainty and pauses ownership.
  While activation is unavailable, even clean active ownership is paused at service
  startup without querying hardware. Restores always pause copied ownership.
- Failure to persist runner state stops its loop. A successful refresh cannot
  resume a paused owner. Canceled or shutdown work does not silently retry writes.
- Backups support schemas 5–7. Migration backs up schema 6; backup/restore tests
  retain calendar audit provenance and clear active/pending ownership on restore.
- The engine does not alter native timers, upload effects or guess Sunday encoding.
  The internal handoff coordinator is implemented and synthetically tested.
  Owner-facing activation and physical acceptance remain required before
  production execution.
- Known gap (2026-10-30): every snapshot sends a clock query, and the driver
  refuses Sunday in the controller timezone (`--timezone`). From Sunday 00:00 to
  Monday 00:00 each runner refresh therefore fails. The runner treats this as a
  read-only failure: it backs off to at most 5 minutes, sends no write and keeps
  ownership active. It does not pause or set a runtime reason; the cause shows
  only in the observation's last error, the failed operation's error in
  operation history (`GET /api/v1/operations`) and the preparation blocker
  `sunday_clock_unverified`. Sunday transitions are not delivered. Monday's first successful
  pass applies only the then-current intent. A delivery whose readback falls on
  Sunday is uncertain and pauses ownership.
- Before production activation, either verified Sunday encoding MUST replace the
  refusal ([test plan phase 3](../plans/scheduling-controller-test.md#phase-3--sunday-clock-support)),
  or the runner MUST pause ownership with a clear Sunday reason instead of silent
  back-off. Neither is implemented yet.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| Calendar ownership checkpoint | Durable pending intent and verified completion |
| Calendar delivery audit | Calendar revision, pinned action and actual outcome |
| Runtime tests | Fake time, synthetic controller, real SQLite and worker queue |

## References

- Rationale: [ADR-0013](../adr/0013-journal-calendar-execution.md).
- Internal transfer rationale: [ADR-0014](../adr/0014-review-calendar-ownership-transfers.md).
- Context: [application design](../design/application.md#scheduling-and-control-ownership).
- Related: [calendar](schedules.md), [native controls](native-controls.md),
  [backups](backups.md), [roadmap](../plans/roadmap.md).

Hardware prerequisites are sequenced in the
[scheduling controller test plan](../plans/scheduling-controller-test.md).
