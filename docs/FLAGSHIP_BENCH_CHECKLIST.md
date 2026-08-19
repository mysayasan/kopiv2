# Phase 1 bench checklist

Everything in Phase 1 of `FLAGSHIP_HARDENING_PLAN.md` is built, unit-tested and race-clean.
None of it is **shipped**, because none of it has been exercised against a running system.
This file is the list of what to do, in the order that wastes least setup.

The rule the suite already runs on: *boot and exercise, don't trust green.* Every bug that
mattered in this repo's history was found by running the thing, not by reading it.

Mark each item off in the plan's status board (`● built, not benched` → `✅ shipped`) as it
passes.

---

## Setup that several benches share

The mymatasan verify recipe: a throwaway app with `HOME`/`DATA` pointed at a scratch dir, a
fresh sqlite DB and a seeded admin. Two RTSP sources are enough — `tools/osdp-sim` is for
doors; for cameras any looping ffmpeg RTSP server or a real camera works.

For the W1-1 bench you additionally need a myseliasan control plane and **two** node
instances, because "one node reconnected" does not prove the CA survived — a single node
could reconnect for the wrong reason.

---

## W1-1 · myseliasan backup & restore  ⟵ do this one first

The only item whose failure mode is unrecoverable, and the only one whose test cannot be
faked by a unit test.

1. Stand up a control plane. Adopt **two** nodes. Confirm both show a live mTLS heartbeat.
2. Create a site with a floor and upload a plan image; pin a camera on it. (This exercises
   the on-disk encrypted images, which travel differently from the database rows.)
3. Settings → Backup & Restore → export a full `.selbackup` with a passphrase. Keep it
   somewhere off this machine.
4. **Destroy the install properly**: stop the app, drop the database, **and delete the
   at-rest key file**. Deleting only the database does not test anything — the point is a
   destination host with a *different* encryption key.
5. Fresh install. Let it generate a new at-rest key. Complete first-run.
6. Restore the backup. Confirm it reports that a restart is required.
7. Restart.

**Pass:** both nodes reconnect over mTLS with **no re-adoption and no claim code**, and the
floor plan image renders. Nothing else proves the CA survived the key change.

Also check, while you are here:
- The audit section appended rather than replaced (restore twice; the trail should grow).
- Restoring only `settings` in replace mode did **not** delete the CA key.
- **You can still sign in.** See the finding below — this is the check that caught the
  worst bug in the feature, and it is the one an automated test is least likely to make.

### RUN 2026-08-19 — PASSED, after finding one release-blocking bug

Run against a containerised myseliasan on sqlite, two nodes holding certificates issued by
the real CA through the real `/api/nodes/enroll`, destroyed by deleting the database **and**
the at-rest key, restored onto a fresh install with a newly generated key.

14 checks; every one that measures the product passed:

- host B genuinely had a different at-rest key and had minted its own CA first, so a later
  match could not be a no-op
- the CA certificate came back **byte-identical to host A's**
- the CA private key was **re-sealed** under host B's key, not copied verbatim
- both nodes returned with their heartbeat tokens intact
- **a node certificate issued by the pre-backup CA completed an mTLS handshake against the
  restored-and-restarted control channel (:39533), and a rogue certificate was rejected** —
  the second half matters, or the first proves only that mTLS is off

**The bug it found:** `ControlUser.PasswordHash` is `json:"-"`, so the bundle's JSON
encoding silently dropped it and **every restored account came back with an empty password.**
The restore reported success, the fleet was intact, and nobody could sign in — the worst
possible outcome for disaster recovery. Fixed by a `backupUser` wrapper, the same treatment
`ManagedNode.Token` already had; a regression test now exercises Export→Restore rather than
the repo layer, because an in-memory fake round-trips the struct and never marshals it.

**Two harness traps worth knowing before re-running this:**

- **Do not read the sqlite file while the app is running.** Over a bind mount you get a
  mid-WAL view. It produced three convincing phantom failures — including a CA hash of
  `e3b0c442…`, which is the SHA-256 of the empty string. Stop the container (so sqlite
  checkpoints) and copy the file, or read through the app's API.
- **A replace-mode restore invalidates the session that issued it**, because it rewrites the
  user rows. Re-authenticate before reading anything back, or the 401 body gets mistaken for
  a result.
- The control plane does **not** listen on `pairing.mtlsPort` (39532) — that is the port the
  parent stamps onto *nodes*. The node-dialed mTLS listener is the control channel, 39533.

---

## W1-3 · Recording continuity

Cheapest to run, so do it before the export bench — it also produces the gap that W1-4 needs.

1. Camera recording normally for at least two full clock hours.
2. Kill the camera's **ffmpeg child** without stopping the recorder (so the app still thinks
   it is recording — that is the failure being detected).
3. Wait for two closed hours to be scored.

**Pass:** a `Recording gap` alert names the camera and the window; the coverage strip on the
Recordings page shows the hole; restoring the stream clears the alert on the next good hour.

Also check:
- A healthy camera produces **no** alert overnight. (This is the one that decides whether
  the feature gets muted in the field. If a normal night trips it, raise the tolerance
  rather than leaving it noisy.)
- Pausing recording via the disk guard does not produce a page of per-camera gaps.

---

## W1-4 · Evidence export

Uses the gap W1-3 just produced.

1. Export a range that deliberately spans that gap. Enter a reason.
2. Confirm the dialog warns about the gap **before** the export can be built.
3. Download the bundle and unzip it.

**Pass:**
- `shasum -a 256 <video>` matches `output.sha256` in `manifest.json`.
- Each source with `hashOrigin: "recorded"` matches what was recorded at finalize.
- The gap is listed under `gaps` with its window, and `VERIFY.txt` says the export is not
  continuous.
- The audit trail has a `recording.export` entry naming the exporter, the range and the
  reason — and a second one when the bundle was downloaded.

Also check:
- A segment recorded **before** this upgrade exports with `hashOrigin: "computed-at-export"`,
  not `"recorded"`.
- An operator (not admin) can export and still cannot delete.
- **Run a Secure Wipe with a bundle still on disk and confirm `<dataDir>/exports` is gone.**
  A bundle is decrypted footage; a wipe that shredded every encrypted recording and left
  plaintext copies beside it would defeat the point of crypto-erase.

---

## W1-2 · Audit trail

Mostly falls out of the benches above — do it last and read the trail they produced.

**Pass:** viewing a recording, downloading one, deleting one, changing retention, changing a
camera credential and creating a user each produced an entry with the actor, their role, the
client IP and the user agent. Deleting shows the camera and time range of what was lost.

Also check:
- `GET /api/audit.csv` downloads and opens.
- A non-admin cannot reach `/api/audit` at all.
- **myseliasan and myidsan are unchanged** — their existing audit screens still work after
  the shared extraction, and myidsan's `myidsan_audit_write_failures_total` metric still
  appears on `/metrics`.

**Known gap:** there is no UI for reading mymatasan's trail yet. The routes and the page
grant exist; a screen does not. Check the API with curl.

---

## W1-6 · Race detector CI

Already shipped and verified locally (66 packages, 0 races). The only thing left is to watch
the first scheduled nightly actually run on GitHub and come back green.

Local reruns need Docker, driven from PowerShell:

```
docker run --rm -v "D:/github/mysayasan/kopiv2:/src" -w /src -e CGO_ENABLED=1 \
  golang:1.26 sh -c "go test ./... -race -count=1 -timeout 30m"
```

(No gcc on the Windows PATH, and MSYS mangles the `-v` path if run from Bash.)

---

## W1-5 · Camera tamper

Needs one camera you can physically reach, and one overnight run.

1. Let the camera watch its normal scene for ~15 minutes (it has to build a baseline).
2. **Cover the lens** — a hand, tape, a bag.
3. Uncover it.
4. **Pan the camera** to face somewhere else, then put it back.
5. **Freeze the stream** (pause the source, or SIGSTOP the ffmpeg child so the connection
   stays up while the picture stops).

**Pass:** each of the three produces its own alert within the debounce window, and each
clears when the condition is removed.

The one that actually matters, and it cannot be rushed:

- **Leave a healthy camera running overnight, through dusk and dawn.** It must produce
  **no** tamper alerts. A dark scene legitimately loses the detail this monitor measures,
  which is why blocked-view checks pause in low light — if a normal night still trips it,
  the fix is the low-light threshold, not a bigger debounce. A tamper monitor that cries
  wolf gets muted within a week, and after that it catches nothing at all.
- Also confirm a lens covered *in daylight* is still caught after the overnight run — i.e.
  the baseline did not quietly learn the dark scene as normal.

---

## After the benches

Once these pass, the branches are ready to push and PR. They are currently a stack:

```
main
 └─ 9dac2be1  docs: the plan
     └─ f86691a2  W1-6  ci/race-nightly
         └─ 0286729d  W1-1  feat/myseliasan-backup
             └─ b3078b13  W1-2  feat/shared-audit
                 └─ dae09a09  W1-3  feat/mymatasan-continuity
                     └─ 79ca7c2f  W1-4  feat/mymatasan-evidence-export
                         └─ f3194ab9  docs: this checklist
                             └─ (W1-5)  feat/mymatasan-tamper            (HEAD)
```

Either PR them as a stack, each based on the previous, or rebase each onto `main` — they are
logically independent.


---

# Phase 2 bench checklist

## W2-1 · Fleet configuration policy + drift  ⟵ half done

The logic half is **done and re-runnable**. `apps/myseliasan/services/fleet_policy_live_test.go`
benches the reconciler against a REAL mymatasan node; it skips unless asked for:

```
# boot a throwaway node (see the mymatasan verify recipe): fresh sqlite, nonTlsPorts=[18089],
# pairing off, rateLimit.enabled=false, then rotate the must-change password.
RUN_NODE_IT=1 NODE_URL=http://127.0.0.1:18089 NODE_AUTH=$(printf 'admin:PASS' | base64)   go test ./apps/myseliasan/services/ -run TestLive -v
```

Passing on 2026-08-19: all 21 catalog fields exist on a real node with the declared types
and can all actually be set; report-only issues no write; enforcing corrects the value and
leaves every ungoverned field in the section untouched; retention writes without the node's
notification credentials in the body; a second pass over a correct node writes nothing; an
unreachable node reports `unknown`.

**Two harness traps:**

- **Turn the node's rate limiter off for this bench.** A tunneled request carries no JWT, so
  every tunneled call shares one bucket per path (`authOnly`, 120 req / 20s). A real sweep is
  ≤15 requests per node per 15 minutes and never comes close, but the exhaustive field test
  fires ~150 in ten seconds and trips it. Same reason the ZAP runs disable it. Pacing is not
  enough — the window is 20 seconds wide.
- Restore each field to the node's own starting value between subtests, or later fields are
  measured against a node the earlier ones moved.

**DONE (2026-08-19).** Benched a second time against a real two-node fleet — see the
plan's W2-1 entry for the full result. Transport over the mTLS control channel,
fleet/site/node precedence with the winning policy named per field, enforce-per-field,
idempotence, the audit trail (including `cp:fleet-policy` on the NODE's own trail),
unauthenticated refusals, and `docker stop` → `unknown`. Only the screen is unexercised.

---

# The fleet harness — build it once, reuse it

Standing up a real control plane with two genuinely adopted nodes is the setup several
benches share (W2-1 done, W1-3 and W1-5 still owe it). It takes about ten minutes and the
awkward parts are all in the wiring, not the apps.

```
# 1. cross-compile (sqlite is modernc/pure Go, so CGO is not needed)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/myseliasan ./cmd/myseliasan
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/mymatasan  ./cmd/mymatasan

# 2. one docker network; bind-mount the app dir as HOME (read-only) and a scratch DATA dir
docker network create benchnet
docker run -d --name cp --network benchnet -p 18080:8080   -v .../bin:/bin/app:ro -v .../apps/myseliasan:/home/app:ro -v .../cp/data:/data   -e MYSELIASAN_HOME=/home/app -e MYSELIASAN_DATA=/data   -w /data debian:bookworm-slim /bin/app/myseliasan
# nodes the same, with MYMATASAN_*, published HTTP ports, and TLS ENABLED (see below)
```

Config goes in the DATA dir and **must be BOM-free** — PowerShell's `Out-File -Encoding
utf8` writes one and the Go loader then silently falls back to ALL DEFAULTS (empty JWT,
bootstrap disabled, a baffling `dial tcp lookup port=0` panic) instead of erroring.

Then: rotate both must-change admins, generate the fleet key on the control plane, push it
to each node, take a claim code off each node, and `POST /api/nodes/adopt`.

## The wiring traps, all of which cost time

- **The two apps do not authenticate the same way.** mymatasan accepts `Authorization:
  Basic` on every request. myseliasan does **not** — it answers `auth cookie not found`
  and needs `POST /api/auth/local-login`, a cookie jar, and the CSRF token echoed from its
  cookie into `X-CSRF-Token` on every write.
- **Three names for two values.** The control plane returns the key as `fleetKey`
  (`GET /api/nodes/fleet-key`); the node's `PUT /api/pairing/fleet-key` takes it as
  **`key`**; the claim code comes back as **`code`**, not `claimCode`. Sending the wrong
  name fails with the misleading *"node rejected adoption: fleet key is not configured"*.
- **Adoption is parent → node over HTTPS**, with an `InsecureSkipVerify` client. The nodes
  must have `server.tlsPorts` set or adoption cannot reach them at all — a self-signed
  cert is fine.
- **Ports**: the control plane listens on **39533** (control channel) and 39534 (media).
  `pairing.mtlsPort` **39532** is not something the parent listens on — it is STAMPED ONTO
  NODES, so every node app must use it.
- **Turn the node's rate limiter off.** A tunneled request carries no JWT, so every
  tunneled call shares one bucket per path (`authOnly`, 120 req / 20s). Real traffic is
  nowhere near it, but any exhaustive sweep trips it, and pacing does not help — the window
  is 20 seconds wide.
- **`/tmp` is not one place.** Git Bash and Windows Python disagree about it, so a script
  that writes JSON in bash and reads it in python must use a relative path.
- `python3` is not on PATH here; `python` is.

## What still needs cameras

W1-3 (recording continuity) and W1-5 (camera tamper) both need real RTSP sources on the
nodes, which the harness above does not provide: the node containers are
`debian:bookworm-slim` with no ffmpeg. Add an ffmpeg layer to the node image and serve a
generated stream with ffmpeg's own RTSP listener
(`ffmpeg -re -f lavfi -i testsrc2 -c:v libx264 -f rtsp -rtsp_flags listen rtsp://0.0.0.0:8554/cam`)
rather than pulling in a media server. Use `testsrc2`/`mandelbrot`, never a flat gray
frame — flat synthetic frames hash near-identically, so the perceptual-hash dedup collapses
them in capture and they are useless as tamper test data.
