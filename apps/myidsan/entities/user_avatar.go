package entities

// UserAvatar is a user's uploaded profile picture, one row per account. The image is
// resized small client-side (a square thumbnail) before upload, so it is kept inline
// as base64 in a TEXT column rather than in file storage — it is tiny and always
// fetched as a whole. It is deliberately NOT carried in the JWT/session claims (which
// would blow the cookie size); the SPA fetches it from GET /api/profile/avatar.
type UserAvatar struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	UserLoginId int64  `json:"userLoginId" form:"userLoginId" query:"userLoginId" ukey:"ukey1" validate:"required"`
	// ContentType is the stored image's MIME type ("image/jpeg", "image/png", …).
	ContentType string `json:"contentType" form:"contentType" query:"contentType"`
	// DataB64 is the base64-encoded image bytes.
	DataB64   string `json:"-" form:"dataB64" query:"dataB64"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
