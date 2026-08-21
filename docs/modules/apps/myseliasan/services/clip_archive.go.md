# Module: apps/myseliasan/services/clip_archive.go

## Purpose

The critical-clip archive: take a copy of the footage that matters OFF the appliance
(flagship hardening plan W2-3, F-09). Owns the queue, the fetch, retention and the cap.
Writes `entities/archived_clip.go.md`; read back through `apis/clips.go.md`.

## PULL, not push — the one real design decision

The hardening plan's one-line sketch said the node would *push* clips upward with a retry
queue for the offline case. This pulls instead, and the reasoning is worth keeping:

A push needs the node to hold a durable queue of undelivered clips, retry them against a
control plane that may be down for a week, and manage the disk that queue consumes — on
the appliance whose disk is already the scarce resource and already has a guard that
pauses **recording** when it fills. All new machinery, on the device we are least able to
debug remotely.

A pull needs none of it. The control plane already learns of every node alert the instant
it happens (the notification is forwarded live up the control channel) and again on
reconnect (the 72h replay backfills whatever was missed), so it always knows what it owes.
The queue lives here, where the database is, where the operator is, and where "this clip
was never retrieved" can be shown to somebody. **Retry is not a mechanism — it is just a
row that is still pending.**

And the transport already exists. Recording playback over the tunnel
(`apis/recording_stream.go`) fetches bounded byte ranges from the node's segment download,
because a whole clip cannot fit in one control-channel message. This walks the same path
with the same 8 MiB chunk. No new listener, no new protocol, no new port.

## The flow

1. **`Consider`** is called for every ingested node notification (from
   `republishNodeNotification` in `app.go` — see below). It returns immediately unless
   `Data["archiveClip"]` is set, and dedups on `(nodeId, alertId)`.
2. **`RunOnce`** (leader-gated, once a minute) takes the due `pending` jobs. For each:
   - node not connected → skip, no attempt burned;
   - ask the node for the segment carrying this alert id
     (`GET /api/recording/segments?alertId=`) — an exact lookup, never a time-range guess,
     because on a busy camera a guess picks up a neighbouring event's footage and an
     archive that sometimes holds the wrong clip is worse than one that holds none;
   - no segment yet → wait (the clip is cut after its post-roll); past `clipCutoff`
     (30 min) → give up, loudly, naming the likely cause;
   - walk the download in ranged chunks, hashing as the bytes arrive, into a `.part` file;
   - rename, encrypt at rest, pull the snapshot best-effort, mark `stored`.

**The hook is on `republishNodeNotification`, not on the live event path.** That one
function is also what the reconnect replay funnels through, so an alert raised while the
node's channel was down is archived when it is backfilled. Hooking the live path alone
would archive the easy half and silently skip every clip raised while the link was down —
which is precisely the case the feature exists for.

## What must not go wrong

- **A truncated clip is never stored.** A short MP4 is still a playable video: nothing
  downstream would notice that the last thirty seconds — the part with the incident in it
  — are missing. `pullSegment` compares what arrived against the `Content-Range` total and
  fails the job rather than storing a file that lies.
- **The digest is computed HERE**, over the bytes that actually arrived, not taken from
  the node. A digest the source reports about itself proves only that the source is
  consistent with itself.
- **A snapshot is stored only if it IS an image** (`looksLikeImage`, magic bytes). A 200
  is not a promise: the node answers some conditions with a JSON envelope, and writing
  that to a `.jpg` produces an archive entry that shows a broken image months later and
  makes an operator doubt everything else in it. The live bench walked straight into this.
- **A full archive STOPS and says so** rather than evicting the oldest clip. This is
  evidence; a system that silently deletes evidence to keep ingesting more of it is worse
  than one that stops and complains.
- **Giving up raises an alert.** A clip the fleet was asked to keep and could not is the
  one outcome that must never be discovered months later by somebody looking for footage
  that was never there.
- **Retention keeps the row.** A deleted row leaves an operator unable to tell "we never
  kept it" from "we kept it and it aged out".

## Asserted role: `operator`

Not `viewer` — on a mymatasan node the viewer role has **no** access to `/api/recording`
at all (see its RBAC defaults), so a viewer assertion fails on every clip with a 403 that
looks like a bug in this code. Not `admin` either: fetching a clip needs no write
authority anywhere, and the archive should not hold more power over an appliance than the
job requires. A refusal is reported in words that name the role, because a bare status
code tells an operator nothing about which of the two machines is at fault.

## Tunables (constants, deliberately)

| Constant | Value | Why |
|---|---|---|
| `clipChunkBytes` | 8 MiB | Matches the playback proxy; the channel caps one frame at 16 MiB. |
| `clipMaxBytes` | 512 MiB | A pre/post roll is measured in seconds; anything near this is a misconfiguration, and the control plane must not fill its disk finding out. |
| `clipMaxAttempts` / `clipRetryBase` | 6 / 2 min doubling | Offline does not count. |
| `clipCutoff` | 30 min | Past this the clip is not coming. |
| `clipRetentionDays` | 90 | The row outlives the media. |
| `clipArchiveMaxBytes` | 20 GiB | The cap that stops rather than evicts. A rule with a short cooldown can fire repeatedly, so the cap is what keeps an over-eager rule from becoming a storage incident. |

## Related

- `entities/archived_clip.go.md` — the row, and why it is outside `.selbackup`.
- `apis/clips.go.md` — the read surface and its audit trail.
- `apis/recording_stream.go` — the ranged-fetch-over-tunnel pattern this reuses.
