# Module: apps/myseliasan/entities/archived_clip.go

## Purpose

`ArchivedClip` — one event clip the fleet has taken a copy of, off the appliance. The data
model behind the critical-clip archive (flagship hardening plan W2-3, F-09).

A mymatasan node is the sole holder of its own footage, and it sits in the building it is
watching. The failure modes that matter most to a customer — the appliance is stolen,
burned, submerged, or simply wiped by whoever set off the alarm — destroy the evidence of
the very event it is evidence of. Recording retention, tamper detection and continuity
monitoring all assume the box survives. This is the one feature that does not.

## Deliberately narrow

Not "upload everything". The fleet keeps only what a rule was explicitly flagged to keep
(`entities.DetectionRule.ArchiveClip` on the node, default **off**). A control plane
holding every clip from every camera is a storage problem pretending to be a security
feature, and the events that matter would be buried inside it.

## States

`pending` → `fetching` → `stored`, with `failed` and `expired` as terminal outcomes.

| State | Meaning |
|---|---|
| `pending` | The alert has been seen and the clip is wanted. The node may not have finished producing it: an event clip is cut after its post-roll, so a job created the instant the alert arrives is deliberately early. |
| `fetching` | A worker is pulling bytes. Nothing else may claim the job while it holds this. |
| `stored` | The footage is here, encrypted at rest, digest recorded. **The only state that means the appliance is now expendable.** |
| `failed` | Retries exhausted. The row is KEPT — "we tried to keep this and could not" is the most important thing this feature can tell an operator, and a deleted row tells them nothing. |
| `expired` | Retention removed the media; the record of the event survives it. |

## Fields worth knowing

| Field | Notes |
|---|---|
| `NodeId` + `AlertId` | The dedup key. The same alert reaches the control plane twice by design — live on the control channel, then again through replay-on-reconnect — and archiving it twice doubles the storage and shows the operator the same incident twice. |
| `NodeName` / `CameraName` / `RuleName` / `Title` | **Snapshotted at archive time, never resolved on read.** A clip outlives the node that produced it — that is the entire point — so resolving names live shows "unknown node" for exactly the incidents where the appliance is gone, which are the ones anybody is looking at. |
| `Attempts` | Counts fetches that actually REACHED the node. A node that is simply offline does not burn one, or a week's planned shutdown exhausts every pending clip's retries for a reason unrelated to any of them. |
| `Sha256` | The digest of the plaintext clip, computed by the control plane **as the bytes arrived**. A copy nobody can prove is the same footage the appliance recorded is a copy, not evidence. |
| `StoredPath` / `SnapshotPath` | Server-side paths (`json:"-"`), under the clip directory, both encrypted at rest with the same cipher that protects the fleet CA key. |

## Deliberately outside `.selbackup`

This is not an oversight to be tidied up later. The bundle is a control-plane
CONFIGURATION backup — RBAC, the fleet CA, sites, rules — small enough to download over a
VPN and restore onto a fresh install. These rows are worthless without their media, and
the media is measured in gigabytes; including either half alone produces a restore that
lists incidents whose footage is missing, which is worse than listing nothing. The clip
directory is backed up the way footage is always backed up: with a file backup of the data
directory.

## Related

- `services/clip_archive.go.md` — the queue, the fetch, retention, the cap.
- `apis/clips.go.md` — the read surface.
- `apps/mymatasan/entities/detection_rule.go` — `ArchiveClip`, the per-rule flag that
  starts all this.
