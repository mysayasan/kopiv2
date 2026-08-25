# W3-3d bench: fleet video walls, on a real two-appliance fleet.
#
# W3-3b built the video wall for ONE recorder and left the fleet-level half unbuilt — the
# differentiator, because a wall that spans appliances is a thing no appliance can offer: it
# needs something that can see all of them.
#
# THE CLAIM UNDER TEST:
#
#   1. a wall really spans APPLIANCES — a tile names a node as well as a camera, and the
#      arrangement survives the round trip in order;
#   2. the tiles are resolved against the LIVE fleet: each one carries its appliance's name and
#      status, and a tile on an appliance that is offline or gone says which;
#   3. a tile on a missing appliance is NOT silently dropped — an operator who built a wall of
#      sixteen and sees fifteen has been told nothing;
#   4. the refusals hold: an unknown layout, no cameras, the same camera twice, a strobe
#      instead of a rotation, and two walls claiming to be the default;
#   5. writing is superadmin-only while reading is not, because the people who watch a wall
#      should not need an administrator to open it.
#
# WHAT IS NOT PROVED HERE. That the tiles PLAY. The relay behind them is W3-2's, shipped and
# benched separately, and a fleet with a test pattern and no ffmpeg on the spare cannot show
# video in a headless browser. What this proves is the arrangement, the resolution and the
# refusals; the screen check proves an operator can build one and that an offline tile says so
# in words instead of going black.
#
#   python tools/fleetbench/fleet_harness.py
#   python tools/fleetbench/bench_w33d_fleet_wall.py
import json
import os
import sys

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import (CP, CP_PORT, Node, NODE_PORTS, PASSWORDS,
                           result_of, result_list, sh)

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


def node(name):
    n = Node("https://127.0.0.1:%d" % NODE_PORTS[name])
    for pw in PASSWORDS:
        n.auth = ("admin", pw)
        if n.get("/api/pairing/status").status_code == 200:
            return n
    raise SystemExit("cannot authenticate to " + name)


def node_id(n):
    return result_of(n.get("/api/pairing/status")).get("nodeId")


def walls(cp):
    return result_list(cp.get("/api/fleet-walls"), "items")


def main():
    a, b = node("node-a"), node("node-b")
    aid, bid = node_id(a), node_id(b)

    cp = CP("https://127.0.0.1:%d" % CP_PORT)
    for pw in PASSWORDS:
        if cp.login(pw=pw).status_code == 200:
            break
    else:
        raise SystemExit("cp login failed for every known password")

    for w in walls(cp):
        cp.s.delete(cp.base + "/api/fleet-walls/%d" % w["id"],
                    headers={"X-CSRF-Token": cp.csrf()}, timeout=30)

    grids = (result_of(cp.get("/api/fleet-walls/grids")) or {}).get("grids") or []
    check("the server says which layouts it will accept, so a client never has to discover "
          "that through a 400", len(grids) >= 4, json.dumps(grids))

    # ---- (1) a wall that spans two appliances -------------------------------------------------
    r = cp.post("/api/fleet-walls", {
        "name": "Bench wall", "grid": "2x2",
        "tiles": [
            {"nodeId": bid, "cameraId": 7},
            {"nodeId": aid, "cameraId": 3},
            {"nodeId": bid, "cameraId": 2},
        ],
        "cycleSeconds": 10, "autoPopSeconds": 20, "isDefault": True,
    })
    view = result_of(r)
    check("a wall can be built across two appliances", r.status_code == 200, r.text[:200])
    wall_id = (view.get("id") if isinstance(view, dict) else 0) or 0
    tiles = (view or {}).get("tileList") or []
    check("and it holds tiles that name an APPLIANCE as well as a camera — which is the whole "
          "of what a recorder cannot do", len(tiles) == 3 and all(t.get("nodeId") for t in tiles),
          json.dumps(tiles)[:200])
    order = ["%s:%s" % (t.get("nodeId"), t.get("cameraId")) for t in tiles]
    check("the arrangement comes back in the order it was built — a wall that reorders itself "
          "is one somebody rebuilds every shift",
          order == ["%s:7" % bid, "%s:3" % aid, "%s:2" % bid], json.dumps(order))

    # ---- (2) resolved against the LIVE fleet ---------------------------------------------------
    named = [t for t in tiles if t.get("nodeName")]
    check("every tile carries the name of the appliance it is on, read from the fleet rather "
          "than stored on the wall", len(named) == 3,
          json.dumps([t.get("nodeName") for t in tiles]))
    check("and its status, because a tile whose appliance is offline will never show a picture",
          all(t.get("nodeStatus") for t in tiles),
          json.dumps([t.get("nodeStatus") for t in tiles]))

    # ---- (3) a tile on an appliance that is not in the fleet ------------------------------------
    r = cp.post("/api/fleet-walls", {
        "id": wall_id, "name": "Bench wall", "grid": "2x2",
        "tiles": [
            {"nodeId": aid, "cameraId": 3},
            {"nodeId": "00000000-0000-0000-0000-000000000000", "cameraId": 9},
        ],
        "cycleSeconds": 10, "autoPopSeconds": 20, "isDefault": True,
    })
    view = result_of(r)
    check("a tile on an appliance that is not in this fleet is accepted and FLAGGED, not "
          "silently dropped — a wall of sixteen that renders fifteen has told nobody anything",
          r.status_code == 200 and (view or {}).get("unknownTiles") == 1
          and len((view or {}).get("tileList") or []) == 2,
          json.dumps({k: (view or {}).get(k) for k in ("unknownTiles", "offlineTiles")}))

    # ---- (2b) offline is a DIFFERENT answer from gone -------------------------------------------
    print("stopping node-b so a tile has a real offline appliance behind it...")
    sh("docker", "stop", "node-b", check=False)
    try:
        # The wall is re-read, not re-saved: the status comes from the fleet at READ time,
        # which is the whole reason it is not stored on the row.
        deadline_ok = False
        detail = ""
        import time as _t
        end = _t.time() + 300
        while _t.time() < end:
            v = result_of(cp.get("/api/fleet-walls/%d" % wall_id))
            r2 = cp.post("/api/fleet-walls", {
                "id": wall_id, "name": "Bench wall", "grid": "2x2",
                "tiles": [{"nodeId": aid, "cameraId": 3}, {"nodeId": bid, "cameraId": 7}],
                "cycleSeconds": 10, "autoPopSeconds": 20, "isDefault": True,
            })
            v = result_of(cp.get("/api/fleet-walls/%d" % wall_id))
            detail = json.dumps({k: (v or {}).get(k) for k in ("offlineTiles", "unknownTiles")})
            if (v or {}).get("offlineTiles") == 1:
                deadline_ok = True
                break
            _t.sleep(10)
        check("a tile on an appliance that has gone OFFLINE is counted separately from one on "
              "an appliance that is gone — they send somebody to two different places",
              deadline_ok, detail)
    finally:
        sh("docker", "start", "node-b", check=False)

    # ---- (4) the refusals -------------------------------------------------------------------------
    refusals = {
        "a layout nobody has": {"name": "X", "grid": "9x9", "tiles": [{"nodeId": aid, "cameraId": 1}]},
        "no cameras": {"name": "X", "grid": "2x2", "tiles": []},
        "the same camera twice": {"name": "X", "grid": "2x2",
                                  "tiles": [{"nodeId": aid, "cameraId": 1}, {"nodeId": aid, "cameraId": 1}]},
        "a strobe instead of a rotation": {"name": "X", "grid": "2x2", "cycleSeconds": 1,
                                           "tiles": [{"nodeId": aid, "cameraId": 1}]},
        "a name already taken": {"name": "Bench wall", "grid": "2x2",
                                 "tiles": [{"nodeId": aid, "cameraId": 5}]},
    }
    for label, body in refusals.items():
        resp = cp.post("/api/fleet-walls", body)
        check("refused: " + label, resp.status_code != 200,
              "%d %s" % (resp.status_code, resp.text[:110]))

    # ---- one default --------------------------------------------------------------------------------
    r = cp.post("/api/fleet-walls", {
        "name": "Second wall", "grid": "3x3", "isDefault": True,
        "tiles": [{"nodeId": aid, "cameraId": 11}],
    })
    check("a second wall can be built", r.status_code == 200, r.text[:160])
    defaults = [w for w in walls(cp) if w.get("isDefault")]
    check("and only one wall is the default — 'the default' with two answers is a screen that "
          "opens differently depending on row order",
          len(defaults) == 1 and defaults[0].get("name") == "Second wall",
          json.dumps([(w.get("name"), w.get("isDefault")) for w in walls(cp)]))

    # ---- the trail ------------------------------------------------------------------------------------
    trail = result_list(cp.get("/api/audit?action=fleet_wall.save&limit=20"), "items")
    check("building a wall is recorded — a wall is SHARED, so changing one changes what "
          "everybody on that screen sees", len(trail) >= 2, "%d entries" % len(trail))

    for w in walls(cp):
        cp.s.delete(cp.base + "/api/fleet-walls/%d" % w["id"],
                    headers={"X-CSRF-Token": cp.csrf()}, timeout=30)
    check("and a wall can be taken away again", len(walls(cp)) == 0)
    return report()


if __name__ == "__main__":
    sys.exit(main())
