# Stand up mypintusan with a REAL OSDP reader on the bus.
#
# WHY THIS EXISTS. mypintusan decides who physically walks into a building, and it had never been
# run against anything. It has the thinnest unit coverage in the suite (7 test files) and zero live
# exercise — and its decision path is a pure function whose unit tests are genuinely good, which is
# exactly the shape that lulls: `Decide()` can be perfect while the SNAPSHOT it is handed is wrong,
# and no test of a pure function will ever notice.
#
# So this stands up the whole chain the way a site runs it: the app, a real OSDP bus over TCP, and
# `tools/osdp-sim` presenting cards on it. A badge read here travels the same path it does in a
# corridor — frame decode, reader state, credential lookup, grant join, schedule, the strike.
#
# WHAT IT TAKES, and none of it is guesswork:
#
#   - mypintusan needs NO redis (its shipped cache provider is the in-process one) and NO postgres
#     (sqlite is its shipped default). It is the easiest app in the suite to stand up.
#   - it HAS a `pairing` block, so fleet_harness.base_config works on it, unlike myidsan.
#   - the OSDP bus port must be `tcp://host:port`. Serial is build-order step 5 and dialBus
#     refuses anything else, so the simulator is the only way to have a reader at all today.
#   - the CONTAINER must reach the simulator, and the simulator runs on the host. `127.0.0.1`
#     inside a container is the container, so the bus is pointed at the host's LAN address — the
#     same lesson the myidsan harness records for the auth-code flow.
#   - buses are seeded from config ONLY on first boot; after that a `runtime_setting` row wins. So
#     the data dir is wiped on every stand-up, or the second run silently ignores the config it was
#     just given.
#
#   python tools/fleetbench/pintusan_harness.py
import io
import json
import os
import shutil
import subprocess
import sys
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import REPO, ROOT, base_config, sh, wait_up
from idsan_harness import host_ip, make_cert

urllib3.disable_warnings()

NET = "pintubench"
APP = "mypintusan"
NAME = "pintu"
TLS_PORT = 3005
HOST_PORT = 18481
HOST = host_ip()
BASE = "https://%s:%d" % (HOST, HOST_PORT)
WORK = os.path.join(ROOT, "pintu-certs")

# The simulator runs on the HOST (it is a go binary, not a container) so the bench can restart it
# with a different card without rebuilding an image.
SIM_PORT = 4870
SIM_ADDR = "tcp://%s:%d" % (HOST, SIM_PORT)

ADMIN_USER = "admin"
ADMIN_PASS = "admin123"
BENCH_PASS = "Bench!2345678"

# The reader address the simulator answers on, and the site key. Both are the simulator's defaults;
# restated here because the bus config has to agree with them exactly or the reader never comes up
# and every later check fails for a reason that has nothing to do with access control.
READER_ADDR = 1
SITE_KEY = "a0a1a2a3a4a5a6a7b0b1b2b3b4b5b6b7"


def app_config():
    cfg = base_config(APP, TLS_PORT)
    cfg["localAuth"] = {"enabled": True, "username": ADMIN_USER, "password": ADMIN_PASS}
    # Multicast discovery finds nothing in docker and logs about it forever.
    cfg["pairing"]["enabled"] = False
    cfg["access"] = dict(cfg.get("access") or {})
    cfg["access"].update({
        "timezone": "UTC",       # so a bench asserting "outside the schedule" is not fighting an offset
        "tickSeconds": 1,
        "pinWindowSeconds": 15,
        "offline": False,
    })
    # ONE bus, pointed at the simulator on the host.
    cfg["buses"] = [{
        "port": SIM_ADDR,
        "slotMillis": 200,
        "replyTimeoutMillis": 1000,
        "readers": [{
            "address": READER_ADDR,
            "scbk": SITE_KEY,
            "requireSecureChannel": False,
        }],
    }]
    return cfg


def write(path, obj):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    io.open(path, "w", encoding="utf-8", newline="\n").write(json.dumps(obj, indent=2))


def build():
    """Build mypintusan for the container.

    KOPIV2_SKIP_BUILD=1 reuses whatever is already at that path, so the SAME bench file can be
    pointed at a binary built from another commit — which is how the before/after numbers are
    measured against one identical set of checks rather than two bench versions."""
    out = os.path.join(ROOT, "bin", APP)
    os.makedirs(os.path.dirname(out), exist_ok=True)
    if os.environ.get("KOPIV2_SKIP_BUILD") == "1":
        print("KOPIV2_SKIP_BUILD=1 — reusing", out)
        return out
    env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
    subprocess.run(["go", "build", "-o", out, "./cmd/" + APP], cwd=REPO, env=env, check=True)
    return out


def start_app():
    data = os.path.join(ROOT, NAME)
    sh("docker", "run", "-d", "--name", NAME, "--network", NET,
       "-p", "%d:%d" % (HOST_PORT, TLS_PORT),
       "-v", os.path.join(ROOT, "bin") + ":/bin/app:ro",
       "-v", os.path.join(REPO, "apps", APP) + ":/home/app:ro",
       "-v", data + ":/data",
       "-e", "%s_HOME=/home/app" % APP.upper(),
       "-e", "%s_DATA=/data" % APP.upper(),
       "-w", "/data", "debian:bookworm-slim", "/bin/app/" + APP)


def start_sim(card="deadbeef", bits=26, every="3s", scenario="happy", pin="", extra=None):
    """Run tools/osdp-sim on the HOST, badging `card` every `every`.

    Returned as a Popen so a bench can stop it and start another with a different card — the
    simulator has no on-demand control channel, so 'present a different credential' means
    restarting it. Slower than a control socket and completely faithful, which is the better
    trade for a bench that runs a handful of scenarios."""
    args = [os.path.join(ROOT, "bin", "osdp-sim.exe" if os.name == "nt" else "osdp-sim"),
            "-addr", ":%d" % SIM_PORT, "-scenario", scenario,
            "-card", card, "-bits", str(bits), "-card-every", every,
            "-site-key", SITE_KEY]
    if pin:
        args += ["-pin", pin]
    if extra:
        args += list(extra)
    return subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)


def build_sim():
    """Always built from THIS tree, even under KOPIV2_SKIP_BUILD: the simulator is bench tooling,
    not the product, so a before/after comparison must hold it constant and vary only the app."""
    out = os.path.join(ROOT, "bin", "osdp-sim.exe" if os.name == "nt" else "osdp-sim")
    # The simulator runs on the HOST, so it is built for the host, not for linux/amd64.
    subprocess.run(["go", "build", "-o", out, "./tools/osdp-sim"], cwd=REPO, check=True)
    return out


def teardown():
    sh("docker", "rm", "-f", NAME, check=False)
    sh("docker", "network", "rm", NET, check=False)
    # Buses are seeded from config only on FIRST boot; a surviving data dir means the second run
    # quietly ignores the bus it was just handed and polls nothing.
    shutil.rmtree(os.path.join(ROOT, NAME), ignore_errors=True)


class Client:
    """A session against mypintusan.

    mypintusan is an APPLIANCE app, not a relying party: it uses the shared local Basic-auth
    stack (domain/shared/apis.NewLocalBasicAuth), the same one mymatasan and myiotsan use —
    NOT myseliasan's `/api/auth/local-login` cookie flow and NOT myidsan's `/api/login/default`.
    Every request carries the credential; the session probe is `GET /api/auth/session`. Getting
    this wrong costs a confusing run of 404s that read like a broken build."""

    def __init__(self, base=BASE):
        self.base = base
        self.auth = (ADMIN_USER, ADMIN_PASS)
        self.s = requests.Session()
        self.s.verify = False
        # REQUESTS_CA_BUNDLE in the environment OVERRIDES session.verify and the failure reads
        # like the app's fault.
        self.s.trust_env = False

    def rotate(self, new=BENCH_PASS):
        """The bootstrap admin is flagged must-change; everything but /auth/session and
        /auth/change-password 403s until the password is rotated."""
        r = self.s.post(self.base + "/api/auth/change-password", auth=self.auth,
                        json={"currentPassword": self.auth[1], "newPassword": new}, timeout=30)
        if r.status_code == 200:
            self.auth = (self.auth[0], new)
        return r

    def get(self, path):
        return self.s.get(self.base + path, auth=self.auth, timeout=60)

    def post(self, path, body=None):
        return self.s.post(self.base + path, auth=self.auth, json=body or {}, timeout=60)

    def put(self, path, body=None):
        return self.s.put(self.base + path, auth=self.auth, json=body or {}, timeout=60)

    def delete(self, path):
        return self.s.delete(self.base + path, auth=self.auth, timeout=60)


def admin():
    """Sign in and rotate the bootstrap password if the app demands it.

    BOTH passwords are tried, because an instance that a previous run already rotated no longer
    answers to the bootstrap one — and the refusal is a bare "access denied" that reads like a
    broken app rather than a stale password."""
    c = Client()
    r = c.get("/api/auth/session")
    if r.status_code != 200:
        c.auth = (ADMIN_USER, BENCH_PASS)
        r = c.get("/api/auth/session")
    if r.status_code != 200:
        raise SystemExit("could not reach %s: %s" % (BASE, (r.text or "")[:200]))
    try:
        must = (r.json().get("result") or {}).get("mustChangePassword")
    except ValueError:
        must = False
    if must:
        c.rotate()
    return c


def main():
    print("host address the container can reach:", HOST)
    teardown()
    os.makedirs(os.path.join(ROOT, "bin"), exist_ok=True)
    sh("docker", "network", "create", NET, check=False)

    build()
    build_sim()

    data = os.path.join(ROOT, NAME)
    certs = os.path.join(data, "certs")
    os.makedirs(certs, exist_ok=True)
    crt, key = make_cert(WORK)
    io.open(os.path.join(certs, "cert.pem"), "wb").write(io.open(crt, "rb").read())
    io.open(os.path.join(certs, "key.pem"), "wb").write(io.open(key, "rb").read())

    write(os.path.join(data, "config.json"), app_config())
    start_app()

    if not wait_up(BASE + "/api/auth/config", timeout=180):
        print(sh("docker", "logs", "--tail", "40", NAME, check=False))
        raise SystemExit("mypintusan did not come up")
    print("mypintusan is serving:", BASE)
    print("point the simulator at :%d — the app dials %s" % (SIM_PORT, SIM_ADDR))
    return 0


if __name__ == "__main__":
    sys.exit(main())
