package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// timelineSegmentRepo is an in-memory stand-in for the segment table that honours the
// filters, sorters, limit and offset the timeline actually builds.
//
// It evaluates the query rather than returning a canned slice on purpose. The
// interesting failures in this file are all in query CONSTRUCTION — a `startedBefore`
// bound that excludes the segment starting exactly at the moment asked for, a
// next-segment lookup that inherits GetSegments' newest-first order and returns the
// furthest future segment instead of the nearest one — and a canned-slice fake passes
// every one of them.
type timelineSegmentRepo struct {
	dbsql.IGenericRepo[entities.RecordingSegment]
	rows []*entities.RecordingSegment
	// gets counts reads, so a test can assert the candidate pager stops rather than
	// walking a camera's whole history.
	gets int
}

func (f *timelineSegmentRepo) Get(_ context.Context, _ string, limit, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.RecordingSegment, uint64, error) {
	f.gets++
	var matched []*entities.RecordingSegment
	for _, row := range f.rows {
		if segmentMatchesFilters(row, filters) {
			matched = append(matched, row)
		}
	}
	desc := false
	for _, s := range sorters {
		if s.FieldName == "StartedAt" && s.Sort == sqldataenums.DESC {
			desc = true
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if desc {
			return matched[i].StartedAt > matched[j].StartedAt
		}
		return matched[i].StartedAt < matched[j].StartedAt
	})
	total := uint64(len(matched))
	if offset >= total {
		return nil, total, nil
	}
	page := matched[offset:]
	if limit > 0 && uint64(len(page)) > limit {
		page = page[:limit]
	}
	return page, total, nil
}

func segmentMatchesFilters(row *entities.RecordingSegment, filters []sqldataenums.Filter) bool {
	for _, f := range filters {
		var have int64
		switch f.FieldName {
		case "CameraId":
			have = row.CameraId
		case "AlertId":
			have = row.AlertId
		case "StartedAt":
			have = row.StartedAt
		default:
			panic(fmt.Sprintf("timelineSegmentRepo: unhandled filter field %q", f.FieldName))
		}
		want, ok := f.Value.(int64)
		if !ok {
			panic(fmt.Sprintf("timelineSegmentRepo: filter %q value is %T, want int64", f.FieldName, f.Value))
		}
		switch f.Compare {
		case sqldataenums.Equal:
			if have != want {
				return false
			}
		case sqldataenums.GreaterThanOrEqualTo:
			if have < want {
				return false
			}
		case sqldataenums.LessThan:
			if have >= want {
				return false
			}
		default:
			panic(fmt.Sprintf("timelineSegmentRepo: unhandled compare %d", f.Compare))
		}
	}
	return true
}

// The day under test: 2026-08-19 14:00 UTC onwards.
const tlBase = int64(1755612000)

func tlSeg(id, start, end int64) *entities.RecordingSegment {
	return &entities.RecordingSegment{Id: id, CameraId: 3, StartedAt: start, EndedAt: end, Codec: "h264"}
}

func tlClip(id, start, end, alertId int64) *entities.RecordingSegment {
	s := tlSeg(id, start, end)
	s.AlertId = alertId
	return s
}

func newTimelineService(rows ...*entities.RecordingSegment) (*recordingService, *timelineSegmentRepo) {
	repo := &timelineSegmentRepo{rows: rows}
	return &recordingService{segments: repo}, repo
}

func TestTimelineMergesOverlappingSegmentsIntoOneSpan(t *testing.T) {
	// A recorder restart re-opens a segment overlapping the previous one. The bar must
	// draw ONE continuous run — two abutting spans with a hairline between them reads as
	// a dropout that never happened.
	svc, _ := newTimelineService(
		tlSeg(1, tlBase, tlBase+600),
		tlSeg(2, tlBase+540, tlBase+1200),
	)
	rep, err := svc.Timeline(context.Background(), []int64{3}, tlBase, tlBase+1800)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	cam := rep.Cameras[0]
	if len(cam.Spans) != 1 {
		t.Fatalf("spans = %+v, want one merged span", cam.Spans)
	}
	if cam.Spans[0].From != tlBase || cam.Spans[0].To != tlBase+1200 {
		t.Fatalf("span = %+v, want [%d,%d)", cam.Spans[0], tlBase, tlBase+1200)
	}
	if cam.CoveredSeconds != 1200 {
		t.Fatalf("coveredSeconds = %d, want 1200 (overlap counted once)", cam.CoveredSeconds)
	}
}

func TestTimelineShowsTheGapBetweenSpans(t *testing.T) {
	svc, _ := newTimelineService(
		tlSeg(1, tlBase, tlBase+600),
		tlSeg(2, tlBase+1200, tlBase+1800),
	)
	rep, err := svc.Timeline(context.Background(), []int64{3}, tlBase, tlBase+1800)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	cam := rep.Cameras[0]
	if len(cam.Spans) != 2 {
		t.Fatalf("spans = %+v, want two", cam.Spans)
	}
	if cam.Spans[0].To != tlBase+600 || cam.Spans[1].From != tlBase+1200 {
		t.Fatalf("spans = %+v, want a hole from %d to %d", cam.Spans, tlBase+600, tlBase+1200)
	}
	if cam.Percent != 66.67 {
		t.Fatalf("percent = %v, want 66.67", cam.Percent)
	}
}

func TestTimelineIncludesTheSegmentThatStartedBeforeTheWindow(t *testing.T) {
	// The segment covering the first minutes of the window began before it. GetSegments
	// filters on StartedAt, so without the lookback slack this segment is missed — and a
	// scrub bar draws that absence as a gap, i.e. as missing evidence.
	svc, _ := newTimelineService(tlSeg(1, tlBase-300, tlBase+300))
	rep, err := svc.Timeline(context.Background(), []int64{3}, tlBase, tlBase+600)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	cam := rep.Cameras[0]
	if len(cam.Segments) != 1 || cam.Segments[0].Id != 1 {
		t.Fatalf("segments = %+v, want the segment that straddles the window start", cam.Segments)
	}
	if len(cam.Spans) != 1 || cam.Spans[0].From != tlBase {
		t.Fatalf("spans = %+v, want one clipped to the window start", cam.Spans)
	}
}

func TestTimelineDropsSegmentsThatNeverReachTheWindow(t *testing.T) {
	// The lookback pulls in an hour of history. A segment that ended before the window
	// must not reach the index: the bar has nowhere to draw it, and the player would be
	// able to seek to a moment the operator cannot see on screen.
	svc, _ := newTimelineService(
		tlSeg(1, tlBase-3000, tlBase-2400),
		tlSeg(2, tlBase+60, tlBase+120),
	)
	rep, err := svc.Timeline(context.Background(), []int64{3}, tlBase, tlBase+600)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	cam := rep.Cameras[0]
	if len(cam.Segments) != 1 || cam.Segments[0].Id != 2 {
		t.Fatalf("segments = %+v, want only the in-window segment", cam.Segments)
	}
}

func TestTimelineReturnsSegmentsOldestFirst(t *testing.T) {
	// GetSegments serves the grid, newest-first. The bar reads left to right and the
	// player advances by walking this list forwards, so the timeline must reverse it.
	svc, _ := newTimelineService(
		tlSeg(1, tlBase, tlBase+300),
		tlSeg(2, tlBase+300, tlBase+600),
		tlSeg(3, tlBase+600, tlBase+900),
	)
	rep, err := svc.Timeline(context.Background(), []int64{3}, tlBase, tlBase+900)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	got := rep.Cameras[0].Segments
	for i := 1; i < len(got); i++ {
		if got[i].StartedAt < got[i-1].StartedAt {
			t.Fatalf("segments are not oldest-first: %+v", got)
		}
	}
}

func TestTimelineRefusesRatherThanDrawingATruncatedBar(t *testing.T) {
	// GetSegments orders newest-first, so a capped read silently drops the OLDEST
	// segments and the LEFT of the bar renders empty. Empty reads as "no footage here",
	// which is a false statement about evidence — so the request is refused instead.
	rows := make([]*entities.RecordingSegment, 0, timelineMaxSegments+1)
	for i := 0; i <= timelineMaxSegments; i++ {
		start := tlBase + int64(i)*60
		rows = append(rows, tlSeg(int64(i+1), start, start+60))
	}
	svc, _ := newTimelineService(rows...)
	_, err := svc.Timeline(context.Background(), []int64{3}, tlBase, tlBase+int64(len(rows))*60)
	if !errors.Is(err, ErrTimelineTooManySegments) {
		t.Fatalf("err = %v, want ErrTimelineTooManySegments", err)
	}
}

func TestTimelineAndCoverageAgreeOnTheSameWindow(t *testing.T) {
	// The bar and the coverage report are two renderings of one fact. If they can
	// disagree, one of them is lying about whether footage exists.
	rows := []*entities.RecordingSegment{
		tlSeg(1, tlBase, tlBase+900),
		tlSeg(2, tlBase+800, tlBase+1500),
		tlSeg(3, tlBase+2400, tlBase+3600),
	}
	svc, _ := newTimelineService(rows...)
	tl, err := svc.Timeline(context.Background(), []int64{3}, tlBase, tlBase+3600)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	cov, err := svc.Coverage(context.Background(), 3, tlBase, tlBase+3600, "hour")
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if tl.Cameras[0].Percent != cov.OverallPercent {
		t.Fatalf("timeline %v%% vs coverage %v%% for the same window", tl.Cameras[0].Percent, cov.OverallPercent)
	}
	var spanTotal int64
	for _, sp := range tl.Cameras[0].Spans {
		spanTotal += sp.To - sp.From
	}
	if spanTotal != tl.Cameras[0].CoveredSeconds {
		t.Fatalf("shaded %ds but claimed %ds covered", spanTotal, tl.Cameras[0].CoveredSeconds)
	}
}

func TestTimelineKeepsCamerasInTheOrderAsked(t *testing.T) {
	svc, _ := newTimelineService(tlSeg(1, tlBase, tlBase+600))
	rep, err := svc.Timeline(context.Background(), []int64{7, 3, 5}, tlBase, tlBase+600)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(rep.Cameras) != 3 {
		t.Fatalf("cameras = %d, want 3", len(rep.Cameras))
	}
	for i, want := range []int64{7, 3, 5} {
		if rep.Cameras[i].CameraId != want {
			t.Fatalf("camera[%d] = %d, want %d", i, rep.Cameras[i].CameraId, want)
		}
	}
}

func TestSeekResolvesAMomentInsideASegment(t *testing.T) {
	svc, _ := newTimelineService(tlSeg(11, tlBase, tlBase+900))
	got, err := svc.SeekAt(context.Background(), []int64{3}, tlBase+450)
	if err != nil {
		t.Fatalf("SeekAt: %v", err)
	}
	res := got[0]
	if !res.Found || res.SegmentId != 11 {
		t.Fatalf("resolved to %+v, want segment 11", res)
	}
	if res.OffsetSeconds != 450 {
		t.Fatalf("offset = %d, want 450 seconds into the file", res.OffsetSeconds)
	}
	if res.Snapped || res.ResolvedAt != tlBase+450 {
		t.Fatalf("%+v: a moment with footage must not be snapped", res)
	}
}

func TestSeekResolvesTheFirstInstantOfASegment(t *testing.T) {
	// The candidate query bounds StartedAt with a LessThan, so the segment beginning at
	// exactly the moment asked for is only reachable if that bound is at+1. Off by one
	// here and every scrub landing on a segment boundary snaps forward to the NEXT
	// segment instead of playing the one under the cursor.
	svc, _ := newTimelineService(
		tlSeg(11, tlBase, tlBase+900),
		tlSeg(12, tlBase+900, tlBase+1800),
	)
	got, err := svc.SeekAt(context.Background(), []int64{3}, tlBase+900)
	if err != nil {
		t.Fatalf("SeekAt: %v", err)
	}
	if got[0].SegmentId != 12 || got[0].OffsetSeconds != 0 || got[0].Snapped {
		t.Fatalf("resolved to %+v, want segment 12 at offset 0, unsnapped", got[0])
	}
}

func TestSeekSnapsForwardOutOfAGapAndSaysHowFar(t *testing.T) {
	// Silently repositioning is the failure here. "You asked for 14:10 and are watching
	// 14:20" is the difference between a camera that missed ten minutes and a player
	// that mis-seeked, and only the first is worth an operator's attention.
	svc, _ := newTimelineService(
		tlSeg(11, tlBase, tlBase+300),
		tlSeg(12, tlBase+900, tlBase+1200),
	)
	got, err := svc.SeekAt(context.Background(), []int64{3}, tlBase+600)
	if err != nil {
		t.Fatalf("SeekAt: %v", err)
	}
	res := got[0]
	if !res.Found || !res.Snapped {
		t.Fatalf("%+v: want a snapped resolution", res)
	}
	if res.SegmentId != 12 || res.ResolvedAt != tlBase+900 || res.OffsetSeconds != 0 {
		t.Fatalf("%+v: want segment 12 from its start", res)
	}
	if res.GapSeconds != 300 {
		t.Fatalf("gapSeconds = %d, want 300", res.GapSeconds)
	}
}

func TestSeekSnapsToTheNEARESTFutureSegment(t *testing.T) {
	// GetSegments sorts newest-first. A next-segment lookup that inherits that order
	// returns the LAST segment of the day instead of the next one, so clicking a gap
	// throws the operator hours forward — and it still looks like it worked.
	svc, _ := newTimelineService(
		tlSeg(11, tlBase, tlBase+300),
		tlSeg(12, tlBase+900, tlBase+1200),
		tlSeg(13, tlBase+7200, tlBase+7500),
		tlSeg(14, tlBase+36000, tlBase+36300),
	)
	got, err := svc.SeekAt(context.Background(), []int64{3}, tlBase+600)
	if err != nil {
		t.Fatalf("SeekAt: %v", err)
	}
	if got[0].SegmentId != 12 {
		t.Fatalf("snapped to segment %d, want 12 (the nearest future segment)", got[0].SegmentId)
	}
}

func TestSeekReportsNoFootageRatherThanReachingBackwards(t *testing.T) {
	// There IS footage on this camera — an hour earlier. Serving it would put an
	// investigator on the wrong side of the incident while the player looked healthy.
	svc, _ := newTimelineService(tlSeg(11, tlBase, tlBase+300))
	got, err := svc.SeekAt(context.Background(), []int64{3}, tlBase+3600)
	if err != nil {
		t.Fatalf("SeekAt: %v", err)
	}
	if got[0].Found {
		t.Fatalf("%+v: want found=false, nothing at or after that moment", got[0])
	}
}

func TestSeekPrefersContinuousFootageOverAnOverlappingEventClip(t *testing.T) {
	// Both cover the moment. Playing the 20-second clip means playback stops dead 20
	// seconds later and jumps, in the middle of a scrub through continuous footage.
	svc, _ := newTimelineService(
		tlSeg(11, tlBase, tlBase+900),
		tlClip(12, tlBase+440, tlBase+460, 99),
	)
	got, err := svc.SeekAt(context.Background(), []int64{3}, tlBase+450)
	if err != nil {
		t.Fatalf("SeekAt: %v", err)
	}
	if got[0].SegmentId != 11 {
		t.Fatalf("resolved to segment %d, want the continuous segment 11", got[0].SegmentId)
	}
	if got[0].OffsetSeconds != 450 {
		t.Fatalf("offset = %d, want 450 (into the continuous file, not the clip)", got[0].OffsetSeconds)
	}
}

func TestSeekFindsContinuousFootageBuriedUnderManyEventClips(t *testing.T) {
	// A busy camera writes hundreds of short event clips over the continuous segment
	// covering the moment. A fixed-size single page of candidates would return only
	// clips and conclude there is no footage; the pager keeps reading until it reaches a
	// continuous segment that starts at or before the moment.
	rows := []*entities.RecordingSegment{tlSeg(1, tlBase, tlBase+3600)}
	for i := 0; i < 250; i++ {
		start := tlBase + 1000 + int64(i)
		rows = append(rows, tlClip(int64(100+i), start, start+1, int64(500+i)))
	}
	svc, repo := newTimelineService(rows...)
	got, err := svc.SeekAt(context.Background(), []int64{3}, tlBase+1800)
	if err != nil {
		t.Fatalf("SeekAt: %v", err)
	}
	if got[0].SegmentId != 1 {
		t.Fatalf("resolved to segment %d, want the continuous segment 1", got[0].SegmentId)
	}
	if repo.gets > 10 {
		t.Fatalf("%d reads to resolve one moment — the pager is not stopping", repo.gets)
	}
}

func TestSeekResolvesEveryCameraInOneCall(t *testing.T) {
	// Multi-camera sync scrubs all tiles to one moment. One camera having a hole there
	// must not stop the others resolving — that hole is exactly what the operator is
	// looking at the wall to find.
	repo := &timelineSegmentRepo{rows: []*entities.RecordingSegment{
		{Id: 1, CameraId: 3, StartedAt: tlBase, EndedAt: tlBase + 900, Codec: "h264"},
		{Id: 2, CameraId: 4, StartedAt: tlBase + 3600, EndedAt: tlBase + 4500, Codec: "h264"},
	}}
	svc := &recordingService{segments: repo}
	got, err := svc.SeekAt(context.Background(), []int64{3, 4}, tlBase+450)
	if err != nil {
		t.Fatalf("SeekAt: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want one per camera", len(got))
	}
	if !got[0].Found || got[0].Snapped {
		t.Fatalf("camera 3: %+v, want footage at the moment asked for", got[0])
	}
	if !got[1].Found || !got[1].Snapped || got[1].GapSeconds != 3150 {
		t.Fatalf("camera 4: %+v, want a snap forward of 3150s", got[1])
	}
}
