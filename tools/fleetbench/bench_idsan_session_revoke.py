# myidsan bench: does revoking a session actually END it — at the app the user is using?
#
# WHY THIS ONE. "Terminate this person's access, now" is the single most important thing an
# identity server does. It is what an administrator reaches for when a laptop is stolen, when
# someone leaves, when an account is compromised. myidsan has a whole session-administration
# surface for it (`/api/session-admin`): it lists sessions, revokes them, writes an audit
# entry, and the listing afterwards says the session is gone.
#
# THE TRAP THIS BENCH IS SHAPED AROUND. The cache is the session AUTHORITY and the table is
# only an index, so a revoke that updated the row and not the cache would LOOK exactly right —
# the listing is what an operator sees, and it reads the row. Every assertion here is therefore
# made against a REAL SECOND PROCESS holding a REAL SESSION, never against the listing that the
# revoke itself just wrote.
#
# And the larger version of the same question: myidsan and the relying app are separate
# processes with separate caches. The relying app is handed myidsan's session id and stores its
# OWN copy of the session under its OWN signing key. So "revoked at the identity server" and
# "revoked at the app the user is actually using" are two different claims, and only one of
# them is easy to test. This bench tests the other one.
#
# IT USES A REAL SECOND ACCOUNT, and that is not a detail. An earlier draft ran everything as
# the stock admin, and the results were worthless: "revoke every session for this user" also
# signed the OPERATOR out (same account), so the audit read that followed returned an empty
# list and looked like a missing trail. An administrator revoking THEMSELVES is not the
# scenario anybody cares about.
#
# THE CLAIMS UNDER TEST:
#
#   1. a second account can be provisioned and can sign in through SSO to the relying app;
#   2. the session is visible to an administrator under the SAME session id the relying app's
#      token carries — one session, not two things that merely look alike;
#   3. an administrator's revoke ends it AT MYIDSAN — the next request with that cookie is
#      refused, not merely absent from a list;
#   4. **an administrator's revoke ends it AT THE RELYING APP** — the app the user is
#      actually holding open. This is the claim the product is really making, and the one an
#      operator believes they are making when they click the button;
#   5. it stays dead: presenting the same cookie again does not resurrect it;
#   6. "sign out everywhere else" ends the OTHER sessions and spares the caller's, which needs
#      two real sessions to mean anything;
#   7. one user cannot end another user's session, and is not told whether it existed;
#   8. the operator's own session survives revoking somebody else's;
#   9. every revoke reaches the audit trail, attributed to whoever did it.
#
#   python tools/fleetbench/idsan_harness.py               # fresh stand-up, then:
#   python tools/fleetbench/bench_idsan_session_revoke.py
import base64
import json
import os
import re
import sys
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import result_list
from idsan_harness import (BENCH_PASS, IDSAN_URL, LOCAL_LOGIN, RELIER_URL, admin)

urllib3.disable_warnings()
CHECKS = []

VICTIM_EMAIL = "revoke.bench@example.test"
VICTIM_PASS = "Bench!2345678"
VIEWER_ROLE = 2  # seeded "viewer"; a new account with NO role lands in pending-clearance.
# sso.policyCacheTtlSeconds from the shipped config: how often a relying app re-asks the
# identity server whether a session it is serving is still live. Restated here so a check that
# depends on it fails loudly if it moves.
POLICY_TTL = 30


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


def browser():
    s = requests.Session()
    s.verify = False
    # REQUESTS_CA_BUNDLE in the environment OVERRIDES session.verify, and the failure reads
    # like the app's fault.
    s.trust_env = False
    return s


def field(html, name):
    m = re.search(r'name="%s"[^>]*value="([^"]*)"' % name, html)
    return m.group(1).replace("&amp;", "&") if m else ""


def hop(s, url, **kw):
    if url.startswith("/"):
        url = IDSAN_URL + url
    return s.get(url, allow_redirects=False, timeout=30, **kw)


def csrf_of(jar):
    return jar.get("__Host-kopiv2_csrf") or jar.get("kopiv2_csrf") or ""


def sign_in_through_sso(username, password):
    """Drive the whole authorization-code flow and hand back TWO SEPARATE cookie jars.

    THE TRAP: cookies ignore ports. myidsan and the relying app are the same host IP on two
    different ports and both set a session cookie under the same name, so ONE requests.Session
    driving the flow keeps whichever was written last — and every later assertion silently
    measures the wrong app. Splitting the jars here is what makes "dead at myidsan" and "dead
    at the relying app" two distinguishable questions instead of one confused one.

    Returns (idsan_jar, relier_jar, steps)."""
    flow = browser()
    steps = []

    r = flow.get(RELIER_URL + "/api/auth/start", allow_redirects=False, timeout=30)
    steps.append(("relier /auth/start", r.status_code))
    if r.status_code not in (301, 302, 303, 307):
        return None, None, steps
    r = hop(flow, r.headers["Location"])
    steps.append(("idsan /authorize", r.status_code))
    if r.status_code not in (301, 302, 303, 307):
        return None, None, steps
    page = hop(flow, r.headers["Location"])
    steps.append(("idsan login page", page.status_code))
    if page.status_code != 200:
        return None, None, steps

    r = flow.post(IDSAN_URL + "/api/auth/login", data={
        "authCsrf": field(page.text, "authCsrf"),
        "continue": field(page.text, "continue"),
        "username": username, "password": password,
    }, allow_redirects=False, timeout=30)
    steps.append(("idsan login POST", r.status_code))
    if r.status_code not in (301, 302, 303, 307):
        steps.append(("body", (r.text or "")[:200]))
        return None, None, steps
    # Captured from THIS response: the identity server's own browser session.
    idsan_jar = requests.cookies.merge_cookies(requests.cookies.RequestsCookieJar(), r.cookies)

    r = hop(flow, r.headers["Location"])
    steps.append(("idsan /authorize (signed in)", r.status_code))
    if r.status_code not in (301, 302, 303, 307):
        return None, None, steps
    r = flow.get(r.headers["Location"], allow_redirects=False, timeout=30)
    steps.append(("relier /auth/callback", r.status_code))
    if r.status_code not in (301, 302, 303, 307):
        steps.append(("body", (r.text or "")[:200]))
        return None, None, steps
    # And from THIS one: the relying app's session — the one the user actually holds open.
    relier_jar = requests.cookies.merge_cookies(requests.cookies.RequestsCookieJar(), r.cookies)

    return idsan_jar, relier_jar, steps


def jarred(jar):
    s = browser()
    if jar:
        s.cookies.update(jar)
    return s


def relier_alive(jar):
    """Is the relying app still serving this session? The question the whole bench turns on."""
    return jarred(jar).get(RELIER_URL + "/api/session/me", timeout=30)


def idsan_alive(jar):
    return jarred(jar).get(IDSAN_URL + "/api/session", timeout=30)


def jwt_claims(token):
    """Decode a JWT payload WITHOUT verifying it.

    Deliberately unverified: the relying app signs its own token with its OWN key, so myidsan
    cannot validate it and neither can this bench. Nothing here trusts the contents — the
    claims are only used to look up what to ask the SERVER about, and every verdict below
    comes from a real request. An earlier draft tried `/api/sso/introspect` on this cookie and
    got "signature is invalid", which is correct behaviour that reads exactly like a revoked
    session; that near-miss is the reason for this comment."""
    parts = (token or "").split(".")
    if len(parts) != 3:
        return {}
    raw = parts[1] + "=" * (-len(parts[1]) % 4)
    try:
        return json.loads(base64.urlsafe_b64decode(raw))
    except Exception:
        return {}


def token_of(jar):
    for name in ("__Host-kopiv2_access", "kopiv2_access", "__Host-kopiv2_token", "kopiv2_token"):
        v = jar.get(name)
        if v:
            return v
    for _, value in (jar or {}).items():
        if value.count(".") == 2 and len(value) > 60:
            return value
    return ""


def session_id_of(relier_jar):
    """The session id the RELYING APP is holding — which is myidsan's, because the relying app
    reuses it verbatim. That reuse is what makes a cross-process revoke even conceivable, and
    check 2 asserts it rather than assuming it."""
    claims = jwt_claims(token_of(relier_jar))
    return claims.get("SessionId") or claims.get("sessionId") or claims.get("jti") or ""


def main():
    op = admin(IDSAN_URL, *LOCAL_LOGIN["myidsan"])

    def op_post(path, body):
        return op.s.post(IDSAN_URL + path, json=body,
                         headers={"X-CSRF-Token": op.csrf()}, timeout=30)

    # ---- 1. a real second account, and a real session at a real second process -------------
    r = op_post("/api/user-credential", {
        "email": VICTIM_EMAIL, "userpwd": VICTIM_PASS, "firstName": "Revoke", "lastName": "Bench",
        "userRoleId": VIEWER_ROLE, "isActive": True, "mustChangePassword": False,
    })
    victim_id = 0
    if r.status_code == 200:
        try:
            victim_id = int((r.json().get("result") or {}).get("id") or 0)
        except (ValueError, TypeError):
            victim_id = 0
    if not victim_id:
        # A re-run against a harness that already has the account: find it rather than failing
        # for a reason that has nothing to do with revocation.
        for row in result_list(op.get(IDSAN_URL + "/api/user-credential?limit=200"), "items"):
            if isinstance(row, dict) and row.get("email") == VICTIM_EMAIL:
                victim_id = int(row.get("id") or 0)
    check("a second, non-administrator account exists to be revoked", bool(victim_id),
          brief(r) if not victim_id else "userId=%d" % victim_id)
    if not victim_id:
        return report()

    idsan_jar, relier_jar, steps = sign_in_through_sso(VICTIM_EMAIL, VICTIM_PASS)
    check("that account signs in through myidsan and lands at the relying app",
          relier_jar is not None, json.dumps(steps))
    if relier_jar is None:
        return report()

    me = relier_alive(relier_jar)
    check("the relying app serves the session", me.status_code == 200, brief(me))
    mine = idsan_alive(idsan_jar)
    check("the identity server serves its own browser session too",
          mine.status_code == 200, brief(mine))

    # ---- 2. one session, seen from both sides ----------------------------------------------
    session_id = session_id_of(relier_jar)
    check("the relying app's token carries a session id", bool(session_id),
          "cookies: %s" % sorted((relier_jar or {}).keys()))

    admin_rows = result_list(op.get(IDSAN_URL + "/api/session-admin/user/%d" % victim_id),
                             "sessions", "items")
    ids = [row.get("sessionId") for row in admin_rows if isinstance(row, dict)]
    check("an administrator sees that session under the SAME id the relying app holds",
          bool(session_id) and session_id in ids,
          "looking for %r among %r" % (session_id, ids))
    check("and the listing reports it active",
          any(isinstance(row, dict) and row.get("sessionId") == session_id and row.get("active")
              for row in admin_rows),
          json.dumps(admin_rows)[:250])

    # ---- 3./4. the revoke, and how far it actually reaches ---------------------------------
    #
    # The ORDER of these is the point. The listing is what an operator sees, so it is checked
    # first and deliberately not trusted; then the identity server itself; then the app the
    # user is holding open. A revoke can satisfy the first two and fail the third, and if it
    # does, "terminate this person's access" did not.
    rev = op_post("/api/session-admin/user/%d/revoke" % victim_id, {})
    check("an administrator can revoke every session for that account",
          rev.status_code == 200, brief(rev))

    after = result_list(op.get(IDSAN_URL + "/api/session-admin/user/%d" % victim_id),
                        "sessions", "items")
    check("the listing an operator reads now says the session is over",
          not any(isinstance(row, dict) and row.get("sessionId") == session_id and row.get("active")
                  for row in after),
          json.dumps(after)[:250])

    dead_at_idsan = idsan_alive(idsan_jar)
    check("the session is dead AT MYIDSAN — the next request with that cookie is refused",
          dead_at_idsan.status_code in (401, 403), brief(dead_at_idsan))

    # THE HEADLINE, and the shape of the claim matters. The relying app re-asks the identity
    # server on a TTL (sso.policyCacheTtlSeconds, 30s by default) rather than on every request
    # — a round trip in front of every API call would make the console unusable whenever the
    # IdP is slow. So the guarantee is not "instantly", it is "within the configured window",
    # and this MEASURES that window rather than asserting an instant that was never promised.
    #
    # An earlier run of this bench asserted the instant and failed, which is worth recording:
    # the delay is real, an operator revoking access in an emergency needs to know about it,
    # and a bench that hid it behind a sleep would have documented nothing.
    started = time.time()
    immediate = relier_alive(relier_jar)
    took, dead_at_relier = None, immediate
    while time.time() - started < POLICY_TTL + 20:
        dead_at_relier = relier_alive(relier_jar)
        if dead_at_relier.status_code in (401, 403):
            took = round(time.time() - started, 1)
            break
        time.sleep(2)
    check("the session dies AT THE RELYING APP — the app the user is actually holding open",
          dead_at_relier.status_code in (401, 403),
          "took %ss (immediate response was %d)" % (took, immediate.status_code)
          if took is not None else brief(dead_at_relier))
    check("and it does so within the configured revocation-check window",
          took is not None and took <= POLICY_TTL + 15,
          "took %ss, window %ss" % (took, POLICY_TTL))

    time.sleep(3)
    still_dead = relier_alive(relier_jar)
    check("and it stays dead when the same cookie is presented again — the relying app drops "
          "its own entry rather than re-asking forever",
          still_dead.status_code in (401, 403), brief(still_dead))

    # The operator must not have signed themselves out by revoking somebody else.
    op_ok = op.get(IDSAN_URL + "/api/session")
    check("the operator's own session survives revoking somebody else's",
          op_ok.status_code == 200, brief(op_ok))

    # ---- 6. sign out everywhere else -------------------------------------------------------
    #
    # TWO real sessions, because with one the "spare the caller" rule is unfalsifiable:
    # sparing everything and sparing exactly the caller look identical.
    first_idsan, first_relier, _ = sign_in_through_sso(VICTIM_EMAIL, VICTIM_PASS)
    second_idsan, second_relier, _ = sign_in_through_sso(VICTIM_EMAIL, VICTIM_PASS)
    check("two independent sessions can be established for one account",
          first_relier is not None and second_relier is not None
          and session_id_of(first_relier) != session_id_of(second_relier),
          "%r vs %r" % (session_id_of(first_relier), session_id_of(second_relier)))

    if first_idsan and second_idsan:
        caller = jarred(first_idsan)
        r = caller.post(IDSAN_URL + "/api/session/revoke-all", json={},
                        headers={"X-CSRF-Token": csrf_of(first_idsan)}, timeout=30)
        check("a user can sign out of their other devices", r.status_code == 200, brief(r))
        try:
            revoked = (r.json().get("result") or {}).get("revoked")
        except ValueError:
            revoked = None
        check("and it reports having ended at least one other session", bool(revoked), str(revoked))

        check("the session that asked is spared",
              idsan_alive(first_idsan).status_code == 200, brief(idsan_alive(first_idsan)))
        other = idsan_alive(second_idsan)
        check("the OTHER session is ended at myidsan", other.status_code in (401, 403), brief(other))
        other_relier = relier_alive(second_relier)
        waited = time.time() + POLICY_TTL + 20
        while other_relier.status_code == 200 and time.time() < waited:
            time.sleep(2)
            other_relier = relier_alive(second_relier)
        check("and the other session is ended AT THE RELYING APP too",
              other_relier.status_code in (401, 403), brief(other_relier))

    # ---- 7. one user must not be able to end another's session -----------------------------
    #
    # A REAL cross-principal attempt: the victim (a viewer) aiming at the OPERATOR's session.
    op_sessions = result_list(op.get(IDSAN_URL + "/api/session"), "sessions", "items")
    op_sid = next((row.get("sessionId") for row in op_sessions
                   if isinstance(row, dict) and row.get("sessionId")), "")
    if op_sid and first_idsan:
        stranger = jarred(first_idsan)
        r = stranger.delete(IDSAN_URL + "/api/session/" + op_sid,
                            headers={"X-CSRF-Token": csrf_of(first_idsan)}, timeout=30)
        check("a viewer cannot end an administrator's session by id",
              r.status_code not in (200,), brief(r))
        check("and is told 'not found' rather than 'forbidden', so it is not an id oracle",
              r.status_code == 404, brief(r))
        check("the administrator's session really did survive that attempt",
              op.get(IDSAN_URL + "/api/session").status_code == 200)
        # The viewer must also not reach the ADMIN surface at all.
        r = stranger.get(IDSAN_URL + "/api/session-admin/user/1", timeout=30)
        check("and cannot list an administrator's sessions either",
              r.status_code in (401, 403), brief(r))

    # ---- 9. the trail ----------------------------------------------------------------------
    rows = result_list(op.get(IDSAN_URL + "/api/audit?limit=300"), "items")
    actions = set(row.get("action") for row in rows if isinstance(row, dict))
    check("the audit trail is readable and non-empty", bool(rows),
          "actions: %s" % sorted(a for a in actions if a))
    check("an administrator's revoke-all is recorded", "session.revoke_all" in actions,
          "actions: %s" % sorted(a for a in actions if a))
    named = [row for row in rows if isinstance(row, dict)
             and row.get("action") == "session.revoke_all"
             and str(row.get("targetId") or "") == str(victim_id)]
    check("and the entry names the account whose sessions were ended", bool(named),
          json.dumps(named[:1])[:250])

    return report()


if __name__ == "__main__":
    sys.exit(main())
