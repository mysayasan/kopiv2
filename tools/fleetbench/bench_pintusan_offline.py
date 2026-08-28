# Bench: offline mode and the cache TTL — does the door actually stop trusting a stale replica?
#
# THE QUESTION. `Decide()`'s GATE 10 is the app's answer to the oldest attack on an access control
# system, and the code says so in its own words:
#
#     "Past the TTL the door denies. There is no allow-all option anywhere in this path:
#      'fail open on network loss' is a documented attack — cut the uplink, walk in."
#
# `docs/MYPINTUSAN_DATA_MODEL.md` §2 turns that into a table an installer can read: interior serves
# from cache for 72h, perimeter for 24h and raises a degraded-mode alert immediately, critical for
# 8h and ONLY for holders flagged `OfflineAllowed`. Past TTL the door denies.
#
# Every line of that is implemented in `Decide()`. This file asks the only question that matters
# about it: on a running appliance, can any of it actually happen?
#
# WHY IT NEEDS A LIVE APPLIANCE. GATE 10 is a pure function over a `Snapshot`, and its unit tests
# are good — `TestOfflineDenialDoesNotMaskTheRealReason` even pins the gate ordering. But every one
# of those tests BUILDS the snapshot it wants: it sets `s.CacheAge = 100 * time.Hour` by hand and
# then checks the gate. Nothing in a unit test can tell you where `CacheAge` comes from on a real
# controller, or whether the door's `OfflinePolicy` can be set to the value the gate is looking for.
# That is exactly the shape #220 found in the alarms — `TestBusTamperSurfaces` passed for the life
# of the driver by handing the bus the LSTAT itself — and it is why this file exists.
#
# THE POSITIVE CONTROL, and it is load-bearing. Offline mode changes almost nothing you can see: a
# cached grant looks exactly like a live grant. So episode 3 badges at a CRITICAL door with a holder
# who is not flagged `OfflineAllowed` and requires the `offline-not-allowed` denial. That denial can
# only come from inside GATE 10. Until it is observed, "the TTL did not deny" and "the gate never
# ran at all" are the same observation, and the whole file would be measuring nothing.
#
#   python tools/fleetbench/bench_pintusan_offline.py
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
# fails leading even parity, so it could never open any door.
GOOD_CARD = "00880040"
# cardNumber is a STRING on this API, not an int. An int is accepted and stored and then NO
# credential matches: every decision comes back `unknown-credential`, which reads exactly like a
# security refusal and makes every negative check pass for the wrong reason.
GOOD_FAC, GOOD_NUM = 1, "4096"

# The reasons GATE 10 can produce. Named here so a check can say which branch fired rather than
# "denied", which is what every other gate also produces.
R_STALE = "offline-cache-expired"
R_DENIED = "offline-denied"
R_NOT_ALLOWED = "offline-not-allowed"
R_REVOKED = "credential-revoked"

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


def events(op, limit=80):
    return result_list(op.get("/api/events?limit=%d" % limit), "events", "items")


def notifications(op, limit=100):
    return result_list(op.get("/api/notifications?limit=%d" % limit), "notifications", "items")


class Sim:
    """The simulator, restarted per episode."""

    def __init__(self):
        self.p = None

    def run(self, scenario="happy", card=GOOD_CARD, every="3s", extra=None):
        self.stop()
        self.p = start_sim(card=card, bits=26, every=every, scenario=scenario,
                           extra=list(extra or []))
        time.sleep(3.0)

    def stop(self):
        """Kill this simulator AND any survivor from a previous episode.

        THE SWEEP IS UNCONDITIONAL. #211 documented this trap, #219 reintroduced it within the hour
        by returning early when `self.p` was None, and the previous episode's simulator then kept
        the port and KEPT BADGING — so every check "passed" on traffic from a reader that was not
        under test."""
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


def make_door(op, name, klass="interior", extra=None):
    """Create a door (which creates its reader) and return what the SERVER stored.

    The stored row, not the request, because half of what this bench measures is fields the create
    handler drops on the floor: a 200 here says the door exists, not that it has the policy that
    was asked for."""
    body = {"name": name, "class": klass, "busPort": SIM_ADDR,
            "osdpAddress": READER_ADDR, "readerName": name + " Reader",
            "unlockSeconds": 5, "heldOpenSeconds": 30}
    body.update(extra or {})
    r = op.post("/api/doors", body)
    door_id = rid(r)
    if not door_id:
        return 0, {"error": brief(r)}
    return door_id, result_of(op.get("/api/doors/%d" % door_id))


def make_holder(op, ref, name, extra=None):
    body = {"ref": ref, "name": name, "kind": "staff"}
    body.update(extra or {})
    r = op.post("/api/holders", body)
    holder_id = rid(r)
    if not holder_id:
        return 0, {"error": brief(r)}
    return holder_id, result_of(op.get("/api/holders/%d" % holder_id))


def enrol(op, holder_id, door_id, group_name="Offline Group"):
    """Card, group, always-open schedule, grant. Returns the credential id so it can be revoked."""
    r = op.post("/api/holders/%d/credentials" % holder_id, {
        "kind": "card", "format": "wiegand26",
        "facilityCode": GOOD_FAC, "cardNumber": GOOD_NUM,
    })
    cred_id = rid(r)
    group_id = rid(op.post("/api/groups", {"name": group_name}))
    op.post("/api/groups/%d/members" % group_id, {"holderId": holder_id})
    sched_id = rid(op.post("/api/schedules", {
        "name": "Always",
        "windows": [{"weekday": d, "startMinute": 0, "endMinute": 1439} for d in range(7)],
    }))
    op.post("/api/grants", {"groupId": group_id, "doorId": door_id, "scheduleId": sched_id})
    return cred_id


def wait_for_decision(op, since=0, timeout=45, door_id=0):
    """Wait for the next badge DECISION after `since` and return the whole event.

    Returns the event, never a boolean. The reason is the entire subject of this bench: a denial
    for `unknown-credential` means the card was never enrolled, and reading that as a security
    decision is how a whole file passes while proving nothing."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        for e in sorted(events(op), key=lambda x: x.get("id") or 0):
            if (e.get("id") or 0) <= since:
                continue
            if not (e.get("rawCredential") or ""):
                continue  # an operator unlock, not a badge
            if door_id and (e.get("doorId") or 0) != door_id:
                continue
            return e
        time.sleep(1.5)
    return {}


def last_event_id(op):
    ids = [e.get("id") or 0 for e in events(op)]
    return max(ids) if ids else 0


def badge(op, door_id=0, timeout=45):
    """Wait for the NEXT badge after this call, so a decision from before a state change is never
    mistaken for one after it. The simulator badges every 3s, unprompted."""
    return wait_for_decision(op, since=last_event_id(op), timeout=timeout, door_id=door_id)


def describe(ev):
    if not ev:
        return "no decision reached the access log"
    return "%s/%s (offline=%s)" % (ev.get("decision"), ev.get("reason"), ev.get("offline"))


# ------------------------------------------------------------------------------------------------

def main():
    build_sim()
    first = True
    sim = None
    ttl_denial_observed = False
    try:
        # ---- 1. ONLINE: the control for the whole file --------------------------------------
        #
        # Everything below is a denial or a grant under offline mode. This episode establishes that
        # the card, the reader, the grant and the schedule are all good with GATE 10 switched OFF,
        # so that nothing later is confusable with a bench that failed to enrol a badge.
        print("\n--- 1. online: the badge works, and the decision says it was not from cache ---")
        Sim().stop()
        boot(build_app=first, offline=False)
        first = False
        op = admin()
        sim = Sim()
        sim.run("happy")
        door_id, door = make_door(op, "Control Door", "interior")
        holder_id, _ = make_holder(op, "OFF-001", "Control Holder")
        enrol(op, holder_id, door_id)
        ev = badge(op, door_id)
        online_ok = check("online: a valid badge is granted",
                          ev.get("decision") == "granted", describe(ev))
        check("online: the decision is not marked as served from cache",
              online_ok and not ev.get("offline"), describe(ev))

        # ---- 2. OFFLINE, interior: the cache serves, and says so ----------------------------
        #
        # The good half of the design: a network blip must not strand staff outside a building.
        print("\n--- 2. offline, interior: the cached grant still opens the door ---")
        sim.stop()
        boot(build_app=False, offline=True)
        op = admin()
        sim.run("happy")
        door_id, door = make_door(op, "Cached Door", "interior")
        holder_id, _ = make_holder(op, "OFF-002", "Cached Holder")
        enrol(op, holder_id, door_id)
        ev = badge(op, door_id)
        check("offline: an interior door still grants from cache",
              ev.get("decision") == "granted", describe(ev))
        # The flag on the RECORD is the only place an operator can later see that a decision was
        # taken without a live source of truth. If it is false here, the access log cannot answer
        # "what did this door do while it was cut off?".
        check("offline: the access log marks the decision as served from cache",
              bool(ev.get("offline")), describe(ev))

        # ---- 3. THE POSITIVE CONTROL FOR GATE 10 --------------------------------------------
        #
        # A critical door, and a holder NOT flagged OfflineAllowed. `offline-not-allowed` is
        # produced by GATE 10 and by nothing else in the app, so observing it is the proof that the
        # offline rules are being evaluated on this controller at all. Without this, every "the TTL
        # did not fire" below would be indistinguishable from "offline mode is not switched on".
        print("\n--- 3. offline, critical: the gate is reached (positive control) ---")
        sim.stop()
        boot(build_app=False, offline=True)
        op = admin()
        sim.run("happy")
        crit_id, crit = make_door(op, "Critical Door", "critical",
                                  {"requireSecureChannel": False})
        holder_id, _ = make_holder(op, "OFF-003", "Ordinary Holder")
        enrol(op, holder_id, crit_id)
        ev = badge(op, crit_id)
        gate_reached = check(
            "offline: a critical door denies a holder who is not offline-allowed",
            ev.get("decision") == "denied" and ev.get("reason") == R_NOT_ALLOWED, describe(ev))
        check("offline: GATE 10 is evaluated on a live controller (positive control)",
              gate_reached,
              "without this every check below measures a gate that never ran")

        # ---- 4. The escape hatch the critical row depends on --------------------------------
        #
        # The data model's critical row is "cache serves ONLY holders explicitly flagged
        # OfflineAllowed". A flag that cannot be set turns that row into "critical doors deny
        # everyone while offline, permanently" — fail-closed, but it bricks the door.
        print("\n--- 4. offline, critical: the offline-allowed holder gets through ---")
        sim.stop()
        boot(build_app=False, offline=True)
        op = admin()
        sim.run("happy")
        crit_id, _ = make_door(op, "Critical Door", "critical", {"requireSecureChannel": False})
        holder_id, holder = make_holder(op, "OFF-004", "Trusted Holder",
                                        {"offlineAllowed": True})
        stored_flag = bool(holder.get("offlineAllowed"))
        check("a holder can be created flagged offlineAllowed", stored_flag,
              json.dumps(holder)[:160])
        enrol(op, holder_id, crit_id)
        ev = badge(op, crit_id)
        check("offline: an offline-allowed holder opens a critical door",
              stored_flag and ev.get("decision") == "granted", describe(ev))

        # ---- 5. THE TTL ---------------------------------------------------------------------
        #
        # The headline. A door is asked for a 2-second offline TTL; the bench then badges well past
        # it. "Past the TTL the door denies" is the sentence under test, and a door whose TTL cannot
        # be set to anything a bench can outlive is a door whose TTL has never been exercised on any
        # install — the class defaults are 8, 24 and 72 HOURS.
        print("\n--- 5. offline: does the cache ever go stale? ---")
        sim.stop()
        boot(build_app=False, offline=True)
        op = admin()
        sim.run("happy")
        ttl_id, ttl_door = make_door(op, "TTL Door", "interior", {"offlineTtlSeconds": 2})
        stored_ttl = int(ttl_door.get("offlineTtlSeconds") or 0)
        check("a door's offline cache TTL can be set through the API", stored_ttl == 2,
              "stored offlineTtlSeconds=%s" % stored_ttl)
        holder_id, _ = make_holder(op, "OFF-005", "Stale Holder")
        cred_id = enrol(op, holder_id, ttl_id)
        # Well past a 2s TTL, and past the class default only if the TTL took. Either way the
        # controller has been offline since boot, which is what a cache age has to be measured from.
        time.sleep(20)
        ev = badge(op, ttl_id)
        ttl_denial_observed = check(
            "offline: past the TTL the door denies (offline-cache-expired)",
            ev.get("decision") == "denied" and ev.get("reason") == R_STALE, describe(ev))

        # THE GATE ORDERING. GATE 10 runs LAST among the denials so that a credential that would
        # have been refused anyway is refused for the REAL reason — `offline-cache-expired` on a
        # card revoked last week sends an operator to investigate the network instead of the
        # revocation.
        #
        # This check is only meaningful if the TTL denial above actually happened. If the cache can
        # never expire, then "revoked wins over stale" is true for the empty reason that stale never
        # competes — and a check that passes on an empty result is not a check. So it is reported as
        # UNPROVABLE rather than allowed to pass.
        op.post("/api/holders/%d/credentials/%d/revoke" % (holder_id, cred_id),
                {"status": "revoked", "reason": "bench"})
        ev = badge(op, ttl_id)
        if ttl_denial_observed:
            check("offline: a revoked card is denied for `revoked`, not for the stale cache",
                  ev.get("decision") == "denied" and ev.get("reason") == R_REVOKED, describe(ev))
        else:
            check("offline: a revoked card is denied for `revoked`, not for the stale cache",
                  False,
                  "cannot be established: the cache never expires, so nothing competes with "
                  "revocation — observed %s" % describe(ev))

        # ---- 6. The deny policy -------------------------------------------------------------
        #
        # `OfflinePolicy` is documented as cached | deny, and `deny` is the setting for the door
        # that must not open at all on a controller running from cache.
        print("\n--- 6. offline: the deny-while-offline policy ---")
        sim.stop()
        boot(build_app=False, offline=True)
        op = admin()
        sim.run("happy")
        deny_id, deny_door = make_door(op, "Deny Door", "interior", {"offlinePolicy": "deny"})
        stored_policy = str(deny_door.get("offlinePolicy") or "")
        check("a door can be created with offlinePolicy=deny", stored_policy == "deny",
              "stored offlinePolicy=%r" % stored_policy)
        holder_id, _ = make_holder(op, "OFF-006", "Deny Holder")
        enrol(op, holder_id, deny_id)
        ev = badge(op, deny_id)
        check("offline: a deny-policy door refuses the badge (offline-denied)",
              ev.get("decision") == "denied" and ev.get("reason") == R_DENIED, describe(ev))

        # The promise in the entity's own comment: there is no allow-all, by design. Asking for one
        # must not produce a door that serves everything from cache forever. This must hold both
        # before and after any fix — it is the invariant, not the gap.
        allow_id, allow_door = make_door(op, "Allow Door", "interior",
                                         {"offlinePolicy": "allow-all",
                                          "osdpAddress": READER_ADDR + 9})
        allow_policy = str(allow_door.get("offlinePolicy") or "")
        # Two ways to be right, and the check names both: the request is REFUSED outright, or a door
        # exists with a policy that is not fail-open. What it must never be is a door that was
        # created believing it had the setting it asked for. Asserting only "the policy is not
        # allow-all" would also pass on a request that never reached the server at all, so the
        # refusal has to be visible as a refusal.
        refused = allow_id == 0 and "400" in str(allow_door.get("error") or "")
        check("an allow-all offline policy cannot be created",
              refused or (allow_id > 0 and allow_policy in ("cached", "deny")),
              "id=%s policy=%r %s" % (allow_id, allow_policy, allow_door.get("error") or ""))

        # ---- 7. Can a site turn offline mode on at all? -------------------------------------
        #
        # `access.offline` is not on the Settings screen. The settings API is documented as the way
        # anything changes after first boot ("config.json only ever SEEDS the first boot"), so this
        # asks whether the flag can be flipped there and whether flipping it reaches the running
        # controller — a setting that persists but does nothing is worse than one that is absent,
        # because the screen then reports a protection the door is not applying.
        print("\n--- 7. offline: can an operator turn it on after installation? ---")
        sim.stop()
        boot(build_app=False, offline=False)
        op = admin()
        sim.run("happy")
        crit_id, _ = make_door(op, "Settings Door", "critical", {"requireSecureChannel": False})
        holder_id, _ = make_holder(op, "OFF-007", "Settings Holder")
        enrol(op, holder_id, crit_id)
        ev = badge(op, crit_id)
        base_ok = check("settings: the critical door grants while online (control)",
                        ev.get("decision") == "granted", describe(ev))

        current = result_of(op.get("/api/settings/access"))
        current["offline"] = True
        saved = op.put("/api/settings/access", current)
        read_back = result_of(op.get("/api/settings/access"))
        check("settings: offline mode can be turned on through the settings API",
              saved.status_code == 200 and bool(read_back.get("offline")),
              "%s; read back offline=%s" % (brief(saved), read_back.get("offline")))
        time.sleep(5)
        ev = badge(op, crit_id)
        check("settings: turning offline mode on reaches the running controller",
              base_ok and ev.get("decision") == "denied" and ev.get("reason") == R_NOT_ALLOWED,
              describe(ev))

        # ---- 8. The degraded-mode alert -----------------------------------------------------
        #
        # `docs/MYPINTUSAN_DATA_MODEL.md` §2: a perimeter door offline "raises a degraded-mode alert
        # immediately". A controller serving a whole site from cache is exactly the condition an
        # operator has to be told about, and it is the one condition nobody can see from a screen —
        # a cached grant looks identical to a live one.
        print("\n--- 8. offline: is anybody told the site is running degraded? ---")
        sim.stop()
        boot(build_app=False, offline=True)
        op = admin()
        sim.run("happy")
        peri_id, _ = make_door(op, "Perimeter Door", "perimeter", {"requireSecureChannel": False})
        holder_id, _ = make_holder(op, "OFF-008", "Perimeter Holder")
        enrol(op, holder_id, peri_id)
        ev = badge(op, peri_id)
        served = check("offline: the perimeter door serves the cached grant",
                       ev.get("decision") == "granted", describe(ev))
        cats = set()
        degraded_note = None
        for n in notifications(op):
            if not isinstance(n, dict):
                continue
            cat = str(n.get("category") or "")
            cats.add(cat)
            if cat == "access.degraded":
                degraded_note = n
        check("offline: an alert says the controller is running from cache",
              served and degraded_note is not None,
              "notification categories seen: %s" % sorted(cats))

    finally:
        if sim:
            sim.stop()
        else:
            Sim().stop()
        teardown()

    print("\n================ offline mode and the cache TTL ================")
    print("PASS %d / %d" % (len(PASSES), len(PASSES) + len(FAILS)))
    for f in FAILS:
        print("  FAIL", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
