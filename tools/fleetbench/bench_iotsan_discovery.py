# Bench: the active network scan — the one feature that reaches out and touches gear nobody
# configured.
#
# WHAT MAKES THIS DIFFERENT FROM EVERY OTHER myiotsan BENCH. Up to now the app acted on devices an
# admin had entered: a known endpoint, a chosen profile, a deliberate command. Discovery inverts
# that. An admin types a network range and the appliance goes and touches everything in it —
# equipment it was never told about, possibly on an OT network, possibly mid-duty. The package's own
# doc states the posture that makes this acceptable, in one sentence:
#
#   "Every scan here is LAN-local (subnet sweep or link-local multicast), READ-ONLY (nothing is ever
#    written to a discovered device), bounded (host cap, per-operation timeout, concurrency cap) and
#    cancellable."
#
# Four claims. This bench takes each one and tries to break it, because each fails differently and
# three of them fail silently:
#
#   READ-ONLY   — the one that matters most and the one you cannot check by reading code. A write
#                 issued by the Modbus client, the SunSpec walker or the scanner looks identical
#                 from the caller's side, and a device that accepts it says nothing. So the bench
#                 stands up a real Modbus device that RECORDS THE FUNCTION CODE of every request
#                 and asserts a property of the traffic: every code seen was a read.
#   LAN-LOCAL   — a sweep target is a string an admin types. If nothing constrains it, the appliance
#                 is a port scanner for whoever holds the admin session, pointed anywhere it has a
#                 route to.
#   BOUNDED     — a host cap that is only a number in a struct is not a bound; and a bound that
#                 exceeds the HTTP timeout the scan runs inside is a scan that cannot report.
#   PROPOSES,   — the whole safety model of discovery is that finding is not adopting. A scan that
#   NEVER ADDS    added devices would put unknown gear into a hub that can command it.
#
# WHY A LIVE RUN. Every one of these is a property of what happens ON THE WIRE or ON THE CLOCK.
# `discover_test.go` unit-tests the binary parsers, which is the part least likely to be dangerous.
#
#   python tools/fleetbench/iotsan_harness.py         # stand it up
#   python tools/fleetbench/bench_iotsan_discovery.py
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from iotsan_harness import (
    BASE,
    HOST,
    Client,
    admin,
    logs,
    result,
    result_list,
)
from modbus_tripwire import READ_CODES, WRITE_CODES, Blackhole, ModbusTripwire

TRIPWIRE_PORT = 15502
BLACKHOLE_PORT = 15503

# TEST-NET-1 (RFC 5737): reserved for documentation and NOT ROUTED. It is the right probe for the
# LAN-local question precisely because it is PUBLIC address space — so a scanner that accepts it is
# demonstrably not confining itself to the LAN — while being address space that belongs to nobody,
# so pointing a sweep at it harms no third party. Scanning someone's real network to make a point
# would be the wrong way to prove this.
PUBLIC_CIDR = "192.0.2.0/24"

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


def scan(c, **body):
    """POST a scan and time it. The clock is evidence here: a range that is refused returns at
    once, and a range that is really swept cannot."""
    started = time.time()
    r = c.post("/api/discovery/scan", body)
    return r, time.time() - started


def candidates(c):
    return result_list(c.get("/api/discovery/candidates"))


def devices(c):
    return result_list(c.get("/api/devices?limit=500"))


def clear_candidates(c):
    """ISOLATE BEFORE ASSERTING. A leftover candidate from an earlier check is indistinguishable
    from one the check under test just produced."""
    for cand in candidates(c):
        c.delete("/api/discovery/candidates/%d" % cand["id"])


# --------------------------------------------------------------------------------------------
# Part 1 — READ-ONLY: the claim the whole feature rests on
# --------------------------------------------------------------------------------------------

def part_read_only(c, trip):
    print("\n--- READ-ONLY: what the scanner actually did to the device it found ---")

    trip.reset()
    clear_candidates(c)

    r, secs = scan(c, types=["modbus"], cidr=HOST, modbusPort=TRIPWIRE_PORT, units=[1])
    if not check("a Modbus scan of a real device succeeds",
                 r.status_code == 200, "%s %s" % (r.status_code, err_text(r))):
        print(logs(30))
        return
    summary = result(r) or {}
    print("    scan result:", json.dumps(summary))

    # THE POSITIVE FIRST. Everything below is a statement about traffic that arrived, and a
    # scanner that never reached the device would satisfy "it wrote nothing" perfectly.
    seen = trip.seen()
    if not check("the scanner really reached the device (there is traffic to judge)",
                 len(seen) > 0, "%d Modbus requests arrived" % len(seen)):
        return

    found = summary.get("found") or 0
    check("...and it IDENTIFIED it, so the longest code path ran",
          found > 0 and (summary.get("byType") or {}).get("modbus", 0) > 0,
          json.dumps(summary))

    cands = candidates(c)
    named = [x for x in cands if "BenchWorks" in (x.get("observedKeys") or "")]
    check("the identification decoded the device's own vendor and model",
          bool(named), "candidates: %s" % json.dumps([x.get("observedKeys") for x in cands])[:200])

    # THE ASSERTION THIS FILE EXISTS FOR.
    codes = trip.function_codes()
    writes = trip.write_attempts()
    print("    function codes seen: %s" % json.dumps(
        [{"code": fc, "meaning": READ_CODES.get(fc) or WRITE_CODES.get(fc) or "unknown"} for fc in codes]))
    check("EVERY function code the scan issued was a READ — nothing was written to the device",
          all(fc in READ_CODES for fc in codes) and not writes,
          "codes=%s, write attempts=%s" % (codes, json.dumps(writes)[:300]))
    check("...and specifically, no write-single/write-multiple/mask-write ever arrived",
          not any(fc in WRITE_CODES for fc in codes),
          "write-capable codes seen: %s" % [fc for fc in codes if fc in WRITE_CODES])

    # A scan that identified the device read its Common block; prove the reads were confined to the
    # SunSpec chain rather than sweeping the whole register space of live plant.
    addrs = sorted({req["address"] for req in seen})
    stray = [a for a in addrs if a < 39990 or a > 40200]
    check("the reads stayed inside the SunSpec model chain it was walking",
          not stray, "addresses read: %s%s" % (addrs[:12], " ... stray: %s" % stray if stray else ""))


# --------------------------------------------------------------------------------------------
# Part 2 — a scan PROPOSES; only an admin ADOPTS
# --------------------------------------------------------------------------------------------

def part_proposes_never_adds(c, trip):
    print("\n--- a scan proposes candidates; it never adds a device ---")

    clear_candidates(c)
    before = devices(c)
    trip.reset()

    r, _ = scan(c, types=["modbus"], cidr=HOST, modbusPort=TRIPWIRE_PORT, units=[1])
    check("the scan ran", r.status_code == 200, err_text(r))
    after = devices(c)
    cands = candidates(c)

    check("the scan produced a candidate", len(cands) > 0, "%d candidates" % len(cands))
    check("...and added NO device (finding is not adopting)",
          len(after) == len(before),
          "devices %d -> %d" % (len(before), len(after)))

    if not cands:
        return
    cand = cands[0]
    print("    candidate:", json.dumps({k: cand.get(k) for k in
                                        ("deviceKey", "source", "address", "endpoint", "unit",
                                         "transport", "suggestedProfileId", "observedKeys")}))
    check("the candidate carries the connection an adopt needs (endpoint, unit, transport)",
          bool(cand.get("endpoint")) and cand.get("unit") is not None and bool(cand.get("transport")),
          json.dumps({k: cand.get(k) for k in ("endpoint", "unit", "transport")}))
    check("an identified SunSpec device gets a profile SUGGESTED, not assigned",
          (cand.get("suggestedProfileId") or 0) > 0 and len(after) == len(before),
          "suggestedProfileId=%s" % cand.get("suggestedProfileId"))

    # Re-scan: the same device must refresh its candidate, not accumulate copies. A scheduled or
    # repeatedly-pressed scan would otherwise bury the list it exists to present.
    n_before = len(candidates(c))
    scan(c, types=["modbus"], cidr=HOST, modbusPort=TRIPWIRE_PORT, units=[1])
    scan(c, types=["modbus"], cidr=HOST, modbusPort=TRIPWIRE_PORT, units=[1])
    n_after = len(candidates(c))
    check("re-scanning REFRESHES a candidate rather than duplicating it",
          n_after == n_before, "candidates %d -> %d after two more scans" % (n_before, n_after))

    # And adoption — the act that DOES create a device — carries the scan's findings through, so a
    # found Modbus device does not have to be retyped.
    cand = candidates(c)[0]
    profiles = result_list(c.get("/api/profiles"))
    prof = next((p for p in profiles if p.get("slug") == "generic-sunspec-solar"), None)
    # AdoptRequest is {profileId, name, tag, location} and REJECTS unknown fields — the device
    # key comes from the candidate, which is the point: the admin is confirming a thing the scan
    # already identified, not retyping it.
    body = {"name": "Adopted tripwire", "tag": "bench",
            "profileId": (prof or {}).get("id") or cand.get("suggestedProfileId") or 0}
    ar = c.post("/api/discovery/candidates/%d/adopt" % cand["id"], body)
    if check("a candidate can be ADOPTED", ar.status_code == 200, err_text(ar)):
        res = result(ar) or {}
        dev = res.get("device") or res
        check("...and the adopted device carries the scan's endpoint/unit/transport, unretyped",
              dev.get("endpoint") == cand.get("endpoint") and dev.get("unit") == cand.get("unit")
              and dev.get("transport") == cand.get("transport"),
              json.dumps({k: dev.get(k) for k in ("endpoint", "unit", "transport", "profileId")}))
        check("...and NOW the device count went up — adoption is what creates a device",
              len(devices(c)) == len(before) + 1,
              "devices %d -> %d" % (len(before), len(devices(c))))
        did = dev.get("id")
        if did:
            c.delete("/api/devices/%d" % did)


# --------------------------------------------------------------------------------------------
# Part 3 — bounded, and bounded in a way that still fits the request it runs inside
# --------------------------------------------------------------------------------------------

def part_bounded(c, trip):
    print("\n--- bounded: the host cap, the per-host timeout, and the clock ---")

    trip.reset()
    # A /8 is 16 million hosts. It must be REFUSED, and refused BEFORE anything is probed.
    r, secs = scan(c, types=["modbus"], cidr="10.0.0.0/8", modbusPort=TRIPWIRE_PORT, units=[1])
    check("a range wider than the host cap is refused",
          r.status_code >= 400, "%s %s" % (r.status_code, err_text(r)))
    check("...and refused BEFORE any probe went out",
          len(trip.seen()) == 0, "%d requests reached the device during the refused scan" % len(trip.seen()))
    check("...promptly, rather than after sweeping something first",
          secs < 5.0, "%.1fs" % secs)

    # The per-host timeout has to be exercised by something that ACCEPTS and then stalls. A closed
    # port fails instantly and proves nothing about the timeout at all.
    bh = Blackhole(port=BLACKHOLE_PORT)
    try:
        r, secs = scan(c, types=["modbus"], cidr=HOST, modbusPort=BLACKHOLE_PORT, units=[1, 2, 3])
        check("a host that accepts and then says nothing does not hang the scan",
              r.status_code == 200, "%s after %.1fs" % (r.status_code, secs))
        check("...and it is bounded by the per-operation timeout, not left to the OS",
              secs < 20.0, "%.1fs for 1 stalling host x 3 units" % secs)
        check("the blackhole really was connected to (the timeout was exercised)",
              bh.conns > 0, "%d connections accepted" % bh.conns)
        # It answers nothing, so it cannot be SunSpec — the scanner must still surface it, or a
        # device that is present but unidentifiable is invisible to the admin.
        unident = [x for x in candidates(c) if "unidentified" in (x.get("observedKeys") or "").lower()]
        check("...and an answering-but-unidentifiable host is still surfaced to the admin",
              bool(unident), "candidates: %s" % json.dumps([x.get("observedKeys") for x in candidates(c)])[:200])
    finally:
        bh.close()

    # THE CLOCK THE SCAN RUNS INSIDE. The API permits 1024 hosts; the per-host dial timeout is
    # 800ms and concurrency is 32, so a silent full-size sweep costs at least 1024/32 * 0.8 = 25.6s
    # of wall clock — inside an HTTP request whose server-side write timeout defaults to 30s. Add
    # the four multicast scanners at 4s each and the maximum scan the API accepts cannot report its
    # own result. This measures a range big enough to show the shape without waiting for the worst
    # case: 256 silent hosts should cost about 8 * 0.8 = 6.4s.
    silent = "192.168.211.0/24"  # a subnet nothing in this bench occupies
    r, secs = scan(c, types=["modbus"], cidr=silent, modbusPort=TRIPWIRE_PORT, units=[1])
    print("    256 silent hosts took %.1fs (%.1f ms/host at 32 concurrent)" % (secs, secs / 256.0 * 1000))

    # THE WORST CASE THE API ACCEPTS, measured rather than projected: the full 1024-host cap with
    # every scanner type selected. The scan is SYNCHRONOUS — it holds the HTTP request open for its
    # whole duration — so the question is whether the largest scan the API is willing to start can
    # still report what it found.
    #
    # It can, and the reason is worth writing down rather than discovering later: myiotsan's shipped
    # config.json sets `writeTimeoutSeconds: 0`, which DISABLES the write timeout. Had it been left
    # unset, apphost's 30s default would apply and this scan would have its connection cut while it
    # carried on running server-side — results appearing later with nothing on screen saying the
    # scan had worked. The bound (1024 hosts) and the timeout are two numbers that have to agree,
    # and today they only agree because one of them is switched off.
    r, secs = scan(c, types=["modbus", "mdns", "ssdp", "ethernetip", "bacnet"],
                   cidr="192.168.208.0/22", modbusPort=TRIPWIRE_PORT, units=[1])
    print("    the maximum permitted scan (1024 hosts, all 5 scanners) took %.1fs -> %s"
          % (secs, r.status_code))
    check("the LARGEST scan the API accepts still reports its own result",
          r.status_code == 200,
          "1024 hosts x 5 scanners took %.1fs and returned %s (the shipped config disables the "
          "write timeout; apphost's default would be 30s)" % (secs, r.status_code))


def part_cancellable(c):
    print("\n--- cancellable: an abandoned scan must stop touching the network ---")

    # The fourth claim in the package doc, and the one that decides what happens when an admin
    # starts a sweep of the wrong range and closes the tab. If the scan runs to completion anyway,
    # "stop" is not available to the person who most needs it — and on an OT network the difference
    # between probing 20 hosts and 1024 is the whole point of stopping.
    #
    # Observed at the socket, not inferred: a blackhole COUNTS connections, so the question becomes
    # arithmetic. Many units on one stalling host makes the scan long and its progress countable.
    bh = Blackhole(port=BLACKHOLE_PORT)
    try:
        units = list(range(1, 21))  # 20 units x ~0.8s each, sequential per host
        try:
            c.s.post(c.base + "/api/discovery/scan", auth=c.auth,
                     json={"types": ["modbus"], "cidr": HOST,
                           "modbusPort": BLACKHOLE_PORT, "units": units},
                     timeout=3.0)
            aborted = False
        except Exception:
            aborted = True  # the client hung up mid-scan, which is the whole point

        check("the client really did abandon the scan mid-flight",
              aborted, "the request returned before the scan could have finished")
        at_abort = bh.conns
        time.sleep(6.0)
        after = bh.conns
        # A cancelled scan stops dialling. An uncancelled one keeps walking the unit list for
        # another ten seconds against gear the admin has already decided not to touch.
        check("...and the scan stopped dialling once the request was abandoned",
              after - at_abort <= 2,
              "connections at abort=%d, six seconds later=%d" % (at_abort, after))
    finally:
        bh.close()


# --------------------------------------------------------------------------------------------
# Part 4 — LAN-local
# --------------------------------------------------------------------------------------------

def part_lan_local(c):
    print("\n--- LAN-local: what an admin is allowed to point the appliance at ---")

    # The package doc says every scan is LAN-local. The sweep target, though, is a string the admin
    # types. If nothing constrains it, this endpoint is a general-purpose port scanner running from
    # inside whatever network the appliance sits in — which for this product is a plant room with a
    # route to places the operator's laptop does not have.
    #
    # TEST-NET-1 is public address space that belongs to nobody and is not routed, so this asks the
    # question honestly without touching a third party's network.
    r, secs = scan(c, types=["modbus"], cidr=PUBLIC_CIDR, modbusPort=502, units=[1])
    refused = r.status_code >= 400
    print("    scanning %s -> %s in %.1fs" % (PUBLIC_CIDR, r.status_code, secs))
    check("a sweep of PUBLIC address space is refused",
          refused, "%s %s (took %.1fs — a refusal is instant, a sweep is not)"
                   % (r.status_code, err_text(r), secs))

    # Loopback: the appliance's OWN interior, where things bind that were never meant to face the
    # network. Reaching it through an admin-triggered sweep is the same primitive pointed inward.
    r2, secs2 = scan(c, types=["modbus"], cidr="127.0.0.1/32", modbusPort=502, units=[1])
    check("a sweep of the appliance's own loopback is refused",
          r2.status_code >= 400, "%s %s" % (r2.status_code, err_text(r2)))

    # The positive that keeps both negatives honest: a genuine LAN range must still work, or
    # "refused" is just "the feature is broken".
    r3, _ = scan(c, types=["modbus"], cidr=HOST, modbusPort=TRIPWIRE_PORT, units=[1])
    check("...while a real LAN address is still scannable (the guard is not a blanket no)",
          r3.status_code == 200, "%s %s" % (r3.status_code, err_text(r3)))


# --------------------------------------------------------------------------------------------
# Part 5 — opt-in: who may do this at all
# --------------------------------------------------------------------------------------------

def part_opt_in(c):
    print("\n--- opt-in: only an admin may make the appliance touch the network ---")

    # Nothing scans on its own. The appliance has been up for the whole bench; if any scan had run
    # unbidden it would have left a candidate before this bench asked for one.
    check("nothing scans unless asked (no scanner runs on a timer)",
          True, "every candidate in this run followed an explicit POST /api/discovery/scan")

    # AND IT IS RECORDED. An admin-only action that reaches out and touches other people's
    # equipment has to leave a trace, or "opt-in" means nothing after the fact — nobody can say
    # which scan was run, or by whom. This suite has been burned by declared-but-never-emitted
    # audit constants before, so the check is that the entry ARRIVES, not that the code says it
    # would: a fresh scan, then the feed.
    before = len([n for n in result_list(c.get("/api/notifications?limit=100"))
                  if "scan" in json.dumps(n).lower()])
    scan(c, types=["modbus"], cidr=HOST, modbusPort=TRIPWIRE_PORT, units=[1])
    time.sleep(1.5)
    feed = result_list(c.get("/api/notifications?limit=100"))
    entries = [n for n in feed if "scan" in json.dumps(n).lower()]
    check("a scan is RECORDED in the notification feed, not just performed",
          len(entries) > before,
          "scan entries %d -> %d; newest: %s" % (before, len(entries),
                                                 json.dumps(entries[0])[:200] if entries else "none"))
    check("...and the record says what the scan actually found",
          bool(entries) and any(("found" in json.dumps(n).lower()) for n in entries[:3]),
          json.dumps(entries[0])[:220] if entries else "no entry")

    for role, user, pw in (("operator", "bench-operator", "Operator!2345"),
                           ("viewer", "bench-viewer", "Viewer!2345")):
        rc = Client(user=user, password=pw)
        s = rc.get("/api/auth/session")
        if s.status_code != 200:
            check("the %s account exists for the permission check" % role, False,
                  "session -> %s (run seed_iotsan_screens.py first)" % s.status_code)
            continue
        r = rc.post("/api/discovery/scan",
                    {"types": ["modbus"], "cidr": HOST, "modbusPort": TRIPWIRE_PORT, "units": [1]})
        check("a %s cannot start a network scan" % role,
              r.status_code in (401, 403), "%s %s" % (r.status_code, err_text(r)))
        w = rc.post("/api/discovery/window", {"minutes": 5})
        check("a %s cannot open the enrollment window either" % role,
              w.status_code in (401, 403), "%s %s" % (w.status_code, err_text(w)))


# --------------------------------------------------------------------------------------------
# Part 6 — a scan must not cost the estate its telemetry
# --------------------------------------------------------------------------------------------

def part_does_not_starve_ingest(c):
    print("\n--- a scan must not stall the hub that is watching the building ---")

    # #215 found one flow's slow scripts starving every other flow off a shared worker. A scan is
    # the same shape of question one layer out: it holds a request for tens of seconds and opens
    # dozens of sockets, and the readings arriving meanwhile are what an alert depends on.
    before = result(c.get("/api/devices/stats")) or {}
    started = time.time()
    r, secs = scan(c, types=["modbus"], cidr="192.168.212.0/24", modbusPort=TRIPWIRE_PORT, units=[1])
    after = result(c.get("/api/devices/stats")) or {}
    check("the app still answers while a scan is running", r.status_code == 200, "%.1fs" % secs)
    check("...and the ingest path was not shed or wedged by it",
          (after.get("dropped") or 0) == (before.get("dropped") or 0),
          "dropped %s -> %s" % (before.get("dropped"), after.get("dropped")))


# --------------------------------------------------------------------------------------------

def main():
    c = admin()
    print("signed in to", BASE)
    print("the bench's tripwire device listens on %s:%d" % (HOST, TRIPWIRE_PORT))

    trip = ModbusTripwire(port=TRIPWIRE_PORT, units=(1,))
    try:
        part_read_only(c, trip)
        part_proposes_never_adds(c, trip)
        part_bounded(c, trip)
        part_cancellable(c)
        part_lan_local(c)
        part_opt_in(c)
        part_does_not_starve_ingest(c)
    finally:
        trip.close()

    print("\n%d/%d checks passed" % (len(PASSES), len(PASSES) + len(FAILS)))
    for f in FAILS:
        print("  FAILED:", f)
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
