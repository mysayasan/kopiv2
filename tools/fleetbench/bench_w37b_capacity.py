# W3-7b bench: failover capacity, against a REAL appliance's own estimate.
#
# W3-7 shipped with a stated gap: "no capacity admission control — a spare is not stopped from
# taking on more cameras than it can encode, and the drill measures reachability, not load."
# This is the bench for the half that was missing.
#
# THE CLAIM UNDER TEST:
#
#   1. the control plane asks the SPARE what it can carry and stores the appliance's own
#      number — nothing here models what a box can encode;
#   2. a plan whose cameras exceed that number is reported OVER, with the arithmetic on show
#      rather than only a verdict;
#   3. the answer is refreshed by staging AND by the drill — staging is when the camera count
#      changes, and the drill is the button an operator presses to find out whether this would
#      work, so it must answer both halves of that question;
#   4. it clears again when the cameras are taken away. A verdict that can only go red is half
#      a feature.
#
# WHAT IS NOT PROVED HERE, and why:
#
#   * THE READINESS COMPOSITION — a drill that passes outright on a spare that is over
#     capacity must read "over capacity" rather than "ready" — needs 55+ cameras that all
#     actually open, on a fleet whose only subject is a test pattern. Unit-tested and
#     mutation-checked in failover_capacity_test.go instead.
#   * A SPARE SHARED BY TWO PLANS. The harness has two appliances, and a second plan onto the
#     same spare needs a third protected recorder. `committedTo` is a pure function with its
#     own test; what this bench adds is that the numbers around it are real.
#
# The cameras created here point at an address nothing answers on. They never have to WORK:
# what is under test is the COUNT the spare is asked to carry. The drill will report them
# unreachable, which is correct and is not what is being measured.
#
#   python tools/fleetbench/fleet_harness.py
#   python tools/fleetbench/bench_w37b_capacity.py
import json
import os
import sys
import time

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import (CP, CP_PORT, Node, NODE_PORTS, PASSWORDS,
                           result_of, result_list)

urllib3.disable_warnings()
CHECKS = []

# Camera hosts that REFUSE INSTANTLY and are all DIFFERENT. Two things this bench learned the
# hard way:
#
#   * the appliance probes a camera before saving it, so an address that black-holes packets
#     costs a full connect timeout per camera — the first version pointed at an unrouted
#     address and took minutes to create ONE;
#   * saving a discovered camera UPSERTS BY HOST, so fifty cameras at one address are one
#     camera. The first run staged "1 wanted" against a fleet of five and every capacity
#     assertion after it failed for that reason rather than the one being tested.
#
# The whole 127/8 range is loopback on Linux, so 127.0.0.2, 127.0.0.3 … are distinct devices
# that all refuse a connection immediately.
def dead_host(i):
    return "127.0.0.%d" % (2 + (i % 250))


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


def node(name):
    n = Node("https://127.0.0.1:%d" % NODE_PORTS[name])
    for pw in PASSWORDS:
        n.auth = ("admin", pw)
        if n.get("/api/pairing/status").status_code == 200:
            return n
    raise SystemExit("cannot authenticate to " + name)


def node_id(n):
    st = result_of(n.get("/api/pairing/status"))
    return st.get("nodeId") or st.get("NodeId")


def add_camera(n, index, name):
    host = dead_host(index)
    r = n.post("/api/cameras/discovered", {
        "name": name, "host": host, "port": 8554,
        "rtspUrl": "rtsp://%s:8554/%s" % (host, name),
        "username": "", "password": "", "description": "w3-7b capacity bench",
    })
    out = result_of(r)
    # The create answers with a bare id, not an object.
    if isinstance(out, dict):
        return out.get("id") or out.get("cameraId")
    return out


def cameras(n):
    # THE ENVELOPE TRAP: /api/cameras answers {data:{result:[...]}}, not {result:{items:[...]}}.
    # Reading the wrong key returns [] for a node full of cameras — which is how W3-7's own
    # bench once made "nothing appeared on the spare" pass for the wrong reason.
    return result_list(n.get("/api/cameras"), "items")


def wipe_cameras(n):
    for cam in cameras(n):
        n.delete("/api/cameras/%d" % cam["id"])


def plans(cp):
    return result_list(cp.get("/api/failover-plans"), "items")


def main():
    a, b = node("node-a"), node("node-b")

    # ---- what the spare actually says about itself -------------------------------------------
    est = result_of(b.get("/api/capacity"))
    spare_max = int(est.get("estimatedMax") or 0)
    check("the spare can be asked what it can carry, and answers with a number",
          spare_max > 0,
          json.dumps({k: est.get(k) for k in
                      ("estimatedMax", "currentCameras", "confidence", "method")}))
    if spare_max <= 0:
        raise SystemExit("the spare gave no capacity estimate; nothing after this would mean anything")

    cp = CP("https://127.0.0.1:%d" % CP_PORT)
    for pw in PASSWORDS:
        if cp.login(pw=pw).status_code == 200:
            break
    else:
        raise SystemExit("cp login failed for every known password")

    # ---- a clean slate -------------------------------------------------------------------------
    for v in plans(cp):
        plan = v.get("plan") or {}
        if not plan.get("id"):
            continue
        if plan.get("state") == "active":
            cp.post("/api/failover-plans/%d/release" % plan["id"])
        cp.s.delete(cp.base + "/api/failover-plans/%d" % plan["id"],
                    headers={"X-CSRF-Token": cp.csrf()}, timeout=30)
    wipe_cameras(a)
    wipe_cameras(b)

    # ---- a plan the spare can comfortably carry -------------------------------------------------
    small = max(2, spare_max // 10)
    print("creating %d cameras on node-a (the spare estimates %d)..." % (small, spare_max))
    for i in range(small):
        add_camera(a, i, "small%02d" % i)

    r = cp.post("/api/failover-plans", {
        "name": "capacity bench", "protectedNodeId": node_id(a), "standbyNodeId": node_id(b),
        "enabled": True, "autoActivate": False, "holdDownSeconds": 300})
    check("a plan can be created", r.status_code == 200, r.text[:200])
    plan_id = (result_of(r).get("plan") or {}).get("id")
    if not plan_id:
        raise SystemExit("no plan to work with")

    cap = (result_of(cp.post("/api/failover-plans/%d/stage" % plan_id)) or {}).get("capacity") or {}
    check("STAGING asks the spare and stores what it said — the number is the appliance's "
          "own, not a model kept here",
          int(cap.get("estimatedMax") or 0) == spare_max, json.dumps(cap))
    check("a plan well within the spare's estimate is reported as fitting",
          cap.get("state") == "fits",
          "%s: %s wanted of about %d" % (cap.get("state"), cap.get("wanted"), spare_max))
    check("and the arithmetic is on show, not just the verdict",
          cap.get("wanted") == small and int(cap.get("headroom") or 0) == spare_max - small,
          json.dumps(cap))

    # ---- more cameras than the spare says it can carry --------------------------------------------
    over_by = 5
    need = spare_max + over_by - small
    print("creating %d more cameras, to put the plan past the spare's estimate..." % need)
    for i in range(need):
        add_camera(a, small + i, "big%03d" % i)

    cap = (result_of(cp.post("/api/failover-plans/%d/stage" % plan_id)) or {}).get("capacity") or {}
    check("a plan asking for more than the spare says it can carry is reported OVER",
          cap.get("state") == "over",
          "%s: %s wanted of about %d" % (cap.get("state"), cap.get("wanted"), spare_max))
    check("with the shortfall stated as a number somebody can act on",
          int(cap.get("headroom") or 0) == -over_by,
          "headroom=%s, want %d" % (cap.get("headroom"), -over_by))

    # ---- the DRILL asks too -----------------------------------------------------------------------
    before = int(cap.get("checkedAt") or 0)
    time.sleep(1.5)
    drilled = result_of(cp.post("/api/failover-plans/%d/drill" % plan_id)) or {}
    cap = drilled.get("capacity") or {}
    check("the DRILL asks about capacity as well as reachability — one button, both halves of "
          "the question it is being asked",
          int(cap.get("checkedAt") or 0) > before,
          "checkedAt %d -> %s" % (before, cap.get("checkedAt")))
    check("and it still says over", cap.get("state") == "over", json.dumps(cap))
    # The drill genuinely failed (these cameras answer nowhere), which is correct and is a
    # different fact from the capacity verdict. Asserting it here keeps the two apart.
    check("the drill's own verdict is about REACHABILITY and is separately false here",
          drilled.get("ready") is not True, "readyState=%s" % drilled.get("readyState"))

    # ---- and it clears ----------------------------------------------------------------------------
    print("taking the extra cameras away again...")
    keep = set()
    for cam in cameras(a):
        if str(cam.get("name", "")).startswith("small"):
            keep.add(cam["id"])
    for cam in cameras(a):
        if cam["id"] not in keep:
            a.delete("/api/cameras/%d" % cam["id"])

    cap = (result_of(cp.post("/api/failover-plans/%d/stage" % plan_id)) or {}).get("capacity") or {}
    check("removing the cameras clears the verdict — it tracks the fleet rather than latching",
          cap.get("state") == "fits",
          "%s: %s wanted of about %d" % (cap.get("state"), cap.get("wanted"), spare_max))

    # ---- leave the fleet as it was found ------------------------------------------------------------
    cp.s.delete(cp.base + "/api/failover-plans/%d" % plan_id,
                headers={"X-CSRF-Token": cp.csrf()}, timeout=30)
    wipe_cameras(a)
    return report()


if __name__ == "__main__":
    sys.exit(main())
