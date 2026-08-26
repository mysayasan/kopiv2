# myidsan bench: the sign-in every other app in the suite depends on.
#
# myidsan is the SSO server. If it is wrong, every app is wrong — and until this it had a third
# of the flagships' unit coverage and NO live exercise at all. Its own tests prove its handlers
# in isolation; nothing proved that a real second app can get a user signed in through it, or
# that its refusals refuse.
#
# THE CLAIM UNDER TEST:
#
#   1. a real relying app can actually sign a user in — the whole authorization-code flow,
#      across two processes, ending in a session the relying app itself accepts;
#   2. a user arriving through SSO for the first time gets NO ROLE and NO PERMISSIONS. Identity
#      is not authorization, and an identity server that hands out access by existing is the
#      single worst thing this component could do;
#   3. every refusal refuses: an unregistered redirect, an unknown client, the wrong audience,
#      the wrong client secret, a replayed authorization code, introspection without the shared
#      token, a wrong password, and a password below the policy;
#   4. the trail records who signed in.
#
# (3) is where the defects live. A happy path that works proves the feature exists; the
# refusals are what make it worth having.
#
#   python tools/fleetbench/idsan_harness.py
#   python tools/fleetbench/bench_idsan_sso.py
import json
import os
import re
import sys

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import result_list
from idsan_harness import (BENCH_PASS, CLIENT_ID, CLIENT_SECRET, IDSAN_URL, INTERNAL_TOKEN,
                           LOCAL_LOGIN, RELIER_URL, Client, admin)

urllib3.disable_warnings()
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


def browser():
    s = requests.Session()
    s.verify = False
    s.trust_env = False
    return s


def field(html, name):
    m = re.search(r'name="%s"[^>]*value="([^"]*)"' % name, html)
    return m.group(1).replace("&amp;", "&") if m else ""


def hop(s, url, **kw):
    """One redirect at a time, so a bench can see WHERE a flow stopped rather than only that
    it did. `allow_redirects=True` would collapse the refusals under test into a 200."""
    if url.startswith("/"):
        url = IDSAN_URL + url
    return s.get(url, allow_redirects=False, timeout=30, **kw)


def sign_in(s, username=BENCH_PASS and "admin", password=BENCH_PASS, start_query=""):
    """Drive the whole flow and return the final response plus every step, so a failure names
    the hop it failed at."""
    steps = []
    r = s.get(RELIER_URL + "/api/auth/start" + start_query, allow_redirects=False, timeout=30)
    steps.append(("relier /auth/start", r.status_code))
    if r.status_code not in (301, 302, 303, 307):
        return r, steps
    r = hop(s, r.headers["Location"])
    steps.append(("idsan /authorize", r.status_code))
    if r.status_code not in (301, 302, 303, 307):
        return r, steps
    page = hop(s, r.headers["Location"])
    steps.append(("idsan login page", page.status_code))
    if page.status_code != 200:
        return page, steps
    r = s.post(IDSAN_URL + "/api/auth/login", data={
        "authCsrf": field(page.text, "authCsrf"),
        "continue": field(page.text, "continue"),
        "username": username, "password": password,
    }, allow_redirects=False, timeout=30)
    steps.append(("idsan login POST", r.status_code))
    if r.status_code not in (301, 302, 303, 307):
        return r, steps
    r = hop(s, r.headers["Location"])
    steps.append(("idsan /authorize (signed in)", r.status_code))
    if r.status_code not in (301, 302, 303, 307):
        return r, steps
    back = r.headers["Location"]
    r = s.get(back, allow_redirects=False, timeout=30)
    steps.append(("relier /auth/callback", r.status_code))
    return r, steps


def main():
    # ---- (1) the whole thing, across two processes -------------------------------------------
    s = browser()
    final, steps = sign_in(s)
    check("a relying app can sign a user in through myidsan, end to end",
          final.status_code in (301, 302, 303, 307), json.dumps(steps))

    me = s.get(RELIER_URL + "/api/session/me", timeout=30)
    body = {}
    try:
        body = me.json().get("result") or {}
    except ValueError:
        pass
    check("and the relying app accepts the session it was handed",
          me.status_code == 200 and body.get("userId"), me.text[:200])
    check("the session says it is FEDERATED, and names who issued it and who it is for",
          body.get("kind") == "federated" and body.get("issuer") == "myidsan"
          and CLIENT_ID in (body.get("audience") or []),
          json.dumps({k: body.get(k) for k in ("kind", "issuer", "audience")}))

    # ---- (2) identity is not authorization ----------------------------------------------------
    # THE ONE THAT MATTERS MOST ON THIS COMPONENT. An identity server that grants access by
    # existing is the worst thing this could do, and it is a mistake that looks like a
    # convenience right up until it does not.
    check("a user arriving through SSO for the first time gets NO ROLE and NO PERMISSIONS — "
          "being known is not being allowed",
          body.get("roleId") in (0, None) and not (body.get("permissions") or []),
          json.dumps({k: body.get(k) for k in ("roleId", "permissions", "pending")}))
    check("and is held pending somebody's clearance rather than quietly admitted",
          bool(body.get("pending")), json.dumps({"pending": body.get("pending")}))

    # ---- (3) the refusals ----------------------------------------------------------------------
    idsan = admin(IDSAN_URL, *LOCAL_LOGIN["myidsan"])

    def authorize(**over):
        params = {
            "response_type": "code", "client_id": CLIENT_ID, "audience": CLIENT_ID,
            "redirect_uri": RELIER_URL + "/api/auth/callback", "state": "benchstate",
        }
        params.update(over)
        return idsan.s.get(IDSAN_URL + "/api/auth/authorize", params=params,
                           allow_redirects=False, timeout=30)

    r = authorize(redirect_uri=RELIER_URL + "/api/auth/callback/")
    check("a redirect URI that is not registered EXACTLY is refused — a trailing slash is a "
          "different place to send somebody's credentials",
          r.status_code not in (301, 302, 303, 307), "%d %s" % (r.status_code, r.text[:120]))

    r = authorize(client_id="not-a-real-client")
    check("an unknown client is refused", r.status_code not in (301, 302, 303, 307),
          "%d %s" % (r.status_code, r.text[:120]))

    r = authorize(audience="mymatasan")
    check("an audience the client is not registered for is refused — otherwise one app's "
          "token opens another app", r.status_code not in (301, 302, 303, 307),
          "%d %s" % (r.status_code, r.text[:120]))

    # A real code, to test what happens to it.
    s2 = browser()
    final2, steps2 = sign_in(s2)
    code = ""
    if final2.status_code in (301, 302, 303, 307) or final2.url:
        # The callback consumed it; take a fresh one straight from /authorize with a signed-in
        # myidsan session instead, so the code is unused.
        r = authorize()
        loc = r.headers.get("Location", "")
        m = re.search(r"[?&]code=([^&]+)", loc)
        code = m.group(1) if m else ""
    check("a signed-in session at myidsan can obtain an authorization code", bool(code),
          "location was " + (r.headers.get("Location", r.text[:120])[:160]))

    if code:
        def token(**over):
            body = {"grant_type": "authorization_code", "code": code,
                    "client_id": CLIENT_ID, "client_secret": CLIENT_SECRET,
                    "redirect_uri": RELIER_URL + "/api/auth/callback"}
            body.update(over)
            return requests.post(IDSAN_URL + "/api/auth/token", json=body,
                                 verify=False, timeout=30)

        r = token(client_secret="wrong-secret")
        check("a code cannot be exchanged with the wrong client secret",
              r.status_code != 200, "%d %s" % (r.status_code, r.text[:140]))

        r = token()
        check("but it can with the right one", r.status_code == 200, "%d %s" % (r.status_code, r.text[:140]))

        r = token()
        check("and a code cannot be exchanged TWICE — a replayed code is somebody else's "
              "session", r.status_code != 200, "%d %s" % (r.status_code, r.text[:140]))

    # Introspection is how a relying app asks "is this session still good". It is gated by a
    # shared token, and apphost DISABLES it outright when that token is a known placeholder.
    r = requests.post(IDSAN_URL + "/api/sso/introspect", json={"token": "anything"},
                      verify=False, timeout=30)
    check("introspection without the shared internal token is refused", r.status_code != 200,
          "%d %s" % (r.status_code, r.text[:120]))

    # ---- passwords ------------------------------------------------------------------------------
    s3 = browser()
    final3, steps3 = sign_in(s3, password="definitely-not-the-password")
    ok3 = s3.get(RELIER_URL + "/api/session/me", timeout=30)
    check("a wrong password does not produce a session anywhere",
          not (ok3.status_code == 200 and (ok3.json().get("result") or {}).get("userId")),
          json.dumps(steps3))

    r = idsan.s.post(IDSAN_URL + "/api/login/default/change-password",
                     json={"currentPassword": BENCH_PASS, "newPassword": "short1!"},
                     headers={"X-CSRF-Token": idsan.csrf()}, timeout=30)
    check("a password below the policy is refused, with the rule stated",
          r.status_code != 200 and "12" in r.text, "%d %s" % (r.status_code, r.text[:160]))

    # ---- (4) the trail -----------------------------------------------------------------------------
    #
    # THE ENVELOPE TRAP, and this bench walked into it on its first run exactly as three
    # others have: /api/audit answers {data:{result:[…]}}, not {result:{items:[…]}}. Reading
    # the wrong key returned zero entries for a trail that was working perfectly — a check
    # that fails on correct output. `result_list` is the harness helper that exists for this.
    trail = result_list(idsan.get("/api/audit?limit=100"), "items")
    actions = {}
    for row in trail:
        actions.setdefault(row.get("action"), 0)
        actions[row.get("action")] += 1
    check("the identity server records sign-ins", len(trail) > 0, json.dumps(actions))

    # WHAT THE TRAIL COULD NOT ANSWER BEFORE THIS BENCH. It said an account signed in. It did
    # not say what that sign-in OPENED — and on an identity server those are different facts:
    # the credential check happens once, and the access it is traded for happens per relying
    # app. "This account was compromised, which applications did it reach?" is the question
    # this component exists to answer, and it was the only party that knew and did not write
    # it down.
    check("and records WHICH APP an account was let into, not merely that it signed in",
          actions.get("sso.authorize", 0) > 0 and actions.get("sso.token_issue", 0) > 0,
          json.dumps(actions))
    granted = [r for r in trail if r.get("action") in ("sso.authorize", "sso.token_issue")]
    check("naming the client and the account together — neither half answers the question "
          "alone",
          all(r.get("targetId") == CLIENT_ID and r.get("actorId") for r in granted),
          json.dumps([{k: r.get(k) for k in ("action", "targetId", "actorId")} for r in granted][:3]))

    # The refusals are the more useful half: an unregistered redirect URI, an unknown client
    # and a replayed code are what an attack on this flow LOOKS like, and a trail holding only
    # successes cannot show one.
    check("and records the REFUSALS, which is what an attack on this flow looks like",
          actions.get("sso.refused", 0) >= 3, json.dumps(actions))
    return report()


if __name__ == "__main__":
    sys.exit(main())
