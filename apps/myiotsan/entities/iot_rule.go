package entities

// IotRule is a condition over telemetry that fires an alert. It is a near-1:1 port of
// mymatasan's DetectionRule, and the correspondence is exact where it matters:
//
//	Threshold        -> Threshold          (the value to compare against)
//	MinFrames        -> ConsecutiveSamples (debounce: N in a row before it counts)
//	CooldownSeconds  -> CooldownSeconds    (do not re-fire for N seconds)
//	SchedulePolicy   -> SchedulePolicy     (only during these hours)
//	ZonePolygon      -> DeviceId / Tag     (WHICH devices, instead of which pixels)
//
// The debounce is not decoration. A PIR sensor twitches, a door contact bounces, a
// temperature probe spikes for one sample. Firing on a single reading means an operator gets
// woken at 03:00 by a bounce, and an alert channel nobody trusts is worse than no alert
// channel. ConsecutiveSamples is the difference between an alarm and a nuisance.
type IotRule struct {
	Id      int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name    string `json:"name" form:"name" query:"name" validate:"required"`
	Enabled bool   `json:"enabled" form:"enabled" query:"enabled"`
	// Scope: a rule watches ONE device (DeviceId > 0), or every device carrying a Tag, or —
	// if neither is set — every device that has the key. Tag scope is what replaces
	// mymatasan's per-camera zone: "every door contact on floor 2", written once.
	DeviceId int64  `json:"deviceId" form:"deviceId" query:"deviceId" idx:"device"`
	Tag      string `json:"tag" form:"tag" query:"tag"`
	// Key is the telemetry key watched. Required for every condition except "offline", which
	// is about the device itself rather than any one datapoint.
	Key string `json:"key" form:"key" query:"key"`
	// Condition is one of: above, below, equals, delta, rate, stuck, offline.
	// See services.EvaluateCondition for exactly what each means.
	Condition string  `json:"condition" form:"condition" query:"condition" validate:"required"`
	Threshold float64 `json:"threshold" form:"threshold" query:"threshold"`
	// Hysteresis widens the gap the value must travel back through before the rule RESETS.
	// Without it, a temperature hovering exactly on the threshold fires, clears, fires,
	// clears — the classic flapping alert. The rule arms again only below (threshold -
	// hysteresis) for an "above" rule, and above (threshold + hysteresis) for a "below" one.
	Hysteresis float64 `json:"hysteresis" form:"hysteresis" query:"hysteresis"`
	// ConsecutiveSamples is the debounce: how many readings in a row must satisfy the
	// condition before it fires. 0 and 1 both mean "fire on the first".
	ConsecutiveSamples int `json:"consecutiveSamples" form:"consecutiveSamples" query:"consecutiveSamples"`
	// WindowSeconds is the lookback for the conditions that need one: "delta" (change over
	// the window), "rate" (change per minute across it), "stuck" (no change throughout it),
	// "offline" (nothing heard for this long).
	WindowSeconds int `json:"windowSeconds" form:"windowSeconds" query:"windowSeconds"`
	// CooldownSeconds suppresses re-firing. It SURVIVES RESTART via LastTriggeredAt — the
	// bug this port must not re-introduce: mymatasan loaded that field, carried it into the
	// detector, and then never read it, so every restart re-armed every rule and produced an
	// alert storm.
	CooldownSeconds int `json:"cooldownSeconds" form:"cooldownSeconds" query:"cooldownSeconds"`
	// SchedulePolicy limits when the rule is live ("always", or "window" with the fields
	// below). A loading-bay door opening is normal at 09:00 and an intrusion at 03:00.
	SchedulePolicy string `json:"schedulePolicy" form:"schedulePolicy" query:"schedulePolicy"`
	ScheduleStart  string `json:"scheduleStart" form:"scheduleStart" query:"scheduleStart"`
	ScheduleEnd    string `json:"scheduleEnd" form:"scheduleEnd" query:"scheduleEnd"`
	// ScheduleDays is a comma-separated list of weekday numbers (0=Sunday). Empty = every day.
	ScheduleDays string `json:"scheduleDays" form:"scheduleDays" query:"scheduleDays"`
	// Severity is the alert's urgency: info, warning, critical.
	Severity string `json:"severity" form:"severity" query:"severity"`
	// LastTriggeredAt (unix seconds) is what makes the cooldown survive a restart. It is
	// persisted on every fire and seeded back into the evaluator at boot.
	LastTriggeredAt int64 `json:"lastTriggeredAt" form:"lastTriggeredAt" query:"lastTriggeredAt"`
	CreatedBy       int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt       int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy       int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt       int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
