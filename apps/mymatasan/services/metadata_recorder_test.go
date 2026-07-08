package services

import (
	"context"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// fakeObsRepo captures the observation rows the recorder writes. Only Create is used.
type fakeObsRepo struct {
	dbsql.IGenericRepo[entities.ObjectObservation]
	created []entities.ObjectObservation
}

func (f *fakeObsRepo) Create(_ context.Context, _ string, model entities.ObjectObservation) (uint64, error) {
	f.created = append(f.created, model)
	return uint64(len(f.created)), nil
}

// fakeConfigLister returns a fixed set of recording configs for the recorder's
// enable/gap lookup.
type fakeConfigLister struct{ cfgs []*entities.RecordingConfig }

func (f fakeConfigLister) ListConfigs(_ context.Context) ([]*entities.RecordingConfig, error) {
	return f.cfgs, nil
}

func newTestRecorder(t *testing.T, cfgs []*entities.RecordingConfig) (*MetadataRecorder, *fakeObsRepo, *int64) {
	t.Helper()
	repo := &fakeObsRepo{}
	r := NewMetadataRecorder(repo, fakeConfigLister{cfgs: cfgs}, 0.25)
	var clock int64 = 1_000
	r.now = func() time.Time { return time.Unix(clock, 0).UTC() }
	r.refreshConfig(context.Background())
	return r, repo, &clock
}

func cand(label string, conf float64) vision.ObjectCandidate {
	return vision.ObjectCandidate{Label: label, Confidence: conf, Box: vision.Box{X: 0.1, Y: 0.1, W: 0.2, H: 0.2}}
}

// drainWrites synchronously flushes the async write queue (closeStale enqueues rather
// than writing inline), so tests can assert on repo rows deterministically.
func drainWrites(r *MetadataRecorder) {
	for {
		select {
		case e := <-r.writeCh:
			r.write(e)
		default:
			return
		}
	}
}

func TestMetadataRecorderCoalescesPresenceIntervals(t *testing.T) {
	r, repo, clock := newTestRecorder(t, []*entities.RecordingConfig{
		{CameraId: 1, MetadataEnabled: true, MetadataGapSeconds: 5},
	})

	// t0: two people + one car (+ a weak candidate that must be filtered out).
	r.Observe(1, 1000, []vision.ObjectCandidate{cand("person", 0.9), cand("person", 0.8), cand("car", 0.6), cand("dog", 0.10)})
	// t0+2: person still present with higher confidence; car gone.
	r.Observe(1, 1002, []vision.ObjectCandidate{cand("person", 0.95)})

	// Nothing has been absent past the 5s gap yet.
	*clock = 1003
	r.closeStale()
	if len(repo.created) != 0 {
		t.Fatalf("expected no intervals closed yet, got %d", len(repo.created))
	}

	// Advance past the gap for both labels.
	*clock = 1010
	r.closeStale()
	drainWrites(r)
	if len(repo.created) != 2 {
		t.Fatalf("expected 2 closed intervals, got %d", len(repo.created))
	}

	byLabel := map[string]entities.ObjectObservation{}
	for _, o := range repo.created {
		byLabel[o.Label] = o
	}

	person, ok := byLabel["person"]
	if !ok {
		t.Fatal("missing person interval")
	}
	if person.StartedAt != 1000 || person.EndedAt != 1002 {
		t.Errorf("person span = [%d,%d], want [1000,1002]", person.StartedAt, person.EndedAt)
	}
	if person.MaxCount != 2 {
		t.Errorf("person maxCount = %d, want 2", person.MaxCount)
	}
	if person.SampleCount != 2 {
		t.Errorf("person sampleCount = %d, want 2", person.SampleCount)
	}
	if person.MaxConfidence < 0.94 || person.MaxConfidence > 0.96 {
		t.Errorf("person maxConfidence = %v, want ~0.95", person.MaxConfidence)
	}

	car, ok := byLabel["car"]
	if !ok {
		t.Fatal("missing car interval")
	}
	if car.StartedAt != 1000 || car.EndedAt != 1000 {
		t.Errorf("car span = [%d,%d], want [1000,1000]", car.StartedAt, car.EndedAt)
	}
	if car.MaxCount != 1 {
		t.Errorf("car maxCount = %d, want 1", car.MaxCount)
	}
	if _, ok := byLabel["dog"]; ok {
		t.Error("weak 'dog' candidate should have been filtered by minConfidence")
	}
}

func TestMetadataRecorderDedupsSameFrame(t *testing.T) {
	r, repo, clock := newTestRecorder(t, []*entities.RecordingConfig{
		{CameraId: 1, MetadataEnabled: true, MetadataGapSeconds: 5},
	})

	r.Observe(1, 1000, []vision.ObjectCandidate{cand("person", 0.9)})
	if !r.Observed(1, 1000) {
		t.Fatal("expected Observed(1,1000) true after first observe")
	}
	// Same frame again (e.g. rule Detect path + metadata ObserveOnly path): ignored.
	r.Observe(1, 1000, []vision.ObjectCandidate{cand("person", 0.9)})

	*clock = 1010
	r.closeStale()
	drainWrites(r)
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 interval, got %d", len(repo.created))
	}
	if repo.created[0].SampleCount != 1 {
		t.Errorf("sampleCount = %d, want 1 (frame counted once)", repo.created[0].SampleCount)
	}
}

func TestMetadataRecorderIgnoresDisabledCamera(t *testing.T) {
	r, repo, clock := newTestRecorder(t, []*entities.RecordingConfig{
		{CameraId: 1, MetadataEnabled: false},
	})

	r.Observe(1, 1000, []vision.ObjectCandidate{cand("person", 0.9)})
	*clock = 1010
	r.closeStale()
	drainWrites(r)
	if len(repo.created) != 0 {
		t.Fatalf("disabled camera should record nothing, got %d rows", len(repo.created))
	}
	if r.IsEnabled(1) {
		t.Error("IsEnabled(1) should be false")
	}
	if len(r.EnabledCameras()) != 0 {
		t.Error("EnabledCameras should be empty")
	}
}

func TestMetadataRecorderFlushAllOnShutdown(t *testing.T) {
	r, repo, _ := newTestRecorder(t, []*entities.RecordingConfig{
		{CameraId: 1, MetadataEnabled: true, MetadataGapSeconds: 5},
	})
	r.Observe(1, 1000, []vision.ObjectCandidate{cand("person", 0.9)})
	// Still within the gap window, but shutdown must not lose the open interval.
	r.flushAll()
	if len(repo.created) != 1 {
		t.Fatalf("flushAll should write the open interval, got %d rows", len(repo.created))
	}
}
