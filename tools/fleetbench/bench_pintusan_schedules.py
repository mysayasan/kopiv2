# Bench: schedules, holidays, the night shift and the site timezone — GATE 8.
#
# THE QUESTION. `Decide()`'s GATE 8 decides whether a grant is in force RIGHT NOW, and it is the
# only gate whose answer changes without anybody touching the system. `Snapshot.Location` exists
# because the plan doc says, in its own words, that "a night shift that runs 22:00-06:00 and a
# public holiday are both wrong if evaluated in UTC" — and `windowCovers` carries the warning that
# "the obvious start <= now && now <= end comparison denies every one of those holders for the whole
# of their shift".
#
# All of that is implemented, and the unit tests for it are good. This file asks the only question
# a unit test cannot: on a running appliance, with the rules entered through the API an operator
# actually has, does any of it reach the door?
#
# WHY IT NEEDS A LIVE APPLIANCE. `scheduleAllows` is a pure function over a `Snapshot`, and every
# one of its tests BUILDS the snapshot it wants — it sets `s.Location`, `s.Windows` and `s.Holiday`
# by hand and then checks the gate. Nothing in a unit test can tell you whether the window an
# operator types is the window the gate is handed, whether the site's timezone reaches the
# controller, or whether a holiday can be scoped to the site whose doors it is meant to close.
# That is the shape #219, #220 and #221 all found on this app: the gate was right, and nothing
# could reach it.
#
# HOW THE CLOCK IS DRIVEN, because this is the part that makes the file possible at all. A bench
# cannot wait until 02:00 to ask whether 02:00 is inside a 22:00-06:00 window. It does not have to:
# the site timezone is a config value, and IANA zones span 26 hours of offset, so for any instant
# there is a real zone in which the wall clock is already at any hour you want. Every episode below
# picks a FIXED-OFFSET zone that puts the site's clock where the question lives, and then asks the
# app. That is not a simulation of the night shift — it is the night shift, somewhere.
#
#   python tools/fleetbench/bench_pintusan_schedules.py
import datetime
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

R_OUT = "out-of-schedule"
R_HOLIDAY = "holiday"
R_UNKNOWN = "unknown-credential"

# Zones with a FIXED offset, one per whole hour from -11 to +14.
#
# FIXED IS LOAD-BEARING. A bench that picks Europe/London and then computes "local = UTC + 0" is
# wrong for half the year, and the failure reads as the app shifting everyone's hours. Every zone
# here observes no daylight saving, so the offset in this table is the offset the container's
# tzdata will apply, today and in six months.
FIXED_ZONES = [
    (-11, "Pacific/Niue"), (-10, "Pacific/Honolulu"), (-9, "Pacific/Gambier"),
    (-8, "Pacific/Pitcairn"), (-7, "America/Phoenix"), (-6, "America/Regina"),
    (-5, "America/Panama"), (-4, "America/Puerto_Rico"),
    (-3, "America/Argentina/Buenos_Aires"), (-2, "America/Noronha"),
    (-1, "Atlantic/Cape_Verde"), (0, "UTC"), (1, "Africa/Lagos"),
    (2, "Africa/Johannesburg"), (3, "Africa/Nairobi"), (4, "Asia/Dubai"),
    (5, "Asia/Karachi"), (6, "Asia/Dhaka"), (7, "Asia/Bangkok"),
    (8, "Asia/Kuala_Lumpur"), (9, "Asia/Tokyo"), (10, "Australia/Brisbane"),
    (11, "Pacific/Guadalcanal"), (12, "Pacific/Tarawa"), (13, "Pacific/Kanton"),
    (14, "Pacific/Kiritimati"),
]

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


# ---- the clock ---------------------------------------------------------------------------------

class Site(object):
    """One choice of site timezone, and what the wall clock reads there right now."""

    def __init__(self, offset, name, local):
        self.offset, self.name, self.local = offset, name, local

    @property
    def weekday(self):
        """Sunday-first, the way `time.Weekday` and `ScheduleWindow.Weekday` count."""
        return (self.local.weekday() + 1) % 7

    def day(self, delta=0):
        return ((self.local + datetime.timedelta(days=delta)).weekday() + 1) % 7

    def date(self, delta=0):
        return (self.local + datetime.timedelta(days=delta)).strftime("%Y-%m-%d")

    def __str__(self):
        return "%s (UTC%+d), local %s" % (self.name, self.offset,
                                          self.local.strftime("%a %Y-%m-%d %H:%M"))


def site_at_hour(lo, hi, margin=10):
    """A real zone in which the site's wall clock is already between lo:00 and hi:00.

    `margin` keeps the clock that many minutes clear of BOTH ends of the range, because an episode
    takes a minute or two to run and a bench that starts at 21:59:30 and badges at 22:00:10 will
    report whichever answer it happens to catch. The zone furthest from either edge wins."""
    now = datetime.datetime.utcnow()
    best, best_slack = None, -1
    for off, name in FIXED_ZONES:
        local = now + datetime.timedelta(hours=off)
        mins = local.hour * 60 + local.minute
        if not (lo * 60 <= mins < hi * 60):
            continue
        slack = min(mins - lo * 60, hi * 60 - mins)
        if slack < margin:
            continue
        if slack > best_slack:
            best, best_slack = Site(off, name, local), slack
    if best is None:
        raise SystemExit("no fixed-offset zone puts the wall clock in [%d:00,%d:00) — "
                         "impossible unless the table above was edited" % (lo, hi))
    return best


def site_on_another_date():
    """A zone whose LOCAL calendar date is not today's UTC date.

    Always available: past 10:00 UTC, +14 has already turned over; before it, -11 has not yet."""
    now = datetime.datetime.utcnow()
    off, name = (14, "Pacific/Kiritimati") if now.hour >= 10 else (-11, "Pacific/Niue")
    return Site(off, name, now + datetime.timedelta(hours=off))


# ---- the simulator -----------------------------------------------------------------------------

class Sim(object):
    def __init__(self):
        self.p = None

    def run(self, scenario="happy", card=GOOD_CARD, every="3s", extra=None):
        self.stop()
        self.p = start_sim(card=card, bits=26, every=every, scenario=scenario,
                           extra=list(extra or []))
        time.sleep(3.0)

    def stop(self):
        """Kill this simulator AND any survivor from a previous episode.

        THE SWEEP IS UNCONDITIONAL. #211 documented this trap and #219 reintroduced it within the
        hour by returning early when `self.p` was None; the previous episode's simulator then kept
        the port and KEPT BADGING, so checks "passed" on traffic from a reader not under test."""
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

def make_door(op, name, klass="interior", extra=None):
    """Create a door (which creates its reader) and return what the SERVER stored.

    The stored row, not the request: a 200 says the door exists, not that it kept the fields it was
    given. That distinction is the whole of #221's second defect."""
    body = {"name": name, "class": klass, "busPort": SIM_ADDR,
            "osdpAddress": READER_ADDR, "readerName": name + " Reader",
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


def make_group(op, holder_id, name="Schedule Group"):
    group_id = rid(op.post("/api/groups", {"name": name}))
    op.post("/api/groups/%d/members" % group_id, {"holderId": holder_id})
    return group_id


def make_schedule(op, name, windows=None, always=False):
    """Create a schedule and return (id, the raw response).

    Windows are (weekday, startMin, endMin) in minutes from midnight, Sunday-first."""
    body = {"name": name, "always": always,
            "windows": [{"weekday": d, "startMin": a, "endMin": b} for d, a, b in (windows or [])]}
    r = op.post("/api/schedules", body)
    return rid(r), r


def all_week(start, end):
    return [(d, start, end) for d in range(7)]


class Rules(object):
    """One door, one badge holder, one group — and a grant that can be repointed at a new schedule.

    Repointing rather than re-enrolling is deliberate: it holds the card, the reader, the door and
    the group constant, so that when two schedules give two different answers the schedule is the
    only thing that changed. It also exercises DELETE, which a bench that only ever creates never
    does."""

    def __init__(self, op, name, door_extra=None, klass="interior"):
        self.op = op
        self.door_id, self.door = make_door(op, name, klass, door_extra)
        self.holder_id = make_holder(op, "SCH-%d" % (self.door_id or 1), name + " Holder")
        self.group_id = make_group(op, self.holder_id, name + " Group")
        self.grant_id = 0

    def use(self, schedule_id):
        if self.grant_id:
            self.op.delete("/api/grants/%d" % self.grant_id)
            self.grant_id = 0
        self.grant_id = rid(self.op.post("/api/grants", {
            "groupId": self.group_id, "doorId": self.door_id, "scheduleId": schedule_id}))
        return self.grant_id


def last_event_id(op):
    ids = [e.get("id") or 0 for e in events(op)]
    return max(ids) if ids else 0


def badge(op, door_id=0, timeout=45):
    """Wait for the NEXT badge decision after this call, so a decision taken before a rule change is
    never mistaken for one taken after it. The simulator badges every 3s, unprompted."""
    since = last_event_id(op)
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


def describe(ev):
    if not ev:
        return "no decision reached the access log"
    return "%s/%s" % (ev.get("decision"), ev.get("reason"))


def granted(ev):
    return ev.get("decision") == "granted"


def denied_for(ev, reason):
    return ev.get("decision") == "denied" and ev.get("reason") == reason


def enrolled(ev):
    """Was the card RECOGNISED? #219's first trap: `unknown-credential` reads exactly like a
    security refusal, so a bench that never checks it passes every negative check for the wrong
    reason — the card was simply never enrolled."""
    return bool(ev) and ev.get("reason") != R_UNKNOWN


def holiday(op, date, behaviour="deny", name="Bench Holiday", site_id=None):
    body = {"name": name, "date": date, "behaviour": behaviour}
    if site_id is not None:
        body["siteId"] = site_id
    r = op.post("/api/schedules/holidays", body)
    return rid(r), r


def clear_holidays(op):
    for h in result_list(op.get("/api/schedules/holidays"), "holidays", "items"):
        if isinstance(h, dict) and h.get("id"):
            op.delete("/api/schedules/holidays/%d" % h["id"])


# ------------------------------------------------------------------------------------------------

def main():
    build_sim()
    first = True
    sim = None
    try:
        # ---- 1. The control: an office-hours window, and a denial that is really a denial ----
        #
        # Everything below turns on being able to tell "inside the window" from "outside" it. This
        # episode establishes both against the same card, the same door and the same group, so no
        # later result can be confused with a bench that failed to enrol a badge.
        site = site_at_hour(10, 15)
        print("\n--- 1. office hours: the control for the whole file ---")
        print("    site:", site)
        Sim().stop()
        boot(build_app=first, timezone=site.name)
        first = False
        op = admin()
        sim = Sim()
        sim.run("happy")

        r = Rules(op, "Office Door")
        office, resp = make_schedule(op, "Office Hours", all_week(9 * 60, 17 * 60))
        stored = (result_of(resp).get("windows") or [{}])[0]
        check("a weekly window survives the round trip through the API",
              int(stored.get("startMin") or -1) == 540 and int(stored.get("endMin") or -1) == 1020,
              "stored %s" % json.dumps(stored)[:120])
        r.use(office)
        ev = badge(op, r.door_id)
        check("a badge inside the window is granted", enrolled(ev) and granted(ev), describe(ev))

        night_only, _ = make_schedule(op, "Small Hours", all_week(60, 2 * 60))
        r.use(night_only)
        ev = badge(op, r.door_id)
        check("a badge outside every window is denied `out-of-schedule`",
              enrolled(ev) and denied_for(ev, R_OUT), describe(ev))

        # ---- 2. The night shift, from the evening side --------------------------------------
        #
        # 22:00-06:00 is one window whose end is BEFORE its start. The evening half is the easy
        # half — it lands on the window's own weekday — but it has to be established before the
        # morning half means anything.
        site = site_at_hour(22, 24)
        print("\n--- 2. the night shift, 22:00-06:00, from the evening side ---")
        print("    site:", site)
        sim.stop()
        boot(build_app=False, timezone=site.name)
        op = admin()
        sim.run("happy")

        r = Rules(op, "Night Door")
        tonight, _ = make_schedule(op, "Night Shift", [(site.weekday, 22 * 60, 6 * 60)])
        r.use(tonight)
        ev = badge(op, r.door_id)
        check("22:00-06:00 grants at %s on its own weekday" % site.local.strftime("%H:%M"),
              enrolled(ev) and granted(ev), describe(ev))

        # The same window on TOMORROW's weekday must NOT cover tonight. Without this, "the wrap
        # works" is indistinguishable from "a wrapped window matches everything" — which is exactly
        # what a zero-length window does today (episode 4).
        tomorrow, _ = make_schedule(op, "Night Shift (tomorrow)", [(site.day(1), 22 * 60, 6 * 60)])
        r.use(tomorrow)
        ev = badge(op, r.door_id)
        check("the same window on tomorrow's weekday does not open the door tonight",
              enrolled(ev) and denied_for(ev, R_OUT), describe(ev))

        # ---- 3. The night shift, from the morning side --------------------------------------
        #
        # THE ONE THE PLAN DOC WARNS ABOUT. At 02:00 the holder is inside the window that began at
        # 22:00 YESTERDAY, and a naive start <= now <= end comparison denies them for the whole of
        # the rest of their shift. This is the check that costs a real person a night in a car park.
        site = site_at_hour(1, 5)
        print("\n--- 3. the night shift, from the morning side (the wrapped tail) ---")
        print("    site:", site)
        sim.stop()
        boot(build_app=False, timezone=site.name)
        op = admin()
        sim.run("happy")

        r = Rules(op, "Small Hours Door")
        yesterday, _ = make_schedule(op, "Night Shift", [(site.day(-1), 22 * 60, 6 * 60)])
        r.use(yesterday)
        ev = badge(op, r.door_id)
        check("22:00-06:00 still grants at %s, the morning after it opened"
              % site.local.strftime("%H:%M"),
              enrolled(ev) and granted(ev), describe(ev))

        today, _ = make_schedule(op, "Night Shift (today)", [(site.weekday, 22 * 60, 6 * 60)])
        r.use(today)
        ev = badge(op, r.door_id)
        check("the wrapped tail belongs to the day the window opened, not to this one",
              enrolled(ev) and denied_for(ev, R_OUT), describe(ev))

        # ---- 4. The zero-length window: the one way a schedule fails OPEN -------------------
        #
        # A window whose end equals its start is not a 24-hour window, and nobody types one meaning
        # 24 hours. It falls into the wrapping branch, where `minutes >= startMin` is true from the
        # first minute of the day — so it matches EVERY hour of EVERY day.
        #
        # This is not hypothetical. `bench_pintusan_offline.py`, in this same directory, posts its
        # windows as `startMinute`/`endMinute` while the API reads `startMin`/`endMin`; every window
        # it has ever created was 0-0, and it has been granting 24/7 while reading in the source as
        # a 09:00-17:00 schedule. A typo produced permanent access at every door and nothing said
        # so. Same clock as episode 3: the small hours, comfortably outside any office window.
        site = site_at_hour(1, 5)
        print("\n--- 4. a window that starts and ends at the same minute ---")
        print("    site:", site)
        sim.stop()
        boot(build_app=False, timezone=site.name)
        op = admin()
        sim.run("happy")

        r = Rules(op, "Zero Window Door")
        zero_id, zero_resp = make_schedule(op, "Zero Length", all_week(9 * 60, 9 * 60))
        refused = zero_id == 0 and zero_resp.status_code == 400
        check("a window that ends at the minute it starts is refused", refused, brief(zero_resp))
        if not refused:
            r.use(zero_id)
            ev = badge(op, r.door_id)
            check("a zero-length window does not silently mean 24/7",
                  enrolled(ev) and not granted(ev), describe(ev))
        else:
            check("a zero-length window does not silently mean 24/7", True,
                  "the API refused to create one")

        # The same defect through the door an operator is far likelier to come in by: a client that
        # sends the wrong field names. Go's decoder drops unknown fields silently, so the windows
        # arrive as 0-0 — and the "a schedule needs a window" guard passes, because there ARE
        # windows. The result must not be a 24/7 schedule created by accident.
        mistyped = op.post("/api/schedules", {
            "name": "Mistyped", "always": False,
            "windows": [{"weekday": d, "startMinute": 9 * 60, "endMinute": 17 * 60}
                        for d in range(7)]})
        mistyped_id = rid(mistyped)
        if mistyped_id:
            r.use(mistyped_id)
            ev = badge(op, r.door_id)
            check("a schedule whose window fields did not parse does not open the door all day",
                  enrolled(ev) and not granted(ev), describe(ev))
        else:
            check("a schedule whose window fields did not parse does not open the door all day",
                  mistyped.status_code == 400, brief(mistyped))

        # ---- 5. The site timezone decides, and it must be correctable ----------------------
        #
        # One instant, one rule, two sites. `Snapshot.Location` exists for exactly this and its
        # pure-function test passes today — but nothing in a unit test says whether the zone an
        # operator types on the Settings screen, or in the FIRST-RUN WIZARD, ever reaches the
        # controller that is deciding.
        inside = site_at_hour(10, 15)
        outside = site_at_hour(1, 5)
        print("\n--- 5. the same rule, the same instant, two site timezones ---")
        print("    inside :", inside)
        print("    outside:", outside)
        sim.stop()
        boot(build_app=False, timezone=inside.name)
        op = admin()
        sim.run("happy")
        r = Rules(op, "Zone Door")
        office, _ = make_schedule(op, "Office Hours", all_week(9 * 60, 17 * 60))
        r.use(office)
        ev = badge(op, r.door_id)
        zone_in_ok = check("09:00-17:00 grants at a site where it is %s"
                           % inside.local.strftime("%H:%M"),
                           enrolled(ev) and granted(ev), describe(ev))

        sim.stop()
        boot(build_app=False, timezone=outside.name)
        op = admin()
        sim.run("happy")
        r = Rules(op, "Zone Door")
        office, _ = make_schedule(op, "Office Hours", all_week(9 * 60, 17 * 60))
        r.use(office)
        ev = badge(op, r.door_id)
        zone_out_ok = check("the same rule denies at a site where it is %s — the timezone decided"
                            % outside.local.strftime("%H:%M"),
                            enrolled(ev) and denied_for(ev, R_OUT), describe(ev))

        # AND NOW THE CORRECTION. A site whose timezone was set wrong at install is the ordinary
        # case here: the setup wizard asks for it on its first screen, before any door exists, and
        # the Settings screen offers it as a plain text field. Fixing it has to reach the controller
        # that is refusing people at the door.
        current = result_of(op.get("/api/settings/access"))
        current["timezone"] = inside.name
        saved = op.put("/api/settings/access", current)
        read_back = result_of(op.get("/api/settings/access"))
        check("settings: the site timezone can be corrected through the API",
              saved.status_code == 200 and read_back.get("timezone") == inside.name,
              "%s; read back %r" % (brief(saved), read_back.get("timezone")))
        time.sleep(5)
        ev = badge(op, r.door_id)
        check("settings: correcting the site timezone reaches the running controller",
              zone_in_ok and zone_out_ok and enrolled(ev) and granted(ev), describe(ev))

        # ---- 6. The holiday calendar -------------------------------------------------------
        #
        # A holiday is the one rule change that shuts a building without anybody editing a grant.
        # All three behaviours are exercised against the SAME door and card, so the behaviour is
        # the only thing that varies.
        site = site_at_hour(10, 15)
        print("\n--- 6. the holiday calendar: deny, ignore, follow-sunday ---")
        print("    site:", site)
        sim.stop()
        boot(build_app=False, timezone=site.name)
        op = admin()
        sim.run("happy")

        r = Rules(op, "Holiday Door")
        office, _ = make_schedule(op, "Office Hours", all_week(9 * 60, 17 * 60))
        r.use(office)
        ev = badge(op, r.door_id)
        control_ok = check("holiday control: the door grants on an ordinary day",
                           enrolled(ev) and granted(ev), describe(ev))

        hid, hresp = holiday(op, site.date(), "deny", "Bench Deny Day")
        check("a holiday can be created for today in site-local time", hid > 0, brief(hresp))
        ev = badge(op, r.door_id)
        deny_ok = check("a deny holiday closes the door, and says `holiday`",
                        control_ok and enrolled(ev) and denied_for(ev, R_HOLIDAY), describe(ev))

        # Deleting it must REOPEN the building. A calendar that can only ever close is a calendar
        # nobody dares use, and a holiday entered on the wrong date has to be undoable.
        op.delete("/api/schedules/holidays/%d" % hid)
        ev = badge(op, r.door_id)
        check("deleting the holiday reopens the door",
              deny_ok and enrolled(ev) and granted(ev), describe(ev))

        holiday(op, site.date(), "ignore", "Bench Ignore Day")
        ev = badge(op, r.door_id)
        check("an `ignore` holiday leaves the day exactly as it was",
              deny_ok and enrolled(ev) and granted(ev), describe(ev))
        clear_holidays(op)

        # follow-sunday is the behaviour for a day that runs on a Sunday timetable. Two checks, and
        # each is only meaningful because the other exists: a schedule with NO Sunday window must
        # close, and a Sunday-ONLY schedule must open.
        if site.weekday == 0:
            for name in ("a follow-sunday holiday runs the Sunday timetable",
                         "...and a Sunday-only schedule opens on a follow-sunday holiday"):
                check(name, False,
                      "cannot be established: it is already Sunday at %s, so the rewritten "
                      "weekday and the real one are the same day" % site.name)
        else:
            no_sunday, _ = make_schedule(op, "Weekdays Only",
                                         [(d, 9 * 60, 17 * 60) for d in range(1, 7)])
            r.use(no_sunday)
            ev = badge(op, r.door_id)
            weekday_ok = enrolled(ev) and granted(ev)
            holiday(op, site.date(), "follow-sunday", "Bench Sunday Day")
            ev = badge(op, r.door_id)
            check("a follow-sunday holiday runs the Sunday timetable",
                  weekday_ok and enrolled(ev) and denied_for(ev, R_OUT), describe(ev))

            sunday_only, _ = make_schedule(op, "Sundays Only", [(0, 9 * 60, 17 * 60)])
            r.use(sunday_only)
            ev = badge(op, r.door_id)
            check("...and a Sunday-only schedule opens on a follow-sunday holiday",
                  enrolled(ev) and granted(ev), describe(ev))

        # THE OTHER HALF OF A RULE CHANGE. #220's lesson, generalised: after proving a rule takes
        # effect, check what the operator can see about it afterwards. A holiday closes every door
        # on the site, and `access.rule-change` is the only record this app keeps of who changed
        # what — the access log records DECISIONS, not edits.
        notes = [n for n in notifications(op) if isinstance(n, dict)]
        bodies = " | ".join(str(n.get("body") or "") for n in notes
                            if n.get("category") == "access.rule-change")
        check("creating and removing a holiday is recorded as a rule change",
              "Bench Deny Day" in bodies and "Bench Sunday Day" in bodies,
              "rule-change bodies: %s" % (bodies[:200] or
                                          "none; categories %s"
                                          % sorted({str(n.get("category")) for n in notes})))
        clear_holidays(op)

        # ---- 7. A holiday is a calendar DAY, in the site's own zone ------------------------
        #
        # `Holiday.Date` is a string rather than a timestamp because "a public holiday is a calendar
        # day, not an instant, and it must not shift with UTC offset". The way to test that claim is
        # to run at a site whose local date is not today's UTC date, and enter both.
        site = site_on_another_date()
        utc_date = datetime.datetime.utcnow().strftime("%Y-%m-%d")
        print("\n--- 7. the holiday is resolved on the SITE's calendar, not the server's ---")
        print("    site:", site, "| UTC date", utc_date)
        sim.stop()
        boot(build_app=False, timezone=site.name)
        op = admin()
        sim.run("happy")

        r = Rules(op, "Calendar Door")
        # A 24/7 schedule, so the only thing that can deny is the calendar — which also makes this
        # the check that a deny holiday overrides an always-on grant. That ordering is why the
        # holiday test sits ABOVE the `Always` short-circuit in `scheduleAllows`.
        always, _ = make_schedule(op, "Always", always=True)
        r.use(always)
        ev = badge(op, r.door_id)
        always_ok = check("calendar control: a 24/7 schedule grants",
                          enrolled(ev) and granted(ev), describe(ev))

        hid, _ = holiday(op, utc_date, "deny", "Wrong Day")
        ev = badge(op, r.door_id)
        check("a holiday on the SERVER's date does not close a site that is not on that date",
              always_ok and enrolled(ev) and granted(ev),
              "%s; site date %s, UTC date %s" % (describe(ev), site.date(), utc_date))
        op.delete("/api/schedules/holidays/%d" % hid)

        holiday(op, site.date(), "deny", "Right Day")
        ev = badge(op, r.door_id)
        check("a holiday on the SITE's date closes it, even against a 24/7 schedule",
              always_ok and enrolled(ev) and denied_for(ev, R_HOLIDAY),
              "%s; site date %s" % (describe(ev), site.date()))
        clear_holidays(op)

        # ---- 8. Holidays scoped to a site --------------------------------------------------
        #
        # `Holiday.SiteId` exists because "Malaysian public holidays vary BY STATE, so a site needs
        # its own calendar rather than a hardcoded national one — and a site with offices in two
        # states needs two". `HolidayOn` implements the precedence and `store_sql_test.go` tests it
        # with SiteId 5. The question a unit test cannot ask: can any door ever BE at site 5?
        site = site_at_hour(10, 15)
        print("\n--- 8. a holiday scoped to one site, and a door at another ---")
        print("    site:", site)
        sim.stop()
        boot(build_app=False, timezone=site.name)
        op = admin()
        sim.run("happy")

        r = Rules(op, "Selangor Door", {"siteId": 7})
        stored_site = int(r.door.get("siteId") or 0)
        check("a door can be placed at a site", stored_site == 7,
              "stored siteId=%s" % stored_site)
        office, _ = make_schedule(op, "Office Hours", all_week(9 * 60, 17 * 60))
        r.use(office)
        ev = badge(op, r.door_id)
        scoped_control = check("site control: the door grants before any holiday",
                               enrolled(ev) and granted(ev), describe(ev))

        hid, _ = holiday(op, site.date(), "deny", "Other State Day", site_id=8)
        ev = badge(op, r.door_id)
        check("a holiday for ANOTHER site does not close this one",
              scoped_control and enrolled(ev) and granted(ev), describe(ev))
        op.delete("/api/schedules/holidays/%d" % hid)

        holiday(op, site.date(), "deny", "This State Day", site_id=7)
        ev = badge(op, r.door_id)
        check("a holiday scoped to this site closes its doors",
              scoped_control and enrolled(ev) and denied_for(ev, R_HOLIDAY),
              "%s; door siteId=%s" % (describe(ev), stored_site))

    finally:
        if sim:
            sim.stop()
        else:
            Sim().stop()
        teardown()

    print("\n========= schedules, holidays, the night shift and the timezone =========")
    print("PASS %d / %d" % (len(PASSES), len(PASSES) + len(FAILS)))
    for f in FAILS:
        print("  FAIL", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
