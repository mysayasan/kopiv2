package entities

// ObjectObservation is one presence interval recorded by the metadata recorder: a
// span during which a given object label was continuously seen on a camera (the
// "text recording" of what the camera saw). It is coalesced from many sampled frames
// into a single row so the metadata scales for long retention, and it links to
// footage by time (camera + started/ended overlap the recording segments).
//
// Attributes and TrackId are reserved for Phase-2 attribute enrichment (per-object
// tracking + color/clothing) — they ship empty and need no migration to populate.
type ObjectObservation struct {
	Id            int64   `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	CameraId      int64   `json:"cameraId" form:"cameraId" query:"cameraId" validate:"required" idx:"cam_time"`
	Label         string  `json:"label" form:"label" query:"label"`
	StartedAt     int64   `json:"startedAt" form:"startedAt" query:"startedAt" idx:"cam_time"`
	EndedAt       int64   `json:"endedAt" form:"endedAt" query:"endedAt"`
	MaxConfidence float64 `json:"maxConfidence" form:"maxConfidence" query:"maxConfidence"`
	MaxCount      int     `json:"maxCount" form:"maxCount" query:"maxCount"`
	SampleCount   int     `json:"sampleCount" form:"sampleCount" query:"sampleCount"`
	PeakBox       string  `json:"peakBox" form:"peakBox" query:"peakBox"`
	// PeakAt is the unix second of the frame where PeakBox was captured (max
	// confidence). Playback seeks here so the drawn box lines up with the object's
	// clearest on-screen moment. 0 for rows recorded before this was tracked.
	PeakAt int64 `json:"peakAt" form:"peakAt" query:"peakAt"`
	Attributes    string  `json:"attributes" form:"attributes" query:"attributes"`
	TrackId       string  `json:"trackId" form:"trackId" query:"trackId"`
	SegmentId     int64   `json:"segmentId" form:"segmentId" query:"segmentId"`
	CreatedAt     int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
}
