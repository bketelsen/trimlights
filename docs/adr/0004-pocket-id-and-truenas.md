# 0004 — Authenticate household access with Pocket ID on TrueNAS

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

The owner chose a custom TrueNAS app on 192.0.2.10 and the eventual address
https://trimlights.example.com. Existing Caddy terminates HTTPS. Pocket ID's
public discovery document reports issuer https://id.example.com and S256 PKCE.
The local development API is not suitable for exposure through Caddy.

## Decision

Use backend authorization-code OIDC with PKCE, state and nonce validation. Allow
only explicitly configured subjects from this issuer. Use opaque random browser
sessions, store only their token hashes in SQLite, and expire them after 12 hours.
Store authorization flow state for at most 10 minutes and bind it to an HttpOnly
browser cookie. Never send OAuth tokens or the client secret to browser storage.

Set Secure, HttpOnly, SameSite=Lax, host-only cookies. Validate the configured
external Host and require exact Origin plus a per-session CSRF token on mutations.
Use a fixed callback at https://trimlights.example.com/auth/callback. Ignore
forwarded headers for security decisions; the deployment must preserve Host.
Keep local bearer-token development mode separate from household OIDC mode.

Deploy one container with persistent local TrueNAS storage, existing Caddy in
front, and secrets mounted outside the repository. Actual dataset and image
registry remain deployment inputs. No TrueNAS resource is created by this ADR.

## Consequences

Both users share household state with stable identity attribution. Login needs
Pocket ID; valid app sessions and background work can continue during an IdP
outage. A server restart preserves sessions. Removing a configured subject denies
its sessions immediately on the next request. Global logout at Pocket ID does not
currently revoke an app session; app logout and expiry do.

SQLite migrations must create a consistent backup before changing an existing
schema. Authentication tests must cover failed state, nonce, signature, audience,
expiry, authorization and cross-site mutation attempts.

## Alternatives considered

- **Caddy-only authentication:** does not give the app its own household session
  and authorization/audit contract.
- **Browser-stored OAuth tokens:** unnecessary token exposure for a backend app.
- **Open LAN access:** does not meet the confirmed shared login requirement.

## References

- Builds on: [ADR-0003](0003-shared-homelab-application.md).
- Shapes: [application design](../design/application.md),
  [household auth contract](../specs/household-auth.md), [roadmap](../plans/roadmap.md).
- Discovery: [Pocket ID configuration](https://id.example.com/.well-known/openid-configuration).

Privacy note (2026-10-28): household addresses/domains were replaced with examples
at the owner’s request. Architectural decisions are unchanged; see
[ADR-0007](0007-public-repository-and-private-deployment.md).
