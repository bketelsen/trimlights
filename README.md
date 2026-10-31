# Trimlights

A self-hosted, mobile-friendly interface for a household's Trimlight Edge lights.
The server and CLI are written in Go, using [clix](https://github.com/frostyard/clix).
React assets are embedded in one static binary; SQLite stores shared state.
Caddy provides HTTPS and Pocket ID provides household sign-in.

**Work in progress.** Shared pattern editing and calendar planning are implemented.
House-photo mapping and software previews are implemented. Native OFF/saved-slot/TIMER controls are implemented. Calendar execution, hardware
calibration and production acceptance remain
unfinished. This is an independent project, not an official
Trimlight application or a general-purpose controller driver.

## What works

- Pocket ID authorization-code login with PKCE, subject allowlisting, sessions and
  CSRF protection; simulated-provider tests pass. The initial TrueNAS/Caddy deployment
  passes readiness and login-redirect checks; the owner has confirmed desktop
  sign-in and phone passkey access. The second household account and mobile
  task workflows remain acceptance work.
- Shared pattern library: static/chase software previews, color runs, brightness,
  favorites, duplication, JSON import/export, revision history and stale-edit detection.
- Import opaque native patterns from a cached snapshot without guessing their effect.
- Calendar planning: weekly routines, seasonal ranges, one-day exceptions, solar
  offsets, overnight windows, expiring override drafts and precedence explanations.
- Pinned pattern revisions with visible calendar references before edits/archives.
- Calendar draft import/export with reference validation, review before saving,
  and preservation of timezone, solar location and override expiry.
- Private house photos, oriented and stripped of metadata; roofline drawing,
  physical addresses, mirrors, individual bulb adjustments, undo/redo and revisions.
- House previews in Library and Calendar, including the calendar’s pinned pattern
  revision; no simulated preview sends a hardware command.
- Serialized controller snapshot requests, persisted observations and operation
  history, and a bounded diagnostic write/restore experiment behind a fingerprint guard.
- Independently verified Go OFF, saved-slot playback and TIMER restoration on one
  controller, with physical confirmation and unchanged saved state. Home controls now route these actions through the authenticated worker, with
  fresh baseline checks, durable audit and read-only recovery after ambiguous sends.

[Roadmap](docs/plans/roadmap.md) · [Architecture](docs/design/application.md) ·
[Acceptance criteria](docs/specs/v1-acceptance.md) · [Documentation index](docs/README.md)

## Build

Use Go 1.27.3, Node 24.19.0 and pnpm 11.25.0 (versions pinned in CI).
The minimum Go version in go.mod is 1.26.6. Node is only a build dependency.

```sh
make check
./build/trimlight --help
```

`make check` runs the frontend build/tests, formatting, vet, Go race tests,
documentation links and a static binary build. It never contacts lighting hardware.
The binary is written to `build/trimlight` and `build/<architecture>/trimlight`.
All public protocol fixtures are synthetic; no original household captures are
included. Python is not required to build, test or run the application.

## Household deployment

The Check workflow publishes `ghcr.io/bketelsen/trimlights` for amd64 and arm64
after checks pass ([image releases](docs/design/application.md#image-releases)):

- Each push to main publishes a full-commit `:sha-<commit>` tag. Serialized
  promotions then move `:main` to the image of the head of main. `:main` can lag
  the head for a short time while the head image builds. It moves with every
  merge; do not deploy it.
- A release is a `vX.Y.Z` tag created by `make bump` (svu, pinned in `mise.toml`;
  run `mise install`). It publishes `:vX.Y.Z`, built with that version.

Each pushed image digest carries a GitHub build provenance attestation. Set
`TRIMLIGHT_IMAGE_TAG` to a release tag, or `vX.Y.Z@sha256:<digest>` to fix the
exact image, and verify it before you deploy:

```sh
gh attestation verify oci://ghcr.io/bketelsen/trimlights:vX.Y.Z --repo bketelsen/trimlights
```

Roll back by deploying an older release tag (after a database restore if its
schema is older; see [backup and restore](#backup-and-restore)). GHCR package
visibility/pull access must be configured before an unauthenticated TrueNAS
install can pull the image. The Compose example runs `/trimlight healthcheck`,
which exits 0 only when the loopback `/readyz` endpoint answers 200.

1. Register a confidential Pocket ID client with your app's exact HTTPS callback,
   for example `https://trimlights.example.com/auth/callback`. Restrict the client
   to the household users.
2. Copy [OIDC configuration](deploy/oidc.example.json) to private storage and fill
   `origin`, `issuer`, `client_id` and `allowed_subjects`. In Pocket ID, open the
   client’s **OIDC Data Preview**, select each user, and copy their `sub` value.
   See [Pocket ID claims](https://pocket-id.org/docs/guides/scopes-and-claims).
3. Save the secret in the referenced file, readable only by the service account.
   Relative secret-file paths resolve against the OIDC JSON file’s directory.
4. Copy [deployment variables](deploy/env.example) to a private `.env` file and
   set the bind address and local dataset paths. Leave `TRIMLIGHT_CONTROLLER_HOST`
   empty while verifying login. No controller address is built into Compose.
5. Deploy [Compose](deploy/compose.example.yaml) as a single TrueNAS custom app.
   Supply interpolated values through your deployment tooling; if the TrueNAS YAML
   editor does not load `.env`, substitute the values locally before pasting.
6. Configure [Caddy](deploy/Caddyfile.example) with `TRIMLIGHT_DOMAIN` and
   `TRIMLIGHT_UPSTREAM`, pointing to the app's private address and port. Use the
   HTTPS domain for login; secure cookies do not work at the raw HTTP address.

The container runs as UID/GID 65532 with a read-only root filesystem. Give that
user write access to the data dataset and read access to the config/secret files.
Use a local filesystem for the live SQLite database, not an NFS/SMB share.
Timezone data and public CA roots are embedded. No privileged network access,
Kubernetes, Redis or Node server is needed.

To build a local container:

```sh
GOOS=linux GOARCH=amd64 make build
podman build -f Containerfile -t trimlights-local .
```

For ARM, compile with `GOARCH=arm64` and build for `linux/arm64` with
`--build-arg TARGETARCH=arm64`. Only the compiled binaries enter the build context.

## Backup and restore

The Go CLI can create a private bundle from the live local database plus OIDC
configuration. It includes photos, mappings, revisions and schedules. Restore
validates the bundle into a new directory, clears copied sessions, and keeps
controller access blocked until deliberately re-enabled. See the
[backup runbook](docs/design/backup-operations.md) for commands and verification.
Bundles contain the OIDC secret and are not encrypted; store them privately.

## House photos and previews

In House, upload a JPEG, PNG or WebP (up to 12 MiB and 50 million pixels). The picker
requests supported formats so Safari can convert compatible photos. If HEIC still
arrives, export it as JPEG in Photos or Preview. Original files and metadata are
not retained. Up to 20 sanitized photos and their thumbnails stay in SQLite.

Add a roofline, set its output, first bulb and count, then tap its corners in order.
Use Move corners or percentage coordinates for precision, Move bulbs for perspective
adjustments, and separate runs for gaps. Multiple photo views can map the same run.
Mirrored runs reuse their source's logical colors. Rotation preserves mapped points.
Save the layout to preview it in Library or while selecting a calendar scene.

All mappings are uncalibrated. Preview colors come from the software pattern model;
opaque native effects cannot be previewed. Actual wiring and real-light comparison
still need verification. Photo deletion is not implemented. Export/import layout JSON to recover drafts;
imports are validated and loaded for review before saving. Layout JSON does not
include photos: restore a full backup to recover images and matching photo IDs.

## Calendar and preview

Add rules in Calendar, select a saved pattern revision or off, and save. Set the
household timezone and coordinates to use sunrise/sunset. “End on the following
day” represents overnight windows. Preview up to 30 local days; the timeline shows
winning and lower-priority rules, conflicts and daylight-saving adjustments.
Home offers native OFF, saved-slot playback and return to controller TIMER. These
controls do not upload software Library designs. Keep the official app closed,
refresh once to load controller state, and acknowledge possible clock synchronization.
After an uncertain command, refresh before another action. Native TIMER does not
mean the lights are currently on; the controller decides that from its timers.

Calendar saves and previews send no controller commands. Native timer handoff and
verified command delivery are still required before enabling execution.

Editing a pattern creates a new revision. Existing rules retain their pinned
versions, including after archival; update each rule explicitly to select a newer
version. Concurrent edits from two phones preserve the stale draft instead of
silently overwriting the saved version.

## Controller diagnostics

The current driver implements a layout observed on one controller. The schedule
offset `3306` is not universal. Unknown layouts and Sunday clock synchronization
are refused. Stop the service and close the manufacturer's app before direct
controller diagnostics so there is one connection owner.

```sh
export TRIMLIGHT_CONTROLLER_HOST=controller.example.invalid
mkdir -p experiments
./build/trimlight snapshot --timezone America/New_York \
  --daily-schedules-offset 3306 --output experiments/snapshot.json
```

Replace the example host with your controller's address. `--host` overrides the
environment; `--host=` explicitly disables an environment-provided address for the
service. A snapshot may synchronize the controller clock. It does not write
patterns or timers. `--dry-run` makes no connection and creates no files.

The optional `probe-schedule2` diagnostic requires a reviewed snapshot’s
`stable_sha256` and a supported disabled slot: slot 2, pattern ID 1, today-only,
00:02–00:03 in the 18-pattern layout. It changes only the off-minute from 03 to 04,
then restores 03 and independently reads back the result. All non-clock baseline
bytes must match the supplied fingerprint before any edit command is sent.

```sh
./build/trimlight probe-schedule2 --timezone America/New_York \
  --baseline-sha256 REVIEWED_STABLE_SHA256 --output experiments/probe-001
```

The evidence directory must be new. Restoration has a separate bounded cleanup
context and reconnect attempts, including after cancellation. A forced process
kill or outage can still prevent restoration; inspect the report. The Go probe
has simulated-controller coverage but still needs a controlled hardware pass.
Hosted native controls use the same verified commands; arbitrary write endpoints remain disabled.

The separate `verify-playback` diagnostic starts from a reviewed TIMER snapshot
with one unambiguous saved-effect match. It selects OFF, plays a different existing
slot, then restores the original effect and TIMER. OFF and target playback each
hold for ten seconds after verified readback. The entire operation, including
recovery and independent verification, retains the client gate.

```sh
./build/trimlight verify-playback --timezone America/New_York \
  --baseline-sha256 REVIEWED_STABLE_SHA256 --pattern-id 9 \
  --output experiments/playback-001
```

Choose an existing slot with different parameters from the original effect; `9`
is an example, not a known household pattern. This test passed on the original
controller on 2026-10-28: the owner confirmed OFF, target playback and original
appearance restored, and independent readback matched every non-clock baseline
byte. It does not establish custom pattern uploads, brightness, Sunday clock
semantics or scheduler handoff. See the [bounded contract](docs/specs/playback-verification.md).

## Local development API

Bearer development mode is loopback-only and separate from household OIDC mode.
Create a private token of at least 32 characters in `data/api.token`.

```sh
./build/trimlight serve --listen 127.0.0.1:8787 --data-dir data \
  --api-token-file data/api.token --host=
./build/trimlight status --api-token-file data/api.token --json
```

Explicit refresh also requires `--acknowledge-clock-sync`. The service does not
query the controller automatically on startup. Status reads cached state only.
CLI household OIDC login is not implemented yet.

For an isolated UI check with temporary storage, synthetic data and no controller:

```sh
TRIMLIGHT_BROWSER_PREVIEW=1 go test ./internal/service \
  -run '^TestBrowserPreview$' -v -count=1 -timeout=12m
```

Open `http://127.0.0.1:8789`. The fixture server stops after ten minutes or a signal;
it is test-only and is never compiled into the application binary.

## Privacy and evidence

Private deployment files, raw captures, experiment output, photos, databases and
builds are ignored. The public [protocol notes](protocol-observations.json) record
observed structure and limitations. [Fixture provenance](internal/trimlight/testdata/README.md)
distinguishes synthetic regression data from private hardware evidence. Do not
commit new real snapshots, even when their visible pattern names look harmless.
