# Spec: Household authentication

This contract governs the backend OIDC login and authenticated browser API.
Integration with the household frontend is built in roadmap Phase 3.

## Interface

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/auth/login` | GET | Begin a state/nonce/PKCE-bound Pocket ID login |
| `/auth/callback` | GET | Validate login and issue an opaque session |
| `/api/v1/me` | GET | Current identity and CSRF token |
| `/api/v1/logout` | POST | Delete local session; requires CSRF and Origin |

The configured origin and issuer MUST be HTTPS URLs without query or fragment.
The origin MUST contain only scheme and authority. The callback is always that
origin plus `/auth/callback`. Allowed identities are exact OIDC `sub` values,
not mutable email addresses. The client is confidential; its secret stays in Go.

## Rules

- Authorization flows MUST use independent cryptographic state, nonce, PKCE
  verifier and browser-binding tokens. State and browser binding MUST match once.
- State MUST expire after 10 minutes, and a consumed callback MUST not replay.
- Signature, issuer, audience and expiry MUST be verified by go-oidc; nonce MUST
  match the original flow. No verification bypass flags are enabled in production.
- Only configured subjects may receive or use a session. At least one subject
  is required. Session IDs MUST be random and stored as SHA-256 hashes.
- Cookies MUST be `__Host-` prefixed, Secure, HttpOnly, SameSite=Lax, Path=/,
  without Domain. Sessions expire after 12 hours and survive service restart.
- Browser mutations MUST have an authenticated session, a matching
  `X-CSRF-Token` header and an exact configured `Origin`. Invalid requests MUST
  fail before an application handler runs.
- The request Host MUST equal the configured origin host. Forwarded headers MUST
  not change the app's origin or identity decisions.
- Local bearer mode MUST NOT be enabled simultaneously with household browser
  mode. OAuth tokens, client secrets and session IDs may not appear in URLs,
  HTML, logs or JSON responses. `/api/v1/me` returns a CSRF token deliberately.
- Logout deletes the local session and cookie. Pocket ID back-channel logout is
  not implemented; sessions otherwise last until expiry or allowlist removal.

## Derived artifacts

SQLite stores login flows and session hashes. Schema migrations back up existing
databases before modifications. Runtime databases and secrets stay outside Git.

## References

- Rationale: [ADR-0004](../adr/0004-pocket-id-and-truenas.md).
- Context: [application design](../design/application.md).
- Built in: [roadmap Phase 3](../plans/roadmap.md#phase-3--shared-mobile-controls-and-pattern-library-ml).
