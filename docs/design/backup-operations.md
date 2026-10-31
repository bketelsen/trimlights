# Backup and recovery operations

Living document. Rationale: [ADR-0009](../adr/0009-consistent-portable-backups.md).
Contracts: [backup and restore](../specs/backups.md).

## Overview

A private ZIP holds one consistent SQLite snapshot and the selected Pocket ID
client configuration and secret. SQLite includes photos, thumbnails, layout history,
pattern revisions, calendars and their references. Restore creates a new guarded
instance; it never replaces or starts the running application.

## Design

`internal/backup` uses a separate read-only SQLite connection and VACUUM INTO,
then validates the snapshot and streams files into ZIP with SHA-256 entries.
Temporary output lives beside the destination. Completed archives are published by
an exclusive hard link; restored directories use Linux RENAME_NOREPLACE. All files
and directories are synced before publication, and incomplete staging is removed.

A restore validates archive membership, size/checksum limits, OIDC settings, SQLite
schema/integrity and foreign keys. It removes copied sessions/login flows and writes
`data/controller-disabled`. Both CLI and service startup honor that marker; the
service cannot communicate with the controller until deliberately re-enabled.

## Operational notes

Use the same image/version as the live app ([image releases](application.md#image-releases)). Create an output directory private to
UID/GID 65532 when running in a container. Example host variables below represent
local filesystem paths; do not put a live SQLite database on NFS/SMB.

```sh
TRIMLIGHT_IMAGE=ghcr.io/bketelsen/trimlights:vX.Y.Z  # the live app's pinned release
TRIMLIGHT_DATA_PATH=/private/trimlights/data
TRIMLIGHT_CONFIG_PATH=/private/trimlights/config
TRIMLIGHT_BACKUP_PATH=/private/trimlights/backups

docker run --rm --network none --read-only \
  -v "$TRIMLIGHT_DATA_PATH:/data" \
  -v "$TRIMLIGHT_CONFIG_PATH:/config:ro" \
  -v "$TRIMLIGHT_BACKUP_PATH:/backups" \
  "$TRIMLIGHT_IMAGE" backup --data-dir=/data \
  --oidc-config=/config/oidc.json --output=/backups/household.zip --json

docker run --rm --network none --read-only \
  -v "$TRIMLIGHT_BACKUP_PATH:/backups" \
  "$TRIMLIGHT_IMAGE" restore --input=/backups/household.zip \
  --output=/backups/recovery-check --json
```

The source mount is writable to allow SQLite's shared-memory/WAL bookkeeping when
needed; the backup database connection itself is read-only. The commands have no
network access. Do not change private configuration while capturing a backup. Use
a new archive name for each backup; the example refuses an existing file.

For an isolated verification, keep `recovery-check/data/controller-disabled`,
start a separate instance on a different loopback port with its own local bearer
token, and verify cached library/calendar/layout/photo data. Do not point Caddy at
this instance. The restored OIDC config can be validated with `serve --dry-run`,
without contacting Pocket ID. Restored sessions are intentionally absent.

Before replacing production, preserve a fresh backup and stop the old controller
owner. Select the verified recovered directories in the TrueNAS app configuration.
Keep the guard until the recovered state, host settings and scheduling ownership
are reviewed. To enable that chosen instance, stop it, deliberately remove only
its `controller-disabled` marker, configure the controller host, then start it.
Never enable two independent instances against the same controller.

A schema rollback requires the matching pre-upgrade database and compatible image;
older binaries refuse newer schemas. Restore compatible state into a new directory,
then switch the stopped app's mounts. Keep the prior directories for recovery.
Backups contain the OIDC client secret and house images: keep them private. This
format is not encrypted; protect off-host copies with storage encryption. Automated
retention/off-host replication and an in-app backup download UI are not implemented.

## References

- Rationale: [ADR-0009](../adr/0009-consistent-portable-backups.md);
  image releases: [ADR-0015](../adr/0015-versioned-attested-images.md).
- Contracts: [backup and restore](../specs/backups.md),
  [household acceptance](../specs/v1-acceptance.md).
- Context: [application design](application.md).
- Built in: [roadmap Phase 6](../plans/roadmap.md#phase-6--operations-household-trial-and-v1-release-m).
