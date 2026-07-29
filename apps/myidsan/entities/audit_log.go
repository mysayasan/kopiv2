package entities

// AuditLog is one immutable record of a security-relevant event on the identity server:
// a sign-in (or a failed one), a lockout, a second-factor enrolment or admin reset, a role
// assignment, an account or SSO-client change, a password reset, a session revocation, or
// a backup export/restore.
//
// The service only ever INSERTS: there is no update, delete or retention-cleanup path, so
// the trail is append-only. That is deliberately different from api_log, which is a
// per-request HTTP access log subject to retention deletion and carries no action
// semantics — "someone called PUT /api/user-credential and got a 200" is not an answer to
// "who granted that role, from where, and when".
//
// Two fields differ from myseliasan's equivalent, because an identity server records
// events for people who are not (yet) authenticated:
//
//   - the actor may be anonymous. A failed sign-in has ActorId 0 and ActorEmail set to the
//     identifier that was ATTEMPTED, which is the only useful attribution available and is
//     exactly what an investigation needs.
//   - UserAgent is captured. For login, MFA and session events the client matters, and it
//     is the field that distinguishes "the user signed in from a new laptop" from "someone
//     replayed their cookie".
type AuditLog struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// Action is the verb.noun of what happened: "login.success", "login.failure",
	// "login.lockout", "mfa.enroll", "mfa.admin_reset", "user.role_change",
	// "session.revoke", "backup.export", ... See the Action* constants in services/audit.go.
	Action string `json:"action" form:"action" query:"action" idx:"action"`
	// ActorId is the user who performed the action; 0 when the actor was not
	// authenticated (a failed sign-in, a public forgot-password request) or when the
	// event was raised by the server itself (a boot-time RESET_MFA marker).
	ActorId int64 `json:"actorId" form:"actorId" query:"actorId"`
	// ActorEmail is human-readable attribution captured at event time. For a failed
	// sign-in this is the ATTEMPTED identifier, which may not name a real account.
	ActorEmail string `json:"actorEmail" form:"actorEmail" query:"actorEmail" idx:"actor"`
	// ActorRole is the actor's role id at the time of the action, recorded because a
	// later role change must not rewrite what authority the action was taken under.
	ActorRole int64 `json:"actorRole" form:"actorRole" query:"actorRole"`
	// TargetType classifies what was acted on: "user", "app", "role", "session",
	// "directory", "backup", "self".
	TargetType string `json:"targetType" form:"targetType" query:"targetType"`
	// TargetId identifies the target within its type.
	TargetId string `json:"targetId" form:"targetId" query:"targetId" idx:"target"`
	// Outcome is "success", "denied", or "error".
	Outcome string `json:"outcome" form:"outcome" query:"outcome"`
	// Detail is a short human-readable summary.
	Detail string `json:"detail" form:"detail" query:"detail"`
	// Metadata is an optional JSON blob of structured context (before/after values, the
	// sign-in method, section counts on a restore). NEVER put a credential, token, TOTP
	// secret or password hash here — this table is readable by every superadmin and is
	// exported to CSV.
	Metadata string `json:"metadata" form:"metadata" query:"metadata"`
	// ClientIp is the caller's source address, resolved through the trusted-proxy rules
	// in middlewares.ClientIP so it cannot be forged by an untrusted caller.
	ClientIp string `json:"clientIp" form:"clientIp" query:"clientIp"`
	// UserAgent is the caller's client string, length-capped at capture.
	UserAgent string `json:"userAgent" form:"userAgent" query:"userAgent"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt" idx:"created"`
}
