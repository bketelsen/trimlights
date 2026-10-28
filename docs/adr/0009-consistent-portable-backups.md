# 0009 — Back up SQLite and private configuration as one recovery bundle

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

Household state now includes image blobs, immutable revisions and OIDC configuration.
Copying a live database file without its WAL can lose committed data. A restored
instance must not compete with the live controller owner or reuse browser sessions.

## Decision

Provide Go/clix `backup` and `restore` commands. Backup opens the existing SQLite
source in read-only mode and uses VACUUM INTO for a consistent live snapshot without
migrating the source. Package that snapshot and explicitly supplied OIDC settings
and secret in a private ZIP with a versioned SHA-256 manifest. Normalize the secret
reference within the bundle; never include ambient controller environment variables.

Restore validates paths, byte limits, checksums, supported schema, database integrity,
foreign keys and OIDC settings before publishing a new directory. Never replace an
existing destination. Clear restored login flows and sessions. Write a persistent
controller-disabled marker that `serve` honors even if its environment contains a
controller host. Re-enabling communication requires deliberate removal of the marker
while the restored service is stopped, after selecting a single controller owner.

## Consequences

Live backups need local access to the data and selected config files but do not
need browser credentials or stop the service. Restore operates on a separate local
directory, allowing isolated acceptance before replacing production. Bundles contain
private house images and the OIDC secret; file permissions are private, but bundles
are not encrypted. Operators must store them privately and use encrypted off-host
storage when desired. Configuration changes should not overlap backup capture.
Checksums detect corruption, not a maliciously rewritten trusted backup.

## Alternatives considered

- **Copy the main database file:** unsafe while WAL writes are active.
- **ZFS-only recovery:** useful locally but not portable to a replacement host.
- **Restore into a running dataset:** risks data loss and competing controller owners.

## References

- Builds on: [ADR-0004](0004-pocket-id-and-truenas.md),
  [ADR-0008](0008-private-photos-and-physical-mapping.md).
- Shapes: [application design](../design/application.md),
  [backup contract](../specs/backups.md), [backup operations](../design/backup-operations.md), [roadmap](../plans/roadmap.md).
- Mechanism: [SQLite VACUUM INTO](https://www.sqlite.org/lang_vacuum.html).
