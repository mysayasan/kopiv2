package services

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// The catalog is the ONLY thing that decides what a fleet policy may govern.
//
// Without it, a policy is a mechanism for posting operator-supplied JSON at an
// arbitrary path on every appliance in the estate, on a timer, from a screen. The node
// would still authorize the request — the tunnel asserts a role and the node evaluates
// its own matrix — but "an admin may change this setting by hand" and "the control plane
// may rewrite this setting unattended on fifty machines" are not the same permission.
// Everything reachable this way is enumerated here, by field, with its type and bounds.
//
// Two whole categories are deliberately absent:
//
//   - Anything hardware-shaped (runtime/decoder: GPU device, thread counts, ffmpeg path).
//     These are properties of the box, not decisions about the fleet. A fleet-wide GPU
//     index is wrong on every machine that does not have that GPU.
//   - Notification ROUTING (webhook URLs, bot tokens, chat ids). Pushing those from the
//     control plane is credential distribution, which needs sealed transport and a
//     rotation story, not a settings comparison. Retention — how long a node keeps its
//     own alert history — carries no secret and is exactly the kind of estate-wide
//     standard a policy should express, so that part is in.

// Policy field kinds. Kept to the three scalar shapes the managed settings actually use;
// a string field would need per-field validation the catalog has no way to express, and
// none of the fleet-shaped settings is one.
const (
	PolicyFieldBool  = "bool"
	PolicyFieldInt   = "int"
	PolicyFieldFloat = "float"
)

// PolicyField is one governable setting inside a section.
type PolicyField struct {
	// Key is the dotted path within the section object ("minCoveragePercent",
	// "disk.criticalPercent").
	Key  string `json:"key"`
	Kind string `json:"kind"`
	// Min/Max bound a numeric field. Equal values (both zero) mean unbounded.
	//
	// These are the CONTROL PLANE's bounds and they are not the node's. The node
	// normalizes whatever it is given — clamping, or substituting a default for a zero —
	// and it is right to, because it must survive a hand-edited database. But a policy
	// whose desired value the node will normalize away is a policy that reports drift
	// forever and can never be satisfied, so values that would be silently adjusted are
	// rejected at save time instead.
	Min float64 `json:"min,omitempty"`
	Max float64 `json:"max,omitempty"`
	// Unit is a display hint for the UI ("%", "ms", "days", "hours", "").
	Unit string `json:"unit,omitempty"`
	// Label is the English display name; the UI translates by field key where it has a
	// translation and falls back to this.
	Label string `json:"label"`
}

// PolicySection is a node settings object the control plane can read and (when a policy
// enforces) write.
type PolicySection struct {
	Id string `json:"id"`
	// Label is the English display name of the section.
	Label string `json:"label"`
	// NodeKinds lists the ManagedNode.Kind values this section exists on. A section is
	// never read from, compared against, or pushed to a node of another kind.
	NodeKinds []string `json:"nodeKinds"`
	// GetPath is the node API path the current values are read from.
	GetPath string `json:"-"`
	// PutPath is where the merged object is written. Usually the same as GetPath.
	PutPath string `json:"-"`
	// ReadAt is the dotted path INSIDE the GET response's result where this section's
	// object lives; empty means the result itself.
	//
	// It exists because a node's read and write surfaces are not always symmetric:
	// retention is read as one field of GET /api/settings/notification but written on its
	// own to PUT /api/settings/notification/retention. Reading the whole notification
	// object and writing it back would mean the control plane round-trips the webhook URL
	// and Telegram bot token through itself on every reconcile — exactly the credential
	// handling this feature is built to avoid.
	ReadAt string        `json:"-"`
	Fields []PolicyField `json:"fields"`
}

// Field returns a section's field by key.
func (s PolicySection) Field(key string) (PolicyField, bool) {
	for _, f := range s.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return PolicyField{}, false
}

// AppliesTo reports whether this section exists on a node of the given kind. An empty
// kind means camera, exactly as ManagedNode.Kind documents — every node adopted before
// kinds existed is a mymatasan recorder.
func (s PolicySection) AppliesTo(nodeKind string) bool {
	k := NormalizePolicyNodeKind(nodeKind)
	for _, want := range s.NodeKinds {
		if want == k {
			return true
		}
	}
	return false
}

// NormalizePolicyNodeKind maps a stored/blank node kind onto a catalog kind.
func NormalizePolicyNodeKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	if k == "" {
		return "camera"
	}
	return k
}

// policySections is the catalog. Order is display order.
var policySections = []PolicySection{
	{
		Id:        "continuity",
		Label:     "Recording continuity",
		NodeKinds: []string{"camera"},
		GetPath:   "/api/settings/continuity",
		PutPath:   "/api/settings/continuity",
		Fields: []PolicyField{
			{Key: "enabled", Kind: PolicyFieldBool, Label: "Monitor recording continuity"},
			{Key: "minCoveragePercent", Kind: PolicyFieldFloat, Min: 1, Max: 100, Unit: "%", Label: "Minimum hourly coverage"},
			{Key: "failureThreshold", Kind: PolicyFieldInt, Min: 1, Max: 24, Label: "Bad hours before alerting"},
			{Key: "recoveryThreshold", Kind: PolicyFieldInt, Min: 1, Max: 24, Label: "Good hours before clearing"},
		},
	},
	{
		Id:        "health",
		Label:     "Camera reachability",
		NodeKinds: []string{"camera"},
		GetPath:   "/api/settings/health",
		PutPath:   "/api/settings/health",
		Fields: []PolicyField{
			{Key: "enabled", Kind: PolicyFieldBool, Label: "Monitor camera reachability"},
			{Key: "timeoutMs", Kind: PolicyFieldInt, Min: 500, Max: 60000, Unit: "ms", Label: "Probe timeout"},
			{Key: "failureThreshold", Kind: PolicyFieldInt, Min: 1, Max: 20, Label: "Failed probes before offline"},
			{Key: "recoveryThreshold", Kind: PolicyFieldInt, Min: 1, Max: 20, Label: "Good probes before online"},
		},
	},
	{
		Id:        "tamper",
		Label:     "Camera tamper",
		NodeKinds: []string{"camera"},
		GetPath:   "/api/settings/tamper",
		PutPath:   "/api/settings/tamper",
		Fields: []PolicyField{
			{Key: "enabled", Kind: PolicyFieldBool, Label: "Detect covered / frozen / moved cameras"},
			{Key: "failureThreshold", Kind: PolicyFieldInt, Min: 1, Max: 20, Label: "Abnormal samples before alerting"},
		},
	},
	{
		Id:        "machineHealth",
		Label:     "Appliance health",
		NodeKinds: []string{"camera"},
		GetPath:   "/api/settings/machine-health",
		PutPath:   "/api/settings/machine-health",
		Fields: []PolicyField{
			{Key: "enabled", Kind: PolicyFieldBool, Label: "Monitor CPU / memory / disk"},
			{Key: "cpu.warnPercent", Kind: PolicyFieldInt, Min: 1, Max: 100, Unit: "%", Label: "CPU warning"},
			{Key: "cpu.criticalPercent", Kind: PolicyFieldInt, Min: 1, Max: 100, Unit: "%", Label: "CPU critical"},
			{Key: "memory.warnPercent", Kind: PolicyFieldInt, Min: 1, Max: 100, Unit: "%", Label: "Memory warning"},
			{Key: "memory.criticalPercent", Kind: PolicyFieldInt, Min: 1, Max: 100, Unit: "%", Label: "Memory critical"},
			{Key: "disk.warnPercent", Kind: PolicyFieldInt, Min: 1, Max: 100, Unit: "%", Label: "Disk warning"},
			{Key: "disk.criticalPercent", Kind: PolicyFieldInt, Min: 1, Max: 100, Unit: "%", Label: "Disk critical"},
			{Key: "mitigation.enabled", Kind: PolicyFieldBool, Label: "Automatic disk mitigation"},
		},
	},
	{
		Id:        "notificationRetention",
		Label:     "Alert retention",
		NodeKinds: []string{"camera"},
		GetPath:   "/api/settings/notification",
		PutPath:   "/api/settings/notification/retention",
		ReadAt:    "retention",
		Fields: []PolicyField{
			{Key: "days", Kind: PolicyFieldInt, Min: 1, Max: 3650, Unit: "days", Label: "Keep alerts for"},
			{Key: "onlyRead", Kind: PolicyFieldBool, Label: "Only purge alerts already read"},
			{Key: "intervalHours", Kind: PolicyFieldInt, Min: 1, Max: 168, Unit: "hours", Label: "Purge sweep interval"},
		},
	},
}

// PolicySections returns the catalog in display order.
func PolicySections() []PolicySection {
	out := make([]PolicySection, len(policySections))
	copy(out, policySections)
	return out
}

// PolicySectionsForKind returns the sections a node of this kind actually has.
func PolicySectionsForKind(nodeKind string) []PolicySection {
	out := []PolicySection{}
	for _, s := range policySections {
		if s.AppliesTo(nodeKind) {
			out = append(out, s)
		}
	}
	return out
}

// PolicyNodeKinds lists the node kinds any catalog section covers, in display order.
// The UI offers only these when creating a policy: a kind with no governable section
// would produce a policy that can never contain an item.
func PolicyNodeKinds() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range policySections {
		for _, k := range s.NodeKinds {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// LookupPolicySection returns a catalog section by id.
func LookupPolicySection(id string) (PolicySection, bool) {
	for _, s := range policySections {
		if s.Id == id {
			return s, true
		}
	}
	return PolicySection{}, false
}

// ParsePolicyValue decodes a stored value literal against a field's declared type and
// bounds, returning the native Go value to compare and push.
//
// It is deliberately strict about type but tolerant about spelling: an int field accepts
// `30` and `"30"` (a select element posts strings) but not `30.5`, because "30.5 failed
// tolerantly into 30" is how a policy comes to say something nobody wrote.
func ParsePolicyValue(f PolicyField, raw string) (any, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("%s: a value is required", f.Key)
	}
	// Unwrap a JSON string literal so `"30"` and 30 behave the same.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var unquoted string
		if err := json.Unmarshal([]byte(s), &unquoted); err == nil {
			s = strings.TrimSpace(unquoted)
		}
	}
	switch f.Kind {
	case PolicyFieldBool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not true or false", f.Key, raw)
		}
		return b, nil
	case PolicyFieldInt:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a whole number", f.Key, raw)
		}
		if err := checkBounds(f, float64(n)); err != nil {
			return nil, err
		}
		return n, nil
	case PolicyFieldFloat:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a number", f.Key, raw)
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("%s: %q is not a finite number", f.Key, raw)
		}
		if err := checkBounds(f, v); err != nil {
			return nil, err
		}
		return v, nil
	default:
		return nil, fmt.Errorf("%s: unknown field type %q", f.Key, f.Kind)
	}
}

func checkBounds(f PolicyField, v float64) error {
	if f.Min == f.Max {
		return nil
	}
	if v < f.Min || v > f.Max {
		return fmt.Errorf("%s: %s must be between %s and %s", f.Key, trimNum(v), trimNum(f.Min), trimNum(f.Max))
	}
	return nil
}

func trimNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// getPath reads a dotted path out of a decoded JSON object. The second return is false
// when any segment is missing or is not an object — which the caller must distinguish
// from "present and zero", since a node that has never heard of a field and a node that
// has it set to 0 are different situations.
func policyGetPath(obj map[string]any, path string) (any, bool) {
	cur := any(obj)
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// setPath writes a dotted path into a decoded JSON object, creating intermediate objects
// only where none exists. It refuses to overwrite a non-object with an object, because
// doing so would silently discard whatever the node had there.
func policySetPath(obj map[string]any, path string, value any) error {
	segs := strings.Split(path, ".")
	cur := obj
	for i, seg := range segs[:len(segs)-1] {
		next, ok := cur[seg]
		if !ok || next == nil {
			created := map[string]any{}
			cur[seg] = created
			cur = created
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: %q is not an object on the node", path, strings.Join(segs[:i+1], "."))
		}
		cur = m
	}
	cur[segs[len(segs)-1]] = value
	return nil
}

// policyValuesEqual compares a desired value against what a node reported.
//
// Both sides arrive shaped by JSON, where every number is a float64 and nothing carries
// its original Go type, so a naive == or reflect.DeepEqual reports drift on a node that
// agrees exactly: the policy holds int64(95) and the node returned float64(95). That
// comparison is the single most load-bearing line in the reconciler — get it wrong in the
// lax direction and drift is never reported; get it wrong in the strict direction and
// every compliant node is flagged, forever, and the feature is turned off.
//
// Numbers are therefore compared numerically and exactly (no epsilon: these are
// thresholds an operator typed, not measurements). Bools are compared as bools and are
// never equal to a number — a node that returned 1 where a bool was expected is a shape
// mismatch worth reporting, not a truthy match.
func policyValuesEqual(want any, got any) bool {
	switch w := want.(type) {
	case bool:
		g, ok := got.(bool)
		return ok && g == w
	case int64:
		g, ok := numericValue(got)
		return ok && g == float64(w)
	case float64:
		g, ok := numericValue(got)
		return ok && g == w
	default:
		return false
	}
}

// numericValue extracts a float from the shapes a JSON decode can produce. json.Number
// is included because a decoder configured with UseNumber produces it, and the node
// response decoder is one edit away from being so configured.
func numericValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// formatPolicyValue renders a value for display and for the audit trail.
func formatPolicyValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		return strconv.FormatBool(t)
	case string:
		return t
	default:
		if f, ok := numericValue(v); ok {
			return trimNum(f)
		}
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}
