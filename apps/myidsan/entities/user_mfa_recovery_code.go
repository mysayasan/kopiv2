package entities

// UserMfaRecoveryCode is one single-use break-glass code, stored only as a bcrypt
// hash. A user is issued a whole set (10) at enrollment and shown them exactly once;
// each can substitute for a TOTP code once at the /api/login/mfa step.
//
// This is a 1-N relation (many codes per user) — always queried with a Get + Equal
// filter on UserLoginId, never GetByForeign (which caps at one row and would hide
// nine of every ten codes).
type UserMfaRecoveryCode struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	UserLoginId int64  `json:"userLoginId" form:"userLoginId" query:"userLoginId" validate:"required"`
	// CodeHash is the bcrypt hash of the normalized code. Codes are high-entropy, so
	// the login-default cost is sufficient.
	CodeHash string `json:"-" form:"codeHash" query:"codeHash"`
	// UsedAt is 0 while unused; set to the consumption time on first (and only) use.
	UsedAt    int64 `json:"usedAt" form:"usedAt" query:"usedAt"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
}
