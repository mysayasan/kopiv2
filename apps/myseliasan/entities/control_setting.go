package entities

// ControlSetting is a key-value row for control-plane settings that persist across
// restarts — currently the fleet key shared with adopted nodes.
type ControlSetting struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Key       string `json:"key" form:"key" query:"key" ukey:"key" validate:"required"`
	Value     string `json:"value" form:"value" query:"value"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
