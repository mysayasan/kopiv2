# Seed a realistic estate for the myiotsan SCREEN check, then get out of the way.
#
# WHY THIS IS SEPARATE. The screen check is a browser, and a browser cannot speak MQTT — so the
# one thing it cannot create for itself is the thing the screens exist to display: real telemetry
# that arrived over the wire from a real device. Everything downstream of that (charts, the
# current-value strip, an alert that actually fired, a command with a history row) is only
# meaningful if the readings underneath it are genuine.
#
# It also mints the two NON-ADMIN accounts. myiotsan draws a harder line than the rest of the
# suite — services/rbac.go makes ACTUATION ADMIN-ONLY, because "a bad relay write is physically
# dangerous in a way a bad PTZ move is not" — while the navigation rail hides Flows and Settings
# on a client-side `session.isAdmin`. Two mechanisms, one intent, and nothing had ever checked
# they agree. That check needs an operator and a viewer to sign in as.
#
# EMPTY SCREENS PROVE ALMOST NOTHING. A table with no rows renders no row controls, a chart with
# no points draws nothing, and a permission check against a screen that would be blank anyway
# cannot tell "refused" from "nothing to show". So this seeds until every screen has something
# on it, and the screen check asserts against a populated app.
#
#   python tools/fleetbench/iotsan_harness.py        # stand it up
#   python tools/fleetbench/seed_iotsan_screens.py   # then this
#   node tools/fleetbench/uicheck_iotsan.js <out> en
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from iotsan_harness import BASE, DeviceWire, admin, result, result_list

DEVICE_KEY = "screen-sensor-01"
TOPIC = "iot/tel/%s" % DEVICE_KEY
RELAY_TOPIC = "iot/cmd/%s/relay" % DEVICE_KEY

# A second device with actuation DISABLED. The screens must tell an operator why a control is
# unavailable, and that is only checkable against a device whose capability is genuinely off —
# distinguishing "the UI hid it" from "there was nothing to hide".
MUTE_KEY = "screen-sensor-02"

OPERATOR = {"username": "bench-operator", "password": "Operator!2345", "role": "operator"}
VIEWER = {"username": "bench-viewer", "password": "Viewer!2345", "role": "viewer"}

PROFILE = {
    "slug": "screen-sensor",
    "name": "Screen bench sensor",
    "vendor": "kopiv2-bench",
    "topicTemplate": TOPIC,
    "payloadFormat": "json",
    "keys": [
        {"key": "temp", "label": "Temperature", "unit": "C", "dataType": "number",
         "jsonPath": "temp", "deadband": 0.2, "min": -40, "max": 90},
        {"key": "door", "label": "Door", "dataType": "bool", "jsonPath": "door", "deadband": 0},
        {"key": "state", "label": "Relay state", "dataType": "number",
         "jsonPath": "state", "deadband": 0},
    ],
    "commands": [
        # A real actuation the Control tab can offer, with the bounds the server enforces.
        {"name": "relay", "label": "Relay", "kind": "switch",
         "topicTemplate": "iot/cmd/{deviceKey}/relay",
         "payloadTemplate": '{"state":{value}}', "confirmKey": "state",
         "min": 0, "max": 1},
        {"name": "setpoint", "label": "Setpoint", "kind": "number",
         "topicTemplate": "iot/cmd/{deviceKey}/setpoint",
         "payloadTemplate": '{"sp":{value}}', "min": 5, "max": 30},
    ],
}


def upsert_profile(c):
    r = c.post("/api/profiles", PROFILE)
    if r.status_code == 200:
        res = result(r) or {}
        return (res.get("profile") or res).get("id")
    existing = [p for p in result_list(c.get("/api/profiles")) if p.get("slug") == PROFILE["slug"]]
    if not existing:
        raise SystemExit("could not create the screen profile: %s" % r.text[:200])
    pid = existing[0]["id"]
    c.put("/api/profiles/%d" % pid, PROFILE)
    return pid


def upsert_device(c, profile_id, key, name, actuation):
    r = c.post("/api/devices", {
        "name": name, "deviceKey": key, "protocol": "mqtt", "profileId": profile_id,
        "enabled": True, "actuationEnabled": actuation, "tag": "screen-bench",
        "location": "Bench rack",
    })
    if r.status_code == 200:
        res = result(r) or {}
        dev = res.get("device") or res
        return dev.get("id"), res.get("password") or dev.get("password")
    existing = [d for d in result_list(c.get("/api/devices?limit=500")) if d.get("deviceKey") == key]
    if not existing:
        raise SystemExit("could not create device %s: %s" % (key, r.text[:200]))
    dev_id = existing[0]["id"]
    pw = (result(c.post("/api/devices/%d/password" % dev_id)) or {}).get("password")
    return dev_id, pw


def upsert_user(c, spec):
    """Create a non-admin account, resolving the role NAME to its id.

    The role id is looked up rather than assumed: a hardcoded 2 or 3 would silently mint the
    WRONG role, and a permission check against an account that is secretly an admin passes
    everything for the worst possible reason."""
    roles = result_list(c.get("/api/settings/roles"))
    match = [r for r in roles if (r.get("name") or "").lower() == spec["role"]]
    if not match:
        raise SystemExit("no %s role on this install; roles seen: %s"
                         % (spec["role"], [r.get("name") for r in roles]))
    role_id = match[0]["id"]

    existing = [u for u in result_list(c.get("/api/settings/users"))
                if (u.get("username") or "") == spec["username"]]
    if existing:
        uid = existing[0]["id"]
        c.post("/api/settings/users/%d/password" % uid, {"password": spec["password"]})
        return uid, role_id
    r = c.post("/api/settings/users", {
        "username": spec["username"], "displayName": spec["username"],
        "password": spec["password"], "roleId": role_id, "isActive": True,
    })
    if r.status_code != 200:
        raise SystemExit("could not create %s: %s" % (spec["username"], r.text[:300]))
    return (result(r) or {}).get("id"), role_id


def main():
    c = admin()
    print("signed in to", BASE)

    profile_id = upsert_profile(c)
    dev_id, pw = upsert_device(c, profile_id, DEVICE_KEY, "Screen bench sensor", True)
    mute_id, _ = upsert_device(c, profile_id, MUTE_KEY, "Screen bench sensor (no actuation)", False)
    print("profile=%s device=%s mute-device=%s" % (profile_id, dev_id, mute_id))

    # A rule that will actually fire, so the Alerts screen has a row on it.
    rules = {r.get("name"): r for r in result_list(c.get("/api/rules"))}
    if "Screen bench overheat" not in rules:
        c.post("/api/rules", {
            "name": "Screen bench overheat", "enabled": True, "deviceId": dev_id, "key": "temp",
            "condition": "above", "threshold": 40.0, "consecutiveSamples": 1,
            "cooldownSeconds": 1, "severity": "warning", "schedulePolicy": "always",
        })

    # THE PART A BROWSER CANNOT DO. Real readings, over the real broker, as the real device.
    wire = DeviceWire(DEVICE_KEY, pw)
    try:
        wire.subscribe("iot/cmd/%s/#" % DEVICE_KEY)
        # A shape worth charting rather than a flat line: a ramp the deadband admits, one door
        # transition, and a relay state the twin can confirm a command against.
        for i in range(40):
            wire.publish(TOPIC, json.dumps({
                "temp": 20.0 + i * 0.6,
                "door": bool(i % 7 == 0),
                "state": 0,
            }))
            time.sleep(0.15)
        # Cross the rule's threshold on purpose, so an alert exists to look at.
        wire.publish(TOPIC, json.dumps({"temp": 55.0}))
        time.sleep(3.0)
    finally:
        wire.close()

    stats = result(c.get("/api/devices/stats")) or {}
    latest = result(c.get("/api/devices/%d/latest" % dev_id)) or {}
    alerts = result_list(c.get("/api/alerts?limit=50"))
    print("ingest:", json.dumps(stats))
    print("keys with a current value:", sorted(latest.keys()))
    print("alerts:", len(alerts))

    # ASSERT THE SEED WORKED. A screen check run against an estate that silently failed to seed
    # reports empty screens as broken ones — and this suite has burned runs on exactly that.
    if not latest:
        raise SystemExit("SEED FAILED: the device stored no readings, so every screen below "
                         "would be empty for a reason that has nothing to do with the screens")
    if not alerts:
        raise SystemExit("SEED FAILED: no alert fired, so the Alerts screen has nothing on it")

    op_id, op_role = upsert_user(c, OPERATOR)
    vw_id, vw_role = upsert_user(c, VIEWER)
    print("operator=%s (role %s)  viewer=%s (role %s)" % (op_id, op_role, vw_id, vw_role))

    print("\nseeded. now:  node tools/fleetbench/uicheck_iotsan.js <out-dir> en")
    return 0


if __name__ == "__main__":
    sys.exit(main())
