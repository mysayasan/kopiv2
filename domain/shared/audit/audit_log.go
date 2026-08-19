// Package audit is the suite's append-only trail of security-relevant actions:
// who did what, to what, from where, and whether it worked.
//
// It exists as a shared package because it was already written twice. myidsan and
// myseliasan each grew their own entity, service and API — near-identical, and already
// diverging (only one truncated hostile input, only one had retention, only one recorded
// the user agent). mymatasan, the app that holds the actual video evidence, had none at
// all: deleting a recording recorded no actor and no reason. Three copies of an audit
// trail is three chances for the one that matters in an investigation to be the one that
// was never finished.
//
// The contract every consumer depends on: entries are only ever INSERTED. There is no
// update path and no targeted delete, so the trail cannot be edited from inside the
// product by the same superadmin whose actions it records. Age-based retention
// (retention.go) is the single exception and is deliberately shaped not to weaken that.
package audit

// AuditLog is one immutable record of a security-relevant event.
//
// The name stutters against the package on purpose. The schema bootstrapper derives the
// table name from the STRUCT name alone (strcase.ToSnake(typeOf.Name())), so this type
// must stay AuditLog to keep writing to the existing `audit_log` table in myidsan and
// myseliasan. Renaming it to audit.Log would silently start writing to a new `log` table
// and orphan every entry already recorded.
//
// The shape is myidsan's, which was the superset: it records events for people who are
// not (yet) authenticated, and that turns out to be the harder case that the other apps
// also benefit from.
//
//   - The actor may be anonymous. A failed sign-in has ActorId 0 and ActorEmail set to
//     the identifier that was ATTEMPTED, which is the only useful attribution available
//     and exactly what an investigation needs.
//   - UserAgent is captured. For login, MFA and session events the client matters, and it
//     is the field that distinguishes "the user signed in from a new laptop" from
//     "someone replayed their cookie". myseliasan's copy lacked it; the column is added
//     additively by the auto-migrator on upgrade.
//
// This is deliberately distinct from api_log, which is a per-request HTTP access log
// freely subject to retention deletion and carrying no action semantics — "someone called
// PUT /api/user-credential and got a 200" is not an answer to "who granted that role,
// from where, and when".
type AuditLog struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// Action is the verb.noun of what happened: "login.success", "recording.delete",
	// "node.adopt", "rbac.set_role". Each app defines its own constants; see the Action*
	// blocks in the per-app services/audit.go.
	Action string `json:"action" form:"action" query:"action" idx:"action"`
	// ActorId is the user who performed the action; 0 when the actor was not
	// authenticated (a failed sign-in, a public forgot-password request) or when the
	// event was raised by the server itself (a boot-time marker, a node self-drop).
	ActorId int64 `json:"actorId" form:"actorId" query:"actorId"`
	// ActorEmail is human-readable attribution captured at event time. For a failed
	// sign-in this is the ATTEMPTED identifier, which may not name a real account.
	ActorEmail string `json:"actorEmail" form:"actorEmail" query:"actorEmail" idx:"actor"`
	// ActorRole is the actor's role id at the time of the action, recorded because a
	// later role change must not rewrite what authority the action was taken under.
	ActorRole int64 `json:"actorRole" form:"actorRole" query:"actorRole"`
	// TargetType classifies what was acted on: "user", "app", "role", "session",
	// "node", "camera", "recording", "backup", "self".
	TargetType string `json:"targetType" form:"targetType" query:"targetType"`
	// TargetId identifies the target within its type.
	TargetId string `json:"targetId" form:"targetId" query:"targetId" idx:"target"`
	// Outcome is "success", "denied", or "error".
	Outcome string `json:"outcome" form:"outcome" query:"outcome"`
	// Detail is a short human-readable summary.
	Detail string `json:"detail" form:"detail" query:"detail"`
	// Metadata is an optional JSON blob of structured context (before/after values, the
	// sign-in method, section counts on a restore). NEVER put a credential, token, TOTP
	// secret, passphrase or password hash here — this table is readable by every
	// superadmin and is exported to CSV.
	Metadata string `json:"metadata" form:"metadata" query:"metadata"`
	// ClientIp is the caller's source address. Resolve it through
	// middlewares.ClientIP (trusted-proxy rules) at the call site so it cannot be forged
	// by an untrusted caller.
	ClientIp string `json:"clientIp" form:"clientIp" query:"clientIp"`
	// UserAgent is the caller's client string, length-capped at capture.
	UserAgent string `json:"userAgent" form:"userAgent" query:"userAgent"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt" idx:"created"`
}
