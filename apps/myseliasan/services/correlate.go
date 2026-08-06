package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// The cross-domain correlator. This is the reason the fourth app exists.
//
//	Motion on Camera 3 (a mymatasan node)
//	AND a door contact opening (a myiotsan node)
//	AND no badge swipe (a myiotsan node)
//	-> intrusion
//
// mymatasan cannot see your door sensors. myiotsan cannot see your cameras. A cloud IoT platform
// can see neither. Only the control plane — which already receives every node's events in one
// feed — is in a position to notice the conjunction.
//
// And the conjunction is the whole value. A camera's motion alert at 03:00 is noise: a moth, a
// spider, headlights through a window. A door contact at 03:00 is noise: cleaners, deliveries,
// wind. The two together, with no badge swipe, is not noise. Correlation is how a fleet of noisy
// sensors becomes one trustworthy signal.
//
// THE GRACE DELAY IS THE HARD PART, and it is why this is not three lines of boolean algebra.
// The canonical rule is "door opened AND no badge swipe". But a badge reader reports over a
// network, through a controller, into a hub — it is routinely a second or two BEHIND the door
// contact it just authorised. Fire the moment the door opens, and the rule cries intrusion on
// every legitimate badge entry, all day, until somebody turns it off in disgust. So when the
// required clauses are all satisfied, the correlator does not fire: it ARMS, waits out the
// grace period, and only then asks whether the absence really held. Waiting five seconds costs
// nothing. Not waiting costs the feature.

// NodeEvent is one node event, flattened to what a rule can match on.
type NodeEvent struct {
	NodeId   string
	Kind     string // the node's kind: "camera" or "iot"
	Category string
	Title    string
	Body     string
	At       time.Time
}

// Correlator evaluates fleet rules over the stream of node events.
type Correlator struct {
	rules   dbsql.IGenericRepo[entities.FleetRule]
	clauses dbsql.IGenericRepo[entities.FleetRuleClause]
	notify  *notification.Service
	nodes   func(ctx context.Context, nodeId string) string // node id -> kind
	metrics telemetry.Metrics
	logf    func(format string, args ...any)
	enrich  func(ctx context.Context, ruleName string) string

	mu     sync.Mutex
	cached []*ruleWithClauses
	// seen holds recent events per (rule, clause), so a required clause can be satisfied by
	// something that happened a moment ago rather than only by the event in hand.
	seen map[clauseKey]time.Time
	// armed holds rules whose required clauses are all satisfied and which are waiting out their
	// grace period before deciding whether the absent clauses really are absent.
	armed map[int64]time.Time
}

type clauseKey struct {
	ruleId   int64
	clauseId int64
}

type ruleWithClauses struct {
	rule    entities.FleetRule
	clauses []entities.FleetRuleClause
}

func NewCorrelator(
	db dbsql.IDbCrud,
	notify *notification.Service,
	nodeKind func(ctx context.Context, nodeId string) string,
	logf func(string, ...any),
) *Correlator {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Correlator{
		rules:   dbsql.NewGenericRepo[entities.FleetRule](db),
		clauses: dbsql.NewGenericRepo[entities.FleetRuleClause](db),
		notify:  notify,
		nodes:   nodeKind,
		logf:    logf,
		seen:    map[clauseKey]time.Time{},
		armed:   map[int64]time.Time{},
	}
}

// SetMetrics wires a recorder in. Optional — a correlator with none still works and the tests
// construct one directly. A separate setter rather than a constructor arg so the ten tests that
// call NewCorrelator do not all have to grow a nil.
func (c *Correlator) SetMetrics(m telemetry.Metrics) { c.metrics = m }

// SetEnricher wires an optional context provider appended to a fired rule's
// notification body — e.g. "this rule fired 3 times in the last 7 days". The
// contract is strict because this sits in the ALERT PATH: the enricher must be
// deterministic (DB reads only, never an LLM), it runs under a hard timeout,
// and any failure or overrun costs only the extra sentence, never the alert.
func (c *Correlator) SetEnricher(fn func(ctx context.Context, ruleName string) string) { c.enrich = fn }

// enrichTimeout bounds the enricher. An alert that waits on anything slower
// than a couple of indexed queries is an alert somebody stopped trusting.
const enrichTimeout = 2 * time.Second

// HasRuleFor reports whether any cached rule already carries a REQUIRED clause
// matching this node+category — the digest's suggested-rule detector uses it to
// avoid proposing a rule the operator already wrote. Advisory (cache freshness
// = last Reload), which is exactly good enough for a suggestion.
func (c *Correlator) HasRuleFor(nodeId, category string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, rw := range c.cached {
		for _, cl := range rw.clauses {
			if !strings.EqualFold(cl.Mode, "required") {
				continue
			}
			nodeMatch := cl.NodeId == "" || strings.EqualFold(cl.NodeId, nodeId)
			catMatch := cl.Category == "" || strings.EqualFold(cl.Category, category)
			if nodeMatch && catMatch && (cl.NodeId != "" || cl.Category != "") {
				return true
			}
		}
	}
	return false
}

// Reload refreshes the rule cache.
func (c *Correlator) Reload(ctx context.Context) error {
	rules, _, err := c.rules.Get(ctx, "", 200, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return err
	}
	out := make([]*ruleWithClauses, 0, len(rules))
	for _, r := range rules {
		cls, _, cerr := c.clauses.Get(ctx, "", 50, 0,
			[]sqldataenums.Filter{{FieldName: "RuleId", Compare: sqldataenums.Equal, Value: r.Id}}, nil)
		if cerr != nil && !isNoResultErr(cerr) {
			return cerr
		}
		rw := &ruleWithClauses{rule: *r}
		for _, cl := range cls {
			rw.clauses = append(rw.clauses, *cl)
		}
		out = append(out, rw)
	}
	c.mu.Lock()
	c.cached = out
	c.mu.Unlock()
	return nil
}

// Observe takes one node event and advances every rule it touches.
//
// It NEVER fires here. Satisfying the required clauses only ARMS the rule; the decision waits
// for the grace period, because an absence you have not waited for is not an absence — it is
// just a race with the badge reader.
func (c *Correlator) Observe(ctx context.Context, e NodeEvent) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	if e.Kind == "" && c.nodes != nil {
		e.Kind = c.nodes(ctx, e.NodeId)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, rw := range c.cached {
		if !rw.rule.Enabled {
			continue
		}
		for _, cl := range rw.clauses {
			if !clauseMatches(cl, e) {
				continue
			}
			c.seen[clauseKey{ruleId: rw.rule.Id, clauseId: cl.Id}] = e.At
		}

		// An ABSENT clause that just matched disarms a pending rule outright: the badge swipe
		// arrived, so this was a legitimate entry after all, and no alert should ever be raised.
		if armedAt, ok := c.armed[rw.rule.Id]; ok {
			if c.absentClauseFired(rw, armedAt, e) {
				delete(c.armed, rw.rule.Id)
				c.logf("rule %q disarmed: %q arrived inside the grace period — this was authorised", rw.rule.Name, e.Title)
				continue
			}
		}

		// Arm the rule when everything required is present and recent.
		if _, already := c.armed[rw.rule.Id]; !already && c.requiredSatisfied(rw, e.At) {
			c.armed[rw.rule.Id] = e.At
			c.logf("rule %q armed — waiting %ds to see whether the absent conditions hold", rw.rule.Name, graceOf(rw.rule))
		}
	}
}

// absentClauseFired reports whether this event satisfies one of the rule's ABSENT clauses inside
// the window that matters — which means the rule must not fire.
func (c *Correlator) absentClauseFired(rw *ruleWithClauses, armedAt time.Time, e NodeEvent) bool {
	window := time.Duration(rw.rule.WindowSeconds) * time.Second
	for _, cl := range rw.clauses {
		if !strings.EqualFold(cl.Mode, "absent") {
			continue
		}
		if !clauseMatches(cl, e) {
			continue
		}
		// The badge swipe counts if it happened anywhere in the window leading up to the trigger,
		// or in the grace period after it — a reader that is two seconds late still authorised
		// the door.
		if e.At.After(armedAt.Add(-window)) {
			return true
		}
	}
	return false
}

// requiredSatisfied reports whether every required clause has been seen inside the window.
func (c *Correlator) requiredSatisfied(rw *ruleWithClauses, now time.Time) bool {
	window := time.Duration(rw.rule.WindowSeconds) * time.Second
	if window <= 0 {
		window = time.Minute
	}
	required := 0
	for _, cl := range rw.clauses {
		if !strings.EqualFold(cl.Mode, "required") {
			continue
		}
		required++
		at, ok := c.seen[clauseKey{ruleId: rw.rule.Id, clauseId: cl.Id}]
		if !ok || now.Sub(at) > window {
			return false
		}
	}
	// A rule with no required clauses would fire on nothing at all, forever. Refuse it here as
	// well as in validation — a rule that fires on nothing is worse than no rule, because
	// somebody will trust it.
	return required > 0
}

// Sweep fires the rules whose grace period has elapsed with their absent conditions still
// holding. It runs on a ticker, because the decision is made by the passage of time rather than
// by an event — an absence does not arrive.
func (c *Correlator) Sweep(ctx context.Context) {
	now := time.Now()

	c.mu.Lock()
	type pending struct {
		rw      *ruleWithClauses
		armedAt time.Time
	}
	var fire []pending
	for _, rw := range c.cached {
		armedAt, ok := c.armed[rw.rule.Id]
		if !ok {
			continue
		}
		if now.Sub(armedAt) < time.Duration(graceOf(rw.rule))*time.Second {
			continue // still waiting to see whether the badge swipe shows up
		}
		delete(c.armed, rw.rule.Id)

		// The cooldown, which survives restart via LastTriggeredAt.
		if rw.rule.CooldownSeconds > 0 && rw.rule.LastTriggeredAt > 0 {
			if now.Unix()-rw.rule.LastTriggeredAt < int64(rw.rule.CooldownSeconds) {
				continue
			}
		}
		fire = append(fire, pending{rw: rw, armedAt: armedAt})
	}
	c.mu.Unlock()

	for _, p := range fire {
		c.fire(ctx, p.rw, p.armedAt)
	}
}

func (c *Correlator) fire(ctx context.Context, rw *ruleWithClauses, armedAt time.Time) {
	now := time.Now()

	rule := rw.rule
	rule.LastTriggeredAt = now.Unix()
	if _, err := c.rules.UpdateById(ctx, "", rule); err != nil {
		c.logf("could not persist the cooldown for fleet rule %q — it may re-fire after a restart: %v", rule.Name, err)
	}
	c.mu.Lock()
	for _, r := range c.cached {
		if r.rule.Id == rule.Id {
			r.rule.LastTriggeredAt = rule.LastTriggeredAt
		}
	}
	c.mu.Unlock()

	body := c.explain(rw, armedAt)
	if c.enrich != nil {
		enrichCtx, cancel := context.WithTimeout(ctx, enrichTimeout)
		if extra := c.enrich(enrichCtx, rule.Name); extra != "" {
			body += "\n" + extra
		}
		cancel()
	}
	c.logf("FLEET RULE FIRED: %s", rule.Name)

	if c.metrics != nil {
		c.metrics.Inc(MetricFleetRuleFiredTotal, telemetry.Labels{"severity": defaultStr(rule.Severity, "critical")})
	}
	if c.notify != nil {
		c.notify.Publish(ctx, notification.Notification{
			Category: notification.CategorySystem,
			Severity: severityOf(rule.Severity),
			Title:    rule.Name,
			Body:     body,
			Source:   "fleet-rule",
			Data:     map[string]any{"fleetRuleId": rule.Id},
		})
	}
}

// explain writes the sentence an operator reads at 03:00. It names what DID happen and what did
// NOT — because "correlation rule 4 fired" tells nobody anything, and the absence is half the
// finding.
func (c *Correlator) explain(rw *ruleWithClauses, armedAt time.Time) string {
	var did, didnt []string
	for _, cl := range rw.clauses {
		label := strings.TrimSpace(cl.Match)
		if label == "" {
			label = cl.Category
		}
		if cl.NodeId != "" {
			label += " on " + cl.NodeId
		}
		if strings.EqualFold(cl.Mode, "absent") {
			didnt = append(didnt, label)
		} else {
			did = append(did, label)
		}
	}
	s := strings.Join(did, ", and ")
	if len(didnt) > 0 {
		s += " — with no " + strings.Join(didnt, " and no ")
	}
	return fmt.Sprintf("%s (within %ds)", s, rw.rule.WindowSeconds)
}

// clauseMatches reports whether an event satisfies a clause.
func clauseMatches(cl entities.FleetRuleClause, e NodeEvent) bool {
	if cl.NodeId != "" && !strings.EqualFold(cl.NodeId, e.NodeId) {
		return false
	}
	if cl.Kind != "" && !strings.EqualFold(cl.Kind, e.Kind) {
		return false
	}
	if cl.Category != "" && !strings.EqualFold(cl.Category, e.Category) {
		return false
	}
	if m := strings.TrimSpace(cl.Match); m != "" {
		hay := strings.ToLower(e.Title + " " + e.Body)
		if !strings.Contains(hay, strings.ToLower(m)) {
			return false
		}
	}
	return true
}

func graceOf(r entities.FleetRule) int {
	if r.GraceSeconds > 0 {
		return r.GraceSeconds
	}
	// A default that is long enough for a slow badge reader and short enough that a real
	// intruder has not left the building.
	return 5
}

func severityOf(s string) notification.Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return notification.Critical
	case "info":
		return notification.Info
	default:
		return notification.Warning
	}
}

func isNoResultErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no result found") || strings.Contains(msg, "total affected: 0")
}
