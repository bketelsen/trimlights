# Spec: Dormant calendar ownership handoff

This contract governs the internal transfer coordinator and synthetic acceptance
tests. It does not expose activation to production or establish hardware behavior.

## Interface

The internal coordinator reviews a direction (`calendar` or `timer`) and applies
the server-created review for an actor. A review binds the controller request,
ownership version, calendar revision and pinned action when taking over, review
time, expiry and a copy of the existing native daily schedules. There is no HTTP
endpoint or CLI command. Schema 7 control audit JSON gains optional `handoff`
provenance: direction, ownership version, calendar revision/action where applicable,
and review validity interval. A control audit cannot be both delivery and handoff.

## Rules

- Review MUST use a canonical, error-free cached observation no more than five
  minutes old, with configured native controls and no pending ownership operation.
  It MUST perform no controller I/O or storage mutation.
- Calendar takeover MUST require inactive ownership, a saved calendar whose
  enabled references match native slots, and supported current intent. Review
  includes the immediate OFF or saved-slot action. TIMER return does not depend
  on calendar validity and never modifies native schedules.
- Review expires after at most 60 seconds, shortened to the current intent's end.
  Apply and the driver's pre-write callback MUST reject expired reviews, changed
  ownership, calendar revisions, desired actions, controller fingerprints or
  native schedules that differ from the review's copy.
  The transport context is bounded by review expiry as well as the worker timeout.
- The pending audit, observation and paused ownership checkpoint MUST commit
  together before a physical command. Pending intent retains the initiating actor.
  A calendar save or competing transfer before this commit rejects the transfer.
- Verified or reconciled completion with the expected ownership version changes
  status to active for takeover or disabled for TIMER. Completion stores the
  resulting canonical fingerprint with the audit. Takeover makes it the owned
  fingerprint; TIMER return clears the owned fingerprint and actor, so a disabled
  runtime has no owner. A concurrent pause MUST win.
- Unverified completion leaves Calendar paused. Persistence failure, interruption
  or cancellation MUST NOT trigger retry, replay or a compensating command.
  Startup recovery and backup restore preserve audit provenance and pause ownership.
- The existing driver MUST preserve native schedules/library bytes and perform
  only the reviewed OFF/play/TIMER action. This does not verify that firmware
  suppresses native timers during MANUAL/OFF; physical boundary tests remain required.
- Production startup MUST continue to suspend ownership without starting the
  runner. No review or acknowledgement can bypass the missing protocol evidence.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| Handoff audit | Reviewed direction, version, action, expiry and actual outcome |
| Ownership checkpoint | Pending handoff and verified completion in SQLite |
| Synthetic acceptance | Real worker/storage, fake controller and controllable time |

## References

- Rationale: [ADR-0014](../adr/0014-review-calendar-ownership-transfers.md).
- Context: [application design](../design/application.md#scheduling-and-control-ownership).
- Related: [runtime](calendar-runtime.md), [native controls](native-controls.md),
  [backups](backups.md), [roadmap](../plans/roadmap.md).

Hardware prerequisites are sequenced in the
[scheduling controller test plan](../plans/scheduling-controller-test.md).
