# mypintusan bench: does the door actually open for the right person, and shut for the wrong one?
#
# WHY THIS ONE. mypintusan decides who physically walks into a building. It has the thinnest unit
# coverage in the suite and, until this, zero live exercise. Its decision path is a PURE FUNCTION
# with genuinely good table tests — which is exactly the shape that lulls, because `Decide()` can be
# flawless while the SNAPSHOT handed to it is wrong, and no test of a pure function will ever notice.
# Every claim below is therefore made by presenting a card ON A REAL OSDP BUS and reading what the
# product itself recorded, never by calling the decision function.
#
# THE CLAIMS UNDER TEST:
#
#   1. a site can be provisioned end to end — door, reader, holder, credential, group, schedule,
#      grant — and the reader comes ONLINE on the bus;
#   2. a valid badge GRANTS, and the access log says so with the holder's name on it;
#   3. an unknown card is DENIED and still logged, with its raw value, because an unknown card is
#      either somebody's first day or somebody who should not be here;
#   4. **a REVOKED credential stops working AT THE DOOR** — the question that found two defects on
#      myidsan, asked here of a physical lock. And how quickly;
#   5. LOCKDOWN denies a credential that would otherwise be granted, and says lockdown;
#   6. a DURESS PIN grants — and raises a silent alarm — and the access event a bystander could see
#      is not distinguishable from a normal grant. If the coercer standing there can tell, the
#      feature is worse than not having it;
#   7. an operator unlock is recorded, with WHO did it;
#   8. every one of these decisions reaches the access log, because the log IS the product.
#
#   python tools/fleetbench/pintusan_harness.py     # stands the app up (wipes its data dir)
#   python tools/fleetbench/bench_pintusan_door.py  # drives the simulator itself
import json
import os
import subprocess
import sys
import time

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import result_list
from pintusan_harness import (BASE, READER_ADDR, SIM_ADDR, admin, start_sim)

urllib3.disable_warnings()
CHECKS = []

# CARDS WITH VALID WIEGAND-26 PARITY. This is not a detail and it cost a run to learn: the
# simulator's default card (`deadbeef`) FAILS leading even parity, and the controller treats a
# decode failure as a hard denial — "a card whose parity failed may be one bit away from a valid
# credential belonging to somebody else". A bench using the default sees nothing but denials and
# would conclude the grant path is broken. These were generated with the app's own DecodeCard.
GOOD_CARD = "00880040"   # facility 1, number 4096
GOOD_FAC, GOOD_NUM = 1, "4096"
OTHER_CARD = "00a00040"  # facility 1, number 16384 — never issued to anybody
DURESS_CARD = "00b80040"  # facility 1, number 28672
DURESS_FAC, DURESS_NUM = 1, "28672"

PIN = "1234"
DURESS_PIN = "9111"


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


def brief(r):
    return "%d %s" % (r.status_code, (r.text or "")[:150].replace("\n", " "))


def rid(r):
    """The id out of a create response, whatever the envelope."""
    try:
        out = r.json()
    except ValueError:
        return 0
    for holder in (out.get("result"), (out.get("data") or {}).get("result"), out):
        if isinstance(holder, dict) and holder.get("id"):
            return int(holder["id"])
        if isinstance(holder, int):
            return holder
    return 0


class Sim:
    """The simulator, restarted whenever the bench needs a different card or PIN.

    There is no on-demand control channel, so 'present a different credential' means stopping the
    process and starting another. Slower than a socket and completely faithful."""

    def __init__(self):
        self.p = None

    def run(self, card, pin="", scenario="happy", every="2s"):
        self.stop()
        self.p = start_sim(card=card, pin=pin, scenario=scenario, every=every)
        # Give the CP time to re-dial the bus and bring the reader back online after the restart.
        time.sleep(8)

    def stop(self):
        if self.p:
            self.p.terminate()
            try:
                self.p.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.p.kill()
            self.p = None
            time.sleep(1)


def events(op, limit=40):
    """The product's OWN record of what happened at the door.

    `result_list` rather than a hand-rolled unwrap: this app answers `{data:{result:[...]}}`, and
    reading the wrong key looks exactly like an empty log — the envelope trap that has now cost
    five benches a check."""
    return result_list(op.get("/api/events?limit=%d" % limit), "events", "items")


def wait_for_event(op, predicate, timeout=30, after=0):
    """Wait for an access event newer than `after` that satisfies predicate.

    Correlating against the product's own log, and against a TIMESTAMP, rather than against
    'something arrived' — the lesson W3-9 paid for when it accepted an unrelated event that
    happened to land first."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        for ev in events(op):
            if not isinstance(ev, dict):
                continue
            if int(ev.get("at") or 0) < after:
                continue
            if predicate(ev):
                return ev
        time.sleep(1)
    return None


def main():
    sim = Sim()
    op = admin()

    # BADGE BEFORE ANYTHING IS PROVISIONED, on purpose. This is the state a commissioning engineer
    # is in: the reader is wired and answering, and nobody has added it yet. It is also the only
    # way the check further down means anything — asking "was an unenrolled reader misreported?"
    # after provisioning finds no such events and passes on an empty list.
    sim.run(GOOD_CARD)
    unenrolled = wait_for_event(op, lambda e: "no reader enrolled" in (e.get("detail") or ""),
                                timeout=45, after=int(time.time()) - 30)
    check("a badge on a reader nobody has enrolled is still recorded",
          unenrolled is not None,
          json.dumps(unenrolled)[:200] if unenrolled else "no event for an unenrolled reader")
    check("and it is NOT reported as a reader that is offline — the reader is answering",
          unenrolled is not None and (unenrolled.get("reason") or "") != "reader-offline",
          json.dumps({k: unenrolled.get(k) for k in ("reason", "detail")}) if unenrolled else "")
    sim.stop()

    provisioned_at = int(time.time())

    # ---- 1. provision a site -------------------------------------------------------------
    #
    # Creating a DOOR also creates its reader: the API takes the bus port and OSDP address and
    # writes both rows. There is no separate reader-create endpoint (readers are list-only),
    # which is worth knowing before hunting for one.
    r = op.post("/api/doors", {
        "name": "Bench Door", "class": "interior",
        "unlockSeconds": 5, "heldOpenSeconds": 30,
        "busPort": SIM_ADDR, "osdpAddress": READER_ADDR,
        "readerName": "Bench Reader", "requireSecureChannel": False,
    })
    door_id = rid(r)
    check("a door and its reader can be provisioned", bool(door_id), brief(r))
    if not door_id:
        return report()

    readers = result_list(op.get("/api/readers"), "readers", "items")
    check("the reader is enrolled against this bus and address",
          any(isinstance(x, dict) and x.get("osdpAddress") == READER_ADDR for x in readers),
          json.dumps(readers)[:200])


    # `ref` AND `name` are both required (the API decodes an entities.Holder directly);
    # `displayName` is not a field and the refusal says so.
    r = op.post("/api/holders", {"ref": "BENCH-001", "name": "Bench Holder", "kind": "staff"})
    holder_id = rid(r)
    check("a card holder can be created", bool(holder_id), brief(r))

    r = op.post("/api/holders/%d/credentials" % holder_id, {
        "kind": "card", "format": "wiegand26",
        "facilityCode": GOOD_FAC, "cardNumber": GOOD_NUM,
    })
    cred_id = rid(r)
    check("a card credential can be issued to that holder", bool(cred_id), brief(r))

    r = op.post("/api/groups", {"name": "Bench Group"})
    group_id = rid(r)
    check("an access group can be created", bool(group_id), brief(r))
    r = op.post("/api/groups/%d/members" % group_id, {"holderId": holder_id})
    check("the holder can be put in that group", r.status_code in (200, 201), brief(r))

    # A schedule that is open all week, so "outside the schedule" is never the reason a check
    # below fails for.
    # The 24/7 flag, not seven windows. This used to post `startMinute`/`endMinute` — the API
    # reads `startMin`/`endMin` — so every window arrived as 0-0 and was taken for one wrapping
    # past midnight, matching at every hour of every day. Every mypintusan bench written before
    # `bench_pintusan_schedules.py` was riding on that fail-open; a zero-length window is now
    # refused, so each of them had to say what it actually meant.
    r = op.post("/api/schedules", {"name": "Always", "always": True})
    sched_id = rid(r)
    check("an always-open schedule can be created", bool(sched_id), brief(r))

    r = op.post("/api/grants", {"groupId": group_id, "doorId": door_id, "scheduleId": sched_id})
    grant_id = rid(r)
    check("a grant can be created for that group at that door", bool(grant_id), brief(r))

    # ---- 2. the badge that should work ---------------------------------------------------
    t0 = int(time.time()) - 1
    sim.run(GOOD_CARD)
    granted = wait_for_event(op, lambda e: e.get("decision") == "granted", timeout=45, after=t0)
    check("a valid badge on a real OSDP bus GRANTS", granted is not None,
          json.dumps(granted)[:220] if granted else "no granted event in 45s")
    if granted:
        check("and the log names the holder rather than only the card",
              (granted.get("holderName") or "").strip() != "",
              json.dumps({k: granted.get(k) for k in ("holderName", "holderId", "reason")}))

    # ---- 3. a card nobody was issued -----------------------------------------------------
    t0 = int(time.time()) - 1
    sim.run(OTHER_CARD)
    unknown = wait_for_event(op, lambda e: e.get("decision") == "denied"
                             and "unknown" in (e.get("reason") or ""), timeout=45, after=t0)
    check("an unknown card is DENIED", unknown is not None,
          json.dumps(unknown)[:200] if unknown else "no unknown-credential denial in 45s")
    if unknown:
        check("and its raw value is still recorded, so a stranger at the door leaves a trace",
              (unknown.get("rawCredential") or unknown.get("rawCred") or "").strip() != "",
              json.dumps(unknown)[:200])

    # ---- 4. revocation, at the door -------------------------------------------------------
    #
    # THE QUESTION THAT FOUND TWO DEFECTS ON MYIDSAN, asked of a physical lock. A revoked card
    # must stop opening the door, and the delay matters: this is the control an operator reaches
    # for when a badge is lost.
    sim.run(GOOD_CARD)
    baseline = wait_for_event(op, lambda e: e.get("decision") == "granted",
                              timeout=45, after=int(time.time()) - 1)
    check("the card is granted again before it is revoked", baseline is not None,
          "" if baseline else "no grant to revoke against")

    r = op.post("/api/holders/%d/credentials/%d/revoke" % (holder_id, cred_id),
                {"reason": "bench: reported lost"})
    check("the credential can be revoked", r.status_code == 200, brief(r))
    revoked_at = int(time.time())
    denied = wait_for_event(op, lambda e: e.get("decision") == "denied"
                            and "revok" in (e.get("reason") or "") + (e.get("detail") or ""),
                            timeout=60, after=revoked_at)
    check("a REVOKED card stops opening the door", denied is not None,
          ("took %ds" % (int(denied.get("at")) - revoked_at)) if denied
          else "still granted 60s after revocation")
    if denied:
        check("and it is refused for the RIGHT reason, not a generic one",
              "revok" in (denied.get("reason") or ""),
              json.dumps({k: denied.get(k) for k in ("reason", "detail")}))
    # Nothing may be granted after the revocation.
    still = wait_for_event(op, lambda e: e.get("decision") == "granted", timeout=8, after=revoked_at + 2)
    # Conditioned on the card having been granted BEFORE the revocation: on a run where it never
    # worked at all, "nothing is granted afterwards" is true and means nothing.
    check("and nothing is granted on that card afterwards",
          baseline is not None and still is None,
          json.dumps(still)[:200] if still else ("no prior grant to compare against" if baseline is None else ""))

    # ---- 5. lockdown ----------------------------------------------------------------------
    r = op.post("/api/holders/%d/credentials" % holder_id, {
        "kind": "card", "format": "wiegand26",
        "facilityCode": DURESS_FAC, "cardNumber": DURESS_NUM,
        "pin": PIN, "duressPin": DURESS_PIN,
    })
    duress_cred = rid(r)
    check("a second credential with a PIN and a DURESS PIN can be issued", bool(duress_cred), brief(r))

    # The body key is `lockdown`, not `on`, and the response reports the RESULTING state. Assert
    # the state: the first run of this bench passed on a 200 whose body said `lockdown: false`,
    # having asked for something the API never parsed.
    r = op.post("/api/lockdown", {"lockdown": True})
    check("the site can be put into lockdown",
          r.status_code == 200 and (r.json().get("result") or {}).get("lockdown") is True, brief(r))
    t0 = int(time.time())
    sim.run(DURESS_CARD, pin=PIN)
    locked = wait_for_event(op, lambda e: e.get("decision") == "denied"
                            and "lockdown" in (e.get("reason") or ""), timeout=45, after=t0)
    check("a credential that would otherwise be granted is DENIED during lockdown",
          locked is not None, json.dumps(locked)[:200] if locked else "no lockdown denial in 45s")
    r = op.post("/api/lockdown", {"lockdown": False})
    check("lockdown can be lifted",
          r.status_code == 200 and (r.json().get("result") or {}).get("lockdown") is False, brief(r))

    # ---- 6. duress -------------------------------------------------------------------------
    #
    # A duress PIN must GRANT — the coercer must see the door open — and must raise a silent
    # alarm. The reader behaves identically by construction (the decision returns a normal grant);
    # what this checks is that the alarm actually fires and that the two events are not trivially
    # distinguishable to somebody watching the door open.
    t0 = int(time.time())
    sim.run(DURESS_CARD, pin=PIN)
    normal = wait_for_event(op, lambda e: e.get("decision") == "granted", timeout=45, after=t0)
    check("the PIN credential is granted with its NORMAL pin", normal is not None,
          json.dumps(normal)[:200] if normal else "no grant with the normal PIN")

    t0 = int(time.time())
    sim.run(DURESS_CARD, pin=DURESS_PIN)
    duress_ev = wait_for_event(op, lambda e: e.get("decision") == "granted"
                               and "duress" in (e.get("reason") or ""), timeout=45, after=t0)
    check("a DURESS pin also GRANTS — the door opens for a coerced holder",
          duress_ev is not None,
          json.dumps(duress_ev)[:220] if duress_ev else "no duress grant in 45s")
    if duress_ev:
        # WHAT THIS CAN AND CANNOT SHOW. The access event carries no strike duration, so an
        # earlier version of this check compared two absent fields and passed on `None == None`
        # — the fourth time in this bench series that a check passed on data that was not there.
        # Strike parity is structural rather than measurable here: Decide() returns
        # `s.Door.StrikeSeconds(holder.ExtendedUnlock)` on BOTH paths, with no duress branch, and
        # the unit tests pin that. What a live run CAN show is that the duress marker exists only
        # in operator-side fields — the decision itself is indistinguishable from a normal grant,
        # which is what somebody standing at the door sees.
        check("the duress entry is recorded as a plain GRANT, like any other",
              duress_ev.get("decision") == "granted"
              and (normal is None or normal.get("decision") == duress_ev.get("decision")),
              json.dumps({"duress": duress_ev.get("decision"),
                          "normal": (normal or {}).get("decision")}))
        check("and the duress marker lives only in fields an operator reads, not in the decision",
              duress_ev.get("duress") is True and duress_ev.get("reason") == "duress",
              json.dumps({k: duress_ev.get(k) for k in ("decision", "reason", "duress")}))

    # Alarms reach an operator as NOTIFICATIONS (services/alarm.go -> /api/notifications).
    # There is no /api/events/alarms; asking for one returns nothing, and the first run of this
    # bench then fell back to a substring match and reported a pass with no duress in the run at
    # all. Conditioned on the duress grant having actually happened.
    notes = result_list(op.get("/api/notifications?limit=50"), "notifications", "items")
    duress_alarm = [n for n in notes if isinstance(n, dict)
                    and "duress" in json.dumps(n).lower()]
    check("a duress entry raises an alarm an operator will see",
          duress_ev is not None and bool(duress_alarm),
          json.dumps(duress_alarm[:1])[:250] if duress_alarm
          else "notifications seen: %s" % json.dumps([n.get("title") or n.get("message") for n in notes[:6]])[:200])
    check("and the alarm is CRITICAL, not filed alongside routine traffic",
          bool(duress_alarm) and any(str(n.get("severity") or n.get("level") or "").lower()
                                     in ("critical", "alarm", "high") for n in duress_alarm),
          json.dumps(duress_alarm[:1])[:250])

    # ---- 7. the operator unlock -------------------------------------------------------------
    t0 = int(time.time())
    r = op.post("/api/doors/%d/unlock" % door_id, {"reason": "bench"})
    check("an operator can unlock the door remotely", r.status_code == 200, brief(r))
    op_ev = wait_for_event(op, lambda e: "operator" in json.dumps(e).lower(), timeout=30, after=t0)
    check("and the unlock is recorded with WHO did it", op_ev is not None,
          json.dumps(op_ev)[:220] if op_ev else "no operator event in 30s")

    # ---- 8. the log is the product ----------------------------------------------------------
    all_events = events(op, 100)
    kinds = set((e.get("decision") or "") for e in all_events if isinstance(e, dict))
    check("the access log holds both grants and denials from this run",
          {"granted", "denied"} <= kinds, "decisions seen: %s" % sorted(k for k in kinds if k))

    sim.stop()
    return report()


if __name__ == "__main__":
    code = 1
    try:
        code = main()
    finally:
        pass
    sys.exit(code)
