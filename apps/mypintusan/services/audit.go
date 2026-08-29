package services

import (
	sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// The administrative trail, from domain/shared/audit.
//
// WHY THIS APP NEEDED IT MOST. mypintusan already keeps the best-kept log in the suite:
// entities.AccessEvent records every badge presented at every door — who, where, when, granted or
// denied, and why. What it does not record is WHO DECIDED THEY COULD. A grant created at 23:40, a
// holiday deleted the morning of a shutdown, a door's offline policy flipped from deny to cached,
// a badge quietly issued to a holder nobody added to the roster: every one of those changes what
// the access log will say tomorrow, and none of them left a mark that survives.
//
// The gap was sharper here than the missing-trail gap was in mymatasan, because the access log
// makes it INVISIBLE. A grant edit does not look like an incident; it looks like an ordinary badge
// event three weeks later, on a door the person was never supposed to reach, with `decision:
// granted, reason: ok` next to it. The log answers "did this happen" perfectly and "was this
// supposed to happen" not at all.
//
// WHAT WAS ALREADY THERE, AND WHY IT IS NOT THIS. access_rules.go publishes an
// `access.rule-change` NOTIFICATION on grant, schedule, holiday and membership edits, naming the
// administrator. That is a good alert and a bad record: the notification feed is a bounded, evicted
// stream meant to be read within minutes (the suite has already been bitten once by a diagnostic
// flood evicting the rows that mattered — see the events-panel eviction fix), it covers only the
// access-rules API, and it has no filter, no export and no retention policy of its own. An
// investigation six months later needs a table. Both are kept: the notification is how a change is
// noticed now, the trail is how it is proven later.
//
// The trail is APPEND-ONLY — no update path, no targeted delete, no API that could offer one. Age
// based retention (domain/shared/audit/retention.go) archives to disk before it removes anything
// and records its own run in the trail it trimmed.

type (
	// AuditEntry is the caller-facing shape for recording an action.
	AuditEntry = sharedaudit.Entry
	// AuditFilter narrows a listing. Zero values mean "no filter on that field".
	AuditFilter = sharedaudit.Filter
	// IAuditService is the trail: record and read, never update or targeted-delete.
	IAuditService = sharedaudit.IService
	// PurgeResult reports what one age-based retention run did.
	PurgeResult = sharedaudit.PurgeResult
)

// Outcomes, re-exported so call sites need no second import.
const (
	OutcomeSuccess = sharedaudit.OutcomeSuccess
	OutcomeDenied  = sharedaudit.OutcomeDenied
	OutcomeError   = sharedaudit.OutcomeError
)

// Action names. Constants rather than inline strings so the set stays greppable and a UI filter can
// offer a closed list — an audit trail whose action names drift is one nobody can query.
//
// Convention: "<subject>.<verb>", lower_snake for multi-word verbs.
//
// A DECLARED ACTION THAT NOTHING EMITS IS A LIE THIS SUITE HAS TOLD BEFORE — the same grep found
// five dead audit constants on myidsan and three alarms on this app that could never fire. Every
// constant below has a call site, and apis/audit_actions_test.go fails the build if one loses it.
const (
	// --- who may enter -----------------------------------------------------------------
	//
	// The reason the file exists. Each of these silently rewrites what tomorrow's access log will
	// say, and none of them looks like anything in that log.
	ActionGrantCreate       = "grant.create"
	ActionGrantDelete       = "grant.delete"
	ActionGroupCreate       = "group.create"
	ActionGroupDelete       = "group.delete"
	ActionGroupMemberAdd    = "group.member_add"
	ActionGroupMemberRemove = "group.member_remove"
	ActionScheduleCreate    = "schedule.create"
	ActionScheduleDelete    = "schedule.delete"
	// A holiday is the only rule change that shuts a whole building without anybody touching a
	// grant — the calendar is consulted ABOVE the 24/7 short circuit — and deleting one REOPENS a
	// site that was meant to be shut, which is the incident rather than the embarrassment.
	ActionHolidayCreate = "holiday.create"
	ActionHolidayDelete = "holiday.delete"

	// --- people and what they carry ----------------------------------------------------
	ActionHolderCreate     = "holder.create"
	ActionCredentialIssue  = "credential.issue"
	ActionCredentialRevoke = "credential.revoke"

	// --- the physical estate -----------------------------------------------------------
	//
	// Creating a door fixes its offline policy, its cache TTL and whether its reader must speak
	// Secure Channel — and there is no PUT /api/doors, so those values are decided once and kept.
	// The entry records them, because "why did this door keep opening after we lost the network"
	// is answered by a field nobody looks at twice at install time.
	ActionDoorCreate = "door.create"
	// ActionDoorUnlockRemote duplicates what the access log already holds, on purpose. A reviewer
	// reading the administrative trail is asking "what did the people with power do", and a remote
	// open is squarely that; sending them to a second table for the answer is how half a story gets
	// told. entities.AccessEvent stays the authority on door DECISIONS.
	ActionDoorUnlockRemote = "door.unlock_remote"

	// --- the building's safety posture -------------------------------------------------
	//
	// Lockdown cannot trap anybody (egress is hardware and nothing in this process can override
	// it), but it is the one control that stops a building working, and "who sealed the site at
	// 08:55 on a Monday" must not be a question with no answer.
	ActionLockdownSet = "lockdown.set"

	// --- the appliance itself ----------------------------------------------------------
	ActionSettingsChange = "settings.change"
	ActionSettingsReset  = "settings.reset"
	ActionUserCreate     = "user.create"
	ActionUserUpdate     = "user.update"
	ActionUserDelete     = "user.delete"
	ActionUserPassword   = "user.password_reset"
	// There is deliberately NO auth.password_change constant here. Somebody changing their OWN
	// password is served by shared code this app does not own, and inventing a name for an event no
	// handler in this repository emits is how the suite ended up with five dead audit constants on
	// myidsan. The middleware below records that change as api.write on /api/auth/change-password —
	// thinner, and actually true.

	// ActionApiWrite is the catch-all the audit middleware records when an accepted mutation
	// reached no handler that audited itself. It is not a category of event — it is the guarantee
	// that a route added next year is in the trail on the day it ships rather than on the day
	// somebody notices it is not. See apis.NewAuditMiddleware.
	ActionApiWrite = "api.write"
)

// Target types, so a listing can be narrowed to one kind of thing.
const (
	TargetGrant      = "grant"
	TargetGroup      = "group"
	TargetSchedule   = "schedule"
	TargetHoliday    = "holiday"
	TargetHolder     = "holder"
	TargetCredential = "credential"
	TargetDoor       = "door"
	TargetSite       = "site"
	TargetSettings   = "settings"
	TargetUser       = "user"
	TargetApi        = "api"
)

// NewAuditService builds the trail over mypintusan's database. logf receives write-failure
// diagnostics (may be nil).
func NewAuditService(db dbsql.IDbCrud, logf func(format string, args ...any)) IAuditService {
	return sharedaudit.NewServiceFromDb(db, logf)
}

// WithAuditMetrics attaches the recorder under mypintusan's series names, so a trail that has
// silently stopped recording becomes visible. Record() swallows its own write errors by design —
// auditing must never fail the action being audited, and on this appliance the action being
// audited may be somebody standing at a door — which means a broken trail otherwise has no symptom
// at all.
func WithAuditMetrics(svc IAuditService, m telemetry.Metrics) IAuditService {
	return sharedaudit.WithMetrics(svc, m, sharedaudit.MetricNames{
		WriteFailuresTotal:   MetricAuditWriteFailuresTotal,
		RetentionPurgedTotal: MetricAuditRetentionPurgedTotal,
	})
}
