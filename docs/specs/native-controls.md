# Spec: Authenticated native controls

The hosted application exposes verified native controller operations through one
worker. This contract excludes custom effect writes. The dormant
[Calendar runner](calendar-runtime.md) reuses delivery with additional ownership
and revision guards; production Calendar activation remains unavailable.

## Interface

`POST /api/v1/controller/control` requires household session/CSRF or the existing
loopback bearer mode, `Content-Type: application/json`, and one object up to 1024 bytes:

```json
{"action":"play","pattern_id":9,"baseline_sha256":"REVIEWED_SNAPSHOT_HASH","acknowledge_clock_sync":true}
```

`action` is `off`, `play` or `timer`. Only `play` accepts a nonzero `pattern_id`,
which must be 1–18. `baseline_sha256` is the displayed snapshot's full non-clock
SHA-256. Successful responses include `operation_id` and `status: verified`.
Malformed inputs return 400, unavailable controls/queue 503, stale baseline 409,
and unverified command outcomes 502. No HTTP error authorizes a retry.
The media type check accepts parameters such as `charset=utf-8`; see the
[local service contract](local-service.md#interface). A 503 answers the fixed
availability reason, `controller access is disabled`, `controller queue is full`
or `controller is unavailable; try again later`. A 502 answers `operation_id`
and a fixed `error`: `controller clock sync is unavailable on Sunday` for the
Sunday refusal, the fixed availability reason, or `controller did not verify the
change; refresh before trying again`. The cause stays in the operation audit and
the [service log](local-service.md#service-logs).

Status advertises native controls only when the controller implements them, the
configured offset is 3306, a supported cached snapshot exists, and no observation
or persistence error is outstanding. Unknown/missing snapshots require refresh.

## Rules

- Before any edit/send, the client MUST fetch fresh state, require the displayed
  fingerprint, the 3492-byte/18-ordered-slot layout and known mode, then persist a
  pending audit with that baseline. Persistence failure MUST send no command.
- Whole reads/controls share the existing bounded queue and client gate. Active
  work has a 15-second context; reconciliation has its own ten-second read-only
  budget while retaining the gate. Queued canceled work MUST send nothing.
- Each write MUST use the captured edit exchange and pacing. OFF MUST read back
  mode 0 with retained parameters; playback MUST read back mode 1 and exact slot
  parameters. TIMER MUST read back mode 2; its active parameters can change with
  native scheduling. Every other non-clock byte MUST remain unchanged.
- An already-satisfied request MAY complete without a control send. Operations
  MUST NOT infer physical illumination from TIMER mode or parameter equality alone.
- After any ambiguous send/readback failure, the client MUST make at most one
  independent read, with no repeat write. Matching desired state is `reconciled`;
  other or unreadable state is `uncertain`. A stale baseline sends no edit/write.
- Intent, actor, action, target, outcome and transport uncertainty MUST be auditable.
  Completion and the latest available observation MUST commit together. Failed
  reads retain the previous snapshot; errors and age remain visible.
- Pending intents survive process loss and become `uncertain` on service startup.
  Startup MUST neither contact hardware nor replay them. Persistence failures MUST
  block further controls until restart; refresh alone cannot clear that block.
- Schema 6 adds `control_audits(id,body)` referencing operations with cascading
  retention. Existing schemas MUST be backed up before migration. Backups/restores
  MUST support schemas 5–7 and retain control audits; restoration remains guarded.
- The UI MUST disable duplicate submissions, preserve errors across cached polls,
  distinguish reported OFF/MANUAL/TIMER, and label native slots separately from
  software Library designs. It MUST explain that manual/OFF lasts until another
  command and TIMER returns to controller scheduling, not the app Calendar.
  Household labels are “Turned off,” “Playing a pattern,” and “Following timers.”
  “Last checked” retains observation age. The clock-update acknowledgement remains
  explicit: a person gives it once per page load, and it stays given until reload
  (owner decision, 2026-10-31). Failure messages give a refresh/recovery action
  without wire details.
- Schema 7 ordinary controls MUST pause active Calendar ownership in their pending
  audit transaction before delivery, as specified by the [runtime contract](calendar-runtime.md).
- Authentication, origin checks, CSRF and restore-guard behavior MUST apply to this
  endpoint. The client MUST never retry a failed mutation automatically.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| Schema 6 control audit | Pending intent and final readback outcome |
| Home native controls | Cached snapshot and status capabilities |
| Settings operation history | Refreshes and native control audit results |

## References

- Rationale: [ADR-0011](../adr/0011-audited-household-native-controls.md).
- Context: [application design](../design/application.md).
- Related: [local service](local-service.md), [authentication](household-auth.md),
  [backup contract](backups.md), [roadmap](../plans/roadmap.md).
