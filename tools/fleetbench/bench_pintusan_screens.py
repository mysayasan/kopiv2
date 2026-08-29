# Drive mypintusan's SCREENS. The first screen check this app has ever had.
#
# WHY. mypintusan is the app in this suite that physically unlocks doors, and after six live
# benches it was still the only app with ZERO screen checks. Every one of those benches drove the
# API. A green API run and a working screen are different claims, and the six benches before this
# one all found the same shape of defect: THE GATE WAS RIGHT AND NOTHING COULD REACH IT. A screen
# is one more thing that can fail to reach a gate that works.
#
# The specific reason this app needs it more than the others: it decides authorization TWICE.
# `App.js` hides Access rules and Settings on a client-side `user.isAdmin`; the server decides with
# the deny-by-default matrix in `services/rbac.go`. Two mechanisms, one intent, and nothing had
# ever checked they agree — the exact root cause this suite already recorded once as "nav uses
# isAdmin, server uses the matrix — two sources of truth".
#
# WHAT THIS DRIVES
#
#   PART A  every nav entry renders, has a heading, leaks no dictionary key, and every visible
#           enabled control is hit-testable at its own centre. In all FOUR languages, one of them
#           right-to-left, and for every role.
#   PART B  admin, English: real workflows with real input events, each confirmed against the
#           server. Lockdown from the screen (the one control an operator reaches for in an
#           emergency), a remote unlock, a badge issued and revoked, a grant changed end to end.
#   PART C  operator and viewer: does what the screen OFFERS match what the server ALLOWS, in both
#           directions? This is the part that pays: on myiotsan the equivalent found that every
#           non-admin was completely locked out of the UI.
#
# HOW TO RUN
#
#   python tools/fleetbench/bench_pintusan_screens.py
#
# KOPIV2_SKIP_BUILD=1 reuses the Go binary AND the frontend bundle, for before/after runs against
# one identical set of checks. Without it BOTH are rebuilt — a screen check that runs against a
# stale bundle is checking a commit nobody has.
import io
import json
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import pintusan_harness as H
from fleet_harness import REPO, ROOT

# Alarm bodies and product text reach this console verbatim, and some of them contain 120-ohm
# and em-dashes. A cp1252 console raises UnicodeEncodeError on the first one and kills the run
# mid-episode, which reads as a crash in the app.
try:
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
except Exception:
    pass

OUT = os.path.join(REPO, ".artifacts", "pintusan-screens")
WEB = os.path.join(REPO, "apps", "mypintusan", "views", "react-webpack")

# The accounts PART C signs in as. Their passwords must satisfy the shared password policy, and
# they are restated in uicheck_pintusan.js — a mismatch there reads as "the role cannot sign in".
OPERATOR = {"username": "bench-operator", "password": "Operator!2345", "role": "operator"}
VIEWER = {"username": "bench-viewer", "password": "Viewer!2345", "role": "viewer"}

# The estate the screens are checked against. Restated here rather than discovered, so a check
# cannot pass by matching something else that happens to be on screen.
SEED = {
    "doorName": "Screen bench lobby",
    "doorClass": "interior",
    "holderName": "Screen Bench Person",
    "holderRef": "SB-0001",
    "cardNumber": "00880040",
    "groupName": "Screen bench group",
    "scheduleName": "Screen bench office hours",
    "holidayName": "Screen bench holiday",
}

CHECKS = []


def check(name, ok, detail=""):
    CHECKS.append({"name": name, "ok": bool(ok), "detail": str(detail)[:400]})
    print(("PASS  " if ok else "FAIL  ") + name + (("   " + str(detail)[:300]) if detail else ""))
    return bool(ok)


def result(r):
    try:
        body = r.json()
    except ValueError:
        return None
    # THE ENVELOPE TRAP. These apps answer BOTH {data:{result}} and a bare {result} depending on
    # the handler. Reaching for one shape turns a working list into an empty one, and an empty
    # list makes every check below pass for no reason at all.
    if isinstance(body, dict):
        data = body.get("data")
        if isinstance(data, dict) and "result" in data:
            return data["result"]
        if "result" in body:
            return body["result"]
    return body


def rows(r):
    v = result(r)
    if isinstance(v, dict) and isinstance(v.get("items"), list):
        return v["items"]
    return v if isinstance(v, list) else []


def build_web():
    """Rebuild the SPA bundle from this tree.

    The container mounts apps/mypintusan read-only and serves apps/mypintusan/static, which is a
    COMMITTED build output. A screen check that skips this is checking whatever bundle was last
    committed, which is exactly the way a screen fix ships un-exercised."""
    if os.environ.get("KOPIV2_SKIP_BUILD") == "1":
        print("KOPIV2_SKIP_BUILD=1 — reusing the committed bundle")
        return
    print("building the mypintusan frontend bundle...")
    subprocess.run(["npm", "run", "build"], cwd=WEB, check=True,
                   shell=(os.name == "nt"), stdout=subprocess.DEVNULL)


def seed_estate(c):
    """Create everything the screens need to have something to render.

    A screen with no rows is a screen that cannot be wrong. Five of the six checks in PART A are
    about content, and every one of them passes vacuously on an empty estate."""
    made = {}

    # A door, which also creates its entry reader — there is no reader-create endpoint. The bus
    # port and address must match the seeded bus or the reader never comes up and the Readers
    # screen has nothing to say about it.
    r = c.post("/api/doors", {
        "name": SEED["doorName"], "class": SEED["doorClass"], "unlockSeconds": 5,
        "busPort": H.SIM_ADDR, "osdpAddress": H.READER_ADDR,
        "readerName": "Screen bench reader",
    })
    made["door"] = (result(r) or {}).get("id")
    check("the bench can create a door to look at", bool(made["door"]),
          "status %s %s" % (r.status_code, (r.text or "")[:160]))

    # A person and a badge. cardNumber is a STRING — an int is accepted, stored, and then never
    # matches anything, which turns every later decision into `unknown-credential`.
    r = c.post("/api/holders", {"name": SEED["holderName"], "ref": SEED["holderRef"], "kind": "staff"})
    made["holder"] = (result(r) or {}).get("id")
    check("the bench can create a person to look at", bool(made["holder"]),
          "status %s %s" % (r.status_code, (r.text or "")[:160]))

    if made["holder"]:
        r = c.post("/api/holders/%d/credentials" % made["holder"], {
            "kind": "card", "format": "wiegand26", "facilityCode": 1,
            "cardNumber": SEED["cardNumber"],
        })
        made["credential"] = (result(r) or {}).get("id")
        check("the bench can issue a badge to look at", bool(made["credential"]),
              "status %s %s" % (r.status_code, (r.text or "")[:160]))

    # A group with the person in it, an office-hours schedule, and a grant joining the three.
    r = c.post("/api/groups", {"name": SEED["groupName"], "description": "created by the screen bench"})
    made["group"] = (result(r) or {}).get("id")
    if made["group"] and made["holder"]:
        c.post("/api/groups/%d/members" % made["group"], {"holderId": made["holder"]})

    # startMin/endMin, NOT startMinute/endMinute. Go drops unknown JSON fields silently, so the
    # wrong names give every window 0-0 — which #222 proved matches every hour of every day.
    r = c.post("/api/schedules", {
        "name": SEED["scheduleName"], "always": False,
        "windows": [{"weekday": d, "startMin": 8 * 60, "endMin": 18 * 60} for d in range(1, 6)],
    })
    made["schedule"] = (result(r) or {}).get("id")
    check("the bench can create a schedule to look at", bool(made["schedule"]),
          "status %s %s" % (r.status_code, (r.text or "")[:160]))

    if made["group"] and made["door"] and made["schedule"]:
        r = c.post("/api/grants", {"groupId": made["group"], "doorId": made["door"],
                                   "scheduleId": made["schedule"]})
        made["grant"] = (result(r) or {}).get("id")
        check("the bench can create a grant to look at", bool(made["grant"]),
              "status %s %s" % (r.status_code, (r.text or "")[:160]))

    r = c.post("/api/schedules/holidays", {
        "name": SEED["holidayName"], "date": "2026-12-25", "behaviour": "deny", "siteId": 0,
    })
    made["holiday"] = (result(r) or {}).get("id")

    return made


def seed_accounts(c):
    """Create the operator and viewer accounts PART C signs in as.

    THE ROLE ID IS LOOKED UP, NEVER ASSUMED. A hardcoded 2 or 3 would silently mint the WRONG
    role, and a permission check against an account that is secretly an admin passes for the worst
    possible reason."""
    made = {}
    rr = c.get("/api/settings/roles")
    if rr.status_code != 200:
        check("an administrator can see the roles this appliance offers", False,
              "GET /api/settings/roles -> %s %s" % (rr.status_code, (rr.text or "")[:200]))
        return made
    roles = rows(rr)
    check("an administrator can see the roles this appliance offers", len(roles) >= 3,
          json.dumps([x.get("name") for x in roles]))

    for spec in (OPERATOR, VIEWER):
        match = [x for x in roles if (x.get("name") or "").lower() == spec["role"]]
        if not match:
            check("the %s role exists and can be assigned" % spec["role"], False,
                  "roles seen: %s" % [x.get("name") for x in roles])
            continue
        role_id = match[0]["id"]
        r = c.post("/api/settings/users", {
            "username": spec["username"], "displayName": spec["username"],
            "password": spec["password"], "roleId": role_id, "isActive": True,
        })
        ok = r.status_code == 200
        if not ok:
            existing = [u for u in rows(c.get("/api/settings/users"))
                        if (u.get("username") or "") == spec["username"]]
            if existing:
                uid = existing[0].get("id")
                c.post("/api/settings/users/%s/password" % uid, {"password": spec["password"]})
                ok = True
        check("an administrator can create a %s account" % spec["role"], ok,
              "POST /api/settings/users -> %s %s" % (r.status_code, (r.text or "")[:200]))
        if ok:
            made[spec["role"]] = spec

    return made


def run_uicheck(lang, role, accounts_ready):
    """One headless-Chrome pass. Returns (passed, total) read from the JSON the pass writes.

    ASSERT ON THE JSON. A screenshot you have to squint at is not an assertion — and then open the
    PNGs anyway, because on this suite a green screen check has twice coexisted with a visibly
    broken screen."""
    env = dict(os.environ, PINTUSAN_BASE=H.BASE,
               PINTUSAN_ACCOUNTS_READY="1" if accounts_ready else "0")
    script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "uicheck_pintusan.js")
    p = subprocess.run(["node", script, OUT, lang, role], env=env, cwd=REPO,
                       capture_output=True, text=True, encoding="utf-8", errors="replace")
    sys.stdout.write(p.stdout or "")
    if p.returncode not in (0, 1):
        print(p.stderr[-2000:] if p.stderr else "(no stderr)")
    path = os.path.join(OUT, "pintusan-%s-%s.json" % (role, lang))
    try:
        got = json.loads(io.open(path, encoding="utf-8").read())
    except Exception as e:
        check("the %s/%s screen pass produced a result" % (role, lang), False, str(e))
        return 0, 1
    for item in got:
        CHECKS.append(item)
    return sum(1 for x in got if x["ok"]), len(got)


def main():
    os.makedirs(OUT, exist_ok=True)
    build_web()

    # The admin password is rotated on first contact and the bootstrap one stops working, so the
    # UI signs in with the ROTATED password. uicheck_pintusan.js restates it.
    H.boot()
    c = H.admin()

    # The first-run wizard stands in front of every screen until setup is marked complete. It is
    # driven separately (PART B opens it deliberately); completing it here is what lets the
    # ordinary screens be reached at all.
    c.post("/api/setup/complete", {})

    seed_estate(c)
    accounts = seed_accounts(c)
    accounts_ready = "operator" in accounts and "viewer" in accounts

    # The simulator gives the Activity screen real rows: a badge that travels the whole chain,
    # rather than an access log seeded through the back door.
    H.build_sim()
    sim = H.start_sim(card=SEED["cardNumber"], bits=26, every="2s")
    try:
        time.sleep(14)
        events = rows(c.get("/api/events?limit=50"))
        check("the access log has real rows for the Activity screen to show", len(events) > 0,
              "%d events" % len(events))

        passes = []

        def keep_sim_alive(proc):
            """Restart the simulator if it has gone.

            The whole run is six browser passes over about ten minutes, and a simulator that exits
            somewhere in the middle takes the bus down with it. Later episodes then get "osdp: PD
            did not reply" from a door that is perfectly fine — which, in a pass whose entire job
            is deciding whether a role was refused, reads as a permission defect."""
            if proc.poll() is None:
                return proc
            print("the simulator had exited — restarting it before the next pass")
            return H.start_sim(card=SEED["cardNumber"], bits=26, every="2s")

        # PART A in every language, for the admin: translation and layout faults live there.
        for lang in ("en", "ms", "zh", "ar"):
            print("\n=== admin / %s ===" % lang)
            passes.append(("admin", lang) + run_uicheck(lang, "admin", accounts_ready))
            sim = keep_sim_alive(sim)
        # PART C for the two non-admin roles, English.
        for role in ("operator", "viewer"):
            print("\n=== %s / en ===" % role)
            sim = keep_sim_alive(sim)
            passes.append((role, "en") + run_uicheck("en", role, accounts_ready))
    finally:
        sim.terminate()
        # UNCONDITIONAL. A surviving simulator keeps the port and keeps badging the old card, and
        # every later episode then reads a grant from a reader that is not under test.
        subprocess.run(["taskkill", "/F", "/IM", "osdp-sim.exe"], capture_output=True,
                       shell=(os.name == "nt"))

    print("\n" + "=" * 78)
    for role, lang, p, t in passes:
        print("  %-9s %-3s  %d/%d" % (role, lang, p, t))
    passed = sum(1 for x in CHECKS if x["ok"])
    print("=" * 78)
    print("%d/%d checks passed" % (passed, len(CHECKS)))
    io.open(os.path.join(OUT, "summary.json"), "w", encoding="utf-8").write(
        json.dumps(CHECKS, indent=2, ensure_ascii=False))
    print("screenshots and per-pass JSON: " + OUT)
    for x in CHECKS:
        if not x["ok"]:
            print("  FAILED: " + x["name"] + "   " + x["detail"][:200])
    return 0 if passed == len(CHECKS) else 1


if __name__ == "__main__":
    sys.exit(main())
