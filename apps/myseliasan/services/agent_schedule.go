package services

import (
	"context"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/infra/config"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// The daily digest scheduler. "Every day at HH:00 local" cannot be expressed as
// a fixed ticker (deps.Scheduler runs immediate+interval), so this is a
// dedicated loop: sleep until the next occurrence, fire once, repeat.
//
// The double-fire guard is a persisted local DATE, not a timestamp: a restart at
// 07:00:30 must not re-run a digest the pre-restart process generated at
// 07:00:05, and "did today's digest already run?" is exactly a date comparison
// in the server's local zone. DST is handled by computing the next occurrence
// with time.Date in the local zone rather than adding 24h.
const digestLastRunKey = "agent.digest.lastRun"

// digestScheduleRetry is how long the loop waits after a failed generation
// before retrying (within the same day).
const digestScheduleRetry = 30 * time.Minute

// RunDigestSchedule starts the daily-digest loop. Returns immediately; the loop
// lives until ctx is cancelled. cfg is read each iteration so the enabled flag
// and hour reflect the booted configuration.
func RunDigestSchedule(
	ctx context.Context,
	digests *DigestService,
	runtimeSettings dbsql.IGenericRepo[sharedentities.RuntimeSetting],
	cfg func() config.AgentConfigModel,
	logf func(string, ...any),
) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := &digestSchedule{
		digests:  digests,
		settings: runtimeSettings,
		cfg:      cfg,
		logf:     logf,
		now:      time.Now,
	}
	safego.Supervise(ctx, "myseliasan.agent-digest", s.run)
}

type digestSchedule struct {
	digests  *DigestService
	settings dbsql.IGenericRepo[sharedentities.RuntimeSetting]
	cfg      func() config.AgentConfigModel
	logf     func(format string, args ...any)
	now      func() time.Time
}

func (s *digestSchedule) run(ctx context.Context) {
	for ctx.Err() == nil {
		cfg := s.cfg()
		if !digestEnabled(cfg) {
			// Disabled: idle, re-checking occasionally (a settings change lands
			// after a restart, so this is belt-and-braces, not the main path).
			if !sleepCtx(ctx, time.Hour) {
				return
			}
			continue
		}
		now := s.now()
		next := nextDigestRun(now, digestHour(cfg), s.lastRunDate(ctx))
		wait := next.Sub(now)
		s.logf("agent digest: next daily run at %s", next.Format("2006-01-02 15:04"))
		if !sleepCtx(ctx, wait) {
			return
		}

		now = s.now()
		today := localDate(now)
		if s.lastRunDate(ctx) == today {
			continue // another instance (pre-restart) already ran today's digest
		}
		if _, err := s.digests.Generate(ctx, "daily", 0); err != nil {
			s.logf("agent digest: daily generation failed: %v — retrying in %s", err, digestScheduleRetry)
			if !sleepCtx(ctx, digestScheduleRetry) {
				return
			}
			continue
		}
		s.setLastRunDate(ctx, today)
	}
}

// nextDigestRun computes when the next daily digest should fire: today at
// localHour if that is still ahead AND today's has not run, else tomorrow at
// localHour. Local-zone time.Date arithmetic keeps DST days correct (a 23- or
// 25-hour day still fires at HH:00 local).
func nextDigestRun(now time.Time, localHour int, lastRunDate string) time.Time {
	todayRun := time.Date(now.Year(), now.Month(), now.Day(), localHour, 0, 0, 0, now.Location())
	if now.Before(todayRun) && lastRunDate != localDate(now) {
		return todayRun
	}
	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), localHour, 0, 0, 0, now.Location())
}

func localDate(t time.Time) string { return t.Format("2006-01-02") }

func digestEnabled(cfg config.AgentConfigModel) bool {
	return cfg.Digest.Enabled == nil || *cfg.Digest.Enabled
}

func digestHour(cfg config.AgentConfigModel) int {
	if cfg.Digest.LocalHour != nil && *cfg.Digest.LocalHour >= 0 && *cfg.Digest.LocalHour <= 23 {
		return *cfg.Digest.LocalHour
	}
	return 7 // unset: a morning digest, not a midnight one
}

func (s *digestSchedule) lastRunDate(ctx context.Context) string {
	if s.settings == nil {
		return ""
	}
	row, err := s.settings.GetByUnique(ctx, "", "key", digestLastRunKey)
	if err != nil || row == nil {
		return ""
	}
	return row.Value
}

func (s *digestSchedule) setLastRunDate(ctx context.Context, date string) {
	if s.settings == nil {
		return
	}
	now := time.Now().UTC().Unix()
	row, err := s.settings.GetByUnique(ctx, "", "key", digestLastRunKey)
	if err != nil || row == nil {
		_, cerr := s.settings.Create(ctx, "", sharedentities.RuntimeSetting{
			Key: digestLastRunKey, Value: date, CreatedAt: now, UpdatedAt: now,
		})
		if cerr != nil {
			s.logf("agent digest: persist last-run: %v", cerr)
		}
		return
	}
	row.Value = date
	row.UpdatedAt = now
	if _, err := s.settings.UpdateById(ctx, "", *row); err != nil {
		s.logf("agent digest: persist last-run: %v", err)
	}
}

// sleepCtx sleeps for d, returning false if ctx was cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d < 0 {
		d = 0
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
