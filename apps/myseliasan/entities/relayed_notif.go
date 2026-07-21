package entities

// RelayedNotif is a dedup marker for a node-originated notification the control plane has
// already ingested, keyed by the node's stable engine id (DedupKey = "<nodeId>|<originId>").
// It lets the control plane REPLAY a node's notifications it missed while the control channel
// was disconnected without re-inserting ones it already holds — whether they arrived on the
// live push or an earlier replay. Rows are pruned to the replay window; this is a short-lived
// dedup ledger, not a durable record.
type RelayedNotif struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// DedupKey is "<nodeId>|<originId>" — unique, so the same node event is recorded once.
	DedupKey string `json:"dedupKey" form:"dedupKey" query:"dedupKey" ukey:"dedup_key" validate:"required"`
	NodeId   string `json:"nodeId" form:"nodeId" query:"nodeId"`
	// CreatedAt is the origin notification's unix time, so pruning can drop markers that fall
	// outside the replay window (they can never be re-offered by a bounded pull).
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
}
