package services

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
)

// The deterministic narrator: every insight the digest reports is computed here,
// in plain Go, from data the control plane already holds. No LLM is involved —
// the language model (when enabled) only REPHRASES these findings, so a wrong
// number in a digest is a bug in this file, never a hallucination.
//
// Findings are STRUCTURED (a typed code + parameters + grounding ids) rather
// than prose, because prose has a language and the suite has four. The frontend
// renders each finding through its i18n dictionary (key "agent.finding.<code>"),
// so a digest generated at 07:00 reads natively in en, ms, zh, and ar alike.
//
// One control-plane-specific rule shapes everything here: relayed node events
// keep their ORIGIN camera ids, and camera 3 on node A is not camera 3 on node
// B. Per-camera analytics would silently merge unrelated cameras, so every
// per-thing finding is keyed by SOURCE ("node:<id>") and the baseline band is
// fleet-wide only.

// Finding is one typed digest insight.
type Finding struct {
	// Code identifies the finding kind; the UI localizes via "agent.finding.<code>".
	Code string `json:"code"`
	// Severity is "info", "warning", or "critical".
	Severity string `json:"severity"`
	// Params are the values interpolated into the localized sentence.
	Params map[string]any `json:"params,omitempty"`
	// NotificationIds / NodeIds ground the finding in clickable records.
	NotificationIds []int64  `json:"notificationIds,omitempty"`
	NodeIds         []string `json:"nodeIds,omitempty"`
}

// Finding codes (the taxonomy). Adding a code means adding its i18n keys to all
// four frontend dictionaries.
const (
	FindingVolumeDelta   = "volume_delta"
	FindingCritical      = "critical_events"
	FindingBaselineSpike = "baseline_spike"
	FindingBaselineQuiet = "baseline_quiet"
	// Per-node variants, computed against that source's OWN learned band (the
	// rollup's source dimension) rather than the fleet-wide envelope.
	FindingNodeBaselineSpike = "node_baseline_spike"
	FindingNodeBaselineQuiet = "node_baseline_quiet"
	FindingSourceAnomaly = "source_anomaly"
	FindingTopSource     = "top_source"
	FindingNoisySource   = "noisy_source"
	FindingNodeOffline   = "node_offline"
	FindingCertExpiring  = "cert_expiring"
	FindingCertExpired   = "cert_expired"
	FindingFleetRule     = "fleet_rule_fired"
	FindingFeedGrowth    = "feed_growth"
	FindingAuditActivity = "audit_highlight"
	FindingSuggestedRule = "suggested_rule"
	FindingAllQuiet      = "all_quiet"
)

// FindingsInput parameterizes one collection run.
type FindingsInput struct {
	Now         time.Time
	WindowHours int
	TZOffsetSec int64
	// RuleFor reports whether a fleet rule already covers (nodeId, category) —
	// the suggested-rule detector skips those. nil = no dedup (suggest anyway).
	RuleFor func(nodeId, category string) bool
}

// Tuning thresholds. Deliberately conservative: a digest that cries wolf daily
// is deleted from the morning routine within a week.
const (
	findingsMaxTotal          = 30
	volumeDeltaWarnPct        = 50
	volumeDeltaMinTotal       = 20
	sourceAnomalyMinCount     = 10
	sourceAnomalyFactor       = 3
	noisySourceMinCount       = 20
	noisySourceMinUnreadPct   = 80
	feedGrowthWarnRows        = 100_000
	topSourcesReported        = 3
	perCodeCap                = 5   // max node_offline / cert_* findings each
	windowRowsFetchCap        = 2000 // raw rows examined per digest, newest-first
	auditHighlightMinCount    = 3
	criticalIdsReported       = 10
)

// sensitiveAuditActions are the audit verbs worth surfacing in a digest.
var sensitiveAuditActions = map[string]bool{
	"settings.save":     true,
	"settings.reset":    true,
	"node.adopt":        true,
	"node.release":      true,
	"node.command":      true,
	"node.revoke":       true,
	"rbac.set_role":     true,
	"rbac.set_disabled": true,
	"rbac.elevate":      true,
	"fleet.key_rotate":  true,
}

// digestNotifSource is the sliver of the notification service the narrator reads.
// *notification.Service satisfies it; tests use a fake.
type digestNotifSource interface {
	Stats(ctx context.Context, from, to, bucketSeconds, tzOffsetSec int64) (*notification.Stats, error)
	Baseline(ctx context.Context, from, to, bucketSeconds, tzOffsetSec, cameraId int64, source string) (*notification.Baseline, error)
	List(ctx context.Context, limit, offset uint64, cameraId int64, unreadOnly bool, category, source string) ([]*sharedentities.Notification, uint64, error)
}

// digestFleetSource is the sliver of the node registry the narrator reads.
type digestFleetSource interface {
	List(ctx context.Context) ([]*entities.ManagedNode, error)
	FleetStatus(ctx context.Context) (FleetStatus, error)
}

// CollectFindings computes the digest findings for the window ending at in.Now.
// Individual sections degrade independently: a failing baseline query costs the
// baseline findings, not the digest.
func CollectFindings(ctx context.Context, in FindingsInput,
	notif digestNotifSource, fleet digestFleetSource, audit IAuditService) ([]Finding, error) {

	if in.WindowHours <= 0 {
		in.WindowHours = 24
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	to := now.Unix()
	from := to - int64(in.WindowHours)*3600

	var findings []Finding

	// Rows first: they also supply the digest-exclusion-corrected severity counts
	// the volume finding needs (Stats cannot exclude a source, and the digest's own
	// critical feed entries must never inflate the next digest's numbers).
	rows, tableTotal := fetchWindowRows(ctx, notif, from)
	adj := adjustedSeverityCounts(rows)

	stats, statsErr := notif.Stats(ctx, from, to, 3600, in.TZOffsetSec)
	if statsErr == nil && stats != nil {
		findings = append(findings, volumeFindings(stats, in.WindowHours, adj)...)
		findings = append(findings, topSourceFindings(stats)...)
		findings = append(findings, sourceAnomalyFindings(ctx, notif, stats, from, to, in.TZOffsetSec)...)
	}

	if baseline, err := notif.Baseline(ctx, from, to, 3600, in.TZOffsetSec, 0, ""); err == nil && stats != nil {
		findings = append(findings, baselineFindings(stats, baseline)...)
	}
	if stats != nil {
		findings = append(findings, sourceBaselineFindings(ctx, notif, stats, rows, from, to, in.TZOffsetSec)...)
	}

	findings = append(findings, rowFindings(rows, in.WindowHours)...)
	findings = append(findings, feedGrowthFinding(tableTotal)...)

	if fleet != nil {
		findings = append(findings, fleetFindings(ctx, fleet, now)...)
	}
	if audit != nil {
		findings = append(findings, auditFindings(ctx, audit, from)...)
	}

	// Suggested rules always look at a 7-day pattern regardless of the digest
	// window (three nights of after-hours activity cannot be seen in 24h).
	suggestRows := rows
	if in.WindowHours < 168 {
		suggestRows, _ = fetchWindowRows(ctx, notif, to-168*3600)
	}
	findings = append(findings, suggestedRuleFindings(suggestRows, now, in.RuleFor)...)

	if len(findings) == 0 {
		findings = append(findings, Finding{
			Code:     FindingAllQuiet,
			Severity: string(notification.Info),
			Params:   map[string]any{"windowHours": in.WindowHours},
		})
	}

	sortFindings(findings)
	if len(findings) > findingsMaxTotal {
		findings = findings[:findingsMaxTotal]
	}
	// Only propagate a hard error when NOTHING could be computed.
	if statsErr != nil && len(findings) == 1 && findings[0].Code == FindingAllQuiet {
		return findings, fmt.Errorf("digest stats: %w", statsErr)
	}
	return findings, nil
}

// MaxFindingSeverity returns the highest severity present ("info" floor).
func MaxFindingSeverity(findings []Finding) string {
	max := string(notification.Info)
	for _, f := range findings {
		if severityRank(f.Severity) > severityRank(max) {
			max = f.Severity
		}
	}
	return max
}

// --- section collectors -----------------------------------------------------

// severityCounts is the digest-exclusion-corrected severity tally derived from
// the raw window rows. complete is false when the row fetch hit its cap, in
// which case the (accurate but digest-inclusive) Stats counts are used instead.
type severityCounts struct {
	critical, warning, own int64
	complete               bool
}

func adjustedSeverityCounts(rows []*sharedentities.Notification) severityCounts {
	out := severityCounts{complete: len(rows) < windowRowsFetchCap}
	for _, n := range rows {
		if n.Source == digestOwnSource {
			out.own++
			continue
		}
		switch strings.ToLower(n.Severity) {
		case string(notification.Critical):
			out.critical++
		case string(notification.Warning):
			out.warning++
		}
	}
	return out
}

func volumeFindings(stats *notification.Stats, windowHours int, adj severityCounts) []Finding {
	if stats.Total == 0 && stats.PrevTotal == 0 {
		return nil
	}
	total := stats.Total
	critical, warning := stats.Critical, stats.Warning
	if adj.complete {
		// The row scan covered the whole window: subtract our own feed entries so
		// the volume numbers agree with critical_events (which excludes them too).
		total -= adj.own
		critical, warning = adj.critical, adj.warning
	}
	if total <= 0 && stats.PrevTotal == 0 {
		return nil
	}
	deltaPct := 0
	if stats.PrevTotal > 0 {
		deltaPct = int(((total - stats.PrevTotal) * 100) / stats.PrevTotal)
	}
	sev := string(notification.Info)
	if abs(deltaPct) >= volumeDeltaWarnPct && total >= volumeDeltaMinTotal {
		sev = string(notification.Warning)
	}
	return []Finding{{
		Code:     FindingVolumeDelta,
		Severity: sev,
		Params: map[string]any{
			"total":       total,
			"prevTotal":   stats.PrevTotal,
			"deltaPct":    deltaPct,
			"critical":    critical,
			"warning":     warning,
			"windowHours": windowHours,
		},
	}}
}

func topSourceFindings(stats *notification.Stats) []Finding {
	var out []Finding
	rank := 0
	for _, item := range stats.BySource {
		if item.Key == digestOwnSource {
			continue // our own feed entries are not fleet activity
		}
		rank++
		if rank > topSourcesReported || item.Count == 0 {
			break
		}
		share := 0
		if stats.Total > 0 {
			share = int(item.Count * 100 / stats.Total)
		}
		out = append(out, Finding{
			Code:     FindingTopSource,
			Severity: string(notification.Info),
			Params:   map[string]any{"source": item.Key, "count": item.Count, "sharePct": share, "rank": rank},
			NodeIds:  nodeIdOf(item.Key),
		})
	}
	return out
}

// sourceAnomalyFindings compares each source's current-window volume against the
// PREVIOUS equal window (a second Stats call), per-source. This replaces the
// cameraId-keyed AnomalyScan, which cannot be trusted on the control plane.
func sourceAnomalyFindings(ctx context.Context, notif digestNotifSource,
	cur *notification.Stats, from, to, tzOffsetSec int64) []Finding {

	prev, err := notif.Stats(ctx, from-(to-from), from, 3600, tzOffsetSec)
	if err != nil || prev == nil {
		return nil
	}
	prevBySource := map[string]int64{}
	for _, item := range prev.BySource {
		prevBySource[item.Key] = item.Count
	}
	var out []Finding
	for _, item := range cur.BySource {
		if item.Key == digestOwnSource {
			continue // never score our own output as fleet activity
		}
		p := prevBySource[item.Key]
		switch {
		case item.Count >= sourceAnomalyMinCount && item.Count >= p*sourceAnomalyFactor && p > 0,
			item.Count >= sourceAnomalyMinCount && p == 0:
			out = append(out, Finding{
				Code:     FindingSourceAnomaly,
				Severity: string(notification.Warning),
				Params:   map[string]any{"source": item.Key, "count": item.Count, "prevCount": p, "direction": "high"},
				NodeIds:  nodeIdOf(item.Key),
			})
		case p >= sourceAnomalyMinCount && item.Count*sourceAnomalyFactor <= p:
			out = append(out, Finding{
				Code:     FindingSourceAnomaly,
				Severity: string(notification.Warning),
				Params:   map[string]any{"source": item.Key, "count": item.Count, "prevCount": p, "direction": "low"},
				NodeIds:  nodeIdOf(item.Key),
			})
		}
		if len(out) >= perCodeCap {
			break
		}
	}
	return out
}

// baselineFindings flags chart buckets whose ACTUAL total breached the
// fleet-wide expected band. Learning buckets never flag.
func baselineFindings(stats *notification.Stats, baseline *notification.Baseline) []Finding {
	if baseline == nil || len(baseline.Buckets) == 0 {
		return nil
	}
	actual := map[int64]int64{}
	for _, b := range stats.Timeseries {
		actual[b.Start] = b.Total
	}
	var out []Finding
	for _, b := range baseline.Buckets {
		if b.Learning {
			continue
		}
		count, ok := actual[b.Start]
		if !ok {
			continue
		}
		if float64(count) > b.Hi && count > 0 {
			out = append(out, Finding{
				Code:     FindingBaselineSpike,
				Severity: string(notification.Warning),
				Params: map[string]any{
					"bucketStart": b.Start, "count": count,
					"expectedHi": int64(b.Hi), "expectedMid": int64(b.Mid),
				},
			})
		} else if float64(count) < b.Lo && b.Mid > 0 {
			out = append(out, Finding{
				Code:     FindingBaselineQuiet,
				Severity: string(notification.Warning),
				Params: map[string]any{
					"bucketStart": b.Start, "count": count,
					"expectedLo": int64(b.Lo), "expectedMid": int64(b.Mid),
				},
			})
		}
		if len(out) >= perCodeCap {
			break
		}
	}
	return out
}

// sourceBaselineFindings checks each top node source's CURRENT window against
// that source's OWN learned band (the rollup's per-source dimension). Actual
// per-source hourly counts come from the already-fetched window rows; a bucket
// with no rows is a real zero — which is exactly how a dead camera looks.
// Learning buckets never flag, so this stays silent until enough source-split
// history accumulates after the upgrade.
func sourceBaselineFindings(ctx context.Context, notif digestNotifSource,
	stats *notification.Stats, rows []*sharedentities.Notification, from, to, tzOffsetSec int64) []Finding {

	// Per-source actual counts per hour bucket, from the window rows.
	actuals := map[string]map[int64]int64{}
	for _, n := range rows {
		if !strings.HasPrefix(n.Source, "node:") {
			continue
		}
		bucket := n.CreatedAt - n.CreatedAt%3600
		if actuals[n.Source] == nil {
			actuals[n.Source] = map[int64]int64{}
		}
		actuals[n.Source][bucket]++
	}

	var out []Finding
	checked := 0
	for _, item := range stats.BySource {
		if !strings.HasPrefix(item.Key, "node:") {
			continue
		}
		if checked >= topSourcesReported || len(out) >= perCodeCap {
			break
		}
		checked++
		band, err := notif.Baseline(ctx, from, to, 3600, tzOffsetSec, 0, item.Key)
		if err != nil || band == nil {
			continue
		}
		src := actuals[item.Key]
		for _, b := range band.Buckets {
			if b.Learning {
				continue
			}
			count := src[b.Start] // absent = genuine zero
			if float64(count) > b.Hi && count > 0 {
				out = append(out, Finding{
					Code:     FindingNodeBaselineSpike,
					Severity: string(notification.Warning),
					Params: map[string]any{
						"source": item.Key, "bucketStart": b.Start, "count": count,
						"expectedHi": int64(b.Hi), "expectedMid": int64(b.Mid),
					},
					NodeIds: nodeIdOf(item.Key),
				})
			} else if float64(count) < b.Lo && b.Mid > 0 {
				out = append(out, Finding{
					Code:     FindingNodeBaselineQuiet,
					Severity: string(notification.Warning),
					Params: map[string]any{
						"source": item.Key, "bucketStart": b.Start, "count": count,
						"expectedLo": int64(b.Lo), "expectedMid": int64(b.Mid),
					},
					NodeIds: nodeIdOf(item.Key),
				})
			}
			if len(out) >= perCodeCap {
				break
			}
		}
	}
	return out
}

// fetchWindowRows pages the feed newest-first until it leaves the window or hits
// the fetch cap, returning the in-window rows plus the table's TOTAL row count
// (which List reports for free).
func fetchWindowRows(ctx context.Context, notif digestNotifSource, from int64) ([]*sharedentities.Notification, uint64) {
	var (
		rows  []*sharedentities.Notification
		total uint64
	)
	const page = 500
	for offset := uint64(0); offset < windowRowsFetchCap; offset += page {
		items, t, err := notif.List(ctx, page, offset, 0, false, "", "")
		if err != nil || len(items) == 0 {
			break
		}
		total = t
		done := false
		for _, n := range items {
			if n.CreatedAt < from {
				done = true
				break
			}
			rows = append(rows, n)
		}
		if done || len(items) < page {
			break
		}
	}
	return rows, total
}

// digestOwnSource is the Source the digest publishes its feed entry under. The
// narrator must NEVER read its own output: yesterday's critical-severity digest
// notification would otherwise count as today's "critical event", and every
// digest from then on stays critical forever. Events come from the fleet;
// conclusions come from here; the two must not be mixed (the correlator makes
// the same exclusion for the same reason).
const digestOwnSource = "ai-digest"

// rowFindings derives the findings that need actual rows (ids, unread ratios,
// fleet-rule firings) from the single window fetch.
func rowFindings(rows []*sharedentities.Notification, windowHours int) []Finding {
	if len(rows) == 0 {
		return nil
	}
	var (
		criticalIds  []int64
		criticalN    int
		ruleIds      []int64
		ruleTitles   []string
		bySourceAll  = map[string]int64{}
		bySourceUnrd = map[string]int64{}
	)
	seenTitles := map[string]bool{}
	for _, n := range rows {
		if n.Source == digestOwnSource {
			continue // never read our own output back as evidence
		}
		if strings.EqualFold(n.Severity, string(notification.Critical)) {
			criticalN++
			if len(criticalIds) < criticalIdsReported {
				criticalIds = append(criticalIds, n.Id)
			}
		}
		if n.Source == "fleet-rule" {
			ruleIds = append(ruleIds, n.Id)
			if !seenTitles[n.Title] && len(ruleTitles) < perCodeCap {
				seenTitles[n.Title] = true
				ruleTitles = append(ruleTitles, n.Title)
			}
		}
		if strings.HasPrefix(n.Source, "node:") {
			bySourceAll[n.Source]++
			if !n.IsRead {
				bySourceUnrd[n.Source]++
			}
		}
	}

	var out []Finding
	if criticalN > 0 {
		out = append(out, Finding{
			Code:            FindingCritical,
			Severity:        string(notification.Critical),
			Params:          map[string]any{"count": criticalN, "windowHours": windowHours},
			NotificationIds: criticalIds,
		})
	}
	if len(ruleIds) > 0 {
		ids := ruleIds
		if len(ids) > criticalIdsReported {
			ids = ids[:criticalIdsReported]
		}
		out = append(out, Finding{
			Code:            FindingFleetRule,
			Severity:        string(notification.Warning),
			Params:          map[string]any{"count": len(ruleIds), "ruleTitles": strings.Join(ruleTitles, ", ")},
			NotificationIds: ids,
		})
	}

	// Noisy sources: high volume, mostly unread — alerts nobody reads are noise,
	// and noise is what gets a whole feed ignored.
	type noisy struct {
		source        string
		count, unread int64
	}
	var candidates []noisy
	for src, count := range bySourceAll {
		unread := bySourceUnrd[src]
		if count >= noisySourceMinCount && unread*100 >= count*noisySourceMinUnreadPct {
			candidates = append(candidates, noisy{source: src, count: count, unread: unread})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].count > candidates[j].count })
	for i, c := range candidates {
		if i >= perCodeCap {
			break
		}
		out = append(out, Finding{
			Code:     FindingNoisySource,
			Severity: string(notification.Warning),
			Params: map[string]any{
				"source": c.source, "count": c.count, "unread": c.unread,
				"unreadPct": int(c.unread * 100 / c.count),
			},
			NodeIds: nodeIdOf(c.source),
		})
	}
	return out
}

func feedGrowthFinding(tableTotal uint64) []Finding {
	if tableTotal < feedGrowthWarnRows {
		return nil
	}
	return []Finding{{
		Code:     FindingFeedGrowth,
		Severity: string(notification.Warning),
		Params:   map[string]any{"tableTotal": tableTotal},
	}}
}

func fleetFindings(ctx context.Context, fleet digestFleetSource, now time.Time) []Finding {
	nodes, err := fleet.List(ctx)
	if err != nil || len(nodes) == 0 {
		return nil
	}
	status, _ := fleet.FleetStatus(ctx)
	warnDays := status.CertWarnDays
	if warnDays <= 0 {
		warnDays = 7
	}
	warnWindow := int64(warnDays) * 86400
	nowUnix := now.Unix()

	var offline, expiring, expired []Finding
	offlineCount := 0
	for _, n := range nodes {
		if n.Status != "online" {
			offlineCount++
			if len(offline) < perCodeCap {
				offline = append(offline, Finding{
					Code:     FindingNodeOffline,
					Severity: string(notification.Warning),
					Params: map[string]any{
						"nodeId": n.NodeId, "name": displayName(n),
						"status": n.Status, "lastSeenAt": n.LastSeenAt,
					},
					NodeIds: []string{n.NodeId},
				})
			}
		}
		if n.CertExpiresAt > 0 {
			left := n.CertExpiresAt - nowUnix
			switch {
			case left <= 0 && len(expired) < perCodeCap:
				expired = append(expired, Finding{
					Code:     FindingCertExpired,
					Severity: string(notification.Critical),
					Params: map[string]any{
						"nodeId": n.NodeId, "name": displayName(n),
						"expiredDays": int(-left / 86400), "autoRenew": n.AutoRenew,
					},
					NodeIds: []string{n.NodeId},
				})
			case left > 0 && left <= warnWindow && len(expiring) < perCodeCap:
				expiring = append(expiring, Finding{
					Code:     FindingCertExpiring,
					Severity: string(notification.Warning),
					Params: map[string]any{
						"nodeId": n.NodeId, "name": displayName(n),
						"daysLeft": int(left / 86400), "autoRenew": n.AutoRenew,
					},
					NodeIds: []string{n.NodeId},
				})
			}
		}
	}
	// A quarter of the fleet dark is not a per-node nuisance, it is an incident.
	if offlineCount*4 > len(nodes) && len(nodes) >= 4 {
		for i := range offline {
			offline[i].Severity = string(notification.Critical)
		}
	}
	return append(append(offline, expired...), expiring...)
}

func auditFindings(ctx context.Context, audit IAuditService, from int64) []Finding {
	rows, _, err := audit.List(ctx, 200, 0, AuditFilter{})
	if err != nil {
		return nil
	}
	counts := map[string]int{}
	for _, r := range rows {
		if r.CreatedAt < from {
			break // newest-first: everything after is out of window
		}
		if sensitiveAuditActions[r.Action] {
			counts[r.Action]++
		}
	}
	actions := make([]string, 0, len(counts))
	for a := range counts {
		actions = append(actions, a)
	}
	sort.Slice(actions, func(i, j int) bool { return counts[actions[i]] > counts[actions[j]] })
	var out []Finding
	for i, a := range actions {
		if i >= perCodeCap || counts[a] < auditHighlightMinCount {
			break
		}
		out = append(out, Finding{
			Code:     FindingAuditActivity,
			Severity: string(notification.Info),
			Params:   map[string]any{"action": a, "count": counts[a]},
		})
	}
	return out
}

// --- suggested rules ---------------------------------------------------------

// Suggested-rule thresholds: after-hours events from one (source, category) on
// at least suggestNights distinct nights, with at least suggestPerNight events
// each night. Conservative on purpose — a bad suggestion teaches the operator
// to ignore the good ones.
const (
	suggestNightStartHour = 22
	suggestNightEndHour   = 6
	suggestNights         = 3
	suggestPerNight       = 2
	suggestCap            = 3
)

// suggestedRuleFindings detects recurring after-hours activity that has no
// covering fleet rule and proposes one. Emitted as an info finding whose params
// pre-fill the rule editor; the agent NEVER creates rules itself.
func suggestedRuleFindings(rows []*sharedentities.Notification, now time.Time, ruleFor func(nodeId, category string) bool) []Finding {
	if len(rows) == 0 {
		return nil
	}
	type key struct{ source, category string }
	// nights[key][localDate] = events that night
	nights := map[key]map[string]int{}
	totals := map[key]int{}
	for _, n := range rows {
		if !strings.HasPrefix(n.Source, "node:") || n.Category == "" {
			continue
		}
		at := time.Unix(n.CreatedAt, 0).In(now.Location())
		h := at.Hour()
		if h < suggestNightStartHour && h >= suggestNightEndHour {
			continue // daytime
		}
		// Attribute the small hours to the night that began the evening before,
		// so 23:50 and 00:10 count as ONE night, not two.
		night := at
		if h < suggestNightEndHour {
			night = at.AddDate(0, 0, -1)
		}
		k := key{source: n.Source, category: n.Category}
		if nights[k] == nil {
			nights[k] = map[string]int{}
		}
		nights[k][localDate(night)]++
		totals[k]++
	}

	var out []Finding
	for k, perNight := range nights {
		qualifying := 0
		for _, c := range perNight {
			if c >= suggestPerNight {
				qualifying++
			}
		}
		if qualifying < suggestNights {
			continue
		}
		nodeId := strings.TrimPrefix(k.source, "node:")
		if ruleFor != nil && ruleFor(nodeId, k.category) {
			continue // the operator already covers this
		}
		out = append(out, Finding{
			Code:     FindingSuggestedRule,
			Severity: string(notification.Info),
			Params: map[string]any{
				"source":   k.source,
				"nodeId":   nodeId,
				"category": k.category,
				"nights":   qualifying,
				"count":    totals[k],
				// Editor prefill: a sane starting shape the operator refines.
				"suggestedName":    fmt.Sprintf("After-hours %s on %s", k.category, nodeId),
				"windowSeconds":    120,
				"graceSeconds":     5,
				"cooldownSeconds":  300,
			},
			NodeIds: []string{nodeId},
		})
		if len(out) >= suggestCap {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return i64(out[i].Params["count"]) > i64(out[j].Params["count"])
	})
	return out
}

// --- ordering helpers -------------------------------------------------------

// codeOrder is the taxonomy display order within one severity tier.
var codeOrder = map[string]int{
	FindingCritical:      0,
	FindingCertExpired:   1,
	FindingFleetRule:     2,
	FindingNodeOffline:   3,
	FindingBaselineSpike:     4,
	FindingBaselineQuiet:     5,
	FindingNodeBaselineSpike: 6,
	FindingNodeBaselineQuiet: 7,
	FindingSourceAnomaly:     8,
	FindingCertExpiring:      9,
	FindingNoisySource:       10,
	FindingFeedGrowth:        11,
	FindingVolumeDelta:       12,
	FindingTopSource:         13,
	FindingAuditActivity:     14,
	FindingSuggestedRule:     15,
	FindingAllQuiet:          16,
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		ri, rj := severityRank(findings[i].Severity), severityRank(findings[j].Severity)
		if ri != rj {
			return ri > rj
		}
		return codeOrder[findings[i].Code] < codeOrder[findings[j].Code]
	})
}

func severityRank(s string) int {
	switch strings.ToLower(s) {
	case string(notification.Critical):
		return 2
	case string(notification.Warning):
		return 1
	}
	return 0
}

func nodeIdOf(source string) []string {
	if id, ok := strings.CutPrefix(source, "node:"); ok && id != "" {
		return []string{id}
	}
	return nil
}

func displayName(n *entities.ManagedNode) string {
	if n.Name != "" {
		return n.Name
	}
	return n.NodeId
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
