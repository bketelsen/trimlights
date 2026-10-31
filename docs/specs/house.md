# Spec: Private photos and roofline mapping

This contract governs household photo ingestion, layout persistence and local
previews. Hardware calibration and playback remain separate acceptance work.

## Interface

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/api/v1/photos` | GET | List photo IDs, neutral display names and oriented dimensions |
| `/api/v1/photos` | POST | Raw image body; creates a sanitized photo |
| `/api/v1/photos/{id}/image` | GET | Authenticated full JPEG |
| `/api/v1/photos/{id}/thumbnail` | GET | Authenticated thumbnail JPEG |
| `/api/v1/house` | GET, PUT | Current house layout; PUT requires quoted If-Match revision |
| `/api/v1/house/validate` | POST | Validate an imported layout and existing photo references without saving |
| `/api/v1/house/revisions` | GET | Immutable saved layouts |

Layout schema 1 contains `runs` and `views`. Each run has a stable `id`, `name`,
`output`, one-based `start`, `count`, and optional `mirror_of` run ID. A view has a
`photo_id`, `turns` (0–3 clockwise quarter turns) and `mappings`. Each mapping names
`run_id`, normalized `points`, `reverse`, and `adjustments` with zero-based bulb
`index` and a normalized `point`. All physical mappings are uncalibrated.

## Rules

- Every photo/layout route MUST enforce household authentication and existing CSRF
  rules for writes. Media MUST be no-store, image/jpeg and nosniff.
- Uploads MUST be at most 12 MiB, 50 million decoded pixels and 16384 pixels on
  either edge. The service MUST inspect dimensions before decoding and serialize
  decoding; a concurrent upload receives 429. At most 20 photos may be stored.
- Before decoding, the service MUST estimate decoder memory from the headers
  (color model, JPEG sampling factors, components and progressive coding) and
  refuse an estimate above 128 MiB with export advice. A PNG is budgeted from
  its IHDR bit depth, color type and interlacing; a non-paletted PNG counts as
  NRGBA (4 bytes per pixel) or NRGBA64 (8 bytes per pixel, 16-bit), because a
  later tRNS chunk can select those. The first PNG chunk MUST be a 13-byte IHDR
  with the dimensions that DecodeConfig reports; otherwise the upload is refused
  as an unsupported format before decoding. A WebP is budgeted from its actual image
  chunk (VP8 or VP8L) and ALPH compression, not from the VP8X header. Lossless
  data (VP8L, or ALPH compression 1) adds the worst-case Huffman storage of
  golang.org/x/image/vp8l: up to 2600 groups, about 206 MiB. In effect, only
  lossless WebP images up to about 144 by 144 pixels fit; larger ones are refused
  with advice to save the photo as JPEG or PNG. A three-component JPEG reserves
  a converted RGBA image (4 bytes per pixel) when its component IDs are R, G, B
  or any Adobe APP14 segment in the file has transform 0, because the decoder
  reads such markers after the point where its configuration stops. A 16-bit
  PNG or a large progressive JPEG can be refused below the pixel limit; a
  baseline 4:2:0 JPEG at 48 million pixels (72 MB) and a progressive 4:2:0 JPEG
  at 12 million pixels (90 MB) are accepted. Scaling MUST read the decoded image directly and apply EXIF orientation to the working
  image, so one upload peaks near 210 MiB (12 MiB input, 128 MiB decode, 64 MiB
  working image).
- JPEG, PNG and WebP MUST be decoded and re-encoded without original metadata;
  EXIF orientation 1–8 MUST be applied. Unsupported HEIC MUST return conversion
  advice. Full images have maximum edge 4096; thumbnails have maximum edge 512.
- Layouts MUST have at most 40 runs and 20 views, with addresses up to 2048 per output and at most 8192 bulbs across all runs.
  Run IDs, output IDs and names MUST be bounded. Address ranges on an output MUST
  not overlap. Mirrors MUST reference a non-mirrored run with the same count.
- Views MUST reference existing photos and runs. Normalized coordinates MUST be
  finite and in [0,1]; polylines contain 2–100 points and have nonzero length.
  Adjustments MUST reference unique valid bulb indices. A run appears once per view.
- Geometry MUST interpolate by aspect-correct arc length, including both endpoints.
  A one-bulb run uses the midpoint. Reverse preserves physical address identity.
- Saves MUST be atomic with retained revisions. Stale saves return 409; missing
  If-Match returns 428. An editor MUST retain its conflicted draft.
- Software previews MUST use the canonical pattern renderer and never contact the
  controller. Opaque native effects MUST display an unavailable-preview explanation.
- Reduced-motion preference MUST prevent automatic house animation, while an
  explicit Play animation action MUST work. Pause/resume uses the shared
  [preview clock contract](patterns.md#rules).
- Layout JSON imports MUST be bounded to 512 KiB and validated by the service before
  rendering. Import MUST preserve the current draft on validation failure, require
  explicit replacement of unsaved edits, and load a reviewable draft without saving.
  Photos are excluded; missing photo IDs require restoring the matching full backup.
- Upload, validation and save failures MUST answer with JSON `{"error": text}`, where
  4xx text is safe to show the household. The browser MUST show that text for a 4xx
  answer, and its own wording for 409 conflicts, the 409 photo limit, 403 and gateway
  pages that are not this JSON (for example a proxy's 413, 429 or 502 HTML).
- Upload originals MUST NOT be retained or copied into the public repository.
  Physical calibration MUST NOT be inferred from uploaded photos or user geometry.

## Derived artifacts

SQLite schema 5 includes sanitized photos, layout revisions and photo-reference
foreign keys. Shared geometry examples exercise Go and TypeScript interpolation.
House views use the existing pattern renderer and its golden vectors.

## References

- Rationale: [ADR-0008](../adr/0008-private-photos-and-physical-mapping.md).
- Context: [application design](../design/application.md).
- Delivery: [roadmap Phase 5](../plans/roadmap.md#phase-5--house-photos-roofline-mapping-and-previews-l).
- Acceptance: [household v1](v1-acceptance.md).
