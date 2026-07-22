# Module: infra/login/kerberos.go

## Purpose

Implements Kerberos SPNEGO single sign-on (Phase 2 of
`docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md`): a domain-joined browser presents a
service ticket for myidsan's SPN via the standard `Authorization: Negotiate`
header, verified server-side against an exported keytab — no password prompt.
Like `ldap.go`, this is not a `RedirectProvider` (see `provider.go.md`): there is
no third-party redirect, just a same-origin 401-challenge/retry dance on one
fixed route. `apps/myidsan/apis/login.go`'s `kerberosLogin` is the only caller.

## Responsibilities

- `KerberosSettings` — `KeytabPath` (required), `ServicePrincipal` (the SPN the
  keytab was exported for, e.g. `HTTP/myidsan.corp.local`; empty lets gokrb5
  match any principal present in the keytab), `OnlyRealms` (case-insensitive
  realm allow-list; empty accepts any realm the keytab can decrypt tickets for).
- `NewKerberosAuthenticator(settings) (*KerberosAuthenticator, error)` — loads
  the keytab once at boot (`keytab.Load`) and normalizes `OnlyRealms` to
  uppercase. An error here (missing/unreadable keytab, no path) is
  misconfiguration: `apps/myidsan/app/app.go` logs a warning and treats
  Kerberos as "not offered" rather than failing boot, mirroring a
  half-configured OAuth provider.
- `(*KerberosAuthenticator) Negotiate(r) (*KerberosPrincipal, error)` — the
  verification path:
  1. `negotiateToken` reads `Authorization: Negotiate <token>`; missing or a
     different scheme is `ErrKerberosNoToken` — the signal to challenge, not a
     failure.
  2. The token is base64-decoded and unmarshalled as a `spnego.SPNEGOToken`;
     either failure is `ErrKerberosRejected`.
  3. `spnego.SPNEGOService(keytab, ...).AcceptSecContext` verifies the token
     against the keytab (scoped to `ServicePrincipal` when set). A rejection,
     nil verified context, or a verified context with no identity is
     `ErrKerberosRejected` (covers wrong SPN, expired/forged ticket, and clock
     skew — see `docs/HOWTO.md`'s failure-modes table).
  4. The realm (`Realm()`, falling back to `Domain()`) is upper-cased and
     checked against `OnlyRealms`; a non-empty allow-list that doesn't contain
     it is `ErrKerberosRealmNotAllowed`.
  5. Returns a `KerberosPrincipal{Username, Realm}` — the verified identity,
     carrying no email or group data (that is the directory's job, see
     `ResolveDirectoryUser` in `services/directory.go.md`).
- `KerberosChallenge(w)` — writes the `401` + `WWW-Authenticate: Negotiate`
  response that makes a domain browser retry the same request with a ticket.
- `StandaloneKerberosIdentity(principal) *Identity` — builds a login identity
  for installs with no directory configured: `username@realm` (lowercased)
  stands in for both `Subject` and `Email`. Documented fallback only —
  configuring the directory yields real emails, groups, and the same identity
  as password/LDAP logins.
- `KerberosProviderKey = "kerberos"` — used on login pages and in metrics. It is
  **not** `Identity.Provider` for a directory-resolved login (those resolve
  through `LdapProviderKey`, same as password logins, so one person is one
  account); only the standalone fallback identity carries it.

## Sentinel errors

`ErrKerberosNoToken` (challenge, not a failure), `ErrKerberosRejected` (bad/
forged/expired ticket, wrong SPN, clock skew), `ErrKerberosRealmNotAllowed`
(verified but disallowed realm). `apps/myidsan/apis/login.go`'s
`kerberosResultLabel` maps these (plus the directory-resolution errors from
`ResolveDirectoryUser`) to distinct `myidsan_federated_login_total{result=...}`
labels: `ticket_rejected`, `realm_refused`, `unreachable`, `not_in_directory`,
`identity_conflict`, `inactive`.

## Notes

- Context-key footgun: the verified principal is read off the `context.Context`
  gokrb5's SPNEGO acceptor returns using the literal string
  `"github.com/jcmturner/gokrb5/v8/ctxCredentials"` (`spnegoCtxCredentialsKey`)
  because the real constant is unexported in `gokrb5/v8/spnego`. This is pinned
  to `gokrb5 v8.4.4` — **re-verify this string against the installed version's
  `spnego/krb5Token.go` on any gokrb5 version bump**; a silent rename would make
  every ticket verify successfully but fail to extract an identity.
- `github.com/jcmturner/gokrb5/v8` is pure Go (no cgo), consistent with the
  suite's single-static-binary rule, and treated as frozen protocol code —
  upstream maintenance is slow, so this integration is deliberately a thin
  wrapper rather than depending on library internals beyond the one documented
  context key.
- Covered by `kerberos_test.go`: keytab load (missing path/file), realm
  allow-list normalization, no-token-means-challenge across several header
  shapes, garbage-token rejection, the 401 challenge response shape, and
  `StandaloneKerberosIdentity`'s derivation (including the nil-principal case).
  The full `AcceptSecContext` success path (a real service ticket from a KDC)
  is not yet exercised — it needs a domain-joined client and a real KDC/realm,
  called out in `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` as not yet live-tested.
