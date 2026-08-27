package services

import (
	"context"
	"strings"
	"testing"
)

// The reserved-topic guard is what keeps every gate in CommandService.Issue from being decoration.
//
// The flow canvas's mqtt_out node publishes through the SERVER's own broker handle, which answers
// to no ACL. Aimed at a device's command topic it moved a relay whose actuation was switched off,
// with a value outside the declared bounds, past the duty-cycle limit, and wrote nothing down —
// found by driving a real device on a real broker (tools/fleetbench/bench_iotsan_actuation.py).
//
// These pin the matching. The live bench pins the wiring; a unit test cannot, which is the whole
// reason the bench exists.

func shapesFor(templates ...string) []commandTopicShape {
	out := make([]commandTopicShape, 0, len(templates))
	for _, tpl := range templates {
		if i := strings.Index(tpl, "{deviceKey}"); i >= 0 {
			out = append(out, commandTopicShape{
				prefix:      tpl[:i],
				suffix:      tpl[i+len("{deviceKey}"):],
				placeholder: true,
				command:     "relay",
			})
			continue
		}
		out = append(out, commandTopicShape{prefix: tpl, command: "relay"})
	}
	return out
}

func known(keys ...string) func(string) bool {
	set := map[string]bool{}
	for _, k := range keys {
		set[k] = true
	}
	return func(k string) bool { return set[k] }
}

func TestReservedTopic_MatchesTheTopicARealDeviceActsOn(t *testing.T) {
	shapes := shapesFor("iot/cmd/{deviceKey}/relay")
	exists := known("relay-01")

	key, cmd, reserved := matchReservedTopic(shapes, "iot/cmd/relay-01/relay", exists)
	if !reserved {
		t.Fatal("a real device's own command topic must be reserved to the guarded path")
	}
	if key != "relay-01" || cmd != "relay" {
		t.Fatalf("the refusal must name the device and command; got %q/%q", key, cmd)
	}
}

func TestReservedTopic_LeavesOrdinaryBridgeTopicsAlone(t *testing.T) {
	// The node exists to bridge a value OUT. A guard that blocked that would have replaced one
	// bug with another, so the non-command cases are pinned as carefully as the command one.
	shapes := shapesFor("iot/cmd/{deviceKey}/relay")
	exists := known("relay-01")

	for _, topic := range []string{
		"bridge/relay-01/state",      // the device's own key, but not its command topic
		"iot/cmd/relay-01/telemetry", // same prefix, different suffix
		"iot/cmd/relay-99/relay",     // the right shape, but no such device
		"iot/cmd//relay",             // an empty key matches nothing
		"home/assistant/light/relay", // unrelated
		"",                           // nothing at all
	} {
		if _, _, reserved := matchReservedTopic(shapes, topic, exists); reserved {
			t.Errorf("%q commands nothing and must stay publishable", topic)
		}
	}
}

func TestReservedTopic_FixedTemplateReservesTheTopicItself(t *testing.T) {
	// A template with no {deviceKey} addresses every device of that type on one topic. There is no
	// key to resolve, so the topic itself is reserved.
	shapes := shapesFor("plant/all/shutdown")
	if _, _, reserved := matchReservedTopic(shapes, "plant/all/shutdown", known()); !reserved {
		t.Fatal("a fixed command topic must be reserved even though it names no device")
	}
	if _, _, reserved := matchReservedTopic(shapes, "plant/all/shutdown/ack", known()); reserved {
		t.Fatal("a fixed command topic must match exactly, not by prefix")
	}
}

func TestReservedTopic_PlaceholderAtTheStart(t *testing.T) {
	// "{deviceKey}/cmd" leaves an EMPTY prefix; an off-by-one here would either miss the match or
	// slice out of range, and this is a safety check, so both directions are pinned.
	shapes := shapesFor("{deviceKey}/cmd")
	if key, _, reserved := matchReservedTopic(shapes, "relay-01/cmd", known("relay-01")); !reserved || key != "relay-01" {
		t.Fatalf("a template starting with the placeholder must still match; got %q/%v", key, reserved)
	}
	if _, _, reserved := matchReservedTopic(shapes, "/cmd", known("relay-01")); reserved {
		t.Fatal("an empty key must not match")
	}
}

// fakeGuard stands in for the CommandService in the runtime test below.
type fakeGuard struct {
	reserved string
	refusals []string
}

func (f *fakeGuard) ReservedTopic(_ context.Context, topic string) (string, string, bool) {
	if topic == f.reserved {
		return "relay-01", "relay", true
	}
	return "", "", false
}

func (f *fakeGuard) RecordOffPathRefusal(_ context.Context, _, _, topic, actor string) {
	f.refusals = append(f.refusals, actor+" -> "+topic)
}

// TestMqttOutNode_RefusesACommandTopicAndRecordsIt pins the NODE's behaviour: a reserved topic is
// refused and written down, an ordinary one still publishes. A refusal nobody can see afterwards
// would be half a fix — "something tried to switch this relay" is exactly what must survive.
func TestMqttOutNode_RefusesACommandTopicAndRecordsIt(t *testing.T) {
	guard := &fakeGuard{reserved: "iot/cmd/relay-01/relay"}
	var published []string
	deps := testDeps(nil, nil, nil)
	deps.topics = guard
	deps.publish = func(topic string, _ []byte, _ bool, _ byte) error {
		published = append(published, topic)
		return nil
	}

	graph := flowGraph{
		Nodes: []flowNode{
			{Id: "in", Type: nodeDeviceTelemetry, Config: map[string]any{"deviceKey": "relay-01", "key": "state"}},
			{Id: "out", Type: nodeMqttOut, Config: map[string]any{"topic": "iot/cmd/relay-01/relay"}},
		},
		Wires: []flowWire{{From: flowPort{Node: "in"}, To: flowPort{Node: "out"}}},
	}
	cf := compileForTest(t, graph, deps)
	cf.onInput(context.Background(), "in", &flowMessage{Payload: 1.0, Key: "state", DeviceKey: "relay-01", Ts: 1})

	if len(published) != 0 {
		t.Fatalf("an mqtt_out node must not reach a device's command topic; published %v", published)
	}
	if len(guard.refusals) != 1 {
		t.Fatalf("the refused off-path publish must be recorded; got %v", guard.refusals)
	}
	if !strings.HasPrefix(guard.refusals[0], "flow:") {
		t.Fatalf("the record must name the flow as the actor; got %q", guard.refusals[0])
	}

	// And the node still does its actual job.
	graph.Nodes[1].Config = map[string]any{"topic": "bridge/relay-01/state"}
	cf = compileForTest(t, graph, deps)
	cf.onInput(context.Background(), "in", &flowMessage{Payload: 1.0, Key: "state", DeviceKey: "relay-01", Ts: 1})
	if len(published) != 1 || published[0] != "bridge/relay-01/state" {
		t.Fatalf("an ordinary bridge topic must still publish; got %v", published)
	}
}
