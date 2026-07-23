# Module: infra/login/provider.go

## Purpose

Defines the identity-provider seam every browser-redirect federated login method
(Google/GitHub OAuth2, and now generic OIDC, Phase 3 of
`docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md`) plugs into, so `apps/myidsan/apis/login.go` can
route `/api/login/{provider}` and `/api/callback/{provider}` generically instead of
hard-coding one pair of routes per provider. LDAP (`ldap.go.md`, Phase 1, shipped) and
Kerberos (`kerberos.go.md`, Phase 2, shipped) are credential/ticket checks, not
redirects, so neither implements `RedirectProvider` or registers into this
`Registry` — they are parallel seams beside `AuthenticateDefault`, invoked directly by
`apps/myidsan/apis/login.go`/`apps/myidsan/services/directory.go`.

## Responsibilities

- `Identity` — the normalized result of any federated login, regardless of protocol:
  `Provider` (registry key, or `login.LdapProviderKey` for directory logins),
  `Subject` (the provider's own stable unique id — Google `id`, GitHub numeric id, AD
  `objectGUID`/RFC 4530 `entryUUID` for LDAP, `sub` for OIDC), `Email`,
  `EmailVerified`, `Name`/`GivenName`/`FamilyName`/`Picture`, and `Groups` (provider-side
  group names/DNs for role mapping via `apps/myidsan/entities.FederatedGroupMapping`;
  empty for the social providers, populated by LDAP's `memberOf` today). Account
  matching keys on `(Provider, Subject)` — never on email alone, which a user can
  change or re-register at the provider.
- `RedirectProvider` — the interface a browser-redirect login method (OAuth2/OIDC)
  implements: `Key()` (stable lowercase identifier used in the routes and stored as
  `Identity.Provider`), `DisplayName()` (button label), `Login(w, r)` (redirect the
  browser to the provider), `Callback(r) (*Identity, error)` (consume the provider's
  redirect and return the normalized identity).
- `Registry` — holds configured providers in registration order (also the button
  render order). `Register` ignores an invalid, reserved (`default`, `providers`,
  `list`, `ldap`, `kerberos` — routes/login methods of their own under
  `/api/login`), or duplicate key rather than erroring, so one misconfigured
  provider degrades to "not offered" instead of breaking login for the rest.
  `ldap`/`kerberos` were added to the reserved set alongside OIDC support so a
  configured OIDC `key` (an operator-chosen string, unlike the fixed Google/GitHub
  keys) can never shadow either fixed route. `Get(key)` and `Keys()` are the read
  side.
- `BuildRegistry(conf *OAuthProvidersConfigModel)` — assembles the registry from the
  OAuth config block: registers Google/GitHub only when both `ClientId` and
  `ClientSecret` are non-empty (the stock config ships empty `google`/`github` blocks,
  and a blank client id would send the browser to the provider just to fail — a dead
  end on an air-gapped intranet); then, for every `conf.Oidc` entry, requires the same
  non-empty `ClientId`/`ClientSecret` before attempting `NewOidcLogin` (`oidc.go.md`),
  which additionally runs live discovery against the issuer. Either a missing
  credential or a discovery failure (typo'd issuer, IdP down at boot, bad pinned CA)
  logs a `WARNING` and **skips** that one provider — the rest of the registry, and
  local/LDAP/Kerberos login, are unaffected; a skipped OIDC provider comes back on the
  next restart once its config/IdP is fixed.

## Notes

- `GoogleLogin`, `GithubLogin` (`google.go`, `github.go`), and `OidcLogin`
  (`oidc.go.md`, one instance per configured entry) implement `RedirectProvider` and
  are the providers `BuildRegistry` wires up today.
- `apps/myidsan/apis/login.go`'s generic `providerLogin`/`providerCallback` handlers and
  `apps/myidsan/apis/federated_auth.go`'s `socialButtonsHTML` both drive off a shared
  `*Registry` instance rather than provider-specific fields, so adding a provider is a
  `Register` call, not a new route pair.
- Covered by `oidc_test.go`'s `TestRegistry_ReservedAndDuplicateKeys` (registry-level
  tests colocated with the OIDC work): reserved-key rejection (including the newly
  reserved `ldap`/`kerberos`) and duplicate-key handling.
