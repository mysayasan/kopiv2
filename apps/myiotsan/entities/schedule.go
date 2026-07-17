package entities

// Schedule fires a scene (or a single device command) at a time — the automation half that a
// telemetry rule cannot express, because its trigger is the CLOCK, not a reading.
//
// TriggerType is:
//   "clock"   — at TimeOfDay ("HH:MM", local) on the listed Days.
//   "sunrise" — at local sunrise ± OffsetMinutes (needs the site latitude/longitude).
//   "sunset"  — at local sunset ± OffsetMinutes.
//
// Like a command, a schedule fires through the actuation chokepoint, so every gate still applies —
// a schedule can command nothing an admin has not made commandable. LastFiredAt is rounded to the
// minute and persisted so a restart in the same minute cannot double-fire (the cooldown lesson).
type Schedule struct {
	Id      int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name    string `json:"name" form:"name" query:"name" validate:"required"`
	Enabled bool   `json:"enabled" form:"enabled" query:"enabled"`
	// TriggerType is "clock" | "sunrise" | "sunset".
	TriggerType string `json:"triggerType" form:"triggerType" query:"triggerType"`
	// TimeOfDay is "HH:MM" (24h, local) for a clock trigger; ignored for sun triggers.
	TimeOfDay string `json:"timeOfDay" form:"timeOfDay" query:"timeOfDay"`
	// OffsetMinutes shifts a sun trigger (e.g. -30 = half an hour before sunset); ignored for clock.
	OffsetMinutes int `json:"offsetMinutes" form:"offsetMinutes" query:"offsetMinutes"`
	// Days is a comma-separated weekday list (0=Sunday..6=Saturday); empty = every day.
	Days string `json:"days" form:"days" query:"days"`
	// TargetType is "scene" or "command". SceneId is used for a scene; DeviceId/CommandName/Value
	// for a single command — the same fields a SceneAction carries.
	TargetType  string  `json:"targetType" form:"targetType" query:"targetType"`
	SceneId     int64   `json:"sceneId" form:"sceneId" query:"sceneId"`
	DeviceId    int64   `json:"deviceId" form:"deviceId" query:"deviceId"`
	CommandName string  `json:"commandName" form:"commandName" query:"commandName"`
	Value       float64 `json:"value" form:"value" query:"value"`
	// LastFiredAt is the unix-minute the schedule last fired, the double-fire guard.
	LastFiredAt int64 `json:"lastFiredAt" form:"lastFiredAt" query:"lastFiredAt"`
	CreatedBy   int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt   int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy   int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt   int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
