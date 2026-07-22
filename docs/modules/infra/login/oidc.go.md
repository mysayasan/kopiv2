# Module: infra/login/oidc.go

## Purpose

Implements `OidcLogin`, a generic OpenID Connect relying party (Phase 3 of
`docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md`): one instance per configured IdP
(Keycloak, Authentik, ADFS, Entra, or any spec-compliant issuer), so an
enterprise can federate against its own IdP without a bespoke integration the
way Google/GitHub each needed one. Unlike `ldap.go`/`kerberos.go`, this **is**
a `RedirectProvider` (see `provider.go.md`) — the login is a real
authorization-code redirect to the IdP, verified via OIDC discovery and JWKS,
not a same-origin credential check or ticket negotiation.

## Responsibilities

- `NewOidcLogin(ctx, cfg OidcProviderConfigModel) (*OidcLogin, error)` — runs
  discovery (`{issuer}/.well-known/openid-configuration`, `oidcDiscoveryTimeout`
  = 10s, optionally through an `http.Client` pinned to `cfg.CaCertPath` for a
  private-CA intranet IdP) and builds the `oauth2.Config` (endpoint from
  discovery, scopes defaulting to `openid`/`profile`/`email`) plus the
  `oidc.IDTokenVerifier` (JWKS-backed, scoped to `cfg.ClientId`). Any failure —
  blank key/issuer/client id/secret/redirect, unreachable issuer, unparsable CA
  file — is a returned error; the caller (`BuildRegistry`) logs a warning and
  skips the provider rather than aborting boot, so a typo'd issuer or an IdP
  that's down at startup costs one restart, not the whole login page.
- `(*OidcLogin) Login(w, r)` — starts the authorization-code flow with PKCE
  (S256) and a nonce: `oauth2.GenerateVerifier()` and a random URL-safe token
  each ride a single-use, `HttpOnly`, callback-path-scoped cookie
  (`oidcFlowCookie`, 5-minute `MaxAge`) alongside the existing shared state
  cookie (`NewOAuthState`, `config.go.md`) — the same "only the browser that
  started the flow can finish it" pattern the state cookie already uses,
  extended to the two OIDC-specific secrets.
- `(*OidcLogin) Callback(r) (*Identity, error)` — validates state
  (`ValidateOAuthState`), reads back the PKCE verifier and nonce cookies (both
  missing is treated as "restart the sign-in," not silently skipped),
  exchanges the code with `oauth2.VerifierOption(pkceVerifier)`, extracts and
  verifies the `id_token` against the discovered JWKS, checks the token's
  `nonce` against the flow cookie, then hands the claims to
  `OidcIdentityFromClaims`.
- `OidcIdentityFromClaims(key, subject, claims, groupsClaim, skipEmailVerified) (*Identity, error)`
  — pure claims mapping, exported and unit-tested:
  - `Provider` is always `OidcProviderKeyPrefix + key` (`"oidc:" + key`) — a
    dedicated namespace so an OIDC key can never collide with a built-in
    provider's identity space, and so `Identity.Provider` alone tells
    `apps/myidsan/services/directory.go`'s group→role mapping which provider a
    login came from (see `AdmitExternalIdentity` in `services/directory.go.md`).
  - `email_verified`: a claim that is present and explicitly `false` refuses the
    login (`identity provider reports the account email as unverified`) unless
    `cfg.InsecureSkipEmailVerified` is set; an **absent** claim is accepted as-is
    (many IdPs, especially intranet Keycloak without SMTP, never set it) —
    `EmailVerified` on the returned `Identity` is `true` whenever the claim was
    true or the override is on.
  - `Groups` comes from `claimStrings`, which accepts either a JSON array of
    strings or a single bare string (ADFS emits a bare string, not a
    single-element array, for a user in exactly one group).
  - No subject (`sub`) is a hard error — there is nothing to key the account on.
- `oidcHTTPClient(caCertPath)` — returns `nil` (default client) when no CA is
  pinned, or an `http.Client` with a `RootCAs` pool built from the PEM file
  otherwise; used for both discovery and the runtime `oidc.ClientContext` on
  token exchange/JWKS fetch, so a private-CA IdP is trusted consistently across
  the whole flow, not just at startup discovery.

## Notes

- `github.com/coreos/go-oidc/v3` is a thin layer over the already-vendored
  `golang.org/x/oauth2` (no new OAuth2 client stack) — consistent with the
  suite's preference for small, focused dependencies over a heavier SSO SDK.
  Pulls in `github.com/go-jose/go-jose/v4` (JWKS/JWT parsing) as a new
  transitive dependency.
- Config lives in `OidcProviderConfigModel` (`login_models.go`), a slice
  (`OAuthProvidersConfigModel.Oidc`) rather than a single struct like
  Google/GitHub, since an install may federate against more than one IdP at
  once (e.g. a corporate Keycloak and a partner's Entra tenant).
- `BuildRegistry` (`provider.go.md`) registers a configured entry only when
  `ClientId`/`ClientSecret` are non-empty **and** `NewOidcLogin` succeeds;
  reaching either warning path renders the provider as "not offered," never a
  broken button. The client secret can also come from
  `OIDC_<KEY>_CLIENT_SECRET` (`infra/apphost/run.go.md`).
- Covered by `oidc_test.go`: claims mapping across groups-claim shapes (array,
  bare string, absent), unverified-email refusal plus the override plus
  absent-claim acceptance, successful discovery against an `httptest` issuer
  (endpoints carried through into the built `oauth2.Config`, key normalized to
  lowercase, display name defaulting to the key), and `NewOidcLogin`'s
  validation errors for a missing key/issuer/secret/redirect. The full
  authorization-code round trip (`Login`/`Callback` against a real IdP) is not
  yet exercised in automated tests — noted in
  `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` alongside LDAP/Kerberos as needing a
  live bench (Keycloak) test.
