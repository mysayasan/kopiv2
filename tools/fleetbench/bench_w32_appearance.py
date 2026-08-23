# W3-2 bench, part 2: appearance SEARCH on a real fleet.
#
# Part 1 (bench_w32_embedding.py) proves the model discriminates. This proves everything
# built on top of it works on a real appliance: storage, the relative ranking, the refusals,
# retention, and the two-hop federated search across a real mTLS control channel.
#
# THE DESCRIPTORS ARE SEEDED, NOT FILMED, and that is a stated limitation rather than a
# shortcut. The harness points synthetic test patterns at its cameras, so the detector finds
# no person or vehicle and the appearance stage correctly produces nothing. There is also
# deliberately no write route for descriptors. So this writes them the way the recorder
# would, straight into the node's sqlite while the app is stopped.
#
# WHAT IS THEREFORE NOT CLAIMED: that a camera watching a real person produces a stored
# descriptor end to end. Part 1 covers the embedding, the unit tests cover the recorder's
# hand-off, and the join between them is not exercised here.
import base64, json, math, os, sqlite3, struct, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import fleet_harness
from fleet_harness import CP, Node, CP_PORT, NODE_PORTS, ROOT, result_of, sh, PASSWORDS
import urllib3

urllib3.disable_warnings()
CHECKS = []
MODEL = "resnet18-hsv-560"
DIM = 560


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


def login_cp():
    cp = CP("https://127.0.0.1:%d" % CP_PORT)
    for pw in PASSWORDS:
        if cp.login(pw=pw).status_code == 200:
            return cp
    raise SystemExit("cannot log in to the control plane")


def unit_vector(seed, tilt):
    """A deterministic unit vector: mostly along one axis, tilted by `tilt` radians.

    Two vectors with the same seed and close tilts are near-identical; different seeds are
    orthogonal. That gives the scene a checkable right answer, which is the only way to tell
    a working ranking from one returning rows in an arbitrary order.
    """
    v = [0.0] * DIM
    a, b = seed % DIM, (seed + 1) % DIM
    v[a] = math.cos(tilt)
    v[b] = math.sin(tilt)
    return v


def encode_plain(vec):
    """float32 little-endian -> base64, matching encodeVectorAtRest with NO cipher."""
    return base64.b64encode(struct.pack("<%df" % len(vec), *vec)).decode()


def find_db(node_name):
    # The harness mounts the node's data dir at the top of its bench directory, not under
    # a data/ subfolder — look in both so this survives a harness layout change rather than
    # reporting "no sqlite file" for a fleet that is running perfectly.
    for d in (os.path.join(ROOT, node_name), os.path.join(ROOT, node_name, "data")):
        if not os.path.isdir(d):
            continue
        for f in sorted(os.listdir(d)):
            if f.endswith(".db"):
                return os.path.join(d, f)
    return ""


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


def seed_rows(node_name, rows):
    """Write appearance rows into the node's sqlite with the app STOPPED.

    Never write a container's sqlite while the app runs: the seed is discarded on restart
    (mid-WAL over a bind mount), and the failure looks exactly like the search ignoring the
    data rather than like a bad seed.
    """
    sh("docker", "stop", node_name)
    err = ""
    total = 0
    try:
        path = find_db(node_name)
        if not path:
            return 0, "no sqlite file under %s" % os.path.join(ROOT, node_name, "data")
        conn = sqlite3.connect(path)
        try:
            cols = [r[1] for r in conn.execute("PRAGMA table_info(object_appearance)")]
            if not cols:
                return 0, "object_appearance table missing (is the node running this build?)"
            # Start from an empty table. Seeding on top of a previous run mixes two
            # scenes that never coexist on a real site, and the calibration then describes
            # neither — the first time this happened the median came from a bimodal blend
            # and the true match was scored below the floor.
            conn.execute("DELETE FROM object_appearance")
            conn.execute("DELETE FROM object_observation")
            now = int(time.time())
            for r in rows:
                # The OBSERVATION as well as its descriptor. The recorder always writes
                # both, so seeding only the descriptor would leave the purge cascade
                # exercised through the camera-wide path alone and never through the
                # by-observation one — and it would leave the Objects screen with no rows
                # to open an appearance search from.
                conn.execute(
                    "INSERT INTO object_observation"
                    " (id, camera_id, label, started_at, ended_at, max_confidence, max_count,"
                    "  sample_count, peak_box, peak_at, attributes, track_id, segment_id, created_at)"
                    " VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
                    (r["observationId"], r["cameraId"], r["label"], r["seenAt"], r["seenAt"] + 8,
                     r["confidence"], 1, 4, "", r["seenAt"], "", "", 0, now))
                conn.execute(
                    "INSERT INTO object_appearance"
                    " (observation_id, camera_id, seen_at, label, vector, dim, model, confidence, created_at)"
                    " VALUES (?,?,?,?,?,?,?,?,?)",
                    (r["observationId"], r["cameraId"], r["seenAt"], r["label"],
                     r["vector"], r["dim"], r["model"], r["confidence"], now))
            conn.commit()
            total = conn.execute("SELECT COUNT(*) FROM object_appearance").fetchone()[0]
        finally:
            conn.close()
    except Exception as exc:
        err = "%s: %s" % (exc.__class__.__name__, exc)
    finally:
        sh("docker", "start", node_name)
        if not wait_node(node_name):
            err = err or ("%s did not come back after the seed" % node_name)
    return total, err


def build_scene():
    """The scene, with a right answer.

    Subject A is the query. Copies live on node-a camera 2 and — the point of federation —
    on node-b. Around them sits a CROWD of mutually-similar background sightings, because
    that crowd is what the relative score calibrates against: without it every candidate
    stands out and the ranking means nothing.
    """
    base = int(time.time()) - 3600
    a_rows, b_rows = [], []
    oid = 1
    # THE CROWD MUST BE HIGHLY SIMILAR TO THE QUERY, and getting this wrong the first time
    # is worth recording. The first version of this scene built the crowd on a different
    # axis, so every crowd vector was ORTHOGONAL to the query: similarity exactly 0, median
    # 0, spread 0, and the search correctly reported it could not calibrate. That scene
    # tested nothing, because it is the opposite of the real condition — on the real model
    # every crop of a person scores ~0.95 against every other one, and the entire point of
    # relative scoring is separating a match from a crowd that is already that close.
    #
    # So: same axis as the query, tilted ~0.6 rad (cosine ~0.8). Absolutely similar, and
    # clearly less similar than the true matches at ~0.999.
    for i in range(40):
        v = unit_vector(0, 0.60 + (i % 9) * 0.01)
        a_rows.append(dict(observationId=oid, cameraId=1, seenAt=base + i * 10, label="person",
                           vector=encode_plain(v), dim=DIM, model=MODEL, confidence=0.8))
        oid += 1
    for i in range(40):
        v = unit_vector(0, 0.60 + (i % 9) * 0.01)
        b_rows.append(dict(observationId=oid, cameraId=1, seenAt=base + i * 10, label="person",
                           vector=encode_plain(v), dim=DIM, model=MODEL, confidence=0.8))
        oid += 1

    # The query is the NEWEST sighting on node-a, so the Objects grid — which lists newest
    # first — puts it in the first row. The screen check opens that row, and without this
    # it opened whichever decoy happened to be latest and found nothing, which made every
    # assertion about the results list vacuously true.
    query_id = oid
    a_rows.append(dict(observationId=query_id, cameraId=1, seenAt=base + 900, label="person",
                       vector=encode_plain(unit_vector(0, 0.0)), dim=DIM, model=MODEL, confidence=0.95))
    oid += 1
    a_match = oid
    a_rows.append(dict(observationId=a_match, cameraId=2, seenAt=base + 560, label="person",
                       vector=encode_plain(unit_vector(0, 0.04)), dim=DIM, model=MODEL, confidence=0.9))
    oid += 1
    # A second match on camera 1, which the retention check does NOT purge. Without it the
    # screen check runs after the purge against a node whose only local match has just been
    # deleted, and reports an empty list as though the ranking were broken.
    a_match_kept = oid
    a_rows.append(dict(observationId=a_match_kept, cameraId=1, seenAt=base + 800, label="person",
                       vector=encode_plain(unit_vector(0, 0.045)), dim=DIM, model=MODEL, confidence=0.88))
    oid += 1
    b1, b2 = oid, oid + 1
    b_rows.append(dict(observationId=b1, cameraId=1, seenAt=base + 620, label="person",
                       vector=encode_plain(unit_vector(0, 0.05)), dim=DIM, model=MODEL, confidence=0.9))
    b_rows.append(dict(observationId=b2, cameraId=1, seenAt=base + 700, label="person",
                       vector=encode_plain(unit_vector(0, 0.06)), dim=DIM, model=MODEL, confidence=0.9))
    oid += 2

    # A CAR whose descriptor is IDENTICAL to the query's. Only the label scope keeps it out,
    # and a person and a car are both points in the same feature space — so this is the row
    # that catches a missing label filter.
    decoy_car = oid
    a_rows.append(dict(observationId=decoy_car, cameraId=1, seenAt=base + 640, label="car",
                       vector=encode_plain(unit_vector(0, 0.0)), dim=DIM, model=MODEL, confidence=0.9))
    oid += 1
    # A row from ANOTHER MODEL, also identical. Ranking across feature spaces is meaningless
    # rather than merely inaccurate, so this must never appear either.
    decoy_model = oid
    a_rows.append(dict(observationId=decoy_model, cameraId=1, seenAt=base + 660, label="person",
                       vector=encode_plain(unit_vector(0, 0.0)), dim=DIM, model="some-other-net-512",
                       confidence=0.9))
    oid += 1

    return dict(base=base, a=a_rows, b=b_rows, query=query_id, aMatch=a_match,
                aMatchKept=a_match_kept, bMatches=[b1, b2],
                decoyCar=decoy_car, decoyModel=decoy_model)


def setup_cameras(node_name, count=2):
    """Create cameras and switch recording + appearance on.

    Needed even though this bench seeds rows directly: the observation service hides
    sightings from cameras whose metadata recording is off, which is correct behaviour and
    means a seeded row is invisible to the Objects screen until a camera exists for it. It
    also exercises the per-camera appearance toggle through the real config API, which is
    the gate the whole compute cost hangs on.
    """
    n = node(node_name)
    made = []
    for i in range(count):
        r = n.post("/api/cameras/discovered", {
            "name": "w32-cam-%d" % (i + 1), "host": "10.255.255.%d" % (i + 1), "port": 8554,
            "rtspUrl": "rtsp://10.255.255.%d:8554/cam" % (i + 1),
            "username": "", "password": "", "description": "w3-2 bench camera",
        })
        cam = result_of(r)
        cid = cam.get("id") or cam.get("cameraId") or cam.get("result")
        if cid:
            made.append(int(cid))
    for cid in made:
        n.put("/api/recording/config", {
            "cameraId": cid, "enabled": True, "segmentMinutes": 5, "retentionDays": 7,
            "preRollSec": 5, "postRollSec": 5, "appearanceEnabled": True,
        })
    return n, made


def main():
    a, cams = setup_cameras("node-a")
    setup_cameras("node-b")
    check("two cameras exist on node-a for the seeded sightings to belong to",
          cams == [1, 2], "camera ids = %s" % cams)

    # The per-camera gate, round-tripped through the real config API. Appearance costs a
    # forward pass per person or vehicle in every sampled frame, so it must be a stored
    # choice rather than something recording turns on by implication.
    cfgs = result_of(a.get("/api/recording/config"))
    if isinstance(cfgs, dict):
        cfgs = cfgs.get("result") or cfgs.get("items") or []
    on = [c for c in (cfgs or []) if isinstance(c, dict) and c.get("appearanceEnabled")]
    check("the per-camera appearance toggle persists", len(on) >= 2,
          "configs with appearance on = %d" % len(on))

    scene = build_scene()

    n, err = seed_rows("node-a", scene["a"])
    check("node-a's appearance table accepted the seeded descriptors",
          not err and n == len(scene["a"]), err or "rows=%s" % n)
    if err:
        return report()
    n, err = seed_rows("node-b", scene["b"])
    check("node-b's appearance table accepted the seeded descriptors",
          not err and n == len(scene["b"]), err or "rows=%s" % n)
    if err:
        return report()
    a = node("node-a")

    frm, to = scene["base"] - 600, scene["base"] + 7200

    # ---- ranking on one node -----------------------------------------------------
    r = a.get("/api/observations/appearance?observationId=%d&from=%d&to=%d"
              % (scene["query"], frm, to))
    check("GET /api/observations/appearance answers", r.status_code == 200,
          "%s %s" % (r.status_code, r.text[:250]))
    if r.status_code != 200:
        return report()
    res = result_of(r)
    ids = [h["observationId"] for h in res.get("hits") or []]
    print("node-a hits: %s (scanned %s, median %s, spread %s, calibrated %s)"
          % (ids, res.get("scanned"), res.get("median"), res.get("spread"), res.get("calibrated")))

    # MEASURE THE SCENE BEFORE TRUSTING IT. A crowd that is not actually similar to the
    # query does not reproduce the condition this feature exists for, and every assertion
    # below would then pass for the wrong reason.
    check("the scene reproduces the real condition: the crowd is already very similar",
          0.5 < float(res.get("median") or 0) < 0.95,
          "median similarity across the crowd = %s" % res.get("median"))
    check("the search calibrated against the crowd", res.get("calibrated") is True,
          "scanned=%s spread=%s" % (res.get("scanned"), res.get("spread")))
    check("the real match is found and ranked first",
          bool(ids) and ids[0] == scene["aMatch"], "ids=%s want first=%s" % (ids, scene["aMatch"]))
    check("both local matches are found and nothing else is",
          sorted(ids) == sorted([scene["aMatch"], scene["aMatchKept"]]), "ids=%s" % ids)
    check("the crowd is NOT returned", len(ids) <= 3, "ids=%s" % ids)
    check("the query sighting does not match itself", scene["query"] not in ids, "ids=%s" % ids)
    check("a car with an identical descriptor is excluded by the label scope",
          scene["decoyCar"] not in ids, "ids=%s" % ids)
    check("a descriptor from another model is excluded from the ranking",
          scene["decoyModel"] not in ids, "ids=%s" % ids)
    hit = (res.get("hits") or [{}])[0]
    check("the match stands well clear of the crowd",
          float(hit.get("standout") or 0) >= 2.0, "standout=%s" % hit.get("standout"))
    check("the crowd's own similarity is high enough that an absolute floor could not have separated it",
          float(res.get("median") or 0) > 0.5 and float(hit.get("similarity") or 0) > 0.9,
          "crowd median = %s, match = %s" % (res.get("median"), hit.get("similarity")))

    # ---- the refusals ------------------------------------------------------------
    r = a.get("/api/observations/appearance?observationId=999999&from=%d&to=%d" % (frm, to))
    check("a sighting with no descriptor is refused with a reason, not answered with zero matches",
          r.status_code == 400 and "appearance" in r.text.lower(),
          "%s %s" % (r.status_code, r.text[:200]))
    r = a.get("/api/observations/appearance?from=%d&to=%d" % (frm, to))
    check("a search naming no sighting is refused", r.status_code == 400, r.text[:140])

    # ---- the vector hop federation needs -----------------------------------------
    r = a.get("/api/observations/appearance/vector?observationId=%d" % scene["query"])
    check("the query descriptor can be fetched for federation", r.status_code == 200,
          "%s %s" % (r.status_code, r.text[:160]))
    vp = result_of(r)
    check("the descriptor travels URL-safe and names its model",
          vp.get("model") == MODEL and vp.get("dim") == DIM
          and not any(c in (vp.get("vector") or "") for c in "+/="),
          "model=%s dim=%s" % (vp.get("model"), vp.get("dim")))

    # ---- federation ---------------------------------------------------------------
    cp = login_cp()
    raw_nodes = result_of(cp.get("/api/nodes"))
    # The node list comes back either as a bare array or wrapped in an items envelope
    # depending on the route; normalise rather than indexing blind, because iterating a
    # dict yields its KEYS and the failure then reads as "the fleet has no nodes".
    if isinstance(raw_nodes, dict):
        # fleet_harness.result_of re-wraps a bare ARRAY result as {"result": [...]}, so a
        # list endpoint arrives as a dict with one key. Unwrap that before anything else,
        # or iterating yields the string "result" and the fleet looks empty.
        raw_nodes = raw_nodes.get("result") or raw_nodes.get("items") or raw_nodes.get("nodes") or []
    nodes = [x for x in (raw_nodes or []) if isinstance(x, dict)]
    node_a_id = next((x["nodeId"] for x in nodes if x.get("name") == "node-a"), "")
    check("the control plane knows node-a", bool(node_a_id),
          "parsed %d node(s); raw=%s" % (len(nodes), json.dumps(raw_nodes)[:200]))
    if not node_a_id:
        return report()

    r = cp.get("/api/nodes/search/appearance?nodeId=%s&observationId=%d&from=%d&to=%d"
               % (node_a_id, scene["query"], frm, to))
    check("GET /api/nodes/search/appearance answers", r.status_code == 200,
          "%s %s" % (r.status_code, r.text[:300]))
    if r.status_code != 200:
        return report()
    fed = result_of(r)
    fed_pairs = [(h["nodeName"], h["observationId"]) for h in fed.get("items") or []]
    found = [oid for _, oid in fed_pairs]
    print("fleet hits: %s" % fed_pairs)
    print("coverage: %s" % json.dumps(fed.get("coverage")))

    # THE POINT OF FEDERATION. A search returning only the source node's own rows would
    # look like it worked — the top hit would still be right.
    check("the fleet search reaches the OTHER node",
          any(oid in found for oid in scene["bMatches"]),
          "hits=%s want any of %s" % (fed_pairs, scene["bMatches"]))
    check("every real match across both nodes is found",
          all(oid in found for oid in [scene["aMatch"], scene["aMatchKept"]] + scene["bMatches"]),
          "hits=%s" % fed_pairs)
    cov = fed.get("coverage") or {}
    check("the fleet result says which nodes answered",
          cov.get("answered") == 2 and cov.get("complete") is True, json.dumps(cov)[:250])
    check("the fleet result reports how much was compared",
          int(fed.get("scanned") or 0) >= 80, "scanned=%s" % fed.get("scanned"))
    check("the query sighting is excluded on its own node too",
          scene["query"] not in found, "hits=%s" % fed_pairs)
    check("the decoys stay excluded across the fleet",
          scene["decoyCar"] not in found and scene["decoyModel"] not in found, "hits=%s" % fed_pairs)

    # An unreachable node must be REPORTED as unreachable, never silently contribute nothing.
    sh("docker", "stop", "node-b")
    try:
        time.sleep(5)
        r = cp.get("/api/nodes/search/appearance?nodeId=%s&observationId=%d&from=%d&to=%d"
                   % (node_a_id, scene["query"], frm, to))
        down = result_of(r) if r.status_code == 200 else {}
        dcov = down.get("coverage") or {}
        check("a node that is down is named as not having answered",
              r.status_code == 200 and dcov.get("complete") is False and dcov.get("answered") == 1,
              "%s %s" % (r.status_code, json.dumps(dcov)[:300]))
        offline = [nc for nc in (dcov.get("nodes") or []) if nc.get("status") != "ok"]
        check("the unreachable node is named, with a reason",
              len(offline) == 1 and offline[0].get("nodeName") == "node-b" and bool(offline[0].get("reason")),
              json.dumps(offline)[:300])
        # A partial fleet is the normal state of a real estate, not an error.
        check("the reachable node's matches are still returned",
              any(h["observationId"] == scene["aMatch"] for h in down.get("items") or []),
              json.dumps([h["observationId"] for h in down.get("items") or []]))
    finally:
        sh("docker", "start", "node-b")
        wait_node("node-b")

    # ---- retention -----------------------------------------------------------------
    # A descriptor that outlives its sighting is a searchable record of somebody the
    # retention policy says has been forgotten.
    r = a.post("/api/recording/purge-camera", {"cameraId": 2})
    check("purging a camera succeeds", r.status_code == 200, "%s %s" % (r.status_code, r.text[:200]))
    # The count matters: it is what proves the purge reached the metadata rather than only
    # the footage. Before this cascade existed, "Purge now" destroyed a camera's video and
    # left a searchable index of everyone it had seen — with an appearance descriptor
    # hanging off each row.
    check("the purge reports the metadata it removed",
          int((result_of(r) or {}).get("observations") or 0) > 0,
          "purge result = %s" % r.text[:200])
    after = result_of(a.get("/api/observations/appearance?observationId=%d&from=%d&to=%d"
                            % (scene["query"], frm, to)))
    after_ids = [h["observationId"] for h in after.get("hits") or []]
    check("the purged camera's descriptors are gone from the ranking",
          scene["aMatch"] not in after_ids, "hits now %s" % after_ids)
    # And the untouched camera's are still there — a purge that took everything would pass
    # the check above while having destroyed the wrong data.
    check("the untouched camera's descriptors survive the purge",
          scene["aMatchKept"] in after_ids, "hits now %s" % after_ids)

    ctx = {
        "nodePort": NODE_PORTS["node-a"],
        "queryObservationId": scene["query"],
        "matchObservationId": scene["aMatch"],
        "keptMatchObservationId": scene["aMatchKept"],
        "from": frm,
        "to": to,
        "password": a.auth[1],
    }
    open(os.path.join(ROOT, "w32_context.json"), "w").write(json.dumps(ctx, indent=2))
    print("wrote %s for the screen check" % os.path.join(ROOT, "w32_context.json"))
    return report()


if __name__ == "__main__":
    sys.exit(main())
