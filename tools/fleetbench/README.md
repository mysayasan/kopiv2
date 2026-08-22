# fleetbench — a real fleet, on demand

`fleet_harness.py` stands up the setup several flagship benches share: a containerised
myseliasan control plane and two genuinely adopted mymatasan nodes, with certificates issued
by the real fleet CA and both nodes dialing the real mTLS control channel. No cameras and no
recording — those benches add their own sources (see
`docs/FLAGSHIP_BENCH_CHECKLIST.md`).

`docs/FLAGSHIP_BENCH_CHECKLIST.md` has said "build it once, reuse it" since W1-1. This is
that, written down: the wiring took about ten minutes the first time and every awkward part
is a trap rather than a difficulty.

## Run it

```
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .artifacts/fleetbench/bin/myseliasan ./cmd/myseliasan
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .artifacts/fleetbench/bin/mymatasan  ./cmd/mymatasan
python tools/fleetbench/fleet_harness.py      # stand up cp + node-a + node-b, adopt both
python tools/fleetbench/bench_w22_sla.py      # W2-2: node state history + SLA reporting
```

Container data dirs and bench output go to `.artifacts/fleetbench/` (gitignored); override
with `KOPIV2_BENCH_DIR`. **Point it at a roomy drive**: the node's disk guard reads the
HOST volume through the bind mount, so a nearly-full disk pauses recording fleet-wide and
any bench that needs footage measures nothing. The guard working is a feature.

A bench that needs the nodes to RECORD must also run them on an image that has ffmpeg —
`KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/fleet_harness.py`. Without
it the recorder cuts nothing, silently. Rerunning the harness wipes those dirs, so each run starts from a
fresh install rather than inheriting a rotated password and an already-paired node.

Docker is the only prerequisite; sqlite is pure Go, so no CGO and no database container.

## The traps, all of which cost real time

- **`pairing.parentBaseUrl` decides whether the fleet works at all.** The parent STAMPS this
  URL onto every node it adopts, and the node enrolls and dials the control channel with it.
  Left at its default the node records `localhost:3002` — its own localhost — so enrollment
  fails forever, no certificate is ever issued, the control channel never comes up, and the
  node drifts to "lost" 90 seconds after adoption on its own. A bench that then stops a
  container measures an outage that was already happening. The harness sets it to
  `https://cp:3002`; `certExpiresAt != 0` on the adopt reply is the fast confirmation.
- **Gate any liveness bench on the fleet being genuinely WATCHED**, not merely on a node
  showing "online" — adoption sets that status itself. `bench_w22_sla.settle()` requires both
  nodes to hold online across three consecutive sweeps.
- **The two apps do not authenticate the same way.** mymatasan accepts `Authorization: Basic`
  on everything; myseliasan needs `POST /api/auth/local-login`, a cookie jar, and the CSRF
  token echoed from the `__Host-kopiv2_csrf` cookie into `X-CSRF-Token` on every write. Both
  apps ship a must-change bootstrap admin, so rotate before anything else.
- **`requests`' `session.verify = False` is overridden by `REQUESTS_CA_BUNDLE`** in the
  environment, and the resulting certificate error reads like the app's fault. Set
  `session.trust_env = False` (the harness does).
- **Field names.** The control plane returns the fleet key as `fleetKey`; the node takes it as
  `key`; the node's claim-code reply calls the code `code`; the adopt body wants `nodeId`,
  `httpsPort` and `claimCode`.
- **Ports.** The control plane listens on 39533 (control channel) and 39534 (media).
  `pairing.mtlsPort` 39532 is not something the parent listens on — it is stamped onto nodes,
  so every node app must use it.
- **Config must be BOM-free**, or the Go loader silently falls back to ALL DEFAULTS instead of
  erroring. Write it from Python/the Write tool, never PowerShell's `Out-File -Encoding utf8`.
- **Never read a container's sqlite while the app is running** (mid-WAL over a bind mount) and
  never write it either — a seed written under a running app is discarded on restart. Stop the
  container first.
- The node's rate limiter shares one bucket per path for tunneled calls (they carry no JWT), so
  an exhaustive sweep trips it even though real traffic never would. The harness disables it.
