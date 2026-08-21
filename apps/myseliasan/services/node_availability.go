package services

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

// Availability reporting — turning the node state history into the number a customer's
// contract is written in.
//
// Three decisions shape every figure below, and all three exist because the easy
// alternative produces a number that is flattering rather than true.
//
//  1. Time we were not watching is NOT uptime. A monitoring gap (see
//     entities.FleetMonitorGap) and the period before a node's history begins are both
//     subtracted from the denominator and reported separately. A report that quietly
//     credits its own downtime to the fleet is worse than no report.
//  2. A node with no measured time is NOT 100% available. It is "no data". Dividing by
//     zero and calling the result perfect is how an appliance that has never once been
//     seen ends up at the top of the fleet health table.
//  3. Availability is FLOORED, never rounded. A single second of downtime in a year is
//     99.999997%, and rounding prints 100.00% — a breach rendered as a clean sheet. The
//     only input that prints 100.00% is zero downtime.
//
// Rollups (per site, per fleet, per month) aggregate NODE-SECONDS rather than averaging
// per-node percentages, so a node adopted yesterday cannot weigh as heavily on a site's
// monthly figure as one that ran all month.

// interval is a half-open [From,To) period in unix seconds.
type interval struct {
	From int64
	To   int64
}

// availabilityTally is the second-by-second accounting of one window. The four buckets
// PARTITION the window: their sum is exactly To-From, which is the invariant that keeps
// a classification bug from silently vanishing time instead of misplacing it.
type availabilityTally struct {
	// UpSeconds / DownSeconds are measured time — the denominator of availability.
	UpSeconds   int64
	DownSeconds int64
	// UnmonitoredSeconds is time nothing was watching: a control-plane gap, or time
	// before this node's history begins.
	UnmonitoredSeconds int64
	// NotInFleetSeconds is time the node had unpaired itself. Deliberately neither up
	// nor down: a decommissioned appliance is not an outage, and counting it as one
	// would let a single retired node bury a site's figure for the rest of the year.
	NotInFleetSeconds int64
	Outages           int
	// LongestOutageSeconds is the longest single measured down span. Two hours in one
	// go and two hours in ninety separate minutes are the same availability and very
	// different operational facts.
	LongestOutageSeconds int64
}

// measured is the time the fleet was actually observed — the denominator.
func (t availabilityTally) measured() int64 { return t.UpSeconds + t.DownSeconds }

func (t *availabilityTally) add(o availabilityTally) {
	t.UpSeconds += o.UpSeconds
	t.DownSeconds += o.DownSeconds
	t.UnmonitoredSeconds += o.UnmonitoredSeconds
	t.NotInFleetSeconds += o.NotInFleetSeconds
	t.Outages += o.Outages
	if o.LongestOutageSeconds > t.LongestOutageSeconds {
		t.LongestOutageSeconds = o.LongestOutageSeconds
	}
}

// availabilityPercent returns the availability of a measured window and whether there
// was anything to measure. See decisions 2 and 3 at the top of this file.
func availabilityPercent(up, down int64) (float64, bool) {
	measured := up + down
	if measured <= 0 {
		return 0, false
	}
	if down <= 0 {
		return 100, true
	}
	pct := math.Floor(float64(up)/float64(measured)*10000) / 100
	if pct >= 100 {
		// Flooring already handles almost everything; this catches the float edge where
		// a vanishing outage divides out to exactly 100 before the floor sees it. A node
		// with recorded downtime must never print a perfect score.
		pct = 99.99
	}
	return pct, true
}

// mergeIntervals sorts and coalesces overlapping/touching intervals, so overlapping
// monitoring gaps (two instances both noticing the same outage, say) subtract the span
// once rather than twice — which would otherwise remove more time from the window than
// the window contains.
func mergeIntervals(in []interval) []interval {
	if len(in) == 0 {
		return nil
	}
	cp := make([]interval, 0, len(in))
	for _, iv := range in {
		if iv.To > iv.From {
			cp = append(cp, iv)
		}
	}
	if len(cp) == 0 {
		return nil
	}
	sort.Slice(cp, func(i, j int) bool { return cp[i].From < cp[j].From })
	out := []interval{cp[0]}
	for _, iv := range cp[1:] {
		last := &out[len(out)-1]
		if iv.From <= last.To {
			if iv.To > last.To {
				last.To = iv.To
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

// overlapSeconds is how much of [from,to) is covered by the (merged) intervals.
func overlapSeconds(from, to int64, merged []interval) int64 {
	var total int64
	for _, iv := range merged {
		s, e := iv.From, iv.To
		if s < from {
			s = from
		}
		if e > to {
			e = to
		}
		if e > s {
			total += e - s
		}
	}
	return total
}

// tallyAvailability reduces one node's chronologically-ordered transitions into the
// accounting for [from,to).
//
// events must be sorted ascending and MAY begin with the last event before `from` —
// that row is what establishes the state the window opened in. Without it a node that
// went down in March and stayed down reports April as unmonitored, which is the single
// most expensive way this could be wrong: the worst month reads as the emptiest one.
//
// gaps need not be sorted or disjoint.
func tallyAvailability(events []*entities.NodeStateEvent, gaps []interval, from, to int64) availabilityTally {
	var t availabilityTally
	if to <= from {
		return t
	}
	merged := mergeIntervals(gaps)

	// segment is a contiguous claim about the node's state. The leading segment has an
	// empty state: before the first recorded event we know nothing, which is a fact
	// about our records rather than about the node, so it is unmonitored — not up.
	type segment struct {
		start, end int64
		state      string
	}
	segs := make([]segment, 0, len(events)+1)
	if len(events) == 0 {
		segs = append(segs, segment{start: from, end: to})
	} else {
		if events[0].At > from {
			segs = append(segs, segment{start: from, end: events[0].At})
		}
		for i, ev := range events {
			end := to
			if i+1 < len(events) {
				end = events[i+1].At
			}
			segs = append(segs, segment{start: ev.At, end: end, state: ev.State})
		}
	}

	for _, seg := range segs {
		start, end := seg.start, seg.end
		if start < from {
			start = from
		}
		if end > to {
			end = to
		}
		if end <= start {
			continue
		}
		unmonitored := overlapSeconds(start, end, merged)
		t.UnmonitoredSeconds += unmonitored
		measured := (end - start) - unmonitored
		if measured <= 0 {
			continue
		}
		switch seg.state {
		case entities.NodeStateOnline:
			t.UpSeconds += measured
		case entities.NodeStateLost:
			t.DownSeconds += measured
			t.Outages++
			if measured > t.LongestOutageSeconds {
				t.LongestOutageSeconds = measured
			}
		case entities.NodeStateSelfDropped:
			t.NotInFleetSeconds += measured
		default:
			// A state written by a future build this one does not understand. Counting
			// it as up would invent uptime out of a string nobody here can read.
			t.UnmonitoredSeconds += measured
		}
	}
	return t
}

// --- report shapes ----------------------------------------------------------------

// NodeAvailability is one node's record over the report window.
type NodeAvailability struct {
	NodeId   string `json:"nodeId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	SiteId   int64  `json:"siteId"`
	SiteName string `json:"siteName"`
	// Status is the node's CURRENT status, for context beside a historical figure.
	Status string `json:"status"`

	UpSeconds          int64 `json:"upSeconds"`
	DownSeconds        int64 `json:"downSeconds"`
	MeasuredSeconds    int64 `json:"measuredSeconds"`
	UnmonitoredSeconds int64 `json:"unmonitoredSeconds"`
	NotInFleetSeconds  int64 `json:"notInFleetSeconds"`

	// HasData is false when nothing about this node was measured in the window. Read it
	// BEFORE Availability, which is 0 in that case and means "unknown", not "never up".
	HasData      bool    `json:"hasData"`
	Availability float64 `json:"availability"`
	// Coverage is the share of the window that was measured at all (0-100). An
	// availability of 100 over a coverage of 4 is not a good month.
	Coverage             float64 `json:"coverage"`
	Outages              int     `json:"outages"`
	LongestOutageSeconds int64   `json:"longestOutageSeconds"`
}

// SiteAvailability rolls nodes up by the building they reside in.
type SiteAvailability struct {
	SiteId               int64   `json:"siteId"`
	Name                 string  `json:"name"`
	Nodes                int     `json:"nodes"`
	UpSeconds            int64   `json:"upSeconds"`
	DownSeconds          int64   `json:"downSeconds"`
	MeasuredSeconds      int64   `json:"measuredSeconds"`
	UnmonitoredSeconds   int64   `json:"unmonitoredSeconds"`
	HasData              bool    `json:"hasData"`
	Availability         float64 `json:"availability"`
	Outages              int     `json:"outages"`
	LongestOutageSeconds int64   `json:"longestOutageSeconds"`
}

// MonthAvailability is the fleet's figure for one calendar month of the window.
type MonthAvailability struct {
	// Month is "2026-08"; Label is "August 2026".
	Month              string  `json:"month"`
	Label              string  `json:"label"`
	From               int64   `json:"from"`
	To                 int64   `json:"to"`
	UpSeconds          int64   `json:"upSeconds"`
	DownSeconds        int64   `json:"downSeconds"`
	MeasuredSeconds    int64   `json:"measuredSeconds"`
	UnmonitoredSeconds int64   `json:"unmonitoredSeconds"`
	HasData            bool    `json:"hasData"`
	Availability       float64 `json:"availability"`
	Outages            int     `json:"outages"`
}

// FleetAvailability is the whole answer: the fleet total, then the same figure sliced
// by node, by site and by month.
type FleetAvailability struct {
	From          int64 `json:"from"`
	To            int64 `json:"to"`
	WindowSeconds int64 `json:"windowSeconds"`

	UpSeconds          int64 `json:"upSeconds"`
	DownSeconds        int64 `json:"downSeconds"`
	MeasuredSeconds    int64 `json:"measuredSeconds"`
	UnmonitoredSeconds int64 `json:"unmonitoredSeconds"`
	NotInFleetSeconds  int64 `json:"notInFleetSeconds"`

	HasData      bool    `json:"hasData"`
	Availability float64 `json:"availability"`
	Coverage     float64 `json:"coverage"`
	Outages      int     `json:"outages"`

	// MonitorGapSeconds is how much of the window the CONTROL PLANE itself was not
	// watching, and MonitorGaps how many separate times. Surfaced on its own because it
	// is the one number in this report that is about us rather than about the fleet.
	MonitorGapSeconds int64 `json:"monitorGapSeconds"`
	MonitorGaps       int   `json:"monitorGaps"`

	Nodes  []NodeAvailability  `json:"nodes"`
	Sites  []SiteAvailability  `json:"sites"`
	Months []MonthAvailability `json:"months"`
}

// INodeAvailabilityService answers what the fleet's uptime WAS.
type INodeAvailabilityService interface {
	// Fleet computes availability over [from,to) for every currently adopted node,
	// rolled up by site and by calendar month (UTC).
	Fleet(ctx context.Context, from, to int64) (*FleetAvailability, error)
}

// availabilityNodeLister is the sliver of the registry this needs. Narrow so the tests
// do not have to build a whole registry to exercise the arithmetic.
type availabilityNodeLister interface {
	List(ctx context.Context) ([]*entities.ManagedNode, error)
}

type availabilitySiteLister interface {
	ListSites(ctx context.Context) ([]*entities.Site, error)
}

type nodeAvailabilityService struct {
	nodes   availabilityNodeLister
	sites   availabilitySiteLister
	history INodeStateHistory
}

// NewNodeAvailabilityService builds the availability reporter over the node registry,
// the site list and the state history.
func NewNodeAvailabilityService(nodes INodeRegistry, sites ISiteService, history INodeStateHistory) INodeAvailabilityService {
	return &nodeAvailabilityService{nodes: nodes, sites: sites, history: history}
}

// maxAvailabilityWindow caps a report window at ~2 years, comfortably beyond the
// history retention. Without it a caller passing from=0 asks for 56 years of monthly
// buckets and gets a report nobody wanted and a slice nobody bounded.
const maxAvailabilityWindow = 2 * 400 * 24 * 60 * 60

func (s *nodeAvailabilityService) Fleet(ctx context.Context, from, to int64) (*FleetAvailability, error) {
	if to <= 0 {
		to = time.Now().Unix()
	}
	if from <= 0 || from >= to {
		from = to - 30*86400
	}
	if to-from > maxAvailabilityWindow {
		from = to - maxAvailabilityWindow
	}

	out := &FleetAvailability{From: from, To: to, WindowSeconds: to - from}

	nodes, err := s.nodes.List(ctx)
	if err != nil {
		return nil, err
	}
	siteName := map[int64]string{}
	if s.sites != nil {
		if sites, serr := s.sites.ListSites(ctx); serr == nil {
			for _, site := range sites {
				siteName[site.Id] = site.Name
			}
		}
	}

	gapRows, err := s.history.Gaps(ctx, from, to)
	if err != nil {
		return nil, err
	}
	gaps := make([]interval, 0, len(gapRows))
	for _, g := range gapRows {
		gaps = append(gaps, interval{From: g.StartedAt, To: g.EndedAt})
	}
	merged := mergeIntervals(gaps)
	out.MonitorGapSeconds = overlapSeconds(from, to, merged)
	out.MonitorGaps = len(merged)

	months := monthBuckets(from, to)
	monthTally := make([]availabilityTally, len(months))
	siteTally := map[int64]*availabilityTally{}
	siteNodes := map[int64]int{}
	var fleet availabilityTally

	for _, node := range nodes {
		events, eerr := s.history.Events(ctx, node.NodeId, from, to)
		if eerr != nil {
			return nil, eerr
		}
		t := tallyAvailability(events, gaps, from, to)
		fleet.add(t)

		row := NodeAvailability{
			NodeId:               node.NodeId,
			Name:                 nodeDisplayName(node),
			Kind:                 node.Kind,
			SiteId:               node.SiteId,
			SiteName:             siteName[node.SiteId],
			Status:               node.Status,
			UpSeconds:            t.UpSeconds,
			DownSeconds:          t.DownSeconds,
			MeasuredSeconds:      t.measured(),
			UnmonitoredSeconds:   t.UnmonitoredSeconds,
			NotInFleetSeconds:    t.NotInFleetSeconds,
			Outages:              t.Outages,
			LongestOutageSeconds: t.LongestOutageSeconds,
		}
		row.Availability, row.HasData = availabilityPercent(t.UpSeconds, t.DownSeconds)
		row.Coverage = ratioPercent(t.measured(), to-from)
		out.Nodes = append(out.Nodes, row)

		// Site attribution follows the node's CURRENT building. A node that moved sites
		// mid-window has its whole history counted where it lives now — the alternative
		// (stamping the site onto every event) makes a past report un-correctable when
		// somebody discovers the node was filed under the wrong building all along.
		st, ok := siteTally[node.SiteId]
		if !ok {
			st = &availabilityTally{}
			siteTally[node.SiteId] = st
		}
		st.add(t)
		siteNodes[node.SiteId]++

		for i, m := range months {
			monthTally[i].add(tallyAvailability(events, gaps, m.From, m.To))
		}
	}

	out.UpSeconds = fleet.UpSeconds
	out.DownSeconds = fleet.DownSeconds
	out.MeasuredSeconds = fleet.measured()
	out.UnmonitoredSeconds = fleet.UnmonitoredSeconds
	out.NotInFleetSeconds = fleet.NotInFleetSeconds
	out.Outages = fleet.Outages
	out.Availability, out.HasData = availabilityPercent(fleet.UpSeconds, fleet.DownSeconds)
	out.Coverage = ratioPercent(fleet.measured(), (to-from)*int64(len(nodes)))

	for siteID, t := range siteTally {
		name := siteName[siteID]
		if name == "" {
			// Nodes with no building are a real group, not an error: a standalone
			// recorder or a hub in the field. Naming the group is what stops it looking
			// like a rendering bug in the report.
			name = "Unassigned"
		}
		row := SiteAvailability{
			SiteId:               siteID,
			Name:                 name,
			Nodes:                siteNodes[siteID],
			UpSeconds:            t.UpSeconds,
			DownSeconds:          t.DownSeconds,
			MeasuredSeconds:      t.measured(),
			UnmonitoredSeconds:   t.UnmonitoredSeconds,
			Outages:              t.Outages,
			LongestOutageSeconds: t.LongestOutageSeconds,
		}
		row.Availability, row.HasData = availabilityPercent(t.UpSeconds, t.DownSeconds)
		out.Sites = append(out.Sites, row)
	}

	for i, m := range months {
		t := monthTally[i]
		row := MonthAvailability{
			Month:              m.Month,
			Label:              m.Label,
			From:               m.From,
			To:                 m.To,
			UpSeconds:          t.UpSeconds,
			DownSeconds:        t.DownSeconds,
			MeasuredSeconds:    t.measured(),
			UnmonitoredSeconds: t.UnmonitoredSeconds,
			Outages:            t.Outages,
		}
		row.Availability, row.HasData = availabilityPercent(t.UpSeconds, t.DownSeconds)
		out.Months = append(out.Months, row)
	}

	// Worst first: a report is read from the top, and the top is where the problem
	// should be. Ties break on more downtime, then on name so the order is stable
	// between two identical runs.
	sort.Slice(out.Nodes, func(i, j int) bool { return worseNode(out.Nodes[i], out.Nodes[j]) })
	sort.Slice(out.Sites, func(i, j int) bool { return worseSite(out.Sites[i], out.Sites[j]) })

	if out.Nodes == nil {
		out.Nodes = []NodeAvailability{}
	}
	if out.Sites == nil {
		out.Sites = []SiteAvailability{}
	}
	if out.Months == nil {
		out.Months = []MonthAvailability{}
	}
	return out, nil
}

// worseNode orders the node table: measured problems first, then unmeasured nodes, then
// healthy ones. A node with no data sorts below any node with real downtime — it is a
// gap in the record, not the worst appliance in the fleet.
func worseNode(a, b NodeAvailability) bool {
	if a.HasData != b.HasData {
		return a.HasData
	}
	if a.Availability != b.Availability {
		return a.Availability < b.Availability
	}
	if a.DownSeconds != b.DownSeconds {
		return a.DownSeconds > b.DownSeconds
	}
	return a.Name < b.Name
}

func worseSite(a, b SiteAvailability) bool {
	if a.HasData != b.HasData {
		return a.HasData
	}
	if a.Availability != b.Availability {
		return a.Availability < b.Availability
	}
	if a.DownSeconds != b.DownSeconds {
		return a.DownSeconds > b.DownSeconds
	}
	return a.Name < b.Name
}

// ratioPercent is part/whole as a percentage, floored, with an empty whole reported as 0.
func ratioPercent(part, whole int64) float64 {
	if whole <= 0 || part <= 0 {
		return 0
	}
	if part >= whole {
		return 100
	}
	return math.Floor(float64(part)/float64(whole)*10000) / 100
}

// nodeDisplayName is the node's name, falling back to its id so a row is never blank.
func nodeDisplayName(n *entities.ManagedNode) string {
	if n.Name != "" {
		return n.Name
	}
	return n.NodeId
}

// monthWindow is one calendar month clipped to the report window.
type monthWindow struct {
	Month string
	Label string
	From  int64
	To    int64
}

// monthBuckets splits [from,to) into calendar months in UTC.
//
// UTC and not the operator's timezone, deliberately: the boundary this report is
// compared against is a contractual month, the figure has to be reproducible by two
// people in different offices, and a browser-supplied offset would make the same window
// yield two different answers. The first and last buckets are PARTIAL when the window
// does not start on the first of a month, and are labelled with their real from/to so a
// reader is not invited to compare a 9-day bucket with a 31-day one.
func monthBuckets(from, to int64) []monthWindow {
	if to <= from {
		return nil
	}
	out := []monthWindow{}
	cursor := time.Unix(from, 0).UTC()
	for {
		monthStart := time.Date(cursor.Year(), cursor.Month(), 1, 0, 0, 0, 0, time.UTC)
		next := monthStart.AddDate(0, 1, 0)
		start := monthStart.Unix()
		if start < from {
			start = from
		}
		end := next.Unix()
		if end > to {
			end = to
		}
		if end > start {
			out = append(out, monthWindow{
				Month: monthStart.Format("2006-01"),
				Label: monthStart.Format("January 2006"),
				From:  start,
				To:    end,
			})
		}
		if next.Unix() >= to {
			break
		}
		cursor = next
	}
	return out
}

// formatDuration renders a span of seconds the way an operator reads an outage:
// "4h 12m", "3d 5h", "48s". Zero is "—" rather than "0s", so a clean row is quiet.
func formatDuration(sec int64) string {
	if sec <= 0 {
		return "—"
	}
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// formatAvailability renders a figure for a table cell. "no data" is spelled out rather
// than rendered as 0.00% — the two mean opposite things and a percent sign on the first
// one is a lie a reader has no way to detect.
func formatAvailability(pct float64, hasData bool) string {
	if !hasData {
		return "no data"
	}
	return fmt.Sprintf("%.2f%%", pct)
}
