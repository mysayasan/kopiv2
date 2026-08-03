# Module: infra/webauthn/webauthn.go

## Purpose

Package `webauthn` wraps `github.com/go-webauthn/webauthn` (NEW dependency, v0.17.4) with
the two things this suite needs and the library deliberately leaves to the caller:
resolving the Relying Party identity / allowed origin from the incoming request when it is
not configured, and keeping every other package free of direct protocol-type imports. The
RP ID and the allowed origin are the security boundary of a WebAuthn ceremony, and deriving
them in ONE place means the derivation rule can be audited once instead of at every call
site.

Consumed today by `apps/myidsan/services/webauthn.go` (see
`docs/modules/apps/myidsan/services/webauthn.go.md`); nothing in this package is
myidsan-specific, so a future app could reuse it the same way `infra/mfa` is reused for
TOTP.

## Responsibilities

- `New(Settings) *Authority` — builds a per-request ceremony handler that holds no mutable
  state, so one instance is shared for the process lifetime. `Settings.RelyingPartyId` and
  `.Origins` may both be empty, in which case they are derived per request.
- `(*Authority) resolve(r *http.Request) (*lib.WebAuthn, error)` — the load-bearing method,
  called by every ceremony method below:
  - **RP ID**: `Settings.RelyingPartyId` if set, else `r.Host` with any port stripped.
  - **Allowed origin**: `Settings.Origins` if set, else `scheme://r.Host` (port kept, since
    an origin is `scheme://host[:port]`), with `scheme` taken from `r.TLS` (not from any
    header the client could forge).
  - Both are derived from the **request**, never from the client-supplied `Origin` header —
    an attacker who could nominate the origin an assertion is accepted from would defeat
    the check entirely.
  - A TLS-terminating reverse proxy sees plain HTTP on the inside, so a deployment behind
    one **must** set `webauthn.relyingPartyOrigins` explicitly; otherwise the derived
    origin (`http://host`) would never match what the browser actually sent
    (`https://host`), and every assertion would be refused — a loud, immediate failure
    rather than a silent weakening.
  - Requests `"none"` attestation (`protocol.PreferNoAttestation`) — keeps the ceremony
    privacy-preserving (no authenticator-model identifier conveyed) and avoids implying
    attestation statements are checked against a metadata service that does not exist here.
    The AAGUID the authenticator volunteers is still recorded, for diagnostics only.
  - `RequireResidentKey: protocol.ResidentKeyNotRequired()` — deliberately **not**
    requiring a resident/discoverable credential: this is a SECOND factor reached after a
    username is already known, so discoverability buys nothing, and requiring it would
    exclude older hardware keys with limited credential slots.
  - Enforces the configured timeout server-side as well as in the browser
    (`lib.TimeoutsConfig`), so a client that ignores the hint does not get an unbounded
    window to answer a challenge.
- `BeginRegistration`/`FinishRegistration` — the enrolment ceremony. `BeginRegistration`'s
  `exclude` parameter lists the user's existing credential IDs so an authenticator already
  registered to the account refuses to enrol twice (the browser reports
  `InvalidStateError`), friendlier than silently accepting a duplicate row.
- `BeginLogin`/`FinishLogin` — the assertion ceremony for an already-identified account (the
  second-factor case: the password already identified who is signing in). The caller must
  persist the returned credential's updated `SignCount`/`CloneWarning`.
- `RelyingPartyId(r)` — reports the RP ID a given request would resolve to, for display and
  diagnostics (used by `apps/myidsan/app/app.go`'s boot log line).
- `TransportsCSV(c *Credential)` — flattens a credential's transport hints for storage.

## Notes

- `Credential`, `SessionData`, `User`, `CredentialCreation`, `CredentialAssertion` are
  re-exported type aliases over the library's own types, so no caller imports the library
  directly — the same reason for the wrapper existing at all.
- `ErrDisabled` is returned by every ceremony method when `Settings.Enabled` is false;
  `Enabled()` and `UserVerificationRequired()` are the two cheap predicates a caller checks
  before doing anything else.
- `UserVerification` accepts `"preferred"` (default)/`"required"`/`"discouraged"`; an
  unrecognised value silently resolves to `protocol.VerificationPreferred` inside
  `resolve` — the config-level default-on-typo behaviour is enforced one layer up, in
  `infra/config/config_models.go.md`'s `WebAuthnConfigModel.Effective()`.
- See `docs/modules/infra/config/config_models.go.md`'s `webauthn` block for how
  `Settings` is populated from `config.json`, and `apps/myidsan/app/app.go.md` for where
  the `*Authority` is constructed and threaded through.
