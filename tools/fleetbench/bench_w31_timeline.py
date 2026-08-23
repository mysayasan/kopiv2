# W3-1 bench: timeline playback, against a node holding real recorded footage with a
# real hole in it.
#
# The claim under test is not "an endpoint returns JSON". It is that an operator can put
# a cursor on a wall-clock moment and see the frames from that moment — which decomposes
# into three physical facts the unit tests cannot reach:
#
#   1. Media time inside a stored segment runs 1:1 from its StartedAt. Every seek in the
#      product is `at - startedAt`, so if a segment's recorded span disagrees with the
#      duration ffprobe reads out of the file, every seek is silently off by the
#      difference and the player still looks like it is working.
#   2. A segment can actually be STREAMED — a 206 with the requested byte range — rather
#      than only downloaded whole. Timeline playback is nothing but seeking, so a
#      playback path that cannot answer a Range request is a download queue.
#   3. The bar and the coverage report agree about the same window, on real segments
#      produced by a real recorder, not on hand-built rows.
#
# It also makes a genuine gap and checks the product says so, because "the camera missed
# this" and "the player mis-seeked" look identical to an operator and only one of them is
# worth acting on.
import json, os, subprocess, sys, time
import urllib3, requests

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import fleet_harness
from fleet_harness import Node, NODE_PORTS, ROOT, result_of, sh, PASSWORDS

if getattr(fleet_harness, "NODE_IMAGE", "debian:bookworm-slim") == "debian:bookworm-slim":
    raise SystemExit(
        "the node containers are running an image with no ffmpeg, so they cannot record. "
        "Re-run the harness with KOPIV2_NODE_IMAGE=debian-ffmpeg:bench first.")

urllib3.disable_warnings()
CHECKS = []

# Two sources so multi-camera sync is exercised on cameras that genuinely differ: one
# records throughout, the other is paused mid-run to punch a hole in its footage.
SRC_STEADY = "tlsrc-steady"
SRC_GAPPY = "tlsrc-gappy"


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


def start_source(container, alias, path):
    """One mediamtx serving a generated RTSP stream, on the bench network."""
    sh("docker", "rm", "-f", container, check=False)
    sh("docker", "run", "-d", "--name", container, "--network", "benchnet",
       "--network-alias", alias, "--entrypoint", "sh", "bench-rtsp:latest", "-c",
       # testsrc2 rather than a flat frame: the capture path perceptually dedups
       # near-identical frames and a flat grey stream collapses to nothing on disk.
       "/opt/mediamtx /opt/mediamtx.yml & sleep 2; "
       "while true; do ffmpeg -re -f lavfi -i testsrc2=size=640x480:rate=15 "
       "-c:v libx264 -preset ultrafast -g 15 -f rtsp -rtsp_transport tcp "
       "rtsp://127.0.0.1:8554/%s; sleep 1; done" % path)


def wait(fn, timeout, every=3):
    deadline = time.time() + timeout
    while time.time() < deadline:
        v = fn()
        if v:
            return v
        time.sleep(every)
    return None


def add_camera(n, name, alias, path):
    r = n.post("/api/cameras/discovered", {
        "name": name, "host": alias, "port": 8554,
        "rtspUrl": "rtsp://%s:8554/%s" % (alias, path),
        "username": "", "password": "", "description": "w3-1 bench camera",
    })
    cam = result_of(r)
    return cam.get("id") or cam.get("cameraId") or cam.get("result")


def timeline(n, ids, frm, to):
    qs = "".join("cameraId=%d&" % i for i in ids)
    r = n.get("/api/recording/timeline?%sfrom=%d&to=%d" % (qs, frm, to))
    return r, result_of(r)


def seek(n, ids, at):
    qs = "".join("cameraId=%d&" % i for i in ids)
    r = n.get("/api/recording/timeline/seek?%sat=%d" % (qs, at))
    return r, result_of(r)


def ffprobe_duration(path):
    """Media duration of a local mp4, in seconds, via the bench ffmpeg image."""
    out = subprocess.run(
        ["docker", "run", "--rm", "-v", "%s:/w" % os.path.dirname(path).replace("\\", "/"),
         "--entrypoint", "ffprobe", "bench-ffmpeg:latest",
         "-v", "error", "-show_entries", "format=duration", "-of",
         "default=nw=1:nk=1", "/w/" + os.path.basename(path)],
        capture_output=True, text=True)
    try:
        return float(out.stdout.strip())
    except ValueError:
        return 0.0


def main():
    a = node("node-a")

    # ---- set up two recording cameras -------------------------------------------
    start_source(SRC_STEADY, "tlcam1", "cam1")
    start_source(SRC_GAPPY, "tlcam2", "cam2")
    time.sleep(8)

    # THE FFMPEG TRAP: the path is captured into runtime_setting at FIRST boot and never
    # re-read from config, so a node whose image was built elsewhere keeps a host path
    # that does not exist in the container — and records nothing, quietly.
    runtime = result_of(a.get("/api/settings/runtime"))
    runtime.setdefault("decoder", {}).setdefault("mjpeg", {})["ffmpegPath"] = "/usr/bin/ffmpeg"
    a.put("/api/settings/runtime", runtime)
    check("the node's ffmpeg path points at a binary inside the container",
          result_of(a.get("/api/settings/runtime")).get("decoder", {})
          .get("mjpeg", {}).get("ffmpegPath") == "/usr/bin/ffmpeg")

    cam_steady = add_camera(a, "Lobby", "tlcam1", "cam1")
    cam_gappy = add_camera(a, "Gate", "tlcam2", "cam2")
    check("two cameras are configured", bool(cam_steady) and bool(cam_gappy),
          "steady=%s gappy=%s" % (cam_steady, cam_gappy))
    if not (cam_steady and cam_gappy):
        return report()

    for cid in (cam_steady, cam_gappy):
        # segmentMinutes 1 so the timeline crosses real segment boundaries within the
        # bench's runtime. Crossing a boundary is the feature; a single long segment
        # would let a broken advance pass.
        rc = a.put("/api/recording/config", {
            "cameraId": cid, "enabled": True, "segmentMinutes": 1,
            "retentionDays": 7, "preRollSec": 5, "postRollSec": 5,
        })
        if result_of(rc).get("config", {}).get("enabled") is not True:
            check("recording enabled on camera %d" % cid, False, rc.text[:160])
            return report()
    check("recording is enabled on both cameras", True)

    started = int(time.time())
    first = wait(lambda: (result_of(a.get("/api/recording/segments?cameraId=%d" % cam_steady))
                          .get("items") or None), 240)
    check("the recorder is writing segments", bool(first),
          "" if first else "no segments after 240s")
    if not first:
        return report()

    # ---- punch a real hole in one camera ----------------------------------------
    #
    # Pausing the SOURCE, not the node: the recorder keeps running and simply has nothing
    # to write, which is the failure an operator actually meets (a camera that went away)
    # rather than a recorder that was switched off.
    print("recording both cameras for 150s before the outage...")
    time.sleep(150)
    outage_start = int(time.time())
    sh("docker", "pause", SRC_GAPPY)
    print("gappy source paused; holding the hole open for 150s...")
    time.sleep(150)
    sh("docker", "unpause", SRC_GAPPY)
    outage_end = int(time.time())
    print("gappy source resumed; recording another 150s...")
    time.sleep(150)
    ended = int(time.time())

    # ---- the bar -----------------------------------------------------------------
    r, tl = timeline(a, [cam_steady, cam_gappy], started, ended)
    check("GET /api/recording/timeline answers for both cameras",
          r.status_code == 200 and len(tl.get("cameras") or []) == 2,
          "%s %s" % (r.status_code, r.text[:200]))
    if r.status_code != 200:
        return report()

    by_cam = {c["cameraId"]: c for c in tl["cameras"]}
    steady = by_cam[cam_steady]
    gappy = by_cam[cam_gappy]

    check("the camera that never stopped has footage on the bar",
          len(steady["spans"]) >= 1 and steady["coveredSeconds"] > 200,
          "spans=%d covered=%ss pct=%s" % (len(steady["spans"]), steady["coveredSeconds"],
                                           steady["percent"]))

    # The hole must be VISIBLE as a hole, not merely as a lower percentage. A bar that
    # shades the outage lightly and a bar that leaves it blank are different claims.
    hole = None
    for i in range(len(gappy["spans"]) - 1):
        a_end = gappy["spans"][i]["to"]
        b_start = gappy["spans"][i + 1]["from"]
        if b_start - a_end > 60:
            hole = (a_end, b_start)
            break
    check("the paused camera's bar shows a hole where the source was down",
          hole is not None,
          "spans=%s" % json.dumps(gappy["spans"]))
    if hole:
        # Bounded rather than exact: the recorder notices the source is gone within a
        # segment, so the hole starts a little after the pause and ends a little after
        # the unpause. Asserting an exact match would fail on a healthy system.
        check("the hole lines up with the real outage",
              hole[0] >= outage_start - 90 and hole[1] <= outage_end + 120,
              "hole=%s outage=(%d,%d)" % (str(hole), outage_start, outage_end))
        check("the outage is reported at roughly its real length",
              abs((hole[1] - hole[0]) - (outage_end - outage_start)) < 120,
              "hole=%ds outage=%ds" % (hole[1] - hole[0], outage_end - outage_start))

    check("the camera that went down is reported as less covered than the one that did not",
          gappy["percent"] < steady["percent"] - 10,
          "gappy=%s%% steady=%s%%" % (gappy["percent"], steady["percent"]))

    # ---- the bar and the coverage report must tell one story ----------------------
    cov = result_of(a.get("/api/recording/coverage?cameraId=%d&from=%d&to=%d&bucket=hour"
                          % (cam_gappy, started, ended)))
    check("the timeline and the coverage report agree on the same window",
          abs(cov.get("overallPercent", -1) - gappy["percent"]) < 0.01,
          "coverage=%s timeline=%s" % (cov.get("overallPercent"), gappy["percent"]))

    shaded = sum(s["to"] - s["from"] for s in gappy["spans"])
    check("the shaded width equals the covered seconds it claims",
          shaded == gappy["coveredSeconds"],
          "shaded=%s claimed=%s" % (shaded, gappy["coveredSeconds"]))

    # ---- seeking -----------------------------------------------------------------
    inside = steady["spans"][0]["from"] + min(20, (steady["spans"][0]["to"] - steady["spans"][0]["from"]) // 2)
    r, sk = seek(a, [cam_steady], inside)
    one = (sk.get("cameras") or [{}])[0]
    check("a moment inside footage resolves to a segment without snapping",
          r.status_code == 200 and one.get("found") and not one.get("snapped"),
          json.dumps(one))
    check("the resolved offset is the moment's distance into that segment",
          one.get("resolvedAt") == inside,
          "resolvedAt=%s asked=%s" % (one.get("resolvedAt"), inside))

    seg_id = one.get("segmentId")
    seg_rows = result_of(a.get("/api/recording/segments?cameraId=%d&limit=500" % cam_steady)).get("items") or []
    seg = next((s for s in seg_rows if s["id"] == seg_id), None)
    check("seek names a segment that exists", seg is not None, "id=%s" % seg_id)
    if seg:
        check("the offset is measured from that segment's own start",
              one.get("offsetSeconds") == inside - seg["startedAt"],
              "offset=%s expected=%s" % (one.get("offsetSeconds"), inside - seg["startedAt"]))

    # THE PHYSICAL CLAIM. `at - startedAt` is only a real offset into the video if the
    # file contains as many seconds as the row says it spans. Download it and ask ffprobe.
    if seg:
        blob = a.get("/api/recording/segments/%d/download" % seg["id"])
        out = os.path.join(ROOT, "w31_seg_%d.mp4" % seg["id"])
        open(out, "wb").write(blob.content)
        dur = ffprobe_duration(out)
        span = seg["endedAt"] - seg["startedAt"]
        check("the segment's recorded span matches the media actually in the file",
              dur > 0 and abs(dur - span) <= 2,
              "ffprobe=%.2fs row span=%ds" % (dur, span))

        # And it must be STREAMABLE. A player seeking a 15-minute clip that can only be
        # fetched whole is a download queue with a scrub bar drawn on it.
        # trust_env=False deliberately: a bare requests.get(verify=False) still honours
        # REQUESTS_CA_BUNDLE from the environment, and the resulting certificate error
        # reads like the app refusing the range.
        s = requests.Session()
        s.trust_env = False
        s.verify = False
        rng = s.get(a.base + "/api/recording/segments/%d/download" % seg["id"],
                    auth=a.auth, headers={"Range": "bytes=0-1023"}, timeout=20)
        check("a segment answers a Range request with 206 Partial Content",
              rng.status_code == 206 and len(rng.content) == 1024,
              "%s %d bytes, content-range=%s"
              % (rng.status_code, len(rng.content), rng.headers.get("Content-Range")))

    # A moment in the hole must snap forward AND say how far.
    if hole:
        in_gap = hole[0] + (hole[1] - hole[0]) // 2
        r, sk = seek(a, [cam_gappy], in_gap)
        one = (sk.get("cameras") or [{}])[0]
        check("a moment in the gap snaps forward to the next footage",
              one.get("found") and one.get("snapped"), json.dumps(one))
        check("the snap says how much dead air it skipped",
              one.get("gapSeconds", 0) > 0
              and one.get("resolvedAt", 0) == in_gap + one.get("gapSeconds", 0),
              "gap=%ss resolvedAt=%s asked=%s"
              % (one.get("gapSeconds"), one.get("resolvedAt"), in_gap))
        check("the snap lands on the far side of the hole, not before it",
              one.get("resolvedAt", 0) >= hole[1] - 5,
              "resolvedAt=%s holeEnds=%s" % (one.get("resolvedAt"), hole[1]))

        # The same moment, both cameras, one call — this is what multi-camera sync is.
        # The steady camera must have real footage at the instant the other has none.
        r, sk = seek(a, [cam_steady, cam_gappy], in_gap)
        got = {c["cameraId"]: c for c in (sk.get("cameras") or [])}
        check("one seek answers for every camera on the wall", len(got) == 2, json.dumps(sk))
        if len(got) == 2:
            check("at one instant, the working camera plays while the failed one reports the hole",
                  got[cam_steady].get("found") and not got[cam_steady].get("snapped")
                  and got[cam_gappy].get("snapped"),
                  "steady=%s gappy=%s" % (json.dumps(got[cam_steady]), json.dumps(got[cam_gappy])))

    # Nothing at or after the moment is "no footage", never the nearest thing backwards —
    # which would put an investigator on the wrong side of an incident silently.
    r, sk = seek(a, [cam_steady], ended + 86400)
    one = (sk.get("cameras") or [{}])[0]
    check("a moment past the end of all footage reports no footage rather than reaching back",
          one.get("found") is False, json.dumps(one))

    # ---- the refusals ------------------------------------------------------------
    r = a.get("/api/recording/timeline?cameraId=%d&from=%d&to=%d"
              % (cam_steady, ended - 40 * 86400, ended))
    check("a window wider than the cap is refused with a range to retry",
          r.status_code == 400 and "days" in r.text, "%s %s" % (r.status_code, r.text[:200]))

    many = "".join("cameraId=%d&" % i for i in range(1, 11))
    r = a.get("/api/recording/timeline?%sfrom=%d&to=%d" % (many, started, ended))
    check("more cameras than the cap is refused",
          r.status_code == 400 and "cameras" in r.text, "%s %s" % (r.status_code, r.text[:200]))

    r = a.get("/api/recording/timeline?from=%d&to=%d" % (started, ended))
    check("a timeline with no camera is refused", r.status_code == 400, r.text[:120])

    r = a.get("/api/recording/timeline/seek?cameraId=%d" % cam_steady)
    check("a seek with no moment is refused", r.status_code == 400, r.text[:120])

    # A window whose `to` is in the future must be clamped to now, or an in-progress
    # segment gets credited with footage that has not been recorded yet.
    r, tl2 = timeline(a, [cam_steady], started, int(time.time()) + 7200)
    check("a window ending in the future is clamped to now",
          r.status_code == 200 and tl2.get("to", 0) <= int(time.time()) + 5,
          "to=%s now=%s" % (tl2.get("to"), int(time.time())))

    # ---- write what the screen check needs ---------------------------------------
    ctx = {
        "nodePort": NODE_PORTS["node-a"],
        "cameraSteady": cam_steady,
        "cameraGappy": cam_gappy,
        "from": started,
        "to": ended,
        "hole": list(hole) if hole else None,
        "password": a.auth[1],
    }
    open(os.path.join(ROOT, "w31_context.json"), "w").write(json.dumps(ctx, indent=2))
    print("\nwrote %s for the screen check" % os.path.join(ROOT, "w31_context.json"))
    return report()


if __name__ == "__main__":
    sys.exit(main())
