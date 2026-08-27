package services

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// flows.go owns the flow GRAPH MODEL, its validation, and the CRUD service. The runtime that
// compiles and executes a graph is flow_runtime.go; the JavaScript substrate is flow_eval.go.

// --- The graph document ---------------------------------------------------------------------
//
// A flow is authored on the canvas and stored as ONE JSON document (IotFlow.Graph). These are its
// shapes. Node device references use natural keys (deviceKey + telemetry key), never database ids,
// so a saved flow is portable across installs.

type flowGraph struct {
	Nodes []flowNode `json:"nodes"`
	Wires []flowWire `json:"wires"`
}

type flowNode struct {
	Id     string         `json:"id"`
	Type   string         `json:"type"`
	X      float64        `json:"x"`
	Y      float64        `json:"y"`
	Config map[string]any `json:"config,omitempty"`
}

type flowWire struct {
	From flowPort `json:"from"`
	To   flowPort `json:"to"`
}

// flowPort names an endpoint of a wire: a node and (for a future multi-port node) which port.
type flowPort struct {
	Node string `json:"node"`
	Port int    `json:"port,omitempty"`
}

// The P1 node palette. Structural nodes (input/output) are declarative config; the code-bearing
// nodes (function/expression/switch) run through the goja sandbox.
const (
	nodeDeviceTelemetry = "device_telemetry" // input: emits when a reading for (deviceKey,key) arrives
	nodeFunction        = "function"         // transform: arbitrary sandboxed JS body ending in `return msg`
	nodeExpression      = "expression"       // transform: a JS expression → msg.payload
	nodeScale           = "scale"            // transform: payload*factor + offset
	nodeThreshold       = "threshold"        // logic: pass only if payload <op> value
	nodeSwitch          = "switch"           // logic: pass only if a JS predicate is truthy
	nodeDeadband        = "deadband"         // logic: pass only if payload moved >= delta since last pass
	nodeThrottle        = "throttle"         // logic: pass at most once per N seconds (rate limit)
	nodeDebug           = "debug"            // output: record the message for the inspector (a sink)
	nodeNotify          = "notify"           // output: publish a notification (a sink)
	nodeCommand         = "command"          // output: actuate a device via the GUARDED path (a sink)
	nodeDerivedMetric   = "derived_metric"   // output: persist a computed value as a telemetry series (a sink)
	nodeMqttOut         = "mqtt_out"         // output: publish the payload to an MQTT topic (a sink)
)

// codeBearing reports whether a node type runs a script in the sandbox.
func codeBearing(t string) bool {
	return t == nodeFunction || t == nodeExpression || t == nodeSwitch
}

// nodeScript returns the JavaScript body a code-bearing node compiles to, and whether it has one.
//
// It exists so there is exactly ONE definition of what a node's script IS. The runtime compiles
// it to run the flow; the save path compiles it to refuse a flow that cannot run. If those two
// built the wrapper text separately they would drift, and the drift would show up as a flow that
// validated on save and failed on the worker — which is the failure this whole seam exists to
// remove.
func nodeScript(n *flowNode) (string, bool) {
	switch n.Type {
	case nodeFunction:
		return cfgString(n.Config, "code"), true
	case nodeExpression:
		return "return (" + cfgString(n.Config, "expr") + ");", true
	case nodeSwitch:
		return "if (" + cfgString(n.Config, "predicate") + ") { return msg; } return null;", true
	}
	return "", false
}

// checkScripts compiles every code-bearing node in a graph and throws the sandbox away.
//
// WHY AT SAVE. flows.go has always claimed that "validation runs at save and again at compile, so
// a bad graph can neither be stored nor run" — and for the graph's SHAPE that was true. For its
// SCRIPTS it was not: a function node with a typo in it saved, enabled, listed as enabled, and
// then failed to compile on the worker, where the only trace was one INFO log line. The operator
// had an alerting flow that was never going to run and nothing anywhere said so. A missed alert
// is the one failure a monitoring product may never have, so the typo is refused here, at the
// point where the person who made it is still looking at it.
func checkScripts(g *flowGraph) error {
	if g == nil {
		return nil
	}
	var sandbox *jsSandbox
	for i := range g.Nodes {
		n := &g.Nodes[i]
		body, ok := nodeScript(n)
		if !ok {
			continue
		}
		if sandbox == nil {
			sandbox = newJSSandbox()
		}
		if err := sandbox.compile(n.Id, body); err != nil {
			return err
		}
	}
	return nil
}

func knownNodeType(t string) bool {
	switch t {
	case nodeDeviceTelemetry, nodeFunction, nodeExpression, nodeScale,
		nodeThreshold, nodeSwitch, nodeDeadband, nodeThrottle, nodeDebug, nodeNotify,
		nodeCommand, nodeDerivedMetric, nodeMqttOut:
		return true
	}
	return false
}

// maxFlowNodes bounds a single graph. It is a sanity ceiling, not a product limit — a graph this
// large is almost certainly a mistake, and an unbounded graph is an unbounded blast radius.
const maxFlowNodes = 200

// parseGraph unmarshals and VALIDATES a graph: known node types only (an unknown type is refused,
// never silently ignored — the same closed-default the command layer uses), unique node ids, wires
// that reference real nodes, a node ceiling, and NO CYCLES (P1 executes a DAG; a cycle would loop a
// message forever). Validation runs at save and again at compile, so a bad graph can neither be
// stored nor run.
func parseGraph(raw string) (*flowGraph, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &flowGraph{}, nil // an empty flow is legal (a freshly created, not-yet-drawn flow)
	}
	var g flowGraph
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("graph is not valid JSON: %w", err)
	}
	if len(g.Nodes) > maxFlowNodes {
		return nil, fmt.Errorf("a flow may have at most %d nodes", maxFlowNodes)
	}
	ids := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		if strings.TrimSpace(n.Id) == "" {
			return nil, fmt.Errorf("every node needs an id")
		}
		if ids[n.Id] {
			return nil, fmt.Errorf("duplicate node id %q", n.Id)
		}
		ids[n.Id] = true
		if !knownNodeType(n.Type) {
			return nil, fmt.Errorf("node %q has unknown type %q", n.Id, n.Type)
		}
	}
	for _, w := range g.Wires {
		if !ids[w.From.Node] || !ids[w.To.Node] {
			return nil, fmt.Errorf("wire references a node that does not exist (%s -> %s)", w.From.Node, w.To.Node)
		}
	}
	if cycle := findCycle(&g); cycle != "" {
		return nil, fmt.Errorf("the graph has a cycle through node %q; flows must be acyclic", cycle)
	}
	return &g, nil
}

// findCycle returns a node id on a cycle, or "" if the graph is acyclic (DFS three-colour).
func findCycle(g *flowGraph) string {
	adj := map[string][]string{}
	for _, w := range g.Wires {
		adj[w.From.Node] = append(adj[w.From.Node], w.To.Node)
	}
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var visit func(string) string
	visit = func(n string) string {
		color[n] = grey
		for _, m := range adj[n] {
			switch color[m] {
			case grey:
				return m
			case white:
				if c := visit(m); c != "" {
					return c
				}
			}
		}
		color[n] = black
		return ""
	}
	for _, n := range g.Nodes {
		if color[n.Id] == white {
			if c := visit(n.Id); c != "" {
				return c
			}
		}
	}
	return ""
}

// --- config accessors (a node's config is free-form JSON) -----------------------------------

func cfgString(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	if v, ok := cfg[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func cfgFloat(cfg map[string]any, key string) (float64, bool) {
	if cfg == nil {
		return 0, false
	}
	if f, ok := coerceFloat(cfg[key]); ok {
		return f, true
	}
	// A config value may arrive as a string (a <select> stores its option value as text) — parse it.
	// Payload coercion (coerceFloat) stays strict; only config is this lenient.
	if s, ok := cfg[key].(string); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func cfgBool(cfg map[string]any, key string) bool {
	if cfg == nil {
		return false
	}
	b, _ := cfg[key].(bool)
	return b
}

// --- FlowService (CRUD) ---------------------------------------------------------------------

// FlowService owns the flow rows: list, detail, create/update (validating the graph), delete.
// Enabling, editing or deleting a flow signals the runtime to recompile, so a change takes effect
// with no process restart — onChange is that signal, wired to FlowRuntime.signalReload in app.go.
type FlowService struct {
	flows    dbsql.IGenericRepo[entities.IotFlow]
	onChange func()
	// topics reserves this hub's device command topics. It is consulted when a flow is SAVED, so
	// an author is told plainly that an mqtt_out node cannot address a device — rather than
	// drawing a graph that looks right and is refused, silently, on every message. The runtime
	// refuses independently; this is the explanation, not the enforcement.
	topics topicGuard
	logf   func(format string, args ...any)
}

func NewFlowService(db dbsql.IDbCrud, logf func(string, ...any)) *FlowService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &FlowService{
		flows: dbsql.NewGenericRepo[entities.IotFlow](db),
		logf:  logf,
	}
}

// SetOnChange registers the runtime's reload signal. Called once at wiring time.
func (s *FlowService) SetOnChange(fn func()) { s.onChange = fn }

// SetTopicGuard registers the reserved-topic check used at save time. Called once at wiring time.
func (s *FlowService) SetTopicGuard(g topicGuard) { s.topics = g }

// checkTopics refuses a graph whose mqtt_out node addresses a device's command topic.
//
// This is the same rule the runtime enforces, moved to the point where a human can act on it: the
// bridge node publishes DATA, and a device is commanded through the guarded path or not at all.
func (s *FlowService) checkTopics(ctx context.Context, raw string) error {
	if s.topics == nil {
		return nil
	}
	g, err := parseGraph(raw)
	if err != nil || g == nil {
		return err
	}
	for _, n := range g.Nodes {
		if n.Type != nodeMqttOut {
			continue
		}
		topic := strings.TrimSpace(cfgString(n.Config, "topic"))
		if topic == "" {
			continue
		}
		if devKey, command, reserved := s.topics.ReservedTopic(ctx, topic); reserved {
			who := devKey
			if who == "" {
				who = "a device of that type"
			}
			return fmt.Errorf("the MQTT node %q publishes to %q, which is the %q command topic of %s — "+
				"use a command output to actuate a device, so it goes through the guarded path",
				n.Id, topic, command, who)
		}
	}
	return nil
}

func (s *FlowService) changed() {
	if s.onChange != nil {
		s.onChange()
	}
}

// SaveFlowRequest is the create/update body. Slug is optional on create (derived from the name).
type SaveFlowRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Enabled     bool   `json:"enabled"`
	Graph       string `json:"graph"`
}

func (s *FlowService) List(ctx context.Context) ([]*entities.IotFlow, error) {
	rows, _, err := s.flows.Get(ctx, "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil && isNoResultErr(err) {
		return nil, nil
	}
	return rows, err
}

// ListEnabled returns the flows the runtime should compile.
func (s *FlowService) ListEnabled(ctx context.Context) ([]*entities.IotFlow, error) {
	rows, _, err := s.flows.Get(ctx, "", 500, 0,
		[]sqldataenums.Filter{{FieldName: "Enabled", Compare: sqldataenums.Equal, Value: true}}, nil)
	if err != nil && isNoResultErr(err) {
		return nil, nil
	}
	return rows, err
}

func (s *FlowService) Detail(ctx context.Context, id int64) (*entities.IotFlow, error) {
	flow, err := s.flows.GetById(ctx, "", uint64(id))
	if err != nil || flow == nil {
		return nil, fmt.Errorf("flow not found")
	}
	return flow, nil
}

func (s *FlowService) getBySlug(ctx context.Context, slug string) (*entities.IotFlow, error) {
	row, err := s.flows.GetByUnique(ctx, "", "slug", slug)
	if err != nil {
		if isNoResultErr(err) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

func (s *FlowService) Create(ctx context.Context, req SaveFlowRequest, actor int64) (*entities.IotFlow, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("a flow needs a name")
	}
	g, err := parseGraph(req.Graph)
	if err != nil {
		return nil, err
	}
	if err := checkScripts(g); err != nil {
		return nil, err
	}
	if err := s.checkTopics(ctx, req.Graph); err != nil {
		return nil, err
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = slugify(name)
	}
	slug, err = s.uniqueSlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	id, err := s.flows.Create(ctx, "", entities.IotFlow{
		Slug: slug, Name: name, Description: strings.TrimSpace(req.Description),
		Category: strings.TrimSpace(req.Category), Enabled: req.Enabled, Graph: req.Graph,
		CreatedBy: actor, CreatedAt: now, UpdatedBy: actor, UpdatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	s.changed()
	return s.Detail(ctx, int64(id))
}

func (s *FlowService) Update(ctx context.Context, id int64, req SaveFlowRequest, actor int64) (*entities.IotFlow, error) {
	existing, err := s.flows.GetById(ctx, "", uint64(id))
	if err != nil || existing == nil {
		return nil, fmt.Errorf("flow not found")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("a flow needs a name")
	}
	g, err := parseGraph(req.Graph)
	if err != nil {
		return nil, err
	}
	if err := checkScripts(g); err != nil {
		return nil, err
	}
	if err := s.checkTopics(ctx, req.Graph); err != nil {
		return nil, err
	}
	existing.Name = name
	existing.Description = strings.TrimSpace(req.Description)
	existing.Category = strings.TrimSpace(req.Category)
	existing.Enabled = req.Enabled
	existing.Graph = req.Graph
	existing.UpdatedBy = actor
	existing.UpdatedAt = time.Now().Unix()
	if _, err := s.flows.UpdateById(ctx, "", *existing); err != nil {
		return nil, err
	}
	s.changed()
	return existing, nil
}

func (s *FlowService) Delete(ctx context.Context, id int64) error {
	existing, err := s.flows.GetById(ctx, "", uint64(id))
	if err != nil || existing == nil {
		return fmt.Errorf("flow not found")
	}
	if existing.Builtin {
		return fmt.Errorf("a builtin flow cannot be deleted (copy it instead)")
	}
	if _, err := s.flows.DeleteById(ctx, "", uint64(id)); err != nil {
		return err
	}
	s.changed()
	return nil
}

// uniqueSlug returns slug, or slug-2, slug-3, … until one is free.
func (s *FlowService) uniqueSlug(ctx context.Context, slug string) (string, error) {
	base := slug
	for i := 1; i < 1000; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		existing, err := s.getBySlug(ctx, candidate)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not derive a unique slug from %q", base)
}

// --- templates: reusable flows with device-role SLOTS ---------------------------------------
//
// A flow becomes a reusable template simply by naming a device by a SLOT — a placeholder like
// "$inverter" in a node's deviceKey — instead of a concrete key. Instantiating the template binds
// each slot to a real device and stamps out a normal, concrete flow. No separate entity, no schema
// change: a template is a flow whose graph still has slots, and an instance is one whose slots are
// filled. This is the "declare once, deploy many" pattern the solar workspace calls for, at the
// flow level.

var slotRef = regexp.MustCompile(`^\$([A-Za-z0-9_.-]+)$`)

// slotName returns the slot a deviceKey refers to ("$inverter" -> "inverter"), or ("", false) for a
// concrete key.
func slotName(deviceKey string) (string, bool) {
	m := slotRef.FindStringSubmatch(strings.TrimSpace(deviceKey))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// flowSlots returns the unique device-role slots a graph declares, in first-seen order.
func flowSlots(g *flowGraph) []string {
	seen := map[string]bool{}
	out := []string{} // never nil, so the /slots API returns [] not null for a concrete flow
	for _, n := range g.Nodes {
		if s, ok := slotName(cfgString(n.Config, "deviceKey")); ok && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Slots reports the slots a flow declares (empty = it is a concrete flow, not a template).
func (s *FlowService) Slots(ctx context.Context, id int64) ([]string, error) {
	flow, err := s.Detail(ctx, id)
	if err != nil {
		return nil, err
	}
	g, err := parseGraph(flow.Graph)
	if err != nil {
		return nil, err
	}
	return flowSlots(g), nil
}

// Instantiate stamps a concrete flow out of a template by binding every slot to a real device key.
// The instance is created DISABLED (an admin reviews, then enables) and is never a template itself
// (all slots are resolved). An unbound slot is refused, listing what is missing.
func (s *FlowService) Instantiate(ctx context.Context, id int64, name string, bindings map[string]string, actor int64) (*entities.IotFlow, error) {
	tpl, err := s.Detail(ctx, id)
	if err != nil {
		return nil, err
	}
	g, err := parseGraph(tpl.Graph)
	if err != nil {
		return nil, err
	}
	slots := flowSlots(g)
	if len(slots) == 0 {
		return nil, fmt.Errorf("this flow has no slots — copy it instead of instantiating it")
	}
	var missing []string
	for _, sl := range slots {
		if strings.TrimSpace(bindings[sl]) == "" {
			missing = append(missing, sl)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("these slots are not bound to a device: %s", strings.Join(missing, ", "))
	}
	for i := range g.Nodes {
		if sl, ok := slotName(cfgString(g.Nodes[i].Config, "deviceKey")); ok {
			if g.Nodes[i].Config == nil {
				g.Nodes[i].Config = map[string]any{}
			}
			g.Nodes[i].Config["deviceKey"] = strings.TrimSpace(bindings[sl])
		}
	}
	raw, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	nm := strings.TrimSpace(name)
	if nm == "" {
		nm = tpl.Name + " (instance)"
	}
	return s.Create(ctx, SaveFlowRequest{
		Name: nm, Description: tpl.Description, Category: tpl.Category, Enabled: false, Graph: string(raw),
	}, actor)
}

var slugNonWord = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugNonWord.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "flow"
	}
	return s
}
