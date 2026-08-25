# fleetbench — a real fleet, on demand

`fleet_harness.py` stands up the setup several flagship benches share: a containerised
myseliasan control plane and two genuinely adopted mymatasan nodes, with certificates issued
by the real fleet CA and both nodes dialing the real mTLS control channel. No cameras and no
recording — those benches add their own sources (see
`docs/FLAGSHIP_BENCH_CHECKLIST.md`).

`bench_w25_rollout.py` (W2-5, staged version rollout) needs one thing none of the other
benches do: a node whose application directory it can actually WRITE INTO, because a
self-update bench that cannot replace the binary measures the refusal, not the feature. Run
the harness with `KOPIV2_NODE_HOME_RW=1` first — see "Self-update benching" below.

`docs/FLAGSHIP_BENCH_CHECKLIST.md` has said "build it once, reuse it" since W1-1. This is
that, written down: the wiring took about ten minutes the first time and every awkward part
is a trap rather than a difficulty.

## Run it

```
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .artifacts/fleetbench/bin/myseliasan ./cmd/myseliasan
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .artifacts/fleetbench/bin/mymatasan  ./cmd/mymatasan
python tools/fleetbench/fleet_harness.py      # stand up cp + node-a + node-b, adopt both
python tools/fleetbench/bench_w22_sla.py      # W2-2: node state history + SLA reporting
python tools/fleetbench/bench_w24_search.py   # W2-4: federated cross-node search
KOPIV2_NODE_HOME_RW=1 python tools/fleetbench/fleet_harness.py  # rerun with writable node homes
python tools/fleetbench/bench_w25_rollout.py  # W2-5: staged version rollout
python tools/fleetbench/bench_w27_email.py    # W2-7: the email channel, both flagships
node   tools/fleetbench/uicheck.js .artifacts/fleetbench objects   # drive a SCREEN
node   tools/fleetbench/uicheck_settings.js  .artifacts/fleetbench en   # myseliasan settings
node   tools/fleetbench/uicheck_settings.js  .artifacts/fleetbench ar   # ...and in RTL
node   tools/fleetbench/uicheck_mail_dest.js .artifacts/fleetbench      # mymatasan, TYPES
KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/bench_w31_timeline.py
node   tools/fleetbench/uicheck_timeline.js .artifacts/fleetbench en  # mymatasan, PLAYS
node   tools/fleetbench/uicheck_timeline.js .artifacts/fleetbench ar  # ...and in RTL
python tools/fleetbench/bench_w32_embedding.py            # W3-2: does the MODEL discriminate?
python tools/fleetbench/bench_w32_appearance.py           # W3-2: search + federation
node   tools/fleetbench/uicheck_appearance.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_appearance.js .artifacts/fleetbench ar
KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/bench_w33_cases.py  # W3-3a: cases
node   tools/fleetbench/uicheck_cases.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_cases.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w33b_walls.py       # W3-3b: video walls (no footage needed)
node   tools/fleetbench/uicheck_wall.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_wall.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w34_dwell.py        # W3-4: time-based rules (no footage needed)
node   tools/fleetbench/uicheck_dwell.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_dwell.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w35_ptz.py          # W3-5a: PTZ, against a real ONVIF device
node   tools/fleetbench/uicheck_ptz.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_ptz.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w35b_events.py      # W3-5b: events, inputs and relay outputs
node   tools/fleetbench/uicheck_relay.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_relay.js .artifacts/fleetbench ar
KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/bench_w36_privacy.py  # W3-6
node   tools/fleetbench/uicheck_privacy.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_privacy.js .artifacts/fleetbench ar
KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/bench_w37_failover.py # W3-7
node   tools/fleetbench/uicheck_failover.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_failover.js .artifacts/fleetbench ar
KOPIV2_NODE_IMAGE=debian-ffmpeg-face:bench python tools/fleetbench/bench_w36b_faceredact.py
node   tools/fleetbench/uicheck_faceredact.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_faceredact.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w39_push.py         # W3-9: mobile push, against a REAL push service
node   tools/fleetbench/uicheck_push.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_push.js .artifacts/fleetbench ar
node   tools/fleetbench/uicheck_policy.js .artifacts/fleetbench en   # W2-1: the policy SCREEN
node   tools/fleetbench/uicheck_policy.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w37b_capacity.py     # W3-7b: failover capacity, real estimate
python tools/fleetbench/bench_w33c_case_feed.py   # W3-3c: a feed entry into a case file
node   tools/fleetbench/uicheck_case_feed.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_case_feed.js .artifacts/fleetbench ar
```

`bench_w37_failover.py` (W3-7, N+1 failover) needs the **ffmpeg node image on BOTH nodes** —
the SPARE is the one that has to record, so patching only the protected node's ffmpeg path
produces a takeover that reports success and writes nothing. It runs for about fourteen
minutes, most of it the real 120s hold-down plus the liveness grace window, because the
feature is ABOUT waiting long enough. It `docker stop`s node-a mid-run and starts it again,
and it downloads a segment the spare wrote and runs ffprobe over it — "a file exists" passes
on a zero-byte file and "the API says enabled" passes on a node with no ffmpeg.

It removes its camera source (`fosrc-one`) in a `finally`, so **seed a recording camera again
before `uicheck_failover.js`** — that check needs node-a to hold one. The screen check creates
its own PLAN through the form, deletes anything a previous run left, and asserts the badge
STATE (`data-fo-ready`) rather than its text, which is what lets it run in four languages and
still assert that the text is not the state token.

`bench_w36b_faceredact.py` (W3-6b, face redaction on export) needs a THIRD node image —
`debian-ffmpeg-face:bench`, which is `debian-ffmpeg:bench` plus python3 and
opencv-python-headless — and `KOPIV2_NODE_PYTHON=python3` when standing the harness up, because
the shipped config names the interpreter by its HOST path and no container has it. It also needs
the YuNet model at `apps/mymatasan/ai/face_detection_yunet_2023mar.onnx`, where a real install
puts it.

**It films a DRAWN face, and YuNet detects it.** That is what lets a fleet with nothing to film
bench a detector end to end: a real detection at a position the bench knows, so the output can be
MEASURED (face region black, background not) rather than admired. The face is stationary and the
background moves — see the checklist entry for why both halves matter.

`uicheck_faceredact.js` drives the export dialog and runs a real face-redacted export from the
browser, so it is slow (a face pass reads every frame). Run the API bench first — the screen
check needs a camera with footage on node-a.

## `onvifsim.py` — a real ONVIF device, on demand

The bench cameras are mediamtx RTSP sources with **no ONVIF service at all**, and everything
in W3-5 is an ONVIF conversation. `onvifsim.py` is a small ONVIF PTZ device (stdlib only,
runs in a bare `python:3-slim` container on `benchnet`) that answers the SOAP calls the
product makes, keeps the state a real dome keeps, and — the part that makes it worth having —
**records every call**, at `GET /journal`.

That is what lets `bench_w35_ptz.py` assert the appliance *sent* `GotoPreset` for the stops of
a tour, in order, spaced by the dwell. A patrol that persuaded its own database it was
running while sending nothing would pass a status-code check and fail this one. It also
exposes `POST /presets/wipe` (somebody clearing the presets from the camera's own web page)
and `POST /journal/reset`.

Every bench and screen check that needs it starts and removes it itself, so none of them
depends on another having been run. **Extend it rather than starting again** for anything
else ONVIF — W3-5b's PullPoint events and relay outputs went into the same file, which took
about twenty minutes.

It now also speaks the W3-5b surface, with two controls a real camera will not give you:

* `POST /inputs/<token>` — flip a digital input. This is how a bench opens a door.
* `POST /subscriptions/expire` — drop every subscription **without telling anybody**, which
  is exactly what a camera does when a lease is not renewed, and the whole reason the event
  listener treats silence as a fault.

For W3-6 it also speaks **Media2 privacy masks**, and it can be told to LIE about them,
which is the whole reason that bench is worth running:

* `POST /masks/mode/<honest|shifted|rectangle|drop>` — store something OTHER than what it was
  sent: a different coordinate space, a squared-off polygon, or nothing at all. A camera that
  accepts a mask with HTTP 200 and applies something else is a real device behaviour, and the
  product's read-back verification exists for exactly it.
* `POST /masks/support/<on|off>` — a camera with no mask support at all.
* `POST /masks/limit/<n>` — how many masks it will hold.

`LOCK` is an **RLock**, not a Lock: `note()` takes it to append to the journal and several
handlers record something while already holding it. With a plain Lock that is a deadlock, and
the symptom is the entire simulator going silent mid-bench — which reads exactly like a
product failure. It was found precisely that way.

`bench_w34_dwell.py` (W3-4) states at the top of the file what it does NOT claim: no
evaluator is driven end to end, because the harness films test patterns and the detector
finds no person, bag or vehicle to track. It benches everything around them — creation,
refusals, persistence across a restart, the alert and notification path, the role model — and
the evaluators are unit-tested and mutation-checked instead.

**`result_list(response, *keys)`** is in the harness now. `result_of` re-wraps a bare array as
`{"result": [...]}`, and three benches have iterated the dict instead, got the string
`"result"`, and reported that the fleet had no nodes / no roles / no rules. Use `result_list`
for any endpoint that answers with a list.

`bench_w33b_walls.py` (W3-3b) runs on the plain node image in about a minute: a wall is an
arrangement of camera ids, so it needs no footage. It restarts node-a to prove walls survive
it, and deletes a camera to prove a wall reports the loss — which is how it found that
deleting a camera failed on most cameras. `uicheck_wall.js` runs TWO Chrome profiles and the
second one is the point: what this feature replaces was a cookie, so reading a wall back in
the profile that saved it proves nothing. Run the API bench first — the screen check looks
for the wall whose camera it deleted.

`bench_w33_cases.py` (W3-3a, case files) needs the **ffmpeg node image** and runs for about
nine minutes: it records real footage on two cameras, opens a case over some of it, then
moves both the segments and the case items three days into the past so the shipped one-day
retention policy considers them expired, and drives every deletion path at them. It leaves
two camera sources behind (`casesrc-one`, `casesrc-two`) and writes `w33_context.json`.
`uicheck_cases.js` does not depend on that file — it creates its own case through the UI —
but it does need footage on the node, so run the bench first.

`bench_w32_embedding.py` (W3-2) runs on the HOST interpreter rather than in a container: the
appearance stage rides torch + torchvision, which the anomaly feature already requires and
which no bench image carries. `bench_w32_appearance.py` needs the fleet up, creates its own
cameras, and SEEDS descriptors directly into each node's sqlite — the harness films synthetic
test patterns, so no detector produces a person to describe. It writes `w32_context.json` for
`uicheck_appearance.js`, so run them in that order.

`bench_w31_timeline.py` (W3-1, timeline playback) needs the **ffmpeg node image** and runs
for about eight minutes: it stands two mediamtx sources up, records both, then `docker
pause`s one for 150 seconds to punch a real hole in its footage. It writes
`w31_context.json` into the bench dir; `uicheck_timeline.js` reads that, so run the two in
that order. The screen check PLAYS rather than looks — it clicks the bar, presses play,
changes speed and reads `currentTime`/`playbackRate` back out of the live `<video>`
elements, because those are the four things that can only fail in a browser.

`bench_w27_email.py` brings its own dependency: `smtp_sink.py`, a real recording SMTP server
run as a container on `benchnet`. **Remove it before re-running the harness** —
`docker rm -f smtp-sink` — or `teardown` cannot delete the network, `docker network create`
fails, and the fleet comes up unadopted.

The same applies to any bench that attaches its own containers to `benchnet`.
`bench_w31_timeline.py` leaves two camera sources behind:

```
docker rm -f smtp-sink camsrc tlsrc-steady tlsrc-gappy casesrc-one casesrc-two
```

**Swapping a node's binary needs the per-node copy.** `node_binary()` gives each container
its own `bin/mymatasan-<name>`, so rebuilding `bin/mymatasan` alone changes nothing until
those copies are refreshed and the containers restarted. The symptom is a bench asserting
against response fields the running build has never heard of.

is the sweep to run before re-standing the fleet up. The symptom of forgetting is always the
same and never mentions containers: the network cannot be recreated, so both nodes come up
unadopted and every assertion fails for a reason unrelated to the item under test.

**`uicheck.js` is the screen half, and it is not optional for an item that ships a screen.**
W2-4's API bench passed 36/36 and the screen it shipped still lied — every sighting from a
camera that records nothing was labelled "Recording…" forever. A green backend and a
misleading screen look identical from the API side. It drives headless Chrome over CDP (no
puppeteer, no npm install): signs in, skips the first-run wizard, clicks the nav entry whose
label you name, submits the page's form, prints a JSON summary of the rendered DOM, and
writes a screenshot. **Assert on the JSON, not on the screenshot** — an assertion you have
to squint at is not one.

Container data dirs and bench output go to `.artifacts/fleetbench/` (gitignored); override
with `KOPIV2_BENCH_DIR`. **Point it at a roomy drive**: the node's disk guard reads the
HOST volume through the bind mount, so a nearly-full disk pauses recording fleet-wide and
any bench that needs footage measures nothing. The guard working is a feature. This bit
W3-3a on a drive at 95%: the nodes paused recording within a minute of boot, and the
symptom — no segments, ever — is indistinguishable from ffmpeg not running. The `bin/`
directory the containers mount lives under the bench dir too, so it moves with it.

A bench that needs the nodes to RECORD must also run them on an image that has ffmpeg —
`KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/fleet_harness.py`. Without
it the recorder cuts nothing, silently. Rerunning the harness wipes those dirs, so each run starts from a
fresh install rather than inheriting a rotated password and an already-paired node.

Docker is the only prerequisite; sqlite is pure Go, so no CGO and no database container.

## Self-update benching (W2-5, `KOPIV2_NODE_HOME_RW`)

By default the harness bind-mounts `apps/<app>` **read-only** into `/home/app` — safe, but a
node whose home is read-only fails `canSelfUpdate`'s writable-probe (a throwaway file it tries
to create) exactly the same way a real package/container install does. That is the correct
default for every bench that doesn't touch self-update; it is a trap for one that does, since
the failure looks identical to "the feature doesn't work" until you notice it's the mount, not
the code.

Set `KOPIV2_NODE_HOME_RW=1` before running `fleet_harness.py` for any bench that exercises a
node's own updater (directly, or driven by a `myseliasan` fleet rollout). This makes
`node_home()` copy `apps/<app>` (minus `views`/`node_modules`/`data`/`logs` — the React source
tree is large and unused at runtime) into a private, writable directory per node under
`KOPIV2_BENCH_DIR`, and bind-mounts *that* instead. It costs a one-time copy per node at
startup and nothing else; every other bench is unaffected since the flag defaults off. Never
make the repo checkout itself writable from a container — a bug in a self-update bench would
then be a bug that can write into your working tree.

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

`bench_w39_push.py` (W3-9, mobile push) needs no node image and no footage, but it does need
`openssl` on PATH and Docker Desktop's `host.docker.internal`. It stands up a **real push
service** on the host over TLS, then **restarts the control-plane container with
`SSL_CERT_FILE` pointing at that service's certificate** — the way Go trusts a CA on Linux.
That is why `start_container` takes an `env` argument: a shipped appliance must not carry a
switch that makes it accept a certificate it should not, so the trust goes in from outside.

It reads the appliance's request **off the wire**: the aes128gcm header layout byte by byte,
the VAPID assertion's audience (the endpoint's ORIGIN, never the full URL), and that the
notification text does not appear anywhere in the body. It does **not** decrypt the payload —
a second implementation of RFC 8291 written in the same sitting would only prove that two
readings of the spec agree with each other. The encryption is checked against the RFC's own
published test vector, byte for byte, in `infra/webpush/webpush_test.go`.

It runs for about seven minutes because it `docker stop`s node-a, **waits out the real liveness
grace window**, and starts it again — and it decides what should have arrived by reading the
control plane's OWN notification log rather than by assuming. That correlation is load-bearing:
the first version waited for "a new request to the phone", got one in sixteen seconds from the
other node reporting low disk on boot, and declared the fleet wiring proved before the event it
was testing had even fired.

`uicheck_push.js` deletes every device and seeds two of its own pointing at `127.0.0.1:9`
(connection refused instantly, so no twenty-second timeout), which puts the panel in the
AIR-GAPPED state — the screen that matters most on this feature. It reads the toast **2.2
seconds** after a click: a toast lives 3.5 seconds, and a four-second wait finds an empty stack
and reports a silent screen on one that spoke.

`uicheck_policy.js` needs nothing but the standing fleet. It deletes every policy first (which
is also what proves the `no policy` verdict renders), then builds one through the FORM, drives
a real drift and a real clearance against node-a over the tunnel, and deletes it again. It
writes TWO screenshots — `policy-<lang>.png` at the end and `policy-drift-<lang>.png` at the
moment the screen is actually saying something. The second one exists because the defect this
check found was only visible by looking at a frame, and the last frame of a run is usually the
least interesting one.

`bench_w37b_capacity.py` needs nothing but the standing fleet — no footage, no ffmpeg image. It
creates as many cameras as the spare says it can carry, plus five, and requires the verdict to
say so. **Every camera gets its own host** (`127.0.0.2`, `127.0.0.3`, …): saving a discovered
camera UPSERTS BY HOST, so fifty cameras at one address are one camera, and the first run of
this bench staged "1 wanted" against a fleet of five. The addresses refuse instantly because
the appliance probes a camera before saving it — a black-holed address costs a full connect
timeout per camera, which is minutes for one.

`bench_w33c_case_feed.py` and `uicheck_case_feed.js` target a NODE (`:18444`), not the control
plane. mymatasan authenticates with **Basic auth held in React state**, not a cookie — so a
plain same-origin `fetch` from the page carries nothing and answers 401 with an empty list,
which looks exactly like "the case was never created". The screen check sends the header
explicitly, the way the SPA's own client does.
