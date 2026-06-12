# Module: infra/recording/buffer.go

## Purpose

Provides a thread-safe fixed-capacity circular ring buffer used to hold the pre-roll JPEG frame window for tick-mode recording.

## Responsibilities

- Maintain a circular array of `FrameEntry` values with a configurable capacity.
- Overwrite the oldest entry when the buffer is full so that the most recent `capacity` frames are always available.
- Return a chronological snapshot of all buffered frames without consuming or clearing them.

## Notes

- All methods are protected by a mutex and safe for concurrent use from the vision monitor goroutine and the flush goroutine.
- Capacity is calculated at recorder creation from `PreRollSec × TickFPS`; a minimum of 8 slots is enforced.
- `Snapshot()` copies the entries, so the caller may read or modify the result slice freely.
