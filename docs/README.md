# Documentation

Implementation baseline: 2026-10-28. The clix CLI and local service foundation
exist. OIDC login and the initial read-only mobile UI are implemented and tested
locally. An initial TrueNAS/Caddy deployment passes HTTPS, readiness and login-redirect
checks with controller access disabled. The shared pattern editor now supports
revisions, conflict detection, favorites, import/export and software previews.
Desktop Pocket ID sign-in is confirmed. Both-phone acceptance, hardware
controls, schedule execution and physical calibration remain in progress. House
photo uploads, manual mapping and software previews are implemented. Calendar editing,
versioned saves and deterministic intent preview are implemented.

This directory starts the four-question taxonomy from
[agentic-template](https://github.com/bketelsen/agentic-template). Its category
templates and canonical agent instructions have been adopted in
[roadmap Phase 1](plans/roadmap.md#phase-1--repository-foundation-and-clix-m).

| Directory | Question | Index |
| --- | --- | --- |
| adr | Why? | [0001 — Record decisions](adr/0001-record-architecture-decisions.md), [0002 — Portable instructions](adr/0002-agent-portable-instruction-surface.md), [0003 — Application architecture](adr/0003-shared-homelab-application.md), [0004 — Pocket ID and TrueNAS](adr/0004-pocket-id-and-truenas.md), [0005 — Versioned patterns](adr/0005-versioned-household-patterns.md), [0006 — Calendar intent](adr/0006-explicit-calendar-intent.md), [0007 — Public source and private deployment](adr/0007-public-repository-and-private-deployment.md), [0008 — Private photos and physical mapping](adr/0008-private-photos-and-physical-mapping.md) |
| design | How? | [Application and controller design](design/application.md) |
| specs | What must be true? | [Local service contract](specs/local-service.md), [Household authentication](specs/household-auth.md), [Pattern library and previews](specs/patterns.md), [Calendar and intent preview](specs/schedules.md), [House photos and mapping](specs/house.md), [Draft v1 acceptance contract](specs/v1-acceptance.md) |
| plans | In what order? | [Roadmap to household v1](plans/roadmap.md) |

Accepted architecture records the implementation direction; it does not imply
that all planned components have shipped.
New docs use their category's TEMPLATE.md and maintain reciprocal relative links.
