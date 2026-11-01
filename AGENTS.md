# Trimlights

A shared Go application for one household's Trimlight Edge controller, with
mobile controls, scheduling and house-photo previews under construction.
Start at [docs/README.md](docs/README.md) for current implementation and plans.

This is the canonical instruction file. CLAUDE.md, GEMINI.md and
.github/copilot-instructions.md link here. Edit canonical paths only.
[ADR-0002](docs/adr/0002-agent-portable-instruction-surface.md) explains portability.

## Skills

Reusable procedures belong in `.agents/skills/`, starting from its TEMPLATE.
`.claude/skills` links to that directory. No project procedures are packaged yet.

## Code conventions (implemented)

- Keep wire encoding and validation in `internal/trimlight/protocol.go`; enforce
  framing bounds and checksum validation before interpreting payloads.
- Serialize complete controller operations through `Client.withSession` in
  `internal/trimlight/client.go`. Preserve paced exchanges and cancellation-safe
  restoration in `ProbeSchedule2`. A snapshot can synchronize the device clock.
- Use captured protocol fixtures and the simulated controller for ordinary tests.
  Never add hardware access to CI or the make gates.
- Run `make check` (formats, then `make ci`) before declaring a change done. CI
  runs `make ci`; `make verify` is the non-mutating gate without pnpm. Fix lint
  findings; never loosen `.golangci.yml`
  ([ADR-0016](docs/adr/0016-adopt-the-frostyard-make-gate.md)).
- Pin workflow actions by full commit SHA with a version comment. Images pass
  through 3 jobs: build (read-only; runs pnpm and Go), publish and promote. Only
  publish and promote hold `packages: write`; only publish holds `id-token` and
  `attestations`; neither runs repository build code
  ([ADR-0015](docs/adr/0015-versioned-attested-images.md)). Release tags come from `make bump`.

## Releases and dependencies

- Tags are `vX.Y.Z`. Brian runs `make bump` on a clean, current main: it runs
  `make check`, takes `svu next` (Conventional Commits) and pushes the tag. The
  tag push runs `.github/workflows/check.yml`, which builds, publishes and
  promotes the attested image. Agents never tag, push tags or release.
- Internal dependencies: `github.com/frostyard/clix` in `go.mod`
  (`github.com/frostyard/std` is indirect). No frostyard or bketelsen package
  is in `web/package.json`. No other repository is known to depend on this one.
- Dependabot ignores `golang.org/x/crypto/x509roots/fallback`, the CA roots of
  the scratch image (`.github/dependabot.yml`). At each hygiene pass, compare
  its pin in `go.mod` with `go list -m golang.org/x/crypto/x509roots/fallback@latest`;
  when `@latest` is newer, bump it with `go get` and `go mod tidy` in a PR
  (Brian, T647c, 2026-11-01).
- Dependabot also ignores `@types/node` majors: the types follow the Node 24
  runtime that CI pins (Brian, T647a, 2026-11-01).

## Repository boundary

Do not commit credentials, captures, experiment output, personal photos, runtime
state or generated builds. Public protocol fixtures are fully synthetic; preserve
that invariant. Original research stays in ignored private storage. The owner
selected public repository bketelsen/trimlights and requested scrubbing personal
details. Keep actual addresses, domains and credentials in private configuration.
Never bake the controller address into source or Compose. Use
TRIMLIGHT_CONTROLLER_HOST or --host. Diagnostic writes require a reviewed baseline
fingerprint. Keep the supported disabled-slot probe unchanged. The owner authorized
a separate bounded OFF/saved-slot/TIMER verification on 2026-10-28; follow
[ADR-0010](docs/adr/0010-bounded-playback-verification.md) and its
[contract](docs/specs/playback-verification.md). This exception does not authorize
arbitrary mode, pattern, schedule, brightness or topology writes.
Verified ordinary OFF/saved-slot/TIMER actions are separately authorized through
the household worker under [ADR-0011](docs/adr/0011-audited-household-native-controls.md).
Keep fresh baseline checks, durable pending audit and read-only reconciliation;
never retry an ambiguous control write or replay an interrupted intent on startup.

## Documentation rules

Every new document starts from its category's `docs/*/TEMPLATE.md`:

- ADRs record why; Accepted decisions are immutable. Reversals use a new ADR.
- Designs record how; update them as code changes and distinguish planned features.
- Specs pin exact contracts; change implemented specs alongside code.
- Plans sequence work with explicit “Done when” criteria.

Maintain reciprocal relative links: ADRs to affected designs/specs and prior ADRs;
designs to rationale, specs and roadmap; specs to rationale and design; each plan
phase to design and specs. Check targets and anchors when editing docs.
Index new documents in `docs/README.md`. Record significant decisions before their
implementation. Use absolute dates in documentation.

## Implemented service and UI boundaries

- Keep clix globals in `cmd/trimlight`; service configuration is immutable.
- Open SQLite through `storage.Open`; migrations back up existing schemas and
  observations/audits commit together. Keep data on a local filesystem.
- Use `auth.Handler` for browser authentication/CSRF and `service.Run` for mode
  validation. Never expose bearer development mode beyond loopback.
- Frontend source is in `web/`; `make web-build` embeds output via `internal/webui`.
  Use exact package versions and the pnpm lockfile. Go tests use simulated OIDC
  and controller endpoints; they must not contact household services.
- Change pattern semantics in `internal/pattern` and `web/src/pattern.ts` together;
  both renderers must pass `web/src/pattern-vectors.json`. Library edits use
  immutable revisions and compare-and-swap through `storage.UpdatePattern`.
  Native imports preserve opaque parameters; do not invent colors or wire writes.

- Keep calendar evaluation in `internal/schedule` free of storage/network effects.
  Resolve civil-time boundaries explicitly; never rely on Go's ambiguous DST
  choice. Calendar saves and reference indexes commit atomically, pin pattern
  revisions, and require compare-and-swap. Preview must never enable execution. Calendar preparation reads a consistent
  cached snapshot through `storage.CalendarInputs`, re-parses wire bytes, and
  matches pinned native designs by original slot and parameters. Copy only reviewed
  timer proposals into drafts; preparation must never invoke the controller worker.
- Calendar execution is implemented but dormant. Keep production activation absent
  until protocol/handoff verification; `service.Run` suspends persisted ownership.
  The runner must use the existing worker, journal pending intent atomically through
  storage, honor interval deadlines and pause on external changes or uncertainty.
  Never replay interrupted deliveries or turn a restored checkpoint into permission
  to control hardware. See [runtime contract](docs/specs/calendar-runtime.md).
- Ownership transfers use the dormant [handoff coordinator](docs/specs/calendar-handoff.md).
  Bind reviews to expiry, exact controller state and ownership/calendar versions;
  journal the paused pending transfer before sending. A concurrent pause wins over
  successful completion. Keep HTTP/CLI activation absent until physical verification.

- House photos are normalized in `internal/house` before storage. Keep byte/pixel
  limits, EXIF transforms and metadata removal intact; never serve upload originals.
  Photo blobs and immutable layout references share SQLite transactions. Geometry
  changes must pass the shared Go/TypeScript vectors. Software mappings remain
  uncalibrated until a verified hardware workflow can establish their addresses.

- Keep backups in `internal/backup` independent of controller/network code. Backup
  opens an existing database read-only; restore publishes only a new directory.
  Keep archive limits, validation, session clearing and the restore guard intact.
  Update backup schema support and round-trip tests whenever storage migrations change.
