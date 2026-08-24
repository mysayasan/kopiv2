# Module: infra/vision/track.go

## Purpose

The identity half of following an object across frames: `trackCore`, `matchTrack` and
`pruneTracks`, shared by every rule that asks a question about time.

Written for line crossing, and extracted here when the dwell rules (W3-4) needed the same
thing. **Extracted rather than copied**: two implementations of "is this the same object I
saw a moment ago" would eventually disagree, and the disagreement would look like one rule
being flaky rather than like two answers to one question.

## Identity first, geometry second

`matchTrack` prefers the stable ByteTrack id the YOLO worker supplies, and falls back to
nearest-centre matching within `maxDistance` when there is none. The id survives two people
crossing paths; nearest-centre does not.

The `used` set stops one track absorbing two candidates in the same pass — which is how two
people standing together become one track that never leaves and loiters forever.

It does not CREATE the track: only the caller knows what its rule-specific state should start
as.

## The grace is in samples, not seconds

`pruneTracks` forgets a track that was not matched in the last `grace` **passes**.

This is the whole point of the function. What is being tolerated is a dropped detection — a
confidence dip, a brief occlusion, somebody walking in front — and detections only happen
when the detector runs. "Not seen for eight seconds" says nothing if the camera was sampled
once in that window: on a camera sampled every twenty seconds a seconds-based grace forgets
every object between samples and no dwell rule can ever reach its threshold, while on one
sampled four times a second the same number absorbs thirty-two consecutive misses.

It is the same confusion W2-2 found in the availability numbers, pointing the other way: do
not read a wall-clock duration as a number of observations.

## Related

- `infra/vision/dwell.go.md`
- `infra/vision/line_crossing.go.md`
