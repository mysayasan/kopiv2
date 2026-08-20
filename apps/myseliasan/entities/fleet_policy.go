package entities

// Policy scopes, most general first. A node's effective configuration is resolved by
// applying every matching policy in this order, so a more specific scope overrides a
// more general one FIELD BY FIELD (see services.ResolveEffectivePolicy).
//
// The ordering is the whole point of the feature. "Every camera node keeps 30 days of
// alerts" is a fleet statement; "except the airport site, which keeps 90 for the
// regulator" is a site statement; "except this one node whose disk is half the size" is
// a node statement. Without precedence an operator has to restate the entire
// configuration at every level, and the restatements drift apart — which is the exact
// problem this feature exists to detect.
const (
	// PolicyScopeFleet applies to every node of the policy's kind. TargetId is ignored.
	PolicyScopeFleet = "fleet"
	// PolicyScopeSite applies to nodes whose ManagedNode.SiteId matches TargetId
	// (the site's numeric id, as a string).
	PolicyScopeSite = "site"
	// PolicyScopeNode applies to exactly one node; TargetId is its ManagedNode.NodeId.
	PolicyScopeNode = "node"
)

// PolicyScopeRank orders scopes by specificity — higher wins a contested field. An
// unknown scope ranks 0 (below fleet) so a row written by a future version this build
// does not understand can never quietly outrank one it does.
func PolicyScopeRank(scope string) int {
	switch scope {
	case PolicyScopeNode:
		return 3
	case PolicyScopeSite:
		return 2
	case PolicyScopeFleet:
		return 1
	default:
		return 0
	}
}

// FleetPolicy is a statement of INTENT about how some part of the fleet should be
// configured — and, separately, whether the control plane is allowed to act on it.
//
// The control plane could already change any node's settings: apis/node_proxy.go tunnels
// an authenticated request to the node's own API. What it could not do was say what the
// settings OUGHT to be. So the configuration of a fleet lived only in the fleet: fifty
// appliances, each authoritative about itself, and the only way to answer "is continuity
// monitoring on everywhere?" was to open fifty screens. A node that was reimaged, restored
// from an old backup, or retuned by a site engineer at 2am simply stopped matching its
// siblings, and nothing anywhere noticed.
//
// A policy is the missing half. It is compared against reality on a schedule, and the
// comparison — not the push — is the product: the fleet answers what it is, and where it
// disagrees with what it should be.
type FleetPolicy struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name        string `json:"name" form:"name" query:"name" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	// Enabled false parks a policy without deleting it: it stops being resolved, so it
	// neither reports drift nor enforces. Useful while a change is being agreed.
	Enabled bool `json:"enabled" form:"enabled" query:"enabled"`
	// Scope is one of the PolicyScope* values; TargetId names what it applies to (a site
	// id for site scope, a nodeId for node scope, empty for fleet).
	Scope    string `json:"scope" form:"scope" query:"scope" validate:"required"`
	TargetId string `json:"targetId" form:"targetId" query:"targetId"`
	// NodeKind restricts the policy to one kind of appliance ("camera", "iot", "door").
	//
	// It is REQUIRED, and that is deliberate. The managed sections are a camera recorder's
	// settings; a door controller has no recording-continuity monitor and never will. A
	// policy without a kind would be permanently, unfixably "drifted" against every door
	// node in the fleet — an alarm that cannot be cleared, which operators learn to ignore,
	// after which the ones that matter are ignored too. Empty is normalized to "camera" on
	// save for the same reason ManagedNode.Kind treats empty as camera: every node adopted
	// before kinds existed is a mymatasan node.
	NodeKind string `json:"nodeKind" form:"nodeKind" query:"nodeKind"`
	// Enforce decides whether the control plane WRITES the desired value back to a node
	// that disagrees, or merely reports the disagreement.
	//
	// DEFAULT FALSE, and it stays false unless somebody deliberately turns it on. An
	// enforcing policy silently reverts the local change an engineer made while standing in
	// front of the appliance — possibly the change they made to stop it alarming at 3am —
	// and does so minutes later, from a screen they are not looking at. That behaviour is
	// occasionally what you want and never what you want by surprise. Report-only is
	// already the valuable half: it turns "fifty screens" into one answer.
	Enforce         bool  `json:"enforce" form:"enforce" query:"enforce"`
	LastEvaluatedAt int64 `json:"lastEvaluatedAt" form:"lastEvaluatedAt" query:"lastEvaluatedAt"`
	CreatedBy       int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt       int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy       int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt       int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// FleetPolicyItem is one managed setting inside a policy: a section, a field within it,
// and the value that field should hold.
//
// A policy names FIELDS, not whole sections, because most of a section is not a fleet
// decision. "Alert after two bad hours" is a fleet standard; the sweep interval is a
// per-appliance tuning knob someone may have a good local reason to have changed. A
// section-shaped policy would force the operator to have an opinion about every knob in
// order to have one about a single threshold, and would report drift on the knobs it
// never meant to govern.
type FleetPolicyItem struct {
	Id       int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	PolicyId int64 `json:"policyId" form:"policyId" query:"policyId" idx:"policy" validate:"required"`
	// Section is a catalog section id (see services.PolicySections) — "continuity",
	// "health", "tamper", "machineHealth", "notificationRetention".
	Section string `json:"section" form:"section" query:"section" validate:"required"`
	// Field is a dotted path within that section's JSON object, e.g.
	// "minCoveragePercent" or "disk.criticalPercent". Only paths the catalog declares
	// are accepted — a policy cannot be used to post arbitrary JSON at a node.
	Field string `json:"field" form:"field" query:"field" validate:"required"`
	// Value is the desired value as a JSON scalar literal (`95`, `true`, `"high"`).
	//
	// Stored as text rather than a typed column because the managed fields are int, float
	// and bool, and a column per type is three columns of which two are always null. The
	// catalog knows the type, so the string is decoded against it on the way in — a value
	// that will not parse is rejected at save time, not discovered by a reconciler at
	// midnight.
	Value string `json:"value" form:"value" query:"value"`
}
