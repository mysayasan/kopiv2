# myseliasan bench: the failed-login lockout that is not there.
#
# WHY THIS ONE. myidsan's lockout was benched under a real cluster (#206) and two defects came
# out of it. That bench also turned up something it could not fix: myseliasan — the fleet
# control plane, the OTHER Tier A clusterable app, the one holding every camera in the estate —
# has NO failed-login lockout at all. `/api/auth/local-login` calls AuthenticateLocal and
# returns. No guard, no per-attempt delay, no escalation, and nothing written to the audit
# trail either way.
#
# The only thing between a password list and the fleet console is the GENERIC rate limiter:
# 30 requests a minute, per source address, per path, per instance, refilling forever. That is
# a budget, not a lockout. It never escalates, it never notices one account being attacked from
# many addresses, and a two-instance cluster simply doubles it.
#
# WHAT THIS BENCH DOES DIFFERENTLY FROM THE MYIDSAN ONE: it starts by proving the control is
# ABSENT, on the shipped configuration, and reports the attacker's real budget as a number. A
# bench for a feature that does not exist has to be able to fail honestly before it can pass.
#
# It stands up a CLUSTER — two myseliasan instances on ONE Postgres and ONE Redis — because
# that is a supported, wizard-declarable deployment ("Tier A, genuinely clusterable") and
# because a lockout is exactly the state that quietly stops working when there are two of you.
#
# THE CLAIMS UNDER TEST:
#
#   1. a wrong password is refused and a right one is accepted, on both instances (baseline);
#   2. repeated wrong passwords TRIP A LOCKOUT rather than being served forever at the rate
#      limiter's pace — and the refusal is the lockout's, not the rate limiter's;
#   3. it is a lockout, not a filter: while locked, the CORRECT password is refused too;
#   4. it is DEPLOYMENT-WIDE — locking on instance A locks on instance B with no free guesses,
#      and the escalating backoff escalates across instances instead of restarting at the base;
#   5. a successful sign-in clears the counters, so a user who mistypes and then gets it right
#      is not left one attempt away from being shut out;
#   6. it is keyed on the ACCOUNT as well as the source, so a spray distributed across
#      addresses against one known username is throttled — and the cost of that (a known
#      username can be held locked by a stranger) is measured rather than assumed;
#   7. change-password — the OTHER endpoint that checks this account's password — is behind the
#      same guard, so a stolen session cookie is not an unmetered password oracle;
#   8. the audit trail records the sign-in, the refusal AND the lockout. myseliasan's trail
#      records node adoptions, policy edits and key rotations and has never recorded a single
#      authentication event, so today a complete brute-force run leaves no trace in the
#      product's own record of who did what — and it must not record the attempted password;
#   9. the clustering preflight tells the operator whether the lockout is actually shared.
#      LoginGuard.SharesState() was written for exactly that in #206 and nothing ever read it.
#
#   python tools/fleetbench/bench_myseliasan_lockout.py   # stands up its own cluster, tears it down
import io
import json
import os
import re
import shutil
import subprocess
import sys
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import REPO, ROOT, base_config, result_list, result_of, sh, wait_up
from idsan_harness import host_ip, make_cert

urllib3.disable_warnings()
CHECKS = []

NET = "selbench"
PG = "sel-pg"
REDIS = "sel-redis"
NODES = [("sel-a", 3002, 18471), ("sel-b", 3002, 18472)]
HOST = host_ip()
A = "https://%s:%d" % (HOST, 18471)
B = "https://%s:%d" % (HOST, 18472)
# The in-container address of instance A, for driving it from a SECOND source address.
A_INTERNAL = "https://sel-a:3002"
WORK = os.path.join(ROOT, "sel-certs")
# A PRIVATE binary name. Other benches leave containers running off bin/myseliasan through a
# bind mount, and rewriting that file underneath a running container kills it with exit 139
# and no log line.
BIN = "myseliasan-sel"

ADMIN = "admin"
STOCK_PASS = "admin123"
BENCH_PASS = "Bench!2345678"
JWT_SECRET = "bench-selcluster-jwt-secret-7c2a9f4e1b6038d5"

# Shipped loginSecurity defaults, restated so a check that depends on them fails loudly if
# they move. myseliasan ships no loginSecurity block at all, and an ABSENT block resolves to
# ON with these numbers (infra/config.LoginSecurityConfigModel.Effective) — which is the point:
# the configuration already says this app locks out, and the code never did.
MAX_ATTEMPTS = 8
BASE_LOCKOUT = 60
FAILED_DELAY_MS = 400

# The generic rate limiter's public tier, from apps/myseliasan/config.json. The bench has to
# stay under it to reach the handler at all, which is itself part of the finding: without a
# lockout this number IS the entire brute-force control.
PUBLIC_RPM = 30


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

    Both answer 429 and both set Retry-After, so "got a 429" proves nothing on its own — the
    myidsan run of this same shape passed two throttle checks purely on rate-limit refusals
    while the lockout it was measuring did not exist. The lockout body carries
    retryAfterSeconds; the rate limiter says "rate limit exceeded"."""
    if r.status_code != 429:
        return False
    return "retryAfterSeconds" in r.text and "rate limit exceeded" not in r.text


def is_ratelimit(r):
    return r.status_code == 429 and not is_lockout(r)


def status_of(r):
    """429 only when it is the lockout; a rate-limit refusal is reported as 'rl' so it can
    never be mistaken for one in a status list."""
    if r.status_code == 429:
        return 429 if is_lockout(r) else "rl"
    return r.status_code


def retry_after(r):
    try:
        return int(float(r.headers.get("Retry-After") or 0))
    except (TypeError, ValueError):
        return 0


def brief(r):
    return "%d %s" % (r.status_code, r.text[:160].replace("\n", " "))


# ---- the cluster -------------------------------------------------------------------------

def build():
    """Build myseliasan for the containers.

    KOPIV2_SKIP_BUILD=1 reuses whatever is already at that path. It exists so the SAME bench
    file can be pointed at a binary built from another commit — which is how the before/after
    numbers in the README were measured against one identical set of checks, rather than
    against the slightly different set the bench happened to have on the day."""
    out = os.path.join(ROOT, "bin", BIN)
    os.makedirs(os.path.dirname(out), exist_ok=True)
    if os.environ.get("KOPIV2_SKIP_BUILD") == "1":
        print("KOPIV2_SKIP_BUILD=1 — reusing", out)
        return out
    env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
    subprocess.run(["go", "build", "-o", out, "./cmd/myseliasan"], cwd=REPO, env=env, check=True)
    return out


def app_config(tls_port):
    """The SHIPPED posture on a real cluster, not the harness's convenience one.

    base_config disables the rate limiter (most benches hammer endpoints and do not want it).
    Here the rate limiter is part of what is measured — it is the only throttle this app has
    today — so it goes back on."""
    cfg = base_config("myseliasan", tls_port)
    cfg["db"] = {"engine": "postgres", "host": PG, "port": 5432, "user": "postgres",
                 "password": "benchpg", "db_name": "selbench", "ssl_mode": "disable"}
    cfg["cache"] = dict(cfg.get("cache") or {})
    cfg["cache"]["provider"] = "redis"
    cfg["cache"]["redis"] = dict(cfg["cache"].get("redis") or {})
    cfg["cache"]["redis"].update({"address": REDIS + ":6379", "password": "", "db": 0})
    # A real cluster coordinates its singletons on redis too; leaving this on "memory" makes
    # the preflight checklist fail a row for a reason unrelated to the lockout.
    cfg["transaction"]["lockProvider"] = "redis"
    # ONE jwt secret across the cluster. Left to per-instance generation, a session issued by
    # one instance is rejected by the other as a bad signature — which is why the clustering
    # preflight makes this a checklist row, and which the myidsan bench walked into first.
    cfg["jwt"] = {"secret": JWT_SECRET}
    cfg["rateLimit"]["enabled"] = True
    # No myidsan in this bench: local accounts only, which is also the shipped package's
    # default posture (providerBaseUrl empty).
    cfg["sso"] = dict(cfg.get("sso") or {})
    cfg["sso"]["providerBaseUrl"] = ""
    # Multicast discovery in docker finds nothing and logs about it forever.
    cfg["pairing"]["enabled"] = False
    return cfg


def write(path, obj):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    io.open(path, "w", encoding="utf-8", newline="\n").write(json.dumps(obj, indent=2))


def start(name, tls_port, host_port):
    data = os.path.join(ROOT, name)
    sh("docker", "run", "-d", "--name", name, "--network", NET,
       "-p", "%d:%d" % (host_port, tls_port),
       "-v", os.path.join(ROOT, "bin") + ":/bin/app:ro",
       "-v", os.path.join(REPO, "apps", "myseliasan") + ":/home/app:ro",
       "-v", data + ":/data",
       "-e", "MYSELIASAN_HOME=/home/app",
       "-e", "MYSELIASAN_DATA=/data",
       "-w", "/data", "debian:bookworm-slim", "/bin/app/" + BIN)


def start_cluster():
    """Two myseliasan instances, ONE Postgres, ONE Redis.

    The shared database is what makes these one deployment rather than two servers: the
    account being attacked has to be the SAME account, or the bench proves nothing about
    clustering. The shared cache is what a clustered install already needs for sessions, so
    requiring it invents no new dependency."""
    teardown_cluster()
    sh("docker", "network", "create", NET, check=False)
    sh("docker", "run", "-d", "--name", PG, "--network", NET,
       "-e", "POSTGRES_PASSWORD=benchpg", "-e", "POSTGRES_USER=postgres",
       "-e", "POSTGRES_DB=postgres", "postgres:16-alpine")
    sh("docker", "run", "-d", "--name", REDIS, "--network", NET, "redis")
    for _ in range(60):
        out = sh("docker", "exec", PG, "pg_isready", "-U", "postgres", check=False)
        if out and "accepting connections" in str(out):
            break
        time.sleep(2)

    crt, key = make_cert(WORK)
    for name, tls, hostport in NODES:
        data = os.path.join(ROOT, name)
        certs = os.path.join(data, "certs")
        os.makedirs(certs, exist_ok=True)
        io.open(os.path.join(certs, "cert.pem"), "wb").write(io.open(crt, "rb").read())
        io.open(os.path.join(certs, "key.pem"), "wb").write(io.open(key, "rb").read())
        write(os.path.join(data, "config.json"), app_config(tls))
        start(name, tls, hostport)
        # Sequential, not parallel: both instances would otherwise race to create the database
        # and run the same migrations, and the loser logs a conflict that reads like a bug.
        if not wait_up("https://%s:%d/api/auth/config" % (HOST, hostport), timeout=240):
            print(sh("docker", "logs", "--tail", "40", name, check=False))
            return False
    return True


def teardown_cluster():
    for name, _, _ in NODES:
        sh("docker", "rm", "-f", name, check=False)
        shutil.rmtree(os.path.join(ROOT, name), ignore_errors=True)
    sh("docker", "rm", "-f", PG, check=False)
    sh("docker", "rm", "-f", REDIS, check=False)


# ---- drivers -----------------------------------------------------------------------------

class Session:
    """A browser-shaped session against one instance: cookie jar, CSRF echo, no env proxies."""

    def __init__(self, base):
        self.base = base
        self.s = requests.Session()
        self.s.verify = False
        # REQUESTS_CA_BUNDLE in the environment OVERRIDES session.verify and the failure reads
        # like the app's fault.
        self.s.trust_env = False

    def csrf(self):
        for name in ("__Host-kopiv2_csrf", "kopiv2_csrf"):
            v = self.s.cookies.get(name)
            if v:
                return v
        return ""

    def login(self, password, username=ADMIN):
        return self.s.post(self.base + "/api/auth/local-login",
                           json={"username": username, "password": password}, timeout=30)

    def change_password(self, current, new):
        return self.s.post(self.base + "/api/auth/change-password",
                           json={"currentPassword": current, "newPassword": new},
                           headers={"X-CSRF-Token": self.csrf()}, timeout=30)

    def get(self, path):
        return self.s.get(self.base + path, timeout=30)


def attempt(base, password, username=ADMIN):
    """One sign-in attempt from the HOST's address, on a throwaway session."""
    return Session(base).login(password, username)


def attempt_from_container(password, username=ADMIN):
    """One sign-in attempt against instance A from a DIFFERENT source address.

    Every request the bench makes from the host arrives with one address, so the per-source and
    per-account counters move together and neither can be told apart from the other. Driving A
    from inside another container on the same docker network gives a genuinely different source
    IP, which is the only way to show the ACCOUNT counter exists — that is the half that stops
    a spray distributed across addresses, and the half that carries the denial-of-service cost."""
    body = json.dumps({"username": username, "password": password})
    out = subprocess.run(
        ["docker", "exec", PG, "sh", "-c",
         'wget -q -S -O- --no-check-certificate --header="Content-Type: application/json" '
         "--post-data='%s' %s/api/auth/local-login 2>&1 || true" % (body, A_INTERNAL)],
        capture_output=True, text=True, timeout=60).stdout
    codes = re.findall(r"HTTP/1\.\d (\d{3})", out)
    code = int(codes[-1]) if codes else (200 if '"message"' in out else 0)
    # Same two-throttles problem as on the host side, and busybox wget hands us the body.
    if code == 429:
        return "rl" if "rate limit exceeded" in out else 429
    return code


def burn(base, n, tag="x", username=ADMIN):
    """n wrong passwords, stopping early if the lockout engages. Returns (statuses, response)."""
    out = []
    last = None
    for i in range(n):
        last = attempt(base, "%s-wrong-%02d" % (tag, i), username)
        out.append(status_of(last))
        if is_lockout(last):
            break
    return out, last


def wait_until_unlocked(base, limit=400):
    """Wait out a lockout, probing with a WRONG password.

    Probing with the CORRECT one would clear the counters as a side effect — a successful
    sign-in resets the escalation, which is intended and exactly what must NOT happen in the
    middle of measuring escalation. A locked request never reaches the credential check, so a
    wrong password costs nothing while locked and merely reports whether the door is open."""
    deadline = time.time() + limit
    while time.time() < deadline:
        r = attempt(base, "probe-still-locked")
        if not is_lockout(r):
            return True
        time.sleep(min(float(retry_after(r) or 5), 15))
    return False


def reset_guard(base, password=BENCH_PASS, limit=900):
    """Return the deployment to a clean slate for this account AND this source address.

    The source key is shared by every attempt the bench makes, so without an explicit reset each
    phase inherits the previous phase's failures and eventually measures the leftovers instead
    of the thing under test. A successful sign-in clears both keys; if the door is still shut,
    wait for it first."""
    deadline = time.time() + limit
    while time.time() < deadline:
        r = attempt(base, password)
        if r.status_code == 200:
            return True
        if is_ratelimit(r):
            time.sleep(min(retry_after(r) + 1, 30) or 10)
            continue
        if r.status_code != 429:
            print("    reset_guard: unexpected %s" % brief(r))
            return False
        time.sleep(min(retry_after(r) + 1, 30))
    return False


def cooldown_rate_limit(seconds=64):
    """Let the GENERIC rate limiter's window roll over.

    Two throttles answer 429 here and they mask each other. /api/auth/local-login has its own
    public bucket of 30 requests a minute, and this bench spends most of it failing to sign in
    — so a phase that starts with an empty bucket measures the rate limiter instead of the
    lockout. Both throttles are real and both are wanted; they cannot be measured in the same
    minute."""
    print("  (rate-limit cooldown %ds)" % seconds)
    time.sleep(seconds)


def signed_in(base, password=BENCH_PASS):
    s = Session(base)
    r = s.login(password)
    return s if r.status_code == 200 else None


def audit_rows(op, action=None, limit=200):
    """The product's OWN record of what happened, not the bench's idea of it.

    Correlating against the audit trail rather than against "something was refused" is the
    lesson W3-9 paid for: a check that waits for "an event" will happily accept a different
    event that arrived first. `result_list` rather than a hand-rolled unwrap — reading the
    wrong envelope key here looks exactly like an empty trail, which has now cost four benches
    a check and made a WORKING audit trail on this very suite read as zero entries."""
    path = "/api/audit?limit=%d" % limit
    if action:
        path += "&action=" + action
    return result_list(op.get(path), "items", "logs")


def main():
    print("host address both sides can reach:", HOST)
    build()
    check("a two-instance cluster stands up on one shared database and one shared cache",
          start_cluster())

    # ---- 1. baseline -------------------------------------------------------------------
    #
    # The stock admin is seeded once, into the SHARED database, so both instances serve the
    # same account — which is what makes everything below an attack on ONE deployment.
    boot = Session(A)
    r = boot.login(STOCK_PASS)
    check("the stock admin signs in on instance A", r.status_code == 200, brief(r))
    r = boot.change_password(STOCK_PASS, BENCH_PASS)
    check("the forced password change is accepted", r.status_code == 200, brief(r))

    r = attempt(B, BENCH_PASS)
    check("the SAME account signs in on instance B (one database, one jwt secret)",
          r.status_code == 200, brief(r))

    r = attempt(A, "definitely-not-the-password")
    check("a wrong password is refused", r.status_code == 403, brief(r))

    # That one wrong password is a real failure and the guard counted it. Clearing it here
    # makes the attempt COUNT below exact — the first fixed run reported "7 guesses served"
    # against a threshold of 8 because the baseline's failure was already on the counter and
    # invisible to the phase that thought it was starting from zero.
    check("the guard starts phase 2 from a clean slate", reset_guard(A))
    cooldown_rate_limit()

    # ---- 2. does a lockout exist at all? ------------------------------------------------
    #
    # THE HEADLINE. Without a guard this loop simply runs out of rate-limit budget: the
    # statuses read 403 x30 then "rl", forever, minute after minute. With a guard the eighth
    # wrong password shuts the door.
    print("\n-- phase 2: guessing until something stops us")
    t0 = time.time()
    first = attempt(A, "wrong-timing-probe")
    per_attempt = time.time() - t0
    statuses, last = burn(A, MAX_ATTEMPTS + 4, tag="p2")
    served = sum(1 for s in statuses if s == 403) + (1 if first.status_code == 403 else 0)
    check("repeated wrong passwords trip a LOCKOUT, not just the rate limiter",
          is_lockout(last),
          "attempt statuses: %s   (guesses served before anything stopped us: %d)"
          % (statuses, served))
    check("the lockout engages within the configured %d attempts" % MAX_ATTEMPTS,
          is_lockout(last) and served <= MAX_ATTEMPTS,
          "served %d, max %d" % (served, MAX_ATTEMPTS))
    check("a failed attempt is slowed by the configured per-failure delay",
          per_attempt >= (FAILED_DELAY_MS / 1000.0) * 0.75,
          "one failed sign-in took %.0fms, configured delay %dms" % (per_attempt * 1000, FAILED_DELAY_MS))

    # A lockout that still lets the right password through is a filter, not a lockout.
    r = attempt(A, BENCH_PASS)
    check("while locked, even the CORRECT password is refused", is_lockout(r), brief(r))

    # ---- 3. is it deployment-wide? ------------------------------------------------------
    print("\n-- phase 3: the second instance")
    r = attempt(B, "p3-wrong-00")
    check("instance B refuses the locked account too, with NO free guesses",
          is_lockout(r), brief(r))
    r = attempt(B, BENCH_PASS)
    check("instance B refuses the CORRECT password while the deployment is locked",
          is_lockout(r), brief(r))
    first_lock = retry_after(last)
    check("the first lockout is the configured base duration",
          BASE_LOCKOUT * 0.5 <= first_lock <= BASE_LOCKOUT * 1.5,
          "retryAfter %ds, base %ds" % (first_lock, BASE_LOCKOUT))

    # ---- 4. does the backoff escalate ACROSS instances? ---------------------------------
    print("\n-- phase 4: escalation across instances (waiting out lockout 1)")
    check("the first lockout expires on its own", wait_until_unlocked(A, limit=first_lock + 180))
    cooldown_rate_limit()
    statuses, last = burn(B, MAX_ATTEMPTS + 4, tag="p4")
    second_lock = retry_after(last)
    check("locking again — from the OTHER instance — escalates rather than restarting at the base",
          is_lockout(last) and second_lock >= BASE_LOCKOUT * 1.5,
          "first %ds on A, second %ds on B (expected roughly %ds)   %s"
          % (first_lock, second_lock, BASE_LOCKOUT * 2, statuses))

    # ---- 5. does a correct sign-in clear it? --------------------------------------------
    print("\n-- phase 5: a user who mistypes and then remembers (waiting out lockout 2)")
    check("the escalated lockout expires on its own", wait_until_unlocked(A, limit=second_lock + 240))
    check("a correct sign-in is accepted once the lockout expires", reset_guard(A))
    cooldown_rate_limit()

    statuses, _ = burn(A, MAX_ATTEMPTS - 1, tag="p5a")
    r = attempt(A, BENCH_PASS)
    ok_mid = r.status_code == 200
    statuses2, last2 = burn(A, MAX_ATTEMPTS - 1, tag="p5b")
    check("a successful sign-in clears the counters, so near-misses do not accumulate forever",
          ok_mid and not is_lockout(last2),
          "%d misses, sign-in %s, then %d more misses: %s" % (MAX_ATTEMPTS - 1, brief(r),
                                                              MAX_ATTEMPTS - 1, statuses2))

    check("the deployment is back to a clean slate", reset_guard(A))
    cooldown_rate_limit()

    # ---- 6. the account axis ------------------------------------------------------------
    #
    # Keyed on the source ALONE, a spray distributed across addresses against one known
    # username is completely unthrottled — which is the shape credential stuffing actually
    # takes. This drives the last attempt from a DIFFERENT source so the two keys can be told
    # apart.
    print("\n-- phase 6: a spray distributed across source addresses")
    statuses, last = burn(A, MAX_ATTEMPTS - 1, tag="p6")
    # TWO attempts from the other address, and the off-by-one is the point rather than a
    # rounding error. The guard is consulted BEFORE the credential, so the attempt that
    # crosses the threshold has already been evaluated and still gets its credential verdict;
    # the lockout applies from the next request on. The first container attempt is therefore
    # the eighth failure on the ACCOUNT key and answers 403, and the second is refused.
    #
    # If the account key did not exist, this source would be starting its own count from zero
    # and BOTH would answer 403 — which is exactly what the unfixed deployment did, and what
    # a spray distributed across a botnet costs when the only key is the source address.
    spray = [attempt_from_container("p6-wrong-from-elsewhere-%d" % i) for i in range(2)]
    check("a spray from a SECOND source address against the same username is throttled",
          spray[-1] == 429,
          "host made %d failures (%s), then two attempts from another address answered %s"
          % (MAX_ATTEMPTS - 1, statuses, spray))
    # The cost of that key, measured rather than assumed: a source that never attacked is now
    # refused this account. It is a nuisance recovered from by waiting, where unlimited
    # guessing against a known account is a compromise that is not recovered from.
    correct_from_other = attempt_from_container(BENCH_PASS)
    check("the cost is recorded: the account is locked even from an address that never attacked",
          correct_from_other == 429,
          "correct password from a clean source answered %s (this is the deliberate tradeoff)"
          % correct_from_other)

    check("the deployment recovers from the account-axis lockout", reset_guard(A))
    cooldown_rate_limit()

    # ---- 7. the other endpoint that checks this password --------------------------------
    #
    # The front door being guarded is worth little if change-password is an unmetered password
    # oracle for whoever holds a stolen cookie. This is the exact shape that was found on
    # myidsan's step-up (#204) and on its change-password and security-key removal (#206).
    print("\n-- phase 7: change-password as a password oracle")
    op = signed_in(A)
    check("an operator session is established for the oracle probe", op is not None)
    oracle = []
    last_cp = None
    if op:
        for i in range(MAX_ATTEMPTS + 3):
            last_cp = op.change_password("oracle-guess-%02d" % i, "Irrelevant!12345")
            oracle.append(status_of(last_cp))
            if is_lockout(last_cp):
                break
    check("change-password is behind the SAME lockout, not an unmetered password oracle",
          last_cp is not None and is_lockout(last_cp),
          "current-password guesses served: %s" % oracle)
    if op:
        # The CORRECT current password, deliberately paired with a new password the policy
        # must refuse. A locked deployment answers 429 before it looks at either; an unlocked
        # one answers 400 on the policy — so the two are still told apart, and the probe can
        # never actually rotate the operator's password out from under the rest of the bench.
        r = op.change_password(BENCH_PASS, "short")
        check("while locked, change-password refuses a request carrying the CORRECT current password",
              is_lockout(r), brief(r))

    check("the deployment recovers after the oracle probe", reset_guard(A))
    cooldown_rate_limit()

    # ---- 8. does any of this reach the audit trail? -------------------------------------
    #
    # myseliasan's trail records node adoptions, policy edits, key rotations and failover
    # takeovers, and has never recorded a single authentication event. A complete brute-force
    # run against the fleet console currently leaves NOTHING in the product's own record of
    # who did what — which is the half an investigation actually reads.
    print("\n-- phase 8: the audit trail")
    op = signed_in(A)
    check("an operator session is established for the audit read", op is not None)
    if op:
        rows = audit_rows(op)
        actions = set((row or {}).get("action") for row in rows)
        check("a successful sign-in is recorded in the audit trail",
              "login.success" in actions, "actions seen: %s" % sorted(a for a in actions if a))
        check("a REFUSED sign-in is recorded in the audit trail", "login.failure" in actions,
              "actions seen: %s" % sorted(a for a in actions if a))
        check("the lockout engaging is recorded in the audit trail", "login.lockout" in actions,
              "actions seen: %s" % sorted(a for a in actions if a))

        failures = [row for row in rows if (row or {}).get("action") == "login.failure"]
        check("a refused sign-in is recorded as DENIED and names the account attempted",
              bool(failures) and all(row.get("outcome") == "denied" for row in failures)
              and any(ADMIN in json.dumps(row) for row in failures),
              "%d failure rows" % len(failures))

        # A trail that stores what people typed into a password box IS a credential store.
        # Conditioned on the trail being non-empty ON PURPOSE: against the unfixed app this
        # check passed for the wrong reason — nothing had been recorded, so nothing had
        # leaked. "No evidence of X" and "evidence of no X" are not the same check.
        blob = json.dumps(rows)
        leaked = [pw for pw in (BENCH_PASS, STOCK_PASS, "oracle-guess-00", "p2-wrong-00")
                  if pw in blob]
        check("the trail records these events without recording the password attempted",
              bool(rows) and not leaked, "%d rows, leaked: %s" % (len(rows), leaked))

    # ---- 9. does the operator get told the truth about their cluster? -------------------
    #
    # LoginGuard.SharesState() was added in #206 with a comment saying the preflight checklist
    # reads it to tell an operator the truth. Nothing ever read it, so an operator declaring a
    # cluster is told about the cache, the lock provider, the at-rest key, the jwt secret and
    # the db pool — and nothing at all about whether the brute-force control is shared.
    print("\n-- phase 9: the clustering preflight")
    if op:
        pre = result_of(op.get("/api/deployment/preflight"))
        ids = [c.get("id") for c in (pre.get("checks") or []) if isinstance(c, dict)]
        row = next((c for c in (pre.get("checks") or [])
                    if isinstance(c, dict) and c.get("id") == "loginLockout"), None)
        check("the clustering preflight has a row for the failed-login lockout",
              row is not None, "preflight rows: %s" % ids)
        check("and it reports the lockout as shared on this redis-backed cluster",
              bool(row) and row.get("ok") is True, json.dumps(row) if row else "no row")

    # ---- 10. no regression --------------------------------------------------------------
    print("\n-- phase 10: the operator can still get in")
    check("the deployment is usable again after the whole attack", reset_guard(A))
    r = attempt(B, BENCH_PASS)
    check("and on the other instance too", r.status_code == 200, brief(r))

    return report()


def standup_only():
    """Stand the cluster up, rotate the admin password, and leave it running.

    For the SCREEN check (`uicheck_sel_lockout.js`), which needs a live instance and has no
    business waiting out the API bench's fourteen minutes of deliberate lockouts to get one.

        python tools/fleetbench/bench_myseliasan_lockout.py --standup
        node   tools/fleetbench/uicheck_sel_lockout.js .artifacts/fleetbench en
        node   tools/fleetbench/uicheck_sel_lockout.js .artifacts/fleetbench ms
        python tools/fleetbench/bench_myseliasan_lockout.py --teardown
    """
    build()
    if not start_cluster():
        return 1
    boot = Session(A)
    if boot.login(STOCK_PASS).status_code == 200:
        boot.change_password(STOCK_PASS, BENCH_PASS)
    print("up:", A, B, " admin /", BENCH_PASS)
    return 0


if __name__ == "__main__":
    if "--teardown" in sys.argv:
        teardown_cluster()
        sys.exit(0)
    if "--standup" in sys.argv:
        sys.exit(standup_only())
    code = 1
    try:
        code = main()
    finally:
        if os.environ.get("KOPIV2_KEEP") != "1":
            teardown_cluster()
    sys.exit(code)
