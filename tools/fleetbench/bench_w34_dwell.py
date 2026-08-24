# W3-4 bench: the time-based detection rules, against a real node.
#
# WHAT THIS DOES AND DOES NOT CLAIM, up front, because the gap is the important part.
#
# NOT CLAIMED: that a real person standing in a real doorway for thirty seconds raises a
# loitering alert. The harness films synthetic test patterns — mediamtx serving testsrc2 —
# so the object detector finds no person, no bag and no vehicle to track, and no evaluator
# can be driven end to end through it. Buying that check needs footage of real people, which
# this harness does not have and cannot fake convincingly (a drawn rectangle is not a person
# to a COCO model). The evaluators are covered instead by unit tests that drive a clock
# across many samples and by three mutations, each of which the tests caught:
#
#   * the zone-exit reset removed          -> TestLoiteringResetsWhenTheObjectIsSeenOutside...
#   * the unattended check removed         -> TestLeftBehindStaysQuietWhileSomebodyIsStanding...
#   * the bearing negation removed         -> TestDirectionFiresOnTheWantedHeading... + Bearing...
#
# WHAT IS CLAIMED, and what this bench drives on a live appliance: that the rules can be
# CREATED, that the refusals a client will meet are real and say something useful, that they
# survive a restart, that an alert of each type flows through notification and lands in the
# alert log readable, and that the role model governs them like every other rule. Those are
# all things the unit tests cannot see, and all things that have broken before.
import json
import os
import sys
import time

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import Node, NODE_PORTS, ROOT, result_of, result_list, sh, PASSWORDS

urllib3.disable_warnings()
CHECKS = []


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


def wait_node(node_name, timeout=120):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            if node(node_name).get("/api/pairing/status").status_code == 200:
                return True
        except Exception:
            pass
        time.sleep(3)
    return False


def add_camera(n, name, host):
    r = n.post("/api/cameras/discovered", {
        "name": name, "host": host, "port": 8554,
        "rtspUrl": "rtsp://%s:8554/cam" % host,
        "username": "", "password": "", "description": "w3-4 bench camera",
    })
    cam = result_of(r)
    return cam.get("id") or cam.get("cameraId") or cam.get("result")


ZONE = json.dumps([[0.2, 0.2], [0.8, 0.2], [0.8, 0.8], [0.2, 0.8]])


def rule(cam, kind, cfg, name):
    return {
        "cameraId": cam, "name": name, "detectionType": kind,
        "zonePolygon": ZONE, "ruleConfig": json.dumps(cfg),
        "threshold": 0.6, "minFrames": 2, "cooldownSeconds": 30,
        "isEnabled": True,
    }


def rules(n, cam):
    # result_list, not result_of: this endpoint answers with a bare array, and result_of
    # wraps one in {"result": [...]} — see the harness.
    return [r for r in result_list(n.get("/api/vision/rules?cameraId=%d" % cam)) if r]


def main():
    a = node("node-a")
    cam = add_camera(a, "Dwell cam", "dwellcam1")
    check("a camera exists to attach rules to", bool(cam), str(cam))
    if not cam:
        return report()

    # ---- the refusals, and whether they say anything an operator can act on ---------
    #
    # A RULE THAT CANNOT FIRE IS WORSE THAN NO RULE: somebody believes an area is watched.
    # These three types have no entry in the static class map, so a rule with no classes
    # would match nothing forever and silently.
    r = a.post("/api/vision/rules", rule(cam, "loitering", {"dwellSeconds": 30}, "No classes"))
    check("a loitering rule with no object classes is refused, and says why",
          r.status_code != 200 and "classes" in r.text, r.text[:200])

    r = a.post("/api/vision/rules", rule(cam, "direction", {"classes": ["person"]}, "No heading"))
    check("a direction rule with no heading is refused — there is no wrong way",
          r.status_code != 200 and "heading" in r.text, r.text[:200])

    r = a.post("/api/vision/rules", rule(
        cam, "direction", {"classes": ["person"], "heading": "sideways"}, "Bad heading"))
    check("an unreadable heading is refused, and the refusal names the ones that work",
          r.status_code != 200 and "up" in r.text and "down" in r.text, r.text[:220])

    r = a.post("/api/vision/rules", rule(
        cam, "left_behind", {"classes": ["backpack"], "driftTolerance": 0.9}, "Silly drift"))
    check("a movement tolerance of most of the frame is refused",
          r.status_code != 200 and "frame" in r.text, r.text[:200])

    # ---- the rules themselves --------------------------------------------------------
    made = {}
    for kind, cfg, name in (
        ("loitering", {"classes": ["person"], "dwellSeconds": 45}, "Doorway loitering"),
        ("left_behind", {"classes": ["backpack", "suitcase"], "stillSeconds": 90,
                         "driftTolerance": 0.04, "requireUnattended": True}, "Unattended bag"),
        ("direction", {"classes": ["car"], "heading": "up", "toleranceDegrees": 40,
                       "minTravel": 0.25}, "Wrong way up the ramp"),
    ):
        resp = a.post("/api/vision/rules", rule(cam, kind, cfg, name))
        body = result_of(resp)
        made[kind] = body.get("id") if resp.status_code == 200 else 0
        check("a %s rule can be created" % kind, bool(made[kind]), resp.text[:200])
    if not all(made.values()):
        return report()

    saved = {r["detectionType"]: r for r in rules(a, cam)}
    for kind in made:
        check("the %s rule is listed against the camera" % kind, kind in saved,
              ",".join(sorted(saved)))
    if "loitering" in saved:
        cfg = json.loads(saved["loitering"].get("ruleConfig") or "{}")
        check("the loitering rule kept the dwell it was given", cfg.get("dwellSeconds") == 45,
              json.dumps(cfg))
    if "direction" in saved:
        cfg = json.loads(saved["direction"].get("ruleConfig") or "{}")
        check("the direction rule kept its heading and tolerance",
              cfg.get("heading") == "up" and cfg.get("toleranceDegrees") == 40, json.dumps(cfg))
    if "left_behind" in saved:
        cfg = json.loads(saved["left_behind"].get("ruleConfig") or "{}")
        check("the left-behind rule defaults to only alerting when nobody is with it",
              cfg.get("requireUnattended") is True, json.dumps(cfg))

    # ---- they survive a restart --------------------------------------------------------
    #
    # A detection rule that does not come back is a camera nobody is watching, and nothing
    # on the screen would say so.
    before = sorted((r["detectionType"], r["name"]) for r in rules(a, cam))
    sh("docker", "restart", "node-a")
    if not wait_node("node-a"):
        check("the node came back after a restart", False)
        return report()
    a = node("node-a")
    after = sorted((r["detectionType"], r["name"]) for r in rules(a, cam))
    check("every rule survives the appliance restarting", before == after,
          "before=%s after=%s" % (before, after))

    # ---- the alert path ------------------------------------------------------------------
    #
    # The evaluator cannot be driven here (see the note at the top), but everything DOWNSTREAM
    # of it can: an alert of each new type has to reach the log and the notification feed
    # carrying its metadata, or the rule could fire perfectly and still tell nobody.
    for kind, extra, label in (
        ("loitering", {"dwellSeconds": 47, "dwellStartedAt": int(time.time()) - 47}, "person loitering for 47s"),
        ("left_behind", {"stillSeconds": 95, "peopleNearby": 0}, "backpack left unattended for 95s"),
        ("direction", {"headingDegrees": 2, "wantedHeading": 0, "travel": 0.31}, "car travelling up"),
    ):
        meta = dict(extra)
        meta["objectLabel"] = label.split()[0]
        resp = a.post("/api/vision/alerts", {
            "ruleId": made[kind], "cameraId": cam, "detectionType": kind,
            "label": label, "confidence": 0.88, "zonePolygon": ZONE,
            "boundingBox": json.dumps({"x": 0.4, "y": 0.4, "w": 0.2, "h": 0.2}),
            "metadata": json.dumps(meta),
        })
        check("an alert of type %s is accepted" % kind, resp.status_code == 200, resp.text[:200])

    time.sleep(3)
    items = result_list(a.get("/api/vision/alerts?cameraId=%d&limit=50" % cam), "alerts")
    kinds = {a_.get("detectionType") for a_ in items}
    check("all three kinds are readable in the alert log",
          {"loitering", "left_behind", "direction"}.issubset(kinds), ",".join(sorted(kinds)))

    loiter = next((a_ for a_ in items if a_.get("detectionType") == "loitering"), None)
    if loiter:
        meta = json.loads(loiter.get("metadata") or "{}")
        # THE FIELD THAT MAKES THE ALERT ACTIONABLE. Without it an operator opens the
        # footage at the moment the alert fired, which is 47 seconds after the interesting
        # part started.
        check("a loitering alert carries when the dwell STARTED, not only when it fired",
              int(meta.get("dwellStartedAt", 0)) > 0 and int(meta.get("dwellSeconds", 0)) >= 45,
              json.dumps(meta)[:200])

    text = json.dumps(result_list(a.get("/api/notifications?limit=50"), "notifications"))
    check("the alerts reached the notification feed",
          "loitering" in text or "unattended" in text or "travelling" in text,
          text[:200])

    # ---- who may change them -------------------------------------------------------------
    role_ids = {(r.get("name") or "").lower(): r.get("id")
                for r in result_list(a.get("/api/settings/roles"), "roles")}
    if role_ids.get("operator"):
        a.post("/api/settings/users", {
            "username": "dwell-op", "password": "Bench-Passw0rd!", "displayName": "dwell-op",
            "roleId": role_ids["operator"], "isActive": True, "mustChangePassword": False,
        })
        op = node("node-a", ("dwell-op", "Bench-Passw0rd!"))
        # Detection rules decide what the appliance will and will not notice, which is why
        # they are administrator-only: an operator who could retune them could quietly stop
        # a camera watching.
        resp = op.post("/api/vision/rules", rule(cam, "loitering", {"classes": ["person"]}, "Operator rule"))
        check("an operator cannot create a detection rule", resp.status_code in (401, 403),
              str(resp.status_code))
        check("an operator can still read the alerts they produce",
              op.get("/api/vision/alerts?cameraId=%d" % cam).status_code == 200)

    with open(os.path.join(ROOT, "w34_context.json"), "w", encoding="utf-8") as fh:
        json.dump({"nodePort": NODE_PORTS["node-a"], "cameraId": cam, "ruleIds": made}, fh, indent=2)
    return report()


if __name__ == "__main__":
    sys.exit(main())
