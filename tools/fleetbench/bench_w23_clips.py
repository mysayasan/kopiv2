# W2-3 bench: the critical-clip archive, against a real fleet with real footage.
#
# The claim under test is "the footage survives the appliance". So the bench does not
# stop at "a file appeared on the control plane" — it DESTROYS the node, wipes its data
# directory, and then plays the clip back from the fleet.
import hashlib, io, json, os, subprocess, sys, time
import urllib3, requests

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import fleet_harness
from fleet_harness import CP, Node, CP_PORT, NODE_PORTS, ROOT, result_of, sh, PASSWORDS

# This is the first bench that needs the nodes to RECORD, so the node containers must run
# an image with ffmpeg in it. Stand the fleet up with:
#
#   KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/fleet_harness.py
#
# Checked rather than assumed: a node without ffmpeg records nothing and does so quietly,
# and a clip bench against it measures an empty disk while every assertion fails for a
# reason that has nothing to do with the archive.
if getattr(fleet_harness, "NODE_IMAGE", "debian:bookworm-slim") == "debian:bookworm-slim":
    raise SystemExit(
        "the node containers are running an image with no ffmpeg, so they cannot record. "
        "Re-run the harness with KOPIV2_NODE_IMAGE=debian-ffmpeg:bench first.")

urllib3.disable_warnings()
CHECKS = []
SOURCE = "camsrc"


def check(name, ok, detail=""):
    CHECKS.append((name, bool(ok), detail))
    print(("PASS  " if ok else "FAIL  ") + name + ("   " + detail if detail else ""))


def login():
    cp = CP("https://127.0.0.1:%d" % CP_PORT)
    for pw in PASSWORDS:
        if cp.login(pw=pw).status_code == 200:
            return cp
    raise SystemExit("cannot log in to the control plane")


def node(name):
    n = Node("https://127.0.0.1:%d" % NODE_PORTS[name])
    for pw in PASSWORDS:
        n.auth = ("admin", pw)
        if n.get("/api/pairing/status").status_code == 200:
            return n
    raise SystemExit("cannot authenticate to " + name)


def start_source():
    """One mediamtx serving a generated RTSP stream, on the bench network."""
    sh("docker", "rm", "-f", SOURCE, check=False)
    sh("docker", "run", "-d", "--name", SOURCE, "--network", "benchnet",
       "--network-alias", "cam1host", "--entrypoint", "sh", "bench-rtsp:latest",
       "-c",
       "/opt/mediamtx /opt/mediamtx.yml & sleep 2; "
       # testsrc2, never a flat frame: the capture path perceptually dedups
       # near-identical frames and a flat grey stream collapses to nothing.
       "while true; do ffmpeg -re -f lavfi -i testsrc2=size=640x480:rate=15 "
       "-c:v libx264 -preset ultrafast -g 15 -f rtsp -rtsp_transport tcp "
       "rtsp://127.0.0.1:8554/cam1; sleep 1; done")
    time.sleep(8)


def wait(fn, timeout, every=3):
    deadline = time.time() + timeout
    while time.time() < deadline:
        v = fn()
        if v:
            return v
        time.sleep(every)
    return None


def clips(cp, **q):
    qs = "&".join("%s=%s" % (k, v) for k, v in q.items())
    r = cp.get("/api/clips" + ("?" + qs if qs else ""))
    if r.status_code != 200:
        raise SystemExit("clip list failed: %s %s" % (r.status_code, r.text[:300]))
    return result_of(r).get("items") or []


def main():
    cp = login()
    a = node("node-a")

    # --- set the node up: a camera, recording, and two rules -------------------------
    start_source()
    # The save body embeds onvif.Device (host/port/rtspUrl), not an ad-hoc shape, and the
    # handler decodes with DisallowUnknownFields — a wrong field name is a 400, not a
    # silently ignored key.
    r = a.post("/api/cameras/discovered", {
        "name": "Gate", "host": "cam1host", "port": 8554,
        "rtspUrl": "rtsp://cam1host:8554/cam1",
        "username": "", "password": "", "description": "bench camera",
    })
    cam = result_of(r)
    # The save returns the new camera's id as a bare number, not an object.
    camera_id = cam.get("id") or cam.get("cameraId") or cam.get("result")
    check("a camera is configured on the node", bool(camera_id), "%s %s" % (r.status_code, r.text[:160]))
    if not camera_id:
        return report()

    # THE FFMPEG TRAP. The path is captured into runtime_setting at FIRST boot and never
    # re-read from config, so a node whose image was built on Windows keeps a
    # "D:\...\ffmpeg.exe" that does not exist inside the container — and records
    # nothing, quietly. Read the whole settings object back, patch the one field, PUT it.
    runtime = result_of(a.get("/api/settings/runtime"))
    runtime.setdefault("decoder", {}).setdefault("mjpeg", {})["ffmpegPath"] = "/usr/bin/ffmpeg"
    rr = a.put("/api/settings/runtime", runtime)
    check("the node's ffmpeg path points at a binary that exists in the container",
          rr.status_code == 200 and result_of(a.get("/api/settings/runtime"))
          .get("decoder", {}).get("mjpeg", {}).get("ffmpegPath") == "/usr/bin/ffmpeg",
          "%s %s" % (rr.status_code, rr.text[:120]))

    # Field names matter and this handler does NOT reject unknown ones: "isEnabled" is
    # accepted, ignored, and leaves recording off — a 200 that did nothing.
    rc = a.put("/api/recording/config", {
        "cameraId": camera_id, "enabled": True, "segmentMinutes": 1,
        "retentionDays": 7, "preRollSec": 5, "postRollSec": 5,
    })
    check("recording is actually enabled on the camera",
          result_of(rc).get("config", {}).get("enabled") is True,
          rc.text[:160])

    segs = wait(lambda: (result_of(a.get("/api/recording/segments?cameraId=%d" % camera_id))
                         .get("items") or None), 180)
    check("the node is really recording", bool(segs), "%d segment(s)" % (len(segs or [])))
    if not segs:
        print(sh("docker", "logs", "--tail", "30", "node-a", check=False))
        return report()

    kept = result_of(a.post("/api/vision/rules", {
        "cameraId": camera_id, "name": "Perimeter gate", "detectionType": "presence",
        "ruleConfig": json.dumps({"classes": ["person"]}), "threshold": 0.5,
        "minFrames": 1, "cooldownSeconds": 0, "archiveClip": True, "isEnabled": True,
    }))
    ignored = result_of(a.post("/api/vision/rules", {
        "cameraId": camera_id, "name": "Daytime noise", "detectionType": "presence",
        "ruleConfig": json.dumps({"classes": ["person"]}), "threshold": 0.5,
        "minFrames": 1, "cooldownSeconds": 0, "archiveClip": False, "isEnabled": True,
    }))
    check("the archive flag round-trips through the node's rule API",
          kept.get("archiveClip") is True and ignored.get("archiveClip") is False,
          "kept=%s ignored=%s" % (kept.get("archiveClip"), ignored.get("archiveClip")))

    # --- 1. a flagged alert is archived; an unflagged one is not ---------------------
    kept_alert = result_of(a.post("/api/vision/alerts", {
        "ruleId": kept["id"], "cameraId": camera_id,
        "detectionType": "presence", "label": "person", "confidence": 0.9}))
    ignored_alert = result_of(a.post("/api/vision/alerts", {
        "ruleId": ignored["id"], "cameraId": camera_id,
        "detectionType": "presence", "label": "person", "confidence": 0.9}))
    kept_alert_id = kept_alert.get("id")
    ignored_alert_id = ignored_alert.get("id")
    print("   alerts raised: flagged=%s unflagged=%s" % (kept_alert_id, ignored_alert_id))

    stored = wait(lambda: next((c for c in clips(cp)
                                if c["state"] == "stored" and c["alertId"] == kept_alert_id), None), 300, 5)
    check("the flagged alert's clip is archived on the control plane", bool(stored),
          json.dumps(clips(cp))[:300])
    if not stored:
        print(sh("docker", "logs", "--tail", "30", "cp", check=False))
        return report()

    # Assert on IDENTITY, not on a count. The archive is cumulative, so a count only holds
    # on the first run against a fresh control plane and fails for the wrong reason on the
    # second — which is exactly what it did here before this was fixed. What must be true
    # is that the unflagged rule's own alert is not in there.
    allc = clips(cp)
    intruder = [c for c in allc if c["alertId"] == ignored_alert_id]
    check("the UNFLAGGED alert was not archived", not intruder,
          "archive holds %d clip(s); unflagged alert %s present: %s"
          % (len(allc), ignored_alert_id, bool(intruder)))
    check("every archived clip came from the flagged rule",
          all(c["ruleName"] == "Perimeter gate" for c in allc),
          str(sorted({c["ruleName"] for c in allc})))
    check("the archived clip records why it was kept",
          stored["ruleName"] == "Perimeter gate" and stored["nodeName"] == "node-a",
          "rule=%s node=%s" % (stored["ruleName"], stored["nodeName"]))
    check("the archived clip has real footage in it", stored["sizeBytes"] > 1000,
          "%d bytes, sha %s" % (stored["sizeBytes"], stored["sha256"][:12]))

    # --- 2. the bytes are the bytes --------------------------------------------------
    media = cp.s.get(cp.base + "/api/clips/%d/media" % stored["id"], timeout=120)
    got = hashlib.sha256(media.content).hexdigest()
    check("the control plane serves the clip back", media.status_code == 200 and len(media.content) > 1000,
          "%d bytes" % len(media.content))
    check("the served bytes match the digest taken as they arrived from the node",
          got == stored["sha256"], "served=%s stored=%s" % (got[:12], stored["sha256"][:12]))
    check("the archived file is a real MP4", media.content[4:8] == b"ftyp",
          repr(media.content[:12]))
    check("the digest is published on the response",
          media.headers.get("X-Clip-Sha256") == stored["sha256"])

    # An alert raised through the API carries no image, so there is no snapshot to
    # archive. What must be true is that nothing BOGUS was kept in its place — the first
    # run of this bench found the archive would have stored the node's JSON refusal as a
    # .jpg — and that the refusal names the snapshot rather than the footage, which is
    # perfectly fine.
    snap = cp.s.get(cp.base + "/api/clips/%d/snapshot" % stored["id"], timeout=60)
    body = snap.content[:400].decode("utf-8", "replace")
    check("no snapshot is invented when the alert had no image",
          snap.status_code == 400 and "no snapshot was archived" in body,
          "%d %s" % (snap.status_code, body[:120]))
    check("the missing snapshot does not implicate the footage",
          "footage itself is stored" in body, body[:140])

    # --- 3. authorisation -------------------------------------------------------------
    anon = requests.get("https://127.0.0.1:%d/api/clips" % CP_PORT, verify=False, timeout=20)
    check("an unauthenticated caller cannot read the archive", anon.status_code in (401, 403),
          "status=%d" % anon.status_code)

    # --- 4. THE OFFLINE CASE ----------------------------------------------------------
    # The plan called for "a retry queue for the offline case". Driving the fetch from
    # this side means there is no queue to build: the control plane simply does not know
    # about the alert yet, and learns of it through replay-on-reconnect. That is the half
    # that is easy to get wrong — hooking only the LIVE event path archives the easy clips
    # and silently skips every one raised while the link was down.
    print("\n-- stopping the control plane, then raising a flagged alert on the node --")
    sh("docker", "stop", "cp")
    time.sleep(3)
    offline_alert = result_of(a.post("/api/vision/alerts", {
        "ruleId": kept["id"], "cameraId": camera_id,
        "detectionType": "presence", "label": "person", "confidence": 0.9}))
    offline_id = offline_alert.get("id")
    check("the node raised an alert while the control plane was down", bool(offline_id),
          "alert %s" % offline_id)
    time.sleep(15)
    sh("docker", "start", "cp")
    time.sleep(12)
    cp = login()
    caught = wait(lambda: next((c for c in clips(cp)
                                if c["alertId"] == offline_id and c["state"] == "stored"), None), 300, 5)
    check("an alert raised while the control plane was DOWN is archived on reconnect",
          bool(caught), "alert %s -> %s" % (offline_id, [(c["alertId"], c["state"]) for c in clips(cp)]))

    # --- 5. THE CLAIM: the footage survives the appliance -----------------------------
    print("\n-- destroying node-a and wiping its data --")
    sh("docker", "rm", "-f", "node-a")
    data = os.path.join(ROOT, "node-a")
    wiped = 0
    for root, dirs, files in os.walk(data, topdown=False):
        for fn in files:
            try:
                os.remove(os.path.join(root, fn))
                wiped += 1
            except OSError:
                pass
    check("the appliance and its recordings are gone", wiped > 0, "%d file(s) destroyed" % wiped)

    media2 = cp.s.get(cp.base + "/api/clips/%d/media" % stored["id"], timeout=120)
    got2 = hashlib.sha256(media2.content).hexdigest()
    check("THE FOOTAGE SURVIVES THE APPLIANCE — the clip still plays from the fleet",
          media2.status_code == 200 and got2 == stored["sha256"],
          "%d bytes, sha %s" % (len(media2.content), got2[:12]))
    io.open(os.path.join(ROOT, "survived.mp4"), "wb").write(media2.content)

    # A clip whose node is gone must still name what it was: the record is snapshotted at
    # archive time precisely so an incident stays readable after the appliance does not.
    after = next((c for c in clips(cp) if c["id"] == stored["id"]), None)
    check("the archived record still names the node and camera that are gone",
          bool(after) and after["nodeName"] == "node-a" and after["cameraName"] == "Gate",
          json.dumps(after)[:200] if after else "missing")
    return report()


def report():
    print("\n" + "=" * 70)
    passed = sum(1 for _, ok, _ in CHECKS if ok)
    print("W2-3 bench: %d/%d" % (passed, len(CHECKS)))
    for name, ok, detail in CHECKS:
        if not ok:
            print("  FAILED: " + name + "   " + detail)
    io.open(os.path.join(ROOT, "bench-w23.json"), "w", encoding="utf-8").write(
        json.dumps([{"check": n, "pass": ok, "detail": d} for n, ok, d in CHECKS], indent=1))
    return 0 if passed == len(CHECKS) else 1


if __name__ == "__main__":
    sys.exit(main() or 0)
