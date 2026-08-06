package services

import (
	"context"
	"strconv"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// rollupCursorKey is the runtime-setting key under which the notification-rollup
// maintainer's watermark (last folded notification id) is persisted. Same key
// mymatasan uses — the value lives in each app's own database, so they never collide.
const rollupCursorKey = "notification.rollup.cursor"

// rollupCursor persists the rollup maintainer's watermark as a single key-value
// runtime-setting row, so the app-neutral notification package stays decoupled
// from this app's settings storage while the cursor survives restarts.
type rollupCursor struct {
	repo dbsql.IGenericRepo[sharedentities.RuntimeSetting]
}

// NewRollupCursor returns a notification.Cursor backed by the shared
// runtime-setting table.
func NewRollupCursor(repo dbsql.IGenericRepo[sharedentities.RuntimeSetting]) notification.Cursor {
	return &rollupCursor{repo: repo}
}

func (c *rollupCursor) Get(ctx context.Context) (int64, error) {
	row, err := c.repo.GetByUnique(ctx, "", "key", rollupCursorKey)
	if err != nil {
		if isNoResultErr(err) {
			return 0, nil
		}
		return 0, err
	}
	if row == nil || row.Value == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(row.Value, 10, 64)
	if err != nil {
		// A corrupt value restarts the rollup from zero rather than wedging; the
		// backfill is idempotent per bucket, so this is a deliberate safety valve,
		// not a hot path.
		return 0, nil
	}
	return id, nil
}

func (c *rollupCursor) Set(ctx context.Context, lastID int64) error {
	value := strconv.FormatInt(lastID, 10)
	now := time.Now().UTC().Unix()
	row, err := c.repo.GetByUnique(ctx, "", "key", rollupCursorKey)
	if err != nil {
		if isNoResultErr(err) {
			_, cerr := c.repo.Create(ctx, "", sharedentities.RuntimeSetting{
				Key:       rollupCursorKey,
				Value:     value,
				CreatedAt: now,
				UpdatedAt: now,
			})
			return cerr
		}
		return err
	}
	row.Value = value
	row.UpdatedAt = now
	_, err = c.repo.UpdateById(ctx, "", *row)
	return err
}
