package entities

// FacePerson is one enrolled identity in the global face gallery — a named person the system should
// recognize when their face appears on ANY camera that has face recognition enabled. Unlike a
// TeachSkill (which is per-camera), a person is GLOBAL: enroll once, recognized everywhere.
//
// A person carries no biometric data itself; their faceprints live in FaceEmbedding rows keyed by
// PersonId. Enabled lets an admin pause a person without deleting their enrollment. Thumbnail is a
// small base64 JPEG of a representative face for the UI only (not used for matching).
//
// FACE TEMPLATES ARE BIOMETRIC DATA. The feature is off by default and admin-only; enrollment is a
// deliberate act gated behind a consent acknowledgment, and deleting a person crypto-shreds their
// embeddings (see FaceGalleryService). See docs — GDPR Art. 9 / BIPA weight.
type FacePerson struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name      string `json:"name" form:"name" query:"name" validate:"required"`
	Notes     string `json:"notes" form:"notes" query:"notes"`
	Enabled   bool   `json:"enabled" form:"enabled" query:"enabled"`
	// Thumbnail is a small base64 JPEG of a representative face, for the roster UI only.
	Thumbnail string `json:"thumbnail" form:"thumbnail" query:"thumbnail"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
