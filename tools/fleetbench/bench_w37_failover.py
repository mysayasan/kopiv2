# W3-7 bench: N+1 node failover, on a real two-node fleet with a real camera.
#
# THE CLAIM UNDER TEST IS NOT "the API returns 200". It is:
#
#   1. a spare that has only been COPIED to is never reported as ready — only a drill that
#      actually opened the cameras can do that;
#   2. the control plane relays a camera set it CANNOT READ, and that bundle cannot be
#      staged onto any appliance but the one it was sealed for;
#   3. a staged camera is NOT a camera — nothing appears on the spare until a takeover;
#   4. after a takeover the spare is writing SEGMENTS THAT DECODE, not a config row that
#      says enabled;
#   5. the recorder that came back is NOT fenced, and the footage recorded during the
#      outage survives the fail-back.
#
# (4) is the one that matters most and the one every check that stops at the API would miss.
# A takeover writes a recording config, and a recording config starts nothing by itself; the
# bench downloads a segment the spare wrote and runs ffprobe over it, because "a file exists"
# passes on a zero-byte file and "the API says enabled" passes on a node with no ffmpeg.
#
# Needs the ffmpeg node image:
#   KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/fleet_harness.py
#   KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/bench_w37_failover.py
#
# Runtime is about fourteen minutes, most of it the hold-down: this feature is ABOUT waiting
# long enough, so the bench waits the real wait rather than one shortened to be convenient.
import base64
import json
import os
import subprocess
import sys
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import fleet_harness
from fleet_harness import (CP, CP_PORT, Node, NODE_PORTS, ROOT, PASSWORDS,
                           result_of, result_list, sh)

if getattr(fleet_harness, "NODE_IMAGE", "debian:bookworm-slim") == "debian:bookworm-slim":
    raise SystemExit(
        "the node containers are running an image with no ffmpeg, so the spare cannot record "
        "and the only assertion that matters here cannot be made. Re-run the harness with "
        "KOPIV2_NODE_IMAGE=debian-ffmpeg:bench first.")

urllib3.disable_warnings()
CHECKS = []

SRC = "fosrc-one"
SRC_ALIAS = "focam1"

# A password that exists only here, so "is this string anywhere in the bundle the control
# plane carried?" is a question with a meaningful answer.
SECRET_PW = "w37-camera-secret-8f21"
UNREACHABLE_HOST = "10.99.99.99"

# The real wait. Long enough to outlast the liveness grace window (three heartbeats, floor
# 90s), which is exactly what the service refuses to let an operator undercut.
HOLD_DOWN = 120


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


def login():
    cp = CP("https://127.0.0.1:%d" % CP_PORT)
    for pw in PASSWORDS:
        if cp.login(pw=pw).status_code == 200:
            return cp
    raise SystemExit("cannot log in to the control plane")


def cp_delete(cp, path):
    return cp.s.delete(cp.base + path, headers={"X-CSRF-Token": cp.csrf()}, timeout=30)


def wait(fn, timeout, every=3, label=""):
    deadline = time.time() + timeout
    while time.time() < deadline:
        v = fn()
        if v:
            return v
        time.sleep(every)
    if label:
        print("  (timed out waiting for %s after %ds)" % (label, timeout))
    return None


def start_source():
    sh("docker", "rm", "-f", SRC, check=False)
    sh("docker", "run", "-d", "--name", SRC, "--network", "benchnet",
       "--network-alias", SRC_ALIAS, "--entrypoint", "sh", "bench-rtsp:latest", "-c",
       "/opt/mediamtx /opt/mediamtx.yml & sleep 2; "
       "while true; do ffmpeg -re -f lavfi -i testsrc2=size=640x480:rate=15 "
       "-c:v libx264 -preset ultrafast -g 15 -f rtsp -rtsp_transport tcp "
       "rtsp://127.0.0.1:8554/cam1; sleep 1; done")


def patch_ffmpeg(n):
    """THE FFMPEG TRAP: the path is captured into runtime_setting at FIRST boot from the
    HOST config, so a container keeps a Windows path that does not exist in it and records
    nothing, quietly. Both nodes need this — the SPARE is the one that has to record."""
    runtime = result_of(n.get("/api/settings/runtime"))
    runtime.setdefault("decoder", {}).setdefault("mjpeg", {})["ffmpegPath"] = "/usr/bin/ffmpeg"
    n.put("/api/settings/runtime", runtime)
    return (result_of(n.get("/api/settings/runtime")).get("decoder", {})
            .get("mjpeg", {}).get("ffmpegPath") == "/usr/bin/ffmpeg")


def add_camera(n, name, host, port, rtsp, user="", pw=""):
    r = n.post("/api/cameras/discovered", {
        "name": name, "host": host, "port": port, "rtspUrl": rtsp,
        "username": user, "password": pw, "description": "w3-7 bench camera",
    })
    cam = result_of(r)
    return cam.get("id") or cam.get("cameraId") or cam.get("result")


def cameras(n):
    """THE ENVELOPE TRAP, and it cost this bench two checks on its first run.

    `/api/cameras` answers with SendPagingResult — {data: {result: [...]}} — not the
    {result: {...}} every other endpoint uses. Reading `result_of(...)["items"]` returns []
    for a node with cameras, which made "nothing appeared on the spare" pass for the wrong
    reason and "the camera exists on the spare" fail on a takeover that had worked
    perfectly. `result_list` is the harness helper that exists for exactly this."""
    return result_list(n.get("/api/cameras"), "items")


def segments(n, cam_id):
    r = n.get("/api/recording/segments?cameraId=%d" % cam_id)
    if r.status_code != 200:
        return []
    return result_list(r, "items")


def node_id_of(cp, name_hint):
    for nd in result_list(cp.get("/api/nodes"), "items"):
        if nd.get("name") == name_hint or nd.get("nodeId") == name_hint:
            return nd
    return None


def plans(cp):
    return result_list(cp.get("/api/failover-plans"), "items")


def plan_of(cp, plan_id):
    for v in plans(cp):
        if (v.get("plan") or {}).get("id") == plan_id:
            return v
    return {}


def notifications(cp):
    r = cp.get("/api/notifications?limit=100")
    if r.status_code != 200:
        return []
    body = result_of(r)
    return body.get("items") or (body if isinstance(body, list) else [])


def has_notification(cp, title):
    return any(title.lower() in (n.get("title") or "").lower() for n in notifications(cp))


def ffprobe_seconds(local_path):
    """Duration of a segment the SPARE wrote, measured rather than assumed. A file that
    exists, has a size and is named .mp4 still passes every check short of this one."""
    d = os.path.dirname(local_path).replace("\\", "/")
    out = subprocess.run(
        ["docker", "run", "--rm", "-v", "%s:/w" % d, "--entrypoint", "ffprobe",
         "bench-ffmpeg:latest", "-v", "error", "-show_entries", "format=duration",
         "-of", "default=nw=1:nk=1", "/w/" + os.path.basename(local_path)],
        capture_output=True, text=True)
    try:
        return float(out.stdout.strip())
    except ValueError:
        return 0.0


def main():
    cp = login()
    a = node("node-a")
    b = node("node-b")

    nodes = result_list(cp.get("/api/nodes"), "items")
    ids = {nd.get("name"): nd.get("nodeId") for nd in nodes}
    node_a_id = ids.get("node-a") or (nodes[0].get("nodeId") if nodes else None)
    node_b_id = ids.get("node-b") or (nodes[1].get("nodeId") if len(nodes) > 1 else None)
    check("the fleet has two adopted appliances", bool(node_a_id and node_b_id),
          "a=%s b=%s" % (node_a_id, node_b_id))
    if not (node_a_id and node_b_id):
        return report()

    # GATE THE WHOLE BENCH ON THE FLEET BEING GENUINELY WATCHED. A node that is "online"
    # merely because adoption said so cannot hold that across three sweeps, and a failover
    # bench run against a fleet that was already broken measures the breakage.
    def both_online():
        st = {nd.get("nodeId"): nd for nd in result_list(cp.get("/api/nodes"), "items")}
        return (st.get(node_a_id, {}).get("status") == "online"
                and st.get(node_b_id, {}).get("status") == "online"
                and st.get(node_a_id, {}).get("certExpiresAt"))
    holds = 0
    for _ in range(20):
        holds = holds + 1 if both_online() else 0
        if holds >= 3:
            break
        time.sleep(10)
    check("both appliances are genuinely connected (held online across 3 sweeps)", holds >= 3)
    if holds < 3:
        return report()

    check("node-a can record (ffmpeg path patched)", patch_ffmpeg(a))
    check("node-b can record (ffmpeg path patched)", patch_ffmpeg(b))

    # ---- a real camera on node-a, recording -------------------------------------
    start_source()
    time.sleep(8)
    cam = add_camera(a, "Lobby", SRC_ALIAS, 8554, "rtsp://%s:8554/cam1" % SRC_ALIAS)
    check("node-a has a camera", bool(cam), "id=%s" % cam)
    if not cam:
        return report()
    rc = a.put("/api/recording/config", {
        "cameraId": cam, "enabled": True, "segmentMinutes": 1,
        "retentionDays": 7, "preRollSec": 5, "postRollSec": 5,
    })
    check("node-a is recording that camera",
          result_of(rc).get("config", {}).get("enabled") is True, rc.text[:160])
    first = wait(lambda: segments(a, cam) or None, 240, label="node-a's first segment")
    check("node-a is writing segments before anything is failed over", bool(first))
    if not first:
        return report()

    # ---- the plan ----------------------------------------------------------------
    #
    # The hold-down refusal is tested FIRST, before any plan exists. Run after the good plan
    # was created it hit the already-protected check instead and passed for a reason that had
    # nothing to do with the hold-down — a green tick on an assertion that was never made.
    rhd = cp.post("/api/failover-plans", {
        "name": "too eager", "protectedNodeId": node_a_id, "standbyNodeId": node_b_id,
        "enabled": True, "autoActivate": False, "holdDownSeconds": 10,
    })
    check("refused: a hold-down shorter than the grace window",
          rhd.status_code != 200 and "hold-down" in rhd.text,
          "%s %s" % (rhd.status_code, rhd.text[:160]))

    r = cp.post("/api/failover-plans", {
        "name": "Lobby site", "protectedNodeId": node_a_id, "standbyNodeId": node_b_id,
        "enabled": True, "autoActivate": False, "holdDownSeconds": HOLD_DOWN,
    })
    check("a plan can be created", r.status_code == 200, "%s %s" % (r.status_code, r.text[:200]))
    if r.status_code != 200:
        return report()
    plan_id = (result_of(r).get("plan") or {}).get("id")

    # The refusals. Every one of these is a thing that would only be discovered during an
    # outage if it were allowed at save time.
    bad = {
        "an appliance standing by for itself": {"protectedNodeId": node_b_id, "standbyNodeId": node_b_id},
        "a second plan for an already-protected appliance": {"protectedNodeId": node_a_id, "standbyNodeId": node_b_id},
        "a chain (the spare given its own protector)": {"protectedNodeId": node_b_id, "standbyNodeId": node_a_id},
    }
    for label, over in bad.items():
        body = {"name": "bad", "enabled": True, "autoActivate": False, "holdDownSeconds": HOLD_DOWN}
        body.update(over)
        rr = cp.post("/api/failover-plans", body)
        check("refused: " + label, rr.status_code != 200, "%s %s" % (rr.status_code, rr.text[:140]))

    v = plan_of(cp, plan_id)
    check("a plan with nothing copied is NOT ready and says why",
          v.get("ready") is False and v.get("readyState") == "not-staged",
          "ready=%s state=%s" % (v.get("ready"), v.get("readyState")))
    ra = cp.post("/api/failover-plans/%d/activate" % plan_id)
    check("taking over is refused while nothing has been copied", ra.status_code != 200,
          "%s %s" % (ra.status_code, ra.text[:140]))

    # ---- staging -----------------------------------------------------------------
    before = cameras(b)
    rs = cp.post("/api/failover-plans/%d/stage" % plan_id)
    check("the camera set can be copied to the spare", rs.status_code == 200,
          "%s %s" % (rs.status_code, rs.text[:200]))
    v = plan_of(cp, plan_id)
    check("the plan records what was copied",
          (v.get("plan") or {}).get("cameraCount") == 1 and (v.get("plan") or {}).get("lastStagedAt"),
          json.dumps(v.get("plan"))[:200])

    # THE ASSERTION THE WHOLE FEATURE RESTS ON.
    check("a copy is NOT readiness: the plan still reports itself untested",
          v.get("ready") is False and v.get("readyState") == "untested",
          "ready=%s state=%s" % (v.get("ready"), v.get("readyState")))

    after = cameras(b)
    check("a staged camera is NOT a camera: nothing appeared on the spare",
          len(after) == len(before), "before=%d after=%d" % (len(before), len(after)))
    held = result_of(b.get("/api/standby"))
    sets = held.get("sets") or []
    check("the spare knows it is holding a set for node-a",
          len(sets) == 1 and sets[0].get("sourceNodeId") == node_a_id
          and sets[0].get("readiness") == "untested",
          json.dumps(held)[:220])

    # ---- the sealed bundle, opened by nobody --------------------------------------
    #
    # Add a second camera WITH a credential, re-copy, and then look inside the envelope the
    # control plane relays. A "sealed" handoff that shipped the password in the clear would
    # pass every other check on this page.
    cam2 = add_camera(a, "Back gate", UNREACHABLE_HOST, 8554,
                      "rtsp://%s:8554/nope" % UNREACHABLE_HOST, "w37user", SECRET_PW)
    check("node-a has a second camera, with a credential", bool(cam2), "id=%s" % cam2)
    key = result_of(b.get("/api/standby/handoff-key"))
    check("the spare publishes a one-exchange handoff key",
          bool(key.get("publicKey")) and key.get("nodeId") == node_b_id, json.dumps(key)[:160])
    rh = a.post("/api/standby/handoff", {"recipientNodeId": node_b_id, "publicKey": key.get("publicKey")})
    sealed = result_of(rh).get("sealed") or ""
    check("node-a will seal its camera set for the named spare", bool(sealed),
          "%s %s" % (rh.status_code, rh.text[:160]))
    raw = base64.b64decode(sealed) if sealed else b""
    check("the sealed bundle does not contain the camera password",
          bool(raw) and SECRET_PW.encode() not in raw, "%d bytes" % len(raw))
    check("the sealed bundle does not contain the camera address either",
          bool(raw) and UNREACHABLE_HOST.encode() not in raw)
    rself = a.post("/api/standby/stage", {"sealed": sealed})
    check("a bundle sealed for the spare cannot be staged onto anybody else",
          rself.status_code != 200, "%s %s" % (rself.status_code, rself.text[:160]))
    rown = a.post("/api/standby/handoff", {"recipientNodeId": node_a_id, "publicKey": key.get("publicKey")})
    check("an appliance refuses to stand by for itself", rown.status_code != 200,
          "%s %s" % (rown.status_code, rown.text[:140]))

    # ---- the drill ----------------------------------------------------------------
    check("the camera set can be re-copied after the site gained a camera",
          cp.post("/api/failover-plans/%d/stage" % plan_id).status_code == 200)
    rd = cp.post("/api/failover-plans/%d/drill" % plan_id)
    check("the spare can be asked to open the cameras", rd.status_code == 200,
          "%s %s" % (rd.status_code, rd.text[:200]))
    drilled = {c.get("name"): c.get("checkStatus") for c in (result_of(rd).get("cameras") or [])}
    check("the drill's own response carries the per-camera verdict",
          drilled.get("Lobby") == "ok" and drilled.get("Back gate") in ("unreachable", "unauthorized"),
          json.dumps(drilled)[:200])
    v = plan_of(cp, plan_id)
    p = v.get("plan") or {}
    check("the drill reports PARTIAL when one camera cannot be opened",
          v.get("ready") is False and v.get("readyState") == "partial"
          and p.get("drillReachable") == 1 and p.get("drillTotal") == 2,
          "state=%s %s/%s" % (v.get("readyState"), p.get("drillReachable"), p.get("drillTotal")))
    detail = result_of(cp.get("/api/failover-plans/%d" % plan_id))
    cams = {c.get("name"): c for c in (detail.get("cameras") or [])}
    check("the drill says WHICH camera failed, and how",
          cams.get("Lobby", {}).get("checkStatus") == "ok"
          and cams.get("Back gate", {}).get("checkStatus") in ("unreachable", "unauthorized"),
          json.dumps(cams)[:240])

    # Remove the camera that cannot be reached, re-copy, re-drill: now it must go green,
    # and only now.
    a.delete("/api/cameras/%d" % cam2)
    check("the camera set can be re-copied after the site LOST a camera",
          cp.post("/api/failover-plans/%d/stage" % plan_id).status_code == 200)
    check("re-drilling after the fix", cp.post("/api/failover-plans/%d/drill" % plan_id).status_code == 200)
    v = plan_of(cp, plan_id)
    check("a drilled, fully reachable plan is the only thing that reports READY",
          v.get("ready") is True and v.get("readyState") == "ready",
          "ready=%s state=%s" % (v.get("ready"), v.get("readyState")))

    # ---- kill the recorder --------------------------------------------------------
    print("\nstopping node-a and waiting out the %ds hold-down..." % HOLD_DOWN)
    sh("docker", "stop", "node-a")
    lost_at = time.time()

    got = wait(lambda: has_notification(cp, "Ready to fail over"), 420, every=10,
               label="the ready-to-fail-over alarm")
    check("the control plane raises an alarm once the hold-down expires", bool(got),
          "waited %ds" % int(time.time() - lost_at))
    v = plan_of(cp, plan_id)
    check("an unarmed plan does NOT take the cameras over by itself",
          (v.get("plan") or {}).get("state") != "active",
          "state=%s" % (v.get("plan") or {}).get("state"))

    # ---- the takeover -------------------------------------------------------------
    rt = cp.post("/api/failover-plans/%d/activate" % plan_id)
    check("the spare takes the cameras over", rt.status_code == 200,
          "%s %s" % (rt.status_code, rt.text[:240]))
    took = result_of(rt)
    outcomes = {c.get("name"): c.get("outcome") for c in (took.get("cameras") or [])}
    # THE DEFECT THIS BENCH FOUND. The appliance computes this while taking over and does
    # not store it — it is a result, not a state — so the control plane rebuilding the view
    # from its database afterwards dropped it, and an operator who had just pressed the
    # button in an emergency was told "active" and nothing about which cameras were actually
    # recording. Assert it on the CONTROL PLANE's response, because that is what a screen
    # reads. Every status code on that path was 200.
    check("the takeover reports what the RECORDER is doing, per camera",
          outcomes.get("Lobby") == "recording", json.dumps(outcomes)[:200])

    bcams = cameras(b)
    lobby = [c for c in bcams if c.get("name") == "Lobby"]
    check("the camera now exists on the spare — and only now", len(lobby) == 1,
          "%d camera(s) on the spare" % len(bcams))
    if not lobby:
        return report()
    bcam = lobby[0]["id"]

    # THE ONE THAT MATTERS. Not "a row says enabled" and not "a file exists": a segment the
    # spare wrote, downloaded, and decoded.
    print("waiting for the spare to actually write footage...")
    segs = wait(lambda: [s for s in segments(b, bcam) if (s.get("fileSize") or 0) > 0] or None,
                300, label="a segment on the spare")
    check("the spare is writing segments of its own", bool(segs),
          "%d segment(s)" % len(segs or []))
    if segs:
        out_dir = os.path.join(ROOT, "w37")
        os.makedirs(out_dir, exist_ok=True)
        dl = b.get("/api/recording/segments/%d/download" % segs[0]["id"])
        local = os.path.join(out_dir, "spare-segment.mp4")
        with open(local, "wb") as fh:
            fh.write(dl.content)
        secs = ffprobe_seconds(local)
        check("that footage DECODES — the takeover recorded video, not a filename",
              secs > 1.0, "%.2fs, %d bytes" % (secs, len(dl.content)))

    v = plan_of(cp, plan_id)
    check("the plan is carrying the cameras", (v.get("plan") or {}).get("state") == "active"
          and v.get("readyState") == "active")
    rdel = cp_delete(cp, "/api/failover-plans/%d" % plan_id)
    check("a plan carrying a building's cameras cannot be deleted", rdel.status_code != 200,
          "%s %s" % (rdel.status_code, rdel.text[:140]))

    # ---- the recorder comes back, and is NOT fenced --------------------------------
    print("\nbringing node-a back...")
    sh("docker", "start", "node-a")
    a = wait(lambda: _relogin("node-a"), 180, every=5, label="node-a coming back")
    check("node-a is serving again", bool(a))
    if not a:
        return report()

    before_a = len(segments(a, cam))
    got = wait(lambda: has_notification(cp, "Failed-over appliance is back"), 240, every=10,
               label="the appliance-is-back warning")
    check("the control plane warns that both may now be recording", bool(got))
    v = plan_of(cp, plan_id)
    check("the returned recorder is NOT handed its cameras back automatically",
          (v.get("plan") or {}).get("state") == "active",
          "state=%s" % (v.get("plan") or {}).get("state"))
    grew = wait(lambda: len(segments(a, cam)) > before_a or None, 240, every=10,
                label="node-a resuming its own recording")
    check("the returned recorder is NOT fenced: it is recording its own cameras again",
          bool(grew), "%d -> %d segments" % (before_a, len(segments(a, cam))))

    # ---- fail back -----------------------------------------------------------------
    spare_before = len(segments(b, bcam))
    rr = cp.post("/api/failover-plans/%d/release" % plan_id)
    check("the cameras can be handed back", rr.status_code == 200,
          "%s %s" % (rr.status_code, rr.text[:200]))
    v = plan_of(cp, plan_id)
    check("the plan is no longer carrying the cameras",
          (v.get("plan") or {}).get("state") == "released")
    # SAME ENVELOPE TRAP, second sighting in one bench: /api/recording/status answers with a
    # bare array, and result_of re-wraps a bare array as {"result": [...]} — so iterating it
    # yields the STRING "result" and the check dies on `'str' object has no attribute get`.
    # result_list is the unwrap, once.
    def recorder_stopped():
        rows = result_list(b.get("/api/recording/status"), "items")
        return not any(r.get("cameraId") == bcam and r.get("ffmpegRunning") for r in rows)
    stopped = wait(recorder_stopped, 90, every=5, label="the spare's recorder stopping")
    check("the spare has actually stopped recording", bool(stopped))
    after_release = segments(b, bcam)
    check("the footage the spare recorded during the outage SURVIVES the fail-back",
          len(after_release) >= spare_before and spare_before > 0,
          "%d before, %d after" % (spare_before, len(after_release)))
    check("the camera the takeover created is still there, so that footage is playable",
          any(c.get("id") == bcam for c in cameras(b)))

    # ---- deleting the plan cleans up the spare --------------------------------------
    rdel = cp_delete(cp, "/api/failover-plans/%d" % plan_id)
    check("a plan that is not carrying anything can be deleted", rdel.status_code == 200,
          "%s %s" % (rdel.status_code, rdel.text[:160]))
    check("the plan is gone", not any((v.get("plan") or {}).get("id") == plan_id for v in plans(cp)))
    still = cameras(b)
    check("deleting the plan did NOT delete the camera or its footage on the spare",
          any(c.get("id") == bcam for c in still) and len(segments(b, bcam)) > 0)

    return report()


def _relogin(name):
    n = Node("https://127.0.0.1:%d" % NODE_PORTS[name])
    for pw in PASSWORDS:
        n.auth = ("admin", pw)
        try:
            if n.get("/api/pairing/status").status_code == 200:
                return n
        except Exception:  # noqa: BLE001
            return None
    return None


if __name__ == "__main__":
    try:
        code = main()
    finally:
        sh("docker", "rm", "-f", SRC, check=False)
    sys.exit(code)
