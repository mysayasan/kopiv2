# Stand up myidsan AND a relying app, so the sign-in that every other app depends on can be
# driven end to end.
#
# WHY THIS EXISTS. myidsan is the SSO server: if it is wrong, every app is wrong. It has a
# third of the flagships' unit coverage and — until this — no live exercise at all. Its own
# tests prove its handlers; nothing proved that a real second app can actually get a user
# signed in through it, or that its refusals refuse.
#
# THE ONE THING THAT MAKES THIS AWKWARD, and the reason this file is not two lines of docker:
# an authorization-code flow has three parties and they must all agree on ONE address for each
# app. The browser (this bench, on the host) follows a redirect to myidsan; the relying app
# (in a container) then calls myidsan server-to-server to exchange the code. So the URL in the
# config has to resolve, to the same machine, from BOTH the host and a container.
#
#   127.0.0.1            works from the host, means "myself" inside a container. No.
#   host.docker.internal works inside a container, does not resolve on the host. No.
#   the host's LAN IP    works from both. Yes — and that is what this uses, discovered at
#                        runtime rather than hardcoded, with a certificate that names it.
#
#   python tools/fleetbench/idsan_harness.py
import io
import json
import os
import shutil
import socket
import sys
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import REPO, ROOT, result_list, sh, wait_up

urllib3.disable_warnings()

NET = "idsanbench"
IDSAN_PORT = 18451
RELIER_PORT = 18452
# The relying app is myseliasan, because it is the one with a real SSO leg already wired.
RELIER_APP = "myseliasan"

CLIENT_ID = "myseliasan"
CLIENT_SECRET = "bench-myseliasan-secret-6f1a2c"
# Long and non-placeholder on purpose: apphost DROPS a known placeholder and silently disables
# /api/sso/introspect, which would make an introspection check fail for a reason that has
# nothing to do with the thing under test.
INTERNAL_TOKEN = "bench-internal-4b8d1e7a9c3f5028d6b4a1e9c7f30528"

ADMIN_USER = "admin"
ADMIN_PASS = "admin123"
# The password every seeded admin is rotated to. THIRTEEN characters, not the ten the fleet
# harness uses: myidsan enforces a 12-character minimum and refuses "Bench!2345" outright,
# which is the password policy working and is worth not fighting.
BENCH_PASS = "Bench!2345678"
WORK = os.path.join(ROOT, "idsan-certs")


def host_ip():
    """The host's LAN address — the one name both the bench and a container can reach."""
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        s.connect(("8.8.8.8", 80))
        return s.getsockname()[0]
    finally:
        s.close()


HOST = host_ip()
IDSAN_URL = "https://%s:%d" % (HOST, IDSAN_PORT)
RELIER_URL = "https://%s:%d" % (HOST, RELIER_PORT)


def make_cert(work):
    """ONE certificate, named for every address these two apps are reached by.

    The apps self-sign on first boot for their own hostnames, which is right for a product and
    useless here: the relying app validates myidsan's certificate on the server-to-server code
    exchange, and it reaches it by an address the app never knew about."""
    os.makedirs(work, exist_ok=True)
    crt, key = os.path.join(work, "bench.crt"), os.path.join(work, "bench.key")
    sh("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
       "-keyout", key, "-out", crt, "-days", "3", "-subj", "/CN=kopiv2-bench",
       "-addext", "subjectAltName=IP:%s,IP:127.0.0.1,DNS:localhost,DNS:idsan,DNS:relier,"
                  "DNS:host.docker.internal" % HOST,
       "-addext", "basicConstraints=critical,CA:TRUE",
       "-addext", "keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign")
    return crt, key


def app_config(app, tls_port):
    """A container-shaped config: sqlite, our certificate, a known admin, no rate limit.

    Deliberately NOT fleet_harness.base_config — that one assumes a `pairing` block, which
    myidsan does not have, and force-disables SSO, which is the one thing being tested."""
    src = json.load(io.open(os.path.join(REPO, "apps", app, "config.json"), encoding="utf-8-sig"))
    src["db"] = {"engine": "sqlite", "host": "", "port": 0, "user": "", "password": "",
                 "db_name": "/data/%s.db" % app, "ssl_mode": ""}
    src["server"]["tlsPorts"] = [tls_port]
    src["server"]["nonTlsPorts"] = []
    src["tls"] = {"certPath": "/data/certs/cert.pem", "keyPath": "/data/certs/key.pem"}
    if "fileStorage" in src:
        src["fileStorage"]["path"] = "/data/uploads"
    src["logging"]["path"] = "/data/logs/%s.log" % app
    src["localAuth"] = {"enabled": True, "username": ADMIN_USER, "password": ADMIN_PASS}
    # A bench hammers the login endpoint on purpose — that IS the lockout test — so the
    # generic per-IP limiter would refuse requests the test needs to reach the handler.
    src["rateLimit"]["enabled"] = False
    return src


def write(path, obj):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    io.open(path, "w", encoding="utf-8", newline="\n").write(json.dumps(obj, indent=2))


def start(name, app, tls_port, host_port):
    data = os.path.join(ROOT, name)
    sh("docker", "run", "-d", "--name", name, "--network", NET,
       "-p", "%d:%d" % (host_port, tls_port),
       "-v", os.path.join(ROOT, "bin") + ":/bin/app:ro",
       "-v", os.path.join(REPO, "apps", app) + ":/home/app:ro",
       "-v", data + ":/data",
       "-e", "%s_HOME=/home/app" % app.upper(),
       "-e", "%s_DATA=/data" % app.upper(),
       "-w", "/data", "debian:bookworm-slim", "/bin/app/" + app)


def teardown():
    """Containers AND their data.

    EVERY RUN STARTS FROM THE SAME STATE, deliberately. Leaving the data dirs behind meant the
    second run tripped over its own registrations from the first, and a harness that behaves
    differently on the second run is one people stop trusting — and one whose failures they
    start explaining away."""
    for name in ("idsan", "relier", "idsan-redis"):
        sh("docker", "rm", "-f", name, check=False)
    sh("docker", "network", "rm", NET, check=False)
    for name in ("idsan", "relier"):
        shutil.rmtree(os.path.join(ROOT, name), ignore_errors=True)


class Client:
    """A browser-shaped session: one cookie jar, no environment proxies, no cert checking of
    our own self-signed pair."""

    def __init__(self, base):
        self.base = base
        self.s = requests.Session()
        self.s.verify = False
        # REQUESTS_CA_BUNDLE in the environment OVERRIDES session.verify, and the failure
        # reads like the app's fault.
        self.s.trust_env = False

    def get(self, path, **kw):
        return self.s.get(self.base + path if path.startswith("/") else path, timeout=30, **kw)

    def post(self, path, **kw):
        return self.s.post(self.base + path if path.startswith("/") else path, timeout=30, **kw)

    def csrf(self):
        for name in ("__Host-kopiv2_csrf", "kopiv2_csrf"):
            v = self.s.cookies.get(name)
            if v:
                return v
        return ""


# Where each app puts its local sign-in and its forced password change. Neither is a guess:
# myseliasan's local login is the fallback for when SSO is unavailable, and myidsan's IS the
# product's front door, so they live in different places and the change-password route follows
# the login route rather than the app.
LOCAL_LOGIN = {
    "myidsan": ("/api/login/default", "/api/login/default/change-password"),
    "myseliasan": ("/api/auth/local-login", "/api/auth/change-password"),
}


def admin(base, path="/api/auth/local-login", change="/api/auth/change-password"):
    """Sign in to an app's LOCAL admin account (not through SSO) — the way an operator
    configures the identity server before anybody can use it.

    THE TWO APPS DO NOT AGREE ON THE PATH, and that is not an accident to paper over: on
    myseliasan a local sign-in is the fallback for when SSO is unavailable
    (`/api/auth/local-login`); on myidsan it IS the product's own front door
    (`/api/login/default`). Named by the caller rather than guessed."""
    c = Client(base)
    for pw in (ADMIN_PASS, BENCH_PASS):
        r = c.s.post(base + path, json={"username": ADMIN_USER, "password": pw}, timeout=30)
        if r.status_code != 200:
            continue
        # The rotation is ATTEMPTED rather than predicted. myseliasan answers a local sign-in
        # with `mustChangePassword`; myidsan answers `{ok:true}` and only tells you by
        # refusing the next authenticated call with `password_change_required`. Attempting it
        # and ignoring a refusal works for both, and does not depend on either app's
        # response shape staying the way it is today.
        if pw != BENCH_PASS:
            c.s.post(base + change,
                     json={"currentPassword": pw, "newPassword": BENCH_PASS},
                     headers={"X-CSRF-Token": c.csrf()}, timeout=30)
            c.s.post(base + path, json={"username": ADMIN_USER, "password": BENCH_PASS}, timeout=30)
        return c
    raise SystemExit("could not sign in to " + base)


def main():
    print("host address both sides can reach:", HOST)
    teardown()
    os.makedirs(os.path.join(ROOT, "bin"), exist_ok=True)
    sh("docker", "network", "create", NET)

    # REDIS IS NOT OPTIONAL FOR myidsan, and running a real one is the faithful choice.
    # Its shipped config names redis as the cache provider, and the cache is the SESSION
    # AUTHORITY here — the table is only an index. Swapping in the in-process cache would
    # bench a deployment nobody ships and would hide anything redis-specific about sessions,
    # which is the half of this app most worth exercising.
    sh("docker", "run", "-d", "--name", "idsan-redis", "--network", NET, "redis")

    crt, key = make_cert(WORK)
    for name, app, tls_port, host_port in (("idsan", "myidsan", 3001, IDSAN_PORT),
                                           ("relier", RELIER_APP, 3002, RELIER_PORT)):
        data = os.path.join(ROOT, name)
        certs = os.path.join(data, "certs")
        os.makedirs(certs, exist_ok=True)
        io.open(os.path.join(certs, "cert.pem"), "wb").write(io.open(crt, "rb").read())
        io.open(os.path.join(certs, "key.pem"), "wb").write(io.open(key, "rb").read())

        cfg = app_config(app, tls_port)
        if name == "idsan":
            cfg["sso"]["internalToken"] = INTERNAL_TOKEN
            cfg["cache"] = dict(cfg.get("cache") or {})
            cfg["cache"]["provider"] = "redis"
            cfg["cache"]["redis"] = dict(cfg["cache"].get("redis") or {})
            cfg["cache"]["redis"].update({"address": "idsan-redis:6379", "password": "", "db": 0})
        else:
            # The relying half. providerBaseUrl and redirectBaseUrl both use the shared host
            # address so the redirect the BROWSER follows and the exchange the SERVER makes
            # reach the same place.
            cfg["sso"].update({
                "enabled": True,
                "issuer": "myidsan",
                "audience": CLIENT_ID,
                "providerBaseUrl": IDSAN_URL,
                "caCertPath": "/data/certs/cert.pem",
                "clientId": CLIENT_ID,
                "clientSecret": CLIENT_SECRET,
                "redirectBaseUrl": RELIER_URL,
                "redirectPath": "/api/auth/callback",
            })
        write(os.path.join(data, "config.json"), cfg)
        start(name, app, tls_port, host_port)

    for label, url in (("idsan", IDSAN_URL), ("relier", RELIER_URL)):
        if not wait_up(url + "/api/auth/session"):
            print(sh("docker", "logs", "--tail", "40", label, check=False))
            raise SystemExit(label + " did not come up")
    print("both apps are serving:", IDSAN_URL, RELIER_URL)

    # ---- register the relying app in the identity server -----------------------------------
    #
    # THREE ROWS, not one, and that shape is the product's rather than this bench's: an app
    # REGISTRY entry says who the app is and what audience its tokens carry; an app-auth-config
    # holds the client credentials; and a redirect URI says where a code may be sent back to.
    # The last one has to match what the relying app asks for EXACTLY — no trailing slash, no
    # host that merely resolves to the same machine.
    idsan = admin(IDSAN_URL, *LOCAL_LOGIN["myidsan"])
    redirect_uri = RELIER_URL + "/api/auth/callback"

    def post(path, body):
        return idsan.s.post(IDSAN_URL + path, json=body,
                            headers={"X-CSRF-Token": idsan.csrf()}, timeout=30)

    def result_id(resp):
        try:
            out = resp.json().get("result")
        except ValueError:
            return None
        if isinstance(out, dict):
            return out.get("id")
        return out if isinstance(out, int) else None

    r = post("/api/app-registry", {
        "code": CLIENT_ID, "title": "MySeliaSan (bench)", "description": "SSO bench relying app",
        "baseUrl": RELIER_URL, "audience": CLIENT_ID, "isActive": True,
    })
    print("register app:", r.status_code, r.text[:200])
    app_id = result_id(r)
    if not app_id:
        # Already registered, from an earlier run of this harness against the same data dir —
        # the containers are recreated but /data is not. The create answers 500 on the unique
        # key, which is ugly but not a failure, so the existing row is found instead.
        #
        # `result_list` rather than a hand-rolled unwrap: this app answers
        # {data:{result:[…]}}, and reading the wrong key here would silently look exactly like
        # "not registered" — which is the envelope trap that has now cost four benches a check.
        for row in result_list(idsan.get("/api/app-registry?limit=100"), "items"):
            if isinstance(row, dict) and row.get("code") == CLIENT_ID:
                app_id = row.get("id")
                print("  (already registered from a previous run: id %s)" % app_id)
    if not app_id:
        raise SystemExit("could not register the relying app: " + r.text[:300])

    r = post("/api/app-auth-config", {
        "appRegistryId": app_id, "clientId": CLIENT_ID, "clientSecret": CLIENT_SECRET,
        "isActive": True,
    })
    print("register client credentials:", r.status_code, r.text[:200])
    cfg_id = result_id(r)

    if cfg_id:
        r = post("/api/app-redirect-uri", {
            "appAuthConfigId": cfg_id, "redirectUri": redirect_uri, "isActive": True,
        })
        print("register redirect uri:", r.status_code, r.text[:200])

    print("redirect_uri that must match EXACTLY:", redirect_uri)
    return 0


if __name__ == "__main__":
    sys.exit(main())
