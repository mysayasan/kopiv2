package entities

// AppAuthConfig stores OAuth-like SSO behavior for a registered relying app.
type AppAuthConfig struct {
	Id                     int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	AppRegistryId          int64  `json:"appRegistryId" form:"appRegistryId" query:"appRegistryId" ukey:"client" validate:"required"`
	ClientId               string `json:"clientId" form:"clientId" query:"clientId" ukey:"client" validate:"required"`
	ClientSecretHash       string `json:"clientSecretHash" form:"clientSecretHash" query:"clientSecretHash" validate:"required"`
	AuthCodeTTLSeconds     int64  `json:"authCodeTtlSeconds" form:"authCodeTtlSeconds" query:"authCodeTtlSeconds"`
	AccessTokenTTLSeconds  int64  `json:"accessTokenTtlSeconds" form:"accessTokenTtlSeconds" query:"accessTokenTtlSeconds"`
	SessionTTLSeconds      int64  `json:"sessionTtlSeconds" form:"sessionTtlSeconds" query:"sessionTtlSeconds"`
	RefreshTokenTTLSeconds int64  `json:"refreshTokenTtlSeconds" form:"refreshTokenTtlSeconds" query:"refreshTokenTtlSeconds"`
	RequirePKCE            bool   `json:"requirePkce" form:"requirePkce" query:"requirePkce"`
	AllowRefreshToken      bool   `json:"allowRefreshToken" form:"allowRefreshToken" query:"allowRefreshToken"`
	IsActive               bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy              int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt              int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy              int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt              int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
