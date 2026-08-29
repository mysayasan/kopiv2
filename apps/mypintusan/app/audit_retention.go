package app

import (
	"context"
	"time"

	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/config"
)

// startAuditRetention schedules the age-based trim of the administrative trail.
//
// OFF unless an operator switches it on in config.json, and that default is the right way round for
// this app. A door controller is a small box with a small disk, so unbounded growth is a real cost —
// but the trail is the only record of who changed who may enter the building, and a controller that
// silently forgot last year's grant edits would be worse than one that filled its disk, because the
// disk announces itself and the missing history does not.
//
// PurgeOlderThan is shaped so that turning it on cannot become a way to remove one inconvenient
// entry: it takes an AGE rather than a selection of rows, it archives to a file and flushes it
// before it deletes anything, and it records its own run inside the trail it trimmed. It is
// reachable from configuration and from nowhere else — there is no API for it, and adding one would
// give the person the trail is about a button that edits it.
//
// The scheduler runs the first tick after one interval rather than at boot, so a restart loop cannot
// become a purge loop, and a failed run is retried on the next tick rather than escalated: a
// retention failure must never take a door controller down.
func startAuditRetention(deps apphost.Dependencies, auditService services.IAuditService) {
	if deps.Config == nil || deps.Scheduler == nil || auditService == nil {
		return
	}
	cfg, clamped := deps.Config.Audit.EffectiveAuditRetention()
	if !cfg.Enabled {
		return
	}
	if clamped && deps.Logger != nil {
		deps.Logger.Warnf("mypintusan.audit",
			"configured maxRetentionDays is below the %d-day floor and was raised to it; a trail shorter than that answers very little",
			config.MinAuditRetentionDays())
	}

	archiveDir := apphost.ResolveWritablePath(deps.DataDir, cfg.ArchiveDir)
	if deps.Logger != nil {
		deps.Logger.Infof("mypintusan.audit",
			"trail retention enabled maxRetentionDays=%d frequencyHours=%d archiveDir=%s",
			cfg.MaxRetentionDays, cfg.FrequencyHours, archiveDir)
	}

	deps.Scheduler.StartPeriodic("mypintusan-audit-retention",
		time.Duration(cfg.FrequencyHours)*time.Hour, func(taskCtx context.Context) error {
			// The leader check is asked per tick rather than once at registration, so a change of
			// leadership takes effect without a restart. This appliance is single-instance by design
			// (the OSDP bus owns its serial port, see apis.NewDeploymentApi), so it always holds the
			// lease — the guard is here because the purge both DELETES rows and writes the archive of
			// what it deleted to a LOCAL disk, and if this app ever does run beside another, two
			// copies racing would leave the trail split across hosts with a gap in each.
			if deps.Leader != nil && !deps.Leader.IsLeader() {
				return nil
			}
			res, err := auditService.PurgeOlderThan(taskCtx, cfg.MaxRetentionDays, archiveDir)
			if err != nil {
				return err
			}
			if res.Deleted > 0 && deps.Logger != nil {
				deps.Logger.Infof("mypintusan.audit",
					"trail retention archived=%d deleted=%d cutoff=%s archive=%s",
					res.Archived, res.Deleted, res.Cutoff.Format(time.RFC3339), res.ArchivePath)
			}
			return nil
		})
}
