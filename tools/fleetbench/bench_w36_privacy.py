# W3-6 bench: privacy zones — camera-side masks, and redaction on export.
#
# THE CLAIM UNDER TEST IS A CLAIM ABOUT HONESTY, not about drawing rectangles. One region
# feeds two mechanisms that protect different things:
#
#   - THE CAMERA burns it in, and then the pixels are never recorded at all.
#   - THE EXPORT redacts it regardless, and then the recording still holds the pixels but a
#     copy handed outside the building does not.
#
# Which one an operator has depends on their hardware, and the product's job is to say which
# — never to imply the stronger claim. So the interesting cases are the DISHONEST cameras,
# and `onvifsim.py` can be told to be one on demand: to store a mask in a different
# coordinate space, to reduce a polygon to a rectangle, or to accept a mask with HTTP 200
# and store nothing at all.
#
# It also checks the half that does not depend on the camera: a redacted export declares
# itself a derivative, in the manifest AND in VERIFY.txt, and still carries the source
# digests — because redaction and the export's integrity promise are in direct tension and
# the answer is to state it, not to hide it.
#
# Runs on the plain node image. No footage is needed for the mask half; the redaction half
# is skipped with a stated reason when the node has no recordings.
import json
import os
import subprocess
import sys
import time
import zipfile

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import fleet_harness
from fleet_harness import Node, NODE_PORTS, ROOT, result_of, result_list, sh, PASSWORDS

urllib3.disable_warnings()
CHECKS = []

SIM = "onvifsim"
SIM_PORT = 8080
SIM_HOST_PORT = 18480
SIM_URL = "http://127.0.0.1:%d" % SIM_HOST_PORT
SIM_XADDR = "http://%s:%d/onvif/device_service" % (SIM, SIM_PORT)

SQUARE = [[0.1, 0.1], [0.4, 0.1], [0.4, 0.4], [0.1, 0.4]]


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
    last = None
    for _ in range(5):
        try:
            return requests.get(SIM_URL + "/journal", timeout=10).json()
        except Exception as err:  # noqa: BLE001
            last = err
            time.sleep(2)
    raise last


def mask_mode(mode):
    requests.post(SIM_URL + "/masks/mode/" + mode, timeout=5)


def mask_support(on):
    requests.post(SIM_URL + "/masks/support/" + ("on" if on else "off"), timeout=5)


def mask_limit(n):
    requests.post(SIM_URL + "/masks/limit/%d" % n, timeout=5)


def add_camera(n):
    r = n.post("/api/cameras/discovered", {
        "name": "Yard camera", "host": SIM, "port": SIM_PORT,
        "xAddr": SIM_XADDR,
        "mediaXAddr": "http://%s:%d/onvif/media_service" % (SIM, SIM_PORT),
        "profileToken": "MainProfile",
        "rtspUrl": "rtsp://ptzcam:8554/cam",
        "username": "", "password": "", "description": "w3-6 bench camera",
    })
    cam = result_of(r)
    for key in ("id", "cameraId", "result"):
        value = cam.get(key)
        if isinstance(value, (int, float)) and value:
            return int(value), r
    return None, r


def start_source(container, alias, path):
    """A real RTSP camera, so the redaction half has footage to redact."""
    sh("docker", "rm", "-f", container, check=False)
    sh("docker", "run", "-d", "--name", container, "--network", "benchnet",
       "--network-alias", alias, "--entrypoint", "sh", "bench-rtsp:latest", "-c",
       "/opt/mediamtx /opt/mediamtx.yml & sleep 2; "
       "while true; do ffmpeg -re -f lavfi -i testsrc2=size=640x480:rate=15 "
       "-c:v libx264 -preset ultrafast -g 15 -f rtsp -rtsp_transport tcp "
       "rtsp://127.0.0.1:8554/%s; sleep 1; done" % path)


def add_rtsp_camera(n, name, alias, path):
    r = n.post("/api/cameras/discovered", {
        "name": name, "host": alias, "port": 8554,
        "rtspUrl": "rtsp://%s:8554/%s" % (alias, path),
        "username": "", "password": "", "description": "w3-6 bench recording camera",
    })
    cam = result_of(r)
    for key in ("id", "cameraId", "result"):
        value = cam.get(key)
        if isinstance(value, (int, float)) and value:
            return int(value)
    return None


def segments(n, cam):
    rows = result_list(n.get("/api/recording/segments?limit=50&cameraId=%d" % cam), "segments", "items")
    return [r for r in rows if r.get("cameraId") == cam and r.get("endedAt")]


def wait(fn, timeout, every=3):
    deadline = time.time() + timeout
    while time.time() < deadline:
        v = fn()
        if v:
            return v
        time.sleep(every)
    return None


def privacy(n, cam):
    body = result_of(n.get("/api/cameras/%d/privacy" % cam))
    return body.get("zones") or [], body.get("status") or {}


def region_luma(mp4_dir, name, crop):
    """Average brightness of one region of the first frame, via ffmpeg signalstats.

    MEASURED, not guessed from a file size. The first version of this check compared the
    size of a cropped PNG against a threshold and PASSED ON UNREDACTED FOOTAGE, because the
    part of the test pattern it cropped happens to be a flat colour band that compresses to
    almost nothing. A check that passes when the feature is broken is worse than no check:
    it was the only green tick on a run whose manifest said, correctly, that nothing had
    been redacted.
    """
    # subprocess directly, not the harness `sh`: ffmpeg's metadata=print writes to
    # STDERR, and sh() returns stdout only — so the reading would always come back None
    # and the check would always fail for a reason that has nothing to do with the video.
    proc = subprocess.run(
        ["docker", "run", "--rm", "-v", mp4_dir + ":/in", "--entrypoint", "ffmpeg",
         "bench-rtsp:latest", "-hide_banner", "-i", "/in/" + name, "-frames:v", "1",
         "-vf", "crop=%s,signalstats,metadata=print:key=lavfi.signalstats.YAVG" % crop,
         "-f", "null", "-"],
        capture_output=True, text=True)
    for line in (proc.stdout + proc.stderr).splitlines():
        if "YAVG" in line:
            try:
                return float(line.rsplit("=", 1)[-1].strip())
            except ValueError:
                return None
    return None


def main():
    if not start_sim():
        check("the simulated ONVIF camera came up", False)
        return report()
    check("the simulated ONVIF camera came up", True)
    mask_mode("honest")
    mask_support(True)
    mask_limit(4)

    a = node("node-a")
    cam, saved = add_camera(a)
    check("an ONVIF camera can be saved", bool(cam), saved.text[:200])
    if not cam:
        return report()

    # ---- an honest camera: the strong claim, and it is EARNED --------------------------
    r = a.post("/api/cameras/%d/privacy" % cam, {
        "name": "Neighbour window", "points": SQUARE, "style": "color", "enabled": True})
    check("a privacy zone can be drawn", r.status_code == 200, r.text[:200])

    dev = device()
    check("the zone was pushed to the CAMERA as a real mask",
          len(dev.get("masks") or {}) == 1, json.dumps(dev.get("masks"))[:240])

    zones, status = privacy(a, cam)
    check("and the camera-side masking is reported as CONFIRMED",
          status.get("masking") == "confirmed", json.dumps(status)[:240])
    check("the confirmed wording says the area is not recorded at all",
          "not recorded" in (status.get("detail") or ""), json.dumps(status.get("detail")))
    check("the zone says the camera is holding it",
          zones and zones[0].get("maskToken"), json.dumps(zones)[:200])

    # ---- THE POINT OF THE WHOLE FEATURE: a camera that lies ---------------------------
    #
    # It accepts the mask with HTTP 200 and stores a different coordinate space. Every layer
    # reports success. Only reading it back catches it — and a privacy mask believed to be
    # applied and not applied is worse than no mask, because somebody relies on it.
    mask_mode("shifted")
    r = a.post("/api/cameras/%d/privacy/apply" % cam, {})
    check("the camera can be re-checked", r.status_code == 200, r.text[:200])
    _, status = privacy(a, cam)
    check("a camera that stored a DIFFERENT shape is NOT reported as confirmed",
          status.get("masking") == "unconfirmed", json.dumps(status)[:240])
    check("and the operator is told which zone is not protected",
          "Neighbour window" in (status.get("detail") or ""), json.dumps(status.get("detail")))
    check("and told to treat the recording as containing it",
          "recording" in (status.get("detail") or ""), json.dumps(status.get("detail")))
    check("while the export protection is still promised",
          status.get("exportRedaction") is True, json.dumps(status)[:200])

    # A camera that accepts the write and stores NOTHING is the loudest silent failure.
    mask_mode("drop")
    a.post("/api/cameras/%d/privacy/apply" % cam, {})
    _, status = privacy(a, cam)
    check("a camera that stored NOTHING is not reported as confirmed either",
          status.get("masking") == "unconfirmed", json.dumps(status)[:240])

    # ---- a camera that cannot mask at all ----------------------------------------------
    mask_support(False)
    a.post("/api/cameras/%d/privacy/apply" % cam, {})
    _, status = privacy(a, cam)
    check("a camera with no mask support says so, rather than erroring",
          status.get("masking") == "unsupported", json.dumps(status)[:240])
    check("and the wording is explicit that the RECORDING will contain the area",
          "recording will contain" in (status.get("detail") or ""), json.dumps(status.get("detail")))
    mask_support(True)
    mask_mode("honest")
    a.post("/api/cameras/%d/privacy/apply" % cam, {})
    _, status = privacy(a, cam)
    check("and it recovers to confirmed once the camera can mask again",
          status.get("masking") == "confirmed", json.dumps(status)[:240])

    # ---- refusals ----------------------------------------------------------------------
    r = a.post("/api/cameras/%d/privacy" % cam, {"name": "Sliver", "points": [[0, 0], [1, 1]], "enabled": True})
    check("a zone that is not a polygon is refused",
          r.status_code != 200 and "three corners" in r.text, r.text[:200])
    r = a.post("/api/cameras/%d/privacy" % cam, {
        "name": "Dot", "points": [[0.5, 0.5], [0.501, 0.5], [0.501, 0.501]], "enabled": True})
    check("a zone with no area is refused rather than stored as protection",
          r.status_code != 200 and "too small" in r.text, r.text[:200])
    r = a.post("/api/cameras/%d/privacy" % cam, {
        "name": "neighbour WINDOW", "points": SQUARE, "enabled": True})
    check("a duplicate name whatever its case is refused",
          r.status_code != 200 and "already has" in r.text, r.text[:200])

    # ---- the camera's mask limit --------------------------------------------------------
    mask_limit(1)
    r = a.post("/api/cameras/%d/privacy" % cam, {
        "name": "Pavement", "points": [[0.5, 0.5], [0.9, 0.5], [0.9, 0.9], [0.5, 0.9]], "enabled": True})
    check("a second zone can still be drawn on a camera that holds only one mask",
          r.status_code == 200, r.text[:200])
    _, status = privacy(a, cam)
    check("but the camera-side claim drops to unconfirmed rather than staying green",
          status.get("masking") != "confirmed", json.dumps(status)[:240])
    mask_limit(4)
    a.post("/api/cameras/%d/privacy/apply" % cam, {})

    # ---- switching one off ---------------------------------------------------------------
    zones, _ = privacy(a, cam)
    pavement = next((z for z in zones if z["name"] == "Pavement"), None)
    check("both zones are listed", pavement is not None, json.dumps([z["name"] for z in zones]))
    if pavement:
        r = a.post("/api/cameras/%d/privacy/%d" % (cam, pavement["id"]), {
            "name": "Pavement", "points": pavement["points"], "enabled": False})
        check("a zone can be switched off without losing the drawing", r.status_code == 200, r.text[:200])
        dev = device()
        check("and the camera stops masking it",
              len(dev.get("masks") or {}) == 1, json.dumps(dev.get("masks"))[:240])

    # ---- deleting ------------------------------------------------------------------------
    zones, _ = privacy(a, cam)
    doomed = next((z for z in zones if z["name"] == "Pavement"), None)
    if doomed:
        r = a.post("/api/cameras/%d/privacy/%d/delete" % (cam, doomed["id"]), {})
        check("a zone can be deleted", r.status_code == 200, r.text[:200])

    # ---- who may do this -------------------------------------------------------------------
    roles = result_list(a.get("/api/settings/roles"), "roles")
    role_ids = {(r.get("name") or "").lower(): r.get("id") for r in roles or []}
    if role_ids.get("operator"):
        for uname, role in (("priv-op", "operator"), ("priv-view", "viewer")):
            a.post("/api/settings/users", {
                "username": uname, "password": "Bench-Passw0rd!", "displayName": uname,
                "roleId": role_ids[role], "isActive": True, "mustChangePassword": False,
            })
        op = node("node-a", ("priv-op", "Bench-Passw0rd!"))
        vw = node("node-a", ("priv-view", "Bench-Passw0rd!"))
        # Deciding what a camera must never record is a policy decision about the site, and
        # an operator who could draw a zone could also remove one — quietly turning off a
        # protection somebody outside the building is relying on.
        check("an operator cannot read the privacy zones",
              op.get("/api/cameras/%d/privacy" % cam).status_code == 403, "")
        check("an operator cannot draw one",
              op.post("/api/cameras/%d/privacy" % cam, {"name": "x", "points": SQUARE}).status_code == 403, "")
        check("a viewer cannot either",
              vw.get("/api/cameras/%d/privacy" % cam).status_code == 403, "")

    # ---- the trail ---------------------------------------------------------------------------
    trail = result_list(a.get("/api/audit?limit=200"), "logs", "entries")
    actions = [e.get("action") for e in trail]
    check("drawing and changing a privacy zone is audited",
          "privacy.zone_change" in actions, json.dumps(actions[:10]))
    check("removing one is audited", "privacy.zone_delete" in actions, json.dumps(actions[:10]))

    # ---- REDACTION ON EXPORT ------------------------------------------------------------------
    #
    # Needs footage. The bench states plainly when it has none rather than passing a check it
    # did not run — an export bench with nothing to export proves nothing.
    # A REAL recording camera, separate from the ONVIF simulator. It has no ONVIF at all,
    # which makes it the honest pairing for this half: a camera that CANNOT mask is exactly
    # the one where the export redaction is the only protection there is.
    if getattr(fleet_harness, "NODE_IMAGE", "debian:bookworm-slim") == "debian:bookworm-slim":
        check("the node image has ffmpeg, so footage can be recorded", False,
              "re-run the harness with KOPIV2_NODE_IMAGE=debian-ffmpeg:bench")
        return report()

    start_source("w36cam", "w36cam", "cam1")
    # WAIT FOR THE SOURCE. Adding a camera whose RTSP server has not finished starting
    # gives a camera the recorder cannot open, and the symptom arrives four minutes later
    # as "no segments" — which reads as a recorder bug rather than a bench that did not
    # wait. mediamtx plus the first ffmpeg publish takes a few seconds.
    if not wait(lambda: "w36cam" in sh("docker", "ps", "--format", "{{.Names}}", check=False), 60):
        check("the RTSP source container is running", False)
        return report()
    time.sleep(12)
    check("the RTSP source is up", True)

    # THE TRAP THAT COST THIS BENCH TWO RUNS. The ffmpeg path is captured into
    # runtime_setting at FIRST boot from the config on the HOST — a Windows path — and is
    # never re-read from config afterwards. A node on the ffmpeg image therefore records
    # NOTHING, quietly, and the only symptom is "0 segments" five minutes later. It is in
    # the checklist; it is easy to forget because every other part of the fleet works.
    runtime = result_of(a.get("/api/settings/runtime"))
    runtime.setdefault("decoder", {}).setdefault("mjpeg", {})["ffmpegPath"] = "/usr/bin/ffmpeg"
    a.put("/api/settings/runtime", runtime)
    check("the node's ffmpeg path points at a binary inside the container",
          result_of(a.get("/api/settings/runtime")).get("decoder", {})
          .get("mjpeg", {}).get("ffmpegPath") == "/usr/bin/ffmpeg")

    rec_cam = add_rtsp_camera(a, "Front desk", "w36cam", "cam1")
    check("a recording camera is configured", bool(rec_cam), str(rec_cam))
    if not rec_cam:
        return report()

    # It has no ONVIF, so it cannot mask — and the product must say THAT rather than
    # claiming it could not be reached.
    _, rec_status = privacy(a, rec_cam)
    check("a camera with no ONVIF is reported as unable to mask, not as unreachable",
          rec_status.get("masking") == "unsupported", json.dumps(rec_status)[:240])

    r = a.post("/api/cameras/%d/privacy" % rec_cam, {
        "name": "Card reader", "points": [[0.05, 0.05], [0.45, 0.05], [0.45, 0.45], [0.05, 0.45]],
        "style": "color", "enabled": True})
    check("a privacy zone can be drawn on it anyway", r.status_code == 200, r.text[:200])

    rc = a.put("/api/recording/config", {
        "cameraId": rec_cam, "enabled": True, "segmentMinutes": 1,
        "retentionDays": 1, "preRollSec": 5, "postRollSec": 5,
    })
    check("recording is enabled", result_of(rc).get("config", {}).get("enabled") is True, rc.text[:200])

    print("recording for up to 4 minutes to get a finalized segment...")
    got = wait(lambda: len(segments(a, rec_cam)) >= 1, 330)
    segs = segments(a, rec_cam)
    check("the recorder wrote a finalized segment", bool(got) and len(segs) >= 1, "%d segment(s)" % len(segs))
    if not segs:
        return report()

    seg = sorted(segs, key=lambda x: x["startedAt"])[0]
    r = a.post("/api/evidence/exports", {
        "cameraId": rec_cam, "from": seg.get("startedAt"), "to": seg.get("endedAt"),
        "reason": "w3-6 bench: redaction", "redact": True,
    })
    job = result_of(r)
    check("a redacted export can be started", r.status_code == 200 and job.get("id"), r.text[:200])
    if not job.get("id"):
        return report()

    deadline = time.time() + 300
    while time.time() < deadline:
        job = result_of(a.get("/api/evidence/exports/%s" % job["id"]))
        if job.get("status") in ("ready", "failed"):
            break
        time.sleep(3)
    check("the redacted export finishes", job.get("status") == "ready", json.dumps(job)[:300])
    if job.get("status") != "ready":
        return report()

    man = job.get("manifest") or {}
    red = man.get("redaction") or {}
    check("the manifest DECLARES the bundle a redacted derivative",
          red.get("applied") is True, json.dumps(red)[:300])
    check("and names what was obscured, so the recipient knows what they are not shown",
          "Card reader" in (red.get("regions") or []), json.dumps(red.get("regions")))
    check("and warns that it will not match the source digests",
          "will not match the digests" in (red.get("note") or ""), json.dumps(red.get("note"))[:200])
    check("the output is marked as transcoded, because it is",
          (man.get("output") or {}).get("transcoded") is True, json.dumps(man.get("output"))[:240])
    check("and the SOURCE digests are still there, so the derivation is traceable",
          all(src.get("sha256") for src in (man.get("sources") or [])),
          json.dumps(man.get("sources"))[:240])
    check("the filename says REDACTED",
          "REDACTED" in ((man.get("output") or {}).get("filename") or ""),
          json.dumps((man.get("output") or {}).get("filename")))

    # VERIFY.txt is the file a PERSON reads, and it has to say it there too.
    bundle = os.path.join(ROOT, "w36-redacted.zip")
    resp = requests.get("https://127.0.0.1:%d/api/evidence/exports/%s/download" % (NODE_PORTS["node-a"], job["id"]),
                        auth=a.auth, verify=False, timeout=120)
    check("the bundle downloads", resp.status_code == 200, str(resp.status_code))
    if resp.status_code == 200:
        with open(bundle, "wb") as fh:
            fh.write(resp.content)
        with zipfile.ZipFile(bundle) as zf:
            verify = zf.read("VERIFY.txt").decode("utf-8", "replace")
        check("VERIFY.txt says, in words, that this is a redacted copy",
              "REDACTED COPY" in verify.upper(), verify[:200])
        check("and that it will not match the recorder's digests",
              "WILL NOT MATCH" in verify.upper(), "")
        check("and it names the areas that were blacked out",
              "Card reader" in verify, "")

        # AND THE PIXELS ARE ACTUALLY GONE. Everything above proves the bundle SAYS it was
        # redacted; this proves it WAS.
        #
        # TWO measurements, and the second is what makes the first mean anything: the
        # region INSIDE the zone must be black, and a region OUTSIDE it must NOT be. One
        # measurement alone passes just as happily on a video that is black everywhere,
        # on a crop that failed, and on a source that was never a picture.
        with zipfile.ZipFile(bundle) as zf:
            media = [n for n in zf.namelist() if n.lower().endswith(".mp4")]
            if media:
                zf.extract(media[0], os.path.join(ROOT, "w36"))
        if media and os.path.exists(os.path.join(ROOT, "w36", media[0])):
            mp4_dir = os.path.join(ROOT, "w36")
            inside = region_luma(mp4_dir, media[0], "iw*0.3:ih*0.3:iw*0.08:ih*0.08")
            outside = region_luma(mp4_dir, media[0], "iw*0.3:ih*0.3:iw*0.6:ih*0.6")
            # BLACK IS 16, NOT 0. H.264 here is limited ("TV") range, where luma runs
            # 16-235 and black is exactly 16 — so a threshold of "near zero" fails on a
            # perfectly black rectangle and reads as a broken redaction. The measured
            # values on this bench are 16.0 inside and ~116 outside, which is not a close
            # call in either direction.
            check("the redacted copy is BLACK where the zone was",
                  inside is not None and inside <= 20.0, "inside YAVG=%s (16 = black in limited range)" % inside)
            check("...and is still a picture everywhere else, so that means something",
                  outside is not None and outside > 20.0, "outside YAVG=%s" % outside)

    return report()


if __name__ == "__main__":
    try:
        sys.exit(main())
    finally:
        sh("docker", "rm", "-f", SIM, check=False)
        sh("docker", "rm", "-f", "w36cam", check=False)
