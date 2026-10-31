# Spec: Local development service and CLI

This implemented contract covers the pre-OIDC local service. It is a development
foundation, not the authenticated household web application in the v1 contract.

## Interface

| Command | Required inputs | Behavior |
| --- | --- | --- |
| `serve` | `--data-dir`, `--api-token-file` | Loopback HTTP service; optional controller host enables explicit reads; `--log-level` sets [service logs](#service-logs) |
| `status` | `--api-token-file` | Cached state from the service, never a controller query |
| `refresh` | `--api-token-file`, `--acknowledge-clock-sync` | Serialized snapshot through the service |
| `snapshot` | `--host` or `TRIMLIGHT_CONTROLLER_HOST` | Direct diagnostic; optional new `--output` file |
| `probe-schedule2` | controller host, `--output`, `--baseline-sha256` | Direct bounded experiment with restoration and evidence |
| `verify-playback` | controller host, `--output`, `--baseline-sha256`, `--pattern-id` | Separate bounded OFF/saved-slot/TIMER [verification](playback-verification.md) |
| `healthcheck` | none; optional `--url`, `--timeout` | One GET to a loopback URL (default `http://127.0.0.1:8787/readyz`, 3 s); exit 0 only on 200 ([ADR-0015](../adr/0015-versioned-attested-images.md)) |

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
| `POST /api/v1/controller/control` | Bearer token | Verified [native control](native-controls.md) through the same worker |

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
- `healthcheck` MUST accept only an http(s) URL whose host is `localhost` or a
  loopback IP address, without credentials. It MUST ignore proxy settings, MUST
  NOT follow redirects, and MUST exit nonzero on a redirect, a non-200 status,
  a refused connection, an incomplete body or the timeout (positive, at most 1 minute).
- `status` MUST NOT synchronize the controller clock. `refresh` MUST explicitly
  acknowledge the potential clock mutation. Sundays remain refused by the driver.
- The queue MUST serialize complete reads, admit at most eight waiting requests,
  bound active reads to 15 seconds and pace operations by one second. Request or
  service cancellation MUST cancel queued/active work and permit audit cleanup.
- A failed read MUST preserve the previous successful snapshot and its timestamp.
  Snapshot age, last attempt and last error MUST remain distinguishable.
- Observation and operation audit writes MUST commit atomically. Storage keeps the
  most recent 100 completed operations. Queued requests are not replayed. Native controls persist pending intent before
  sending and mark interrupted operations uncertain on startup; see [native controls](native-controls.md).
- The store MUST reject a second process using the same local data directory and
  reject a schema newer than this binary understands. Schema version 1 creates
  observation and operation tables; version 2 adds auth state, version 3 adds
  the [pattern library](patterns.md), version 4 adds the [calendar](schedules.md), and version 5 adds
  [photos and layout history](house.md). Schema 6 adds [native control audits](native-controls.md). Upgrades back up
  the existing database with VACUUM INTO before a transactional migration.
  SQLite uses WAL and FULL sync. The store opens `trimlights.db` through an
  escaped absolute `file:` URI, so `?`, `%` or `#` in a data directory name
  stay part of the path. The URI applies a 5-second busy timeout, WAL, foreign
  keys and FULL sync to every connection, including a replacement connection.
- Data files MUST NOT be readable by other users, whatever the umask or the mode of
  an existing data directory. The store creates `trimlights.db` with mode 0600
  before SQLite opens it; SQLite gives `-wal` and `-shm` the database mode, and
  the store checks them again after open. At startup the store sets mode 0600 on
  the database, `-wal`, `-shm` and `-journal` files that have group or world access,
  when this process owns them. The store MUST NOT change the data directory mode:
  an operator dataset (for example on TrueNAS) can carry ACLs or settings that a
  chmod would disturb. The store logs a `data permissions` warning for each file it
  changes, each file it cannot change, and a data directory with group or world
  access. It does not refuse to start. Migration backups are created 0600 before
  VACUUM INTO writes them.
  `serve` sets umask 0077 as defence in depth.
- A process lock does not protect against another machine or another data directory
  owning the same hardware. Deployment MUST use a single active owner.
- A restored data directory containing `controller-disabled` MUST disable controller
  access regardless of host flags or environment. See [backup contract](backups.md).
- Controller access MUST remain disabled when neither a host flag nor
  `TRIMLIGHT_CONTROLLER_HOST` supplies a value. An explicit `--host=` disables
  environment-provided access. Service
  startup MUST NOT refresh automatically. Only [verified native controls](native-controls.md) are available with supported
  state; custom scene uploads and schedule writes remain unavailable.
- All API responses MUST use `Cache-Control: no-store`. No credentialed CORS or
  cookie authentication exists in this development mode. Use the separate
  [household OIDC mode](household-auth.md) through Caddy instead.

## Service logs

`serve` writes structured JSON lines with Go `log/slog` to standard error, so
`docker logs` shows them. `--log-level` accepts exactly `debug`, `info` (default),
`warn` or `error`; any other value MUST fail before the service starts. Both
bearer development and [household OIDC](household-auth.md#service-log-events) modes use these rules.

| Message | Level | Fields |
| --- | --- | --- |
| `trimlight serving` | info | `version`, `commit`, `listen`, `mode` (`oidc` or `bearer-development`), `controller_enabled`, `restore_guard` |
| `request failed` | error | `route` (matched method and pattern), `status`, `error`; `operation_id` for a failed controller operation; `path` for a frontend file error |
| `controller operation finished` | info, warn on failure, error on audit failure | `operation_id`, `kind`, `outcome`, `duration_ms`; `action` and `write_attempted` for controls; `error`, `persistence_error` |
| `request` | debug | `method`, `path` without query, `status`, `duration_ms` |
| `trimlight shutting down`, `trimlight stopped` | info, error | `error` when the service stops on failure: a category for household login setup, otherwise the error without network addresses |

- Every 5xx response MUST log its underlying cause once at that site. The response
  body MUST stay the generic message documented for its endpoint.
- Logs MUST NOT contain the controller host or address. The startup line reports
  only whether controller access is enabled. Controller network errors are logged
  as a category, such as `network dial failed: timeout`.
- A missing embedded frontend (`503`) and a frontend file error (`5xx`) are
  logged as `request failed` with a fixed `error` text.
- The error that `serve` returns is printed to standard error, which is also the
  container log, so it MUST follow the same rules: a failed household login setup
  returns a fixed message, and other errors lose network addresses.
- Request lines exist only at debug level, so `/healthz` and `/readyz` probes add
  no lines at the default level. Query strings are never logged.

## Observed playback state — 2026-10-28

Snapshot JSON MAY include `playback` for the observed 3492-byte, 18-pattern
layout, and MUST omit it unless the explicit schedule offset is 3306. This is
reported controller state, not confirmation that physical lights are illuminated.

| Field | Type | Meaning |
| --- | --- | --- |
| `mode_raw` | integer | Snapshot byte 3, retained even if unknown |
| `mode` | string | `off` for 0, `manual` for 1, `timer` for 2; otherwise `unknown` |
| `uninterpreted_parameters_hex` | string | 156 bytes at snapshot offsets 3320–3475, lowercase hex |
| `matching_pattern_ids` | integer array | All saved slots with identical parameters, in snapshot order; empty if none |

Parameter matches MUST NOT be presented as a uniquely identified selected slot
when multiple slots match. OFF can retain effect parameters. TIMER can retain
parameters while outside an active schedule. Neither a match nor mode alone
establishes physical output. Old stored snapshots without this field remain valid.
Unknown layout or mode MUST NOT confer a playback capability.

OFF/TIMER and saved-pattern selection have been captured from the official app
with owner observations. Independent Go playback and restoration are exercised
only by the separately authorized [verification diagnostic](playback-verification.md),
following [ADR-0010](../adr/0010-bounded-playback-verification.md). Hosted controls follow the separate [native control contract](native-controls.md)
and [ADR-0011](../adr/0011-audited-household-native-controls.md).

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| `build/trimlight` | Static Go binary with clix build metadata and timezone data |
| `trimlights.db` | Local SQLite storage; generated runtime state, never committed |
| `make check` | Formatting, vet, race tests, protocol fixtures, doc links and build |

## References

- Rationale: [ADR-0003](../adr/0003-shared-homelab-application.md),
  [ADR-0002](../adr/0002-agent-portable-instruction-surface.md);
  health check: [ADR-0015](../adr/0015-versioned-attested-images.md).
- Context: [application design](../design/application.md).
- Built in: [roadmap](../plans/roadmap.md).
- Future household requirements: [v1 acceptance](v1-acceptance.md).

Public deployment and fixture policy follows
[ADR-0007](../adr/0007-public-repository-and-private-deployment.md).
A snapshot returns `stable_sha256`, the SHA-256 of all bytes except its seven-byte
clock suffix. Parsing MUST reject a snapshot whose pattern records or explicit
schedule records reach into that suffix, so every parsed field is fingerprinted
(added 2026-10-30). A non-dry diagnostic probe MUST require that reviewed fingerprint,
match it against a fresh snapshot, and check its supported shape before entering
edit mode. `probe-schedule2` retains its disabled-slot restriction; the separate
[playback verification](playback-verification.md) requires a restorable TIMER
baseline. A fingerprint mismatch MUST send no write.

Before each `probe-schedule2` restoration write, including on a reconnect, the
probe MUST read current state. Only snapshot offset 3319 (schedule 2 off-minute)
MAY differ from the baseline non-clock bytes, and only with the probe value 4.
Any other difference is an external change: cleanup MUST stop without a write and
report `RESTORATION NOT VERIFIED`, with the observed state as `reconciled` in the
report (added 2026-10-30). The restoration payload is unchanged. The probe MUST
hold one client gate acquisition from its first exchange through cleanup, reconnect
recovery and independent verification; fresh connections reuse that acquisition.
