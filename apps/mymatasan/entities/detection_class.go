package entities

// DetectionClass is one selectable detection target in the class registry. It
// decouples the "what to detect" axis from the "how to detect" (mode) axis of a
// detection rule. Built-in rows are seeded from the configured classMap; trained
// rows are added when a custom model is activated; group rows compose other
// classes (e.g. "delivery" = courier + van).
type DetectionClass struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name        string `json:"name" form:"name" query:"name" ukey:"name" validate:"required"`
	DisplayName string `json:"displayName" form:"displayName" query:"displayName"`
	Icon        string `json:"icon" form:"icon" query:"icon"`
	// Kind is "object", "hazard", or "group".
	Kind string `json:"kind" form:"kind" query:"kind"`
	// Members is a JSON string array. For object/hazard rows it holds raw model
	// labels (e.g. ["car","truck"]); for group rows it holds child class slugs.
	Members string `json:"members" form:"members" query:"members"`
	// Source is "builtin", "trained", or "group".
	Source    string `json:"source" form:"source" query:"source"`
	IsEnabled bool   `json:"isEnabled" form:"isEnabled" query:"isEnabled"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
