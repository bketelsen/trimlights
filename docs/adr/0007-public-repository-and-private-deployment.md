# 0007 — Publish synthetic fixtures and keep deployment settings private

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

The owner selected the public repository `bketelsen/trimlights` and explicitly
required scrubbing personal details and test data, with no baked-in controller
address in Compose. The workspace has never been committed, so publication can
start with a clean history. Original captures remain useful private evidence.

## Decision

Publish application code and synthetic fixtures to the selected repository, with
GHCR images. Keep raw captures, household names, addresses, domains, credentials,
photos and actual deployment configuration in ignored local storage. Replace all
published snapshot bytes, including unknown fields, with deterministic synthetic
data. Label public vectors as synthetic rather than original capture evidence.
Preserve the original research locally for private hardware validation.

Read the controller address from `TRIMLIGHT_CONTROLLER_HOST` or an explicit
`--host` flag. An explicitly empty flag disables an environment-provided address.
Compose interpolates image, bind address, storage paths, controller host and
controller timezone from deployment variables. Controller access defaults off.

A diagnostic write probe requires an explicit SHA-256 fingerprint of a reviewed
snapshot's non-clock bytes, in addition to the supported disabled-slot shape.
Do not bake household pattern names into write authorization. Verify the fresh
baseline fingerprint before sending the edit-session command or any write.

Redact household details from existing ADRs at the owner's explicit request,
without changing their architectural decisions; retain originals privately.
Use a GitHub noreply commit email for this repository.

## Consequences

The public suite can be reproduced without private data. Synthetic vectors test
framing and simulated behavior but cannot substitute for hardware evidence.
Images contain only the static application binary; local data is excluded from
build context. Publishing the source does not expose the running homelab app.

## Alternatives considered

- Redacting names alone: unknown raw snapshot fields could retain identifying data.
- Keeping the repository private: contradicts the selected public destination.
- Broadening the probe by removing name checks: would weaken its baseline guard.

## References

- Builds on: [ADR-0003](0003-shared-homelab-application.md),
  [ADR-0004](0004-pocket-id-and-truenas.md).
- Shapes: [application design](../design/application.md),
  [local service contract](../specs/local-service.md), [roadmap](../plans/roadmap.md).
