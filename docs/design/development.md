# Development and local testing

Living document. Rationale: [ADR-0016](../adr/0016-adopt-the-frostyard-make-gate.md),
[ADR-0007](../adr/0007-public-repository-and-private-deployment.md).
Contracts: [local service and CLI](../specs/local-service.md).

## Overview

How to build Trimlights, run its gates, and try the service and the web app
locally without the household controller. The root
[README](../../README.md#develop) has the short form.

## Design

### Build

Run `mise install` for the pinned tools: Go 1.27.4 comes from the `go` line of
`go.mod`, which is both the minimum and the only Go version in `go.mod`; the
CI workflow sets up the same release. golangci-lint and svu
come from `mise.toml` (checksums in `mise.lock`). Use Node 24.19.0 and pnpm
11.25.0 (versions pinned in CI). `web/package.json` also pins pnpm
(`packageManager`, with its hash) and requires Node 24.19.0 or later
(`engines`). Node is only a build dependency.

```sh
make verify   # tidy, gofmt -l, vet, lint, race tests, doc links; no pnpm, no writes
make ci       # verify, then frontend checks/tests/build and the static binary (CI runs this)
make check    # gofmt -w, then make ci: run before declaring a change done
./build/trimlight --help
```

`make vulncheck` runs the pinned govulncheck and needs the network. Set
`IN_NSL='nsl run -m <machine>'` to run pnpm in an nsl machine. No target contacts
lighting hardware ([repository gate](application.md#repository-gate)).
The frontend tests (`pnpm --dir web test`, run by `make web-test`) use `node:test`
with no test framework. They first compile a few components with the project's
TypeScript compiler into the ignored `web/.test-build` directory, then render them
to static HTML with `react-dom/server` as smoke tests
([frontend design](application.md#application-boundaries)).
`make ci` (and so `make check`) also runs `web-check`, which runs `pnpm run check`
in `web/`: ESLint (typescript-eslint, React hooks, jsx-a11y, no
`dangerouslySetInnerHTML`), the Prettier check and TypeScript checks of the app,
the `.mjs` tests, the build scripts and the Vite config. Run
`pnpm run format` in `web/` to apply Prettier. ESLint uses the TypeScript 6 API
(`typescript` is an alias of `@typescript/typescript6`); `tsc` is TypeScript 7
(`@typescript/native`), because TypeScript 7.0 ships no compiler API.
The binary is written to `build/trimlight` and `build/<architecture>/trimlight`.
All public protocol fixtures are synthetic; no original household captures are
included. Python is not required to build, test or run the application.

### Local development API

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

## Operational notes

### Privacy and evidence

Private deployment files, raw captures, experiment output, photos, databases and
builds are ignored. The public [protocol notes](../../protocol-observations.json) record
observed structure and limitations. [Fixture provenance](../../internal/trimlight/testdata/README.md)
distinguishes synthetic regression data from private hardware evidence. Do not
commit new real snapshots, even when their visible pattern names look harmless.

## References

- Rationale: [ADR-0016](../adr/0016-adopt-the-frostyard-make-gate.md),
  [ADR-0007](../adr/0007-public-repository-and-private-deployment.md)
- Contracts: [local service and CLI](../specs/local-service.md)
- Design: [repository gate](application.md#repository-gate)
- Built in: [roadmap](../plans/roadmap.md)
