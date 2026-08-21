# W2-2 bench harness: a real control plane + two really adopted mymatasan nodes.
# Follows docs/FLAGSHIP_BENCH_CHECKLIST.md; only the liveness half is needed here
# (no cameras, no recording), so this is the cheap version of that harness.
import io, json, os, subprocess, sys, time
import urllib3, requests

urllib3.disable_warnings()
REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
# Container data dirs, binaries and bench output live under .artifacts (gitignored);
# only the harness itself is tracked. KOPIV2_BENCH_DIR overrides.
ROOT = os.environ.get("KOPIV2_BENCH_DIR") or os.path.join(REPO, ".artifacts", "fleetbench")

CP_PORT = 18443
NODE_PORTS = {"node-a": 18444, "node-b": 18445}
# The heartbeat SWEEP interval only; the lost-grace floor (90s) is a shipped value and
# is left alone — compressing a threshold benches software that does not ship.
HB = 10


def result_of(r):
    """Unwrap the standard {message, result} envelope. Returns {} on anything else."""
    try:
        body = r.json()
    except Exception:
        return {}
    res = body.get("result", body) if isinstance(body, dict) else {}
    return res if isinstance(res, dict) else {"result": res}


def sh(*args, check=True, capture=True):
    p = subprocess.run(args, capture_output=capture, text=True)
    if check and p.returncode != 0:
        raise SystemExit("FAILED %s\n%s\n%s" % (" ".join(args), p.stdout, p.stderr))
    return (p.stdout or "").strip()


def write_json(path, obj):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    # BOM-free or the Go config loader silently falls back to ALL DEFAULTS.
    io.open(path, "w", encoding="utf-8", newline="\n").write(json.dumps(obj, indent=2))


def base_config(app, tls_port):
    src = json.load(io.open(os.path.join(REPO, "apps", app, "config.json"), encoding="utf-8-sig"))
    src["db"] = {"engine": "sqlite", "host": "", "port": 0, "user": "", "password": "",
                 "db_name": "/data/%s.db" % app, "ssl_mode": ""}
    src["server"]["tlsPorts"] = [tls_port]
    src["server"]["nonTlsPorts"] = []
    src["tls"] = {"certPath": "/data/certs/cert.pem", "keyPath": "/data/certs/key.pem"}
    src["fileStorage"]["path"] = "/data/uploads"
    src["logging"]["path"] = "/data/logs/%s.log" % app
    src["pairing"]["heartbeatIntervalSeconds"] = HB
    src["localAuth"] = {"enabled": True, "username": "admin", "password": "admin123"}
    # A tunneled request carries no JWT, so every tunneled call shares one bucket.
    src["rateLimit"]["enabled"] = False
    if "sso" in src:
        src["sso"]["enabled"] = False
    return src


def teardown(wipe=False):
    for name in ["cp", "node-a", "node-b"]:
        sh("docker", "rm", "-f", name, check=False)
    sh("docker", "network", "rm", "benchnet", check=False)
    if wipe:
        # Data dirs survive a container removal, so a re-run otherwise inherits a
        # rotated password and an already-paired node and fails in confusing ways.
        for name in ["cp", "node-a", "node-b"]:
            d = os.path.join(ROOT, name)
            for root, dirs, files in os.walk(d, topdown=False):
                for f in files:
                    try:
                        os.remove(os.path.join(root, f))
                    except OSError:
                        pass
                for x in dirs:
                    try:
                        os.rmdir(os.path.join(root, x))
                    except OSError:
                        pass


def start_container(name, app, tls_port, host_port):
    data = os.path.join(ROOT, name)
    args = ["docker", "run", "-d", "--name", name, "--network", "benchnet",
            "-p", "%d:%d" % (host_port, tls_port),
            "-v", os.path.join(ROOT, "bin") + ":/bin/app:ro",
            "-v", os.path.join(REPO, "apps", app) + ":/home/app:ro",
            "-v", data + ":/data",
            "-e", "%s_HOME=/home/app" % app.upper(),
            "-e", "%s_DATA=/data" % app.upper(),
            "-w", "/data", "debian:bookworm-slim", "/bin/app/" + app]
    sh(*args)


def wait_up(url, timeout=90):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            requests.get(url, verify=False, timeout=3)
            return True
        except Exception:
            time.sleep(1)
    return False


class Node:
    """mymatasan accepts Basic auth on every request; the bootstrap admin is
    must-change, so everything but /auth/session and /auth/change-password 403s until
    the password is rotated."""

    def __init__(self, base):
        self.base = base
        self.auth = ("admin", "admin123")

    def rotate(self, new="Bench!2345"):
        r = requests.post(self.base + "/api/auth/change-password", auth=self.auth,
                          json={"currentPassword": self.auth[1], "newPassword": new},
                          verify=False, timeout=15)
        if r.status_code == 200:
            self.auth = ("admin", new)
        return r.status_code

    def put(self, path, body):
        return requests.put(self.base + path, auth=self.auth, json=body, verify=False, timeout=20)

    def post(self, path, body=None):
        return requests.post(self.base + path, auth=self.auth, json=body or {}, verify=False, timeout=20)

    def get(self, path):
        return requests.get(self.base + path, auth=self.auth, verify=False, timeout=20)


class CP:
    """myseliasan refuses Basic auth: session cookie + X-CSRF-Token echoed from the
    cookie on every write."""

    def __init__(self, base):
        self.base = base
        self.s = requests.Session()
        self.s.verify = False
        # REQUESTS_CA_BUNDLE in the environment OVERRIDES session.verify, so a bench
        # against a self-signed cert fails with a verification error that looks like the
        # app's fault. Turn env trust off.
        self.s.trust_env = False

    def login(self, user="admin", pw="admin123"):
        r = self.s.post(self.base + "/api/auth/local-login",
                        json={"username": user, "password": pw}, timeout=20)
        return r

    def csrf(self):
        # Over TLS the cookie is the __Host- prefixed one; plain HTTP gets the dev name.
        for name in ("__Host-kopiv2_csrf", "kopiv2_csrf"):
            v = self.s.cookies.get(name)
            if v:
                return v
        return ""

    def get(self, path):
        return self.s.get(self.base + path, timeout=30)

    def post(self, path, body=None):
        return self.s.post(self.base + path, json=body or {},
                           headers={"X-CSRF-Token": self.csrf()}, timeout=60)

    def change_password(self, new="Bench!2345"):
        return self.s.post(self.base + "/api/auth/change-password",
                           json={"currentPassword": "admin123", "newPassword": new},
                           headers={"X-CSRF-Token": self.csrf()}, timeout=20)


PASSWORDS = ["admin123", "Bench!2345"]


def main():
    os.makedirs(os.path.join(ROOT, "bin"), exist_ok=True)
    teardown(wipe=True)
    sh("docker", "network", "create", "benchnet")

    cpcfg = base_config("myseliasan", 3002)
    # THE ONE THAT BITES. The parent stamps this URL onto every node it adopts, and the
    # node uses it to enroll for its certificate and to dial the control channel. Left at
    # the default the node records "localhost:3002" — its OWN localhost — so enrollment
    # fails forever, it never gets a cert, the control channel never comes up, and the
    # node drifts to "lost" 90 seconds after adoption entirely on its own. A bench that
    # then stops a container measures an outage that was already happening.
    cpcfg["pairing"]["parentBaseUrl"] = "https://cp:3002"
    write_json(os.path.join(ROOT, "cp", "config.json"), cpcfg)
    for name, port in NODE_PORTS.items():
        cfg = base_config("mymatasan", 3000)
        # Every node app must stamp the SAME mtlsPort the parent expects on nodes.
        cfg["pairing"]["mtlsPort"] = 39532
        write_json(os.path.join(ROOT, name, "config.json"), cfg)

    start_container("cp", "myseliasan", 3002, CP_PORT)
    for name, port in NODE_PORTS.items():
        start_container(name, "mymatasan", 3000, port)

    cp_url = "https://127.0.0.1:%d" % CP_PORT
    if not wait_up(cp_url + "/api/auth/session"):
        print(sh("docker", "logs", "--tail", "40", "cp", check=False))
        raise SystemExit("control plane did not come up")
    for name, port in NODE_PORTS.items():
        if not wait_up("https://127.0.0.1:%d/api/auth/session" % port):
            print(sh("docker", "logs", "--tail", "40", name, check=False))
            raise SystemExit("%s did not come up" % name)
    print("all three containers are serving")

    cp = CP(cp_url)
    for pw in PASSWORDS:
        r = cp.login(pw=pw)
        if r.status_code == 200:
            print("cp login:", pw, r.text[:120])
            if result_of(r).get("mustChangePassword"):
                print("cp rotate:", cp.change_password().status_code)
                cp.login(pw="Bench!2345")
            break
    else:
        raise SystemExit("cp login failed for every known password")

    key = result_of(cp.get("/api/nodes/fleet-key")).get("fleetKey")
    if not key:
        r = cp.post("/api/nodes/fleet-key")
        key = result_of(r).get("fleetKey")
        if not key:
            raise SystemExit("no fleet key: %s %s" % (r.status_code, r.text[:300]))
    print("fleet key:", key[:12] + "...")

    adopted = {}
    for name, port in NODE_PORTS.items():
        n = Node("https://127.0.0.1:%d" % port)
        for pw in PASSWORDS:
            n.auth = ("admin", pw)
            if n.get("/api/pairing/status").status_code == 200:
                break
            if n.rotate() == 200:
                break
        st = result_of(n.get("/api/pairing/status"))
        node_id = st.get("nodeId") or st.get("NodeId")
        print(name, "identity:", node_id, "paired:", st.get("paired"))
        # The node takes the fleet key as "key", not "fleetKey".
        print(name, "fleet-key:", n.put("/api/pairing/fleet-key", {"key": key}).status_code)
        code = result_of(n.post("/api/pairing/claim-code")).get("code")
        print(name, "claim:", code)
        # Adoption is parent -> node over HTTPS, using the docker network alias.
        r = cp.post("/api/nodes/adopt", {"nodeId": node_id, "ip": name, "httpsPort": 3000,
                                         "claimCode": code, "name": name})
        print(name, "adopt:", r.status_code, r.text[:300])
        adopted[name] = r.status_code == 200

    print(json.dumps(adopted))
    r = cp.get("/api/nodes")
    print("nodes:", r.status_code, r.text[:600])


if __name__ == "__main__":
    main()
