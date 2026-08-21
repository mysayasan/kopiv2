package services

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// queryRepo is an in-memory repo that ACTUALLY APPLIES filters, sorters, limit and
// offset.
//
// The package's existing memRepo (backup_test.go) ignores all four, which is fine for
// what it was written for and useless here: every interesting thing this service does is
// expressed as a query — "the last event before the window", "gaps overlapping the
// window", "events older than the cutoff". Testing those against a repo that returns
// everything regardless would assert that the Go around the query compiles, and would
// pass just as happily if the filters were empty.
type queryRepo[T any] struct {
	rows []T
	next int64
	// uniqueField is the field GetByUnique matches on (control_setting's Key).
	uniqueField string
}

func newQueryRepo[T any](uniqueField string) *queryRepo[T] {
	return &queryRepo[T]{next: 1, uniqueField: uniqueField}
}

func reflectField[T any](row *T, name string) reflect.Value {
	return reflect.ValueOf(row).Elem().FieldByName(name)
}

func matchesFilter[T any](row *T, f sqldataenums.Filter) bool {
	fv := reflectField(row, f.FieldName)
	if !fv.IsValid() {
		return false
	}
	switch fv.Kind() {
	case reflect.String:
		want, _ := f.Value.(string)
		got := fv.String()
		switch f.Compare {
		case sqldataenums.Equal:
			return got == want
		case sqldataenums.NotEqual:
			return got != want
		}
		return false
	case reflect.Int64, reflect.Int:
		var want int64
		switch v := f.Value.(type) {
		case int64:
			want = v
		case int:
			want = int64(v)
		default:
			return false
		}
		got := fv.Int()
		switch f.Compare {
		case sqldataenums.Equal:
			return got == want
		case sqldataenums.NotEqual:
			return got != want
		case sqldataenums.GreaterThan:
			return got > want
		case sqldataenums.LessThan:
			return got < want
		case sqldataenums.GreaterThanOrEqualTo:
			return got >= want
		case sqldataenums.LessThanOrEqualTo:
			return got <= want
		}
	}
	return false
}

func (r *queryRepo[T]) Get(_ context.Context, _ string, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*T, uint64, error) {
	out := []*T{}
	for i := range r.rows {
		row := r.rows[i]
		ok := true
		for _, f := range filters {
			if !matchesFilter(&row, f) {
				ok = false
				break
			}
		}
		if ok {
			cp := row
			out = append(out, &cp)
		}
	}
	if len(sorters) > 0 {
		sort.SliceStable(out, func(i, j int) bool {
			for _, s := range sorters {
				a, b := reflectField(out[i], s.FieldName), reflectField(out[j], s.FieldName)
				if !a.IsValid() || a.Int() == b.Int() {
					continue
				}
				if s.Sort == sqldataenums.DESC {
					return a.Int() > b.Int()
				}
				return a.Int() < b.Int()
			}
			return false
		})
	}
	total := uint64(len(out))
	if offset >= total {
		return []*T{}, total, nil
	}
	out = out[offset:]
	if limit > 0 && uint64(len(out)) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (r *queryRepo[T]) Create(_ context.Context, _ string, model T) (uint64, error) {
	row := model
	reflectField(&row, "Id").SetInt(r.next)
	r.next++
	r.rows = append(r.rows, row)
	return uint64(r.next - 1), nil
}

func (r *queryRepo[T]) GetByUnique(_ context.Context, _ string, _ string, uids ...any) (*T, error) {
	if r.uniqueField == "" || len(uids) == 0 {
		return nil, errNoResultFound
	}
	want, _ := uids[0].(string)
	for i := range r.rows {
		if reflectField(&r.rows[i], r.uniqueField).String() == want {
			row := r.rows[i]
			return &row, nil
		}
	}
	return nil, errNoResultFound
}

func (r *queryRepo[T]) UpdateById(_ context.Context, _ string, model T) (uint64, error) {
	id := reflectField(&model, "Id").Int()
	for i := range r.rows {
		if reflectField(&r.rows[i], "Id").Int() == id {
			r.rows[i] = model
			return 1, nil
		}
	}
	return 0, errNoResultFound
}

func (r *queryRepo[T]) Delete(_ context.Context, _ string, filters []sqldataenums.Filter) (uint64, error) {
	kept := r.rows[:0]
	var n uint64
	for i := range r.rows {
		row := r.rows[i]
		ok := true
		for _, f := range filters {
			if !matchesFilter(&row, f) {
				ok = false
				break
			}
		}
		if ok {
			n++
			continue
		}
		kept = append(kept, row)
	}
	r.rows = kept
	if n == 0 {
		return 0, errNoResultFound
	}
	return n, nil
}

func (r *queryRepo[T]) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i := range r.rows {
		if reflectField(&r.rows[i], "Id").Int() == int64(id) {
			r.rows = append(r.rows[:i], r.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, errNoResultFound
}

// Unused by this service; present to satisfy the interface.
func (r *queryRepo[T]) GetJoin(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}
func (r *queryRepo[T]) GetJoinWithSpec(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...dbsql.JoinSpec) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}
func (r *queryRepo[T]) GetSingle(context.Context, string, []sqldataenums.Filter) (*T, error) {
	return nil, errNoResultFound
}
func (r *queryRepo[T]) GetById(context.Context, string, uint64) (*T, error) {
	return nil, errNoResultFound
}
func (r *queryRepo[T]) GetByForeign(context.Context, string, string, ...any) ([]*T, error) {
	return nil, nil
}
func (r *queryRepo[T]) CreateMultiple(context.Context, string, []T) (uint64, error) { return 0, nil }
func (r *queryRepo[T]) UpdateByUnique(context.Context, string, string, T) (uint64, error) {
	return 0, nil
}
func (r *queryRepo[T]) UpdateByForeign(context.Context, string, string, T) (uint64, error) {
	return 0, nil
}
func (r *queryRepo[T]) DeleteByUnique(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}
func (r *queryRepo[T]) DeleteByForeign(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}

type historyFixture struct {
	h        *nodeStateHistory
	events   *queryRepo[entities.NodeStateEvent]
	gaps     *queryRepo[entities.FleetMonitorGap]
	settings *queryRepo[entities.ControlSetting]
}

func newHistoryFixture() *historyFixture {
	events := newQueryRepo[entities.NodeStateEvent]("")
	gaps := newQueryRepo[entities.FleetMonitorGap]("")
	settings := newQueryRepo[entities.ControlSetting]("Key")
	return &historyFixture{h: newNodeStateHistory(events, gaps, settings), events: events, gaps: gaps, settings: settings}
}

// rowsFor returns one node's events, oldest first.
func (f *historyFixture) rowsFor(nodeID string) []entities.NodeStateEvent {
	out := []entities.NodeStateEvent{}
	for _, row := range f.events.rows {
		if row.NodeId == nodeID {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// A node with no history gets a baseline row, and it is labelled baseline whatever the
// caller said. A first observation mislabelled "heartbeat" would be indistinguishable
// from an ordinary transition, and an upgraded fleet would look as though it had been
// measured all along.
func TestObserveWritesABaselineForANodeWithNoHistory(t *testing.T) {
	f := newHistoryFixture()
	if err := f.h.Observe(context.Background(), "n1", entities.NodeStateOnline, entities.NodeStateReasonHeartbeat, 1000); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	rows := f.rowsFor("n1")
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Reason != entities.NodeStateReasonBaseline {
		t.Fatalf("reason = %q, want %q", rows[0].Reason, entities.NodeStateReasonBaseline)
	}
	if rows[0].PrevState != "" {
		t.Fatalf("prevState = %q, want empty on a baseline", rows[0].PrevState)
	}
}

// The transition test lives in the recorder, so callers can report unconditionally.
// Without it every heartbeat sweep writes a row and the table grows by the size of the
// fleet every minute.
func TestObserveWritesOnlyOnChange(t *testing.T) {
	f := newHistoryFixture()
	ctx := context.Background()
	for i := int64(0); i < 5; i++ {
		if err := f.h.Observe(ctx, "n1", entities.NodeStateOnline, entities.NodeStateReasonHeartbeat, 1000+i); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}
	if got := len(f.rowsFor("n1")); got != 1 {
		t.Fatalf("rows = %d, want 1 — an unchanged state must not write", got)
	}
	if err := f.h.Observe(ctx, "n1", entities.NodeStateLost, entities.NodeStateReasonHeartbeat, 2000); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	rows := f.rowsFor("n1")
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[1].PrevState != entities.NodeStateOnline || rows[1].State != entities.NodeStateLost {
		t.Fatalf("transition = %q → %q, want online → lost", rows[1].PrevState, rows[1].State)
	}
	if rows[1].Reason != entities.NodeStateReasonHeartbeat {
		t.Fatalf("reason = %q, want the caller's reason on a real transition", rows[1].Reason)
	}
}

// A restart (or a leadership handover) starts with an empty cache. The recorder must
// read the last state back from the database rather than treating every node as new —
// otherwise every restart writes a fresh baseline for the whole fleet, and a restart
// loop turns the history into a log of restarts.
func TestObserveRereadsStateAfterARestart(t *testing.T) {
	f := newHistoryFixture()
	ctx := context.Background()
	if err := f.h.Observe(ctx, "n1", entities.NodeStateOnline, entities.NodeStateReasonAdopt, 1000); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	// Same tables, brand-new service: exactly what a process restart looks like.
	restarted := newNodeStateHistory(f.events, f.gaps, f.settings)
	if err := restarted.Observe(ctx, "n1", entities.NodeStateOnline, entities.NodeStateReasonHeartbeat, 2000); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if got := len(f.rowsFor("n1")); got != 1 {
		t.Fatalf("rows = %d, want 1 — the restart must not re-baseline a known node", got)
	}
}

// Releasing a node deletes its history and nobody else's. NodeId is stable per
// appliance, so keeping it means a re-adoption inherits an outage that never happened.
func TestForgetDropsOnlyThatNodesHistory(t *testing.T) {
	f := newHistoryFixture()
	ctx := context.Background()
	_ = f.h.Observe(ctx, "n1", entities.NodeStateOnline, entities.NodeStateReasonAdopt, 1000)
	_ = f.h.Observe(ctx, "n2", entities.NodeStateOnline, entities.NodeStateReasonAdopt, 1000)
	_ = f.h.Observe(ctx, "n1", entities.NodeStateLost, entities.NodeStateReasonHeartbeat, 2000)
	if err := f.h.Forget(ctx, "n1"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if got := len(f.rowsFor("n1")); got != 0 {
		t.Fatalf("released node kept %d rows", got)
	}
	if got := len(f.rowsFor("n2")); got != 1 {
		t.Fatalf("bystander node lost history: %d rows, want 1", got)
	}
	// And a re-adoption starts clean rather than continuing the old record.
	_ = f.h.Observe(ctx, "n1", entities.NodeStateOnline, entities.NodeStateReasonAdopt, 3000)
	rows := f.rowsFor("n1")
	if len(rows) != 1 || rows[0].Reason != entities.NodeStateReasonBaseline {
		t.Fatalf("re-adoption produced %+v, want a single baseline", rows)
	}
}

// The gap detector. First sweep claims nothing; a normal sweep claims nothing; a sweep
// arriving after the grace window records the span nobody was watching.
func TestNoteSweepRecordsOnlyRealMonitoringGaps(t *testing.T) {
	f := newHistoryFixture()
	ctx := context.Background()
	const grace = int64(180)

	if err := f.h.NoteSweep(ctx, 1000, grace); err != nil {
		t.Fatalf("NoteSweep: %v", err)
	}
	if len(f.gaps.rows) != 0 {
		t.Fatalf("first sweep invented a gap: %+v", f.gaps.rows)
	}
	if err := f.h.NoteSweep(ctx, 1060, grace); err != nil {
		t.Fatalf("NoteSweep: %v", err)
	}
	if len(f.gaps.rows) != 0 {
		t.Fatalf("an on-time sweep recorded a gap: %+v", f.gaps.rows)
	}
	// Now the control plane was down for an hour.
	if err := f.h.NoteSweep(ctx, 1060+3600, grace); err != nil {
		t.Fatalf("NoteSweep: %v", err)
	}
	if len(f.gaps.rows) != 1 {
		t.Fatalf("gaps = %d, want 1", len(f.gaps.rows))
	}
	got := f.gaps.rows[0]
	if got.StartedAt != 1060 || got.EndedAt != 1060+3600 {
		t.Fatalf("gap = [%d,%d], want [1060,%d]", got.StartedAt, got.EndedAt, 1060+3600)
	}
}

// Events must include the last transition BEFORE the window — the state the window
// opened in — and must not leak another node's history into it.
func TestEventsIncludeTheOpeningStateAndOnlyThisNode(t *testing.T) {
	f := newHistoryFixture()
	ctx := context.Background()
	_ = f.h.Observe(ctx, "n1", entities.NodeStateOnline, entities.NodeStateReasonAdopt, 100)
	_ = f.h.Observe(ctx, "n1", entities.NodeStateLost, entities.NodeStateReasonHeartbeat, 200)
	_ = f.h.Observe(ctx, "n1", entities.NodeStateOnline, entities.NodeStateReasonHeartbeat, 5000)
	_ = f.h.Observe(ctx, "n2", entities.NodeStateLost, entities.NodeStateReasonHeartbeat, 300)

	got, err := f.h.Events(ctx, "n1", 1000, 9000)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("events = %d (%+v), want the opening row plus the in-window one", len(got), got)
	}
	if got[0].At != 200 || got[0].State != entities.NodeStateLost {
		t.Fatalf("opening event = %+v, want the lost row at 200", got[0])
	}
	if got[1].At != 5000 {
		t.Fatalf("second event at %d, want 5000", got[1].At)
	}
	for _, e := range got {
		if e.NodeId != "n1" {
			t.Fatalf("another node's event leaked in: %+v", e)
		}
	}
}

// A gap that STARTED before the window and ended inside it is the case that matters
// (the control plane was down over the first of the month). A containment filter drops
// it and the month reports as fully observed.
func TestGapsMatchOnOverlapNotContainment(t *testing.T) {
	f := newHistoryFixture()
	ctx := context.Background()
	_, _ = f.gaps.Create(ctx, "", entities.FleetMonitorGap{StartedAt: 500, EndedAt: 1500})
	_, _ = f.gaps.Create(ctx, "", entities.FleetMonitorGap{StartedAt: 9500, EndedAt: 10500})
	_, _ = f.gaps.Create(ctx, "", entities.FleetMonitorGap{StartedAt: 20000, EndedAt: 21000})

	got, err := f.h.Gaps(ctx, 1000, 10000)
	if err != nil {
		t.Fatalf("Gaps: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("gaps = %d (%+v), want the two that straddle the window", len(got), got)
	}
}

// Pruning must never delete a node's NEWEST event, however old it is: a node online
// without interruption for two years holds exactly one row, and that row is the only
// thing asserting it is up. Deleting it turns the best-behaved appliance in the fleet
// into one with no history.
func TestPruneKeepsTheNewestEventPerNode(t *testing.T) {
	f := newHistoryFixture()
	ctx := context.Background()
	now := int64(500 * 86400)
	old := now - 450*86400 // older than the 400-day retention

	// n1: three ancient rows. n2: one ancient row and nothing since.
	_ = f.h.Observe(ctx, "n1", entities.NodeStateOnline, entities.NodeStateReasonAdopt, old)
	_ = f.h.Observe(ctx, "n1", entities.NodeStateLost, entities.NodeStateReasonHeartbeat, old+100)
	_ = f.h.Observe(ctx, "n1", entities.NodeStateOnline, entities.NodeStateReasonHeartbeat, old+200)
	_ = f.h.Observe(ctx, "n2", entities.NodeStateOnline, entities.NodeStateReasonAdopt, old)
	_, _ = f.gaps.Create(ctx, "", entities.FleetMonitorGap{StartedAt: old, EndedAt: old + 60})

	if err := f.h.Prune(ctx, now); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	n1 := f.rowsFor("n1")
	if len(n1) != 1 || n1[0].At != old+200 {
		t.Fatalf("n1 kept %+v, want only its newest row", n1)
	}
	n2 := f.rowsFor("n2")
	if len(n2) != 1 {
		t.Fatalf("n2 kept %d rows, want its only (and newest) row", len(n2))
	}
	if len(f.gaps.rows) != 0 {
		t.Fatalf("aged gaps survived: %+v", f.gaps.rows)
	}
}

// Retention must not run on every heartbeat sweep — it would scan the table every
// minute for the sake of rows nothing has touched in over a year.
func TestPruneThrottlesToOncePerDay(t *testing.T) {
	f := newHistoryFixture()
	ctx := context.Background()
	now := int64(500 * 86400)
	old := now - 450*86400
	_ = f.h.Observe(ctx, "n1", entities.NodeStateOnline, entities.NodeStateReasonAdopt, old)
	_ = f.h.Observe(ctx, "n1", entities.NodeStateLost, entities.NodeStateReasonHeartbeat, old+100)
	if err := f.h.Prune(ctx, now); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	// Add another aged row and prune again an hour later: the throttle must skip it.
	_, _ = f.events.Create(ctx, "", entities.NodeStateEvent{NodeId: "n1", State: entities.NodeStateLost, At: old + 50})
	if err := f.h.Prune(ctx, now+3600); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if got := len(f.rowsFor("n1")); got != 2 {
		t.Fatalf("rows = %d, want 2 — the second prune should have been throttled", got)
	}
	// A day later it runs again.
	if err := f.h.Prune(ctx, now+2*86400); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if got := len(f.rowsFor("n1")); got != 1 {
		t.Fatalf("rows = %d, want 1 after the throttle expired", got)
	}
}
