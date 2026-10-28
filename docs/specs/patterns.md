# Spec: Household pattern library and previews

This contract covers durable shared designs, opaque imported controller patterns,
revision conflicts and the deterministic logical-pixel preview. Hardware playback
is a separate capability and remains disabled until verified.

## Interface

| Endpoint | Method | Result |
| --- | --- | --- |
| `/api/v1/patterns` | GET | Current non-archived patterns, favorites first |
| `/api/v1/patterns` | POST | Create from one schema-1 design; return revision 1 |
| `/api/v1/patterns/{id}` | GET | Current revision and metadata |
| `/api/v1/patterns/{id}` | PUT | Create immutable revision; requires `If-Match: "N"` |
| `/api/v1/patterns/{id}/archive` | POST | Archive with `If-Match`; preserve all revisions |
| `/api/v1/patterns/{id}/restore` | POST | Restore an archived pattern with `If-Match` |
| `/api/v1/patterns/{id}/revisions` | GET | Retained revision history |
| `/api/v1/patterns/import-controller` | POST | Import previously unseen native slots from cached snapshot |

Browser mutations use the household auth/CSRF contract. Local CLI clients use the
existing loopback bearer API. Bodies are one JSON object, at most 32 KiB, with
`application/json` content type. Unknown fields and extra JSON values are rejected.
Import, archive and restore accept `{}`. List with `?include_archived=1` to include
archived entries. Create also serves import/duplicate: it always assigns a new
application ID, never imports identity or overwrites an existing pattern.

```json
{"schema":1,"name":"Porch warmth","favorite":false,"effect":"static","brightness":60,"speed":0,"reverse":false,"palette":[{"color":"#ffb05a","count":3},{"color":"#000000","count":1}]}
```

Names contain 1–80 Unicode characters without control characters or outer spaces.
Custom effects are `static` or `chase`; brightness and speed are integers 0–100.
Palettes contain 1–30 runs, each a #RRGGBB color and count 1–60. Native designs use
`effect: native`, empty palette, brightness 100, speed 0, reverse false, plus a
`native` object preserving slot ID, original name, 156 raw parameter bytes as hex,
and capture timestamp. A native design has no simulated color output.

## Rules

- Application IDs MUST remain independent from controller slot IDs.
- Each save MUST atomically add an immutable revision and update the current
  pointer. Every revision records its actor and UTC time.
- A stale revision MUST return 409 and leave saved state untouched. Missing
  `If-Match` MUST return 428; invalid versions MUST return 400. Archived patterns
  cannot be edited; archive increments the version so stale edits fail.
- Archive MUST retain revision history. Future schedules MUST reference retained
  revisions and expose any impact before changing those references.
- Restore MUST create a new revision and retain the archive event. Native design
  updates MUST preserve their original opaque parameters; names and favorites
  remain editable. Convert by creating a separate design, not changing provenance.
- Controller import MUST use cached state only. It MUST preserve raw parameters,
  be atomic, and skip previously imported slots without overwriting edits.
- Export/import MUST use only the design schema, excluding IDs and credentials.
- List views SHOULD refresh shared state while visible. An active editor MUST
  retain unsaved changes when a newer server revision arrives and report conflicts.
- Unsaved new, copied, imported and edited patterns MUST warn before tab navigation,
  editor exit, revision replacement or reload. Browser reload/close uses its native
  unsaved-changes warning. Canceling a warning MUST retain the draft.
- A successful save MUST update the saved baseline without replacing edits made
  after submission. Those newer edits remain visibly unsaved and the next save
  uses the returned revision. A response to an editor that has been closed MUST
  NOT reopen it. Failed saves MUST retain the draft.
- Preview MUST never call the controller or claim physical playback is verified.
  Native effects MUST show an unsupported-preview explanation.
- Preview RGB channel scaling is integer `(channel * brightness + 50) / 100`,
  rounded down. Palette runs repeat over logical addresses 0–2047. Reverse maps
  address i to `paletteLength - 1 - i`. Chase shifts that index backwards by
  `floor(tickMS * speed / 10000)`; speed 0 freezes motion. Negative indices wrap.
- Preview ticks are integer milliseconds in 0–86,400,000. Rendering the same
  design/count/tick in Go and TypeScript MUST match the shared golden vectors.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| SQLite schema 3 | Pattern heads, immutable revisions and controller-import registry |
| `web/src/pattern-vectors.json` | Shared expected logical-pixel colors for Go and browser tests |
| Browser strip preview | Canonical design and renderer; photo mapping reuses these colors |

## References

- Rationale: [ADR-0005](../adr/0005-versioned-household-patterns.md).
- Context: [application design](../design/application.md).
- Authentication: [household auth](household-auth.md).
- Delivery: [roadmap](../plans/roadmap.md).

Calendar references are indexed with each saved calendar revision. The editor and
archive dialog list current references and their pinned versions; both wait for
that lookup before enabling the save/archive action. Existing scheduled revisions
remain retained after archival. See the [calendar contract](schedules.md) and
[ADR-0006](../adr/0006-explicit-calendar-intent.md).
