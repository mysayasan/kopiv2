# W2-7 bench: the email notification channel, on both flagships.
#
# The whole claim of this item is "an operator finds out by email". Nothing short of a real
# SMTP conversation tests that, so the bench stands up a recording relay on the bench network
# (smtp_sink.py) and asserts on what the relay RECEIVED — envelope, headers, body, MIME parts
# — not on what the app said it sent. A 200 from a save endpoint is not a delivered message.
#
# What it drives:
#
#   NODE (mymatasan)  the per-destination model. Configure the shared relay + an email
#                     destination through the real settings API, fire a notification, and
#                     read the message off the relay. Then the parts that are easy to get
#                     wrong and impossible to notice: the severity floor, the subject prefix,
#                     the classification headers, and the PARTIAL-REJECTION contract — one
#                     dead address in a distribution list must not silence the alert for
#                     everybody else, and must not cause the working addresses to be mailed
#                     again on every retry.
#
#   CONTROL PLANE     the leg that did not exist. myseliasan built a notification service and
#     (myseliasan)    never called Configure, so a node going dark reached a browser and
#                     nowhere else. Configure the notification section through the settings
#                     editor, restart, and prove mail leaves the control plane.
#
# Two guards are asserted as REFUSALS, because a mail path that silently never delivers is
# the failure this item is supposed to end: a username without STARTTLS must be rejected at
# save time (the sender will not put a credential on a cleartext link, so that configuration
# can never deliver), and email delivery without a relay must be rejected too.
import io
import json
import os
import shutil
import sys
import time

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import CP, Node, CP_PORT, NODE_PORTS, ROOT, REPO, result_of, sh, PASSWORDS

urllib3.disable_warnings()

CHECKS = []

SINK_HOST = "smtp-sink"
SINK_PORT = 1025
MAILDIR = os.path.join(ROOT, "maildir")

OPS = "ops@corp.test"
NOC = "noc@corp.test"
GONE = "gone@corp.test"          # the sink refuses this one at RCPT time
SENDER = "nvr@corp.test"
CP_SENDER = "fleet@corp.test"
CP_TO = "fleetops@corp.test"


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


# ---------------------------------------------------------------- the relay

def start_sink(reject=(GONE,)):
    """(Re)start the recording relay with an empty maildir."""
    sh("docker", "rm", "-f", "smtp-sink", check=False)
    if os.path.isdir(MAILDIR):
        shutil.rmtree(MAILDIR, ignore_errors=True)
    os.makedirs(MAILDIR, exist_ok=True)
    sh("docker", "run", "-d", "--name", "smtp-sink", "--network", "benchnet",
       "-v", MAILDIR + ":/out",
       "-v", os.path.join(REPO, "tools", "fleetbench", "smtp_sink.py") + ":/app/smtp_sink.py:ro",
       "-e", "SINK_REJECT=" + ",".join(reject),
       "-e", "SINK_PORT=%d" % SINK_PORT,
       "python:3-slim", "python", "/app/smtp_sink.py")
    # Wait for the listener rather than sleeping a guessed amount: a bench that
    # starts firing before the relay is up measures the retry path, not the feature.
    deadline = time.time() + 60
    while time.time() < deadline:
        out = sh("docker", "logs", "smtp-sink", check=False) or ""
        if "listening" in out:
            return
        time.sleep(1)
    raise SystemExit("smtp-sink never came up:\n" + (sh("docker", "logs", "smtp-sink", check=False) or ""))


def mailbox():
    """Every message the relay has stored, oldest first.

    RAISES rather than returning empty on a missing maildir. A helper that returns
    a falsy value on failure cannot tell 'nothing was delivered' from 'I failed to
    look', which is the bench-bug pattern this programme has now hit four times.
    """
    if not os.path.isdir(MAILDIR):
        raise RuntimeError("maildir %s does not exist - the sink never started" % MAILDIR)
    out = []
    for name in sorted(os.listdir(MAILDIR)):
        if not name.endswith(".json"):
            continue
        stem = os.path.join(MAILDIR, name[:-5])
        with io.open(stem + ".json", encoding="utf-8") as f:
            meta = json.load(f)
        with io.open(stem + ".eml", encoding="utf-8", newline="") as f:
            meta["raw"] = f.read()
        meta["headers"] = parse_headers(meta["raw"])
        out.append(meta)
    return out


def parse_headers(raw):
    headers = {}
    for line in raw.split("\r\n"):
        if line == "":
            break
        if ":" in line:
            k, v = line.split(":", 1)
            headers[k.strip().lower()] = v.strip()
    return headers


def matching(since=0, subject=""):
    """Messages after index `since` whose subject contains `subject`.

    EVERY assertion filters by subject, because the fleet raises its own mail while
    the bench runs — the node's disk guard alone produced four unrelated criticals
    during one run. An assertion that merely counts new messages would fail (or
    pass) on that traffic and blame the code under test.
    """
    out = mailbox()[since:]
    if not subject:
        return out
    return [m for m in out if subject in m["headers"].get("subject", "")]


def wait_mail(count=1, timeout=90, since=0, subject=""):
    """Wait until at least `count` matching messages have arrived after `since`."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        if len(matching(since, subject)) >= count:
            # A moment's grace so a second message in flight is not missed by a
            # check that then asserts "exactly one".
            time.sleep(1.5)
            return matching(since, subject)
        time.sleep(1)
    return matching(since, subject)


def quiet_for(seconds, since, subject=""):
    """Assert no matching message arrives for `seconds`. This is the half that is
    easy to get wrong and impossible to see once the run is over: a severity floor
    that does not filter looks identical to one that does until something below the
    floor is fired at it."""
    time.sleep(seconds)
    return matching(since, subject)


# ---------------------------------------------------------------- sessions

def login_cp():
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


# ---------------------------------------------------------------- node (mymatasan)

def bench_node(node):
    print("\n--- mymatasan: per-destination email ---")

    # The relay first. A username with STARTTLS off must be REFUSED: the sender will
    # not put a credential on a cleartext link, so that configuration is one that can
    # never deliver, and the operator must find that out here rather than during an
    # incident.
    bad = node.put("/api/settings/notification/smtp", {
        "enabled": True, "host": SINK_HOST, "port": SINK_PORT, "from": SENDER,
        "username": "nvr", "password": "s3cret", "useStartTls": False,
    })
    check("node refuses an SMTP username with STARTTLS off",
          bad.status_code >= 400 and "STARTTLS" in bad.text,
          "HTTP %d %s" % (bad.status_code, bad.text[:140]))

    r = node.put("/api/settings/notification/smtp", {
        "enabled": True, "host": SINK_HOST, "port": SINK_PORT, "from": SENDER,
        "username": "", "password": "", "useStartTls": False,
    })
    check("node accepts the mail relay", r.status_code == 200, "HTTP %d %s" % (r.status_code, r.text[:140]))

    # A destination with a bad recipient must not be storable — a CR/LF in an address
    # is a mail-header injection waiting for the next alert.
    inj = node.put("/api/settings/notification/destination", {
        "name": "Injected", "type": "email", "enabled": True, "minSeverity": "info",
        "email": {"to": ["ops@corp.test\r\nBcc: attacker@evil.test"]},
    })
    check("node refuses a recipient carrying CR/LF",
          inj.status_code >= 400, "HTTP %d %s" % (inj.status_code, inj.text[:140]))

    r = node.put("/api/settings/notification/destination", {
        "name": "Ops mail", "type": "email", "enabled": True, "minSeverity": "warning",
        "snapshotMode": "inline",
        "email": {"to": [OPS, NOC], "subjectPrefix": "[Warehouse 3]"},
    })
    check("node accepts an email destination", r.status_code == 200,
          "HTTP %d %s" % (r.status_code, r.text[:140]))
    if r.status_code != 200:
        return
    # KEEP THE ID. A PUT with no id CREATES; without this the later edit adds a
    # SECOND destination and every alert is delivered twice, which reads exactly
    # like a retry bug in the code under test. (It cost this bench a full run.)
    dest_id = ((result_of(r) or {}).get("destination") or {}).get("id", "")
    check("the saved destination came back with an id", bool(dest_id), dest_id or "<none>")
    if not dest_id:
        return

    # ---- a critical notification arrives, and says what it needs to say
    before = len(mailbox())
    node.post("/api/settings/notification/test?severity=critical")
    msgs = wait_mail(1, since=before, subject="Test notification")
    check("a critical notification is delivered by email", len(msgs) >= 1,
          "%d message(s)" % len(msgs))
    if not msgs:
        return
    m = msgs[0]

    check("the envelope carries both recipients",
          sorted(m["to"]) == sorted([OPS, NOC]), json.dumps(m["to"]))
    check("the envelope sender is the configured From", m["from"] == SENDER, m["from"])
    subject = m["headers"].get("subject", "")
    check("the subject carries the prefix and the severity",
          subject.startswith("[Warehouse 3]") and "CRITICAL" in subject, subject)
    check("the severity is exposed as a filterable header",
          m["headers"].get("x-kopiv2-severity") == "critical",
          m["headers"].get("x-kopiv2-severity", "<absent>"))
    check("the category is exposed as a filterable header",
          m["headers"].get("x-kopiv2-category") == "system",
          m["headers"].get("x-kopiv2-category", "<absent>"))
    body = m["raw"].split("\r\n\r\n", 1)[-1]
    check("the body states the severity and a UTC-labelled time",
          "Severity: critical" in body and "UTC" in body, body[:200].replace("\r\n", " | "))

    # ---- the severity floor actually filters
    before = len(mailbox())
    node.post("/api/settings/notification/test?severity=info")
    late = quiet_for(12, before, subject="Test notification")
    check("a notification below the severity floor is NOT emailed",
          len(late) == 0, "%d unexpected message(s)" % len(late))

    # ---- partial rejection: one dead address must not silence the rest
    r = node.put("/api/settings/notification/destination", {
        "id": dest_id,
        "name": "Ops mail", "type": "email", "enabled": True, "minSeverity": "warning",
        "snapshotMode": "inline",
        "email": {"to": [GONE, OPS], "subjectPrefix": "[Warehouse 3]"},
    })
    check("editing the destination replaces it rather than adding another",
          r.status_code == 200 and
          len([d for d in result_of(r).get("settings", {}).get("destinations", [])
               if d.get("type") == "email"]) <= 1,
          "HTTP %d" % r.status_code)
    before = len(mailbox())
    node.post("/api/settings/notification/test?severity=critical")
    msgs = wait_mail(1, since=before, subject="Test notification")
    check("one rejected recipient does not silence the alert for the others",
          len(msgs) >= 1 and msgs[0]["to"] == [OPS],
          json.dumps(msgs[0]["to"]) if msgs else "nothing delivered")
    if msgs:
        check("the relay did reject the dead address (so this is a real partial send)",
              msgs[0].get("rejected") == [GONE], json.dumps(msgs[0].get("rejected")))
        check("the To header does not name the address the relay refused",
              GONE not in msgs[0]["headers"].get("to", ""), msgs[0]["headers"].get("to", ""))

    # The retry schedule is 1s/3s/6s. A partial rejection is a DELIVERY, so the
    # working address must NOT be mailed again — otherwise one stale entry in a
    # distribution list quadruples every alert everybody else receives.
    extra = quiet_for(14, before, subject="Test notification")[1:]
    check("a partially-rejected send is not retried",
          len(extra) == 0, "%d duplicate message(s)" % len(extra))

    # ---- disabling the relay silences mail without deleting anyone's destination
    node.put("/api/settings/notification/smtp", {
        "enabled": False, "host": SINK_HOST, "port": SINK_PORT, "from": SENDER,
        "username": "", "password": "", "useStartTls": False,
    })
    before = len(mailbox())
    node.post("/api/settings/notification/test?severity=critical")
    late = quiet_for(12, before, subject="Test notification")
    check("switching the relay off silences email without touching destinations",
          len(late) == 0, "%d message(s) after the relay was disabled" % len(late))

    settings = result_of(node.get("/api/settings/notification"))
    dests = settings.get("destinations", [])
    check("the email destination survived the relay being switched off",
          any(d.get("type") == "email" for d in dests),
          json.dumps([d.get("type") for d in dests]))

    # Put it back for anything that follows.
    node.put("/api/settings/notification/smtp", {
        "enabled": True, "host": SINK_HOST, "port": SINK_PORT, "from": SENDER,
        "username": "", "password": "", "useStartTls": False,
    })
    return dest_id


# ---------------------------------------------------------------- control plane

def bench_control_plane(cp):
    print("\n--- myseliasan: the control plane's outbound leg ---")

    def save(body):
        return cp.s.put(cp.base + "/api/settings/notification", json=body,
                        headers={"X-CSRF-Token": cp.csrf()}, timeout=30)

    good = {
        "smtp": {"enabled": True, "host": SINK_HOST, "port": SINK_PORT,
                 "from": CP_SENDER, "username": "", "password": "", "useStartTls": False},
        "notification": {"email": {"enabled": True, "to": CP_TO,
                                   "subjectPrefix": "[HQ]", "minSeverity": "warning",
                                   "categories": ""}},
    }

    # Both guards, as refusals.
    bad = json.loads(json.dumps(good))
    bad["smtp"]["username"] = "fleet"
    r = save(bad)
    check("control plane refuses an SMTP username with STARTTLS off",
          r.status_code >= 400 and "STARTTLS" in r.text,
          "HTTP %d %s" % (r.status_code, r.text[:140]))

    bad = json.loads(json.dumps(good))
    bad["smtp"]["enabled"] = False
    r = save(bad)
    check("control plane refuses email delivery with no relay",
          r.status_code >= 400, "HTTP %d %s" % (r.status_code, r.text[:140]))

    bad = json.loads(json.dumps(good))
    bad["notification"]["email"]["to"] = "fleetops@corp.test\r\nBcc: attacker@evil.test"
    r = save(bad)
    check("control plane refuses a recipient carrying CR/LF",
          r.status_code >= 400, "HTTP %d %s" % (r.status_code, r.text[:140]))

    r = save(good)
    check("control plane accepts the notification settings",
          r.status_code == 200, "HTTP %d %s" % (r.status_code, r.text[:200]))
    if r.status_code != 200:
        return

    # The Test button: a real message through the real relay, before anyone relies
    # on it. This is the affordance the control plane had for Redis and not for mail.
    before = len(mailbox())
    r = cp.post("/api/settings/notification/test", {
        "host": SINK_HOST, "port": SINK_PORT, "from": CP_SENDER,
        "username": "", "password": "", "useStartTls": False, "to": CP_TO,
    })
    check("the control plane's mail test reports success",
          r.status_code == 200, "HTTP %d %s" % (r.status_code, r.text[:160]))
    msgs = wait_mail(1, since=before, subject="Test notification")
    check("the mail test actually delivered a message",
          len(msgs) >= 1 and CP_TO in msgs[0]["to"],
          json.dumps(msgs[0]["to"]) if msgs else "nothing delivered")
    if msgs:
        check("the test message carries the configured subject prefix",
              msgs[0]["headers"].get("subject", "").startswith("[HQ]"),
              msgs[0]["headers"].get("subject", ""))

    # A test against a relay that is NOT listening must fail loudly. Otherwise the
    # button reassures an operator about a path that cannot deliver.
    r = cp.post("/api/settings/notification/test", {
        "host": SINK_HOST, "port": 1, "from": CP_SENDER,
        "username": "", "password": "", "useStartTls": False, "to": CP_TO,
    })
    check("a mail test against a dead relay reports failure",
          r.status_code >= 400, "HTTP %d %s" % (r.status_code, r.text[:160]))

    # ---- the real thing: settings saved to config.json, restart, fleet event mails out
    sh("docker", "restart", "cp")
    base = "https://127.0.0.1:%d" % CP_PORT
    deadline = time.time() + 120
    up = False
    while time.time() < deadline:
        try:
            import requests as _rq
            s = _rq.Session()
            s.verify = False
            s.trust_env = False
            if s.get(base + "/api/health", timeout=5).status_code < 500:
                up = True
                break
        except Exception:
            pass
        time.sleep(2)
    check("the control plane came back after the settings restart", up)
    if not up:
        return

    cp2 = login_cp()
    section = result_of(cp2.get("/api/settings/notification"))
    stored = (((section.get("notification") or {}).get("email")) or {})
    check("the saved settings survived the restart",
          stored.get("enabled") is True and CP_TO in str(stored.get("to", "")),
          json.dumps(section)[:220])
    smtp_section = section.get("smtp") or {}
    check("the relay password is not returned to the browser",
          smtp_section.get("password", "") == "",
          json.dumps({k: v for k, v in smtp_section.items() if k == "password"}))

    # A node going dark is the event this leg exists for. Stop node-b and wait for the
    # control plane to declare it lost, then read the operator's inbox.
    before = len(mailbox())
    sh("docker", "stop", "node-b")
    print("    waiting for the control plane to notice node-b is gone…")
    # MATCH THE EVENT, NOT A WORD THAT APPEARS IN IT. The first version of this check
    # looked for "node" anywhere in the message and passed on a relayed DISK alert
    # from the surviving appliance — which proves mail works but says nothing about
    # a node going dark, the case this leg exists for. It also printed msgs[0]'s
    # subject while asserting on a different message, so the evidence in the log was
    # not the evidence the check used.
    #
    # "Node offline" is the control plane's own title (apps/myseliasan/app/app.go),
    # and the [HQ] prefix proves the mail came from the CONTROL PLANE rather than
    # from a node's own destination, which uses [Warehouse 3].
    hit = None
    deadline = time.time() + 300
    while time.time() < deadline:
        for m in mailbox()[before:]:
            subject = m["headers"].get("subject", "")
            if "Node offline" in subject and subject.startswith("[HQ]"):
                hit = m
                break
        if hit:
            break
        time.sleep(3)
    check("a node going offline reaches the operator by email",
          hit is not None,
          (hit["headers"].get("subject", "") if hit
           else "no [HQ] Node offline mail in 300s; saw: " +
                json.dumps([m["headers"].get("subject", "") for m in mailbox()[before:]])[:200]))
    if hit:
        check("the offline email is addressed to the configured recipient",
              hit["to"] == [CP_TO], json.dumps(hit["to"]))
        check("the offline email is classified for mailbox rules",
              hit["headers"].get("x-kopiv2-severity") in ("warning", "critical"),
              hit["headers"].get("x-kopiv2-severity", "<absent>"))
    sh("docker", "start", "node-b")


def main():
    start_sink()
    node = node_for("node-a")
    cp = login_cp()
    bench_node(node)
    bench_control_plane(cp)
    return report()


if __name__ == "__main__":
    sys.exit(main())
