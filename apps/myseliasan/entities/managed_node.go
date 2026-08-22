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
	// Kind is what this node IS: "camera" (a mymatasan NVR), "iot" (a myiotsan sensor hub), or
	// "door" (a mypintusan access controller).
	//
	// The control plane manages several sorts of appliance and they are NOT interchangeable — a
	// camera node has recordings and live views; a sensor node has telemetry and relays; a door
	// node has doors, badge decisions and an access log. A fleet UI that cannot tell them apart
	// is one that will offer to play back the footage from a door.
	//
	// It is set from the node's own ADOPT REPLY, which is fleet-key-signed and claim-code-gated —
	// not from the discovery announce, whose kind hint is unsigned and advisory (see
	// infra/pairing.Announce.Kind).
	//
	// EMPTY MEANS CAMERA. Every node adopted before this field existed is a mymatasan node, and
	// it must keep behaving exactly as it always did rather than appearing as some unknown thing
	// the UI cannot render.
	Kind        string `json:"kind" form:"kind" query:"kind"`
	// Version is the app version the node last reported on its control-channel hello.
	//
	// It is REPORTED, never assigned: the node says what it is running, and the control
	// plane records it. That distinction matters for a staged rollout, whose entire job is
	// to compare what a node was asked to install against what it actually came back
	// running. A field the control plane wrote from its own intent could only ever agree
	// with itself.
	//
	// Empty for a node that has not dialed in since this field existed, and for one that has
	// never connected at all. Empty is not "old" — it is unknown, and a rollout treats it as
	// a node it cannot judge rather than one it may skip.
	Version string `json:"version" form:"version" query:"version"`
	// VersionSeenAt is when Version was last reported, so a stale reading is visible as one.
	VersionSeenAt int64 `json:"versionSeenAt" form:"versionSeenAt" query:"versionSeenAt"`
	IP          string `json:"ip" form:"ip" query:"ip"`
	HTTPSPort   int    `json:"httpsPort" form:"httpsPort" query:"httpsPort"`
	Token       string `json:"-" form:"token" query:"token"`
	Fingerprint string `json:"fingerprint" form:"fingerprint" query:"fingerprint"`
	// MTLSPort is the node's mutual-TLS management listener port (heartbeat/release).
	MTLSPort int `json:"mtlsPort" form:"mtlsPort" query:"mtlsPort"`
	// CertExpiresAt is the unix expiry of the last node certificate this CA issued,
	// so the UI can surface renewal health.
	CertExpiresAt int64 `json:"certExpiresAt" form:"certExpiresAt" query:"certExpiresAt"`
	// AutoRenew gates whether this node's certificate is allowed to renew. The node
	// re-enrolls automatically before expiry, but the control plane REFUSES that renewal
	// unless an operator has turned this on — so a node nobody blesses lets its cert lapse
	// by itself, drops off the control channel, and goes "lost". It is a per-node
	// dead-man's switch for fleet trust: forgotten or decommissioned nodes fall out of the
	// fleet without any explicit revoke.
	//
	// DEFAULT FALSE for a newly adopted node (the initial enrollment right after adoption
	// is always allowed; only subsequent renewals are gated). Nodes adopted before this
	// field existed are backfilled to true once at startup so an existing fleet — which was
	// renewing automatically — is not surprise-expired. See INodeRegistry.BackfillAutoRenew.
	AutoRenew bool `json:"autoRenew" form:"autoRenew" query:"autoRenew"`
	// Status: "online" (adopted, reachable), "lost" (unreachable / token rejected),
	// or "self-dropped" (the node unpaired itself).
	Status string `json:"status" form:"status" query:"status"`
	// Lat/Lon are the node's position on the geographic fleet map, set by an operator
	// dragging the pin. MapPlaced distinguishes "deliberately positioned" from the zero
	// value — (0,0) is a real coordinate in the Gulf of Guinea, so a bare float can't tell
	// an unplaced node from one a prankster parked at null island. A node with MapPlaced
	// false is simply absent from the map until someone places it.
	Lat       float64 `json:"lat" form:"lat" query:"lat"`
	Lon       float64 `json:"lon" form:"lon" query:"lon"`
	MapPlaced bool    `json:"mapPlaced" form:"mapPlaced" query:"mapPlaced"`
	// SiteId is the building this appliance RESIDES IN (0 = none / standalone). A node's cameras
	// physically live at the site+floor of each placement; SiteId records where the *box* itself
	// sits, so the map can represent an in-building node by its building rather than a redundant
	// pin. A node with a SiteId is NOT drawn as its own map pin — only a building-less node is
	// (a standalone recorder, an IoT hub in the field, an off-site aggregator).
	SiteId int64 `json:"siteId" form:"siteId" query:"siteId"`
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
