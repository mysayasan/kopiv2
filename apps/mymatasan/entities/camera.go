package entities

// Camera stores one discovered or manually added camera, regardless of protocol.
type Camera struct {
	Id                int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true"`
	Name              string `json:"name" form:"name" query:"name"`
	Description       string `json:"description" form:"description" query:"description"`
	Host              string `json:"host" form:"host" query:"host" validate:"required"`
	Port              int    `json:"port" form:"port" query:"port"`
	Manufacturer      string `json:"manufacturer" form:"manufacturer" query:"manufacturer"`
	Model             string `json:"model" form:"model" query:"model"`
	FirmwareVersion   string `json:"firmwareVersion" form:"firmwareVersion" query:"firmwareVersion"`
	SerialNumber      string `json:"serialNumber" form:"serialNumber" query:"serialNumber"`
	RTSPUrl           string `json:"rtspUrl" form:"rtspUrl" query:"rtspUrl"`
	SnapshotURI       string `json:"snapshotUri" form:"snapshotUri" query:"snapshotUri"`
	RTSPStatus        string `json:"rtspStatus" form:"rtspStatus" query:"rtspStatus"`
	RTSPTransport     string `json:"rtspTransport" form:"rtspTransport" query:"rtspTransport"`
	RTSPTracks        string `json:"rtspTracks" form:"rtspTracks" query:"rtspTracks"`
	LastStreamCheckAt int64  `json:"lastStreamCheckAt" form:"lastStreamCheckAt" query:"lastStreamCheckAt"`
	// HealthStatus is the live reachability state set by the camera health monitor:
	// "online", "offline", or "" (not yet checked). Distinct from RTSPStatus, which
	// is stream-resolution state, not live liveness.
	HealthStatus string `json:"healthStatus" form:"healthStatus" query:"healthStatus"`
	// LastHealthCheckAt is the unix timestamp (seconds) of the last health-state
	// change recorded by the monitor.
	LastHealthCheckAt int64 `json:"lastHealthCheckAt" form:"lastHealthCheckAt" query:"lastHealthCheckAt"`
	LastSeenAt        int64 `json:"lastSeenAt" form:"lastSeenAt" query:"lastSeenAt"`
	// TalkTransport caches the detected two-way-audio (talk-back) transport:
	// "onvif" (ONVIF RTSP audio backchannel), "tapo"/"vigi" (TP-Link proprietary
	// port-8800 protocol), "none" (probed, unsupported), or "" (not yet probed).
	TalkTransport string `json:"talkTransport" form:"talkTransport" query:"talkTransport"`
	// TalkPassword is the speaker password for transports that need one (the
	// TP-Link cloud-account password for Tapo). Never serialized to clients.
	TalkPassword     string `json:"-" form:"talkPassword" query:"talkPassword"`
	DiscoveryMethods string `json:"discoveryMethods" form:"discoveryMethods" query:"discoveryMethods"`
	IsActive         bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy        int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt        int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy        int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt        int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
