# Spec: Portable backup and isolated restore

The Go CLI backs up household state and private OIDC configuration without contacting
the controller or identity provider. Restore creates an isolated, guarded directory.

## Interface

```sh
trimlight backup --data-dir /private/data --oidc-config /private/config/oidc.json --output /private/household.zip
trimlight restore --input /private/household.zip --output /private/recovered
```

Both commands support clix `--dry-run`, `--json` and `--silent`. Both require a new
output path. Backup requires existing data and OIDC configuration. Restore emits
`data/trimlights.db`, `config/oidc.json`, `config/pocket-id.secret` and
`data/controller-disabled`. The app reads its OIDC configuration from that directory.

The archive includes `manifest.json` with `format: 1`, `schema: 5`, `created`,
`version`, and `files` entries (`name`, `size`, `sha256`) for exactly the three data
and config files. All restored files are private (0600), directories 0700.

## Rules

- Backup MUST use a consistent SQLite snapshot. The source MUST not be migrated or
  modified. A missing database MUST fail rather than create an empty backup.
- All persisted photos, layout/pattern/calendar revisions, reference indexes and
  observations MUST survive restore. Config and secret MUST be bundled together;
  the normalized config secret reference is `pocket-id.secret`.
- Archive creation MUST publish only after the database and archive are complete
  and synced. Existing output files/directories MUST never be overwritten.
- Restore MUST reject unsupported formats/schemas, duplicate/unexpected entries,
  symlinks, path traversal, missing files, size/hash mismatches and invalid databases.
  Database limit is 1 GiB, OIDC config 64 KiB, secret 4096 bytes, manifest 16 KiB.
- Restore MUST validate SQLite integrity and foreign keys and require the known
  application tables. It MUST clear login flows and sessions before publishing.
- Restore MUST write the controller-disabled marker. Service startup MUST honor it
  regardless of the host environment/flag. Restoring MUST never start a service.
- A dry-run MUST create no files or directories and open no database or network
  connection. It describes intent and MUST not claim the archive/source is verified.
- A restore MUST target a new directory; it MUST NOT replace a running application.
- Backups contain secrets and private images. CLI output MUST not print either.

## Derived artifacts

The archive is a recovery artifact, not a public repository artifact. Migration
backups remain separate and do not replace a full bundle with private configuration.

## References

- Rationale: [ADR-0009](../adr/0009-consistent-portable-backups.md).
- Context: [application design](../design/application.md),
  [backup operations](../design/backup-operations.md).
- Delivery: [roadmap](../plans/roadmap.md).
- Acceptance: [household v1](v1-acceptance.md).
