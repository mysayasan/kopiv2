# Module: apps/mymatasan/services/evidence_export.go

## Purpose

Hand somebody a defensible copy of a span of footage.

This is the moment the whole product is bought for, and until now it did not exist. Playback and download worked per stored segment, so producing "14:05 to 14:40 on camera 3" meant an operator downloading several files and stitching them in an external tool — which destroys any claim the footage is unmodified and leaves no record that it happened.

A bundle is a `.zip` containing one video file, `manifest.json`, and `VERIFY.txt`.

## What makes the bundle evidence rather than a download

- **A required reason.** `Create` refuses an empty one. An evidence export with no stated purpose is the one nobody can account for afterwards; it is written into the audit trail and printed in the bundle.
- **Every gap, always.** `Gaps` is never omitted and never `null` — an empty array means "checked, none found", where a missing field would be indistinguishable from "did not look". An export that silently skips missing footage looks continuous, and a recipient reasonably concludes nothing happened in minutes that are simply absent. That is worse than refusing to export.
- **Two grades of digest, labelled.** `hashOrigin: "recorded"` means the SHA-256 was taken at finalize, before the segment was encrypted (`infra/recording.HashPlaintextFile`) — the strong claim: the footage has not been altered between recording and export. `"computed-at-export"` means no digest was stored (the segment predates hashing, or was adopted after a crash when the file was already encrypted); that digest proves only that the file has not changed since this export and **must not** be read as the stronger claim. `VERIFY.txt` spells the difference out in plain words.
- **An integrity check on the way out.** A segment whose stored digest does not match the bytes on disk fails the export rather than being included silently — that mismatch is exactly what the digest exists to detect.
- **No re-encoding.** The parts are joined with `-c copy`. Re-encoding changes every pixel and hands the other side an obvious argument that the footage was processed.

## Responsibilities

- `IEvidenceExportService` — `Preview` (what an export *would* contain, so the UI can warn about gaps before the operator commits), `Create` (starts an async build), `Get` (job status).
- `plan` resolves the segments overlapping the range and computes coverage, gaps and sources using the **same merged-interval maths** as `recording_coverage.go` — so an export cannot disagree with the coverage strip that sent the operator to it. It widens the query backwards by `coverageLookbackSlack` for the same reason: `GetSegments` filters on `StartedAt` and would otherwise miss the segment covering the first minutes.
- `build` decrypts each source into the work directory (digesting as it goes), joins them, hashes the output, writes the bundle, then deletes the loose parts.
- `verifyNote` is written for somebody who did not build this system and may be reading it years later — a manifest nobody knows how to verify proves nothing.

## Notes

- Bundles land under `<dataDir>/exports`, not the OS temp dir: a bundle is DECRYPTED footage and belongs on the volume the operator already governs. `exportRetention` (6h) removes it afterwards, so decrypted evidence does not accumulate beside the encrypted recordings.
- **The export directory is in the factory reset's shred list** (`resetMediaPaths` in `app/app.go`). A Secure Wipe that shredded every encrypted recording and left plaintext copies of them in `exports/` would defeat the entire point of crypto-erase. Uninstall is already covered — both the Windows and deb/rpm uninstallers remove the whole data root.
- Builds run detached from the request context, so a bundle is not abandoned half-built when the operator's browser navigates away.
- **The ffmpeg path is resolved per export, not captured at construction.** It lives in runtime settings and `services/ffmpeg_install.go` rewrites it, so a boot-time copy goes stale the moment an operator installs ffmpeg through the product — and every export after that fails until a restart. The recorders learned this first; this service now follows.
- `exportMaxRangeSeconds` caps one export at 24 hours. Beyond that an operator wants several exports, each with its own stated reason.
- **RBAC: exporting is an operator capability, deleting is not.** `PageRecordings` gained a second level (`use`) granting `/api/evidence`; deleting footage remains superadmin-only. That is the same line drawn twice — an operator who was present at an incident must be able to hand the footage to somebody, and must not be able to destroy it.
- Audited twice on purpose (`recording.export`): at request time, because deciding to take a copy out of the system is the auditable act and a build that later fails must still leave a record; and at download, because requesting a bundle and collecting it can be minutes and one shift apart.
- Covered by `evidence_export_test.go`: gaps mid-range and at both edges, `"gaps":[]` surviving JSON encoding, honest hash-origin labelling, a required reason, range validation, out-of-range segments excluded, a straddling segment counted, and the verification note stating the digest and any gaps.
- **Live-benched 2026-08-19 and passing.** Real ffmpeg-produced H.264 clips, hashed as plaintext and sealed with the app's own cipher, registered across an hour with a deliberate 15-minute gap: the concat produced a 12.02s file ffprobe reads cleanly, its SHA-256 matched the manifest, the gap was reported in the preview, the manifest and `VERIFY.txt`, and corrupting one stored digest made the export refuse to run. See `docs/FLAGSHIP_BENCH_CHECKLIST.md`.

## `CreateCase` (W3-3)

The same service also builds **case bundles** — every clip in a case file, one manifest, and
the case's chain of custody — reusing `plan`/`materialize`/`concat` unchanged so there is one
implementation of "what footage covers this range". `ExportJob` gained `CaseId` and
`CaseManifest`; which manifest is populated is what tells the two bundle kinds apart. See
`apps/mymatasan/services/case_export.go.md`.

## What the file contains, versus what was asked for (W3-3a)

An export is whole stored segments joined without re-encoding, so a request for 14:05-14:40
against fifteen-minute segments produces a file whose first frame is 14:00. The manifest used
to describe only the REQUEST — `requestedRange`, `coveredSeconds`, `gaps` — and said nothing
about the media, so a recipient counting wall-clock times from the start of the file (the only
thing a recipient can do) was out by the difference. Found by ffprobing a bundle in the W3-3a
bench: an eighteen-second clip that was sixty seconds of video.

`Output` now carries `startsAt`, `endsAt`, `mediaSeconds` and `requestedOffsetSeconds`,
computed in `plan()` from the sources, and `VERIFY.txt` states them in words.

**The footage is not cut to fit, and that is the decision, not an omission.** A stream-copy
cut lands on a keyframe rather than on the requested instant and can break the leading GOP;
handing over less footage than was recorded is a worse answer for evidence than handing over
more and describing it exactly.

`mediaSeconds` is the SUM of the source spans, not `endsAt - startsAt`: gaps between sources
are not in the file, and the file jumps across them. They are listed under `gaps`.

## Export ids are unguessable (W3-3a)

A job is looked up by id alone, so the id is the only thing between a caller holding some
export grant and a bundle they did not create. `exp-<unix>-<counter>` was enumerable inside
the six-hour retention window; ids now carry 8 random bytes (`exportNonce`).

## Redaction (W3-6)

`ExportRequest.Redact` burns the camera's privacy zones into the exported video.

**This is the one place in the product that deliberately breaks the rule stated on
`concat`** - *"an export must not re-encode, because re-encoding changes every pixel and
hands the other side an obvious argument that the footage was processed"*. Redacting **is**
re-encoding; it changes pixels on purpose.

The answer is not to pretend otherwise. A redacted bundle:

- **says so in its filename** (`camera-REDACTED<id>_...`), which is the first thing anybody
  sees and often the only thing that survives being forwarded;
- **declares itself in the manifest** (`redaction.applied`, the region names, the method,
  and a note saying in words that it will not match the source digests);
- **says it again in VERIFY.txt**, first, before the verification steps - that file is the
  one a person reads, and "you are not being shown everything that was recorded" is not a
  footnote;
- **still carries the source digests**, so the derivation is traceable back to footage that
  remains on the recorder and can be exported separately by somebody entitled to it.

**Solid black, not blur.** Blurring is reversible-looking: it invites the argument that
something could be recovered, and on a low-detail region it sometimes can be. The
camera-side mask may blur if an operator prefers, because there the original never existed.

The filter is `drawbox` over each zone's **bounding rectangle**, expressed in fractions of
the frame - so the same zone is correct at any resolution, including after somebody changes
it. Erring towards covering **more** is the only safe direction for a privacy control: too
much black is a complaint, too little is a disclosure.

**A redaction that was asked for and finds no zones does not mark the bundle as redacted.**
A bundle that claims to be redacted with nothing burned into it is a false statement about
what the recipient is being protected from.

The flag is threaded explicitly from the handler into `ExportRequest`. It was not, at first:
this handler builds the service request field by field rather than passing the decoded body,
and the flag was silently dropped - the screen offered it, the service supported it, and the
export came out unredacted with a manifest that correctly said so. The live bench caught it
because it asserted the **manifest**, not merely that the export succeeded.

The bench also measures the pixels (`signalstats` YAVG inside the zone and outside it),
because a bundle that *says* it was redacted and a bundle that *was* are different claims -
and the first version of that check, which guessed from a PNG's file size, passed on
unredacted footage.
