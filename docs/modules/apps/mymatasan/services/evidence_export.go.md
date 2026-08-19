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
- Builds run detached from the request context, so a bundle is not abandoned half-built when the operator's browser navigates away.
- `exportMaxRangeSeconds` caps one export at 24 hours. Beyond that an operator wants several exports, each with its own stated reason.
- **RBAC: exporting is an operator capability, deleting is not.** `PageRecordings` gained a second level (`use`) granting `/api/evidence`; deleting footage remains superadmin-only. That is the same line drawn twice — an operator who was present at an incident must be able to hand the footage to somebody, and must not be able to destroy it.
- Audited twice on purpose (`recording.export`): at request time, because deciding to take a copy out of the system is the auditable act and a build that later fails must still leave a record; and at download, because requesting a bundle and collecting it can be minutes and one shift apart.
- Covered by `evidence_export_test.go`: gaps mid-range and at both edges, `"gaps":[]` surviving JSON encoding, honest hash-origin labelling, a required reason, range validation, out-of-range segments excluded, a straddling segment counted, and the verification note stating the digest and any gaps.
- **Not yet live-benched.** The acceptance test is in W1-4 of `docs/FLAGSHIP_HARDENING_PLAN.md`: export a range that deliberately spans a gap, then recompute SHA-256 on the output and confirm it matches the manifest, each source hash matches what was recorded at finalize, and the gap is listed with its reason.
