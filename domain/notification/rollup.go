package notification

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// rollupBucketSeconds is the fixed hourly grain of the rollup table. Baselines
// re-bucket these hourly rows into hour-of-day × day-of-week slots in the
// viewer's timezone, so the stored grain stays UTC and timezone-neutral.
const rollupBucketSeconds = 3600

// Cursor persists the id of the last notification folded into the rollup, so the
// maintainer resumes incrementally (and rebuilds from zero on a fresh install or
// after the rollup is cleared). It is injected so this app-neutral package does
// not depend on any particular app's settings storage.
type Cursor interface {
	Get(ctx context.Context) (int64, error)
	Set(ctx context.Context, lastID int64) error
}

// RollupMaintainer incrementally aggregates the raw notifications table into the
// hourly notification_rollup table. It sweeps on an interval, folding every
// notification with an id past the cursor into its (hour, camera, category,
// severity, rule, label) bucket. Paging by ascending id makes the sweep
// exactly-once and offset-free; the first sweep on an existing database backfills
// all history. A single goroutine owns the sweep, so a bucket's read-modify-write
// needs no cross-process locking on this single-node app.
type RollupMaintainer struct {
	notifs   dbsql.IGenericRepo[entities.Notification]
	rollups  dbsql.IGenericRepo[entities.NotificationRollup]
	cursor   Cursor
	interval time.Duration
	pageSize uint64
}

// NewRollupMaintainer builds a maintainer. interval defaults to 60s and pageSize
// to 5000 when non-positive.
func NewRollupMaintainer(
	notifs dbsql.IGenericRepo[entities.Notification],
	rollups dbsql.IGenericRepo[entities.NotificationRollup],
	cursor Cursor,
	interval time.Duration,
	pageSize uint64,
) *RollupMaintainer {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	if pageSize == 0 {
		pageSize = 5000
	}
	return &RollupMaintainer{notifs: notifs, rollups: rollups, cursor: cursor, interval: interval, pageSize: pageSize}
}

// Start runs the sweep loop until ctx is cancelled. The first tick fires a few
// seconds after start so a fresh process backfills without waiting a full
// interval.
func (m *RollupMaintainer) Start(ctx context.Context) { go m.run(ctx) }

func (m *RollupMaintainer) run(ctx context.Context) {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_, _ = m.Sweep(ctx)
			timer.Reset(m.interval)
		}
	}
}

// rollupKey is the composite bucket identity a notification folds into.
type rollupKey struct {
	bucketStart int64
	cameraID    int64
	ruleID      int64
	category    string
	severity    string
	label       string
	source      string
}

// Sweep folds every notification past the cursor into the rollup table, advancing
// and persisting the cursor after each page so a crash mid-backfill resumes where
// it stopped. It returns the number of notifications processed.
func (m *RollupMaintainer) Sweep(ctx context.Context) (int, error) {
	lastID, err := m.cursor.Get(ctx)
	if err != nil {
		return 0, err
	}
	sorters := []sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}}
	processed := 0
	for {
		filters := []sqldataenums.Filter{
			{FieldName: "Id", Compare: sqldataenums.GreaterThan, Value: lastID},
		}
		page, _, err := m.notifs.Get(ctx, "", m.pageSize, 0, filters, sorters)
		if err != nil {
			return processed, err
		}
		if len(page) == 0 {
			return processed, nil
		}
		deltas := map[rollupKey]int64{}
		maxID := lastID
		for _, n := range page {
			deltas[rollupKeyFor(n)]++
			if n.Id > maxID {
				maxID = n.Id
			}
		}
		if err := m.flush(ctx, deltas); err != nil {
			return processed, err
		}
		lastID = maxID
		if err := m.cursor.Set(ctx, lastID); err != nil {
			return processed, err
		}
		processed += len(page)
		// NOTE: the generic repo's Select hard-caps every query at 100 rows
		// regardless of the requested limit, so a full page is not a reliable
		// "more remains" signal and a short page is not a reliable "drained"
		// signal. Termination is driven solely by an empty page at the top of the
		// loop; the cursor advances by max-id each iteration so successive pages
		// march forward until none remain — draining the whole backlog in one sweep.
	}
}

// flush applies a batch of per-bucket deltas to the rollup table: each existing
// bucket row has its count incremented, and missing buckets are created.
func (m *RollupMaintainer) flush(ctx context.Context, deltas map[rollupKey]int64) error {
	now := time.Now().UTC().Unix()
	for key, delta := range deltas {
		if delta == 0 {
			continue
		}
		filters := []sqldataenums.Filter{
			{FieldName: "BucketStart", Compare: sqldataenums.Equal, Value: key.bucketStart},
			{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: key.cameraID},
			{FieldName: "Category", Compare: sqldataenums.Equal, Value: key.category},
			{FieldName: "Severity", Compare: sqldataenums.Equal, Value: key.severity},
			{FieldName: "RuleId", Compare: sqldataenums.Equal, Value: key.ruleID},
			{FieldName: "Label", Compare: sqldataenums.Equal, Value: key.label},
			{FieldName: "Source", Compare: sqldataenums.Equal, Value: key.source},
		}
		existing, _, err := m.rollups.Get(ctx, "", 1, 0, filters, nil)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			row := existing[0]
			row.Count += delta
			row.UpdatedAt = now
			if _, err := m.rollups.UpdateById(ctx, "", *row); err != nil {
				return err
			}
			continue
		}
		if _, err := m.rollups.Create(ctx, "", entities.NotificationRollup{
			BucketStart: key.bucketStart,
			CameraId:    key.cameraID,
			Category:    key.category,
			Severity:    key.severity,
			RuleId:      key.ruleID,
			Label:       key.label,
			Source:      key.source,
			Count:       delta,
			UpdatedAt:   now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// rollupKeyFor derives the composite bucket a notification belongs to, aligning
// its timestamp to the UTC hour and pulling the rule id and object label from the
// vision-alert metadata when present. Category/severity default to "other"/"info"
// so unset values still land in a stable bucket.
func rollupKeyFor(n *entities.Notification) rollupKey {
	cat := strings.TrimSpace(n.Category)
	if cat == "" {
		cat = "other"
	}
	sev := strings.TrimSpace(n.Severity)
	if sev == "" {
		sev = "info"
	}
	return rollupKey{
		bucketStart: n.CreatedAt - n.CreatedAt%rollupBucketSeconds,
		cameraID:    n.CameraId,
		ruleID:      ruleIDFromNotification(n),
		category:    cat,
		severity:    sev,
		label:       labelFromNotification(n),
		source:      strings.TrimSpace(n.Source),
	}
}

// ruleIDFromNotification extracts the detection rule id stored in a vision-alert
// notification's metadata (a JSON number, or a string for some payloads), or 0
// for notifications without one.
func ruleIDFromNotification(n *entities.Notification) int64 {
	if strings.TrimSpace(n.Metadata) == "" {
		return 0
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(n.Metadata), &data); err != nil {
		return 0
	}
	switch v := data["ruleId"].(type) {
	case float64:
		return int64(v)
	case string:
		if id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return id
		}
	}
	return 0
}
