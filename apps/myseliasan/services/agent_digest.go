package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appentities "github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/config"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/llm"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// DigestService orchestrates fleet digests: collect findings (deterministic),
// optionally polish with the LLM (best-effort), persist, publish to the feed.
//
// THE INVARIANT: Generate never fails because of the LLM. A model that is off,
// still loading, crashed, timing out, or answering garbage costs exactly one
// thing — the prose narrative — and the digest ships without it.
type DigestService struct {
	repo dbsql.IGenericRepo[appentities.AgentDigest]
	// notif is the narrator's read seam; publisher is the feed's write seam. Both
	// are satisfied by *notification.Service and faked independently in tests.
	notif     digestNotifSource
	publisher digestPublisher
	fleet     digestFleetSource
	audit     IAuditService
	llm       *LLMManager
	cfg       func() config.AgentConfigModel
	metrics   telemetry.Metrics
	logf      func(format string, args ...any)
	// ruleFor lets the suggested-rule detector skip patterns an existing fleet
	// rule already covers. Wired post-construction (the correlator is built later).
	ruleFor func(nodeId, category string) bool
}

// SetRuleChecker wires the correlator's rule-coverage oracle. Optional.
func (d *DigestService) SetRuleChecker(fn func(nodeId, category string) bool) { d.ruleFor = fn }

// digestPublisher is the sliver of the notification service the digest writes to.
type digestPublisher interface {
	Publish(ctx context.Context, n notification.Notification) notification.Notification
}

// digestLLMTimeout bounds the polish call independently of the chat timeout —
// the scheduler must never hang a morning digest on a wedged model.
const digestLLMTimeout = 45 * time.Second

// NewDigestService wires the digest orchestrator. cfg is a getter so callers
// pass the live config block.
func NewDigestService(
	db dbsql.IDbCrud,
	notif *notification.Service,
	fleet digestFleetSource,
	audit IAuditService,
	llmMgr *LLMManager,
	cfg func() config.AgentConfigModel,
	metrics telemetry.Metrics,
	logf func(string, ...any),
) *DigestService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &DigestService{
		repo:      dbsql.NewGenericRepo[appentities.AgentDigest](db),
		notif:     notif,
		publisher: notif,
		fleet:     fleet,
		audit:     audit,
		llm:       llmMgr,
		cfg:       cfg,
		metrics:   metrics,
		logf:      logf,
	}
}

// Generate produces, persists, and publishes one digest. kind is "daily"
// (scheduler) or "manual"; actor is the operator's user id for manual runs.
func (d *DigestService) Generate(ctx context.Context, kind string, actor int64) (*appentities.AgentDigest, error) {
	started := time.Now()
	cfg := d.cfg()
	windowHours := cfg.Digest.WindowHours
	if windowHours <= 0 {
		windowHours = 24
	}
	if kind == "weekly" {
		// The weekly digest is the management-cadence summary: a fixed 7-day
		// window regardless of the daily digest's configured look-back.
		windowHours = 168
	}

	findings, err := CollectFindings(ctx, FindingsInput{Now: started, WindowHours: windowHours, RuleFor: d.ruleFor},
		d.notif, d.fleet, d.audit)
	if err != nil {
		d.count("failed", "none")
		return nil, err
	}

	narrative, model, source := d.polish(ctx, findings, cfg)
	severity := MaxFindingSeverity(findings)

	findingsJSON, err := json.Marshal(findings)
	if err != nil {
		d.count("failed", source)
		return nil, fmt.Errorf("encode findings: %w", err)
	}

	now := time.Now()
	digest := appentities.AgentDigest{
		Kind:            kind,
		PeriodStart:     started.Add(-time.Duration(windowHours) * time.Hour).Unix(),
		PeriodEnd:       started.Unix(),
		FindingsJson:    string(findingsJSON),
		Severity:        severity,
		Narrative:       narrative,
		NarrativeLang:   digestLanguage(cfg),
		NarrativeSource: source,
		Model:           model,
		DurationMs:      time.Since(started).Milliseconds(),
		GeneratedBy:     actor,
		CreatedBy:       actor,
		CreatedAt:       now.Unix(),
		UpdatedBy:       actor,
		UpdatedAt:       now.Unix(),
	}
	id, err := d.repo.Create(ctx, "", digest)
	if err != nil {
		d.count("failed", source)
		return nil, fmt.Errorf("persist digest: %w", err)
	}
	digest.Id = int64(id)

	// Feed entry: best-effort, English (like every server-published notification);
	// the localized rendering lives on the Insight page the entry links to.
	if d.publisher != nil {
		counts := countBySeverity(findings)
		d.publisher.Publish(ctx, notification.Notification{
			Category: notification.CategorySystem,
			Severity: notification.Severity(severity),
			Title:    "Fleet digest",
			Body: fmt.Sprintf("%d finding(s) over the last %dh: %d critical, %d warning.",
				len(findings), windowHours, counts[string(notification.Critical)], counts[string(notification.Warning)]),
			Source:  "ai-digest",
			RefType: "agent_digest",
			RefId:   digest.Id,
			Link:    "#insight",
		})
	}

	if d.metrics != nil {
		d.metrics.Set(MetricAgentDigestDurationMs, nil, float64(digest.DurationMs))
	}
	d.count("ok", source)
	d.logf("agent digest #%d generated (%s, %d findings, narrative=%s, %dms)",
		digest.Id, kind, len(findings), source, digest.DurationMs)
	return &digest, nil
}

// polish asks the LLM to rewrite the findings as an operator briefing. Any
// failure returns ("", "", "none") — by design, never an error.
//
// The model receives PRE-RENDERED English lines, not the findings JSON: small
// models copy raw unix timestamps and field soup into garbled prose ("17:85 UTC
// on 2021-01-01", live-bench verbatim), while they rewrite plain sentences with
// the numbers already formatted essentially faithfully.
func (d *DigestService) polish(ctx context.Context, findings []Finding, cfg config.AgentConfigModel) (narrative, model, source string) {
	if d.llm == nil || !d.llm.Enabled() {
		return "", "", "none"
	}
	client, err := d.llm.Client()
	if err != nil {
		return "", "", "none"
	}
	lang := digestLanguage(cfg)

	llmCtx, cancel := context.WithTimeout(ctx, digestLLMTimeout)
	defer cancel()
	res, err := client.Chat(llmCtx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: digestSystemPrompt(lang)},
			{Role: "user", Content: renderFindingsForLLM(findings)},
		},
		Temperature: 0.3,
		MaxTokens:   d.llm.MaxTokens(),
	})
	d.countLLM("digest", err)
	if err != nil {
		d.logf("agent digest: llm polish skipped: %v", err)
		return "", "", "none"
	}
	text := strings.TrimSpace(res.Content)
	if text == "" {
		return "", "", "none"
	}
	return text, d.llm.ModelName(), "llm"
}

// digestSystemPrompt is the polish instruction. The grounding rule is absolute:
// the model may ONLY rephrase what the findings carry.
func digestSystemPrompt(lang string) string {
	return "You are writing the morning fleet briefing for a physical-security operator. " +
		"You will receive a bullet list of findings from their monitoring system. " +
		"Rewrite them as a short, plain briefing (at most 6 sentences). Rules: " +
		"use ONLY the facts, numbers, names, and times present in the findings — never add, estimate, or infer anything; " +
		"lead with the most severe items; " +
		"write in " + languageName(lang) + "."
}

func languageName(lang string) string {
	switch lang {
	case "ms":
		return "Malay (Bahasa Melayu)"
	case "zh":
		return "Simplified Chinese"
	case "ar":
		return "Arabic"
	}
	return "English"
}

// renderFindingsForLLM turns findings into plain English input lines for the
// polish call. English regardless of the output language (translation is the
// model's job); timestamps and severities pre-formatted so the model never sees
// a raw unix integer it could mangle.
func renderFindingsForLLM(findings []Finding) string {
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "- [%s] %s", strings.ToUpper(f.Severity), findingEnglish(f))
		b.WriteString("\n")
	}
	return b.String()
}

// findingEnglish is a compact English sentence per finding code — the LLM-input
// mirror of the frontend's agent.finding.* dictionary entries.
func findingEnglish(f Finding) string {
	p := func(key string) any { return f.Params[key] }
	ts := func(key string) string {
		if v, ok := f.Params[key].(int64); ok && v > 0 {
			return time.Unix(v, 0).Format("2006-01-02 15:04")
		}
		return "unknown"
	}
	switch f.Code {
	case FindingVolumeDelta:
		return fmt.Sprintf("%v events in the last %vh (%v%% vs the previous window; %v critical, %v warning).",
			p("total"), p("windowHours"), p("deltaPct"), p("critical"), p("warning"))
	case FindingCritical:
		return fmt.Sprintf("%v critical event(s) in the last %vh.", p("count"), p("windowHours"))
	case FindingBaselineSpike:
		return fmt.Sprintf("Activity spike at %s: %v events where up to %v is normal.", ts("bucketStart"), p("count"), p("expectedHi"))
	case FindingBaselineQuiet:
		return fmt.Sprintf("Unusual quiet at %s: %v events where at least %v is normal.", ts("bucketStart"), p("count"), p("expectedLo"))
	case FindingNodeBaselineSpike:
		return fmt.Sprintf("%v spiked at %s: %v events where its own normal tops out at %v.", p("source"), ts("bucketStart"), p("count"), p("expectedHi"))
	case FindingNodeBaselineQuiet:
		return fmt.Sprintf("%v went unusually quiet at %s: %v events where it normally produces at least %v.", p("source"), ts("bucketStart"), p("count"), p("expectedLo"))
	case FindingSourceAnomaly:
		return fmt.Sprintf("Sharp volume change from %v: %v events vs %v in the previous window.", p("source"), p("count"), p("prevCount"))
	case FindingTopSource:
		return fmt.Sprintf("Top source #%v: %v with %v events (%v%% of activity).", p("rank"), p("source"), p("count"), p("sharePct"))
	case FindingNoisySource:
		return fmt.Sprintf("%v raised %v events, %v%% unread — likely noise.", p("source"), p("count"), p("unreadPct"))
	case FindingNodeOffline:
		return fmt.Sprintf("Node %v (%v) is %v; last seen %s.", p("name"), p("nodeId"), p("status"), ts("lastSeenAt"))
	case FindingCertExpiring:
		return fmt.Sprintf("Certificate for node %v expires in %v day(s).", p("name"), p("daysLeft"))
	case FindingCertExpired:
		return fmt.Sprintf("Certificate for node %v expired %v day(s) ago.", p("name"), p("expiredDays"))
	case FindingFleetRule:
		return fmt.Sprintf("Fleet correlation rules fired %v time(s): %v.", p("count"), p("ruleTitles"))
	case FindingFeedGrowth:
		return fmt.Sprintf("The notification feed holds %v rows with retention disabled.", p("tableTotal"))
	case FindingAuditActivity:
		return fmt.Sprintf("Sensitive action %q occurred %v time(s).", p("action"), p("count"))
	case FindingSuggestedRule:
		return fmt.Sprintf("Recurring after-hours %v activity from %v (%v events on %v nights) — consider a fleet correlation rule.",
			p("category"), p("source"), p("count"), p("nights"))
	case FindingAllQuiet:
		return fmt.Sprintf("All quiet: nothing unusual in the last %vh.", p("windowHours"))
	}
	// Unknown code: fall back to the raw params rather than dropping the finding.
	raw, _ := json.Marshal(f.Params)
	return fmt.Sprintf("%s: %s", f.Code, raw)
}

func digestLanguage(cfg config.AgentConfigModel) string {
	switch cfg.Digest.Language {
	case "ms", "zh", "ar":
		return cfg.Digest.Language
	}
	return "en"
}

// Briefing is the report-facing summary: findings over an arbitrary window plus
// the optional LLM narrative. Reports render in cp1252 (Latin only), so the
// prose is always requested in English here regardless of the digest language —
// the PDF cannot carry zh/ar glyphs yet, and silent blank text is worse than
// English. Never returns an error for LLM trouble; Lines always renders.
type Briefing struct {
	Findings []Finding
	// Lines are the deterministic English sentences (one per finding).
	Lines []string
	// Narrative is the optional LLM prose ("" when off/failed); Model wrote it.
	Narrative string
	Model     string
}

// GenerateBriefing runs the narrator (and optional English polish) without
// persisting anything — the report is the artifact, not a digest row.
func (d *DigestService) GenerateBriefing(ctx context.Context, windowHours int) (Briefing, error) {
	findings, err := CollectFindings(ctx, FindingsInput{Now: time.Now(), WindowHours: windowHours, RuleFor: d.ruleFor},
		d.notif, d.fleet, d.audit)
	if err != nil {
		return Briefing{}, err
	}
	out := Briefing{Findings: findings}
	for _, f := range findings {
		out.Lines = append(out.Lines, findingEnglish(f))
	}
	var cfg config.AgentConfigModel
	if d.cfg != nil {
		cfg = d.cfg()
	}
	cfg.Digest.Language = "en" // cp1252 PDF: English prose only
	narrative, model, source := d.polish(ctx, findings, cfg)
	if source == "llm" {
		out.Narrative = narrative
		out.Model = model
	}
	return out, nil
}

// List returns stored digests newest-first.
func (d *DigestService) List(ctx context.Context, limit, offset uint64) ([]*appentities.AgentDigest, uint64, error) {
	if limit == 0 || limit > 100 {
		limit = 20
	}
	rows, total, err := d.repo.Get(ctx, "", limit, offset, nil,
		[]sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.DESC}})
	if err != nil {
		if isNoResultErr(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	return rows, total, nil
}

// Latest returns the most recent digest, or nil when none exist yet.
func (d *DigestService) Latest(ctx context.Context) (*appentities.AgentDigest, error) {
	rows, _, err := d.List(ctx, 1, 0)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// PurgeOld removes digests older than retentionDays (0 keeps everything).
func (d *DigestService) PurgeOld(ctx context.Context, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	n, err := d.repo.Delete(ctx, "", []sqldataenums.Filter{
		{FieldName: "CreatedAt", Compare: sqldataenums.LessThan, Value: cutoff},
	})
	if err != nil {
		if isNoResultErr(err) {
			return 0, nil
		}
		return 0, err
	}
	return int(n), nil
}

func (d *DigestService) count(outcome, narrative string) {
	if d.metrics != nil {
		d.metrics.Inc(MetricAgentDigestRunsTotal, telemetry.Labels{"outcome": outcome, "narrative": narrative})
	}
}

func (d *DigestService) countLLM(purpose string, err error) {
	if d.metrics == nil {
		return
	}
	outcome := "ok"
	if err != nil {
		outcome = "error"
		if strings.Contains(err.Error(), "context deadline exceeded") {
			outcome = "timeout"
		}
	}
	d.metrics.Inc(MetricAgentLLMRequestsTotal, telemetry.Labels{"purpose": purpose, "outcome": outcome})
}

func countBySeverity(findings []Finding) map[string]int {
	out := map[string]int{}
	for _, f := range findings {
		out[strings.ToLower(f.Severity)]++
	}
	return out
}
