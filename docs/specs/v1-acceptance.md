# Spec: Household v1 acceptance

Draft target contract, dated 2026-10-28. These are future release requirements, not
claims about the current prototype. Freeze API/data-format details alongside their
implementation. The owner confirmed the scope: shared controls, full calendar
scheduling, pattern editing, and manual roofline mapping with photo previews.

## Interface

| Surface | Required v1 capabilities |
| --- | --- |
| Home | Current scene, brightness, connection/freshness, favorites, off, temporary override, resume schedule, next transition |
| Schedule | Weekly and date-range rules, one-off exceptions, solar offsets, overnight windows, precedence, upcoming timeline and conflict explanation |
| Patterns | Import existing library, edit supported palettes/effects, duplicate, favorite, preview, apply, export/import, schedule references |
| House | Photo upload, manual roofline drawing, bulb counts/addresses/direction, calibration, multiple photo views, animated preview |
| Settings | Controller/topology, timezone/location, household access, diagnostics and backup/restore |
| CLI | clix conventions; serve/status/diagnostics plus supported application operations, JSON and meaningful dry-run behavior |

## Rules

1. Both users MUST sign in through Pocket ID and use the same household library,
   schedules and mappings. An unauthorized identity MUST not access household data.
2. Core tasks MUST work in iPhone Safari and as a home-screen web app at 375 CSS px
   width without horizontal scrolling. Controls MUST have accessible names,
   visible focus, sufficient contrast and touch targets of at least 44 CSS px.
3. Concurrent edits MUST detect stale versions and explain conflicts. A confirmed
   action MUST update the other open phone without a full page reload.
4. UI success for a physical change MUST depend on verified resulting state, or
   explicitly state that verification is pending/unknown. A timeout is not success.
5. One service instance MUST own controller communication. Browser tabs, CLI
   commands and the scheduler MUST use its serialized, paced worker.
6. Dry-run and local previews MUST send no controller mutations, including clock
   synchronization. Stale/offline UI MUST not silently defer physical actions.
7. Server-owned schedules MUST include date ranges, weekly selection, one-off
   overrides and local solar offsets. DST, overnight intervals and equal-priority
   overlaps MUST have documented deterministic behavior and fixture tests.
8. Restart/reconnect MUST apply the currently effective intent, not replay missed
   scenes. Manual overrides MUST expire predictably and support immediate resume.
9. Migration MUST preserve the existing controller configuration and identify
   unsupported native entries. Two competing schedulers MUST not run silently.
10. Editing or deleting a pattern used by a schedule MUST show its impact and
    preserve referential integrity. Unsupported effects/settings MUST be identified.
11. The owner MUST be able to upload an ordinary iPhone house photo, correct its
    orientation if needed, and map a roofline by drawing, counting and choosing
    direction. HEIC inputs MUST either convert successfully in the supported path
    or receive an actionable format-specific response; normal iPhone upload must
    be exercised during acceptance, not assumed from desktop JPEG tests.
12. Photo mapping MUST preserve normalized coordinates across resize/reload and
    keep physical bulb IDs distinct from image locations. Mirrored outputs and
    uncalibrated addresses MUST not imply independent controllability.
13. Preview MUST not affect the lights until Apply is chosen. Static/validated
    animations MUST match tested logical-pixel output. Approximate firmware-effect
    previews MUST be visibly labeled.
14. One backup/restore procedure MUST recover schedules, library, photos, geometry,
    revisions and required configuration. Restoration MUST be tested with hardware
    access disabled before replacing the running app.
15. The release MUST pass offline protocol/recovery tests, scheduler edge-case
    tests, mobile browser tests and a seven-night household trial. The trial MUST
    include a controlled service restart and controller reconnect with observed
    recovery. Any failure resets or extends the relevant trial coverage.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| API and storage contracts | Implemented domain model and migrations; exact routes/schemas added with code |
| Shared pattern vectors | Canonical pattern examples consumed by compiler and preview tests |
| Release checklist | Rules above plus each roadmap phase's Done when |
| Deployment runbook | Selected homelab host, Caddy route, Pocket ID client, backup/restore test |

## References

- Rationale: [ADR-0003](../adr/0003-shared-homelab-application.md).
- Context: [application design](../design/application.md).
- Delivery: [roadmap](../plans/roadmap.md).


The [local development service contract](local-service.md) records the smaller
implemented foundation; it does not satisfy the household acceptance criteria yet.
