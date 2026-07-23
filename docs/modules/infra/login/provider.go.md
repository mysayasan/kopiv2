# Module: infra/login/provider.go

## Purpose

Defines the identity-provider seam every federated login method (OAuth2 today;
LDAP/Kerberos/OIDC in later phases of `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md`) plugs into,
so `apps/myidsan/apis/login.go` can route `/api/login/{provider}` and
`/api/callback/{provider}` generically instead of hard-coding one pair of routes per
provider.

## Responsibilities

- `Identity` — the normalized result of any federated login, regardless of protocol:
  `Provider` (registry key), `Subject` (the provider's own stable unique id — Google
  `id`, GitHub numeric id, later AD `objectGUID` / OIDC `sub`), `Email`,
  `EmailVerified`, `Name`/`GivenName`/`FamilyName`/`Picture`, and `Groups` (provider-side
  group names/DNs for role mapping; empty for the social providers, populated by
  directory-backed providers later). Account matching keys on `(Provider, Subject)` —
  never on email alone, which a user can change or re-register at the provider.
- `RedirectProvider` — the interface a browser-redirect login method (OAuth2/OIDC)
  implements: `Key()` (stable lowercase identifier used in the routes and stored as
  `Identity.Provider`), `DisplayName()` (button label), `Login(w, r)` (redirect the
  browser to the provider), `Callback(r) (*Identity, error)` (consume the provider's
  redirect and return the normalized identity).
- `Registry` — holds configured providers in registration order (also the button
  render order). `Register` ignores an invalid, reserved (`default`, `providers`,
  `list` — routes of their own under `/api/login`), or duplicate key rather than
  erroring, so one misconfigured provider degrades to "not offered" instead of
  breaking login for the rest. `Get(key)` and `Keys()` are the read side.
- `BuildRegistry(conf *OAuthProvidersConfigModel)` — assembles the registry from the
  OAuth config block, registering Google/GitHub only when both `ClientId` and
  `ClientSecret` are non-empty (the stock config ships empty `google`/`github` blocks,
  and a blank client id would send the browser to the provider just to fail — a dead
  end on an air-gapped intranet).

## Notes

- `GoogleLogin` and `GithubLogin` (`google.go`, `github.go`) implement
  `RedirectProvider` and are the only two providers `BuildRegistry` wires up today.
- `apps/myidsan/apis/login.go`'s generic `providerLogin`/`providerCallback` handlers and
  `apps/myidsan/apis/federated_auth.go`'s `socialButtonsHTML` both drive off a shared
  `*Registry` instance rather than provider-specific fields, so adding a provider is a
  `Register` call, not a new route pair.
