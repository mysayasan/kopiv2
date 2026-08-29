# Bench: can a reader you cannot trust still open a door?
#
# THE CLAIM UNDER TEST. `Decide()` GATE 2 is the first runtime gate, and its comment says why:
#
#   "a reader we cannot trust makes every later check meaningless — the credential it reported may
#    not be the credential presented. There is no cleartext fallback: if the door requires a Secure
#    Channel and there is not one, the answer is no, and the reader is out of service."
#
# And `entities/door.go` states the threat in as many words: "A downgrade-to-plaintext fallback is
# exactly what an RS-485 tap wants, and 'the door kept working' is how it goes unnoticed for a year."
#
# That is the right design. It has never been run against a reader that actually refuses.
#
# WHY THIS IS ITEM 1. OSDP Secure Channel is the only thing standing between a screwdriver and a
# building. RS-485 is a two-wire bus in a ceiling void; with no SC, card numbers cross it in clear
# and replies replay. Every other gate in the decision path — the schedule, the grant, the
# revocation — is reasoning about a credential the reader TOLD it about, so all of them are worth
# exactly what the reader's word is worth.
#
# WHAT THE SIMULATOR MAKES POSSIBLE. tools/osdp-sim ships six Secure Channel scenarios and none has
# ever been driven: `secure` (a healthy session), `no-sc` / `refuse-sc` (a reader that will not),
# `sc-drop` (a session that dies MID-CONVERSATION on a reader already trusted and already bound to a
# live door), `wrong-key`, and `default-scbk` — a reader still on the well-known factory base key,
# which the simulator's own description says "must be capped at `interior` until rekeyed".
#
# EACH EPISODE IS A FRESH BOOT, and that is not laziness. A reader's key and its Secure Channel
# policy are seeded from config on FIRST BOOT ONLY and there is no API to change either afterwards,
# so "what happens when the site key is wrong" is a question you can only ask a new appliance.
#
#   python tools/fleetbench/bench_pintusan_securechannel.py
import io
import json
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from pintusan_harness import (
    BASE,
    READER_ADDR,
    SIM_ADDR,
    SITE_KEY,
    Client,
    admin,
    boot,
    build_sim,
    start_sim,
    teardown,
)
from fleet_harness import result_list

# A card that PASSES Wiegand-26 parity. #211 paid for this one: the simulator's old default
# `deadbeef` fails parity, so it could never open any door, and every grant check failed for a
# reason that had nothing to do with what was being tested.
GOOD_CARD = "00880040"
# cardNumber is a STRING on this API, not an int. An int is accepted and stored, and then no
# credential ever matches — every decision comes back `unknown-credential`, which reads exactly
# like a security refusal and makes every negative check in this file pass for the wrong reason.
GOOD_FAC, GOOD_NUM = 1, "4096"

# A key that is 16 valid hex bytes and is NOT the site key — for the wrong-key episode. The reader
# and the appliance each believe they are keyed; they simply disagree, which is what a mis-keyed
# reader looks like on a real site.
OTHER_KEY = "f0f1f2f3f4f5f6f7c0c1c2c3c4c5c6c7"

PASSES = []
FAILS = []


def check(name, ok, detail=""):
    (PASSES if ok else FAILS).append(name)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, ("  — " + detail) if detail else ""))
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


def events(op, limit=60):
    return result_list(op.get("/api/events?limit=%d" % limit), "events", "items")


def wait_for_decision(op, since, timeout=25):
    """Wait for an access decision recorded after `since`, and return it.

    Returns the event rather than a boolean on purpose: every check below wants the REASON, and
    'was there an event' is the question that passes when the answer is wrong."""
    deadline = time.time() + timeout
    fallback = None
    while time.time() < deadline:
        for e in events(op):
            at = e.get("at") or e.get("occurredAt") or 0
            if at < since or not (e.get("decision") or e.get("reason")):
                continue
            # A `reader-not-enrolled` event carries doorId 0: the badge arrived before the app had
            # finished identifying the reader, so it is a RACE, not a decision about this door.
            # Keep waiting for one that resolved a door, and only fall back to it if none comes.
            if (e.get("doorId") or 0) > 0:
                return e
            fallback = fallback or e
        time.sleep(1.0)
    return fallback


class Sim:
    """The simulator, restarted per scenario. It has no control channel — changing what a reader
    IS means running a different one."""

    def __init__(self):
        self.p = None

    def run(self, scenario, site_key=SITE_KEY, extra=None):
        self.stop()
        self.p = start_sim(card=GOOD_CARD, bits=26, every="2s", scenario=scenario,
                           extra=(["-site-key", site_key] if site_key else []) + list(extra or []))
        time.sleep(3.0)  # let the app dial, identify and (maybe) establish a session

    def stop(self):
        """Kill this simulator AND any survivor from a previous episode.

        THE SWEEP IS UNCONDITIONAL, and that is the whole point. An earlier version of this file
        returned early when `self.p` was None — which is true for a freshly constructed Sim — so a
        simulator left over from the previous episode was never swept, kept the port, and KEPT
        BADGING ITS OLD CARD over its old secure session. Every downgrade check then "passed" a
        grant that came from a reader that was not the one under test. #211 documented this trap
        and it was reintroduced within the hour; the lesson is that the sweep must not be
        conditional on this object believing it started something."""
        if self.p:
            try:
                self.p.kill()
                self.p.wait(timeout=10)
            except Exception:
                pass
            self.p = None
        # pkill does NOT reach this process on Windows.
        if os.name == "nt":
            subprocess.run(["taskkill", "/F", "/IM", "osdp-sim.exe"],
                           capture_output=True, text=True)
        time.sleep(1.0)  # let the port actually free before the next one binds


def provision(op, door_body):
    """Create one door (which creates its reader), a holder, a card, a group and an open schedule.

    Returned as a dict so an episode can assert on the door the server ACTUALLY stored, which is
    the whole point when the question is what a defaulted field became."""
    r = op.post("/api/doors", door_body)
    door_id = rid(r)
    if not door_id:
        return {"error": brief(r)}

    stored = (op.get("/api/doors/%d" % door_id).json() or {}).get("result") or {}

    r = op.post("/api/holders", {"ref": "SC-001", "name": "Secure Channel Holder", "kind": "staff"})
    holder_id = rid(r)
    op.post("/api/holders/%d/credentials" % holder_id, {
        "kind": "card", "format": "wiegand26", "facilityCode": GOOD_FAC, "cardNumber": GOOD_NUM,
    })
    r = op.post("/api/groups", {"name": "SC Group"})
    group_id = rid(r)
    op.post("/api/groups/%d/members" % group_id, {"holderId": holder_id})
    # The 24/7 flag, not seven windows. This used to post `startMinute`/`endMinute` — the API
    # reads `startMin`/`endMin` — so every window arrived as 0-0 and was taken for one wrapping
    # past midnight, matching at every hour of every day. Every mypintusan bench written before
    # `bench_pintusan_schedules.py` was riding on that fail-open; a zero-length window is now
    # refused, so each of them had to say what it actually meant.
    r = op.post("/api/schedules", {"name": "Always", "always": True})
    sched_id = rid(r)
    op.post("/api/grants", {"groupId": group_id, "doorId": door_id, "scheduleId": sched_id})
    return {"doorId": door_id, "door": stored, "holderId": holder_id}


def readers(op):
    return result_list(op.get("/api/readers"), "readers", "items")


def episode(scenario, door_body, site_key=SITE_KEY, reader_requires_sc=False,
            reader_scbk=None, build_app=False, sim_extra=None, settle=0.0):
    """One fresh appliance + one simulator scenario + one door policy, badged once.

    Returns (op, sim, provisioned, decision) so the caller asserts on the decision AND on the
    stored door — a defaulted field is only observable in what the server kept."""
    # Sweep BEFORE booting: a survivor from the previous episode would otherwise spend the whole
    # boot connected to the fresh appliance, badging a card from a reader that is not under test.
    Sim().stop()
    boot(reader_scbk=reader_scbk, reader_requires_sc=reader_requires_sc, build_app=build_app)
    op = admin()
    sim = Sim()
    sim.run(scenario, site_key=site_key, extra=sim_extra)
    prov = provision(op, door_body)
    if prov.get("error"):
        return op, sim, prov, None
    # `settle` exists for the TIME-BASED scenarios. `-fault-after` defaults to 15 SECONDS, and a
    # bench that badges six seconds in reads a session that has not dropped yet as a door that
    # opened on a dropped session — a defect report about the bench's own clock. The decision is
    # taken only from events recorded AFTER the fault has bitten.
    if settle:
        time.sleep(settle)
    since = int(time.time())
    time.sleep(1.0)
    decision = wait_for_decision(op, since)
    return op, sim, prov, decision


def reason_of(decision):
    return (decision or {}).get("reason") or ""


def credential_resolved(decision):
    """Did the appliance RECOGNISE the card at all?

    Every refusal below is only a security statement if the credential was known. An
    `unknown-credential` denial means the bench failed to enrol the card, and reading that as
    "the door correctly refused" is how a whole file passes while testing nothing. Conditioning
    on this is the suite's oldest lesson, applied to its most dangerous app."""
    return reason_of(decision) != "unknown-credential"


def granted(decision):
    d = (decision or {}).get("decision")
    if isinstance(d, str):
        return d.lower() in ("granted", "grant", "allow", "allowed")
    return bool((decision or {}).get("granted"))


# --------------------------------------------------------------------------------------------

def main():
    build_sim()
    first = True
    sim = None
    try:
        # ---- 1. THE POSITIVE: an encrypted session works ---------------------------------
        #
        # First, and load-bearing. Every refusal below is only meaningful if a HEALTHY secure
        # reader opens the door — otherwise "the door did not open" is just a broken bench.
        print("\n--- a healthy Secure Channel session ---")
        op, sim, prov, dec = episode(
            "secure",
            {"name": "SC Door", "class": "interior", "busPort": SIM_ADDR,
             "osdpAddress": READER_ADDR, "readerName": "SC Reader",
             "requireSecureChannel": True},
            build_app=first)
        first = False
        check("a door that REQUIRES an encrypted session can be provisioned",
              bool(prov.get("doorId")), prov.get("error") or "door %s" % prov.get("doorId"))
        check("...and a card on a reader WITH a secure session opens it",
              granted(dec), json.dumps(dec)[:220] if dec else "no decision recorded")

        # ---- 2. THE DOWNGRADE: a reader that will not do Secure Channel -------------------
        for scenario, label in (("no-sc", "never offers"), ("refuse-sc", "actively refuses")):
            print("\n--- a reader that %s Secure Channel, on a door that REQUIRES it ---" % label)
            op, sim, prov, dec = episode(
                scenario,
                {"name": "SC Door", "class": "interior", "busPort": SIM_ADDR,
                 "osdpAddress": READER_ADDR, "readerName": "SC Reader",
                 "requireSecureChannel": True})
            check("[%s] the enrolled card was RECOGNISED (so a refusal means something)" % scenario,
                  credential_resolved(dec),
                  "reason=%r — an unknown-credential denial is a BENCH fault, not a refusal"
                  % reason_of(dec))
            check("[%s] a card on a downgraded reader does NOT open a door that requires SC"
                  % scenario,
                  dec is not None and credential_resolved(dec) and not granted(dec),
                  json.dumps(dec)[:220] if dec else "NO DECISION AT ALL — the badge vanished")
            check("[%s] ...and it is refused for the SECURE CHANNEL reason, not a vaguer one"
                  % scenario,
                  "secure" in reason_of(dec).lower(),
                  "reason=%r" % reason_of(dec))

        # ---- 3. the documented low-security case still works ------------------------------
        #
        # The guard must not be a blanket no: a door that does NOT require SC is entitled to work
        # with a reader that cannot do it. Without this the checks above could pass on an app that
        # simply never opens anything.
        print("\n--- the same downgraded reader, on a door that does NOT require SC ---")
        op, sim, prov, dec = episode(
            "no-sc",
            {"name": "Open Door", "class": "interior", "busPort": SIM_ADDR,
             "osdpAddress": READER_ADDR, "readerName": "Open Reader",
             "requireSecureChannel": False})
        check("a door that does not require SC still works with a reader that cannot do it",
              granted(dec), json.dumps(dec)[:220] if dec else "no decision")

        # ---- 4. THE DEFAULT: what a CRITICAL door gets when nobody says ------------------
        #
        # entities/door.go: "RequireSecureChannel defaults on for perimeter and critical." Its
        # neighbours in the create handler all get defaults — UnlockSeconds, HeldOpenSeconds,
        # OfflinePolicy — so this asks whether the one SECURITY-relevant default is applied too.
        # It matters more than the others because there is no PUT /api/doors: a door created with
        # the wrong policy keeps it forever.
        for cls in ("critical", "perimeter"):
            print("\n--- a %s door created WITHOUT saying anything about Secure Channel ---" % cls)
            op, sim, prov, dec = episode(
                "no-sc",
                {"name": "%s Door" % cls.title(), "class": cls, "busPort": SIM_ADDR,
                 "osdpAddress": READER_ADDR, "readerName": "%s Reader" % cls.title()})
            stored = prov.get("door") or {}
            check("[%s] the stored door records the documented default (SC required)" % cls,
                  bool(stored.get("requireSecureChannel")),
                  "requireSecureChannel=%r on a %s door — door.go says it defaults on for "
                  "perimeter and critical" % (stored.get("requireSecureChannel"), cls))
            check("[%s] ...and a card on a plaintext reader does NOT open it" % cls,
                  dec is not None and credential_resolved(dec) and not granted(dec),
                  json.dumps(dec)[:200] if dec else "no decision")

        # ---- 5. WRONG KEY: both sides believe they are keyed, and disagree ----------------
        print("\n--- the reader and the appliance hold DIFFERENT site keys ---")
        op, sim, prov, dec = episode(
            "secure",
            {"name": "Keyed Door", "class": "interior", "busPort": SIM_ADDR,
             "osdpAddress": READER_ADDR, "readerName": "Keyed Reader",
             "requireSecureChannel": True},
            site_key=OTHER_KEY, reader_requires_sc=True)
        check("a key mismatch does not silently become a cleartext session",
              dec is None or (credential_resolved(dec) and not granted(dec)),
              json.dumps(dec)[:220] if dec else "no grant — the reader never came into service")
        # Anchored to the ALARM, not to any notification mentioning the word: the access
        # rule-change feed says "Secure Channel Holder added to group" and matched a loose filter,
        # which would have reported an alarm that never fired.
        alarms = [n for n in result_list(op.get("/api/notifications?limit=50"), "notifications", "items")
                  if "secure-channel" in json.dumps(n).lower()
                  or "reader-offline" in json.dumps(n).lower()
                  or (n.get("category") or "").startswith("access.alarm")]
        check("...and it is ALARMED, not merely logged",
              bool(alarms), json.dumps(alarms[0])[:200] if alarms else "no secure-channel alarm")

        # ---- 6. SC-DROP: the session dies mid-conversation --------------------------------
        #
        # The harder half of the pair, in the PD's own words: "refusing a handshake is caught at
        # enrolment, but losing a session mid-conversation happens to a reader that is already
        # trusted and already bound to a live door."
        print("\n--- an established session that DROPS mid-conversation ---")
        op, sim, prov, dec = episode(
            "sc-drop",
            {"name": "Drop Door", "class": "interior", "busPort": SIM_ADDR,
             "osdpAddress": READER_ADDR, "readerName": "Drop Reader",
             "requireSecureChannel": True},
            sim_extra=["-fault-after", "3s"], settle=9.0)
        check("a session that drops mid-life does not leave the door opening on cleartext",
              dec is None or (credential_resolved(dec) and not granted(dec)),
              json.dumps(dec)[:220] if dec else "no grant after the session dropped")

        # ---- 7. DEFAULT SCBK: a reader still on the factory key ---------------------------
        #
        # The simulator's own description of this scenario states the intended rule: "reader still
        # on the well-known default base key — MUST BE CAPPED AT `interior` UNTIL REKEYED". The
        # reader reports SCBK-D through PDCAP so that rule can be enforced. This asks whether any
        # of it is implemented.
        print("\n--- a reader still on the well-known FACTORY base key ---")
        op, sim, prov, dec = episode(
            "default-scbk",
            {"name": "Factory Door", "class": "critical", "busPort": SIM_ADDR,
             "osdpAddress": READER_ADDR, "readerName": "Factory Reader",
             "requireSecureChannel": True})
        rs = readers(op)
        states = [r.get("scbkState") for r in rs]
        print("    reader SCBK states as the app reports them:", json.dumps(states))
        # A GAP, stated rather than asserted into a permanent red. `Reader.ScbkState` is written
        # once at creation as "default" and never updated: `ScbkRekeyed` and `ScbkFailed` are
        # declared in entities/reader.go and assigned NOWHERE, and the CP never sends CmdCap, so
        # the PDCAP report the simulator makes ("still on SCBK-D") is never asked for. The
        # simulator's own description of this scenario names the rule that is missing: such a
        # reader "must be capped at `interior` until rekeyed".
        #
        # What IS checkable today is the direction of the error, and it is the safe one: the app
        # never claims a reader has been rekeyed when nothing has established that.
        print("    GAP: scbkState is permanently 'default' for every reader — the column cannot "
              "distinguish a factory-keyed reader from a properly keyed one, and no trust cap "
              "is applied. See the PR narrative.")
        check("the appliance never CLAIMS a reader is rekeyed without evidence",
              all((st or "") != "rekeyed" for st in states),
              "scbkState=%s" % states)
        check("...and a factory-keyed reader is not trusted for a CRITICAL door",
              dec is None or (credential_resolved(dec) and not granted(dec)),
              json.dumps(dec)[:220] if dec else "no grant")

    finally:
        if sim:
            sim.stop()

    print("\n%d/%d checks passed" % (len(PASSES), len(PASSES) + len(FAILS)))
    for f in FAILS:
        print("  FAILED:", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
