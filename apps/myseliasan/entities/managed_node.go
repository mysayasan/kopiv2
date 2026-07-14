package entities

// ManagedNode is one mymatasan node this control plane has adopted. The pairing
// token is the secret returned by the node on adoption; it authenticates later
// release calls, so it is stored here (the on-prem control-plane database).
type ManagedNode struct {
	Id     int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	NodeId string `json:"nodeId" form:"nodeId" query:"nodeId" ukey:"node_id" validate:"required"`
	Name   string `json:"name" form:"name" query:"name"`
	// Description is an optional operator note shown as a hover tooltip in the nav.
	Description string `json:"description" form:"description" query:"description"`
	// Icon is the pre-installed glyph name shown for this node in the nav (e.g. "camera").
	Icon    string `json:"icon" form:"icon" query:"icon"`
	BaseUrl string `json:"baseUrl" form:"baseUrl" query:"baseUrl"`
	// Kind is what this node IS: "camera" (a mymatasan NVR) or "iot" (a myiotsan sensor hub).
	//
	// The control plane manages two sorts of appliance and they are NOT interchangeable — a
	// camera node has recordings and live views; a sensor node has telemetry and relays. A fleet
	// UI that cannot tell them apart is one that will offer to play back the footage from a door
	// sensor.
	//
	// It is set from the node's own ADOPT REPLY, which is fleet-key-signed and claim-code-gated —
	// not from the discovery announce, whose kind hint is unsigned and advisory (see
	// infra/pairing.Announce.Kind).
	//
	// EMPTY MEANS CAMERA. Every node adopted before this field existed is a mymatasan node, and
	// it must keep behaving exactly as it always did rather than appearing as some unknown thing
	// the UI cannot render.
	Kind        string `json:"kind" form:"kind" query:"kind"`
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
	Status string `json:"status" form:"status" query:"status"`
	// OwnerRoleId is the myseliasan RoleId that adopted this node. That role gets full
	// (admin) access to the node by default; other roles need an explicit
	// NodeAccessGrant. 0 means legacy/unknown owner (no default access).
	OwnerRoleId int64 `json:"ownerRoleId" form:"ownerRoleId" query:"ownerRoleId"`
	AdoptedAt   int64 `json:"adoptedAt" form:"adoptedAt" query:"adoptedAt"`
	LastSeenAt  int64 `json:"lastSeenAt" form:"lastSeenAt" query:"lastSeenAt"`
	CreatedBy   int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
