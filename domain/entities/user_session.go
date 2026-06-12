package entities

// UserSession records issued SSO sessions for audit and future revocation flows.
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
	CreatedBy     int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt     int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy     int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt     int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
