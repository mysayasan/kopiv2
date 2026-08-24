package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// The hold is the property that makes a case worth opening: footage an OPEN case points at
// survives retention, survives "Purge now", and survives the disk-pressure sweeper. These
// tests drive each of those to the point where it actually deletes something, because a
// guard with no test that makes it FIRE is a guard that may not work at all — the tamper
// MOVED verdict shipped dead for exactly that reason.

// holdSegmentRepo honours the camera and time filters the purge paths send, and the
// offset — the offset matters because held rows stay at the front of an oldest-first
// window, and a purge that cannot page past them silently stops at the first held row.
type holdSegmentRepo struct {
	dbsql.IGenericRepo[entities.RecordingSegment]
	rows []*entities.RecordingSegment
}

func (f *holdSegmentRepo) Get(_ context.Context, _ string, limit, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.RecordingSegment, uint64, error) {
	var out []*entities.RecordingSegment
	for _, row := range f.rows {
		keep := true
		for _, fl := range filters {
			switch {
			case fl.FieldName == "CameraId" && fl.Compare == sqldataenums.Equal:
				keep = keep && row.CameraId == fl.Value.(int64)
			case fl.FieldName == "AlertId" && fl.Compare == sqldataenums.Equal:
				keep = keep && row.AlertId == fl.Value.(int64)
			case fl.FieldName == "StartedAt" && fl.Compare == sqldataenums.LessThan:
				keep = keep && row.StartedAt < fl.Value.(int64)
			case fl.FieldName == "StartedAt" && fl.Compare == sqldataenums.GreaterThanOrEqualTo:
				keep = keep && row.StartedAt >= fl.Value.(int64)
			}
		}
		if keep {
			cp := *row
			out = append(out, &cp)
		}
	}
	total := uint64(len(out))
	desc := false
	for _, s := range sorters {
		if s.FieldName == "StartedAt" && s.Sort == sqldataenums.DESC {
			desc = true
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if desc {
			return out[i].StartedAt > out[j].StartedAt
		}
		return out[i].StartedAt < out[j].StartedAt
	})
	if offset >= uint64(len(out)) {
		return nil, total, nil
	}
	out = out[offset:]
	if limit > 0 && uint64(len(out)) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *holdSegmentRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func (f *holdSegmentRepo) ids() []int64 {
	out := make([]int64, 0, len(f.rows))
	for _, row := range f.rows {
		out = append(out, row.Id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

type holdConfigRepo struct {
	dbsql.IGenericRepo[entities.RecordingConfig]
	rows []*entities.RecordingConfig
}

func (f *holdConfigRepo) Get(_ context.Context, _ string, _, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.RecordingConfig, uint64, error) {
	return f.rows, uint64(len(f.rows)), nil
}

func (f *holdConfigRepo) GetSingle(_ context.Context, _ string, filters []sqldataenums.Filter) (*entities.RecordingConfig, error) {
	for _, row := range f.rows {
		for _, fl := range filters {
			if fl.FieldName == "CameraId" && row.CameraId == fl.Value.(int64) {
				cp := *row
				return &cp, nil
			}
		}
	}
	return nil, errors.New("no result found")
}

// holdFixture is a case service and a recording service over the same fakes, wired
// together through the guard exactly as the composition root wires them.
type holdFixture struct {
	cases    *caseFixture
	segments *holdSegmentRepo
	configs  *holdConfigRepo
	guard    *FootageGuard
	rec      *recordingService
	now      int64
}

func newHoldFixture(t *testing.T, retentionDays int, segs []*entities.RecordingSegment) *holdFixture {
	t.Helper()
	f := &holdFixture{
		cases:    newCaseFixture(t),
		segments: &holdSegmentRepo{rows: segs},
		configs: &holdConfigRepo{rows: []*entities.RecordingConfig{
			{Id: 1, CameraId: 3, Enabled: true, RetentionDays: retentionDays},
		}},
	}
	f.now = f.cases.now
	f.guard = NewFootageGuard(f.cases.svc, nil)
	f.rec = &recordingService{segments: f.segments, configs: f.configs, guard: f.guard}
	f.cases.svc.recording = f.rec
	return f
}

// hold puts one span of camera 3 into an open case.
func (f *holdFixture) hold(t *testing.T, from, to int64) *entities.CaseFile {
	t.Helper()
	row := f.cases.openCase(t, "Loading bay")
	if _, err := f.cases.svc.AddItem(context.Background(), row.Id, CaseItemInput{
		CameraId: 3, StartedAt: from, EndedAt: to, Actor: CaseActor{Id: 7, Name: "sam"},
	}); err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	return row
}

func expiredSegments(now int64) []*entities.RecordingSegment {
	// All three are older than a one-day retention window. Only the middle one is going
	// to be evidence, so a purge that respects the hold still has work to do — a test
	// where everything is held cannot tell "held" from "broken".
	return []*entities.RecordingSegment{
		{Id: 1, CameraId: 3, StartedAt: now - 5*86400, EndedAt: now - 5*86400 + 900, FileSize: 100},
		{Id: 2, CameraId: 3, StartedAt: now - 4*86400, EndedAt: now - 4*86400 + 900, FileSize: 100},
		{Id: 3, CameraId: 3, StartedAt: now - 3*86400, EndedAt: now - 3*86400 + 900, FileSize: 100},
	}
}

func TestRetentionKeepsFootageAnOpenCaseHolds(t *testing.T) {
	f := newHoldFixture(t, 1, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	f.hold(t, now-4*86400+100, now-4*86400+200)

	deleted, err := f.rec.PurgeOldSegments(context.Background())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected the two unheld expired segments to go, deleted %d", deleted)
	}
	if got := f.segments.ids(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("retention deleted the wrong segments, left %v", got)
	}
}

// The half that is easy to get wrong: once the case is closed the footage goes back to the
// retention policy it would have had anyway. A hold that never releases is a disk that
// fills up.
func TestRetentionTakesTheFootageOnceTheCaseIsClosed(t *testing.T) {
	f := newHoldFixture(t, 1, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	row := f.hold(t, now-4*86400+100, now-4*86400+200)
	if _, err := f.cases.svc.Close(context.Background(), row.Id, "no further action", CaseActor{Id: 7}); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := f.rec.PurgeOldSegments(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if got := f.segments.ids(); len(got) != 0 {
		t.Fatalf("closing the case must release the hold, left %v", got)
	}
}

func TestRemovingEvidenceReleasesTheHold(t *testing.T) {
	f := newHoldFixture(t, 1, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	row := f.hold(t, now-4*86400+100, now-4*86400+200)
	items, err := f.cases.svc.itemsForCases(context.Background(), []int64{row.Id})
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one item: %v %d", err, len(items))
	}
	if _, err := f.cases.svc.RemoveItem(context.Background(), row.Id, items[0].Id); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := f.rec.PurgeOldSegments(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if got := f.segments.ids(); len(got) != 0 {
		t.Fatalf("removing the evidence must release the hold, left %v", got)
	}
}

// A hold that only covers part of a segment still covers the segment: a file cannot be
// half-deleted, and the half that matters is the evidence.
func TestAPartialOverlapHoldsTheWholeSegment(t *testing.T) {
	f := newHoldFixture(t, 1, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	// Starts inside segment 2's last second and runs past its end.
	f.hold(t, now-4*86400+899, now-4*86400+1800)

	if _, err := f.rec.PurgeOldSegments(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if got := f.segments.ids(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("a partially overlapping hold must keep the segment, left %v", got)
	}
}

// Touching endpoints are not an overlap. Evidence that starts exactly when a segment ends
// contains none of that segment, and holding it would keep footage for no reason — which
// on an appliance means keeping it forever, since nothing ever revisits the decision.
func TestATouchingSpanDoesNotHold(t *testing.T) {
	f := newHoldFixture(t, 1, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	f.hold(t, now-4*86400+900, now-4*86400+1000)

	if _, err := f.rec.PurgeOldSegments(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if got := f.segments.ids(); len(got) != 0 {
		t.Fatalf("a touching span must not hold, left %v", got)
	}
}

// FAIL CLOSED. A hold that cannot be read is not "no hold" — the two are indistinguishable
// from the purge's side, and one of them destroys evidence.
func TestAnUnreadableHoldStopsEveryPurge(t *testing.T) {
	f := newHoldFixture(t, 1, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	f.hold(t, now-4*86400+100, now-4*86400+200)
	f.cases.cases.failGet = true

	deleted, err := f.rec.PurgeOldSegments(context.Background())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 0 || len(f.segments.ids()) != 3 {
		t.Fatalf("a failed hold lookup must keep everything, deleted %d left %v", deleted, f.segments.ids())
	}

	res, err := f.rec.PurgeCameraFootage(context.Background(), 3)
	if err != nil {
		t.Fatalf("purge now: %v", err)
	}
	if res.Deleted != 0 || res.Kept != 3 {
		t.Fatalf("purge now must keep everything when the hold is unreadable: %+v", res)
	}

	n, _, err := f.rec.PurgeOldestSegments(context.Background(), now, 1000)
	if err != nil {
		t.Fatalf("disk pressure: %v", err)
	}
	if n != 0 {
		t.Fatalf("the disk-pressure sweeper must free nothing when the hold is unreadable, freed %d", n)
	}
}

// "Purge now" is the operator's destroy button and it still refuses held footage — and it
// says so, because a purge that silently leaves footage behind is one nobody trusts again.
func TestPurgeNowKeepsHeldFootageAndSaysWhy(t *testing.T) {
	f := newHoldFixture(t, 0, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	f.hold(t, now-4*86400+100, now-4*86400+200)

	res, err := f.rec.PurgeCameraFootage(context.Background(), 3)
	if err != nil {
		t.Fatalf("purge now: %v", err)
	}
	if res.Deleted != 2 || res.Kept != 1 {
		t.Fatalf("expected 2 deleted and 1 kept, got %+v", res)
	}
	if res.Reason == "" {
		t.Fatal("the operator must be told which case kept the footage")
	}
	if got := f.segments.ids(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("wrong segment kept: %v", got)
	}
}

// The camera-delete cascade takes the unconditional purge: the camera is going away, and
// footage held by a case nobody can then find or release would be held forever.
func TestDeletingACameraPurgesEvenHeldFootage(t *testing.T) {
	f := newHoldFixture(t, 0, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	f.hold(t, now-4*86400+100, now-4*86400+200)

	deleted, err := f.rec.PurgeAllForCamera(context.Background(), 3)
	if err != nil {
		t.Fatalf("purge all: %v", err)
	}
	if deleted != 3 || len(f.segments.ids()) != 0 {
		t.Fatalf("the cascade purge must take everything, deleted %d left %v", deleted, f.segments.ids())
	}
}

// The disk-pressure sweeper runs BECAUSE the appliance is short of space, which is exactly
// when the temptation is to take whatever it needs. It must not take evidence, and it must
// report the shortfall rather than pretending it freed enough.
func TestDiskPressureNeverEvictsEvidence(t *testing.T) {
	f := newHoldFixture(t, 0, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	f.hold(t, now-4*86400+100, now-4*86400+200)

	deleted, freed, err := f.rec.PurgeOldestSegments(context.Background(), now, 300)
	if err != nil {
		t.Fatalf("disk pressure: %v", err)
	}
	if deleted != 2 || freed != 200 {
		t.Fatalf("expected to free only the unheld 200 bytes, got %d segments / %d bytes", deleted, freed)
	}
	if got := f.segments.ids(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("the sweeper evicted evidence: left %v", got)
	}
}

// The recorder's own file sweep is the fourth deletion path and the one that knows nothing
// about the database. The predicate it is handed must answer the same way the DB purge
// does, or a hold enforced everywhere else is undone within the hour.
func TestTheRecorderPredicateAnswersTheSameAsThePurge(t *testing.T) {
	f := newHoldFixture(t, 1, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	f.hold(t, now-4*86400+100, now-4*86400+200)

	predicate := f.guard.Predicate()
	if predicate == nil {
		t.Fatal("the recorder must be given a predicate")
	}
	if !predicate(3, now-4*86400, now-4*86400+900) {
		t.Fatal("the recorder must keep a file the case holds")
	}
	if predicate(3, now-5*86400, now-5*86400+900) {
		t.Fatal("the recorder must delete a file no case holds")
	}
	if predicate(4, now-4*86400, now-4*86400+900) {
		t.Fatal("a hold on camera 3 must not keep camera 4's footage")
	}
}

// What the case screen shows about its own hold. BeyondRetention is the number that turns
// closing a case into a decision: those clips exist only because the case is open.
func TestTheCaseSaysWhatItIsKeepingAlive(t *testing.T) {
	f := newHoldFixture(t, 1, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	row := f.hold(t, now-4*86400+100, now-4*86400+200)
	// A second piece of evidence whose footage is already gone.
	if _, err := f.cases.svc.AddItem(context.Background(), row.Id, CaseItemInput{
		CameraId: 3, StartedAt: now - 40*86400, EndedAt: now - 40*86400 + 60,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	detail, err := f.cases.svc.Get(context.Background(), row.Id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.Hold.Items != 2 {
		t.Fatalf("expected two footage items, got %d", detail.Hold.Items)
	}
	if detail.Hold.Segments != 1 || detail.Hold.Bytes != 100 {
		t.Fatalf("expected one held segment of 100 bytes, got %d / %d", detail.Hold.Segments, detail.Hold.Bytes)
	}
	if detail.Hold.BeyondRetention != 1 {
		t.Fatalf("the held clip is four days past a one-day retention: expected 1, got %d", detail.Hold.BeyondRetention)
	}
	if detail.Hold.Missing != 1 {
		t.Fatalf("expected the vanished clip to be reported missing, got %d", detail.Hold.Missing)
	}
}

// A camera that keeps footage forever has no retention cutoff, so nothing in its case is
// "only alive because of the hold". Reporting otherwise would tell an operator that
// closing the case destroys footage that closing the case does not touch.
func TestNothingIsBeyondRetentionWhenRetentionIsForever(t *testing.T) {
	f := newHoldFixture(t, 0, nil)
	now := f.now
	f.segments.rows = expiredSegments(now)
	row := f.hold(t, now-4*86400+100, now-4*86400+200)

	detail, err := f.cases.svc.Get(context.Background(), row.Id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.Hold.BeyondRetention != 0 {
		t.Fatalf("retention is off; nothing can be past it, got %d", detail.Hold.BeyondRetention)
	}
}

// A guard with no case service blocks nothing. That is the behaviour a unit test or an
// install without cases must get — and it must not panic, because the guard is held by
// every deletion path in the app.
func TestAGuardWithNoCasesBlocksNothing(t *testing.T) {
	guard := NewFootageGuard(nil, nil)
	spans := guard.CameraHolds(context.Background(), 3)
	if spans.Any() {
		t.Fatal("an unwired guard must hold nothing")
	}
	if blocked, _ := spans.Blocked(0, 1); blocked {
		t.Fatal("an unwired guard must block nothing")
	}
	var nilGuard *FootageGuard
	if nilGuard.CameraHolds(context.Background(), 3).Any() {
		t.Fatal("a nil guard must hold nothing")
	}
}

// Retention deletes the FILE as well as the row. The hold has to keep both, or the case
// keeps a row pointing at nothing.
func TestAHeldSegmentsFileIsNotShredded(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "kept.mp4")
	gone := filepath.Join(dir, "gone.mp4")
	for _, p := range []string{kept, gone} {
		if err := os.WriteFile(p, []byte("video"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	f := newHoldFixture(t, 1, nil)
	now := f.now
	f.segments.rows = []*entities.RecordingSegment{
		{Id: 1, CameraId: 3, StartedAt: now - 5*86400, EndedAt: now - 5*86400 + 900, FilePath: gone},
		{Id: 2, CameraId: 3, StartedAt: now - 4*86400, EndedAt: now - 4*86400 + 900, FilePath: kept},
	}
	f.hold(t, now-4*86400+100, now-4*86400+200)

	if _, err := f.rec.PurgeOldSegments(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("the held segment's file was deleted: %v", err)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatalf("the unheld segment's file should have been removed, got %v", err)
	}
}
