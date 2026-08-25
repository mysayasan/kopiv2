# W3-6b bench: face redaction on evidence export, against a REAL detector on REAL recorded
# footage.
#
# THE PROBLEM THIS BENCH HAD TO SOLVE FIRST. The fleet harness films test patterns, so there
# are no faces anywhere in it — which is why W3-4 stated plainly that its evaluators were
# never driven end to end. Here that would have gutted the bench: the whole feature is a
# detector, and a run that never detects anything proves nothing.
#
# So the camera films a DRAWN face, and YuNet detects it (0.7-0.9 confidence — checked before
# this bench was written, not assumed). That gives the one thing a synthetic scene normally
# cannot: a real detector making a real detection at a position we KNOW, which is what lets
# the output be measured rather than admired.
#
# WHAT IS MEASURED, and the rule it follows. Two readings, W3-6's rule: the face region must
# be BLACK and the rest of the frame must NOT be. One reading alone passes just as happily on
# a video that is black everywhere, on a crop that silently failed, and on a source that was
# never a picture. Black is 16, not 0 — limited-range H.264 luma runs 16-235.
#
# Needs a node image with ffmpeg AND python3 + opencv, and the interpreter override:
#   KOPIV2_NODE_IMAGE=debian-ffmpeg-face:bench KOPIV2_NODE_PYTHON=python3 \
#       python tools/fleetbench/fleet_harness.py
#   KOPIV2_NODE_IMAGE=debian-ffmpeg-face:bench python tools/fleetbench/bench_w36b_faceredact.py
import io
import json
import os
import shutil
import subprocess
import sys
import time
import zipfile

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import fleet_harness
from fleet_harness import Node, NODE_PORTS, ROOT, PASSWORDS, result_of, result_list, sh

urllib3.disable_warnings()
CHECKS = []

SRC = "facesrc"
ALIAS = "facecam"
REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
MODEL = os.path.join(REPO, "apps", "mymatasan", "ai", "face_detection_yunet_2023mar.onnx")
WORK = os.path.join(ROOT, "w36b")

# The face is STATIONARY and centred, and the background moves. Both halves are deliberate:
# a fixed face means the region to measure is known without having to map an export's frames
# back to the source's, and a moving background means the "outside is not black" reading is
# of a live picture rather than of a frozen one.
FRAME_W, FRAME_H, FPS = 640, 480, 15
FACE_CX, FACE_CY, FACE_SIZE = 320, 240, 170
# Generous crop around the face: the product deliberately covers MORE than the detected box
# (28% margin), so measuring a tight crop would be measuring the wrong thing.
FACE_CROP = "150:190:245:145"
# A corner the face never reaches, and which the moving background keeps bright.
BG_CROP = "120:80:20:20"

# The terminal status is "ready", not "done". Named here rather than spelled inline four
# times: the first run of this bench asserted "done", which no code path ever sets, so a
# completely successful export was reported as a failure — a check that fails on working
# output, which is the same class of mistake as one that passes on broken output.
DONE = "ready"


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


def draw_face(img, cx, cy, size):
    """The same drawn face the detector was checked against before this bench existed."""
    import cv2
    skin = (150, 190, 220)
    cv2.ellipse(img, (cx, cy), (int(size * 0.42), int(size * 0.55)), 0, 0, 360, skin, -1)
    cv2.ellipse(img, (cx, cy - int(size * 0.30)), (int(size * 0.44), int(size * 0.30)), 0, 180, 360, (30, 30, 40), -1)
    ey = cy - int(size * 0.10)
    for dx in (-int(size * 0.17), int(size * 0.17)):
        cv2.ellipse(img, (cx + dx, ey), (int(size * 0.09), int(size * 0.05)), 0, 0, 360, (245, 245, 245), -1)
        cv2.circle(img, (cx + dx, ey), int(size * 0.045), (60, 45, 35), -1)
        cv2.circle(img, (cx + dx, ey), int(size * 0.018), (10, 10, 10), -1)
        cv2.ellipse(img, (cx + dx, ey - int(size * 0.11)), (int(size * 0.11), int(size * 0.05)),
                    0, 200, 340, (35, 35, 45), max(2, int(size * 0.025)))
    ny = cy + int(size * 0.10)
    cv2.line(img, (cx, ey + int(size * 0.04)), (cx - int(size * 0.04), ny), (110, 150, 180), max(2, int(size * 0.02)))
    cv2.line(img, (cx - int(size * 0.04), ny), (cx + int(size * 0.03), ny), (110, 150, 180), max(2, int(size * 0.02)))
    cv2.ellipse(img, (cx, cy + int(size * 0.27)), (int(size * 0.14), int(size * 0.06)),
                0, 10, 170, (70, 70, 140), max(2, int(size * 0.03)))
    return img


def make_face_clip(path, seconds=12):
    import cv2
    import numpy as np
    vw = cv2.VideoWriter(path, cv2.VideoWriter_fourcc(*"mp4v"), FPS, (FRAME_W, FRAME_H))
    for i in range(FPS * seconds):
        # A moving background, so "the rest of the frame is not black" is a reading of a
        # picture that is actually changing.
        img = np.zeros((FRAME_H, FRAME_W, 3), np.uint8)
        shift = (i * 7) % 256
        img[:, :, 0] = (np.arange(FRAME_W, dtype=np.int32)[None, :] + shift) % 256
        img[:, :, 1] = (np.arange(FRAME_H, dtype=np.int32)[:, None] + shift) % 200 + 40
        img[:, :, 2] = 90
        vw.write(draw_face(img, FACE_CX, FACE_CY, FACE_SIZE))
    vw.release()
    return os.path.exists(path) and os.path.getsize(path) > 1000


def start_source(clip_dir):
    sh("docker", "rm", "-f", SRC, check=False)
    sh("docker", "run", "-d", "--name", SRC, "--network", "benchnet",
       "--network-alias", ALIAS, "-v", clip_dir.replace("\\", "/") + ":/clip:ro",
       "--entrypoint", "sh", "bench-rtsp:latest", "-c",
       "/opt/mediamtx /opt/mediamtx.yml & sleep 2; "
       "while true; do ffmpeg -stream_loop -1 -re -i /clip/face.mp4 "
       "-c:v libx264 -preset ultrafast -g 15 -pix_fmt yuv420p -an "
       "-f rtsp -rtsp_transport tcp rtsp://127.0.0.1:8554/face; sleep 1; done")


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


def region_luma(mp4_dir, name, crop):
    """Average brightness of one region of the first frame, via ffmpeg signalstats.

    MEASURED, not guessed. W3-6's first pixel check decided "is it black" from a cropped
    PNG's FILE SIZE and passed on completely unredacted footage. subprocess directly rather
    than the harness `sh`, because ffmpeg's metadata=print writes to STDERR.
    """
    proc = subprocess.run(
        ["docker", "run", "--rm", "-v", mp4_dir.replace("\\", "/") + ":/in", "--entrypoint", "ffmpeg",
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


def media_seconds(mp4_dir, name):
    proc = subprocess.run(
        ["docker", "run", "--rm", "-v", mp4_dir.replace("\\", "/") + ":/in", "--entrypoint", "ffprobe",
         "bench-rtsp:latest", "-v", "error", "-show_entries", "format=duration",
         "-of", "default=nw=1:nk=1", "/in/" + name],
        capture_output=True, text=True)
    try:
        return float(proc.stdout.strip())
    except ValueError:
        return 0.0


def export(n, cam, frm, to, reason, redact=False, blur=False, wait_s=600):
    r = n.post("/api/evidence/exports", {
        "cameraId": cam, "from": frm, "to": to, "reason": reason,
        "redact": redact, "blurFaces": blur,
    })
    if r.status_code != 200:
        return None, r
    job = result_of(r)
    deadline = time.time() + wait_s
    while time.time() < deadline and job.get("status") in ("pending", "running"):
        time.sleep(3)
        job = result_of(n.get("/api/evidence/exports/%s" % job["id"]))
    return job, r


def fetch_bundle(n, job_id, dest_dir, tag):
    r = n.get("/api/evidence/exports/%s/download" % job_id)
    if r.status_code != 200:
        return None, None
    zpath = os.path.join(dest_dir, "bundle-%s.zip" % tag)
    with open(zpath, "wb") as fh:
        fh.write(r.content)
    with zipfile.ZipFile(zpath) as z:
        names = z.namelist()
        mp4 = next((x for x in names if x.endswith(".mp4")), None)
        man = next((x for x in names if x.endswith("manifest.json")), None)
        manifest = json.loads(z.read(man).decode("utf-8")) if man else None
        out_name = None
        if mp4:
            out_name = "media-%s.mp4" % tag
            with open(os.path.join(dest_dir, out_name), "wb") as fh:
                fh.write(z.read(mp4))
    return out_name, manifest


def main():
    if getattr(fleet_harness, "NODE_IMAGE", "debian:bookworm-slim") == "debian:bookworm-slim":
        raise SystemExit(
            "the node containers have neither ffmpeg nor opencv, so nothing here can run. "
            "Re-run the harness with KOPIV2_NODE_IMAGE=debian-ffmpeg-face:bench and "
            "KOPIV2_NODE_PYTHON=python3.")
    if not os.path.exists(MODEL):
        raise SystemExit(
            "the YuNet model is not installed at %s — a real install downloads it with the "
            "face-recognition setup, and without it this bench would measure the refusal "
            "rather than the feature." % MODEL)

    if os.path.isdir(WORK):
        shutil.rmtree(WORK, ignore_errors=True)
    os.makedirs(WORK, exist_ok=True)

    a = node("node-a")

    # THE FFMPEG TRAP, again: the path is captured into runtime_setting at FIRST boot from
    # the HOST config (a Windows path), so a node records nothing, quietly.
    runtime = result_of(a.get("/api/settings/runtime"))
    runtime.setdefault("decoder", {}).setdefault("mjpeg", {})["ffmpegPath"] = "/usr/bin/ffmpeg"
    a.put("/api/settings/runtime", runtime)
    check("the node's ffmpeg path points at a binary inside the container",
          result_of(a.get("/api/settings/runtime")).get("decoder", {})
          .get("mjpeg", {}).get("ffmpegPath") == "/usr/bin/ffmpeg")

    clip_dir = os.path.join(WORK, "clip")
    os.makedirs(clip_dir, exist_ok=True)
    check("a clip containing a drawn face was generated",
          make_face_clip(os.path.join(clip_dir, "face.mp4")))
    start_source(clip_dir)
    time.sleep(10)

    r = a.post("/api/cameras/discovered", {
        "name": "Reception", "host": ALIAS, "port": 8554,
        "rtspUrl": "rtsp://%s:8554/face" % ALIAS,
        "username": "", "password": "", "description": "w3-6b bench camera",
    })
    cam = result_of(r)
    cam_id = cam.get("id") or cam.get("cameraId") or cam.get("result")
    check("the camera filming the face is configured", bool(cam_id), "id=%s" % cam_id)
    if not cam_id:
        return report()

    rc = a.put("/api/recording/config", {
        "cameraId": cam_id, "enabled": True, "segmentMinutes": 1,
        "retentionDays": 7, "preRollSec": 5, "postRollSec": 5,
    })
    check("the node is recording it", result_of(rc).get("config", {}).get("enabled") is True,
          rc.text[:160])
    started = int(time.time())
    segs = wait(lambda: result_list(a.get("/api/recording/segments?cameraId=%d" % cam_id), "items") or None,
                300, label="the first segment")
    check("there is real recorded footage of the face", bool(segs))
    if not segs:
        return report()
    print("recording for another 70s so the export spans a finished segment...")
    time.sleep(70)
    ended = int(time.time())

    # ---- the control: an ordinary export says nothing about faces --------------------
    job, resp = export(a, cam_id, started, ended, "w3-6b control")
    # The FINAL job in the detail, not the response that started it: a check whose message
    # shows the request rather than the outcome tells you nothing about why it failed.
    check("an ordinary export still succeeds", job is not None and job.get("status") == DONE,
          json.dumps(job)[:300] if job else resp.text[:200])
    if not job or job.get("status") != DONE:
        return report()
    plain_name, plain_man = fetch_bundle(a, job["id"], WORK, "plain")
    check("the ordinary bundle claims no redaction at all",
          plain_man is not None and plain_man.get("redaction") is None,
          json.dumps((plain_man or {}).get("redaction"))[:160])
    # The face has to be VISIBLE in the control, or the measurement below proves nothing:
    # a face region that was already black would make the redacted reading meaningless.
    plain_face = region_luma(WORK, plain_name, FACE_CROP) if plain_name else None
    check("the face is plainly visible in an unredacted export",
          plain_face is not None and plain_face > 40.0,
          "face region YAVG=%s" % plain_face)

    # ---- the feature ------------------------------------------------------------------
    job, resp = export(a, cam_id, started, ended, "w3-6b disclosure copy", blur=True)
    check("an export can be asked to hide the faces in it",
          job is not None and job.get("status") == DONE,
          json.dumps(job)[:300] if job else resp.text[:200])
    if not job or job.get("status") != DONE:
        return report()

    man = job.get("manifest") or {}
    red = man.get("redaction") or {}
    faces = red.get("faces") or {}
    check("the bundle declares itself a derivative", bool(red.get("applied")))
    check("the manifest reports the face pass separately from the zones",
          bool(faces.get("applied")) and red.get("regions") in (None, [], ),
          json.dumps(red)[:200])
    check("it says how many frames were SCANNED, not just that it ran",
          faces.get("framesScanned", 0) > 0, "framesScanned=%s" % faces.get("framesScanned"))
    check("the detector actually found the face on real recorded footage",
          faces.get("facesObscured", 0) > 0, "facesObscured=%s" % faces.get("facesObscured"))
    check("it reports the safety margins it applied",
          faces.get("holdFrames", 0) > 0 and faces.get("marginPercent", 0) > 0,
          "hold=%s margin=%s%%" % (faces.get("holdFrames"), faces.get("marginPercent")))
    # THE SENTENCE THE WHOLE BLOCK EXISTS FOR. A face pass is not a guarantee, and a bundle
    # that reads like one is a claim nobody can stand behind.
    lim = faces.get("limitation") or ""
    check("the manifest says plainly that this is NOT a guarantee",
          "NOT a guarantee" in lim and "miss faces" in lim, lim[:120])
    check("...and that the count is detections, not people",
          "not the number of people" in lim, lim[-120:])
    check("the file NAMES itself a redacted derivative",
          (man.get("output") or {}).get("filename", "").startswith("camera-REDACTED"),
          (man.get("output") or {}).get("filename"))
    check("the manifest still carries the SOURCE digests, so it is traceable",
          len(man.get("sources") or []) > 0 and all(s.get("sha256") for s in man["sources"]))

    # ---- MEASURE THE PIXELS ------------------------------------------------------------
    name, dl_man = fetch_bundle(a, job["id"], WORK, "faces")
    check("the redacted bundle downloads and contains the video", bool(name))
    if not name:
        return report()
    inside = region_luma(WORK, name, FACE_CROP)
    outside = region_luma(WORK, name, BG_CROP)
    # TWO readings. Black is 16, not 0: limited-range H.264 luma runs 16-235, so a "near
    # zero" threshold fails on a perfectly black rectangle.
    check("the face region is BLACK in the redacted copy",
          inside is not None and inside <= 20.0,
          "face YAVG=%s (16 = black in limited range; %s unredacted)" % (inside, plain_face))
    check("...and the rest of the frame is still a picture, so that means something",
          outside is not None and outside > 40.0, "background YAVG=%s" % outside)

    # A render that stopped early produces a shorter file that plays perfectly and simply
    # ends. The product checks this itself; the bench checks the product's check.
    plain_secs = media_seconds(WORK, plain_name) if plain_name else 0.0
    red_secs = media_seconds(WORK, name)
    check("the redacted copy is not truncated",
          plain_secs > 0 and abs(red_secs - plain_secs) <= 1.5,
          "%.2fs redacted vs %.2fs unredacted" % (red_secs, plain_secs))

    # ---- the refusal, which is the failure mode that matters most -----------------------
    #
    # An appliance that cannot obscure faces must REFUSE, not hand back a bundle that did
    # not. W3-6's bench found exactly that shape one item ago.
    hidden = MODEL + ".hidden"
    os.rename(MODEL, hidden)
    try:
        job2, resp2 = export(a, cam_id, started, ended, "w3-6b without the model", blur=True, wait_s=30)
        refused = job2 is None and resp2.status_code != 200
        check("with no detector installed the export is REFUSED, not silently unredacted",
              refused, "%s %s" % (resp2.status_code, resp2.text[:160]))
        check("...and the refusal says what is missing",
              refused and "face detection model" in resp2.text, resp2.text[:200])
    finally:
        os.rename(hidden, MODEL)

    # ---- zones and faces together --------------------------------------------------------
    zone = a.post("/api/cameras/%d/privacy" % cam_id, {
        "name": "Doorway", "points": [[0.02, 0.60], [0.22, 0.60], [0.22, 0.95], [0.02, 0.95]],
        "style": "color", "enabled": True,
    })
    if zone.status_code == 200:
        job3, resp3 = export(a, cam_id, started, ended, "w3-6b both", redact=True, blur=True)
        ok3 = job3 is not None and job3.get("status") == DONE
        check("a bundle can hide the faces AND burn in the privacy zones", ok3,
              (job3 or {}).get("error") or resp3.text[:200])
        if ok3:
            red3 = (job3.get("manifest") or {}).get("redaction") or {}
            check("both are named in the manifest, and neither is folded into the other",
                  (red3.get("regions") or []) == ["Doorway"] and bool((red3.get("faces") or {}).get("applied")),
                  json.dumps(red3)[:200])
            name3, _ = fetch_bundle(a, job3["id"], WORK, "both")
            if name3:
                zluma = region_luma(WORK, name3, "128:168:13:288")
                fluma = region_luma(WORK, name3, FACE_CROP)
                check("the zone is black in the combined copy", zluma is not None and zluma <= 20.0,
                      "zone YAVG=%s" % zluma)
                check("and the face is too", fluma is not None and fluma <= 20.0,
                      "face YAVG=%s" % fluma)
    else:
        check("a privacy zone could be drawn for the combined test", False, zone.text[:160])

    # ---- the trail ------------------------------------------------------------------------
    trail = result_list(a.get("/api/audit?action=recording.export&limit=20"), "items")

    def meta_of(entry):
        # The trail stores metadata as a JSON STRING, not as an object. Iterating it as a
        # dict raises 'str' has no attribute 'get' — which is the same envelope-shaped
        # assumption that cost W3-7 two checks, in a different costume.
        raw = (entry or {}).get("metadata")
        if isinstance(raw, dict):
            return raw
        try:
            return json.loads(raw) if raw else {}
        except (TypeError, ValueError):
            return {}

    hid = [e for e in trail if meta_of(e).get("facesHidden") is True]
    check("the trail records WHICH kind of copy left the building", len(hid) >= 1,
          "%d of %d export entries recorded facesHidden" % (len(hid), len(trail)))

    return report()


if __name__ == "__main__":
    try:
        code = main()
    finally:
        sh("docker", "rm", "-f", SRC, check=False)
    sys.exit(code)
