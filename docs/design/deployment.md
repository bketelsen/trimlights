# Household deployment and diagnostics

Living document. Rationale: [ADR-0004](../adr/0004-pocket-id-and-truenas.md),
[ADR-0007](../adr/0007-public-repository-and-private-deployment.md),
[ADR-0015](../adr/0015-versioned-attested-images.md),
[ADR-0010](../adr/0010-bounded-playback-verification.md).
Contracts: [local service and CLI](../specs/local-service.md),
[household authentication](../specs/household-auth.md),
[bounded playback verification](../specs/playback-verification.md).

## Overview

The runbook for the household deployment: the published image, the Pocket ID
client, the TrueNAS custom app, the Caddy route, logs, rollback, backups, and
the bounded controller diagnostics. Actual addresses, domains and credentials
stay in private configuration.

## Design

The image, its tags and its attestation are described in
[image releases](application.md#image-releases). The service and its CLI
follow the [local service contract](../specs/local-service.md).

## Operational notes

### Household deployment

The Check workflow publishes `ghcr.io/bketelsen/trimlights` for amd64 and arm64
after checks pass ([image releases](application.md#image-releases)):

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
   to the household users. The app requests only the `openid profile` scopes.
2. Copy [OIDC configuration](../../deploy/oidc.example.json) to private storage and fill
   `origin`, `issuer`, `client_id` and `allowed_subjects`. In Pocket ID, open the
   client’s **OIDC Data Preview**, select each user, and copy their `sub` value.
   See [Pocket ID claims](https://pocket-id.org/docs/guides/scopes-and-claims).
3. Save the secret in the referenced file, readable only by the service account.
   Relative secret-file paths resolve against the OIDC JSON file’s directory.
4. Copy [deployment variables](../../deploy/env.example) to a private `.env` file and
   set the bind address and local dataset paths. Leave `TRIMLIGHT_CONTROLLER_HOST`
   empty while verifying login. No controller address is built into Compose.
5. Deploy [Compose](../../deploy/compose.example.yaml) as a single TrueNAS custom app.
   Supply interpolated values through your deployment tooling; if the TrueNAS YAML
   editor does not load `.env`, substitute the values locally before pasting.
6. Configure [Caddy](../../deploy/Caddyfile.example) with `TRIMLIGHT_DOMAIN` and
   `TRIMLIGHT_UPSTREAM`, pointing to the app's private address and port. Use the
   HTTPS domain for login; secure cookies do not work at the raw HTTP address.

The service writes JSON logs to standard error; read them with `docker logs`
(or the TrueNAS app log view). Set `TRIMLIGHT_LOG_LEVEL` to `debug`, `info`,
`warn` or `error`; `debug` adds request lines (method, path, status, duration). Logs record sign-in subjects
and the causes of server errors, never tokens, cookies or the controller address.
See the [service log contract](../specs/local-service.md#service-logs).

The container runs as UID/GID 65532 with a read-only root filesystem. Give that
user write access to the data dataset and read access to the config/secret files.
The service keeps its database files at mode 0600. It does not change the data
directory: when the directory has group or world access, it logs a `data
permissions` warning, so restrict that directory to the service user.
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

### Backup and restore

The Go CLI can create a private bundle from the live local database plus OIDC
configuration. It includes photos, mappings, revisions and schedules. Restore
validates the bundle into a new directory, clears copied sessions, and keeps
controller access blocked until deliberately re-enabled. See the
[backup runbook](backup-operations.md) for commands and verification.
Bundles contain the OIDC secret and are not encrypted; store them privately.

### Controller diagnostics

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
semantics or scheduler handoff. See the [bounded contract](../specs/playback-verification.md).

## References

- Rationale: [ADR-0004](../adr/0004-pocket-id-and-truenas.md),
  [ADR-0007](../adr/0007-public-repository-and-private-deployment.md),
  [ADR-0010](../adr/0010-bounded-playback-verification.md),
  [ADR-0015](../adr/0015-versioned-attested-images.md)
- Contracts: [local service and CLI](../specs/local-service.md),
  [household authentication](../specs/household-auth.md),
  [bounded playback verification](../specs/playback-verification.md),
  [backup and restore](../specs/backups.md)
- Design: [application](application.md), [backup operations](backup-operations.md)
- Built in: [roadmap](../plans/roadmap.md)
