package services

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// maxPolicies caps how many policies are resolved in one pass. Resolution is O(policies
// x nodes) and runs on a timer; the cap keeps a runaway import from turning the
// reconciler into the busiest thing in the process.
const maxPolicies = 500

// FleetPolicyDetail is a policy with the settings it governs.
type FleetPolicyDetail struct {
	Policy *entities.FleetPolicy       `json:"policy"`
	Items  []*entities.FleetPolicyItem `json:"items"`
}

// SaveFleetPolicyRequest creates or replaces a policy. Items are replaced wholesale, for
// the same reason a correlation rule's clauses are: a policy is a small declarative
// document and a half-applied edit is worse than one that replaces.
type SaveFleetPolicyRequest struct {
	Id          int64                 `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Enabled     bool                  `json:"enabled"`
	Scope       string                `json:"scope"`
	TargetId    string                `json:"targetId"`
	NodeKind    string                `json:"nodeKind"`
	Enforce     bool                  `json:"enforce"`
	Items       []SaveFleetPolicyItem `json:"items"`
}

// SaveFleetPolicyItem is one governed setting in a save request.
type SaveFleetPolicyItem struct {
	Section string `json:"section"`
	Field   string `json:"field"`
	Value   string `json:"value"`
}

// IFleetPolicyService owns fleet configuration policy: what the estate's settings ought
// to be, independently of what any node currently believes.
type IFleetPolicyService interface {
	List(ctx context.Context) ([]*FleetPolicyDetail, error)
	Save(ctx context.Context, req SaveFleetPolicyRequest, actor int64) (*FleetPolicyDetail, error)
	Delete(ctx context.Context, id int64) error
	// Resolve computes the effective desired configuration for one node.
	Resolve(ctx context.Context, node *entities.ManagedNode) (EffectivePolicy, error)
	// MarkEvaluated stamps the policies that contributed to a reconcile pass, so the UI
	// can say when a policy was last actually compared against reality rather than only
	// when it was last edited.
	MarkEvaluated(ctx context.Context, policyIds []int64, at int64) error
}

type fleetPolicyService struct {
	policies dbsql.IGenericRepo[entities.FleetPolicy]
	items    dbsql.IGenericRepo[entities.FleetPolicyItem]
}

// NewFleetPolicyService builds the policy service over the control-plane database.
func NewFleetPolicyService(db dbsql.IDbCrud) IFleetPolicyService {
	return &fleetPolicyService{
		policies: dbsql.NewGenericRepo[entities.FleetPolicy](db),
		items:    dbsql.NewGenericRepo[entities.FleetPolicyItem](db),
	}
}

// ResolvedField is one setting the fleet has an opinion about, and which policy won it.
type ResolvedField struct {
	Section string `json:"section"`
	Field   string `json:"field"`
	// Value is the parsed, typed desired value (bool / int64 / float64).
	Value any `json:"value"`
	// Display is Value rendered for humans and the audit trail.
	Display string `json:"display"`
	// PolicyId / PolicyName identify the policy that won this field, and Scope why it
	// won. Surfacing the winner is what makes an unexpected desired value diagnosable —
	// otherwise the operator sees a number their site policy does not contain and has to
	// guess which of the other policies produced it.
	PolicyId   int64  `json:"policyId"`
	PolicyName string `json:"policyName"`
	Scope      string `json:"scope"`
	// Enforce is the winning policy's enforce flag. It is per FIELD, not per node,
	// because a report-only fleet policy and an enforcing node override legitimately
	// coexist — that is precisely how an operator pins one appliance without arming the
	// whole estate.
	Enforce bool `json:"enforce"`
}

// EffectivePolicy is everything the fleet expects of one node.
type EffectivePolicy struct {
	NodeId string `json:"nodeId"`
	// Sections maps a catalog section id to the fields governed in it.
	Sections map[string][]ResolvedField `json:"sections"`
	// PolicyIds are the policies that contributed, for the evaluation stamp.
	PolicyIds []int64 `json:"policyIds"`
}

// Empty reports whether the fleet has no opinion at all about this node.
func (e EffectivePolicy) Empty() bool { return len(e.Sections) == 0 }

func (s *fleetPolicyService) List(ctx context.Context) ([]*FleetPolicyDetail, error) {
	policies, _, err := s.policies.Get(ctx, "", maxPolicies, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	out := make([]*FleetPolicyDetail, 0, len(policies))
	for _, p := range policies {
		items, ierr := s.itemsFor(ctx, p.Id)
		if ierr != nil {
			return nil, ierr
		}
		out = append(out, &FleetPolicyDetail{Policy: p, Items: items})
	}
	return out, nil
}

func (s *fleetPolicyService) itemsFor(ctx context.Context, policyId int64) ([]*entities.FleetPolicyItem, error) {
	// Get + Equal, never GetByForeign: that helper returns exactly ONE row, so a policy
	// with four governed settings would silently become a policy with one.
	items, _, err := s.items.Get(ctx, "", 200, 0,
		[]sqldataenums.Filter{{FieldName: "PolicyId", Compare: sqldataenums.Equal, Value: policyId}}, nil)
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	if items == nil {
		items = []*entities.FleetPolicyItem{}
	}
	return items, nil
}

// validateFleetPolicy refuses policies that cannot mean anything, and — more importantly
// — policies that would produce drift no node can ever clear.
func validateFleetPolicy(req SaveFleetPolicyRequest) (entities.FleetPolicy, []entities.FleetPolicyItem, error) {
	var zero entities.FleetPolicy
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return zero, nil, fmt.Errorf("the policy needs a name — it is what an operator reads next to a drifted node")
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if entities.PolicyScopeRank(scope) == 0 {
		return zero, nil, fmt.Errorf("scope must be %q, %q or %q", entities.PolicyScopeFleet, entities.PolicyScopeSite, entities.PolicyScopeNode)
	}
	target := strings.TrimSpace(req.TargetId)
	switch scope {
	case entities.PolicyScopeFleet:
		// A fleet policy applies to everything of its kind; a target would be ignored, and
		// an ignored field that looks meaningful is a trap.
		target = ""
	case entities.PolicyScopeSite:
		id, err := strconv.ParseInt(target, 10, 64)
		if err != nil || id <= 0 {
			return zero, nil, fmt.Errorf("a site policy must name a site")
		}
	case entities.PolicyScopeNode:
		if target == "" {
			return zero, nil, fmt.Errorf("a node policy must name a node")
		}
	}

	kind := NormalizePolicyNodeKind(req.NodeKind)
	if len(PolicySectionsForKind(kind)) == 0 {
		return zero, nil, fmt.Errorf("no settings can be governed on %q nodes yet", kind)
	}

	if len(req.Items) == 0 {
		return zero, nil, fmt.Errorf("a policy with no settings states nothing — add at least one")
	}

	seen := map[string]bool{}
	items := make([]entities.FleetPolicyItem, 0, len(req.Items))
	for _, it := range req.Items {
		sectionId := strings.TrimSpace(it.Section)
		section, ok := LookupPolicySection(sectionId)
		if !ok {
			return zero, nil, fmt.Errorf("unknown settings section %q", sectionId)
		}
		// A section the node kind does not have can never be satisfied. Refusing it here is
		// the difference between a validation error the author fixes in the dialog and a
		// permanently red node nobody can explain.
		if !section.AppliesTo(kind) {
			return zero, nil, fmt.Errorf("%s is not a setting %s nodes have", section.Label, kind)
		}
		field, ok := section.Field(strings.TrimSpace(it.Field))
		if !ok {
			return zero, nil, fmt.Errorf("%s has no governable setting %q", section.Label, it.Field)
		}
		key := sectionId + "." + field.Key
		if seen[key] {
			return zero, nil, fmt.Errorf("%s is set twice in this policy", field.Label)
		}
		seen[key] = true
		// Parse now, so an unparseable or out-of-bounds value is a dialog error rather than
		// a reconciler that skips the field silently at midnight.
		if _, err := ParsePolicyValue(field, it.Value); err != nil {
			return zero, nil, err
		}
		items = append(items, entities.FleetPolicyItem{
			Section: sectionId,
			Field:   field.Key,
			Value:   strings.TrimSpace(it.Value),
		})
	}

	return entities.FleetPolicy{
		Id:          req.Id,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		Enabled:     req.Enabled,
		Scope:       scope,
		TargetId:    target,
		NodeKind:    kind,
		Enforce:     req.Enforce,
	}, items, nil
}

func (s *fleetPolicyService) Save(ctx context.Context, req SaveFleetPolicyRequest, actor int64) (*FleetPolicyDetail, error) {
	policy, items, err := validateFleetPolicy(req)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	policy.UpdatedAt = now
	policy.UpdatedBy = actor

	if policy.Id == 0 {
		policy.CreatedAt = now
		policy.CreatedBy = actor
		id, cerr := s.policies.Create(ctx, "", policy)
		if cerr != nil {
			return nil, cerr
		}
		policy.Id = int64(id)
	} else {
		existing, gerr := s.policies.GetById(ctx, "", uint64(policy.Id))
		if gerr != nil || existing == nil {
			return nil, fmt.Errorf("policy not found")
		}
		policy.CreatedAt = existing.CreatedAt
		policy.CreatedBy = existing.CreatedBy
		// Deliberately NOT carried over: LastEvaluatedAt. An edited policy has not been
		// compared against anything yet, and a stale "checked 2 minutes ago" next to a
		// brand-new threshold is a claim the system cannot support.
		if _, uerr := s.policies.UpdateById(ctx, "", policy); uerr != nil {
			return nil, uerr
		}
	}

	if _, derr := s.items.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "PolicyId", Compare: sqldataenums.Equal, Value: policy.Id}}); derr != nil && !isNoResultErr(derr) {
		return nil, derr
	}
	for _, it := range items {
		it.PolicyId = policy.Id
		if _, cerr := s.items.Create(ctx, "", it); cerr != nil {
			return nil, cerr
		}
	}

	saved, err := s.policies.GetById(ctx, "", uint64(policy.Id))
	if err != nil {
		return nil, err
	}
	stored, err := s.itemsFor(ctx, policy.Id)
	if err != nil {
		return nil, err
	}
	return &FleetPolicyDetail{Policy: saved, Items: stored}, nil
}

func (s *fleetPolicyService) Delete(ctx context.Context, id int64) error {
	if _, err := s.items.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "PolicyId", Compare: sqldataenums.Equal, Value: id}}); err != nil && !isNoResultErr(err) {
		return err
	}
	if _, err := s.policies.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "Id", Compare: sqldataenums.Equal, Value: id}}); err != nil && !isNoResultErr(err) {
		return err
	}
	return nil
}

func (s *fleetPolicyService) MarkEvaluated(ctx context.Context, policyIds []int64, at int64) error {
	for _, id := range policyIds {
		p, err := s.policies.GetById(ctx, "", uint64(id))
		if err != nil || p == nil {
			continue
		}
		p.LastEvaluatedAt = at
		if _, err := s.policies.UpdateById(ctx, "", *p); err != nil {
			return err
		}
	}
	return nil
}

func (s *fleetPolicyService) Resolve(ctx context.Context, node *entities.ManagedNode) (EffectivePolicy, error) {
	if node == nil {
		return EffectivePolicy{}, fmt.Errorf("no node")
	}
	details, err := s.List(ctx)
	if err != nil {
		return EffectivePolicy{}, err
	}
	return ResolveEffectivePolicy(node, details), nil
}

// ResolveEffectivePolicy merges every policy that applies to a node into one desired
// configuration, most specific winner per field.
//
// It is a pure function of (node, policies) on purpose: it is the part with the
// interesting behaviour, and a pure function is the part that can be tested without a
// database, a node, or a control channel.
//
// Precedence is node > site > fleet, and within one scope the HIGHER ID wins — the most
// recently created policy. Something has to break the tie deterministically or the
// desired value depends on row order, which means it depends on the database, which means
// two instances of a clustered control plane can disagree about what the fleet wants.
func ResolveEffectivePolicy(node *entities.ManagedNode, policies []*FleetPolicyDetail) EffectivePolicy {
	eff := EffectivePolicy{NodeId: node.NodeId, Sections: map[string][]ResolvedField{}}
	kind := NormalizePolicyNodeKind(node.Kind)

	applicable := make([]*FleetPolicyDetail, 0, len(policies))
	for _, d := range policies {
		if d == nil || d.Policy == nil || !d.Policy.Enabled {
			continue
		}
		if NormalizePolicyNodeKind(d.Policy.NodeKind) != kind {
			continue
		}
		switch d.Policy.Scope {
		case entities.PolicyScopeFleet:
		case entities.PolicyScopeSite:
			// A node with no site (SiteId 0) is matched by NO site policy. Treating "no
			// site" as a wildcard would make a single site's standard leak onto every
			// standalone recorder in the estate.
			if node.SiteId == 0 || strconv.FormatInt(node.SiteId, 10) != d.Policy.TargetId {
				continue
			}
		case entities.PolicyScopeNode:
			if d.Policy.TargetId != node.NodeId {
				continue
			}
		default:
			continue
		}
		applicable = append(applicable, d)
	}

	// Sort weakest-first so a later assignment always outranks an earlier one and the
	// merge is a straight overwrite rather than a comparison at every field.
	sort.SliceStable(applicable, func(i, j int) bool {
		ri, rj := entities.PolicyScopeRank(applicable[i].Policy.Scope), entities.PolicyScopeRank(applicable[j].Policy.Scope)
		if ri != rj {
			return ri < rj
		}
		return applicable[i].Policy.Id < applicable[j].Policy.Id
	})

	winners := map[string]ResolvedField{}
	contributed := map[int64]bool{}
	for _, d := range applicable {
		for _, it := range d.Items {
			if it == nil {
				continue
			}
			section, ok := LookupPolicySection(it.Section)
			if !ok || !section.AppliesTo(kind) {
				// A section this build does not know, or one that does not exist on this
				// kind of node. Skipping is right: the alternative is comparing against a
				// path the node has never served and reporting drift nobody can clear.
				continue
			}
			field, ok := section.Field(it.Field)
			if !ok {
				continue
			}
			value, err := ParsePolicyValue(field, it.Value)
			if err != nil {
				// Validation rejects these on save, so reaching here means a row was
				// hand-edited or written by a version with different bounds. Dropping the
				// field is the safe half: an unparseable desired value must never be
				// pushed to a node.
				continue
			}
			winners[it.Section+"\x00"+field.Key] = ResolvedField{
				Section:    section.Id,
				Field:      field.Key,
				Value:      value,
				Display:    formatPolicyValue(value),
				PolicyId:   d.Policy.Id,
				PolicyName: d.Policy.Name,
				Scope:      d.Policy.Scope,
				Enforce:    d.Policy.Enforce,
			}
			contributed[d.Policy.Id] = true
		}
	}

	for _, w := range winners {
		eff.Sections[w.Section] = append(eff.Sections[w.Section], w)
	}
	// Stable field order within a section — the UI renders this list and a set-iteration
	// order would reshuffle the rows on every poll.
	for id := range eff.Sections {
		fields := eff.Sections[id]
		sort.Slice(fields, func(i, j int) bool { return fields[i].Field < fields[j].Field })
		eff.Sections[id] = fields
	}
	for id := range contributed {
		eff.PolicyIds = append(eff.PolicyIds, id)
	}
	sort.Slice(eff.PolicyIds, func(i, j int) bool { return eff.PolicyIds[i] < eff.PolicyIds[j] })
	return eff
}
