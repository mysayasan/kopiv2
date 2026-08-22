package entities

// FleetRollout is one staged upgrade of the fleet to a specific version.
//
// The unit of a rollout is the RING, not the node: nodes are updated a few at a time, and
// each ring must prove itself before the next one starts. That is the whole point — an
// upgrade that breaks recording is going to break it everywhere at once otherwise, and the
// operator finds out from a customer rather than from the first three appliances.
//
// A rollout is a PLAN plus its progress. The plan (target version, ring composition) is
// fixed when it is created; everything else on this row is the driver reporting what has
// happened, so a rollout interrupted by a control-plane restart resumes from the database
// rather than from memory.
type FleetRollout struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// TargetVersion is the exact app version every node in this rollout is asked to install
	// ("1.128.0", stored without the leading v). Never "latest": a ring is only meaningful
	// if every node in it installs the same thing, and "latest" is a moving target that can
	// change between the first ring and the last.
	TargetVersion string `json:"targetVersion" form:"targetVersion" query:"targetVersion" validate:"required"`
	// NodeKind restricts the rollout to one kind of appliance. Only camera nodes (mymatasan)
	// have the self-update primitive this drives, so this is "camera" today; it is stored
	// rather than assumed so the row still says what it meant when another kind gains one.
	NodeKind string `json:"nodeKind" form:"nodeKind" query:"nodeKind"`
	// State is one of the RolloutState* constants.
	State string `json:"state" form:"state" query:"state" idx:"state"`
	// RingSize is how many nodes are updated together. 1 is a true canary.
	RingSize int `json:"ringSize" form:"ringSize" query:"ringSize"`
	// CurrentRing is the ring being worked (1-based; 0 before the rollout starts).
	CurrentRing int `json:"currentRing" form:"currentRing" query:"currentRing"`
	// RingCount is how many rings the plan has, fixed at creation.
	RingCount int `json:"ringCount" form:"ringCount" query:"ringCount"`
	// SettleSeconds is how long a ring's nodes must hold online, on the target version,
	// before the next ring is allowed to start. An upgrade that crashes on the second
	// startup, or wedges a minute after boot, looks exactly like a success without it.
	SettleSeconds int `json:"settleSeconds" form:"settleSeconds" query:"settleSeconds"`
	// NodeTimeoutSeconds bounds how long one node may take to come back on the target
	// version before it is called failed.
	NodeTimeoutSeconds int `json:"nodeTimeoutSeconds" form:"nodeTimeoutSeconds" query:"nodeTimeoutSeconds"`
	// HaltReason says why a halted rollout stopped, in words an operator can act on. It is
	// the field that makes a halt useful rather than merely safe.
	HaltReason string `json:"haltReason" form:"haltReason" query:"haltReason"`
	// RingReadyAt is when the current ring first had every node reporting the target
	// version. The settle window is measured from here, and it is persisted rather than
	// held in memory so a control-plane restart mid-settle does not restart the clock (or,
	// worse, skip it).
	RingReadyAt int64 `json:"ringReadyAt" form:"ringReadyAt" query:"ringReadyAt"`
	Note        string `json:"note" form:"note" query:"note"`

	CreatedBy  int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt  int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	StartedAt  int64 `json:"startedAt" form:"startedAt" query:"startedAt"`
	FinishedAt int64 `json:"finishedAt" form:"finishedAt" query:"finishedAt"`
	UpdatedAt  int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// RolloutNode is one node's place in a rollout, and what became of it.
//
// FromVersion is captured when the node is asked to update, not when the rollout is planned,
// because a node can be upgraded by hand in between — and a report that says a node went
// from a version it was never on is a report nobody trusts twice.
type RolloutNode struct {
	Id        int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	RolloutId int64 `json:"rolloutId" form:"rolloutId" query:"rolloutId" validate:"required" idx:"rollout"`
	// NodeId is the fleet node id. NodeName is snapshotted so a completed rollout still
	// reads correctly after a node is renamed or released.
	NodeId   string `json:"nodeId" form:"nodeId" query:"nodeId" validate:"required"`
	NodeName string `json:"nodeName" form:"nodeName" query:"nodeName"`
	SiteId   int64  `json:"siteId" form:"siteId" query:"siteId"`
	// Ring is 1-based; ring 1 is the canary.
	Ring int `json:"ring" form:"ring" query:"ring" idx:"rollout"`
	// State is one of the RolloutNodeState* constants.
	State string `json:"state" form:"state" query:"state"`
	// FromVersion is what the node reported it was running when it was asked to update.
	FromVersion string `json:"fromVersion" form:"fromVersion" query:"fromVersion"`
	// Detail carries the failure in words when State is failed or skipped.
	Detail     string `json:"detail" form:"detail" query:"detail"`
	AskedAt    int64  `json:"askedAt" form:"askedAt" query:"askedAt"`
	FinishedAt int64  `json:"finishedAt" form:"finishedAt" query:"finishedAt"`
}
