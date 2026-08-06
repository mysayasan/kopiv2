package entities

// NotificationRollup is an hourly, pre-aggregated tally of notification activity,
// keyed by time bucket and event dimensions. It powers the dashboard's baseline
// and anomaly analytics (heatmaps, expected-band overlays, σ/median-MAD baselines)
// without re-scanning the raw notifications table.
//
// The key is deliberately wide — camera, category, severity, rule and object
// label — so per-rule noise baselines and per-label object-mix analysis need no
// schema migration later. RuleId and Label populate only for vision-alert rows
// (0 / "" otherwise). BucketStart is aligned to the UTC hour; callers re-bucket
// into local hour-of-day × day-of-week slots when computing baselines.
type NotificationRollup struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	BucketStart int64  `json:"bucketStart" form:"bucketStart" query:"bucketStart" ukey:"slot"`
	CameraId    int64  `json:"cameraId" form:"cameraId" query:"cameraId" ukey:"slot"`
	// Source splits each bucket by the notification's Source (e.g. "node:<id>" on a
	// control plane), enabling per-node baselines. Rows folded before this column
	// existed carry "" and keep aggregating into every fleet-wide read — per-source
	// bands simply stay in "learning" until enough split history accumulates.
	// Upgraded databases need the ux_notification_rollup_slot index rebuilt (each
	// app ships an idempotent migration dropping the old one; the auto-migrator
	// recreates it with this column included).
	Source string `json:"source" form:"source" query:"source" ukey:"slot"`
	Category    string `json:"category" form:"category" query:"category" ukey:"slot"`
	Severity    string `json:"severity" form:"severity" query:"severity" ukey:"slot"`
	RuleId      int64  `json:"ruleId" form:"ruleId" query:"ruleId" ukey:"slot"`
	Label       string `json:"label" form:"label" query:"label" ukey:"slot"`
	Count       int64  `json:"count" form:"count" query:"count"`
	UpdatedAt   int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
