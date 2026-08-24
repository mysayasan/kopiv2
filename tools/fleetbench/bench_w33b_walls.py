# W3-3b bench: video walls, against a real node.
#
# The claim under test is the one word that separates this from what Live View already did:
# a wall is SHARED and it OUTLIVES THE BROWSER. That decomposes into things a unit test
# cannot reach:
#
#   1. A wall survives the appliance restarting. The arrangement it replaces was a cookie;
#      if the wall does not come back byte-for-byte after a restart, nothing has been fixed.
#   2. A viewer can READ a wall and cannot change one. A viewer who cannot load the wall
#      sees a blank grid and no way to tell that from "no cameras", and an operator is the
#      role that arranges them.
#   3. A wall naming a deleted camera SAYS so, through the real camera-delete cascade rather
#      than through a doctored row.
#   4. Every refusal comes back as a sentence — including the grid list, which is the one
#      thing a client cannot guess.
#
# It does not need footage, so it runs on the plain node image and takes about a minute.
import json
import os
import sys
import time

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import Node, NODE_PORTS, ROOT, result_of, sh, PASSWORDS

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
        "username": "", "password": "", "description": "w3-3b bench camera",
    })
    cam = result_of(r)
    return cam.get("id") or cam.get("cameraId") or cam.get("result")


def walls(n):
    return result_of(n.get("/api/walls"))


def main():
    a = node("node-a")

    # Cameras that need never stream: a wall is an arrangement of ids, and this bench is
    # about the arrangement. (The tiles themselves are the screen check's problem.)
    cams = [add_camera(a, "Wall cam %d" % i, "wallcam%d" % i) for i in range(1, 5)]
    check("four cameras exist to arrange", all(cams), str(cams))
    if not all(cams):
        return report()

    # ---- refusals, and whether they say anything useful ---------------------------
    r = a.post("/api/walls", {"name": " ", "grid": "2x2", "cameraIds": cams[:1]})
    check("a nameless wall is refused", r.status_code != 200, r.text[:120])

    r = a.post("/api/walls", {"name": "Bad grid", "grid": "9x9", "cameraIds": cams[:1]})
    check("an unknown grid is refused, and the refusal NAMES the grids that exist",
          r.status_code != 200 and "3x3" in r.text, r.text[:200])

    r = a.post("/api/walls", {"name": "Empty", "grid": "2x2", "cameraIds": []})
    check("a wall with no cameras is refused", r.status_code != 200, r.text[:120])

    r = a.post("/api/walls", {"name": "Strobe", "grid": "2x2", "cameraIds": cams[:1], "cycleSeconds": 1})
    check("a one-second cycle is refused rather than quietly clamped",
          r.status_code != 200, r.text[:160])

    # ---- the wall ------------------------------------------------------------------
    made = result_of(a.post("/api/walls", {
        "name": "Perimeter", "grid": "3x2",
        # Deliberately not in id order: the order IS the arrangement.
        "cameraIds": [cams[2], cams[0], cams[3], cams[1]],
        "cycleSeconds": 5, "autoPopSeconds": 10, "isDefault": True,
    }))
    wall_id = made.get("id")
    check("a wall can be saved", bool(wall_id), json.dumps(made)[:200])
    if not wall_id:
        return report()
    check("the wall keeps the camera order it was given",
          made.get("cameraIds") == [cams[2], cams[0], cams[3], cams[1]],
          json.dumps(made.get("cameraIds")))
    check("the wall keeps its grid and behaviour",
          made.get("grid") == "3x2" and made.get("cycleSeconds") == 5
          and made.get("autoPopSeconds") == 10, json.dumps(made)[:200])
    check("missingCameras is an empty list, never null", made.get("missingCameras") == [],
          json.dumps(made.get("missingCameras")))

    listed = walls(a)
    check("the list ships the grids the server will accept",
          "3x3" in (listed.get("grids") or []), json.dumps(listed.get("grids")))

    dup = a.post("/api/walls", {"name": "perimeter", "grid": "2x2", "cameraIds": cams[:1]})
    check("a duplicate name is refused whatever its case",
          dup.status_code != 200 and "Perimeter" in dup.text, dup.text[:160])

    same = a.post("/api/walls/%d" % wall_id, {
        "name": "Perimeter", "grid": "2x2", "cameraIds": cams[:2],
    })
    check("a wall can be re-saved under its own name", same.status_code == 200, same.text[:160])

    # ---- one default ----------------------------------------------------------------
    second = result_of(a.post("/api/walls", {
        "name": "Loading bays", "grid": "2x2", "cameraIds": cams[2:], "isDefault": True,
    }))
    rows = walls(a).get("walls") or []
    defaults = [row["name"] for row in rows if row.get("isDefault")]
    check("only one wall is ever the default",
          defaults == ["Loading bays"], json.dumps(defaults))

    # ---- IT OUTLIVES THE BROWSER, WHICH IS THE WHOLE POINT ---------------------------
    before = json.dumps(sorted((w["name"], w["grid"], tuple(w["cameraIds"]), w["isDefault"])
                               for w in rows), default=str)
    sh("docker", "restart", "node-a")
    if not wait_node("node-a"):
        check("the node came back after a restart", False)
        return report()
    a = node("node-a")
    rows_after = walls(a).get("walls") or []
    after = json.dumps(sorted((w["name"], w["grid"], tuple(w["cameraIds"]), w["isDefault"])
                              for w in rows_after), default=str)
    check("every wall survives the appliance restarting, unchanged", before == after,
          "before=%s after=%s" % (before[:160], after[:160]))

    # ---- a deleted camera is REPORTED, through the real cascade ----------------------
    gone = cams[3]
    delete = a.delete("/api/cameras/%d" % gone)
    check("a camera on a wall can be deleted", delete.status_code == 200, delete.text[:160])
    wall = result_of(a.get("/api/walls/%d" % wall_id))
    perimeter = [w for w in (walls(a).get("walls") or []) if w["name"] == "Perimeter"]
    target = perimeter[0] if perimeter else wall
    # The Perimeter wall was re-saved above with cams[:2], so pick whichever wall still
    # names the deleted camera rather than assuming which one does.
    holder = next((w for w in (walls(a).get("walls") or []) if gone in (w.get("cameraIds") or [])), None)
    check("a wall still names the camera it was built with", holder is not None,
          json.dumps([w.get("cameraIds") for w in (walls(a).get("walls") or [])]))
    if holder:
        check("and reports it as gone rather than quietly showing one tile fewer",
              holder.get("missingCameras") == [gone], json.dumps(holder.get("missingCameras")))

    # ---- who may do what --------------------------------------------------------------
    roles = result_of(a.get("/api/settings/roles"))
    if isinstance(roles, dict):
        roles = roles.get("result") or roles.get("roles") or []
    role_ids = {(r.get("name") or "").lower(): r.get("id") for r in roles or []}
    check("the built-in roles are readable", bool(role_ids.get("viewer")), json.dumps(role_ids))
    if not role_ids.get("viewer"):
        return report()
    for uname, role in (("wall-op", "operator"), ("wall-view", "viewer")):
        a.post("/api/settings/users", {
            "username": uname, "password": "Bench-Passw0rd!", "displayName": uname,
            "roleId": role_ids[role], "isActive": True, "mustChangePassword": False,
        })
    op = node("node-a", ("wall-op", "Bench-Passw0rd!"))
    vw = node("node-a", ("wall-view", "Bench-Passw0rd!"))

    # A viewer must be able to LOAD the wall — it is what they are here to watch.
    r = vw.get("/api/walls")
    check("a viewer can read the walls", r.status_code == 200, str(r.status_code))
    r = vw.post("/api/walls", {"name": "Viewer wall", "grid": "2x2", "cameraIds": cams[:1]})
    check("a viewer cannot create a wall", r.status_code in (401, 403), str(r.status_code))
    r = vw.post("/api/walls/%d/delete" % wall_id)
    check("a viewer cannot delete a wall", r.status_code in (401, 403), str(r.status_code))

    r = op.post("/api/walls", {"name": "Operator wall", "grid": "2x2", "cameraIds": cams[:2]})
    check("an operator can create a wall", r.status_code == 200, r.text[:160])
    op_wall = result_of(r).get("id")
    if op_wall:
        r = op.post("/api/walls/%d/delete" % op_wall)
        check("an operator can tidy a wall away", r.status_code == 200, r.text[:160])
        check("and it is gone",
              all(w.get("id") != op_wall for w in (walls(a).get("walls") or [])))

    r = a.post("/api/walls/%d/delete" % 99999)
    check("deleting a wall that is not there is an error, not a silent success",
          r.status_code != 200, r.text[:120])

    io_path = os.path.join(ROOT, "w33b_context.json")
    with open(io_path, "w", encoding="utf-8") as fh:
        json.dump({"nodePort": NODE_PORTS["node-a"], "cameraIds": cams[:3],
                   "wallId": wall_id, "secondWallId": second.get("id")}, fh, indent=2)
    return report()


if __name__ == "__main__":
    sys.exit(main())
