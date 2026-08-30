package entities

// FaceEmbedding is ONE faceprint for an enrolled person — a fixed-length numeric vector computed
// from one enrolled photo (or captured frame) by the face-recognition model. A person has several,
// spanning angles/lighting (the "exemplar" strategy: a live face is matched by its BEST similarity
// to ANY of a person's embeddings, which is far more robust than averaging them into one prototype).
//
// PORTABILITY: the vector is stored as a base64 TEXT string, not a binary blob, so it persists
// identically on every supported SQL engine (sqlite / MariaDB / Postgres) — a Go string maps to a
// TEXT column on all three. Matching never happens in SQL; the face worker loads all embeddings into
// memory and compares with cosine similarity, so the database's only job is durable, portable storage.
//
// ENCRYPTION-AT-REST: Vector holds the base64 of the model's raw float32 bytes AFTER passing through
// infra/atrest (the same AES-256-GCM envelope the recordings/snapshots use), so a stolen database
// file does not leak biometric templates. Deleting a person crypto-shreds these rows.
type FaceEmbedding struct {
	Id       int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	PersonId int64 `json:"personId" form:"personId" query:"personId" idx:"person" validate:"required"`
	// Vector is base64(atrest-encrypted(float32-le bytes)). Dim records the vector length (128 for
	// the SFace embedder) so a future model swap can be detected rather than silently mismatched.
	Vector    string  `json:"-" form:"vector" query:"vector"`
	Dim       int     `json:"dim" form:"dim" query:"dim"`
	Model     string  `json:"model" form:"model" query:"model"`
	Source    string  `json:"source" form:"source" query:"source"` // "upload" | "camera"
	Quality   float64 `json:"quality" form:"quality" query:"quality"`
	// Thumbnail is a small base64 JPEG of the cropped face this faceprint was computed from,
	// for the enrollment UI only (never used for matching). It is what lets the People screen
	// show WHICH photos are enrolled: without it a faceprint is an invisible row, and an
	// operator can neither confirm a good enrollment nor spot the bad one poisoning matches.
	// Nullable/added by the auto-migrator; rows enrolled before this column show a placeholder.
	Thumbnail string  `json:"thumbnail" form:"thumbnail" query:"thumbnail"`
	CreatedAt int64   `json:"createdAt" form:"createdAt" query:"createdAt"`
}
