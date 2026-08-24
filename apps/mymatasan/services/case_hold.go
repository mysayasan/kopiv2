package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// The footage hold: evidence in an open case outlives the retention policy.
//
// WHY THIS EXISTS. A case that points at footage the appliance is about to delete is not
// a case, it is a memo. Sites run seven, fourteen, thirty days of retention; an
// investigation routinely outlives all three, and the moment it does, every clip the case
// names quietly becomes a broken link. Worse, it does so silently — nothing about the case
// changes, the rows are all still there, and the operator discovers it when they open the
// export and find nothing in it.
//
// THE RULE, in one line: while a case is OPEN, footage overlapping any of its evidence is
// not deleted by retention, by the per-camera "Purge now" action, or by the disk-pressure
// sweeper. Closing the case, or removing the item, returns that footage to the policy it
// would have had anyway.
//
// THE FOUR PATHS. Footage leaves this appliance by four routes, and a hold honoured by
// three of them is not a hold:
//
//	services/recording.go  PurgeOldSegments     — the retention sweep (DB-driven)
//	services/recording.go  PurgeAllForCamera    — the per-camera "Purge now"
//	services/recording.go  PurgeOldestSegments  — the disk-pressure sweeper
//	infra/recording/rtsp   purgeOldFiles        — the recorder's own hourly FILE sweep
//
// The last one is the trap. It walks the live directory and deletes by filename age with
// no knowledge of the database at all, so a hold enforced only in the service layer is
// undone by a file sweeper an hour later, and the DB rows survive pointing at files that
// no longer exist. It is wired through a predicate on RecorderConfig for exactly this
// reason.
//
// SECURE WIPE AND FACTORY RESET DELIBERATELY IGNORE THE HOLD. Those are the documented
// "destroy everything on this appliance" operations, they are superadmin-only, they are
// audited, and a hold that could block them would turn a case into a way to make footage
// undeletable. Deleting a CAMERA likewise cascades through: the camera is gone, and
// keeping its footage alive under a hold nobody can find or release is worse than losing
// it. Both are stated here because "which destroyers does this apply to" is the first
// question anybody reviewing this asks.
//
// IT FAILS CLOSED. If the hold cannot be read — a database error, a read that came back
// truncated — every purge path treats the footage as held and deletes nothing. The
// alternative is a transient error being indistinguishable from "no case wants this",
// which is a data-loss bug that only shows up on the day it matters.

// footageHoldSlack widens the segment lookup around a held span for the same reason the
// coverage query does: GetSegments filters on StartedAt, so the segment that began before
// the span and runs into it — usually the one holding the first minutes of the evidence —
// would otherwise not be returned at all.
const footageHoldSlack = int64(4 * 3600)

// openCaseScanLimit caps how many open cases one hold read considers. It is far above any
// plausible working set; if a site ever exceeds it the read reports truncation and the
// guard fails closed rather than silently ignoring the overflow.
const openCaseScanLimit = uint64(500)

// FootageHold is one open case's claim on a span of one camera's footage.
type FootageHold struct {
	CaseId    int64  `json:"caseId"`
	CaseTitle string `json:"caseTitle"`
	ItemId    int64  `json:"itemId"`
	CameraId  int64  `json:"cameraId"`
	From      int64  `json:"from"`
	To        int64  `json:"to"`
}

// Covers reports whether this hold overlaps [from, to). Touching endpoints do not overlap:
// a segment that ends exactly when the evidence starts contains none of it.
func (h FootageHold) Covers(from, to int64) bool {
	if to <= from {
		// A zero-length probe is a point in time (the recorder's file sweep asks about one
		// instant when it cannot know a segment's end). Treat it as the instant it names.
		to = from + 1
	}
	return h.From < to && from < h.To
}

// CaseHoldSummary is what one case is keeping alive, for the case screen.
type CaseHoldSummary struct {
	// Items is how many pieces of evidence in this case hold footage.
	Items int `json:"items"`
	// Segments and Bytes are the recorded footage those items currently pin.
	Segments int   `json:"segments"`
	Bytes    int64 `json:"bytes"`
	// BeyondRetention counts the items whose footage has already outlived its camera's
	// retention window — the clips that exist ONLY because this case is open. It is the
	// number that makes closing the case a decision rather than a click: those are the
	// ones that go the moment the hold releases.
	BeyondRetention int `json:"beyondRetention"`
	// Missing counts items whose footage is already gone. Evidence added after the fact,
	// or footage destroyed by a wipe: either way the case must say so out loud.
	Missing int `json:"missing"`
}

// HoldsFor returns every open-case claim on one camera's footage.
//
// Callers MUST treat an error as "everything is held" — see the fail-closed note at the
// top of this file.
func (s *caseService) HoldsFor(ctx context.Context, cameraId int64) ([]FootageHold, error) {
	if cameraId <= 0 {
		return nil, nil
	}
	open, err := s.openCases(ctx)
	if err != nil {
		return nil, err
	}
	if len(open) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(open))
	for id := range open {
		ids = append(ids, id)
	}
	filters := []sqldataenums.Filter{
		{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId},
		{FieldName: "CaseId", Compare: sqldataenums.In, Value: ids},
	}
	sorters := []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.ASC}}
	var holds []FootageHold
	var offset uint64
	for {
		batch, _, err := s.items.Get(ctx, "", pageOrCap(0), offset, filters, sorters)
		if err != nil {
			return nil, err
		}
		for _, item := range batch {
			if !item.HoldsFootage() {
				continue
			}
			holds = append(holds, FootageHold{
				CaseId: item.CaseId, CaseTitle: open[item.CaseId],
				ItemId: item.Id, CameraId: item.CameraId,
				From: item.StartedAt, To: item.EndedAt,
			})
		}
		if uint64(len(batch)) < pageOrCap(0) {
			break
		}
		offset += uint64(len(batch))
	}
	return holds, nil
}

// AnyHolds reports whether any case is open at all. It is a short-circuit for the purge
// paths — no open case means no hold can exist, so they can skip the per-camera reads —
// and it is conservative in the safe direction: an error, or an open case with no footage
// in it, returns true and the per-camera check runs for real.
func (s *caseService) AnyHolds(ctx context.Context) bool {
	filters := []sqldataenums.Filter{
		{FieldName: "Status", Compare: sqldataenums.Equal, Value: entities.CaseStatusOpen},
	}
	rows, _, err := s.cases.Get(ctx, "", 1, 0, filters, nil)
	if err != nil {
		return true
	}
	return len(rows) > 0
}

// openCases reads the open cases as id → title.
func (s *caseService) openCases(ctx context.Context) (map[int64]string, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "Status", Compare: sqldataenums.Equal, Value: entities.CaseStatusOpen},
	}
	sorters := []sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}}
	out := map[int64]string{}
	var offset uint64
	for {
		batch, _, err := s.cases.Get(ctx, "", pageOrCap(0), offset, filters, sorters)
		if err != nil {
			return nil, err
		}
		for _, row := range batch {
			if row != nil {
				out[row.Id] = row.Title
			}
		}
		if uint64(len(batch)) < pageOrCap(0) {
			break
		}
		offset += uint64(len(batch))
		if offset >= openCaseScanLimit {
			// Refusing beats guessing: an unread page is a case whose evidence would be
			// deleted, and the caller's fail-closed handling turns this into "keep the
			// footage" instead of "lose it".
			return nil, fmt.Errorf("more than %d open cases — cannot determine what footage is held", openCaseScanLimit)
		}
	}
	return out, nil
}

// summarizeHold reports what one case is keeping alive, for the case screen.
func (s *caseService) summarizeHold(ctx context.Context, row *entities.CaseFile, items []*entities.CaseItem) CaseHoldSummary {
	var sum CaseHoldSummary
	if row == nil {
		return sum
	}
	now := s.now()
	// Retention is per camera, so the "only alive because of this case" test needs each
	// camera's own cutoff. Read once per camera rather than once per item.
	cutoffs := map[int64]int64{}
	for _, item := range items {
		if !item.HoldsFootage() {
			continue
		}
		sum.Items++
		if _, seen := cutoffs[item.CameraId]; !seen {
			cutoffs[item.CameraId] = s.retentionCutoff(ctx, item.CameraId, now)
		}
		segs := s.segmentsOverlapping(ctx, item.CameraId, item.StartedAt, item.EndedAt)
		if len(segs) == 0 {
			sum.Missing++
		}
		for _, seg := range segs {
			sum.Segments++
			sum.Bytes += seg.FileSize
		}
		// Beyond retention is asked of the EVIDENCE span, not of the segments: a clip whose
		// footage has already been deleted is Missing, not held, and counting it here would
		// promise the operator something they no longer have.
		if cut := cutoffs[item.CameraId]; cut > 0 && len(segs) > 0 && item.EndedAt < cut {
			sum.BeyondRetention++
		}
	}
	return sum
}

// retentionCutoff is the moment before which this camera's footage would normally have
// been deleted. 0 when the camera keeps footage forever or has no config to read.
func (s *caseService) retentionCutoff(ctx context.Context, cameraId, now int64) int64 {
	if s.recording == nil {
		return 0
	}
	cfg, err := s.recording.GetConfig(ctx, cameraId)
	if err != nil || cfg == nil || cfg.RetentionDays <= 0 {
		return 0
	}
	return now - int64(cfg.RetentionDays)*24*3600
}

func (s *caseService) segmentsOverlapping(ctx context.Context, cameraId, from, to int64) []*entities.RecordingSegment {
	if s.recording == nil || cameraId <= 0 || to <= from {
		return nil
	}
	segs, _, err := s.recording.GetSegments(ctx, 200, 0, cameraId, 0, from-footageHoldSlack, to)
	if err != nil {
		return nil
	}
	var out []*entities.RecordingSegment
	for _, seg := range segs {
		if seg == nil {
			continue
		}
		end := seg.EndedAt
		if end <= 0 {
			// Still recording: it covers everything from its start to now.
			end = s.now()
		}
		if end <= from || seg.StartedAt >= to {
			continue
		}
		out = append(out, seg)
	}
	return out
}

// --- the guard every deletion path goes through ------------------------------

// FootageGuard is the one place a purge asks "may I delete this?".
//
// It is a small struct rather than a bare function so that the fail-closed behaviour and
// the reason string live together: a purge that skips footage must be able to SAY which
// case stopped it, or the operator sees a purge that reports fewer deletions than expected
// and has no way to find out why.
type FootageGuard struct {
	mu    sync.RWMutex
	cases ICaseService
	logf  func(format string, args ...any)
}

// NewFootageGuard wires the guard. A nil cases service yields a guard that blocks nothing,
// which is what an install without the case feature (or a unit test that is not about
// holds) should get.
func NewFootageGuard(cases ICaseService, logf func(format string, args ...any)) *FootageGuard {
	return &FootageGuard{cases: cases, logf: logf}
}

// SetCases fills in the case service after construction. The composition root builds the
// guard first and hands it to everything that deletes footage, then builds the case
// service (which needs the recording service) and closes the loop here. Written as a
// setter rather than resolved with a type assertion so a mis-wiring is a nil guard that
// blocks nothing loudly at boot, not a silently unmatched interface — that exact
// assertion trick has already dropped a metric in this codebase once.
func (g *FootageGuard) SetCases(cases ICaseService) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cases = cases
}

func (g *FootageGuard) casesService() ICaseService {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.cases
}

// CameraHolds loads one camera's holds once, so a purge loop over hundreds of segments
// makes one query rather than one per segment. The returned checker fails closed: if the
// read failed, every segment is reported held.
func (g *FootageGuard) CameraHolds(ctx context.Context, cameraId int64) *HeldSpans {
	cases := g.casesService()
	if cases == nil || cameraId <= 0 {
		return &HeldSpans{}
	}
	if !cases.AnyHolds(ctx) {
		return &HeldSpans{}
	}
	holds, err := cases.HoldsFor(ctx, cameraId)
	if err != nil {
		if g.logf != nil {
			g.logf("case hold lookup failed for camera %d, keeping its footage: %v", cameraId, err)
		}
		return &HeldSpans{unreadable: true}
	}
	return &HeldSpans{holds: holds}
}

// HeldSpans is one camera's holds, resolved once and asked many times.
type HeldSpans struct {
	holds []FootageHold
	// unreadable means the hold could not be determined. Everything is held.
	unreadable bool
}

// Any reports whether anything at all is held (or unknown), so a caller can skip the
// per-segment questions entirely.
func (h *HeldSpans) Any() bool {
	return h != nil && (h.unreadable || len(h.holds) > 0)
}

// Blocked reports whether footage spanning [from, to) must be kept, and why.
func (h *HeldSpans) Blocked(from, to int64) (bool, string) {
	if h == nil {
		return false, ""
	}
	if h.unreadable {
		return true, "the open cases could not be read"
	}
	for _, hold := range h.holds {
		if hold.Covers(from, to) {
			title := strings.TrimSpace(hold.CaseTitle)
			if title == "" {
				title = fmt.Sprintf("case %d", hold.CaseId)
			}
			return true, fmt.Sprintf("held by %s", title)
		}
	}
	return false, ""
}

// BlockedSegment is the segment-shaped form of Blocked. A segment still being written
// (EndedAt 0) is treated as running to now, the same way the coverage maths treats it.
func (h *HeldSpans) BlockedSegment(seg *entities.RecordingSegment) (bool, string) {
	if seg == nil {
		return false, ""
	}
	end := seg.EndedAt
	if end <= 0 {
		end = time.Now().UTC().Unix()
	}
	return h.Blocked(seg.StartedAt, end)
}

// PurgeFootageResult is what one hold-honouring purge did. Kept > 0 is not a failure: it
// is the purge working, and the count plus the reason are what the operator is shown so
// "it says it purged, and the footage is still there" has an answer on the screen.
type PurgeFootageResult struct {
	Deleted int    `json:"deleted"`
	Kept    int    `json:"kept"`
	Reason  string `json:"reason,omitempty"`
}

// Predicate is the hold in the shape infra/recording's file sweeper needs: it knows a
// camera and a segment's span, and nothing else. It opens its own short-lived context
// because it is called from a background ticker with no request behind it.
//
// The sweeper asks about every file in a camera's live directory in one pass, so the
// camera's holds are memoized for predicateCacheTTL — long enough to serve one sweep with
// a single query, short enough that evidence added to a case is protected within seconds
// rather than at the next hourly tick. The failure it cannot avoid — a case created in the
// same instant the sweeper is deleting that exact file — is inherent to a background
// deleter and is why the DB-side purge (which reads holds fresh) is the primary defence.
func (g *FootageGuard) Predicate() func(cameraId, startedAt, endedAt int64) bool {
	if g == nil {
		return nil
	}
	type entry struct {
		spans *HeldSpans
		at    time.Time
	}
	var mu sync.Mutex
	cache := map[int64]entry{}
	return func(cameraId, startedAt, endedAt int64) bool {
		mu.Lock()
		got, ok := cache[cameraId]
		if !ok || time.Since(got.at) > predicateCacheTTL {
			mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			spans := g.CameraHolds(ctx, cameraId)
			cancel()
			got = entry{spans: spans, at: time.Now()}
			mu.Lock()
			cache[cameraId] = got
		}
		spans := got.spans
		mu.Unlock()
		blocked, _ := spans.Blocked(startedAt, endedAt)
		return blocked
	}
}

// predicateCacheTTL is how long the file sweeper's view of a camera's holds may be stale.
const predicateCacheTTL = 30 * time.Second
