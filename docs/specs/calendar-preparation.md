# Spec: Calendar preparation for native controller handoff

Authenticated Calendar preparation uses cached observations and immutable pattern
revisions. It does not acquire controller ownership, query hardware or send writes.

## Interface

Both POST endpoints require the existing authentication/CSRF boundary, JSON up to
128 KiB and quoted `If-Match` saved-calendar revision. Invalid input/reference is
400, missing precondition 428 and stale revision 409. Reads share one SQLite
transaction. Neither endpoint creates revisions, imports patterns or records
controller operations.

- `/api/v1/schedule/readiness`: `{plan}`. Returns `execution_enabled: false`,
  `timezone`, `checked_at`, `until` (seven elapsed days), `schedule_revision`, cached source
  timestamp/fingerprint, `blockers`, per-rule/override `bindings`, `current`,
  `next` (nullable) and evaluator `issues` for the stated horizon.
- `/api/v1/schedule/native-draft`: `{plan,today_only_date}`. The date is a required
  explicit `YYYY-MM-DD` in 1970–2199, interpreted in the plan timezone. Returns
  `execution_enabled: false`, source timestamp/fingerprint, `timezone`, `schedule_revision`,
  and `entries`. Each entry retains the decoded native schedule, has a `reason`,
  and has either one proposed `rule` or no rule. Unsupported/missing snapshots
  return 409 with refresh guidance. No raw controller data is sent by these endpoints.

## Rules

- Decode the known layout from cached raw bytes and verify its fingerprint; never
  trust derived cached pattern/schedule fields independently of those bytes.
- A pinned native design MUST validate and match original slot ID and all 156
  parameter bytes. Labels/favorites do not affect matching. No custom design is
  playable merely because its software preview works. Historical pinned revisions
  are resolved directly; later edits and archival do not silently replace them.
- Readiness checks every enabled rule, even outside the seven-day horizon, and
  every unexpired override. Disabled rules and expired overrides are shown but do
  not contribute pattern blockers. Overlap conflicts and evaluator issues are
  reported for the stated horizon only. The default intent is explicitly off.
- Missing, unsupported, errored, undated, future-dated or older-than-five-minute
  cached observations are blockers. A readiness request MUST NOT refresh them.
- Unverified Sunday clock encoding, native timer handoff and unavailable runtime
  execution are explicit blockers regardless of pattern matches. A report can
  never authorize a physical command. Future execution MUST recheck fresh state.
- Native copy supports repetition 1 (every day) and 0 (today only at the chosen
  date), with start strictly before off. Equal/overnight or unknown repetition
  stays visible without a proposed rule. Enabled/disabled is preserved. Clock numbers are retained in the draft timezone;
  copying does not infer or convert the controller timezone.
- Copy matches a current unarchived native Library revision by exact slot and
  parameters. Zero or multiple matching Library entries require explicit user
  resolution; do not choose by name. The user can import the controller library
  through the existing separate Library action. A changed slot is not remapped.
- Proposed rule IDs are `native-timer-1` and `native-timer-2`. Existing rule/override IDs are not
  overwritten. Copying appends only chosen rules, honors the 100-rule limit and
  preserves the current timezone, location, rules and absolute override expiry.
- The browser labels results stale after draft/revision/date changes and requires
  another review before adding a proposal. New requests do not overwrite later
  edits. Appending remains unsaved until the ordinary Calendar save succeeds.
- The native schedules MUST remain unchanged. No “enable” control is exposed.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| Readiness report | Canonical cached bytes, pinned designs and pure evaluator |
| Native rule proposals | Supported native entries and unambiguous Library matches |

## References

- Rationale: [ADR-0012](../adr/0012-prepare-native-calendar-handoff.md).
- Context: [application design](../design/application.md#scheduling-and-control-ownership).
- Related: [calendar contract](schedules.md), [native controls](native-controls.md),
  [roadmap](../plans/roadmap.md#phase-4--full-scheduling-and-first-household-beta-l).
