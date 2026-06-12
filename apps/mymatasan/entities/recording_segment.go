package entities

// RecordingSegment stores metadata for one recorded video clip.
type RecordingSegment struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	CameraId  int64  `json:"cameraId" form:"cameraId" query:"cameraId" validate:"required"`
	AlertId   int64  `json:"alertId" form:"alertId" query:"alertId"`
	FilePath  string `json:"filePath" form:"filePath" query:"filePath" validate:"required"`
	StartedAt int64  `json:"startedAt" form:"startedAt" query:"startedAt"`
	EndedAt   int64  `json:"endedAt" form:"endedAt" query:"endedAt"`
	FileSize  int64  `json:"fileSize" form:"fileSize" query:"fileSize"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
}
