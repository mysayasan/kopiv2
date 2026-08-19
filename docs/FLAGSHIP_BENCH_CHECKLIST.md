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
