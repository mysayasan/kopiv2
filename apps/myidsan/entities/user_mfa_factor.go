package entities

// UserMfaFactor is one enrolled second factor for a local (or directory-password)
// account. It lives in its own table rather than as columns on UserLogin because
// SecretEnc is sealed at rest (infra/atrest) and must never ride along in a
// UserLogin projection — the same discipline that keeps the password hash out of
// user listings.
//
// Lookups filter on UserLoginId with an Equal filter (Get), never GetByForeign —
// that generic helper silently returns only one child row, which would break once
// a second Kind (WebAuthn) is added per user.
type UserMfaFactor struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	UserLoginId int64  `json:"userLoginId" form:"userLoginId" query:"userLoginId" validate:"required"`
	// Kind is "totp" — the only value this table holds, and in practice the only one it
	// ever will. Security keys were the anticipated second kind, but they landed in their
	// own table (UserWebauthnCredential) instead: they are MANY-per-user where this is one,
	// and they carry a public key, a signature counter and transport hints where this
	// carries a sealed shared secret and a time-step. The column stays because the queries
	// filter on it and a future kind that IS shaped like a shared secret would fit here.
	Kind string `json:"kind" form:"kind" query:"kind" validate:"required"`
	// SecretEnc is the atrest-sealed, base64-wrapped TOTP shared secret. It is NEVER
	// returned by any API — not even to the owning user after enrollment completes.
	SecretEnc string `json:"-" form:"secretEnc" query:"secretEnc"`
	// Label is the device name the user typed at enrollment ("Pixel 8").
	Label string `json:"label" form:"label" query:"label"`
	// ConfirmedAt is 0 until the user proves one code. An unconfirmed factor never
	// gates a login and is overwritten by a fresh enroll.
	ConfirmedAt int64 `json:"confirmedAt" form:"confirmedAt" query:"confirmedAt"`
	// LastStep is the last accepted TOTP time-step — the replay guard. Any code whose
	// step is ≤ LastStep is refused (see infra/mfa.Validate).
	LastStep   int64 `json:"lastStep" form:"lastStep" query:"lastStep"`
	CreatedBy  int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt  int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy  int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt  int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
	LastUsedAt int64 `json:"lastUsedAt" form:"lastUsedAt" query:"lastUsedAt"`
}
