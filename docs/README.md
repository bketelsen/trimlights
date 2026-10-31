# Documentation

Implementation baseline: 2026-10-28. The clix CLI and local service foundation
exist. OIDC login and the initial read-only mobile UI are implemented and tested
locally. An initial TrueNAS/Caddy deployment passes HTTPS, readiness and login-redirect
checks with controller access disabled. The shared pattern editor now supports
revisions, conflict detection, favorites, import/export and software previews.
Desktop Pocket ID sign-in and owner phone passkey access are confirmed.
Second-user and mobile workflow acceptance, hardware
controls, schedule execution and physical calibration remain in progress. House
photo uploads, manual mapping and software previews are implemented. Calendar editing,
versioned saves and deterministic intent preview are implemented.
Bounded Go OFF/saved-slot/TIMER verification passed with owner physical confirmation
and exact non-clock restoration on 2026-10-28. Authenticated native controls are implemented under the native control contract;
production enablement and owner-confirmed signed-in playback/TIMER acceptance
are recorded in the roadmap. Calendar now checks pinned native slot compatibility
and copies supported cached native timers into reviewable drafts. These preparation
steps do not enable execution or change controller ownership. The execution worker
is implemented and tested against synthetic hardware; production activation remains
unavailable pending protocol and native timer handoff verification. Reviewed ownership
transfers are implemented internally and tested with synthetic hardware; there is
still no activation endpoint or flag.

This directory starts the four-question taxonomy from
[agentic-template](https://github.com/bketelsen/agentic-template). Its category
templates and canonical agent instructions have been adopted in
[roadmap Phase 1](plans/roadmap.md#phase-1--repository-foundation-and-clix-m).

| Directory | Question | Index |
| --- | --- | --- |
| adr | Why? | [0001 — Record decisions](adr/0001-record-architecture-decisions.md), [0002 — Portable instructions](adr/0002-agent-portable-instruction-surface.md), [0003 — Application architecture](adr/0003-shared-homelab-application.md), [0004 — Pocket ID and TrueNAS](adr/0004-pocket-id-and-truenas.md), [0005 — Versioned patterns](adr/0005-versioned-household-patterns.md), [0006 — Calendar intent](adr/0006-explicit-calendar-intent.md), [0007 — Public source and private deployment](adr/0007-public-repository-and-private-deployment.md), [0008 — Private photos and physical mapping](adr/0008-private-photos-and-physical-mapping.md), [0009 — Portable backups](adr/0009-consistent-portable-backups.md), [0010 — Bounded playback verification](adr/0010-bounded-playback-verification.md), [0011 — Audited household controls](adr/0011-audited-household-native-controls.md), [0012 — Prepare native calendar handoff](adr/0012-prepare-native-calendar-handoff.md), [0013 — Journal calendar execution](adr/0013-journal-calendar-execution.md), [0014 — Review ownership transfers](adr/0014-review-calendar-ownership-transfers.md), [0015 — Versioned attested images](adr/0015-versioned-attested-images.md), [0016 — Frostyard make gate](adr/0016-adopt-the-frostyard-make-gate.md) |
| design | How? | [Application and controller design](design/application.md), [Backup operations](design/backup-operations.md) |
| specs | What must be true? | [Local service contract](specs/local-service.md), [Bounded playback verification](specs/playback-verification.md), [Native controls](specs/native-controls.md), [Household authentication](specs/household-auth.md), [Pattern library and previews](specs/patterns.md), [Calendar and intent preview](specs/schedules.md), [Calendar preparation](specs/calendar-preparation.md), [Dormant calendar worker](specs/calendar-runtime.md), [Dormant ownership handoff](specs/calendar-handoff.md), [House photos and mapping](specs/house.md), [Backup and restore](specs/backups.md), [Draft v1 acceptance contract](specs/v1-acceptance.md) |
| plans | In what order? | [Roadmap to household v1](plans/roadmap.md), [Scheduling controller test](plans/scheduling-controller-test.md), [Pattern decoding test](plans/pattern-decoding-test.md) |

Accepted architecture records the implementation direction; it does not imply
that all planned components have shipped.
New docs use their category's TEMPLATE.md and maintain reciprocal relative links.
