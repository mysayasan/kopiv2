# myidsan bench: the brute-force lockout, under the deployment customers actually run.
#
# WHY THIS ONE. myidsan authenticates the whole suite, so its failed-login lockout is the
# control standing between a password list and every application in the estate. It had unit
# tests for its arithmetic and nothing that had ever attacked it.
#
# WHAT MAKES THIS BENCH DIFFERENT FROM THE OTHER THREE: it stands up a CLUSTER — two myidsan
# instances on ONE shared Postgres and ONE shared Redis — because that is a documented,
# supported, wizard-declarable deployment ("Tier A, genuinely clusterable" in docs/HOWTO.md,
# with a Settings panel for declaring it) and because a lockout is exactly the kind of state
# that quietly stops working when there are two of you. A single-instance bench cannot see
# any of it.
#
# It also runs with the SHIPPED configuration rather than the harness's convenience one: the
# generic rate limiter ON, the login-security block at its defaults. "Under real attack"
# means the configuration that is actually deployed, and a bench that disables the other
# throttles measures a server nobody runs.
#
# THE CLAIMS UNDER TEST:
#
#   1. the lockout is DEPLOYMENT-WIDE — locking an account on one instance locks it on the
#      other, with no free guesses in between, and the escalating backoff escalates across
#      instances rather than restarting at the base on each hop;
#   2. it is a lockout, not a filter: while locked, even the CORRECT password is refused;
#   3. a successful sign-in clears the counters everywhere, so a user who mistypes a few
#      times and then gets it right is not left one attempt from being shut out;
#   4. it is keyed on the ACCOUNT as well as the source, so a spray distributed across
#      addresses against one known username is throttled — and the cost of that (a known
#      username can be held locked by a stranger) is measured rather than assumed;
#   5. every OTHER endpoint that checks the account password is behind the same lockout.
#      The front door being guarded is worth little if change-password, second-factor
#      teardown, security-key removal and step-up are each an unmetered password oracle for
#      whoever holds a stolen cookie;
#   6. the password policy refuses what it says it refuses, on every path that sets one.
#
#   python tools/fleetbench/idsan_harness.py        # for the network, the cert and the image
#   python tools/fleetbench/bench_idsan_lockout.py  # stands up its own cluster, tears it down
import base64
import hashlib
import hmac
import io
import json
import os
import re
import shutil
import struct
import subprocess
import sys
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import ROOT, sh, wait_up
from idsan_harness import (ADMIN_USER, ADMIN_PASS, BENCH_PASS, INTERNAL_TOKEN, LOCAL_LOGIN,
                           NET, WORK, Client, admin, app_config, host_ip, start, write)

urllib3.disable_warnings()
CHECKS = []

PG = "idsan-pg"
NODES = [("idsan-a", 3011, 18461), ("idsan-b", 3012, 18462)]
HOST = host_ip()
A = "https://%s:%d" % (HOST, 18461)
B = "https://%s:%d" % (HOST, 18462)
# The in-container address of instance A, for driving it from a SECOND source address.
A_INTERNAL = "https://idsan-a:3011"

# Shipped defaults, restated here so a check that depends on them fails loudly if they move.
MAX_ATTEMPTS = 8
BASE_LOCKOUT = 60


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


def is_lockout(r):
    """A 429 from the LOCKOUT, not from the generic per-endpoint rate limiter.

    Both answer 429 and both set Retry-After, and the bench runs with the rate limiter ON
    because that is the shipped posture — so "got a 429" proves nothing on its own. A run of
    this bench against UNFIXED code passed two throttle checks purely on rate-limit refusals
    while the lockout it was supposed to be measuring did not exist at all. The lockout body
    carries retryAfterSeconds; the rate limiter says "rate limit exceeded"."""
    if r.status_code != 429:
        return False
    return "retryAfterSeconds" in r.text and "rate limit exceeded" not in r.text


def status_of(r):
    """429 only when it is the lockout; a rate-limit refusal is reported as 'rl' so it can
    never be mistaken for one in a status list."""
    if r.status_code == 429:
        return 429 if is_lockout(r) else "rl"
    return r.status_code


def brief(r):
    return "%d %s" % (r.status_code, r.text[:150].replace("\n", " "))


# ---- the cluster -------------------------------------------------------------------------

def start_cluster():
    """Two myidsan instances, ONE Postgres, ONE Redis.

    The shared database is what makes these one deployment rather than two servers: the
    account being attacked has to be the SAME account, or the bench proves nothing about
    clustering. The shared cache is what a real clustered install already needs for sessions
    — the preflight checklist makes it a blocker — so requiring it here invents no new
    dependency."""
    for name, _, _ in NODES:
        sh("docker", "rm", "-f", name, check=False)
        shutil.rmtree(os.path.join(ROOT, name), ignore_errors=True)
    sh("docker", "rm", "-f", PG, check=False)

    sh("docker", "run", "-d", "--name", PG, "--network", NET,
       "-e", "POSTGRES_PASSWORD=benchpg", "-e", "POSTGRES_USER=postgres",
       "-e", "POSTGRES_DB=postgres", "postgres:16-alpine")
    for _ in range(60):
        out = sh("docker", "exec", PG, "pg_isready", "-U", "postgres", check=False)
        if out and "accepting connections" in str(out):
            break
        time.sleep(2)

    for name, tls, hostport in NODES:
        data = os.path.join(ROOT, name)
        certs = os.path.join(data, "certs")
        os.makedirs(certs, exist_ok=True)
        for src, dst in ((os.path.join(WORK, "bench.crt"), "cert.pem"),
                         (os.path.join(WORK, "bench.key"), "key.pem")):
            io.open(os.path.join(certs, dst), "wb").write(io.open(src, "rb").read())
        cfg = app_config("myidsan", tls)
        cfg["db"] = {"engine": "postgres", "host": PG, "port": 5432, "user": "postgres",
                     "password": "benchpg", "db_name": "idsanbench", "ssl_mode": "disable"}
        cfg["cache"] = {"provider": "redis",
                        "redis": {"address": "idsan-redis:6379", "password": "", "db": 0}}
        cfg["sso"]["internalToken"] = INTERNAL_TOKEN
        # ONE jwt secret across the cluster. Left empty each instance mints its own, and a
        # session issued by one is rejected by the other as a bad signature — which is why
        # the clustering preflight makes this a checklist row, and which this bench walked
        # straight into before setting it.
        cfg["jwt"] = {"secret": "bench-cluster-jwt-secret-3f9a1c7e5b2d4086"}
        # The SHIPPED posture, not the harness's convenience one.
        cfg["rateLimit"]["enabled"] = True
        write(os.path.join(data, "config.json"), cfg)
        start(name, "myidsan", tls, hostport)

    ok = True
    for name, _, hostport in NODES:
        url = "https://%s:%d" % (HOST, hostport)
        if not wait_up(url + "/api/auth/session"):
            print(sh("docker", "logs", "--tail", "30", name, check=False))
            ok = False
    return ok


def teardown_cluster():
    for name, _, _ in NODES:
        sh("docker", "rm", "-f", name, check=False)
        shutil.rmtree(os.path.join(ROOT, name), ignore_errors=True)
    sh("docker", "rm", "-f", PG, check=False)


# ---- drivers -----------------------------------------------------------------------------

def attempt(base, username, password):
    """One sign-in attempt from the HOST's address. Returns the response."""
    s = requests.Session()
    s.verify = False
    s.trust_env = False
    return s.post(base + "/api/login/default",
                  json={"username": username, "password": password}, timeout=30)


def attempt_from_container(username, password):
    """One sign-in attempt from a DIFFERENT source address.

    Every request the bench makes from the host arrives with one address, so the per-source
    and per-account counters move together and neither can be told apart from the other.
    Driving instance A from inside another container on the same docker network gives a
    genuinely different source IP, which is the only way to show that the ACCOUNT counter
    exists and does its job — that is the half that stops a spray distributed across
    addresses, and the half that carries the denial-of-service cost."""
    body = json.dumps({"username": username, "password": password})
    out = subprocess.run(
        ["docker", "exec", PG, "sh", "-c",
         'wget -q -O- --no-check-certificate --header="Content-Type: application/json" '
         "--post-data='%s' %s/api/login/default 2>&1 || true" % (body, A_INTERNAL)],
        capture_output=True, text=True, timeout=60).stdout
    m = re.search(r"HTTP/1\.\d (\d{3})", out)
    if m:
        return int(m.group(1))
    # busybox wget prints the BODY on success and the status line only on an error, so
    # anything that parsed as a result envelope is a 200.
    return 200 if '"message"' in out else 0


def signed_in(base, username, password):
    s = requests.Session()
    s.verify = False
    s.trust_env = False
    r = s.post(base + "/api/login/default", json={"username": username, "password": password}, timeout=30)
    return r, s


def clear_guard(base, username, password):
    """A correct sign-in resets both counters for that account and this source.

    The bench produces failed attempts on purpose and they all share one source key, so
    without this every later check ends up measuring the leftovers of an earlier one."""
    return attempt(base, username, password).status_code


def burn(base, username, n, tag="x"):
    """n wrong passwords, stopping early if the lockout engages. Returns the status list."""
    out = []
    for i in range(n):
        r = attempt(base, username, "%s-wrong-%02d" % (tag, i))
        out.append(status_of(r))
        if is_lockout(r):
            break
    return out


def wait_until_unlocked(base, username, limit=400):
    """Wait out a lockout, probing with a WRONG password.

    Probing with the CORRECT one would clear the counters as a side effect — a successful
    sign-in resets the escalation, which is intended behaviour and exactly what must NOT
    happen in the middle of measuring escalation. A locked request never reaches the
    credential check, so a wrong password costs nothing while locked and merely reports
    whether the door is open yet."""
    deadline = time.time() + limit
    while time.time() < deadline:
        r = attempt(base, username, "probe-still-locked")
        if r.status_code != 429:
            return True
        time.sleep(min(float(r.headers.get("Retry-After") or 5), 15))
    return False


def reset_guard(base, username, password, limit=700):
    """Return the deployment to a clean slate for this account AND this source address.

    The source key is shared by every attempt the bench makes, so without an explicit reset
    each phase inherits the previous phase's failures and eventually measures the leftovers
    instead of the thing under test. A successful sign-in clears both keys; if the door is
    still shut, wait for it first."""
    deadline = time.time() + limit
    while time.time() < deadline:
        r = attempt(base, username, password)
        if r.status_code == 200:
            return True
        if r.status_code != 429:
            # Not locked, but not accepted either — a wrong password would be a bench bug,
            # so surface it rather than spinning until the deadline.
            print("    reset_guard: unexpected %s" % brief(r))
            return False
        time.sleep(min(float(r.headers.get("Retry-After") or 5) + 1, 30))
    return False


def cooldown_rate_limit(seconds=62):
    """Let the GENERIC per-endpoint rate limiter's window roll over.

    Two different throttles answer 429 here and they mask each other. /api/login/* shares one
    public bucket of 30 requests a minute, and this bench spends most of it signing in and
    failing to — so by the time it probes change-password (which lives under the same prefix)
    the bucket is empty and the rate limiter refuses first, hiding whether the LOCKOUT would
    have engaged. Both throttles are real and both are wanted; they simply cannot be measured
    in the same minute. step-up and the security-key routes have their own buckets, which is
    why only this one needs the pause."""
    time.sleep(seconds)


def csrf_post(client, base, path, body, method="POST"):
    return client.s.request(method, base + path, json=body,
                            headers={"X-CSRF-Token": client.csrf()}, timeout=30)


def main():
    check("a two-instance cluster stands up on one shared database and one shared cache",
          start_cluster())

    # The stock admin is seeded once, into the SHARED database, so both instances serve the
    # same account — which is what makes the attack below an attack on one deployment.
    op = admin(A, *LOCAL_LOGIN["myidsan"])
    check("the shared account is reachable from BOTH instances — without that this is two "
          "servers, not a cluster",
          attempt(A, ADMIN_USER, BENCH_PASS).status_code == 200
          and attempt(B, ADMIN_USER, BENCH_PASS).status_code == 200)

    # A second account, so tests that must leave an account locked do not strand the
    # operator account the rest of the bench needs.
    victim = "victim@bench.test"
    other = "bystander@bench.test"
    for email in (victim, other):
        r = csrf_post(op, A, "/api/user-credential", {
            "email": email, "userpwd": BENCH_PASS, "firstName": "Bench", "lastName": "User",
            "isActive": True})
        check("an account exists to attack (%s)" % email, r.status_code == 200, brief(r))

    # ---- (6) the password policy ------------------------------------------------------------
    r = csrf_post(op, A, "/api/user-credential", {
        "email": "weak@bench.test", "userpwd": "short", "isActive": True})
    check("an administrator cannot create an account below the password policy — this was "
          "once the ONE password path with no check at all",
          r.status_code == 400 and "12" in r.text, brief(r))
    r = csrf_post(op, A, "/api/user-credential", {
        "email": "common@bench.test", "userpwd": "password1234", "isActive": True})
    check("and a common password is refused even when it is long enough",
          r.status_code == 400, brief(r))
    r = requests.get(A + "/api/login/password-policy", verify=False, timeout=30)
    policy = (r.json() or {}).get("result") or {}
    check("the rules are published so a form can state them BEFORE the user types",
          r.status_code == 200 and policy.get("minLength") == 12, brief(r))

    # ---- (1) the lockout is deployment-wide -------------------------------------------------
    #
    # THE claim. Each instance keeps its own counters unless they are deliberately shared, so
    # a cluster is where a lockout quietly stops being one.
    codes = burn(A, victim, MAX_ATTEMPTS + 2, "a")
    check("instance A locks the account after the configured number of wrong passwords",
          codes.count(429) == 1 and len(codes) == MAX_ATTEMPTS + 1,
          "%s (max attempts %d)" % (codes, MAX_ATTEMPTS))

    r = attempt(B, victim, "still-wrong")
    check("AND INSTANCE B REFUSES IT IMMEDIATELY — the account is locked out of the "
          "DEPLOYMENT, not merely out of one process. Per-process counters would hand an "
          "attacker a fresh budget on every instance behind the load balancer",
          is_lockout(r), brief(r))
    check("with a Retry-After the client can act on", bool(r.headers.get("Retry-After")),
          r.headers.get("Retry-After", "(none)"))

    # ---- (2) it is a lockout, not a filter --------------------------------------------------
    r = attempt(B, victim, BENCH_PASS)
    check("while locked, even the CORRECT password is refused — otherwise an attacker who "
          "guessed right on the last attempt before the lockout would still be let in",
          is_lockout(r), brief(r))

    # ---- (4) the account key, seen from a second address ------------------------------------
    sc = attempt_from_container(victim, "wrong-from-elsewhere")
    check("the same account is locked from a DIFFERENT source address too — the counter is "
          "keyed on the account as well as the source, which is what throttles a spray "
          "distributed across many addresses", sc == 429, "status %s" % sc)
    sc = attempt_from_container(other, "wrong-for-a-bystander")
    check("but a DIFFERENT account from that address is not — the lockout is scoped to who "
          "is being attacked, not to everyone who shares the network", sc == 401,
          "status %s" % sc)

    # The cost of (4), measured rather than assumed. Locking a known username out is
    # something a stranger can do; the design accepts that on the grounds that unlimited
    # guessing against a known account is the worse outcome.
    r = attempt(A, victim, BENCH_PASS)
    retry = int(r.headers.get("Retry-After") or 0)
    check("the denial-of-service this buys is bounded and reported: a stranger who knows a "
          "username can hold it locked, and the wait is stated rather than open-ended",
          is_lockout(r) and 0 < retry <= BASE_LOCKOUT + 5,
          "Retry-After %ds against a base lockout of %ds" % (retry, BASE_LOCKOUT))

    # ---- (1b) escalation crosses instances --------------------------------------------------
    #
    # Waited out with a WRONG password on purpose. Signing in correctly would clear the
    # escalation counter — which is right, and is exactly what must not happen mid-measurement.
    check("the first lockout expires on its own", wait_until_unlocked(A, victim))
    burn(B, victim, MAX_ATTEMPTS + 1, "b")
    r = attempt(A, victim, "wrong-again")
    second = int(r.headers.get("Retry-After") or 0)
    check("a SECOND lockout, tripped on the OTHER instance, backs off further than the "
          "first — the escalation counter is shared, so an attacker rotating instances "
          "cannot reset the backoff to its base on every hop",
          is_lockout(r) and second > retry,
          "first %ds, second %ds" % (retry, second))

    # ---- (3) success clears the counters everywhere -----------------------------------------
    check("the escalated lockout expires too", wait_until_unlocked(A, victim, limit=400))
    check("a correct sign-in is accepted again once it has", reset_guard(A, victim, BENCH_PASS))

    check("the bystander account starts from a clean slate", reset_guard(A, other, BENCH_PASS))
    codes = burn(A, other, MAX_ATTEMPTS - 1, "c")
    check("a run of failures one short of the threshold does not lock", 429 not in codes,
          json.dumps(codes))
    check("a correct sign-in on ONE instance clears the counters the OTHER instance can "
          "see — a user who mistypes a few times and then gets it right must not be left "
          "one attempt away from being shut out",
          attempt(B, other, BENCH_PASS).status_code == 200)
    codes = burn(B, other, MAX_ATTEMPTS - 1, "d")
    check("and the proof is that a second run of near-threshold failures still does not "
          "lock — the counter really was reset, not merely quiet", 429 not in codes,
          json.dumps(codes))
    check("clean slate again before the next phase", reset_guard(A, other, BENCH_PASS))

    # ---- (5) the other endpoints that check the same password -------------------------------
    #
    # An attacker holding a stolen cookie cannot sign in — they do not have the password.
    # Every authenticated endpoint that CHECKS the password is a way for them to go looking
    # for it, and the front door being guarded is worth little if these are not.
    #
    # THE TRAP HERE, hit on the first run of this bench: if the session is not actually
    # established, every one of these endpoints answers 401 "not authenticated" and the loop
    # below reads as "no lockout" — a check that fails for a reason that has nothing to do
    # with throttling. So the session is asserted first, and each probe asserts it is being
    # refused for the CREDENTIAL rather than for the cookie.
    cooldown_rate_limit()
    holder = Client(A)
    r = holder.s.post(A + "/api/login/default",
                      json={"username": other, "password": BENCH_PASS}, timeout=30)
    check("a stolen-cookie attacker's starting position: a valid session, no password",
          r.status_code == 200 and any(n.endswith("kopiv2_access") for n in holder.s.cookies.keys()),
          brief(r))

    # A cluster is only a cluster if a session issued by one instance is honoured by the
    # other; without that a load balancer signs people out at random.
    r = holder.s.get(B + "/api/mfa", timeout=30)
    check("a session issued by instance A is honoured by instance B — the load balancer "
          "may land any request anywhere", r.status_code == 200, brief(r))

    oracles = [
        ("change-password", "/api/login/default/change-password", "POST",
         lambda i: {"currentPassword": "guess-%02d" % i, "newPassword": "Bench!9876543"}),
        ("step-up", "/api/step-up", "POST",
         lambda i: {"password": "guess-%02d" % i}),
        # Security-key removal re-proves identity BEFORE it looks up whether the key
        # exists, so any id works and no second factor is needed to reach the password
        # check — which makes it the one of these three that a session thief can drive
        # from a standing start.
        ("security-key removal", "/api/mfa/webauthn/999", "DELETE",
         lambda i: {"password": "guess-%02d" % i}),
    ]
    for label, path, method, make in oracles:
        check("%s: the deployment starts unlocked, so what follows measures the endpoint "
              "and not the previous phase" % label, reset_guard(A, other, BENCH_PASS))
        first = csrf_post(holder, A, path, make(0), method=method)
        check("%s: a wrong password is refused for the CREDENTIAL, not for the session — "
              "otherwise this whole probe is measuring an unauthenticated 401" % label,
              not is_lockout(first) and "not authenticated" not in first.text.lower()
              and first.status_code == 401,
              brief(first))
        seen = [status_of(first)]
        locked = False
        for i in range(1, MAX_ATTEMPTS + 2):
            rr = csrf_post(holder, A, path, make(i), method=method)
            seen.append(status_of(rr))
            if is_lockout(rr):
                locked = True
                break
        check("%s stops answering once the lockout engages — otherwise it is an unmetered "
              "password oracle for whoever holds the cookie" % label,
              locked, json.dumps(seen))
    # Checked IMMEDIATELY, with no cooldown: the base lockout is 60s and a minute-long pause
    # to clear a rate-limit bucket would outlive the very thing being looked for. So the
    # probe goes to step-up on the OTHER instance instead — a different bucket, untouched on
    # that host, and reachable with the same session because a cluster shares its JWT secret.
    r = csrf_post(holder, B, "/api/step-up", {"password": BENCH_PASS})
    check("and those endpoints feed the SAME deployment-wide lockout — guesses made against "
          "an authenticated route on ONE instance shut the door on the OTHER",
          is_lockout(r), brief(r))

    check("the deployment is left unlocked", reset_guard(A, other, BENCH_PASS))

    return report()


if __name__ == "__main__":
    try:
        sys.exit(main())
    finally:
        teardown_cluster()
