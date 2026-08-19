package services

import (
	sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// The trail itself now lives in domain/shared/audit. myseliasan had grown an
// independent copy of this file and the two had already drifted — only this one truncated
// hostile input, only this one had retention, only this one recorded the user agent — and
// mymatasan, the app holding the actual video evidence, had none at all. What stays here
// is the part that is genuinely myidsan's: the action vocabulary below.
//
// The table is unchanged. The shared entity keeps the struct name AuditLog, which is what
// the schema bootstrapper derives the table name from.

// Action names. Kept as constants rather than inline strings so the set stays greppable
// and the UI filter can offer a closed list — an audit trail whose action names drift is
// one nobody can query.
//
// Convention: "<subject>.<verb>", past-tense-ish, lower_snake for multi-word verbs.
const (
	// Authentication.
	ActionLoginSuccess  = "login.success"
	ActionLoginFailure  = "login.failure"
	ActionLoginLockout  = "login.lockout"
	ActionLogout        = "login.logout"
	ActionMfaChallenge  = "login.mfa_challenge"
	ActionPasswordReset = "password.reset"

	// Second factor.
	ActionMfaEnroll     = "mfa.enroll"
	ActionMfaDisable    = "mfa.disable"
	ActionMfaAdminReset = "mfa.admin_reset"
	ActionMfaRecovery   = "mfa.recovery_used"
	ActionMfaRegenerate = "mfa.recovery_regenerate"

	// Security keys (WebAuthn/FIDO2). Separate actions from the TOTP ones above because
	// they answer different questions in an investigation: "which key was added, and from
	// where" is per-credential, not per-account.
	ActionWebAuthnEnroll = "webauthn.enroll"
	ActionWebAuthnRemove = "webauthn.remove"
	ActionWebAuthnRename = "webauthn.rename"
	// ActionWebAuthnAdminReset is an administrator clearing SOMEONE ELSE's keys.
	ActionWebAuthnAdminReset = "webauthn.admin_reset"
	// ActionWebAuthnClone records an assertion whose signature counter did not advance.
	// The sign-in is still allowed (see services/webauthn.go for why that ambiguity is not
	// treated as proof), so this entry is the only durable trace that it happened.
	ActionWebAuthnClone = "webauthn.clone_warning"

	// Accounts and authorization.
	ActionUserCreate     = "user.create"
	ActionUserUpdate     = "user.update"
	ActionUserDelete     = "user.delete"
	ActionUserRoleChange = "user.role_change"
	ActionPasswordChange = "password.change"
	ActionRoleCreate     = "role.create"
	ActionRoleUpdate     = "role.update"
	ActionRoleDelete     = "role.delete"
	ActionPermissionSet  = "role.permission_set"

	// Relying apps.
	ActionAppCreate       = "app.create"
	ActionAppUpdate       = "app.update"
	ActionAppDelete       = "app.delete"
	ActionAppSecretRotate = "app.secret_rotate"
	ActionAppRedirectSet  = "app.redirect_set"

	// Federation.
	ActionDirectoryUpdate = "directory.update"
	ActionGroupMapChange  = "directory.group_map_change"

	// Step-up re-authentication. A failure here is what an attacker holding only a
	// stolen cookie produces while trying to escalate, so it is recorded separately.
	ActionStepUpSuccess = "stepup.success"
	ActionStepUpFailure = "stepup.failure"

	// Sessions.
	ActionSessionRevoke    = "session.revoke"
	ActionSessionRevokeAll = "session.revoke_all"

	// Disaster recovery. Both are among the most sensitive actions on the server: an
	// export removes the entire identity store from it, a restore rewrites it.
	ActionBackupExport  = "backup.export"
	ActionBackupRestore = "backup.restore"
)

// Sign-in methods recorded in an authentication event's metadata, so "how did they get
// in?" is answerable without correlating against config.
const (
	MethodLocal     = "local"
	MethodDirectory = "ldap"
	MethodKerberos  = "kerberos"
	MethodOIDC      = "oidc"
	MethodSocial    = "social"
	MethodRecovery  = "recovery_code"
)

type (
	// AuditEntry is the caller-facing shape for recording an event.
	AuditEntry = sharedaudit.Entry
	// AuditFilter narrows a listing. Zero values mean "no filter on that field".
	AuditFilter = sharedaudit.Filter
	// IAuditService is the append-only security trail: record and read, never update or
	// targeted-delete. PurgeOlderThan is the one age-based exception; see the shared package.
	IAuditService = sharedaudit.IService
	// PurgeResult reports what one retention run did.
	PurgeResult = sharedaudit.PurgeResult
)

// Outcomes, re-exported so call sites need no second import.
const (
	OutcomeSuccess = sharedaudit.OutcomeSuccess
	OutcomeDenied  = sharedaudit.OutcomeDenied
	OutcomeError   = sharedaudit.OutcomeError
)

// ActionAuditPurge is written into the trail by the retention purge itself, so a trimmed
// trail is distinguishable from one whose history simply starts there.
const ActionAuditPurge = sharedaudit.ActionAuditPurge

// NewAuditService builds the trail over an existing repo. logf receives write-failure
// diagnostics so a silently-failing trail is still visible somewhere (may be nil).
func NewAuditService(repo dbsql.IGenericRepo[sharedaudit.AuditLog], logf func(format string, args ...any)) IAuditService {
	return sharedaudit.NewService(repo, logf)
}

// WithAuditMetrics attaches the recorder and myidsan's series names, so audit write
// failures and retention purges stay observable under the metric names already shipped.
func WithAuditMetrics(svc IAuditService, m telemetry.Metrics) IAuditService {
	return sharedaudit.WithMetrics(svc, m, sharedaudit.MetricNames{
		WriteFailuresTotal:   MetricAuditWriteFailuresTotal,
		RetentionPurgedTotal: MetricAuditRetentionPurgedTotal,
	})
}
