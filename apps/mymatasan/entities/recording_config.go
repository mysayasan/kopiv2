package entities

// RecordingConfig stores per-camera NVR recording settings.
type RecordingConfig struct {
	Id             int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	CameraId       int64  `json:"cameraId" form:"cameraId" query:"cameraId" ukey:"camera" validate:"required"`
	Enabled        bool   `json:"enabled" form:"enabled" query:"enabled"`
	PreRollSec     int    `json:"preRollSec" form:"preRollSec" query:"preRollSec"`
	PostRollSec    int    `json:"postRollSec" form:"postRollSec" query:"postRollSec"`
	StoragePath    string `json:"storagePath" form:"storagePath" query:"storagePath"`
	RetentionDays  int    `json:"retentionDays" form:"retentionDays" query:"retentionDays"`
	SegmentMinutes int    `json:"segmentMinutes" form:"segmentMinutes" query:"segmentMinutes"`
	LiveStreamUrl     string `json:"liveStreamUrl" form:"liveStreamUrl" query:"liveStreamUrl"`
	StreamURL         string `json:"streamUrl" form:"streamUrl" query:"streamUrl"`
	FallbackStreamUrl string `json:"fallbackStreamUrl" form:"fallbackStreamUrl" query:"fallbackStreamUrl"`
	CreatedAt      int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
