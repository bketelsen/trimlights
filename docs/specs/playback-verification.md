# Spec: Bounded native playback verification

This diagnostic contract governs the owner-authorized Go hardware test of captured
OFF, saved-slot playback and TIMER commands. It does not expose household controls.

## Interface

`trimlight verify-playback --host controller.example.invalid --baseline-sha256 HEX
--pattern-id ID --output NEW_DIRECTORY --timezone America/New_York`

| Input | Required | Constraints |
| --- | --- | --- |
| `--host` / `TRIMLIGHT_CONTROLLER_HOST` | yes | Same explicit-flag precedence as snapshot; TCP 8586 |
| `--baseline-sha256` | yes for writes | Reviewed SHA-256 of all non-clock snapshot bytes |
| `--pattern-id` | yes | Existing ID 1–18, with parameters different from the original effect |
| `--output` | yes | New private evidence directory; never overwrite |
| `--timezone` | default provided | Controller local IANA timezone; query can synchronize its clock |

The result includes snapshots `before`, `off`, `played`, `restored`, `independent`,
the original and target slot IDs, `write_attempted`, and readback verification
booleans for OFF, playback, restoration and independent reconnect. Physical
observations are recorded separately; these flags describe protocol readback only.
Evidence recording errors MUST fail the command even if restoration succeeded.

## Rules

- Dry-run MUST create no files, perform no network IO and report the baseline as
  unverified. Normal JSON output MUST be one document; progress is human-mode only.
- Fresh baseline MUST match the fingerprint before edit mode. It MUST have 3492
  bytes, 18 saved records with IDs 1–18 in order, the explicit schedule offset 3306,
  mode 2 and exactly one original parameter match. No other layout is writable.
- Baseline evidence MUST be persisted before any edit command or write. The
  operation MUST hold the client gate through cleanup and independent readback.
- Before any edit command or write, the operation MUST compute the latest time at
  which cleanup and independent verification can finish: six paced exchanges at
  the I/O timeout, both holds, the 40-second cleanup budget, the ten-second
  independent budget and a one-minute evidence margin (166 seconds with 1-second
  pacing, 5-second timeouts and ten-second holds). If any instant from now to that time falls on
  Sunday in `--timezone`, it MUST refuse without a write. Every snapshot sends a
  clock query, which refuses unverified Sunday times, so cleanup could not read or
  restore state. No Sunday encoding is assumed (added 2026-10-30).
- The check MUST also be enforced (added 2026-10-30). At the check, the operation
  sets an absolute active-phase deadline: the window end less the cleanup and
  independent budgets and a five-second hand-over guard (111 seconds after the
  check with the values above). OFF, playback, readbacks, holds and the `off` and
  `played` evidence records MUST end by it. At the deadline, no further control
  write is sent and cleanup starts at once. The `restored` and `independent`
  records MUST return within the cleanup and independent budgets. A recorder call
  that does not return in time fails the command as an evidence error; recorder
  calls stay serialized, so later records are skipped, and reported, while an
  overrun call still runs. Restoration does not wait for evidence recording.
- OFF uses payload `02 00 01 00`. Saved playback uses `04 00 01 ID`. TIMER uses
  `02 00 01 02`. Each write MUST follow the captured edit-session exchange; all
  exchanges MUST retain the client's pacing. No standalone MANUAL mode command
  or arbitrary payload is exposed.
- OFF readback MUST have mode 0 and retain baseline parameters; playback readback
  MUST have mode 1 and match the selected slot's parameters. Both MUST match all
  other baseline bytes except the seven-byte clock suffix. Each successful stage
  holds for ten seconds, interruptible by cancellation.
- After any attempted control send, cleanup MUST run with its own deadline. It
  MUST read current state before restoring; immutable-field changes MUST prevent
  further writes. A disconnected/failed restoration MAY use one fresh connection
  to reconcile and retry. Total cleanup budget is 40 seconds, plus a separately
  bounded ten-second independent verification. No unbounded retries.
- Restoration MUST replay the original saved effect then select TIMER, unless
  readback already exactly matches the original non-clock state. Final restoration
  and independent verification MUST compare every non-clock byte. A failed test
  remains failed even if cleanup recovers successfully.
- Evidence recording failure MUST NOT prevent restoration. A failure before the
  baseline is saved MUST send no edit command or control write. A failure after a
  write MUST trigger cleanup. Unverified restoration MUST be explicit in errors.
- The service worker MUST be disabled/stopped and the official app closed. Process
  kill, host failure or persistent controller outage can prevent cleanup; the
  official app remains the manual recovery path.
- Fixtures MUST be synthetic. Normal tests/CI MUST never access physical hardware.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| Private stage JSON files | Validated snapshots; owner-only permissions |
| Private `result.json` | Report and any failure, including restoration status |

## References

- Rationale: [ADR-0010](../adr/0010-bounded-playback-verification.md).
- Context: [control design](../design/application.md#observed-native-playback--2026-10-28).
- Snapshot fields: [local service](local-service.md#observed-playback-state--2026-10-28).
- Delivery: [roadmap Phase 2](../plans/roadmap.md#phase-2--reliable-controller-service-l-highest-uncertainty).
