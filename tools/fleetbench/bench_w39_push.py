# W3-9 bench: mobile push, against a REAL push service and a REAL fleet event.
#
# THE CLAIM UNDER TEST IS NOT "the API returns 200". It is:
#
#   1. this appliance actually POSTs to a push service, with the wire format the spec
#      requires and a VAPID assertion addressed to that service — checked by reading the
#      bytes off the wire, not by trusting the sender;
#   2. what leaves the building is CIPHERTEXT: the notification text the operator would read
#      does not appear anywhere in the request body;
#   3. the four answers a push service can give produce four DIFFERENT outcomes, and in
#      particular "could not be reached" is never reported as a refusal — that distinction is
#      the whole feature on an intranet install;
#   4. a subscription the service says is GONE is deleted, not left to be retried forever;
#   5. a real fleet event — a node actually going dark — reaches the device, and the
#      device's own severity floor is honoured when it comes back;
#   6. the app is genuinely installable: manifest, service worker and icons are served, and
#      the worker really handles push.
#
# (5) is the one that matters most. Everything else could pass on a feature that is wired to
# nothing: the notification hub is where this has to be plugged in, and a bench that only
# presses "send a test" never touches that wiring.
#
# HOW THE PUSH SERVICE IS REAL. A fake one runs on the host over TLS, and the control plane
# is restarted with SSL_CERT_FILE pointing at its certificate — the way Go trusts a CA on
# Linux. The product has no trust override and must not grow one.
#
# WHAT THIS BENCH DOES NOT PROVE. It does not decrypt the payload: that would mean writing a
# second implementation of RFC 8291 here, and two implementations by the same author agreeing
# proves they share a misreading. The encryption is checked instead against the RFC's own
# published test vector, byte for byte, in infra/webpush/webpush_test.go. What this bench
# proves is the WIRING — that a real request, with real ciphertext, really goes out.
#
#   python tools/fleetbench/fleet_harness.py
#   python tools/fleetbench/bench_w39_push.py
import base64
import http.server
import json
import os
import ssl
import sys
import threading
import time

import urllib3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import fleet_harness
from fleet_harness import (CP, CP_PORT, NODE_PORTS, ROOT, PASSWORDS,
                           result_of, result_list, sh, start_container, wait_up)

urllib3.disable_warnings()
CHECKS = []

WORK = os.path.join(ROOT, "w39")
# The name a container uses to reach a service on the host under Docker Desktop. Asserted
# before anything depends on it, so "nothing was delivered" can never quietly mean "the
# container could not resolve the host".
HOST_ALIAS = "host.docker.internal"
PUSH_PORT = 18446
PUSH_ORIGIN = "https://%s:%d" % (HOST_ALIAS, PUSH_PORT)
# An address nothing answers on. This is the AIR-GAPPED INSTALL, which is the deployment this
# product is normally sold into — so it is a first-class case here, not an edge case.
BLACKHOLE = "https://10.99.99.99/push/nowhere"


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


# --- the fake push service ------------------------------------------------------------------

# Every request the appliance made, in order: (path, headers dict, body bytes).
RECEIVED = []
RECEIVED_LOCK = threading.Lock()


class PushHandler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_POST(self):  # noqa: N802 (http.server's spelling)
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b""
        with RECEIVED_LOCK:
            RECEIVED.append((self.path, dict(self.headers), body))
        # The path decides the answer, so one service can play all four push services an
        # operator might meet.
        if self.path.startswith("/p/gone"):
            code = 410
        elif self.path.startswith("/p/refuse"):
            code = 400
        else:
            code = 201
        self.send_response(code)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def log_message(self, *_args):
        pass


def make_cert(work):
    """A self-signed certificate that is ALSO its own trust anchor, so one file serves as
    both the server's identity and the appliance's root store."""
    crt = os.path.join(work, "push.crt")
    key = os.path.join(work, "push.key")
    sh("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
       "-keyout", key, "-out", crt, "-days", "3",
       "-subj", "/CN=" + HOST_ALIAS,
       "-addext", "subjectAltName=DNS:%s,DNS:localhost,IP:127.0.0.1" % HOST_ALIAS,
       "-addext", "basicConstraints=critical,CA:TRUE",
       "-addext", "keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign")
    return crt, key


def serve_push(crt, key):
    httpd = http.server.ThreadingHTTPServer(("0.0.0.0", PUSH_PORT), PushHandler)
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(crt, key)
    httpd.socket = ctx.wrap_socket(httpd.socket, server_side=True)
    thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    thread.start()
    return httpd


def requests_to(path_prefix):
    with RECEIVED_LOCK:
        return [r for r in RECEIVED if r[0].startswith(path_prefix)]


# --- helpers ----------------------------------------------------------------------------------

def b64pad(s):
    return s + "=" * ((4 - len(s) % 4) % 4)


def header(headers, name):
    """HTTP header names are case-insensitive, and Go canonicalises what it sends: `TTL` goes
    out as `Ttl`. A dict lookup for the spelling the spec uses therefore finds nothing, and
    the bench reports a missing header on a request that carries it — a check that fails on
    correct output, which is as much a defect in a bench as one that passes on broken output."""
    for key, value in headers.items():
        if key.lower() == name.lower():
            return value
    return None


def jwt_claims(token):
    parts = token.split(".")
    if len(parts) != 3:
        return {}
    try:
        return json.loads(base64.urlsafe_b64decode(b64pad(parts[1])))
    except Exception:
        return {}


def subscribe(cp, path, label, min_severity):
    """Register a device. The API PROVES it before returning, so the response already
    carries the verdict — that is the contract this bench is here to check."""
    return cp.post("/api/push/devices", {
        "endpoint": (PUSH_ORIGIN + path) if path.startswith("/") else path,
        # A REAL P-256 public key, generated once and pinned here. Key material a browser
        # would supply — and it has to be genuine: the appliance derives a shared secret from
        # it, so a made-up string fails encryption before any request goes out, and the bench
        # would then be measuring its own bad input rather than the product.
        "p256dh": "BKJojqbb7aQGaXYYQARTwFh792CxoPZWFEiEKapRwo9sO6mWv-3VFgAORRIzaYgl8jzR4A8KbTGeWAzgiArebqo",
        "auth": "ytBPrsxcRCEmq6x5dGIh0g",
        "label": label,
        "minSeverity": min_severity,
    })


def devices(cp):
    return result_list(cp.get("/api/push/devices"), "items")


def wait_for(predicate, timeout, poll=2.0):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if predicate():
            return True
        time.sleep(poll)
    return predicate()


def main():
    os.makedirs(WORK, exist_ok=True)

    # ---- the push service, and an appliance that trusts it ----------------------------------
    crt, key = make_cert(WORK)
    # /data is the control plane's own mount, so the certificate is reachable from inside the
    # container without a second volume.
    with open(crt, "rb") as fh:
        cert_bytes = fh.read()
    with open(os.path.join(ROOT, "cp", "pushca.pem"), "wb") as fh:
        fh.write(cert_bytes)
    httpd = serve_push(crt, key)

    sh("docker", "rm", "-f", "cp", check=False)
    start_container("cp", "myseliasan", 3002, CP_PORT, env={"SSL_CERT_FILE": "/data/pushca.pem"})
    cp_url = "https://127.0.0.1:%d" % CP_PORT
    if not wait_up(cp_url + "/api/auth/session"):
        print(sh("docker", "logs", "--tail", "40", "cp", check=False))
        raise SystemExit("the control plane did not come back up")

    resolved = sh("docker", "exec", "cp", "getent", "hosts", HOST_ALIAS, check=False)
    check("the appliance can resolve the host running the push service",
          HOST_ALIAS in (resolved or ""),
          "getent said: %s" % (resolved or "").strip()[:120])
    if HOST_ALIAS not in (resolved or ""):
        # Everything after this would fail for a reason that has nothing to do with the
        # feature, and a wall of red hides the one line that matters.
        raise SystemExit("no route from the container to the host; the rest of this bench "
                         "would be measuring Docker, not the product")

    cp = CP(cp_url)
    for pw in PASSWORDS:
        if cp.login(pw=pw).status_code == 200:
            break
    else:
        raise SystemExit("cp login failed for every known password")

    # ---- (6) is it actually installable -----------------------------------------------------
    man = cp.get("/manifest.json")
    manifest = {}
    try:
        manifest = man.json()
    except Exception:
        pass
    icons = manifest.get("icons") or []
    check("the web manifest is served and parses", man.status_code == 200 and bool(manifest.get("name")),
          "%d %s" % (man.status_code, man.text[:120]))
    check("it declares a maskable icon and a 192px one, which is what makes it installable",
          any(i.get("sizes") == "192x192" for i in icons)
          and any("maskable" in (i.get("purpose") or "") for i in icons),
          json.dumps([i.get("sizes") for i in icons]))
    for icon in icons:
        src = icon.get("src") or ""
        r = cp.get(src)
        # A manifest that names an icon nobody serves installs with a blank square, and the
        # manifest itself still validates — so the file has to be fetched, not trusted.
        ok = r.status_code == 200 and r.content[:8] == b"\x89PNG\r\n\x1a\n"
        check("the icon it names is really there and really a PNG: " + src, ok,
              "%d, %d bytes" % (r.status_code, len(r.content)))

    sw = cp.get("/sw.js")
    body = sw.text if sw.status_code == 200 else ""
    check("the service worker is served from the site ROOT, so its scope covers the whole app",
          sw.status_code == 200 and "javascript" in (sw.headers.get("Content-Type") or ""),
          "%d %s" % (sw.status_code, sw.headers.get("Content-Type")))
    check("and it handles push and the tap on the notification",
          '"push"' in body and '"notificationclick"' in body,
          "%d bytes" % len(body))
    # The one thing this worker must NOT do. A control plane that served yesterday's node
    # list out of a cache would show a green estate that went dark an hour ago.
    check("it caches nothing: a stale fleet is worse than an unreachable one",
          "caches" not in body and "CacheStorage" not in body)

    # ---- identity ---------------------------------------------------------------------------
    st = result_of(cp.get("/api/push/status"))
    public_key = st.get("publicKey") or ""
    check("the install has a push identity and offers its public half to browsers",
          bool(st.get("configured")) and len(public_key) > 80, public_key[:24])
    check("with nothing registered it says so, rather than reporting itself healthy",
          st.get("delivery") == "no-devices", json.dumps(st)[:160])

    # ---- (1)(2) a real delivery, read off the wire -------------------------------------------
    r = subscribe(cp, "/p/ok/phone", "Bench phone", "critical")
    device_ok = result_of(r)
    check("registering a device performs a REAL delivery and reports what it found",
          device_ok.get("lastOutcome") == "delivered",
          "%d %s" % (r.status_code, json.dumps(device_ok)[:200]))
    check("and the push service really was contacted", len(requests_to("/p/ok")) == 1,
          "%d requests arrived" % len(requests_to("/p/ok")))

    if requests_to("/p/ok"):
        path, headers, raw = requests_to("/p/ok")[0]
        check("the request declares the encrypted content encoding the spec requires",
              header(headers, "Content-Encoding") == "aes128gcm", str(header(headers, "Content-Encoding")))
        check("and carries a TTL, so a phone that is off does not lose the alert outright",
              (header(headers, "TTL") or "").isdigit(), str(header(headers, "TTL")))

        auth = header(headers, "Authorization") or ""
        token = ""
        sent_key = ""
        for part in auth.replace("vapid ", "").split(","):
            part = part.strip()
            if part.startswith("t="):
                token = part[2:]
            elif part.startswith("k="):
                sent_key = part[2:]
        claims = jwt_claims(token)
        check("it identifies this appliance with a VAPID assertion", bool(token) and bool(claims),
              auth[:60])
        # THE ONE THAT IS EASY TO GET WRONG, and it fails at exactly one vendor rather than
        # all of them: the audience is the ORIGIN of the endpoint, never the full URL.
        check("addressed to the push service's ORIGIN, not to the endpoint URL",
              claims.get("aud") == PUSH_ORIGIN, str(claims.get("aud")))
        check("the assertion has not already expired", int(claims.get("exp") or 0) > time.time(),
              "exp=%s now=%d" % (claims.get("exp"), int(time.time())))
        check("and it is signed by the same key the browser was told to subscribe with",
              sent_key == public_key, "%s vs %s" % (sent_key[:16], public_key[:16]))

        # The wire format, byte for byte (RFC 8188 aes128gcm header).
        ok_len = len(raw) >= 16 + 4 + 1 + 65 + 17
        check("the body carries a salt, a record size, and the appliance's ephemeral key",
              ok_len
              and int.from_bytes(raw[16:20], "big") == 4096
              and raw[20] == 65
              and raw[21] == 0x04,
              "%d bytes, rs=%s idlen=%s first=%s" % (
                  len(raw),
                  int.from_bytes(raw[16:20], "big") if ok_len else "-",
                  raw[20] if ok_len else "-",
                  hex(raw[21]) if ok_len else "-"))
        # (2) The whole point of RFC 8291: the vendor carries the message and cannot read it.
        check("what leaves the building is ciphertext — the text is not in the request",
              b"Notifications are on" not in raw and b"MySeliaSan" not in raw,
              "%d bytes of body" % len(raw))

    # ---- (3) four answers, four outcomes -----------------------------------------------------
    refused = result_of(subscribe(cp, "/p/refuse/laptop", "Bench laptop", "info"))
    check("a service that answers and REFUSES is recorded as a refusal",
          refused.get("lastOutcome") == "rejected", json.dumps(refused)[:160])

    print("registering a device at an address nothing answers on (this waits for the timeout)...")
    unreachable = result_of(subscribe(cp, BLACKHOLE, "Bench air-gapped", "info"))
    # THE DISTINCTION THIS FEATURE EXISTS FOR. An intranet install reported as "the push
    # service refused us" sends somebody hunting a bug in the product for an afternoon.
    check("a service that cannot be REACHED is a different answer from one that refused",
          unreachable.get("lastOutcome") == "unreachable", json.dumps(unreachable)[:200])

    # ---- (4) a dead subscription is forgotten ------------------------------------------------
    gone = result_of(subscribe(cp, "/p/gone/old-phone", "Bench uninstalled", "info"))
    listed = devices(cp)
    check("a subscription the service says is GONE is deleted, not kept and retried forever",
          not any(d.get("label") == "Bench uninstalled" for d in listed),
          "gone response=%s; %d devices listed" % (json.dumps(gone)[:80], len(listed)))

    # ---- (H) key material never comes back ---------------------------------------------------
    raw_list = cp.get("/api/push/devices").text
    check("the browser's key material never comes back out of the API",
          "p256dh" not in raw_list and "ytBPrsxcRCEmq6x5dGIh0g" not in raw_list,
          raw_list[:160])

    # ---- (E) the install verdict --------------------------------------------------------------
    st = result_of(cp.get("/api/push/status"))
    check("one device proved reachable is enough to call delivery confirmed",
          st.get("delivery") == "confirmed", json.dumps(st)[:200])
    check("and the verdict is checkable: it says how many of how many were reached",
          st.get("devicesReached") == 1 and st.get("devices") == 3,
          "%s of %s" % (st.get("devicesReached"), st.get("devices")))
    vendors = st.get("vendors") or []
    # host:PORT, not just the host — which is what somebody writing a firewall rule actually
    # needs, and why this asserts a prefix rather than equality.
    check("it names the hosts a firewall would have to allow",
          any(v.startswith(HOST_ALIAS) for v in vendors)
          and any(v.startswith("10.99.99.99") for v in vendors),
          json.dumps(vendors))

    # ---- (G) the trail -------------------------------------------------------------------------
    trail = result_list(cp.get("/api/audit?action=push.subscribe&limit=20"), "items")

    def meta_of(entry):
        raw_meta = (entry or {}).get("metadata")
        if isinstance(raw_meta, dict):
            return raw_meta
        try:
            return json.loads(raw_meta) if raw_meta else {}
        except (TypeError, ValueError):
            return {}

    check("enrolling a device is recorded", len(trail) >= 4, "%d entries" % len(trail))
    check("the trail names the vendor, so somebody can answer where alerts went",
          any((meta_of(e).get("vendor") or "").startswith(HOST_ALIAS) for e in trail),
          json.dumps([meta_of(e).get("vendor") for e in trail])[:160])
    # An audit trail is a long-lived document read by people who never needed to know which
    # phone anybody carries.
    check("and it does NOT record the endpoint, which identifies somebody's personal device",
          "/p/ok/phone" not in json.dumps(trail), json.dumps(trail)[:160])

    # ---- (5) a REAL fleet event reaches the phone -----------------------------------------------
    #
    # THE FIRST VERSION OF THIS PASSED FOR THE WRONG REASON, and it is worth recording why.
    # It stopped node-a and waited for "a new request to the phone". One arrived in sixteen
    # seconds — from node-b, reporting low disk on boot. The bench declared the fleet wiring
    # proved and restarted node-a immediately, so the node-offline event it was supposedly
    # testing never even fired. A fleet is a NOISY place; "something arrived" proves nothing
    # about what arrived.
    #
    # So the control plane's own notification log is the oracle. In a settled window, the
    # counts must agree exactly: the device that asked for everything gets every notification,
    # and the device that asked for critical gets exactly the critical ones. That makes the
    # per-device floor checkable rather than assumed, and it tolerates whatever else the fleet
    # decides to say while the window is open.

    def notifications():
        return result_list(cp.get("/api/notifications?limit=200"), "items")

    def snapshot():
        with RECEIVED_LOCK:
            return {n.get("id") for n in notifications()}, len(RECEIVED)

    def quiet(seconds=25, timeout=240):
        """Wait until nothing new has been published OR pushed for `seconds`."""
        ids, count = snapshot()
        calm = time.time()
        deadline = time.time() + timeout
        while time.time() < deadline:
            time.sleep(3)
            ids2, count2 = snapshot()
            if ids2 != ids or count2 != count:
                ids, count, calm = ids2, count2, time.time()
            elif time.time() - calm >= seconds:
                return True
        return False

    def titled(items, title):
        return [n for n in items if (n.get("title") or "") == title]

    def window(label, action, expect_title, wait_seconds):
        """Run `action`, wait for `expect_title` to be published, let the fleet settle, and
        return (new notifications, new requests to the phone, new requests to the laptop)."""
        quiet()
        seen, _ = snapshot()
        before_phone = len(requests_to("/p/ok"))
        before_laptop = len(requests_to("/p/refuse"))
        print(label)
        action()
        arrived = wait_for(
            lambda: bool(titled([n for n in notifications() if n.get("id") not in seen], expect_title)),
            timeout=wait_seconds)
        check("the control plane really published %r — without it the rest of this window "
              "would be measuring nothing" % expect_title, arrived)
        quiet()
        fresh = [n for n in notifications() if n.get("id") not in seen]
        return (fresh,
                len(requests_to("/p/ok")) - before_phone,
                len(requests_to("/p/refuse")) - before_laptop)

    fresh, phone_new, laptop_new = window(
        "taking node-a down for real and waiting out the grace window...",
        lambda: sh("docker", "stop", "node-a", check=False),
        "Node offline", 300)
    crit = [n for n in fresh if (n.get("severity") or "").lower() == "critical"]
    check("a node actually going dark wakes the phone — the feature is wired to the FLEET, "
          "not only to its own test button",
          phone_new == len(crit) and len(crit) >= 1,
          "%d pushes to the phone, %d critical notifications" % (phone_new, len(crit)))
    check("and the device that asked for everything got everything",
          laptop_new == len(fresh) and len(fresh) >= 1,
          "%d pushes to the laptop, %d notifications" % (laptop_new, len(fresh)))

    def restart_node_a():
        sh("docker", "start", "node-a", check=False)
        wait_up("https://127.0.0.1:%d/api/auth/session" % NODE_PORTS["node-a"], timeout=120)

    fresh, phone_new, laptop_new = window(
        "bringing node-a back and watching which devices are told...",
        restart_node_a, "Node back online", 300)
    crit = [n for n in fresh if (n.get("severity") or "").lower() == "critical"]
    routine = [n for n in fresh if (n.get("severity") or "").lower() != "critical"]
    # Without this the floor check below is vacuous: if nothing routine happened, "the phone
    # was not woken" is true of a window in which nothing could have woken it.
    check("the window contained something the phone's floor should have held back",
          len(routine) >= 1,
          "%d routine, %d critical" % (len(routine), len(crit)))
    check("a routine recovery reaches the device that asked for everything",
          laptop_new == len(fresh) and bool(titled(routine, "Node back online")),
          "%d pushes, %d notifications" % (laptop_new, len(fresh)))
    check("and the phone that asked for critical only was woken by exactly the critical ones "
          "— the floor is per DEVICE, which is the difference between a phone somebody keeps "
          "listening to and one they mute",
          phone_new == len(crit),
          "%d pushes to the phone, %d critical of %d notifications"
          % (phone_new, len(crit), len(fresh)))

    httpd.shutdown()
    return report()


if __name__ == "__main__":
    sys.exit(main())
