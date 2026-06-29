package entities

// SsoCa is myidsan's persisted certificate authority for issuing relying-app SSO
// client certificates (mTLS client auth). It is a singleton row (Name = "default");
// the CA private key lives in the myidsan database, mirroring how myseliasan stores
// its fleet CA. Issued client leaves carry CN = the app's OAuth client_id.
type SsoCa struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name      string `json:"name" form:"name" query:"name" ukey:"ukey1" validate:"required"`
	CertPem   string `json:"certPem" form:"certPem" query:"certPem"`
	KeyPem    string `json:"keyPem" form:"keyPem" query:"keyPem"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
