package services

import (
	"context"
	"sort"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// TelemetryService reads the history back out: the charts, the latest values, and the rollup
// and retention workers that keep the hot table from growing without bound.
type TelemetryService struct {
	readings dbsql.IGenericRepo[entities.DeviceReading]
	rollups  dbsql.IGenericRepo[entities.ReadingRollup]
	logf     func(format string, args ...any)
}

func NewTelemetryService(db dbsql.IDbCrud, logf func(string, ...any)) *TelemetryService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &TelemetryService{
		readings: dbsql.NewGenericRepo[entities.DeviceReading](db),
		rollups:  dbsql.NewGenericRepo[entities.ReadingRollup](db),
		logf:     logf,
	}
}

// Series returns a device's readings for one key over a window, oldest first.
//
// maxPoints caps what is returned. A month of a busy key can be tens of thousands of rows, and
// a chart 900 pixels wide cannot draw them — sending them all would just make the browser do
// the discarding, slowly. Over the cap it reads the ROLLUPS instead, which is what they exist
// for.
func (s *TelemetryService) Series(ctx context.Context, deviceId int64, key string, fromSec, toSec int64, maxPoints int) ([]*entities.DeviceReading, error) {
	if maxPoints <= 0 {
		maxPoints = 2000
	}
	filters := []sqldataenums.Filter{
		{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId},
		{FieldName: "Key", Compare: sqldataenums.Equal, Value: key},
		{FieldName: "Ts", Compare: sqldataenums.GreaterThanOrEqualTo, Value: fromSec * 1000},
		{FieldName: "Ts", Compare: sqldataenums.LessThanOrEqualTo, Value: toSec * 1000},
	}
	rows, _, err := s.readings.Get(ctx, "", uint64(maxPoints), 0, filters,
		[]sqldataenums.Sorter{{FieldName: "Ts", Sort: sqldataenums.ASC}})
	if err != nil {
		if isNoResultErr(err) {
			return []*entities.DeviceReading{}, nil
		}
		return nil, err
	}
	return rows, nil
}

// Latest returns the most recent reading for each of a device's keys — what the device page
// shows at the top, and the only query on that page that has to be fast.
func (s *TelemetryService) Latest(ctx context.Context, deviceId int64) (map[string]*entities.DeviceReading, error) {
	// Read the recent tail and fold it down. The alternative — a correlated subquery per key —
	// is not expressible through the generic repo, and the tail is small because the deadband
	// keeps it small.
	filters := []sqldataenums.Filter{
		{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId},
	}
	rows, _, err := s.readings.Get(ctx, "", 500, 0, filters,
		[]sqldataenums.Sorter{{FieldName: "Ts", Sort: sqldataenums.DESC}})
	if err != nil {
		if isNoResultErr(err) {
			return map[string]*entities.DeviceReading{}, nil
		}
		return nil, err
	}
	latest := map[string]*entities.DeviceReading{}
	for _, r := range rows {
		if _, seen := latest[r.Key]; !seen {
			latest[r.Key] = r
		}
	}
	return latest, nil
}

// ValueAt returns the reading in effect at a moment — the last one at or before it.
//
// "In effect", not "recorded at": with a deadband, there is usually NO row at the instant the
// rule asks about. A door that opened at 02:14 and has not moved since has no 03:00 row, but it
// is still open at 03:00. Reading the last row at or before the mark is what makes a
// deadbanded series answerable at arbitrary times, and getting this wrong would quietly break
// every delta, rate and stuck rule.
func (s *TelemetryService) ValueAt(ctx context.Context, deviceId int64, key string, atSec int64) (float64, int64, bool) {
	filters := []sqldataenums.Filter{
		{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId},
		{FieldName: "Key", Compare: sqldataenums.Equal, Value: key},
		{FieldName: "Ts", Compare: sqldataenums.LessThanOrEqualTo, Value: atSec * 1000},
	}
	rows, _, err := s.readings.Get(ctx, "", 1, 0, filters,
		[]sqldataenums.Sorter{{FieldName: "Ts", Sort: sqldataenums.DESC}})
	if err != nil || len(rows) == 0 {
		return 0, 0, false
	}
	return rows[0].Num, rows[0].Ts / 1000, true
}

// Rollups reads the downsampled buckets for a long window.
func (s *TelemetryService) Rollups(ctx context.Context, deviceId int64, key, span string, fromSec, toSec int64) ([]*entities.ReadingRollup, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId},
		{FieldName: "Key", Compare: sqldataenums.Equal, Value: key},
		{FieldName: "Span", Compare: sqldataenums.Equal, Value: span},
		{FieldName: "Bucket", Compare: sqldataenums.GreaterThanOrEqualTo, Value: fromSec},
		{FieldName: "Bucket", Compare: sqldataenums.LessThanOrEqualTo, Value: toSec},
	}
	rows, _, err := s.rollups.Get(ctx, "", 5000, 0, filters,
		[]sqldataenums.Sorter{{FieldName: "Bucket", Sort: sqldataenums.ASC}})
	if err != nil && isNoResultErr(err) {
		return []*entities.ReadingRollup{}, nil
	}
	return rows, err
}

// RetentionConfig bounds how long telemetry is kept.
type RetentionConfig struct {
	// RawDays is how long individual readings survive.
	RawDays int
	// RollupDays is how long the downsampled buckets survive. It is LONGER than RawDays on
	// purpose: the rollups are what make "what did this room do last summer" answerable after
	// the raw rows are gone, at a tiny fraction of the size.
	RollupDays int
	// Interval is how often the workers run.
	Interval time.Duration
}

func (c RetentionConfig) withDefaults() RetentionConfig {
	if c.RawDays <= 0 {
		c.RawDays = 30
	}
	if c.RollupDays <= 0 {
		c.RollupDays = 400 // over a year: last summer is comparable to this one
	}
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}
	return c
}

// RunRollup periodically folds raw readings into 1m and 1h buckets.
//
// Rollups are built BEFORE retention purges the rows they summarize (both run on the same
// ticker, rollup first) — the other order would silently throw away the data the rollup was
// supposed to preserve, and nobody would notice until they asked for a chart of last month.
func (s *TelemetryService) RunRollup(ctx context.Context, cfg RetentionConfig) {
	cfg = cfg.withDefaults()
	safego.Supervise(ctx, "myiotsan.rollup", func(ctx context.Context) {
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.rollupOnce(ctx, "1m", time.Minute)
				s.rollupOnce(ctx, "1h", time.Hour)
				s.purge(ctx, cfg)
			}
		}
	})
}

// rollupOnce builds buckets for the readings that do not have one yet.
func (s *TelemetryService) rollupOnce(ctx context.Context, span string, width time.Duration) {
	// Only fold COMPLETE buckets. Rolling up the current, still-filling bucket would write a
	// summary of a partial minute and then have to correct it — so stop one width short.
	cutoff := time.Now().Add(-width).Unix()

	// Where the last rollup of this span got to.
	last, _, err := s.rollups.Get(ctx, "", 1, 0,
		[]sqldataenums.Filter{{FieldName: "Span", Compare: sqldataenums.Equal, Value: span}},
		[]sqldataenums.Sorter{{FieldName: "Bucket", Sort: sqldataenums.DESC}})
	if err != nil && !isNoResultErr(err) {
		s.logf("rollup cursor read failed: %v", err)
		return
	}
	var from int64
	if len(last) > 0 {
		from = last[0].Bucket + int64(width.Seconds())
	}

	rows, _, err := s.readings.Get(ctx, "", 20000, 0,
		[]sqldataenums.Filter{
			{FieldName: "Ts", Compare: sqldataenums.GreaterThanOrEqualTo, Value: from * 1000},
			{FieldName: "Ts", Compare: sqldataenums.LessThan, Value: cutoff * 1000},
		},
		[]sqldataenums.Sorter{{FieldName: "Ts", Sort: sqldataenums.ASC}})
	if err != nil || len(rows) == 0 {
		return
	}

	type bkey struct {
		device int64
		key    string
		bucket int64
	}
	acc := map[bkey]*entities.ReadingRollup{}
	secs := int64(width.Seconds())
	for _, r := range rows {
		b := (r.Ts / 1000) / secs * secs
		k := bkey{device: r.DeviceId, key: r.Key, bucket: b}
		cur, ok := acc[k]
		if !ok {
			acc[k] = &entities.ReadingRollup{
				DeviceId: r.DeviceId, Key: r.Key, Span: span, Bucket: b,
				Count: 1, Min: r.Num, Max: r.Num, Sum: r.Num, Last: r.Num,
			}
			continue
		}
		cur.Count++
		cur.Sum += r.Num
		cur.Last = r.Num // rows are time-ordered, so the last one wins
		if r.Num < cur.Min {
			cur.Min = r.Num
		}
		if r.Num > cur.Max {
			cur.Max = r.Num
		}
	}

	out := make([]entities.ReadingRollup, 0, len(acc))
	for _, v := range acc {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bucket < out[j].Bucket })
	if len(out) == 0 {
		return
	}
	if _, err := s.rollups.CreateMultiple(ctx, "", out); err != nil {
		s.logf("rollup write failed for span %s: %v", span, err)
	}
}

// purge enforces retention. Raw readings go first and rollups much later, so the shape of the
// past survives long after its detail does.
func (s *TelemetryService) purge(ctx context.Context, cfg RetentionConfig) {
	rawCutoff := time.Now().AddDate(0, 0, -cfg.RawDays).UnixMilli()
	if n, err := s.readings.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "Ts", Compare: sqldataenums.LessThan, Value: rawCutoff}}); err != nil {
		if !isNoResultErr(err) {
			s.logf("reading retention purge failed: %v", err)
		}
	} else if n > 0 {
		s.logf("retention purged %d raw readings older than %d days", n, cfg.RawDays)
	}

	rollupCutoff := time.Now().AddDate(0, 0, -cfg.RollupDays).Unix()
	if _, err := s.rollups.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "Bucket", Compare: sqldataenums.LessThan, Value: rollupCutoff}}); err != nil && !isNoResultErr(err) {
		s.logf("rollup retention purge failed: %v", err)
	}
}
