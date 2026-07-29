package services

import "github.com/mysayasan/kopiv2/infra/telemetry"

// myidsan's runtime metrics.
//
// The rule is the suite's: instrument what FAILS SILENTLY. A metric that restates a log
// line someone reads anyway is noise; a metric for a failure with no other symptom is the
// whole point. An identity provider has an unusual number of the second kind, because most
// of its failures are experienced by somebody ELSE:
//
//   - A token exchange that fails is seen by the relying app, which shows its own user its
//     own error. Nothing on the myidsan side raises anything an operator would notice, so
//     "every login to the payroll app has been broken since the secret was rotated" can run
//     for days.
//   - The audit trail is best-effort by design (see IAuditService.Record): a write failure
//     is logged and swallowed, because refusing a login when the audit table is full is
//     worse. The consequence is that the security trail can stop recording while every
//     other symptom looks healthy — which is precisely the situation in which an intact
//     trail matters most.
//   - A directory (LDAP/AD) that has gone slow or unreachable makes logins fail upstream.
//     myidsan itself stays up and green.
//
// Deliberately NOT here: successful-login counts beyond what already exists, request rates,
// or anything the API-duration histogram already covers. Those are visible elsewhere.
const (
	// MetricTokenExchangeTotal counts authorization-code redemptions at
	// POST /api/auth/token, labelled by outcome. Every non-success is a relying app that
	// just failed to sign somebody in.
	//
	// Outcomes are a closed set (see the TokenExchange* constants) so the label stays
	// low-cardinality and an operator can alert on a specific failure rather than "not
	// success". `code_invalid` covers the ordinary races (a user who took too long, a
	// double-submitted callback) and is expected at a low rate; `code_mismatch` and
	// `secret_invalid` are not ordinary — a code presented by a different client, or a
	// wrong secret, is either a misconfiguration or someone probing.
	MetricTokenExchangeTotal = "myidsan_token_exchange_total"

	// MetricAuditWriteFailuresTotal counts audit entries that could not be persisted.
	// Any non-zero value means the security trail has gaps; alert on any increase. This
	// is the metric that exists because the failure it reports is deliberately silent.
	MetricAuditWriteFailuresTotal = "myidsan_audit_write_failures_total"

	// MetricAuditRetentionPurgedTotal counts audit rows removed by age-based retention.
	// Paired with the trail's own `audit.retention_purge` entry so that shrinkage is
	// attributable: rows disappearing WITHOUT this counter moving is not retention.
	MetricAuditRetentionPurgedTotal = "myidsan_audit_retention_purged_total"

	// MetricSessionsActive is the number of live sessions the index knows about. A
	// gauge rather than a counter: the useful signals are the level (capacity, and
	// whether a revocation actually took effect) and a sudden jump.
	MetricSessionsActive = "myidsan_sessions_active"
)

// Token-exchange outcomes. Closed set: adding one means adding it here, which keeps the
// metric's cardinality bounded and its dashboard stable.
const (
	TokenExchangeSuccess       = "success"
	TokenExchangeBadRequest    = "bad_request"
	TokenExchangeBadGrant      = "unsupported_grant"
	TokenExchangeClientUnknown = "client_unknown"
	TokenExchangeSecretInvalid = "secret_invalid"
	TokenExchangeRedirectBad   = "redirect_invalid"
	TokenExchangeCodeInvalid   = "code_invalid"
	TokenExchangeCodeMismatch  = "code_mismatch"
	TokenExchangeServerError   = "server_error"
)

// DescribeMetrics attaches help text. Call once at startup.
//
// The help text is where the "why" lives for whoever finds one of these on a dashboard at
// 3am without the source to hand, so it says what a rising value MEANS rather than
// restating the metric name.
func DescribeMetrics(m telemetry.Metrics) {
	if m == nil {
		return
	}
	m.Describe(MetricTokenExchangeTotal,
		"Authorization-code redemptions by outcome. Every non-success is a relying app that failed to sign a user in; myidsan itself logs nothing an operator would notice, so this is the only signal. secret_invalid or code_mismatch rising means a misconfigured client or someone probing.")
	m.Describe(MetricAuditWriteFailuresTotal,
		"Audit entries that could not be persisted. Auditing is best-effort so it can never block a login, which means the trail can stop recording silently. Any increase means the security trail has gaps — alert on it.")
	m.Describe(MetricAuditRetentionPurgedTotal,
		"Audit rows removed by age-based retention. Makes trail shrinkage attributable: rows vanishing without this moving did not come from retention.")
	m.Describe(MetricSessionsActive,
		"Live sessions in the session index. Watch the level for capacity and to confirm a revocation took effect, and sudden jumps for anomalies.")
}
