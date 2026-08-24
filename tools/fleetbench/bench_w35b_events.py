# W3-5b bench: ONVIF events, digital inputs and relay outputs — against a device.
#
# WHAT IS ACTUALLY UNDER TEST. Two things, and both of them fail in ways a status code
# cannot show:
#
#   1. THE EVENT TRANSPORT'S FAILURE MODE IS SILENCE. A PullPoint subscription is a lease.
#      A camera drops it without a word if it is not renewed, and a door contact that has
#      stopped reporting looks exactly like a door nobody has opened. So this bench opens a
#      door and watches it arrive; then EXPIRES the subscription behind the appliance's back
#      and watches it (a) notice and (b) come back. `onvifsim.py` can lapse a subscription on
#      demand precisely because that is the interesting case.
#   2. A RELAY ACTS ON THE WORLD. The device records every state change, so the bench asserts
#      what the appliance actually SENT — including that a pulse was RELEASED, that a
#      bistable output the camera refuses to reconfigure is held and then let go, and that
#      switching something OFF is never refused however hard the rate limiter has been hit.
#
# It also covers the one that only exists because of the transport: on subscribing, a camera
# announces the CURRENT state of every input it has. Treated as events, every reconnect
# raises an alarm for every door that happens to be closed.
#
# Runs on the plain node image; no footage needed. About three minutes.
import json
import os
import sys
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import Node, NODE_PORTS, result_of, result_list, sh, PASSWORDS

urllib3.disable_warnings()
CHECKS = []

SIM = "onvifsim"
SIM_PORT = 8080
SIM_HOST_PORT = 18480
SIM_URL = "http://127.0.0.1:%d" % SIM_HOST_PORT
SIM_XADDR = "http://%s:%d/onvif/device_service" % (SIM, SIM_PORT)


def check(name, ok, detail=""):
    CHECKS.append((name, bool(ok), detail))
    print(("PASS  " if ok else "FAIL  ") + name + ("   " + detail if detail else ""))


def report():
    ok = sum(1 for _, good, _ in CHECKS if good)
    print("\n%d/%d checks passed" % (ok, len(CHECKS)))
    for name, good, detail in CHECKS:
        if not good:
            print("  FAILED: %s   %s" % (name, detail))
    return 0 if ok == len(CHECKS) else 1


def node(name, auth=None):
    n = Node("https://127.0.0.1:%d" % NODE_PORTS[name])
    if auth:
        n.auth = auth
        return n
    for pw in PASSWORDS:
        n.auth = ("admin", pw)
        if n.get("/api/pairing/status").status_code == 200:
            return n
    raise SystemExit("cannot authenticate to " + name)


def start_sim():
    sh("docker", "rm", "-f", SIM, check=False)
    script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "onvifsim.py")
    sh("docker", "run", "-d", "--name", SIM, "--network", "benchnet",
       "-p", "%d:%d" % (SIM_HOST_PORT, SIM_PORT), "-v", script + ":/onvifsim.py:ro",
       "python:3-slim", "python", "/onvifsim.py", str(SIM_PORT))
    deadline = time.time() + 90
    while time.time() < deadline:
        try:
            if requests.get(SIM_URL + "/journal", timeout=3).status_code == 200:
                return True
        except Exception:
            time.sleep(1)
    return False


def device():
    """Read the device's journal, retrying a slow read.

    The port forward into the bench network occasionally stalls for a second or two on
    Windows, and a bench that dies on one slow read is measuring Docker rather than the
    product — which is worse than useless, because it looks like a product failure.
    """
    last = None
    for _ in range(5):
        try:
            return requests.get(SIM_URL + "/journal", timeout=10).json()
        except Exception as err:  # noqa: BLE001 - any transport hiccup is worth one retry
            last = err
            time.sleep(2)
    raise last


def reset_journal():
    requests.post(SIM_URL + "/journal/reset", timeout=5)


def flip_input(token):
    return requests.post(SIM_URL + "/inputs/" + token, timeout=5).json()


def expire_subscriptions():
    requests.post(SIM_URL + "/subscriptions/expire", timeout=5)


def relay_states(journal):
    """Every relay state change the device was actually sent, in order."""
    return [(e["detail"].get("token"), e["detail"].get("state"))
            for e in journal["journal"]
            if e["action"] == "SetRelayOutputState" and not e["detail"].get("refused")]


def add_camera(n):
    r = n.post("/api/cameras/discovered", {
        "name": "Gate camera", "host": SIM, "port": SIM_PORT,
        "xAddr": SIM_XADDR,
        "mediaXAddr": "http://%s:%d/onvif/media_service" % (SIM, SIM_PORT),
        "rtspUrl": "rtsp://ptzcam:8554/cam",
        "username": "", "password": "", "description": "w3-5b bench camera",
    })
    cam = result_of(r)
    for key in ("id", "cameraId", "result"):
        value = cam.get(key)
        if isinstance(value, (int, float)) and value:
            return int(value), r
    return None, r


def notifications(n, limit=200):
    return result_list(n.get("/api/notifications?limit=%d" % limit), "notifications")


def wait_for_notification(n, needle, timeout, since=0):
    """Wait for a notification whose title contains `needle`. Returns the row or None."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        for row in notifications(n):
            if needle.lower() in (row.get("title") or "").lower() and int(row.get("id") or 0) > since:
                return row
        time.sleep(3)
    return None


def newest_notification_id(n):
    rows = notifications(n)
    return max([int(r.get("id") or 0) for r in rows] or [0])


def main():
    if not start_sim():
        check("the simulated ONVIF device came up", False)
        return report()
    check("the simulated ONVIF device came up", True)

    a = node("node-a")
    cam, saved = add_camera(a)
    check("an ONVIF camera can be saved", bool(cam), saved.text[:200])
    if not cam:
        return report()

    # ---- relays: what the camera has, and who may see it ----------------------------
    listed = result_list(a.get("/api/cameras/%d/relays" % cam), "relays")
    check("the camera's relay outputs are listed",
          sorted(r["token"] for r in listed) == ["RELAY_1", "RELAY_2"], json.dumps(listed)[:240])
    # The device says nothing useful about mode until it is configured, and "unknown" has to
    # count as bistable — the reading where WE are responsible for switching it off.
    check("an output the device calls bistable is reported as one",
          all(r.get("bistable") for r in listed), json.dumps(listed)[:240])

    # ---- a pulse, and the two ways it can be released --------------------------------
    #
    # RELAY_2 lets itself be reconfigured as a timed pulse, so the DEVICE owns the release
    # and this appliance dying mid-pulse is harmless.
    reset_journal()
    r = a.post("/api/cameras/%d/relays/RELAY_2/fire" % cam, {"action": "pulse", "pulseSeconds": 3})
    check("an output can be pulsed", r.status_code == 200, r.text[:200])
    dev = device()
    settings_calls = [e for e in dev["journal"] if e["action"] == "SetRelayOutputSettings"]
    check("the camera is asked to run the pulse ITSELF where it can",
          any(not e["detail"].get("refused") for e in settings_calls), json.dumps(settings_calls)[:240])
    check("and the output was switched on",
          ("RELAY_2", "active") in relay_states(dev), json.dumps(relay_states(dev)))
    after = result_list(a.get("/api/cameras/%d/relays" % cam), "relays")
    held = {x["token"]: x.get("heldByUs") for x in after}
    check("an output the device releases itself is NOT reported as held by us",
          held.get("RELAY_2") is not True, json.dumps(held))

    # RELAY_1 REFUSES to be reconfigured, so this appliance has to hold it — and has to say
    # so, because that is the one state where a restart leaves the output energised.
    reset_journal()
    r = a.post("/api/cameras/%d/relays/RELAY_1/fire" % cam, {"action": "pulse", "pulseSeconds": 4})
    check("an output the camera will not time can still be pulsed", r.status_code == 200, r.text[:200])
    after = result_list(a.get("/api/cameras/%d/relays" % cam), "relays")
    held = {x["token"]: x.get("heldByUs") for x in after}
    check("and the screen is told WE are holding it", held.get("RELAY_1") is True, json.dumps(held))
    # ...and it is actually released when the pulse is up.
    time.sleep(8)
    states = relay_states(device())
    check("the held output is released without anybody asking",
          ("RELAY_1", "inactive") in states, json.dumps(states))
    after = result_list(a.get("/api/cameras/%d/relays" % cam), "relays")
    held = {x["token"]: x.get("heldByUs") for x in after}
    check("and the hold is dropped once it is released", held.get("RELAY_1") is not True, json.dumps(held))

    # ---- THE RULE THAT MATTERS MOST: OFF IS NEVER REFUSED ----------------------------
    #
    # Hammer the rate limiter with automatic-shaped traffic first, then switch off. A
    # limiter that can block an OFF is a siren nobody can silence.
    a.post("/api/cameras/%d/relays/RELAY_1/fire" % cam, {"action": "on"})
    reset_journal()
    for _ in range(4):
        a.post("/api/cameras/%d/relays/RELAY_1/fire" % cam, {"action": "on"})
    r = a.post("/api/cameras/%d/relays/RELAY_1/fire" % cam, {"action": "off"})
    check("switching an output OFF is never refused", r.status_code == 200, r.text[:200])
    time.sleep(1)
    check("and the DEVICE really was switched off",
          device()["relays"]["RELAY_1"]["active"] is False, json.dumps(device()["relays"]))

    r = a.post("/api/cameras/%d/relays/RELAY_1/fire" % cam, {"action": "pulse", "pulseSeconds": 9999})
    check("a pulse longer than the cap is refused rather than clamped",
          r.status_code != 200, r.text[:200])
    r = a.post("/api/cameras/%d/relays/NOPE/fire" % cam, {"action": "pulse"})
    check("an output the camera does not have is refused", r.status_code != 200, r.text[:200])

    # ---- every actuation is audited ---------------------------------------------------
    trail = result_list(a.get("/api/audit?limit=200"), "logs", "entries")
    fires = [e for e in trail if e.get("action") == "relay.fire"]
    check("every actuation is in the audit trail", len(fires) >= 5, str(len(fires)))
    check("including the ones that FAILED",
          any((e.get("outcome") or "") == "failure" for e in fires),
          json.dumps([e.get("outcome") for e in fires][:8]))

    # ---- the event listener ------------------------------------------------------------
    r = a.get("/api/settings/camera-events")
    cfg = result_of(r)
    check("the event listener is OFF by default", cfg.get("enabled") is False, json.dumps(cfg))

    before = newest_notification_id(a)
    cfg.update({"enabled": True, "leaseSeconds": 30, "pullTimeoutSeconds": 5, "lostAfterSeconds": 30})
    r = a.put("/api/settings/camera-events", cfg)
    check("the event listener can be switched on", r.status_code == 200, r.text[:200])

    # Give the reconcile loop a pass and the subscription time to establish.
    deadline = time.time() + 90
    subscribed = False
    while time.time() < deadline:
        if device().get("subscriptions", 0) > 0:
            subscribed = True
            break
        time.sleep(3)
    check("the appliance subscribes to the camera's events", subscribed,
          json.dumps({"subscriptions": device().get("subscriptions")}))
    if not subscribed:
        return report()

    # SUBSCRIBING MUST NOT RAISE ANYTHING. The camera announces the current state of every
    # input the moment we connect; treated as events, every reconnect alarms about every
    # door that happens to be closed.
    time.sleep(8)
    fresh = [n for n in notifications(a) if int(n.get("id") or 0) > before
             and "input" in (n.get("title") or "").lower()]
    check("connecting does not raise an alarm for every closed door",
          len(fresh) == 0, json.dumps([n.get("title") for n in fresh][:6]))

    # ---- opening a door ----------------------------------------------------------------
    before = newest_notification_id(a)
    flip_input("DIGIT_INPUT_000")
    hit = wait_for_notification(a, "input", 60, since=before)
    check("opening a door contact reaches the operator", hit is not None,
          json.dumps(hit or {})[:240])
    if hit:
        check("and it names WHICH input, on WHICH camera",
              "DIGIT_INPUT_000" in json.dumps(hit) and "Gate camera" in json.dumps(hit),
              json.dumps(hit)[:240])
        # A sensor reading is a DEVICE alert, not a detection: a destination has to be able
        # to subscribe to door contacts without subscribing to every person the AI sees.
        check("a door contact is filed as a device alert, not a vision alert",
              (hit.get("category") or "") == "device.alert", json.dumps(hit.get("category")))

    # THE FEED IS THE HOME, and it has to be answerable BY CAMERA — "what happened on this
    # camera at 02:14" is the question, and an event that cannot be filtered to the camera
    # it happened on does not answer it.
    #
    # The first version of this code also wrote an AI alert-log row, and this bench is what
    # found that it never appeared: alert_event requires a rule id and a digital input has
    # no rule, so the write was refused and the only symptom was a log line. See
    # services/camera_events.go.
    per_camera = result_list(a.get("/api/notifications?limit=100&cameraId=%d" % cam), "notifications")
    inputs = [x for x in per_camera if "input" in (x.get("title") or "").lower()]
    check("the input is in the feed, filterable to the camera it happened on",
          len(inputs) >= 1, json.dumps([x.get("title") for x in per_camera][:8]))
    check("and the AI alert log is left to detections, which is all it can hold",
          all("onvif" not in (x.get("detectionType") or "")
              for x in result_list(a.get("/api/vision/alerts?limit=50"), "alerts", "items")), "")

    # ---- THE FAILURE MODE THAT IS SILENCE ----------------------------------------------
    before = newest_notification_id(a)
    expire_subscriptions()
    hit = wait_for_notification(a, "events stopped", 180, since=before)
    check("a subscription that lapses is reported rather than left silent", hit is not None,
          json.dumps(hit or {})[:240])

    # ...and it comes back on its own.
    deadline = time.time() + 180
    recovered = False
    while time.time() < deadline:
        if device().get("subscriptions", 0) > 0:
            recovered = True
            break
        time.sleep(5)
    check("and the listener re-subscribes without being restarted", recovered,
          json.dumps({"subscriptions": device().get("subscriptions")}))

    if recovered:
        before = newest_notification_id(a)
        flip_input("DIGIT_INPUT_001")
        hit = wait_for_notification(a, "input", 60, since=before)
        check("a door opened AFTER the recovery still reaches the operator", hit is not None,
              json.dumps(hit or {})[:240])

    # ---- who may switch the building's outputs ------------------------------------------
    roles = result_list(a.get("/api/settings/roles"), "roles")
    role_ids = {(r.get("name") or "").lower(): r.get("id") for r in roles or []}
    check("the built-in roles are readable", bool(role_ids.get("operator")), json.dumps(role_ids))
    if not role_ids.get("operator"):
        return report()
    for uname, role in (("relay-op", "operator"), ("relay-view", "viewer")):
        a.post("/api/settings/users", {
            "username": uname, "password": "Bench-Passw0rd!", "displayName": uname,
            "roleId": role_ids[role], "isActive": True, "mustChangePassword": False,
        })
    op = node("node-a", ("relay-op", "Bench-Passw0rd!"))
    vw = node("node-a", ("relay-view", "Bench-Passw0rd!"))

    # An operator SEES the outputs — a screen that cannot list them cannot offer a button —
    # but the default operator preset does not get to fire one. Switching a gate is a
    # decision about the building, granted deliberately, not a side effect of being allowed
    # to move a camera.
    r = op.get("/api/cameras/%d/relays" % cam)
    check("an operator can see the outputs", r.status_code == 200, str(r.status_code))
    r = op.post("/api/cameras/%d/relays/RELAY_1/fire" % cam, {"action": "pulse"})
    check("an operator does not get to switch them by default", r.status_code == 403,
          "%d %s" % (r.status_code, r.text[:120]))
    r = vw.get("/api/cameras/%d/relays" % cam)
    check("a viewer cannot even see them", r.status_code == 403, str(r.status_code))

    return report()


if __name__ == "__main__":
    try:
        sys.exit(main())
    finally:
        sh("docker", "rm", "-f", SIM, check=False)
