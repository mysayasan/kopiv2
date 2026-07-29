package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myidsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeAuditRepo is an in-memory IGenericRepo supporting exactly the query the retention
// path issues: a CreatedAt < cutoff filter, ordered by Id, paged by limit/offset.
type fakeAuditRepo struct {
	dbsql.IGenericRepo[entities.AuditLog]
	rows      []*entities.AuditLog
	nextID    int64
	deleteErr error
	// deletes records every Delete call so a test can assert none happened.
	deletes int
}

func (f *fakeAuditRepo) matching(filters []sqldataenums.Filter) []*entities.AuditLog {
	out := make([]*entities.AuditLog, 0, len(f.rows))
	for _, row := range f.rows {
		keep := true
		for _, filter := range filters {
			if filter.FieldName != "CreatedAt" {
				continue
			}
			bound, ok := filter.Value.(int64)
			if !ok {
				continue
			}
			if filter.Compare == sqldataenums.LessThan && row.CreatedAt >= bound {
				keep = false
			}
		}
		if keep {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return out
}

func (f *fakeAuditRepo) Get(_ context.Context, _ string, limit uint64, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.AuditLog, uint64, error) {
	matched := f.matching(filters)
	total := uint64(len(matched))
	if offset > total {
		offset = total
	}
	page := matched[offset:]
	if limit > 0 && uint64(len(page)) > limit {
		page = page[:limit]
	}
	return page, total, nil
}

func (f *fakeAuditRepo) Create(_ context.Context, _ string, model entities.AuditLog) (uint64, error) {
	f.nextID++
	model.Id = f.nextID
	cp := model
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}

func (f *fakeAuditRepo) Delete(_ context.Context, _ string, filters []sqldataenums.Filter) (uint64, error) {
	f.deletes++
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	doomed := map[int64]bool{}
	for _, row := range f.matching(filters) {
		doomed[row.Id] = true
	}
	kept := make([]*entities.AuditLog, 0, len(f.rows))
	for _, row := range f.rows {
		if !doomed[row.Id] {
			kept = append(kept, row)
		}
	}
	removed := uint64(len(f.rows) - len(kept))
	f.rows = kept
	return removed, nil
}

// seedAuditRows builds a repo holding `old` entries aged well past the retention window and
// `recent` entries from today.
func seedAuditRows(old, recent int) *fakeAuditRepo {
	repo := &fakeAuditRepo{}
	ancient := time.Now().UTC().AddDate(0, 0, -400).Unix()
	now := time.Now().UTC().Unix()
	for i := 0; i < old; i++ {
		repo.nextID++
		repo.rows = append(repo.rows, &entities.AuditLog{
			Id: repo.nextID, Action: ActionLoginSuccess, ActorEmail: fmt.Sprintf("old%d@example.test", i),
			Outcome: OutcomeSuccess, CreatedAt: ancient + int64(i),
		})
	}
	for i := 0; i < recent; i++ {
		repo.nextID++
		repo.rows = append(repo.rows, &entities.AuditLog{
			Id: repo.nextID, Action: ActionLoginFailure, ActorEmail: fmt.Sprintf("new%d@example.test", i),
			Outcome: OutcomeDenied, CreatedAt: now - int64(i),
		})
	}
	return repo
}

func readArchive(t *testing.T, path string) []entities.AuditLog {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	var out []entities.AuditLog
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var row entities.AuditLog
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatalf("archive line is not valid JSON: %v", err)
		}
		out = append(out, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan archive: %v", err)
	}
	return out
}

// The headline behaviour: expired rows leave the table only after landing in a readable
// archive, and rows inside the window are untouched.
func TestPurgeArchivesExpiredRowsAndKeepsRecentOnes(t *testing.T) {
	repo := seedAuditRows(3, 2)
	svc := &auditService{repo: repo}
	dir := t.TempDir()

	res, err := svc.PurgeOlderThan(context.Background(), 30, dir)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if res.Archived != 3 || res.Deleted != 3 {
		t.Fatalf("archived=%d deleted=%d want 3/3", res.Archived, res.Deleted)
	}

	archived := readArchive(t, res.ArchivePath)
	if len(archived) != 3 {
		t.Fatalf("archive holds %d rows want 3", len(archived))
	}
	// Content must survive the round trip, not just the row count — an archive of empty
	// records would satisfy a count-only assertion and be worthless in an investigation.
	if archived[0].ActorEmail != "old0@example.test" || archived[0].Action != ActionLoginSuccess {
		t.Fatalf("archived row lost its content: %+v", archived[0])
	}

	// Two recent rows plus the purge's own record of itself.
	var remainingRecent, purgeRecords int
	for _, row := range repo.rows {
		switch row.Action {
		case ActionAuditPurge:
			purgeRecords++
		case ActionLoginFailure:
			remainingRecent++
		default:
			t.Fatalf("an expired row survived the purge: %+v", row)
		}
	}
	if remainingRecent != 2 {
		t.Fatalf("recent rows remaining = %d want 2", remainingRecent)
	}
	if purgeRecords != 1 {
		t.Fatalf("purge wrote %d self-records, want exactly 1", purgeRecords)
	}
}

// The trim must be visible from inside the trail. Otherwise a reader cannot tell a history
// that was deliberately trimmed from one that never existed.
func TestPurgeRecordsItselfWithCutoffAndArchiveName(t *testing.T) {
	repo := seedAuditRows(2, 0)
	svc := &auditService{repo: repo}

	res, err := svc.PurgeOlderThan(context.Background(), 30, t.TempDir())
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}

	var marker *entities.AuditLog
	for _, row := range repo.rows {
		if row.Action == ActionAuditPurge {
			marker = row
		}
	}
	if marker == nil {
		t.Fatal("the purge left no record of itself in the trail")
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(marker.Metadata), &meta); err != nil {
		t.Fatalf("purge record metadata is not JSON: %v", err)
	}
	if meta["archiveFile"] != res.ArchivePath {
		t.Fatalf("purge record names archive %v, actual %s", meta["archiveFile"], res.ArchivePath)
	}
	if _, ok := meta["cutoff"]; !ok {
		t.Fatal("purge record must state the cutoff it applied")
	}
	if got := meta["deleted"]; got != float64(2) {
		t.Fatalf("purge record deleted=%v want 2", got)
	}
}

// The property the whole design rests on: if the archive cannot be written, nothing is
// deleted. A retention run may fail; it may not lose history.
func TestPurgeDeletesNothingWhenArchiveCannotBeWritten(t *testing.T) {
	repo := seedAuditRows(4, 1)
	svc := &auditService{repo: repo}

	// A regular file where the archive directory should be: MkdirAll cannot create a
	// directory underneath it on any supported platform.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}

	res, err := svc.PurgeOlderThan(context.Background(), 30, filepath.Join(blocker, "archive"))
	if err == nil {
		t.Fatal("an unwritable archive directory must fail the run, not proceed to delete")
	}
	// Guard against passing vacuously: the run has to reach the archive step and fail
	// THERE, not bail out earlier for an unrelated reason.
	if !strings.Contains(err.Error(), "archive directory") {
		t.Fatalf("expected the failure to come from the archive step, got: %v", err)
	}
	if res.Deleted != 0 {
		t.Fatalf("deleted %d rows despite the archive failing", res.Deleted)
	}
	if repo.deletes != 0 {
		t.Fatalf("Delete was called %d times despite the archive failing", repo.deletes)
	}
	if len(repo.rows) != 5 {
		t.Fatalf("table holds %d rows, want all 5 still present", len(repo.rows))
	}
}

// If the delete fails after the archive is durable, the archive must be kept and named in
// the error — the rows are still in the table, so the next run retries the same window, and
// an operator needs to know the file is there rather than orphaned.
func TestPurgeKeepsArchiveWhenDeleteFails(t *testing.T) {
	repo := seedAuditRows(2, 0)
	repo.deleteErr = fmt.Errorf("database is locked")
	svc := &auditService{repo: repo}
	dir := t.TempDir()

	_, err := svc.PurgeOlderThan(context.Background(), 30, dir)
	if err == nil {
		t.Fatal("a failed delete must be reported")
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "audit-through-id*.jsonl"))
	if len(matches) != 1 {
		t.Fatalf("expected the durable archive to be kept, found %v", matches)
	}
	// And no partial file left behind to be mistaken for one.
	partial, _ := filepath.Glob(filepath.Join(dir, ".audit-archive.partial"))
	if len(partial) != 0 {
		t.Fatalf("partial archive left behind: %v", partial)
	}
}

// Paging must cover the whole expired window, not just the first batch.
func TestPurgeArchivesEveryRowAcrossMultipleBatches(t *testing.T) {
	const rows = auditArchiveBatch*2 + 7
	repo := seedAuditRows(rows, 3)
	svc := &auditService{repo: repo}

	res, err := svc.PurgeOlderThan(context.Background(), 30, t.TempDir())
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if res.Archived != rows {
		t.Fatalf("archived %d of %d expired rows — paging dropped some", res.Archived, rows)
	}
	if got := len(readArchive(t, res.ArchivePath)); got != rows {
		t.Fatalf("archive file holds %d rows want %d", got, rows)
	}
}

// Nothing old enough means no run at all: no archive file, no delete, and no purge record
// cluttering the trail every single day.
func TestPurgeWithNothingExpiredIsANoOp(t *testing.T) {
	repo := seedAuditRows(0, 5)
	svc := &auditService{repo: repo}
	dir := t.TempDir()

	res, err := svc.PurgeOlderThan(context.Background(), 30, dir)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if res.Archived != 0 || res.Deleted != 0 || res.ArchivePath != "" {
		t.Fatalf("unexpected work done: %+v", res)
	}
	if repo.deletes != 0 {
		t.Fatalf("Delete called %d times with nothing expired", repo.deletes)
	}
	if len(repo.rows) != 5 {
		t.Fatalf("row count changed to %d", len(repo.rows))
	}
	entriesInDir, _ := os.ReadDir(dir)
	if len(entriesInDir) != 0 {
		t.Fatalf("no-op run created files: %v", entriesInDir)
	}
}

func TestPurgeRejectsNonPositiveRetention(t *testing.T) {
	repo := seedAuditRows(3, 0)
	svc := &auditService{repo: repo}

	if _, err := svc.PurgeOlderThan(context.Background(), 0, t.TempDir()); err == nil {
		t.Fatal("a zero retention window would delete the entire trail and must be refused")
	}
	if repo.deletes != 0 {
		t.Fatal("Delete was called for a zero retention window")
	}
}
