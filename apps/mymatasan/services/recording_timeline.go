package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// Recording timeline: the read model behind continuous, wall-clock playback.
//
// The browser's job on this screen is to answer one question over and over — "what is on
// screen at moment T, across these cameras" — and the two halves of that answer must come
// from the same arithmetic. The SHADING says where footage exists; the SEEK says which
// file and offset a moment lives at. If those two disagree, the bar shows footage the
// player cannot reach, which reads to an operator as lost evidence.
//
// So both are computed here, from the same merged spans as recording_coverage.go's
// percentages. The coverage strip, the scrub bar and the player therefore cannot tell
// three different stories about the same hour.

// TimelineSegment is one playable file on the timeline.
//
// StartedAt/EndedAt are WALL CLOCK, and — this is what makes seeking work at all — media
// time inside the file runs 1:1 from StartedAt, because infra/recording.segmentEndedAt
// stores StartedAt plus the file's PROBED duration rather than the moment the recorder
// closed it. A segment truncated by an ffmpeg crash therefore reports the length it
// actually contains, so `at - StartedAt` is a real offset into the video and not an
// estimate that drifts by however long the stream was stalled.
type TimelineSegment struct {
	Id        int64  `json:"id"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   int64  `json:"endedAt"`
	Codec     string `json:"codec"`
	// AlertId > 0 marks an event clip rather than continuous footage. It is carried
	// because playback PREFERS continuous footage where both cover a moment — see
	// pickCovering — and because the bar draws the two differently.
	AlertId int64 `json:"alertId"`
}

// TimelineSpan is one continuous run of footage, after overlapping segments have been
// merged and everything has been clipped to the requested window.
type TimelineSpan struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

// CameraTimeline is one camera's footage over the requested window.
type CameraTimeline struct {
	CameraId int64             `json:"cameraId"`
	Segments []TimelineSegment `json:"segments"`
	Spans    []TimelineSpan    `json:"spans"`
	// CoveredSeconds/Percent are the same numbers /api/recording/coverage reports for
	// the same window, computed by the same merge — see coveredSpans.
	CoveredSeconds int64   `json:"coveredSeconds"`
	Percent        float64 `json:"percent"`
}

// TimelineReport is the whole scrub bar for one window.
type TimelineReport struct {
	From    int64            `json:"from"`
	To      int64            `json:"to"`
	Cameras []CameraTimeline `json:"cameras"`
}

// ErrTimelineTooManySegments is returned when a camera has more segments in the window
// than one timeline may carry.
//
// The alternative — return the newest N and stop — is the failure this programme keeps
// finding: GetSegments orders newest-first, so a truncated read silently drops the OLDEST
// segments and the left half of the scrub bar renders empty. Empty on a scrub bar does
// not read as "we did not fetch this"; it reads as "there is no footage here", which is a
// false statement about evidence. Refusing, with a range the caller can retry, is the only
// answer that cannot be misread.
var ErrTimelineTooManySegments = errors.New("too many segments in this range")

// timelineMaxSegments bounds one camera's segment list in one timeline request. At the
// UI's maximum 60-minute segment length this is years of footage; at the 1-minute minimum
// it is a bit over a week — which is why the error names the fix (a shorter window)
// rather than just failing.
const timelineMaxSegments = 12000

// Timeline returns each requested camera's footage over [from, to): the playable segment
// index, the merged spans the bar shades, and the coverage those spans amount to.
func (s *recordingService) Timeline(ctx context.Context, cameraIds []int64, from, to int64) (TimelineReport, error) {
	report := TimelineReport{From: from, To: to, Cameras: []CameraTimeline{}}
	if to <= from {
		return report, nil
	}
	for _, cameraId := range cameraIds {
		if cameraId <= 0 {
			continue
		}
		cam, err := s.cameraTimeline(ctx, cameraId, from, to)
		if err != nil {
			return TimelineReport{}, err
		}
		report.Cameras = append(report.Cameras, cam)
	}
	return report, nil
}

func (s *recordingService) cameraTimeline(ctx context.Context, cameraId, from, to int64) (CameraTimeline, error) {
	cam := CameraTimeline{CameraId: cameraId, Segments: []TimelineSegment{}, Spans: []TimelineSpan{}}

	// The same backwards slack coverage uses: GetSegments filters on StartedAt, so the
	// segment covering the first minutes of the window began before it and would
	// otherwise be missed — and on a scrub bar that absence is drawn as a gap.
	segs, total, err := s.GetSegments(ctx, timelineMaxSegments+1, 0, cameraId, 0, from-coverageLookbackSlack, to)
	if err != nil {
		return cam, err
	}
	if total > uint64(timelineMaxSegments) || len(segs) > timelineMaxSegments {
		return cam, fmt.Errorf("%w: camera %d has %d segments in this range (limit %d) — ask for a shorter window",
			ErrTimelineTooManySegments, cameraId, total, timelineMaxSegments)
	}

	for _, seg := range segs {
		if seg == nil {
			continue
		}
		// Drop rows the lookback pulled in that never reach the window. Keeping them
		// would put segments on the index that the bar has nowhere to draw, and the
		// player could seek to a moment the operator cannot see.
		end := seg.EndedAt
		if end <= 0 {
			end = to
		}
		if end <= from || seg.StartedAt >= to {
			continue
		}
		cam.Segments = append(cam.Segments, TimelineSegment{
			Id:        seg.Id,
			StartedAt: seg.StartedAt,
			EndedAt:   seg.EndedAt,
			Codec:     seg.Codec,
			AlertId:   seg.AlertId,
		})
	}
	// Oldest-first: the bar reads left to right, and the player advances by walking this
	// list forwards. GetSegments hands back newest-first, for the grid.
	reverseTimelineSegments(cam.Segments)

	spans, covered := coveredSpans(segs, from, to)
	for _, sp := range spans {
		cam.Spans = append(cam.Spans, TimelineSpan{From: sp.start, To: sp.end})
	}
	cam.CoveredSeconds = covered
	cam.Percent = round2(float64(covered) / float64(to-from) * 100)
	return cam, nil
}

func reverseTimelineSegments(in []TimelineSegment) {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
}

// TimelineSeek is where one wall-clock moment lives, for one camera.
type TimelineSeek struct {
	CameraId int64 `json:"cameraId"`
	// At is the moment that was asked for; ResolvedAt is the moment actually served,
	// which differs only when the request landed in a gap and was snapped forward.
	At         int64 `json:"at"`
	ResolvedAt int64 `json:"resolvedAt"`
	// Found is false when this camera has no footage at or after At — the honest answer
	// to "show me 3am on camera 4" when camera 4 was not recording and never came back.
	// A player that instead served the nearest footage in EITHER direction would put an
	// operator on the wrong side of an incident without saying so.
	Found         bool   `json:"found"`
	SegmentId     int64  `json:"segmentId"`
	OffsetSeconds int64  `json:"offsetSeconds"`
	SegmentEndsAt int64  `json:"segmentEndsAt"`
	Codec         string `json:"codec"`
	// Snapped/GapSeconds say that the moment asked for had no footage, and how much dead
	// air was skipped to reach some. The screen states this rather than silently
	// repositioning: "you asked for 02:14 and are watching 02:47" is the difference
	// between a camera that missed half an hour and a player that mis-seeked.
	Snapped    bool  `json:"snapped"`
	GapSeconds int64 `json:"gapSeconds"`
}

// SeekAt resolves one wall-clock moment to a playable (segment, offset) per camera.
//
// This is deliberately a server call rather than arithmetic the browser does over the
// index it already holds, for two reasons. It is the ONE place the "which file covers this
// moment" rule lives — the same pickCovering the object-search path uses, including its
// preference for continuous footage over a short event clip that overlaps it — so there is
// no second copy in JavaScript to drift out of step with it. And on an appliance under
// disk pressure the oldest footage is being evicted continuously, so an index the browser
// fetched a minute ago can name a segment that no longer exists; asking at seek time turns
// that into an honest "that footage is gone, the next is at 04:12".
func (s *recordingService) SeekAt(ctx context.Context, cameraIds []int64, at int64) ([]TimelineSeek, error) {
	out := make([]TimelineSeek, 0, len(cameraIds))
	for _, cameraId := range cameraIds {
		if cameraId <= 0 {
			continue
		}
		res, err := s.seekCamera(ctx, cameraId, at)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}

func (s *recordingService) seekCamera(ctx context.Context, cameraId, at int64) (TimelineSeek, error) {
	res := TimelineSeek{CameraId: cameraId, At: at}
	if at <= 0 {
		return res, nil
	}

	// Page back only as far as the nearest preceding CONTINUOUS segment, exactly as the
	// object-search path does. A fixed time lookback cannot do this job: segment length is
	// configurable from 1 to 60 minutes, so any constant is either far too small for a
	// long-segment install or wastefully large for a short-segment one — and a camera
	// recording nothing but 10-second event clips can bury the continuous segment that
	// actually covers the moment behind hundreds of rows.
	candidates := coveringSegmentCandidates(ctx, s, cameraId, at, at)
	if seg := pickCovering(candidates, at); seg != nil {
		res.Found = true
		res.ResolvedAt = at
		res.SegmentId = seg.Id
		res.OffsetSeconds = at - seg.StartedAt
		res.SegmentEndsAt = seg.EndedAt
		res.Codec = seg.Codec
		return res, nil
	}

	next, err := s.nextSegmentFrom(ctx, cameraId, at)
	if err != nil {
		return res, err
	}
	if next == nil {
		return res, nil
	}
	res.Found = true
	res.Snapped = true
	res.ResolvedAt = next.StartedAt
	res.GapSeconds = next.StartedAt - at
	res.SegmentId = next.Id
	res.OffsetSeconds = 0
	res.SegmentEndsAt = next.EndedAt
	res.Codec = next.Codec
	return res, nil
}

// nextSegmentFrom returns the earliest segment starting at or after `at`.
//
// Only reachable when nothing covers `at`, and that is what makes one row enough: a
// segment that started before `at` and covers anything after it necessarily covers `at`
// too, so it would already have been picked.
func (s *recordingService) nextSegmentFrom(ctx context.Context, cameraId, at int64) (*entities.RecordingSegment, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId},
		{FieldName: "StartedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: at},
	}
	sorters := []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.ASC}}
	rows, _, err := s.segments.Get(ctx, "", 1, 0, filters, sorters)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}
