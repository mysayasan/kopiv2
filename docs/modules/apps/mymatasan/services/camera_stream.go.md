# Module: apps/mymatasan/services/camera_stream.go

## Purpose

Handles camera stream lifecycle and MJPEG frame fan-out.

## Design

- One worker per camera ID.
- Worker state stored in `workers map[int64]*streamWorker`.
- Buffered channel (`streamBufferSize=8`) per worker to protect latency.
- WaitGroup-based coordinated shutdown.

## Key Behavior

- `StartAllMjpegStream`: autostarts streams marked `AutoStart=true`.
- `ReadMjpeg`: lazy-start worker if not running.
- `startMjpegStream`: reconnect loop and frame parsing logic.
- `pushFrame`: non-blocking send; drops frame when buffer full.
- `Shutdown`: cancels all workers and waits for exit.

When no rows match `AutoStart=true`, startup continues without error.

## Recovery and Fallback

- On stream disruption (`io.EOF`), sends `nosignal.gif` frame.
- Retries restart with backoff window (`10s`) up to retry threshold.

## Operational Notes

- Keeping frames buffered but bounded reduces tail latency.
- Worker map mutation is mutex-protected.
- Stream lifecycle messages are written through the injected runtime logger when available.
