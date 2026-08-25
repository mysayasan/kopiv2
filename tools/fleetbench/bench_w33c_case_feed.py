# W3-3c bench: putting a feed entry into a case file, on a real appliance.
#
# W3-3a shipped with this as a named follow-up: "bookmarking a camera event into a case file
# needs a case item that can reference a notification". Until now the only route from noticing
# something on the Notifications screen to having it in a case was to read the time off the
# row, go to the timeline, find it again and bookmark it — which loses the provenance and takes
# long enough that people do not do it.
#
# THE CLAIM UNDER TEST:
#
#   1. a feed entry that names a camera goes into a case with footage around it, and that
#      footage is HELD — the case's own count says so, which is the thing that stops retention
#      deleting the evidence while the case is open;
#   2. a feed entry with NO camera goes in too, and holds nothing. "The recorder rebooted at
#      03:12" is not footage and is still the fact that explains the gap either side of it;
#   3. the provenance survives: the item names the feed row it came from;
#   4. the refusals hold — the same entry twice, an entry that does not exist, and a closed
#      case.
#
# WHAT IS NOT PROVED HERE. The resolution of a feed entry that points at an AI ALERT into an
# alert item — with the alert's own camera, time and snapshot — needs a real detection, and the
# harness has a test pattern to film. That path is unit-tested and mutation-checked in
# case_notification_test.go, including the case where the alert has been purged and the feed
# row has not.
#
#   python tools/fleetbench/fleet_harness.py
#   python tools/fleetbench/bench_w33c_case_feed.py
import json
import os
import sys

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import Node, NODE_PORTS, PASSWORDS, result_of, result_list

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


def feed(n, limit=200):
    return result_list(n.get("/api/notifications?limit=%d" % limit), "items")


def cases(n):
    out = result_of(n.get("/api/cases"))
    if isinstance(out, dict):
        return out.get("cases") or []
    return out or []


def main():
    a = node("node-a")

    rows = feed(a)
    check("the appliance has a feed to work from", len(rows) > 0, "%d entries" % len(rows))
    if not rows:
        raise SystemExit("nothing in the feed; the rest of this would measure nothing")

    with_camera = next((r for r in rows if int(r.get("cameraId") or 0) > 0), None)
    without_camera = next((r for r in rows if int(r.get("cameraId") or 0) == 0), None)
    check("the feed contains an entry that names a camera", with_camera is not None,
          json.dumps(with_camera)[:120] if with_camera else "none found")
    check("and one that does not — the two go into a case differently, so both are needed "
          "for this to mean anything", without_camera is not None,
          json.dumps(without_camera)[:120] if without_camera else "none found")

    # ---- a clean case to work in -------------------------------------------------------------
    for c in cases(a):
        if str(c.get("title", "")).startswith("feed bench"):
            a.delete("/api/cases/%d" % c["id"])
    created = result_of(a.post("/api/cases", {"title": "feed bench"}))
    case_id = created.get("id") if isinstance(created, dict) else created
    check("a case can be opened", bool(case_id), json.dumps(created)[:160])
    if not case_id:
        raise SystemExit("no case to work with")

    def add(notif_id, note=""):
        return a.post("/api/cases/%d/items/from-notification" % case_id,
                      {"notificationId": int(notif_id), "note": note})

    def detail():
        return result_of(a.get("/api/cases/%d" % case_id))

    # ---- (1) an entry that names a camera ------------------------------------------------------
    if with_camera:
        r = add(with_camera["id"], "noticed on the notifications screen")
        item = result_of(r)
        check("a feed entry that names a camera goes into the case",
              r.status_code == 200 and isinstance(item, dict), r.text[:200])
        if isinstance(item, dict):
            check("as an entry of its own kind, not disguised as a hand-made bookmark",
                  item.get("kind") == "notification", str(item.get("kind")))
            check("carrying the camera it happened on",
                  int(item.get("cameraId") or 0) == int(with_camera["cameraId"]),
                  "%s vs %s" % (item.get("cameraId"), with_camera.get("cameraId")))
            # THE PART THAT MATTERS. An instant cannot be exported and cannot be held; an
            # operator who marks a moment means the footage around it.
            span = int(item.get("endedAt") or 0) - int(item.get("startedAt") or 0)
            check("with footage around the moment rather than an instant", span > 0,
                  "span %ds" % span)
            check("and the provenance points back at the feed row it came from",
                  int(item.get("sourceId") or 0) == int(with_camera["id"]),
                  "sourceId=%s want %s" % (item.get("sourceId"), with_camera["id"]))
            check("the operator's note travelled with it",
                  item.get("note") == "noticed on the notifications screen", str(item.get("note")))

        # The hold is the reason a case protects anything, so it is checked from the two
        # places that answer independently: the case detail's HOLD ledger (which is what the
        # retention sweep consults) and the case list's own footage count.
        d = detail()
        held = int(((d.get("hold") or {}).get("items")) or 0)
        listed = 0
        for row in cases(a):
            if int(row.get("id") or 0) == int(case_id):
                listed = int(row.get("footageItems") or 0)
        check("the HOLD ledger counts it — which is what stops retention deleting the "
              "evidence while the case is open", held >= 1,
              "hold=%s" % json.dumps(d.get("hold")))
        check("and the case list agrees it is holding footage", listed >= 1,
              "footageItems=%d" % listed)
        # Honest about what the harness can and cannot show: these cameras never recorded, so
        # the hold correctly reports it is protecting a span with no segments behind it.
        print("   (note: hold reports missing=%s — the bench's cameras never recorded, which "
              "is the hold being truthful, not a failure)" % (d.get("hold") or {}).get("missing"))

        # ---- (4) the same entry twice ----------------------------------------------------------
        again = add(with_camera["id"])
        check("the same feed entry cannot go into the same case twice — a duplicate clip in "
              "an export bundle is somebody else's job to explain",
              again.status_code != 200, "%d %s" % (again.status_code, again.text[:120]))

    # ---- (2) an entry with no camera -------------------------------------------------------------
    if without_camera:
        r = add(without_camera["id"])
        item = result_of(r)
        check("a feed entry with no camera is evidence too — it is the fact that explains the "
              "gap in the footage either side of it",
              r.status_code == 200 and isinstance(item, dict), r.text[:200])
        if isinstance(item, dict):
            check("and it holds no footage, because there is none to hold",
                  int(item.get("cameraId") or 0) == 0 and int(item.get("endedAt") or 0) == 0,
                  json.dumps({k: item.get(k) for k in ("cameraId", "startedAt", "endedAt")}))

    # ---- (4) the other refusals --------------------------------------------------------------------
    missing = add(99999999)
    check("a feed entry that does not exist is refused rather than stored as an empty item",
          missing.status_code != 200, "%d %s" % (missing.status_code, missing.text[:120]))

    a.post("/api/cases/%d/close" % case_id, {"outcome": "resolved"})
    if without_camera and with_camera:
        shut = add(with_camera["id"] if without_camera is with_camera else without_camera["id"])
        check("and a CLOSED case takes no more evidence, through this door as well as the "
              "old one", shut.status_code != 200, "%d %s" % (shut.status_code, shut.text[:120]))

    a.post("/api/cases/%d/reopen" % case_id, {})
    a.delete("/api/cases/%d" % case_id)
    return report()


if __name__ == "__main__":
    sys.exit(main())
