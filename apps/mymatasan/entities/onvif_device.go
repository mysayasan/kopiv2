package entities

// OnvifDevice stores one discovered or manually added ONVIF device.
type OnvifDevice struct {
	Id                int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name              string `json:"name" form:"name" query:"name"`
	Description       string `json:"description" form:"description" query:"description"`
	Host              string `json:"host" form:"host" query:"host" validate:"required"`
	Port              int    `json:"port" form:"port" query:"port"`
	XAddr             string `json:"xAddr" form:"xAddr" query:"xAddr" ukey:"xaddr" validate:"required"`
	Types             string `json:"types" form:"types" query:"types"`
	Scopes            string `json:"scopes" form:"scopes" query:"scopes"`
	HardwareID        string `json:"hardwareId" form:"hardwareId" query:"hardwareId"`
	Manufacturer      string `json:"manufacturer" form:"manufacturer" query:"manufacturer"`
	Model             string `json:"model" form:"model" query:"model"`
	FirmwareVersion   string `json:"firmwareVersion" form:"firmwareVersion" query:"firmwareVersion"`
	SerialNumber      string `json:"serialNumber" form:"serialNumber" query:"serialNumber"`
	Username          string `json:"username" form:"username" query:"username"`
	Password          string `json:"-" form:"password" query:"password"`
	MediaXAddr        string `json:"mediaXAddr" form:"mediaXAddr" query:"mediaXAddr"`
	PTZXAddr          string `json:"ptzXAddr" form:"ptzXAddr" query:"ptzXAddr"`
	PTZSupported      bool   `json:"ptzSupported" form:"ptzSupported" query:"ptzSupported"`
	ProfileToken      string `json:"profileToken" form:"profileToken" query:"profileToken"`
	RTSPUrl           string `json:"rtspUrl" form:"rtspUrl" query:"rtspUrl"`
	SnapshotURI       string `json:"snapshotUri" form:"snapshotUri" query:"snapshotUri"`
	RTSPStatus        string `json:"rtspStatus" form:"rtspStatus" query:"rtspStatus"`
	RTSPTransport     string `json:"rtspTransport" form:"rtspTransport" query:"rtspTransport"`
	RTSPTracks        string `json:"rtspTracks" form:"rtspTracks" query:"rtspTracks"`
	LastStreamCheckAt int64  `json:"lastStreamCheckAt" form:"lastStreamCheckAt" query:"lastStreamCheckAt"`
	LastSeenAt        int64  `json:"lastSeenAt" form:"lastSeenAt" query:"lastSeenAt"`
	IsActive          bool   `json:"isActive" form:"isActive" query:"isActive"`
	CreatedBy         int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt         int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy         int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt         int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
