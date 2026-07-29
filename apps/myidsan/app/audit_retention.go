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
