package services

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
)

// These tests exercise the engine in isolation — compile a graph, drive a message through it, and
// inspect the debug snapshot and the fake outputs. No database, no broker: the CRUD/transfer paths
// and the builtin catalog are covered by the live boot against the simulator. The high-value,
// high-risk behaviour is here — propagation, the JS sandbox, the watchdog, and the guarantee that
// actuation only ever leaves through the guarded issuer.

// --- fakes ----------------------------------------------------------------------------------

type fakeNotifier struct{ published []notification.Notification }

func (f *fakeNotifier) Publish(_ context.Context, n notification.Notification) notification.Notification {
	f.published = append(f.published, n)
	return n
}

type fakeDevices struct {
	byKey map[string]*entities.IotDevice
}

func (f *fakeDevices) GetByKey(_ context.Context, key string) (*entities.IotDevice, error) {
	if d, ok := f.byKey[key]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("device %q not found", key)
}

func testDeps(iss commandIssuer, notif flowNotifier, dev deviceResolver) *flowDeps {
	return &flowDeps{issuer: iss, flowNotifier: notif, devices: dev, logf: func(string, ...any) {}}
}

func graphJSON(t *testing.T, g flowGraph) string {
	t.Helper()
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal graph: %v", err)
	}
	return string(b)
}

func compileForTest(t *testing.T, g flowGraph, deps *flowDeps) *compiledFlow {
	t.Helper()
	cf, err := compileFlow(1, "test", graphJSON(t, g), deps)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return cf
}

func seed(cf *compiledFlow, nodeId, key string, value float64) {
	cf.onInput(context.Background(), nodeId, &flowMessage{Payload: value, Key: key, DeviceKey: "d1", Ts: 1})
}

// --- propagation + declarative nodes --------------------------------------------------------

func TestFlow_ScaleThenThresholdPasses(t *testing.T) {
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "d1", "key": "x"}},
			{Id: "sc", Type: nodeScale, Config: map[string]any{"factor": 10.0, "offset": 5.0}},
			{Id: "th", Type: nodeThreshold, Config: map[string]any{"op": ">", "value": 50.0}},
			{Id: "dbg", Type: nodeDebug},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in"}, To: flowPort{Node: "sc"}},
			{From: flowPort{Node: "sc"}, To: flowPort{Node: "th"}},
			{From: flowPort{Node: "th"}, To: flowPort{Node: "dbg"}},
		},
	}
	cf := compileForTest(t, g, testDeps(nil, nil, nil))

	seed(cf, "in", "x", 5) // 5*10+5 = 55 > 50 -> passes
	snap := cf.debug.snapshot()
	if e, ok := snap["dbg"]; !ok || e.Payload.(float64) != 55 {
		t.Fatalf("expected debug to see 55, got %+v (present=%v)", snap["dbg"], ok)
	}
}

func TestFlow_ThresholdBlocksBelow(t *testing.T) {
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "d1", "key": "x"}},
			{Id: "th", Type: nodeThreshold, Config: map[string]any{"op": ">", "value": 100.0}},
			{Id: "dbg", Type: nodeDebug},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in"}, To: flowPort{Node: "th"}},
			{From: flowPort{Node: "th"}, To: flowPort{Node: "dbg"}},
		},
	}
	cf := compileForTest(t, g, testDeps(nil, nil, nil))
	seed(cf, "in", "x", 50) // below threshold -> dropped, debug never reached
	if _, ok := cf.debug.snapshot()["dbg"]; ok {
		t.Fatal("debug should not have been reached below the threshold")
	}
}

func TestFlow_DeadbandSuppressesSmallMoves(t *testing.T) {
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "d1", "key": "x"}},
			{Id: "db", Type: nodeDeadband, Config: map[string]any{"delta": 5.0}},
			{Id: "dbg", Type: nodeDebug},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in"}, To: flowPort{Node: "db"}},
			{From: flowPort{Node: "db"}, To: flowPort{Node: "dbg"}},
		},
	}
	cf := compileForTest(t, g, testDeps(nil, nil, nil))
	seed(cf, "in", "x", 10)
	if cf.debug.snapshot()["dbg"].Payload.(float64) != 10 {
		t.Fatal("first value should pass")
	}
	seed(cf, "in", "x", 12) // move of 2 < 5 -> suppressed, debug stays at 10
	if cf.debug.snapshot()["dbg"].Payload.(float64) != 10 {
		t.Fatal("small move should have been suppressed")
	}
	seed(cf, "in", "x", 20) // move of 10 >= 5 -> passes
	if cf.debug.snapshot()["dbg"].Payload.(float64) != 20 {
		t.Fatal("large move should pass")
	}
}

// --- the JavaScript nodes -------------------------------------------------------------------

func TestFlow_ExpressionComputes(t *testing.T) {
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "d1", "key": "x"}},
			{Id: "ex", Type: nodeExpression, Config: map[string]any{"expr": "msg.payload * 2 + 1"}},
			{Id: "dbg", Type: nodeDebug},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in"}, To: flowPort{Node: "ex"}},
			{From: flowPort{Node: "ex"}, To: flowPort{Node: "dbg"}},
		},
	}
	cf := compileForTest(t, g, testDeps(nil, nil, nil))
	seed(cf, "in", "x", 20)
	if got := cf.debug.snapshot()["dbg"].Payload.(float64); got != 41 {
		t.Fatalf("expected 41, got %v", got)
	}
}

// A function node with the per-flow context is how two telemetry streams are combined — the one
// thing the data-flow model exists to do. Two inputs land as separate messages; the node stashes
// each and recomputes.
func TestFlow_FunctionCombinesTwoStreams(t *testing.T) {
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in_a", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "d1", "key": "a"}},
			{Id: "in_b", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "d1", "key": "b"}},
			{Id: "fn", Type: nodeFunction, Config: map[string]any{"code": "" +
				"if (msg.key === 'a') flow.set('a', msg.payload);\n" +
				"if (msg.key === 'b') flow.set('b', msg.payload);\n" +
				"msg.payload = (flow.get('a')||0) + (flow.get('b')||0);\n" +
				"return msg;"}},
			{Id: "dbg", Type: nodeDebug},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in_a"}, To: flowPort{Node: "fn"}},
			{From: flowPort{Node: "in_b"}, To: flowPort{Node: "fn"}},
			{From: flowPort{Node: "fn"}, To: flowPort{Node: "dbg"}},
		},
	}
	cf := compileForTest(t, g, testDeps(nil, nil, nil))
	seed(cf, "in_a", "a", 3)
	if cf.debug.snapshot()["dbg"].Payload.(float64) != 3 {
		t.Fatal("after a=3 the sum should be 3 (b unset)")
	}
	seed(cf, "in_b", "b", 4)
	if cf.debug.snapshot()["dbg"].Payload.(float64) != 7 {
		t.Fatal("after b=4 the sum should be 7 — flow context did not persist")
	}
}

// A script that returns null drops the message (Node-RED semantics).
func TestFlow_FunctionReturningNullDrops(t *testing.T) {
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "d1", "key": "x"}},
			{Id: "fn", Type: nodeFunction, Config: map[string]any{"code": "if (msg.payload < 100) return null; return msg;"}},
			{Id: "dbg", Type: nodeDebug},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in"}, To: flowPort{Node: "fn"}},
			{From: flowPort{Node: "fn"}, To: flowPort{Node: "dbg"}},
		},
	}
	cf := compileForTest(t, g, testDeps(nil, nil, nil))
	seed(cf, "in", "x", 50)
	if _, ok := cf.debug.snapshot()["dbg"]; ok {
		t.Fatal("a null return should have dropped the message")
	}
}

// --- the sandbox: it cannot escape, and it cannot hang the worker ---------------------------

func TestFlow_SandboxCannotReachHost(t *testing.T) {
	for _, escape := range []string{"return require('fs');", "return process.pid;", "return readFileSync('/etc/passwd');"} {
		g := flowGraph{
			Nodes: []flowNode{
				{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "d1", "key": "x"}},
				{Id: "fn", Type: nodeFunction, Config: map[string]any{"code": escape}},
				{Id: "dbg", Type: nodeDebug},
			},
			Wires: []flowWire{
				{From: flowPort{Node: "in"}, To: flowPort{Node: "fn"}},
				{From: flowPort{Node: "fn"}, To: flowPort{Node: "dbg"}},
			},
		}
		cf := compileForTest(t, g, testDeps(nil, nil, nil))
		seed(cf, "in", "x", 1)
		if _, ok := cf.debug.snapshot()["dbg"]; ok {
			t.Fatalf("escape %q should have errored and dropped the message, not reached downstream", escape)
		}
	}
}

func TestFlow_WatchdogKillsInfiniteLoop(t *testing.T) {
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "d1", "key": "x"}},
			{Id: "fn", Type: nodeFunction, Config: map[string]any{"code": "while(true){} return msg;"}},
			{Id: "dbg", Type: nodeDebug},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in"}, To: flowPort{Node: "fn"}},
			{From: flowPort{Node: "fn"}, To: flowPort{Node: "dbg"}},
		},
	}
	cf := compileForTest(t, g, testDeps(nil, nil, nil))
	done := make(chan struct{})
	go func() { seed(cf, "in", "x", 1); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the watchdog did not interrupt a runaway script")
	}
	if _, ok := cf.debug.snapshot()["dbg"]; ok {
		t.Fatal("a timed-out script must not reach downstream")
	}
}

// --- actuation is contained ------------------------------------------------------------------

// A command output actuates ONLY through the issuer (CommandService in production). This is the
// safety invariant: even arbitrary JS upstream can do nothing but shape the value the guarded issuer
// then receives.
func TestFlow_CommandRoutesThroughGuardedIssuer(t *testing.T) {
	iss := &fakeIssuer{}
	dev := &fakeDevices{byKey: map[string]*entities.IotDevice{"relay-1": {Id: 42, DeviceKey: "relay-1"}}}
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "sensor", "key": "temp"}},
			{Id: "th", Type: nodeThreshold, Config: map[string]any{"op": ">", "value": 30.0}},
			{Id: "cmd", Type: nodeCommand, Config: map[string]any{"deviceKey": "relay-1", "command": "power", "value": 1.0}},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in"}, To: flowPort{Node: "th"}},
			{From: flowPort{Node: "th"}, To: flowPort{Node: "cmd"}},
		},
	}
	cf := compileForTest(t, g, testDeps(iss, nil, dev))

	seed(cf, "in", "temp", 25) // below threshold -> no command
	if len(iss.calls) != 0 {
		t.Fatal("no command should be issued below the threshold")
	}
	seed(cf, "in", "temp", 35) // above -> exactly one command to device 42
	if len(iss.calls) != 1 || iss.devices[0] != 42 || iss.calls[0].Name != "power" || iss.calls[0].Value != 1 {
		t.Fatalf("expected one guarded power=1 command to device 42, got calls=%+v devices=%+v", iss.calls, iss.devices)
	}
}

func TestFlow_NotifyPublishes(t *testing.T) {
	nf := &fakeNotifier{}
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "d1", "key": "x"}},
			{Id: "th", Type: nodeThreshold, Config: map[string]any{"op": ">=", "value": 90.0}},
			{Id: "nt", Type: nodeNotify, Config: map[string]any{"title": "hot", "severity": "critical"}},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in"}, To: flowPort{Node: "th"}},
			{From: flowPort{Node: "th"}, To: flowPort{Node: "nt"}},
		},
	}
	cf := compileForTest(t, g, testDeps(nil, nf, nil))
	seed(cf, "in", "x", 95)
	if len(nf.published) != 1 || nf.published[0].Title != "hot" || nf.published[0].Severity != notification.Critical {
		t.Fatalf("expected one critical 'hot' notification, got %+v", nf.published)
	}
	if nf.published[0].Source != "flow:test" {
		t.Fatalf("notification should be sourced to the flow, got %q", nf.published[0].Source)
	}
}

// --- validation at the door -----------------------------------------------------------------

func TestFlow_ParseGraphRejectsBadGraphs(t *testing.T) {
	cases := map[string]string{
		"unknown type":  `{"nodes":[{"id":"a","type":"wormhole"}],"wires":[]}`,
		"dangling wire": `{"nodes":[{"id":"a","type":"debug"}],"wires":[{"from":{"node":"a"},"to":{"node":"ghost"}}]}`,
		"cycle":         `{"nodes":[{"id":"a","type":"scale"},{"id":"b","type":"scale"}],"wires":[{"from":{"node":"a"},"to":{"node":"b"}},{"from":{"node":"b"},"to":{"node":"a"}}]}`,
		"duplicate id":  `{"nodes":[{"id":"a","type":"debug"},{"id":"a","type":"debug"}],"wires":[]}`,
	}
	for name, raw := range cases {
		if _, err := parseGraph(raw); err == nil {
			t.Errorf("%s: expected parseGraph to reject the graph", name)
		}
	}
	// A well-formed graph parses.
	if _, err := parseGraph(`{"nodes":[{"id":"a","type":"debug"}],"wires":[]}`); err != nil {
		t.Errorf("a valid graph should parse: %v", err)
	}
	// An empty flow is legal.
	if _, err := parseGraph(""); err != nil {
		t.Errorf("an empty graph should be legal: %v", err)
	}
}

// --- derived metric output --------------------------------------------------------------------

type fakeSink struct{ rows []entities.DeviceReading }

func (f *fakeSink) Enqueue(r entities.DeviceReading) { f.rows = append(f.rows, r) }

func TestFlow_DerivedMetricWritesReading(t *testing.T) {
	sink := &fakeSink{}
	dev := &fakeDevices{byKey: map[string]*entities.IotDevice{"site": {Id: 7, DeviceKey: "site"}}}
	deps := testDeps(nil, nil, dev)
	deps.writer = sink
	g := flowGraph{
		Nodes: []flowNode{
			{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "grid", "key": "p"}},
			{Id: "ex", Type: nodeExpression, Config: map[string]any{"expr": "msg.payload * -1"}},
			{Id: "dm", Type: nodeDerivedMetric, Config: map[string]any{"deviceKey": "site", "key": "net_grid"}},
		},
		Wires: []flowWire{
			{From: flowPort{Node: "in"}, To: flowPort{Node: "ex"}},
			{From: flowPort{Node: "ex"}, To: flowPort{Node: "dm"}},
		},
	}
	cf := compileForTest(t, g, deps)
	seed(cf, "in", "p", 600) // negated -> -600 stored under site/net_grid
	if len(sink.rows) != 1 || sink.rows[0].DeviceId != 7 || sink.rows[0].Key != "net_grid" || sink.rows[0].Num != -600 {
		t.Fatalf("expected one net_grid=-600 reading for device 7, got %+v", sink.rows)
	}
}

// --- templates: slots + instantiation ----------------------------------------------------------

func TestFlow_SlotDetectionAndSubstitution(t *testing.T) {
	graph := `{"nodes":[
		{"id":"in","type":"device_telemetry","config":{"deviceKey":"$inverter","key":"grid_ac_power"}},
		{"id":"cmd","type":"command","config":{"deviceKey":"$battery","command":"charge"}},
		{"id":"dbg","type":"debug"}
	],"wires":[]}`
	g, err := parseGraph(graph)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	slots := flowSlots(g)
	if len(slots) != 2 || slots[0] != "inverter" || slots[1] != "battery" {
		t.Fatalf("expected slots [inverter battery] in order, got %v", slots)
	}
	// slotName distinguishes a slot from a concrete key.
	if s, ok := slotName("$inverter"); !ok || s != "inverter" {
		t.Fatalf("slotName($inverter) wrong: %q %v", s, ok)
	}
	if _, ok := slotName("real-device-1"); ok {
		t.Fatal("a concrete key must not read as a slot")
	}
}

func TestFlow_InstantiateBindsAllSlots(t *testing.T) {
	// Instantiate on a service with no repo would try to hit the DB via Create; here we test the
	// pure substitution + validation by driving the graph transform directly, the way Instantiate
	// does, so it stays hermetic.
	graph := `{"nodes":[{"id":"in","type":"device_telemetry","config":{"deviceKey":"$inverter","key":"p"}}],"wires":[]}`
	g, _ := parseGraph(graph)
	slots := flowSlots(g)
	// unbound -> the missing slot is reported
	bindings := map[string]string{}
	var missing []string
	for _, sl := range slots {
		if bindings[sl] == "" {
			missing = append(missing, sl)
		}
	}
	if len(missing) != 1 || missing[0] != "inverter" {
		t.Fatalf("expected inverter reported missing, got %v", missing)
	}
	// bound -> the deviceKey is substituted
	bindings["inverter"] = "sun2000-a"
	for i := range g.Nodes {
		if sl, ok := slotName(cfgString(g.Nodes[i].Config, "deviceKey")); ok {
			g.Nodes[i].Config["deviceKey"] = bindings[sl]
		}
	}
	if got := cfgString(g.Nodes[0].Config, "deviceKey"); got != "sun2000-a" {
		t.Fatalf("slot not substituted: %q", got)
	}
	if len(flowSlots(g)) != 0 {
		t.Fatal("instance should have no remaining slots")
	}
}

func TestFlow_BuiltinSolarIsATemplate(t *testing.T) {
	g, err := parseGraph(solarSystemFlow().Graph)
	if err != nil {
		t.Fatalf("builtin solar graph invalid: %v", err)
	}
	if slots := flowSlots(g); len(slots) != 1 || slots[0] != "inverter" {
		t.Fatalf("builtin solar should declare the $inverter slot, got %v", slots)
	}
}

func TestFlow_ExportImportDocVersionGuard(t *testing.T) {
	// A document from a future format is refused rather than half-understood. (The full DB round
	// trip is covered by the live boot; this pins the version guard, which is pure.)
	doc := FlowExport{Version: flowExportVersion + 1, Slug: "x", Name: "X", Graph: "{}"}
	b, _ := json.Marshal(doc)
	svc := &FlowService{logf: func(string, ...any) {}}
	if _, err := svc.Import(context.Background(), b, 0); err == nil {
		t.Fatal("an unknown format version must be refused")
	}
}
