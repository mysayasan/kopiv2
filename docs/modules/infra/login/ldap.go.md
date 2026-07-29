# Module: infra/login/ldap.go

## Purpose

Implements LDAP/Active Directory bind authentication (Phase 1 of
`docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md`) as a credential check — not a
`RedirectProvider` (see `provider.go.md`): the caller already has a
username/password (posted form or JSON), and this package turns that pair into a
normalized `login.Identity` or a sentinel error. `apps/myidsan/services/directory.go`
is the only caller.

## Responsibilities

- `LdapSettings` — everything needed to reach and query one directory server (Active
  Directory, Samba AD, FreeIPA, OpenLDAP, 389-ds): `Host`/`Port` (default 636 for
  implicit TLS, 389 when `UseStartTLS`), `CaCertPem` (optional pinned CA; system roots
  when empty), `BindDn`/`BindPassword` (read-only service account), `BaseDn`,
  `UserFilter` (a `%s`-templated filter, default matches AD/inetOrgPerson by
  `sAMAccountName`/`uid`/`mail`), `GroupAttr` (default `memberOf`), and `SubjectAttr`
  (override for the stable-id attribute; empty = auto `objectGUID` → `entryUUID` →
  DN). Transport is **always TLS** — implicit `ldaps` by default or `StartTLS` on a
  plaintext port; there is deliberately no insecure mode. `withDefaults()`/`Validate()`
  fill in and check the settings before use.
- `LdapAuthenticate(ctx, settings, username, password) (*Identity, error)` — the full
  credential check: service bind → single-entry search (filter built from
  `UserFilter` with the username escaped) → bind AS the found entry's DN with the
  supplied password. The empty/NUL-password refusal happens **before any network
  I/O**: many LDAP servers treat an empty password as a successful
  anonymous/unauthenticated bind (RFC 4513 §5.1.2), which would otherwise let anyone
  in with a blank password. A search matching zero entries is
  `ErrLdapInvalidCredential` (same as a wrong password — indistinguishable to the
  caller); matching more than one is `ErrLdapAmbiguousUser` (a misconfigured filter
  must fail closed, never bind "one of them" — enforced with a search size limit of 2
  so ambiguity is detected without pulling the whole directory). A failed user bind
  with `LDAPResultInvalidCredentials` maps to `ErrLdapInvalidCredential`; any other
  connect/service-bind failure maps to `ErrLdapUnreachable` (an operational problem,
  not a credential one, so callers can distinguish "try again" from "wrong password").
- `LdapLookup(ctx, settings, username) (*Identity, error)` — resolves a username
  to its directory identity with **no user bind**: service bind + single-entry
  search only (the same `ldapFindUser`/`ldapIdentityFromEntry` path
  `LdapAuthenticate` uses, minus the final bind-as-user step). This is the
  resolution path for callers that have already authenticated the user by other
  means — Kerberos SPNEGO (`apps/myidsan/services/directory.go`'s
  `ResolveDirectoryUser`, see `services/directory.go.md`) proves who the user is
  but carries no email/groups, so the directory fills those in. **Never call
  this on an unauthenticated username** — it performs no credential check; the
  doc comment on the function says so explicitly.
- `LdapTest(ctx, settings, sampleUsername) *LdapTestResult` — the admin "Test
  connection" probe. It NEVER binds as the sample user (no password is taken or
  checked): with no sample username it only proves the service bind and a base-DN
  read succeed; with one, it runs the same `ldapFindUser` lookup `Authenticate` would
  and reports the matched DN, resolved subject/email, and up to 5 sample groups —
  enough to validate the filter/attribute mapping without ever touching the sample
  user's credentials.
- `BuildLdapUserFilter(template, username)` — substitutes the filter-escaped username
  (`ldap.EscapeFilter`) for every `%s` in the template. Escaping is load-bearing: the
  username is attacker-controlled input inside an LDAP filter (classic LDAP
  injection, e.g. `*)(uid=*`).
- `decodeObjectGUID` — renders AD's 16-byte binary `objectGUID` in the canonical
  mixed-endian GUID text form (first three groups little-endian, per MS-DTYP) so the
  stored subject matches what AD tooling (ADUC, PowerShell) displays for the same
  account, letting an operator correlate a stored subject with their DC.
- `ldapIdentityFromEntry` — resolves `Identity.Subject` (`SubjectAttr` override, else
  `objectGUID` then `entryUUID`, else `"dn:" + lowercased DN` as a last resort —
  logged as a real risk, since a DN changes when the account moves OUs, silently
  splitting the user into a new local account), `Email` (`mail`, falling back to
  `userPrincipalName` when it looks like an email), `Name`/`GivenName`/`FamilyName`,
  and `Groups` (the raw `GroupAttr` values, e.g. AD `memberOf` DNs). No `mail`/`upn`
  match is `ErrLdapNoEmail` — the suite's auth middleware requires a non-empty `Email`
  claim, so a login with none cannot proceed.

## Sentinel errors

`ErrLdapInvalidCredential`, `ErrLdapAmbiguousUser`, `ErrLdapUnreachable`,
`ErrLdapNoEmail` — `apps/myidsan/apis/login.go`'s `ldapResultLabel` maps each to a
distinct `myidsan_federated_login_total{result=...}` label and a distinct HTTP status
(`ErrAuthFailed`/`ErrStatusUnprocessableEntity`/`ErrInternalServerError`), and both
login surfaces treat only `ErrLdapInvalidCredential` as a genuine credential failure
for the shared `LoginGuard` per-IP lockout counters.

## Notes

- `LdapProviderKey = "ldap"` is the `Identity.Provider` value for every way of
  proving a directory identity: password bind (`LdapAuthenticate`) and, as of
  Phase 2, a Kerberos-verified principal resolved via `LdapLookup` — so one
  person always resolves to one local account regardless of which credential
  path proved it. Only the directory-less Kerberos fallback
  (`login.StandaloneKerberosIdentity`, see `kerberos.go.md`) uses a different
  provider key (`"kerberos"`).
- Pure Go (`github.com/go-ldap/ldap/v3`), no cgo — consistent with the suite's
  single-static-binary rule.
- Covered by `ldap_test.go`: filter-escaping/injection, empty/NUL-password refusal
  (no network I/O), `LdapSettings.Validate`/`withDefaults`, and `decodeObjectGUID`'s
  mixed-endian decode against a known byte layout.
- **Live-tested** (`ldap_integration_test.go`, 9 tests, gated on `RUN_LDAP_IT=1` so it
  never runs in the default `go test`/CI pass) against a real OpenLDAP 2.4 server over
  both implicit TLS (LDAPS/636) and StartTLS/389: `LdapAuthenticate` (correct bind,
  wrong password, empty password, unknown user), `memberOf` group retrieval,
  `LdapLookup` (bind-free) resolving to the same `Identity.Subject` as an authenticated
  bind for the same account, `LdapTest`'s admin probe, and CA-pinning fail-closed
  behavior. The file itself carries the container recipe (bind DN, base DN, LDIF
  fixtures) so the bench is repeatable. Not yet exercised against Active Directory or
  Samba AD specifically, where `objectGUID` (rather than `entryUUID`) is the subject
  attribute — the bind/search paths against those are still only proven through
  `apps/myidsan/services/directory_test.go` and live-boot testing against a fake host.
