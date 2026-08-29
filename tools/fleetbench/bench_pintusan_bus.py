# Bench: bus faults, reconnect, and lockdown re-application.
#
# THE QUESTION. Every other mypintusan bench asked whether a DECISION was right. This one asks
# whether the wire under the decision survives the things that actually happen to an RS-485 segment
# in a building: a reader that says BUSY, one whose sequence numbers are skewed, one whose replies
# fail their CRC, one spewing junk, one that dies while its neighbours keep working, two brand-new
# readers fighting over factory address 0, and — the one that matters most — the adapter being
# unplugged and plugged back in.
#
# TWO PROPERTIES ARE WORTH MORE THAN THE REST:
#
#   (a) A SICK SEGMENT MUST NOT TAKE OUT A HEALTHY ONE. `one-down` and `slow` are the two shapes
#       of that: a reader that stops answering costs a full reply timeout on every round, and a
#       reader that answers just inside the timeout costs almost as much. Either can starve the
#       poll cadence of every other door on the cable, and the symptom — "badges sometimes take
#       ten seconds" — is the single hardest access-control complaint to diagnose from a desk.
#
#   (b) LOCKDOWN MUST BE RE-APPLIED ON RECONNECT. `runtime.superviseBus` builds a FRESH Bus and a
#       FRESH Controller on every re-dial, so a lockdown held only inside a controller dies with
#       the session — and unplugging a USB adapter silently UNSEALS the building. That was found
#       and fixed by booting the binary, and it is exactly the kind of fix that a later refactor
#       quietly undoes, because no unit test runs over a transport that can be torn down.
#
# WHY IT NEEDS A LIVE APPLIANCE. `bus_test.go` is 733 lines and genuinely good — but every one of
# its cases runs over an in-memory pipe that is never closed mid-run, which means the entire
# reconnect path (`failPort` -> `Run` returning -> `superviseBus` re-dialling -> lockdown carried
# into the new controller) has no test at all and cannot have one at that layer. Killing the
# simulator is the only way to ask the question, and it takes a real process to kill.
#
#   python tools/fleetbench/bench_pintusan_bus.py
import json
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from pintusan_harness import (
    SIM_ADDR,
    SIM_PORT,
    admin,
    boot,
    build_sim,
    start_sim,
    teardown,
)
from fleet_harness import result_list

# The alarm bodies this file READS are the product's own installer diagnostics, and they contain
# `120Ω`, `—` and other non-Latin-1 characters. A Windows console is cp1252, so printing one raises
# UnicodeEncodeError and the whole run dies mid-episode — which happened here, at exactly the check
# whose evidence is the Ω. A bench that cannot print what it found is not a bench.
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

# A card with VALID Wiegand-26 parity. #211 paid for this: the simulator's old default `deadbeef`
# fails leading even parity, so it could never open any door.
GOOD_CARD = "00880040"
# cardNumber is a STRING on this API, not an int. An int is accepted and stored and then NO
# credential matches: every decision comes back `unknown-credential`, which reads exactly like a
# security refusal and makes every negative check pass for the wrong reason.
GOOD_FAC, GOOD_NUM = 1, "4096"

R_UNKNOWN = "unknown-credential"
R_LOCKDOWN = "lockdown"

# `access.` categories that are not alarms: the routine decision stream and the administrative
# rule-change feed.
NOT_ALARMS = ("access.granted", "access.denied", "access.rule-change")

PASSES = []
FAILS = []


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


def result_of(r):
    try:
        body = r.json()
    except ValueError:
        return {}
    if not isinstance(body, dict):
        return {}
    res = body.get("result")
    return res if isinstance(res, dict) else body


# ---- the feeds ---------------------------------------------------------------------------------

def events(op, limit=120):
    return result_list(op.get("/api/events?limit=%d" % limit), "events", "items")


def notifications(op, limit=200):
    return result_list(op.get("/api/notifications?limit=%d" % limit), "notifications", "items")


def watermark(op):
    """Where this episode starts asking: (a CreatedAt second, the ids already in the feed).

    EVERY EPISODE IN THIS FILE SHARES A BOOT WHERE IT CAN, because a bus that has survived four
    faults in a row is a stronger statement than four buses that each survived one. The price is
    that the alarm feed accumulates, so each episode has to say WHEN it started asking — otherwise
    an alarm from three episodes ago satisfies this one's assertion.

    IT RETURNS BOTH HALVES BECAUSE `?since=` IS NOT WHAT IT LOOKS LIKE. `Service.ListSince` filters
    on **CreatedAt >= since** — a unix SECOND — not on the row id. Passing a max id (a small
    integer) therefore asks for everything since 1970, and the filter silently does nothing: the
    first version of this file did exactly that, and episode 7's "pulling the cable raises an alarm
    at all" was satisfied by the `secure-channel` notice from the handshake made while the cable
    was still plugged in. The id set is carried alongside because CreatedAt has one-second
    resolution, so a `>=` boundary either re-admits the rows that were already there or discards a
    genuine alarm raised in the same second."""
    rows = result_list(op.get("/api/notifications?limit=500"), "notifications", "items")
    ts = max([int(n.get("createdAt") or 0) for n in rows] or [0])
    return (ts, set(int(n.get("id") or 0) for n in rows))


def alarms_since(op, since, limit=400):
    """Every ALARM raised after `since`, keyed by kind.

    Anchored on the category the alarmer publishes (`access.` + kind), never on a substring: the
    rule-change feed says things like "Secure Channel Holder added to group", which matches a loose
    text filter and would report an alarm that never fired."""
    ts, seen = since
    rows = result_list(op.get("/api/notifications?since=%d&limit=%d" % (ts, limit)),
                       "notifications", "items")
    out = {}
    for n in rows:
        if not isinstance(n, dict):
            continue
        if int(n.get("id") or 0) in seen:
            continue
        cat = str(n.get("category") or "")
        if not cat.startswith("access.") or cat in NOT_ALARMS:
            continue
        out.setdefault(cat[len("access."):], []).append(n)
    return out


def wait_alarm(op, kind, since, timeout=45, match=None):
    """Wait for an alarm of `kind` raised after `since`, optionally satisfying `match`."""
    deadline = time.time() + timeout
    while True:
        for n in alarms_since(op, since).get(kind, []):
            if match is None or match(n):
                return n
        if time.time() >= deadline:
            return None
        time.sleep(1.5)


def data_of(note):
    """The alarm's structured context.

    It arrives as a JSON STRING in `metadata`, not as an object: `notification/store.go` serialises
    the Data map into a text column on the way in. A bench that reads `note["data"]` gets None from
    every notification ever raised and then finds nothing it looks for — the check passes or fails
    on an empty dict rather than on the product."""
    raw = (note or {}).get("metadata")
    if isinstance(raw, dict):
        return raw
    try:
        d = json.loads(raw or "{}")
    except ValueError:
        return {}
    return d if isinstance(d, dict) else {}


def body_of(note):
    return str((note or {}).get("body") or "")


def title_of(note):
    return str((note or {}).get("title") or "")


# ---- the simulator -----------------------------------------------------------------------------

class Sim(object):
    def __init__(self):
        self.p = None

    def run(self, scenario="happy", card=GOOD_CARD, every="3s", extra=None, settle=3.0):
        self.stop()
        self.p = start_sim(card=card, bits=26, every=every, scenario=scenario,
                           extra=list(extra or []))
        time.sleep(settle)
        return time.time()

    def stop(self):
        """Kill this simulator AND any survivor from a previous episode.

        THE SWEEP IS UNCONDITIONAL. #211 documented this trap and #219 reintroduced it within the
        hour by returning early when `self.p` was None; the previous episode's simulator then kept
        the port and KEPT BADGING, so checks "passed" on traffic from a reader not under test. In
        THIS file the sweep is also the fault injector — killing the simulator is how the adapter
        gets unplugged — so a stop that quietly does nothing would make the reconnect episodes
        assert that a bus which never went down came back."""
        if self.p:
            try:
                self.p.kill()
                self.p.wait(timeout=10)
            except Exception:
                pass
            self.p = None
        if os.name == "nt":
            subprocess.run(["taskkill", "/F", "/IM", "osdp-sim.exe"], capture_output=True, text=True)
        time.sleep(1.0)


# ---- the fixtures ------------------------------------------------------------------------------

def make_door(op, name, addr, klass="interior", extra=None):
    """Create a door (which creates its reader at `addr`) and return what the SERVER stored."""
    body = {"name": name, "class": klass, "busPort": SIM_ADDR,
            "osdpAddress": addr, "readerName": name + " Reader",
            "unlockSeconds": 5, "heldOpenSeconds": 30}
    body.update(extra or {})
    r = op.post("/api/doors", body)
    door_id = rid(r)
    if not door_id:
        return 0, {"error": brief(r)}
    return door_id, result_of(op.get("/api/doors/%d" % door_id))


def make_holder(op, ref, name):
    holder_id = rid(op.post("/api/holders", {"ref": ref, "name": name, "kind": "staff"}))
    op.post("/api/holders/%d/credentials" % holder_id, {
        "kind": "card", "format": "wiegand26",
        "facilityCode": GOOD_FAC, "cardNumber": GOOD_NUM,
    })
    return holder_id


class Site(object):
    """One holder, one group, an always-on schedule, and a door per OSDP address on the cable.

    An ALWAYS schedule deliberately: this file is about the wire, and a `out-of-schedule` denial
    arriving while a bus fault is under test would be indistinguishable from the fault. #222 is why
    the schedule is created through the API and not assumed — all four benches before it ran on an
    accidental 24/7 schedule they believed was office hours, because they posted field names the
    create handler does not read."""

    def __init__(self, op, addrs, name="Bus"):
        self.op = op
        self.holder_id = make_holder(op, "BUS-1", name + " Holder")
        self.group_id = rid(op.post("/api/groups", {"name": name + " Group"}))
        op.post("/api/groups/%d/members" % self.group_id, {"holderId": self.holder_id})
        self.schedule_id = rid(op.post("/api/schedules", {"name": "Always", "always": True}))
        self.doors = {}
        for a in addrs:
            door_id, stored = make_door(op, "%s Door %d" % (name, a), a)
            self.doors[a] = door_id
            if door_id:
                op.post("/api/grants", {"groupId": self.group_id, "doorId": door_id,
                                        "scheduleId": self.schedule_id})
            else:
                print("    !! door at address %d was not created: %s" % (a, stored.get("error")))

    def door(self, addr):
        return self.doors.get(addr, 0)

    def reader_id(self, addr):
        for rd in result_list(self.op.get("/api/readers"), "readers", "items"):
            if isinstance(rd, dict) and int(rd.get("osdpAddress") or -1) == addr:
                return int(rd.get("id") or 0)
        return 0

    def reader_row(self, addr):
        for rd in result_list(self.op.get("/api/readers"), "readers", "items"):
            if isinstance(rd, dict) and int(rd.get("osdpAddress") or -1) == addr:
                return rd
        return {}


# ---- badges ------------------------------------------------------------------------------------

def last_event_id(op):
    ids = [e.get("id") or 0 for e in events(op)]
    return max(ids) if ids else 0


def badge(op, door_id=0, timeout=45, since=None):
    """Wait for the NEXT badge decision after this call, so a decision taken before a fault was
    injected is never mistaken for one taken after it. The simulator badges unprompted."""
    if since is None:
        since = last_event_id(op)
    deadline = time.time() + timeout
    while True:
        for e in sorted(events(op), key=lambda x: x.get("id") or 0):
            if (e.get("id") or 0) <= since:
                continue
            if not (e.get("rawCredential") or ""):
                continue  # an operator unlock, not a badge
            if door_id and (e.get("doorId") or 0) != door_id:
                continue
            return e
        if time.time() >= deadline:
            return {}
        time.sleep(1.0)


def no_badge_for(op, door_id, seconds):
    """Watch a door for `seconds` and return every badge decision it took. The POSITIVE is what
    makes this meaningful: an empty list is also what a bench that forgot to start the simulator
    produces, so every caller below pairs it with a grant it has already observed."""
    since = last_event_id(op)
    deadline = time.time() + seconds
    seen = []
    while time.time() < deadline:
        for e in sorted(events(op), key=lambda x: x.get("id") or 0):
            if (e.get("id") or 0) <= since:
                continue
            if not (e.get("rawCredential") or ""):
                continue
            if door_id and (e.get("doorId") or 0) != door_id:
                continue
            seen.append(e)
            since = max(since, e.get("id") or 0)
        time.sleep(1.0)
    return seen


def describe(ev):
    if not ev:
        return "no decision reached the access log"
    return "%s/%s" % (ev.get("decision"), ev.get("reason"))


def granted(ev):
    return ev.get("decision") == "granted"


def denied_for(ev, reason):
    return ev.get("decision") == "denied" and ev.get("reason") == reason


def enrolled(ev):
    """Was the card RECOGNISED? `unknown-credential` reads exactly like a security refusal, so a
    bench that never checks it passes every negative check for the wrong reason."""
    return bool(ev) and ev.get("reason") != R_UNKNOWN


def sim_sockets():
    """How many TCP connections to the simulator's port the host currently holds open.

    Counted from netstat rather than from the app, because the whole point is a socket the app has
    LOST TRACK OF: it cannot report a connection it no longer has a reference to. The simulator's
    listening side still does."""
    try:
        out = subprocess.run(["netstat", "-an"], capture_output=True, text=True).stdout
    except Exception:
        return -1
    n = 0
    for line in out.splitlines():
        up = line.upper()
        if ":%d" % SIM_PORT in line and "ESTABLISHED" in up:
            n += 1
    return n


def alive(op):
    """Is the appliance still serving? Every fault episode below ends with this, because a decoder
    that panics on junk takes the API down with it and every later check would then fail for a
    reason that has nothing to do with what it claims to test."""
    try:
        return op.get("/api/auth/session").status_code == 200
    except Exception:
        return False


# ------------------------------------------------------------------------------------------------

def main():
    build_sim()
    sim = Sim()
    first = True
    try:
        # ---- 1. multidrop, then one-down: a sick reader must not take out a healthy one ----
        #
        # THE HEADLINE PROPERTY. Three readers on one cable, doors on all three, and then the
        # middle one dies. Every dead reader costs a full reply timeout on every round of the bus,
        # so the failure mode is not "door 2 stops working" — that is expected and correct — it is
        # "doors 1 and 3 stop working too, slowly enough that nobody connects it to reader 2".
        print("\n--- 1. three readers on one cable, and the middle one dies ---")
        sim.stop()
        boot(build_app=first, readers=[1, 2, 3])
        first = False
        op = admin()
        site = Site(op, [1, 2, 3], "Multidrop")
        check("a door can be created at each address on one cable",
              all(site.door(a) for a in (1, 2, 3)),
              "doors %s" % json.dumps(site.doors))

        mark = watermark(op)
        # -fault-after arms reader 2's death; the default is 15s, and a bench that badges before it
        # bites reads a healthy bus and reports a defect that is its own clock (#219 (iii)).
        sim.run("one-down", every="3s", extra=["-fault-after", "12s"])

        ev1 = badge(op, site.door(1))
        ev3 = badge(op, site.door(3))
        control = check("multidrop control: readers 1 and 3 both open their doors",
                        enrolled(ev1) and granted(ev1) and enrolled(ev3) and granted(ev3),
                        "door1 %s | door3 %s" % (describe(ev1), describe(ev3)))

        offline_note = wait_alarm(op, "reader-offline", mark, timeout=60,
                                  match=lambda n: int(data_of(n).get("readerId") or 0)
                                  == site.reader_id(2))
        check("the reader that died is alarmed offline, and named",
              offline_note is not None,
              "readerId %s; %s" % (site.reader_id(2), body_of(offline_note)[:120]))

        row2 = site.reader_row(2)
        check("the readers list agrees the dead one is offline",
              str(row2.get("tamperState") or "") == "offline",
              json.dumps({k: row2.get(k) for k in ("name", "tamperState", "lastSeenAt")}))

        # The real question, asked AFTER the fault: are the survivors still opening doors?
        t0 = time.time()
        ev1 = badge(op, site.door(1), timeout=40)
        ev3 = badge(op, site.door(3), timeout=40)
        check("a dead reader does not take the other doors on its cable down with it",
              control and enrolled(ev1) and granted(ev1) and enrolled(ev3) and granted(ev3),
              "door1 %s | door3 %s | %.1fs for both" % (describe(ev1), describe(ev3),
                                                        time.time() - t0))
        check("the survivors' own rows still read healthy",
              str(site.reader_row(1).get("tamperState") or "") == "ok"
              and str(site.reader_row(3).get("tamperState") or "") == "ok",
              "r1=%s r3=%s" % (site.reader_row(1).get("tamperState"),
                               site.reader_row(3).get("tamperState")))
        check("the appliance is still serving after a reader died on the bus", alive(op))

        # ---- 2. slow: a reader answering just inside the timeout ---------------------------
        #
        # The other half of (a), and the nastier half. A SILENT reader is at least obviously
        # broken; one that answers at 800 ms on a 1000 ms timeout is "working", and it eats the
        # slot budget of every door on the cable while looking perfectly healthy on the screen.
        print("\n--- 2. a slow reader must not starve the healthy one beside it ---")
        sim.stop()
        boot(build_app=False, readers=[1, 2], slot_millis=50, reply_timeout_millis=1000)
        op = admin()
        site = Site(op, [1, 2], "Slow")
        sim.run("slow", every="3s", extra=["-slow-reply", "800ms"])

        t0 = time.time()
        ev = badge(op, site.door(2), timeout=60)
        took = time.time() - t0
        check("the healthy reader still opens its door beside a slow one",
              enrolled(ev) and granted(ev), "%s after %.1fs" % (describe(ev), took))
        check("a slow reader is not declared offline while it answers inside the timeout",
              str(site.reader_row(1).get("tamperState") or "") == "ok",
              "reader 1 state %s" % site.reader_row(1).get("tamperState"))

        # Two badges in a row, timed: the cadence is what starvation would show up in, and one
        # sample cannot tell a slow bus from a card that happened to arrive between polls.
        t0 = time.time()
        badge(op, site.door(2), timeout=60)
        gap = time.time() - t0
        check("badges keep arriving on a cadence the cable can sustain",
              gap < 20.0, "next badge %.1fs later (card presented every 3s)" % gap)

        # ---- 3. busy: "ask me again" is not "I am broken" ----------------------------------
        print("\n--- 3. a reader that replies BUSY must be retried, not declared dead ---")
        sim.stop()
        boot(build_app=False, readers=[1])
        op = admin()
        site = Site(op, [1], "Busy")
        mark = watermark(op)
        sim.run("busy", every="3s")

        ev = badge(op, site.door(1), timeout=60)
        check("a badge is granted on a reader that keeps replying BUSY",
              enrolled(ev) and granted(ev), describe(ev))
        check("a BUSY reader is not alarmed offline",
              not alarms_since(op, mark).get("reader-offline"),
              json.dumps(sorted(alarms_since(op, mark).keys())))

        # ---- 4. bad-sequence: present, answering, and completely unusable -------------------
        #
        # The §3.1 trap. The reader is alive and talking, so nothing about it looks dead — and
        # until supervision was added, its failure counter was reset by every reply and it sat in
        # limbo forever with a door bound to it. Two things must be true: the door does not open,
        # and somebody is TOLD, in language that sends them to the right place.
        print("\n--- 4. a reader whose sequence numbers are skewed ---")
        sim.stop()
        boot(build_app=False, readers=[1])
        op = admin()
        site = Site(op, [1], "Sequence")
        mark = watermark(op)
        sim.run("bad-sequence", every="3s")

        seen = no_badge_for(op, site.door(1), 25)
        check("a reader that is present but unusable does not open its door",
              not any(granted(e) for e in seen),
              "%d decision(s): %s" % (len(seen), ", ".join(describe(e) for e in seen[:4])))

        offline_note = wait_alarm(op, "reader-offline", mark, timeout=45)
        check("a permanently out-of-sequence reader is eventually declared offline",
              offline_note is not None,
              body_of(offline_note)[:140] if offline_note else "no reader-offline alarm")

        raised = alarms_since(op, mark)
        sc_notes = raised.get("secure-channel") or []
        # THE LEAD FROM #220, SETTLED HERE. `EventFault` is raised as AlarmSecureChannel
        # UNCONDITIONALLY, so a skewed sequence number — a cabling or firmware fault with nothing
        # to do with encryption — reaches the operator titled "Reader secure channel fault" and
        # sends them hunting for a bus tap. Note this reader has NO secure channel configured at
        # all in this episode, so any such alarm is unambiguously a mislabel.
        check("a sequence fault is not reported to the operator as a secure channel fault",
              not sc_notes,
              "; ".join("%s / %s" % (title_of(n), body_of(n)[:90]) for n in sc_notes[:3]))
        check("the fault the operator is shown names the sequence problem",
              any("sequence" in body_of(n).lower()
                  for ns in raised.values() for n in ns),
              "alarms raised: %s" % json.dumps({k: len(v) for k, v in raised.items()}))
        check("the appliance is still serving after a skewed-sequence reader", alive(op))

        # ---- 5. bad-crc and garbage: the decoder, and the diagnosis -------------------------
        #
        # `awaitReply` distinguishes three faults that look identical from a distance — nothing on
        # the wire, wreckage on the wire, and well-formed frames failing CRC — because they send an
        # installer to three different places. That distinction is only worth writing if it
        # actually reaches the alarm an installer reads, which is what these two episodes check.
        print("\n--- 5a. every reply fails its CRC ---")
        sim.stop()
        boot(build_app=False, readers=[1])
        op = admin()
        site = Site(op, [1], "CRC")
        mark = watermark(op)
        sim.run("bad-crc", every="3s")

        note = wait_alarm(op, "reader-offline", mark, timeout=45)
        check("a reader whose frames all fail CRC is declared offline", note is not None,
              body_of(note)[:160] if note else "no reader-offline alarm")
        check("the CRC diagnosis reaches the operator, not just the debug log",
              note is not None and "crc" in body_of(note).lower(),
              body_of(note)[:200] if note else "")
        check("that diagnosis names termination, which is where the installer must look",
              note is not None and "termination" in body_of(note).lower(),
              body_of(note)[:200] if note else "")
        seen = no_badge_for(op, site.door(1), 12)
        check("no door opens on a cable whose frames cannot be trusted",
              not any(granted(e) for e in seen),
              "%d decision(s)" % len(seen))
        check("the appliance is still serving after a run of corrupted frames", alive(op))

        print("\n--- 5b. junk bytes with stray SOMs ---")
        sim.stop()
        boot(build_app=False, readers=[1])
        op = admin()
        site = Site(op, [1], "Garbage")
        mark = watermark(op)
        sim.run("garbage", every="3s")

        note = wait_alarm(op, "reader-offline", mark, timeout=45)
        check("a reader emitting junk is declared offline rather than hanging the poll loop",
              note is not None, body_of(note)[:160] if note else "no reader-offline alarm")
        check("the appliance survives frame resynchronisation on a junk-filled segment", alive(op))

        # ---- 6. addr-collision: the out-of-box case -----------------------------------------
        #
        # Two brand-new readers both at factory address 0, fighting on the wire. The CP cannot
        # resolve this — reader onboarding is designed for and not built — but it CAN say what is
        # wrong, and `awaitReply` claims it does: "likely two readers sharing address 0". An
        # installer who reads that fixes it in a minute; one who reads "no reply" spends an
        # afternoon on it.
        print("\n--- 6. two brand-new readers on factory address 0 ---")
        sim.stop()
        boot(build_app=False, readers=[0])
        op = admin()
        site = Site(op, [0], "Collision")
        check("a door can be created at the factory address 0", bool(site.door(0)),
              "door id %s" % site.door(0))
        mark = watermark(op)
        sim.run("addr-collision", every="3s")

        note = wait_alarm(op, "reader-offline", mark, timeout=60)
        check("an address collision is reported rather than looking like an empty address",
              note is not None, body_of(note)[:180] if note else "no reader-offline alarm")
        check("the alarm names the collision, so the installer is sent to the right fault",
              note is not None and "sharing address" in body_of(note).lower(),
              body_of(note)[:200] if note else "")
        check("the appliance is still serving after an address collision", alive(op))

        # ---- 7. reconnect: the bus comes back by itself --------------------------------------
        #
        # THE REGRESSION BENCH. `superviseBus` is the difference between a door controller and a
        # demo, and it was found only by restarting the simulator: the app never reconnected,
        # events simply stopped, and nothing in the logs said why. Nothing at the unit-test layer
        # can catch its removal, because the tests run over a pipe that is never torn down.
        print("\n--- 7. the adapter is unplugged and plugged back in ---")
        sim.stop()
        boot(build_app=False, readers=[1])
        op = admin()
        site = Site(op, [1], "Reconnect")
        mark = watermark(op)
        sim.run("happy", every="3s")

        ev = badge(op, site.door(1))
        reconnect_control = check("reconnect control: the door opens before the cable is pulled",
                                  enrolled(ev) and granted(ev), describe(ev))

        # RE-MARK HERE, not at the top of the episode. The first run of this file took its
        # watermark before the simulator started, so the `secure-channel` notice raised by the
        # INITIAL handshake (the sim holds no site key, so the reader correctly falls back to
        # cleartext) sat inside the window and satisfied "pulling the cable raises an alarm at
        # all" — a check that passed on an alarm raised while the cable was still plugged in.
        mark = watermark(op)
        sim.stop()   # the adapter comes out of the socket
        # THE FAULT WITH THE BIGGEST BLAST RADIUS, AND THE EASIEST ONE TO LEAVE SILENT. Per-reader
        # supervision cannot cover it: `failPort` ends the run on the very next slot — deliberately,
        # so the owner can re-dial — which is long before any reader has failed the three
        # transactions that declare it offline. So "the reader-offline alarm covers this too" is
        # exactly the assumption to check, and it is why this asks for ANY alarm first and only
        # then for the right one.
        deadline = time.time() + 45
        raised = {}
        while time.time() < deadline:
            raised = alarms_since(op, mark)
            if raised:
                break
            time.sleep(1.5)
        check("pulling the cable raises an alarm at all", bool(raised),
              "alarms since the cable came out: %s" % json.dumps({k: len(v) for k, v in
                                                                 raised.items()}))
        seg = (raised.get("bus-offline") or [None])[0]
        check("the alarm says the SEGMENT is down and names which cable",
              seg is not None and SIM_ADDR in body_of(seg),
              (title_of(seg) + " / " + body_of(seg)[:160]) if seg else
              "no bus-offline alarm; an operator is told which reader is dead but not that the "
              "whole cable is gone")
        row = site.reader_row(1)
        check("the readers list stops claiming the reader is healthy while the cable is out",
              str(row.get("tamperState") or "") == "offline",
              json.dumps({k: row.get(k) for k in ("tamperState", "lastSeenAt")}))

        t0 = time.time()
        sim.run("happy", every="3s", settle=0.5)   # and goes back in
        ev = badge(op, site.door(1), timeout=90)
        back = time.time() - t0
        check("the bus re-dials by itself and the door works again, with no restart",
              reconnect_control and enrolled(ev) and granted(ev),
              "%s after %.1fs" % (describe(ev), back))
        row = site.reader_row(1)
        check("the readers list shows the recovered reader healthy again",
              str(row.get("tamperState") or "") == "ok" and int(row.get("lastSeenAt") or 0) > 0,
              json.dumps({k: row.get(k) for k in ("tamperState", "lastSeenAt")}))

        # ---- 8. the re-dial does not get slower every time it is used ------------------------
        #
        # `superviseBus`'s backoff doubles on every attempt and is capped, which is right for an
        # adapter that is genuinely absent. The question is what happens to a bus that keeps
        # RECOVERING: a site whose gateway reboots nightly, or whose cable is momentarily knocked,
        # accumulates the backoff across a process lifetime rather than across an outage — and a
        # door that comes back then waits the cap before anybody can badge through it, having been
        # perfectly healthy for a week in between.
        print("\n--- 8. a bus that has recovered before must not come back slower each time ---")
        drops = 4
        for i in range(drops):
            sim.stop()
            time.sleep(1.5)
            sim.run("happy", every="3s", settle=0.5)
            # Wait for the segment to actually be back before dropping it again, or the loop just
            # measures the simulator's start-up rather than the app's re-dial.
            got = badge(op, site.door(1), timeout=120)
            print("    drop %d/%d: back with %s" % (i + 1, drops, describe(got)))

        # A healthy stretch: whatever the backoff has climbed to, a session that ran fine for this
        # long is the evidence that the outage is over.
        print("    letting the bus run healthy for 30s")
        time.sleep(30)

        sim.stop()
        time.sleep(1.5)
        t0 = time.time()
        sim.run("happy", every="3s", settle=0.5)
        ev = badge(op, site.door(1), timeout=120)
        after_many = time.time() - t0
        check("a door comes back promptly after its cable is knocked for the sixth time",
              enrolled(ev) and granted(ev) and after_many < 20.0,
              "%s after %.1fs (the first reconnect took %.1fs)" % (describe(ev), after_many, back))

        # ---- 9. lockdown MUST survive the reconnect ------------------------------------------
        #
        # THE ONE THAT UNSEALS A BUILDING. `runBus` builds a fresh Controller on every re-dial, so
        # a lockdown held only inside a controller dies with the session: an unplugged adapter,
        # or a gateway reboot, silently lifts the site's lockdown and the screen goes on saying it
        # is sealed. This is a regression bench on a fix, and it is worth its cost precisely
        # because the failure is invisible — the door just opens.
        print("\n--- 9. lockdown survives an unplugged adapter ---")
        r = op.post("/api/lockdown", {"lockdown": True})
        # A 200 can say `false`: the endpoint RETURNS the resulting state, and asserting the status
        # code alone is how a bench reports a lockdown it never actually applied.
        check("lockdown can be applied and the API reports the state it reached",
              result_of(r).get("lockdown") is True, brief(r))

        ev = badge(op, site.door(1), timeout=60)
        sealed = check("a badge is denied `lockdown` while the site is sealed",
                       enrolled(ev) and denied_for(ev, R_LOCKDOWN), describe(ev))

        sim.stop()
        time.sleep(3.0)
        check("the site still reports itself sealed while the bus is down",
              result_of(op.get("/api/lockdown")).get("lockdown") is True,
              brief(op.get("/api/lockdown")))

        sim.run("happy", every="3s", settle=0.5)
        ev = badge(op, site.door(1), timeout=120)
        check("a badge on the RECONNECTED bus is still denied `lockdown`",
              sealed and enrolled(ev) and denied_for(ev, R_LOCKDOWN),
              "%s — a grant here means unplugging an adapter unseals the building" % describe(ev))

        r = op.post("/api/lockdown", {"lockdown": False})
        check("lockdown can be lifted again", result_of(r).get("lockdown") is False, brief(r))
        ev = badge(op, site.door(1), timeout=90)
        check("the door opens once lockdown is lifted, proving the denial was the lockdown",
              enrolled(ev) and granted(ev), describe(ev))

        # ---- 10. a bus that cannot enrol a reader must not leak its socket ------------------
        #
        # `runBus` dials FIRST and enrols SECOND, and the Bus closes the port in `Run`'s defer and
        # nowhere else — so the one path that returns before Run has to close it by hand. A single
        # mistyped SCBK, or `requireSecureChannel` with no key, refuses every reader on the segment
        # and takes that path, every 1-30 s, for the life of the process.
        #
        # WHY IT IS WORSE THAN AN ORDINARY LEAK. The far end is usually a serial-to-Ethernet
        # gateway, and those commonly accept ONE to FOUR TCP clients. Filling its table with dead
        # sockets means that once the typo is corrected the segment still cannot be reached, and
        # the gateway has to be power-cycled by somebody standing next to it.
        print("")
        print("--- 10. a reader that cannot be enrolled must not leak a socket per re-dial ---")
        sim.stop()
        # requireSecureChannel with NO key: refused at enrolment, by design and correctly.
        boot(build_app=False, readers=[{"address": 1, "requireSecureChannel": True}],
             reader_scbk="")
        op = admin()
        sim.run("happy", every="3s", settle=2.0)
        baseline = sim_sockets()
        print("    sockets on :%d right after boot: %s" % (SIM_PORT, baseline))
        # Long enough for the backoff to walk 1+2+4+8+16 s and reach its cap several times over.
        time.sleep(75)
        held = sim_sockets()
        check("a segment whose readers all fail to enrol does not stack up dead connections",
              held >= 0 and held <= 2,
              "%s connection(s) to the simulator after 75s of re-dialling (was %s at boot)"
              % (held, baseline))

    finally:
        sim.stop()
        teardown()

    print("\n========= bus faults, reconnect and lockdown re-application =========")
    print("PASS %d / %d" % (len(PASSES), len(PASSES) + len(FAILS)))
    for f in FAILS:
        print("  FAIL", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
