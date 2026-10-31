# 0016 — Adopt the frostyard make gate, linter and tool pins

- **Status:** Accepted
- **Date:** 2026-10-30

## Context

`make check` was the only gate. It ran the frontend build and tests, `gofmt -l`,
`go vet`, race tests, documentation links and the binary build, and CI ran the
same target. No linter and no vulnerability scan ran. golangci-lint 2.15.0 with
the frostyard standard set reported 35 findings on 2026-10-30: 18 errcheck
(mostly deferred `tx.Rollback`) and 17 staticcheck. `mise.toml` pinned only svu,
so local and CI tool versions could drift. A read-only reviewer had no gate
without pnpm, because `make check` installs and runs the frontend toolchain.
The 2026-10-30 review filed this as issue #18. The frostyard repositories
follow frostyard/core
[ADR-0043](https://github.com/frostyard/core/blob/main/docs/adr/0043-pin-repository-tools-in-mise-and-name-the-verify-gate.md)
and
[ADR-0044](https://github.com/frostyard/core/blob/main/docs/adr/0044-expose-the-make-gate-triad-in-every-repository.md):
tools pinned in `mise.toml` and `mise.lock`, and the gate triad
`make verify`, `make check` and `make ci`.

## Decision

This repository adopts those conventions.

- `mise.toml` pins golangci-lint 2.15.0 and svu; `mise.lock` carries their
  checksums. Go comes only from `go.mod`'s toolchain line, through
  `idiomatic_version_file_enable_tools = ["go"]`.
- `.golangci.yml` is frostread's v2 configuration with the standard linter set.
  A finding fails the gate: fix the code, never loosen the configuration.
- `make verify` is credential-free and non-mutating, and needs no Node or pnpm:
  `go mod tidy -diff`, `gofmt -l`, `go vet`, the pinned linter, race tests and
  documentation links.
- `make ci` runs `verify`, then the frontend tests and build and the static
  binary build. The Check workflow's `check` job runs exactly `make ci`, with
  golangci-lint installed from `mise.lock` by `jdx/mise-action`, pinned by
  commit SHA.
- `make check` is the developer gate: it formats Go code, then runs `make ci`.
  `make bump` still runs it and refuses a tree that the gate changed.
- `make vulncheck` runs govulncheck at a release pinned in the Makefile. It needs
  the network, so it stays outside the gate and runs as its own workflow job.
- `IN_NSL` (empty by default) prefixes every pnpm call, so a developer can run
  the frontend toolchain in an nsl machine.

## Consequences

A reviewer can run `make verify` on a clean checkout without credentials,
Node or a write to the tree. Every Go change must pass the linter. A linter
or Go bump is a change to `mise.toml`, `mise.lock` or `go.mod` that CI proves
first. Developers need `mise install` (or golangci-lint 2.15.0 on `PATH`):
`make lint` fails with the install command when the linter is absent, and
warns when its version differs from the pin. A new vulnerability in a
dependency turns the vulncheck job red without a code change; that job does
not gate image publication. `make check` now writes formatting changes to Go
files instead of only reporting them.

## Alternatives considered

- **Keep `make check` as the only target:** frostyard tooling and reviewers
  call `make verify` and `make ci`, and a read-only review would still need pnpm.
- **Run govulncheck inside `make ci`:** core ADR-0022 keeps network access out
  of the credential-free gate, and a new advisory would then block unrelated
  changes and image publication.
- **Install golangci-lint with its own action or a Go `tool` directive:** the
  version would then live outside `mise.lock`, and the `tool` directive can
  change lint results through module merging.

## References

- Shapes: [application design](../design/application.md#repository-gate),
  [local service contract](../specs/local-service.md#derived-artifacts),
  [roadmap Phase 1](../plans/roadmap.md#phase-1--repository-foundation-and-clix-m)
- Builds on: [ADR-0002](0002-agent-portable-instruction-surface.md),
  [ADR-0015](0015-versioned-attested-images.md)
