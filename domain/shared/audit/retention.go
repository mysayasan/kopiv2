package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// ActionAuditPurge is written into the trail by the purge itself, so the boundary is
// self-documenting. Without it a reader cannot distinguish a trail that was trimmed from
// one whose history simply starts there — and that distinction is the whole reason the
// table is append-only in the first place.
const ActionAuditPurge = "audit.retention_purge"

// auditArchiveBatch is how many rows are read and written per round trip. Small enough that
// a multi-million-row first purge does not have to fit in memory, large enough that the
// paging overhead stays irrelevant.
const auditArchiveBatch = 500

// PurgeResult reports what one retention run did.
type PurgeResult struct {
	// Archived is the number of rows written to the archive and then deleted.
	Archived uint64
	// Deleted is the number of rows the database actually removed. It should equal
	// Archived; a mismatch is worth logging because it means something else wrote to the
	// table mid-purge.
	Deleted uint64
	// ArchivePath is the file the rows were written to, empty when nothing was old enough.
	ArchivePath string
	// Cutoff is the timestamp rows had to predate to be purged.
	Cutoff time.Time
}

// PurgeOlderThan archives every entry older than maxRetentionDays into a JSON-lines file
// under archiveDir and only then deletes those rows.
//
// The ordering is the point. Archive fully, fsync, rename into place, and only once the
// archive is durable on disk delete anything; any failure before that returns early having
// deleted nothing, so a full disk or a permissions mistake costs a retention run, never
// history. The set being purged is stable while this runs precisely because the table is
// append-only: every row inserted after the cutoff is computed is stamped with the current
// time, so it can never fall into the window being deleted.
func (s *service) PurgeOlderThan(ctx context.Context, maxRetentionDays int, archiveDir string) (PurgeResult, error) {
	res := PurgeResult{}
	if maxRetentionDays <= 0 {
		return res, fmt.Errorf("audit retention: maxRetentionDays must be greater than zero")
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -maxRetentionDays)
	res.Cutoff = cutoff

	older := []sqldataenums.Filter{
		{FieldName: "CreatedAt", Compare: sqldataenums.LessThan, Value: cutoff.Unix()},
	}
	// Oldest first, so a partial read is a prefix of history rather than a hole in it.
	oldestFirst := []sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}}

	first, total, err := s.repo.Get(ctx, "", auditArchiveBatch, 0, older, oldestFirst)
	if err != nil {
		if IsNotFound(err) {
			return res, nil
		}
		return res, fmt.Errorf("audit retention: reading expired rows: %w", err)
	}
	if len(first) == 0 || total == 0 {
		return res, nil
	}

	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		return res, fmt.Errorf("audit retention: archive directory %q: %w", archiveDir, err)
	}

	// Named for the cutoff and the highest id it contains: unique without a clock read,
	// and it tells a reader what the file holds before they open it.
	tmpPath := filepath.Join(archiveDir, ".audit-archive.partial")
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return res, fmt.Errorf("audit retention: creating archive: %w", err)
	}
	// Best-effort cleanup of the partial file on every path that does not rename it away.
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	enc := json.NewEncoder(tmp)
	var highestId int64
	// scanned counts rows the query returned, which is what the offset must advance by.
	// res.Archived counts rows actually written and would drift from it on a nil row.
	var scanned uint64
	batch := first
	for {
		for _, row := range batch {
			scanned++
			if row == nil {
				continue
			}
			if err := enc.Encode(row); err != nil {
				return res, fmt.Errorf("audit retention: writing archive: %w", err)
			}
			if row.Id > highestId {
				highestId = row.Id
			}
			res.Archived++
		}
		if uint64(len(batch)) < auditArchiveBatch || scanned >= total {
			break
		}
		if err := ctx.Err(); err != nil {
			return res, err
		}
		// Page by offset rather than by id: rows are only ever removed at the very end,
		// so the window cannot shift underneath the paging.
		batch, _, err = s.repo.Get(ctx, "", auditArchiveBatch, scanned, older, oldestFirst)
		if err != nil {
			if IsNotFound(err) {
				break
			}
			return res, fmt.Errorf("audit retention: reading expired rows: %w", err)
		}
		if len(batch) == 0 {
			break
		}
	}

	if res.Archived == 0 {
		return res, nil
	}

	// Durability before deletion. Without the Sync the rows could be gone from the table
	// while the archive is still only in the page cache.
	if err := tmp.Sync(); err != nil {
		return res, fmt.Errorf("audit retention: flushing archive: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return res, fmt.Errorf("audit retention: closing archive: %w", err)
	}
	finalPath := filepath.Join(archiveDir, fmt.Sprintf("audit-through-id%d-%s.jsonl", highestId, cutoff.Format("20060102")))
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return res, fmt.Errorf("audit retention: publishing archive: %w", err)
	}
	res.ArchivePath = finalPath

	deleted, err := s.repo.Delete(ctx, "", older)
	if err != nil {
		// The archive is already durable, so the history survives; the table just did not
		// shrink. Surfacing the error lets the next run retry against the same window.
		return res, fmt.Errorf("audit retention: deleting archived rows (archive kept at %s): %w", finalPath, err)
	}
	res.Deleted = deleted
	if s.metrics != nil && s.names.RetentionPurgedTotal != "" && deleted > 0 {
		s.metrics.Add(s.names.RetentionPurgedTotal, nil, float64(deleted))
	}

	// Record the trim inside the trail it trimmed. This entry is younger than the cutoff,
	// so it survives this run and every later one.
	s.Record(ctx, Entry{
		Action:     ActionAuditPurge,
		TargetType: "audit",
		TargetId:   filepath.Base(finalPath),
		Outcome:    OutcomeSuccess,
		Detail: fmt.Sprintf("archived and removed %d entries older than %s",
			res.Deleted, cutoff.Format(time.RFC3339)),
		Metadata: map[string]any{
			"cutoff":           cutoff.Format(time.RFC3339),
			"maxRetentionDays": maxRetentionDays,
			"archived":         res.Archived,
			"deleted":          res.Deleted,
			"archiveFile":      finalPath,
			"highestId":        highestId,
		},
	})
	return res, nil
}
