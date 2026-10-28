# Spec: Local development service and CLI

This implemented contract covers the pre-OIDC local service. It is a development
foundation, not the authenticated household web application in the v1 contract.

## Interface

| Command | Required inputs | Behavior |
| --- | --- | --- |
| `serve` | `--data-dir`, `--api-token-file` | Loopback HTTP service; optional controller host enables explicit reads |
| `status` | `--api-token-file` | Cached state from the service, never a controller query |
| `refresh` | `--api-token-file`, `--acknowledge-clock-sync` | Serialized snapshot through the service |
| `snapshot` | `--host` or `TRIMLIGHT_CONTROLLER_HOST` | Direct diagnostic; optional new `--output` file |
| `probe-schedule2` | controller host, `--output`, `--baseline-sha256` | Direct bounded experiment with restoration and evidence |

The service defaults to `127.0.0.1:8787`. CLI API commands accept a literal loopback
HTTP URL with `--url`. Tokens are read from a file, contain at least 32 characters,
and must not be passed in command-line arguments or committed. Direct diagnostics
must run with the service stopped and the phone app closed.

| HTTP endpoint | Authentication | Result |
| --- | --- | --- |
| `GET /healthz` | None | Process health |
| `GET /readyz` | None | Storage availability, independent of hardware |
| `GET /api/v1/status` | Bearer token | Observation, snapshot age, worker queue, capability limits |
| `GET /api/v1/operations` | Bearer token | Last 100 completed refresh results |
| `POST /api/v1/controller/refresh` | Bearer token | One verified snapshot result or error, with operation ID |

Refresh accepts exactly one JSON object, with `Content-Type: application/json`:

```json
{"acknowledge_clock_sync": true}
```

## Rules

- `--dry-run` MUST make no network request and create no database, listener,
  evidence file or directory. It reports intent and leaves baseline validation
  unverified. Reading local configuration/token files is allowed.
- Successful ordinary commands with `--json` MUST emit one JSON document.
  Run-time command errors emit clix's `error`, `message`, `details` envelope and
  exit nonzero. Cobra parse/help/version output follows clix conventions.
- `--silent` MUST suppress application result output. Errors still exit nonzero.
- `status` MUST NOT synchronize the controller clock. `refresh` MUST explicitly
  acknowledge the potential clock mutation. Sundays remain refused by the driver.
- The queue MUST serialize complete reads, admit at most eight waiting requests,
  bound active reads to 15 seconds and pace operations by one second. Request or
  service cancellation MUST cancel queued/active work and permit audit cleanup.
- A failed read MUST preserve the previous successful snapshot and its timestamp.
  Snapshot age, last attempt and last error MUST remain distinguishable.
- Observation and operation audit writes MUST commit atomically. Storage keeps the
  most recent 100 completed operations. Pending requests are not persisted or replayed.
- The store MUST reject a second process using the same local data directory and
  reject a schema newer than this binary understands. Schema version 1 creates
  observation and operation tables; version 2 adds auth state, version 3 adds
  the [pattern library](patterns.md), version 4 adds the [calendar](schedules.md), and version 5 adds
  [photos and layout history](house.md). Upgrades back up
  the existing database with VACUUM INTO before a transactional migration.
  SQLite uses WAL and FULL sync.
- A process lock does not protect against another machine or another data directory
  owning the same hardware. Deployment MUST use a single active owner.
- A restored data directory containing `controller-disabled` MUST disable controller
  access regardless of host flags or environment. See [backup contract](backups.md).
- Controller access MUST remain disabled when neither a host flag nor
  `TRIMLIGHT_CONTROLLER_HOST` supplies a value. An explicit `--host=` disables
  environment-provided access. Service
  startup MUST NOT refresh automatically. Power, scene apply and schedule writes
  remain unavailable until their semantics and read-back are verified.
- All API responses MUST use `Cache-Control: no-store`. No credentialed CORS or
  cookie authentication exists in this development mode. Use the separate
  [household OIDC mode](household-auth.md) through Caddy instead.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| `build/trimlight` | Static Go binary with clix build metadata and timezone data |
| `trimlights.db` | Local SQLite storage; generated runtime state, never committed |
| `make check` | Formatting, vet, race tests, protocol fixtures, doc links and build |

## References

- Rationale: [ADR-0003](../adr/0003-shared-homelab-application.md),
  [ADR-0002](../adr/0002-agent-portable-instruction-surface.md).
- Context: [application design](../design/application.md).
- Built in: [roadmap](../plans/roadmap.md).
- Future household requirements: [v1 acceptance](v1-acceptance.md).

Public deployment and fixture policy follows
[ADR-0007](../adr/0007-public-repository-and-private-deployment.md).
A snapshot returns `stable_sha256`, the SHA-256 of all bytes except its seven-byte
clock suffix. A non-dry diagnostic probe MUST require that reviewed fingerprint,
match it against a fresh snapshot, and check the supported disabled-slot shape
before entering edit mode. A fingerprint mismatch MUST send no write.
