package entities

// CameraOnvif stores ONVIF-protocol-specific data for a camera row.
// One row per camera; linked by CameraId (unique FK to camera.id).
type CameraOnvif struct {
	Id           int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true"`
	CameraId     int64  `json:"cameraId" form:"cameraId" query:"cameraId" ukey:"camera_id" validate:"required"`
	XAddr        string `json:"xAddr" form:"xAddr" query:"xAddr" ukey:"xaddr"`
	Types        string `json:"types" form:"types" query:"types"`
	Scopes       string `json:"scopes" form:"scopes" query:"scopes"`
	HardwareID   string `json:"hardwareId" form:"hardwareId" query:"hardwareId"`
	MediaXAddr   string `json:"mediaXAddr" form:"mediaXAddr" query:"mediaXAddr"`
	PTZXAddr     string `json:"ptzXAddr" form:"ptzXAddr" query:"ptzXAddr"`
	PTZSupported bool   `json:"ptzSupported" form:"ptzSupported" query:"ptzSupported"`
	ProfileToken string `json:"profileToken" form:"profileToken" query:"profileToken"`
	Username     string `json:"username" form:"username" query:"username"`
	Password     string `json:"-" form:"password" query:"password"`
}
