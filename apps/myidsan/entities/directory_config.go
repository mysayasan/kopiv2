package entities

// DirectoryConfig is the LDAP/Active Directory connection myidsan authenticates
// domain accounts against. Singleton row (Name = "default"), managed from the
// Federation → Directory admin page. BindPassword is stored encrypted at rest
// (infra/atrest) — unlike OAuth client secrets it cannot be hashed, because the
// service bind has to replay it against the directory on every login.
type DirectoryConfig struct {
	Id      int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name    string `json:"name" form:"name" query:"name" ukey:"ukey1" validate:"required"`
	Enabled bool   `json:"enabled" form:"enabled" query:"enabled"`
	// DisplayLabel names the login option on the sign-in pages ("Domain account",
	// "ACME corporate login", ...).
	DisplayLabel string `json:"displayLabel" form:"displayLabel" query:"displayLabel"`
	Host         string `json:"host" form:"host" query:"host"`
	Port         int64  `json:"port" form:"port" query:"port"`
	// UseStartTls upgrades a plaintext port (389) via StartTLS; off means implicit
	// TLS (ldaps, 636). There is no insecure mode.
	UseStartTls bool `json:"useStartTls" form:"useStartTls" query:"useStartTls"`
	// CaCertPem optionally pins the directory server's CA (PEM). Empty = system roots.
	CaCertPem    string `json:"caCertPem" form:"caCertPem" query:"caCertPem"`
	BindDn       string `json:"bindDn" form:"bindDn" query:"bindDn"`
	BindPassword string `json:"bindPassword" form:"bindPassword" query:"bindPassword"`
	BaseDn       string `json:"baseDn" form:"baseDn" query:"baseDn"`
	// UserFilter is an LDAP filter template; %s is replaced with the escaped
	// username. Empty uses the built-in AD/inetOrgPerson default.
	UserFilter  string `json:"userFilter" form:"userFilter" query:"userFilter"`
	GroupAttr   string `json:"groupAttr" form:"groupAttr" query:"groupAttr"`
	SubjectAttr string `json:"subjectAttr" form:"subjectAttr" query:"subjectAttr"`
	// Authoritative makes the group→role mapping win on EVERY login (directory is
	// the system of record); off means mappings only seed the role for users still
	// pending clearance, and manual role assignments stick.
	Authoritative bool  `json:"authoritative" form:"authoritative" query:"authoritative"`
	CreatedBy     int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt     int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy     int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt     int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
