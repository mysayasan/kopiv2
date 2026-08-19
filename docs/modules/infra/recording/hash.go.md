# Module: infra/recording/hash.go

## Purpose

`HashPlaintextFile` computes the hex SHA-256 of a segment's PLAINTEXT mp4, streamed rather than read whole (segments run tens to hundreds of megabytes and there is one call per camera per segment interval).

## Responsibilities

- `HashPlaintextFile(path string) (string, error)` — opens the file and streams it through `crypto/sha256` via `io.Copy`, returning the lowercase hex digest.

## Why plaintext, and why at finalize

- **Plaintext, not ciphertext.** Hashing the plaintext makes the digest stable across an at-rest key change, a backup/restore, and a move between hosts. A ciphertext hash would change on every one of those and prove nothing about the video itself.
- **At finalize, not at export.** Called from `rtsp.go`'s `remuxSegment` (and, best-effort, `adoptSegment`) in the window after the duration/codec probes and before at-rest encryption — the only moment the plaintext mp4 exists in its final form. That ordering is what lets an evidence bundle claim the footage "was not altered between recording and export" (`services/evidence_export.go.md`, `hashOrigin: "recorded"`). A hash computed later, at export time, only proves the file has not changed since the export — a materially weaker claim that must never be presented as the stronger one.

## Notes

- A hashing failure at finalize is not fatal: `remuxSegment` logs and stores the segment without a digest (`Sha256 = ""`) rather than dropping the footage over it. An already-encrypted file (the `adoptSegment` legacy-safety-net path) is deliberately left unhashed rather than given a ciphertext digest that would change on the next key rotation.
- Consumed by `apps/mymatasan/entities/recording_segment.go` (`Sha256` column) via `infra/recording.SegmentResult.Sha256` (`types.go.md`) and by the evidence export service's per-segment integrity check.
- No dedicated test file; exercised indirectly by `apps/mymatasan/services/evidence_export_test.go` (honest hash-origin labelling) and at runtime through `rtsp.go`'s remux/adopt paths.
