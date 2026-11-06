# Household guide: house photos and calendar

Living document. Rationale: [ADR-0006](../adr/0006-explicit-calendar-intent.md),
[ADR-0008](../adr/0008-private-photos-and-physical-mapping.md),
[ADR-0011](../adr/0011-audited-household-native-controls.md).
Contracts: [house photos and mapping](../specs/house.md),
[calendar and intent preview](../specs/schedules.md),
[native controls](../specs/native-controls.md).

## Overview

How the household uses the app's House, Library, Calendar and Home views:
photo uploads and roofline mapping, previews, calendar rules, and the native
controls.

## Design

### House photos and previews

In House, upload a JPEG, PNG or WebP (up to 12 MiB and 50 million pixels). The picker
requests supported formats so Safari can convert compatible photos. If HEIC still
arrives, export it as JPEG in Photos or Preview. A photo that needs more than
128 MiB to decode, such as a 16-bit PNG, a large progressive JPEG or a lossless
WebP, is refused; export it as a standard JPEG. Original files and metadata are
not retained. Up to 20 sanitized photos and their thumbnails stay in SQLite.

Add a roofline, set its output, first bulb and count, then tap its corners in order.
Use Move corners or percentage coordinates for precision, Move bulbs for perspective
adjustments, and separate runs for gaps. Multiple photo views can map the same run.
Mirrored runs reuse their source's logical colors. Rotation preserves mapped points.
Save the layout to preview it in Library or while selecting a calendar scene.

All mappings are uncalibrated. Preview colors come from the software pattern model;
opaque native effects cannot be previewed. Actual wiring and real-light comparison
still need verification. Photo deletion is not implemented. Export/import layout JSON to recover drafts;
imports are validated and loaded for review before saving. Layout JSON does not
include photos: restore a full backup to recover images and matching photo IDs.

### Calendar and preview

Add rules in Calendar, select a saved pattern revision or off, and save. Set the
household timezone and coordinates to use sunrise/sunset. “End on the following
day” represents overnight windows. Preview up to 30 local days; the timeline shows
winning and lower-priority rules, conflicts and daylight-saving adjustments.
Home offers native OFF, saved-slot playback and return to controller TIMER. These
controls do not upload software Library designs. Keep the official app closed,
refresh once to load controller state, and acknowledge possible clock synchronization.
After an uncertain command, refresh before another action. Native TIMER does not
mean the lights are currently on; the controller decides that from its timers.

Calendar saves and previews send no controller commands. Native timer handoff and
verified command delivery are still required before enabling execution.

Editing a pattern creates a new revision. Existing rules retain their pinned
versions, including after archival; update each rule explicitly to select a newer
version. Concurrent edits from two phones preserve the stale draft instead of
silently overwriting the saved version.

## Operational notes

An uncertain native command is not retried: refresh before another action
([native controls](../specs/native-controls.md)).

## References

- Rationale: [ADR-0006](../adr/0006-explicit-calendar-intent.md),
  [ADR-0008](../adr/0008-private-photos-and-physical-mapping.md),
  [ADR-0011](../adr/0011-audited-household-native-controls.md)
- Contracts: [house photos and mapping](../specs/house.md),
  [calendar and intent preview](../specs/schedules.md),
  [native controls](../specs/native-controls.md),
  [pattern library and previews](../specs/patterns.md)
- Design: [application](application.md)
- Built in: [roadmap](../plans/roadmap.md)
