# Bench: the flow engine runs arbitrary JavaScript inside the app that writes to the building.
#
# THE CLAIM UNDER TEST. myiotsan's flow canvas is a Node-RED-style executable graph, and the user
# who asked for it locked the riskiest fork on purpose: arbitrary JavaScript function nodes, true
# Node-RED parity. services/flow_eval.go states the safety model that makes that acceptable, in
# three lines — a bare goja runtime has no require, no filesystem, no network and no host
# bindings; every call is fenced by a watchdog so a `while(true){}` cannot wedge the worker; and a
# script cannot actuate, because only an OUTPUT node acts and the command output goes through
# CommandService.Issue.
#
# So there are three questions, and this bench asks all three of a running app:
#
#   1. CAN A SCRIPT REACH ANYTHING? Not "is goja safe by default" — is THIS runtime, with THIS
#      bootstrap, reachable from the graph an admin can draw.
#   2. CAN A FLOW REACH A DEVICE ANY OTHER WAY? The actuation bench (#212) closed the mqtt_out
#      escape hatch onto a command topic. The other direction is the one left: a flow publishing
#      onto a device's own TELEMETRY topic would forge readings and, if the hub ingested its own
#      publish, feed itself forever.
#   3. DOES A FLOW THAT MISBEHAVES TAKE ANYTHING DOWN? "Does not crash" is the easy half. The
#      hard half is that every flow in the install shares ONE worker goroutine, so the real
#      question is not whether a runaway script survives — it is what it costs everybody else
#      while it does.
#
# WHY A LIVE RUN. Question 1 is answerable by enumerating what a script can actually see from
# inside the real sandbox, which no unit test of a pure function reaches. Question 3 is a property
# of WALL CLOCK on a shared worker under real telemetry: the only way to measure it is to put a
# reading in one end and time how long an unrelated flow waits.
#
# WHAT MAKES THE NEGATIVES REAL. Every "it could not" here is preceded by the positive that proves
# the mechanism works at all — a script that CAN compute before one that cannot escape, a payload
# that DID reach the wire before a reading that was not forged, a flow that DOES fire on live
# telemetry before one that is starved. A check that passes on an empty result is not a check, and
# this app has already cost the suite two of those.
#
#   python tools/fleetbench/iotsan_harness.py      # stand it up
#   python tools/fleetbench/bench_iotsan_flows.py
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from iotsan_harness import (
    BASE,
    Client,
    DeviceWire,
    admin,
    logs,
    result,
    result_list,
)

DEVICE_KEY = "bench-flow-01"
TELEMETRY_TOPIC = "iot/tel/%s" % DEVICE_KEY
RELAY_TOPIC = "iot/cmd/%s/relay" % DEVICE_KEY

# The product's own constants (services/flow_runtime.go). Restated rather than guessed.
SCRIPT_TIMEOUT_MS = 100
MAX_STEPS = 1000
RECONCILE_SECONDS = 30

# How long an unrelated flow may be kept waiting by a misbehaving one. This is the number the
# whole starvation half of the bench turns on, so it is worth saying why it is what it is: the
# flow worker is shared by every flow in the install, a reading is delivered to it within
# milliseconds, and a fault that delays an alerting flow by more than a few seconds is a fault an
# operator would call an outage. Ten seconds is already generous — the unfixed app spent 15 on ONE
# reading — and picking it generously means a pass here is a real pass, not a tuned one.
STARVATION_BUDGET = 10.0

PASSES = []
FAILS = []


def check(name, ok, detail=""):
    (PASSES if ok else FAILS).append(name)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, ("  — " + detail) if detail else ""))
    return ok


def err_text(r):
    try:
        body = r.json()
    except ValueError:
        return (r.text or "")[:200]
    for k in ("message", "error", "msg"):
        if isinstance(body, dict) and body.get(k):
            return str(body[k])[:200]
    return json.dumps(body)[:200]


# --------------------------------------------------------------------------------------------
# Setup
# --------------------------------------------------------------------------------------------

PROFILE = {
    "slug": "bench-flow",
    "name": "Bench flow engine",
    "vendor": "kopiv2-bench",
    "topicTemplate": TELEMETRY_TOPIC,
    "payloadFormat": "json",
    "keys": [
        # `spin` drives the flow under test; `calm` drives the WITNESS flow that measures what the
        # flow under test costs everybody else. Two keys on ONE device so both travel the same
        # ingest path and the comparison is between flows, not between transports.
        {"key": "spin", "label": "Spin", "dataType": "number", "jsonPath": "spin"},
        {"key": "calm", "label": "Calm", "dataType": "number", "jsonPath": "calm"},
    ],
    "commands": [
        {"name": "relay", "label": "Relay", "kind": "switch",
         "topicTemplate": "iot/cmd/{deviceKey}/relay",
         "payloadTemplate": '{"state":{value}}', "confirmKey": "spin"},
    ],
}


def make_profile(c):
    r = c.post("/api/profiles", PROFILE)
    if r.status_code == 200:
        res = result(r) or {}
        return (res.get("profile") or res).get("id")
    existing = [p for p in result_list(c.get("/api/profiles")) if p.get("slug") == PROFILE["slug"]]
    if not existing:
        raise SystemExit("could not create the bench profile: %s" % err_text(r))
    pid = existing[0].get("id")
    if c.put("/api/profiles/%d" % pid, PROFILE).status_code != 200:
        raise SystemExit("could not update the bench profile")
    return pid


def make_device(c, profile_id):
    r = c.post("/api/devices", {
        "name": "Bench flow device", "deviceKey": DEVICE_KEY, "protocol": "mqtt",
        "profileId": profile_id, "enabled": True, "actuationEnabled": True,
    })
    if r.status_code == 200:
        res = result(r) or {}
        dev = res.get("device") or res
        return dev.get("id"), res.get("password") or dev.get("password")
    existing = [d for d in result_list(c.get("/api/devices?limit=500")) if d.get("deviceKey") == DEVICE_KEY]
    if not existing:
        raise SystemExit("could not create the bench device: %s" % err_text(r))
    dev_id = existing[0].get("id")
    pw = (result(c.post("/api/devices/%d/password" % dev_id)) or {}).get("password")
    return dev_id, pw


def clear_flows(c):
    """Delete every non-builtin flow.

    ISOLATE BEFORE ASSERTING: the flows this bench leaves behind are bound to the same device and
    key as the next check's, so a leftover writes output indistinguishable from the flow under
    test. The six shipped SOLAR flows CANNOT be deleted by design — counting them as leftovers is
    how a cleanup check fails for an unrelated reason."""
    for f in result_list(c.get("/api/flows")):
        if not f.get("builtin"):
            c.delete("/api/flows/%d" % f["id"])


def graph(nodes, wires):
    return json.dumps({"nodes": nodes, "wires": wires})


def line(*specs):
    """Build a straight-line graph from (id, type, config) triples."""
    nodes, wires = [], []
    for i, (nid, ntype, cfg) in enumerate(specs):
        nodes.append({"id": nid, "type": ntype, "x": float(i), "y": 0.0, "config": cfg})
        if i:
            wires.append({"from": {"node": specs[i - 1][0]}, "to": {"node": nid}})
    return graph(nodes, wires)


def save(c, name, g, enabled=False):
    r = c.post("/api/flows", {"name": name, "graph": g, "enabled": enabled})
    return r, (result(r) or {}).get("id")


def test_fire(c, flow_id, seed=1.0):
    r = c.post("/api/flows/%d/run" % flow_id, {"seed": seed})
    nodes = ((result(r) or {}).get("nodes") or {}) if r.status_code == 200 else {}
    return r, nodes


def script_probe(c, code, seed=7.0):
    """Run one script inside the real sandbox and return what the node downstream of it received.

    The debug node is the observation channel: the runtime records the message each node RECEIVED
    on entry, so whatever the script returns is readable at the node it was wired into. That is
    how a question like "what can a script see" gets asked of the running app rather than of the
    source."""
    g = line(("in", "device_telemetry", {"deviceKey": "no-such-device", "key": "x"}),
             ("fn", "function", {"code": code}),
             ("dbg", "debug", {}))
    r, fid = save(c, "probe", g)
    if r.status_code != 200 or not fid:
        return None, err_text(r)
    try:
        _, nodes = test_fire(c, fid, seed)
        return (nodes.get("dbg") or {}).get("payload"), ""
    finally:
        c.delete("/api/flows/%d" % fid)


def readings(c, dev_id, key, limit=2000):
    to = int(time.time()) + 3600
    return result_list(c.get("/api/devices/%d/readings?key=%s&from=0&to=%d&limit=%d"
                             % (dev_id, key, to, limit)))


def notifications(c, limit=200):
    return result_list(c.get("/api/notifications?limit=%d" % limit))


# --------------------------------------------------------------------------------------------
# 1. What can a script reach?
# --------------------------------------------------------------------------------------------

def bench_sandbox_confinement(c):
    print("\n--- the sandbox's whole world is the message -------------------------------------")
    clear_flows(c)

    # POSITIVE CONTROL FIRST. Every "the script could not reach X" below is worthless if scripts
    # are not running at all — a graph that never executes answers "undefined" to everything.
    got, why = script_probe(c, "return msg.payload * 3 + 1;")
    check("a function node runs and its result reaches the next node", got == 22.0,
          "payload=%r %s" % (got, why))

    got, _ = script_probe(c, "return typeof require;")
    check("`require` does not exist inside the sandbox", got == "undefined", "typeof require = %r" % got)

    got, _ = script_probe(c, "return Object.getOwnPropertyNames(this).sort().join(',');")
    check("the global object carries nothing but the flow scratchpad",
          got == "__flowctx,flow", "globals = %r" % got)

    # The classic escape: reach the real global through a function built at runtime. If the
    # runtime had host bindings, this is where they would show up.
    got, _ = script_probe(
        c, "var g = (function(){}).constructor('return this')();"
           " return Object.getOwnPropertyNames(g).sort().join(',');")
    check("the Function-constructor escape lands in the same empty global",
          got == "__flowctx,flow", "globals via constructor = %r" % got)

    for name in ("process", "fs", "net", "os", "child_process", "Java", "globalThis.goja"):
        got, _ = script_probe(c, "try { return typeof %s; } catch (e) { return 'threw'; }" % name)
        check("`%s` is not reachable" % name, got in ("undefined", "threw"),
              "typeof %s = %r" % (name, got))

    # The scratchpad is per-FLOW: it is how two wires combine inside one graph, and it must not be
    # a channel between graphs. Prove both halves — the sharing that is meant to work, and the
    # isolation that is meant to hold.
    g = line(("in", "device_telemetry", {"deviceKey": "no-such-device", "key": "x"}),
             ("a", "function", {"code": "flow.set('token', 'seen-by-a'); return msg;"}),
             ("b", "function", {"code": "return flow.get('token') || 'nothing';"}),
             ("dbg", "debug", {}))
    r, shared_id = save(c, "probe-shared", g)
    _, nodes = test_fire(c, shared_id)
    check("two nodes in ONE flow share the scratchpad",
          (nodes.get("dbg") or {}).get("payload") == "seen-by-a",
          "dbg=%r" % (nodes.get("dbg") or {}).get("payload"))

    got, _ = script_probe(c, "return flow.get('token') || 'nothing';")
    check("and a DIFFERENT flow cannot read it", got == "nothing", "other flow saw %r" % got)
    c.delete("/api/flows/%d" % shared_id)


# --------------------------------------------------------------------------------------------
# 2. A runaway script is stopped, and stops nothing else
# --------------------------------------------------------------------------------------------

def bench_runaway_scripts(c):
    print("\n--- a script that loops, throws or recurses --------------------------------------")
    clear_flows(c)

    t0 = time.time()
    got, _ = script_probe(c, "while (true) {} return 1;")
    spent = time.time() - t0
    check("an infinite loop is interrupted and its branch is dropped", got is None, "dbg=%r" % got)
    check("and it costs one script budget, not the process", spent < 5.0, "%.2fs" % spent)

    t0 = time.time()
    got, _ = script_probe(c, "function f(n) { return f(n + 1); } return f(0);")
    check("unbounded recursion is stopped the same way", got is None, "dbg=%r" % got)
    check("and it does not exhaust the host stack", time.time() - t0 < 5.0, "%.2fs" % (time.time() - t0))

    # Partial failure is first-class: one branch throwing must not silence its sibling. Both
    # branches hang off the same input, so if the throwing one took the event down, the healthy
    # one records nothing.
    g = graph(
        [{"id": "in", "type": "device_telemetry", "x": 0, "y": 0,
          "config": {"deviceKey": "no-such-device", "key": "x"}},
         {"id": "boom", "type": "function", "x": 1, "y": -1,
          "config": {"code": "throw new Error('deliberate');"}},
         {"id": "ok", "type": "function", "x": 1, "y": 1, "config": {"code": "return 42;"}},
         {"id": "dead", "type": "debug", "x": 2, "y": -1, "config": {}},
         {"id": "alive", "type": "debug", "x": 2, "y": 1, "config": {}}],
        [{"from": {"node": "in"}, "to": {"node": "boom"}},
         {"from": {"node": "in"}, "to": {"node": "ok"}},
         {"from": {"node": "boom"}, "to": {"node": "dead"}},
         {"from": {"node": "ok"}, "to": {"node": "alive"}}])
    r, fid = save(c, "probe-throw", g)
    _, nodes = test_fire(c, fid)
    check("a throwing node drops only its own branch",
          "dead" not in nodes and (nodes.get("alive") or {}).get("payload") == 42.0,
          "dead=%r alive=%r" % (nodes.get("dead"), (nodes.get("alive") or {}).get("payload")))
    c.delete("/api/flows/%d" % fid)

    check("the app is still serving after all of that",
          c.get("/api/auth/session").status_code == 200)


# --------------------------------------------------------------------------------------------
# 3. A flow reaches a device through Issue or not at all
# --------------------------------------------------------------------------------------------

def bench_no_other_way_to_a_device(c, dev_id, wire):
    print("\n--- the only way to a device ------------------------------------------------------")
    clear_flows(c)
    wire.drain()

    # Regression guard for the escape hatch the actuation bench closed: an mqtt_out node aimed at
    # a device's COMMAND topic is refused where a human can read the refusal.
    g = line(("in", "device_telemetry", {"deviceKey": DEVICE_KEY, "key": "spin"}),
             ("out", "mqtt_out", {"topic": RELAY_TOPIC, "qos": 1, "retain": False}))
    r, fid = save(c, "probe-cmd-topic", g, enabled=True)
    check("an mqtt_out node aimed at a command topic is still refused at save",
          r.status_code != 200 and "command" in err_text(r).lower(), err_text(r))
    if fid:
        c.delete("/api/flows/%d" % fid)

    # The other direction: publishing onto the device's own TELEMETRY topic. Nothing reserves it —
    # it is not a command topic — so the publish goes out. The question is whether the hub then
    # INGESTS its own publish, which would both forge a reading attributed to a real device and
    # give the flow a way to feed itself forever.
    g = line(("in", "device_telemetry", {"deviceKey": DEVICE_KEY, "key": "spin"}),
             ("fn", "function", {"code": "return {payload: JSON.stringify({spin: 4242})};"}),
             ("out", "mqtt_out", {"topic": TELEMETRY_TOPIC, "qos": 1, "retain": False}))
    r, fid = save(c, "probe-tel-topic", g, enabled=True)
    if not check("a flow may publish onto a device's telemetry topic (it is not a command topic)",
                 r.status_code == 200, err_text(r)):
        return
    time.sleep(2)

    before = len(readings(c, dev_id, "spin"))
    wire.publish(TELEMETRY_TOPIC, json.dumps({"spin": 1}))
    time.sleep(5)
    seen = wire.drain()
    # POSITIVE FIRST: the forged payload really did reach the broker. Without this, "no forged
    # reading was stored" would pass just as happily on a flow that never ran.
    check("the forged payload really reached the wire",
          any("4242" in m[1] for m in seen), "%d messages: %s" % (len(seen), [m[1] for m in seen][:4]))

    rows = readings(c, dev_id, "spin")
    forged = [x for x in rows if abs((x.get("num") or 0) - 4242) < 0.001]
    check("but the hub does not ingest its own publish — no forged reading is stored",
          not forged, "%d forged rows (spin rows %d -> %d)" % (len(forged), before, len(rows)))
    check("and the flow did not feed itself", len(rows) - before <= 2,
          "spin rows %d -> %d after one real reading" % (before, len(rows)))
    c.delete("/api/flows/%d" % fid)


# --------------------------------------------------------------------------------------------
# 4. A flow really fires on live telemetry
# --------------------------------------------------------------------------------------------

def bench_live_telemetry(c, dev_id, wire):
    print("\n--- a flow on real telemetry ------------------------------------------------------")
    clear_flows(c)

    # The one thing the flow engine's own verification never proved: that the worker fires on a
    # REAL broker message rather than only on a test-fire. The broker was off when P1 was checked.
    g = line(("in", "device_telemetry", {"deviceKey": DEVICE_KEY, "key": "calm"}),
             ("fn", "function", {"code": "return msg.payload * 2;"}),
             ("out", "derived_metric", {"deviceKey": DEVICE_KEY, "key": "calm_doubled"}))
    r, fid = save(c, "probe-live", g, enabled=True)
    if not check("an enabled flow saves", r.status_code == 200, err_text(r)):
        return
    time.sleep(2)

    before = len(readings(c, dev_id, "calm_doubled"))
    wire.publish(TELEMETRY_TOPIC, json.dumps({"calm": 21}))
    deadline = time.time() + 20
    rows = []
    while time.time() < deadline:
        rows = readings(c, dev_id, "calm_doubled")
        if len(rows) > before:
            break
        time.sleep(0.5)
    check("a real MQTT reading drives the flow end to end", len(rows) > before,
          "%d -> %d rows" % (before, len(rows)))
    check("and the value it stored is the one the script computed",
          any(abs((x.get("num") or 0) - 42) < 0.001 for x in rows),
          "values=%s" % [x.get("num") for x in rows][-4:])
    c.delete("/api/flows/%d" % fid)


# --------------------------------------------------------------------------------------------
# 5. A flow that cannot run says so
# --------------------------------------------------------------------------------------------

def bench_broken_flow_is_visible(c, dev_id, wire):
    print("\n--- a flow that cannot compile ----------------------------------------------------")
    clear_flows(c)

    # flows.go says of its own validation: "Validation runs at save and again at compile, so a bad
    # graph can neither be stored nor run." A SCRIPT is the half of a graph that is neither.
    g = line(("in", "device_telemetry", {"deviceKey": DEVICE_KEY, "key": "calm"}),
             ("fn", "function", {"code": "return (1 +;"}),
             ("out", "derived_metric", {"deviceKey": DEVICE_KEY, "key": "never_written"}))
    r, fid = save(c, "probe-broken", g, enabled=True)
    check("a flow whose script does not compile is refused at save",
          r.status_code != 200, "status=%d %s" % (r.status_code, err_text(r)))
    check("and the refusal names the problem",
          r.status_code != 200 and "syntax" in err_text(r).lower(), err_text(r))

    if r.status_code != 200:
        return  # refused at the door: there is nothing left to be silent about
    # It was stored. Then the operator must at least be able to SEE that it is not running,
    # because everything else about it says it is.
    row = result(c.get("/api/flows/%d" % fid)) or {}
    check("a stored-but-uncompilable flow does not claim to be enabled and healthy",
          not row.get("enabled") or row.get("runtimeState") in ("error", "broken"),
          "enabled=%r runtimeState=%r" % (row.get("enabled"), row.get("runtimeState")))

    # Wait out one reconcile, then drive it: nothing runs, and nothing said so.
    time.sleep(RECONCILE_SECONDS + 5)
    wire.publish(TELEMETRY_TOPIC, json.dumps({"calm": 7}))
    time.sleep(5)
    told = [n for n in notifications(c) if "probe-broken" in (n.get("body") or "") + (n.get("title") or "")]
    check("the operator is told that a flow could not be compiled", bool(told),
          "%d notifications naming it" % len(told))
    c.delete("/api/flows/%d" % fid)


# --------------------------------------------------------------------------------------------
# 6. One flow cannot starve the rest
# --------------------------------------------------------------------------------------------

WITNESS_KEY = "witness_ran"


def start_witness(c):
    """A trivial flow on its own telemetry key, whose only job is to be quick.

    It is the clock for the whole starvation section: how long IT waits after its reading arrives
    is exactly what a misbehaving neighbour costs an install, measured on the product's own
    storage rather than on anything the bench controls."""
    g = line(("in", "device_telemetry", {"deviceKey": DEVICE_KEY, "key": "calm"}),
             ("out", "derived_metric", {"deviceKey": DEVICE_KEY, "key": WITNESS_KEY}))
    r, fid = save(c, "witness", g, enabled=True)
    if r.status_code != 200:
        raise SystemExit("could not create the witness flow: %s" % err_text(r))
    return fid


def witness_delay(c, dev_id, wire, before, timeout):
    """Publish a witness reading and time how long the witness flow takes to record it."""
    t0 = time.time()
    wire.publish(TELEMETRY_TOPIC, json.dumps({"calm": int(t0) % 100000}))
    while time.time() - t0 < timeout:
        if len(readings(c, dev_id, WITNESS_KEY)) > before:
            return time.time() - t0
        time.sleep(0.25)
    return None


def bench_one_flow_cannot_starve_the_rest(c, dev_id, wire):
    print("\n--- what a misbehaving flow costs everybody else ----------------------------------")
    clear_flows(c)
    witness = start_witness(c)
    time.sleep(2)

    # Baseline: with nothing else running, how long does the witness take? Everything below is
    # measured against this, so a slow docker host cannot be mistaken for starvation.
    base = witness_delay(c, dev_id, wire, len(readings(c, dev_id, WITNESS_KEY)), 30)
    check("the witness flow records a reading promptly when nothing else is running",
          base is not None and base < 10.0, "%s s" % (("%.1f" % base) if base else "never"))

    # (a) A graph of scripts that each stay INSIDE the budget. Nothing here misbehaves by the
    # runtime's own rule — no single script times out — so nothing detects it. The cost is the
    # graph: 190 nodes at ~80ms is fifteen seconds of shared worker for ONE reading, and the
    # ceiling the runtime actually allows is flowMaxSteps x flowScriptTimeout, a hundred seconds.
    burn = "var t = Date.now(); while (Date.now() - t < 80) {} return msg;"
    nodes = [{"id": "in", "type": "device_telemetry", "x": 0, "y": 0,
              "config": {"deviceKey": DEVICE_KEY, "key": "spin"}}]
    wires = []
    prev = "in"
    for i in range(190):
        nid = "n%d" % i
        nodes.append({"id": nid, "type": "function", "x": float(i + 1), "y": 0.0, "config": {"code": burn}})
        wires.append({"from": {"node": prev}, "to": {"node": nid}})
        prev = nid
    r, chain = save(c, "slow-chain", graph(nodes, wires), enabled=True)
    if check("a 190-node graph of legal 80ms scripts saves", r.status_code == 200, err_text(r)):
        time.sleep(2)
        before = len(readings(c, dev_id, WITNESS_KEY))
        wire.publish(TELEMETRY_TOPIC, json.dumps({"spin": 1}))
        time.sleep(0.3)
        delay = witness_delay(c, dev_id, wire, before, 120)
        check("ONE reading into a big slow graph does not stall an unrelated flow",
              delay is not None and delay < STARVATION_BUDGET,
              "the witness waited %s s (budget %.0fs)"
              % (("%.1f" % delay) if delay else ">120", STARVATION_BUDGET))
        c.delete("/api/flows/%d" % chain)
        time.sleep(2)

    # (b) A flow whose script times out on EVERY message. This one the runtime can see — it logs
    # the timeout each time — and the question is whether seeing it changes anything. Each reading
    # costs a full script budget out of a worker shared by every flow, so the delay is simply the
    # burst size times that budget: 200 readings is twenty seconds. (100 was the first size tried
    # and it landed at 9.7s against a 10s budget — a knife edge is not a measurement, so the burst
    # is the size that makes the answer unambiguous either way.)
    g = line(("in", "device_telemetry", {"deviceKey": DEVICE_KEY, "key": "spin"}),
             ("fn", "function", {"code": "while (true) {} return msg;"}),
             ("dbg", "debug", {}))
    r, spinner = save(c, "spinner", g, enabled=True)
    if check("a flow whose script never returns saves and enables", r.status_code == 200, err_text(r)):
        time.sleep(2)
        before = len(readings(c, dev_id, WITNESS_KEY))
        for i in range(200):
            wire.publish(TELEMETRY_TOPIC, json.dumps({"spin": i}), qos=0)
        time.sleep(0.5)
        delay = witness_delay(c, dev_id, wire, before, 120)
        check("a burst into a runaway flow does not stall an unrelated flow",
              delay is not None and delay < STARVATION_BUDGET,
              "the witness waited %s s (budget %.0fs)"
              % (("%.1f" % delay) if delay else ">120", STARVATION_BUDGET))
        told = [n for n in notifications(c)
                if "spinner" in (n.get("body") or "") + (n.get("title") or "")]
        check("the operator is told which flow is misbehaving", bool(told),
              "%d notifications naming it" % len(told))
        c.delete("/api/flows/%d" % spinner)

    c.delete("/api/flows/%d" % witness)


# --------------------------------------------------------------------------------------------
# 7. Events the runtime sheds are visible
# --------------------------------------------------------------------------------------------

def bench_shed_events_are_visible(c):
    print("\n--- what the runtime sheds --------------------------------------------------------")
    # FlowRuntime counts the events it drops when its queue overflows and the comment says the
    # count is "for logging". Nothing reads it. A flow that quietly stops firing under load is
    # exactly the silent failure services/metrics.go says an instrument exists for.
    r = c.get("/api/flows/stats")
    stats = result(r) or {}
    check("the flow runtime reports what it is doing", r.status_code == 200 and isinstance(stats, dict),
          "status=%d %s" % (r.status_code, json.dumps(stats)[:160]))
    check("including the events it shed under backpressure", "dropped" in stats,
          "fields=%s" % sorted(stats.keys()) if isinstance(stats, dict) else stats)


# --------------------------------------------------------------------------------------------
# 8. The hundred-and-first flow
# --------------------------------------------------------------------------------------------

def bench_the_hundred_and_first_flow(c, dev_id, wire):
    print("\n--- an install with more than a hundred flows -------------------------------------")
    clear_flows(c)
    existing = len(result_list(c.get("/api/flows")))

    made = 0
    for i in range(110):
        g = line(("in", "device_telemetry", {"deviceKey": DEVICE_KEY, "key": "calm"}),
                 ("out", "derived_metric", {"deviceKey": DEVICE_KEY, "key": "fan%03d" % i}))
        if c.post("/api/flows", {"name": "fan-%03d" % i, "graph": g, "enabled": True}).status_code == 200:
            made += 1
    check("110 flows are accepted", made == 110, "%d created" % made)

    listed = [f for f in result_list(c.get("/api/flows")) if f.get("name", "").startswith("fan-")]
    check("every flow that was created is listed", len(listed) == made,
          "%d created, %d listed (plus %d pre-existing)" % (made, len(listed), existing))

    time.sleep(RECONCILE_SECONDS / 2 + 3)
    wire.publish(TELEMETRY_TOPIC, json.dumps({"calm": 12345}))
    time.sleep(10)

    # Ask the product which of them actually ran. Each flow writes its OWN derived key, so "did
    # flow N run" is answerable per flow rather than in aggregate.
    latest = result(c.get("/api/devices/%d/latest" % dev_id)) or {}
    ran = sorted(k for k in latest if k.startswith("fan"))
    check("every enabled flow ran, not just the first hundred", len(ran) == made,
          "%d of %d ran (highest %s)" % (len(ran), made, ran[-1] if ran else "none"))
    clear_flows(c)


def bench_a_long_series_is_not_truncated(c, dev_id, wire):
    print("\n--- reading back more than a hundred samples --------------------------------------")
    clear_flows(c)
    n = 300
    for i in range(n):
        wire.publish(TELEMETRY_TOPIC, json.dumps({"calm": 1000 + i}), qos=0)
    # Wait for ingest to settle rather than guessing: the stored counter is the product's own
    # account of what it took in.
    deadline = time.time() + 90
    last, stable = -1, 0
    while time.time() < deadline:
        cur = len(readings(c, dev_id, "calm"))
        if cur == last:
            stable += 1
            if stable >= 3:
                break
        else:
            stable = 0
        last = cur
        time.sleep(2)
    rows = readings(c, dev_id, "calm")
    check("a telemetry series longer than a hundred samples reads back in full", len(rows) > 100,
          "%d rows returned for %d published readings" % (len(rows), n))


# --------------------------------------------------------------------------------------------

def main():
    c = admin()
    print("signed in as admin")
    profile_id = make_profile(c)
    dev_id, password = make_device(c, profile_id)
    print("device %s (id %d) provisioned" % (DEVICE_KEY, dev_id))

    wire = DeviceWire(DEVICE_KEY, password)
    wire.subscribe("iot/tel/%s/#" % DEVICE_KEY)
    wire.subscribe("iot/cmd/%s/#" % DEVICE_KEY)
    print("the device is on the broker")

    try:
        bench_sandbox_confinement(c)
        bench_runaway_scripts(c)
        bench_no_other_way_to_a_device(c, dev_id, wire)
        bench_live_telemetry(c, dev_id, wire)
        bench_broken_flow_is_visible(c, dev_id, wire)
        bench_one_flow_cannot_starve_the_rest(c, dev_id, wire)
        bench_shed_events_are_visible(c)
        bench_the_hundred_and_first_flow(c, dev_id, wire)
        bench_a_long_series_is_not_truncated(c, dev_id, wire)
    finally:
        try:
            clear_flows(c)
            wire.close()
        except Exception:
            pass

    print("\n================================================================================")
    print("%d/%d checks passed" % (len(PASSES), len(PASSES) + len(FAILS)))
    for f in FAILS:
        print("  FAILED:", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
