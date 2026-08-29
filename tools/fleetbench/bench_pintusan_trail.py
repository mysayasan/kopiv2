# mypintusan bench: is there a record of WHO CHANGED THE RULES ABOUT WHO GETS IN?
#
# WHY THIS ONE. This app keeps the best log in the suite — `AccessEvent` records every badge at
# every door, granted or denied, with the reason. And until this change it recorded nothing at all
# about the decisions BEHIND those badges: who created the grant, who deleted the holiday that was
# closing the site, who flipped a perimeter door's offline policy from deny to cached, who sealed
# the building and who quietly unsealed it twenty minutes later.
#
# That gap is worse here than the identical gap was on mymatasan, because the access log HIDES it.
# A grant edited at 23:40 produces no event; it produces ordinary green badge rows three weeks
# later, on a door the person was never meant to reach, with `decision: granted, reason: ok` beside
# them. The log answers "did this happen" perfectly and "was this supposed to happen" not at all.
#
# THE CLAIMS UNDER TEST — each one asserted against WHAT THE PRODUCT ITSELF STORED, read back
# through its own API, never against "the handler returned 200":
#
#   1. every audited administrative act lands in the trail, with the ACTOR taken from the session;
#   2. the DETAIL is a sentence a human can act on, not a row of ids — a grant entry names the
#      group, the door and the schedule;
#   3. a settings save says WHICH FIELD MOVED, and a rekey is recorded as a fact with NO KEY IN IT;
#   4. an accepted write that no handler instrumented is STILL in the trail — the guarantee that a
#      route added next year is recorded on the day it ships. This is the claim the whole design
#      rests on, and it is the one a unit test can only assert about a fake router;
#   5. an act that a handler REFUSES is recorded as denied, so "somebody with access tried" is
#      answerable;
#   6. the trail is APPEND-ONLY over HTTP: no route on the running app can change or remove a row,
#      not even for the administrator the rows are about;
#   7. the CSV export is real CSV, carries the detail column, and is offered by the running app;
#   8. **the ROLES.** Driving this as an admin proves nothing about the other two — #224 found the
#      admin pass at 51/51 while a viewer could not sign in at all. So an operator and a viewer are
#      created and each is asked for the trail, and refused;
#   9. reads are NOT in the trail, and neither is marking a notification read: an audit log that
#      fills with noise is one nobody opens, which is the same as not having one.
#
#   python tools/fleetbench/pintusan_harness.py    # stands the app up (wipes its data dir)
#   python tools/fleetbench/bench_pintusan_trail.py
import io
import json
import os
import sys
import time

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import result_list
from pintusan_harness import BASE, Client, admin

urllib3.disable_warnings()
# The trail's own entries contain em dashes and arrows, and a Windows console defaults to cp1252 —
# so the bench would die printing the product's own output, which reads as a product failure.
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
CHECKS = []


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
    return "%d %s" % (r.status_code, (r.text or "")[:200].replace("\n", " "))


def rid(r):
    """The id out of a create response, whatever the envelope."""
    try:
        out = r.json()
    except ValueError:
        return 0
    for holder in (out.get("result"), (out.get("data") or {}).get("result"), out):
        if isinstance(holder, dict) and holder.get("id"):
            return int(holder["id"])
    return 0


def trail(c, limit=400):
    """The product's OWN administrative trail.

    `result_list` with "items", not a hand-rolled unwrap: this app answers BOTH `{result: ...}` and
    `{data:{result:...}}` depending on the handler, and reading the wrong key looks exactly like an
    appliance where nothing has ever been changed. That envelope trap has cost this suite three
    benches already."""
    r = c.get("/api/audit?limit=%d" % limit)
    if r.status_code != 200:
        return [], r
    return result_list(r, "items"), r


def find(entries, action, contains=None):
    """The newest entry for an action, optionally whose detail contains a string."""
    for e in entries:
        if e.get("action") != action:
            continue
        if contains and contains not in (e.get("detail") or ""):
            continue
        return e
    return None


def main():
    c = admin()
    me = "admin"

    # A baseline, so every later claim is about what THIS bench caused. Without it a trail that was
    # already full would make any assertion pass, which is the "a check that passes on an empty (or
    # a pre-filled) result is not a check" trap this app has now hit seven times.
    before, r0 = trail(c)
    check("the administrative trail is readable by an administrator", r0.status_code == 200, brief(r0))
    baseline = len(before)
    print("   baseline entries: %d" % baseline)

    # --- 1. provision a site, and watch every step land in the trail ------------------------
    stamp = str(int(time.time()))[-6:]

    door = c.post("/api/doors", {
        "name": "Loading bay " + stamp, "class": "perimeter", "busPort": "tcp://127.0.0.1:4870",
        "osdpAddress": 9, "readerName": "Loading bay reader", "offlinePolicy": "deny",
        "unlockSeconds": 5, "heldOpenSeconds": 30,
    })
    door_id = rid(door)
    check("a door can be created", door_id > 0, brief(door))

    group = c.post("/api/groups", {"name": "Night cleaners " + stamp, "description": "bench"})
    group_id = rid(group)
    check("an access group can be created", group_id > 0, brief(group))

    sched = c.post("/api/schedules", {"name": "Out of hours " + stamp, "always": False,
                                      "windows": [{"weekday": 1, "startMin": 1320, "endMin": 360}]})
    sched_id = rid(sched)
    check("a schedule can be created", sched_id > 0, brief(sched))

    holder = c.post("/api/holders", {"ref": "bench-" + stamp, "name": "Aida Rahman", "kind": "staff"})
    holder_id = rid(holder)
    check("a person can be enrolled", holder_id > 0, brief(holder))

    cred = c.post("/api/holders/%d/credentials" % holder_id, {
        "kind": "card", "format": "wiegand26", "facilityCode": 1, "cardNumber": "4096"})
    cred_id = rid(cred)
    check("a badge can be issued", cred_id > 0, brief(cred))

    member = c.post("/api/groups/%d/members" % group_id, {"holderId": holder_id})
    member_id = rid(member)
    check("a person can be added to a group", member_id > 0, brief(member))

    grant = c.post("/api/grants", {"groupId": group_id, "doorId": door_id, "scheduleId": sched_id})
    grant_id = rid(grant)
    check("a grant can be created", grant_id > 0, brief(grant))

    holiday = c.post("/api/schedules/holidays", {"name": "Bench day " + stamp,
                                                 "date": "2026-12-25", "behaviour": "deny"})
    holiday_id = rid(holiday)
    check("a holiday can be added", holiday_id > 0, brief(holiday))

    lock_on = c.post("/api/lockdown", {"lockdown": True})
    check("the site can be sealed", lock_on.status_code == 200, brief(lock_on))
    lock_off = c.post("/api/lockdown", {"lockdown": False})
    check("the site can be unsealed", lock_off.status_code == 200, brief(lock_off))

    unlock = c.post("/api/doors/%d/unlock" % door_id)
    # The reader for this door is not on the bus (address 9, nothing there), so the unlock may be
    # refused by the controller. Either outcome is fine for THIS bench — what is under test is
    # whether the attempt is in the trail when it succeeded, so the claim below is conditioned on it.
    unlock_ok = unlock.status_code == 200
    print("   remote unlock: %s" % brief(unlock))

    # --- 2. what the trail actually holds ---------------------------------------------------
    entries, r1 = trail(c)
    check("the trail grew as the site was provisioned", len(entries) > baseline,
          "%d -> %d entries" % (baseline, len(entries)))

    expect = [
        ("door.create", "Loading bay " + stamp, "a door creation is recorded"),
        ("group.create", "Night cleaners " + stamp, "a group creation is recorded"),
        ("schedule.create", "Out of hours " + stamp, "a schedule creation is recorded"),
        ("holder.create", "Aida Rahman", "enrolling a person is recorded"),
        ("credential.issue", "Aida Rahman", "issuing a badge is recorded"),
        ("group.member_add", "Aida Rahman", "adding somebody to a group is recorded"),
        ("grant.create", "Night cleaners " + stamp, "A GRANT IS RECORDED - the row this feature exists for"),
        ("holiday.create", "Bench day " + stamp, "adding a holiday is recorded"),
        ("lockdown.set", "sealed", "sealing the site is recorded"),
    ]
    for action, contains, label in expect:
        e = find(entries, action, contains)
        check(label, e is not None,
              "no %s entry whose detail contains %r" % (action, contains) if e is None else
              (e.get("detail") or "")[:120])

    # THE DETAIL IS THE ROW. A trail of ids is one nobody can read without the database open beside
    # them, and the person reading it six months from now is an auditor, not an engineer.
    g = find(entries, "grant.create", "Night cleaners " + stamp)
    if g:
        detail = g.get("detail") or ""
        named_all = ("Night cleaners" in detail and "Loading bay" in detail and "Out of hours" in detail)
        check("the grant entry names the group, the door AND the schedule in one sentence",
              named_all, detail[:200])
        check("the grant entry attributes the change to the signed-in administrator",
              (g.get("actorEmail") or "") == me, json.dumps({"actor": g.get("actorEmail"),
                                                             "actorId": g.get("actorId")}))
        check("the grant entry records where the change came from", bool(g.get("clientIp")),
              json.dumps({"clientIp": g.get("clientIp"), "userAgent": (g.get("userAgent") or "")[:40]}))
        meta = g.get("metadata") or ""
        check("the grant entry carries the ids as structured metadata",
              str(door_id) in meta and str(group_id) in meta, meta[:200])

    # Deleting a holiday REOPENS a site that was meant to be shut. Of every row in this trail it is
    # the one most likely to answer "why was the building open that day".
    c.delete("/api/schedules/holidays/%d" % holiday_id)
    entries, _ = trail(c)
    hd = find(entries, "holiday.delete", "Bench day " + stamp)
    check("REMOVING a holiday is recorded, and says the site is no longer closed", hd is not None,
          (hd.get("detail") if hd else "no holiday.delete entry")[:160])

    # Revoking a badge: the REASON is the field worth having, because it is what turns a card
    # presented tomorrow from a denial into an incident.
    c.post("/api/holders/%d/credentials/%d/revoke" % (holder_id, cred_id),
           {"status": "stolen", "reason": "reported stolen at the gate"})
    entries, _ = trail(c)
    rv = find(entries, "credential.revoke")
    check("revoking a badge is recorded", rv is not None, (rv or {}).get("detail", "")[:120])
    if rv:
        check("the revocation carries the REASON, not just the status",
              "reported stolen at the gate" in (rv.get("metadata") or ""),
              (rv.get("metadata") or "")[:200])

    if unlock_ok:
        u = find(entries, "door.unlock_remote")
        check("a remote door open is in the administrative trail too, not only the access log",
              u is not None, (u or {}).get("detail", "")[:120])

    # --- 3. the settings diff ----------------------------------------------------------------
    cur = c.get("/api/settings/access")
    check("the settings are readable", cur.status_code == 200, brief(cur))
    body = None
    try:
        body = cur.json().get("result") or (cur.json().get("data") or {}).get("result")
    except ValueError:
        pass
    if isinstance(body, dict):
        body = dict(body)
        body["pinWindowSeconds"] = int(body.get("pinWindowSeconds") or 10) + 3
        saved = c.put("/api/settings/access", body)
        check("a settings edit is accepted", saved.status_code == 200, brief(saved))
        entries, _ = trail(c)
        st = find(entries, "settings.change")
        check("a settings change is recorded", st is not None, (st or {}).get("detail", "")[:160])
        if st:
            # "Settings changed" is the least useful entry an audit log can hold. The screen posts
            # the whole object on every save, so the request body cannot answer "what moved".
            check("the settings entry names the FIELD that moved, not just that a save happened",
                  "pinWindowSeconds" in (st.get("detail") or ""), (st.get("detail") or "")[:200])

    # NO KEY MATERIAL ANYWHERE IN THE TRAIL. It is readable by every administrator and exported to
    # CSV; a site base key in it is a key handed out, and it is the exact secret Secure Channel
    # exists to protect. Asserted over EVERY row rather than the ones this bench wrote.
    entries, _ = trail(c)
    site_key = "a0a1a2a3a4a5a6a7b0b1b2b3b4b5b6b7"
    leaked = [e for e in entries
              if site_key in ((e.get("detail") or "") + (e.get("metadata") or ""))]
    check("NO SITE KEY APPEARS ANYWHERE IN THE TRAIL", not leaked,
          json.dumps([e.get("action") for e in leaked])[:200])

    # --- 4. the guarantee: an uninstrumented write is still recorded -------------------------
    #
    # /api/setup/complete is served by SHARED code this app does not own, and no handler in
    # apps/mypintusan audits it. If it is in the trail, it is there because the middleware put it
    # there — which is the whole claim: a route added next year is recorded on the day it ships,
    # not on the day somebody notices it was not.
    done = c.post("/api/setup/complete")
    entries, _ = trail(c)
    generic = find(entries, "api.write", "/api/setup/complete")
    check("AN ACCEPTED WRITE THAT NO HANDLER INSTRUMENTED IS STILL IN THE TRAIL",
          generic is not None if done.status_code == 200 else True,
          "POST /api/setup/complete -> %s; entry: %s" % (done.status_code,
                                                         (generic or {}).get("detail", "none")))
    if generic:
        check("the fallback entry still attributes the actor from the session",
              (generic.get("actorEmail") or "") == me, json.dumps(generic.get("actorEmail")))

    # A read is not an administrative act. A trail that recorded every GET would be a request log,
    # and the rows that matter would be one in ten thousand.
    n_before = len(entries)
    for _ in range(5):
        c.get("/api/doors")
        c.get("/api/holders")
        c.get("/api/events")
    entries, _ = trail(c)
    check("reads are NOT recorded — the trail is a record of changes, not a request log",
          len(entries) == n_before, "%d -> %d entries after 15 reads" % (n_before, len(entries)))

    # --- 5. append-only over HTTP -------------------------------------------------------------
    #
    # The value of the trail is that the person whose actions it records cannot edit it. Asked as
    # the ADMINISTRATOR, because a superadmin bypasses the permission matrix — if a write route
    # existed, this is the account that would reach it.
    for method, path in (("DELETE", "/api/audit"), ("DELETE", "/api/audit/1"),
                         ("POST", "/api/audit"), ("PUT", "/api/audit")):
        r = c.s.request(method, BASE + path, auth=c.auth, timeout=30, verify=False)
        check("the running app refuses %s %s" % (method, path), r.status_code >= 400,
              brief(r))

    # --- 6. the CSV export --------------------------------------------------------------------
    csv = c.get("/api/audit.csv")
    check("the trail exports as CSV", csv.status_code == 200, brief(csv))
    if csv.status_code == 200:
        text = csv.text or ""
        # THE BOM IS DELIBERATE, and has to be stripped before reading the header. The export's
        # destination is Excel on somebody's Windows laptop, and without it every em dash in the
        # trail arrives as mojibake in the one artefact whose whole job is to be handed to an
        # outsider. This bench reported a broken header on a correct export until it knew that.
        check("the export starts with a UTF-8 BOM so Excel decodes it correctly",
              text.startswith(u"﻿"), repr(text[:6]))
        head = text.lstrip(u"﻿").split("\n")[0].strip()
        check("the export carries a header row an auditor can read",
              head.startswith("time,action,outcome,actor"), head[:120])
        check("the export carries the human sentence, not only ids",
              "Night cleaners " + stamp in text, "looked for the group name in %d bytes" % len(text))
        check("the export is offered as a download with a stamped filename",
              "attachment" in (csv.headers.get("Content-Disposition") or ""),
              csv.headers.get("Content-Disposition") or "(no header)")

    # --- 7. THE ROLES. Driving this as an admin proves nothing about the other two. ------------
    roles = result_list(c.get("/api/settings/roles"), "items")
    by_name = {(x.get("name") or "").lower(): x.get("id") for x in roles}
    check("the appliance offers the three roles", len(by_name) >= 3, json.dumps(list(by_name)))

    made = {}
    for role in ("operator", "viewer"):
        if role not in by_name:
            continue
        user = "trail-%s-%s" % (role, stamp)
        pw = "Bench!2345678"
        r = c.post("/api/settings/users", {"username": user, "displayName": user,
                                           "password": pw, "roleId": by_name[role], "isActive": True})
        if r.status_code == 200:
            made[role] = (user, pw)
        check("an administrator can create a %s account" % role, r.status_code == 200, brief(r))

    entries, _ = trail(c)
    uc = find(entries, "user.create", made.get("operator", ("",))[0])
    check("minting an account is recorded, WITH THE ROLE it was given", uc is not None,
          (uc or {}).get("detail", "no user.create entry")[:160])

    for role, (user, pw) in made.items():
        rc = Client()
        rc.auth = (user, pw)
        sess = rc.get("/api/auth/session")
        # Sign-in has to WORK first. A 401 here would make the refusal below pass for the wrong
        # reason — the trap that let #224 ship a viewer who could not get past the login card.
        check("a %s can sign in" % role, sess.status_code == 200, brief(sess))
        if sess.status_code != 200:
            continue
        got = rc.get("/api/audit")
        check("a %s is REFUSED the administrative trail" % role, got.status_code in (401, 403),
              brief(got))
        gotcsv = rc.get("/api/audit.csv")
        check("a %s is REFUSED the CSV export too — a separate path needs a separate rule" % role,
              gotcsv.status_code in (401, 403), brief(gotcsv))
        caps = rc.get("/api/auth/capabilities")
        try:
            flags = caps.json().get("result") or {}
        except ValueError:
            flags = {}
        check("the %s's own capabilities say the trail is not on offer" % role,
              flags.get("viewAudit") is False,
              json.dumps({"viewAudit": flags.get("viewAudit")}))

    # An operator who tries an admin-only act is turned away — and THAT is recorded. The caller got
    # past the permission matrix (an operator may POST under /api/holders) and was refused by the
    # handler's own gate, which is the denial worth a row of its own.
    if "operator" in made:
        user, pw = made["operator"]
        rc = Client()
        rc.auth = (user, pw)
        refused = rc.post("/api/grants", {"groupId": group_id, "doorId": door_id,
                                          "scheduleId": sched_id})
        check("an operator cannot create a grant", refused.status_code in (401, 403), brief(refused))
        entries, _ = trail(c)
        denied = [e for e in entries if e.get("outcome") == "denied"
                  and (e.get("actorEmail") or "") == user]
        # WHICH REFUSAL THIS IS, stated rather than glossed. The audit middleware runs INSIDE the
        # permission middleware, so a matrix-level 403 never reaches it — the refusal above is the
        # catalog's, and it belongs to api_log rather than here. The branch that DOES write a denied
        # row is the one where a caller passes the matrix and a handler's own "administrators only"
        # gate turns them away, and in this app's CURRENT catalog no role can reach such a route at
        # all: every admin-gated handler is already denied to viewer and operator by the matrix.
        #
        # So this asserts what is true instead of claiming coverage the wire cannot give. The denied
        # branch is covered by apis/audit_test.go:TestAuditMiddleware_RecordsAHandlerRefusal, and
        # that gap is real — say it, rather than reporting a green that means nothing.
        check("a MATRIX-level refusal is NOT in the trail, exactly as documented "
              "(it is in api_log; the trail records a handler's own refusal)",
              len(denied) == 0, "denied entries attributed to the operator: %d" % len(denied))

    # --- 8. the appliance's own users, and the last row ---------------------------------------
    for role, (user, _pw) in list(made.items()):
        uid = 0
        for u in result_list(c.get("/api/settings/users"), "items"):
            if (u.get("username") or "") == user:
                uid = u.get("id")
        if uid:
            c.post("/api/settings/users/%s/password" % uid, {"password": "Bench!9876543"})
            c.delete("/api/settings/users/%s" % uid)
    entries, _ = trail(c)
    check("resetting somebody's password is recorded", find(entries, "user.password_reset") is not None)
    ud = find(entries, "user.delete")
    check("deleting an account is recorded, and still NAMES the account that is gone",
          ud is not None and "trail-" in (ud.get("detail") or ""),
          (ud or {}).get("detail", "no user.delete entry")[:160])

    # Nothing in the trail carries a password, a PIN or a hash. Checked over every row, because the
    # export is the artefact that leaves the building.
    entries, _ = trail(c)
    blob = json.dumps(entries)
    for secret in ("Bench!2345678", "Bench!9876543", "admin123", "$2a$", "$2b$"):
        check("no credential material in the trail: %r" % secret, secret not in blob)

    out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "runs")
    try:
        os.makedirs(out, exist_ok=True)
        io.open(os.path.join(out, "pintusan-trail.json"), "w", encoding="utf-8").write(
            json.dumps([{"name": n, "ok": o, "detail": d} for n, o, d in CHECKS], indent=2))
    except Exception:
        pass

    return report()


if __name__ == "__main__":
    sys.exit(main())
