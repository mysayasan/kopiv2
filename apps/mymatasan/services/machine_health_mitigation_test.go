package services

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/recording"
)

// fakeSegmentRepo is a minimal in-memory IGenericRepo for the methods
// PurgeOldestSegments touches (Get with a StartedAt LessThan filter + ASC sort,
// and DeleteById). Other methods are unimplemented and panic if called.
type fakeSegmentRepo struct {
	dbsql.IGenericRepo[entities.RecordingSegment]
	rows []*entities.RecordingSegment
}

func (f *fakeSegmentRepo) Get(_ context.Context, _ string, limit uint64, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.RecordingSegment, uint64, error) {
	var out []*entities.RecordingSegment
	for _, r := range f.rows {
		keep := true
		for _, fl := range filters {
			if fl.FieldName == "StartedAt" && fl.Compare == sqldataenums.LessThan {
				cutoff, _ := fl.Value.(int64)
				if !(r.StartedAt < cutoff) {
					keep = false
				}
			}
		}
		if keep {
			cp := *r
			out = append(out, &cp)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	if offset >= uint64(len(out)) {
		return nil, uint64(len(out)), nil
	}
	out = out[offset:]
	if limit > 0 && uint64(len(out)) > limit {
		out = out[:limit]
	}
	return out, uint64(len(out)), nil
}

func (f *fakeSegmentRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, r := range f.rows {
		if r.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeSegmentRepo) ids() []int64 {
	out := make([]int64, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r.Id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestPurgeOldestSegmentsDeletesOldestFirstUntilBytesFreed(t *testing.T) {
	now := time.Now().UTC().Unix()
	repo := &fakeSegmentRepo{rows: []*entities.RecordingSegment{
		{Id: 1, CameraId: 1, StartedAt: now - 4*3600, FileSize: 100},
		{Id: 2, CameraId: 1, StartedAt: now - 3*3600, FileSize: 100},
		{Id: 3, CameraId: 2, StartedAt: now - 2*3600, FileSize: 100},
		{Id: 4, CameraId: 2, StartedAt: now - 1*3600, FileSize: 100},
	}}
	svc := &recordingService{segments: repo}

	deleted, freed, err := svc.PurgeOldestSegments(context.Background(), now, 150)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 2 || freed != 200 {
		t.Fatalf("deleted=%d freed=%d, want 2 segments / 200 bytes", deleted, freed)
	}
	got := repo.ids()
	if len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("remaining ids = %v, want [3 4] (oldest deleted first)", got)
	}
}

func TestPurgeOldestSegmentsHonorsKeepFloor(t *testing.T) {
	now := time.Now().UTC().Unix()
	floor := now - 24*3600
	repo := &fakeSegmentRepo{rows: []*entities.RecordingSegment{
		{Id: 1, CameraId: 1, StartedAt: now - 48*3600, FileSize: 100},
		{Id: 2, CameraId: 1, StartedAt: now - 3600, FileSize: 100}, // inside floor
	}}
	svc := &recordingService{segments: repo}

	deleted, freed, err := svc.PurgeOldestSegments(context.Background(), floor, 1<<40)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 1 || freed != 100 {
		t.Fatalf("deleted=%d freed=%d, want only the pre-floor segment", deleted, freed)
	}
	if got := repo.ids(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("remaining ids = %v, want [2]", got)
	}
}

// fakeRecordingPurger stubs IRecordingService for the monitor tests; only
// PurgeOldestSegments is implemented.
type fakeRecordingPurger struct {
	IRecordingService
	deleted   int
	freed     int64
	calls     int
	wantBytes int64
	keepAfter int64
}

func (f *fakeRecordingPurger) PurgeOldestSegments(_ context.Context, keepAfter int64, wantBytes int64) (int, int64, error) {
	f.calls++
	f.keepAfter = keepAfter
	f.wantBytes = wantBytes
	return f.deleted, f.freed, nil
}

func overwriteTestSettings() MachineHealthSettings {
	cfg := DefaultMachineHealthSettings()
	cfg.SustainedSamples = 1
	cfg.Mitigation.OverwriteOldest = true
	cfg.Mitigation.OverwriteMinKeepDays = 2
	// Keep the early expired-purge branch out of the way (the fake only
	// implements PurgeOldestSegments).
	cfg.Mitigation.PurgeAtPercent = 100
	return cfg
}

func overwriteTestMetrics(usedPercent float64) MachineMetrics {
	return MachineMetrics{Disks: []MachineDiskMetric{
		{Mountpoint: "D:\\", UsedPercent: usedPercent, TotalBytes: 1000 * 1000},
	}}
}

func TestRunMitigationOverwriteKeepsRecordingRunning(t *testing.T) {
	purger := &fakeRecordingPurger{deleted: 3, freed: 5000}
	m := &MachineHealthMonitor{
		recorder:  &recording.Manager{},
		recording: purger,
		states:    map[string]*machineMetricState{},
	}
	cfg := overwriteTestSettings()

	m.runMitigation(context.Background(), cfg, overwriteTestMetrics(96))

	if purger.calls != 1 {
		t.Fatalf("purge calls = %d, want 1", purger.calls)
	}
	if m.recorder.IsPaused() || m.pausedByUs {
		t.Fatal("recording must stay running when the overwrite freed space")
	}
	// want = (96% - 80% resume) of 1,000,000 total bytes.
	if purger.wantBytes != 160000 {
		t.Fatalf("wantBytes = %d, want 160000", purger.wantBytes)
	}
	// keepAfter must sit ~2 days back (the keep floor).
	wantFloor := time.Now().UTC().Add(-48 * time.Hour).Unix()
	if diff := purger.keepAfter - wantFloor; diff < -5 || diff > 5 {
		t.Fatalf("keepAfter = %d, want ~%d", purger.keepAfter, wantFloor)
	}
}

func TestRunMitigationOverwriteFallsBackToPauseWhenNothingFreed(t *testing.T) {
	purger := &fakeRecordingPurger{deleted: 0, freed: 0}
	m := &MachineHealthMonitor{
		recorder:  &recording.Manager{},
		recording: purger,
		states:    map[string]*machineMetricState{},
	}
	cfg := overwriteTestSettings()

	m.runMitigation(context.Background(), cfg, overwriteTestMetrics(96))

	if purger.calls != 1 {
		t.Fatalf("purge calls = %d, want 1", purger.calls)
	}
	if !m.recorder.IsPaused() || !m.pausedByUs {
		t.Fatal("recording must pause when the overwrite could not free anything")
	}
}

func TestNormalizeOverwriteMinKeepDays(t *testing.T) {
	got := normalizeMachineHealthSettings(MachineHealthSettings{}).Mitigation
	if got.OverwriteMinKeepDays != 1 {
		t.Fatalf("zero keep days should default to 1, got %d", got.OverwriteMinKeepDays)
	}
	got = normalizeMachineHealthSettings(MachineHealthSettings{
		Mitigation: MachineMitigationSettings{OverwriteMinKeepDays: 9999},
	}).Mitigation
	if got.OverwriteMinKeepDays != 365 {
		t.Fatalf("keep days should clamp to 365, got %d", got.OverwriteMinKeepDays)
	}
}
