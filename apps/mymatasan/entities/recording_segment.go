package entities

// RecordingSegment stores metadata for one recorded video clip.
type RecordingSegment struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	CameraId  int64  `json:"cameraId" form:"cameraId" query:"cameraId" validate:"required" idx:"cam_time"`
	AlertId   int64  `json:"alertId" form:"alertId" query:"alertId"`
	FilePath  string `json:"filePath" form:"filePath" query:"filePath" validate:"required"`
	StartedAt int64  `json:"startedAt" form:"startedAt" query:"startedAt" idx:"cam_time"`
	EndedAt   int64  `json:"endedAt" form:"endedAt" query:"endedAt"`
	FileSize  int64  `json:"fileSize" form:"fileSize" query:"fileSize"`
	// Codec is the on-disk video codec (e.g. "h264", "hevc"); empty for legacy rows.
	// The playback path reads it to decide whether the browser needs a transcode.
	Codec string `json:"codec" form:"codec" query:"codec"`
	// Sha256 is the hex SHA-256 of this segment's PLAINTEXT mp4, taken at finalize
	// before at-rest encryption (infra/recording.HashPlaintextFile).
	//
	// EMPTY MEANS UNHASHED, not "unchanged". Rows written before this column existed
	// have none, and so does a segment adopted after a crash — by then the file on disk
	// is already encrypted. An evidence export must report that difference rather than
	// paper over it: a digest taken at export time proves only that the file has not
	// changed since the export, which is a far weaker claim than "not altered since it
	// was recorded" and must never be presented as the latter.
	//
	// Added additively; the auto-migrator adds the column and leaves existing rows blank.
	Sha256    string `json:"sha256" form:"sha256" query:"sha256"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
}
