package entities

// Scene is a named, ordered set of device commands run together — "movie night", "all off",
// "goodnight". Running a scene fans its actions out through the SAME actuation path a single manual
// command takes, so every gate (actuation-enabled by device, admin-only to run, profile-declared
// bounds, rate limit, audit, twin confirmation, never auto-retried) applies per action. A scene is
// convenience, not a new authority: it can command nothing a person could not command by hand.
type Scene struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name        string `json:"name" form:"name" query:"name" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	// Enabled is a soft off switch; a disabled scene is kept and editable but refuses to run and is
	// not offered to a schedule.
	Enabled   bool  `json:"enabled" form:"enabled" query:"enabled"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
