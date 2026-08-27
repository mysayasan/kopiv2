# Bench: a Modbus command that actually reaches the wire, and actually moves the plant.
#
# THE CLAIM UNDER TEST. myiotsan can command a POLLED device — a solar inverter, a battery — by
# writing a holding register, through the same guarded path an MQTT relay takes. That path ends in
# `modbus.WriteConfirm`, which writes once, reads the register back, and NEVER re-issues the write.
#
# WHY THIS NEEDS A LIVE RUN. `Issue` -> `sendModbus` -> `WriteConfirm` is confirmed by READING BACK
# THE REGISTER IT JUST WROTE. That is the right design and it is also self-certifying: a write that
# went to the wrong register, or the wrong unit, or was encoded with the wrong sign, confirms itself
# just as happily as a correct one — it reads back whatever it wrote, wherever it wrote it. So the
# bench brings its OWN Modbus client (`iotsan_harness.Modbus`), reads the simulator directly, and
# never asks the app whether the app was right.
#
# And a register is not the point either. A curtailment command that lands in the right register and
# changes nothing about what the inverter DOES is a confirmed command that did nothing. So every
# write here is checked three ways: the app's own status, the raw register on the wire, and the
# PLANT'S BEHAVIOUR afterwards — read back through the product's own telemetry.
#
# THE SIGN CONVENTION IS THE KNOWN FOOTGUN of this domain (see the solar memory: meter W is
# +import/-export, battery W is +charging). A battery rate of -40% is 65496 on the wire. A bench
# that compares the raw word against -40 calls a correct write a failure; a product that encodes it
# as u16 writes 0 or refuses. Both directions are checked.
#
#   python tools/fleetbench/iotsan_harness.py          # stand the app up
#   python tools/fleetbench/bench_iotsan_modbus.py     # starts the simulator itself
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from iotsan_harness import (
    SIM_ADDR,
    Modbus,
    ModbusError,
    admin,
    as_i16,
    build_sim,
    logs,
    result,
    result_list,
    start_sim,
    stop_sim,
    sunspec_models,
)

# The simulator is started at MIDDAY. At its own default (dawn) the inverter produces almost
# nothing, and every curtailment assertion would compare zero against zero and pass — the empty
# result trap, in a bench about physical plant. Realtime speed (not the simulator's compressed day)
# so the value read after a write is comparable to the value read before it.
SIM_TOD = 12
NAMEPLATE_W = 10000.0          # -pv: unit 1's nameplate
VENDOR_NAMEPLATE_W = 5000.0    # unit 3 is built at half the nameplate (buildVendor(3, pv*0.5))

# The inverter's SunSpec operating state, from tools/sunspec-sim/plant.go: 4 = MPPT (producing
# normally), 5 = THROTTLED (curtailed by a WMaxLimPct write). This is the DIRECT evidence that a
# curtailment took effect, and unlike AC power it does not move with how the plant is splitting
# production between the battery and the grid.
ST_THROTTLED = 5

# Point offsets WITHIN a SunSpec model, from tools/sunspec-sim/models.go. The model's own base is
# discovered by walking the chain — never arithmetic — and the model length is asserted before any
# write, so a change to the model shape fails the bench loudly instead of writing to whatever now
# lives at that address.
CTL_LEN = 24
STO_LEN = 24
OFF_WMAXLIMPCT = 3     # model 123: Conn_WinTms, Conn_RvrtTms, Conn, WMaxLimPct
OFF_WMAXLIM_ENA = 7    # ... WMaxLimPct_WinTms, _RvrtTms, _RmpTms, WMaxLim_Ena
OFF_OUTWRTE = 10       # model 124: ..., ChaSt, OutWRte (i16)
OFF_INWRTE = 11        # ... InWRte (i16)

# The vendor (non-SunSpec) block, from tools/sunspec-sim/devices.go.
V_PAC = 7          # u32, 0.1 W
V_POWER_LIMIT = 16  # u16, percent of nameplate; WRITABLE

PASSES = []
FAILS = []
STATE = {}


def check(name, ok, detail=""):
    (PASSES if ok else FAILS).append(name)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, ("  — " + detail) if detail else ""))
    return ok


def settle():
    """Wait out the per-device duty cycle (services.minCommandInterval, 2s)."""
    time.sleep(2.2)


def err_text(r):
    try:
        body = r.json()
    except ValueError:
        return (r.text or "")[:200]
    for k in ("message", "error", "msg"):
        if isinstance(body, dict) and body.get(k):
            return str(body[k])[:200]
    return json.dumps(body)[:200]


def issue(c, dev_id, name, value):
    return c.post("/api/devices/%d/commands" % dev_id, {"name": name, "value": value})


def history(c, dev_id, limit=50):
    return result_list(c.get("/api/devices/%d/commands/history?limit=%d" % (dev_id, limit)))


def latest(c, dev_id):
    """The device's most recent reading per key, as the PRODUCT decoded it.

    Asserting on the simulator alone would prove the simulator works. The point of a curtailment
    check is that the product SEES the plant respond, so the effect is read back through myiotsan's
    own telemetry wherever it can be."""
    # THE ENVELOPE TRAP, second variant. Most of this app answers {data:{result:{items:[...]}}},
    # but Latest returns a MAP keyed by telemetry key -> reading. Reaching for "items" here yields
    # {} and reads as "the app is not polling", which is exactly the wrong conclusion and cost a
    # run to see. Both shapes are handled.
    res = result(c.get("/api/devices/%d/latest" % dev_id)) or {}

    def value_of(row):
        # The numeric value is `num` on entities.DeviceReading, NOT `value`. Reading the wrong
        # field yields None, which `or 0` then turns into a confident 0 W — and every "the plant
        # responded" comparison becomes zero-against-zero. This bench only noticed because each
        # curtailment check requires the BEFORE value to be high before believing the after; a
        # check written as "power dropped" would have passed on None the whole way through.
        v = row.get("num")
        return row.get("value") if v is None else v

    out = {}
    if isinstance(res, dict) and "items" not in res:
        for key, row in res.items():
            if isinstance(row, dict):
                out[key] = value_of(row)
        return out
    for row in (res.get("items") if isinstance(res, dict) else res) or []:
        out[row.get("key")] = value_of(row)
    return out


def wait_for_reading(c, dev_id, key, timeout=30):
    """Wait until the poller has stored a reading for `key`."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        vals = latest(c, dev_id)
        if key in vals:
            return vals
        time.sleep(1)
    return latest(c, dev_id)


# --------------------------------------------------------------------------------------------
# Setup
# --------------------------------------------------------------------------------------------

def make_sunspec_profile(c, ctl_base, sto_base):
    """A SunSpec-mode profile whose COMMANDS bind absolute registers.

    Worth noting as a product fact: a SunSpec profile discovers its telemetry, but a command still
    has to name an absolute register, and a SunSpec model's address depends on the chain in front of
    it. So a command binding is authored against a particular device's layout even when the reads
    are self-describing."""
    body = {
        "slug": "bench-sunspec-ctl",
        "name": "Bench SunSpec inverter (controls)",
        "vendor": "kopiv2-bench",
        "transport": "modbus",
        "modbusMode": "sunspec",
        "modbusBase": 40000,
        "pollSeconds": 2,
        # A SunSpec profile discovers its DATAPOINTS, but it still has to declare the KEYS worth
        # storing: a sample whose key the profile does not declare is dropped by design
        # (Ingest.handleSamples). These are the role-prefixed names the decoder emits, the same
        # ones the shipped generic-sunspec-solar builtin declares. Deadbands are deliberately
        # tiny — this bench needs the stored value to follow the plant closely enough to see a
        # curtailment, where a production profile would rather not store every flicker.
        "keys": [
            {"key": "inv_ac_power", "label": "Inverter AC power", "unit": "W",
             "dataType": "number", "deadband": 5, "heartbeatSeconds": 5},
            {"key": "inv_dc_power", "label": "Inverter DC power", "unit": "W",
             "dataType": "number", "deadband": 5, "heartbeatSeconds": 5},
            {"key": "inv_operating_state", "label": "Inverter state", "dataType": "number",
             "deadband": 0, "heartbeatSeconds": 5},
            {"key": "batt_soc", "label": "Battery SoC", "unit": "%", "dataType": "number",
             "deadband": 0.5, "heartbeatSeconds": 10},
            {"key": "ctl_w_max_lim_pct", "label": "Export limit", "unit": "%",
             "dataType": "number", "deadband": 0, "heartbeatSeconds": 10},
        ],
        "commands": [
            # Export curtailment: the percent, and the enable that decides whether the percent
            # means anything. The shipped deye/sungrow profiles carry the same enable-then-value
            # shape ("solar sell must be on before the export limit matters").
            {"name": "curtail_pct", "label": "Export limit", "kind": "setpoint",
             "min": 0, "max": 100,
             "transport": "modbus", "register": ctl_base + OFF_WMAXLIMPCT,
             "regKind": "u16", "scaleFactor": 1},
            {"name": "curtail_enable", "label": "Enable export limit", "kind": "switch",
             "transport": "modbus", "register": ctl_base + OFF_WMAXLIM_ENA,
             "regKind": "u16", "scaleFactor": 1},
            # The signed pair. OutWRte is written whole; InWRte carries a 0.1 scale as well, so one
            # command exercises sign AND the reverse-scale together.
            {"name": "batt_out_rate", "label": "Discharge rate", "kind": "setpoint",
             "min": -100, "max": 100,
             "transport": "modbus", "register": sto_base + OFF_OUTWRTE,
             "regKind": "i16", "scaleFactor": 1},
            {"name": "batt_in_rate", "label": "Charge rate", "kind": "setpoint",
             "min": -100, "max": 100,
             "transport": "modbus", "register": sto_base + OFF_INWRTE,
             "regKind": "i16", "scaleFactor": 0.1},
            # A register that does not exist on this device: the bank is ~400 registers from the
            # base, so this is beyond it and the simulator answers with an exception.
            {"name": "nowhere", "label": "Unmapped register", "kind": "switch",
             "transport": "modbus", "register": 49000,
             "regKind": "u16", "scaleFactor": 1},
        ],
    }
    return save_profile(c, body)


def make_vendor_profile(c):
    """A regmap-mode profile: the NON-SunSpec path, where nothing is self-describing and every
    address and scale is authored by hand from the vendor's document."""
    body = {
        "slug": "bench-vendor-ctl",
        "name": "Bench vendor inverter (controls)",
        "vendor": "kopiv2-bench",
        "transport": "modbus",
        "modbusMode": "regmap",
        "modbusBase": 0,
        "pollSeconds": 2,
        "keys": [
            {"key": "pac", "label": "AC power", "unit": "W", "dataType": "number",
             "register": V_PAC, "regKind": "u32", "scaleFactor": 0.1},
            {"key": "soc", "label": "Battery SoC", "unit": "%", "dataType": "number",
             "register": 13, "regKind": "u16", "scaleFactor": 1},
        ],
        "commands": [
            {"name": "power_limit", "label": "Export limit", "kind": "setpoint",
             "min": 0, "max": 100,
             "transport": "modbus", "register": V_POWER_LIMIT,
             "regKind": "u16", "scaleFactor": 1},
        ],
    }
    return save_profile(c, body)


def save_profile(c, body):
    r = c.post("/api/profiles", body)
    if r.status_code != 200:
        existing = [p for p in result_list(c.get("/api/profiles")) if p.get("slug") == body["slug"]]
        if not existing:
            raise SystemExit("could not create profile %s: %s" % (body["slug"], err_text(r)))
        pid = existing[0].get("id")
        r = c.put("/api/profiles/%d" % pid, body)
        if r.status_code != 200:
            raise SystemExit("could not update profile %s: %s" % (body["slug"], err_text(r)))
        return pid
    res = result(r) or {}
    return (res.get("profile") or res).get("id")


def make_device(c, key, profile_id, unit, endpoint=SIM_ADDR, actuation=True):
    body = {"name": key, "deviceKey": key, "protocol": "modbus", "profileId": profile_id,
            "enabled": True, "actuationEnabled": actuation,
            "endpoint": endpoint, "unit": unit, "transport": "tcp"}
    r = c.post("/api/devices", body)
    if r.status_code == 200:
        res = result(r) or {}
        return ((res.get("device") or res)).get("id")
    existing = [d for d in result_list(c.get("/api/devices?limit=200")) if d.get("deviceKey") == key]
    if not existing:
        raise SystemExit("could not create device %s: %s" % (key, err_text(r)))
    dev_id = existing[0].get("id")
    set_device(c, dev_id, key, profile_id, unit, endpoint, actuation)
    return dev_id


def set_device(c, dev_id, key, profile_id, unit, endpoint=SIM_ADDR, actuation=True):
    r = c.put("/api/devices/%d" % dev_id, {
        "name": key, "profileId": profile_id, "enabled": True,
        "actuationEnabled": actuation, "endpoint": endpoint, "unit": unit, "transport": "tcp"})
    if r.status_code != 200:
        raise SystemExit("could not update device %s: %s" % (key, err_text(r)))


# --------------------------------------------------------------------------------------------
# The checks
# --------------------------------------------------------------------------------------------

def bench_wire(c, mb, dev_id, ctl_base, sto_base):
    print("\n--- does a command reach the register? -----------------------------------------")

    # PRECONDITION, and it is load-bearing: the app must really be polling this device. Every
    # "the plant responded" assertion below reads the product's telemetry, and a device that is
    # not polled has no telemetry to contradict anything.
    vals = wait_for_reading(c, dev_id, "inv_ac_power", timeout=40)
    if not check("the app polls the simulated inverter over Modbus", "inv_ac_power" in vals,
                 "keys: %s" % sorted(vals)[:8]):
        print(logs(30))
        raise SystemExit("nothing is being polled — every later assertion would be vacuous")
    STATE["nameplate_seen"] = vals.get("inv_ac_power")
    print("     inverter is producing %.0f W" % (vals.get("inv_ac_power") or 0))

    reg = ctl_base + OFF_WMAXLIMPCT
    before = mb.read_holding(1, reg)[0]
    settle()
    r = issue(c, dev_id, "curtail_pct", 60)
    cmd = result(r) or {}
    on_wire = mb.read_holding(1, reg)[0]
    check("a Modbus command reaches the real register on the real device",
          r.status_code == 200 and on_wire == 60,
          "http=%s reg%d %s->%s" % (r.status_code, reg, before, on_wire))
    check("and the command is CONFIRMED, not merely sent",
          cmd.get("status") == "confirmed",
          "status=%s error=%s" % (cmd.get("status"), cmd.get("error")))
    rows = history(c, dev_id)
    check("the confirmed write is in the device's command history, naming the actor",
          any(x.get("id") == cmd.get("id") and x.get("requestedByName") for x in rows),
          "%d rows" % len(rows))


def bench_physical_effect(c, mb, dev_id, ctl_base):
    """A register is not the point. The point is that the inverter DOES something."""
    print("\n--- is it a physical action, or just a stored number? --------------------------")

    ena = ctl_base + OFF_WMAXLIM_ENA
    pct = ctl_base + OFF_WMAXLIMPCT

    # Full output first, so the drop afterwards is measured against a known-high baseline rather
    # than against a cloud.
    settle()
    issue(c, dev_id, "curtail_enable", 0)
    settle()
    issue(c, dev_id, "curtail_pct", 100)
    time.sleep(4)
    full = latest(c, dev_id).get("inv_ac_power") or 0

    settle()
    r1 = issue(c, dev_id, "curtail_enable", 1)
    settle()
    r2 = issue(c, dev_id, "curtail_pct", 30)
    time.sleep(5)
    limited = latest(c, dev_id).get("inv_ac_power") or 0

    cap = NAMEPLATE_W * 0.30 * 0.97 + 50  # inverter efficiency, plus a little slack
    state = latest(c, dev_id).get("inv_operating_state")
    check("a curtailment command actually throttles the inverter",
          r1.status_code == 200 and r2.status_code == 200 and full > cap and limited <= cap,
          "%.0f W full -> %.0f W at 30%% (cap ~%.0f W)" % (full, limited, cap))
    # The inverter's own operating state is the direct signal, and it does not depend on how the
    # plant happens to be splitting production between the battery and the grid at that moment.
    check("and the inverter itself reports THROTTLED, through the product's own telemetry",
          state == ST_THROTTLED, "inv_operating_state=%s (want %d THROTTLED)" % (state, ST_THROTTLED))

    # THE ENABLE GATE. The percent register means nothing until the enable register is set — the
    # shipped deye/sungrow profiles say so in a comment. With the enable off, the write still
    # confirms, because confirming means "the register holds the value", which it does.
    settle()
    issue(c, dev_id, "curtail_enable", 0)
    settle()
    time.sleep(4)
    restored = latest(c, dev_id).get("inv_ac_power") or 0
    settle()
    r = issue(c, dev_id, "curtail_pct", 10)
    cmd = result(r) or {}
    time.sleep(5)
    after = latest(c, dev_id).get("inv_ac_power") or 0
    on_wire = mb.read_holding(1, pct)[0]
    state = latest(c, dev_id).get("inv_operating_state")
    inert = (cmd.get("status") == "confirmed" and on_wire == 10
             and after > cap and state != ST_THROTTLED)
    check("a confirmed command whose enable register is off changes nothing (documented, checked)",
          inert, "status=%s reg=%s power %.0f->%.0f W state=%s"
          % (cmd.get("status"), on_wire, restored, after, state))
    STATE["inert_confirm"] = inert

    # Leave the plant unthrottled for whatever runs next.
    settle()
    issue(c, dev_id, "curtail_pct", 100)


def bench_sign_and_scale(c, mb, dev_id, sto_base):
    print("\n--- the sign convention, and the scale, on the wire ----------------------------")

    out_reg = sto_base + OFF_OUTWRTE
    in_reg = sto_base + OFF_INWRTE

    settle()
    r = issue(c, dev_id, "batt_out_rate", -40)
    raw = mb.read_holding(1, out_reg)[0]
    check("a NEGATIVE setpoint is written as two's complement, not clamped or refused",
          r.status_code == 200 and raw == 0xFFD8 and as_i16(raw) == -40,
          "http=%s raw=%d (%d signed), want 65496 (-40)" % (r.status_code, raw, as_i16(raw)))

    settle()
    r = issue(c, dev_id, "batt_out_rate", 25)
    raw = mb.read_holding(1, out_reg)[0]
    check("a positive setpoint on the same register is written plainly",
          r.status_code == 200 and raw == 25, "raw=%d" % raw)

    # Sign AND the reverse scale together: -12.5 at a 0.1 scale is raw -125 = 65411.
    settle()
    r = issue(c, dev_id, "batt_in_rate", -12.5)
    raw = mb.read_holding(1, in_reg)[0]
    check("a scaled negative setpoint applies the scale in reverse before the sign",
          r.status_code == 200 and raw == 0xFF83 and as_i16(raw) == -125,
          "http=%s raw=%d (%d signed), want 65411 (-125)" % (r.status_code, raw, as_i16(raw)))


def bench_gates_on_the_modbus_path(c, mb, dev_id, ctl_base, profile_id):
    """The gates are proven on the MQTT path by bench_iotsan_actuation.py. What matters here is
    that the Modbus transport is not a second, looser path around them — and, crucially, that a
    refusal leaves the REGISTER UNTOUCHED, which only a read of the wire can show."""
    print("\n--- the gates, on the Modbus transport -----------------------------------------")

    reg = ctl_base + OFF_WMAXLIMPCT
    settle()
    issue(c, dev_id, "curtail_pct", 55)
    baseline = mb.read_holding(1, reg)[0]
    if not check("a write lands, so 'unchanged' below means something", baseline == 55,
                 "reg=%s" % baseline):
        return

    settle()
    r = issue(c, dev_id, "curtail_pct", 900)
    after = mb.read_holding(1, reg)[0]
    check("an out-of-bounds setpoint is refused server-side on the Modbus path",
          r.status_code != 200 and "range" in err_text(r).lower(), err_text(r))
    check("and NOTHING was written to the register", after == baseline,
          "reg %s -> %s" % (baseline, after))

    settle()
    set_device(c, dev_id, "bench-inverter", profile_id, 1, actuation=False)
    r = issue(c, dev_id, "curtail_pct", 20)
    after = mb.read_holding(1, reg)[0]
    check("a device with actuation switched off refuses the Modbus write",
          r.status_code != 200 and "actuation" in err_text(r).lower(), err_text(r))
    check("and NOTHING was written to the register", after == baseline,
          "reg %s -> %s" % (baseline, after))
    set_device(c, dev_id, "bench-inverter", profile_id, 1, actuation=True)


def bench_failure_is_honest(c, mb, dev_id, ctl_base, profile_id):
    print("\n--- when the write cannot land -------------------------------------------------")

    reg = ctl_base + OFF_WMAXLIMPCT
    baseline = mb.read_holding(1, reg)[0]

    # A register this device does not have. The simulator answers with a Modbus exception, which is
    # what a real device does when asked for an address outside its map.
    settle()
    before = len(history(c, dev_id))
    r = issue(c, dev_id, "nowhere", 1)
    rows = history(c, dev_id)
    newest = rows[0] if rows else {}
    check("a write to a register the device does not have FAILS",
          r.status_code != 200 and newest.get("status") == "failed",
          "http=%s recorded status=%s" % (r.status_code, newest.get("status")))
    check("and it says plainly that it was not confirmed",
          "confirm" in (err_text(r) + str(newest.get("error") or "")).lower(),
          str(newest.get("error"))[:160])
    check("and it is recorded exactly ONCE — nothing retried it",
          len(rows) == before + 1, "history %d -> %d" % (before, len(rows)))
    check("and no other register was touched", mb.read_holding(1, reg)[0] == baseline)

    # An endpoint nothing answers on: the device is unreachable rather than refusing.
    settle()
    set_device(c, dev_id, "bench-inverter", profile_id, 1, endpoint="127.0.0.1:1", actuation=True)
    before = len(history(c, dev_id))
    r = issue(c, dev_id, "curtail_pct", 45)
    rows = history(c, dev_id)
    # A REFUSED/FAILED command is not echoed in the response body — the handler answers with the
    # reason alone — so the recorded row is where its status has to be read.
    newest = rows[0] if rows else {}
    check("a write to an unreachable device FAILS rather than reporting success",
          r.status_code != 200 and newest.get("status") == "failed",
          "http=%s recorded status=%s error=%s"
          % (r.status_code, newest.get("status"), str(newest.get("error"))[:80]))
    check("an unreachable write is recorded exactly ONCE — never auto-retried",
          len(rows) == before + 1, "history %d -> %d" % (before, len(rows)))
    check("and the real device's register is untouched by a write aimed elsewhere",
          mb.read_holding(1, reg)[0] == baseline)
    set_device(c, dev_id, "bench-inverter", profile_id, 1, actuation=True)


def bench_keyless_sunspec_profile(c):
    """A SunSpec profile that declares no telemetry keys.

    This bench walked into it: the profile polled the simulator perfectly, the poller logged
    "polling ... every 2s", the device's Last seen kept advancing, no error appeared anywhere, and
    NOTHING was ever stored — because a sample whose key the profile does not declare is dropped,
    and a profile with no keys declares none of them. The only trace was `decoded: 0` buried in
    GET /api/devices/stats. An integrator sees a connected inverter with an empty chart.

    The register-map mode has always refused the equivalent profile out loud. This checks the
    SunSpec mode now does too."""
    print("\n--- a SunSpec profile with no declared keys ------------------------------------")

    pid = save_profile(c, {
        "slug": "bench-keyless", "name": "Bench keyless SunSpec", "vendor": "kopiv2-bench",
        "transport": "modbus", "modbusMode": "sunspec", "modbusBase": 40000, "pollSeconds": 2,
        "keys": [], "commands": [],
    })
    dev_id = make_device(c, "bench-keyless", pid, unit=2)

    # Give the poller a reconcile window to pick the device up (or refuse it).
    time.sleep(35)
    app_log = logs(400)
    refused = "bench-keyless" in app_log and "not pollable" in app_log
    stored = latest(c, dev_id)
    check("a SunSpec profile with no keys is refused, not silently polled into the void",
          refused, "logged refusal: %s; stored keys: %s" % (refused, sorted(stored)))
    check("and nothing was stored either way (the silence was never hiding data)",
          not stored, str(sorted(stored)))


def bench_vendor_regmap(c, mb):
    """Unit 3: the NON-SunSpec device. Nothing is self-describing — every address and scale is
    authored by hand — and it is a different configuration path through the poller."""
    print("\n--- the non-SunSpec vendor device (register map) -------------------------------")

    profile_id = make_vendor_profile(c)
    dev_id = make_device(c, "bench-vendor", profile_id, unit=3)

    vals = wait_for_reading(c, dev_id, "pac", timeout=40)
    if not check("the app polls the vendor device through a hand-authored register map",
                 "pac" in vals, "keys: %s" % sorted(vals)):
        print(logs(30))
        return

    full = vals.get("pac") or 0
    check("and it decodes the vendor's u32 + 0.1 scale correctly",
          full > 0 and abs(full - mb.read_holding(3, V_PAC, 2)[1] * 0.1) < 200,
          "app says %.0f W, wire says %.0f W"
          % (full, ((mb.read_holding(3, V_PAC, 2)[0] << 16) | mb.read_holding(3, V_PAC, 2)[1]) * 0.1))

    settle()
    r = issue(c, dev_id, "power_limit", 30)
    on_wire = mb.read_holding(3, V_POWER_LIMIT)[0]
    check("a command to a NON-SunSpec device reaches its vendor register",
          r.status_code == 200 and on_wire == 30,
          "http=%s reg%d=%s" % (r.status_code, V_POWER_LIMIT, on_wire))

    time.sleep(5)
    limited = latest(c, dev_id).get("pac") or 0
    cap = VENDOR_NAMEPLATE_W * 0.30 + 100
    check("and the vendor inverter actually throttles to it",
          full > cap and 0 < limited <= cap,
          "%.0f W -> %.0f W at 30%% (cap ~%.0f W)" % (full, limited, cap))

    settle()
    issue(c, dev_id, "power_limit", 100)
    time.sleep(5)
    restored = latest(c, dev_id).get("pac") or 0
    check("and lifting the limit restores production",
          restored > cap, "%.0f W" % restored)


def main():
    build_sim()
    sim = start_sim(speed=1, tick_ms=500, extra=["-tod", str(SIM_TOD)])
    try:
        mb = Modbus()
        try:
            models = sunspec_models(mb, 1)
        except ModbusError as e:
            raise SystemExit("the simulator is not presenting a SunSpec chain: %s" % e)
        print("unit 1 SunSpec chain:", {k: v[0] for k, v in sorted(models.items())})

        # Guard the offsets this bench binds commands to. If a model's shape changes, the bench
        # must fail here rather than write a curtailment into whatever now sits at that address.
        ctl_base, ctl_len = models[123]
        sto_base, sto_len = models[124]
        if not check("the control models are the shape this bench binds registers to",
                     ctl_len == CTL_LEN and sto_len == STO_LEN,
                     "123 len=%d (want %d), 124 len=%d (want %d)" % (ctl_len, CTL_LEN, sto_len, STO_LEN)):
            raise SystemExit("model layout changed — the register offsets would be wrong")

        c = admin()
        profile_id = make_sunspec_profile(c, ctl_base, sto_base)
        dev_id = make_device(c, "bench-inverter", profile_id, unit=1)
        print("device bench-inverter (id %d) -> %s unit 1" % (dev_id, SIM_ADDR))

        bench_wire(c, mb, dev_id, ctl_base, sto_base)
        bench_physical_effect(c, mb, dev_id, ctl_base)
        bench_sign_and_scale(c, mb, dev_id, sto_base)
        bench_gates_on_the_modbus_path(c, mb, dev_id, ctl_base, profile_id)
        bench_failure_is_honest(c, mb, dev_id, ctl_base, profile_id)
        bench_vendor_regmap(c, mb)
        bench_keyless_sunspec_profile(c)
    finally:
        stop_sim(sim)

    print("\n================================================================================")
    print("%d/%d checks passed" % (len(PASSES), len(PASSES) + len(FAILS)))
    for f in FAILS:
        print("  FAILED:", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
