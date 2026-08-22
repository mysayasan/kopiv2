# W2-4 bench: federated cross-node search, against a real two-node fleet.
#
# What this has to prove is not "the query works" — a unit test can say that. It is that a
# search which could NOT reach part of the fleet says so, because the failure mode this
# feature exists to prevent is an empty result set being read as "it never happened".
#
# Everything is asserted against the API, never against the log: an earlier bench in this
# programme produced a confident, wrong pass by grepping a log line that merely mentioned
# the thing it was looking for.
import io, json, os, sqlite3, sys, time
import urllib3, requests

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import CP, CP_PORT, NODE_PORTS, ROOT, result_of, sh, PASSWORDS

urllib3.disable_warnings()

CHECKS = []


def check(name, ok, detail=""):
    CHECKS.append((name, bool(ok), detail))
    print(("PASS  " if ok else "FAIL  ") + name + ("   " + detail if detail else ""))


def report():
    passed = sum(1 for _, ok, _ in CHECKS if ok)
    print("\n%d/%d checks passed" % (passed, len(CHECKS)))
    for name, ok, detail in CHECKS:
        if not ok:
            print("  FAILED: " + name + ("   " + detail if detail else ""))
    return 0 if passed == len(CHECKS) else 1


def now():
    return int(time.time())


def login():
    cp = CP("https://127.0.0.1:%d" % CP_PORT)
    for pw in PASSWORDS:
        if cp.login(pw=pw).status_code == 200:
            return cp
    raise SystemExit("cannot log in to the control plane")


def statuses(cp):
    r = cp.get("/api/nodes")
    return {n["name"]: n["status"] for n in (r.json().get("result") or [])}


def settle(cp, want_online=None, timeout=240):
    """Gate the bench on the fleet actually being WATCHED.

    Adoption sets a node's status to online by itself, so "online" alone proves nothing —
    W2-2's first run measured an outage that was already in progress because of exactly
    that. Require the expected set of nodes to hold online across three consecutive sweeps.
    """
    streak = 0
    deadline = time.time() + timeout
    while time.time() < deadline:
        st = statuses(cp)
        names = want_online if want_online is not None else list(st)
        if st and names and all(st.get(n) == "online" for n in names):
            streak += 1
            if streak >= 3:
                return True
        else:
            streak = 0
        time.sleep(10)
    return False


# --------------------------------------------------------------------------- seeding ---
#
# Detections need cameras, models and hours of video; what this bench is testing is the
# federation, so the sightings are seeded straight into each node's database instead.
#
# TWO RULES, both learned the hard way (see docs/FLAGSHIP_BENCH_CHECKLIST.md):
#   * Never read OR write a container's sqlite while the app is running. A seed written
#     under a running app is silently discarded on restart, and looks exactly like the
#     feature ignoring the data.
#   * Insert with an explicit, VERIFIED column list. A key that does not match a real
#     column is dropped in silence by the dict filter, and the row lands with that field
#     empty — which reads as a bug in whatever consumes it.

def seed(node, cameras, observations, alerts):
    sh("docker", "stop", node)
    path = os.path.join(ROOT, node, "mymatasan.db")
    conn = sqlite3.connect(path)
    try:
        def cols(table):
            names = [r[1] for r in conn.execute("PRAGMA table_info(%s)" % table)]
            if not names:
                raise SystemExit("table %s does not exist in %s" % (table, path))
            return set(names)

        def insert(table, rows):
            known = cols(table)
            for row in rows:
                missing = [k for k in row if k not in known]
                if missing:
                    raise SystemExit("%s: unknown columns %s (schema drift, not a typo to ignore)"
                                     % (table, missing))
                keys = list(row)
                conn.execute("INSERT INTO %s (%s) VALUES (%s)"
                             % (table, ",".join(keys), ",".join("?" * len(keys))),
                             [row[k] for k in keys])

        insert("camera", cameras)
        insert("object_observation", observations)
        insert("alert_event", alerts)
        conn.commit()
    finally:
        conn.close()
    sh("docker", "start", node)


def camera(cid, name):
    return {"id": cid, "name": name, "host": "10.0.0.%d" % cid, "port": 80, "is_active": 1,
            "created_at": now(), "updated_at": now()}


def observation(cid, label, at, conf=0.9, count=1):
    return {"camera_id": cid, "label": label, "started_at": at, "ended_at": at + 4,
            "max_confidence": conf, "max_count": count, "sample_count": 3,
            "peak_box": json.dumps({"x": 0.2, "y": 0.2, "w": 0.3, "h": 0.4}),
            "peak_at": at + 1, "attributes": "", "track_id": "", "segment_id": 0,
            "created_at": at}


def plate_alert(cid, at, plate, color, vehicle):
    return {"rule_id": 1, "camera_id": cid, "detection_type": "object",
            "label": "Plate %s (%s %s)" % (plate, color, vehicle), "confidence": 0.88,
            "zone_polygon": "", "bounding_box": json.dumps({"x": 0.3, "y": 0.3, "w": 0.2, "h": 0.1}),
            "snapshot_path": "", "metadata": json.dumps({"plate": plate, "color": color,
                                                         "vehicleType": vehicle, "watchlisted": False}),
            "is_diagnostic": 0, "is_acknowledged": 0, "created_at": at, "updated_at": at}


def face_alert(cid, at, person):
    return {"rule_id": 2, "camera_id": cid, "detection_type": "person",
            "label": "%s (94%%)" % person, "confidence": 0.94, "zone_polygon": "",
            "bounding_box": json.dumps({"x": 0.4, "y": 0.2, "w": 0.1, "h": 0.2}),
            "snapshot_path": "", "metadata": json.dumps({"personName": person, "personId": 7,
                                                         "matchConfidence": 0.94}),
            "is_diagnostic": 0, "is_acknowledged": 0, "created_at": at, "updated_at": at}


def noise_alert(cid, at, diagnostic):
    """An alert carrying NO identity: a plain object hit, and a sampling diagnostic. Both
    must stay out of identity results — the diagnostic rows are the bulk of the table."""
    return {"rule_id": 3, "camera_id": cid, "detection_type": "object", "label": "person",
            "confidence": 0.7, "zone_polygon": "", "bounding_box": "", "snapshot_path": "",
            "metadata": json.dumps({"sampled": True}), "is_diagnostic": 1 if diagnostic else 0,
            "is_acknowledged": 0, "created_at": at, "updated_at": at}


# ---------------------------------------------------------------------------- queries ---

def search(cp, **params):
    q = "&".join("%s=%s" % (k, requests.utils.quote(str(v))) for k, v in params.items() if v not in (None, ""))
    r = cp.get("/api/nodes/search" + ("?" + q if q else ""))
    if r.status_code != 200:
        raise SystemExit("search failed: %s %s" % (r.status_code, r.text[:400]))
    return result_of(r)


def labels(cp, **params):
    q = "&".join("%s=%s" % (k, params[k]) for k in params if params[k] not in (None, ""))
    r = cp.get("/api/nodes/search/labels" + ("?" + q if q else ""))
    if r.status_code != 200:
        raise SystemExit("labels failed: %s %s" % (r.status_code, r.text[:400]))
    return result_of(r)


def paged_result(r):
    """Unwrap a SendPagingResult body.

    It is DOUBLE-wrapped — {message, durationMs, data:{result, paging}} — where the plain
    SendResult used everywhere else in this bench returns {message, result}. Reading it with
    the single-level unwrapper yields an empty list, which is indistinguishable from a
    feature that recorded nothing; the first run of this bench reported exactly that about a
    perfectly working audit trail.
    """
    body = r.json()
    if isinstance(body, dict):
        if isinstance(body.get("data"), dict) and "result" in body["data"]:
            return body["data"]["result"] or []
        if "result" in body:
            return body["result"] or []
    return []


def cov_node(res, name):
    for n in res.get("coverage", {}).get("nodes", []):
        if n.get("nodeName") == name:
            return n
    return None


def nodes_in(res):
    return sorted({i["nodeName"] for i in res.get("items", [])})


def main():
    cp = login()
    if not settle(cp):
        check("the fleet is genuinely being watched before anything is searched", False, str(statuses(cp)))
        return report()
    check("the fleet is genuinely being watched before anything is searched", True, str(statuses(cp)))

    t = now()
    # Camera IDS COLLIDE ACROSS NODES on purpose: every recorder numbers its cameras from
    # 1, so a federated result identified by id alone is unreadable. If the node did not
    # join its own camera names, "camera 1" would appear twice meaning two different places.
    seed("node-a",
         [camera(1, "Front Gate"), camera(2, "Loading Bay")],
         [observation(1, "person", t - 600), observation(2, "car", t - 500),
          observation(1, "person", t - 400)],
         [plate_alert(1, t - 580, "WXY1234", "white", "car"),
          noise_alert(2, t - 560, diagnostic=False), noise_alert(2, t - 550, diagnostic=True)])
    seed("node-b",
         [camera(1, "North Barrier"), camera(2, "Warehouse Aisle")],
         [observation(1, "truck", t - 550), observation(2, "person", t - 450),
          observation(1, "dog", t - 350)],
         [face_alert(2, t - 520, "Alice")])
    if not settle(cp):
        check("both nodes are back on the control channel after seeding", False, str(statuses(cp)))
        return report()
    check("both nodes are back on the control channel after seeding", True)

    win = {"from": t - 3600, "to": t + 60}

    # --- 1. the fan-out itself --------------------------------------------------------
    res = search(cp, sources="objects", **win)
    check("one search returns sightings from BOTH nodes", nodes_in(res) == ["node-a", "node-b"],
          "nodes=%s items=%d" % (nodes_in(res), len(res.get("items", []))))
    check("every object sighting comes back", len(res.get("items", [])) == 6,
          "items=%d" % len(res.get("items", [])))
    times = [i["startedAt"] for i in res.get("items", [])]
    check("results are merged newest-first across nodes", times == sorted(times, reverse=True),
          str(times))
    check("the search reports itself complete when every node answered",
          res["coverage"]["complete"] and res["coverage"]["answered"] == 2,
          json.dumps(res["coverage"])[:200])

    # --- 2. results are readable across the fleet -------------------------------------
    by_cam = {(i["nodeName"], i["cameraId"]): i["cameraName"] for i in res["items"]}
    check("each node names its own cameras, so colliding ids stay distinguishable",
          by_cam.get(("node-a", 1)) == "Front Gate" and by_cam.get(("node-b", 1)) == "North Barrier",
          str(by_cam))

    # No recording ran in this bench, so EVERY sighting here is footage-less. The node's
    # own Objects grid hides those; a fleet search must not, or a detect-only camera — often
    # the only thing that saw the vehicle — silently answers "never seen here".
    check("footage-less sightings are returned rather than hidden",
          all(not i.get("segmentId") for i in res["items"]) and len(res["items"]) == 6)
    # ...and they must not promise a clip that is never coming. Every one of these cameras
    # is detect-only, so "footage is still being written" would be false forever — which is
    # exactly what the screen showed before a live UI check caught it.
    check("a sighting on a camera that records nothing does not claim footage is on its way",
          all(not i.get("footagePending") for i in res["items"]),
          str([i.get("footagePending") for i in res["items"]]))

    # --- 3. the query terms F-10 named ------------------------------------------------
    res = search(cp, sources="objects", labels="person", **win)
    check("the object filter narrows the fleet search",
          sorted(i["label"] for i in res["items"]) == ["person", "person", "person"],
          str([i["label"] for i in res["items"]]))

    # Sightings sit at t-600/-500/-400 on node-a and t-550/-450/-350 on node-b, so a
    # window opening at t-480 must keep exactly the three newest and drop the three older.
    res = search(cp, sources="objects", **{"from": t - 480, "to": t + 60})
    check("the time window narrows the fleet search",
          len(res["items"]) == 3 and all(i["startedAt"] >= t - 480 for i in res["items"]),
          str(sorted(i["startedAt"] - t for i in res["items"])))

    res = search(cp, sources="identities", text="WXY", **win)
    check("a partial plate is found, on the right node and camera",
          len(res["items"]) == 1 and res["items"][0]["identity"] == "WXY1234"
          and res["items"][0]["nodeName"] == "node-a" and res["items"][0]["cameraName"] == "Front Gate",
          json.dumps(res["items"])[:250])

    res = search(cp, sources="identities", text="alice", **win)
    check("a person's name is found case-insensitively, on the other node",
          len(res["items"]) == 1 and res["items"][0]["identity"] == "Alice"
          and res["items"][0]["identityKind"] == "face" and res["items"][0]["nodeName"] == "node-b",
          json.dumps(res["items"])[:250])

    res = search(cp, sources="identities", text="white car", **win)
    check("the descriptor an operator read off an alert finds the plate",
          len(res["items"]) == 1 and res["items"][0]["identity"] == "WXY1234")

    res = search(cp, sources="identities", **win)
    check("alerts carrying no identity are not identity hits",
          len(res["items"]) == 2 and sorted(i["identity"] for i in res["items"]) == ["Alice", "WXY1234"],
          json.dumps([i["label"] for i in res["items"]])[:250])

    res = search(cp, **win)
    check("objects and identities arrive interleaved in one list",
          len({i["kind"] for i in res["items"]}) == 2 and len(res["items"]) == 8,
          "kinds=%s items=%d" % ({i["kind"] for i in res["items"]}, len(res["items"])))

    lab = labels(cp)
    check("the label picker is the fleet-wide union",
          sorted(lab["labels"]) == ["car", "dog", "person", "truck"], str(lab["labels"]))
    check("the label list reports its own coverage", lab["coverage"]["complete"] is True)

    # --- 4. site scope ----------------------------------------------------------------
    r = cp.post("/api/sites", {"name": "North Depot", "kind": "building"})
    site_id = result_of(r).get("id")
    node_a_id = None
    for n in (cp.get("/api/nodes").json().get("result") or []):
        if n["name"] == "node-a":
            node_a_id = n["nodeId"]
    ok_assign = cp.s.put(cp.base + "/api/nodes/%s/building" % node_a_id,
                         json={"siteId": site_id}, headers={"X-CSRF-Token": cp.csrf()}, timeout=20)
    res = search(cp, sources="objects", siteId=site_id, **win)
    check("a site scope searches only that site's nodes",
          nodes_in(res) == ["node-a"] and all(i.get("siteName") == "North Depot" for i in res["items"]),
          "assign=%s nodes=%s" % (ok_assign.status_code, nodes_in(res)))
    check("the site scope's coverage counts only the nodes it searched",
          res["coverage"]["searched"] == 1, json.dumps(res["coverage"])[:200])

    # --- 5. the cap is declared, not hidden -------------------------------------------
    res = search(cp, sources="objects", limit=2, **win)
    check("a truncated result set says so", res.get("truncated") is True and len(res["items"]) == 2)
    check("a truncated result set is not reported as complete", res["coverage"]["complete"] is False)
    check("a truncated result set says how far back it IS complete",
          res["coverage"].get("completeThrough") == min(i["startedAt"] for i in res["items"]),
          "completeThrough=%s oldest kept=%s" % (res["coverage"].get("completeThrough"),
                                                 min(i["startedAt"] for i in res["items"])))

    # --- 6. THE ONE THAT MATTERS: a node that cannot be reached ------------------------
    print("\n-- stopping node-b --")
    sh("docker", "stop", "node-b")
    deadline = time.time() + 240
    gone = None
    while time.time() < deadline:
        probe = search(cp, sources="objects", **win)
        nb = cov_node(probe, "node-b")
        if nb and nb["status"] != "ok":
            gone = probe
            break
        time.sleep(6)
    if gone is None:
        check("a node that cannot be reached is reported, not omitted", False,
              "node-b kept answering after the container stopped")
        sh("docker", "start", "node-b")
        return report()

    nb = cov_node(gone, "node-b")
    check("a node that cannot be reached is reported, not omitted",
          nb is not None and nb["status"] in ("offline", "timeout", "error"),
          "status=%s reason=%s" % (nb["status"], nb.get("reason")))
    check("the unreachable node carries a reason an operator can act on", bool(nb.get("reason")),
          str(nb.get("reason")))
    check("a search missing a node does NOT report itself complete",
          gone["coverage"]["complete"] is False, json.dumps(gone["coverage"])[:200])
    check("the counts say how much of the fleet answered",
          gone["coverage"]["searched"] == 2 and gone["coverage"]["answered"] == 1,
          "searched=%s answered=%s" % (gone["coverage"]["searched"], gone["coverage"]["answered"]))
    check("the reachable node's sightings still come back",
          nodes_in(gone) == ["node-a"] and len(gone["items"]) == 3,
          "nodes=%s items=%d" % (nodes_in(gone), len(gone["items"])))

    # The plate is on node-a and still found; Alice is on node-b and is NOT — and the
    # coverage block is the only thing standing between that and "Alice was never here".
    away = search(cp, sources="identities", text="alice", **win)
    check("an identity on the unreachable node returns nothing — with coverage saying why",
          len(away["items"]) == 0 and away["coverage"]["complete"] is False
          and cov_node(away, "node-b")["status"] != "ok",
          json.dumps(away["coverage"])[:220])

    lab_away = labels(cp)
    check("the label picker drops the missing node's labels AND says it is incomplete",
          "truck" not in lab_away["labels"] and lab_away["coverage"]["complete"] is False,
          str(lab_away["labels"]))

    print("-- restarting node-b --")
    sh("docker", "start", "node-b")
    if not settle(cp, timeout=300):
        check("the fleet recovers and search is complete again", False, str(statuses(cp)))
        return report()
    back = search(cp, sources="objects", **win)
    check("the fleet recovers and search is complete again",
          back["coverage"]["complete"] is True and nodes_in(back) == ["node-a", "node-b"],
          json.dumps(back["coverage"])[:200])

    # --- 7. the audit trail -----------------------------------------------------------
    r = cp.get("/api/audit?action=fleet.search&limit=100")
    rows = paged_result(r)
    check("every fleet search is audited", r.status_code == 200 and len(rows) >= 10,
          "status=%s fleet.search rows=%d" % (r.status_code, len(rows)))
    searched_text = [x for x in rows if "WXY" in (x.get("detail") or "")]
    check("an estate-wide search for a plate is audited WITH the plate it searched for",
          len(searched_text) >= 1,
          str([x.get("detail") for x in rows[:6]]))
    partial = [x for x in rows if x.get("outcome") == "partial"]
    check("a search that could not reach the whole fleet is audited as partial, not success",
          len(partial) >= 1, "partial rows=%d of %d" % (len(partial), len(rows)))
    complete = [x for x in rows if x.get("outcome") == "success"]
    check("a search that DID reach the whole fleet is audited as a success",
          len(complete) >= 1, "success rows=%d of %d" % (len(complete), len(rows)))
    meta = json.loads(partial[0]["metadata"]) if partial else {}
    check("the audited coverage says how much of the fleet actually answered",
          meta.get("searched") == 2 and meta.get("answered") == 1 and meta.get("complete") is False,
          json.dumps(meta)[:200])

    return report()


if __name__ == "__main__":
    sys.exit(main())
