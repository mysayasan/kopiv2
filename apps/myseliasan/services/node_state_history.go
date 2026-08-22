package services

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Watermarks the history keeps in control_setting. Rows, not memory, because the point
// of the sweep watermark is to survive the process NOT shutting down cleanly.
const (
	monitorSweepKey = "monitor.lastSweepAt"
	monitorPruneKey = "monitor.lastPruneAt"
)

const (
	// stateHistoryRetention is how far back history is kept. A year plus slack, so a
	// twelve-month SLA report always has a complete year behind it and an operator who
	// runs one on the last day of the period is not quietly served eleven months.
	stateHistoryRetention = 400 * 24 * time.Hour
	// stateHistoryPruneEvery bounds how often pruning runs. The sweep is on a
	// heartbeat-interval ticker; deleting year-old rows every 60 seconds would be pure
	// waste on a fleet that writes a handful of rows a week.
	stateHistoryPruneEvery = 24 * time.Hour
	// stateEventPageSize pages the window query.
	stateEventPageSize = 1000
	// stateEventMaxRows caps how many events one node can contribute to one report. A
	// node flapping at the grace boundary for a year is the only way to approach it;
	// the cap keeps a single sick node from turning a report into an OOM.
	stateEventMaxRows = 20000
)

// INodeStateHistory records what each node's liveness DID, so availability can be
// reported over a past window rather than only observed in the present.
//
// It is deliberately not part of INodeRegistry. The registry answers "what is the fleet
// now" and is consulted on every page load; this answers "what was the fleet" and is
// consulted when somebody generates a report. Keeping them apart is also what lets the
// registry's existing tests keep their in-memory repos with no history at all.
type INodeStateHistory interface {
	// Observe reports a node's CURRENT state. It writes only when that differs from the
	// last recorded state (or when the node has no history yet), so callers may — and
	// should — call it unconditionally on every observation rather than trying to work
	// out for themselves whether something changed. That is the whole reason the
	// transition test lives here: the four call sites that change node status are in
	// three different files and would each have had to get it right.
	Observe(ctx context.Context, nodeID, state, reason string, at int64) error
	// Forget deletes a node's history. Called when a node is RELEASED, because NodeId
	// is stable per appliance: without this, re-adopting the same box inherits its old
	// history, and the release-to-re-adoption interval — during which the node was not
	// in the fleet at all — is reported as an outage.
	Forget(ctx context.Context, nodeID string) error
	// NoteSweep stamps the monitoring watermark and, when the previous stamp is older
	// than maxGap, records the span in between as unmonitored. Call once per sweep,
	// BEFORE reconciling, so the gap is closed before this sweep's observations land.
	NoteSweep(ctx context.Context, now, maxGap int64) error
	// Prune drops events and gaps older than the retention window. Self-throttling.
	Prune(ctx context.Context, now int64) error
	// Events returns one node's transitions overlapping [from,to] — including the last
	// event BEFORE from, which is what establishes the state the window opened in. A
	// window that only sees events inside itself cannot tell a node that has been down
	// all month from one with no history at all.
	Events(ctx context.Context, nodeID string, from, to int64) ([]*entities.NodeStateEvent, error)
	// Gaps returns monitoring gaps overlapping [from,to].
	Gaps(ctx context.Context, from, to int64) ([]*entities.FleetMonitorGap, error)
}

type nodeStateHistory struct {
	events   dbsql.IGenericRepo[entities.NodeStateEvent]
	gaps     dbsql.IGenericRepo[entities.FleetMonitorGap]
	settings dbsql.IGenericRepo[entities.ControlSetting]

	// mu serialises the read-modify-write in Observe. The heartbeat sweep is
	// leader-gated and therefore single, but AcceptControlConn runs on the control
	// server's accept path and MarkSelfDropped on an HTTP handler, so two goroutines
	// genuinely can observe the same node at the same moment. Without the lock both
	// read "last state = lost", both decide it is a transition, and the node's history
	// gains two identical recoveries — which is not merely untidy: the second one ends
	// a zero-length span and the outage count is wrong for the rest of the year.
	mu sync.Mutex
	// last caches the newest recorded state per node so a steady fleet does not query
	// once per node per sweep. Populated lazily from the database, so a restart or a
	// leadership handover simply re-reads it.
	last map[string]string
}

// NewNodeStateHistory builds the history over the control-plane database.
func NewNodeStateHistory(db dbsql.IDbCrud) INodeStateHistory {
	return newNodeStateHistory(
		dbsql.NewGenericRepo[entities.NodeStateEvent](db),
		dbsql.NewGenericRepo[entities.FleetMonitorGap](db),
		dbsql.NewGenericRepo[entities.ControlSetting](db),
	)
}

// newNodeStateHistory is the repo-injecting constructor used by tests.
func newNodeStateHistory(
	events dbsql.IGenericRepo[entities.NodeStateEvent],
	gaps dbsql.IGenericRepo[entities.FleetMonitorGap],
	settings dbsql.IGenericRepo[entities.ControlSetting],
) *nodeStateHistory {
	return &nodeStateHistory{events: events, gaps: gaps, settings: settings, last: map[string]string{}}
}

func (h *nodeStateHistory) Observe(ctx context.Context, nodeID, state, reason string, at int64) error {
	if nodeID == "" || state == "" {
		return nil
	}
	if at <= 0 {
		at = time.Now().Unix()
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	prev, cached := h.last[nodeID]
	if !cached {
		row, err := h.latest(ctx, nodeID)
		if err != nil {
			return err
		}
		if row != nil {
			prev = row.State
			h.last[nodeID] = prev
			cached = true
		}
	}
	if cached && prev == state {
		return nil
	}

	ev := entities.NodeStateEvent{
		NodeId:    nodeID,
		State:     state,
		PrevState: prev,
		At:        at,
		Reason:    reason,
	}
	if !cached {
		// No history at all: this is where this node's measurable life begins. The
		// reason is overridden rather than trusted, because the caller has no idea
		// whether it is the first observation — and a row labelled "heartbeat" that is
		// really the start of the record would make an upgraded fleet look as though it
		// had been measured all along.
		ev.PrevState = ""
		ev.Reason = entities.NodeStateReasonBaseline
	}
	if _, err := h.events.Create(ctx, "", ev); err != nil {
		return err
	}
	h.last[nodeID] = state
	return nil
}

// latest returns the newest recorded event for a node, or nil when it has none.
func (h *nodeStateHistory) latest(ctx context.Context, nodeID string) (*entities.NodeStateEvent, error) {
	rows, _, err := h.events.Get(ctx, "", 1, 0,
		[]sqldataenums.Filter{{FieldName: "NodeId", Compare: sqldataenums.Equal, Value: nodeID}},
		[]sqldataenums.Sorter{{FieldName: "At", Sort: sqldataenums.DESC}, {FieldName: "Id", Sort: sqldataenums.DESC}})
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (h *nodeStateHistory) Forget(ctx context.Context, nodeID string) error {
	if nodeID == "" {
		return nil
	}
	h.mu.Lock()
	delete(h.last, nodeID)
	h.mu.Unlock()
	_, err := h.events.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "NodeId", Compare: sqldataenums.Equal, Value: nodeID}})
	if err != nil && !isNoResultErr(err) {
		return err
	}
	return nil
}

func (h *nodeStateHistory) NoteSweep(ctx context.Context, now, maxGap int64) error {
	if now <= 0 {
		now = time.Now().Unix()
	}
	if maxGap <= 0 {
		maxGap = 90
	}
	prev, err := h.watermark(ctx, monitorSweepKey)
	if err != nil {
		return err
	}
	// First sweep ever (or after a restore that dropped the watermark): there is no
	// span to claim. Time before the first watermark is already unmonitored by
	// construction — no node has any history before its first event either.
	if prev > 0 && now-prev > maxGap {
		if _, err := h.gaps.Create(ctx, "", entities.FleetMonitorGap{
			StartedAt: prev,
			EndedAt:   now,
			Reason:    "control plane was not monitoring the fleet",
		}); err != nil {
			return err
		}
	}
	return h.setWatermark(ctx, monitorSweepKey, now)
}

func (h *nodeStateHistory) Prune(ctx context.Context, now int64) error {
	if now <= 0 {
		now = time.Now().Unix()
	}
	last, err := h.watermark(ctx, monitorPruneKey)
	if err != nil {
		return err
	}
	if last > 0 && now-last < int64(stateHistoryPruneEvery.Seconds()) {
		return nil
	}
	cutoff := now - int64(stateHistoryRetention.Seconds())

	// A NODE'S NEWEST EVENT IS NEVER PRUNED, however old it is. This is the whole
	// subtlety of pruning this table: a node that has been online without interruption
	// for two years holds exactly ONE row, and a plain "delete everything older than
	// the cutoff" erases it — turning the best-behaved appliance in the fleet into one
	// with no history, reported as unmonitored until it next changes state (which, being
	// healthy, it does not). The row is not stale data; it is the only thing asserting
	// the node is up at all.
	old, _, err := h.events.Get(ctx, "", stateEventPageSize, 0,
		[]sqldataenums.Filter{{FieldName: "At", Compare: sqldataenums.LessThan, Value: cutoff}},
		[]sqldataenums.Sorter{{FieldName: "At", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return err
	}
	newestID := map[string]int64{}
	for _, ev := range old {
		if _, seen := newestID[ev.NodeId]; seen {
			continue
		}
		latest, lerr := h.latest(ctx, ev.NodeId)
		if lerr != nil {
			return lerr
		}
		if latest != nil {
			newestID[ev.NodeId] = latest.Id
		}
	}
	for _, ev := range old {
		if newestID[ev.NodeId] == ev.Id {
			continue
		}
		if _, derr := h.events.DeleteById(ctx, "", uint64(ev.Id)); derr != nil && !isNoResultErr(derr) {
			return derr
		}
	}
	if _, err := h.gaps.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "EndedAt", Compare: sqldataenums.LessThan, Value: cutoff}}); err != nil && !isNoResultErr(err) {
		return err
	}
	return h.setWatermark(ctx, monitorPruneKey, now)
}

func (h *nodeStateHistory) Events(ctx context.Context, nodeID string, from, to int64) ([]*entities.NodeStateEvent, error) {
	if nodeID == "" {
		return nil, nil
	}
	out := make([]*entities.NodeStateEvent, 0, 16)

	// The state the window OPENS in. Without this row a node that went down before the
	// window and stayed down reports as having no history — a perfect score for its
	// worst month.
	opening, _, err := h.events.Get(ctx, "", 1, 0,
		[]sqldataenums.Filter{
			{FieldName: "NodeId", Compare: sqldataenums.Equal, Value: nodeID},
			{FieldName: "At", Compare: sqldataenums.LessThan, Value: from},
		},
		[]sqldataenums.Sorter{{FieldName: "At", Sort: sqldataenums.DESC}, {FieldName: "Id", Sort: sqldataenums.DESC}})
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	if len(opening) > 0 {
		out = append(out, opening[0])
	}

	var offset uint64
	for {
		page, total, err := h.events.Get(ctx, "", stateEventPageSize, offset,
			[]sqldataenums.Filter{
				{FieldName: "NodeId", Compare: sqldataenums.Equal, Value: nodeID},
				{FieldName: "At", Compare: sqldataenums.GreaterThanOrEqualTo, Value: from},
				{FieldName: "At", Compare: sqldataenums.LessThanOrEqualTo, Value: to},
			},
			[]sqldataenums.Sorter{{FieldName: "At", Sort: sqldataenums.ASC}, {FieldName: "Id", Sort: sqldataenums.ASC}})
		if err != nil {
			if isNoResultErr(err) {
				break
			}
			return nil, err
		}
		out = append(out, page...)
		offset += uint64(len(page))
		if len(page) == 0 || offset >= total || offset >= stateEventMaxRows {
			break
		}
	}
	return out, nil
}

func (h *nodeStateHistory) Gaps(ctx context.Context, from, to int64) ([]*entities.FleetMonitorGap, error) {
	// Overlap, not containment: a gap that started before the window and ended inside
	// it is exactly the case that matters (the control plane was down over midnight on
	// the first of the month), and a containment filter drops it.
	rows, _, err := h.gaps.Get(ctx, "", stateEventPageSize, 0,
		[]sqldataenums.Filter{
			{FieldName: "StartedAt", Compare: sqldataenums.LessThanOrEqualTo, Value: to},
			{FieldName: "EndedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: from},
		},
		[]sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	return rows, nil
}

func (h *nodeStateHistory) watermark(ctx context.Context, key string) (int64, error) {
	row, err := h.settings.GetByUnique(ctx, "", "key", key)
	if err != nil {
		if isNoResultErr(err) {
			return 0, nil
		}
		return 0, err
	}
	if row == nil {
		return 0, nil
	}
	v, _ := strconv.ParseInt(row.Value, 10, 64)
	return v, nil
}

func (h *nodeStateHistory) setWatermark(ctx context.Context, key string, at int64) error {
	now := time.Now().Unix()
	row, err := h.settings.GetByUnique(ctx, "", "key", key)
	if err != nil && !isNoResultErr(err) {
		return err
	}
	if err != nil || row == nil {
		_, cerr := h.settings.Create(ctx, "", entities.ControlSetting{
			Key: key, Value: strconv.FormatInt(at, 10), CreatedAt: now, UpdatedAt: now,
		})
		return cerr
	}
	row.Value = strconv.FormatInt(at, 10)
	row.UpdatedAt = now
	_, err = h.settings.UpdateById(ctx, "", *row)
	return err
}
