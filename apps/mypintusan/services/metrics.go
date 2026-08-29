package services

import "github.com/mysayasan/kopiv2/infra/telemetry"

// The metrics this appliance emits beyond the shared API middleware's.
//
// Naming follows the suite: kopiv2_* for app-neutral infra, mypintusan_* for this app's own.
const (
	// MetricAuditWriteFailuresTotal counts administrative-trail entries that could not be
	// persisted. The audit service swallows its own write errors on purpose — auditing must never
	// fail the action being audited, and here that action may be a person standing at a door — so
	// this counter is the ONLY symptom a trail that has stopped recording produces. Everything
	// else stays green while the record of who changed the rules quietly develops a hole.
	MetricAuditWriteFailuresTotal = "mypintusan_audit_write_failures_total"
	// MetricAuditRetentionPurgedTotal counts trail rows removed by age-based retention.
	MetricAuditRetentionPurgedTotal = "mypintusan_audit_retention_purged_total"
)

// DescribeMetrics registers help text so a scrape is readable by somebody who did not write this
// code. Call once at startup.
func DescribeMetrics(m telemetry.Metrics) {
	if m == nil {
		return
	}
	m.Describe(MetricAuditWriteFailuresTotal,
		"Administrative-trail entries that could not be persisted. The audit service swallows its own write errors, so this is the only symptom a trail that stopped recording produces.")
	m.Describe(MetricAuditRetentionPurgedTotal, "Administrative-trail rows removed by age-based retention.")
}
