# Module: apps/myiotsan/services/reading_writer.go

## Purpose

The write-behind batcher — the second throughput lever, behind the deadband. The deadband
decides WHETHER a reading is worth a row; this decides HOW those rows reach the disk. Inserting
one at a time means one transaction (one fsync) per reading, and SQLite will do a few hundred of
those a second at best; batching a few hundred rows into one transaction turns that into a
per-batch fsync.

## Key Type: ReadingWriter

```go
func NewReadingWriter(repo readingRepo, opts ReadingWriterOptions) *ReadingWriter
func (w *ReadingWriter) Enqueue(r entities.DeviceReading)
func (w *ReadingWriter) Run(ctx context.Context)
func (w *ReadingWriter) Wait(timeout time.Duration)
func (w *ReadingWriter) Stats() (written, dropped int64, queued int)
```

- `readingRepo` is a **consumer-defined narrow interface** — one method (`CreateMultiple`), the
  only one this needs; a test fake only has to implement one method instead of the generic
  repo's dozen.
- `Enqueue` never blocks: a full queue **drops the reading and counts it** rather than stalling
  the broker's publish path on the disk. The alternative — making publish wait on the database —
  turns a slow disk into a broker-wide stall and eventually into devices timing out and
  reconnecting in a storm; shedding load is the lesser harm. Drop logging ramps (every 1, then
  every 1000) rather than logging every drop, so the log itself does not become the bottleneck
  under sustained overload.
- `Run` (via `safego.Supervise`) drains the queue on a ticker (`FlushInterval`) or when
  `BatchSize` is reached, whichever comes first. On `ctx.Done()` it drains what is already
  queued and flushes before returning, so a clean shutdown does not throw away readings it
  already accepted.
- `Wait(timeout)` blocks the caller (the app's shutdown func) until the drain above completes.
- `Stats()` — `Dropped` is the number that matters: non-zero and growing means ingest is
  outrunning the disk, and either the deadbands need widening or the hardware needs to be
  faster. Surfaced via `GET /api/devices/stats`.

## Key Type: ReadingWriterOptions

`BatchSize` (default 200), `FlushInterval` (default 250ms — bounds how long a reading waits in
a quiet building), `QueueSize` (default 8192 — the shed threshold), `Logf`. Sourced from
`apps/myiotsan/config`'s `telemetry_store` block.

## Notes

- **Deliberate trade**: a crash loses whatever is still in the buffer (at most one
  `FlushInterval` of readings). Correct for telemetry (a sampled, redundant signal — losing
  250ms of it costs nothing an operator would notice) and wrong for an alert — alerts are
  written straight to disk by `services.RuleService.fire`, never through here (see
  `rules.go.md`).
- `context.WithoutCancel(ctx)` is used for the actual DB write inside `flush`, so an in-flight
  batch is not aborted mid-write by the same cancellation that triggered the final drain.
