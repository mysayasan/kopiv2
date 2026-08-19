package services

import (
	"context"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
)

// --- fakes -------------------------------------------------------------------

type fakeNotifSource struct {
	stats     map[int64]*notification.Stats // keyed by "from" so current vs previous window differ
	baseline  *notification.Baseline
	rows      []*sharedentities.Notification
	rowsTotal uint64
}

func (f *fakeNotifSource) Stats(_ context.Context, from, _, _, _ int64) (*notification.Stats, error) {
	if s, ok := f.stats[from]; ok {
		return s, nil
	}
	return &notification.Stats{}, nil
}

func (f *fakeNotifSource) Baseline(_ context.Context, _, _, _, _, _ int64, _ string) (*notification.Baseline, error) {
	if f.baseline == nil {
		return &notification.Baseline{}, nil
	}
	return f.baseline, nil
}

func (f *fakeNotifSource) List(_ context.Context, limit, offset uint64, _ int64, _ bool, _, _ string) ([]*sharedentities.Notification, uint64, error) {
	if offset >= uint64(len(f.rows)) {
		return nil, f.rowsTotal, nil
	}
	end := offset + limit
	if end > uint64(len(f.rows)) {
		end = uint64(len(f.rows))
	}
	return f.rows[offset:end], f.rowsTotal, nil
}

type fakeFleetSource struct {
	nodes  []*entities.ManagedNode
	status FleetStatus
}

func (f *fakeFleetSource) List(context.Context) ([]*entities.ManagedNode, error) { return f.nodes, nil }
func (f *fakeFleetSource) FleetStatus(context.Context) (FleetStatus, error)      { return f.status, nil }

type fakeDigestAudit struct {
	// Embedded so the fake satisfies the whole trail interface without stubbing
	// PurgeOlderThan, which nothing here calls — an unexpected call nil-panics loudly
	// rather than quietly returning a zero result.
	IAuditService
	rows []*entities.AuditLog
}

func (f *fakeDigestAudit) Record(context.Context, AuditEntry) {}
func (f *fakeDigestAudit) List(_ context.Context, _, _ uint64, _ AuditFilter) ([]*entities.AuditLog, uint64, error) {
	return f.rows, uint64(len(f.rows)), nil
}

// --- helpers -----------------------------------------------------------------

func findingsByCode(fs []Finding) map[string][]Finding {
	out := map[string][]Finding{}
	for _, f := range fs {
		out[f.Code] = append(out[f.Code], f)
	}
	return out
}

// --- tests -------------------------------------------------------------------

func TestCollectFindingsSyntheticDay(t *testing.T) {
	now := time.Date(2026, 8, 6, 7, 0, 0, 0, time.Local)
	to := now.Unix()
	from := to - 24*3600
	prevFrom := from - 24*3600

	notifSrc := &fakeNotifSource{
		stats: map[int64]*notification.Stats{
			from: { // current window: doubled volume, one hot source
				Total: 120, PrevTotal: 60, Critical: 2, Warning: 30,
				BySource: []notification.CountItem{
					{Key: "node:cam-lobby", Count: 80},
					{Key: "node:iot-hub", Count: 30},
					{Key: "fleet-rule", Count: 3},
				},
				Timeseries: []notification.TimeBucket{
					{Start: from + 3600, Total: 50}, // way above the band below
					{Start: from + 7200, Total: 5},
				},
			},
			prevFrom: { // previous window, for the per-source comparison
				Total: 60,
				BySource: []notification.CountItem{
					{Key: "node:cam-lobby", Count: 20},
					{Key: "node:iot-hub", Count: 28},
				},
			},
		},
		baseline: &notification.Baseline{Buckets: []notification.BaselineBucket{
			{Start: from + 3600, Mid: 10, Lo: 2, Hi: 20},           // bucket 1 breaches Hi (50 > 20)
			{Start: from + 7200, Mid: 8, Lo: 2, Hi: 18},            // bucket 2 inside band
			{Start: from + 10800, Mid: 0, Lo: 0, Hi: 5, Learning: true}, // learning: never flags
		}},
		rowsTotal: 200_000, // triggers feed_growth
	}
	// Window rows: two criticals, three fleet-rule firings, one noisy source.
	mkRow := func(id int64, sev, source, title string, read bool) *sharedentities.Notification {
		return &sharedentities.Notification{Id: id, Severity: sev, Source: source, Title: title, IsRead: read, CreatedAt: to - 100}
	}
	for i := int64(0); i < 25; i++ { // noisy: 25 unread from cam-lobby
		notifSrc.rows = append(notifSrc.rows, mkRow(100+i, "warning", "node:cam-lobby", "Motion", false))
	}
	notifSrc.rows = append(notifSrc.rows,
		mkRow(1, "critical", "node:cam-lobby", "Intrusion", false),
		mkRow(2, "critical", "node:iot-hub", "Tamper", false),
		mkRow(3, "warning", "fleet-rule", "Door w/o badge", false),
		mkRow(4, "warning", "fleet-rule", "Door w/o badge", false),
		mkRow(5, "warning", "fleet-rule", "Night motion", false),
	)

	fleet := &fakeFleetSource{
		nodes: []*entities.ManagedNode{
			{NodeId: "cam-lobby", Name: "Lobby cam", Status: "online", CertExpiresAt: to + 3*86400, AutoRenew: false},
			{NodeId: "iot-hub", Name: "IoT hub", Status: "online", CertExpiresAt: to + 60*86400},
			{NodeId: "gate-cam", Name: "Gate cam", Status: "lost", LastSeenAt: to - 7200, CertExpiresAt: to - 86400},
			{NodeId: "yard-cam", Status: "online", CertExpiresAt: to + 90*86400},
		},
		status: FleetStatus{Total: 4, Online: 3, Lost: 1, CertWarnDays: 7},
	}
	audit := &fakeDigestAudit{}
	for i := 0; i < 4; i++ {
		audit.rows = append(audit.rows, &entities.AuditLog{Action: "rbac.set_role", CreatedAt: to - 500})
	}

	findings, err := CollectFindings(context.Background(),
		FindingsInput{Now: now, WindowHours: 24}, notifSrc, fleet, audit)
	if err != nil {
		t.Fatalf("CollectFindings: %v", err)
	}
	byCode := findingsByCode(findings)

	// The synthetic day must produce each expected code exactly as designed.
	assertOne := func(code string) Finding {
		t.Helper()
		fs := byCode[code]
		if len(fs) != 1 {
			t.Fatalf("%s: want exactly 1 finding, got %d (%+v)", code, len(fs), fs)
		}
		return fs[0]
	}

	vd := assertOne(FindingVolumeDelta)
	if vd.Severity != "warning" || vd.Params["deltaPct"].(int) != 100 {
		t.Errorf("volume_delta wrong: %+v", vd)
	}
	crit := assertOne(FindingCritical)
	if crit.Params["count"].(int) != 2 || len(crit.NotificationIds) != 2 {
		t.Errorf("critical_events wrong: %+v", crit)
	}
	spike := assertOne(FindingBaselineSpike)
	if spike.Params["count"].(int64) != 50 || spike.Params["expectedHi"].(int64) != 20 {
		t.Errorf("baseline_spike wrong: %+v", spike)
	}
	if len(byCode[FindingBaselineQuiet]) != 0 {
		t.Errorf("no quiet bucket expected, got %+v", byCode[FindingBaselineQuiet])
	}
	// cam-lobby: 80 vs 20 previous = 4x with count>=10 → anomaly high. iot-hub 30 vs 28 → none.
	an := assertOne(FindingSourceAnomaly)
	if an.Params["source"] != "node:cam-lobby" || an.Params["direction"] != "high" {
		t.Errorf("source_anomaly wrong: %+v", an)
	}
	if len(byCode[FindingTopSource]) != 3 {
		t.Errorf("want 3 top sources, got %d", len(byCode[FindingTopSource]))
	}
	noisy := assertOne(FindingNoisySource)
	if noisy.Params["source"] != "node:cam-lobby" {
		t.Errorf("noisy_source wrong: %+v", noisy)
	}
	rule := assertOne(FindingFleetRule)
	if rule.Params["count"].(int) != 3 {
		t.Errorf("fleet_rule_fired wrong: %+v", rule)
	}
	off := assertOne(FindingNodeOffline)
	if off.Params["nodeId"] != "gate-cam" || off.Severity != "warning" {
		t.Errorf("node_offline wrong: %+v", off)
	}
	expd := assertOne(FindingCertExpired)
	if expd.Params["nodeId"] != "gate-cam" || expd.Severity != "critical" {
		t.Errorf("cert_expired wrong: %+v", expd)
	}
	expg := assertOne(FindingCertExpiring)
	if expg.Params["nodeId"] != "cam-lobby" || expg.Params["daysLeft"].(int) != 3 {
		t.Errorf("cert_expiring wrong: %+v", expg)
	}
	assertOne(FindingFeedGrowth)
	ah := assertOne(FindingAuditActivity)
	if ah.Params["action"] != "rbac.set_role" || ah.Params["count"].(int) != 4 {
		t.Errorf("audit_highlight wrong: %+v", ah)
	}
	if len(byCode[FindingAllQuiet]) != 0 {
		t.Error("all_quiet must not appear alongside findings")
	}

	// Ordering: severity desc — first finding must be critical.
	if findings[0].Severity != "critical" {
		t.Errorf("first finding severity = %s, want critical", findings[0].Severity)
	}
	if MaxFindingSeverity(findings) != "critical" {
		t.Errorf("MaxFindingSeverity = %s", MaxFindingSeverity(findings))
	}
}

// A digest publishes its own (possibly critical) feed notification; the next
// digest must NOT read it back as evidence, or every digest after one critical
// day stays critical forever.
func TestCollectFindingsIgnoresOwnDigestNotifications(t *testing.T) {
	now := time.Now()
	notifSrc := &fakeNotifSource{stats: map[int64]*notification.Stats{}}
	notifSrc.rows = append(notifSrc.rows, &sharedentities.Notification{
		Id: 1, Severity: "critical", Source: digestOwnSource, Title: "Fleet digest", CreatedAt: now.Unix() - 100,
	})
	findings, err := CollectFindings(context.Background(),
		FindingsInput{Now: now, WindowHours: 24}, notifSrc, &fakeFleetSource{}, &fakeDigestAudit{})
	if err != nil {
		t.Fatalf("CollectFindings: %v", err)
	}
	if len(findingsByCode(findings)[FindingCritical]) != 0 {
		t.Fatal("a digest's own critical feed entry must never count as a critical event")
	}
}

func TestSuggestedRuleDetector(t *testing.T) {
	now := time.Date(2026, 8, 6, 7, 0, 0, 0, time.Local)
	mk := func(daysAgo int, hour int, source, category string) *sharedentities.Notification {
		at := time.Date(2026, 8, 6-daysAgo, hour, 30, 0, 0, time.Local)
		return &sharedentities.Notification{Source: source, Category: category, Severity: "warning", CreatedAt: at.Unix()}
	}
	var rows []*sharedentities.Notification
	// Three nights (2 events each, 23:xx + 00:xx straddle counts as ONE night) on cam-yard.
	for d := 1; d <= 3; d++ {
		rows = append(rows, mk(d, 23, "node:cam-yard", "vision.alert"))
		rows = append(rows, mk(d-1, 0, "node:cam-yard", "vision.alert")) // small hours of same night
	}
	// Daytime noise on another node: never suggested.
	for d := 1; d <= 5; d++ {
		rows = append(rows, mk(d, 14, "node:cam-lobby", "vision.alert"))
	}

	got := suggestedRuleFindings(rows, now, nil)
	if len(got) != 1 {
		t.Fatalf("want 1 suggestion, got %d (%+v)", len(got), got)
	}
	s := got[0]
	if s.Params["nodeId"] != "cam-yard" || s.Params["category"] != "vision.alert" {
		t.Fatalf("wrong target: %+v", s.Params)
	}
	if s.Params["nights"].(int) != 3 {
		t.Fatalf("nights = %v (straddling midnight must count once per night)", s.Params["nights"])
	}

	// A covering rule suppresses the suggestion.
	got = suggestedRuleFindings(rows, now, func(nodeId, category string) bool {
		return nodeId == "cam-yard" && category == "vision.alert"
	})
	if len(got) != 0 {
		t.Fatalf("covered pattern must not be suggested, got %+v", got)
	}
}

func TestCollectFindingsQuietDay(t *testing.T) {
	notifSrc := &fakeNotifSource{stats: map[int64]*notification.Stats{}}
	fleet := &fakeFleetSource{nodes: []*entities.ManagedNode{
		{NodeId: "n1", Status: "online", CertExpiresAt: time.Now().Unix() + 90*86400},
	}}
	findings, err := CollectFindings(context.Background(),
		FindingsInput{Now: time.Now(), WindowHours: 24}, notifSrc, fleet, &fakeDigestAudit{})
	if err != nil {
		t.Fatalf("CollectFindings: %v", err)
	}
	if len(findings) != 1 || findings[0].Code != FindingAllQuiet {
		t.Fatalf("quiet day should yield exactly all_quiet, got %+v", findings)
	}
}

func TestCollectFindingsQuarterFleetDarkEscalates(t *testing.T) {
	to := time.Now().Unix()
	fleet := &fakeFleetSource{nodes: []*entities.ManagedNode{
		{NodeId: "a", Status: "lost", LastSeenAt: to - 100},
		{NodeId: "b", Status: "lost", LastSeenAt: to - 100},
		{NodeId: "c", Status: "online"},
		{NodeId: "d", Status: "online"},
	}}
	findings, err := CollectFindings(context.Background(),
		FindingsInput{Now: time.Now(), WindowHours: 24},
		&fakeNotifSource{stats: map[int64]*notification.Stats{}}, fleet, &fakeDigestAudit{})
	if err != nil {
		t.Fatalf("CollectFindings: %v", err)
	}
	offline := findingsByCode(findings)[FindingNodeOffline]
	if len(offline) != 2 {
		t.Fatalf("want 2 offline findings, got %d", len(offline))
	}
	for _, f := range offline {
		if f.Severity != "critical" {
			t.Errorf("half the fleet dark must escalate to critical, got %s", f.Severity)
		}
	}
}
