# Bench: EVERY actuation path in myiotsan goes through the chokepoint, and the chokepoint holds.
#
# THE CLAIM UNDER TEST. myiotsan's safety story is one function — services.CommandService.Issue —
# which every actuation is supposed to pass through, applying six ordered gates: read-only by
# default, admin only, only-what-the-profile-declares, server-side bounds, a per-device rate limit,
# and an audit row for every attempt INCLUDING every refusal. If that claim is true, the blast
# radius of the whole app is one function. If any caller goes around it, the gates are decoration.
#
# WHY UNIT TESTS CANNOT CHECK THIS. Every caller's own test can pass while a caller nobody wrote a
# test for reaches the wire another way. A chokepoint is a claim about the WHOLE PROGRAM, and the
# only way to check it is to enumerate every path that can move a physical device and drive each
# one for real — which is what this does: the direct API, a scene, a schedule (test-fired AND
# fired by the clock), a flow, and the rule engine.
#
# WHAT MAKES THE ASSERTIONS REAL. A refusal reported by the API proves only what the API SAID. So
# a real MQTT client, authenticated as the real provisioned device and confined by the real broker
# ACL, sits on the device's command topic for the whole run. "Refused" means the API said no AND
# nothing arrived on that wire. Every negative assertion is conditioned on a positive that ran
# first on the same wire — a check that passes on an empty result is not a check, and this has
# bitten five times across the suite.
#
#   python tools/fleetbench/iotsan_harness.py      # stand it up
#   python tools/fleetbench/bench_iotsan_actuation.py
import json
import os
import sys
import time
from datetime import datetime, timedelta, timezone

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from iotsan_harness import (
    BENCH_PASS,
    Client,
    DeviceWire,
    admin,
    logs,
    result,
    result_list,
)

DEVICE_KEY = "bench-relay-01"
RELAY_TOPIC = "iot/cmd/%s/relay" % DEVICE_KEY
SETPOINT_TOPIC = "iot/cmd/%s/setpoint" % DEVICE_KEY
TELEMETRY_TOPIC = "iot/tel/%s" % DEVICE_KEY
WIRE_FILTER = "iot/cmd/%s/#" % DEVICE_KEY
BRIDGE_FILTER = "bridge/%s/#" % DEVICE_KEY

# A SECOND device, created only AFTER a flow already addresses its command topic — the upgrade
# case, and the one that proves the runtime rather than the save-time check is the authority.
SECOND_KEY = "bench-relay-02"
SECOND_RELAY_TOPIC = "iot/cmd/%s/relay" % SECOND_KEY

# The per-device duty-cycle floor in services.CommandService (minCommandInterval). A bench that
# issues faster than this gets "too soon" on checks that are not about the rate limit, so every
# check that expects to reach the wire waits it out first.
RATE_LIMIT = 2.0

PASSES = []
FAILS = []
PROFILE_ID = 0


def check(name, ok, detail=""):
    (PASSES if ok else FAILS).append(name)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, ("  — " + detail) if detail else ""))
    return ok


def settle():
    """Wait out the per-device rate limit so the NEXT command is judged by the gate under test."""
    time.sleep(RATE_LIMIT + 0.2)


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
# Setup: a device type that declares two commands, and one real device of that type.
# --------------------------------------------------------------------------------------------

def make_profile(c):
    body = {
        "slug": "bench-relay",
        "name": "Bench relay + thermostat",
        "vendor": "kopiv2-bench",
        "topicTemplate": TELEMETRY_TOPIC,
        "payloadFormat": "json",
        "keys": [
            {"key": "state", "label": "Relay state", "dataType": "number", "jsonPath": "state"},
            {"key": "setpoint", "label": "Setpoint", "unit": "C", "dataType": "number",
             "jsonPath": "sp"},
        ],
        "commands": [
            # A switch: the value must be 0 or 1, and nothing else.
            {"name": "relay", "label": "Relay", "kind": "switch",
             "topicTemplate": "iot/cmd/{deviceKey}/relay",
             "payloadTemplate": '{"state":{value}}', "confirmKey": "state"},
            # A setpoint with REAL bounds. 5..30 is the safety property the bench pushes against:
            # a thermostat that accepts 200 because a slider was bypassed is a fire.
            {"name": "setpoint", "label": "Setpoint", "kind": "setpoint",
             "min": 5, "max": 30,
             "topicTemplate": "iot/cmd/{deviceKey}/setpoint",
             "payloadTemplate": '{"sp":{value}}', "confirmKey": "setpoint"},
        ],
    }
    r = c.post("/api/profiles", body)
    if r.status_code != 200:
        # A re-run against a surviving instance: the slug is taken, so the existing profile is
        # UPDATED to this exact declaration instead. Without this a second run dies on a unique
        # constraint, and a bench that only works on a virgin database is a bench nobody re-runs.
        existing = [p for p in result_list(c.get("/api/profiles"))
                    if p.get("slug") == body["slug"]]
        if not existing:
            raise SystemExit("could not create the bench profile: %s" % err_text(r))
        pid = existing[0].get("id")
        r = c.put("/api/profiles/%d" % pid, body)
        if r.status_code != 200:
            raise SystemExit("could not update the bench profile: %s" % err_text(r))
        return pid
    return (result(r) or {}).get("profile", {}).get("id") or (result(r) or {}).get("id")


def make_device(c, profile_id):
    r = c.post("/api/devices", {
        "name": "Bench relay",
        "deviceKey": DEVICE_KEY,
        "protocol": "mqtt",
        "profileId": profile_id,
        "enabled": True,
        # Actuation ON to start: the FIRST thing the bench proves is that a command really reaches
        # the wire. Every later "it was refused" is measured against that known-working wire.
        "actuationEnabled": True,
    })
    if r.status_code != 200:
        # Re-run: the device key is taken. Reuse it and ROTATE the credential — the provisioning
        # password is returned exactly once and can never be read back, which is correct product
        # behaviour and means a re-run has to mint a new one.
        existing = [d for d in result_list(c.get("/api/devices?limit=200"))
                    if d.get("deviceKey") == DEVICE_KEY]
        if not existing:
            raise SystemExit("could not create the bench device: %s" % err_text(r))
        dev_id = existing[0].get("id")
        rot = c.post("/api/devices/%d/password" % dev_id)
        password = (result(rot) or {}).get("password")
        if not password:
            raise SystemExit("could not rotate the bench device credential: %s" % err_text(rot))
        return dev_id, password
    res = result(r) or {}
    dev = res.get("device") or res
    password = res.get("password") or dev.get("password")
    if not password:
        raise SystemExit("device create did not return the one-time broker password: %s"
                         % json.dumps(res)[:300])
    return dev.get("id"), password


def make_second_device(c):
    """Provision bench-relay-02 with actuation OFF — nothing may reach it by any route."""
    r = c.post("/api/devices", {
        "name": "Bench relay 2", "deviceKey": SECOND_KEY, "protocol": "mqtt",
        "profileId": PROFILE_ID, "enabled": True, "actuationEnabled": False,
    })
    if r.status_code == 200:
        res = result(r) or {}
        dev = res.get("device") or res
        return dev.get("id"), res.get("password") or dev.get("password")
    existing = [d for d in result_list(c.get("/api/devices?limit=200"))
                if d.get("deviceKey") == SECOND_KEY]
    if not existing:
        raise SystemExit("could not create the second bench device: %s" % err_text(r))
    dev_id = existing[0].get("id")
    rot = c.post("/api/devices/%d/password" % dev_id)
    return dev_id, (result(rot) or {}).get("password")


def set_device(c, dev_id, **fields):
    """PUT the device with a full update body.

    UpdateDeviceRequest REPLACES rather than patches, and it has no `protocol` field (an unknown
    field is rejected outright, which is the right call and worth knowing before writing the
    body). profileId must be resent or the device silently loses its type and every command
    afterwards is refused for the wrong reason."""
    body = {
        "name": "Bench relay",
        "profileId": PROFILE_ID,
        "enabled": True,
        "actuationEnabled": True,
    }
    body.update(fields)
    r = c.put("/api/devices/%d" % dev_id, body)
    if r.status_code != 200:
        raise SystemExit("could not update the bench device: %s" % err_text(r))
    return result(r)


def history(c, dev_id, limit=50):
    r = c.get("/api/devices/%d/commands/history?limit=%d" % (dev_id, limit))
    return result_list(r)


def issue(c, dev_id, name, value):
    return c.post("/api/devices/%d/commands" % dev_id, {"name": name, "value": value})


# --------------------------------------------------------------------------------------------
# The gates, driven through the direct API.
# --------------------------------------------------------------------------------------------

def bench_direct(c, dev_id, wire):
    print("\n--- the chokepoint's own gates -------------------------------------------------")

    # POSITIVE FIRST. This is what makes every "nothing arrived" below mean something: the wire is
    # proven to carry a command before it is used as evidence that one did not.
    wire.drain()
    r = issue(c, dev_id, "relay", 1)
    msgs = wire.wait_for(timeout=6)
    ok = r.status_code == 200 and any(t == RELAY_TOPIC and '"state":1' in p.replace(" ", "")
                                      for t, p, _ in msgs)
    check("a command reaches the real device on the real broker", ok,
          "http=%s wire=%s" % (r.status_code, msgs))
    if not ok:
        raise SystemExit("the wire is not carrying commands — every later assertion would be "
                         "vacuous. app logs:\n%s" % logs(40))

    cmd = result(r) or {}
    check("the accepted command is recorded as sent, naming the actor",
          cmd.get("status") == "sent" and cmd.get("requestedByName"),
          "status=%s by=%s" % (cmd.get("status"), cmd.get("requestedByName")))

    rows = history(c, dev_id)
    check("the command is in the device's history", any(x.get("id") == cmd.get("id") for x in rows),
          "%d rows" % len(rows))

    tw = result(c.get("/api/devices/%d/twin" % dev_id)) or {}
    desired = json.dumps(tw)
    check("the twin records the desired state", '"state"' in desired and "1" in desired,
          desired[:160])

    # GATE 1 — read-only by default.
    settle()
    set_device(c, dev_id, actuationEnabled=False)
    before = len(history(c, dev_id))
    wire.drain()
    r = issue(c, dev_id, "relay", 0)
    quiet = wire.quiet_for(2.5)
    check("GATE actuation-enabled: a device with actuation off refuses the command",
          r.status_code != 200 and "actuation" in err_text(r).lower(), err_text(r))
    check("GATE actuation-enabled: and NOTHING reaches the relay", not quiet, str(quiet))
    rows = history(c, dev_id)
    refused = [x for x in rows if x.get("status") == "failed" and "actuation" in (x.get("error") or "")]
    check("a REFUSED command is still an audit row naming the actor",
          len(rows) > before and refused and refused[0].get("requestedByName"),
          "rows %d->%d by=%s" % (before, len(rows), refused[0].get("requestedByName") if refused else "-"))

    # GATE 2 — only what the profile declares.
    set_device(c, dev_id, actuationEnabled=True)
    settle()
    wire.drain()
    r = issue(c, dev_id, "unlock", 1)
    quiet = wire.quiet_for(2.0)
    check("GATE declared-only: an undeclared command is refused",
          r.status_code != 200, err_text(r))
    check("GATE declared-only: and nothing reaches the device", not quiet, str(quiet))

    # GATE 3 — bounds, server-side. The UI would never produce these values; the point is that the
    # server refuses them even when the caller does.
    settle()
    wire.drain()
    r = issue(c, dev_id, "setpoint", 999)
    quiet = wire.quiet_for(2.0)
    check("GATE bounds: a setpoint above the declared max is refused server-side",
          r.status_code != 200 and "range" in err_text(r).lower(), err_text(r))
    check("GATE bounds: and nothing reaches the device", not quiet, str(quiet))

    settle()
    r = issue(c, dev_id, "setpoint", -40)
    check("GATE bounds: a setpoint below the declared min is refused",
          r.status_code != 200, err_text(r))

    settle()
    r = issue(c, dev_id, "relay", 7)
    check("GATE bounds: a switch refuses a value that is not 0 or 1",
          r.status_code != 200, err_text(r))

    # And the bound is a RANGE, not a blanket no: the edge value is accepted and lands.
    settle()
    wire.drain()
    r = issue(c, dev_id, "setpoint", 30)
    msgs = wire.wait_for(timeout=6)
    check("GATE bounds: the max itself is accepted and reaches the device",
          r.status_code == 200 and any(t == SETPOINT_TOPIC for t, _, _ in msgs),
          "http=%s wire=%s" % (r.status_code, msgs))

    # GATE 4 — the per-device duty cycle.
    settle()
    wire.drain()
    r1 = issue(c, dev_id, "relay", 1)
    r2 = issue(c, dev_id, "relay", 0)
    msgs = wire.wait_for(timeout=4)
    check("GATE rate-limit: a second command inside the duty cycle is refused",
          r1.status_code == 200 and r2.status_code != 200 and "too soon" in err_text(r2).lower(),
          "%s / %s" % (r1.status_code, err_text(r2)))
    check("GATE rate-limit: exactly ONE message reached the relay", len(msgs) == 1, str(msgs))

    # A disabled device is not commandable either — checked last of the gates because disabling it
    # is the one change that can also affect the broker session the wire depends on.
    settle()
    set_device(c, dev_id, enabled=False)
    wire.drain()
    r = issue(c, dev_id, "relay", 1)
    quiet = wire.quiet_for(2.0)
    check("GATE enabled: a disabled device refuses the command",
          r.status_code != 200 and "disabled" in err_text(r).lower(), err_text(r))
    check("GATE enabled: and nothing reaches it", not quiet, str(quiet))
    set_device(c, dev_id, enabled=True)


def bench_rbac(c, dev_id):
    print("\n--- who may actuate ------------------------------------------------------------")
    roles = result(c.get("/api/settings/roles"))
    roles = roles if isinstance(roles, list) else (roles or {}).get("items") or []
    by_name = {}
    for role in roles:
        by_name[str(role.get("name", "")).lower()] = role.get("id")
    made = {}
    for want in ("operator", "viewer"):
        rid = by_name.get(want)
        if not rid:
            check("a %s role exists to test with" % want, False, str(list(by_name)))
            continue
        user = "bench-%s" % want
        c.post("/api/settings/users", {
            "username": user, "password": BENCH_PASS, "displayName": user,
            "roleId": rid, "isActive": True, "mustChangePassword": False,
        })
        made[want] = Client(user=user, password=BENCH_PASS)

    for want, cl in made.items():
        r = cl.post("/api/devices/%d/commands" % dev_id, {"name": "relay", "value": 1})
        check("a %s may NOT command a device" % want, r.status_code in (401, 403),
              "http=%s" % r.status_code)
        r = cl.get("/api/devices/%d/commands/history" % dev_id)
        rows = result_list(r)
        # Conditioned on the positive: the history must actually contain the rows the admin wrote,
        # or "a viewer can read the trail" would pass against an empty answer.
        check("a %s CAN see what was commanded (the trail is not admin-only)" % want,
              r.status_code == 200 and len(rows) > 0, "http=%s rows=%d" % (r.status_code, len(rows)))


# --------------------------------------------------------------------------------------------
# Every OTHER path that can move a device.
# --------------------------------------------------------------------------------------------

def bench_scene(c, dev_id, wire):
    print("\n--- scenes ---------------------------------------------------------------------")
    r = c.post("/api/scenes", {
        "name": "Bench scene", "enabled": True,
        "actions": [{"deviceId": dev_id, "commandName": "relay", "value": 1}],
    })
    if r.status_code != 200:
        return check("a scene can be created", False, err_text(r))
    scene = result(r) or {}
    sid = (scene.get("scene") or scene).get("id")

    settle()
    wire.drain()
    r = c.post("/api/scenes/%d/run" % sid)
    msgs = wire.wait_for(timeout=6)
    check("a scene run reaches the device",
          r.status_code == 200 and any(t == RELAY_TOPIC for t, _, _ in msgs),
          "http=%s wire=%s" % (r.status_code, msgs))

    settle()
    set_device(c, dev_id, actuationEnabled=False)
    before = len(history(c, dev_id))
    wire.drain()
    r = c.post("/api/scenes/%d/run" % sid)
    quiet = wire.quiet_for(2.5)
    body = json.dumps(result(r) or {})
    check("a scene CANNOT go around the gate: actuation off means the action is refused",
          "actuation" in body.lower(), body[:200])
    check("a scene CANNOT go around the gate: nothing reaches the device", not quiet, str(quiet))
    check("the scene's refusal is recorded in the device's history",
          len(history(c, dev_id)) > before)
    set_device(c, dev_id, actuationEnabled=True)


def bench_schedule(c, dev_id, wire):
    print("\n--- schedules ------------------------------------------------------------------")
    r = c.post("/api/schedules", {
        "name": "Bench schedule", "enabled": True,
        "triggerType": "clock", "timeOfDay": "03:00", "days": "",
        "targetType": "command", "deviceId": dev_id, "commandName": "relay", "value": 1,
    })
    if r.status_code != 200:
        return check("a schedule can be created", False, err_text(r))
    sid = (result(r) or {}).get("id")

    settle()
    wire.drain()
    r = c.post("/api/schedules/%d/run" % sid)
    msgs = wire.wait_for(timeout=6)
    check("a test-fired schedule reaches the device",
          r.status_code == 200 and any(t == RELAY_TOPIC for t, _, _ in msgs),
          "http=%s wire=%s" % (r.status_code, msgs))

    settle()
    set_device(c, dev_id, actuationEnabled=False)
    before = len(history(c, dev_id))
    wire.drain()
    c.post("/api/schedules/%d/run" % sid)
    quiet = wire.quiet_for(2.5)
    check("a test-fired schedule CANNOT go around the gate", not quiet, str(quiet))
    check("the schedule's refusal is recorded in the device's history",
          len(history(c, dev_id)) > before)
    set_device(c, dev_id, actuationEnabled=True)
    return sid


def bench_schedule_clock(c, dev_id, wire, sid):
    """The schedule fired BY THE CLOCK, not by a person pressing test.

    This is the path with no user behind it, and the one where an audit trail can quietly read
    "System". The scheduler ticks on the minute, so this costs up to two minutes and is worth it:
    an automatic actuation nobody can attribute is exactly the failure that only shows up live."""
    print("\n--- the schedule the clock fires -----------------------------------------------")
    fire_at = (datetime.now(timezone.utc) + timedelta(minutes=1, seconds=20)).strftime("%H:%M")
    r = c.put("/api/schedules/%d" % sid, {
        "name": "Bench schedule", "enabled": True,
        "triggerType": "clock", "timeOfDay": fire_at, "days": "",
        "targetType": "command", "deviceId": dev_id, "commandName": "relay", "value": 1,
    })
    if r.status_code != 200:
        return check("the schedule can be pointed at the next minute", False, err_text(r))
    print("waiting for the scheduler to reach %s UTC..." % fire_at)
    settle()
    wire.drain()
    before = len(history(c, dev_id))
    msgs = wire.wait_for(timeout=150)
    check("a schedule fired by the CLOCK reaches the device",
          any(t == RELAY_TOPIC for t, _, _ in msgs), str(msgs))
    rows = history(c, dev_id)
    fresh = rows[:max(1, len(rows) - before)] if len(rows) > before else []
    named = [x for x in fresh if str(x.get("requestedByName", "")).startswith("schedule:")]
    check("and the trail attributes it to the SCHEDULE, not to 'System'",
          bool(named), str([x.get("requestedByName") for x in fresh])[:200])


def flow_graph(nodes, wires=None):
    return json.dumps({"nodes": nodes, "wires": wires or []})


def bench_flow_command(c, dev_id, wire):
    print("\n--- flows: the command node ----------------------------------------------------")
    graph = flow_graph(
        [
            {"id": "in", "type": "device_telemetry", "x": 0, "y": 0,
             "config": {"deviceKey": DEVICE_KEY, "key": "state"}},
            {"id": "out", "type": "command", "x": 200, "y": 0,
             "config": {"deviceKey": DEVICE_KEY, "command": "relay", "value": 1}},
        ],
        [{"from": {"node": "in"}, "to": {"node": "out"}}],
    )
    r = c.post("/api/flows", {"name": "Bench command flow", "slug": "bench-cmd",
                              "enabled": True, "graph": graph})
    if r.status_code != 200:
        return check("a flow with a command node can be created", False, err_text(r))
    fid = ((result(r) or {}).get("flow") or result(r) or {}).get("id")

    settle()
    wire.drain()
    r = c.post("/api/flows/%d/run" % fid, {"seed": 1})
    msgs = wire.wait_for(timeout=6)
    check("a flow's command node reaches the device",
          r.status_code == 200 and any(t == RELAY_TOPIC for t, _, _ in msgs),
          "http=%s wire=%s" % (r.status_code, msgs))
    rows = history(c, dev_id)
    check("and the trail names the FLOW as the actor",
          any(str(x.get("requestedByName", "")).startswith("flow:") for x in rows),
          str([x.get("requestedByName") for x in rows[:3]]))

    settle()
    set_device(c, dev_id, actuationEnabled=False)
    before = len(history(c, dev_id))
    wire.drain()
    c.post("/api/flows/%d/run" % fid, {"seed": 1})
    quiet = wire.quiet_for(2.5)
    check("a flow's command node CANNOT go around the gate", not quiet, str(quiet))
    check("the flow's refusal is recorded in the device's history",
          len(history(c, dev_id)) > before)
    set_device(c, dev_id, actuationEnabled=True)


def bench_flow_mqtt_out(c, dev_id, wire):
    """THE ENUMERATION CHECK. The flow palette has a second output that touches the broker:
    mqtt_out, which publishes an arbitrary payload to an arbitrary topic — through the SERVER's own
    broker handle, which answers to no ACL.

    The design says there is deliberately no "publish this payload to that topic" endpoint, because
    one would be a remote shell for the building's electrics. So: can mqtt_out be aimed at a
    device's COMMAND topic? If it can, the relay moves with actuation switched off, outside the
    declared bounds, past the duty cycle, with nothing in the trail — every gate bypassed at once.

    Three things are checked, and the third is the one that matters:
      1. aiming it at a real device's command topic is refused when the flow is SAVED;
      2. an ordinary bridge topic still works, because that is what the node is FOR;
      3. a flow saved when no such device existed still cannot reach that device once it does —
         i.e. the RUNTIME is the authority, not the save-time check. That is also the upgrade case:
         a flow drawn before this guard existed is still refused at run time."""
    print("\n--- flows: the mqtt_out node pointed at a command topic ------------------------")

    # 1. Save-time refusal, against a device that exists right now.
    graph = flow_graph(
        [
            {"id": "in", "type": "device_telemetry", "x": 0, "y": 0,
             "config": {"deviceKey": DEVICE_KEY, "key": "state"}},
            {"id": "out", "type": "mqtt_out", "x": 200, "y": 0,
             "config": {"topic": RELAY_TOPIC, "qos": 1}},
        ],
        [{"from": {"node": "in"}, "to": {"node": "out"}}],
    )
    r = c.post("/api/flows", {"name": "Bench raw publish", "slug": "bench-raw",
                              "enabled": True, "graph": graph})
    msg = err_text(r)
    check("a flow whose MQTT node addresses a device's command topic is refused at save",
          r.status_code != 200, "http=%s %s" % (r.status_code, msg))
    check("and the refusal names the topic and points at the command output instead",
          RELAY_TOPIC in msg and "command" in msg.lower(), msg)

    # 2. The node's legitimate purpose still works: bridging a value OUT under any other topic.
    #    (The bench device may subscribe here because the broker ACL allows any topic containing
    #    its own key — the same confinement a real sensor lives under.)
    bridge_topic = "bridge/%s/state" % DEVICE_KEY
    graph = flow_graph(
        [
            {"id": "in", "type": "device_telemetry", "x": 0, "y": 0,
             "config": {"deviceKey": DEVICE_KEY, "key": "state"}},
            {"id": "out", "type": "mqtt_out", "x": 200, "y": 0,
             "config": {"topic": bridge_topic, "qos": 1}},
        ],
        [{"from": {"node": "in"}, "to": {"node": "out"}}],
    )
    r = c.post("/api/flows", {"name": "Bench bridge", "slug": "bench-bridge",
                              "enabled": True, "graph": graph})
    if not check("an ordinary bridge topic is still allowed", r.status_code == 200, err_text(r)):
        return
    fid = ((result(r) or {}).get("flow") or result(r) or {}).get("id")
    wire.drain()
    c.post("/api/flows/%d/run" % fid, {"seed": 42})
    bridged = wire.wait_for(timeout=5)
    check("and the bridge really publishes (the guard did not break the node's purpose)",
          any(t == bridge_topic for t, _, _ in bridged), str(bridged))

    # 3. THE RUNTIME IS THE AUTHORITY. The flow is saved while no device answers to that key, so
    #    the save-time check has nothing to object to — then the device is created.
    graph = flow_graph(
        [
            {"id": "in", "type": "device_telemetry", "x": 0, "y": 0,
             "config": {"deviceKey": DEVICE_KEY, "key": "state"}},
            {"id": "out", "type": "mqtt_out", "x": 200, "y": 0,
             "config": {"topic": SECOND_RELAY_TOPIC, "qos": 1}},
        ],
        [{"from": {"node": "in"}, "to": {"node": "out"}}],
    )
    r = c.post("/api/flows", {"name": "Bench pre-existing raw publish", "slug": "bench-raw-pre",
                              "enabled": True, "graph": graph})
    if not check("a flow may address a topic no device answers to yet", r.status_code == 200,
                 err_text(r)):
        return
    fid = ((result(r) or {}).get("flow") or result(r) or {}).get("id")

    second_id, second_pass = make_second_device(c)
    wire2 = DeviceWire(SECOND_KEY, second_pass)
    wire2.subscribe("iot/cmd/%s/#" % SECOND_KEY)
    try:
        before = len(history(c, second_id))
        wire2.drain()
        c.post("/api/flows/%d/run" % fid, {"seed": 1})
        landed = wire2.wait_for(timeout=5)
        after = history(c, second_id)
        check("a raw publish CANNOT reach a device whose actuation is switched off",
              not landed, "arrived on the relay's own topic: %s" % landed)
        check("the refused off-path publish is written into the device's command history",
              len(after) > before and any("guarded" in (x.get("error") or "") for x in after),
              "history %d->%d: %s" % (before, len(after), [x.get("error") for x in after[:2]]))
    finally:
        wire2.close()


def bench_rules_do_not_actuate(c, dev_id, wire):
    """The rule engine showed no reference to Issue or to scenes in a grep. That is evidence of
    absence in the source, which is not the same as proof — so this drives a rule until it fires
    and checks that a firing rule moves nothing."""
    print("\n--- rules ----------------------------------------------------------------------")
    # ISOLATION. The flows created above are enabled and bound to this device's `state` key, so a
    # real published reading drives them too — and their output would be indistinguishable from a
    # rule actuating. A refusal (or an absence) must be tested in a state where only the thing
    # under test can act, so the flows are deleted first. That also exercises a verb the rest of
    # the bench never used: a green API bench only covers the verbs it used.
    # The shipped BUILTIN flows (the solar templates) are deliberately undeletable and are left
    # alone — counting them as "left over" would have made this check fail for a reason that has
    # nothing to do with the bench's own flows.
    for f in result_list(c.get("/api/flows")):
        if not f.get("builtin"):
            c.delete("/api/flows/%d" % f.get("id"))
    left = [f for f in result_list(c.get("/api/flows")) if not f.get("builtin")]
    check("the bench's flows can be deleted", not left,
          "%d left: %s" % (len(left), [f.get("slug") for f in left]))
    time.sleep(2)  # let the runtime reconcile them away

    r = c.post("/api/rules", {
        "name": "Bench rule", "enabled": True, "deviceId": dev_id, "key": "state",
        "condition": "above", "threshold": 0.5, "consecutiveSamples": 1,
        "cooldownSeconds": 0, "severity": "warning", "schedulePolicy": "always",
    })
    if r.status_code != 200:
        return check("a rule can be created", False, err_text(r))

    settle()
    wire.drain()
    before = len(history(c, dev_id))
    alerts_before = len(result_list(c.get("/api/alerts?limit=50")))
    # The device publishes a reading that breaches the rule — the real ingest path, not an API poke.
    wire.publish(TELEMETRY_TOPIC, json.dumps({"state": 1, "sp": 21}))
    time.sleep(3)
    alerts = result_list(c.get("/api/alerts?limit=50"))
    check("the rule fires on a real published reading", len(alerts) > alerts_before,
          "alerts %d->%d" % (alerts_before, len(alerts)))
    stray = [m for m in wire.drain() if m[0].startswith("iot/cmd/")]
    check("a firing rule actuates NOTHING (rules alert; they do not command)",
          not stray and len(history(c, dev_id)) == before,
          "wire=%s history=%d" % (stray, len(history(c, dev_id))))


def main():
    c = admin()
    print("signed in as admin")
    global PROFILE_ID
    PROFILE_ID = make_profile(c)
    profile_id = PROFILE_ID
    dev_id, password = make_device(c, profile_id)
    print("device %s (id %d) provisioned" % (DEVICE_KEY, dev_id))

    wire = DeviceWire(DEVICE_KEY, password)
    wire.subscribe(WIRE_FILTER)
    wire.subscribe(BRIDGE_FILTER)
    print("the device is on the broker, listening on", WIRE_FILTER)

    try:
        bench_direct(c, dev_id, wire)
        bench_rbac(c, dev_id)
        bench_scene(c, dev_id, wire)
        sid = bench_schedule(c, dev_id, wire)
        bench_flow_command(c, dev_id, wire)
        bench_flow_mqtt_out(c, dev_id, wire)
        bench_rules_do_not_actuate(c, dev_id, wire)
        if sid:
            bench_schedule_clock(c, dev_id, wire, sid)
    finally:
        wire.close()

    print("\n================================================================================")
    print("%d/%d checks passed" % (len(PASSES), len(PASSES) + len(FAILS)))
    for f in FAILS:
        print("  FAILED:", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
