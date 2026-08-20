package services

import (
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
)

func policy(id int64, scope, target, kind string, enforce bool, items ...*entities.FleetPolicyItem) *FleetPolicyDetail {
	for _, it := range items {
		it.PolicyId = id
	}
	return &FleetPolicyDetail{
		Policy: &entities.FleetPolicy{
			Id: id, Name: scope + "-policy", Enabled: true,
			Scope: scope, TargetId: target, NodeKind: kind, Enforce: enforce,
		},
		Items: items,
	}
}

func item(section, field, value string) *entities.FleetPolicyItem {
	return &entities.FleetPolicyItem{Section: section, Field: field, Value: value}
}

func fieldValue(t *testing.T, eff EffectivePolicy, section, field string) ResolvedField {
	t.Helper()
	for _, f := range eff.Sections[section] {
		if f.Field == field {
			return f
		}
	}
	t.Fatalf("field %s.%s not resolved; got %+v", section, field, eff.Sections)
	return ResolvedField{}
}

// A more specific scope must win the FIELD it names, and only that field. This is the
// whole reason the feature has scopes: "the estate keeps 30 days, this site keeps 90"
// must not force the site policy to restate every other setting.
func TestNodeScopeBeatsSiteBeatsFleetPerField(t *testing.T) {
	node := &entities.ManagedNode{NodeId: "n1", Kind: "camera", SiteId: 7}
	// Ids run OPPOSITE to specificity on purpose: the fleet policy is the newest. A
	// resolver that ordered by id alone — or that merely applied policies in the order it
	// found them — would then let the fleet default overwrite both overrides, and this
	// test is the only thing that notices.
	policies := []*FleetPolicyDetail{
		policy(30, entities.PolicyScopeFleet, "", "camera", false,
			item("notificationRetention", "days", "30"),
			item("continuity", "minCoveragePercent", "95"),
			item("health", "failureThreshold", "3")),
		policy(20, entities.PolicyScopeSite, "7", "camera", false,
			item("notificationRetention", "days", "90")),
		policy(10, entities.PolicyScopeNode, "n1", "camera", false,
			item("continuity", "minCoveragePercent", "80")),
	}

	eff := ResolveEffectivePolicy(node, policies)

	if got := fieldValue(t, eff, "notificationRetention", "days"); got.Value != int64(90) || got.Scope != entities.PolicyScopeSite {
		t.Fatalf("retention days: want 90 from site scope, got %v from %q", got.Value, got.Scope)
	}
	if got := fieldValue(t, eff, "continuity", "minCoveragePercent"); got.Value != float64(80) || got.Scope != entities.PolicyScopeNode {
		t.Fatalf("coverage: want 80 from node scope, got %v from %q", got.Value, got.Scope)
	}
	// The field NOTHING overrode must survive from the fleet policy — a scope must not
	// replace a section wholesale.
	if got := fieldValue(t, eff, "health", "failureThreshold"); got.Value != int64(3) || got.Scope != entities.PolicyScopeFleet {
		t.Fatalf("health threshold: want 3 from fleet scope, got %v from %q", got.Value, got.Scope)
	}
}

// A site policy must not reach a node that belongs to no site. SiteId 0 is "not placed",
// and treating it as a wildcard would leak one site's standard onto every standalone
// recorder in the estate.
func TestSitePolicySkipsNodeWithNoSite(t *testing.T) {
	placed := &entities.ManagedNode{NodeId: "n1", Kind: "camera", SiteId: 7}
	unplaced := &entities.ManagedNode{NodeId: "n2", Kind: "camera", SiteId: 0}
	policies := []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeSite, "7", "camera", false,
			item("notificationRetention", "days", "90")),
	}

	if eff := ResolveEffectivePolicy(placed, policies); eff.Empty() {
		t.Fatal("the node at site 7 should be governed by the site policy")
	}
	if eff := ResolveEffectivePolicy(unplaced, policies); !eff.Empty() {
		t.Fatalf("a node with no site must not inherit a site policy; got %+v", eff.Sections)
	}
}

// A policy for one kind of appliance must never be resolved against another. A door
// controller has no recording-continuity monitor, so a camera policy applied to it would
// report drift that no action could ever clear.
func TestPolicyDoesNotCrossNodeKinds(t *testing.T) {
	door := &entities.ManagedNode{NodeId: "d1", Kind: "door"}
	policies := []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "enabled", "true")),
	}
	if eff := ResolveEffectivePolicy(door, policies); !eff.Empty() {
		t.Fatalf("a camera policy must not govern a door node; got %+v", eff.Sections)
	}
}

// An empty Kind means camera — every node adopted before kinds existed is a mymatasan
// recorder, and a fleet policy must still reach it.
func TestBlankNodeKindIsTreatedAsCamera(t *testing.T) {
	legacy := &entities.ManagedNode{NodeId: "old", Kind: ""}
	policies := []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "enabled", "true")),
	}
	if eff := ResolveEffectivePolicy(legacy, policies); eff.Empty() {
		t.Fatal("a node adopted before kinds existed must still be governed as a camera")
	}
}

// Two policies at the SAME scope must resolve deterministically, or two instances of a
// clustered control plane can disagree about what the fleet wants.
func TestSameScopeTieBrokenByNewerPolicy(t *testing.T) {
	node := &entities.ManagedNode{NodeId: "n1", Kind: "camera"}
	policies := []*FleetPolicyDetail{
		policy(5, entities.PolicyScopeFleet, "", "camera", false, item("notificationRetention", "days", "30")),
		policy(2, entities.PolicyScopeFleet, "", "camera", false, item("notificationRetention", "days", "14")),
	}
	// Deliberately passed newest-first, so a resolver that merely took the last one seen
	// would get this wrong.
	if got := fieldValue(t, ResolveEffectivePolicy(node, policies), "notificationRetention", "days"); got.PolicyId != 5 {
		t.Fatalf("the newer policy (id 5) must win; got id %d value %v", got.PolicyId, got.Value)
	}
}

// Enforce belongs to the winning policy of each FIELD, not to the node. Pinning one
// appliance must not arm the report-only policy governing everything else.
func TestEnforceIsPerFieldNotPerNode(t *testing.T) {
	node := &entities.ManagedNode{NodeId: "n1", Kind: "camera"}
	policies := []*FleetPolicyDetail{
		policy(1, entities.PolicyScopeFleet, "", "camera", false,
			item("continuity", "minCoveragePercent", "95"),
			item("health", "failureThreshold", "3")),
		policy(2, entities.PolicyScopeNode, "n1", "camera", true,
			item("continuity", "minCoveragePercent", "80")),
	}
	eff := ResolveEffectivePolicy(node, policies)
	if got := fieldValue(t, eff, "continuity", "minCoveragePercent"); !got.Enforce {
		t.Fatal("the node-scoped enforcing policy should enforce its own field")
	}
	if got := fieldValue(t, eff, "health", "failureThreshold"); got.Enforce {
		t.Fatal("a field won by the report-only fleet policy must not be enforced")
	}
}

// A parked policy states nothing. Resolving a disabled policy would make "turn it off
// while we agree the change" indistinguishable from "delete it".
func TestDisabledPolicyIsNotResolved(t *testing.T) {
	node := &entities.ManagedNode{NodeId: "n1", Kind: "camera"}
	d := policy(1, entities.PolicyScopeFleet, "", "camera", false, item("continuity", "enabled", "true"))
	d.Policy.Enabled = false
	if eff := ResolveEffectivePolicy(node, []*FleetPolicyDetail{d}); !eff.Empty() {
		t.Fatalf("a disabled policy must govern nothing; got %+v", eff.Sections)
	}
}

func TestParsePolicyValueTypesAndBounds(t *testing.T) {
	section, _ := LookupPolicySection("notificationRetention")
	days, _ := section.Field("days")
	onlyRead, _ := section.Field("onlyRead")

	if v, err := ParsePolicyValue(days, "30"); err != nil || v != int64(30) {
		t.Fatalf("30 should parse as int64(30): %v %v", v, err)
	}
	// A select element posts strings; the same value written two ways must mean one thing.
	if v, err := ParsePolicyValue(days, `"30"`); err != nil || v != int64(30) {
		t.Fatalf(`"30" should parse as int64(30): %v %v`, v, err)
	}
	if _, err := ParsePolicyValue(days, "30.5"); err == nil {
		t.Fatal("a fractional value for a whole-number field must be rejected, not truncated")
	}
	if _, err := ParsePolicyValue(days, "0"); err == nil {
		t.Fatal("0 days is outside the declared bounds and the node would normalize it away")
	}
	if _, err := ParsePolicyValue(days, "yes"); err == nil {
		t.Fatal("a non-numeric value must be rejected")
	}
	if v, err := ParsePolicyValue(onlyRead, "true"); err != nil || v != true {
		t.Fatalf("true should parse as bool: %v %v", v, err)
	}
	if _, err := ParsePolicyValue(onlyRead, "1"); err != nil {
		t.Fatalf("ParseBool accepts 1: %v", err)
	}
}

// The comparison the whole reconciler rests on. A policy holds int64(95); a node's JSON
// decodes to float64(95). Comparing them with == or DeepEqual reports drift on a node
// that agrees exactly, and every compliant node goes red forever.
func TestPolicyValuesEqualAcrossJSONNumberShapes(t *testing.T) {
	if !policyValuesEqual(int64(95), float64(95)) {
		t.Fatal("int64(95) and the float64(95) a JSON decode produces are the same value")
	}
	if !policyValuesEqual(float64(95), float64(95)) {
		t.Fatal("95 should equal 95")
	}
	if policyValuesEqual(int64(95), float64(95.5)) {
		t.Fatal("95 and 95.5 are different values and the difference is the drift")
	}
	if !policyValuesEqual(true, true) {
		t.Fatal("true should equal true")
	}
	if policyValuesEqual(true, false) {
		t.Fatal("true and false are different values")
	}
	// A node that answered 1 where a bool was expected is a shape mismatch worth
	// reporting, not a truthy match to be quietly accepted.
	if policyValuesEqual(true, float64(1)) {
		t.Fatal("a number must never satisfy a boolean policy")
	}
	if policyValuesEqual(int64(1), true) {
		t.Fatal("a boolean must never satisfy a numeric policy")
	}
	// A field the node did not send arrives as nil. It must not match anything.
	if policyValuesEqual(int64(0), nil) {
		t.Fatal("a missing value must never compare equal")
	}
}

func TestValidateRejectsSectionsTheNodeKindDoesNotHave(t *testing.T) {
	_, _, err := validateFleetPolicy(SaveFleetPolicyRequest{
		Name: "doors", Scope: entities.PolicyScopeFleet, NodeKind: "door", Enabled: true,
		Items: []SaveFleetPolicyItem{{Section: "continuity", Field: "enabled", Value: "true"}},
	})
	if err == nil {
		t.Fatal("a policy naming a section its node kind does not have would be permanently drifted; it must be refused at save")
	}
}

func TestValidateRejectsUnknownField(t *testing.T) {
	_, _, err := validateFleetPolicy(SaveFleetPolicyRequest{
		Name: "sneaky", Scope: entities.PolicyScopeFleet, NodeKind: "camera", Enabled: true,
		Items: []SaveFleetPolicyItem{{Section: "continuity", Field: "ffmpegPath", Value: `"/bin/sh"`}},
	})
	if err == nil {
		t.Fatal("only catalog fields may be governed; a policy is not a way to post arbitrary JSON at a node")
	}
}

func TestValidateRequiresSiteAndNodeTargets(t *testing.T) {
	base := SaveFleetPolicyRequest{
		Name: "p", NodeKind: "camera", Enabled: true,
		Items: []SaveFleetPolicyItem{{Section: "continuity", Field: "enabled", Value: "true"}},
	}
	site := base
	site.Scope = entities.PolicyScopeSite
	if _, _, err := validateFleetPolicy(site); err == nil {
		t.Fatal("a site policy with no site would silently govern nothing")
	}
	node := base
	node.Scope = entities.PolicyScopeNode
	if _, _, err := validateFleetPolicy(node); err == nil {
		t.Fatal("a node policy with no node would silently govern nothing")
	}
	fleet := base
	fleet.Scope = entities.PolicyScopeFleet
	fleet.TargetId = "7"
	got, _, err := validateFleetPolicy(fleet)
	if err != nil {
		t.Fatalf("a fleet policy needs no target: %v", err)
	}
	if got.TargetId != "" {
		t.Fatal("a fleet policy's target is ignored, so it must be cleared rather than stored as a meaningless value")
	}
}
