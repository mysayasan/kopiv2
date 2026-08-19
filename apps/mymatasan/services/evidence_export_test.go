package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
)

// stubSegments serves a fixed segment list to the planner.
type stubSegments struct {
	IRecordingService
	rows []*entities.RecordingSegment
}

func (s *stubSegments) GetSegments(_ context.Context, _, _ uint64, _, _, _, _ int64) ([]*entities.RecordingSegment, uint64, error) {
	return s.rows, uint64(len(s.rows)), nil
}

type stubCameraName struct {
	ICameraService
	name string
}

func (s *stubCameraName) DisplayName(context.Context, int64) string { return s.name }

func newExportService(rows []*entities.RecordingSegment) *evidenceExportService {
	return &evidenceExportService{
		recording:  &stubSegments{rows: rows},
		camera:     &stubCameraName{name: "Lobby"},
		appVersion: "1.2.3",
		jobs:       map[string]*ExportJob{},
	}
}

func evSeg(id, start, end int64, sha string) *entities.RecordingSegment {
	return &entities.RecordingSegment{
		Id: id, CameraId: 3, StartedAt: start, EndedAt: end, Sha256: sha,
		FilePath: "/rec/cam3/seg.mp4", Codec: "h264",
	}
}

// THE test for this feature. An export that silently skips missing footage looks
// continuous, and that is actively misleading — the recipient sees an unbroken video and
// reasonably concludes nothing happened in the minutes that are simply absent. Refusing
// to export would be better than that; reporting it is better still.
func TestExportManifestReportsGapsInTheRange(t *testing.T) {
	// 14:00–15:00 requested. Recorded: 14:00–14:20 and 14:40–15:00. Missing: 14:20–14:40.
	svc := newExportService([]*entities.RecordingSegment{
		evSeg(1, testHourStart, testHourStart+1200, "aa"),
		evSeg(2, testHourStart+2400, testHourEnd, "bb"),
	})

	man, err := svc.Preview(context.Background(), 3, testHourStart, testHourEnd)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(man.Gaps) != 1 {
		t.Fatalf("expected 1 gap, got %d: %+v", len(man.Gaps), man.Gaps)
	}
	g := man.Gaps[0]
	if g.From != testHourStart+1200 || g.To != testHourStart+2400 {
		t.Fatalf("gap = [%d,%d), want [%d,%d)", g.From, g.To, testHourStart+1200, testHourStart+2400)
	}
	if man.CoveredSeconds != 2400 {
		t.Errorf("coveredSeconds = %d, want 2400", man.CoveredSeconds)
	}
	if man.CoveragePct < 66 || man.CoveragePct > 67 {
		t.Errorf("coveragePercent = %v, want ~66.67", man.CoveragePct)
	}
}

// A leading and trailing gap are the easy ones to miss: the loop that walks merged spans
// naturally finds the holes BETWEEN them and can forget the edges entirely.
func TestExportManifestReportsLeadingAndTrailingGaps(t *testing.T) {
	// Requested 14:00–15:00; recorded only the middle 20 minutes.
	svc := newExportService([]*entities.RecordingSegment{
		evSeg(1, testHourStart+1200, testHourStart+2400, "aa"),
	})
	man, err := svc.Preview(context.Background(), 3, testHourStart, testHourEnd)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(man.Gaps) != 2 {
		t.Fatalf("expected leading and trailing gaps, got %d: %+v", len(man.Gaps), man.Gaps)
	}
	if man.Gaps[0].From != testHourStart || man.Gaps[0].To != testHourStart+1200 {
		t.Errorf("leading gap wrong: %+v", man.Gaps[0])
	}
	if man.Gaps[1].From != testHourStart+2400 || man.Gaps[1].To != testHourEnd {
		t.Errorf("trailing gap wrong: %+v", man.Gaps[1])
	}
}

// An empty array means "checked, none found". A missing field would be indistinguishable
// from "did not look", which is the ambiguity the whole manifest exists to remove — so it
// must survive JSON encoding as [], never as null or an absent key.
func TestExportManifestAlwaysCarriesAGapsArray(t *testing.T) {
	svc := newExportService([]*entities.RecordingSegment{
		evSeg(1, testHourStart, testHourEnd, "aa"),
	})
	man, err := svc.Preview(context.Background(), 3, testHourStart, testHourEnd)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(man.Gaps) != 0 {
		t.Fatalf("a fully covered range should have no gaps, got %+v", man.Gaps)
	}
	blob, err := json.Marshal(man)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(blob), `"gaps":[]`) {
		t.Fatalf(`manifest must encode "gaps":[] so "none found" is distinguishable from "did not look": %s`, blob)
	}
}

// A digest taken at export time proves only that the file has not changed SINCE the
// export. Presenting that as the stronger "not altered since it was recorded" claim would
// misrepresent the evidence, so the two are labelled differently and the label is part of
// the manifest a recipient reads.
func TestExportLabelsUnhashedSegmentsHonestly(t *testing.T) {
	svc := newExportService([]*entities.RecordingSegment{
		evSeg(1, testHourStart, testHourStart+1800, "recorded-digest"),
		evSeg(2, testHourStart+1800, testHourEnd, ""), // legacy row, or adopted after a crash
	})
	man, err := svc.Preview(context.Background(), 3, testHourStart, testHourEnd)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(man.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(man.Sources))
	}
	if man.Sources[0].HashOrigin != "recorded" {
		t.Errorf("a segment hashed at finalize must be labelled 'recorded', got %q", man.Sources[0].HashOrigin)
	}
	if man.Sources[1].HashOrigin != "computed-at-export" {
		t.Errorf("a segment with no stored digest must be labelled 'computed-at-export', got %q", man.Sources[1].HashOrigin)
	}
}

func TestExportRejectsAnEmptyReason(t *testing.T) {
	svc := newExportService([]*entities.RecordingSegment{evSeg(1, testHourStart, testHourEnd, "aa")})
	_, err := svc.Create(context.Background(), ExportRequest{
		CameraId: 3, From: testHourStart, To: testHourEnd, Reason: "   ",
	})
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("an evidence export with no stated purpose must be refused, got %v", err)
	}
}

func TestExportRejectsAnOverlongRange(t *testing.T) {
	svc := newExportService(nil)
	_, err := svc.Preview(context.Background(), 3, testHourStart, testHourStart+exportMaxRangeSeconds+1)
	if err == nil || !strings.Contains(err.Error(), "range too long") {
		t.Fatalf("expected a range-length refusal, got %v", err)
	}
}

func TestExportRejectsAnInvertedRange(t *testing.T) {
	svc := newExportService(nil)
	if _, err := svc.Preview(context.Background(), 3, testHourEnd, testHourStart); err == nil {
		t.Fatal("an end before the start must be refused")
	}
}

// Segments entirely outside the requested range must not appear as sources — a manifest
// that lists footage the bundle does not contain is a manifest nobody can rely on.
func TestExportExcludesSegmentsOutsideTheRange(t *testing.T) {
	svc := newExportService([]*entities.RecordingSegment{
		evSeg(1, testHourStart-7200, testHourStart-3600, "old"),
		evSeg(2, testHourStart, testHourEnd, "wanted"),
		evSeg(3, testHourEnd+3600, testHourEnd+7200, "later"),
	})
	man, err := svc.Preview(context.Background(), 3, testHourStart, testHourEnd)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(man.Sources) != 1 || man.Sources[0].SegmentId != 2 {
		t.Fatalf("expected only the in-range segment, got %+v", man.Sources)
	}
}

// A segment that straddles the range start covers its first minutes. Dropping it would
// invent a leading gap that does not exist and understate the evidence.
func TestExportCountsASegmentStraddlingTheStart(t *testing.T) {
	svc := newExportService([]*entities.RecordingSegment{
		evSeg(1, testHourStart-600, testHourStart+1800, "aa"),
		evSeg(2, testHourStart+1800, testHourEnd, "bb"),
	})
	man, err := svc.Preview(context.Background(), 3, testHourStart, testHourEnd)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(man.Gaps) != 0 {
		t.Fatalf("the range is fully covered; got gaps %+v", man.Gaps)
	}
	if man.CoveredSeconds != 3600 {
		t.Errorf("coveredSeconds = %d, want 3600", man.CoveredSeconds)
	}
}

// The verification note is written for somebody who did not build this system. It has to
// name the file, state the expected digest, and — when the export is not continuous — say
// so in plain words rather than only in JSON.
func TestVerifyNoteStatesTheDigestAndAnyGaps(t *testing.T) {
	man := &ExportManifest{}
	man.Output.Sha256 = "deadbeef"
	man.CoveragePct = 66.7
	man.Gaps = []Gap{{From: testHourStart + 1200, To: testHourStart + 2400, Reason: "no-recording"}}

	note := verifyNote(man, "camera3.mp4")
	for _, want := range []string{"camera3.mp4", "deadbeef", "NOT CONTINUOUS", "no-recording", "sha256"} {
		if !strings.Contains(strings.ToLower(note), strings.ToLower(want)) {
			t.Errorf("the verification note should mention %q:\n%s", want, note)
		}
	}

	clean := &ExportManifest{Gaps: []Gap{}}
	clean.Output.Sha256 = "cafe"
	if n := verifyNote(clean, "x.mp4"); !strings.Contains(n, "fully covered") {
		t.Errorf("a complete export should say so plainly:\n%s", n)
	}
}
