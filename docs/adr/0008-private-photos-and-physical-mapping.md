# 0008 — Separate private photo views from physical bulb addresses

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

House previews are part of v1. Multiple photos can show the same physical bulbs;
a drawn roofline alone cannot establish wiring or independently controlled outputs.
The Go service runs without a native image conversion runtime.

## Decision

Store bounded, re-encoded JPEG photos and thumbnails as SQLite blobs alongside
immutable, compare-and-swap house-layout revisions. Keep at most 20 photos, each
with a 4096-pixel maximum edge, so media and geometry share atomic database backups.
Do not retain original files, filenames, EXIF or GPS metadata. Authenticate every
media read. Decode JPEG, PNG and WebP; reject HEIC with actionable conversion advice.
Request those supported types explicitly in the browser picker to allow Safari's
native conversion. Correct EXIF orientation and support manual quarter-turn views.

A layout has physical runs (output, first address, count, optional mirrored source)
and photo views mapping run IDs to normalized polylines and optional bulb positions.
Interpolation uses image aspect ratio. Reverse changes only a view's placement;
physical addresses remain stable. Keep all mappings uncalibrated until a verified
hardware identification workflow exists. Use the canonical pattern renderer for
software previews; refuse to invent previews for opaque native effects.

## Consequences

Backups can capture photos and their references consistently. Database backups
become larger; input byte/pixel limits, one decoding operation at a time and a
household photo quota bound resource use. Physical calibration and real iPhone
upload acceptance remain required. JPEG re-encoding loses transparency and may
change color appearance; a preview is not a photometric prediction.

## Alternatives considered

- **Original files on disk:** retain unnecessary metadata and complicate atomic
  restore. Reconsider external blob storage if household media limits grow.
- **Native HEIC libraries:** add runtime dependencies; explicit picker conversion
  and a format-specific fallback meet the initial acceptance contract.
- **Photo-only bulb numbering:** cannot represent shared views or mirrored outputs.

## References

- Builds on: [ADR-0003](0003-shared-homelab-application.md),
  [ADR-0005](0005-versioned-household-patterns.md).
- Shapes: [application design](../design/application.md),
  [house contract](../specs/house.md), [roadmap](../plans/roadmap.md).
