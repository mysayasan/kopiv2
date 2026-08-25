# Module: apps/mymatasan/services/face_redactor.go

## Purpose

Obscure the faces in an evidence export (W3-6b). The privacy-zone half of redaction lives in
`evidence_export.go`; this is the half that has to look at every frame.

## The claim this feature can honestly make, and the one it must not

A **privacy zone is a guarantee**: a human named the region, it does not move, and the export
covers it. **Face redaction is not a guarantee and cannot be made into one.** A detector
misses faces in profile, at distance, partly occluded, motion-blurred, or simply because a
frame was hard. Anything in the product implying "no faces are visible in this file" would be
a claim nobody can stand behind.

So the manifest reports what was actually done — frames scanned, detections obscured, the two
safety margins — and carries a `Limitation` sentence saying plainly that faces may remain, and
that a count of detections is not a count of people. The screen repeats it beside the control
*and* beside the finished file. See `ExportFaceRedaction` in `evidence_export.go.md`.

## The misses that CAN be prevented

**Detection flickers.** YuNet finds a face in frames 100 and 102 and misses 101. That single
frame is a full-resolution face in the middle of a clip nobody will scrub through, and it is
the failure that makes naive face blurring worthless — because it looks like it worked.

`planFaceCover` therefore expands every detection twice:

| Expansion | Constant | Why |
|---|---|---|
| In time | `faceHoldFrames` = 3 | Covers the frames either side, against the flicker. Chosen against dropped detections, not against motion. |
| In space | `faceMarginFraction` = 0.28 | The detector's box is tight around the features; the jaw, hairline and ears identify somebody perfectly well. |

Both round outwards and clamp to the frame; **nothing in this file ever shrinks a box**. Too
much black is a complaint, too little is a disclosure.

`faceScoreThreshold` = 0.4 is deliberately **lower** than the 0.7 the face-*recognition* path
uses. Recognition asks "is this Ahmad?", where a weak detection is a false accusation.
Redaction asks "might this be a face?", where a weak detection costs a black rectangle over
something that was not one. Opposite errors are wanted.

**That arithmetic lives in Go, not in the worker**, because it is the difference between a
face being covered and a face being visible — so it belongs where it can be unit-tested and
mutation-checked.

## The pipeline

Two invocations of `ai/face_redact_worker.py`, with Go deciding what happens in between:

1. `--detect` — scans every frame, writes `{width,height,fps,frames,failedFrames,detections}`
   to a file (the bulk never has to survive a pipe).
2. `planFaceCover` in Go → per-frame boxes → `face-cover.json`.
3. `--render` — writes ONE JSON header line to stdout (so the encoder can be started with the
   right size and rate **without ffprobe**, which the appliance's ffmpeg install does not
   guarantee), then raw BGR frames.
4. Go pipes those frames into ffmpeg, which encodes them, applies the privacy-zone `drawbox`
   filter **in the same pass**, and muxes the ORIGINAL audio back in (`-map 1:a:0?` — the `?`
   is why a camera with no audio track does not fail the export).

## The three refusals

- **`Available()` fails → the export is refused at REQUEST time** (in `Create`, not in the
  build), so the operator learns while looking at the form. An export asked to hide faces must
  never come back as a bundle that did not — the shape W3-6's bench found one item earlier,
  where a redact flag was accepted and dropped.
- **`FailedFrames > 0` → refuse.** A frame the detector could not scan is not a frame with no
  faces. A partial scan that produces a bundle labelled face-redacted looks complete, and the
  frames nobody scanned are exactly the ones nobody will check.
- **Frames written ≠ frames scanned → refuse.** A render that stopped early produces a shorter
  file that plays perfectly and simply ends.

`maxFaceRedactionSeconds` (20 min, enforced in `Create`) bounds the job before any work is
done, because a face pass costs the length of the clip rather than a fixed overhead.

## `parseWorkerTail`

The worker's report is written to stderr after the last frame — and **OpenCV writes its own
warnings there too** (`[ WARN:0@0.07] global net_impl_backend.cpp … Targets are not
supported`, on every run of the bench image). Treating stderr as pure JSON turned a completely
successful export into "the worker did not report what it wrote". It reads backwards for the
last decodable JSON line. Found by running the worker in the bench image and reading its
output rather than only its exit code.

## Tests

`face_redactor_test.go` covers the hold across a missed frame, the spatial widening, clamping
an off-edge face without dropping it, not running past the end of the footage, a clip with no
faces, all three `Available()` refusals, the request-time refusal, and the manifest's
limitation wording. Mutation-checked in three places: removing the hold, removing the margin,
and dropping off-edge boxes each fail the matching test with a message that names the defect.

The stderr-noise test exercises the **parser**, not the call site — reverting `render()` to a
naive unmarshal leaves it green. What guards the wiring is the live bench.

## Not claimed

- **No capacity or speed guarantee**: a face pass reads every frame on CPU.
- **No tracking.** Each frame is detected independently; the hold is a fixed window, not a
  motion model. A face that the detector loses for longer than the hold is uncovered for the
  gap.
- The worker needs `opencv-python` and the YuNet model that the face-recognition setup
  downloads. Without them the feature refuses rather than degrading.
