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

## W1-3 · Recording continuity  ⟵ DONE (coverage 2026-08-19, alert 2026-08-21)

Cheapest to run, so do it before the export bench — it also produces the gap that W1-4 needs.

The live-fire version below is the highest-fidelity form and is what a pre-release run
should do. For a fast re-check, the seed-the-past recipe in the W1-3 section further down
exercises the same scoring path in minutes.

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

### RUN 2026-08-19 — W1-4 and W1-2 PASSED, W1-3 partially

Containerised mymatasan on sqlite with encryption-at-rest on and real ffmpeg. Four genuine
H.264 clips were produced with ffmpeg, hashed as plaintext and sealed with the app's own
`infra/atrest` (via `tools/benchseal`, so the bench exercised the real cipher rather than a
reimplementation), then registered as segments across one hour **with a deliberate
15-minute gap**.

22/22 checks passed, plus a tamper check run separately. The ones worth naming:

- the export's **ffmpeg concat ran against real segments for the first time** and produced a
  12.02s H.264+AAC file that ffprobe reads cleanly — a concat that silently produced a
  broken container would still have hashed fine, so the probe is the check that matters
- **recomputing SHA-256 on the downloaded media matched `output.sha256` in the manifest**
- all three sources were labelled `hashOrigin: "recorded"`, i.e. digested at finalize
- the manifest and `VERIFY.txt` both reported the gap, and the preview reported it *before*
  the export could be built
- **corrupting one stored digest made the export REFUSE to run** ("segment 5 failed its
  integrity check"). Without that, the evidentiary claim would be decoration
- the audit trail recorded the export at both request and download, with the actor and the
  operator's stated reason; `/api/audit.csv` downloads

**The bug it found:** the evidence service captured the ffmpeg path **once at boot**, while
every other consumer resolves it live. `services/ffmpeg_install.go` rewrites that setting at
runtime, so an operator who installs ffmpeg **through the product** and then exports gets a
failure that persists until someone restarts the app — on precisely the install where the
product was asked to set ffmpeg up. Now resolved per export.

**Harness notes:** the `Sha256` field's column is `sha_256` (`strcase.ToSnake`), and a
dict-filter seeding by the wrong name silently drops it. A PowerShell here-string piped into
python prepends a BOM to the first line — it ended up inside a stored `file_path` and
produced a puzzling "no such file". The ffmpeg path lives in the `runtime_setting` table
seeded from config at first boot, so editing config.json afterwards changes nothing.

### W1-3 — coverage benched here, alert benched 2026-08-21

Coverage is benched: `GET /api/recording/coverage` reported exactly 75% for the seeded hour
with its 15-minute hole. The gap ALERT was still owed at this point; it was benched on
2026-08-21 by seeding the two past hours the monitor was about to score — see the W1-3
section further down for the run and its traps. W1-5 still needs live siphon frames.

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


## W2-3 · Critical-clip archive  ⟨ DONE (2026-08-21), 22/22

`tools/fleetbench/bench_w23_clips.py`, on top of the shared harness. This is the first
bench in the programme that needs REAL FOOTAGE, so it also stands up a camera: the
`bench-rtsp` image (mediamtx + ffmpeg) publishing `testsrc2` on a network alias, the node
recording it at `segmentMinutes: 1`.

**The headline check is the product claim.** The bench raises a flagged alert, waits for
the clip to be archived, then `docker rm -f`s node-a and deletes every file in its data
directory — and plays the clip back from the control plane, byte-identical to the digest
taken as it arrived. Also verified: an unflagged rule's alert is NOT archived; the served
bytes really are an MP4 (`ftyp`); the digest is published on the response; unauthenticated
reads are refused; and the record still names the node and camera that no longer exist.

**The offline case is exercised for real**: the control plane is stopped, an alert raised
on the node while it is down, the control plane restarted — and the clip is archived on
reconnect. That is the half that would be easy to get wrong.

**The defect it found:** an alert raised without an image (the API path) made the archive
store the node's JSON refusal as the "snapshot". Fixed with a signature check, plus an
error message that distinguishes a missing snapshot from missing footage.

### Traps this bench added

- **The node image needs ffmpeg.** `debian:bookworm-slim` has none, so the recorder cuts
  nothing and a clip bench measures an empty disk. Use `debian-ffmpeg:bench` (that image
  plus `apt-get install ffmpeg`); the harness takes `KOPIV2_NODE_IMAGE`.
- **The ffmpeg PATH is captured into `runtime_setting` at FIRST boot** and never re-read
  from config — a node whose repo was checked out on Windows keeps a `D:\...\ffmpeg.exe`
  that does not exist inside the container, and records nothing, quietly. GET
  `/api/settings/runtime`, patch `decoder.mjpeg.ffmpegPath`, PUT the whole object back
  (it is a nested document, not dotted keys). **Assert the value came back**, or you have
  changed nothing.
- **`PUT /api/recording/config` does NOT reject unknown fields.** `isEnabled` is accepted,
  ignored, and leaves recording off — a 200 that did nothing. The field is `enabled`, and
  the rolls are `preRollSec`/`postRollSec`. **Always read the saved config back.** (The
  camera save next door DOES use `DisallowUnknownFields`, so the two behave oppositely.)
- **The disk guard will stop your bench**, and it reads the HOST volume through the bind
  mount. A 99%-full drive pauses recording fleet-wide with a perfectly clear notification
  that is easy to miss if you only look at the segment list. Put `KOPIV2_BENCH_DIR` on a
  roomy drive — the guard working is a feature.
- **Never assert on a COUNT against a cumulative archive.** "exactly one clip" holds on
  the first run against a fresh control plane and fails for the wrong reason on the
  second. Assert on identity — the unflagged alert's own id is not in there.
- **The node's own vision monitor also fires the rules** you create, on the live stream,
  so more alerts appear than the bench posted. Pin every assertion to a specific alert id.
- `POST /api/cameras/discovered` embeds `onvif.Device`: `host`/`port`/`rtspUrl`, and it
  returns the new camera's id as a BARE NUMBER, not an object.

---

## W2-2 · Node state history + SLA reporting  ⟨ DONE (2026-08-21), 33/33

**The harness is checked in and re-runnable**: `tools/fleetbench/fleet_harness.py` stands up a
control plane and two genuinely adopted mymatasan nodes (real fleet CA, real mTLS control
channel, no cameras — this bench only needs liveness), and
`tools/fleetbench/bench_w22_sla.py` drives it. Only the heartbeat SWEEP interval is
compressed (10s); the lost-grace floor of 90s is a shipped value and was left alone.

What passed: a fleet adopted minutes ago reports near-zero coverage over 30 days rather than a
perfect month; a window entirely before adoption is "no data", not 100%; a real `docker stop`
produces exactly one outage with the healthy neighbour untouched; stopping the CONTROL PLANE
for ~110s is recorded as its own blind spot and subtracted (over a pinned window, the
unmonitored time IS the gap, and the healthy node still reads 100% — a gap is not downtime);
the monthly buckets tile the window exactly and pre-fleet months read "no data"; the PDF
carries the Availability section, names both nodes, states the gap in words, and no longer
carries "historical uptime is not yet tracked"; an unauthenticated caller is refused; and
releasing a node removes its history rows from the table (verified by reading the sqlite with
the app STOPPED).

**The defect it found:** a 94-second outage recorded as **10 seconds** — the transition was
dated to the sweep that declared the node lost, one full grace window after contact was
actually lost. See the plan's W2-2 entry. Re-benched at 99s against 100s.

### Traps this bench added to the pile

- **`pairing.parentBaseUrl` is the one that bites.** The parent STAMPS this URL onto every
  node it adopts, and the node enrolls and dials the control channel with it. Left at the
  default the node records `localhost:3002` — its OWN localhost — so enrollment fails
  forever, no cert is ever issued, the control channel never comes up, and the node drifts to
  "lost" 90 seconds after adoption entirely on its own. The first bench run then "measured" an
  outage that was already happening. **Gate every liveness bench on the fleet being genuinely
  WATCHED** — this one now requires both nodes to hold `online` across three consecutive
  sweeps before anything is stopped, which a node that is merely online because adoption said
  so cannot pass. Check `certExpiresAt != 0` on the adopt reply for a fast signal.
- **`requests`' `session.verify = False` is overridden by `REQUESTS_CA_BUNDLE`** in the
  environment. Against a self-signed bench cert this fails with a certificate error that
  reads like the app's fault. Set `session.trust_env = False`.
- The adopt body wants `nodeId` + `httpsPort` + `claimCode`; the node's claim-code REPLY
  calls it `code`. Three names for two values, as the W2-1 notes said, plus a fourth.
- **fpdf compresses its content streams**, so grepping a PDF for a heading finds nothing.
  Inflate each `stream…endstream` with zlib and pull the `(...) Tj` operands —
  `extract_pdf_text` in `bench.py` does it in a dozen lines. Asserting only on HTTP 200 and
  `%PDF` would have passed with the section missing entirely.
- **A trailing "last 24h" window is a useless assertion target** on a fleet minutes old: it is
  dominated by time before the fleet existed, so almost anything you assert about it passes
  for the wrong reason. Pin `from`/`to` around the event you are actually testing.
- Data dirs survive `docker rm`, so a re-run inherits a rotated password and an already-paired
  node. Wipe them, or try every known password.

## W2-4 · Federated cross-node search  ⟨ DONE (2026-08-22), 36/36

`tools/fleetbench/bench_w24_search.py` on the same two-node harness. No cameras and no
recording: the sightings are SEEDED straight into each node's sqlite with the container
stopped, because what is being benched is the federation, not the detector.

The seed is deliberately awkward in one way — **both nodes number their cameras 1 and 2** —
so a result identified by camera id alone would be ambiguous, and the assertion that each
node joined its OWN camera names actually means something.

What passed: one search returning both nodes' sightings merged newest-first; object, time,
site, plate-text and person-name filters each narrowing correctly; a partial plate and a
lower-cased person name both found on the right node and camera; the descriptor an operator
reads off an alert ("white car") finding the plate; alerts carrying no identity — including
the diagnostics that are the bulk of the table — staying out of identity results; objects and
identities interleaved in one list; the label picker as the fleet-wide union; a truncated
page declaring itself and saying how far back it IS complete; and every search written to the
audit trail with its terms, `partial` when coverage was incomplete and `success` when it was
not.

**The assertion the feature exists for:** `docker stop node-b` mid-investigation, then search
again. node-b is reported BY NAME with status `offline` and a reason, the result refuses to
call itself complete, node-a's sightings still come back, the label picker drops node-b's
labels AND says so — and a search for a person who is only on node-b returns nothing with the
coverage block explaining why. Restarting node-b returns the search to complete.

**The defect the bench found came from the BROWSER, not the API.** A headless-Chrome pass
over the rewritten screen showed every sighting labelled "Recording…" — footage on its way —
on cameras that record nothing at all, because "newer than this camera's newest footage" is
true forever when there is none. Fixed by reading the recording configs to LABEL, never to
filter. **Drive the screen, not only the endpoint: a green API and a screen that lies look
identical from the API side.**

**That pass is now code too: `tools/fleetbench/uicheck.js`.** Headless Chrome over CDP with
no puppeteer and no npm install — sign in, skip the first-run wizard (a fresh install lands
there, and every selector finds nothing until you do), click the nav entry by its label,
submit the page's form, print a JSON summary of the rendered DOM, write a screenshot.
**Assert on the JSON, never on the screenshot.** It took one argument to point it at W2-1's
Fleet Policy screen, which had shipped without ever being loaded in a browser; it renders
correctly.

### Traps this bench added

- **`SendPagingResult` is DOUBLE-wrapped** — `{message, durationMs, data:{result, paging}}` —
  while `SendResult` returns `{message, result}`. Reading the audit endpoint with the
  single-level unwrapper yields an empty list, which is indistinguishable from a feature that
  recorded nothing; the first run of this bench reported exactly that about a perfectly
  working audit trail. The same double-wrapping is why the control plane's node-response
  decoder accepts all three shapes.
- **Seed with a VERIFIED column list.** `PRAGMA table_info` first and fail loudly on any key
  that is not a real column: a mismatched name is dropped in silence by the dict filter and
  the row lands with that field empty, which reads as a bug in whatever consumes it.
- **Compute the expected count from the seed, not from memory.** The first run asserted two
  rows in a time window that genuinely contains three — a bench bug that looked exactly like
  a filter being off by one boundary.

## W2-5 · Staged version rollout  ⟨ DONE (2026-08-22), 22/22

`tools/fleetbench/bench_w25_rollout.py`, on the two-node harness with
`KOPIV2_NODE_HOME_RW=1`.

**What is real and what is stood in for.** The download half of a self-update needs a real
published release, which a bench cannot conjure — so the HALT path is exercised completely
(the rollout asks for a version that was never published, the node's own updater fails on it,
and the node's own words come back in the halt reason), while the SUCCESS path performs the
half a real update performs at the end: the canary's binary is replaced with one built at the
target version and the container restarted. That is the whole of what the control plane can
observe, because the gate judges what a node REPORTS.

What passed: the control plane's record of each node's version agrees with what that node says
about itself over the tunnel; a draft plan is never driven; a second concurrent rollout is
refused; ring composition is deterministic across two separate plans; the canary is asked and
the second ring is not; a version that was never published halts the rollout, names what the
node came back running, and leaves the rest of the fleet on the version it started on; the
canary passes only once it reports the target; the next ring waits out the settle window and
then starts; a ring whose node can never reach the target halts one ring in while the node
that DID upgrade keeps its success; and plan / start / halt are all audited, with a halt
recorded as an error rather than a success.

**The defect it found — on the very first run.** The canary halted with "self-update is not
available for this install type". The rollout had been planning across nodes that can never
replace their own binary, and only discovered it by failing on one. Capability is now probed
while PLANNING: such nodes are recorded `unsupported` with the real remedy and left out of the
rings, still listed. **Ask what a plan CANNOT do before it starts, not by watching it fail.**

### Traps this bench added, three of which cost a whole run each

- **A node refuses to self-update when its application directory is read-only** — which the
  standard harness mount is. Any self-update bench needs `KOPIV2_NODE_HOME_RW=1` (a writable
  private copy), or it measures the refusal rather than the feature.
- **Take ground truth from the SUT, not from the repo.** Deriving the "current" version from
  `infra/versioning/version.json` made the run depend on which binary a previous run had left
  in the shared bin dir; eight assertions failed confidently against a fleet that was never on
  the version the bench assumed. It now asks the node.
- **Never `copyfile` over a binary something is executing.** It truncates and rewrites the
  very inode the process is running from, so the "upgraded" node comes back on the old
  version — a swap that looks like it happened and did not. `os.replace` is the fix on Linux,
  and is REFUSED on Windows while another container holds the file. The harness now gives
  every node its own binary (`bin/mymatasan-<name>`), which is what makes "upgrade exactly one
  appliance" testable at all.
- **Restore a file you borrowed byte-for-byte.** The bench edits `version.json` to build a
  differently-versioned binary; restoring it through a text write re-encoded the line endings
  and left the file permanently modified in git with an empty diff.
- Python buffers stdout when it is not a tty, so a long bench looks hung. Run it with `-u`.

## W2-6 · Dropped control-channel events + the replay horizon  ⟨ DONE (2026-08-23), 19/19

`tools/fleetbench/bench_w26_dropped_events.py`. This item is entirely about making silent
loss visible, so the bench's job is to make loss happen for real and prove it is no longer
silent — not to prove a happy path.

**Drops are caused, not simulated:** stop the control plane, raise real alerts on a node so
its notification service tries to forward them up a channel that is down, scrape the node's
own `/metrics`, then bring the control plane back and check the node ADMITS the count on its
hello and the control plane records it in the operator's feed.

**The horizon is SEEDED, not waited for.** Its state derives from `LastSeenAt`, which is a
fact about the PAST — so seed the past and ask the endpoint, exactly as W1-3 did for the gap
alert. No shipped threshold was weakened: the 72h window and the 2/3 warn fraction are the
values that ship; only `LastSeenAt` moves, which is what a real outage moves too. Write it
with the control plane STOPPED, and stop the node too, or it is judged connected on sight.

What passed: a healthy node exports the counters at zero; an event raised while connected is
counted as forwarded and not as a drop; four events raised while disconnected are counted,
labelled with kind AND reason, and never also counted as forwarded; the node admits the exact
number on reconnect; a node 80% through the window reads `approaching` and claims nothing is
unrecoverable yet; past the window it reads `lapsed`, names when recovery stopped being
possible, leaves its healthy neighbour alone, and raises a critical notification an operator
will actually see.

### Traps this bench added — all three of them the same shape

**Every one was a check that could not tell "the thing did not happen" from "I failed to make
it happen."** That is the same defect class the product work keeps finding, and benches
deserve the same standard.

- **A bare `requests.get(..., verify=False)` still honours `REQUESTS_CA_BUNDLE`.** The
  README already recorded this for sessions; a bare call uses the default session and has
  the same problem. The helper returned `""` on failure, so six drop assertions read zero
  against metrics that were present and correct all along. Use a session with
  `trust_env = False`, and **raise instead of returning empty** — an unreadable scrape is a
  broken bench, not a node with no metrics.
- **A trigger whose status you discard is not a trigger.** `POST /api/vision/alerts` needs
  `ruleId`; without it every alert was refused with 400, no notification was ever published,
  and the drop counters correctly stayed at zero. Check the status and fail loudly.
- **An unsampled Prometheus counter is absent from the scrape**, so "no drops" and "no
  instrumentation" are indistinguishable. This one was a REAL product gap the bench caught,
  not a bench bug: the counters are now published at zero from boot.

## W2-7 · Email notification channel  ⟨ DONE (2026-08-23), 34/34 + two screen passes

`tools/fleetbench/bench_w27_email.py`, with `tools/fleetbench/smtp_sink.py` as a REAL
recording SMTP server on the bench network. The claim of this item is "an operator finds out
by email", and nothing short of a real SMTP conversation tests it — so every assertion is on
what the relay RECEIVED (envelope, headers, body), never on what the app said it sent. A 200
from a save endpoint is not a delivered message.

**Both flagships, because the control plane had no outbound leg at all.** `myseliasan` built
a notification service and never called `Configure`: a node going dark reached a browser and
nowhere else. The bench configures the new `notification` settings section, restarts the
control plane, stops a node, and waits for the mail.

What passed — node (`mymatasan`): a critical notification arrives with the right envelope,
subject prefix, severity tag, `X-Kopiv2-*` classification headers and a UTC-labelled body; a
notification below the severity floor is NOT emailed; one rejected recipient does not silence
the alert for the others AND the surviving address is not mailed again by a retry; the `To`
header never names an address the relay refused; switching the relay off silences mail
without deleting anyone's destination; a username with STARTTLS off is refused at save time;
a recipient carrying CR/LF is refused.

Control plane (`myseliasan`): the same two refusals plus "email needs the relay enabled"; the
new `POST /api/settings/notification/test` delivers a real message and FAILS loudly against a
dead relay; settings survive the config.json round trip and restart; the relay password is
never returned to the browser; and stopping a node produces `[HQ] [CRITICAL] Node offline` in
the operator's mailbox.

**Not claimed:** a snapshot attachment travelling end to end. The harness runs no cameras, so
no notification with an image was raised. The MIME assembly is unit-tested with a
decode-and-compare round trip and mutation-checked, but it has not been benched live.

### Traps this bench added

- **Tear the SMTP sink down before re-running the harness.** The sink joins `benchnet`, so
  `teardown` cannot remove the network and `fleet_harness.py` aborts half way through
  `docker network create` — leaving a fleet that looks up but is not adopted.
- **A PUT with no `id` CREATES a destination.** Editing the recipient list without echoing
  the saved id added a SECOND email destination, so every alert was delivered twice — which
  reads exactly like a retry bug in the code under test. Cost a full run.
- **Filter every assertion by subject.** The fleet raises its own mail while the bench runs
  (the node's disk guard alone produced four unrelated criticals), so a check that merely
  counts new messages passes or fails on unrelated traffic and blames the code under test.
- **Match the EVENT, not a word that appears in it.** The node-offline check first looked for
  "node" anywhere in the message and passed on a relayed disk alert — proving mail works
  while saying nothing about a node going dark. It also printed `msgs[0]`'s subject while
  asserting on a different message, so the evidence in the log was not the evidence the check
  used. Now it requires the title `Node offline` AND the control plane's `[HQ]` prefix.

### The screen passes — and the one that lied about itself

`tools/fleetbench/uicheck_settings.js` (myseliasan) and `tools/fleetbench/uicheck_mail_dest.js`
(mymatasan). Both scan the rendered page for untranslated keys, which every API assertion in
the world passes straight over.

- **`uicheck_mail_dest.js` TYPES.** The recipient list is stored as an array and edited as
  text; a controlled textarea whose value is `to.join('
')` is impossible to type a second
  address into, because parsing drops the empty entry the instant you press Enter. It
  renders, the API accepts, unit tests pass, and the feature is unusable. The bench dispatches
  real `Input.insertText` + `Enter` and asserts both addresses survive.
- **Chrome silently refuses a RELATIVE `--user-data-dir`** and never opens the devtools port;
  it surfaces only as "devtools never came up". Resolve the path.
- **Scope a tab click to the settings tab bar.** A page-wide search for "Notifications" finds
  the nav rail's notification FEED first (it renders as `Notifications10`, label plus unread
  count), navigates away from Settings, and every assertion then reports an empty page —
  reading as "the section did not render".
- **A language switch must be PROVEN, not written.** The Arabic pass set a made-up
  localStorage key, so the page stayed in English and the run reported "renders in ar" having
  never switched language — passing for exactly the reason it was written to catch. The key
  is the app's own (`myseliasan_lang`), and the check now fails if the labels are still ASCII
  or the page is not RTL.
- **A summary field that is always empty is a check that checks nothing.** The label list came
  back `[]` because the selector read `childNodes[0]`, which misses any label wrapping a
  `FieldTitle`. Assert the summary is non-empty.

**What the screen pass found:** the snapshot-delivery hint enumerated "webhook/MQTT base64,
Telegram photo" and not email, so an operator on an email destination could not tell whether
the control applied to them. Fixed in all four languages. Same class as W2-4's "Recording…"
label — a green API and a label that lies look identical from the API side.

---

# Phase 3 bench checklist

## W3-1 · Timeline playback  ⟨ DONE (2026-08-23), 29/29 + 25/25 en and 26/26 ar screen passes

`tools/fleetbench/bench_w31_timeline.py` (API) and `tools/fleetbench/uicheck_timeline.js`
(the screen). Needs the ffmpeg node image:

```
KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/fleet_harness.py
KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/bench_w31_timeline.py
node tools/fleetbench/uicheck_timeline.js .artifacts/fleetbench en
node tools/fleetbench/uicheck_timeline.js .artifacts/fleetbench ar
```

**The bench makes a REAL hole.** Two mediamtx sources on `benchnet`, one recording
throughout and one `docker pause`d for 150 seconds mid-run. Pausing the SOURCE rather than
the node is the point: the recorder keeps running and simply has nothing to write, which is
the failure an operator actually meets. It runs ~8 minutes, most of it waiting for footage.
The API bench writes `w31_context.json` (camera ids, window, the hole) for the screen check
to read — run them in that order.

**The assertion worth copying into any playback item: ffprobe the footage.** Every seek in
the product is `at - startedAt`, which is only a real offset if the file holds as many
seconds as its row claims to span. The bench downloads a segment and compares ffprobe's
duration to `endedAt - startedAt` (60.00s vs 60s). Nothing above the storage layer can
notice that being wrong — the player just shows the wrong moment, confidently. It holds
because `infra/recording.segmentEndedAt` stores the PROBED duration, not the close time.

Also asserted on real data: a Range request answers **206** with exactly the bytes asked for
(playback that can only fetch whole clips is a download queue with a scrub bar drawn on it);
the timeline's percentage equals `/api/recording/coverage`'s for the same window to 0.01%;
the shaded width equals the covered seconds claimed; the drawn hole lines up with the real
outage in both position and length; a moment in the hole snaps forward and reports the skip;
a moment past all footage returns `found: false` rather than reaching backwards; one seek
answers for every camera on the wall, and at one instant the working camera plays while the
failed one reports its hole.

**The screen pass found THREE defects the API bench passed straight through**, which is why
the three assertions below exist. (1) Every scrub landed a constant ~5.6% of the window late,
because the bar hit-tested against the surrounding panel while the marks are positioned
inside the inset track — caught only by comparing where the cursor came to rest with where
the click was. (2) The "ending at" control was inert: an unconditional keep-the-cursor-visible
effect snapped the window back to the playhead, so an operator could not navigate to last
night at all — caught by reading the control's value BACK after setting it, rather than
trusting that it took. (3) Changing the zoom and the end time in one tick discarded one of
them, each handler having read the other's pre-change value out of its closure.

**And one more, only in Arabic.** The tile that skipped a hole
rendered "تم التخطي 0 دقيقة" — *skipped 0 minutes* — for a 43-second gap, because the note
rounded to whole minutes. That note's only job is to distinguish "this camera missed the
incident" from "the player mis-seeked", and zero states that nothing was skipped. Fixed with
a magnitude-following formatter; `uicheck_timeline.js` now asserts the figure is never zero
in any of the four languages, and the fix was mutation-checked by restoring the rounding and
watching the check fail with the original wording.

### The traps this one added

- **The screen check must PLAY, not look.** Four things can only fail in a browser: the
  `<video>` src authenticating (the tiles ride the session cookie, not the Basic credentials
  the SPA holds in memory), `currentTime` actually applying (setting it before
  `loadedmetadata` is silently discarded, landing playback at the START of the segment — a
  scrub to 14:52 that plays 14:45 and looks entirely plausible), `playbackRate` surviving a
  source change (the element resets it to 1 on every swap, so a 4× review quietly drops to
  real time at the first segment boundary), and the cursor advancing as frames decode. All
  four are asserted by reading numbers back out of the live elements.
- **Aim clicks at drawn FOOTAGE, not at a fraction of the window.** A fixed fraction lands in
  dead air whenever the footage does not fill the window, and then the check measures the
  gap-snap while claiming to measure the seek. Measure a `.tl-span` rect and click its middle.
- **Read a control's value BACK after setting it.** The gap step sets the zoom and the
  window end before clicking; asserting only that the click happened would have reported a
  broken bar when the truth was that the window never moved. Proving the scene was set is
  what turned "no gap drawn" into a product bug rather than a bench mystery.
- **Set one control per step, with a settle between.** Driving two controlled inputs in a
  single synchronous batch is not what a person does, and it made a real closure-staleness
  bug look like a harness artefact — worth separating so the two are distinguishable.
- **Pause before moving the window.** Leaving playback running means the follow behaviour is
  live while the control is being exercised, and the check races the player.
- **A tile that crosses a segment boundary restarts near zero, and that is playback WORKING.**
  Assert the wall-clock readout advances, not that `currentTime` rose.
- **Pin the window before clicking the gap.** The default window ends at "now", so it slides
  while the bench runs and the drawn hole drifts out from under a click computed a moment
  earlier — producing "no note", which is indistinguishable from a broken note.
- **`[].every(...)` is true.** The zero-gap assertion passed on a run that never reached the
  hole. Any assertion over a collection needs a non-empty guard, or it reports success for
  having tested nothing. Fourth sighting of this pattern in the programme.
- **Assert on failing request URLs, not console text.** "404" with no URL names nothing.
  `Network.responseReceived` with `status >= 400` gives the path. Expect exactly one:
  `/api/system/recovery/gate`, which the app shell probes before login and which is
  deliberately not mounted outside recovery mode. Name it rather than filtering by status, so
  a new failure cannot hide behind it.
- **`--autoplay-policy=no-user-gesture-required`** or the play assertion measures Chrome's
  autoplay block rather than the player.
- **Chrome needs an ABSOLUTE `--user-data-dir`** (already in the other uicheck scripts) and a
  distinct devtools port per script — `uicheck_timeline.js` uses 9225.
- **Sweep the bench's own containers before re-standing the fleet up.** This bench leaves
  `tlsrc-steady` and `tlsrc-gappy` on `benchnet`; like `smtp-sink`, they stop the network
  being deleted, so `docker network create` fails and both nodes come up UNADOPTED — a
  symptom that never mentions containers and looks like the item under test is broken.
  `docker rm -f smtp-sink camsrc tlsrc-steady tlsrc-gappy` first.
- **The docker CLI's `--format` is mangled by bash on Windows**; drive it from PowerShell or
  from Python's subprocess, which is what the benches do.

---

# The fleet harness — build it once, reuse it

**It is now built: `tools/fleetbench/` (see its README).** `fleet_harness.py` does everything
described below — cross-compiled binaries into `debian:bookworm-slim`, one docker network,
BOM-free config in the data dirs, both admins rotated, fleet key generated and pushed, claim
codes taken and both nodes adopted — and wipes the data dirs first so a re-run starts from a
fresh install. What follows is the manual recipe it encodes, kept because the traps are worth
reading before debugging one.

Standing up a real control plane with two genuinely adopted nodes is the setup several
benches share (W2-1 and W2-2 done, W1-3 and W1-5 still owe it). It takes about ten minutes and
the awkward parts are all in the wiring, not the apps.

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

## Cameras: how to give the nodes something to record

**ffmpeg cannot serve RTSP.** `-rtsp_flags listen` is a DEMUXER option; `ffmpeg -h muxer=rtsp`
lists only rtpflags/rtsp_transport/min_port/max_port/buffer_size/pkt_size. A real RTSP
server is not optional — mediamtx is one static Go binary and works.

```
# in the node image AND the source image
apt-get install -y --no-install-recommends ffmpeg
# source container: mediamtx + one ffmpeg publisher per path
ffmpeg -re -f lavfi -i "mandelbrot=size=640x480:rate=15" -c:v libx264 -preset ultrafast   -f rtsp -rtsp_transport tcp rtsp://127.0.0.1:8554/cam1
```

Then on the node: `POST /api/cameras/discovered` to add each camera and
`PUT /api/recording/config` with `segmentMinutes: 1` so a bench sees segments in a minute
rather than an hour.

### Traps, every one of which cost time

- **The ffmpeg path is seeded into `runtime_setting` at FIRST boot** and never re-read from
  config. A node that first booted without ffmpeg keeps whatever it captured — in this run,
  the repo's WINDOWS path, inside a Linux container, reporting `found: false`. Patch it with
  `PUT /api/settings/runtime` (`decoder.mjpeg.ffmpegPath`), not by editing config.json.
- **Two cameras at the same host:port collapse into one.** The second save overwrote the
  first and returned the same id. Give the source container a network ALIAS per camera
  (`docker network connect --alias cam1host --alias cam2host`) so they are distinct devices.
- **The image has no procps.** `pkill` and `ps` do not exist, so a `pkill`-based "cut the
  stream" step silently does nothing and the bench measures a cut that never happened —
  which is exactly how the first run of the W1-3 bench produced two confident, wrong
  failures. Walk `/proc/[0-9]*/cmdline` instead.
- **To hold a publisher down, SIGSTOP it, do not kill it.** Publishers run under a
  `while true` supervisor that restarts them in seconds; a stopped process is still a live
  process, so the supervisor leaves it alone and the camera stays silent for exactly as long
  as the bench says. mediamtx then reports `no stream is available on path 'cam1'`.
- **Do not drive `docker exec` from Git Bash.** MSYS rewrites any argument that looks like an
  absolute path, so `docker exec c bash /scene.sh` becomes `C:/msys64/scene.sh`. Use
  PowerShell (as the memory already says for `docker -v`) or `MSYS_NO_PATHCONV=1` with `//`.
- **PowerShell 5.1 reads a BOM-less file as ANSI.** A bench script written as UTF-8 with an
  em-dash in a comment dies with "string is missing the terminator". Keep `.ps1` files ASCII,
  and never `Set-Content -Encoding ascii` a bash script — it writes CRLF and bash then fails
  with `$'
': command not found`.
- **The disk guard is real and it will stop your bench.** `/data` on a bind mount reports the
  HOST volume, so a nearly-full drive pauses recording fleet-wide with "Recording paused —
  low disk space … resumes automatically below 80%". Put the bench data dir on a roomy drive
  rather than disabling the mitigation — the guard working is a feature, and a node's
  adoption survives moving its data dir.

## What each camera bench still needs


W1-3 (recording continuity) and W1-5 (camera tamper) both need real RTSP sources on the
nodes, which the harness above does not provide: the node containers are
`debian:bookworm-slim` with no ffmpeg. Add an ffmpeg layer to the node image and serve a
generated stream with ffmpeg's own RTSP listener
(`ffmpeg -re -f lavfi -i testsrc2 -c:v libx264 -f rtsp -rtsp_flags listen rtsp://0.0.0.0:8554/cam`)
rather than pulling in a media server. Use `testsrc2`/`mandelbrot`, never a flat gray
frame — flat synthetic frames hash near-identically, so the perceptual-hash dedup collapses
them in capture and they are useless as tamper test data.

### W1-3 · recording continuity — DONE, coverage 2026-08-19 + alert 2026-08-21 (15/15)

Done: the coverage endpoint measured a real gap on real segments (21.19% on a recording
camera vs 2.81% on one whose source had died). The alert is now benched too.

**"Nothing about it can be compressed" was wrong** — worth reading before assuming any
other monitor needs wall clock. The claim was that `FailureThreshold` consecutive closed
hours means two hours of waiting. But the monitor scores **the previous closed hour on
every sweep**, so the two hours it is about to score are already in the PAST and can be
seeded now. Seed both, wait for the next hour boundary, done — no change to
`MinCoveragePercent` (95) or `FailureThreshold` (2), which is the point: compressing the
thresholds would bench software that does not ship. Only `intervalMs` was lowered (to 60s),
which changes sweep frequency and nothing about the scoring.

The general form: **a monitor that scores a closed past window can be benched by seeding
the past, not by waiting for the future.** Ask this of every remaining timed bench.

Recipe (`scratchpad/p1bench/`: `seed_w13.py`, `verify_w13.py`, `w13.json`) — one container,
four cameras: subject ~33% for two consecutive hours, healthy control at 100%, a
recording-disabled control, and an offline camera at 33% for the attribution path.

Result: alert fired 31s after the boundary on the sweep scoring the SECOND bad hour, silent
after the first (the half that is easy to get wrong and indistinguishable afterwards);
correct camera, critical, `coveragePercent: 33.33`, fired once not per sweep; healthy
control never alerted; disabled control never scored; both Prometheus gauges exported; and
the offline camera reported `reason: "camera-offline"` while the reachable one on identical
coverage reported `unexplained`.

Pause suppression is NOT benched (it needs the disk guard to trip) — mutation-tested
instead: delete the `transition == "gap" && paused` check and
`TestContinuityDoesNotAlertWhileTheDiskGuardHasPausedRecording` fails usefully.

#### Harness traps this run cost time to

- **Never write to the app's sqlite while the app is running.** The checklist already said
  never to READ it live; the write side is worse. A seed written to the bind-mounted DB
  under a running app was silently discarded on restart. `docker stop` → write → `docker
  start`. A rolled-back seed also looks exactly like a monitor that ignored the camera:
  the first cam6 attempt hit `NOT NULL constraint failed: camera.host` before `commit()`,
  so the config and segment inserts vanished with it and the camera simply was not scored.
- **Do not grep the log for the alert.** A health-monitor line containing the camera name
  matched first and read as success. Assert against `/api/notifications` and `/metrics` —
  the interfaces an operator actually has.
- **Check the notification's `metadata` field, not `data`.** The structured context is
  persisted as a JSON *string* in `metadata`; `Notification.Data` is marshalled on the way
  into the store. Reading the wrong field made three passing checks look like product
  failures on the first run.
- **Give a check a full sweep interval before believing a negative.** The cam6 attribution
  check ran between the sweep that scored it and the one that published, and reported a
  missing alert that arrived seconds later.
- **`docker --format "{{.Status}}"` needs PowerShell here** — bash mangles the Go template
  and prints the literal `.Status`.

### W1-5 · camera tamper — covered/frozen/recovery proven, MOVED is broken

Done, on live frames with the scene swapped underneath a real recorder: covered fires on a
bright edge-free scene, recovery clears it, and frozen fires independently on a SHARP still
frame without co-firing covered. Scene choices matter and are recorded in the plan.

**MOVED never fires and cannot** — the plan's W1-5 section has the analysis. Not a bench
problem: it compares consecutive samples (transient) while requiring a 3-sample streak
(persistent), and no test in the repo drives it to an alert. Fix it before re-benching;
there is nothing to observe until then.
