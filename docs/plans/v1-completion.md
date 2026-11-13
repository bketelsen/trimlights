# Plan: Completing household v1

Prepared **2026-11-13**. The [roadmap](roadmap.md) records what was built through
2026-11-02. This plan sequences what is left until the
[v1 acceptance contract](../specs/v1-acceptance.md) passes. It does not replace
the roadmap phases; it orders their open items around the one scarce resource,
which is time with the owner at the house. The two prepared hardware plans
([scheduling](scheduling-controller-test.md), [pattern decoding](pattern-decoding-test.md))
are steps inside this plan.

## Starting point — 2026-11-13

Software: main passes CI. The deployed release is healthy and differs from main
only by repository housekeeping. Sign-in, the pattern library, calendar editing
and preview, calendar preparation, house photos and the roofline editor, native
OFF/saved-pattern/TIMER controls, backup and restore are implemented and tested.
The calendar runner and ownership handoff are implemented and dormant.

Household data in the running instance (read-only inspection, 2026-11-13):

- No one has used the app since the evening of 2026-10-28. The only stored
  session has expired, and only one household identity has ever signed in.
- The library holds the imported controller patterns and one application pattern.
  All imported patterns are opaque: none has a preview.
- The calendar has no rules. Calendar ownership is disabled.
- The house has one photo and two placeholder runs of 20 bulbs on one output.
  This is test data, not the real roofline. Nothing is calibrated.
- The controller history is one refresh and four verified controls, all from the
  acceptance check of 2026-10-28.

Hardware evidence: none of the prepared sessions has run. The planned Sunday
capture of 2026-11-01 did not happen. Five facts are still unknown, and each one
blocks a part of v1:

| Unknown | What it blocks today |
| --- | --- |
| Sunday weekday byte in the clock query | Every refresh and control is refused on Sundays |
| Whether MANUAL and OFF suppress native timers | Calendar activation; no entry point exists |
| Output topology (the official app shows 4 ports, each 1 to 600) | Real run addresses, mirrors and calibration |
| Saved-pattern parameter encoding | Previews of controller patterns, custom patterns on the lights, brightness |
| Snapshot layout when the saved-pattern count is not 18 | Adding or deleting a pattern in the official app makes the controls refuse |

## Order of work

| Phase | Owner at the house | Depends on |
| --- | --- | --- |
| 1. Household setup | No (phones only) | Nothing; start now |
| 2. Sunday clock | 10 minutes on a Sunday, twice at most | Nothing; date-bound |
| 3. Evidence session | One session, 75 to 90 minutes | Nothing; not on a Sunday |
| 4. Calendar ownership | One evening | Phases 2 and 3 |
| 5. Calibrated mapping | One evening, 30 to 45 minutes | Phases 1 and 3 |
| 6. Patterns on the lights | One short session | Phase 3 |
| 7. Trial and release | Seven nights | Phases 4 and 5; Phase 6 or its cut |

Phases 1 to 3 need no new code and can all start this week. Phases 4, 5 and 6
are independent of each other once Phase 3 has run.

## Phase 1 — Household setup without hardware (S)

Closes open items of [roadmap Phase 3](roadmap.md#phase-3--shared-mobile-controls-and-pattern-library-ml)
and [Phase 5](roadmap.md#phase-5--house-photos-roofline-mapping-and-previews-l)
under the [house contract](../specs/house.md) and
[household authentication](../specs/household-auth.md). No controller command
is involved.

- Both household identities sign in on their own phones. Check that an identity
  outside the allowlist is refused ([acceptance rule 1](../specs/v1-acceptance.md#rules)).
- Take the house photo with the iPhone camera and upload it from the phone
  (rule 11). Try one HEIC file and confirm the conversion advice.
- Make a physical inventory before drawing: which controller ports have a strip
  connected, where each strip starts, its direction, the corners, and the bulb
  count of each segment (counted, or from the installation record). Keep the
  inventory in private storage.
- Replace the two placeholder runs with the real rooflines from that inventory.
  The runs stay uncalibrated; Phase 5 confirms them against the lights.
- Mark favorites. Draft the real calendar from the imported controller patterns,
  starting with "copy native timers" in [calendar preparation](../specs/calendar-preparation.md),
  and read next week's preview on both phones. The calendar stays planning-only.
- Take a backup and repeat the isolated restore drill with this populated data
  ([backup operations](../design/backup-operations.md), rule 14).
- **Done when:** both identities see the same library, calendar draft and house
  on their phones; the layout shows the real rooflines with inventory counts; a
  populated backup restores in isolation with its photo.

## Phase 2 — Sunday clock (S, date-bound)

Runs [scheduling test Phase 3](scheduling-controller-test.md#phase-3--sunday-clock-support)
for the [control design](../design/application.md#scheduling-and-control-ownership).

- Capture the official app's clock query on a real Sunday at midday. The next
  Sundays are **2026-11-15** and **2026-11-22**; United States clocks changed on
  2026-11-01, so neither is near a transition.
- Add synthetic fixtures for the observed weekday byte, change `clockQuery` in
  `internal/trimlight/protocol.go` with tests for every weekday, and remove the
  Sunday refusals in `internal/trimlight/playback.go` in the same reviewed change.
- Run the real-Sunday Go read on the same day if the change is reviewed in time;
  otherwise on the following Sunday.
- **Done when:** on a Sunday, a refresh through the deployed app succeeds, the
  controller clock reads back correctly and all non-clock bytes are unchanged.

## Phase 3 — One combined evidence session (M)

One session on a day other than Sunday, outside every native timer window, with
the access-point capture running. It collects everything Phases 4 to 6 need, in
this order, with a restoration check between stages. Calendar stays disabled.

- **Timers:** [scheduling test Phases 1 and 2](scheduling-controller-test.md#phase-2--native-timer-interference),
  the short option with the disabled second timer (20 to 25 minutes).
- **Topology:** record the official app's output settings for each port, and
  observe which ports drive lights and whether two ports show the same bulbs.
  This is read-only. It replaces the "may share addresses" hypothesis with facts
  for the [layout model](../design/application.md#pattern-library-and-photo-mapping).
- **Patterns:** the core rows of the [pattern decoding test](pattern-decoding-test.md)
  on the owner-approved expendable entry (30 to 45 minutes). Brightness rows are
  required, because Home brightness depends on them.
- **Ruler:** as the last pattern edit, save the expendable entry as a ruler: a
  low-brightness sequence of distinct colors in fixed blocks (for example 6
  colors of 5 bulbs), with a period longer than any doubt about a run's start.
  Record its exact sequence. Phase 5 plays it through the already verified
  saved-pattern control, so calibration needs no new wire command.
- **Layout (only if time remains, last):** add one saved pattern in the official
  app, capture a refresh, delete it and capture again. The household controls
  refuse while the count is not 18, which is the safe result. This gives the
  evidence for variable-layout parsing.
- Untested hypothesis to check against the captures: the 156-byte parameter
  block fits a 6-byte header and 30 records of 5 bytes, which matches the
  30-run palette limit of the [pattern contract](../specs/patterns.md).
- **Done when:** private evidence holds the six timer observations, the port
  facts, the decoding rows and the ruler's sequence; timers, library (apart from
  the agreed ruler entry) and mode are restored and read back.

## Phase 4 — Calendar ownership and the first scheduled evening (L)

Runs [scheduling test Phase 4](scheduling-controller-test.md#phase-4--calendar-activation-and-first-evening)
and completes [roadmap Phase 4](roadmap.md#phase-4--full-scheduling-and-first-household-beta-l)
under the [handoff](../specs/calendar-handoff.md) and
[runtime](../specs/calendar-runtime.md) contracts.

- Before the Phase 3 results: add to Home the next change and its reason from
  the saved calendar, labeled planning-only, and the favorites. These read the
  evaluator and send nothing.
- Record the timer result in an ADR. If MANUAL or OFF does not suppress native
  timers, stop: the takeover design changes (the likely alternative disables the
  native timers at handoff, which needs a new write authorization).
- Then add the signed-in review and activation flow bound to the acting identity,
  start the runner from `service.Run` under the tested recovery policy, and add
  "temporary change", "resume calendar" and "return to controller timers" to
  Home (acceptance rules 8 and 9). Add the new controller-write files to
  `.github/risk-paths.json`.
- Decide the outage behavior from the timer evidence: a verified native off
  fallback, or a documented "last state stays" rule.
- **Done when:** the calendar runs one real evening including both native timer
  boundaries, a temporary change and an automatic resume; a restart and a
  reconnect select the correct current scene; the owner returns to native timers
  from the app.

## Phase 5 — Calibrated mapping (M to L)

Completes [roadmap Phase 5](roadmap.md#phase-5--house-photos-roofline-mapping-and-previews-l)
under [ADR-0008](../adr/0008-private-photos-and-physical-mapping.md), the
[house contract](../specs/house.md) and
[native controls](../specs/native-controls.md).

- Write an ADR first: calibration is an owner observation of the ruler entry,
  played and ended by the audited saved-pattern and TIMER controls. It refines
  ADR-0008's "identify action" without a new protocol command.
- Layout schema 2: each run can carry a calibration record (ruler sequence,
  layout revision, actor, time). Any change to a run's output, start, count,
  direction or mirror clears it. Update the migration, the backup schema
  support and round-trip tests, and the shared Go and TypeScript vectors.
- House gets a Calibrate mode: play the ruler, draw the predicted ruler over the
  photo from the current mapping, let the household shift the start, count and
  direction of each run until the photo matches the house, confirm each run,
  then return to the previous mode. Previews label calibrated and uncalibrated
  runs. Settings shows the outputs and mirrors recorded in Phase 3.
- **Done when:** every run is calibrated or visibly marked unverified, the
  second phone shows the same result, and one static controller pattern on the
  lights matches its photo preview in order and color.

## Phase 6 — Patterns on the lights (L, highest uncertainty)

Closes the pattern items of [roadmap Phase 2](roadmap.md#phase-2--reliable-controller-service-l-highest-uncertainty)
and [Phase 3](roadmap.md#phase-3--shared-mobile-controls-and-pattern-library-ml)
under the [pattern contract](../specs/patterns.md) and
[pattern design](../design/application.md#pattern-library-and-photo-mapping).
It can run beside Phases 4 and 5 and must not delay them.

- Decode the parameter block from the Phase 3 evidence, with synthetic
  fixtures. Imported patterns then get previews: exact for decoded static and
  chase sequences, labeled approximate for firmware effects (rule 13).
- Parse the snapshot for any saved-pattern count, if the layout evidence
  exists, so an official-app edit no longer disables the controls.
- Write an ADR and a bounded verification contract, modeled on
  [ADR-0010](../adr/0010-bounded-playback-verification.md), for saving one
  application pattern into the reserved expendable entry and for brightness.
  Run it once with the owner present, with baseline, readback and restoration.
- After that verification: Apply in the Library and brightness on Home, through
  the existing worker with pending audit and no retry of an ambiguous write.
- **Cut line:** if the evidence does not support a safe write by the time
  Phases 4 and 5 are done, record an ADR that v1 applies controller-saved
  patterns only, amend the acceptance contract, and move upload and brightness
  to a later release. The owner decides; see the open questions.
- **Done when:** a pattern edited on a phone is applied and the lights match
  its preview, or the cut is recorded and the interface says so plainly.

## Phase 7 — Trial and release (M)

Completes [roadmap Phase 6](roadmap.md#phase-6--operations-household-trial-and-v1-release-m)
and [acceptance rule 15](../specs/v1-acceptance.md#rules), with the
[deployment](../design/deployment.md) and [household guide](../design/household-guide.md).

- Physical checks that automation cannot make: iPhone Safari and the home-screen
  app at 375 px, screen-reader announcements, the navigation and reload
  warnings, and two phones seeing each other's confirmed change (rules 2 and 3).
- Failure drills: expired sign-in, slow and offline network, controller
  unplugged and reconnected, service restart, a second service instance refused.
- Seven nights with both users: normal nights, one exception, one temporary
  change, one controlled restart and one reconnect. A failure repeats the
  affected coverage after the fix.
- Final populated backup, isolated restore and one upgrade with rollback. Update
  the household guide and the deployment runbook. Freeze the acceptance
  contract. Brian tags the release with `make bump`.
- **Done when:** the trial passes, restore and rollback are demonstrated, and
  both users do the v1 tasks without help.

## Later / ideas

The roadmap's [later list](roadmap.md#later--ideas) is unchanged. In-app backup
controls, automated retention and off-host copies also stay out of v1 unless the
second open question says otherwise.

## Open questions

- **Custom patterns in v1:** is uploading application patterns and brightness
  required for v1, or is the Phase 6 cut acceptable? The owner decides after
  the Phase 3 evidence. Recommended: accept the cut if the write is not
  verified when Phases 4 and 5 finish, so the trial is not held up.
- **Backup in Settings:** the acceptance table lists backup and restore under
  Settings; they exist as CLI commands with a runbook. Recommended: accept the
  CLI for v1 and amend the table when the contract is frozen in Phase 7.
- **Timer suppression:** decided by Phase 3. A negative result reopens the
  handoff design before any Phase 4 activation code.
- **Topology:** how many ports are connected and whether they mirror is decided
  by Phase 3 and fixes the run model before Phase 5.

## References

- Implements: [application design](../design/application.md),
  [v1 acceptance](../specs/v1-acceptance.md), [house](../specs/house.md),
  [patterns](../specs/patterns.md), [native controls](../specs/native-controls.md),
  [calendar handoff](../specs/calendar-handoff.md),
  [calendar runtime](../specs/calendar-runtime.md).
- Sequences: [roadmap](roadmap.md),
  [scheduling controller test](scheduling-controller-test.md),
  [pattern decoding test](pattern-decoding-test.md).
- Rationale: [ADR-0008](../adr/0008-private-photos-and-physical-mapping.md),
  [ADR-0010](../adr/0010-bounded-playback-verification.md),
  [ADR-0011](../adr/0011-audited-household-native-controls.md),
  [ADR-0014](../adr/0014-review-calendar-ownership-transfers.md).
