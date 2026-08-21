# W2-2 bench: node state history + SLA reporting, against a real two-node fleet.
#
# Everything here is asserted against the API, never against the log — a health-monitor
# line that merely mentions a node reads as success and is how an earlier bench in this
# programme produced a confident, wrong pass.
import io, json, os, re, subprocess, sys, time, zlib
import urllib3, requests

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import CP, Node, CP_PORT, NODE_PORTS, ROOT, result_of, sh, PASSWORDS

urllib3.disable_warnings()

# Everything this bench writes goes to the scratch dir, never into the repo.
SCRATCH = ROOT

CHECKS = []


def check(name, ok, detail=""):
    CHECKS.append((name, bool(ok), detail))
    print(("PASS  " if ok else "FAIL  ") + name + ("   " + detail if detail else ""))


def now():
    return int(time.time())


def login():
    cp = CP("https://127.0.0.1:%d" % CP_PORT)
    for pw in PASSWORDS:
        if cp.login(pw=pw).status_code == 200:
            return cp
    raise SystemExit("cannot log in to the control plane")


def availability(cp, days=1, frm=None, to=None):
    q = "?days=%d" % days
    if frm is not None:
        q = "?from=%d&to=%d" % (frm, to)
    r = cp.get("/api/nodes/availability" + q)
    if r.status_code != 200:
        raise SystemExit("availability failed: %s %s" % (r.status_code, r.text[:300]))
    return result_of(r)


def node_row(av, name):
    for n in av.get("nodes", []):
        if n["name"] == name:
            return n
    return None


def statuses(cp):
    r = cp.get("/api/nodes")
    return {n["name"]: n["status"] for n in (result_of(r).get("result") or r.json().get("result", []))}


def wait_status(cp, name, want, timeout=240):
    deadline = time.time() + timeout
    while time.time() < deadline:
        st = statuses(cp).get(name)
        if st == want:
            return True
        time.sleep(3)
    return False


def settle(cp):
    """Gate the bench on the fleet actually being WATCHED.

    The first run of this bench measured an outage that was already happening: the nodes
    could not enroll (the parent had stamped its own localhost on them), so neither ever
    established a control channel and both were drifting to "lost" 90 seconds after
    adoption on their own. `docker stop` then looked like it caused the outage. So:
    require both nodes to hold "online" across three consecutive sweeps before anything
    is stopped. A node that is merely online because adoption said so cannot pass this."""
    streak = 0
    deadline = time.time() + 240
    while time.time() < deadline:
        st = statuses(cp)
        if st and all(v == "online" for v in st.values()):
            streak += 1
            if streak >= 3:
                return True
        else:
            streak = 0
        time.sleep(12)
    return False


def main():
    cp = login()
    ok = settle(cp)
    check("the fleet is genuinely being watched before anything is broken", ok,
          str(statuses(cp)))
    if not ok:
        return report()

    # --- 1. baseline: a fleet adopted minutes ago must not claim a measured month -----
    av = availability(cp, days=30)
    a, b = node_row(av, "node-a"), node_row(av, "node-b")
    check("both nodes appear in the availability report", a is not None and b is not None)
    check("a node adopted minutes ago reports near-zero coverage over 30 days",
          a["coverage"] < 1.0,
          "coverage=%.4f%% measured=%ds window=%ds" % (a["coverage"], a["measuredSeconds"], av["windowSeconds"]))
    check("the unmeasured 30 days are reported as unmonitored, not as uptime",
          a["unmonitoredSeconds"] > 29 * 86400,
          "unmonitored=%ds" % a["unmonitoredSeconds"])
    check("the measured sliver is 100%% available", a["hasData"] and a["availability"] == 100,
          "availability=%s hasData=%s" % (a["availability"], a["hasData"]))

    # A window entirely BEFORE adoption is "no data", not a perfect score.
    past = availability(cp, frm=now() - 400 * 86400, to=now() - 399 * 86400)
    pa = node_row(past, "node-a")
    check("a window before the node joined reports no data, not 100%%",
          pa is not None and not pa["hasData"] and pa["availability"] == 0,
          "hasData=%s availability=%s" % (pa and pa["hasData"], pa and pa["availability"]))

    # --- 2. a real node outage --------------------------------------------------------
    print("\n-- stopping node-b --")
    stopped_at = now()
    sh("docker", "stop", "node-b")
    if not wait_status(cp, "node-b", "lost"):
        check("node-b is reported lost after the grace window", False, "never went lost")
        return report()
    lost_at = now()
    check("node-b is reported lost after the grace window", True,
          "%ds after it stopped (grace floor is 90s)" % (lost_at - stopped_at))

    print("-- restarting node-b --")
    sh("docker", "start", "node-b")
    if not wait_status(cp, "node-b", "online", timeout=300):
        check("node-b recovers", False, "never came back online")
        return report()
    back_at = now()
    check("node-b recovers", True, "down for ~%ds of wall clock" % (back_at - stopped_at))

    av = availability(cp, days=1)
    b = node_row(av, "node-b")
    a = node_row(av, "node-a")
    check("the outage is recorded as exactly one outage", b["outages"] == 1,
          "outages=%d" % b["outages"])
    # The observed outage runs from the sweep that declared it lost to the sweep (or
    # control-channel reconnect) that saw it back, so it is bounded by the wall clock and
    # is at least a few seconds long.
    observed = b["downSeconds"]
    # The recorded outage must cover most of the real one. Dating the transition to the
    # sweep that DECLARED it lost instead of to the last contact silently drops the whole
    # 90s grace window from every incident; the first run of this bench measured exactly
    # that (94 seconds of wall-clock outage recorded as 10).
    wall = back_at - stopped_at
    check("the recorded downtime covers the real outage, not just the part after the grace window",
          wall - 25 <= observed <= wall + 25,
          "down=%ds wall=%ds" % (observed, wall))
    check("node-b's availability is now below 100%%", b["availability"] < 100,
          "availability=%.2f%%" % b["availability"])
    check("the healthy node is untouched by its neighbour's outage",
          a["outages"] == 0 and a["availability"] == 100,
          "node-a outages=%d availability=%s" % (a["outages"], a["availability"]))
    check("the worst node sorts first", av["nodes"][0]["name"] == "node-b")
    check("the fleet total counts the outage once",
          av["outages"] == 1 and av["downSeconds"] == observed,
          "fleet outages=%d down=%ds" % (av["outages"], av["downSeconds"]))

    # --- 3. the control plane's OWN downtime ------------------------------------------
    print("\n-- stopping the control plane for ~110s --")
    before_gap = availability(cp, days=1)
    a_up_before = node_row(before_gap, "node-a")["upSeconds"]
    cp_stopped = now()
    sh("docker", "stop", "cp")
    time.sleep(110)
    sh("docker", "start", "cp")
    cp_back = now()
    time.sleep(5)
    cp = login()
    # Give it a full sweep to stamp the watermark and notice the gap.
    time.sleep(HB_SETTLE)

    av = availability(cp, days=1)
    check("the control plane recorded its own blind spot", av["monitorGaps"] == 1,
          "gaps=%d seconds=%d" % (av["monitorGaps"], av["monitorGapSeconds"]))
    gap = av["monitorGapSeconds"]
    check("the recorded gap matches how long it was actually down",
          abs(gap - (cp_back - cp_stopped)) <= 25,
          "recorded=%ds actual=%ds" % (gap, cp_back - cp_stopped))
    a_after = node_row(av, "node-a")
    grew = a_after["upSeconds"] - a_up_before
    elapsed = now() - cp_stopped
    check("the dead period was NOT credited to the fleet as uptime",
          grew < elapsed - gap + 30,
          "uptime grew %ds over %ds elapsed, of which %ds was unmonitored" % (grew, elapsed, gap))

    # A PINNED window over just the outage-free period around the control-plane restart:
    # node-a was up throughout, so every unmeasured second in it is the gap and nothing
    # else. A trailing "last 24h" window cannot show this — it is dominated by the time
    # before the fleet existed, and any assertion against it passes for the wrong reason.
    pinned = availability(cp, frm=cp_stopped - 60, to=now())
    pa = node_row(pinned, "node-a")
    check("over a pinned window the unmonitored time IS the control plane's own downtime",
          abs(pa["unmonitoredSeconds"] - gap) <= 20,
          "unmonitored=%ds gap=%ds window=%ds" % (pa["unmonitoredSeconds"], gap, pinned["windowSeconds"]))
    check("the healthy node still reports 100%% over that window — the gap is not downtime",
          pa["hasData"] and pa["availability"] == 100 and pa["outages"] == 0,
          "availability=%s outages=%d" % (pa["availability"], pa["outages"]))
    check("coverage over the pinned window reflects the blind spot",
          20 < pa["coverage"] < 100,
          "coverage=%.2f%%" % pa["coverage"])

    # --- 4. the monthly breakdown -----------------------------------------------------
    av90 = availability(cp, days=90)
    months = av90.get("months", [])
    check("the report is broken down by calendar month", len(months) >= 3,
          "months=%s" % ",".join(m["month"] for m in months))
    covered = sum(m["to"] - m["from"] for m in months)
    check("the month buckets tile the window exactly",
          covered == av90["windowSeconds"],
          "buckets=%ds window=%ds" % (covered, av90["windowSeconds"]))
    empty = [m for m in months if not m["hasData"]]
    check("months before the fleet existed read 'no data', not 100%%",
          all(m["availability"] == 0 for m in empty) and len(empty) >= 1,
          "%d empty month(s)" % len(empty))

    # --- 5. the PDF actually carries the section --------------------------------------
    r = cp.s.get(cp.base + "/api/reports/fleet-health.pdf?range=1", timeout=120)
    pdf = r.content
    check("the fleet health report renders", r.status_code == 200 and pdf[:4] == b"%PDF",
          "%d bytes" % len(pdf))
    text = extract_pdf_text(pdf)
    io.open(os.path.join(SCRATCH, "fleet-health.pdf"), "wb").write(pdf)
    check("the PDF contains the Availability section", "Availability" in text)
    check("the PDF names both nodes", "node-a" in text and "node-b" in text)
    check("the PDF reports the monitoring gap in words", "not monitoring" in text or "was not" in text,
          text_snippet(text, "observed"))
    check("the old 'historical uptime is not yet tracked' footnote is gone",
          "not yet tracked" not in text)

    # --- 6. authorisation --------------------------------------------------------------
    anon = requests.get("https://127.0.0.1:%d/api/nodes/availability" % CP_PORT, verify=False, timeout=20)
    check("an unauthenticated caller cannot read the fleet's uptime record",
          anon.status_code in (401, 403), "status=%d" % anon.status_code)

    # --- 7. releasing a node forgets its history --------------------------------------
    nodes = result_of(cp.get("/api/nodes")).get("result") or cp.get("/api/nodes").json()["result"]
    bid = [n["nodeId"] for n in nodes if n["name"] == "node-b"][0]
    r = cp.post("/api/nodes/%s/release" % bid)
    check("node-b is released", r.status_code == 200, "%d %s" % (r.status_code, r.text[:120]))
    av = availability(cp, days=1)
    check("a released node leaves the availability report",
          node_row(av, "node-b") is None and node_row(av, "node-a") is not None)
    # The definitive check: read the table itself, with the app STOPPED (never read the
    # sqlite of a running app — mid-WAL over a bind mount).
    sh("docker", "stop", "cp")
    rows = sqlite_rows("select node_id, state from node_state_event")
    sh("docker", "start", "cp")
    check("the released node's history rows are gone from the table",
          all(row[0] != bid for row in rows),
          "%d rows remain, for %d node(s)" % (len(rows), len(set(r[0] for r in rows))))
    check("the surviving node still has its history", len(rows) > 0)

    report()


HB_SETTLE = 30


def sqlite_rows(sql):
    import sqlite3
    db = os.path.join(ROOT, "cp", "myseliasan.db")
    con = sqlite3.connect(db)
    try:
        return con.execute(sql).fetchall()
    finally:
        con.close()


def extract_pdf_text(pdf):
    """Pull readable text out of the fpdf output: streams are FlateDecode'd, so the
    section headings are invisible to a plain grep of the file."""
    out = []
    for m in re.finditer(rb"stream\r?\n(.*?)endstream", pdf, re.S):
        chunk = m.group(1)
        try:
            chunk = zlib.decompress(chunk)
        except Exception:
            pass
        for t in re.finditer(rb"\((.*?)\)\s*Tj", chunk, re.S):
            out.append(t.group(1).decode("latin-1", "replace"))
        for t in re.finditer(rb"\((.*?)\)", chunk, re.S):
            out.append(t.group(1).decode("latin-1", "replace"))
    return "\n".join(out)


def text_snippet(text, word):
    i = text.find(word)
    return "" if i < 0 else text[max(0, i - 60):i + 60].replace("\n", " ")


def report():
    print("\n" + "=" * 70)
    passed = sum(1 for _, ok, _ in CHECKS if ok)
    print("W2-2 bench: %d/%d" % (passed, len(CHECKS)))
    for name, ok, detail in CHECKS:
        if not ok:
            print("  FAILED: " + name + "   " + detail)
    io.open(os.path.join(SCRATCH, "bench-result.json"), "w", encoding="utf-8").write(
        json.dumps([{"check": n, "pass": ok, "detail": d} for n, ok, d in CHECKS], indent=1))
    return 0 if passed == len(CHECKS) else 1


if __name__ == "__main__":
    sys.exit(main() or 0)
