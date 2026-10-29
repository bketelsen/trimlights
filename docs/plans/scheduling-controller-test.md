# Plan: Scheduling controller verification

Prepared **2026-10-29** for [roadmap Phase 4](roadmap.md#phase-4--full-scheduling-and-first-household-beta-l).
This plan collects the two missing hardware observations before exposing Calendar
activation: native timer behavior during MANUAL/OFF and the official app's Sunday
clock encoding. Preparation performs no controller operations. Execution requires
the owner present and ready for the selected session. Calendar stays disabled.

## Phase 1 — Capture and restoration setup

Implements the verification prerequisites in the
[scheduling design](../design/application.md#scheduling-and-control-ownership),
[native controls](../specs/native-controls.md), and
[handoff contract](../specs/calendar-handoff.md).

- Save a fresh private application backup and record firmware, local date,
  timezone/UTC offset, original mode, visible appearance, selected saved pattern,
  both native timers (all fields), library bytes and the reviewed baseline hash.
  Read the baseline through the existing household worker with the official app
  closed. Refresh can set the controller's clock; perform it before observations.
- Choose two visibly different existing saved patterns: A for the timer and B for
  manual playback. Confirm their actual appearance before starting. No pattern,
  brightness, topology or application calendar edits are part of this test.
- Capture on the AP carrying controller traffic, using its verified `any`
  interface. Set `TRIMLIGHT_CONTROLLER_HOST` in that SSH shell from private config:

  ```sh
  : "${TRIMLIGHT_CONTROLLER_HOST:?Set the controller address in this shell}"
  umask 077
  tcpdump -i any -nn -s 0 -U -w /tmp/trimlight-scheduling.pcap "host $TRIMLIGHT_CONTROLLER_HOST and tcp port 8586"
  ```

  Stop with Ctrl-C, retain the packet/drop counters, and copy the file into an
  ignored private evidence directory before removing the AP copy. Check capture
  completeness, TCP reassembly and encoded-frame checksums offline. Capture files
  contain private device details and must not become public fixtures.
- Log each user action and visible change with local time including UTC offset.
  During unattended boundaries, close the official app and make no refreshes,
  controls or direct CLI connections. Keep one controller owner at a time.
- **Done when:** baseline, rollback fields, A/B appearances, capture setup and
  an observation sheet are ready. A backup alone is not a device restore command.

## Phase 2 — Native timer interference

Tests the prerequisite for [ownership handoff](../specs/calendar-handoff.md) and
the [runtime](../specs/calendar-runtime.md), following the
[scheduling design](../design/application.md#scheduling-and-control-ownership).

### Choose the window

The no-edit option observes the existing timer's start and stop on three evenings,
one for each row below. It uses only the already supported household controls.

The shorter proposed option takes approximately **20–25 minutes** outside every
existing enabled timer window. At session start, explicitly agree to temporarily
use the currently disabled second timer through the official app. Leave the first
timer unchanged. Set timer 2 to today-only, pattern A, starting at least three
minutes ahead and ending two minutes later. Repeat with a new future start for
each row, leaving time to finish readback and setup between rows. Do not cross
midnight or a clock change. Record each temporary schedule in the capture.
If no disabled slot or conflict-free window exists, use the no-edit option.

This plan does not authorize a new Go schedule-writing command or reuse of the
disabled-slot diagnostic probe for enabled timer edits. Proposed temporary edits
are performed by the owner in the official app during the agreed session.

### Three trials, six observations

After each official-app timer edit, close that app. Refresh through the household
worker, check that only the agreed timer fields changed, and set the row's mode
using the existing household controls. Finish setup at least 60 seconds before
the start. Observe from 30 seconds before start until 60 seconds after stop.

| Mode before start | Expected at start if modes are isolated | Expected at stop if modes are isolated |
| --- | --- | --- |
| Follow timers (TIMER), positive control | Pattern A turns on | Lights turn off |
| Play pattern B (MANUAL) | Pattern B continues | Pattern B continues |
| Turn off (OFF) | Lights stay off | Lights stay off |

- The TIMER row must visibly fire at both boundaries; otherwise timing or setup
  is inconclusive and the MANUAL/OFF rows cannot establish isolation.
- Record actual behavior even when it differs from the table. If a timer
  interrupts MANUAL/OFF, stop the takeover approach and review an alternative
  design before enabling Calendar. Do not hide interference with repeated writes.
- Read back through the worker only after the uninterrupted observation window;
  preserve reported mode, selected parameters, timers and library for comparison.
  The visible observations are needed because a later read cannot show a brief
  transition that happened earlier.
- A missed boundary, competing app connection, unexplained clock jump or missing
  packets makes the affected row inconclusive. Retest that row in a fresh window.
- **Done when:** all six outcomes have timestamps, physical observations and
  matching post-trial records, followed by restoration below. Record firmware and
  tested timer recurrence; do not generalize today-only results to every recurrence.
  Before beta, also observe the enabled daily timer boundaries during app control.

### Restore and stop conditions

- Restore every temporary timer field in the official app, including disabled
  status, repetition, saved pattern, start and stop times. Close the official app.
- Through the household worker, verify original timers and library byte-for-byte,
  then restore the original mode using supported controls. When returning to TIMER,
  the expected appearance follows the current time, which may differ from arrival.
- Save a final comparison allowing only understood clock and playback-state
  changes. Record unknown differences; do not declare restoration complete while
  they remain. The owner confirms the final appearance.
- On loss of communication or an uncertain action, stop automated writes and
  record the last known state. Use the official app for owner-guided recovery once
  reachable, then obtain an independent final readback. Never blindly retry a write.
- **Done when:** temporary settings are gone, original mode/timers/library are
  confirmed, and private evidence records the result of every attempted trial.

## Phase 3 — Sunday clock support

Implements the clock prerequisite for the [runtime](../specs/calendar-runtime.md)
and [control design](../design/application.md#observed-native-playback--2026-10-28).

- The next real Sunday after preparation is **2026-11-01**. Prefer midday, well
  away from local clock changes and timer boundaries. Record local timezone and
  UTC offset. Keep automatic time enabled on the phone; do not change a phone,
  server or controller date to simulate Sunday on live hardware.
- Capture one short official-app local connection and refresh. Keep the household
  app idle; do not invoke its refresh/control actions while Sunday remains blocked.
  Note the phone date/time and the visible lights before and after. Close the
  official app after collection. Capture a second connection to confirm consistency.
- Offline, decode complete checksum-valid outbound clock-query payloads beginning
  `15 00 07`. Their fields are year-minus-2000, month, day, weekday, hour, minute,
  second. Compare the weekday byte with the recorded actual Sunday date, and check
  the returned clock/date fields. Never assume Sunday is 0 or 7 from another device.
- Add **synthetic** Sunday fixtures after the observed encoding is known. Retain
  original captures privately. Change `clockQuery` only after reviewing that
  evidence, with tests for every weekday and the date bounds. No bypass flag.
- Then prepare a separately reviewed real-Sunday Go clock/read verification using
  the observed bytes, actual local time and unchanged non-clock state. Keep
  ordinary Sunday writes gated until this verification passes. Code preparation
  and official-app capture alone do not complete Sunday hardware acceptance.
- **Done when:** official-app encoding, synthetic tests and real-Sunday Go readback
  agree, with original library/timers and expected appearance preserved.

## Phase 4 — Calendar activation and first evening

Follows the [handoff](../specs/calendar-handoff.md),
[runtime](../specs/calendar-runtime.md) and
[scheduling design](../design/application.md#scheduling-and-control-ownership).

- Review the observed timer results and record any new architecture decision
  before implementation. If timers interfere, resolve ownership before proceeding.
- Add the authenticated review/activation flow, bind reviews to the requesting
  household identity, and connect the runner to service startup under an explicit
  tested recovery policy. Preserve paused recovery on uncertainty and restore.
- Add next-change status, temporary changes and resume, then run a real evening
  using existing saved patterns. Include both daily-timer boundaries, an override
  and resumption, followed by restart/reconnect acceptance in the household trial.
- **Done when:** the app runs the agreed evening, the existing timers cause no
  surprises, and the owner can return to them from the app.

## Open questions

- Do MANUAL and OFF suppress both native start and stop events, including daily
  timers? Phase 2 supplies the evidence; positive control is mandatory.
- What weekday byte does this firmware use on Sunday? Phase 3 supplies the answer.
- Does TIMER return immediately select the appropriate current timer state?
  Record this on each restoration; test any unobserved outside-window case before
  offering it as a production rollback guarantee.

## References

- Implements: [application design](../design/application.md),
  [native controls](../specs/native-controls.md),
  [calendar handoff](../specs/calendar-handoff.md),
  [calendar runtime](../specs/calendar-runtime.md).
- Delivery: [roadmap](roadmap.md), [v1 acceptance](../specs/v1-acceptance.md).
- Rationale: [ADR-0011](../adr/0011-audited-household-native-controls.md),
  [ADR-0014](../adr/0014-review-calendar-ownership-transfers.md).
