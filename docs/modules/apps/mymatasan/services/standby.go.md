# Module: apps/mymatasan/services/standby.go

## Purpose

The appliance half of N+1 failover (W3-7): the camera sets this recorder holds on behalf of
others, the proof that it can actually open them, and the one path by which it takes them
over. The control-plane half is `apps/myseliasan/services/failover.go.md`.

## The fact everything follows from

At the moment failover matters, the appliance that held the cameras is unreachable. Nothing
can be fetched from it. So the whole exchange happens **early**, while it is healthy — the
spare is given a copy of the camera set and can be made to **prove** it can open those
cameras before anything has gone wrong. That proof is the product; a spare nobody has tested
is a line item.

## Three things it deliberately does not do

1. **It does not bring back the footage** that was on the lost appliance. That footage is on
   that appliance. Failover restores *recording*, from the moment it happens; the past is
   what the critical-clip archive (W2-3) is for, and only for the clips flagged into it.
2. **It does not stop the lost appliance** — it cannot, that is the premise — and even when
   the appliance comes back it is not told to stand down. A control plane that cannot reach
   a node cannot tell "dead" from "partitioned and recording perfectly". Failover here is
   **additive**: worst case both record the same camera for a while, costing a duplicate
   stream and duplicate footage, and the operator is told and can fail back. That is a far
   better worst case than a fencing protocol that gets it wrong once.
3. **It does not delete the taken-over cameras on fail-back.** The footage recorded here
   during the outage hangs off those camera rows, and removing a camera on this appliance
   purges its footage.

## `IStandbyService`

| Method | Runs on | What it does |
|---|---|---|
| `HandoffKey` | the spare | Mints (or returns) the current one-exchange `infra/handoff` recipient key. TTL 15 min, memory only. |
| `Handoff(req)` | the **protected** recorder | Seals THIS appliance's camera set for a named spare. Refuses `recipientNodeId == self`. |
| `Stage(req)` | the spare | Opens a bundle sealed to it and stores it. |
| `Status` | either | What this appliance is holding, for whom, and how ready each set is. |
| `Drill(sourceNodeId)` | the spare | Asks the cameras in one staged set whether **this** appliance can open them. |
| `Activate(sourceNodeId)` | the spare | Materializes the set and starts recording it. |
| `Release(sourceNodeId)` | the spare | Stops recording. Cameras and footage stay. |
| `Forget(sourceNodeId)` | the spare | Drops a staged set. Never touches a camera row or a byte of footage. |

## The sealed bundle

`standbyBundle` (version 1) carries the source node id/name and one `standbyBundleCamera` per
camera: addressing, ONVIF handles, username, password, and the recording intent read from
`IRecordingService.GetConfig`. It is JSON, sealed with `handoff.Seal(recipientPub,
[]byte(recipientNodeId), plain)` — the recipient's node id as associated data is what stops a
bundle intercepted on its way to node B from being staged onto node C. It is never persisted
in this shape and never leaves either appliance unsealed.

`Stage` refuses: a bundle it has no matching key for (`ErrStandbyKeyUnknown` — the key
expired or the appliance restarted mid-exchange; the fix is to start again, so it says so
rather than surfacing a decryption failure), a bundle sealed for somebody else, an unknown
version, a bundle whose source is this appliance, and a set larger than
`standbyMaxCameras` (512).

Re-staging **preserves what THIS appliance has done with the set**: `LocalCameraId`, `State`,
`ActivatedAt`, `ReleasedAt` and the last drill result all belong here, not to the bundle. A
camera arriving with an empty password keeps the one already held rather than being blanked
into unusability. A camera the source no longer has is dropped — **unless** it has already
been materialized here, because that row is the only link between a local camera (with its
footage) and where it came from.

## `Drill`

Bounded fan-out (`standbyDrillConcurrency` = 4: forty serial probes take minutes, forty
simultaneous RTSP sessions make one appliance look like an attack to a small switch). Each
camera is checked with `ICameraService.VerifyDeviceCredentials` — the **same** ONVIF resolve
plus RTSP DESCRIBE the add-a-camera flow uses — so a drill answers the question recording
will ask, rather than a weaker one (a ping, a TCP connect) that a camera behind a firewall
answers happily.

## `Activate` and the read-back

Per camera: `ICameraService.Save` (reusing `LocalCameraId` when the set has been taken over
before, so the footage from both outages lives under one camera), then
`IRecordingService.SaveConfig`, then **`RecorderConfigBuilder.ForRecording` +
`Manager.Configure`** — because writing a recording config row starts nothing. The settings
screen has always had to hot-reload the recorder after a save, and a failover that only wrote
rows would report success and record nothing until the next restart.

Then the assertion that matters — and it took two passes to get honest.

`Manager.Statuses()` is consulted, but **`FFmpegRunning` alone is not evidence of footage**.
A recorder pointed at a host this appliance cannot resolve has a live process too: it is
retrying. Asking immediately therefore reported every camera as `recording`, including the
ones the drill on the same screen said it could not reach — a card that said both things at
once. (Found by the screen pass; see `FLAGSHIP_BENCH_CHECKLIST.md`.)

So every recorder in the set is started first, and then `settleRecording` waits — once for
the whole set, `standbyActivateSettle` = 12s, not once per camera — until each has
`LiveFiles > 0`, which counts the segment files actually on disk. The verdicts are:

| Outcome | Means |
|---|---|
| `recording` | the process is up AND has written something |
| `pending` | the process is up and nothing has reached the disk yet |
| `not-recording` | no live process for this camera (detail = the recorder's own error) |
| `not-wanted` | the source was not recording it, so neither is this |
| `create-failed` / `config-failed` / `refused` | it never got as far as recording |
| `no-recorder` / `already-recording` | this build has no recorder / it was already running |

`pending` is the honest third answer: a camera that is merely slow and one that will never
connect look identical in the first few seconds, and saying so beats guessing either.

The settle window is a field (`settleFor`) rather than the constant so a unit test can
shorten it — at the shipped value every activation test would spend twelve seconds asserting
something none of them is about.

**The outcome is a CODE, never a sentence.** A sentence composed on the appliance arrives in
English on an Arabic screen; W3-4 shipped that with schedule summaries, W3-6 with the privacy
status line, and this screen pass found it a third time in the takeover table. The appliance
says what happened, the screen says it in the operator's language, and `OutcomeDetail` — a
machine's own words about a failure — stays raw because it cannot be enumerated in advance
and a paraphrase would help nobody who has to act on it.

A camera that could not be created does not stop the rest of the set from being taken over:
the point of a takeover is the cameras it *can* cover.

## `Release` / `Forget`

`Release` writes `Enabled: false` **and** calls `Manager.Configure` with `Enabled: false` —
without the second the ffmpeg process keeps running against a camera this appliance is no
longer responsible for, which is the duplicate stream the fail-back was performed to end. The
camera row and every segment stay; state becomes `released`.

`Forget` drops staged rows, refuses while any camera is `active`, and never deletes a
materialized camera. Purging footage is the camera screen's job, in front of somebody who
meant to.

## Readiness (`buildStandbySet`)

`untested` → `ready` / `partial` / `blind`, plus `active`. **A set nobody has drilled is
`untested`, never `ready`** — the same rule the fleet policy reconciler follows for an
unreachable node: absence of evidence gets its own colour. `blind` (nothing answered) is kept
distinct from `partial` because they send somebody to different places: partial is a camera
problem, blind is a network or credentials problem, and "3 of 40" sends an engineer to look
at three cameras when the answer is the VLAN.

## Consumer interfaces

`standbyCameras`, `standbyRecordings`, `standbyRecorder`, `standbyRecorderConfig`,
`standbyIdentity` — narrow interfaces declared at the consumer rather than depending on
`ICameraService` (fifty-odd methods). A fake in a test is five methods, not fifty stubs of
which forty-five panic, and adding an unrelated ONVIF call cannot break this file.
`standbyIdentity` takes `Status` rather than `NodeID` because the display **name** is needed
too and is a live value — a name captured at boot keeps the old one after a rename until the
next restart.

## Secrets at rest

`seal`/`open` wrap `infra/atrest` for the one secret this table holds, base64-wrapped because
the column is TEXT across sqlite/postgres/mariadb. A value that is not atrest ciphertext
passes through unchanged, so a database written before encryption was enabled still reads.

## Tests

`standby_test.go` runs the real three-step exchange between two service instances. Covers:
the round trip and the recording intent travelling with it; staging creating **no** cameras
and **no** recording configs; a bundle sealed for one spare refused by another; an appliance
refusing to stand by for itself; the three drill verdicts; the recorder read-back (a camera
whose ffmpeg never started must not report "recording"); the source's recording intent being
honoured; fail-back keeping the camera row; re-staging preserving local state and dropping
only unmaterialized removals; `Forget` refused while active; and a per-camera create failure
being reported without sinking the set.

It also carries the two regressions the live run found: a recorder that is up and has
written nothing must not be called recording, and one that has written something must not be
left hedging.

Mutation-checked in three places: trusting the 200 instead of reading the recorder back,
treating an undrilled set as ready, and losing `LocalCameraId` on release — each makes the
matching test fail with a message that names the defect.
