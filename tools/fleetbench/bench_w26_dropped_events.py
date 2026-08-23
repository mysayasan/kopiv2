# W2-6 bench: control-channel drop instrumentation + the replay horizon.
#
# This item exists because two paths lost events in silence. So the bench's job is to make
# loss happen for real and then prove it is no longer silent — not to prove the happy path.
#
#   * DROPS: stop the control plane, raise real alerts on a node so its notification
#     service tries to forward them up a channel that is down, and check the node's own
#     /metrics counts them by kind and reason. Then bring the control plane back and check
#     the node ADMITS the loss on its hello and the control plane records it.
#   * HORIZON: the window is 72h, so it is seeded rather than waited for — the state is
#     derived from a node's LastSeenAt, which is a fact about the PAST. Seed the past,
#     ask the endpoint, done. (Same trick as W1-3's gap alert.)
#
# No shipped threshold is weakened: the 72h window and the 2/3 warn fraction are the values
# that ship. Only LastSeenAt moves, which is what a real outage moves too.
import io, json, os, sqlite3, sys, time
import urllib3, requests

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import CP, Node, CP_PORT, NODE_PORTS, ROOT, result_of, sh, PASSWORDS

urllib3.disable_warnings()

CHECKS = []
REPLAY_WINDOW_HOURS = 72  # apps/myseliasan/app/app.go notifReplayWindow


def check(name, ok, detail=""):
    CHECKS.append((name, bool(ok), detail))
    print(("PASS  " if ok else "FAIL  ") + name + ("   " + detail if detail else ""))


def report():
    passed = sum(1 for _, ok, _ in CHECKS if ok)
    print("")
    print("%d/%d checks passed" % (passed, len(CHECKS)))
    for name, ok, detail in CHECKS:
        if not ok:
            print("  FAILED: " + name + ("   " + detail if detail else ""))
    return 0 if passed == len(CHECKS) else 1


def login():
    cp = CP("https://127.0.0.1:%d" % CP_PORT)
    for pw in PASSWORDS:
        if cp.login(pw=pw).status_code == 200:
            return cp
    raise SystemExit("cannot log in to the control plane")


def node_for(name):
    n = Node("https://127.0.0.1:%d" % NODE_PORTS[name])
    for pw in PASSWORDS:
        n.auth = ("admin", pw)
        if n.get("/api/pairing/status").status_code == 200:
            return n
    raise SystemExit("cannot authenticate to %s" % name)


# One session for every scrape, with env trust OFF.
#
# THE TRAP, and it is the same one the harness README records for sessions: a bare
# requests.get() uses a default session that HONOURS REQUESTS_CA_BUNDLE, so verify=False is
# overridden and the request fails against the bench's self-signed cert. The failure is
# silent here — the helper returned "" — which made every drop counter read zero and looked
# exactly like the feature not working. Six assertions failed against metrics that were
# present and correct all along.
_scrape = requests.Session()
_scrape.verify = False
_scrape.trust_env = False


def node_metrics(name):
    """Scrape a node's Prometheus endpoint. Raises rather than returning empty: an
    unreadable scrape is a broken bench, not a node with no metrics."""
    r = _scrape.get("https://127.0.0.1:%d/metrics" % NODE_PORTS[name], timeout=15)
    if r.status_code != 200:
        raise SystemExit("scraping %s /metrics returned %s" % (name, r.status_code))
    return r.text


def metric_value(text, name, *label_fragments):
    """Sum every sample of `name` whose line contains all the given label fragments.

    Summing rather than matching one exact label set keeps the assertion honest about what
    it is measuring: "how many notification drops were recorded", regardless of how the
    recorder happened to order the labels.
    """
    total = 0.0
    for line in text.splitlines():
        if not line.startswith(name):
            continue
        if any(frag not in line for frag in label_fragments):
            continue
        try:
            total += float(line.rsplit(" ", 1)[1])
        except (ValueError, IndexError):
            continue
    return total


def raise_alert(node, label):
    """Raise a real alert on the node, which publishes a real notification through the
    node's normal delivery path — including the control-channel sink under test.

    It CHECKS the status. The first version of this helper discarded it and posted a body
    missing ruleId, so every alert was refused with 400, no notification was ever published,
    and the drop counters correctly stayed at zero — which read exactly like the feature not
    working. A trigger that fails silently makes every assertion downstream meaningless.
    """
    r = node.post("/api/vision/alerts", {
        "ruleId": 1, "cameraId": 1, "detectionType": "object", "label": label, "confidence": 0.9,
    })
    if r.status_code != 200:
        raise SystemExit("raising an alert on the node failed: %s %s" % (r.status_code, r.text[:200]))
    return r.status_code


def settle(cp, timeout=240):
    streak = 0
    deadline = time.time() + timeout
    while time.time() < deadline:
        rows = cp.get("/api/nodes").json().get("result") or []
        st = {n["name"]: n["status"] for n in rows}
        if st and all(v == "online" for v in st.values()):
            streak += 1
            if streak >= 3:
                return True
        else:
            streak = 0
        time.sleep(10)
    return False


def main():
    cp = login()
    if not settle(cp):
        check("the fleet is genuinely being watched before anything is broken", False)
        return report()
    check("the fleet is genuinely being watched before anything is broken", True)

    node = node_for("node-a")

    # --- 1. a baseline, so the drop count is attributable to what we do next ----------
    before = node_metrics("node-a")
    base_dropped = metric_value(before, "kopiv2_control_events_dropped_total")
    base_forwarded = metric_value(before, "kopiv2_control_events_forwarded_total")
    check("the node exports the forwarding counters at all",
          "kopiv2_control_events_forwarded_total" in before,
          "forwarded=%g dropped=%g" % (base_forwarded, base_dropped))

    # A notification raised while the channel is UP must be forwarded, not dropped. Without
    # this the drop counter could be measuring nothing but its own bugs.
    raise_alert(node, "bench-connected")
    time.sleep(3)
    live = node_metrics("node-a")
    check("an event raised while the channel is up is counted as forwarded",
          metric_value(live, "kopiv2_control_events_forwarded_total") > base_forwarded
          and metric_value(live, "kopiv2_control_events_dropped_total") == base_dropped,
          "forwarded %g->%g dropped %g->%g" % (
              base_forwarded, metric_value(live, "kopiv2_control_events_forwarded_total"),
              base_dropped, metric_value(live, "kopiv2_control_events_dropped_total")))
    pre_drop_forwarded = metric_value(live, "kopiv2_control_events_forwarded_total")

    # --- 2. real loss, with nobody to receive it --------------------------------------
    print("-- stopping the control plane --")
    sh("docker", "stop", "cp")
    # Let the node notice the channel is gone before we start raising events.
    time.sleep(10)

    raised = 4
    for i in range(raised):
        raise_alert(node, "bench-dropped-%d" % i)
    time.sleep(3)

    during = node_metrics("node-a")
    dropped_now = metric_value(during, "kopiv2_control_events_dropped_total")
    check("events raised while the channel is down are COUNTED, not lost in silence",
          dropped_now >= base_dropped + raised,
          "dropped %g -> %g after raising %d alerts" % (base_dropped, dropped_now, raised))
    check("the drop is labelled with why it happened",
          metric_value(during, "kopiv2_control_events_dropped_total", 'reason="disconnected"') >= raised,
          "disconnected drops = %g" % metric_value(during, "kopiv2_control_events_dropped_total", 'reason="disconnected"'))
    check("the drop is labelled with what was lost",
          metric_value(during, "kopiv2_control_events_dropped_total", 'kind="notification"') >= raised,
          "notification drops = %g" % metric_value(during, "kopiv2_control_events_dropped_total", 'kind="notification"'))
    check("a dropped event is never also counted as forwarded",
          metric_value(during, "kopiv2_control_events_forwarded_total") == pre_drop_forwarded,
          "forwarded stayed at %g" % pre_drop_forwarded)

    # --- 3. the node admits the loss on reconnect -------------------------------------
    print("-- restarting the control plane --")
    sh("docker", "start", "cp")
    time.sleep(20)
    cp = login()
    if not settle(cp, timeout=300):
        check("the fleet reconnects after the control plane returns", False)
        return report()
    check("the fleet reconnects after the control plane returns", True)

    deadline = time.time() + 180
    admission = None
    while time.time() < deadline:
        rows = result_of(cp.get("/api/notifications?limit=50")).get("items", [])
        for row in rows:
            if "dropping events" in str(row.get("title", "")):
                admission = row
                break
        if admission:
            break
        time.sleep(5)
    check("the node ADMITS on reconnect how many events it could not forward",
          admission is not None,
          json.dumps(admission)[:220] if admission else "no admission notification arrived")
    if admission:
        body = str(admission.get("body", ""))
        check("the admission says HOW MANY were lost, not merely that some were",
              any(str(n) in body for n in range(raised, raised + 3)),
              body[:160])

    # --- 4. the replay horizon ---------------------------------------------------------
    #
    # Seeded rather than waited for: the state is derived from LastSeenAt, a fact about the
    # past. Written with the control plane STOPPED — a seed applied under a running app is
    # discarded on restart, which is the trap W1-3 recorded.
    r = cp.get("/api/nodes/replay-horizon")
    fresh = result_of(r)
    check("the horizon endpoint answers, and a connected fleet is ok",
          r.status_code == 200 and fresh.get("approaching") == 0 and fresh.get("lapsed") == 0
          and all(n["state"] == "ok" for n in fresh.get("nodes", [])),
          "status=%s report=%s" % (r.status_code, json.dumps(fresh)[:180]))
    check("every node reports the window it was judged against",
          all(n.get("windowSeconds") == REPLAY_WINDOW_HOURS * 3600 for n in fresh.get("nodes", [])),
          str([n.get("windowSeconds") for n in fresh.get("nodes", [])]))

    print("-- seeding node-b's last contact into the past --")
    sh("docker", "stop", "cp")
    sh("docker", "stop", "node-b")  # it must not be connected, or it is judged ok on sight
    db = os.path.join(ROOT, "cp", "myseliasan.db")
    conn = sqlite3.connect(db)
    try:
        # Two-thirds of the way into the window: approaching, not yet lapsed.
        approaching_at = int(time.time()) - int(REPLAY_WINDOW_HOURS * 3600 * 0.8)
        conn.execute("UPDATE managed_node SET last_seen_at=? WHERE name=?", (approaching_at, "node-b"))
        conn.commit()
    finally:
        conn.close()
    sh("docker", "start", "cp")
    time.sleep(20)
    cp = login()

    report_a = result_of(cp.get("/api/nodes/replay-horizon"))
    row_b = next((n for n in report_a.get("nodes", []) if n["nodeName"] == "node-b"), None)
    check("a node most of the way through the window is flagged BEFORE its events are lost",
          row_b is not None and row_b["state"] == "approaching",
          json.dumps(row_b)[:200] if row_b else "node-b missing")
    check("an approaching node does not yet claim anything is unrecoverable",
          row_b is not None and not row_b.get("unrecoverableBefore"),
          json.dumps(row_b)[:200] if row_b else "")

    print("-- seeding node-b past the window --")
    sh("docker", "stop", "cp")
    conn = sqlite3.connect(db)
    try:
        lapsed_at = int(time.time()) - int(REPLAY_WINDOW_HOURS * 3600 * 1.2)
        conn.execute("UPDATE managed_node SET last_seen_at=? WHERE name=?", (lapsed_at, "node-b"))
        conn.commit()
    finally:
        conn.close()
    sh("docker", "start", "cp")
    time.sleep(20)
    cp = login()

    report_b = result_of(cp.get("/api/nodes/replay-horizon"))
    row_b = next((n for n in report_b.get("nodes", []) if n["nodeName"] == "node-b"), None)
    check("a node past the window is reported as LAPSED", row_b is not None and row_b["state"] == "lapsed",
          json.dumps(row_b)[:200] if row_b else "node-b missing")
    check("a lapsed node says WHEN its events stopped being recoverable",
          row_b is not None and row_b.get("unrecoverableBefore", 0) > 0,
          str(row_b.get("unrecoverableBefore") if row_b else None))
    check("the healthy node is untouched by its neighbour's horizon",
          all(n["state"] == "ok" for n in report_b.get("nodes", []) if n["nodeName"] == "node-a"),
          json.dumps(report_b.get("nodes"))[:200])
    check("the report counts what it found", report_b.get("lapsed") == 1 and report_b.get("approaching") == 0,
          "approaching=%s lapsed=%s" % (report_b.get("approaching"), report_b.get("lapsed")))

    # The warning must have reached the operator, not just the report.
    deadline = time.time() + 120
    warned = None
    while time.time() < deadline:
        rows = result_of(cp.get("/api/notifications?limit=50")).get("items", [])
        warned = next((x for x in rows if "no longer be recovered" in str(x.get("title", "")) + str(x.get("body", ""))), None)
        if warned:
            break
        time.sleep(5)
    check("crossing the horizon raises a notification an operator will see",
          warned is not None, json.dumps(warned)[:220] if warned else "no lapsed notification")

    sh("docker", "start", "node-b")
    return report()


if __name__ == "__main__":
    sys.exit(main())
