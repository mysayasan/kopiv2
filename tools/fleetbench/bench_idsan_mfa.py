# myidsan bench: the second factor, and the re-authentication that guards the crown jewels.
#
# WHY THIS ONE NEXT. bench_idsan_sso.py proved a relying app can sign a user in. This proves
# the two controls that stand between a STOLEN CREDENTIAL and the identity store itself:
#
#   * the pre-session second factor — a password alone must not produce a kopiv2_access
#     cookie, because that cookie is a FEDERATION-CAPABLE SSO session. Hand one out before
#     the code is verified and a password-only attacker can mint relying-app tokens at
#     /api/auth/authorize for every app in the suite. The ordering is the whole control.
#
#   * step-up — a myidsan session lasts 72 hours and can export or restore the entire
#     identity store, clear anyone's second factor, and issue a temporary password for any
#     account. Step-up is what stops a cookie lifted from an unlocked laptop from doing all
#     of that for three days.
#
# THE CLAIMS UNDER TEST:
#
#   1. enrollment is real: a wrong code does not confirm a factor, a confirmed factor
#      cannot be silently re-enrolled over;
#   2. THE PRE-SESSION ORDERING HOLDS ON BOTH LEGS — the SPA's /api/login/default and the
#      server-rendered /api/auth/login the SSO redirect actually lands on — and the
#      half-authenticated state cannot be walked around by going straight to /authorize;
#   3. the challenge token is credential-grade: single-use, bound to the client it was
#      issued to, and attempt-bounded so a captured token cannot be ground against 10^6
#      codes;
#   4. a recovery code works EXACTLY once, and regenerating the set kills the old one;
#   5. a TOTP code cannot be replayed;
#   6. step-up guards the routes it claims to: export, restore and MFA admin-reset. A
#      password ALONE does not elevate an account that has a factor. Elevation belongs to
#      ONE session, and is throttled the way a login is;
#   7. the documented lost-device escape hatch (a RESET_MFA marker in the data dir) works,
#      and is reachable ONLY from the host — never by the locked-out user over HTTP;
#   8. every one of these events reaches the audit trail. On an identity server the trail
#      is the product: "who removed the second factor from this account, and when" is not
#      a question the operator can answer any other way.
#
# (2), (6) and (8) are where the defects live. A happy path proves the feature exists; the
# refusals and the record are what make it worth having.
#
#   python tools/fleetbench/idsan_harness.py     # MUST be a fresh stand-up: admin starts
#   python tools/fleetbench/bench_idsan_mfa.py   # with no factor, and this enrolls one
import base64
import hashlib
import hmac
import io
import json
import os
import re
import struct
import sys
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import ROOT, result_list, sh, wait_up
from idsan_harness import (BENCH_PASS, IDSAN_URL, LOCAL_LOGIN, RELIER_URL, Client, admin)

urllib3.disable_warnings()
CHECKS = []

# The passphrase the export is sealed with. Its own minimum (12) is not the password
# policy's — a different control with a different number, and using one for the other is
# how a bench ends up proving the wrong refusal.
EXPORT_PASSPHRASE = "bench-export-passphrase"


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
    return "%d %s" % (r.status_code, r.text[:160].replace("\n", " "))


# ---- TOTP, hand-rolled ---------------------------------------------------------------------
#
# Deliberately NOT pyotp: the bench must not be able to pass because it shares a library with
# the thing under test, and RFC 6238 with SHA-1/6 digits/30s is nine lines. If this and
# infra/mfa disagree, one of them is wrong and the bench is the one that says so.

def _step_now():
    return int(time.time() // 30)


def _code_for(secret, step):
    key = base64.b32decode(secret.upper() + "=" * (-len(secret) % 8))
    mac = hmac.new(key, struct.pack(">Q", step), hashlib.sha1).digest()
    off = mac[-1] & 0x0F
    return "%06d" % ((struct.unpack(">I", mac[off:off + 4])[0] & 0x7FFFFFFF) % 1000000)


class Totp:
    """A code generator that respects the server's REPLAY GUARD.

    myidsan refuses any step <= the last one it accepted, which is the point of the guard and
    is also a trap for a bench: two codes generated four seconds apart are the SAME code, and
    the second attempt fails for a reason that has nothing to do with the check being made.
    `fresh()` waits out the step; `repeat()` deliberately hands back the last one so the
    replay itself can be tested."""

    def __init__(self, secret):
        self.secret = secret
        self.last = -1
        self.last_code = ""

    def fresh(self):
        while _step_now() <= self.last:
            time.sleep(1)
        self.last = _step_now()
        self.last_code = _code_for(self.secret, self.last)
        return self.last_code

    def repeat(self):
        return self.last_code


# ---- clients -------------------------------------------------------------------------------

def bare(agent="kopiv2-bench/1.0"):
    """A cookie jar with a stable User-Agent — the challenge token is FINGERPRINTED against
    it, so a bench that lets requests pick its own default cannot test the binding."""
    c = Client(IDSAN_URL)
    c.s.headers["User-Agent"] = agent
    return c


def password_login(client, username="admin", password=BENCH_PASS):
    return client.s.post(IDSAN_URL + "/api/login/default",
                         json={"username": username, "password": password}, timeout=30)


def clear_guard():
    """Reset the per-IP lockout counter.

    The bench produces failed second-factor attempts ON PURPOSE — that IS the attempt-bound
    test — and myidsan counts them toward the same eight-in-five-minutes lockout a wrong
    password feeds. A SUCCESSFUL password check clears both counters (guardSuccess runs
    before the MFA fork), so one clean password POST buys the next batch of checks a clean
    slate. Without this the bench starts measuring the lockout instead of the thing under
    test — and would 'pass' its refusals for entirely the wrong reason."""
    password_login(bare())


def wait_out_lockout(limit=180):
    """Block until the shared lockout releases this address.

    Needed because the throttle test DELIBERATELY trips the lockout, and a tripped lockout
    is checked before the credential — so the account cannot clear it by signing in
    correctly, which is the point. Everything after that check has to wait it out rather
    than measure it."""
    deadline = time.time() + limit
    while time.time() < deadline:
        r = password_login(bare())
        if r.status_code != 429:
            return True
        time.sleep(float(r.headers.get("Retry-After") or 5))
    return False


def result_of(r):
    try:
        return r.json().get("result")
    except ValueError:
        return None


def field(html, name):
    m = re.search(r'name="%s"[^>]*value="([^"]*)"' % name, html)
    return m.group(1).replace("&amp;", "&") if m else ""


def main():
    # ---- (1) enrollment ---------------------------------------------------------------------
    c = admin(IDSAN_URL, *LOCAL_LOGIN["myidsan"])

    def api(path, body=None, method="POST"):
        return c.s.request(method, IDSAN_URL + path, json=body,
                           headers={"X-CSRF-Token": c.csrf()}, timeout=30)

    st = result_of(api("/api/mfa", method="GET")) or {}
    check("a fresh superadmin has no second factor", st.get("enrolled") is False, json.dumps(st))
    if st.get("enrolled"):
        print("\n  This bench needs a FRESH stand-up (admin must start with no factor).")
        print("  Re-run: python tools/fleetbench/idsan_harness.py\n")

    r = api("/api/mfa/enroll", {"label": "bench authenticator"})
    enroll = result_of(r) or {}
    secret = enroll.get("secret") or ""
    check("enrollment hands back a secret, an otpauth URI and a QR to scan",
          bool(secret) and enroll.get("otpauthUri", "").startswith("otpauth://totp/")
          and len(enroll.get("qrPngBase64") or "") > 500, brief(r))
    totp = Totp(secret)

    r = api("/api/mfa/enroll/verify", {"code": "000000"})
    check("a WRONG code does not confirm the factor", r.status_code != 200, brief(r))
    st = result_of(api("/api/mfa", method="GET")) or {}
    check("and the account is still un-enrolled after that refusal",
          st.get("enrolled") is False, json.dumps(st))

    r = api("/api/mfa/enroll/verify", {"code": totp.fresh()})
    codes = (result_of(r) or {}).get("recoveryCodes") or []
    check("the right code confirms the factor and mints recovery codes",
          r.status_code == 200 and len(codes) >= 8, "%d codes  %s" % (len(codes), brief(r)))

    st = result_of(api("/api/mfa", method="GET")) or {}
    check("status now reports an enrolled factor and its unused recovery codes",
          st.get("enrolled") is True and st.get("recoveryRemaining") == len(codes),
          json.dumps(st))

    r = api("/api/mfa/enroll", {"label": "second try"})
    check("a CONFIRMED factor cannot be silently re-enrolled over — the old secret would "
          "stop working with no one told", r.status_code != 200, brief(r))

    # ---- (2) the pre-session ordering, on BOTH legs -----------------------------------------
    #
    # This is the control. A kopiv2_access cookie is a federation-capable SSO session, so
    # issuing one before the code is verified would hand a password-only attacker every
    # relying app in the suite. Both sign-in legs must withhold it.

    spa = bare()
    r = password_login(spa)
    out = result_of(r) or {}
    check("the SPA leg: a correct password alone answers 'second factor required'",
          r.status_code == 200 and out.get("mfaRequired") is True and out.get("mfaToken"),
          brief(r))
    check("and sets NO session cookie — the challenge is not a session",
          not any(n.endswith("kopiv2_access") for n in spa.s.cookies.keys()),
          json.dumps(list(spa.s.cookies.keys())))
    check("and tells the client WHICH factor to prompt for, so a key-only account is not "
          "stranded at a code box", out.get("mfaMethods") == ["totp"], json.dumps(out.get("mfaMethods")))
    spa_token = out.get("mfaToken") or ""

    r = spa.s.get(IDSAN_URL + "/api/audit?limit=1", timeout=30)
    check("the half-authenticated client cannot reach an authenticated route",
          r.status_code != 200, brief(r))

    # The server-rendered leg is the one an SSO redirect actually lands on, and it is a
    # SEPARATE implementation (federated_auth.go, not login.go). Proving one says nothing
    # about the other.
    web = requests.Session()
    web.verify = False
    web.trust_env = False
    web.headers["User-Agent"] = "kopiv2-bench-browser/1.0"
    r = web.get(RELIER_URL + "/api/auth/start", allow_redirects=False, timeout=30)
    authorize_url = r.headers.get("Location", "")
    r = web.get(authorize_url, allow_redirects=False, timeout=30)
    hops = 0
    while r.status_code in (301, 302, 303, 307, 308) and hops < 6:
        nxt = r.headers["Location"]
        r = web.get(IDSAN_URL + nxt if nxt.startswith("/") else nxt, allow_redirects=False, timeout=30)
        hops += 1
    login_html = r.text
    r = web.post(IDSAN_URL + "/api/auth/login", allow_redirects=False, timeout=30, data={
        "username": "admin", "password": BENCH_PASS,
        "authCsrf": field(login_html, "authCsrf"), "continue": field(login_html, "continue"),
    })
    web_token = field(r.text, "mfaToken")
    check("the server-rendered leg (the one an SSO redirect lands on) renders the code page, "
          "not a session", r.status_code == 200 and bool(web_token), brief(r))
    check("and it too sets NO session cookie",
          not any(n.endswith("kopiv2_access") for n in web.cookies.keys()),
          json.dumps(list(web.cookies.keys())))

    # THE SHARP ONE. A password-only client that walks straight to /authorize must not get a
    # code. This is the "skip the challenge by going to the callback" attack, and it is the
    # single worst thing this ordering could get wrong.
    r = web.get(authorize_url, allow_redirects=False, timeout=30)
    loc = r.headers.get("Location", "")
    check("a password-only client that goes STRAIGHT to /authorize is sent back to sign in, "
          "not handed an authorization code",
          r.status_code in (302, 303) and "/api/auth/login" in loc and "code=" not in loc,
          "%d %s" % (r.status_code, loc[:120]))
    r = web.get(RELIER_URL + "/api/session/me", timeout=30)
    check("and the relying app never sees a session appear", r.status_code != 200, brief(r))

    # Now finish the SPA login properly.
    r = spa.s.post(IDSAN_URL + "/api/login/mfa",
                   json={"mfaToken": spa_token, "code": totp.fresh()}, timeout=30)
    check("token + code completes the login and issues the session that was withheld",
          r.status_code == 200 and any(n.endswith("kopiv2_access") for n in spa.s.cookies.keys()),
          brief(r))
    r = spa.s.get(IDSAN_URL + "/api/mfa", timeout=30)
    check("and that session works on an authenticated route", r.status_code == 200, brief(r))

    # ---- (5) the TOTP replay guard ----------------------------------------------------------
    #
    # Done here because it needs the code that was JUST spent. A code shoulder-surfed or read
    # out of a proxy log must not still be good for the rest of its 30-second window.
    clear_guard()
    replay = bare()
    tok = (result_of(password_login(replay)) or {}).get("mfaToken")
    r = replay.s.post(IDSAN_URL + "/api/login/mfa",
                      json={"mfaToken": tok, "code": totp.repeat()}, timeout=30)
    check("the SAME TOTP code cannot be spent twice, even inside its own 30-second window",
          r.status_code != 200
          and not any(n.endswith("kopiv2_access") for n in replay.s.cookies.keys()), brief(r))

    # ---- (3) the challenge token is credential-grade ----------------------------------------
    clear_guard()

    # Single use. The token spent by the SPA login above must be dead even with a good code.
    stale = bare()
    r = stale.s.post(IDSAN_URL + "/api/login/mfa",
                     json={"mfaToken": spa_token, "code": totp.fresh()}, timeout=30)
    check("a challenge token is SINGLE USE — replaying a spent one with a valid code fails",
          r.status_code != 200, brief(r))

    # Client binding. A token lifted from one browser must not drive from another.
    clear_guard()
    victim = bare("victim-browser/1.0")
    stolen = (result_of(password_login(victim)) or {}).get("mfaToken")
    thief = bare("thief-browser/9.9")
    r = thief.s.post(IDSAN_URL + "/api/login/mfa",
                     json={"mfaToken": stolen, "code": totp.fresh()}, timeout=30)
    check("a challenge token lifted to a DIFFERENT client is refused — it is bound to the "
          "one it was issued to", r.status_code != 200
          and not any(n.endswith("kopiv2_access") for n in thief.s.cookies.keys()), brief(r))

    # Attempt bound. Five wrong codes must burn the token, so a captured one cannot be ground
    # against a million codes. The CORRECT code afterwards is the only proof that the token
    # died rather than the guesses merely failing.
    clear_guard()
    grinder = bare("grinder/1.0")
    burn = (result_of(password_login(grinder)) or {}).get("mfaToken")
    tries = []
    for i in range(5):
        rr = grinder.s.post(IDSAN_URL + "/api/login/mfa",
                            json={"mfaToken": burn, "code": "%06d" % (111111 * (i + 1) % 1000000)},
                            timeout=30)
        tries.append(rr.status_code)
    good = totp.fresh()
    r = grinder.s.post(IDSAN_URL + "/api/login/mfa", json={"mfaToken": burn, "code": good}, timeout=30)
    check("five wrong codes BURN the challenge token — the correct code afterwards is refused "
          "too, so a captured token cannot be ground against 10^6 codes",
          r.status_code != 200
          and not any(n.endswith("kopiv2_access") for n in grinder.s.cookies.keys()),
          "wrong-code attempts %s, then the right code: %s" % (tries, brief(r)))

    # ---- (4) recovery codes -----------------------------------------------------------------
    clear_guard()
    before = (result_of(api("/api/mfa", method="GET")) or {}).get("recoveryRemaining")
    rescue = bare()
    tok = (result_of(password_login(rescue)) or {}).get("mfaToken")
    r = rescue.s.post(IDSAN_URL + "/api/login/mfa", json={"mfaToken": tok, "code": codes[0]}, timeout=30)
    check("a recovery code completes a login when the authenticator is gone — the whole "
          "point of holding them", r.status_code == 200
          and any(n.endswith("kopiv2_access") for n in rescue.s.cookies.keys()), brief(r))

    after = (result_of(api("/api/mfa", method="GET")) or {}).get("recoveryRemaining")
    check("and spending one draws the remaining count down by exactly one",
          before is not None and after == before - 1, "%s -> %s" % (before, after))

    clear_guard()
    again = bare()
    tok = (result_of(password_login(again)) or {}).get("mfaToken")
    r = again.s.post(IDSAN_URL + "/api/login/mfa", json={"mfaToken": tok, "code": codes[0]}, timeout=30)
    check("THE SAME recovery code cannot be used a second time — single-use is what makes a "
          "written-down code survivable", r.status_code != 200
          and not any(n.endswith("kopiv2_access") for n in again.s.cookies.keys()), brief(r))

    r = api("/api/mfa/recovery", {"code": "000000"})
    check("regenerating the recovery set requires proving the factor first — a hijacked "
          "session must not be able to quietly rotate the break-glass codes",
          r.status_code != 200, brief(r))

    r = api("/api/mfa/recovery", {"code": totp.fresh()})
    fresh_codes = (result_of(r) or {}).get("recoveryCodes") or []
    check("with a valid code it issues a fresh set",
          r.status_code == 200 and len(fresh_codes) >= 8 and set(fresh_codes) != set(codes),
          brief(r))

    clear_guard()
    old = bare()
    tok = (result_of(password_login(old)) or {}).get("mfaToken")
    r = old.s.post(IDSAN_URL + "/api/login/mfa", json={"mfaToken": tok, "code": codes[1]}, timeout=30)
    check("and the OLD set is dead — regenerating that left the printout still working would "
          "be worse than not regenerating", r.status_code != 200, brief(r))

    # ---- (6) step-up ------------------------------------------------------------------------
    clear_guard()
    op = bare("operator/1.0")
    tok = (result_of(password_login(op)) or {}).get("mfaToken")
    op.s.post(IDSAN_URL + "/api/login/mfa", json={"mfaToken": tok, "code": totp.fresh()}, timeout=30)

    def opapi(path, body=None, method="POST"):
        return op.s.request(method, IDSAN_URL + path, json=body,
                            headers={"X-CSRF-Token": op.csrf()}, timeout=30)

    st = result_of(opapi("/api/step-up", method="GET")) or {}
    check("a session that has just completed a FULL sign-in is still not elevated — step-up "
          "is a separate, short-lived proof", st.get("elevated") is False, json.dumps(st))

    export_body = {"sections": ["identity"], "passphrase": EXPORT_PASSPHRASE}
    r = opapi("/api/backup/export", export_body)
    check("exporting the identity store is refused without a fresh credential, with the "
          "sentinel the UI needs to PROMPT rather than show a dead end",
          r.status_code == 403 and "step_up_required" in r.text, brief(r))
    r = opapi("/api/backup/restore", {"dataBase64": "", "passphrase": EXPORT_PASSPHRASE})
    check("so is restoring over it", r.status_code == 403 and "step_up_required" in r.text, brief(r))
    me = (result_of(opapi("/api/auth/session", method="GET")) or {}).get("userId") or 1
    r = opapi("/api/mfa-admin/%s" % me, method="DELETE")
    check("and so is clearing an account's second factor — the exact move an attacker with a "
          "stolen cookie makes to take the account over",
          r.status_code == 403 and "step_up_required" in r.text, brief(r))
    r = opapi("/api/backup/preview", {"dataBase64": "", "passphrase": EXPORT_PASSPHRASE})
    check("but PREVIEW is not gated — making an operator re-authenticate to LOOK before they "
          "leap only teaches them to skip the looking", r.status_code != 403, brief(r))

    r = opapi("/api/step-up", {"password": BENCH_PASS})
    check("THE PASSWORD ALONE DOES NOT ELEVATE an account that has a second factor — a "
          "phished password is precisely what the attacker already holds",
          r.status_code != 200, brief(r))
    st = result_of(opapi("/api/step-up", method="GET")) or {}
    check("and the session is still not elevated after that refusal",
          st.get("elevated") is False, json.dumps(st))

    r = opapi("/api/step-up", {"password": "not-the-password-at-all", "code": totp.fresh()})
    check("a valid code with the WRONG password does not elevate either — both halves are "
          "required", r.status_code != 200, brief(r))

    r = opapi("/api/step-up", {"password": BENCH_PASS, "code": totp.fresh()})
    check("password + code elevates the session", r.status_code == 200
          and (result_of(r) or {}).get("elevated") is True, brief(r))

    r = opapi("/api/backup/export", export_body)
    blob = (result_of(r) or {}).get("dataBase64") or ""
    check("and the export the gate was refusing now runs",
          r.status_code == 200 and len(blob) > 100, brief(r))

    # Elevation is attached to ONE SESSION, not to the account. If it leaked to every session
    # the user holds, the stolen cookie the control exists to stop would be elevated too — by
    # the legitimate operator, from their own desk, without either of them knowing.
    clear_guard()
    other = bare("second-session/1.0")
    tok = (result_of(password_login(other)) or {}).get("mfaToken")
    other.s.post(IDSAN_URL + "/api/login/mfa", json={"mfaToken": tok, "code": totp.fresh()}, timeout=30)
    r = other.s.get(IDSAN_URL + "/api/step-up", timeout=30)
    check("elevation belongs to the ONE session that proved it — a second session of the same "
          "account is not elevated", (result_of(r) or {}).get("elevated") is False, brief(r))
    r = other.s.post(IDSAN_URL + "/api/backup/export", json=export_body,
                     headers={"X-CSRF-Token": other.csrf()}, timeout=30)
    check("and that second session is still refused the export",
          r.status_code == 403 and "step_up_required" in r.text, brief(r))

    # THROTTLING. Step-up takes a password, so it is a password-checking endpoint — and every
    # OTHER password-checking endpoint on this server is behind the lockout. If this one is
    # not, an attacker holding only a stolen cookie has an unmetered oracle to guess the
    # password they are missing, which is the entire credential this control rests on.
    clear_guard()
    started = time.time()
    codes_seen = []
    throttled = None
    for i in range(12):
        rr = opapi("/api/step-up", {"password": "wrong-guess-%02d" % i, "code": "000000"})
        codes_seen.append(rr.status_code)
        if rr.status_code == 429:
            throttled = rr
            break
    elapsed = time.time() - started
    check("repeated WRONG passwords at step-up are throttled the way repeated wrong logins "
          "are — otherwise a stolen cookie is an unmetered password-guessing oracle",
          429 in codes_seen,
          "%d attempts in %.1fs, statuses %s" % (len(codes_seen), elapsed, codes_seen))
    # The counter alone is not the whole control: eight guesses at wire speed is a very
    # different proposition from eight slow ones when the candidate list is a breach dump.
    check("and each wrong guess is DELAYED, so the attempts before the lockout are not free",
          elapsed >= 0.3 * len(codes_seen),
          "%.1fs across %d attempts" % (elapsed, len(codes_seen)))
    # The SPA renders this text verbatim inside the re-authentication prompt. Telling a
    # signed-in operator that there were "too many failed LOGIN attempts" describes
    # something they did not do, and a wait with no duration is a dead end in a modal they
    # are already blocked by.
    body = throttled.text if throttled is not None else ""
    check("and the refusal says what was throttled and for how long, in the words the "
          "re-authentication prompt will show",
          "login attempts" not in body and "re-authentication" in body
          and (throttled is not None and throttled.headers.get("Retry-After")),
          body[:160])

    r = opapi("/api/backup/export", export_body)
    check("a step-up lockout does not take the ALREADY-elevated session down with it — the "
          "operator mid-task is not collateral", r.status_code == 200, brief(r))
    if not wait_out_lockout():
        print("  (the lockout did not release in time; later checks may be measuring it)")

    # ---- (7) the lost-device escape hatch ---------------------------------------------------
    #
    # The documented recovery for "the sole superadmin lost the authenticator AND the recovery
    # codes" is a RESET_MFA marker in the data dir. It has never been exercised. Two halves
    # matter and they pull in opposite directions: it must WORK (or the install is bricked),
    # and it must NOT be reachable by the locked-out party over HTTP (or it is the bypass).
    #
    # The refusal has to be tested in a state where ONLY the thing under test can refuse.
    # A client with no session also has no CSRF cookie, so a naive attempt here is turned
    # away by the CSRF check and proves nothing about authorization at all. So the challenge
    # token is planted AS the session cookie, and a matching CSRF pair is planted alongside
    # it: everything else about the request is now well-formed, and the only thing left that
    # can refuse is the one being measured.
    clear_guard()
    locked_out = bare("locked-out/1.0")
    tok = (result_of(password_login(locked_out)) or {}).get("mfaToken") or ""
    locked_out.s.cookies.set("__Host-kopiv2_access", tok, domain=IDSAN_URL.split("//")[1].split(":")[0])
    locked_out.s.cookies.set("__Host-kopiv2_csrf", "bench-csrf", domain=IDSAN_URL.split("//")[1].split(":")[0])
    hdr = {"X-CSRF-Token": "bench-csrf"}

    r = locked_out.s.get(IDSAN_URL + "/api/mfa", timeout=30)
    check("the challenge token is NOT a session — presented as the access cookie it opens "
          "nothing", r.status_code != 200, brief(r))
    r = locked_out.s.delete(IDSAN_URL + "/api/mfa", json={"password": BENCH_PASS},
                            headers=hdr, timeout=30)
    check("someone holding ONLY the password cannot clear the factor over HTTP",
          r.status_code != 200 and "csrf" not in r.text.lower(), brief(r))
    r = locked_out.s.delete(IDSAN_URL + "/api/mfa-admin/1", headers=hdr, timeout=30)
    check("nor reach the administrator's reset with a challenge token instead of a session",
          r.status_code != 200 and "csrf" not in r.text.lower(), brief(r))

    # Self-service teardown, from a fully signed-in and elevated session, still needs BOTH.
    r = opapi("/api/mfa", {"password": BENCH_PASS, "code": "000000"}, method="DELETE")
    check("even a fully signed-in session needs a VALID CODE to remove its own factor — "
          "otherwise a hijacked session strips the control it exists to defeat",
          r.status_code != 200, brief(r))
    r = opapi("/api/mfa", {"password": "wrong", "code": totp.fresh()}, method="DELETE")
    check("and the current password as well", r.status_code != 200, brief(r))

    # Now the marker itself. Written from the HOST — a locked-out user has no way to put it
    # there, and that asymmetry IS the control.
    marker = os.path.join(ROOT, "idsan", "RESET_MFA")
    io.open(marker, "w").write("")
    sh("docker", "restart", "idsan")
    up = wait_up(IDSAN_URL + "/api/auth/session")
    check("the identity server comes back after the reset marker is dropped", up)
    check("the marker is CONSUMED, not left to re-clear the factor on every later restart",
          not os.path.exists(marker), marker)
    recovered = bare("recovered/1.0")
    r = password_login(recovered)
    out = result_of(r) or {}
    check("and the locked-out superadmin can sign in with the password alone again — the "
          "documented escape hatch actually works, from the host and only from the host",
          r.status_code == 200 and not out.get("mfaRequired")
          and any(n.endswith("kopiv2_access") for n in recovered.s.cookies.keys()), brief(r))

    # Re-enrol, because a bench that only ever adds is a bench that has not tested the
    # lifecycle. W3-5a passed 44/44 without once deleting anything.
    r = recovered.s.post(IDSAN_URL + "/api/mfa/enroll", json={"label": "after recovery"},
                         headers={"X-CSRF-Token": recovered.csrf()}, timeout=30)
    secret2 = (result_of(r) or {}).get("secret") or ""
    totp2 = Totp(secret2)
    r = recovered.s.post(IDSAN_URL + "/api/mfa/enroll/verify", json={"code": totp2.fresh()},
                         headers={"X-CSRF-Token": recovered.csrf()}, timeout=30)
    check("and the account can enrol a fresh factor afterwards — the reset cleared the row, "
          "it did not wedge it", r.status_code == 200
          and len((result_of(r) or {}).get("recoveryCodes") or []) >= 8, brief(r))

    # And now REMOVE it properly, with both halves. Every check above this point that
    # touched teardown was a refusal, and a bench made only of refusals never finds out
    # whether the thing it is refusing actually works — W3-5a passed 44/44 without once
    # deleting a preset, a tour or a camera.
    r = recovered.s.delete(IDSAN_URL + "/api/mfa",
                           json={"password": BENCH_PASS, "code": totp2.fresh()},
                           headers={"X-CSRF-Token": recovered.csrf()}, timeout=30)
    check("with the password AND a valid code, a user can remove their own second factor",
          r.status_code == 200, brief(r))
    st = result_of(recovered.s.get(IDSAN_URL + "/api/mfa", timeout=30)) or {}
    check("and the account is genuinely back to no factor and no recovery codes",
          st.get("enrolled") is False and st.get("recoveryRemaining") == 0, json.dumps(st))
    clear_guard()
    plain = bare("after-teardown/1.0")
    r = password_login(plain)
    check("so the password alone signs in again — teardown is a real teardown, not a "
          "status flag with the gate still standing",
          r.status_code == 200 and not (result_of(r) or {}).get("mfaRequired")
          and any(n.endswith("kopiv2_access") for n in plain.s.cookies.keys()), brief(r))

    # ---- (8) the trail ----------------------------------------------------------------------
    #
    # THE ENVELOPE TRAP: /api/audit answers {data:{result:[…]}}, not {result:{items:[…]}}.
    # Reading the wrong key returns zero entries for a trail that is working perfectly — a
    # check that FAILS ON CORRECT OUTPUT. `result_list` is the helper that exists for this.
    trail = result_list(recovered.s.get(IDSAN_URL + "/api/audit?limit=300", timeout=30), "items")
    actions = {}
    for row in trail:
        actions[row.get("action")] = actions.get(row.get("action"), 0) + 1
    seen = json.dumps(actions)
    check("the trail is readable at all", len(trail) > 0, seen)

    check("a failed step-up is recorded — it is exactly what an attacker holding only a "
          "stolen cookie produces while trying to escalate",
          actions.get("stepup.failure", 0) > 0, seen)
    check("and a successful one, so the export below it has a cause",
          actions.get("stepup.success", 0) > 0, seen)
    check("the export of the whole identity store is recorded",
          actions.get("backup.export", 0) > 0, seen)

    # The MFA lifecycle. On an identity server "who removed the second factor from this
    # account, and when" cannot be answered any other way — the row is simply gone.
    check("ENROLLING a second factor is recorded", actions.get("mfa.enroll", 0) > 0, seen)
    check("REMOVING one is recorded — a hijacked session stripping the second factor is the "
          "single most important line in this trail, and the row it deletes is the only other "
          "evidence it ever existed", actions.get("mfa.disable", 0) > 0, seen)
    check("SPENDING A RECOVERY CODE is recorded — break-glass being used is the event an "
          "operator most needs to see, and it looks identical to a normal sign-in otherwise",
          actions.get("mfa.recovery_used", 0) > 0, seen)
    check("REGENERATING the recovery set is recorded — quietly rotating the break-glass codes "
          "is how an attacker locks the real owner out", actions.get("mfa.recovery_regenerate", 0) > 0, seen)
    check("a failed SECOND-FACTOR challenge is recorded distinctly from a failed password — "
          "someone grinding codes already has the password, which is a different incident",
          actions.get("login.mfa_challenge", 0) > 0, seen)
    check("and the step-up lockout is recorded when it engages, so a burst of guessing "
          "against a live session is visible after the fact",
          actions.get("login.lockout", 0) > 0, seen)

    # The escape hatch is the one path that strips the second factor from the most
    # privileged account with nobody signed in, and it deletes the only other evidence the
    # factor existed. An application-log line is not the trail an operator reviews.
    hatch = [row for row in trail
             if row.get("action") == "mfa.admin_reset" and "RESET_MFA" in (row.get("detail") or "")]
    check("the RESET_MFA escape hatch writes itself into the SECURITY trail, not only the "
          "application log — it clears a superadmin's second factor with nobody signed in",
          len(hatch) > 0, json.dumps(hatch[:1]) if hatch else seen)

    return report()


if __name__ == "__main__":
    sys.exit(main())
