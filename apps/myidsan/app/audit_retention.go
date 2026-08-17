package app

import (
	"context"
	"log"
	"time"

	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/config"
)

// startSessionGauge refreshes the active-session gauge on a timer.
//
// Polled rather than incremented at the call sites because sessions also end without
// anyone calling Revoke — a cache entry simply expires — so a counter maintained by hand
// would drift away from the truth and never come back. A periodic read of the index cannot
// drift; it is at worst one interval stale, which is fine for a capacity and anomaly gauge.
//
// Deliberately NOT restricted to the leader, unlike the retention trim below. This only
// READS and publishes a number; every instance is its own Prometheus scrape target, so
// gating it would leave the followers' series stale or absent and make a dashboard read
// as though those instances had no sessions. Exclusivity matters for work that CHANGES
// something, not for observing it.
func startSessionGauge(deps apphost.Dependencies, sessions services.ISessionService) {
	if deps.Scheduler == nil || deps.Metrics == nil || sessions == nil {
		return
	}
	deps.Scheduler.StartPeriodic("myidsan-session-gauge", sessionGaugeInterval, func(taskCtx context.Context) error {
		return services.PublishActiveSessions(taskCtx, sessions, deps.Metrics)
	})
}

// sessionGaugeInterval is a compromise: often enough that a revocation shows up on a
// dashboard while an operator is still looking at it, rare enough that it is one trivial
// COUNT per minute rather than a load source of its own.
const sessionGaugeInterval = time.Minute

// startAuditRetention schedules the age-based trim of the security trail. It is a no-op
// unless an operator has switched it on in config.json, because unbounded growth costs disk
// while missing security history costs an investigation.
//
// The scheduler runs the first tick after one interval rather than at boot, so a restart
// loop cannot turn into a purge loop, and a run that fails is logged and retried on the next
// tick rather than escalated — a retention failure must never take the identity server down.
//
// Restricted to the instance holding the background-work lease. This one both DELETES rows
// and writes the archive of what it deleted to a LOCAL directory, so several instances
// running it would race on the delete and each write a partial archive to its own disk —
// leaving the security trail split across hosts with gaps in each copy, discovered only
// when somebody needed it for an investigation. Standalone, this instance always holds the
// lease, so nothing changes.
func startAuditRetention(deps apphost.Dependencies, auditService services.IAuditService) {
	if deps.Config == nil || deps.Scheduler == nil {
		return
	}
	cfg, clamped := deps.Config.Audit.EffectiveAuditRetention()
	if !cfg.Enabled {
		return
	}
	if clamped {
		log.Printf("audit-retention: configured maxRetentionDays is below the %d-day floor and was raised to it; a security trail shorter than that answers very little",
			config.MinAuditRetentionDays())
	}

	archiveDir := apphost.ResolveWritablePath(deps.DataDir, cfg.ArchiveDir)
	log.Printf("audit-retention: enabled maxRetentionDays=%d frequencyHours=%d archiveDir=%s",
		cfg.MaxRetentionDays, cfg.FrequencyHours, archiveDir)

	deps.Scheduler.StartPeriodic("myidsan-audit-retention", time.Duration(cfg.FrequencyHours)*time.Hour, func(taskCtx context.Context) error {
		// Asked per tick, not once at registration, so a change of leadership takes effect
		// without restarting any instance.
		if !deps.Leader.IsLeader() {
			return nil
		}
		res, err := auditService.PurgeOlderThan(taskCtx, cfg.MaxRetentionDays, archiveDir)
		if err != nil {
			return err
		}
		if res.Deleted > 0 {
			log.Printf("audit-retention: archived=%d deleted=%d cutoff=%s archive=%s",
				res.Archived, res.Deleted, res.Cutoff.Format(time.RFC3339), res.ArchivePath)
		}
		return nil
	})
}
