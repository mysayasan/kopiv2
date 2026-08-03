# Module: apps/myidsan/entities/user_webauthn_credential.go

## Purpose

Entity for one enrolled FIDO2 / WebAuthn authenticator (a hardware key, or a platform
authenticator such as Windows Hello or Touch ID) belonging to an account — the security-key
second factor alongside TOTP (`entities/user_mfa_factor.go.md`). Note that this ships as its
**own** table rather than as `UserMfaFactor.Kind = "webauthn"`, which is what that entity's
comment anticipated; the two verification shapes (a code comparison vs. a signature check
bound to the request origin, against MANY rows instead of one) diverged enough that a
combining type at the login gate (`apis/mfa_challenge.go.md`'s `mfaChallenger`) made more
sense than folding a second kind into the existing table.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `UserLoginId` | FK to the owning account. **Many rows per user by design** — the whole point of a security key is registering a second one and keeping it somewhere else, so losing the first is an inconvenience rather than a lockout. |
| `CredentialId` | The authenticator's opaque credential handle, base64url-encoded. What the browser presents at assertion time to say which key it is using — unique across the **whole table**, not merely per user. |
| `PublicKey` | `json:"-"` — the COSE-encoded public key, base64-encoded. Used to verify every assertion signature. Public by nature; see "Why no sealing" below. |
| `Aaguid` | Identifies the authenticator **model** (not the individual device), base64-encoded. Recorded for operator diagnostics only — never used for policy, since attestation is requested as `"none"`. |
| `SignCount` | The authenticator's monotonic signature counter and the clone detector — see "Clone detection" below. |
| `Transports` | Comma-separated hints (`"usb"`, `"nfc"`, `"internal"`, …) recorded at enrolment so the browser can hint the right prompt later. |
| `Label` | The name the user gave the key ("YubiKey 5 — desk drawer"). The only field a user can edit after enrolment, and it matters: a revoke button is useless if every row reads "Security key". |
| `BackedUp` | Whether the authenticator reports this credential as synced to a passkey provider (iCloud Keychain, a password manager) — shown to operators because a synced passkey and a single hardware key have very different loss/theft profiles. |
| `CloneWarning` | Set once an assertion arrived with a non-advancing counter. A flag, not a hard refusal — see "Clone detection" below. |
| `CreatedBy / UpdatedBy` | Audit user IDs. |
| `CreatedAt / UpdatedAt` | Audit timestamps (Unix). |
| `LastUsedAt` | Unix timestamp of the last successful assertion. |

## Why no sealing

Nothing in this table is secret. A WebAuthn credential is a **public key**: the private
half never leaves the authenticator, which is precisely why `PublicKey` needs no
`infra/atrest` sealing while `UserMfaFactor.SecretEnc` does — a TOTP shared secret is
symmetric, so a database read would hand an attacker the ability to mint codes. That
asymmetry is the security argument for preferring a key over TOTP, and it is why this table
is safe to include in a backup archive without re-sealing (`services/backup.go.md`).

"No sealing" is **not** "no wrapper", though — `PublicKey` is `json:"-"`, so a backup must
re-expose it explicitly (`backupWebauthnCredential` in `services/backup.go`) or
`json.Marshal` drops it and the restored credential can never verify. That exact conflation
shipped as a bug once and is covered by `TestBackupCarriesSecurityKeyPublicKey`.

## Clone detection

`SignCount` is the replay/clone guard: a genuine authenticator only ever counts up, so an
assertion arriving with a counter at or below the stored value is the spec's documented
clone signal. It is also legal for an authenticator to never implement counters and report
`0` forever (true of most platform authenticators and every synced passkey), which makes
that signal ambiguous rather than proof. `services/webauthn.go.md`'s `FinishAssert`
therefore **accepts** the assertion anyway and sets `CloneWarning` on the row, returning a
non-fatal `note` that the caller (`apis/login.go.md`'s `webauthnLoginFinish`) writes to the
audit trail as `services.ActionWebAuthnClone` — refusing would lock users out of working
hardware on a signal the spec itself calls ambiguous.

## Notes

- Lookups filter on `UserLoginId` with an `Equal` filter (`Get`), **never**
  `dbsql.GetByForeign` — that generic helper silently returns only one child row, which is
  precisely wrong for a table whose whole purpose is holding several keys per account
  ([[getbyforeign-limit1-bug]]).
- Registered in `apps/myidsan/app/app.go`'s `Entities()` for bootstrap schema generation,
  alongside `UserMfaFactor`/`UserMfaRecoveryCode` (see `apps/myidsan/app/app.go.md`).
- Owned exclusively by `apps/myidsan/services/webauthn.go` (`IWebAuthnService`); no other
  service reads or writes this table, mirroring how `UserMfaFactor` is owned solely by
  `services/mfa.go`.
- Included in the `.idbackup` archive's `mfa` section (`services/backup.go.md`) — restored
  verbatim apart from the FK remap, since nothing here needs re-sealing.
