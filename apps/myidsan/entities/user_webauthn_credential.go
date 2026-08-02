package entities

// UserWebauthnCredential is one enrolled FIDO2 / WebAuthn authenticator (a hardware key,
// or a platform authenticator such as Windows Hello or Touch ID) belonging to an account.
//
// Unlike UserMfaFactor this is a MANY-per-user table by design: the whole point of a
// security key is that you register a second one and keep it somewhere else, so losing the
// first is an inconvenience rather than a lockout. Lookups therefore filter on UserLoginId
// with an Equal filter via Get — never GetByForeign, which silently returns only one child
// row (the same trap UserMfaFactor documents).
//
// Nothing here is secret. A WebAuthn credential is a PUBLIC key: the private half never
// leaves the authenticator, which is precisely why this table needs no atrest sealing while
// UserMfaFactor.SecretEnc does — a TOTP shared secret is symmetric, so a database read
// would hand an attacker the ability to mint codes. That asymmetry is the security argument
// for preferring a key over TOTP, and it is why this table is safe to include in a backup
// without re-sealing.
//
// "No sealing" is NOT "no wrapper", though — PublicKey below is json:"-", so a backup must
// re-expose it explicitly or json.Marshal drops it and the restored credential can never
// verify. See services/backup.go's backupWebauthnCredential; that exact conflation shipped
// as a bug once and is covered by TestBackupCarriesSecurityKeyPublicKey.
type UserWebauthnCredential struct {
	Id          int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	UserLoginId int64 `json:"userLoginId" form:"userLoginId" query:"userLoginId" validate:"required"`
	// CredentialId is the authenticator's opaque credential handle, base64url-encoded.
	// It is what the browser presents at assertion time to say which key it is using, so
	// it must be unique across the whole table, not merely per user.
	CredentialId string `json:"credentialId" form:"credentialId" query:"credentialId" validate:"required"`
	// PublicKey is the COSE-encoded public key, base64-encoded. Used to verify every
	// assertion signature. Public by nature — see the type comment.
	PublicKey string `json:"-" form:"publicKey" query:"publicKey"`
	// Aaguid identifies the authenticator MODEL (not the individual device),
	// base64-encoded. Recorded for operator diagnostics — "which kind of key is this" —
	// and deliberately not used for policy, since attestation is requested as "none".
	Aaguid string `json:"aaguid" form:"aaguid" query:"aaguid"`
	// SignCount is the authenticator's monotonic signature counter and the clone
	// detector: a genuine authenticator only ever counts up, so an assertion arriving
	// with a counter at or below the stored value means the credential was copied (or the
	// authenticator does not implement counters, which is legal and reports 0 forever).
	// See services/webauthn.go for how that ambiguity is handled.
	SignCount int64 `json:"signCount" form:"signCount" query:"signCount"`
	// Transports records how the key was reachable at enrolment ("usb", "nfc",
	// "internal", …), comma-separated, so the browser can hint the right prompt later.
	Transports string `json:"transports" form:"transports" query:"transports"`
	// Label is the name the user gave the key ("YubiKey 5 — desk drawer"). This is the
	// only field a user can edit after enrolment, and it matters more than it looks: the
	// revoke button is useless if every row reads "Security key".
	Label string `json:"label" form:"label" query:"label"`
	// BackedUp reports whether the authenticator says this credential is synced to a
	// passkey provider (iCloud Keychain, a password manager). Shown to operators because
	// a synced passkey and a single hardware key have very different loss/theft profiles.
	BackedUp bool `json:"backedUp" form:"backedUp" query:"backedUp"`
	// CloneWarning is set once an assertion arrived with a non-advancing counter. Kept as
	// a flag rather than being enforced as a hard refusal — see the service for why.
	CloneWarning bool  `json:"cloneWarning" form:"cloneWarning" query:"cloneWarning"`
	CreatedBy    int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt    int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy    int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt    int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
	LastUsedAt   int64 `json:"lastUsedAt" form:"lastUsedAt" query:"lastUsedAt"`
}
