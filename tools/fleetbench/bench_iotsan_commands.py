# Bench: a command that did not happen must SAY so — and must keep saying so.
#
# THE CLAIM UNDER TEST. myiotsan deliberately never auto-retries an actuation: re-sending a relay
# write is a SECOND PHYSICAL ACTION, and if the first one landed while its confirmation was lost,
# the retry opens the door twice. entities.DeviceCommand states the consequence of that choice
# outright — "a command that is never confirmed becomes FAILED, and STAYS failed" — and
# services.CommandService carries the same promise in its header.
#
# That promise has two halves, and the second one is the dangerous one:
#
#   1. NOTHING RETRIES. Easy to believe, and this bench still checks it on the wire rather than in
#      the source, over the whole timeout window and across a restart.
#   2. NOTHING IS SILENTLY LOST. Much harder. A command that ends up neither confirmed nor failed
#      is worse than a retry: it is an actuation that nobody will ever look at again. The operator
#      is not told, the metric is not incremented, and the row sits there claiming to be in flight.
#      "Not retried" is only safe BECAUSE a human is supposed to be handed the decision — so a
#      failure that never reaches a human turns the safety property into a silent drop.
#
# WHY A LIVE RUN. Both halves are properties of the app OVER TIME — a 30-second confirmation
# window, a 10-second sweep, a restart in the middle of a write — and none of them is reachable
# from a unit test of a pure function. The failure modes hunted here are exactly the ones that
# look identical to success in every static reading of the code: a command marked `confirmed` by
# a report that had nothing to do with it, and a command left `pending` forever by a restart.
#
# WHAT MAKES THE ASSERTIONS REAL. A real MQTT client, authenticated as the real provisioned device
# and confined by the real broker ACL, sits on the device's command topics for the whole run. It
# is also the thing that CONFIRMS: the device reports its state back over the same wire a real one
# would use, through the real ingest path. Every negative ("it did not confirm", "nothing was
# re-sent") is preceded by the positive that proves the mechanism works at all — a check that
# passes on a broken confirm path is not a check.
#
#   python tools/fleetbench/iotsan_harness.py      # stand it up
#   python tools/fleetbench/bench_iotsan_commands.py
import json
import os
import socket
import sys
import threading
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from fleet_harness import sh, wait_up
from iotsan_harness import (
    BASE,
    HOST,
    NAME,
    Client,
    DeviceWire,
    admin,
    logs,
    result,
    result_list,
)

DEVICE_KEY = "bench-life-01"
RELAY_TOPIC = "iot/cmd/%s/relay" % DEVICE_KEY
FAN_TOPIC = "iot/cmd/%s/fan" % DEVICE_KEY
BEACON_TOPIC = "iot/cmd/%s/beacon" % DEVICE_KEY
TELEMETRY_TOPIC = "iot/tel/%s" % DEVICE_KEY
WIRE_FILTER = "iot/cmd/%s/#" % DEVICE_KEY

# The Modbus half of the bench talks to a socket that ACCEPTS AND NEVER ANSWERS — a plant device
# that is powered but wedged, which is the ordinary way a write stalls in a plant room. The
# simulator is deliberately not used here: this bench needs a device that does NOT answer.
BLACKHOLE_PORT = 15020
MB_KEY = "bench-life-mb"
MB_REGISTER = 40100

# The product's own timings (services/commands.go, app/app.go). Restated here rather than guessed:
#   confirmTimeout        30s  — how long a device has to report the state back
#   commandSweepInterval  10s  — how often unconfirmed commands are ENDED
CONFIRM_TIMEOUT = 30
SWEEP_INTERVAL = 10
# Generous enough that a slow docker host does not turn a real pass into a flake, tight enough
# that "it eventually failed" still means "it failed on the timeout".
FAIL_WINDOW = CONFIRM_TIMEOUT + 2 * SWEEP_INTERVAL + 10

# The per-device duty-cycle floor (minCommandInterval).
RATE_LIMIT = 2.0

PASSES = []
FAILS = []
PROFILE_ID = 0


def check(name, ok, detail=""):
    (PASSES if ok else FAILS).append(name)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, ("  — " + detail) if detail else ""))
    return ok


def settle():
    time.sleep(RATE_LIMIT + 0.2)


def err_text(r):
    try:
        body = r.json()
    except ValueError:
        return (r.text or "")[:200]
    for k in ("message", "error", "msg"):
        if isinstance(body, dict) and body.get(k):
            return str(body[k])[:200]
    return json.dumps(body)[:200]


# --------------------------------------------------------------------------------------------
# A socket that accepts and never answers.
# --------------------------------------------------------------------------------------------

class Blackhole(object):
    """A TCP listener that completes the handshake and then says nothing, counting connections.

    Two jobs. It stalls a Modbus write inside its 3-second per-operation timeout, which is the
    window a restart has to land in; and it COUNTS how many times the app dialled, which is how
    "nothing retried it behind the operator's back" is measured on the polled transport — the
    Modbus equivalent of watching the MQTT wire.

    The accepted sockets are deliberately kept open: closing one would hand the client an EOF, the
    write would fail instantly, and the stall the bench depends on would never happen."""

    def __init__(self, port=BLACKHOLE_PORT):
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.sock.bind(("0.0.0.0", port))
        self.sock.listen(16)
        self.conns = []
        self._lock = threading.Lock()
        self._stop = False
        self.t = threading.Thread(target=self._run, daemon=True)
        self.t.start()

    def _run(self):
        self.sock.settimeout(0.5)
        while not self._stop:
            try:
                conn, _ = self.sock.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            with self._lock:
                self.conns.append(conn)

    @property
    def dials(self):
        with self._lock:
            return len(self.conns)

    def close(self):
        self._stop = True
        for c in list(self.conns):
            try:
                c.close()
            except OSError:
                pass
        try:
            self.sock.close()
        except OSError:
            pass


# --------------------------------------------------------------------------------------------
# Setup
# --------------------------------------------------------------------------------------------

def make_profile(c):
    """A device type with three commands that confirm on THREE DIFFERENT keys — and one that
    cannot confirm at all.

    The shape is the point. `relay` and `fan` are both switches, so both carry the value 1, and
    they report on different telemetry keys. `beacon` declares NO confirmKey, which the product
    documents as an honest "sent, never confirmed" (a device that reports colour per-channel
    cannot be confirmed by one float, so the profile declares nothing). Any of those three is a
    perfectly ordinary building fit-out: a lock, a fan, and an indicator on one controller."""
    body = {
        "slug": "bench-life",
        "name": "Bench command lifecycle",
        "vendor": "kopiv2-bench",
        "topicTemplate": TELEMETRY_TOPIC,
        "payloadFormat": "json",
        "keys": [
            {"key": "state", "label": "Relay state", "dataType": "number", "jsonPath": "state"},
            {"key": "fan_state", "label": "Fan state", "dataType": "number", "jsonPath": "fan"},
            {"key": "sp", "label": "Setpoint", "unit": "C", "dataType": "number", "jsonPath": "sp"},
        ],
        "commands": [
            {"name": "relay", "label": "Relay", "kind": "switch",
             "topicTemplate": "iot/cmd/{deviceKey}/relay",
             "payloadTemplate": '{"state":{value}}', "confirmKey": "state"},
            {"name": "fan", "label": "Fan", "kind": "switch",
             "topicTemplate": "iot/cmd/{deviceKey}/fan",
             "payloadTemplate": '{"fan":{value}}', "confirmKey": "fan_state"},
            # No confirmKey: the product's own documented "sent is the best that can ever be said".
            {"name": "beacon", "label": "Beacon", "kind": "switch",
             "topicTemplate": "iot/cmd/{deviceKey}/beacon",
             "payloadTemplate": '{"beacon":{value}}'},
        ],
    }
    r = c.post("/api/profiles", body)
    if r.status_code != 200:
        existing = [p for p in result_list(c.get("/api/profiles")) if p.get("slug") == body["slug"]]
        if not existing:
            raise SystemExit("could not create the bench profile: %s" % err_text(r))
        pid = existing[0].get("id")
        r = c.put("/api/profiles/%d" % pid, body)
        if r.status_code != 200:
            raise SystemExit("could not update the bench profile: %s" % err_text(r))
        return pid
    return (result(r) or {}).get("profile", {}).get("id") or (result(r) or {}).get("id")


def make_mqtt_device(c, profile_id):
    r = c.post("/api/devices", {
        "name": "Bench lifecycle relay", "deviceKey": DEVICE_KEY, "protocol": "mqtt",
        "profileId": profile_id, "enabled": True, "actuationEnabled": True,
    })
    if r.status_code == 200:
        res = result(r) or {}
        dev = res.get("device") or res
        return dev.get("id"), res.get("password") or dev.get("password")
    return reuse_device(c, DEVICE_KEY, r)


def rotate_password(c, dev_id):
    """Mint a new broker credential for a device. The provisioning password is returned exactly
    once, so every reconnect this bench makes needs a fresh one."""
    rot = c.post("/api/devices/%d/password" % dev_id)
    password = (result(rot) or {}).get("password")
    if not password:
        raise SystemExit("could not rotate the device credential: %s" % err_text(rot))
    return password


def reuse_device(c, key, r):
    """A re-run against a surviving instance: the key is taken, so reuse the device and mint a new
    broker credential (the provisioning password is returned exactly once and cannot be read
    back — correct product behaviour, and the reason this exists)."""
    existing = [d for d in result_list(c.get("/api/devices?limit=200")) if d.get("deviceKey") == key]
    if not existing:
        raise SystemExit("could not create device %s: %s" % (key, err_text(r)))
    dev_id = existing[0].get("id")
    return dev_id, rotate_password(c, dev_id)


def make_modbus_profile(c):
    """A Modbus device type that declares a COMMAND and no telemetry keys.

    Declaring no keys is deliberate: the poller refuses to run such a profile out loud (the fix
    that came out of the Modbus bench), so nothing but the command path ever dials the endpoint —
    which is what lets the blackhole's connection count be read as "how many times did the app
    try to write". A command binds an absolute register and needs no read map."""
    body = {
        "slug": "bench-life-mb",
        "name": "Bench lifecycle Modbus",
        "vendor": "kopiv2-bench",
        "transport": "modbus",
        "modbusMode": "sunspec",
        "modbusBase": 40000,
        "pollSeconds": 3600,
        "keys": [],
        "commands": [
            {"name": "setpoint_reg", "label": "Register setpoint", "kind": "setpoint",
             "min": 0, "max": 100,
             "transport": "modbus", "register": MB_REGISTER, "regKind": "u16", "scaleFactor": 1},
        ],
    }
    r = c.post("/api/profiles", body)
    if r.status_code != 200:
        existing = [p for p in result_list(c.get("/api/profiles")) if p.get("slug") == body["slug"]]
        if not existing:
            raise SystemExit("could not create the modbus bench profile: %s" % err_text(r))
        pid = existing[0].get("id")
        r = c.put("/api/profiles/%d" % pid, body)
        if r.status_code != 200:
            raise SystemExit("could not update the modbus bench profile: %s" % err_text(r))
        return pid
    return (result(r) or {}).get("profile", {}).get("id") or (result(r) or {}).get("id")


def make_modbus_device(c, profile_id):
    body = {
        "name": "Bench wedged inverter", "deviceKey": MB_KEY, "protocol": "modbus",
        "profileId": profile_id, "enabled": True, "actuationEnabled": True,
        "endpoint": "%s:%d" % (HOST, BLACKHOLE_PORT), "unit": 1, "transport": "tcp",
    }
    r = c.post("/api/devices", body)
    if r.status_code == 200:
        res = result(r) or {}
        dev = res.get("device") or res
        return dev.get("id")
    existing = [d for d in result_list(c.get("/api/devices?limit=200")) if d.get("deviceKey") == MB_KEY]
    if not existing:
        raise SystemExit("could not create the modbus bench device: %s" % err_text(r))
    return existing[0].get("id")


# --------------------------------------------------------------------------------------------
# Small readers
# --------------------------------------------------------------------------------------------

def history(c, dev_id, limit=100):
    return result_list(c.get("/api/devices/%d/commands/history?limit=%d" % (dev_id, limit)))


def issue(c, dev_id, name, value):
    return c.post("/api/devices/%d/commands" % dev_id, {"name": name, "value": value})


def row(c, dev_id, cmd_id):
    for x in history(c, dev_id):
        if x.get("id") == cmd_id:
            return x
    return None


def wait_status(c, dev_id, cmd_id, want, timeout):
    """Poll one command row until it reaches `want`. Returns (row, seconds) or (last row, None)."""
    start = time.time()
    last = None
    while time.time() - start < timeout:
        cur = row(c, dev_id, cmd_id)
        if cur:
            last = cur
            if cur.get("status") == want:
                return cur, time.time() - start
        time.sleep(1)
    return last, None


def notifications(c, limit=200):
    return result_list(c.get("/api/notifications?limit=%d" % limit))


# --------------------------------------------------------------------------------------------
# A. The confirmation loop works at all. Everything below is measured against this.
# --------------------------------------------------------------------------------------------

def bench_confirm_path(c, dev_id, wire):
    print("\n--- the confirmation loop ------------------------------------------------------")
    wire.drain()
    r = issue(c, dev_id, "relay", 1)
    msgs = wire.wait_for(timeout=6)
    cmd = result(r) or {}
    ok = r.status_code == 200 and any(t == RELAY_TOPIC for t, _, _ in msgs)
    check("a command reaches the real device on the real broker", ok,
          "http=%s wire=%s" % (r.status_code, msgs))
    if not ok:
        raise SystemExit("the wire is not carrying commands — every later assertion would be "
                         "vacuous. app logs:\n%s" % logs(40))
    check("and it is recorded as SENT, not as done", cmd.get("status") == "sent",
          "status=%s" % cmd.get("status"))

    # The device reports the state back over the same wire a real one would use. THIS is the only
    # thing that may turn a command into "confirmed".
    wire.publish(TELEMETRY_TOPIC, json.dumps({"state": 1}))
    got, secs = wait_status(c, dev_id, cmd.get("id"), "confirmed", 20)
    check("the device reporting the state back CONFIRMS the command",
          secs is not None and (got or {}).get("confirmedAt", 0) > 0,
          "status=%s after %s" % ((got or {}).get("status"), "%.1fs" % secs if secs else "never"))
    return cmd.get("id")


# --------------------------------------------------------------------------------------------
# B. An unconfirmed command fails — once, visibly, and permanently.
# --------------------------------------------------------------------------------------------

def bench_unconfirmed_fails_and_stays(c, dev_id, wire):
    print("\n--- an unconfirmed command -----------------------------------------------------")
    settle()
    rows_before = len(history(c, dev_id))
    wire.drain()
    # The device is listening but will say NOTHING about this one — the ordinary "did the relay
    # actually move?" case: the message left, and no report came back.
    r = issue(c, dev_id, "relay", 0)
    cmd = result(r) or {}
    cid = cmd.get("id")
    sent = wire.wait_for(timeout=6)
    check("the command reaches the device and is recorded as sent",
          r.status_code == 200 and len(sent) == 1 and sent[0][0] == RELAY_TOPIC,
          "wire=%s" % sent)

    # Inside the window it must still be SENT — a command that failed instantly would make the
    # timeout check below pass for the wrong reason.
    time.sleep(6)
    mid = row(c, dev_id, cid) or {}
    check("inside the confirmation window it is still in flight, not failed",
          mid.get("status") == "sent", "status=%s" % mid.get("status"))

    got, secs = wait_status(c, dev_id, cid, "failed", FAIL_WINDOW)
    check("an unconfirmed command becomes FAILED on the timeout",
          secs is not None, "status=%s after %ds" % ((got or {}).get("status"), FAIL_WINDOW))
    check("and the failure says plainly that it was not confirmed and was not retried",
          "report" in (got or {}).get("error", "").lower()
          and "retr" in (got or {}).get("error", "").lower(),
          (got or {}).get("error", "")[:160])
    check("a failed command is never claimed to be confirmed",
          (got or {}).get("confirmedAt", 0) == 0, "confirmedAt=%s" % (got or {}).get("confirmedAt"))

    # NOTHING was re-sent while it was timing out. The wire has not been drained since the single
    # message above, so anything here is a second physical action.
    stray = [m for m in wire.drain() if m[0].startswith("iot/cmd/")]
    check("NOTHING was re-sent to the device during the whole timeout window", not stray, str(stray))

    # And the operator is actually TOLD. "Not retried" is only safe because a human is handed the
    # decision — a failure that reaches no human is a silent drop wearing a failed status.
    notes = [n for n in notifications(c)
             if "not confirmed" in (n.get("body") or "").lower()
             and "Bench lifecycle relay" in (n.get("body") or "")]
    check("the operator is told: the unconfirmed command is on the notification feed",
          bool(notes), "%d matching notifications" % len(notes))

    # THE HALF THAT MATTERS. The device comes back and reports the state late. The command must
    # STAY failed: it already told a human it was not confirmed, and a late report cannot prove
    # that this command is what caused the state.
    before = row(c, dev_id, cid) or {}
    wire.publish(TELEMETRY_TOPIC, json.dumps({"state": 0}))
    time.sleep(6)
    after = row(c, dev_id, cid) or {}
    check("a LATE report does not resurrect the failed command",
          after.get("status") == "failed" and after.get("confirmedAt", 0) == 0,
          "status=%s confirmedAt=%s (was %s)"
          % (after.get("status"), after.get("confirmedAt"), before.get("status")))
    check("and no new command row appeared behind the operator's back",
          len(history(c, dev_id)) == rows_before + 1,
          "%d -> %d rows" % (rows_before, len(history(c, dev_id))))

    # The twin still SHOWS the disagreement — the entity says an operator must be able to see that
    # what was asked never took effect.
    twin = result_list(c.get("/api/devices/%d/twin" % dev_id))
    state = [a for a in twin if a.get("key") == "state"]
    check("the twin still shows what was asked for and what the device says",
          bool(state) and state[0].get("hasReported"),
          json.dumps(state[:1])[:200])
    return cid


# --------------------------------------------------------------------------------------------
# C. A report may only confirm the command it belongs to.
# --------------------------------------------------------------------------------------------

def bench_cross_command_confirmation(c, dev_id, wire):
    """Three commands are outstanding; the device reports ONE of them.

    This is the check that a static reading of the code cannot make for you. Confirmation matches
    a report against the commands waiting on a device, and the question is what "waiting on" means:
    the command that declared THIS telemetry key, or any command that happens to carry the same
    number. A relay and a fan on one controller are both switches, so both are the value 1 — and if
    the number alone is enough, reporting the relay confirms the fan that never moved, and the fan
    command never fails at all. That is both halves of the promise broken at once by one report:
    an actuation claimed to have happened, and a failure silently lost."""
    print("\n--- a report confirms ONE command ----------------------------------------------")
    settle()
    wire.drain()
    r1 = issue(c, dev_id, "relay", 1)
    settle()
    r2 = issue(c, dev_id, "fan", 1)
    settle()
    r3 = issue(c, dev_id, "beacon", 1)
    ids = [(result(r) or {}).get("id") for r in (r1, r2, r3)]
    ok = all(r.status_code == 200 for r in (r1, r2, r3)) and all(ids)
    check("three commands are outstanding on one device", ok,
          "http=%s ids=%s" % ([r.status_code for r in (r1, r2, r3)], ids))
    if not ok:
        return
    topics = sorted(t for t, _, _ in wire.drain())
    check("all three reached the device on their own topics",
          topics == sorted([RELAY_TOPIC, FAN_TOPIC, BEACON_TOPIC]), str(topics))

    # The device reports ONLY the relay's key. The fan never moved; the beacon cannot report at all.
    wire.publish(TELEMETRY_TOPIC, json.dumps({"state": 1}))
    relay, secs = wait_status(c, dev_id, ids[0], "confirmed", 20)
    check("the command whose key was reported IS confirmed",
          secs is not None, "relay=%s" % (relay or {}).get("status"))

    fan = row(c, dev_id, ids[1]) or {}
    check("a command on a DIFFERENT key is NOT confirmed by that report",
          fan.get("status") != "confirmed",
          "fan=%s confirmedAt=%s" % (fan.get("status"), fan.get("confirmedAt")))

    beacon = row(c, dev_id, ids[2]) or {}
    check("a command that declares no confirm key is never confirmed by another key's report",
          beacon.get("status") != "confirmed",
          "beacon=%s confirmedAt=%s" % (beacon.get("status"), beacon.get("confirmedAt")))

    # ...and because they were not confirmed, they must still END — as failures the operator sees.
    fan, fsecs = wait_status(c, dev_id, ids[1], "failed", FAIL_WINDOW)
    check("the unreported command still ends as a failure the operator can see",
          fsecs is not None, "fan=%s" % (fan or {}).get("status"))
    # The beacon was issued a couple of seconds after the fan, so it ends on the following sweep.
    beacon, bsecs = wait_status(c, dev_id, ids[2], "failed", SWEEP_INTERVAL + 15)
    check("so does the one that could never confirm",
          bsecs is not None or (beacon or {}).get("status") == "failed",
          "beacon=%s" % (beacon or {}).get("status"))


# --------------------------------------------------------------------------------------------
# D. A restart with a command in flight.
# --------------------------------------------------------------------------------------------

def bench_restart_in_flight(c, dev_id, wire, password):
    """An appliance in a plant room gets restarted. A command was in flight when it happened.

    Two things must be true afterwards, and they pull in opposite directions: the command must not
    be re-sent (the relay must not fire twice), and it must not be left hanging (an operator must
    still be told it was never confirmed). This drives both."""
    print("\n--- a restart with a command in flight -----------------------------------------")
    settle()
    wire.drain()
    r = issue(c, dev_id, "relay", 1)
    cmd = result(r) or {}
    cid = cmd.get("id")
    sent = wire.wait_for(timeout=6)
    check("a command is in flight when the app goes down",
          r.status_code == 200 and len(sent) == 1, "wire=%s" % sent)
    wire.close()

    sh("docker", "restart", NAME, check=False)
    if not wait_up(BASE + "/api/auth/config", timeout=180):
        check("the app comes back after a restart", False, logs(30))
        return None
    check("the app comes back after a restart", True)

    c2 = admin()
    # A fresh device session: the broker credential is minted once and cannot be read back, so a
    # reconnect after a restart mints a new one. The wire is back before the first sweep can run.
    newpass = rotate_password(c2, dev_id)
    wire2 = DeviceWire(DEVICE_KEY, newpass)
    wire2.subscribe(WIRE_FILTER)

    got, secs = wait_status(c2, dev_id, cid, "failed", FAIL_WINDOW)
    check("the command that survived the restart is ENDED, not left hanging",
          secs is not None, "status=%s" % (got or {}).get("status"))
    stray = [m for m in wire2.drain() if m[0].startswith("iot/cmd/")]
    check("and it is NOT re-sent after the restart", not stray, str(stray))
    return wire2


# --------------------------------------------------------------------------------------------
# E. A restart in the MIDDLE of a write.
# --------------------------------------------------------------------------------------------

def bench_pending_orphan(c, hole):
    """The app is killed while a Modbus write is stalled on a wedged device.

    The command was recorded BEFORE the write went out — deliberately, and correctly: an actuation
    that was sent but never written down is the worst possible ordering. So a row exists, in the
    state the write left it in. The question this asks is what becomes of that row, and there are
    only two acceptable answers: it is re-driven (which this app has ruled out, for good reasons),
    or it is ENDED so a human is told. Anything else is an actuation that nobody will ever look at
    again — the silent loss that "we never retry" is only safe without."""
    print("\n--- a restart in the middle of a write -----------------------------------------")
    pid = make_modbus_profile(c)
    mb_id = make_modbus_device(c, pid)

    # POSITIVE FIRST: a write to a wedged device fails on its own, says so, and dials exactly once.
    # Without this the "still pending" below could be a device that was never reached at all.
    dials_before = hole.dials
    r = issue(c, mb_id, "setpoint_reg", 42)
    cmd = result(r) or {}
    check("a write to a wedged device FAILS rather than hanging",
          r.status_code != 200 and "confirm" in err_text(r).lower(), err_text(r))
    time.sleep(1)
    rows = history(c, mb_id)
    failed = [x for x in rows if x.get("status") == "failed"]
    check("and it is recorded as failed, with the reason", bool(failed),
          "%d rows, statuses=%s" % (len(rows), [x.get("status") for x in rows]))
    check("the app dialled the wedged device exactly once (no retry inside the write)",
          hole.dials == dials_before + 1, "dials %d -> %d" % (dials_before, hole.dials))

    # Now kill the app WHILE the next write is stalled on that same wedged device.
    settle()
    dials_before = hole.dials
    started = {}

    def fire():
        try:
            started["r"] = issue(c, mb_id, "setpoint_reg", 43)
        except Exception as exc:  # the app is killed mid-request; that is the point
            started["exc"] = str(exc)

    t = threading.Thread(target=fire, daemon=True)
    t.start()
    time.sleep(1.2)  # inside the 3s per-operation Modbus timeout
    sh("docker", "kill", NAME, check=False)
    t.join(timeout=30)

    sh("docker", "start", NAME, check=False)
    if not wait_up(BASE + "/api/auth/config", timeout=180):
        check("the app comes back after being killed mid-write", False, logs(30))
        return
    check("the app comes back after being killed mid-write", True)
    c2 = admin()
    # From here on, any dial is one the RESTARTED app made — which is what "nothing re-drives an
    # interrupted actuation" has to be measured against.
    dials_at_boot = hole.dials

    rows = history(c2, mb_id)
    pend = [x for x in rows if x.get("status") == "pending"]
    check("the interrupted command was written down before the write went out",
          bool(pend), "statuses=%s" % [x.get("status") for x in rows])
    if not pend:
        return
    cid = pend[0].get("id")

    got, secs = wait_status(c2, mb_id, cid, "failed", FAIL_WINDOW)
    check("a command interrupted mid-write is ENDED, not left claiming to be in flight",
          secs is not None,
          "still %s after %ds" % ((got or {}).get("status"), FAIL_WINDOW))
    if secs is not None:
        check("and its reason is honest about not knowing whether the device acted",
              "may" in (got or {}).get("error", "").lower(), (got or {}).get("error", "")[:160])
        notes = [n for n in notifications(c2)
                 if "Bench wedged inverter" in (n.get("body") or "")
                 and ("never completed" in (n.get("body") or "").lower()
                      or "interrupted" in (n.get("body") or "").lower()
                      or "not confirm" in (n.get("body") or "").lower())]
        check("the operator is told about the interrupted command", bool(notes),
              "%d matching notifications" % len(notes))
    check("the interrupted write dialled the device exactly once, before the kill",
          hole.dials == dials_before + 1, "dials %d -> %d" % (dials_before, hole.dials))
    check("and NOTHING re-dialled the wedged device after the restart",
          hole.dials == dials_at_boot, "dials %d -> %d since boot" % (dials_at_boot, hole.dials))


def main():
    hole = Blackhole()
    print("blackhole listening on %s:%d" % (HOST, BLACKHOLE_PORT))
    c = admin()
    print("signed in as admin")
    global PROFILE_ID
    PROFILE_ID = make_profile(c)
    dev_id, password = make_mqtt_device(c, PROFILE_ID)
    print("device %s (id %d) provisioned" % (DEVICE_KEY, dev_id))

    wire = DeviceWire(DEVICE_KEY, password)
    wire.subscribe(WIRE_FILTER)
    print("the device is on the broker, listening on", WIRE_FILTER)

    try:
        bench_confirm_path(c, dev_id, wire)
        bench_unconfirmed_fails_and_stays(c, dev_id, wire)
        bench_cross_command_confirmation(c, dev_id, wire)
        wire2 = bench_restart_in_flight(c, dev_id, wire, password)
        if wire2:
            wire2.close()
        bench_pending_orphan(admin(), hole)
    finally:
        try:
            wire.close()
        except Exception:
            pass
        hole.close()

    print("\n================================================================================")
    print("%d/%d checks passed" % (len(PASSES), len(PASSES) + len(FAILS)))
    for f in FAILS:
        print("  FAILED:", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
