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
- An unauthenticated `/auth/login` MUST NOT write to SQLite or consume a shared
  capacity. The pending flow (state hash, browser binding, nonce, PKCE verifier
  and expiry) is sealed with AES-256-GCM into the `__Host-trimlight-flow` cookie,
  with the cookie name as associated data. The key is random per process and is
  never stored, so a restart invalidates only pending logins.
- The callback MUST clear the flow cookie on success and failure, and MUST
  refuse a tampered, foreign-key, expired or state-mismatched flow before the
  token exchange. An in-memory guard holds each flow during its exchange and,
  after the IdP accepts the code, until the flow expires; a refused exchange
  releases it. The IdP's single-use code also bounds replay across restarts.
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

## Service log events

Sign-in outcomes follow the [service log contract](local-service.md#service-logs).

| Message | Level | Fields |
| --- | --- | --- |
| `sign-in succeeded` | info | `subject` |
| `sign-in denied` | warn | `subject` not in the allowlist |
| `sign-in callback refused` | warn | `reason`: `malformed_callback`, `unexpected_issuer`, `invalid_or_expired_flow`, `state_mismatch` or `flow_already_used` |
| `sign-in token exchange failed` | warn | `reason`: `idp_rejected` with `idp_status` and `oauth_error`, `transport` with `timeout`, or `invalid_token_response` |
| `sign-in verification failed` | warn | `reason`: `missing_id_token`, `expired_id_token`, `jwks_unavailable`, `issuer_mismatch`, `audience_mismatch`, `malformed_id_token`, `id_token_not_yet_valid`, `invalid_signature`, `invalid_id_token`, `nonce_mismatch` or `invalid_claims` |
| `sign-in flow unavailable`, `sign-in session not saved`, `sign-out failed` | error | `status`, `error`; `subject` when known |

- Logs MUST NOT contain authorization codes, state, nonces, PKCE verifiers, ID or
  access tokens, flow or session cookie values, session hashes, CSRF tokens or the
  client secret.
- Provider failures MUST be logged as the fixed categories above, never as raw
  exchange or verifier errors: those can quote response bodies, claim values and
  URLs with queries. `oauth_error` MUST be one of the RFC 6749 token error codes
  (`invalid_request`, `invalid_client`, `invalid_grant`, `unauthorized_client`,
  `unsupported_grant_type`, `invalid_scope`) or `unknown`. A failed OIDC setup at
  startup logs only `household login setup failed`.

## Derived artifacts

SQLite stores session hashes. Pending login flows live only in the sealed
browser cookie; the legacy `login_flows` table remains in the schema unused, and
restore still clears it ([backup contract](backups.md)). Schema migrations back up existing
databases before modifications. Runtime databases and secrets stay outside Git.

## References

- Rationale: [ADR-0004](../adr/0004-pocket-id-and-truenas.md).
- Context: [application design](../design/application.md).
- Built in: [roadmap Phase 3](../plans/roadmap.md#phase-3--shared-mobile-controls-and-pattern-library-ml).
