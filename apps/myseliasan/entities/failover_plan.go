package entities

// FailoverPlan says which spare appliance covers which recorder if that recorder stops
// (W3-7, N+1 failover).
//
// The control plane is the only thing that can arrange this, and that is not an accident of
// architecture — it is the only party that talks to both appliances, knows which of them
// are alive, and is still running when one of them is not. `apis/deployment.go` is right
// that a mymatasan node cannot cluster: it is pinned to its own disks and its own capture
// hardware. Failover is therefore not clustering. It is a rehearsed handover, arranged in
// advance by the one component that outlives the failure.
//
// ONE PLAN PER PROTECTED APPLIANCE, enforced by the unique key. Two plans naming the same
// recorder would mean two spares racing to take it over on the same outage — three copies
// of every camera stream and two sets of footage nobody can reconcile afterwards. A spare
// may cover SEVERAL recorders (that is what the "+1" in N+1 means), so the uniqueness is on
// the protected side only.
type FailoverPlan struct {
	Id   int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name string `json:"name" form:"name" query:"name"`
	// ProtectedNodeId is the recorder being covered; StandbyNodeId is the spare that
	// covers it. Both are ManagedNode.NodeId values.
	ProtectedNodeId string `json:"protectedNodeId" form:"protectedNodeId" query:"protectedNodeId" ukey:"failover_protected" validate:"required"`
	StandbyNodeId   string `json:"standbyNodeId" form:"standbyNodeId" query:"standbyNodeId" validate:"required"`
	// Enabled parks a plan without deleting it: it stops being staged, drilled or acted
	// on. Useful while a spare is being serviced — and better than deleting, because
	// deleting loses the record that this recorder was ever meant to be covered.
	Enabled bool `json:"enabled" form:"enabled" query:"enabled"`
	// AutoActivate decides whether the control plane takes the cameras over BY ITSELF once
	// the recorder has been unreachable for the hold-down, or only says it is ready to.
	//
	// DEFAULT FALSE, and the reasoning is the same as the fleet policy's enforce flag but
	// with more at stake. An automatic takeover is correct exactly when the recorder is
	// really dead, and wrong when the control plane merely cannot see it: a site behind a
	// flapping link would have its cameras taken over, handed back, and taken over again,
	// each cycle starting a second stream on every camera. Report-and-wait is already most
	// of the value — the operator learns within a minute, from a screen and a notification,
	// and presses one button. Somebody who has watched their own drills pass can turn this
	// on deliberately.
	AutoActivate bool `json:"autoActivate" form:"autoActivate" query:"autoActivate"`
	// HoldDownSeconds is how long the recorder must have been out of contact before this
	// plan will act, counted from the last time the fleet heard from it.
	//
	// It is measured from LAST CONTACT rather than from the moment the node was declared
	// lost, so it subsumes the liveness grace window (three heartbeats, at least 90s)
	// rather than stacking on top of it — one clock instead of two, and the number an
	// operator types is the number of seconds of silence they are willing to accept. It
	// therefore has to be longer than that window to mean anything at all, which the
	// service enforces.
	//
	// The wait exists because a reboot after an update looks exactly like a dead appliance
	// for about a minute. Taking a site's cameras over for that minute is pure cost: two
	// streams per camera, a fragment of footage in the wrong place, and somebody paged for
	// an appliance that was already coming back. 0 = the service default.
	HoldDownSeconds int `json:"holdDownSeconds" form:"holdDownSeconds" query:"holdDownSeconds"`

	// State is one of the FailoverState* values.
	State string `json:"state" form:"state" query:"state"`

	// What the last staging pass achieved. The ERROR is stored, not just logged: a plan
	// that has been failing to stage for three weeks is invisible everywhere else — the
	// screen would show a plan, the fleet would show two healthy nodes, and the spare
	// would be holding a camera list from before the site was extended.
	LastStagedAt   int64  `json:"lastStagedAt" form:"lastStagedAt" query:"lastStagedAt"`
	LastStageError string `json:"lastStageError" form:"lastStageError" query:"lastStageError"`
	CameraCount    int    `json:"cameraCount" form:"cameraCount" query:"cameraCount"`

	// What the last drill proved. Readiness is a StandbyReadiness* value mirrored from the
	// spare's own answer; it is NOT computed here, because the spare is the only thing that
	// actually tried to open the cameras.
	// What the spare said about its own CAPACITY the last time it was asked (W3-7's stated
	// gap, closed): a drill proves the spare can REACH the cameras, which is a different
	// question from whether it could encode them all at once. Stored rather than read on
	// every page load, because asking is a tunneled round trip and a fleet screen must not
	// be as slow as its slowest appliance.
	//
	// StandbyMax is the spare's own estimate of how many cameras it can carry; StandbyOwn is
	// how many it already has of its own. Both come from the appliance, never from a guess
	// made here.
	StandbyMax        int    `json:"standbyMax" form:"standbyMax" query:"standbyMax"`
	StandbyOwn        int    `json:"standbyOwn" form:"standbyOwn" query:"standbyOwn"`
	CapacityState     string `json:"capacityState" form:"capacityState" query:"capacityState"`
	CapacityCheckedAt int64  `json:"capacityCheckedAt" form:"capacityCheckedAt" query:"capacityCheckedAt"`

	LastDrillAt    int64  `json:"lastDrillAt" form:"lastDrillAt" query:"lastDrillAt"`
	DrillReadiness string `json:"drillReadiness" form:"drillReadiness" query:"drillReadiness"`
	DrillReachable int    `json:"drillReachable" form:"drillReachable" query:"drillReachable"`
	DrillTotal     int    `json:"drillTotal" form:"drillTotal" query:"drillTotal"`

	ActivatedAt int64 `json:"activatedAt" form:"activatedAt" query:"activatedAt"`
	// ActivatedAutomatically distinguishes a takeover nobody chose from one somebody did.
	// It is the first thing asked about an unexpected handover.
	ActivatedAutomatically bool  `json:"activatedAutomatically" form:"activatedAutomatically" query:"activatedAutomatically"`
	ReleasedAt             int64 `json:"releasedAt" form:"releasedAt" query:"releasedAt"`

	// NotifiedLostAt / NotifiedBackAt make the two operator notifications EDGE-TRIGGERED.
	// The sweep runs every half minute; without these, a recorder that stays down over a
	// weekend produces a notification every thirty seconds, and the feed that was supposed
	// to carry the alarm is the reason nobody sees it.
	NotifiedLostAt int64 `json:"notifiedLostAt" form:"notifiedLostAt" query:"notifiedLostAt"`
	NotifiedBackAt int64 `json:"notifiedBackAt" form:"notifiedBackAt" query:"notifiedBackAt"`

	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// Plan states. They describe what the SPARE is doing, not what the protected node is:
// node liveness has its own vocabulary and duplicating it here would give the fleet two
// answers to "is this recorder up".
const (
	// FailoverStatePending means nothing has been staged yet — a plan that has just been
	// written, or one whose staging has never succeeded. It is deliberately its own state
	// rather than a variety of "ready": a plan that has never staged protects nothing.
	FailoverStatePending = "pending"
	// FailoverStateStaged means the spare holds a current copy of the camera set.
	FailoverStateStaged = "staged"
	// FailoverStateActive means the spare has taken the cameras over and is recording them.
	FailoverStateActive = "active"
	// FailoverStateReleased means it took over and has since handed back.
	FailoverStateReleased = "released"
)
