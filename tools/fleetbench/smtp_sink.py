"""A recording SMTP sink for the mail benches.

It is a REAL SMTP server, not a mock: the mailer is exercised over an actual
protocol exchange, because that is where its failures live — a header that never
reaches the wire, a RCPT loop that aborts on the first rejection, a body that is
not dot-stuffed. Every accepted message is written to /out as a .eml file plus a
.json sidecar carrying the envelope, so a bench can assert on what the relay
actually received rather than on what the app said it sent.

Two behaviours are configurable through the environment, both of them needed to
bench the partial-delivery contract:

  SINK_REJECT   comma-separated addresses to refuse at RCPT time, the way a real
                relay refuses an unknown mailbox.
  SINK_PORT     listen port (default 1025).

Run it inside the bench network:

    docker run -d --name smtp-sink --network benchnet \\
      -v <outdir>:/out -v <this file>:/app/smtp_sink.py \\
      -e SINK_REJECT=gone@corp.test \\
      python:3-slim python /app/smtp_sink.py
"""
import json
import os
import socket
import socketserver
import threading

OUT = os.environ.get("SINK_OUT", "/out")
PORT = int(os.environ.get("SINK_PORT", "1025"))
REJECT = {a.strip().lower() for a in os.environ.get("SINK_REJECT", "").split(",") if a.strip()}

_seq_lock = threading.Lock()
_seq = [0]


def next_seq():
    with _seq_lock:
        _seq[0] += 1
        return _seq[0]


class Handler(socketserver.StreamRequestHandler):
    timeout = 60

    def reply(self, line):
        self.wfile.write((line + "\r\n").encode("utf-8"))
        self.wfile.flush()

    def handle(self):
        self.reply("220 smtp-sink ESMTP")
        env_from, rcpts, rejected = "", [], []
        while True:
            raw = self.rfile.readline()
            if not raw:
                return
            line = raw.decode("utf-8", "replace").rstrip("\r\n")
            verb = (line.split(" ", 1)[0] or "").upper()

            if verb == "EHLO":
                self.reply("250-smtp-sink")
                self.reply("250 SIZE 35882577")
            elif verb == "HELO":
                self.reply("250 smtp-sink")
            elif verb == "MAIL":
                env_from, rcpts, rejected = addr_of(line), [], []
                self.reply("250 2.1.0 Ok")
            elif verb == "RCPT":
                addr = addr_of(line)
                if addr.lower() in REJECT:
                    rejected.append(addr)
                    self.reply("550 5.1.1 <%s>: Recipient address rejected: User unknown" % addr)
                else:
                    rcpts.append(addr)
                    self.reply("250 2.1.5 Ok")
            elif verb == "DATA":
                if not rcpts:
                    # A relay must not accept DATA with no recipients. Getting this
                    # wrong would let a bench "receive" a message nobody was sent.
                    self.reply("554 5.5.1 No valid recipients")
                    continue
                self.reply("354 End data with <CR><LF>.<CR><LF>")
                body = read_data(self.rfile)
                self.store(env_from, rcpts, rejected, body)
                self.reply("250 2.0.0 Ok: queued")
            elif verb == "RSET":
                env_from, rcpts, rejected = "", [], []
                self.reply("250 2.0.0 Ok")
            elif verb == "QUIT":
                self.reply("221 2.0.0 Bye")
                return
            elif verb == "NOOP":
                self.reply("250 2.0.0 Ok")
            else:
                self.reply("502 5.5.2 Command not implemented")

    def store(self, env_from, rcpts, rejected, body):
        n = next_seq()
        stem = os.path.join(OUT, "msg-%04d" % n)
        # The .eml lands first, so a bench that polls for the .json never reads a
        # sidecar whose message body is not on disk yet.
        with open(stem + ".eml", "w", encoding="utf-8", newline="") as f:
            f.write(body)
        meta = {"from": env_from, "to": rcpts, "rejected": rejected, "bytes": len(body)}
        tmp = stem + ".json.tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(meta, f)
        os.replace(tmp, stem + ".json")
        print("stored %s from=%s to=%s rejected=%s" % (stem, env_from, rcpts, rejected), flush=True)


def read_data(rfile):
    """Read to the lone-dot terminator, undoing dot-stuffing so the bench sees the
    message as the sender wrote it."""
    out = []
    while True:
        raw = rfile.readline()
        if not raw:
            break
        line = raw.decode("utf-8", "replace")
        if line in (".\r\n", ".\n"):
            break
        if line.startswith(".."):
            line = line[1:]
        out.append(line)
    return "".join(out)


def addr_of(line):
    """Pull the address out of 'MAIL FROM:<a@b>' / 'RCPT TO:<a@b>'."""
    if "<" in line and ">" in line:
        return line[line.index("<") + 1:line.index(">", line.index("<"))]
    if ":" in line:
        return line.split(":", 1)[1].strip()
    return ""


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True
    address_family = socket.AF_INET


if __name__ == "__main__":
    os.makedirs(OUT, exist_ok=True)
    print("smtp-sink listening on 0.0.0.0:%d, rejecting %s" % (PORT, sorted(REJECT) or "nothing"), flush=True)
    Server(("0.0.0.0", PORT), Handler).serve_forever()
