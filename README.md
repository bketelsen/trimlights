<!-- readme-kind: service -->
# Trimlights

[![Check](https://github.com/bketelsen/trimlights/actions/workflows/ci.yml/badge.svg)](https://github.com/bketelsen/trimlights/actions/workflows/ci.yml)

Trimlights is a self-hosted, mobile-friendly web app for one household's
Trimlight Edge lights. Household members sign in with Pocket ID to edit shared
patterns, plan a lighting calendar, preview designs on photos of the house, and
use the controller's native OFF, saved-slot and TIMER modes. It is an
independent project, not an official Trimlight application or a general-purpose
controller driver.

**Status:** pre-release. Calendar execution, hardware calibration and production acceptance are not finished ([roadmap](docs/plans/roadmap.md)).

**Documentation:** [docs/README.md](docs/README.md)

## What it does

- **Household sign-in:** Pocket ID login with PKCE, a subject allowlist,
  sessions and CSRF protection
  ([household authentication](docs/specs/household-auth.md)).
- **Shared pattern library:** colors, brightness, static and chase previews,
  favorites, JSON import and export, revisions and stale-edit detection; native
  patterns import as opaque designs ([patterns](docs/specs/patterns.md)).
- **Calendar planning:** weekly routines, seasonal ranges, one-day exceptions,
  solar offsets, overnight windows and precedence explanations; saves and
  previews send no controller command
  ([calendar and intent preview](docs/specs/schedules.md)).
- **House photos:** private uploads stripped of metadata, roofline mapping and
  software previews in Library and Calendar
  ([house photos and mapping](docs/specs/house.md),
  [household guide](docs/design/household-guide.md)).
- **Native controls:** OFF, saved-slot playback and return to TIMER through
  one authenticated worker, with fresh baseline checks, a durable audit and
  read-only recovery ([native controls](docs/specs/native-controls.md)).
- **Backups:** a private bundle of the database and OIDC configuration,
  restored into a new directory with controller access blocked
  ([backup operations](docs/design/backup-operations.md)).
- **Bounded diagnostics:** `snapshot`, `probe-schedule2` and `verify-playback`,
  each behind a reviewed baseline fingerprint
  ([controller diagnostics](docs/design/deployment.md#controller-diagnostics)).
- **Calendar execution:** implemented and tested against a simulated
  controller, but dormant until protocol and handoff verification
  ([calendar runtime](docs/specs/calendar-runtime.md)).

## Deploy

Trimlights runs as one TrueNAS custom app (Compose) behind Caddy, which serves
HTTPS, with Pocket ID for sign-in. CI publishes `ghcr.io/bketelsen/trimlights`
for amd64 and arm64 with a build provenance attestation. A deployment pins a
release tag `vX.Y.Z`, which `make bump` creates, and never `:main`
([image releases](docs/design/application.md#image-releases)). The
[deployment runbook](docs/design/deployment.md) covers the Pocket ID client,
the configuration, Compose, Caddy, logs, rollback and the controller
diagnostics; [backup operations](docs/design/backup-operations.md) covers backup
and restore. Actual addresses, domains and credentials stay in private
configuration
([ADR-0007](docs/adr/0007-public-repository-and-private-deployment.md)).

## Interface

| Endpoint or command | What it is |
| --- | --- |
| `/` | The embedded web app, behind sign-in |
| `/auth/login`, `/auth/callback` | The Pocket ID authorization-code flow |
| `/api/v1/` | The JSON API of the web app and the CLI |
| `/healthz`, `/readyz` | Liveness and readiness; `trimlight healthcheck` checks `/readyz` |
| `trimlight serve` | The service: household OIDC mode, or bearer development mode on loopback only |
| `trimlight status` | Cached controller state, through the development API |
| `trimlight backup`, `trimlight restore` | The recovery bundle |
| `trimlight snapshot`, `probe-schedule2`, `verify-playback` | Bounded controller diagnostics |

The exact contract is the
[local service and CLI spec](docs/specs/local-service.md).

## Privacy

- Private deployment files, raw captures, experiment output, photos, databases
  and builds are never committed.
- Public protocol fixtures are synthetic
  ([fixture provenance](internal/trimlight/testdata/README.md)).
- House photos keep no original file and no metadata.
- Logs never record tokens, cookies or the controller address.

## Develop

```sh
mise install   # Go toolchain, golangci-lint and svu, as pinned
make verify    # tidy, gofmt, vet, lint, race tests, doc links; no pnpm, no writes
make check     # gofmt -w, then make ci (CI runs make ci)
```

`make ci` also needs Node 24.19.0 and pnpm 11.25.0 for the web app. No target
contacts lighting hardware. The
[development guide](docs/design/development.md) has the details and the local
development API.

## Where to go next

- [AGENTS.md](AGENTS.md): the rules for agents and contributors
- [docs/README.md](docs/README.md): decisions, designs, specs and plans
- [SECURITY.md](SECURITY.md): how to report a vulnerability
- [LICENSE](LICENSE): MIT
