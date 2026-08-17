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

// digestLastWeeklyKey guards the weekly digest the same way (its own key: the
// two cadences fire independently, possibly on the same morning).
const digestLastWeeklyKey = "agent.digest.lastWeeklyRun"

// digestScheduleRetry is how long the loop waits after a failed generation
// before retrying (within the same day).
const digestScheduleRetry = 30 * time.Minute

// RunDigestSchedule starts the daily-digest loop. Returns immediately; the loop
// lives until ctx is cancelled. cfg is read each iteration so the enabled flag
// and hour reflect the booted configuration.
// gate, when set, reports whether THIS instance should generate. Pass the
// background-work leader's IsLeader in a deployment that can be replicated; nil means
// "always", which is correct for a single instance.
func RunDigestSchedule(
	ctx context.Context,
	digests *DigestService,
	runtimeSettings dbsql.IGenericRepo[sharedentities.RuntimeSetting],
	cfg func() config.AgentConfigModel,
	gate func() bool,
	logf func(string, ...any),
) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := &digestSchedule{
		digests:  digests,
		settings: runtimeSettings,
		cfg:      cfg,
		gate:     gate,
		logf:     logf,
		now:      time.Now,
	}
	safego.Supervise(ctx, "myseliasan.agent-digest", s.run)
}

type digestSchedule struct {
	digests  *DigestService
	settings dbsql.IGenericRepo[sharedentities.RuntimeSetting]
	cfg      func() config.AgentConfigModel
	gate     func() bool
	logf     func(format string, args ...any)
	now      func() time.Time
}

// shouldRun reports whether this instance is the one to generate the digest.
func (s *digestSchedule) shouldRun() bool {
	return s.gate == nil || s.gate()
}

func (s *digestSchedule) run(ctx context.Context) {
	for ctx.Err() == nil {
		cfg := s.cfg()
		daily := digestEnabled(cfg)
		weekly := weeklyDigestEnabled(cfg)
		if !daily && !weekly {
			// Disabled: idle, re-checking occasionally (a settings change lands
			// after a restart, so this is belt-and-braces, not the main path).
			if !sleepCtx(ctx, time.Hour) {
				return
			}
			continue
		}
		now := s.now()
		hour := digestHour(cfg)

		// The next occurrence of whichever cadence is due first.
		var next time.Time
		var kind string
		if daily {
			next = nextDigestRun(now, hour, s.lastDate(ctx, digestLastRunKey))
			kind = "daily"
		}
		if weekly {
			wn := nextWeeklyRun(now, hour, digestWeekday(cfg), s.lastDate(ctx, digestLastWeeklyKey))
			if next.IsZero() || wn.Before(next) {
				next = wn
				kind = "weekly"
			}
		}
		s.logf("agent digest: next %s run at %s", kind, next.Format("2006-01-02 15:04"))
		if !sleepCtx(ctx, next.Sub(now)) {
			return
		}

		now = s.now()
		today := localDate(now)
		guardKey := digestLastRunKey
		if kind == "weekly" {
			guardKey = digestLastWeeklyKey
		}
		// Checked HERE, after the sleep, rather than at the top of the loop: the wait is
		// hours long and leadership can move during it, so the only moment worth asking
		// about is the moment of generating.
		//
		// The date watermark below is not sufficient on its own. It is a read-then-write
		// with no lock, so two instances waking in the same second both read "not run
		// today" and both generate — and a digest is an LLM call and an operator-visible
		// artefact, so duplicates cost real money and real confusion.
		if !s.shouldRun() {
			continue
		}
		if s.lastDate(ctx, guardKey) == today {
			continue // another instance (pre-restart) already ran this one today
		}
		if _, err := s.digests.Generate(ctx, kind, 0); err != nil {
			s.logf("agent digest: %s generation failed: %v — retrying in %s", kind, err, digestScheduleRetry)
			if !sleepCtx(ctx, digestScheduleRetry) {
				return
			}
			continue
		}
		s.setLastDate(ctx, guardKey, today)
	}
}

// nextWeeklyRun computes the next Weekday-at-localHour occurrence, skipping
// today's slot when this week's weekly digest already ran.
func nextWeeklyRun(now time.Time, localHour int, weekday time.Weekday, lastRunDate string) time.Time {
	daysAhead := (int(weekday) - int(now.Weekday()) + 7) % 7
	candidate := time.Date(now.Year(), now.Month(), now.Day(), localHour, 0, 0, 0, now.Location()).
		AddDate(0, 0, daysAhead)
	if !candidate.After(now) || (daysAhead == 0 && lastRunDate == localDate(now)) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
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

// weeklyDigestEnabled defaults OFF — the weekly cadence is opt-in.
func weeklyDigestEnabled(cfg config.AgentConfigModel) bool {
	return cfg.Digest.WeeklyEnabled != nil && *cfg.Digest.WeeklyEnabled
}

func digestWeekday(cfg config.AgentConfigModel) time.Weekday {
	if wd := cfg.Digest.Weekday; wd >= 0 && wd <= 6 {
		return time.Weekday(wd)
	}
	return time.Monday
}

func digestHour(cfg config.AgentConfigModel) int {
	if cfg.Digest.LocalHour != nil && *cfg.Digest.LocalHour >= 0 && *cfg.Digest.LocalHour <= 23 {
		return *cfg.Digest.LocalHour
	}
	return 7 // unset: a morning digest, not a midnight one
}

func (s *digestSchedule) lastDate(ctx context.Context, key string) string {
	if s.settings == nil {
		return ""
	}
	row, err := s.settings.GetByUnique(ctx, "", "key", key)
	if err != nil || row == nil {
		return ""
	}
	return row.Value
}

func (s *digestSchedule) setLastDate(ctx context.Context, key, date string) {
	if s.settings == nil {
		return
	}
	now := time.Now().UTC().Unix()
	row, err := s.settings.GetByUnique(ctx, "", "key", key)
	if err != nil || row == nil {
		_, cerr := s.settings.Create(ctx, "", sharedentities.RuntimeSetting{
			Key: key, Value: date, CreatedAt: now, UpdatedAt: now,
		})
		if cerr != nil {
			s.logf("agent digest: persist last-run (%s): %v", key, cerr)
		}
		return
	}
	row.Value = date
	row.UpdatedAt = now
	if _, err := s.settings.UpdateById(ctx, "", *row); err != nil {
		s.logf("agent digest: persist last-run (%s): %v", key, err)
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
