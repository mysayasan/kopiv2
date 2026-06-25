package entities

// ManagedNode is one mymatasan node this control plane has adopted. The pairing
// token is the secret returned by the node on adoption; it authenticates later
// release calls, so it is stored here (the on-prem control-plane database).
type ManagedNode struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	NodeId      string `json:"nodeId" form:"nodeId" query:"nodeId" ukey:"node_id" validate:"required"`
	Name        string `json:"name" form:"name" query:"name"`
	BaseUrl     string `json:"baseUrl" form:"baseUrl" query:"baseUrl"`
	IP          string `json:"ip" form:"ip" query:"ip"`
	HTTPSPort   int    `json:"httpsPort" form:"httpsPort" query:"httpsPort"`
	Token       string `json:"-" form:"token" query:"token"`
	Fingerprint string `json:"fingerprint" form:"fingerprint" query:"fingerprint"`
	// MTLSPort is the node's mutual-TLS management listener port (heartbeat/release).
	MTLSPort int `json:"mtlsPort" form:"mtlsPort" query:"mtlsPort"`
	// CertExpiresAt is the unix expiry of the last node certificate this CA issued,
	// so the UI can surface renewal health.
	CertExpiresAt int64 `json:"certExpiresAt" form:"certExpiresAt" query:"certExpiresAt"`
	// Status: "online" (adopted, reachable), "lost" (unreachable / token rejected),
	// or "self-dropped" (the node unpaired itself).
	Status     string `json:"status" form:"status" query:"status"`
	AdoptedAt  int64  `json:"adoptedAt" form:"adoptedAt" query:"adoptedAt"`
	LastSeenAt int64  `json:"lastSeenAt" form:"lastSeenAt" query:"lastSeenAt"`
	CreatedBy  int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt  int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy  int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt  int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
