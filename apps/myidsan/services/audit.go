package services

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myidsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

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

	// Outcomes.
	OutcomeSuccess = "success"
	OutcomeDenied  = "denied"
	OutcomeError   = "error"
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

// AuditEntry is the caller-facing shape for recording an event. The service fills in
// CreatedAt, marshals Metadata, and defaults Outcome to "success".
type AuditEntry struct {
	Action     string
	ActorId    int64
	ActorEmail string
	ActorRole  int64
	TargetType string
	TargetId   string
	Outcome    string
	Detail     string
	Metadata   map[string]any
	ClientIp   string
	UserAgent  string
}

// AuditFilter narrows a listing. Zero values mean "no filter on that field".
type AuditFilter struct {
	Action     string
	Outcome    string
	ActorEmail string
	TargetType string
	TargetId   string
	// From/To bound CreatedAt inclusively (unix seconds).
	From int64
	To   int64
}

// IAuditService is the append-only security trail. It exposes recording and reading, and
// deliberately no update and no targeted delete, so the trail cannot be edited from inside
// the product by the same superadmin whose actions it records.
//
// PurgeOlderThan is the single exception and is shaped so as not to weaken that. It cannot
// be reached over the API at all — it runs only from a config-file setting that is off by
// default — it takes an age rather than a selection of rows, it archives to disk before it
// deletes anything, and it records its own run in the trail. Trimming disk usage is
// possible; quietly removing a specific inconvenient event is not.
type IAuditService interface {
	// Record persists one entry. It is best-effort by design: a write failure is logged
	// but never returned, so auditing can never block or fail the action being audited.
	// The alternative — refusing a login because the audit table is full — is worse.
	Record(ctx context.Context, e AuditEntry)
	List(ctx context.Context, limit, offset uint64, f AuditFilter) ([]*entities.AuditLog, uint64, error)
	// PurgeOlderThan archives entries older than maxRetentionDays to a file under
	// archiveDir and then removes them from the table. It deletes nothing unless the
	// archive was written and flushed successfully. See audit_retention.go.
	PurgeOlderThan(ctx context.Context, maxRetentionDays int, archiveDir string) (PurgeResult, error)
}

type auditService struct {
	repo dbsql.IGenericRepo[entities.AuditLog]
	logf func(format string, args ...any)
	// metrics is optional. It exists for one counter, and that counter exists because
	// this service swallows its own write errors on purpose: auditing must never fail
	// the action being audited, which means a broken trail has no symptom at all.
	metrics telemetry.Metrics
}

// WithMetrics attaches a recorder so audit write failures become observable. Optional and
// chainable so existing construction sites are unaffected.
func (s *auditService) WithMetrics(m telemetry.Metrics) IAuditService {
	s.metrics = m
	return s
}

// NewAuditService builds the audit service over its own table. logf receives write-failure
// diagnostics so a silently-failing trail is still visible somewhere (may be nil).
func NewAuditService(repo dbsql.IGenericRepo[entities.AuditLog], logf func(format string, args ...any)) IAuditService {
	return &auditService{repo: repo, logf: logf}
}

// maxDetailLen bounds free-text so a hostile identifier (a 10KB "username" on a failed
// login) cannot bloat the table.
const maxAuditDetailLen = 1024

func (s *auditService) Record(ctx context.Context, e AuditEntry) {
	outcome := strings.TrimSpace(e.Outcome)
	if outcome == "" {
		outcome = OutcomeSuccess
	}
	meta := ""
	if len(e.Metadata) > 0 {
		if b, err := json.Marshal(e.Metadata); err == nil {
			meta = truncate(string(b), maxAuditDetailLen*4)
		}
	}
	row := entities.AuditLog{
		Action:     truncate(e.Action, 128),
		ActorId:    e.ActorId,
		ActorEmail: truncate(e.ActorEmail, 320),
		ActorRole:  e.ActorRole,
		TargetType: truncate(e.TargetType, 64),
		TargetId:   truncate(e.TargetId, 256),
		Outcome:    outcome,
		Detail:     truncate(e.Detail, maxAuditDetailLen),
		Metadata:   meta,
		ClientIp:   truncate(e.ClientIp, 64),
		UserAgent:  truncate(e.UserAgent, 512),
		CreatedAt:  time.Now().Unix(),
	}
	if _, err := s.repo.Create(ctx, "", row); err != nil {
		// Counted before logging: the log line is what an operator reads if they already
		// suspect a problem, the counter is what tells them there IS one.
		if s.metrics != nil {
			s.metrics.Inc(MetricAuditWriteFailuresTotal, nil)
		}
		if s.logf != nil {
			s.logf("audit write failed for %q on %s/%s: %v", e.Action, e.TargetType, e.TargetId, err)
		}
	}
}

func (s *auditService) List(ctx context.Context, limit, offset uint64, f AuditFilter) ([]*entities.AuditLog, uint64, error) {
	if limit == 0 || limit > 1000 {
		limit = 100
	}
	var filters []sqldataenums.Filter
	add := func(field, value string) {
		if v := strings.TrimSpace(value); v != "" {
			filters = append(filters, sqldataenums.Filter{FieldName: field, Compare: sqldataenums.Equal, Value: v})
		}
	}
	add("Action", f.Action)
	add("Outcome", f.Outcome)
	add("ActorEmail", f.ActorEmail)
	add("TargetType", f.TargetType)
	add("TargetId", f.TargetId)
	if f.From > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CreatedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: f.From})
	}
	if f.To > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CreatedAt", Compare: sqldataenums.LessThanOrEqualTo, Value: f.To})
	}

	// Newest first by primary key: CreatedAt has second resolution, so several events in
	// the same second would otherwise come back in an arbitrary order.
	sorters := []sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.DESC}}
	rows, total, err := s.repo.Get(ctx, "", limit, offset, filters, sorters)
	if err != nil {
		if isNotFoundErr(err) {
			return []*entities.AuditLog{}, 0, nil
		}
		return nil, 0, err
	}
	return rows, total, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
