package services

import (
	"context"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

const day = int64(86400)

func sev(at int64, state string) *entities.NodeStateEvent {
	return &entities.NodeStateEvent{NodeId: "n1", State: state, At: at}
}

// The four buckets must PARTITION the window. If a classification bug ever loses time
// instead of misfiling it, every percentage silently shifts and nothing else notices.
func assertPartitions(t *testing.T, got availabilityTally, from, to int64) {
	t.Helper()
	sum := got.UpSeconds + got.DownSeconds + got.UnmonitoredSeconds + got.NotInFleetSeconds
	if sum != to-from {
		t.Fatalf("buckets do not partition the window: up=%d down=%d unmon=%d notfleet=%d sum=%d want %d",
			got.UpSeconds, got.DownSeconds, got.UnmonitoredSeconds, got.NotInFleetSeconds, sum, to-from)
	}
}

func TestTallyCountsOnlineAsUpAndLostAsDown(t *testing.T) {
	from, to := int64(0), 10*day
	events := []*entities.NodeStateEvent{
		sev(0, entities.NodeStateOnline),
		sev(8*day, entities.NodeStateLost),
		sev(9*day, entities.NodeStateOnline),
	}
	got := tallyAvailability(events, nil, from, to)
	if got.UpSeconds != 9*day {
		t.Fatalf("up = %d, want %d", got.UpSeconds, 9*day)
	}
	if got.DownSeconds != day {
		t.Fatalf("down = %d, want %d", got.DownSeconds, day)
	}
	if got.Outages != 1 || got.LongestOutageSeconds != day {
		t.Fatalf("outages = %d longest = %d, want 1 / %d", got.Outages, got.LongestOutageSeconds, day)
	}
	assertPartitions(t, got, from, to)
}

// THE CRUX. A node that went down before the window opened and stayed down must report
// the window as downtime. If the query or the maths ignores the last event BEFORE the
// window, the node's worst month reads as its emptiest one — a total outage rendered as
// "no data", which is the single most expensive way this feature could be wrong.
func TestTallyUsesTheEventBeforeTheWindowToOpenIt(t *testing.T) {
	from, to := 30*day, 31*day
	events := []*entities.NodeStateEvent{sev(day, entities.NodeStateLost)}
	got := tallyAvailability(events, nil, from, to)
	if got.DownSeconds != day {
		t.Fatalf("down = %d, want the whole window (%d)", got.DownSeconds, day)
	}
	if got.UnmonitoredSeconds != 0 {
		t.Fatalf("unmonitored = %d, want 0 — the state was known throughout", got.UnmonitoredSeconds)
	}
	assertPartitions(t, got, from, to)
}

// Time before a node's history begins is unmonitored, never up. Otherwise upgrading an
// existing fleet retroactively awards it a perfect record for months nobody measured.
func TestTallyTreatsTimeBeforeFirstEventAsUnmonitored(t *testing.T) {
	from, to := int64(0), 10*day
	events := []*entities.NodeStateEvent{sev(6*day, entities.NodeStateOnline)}
	got := tallyAvailability(events, nil, from, to)
	if got.UnmonitoredSeconds != 6*day {
		t.Fatalf("unmonitored = %d, want %d", got.UnmonitoredSeconds, 6*day)
	}
	if got.UpSeconds != 4*day {
		t.Fatalf("up = %d, want %d", got.UpSeconds, 4*day)
	}
	assertPartitions(t, got, from, to)
}

// THE OTHER CRUX. A control-plane outage must not be credited to the fleet as uptime.
// The node was recorded online and never changed state, so without the gap subtraction
// this window reads as a flawless 10 days — including the 2 days we were dead.
func TestTallySubtractsMonitorGapsFromUptime(t *testing.T) {
	from, to := int64(0), 10*day
	events := []*entities.NodeStateEvent{sev(0, entities.NodeStateOnline)}
	gaps := []interval{{From: 4 * day, To: 6 * day}}
	got := tallyAvailability(events, gaps, from, to)
	if got.UpSeconds != 8*day {
		t.Fatalf("up = %d, want %d — the gap must not count as uptime", got.UpSeconds, 8*day)
	}
	if got.UnmonitoredSeconds != 2*day {
		t.Fatalf("unmonitored = %d, want %d", got.UnmonitoredSeconds, 2*day)
	}
	pct, ok := availabilityPercent(got.UpSeconds, got.DownSeconds)
	if !ok || pct != 100 {
		t.Fatalf("availability = %v/%v, want 100 — the measured time was all up", pct, ok)
	}
	assertPartitions(t, got, from, to)
}

// A gap covering a whole outage means we cannot claim the outage either. Erring toward
// "we do not know" has to cut both ways, or the gap subtraction becomes a way to
// launder downtime out of the record.
func TestTallyGapOverAnOutageRemovesTheDowntimeToo(t *testing.T) {
	from, to := int64(0), 10*day
	events := []*entities.NodeStateEvent{
		sev(0, entities.NodeStateOnline),
		sev(4*day, entities.NodeStateLost),
		sev(6*day, entities.NodeStateOnline),
	}
	gaps := []interval{{From: 4 * day, To: 6 * day}}
	got := tallyAvailability(events, gaps, from, to)
	if got.DownSeconds != 0 {
		t.Fatalf("down = %d, want 0 — that span was unmonitored", got.DownSeconds)
	}
	if got.Outages != 0 {
		t.Fatalf("outages = %d, want 0 — an outage nobody observed is not a counted outage", got.Outages)
	}
	assertPartitions(t, got, from, to)
}

// Overlapping gaps must not subtract the same second twice, or the buckets stop
// partitioning the window and every percentage drifts.
func TestTallyOverlappingGapsSubtractOnce(t *testing.T) {
	from, to := int64(0), 10*day
	events := []*entities.NodeStateEvent{sev(0, entities.NodeStateOnline)}
	gaps := []interval{{From: 2 * day, To: 5 * day}, {From: 3 * day, To: 6 * day}, {From: 8 * day, To: 9 * day}}
	got := tallyAvailability(events, gaps, from, to)
	if got.UnmonitoredSeconds != 5*day {
		t.Fatalf("unmonitored = %d, want %d (4d merged + 1d)", got.UnmonitoredSeconds, 5*day)
	}
	assertPartitions(t, got, from, to)
}

// A node that unpaired itself is out of the fleet: neither up nor down. Counting it as
// downtime lets one decommissioned appliance bury a site's figure for a year.
func TestTallySelfDroppedIsExcludedNotDowntime(t *testing.T) {
	from, to := int64(0), 10*day
	events := []*entities.NodeStateEvent{
		sev(0, entities.NodeStateOnline),
		sev(5*day, entities.NodeStateSelfDropped),
	}
	got := tallyAvailability(events, nil, from, to)
	if got.DownSeconds != 0 {
		t.Fatalf("down = %d, want 0", got.DownSeconds)
	}
	if got.NotInFleetSeconds != 5*day {
		t.Fatalf("notInFleet = %d, want %d", got.NotInFleetSeconds, 5*day)
	}
	pct, ok := availabilityPercent(got.UpSeconds, got.DownSeconds)
	if !ok || pct != 100 {
		t.Fatalf("availability = %v/%v, want 100 over the measured five days", pct, ok)
	}
	assertPartitions(t, got, from, to)
}

// A state string from a future build must not be read as uptime.
func TestTallyUnknownStateIsUnmonitored(t *testing.T) {
	from, to := int64(0), 10*day
	events := []*entities.NodeStateEvent{sev(0, "quarantined")}
	got := tallyAvailability(events, nil, from, to)
	if got.UpSeconds != 0 || got.UnmonitoredSeconds != 10*day {
		t.Fatalf("up = %d unmonitored = %d, want 0 / %d", got.UpSeconds, got.UnmonitoredSeconds, 10*day)
	}
	assertPartitions(t, got, from, to)
}

// Repeated outages accumulate; the longest is reported separately because two hours in
// one go and two hours in ninety pieces are the same figure and different problems.
func TestTallyCountsEachOutageAndTheLongest(t *testing.T) {
	from, to := int64(0), int64(100*3600)
	events := []*entities.NodeStateEvent{
		sev(0, entities.NodeStateOnline),
		sev(3600, entities.NodeStateLost), sev(2*3600, entities.NodeStateOnline),
		sev(10*3600, entities.NodeStateLost), sev(14*3600, entities.NodeStateOnline),
	}
	got := tallyAvailability(events, nil, from, to)
	if got.Outages != 2 {
		t.Fatalf("outages = %d, want 2", got.Outages)
	}
	if got.LongestOutageSeconds != 4*3600 {
		t.Fatalf("longest = %d, want %d", got.LongestOutageSeconds, 4*3600)
	}
	if got.DownSeconds != 5*3600 {
		t.Fatalf("down = %d, want %d", got.DownSeconds, 5*3600)
	}
	assertPartitions(t, got, from, to)
}

// A window nothing was measured in is NOT 100% available — it is unknown. This is the
// difference between an appliance nobody has ever seen sitting at the top of the health
// table and it being reported as what it is.
func TestNoMeasuredTimeIsNotPerfect(t *testing.T) {
	pct, ok := availabilityPercent(0, 0)
	if ok {
		t.Fatalf("hasData = true with nothing measured (pct %v)", pct)
	}
	if got := formatAvailability(pct, ok); got != "no data" {
		t.Fatalf("formatted %q, want %q", got, "no data")
	}
}

// Availability is FLOORED. One second of downtime in a year rounds to 100.00% and
// prints a breach as a clean sheet; only zero downtime may print 100.
func TestAvailabilityNeverRoundsUpToAPerfectScore(t *testing.T) {
	year := int64(365 * 86400)
	pct, ok := availabilityPercent(year-1, 1)
	if !ok {
		t.Fatal("hasData = false with a year of measured time")
	}
	if got := formatAvailability(pct, ok); got == "100.00%" {
		t.Fatalf("one second of downtime formatted as %q — a breach reported as perfect", got)
	}
	if pct != 99.99 {
		t.Fatalf("availability = %v, want 99.99", pct)
	}
	// And zero downtime still reads as perfect, or the rule above would make a clean
	// record unreportable.
	if pct, ok := availabilityPercent(year, 0); !ok || formatAvailability(pct, ok) != "100.00%" {
		t.Fatalf("a clean year formatted as %q", formatAvailability(pct, ok))
	}
}

// Floors, not rounds, away from the 100% edge as well — where the "never a perfect
// score" clamp is not there to cover for it. 98.999% must read as 98.99, not 99.00: an
// SLA written at 99% is either met or it is not, and rounding decides that question in
// the vendor's favour.
func TestAvailabilityFloorsToTwoDecimals(t *testing.T) {
	// 1001 down out of 100000 → 98.999%. Rounding prints 99.00 and clears a 99% SLA the
	// fleet actually missed.
	pct, _ := availabilityPercent(98999, 1001)
	if pct != 98.99 {
		t.Fatalf("availability = %v, want 98.99 — 98.999%% must not round up to 99.00", pct)
	}
	// 2 down out of 100000 → 99.998%, floored to 99.99.
	pct, _ = availabilityPercent(99998, 2)
	if pct != 99.99 {
		t.Fatalf("availability = %v, want 99.99", pct)
	}
}

func TestMonthBucketsSplitOnCalendarBoundariesInUTC(t *testing.T) {
	from := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC).Unix()
	to := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC).Unix()
	got := monthBuckets(from, to)
	if len(got) != 3 {
		t.Fatalf("buckets = %d, want 3", len(got))
	}
	if got[0].Month != "2026-01" || got[1].Month != "2026-02" || got[2].Month != "2026-03" {
		t.Fatalf("months = %q %q %q", got[0].Month, got[1].Month, got[2].Month)
	}
	// The partial first bucket must start at the window, not at the 1st — otherwise the
	// month is credited with 19 days nobody asked about.
	if got[0].From != from {
		t.Fatalf("first bucket starts %d, want the window start %d", got[0].From, from)
	}
	if got[2].To != to {
		t.Fatalf("last bucket ends %d, want the window end %d", got[2].To, to)
	}
	if got[1].From != time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("february starts %d, want the 1st", got[1].From)
	}
	// The buckets must tile the window exactly.
	var total int64
	for _, m := range got {
		total += m.To - m.From
	}
	if total != to-from {
		t.Fatalf("buckets cover %d seconds, want %d", total, to-from)
	}
}

// A window ending exactly on a month boundary must not produce a zero-length trailing
// bucket (an empty "March 2026" row reading "no data" beside a complete February).
func TestMonthBucketsDoNotEmitAnEmptyTrailingMonth(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	to := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Unix()
	got := monthBuckets(from, to)
	if len(got) != 2 {
		t.Fatalf("buckets = %d (%v), want 2", len(got), got)
	}
}

// --- service level ----------------------------------------------------------------

type stubHistory struct {
	events map[string][]*entities.NodeStateEvent
	gaps   []*entities.FleetMonitorGap
}

func (s *stubHistory) Observe(context.Context, string, string, string, int64) error { return nil }
func (s *stubHistory) Forget(context.Context, string) error                         { return nil }
func (s *stubHistory) NoteSweep(context.Context, int64, int64) error                { return nil }
func (s *stubHistory) Prune(context.Context, int64) error                           { return nil }
func (s *stubHistory) Events(_ context.Context, nodeID string, _, _ int64) ([]*entities.NodeStateEvent, error) {
	return s.events[nodeID], nil
}
func (s *stubHistory) Gaps(context.Context, int64, int64) ([]*entities.FleetMonitorGap, error) {
	return s.gaps, nil
}

type stubNodes struct{ rows []*entities.ManagedNode }

func (s *stubNodes) List(context.Context) ([]*entities.ManagedNode, error) { return s.rows, nil }

type stubSites struct{ rows []*entities.Site }

func (s *stubSites) ListSites(context.Context) ([]*entities.Site, error) { return s.rows, nil }

func TestFleetAvailabilityRollsUpBySiteAndMonth(t *testing.T) {
	to := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Unix()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	febStart := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC).Unix()

	svc := &nodeAvailabilityService{
		nodes: &stubNodes{rows: []*entities.ManagedNode{
			{NodeId: "a", Name: "Lobby", SiteId: 1, Status: "online"},
			{NodeId: "b", Name: "Dock", SiteId: 1, Status: "online"},
			{NodeId: "c", Name: "Remote", SiteId: 0, Status: "online"},
		}},
		sites: &stubSites{rows: []*entities.Site{{Id: 1, Name: "Airport"}}},
		history: &stubHistory{events: map[string][]*entities.NodeStateEvent{
			"a": {{NodeId: "a", State: entities.NodeStateOnline, At: from}},
			// b is down for one day, in February only.
			"b": {
				{NodeId: "b", State: entities.NodeStateOnline, At: from},
				{NodeId: "b", State: entities.NodeStateLost, At: febStart},
				{NodeId: "b", State: entities.NodeStateOnline, At: febStart + day},
			},
			// c has no history at all.
		}},
	}

	got, err := svc.Fleet(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Fleet: %v", err)
	}
	if len(got.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(got.Nodes))
	}
	// Worst first, and the node with no data sorts BELOW the one with real downtime —
	// an absent record is not the fleet's worst appliance.
	if got.Nodes[0].NodeId != "b" {
		t.Fatalf("first row = %q, want the node with downtime", got.Nodes[0].NodeId)
	}
	if got.Nodes[2].NodeId != "c" || got.Nodes[2].HasData {
		t.Fatalf("last row = %q hasData=%v, want the unmeasured node", got.Nodes[2].NodeId, got.Nodes[2].HasData)
	}
	if got.Nodes[2].Coverage != 0 {
		t.Fatalf("unmeasured node coverage = %v, want 0", got.Nodes[2].Coverage)
	}

	// January was clean, February had the outage — the whole point of the monthly
	// breakdown is that a bad February is not averaged away by a good January.
	if len(got.Months) != 2 {
		t.Fatalf("months = %d, want 2", len(got.Months))
	}
	if got.Months[0].DownSeconds != 0 {
		t.Fatalf("january downtime = %d, want 0", got.Months[0].DownSeconds)
	}
	if got.Months[1].DownSeconds != day {
		t.Fatalf("february downtime = %d, want %d", got.Months[1].DownSeconds, day)
	}

	// Sites: Airport holds a+b; the site-less node forms its own named group rather
	// than vanishing from the breakdown.
	var airport, unassigned *SiteAvailability
	for i := range got.Sites {
		switch got.Sites[i].SiteId {
		case 1:
			airport = &got.Sites[i]
		case 0:
			unassigned = &got.Sites[i]
		}
	}
	if airport == nil || airport.Nodes != 2 {
		t.Fatalf("airport site missing or wrong node count: %+v", airport)
	}
	if airport.DownSeconds != day {
		t.Fatalf("airport downtime = %d, want %d", airport.DownSeconds, day)
	}
	if unassigned == nil || unassigned.Name != "Unassigned" {
		t.Fatalf("site-less node group missing or unnamed: %+v", unassigned)
	}
	// Aggregated by node-seconds, so one node's day of downtime against two nodes'
	// two months is a small dent, not half the site.
	if !airport.HasData || airport.Availability > 100 || airport.Availability < 99 {
		t.Fatalf("airport availability = %v, want just under 100", airport.Availability)
	}
}

// The fleet total must not count an unmeasured node as up, and coverage must reflect
// that a third of the fleet was never observed.
func TestFleetTotalsExcludeUnmeasuredNodes(t *testing.T) {
	to := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Unix()
	from := to - 10*day
	svc := &nodeAvailabilityService{
		nodes: &stubNodes{rows: []*entities.ManagedNode{
			{NodeId: "a", Name: "A", Status: "online"},
			{NodeId: "b", Name: "B", Status: "online"},
		}},
		sites: &stubSites{},
		history: &stubHistory{events: map[string][]*entities.NodeStateEvent{
			"a": {{NodeId: "a", State: entities.NodeStateOnline, At: from}},
		}},
	}
	got, err := svc.Fleet(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Fleet: %v", err)
	}
	if got.MeasuredSeconds != 10*day {
		t.Fatalf("measured = %d, want one node's ten days (%d)", got.MeasuredSeconds, 10*day)
	}
	if got.UnmonitoredSeconds != 10*day {
		t.Fatalf("unmonitored = %d, want the other node's ten days", got.UnmonitoredSeconds)
	}
	if got.Coverage != 50 {
		t.Fatalf("coverage = %v, want 50 — half the fleet was never observed", got.Coverage)
	}
}

// A control-plane gap is reported in its own right, not folded silently into the
// per-node unmonitored totals: it is the one figure here that is about us.
func TestFleetSurfacesTheControlPlanesOwnDowntime(t *testing.T) {
	to := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Unix()
	from := to - 10*day
	svc := &nodeAvailabilityService{
		nodes: &stubNodes{rows: []*entities.ManagedNode{{NodeId: "a", Name: "A", Status: "online"}}},
		sites: &stubSites{},
		history: &stubHistory{
			events: map[string][]*entities.NodeStateEvent{
				"a": {{NodeId: "a", State: entities.NodeStateOnline, At: from}},
			},
			gaps: []*entities.FleetMonitorGap{{StartedAt: from + 2*day, EndedAt: from + 3*day}},
		},
	}
	got, err := svc.Fleet(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Fleet: %v", err)
	}
	if got.MonitorGapSeconds != day || got.MonitorGaps != 1 {
		t.Fatalf("monitor gap = %ds x%d, want %ds x1", got.MonitorGapSeconds, got.MonitorGaps, day)
	}
	if got.UpSeconds != 9*day {
		t.Fatalf("up = %d, want %d — the gap is not uptime", got.UpSeconds, 9*day)
	}
	if got.Coverage != 90 {
		t.Fatalf("coverage = %v, want 90", got.Coverage)
	}
}

func TestFormatDurationReadsLikeAnOutage(t *testing.T) {
	cases := map[int64]string{0: "—", 48: "48s", 125: "2m 5s", 3900: "1h 5m", 90000: "1d 1h"}
	for in, want := range cases {
		if got := formatDuration(in); got != want {
			t.Fatalf("formatDuration(%d) = %q, want %q", in, got, want)
		}
	}
}
