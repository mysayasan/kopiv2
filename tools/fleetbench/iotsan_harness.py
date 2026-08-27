# Stand up myiotsan with a REAL MQTT device on the broker.
#
# WHY THIS EXISTS. myiotsan is the last app in the suite that had never been run against anything.
# It has 11 test files and, until this harness, zero live exercise — and it is the app that WRITES
# to the physical world. A bad relay write is not a wrong number on a chart; it is a door that
# opens, a breaker that trips, a thermostat set to 200 degrees. The whole safety story is one
# chokepoint (services.CommandService.Issue) that every actuation is supposed to pass through, and
# a chokepoint is exactly the kind of claim that unit tests cannot check: each caller's test can
# pass while a caller nobody wrote a test for goes around it.
#
# So this stands up the whole chain the way a site runs it: the app, its EMBEDDED broker, and a
# real MQTT client authenticated as a real provisioned device, subscribed to the topic a relay
# would actually listen on. A command issued here travels the wire it travels in a plant room, and
# the bench can see exactly what arrived — which is the only way to tell "refused" from "refused,
# and the relay fired anyway".
#
# WHAT IT TAKES, and none of it is guesswork:
#
#   - myiotsan needs NO redis (its shipped cache provider is the in-process one) and NO postgres
#     (sqlite is its shipped default). Like mypintusan, it is easy to stand up.
#   - it HAS a `pairing` block, so fleet_harness.base_config works on it, unlike myidsan.
#   - it owns two config blocks nothing else in the suite has: `mqtt` and `telemetry_store`. The
#     broker must be bound to 0.0.0.0 and its port PUBLISHED, or a device on the host cannot
#     connect and every wire assertion silently passes on an empty result.
#   - it is an APPLIANCE app: shared local Basic auth. Session probe `GET /api/auth/session`,
#     rotate via `POST /api/auth/change-password`. NOT myseliasan's `/api/auth/local-login`, NOT
#     myidsan's `/api/login/default`. The bootstrap admin is must-change; everything else 403s
#     until it is rotated.
#   - the broker ACL confines a client to topics CONTAINING ITS OWN KEY (DeviceService
#     .AuthorizeTopic). A bench device therefore subscribes to `.../<deviceKey>/#` and nothing
#     else — the same confinement a real sensor lives under, which is the point.
#
#   python tools/fleetbench/iotsan_harness.py
import io
import json
import os
import shutil
import subprocess
import sys
import threading
import time

import requests
import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import REPO, ROOT, base_config, sh, wait_up
from idsan_harness import host_ip, make_cert

urllib3.disable_warnings()

NET = "iotbench"
APP = "myiotsan"
NAME = "iot"
TLS_PORT = 3003
HOST_PORT = 18483
MQTT_PORT = 1883
HOST_MQTT_PORT = 18883
HOST = host_ip()
BASE = "https://%s:%d" % (HOST, HOST_PORT)
WORK = os.path.join(ROOT, "iot-certs")

ADMIN_USER = "admin"
ADMIN_PASS = "admin123"
BENCH_PASS = "Bench!2345678"

# tools/sunspec-sim runs on the HOST (it is a go binary, not a container), so the app reaches it
# at the host's LAN address — 127.0.0.1 inside a container is the container. Port 1502 rather than
# the real 502, which needs root.
SIM_PORT = 1502
SIM_ADDR = "%s:%d" % (HOST, SIM_PORT)


def app_config():
    cfg = base_config(APP, TLS_PORT)
    cfg["localAuth"] = {"enabled": True, "username": ADMIN_USER, "password": ADMIN_PASS}
    # Multicast discovery finds nothing in docker and logs about it forever.
    cfg["pairing"]["enabled"] = False
    # The embedded broker. 0.0.0.0 so the published port actually reaches it — bound to loopback
    # inside the container, a device on the host connects to nothing and every wire check reads
    # as "no message arrived", which is indistinguishable from "the gate refused it".
    cfg["mqtt"] = {"enabled": True, "addr": "0.0.0.0:%d" % MQTT_PORT}
    # Flush fast: the bench asserts on stored readings within seconds, not on the 250ms/200-row
    # production batching window.
    cfg["telemetry_store"] = {
        "batchSize": 1,
        "flushMs": 50,
        "queueSize": 8192,
        "rawRetentionDays": 30,
        "rollupRetentionDays": 400,
    }
    # A bench issues commands in bursts; the shipped per-endpoint rate limit is not what is under
    # test here (the per-DEVICE actuation rate limit is, and that one stays on).
    cfg["rateLimit"]["enabled"] = False
    return cfg


def write(path, obj):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    io.open(path, "w", encoding="utf-8", newline="\n").write(json.dumps(obj, indent=2))


def build():
    """Build myiotsan for the container.

    KOPIV2_SKIP_BUILD=1 reuses whatever is already at that path, so the SAME bench file can be
    pointed at a binary built from another commit — which is how before/after numbers are measured
    against one identical set of checks rather than two bench versions."""
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
       "-p", "%d:%d" % (HOST_MQTT_PORT, MQTT_PORT),
       "-v", os.path.join(ROOT, "bin") + ":/bin/app:ro",
       "-v", os.path.join(REPO, "apps", APP) + ":/home/app:ro",
       "-v", data + ":/data",
       "-e", "%s_HOME=/home/app" % APP.upper(),
       "-e", "%s_DATA=/data" % APP.upper(),
       "-w", "/data", "debian:bookworm-slim", "/bin/app/" + APP)


def build_sim():
    """Build tools/sunspec-sim for the HOST.

    Always built from THIS tree, even under KOPIV2_SKIP_BUILD: the simulator is bench tooling, not
    the product, so a before/after comparison must hold it constant and vary only the app."""
    out = os.path.join(ROOT, "bin", "sunspec-sim.exe" if os.name == "nt" else "sunspec-sim")
    subprocess.run(["go", "build", "-o", out, "./tools/sunspec-sim"], cwd=REPO, check=True)
    return out


def start_sim(scenario="sunny", speed=1, tick_ms=500, pv=10000, extra=None):
    """Run the simulator on the host, serving all four units.

    `speed=1` (realtime) is the default HERE, unlike the simulator's own 1800: a write bench asserts
    on a register it just wrote and on the plant's response to it, and a compressed day moves the
    physics far enough between two reads to make a comparison meaningless. Benches that want to
    fast-forward a schedule pass their own speed."""
    args = [os.path.join(ROOT, "bin", "sunspec-sim.exe" if os.name == "nt" else "sunspec-sim"),
            "-addr", ":%d" % SIM_PORT, "-scenario", scenario,
            "-speed", str(speed), "-tick", str(tick_ms), "-pv", str(pv), "-quiet"]
    if extra:
        args += list(extra)
    proc = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)

    # A STALE simulator from a previous run keeps the port, so this one dies on bind while the
    # port still answers — and the bench then measures the OLD binary, at the OLD time of day,
    # with none of the flags it just asked for, reporting nothing wrong. Cost a run to learn: the
    # start is not complete until the process we launched is alive AND serving.
    deadline = time.time() + 15
    while time.time() < deadline:
        if proc.poll() is not None:
            raise SystemExit("the simulator exited immediately — most likely another one still "
                             "holds :%d. Its output:\n%s" % (SIM_PORT, proc.stdout.read()))
        try:
            Modbus(port=SIM_PORT).read_holding(1, 40000, 2)
            return proc
        except Exception:
            time.sleep(0.3)
    proc.kill()
    raise SystemExit("the simulator did not start serving on :%d" % SIM_PORT)


def stop_sim(proc):
    if proc is None:
        return
    try:
        proc.kill()
        proc.wait(timeout=10)
    except Exception:
        pass


class Modbus:
    """A minimal Modbus TCP client — the bench's OWN eyes on the wire.

    This exists so a write can be verified INDEPENDENTLY of the thing that made it. myiotsan's own
    WriteConfirm reads the register back, so asking the app whether the write landed is asking the
    accused to testify: a confirm that never wrote, or wrote elsewhere, would answer exactly the
    same. This client reads the simulator directly.

    Stdlib sockets only, same reasoning as the simulator itself carrying no Modbus dependency."""

    def __init__(self, host=HOST, port=SIM_PORT, timeout=5.0):
        self.host, self.port, self.timeout = host, port, timeout
        self._tid = 0

    def _txn(self, unit, pdu):
        import socket
        import struct

        self._tid = (self._tid + 1) % 0xFFFF
        frame = struct.pack(">HHHB", self._tid, 0, len(pdu) + 1, unit) + pdu
        sock = socket.create_connection((self.host, self.port), self.timeout)
        try:
            sock.settimeout(self.timeout)
            sock.sendall(frame)
            head = self._recv(sock, 8)
            length = struct.unpack(">H", head[4:6])[0]
            body = self._recv(sock, length - 2) if length > 2 else b""
            resp = head[7:8] + body
        finally:
            sock.close()
        if resp[0] & 0x80:
            raise ModbusError("modbus exception %d on unit %d" % (resp[1], unit))
        return resp

    @staticmethod
    def _recv(sock, n):
        buf = b""
        while len(buf) < n:
            chunk = sock.recv(n - len(buf))
            if not chunk:
                raise ModbusError("the simulator closed the connection")
            buf += chunk
        return buf

    def read_holding(self, unit, addr, qty=1):
        import struct

        resp = self._txn(unit, struct.pack(">BHH", 3, addr, qty))
        count = resp[1]
        return list(struct.unpack(">%dH" % (count // 2), resp[2:2 + count]))

    def write_single(self, unit, addr, value):
        import struct

        self._txn(unit, struct.pack(">BHH", 6, addr, value & 0xFFFF))


class ModbusError(Exception):
    pass


def as_i16(v):
    """Read a register as a SIGNED 16-bit value — the sign convention that is this domain's
    classic footgun. A curtailment written as -40 is 65496 on the wire, and a bench that compares
    the raw word against -40 reports a correct write as a failure (or the reverse)."""
    return v - 0x10000 if v >= 0x8000 else v


def sunspec_models(mb, unit, base=40000):
    """Walk a unit's SunSpec chain and return {model id: (first point address, length)}.

    A command's register has to be authored ABSOLUTELY, and a SunSpec model's address depends on
    the chain in front of it — so the bench discovers the addresses rather than hard-coding
    arithmetic that a change to the chain would silently invalidate. It also proves, before any
    write, that the device really is presenting the chain the profile assumes."""
    marker = mb.read_holding(unit, base, 2)
    if bytes([marker[0] >> 8, marker[0] & 0xFF, marker[1] >> 8, marker[1] & 0xFF]) != b"SunS":
        raise ModbusError("unit %d has no SunS marker at %d" % (unit, base))
    out, cur = {}, base + 2
    while True:
        head = mb.read_holding(unit, cur, 2)
        mid, length = head[0], head[1]
        if mid == 0xFFFF:
            return out
        out[mid] = (cur + 2, length)
        cur += 2 + length
        if cur > base + 1000:
            raise ModbusError("unit %d chain did not terminate" % unit)


def teardown():
    sh("docker", "rm", "-f", NAME, check=False)
    sh("docker", "network", "rm", NET, check=False)
    # A surviving data dir means the second run reuses the first run's devices, rotated password
    # and command history — and a rate limit keyed by device id would then be primed by a run
    # that is no longer on screen.
    shutil.rmtree(os.path.join(ROOT, NAME), ignore_errors=True)


class Client:
    """A session against myiotsan.

    myiotsan is an APPLIANCE app: the shared local Basic-auth stack
    (domain/shared/apis.NewLocalBasicAuth), the same one mymatasan and mypintusan use. Every
    request carries the credential; the session probe is `GET /api/auth/session`."""

    def __init__(self, base=BASE, user=ADMIN_USER, password=ADMIN_PASS):
        self.base = base
        self.auth = (user, password)
        self.s = requests.Session()
        self.s.verify = False
        # REQUESTS_CA_BUNDLE in the environment OVERRIDES session.verify and the failure reads
        # like the app's fault.
        self.s.trust_env = False

    def rotate(self, new=BENCH_PASS):
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


def result(r):
    """Unwrap this suite's envelope: {"data": {"result": ...}}.

    THE ENVELOPE TRAP, restated because it has cost real time: reaching for r.json()["result"]
    or for a bare list makes a working endpoint read as empty, and a check that passes on an
    empty result is not a check."""
    try:
        body = r.json()
    except ValueError:
        return None
    if isinstance(body, dict) and "data" in body and isinstance(body["data"], dict):
        return body["data"].get("result")
    return body.get("result") if isinstance(body, dict) else None


def result_list(r, field="items"):
    res = result(r)
    if isinstance(res, dict):
        return res.get(field) or []
    return res or []


def admin():
    """Sign in and rotate the bootstrap password if the app demands it.

    BOTH passwords are tried, because an instance a previous run already rotated no longer answers
    to the bootstrap one — and the refusal is a bare "access denied" that reads like a broken app
    rather than a stale password."""
    c = Client()
    r = c.get("/api/auth/session")
    if r.status_code != 200:
        c.auth = (ADMIN_USER, BENCH_PASS)
        r = c.get("/api/auth/session")
    if r.status_code != 200:
        raise SystemExit("could not reach %s: %s" % (BASE, (r.text or "")[:200]))
    try:
        must = (result(r) or {}).get("mustChangePassword")
    except AttributeError:
        must = False
    if must:
        c.rotate()
    return c


class DeviceWire:
    """A real MQTT client authenticated as a provisioned device — the wire itself.

    This is the load-bearing part of the whole bench. Asserting that the API said "refused" proves
    only what the API said. Asserting that NOTHING ARRIVED HERE proves the relay did not move, and
    asserting that something DID arrive proves it moved even when the API said no.

    The broker authenticates on the device key + the password minted once at provisioning, and the
    ACL confines the session to topics containing that key — so this client sees exactly what the
    real device would see and nothing else."""

    def __init__(self, device_key, password, host=HOST, port=HOST_MQTT_PORT):
        import paho.mqtt.client as mqtt

        self.key = device_key
        self.messages = []
        self._lock = threading.Lock()
        self._connected = threading.Event()
        try:
            self.c = mqtt.Client(mqtt.CallbackAPIVersion.VERSION1, client_id=device_key)
        except AttributeError:  # paho 1.x
            self.c = mqtt.Client(client_id=device_key)
        # The broker reads the key from the USERNAME when present, falling back to the client id.
        # Both are set to the device key so this works either way.
        self.c.username_pw_set(device_key, password)
        self.c.on_connect = self._on_connect
        self.c.on_message = self._on_message
        self.c.connect(host, port, keepalive=30)
        self.c.loop_start()
        if not self._connected.wait(15):
            raise SystemExit("device %s could not connect to the broker at %s:%d"
                             % (device_key, host, port))

    def _on_connect(self, client, userdata, flags, rc, properties=None):
        if rc == 0:
            self._connected.set()

    def _on_message(self, client, userdata, msg):
        with self._lock:
            self.messages.append((msg.topic, msg.payload.decode("utf-8", "replace"), time.time()))

    def subscribe(self, topic):
        self.c.subscribe(topic, qos=1)
        time.sleep(0.4)  # let the SUBACK land before anything is published

    def publish(self, topic, payload, qos=1):
        self.c.publish(topic, payload, qos=qos)

    def drain(self):
        """Take everything seen so far and reset. A check that asserts 'nothing arrived' must
        start from a known-empty wire, or it is asserting about the previous check's traffic."""
        with self._lock:
            out = list(self.messages)
            self.messages = []
        return out

    def wait_for(self, timeout=5.0, count=1):
        """Wait for at least `count` messages, then return everything and reset."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            with self._lock:
                if len(self.messages) >= count:
                    break
            time.sleep(0.05)
        return self.drain()

    def quiet_for(self, seconds=2.0):
        """Return whatever arrives during `seconds` — for asserting that NOTHING did."""
        time.sleep(seconds)
        return self.drain()

    def close(self):
        try:
            self.c.loop_stop()
            self.c.disconnect()
        except Exception:
            pass


def logs(tail=60):
    return sh("docker", "logs", "--tail", str(tail), NAME, check=False)


def main():
    print("host address the container can reach:", HOST)
    teardown()
    os.makedirs(os.path.join(ROOT, "bin"), exist_ok=True)
    sh("docker", "network", "create", NET, check=False)

    build()

    data = os.path.join(ROOT, NAME)
    certs = os.path.join(data, "certs")
    os.makedirs(certs, exist_ok=True)
    crt, key = make_cert(WORK)
    io.open(os.path.join(certs, "cert.pem"), "wb").write(io.open(crt, "rb").read())
    io.open(os.path.join(certs, "key.pem"), "wb").write(io.open(key, "rb").read())

    write(os.path.join(data, "config.json"), app_config())
    start_app()

    if not wait_up(BASE + "/api/auth/config", timeout=180):
        print(logs(40))
        raise SystemExit("myiotsan did not come up")
    print("myiotsan is serving:", BASE)
    print("its broker is on %s:%d" % (HOST, HOST_MQTT_PORT))
    return 0


if __name__ == "__main__":
    sys.exit(main())
