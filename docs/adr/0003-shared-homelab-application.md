# 0003 — Build a shared homelab application around the Go controller client

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

The owner and his wife need a mobile-friendly replacement for routine Trimlight
use, particularly scheduling. The owner selected Go, requested frostyard/clix and
an agentic-template retrofit, and confirmed v1 includes shared controls, full
calendar scheduling, pattern editing, and manual roofline mapping over house
photos. One controller is known, at controller.example.invalid. Music sync and multiple-controller
management are outside the agreed v1 scope.

The homelab already has a Caddy VM for reverse proxying and Pocket ID for OIDC.
A Go snapshot client works against the controller. A restricted write/restore
experiment was verified in Python and ported to Go with simulated-controller
tests; Go schedule writes have not yet been revalidated on hardware.

## Decision

Use the following architecture for implementation in the roadmap:

- One Go application and one active controller-owning service instance. Use clix
  for CLI conventions. The normal CLI talks to the service; direct hardware
  diagnostics require the service worker to release ownership.
- Embed the compiled mobile frontend into the Go binary. Use TypeScript and
  React for the interactive browser UI, with Canvas 2D for photo previews.
  Node is a build dependency, not a homelab runtime service.
- Store household state in SQLite and photos in a persistent media directory.
  Choose a SQLite driver that preserves a static Go build where practical.
  Back up database, media, mappings and required configuration together.
- Route HTTPS through existing Caddy. Implement server-side OIDC authorization
  code login with Pocket ID; keep tokens/client secrets out of browser storage.
- Make the Go service authoritative for v1 schedules. The controller runs effects;
  the service resolves dates, recurrence, priorities and temporary overrides.
  Migrate native timers explicitly, with a captured baseline and rollback path.
- Use one canonical pattern model for editing, controller compilation and preview
  rendering. Declare unsupported capabilities and approximate previews explicitly.

## Consequences

The household gets shared state, consistent scheduling and one deployable app.
The CLI, browser and scheduler share the same application services and controller
queue instead of creating competing TCP connections.

The scheduler now depends on the server being alive. A server outage can leave
the controller showing its last state; no automatic hardware shutoff is promised
until its behavior has been verified. The app must report that limit clearly.

clix adds Go dependencies and raises the prototype's current minimum Go version:
v0.5.0 declares Go 1.26.6 and toolchain 1.26.10. Pin the dependency and a tested
toolchain during Phase 1. The existing stdlib-only claim will no longer describe
the complete application, although the protocol layer can remain stdlib-only.

Photo geometry identifies logical bulb positions; it does not create hardware
zones. Output addressing, mirrored runs and direction must be measured first.

## Alternatives considered

- **Native iOS app:** adds signing/distribution and a separate client lifecycle;
  a mobile web app serves both users and remains easy to host.
- **Home Assistant as the product:** useful integration later, but does not directly
  provide the desired photo editor and household scheduling experience.
- **Controller-native schedules as the sole authority:** preserves operation while
  the server is down, but exposes native limits and requires more reverse
  engineering for a full calendar. Native schedule export/fallback can follow
  after validation; it is not required for the proposed server-owned calendar.
- **Automatic 3D reconstruction or AI roofline detection first:** unnecessary for
  usable previews. Manual polylines and known bulb counts are more directly
  verifiable for v1.
- **Separate frontend, API and scheduler services:** extra deployment and ownership
  coordination without a benefit for this single-household installation.

## References

- Builds on: [ADR-0001](0001-record-architecture-decisions.md),
  [ADR-0002](0002-agent-portable-instruction-surface.md).
- Initial implemented contract: [local service](../specs/local-service.md).
- Shapes: [application design](../design/application.md),
  [v1 acceptance](../specs/v1-acceptance.md), [roadmap](../plans/roadmap.md).
- [clix v0.5.0](https://github.com/frostyard/clix/releases/tag/v0.5.0) and
  [its Go module](https://github.com/frostyard/clix/blob/v0.5.0/go.mod).
- [agentic-template](https://github.com/bketelsen/agentic-template).
- [Pocket ID app integration](https://pocket-id.org/docs/setup/connect-an-app).
- [Caddy reverse proxy](https://caddyserver.com/docs/quick-starts/reverse-proxy).


Privacy note (2026-10-28): household addresses/domains were replaced with examples
at the owner’s request. Architectural decisions are unchanged; see
[ADR-0007](0007-public-repository-and-private-deployment.md).
