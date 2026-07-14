package services

import (
	"context"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// The write-behind batcher: the second throughput lever, behind the deadband.
//
// The deadband decides WHETHER a reading is worth a row. This decides HOW those rows reach
// the disk. Inserting them one at a time means one transaction — one fsync — per reading, and
// SQLite will do a few hundred of those a second at best. Batching a few hundred rows into a
// single transaction turns a per-row fsync into a per-batch one, and the write rate stops
// being the bottleneck.
//
// The deliberate trade: a crash loses whatever is still in the buffer (at most one flush
// interval of readings). That is the right call for telemetry and would be the wrong call for
// an alert. Sensor readings are a sampled, redundant signal — losing 250ms of them costs
// nothing an operator would notice, and the device will report again shortly. Alerts are NOT
// written through here; they go straight to disk, because an alert that is lost in a buffer
// during the crash it was warning about is worse than useless.
//
// Backpressure: if the queue is full the reading is DROPPED and counted, not blocked on. The
// alternative is to make the MQTT broker's publish path wait on the database, which turns a
// slow disk into a broker-wide stall and eventually into devices timing out and reconnecting
// in a storm. Shedding load is the lesser harm, and the drop counter makes it visible rather
// than silent.

// readingRepo is a CONSUMER-DEFINED narrow interface: one method, the only one this needs.
// The generic repo has a dozen; a test fake for this has to implement one.
type readingRepo interface {
	CreateMultiple(ctx context.Context, datasrc string, models []entities.DeviceReading) (uint64, error)
}

// ReadingWriterOptions tunes the batcher.
type ReadingWriterOptions struct {
	// BatchSize is the row count that triggers an immediate flush.
	BatchSize int
	// FlushInterval bounds how long a reading waits in the buffer when the batch is not
	// filling — a quiet building must not leave readings unwritten indefinitely.
	FlushInterval time.Duration
	// QueueSize is the buffer depth before load is shed.
	QueueSize int
	Logf      func(format string, args ...any)
}

func (o ReadingWriterOptions) withDefaults() ReadingWriterOptions {
	if o.BatchSize <= 0 {
		o.BatchSize = 200
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = 250 * time.Millisecond
	}
	if o.QueueSize <= 0 {
		o.QueueSize = 8192
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	return o
}

// ReadingWriter buffers readings and writes them in batches.
type ReadingWriter struct {
	repo  readingRepo
	opts  ReadingWriterOptions
	queue chan entities.DeviceReading

	mu      sync.Mutex
	dropped int64
	written int64

	stopOnce sync.Once
	done     chan struct{}
}

func NewReadingWriter(repo readingRepo, opts ReadingWriterOptions) *ReadingWriter {
	opts = opts.withDefaults()
	return &ReadingWriter{
		repo:  repo,
		opts:  opts,
		queue: make(chan entities.DeviceReading, opts.QueueSize),
		done:  make(chan struct{}),
	}
}

// Enqueue accepts a reading for eventual persistence. It never blocks: a full queue sheds the
// reading and counts it, rather than stalling the broker's publish path on the disk.
func (w *ReadingWriter) Enqueue(r entities.DeviceReading) {
	select {
	case w.queue <- r:
	default:
		w.mu.Lock()
		w.dropped++
		dropped := w.dropped
		w.mu.Unlock()
		// Log on a ramp, not on every drop: under sustained overload the log itself would
		// become the bottleneck.
		if dropped == 1 || dropped%1000 == 0 {
			w.opts.Logf("telemetry write queue is full — shedding readings (%d dropped so far); the disk cannot keep up with ingest", dropped)
		}
	}
}

// Run drains the queue until ctx is cancelled, then flushes what is left.
func (w *ReadingWriter) Run(ctx context.Context) {
	safego.Supervise(ctx, "myiotsan.reading-writer", func(ctx context.Context) {
		ticker := time.NewTicker(w.opts.FlushInterval)
		defer ticker.Stop()

		batch := make([]entities.DeviceReading, 0, w.opts.BatchSize)

		flush := func() {
			if len(batch) == 0 {
				return
			}
			n, err := w.repo.CreateMultiple(context.WithoutCancel(ctx), "", batch)
			if err != nil {
				w.opts.Logf("telemetry batch write failed (%d readings lost): %v", len(batch), err)
			} else {
				w.mu.Lock()
				w.written += int64(n)
				w.mu.Unlock()
			}
			batch = batch[:0]
		}

		for {
			select {
			case <-ctx.Done():
				// Drain what is already queued before going down. A clean shutdown should not
				// throw away readings it has already accepted.
				for {
					select {
					case r := <-w.queue:
						batch = append(batch, r)
						if len(batch) >= w.opts.BatchSize {
							flush()
						}
					default:
						flush()
						w.stopOnce.Do(func() { close(w.done) })
						return
					}
				}
			case r := <-w.queue:
				batch = append(batch, r)
				if len(batch) >= w.opts.BatchSize {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	})
}

// Wait blocks until the writer has drained after its context was cancelled.
func (w *ReadingWriter) Wait(timeout time.Duration) {
	select {
	case <-w.done:
	case <-time.After(timeout):
	}
}

// Stats reports what the writer has done. Dropped is the number that matters: a non-zero and
// growing value means ingest is outrunning the disk, and the deadbands need widening or the
// hardware needs to be faster.
func (w *ReadingWriter) Stats() (written, dropped int64, queued int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written, w.dropped, len(w.queue)
}
