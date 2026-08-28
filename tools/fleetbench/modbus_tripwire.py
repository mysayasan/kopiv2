# A Modbus TCP device that REMEMBERS WHAT WAS DONE TO IT.
#
# WHY THIS EXISTS, and why it is not the SunSpec simulator. `tools/sunspec-sim` is a device to read
# FROM; this is a device to be measured AT. myiotsan's discovery scanner states a safety posture in
# its own package doc — "READ-ONLY (nothing is ever written to a discovered device)" — and that is
# the single most consequential claim in the whole feature. An active sweep runs against gear the
# operator did not configure and may not own: an inverter mid-export, a PLC holding an interlock, a
# breaker controller. A scanner that writes even one register to identify a device is not a
# discovery tool, it is an incident.
#
# You cannot check that claim by reading the scanner's code and agreeing with it. The Modbus client,
# the SunSpec walker and the scanner are three layers, any of which could issue a write; and a write
# that a device happens to accept looks exactly like a read from the caller's side. So this stands
# up a real device on a real socket and records the FUNCTION CODE of every request that arrives.
# Afterwards the bench asserts a property about the traffic itself: every function code seen was a
# READ. That is evidence, not agreement.
#
# It answers as a plausible SunSpec device so the scanner takes the identification path — the LONGEST
# code path, the one that walks the model chain and decodes the Common block. A tripwire the scanner
# gives up on after one probe proves very little.
#
# It also deliberately ANSWERS writes with success rather than an exception. If the scanner ever did
# write, an error reply might make it retry or fall back to a read path and hide the evidence; a
# device that cheerfully accepts is the harsher, more honest test.
import socket
import struct
import threading

# Modbus function codes, split by what they DO. This division is the whole point of the file.
READ_CODES = {
    1: "read coils",
    2: "read discrete inputs",
    3: "read holding registers",
    4: "read input registers",
}
WRITE_CODES = {
    5: "write single coil",
    6: "write single register",
    15: "write multiple coils",
    16: "write multiple registers",
    22: "mask write register",
    23: "read/write multiple registers",  # a READ in name, a WRITE in effect
}


def sunspec_common(mfg="BenchWorks", model="TRIPWIRE-1", serial="TW-0001", version="1.0"):
    """Build the 66 registers of a SunSpec Common block (model 1).

    Field offsets are the ones sunspec.DecodeCommon actually reads (Mfg 0..16, Model 16..32,
    Version 40..48, Serial 48..64) — taken from the decoder rather than from the spec, so the
    tripwire is identified by THIS app rather than by a standard it might read differently."""
    regs = [0] * 66

    def put(text, at, words):
        raw = text.encode("ascii", "replace")[: words * 2]
        raw = raw + b"\x00" * (words * 2 - len(raw))
        for i in range(words):
            regs[at + i] = (raw[2 * i] << 8) | raw[2 * i + 1]

    put(mfg, 0, 16)
    put(model, 16, 16)
    put(version, 40, 8)
    put(serial, 48, 16)
    return regs


class ModbusTripwire:
    """A SunSpec-looking Modbus TCP device that records every request made of it.

    `requests` accumulates (unit, function, address, quantity). `writes` is the subset that would
    have changed something. A bench asserts on those lists, not on the app's own account of itself."""

    def __init__(self, host="0.0.0.0", port=15502, base=40000, units=(1,)):
        self.host, self.port, self.base = host, port, base
        self.units = set(units)
        self.requests = []
        self.writes = []
        self._lock = threading.Lock()
        self._stop = threading.Event()

        # The register map: marker, one Common model, terminator.
        self.regs = {}
        self.regs[base] = 0x5375      # "Su"
        self.regs[base + 1] = 0x6E53  # "nS"
        self.regs[base + 2] = 1       # model id: Common
        self.regs[base + 3] = 66      # length
        for i, v in enumerate(sunspec_common()):
            self.regs[base + 4 + i] = v
        self.regs[base + 70] = 0xFFFF  # end of chain
        self.regs[base + 71] = 0

        self._sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self._sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self._sock.bind((host, port))
        self._sock.listen(64)
        self._sock.settimeout(0.5)
        self._thread = threading.Thread(target=self._serve, daemon=True)
        self._thread.start()

    # -- what the bench asks it ----------------------------------------------------------
    def seen(self):
        with self._lock:
            return list(self.requests)

    def write_attempts(self):
        with self._lock:
            return list(self.writes)

    def function_codes(self):
        with self._lock:
            return sorted({r["function"] for r in self.requests})

    def reset(self):
        """Start from a known-empty wire. A check that asserts 'nothing was written' must not be
        reading the previous check's traffic."""
        with self._lock:
            self.requests, self.writes = [], []

    def close(self):
        self._stop.set()
        try:
            self._sock.close()
        except Exception:
            pass

    # -- the device ----------------------------------------------------------------------
    def _serve(self):
        while not self._stop.is_set():
            try:
                conn, _ = self._sock.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            threading.Thread(target=self._session, args=(conn,), daemon=True).start()

    def _session(self, conn):
        conn.settimeout(5.0)
        try:
            while not self._stop.is_set():
                head = self._recv(conn, 8)
                if not head:
                    return
                tid, _proto, length, unit = struct.unpack(">HHHB", head[:7])
                func = head[7]
                body = self._recv(conn, length - 2) if length > 2 else b""
                if body is None:
                    return
                resp = self._handle(unit, func, body)
                if resp is None:
                    return
                pdu = bytes([unit]) + resp
                conn.sendall(struct.pack(">HHH", tid, 0, len(pdu)) + pdu)
        except Exception:
            pass
        finally:
            try:
                conn.close()
            except Exception:
                pass

    @staticmethod
    def _recv(conn, n):
        buf = b""
        while len(buf) < n:
            try:
                chunk = conn.recv(n - len(buf))
            except socket.timeout:
                return None
            if not chunk:
                return None
            buf += chunk
        return buf

    def _handle(self, unit, func, body):
        addr = qty = 0
        if len(body) >= 4:
            addr, qty = struct.unpack(">HH", body[:4])
        record = {"unit": unit, "function": func, "address": addr, "quantity": qty,
                  "kind": READ_CODES.get(func) or WRITE_CODES.get(func) or "unknown"}
        with self._lock:
            self.requests.append(record)
            if func in WRITE_CODES:
                self.writes.append(record)

        if unit not in self.units:
            return bytes([func | 0x80, 0x0B])  # gateway target failed to respond

        if func in (3, 4):
            values = [self.regs.get(addr + i, 0) for i in range(qty)]
            payload = b"".join(struct.pack(">H", v & 0xFFFF) for v in values)
            return bytes([func, len(payload)]) + payload
        if func in (1, 2):
            nbytes = (qty + 7) // 8
            return bytes([func, nbytes]) + b"\x00" * nbytes
        if func in (5, 6):
            # ACCEPT it. An exception reply might make a writing scanner retry down a read path
            # and hide the very evidence this file exists to capture.
            return bytes([func]) + body[:4]
        if func in (15, 16):
            return bytes([func]) + body[:4]
        return bytes([func | 0x80, 0x01])  # illegal function


class Blackhole:
    """Accepts TCP connections and then says nothing, ever.

    A scanner's per-host timeout is only real if something exercises it. A closed port fails
    instantly and proves nothing; this makes the connect SUCCEED and then stalls, which is the
    case that actually costs wall-clock — and the one a bounded scan has to survive."""

    def __init__(self, host="0.0.0.0", port=15503):
        self.host, self.port = host, port
        self.conns = 0
        self._stop = threading.Event()
        self._sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self._sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self._sock.bind((host, port))
        self._sock.listen(64)
        self._sock.settimeout(0.5)
        self._held = []
        threading.Thread(target=self._serve, daemon=True).start()

    def _serve(self):
        while not self._stop.is_set():
            try:
                conn, _ = self._sock.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            self.conns += 1
            self._held.append(conn)  # hold it open, answer nothing

    def close(self):
        self._stop.set()
        for c in self._held:
            try:
                c.close()
            except Exception:
                pass
        try:
            self._sock.close()
        except Exception:
            pass
