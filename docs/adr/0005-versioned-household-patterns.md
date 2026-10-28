# 0005 — Version household patterns independently from controller slots

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

Both household members need to create and edit patterns without silently replacing
each other's changes. Controller slots and proprietary effect parameters are only
partially understood. Scheduling and photo previews need stable pattern identities
and reproducible revisions independently of hardware storage limits.

## Decision

Give each household pattern a random application ID and immutable revisions.
Maintain a current revision pointer with compare-and-swap updates. Archive patterns
instead of deleting their revisions; future schedule references can retain their
chosen revision. Record actor and time with every revision and archive operation.

Use a versioned palette schema shared by Go logical-pixel rendering and browser
preview. Initially support static palettes and a deterministic chase preview.
These are application designs; controller playback remains unverified and disabled
until an adapter is tested. Do not equate a matching software preview with measured
physical behavior.

Import existing controller patterns from the cached snapshot as opaque native
references. Preserve ID, original name, raw parameters and snapshot time. Do not
invent their palette or animation. Imported native patterns can be renamed,
favorited, duplicated and exported, but palette editing starts a separate design.
Importing again skips slots already imported and does not overwrite household edits.

## Consequences

Two phones cannot silently overwrite the same revision. Older revisions remain
available for scheduling and recovery. Drafts can be edited offline on screen,
but saving requires an explicit online request; no writes are silently queued.
Native effects remain usable as retained data while their decoding is incomplete.
Revision retention grows storage and must be included in backups.

## Alternatives considered

- **Controller IDs as application IDs:** ties library size and edits to mutable
  slots and makes previews dependent on hardware access.
- **Last write wins:** loses household edits without warning.
- **Guess native effect palettes:** could mislead previews and cause wrong output.

## References

- Builds on: [ADR-0003](0003-shared-homelab-application.md).
- Shapes: [application design](../design/application.md),
  [pattern contract](../specs/patterns.md), [roadmap](../plans/roadmap.md).
