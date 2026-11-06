# Security policy

Trimlights is a web app for one household's Trimlight Edge lights. It holds
the household's Pocket ID client secret, its sign-in sessions, private house
photos, and the only path from the network to the light controller. Sign-in
uses Pocket ID with a subject allowlist and CSRF protection
([household authentication](docs/specs/household-auth.md)); bearer
development mode is limited to loopback. Controller writes go through one
audited worker with fresh baseline checks, and an ambiguous write is never
retried ([ADR-0011](docs/adr/0011-audited-household-native-controls.md)).
Treat any report that touches sign-in, sessions, the client secret, backup
bundles, house photos, or a path to a controller write with care.

## Supported versions

Trimlights is pre-release. Fixes go to `main` and ship in the next release.
Only the latest `vX.Y.Z` release and its image
`ghcr.io/bketelsen/trimlights:vX.Y.Z` get fixes. There are no backports to
older releases. The moving `:main` and `:sha-<commit>` images are not
releases.

## Reporting a vulnerability

Report it privately, through GitHub's private vulnerability reporting for
this repository: <https://github.com/bketelsen/trimlights/security>
("Report a vulnerability").

Do not open a public issue for a vulnerability. Never post secrets, tokens,
cookies, host names, domains, controller addresses, house photos or
controller captures.

## What to include

- the version: `trimlight --version`, or the image tag and digest;
- where it runs: the published image, or a local build;
- the impact, and what you expected instead;
- the steps to reproduce it, with made-up data (`trimlights.example.com`,
  `controller.example.invalid`, a synthetic snapshot).
