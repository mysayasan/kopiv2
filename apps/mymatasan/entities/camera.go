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
	LastSeenAt        int64  `json:"lastSeenAt" form:"lastSeenAt" query:"lastSeenAt"`
	DiscoveryMethods  string `json:"discoveryMethods" form:"discoveryMethods" query:"discoveryMethods"`
	IsActive          bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy         int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt         int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy         int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt         int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
