package entities

// ObjectAppearance is ONE appearance descriptor for one recorded sighting — a fixed-length
// numeric vector describing what a person or vehicle looked like at the clearest moment of
// an ObjectObservation interval.
//
// It is what lets an operator point at a sighting and ask "where else did this go?" without
// the appliance re-decoding a month of video to compare. One vector per presence interval,
// not per frame: the observation is already the coalesced unit, and its peak frame is by
// construction the best-confidence view the camera got.
//
// WHY A SEPARATE TABLE rather than a column on ObjectObservation. The observation index is
// the highest-write table the metadata recorder owns and is read constantly by the search
// grid, which selects whole rows; hanging a two-kilobyte vector off every row makes every
// unrelated query drag it across the wire. Keeping it beside means appearance can also be
// purged, or never written at all, without touching the index the rest of the product reads.
//
// PORTABILITY AND ENCRYPTION-AT-REST follow FaceEmbedding exactly, for the same reasons:
// Vector is base64 TEXT (identical on sqlite / MariaDB / Postgres — matching never happens
// in SQL) holding the model's raw float32 bytes AFTER passing through infra/atrest, so a
// stolen database file does not hand over a searchable index of who was where.
//
// A vector is NOT biometric identification the way a faceprint is — it describes clothing,
// build and colour, not a face — but it is still a tracking aid, and it is treated with the
// same care rather than a lesser one.
type ObjectAppearance struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// ObservationId ties this back to the sighting it describes. Indexed so the
	// observation's own delete/purge can find and shred it.
	ObservationId int64 `json:"observationId" form:"observationId" query:"observationId" idx:"observation" validate:"required"`
	// CameraId and SeenAt are DENORMALISED from the observation on purpose. A ranked
	// appearance search reads every candidate vector in a window and scores it in memory;
	// having to join back to the observation index for each one to discover when and where
	// it was would turn one indexed range scan into a scan plus N lookups.
	CameraId int64 `json:"cameraId" form:"cameraId" query:"cameraId" idx:"cam_time" validate:"required"`
	SeenAt   int64 `json:"seenAt" form:"seenAt" query:"seenAt" idx:"cam_time"`
	// Label is the object class this describes ("person", "car", ...). Ranking is always
	// scoped to one label: a person and a car are both points in the same feature space and
	// will happily return a similarity score for each other, which is a confident answer to
	// a question nobody asked.
	Label string `json:"label" form:"label" query:"label"`
	// Vector is base64(atrest-encrypted(float32-le bytes)).
	Vector string `json:"-" form:"vector" query:"vector"`
	// Dim and Model identify the feature space. Vectors from two different models must
	// NEVER be compared — cosine similarity across unrelated spaces returns plausible
	// numbers with no meaning behind them, which is worse than returning nothing because it
	// looks like a result. Every query filters on Model, so swapping the embedder degrades
	// to "no older matches" rather than to silent nonsense.
	Dim   int    `json:"dim" form:"dim" query:"dim"`
	Model string `json:"model" form:"model" query:"model"`
	// Confidence is the detector's confidence in the sighting the crop came from. Carried
	// so a ranking can prefer a clear view over a marginal one at equal similarity.
	Confidence float64 `json:"confidence" form:"confidence" query:"confidence"`
	CreatedAt  int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
}
