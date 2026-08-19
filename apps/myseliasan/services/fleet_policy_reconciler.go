package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/control"
)

// Compliance states for a node against its effective policy.
//
// UNKNOWN is not a rounding of "probably fine", and keeping it distinct from COMPLIANT is
// the single most important decision in this file. A configuration tool that reports an
// unreachable machine as compliant is worse than no tool: the estate shows all-green while
// the node that has been off the network for three weeks — the one most likely to have
// been reimaged, downgraded or restored from an ancient backup — is the one contributing
// the reassurance. Absence of evidence gets its own colour.
const (
	// ComplianceCompliant means every governed field was read back and matched.
	ComplianceCompliant = "compliant"
	// ComplianceDrifted means at least one governed field disagreed.
	ComplianceDrifted = "drifted"
	// ComplianceUnknown means the node could not be asked (offline, disconnected, error).
	ComplianceUnknown = "unknown"
	// ComplianceUnmanaged means no policy applies to this node at all.
	ComplianceUnmanaged = "unmanaged"
)

// Per-field drift outcomes.
const (
	FieldMatch = "match"
	FieldDrift = "drift"
	// FieldMissing means the node's response had no such field — an older or different
	// build. Reported separately from drift because the fix is different: a drifted field
	// is corrected by enforcing, a missing one by upgrading the node, and enforcing it
	// would post a field the node's decoder rejects.
	FieldMissing = "missing"
)

// Enforcement outcomes recorded per section.
const (
	EnforceSkipped    = "skipped"
	EnforceApplied    = "applied"
	EnforceFailed     = "failed"
	EnforceUnverified = "unverified"
)

// reconcileTimeout bounds one node's whole reconcile. A node that is connected but wedged
// must not hold the pass open — the sweep visits every node in turn.
const reconcileTimeout = 20 * time.Second

// FieldReport is one governed setting's verdict on one node.
type FieldReport struct {
	Section string `json:"section"`
	Field   string `json:"field"`
	Label   string `json:"label"`
	Unit    string `json:"unit,omitempty"`
	// Desired/Actual are rendered for display; Status is one of the Field* constants.
	Desired string `json:"desired"`
	Actual  string `json:"actual"`
	Status  string `json:"status"`
	// PolicyId / PolicyName / Scope say which policy asked for this, so an operator
	// looking at an unexpected desired value can go straight to the policy that set it.
	PolicyId   int64  `json:"policyId"`
	PolicyName string `json:"policyName"`
	Scope      string `json:"scope"`
	Enforce    bool   `json:"enforce"`
}

// SectionReport is one settings section's verdict on one node.
type SectionReport struct {
	Section string        `json:"section"`
	Label   string        `json:"label"`
	Fields  []FieldReport `json:"fields"`
	// Enforcement is one of the Enforce* constants; Error carries the reason when the
	// section could not be read or written.
	Enforcement string `json:"enforcement"`
	Error       string `json:"error,omitempty"`
}

// NodeCompliance is the whole verdict for one node.
type NodeCompliance struct {
	NodeId   string          `json:"nodeId"`
	NodeName string          `json:"nodeName"`
	NodeKind string          `json:"nodeKind"`
	SiteId   int64           `json:"siteId"`
	Status   string          `json:"status"`
	Sections []SectionReport `json:"sections"`
	// DriftCount is the number of fields that disagreed, so the fleet list can rank.
	DriftCount int `json:"driftCount"`
	// Reason explains an unknown verdict ("node is not connected").
	Reason       string `json:"reason,omitempty"`
	CheckedAt    int64  `json:"checkedAt"`
	AppliedCount int    `json:"appliedCount"`
}

// FleetCompliance is one full pass over the fleet.
type FleetCompliance struct {
	Nodes     []NodeCompliance `json:"nodes"`
	CheckedAt int64            `json:"checkedAt"`
	// Counts is a status→count summary for the header tiles.
	Counts map[string]int `json:"counts"`
}

// FleetPolicyReconciler compares every node's live settings against the policies that
// apply to it, and — only where a winning policy says to — writes the difference back.
//
// It reads through the SAME control channel the operator's own node screens use
// (ControlSender), so it is subject to the same connectivity, the same node-side
// authorization and the same audit choke point. It does not get a private path to the
// appliance, and that is deliberate: a reconciler with more reach than the UI is a
// reconciler nobody can reason about.
type FleetPolicyReconciler struct {
	policies IFleetPolicyService
	nodes    PolicyNodeLister
	sender   ControlSender
	audit    IAuditService
	logf     func(format string, args ...any)

	mu   sync.RWMutex
	last FleetCompliance
	// sweeping serialises passes. The 15-minute ticker and the operator's "check now"
	// button both call ReconcileAll, and on a large estate a sweep can still be running
	// when the next one is asked for. Two concurrent sweeps would double the settings
	// traffic every appliance sees and, where a policy enforces, have two writers pushing
	// to the same node at once — so a second pass waits for the first rather than joining
	// it.
	sweeping sync.Mutex
}

// NewFleetPolicyReconciler builds the reconciler. audit may be nil (tests).
// PolicyNodeLister is the only thing the reconciler needs from the node registry.
//
// Narrowed on purpose: INodeRegistry is the adoption, enrollment and revocation surface,
// and a reconciler holding all of it is one refactor away from being able to release a
// node. It is also what makes this testable without stubbing twenty methods that have
// nothing to do with configuration.
type PolicyNodeLister interface {
	List(ctx context.Context) ([]*entities.ManagedNode, error)
}

func NewFleetPolicyReconciler(policies IFleetPolicyService, nodes PolicyNodeLister, sender ControlSender, audit IAuditService, logf func(string, ...any)) *FleetPolicyReconciler {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &FleetPolicyReconciler{policies: policies, nodes: nodes, sender: sender, audit: audit, logf: logf}
}

// Last returns the most recent completed pass, for the API to serve without making the
// browser wait on fifty tunneled requests.
func (r *FleetPolicyReconciler) Last() FleetCompliance {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.last
}

// ReconcileAll sweeps the fleet once and stores the result.
func (r *FleetPolicyReconciler) ReconcileAll(ctx context.Context) (FleetCompliance, error) {
	r.sweeping.Lock()
	defer r.sweeping.Unlock()

	nodes, err := r.nodes.List(ctx)
	if err != nil {
		return FleetCompliance{}, err
	}
	details, err := r.policies.List(ctx)
	if err != nil {
		return FleetCompliance{}, err
	}

	now := time.Now().Unix()
	out := FleetCompliance{CheckedAt: now, Counts: map[string]int{}}
	touched := map[int64]bool{}

	for _, node := range nodes {
		if node == nil {
			continue
		}
		eff := ResolveEffectivePolicy(node, details)
		nc := r.reconcileNode(ctx, node, eff)
		nc.CheckedAt = now
		out.Nodes = append(out.Nodes, nc)
		out.Counts[nc.Status]++
		// Only stamp policies whose node was actually ASKED. A pass that reached nothing
		// but offline nodes has evaluated no policy, and saying otherwise would put a
		// fresh "last checked" next to a comparison that never happened.
		if nc.Status != ComplianceUnknown {
			for _, id := range eff.PolicyIds {
				touched[id] = true
			}
		}
	}
	sort.SliceStable(out.Nodes, func(i, j int) bool { return out.Nodes[i].NodeId < out.Nodes[j].NodeId })

	if len(touched) > 0 {
		ids := make([]int64, 0, len(touched))
		for id := range touched {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		if err := r.policies.MarkEvaluated(ctx, ids, now); err != nil {
			r.logf("fleet policy: stamp evaluation: %v", err)
		}
	}

	r.mu.Lock()
	r.last = out
	r.mu.Unlock()
	return out, nil
}

// ReconcileNode sweeps one node on demand (the "check now" button).
func (r *FleetPolicyReconciler) ReconcileNode(ctx context.Context, nodeID string) (NodeCompliance, error) {
	nodes, err := r.nodes.List(ctx)
	if err != nil {
		return NodeCompliance{}, err
	}
	var node *entities.ManagedNode
	for _, n := range nodes {
		if n != nil && n.NodeId == nodeID {
			node = n
			break
		}
	}
	if node == nil {
		return NodeCompliance{}, fmt.Errorf("node not found")
	}
	details, err := r.policies.List(ctx)
	if err != nil {
		return NodeCompliance{}, err
	}
	nc := r.reconcileNode(ctx, node, ResolveEffectivePolicy(node, details))
	nc.CheckedAt = time.Now().Unix()
	return nc, nil
}

func (r *FleetPolicyReconciler) reconcileNode(ctx context.Context, node *entities.ManagedNode, eff EffectivePolicy) NodeCompliance {
	nc := NodeCompliance{
		NodeId:   node.NodeId,
		NodeName: node.Name,
		NodeKind: NormalizePolicyNodeKind(node.Kind),
		SiteId:   node.SiteId,
		Status:   ComplianceUnmanaged,
	}
	if eff.Empty() {
		return nc
	}

	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()

	sectionIds := make([]string, 0, len(eff.Sections))
	for id := range eff.Sections {
		sectionIds = append(sectionIds, id)
	}
	sort.Strings(sectionIds)

	compliant := true
	anyRead := false
	for _, id := range sectionIds {
		section, ok := LookupPolicySection(id)
		if !ok {
			continue
		}
		sr := r.reconcileSection(ctx, node, section, eff.Sections[id])
		nc.Sections = append(nc.Sections, sr)
		// Whether the section was READ is len(Fields), not Error. Error is also set when a
		// section read fine but its enforcement could not be verified, and conflating the
		// two made a node whose value the appliance had quietly clamped report as
		// "unknown" — hiding a genuine, actionable disagreement behind the colour reserved
		// for machines nobody could reach.
		if len(sr.Fields) == 0 {
			compliant = false
			if nc.Reason == "" && sr.Error != "" {
				nc.Reason = sr.Error
			}
			continue
		}
		anyRead = true
		for _, f := range sr.Fields {
			if f.Status != FieldMatch {
				compliant = false
				nc.DriftCount++
			}
		}
		if sr.Enforcement == EnforceApplied {
			nc.AppliedCount++
		}
	}

	switch {
	case !anyRead:
		// Every section failed to read. The node was not compared against anything, so it
		// is unknown — never compliant, and never "drifted" either (a drift claim asserts
		// a value was seen and disagreed).
		nc.Status = ComplianceUnknown
	case compliant:
		nc.Status = ComplianceCompliant
	default:
		nc.Status = ComplianceDrifted
	}
	return nc
}

func (r *FleetPolicyReconciler) reconcileSection(ctx context.Context, node *entities.ManagedNode, section PolicySection, want []ResolvedField) SectionReport {
	sr := SectionReport{Section: section.Id, Label: section.Label, Enforcement: EnforceSkipped}

	current, err := r.readSection(ctx, node.NodeId, section)
	if err != nil {
		sr.Error = err.Error()
		return sr
	}

	drifted := make([]ResolvedField, 0, len(want))
	for _, w := range want {
		field, _ := section.Field(w.Field)
		fr := FieldReport{
			Section: section.Id, Field: w.Field, Label: field.Label, Unit: field.Unit,
			Desired: w.Display, PolicyId: w.PolicyId, PolicyName: w.PolicyName,
			Scope: w.Scope, Enforce: w.Enforce,
		}
		actual, present := policyGetPath(current, w.Field)
		switch {
		case !present:
			fr.Status = FieldMissing
			fr.Actual = ""
		case policyValuesEqual(w.Value, actual):
			fr.Status = FieldMatch
			fr.Actual = formatPolicyValue(actual)
		default:
			fr.Status = FieldDrift
			fr.Actual = formatPolicyValue(actual)
			if w.Enforce {
				drifted = append(drifted, w)
			}
		}
		sr.Fields = append(sr.Fields, fr)
	}

	if len(drifted) == 0 {
		return sr
	}

	if err := r.enforceSection(ctx, node, section, current, drifted); err != nil {
		sr.Enforcement = EnforceFailed
		sr.Error = err.Error()
		// A FAILED unattended write is audited too. It is the more interesting of the two
		// records: a policy that has been trying and failing to configure an appliance for
		// a month is invisible everywhere else — the compliance screen shows the node as
		// drifted, which looks like a node nobody has got around to, not one the system
		// has been unable to reach agreement with.
		r.recordEnforcement(ctx, node, section, drifted, EnforceFailed)
		return sr
	}

	// Read back. A 200 from the node is not proof the value stuck: every settings service
	// on the node normalizes what it is given — clamping out-of-range numbers, substituting
	// a default for a zero — and it is right to, because it must survive a hand-edited
	// database. Trusting the 200 would let the control plane report "applied" forever while
	// the node holds a different number, which is the precise failure a compliance tool
	// exists to make impossible.
	verified, verr := r.readSection(ctx, node.NodeId, section)
	if verr != nil {
		sr.Enforcement = EnforceUnverified
		sr.Error = verr.Error()
		return sr
	}
	applied := true
	for i, fr := range sr.Fields {
		if fr.Status != FieldDrift || !fr.Enforce {
			continue
		}
		actual, present := policyGetPath(verified, fr.Field)
		w, ok := findResolved(want, fr.Field)
		if present && ok && policyValuesEqual(w.Value, actual) {
			sr.Fields[i].Status = FieldMatch
			sr.Fields[i].Actual = formatPolicyValue(actual)
			continue
		}
		applied = false
		if present {
			sr.Fields[i].Actual = formatPolicyValue(actual)
		}
	}
	if applied {
		sr.Enforcement = EnforceApplied
	} else {
		sr.Enforcement = EnforceUnverified
		if sr.Error == "" {
			sr.Error = "the node accepted the change but did not store the requested value"
		}
	}
	r.recordEnforcement(ctx, node, section, drifted, sr.Enforcement)
	return sr
}

func findResolved(fields []ResolvedField, key string) (ResolvedField, bool) {
	for _, f := range fields {
		if f.Field == key {
			return f, true
		}
	}
	return ResolvedField{}, false
}

// readSection fetches a section's current values from the node over the control channel.
func (r *FleetPolicyReconciler) readSection(ctx context.Context, nodeID string, section PolicySection) (map[string]any, error) {
	resp, err := r.sender.SendRequest(ctx, nodeID, control.Request{
		Method: http.MethodGet,
		Path:   section.GetPath,
		// The reconciler reads and writes settings, which on the node is an admin action.
		// It asserts a role NAME; the node resolves it against its OWN roles and evaluates
		// its OWN matrix, exactly as the operator-driven proxy does. A node that does not
		// grant admin to this name refuses, and the section reports an error rather than
		// the control plane acquiring a private capability.
		Role:  "admin",
		Actor: "fleet-policy",
	})
	if err != nil {
		if errors.Is(err, ErrNodeOffline) || errors.Is(err, ErrNodeDisconnected) {
			return nil, fmt.Errorf("node is not connected")
		}
		return nil, err
	}
	if resp.Status == http.StatusNotFound {
		// The node does not serve this section — an older build, or a kind whose catalog
		// entry is wrong. Distinguished from drift on purpose.
		return nil, fmt.Errorf("this node does not have %s settings", strings.ToLower(section.Label))
	}
	if resp.Status == http.StatusTooManyRequests {
		// The node rate-limits the control plane like any other caller, and a tunneled
		// request carries no JWT — so every tunneled call shares one bucket per path.
		// A scheduled sweep is nowhere near the limit, but an operator pressing "check
		// now" repeatedly can reach it, and "node returned 429" is not something anyone
		// should have to decode at that moment.
		return nil, fmt.Errorf("the node is rate-limiting the control plane; this check will retry on the next sweep")
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return nil, fmt.Errorf("node returned %d reading %s", resp.Status, section.Id)
	}
	return decodeSectionBody(resp.Body, section.ReadAt)
}

// decodeSectionBody unwraps the node's standard {message,durationMs,result} envelope and
// then descends ReadAt to the object the section actually governs.
func decodeSectionBody(body []byte, readAt string) (map[string]any, error) {
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("unreadable response from node: %w", err)
	}
	if len(env.Result) == 0 {
		return nil, fmt.Errorf("node returned no settings")
	}
	obj := map[string]any{}
	if err := json.Unmarshal(env.Result, &obj); err != nil {
		return nil, fmt.Errorf("unreadable settings from node: %w", err)
	}
	if readAt == "" {
		return obj, nil
	}
	sub, ok := policyGetPath(obj, readAt)
	if !ok {
		return nil, fmt.Errorf("node returned no %s settings", readAt)
	}
	m, ok := sub.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("node returned an unexpected shape for %s", readAt)
	}
	return m, nil
}

// enforceSection writes the desired fields back, MERGED onto what the node currently has.
//
// The merge is not a nicety. Every settings endpoint on the node decodes the WHOLE section
// struct with DisallowUnknownFields, so a PUT carrying only the governed fields is a PUT
// carrying zero for all the others: enforcing "alert after 2 bad hours" would also set the
// sweep interval to 0 and the minimum coverage to 0%, silently disabling the very monitor
// the policy was tightening. The section is therefore read, the governed fields are
// overlaid, and the complete object goes back.
//
// For a section whose write path is narrower than its read path (retention), only the
// sub-object is sent — which is why ReadAt has already descended into it.
func (r *FleetPolicyReconciler) enforceSection(ctx context.Context, node *entities.ManagedNode, section PolicySection, current map[string]any, want []ResolvedField) error {
	merged := cloneJSONObject(current)
	for _, w := range want {
		if err := policySetPath(merged, w.Field, w.Value); err != nil {
			return err
		}
	}
	payload, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	resp, err := r.sender.SendRequest(ctx, node.NodeId, control.Request{
		Method:  http.MethodPut,
		Path:    section.PutPath,
		Role:    "admin",
		Actor:   "fleet-policy",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    payload,
	})
	if err != nil {
		if errors.Is(err, ErrNodeOffline) || errors.Is(err, ErrNodeDisconnected) {
			return fmt.Errorf("node is not connected")
		}
		return err
	}
	if resp.Status == http.StatusTooManyRequests {
		return fmt.Errorf("the node is rate-limiting the control plane; the change was not applied and will retry")
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return fmt.Errorf("node refused the change (%d)", resp.Status)
	}
	return nil
}

// cloneJSONObject deep-copies the decoded response so the merge never mutates the map the
// comparison already read from — the read-back check compares against what was ASKED for,
// and an aliased map would make it compare the desired value with itself and always pass.
func cloneJSONObject(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if m, ok := v.(map[string]any); ok {
			out[k] = cloneJSONObject(m)
			continue
		}
		if a, ok := v.([]any); ok {
			cp := make([]any, len(a))
			copy(cp, a)
			out[k] = cp
			continue
		}
		out[k] = v
	}
	return out
}

// recordEnforcement audits an unattended remote configuration change.
//
// This is the entry an investigator needs and the one nobody would think to write: the
// operator-driven tunnel is already audited at apis/node_proxy.go, but an enforced policy
// change has no operator behind it — it happens on a timer, possibly weeks after the
// policy was written, and without it the node's own trail says only "admin changed the
// setting" with no admin anywhere near the building.
func (r *FleetPolicyReconciler) recordEnforcement(ctx context.Context, node *entities.ManagedNode, section PolicySection, applied []ResolvedField, outcome string) {
	if r.audit == nil {
		return
	}
	changes := make([]string, 0, len(applied))
	policyIds := map[int64]string{}
	for _, w := range applied {
		changes = append(changes, w.Field+"="+w.Display)
		policyIds[w.PolicyId] = w.PolicyName
	}
	names := make([]string, 0, len(policyIds))
	for _, n := range policyIds {
		names = append(names, n)
	}
	sort.Strings(names)
	result := OutcomeSuccess
	if outcome != EnforceApplied {
		result = OutcomeError
	}
	r.audit.Record(ctx, AuditEntry{
		Action:     ActionPolicyEnforce,
		TargetType: "node",
		TargetId:   node.NodeId,
		Outcome:    result,
		Detail: fmt.Sprintf("fleet policy %s set %s on %s (%s)",
			strings.Join(names, ", "), strings.Join(changes, ", "), displayNodeName(node), section.Label),
		Metadata: map[string]any{
			"section":  section.Id,
			"changes":  changes,
			"policies": names,
			"outcome":  outcome,
		},
	})
}

func displayNodeName(node *entities.ManagedNode) string {
	if strings.TrimSpace(node.Name) != "" {
		return node.Name
	}
	return node.NodeId
}
