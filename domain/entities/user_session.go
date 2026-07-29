package entities

// UserSession records issued SSO sessions so they can be listed and revoked.
//
// The authoritative session state is still the cache entry at sso:session:<sid> — that is
// what the auth middleware validates on every request, and deleting it is what actually
// ends a session. This table exists because the cache is keyed by session id alone, so it
// can answer "is this session valid?" but not "which sessions does this user have?", which
// is the question both an administrator revoking access and a user reviewing their own
// devices need answered. Rows are therefore an index over the cache, not a second source
// of truth: a row whose cache entry is gone is already dead regardless of what it says.
//
// The table was declared and created long before anything wrote to it. IpAddress,
// UserAgent and LastSeenAt were added when it was finally populated — without them a
// "your active sessions" screen can only say how many, not which.
type UserSession struct {
	Id            int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	SessionId     string `json:"sessionId" form:"sessionId" query:"sessionId" ukey:"ukey1" validate:"required"`
	UserLoginId   int64  `json:"userLoginId" form:"userLoginId" query:"userLoginId" fkey:"fkey1" validate:"required"`
	AppCode       string `json:"appCode" form:"appCode" query:"appCode"`
	Issuer        string `json:"issuer" form:"issuer" query:"issuer"`
	Audience      string `json:"audience" form:"audience" query:"audience"`
	PolicyVersion int64  `json:"policyVersion" form:"policyVersion" query:"policyVersion"`
	ExpiresAt     int64  `json:"expiresAt" form:"expiresAt" query:"expiresAt"`
	RevokedAt     int64  `json:"revokedAt" form:"revokedAt" query:"revokedAt"`
	IsActive      bool   `json:"isActive" form:"isActive" query:"isActive"`
	// IpAddress is the source address at sign-in, resolved through the trusted-proxy
	// rules so it cannot be forged by an untrusted caller.
	IpAddress string `json:"ipAddress" form:"ipAddress" query:"ipAddress"`
	// UserAgent is the signing-in client, length-capped at capture. It is what makes a
	// device list recognisable to the person reading it.
	UserAgent string `json:"userAgent" form:"userAgent" query:"userAgent"`
	// LastSeenAt is refreshed as the session is used, so a stale entry is distinguishable
	// from one in active use when deciding what to revoke.
	LastSeenAt int64 `json:"lastSeenAt" form:"lastSeenAt" query:"lastSeenAt"`
	CreatedBy     int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt     int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy     int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt     int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
