# Module: apps/myseliasan/apis/clips.go

## Purpose

The read surface over the fleet's critical-clip archive (W2-3, F-09). Everything it serves
comes from `services/clip_archive.go.md`.

## Routes

| Method | Path | Notes |
|---|---|---|
| GET | `/api/clips?limit=&offset=&nodeId=&state=` | What the fleet has kept, newest event first. |
| GET | `/api/clips/stats` | Counts by state plus `usedBytes`/`capBytes`/`full`. Registered before `/{id}` as a literal segment. |
| GET | `/api/clips/{id}` | One record. |
| GET | `/api/clips/{id}/media` | The footage. Range-capable via `http.ServeContent`, so a browser scrubs an archived clip exactly as it scrubs one on the node. Carries `X-Clip-Sha256`. |
| GET | `/api/clips/{id}/snapshot` | The still image, when the alert had one. |

## No delete route, deliberately

The whole point of this archive is that the footage survives things that happen at the
other end. An operator who can delete a clip from here can undo that from a browser.
Retention removes media on a schedule, and the record of the event outlives the media
either way.

## Every media read is audited

"Who watched this footage" is the question a tender and a GDPR Article 30 record both ask,
and the node's own audit trail cannot answer it once the fleet holds a copy. Recorded as
`clip.view` against `archived_clip`, with the node id, alert id and digest in the metadata.

Only on the **opening (unranged)** request of a playback: a scrubbing `<video>` element
issues dozens of ranged requests for one viewing, and a row per range buries the trail it
is meant to provide. Same split the node's own recording download uses.

## Unavailable media says WHICH half is missing

The clip and its snapshot fail independently. An alert raised without an image has no
snapshot to archive while its footage is perfectly fine, so the two cases have different
messages — reporting the first as "the clip is not available (state: stored)" reads like a
contradiction and sends the reader looking for a bug that is not there. The live bench
produced exactly that sentence, which is how it came to be split.

## Related

- `services/clip_archive.go.md`
- `entities/archived_clip.go.md`
