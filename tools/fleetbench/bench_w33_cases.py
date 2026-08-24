# W3-3a bench: case files, against a node holding real recorded footage.
#
# The claim under test is not "an endpoint returns JSON". It is that an investigation
# opened today still has its evidence next week, and that what you hand over is what was
# recorded. That decomposes into things no unit test can reach:
#
#   1. THE HOLD SURVIVES A REAL PURGE, ROW AND FILE. The retention sweep and the operator's
#      "Purge now" both delete the mp4 as well as the row. A hold that keeps the row and
#      loses the file is worse than no hold: the case still lists the evidence.
#   2. CLOSING RELEASES IT. A hold that never releases is a disk that fills up, and that
#      half is the one that is easy to get wrong and impossible to notice.
#   3. THE BUNDLE CONTAINS PLAYABLE FOOTAGE OF THE RIGHT LENGTH. ffprobe each clip: a clip
#      whose manifest claims sixty seconds of coverage and holds four is a claim about
#      evidence, and nothing above storage would notice.
#   4. THE ROLE THAT DOES THE WORK CAN DO THE WORK. An operator opens cases, adds evidence,
#      exports and downloads; cannot delete a case; a viewer sees none of it. This also
#      re-checks the single-clip export end to end for an operator, which is the defect
#      W3-3a fixed: /api/evidence was granted POST only, so the role could start an export
#      and never collect it.
#
# NOT CLAIMED. The recorder's own hourly FILE sweep (infra/recording purgeOldFiles) honours
# the hold through a predicate on RecorderConfig, and this bench does not drive it: its
# ticker is one hour and compressing that would bench software that does not ship. It is
# unit-tested (TestTheRecorderPredicateAnswersTheSameAsThePurge) and mutation-checked
# instead. The DB-side purge is the primary defence and IS driven here, file and all.
import io
import json
import os
import sqlite3
import subprocess
import sys
import time
import zipfile

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import fleet_harness
from fleet_harness import Node, NODE_PORTS, ROOT, result_of, sh, PASSWORDS

if getattr(fleet_harness, "NODE_IMAGE", "debian:bookworm-slim") == "debian:bookworm-slim":
    raise SystemExit(
        "the node containers are running an image with no ffmpeg, so they cannot record. "
        "Re-run the harness with KOPIV2_NODE_IMAGE=debian-ffmpeg:bench first.")

urllib3.disable_warnings()
CHECKS = []

SRC_ONE = "casesrc-one"
SRC_TWO = "casesrc-two"
# BACKDATE is how far into the past the recorded segments are moved so a one-day retention
# policy considers them expired. Seeding the past rather than waiting for the future: the
# purge scores a cutoff, and the cutoff is already behind us.
BACKDATE = 3 * 86400


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


def start_source(container, alias, path):
    sh("docker", "rm", "-f", container, check=False)
    sh("docker", "run", "-d", "--name", container, "--network", "benchnet",
       "--network-alias", alias, "--entrypoint", "sh", "bench-rtsp:latest", "-c",
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
        "username": "", "password": "", "description": "w3-3 bench camera",
    })
    cam = result_of(r)
    return cam.get("id") or cam.get("cameraId") or cam.get("result")


def find_db(node_name):
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


def backdate(node_name, seconds):
    """Move every recorded segment AND every case item `seconds` into the past, with the
    app STOPPED.

    Both tables, by the same amount: backdating only the footage would slide it out from
    under the evidence that points at it, and the hold would then correctly protect
    nothing — a bench measuring its own scene rather than the product.

    Never write a container's sqlite while the app runs — the write lands mid-WAL over a
    bind mount and is discarded on restart, and the failure looks exactly like retention
    ignoring the rows.
    """
    sh("docker", "stop", node_name)
    moved, err = 0, ""
    try:
        path = find_db(node_name)
        if not path:
            return 0, "no sqlite file for " + node_name
        conn = sqlite3.connect(path)
        try:
            conn.execute(
                "UPDATE recording_segment SET started_at = started_at - ?,"
                " ended_at = CASE WHEN ended_at > 0 THEN ended_at - ? ELSE 0 END",
                (seconds, seconds))
            conn.execute(
                "UPDATE case_item SET started_at = started_at - ?,"
                " ended_at = CASE WHEN ended_at > 0 THEN ended_at - ? ELSE 0 END"
                " WHERE camera_id > 0",
                (seconds, seconds))
            conn.commit()
            moved = conn.execute("SELECT COUNT(*) FROM recording_segment").fetchone()[0]
        finally:
            conn.close()
    except Exception as exc:
        err = "%s: %s" % (exc.__class__.__name__, exc)
    finally:
        sh("docker", "start", node_name)
        if not wait_node(node_name):
            err = err or (node_name + " did not come back after the backdate")
    return moved, err


def segments(n, cam):
    return result_of(n.get("/api/recording/segments?cameraId=%d&limit=100" % cam)).get("items") or []


def file_exists(node_name, path):
    out = subprocess.run(["docker", "exec", node_name, "sh", "-c",
                          "test -f '%s' && echo yes || echo no" % path],
                         capture_output=True, text=True)
    return out.stdout.strip() == "yes"


def ffprobe_duration(local_path):
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
    a = node("node-a")
    bench_dir = os.path.join(ROOT, "w33")
    os.makedirs(bench_dir, exist_ok=True)

    # ---- record something real ---------------------------------------------------
    start_source(SRC_ONE, "casecam1", "cam1")
    start_source(SRC_TWO, "casecam2", "cam2")
    time.sleep(8)

    # The ffmpeg path is captured into runtime_setting at FIRST boot and never re-read
    # from config, so a node whose image was built elsewhere records nothing, quietly.
    runtime = result_of(a.get("/api/settings/runtime"))
    runtime.setdefault("decoder", {}).setdefault("mjpeg", {})["ffmpegPath"] = "/usr/bin/ffmpeg"
    a.put("/api/settings/runtime", runtime)
    check("the node's ffmpeg path points at a binary inside the container",
          result_of(a.get("/api/settings/runtime")).get("decoder", {})
          .get("mjpeg", {}).get("ffmpegPath") == "/usr/bin/ffmpeg")

    cam1 = add_camera(a, "Loading bay", "casecam1", "cam1")
    cam2 = add_camera(a, "Car park", "casecam2", "cam2")
    check("two cameras are configured", bool(cam1) and bool(cam2), "%s %s" % (cam1, cam2))
    if not (cam1 and cam2):
        return report()

    for cid in (cam1, cam2):
        rc = a.put("/api/recording/config", {
            "cameraId": cid, "enabled": True, "segmentMinutes": 1,
            # One day, the shortest retention the product offers. Not weakened: the
            # segments are moved into the past instead, so the shipped threshold is what
            # decides.
            "retentionDays": 1, "preRollSec": 5, "postRollSec": 5,
        })
        if result_of(rc).get("config", {}).get("enabled") is not True:
            check("recording enabled on camera %d" % cid, False, rc.text[:160])
            return report()
    check("recording is enabled on both cameras", True)

    print("recording both cameras for up to 5 minutes to get three segments each...")
    got = wait(lambda: (len(segments(a, cam1)) >= 3 and len(segments(a, cam2)) >= 2), 330)
    segs1, segs2 = segments(a, cam1), segments(a, cam2)
    check("the recorder wrote several finalized segments on both cameras",
          len(segs1) >= 3 and len(segs2) >= 2,
          "cam1=%d cam2=%d" % (len(segs1), len(segs2)))
    if not (len(segs1) >= 3 and len(segs2) >= 2):
        return report()

    segs1.sort(key=lambda s: s["startedAt"])
    segs2.sort(key=lambda s: s["startedAt"])
    held1, held2 = segs1[1], segs2[0]
    doomed = [s for s in segs1 if s["id"] != held1["id"]]
    check("the bench has both held and unheld footage to tell apart",
          len(doomed) >= 2, "%d unheld on cam1" % len(doomed))

    # ---- open a case over some of it ---------------------------------------------
    case = result_of(a.post("/api/cases", {"title": "Loading bay theft",
                                           "summary": "bench case"}))
    case_id = case.get("id")
    check("a case can be opened", bool(case_id), json.dumps(case)[:160])
    if not case_id:
        return report()

    for cam, seg in ((cam1, held1), (cam2, held2)):
        r = a.post("/api/cases/%d/items" % case_id, {
            "kind": "footage", "cameraId": cam,
            "startedAt": seg["startedAt"] + 2, "endedAt": seg["startedAt"] + 20,
            "label": "person", "note": "the jacket",
        })
        if r.status_code != 200:
            check("evidence added for camera %d" % cam, False, r.text[:200])
            return report()
    # A piece of evidence whose footage never existed: the bundle must still be produced
    # and must say this one is missing.
    a.post("/api/cases/%d/items" % case_id, {
        "kind": "footage", "cameraId": cam1,
        "startedAt": int(time.time()) - 40 * 86400,
        "endedAt": int(time.time()) - 40 * 86400 + 60, "label": "car",
    })
    a.post("/api/cases/%d/items" % case_id, {"kind": "note", "note": "same person as item 1"})

    detail = result_of(a.get("/api/cases/%d" % case_id))
    hold = detail.get("hold") or {}
    check("the case reports what it is holding",
          hold.get("segments", 0) >= 2 and hold.get("bytes", 0) > 0,
          json.dumps(hold))
    check("the case says the evidence with no footage is missing",
          hold.get("missing", 0) == 1, json.dumps(hold))
    check("a note holds no footage",
          hold.get("items", 0) == 3, json.dumps(hold))

    items = detail.get("items") or []
    playable = [i for i in items if i.get("segmentId")]
    check("held evidence resolves to a playable segment", len(playable) >= 2,
          "%d of %d" % (len(playable), len(items)))
    check("the evidence with no footage is marked so on the row",
          any(i.get("footageMissing") for i in items))

    # ---- make it all expired ------------------------------------------------------
    #
    # Seeding the past rather than waiting for the future: retention scores a cutoff, and
    # after this the cutoff is behind every segment. The shipped one-day threshold is
    # untouched — compressing a threshold benches software that does not ship.
    moved, err = backdate("node-a", BACKDATE)
    check("every recorded segment and its evidence is now older than the retention window",
          moved > 0 and not err, err or ("%d segments" % moved))
    if err:
        return report()

    after = result_of(a.get("/api/cases/%d" % case_id)).get("hold") or {}
    check("once the footage is past retention the case says which clips only it is keeping",
          after.get("beyondRetention", 0) >= 2 and after.get("segments", 0) >= 2,
          json.dumps(after))

    # ---- the retention sweep -----------------------------------------------------
    #
    # The node runs a purge of its own shortly after boot, so by the time this call
    # returns the sweep may have run twice. The assertion is therefore on the STATE, not
    # on a delete count: the endpoint answering is one check, what survived is the rest.
    purge_call = a.post("/api/recording/segments/purge", {})
    check("the retention sweep runs", purge_call.status_code == 200, purge_call.text[:160])

    left1 = {s["id"]: s for s in segments(a, cam1)}
    left2 = {s["id"]: s for s in segments(a, cam2)}
    check("retention kept the segment the open case holds (camera 1)",
          held1["id"] in left1, "left: %s" % sorted(left1))
    check("retention kept the segment the open case holds (camera 2)",
          held2["id"] in left2, "left: %s" % sorted(left2))
    check("retention removed the expired segments no case holds",
          all(s["id"] not in left1 for s in doomed))
    # THE FILE, not just the row. A hold that keeps the row and loses the mp4 leaves the
    # case listing evidence that cannot be played, which is the failure it exists to stop.
    check("the held segment's FILE is still on disk",
          file_exists("node-a", held1["filePath"]), held1["filePath"])
    check("an unheld segment's file was actually removed",
          not file_exists("node-a", doomed[0]["filePath"]), doomed[0]["filePath"])

    # ---- the operator's destroy button -------------------------------------------
    now_purge = result_of(a.post("/api/recording/purge-camera", {"cameraId": cam1}))
    check("\"Purge now\" kept the held footage and said so",
          now_purge.get("keptHeld", 0) >= 1 and "Loading bay theft" in (now_purge.get("heldReason") or ""),
          json.dumps(now_purge))
    check("the held segment survived \"Purge now\", file and all",
          held1["id"] in {s["id"] for s in segments(a, cam1)}
          and file_exists("node-a", held1["filePath"]))

    # ---- the bundle ---------------------------------------------------------------
    job = result_of(a.post("/api/cases/%d/export" % case_id,
                           {"reason": "handed to the investigating officer"}))
    check("an export needs a reason",
          a.post("/api/cases/%d/export" % case_id, {"reason": " "}).status_code != 200)
    job_id = job.get("id")
    check("the case export starts", bool(job_id), json.dumps(job)[:200])
    if not job_id:
        return report()

    final = wait(lambda: (lambda j: j if j.get("status") in ("ready", "failed") else None)(
        result_of(a.get("/api/cases/exports/%s" % job_id))), 300, every=2)
    check("the bundle finished building", bool(final) and final.get("status") == "ready",
          json.dumps(final)[:300] if final else "timed out")
    if not final or final.get("status") != "ready":
        return report()

    man = final.get("caseManifest") or {}
    totals = man.get("totals") or {}
    check("the bundle contains a clip per piece of footage, and says which one is missing",
          totals.get("clipsWritten") == 2 and totals.get("clipsMissing") == 1
          and totals.get("notes") == 1, json.dumps(totals))

    dl = a.get("/api/cases/exports/%s/download" % job_id)
    zip_path = os.path.join(bench_dir, "case.zip")
    io.open(zip_path, "wb").write(dl.content)
    check("the bundle downloads as a zip", dl.status_code == 200 and len(dl.content) > 10000,
          "%s %d bytes" % (dl.status_code, len(dl.content)))

    names, clip_files = [], []
    with zipfile.ZipFile(zip_path) as zf:
        names = zf.namelist()
        for entry in man.get("clips") or []:
            if not entry.get("file"):
                continue
            out = os.path.join(bench_dir, os.path.basename(entry["file"]))
            io.open(out, "wb").write(zf.read(entry["file"]))
            clip_files.append((entry, out))
        custody = zf.read("chain-of-custody.csv").decode("utf-8", "replace")
        verify = zf.read("VERIFY.txt").decode("utf-8", "replace")
        on_disk = json.loads(zf.read("manifest.json").decode("utf-8"))

    check("the bundle carries the manifest, the custody list and the verification note",
          "manifest.json" in names and "chain-of-custody.csv" in names and "VERIFY.txt" in names,
          ",".join(names[:6]))
    check("every clip named in the manifest is in the bundle", len(clip_files) == 2,
          ",".join(names))

    import hashlib
    digests_ok = True
    for entry, path in clip_files:
        want = (entry.get("evidence") or {}).get("output", {}).get("sha256", "")
        got = hashlib.sha256(io.open(path, "rb").read()).hexdigest()
        if want != got:
            digests_ok = False
            check("clip %s matches its manifest digest" % entry["file"], False,
                  "%s != %s" % (want[:16], got[:16]))
    check("every clip matches the SHA-256 the manifest claims for it", digests_ok)

    # THE PHYSICAL ASSERTION, and the one that found the defect. A manifest can claim
    # anything; the file either holds the seconds it says it does or the bundle is a story
    # about evidence.
    #
    # It is asserted against output.mediaSeconds, NOT against the bookmarked span. A clip is
    # whole stored segments joined, so it is normally LONGER than the moment it was added
    # for — which is exactly what the first run of this bench caught: an eighteen-second
    # bookmark exported as sixty seconds of video, with nothing in the bundle saying so. The
    # manifest now states what the file holds and where the bookmark sits inside it, and
    # this checks both against the actual bytes.
    lengths, spans_ok = [], True
    for entry, path in clip_files:
        out = (entry.get("evidence") or {}).get("output", {})
        media = out.get("mediaSeconds", 0)
        dur = ffprobe_duration(path)
        lengths.append("%s claims=%ss probed=%.2fs offset=%ss"
                       % (os.path.basename(path), media, dur, out.get("requestedOffsetSeconds")))
        if not (media > 0 and abs(dur - media) <= 2.5):
            spans_ok = False
        # And the offset has to be arithmetic anybody can redo: the bookmark begins where
        # the manifest says it does, relative to the first frame of the file.
        if out.get("startsAt", 0) + out.get("requestedOffsetSeconds", 0) != entry["from"]:
            spans_ok = False
    check("every exported clip holds exactly the seconds its manifest claims, "
          "and says where the bookmark begins inside it", spans_ok, " | ".join(lengths))

    check("the chain of custody records the case being opened and worked",
          "case.create" in custody and "case.item_add" in custody
          and "recording.export" in custody, custody[:200].replace("\n", " / "))
    check("the chain of custody ends with the export itself",
          "exported this case bundle" in custody, custody[-200:].replace("\n", " / "))
    check("the chain of custody reads forwards, oldest first",
          "case.create" in custody and "recording.export" in custody
          and custody.index("case.create") < custody.index("recording.export"))
    check("the verification note says the bundle is incomplete",
          "INCOMPLETE" in verify)
    check("the manifest names the case, the exporter and the stated reason",
          on_disk.get("case", {}).get("title") == "Loading bay theft"
          and on_disk.get("reason") == "handed to the investigating officer"
          and on_disk.get("exporterName"), json.dumps(on_disk.get("case", {}))[:160])

    # ---- closing releases ---------------------------------------------------------
    check("a case cannot be closed without saying what came of it",
          a.post("/api/cases/%d/close" % case_id, {"outcome": "  "}).status_code != 200)
    closed = result_of(a.post("/api/cases/%d/close" % case_id,
                              {"outcome": "handed to police, no further action here"}))
    check("the case closes with its outcome recorded",
          closed.get("status") == "closed" and closed.get("closedAt", 0) > 0,
          json.dumps(closed)[:200])

    after = result_of(a.post("/api/recording/segments/purge", {}))
    left1 = {s["id"] for s in segments(a, cam1)}
    left2 = {s["id"] for s in segments(a, cam2)}
    check("closing the case released its footage to retention",
          held1["id"] not in left1 and held2["id"] not in left2,
          "purged=%s left1=%s left2=%s" % (after.get("deleted"), sorted(left1), sorted(left2)))
    check("the released segment's file is gone too",
          not file_exists("node-a", held1["filePath"]))

    # ---- the role that does the work ----------------------------------------------
    # result_of re-wraps a bare array as {"result": [...]}, and this endpoint answers with
    # one. Reading it as a dict of named lists silently found no roles at all.
    roles = result_of(a.get("/api/settings/roles"))
    if isinstance(roles, dict):
        roles = roles.get("result") or roles.get("roles") or []
    role_ids = {}
    for role in roles or []:
        role_ids[(role.get("name") or role.get("code") or "").lower()] = role.get("id")
    check("the appliance's built-in roles are readable",
          bool(role_ids.get("operator")) and bool(role_ids.get("viewer")), json.dumps(role_ids))
    if not role_ids.get("operator"):
        return report()

    made = []
    for uname, role in (("bench-op", "operator"), ("bench-view", "viewer")):
        r = a.post("/api/settings/users", {
            "username": uname, "password": "Bench-Passw0rd!", "displayName": uname,
            "roleId": role_ids[role], "isActive": True, "mustChangePassword": False,
        })
        made.append(r.status_code == 200)
    check("an operator and a viewer account exist", all(made))

    op = node("node-a", ("bench-op", "Bench-Passw0rd!"))
    vw = node("node-a", ("bench-view", "Bench-Passw0rd!"))

    op_case = result_of(op.post("/api/cases", {"title": "Operator's own case"}))
    check("an operator can open a case", bool(op_case.get("id")), json.dumps(op_case)[:160])
    check("a viewer cannot see cases at all", vw.get("/api/cases").status_code in (401, 403),
          str(vw.get("/api/cases").status_code))
    if op_case.get("id"):
        check("an operator cannot delete a case",
              op.delete("/api/cases/%d" % op_case["id"]).status_code in (401, 403),
              str(op.delete("/api/cases/%d" % op_case["id"]).status_code))

    # The defect W3-3a fixed: a single-clip export, end to end, AS AN OPERATOR. Start,
    # poll, download — the two reads that the POST-only grant refused.
    segs2 = segments(a, cam2)
    if segs2:
        s = segs2[0]
        ev = op.post("/api/evidence/exports", {
            "cameraId": cam2, "from": s["startedAt"] + 1, "to": s["startedAt"] + 15,
            "reason": "operator export check",
        })
        check("an operator can START a single-clip evidence export", ev.status_code == 200,
              ev.text[:160])
        ev_id = result_of(ev).get("id") if ev.status_code == 200 else ""
        if ev_id:
            st = wait(lambda: (lambda j: j if j.get("status") in ("ready", "failed") else None)(
                result_of(op.get("/api/evidence/exports/%s" % ev_id))), 180, every=2)
            check("an operator can POLL their own export", bool(st), json.dumps(st)[:200] if st else "")
            got = op.get("/api/evidence/exports/%s/download" % ev_id)
            check("an operator can DOWNLOAD the bundle they exported",
                  got.status_code == 200 and len(got.content) > 1000,
                  "%s %d bytes" % (got.status_code, len(got.content)))
    check("a case export id cannot be collected through the single-clip route",
          op.get("/api/evidence/exports/%s/download" % job_id).status_code != 200)

    io.open(os.path.join(ROOT, "w33_context.json"), "w", encoding="utf-8").write(json.dumps({
        "nodePort": NODE_PORTS["node-a"], "cameraIds": [cam1, cam2],
        "caseId": case_id, "operator": {"username": "bench-op", "password": "Bench-Passw0rd!"},
    }, indent=2))
    return report()


if __name__ == "__main__":
    sys.exit(main())
