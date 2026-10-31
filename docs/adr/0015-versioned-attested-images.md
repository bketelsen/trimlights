# 0015 — Publish versioned, attested images from a split pipeline

- **Status:** Accepted
- **Date:** 2026-10-30

## Context

Every push to main published `ghcr.io/bketelsen/trimlights:main` and
`:sha-<commit>`, built with `VERSION=main`. There were no release tags and no
provenance, so each merge was in effect a production release, and nothing
proved that an image came from this repository's workflow. The image job held
`packages: write` while it ran `pnpm install` and the frontend build, so a
compromised npm package could push to the registry that TrueNAS pulls. Actions
were pinned by mutable tags. The scratch image had no health check because it
has no shell or curl. The live TrueNAS app pulls `:main` or a `sha-` tag today.
The 2026-10-30 review filed this as issues #19, #20 and #21.

## Decision

The Check workflow pins every action by full commit SHA with a version comment,
sets `permissions: {}` at workflow level and grants each job only what it needs.
Checkouts do not persist credentials; jobs have timeouts; pull request runs
cancel superseded runs, main and tag runs never do. Dependabot updates Go
modules, the pnpm-locked web packages and the action pins weekly.

Image publication has two jobs. The build job holds only `contents: read`. It
runs pnpm and `make build` for linux/amd64 and linux/arm64 and uploads the two
static binaries as an artifact. The publish job holds `packages: write`,
`id-token: write` and `attestations: write`. It downloads the binaries, packages
them with the copy-only Containerfile, pushes, and runs
`actions/attest-build-provenance` on the pushed index digest with
`push-to-registry: true`. It runs no pnpm, Node, Go or repository build code.

Main pushes keep `:main` and `:sha-<full commit>`, now attested, so the live app
keeps working. Each non-PR run has its own concurrency group, because GitHub
cancels all but 1 pending run in a group, so main runs can publish at the same
time. The publish job always pushes the run's `:sha-<full commit>` tag. A
separate `promote` job, with only `contents: read` and `packages: write`, then
moves `:main`. Promote jobs share the concurrency group `promote-main`, so
promotions are serialized. Each promotion reads the current head of main and
retags `:main` to the exact digest of `:sha-<head>`; it never promotes its own
run's commit. When the head image is not pushed yet, it skips, and the head's
own run promotes it later. A release is a `vX.Y.Z` tag that only `make bump` creates, with
the svu release pinned in `mise.toml`. A tag run passes the same check gate,
builds with `VERSION=vX.Y.Z`, and pushes and attests `:vX.Y.Z`. Deployments pin
a release tag, optionally with its digest, and verify it with
`gh attestation verify`.

The binary gains `trimlight healthcheck`. It sends one GET to a loopback URL,
by default `http://127.0.0.1:8787/readyz`, ignores proxy settings, never
follows redirects, and exits 0 only for 200 OK within its timeout. The Compose
example uses it as the container health check.

## Consequences

A compromised build dependency can no longer push or sign images; it can only
corrupt the artifact that the publish job packages, and that artifact is still
bound to this workflow run by the attestation. Rollback means choosing an older
`vX.Y.Z` tag. Releases need svu (`mise install`) and a passing local gate.
Dependabot adds weekly pull requests to review. A tag pushed outside
`make bump` still publishes if it is `vX.Y.Z` and passes the gate.
`:main` always moves to the image of the head of main at promotion time, so an
older run cannot move it back. GitHub can replace a pending promote job with a
newer one; that loses nothing, because the newer job reads the head again.
`:main` can lag the head for a short time while the head image builds, and
longer when the head's run fails. A failed promotion is repaired by the next
one. The `:main` tag stays mutable until the live deployment moves to release tags;
removing it is a separate change, coordinated with the TrueNAS app owner.
Images built before this decision have no `healthcheck` command, so a Compose
health check needs an image from this change or later.

## Alternatives considered

- **Keep one image job with a write token:** simpler, but it gives third-party
  npm code a token that can push the production image.
- **A separate release workflow:** frostread uses one, but here it would
  duplicate the check and build jobs; one workflow keeps a tag behind the same gate.
- **Stop publishing `:main` now:** it would break the live app's re-pull before
  a release tag exists.
- **A health check through `wget` or a shell:** the scratch image has neither, and
  adding a base image enlarges the attack surface.

## References

- Shapes: [application design](../design/application.md#image-releases),
  [local service contract](../specs/local-service.md),
  [backup operations](../design/backup-operations.md)
- Builds on: [ADR-0004](0004-pocket-id-and-truenas.md),
  [ADR-0007](0007-public-repository-and-private-deployment.md)
