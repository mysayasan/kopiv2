# Bench: the deadband gate and the telemetry store it exists to protect.
#
# THE CLAIM UNDER TEST. myiotsan stores sensor history in SQLite, and entities/telemetry_key.go
# says outright why that is allowed to work: "THE DEADBAND IS THE STORAGE DESIGN." A hundred
# devices times ten keys at 1 Hz is a thousand rows a second, which SQLite will not absorb — so
# services/deadband.go persists a sample only when it MOVES, and the claim is that this
# "loses nothing an operator would ask about later."
#
# That is a claim with two failure directions and they are not symmetric:
#
#   SUPPRESSES TOO MUCH -> real movement is gone forever. There is no second copy; the packet was
#      dropped on the way to the disk. A slow drift that never trips the deadband in one step,
#      a value the gate compared against the wrong baseline, a device whose gate state outlived
#      it — each of those is history that silently never existed.
#   SUPPRESSES TOO LITTLE -> the store floods, the write queue sheds, and the readings that DO
#      matter are the ones dropped, because shedding is indiscriminate.
#
# And one property sits underneath both, stated in ingest.go in a comment that calls getting it
# wrong "the worst possible bug in this app": the deadband is a STORAGE decision, not a DETECTION
# one. A value that sits three degrees over the limit without moving is suppressed from the table
# and must still fire the rule. If those two ever got wired together, a steady overheat would be
# a monitoring system quietly saying nothing.
#
# WHY A LIVE RUN. deadband_test.go already pins Admit() as a pure function, and it passes. What a
# unit test of Admit cannot see is everything AROUND it: whether an edited deadband reaches the
# hot path without a restart, whether a deleted device's baseline is dropped, whether liveness
# really bypasses the gate, and — the one this bench was written for — whether the rows the gate
# was careful to keep can actually be READ BACK. A store that admits the right samples and then
# cannot show them is the same outage as a store that dropped them.
#
# WHAT MAKES THE NEGATIVES REAL. Every "it was suppressed" here is measured as a row count that
# did NOT change while a control key's row count DID, on the same device, in the same publish.
# A check that passes on an empty result is not a check, and asserting "no new row" against an
# app that never stored anything is exactly that.
#
#   python tools/fleetbench/iotsan_harness.py        # stand it up
#   python tools/fleetbench/bench_iotsan_telemetry.py
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from iotsan_harness import (
    BASE,
    Client,
    DeviceWire,
    admin,
    db,
    logs,
    result,
    result_list,
)

DEVICE_KEY = "bench-tel-01"
TELEMETRY_TOPIC = "iot/tel/%s" % DEVICE_KEY

# How long to allow a published reading to travel broker -> ingest -> gate -> batcher -> disk.
# The harness configures batchSize 1 / flushMs 50, so this is ~40x the expected latency; it is
# generous on purpose, because a settle that is too short turns a stored row into a false
# "suppressed" and every suppression check in this file would pass for the wrong reason.
SETTLE = 2.0

PASSES = []
FAILS = []


def check(name, ok, detail=""):
    (PASSES if ok else FAILS).append(name)
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, ("  — " + detail) if detail else ""))
    return ok


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
# The profile: one key per gate behaviour, so no check can be explained by another key's rule
# --------------------------------------------------------------------------------------------
#
# Every key here is on ONE device on purpose. They share a payload, a topic, a connection and one
# pass through the ingest loop, so a difference between two of them is a difference between their
# GATE RULES and nothing else — not a difference in timing, transport or device state.

PROFILE = {
    "slug": "bench-telemetry",
    "name": "Bench telemetry gate",
    "vendor": "kopiv2-bench",
    "topicTemplate": TELEMETRY_TOPIC,
    "payloadFormat": "json",
    "keys": [
        # The classic analogue sensor: a deadband wide enough that sensor noise is not a row.
        {"key": "temp", "label": "Temperature", "unit": "C", "dataType": "number",
         "jsonPath": "temp", "deadband": 0.5, "min": -40, "max": 90},
        # The control key. Deadband 0, so EVERY distinct value it carries is stored. Its row
        # count is what proves a publish arrived at all, which is what makes "temp did not move"
        # mean "the gate suppressed it" rather than "nothing got through".
        {"key": "witness", "label": "Witness", "dataType": "number",
         "jsonPath": "witness", "deadband": 0},
        # A door contact: deadband 0 is correct here, every transition is the event.
        {"key": "door", "label": "Door", "dataType": "bool", "jsonPath": "door", "deadband": 0},
        # A flat line that must still prove itself alive. Deadband huge so ONLY the heartbeat can
        # admit it — otherwise a pass would not distinguish the two paths through the gate.
        {"key": "beat", "label": "Heartbeat", "dataType": "number",
         "jsonPath": "beat", "deadband": 1000, "heartbeatSeconds": 3},
        # A string key: it changes or it does not, there is no "how much".
        {"key": "mode", "label": "Mode", "dataType": "string", "jsonPath": "mode"},
        # The pair for the read-back half: one key that talks constantly and one that does not.
        {"key": "chatty", "label": "Chatty", "dataType": "number",
         "jsonPath": "chatty", "deadband": 0},
        {"key": "quiet", "label": "Quiet", "dataType": "number",
         "jsonPath": "quiet", "deadband": 0},
    ],
    "commands": [],
}


def make_profile(c):
    r = c.post("/api/profiles", PROFILE)
    if r.status_code == 200:
        res = result(r) or {}
        return (res.get("profile") or res).get("id")
    existing = [p for p in result_list(c.get("/api/profiles")) if p.get("slug") == PROFILE["slug"]]
    if not existing:
        raise SystemExit("could not create the bench profile: %s" % err_text(r))
    pid = existing[0].get("id")
    if c.put("/api/profiles/%d" % pid, PROFILE).status_code != 200:
        raise SystemExit("could not update the bench profile")
    return pid


def make_device(c, profile_id, key=DEVICE_KEY, name="Bench telemetry device"):
    r = c.post("/api/devices", {
        "name": name, "deviceKey": key, "protocol": "mqtt",
        "profileId": profile_id, "enabled": True, "actuationEnabled": False,
    })
    if r.status_code == 200:
        res = result(r) or {}
        dev = res.get("device") or res
        return dev.get("id"), res.get("password") or dev.get("password")
    existing = [d for d in result_list(c.get("/api/devices?limit=500")) if d.get("deviceKey") == key]
    if not existing:
        raise SystemExit("could not create device %s: %s" % (key, err_text(r)))
    dev_id = existing[0].get("id")
    pw = (result(c.post("/api/devices/%d/password" % dev_id)) or {}).get("password")
    return dev_id, pw


# --------------------------------------------------------------------------------------------
# Reading the store back
# --------------------------------------------------------------------------------------------

WIDE_FROM = 0
WIDE_TO = 4000000000  # well past any bench clock; "everything this device ever stored"


def series(c, dev_id, key, frm=WIDE_FROM, to=WIDE_TO):
    """One chart's worth of points AND what they are.

    /readings answers {items, span, truncated} — the envelope trap plus one more layer: reaching
    for a bare list here loses the span, and the span is the difference between "these are the
    sensor's own samples" and "these are hourly summaries"."""
    r = c.get("/api/devices/%d/readings?key=%s&from=%d&to=%d" % (dev_id, key, frm, to))
    if r.status_code != 200:
        raise SystemExit("readings for %s failed: %s" % (key, err_text(r)))
    res = result(r) or {}
    return res.get("items") or [], res.get("span"), bool(res.get("truncated"))


def rows(c, dev_id, key, frm=WIDE_FROM, to=WIDE_TO):
    """Just the points, for the many checks that only count them."""
    return series(c, dev_id, key, frm, to)[0]


def count(c, dev_id, key):
    return len(rows(c, dev_id, key))


def last_num(c, dev_id, key):
    """The newest stored value for a key.

    A reading's numeric value is `num`, NOT `value` — the wrong field yields None, and `or 0`
    then turns every comparison into a confident zero-vs-zero. It cost #213 a run."""
    got = rows(c, dev_id, key)
    return got[-1].get("num") if got else None


def latest_map(c, dev_id):
    """/latest answers a MAP keyed by telemetry key, NOT {items:[...]}."""
    r = c.get("/api/devices/%d/latest" % dev_id)
    return result(r) or {}


def stats(c):
    return result(c.get("/api/devices/stats")) or {}


def publish(wire, **fields):
    wire.publish(TELEMETRY_TOPIC, json.dumps(fields))


def publish_settle(wire, **fields):
    publish(wire, **fields)
    time.sleep(SETTLE)


# --------------------------------------------------------------------------------------------
# Part 1 — the gate admits what moved and refuses what did not
# --------------------------------------------------------------------------------------------

def part_gate(c, dev_id, wire):
    print("\n--- the gate: what gets a row ---")

    # POSITIVE FIRST. The first sample of a series has nothing to compare against, and the first
    # value is itself a fact. If this does not land, nothing below means anything.
    publish_settle(wire, temp=20.0, witness=1)
    check("a key's FIRST sample is stored",
          count(c, dev_id, "temp") == 1 and last_num(c, dev_id, "temp") == 20.0,
          "temp rows=%d last=%s" % (count(c, dev_id, "temp"), last_num(c, dev_id, "temp")))

    # A move smaller than the deadband. The witness key moves in the SAME payload, so its row
    # proves the publish was delivered and decoded — without it, "temp gained no row" is equally
    # explained by the message never arriving.
    before_t, before_w = count(c, dev_id, "temp"), count(c, dev_id, "witness")
    publish_settle(wire, temp=20.2, witness=2)
    after_t, after_w = count(c, dev_id, "temp"), count(c, dev_id, "witness")
    check("a sub-deadband move is NOT stored (proven by a witness key that WAS)",
          after_t == before_t and after_w == before_w + 1,
          "temp %d->%d, witness %d->%d" % (before_t, after_t, before_w, after_w))

    check("and the stored value is still the pre-move one",
          last_num(c, dev_id, "temp") == 20.0, "last temp=%s" % last_num(c, dev_id, "temp"))

    # The boundary. Admit uses >=, so a move of EXACTLY the deadband must land. An off-by-one
    # here is the difference between a deadband of 0.5 and one of 0.5-epsilon, which nobody
    # would ever notice and which shifts every stored series.
    before = count(c, dev_id, "temp")
    publish_settle(wire, temp=20.5, witness=3)
    check("a move of EXACTLY the deadband is stored (>= not >)",
          count(c, dev_id, "temp") == before + 1 and last_num(c, dev_id, "temp") == 20.5,
          "temp rows %d -> %d" % (before, count(c, dev_id, "temp")))

    before = count(c, dev_id, "temp")
    publish_settle(wire, temp=25.0, witness=4)
    check("a move larger than the deadband is stored",
          count(c, dev_id, "temp") == before + 1, "temp rows %d -> %d" % (before, count(c, dev_id, "temp")))

    # THE ONE THAT MATTERS MOST IN THIS PART. The baseline must be the last STORED value, not the
    # last SEEN one. If a suppressed sample moved the baseline, a slow drift — a freezer warming
    # by 0.2 degrees an hour — would step under the deadband forever and never be recorded at
    # all. The whole series would be a flat line through a real excursion.
    before = count(c, dev_id, "temp")
    ramp = [25.2, 25.4, 25.6]   # each step 0.2 < 0.5, cumulative 0.6 > 0.5
    for i, v in enumerate(ramp):
        publish(wire, temp=v, witness=10 + i)
        time.sleep(0.4)
    time.sleep(SETTLE)
    after = count(c, dev_id, "temp")
    check("a slow drift of sub-deadband steps IS eventually recorded (baseline does not creep)",
          after > before, "temp rows %d -> %d across steps of 0.2 with a 0.5 deadband" % (before, after))
    check("and it was recorded at the step that crossed, not at every step",
          after - before == 1, "%d new rows for 3 sub-deadband steps" % (after - before))

    # Deadband 0 means "store every distinct value" — correct for a contact, where the transition
    # IS the event — but it still suppresses an identical repeat, which a device republishing its
    # state every second will send all day.
    publish_settle(wire, door=False, witness=20)
    before = count(c, dev_id, "door")
    publish_settle(wire, door=True, witness=21)
    check("a deadband-0 key stores every transition",
          count(c, dev_id, "door") == before + 1, "door rows %d -> %d" % (before, count(c, dev_id, "door")))

    before_d, before_w = count(c, dev_id, "door"), count(c, dev_id, "witness")
    publish_settle(wire, door=True, witness=22)
    check("a deadband-0 key still suppresses an identical repeat",
          count(c, dev_id, "door") == before_d and count(c, dev_id, "witness") == before_w + 1,
          "door %d->%d, witness %d->%d" % (before_d, count(c, dev_id, "door"),
                                           before_w, count(c, dev_id, "witness")))

    # A string key changes or it does not.
    publish_settle(wire, mode="heat", witness=30)
    before = count(c, dev_id, "mode")
    publish_settle(wire, mode="cool", witness=31)
    check("a string key stores a change",
          count(c, dev_id, "mode") == before + 1, "mode rows %d -> %d" % (before, count(c, dev_id, "mode")))
    before = count(c, dev_id, "mode")
    publish_settle(wire, mode="cool", witness=32)
    check("a string key suppresses an identical repeat",
          count(c, dev_id, "mode") == before, "mode rows %d -> %d" % (before, count(c, dev_id, "mode")))


def part_heartbeat(c, dev_id, wire):
    print("\n--- the heartbeat: a flat line still proves itself alive ---")

    # `beat` has a deadband of 1000, so nothing it reports can move enough to be admitted on
    # value. Only the heartbeat can put a row in the table, which is what makes this a test of
    # the heartbeat path and not of the deadband path.
    publish_settle(wire, beat=50.0, witness=40)
    first = count(c, dev_id, "beat")
    check("the first heartbeat sample lands as first-seen", first == 1, "beat rows=%d" % first)

    # Immediately after: the heartbeat has not elapsed, the value has not moved, no row.
    before_b, before_w = count(c, dev_id, "beat"), count(c, dev_id, "witness")
    publish_settle(wire, beat=50.0, witness=41)
    check("an unchanged value BEFORE the heartbeat elapses is not stored",
          count(c, dev_id, "beat") == before_b and count(c, dev_id, "witness") == before_w + 1,
          "beat %d->%d, witness %d->%d" % (before_b, count(c, dev_id, "beat"),
                                           before_w, count(c, dev_id, "witness")))

    # Now wait past the 3s heartbeat and republish the SAME value.
    time.sleep(3.5)
    before = count(c, dev_id, "beat")
    publish_settle(wire, beat=50.0, witness=42)
    check("an unchanged value AFTER the heartbeat elapses IS stored",
          count(c, dev_id, "beat") == before + 1,
          "beat rows %d -> %d after a %ds heartbeat" % (before, count(c, dev_id, "beat"), 3))


# --------------------------------------------------------------------------------------------
# Part 2 — a deadband is a STORAGE decision, not a DETECTION one
# --------------------------------------------------------------------------------------------

def part_suppressed_still_alerts(c, dev_id, wire):
    print("\n--- the load-bearing claim: a suppressed sample still reaches the rules ---")

    # ingest.go calls wiring these together "the worst possible bug in this app", and it is right:
    # a room that goes to 80 degrees and STAYS there produces one row and then nothing, because
    # nothing is moving. If the rule only saw admitted samples, the alert would depend on the
    # temperature continuing to change — the one thing a genuine sustained fault does not do.

    r = c.post("/api/rules", {
        "name": "Bench overheat", "enabled": True, "deviceId": dev_id, "key": "temp",
        "condition": "above", "threshold": 70.0, "consecutiveSamples": 3,
        "cooldownSeconds": 1, "severity": "critical", "schedulePolicy": "always",
    })
    if r.status_code != 200:
        raise SystemExit("could not create the overheat rule: %s" % err_text(r))
    rule_id = (result(r) or {}).get("id")
    time.sleep(1.0)

    def alerts():
        return [a for a in result_list(c.get("/api/alerts?limit=200"))
                if a.get("ruleId") == rule_id]

    before_alerts = len(alerts())

    # Drive it to a steady overheat: one move that IS stored, then repeats of the identical value
    # that the gate must suppress. `consecutiveSamples: 3` means the rule needs three samples over
    # the line — and only the first of the three can possibly be a stored row.
    publish_settle(wire, temp=80.0, witness=50)
    stored_after_first = count(c, dev_id, "temp")

    for i in range(4):
        publish(wire, temp=80.0, witness=51 + i)
        time.sleep(0.5)
    time.sleep(SETTLE + 2.0)

    stored_after_repeats = count(c, dev_id, "temp")
    check("the steady overheat WAS suppressed from the table (identical repeats stored nothing)",
          stored_after_repeats == stored_after_first,
          "temp rows %d -> %d across 4 identical 80.0 publishes"
          % (stored_after_first, stored_after_repeats))

    fired = alerts()
    check("...and the rule fired anyway — the deadband gates STORAGE, not DETECTION",
          len(fired) > before_alerts,
          "alerts for this rule %d -> %d" % (before_alerts, len(fired)))

    c.delete("/api/rules/%d" % rule_id)
    return rule_id


# --------------------------------------------------------------------------------------------
# Part 3 — an edited deadband takes effect now, not at the next restart
# --------------------------------------------------------------------------------------------

def part_profile_edit(c, dev_id, profile_id, wire):
    print("\n--- tuning: an edited deadband must reach the hot path without a restart ---")

    # Tuning a deadband is the main thing an integrator DOES with this product — telemetry_key.go
    # calls it "real work" worth exporting between sites. If the change only took effect at the
    # next restart, the operator would widen a deadband, watch the flood continue, and widen it
    # again.
    edited = json.loads(json.dumps(PROFILE))
    for k in edited["keys"]:
        if k["key"] == "temp":
            k["deadband"] = 5.0
    if c.put("/api/profiles/%d" % profile_id, edited).status_code != 200:
        raise SystemExit("could not widen the temp deadband")
    time.sleep(1.0)

    # Establish a fresh baseline under the NEW rule, then move by 2.0 — which the old 0.5
    # deadband would have admitted and the new 5.0 must not.
    publish_settle(wire, temp=10.0, witness=60)
    before_t, before_w = count(c, dev_id, "temp"), count(c, dev_id, "witness")
    publish_settle(wire, temp=12.0, witness=61)
    check("a WIDENED deadband takes effect on the next message (no restart)",
          count(c, dev_id, "temp") == before_t and count(c, dev_id, "witness") == before_w + 1,
          "a 2.0 move under a fresh 5.0 deadband: temp %d->%d, witness %d->%d"
          % (before_t, count(c, dev_id, "temp"), before_w, count(c, dev_id, "witness")))

    # And back the other way: narrowing must admit what the wide one refused.
    for k in edited["keys"]:
        if k["key"] == "temp":
            k["deadband"] = 0.5
    if c.put("/api/profiles/%d" % profile_id, edited).status_code != 200:
        raise SystemExit("could not narrow the temp deadband back")
    time.sleep(1.0)
    before = count(c, dev_id, "temp")
    publish_settle(wire, temp=14.0, witness=62)
    check("a NARROWED deadband likewise takes effect immediately",
          count(c, dev_id, "temp") == before + 1,
          "temp rows %d -> %d" % (before, count(c, dev_id, "temp")))


# --------------------------------------------------------------------------------------------
# Part 4 — what the ingest path refuses to fabricate
# --------------------------------------------------------------------------------------------

def part_ingest(c, dev_id, wire):
    print("\n--- ingest: what must never become a reading ---")

    before_w = count(c, dev_id, "witness")

    # Zigbee2MQTT really does send "unavailable" for a numeric point. A 0 is a reading, and 0
    # degrees is not the same as "no reading" — codec.coerce must yield no sample at all.
    before = count(c, dev_id, "temp")
    wire.publish(TELEMETRY_TOPIC, json.dumps({"temp": "unavailable", "witness": 70}))
    time.sleep(SETTLE)
    got = rows(c, dev_id, "temp")
    check('"unavailable" on a numeric key stores NOTHING, not a fabricated 0',
          len(got) == before and not any(g.get("num") == 0 for g in got[before:]),
          "temp rows %d -> %d" % (before, len(got)))

    # A field the profile does not declare is not telemetry. Storing it would let a device invent
    # its own schema, and the profile is the only thing that says what a device may report.
    before_all = sum(count(c, dev_id, k["key"]) for k in PROFILE["keys"])
    wire.publish(TELEMETRY_TOPIC, json.dumps({"undeclared_key": 999, "witness": 71}))
    time.sleep(SETTLE)
    after_all = sum(count(c, dev_id, k["key"]) for k in PROFILE["keys"])
    check("an UNDECLARED field in the payload is dropped",
          after_all == before_all + 1, "total rows %d -> %d (only the witness)" % (before_all, after_all))

    # A malformed payload is a device problem worth logging, not a pipeline that stops.
    wire.publish(TELEMETRY_TOPIC, "this is not json")
    time.sleep(SETTLE)
    before = count(c, dev_id, "witness")
    publish_settle(wire, witness=72)
    check("a malformed payload does not stop the pipeline (the next good one is stored)",
          count(c, dev_id, "witness") == before + 1,
          "witness rows %d -> %d after a garbage payload" % (before, count(c, dev_id, "witness")))

    # A sensor reporting -3000 degrees is broken, not cold. The evidence is STORED and flagged,
    # because dropping it would hide the failing device.
    before = count(c, dev_id, "temp")
    publish_settle(wire, temp=-3000.0, witness=73)
    got = rows(c, dev_id, "temp")
    check("an out-of-range reading is STORED (dropping it would hide a failing sensor)",
          len(got) == before + 1, "temp rows %d -> %d" % (before, len(got)))
    check("...and flagged suspect",
          bool(got and got[-1].get("suspect")),
          "last temp row suspect=%s num=%s" % (got[-1].get("suspect") if got else None,
                                               got[-1].get("num") if got else None))

    check("the witness key kept moving throughout (the whole part was live)",
          count(c, dev_id, "witness") > before_w,
          "witness %d -> %d" % (before_w, count(c, dev_id, "witness")))


def part_liveness(c, dev_id, wire):
    print("\n--- liveness rides ABOVE the gate ---")

    # ingest.go puts TouchSeen first and unconditionally, and says why: behind the deadband, a
    # perfectly healthy stable sensor would look dead to the offline rule. This is the check that
    # a flat-lining-but-alive device is not reported offline.
    def seen_at():
        d = result(c.get("/api/devices/%d" % dev_id)) or {}
        dev = d.get("device") or d
        return dev.get("lastSeenAt") or dev.get("lastSeen") or 0

    publish_settle(wire, door=True, witness=80)   # move the baseline somewhere known
    before = seen_at()

    # DeviceService.lastSeenWriteInterval throttles the liveness WRITE to once every 30 seconds,
    # so the whole check has to be conducted outside that window. Costing a run to learn: a
    # shorter wait reports a healthy app as broken, and this is exactly the class of check that
    # fails for a reason that has nothing to do with what it is testing.
    print("    waiting out the 30s liveness write throttle...")
    time.sleep(32.0)

    # Now publish only values the gate MUST suppress: the same door state, repeated.
    stored_before = count(c, dev_id, "door")
    for _ in range(3):
        wire.publish(TELEMETRY_TOPIC, json.dumps({"door": True}))
        time.sleep(0.6)
    time.sleep(SETTLE)

    check("the repeats really were suppressed (no new rows for the whole burst)",
          count(c, dev_id, "door") == stored_before,
          "door rows %d -> %d" % (stored_before, count(c, dev_id, "door")))
    after = seen_at()
    check("...and the device's liveness advanced anyway — a flat sensor is not a dead one",
          after > before, "lastSeen %s -> %s" % (before, after))


# --------------------------------------------------------------------------------------------
# Part 5 — the gate's own memory across a device's life
# --------------------------------------------------------------------------------------------

def part_gate_memory(c, profile_id):
    print("\n--- the gate's memory: what happens to a series when its device goes away ---")

    # deadband.go ships Forget() with a comment stating exactly why it exists: "so a deleted or
    # re-provisioned device does not leave its last values behind to be compared against by a
    # future device with the same id." SQLite hands out `INTEGER PRIMARY KEY` rowids as max+1, so
    # deleting the newest device and creating another really does reuse the id — this walks that
    # path and asks whether the NEW device's first reading survives.

    before_series = stats(c).get("series")

    key = "bench-recycle-01"
    dev_id, pw = make_device(c, profile_id, key=key, name="Bench recycle A")
    topic = "iot/tel/%s" % key
    w = DeviceWire(key, pw)
    w.publish(topic, json.dumps({"temp": 60.0}))
    time.sleep(SETTLE)
    seeded = count(c, dev_id, "temp")
    check("the first device stored its baseline before deletion", seeded >= 1,
          "temp rows=%d at id %s" % (seeded, dev_id))
    w.close()

    c.delete("/api/devices/%d" % dev_id)
    time.sleep(1.0)

    # A NEW physical device, same profile. If it lands on the same id, its first reading is being
    # compared against the DELETED device's last value.
    key2 = "bench-recycle-02"
    dev_id2, pw2 = make_device(c, profile_id, key=key2, name="Bench recycle B")
    topic2 = "iot/tel/%s" % key2
    w2 = DeviceWire(key2, pw2)

    reused = (dev_id2 == dev_id)
    print("    the replacement device took id %s (the deleted one was %s) — id reused: %s"
          % (dev_id2, dev_id, reused))

    # 60.2 is within the old device's 0.5 deadband of 60.0. For a genuinely new series it is a
    # first-seen sample and MUST be stored; only a stale baseline can suppress it.
    w2.publish(topic2, json.dumps({"temp": 60.2}))
    time.sleep(SETTLE)
    got = count(c, dev_id2, "temp")
    if reused:
        check("a REPLACEMENT device's first reading is stored (the old baseline was forgotten)",
              got >= 1, "temp rows=%d on the recycled id %s" % (got, dev_id2))
    else:
        check("a replacement device on a FRESH id stores its first reading", got >= 1,
              "temp rows=%d at id %s" % (got, dev_id2))
    w2.close()

    # And the leak: the gate map is the one structure ingest.go calls unbounded, and Size() is
    # exposed to the metrics endpoint precisely so it can be watched. After deleting a device,
    # its series must stop being tracked.
    c.delete("/api/devices/%d" % dev_id2)
    time.sleep(1.5)
    after_series = stats(c).get("series")
    check("a deleted device's series stop being tracked by the gate",
          after_series is not None and before_series is not None and after_series <= before_series,
          "gate series %s -> %s across two devices created and deleted" % (before_series, after_series))


# --------------------------------------------------------------------------------------------
# Part 6 — reading it back: the half the gate was careful FOR
# --------------------------------------------------------------------------------------------

CHATTY_BURST = 520      # > the 500-row tail Latest folds
SERIES_BURST = 2100     # > the 2000-point cap /readings applies


def burst(wire, field, n, other=None):
    """Publish n distinct values of one key as fast as the broker will take them."""
    for i in range(n):
        payload = {field: float(i)}
        if other:
            payload.update(other)
        wire.publish(TELEMETRY_TOPIC, json.dumps(payload))
    # Let the queue drain. batchSize 1 means one transaction per row, so this is not instant.
    time.sleep(max(8.0, n * 0.02))


def part_readback(c, dev_id, wire):
    print("\n--- reading it back: /latest and /readings ---")

    # THE DEVICE PAGE. Latest is "the current value of every key" — the top of the device page and
    # the only query on it that has to be fast. It folds a fixed tail of the device's newest rows
    # and takes the first row it meets per key. A key that has not moved recently is therefore
    # only visible if it is still INSIDE that tail.
    publish_settle(wire, quiet=42.0, chatty=0.0)
    got = latest_map(c, dev_id)
    check("a quiet key IS on the device page before anything else happens",
          "quiet" in got and (got.get("quiet") or {}).get("num") == 42.0,
          "latest quiet=%s" % json.dumps(got.get("quiet"))[:80])

    print("    publishing %d rows on a chatty key..." % CHATTY_BURST)
    burst(wire, "chatty", CHATTY_BURST)
    stored_chatty = count(c, dev_id, "chatty")
    check("the chatty burst really was stored (this check has something to crowd with)",
          stored_chatty >= CHATTY_BURST * 0.9,
          "%d of %d chatty rows stored" % (stored_chatty, CHATTY_BURST))

    got = latest_map(c, dev_id)
    check("a quiet key is STILL on the device page after a chatty key filled the tail",
          "quiet" in got, "keys on the page: %s" % ",".join(sorted(got.keys())))

    # THE CHART. /readings caps at 2000 points and sorts oldest-first. The question is what
    # happens over the cap — the UI offers a 7-day range, and a 7-day window on a busy key is
    # comfortably more than 2000 rows.
    print("    publishing %d rows on one key for the chart window..." % SERIES_BURST)
    burst(wire, "chatty", SERIES_BURST)
    total = count(c, dev_id, "chatty")
    print("    /readings returned %d rows for chatty" % total)

    newest_stored = None
    m = latest_map(c, dev_id)
    if "chatty" in m:
        newest_stored = (m["chatty"] or {}).get("ts")

    points, span, truncated = series(c, dev_id, "chatty")
    print("    the chart got %d points, span=%s, truncated=%s" % (len(points), span, truncated))
    newest_in_series = points[-1].get("ts") if points else None
    check("a chart window WIDER than the cap still reaches the present",
          newest_stored is not None and newest_in_series is not None
          and newest_in_series >= newest_stored,
          "newest row on the device page ts=%s, newest point the chart got ts=%s"
          % (newest_stored, newest_in_series))

    # A chart that quietly swaps raw samples for hourly summaries is a chart that lies. Whatever
    # it served, it has to name it.
    check("the chart is TOLD what resolution it received",
          span in ("raw", "1m", "1h"), "span=%s truncated=%s" % (span, truncated))
    check("...and over the cap it says the window was reduced somehow",
          span in ("1m", "1h") or (span == "raw" and truncated),
          "%d raw rows in the store, %d points returned, span=%s truncated=%s"
          % (total, len(points), span, truncated))

    oldest_in_series = points[0].get("ts") if points else None
    print("    window covered: %s .. %s" % (oldest_in_series, newest_in_series))


# --------------------------------------------------------------------------------------------
# Part 7 — the rollup worker, watched for the first time
# --------------------------------------------------------------------------------------------

def part_rollup(c, dev_id):
    print("\n--- the rollup: the background job nobody had ever watched run ---")

    # The shipped rollup interval is an hour and was not settable, so on every bench this app has
    # ever had, the rollup worker never ran once. The harness now sets it to five seconds. What
    # follows checks its ARITHMETIC against the raw table directly, because no API exposes a
    # bucket — asking the app to summarize its own summaries would prove nothing.

    deadline = time.time() + 120
    buckets = []
    while time.time() < deadline:
        con = db()
        try:
            buckets = [dict(r) for r in con.execute(
                "select * from reading_rollup where device_id=? and span='1m' order by bucket",
                (dev_id,))]
        finally:
            con.close()
        if buckets:
            break
        time.sleep(5)

    if not check("the rollup worker produced buckets at all", bool(buckets),
                 "%d 1m buckets for device %s" % (len(buckets), dev_id)):
        print(logs(30))
        return

    # THE ARITHMETIC. A bucket claims a count, a min and a max over one minute of one key. Read
    # the raw rows of that same minute out of the same file and compare. This is what catches a
    # bucket summarized from HALF its minute — invisible from the outside, because an
    # undercounted bucket looks exactly like a quiet minute.
    con = db()
    try:
        wrong = []
        for b in buckets:
            lo, hi = b["bucket"] * 1000, (b["bucket"] + 60) * 1000
            nums = [r["num"] for r in con.execute(
                "select num from device_reading where device_id=? and key=? and ts>=? and ts<?",
                (b["device_id"], b["key"], lo, hi))]
            if not nums:
                continue  # the raw rows were purged; nothing to compare against
            if (b["count"] != len(nums) or abs(b["min"] - min(nums)) > 1e-9
                    or abs(b["max"] - max(nums)) > 1e-9):
                wrong.append({"key": b["key"], "bucket": b["bucket"],
                              "claimed": b["count"], "actual": len(nums),
                              "min": (b["min"], min(nums)), "max": (b["max"], max(nums))})
    finally:
        con.close()

    check("every bucket summarizes its WHOLE minute (count/min/max match the raw rows)",
          not wrong,
          ("%d of %d buckets disagree with the raw table; first: %s"
           % (len(wrong), len(buckets), json.dumps(wrong[0]))) if wrong
          else "%d buckets checked against the raw table" % len(buckets))

    # A bucket must never be written twice: the cursor is supposed to move past what it folded,
    # and a duplicate would double every count a chart draws from it.
    con = db()
    try:
        dupes = con.execute(
            "select device_id,key,span,bucket,count(*) n from reading_rollup "
            "group by 1,2,3,4 having n>1").fetchall()
    finally:
        con.close()
    check("no bucket was written twice", not dupes,
          "%d duplicated (device,key,span,bucket) groups" % len(dupes))

    # And the still-filling bucket must be left alone: rolling up the current minute would write
    # a summary the cursor then steps past, so the rest of that minute is folded by nothing.
    current = int(time.time()) // 60 * 60
    con = db()
    try:
        premature = con.execute(
            "select count(*) from reading_rollup where span='1m' and bucket>=?",
            (current,)).fetchone()[0]
    finally:
        con.close()
    check("the current, still-filling bucket has NOT been rolled up",
          premature == 0, "%d buckets at or after %d" % (premature, current))


def part_rollup_fallback(c, dev_id, wire):
    print("\n--- and what the rollups are FOR: a chart wider than the cap ---")

    # This is the promise Series' own comment has always made — "over the cap it reads the
    # ROLLUPS instead, which is what they exist for" — and which nothing implemented: Rollups()
    # had zero callers anywhere in the app.
    #
    # THE WINDOW IS THE UI's OWN 24h RANGE, deliberately. Asking for an all-time window selects
    # the 1h span, and a bench that has been running for ten minutes has no complete HOUR to have
    # rolled up — so the app correctly falls back to truncated raw, and a check demanding rollups
    # there would be measuring the bench's runtime, not the app. 24h is what the device page
    # actually requests when you open it, and it selects the 1m span the worker has built.
    now = int(time.time())
    points, span, truncated = series(c, dev_id, "chatty", frm=now - 24 * 3600, to=now)
    print("    24h window: span=%s truncated=%s points=%d" % (span, truncated, len(points)))
    check("a window over the cap is now served from the ROLLUPS, not truncated away",
          span in ("1m", "1h"), "span=%s truncated=%s" % (span, truncated))

    if span in ("1m", "1h") and points:
        # The fallback tops the buckets up with the raw tail the worker has not folded yet —
        # without it the chart would always be missing its most recent stretch, which is the part
        # anybody actually looks at. Gated on the rollup path having been taken: run
        # unconditionally, this passes on a plain truncated window and proves nothing.
        newest = (latest_map(c, dev_id).get("chatty") or {}).get("ts")
        check("...and it still reaches the newest reading (buckets plus the unfolded raw tail)",
              newest is not None and points[-1].get("ts") >= newest - 60000,
              "newest stored ts=%s, newest charted ts=%s" % (newest, points[-1].get("ts")))

        # And it must cover the OLD end too — that is the whole point of falling back rather
        # than truncating. The bench published its chatty rows in two bursts, so the window
        # starts well before the last 2000 raw rows the unfixed app would have returned.
        con = db()
        try:
            oldest_raw = con.execute(
                "select min(ts) from device_reading where device_id=? and key='chatty'",
                (dev_id,)).fetchone()[0]
        finally:
            con.close()
        check("...and it reaches back to the START of the window, not just the recent slice",
              oldest_raw is not None and points[0].get("ts") <= oldest_raw + 120000,
              "oldest stored ts=%s, oldest charted ts=%s" % (oldest_raw, points[0].get("ts")))

    # THE RAW TAIL, exercised rather than tolerated. A rollup bucket is stamped at its own start,
    # so "the newest charted point is within a minute of the newest reading" can pass with no tail
    # appended at all — and the tail is the part that stops a chart from permanently lagging the
    # rollup interval. Publish fresh rows INTO the current, deliberately-unfolded bucket, then ask
    # again: those rows exist only in raw, so the chart can only reach them through the top-up.
    print("    publishing into the current (unfolded) bucket to exercise the raw tail...")
    for i in range(40):
        wire.publish(TELEMETRY_TOPIC, json.dumps({"chatty": 9000.0 + i}))
    time.sleep(SETTLE + 2.0)

    con = db()
    try:
        newest_raw = con.execute(
            "select max(ts) from device_reading where device_id=? and key='chatty'",
            (dev_id,)).fetchone()[0]
        last_bucket = con.execute(
            "select max(bucket) from reading_rollup where device_id=? and key='chatty' and span='1m'",
            (dev_id,)).fetchone()[0]
    finally:
        con.close()

    now2 = int(time.time())
    pts2, span2, trunc2 = series(c, dev_id, "chatty", frm=now2 - 24 * 3600, to=now2)
    unfolded = last_bucket is not None and newest_raw is not None and newest_raw > (last_bucket + 60) * 1000
    check("there really ARE raw rows past the newest bucket (the tail has something to add)",
          unfolded, "newest raw ts=%s, last complete bucket=%s" % (newest_raw, last_bucket))
    if unfolded:
        check("...and the chart reaches them — buckets topped up with the UNFOLDED raw tail",
              span2 in ("1m", "1h") and pts2 and pts2[-1].get("ts") >= newest_raw,
              "span=%s, newest charted ts=%s, newest stored ts=%s"
              % (span2, pts2[-1].get("ts") if pts2 else None, newest_raw))

    # The other half of the promise: when NO rollup covers the span (an all-time window on an
    # appliance that has not completed an hour yet), the answer must be the most recent raw slice
    # AND an explicit truncated flag — never an empty chart over a window full of data.
    wide, wide_span, wide_trunc = series(c, dev_id, "chatty")
    check("a span the rollup worker has not reached yet falls back to raw and SAYS so",
          wide_span == "raw" and wide_trunc and len(wide) > 0,
          "all-time window: %d points, span=%s truncated=%s" % (len(wide), wide_span, wide_trunc))


# --------------------------------------------------------------------------------------------
# Part 8 — the counters an operator watches
# --------------------------------------------------------------------------------------------

def part_stats(c):
    print("\n--- the counters: suppressed vs stored IS the storage design ---")
    s = stats(c)
    print("    ", json.dumps(s))
    check("the ingest counters are exposed and non-zero",
          (s.get("received") or 0) > 0 and (s.get("stored") or 0) > 0,
          "received=%s decoded=%s stored=%s suppressed=%s"
          % (s.get("received"), s.get("decoded"), s.get("stored"), s.get("suppressed")))
    check("the deadband suppressed real traffic (it is doing its job at all)",
          (s.get("suppressed") or 0) > 0, "suppressed=%s" % s.get("suppressed"))
    check("nothing was shed by the write queue during the bench",
          (s.get("dropped") or 0) == 0, "dropped=%s" % s.get("dropped"))

    # `written` is the counter an operator compares against `stored` to see whether the batcher
    # is keeping up. It used to be the sum of each batch's LAST INSERT ID, so it grew with the
    # size of the table rather than with the work done — 2,666 readings reported as 3,555,111.
    written, stored_n = s.get("written") or 0, s.get("stored") or 0
    check("`written` counts ROWS, not the driver's last insert id",
          0 < written <= stored_n,
          "written=%s vs stored=%s (written must never exceed what was enqueued)"
          % (written, stored_n))

    con = db()
    try:
        on_disk = con.execute("select count(*) from device_reading").fetchone()[0]
    finally:
        con.close()
    check("...and it agrees with the number of rows actually on disk",
          abs(written - on_disk) <= 50,
          "written=%s, rows in device_reading=%s" % (written, on_disk))


# --------------------------------------------------------------------------------------------

def main():
    c = admin()
    print("signed in to", BASE)

    profile_id = make_profile(c)
    dev_id, pw = make_device(c, profile_id)
    print("profile=%s device=%s" % (profile_id, dev_id))
    wire = DeviceWire(DEVICE_KEY, pw)

    try:
        part_gate(c, dev_id, wire)
        part_heartbeat(c, dev_id, wire)
        part_suppressed_still_alerts(c, dev_id, wire)
        part_profile_edit(c, dev_id, profile_id, wire)
        part_ingest(c, dev_id, wire)
        part_liveness(c, dev_id, wire)
        part_gate_memory(c, profile_id)
        part_readback(c, dev_id, wire)
        part_rollup(c, dev_id)
        part_rollup_fallback(c, dev_id, wire)
        part_stats(c)
    finally:
        wire.close()

    print("\n%d/%d checks passed" % (len(PASSES), len(PASSES) + len(FAILS)))
    for f in FAILS:
        print("  FAILED:", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
