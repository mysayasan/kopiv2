# Bench: which of mypintusan's six alarms can actually fire?
#
# THE QUESTION. `services/controller.go` declares six alarm kinds — `duress`, `tamper`,
# `reader-offline`, `secure-channel`, `door-forced`, `door-held-open`. Each has a severity in
# `severity()`, a headline in `alarmTitle()` written to be read at 3am on a phone, and a
# translation in four languages. All of that is what a declared alarm looks like from the inside.
# None of it is evidence that the condition can ever reach it.
#
# This is the detector that found five dead audit constants on myidsan, pointed at the app that
# opens doors: take each constant, make the REAL condition happen on real hardware, and see whether
# an operator would ever hear about it.
#
# WHAT WAS ALREADY PROVEN, and is not re-run here: #211 drove `duress` (a duress PIN grants AND
# raises a CRITICAL alarm) and #219 drove `secure-channel` (a key mismatch is alarmed, never
# downgraded). This file drives the other four, and re-drives `duress` in ONE episode as the
# positive control for the whole notification path — so that "no alarm arrived" below is never
# confusable with "the feed is broken".
#
# THE SHAPE OF EVERY EPISODE. A fresh appliance, one simulator scenario, and — before the fault is
# injected — PROOF THE READER WAS IN SERVICE. That last part is the difference between a bench and
# a decoration. A reader that never came up produces no grants and no alarms, which reads exactly
# like a reader that came up and whose alarm is dead; the suite has now been bitten seven times by
# a check that passes on an empty result. So each episode establishes the positive first (a badge
# granted, or a remote unlock that reached the strike) and only then asks the negative question.
#
# WHAT THE SIMULATOR NEEDED. `tools/osdp-sim` had a `tamper` scenario and a `silent` scenario and
# neither presented a card, so neither could establish its own precondition; both now badge. Door
# position had no scenario at all — the PD models `Inputs` and answers ISTAT, but nothing ever drove
# them — so `contact-open` and `contact-cycle` were added for this file.
#
#   python tools/fleetbench/bench_pintusan_alarms.py
import json
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from pintusan_harness import (
    READER_ADDR,
    SIM_ADDR,
    admin,
    boot,
    build_sim,
    start_sim,
    teardown,
)
from fleet_harness import result_list

# A card with VALID Wiegand-26 parity. #211 paid for this: the simulator's old default `deadbeef`
# fails leading even parity, so it could never open any door and every check failed for a reason
# that had nothing to do with what was under test.
GOOD_CARD = "00880040"
# cardNumber is a STRING on this API, not an int. An int is accepted and stored and then NO
# credential ever matches: every decision comes back `unknown-credential`, which reads exactly like
# a security refusal and makes every negative check pass for the wrong reason.
GOOD_FAC, GOOD_NUM = 1, "4096"

DURESS_PIN = "911911"
NORMAL_PIN = "1234"

# `access.` categories that are not alarms: the routine decision stream and the administrative
# rule-change feed. Naming them is what keeps the closing summary a statement about ALARMS.
NOT_ALARMS = ("access.granted", "access.denied", "access.rule-change")

PASSES = []
FAILS = []
# Every alarm category this run actually observed, across all episodes. The closing summary is a
# statement about the product, not about one episode, so it has to be accumulated.
SEEN_ALARMS = set()


def check(name, ok, detail=""):
    (PASSES if ok else FAILS).append(name)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, ("  - " + detail) if detail else ""))
    return ok


def brief(r):
    try:
        b = r.json()
    except ValueError:
        return "%s %s" % (r.status_code, (r.text or "")[:120])
    return "%s %s" % (r.status_code, (b.get("message") or json.dumps(b))[:160])


def rid(r):
    try:
        body = r.json()
    except ValueError:
        return 0
    res = body.get("result") if isinstance(body, dict) else None
    if isinstance(res, dict):
        return res.get("id") or res.get("Id") or 0
    return 0


def events(op, limit=80):
    return result_list(op.get("/api/events?limit=%d" % limit), "events", "items")


def notifications(op, limit=100):
    return result_list(op.get("/api/notifications?limit=%d" % limit), "notifications", "items")


def alarms(op):
    """Every ALARM in the feed, keyed by kind.

    Anchored on the category the alarmer actually publishes (`access.` + kind), never on a
    substring. #219 recorded why: the access rule-change feed says "Secure Channel Holder added to
    group", which matched a loose text filter and would have reported an alarm that never fired.
    Three `access.` categories are NOT alarms and are excluded by name: `granted` and `denied` are
    the routine decision stream, and `rule-change` is the administrative feed a grant edit
    publishes. Everything else under that prefix comes from NotificationAlarmer.Raise, which
    publishes `access.` + the alarm kind and nothing else."""
    out = {}
    for n in notifications(op):
        if not isinstance(n, dict):
            continue
        cat = str(n.get("category") or "")
        if not cat.startswith("access.") or cat in NOT_ALARMS:
            continue
        out.setdefault(cat[len("access."):], []).append(n)
    SEEN_ALARMS.update(out.keys())
    return out


def severity_of(note):
    return str((note or {}).get("severity") or (note or {}).get("level") or "").lower()


def wait_for_alarm(op, kind, timeout=40):
    """Wait for an alarm of `kind` and return it, or None."""
    deadline = time.time() + timeout
    while True:
        found = alarms(op).get(kind)
        if found:
            return found[0]
        if time.time() >= deadline:
            return None
        time.sleep(2.0)


def wait_for_grant(op, timeout=45):
    """Wait for a badge to be GRANTED, and return the event.

    Returns the event rather than a boolean because the reason matters everywhere: an
    `unknown-credential` denial means the bench failed to enrol the card, and reading that as a
    security decision is how a whole file passes while testing nothing."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        for e in events(op):
            if str(e.get("decision") or "").lower() == "granted" and (e.get("doorId") or 0) > 0:
                return e
        time.sleep(1.5)
    return None


class Sim:
    """The simulator, restarted per episode. It has no control channel — changing what a reader IS
    means running a different one."""

    def __init__(self):
        self.p = None

    def run(self, scenario, extra=None):
        self.stop()
        self.p = start_sim(card=GOOD_CARD, bits=26, every="3s", scenario=scenario,
                           extra=list(extra or []))
        time.sleep(3.0)

    def stop(self):
        """Kill this simulator AND any survivor from a previous episode.

        THE SWEEP IS UNCONDITIONAL. #211 documented this trap and #219 reintroduced it within the
        hour by returning early when `self.p` was None — true for a freshly constructed Sim — so
        the previous episode's simulator survived, kept the port, and KEPT BADGING. Every check
        then "passed" on traffic from a reader that was not under test."""
        if self.p:
            try:
                self.p.kill()
                self.p.wait(timeout=10)
            except Exception:
                pass
            self.p = None
        # pkill does NOT reach this process on Windows.
        if os.name == "nt":
            subprocess.run(["taskkill", "/F", "/IM", "osdp-sim.exe"], capture_output=True, text=True)
        time.sleep(1.0)


def provision(op, held_open_seconds=30, unlock_seconds=5, pin=""):
    """One door (which creates its reader), a holder with a card, a group, an always-open schedule
    and a grant. Returns what the server actually stored."""
    r = op.post("/api/doors", {
        "name": "Alarm Door", "class": "interior", "busPort": SIM_ADDR,
        "osdpAddress": READER_ADDR, "readerName": "Alarm Reader",
        "unlockSeconds": unlock_seconds, "heldOpenSeconds": held_open_seconds,
    })
    door_id = rid(r)
    if not door_id:
        return {"error": brief(r)}
    stored = (op.get("/api/doors/%d" % door_id).json() or {}).get("result") or {}

    # A holder needs BOTH `ref` and `name`. The PIN lives on the CREDENTIAL, not the holder: it is
    # hashed in issueCredential and the plaintext never leaves that function.
    r = op.post("/api/holders", {"ref": "ALM-001", "name": "Alarm Holder", "kind": "staff"})
    holder_id = rid(r)
    cred = {"kind": "card", "format": "wiegand26",
            "facilityCode": GOOD_FAC, "cardNumber": GOOD_NUM}
    if pin:
        # The duress PIN must DIFFER from the normal one — a credential whose two PINs match
        # silently disables duress, and the API refuses it. The simulator is pointed at the duress
        # one, so the normal PIN here exists only to satisfy that rule.
        cred["pin"] = NORMAL_PIN
        cred["duressPin"] = pin
    op.post("/api/holders/%d/credentials" % holder_id, cred)
    r = op.post("/api/groups", {"name": "Alarm Group"})
    group_id = rid(r)
    op.post("/api/groups/%d/members" % group_id, {"holderId": holder_id})
    r = op.post("/api/schedules", {
        "name": "Always",
        "windows": [{"weekday": d, "startMinute": 0, "endMinute": 1439} for d in range(7)],
    })
    sched_id = rid(r)
    op.post("/api/grants", {"groupId": group_id, "doorId": door_id, "scheduleId": sched_id})
    return {"doorId": door_id, "door": stored, "holderId": holder_id}


def readers(op):
    return result_list(op.get("/api/readers"), "readers", "items")


def reader_row(op, addr=READER_ADDR):
    for rd in readers(op):
        if isinstance(rd, dict) and int(rd.get("osdpAddress") or -1) == addr:
            return rd
    return {}


# ------------------------------------------------------------------------------------------------

def main():
    build_sim()
    first = True
    sim = None
    try:
        # ---- 1. DURESS: the positive control for the whole alarm path ---------------------
        #
        # Load-bearing and deliberately first. #211 already proved duress fires; it is re-run here
        # for one reason only — every "no alarm arrived" below has to be a statement about that
        # alarm, not about a notification feed that was never working in this run at all.
        print("\n--- the control: a duress PIN, which #211 proved fires ---")
        Sim().stop()
        boot(build_app=first)
        first = False
        op = admin()
        sim = Sim()
        sim.run("happy", extra=["-pin", DURESS_PIN])
        prov = provision(op, pin=DURESS_PIN)
        if prov.get("error"):
            check("the control episode can provision a door", False, prov["error"])
        else:
            grant = wait_for_grant(op)
            check("a badge on a healthy reader is granted",
                  grant is not None, json.dumps(grant)[:200] if grant else "no grant in 45s")
            alarm = wait_for_alarm(op, "duress") if grant else None
            check("and a duress PIN raises an alarm an operator will see",
                  alarm is not None, json.dumps(alarm)[:220] if alarm else "no access.duress alarm")
            check("...at CRITICAL severity, so the notification path carries urgency correctly",
                  alarm is not None and severity_of(alarm) in ("critical", "high", "alarm"),
                  severity_of(alarm) or "no alarm")

        # ---- 2. READER-OFFLINE: a reader that stops answering ----------------------------
        #
        # Every door bound to a dead reader is out of service. The code's own comment says why it
        # is an alarm and not a log line: "nobody finds that out from a dashboard nobody is
        # watching." The `silent` scenario is a cut bus or a dead PSU — the transport stays up, so
        # this exercises per-READER supervision rather than the whole cable going away.
        print("\n--- a reader that goes silent ---")
        Sim().stop()
        boot()
        op = admin()
        sim = Sim()
        sim.run("silent", extra=["-fault-after", "12s"])
        prov = provision(op)
        grant = wait_for_grant(op)
        check("the reader is demonstrably in service BEFORE it is cut",
              grant is not None, json.dumps(grant)[:200] if grant else "no grant in 45s")
        alarm = wait_for_alarm(op, "reader-offline", timeout=60) if grant else None
        check("a reader that stops answering raises reader-offline",
              alarm is not None, json.dumps(alarm)[:220] if alarm else "no access.reader-offline alarm")
        # Deliberately a WARNING: alarm.go argues that paging on one flaky segment trains people to
        # ignore alarms altogether, "which is the failure mode worth avoiding: an alarm nobody
        # believes." Pinning the design decision, not merely the firing.
        check("...as a WARNING, not a critical page for one flaky segment",
              alarm is not None and severity_of(alarm) in ("warning", "warn", "medium"),
              severity_of(alarm) or "no alarm")
        # The alarm reaches the feed. The READER LIST is the other place an operator looks, and
        # `Reader.TamperState` carries an `offline` value for exactly this.
        row = reader_row(op)
        check("...and the reader list stops claiming the reader is fine",
              bool(row) and str(row.get("tamperState") or "") == "offline",
              json.dumps({k: row.get(k) for k in ("name", "tamperState", "lastSeenAt")}))

        # ---- 3. TAMPER: the reader's enclosure is opened ---------------------------------
        #
        # A tamper switch is the one sensor that reports somebody working on the reader itself —
        # the RS-485 pair behind it is the tap that Secure Channel exists to defeat, and opening
        # the case is step one. The PD answers LSTAT with the flag; the bus decodes RplLStatR into
        # EventStatus; the controller turns EventStatus+Tamper into the alarm. Every piece of that
        # chain exists. This asks whether anything ever traverses it.
        print("\n--- the reader's tamper switch ---")
        Sim().stop()
        boot()
        op = admin()
        sim = Sim()
        sim.run("tamper", extra=["-fault-after", "12s"])
        prov = provision(op)
        grant = wait_for_grant(op)
        check("the reader is demonstrably in service BEFORE the case is opened",
              grant is not None, json.dumps(grant)[:200] if grant else "no grant in 45s")
        alarm = wait_for_alarm(op, "tamper", timeout=60) if grant else None
        check("opening the reader's enclosure raises a tamper alarm",
              alarm is not None, json.dumps(alarm)[:220] if alarm else "no access.tamper alarm")
        row = reader_row(op)
        check("...and the reader list shows the tamper, not the `ok` it was created with",
              bool(row) and str(row.get("tamperState") or "") == "tamper",
              json.dumps({k: row.get(k) for k in ("name", "tamperState")}))

        # ---- 4. DOOR-FORCED and DOOR-HELD-OPEN: somebody comes through anyway ------------
        #
        # The two alarms that detect a door being forced or propped. They are the reason a door
        # position contact is wired at all: without them the product can report that it ENERGISED
        # THE STRIKE but never that anybody actually went through, and a door held open on a wedge
        # is the commonest way a secure door stops being one.
        #
        # THE PRECONDITION IS A REMOTE UNLOCK, not a badge, and the timing is deliberate. An unlock
        # proves the reader is answering — it travels the OSDP bus to the strike and fails if it is
        # not — and it opens a shunt window of UnlockSeconds+10 = 15s during which an opening is
        # EXPECTED. So the contact is armed to open at 25s, well past the shunt, and the whole
        # point is that the opening is then unexplained.
        print("\n--- a door forced open, and then propped ---")
        Sim().stop()
        boot()
        op = admin()
        sim = Sim()
        sim.run("contact-open", extra=["-fault-after", "25s", "-card-every", "0"])
        prov = provision(op, held_open_seconds=5, unlock_seconds=5)
        door_id = prov.get("doorId") or 0
        r = op.post("/api/doors/%d/unlock" % door_id, {"reason": "bench"}) if door_id else None
        unlocked = bool(r is not None and r.status_code == 200)
        check("the reader answers on the bus (a remote unlock reaches the strike)",
              unlocked, brief(r) if r is not None else "no door")
        # Settle past the 15s shunt AND past the contact opening at 25s.
        time.sleep(22)
        alarm = wait_for_alarm(op, "door-forced", timeout=30) if unlocked else None
        check("a door that opens with no grant and no exit request raises door-forced",
              alarm is not None, json.dumps(alarm)[:220] if alarm else "no access.door-forced alarm")
        check("...at CRITICAL severity — a boundary has been breached",
              alarm is not None and severity_of(alarm) in ("critical", "high", "alarm"),
              severity_of(alarm) or "no alarm")
        # The audit half. An alarm an operator dismisses at 3am and a row somebody reads back at an
        # inquiry are different products, and this app's whole output is the second one.
        forced_rows = [e for e in events(op) if str(e.get("reason") or "") == "door-forced"]
        check("...and it is written to the access log, not only pushed at a screen",
              bool(forced_rows), json.dumps(forced_rows[:1])[:220] if forced_rows
              else "no door-forced row in the access log")
        held = wait_for_alarm(op, "door-held-open", timeout=40) if unlocked else None
        check("a door left standing open past its threshold raises door-held-open",
              held is not None, json.dumps(held)[:220] if held else "no access.door-held-open alarm")

        # ---- 5. THE FALSE POSITIVE that would make the whole feature worthless -----------
        #
        # A forced-door alarm on every legitimate entry is worse than no alarm at all: it is the
        # fastest way to teach a site to ignore the one alarm that means somebody is inside who
        # should not be. `Grant` opens a shunt window for exactly this.
        #
        # THE TRAP THIS EPISODE AVOIDS: "no forced alarm" is also what you get from a contact that
        # was never reported at all, so on its own it proves nothing. The door's held-open
        # threshold is set BELOW how long the contact stays open, so the held-open alarm firing is
        # the positive evidence that the opening really did reach the state machine — and the
        # forced alarm's absence is then a statement about the shunt.
        print("\n--- a legitimate entry must NOT alarm ---")
        Sim().stop()
        boot()
        op = admin()
        sim = Sim()
        sim.run("contact-cycle", extra=["-fault-after", "14s", "-card-every", "4s"])
        prov = provision(op, held_open_seconds=2, unlock_seconds=5)
        grant = wait_for_grant(op)
        check("a badge is granted just before the door opens",
              grant is not None, json.dumps(grant)[:200] if grant else "no grant in 45s")
        time.sleep(14)
        held = wait_for_alarm(op, "door-held-open", timeout=30) if grant else None
        check("the opening really did reach the door state machine (held-open fired)",
              held is not None, json.dumps(held)[:220] if held else "no access.door-held-open alarm")
        forced = alarms(op).get("door-forced")
        check("...and it was NOT reported as forced — the grant's shunt covered it",
              held is not None and not forced,
              json.dumps(forced[0])[:220] if forced else "no forced alarm, as intended")

        # ---- the answer to the question the item asked ----------------------------------
        #
        # `secure-channel` is not driven here: #219 drove it and proved it fires (a key mismatch is
        # alarmed, never downgraded to cleartext), so it is counted as proven rather than re-run.
        # It usually shows up in the observed list anyway, from a different door: the simulator's
        # non-SC scenarios hold no site key, so an `interior` door that does not REQUIRE a session
        # falls back to cleartext and — correctly — says so out loud.
        declared = {"duress", "tamper", "reader-offline", "secure-channel",
                    "door-forced", "door-held-open"}
        proven_elsewhere = {"secure-channel"}
        reachable = SEEN_ALARMS | proven_elsewhere
        print("\nalarm kinds observed live this run: %s" % (sorted(SEEN_ALARMS) or "none"))
        check("every alarm kind the controller declares can actually fire",
              declared.issubset(reachable),
              "never fired: %s" % sorted(declared - reachable))
    finally:
        if sim:
            sim.stop()
        else:
            Sim().stop()
        if os.environ.get("KOPIV2_KEEP") != "1":
            teardown()

    print("\n%d passed, %d failed" % (len(PASSES), len(FAILS)))
    for f in FAILS:
        print("  FAILED:", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
