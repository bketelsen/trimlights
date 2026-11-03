# 0017 — Delete house photos that the current layout does not use

- **Status:** Accepted
- **Date:** 2026-11-02

## Context

[ADR-0008](0008-private-photos-and-physical-mapping.md) stores at most 20
sanitized photos as SQLite blobs beside immutable house-layout revisions. Until
2026-11-02 a photo could not be removed: a wrong or private upload stayed in the
household, and it kept one of the 20 places. Each layout revision is an immutable
JSON body, and `house_photo_refs` holds a foreign key from each revision to each
photo that its views name. SQLite enforces those foreign keys, and backup refuses a
database that fails `PRAGMA foreign_key_check`. A photo can be named by the current
layout, by older revisions, or by neither.

The 2026-11-02 feature proposal gave 2 options. Option A refuses deletion while the
current layout uses the photo, and older revisions show "photo removed". Option B
deletes the photo and every layout view that names it, so it writes a new layout
revision as a side effect. Brian's 2026-11-02 night-shift instruction (T721a)
accepted the pick of option A.

## Decision

The service deletes a photo only when the current (newest) layout revision has no
view of it. Otherwise it refuses with 409 and explains that the household must
remove the photo's view and save the house first. The browser offers that step: it
removes the view from the draft, and the household saves as usual.

One SQLite transaction checks the current revision, deletes the photo's
`house_photo_refs` rows (which now belong only to older revisions), and deletes the
photo row with its image and thumbnail. Revision bodies stay unchanged: an older
revision can name a photo that no longer exists. The browser marks such a view
"photo removed", and "Use as draft" leaves those views out and says so. The
deletion runs with `secure_delete` on, and the service then asks SQLite for a
truncating WAL checkpoint, so the live database keeps no readable copy of the
blob after a successful checkpoint. Backups made before the deletion still hold the
photo; restoring one brings it back with the rest of that household state.

## Consequences

- A wrong or private upload can go away, and its place in the 20-photo limit is
  free again.
- The current layout always names existing photos, so saves, previews and backups
  keep their foreign-key guarantees. No schema change is needed; backups stay at
  schema 7.
- `house_photo_refs` no longer lists every photo an older revision body names: it
  lists the photos that each retained revision can still show.
- An older revision can no longer be restored exactly. Its other views and all of
  its runs still load as a draft.
- Deleting a photo that the layout uses takes 2 steps: remove its view and save,
  then delete. This is the cost of never changing the layout as a side effect.
- Deletion is permanent within the live database. Recovery needs an earlier full
  backup, which also rolls back every later change.

## Alternatives considered

- **Delete the photo and its views (option B):** one step, but it creates a layout
  revision that nobody reviewed and can silently discard mapping work.
- **Keep the blob and hide the photo:** keeps private content that the household
  wanted gone, and keeps the place in the 20-photo limit.
- **Rewrite older revision bodies:** breaks revision immutability
  ([ADR-0005](0005-versioned-household-patterns.md) applies the same rule to
  patterns).

## References

- Builds on: [ADR-0008](0008-private-photos-and-physical-mapping.md),
  [ADR-0009](0009-consistent-portable-backups.md).
- Shapes: [house contract](../specs/house.md),
  [backup contract](../specs/backups.md),
  [application design](../design/application.md),
  [roadmap](../plans/roadmap.md#house-photo-deletion--2026-11-02).
