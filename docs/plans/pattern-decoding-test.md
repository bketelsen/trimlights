# Plan: Decode existing controller patterns

Prepared **2026-10-29** for [roadmap Phase 2](roadmap.md#phase-2--reliable-controller-service-l-highest-uncertainty)
and [Phase 3](roadmap.md#phase-3--shared-mobile-controls-and-pattern-library-ml).
This plan collects official-app traffic and matching light observations so existing
saved patterns can acquire accurate previews and, later, editable settings.
Preparation changes no controller or household data. The next live session can
combine this with the [scheduling test](scheduling-controller-test.md), in separate
stages with independent restoration checks.

## Phase 1 — Choose a recoverable test entry and record the starting state

Implements the [pattern design](../design/application.md#pattern-library-and-photo-mapping),
[pattern contract](../specs/patterns.md), and existing
[native control boundaries](../specs/native-controls.md).

- Finish the timer-interference session and confirm restoration before pattern
  edits. Keep Calendar execution disabled. Plan **30–45 additional minutes** for
  the core pattern capture, with optional cases deferred if time is short. Choose a
  window without native timer/event boundaries; pause between groups and restore
  the original entry if the session needs to end early.
- At session start, choose one existing, owner-approved expendable custom-pattern
  entry. Check both daily timers, the official app's dated events/combined effects,
  and our Calendar references; the entry must not be used by any of them or by
  the timer test. Agree to its temporary edits and the desired final state before
  touching it. Do not overwrite a favorite just to complete the test.
- Prefer an existing entry with settings fully visible and reproducible in the
  official editor. Save its name, slot ID, all settings, raw saved record and
  appearance. If it cannot be restored through the official app, do not use it.
  A captured record or application backup is not a supported device restore tool.
- Do not create/delete slots in this combined session: current household control
  validation expects the observed 18-entry layout. A duplicate/save-as-new action
  could change slot count or numbering. If no expendable entry exists, collect
  read-only editor/library observations and arrange a separate storage-layout test.
- Record firmware/app versions, local date/time with UTC offset, original mode,
  currently playing parameters, library order/count/IDs, schedules and output
  settings. Take an application backup and a fresh worker baseline with the
  official app closed; review its non-clock fingerprint. Record original custom
  color swatches as well if any palette picker edits could change shared swatches.
- Open the official app locally; leave the household app and CLI idle during
  editor work. The official app is the only writer for this session. Touch no
  controller settings, firmware, RGB order, output addresses, or bulb counts.
- **Done when:** the owner has identified the test entry and restoration path,
  reference checks are clear, and private baseline evidence is saved. Otherwise
  stop before any pattern edit.

## Phase 2 — Capture editing and saving separately

Implements evidence collection for the [pattern contract](../specs/patterns.md)
and [controller design](../design/application.md#pattern-library-and-photo-mapping).

The [official Edge manual](https://trimlight.com/hubfs/Manuals/trimlight-edge-manual.pdf)
(PDF pages 1, 5 and 7) describes preset selection, custom sequence colors/counts,
movement, brightness/speed, and saved-entry editing. It distinguishes black sequence
spaces from the X blackout option. Use the controls actually visible in the
installed app; record unavailable controls rather than inventing equivalent steps.

### Capture setup

Use the same AP as the scheduling capture, with a separate file. Set the controller
address from private configuration in the AP shell:

```sh
: "${TRIMLIGHT_CONTROLLER_HOST:?Set the controller address in this shell}"
umask 077
tcpdump -i any -nn -s 0 -U -w /tmp/trimlight-patterns.pcap "host $TRIMLIGHT_CONTROLLER_HOST and tcp port 8586"
```

Keep the capture running through reconnects and restoration. Stop with Ctrl-C,
record packet/drop counters, and copy it into ignored private storage before
removing the AP copy. If the capture must be restarted, use a new numbered file;
do not overwrite earlier evidence. A screen recording of the official editor is
useful if the owner chooses to make one; keep it private too.

Record each action ID with local time/UTC offset, the exact displayed setting,
Save/Apply button label, and what the lights do. Wait at least five seconds between
settled changes and ten seconds for moving effects. Slider drags can emit many
messages: record final values and release times, and retain intermediate packets.
Observe a slow moving marker for a full cycle where possible. Do not refresh from
our app during an edit or assume the final outbound packet is the saved value.

### Entry lifecycle, before the test matrix

First record the editor settings of two other existing saved patterns (**E01**,
**E02**) with different palettes/counts or effects, without saving edits. Selecting
them may play them; log their appearance. These are held-out examples for later
decoder checks. Return to the approved test entry before L00.

1. **L00:** select the entry, open its editor without changing anything, and record
   every displayed field. Observe whether entering the editor changes playback.
2. **L01:** return to the library and reopen it unchanged. Capture an official-app
   reconnect/refresh so there is a complete saved-library response for comparison.
3. **L02:** on the approved entry, establish the baseline below. Record each setup
   change separately. Capture the final live appearance **before** pressing Save.
4. **L03:** save back to that same entry using the visible edit/save action. Record
   any rename/overwrite prompt. If it would create another entry, cancel and stop
   to review the workflow. Do not assume closing the editor discards live changes.
5. **L04:** close and reopen the official app, reread the library, select the entry
   and record its settings and appearance. This separates saved state from a
   phone-local draft or temporary playback command.

**Baseline B:** a simple static sequence with distinct red/green/blue/black runs
of 2/3/5/7 bulbs, moderate brightness (25% if available), a recorded middle speed,
and the original/default direction. Record actual UI values and all otherwise
unused sequence spaces. These are proposed test values, not assumed wire values.
For numeric color channels, use exact values only if the app exposes them; a
visually selected hue is not an exact RGB measurement.

### Core matrix

For each row: begin at B (or the explicitly named moving baseline), make **one**
change, observe live output, save to the same entry, reconnect/reread in the
official app, then restore and save the baseline. Record both the changed and
restored states. Use `-change`, `-save`, `-read`, and `-restore` action suffixes.
A failed or unavailable row is recorded as such; it is not silently skipped.

| ID | Only change | Evidence sought |
| --- | --- | --- |
| P01 | First run red → green | Location and representation of one color |
| P02 | First run red → blue | Independent channel/order comparison |
| P03 | First run → unequal-channel custom color, if exact values are visible | Channel order versus preset/palette indexes; record shared swatch effects |
| P04 | First run count 2 → 1, then a separate trial 2 → 5 | Count byte(s), indexing and visible repeat length |
| P05 | Remove the last black run, then restore it | Active sequence length, unused entries and black-versus-empty distinction |
| P06 | Reorder the first two runs without changing their colors/counts | Entry stride and ordering |
| P07 | Brightness 25 → 0, then separate 25 → 50 trials, if percentages are available | Stored level, live level, and whether colors are rewritten/scaled |
| P08 | Static → one ordinary non-flashing moving effect | Effect selector, defaults altered by effect selection, visible movement |
| P09 | On saved moving baseline M, speed → low, then a separate high trial | Speed encoding and measured movement; same palette and brightness |
| P10 | On M, reverse once, then restore, if exposed | Direction flag versus reordered palette; physical direction from a fixed viewpoint |
| P11 | Change only the entry name to a short ASCII test name, then restore | Save target identity, name bytes, and whether edits renumber/reorder entries |
| P12 | Repeat P01, including its restore, in a new connection | Reproducibility and session-dependent fields |

If controls lack numeric percentages, record their displayed units and exact
available values rather than treating thumb position as a percentage. If effect
selection changes several fields, capture the full before/after settings and do
not assign all changed bytes to the effect ID. Establish and save **M** with the
P08 effect, otherwise matching B, before P09/P10; return to B afterward.

- Collect a complete saved-library response after each saved state, including
  each restore, using the official app. If reconnecting does not yield one,
  stop and investigate capture coverage before continuing many more rows.
- If selecting/saving a test changes other entries, count, slot IDs, timers or
  output configuration, stop and restore through the official app before resuming.
  Do not use the household worker against a changed/unrecognized layout.
- **Done when:** each core setting has labeled before/change/saved/readback/restore
  evidence, or an explicit unsupported/inconclusive result. Live output and saved
  output are both recorded; silent persistence assumptions are excluded.

## Phase 3 — Optional cases, then restore

Extends the [pattern design](../design/application.md#pattern-library-and-photo-mapping)
and [preview contract](../specs/patterns.md) after the core matrix is complete.

- **O01:** compare black with X blackout in an otherwise identical moving
  sequence, if exposed. Observe whether the dark positions move or stay fixed.
- **O02:** test one color count at 60, 61 and the official UI's displayed maximum,
  restoring between trials. Our current schema caps a run at 60; larger observed
  values must be preserved as unsupported until the schema/renderer are extended.
- **O03:** edit a later sequence space (for example 7, then 30 if available),
  with other spaces unchanged, to establish record stride and inactive padding.
- **O04:** inspect one existing built-in saved effect without editing it. Compare
  its settings/record with the custom sequence family. Optional preset mutation
  needs a second approved recoverable entry; do not replace the core test entry
  with an opaque preset whose restoration is unknown.
- Avoid flashing effects, music, combined shows and output zoning configuration.
  Wave/comet and other complex effects can be separate follow-up experiments.
- Restore the selected entry's complete original settings/name, plus any modified
  shared swatches. Save and reconnect in the official app. Confirm all original
  IDs, names and raw records, not just the visible test color. No test library
  entry should remain unless the owner chose that as the final state beforehand.
- Close the official app. If the original layout is restored and the date supports
  our clock encoding, read independently through the household worker and compare
  the library, schedules and topology to the reviewed baseline. Return to the
  original playback mode/pattern through supported controls. TIMER appearance
  follows the time at restoration. On Sunday, use the official app for recovery
  until the separate Sunday Go verification is complete.
- Preserve expected clock/current-playback differences separately from persisted
  state. Any unexplained difference prevents a successful restoration result.
  On lost communication or ambiguous saves, stop; do not blindly repeat commands
  or try replaying captured packets. The owner uses the official app for recovery.
- **Done when:** the original saved state and expected appearance are confirmed,
  and capture files, action notes, final comparison and unresolved rows are private.

## Phase 4 — Decode offline and prepare implementation

Follows the [pattern contract](../specs/patterns.md),
[pattern design](../design/application.md#pattern-library-and-photo-mapping), and
[ADR-0005](../adr/0005-versioned-household-patterns.md).

- Reassemble each TCP connection independently; handle retransmissions/duplicate
  AP interfaces, reject gaps/conflicting bytes, validate framing bounds and encoded
  checksums. Keep packet timestamps and match traffic to action IDs. Do not use a
  parser that merges reconnects sharing the same address/port tuple without TCP
  connection boundaries. Preserve plain acknowledgments as distinct message types.
- Diff decoded payloads, not XOR-encoded frames or checksums. Compare outbound live
  commands, saved-entry commands and complete returned records independently.
  Current parsing identifies 182-byte records containing ID, 25-byte name and
  156 parameter bytes; the latter remain opaque until these observations explain
  them. Do not infer a fixed global schedule offset after a library-layout change.
- Produce a field table with offset/width, proposed type/range, supporting action
  IDs, live-versus-persisted scope, untouched bytes and remaining uncertainty.
  Require independent values plus repeated baseline restoration for every mapping.
  Distinguish global brightness/speed from per-pattern values if evidence shows it.
- Check candidate decoding against at least two existing saved patterns and their
  official editor settings, without edits. Use different palettes/counts/effects;
  decoding one constructed sequence is insufficient for general native imports.
- Create fully synthetic fixtures for public Go/TypeScript tests. Keep captures,
  household names/colors, screenshots and deployment addresses out of Git.
  Record significant schema/adapter choices in a new ADR before implementation.
- First deliver read-only decoded views for verified pattern families, preserving
  all opaque bytes and unsupported effects. Decide how previously imported slots
  gain decoded views without overwriting renamed/favorited entries or pinned
  calendar revisions; current reimport deliberately skips known slots.
- A later write adapter needs its own reviewed baseline, fresh slot checks,
  serialized worker/audit flow, bounded physical edit/restore verification and
  no ambiguous retries. This capture plan adds no pattern-write API or permission
  to replay official-app commands. Software chase speed may differ from firmware;
  measure it before promising a matching animation.
- **Done when:** a reviewed field map is backed by the matrix and two existing
  entries, unsupported settings remain explicit, and synthetic decode/round-trip
  tests are ready. Write support remains a separate implementation checkpoint.

## Open questions

- Which existing entry is expendable and fully recoverable? Resolve at session start.
- Which controls affect the current playback only, shared swatches, or saved data?
- Do preset and custom patterns share the same parameter format?
- Does Save update in place, and what response establishes persistent success?
- What happens to unknown bytes, large counts, unused spaces and blackout masks?

## References

- Implements: [application design](../design/application.md),
  [patterns](../specs/patterns.md), [native controls](../specs/native-controls.md).
- Delivery: [roadmap](roadmap.md), [scheduling test](scheduling-controller-test.md).
- Rationale: [ADR-0005](../adr/0005-versioned-household-patterns.md),
  [ADR-0011](../adr/0011-audited-household-native-controls.md).
- UI reference: [official Edge manual](https://trimlight.com/hubfs/Manuals/trimlight-edge-manual.pdf),
  consulted 2026-10-29. Proposed trial values above are our experiment design.
