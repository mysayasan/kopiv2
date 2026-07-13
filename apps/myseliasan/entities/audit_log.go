package entities

// AuditLog is one immutable record of a sensitive control-plane action — node
// adopt/release, a tunneled node command (e.g. wipe / factory-reset), an RBAC
// role/disable/elevate change, a fleet-key rotation, or a certificate revoke.
//
// The audit service only ever INSERTS rows: there is no update or delete path and
// no retention cleanup, so the trail is append-only and tamper-evident for incident
// review. This is deliberately distinct from api_log, which is a per-request HTTP
// access log subject to retention-based deletion and carries no action semantics.
type AuditLog struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// Action is the verb.noun of what happened, e.g. "node.adopt", "node.release",
	// "node.command", "rbac.set_role", "fleet.key_rotate".
	Action string `json:"action" form:"action" query:"action" idx:"action"`
	// ActorId is the control-plane user who performed the action (0 for a
	// node/system-initiated action such as a self-drop).
	ActorId int64 `json:"actorId" form:"actorId" query:"actorId"`
	// ActorEmail is a human-readable attribution captured at action time (email, else
	// display name, else "node:<id>" for node-initiated events).
	ActorEmail string `json:"actorEmail" form:"actorEmail" query:"actorEmail"`
	// ActorRole is the actor's role id at the time of the action.
	ActorRole int64 `json:"actorRole" form:"actorRole" query:"actorRole"`
	// TargetType classifies what was acted on: "node", "user", "fleet", "node-access".
	TargetType string `json:"targetType" form:"targetType" query:"targetType"`
	// TargetId identifies the target within its type (node id, user id, etc.).
	TargetId string `json:"targetId" form:"targetId" query:"targetId" idx:"target"`
	// Outcome is "success", "denied", or "error".
	Outcome string `json:"outcome" form:"outcome" query:"outcome"`
	// Detail is a short human-readable summary of the action.
	Detail string `json:"detail" form:"detail" query:"detail"`
	// Metadata is an optional JSON blob carrying structured context (before/after
	// values, request path, extra fields) for deeper review.
	Metadata string `json:"metadata" form:"metadata" query:"metadata"`
	// ClientIp is the source address of the operator's request (empty for
	// node-initiated events).
	ClientIp  string `json:"clientIp" form:"clientIp" query:"clientIp"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt" idx:"created"`
}
