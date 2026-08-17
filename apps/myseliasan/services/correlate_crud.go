package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// FleetRuleDetail is a correlation rule with its clauses.
type FleetRuleDetail struct {
	Rule    *entities.FleetRule         `json:"rule"`
	Clauses []*entities.FleetRuleClause `json:"clauses"`
}

// SaveFleetRuleRequest creates or replaces a correlation rule. Clauses are replaced wholesale:
// a rule is a small declarative document, and an edit that half-applies is worse than one that
// replaces.
type SaveFleetRuleRequest struct {
	Id              int64             `json:"id"`
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	WindowSeconds   int               `json:"windowSeconds"`
	GraceSeconds    int               `json:"graceSeconds"`
	CooldownSeconds int               `json:"cooldownSeconds"`
	Severity        string            `json:"severity"`
	Clauses         []SaveFleetClause `json:"clauses"`
}

// SaveFleetClause is one condition: something that must have happened, or something that must
// not have.
type SaveFleetClause struct {
	Mode     string `json:"mode"` // "required" | "absent"
	NodeId   string `json:"nodeId"`
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Match    string `json:"match"`
}

func (c *Correlator) List(ctx context.Context) ([]*FleetRuleDetail, error) {
	rules, _, err := c.rules.Get(ctx, "", 200, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	out := make([]*FleetRuleDetail, 0, len(rules))
	for _, r := range rules {
		cls, _, cerr := c.clauses.Get(ctx, "", 50, 0,
			[]sqldataenums.Filter{{FieldName: "RuleId", Compare: sqldataenums.Equal, Value: r.Id}}, nil)
		if cerr != nil && !isNoResultErr(cerr) {
			return nil, cerr
		}
		out = append(out, &FleetRuleDetail{Rule: r, Clauses: cls})
	}
	return out, nil
}

// validate refuses rules that cannot mean anything.
func validateFleetRule(req SaveFleetRuleRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("the rule needs a name — it is what an operator reads at 03:00")
	}
	required := 0
	for _, cl := range req.Clauses {
		switch strings.ToLower(strings.TrimSpace(cl.Mode)) {
		case "required":
			required++
		case "absent":
		default:
			return fmt.Errorf("a clause is either %q or %q, not %q", "required", "absent", cl.Mode)
		}
		if strings.TrimSpace(cl.Match) == "" && strings.TrimSpace(cl.Category) == "" {
			return fmt.Errorf("a clause that matches nothing in particular would match everything — give it a category or some text to look for")
		}
	}
	// A rule with nothing REQUIRED would fire on nothing at all, forever. It is worse than no
	// rule, because somebody will trust it.
	if required == 0 {
		return fmt.Errorf("the rule needs at least one thing that must HAPPEN — a rule made only of absences fires on nothing")
	}
	if req.WindowSeconds <= 0 {
		return fmt.Errorf("the rule needs a window: how close in time these events must be to count as one event")
	}
	return nil
}

func (c *Correlator) Save(ctx context.Context, req SaveFleetRuleRequest) (*FleetRuleDetail, error) {
	if err := validateFleetRule(req); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	rule := entities.FleetRule{
		Id:              req.Id,
		Name:            strings.TrimSpace(req.Name),
		Enabled:         req.Enabled,
		WindowSeconds:   req.WindowSeconds,
		GraceSeconds:    req.GraceSeconds,
		CooldownSeconds: req.CooldownSeconds,
		Severity:        defaultStr(req.Severity, "critical"),
		UpdatedAt:       now,
	}

	if rule.Id == 0 {
		rule.CreatedAt = now
		id, err := c.rules.Create(ctx, "", rule)
		if err != nil {
			return nil, err
		}
		rule.Id = int64(id)
	} else {
		existing, err := c.rules.GetById(ctx, "", uint64(rule.Id))
		if err != nil || existing == nil {
			return nil, fmt.Errorf("rule not found")
		}
		// The cooldown must survive an edit — otherwise editing a rule during an incident
		// re-arms it and it fires again immediately.
		rule.LastTriggeredAt = existing.LastTriggeredAt
		rule.CreatedAt = existing.CreatedAt
		if _, err := c.rules.UpdateById(ctx, "", rule); err != nil {
			return nil, err
		}
	}

	if _, err := c.clauses.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "RuleId", Compare: sqldataenums.Equal, Value: rule.Id}}); err != nil && !isNoResultErr(err) {
		return nil, err
	}
	for _, cl := range req.Clauses {
		if _, err := c.clauses.Create(ctx, "", entities.FleetRuleClause{
			RuleId:   rule.Id,
			Mode:     strings.ToLower(strings.TrimSpace(cl.Mode)),
			NodeId:   strings.TrimSpace(cl.NodeId),
			Kind:     strings.TrimSpace(cl.Kind),
			Category: strings.TrimSpace(cl.Category),
			Match:    strings.TrimSpace(cl.Match),
		}); err != nil {
			return nil, err
		}
	}

	// Drop any in-flight state: a rule that was armed under the OLD clauses must not fire under
	// the new ones.
	c.mu.Lock()
	delete(c.armed, rule.Id)
	for k := range c.seen {
		if k.ruleId == rule.Id {
			delete(c.seen, k)
		}
	}
	c.mu.Unlock()

	if err := c.Reload(ctx); err != nil {
		return nil, err
	}
	// Reload above refreshed THIS instance. Behind a load balancer the edit landed on
	// whichever instance the operator's browser reached, while the LEADER is the one that
	// actually fires rules — so the others have to be told.
	c.announceRulesChanged()
	cls, _, _ := c.clauses.Get(ctx, "", 50, 0,
		[]sqldataenums.Filter{{FieldName: "RuleId", Compare: sqldataenums.Equal, Value: rule.Id}}, nil)
	saved, _ := c.rules.GetById(ctx, "", uint64(rule.Id))
	return &FleetRuleDetail{Rule: saved, Clauses: cls}, nil
}

func (c *Correlator) Delete(ctx context.Context, id int64) error {
	if _, err := c.clauses.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "RuleId", Compare: sqldataenums.Equal, Value: id}}); err != nil && !isNoResultErr(err) {
		return err
	}
	if _, err := c.rules.DeleteById(ctx, "", uint64(id)); err != nil && !isNoResultErr(err) {
		return err
	}
	c.mu.Lock()
	delete(c.armed, id)
	c.mu.Unlock()
	if err := c.Reload(ctx); err != nil {
		return err
	}
	// See Save: a deleted rule that only this instance forgot would keep firing from the
	// leader, which is the more alarming direction of the two.
	c.announceRulesChanged()
	return nil
}

func defaultStr(v, fallback string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return fallback
}
