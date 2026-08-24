# W3-5 bench: PTZ presets, home, guard tours and alarm recall — against a device.
#
# WHAT MAKES THIS BENCHABLE AT ALL. The harness's cameras are mediamtx RTSP sources with no
# ONVIF service, and every claim in this item is an ONVIF conversation. `onvifsim.py` is a
# small ONVIF PTZ device that keeps the state a real dome keeps and, crucially, RECORDS WHAT
# IT WAS ASKED TO DO — so this bench asserts what the appliance actually SENT, in what order
# and roughly when, rather than that an API call returned 200. A patrol that persuades its
# own database it is running while sending nothing would pass the second and fail the first.
#
# The claims under test, none of which a unit test reaches:
#
#   1. The presets live on the CAMERA. Saving one through the appliance puts it on the
#      device; deleting one takes it off; the appliance never shows a preset the device
#      does not have.
#   2. A guard tour actually WALKS. The device sees GotoPreset for stop 1, then stop 2, then
#      stop 3, then stop 1 again, spaced by the dwell.
#   3. A tour SURVIVES A RESTART, because IsRunning is a persisted column and an appliance
#      that reboots at 03:00 must come back patrolling.
#   4. Presets deleted from the camera's own web page STOP the tour and RAISE it. A patrol
#      that has quietly stopped patrolling is a security failure.
#   5. An ALARM points the camera and SUSPENDS the patrol, so the recording is not of the
#      corridor next door three seconds later.
#   6. A PERSON at the ring outranks both.
#   7. The camera's own refusal reaches the operator in the camera's own words, not as
#      "status 500".
#   8. An operator may do all of this and a viewer may do none of it — including the READS,
#      which is the half that gets granted wrong.
#
# Runs on the plain node image; no footage needed. About three minutes, most of it dwell.
import json
import os
import sys
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import Node, NODE_PORTS, ROOT, result_of, result_list, sh, PASSWORDS

urllib3.disable_warnings()
CHECKS = []

SIM = "onvifsim"
SIM_PORT = 8080
SIM_HOST_PORT = 18480
SIM_URL = "http://127.0.0.1:%d" % SIM_HOST_PORT
# What the NODE calls it: a docker network alias, because the node reaches the device from
# inside benchnet and 127.0.0.1 there is the node itself.
SIM_XADDR = "http://%s:%d/onvif/device_service" % (SIM, SIM_PORT)

# The tour dwell. Short enough to bench, long enough that the 2s runner tick cannot make a
# step land in the wrong slot.
DWELL = 8


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


def wait_node(node_name, timeout=180):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            if node(node_name).get("/api/pairing/status").status_code == 200:
                return True
        except Exception:
            pass
        time.sleep(3)
    return False


# --- the simulated dome -------------------------------------------------------------

def start_sim():
    sh("docker", "rm", "-f", SIM, check=False)
    script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "onvifsim.py")
    sh("docker", "run", "-d", "--name", SIM, "--network", "benchnet",
       "-p", "%d:%d" % (SIM_HOST_PORT, SIM_PORT),
       "-v", script + ":/onvifsim.py:ro",
       "python:3-slim", "python", "/onvifsim.py", str(SIM_PORT))
    deadline = time.time() + 90
    while time.time() < deadline:
        try:
            if requests.get(SIM_URL + "/journal", timeout=3).status_code == 200:
                return True
        except Exception:
            time.sleep(1)
    return False


def journal(reset=False):
    if reset:
        requests.post(SIM_URL + "/journal/reset", timeout=5)
        return {}
    return requests.get(SIM_URL + "/journal", timeout=5).json()


def gotos(entries):
    """The preset tokens the device was actually sent to, in order."""
    return [e["detail"].get("token") for e in entries
            if e["action"] == "GotoPreset" and not e["detail"].get("refused")]


def add_ptz_camera(n):
    r = n.post("/api/cameras/discovered", {
        "name": "Yard dome", "host": SIM, "port": SIM_PORT,
        "xAddr": SIM_XADDR,
        "mediaXAddr": "http://%s:%d/onvif/media_service" % (SIM, SIM_PORT),
        "ptzXAddr": "http://%s:%d/onvif/ptz_service" % (SIM, SIM_PORT),
        "ptzSupported": True,
        "profileToken": "MainProfile",
        "rtspUrl": "rtsp://ptzcam:8554/cam",
        "username": "", "password": "", "description": "w3-5 bench PTZ camera",
    })
    # THE result_of TRAP, fifth sighting. Saving a camera answers with a BARE ID —
    # {"result": 1} — and result_of re-wraps any non-dict result as {"result": <it>} so it
    # can always return a dict. So the id is under "result", not under "id", and reading
    # only "id"/"cameraId" reports a save that worked as a failure.
    cam = result_of(r)
    for key in ("id", "cameraId", "result"):
        value = cam.get(key)
        if isinstance(value, (int, float)) and value:
            return int(value), r
        if isinstance(value, dict) and value.get("id"):
            return int(value["id"]), r
    return None, r


def presets(n, cam):
    return result_list(n.get("/api/cameras/%d/ptz/presets" % cam), "presets")


def tours(n, cam):
    return result_list(n.get("/api/cameras/%d/ptz/tours" % cam), "tours")


def main():
    if not start_sim():
        check("the simulated ONVIF dome came up", False)
        return report()
    check("the simulated ONVIF dome came up", True)

    a = node("node-a")
    cam, saved = add_ptz_camera(a)
    check("a PTZ camera can be saved", bool(cam), saved.text[:200])
    if not cam:
        return report()

    # ---- 1. the presets live on the CAMERA -----------------------------------------
    check("a camera with no saved positions answers with an empty list, not an error",
          presets(a, cam) == [], json.dumps(presets(a, cam))[:160])

    tokens = {}
    for name in ("Front gate", "Loading bay", "Car park"):
        r = a.post("/api/cameras/%d/ptz/presets" % cam, {"name": name})
        tokens[name] = (result_of(r) or {}).get("token")
    check("three positions can be saved", all(tokens.values()), json.dumps(tokens))
    if not all(tokens.values()):
        return report()

    device = journal()
    check("the positions were stored ON THE DEVICE, not in our database",
          sorted(p["name"] for p in device["presets"].values())
          == ["Car park", "Front gate", "Loading bay"],
          json.dumps(device["presets"]))

    listed = presets(a, cam)
    check("the appliance lists exactly what the device holds",
          sorted(p["name"] for p in listed) == ["Car park", "Front gate", "Loading bay"],
          json.dumps(listed)[:200])

    journal(reset=True)
    r = a.post("/api/cameras/%d/ptz/presets/%s/goto" % (cam, tokens["Front gate"]), {})
    check("a position can be recalled", r.status_code == 200, r.text[:160])
    check("and the DEVICE was sent there", gotos(journal()["journal"]) == [tokens["Front gate"]],
          json.dumps(journal()["journal"])[:200])

    # ---- 7. the camera's own refusal, in the camera's own words ---------------------
    r = a.post("/api/cameras/%d/ptz/presets/NOPE/goto" % cam, {})
    check("recalling a position the camera does not have is refused",
          r.status_code != 200, str(r.status_code))
    check("and the refusal is the CAMERA's sentence, not 'status 500'",
          "preset token does not exist" in r.text and "500" not in r.text, r.text[:200])

    # ---- home ------------------------------------------------------------------------
    journal(reset=True)
    r = a.post("/api/cameras/%d/ptz/home/set" % cam, {})
    check("the current position can be made home", r.status_code == 200, r.text[:160])
    r = a.post("/api/cameras/%d/ptz/home" % cam, {})
    check("the camera can be sent home", r.status_code == 200, r.text[:160])
    actions = [e["action"] for e in journal()["journal"]]
    check("home is set and recalled on the DEVICE",
          "SetHomePosition" in actions and "GotoHomePosition" in actions, json.dumps(actions))

    status = result_of(a.get("/api/cameras/%d/ptz/status" % cam))
    check("the camera reports where it is pointing",
          isinstance(status, dict) and status.get("hasPosition") is True, json.dumps(status)[:200])

    # ---- tour refusals ----------------------------------------------------------------
    r = a.post("/api/cameras/%d/ptz/tours" % cam, {"name": "Solo", "dwellSeconds": DWELL,
                                                   "stops": [{"preset": tokens["Front gate"]}]})
    check("a one-stop tour is refused, and the refusal says why",
          r.status_code != 200 and "two stops" in r.text, r.text[:200])

    r = a.post("/api/cameras/%d/ptz/tours" % cam, {
        "name": "Ghost", "dwellSeconds": DWELL,
        "stops": [{"preset": tokens["Front gate"]}, {"preset": "NOPE"}]})
    check("a tour naming a position the camera does not have is refused at SAVE time",
          r.status_code != 200 and "no preset" in r.text, r.text[:200])

    r = a.post("/api/cameras/%d/ptz/tours" % cam, {
        "name": "Strobe", "dwellSeconds": 1,
        "stops": [{"preset": tokens["Front gate"]}, {"preset": tokens["Car park"]}]})
    check("a one-second dwell is refused rather than quietly clamped",
          r.status_code != 200, r.text[:200])

    # ---- 2. THE PATROL ACTUALLY WALKS -------------------------------------------------
    route = [tokens["Front gate"], tokens["Loading bay"], tokens["Car park"]]
    made = result_of(a.post("/api/cameras/%d/ptz/tours" % cam, {
        "name": "Perimeter", "dwellSeconds": DWELL,
        "stops": [{"preset": token} for token in route],
    }))
    tour_id = made.get("id")
    check("a tour can be saved", bool(tour_id), json.dumps(made)[:200])
    if not tour_id:
        return report()
    check("a saved tour is not running until somebody starts it",
          made.get("isRunning") is False, json.dumps(made)[:200])

    journal(reset=True)
    r = a.post("/api/cameras/%d/ptz/tours/%d/start" % (cam, tour_id), {})
    check("a tour can be started", r.status_code == 200, r.text[:200])

    # Four stops' worth: enough to see it WRAP, which is where an off-by-one lives.
    time.sleep(DWELL * 3 + 6)
    walked = gotos(journal()["journal"])
    check("the device was walked round the route, in order, and wrapped",
          walked[:4] == route + route[:1], json.dumps(walked))
    # A patrol that fired every tick instead of every dwell would look right in order and
    # very wrong in count.
    check("it stepped once per dwell, not once per tick",
          4 <= len(walked) <= 6, "%d steps in ~%ds at a %ds dwell" % (len(walked), DWELL * 3 + 6, DWELL))

    # ---- 5. AN ALARM POINTS THE CAMERA AND THE PATROL WAITS ---------------------------
    # Driven through a real detection rule, not by calling the service: the claim is that a
    # RULE can point a camera, and the wiring between the vision monitor and the PTZ service
    # is exactly the part a unit test does not cross.
    rule = a.post("/api/vision/rules", {
        "cameraId": cam, "name": "Gate recall", "detectionType": "motion",
        "ruleConfig": json.dumps({"ptzRecall": {"preset": tokens["Car park"], "holdSeconds": 40}}),
        "threshold": 0.5, "minFrames": 1, "cooldownSeconds": 0, "isEnabled": True,
    })
    check("a rule can carry a PTZ recall", rule.status_code == 200, rule.text[:200])

    journal(reset=True)
    # Raised through the real create-alert API rather than by calling the service: the
    # appliance films a test pattern, so no detector will find a person for us, and this is
    # the OTHER path a rule's alert travels — the one the rule editor's Test button uses.
    rule_id = (result_of(rule) or {}).get("id")
    trigger = a.post("/api/vision/alerts", {
        "ruleId": rule_id, "cameraId": cam, "detectionType": "motion",
        "label": "bench recall", "confidence": 0.9,
    })
    if trigger.status_code != 200:
        check("an alert could be raised for the recall path", False, trigger.text[:200])
    else:
        time.sleep(4)
        after_alert = gotos(journal()["journal"])
        check("an alert points the camera at the position the rule names",
              tokens["Car park"] in after_alert, json.dumps(after_alert))
        # And the patrol waits: well past a dwell, the camera must still be where the alarm
        # put it rather than back on its rotation.
        journal(reset=True)
        time.sleep(DWELL + 4)
        check("the patrol waits while the camera is held on the alarm",
              gotos(journal()["journal"]) == [], json.dumps(journal()["journal"])[:200])

    # ---- 6. A PERSON OUTRANKS BOTH ----------------------------------------------------
    # Wait out the recall hold first, so what is measured is the JOG and not the alarm.
    time.sleep(45)
    journal(reset=True)
    r = a.post("/api/cameras/%d/ptz/move" % cam, {"direction": "left", "speed": 0.3})
    check("an operator can jog the camera", r.status_code == 200, r.text[:200])
    a.post("/api/cameras/%d/ptz/stop" % cam, {})
    time.sleep(DWELL + 4)
    check("the patrol does not step while an operator has the camera",
          gotos(journal()["journal"]) == [], json.dumps(journal()["journal"])[:240])
    # ...and comes back when they are done.
    time.sleep(34)
    check("the patrol resumes once the operator stops driving",
          len(gotos(journal()["journal"])) > 0, json.dumps(journal()["journal"])[:200])

    # ---- 3. IT SURVIVES A RESTART -----------------------------------------------------
    sh("docker", "restart", "node-a")
    if not wait_node("node-a"):
        check("the node came back after a restart", False)
        return report()
    a = node("node-a")
    rows = tours(a, cam)
    check("the tour is still there, and still running, after the appliance restarts",
          len(rows) == 1 and rows[0].get("isRunning") is True, json.dumps(rows)[:240])
    journal(reset=True)
    time.sleep(DWELL * 2 + 6)
    check("and it is actually patrolling again, not merely claiming to",
          len(gotos(journal()["journal"])) >= 2, json.dumps(journal()["journal"])[:240])

    # ---- 4. PRESETS DELETED ON THE CAMERA STOP THE PATROL, LOUDLY ---------------------
    before_notifications = len(result_list(a.get("/api/notifications?limit=200"), "notifications"))
    requests.post(SIM_URL + "/presets/wipe", timeout=5)
    time.sleep(DWELL * 2 + 8)
    rows = tours(a, cam)
    check("a tour whose positions are gone stops claiming to be running",
          rows and rows[0].get("isRunning") is False, json.dumps(rows)[:240])
    check("and its stops are reported as missing rather than silently skipped",
          rows and all(stop.get("missing") for stop in (rows[0].get("stopList") or [])),
          json.dumps(rows[0].get("stopList") if rows else None)[:240])
    notes = result_list(a.get("/api/notifications?limit=200"), "notifications")
    stopped = [n for n in notes if "tour" in (n.get("title") or "").lower()]
    check("somebody is TOLD the patrol stopped",
          len(notes) > before_notifications and len(stopped) >= 1,
          json.dumps([n.get("title") for n in notes[:6]]))
    check("and told once, not once per sweep", len(stopped) == 1, str(len(stopped)))

    r = a.post("/api/cameras/%d/ptz/tours/%d/start" % (cam, tour_id), {})
    check("restarting a tour whose positions are gone is refused, not accepted silently",
          r.status_code != 200 and "no longer on the camera" in r.text, r.text[:200])

    # ---- 9. THE VERBS THE REST OF THIS BENCH NEVER USED --------------------------------
    #
    # A bench only covers the verbs it uses. W3-3b's bench passed 25/25 without ever
    # DELETING a camera, and the delete was broken for most of the fleet. Everything below
    # is a verb the checks above happen not to reach.

    # A tour can be edited, and editing a running one keeps it running rather than silently
    # stopping the patrol under an operator adjusting a dwell.
    for name in ("Front gate", "Loading bay", "Car park"):
        r = a.post("/api/cameras/%d/ptz/presets" % cam, {"name": name})
        tokens[name] = (result_of(r) or {}).get("token")
    route = [tokens["Front gate"], tokens["Loading bay"], tokens["Car park"]]
    edited = result_of(a.post("/api/cameras/%d/ptz/tours/%d" % (cam, tour_id), {
        "name": "Perimeter", "dwellSeconds": DWELL,
        "stops": [{"preset": token} for token in route],
    }))
    check("a tour can be edited back into a walkable route",
          edited.get("stopList") and not any(x.get("missing") for x in edited["stopList"]),
          json.dumps(edited.get("stopList")))

    a.post("/api/cameras/%d/ptz/tours/%d/start" % (cam, tour_id), {})
    still = result_of(a.post("/api/cameras/%d/ptz/tours/%d" % (cam, tour_id), {
        "name": "Perimeter", "dwellSeconds": DWELL + 2,
        "stops": [{"preset": token} for token in route],
    }))
    check("editing a RUNNING tour does not silently stop the patrol",
          still.get("isRunning") is True, json.dumps(still)[:200])

    dup = a.post("/api/cameras/%d/ptz/tours" % cam, {
        "name": "perimeter", "dwellSeconds": DWELL,
        "stops": [{"preset": route[0]}, {"preset": route[1]}]})
    check("a duplicate tour name on the same camera is refused whatever its case",
          dup.status_code != 200 and "Perimeter" in dup.text, dup.text[:200])

    # STOPPING one. Never exercised above, and the half that matters: a tour that cannot be
    # stopped is a camera nobody can take back.
    journal(reset=True)
    r = a.post("/api/cameras/%d/ptz/tours/%d/stop" % (cam, tour_id), {})
    check("a patrol can be stopped", r.status_code == 200 and (result_of(r) or {}).get("isRunning") is False,
          r.text[:200])
    time.sleep(DWELL + 6)
    check("and a stopped patrol really stops moving the camera",
          gotos(journal()["journal"]) == [], json.dumps(journal()["journal"])[:240])

    # DELETING a saved position, through the appliance, off the device.
    doomed = tokens["Car park"]
    r = a.post("/api/cameras/%d/ptz/presets/%s/delete" % (cam, doomed), {})
    check("a saved position can be deleted", r.status_code == 200, r.text[:200])
    check("and it is gone from the DEVICE, not just from our screen",
          doomed not in journal()["presets"], json.dumps(journal()["presets"]))
    check("the appliance stops offering it", doomed not in [x["token"] for x in presets(a, cam)],
          json.dumps(presets(a, cam))[:200])
    r = a.post("/api/cameras/%d/ptz/presets/%s/delete" % (cam, doomed), {})
    check("deleting it twice is refused in the camera's words, not as a 500",
          r.status_code != 200 and "does not exist" in r.text, r.text[:200])

    # OVERWRITING one: the "point it somewhere else and keep the name" gesture.
    keep = tokens["Front gate"]
    r = a.post("/api/cameras/%d/ptz/presets" % cam, {"name": "Front gate", "token": keep})
    check("a saved position can be re-pointed under the same name",
          r.status_code == 200 and (result_of(r) or {}).get("token") == keep, r.text[:200])
    check("and it did not become a SECOND position with the same name",
          [x["name"] for x in presets(a, cam)].count("Front gate") == 1,
          json.dumps([x["name"] for x in presets(a, cam)]))

    # DELETING A TOUR.
    scratch = result_of(a.post("/api/cameras/%d/ptz/tours" % cam, {
        "name": "Scratch", "dwellSeconds": DWELL,
        "stops": [{"preset": route[0]}, {"preset": route[1]}]}))
    r = a.post("/api/cameras/%d/ptz/tours/%d/delete" % (cam, scratch.get("id")), {})
    check("a tour can be deleted", r.status_code == 200, r.text[:200])
    check("and it is gone from the list",
          scratch.get("id") not in [t["id"] for t in tours(a, cam)],
          json.dumps([t["name"] for t in tours(a, cam)]))

    # ---- 8. who may do what ------------------------------------------------------------
    roles = result_list(a.get("/api/settings/roles"), "roles")
    role_ids = {(r.get("name") or "").lower(): r.get("id") for r in roles or []}
    check("the built-in roles are readable", bool(role_ids.get("viewer")), json.dumps(role_ids))
    if not role_ids.get("viewer"):
        return report()
    for uname, role in (("ptz-op", "operator"), ("ptz-view", "viewer")):
        a.post("/api/settings/users", {
            "username": uname, "password": "Bench-Passw0rd!", "displayName": uname,
            "roleId": role_ids[role], "isActive": True, "mustChangePassword": False,
        })
    op = node("node-a", ("ptz-op", "Bench-Passw0rd!"))
    vw = node("node-a", ("ptz-view", "Bench-Passw0rd!"))

    # THE HALF THAT GETS GRANTED WRONG. An operator who can POST a recall and cannot GET the
    # preset list has a panel with no places on it — the capability the role model says they
    # have, unusable. Same shape as the evidence export shipped with.
    r = op.get("/api/cameras/%d/ptz/presets" % cam)
    check("an operator can READ the saved positions", r.status_code == 200, str(r.status_code))
    r = op.get("/api/cameras/%d/ptz/tours" % cam)
    check("an operator can READ the tours", r.status_code == 200, str(r.status_code))
    r = op.post("/api/cameras/%d/ptz/tours" % cam, {
        "name": "Operator tour", "dwellSeconds": DWELL, "stops": []})
    check("an operator may write tours (refused here on content, not on permission)",
          r.status_code != 403, "%d %s" % (r.status_code, r.text[:120]))

    r = vw.get("/api/cameras/%d/ptz/presets" % cam)
    check("a viewer may NOT read the saved positions", r.status_code == 403, str(r.status_code))
    r = vw.post("/api/cameras/%d/ptz/presets/%s/goto" % (cam, "PRESET_1"), {})
    check("a viewer may NOT move the camera", r.status_code == 403, str(r.status_code))

    # ---- the trail ---------------------------------------------------------------------
    trail = result_list(a.get("/api/audit?limit=200"), "logs", "entries")
    actions = [e.get("action") for e in trail]
    check("saving a position is audited", "ptz.preset_save" in actions, json.dumps(actions[:12]))
    check("starting and stopping a patrol is audited", "ptz.tour_run" in actions,
          json.dumps(actions[:12]))
    # Recalling one is deliberately NOT audited: an operator driving a camera makes one per
    # press, and a trail that fills with them is a trail nobody reads.
    check("recalling a position is NOT audited", "ptz.preset_goto" not in actions, "")

    # ---- 10. DELETING THE CAMERA, which nothing above does ------------------------------
    #
    # Last, because it destroys what every check above used. A tour left behind by a deleted
    # camera keeps commanding a device that is no longer configured, every dwell, forever —
    # and is listed under an id nothing can render. W3-2 shipped exactly this shape (its
    # descriptors), and it was found only when a bench finally deleted a camera.
    a.post("/api/cameras/%d/ptz/tours/%d/start" % (cam, tour_id), {})
    r = a.delete("/api/cameras/%d" % cam)
    check("a camera with a running guard tour can be deleted", r.status_code == 200, r.text[:240])
    if r.status_code == 200:
        left = result_list(a.get("/api/cameras/%d/ptz/tours" % cam), "tours")
        check("its tours go with it", left == [], json.dumps(left)[:240])
        journal(reset=True)
        time.sleep(DWELL + 6)
        check("and nothing is still commanding the camera that no longer exists",
              gotos(journal()["journal"]) == [], json.dumps(journal()["journal"])[:240])

    return report()


if __name__ == "__main__":
    try:
        sys.exit(main())
    finally:
        sh("docker", "rm", "-f", SIM, check=False)
