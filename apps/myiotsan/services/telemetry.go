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

// SeriesPage is one chart's worth of points, and — the part that matters — WHAT they are.
//
// A chart that quietly changes what it is drawing is a chart that lies. Over a wide window the
// points are rollup buckets, not raw samples, and the caller has to be able to say so rather
// than present an hourly summary as if it were the sensor's own trace.
type SeriesPage struct {
	Items []*entities.DeviceReading `json:"items"`
	// Span is "raw", "1m" or "1h": the resolution the points actually carry.
	Span string `json:"span"`
	// Truncated is true when the window held more raw points than could be returned AND no
	// rollups existed to summarize them — so what came back is the most recent slice of the
	// window, not the whole of it.
	Truncated bool `json:"truncated"`
}

// Series returns a device's readings for one key over a window, oldest first.
//
// maxPoints caps what is returned. A month of a busy key can be tens of thousands of rows, and
// a chart 900 pixels wide cannot draw them — sending them all would just make the browser do
// the discarding, slowly.
//
// WHICH END GETS DISCARDED IS THE WHOLE QUESTION, and it used to be answered wrong. Asking the
// database for the first maxPoints rows ASCENDING returns the OLDEST slice of the window: a
// 7-day chart of a busy key drew Monday and Tuesday and then stopped, and a chart that stops
// five days ago is indistinguishable from a device that died five days ago. The most recent
// reading is the one nobody can do without, so it is the one that is never discarded — the
// window is read newest-first and reversed for drawing.
//
// Over the cap it reads the ROLLUPS instead, which is what they exist for, and tops them up with
// the raw tail the rollup worker has not folded yet (it runs on an interval, so the newest
// stretch of any window is always still raw). Only when there are no rollups to fall back on —
// a freshly installed appliance, or a span the worker has not reached — does it return a
// truncated raw window, and then it says so.
func (s *TelemetryService) Series(ctx context.Context, deviceId int64, key string, fromSec, toSec int64, maxPoints int) (SeriesPage, error) {
	if maxPoints <= 0 {
		maxPoints = 2000
	}

	// Ask for one more than the cap: the extra row is how "exactly full" is told apart from
	// "there is more behind this", without a second counting query.
	raw, err := s.rawWindow(ctx, deviceId, key, fromSec, toSec, maxPoints+1)
	if err != nil {
		return SeriesPage{Items: []*entities.DeviceReading{}, Span: "raw"}, err
	}
	if len(raw) <= maxPoints {
		return SeriesPage{Items: raw, Span: "raw"}, nil
	}

	// Over the cap. Pick the coarsest span that still gives the chart something to draw: if a
	// point per minute would itself overflow the cap, the window is long enough to want hours.
	span, width := "1m", int64(60)
	if toSec-fromSec > int64(maxPoints)*60 {
		span, width = "1h", int64(3600)
	}
	if points, ok := s.rollupSeries(ctx, deviceId, key, span, width, fromSec, toSec, maxPoints); ok {
		return SeriesPage{Items: points, Span: span}, nil
	}

	// No rollups cover this window yet. Return the most recent maxPoints raw samples and say
	// plainly that the window was truncated — a short honest chart beats a long wrong one.
	return SeriesPage{Items: raw[len(raw)-maxPoints:], Span: "raw", Truncated: true}, nil
}

// rawWindow reads up to limit raw readings from the END of a window and returns them oldest
// first. Newest-first in SQL, reversed in memory: the cap must bite on the old end.
func (s *TelemetryService) rawWindow(ctx context.Context, deviceId int64, key string, fromSec, toSec int64, limit int) ([]*entities.DeviceReading, error) {
	if limit <= 0 {
		return []*entities.DeviceReading{}, nil
	}
	rows, _, err := s.readings.Get(ctx, "", uint64(limit), 0,
		[]sqldataenums.Filter{
			{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId},
			{FieldName: "Key", Compare: sqldataenums.Equal, Value: key},
			{FieldName: "Ts", Compare: sqldataenums.GreaterThanOrEqualTo, Value: fromSec * 1000},
			{FieldName: "Ts", Compare: sqldataenums.LessThanOrEqualTo, Value: toSec * 1000},
		},
		[]sqldataenums.Sorter{{FieldName: "Ts", Sort: sqldataenums.DESC}})
	if err != nil {
		if isNoResultErr(err) {
			return []*entities.DeviceReading{}, nil
		}
		return nil, err
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows, nil
}

// rollupSeries renders a window from its rollup buckets, plus the raw tail the rollup worker has
// not folded yet. It reports false when no bucket covers the window, so the caller can fall back
// rather than draw an empty chart over a window that has plenty of data in it.
func (s *TelemetryService) rollupSeries(ctx context.Context, deviceId int64, key, span string, width, fromSec, toSec int64, maxPoints int) ([]*entities.DeviceReading, bool) {
	buckets, err := s.Rollups(ctx, deviceId, key, span, fromSec, toSec)
	if err != nil || len(buckets) == 0 {
		return nil, false
	}
	if len(buckets) > maxPoints {
		buckets = buckets[len(buckets)-maxPoints:] // keep the recent end, same rule as raw
	}

	out := make([]*entities.DeviceReading, 0, maxPoints)
	for _, b := range buckets {
		// Last, not the mean: telemetry_key.go's own argument — for a state-like key (a door's
		// position, a mode) an average is nonsense, and for a continuous one the last value in
		// a bucket is a sample the sensor really reported.
		out = append(out, &entities.DeviceReading{
			DeviceId: b.DeviceId, Key: b.Key, Ts: b.Bucket * 1000, Num: b.Last,
		})
	}

	// The rollup worker runs on an interval, so the newest stretch of any window has no bucket
	// yet. Without this the chart would always be missing its most recent hour — which is the
	// part anybody actually looks at.
	tailFrom := buckets[len(buckets)-1].Bucket + width
	if room := maxPoints - len(out); room > 0 && tailFrom <= toSec {
		if tail, err := s.rawWindow(ctx, deviceId, key, tailFrom, toSec, room); err == nil {
			out = append(out, tail...)
		}
	}
	return out, true
}

// Latest returns the most recent reading for each of a device's keys — what the device page
// shows at the top, and the only query on that page that has to be fast.
//
// It takes the key list rather than discovering it, and that is the fix for a real outage. This
// used to read the device's newest 500 rows and fold them down, on the reasoning that the tail
// is small because the deadband keeps it small. The deadband keeps the tail small PER KEY; it
// says nothing about a device whose keys are unequally chatty. One key reporting a power figure
// every second fills 500 rows in eight minutes, and every other key on the device — the door,
// the mode, the battery — falls off the end of the tail and vanishes from the page. Measured on
// a running appliance: after one key wrote 520 rows, a seven-key device showed exactly one.
//
// One indexed seek per declared key is both correct and cheaper than reading 500 rows: the
// (device, key, time) index makes each of them a descent to a leaf, and there are as many of
// them as the profile declares — a bounded, small number that does not depend on traffic.
func (s *TelemetryService) Latest(ctx context.Context, deviceId int64, keys []string) (map[string]*entities.DeviceReading, error) {
	if len(keys) == 0 {
		// A device with no profile declares no keys, and its page still has to show whatever it
		// has actually reported. The tail fold is the only way to discover keys without a
		// DISTINCT the generic repo does not offer — and for an undecodable device there are no
		// rows to crowd it anyway.
		return s.latestByTail(ctx, deviceId)
	}
	latest := map[string]*entities.DeviceReading{}
	for _, key := range keys {
		rows, _, err := s.readings.Get(ctx, "", 1, 0,
			[]sqldataenums.Filter{
				{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId},
				{FieldName: "Key", Compare: sqldataenums.Equal, Value: key},
			},
			[]sqldataenums.Sorter{{FieldName: "Ts", Sort: sqldataenums.DESC}})
		if err != nil {
			if isNoResultErr(err) {
				continue // this key has simply never reported
			}
			return nil, err
		}
		if len(rows) > 0 {
			latest[key] = rows[0]
		}
	}
	return latest, nil
}

func (s *TelemetryService) latestByTail(ctx context.Context, deviceId int64) (map[string]*entities.DeviceReading, error) {
	rows, _, err := s.readings.Get(ctx, "", 500, 0,
		[]sqldataenums.Filter{{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId}},
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
	// Interval is how often the workers run. An hour is right for a running site, and being
	// able to set it is what makes the worker EXERCISABLE — an hourly job that cannot be
	// driven is a job nobody has ever watched do its work, on an install or on a bench.
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

// rollupBatch is how many raw readings one rollup pass will materialise. A pass that hits it
// stops at the last bucket it can prove is whole; the rest waits for the next pass.
const rollupBatch = 20000

// rollupOnce builds buckets for the readings that do not have one yet.
func (s *TelemetryService) rollupOnce(ctx context.Context, span string, width time.Duration) {
	secs := int64(width.Seconds())

	// Only fold COMPLETE buckets. Rolling up the current, still-filling bucket would write a
	// summary of a partial minute, and — because the cursor then steps past it — never correct
	// it: the rest of that minute would be dropped on the floor, permanently.
	//
	// "now minus one width" is NOT a bucket boundary and does not achieve that. At 10:01:30 it
	// cuts at 10:00:30, which is the MIDDLE of the 10:00 bucket: that bucket gets written from
	// the thirty seconds read so far, the cursor advances to 10:01, and the readings between
	// 10:00:30 and 10:00:59 are never folded by anything. The cut has to be the start of the
	// current bucket, which is the newest instant everything before it is provably whole.
	cutoff := time.Now().Unix() / secs * secs

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
		from = last[0].Bucket + secs
	}

	rows, _, err := s.readings.Get(ctx, "", rollupBatch, 0,
		[]sqldataenums.Filter{
			{FieldName: "Ts", Compare: sqldataenums.GreaterThanOrEqualTo, Value: from * 1000},
			{FieldName: "Ts", Compare: sqldataenums.LessThan, Value: cutoff * 1000},
		},
		[]sqldataenums.Sorter{{FieldName: "Ts", Sort: sqldataenums.ASC}})
	if err != nil || len(rows) == 0 {
		return
	}
	// A pass that filled its batch stopped somewhere arbitrary — very likely mid-bucket. Folding
	// that partial bucket has the same consequence as a mid-bucket cutoff, because the cursor
	// would step past it: see dropIncompleteTail.
	rows = dropIncompleteTail(rows, secs, len(rows) >= rollupBatch)
	if len(rows) == 0 {
		return
	}

	type bkey struct {
		device int64
		key    string
		bucket int64
	}
	acc := map[bkey]*entities.ReadingRollup{}
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

// dropIncompleteTail removes the trailing rows of a truncated read that belong to a bucket the
// read may have cut in half.
//
// The rollup cursor advances to the newest bucket written, so a bucket summarized from a partial
// read is never revisited: its count, min and max are wrong forever, and the readings past the
// cut are folded by nothing. Stopping at the last boundary the batch cleared costs one extra pass
// and is always right.
//
// The exception is a single bucket that is bigger than the whole batch. Dropping it would leave
// nothing to fold, the cursor would not advance, and every future pass would read the same rows
// and drop them again — a rollup that silently stops forever. In that case the partial summary is
// the lesser harm, so it is kept.
func dropIncompleteTail(rows []*entities.DeviceReading, secs int64, truncated bool) []*entities.DeviceReading {
	if !truncated || len(rows) == 0 {
		return rows
	}
	lastBucket := rows[len(rows)-1].Ts / 1000 / secs * secs
	cut := len(rows)
	for cut > 0 && rows[cut-1].Ts/1000/secs*secs == lastBucket {
		cut--
	}
	if cut == 0 {
		return rows // one bucket wider than the batch — fold it rather than stall forever
	}
	return rows[:cut]
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
